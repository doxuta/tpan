package steps

// Hợp đồng M4: các Step cho sửa tự động của health (mục 6.7 của
// docs/KE-HOACH.md). Theo quy ước của gói (common.go) và skill tpan-step.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*PHPFPMRestart)(nil)
	_ engine.Step = (*DBUserUnlock)(nil)
	_ engine.Step = (*WPConfigPerm)(nil)
)

// ============================================================== PHPFPMRestart

// PHPFPMRestart khởi động lại php<PHPVersion>-fpm để socket của các pool được
// tạo lại (socket bị xoá thì reload không tạo lại, vì PHP-FPM giữ socket cũ
// qua lần reload; đã kiểm trên máy ảo M4: sau reload socket vẫn mất, sau
// restart có lại). Check: Socket tồn tại và là socket.
// Apply: systemctl restart php<v>-fpm. Undo: không làm gì (NoUndo có lý do).
// Danger: true (mọi site dùng bản PHP này gián đoạn vài giây).
type PHPFPMRestart struct {
	PHPVersion string
	// Socket là socket phải có sau khi khởi động lại (site.Layout.SocketFor của bản PHP).
	Socket string
}

// phpSocketDir: mọi socket pool của tpan nằm ở đây (site.LayoutOf). Chỉ nhận
// socket trong thư mục này để Step không bị dùng để "kiểm" một đường dẫn lạ.
const phpSocketDir = "/run/php/"

func (s *PHPFPMRestart) validate() error {
	if !phpVersion.MatchString(s.PHPVersion) {
		return fmt.Errorf("bản PHP %q không hợp lệ (dạng 8.3)", s.PHPVersion)
	}
	if err := checkPath("socket của pool", s.Socket); err != nil {
		return err
	}
	if path.Dir(s.Socket)+"/" != phpSocketDir || !strings.HasSuffix(s.Socket, ".sock") {
		return fmt.Errorf("socket %q không nằm ngay trong %s hoặc không có đuôi .sock", s.Socket, phpSocketDir)
	}
	return nil
}

func (s *PHPFPMRestart) restartCmd() system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{"restart", "php" + s.PHPVersion + "-fpm"}}
}

func (s *PHPFPMRestart) Info() engine.StepInfo {
	unit := "php" + s.PHPVersion + "-fpm"
	return engine.StepInfo{
		ID:    "php.restart:" + s.PHPVersion,
		Title: i18n.T("Khởi động lại "+unit+" để tạo lại "+s.Socket, "Restart "+unit+" to recreate "+s.Socket),
		Why: i18n.T(
			"Socket là cửa web server chuyển request .php sang PHP-FPM. Socket mất thì site trả 502. Nạp lại (reload) không tạo lại socket đã bị xoá, nên phải khởi động lại hẳn PHP-FPM.",
			"The socket is how the web server hands .php requests to PHP-FPM. Without it the site returns 502. A reload does not recreate a deleted socket, so PHP-FPM has to be fully restarted.",
		),
		Commands: []string{s.restartCmd().String()},
		// Mọi site dùng bản PHP này mất PHP trong vài giây.
		Danger: true,
		NoUndo: i18n.T(
			"Khởi động lại không đổi cấu hình nào, không có trạng thái cũ để trả về (dừng PHP-FPM lại chỉ làm site chết).",
			"A restart changes no configuration, so there is no earlier state to return to (stopping PHP-FPM again would only take the sites down).",
		),
	}
}

func (s *PHPFPMRestart) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, err := sys.Stat(s.Socket)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm socket %s: %w", s.Socket, err)
	}
	return fi.Mode&fs.ModeSocket != 0, nil
}

func (s *PHPFPMRestart) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, s.restartCmd()); err != nil {
		return withStdout(fmt.Errorf("khởi động lại php%s-fpm: %w", s.PHPVersion, err), res)
	}
	return nil
}

func (s *PHPFPMRestart) Undo(context.Context, system.System) error { return nil }

// =============================================================== DBUserUnlock

// DBUserUnlock mở khoá user DB của site (ALTER USER '<db>'@'localhost' ACCOUNT
// UNLOCK, SQL qua stdin của mariadb như site_db.go). Check: user có và không
// bị khoá. Undo: khoá lại nếu Apply đã mở.
//
// Trạng thái khoá đọc từ cột JSON Priv của mysql.global_priv (khoá
// "account_locked", MariaDB 10.4+), KHÔNG từ SHOW CREATE USER: câu đó in cả
// hash mật khẩu (IDENTIFIED BY PASSWORD '*...'), mà engine ghi mọi dòng output
// vào log của job. Câu dưới chỉ trả hai số.
// https://mariadb.com/kb/en/mysql-global_priv-table/
// https://mariadb.com/kb/en/account-locking/
type DBUserUnlock struct {
	Layout site.Layout

	applied  bool
	unlocked bool // chính Apply đã mở khoá (Undo chỉ khoá lại khi đó)
}

// dbLockStateSQL trả một dòng, hai số: user có không (0/1), bị khoá không (0/1).
// MAX và COALESCE để luôn có đúng một dòng kể cả khi user không có.
// JSON_VALUE của giá trị JSON true trả chuỗi "1" trên MariaDB 10.11 (đã kiểm
// trên máy ảo M4: sau ACCOUNT LOCK là "1", sau UNLOCK là "0", JSON_EXTRACT là
// true/false), không phải "true": so với 'true' thì user bị khoá vẫn ra 0 và
// Step bỏ qua bước mở khoá. Nhận cả hai cách viết.
func dbLockStateSQL(name string) string {
	return "SELECT COUNT(*), COALESCE(MAX(JSON_VALUE(Priv, '$.account_locked') IN ('1', 'true')), 0) " +
		"FROM mysql.global_priv WHERE User='" + name + "' AND Host='localhost';\n"
}

func dbUnlockSQL(name string) string {
	return "ALTER USER '" + name + "'@'localhost' ACCOUNT UNLOCK;\n"
}

func dbLockSQL(name string) string {
	return "ALTER USER '" + name + "'@'localhost' ACCOUNT LOCK;\n"
}

// lockState đọc user có không và có bị khoá không.
func (s *DBUserUnlock) lockState(ctx context.Context, sys system.System) (exists, locked bool, err error) {
	if err := dbCheckLayout(s.Layout); err != nil {
		return false, false, err
	}
	l := s.Layout
	res, err := sys.Run(ctx, mariadbCmd(dbLockStateSQL(l.DB)))
	if err != nil {
		return false, false, withStdout(fmt.Errorf("đọc trạng thái khoá của user DB %s: %w", l.DB, err), res)
	}
	f := strings.Fields(res.Stdout)
	if len(f) != 2 {
		return false, false, fmt.Errorf("đọc trạng thái khoá của user DB %s: kết quả lạ %q (cần đúng hai số)", l.DB, res.Stdout)
	}
	return f[0] != "0", f[1] != "0", nil
}

func (s *DBUserUnlock) Info() engine.StepInfo {
	l := s.Layout
	return engine.StepInfo{
		ID: "db.unlock:" + l.Domain.String(),
		Title: i18n.T(
			"Mở khoá user DB "+l.DB+" của site "+l.Domain.String(),
			"Unlock the database user "+l.DB+" of "+l.Domain.String(),
		),
		Why: i18n.T(
			"User DB bị khoá thì WordPress không đăng nhập được vào DB và site hiện \"Error establishing a database connection\". Mở khoá không đổi mật khẩu hay quyền nào.",
			"While the database user is locked WordPress cannot log in to its database and the site shows \"Error establishing a database connection\". Unlocking changes no password or privilege.",
		),
		Commands: []string{dbShow(dbLockStateSQL(l.DB)), dbShow(dbUnlockSQL(l.DB))},
	}
}

func (s *DBUserUnlock) Check(ctx context.Context, sys system.System) (bool, error) {
	exists, locked, err := s.lockState(ctx, sys)
	return exists && !locked, err
}

func (s *DBUserUnlock) Apply(ctx context.Context, sys system.System) error {
	exists, locked, err := s.lockState(ctx, sys)
	if err != nil {
		return err
	}
	name := s.Layout.DB
	if !exists {
		// Tạo lại user cần mật khẩu trong wp-config.php; tpan không đọc nó ra.
		return fmt.Errorf("user DB %s không có, không mở khoá được; tạo lại bằng tay với mật khẩu trong %s", name, s.Layout.WPConfig)
	}
	if locked {
		if err := dbRun(ctx, sys, dbUnlockSQL(name), "", "mở khoá user DB "+name); err != nil {
			return err
		}
	}
	// Undo không khoá một user vốn đã mở.
	s.applied, s.unlocked = true, locked
	return nil
}

func (s *DBUserUnlock) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("mở khoá user DB %s: %w", s.Layout.DB, errNotApplied)
	}
	if !s.unlocked {
		return nil
	}
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := dbRun(ctx, sys, dbLockSQL(s.Layout.DB), "", "khoá lại user DB "+s.Layout.DB); err != nil {
		return err
	}
	s.unlocked = false
	return nil
}

// =============================================================== WPConfigPerm

// WPConfigPerm đưa wp-config.php về đúng điều checks.WPConfigPerm đòi: file
// thường, quyền 0600, chủ là user của site. Check so đúng ba điều đó (không so
// nhóm), để job sửa thành công thì phép kiểm cũng đạt và ngược lại.
//
// Không dùng OwnedFile: nó tạo file rỗng khi file mất, mà wp-config.php rỗng
// thì WordPress hiện trang cài đặt và ai vào trước chiếm site. Ở đây file mất,
// hoặc ở chỗ đó là symlink hay thư mục, thì Apply từ chối.
//
// Chống symlink: chủ sai thì root đổi bằng sys.Chown (Lchown, không đi theo
// link), rồi chmod chạy DƯỚI user của site (wpChmod): lỡ file bị tráo thành
// symlink giữa chừng thì user đó không chmod được file của người khác.
type WPConfigPerm struct {
	Layout site.Layout

	applied   bool
	prevPerm  fs.FileMode
	prevOwner system.Owner
}

// wpConfigMode là quyền checks.WPConfigPerm đòi.
const wpConfigMode fs.FileMode = 0o600

func (s *WPConfigPerm) Info() engine.StepInfo {
	l := s.Layout
	chmod := wpChmod(l, wpConfigMode, l.WPConfig)
	return engine.StepInfo{
		ID:    "wp.config.perm:" + l.Domain.String(),
		Title: i18n.T("Đặt lại quyền 0600 và chủ "+l.User+" cho "+l.WPConfig, "Reset "+l.WPConfig+" to mode 0600 and owner "+l.User),
		Why: i18n.T(
			"wp-config.php chứa mật khẩu DB. Quyền rộng hơn 0600 thì site khác trên máy đọc được mật khẩu; chủ không phải "+l.User+" thì PHP của site không đọc được file và site lỗi.",
			"wp-config.php holds the database password. A wider mode lets other sites on the machine read it; if the owner is not "+l.User+" the site's PHP cannot read the file and the site breaks.",
		),
		// Không khai Files: lúc xem trước engine đọc nội dung file trong Files,
		// tức là đọc mật khẩu DB ra giao diện (như WPConfig).
		Commands: []string{"chown " + l.User + ":" + site.WebGroup + " " + l.WPConfig, chmod.String()},
	}
}

func (s *WPConfigPerm) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	return wpCheckLayout(s.Layout)
}

// stat trả thông tin wp-config.php; không có, hoặc không phải file thường, là lỗi.
func (s *WPConfigPerm) stat(sys system.System) (system.FileInfo, error) {
	p := s.Layout.WPConfig
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return fi, fmt.Errorf("%s không có; tpan không tạo lại được (cần mật khẩu DB đang dùng), lấy lại từ backup", p)
	}
	if err != nil {
		return fi, fmt.Errorf("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsRegular() {
		return fi, fmt.Errorf("%s không phải file thường (%v); tpan không đặt quyền qua symlink hay thư mục", p, fi.Mode.Type())
	}
	return fi, nil
}

func (s *WPConfigPerm) ok(fi system.FileInfo) bool {
	return fi.Mode.Perm() == wpConfigMode && normOwner(fi.Owner).User == s.Layout.User
}

func (s *WPConfigPerm) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, err := sys.Stat(s.Layout.WPConfig)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Layout.WPConfig, err)
	}
	return fi.Mode.IsRegular() && s.ok(fi), nil
}

func (s *WPConfigPerm) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	l := s.Layout
	fi, err := s.stat(sys)
	if err != nil {
		return err
	}
	prevPerm, prevOwner := fi.Mode.Perm(), normOwner(fi.Owner)
	chowned := false
	if prevOwner.User != l.User {
		// Nhóm www-data: thư mục site có bit setgid nhóm www-data (mục 5), nên
		// file WP-CLI tạo dưới user của site mang nhóm đó; khôi phục backup
		// (M9) cũng đặt như vậy. Quyền 600 nên nhóm không cho ai đọc thêm.
		if err := sys.Chown(l.WPConfig, system.Owner{User: l.User, Group: site.WebGroup}); err != nil {
			return fmt.Errorf("đổi chủ %s: %w", l.WPConfig, err)
		}
		chowned = true
	}
	if res, err := sys.Run(ctx, wpChmod(l, wpConfigMode, l.WPConfig)); err != nil {
		err = withStdout(fmt.Errorf("đặt quyền 600 cho %s: %w", l.WPConfig, err), res)
		// Engine không Undo bước có Apply lỗi: tự trả chủ cũ.
		if chowned {
			if rerr := sys.Chown(l.WPConfig, prevOwner); rerr != nil {
				return errors.Join(err, fmt.Errorf("trả lại chủ cũ của %s: %w", l.WPConfig, rerr))
			}
		}
		return err
	}
	s.applied, s.prevPerm, s.prevOwner = true, prevPerm, prevOwner
	return nil
}

// Undo đi ngược Apply: chmod về quyền cũ khi user site còn là chủ, rồi mới trả
// chủ cũ.
func (s *WPConfigPerm) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("quyền của %s: %w", s.Layout.WPConfig, errNotApplied)
	}
	l := s.Layout
	if res, err := sys.Run(ctx, wpChmod(l, s.prevPerm, l.WPConfig)); err != nil {
		return withStdout(fmt.Errorf("trả lại quyền cũ của %s: %w", l.WPConfig, err), res)
	}
	if s.prevOwner.User != l.User {
		if err := sys.Chown(l.WPConfig, s.prevOwner); err != nil {
			return fmt.Errorf("trả lại chủ cũ của %s: %w", l.WPConfig, err)
		}
	}
	return nil
}
