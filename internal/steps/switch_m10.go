package steps

// Các Step của "chuyển LEMP ⇄ LAMP" (mục 6.6 của docs/KE-HOACH.md). Hợp đồng ở
// m10_types.go; ba luật chung ở đầu common.go.
//
// Điều xuyên suốt: từ đầu Plan tới WebSwap, web server cũ vẫn giữ cổng 80 và
// 443. Không bước nào ở đây được để web server mới tự chạy hay tự bật khi khởi
// động trước WebSwap, vì nó sẽ tranh cổng với web server cũ.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*AptInstallNoStart)(nil)
	_ engine.Step = (*WebSwap)(nil)
	_ engine.Step = (*ServiceDisable)(nil)
)

// unitEnabled: `systemctl is-enabled` thoát 0 khi unit bật khi khởi động, khác
// 0 (1: disabled, masked; 4: không có unit) khi không. Như unitActive, mọi
// *ExitError là câu trả lời "không".
func unitEnabled(ctx context.Context, sys system.System, unit string) (bool, error) {
	_, err := sys.Run(ctx, systemctl("is-enabled", unit))
	if err == nil {
		return true, nil
	}
	if _, ok := isExit(err); ok {
		return false, nil
	}
	return false, fmt.Errorf("hỏi systemd %s có bật khi khởi động không: %w", unit, err)
}

// ========================================================== AptInstallNoStart

// policyRcPath là file mà invoke-rc.d và deb-systemd-invoke hỏi trước khi khởi
// động dịch vụ trong lúc cài gói (xem /usr/share/doc/init-system-helpers/
// README.policy-rc.d). Nó thoát 101 nghĩa là "không được khởi động".
const policyRcPath = "/usr/sbin/policy-rc.d"

// policyRcMarker là dòng đánh dấu file do tpan ghi. Gặp file có dòng này lúc
// bắt đầu (tpan chết giữa lần trước) thì coi như trước đó không có file, để
// lần này gỡ nó đi thay vì "trả lại" file sót.
const policyRcMarker = "# tpan: block service start during apt-get install; removed right after"

var policyRcData = []byte("#!/bin/sh\n" + policyRcMarker + "\nexit 101\n")

// AptInstallNoStart cài gói mà không cho dịch vụ của gói tự chạy, rồi tắt
// "bật khi khởi động" của các unit trong Disable. Xem hợp đồng ở m10_types.go.
type AptInstallNoStart struct {
	Packages []string // ["apache2"] hoặc ["nginx"]
	Disable  []string // ["apache2"] hoặc ["nginx"]
}

func (s *AptInstallNoStart) validate() error {
	// Cùng luật với AptInstall: tên gói sạch và không kéo theo mod_php.
	if err := (&AptInstall{Packages: s.Packages}).validate(); err != nil {
		return err
	}
	for _, u := range s.Disable {
		if err := checkUnit(u); err != nil {
			return err
		}
	}
	return nil
}

func (s *AptInstallNoStart) cmd() system.Cmd {
	return aptGet([]string{"install", "-y"}, s.Packages...)
}

func (s *AptInstallNoStart) disableCmd(units []string) system.Cmd {
	return system.Cmd{Name: "systemctl", Args: append([]string{"disable", "--now"}, units...)}
}

func (s *AptInstallNoStart) Info() engine.StepInfo {
	names := strings.Join(s.Packages, " ")
	// Policy-rc.d cố ý KHÔNG nằm trong Files: engine ghi mọi file trong Files vào
	// managed_files, mà file này bị gỡ ngay trong Apply, nên health sẽ báo "file
	// không còn" mãi. Hai dòng về nó trong Commands chỉ để người đọc thấy.
	cmds := []string{
		"write " + policyRcPath + " (exit 101)",
		s.cmd().String(),
		"restore " + policyRcPath + " (remove it if it was not there)",
	}
	if len(s.Disable) > 0 {
		cmds = append(cmds, s.disableCmd(s.Disable).String())
	}
	return engine.StepInfo{
		ID:    "apt.install-nostart:" + strings.Join(s.Packages, ","),
		Title: i18n.T("Cài gói nhưng chưa cho chạy: "+names, "Install packages without starting them: "+names),
		Why: i18n.T(
			"Web server mới cần có mặt để ghi cấu hình cho nó, nhưng web server cũ vẫn đang giữ cổng 80 và 443 cho mọi site. Cài theo cách thường thì gói tự khởi động và tự bật khi khởi động máy: nó tranh cổng với web server cũ, và nếu máy khởi động lại giữa chừng thì hai web server giành nhau cổng 80. Bước này cài gói trong lúc chặn tạm việc tự khởi động, rồi tắt luôn việc bật khi khởi động; web server mới chỉ chạy ở bước đổi web server.",
			"The new web server must be present so its configuration can be written, but the old web server still holds ports 80 and 443 for every site. A normal install starts the package and enables it at boot: it fights the old server for the ports, and a reboot in the middle leaves two web servers racing for port 80. This step installs the package while starting is blocked, then turns off starting at boot; the new web server only starts at the swap step.",
		),
		Commands: cmds,
		NoUndo: i18n.T(
			"Gói đã cài không được gỡ khi hoàn tác (gỡ gói có thể kéo theo gói khác), và web server mới vẫn ở trạng thái tắt, không bật khi khởi động: bật lại nó là đem lại đúng chuyện hai web server tranh cổng. Muốn gỡ thì dùng apt-get remove bằng tay.",
			"Installed packages are not removed on undo (removing a package can take others with it), and the new web server stays stopped and disabled at boot: turning it back on would bring back two web servers fighting for the ports. Remove it by hand with apt-get remove if needed.",
		),
	}
}

// unitsOn trả các unit trong Disable còn bật khi khởi động hoặc đang chạy.
func (s *AptInstallNoStart) unitsOn(ctx context.Context, sys system.System) ([]string, error) {
	var on []string
	for _, u := range s.Disable {
		enabled, err := unitEnabled(ctx, sys, u)
		if err != nil {
			return nil, err
		}
		active, err := unitActive(ctx, sys, u)
		if err != nil {
			return nil, err
		}
		if enabled || active {
			on = append(on, u)
		}
	}
	return on, nil
}

func (s *AptInstallNoStart) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	// policy-rc.d của tpan sót lại (tiến trình chết giữa apt-get) thì chưa đạt:
	// bỏ qua bước này là để file đó chặn mọi lần khởi động dịch vụ của apt sau
	// này. Apply coi nó như không có, nên chạy lại là gỡ được.
	if data, err := sys.ReadFile(policyRcPath); err == nil && strings.Contains(string(data), policyRcMarker) {
		return false, nil
	}
	missing, err := missingPackages(ctx, sys, s.Packages)
	if err != nil || len(missing) > 0 {
		return false, err
	}
	on, err := s.unitsOn(ctx, sys)
	return len(on) == 0, err
}

func (s *AptInstallNoStart) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	err := s.install(ctx, sys)
	// Tắt unit cả khi cài lỗi: postinst của gói có thể đã enable unit rồi mới
	// lỗi ở bước sau, và unit enabled thì lần khởi động máy tới có hai web
	// server tranh cổng 80. Chỉ tắt unit đang bật hoặc đang chạy, để cài lỗi vì
	// gói không có thì không thêm lỗi "Unit file does not exist" gây rối.
	on, qerr := s.unitsOn(ctx, sys)
	if qerr != nil {
		return errors.Join(err, qerr)
	}
	if len(on) == 0 {
		return err
	}
	if res, derr := sys.Run(ctx, s.disableCmd(on)); derr != nil {
		return errors.Join(err, withStdout(fmt.Errorf("tắt %s sau khi cài: %w", strings.Join(on, " "), derr), res))
	}
	return err
}

// install là ba việc đầu: đặt policy-rc.d, apt-get install, trả policy-rc.d về.
// Tách hàm riêng để defer trả file chạy NGAY khi apt-get xong (trước bước tắt
// unit), cả khi apt-get lỗi hay ctx bị huỷ: để sót file này thì mọi lần
// apt-get sau đó trên máy (kể cả unattended-upgrades) không khởi động lại được
// dịch vụ nào.
func (s *AptInstallNoStart) install(ctx context.Context, sys system.System) (err error) {
	prev, err := capture(sys, policyRcPath)
	if err != nil {
		return err
	}
	if prev.exists && prev.link == "" && bytes.Contains(prev.data, []byte(policyRcMarker)) {
		prev = saved{path: policyRcPath} // file sót của tpan: gỡ, không trả lại
	}
	// Đặt defer trước khi ghi: lỡ có file sót của tpan mà ghi lỗi thì vẫn gỡ nó.
	defer func() {
		if rerr := prev.restore(sys); rerr != nil {
			err = errors.Join(err, fmt.Errorf("trả %s về như trước: %w", policyRcPath, rerr))
		}
	}()
	if err := sys.WriteFileAtomic(policyRcPath, policyRcData, 0o755, system.Root); err != nil {
		// Không chặn được thì không cài: gói tự chạy sẽ tranh cổng với web server cũ.
		return fmt.Errorf("ghi %s để chặn dịch vụ tự khởi động: %w", policyRcPath, err)
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("cài gói %s: %w", strings.Join(s.Packages, " "), err), res)
	}
	return nil
}

func (s *AptInstallNoStart) Undo(context.Context, system.System) error { return nil }

// ===================================================================== WebSwap

// WebSwap dừng web server cũ (From) rồi khởi động web server mới (To): mục 6.6
// bước 3. Xem hợp đồng ở m10_types.go.
type WebSwap struct {
	From, To string // "nginx", "apache2"

	applied bool
}

func (s *WebSwap) validate() error {
	if err := checkUnit(s.From); err != nil {
		return err
	}
	if err := checkUnit(s.To); err != nil {
		return err
	}
	if s.From == s.To {
		return fmt.Errorf("web server cũ và mới cùng là %q", s.From)
	}
	return nil
}

func (s *WebSwap) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "web.swap:" + s.From + ":" + s.To,
		Title: i18n.T("Đổi web server: dừng "+s.From+", chạy "+s.To, "Swap web server: stop "+s.From+", start "+s.To),
		Why: i18n.T(
			"Cổng 80 và 443 chỉ một chương trình giữ được. Web server cũ phải nhả cổng thì web server mới mới nghe được. Trong khoảng giữa hai lệnh, mọi site không trả lời. Web server mới không chạy được thì web server cũ được bật lại ngay.",
			"Only one program can hold ports 80 and 443. The old web server must let go of them before the new one can listen. Between the two commands every site is down. If the new web server fails to start, the old one is started again right away.",
		),
		Commands: []string{systemctl("stop", s.From).String(), systemctl("start", s.To).String()},
		Danger:   true,
	}
}

// Check: cũ đã dừng VÀ mới đang chạy. Lần trước hỏng giữa chừng (cũ đã dừng,
// mới chưa chạy) thì Check false và Apply chạy lại cho hội tụ; đã đổi xong thì
// không dừng rồi chạy lại vô ích.
func (s *WebSwap) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fromActive, err := unitActive(ctx, sys, s.From)
	if err != nil || fromActive {
		return false, err
	}
	return unitActive(ctx, sys, s.To)
}

func (s *WebSwap) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	fromActive, err := unitActive(ctx, sys, s.From)
	if err != nil {
		return err
	}
	if res, err := sys.Run(ctx, systemctl("stop", s.From)); err != nil {
		err = withStdout(fmt.Errorf("dừng %s: %w", s.From, err), res)
		// Stop lỗi có thể để dịch vụ nửa chừng: bật lại cho site chạy tiếp.
		if fromActive {
			return errors.Join(err, s.start(ctx, sys, s.From))
		}
		return err
	}
	if res, err := sys.Run(ctx, systemctl("start", s.To)); err != nil {
		// Web server mới không chạy: bật lại cái cũ để site sống lại ngay, không
		// chờ engine hoàn tác (engine không Undo bước có Apply lỗi).
		err = withStdout(fmt.Errorf("khởi động %s: %w", s.To, err), res)
		return errors.Join(err, s.start(ctx, sys, s.From))
	}
	s.applied = true
	return nil
}

func (s *WebSwap) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("đổi web server: %w", errNotApplied)
	}
	if res, err := sys.Run(ctx, systemctl("stop", s.To)); err != nil {
		return withStdout(fmt.Errorf("dừng %s: %w", s.To, err), res)
	}
	return s.start(ctx, sys, s.From)
}

// start chạy `systemctl start unit`, trả nil hoặc lỗi có ngữ cảnh.
func (s *WebSwap) start(ctx context.Context, sys system.System, unit string) error {
	if res, err := sys.Run(ctx, systemctl("start", unit)); err != nil {
		return withStdout(fmt.Errorf("bật lại %s: %w", unit, err), res)
	}
	return nil
}

// ============================================================== ServiceDisable

// ServiceDisable tắt "bật khi khởi động" của một unit, không dừng nó (mục 6.6
// bước 5). Xem hợp đồng ở m10_types.go.
type ServiceDisable struct {
	Unit string

	wasEnabled, applied bool
}

func (s *ServiceDisable) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "service.disable:" + s.Unit,
		Title: i18n.T("Không bật "+s.Unit+" khi khởi động", "Do not start "+s.Unit+" at boot"),
		Why: i18n.T(
			"Web server cũ đã dừng nhưng vẫn được bật khi khởi động. Không tắt thì lần khởi động máy sau, nó chạy cùng web server mới và hai bên tranh cổng 80: một trong hai không lên, và site có thể chết.",
			"The old web server is stopped but still enabled at boot. If it stays enabled, the next reboot starts it alongside the new web server and the two fight for port 80: one of them fails, and sites can go down.",
		),
		Commands: []string{systemctl("disable", s.Unit).String()},
	}
}

func (s *ServiceDisable) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := checkUnit(s.Unit); err != nil {
		return false, err
	}
	enabled, err := unitEnabled(ctx, sys, s.Unit)
	return !enabled, err
}

func (s *ServiceDisable) Apply(ctx context.Context, sys system.System) error {
	if err := checkUnit(s.Unit); err != nil {
		return err
	}
	enabled, err := unitEnabled(ctx, sys, s.Unit)
	if err != nil {
		return err
	}
	if res, err := sys.Run(ctx, systemctl("disable", s.Unit)); err != nil {
		return withStdout(fmt.Errorf("tắt bật-khi-khởi-động của %s: %w", s.Unit, err), res)
	}
	s.wasEnabled, s.applied = enabled, true
	return nil
}

func (s *ServiceDisable) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("tắt bật-khi-khởi-động %s: %w", s.Unit, errNotApplied)
	}
	if !s.wasEnabled {
		return nil
	}
	if res, err := sys.Run(ctx, systemctl("enable", s.Unit)); err != nil {
		return withStdout(fmt.Errorf("bật lại %s khi khởi động: %w", s.Unit, err), res)
	}
	return nil
}
