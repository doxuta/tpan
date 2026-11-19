package steps

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// ApacheModules bật và tắt module Apache (mục 6.1, bước 5): tắt mpm_prefork,
// bật mpm_event proxy_fcgi setenvif rewrite headers http2 (ssl để tới M5, xem mục 6.1).
//
// Đổi MPM không reload được, phải restart apache2: mọi kết nối đang mở bị cắt,
// nên bước này Danger.
type ApacheModules struct {
	Enable  []string
	Disable []string

	// Các module bước này đã thật sự đổi, để Undo chỉ đảo đúng những module đó.
	enabled, disabled []string
	applied           bool
}

var (
	apacheConfigtest = system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
	apacheRestart    = system.Cmd{Name: "systemctl", Args: []string{"restart", "apache2"}}
)

func a2enmod(mods []string) system.Cmd {
	return system.Cmd{Name: "a2enmod", Args: append([]string{"-q"}, mods...)}
}

func a2dismod(mods []string) system.Cmd {
	return system.Cmd{Name: "a2dismod", Args: append([]string{"-q"}, mods...)}
}

func (s *ApacheModules) validate() error {
	if len(s.Enable) == 0 && len(s.Disable) == 0 {
		return errors.New("danh sách module cần bật và cần tắt đều trống")
	}
	for _, m := range append(slices.Clone(s.Enable), s.Disable...) {
		if !modName.MatchString(m) {
			return fmt.Errorf("tên module %q có ký tự không cho phép", m)
		}
	}
	for _, m := range s.Enable {
		if slices.Contains(s.Disable, m) {
			return fmt.Errorf("module %q vừa nằm trong danh sách bật vừa nằm trong danh sách tắt", m)
		}
	}
	return nil
}

func (s *ApacheModules) Info() engine.StepInfo {
	var cmds []string
	if len(s.Disable) > 0 {
		cmds = append(cmds, a2dismod(s.Disable).String())
	}
	if len(s.Enable) > 0 {
		cmds = append(cmds, a2enmod(s.Enable).String())
	}
	cmds = append(cmds, apacheConfigtest.String(), apacheRestart.String())
	return engine.StepInfo{
		ID:    "apache.modules:apache2",
		Title: apacheModulesTitle(s.Enable, s.Disable),
		Why: i18n.T(
			"Thiếu proxy_fcgi thì Apache không chuyển được request sang PHP-FPM và trả mã nguồn PHP hoặc lỗi 500. Còn mpm_prefork thì mỗi kết nối chiếm một tiến trình, máy nhỏ hết RAM khi đông khách; mpm_event và http2 phục vụ nhiều kết nối hơn với cùng RAM. Thiếu rewrite thì đường dẫn đẹp của WordPress trả 404.",
			"Without proxy_fcgi Apache cannot hand requests to PHP-FPM and serves PHP source or a 500 error. With mpm_prefork every connection holds a whole process and a small machine runs out of RAM under load; mpm_event and http2 serve more connections with the same RAM. Without rewrite WordPress pretty links return 404.",
		),
		Commands: cmds,
		Danger:   true,
	}
}

// modEnabled hỏi `a2query -m <mod>`. Theo `man a2query` (Ubuntu 24.04): mã 0
// là đang bật; 32 là không bật; 33 là bị script của gói tắt; 1 là không có
// module đó. Ba mã khác 0 đều là câu trả lời "chưa bật", không phải lỗi.
func modEnabled(ctx context.Context, sys system.System, mod string) (bool, error) {
	_, err := sys.Run(ctx, system.Cmd{Name: "a2query", Args: []string{"-m", mod}})
	if err == nil {
		return true, nil
	}
	if _, ok := isExit(err); ok {
		return false, nil
	}
	return false, fmt.Errorf("hỏi a2query module %s: %w", mod, err)
}

// pending trả các module còn phải tắt và còn phải bật.
func (s *ApacheModules) pending(ctx context.Context, sys system.System) (toDisable, toEnable []string, err error) {
	for _, m := range s.Disable {
		on, err := modEnabled(ctx, sys, m)
		if err != nil {
			return nil, nil, err
		}
		if on {
			toDisable = append(toDisable, m)
		}
	}
	for _, m := range s.Enable {
		on, err := modEnabled(ctx, sys, m)
		if err != nil {
			return nil, nil, err
		}
		if !on {
			toEnable = append(toEnable, m)
		}
	}
	return toDisable, toEnable, nil
}

func (s *ApacheModules) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	dis, en, err := s.pending(ctx, sys)
	return len(dis) == 0 && len(en) == 0, err
}

func (s *ApacheModules) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	toDisable, toEnable, err := s.pending(ctx, sys)
	if err != nil {
		return err
	}
	if len(toDisable) == 0 && len(toEnable) == 0 {
		s.enabled, s.disabled, s.applied = nil, nil, true
		return nil
	}
	// Tắt trước rồi mới bật: a2enmod mpm_event từ chối khi mpm_prefork còn bật
	// (hai MPM xung đột).
	var disabled, enabled []string
	fail := func(err error, restart bool) error {
		if rerr := s.revert(ctx, sys, enabled, disabled, restart); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	if len(toDisable) > 0 {
		if res, err := sys.Run(ctx, a2dismod(toDisable)); err != nil {
			// a2dismod có thể đã tắt được vài module trước khi lỗi; bật lại cả nhóm.
			disabled = toDisable
			return fail(withStdout(fmt.Errorf("tắt module %s: %w", strings.Join(toDisable, " "), err), res), false)
		}
		disabled = toDisable
	}
	if len(toEnable) > 0 {
		if res, err := sys.Run(ctx, a2enmod(toEnable)); err != nil {
			enabled = toEnable
			return fail(withStdout(fmt.Errorf("bật module %s: %w", strings.Join(toEnable, " "), err), res), false)
		}
		enabled = toEnable
	}
	// configtest lỗi thì Apache vẫn đang chạy bằng config cũ trong bộ nhớ: chỉ
	// cần trả module về, không restart.
	if res, err := sys.Run(ctx, apacheConfigtest); err != nil {
		return fail(withStdout(fmt.Errorf("kiểm cấu hình Apache sau khi đổi module: %w", err), res), false)
	}
	if res, err := sys.Run(ctx, apacheRestart); err != nil {
		return fail(withStdout(fmt.Errorf("khởi động lại apache2 sau khi đổi module: %w", err), res), true)
	}
	s.enabled, s.disabled, s.applied = enabled, disabled, true
	return nil
}

func (s *ApacheModules) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("module Apache: %w", errNotApplied)
	}
	if len(s.enabled) == 0 && len(s.disabled) == 0 {
		return nil
	}
	return s.revert(ctx, sys, s.enabled, s.disabled, true)
}

// revert đảo các module đã đổi: tắt những module đã bật trước (để gỡ xung đột
// MPM), rồi bật lại những module đã tắt. restart: apache2 đã được restart bằng
// config mới, nên cần kiểm và restart lại để nó chạy bằng config cũ.
func (s *ApacheModules) revert(ctx context.Context, sys system.System, enabled, disabled []string, restart bool) error {
	if len(enabled) > 0 {
		if res, err := sys.Run(ctx, a2dismod(enabled)); err != nil {
			return withStdout(fmt.Errorf("tắt lại module %s: %w", strings.Join(enabled, " "), err), res)
		}
	}
	if len(disabled) > 0 {
		if res, err := sys.Run(ctx, a2enmod(disabled)); err != nil {
			return withStdout(fmt.Errorf("bật lại module %s: %w", strings.Join(disabled, " "), err), res)
		}
	}
	if !restart {
		return nil
	}
	if res, err := sys.Run(ctx, apacheConfigtest); err != nil {
		return withStdout(fmt.Errorf("kiểm cấu hình Apache sau khi trả module về: %w", err), res)
	}
	if res, err := sys.Run(ctx, apacheRestart); err != nil {
		return withStdout(fmt.Errorf("khởi động lại apache2 sau khi trả module về: %w", err), res)
	}
	return nil
}

// apacheModulesTitle bỏ vế "tắt" khi không tắt gì (Plan SSL của M5 chỉ bật
// ssl), để tiêu đề không cụt thành "bật ssl; tắt ".
func apacheModulesTitle(enable, disable []string) i18n.Text {
	vi, en := "Module Apache: bật "+strings.Join(enable, " "), "Apache modules: enable "+strings.Join(enable, " ")
	if len(disable) > 0 {
		vi += "; tắt " + strings.Join(disable, " ")
		en += "; disable " + strings.Join(disable, " ")
	}
	return i18n.T(vi, en)
}
