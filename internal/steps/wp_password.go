package steps

// Đặt lại mật khẩu tài khoản quản trị WordPress của một site (người dùng quên
// mật khẩu, hoặc nhận lại site sau khi khôi phục). Chọn tài khoản quản trị có
// ID nhỏ nhất (tài khoản tạo lúc cài), vì tên đăng nhập có thể không còn là
// "admin" sau một lần khôi phục backup. Mật khẩu đi qua stdin (--prompt), không
// nằm trong dòng lệnh.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// wpLoginRe: tên đăng nhập WordPress (sanitize_user, chế độ thường). Chặn tên
// bắt đầu bằng '-' để không bao giờ bị hiểu là cờ của WP-CLI.
var wpLoginRe = regexp.MustCompile(`^[A-Za-z0-9_.@][A-Za-z0-9 _.@-]{0,59}$`)

// WPAdminPassword đặt mật khẩu mới cho tài khoản quản trị đầu tiên của site.
type WPAdminPassword struct {
	Layout   site.Layout
	Password system.Secret

	// Login là tên đăng nhập đã được đặt lại; có sau Apply.
	Login string
}

func (s *WPAdminPassword) listCmd() system.Cmd {
	return wpRun(s.Layout, nil, "user", "list", "--role=administrator", "--orderby=ID", "--order=ASC", "--field=user_login", "--skip-plugins", "--skip-themes")
}

func (s *WPAdminPassword) Info() engine.StepInfo {
	list := s.listCmd()
	return engine.StepInfo{
		ID:    "wp.admin-password:" + s.Layout.Domain.String(),
		Title: i18n.T("Đặt mật khẩu mới cho tài khoản quản trị WordPress của "+s.Layout.Domain.String(), "Set a new password for the WordPress admin account of "+s.Layout.Domain.String()),
		Why: i18n.T(
			"Mật khẩu WordPress chỉ được lưu dạng băm nên không xem lại được; quên thì phải đặt cái mới. TPAN sinh mật khẩu ngẫu nhiên và hiện một lần sau khi xong. Các tài khoản khác của site không đổi.",
			"WordPress only stores a hash of the password, so it cannot be shown again; a forgotten one has to be replaced. TPAN generates a random password and shows it once when done. Other accounts of the site do not change."),
		Commands: append(cmdStrings(&list), "wp user update <tài khoản quản trị> --prompt=user_pass --skip-email --quiet --path="+s.Layout.Public),
		NoUndo:   i18n.T("Mật khẩu cũ chỉ còn dạng băm, không đặt lại được.", "The old password only exists as a hash and cannot be put back."),
	}
}

func (s *WPAdminPassword) Check(context.Context, system.System) (bool, error) {
	return s.Login != "", nil
}

func (s *WPAdminPassword) Apply(ctx context.Context, sys system.System) error {
	if err := wpCheckLayout(s.Layout); err != nil {
		return err
	}
	pw := s.Password.Reveal()
	if pw == "" || len(pw) > wpSecretMax || strings.ContainsAny(pw, "\r\n") {
		return errors.New("mật khẩu quản trị mới không hợp lệ")
	}
	res, err := sys.Run(ctx, s.listCmd())
	if err != nil {
		return withStdout(fmt.Errorf("đọc danh sách tài khoản quản trị WordPress của %s: %w", s.Layout.Domain, err), res)
	}
	login := ""
	for _, l := range strings.Split(res.Stdout, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			login = l
			break
		}
	}
	if login == "" {
		return fmt.Errorf("WordPress của %s không có tài khoản quản trị nào", s.Layout.Domain)
	}
	if !wpLoginRe.MatchString(login) {
		return fmt.Errorf("tên tài khoản quản trị của %s có ký tự lạ; đặt lại bằng tay: sudo -u %s wp user update <tên> --prompt=user_pass --path=%s", s.Layout.Domain, s.Layout.User, s.Layout.Public)
	}
	// --quiet: WP-CLI in lại tham số đã hỏi qua --prompt nếu không có cờ này.
	up := wpRun(s.Layout, []byte(pw+"\n"), "user", "update", login, "--prompt=user_pass", "--skip-email", "--quiet", "--skip-plugins", "--skip-themes")
	if res, err := sys.Run(ctx, up); err != nil {
		return withStdout(fmt.Errorf("đặt mật khẩu mới cho tài khoản %s của %s: %w", login, s.Layout.Domain, err), res)
	}
	s.Login = login
	return nil
}

func (s *WPAdminPassword) Undo(context.Context, system.System) error { return nil }
