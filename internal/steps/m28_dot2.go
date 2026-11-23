package steps

// M28 đợt 2 (H5, M1-DBCONN): tắt một module Apache bằng reload, và trần số kết
// nối cùng lúc cho một user DB đã có. Hợp đồng tên ở
// agent-dev/M28-dot-2-ban-nhap/tra-loi-soat.md ("Hợp đồng tên").

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*ApacheModOff)(nil)
	_ engine.Step = (*DBUserLimit)(nil)
)

// ApacheModulesOff là các module Apache mà stack của tpan luôn tắt, dùng chung
// cho cài stack và chuyển sang Apache (webSteps của core) để hai nơi không lệch:
//
//   - mpm_prefork: xung đột với mpm_event (mục 6.1);
//   - status (H5): /server-status là `Require local`, nên PHP của bất kỳ site
//     nào gọi 127.0.0.1/server-status là thấy URL (kèm token, khoá API trong
//     tham số) của khách mọi site. tpan không dùng trang này cho việc gì.
//
// Là biến vì Go không có hằng kiểu slice; người dùng phải slices.Clone trước
// khi sửa.
var ApacheModulesOff = []string{"mpm_prefork", "status"}

var apacheReload = system.Cmd{Name: "systemctl", Args: []string{"reload", "apache2"}}

// apacheLoaded là `apachectl -M`: danh sách module Apache ĐÃ NẠP theo cấu hình
// trên đĩa (mỗi dòng " status_module (shared)"). Khác `a2query -m`, lệnh này
// thấy cả module nạp bằng một dòng LoadModule đặt ngoài mods-enabled.
var apacheLoaded = system.Cmd{Name: "apachectl", Args: []string{"-M"}}

// ================================================================ ApacheModOff

// ApacheModOff tắt một module Apache rồi RELOAD (không restart: không cắt kết
// nối và lần tải lên đang dở của site nào). Dùng trong `security harden` trên
// Apache để tắt status (H5, S15): harden chỉ tắt, không áp lại phần bật của
// danh sách stack, vì admin có thể đã cố ý đổi các module đó.
//
// Check: `apachectl -M` không có `<Module>_module`. Apply: `a2dismod -q`,
// `apachectl configtest`, `systemctl reload apache2`; hỏng ở đâu thì bật lại
// module (và reload lại nếu đã reload) trước khi trả lỗi. Undo: bật lại nếu
// chính Apply đã tắt, kiểm cú pháp, reload.
type ApacheModOff struct {
	Module string

	applied  bool
	disabled bool // chính Apply đã tắt (Undo chỉ bật lại khi đó)
}

func (s *ApacheModOff) validate() error {
	// 64: tên module thật ngắn hơn nhiều; chặn chuỗi dài lọt vào argv và log.
	if !modName.MatchString(s.Module) || len(s.Module) > 64 {
		return fmt.Errorf("tên module Apache %q có ký tự không cho phép", s.Module)
	}
	return nil
}

func (s *ApacheModOff) Info() engine.StepInfo {
	m := s.Module
	why := i18n.T(
		"Module "+m+" không cần cho site nào của tpan; để bật là mở thêm một chỗ có thể bị lợi dụng.",
		"Module "+m+" is not needed by any tpan site; leaving it on only adds something that can be abused.")
	if m == "status" {
		why = i18n.T(
			"Module status mở trang /server-status cho mọi request từ chính máy này. PHP của bất kỳ site nào gọi http://127.0.0.1/server-status là thấy đường dẫn đầy đủ (kèm mã đặt lại mật khẩu, khoá API trong tham số) của khách ở mọi site khác. tpan không dùng trang này. Tắt rồi nạp lại Apache, không khởi động lại: không cắt kết nối nào.",
			"The status module serves /server-status to any request from this machine. PHP on any site can fetch http://127.0.0.1/server-status and see the full URLs (password reset tokens, API keys in query strings) of visitors on every other site. tpan does not use this page. It is disabled with a reload, not a restart, so no connection is cut.")
	}
	return engine.StepInfo{
		ID:       "apache.mod-off:" + m,
		Title:    i18n.T("Tắt module Apache "+m, "Disable the Apache module "+m),
		Why:      why,
		Commands: []string{apacheLoaded.String(), a2dismod([]string{m}).String(), apacheConfigtest.String(), apacheReload.String()},
	}
}

// loaded hỏi `apachectl -M` module có đang nạp không. So đúng cột đầu với
// "<module>_module": "status" không được khớp "proxy_status_module".
func (s *ApacheModOff) loaded(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	res, err := sys.Run(ctx, apacheLoaded)
	if err != nil {
		return false, withStdout(fmt.Errorf("hỏi apachectl các module đang nạp: %w", err), res)
	}
	want := s.Module + "_module"
	for _, ln := range strings.Split(res.Stdout, "\n") {
		if f := strings.Fields(ln); len(f) > 0 && f[0] == want {
			return true, nil
		}
	}
	return false, nil
}

func (s *ApacheModOff) Check(ctx context.Context, sys system.System) (bool, error) {
	on, err := s.loaded(ctx, sys)
	return !on, err
}

func (s *ApacheModOff) Apply(ctx context.Context, sys system.System) error {
	on, err := s.loaded(ctx, sys)
	if err != nil {
		return err
	}
	s.applied, s.disabled = true, false
	if !on {
		return nil
	}
	m := []string{s.Module}
	if res, err := sys.Run(ctx, a2dismod(m)); err != nil {
		// a2dismod có thể đã gỡ link trước khi lỗi: bật lại cho chắc.
		return errors.Join(withStdout(fmt.Errorf("tắt module %s: %w", s.Module, err), res), s.enable(ctx, sys, false))
	}
	// configtest lỗi: Apache vẫn chạy bằng cấu hình cũ trong bộ nhớ, chỉ cần
	// bật lại module trên đĩa.
	if res, err := sys.Run(ctx, apacheConfigtest); err != nil {
		return errors.Join(withStdout(fmt.Errorf("kiểm cấu hình Apache sau khi tắt module %s: %w", s.Module, err), res), s.enable(ctx, sys, false))
	}
	if err := runReload(ctx, sys, &apacheReload, "tắt module "+s.Module); err != nil {
		return errors.Join(err, s.enable(ctx, sys, true))
	}
	s.disabled = true
	return nil
}

// enable bật lại module; reload: Apache đã (hoặc có thể đã) nạp cấu hình mới,
// nên kiểm cú pháp rồi nạp lại để nó chạy lại bằng cấu hình cũ.
func (s *ApacheModOff) enable(ctx context.Context, sys system.System, reload bool) error {
	if res, err := sys.Run(ctx, a2enmod([]string{s.Module})); err != nil {
		return withStdout(fmt.Errorf("bật lại module %s: %w", s.Module, err), res)
	}
	if !reload {
		return nil
	}
	if err := runTest(ctx, sys, &apacheConfigtest, "bật lại module "+s.Module); err != nil {
		return err
	}
	return runReload(ctx, sys, &apacheReload, "bật lại module "+s.Module)
}

func (s *ApacheModOff) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("tắt module Apache %s: %w", s.Module, errNotApplied)
	}
	if !s.disabled {
		return nil
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.enable(ctx, sys, true); err != nil {
		return err
	}
	s.disabled = false
	return nil
}

// ================================================================= DBUserLimit

// DBUserLimit đặt trần số kết nối cùng lúc (MAX_USER_CONNECTIONS) cho user DB
// 'User'@'localhost' đã có (M1-DBCONN). Không có trần thì PHP của một site mở
// hết max_connections của MariaDB và mọi site khác trả 500.
//
// Theo S16 của bản trả lời soát thiết kế:
//   - Check đạt khi trần khác 0: admin đã cố ý nâng trần cho một site lớn thì
//     giữ. User không có cũng đạt (không có gì để đặt); core nêu tên user đó
//     trong bản xem trước, không để một user lệch làm hỏng cả Plan (S2).
//   - Số cũ đọc ở Apply (Check còn chạy lúc xem trước), Undo trả lại số đó.
//
// Trần đọc từ cột max_user_connections của view mysql.user (MariaDB 10.4+,
// "Zero is unlimited"), KHÔNG từ SHOW CREATE USER: câu đó in cả hash mật khẩu,
// mà engine ghi mọi dòng output vào log của job.
// https://mariadb.com/kb/en/mysql-user-table/
// https://mariadb.com/kb/en/alter-user/ (WITH MAX_USER_CONNECTIONS)
type DBUserLimit struct {
	User string
	Max  int

	applied bool
	changed bool // chính Apply đã đổi trần
	prev    int
}

func (s *DBUserLimit) validate() error {
	if !dbNameRe.MatchString(s.User) {
		return fmt.Errorf("tên user DB %q có ký tự không cho phép", s.User)
	}
	if s.Max <= 0 {
		return fmt.Errorf("trần kết nối của user DB %s phải lớn hơn 0 (có %d)", s.User, s.Max)
	}
	return nil
}

// dbLimitStateSQL trả đúng một dòng, hai số: user có không (0/1) và trần hiện
// tại (0 khi không có user). COUNT và COALESCE(MAX) để luôn có một dòng.
func dbLimitStateSQL(name string) string {
	return "SELECT COUNT(*), COALESCE(MAX(max_user_connections), 0) FROM mysql.user WHERE User='" + name + "' AND Host='localhost';\n"
}

func dbLimitSQL(name string, max int) string {
	return "ALTER USER '" + name + "'@'localhost' WITH MAX_USER_CONNECTIONS " + strconv.Itoa(max) + ";\n"
}

// DBUserLimitState đọc user 'user'@'localhost' có không và trần kết nối của
// nó (0 là không trần). Core gọi lúc dựng Plan để nêu user không có.
func DBUserLimitState(ctx context.Context, sys system.System, user string) (exists bool, limit int, err error) {
	if !dbNameRe.MatchString(user) {
		return false, 0, fmt.Errorf("tên user DB %q có ký tự không cho phép", user)
	}
	res, err := sys.Run(ctx, mariadbCmd(dbLimitStateSQL(user)))
	if err != nil {
		return false, 0, withStdout(fmt.Errorf("đọc trần kết nối của user DB %s: %w", user, err), res)
	}
	f := strings.Fields(res.Stdout)
	if len(f) != 2 {
		return false, 0, fmt.Errorf("đọc trần kết nối của user DB %s: kết quả lạ %q (cần đúng hai số)", user, res.Stdout)
	}
	n, err := strconv.Atoi(f[1])
	if err != nil {
		return false, 0, fmt.Errorf("đọc trần kết nối của user DB %s: %q không phải số", user, f[1])
	}
	return f[0] != "0", n, nil
}

func (s *DBUserLimit) Info() engine.StepInfo {
	n := strconv.Itoa(s.Max)
	return engine.StepInfo{
		ID: "mariadb.user-limit:" + s.User,
		Title: i18n.T(
			"Đặt trần "+n+" kết nối cùng lúc cho user DB "+s.User,
			"Limit the database user "+s.User+" to "+n+" simultaneous connections"),
		Why: i18n.T(
			"Không có trần thì mã PHP của một site (plugin lỗi, hay site bị chiếm) mở hết số kết nối MariaDB cho phép, và mọi site khác trên máy báo lỗi kết nối DB. "+n+" là gấp đôi số kết nối một site WordPress cần lúc đông nhất. Trần admin đã đặt khác 0 thì giữ nguyên.",
			"Without a limit, one site's PHP code (a broken plugin, or a compromised site) can take every connection MariaDB allows, and every other site on the machine fails to reach its database. "+n+" is twice what a WordPress site needs at its busiest. A non-zero limit an admin already set is left alone."),
		Commands: []string{dbShow(dbLimitStateSQL(s.User)), dbShow(dbLimitSQL(s.User, s.Max))},
	}
}

func (s *DBUserLimit) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	exists, limit, err := DBUserLimitState(ctx, sys, s.User)
	return !exists || limit != 0, err
}

func (s *DBUserLimit) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	exists, limit, err := DBUserLimitState(ctx, sys, s.User)
	if err != nil {
		return err
	}
	s.applied, s.changed, s.prev = true, false, limit
	if !exists || limit != 0 {
		return nil
	}
	if err := dbRun(ctx, sys, dbLimitSQL(s.User, s.Max), "", "đặt trần kết nối cho user DB "+s.User); err != nil {
		return err
	}
	s.changed = true
	return nil
}

func (s *DBUserLimit) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("trần kết nối của user DB %s: %w", s.User, errNotApplied)
	}
	if !s.changed {
		return nil
	}
	if !dbNameRe.MatchString(s.User) {
		return fmt.Errorf("tên user DB %q có ký tự không cho phép", s.User)
	}
	if err := dbRun(ctx, sys, dbLimitSQL(s.User, s.prev), "", "trả lại trần kết nối cũ của user DB "+s.User); err != nil {
		return err
	}
	s.changed = false
	return nil
}
