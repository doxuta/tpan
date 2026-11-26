package steps

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// DB và user DB của một site (mục 5 "Cô lập từng site", mục 6.2 bước 4).
//
// Mỗi site có DB tp_<slug> và user 'tp_<slug>'@'localhost' chỉ có quyền trên
// đúng DB đó. Lệnh mariadb chạy bằng root của máy, vào root@localhost qua
// unix_socket (MariaDBSecure đã đảm bảo), nên không cần mật khẩu root nào.
//
// Câu SQL viết theo tài liệu MariaDB 10.11 (bản trong Ubuntu 24.04):
//   - CREATE DATABASE: https://mariadb.com/kb/en/create-database/
//   - CREATE USER:     https://mariadb.com/kb/en/create-user/ (user đã có: lỗi 1396)
//   - DROP USER:       https://mariadb.com/kb/en/drop-user/ (IF EXISTS; xoá luôn quyền đã cấp)
//   - GRANT:           https://mariadb.com/kb/en/grant/ (tên DB nhận ký tự đại diện _ và %)
//   - mysql.global_priv (tài khoản, từ 10.4): https://mariadb.com/kb/en/mysql-global_priv-table/
//
// Mật khẩu chỉ nằm trong Cmd.Stdin, không bao giờ nằm trên dòng lệnh, trong
// Info hay trong thông báo lỗi (stderr của mariadb có thể chép lại đoạn SQL
// lỗi, nên lỗi được lọc mật khẩu trước khi trả ra).

var (
	_ engine.Step = (*SiteDB)(nil)
	_ engine.Step = (*SiteDBAbsent)(nil)
)

// dbNameRe là lớp chặn cuối cho tên DB và tên user DB. Tên đi thẳng vào chuỗi
// SQL (trong `...` và '...'), nên chỉ nhận đúng các ký tự slug sinh ra: không
// có dấu nháy, backtick, dấu chấm phẩy hay xuống dòng nào lọt vào được.
// 64 là độ dài tối đa của tên DB trong MariaDB.
var dbNameRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// dbPasswordRe: mật khẩu nằm trong '...' của câu CREATE USER. Chỉ chữ và số
// thì không cần thoát ký tự nào; ≥ 24 ký tự để không đoán được (core sinh 32).
var dbPasswordRe = regexp.MustCompile(`^[A-Za-z0-9]{24,}$`)

// dbChosenPasswordRe (M36): mật khẩu người dùng tự đặt cho user DB rời và user
// DB của site PHP trống. Vẫn nằm trong '...' của câu SQL gửi qua stdin của
// mariadb: không có nháy, backtick, gạch chéo ngược, khoảng trắng hay xuống
// dòng nên vẫn không cần thoát ký tự nào. Phải khớp core.chosenDBPasswordRe.
var dbChosenPasswordRe = regexp.MustCompile(`^[A-Za-z0-9!#%*+,\-./:=?@^_~]{12,64}$`)

// dbUserPasswordOK: mật khẩu tpan sinh, hoặc mật khẩu tự đặt đúng luật.
func dbUserPasswordOK(pw string) bool {
	return dbPasswordRe.MatchString(pw) || dbChosenPasswordRe.MatchString(pw)
}

// dbMask là chỗ mật khẩu hiện ra trong Info và thông báo lỗi.
var dbMask = system.Secret{}.String()

// dbCheckLayout kiểm Layout trước mọi lệnh. Layout zero (quên ParseDomain)
// hay tên DB không đúng tên suy từ domain thì từ chối: tên DB không bao giờ
// được lấy từ chỗ nào khác ngoài site.LayoutOf (mục 11).
func dbCheckLayout(l site.Layout) error {
	if l.Domain.IsZero() {
		return errors.New("layout của site đang trống (domain chưa được kiểm)")
	}
	if !dbNameRe.MatchString(l.DB) {
		return fmt.Errorf("tên DB %q có ký tự không cho phép", l.DB)
	}
	if want := site.LayoutOf(l.Domain).DB; l.DB != want {
		return fmt.Errorf("tên DB %q không khớp tên suy từ domain %s (%s)", l.DB, l.Domain, want)
	}
	return nil
}

// ---- Câu SQL ----
//
// Mỗi hàm trả đúng một câu, kết thúc bằng ";\n". Info và Apply dùng chung các
// hàm này (Info truyền mật khẩu đã che), nên câu hiện ra luôn là câu chạy thật.

// dbStateSQL trả một dòng, hai số: DB có không (0/1), user DB có không (0/1).
// So bằng "=", không dùng LIKE: trong LIKE dấu "_" của tên là ký tự đại diện.
func dbStateSQL(name string) string {
	return "SELECT " +
		"(SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='" + name + "'), " +
		"(SELECT COUNT(*) FROM mysql.global_priv WHERE User='" + name + "' AND Host='localhost');\n"
}

// Không dùng IF NOT EXISTS khi tạo: nếu DB hay user xuất hiện giữa lúc đọc
// trạng thái và lúc tạo, câu lệnh phải lỗi, thay vì im lặng bỏ qua rồi bước
// này tưởng mình đã tạo và Undo xoá mất thứ không phải của nó.
func dbCreateDBSQL(name string) string {
	return "CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4;\n"
}

// M28 đợt 2 (M1-DBCONN): user mới có trần kết nối cùng lúc ngay khi tạo, dùng
// chung cho user DB của site và của DB rời. Lý do của số ở site.DBMaxUserConnections.
func dbCreateUserSQL(name, password string) string {
	return "CREATE USER '" + name + "'@'localhost' IDENTIFIED BY '" + password +
		"' WITH MAX_USER_CONNECTIONS " + strconv.Itoa(site.DBMaxUserConnections) + ";\n"
}

// dbGrantSQL cấp quyền trên đúng một DB. Trong GRANT ... ON `db`.*, dấu "_"
// và "%" là ký tự đại diện: `tp_shop_vn_3fa1c2`.* cũng khớp DB "tpXshop..."
// của site khác. Thoát "_" thành "\_" để quyền chỉ áp cho đúng tên này.
// Trang GRANT của MariaDB nói có ký tự đại diện nhưng không nói cách thoát;
// cách thoát lấy từ tài liệu MySQL (MariaDB kế thừa cú pháp này), đã kiểm
// trên máy ảo ở M2: SHOW GRANTS hiện `tp\_..`.* và user vẫn dùng được DB.
// https://dev.mysql.com/doc/refman/8.0/en/grant.html
func dbGrantSQL(name string) string {
	return "GRANT ALL PRIVILEGES ON `" + strings.ReplaceAll(name, "_", `\_`) + "`.* TO '" + name + "'@'localhost';\n"
}

// Xoá thì dùng IF EXISTS: chạy lại sau một lần hỏng giữa chừng vẫn được.
func dbDropUserSQL(name string) string {
	return "DROP USER IF EXISTS '" + name + "'@'localhost';\n"
}

func dbDropDBSQL(name string) string {
	return "DROP DATABASE IF EXISTS `" + name + "`;\n"
}

// dbShow là một dòng của Info().Commands: lệnh mariadb kèm câu SQL nó nhận
// qua stdin (chỉ để đọc, không phải lệnh shell chạy được).
func dbShow(sql string) string {
	return mariadbCmd("").String() + "  (stdin: " + strings.TrimSpace(sql) + ")"
}

// ---- Chạy SQL ----

// dbRedact gỡ mật khẩu khỏi lỗi của mariadb. Lỗi cú pháp của MariaDB chép lại
// đoạn SQL quanh chỗ sai ("near 'IDENTIFIED BY '...'"), nên stderr có thể
// chứa mật khẩu. Giữ *ExitError (bản sao đã lọc) để ai cần vẫn lấy được mã thoát.
func dbRedact(err error, password string) error {
	if err == nil || password == "" {
		return err
	}
	var ee *system.ExitError
	if errors.As(err, &ee) && err == error(ee) {
		c := *ee
		c.Stderr = strings.ReplaceAll(c.Stderr, password, dbMask)
		c.Cmd = strings.ReplaceAll(c.Cmd, password, dbMask)
		err = &c
	}
	if msg := err.Error(); strings.Contains(msg, password) {
		return errors.New(strings.ReplaceAll(msg, password, dbMask))
	}
	return err
}

// dbRun chạy một đoạn SQL qua stdin. password khác rỗng là mật khẩu có trong
// sql: nó bị lọc khỏi lỗi và stdout trước khi trả ra.
func dbRun(ctx context.Context, sys system.System, sql, password, what string) error {
	res, err := sys.Run(ctx, mariadbCmd(sql))
	if err == nil {
		return nil
	}
	if password != "" {
		res.Stdout = strings.ReplaceAll(res.Stdout, password, dbMask)
	}
	return withStdout(fmt.Errorf("%s: %w", what, dbRedact(err, password)), res)
}

// SiteDBState cho core biết DB và user DB của site có chưa, để từ chối khi
// máy lệch (user DB có mà wp-config.php không có, hoặc ngược lại; mục 6.2).
func SiteDBState(ctx context.Context, sys system.System, l site.Layout) (dbExists, userExists bool, err error) {
	if err := dbCheckLayout(l); err != nil {
		return false, false, err
	}
	res, err := sys.Run(ctx, mariadbCmd(dbStateSQL(l.DB)))
	if err != nil {
		return false, false, withStdout(fmt.Errorf("đọc trạng thái DB của site %s: %w", l.Domain, err), res)
	}
	f := strings.Fields(res.Stdout)
	if len(f) != 2 {
		return false, false, fmt.Errorf("đọc trạng thái DB của site %s: kết quả lạ %q (cần đúng hai số)", l.Domain, res.Stdout)
	}
	return f[0] != "0", f[1] != "0", nil
}

// ---- SiteDB ----

// SiteDB đảm bảo DB l.DB (utf8mb4) và user 'l.DB'@'localhost' có quyền ALL
// trên đúng DB đó. Check: DB có VÀ user có. Apply tạo phần còn thiếu và nhớ
// phần nào mình tạo; Undo xoá đúng phần đó (user trước, DB sau).
//
// User đã có thì không đụng tới: không đổi mật khẩu, không cấp thêm quyền.
// Trường hợp lệch (user có mà DB không, hay ngược lại) core từ chối từ trước
// bằng SiteDBState, bước này không tự đoán mật khẩu bên kia đang dùng.
type SiteDB struct {
	Layout   site.Layout
	Password system.Secret

	applied     bool
	createdDB   bool
	createdUser bool
}

func (s *SiteDB) validate() error {
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	// Không in mật khẩu ra lỗi, kể cả mật khẩu sai.
	if !dbPasswordRe.MatchString(s.Password.Reveal()) {
		return fmt.Errorf("mật khẩu DB của site %s phải gồm ít nhất 24 ký tự, chỉ chữ cái và chữ số", s.Layout.Domain)
	}
	return nil
}

func (s *SiteDB) Info() engine.StepInfo {
	l := s.Layout
	return engine.StepInfo{
		ID: "mariadb.site-db:" + l.Domain.String(),
		Title: i18n.T(
			"Tạo DB "+l.DB+" và user DB riêng của site "+l.Domain.String(),
			"Create database "+l.DB+" and the site's own database user for "+l.Domain.String(),
		),
		Why: i18n.T(
			"WordPress cần một DB để lưu bài viết, tài khoản và cài đặt; thiếu nó thì không cài được site. User DB riêng chỉ có quyền trên đúng DB này, nên lỡ site bị chiếm thì kẻ tấn công không đọc được DB của site khác.",
			"WordPress needs a database for posts, accounts and settings; without it the site cannot be installed. The site's own database user only has rights on this one database, so if the site is compromised the attacker cannot read other sites' databases.",
		),
		Commands: []string{
			dbShow(dbCreateDBSQL(l.DB)),
			dbShow(dbCreateUserSQL(l.DB, dbMask)),
			dbShow(dbGrantSQL(l.DB)),
		},
	}
}

func (s *SiteDB) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	dbOK, userOK, err := SiteDBState(ctx, sys, s.Layout)
	return dbOK && userOK, err
}

func (s *SiteDB) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	dbOK, userOK, err := SiteDBState(ctx, sys, s.Layout)
	if err != nil {
		return err
	}
	s.applied, s.createdDB, s.createdUser = true, false, false
	name, pw := s.Layout.DB, s.Password.Reveal()

	// Ba lệnh riêng, mỗi cờ chỉ bật khi lệnh của nó xong: lệnh sau lỗi thì biết
	// chắc phần nào đã tạo để xoá lại.
	if !dbOK {
		if err := dbRun(ctx, sys, dbCreateDBSQL(name), "", "tạo DB "+name); err != nil {
			return err
		}
		s.createdDB = true
	}
	if !userOK {
		if err := dbRun(ctx, sys, dbCreateUserSQL(name, pw), pw, "tạo user DB "+name); err != nil {
			return s.rollback(ctx, sys, err)
		}
		s.createdUser = true
		if err := dbRun(ctx, sys, dbGrantSQL(name), "", "cấp quyền trên DB "+name+" cho user "+name); err != nil {
			return s.rollback(ctx, sys, err)
		}
	}
	return nil
}

// rollback xoá phần Apply vừa tạo rồi trả lỗi gốc: engine không gọi Undo cho
// bước có Apply lỗi (xem common.go).
func (s *SiteDB) rollback(ctx context.Context, sys system.System, err error) error {
	if rerr := s.dropCreated(ctx, sys); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
}

// dropCreated xoá user trước, DB sau: không lúc nào còn một user có quyền
// trên một DB đã mất (quyền cấp theo tên sẽ dính vào DB tạo lại cùng tên).
func (s *SiteDB) dropCreated(ctx context.Context, sys system.System) error {
	var sql strings.Builder
	if s.createdUser {
		sql.WriteString(dbDropUserSQL(s.Layout.DB))
	}
	if s.createdDB {
		sql.WriteString(dbDropDBSQL(s.Layout.DB))
	}
	if sql.Len() == 0 {
		return nil
	}
	if err := dbRun(ctx, sys, sql.String(), "", "xoá DB và user DB "+s.Layout.DB+" vừa tạo"); err != nil {
		return err
	}
	s.createdUser, s.createdDB = false, false
	return nil
}

func (s *SiteDB) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return errNotApplied
	}
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	return s.dropCreated(ctx, sys)
}

// ---- SiteDBAbsent ----

// SiteDBAbsent xoá user DB rồi DB của site (tpan site rm, mục 6.4).
type SiteDBAbsent struct{ Layout site.Layout }

func (s *SiteDBAbsent) sql() string {
	return dbDropUserSQL(s.Layout.DB) + dbDropDBSQL(s.Layout.DB)
}

func (s *SiteDBAbsent) Info() engine.StepInfo {
	l := s.Layout
	return engine.StepInfo{
		ID: "mariadb.site-db-absent:" + l.Domain.String(),
		Title: i18n.T(
			"Xoá user DB và DB "+l.DB+" của site "+l.Domain.String(),
			"Drop the database user and database "+l.DB+" of "+l.Domain.String(),
		),
		Why: i18n.T(
			"Site đã gỡ mà DB và user DB còn lại thì dữ liệu cũ nằm đó không ai quản, user DB vẫn đăng nhập được, và tạo lại site cùng domain sẽ bị từ chối vì DB đã có.",
			"If the site is gone but its database and user remain, old data sits there unmanaged, the database user can still log in, and re-creating a site with the same domain is refused because the database exists.",
		),
		Commands: []string{
			dbShow(dbDropUserSQL(l.DB)),
			dbShow(dbDropDBSQL(l.DB)),
		},
		// Xoá toàn bộ bài viết, tài khoản của site.
		Danger: true,
		NoUndo: i18n.T(
			"DB đã xoá thì dữ liệu mất, chỉ lấy lại được từ backup.",
			"Once the database is dropped its data is gone and can only be restored from a backup.",
		),
	}
}

func (s *SiteDBAbsent) Check(ctx context.Context, sys system.System) (bool, error) {
	dbOK, userOK, err := SiteDBState(ctx, sys, s.Layout)
	return !dbOK && !userOK, err
}

func (s *SiteDBAbsent) Apply(ctx context.Context, sys system.System) error {
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	return dbRun(ctx, sys, s.sql(), "", "xoá DB và user DB của site "+s.Layout.Domain.String())
}

func (s *SiteDBAbsent) Undo(context.Context, system.System) error { return nil }

// CreatedUser: lần Apply này đã tạo user DB (tức là Password đang là mật khẩu
// thật của user đó). false khi user có sẵn: bước không đổi mật khẩu của nó.
func (s *SiteDB) CreatedUser() bool { return s.createdUser }
