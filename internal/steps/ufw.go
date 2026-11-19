package steps

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Các bước tường lửa ufw của trang bảo mật (mục 6.8 của docs/KE-HOACH.md).
// Hợp đồng: ssl_types.go. Core luôn đặt UFWAllow (có cổng SSH thật) trước
// UFWEnable, để lúc bật tường lửa thì đường vào máy đã mở sẵn.

var (
	_ engine.Step = (*UFWAllow)(nil)
	_ engine.Step = (*UFWEnable)(nil)
)

// ufwRuleRe: luật chỉ có dạng <cổng>/tcp. Chữ số đầu khác 0: ufw tự chuẩn hoá
// "022/tcp" thành "22/tcp", khi đó Check so chuỗi không bao giờ khớp.
var ufwRuleRe = regexp.MustCompile(`^[1-9][0-9]{0,4}/tcp$`)

// ufwRun là một lệnh ufw. Mọi lệnh ufw chạy bằng root.
func ufwRun(args ...string) system.Cmd { return system.Cmd{Name: "ufw", Args: args} }

// ================================================================== UFWAllow

// UFWAllow đảm bảo ufw có các luật cho phép, ví dụ "22/tcp", "443/tcp".
type UFWAllow struct {
	Rules []string

	added   []string // luật Apply đã thêm, theo thứ tự thêm
	applied bool
}

func (s *UFWAllow) validate() error {
	if len(s.Rules) == 0 {
		return errors.New("danh sách luật ufw đang trống")
	}
	for i, r := range s.Rules {
		if !ufwRuleRe.MatchString(r) {
			return fmt.Errorf("luật ufw %q không đúng dạng <cổng>/tcp", r)
		}
		port, _ := strconv.Atoi(strings.TrimSuffix(r, "/tcp"))
		if port < 1 || port > 65535 {
			return fmt.Errorf("luật ufw %q: cổng phải từ 1 tới 65535", r)
		}
		// Trùng thì Undo chạy `ufw delete` hai lần cho một luật, lần hai lỗi.
		if slices.Contains(s.Rules[:i], r) {
			return fmt.Errorf("luật ufw %q bị lặp", r)
		}
	}
	return nil
}

func (s *UFWAllow) Info() engine.StepInfo {
	var cmds []string
	for _, r := range s.Rules {
		cmds = append(cmds, ufwRun("allow", r).String())
	}
	return engine.StepInfo{
		ID:    "ufw.allow:" + strings.Join(s.Rules, ","),
		Title: i18n.T("Mở cổng trên tường lửa: "+strings.Join(s.Rules, " "), "Open firewall ports: "+strings.Join(s.Rules, " ")),
		Why: i18n.T(
			"Bật tường lửa là chặn mọi cổng chưa được mở. Thiếu luật cho cổng SSH thì mất đường vào máy; thiếu 80, 443 thì site không ai vào được; thiếu cổng panel thì không mở được tpan.",
			"Enabling the firewall blocks every port not opened first. Without a rule for the SSH port you lose access to the machine; without 80 and 443 nobody reaches the sites; without the panel port tpan cannot be opened.",
		),
		Commands: cmds,
	}
}

// missing trả các luật chưa có. `ufw show added` liệt kê luật người dùng đã
// thêm, đọc được cả khi ufw đang tắt (khác `ufw status`), mỗi luật một dòng
// "ufw allow 22/tcp", có thể kèm " comment '...'" nếu người quản trị đặt chú
// thích; so ba từ đầu để luật có chú thích vẫn được tính là đã có. Dòng tiêu
// đề và "(None)" không bao giờ khớp.
func (s *UFWAllow) missing(ctx context.Context, sys system.System) ([]string, error) {
	res, err := sys.Run(ctx, ufwRun("show", "added"))
	if err != nil {
		return nil, withStdout(fmt.Errorf("đọc luật ufw đã thêm: %w", err), res)
	}
	have := map[string]bool{}
	for line := range strings.SplitSeq(res.Stdout, "\n") {
		if f := strings.Fields(line); len(f) >= 3 && f[0] == "ufw" && f[1] == "allow" {
			have[f[2]] = true
		}
	}
	var out []string
	for _, r := range s.Rules {
		if !have[r] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *UFWAllow) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	m, err := s.missing(ctx, sys)
	return len(m) == 0, err
}

func (s *UFWAllow) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	todo, err := s.missing(ctx, sys)
	if err != nil {
		return err
	}
	var added []string
	for _, r := range todo {
		res, err := sys.Run(ctx, ufwRun("allow", r))
		if err != nil {
			err = withStdout(fmt.Errorf("mở %s trên ufw: %w", r, err), res)
			// Engine không Undo bước có Apply lỗi: tự gỡ các luật vừa thêm.
			return errors.Join(err, ufwDelete(ctx, sys, added))
		}
		// Chỉ nhớ luật ufw báo "Rule added". Luật đã có ("Skipping adding
		// existing rule", "Rule updated") không phải do bước này thêm: Undo mà
		// gỡ nó thì có thể gỡ mất luật SSH của người quản trị và khoá máy.
		if strings.Contains(res.Stdout, "Rule added") {
			added = append(added, r)
		}
	}
	s.added, s.applied = added, true
	return nil
}

// ufwDelete gỡ các luật, từ luật thêm sau cùng trở về.
func ufwDelete(ctx context.Context, sys system.System, rules []string) error {
	for _, r := range slices.Backward(rules) {
		if res, err := sys.Run(ctx, ufwRun("delete", "allow", r)); err != nil {
			return withStdout(fmt.Errorf("gỡ luật %s khỏi ufw: %w", r, err), res)
		}
	}
	return nil
}

// Undo gỡ đúng những luật Apply đã thêm; luật có sẵn từ trước giữ nguyên.
func (s *UFWAllow) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("luật ufw: %w", errNotApplied)
	}
	return ufwDelete(ctx, sys, s.added)
}

// ================================================================= UFWEnable

// UFWEnable bật tường lửa bằng `ufw --force enable` (--force: không hỏi y/n,
// tpan không có ai ngồi gõ). Danger: bật mà thiếu luật SSH là mất đường vào máy.
type UFWEnable struct {
	applied bool
}

func (s *UFWEnable) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "ufw.enable",
		Title: i18n.T("Bật tường lửa ufw", "Enable the ufw firewall"),
		Why: i18n.T(
			"Tường lửa tắt thì mọi dịch vụ đang nghe trên máy (MariaDB, cổng thử của ứng dụng khác) đều mở ra internet. Bật lên thì chỉ các cổng đã mở ở bước trước còn vào được. tpan mở cổng SSH trước khi bật để không mất đường vào máy.",
			"With the firewall off every listening service (MariaDB, other apps' test ports) is open to the internet. Once enabled only the ports opened in the previous step are reachable. tpan opens the SSH port before enabling it so you keep access to the machine.",
		),
		Commands: []string{ufwRun("--force", "enable").String()},
		Danger:   true,
	}
}

func (s *UFWEnable) Check(ctx context.Context, sys system.System) (bool, error) {
	res, err := sys.Run(ctx, ufwRun("status"))
	if err != nil {
		return false, withStdout(fmt.Errorf("đọc trạng thái ufw: %w", err), res)
	}
	first, _, _ := strings.Cut(res.Stdout, "\n")
	return strings.TrimSpace(first) == "Status: active", nil
}

func (s *UFWEnable) Apply(ctx context.Context, sys system.System) error {
	if res, err := sys.Run(ctx, ufwRun("--force", "enable")); err != nil {
		return withStdout(fmt.Errorf("bật ufw: %w", err), res)
	}
	s.applied = true
	return nil
}

// Undo tắt lại tường lửa. Engine chỉ gọi Undo khi Apply đã chạy, tức là trước
// đó ufw đang tắt.
func (s *UFWEnable) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("bật ufw: %w", errNotApplied)
	}
	if res, err := sys.Run(ctx, ufwRun("disable")); err != nil {
		return withStdout(fmt.Errorf("tắt lại ufw: %w", err), res)
	}
	return nil
}
