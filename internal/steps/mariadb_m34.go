package steps

// M34: DB có phần bị xoá ngoài tpan (docs/thay-doi/M34.md).
//
// Hai bước tạo lại user DB đã bị DROP bằng tay:
//
//   - DBUserRecreate: user của một DB rời, hoặc của DB một site PHP trống. tpan
//     không giữ mật khẩu cũ nên user có mật khẩu mới (hiện một lần).
//   - SiteDBUserRestore: user DB của một site WordPress, với đúng mật khẩu đang
//     ghi trong wp-config.php của site, nên site chạy lại mà không phải sửa gì.
//
// DB đã mất thì không cần bước mới ở đây: DB rời được gỡ dòng (DBUserAbsent,
// DBTrashDumpAbsent), DB của site được tạo lại lúc nhập (DBImport với Fresh).

import (
	"context"
	"errors"
	"fmt"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*DBUserRecreate)(nil)
	_ engine.Step = (*SiteDBUserRestore)(nil)
)

// DBUserRecreate tạo lại user 'Name'@'localhost' cho DB rời Name: CREATE USER
// (kèm trần kết nối như lúc tạo DB), GRANT trên đúng DB đó, và khoá ngay nếu
// Locked (DB đang tạm khoá thì user mới cũng phải khoá: tạo lại không được là
// một cách mở khoá lén). tpan không giữ mật khẩu cũ nên user có mật khẩu mới.
//
// Check: chỉ true sau Apply của chính bước. Apply: DB không có thì lỗi (không
// có gì để cấp quyền); user đã có thì lỗi (không do bước này tạo, đổi mật khẩu
// là việc của DBUserPassword). Undo: DROP USER nếu chính Apply đã tạo.
//
// NoDB: người gọi biết DB cũng đã mất và vẫn muốn có user trước (DB của một
// site: user phải có thì khôi phục site từ backup mới qua được kiểm chứng).
// MariaDB cho cấp quyền trên một DB chưa có; quyền có hiệu lực khi DB được tạo.
type DBUserRecreate struct {
	Name     string
	Password system.Secret
	Locked   bool
	NoDB     bool

	applied bool
}

func (s *DBUserRecreate) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	if !dbPasswordRe.MatchString(s.Password.Reveal()) {
		return fmt.Errorf("mật khẩu mới của user DB %s phải gồm ít nhất 24 ký tự, chỉ chữ cái và chữ số", s.Name)
	}
	return nil
}

func (s *DBUserRecreate) Info() engine.StepInfo {
	cmds := []string{dbShow(dbStateSQL(s.Name)), dbShow(dbCreateUserSQL(s.Name, dbMask)), dbShow(dbGrantSQL(s.Name))}
	title := i18n.T("Tạo lại user DB "+s.Name+" với mật khẩu mới", "Recreate the database user "+s.Name+" with a new password")
	if s.Locked {
		cmds = append(cmds, dbShow(dbLockSQL(s.Name)))
		title = i18n.T("Tạo lại user DB "+s.Name+" với mật khẩu mới, ở trạng thái khoá", "Recreate the database user "+s.Name+" with a new password, locked")
	}
	return engine.StepInfo{
		ID:       "mariadb.user-recreate:" + s.Name,
		Title:    title,
		Why:      s.why(),
		Commands: cmds,
	}
}

func (s *DBUserRecreate) why() i18n.Text {
	if s.NoDB {
		return i18n.T(
			"User DB "+s.Name+" đã bị xoá ngoài tpan, và DB cùng tên cũng không còn. Bước này tạo lại user trước, chỉ có quyền trên đúng DB đó (quyền có hiệu lực khi DB được tạo lại), kèm trần số kết nối như lúc tạo. tpan không giữ mật khẩu cũ: user có mật khẩu mới, hiện một lần sau khi chạy.",
			"The database user "+s.Name+" was dropped outside tpan, and the database of the same name is gone too. This step recreates the user first, with rights on that one database only (they take effect once the database is created again) and the same connection limit as at creation. tpan never stores the old password: the user gets a new one, shown once after the run.",
		)
	}
	return i18n.T(
		"DB "+s.Name+" còn trong MariaDB nhưng user DB cùng tên đã bị xoá ngoài tpan, nên không ứng dụng nào đăng nhập được vào DB. Bước này tạo lại user chỉ có quyền trên đúng DB đó, kèm trần số kết nối như lúc tạo DB. tpan không giữ mật khẩu cũ: user có mật khẩu mới, hiện một lần sau khi chạy. Dữ liệu trong DB không bị đụng tới.",
		"The database "+s.Name+" is still in MariaDB but its database user was dropped outside tpan, so no application can log in to it. This step recreates the user with rights on that one database only, and the same connection limit as when the database was created. tpan never stores the old password: the user gets a new one, shown once after the run. The data in the database is not touched.",
	)
}

func (s *DBUserRecreate) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *DBUserRecreate) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	db, user, err := DBNameState(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	if !db && !s.NoDB {
		return fmt.Errorf("DB %s không có trong MariaDB: không có gì để cấp quyền cho user mới", s.Name)
	}
	if user {
		return fmt.Errorf("user DB %s đã có trong MariaDB (không do bước này tạo); muốn mật khẩu mới thì đổi mật khẩu", s.Name)
	}
	name, pw := s.Name, s.Password.Reveal()
	sql := dbCreateUserSQL(name, pw) + dbGrantSQL(name)
	if s.Locked {
		sql += dbLockSQL(name)
	}
	// Engine không Undo bước có Apply lỗi: user tạo dở (CREATE được, GRANT hay
	// LOCK lỗi) thì tự xoá, để không sót một user không quyền hoặc không khoá.
	if err := dbRun(ctx, sys, sql, pw, "tạo lại user DB "+name); err != nil {
		if rerr := dbRun(ctx, sys, dbDropUserSQL(name), "", "xoá user DB "+name+" tạo dở"); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	s.applied = true
	return nil
}

func (s *DBUserRecreate) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("tạo lại user DB %s: %w", s.Name, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := dbRun(ctx, sys, dbDropUserSQL(s.Name), "", "xoá user DB "+s.Name+" vừa tạo lại"); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// ============================================================ SiteDBUserRestore

// SiteDBUserRestore tạo lại user DB của một site WordPress đã bị xoá ngoài
// tpan, với ĐÚNG mật khẩu đang ghi trong wp-config.php của site: CREATE USER
// (kèm trần kết nối như lúc tạo site), GRANT trên đúng DB của site, và khoá
// ngay nếu Locked (DB của site đang tạm khoá). wp-config.php không bị sửa, nên
// site chạy lại như trước khi user bị xoá.
//
// Mật khẩu đọc bằng DBPassword.readOld: file thường của user site, đúng một
// dòng define mỗi DB_NAME, DB_USER, DB_PASSWORD, tên DB và user là của chính
// site này, mật khẩu đúng dạng tpan sinh (chỉ chữ và số, nên ghép vào câu SQL
// không cần thoát ký tự nào). Sai một điều thì dừng, không tạo user nào: mật
// khẩu do người khác đặt có thể mang ký tự mà câu CREATE USER này không chứa
// an toàn được.
//
// Check: user DB đã có (kể cả do người khác vừa tạo lại) thì bước được bỏ qua.
// Apply: user đã có thì lỗi (Check vừa nói chưa có: đã có thay đổi ngoài tpan
// giữa hai lần đọc). Undo: DROP USER nếu chính Apply đã tạo.
type SiteDBUserRestore struct {
	Layout site.Layout
	Locked bool

	applied bool
}

func (s *SiteDBUserRestore) validate() error {
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	return wpCheckLayout(s.Layout)
}

func (s *SiteDBUserRestore) Info() engine.StepInfo {
	l := s.Layout
	cmds := []string{dbShow(dbStateSQL(l.DB)), dbShow(dbCreateUserSQL(l.DB, dbMask)), dbShow(dbGrantSQL(l.DB))}
	title := i18n.T(
		"Tạo lại user DB "+l.DB+" của site "+l.Domain.String()+" với mật khẩu trong wp-config.php",
		"Recreate database user "+l.DB+" of "+l.Domain.String()+" with the password in wp-config.php")
	if s.Locked {
		cmds = append(cmds, dbShow(dbLockSQL(l.DB)))
		title = i18n.T(
			"Tạo lại user DB "+l.DB+" của site "+l.Domain.String()+" với mật khẩu trong wp-config.php, ở trạng thái khoá",
			"Recreate database user "+l.DB+" of "+l.Domain.String()+" with the password in wp-config.php, locked")
	}
	return engine.StepInfo{
		ID:    "mariadb.site-user-restore:" + l.Domain.String(),
		Title: title,
		Why: i18n.T(
			"User DB của site đã bị xoá ngoài tpan, nên WordPress báo \"Error establishing a database connection\". Bước này tạo lại user chỉ có quyền trên đúng DB của site, với mật khẩu đang ghi trong "+l.WPConfig+" (đọc trực tiếp, không qua dòng lệnh hay log). wp-config.php và dữ liệu trong DB không bị đụng tới.",
			"The site's database user was dropped outside tpan, so WordPress shows \"Error establishing a database connection\". This step recreates the user with rights on the site's database only, using the password written in "+l.WPConfig+" (read directly, never through a command line or a log). wp-config.php and the data in the database are not touched.",
		),
		// Không đưa wp-config.php vào Files: xem trước sẽ đọc mật khẩu ra giao diện.
		Commands: cmds,
	}
}

func (s *SiteDBUserRestore) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	_, user, err := SiteDBState(ctx, sys, s.Layout)
	if err != nil {
		return false, err
	}
	return user, nil
}

func (s *SiteDBUserRestore) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	l := s.Layout
	_, user, err := SiteDBState(ctx, sys, l)
	if err != nil {
		return err
	}
	if user {
		return fmt.Errorf("user DB %s đã có trong MariaDB (không do bước này tạo); không ghi đè", l.DB)
	}
	pw, err := (&DBPassword{Layout: l}).readOld(sys)
	if err != nil {
		return fmt.Errorf("không tạo lại được user DB %s từ wp-config.php: %w", l.DB, err)
	}
	v := pw.Reveal()
	sql := dbCreateUserSQL(l.DB, v) + dbGrantSQL(l.DB)
	if s.Locked {
		sql += dbLockSQL(l.DB)
	}
	// Engine không Undo bước có Apply lỗi: user tạo dở thì tự xoá.
	if err := dbRun(ctx, sys, sql, v, "tạo lại user DB "+l.DB); err != nil {
		if rerr := dbRun(ctx, sys, dbDropUserSQL(l.DB), "", "xoá user DB "+l.DB+" tạo dở"); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return wpHide(err, pw)
	}
	s.applied = true
	return nil
}

func (s *SiteDBUserRestore) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("tạo lại user DB của %s: %w", s.Layout.Domain, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := dbRun(ctx, sys, dbDropUserSQL(s.Layout.DB), "", "xoá user DB "+s.Layout.DB+" vừa tạo lại"); err != nil {
		return err
	}
	s.applied = false
	return nil
}
