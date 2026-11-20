package steps

// Các Step MariaDB của M13 (mục 6.12 của docs/KE-HOACH.md): DB rời, đổi
// charset, tạm khoá user DB, thùng rác của DB rời. Khác site_db.go ở chỗ
// mọi bước nhận TÊN (DB rời "<owner>_<tên>" hoặc DB của site "tp_<slug>"),
// không nhận site.Layout. Tên vẫn bị kiểm lại bằng dbNameRe trong từng bước:
// nó đi thẳng vào chuỗi SQL.
//
// Tài liệu đã tra (MariaDB 10.11, bản trong Ubuntu 24.04):
//   - ALTER USER ... ACCOUNT LOCK/UNLOCK: https://mariadb.com/kb/en/account-locking/
//   - KILL: https://mariadb.com/kb/en/kill/ ("KILL [HARD | SOFT] {CONNECTION|QUERY}
//     thread_id"; id không còn thì "ERROR 1094 (HY000): Unknown thread id")
//   - information_schema.PROCESSLIST: https://mariadb.com/kb/en/information-schema-processlist-table/
//   - information_schema.SCHEMATA (DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME):
//     https://mariadb.com/kb/en/information-schema-schemata-table/
//   - ALTER DATABASE: https://mariadb.com/kb/en/alter-database/ (chỉ đổi mặc
//     định cho bảng tạo SAU đó; bảng đã có giữ charset cũ)
//   - CREATE DATABASE ... CHARACTER SET ... COLLATE ...: https://mariadb.com/kb/en/create-database/
//
// Trạng thái khoá đọc bằng dbLockStateSQL (mysql.global_priv, fix.go), không
// bằng SHOW CREATE USER: câu đó in hash mật khẩu, mà engine ghi mọi output
// vào log của job.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*StandaloneDB)(nil)
	_ engine.Step = (*DBCharset)(nil)
	_ engine.Step = (*DBAccountLock)(nil)
	_ engine.Step = (*DBKillConnections)(nil)
	_ engine.Step = (*DBAccountUnlock)(nil)
	_ engine.Step = (*DBUserPassword)(nil)
	_ engine.Step = (*DBTrashDump)(nil)
	_ engine.Step = (*DBDrop)(nil)
	_ engine.Step = (*DBRestore)(nil)
	_ engine.Step = (*DBTrashDumpAbsent)(nil)
	_ engine.Step = (*DBUserAbsent)(nil)
)

// sdbCharsetRe: tên charset và collation đi vào câu SQL không có dấu nháy
// (CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci). Core đã kiểm cặp có
// trong information_schema.COLLATIONS; đây là lớp chặn cuối.
var sdbCharsetRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// sdbKillWait, sdbKillEvery: trần và nhịp chờ kết nối đã KILL biến khỏi
// PROCESSLIST. KILL chỉ đặt cờ; luồng tự thoát ngay sau đó. Biến để test rút ngắn.
var (
	sdbKillWait  = 3 * time.Second
	sdbKillEvery = 100 * time.Millisecond
)

// sdbTrashParent là /var/lib/tpan: giữ 0755 vì acme bên trong phải đi qua được
// (như backupMkdirs); từ thư mục thùng rác trở xuống là 0700 root.
const sdbTrashParent = "/var/lib/tpan"

func sdbCheckName(name string) error {
	if !dbNameRe.MatchString(name) {
		return fmt.Errorf("tên DB %q có ký tự không cho phép", name)
	}
	return nil
}

func sdbCheckCharset(charset, collation string) error {
	if !sdbCharsetRe.MatchString(charset) || !sdbCharsetRe.MatchString(collation) {
		return fmt.Errorf("charset %q hoặc collation %q có ký tự không cho phép", charset, collation)
	}
	return nil
}

// ---- Câu SQL ----

func sdbCreateDBSQL(name, charset, collation string) string {
	return "CREATE DATABASE `" + name + "` CHARACTER SET " + charset + " COLLATE " + collation + ";\n"
}

func sdbAlterDBSQL(name, charset, collation string) string {
	return "ALTER DATABASE `" + name + "` CHARACTER SET " + charset + " COLLATE " + collation + ";\n"
}

// sdbSchemaSQL trả một dòng "charset<TAB>collation" khi DB có, không dòng nào khi không.
func sdbSchemaSQL(name string) string {
	return "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='" + name + "';\n"
}

// sdbConnsSQL: mọi kết nối của user, lọc theo USER thôi. HOST của kết nối TCP
// là "localhost:<cổng>" chứ không phải "localhost", lọc theo HOST sẽ sót.
func sdbConnsSQL(name string) string {
	return "SELECT ID FROM information_schema.PROCESSLIST WHERE USER='" + name + "';\n"
}

func sdbKillSQL(id uint64) string { return "KILL CONNECTION " + strconv.FormatUint(id, 10) + ";\n" }

// ---- Đọc trạng thái (core cũng gọi) ----

// DBNameState cho biết DB tên name và user 'name'@'localhost' có chưa. Core
// dùng để từ chối tạo DB rời trùng thứ đã có trong MariaDB mà không có trong
// bảng databases (do người khác tạo tay).
func DBNameState(ctx context.Context, sys system.System, name string) (dbExists, userExists bool, err error) {
	if err := sdbCheckName(name); err != nil {
		return false, false, err
	}
	res, err := sys.Run(ctx, mariadbCmd(dbStateSQL(name)))
	if err != nil {
		return false, false, withStdout(fmt.Errorf("đọc trạng thái DB %s: %w", name, err), res)
	}
	f := strings.Fields(res.Stdout)
	if len(f) != 2 {
		return false, false, fmt.Errorf("đọc trạng thái DB %s: kết quả lạ %q (cần đúng hai số)", name, res.Stdout)
	}
	return f[0] != "0", f[1] != "0", nil
}

// DBCharsetOf đọc charset và collation mặc định của DB; exists=false khi DB không có.
func DBCharsetOf(ctx context.Context, sys system.System, name string) (charset, collation string, exists bool, err error) {
	if err := sdbCheckName(name); err != nil {
		return "", "", false, err
	}
	res, err := sys.Run(ctx, mariadbCmd(sdbSchemaSQL(name)))
	if err != nil {
		return "", "", false, withStdout(fmt.Errorf("đọc charset của DB %s: %w", name, err), res)
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return "", "", false, nil
	}
	f := strings.Split(out, "\t")
	if len(f) != 2 || strings.Contains(out, "\n") {
		return "", "", false, fmt.Errorf("đọc charset của DB %s: kết quả lạ %q", name, res.Stdout)
	}
	return f[0], f[1], true, nil
}

// sdbLockState: user có không, đang khoá không (cùng câu với DBUserUnlock của M4).
func sdbLockState(ctx context.Context, sys system.System, name string) (exists, locked bool, err error) {
	if err := sdbCheckName(name); err != nil {
		return false, false, err
	}
	res, err := sys.Run(ctx, mariadbCmd(dbLockStateSQL(name)))
	if err != nil {
		return false, false, withStdout(fmt.Errorf("đọc trạng thái khoá của user DB %s: %w", name, err), res)
	}
	f := strings.Fields(res.Stdout)
	if len(f) != 2 {
		return false, false, fmt.Errorf("đọc trạng thái khoá của user DB %s: kết quả lạ %q (cần đúng hai số)", name, res.Stdout)
	}
	return f[0] != "0", f[1] != "0", nil
}

// sdbConns đọc id các kết nối đang mở của user. Mỗi id phải là số: stdout
// không bao giờ được ghép thẳng vào câu KILL.
func sdbConns(ctx context.Context, sys system.System, name string) ([]uint64, error) {
	res, err := sys.Run(ctx, mariadbCmd(sdbConnsSQL(name)))
	if err != nil {
		return nil, withStdout(fmt.Errorf("đọc kết nối đang mở của user DB %s: %w", name, err), res)
	}
	var ids []uint64
	for _, f := range strings.Fields(res.Stdout) {
		id, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("đọc kết nối đang mở của user DB %s: id lạ %q", name, f)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// sdbLock khoá user (đã có, chưa khoá). Dùng chung cho DBAccountLock và cho
// hoàn tác của DBAccountUnlock.
func sdbLock(ctx context.Context, sys system.System, name string) error {
	return dbRun(ctx, sys, dbLockSQL(name), "", "khoá user DB "+name)
}

func sdbUnlock(ctx context.Context, sys system.System, name string) error {
	return dbRun(ctx, sys, dbUnlockSQL(name), "", "mở khoá user DB "+name)
}

// sdbDump là bước xuất của mariadb-dump cho một DB (cờ giống M9:
// --single-transaction --quick, không --databases). Gọi write/complete thẳng
// vì validate của MariaDBDump chỉ nhận thư mục backup và khôi phục của site.
func sdbDump(name string) *MariaDBDump {
	return &MariaDBDump{Path: account.DBTrashDump(name), DB: name}
}

// sdbDumpReady: file xuất trong thùng rác có và trọn vẹn (dòng cuối "-- Dump completed").
func sdbDumpReady(ctx context.Context, sys system.System, name string) error {
	d := sdbDump(name)
	if !importPathRe.MatchString(d.Path) {
		return fmt.Errorf("file xuất %q có ký tự không cho phép", d.Path)
	}
	ok, err := d.complete(ctx, sys)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s không có hoặc không phải bản xuất trọn vẹn (thiếu dòng %q)", d.Path, dumpDone)
	}
	return nil
}

// sdbLoad tạo DB với charset, collation cho trước rồi nhập file xuất
// (--abort-source-on-error, như nhập DB của M9). Nhập lỗi thì xoá DB vừa tạo:
// một DB nhập dở trông như DB dùng được.
func sdbLoad(ctx context.Context, sys system.System, name, charset, collation, file string) error {
	if err := dbRun(ctx, sys, sdbCreateDBSQL(name, charset, collation), "", "tạo lại DB "+name); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, importCmd(name, file)); err != nil {
		err = withStdout(fmt.Errorf("nhập %s vào DB %s: %w", file, name, err), res)
		if rerr := dbRun(ctx, sys, dbDropDBSQL(name), "", "xoá DB "+name+" nhập dở"); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return nil
}

// ================================================================ StandaloneDB

// StandaloneDB tạo DB rời Name (charset, collation cho trước) và user
// 'Name'@'localhost' có quyền ALL trên đúng DB đó (mục 6.12). Check: cả DB
// lẫn user đã có. Apply: thấy một trong hai đã có thì lỗi, không đụng gì
// (thứ đó không do bước này tạo; core đã từ chối từ trước bằng DBNameState).
// Undo: xoá user rồi DB.
type StandaloneDB struct {
	Name      string
	Charset   string
	Collation string
	Password  system.Secret

	applied bool
}

func (s *StandaloneDB) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	if err := sdbCheckCharset(s.Charset, s.Collation); err != nil {
		return err
	}
	if !dbPasswordRe.MatchString(s.Password.Reveal()) {
		return fmt.Errorf("mật khẩu của user DB %s phải gồm ít nhất 24 ký tự, chỉ chữ cái và chữ số", s.Name)
	}
	return nil
}

func (s *StandaloneDB) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.standalone-db:" + s.Name,
		Title: i18n.T("Tạo DB "+s.Name+" ("+s.Collation+") và user DB cùng tên", "Create database "+s.Name+" ("+s.Collation+") and a database user of the same name"),
		Why: i18n.T(
			"DB rời dành cho ứng dụng không phải site WordPress của tpan. User DB riêng chỉ có quyền trên đúng DB này, nên lộ mật khẩu của nó không làm lộ DB nào khác. Mật khẩu chỉ hiện một lần sau khi tạo.",
			"A standalone database is for an application other than a tpan WordPress site. Its own database user only has rights on this one database, so leaking its password exposes no other database. The password is shown once after creation.",
		),
		Commands: []string{
			dbShow(sdbCreateDBSQL(s.Name, s.Charset, s.Collation)),
			dbShow(dbCreateUserSQL(s.Name, dbMask)),
			dbShow(dbGrantSQL(s.Name)),
		},
	}
}

func (s *StandaloneDB) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	db, user, err := DBNameState(ctx, sys, s.Name)
	return db && user, err
}

func (s *StandaloneDB) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	db, user, err := DBNameState(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	if db || user {
		return fmt.Errorf("DB hoặc user DB %s đã có trong MariaDB mà không do bước này tạo; tpan không đụng tới", s.Name)
	}
	name, pw := s.Name, s.Password.Reveal()
	if err := dbRun(ctx, sys, sdbCreateDBSQL(name, s.Charset, s.Collation), "", "tạo DB "+name); err != nil {
		return err
	}
	// Engine không Undo bước có Apply lỗi: tự xoá phần đã tạo.
	if err := dbRun(ctx, sys, dbCreateUserSQL(name, pw)+dbGrantSQL(name), pw, "tạo user DB "+name+" và cấp quyền"); err != nil {
		if rerr := s.drop(ctx, sys); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	s.applied = true
	return nil
}

func (s *StandaloneDB) drop(ctx context.Context, sys system.System) error {
	return dbRun(ctx, sys, dbDropUserSQL(s.Name)+dbDropDBSQL(s.Name), "", "xoá DB và user DB "+s.Name+" vừa tạo")
}

func (s *StandaloneDB) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("tạo DB %s: %w", s.Name, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.drop(ctx, sys); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// =================================================================== DBCharset

// DBCharset đổi charset và collation mặc định của DB (ALTER DATABASE). Check:
// SCHEMATA đã đúng cặp. Undo: đổi về cặp cũ đọc lúc Apply. DB không có thì lỗi.
type DBCharset struct {
	Name      string
	Charset   string
	Collation string

	applied             bool
	oldCharset, oldColl string
}

func (s *DBCharset) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	return sdbCheckCharset(s.Charset, s.Collation)
}

func (s *DBCharset) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.charset:" + s.Name,
		Title: i18n.T("Đổi charset của DB "+s.Name+" thành "+s.Charset+" / "+s.Collation, "Change the charset of database "+s.Name+" to "+s.Charset+" / "+s.Collation),
		Why: i18n.T(
			"Charset và collation quyết định chữ nào lưu được (utf8mb4 mới lưu được emoji) và cách so, sắp chữ. ALTER DATABASE chỉ đổi mặc định cho bảng tạo SAU lúc này; bảng đã có giữ charset cũ, muốn đổi phải chuyển từng bảng (ALTER TABLE ... CONVERT TO), tpan không tự làm.",
			"The charset and collation decide which characters can be stored (only utf8mb4 stores emoji) and how text is compared and sorted. ALTER DATABASE only changes the default for tables created AFTER this; existing tables keep their old charset and must be converted one by one (ALTER TABLE ... CONVERT TO), which tpan does not do.",
		),
		Commands: []string{dbShow(sdbSchemaSQL(s.Name)), dbShow(sdbAlterDBSQL(s.Name, s.Charset, s.Collation))},
	}
}

func (s *DBCharset) current(ctx context.Context, sys system.System) (string, string, error) {
	cs, coll, ok, err := DBCharsetOf(ctx, sys, s.Name)
	if err == nil && !ok {
		err = fmt.Errorf("DB %s không có trong MariaDB", s.Name)
	}
	return cs, coll, err
}

func (s *DBCharset) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	cs, coll, err := s.current(ctx, sys)
	return cs == s.Charset && coll == s.Collation, err
}

func (s *DBCharset) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	cs, coll, err := s.current(ctx, sys)
	if err != nil {
		return err
	}
	// Cặp cũ đọc từ MariaDB nên luôn đúng dạng; kiểm lại vì Undo ghép nó vào SQL.
	if err := sdbCheckCharset(cs, coll); err != nil {
		return fmt.Errorf("charset hiện tại của DB %s: %w", s.Name, err)
	}
	if err := dbRun(ctx, sys, sdbAlterDBSQL(s.Name, s.Charset, s.Collation), "", "đổi charset của DB "+s.Name); err != nil {
		return err
	}
	s.applied, s.oldCharset, s.oldColl = true, cs, coll
	return nil
}

func (s *DBCharset) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("đổi charset của DB %s: %w", s.Name, errNotApplied)
	}
	if err := dbRun(ctx, sys, sdbAlterDBSQL(s.Name, s.oldCharset, s.oldColl), "", "trả charset cũ cho DB "+s.Name); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// =============================================================== DBAccountLock

// DBAccountLock khoá user 'Name'@'localhost' (ALTER USER ... ACCOUNT LOCK).
// Kết nối đang mở không bị cắt bởi câu này: DBKillConnections chạy ngay sau.
// Check: user đang khoá; user không có cũng là đạt (như DBUserLock của M6:
// không ai đăng nhập được bằng user không tồn tại). Undo: mở nếu chính Apply đã khoá.
type DBAccountLock struct {
	Name string

	applied bool
	locked  bool
}

func (s *DBAccountLock) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.lock:" + s.Name,
		Title: i18n.T("Khoá user DB "+s.Name, "Lock the database user "+s.Name),
		Why: i18n.T(
			"Khoá user DB thì không ai đăng nhập được vào DB bằng user này nữa: site hay ứng dụng đang dùng nó báo lỗi kết nối DB cho tới khi mở khoá. Khoá không đổi mật khẩu, quyền hay dữ liệu nào; mở khoá là chạy lại như cũ.",
			"Once the database user is locked nobody can log in with it: the site or application using it shows a database connection error until it is unlocked. Locking changes no password, privilege or data; unlocking brings everything back.",
		),
		Commands: []string{dbShow(dbLockStateSQL(s.Name)), dbShow(dbLockSQL(s.Name))},
		Danger:   true,
	}
}

func (s *DBAccountLock) Check(ctx context.Context, sys system.System) (bool, error) {
	exists, locked, err := sdbLockState(ctx, sys, s.Name)
	return !exists || locked, err
}

func (s *DBAccountLock) Apply(ctx context.Context, sys system.System) error {
	exists, locked, err := sdbLockState(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	doLock := exists && !locked
	if doLock {
		if err := sdbLock(ctx, sys, s.Name); err != nil {
			return err
		}
	}
	s.applied, s.locked = true, doLock
	return nil
}

func (s *DBAccountLock) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("khoá user DB %s: %w", s.Name, errNotApplied)
	}
	if s.locked {
		if err := sdbCheckName(s.Name); err != nil {
			return err
		}
		if err := sdbUnlock(ctx, sys, s.Name); err != nil {
			return err
		}
	}
	s.applied, s.locked = false, false
	return nil
}

// =========================================================== DBKillConnections

// DBKillConnections cắt mọi kết nối đang mở của user Name (KILL CONNECTION
// <id>, id đọc từ information_schema.PROCESSLIST). Chạy sau DBAccountLock:
// user đã khoá thì không mở được kết nối mới, nên sau bước này không còn ai
// dùng DB bằng user đó. Mỗi KILL một lần gọi mariadb: client dừng ở câu lỗi
// đầu tiên, mà kết nối tự đóng giữa lúc đọc và lúc KILL cho lỗi 1094, coi là
// đã xong. Check: không còn kết nối nào. Undo: không có (kết nối đã cắt
// không nối lại được; chương trình phía kia tự nối lại khi user được mở).
type DBKillConnections struct {
	Name string
}

func (s *DBKillConnections) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.kill:" + s.Name,
		Title: i18n.T("Cắt các kết nối đang mở của user DB "+s.Name, "Close the open connections of database user "+s.Name),
		Why: i18n.T(
			"Khoá user chỉ chặn đăng nhập mới; kết nối đã mở từ trước vẫn đọc, ghi DB được. Bỏ bước này thì tạm khoá chưa có tác dụng với chương trình đang chạy. Giao dịch đang dở của các kết nối này bị huỷ (rollback).",
			"Locking the user only blocks new logins; connections opened earlier can still read and write. Without this step a suspension has no effect on programs already running. Unfinished transactions on these connections are rolled back.",
		),
		Commands: []string{dbShow(sdbConnsSQL(s.Name)), mariadbCmd("").String() + "  (stdin: KILL CONNECTION <id>; mỗi id một lần)"},
		Danger:   true,
		NoUndo: i18n.T(
			"Kết nối đã cắt không nối lại được; chương trình tự nối lại khi user được mở khoá.",
			"A closed connection cannot be reopened; programs reconnect by themselves once the user is unlocked.",
		),
	}
}

func (s *DBKillConnections) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := sdbCheckName(s.Name); err != nil {
		return false, err
	}
	ids, err := sdbConns(ctx, sys, s.Name)
	return len(ids) == 0, err
}

func (s *DBKillConnections) Apply(ctx context.Context, sys system.System) error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	ids, err := sdbConns(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	for _, id := range ids {
		err := dbRun(ctx, sys, sdbKillSQL(id), "", fmt.Sprintf("cắt kết nối %d của user DB %s", id, s.Name))
		if ee, ok := isExit(err); ok && strings.Contains(ee.Stderr, "1094") {
			continue // kết nối đã tự đóng
		}
		if err != nil {
			return err
		}
	}
	// KILL chỉ đặt cờ; chờ các luồng thoát hẳn để Check sau Apply thấy đúng.
	deadline := time.Now().Add(sdbKillWait)
	for {
		left, err := sdbConns(ctx, sys, s.Name)
		if err != nil || len(left) == 0 {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("user DB %s vẫn còn %d kết nối sau khi KILL (xem: SHOW PROCESSLIST)", s.Name, len(left))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sdbKillEvery):
		}
	}
}

func (s *DBKillConnections) Undo(context.Context, system.System) error { return nil }

// ============================================================= DBAccountUnlock

// DBAccountUnlock mở khoá user 'Name'@'localhost' (ACCOUNT UNLOCK). Check:
// user có và không khoá. User không có thì Apply lỗi (tạo lại user cần mật
// khẩu mà tpan không giữ). Undo: khoá lại nếu chính Apply đã mở.
type DBAccountUnlock struct {
	Name string

	applied  bool
	unlocked bool
}

func (s *DBAccountUnlock) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.unlock:" + s.Name,
		Title: i18n.T("Mở khoá user DB "+s.Name, "Unlock the database user "+s.Name),
		Why: i18n.T(
			"User DB đang khoá thì site hay ứng dụng dùng nó không đăng nhập được vào DB. Mở khoá không đổi mật khẩu hay quyền nào.",
			"While the database user is locked the site or application using it cannot log in to the database. Unlocking changes no password or privilege.",
		),
		Commands: []string{dbShow(dbLockStateSQL(s.Name)), dbShow(dbUnlockSQL(s.Name))},
	}
}

func (s *DBAccountUnlock) Check(ctx context.Context, sys system.System) (bool, error) {
	exists, locked, err := sdbLockState(ctx, sys, s.Name)
	return exists && !locked, err
}

func (s *DBAccountUnlock) Apply(ctx context.Context, sys system.System) error {
	exists, locked, err := sdbLockState(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("user DB %s không có trong MariaDB, không mở khoá được", s.Name)
	}
	if locked {
		if err := sdbUnlock(ctx, sys, s.Name); err != nil {
			return err
		}
	}
	s.applied, s.unlocked = true, locked
	return nil
}

func (s *DBAccountUnlock) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("mở khoá user DB %s: %w", s.Name, errNotApplied)
	}
	if s.unlocked {
		if err := sdbCheckName(s.Name); err != nil {
			return err
		}
		if err := sdbLock(ctx, sys, s.Name); err != nil {
			return err
		}
	}
	s.applied, s.unlocked = false, false
	return nil
}

// ============================================================== DBUserPassword

// DBUserPassword đổi mật khẩu user 'Name'@'localhost' (ALTER USER ...
// IDENTIFIED BY qua stdin) cho DB rời: không có file cấu hình nào của tpan
// giữ mật khẩu này. Được làm cả khi user đang khoá; sau câu đổi mật khẩu, nếu
// user vốn khoá mà thành mở (không mong, kiểm cho chắc) thì khoá lại.
// Check: chỉ true sau Apply của chính bước. Không có Undo: tpan không bao giờ
// biết mật khẩu cũ.
type DBUserPassword struct {
	Name     string
	Password system.Secret

	applied bool
}

func (s *DBUserPassword) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	if !dbPasswordRe.MatchString(s.Password.Reveal()) {
		return fmt.Errorf("mật khẩu mới của user DB %s phải gồm ít nhất 24 ký tự, chỉ chữ cái và chữ số", s.Name)
	}
	return nil
}

func (s *DBUserPassword) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.password:" + s.Name,
		Title: i18n.T("Đổi mật khẩu user DB "+s.Name, "Change the password of database user "+s.Name),
		Why: i18n.T(
			"Ai có mật khẩu DB thì đọc và sửa được toàn bộ DB. Đổi khi nghi mật khẩu bị lộ. Ứng dụng đang dùng mật khẩu cũ sẽ không đăng nhập được nữa cho tới khi được sửa sang mật khẩu mới (hiện một lần sau khi đổi).",
			"Whoever has the database password can read and change the whole database. Change it when you suspect a leak. Applications using the old password can no longer log in until they are given the new one (shown once after the change).",
		),
		Commands: []string{dbShow(dbAlterUserSQL(s.Name, dbMask))},
		Danger:   true,
		NoUndo: i18n.T(
			"tpan không lưu mật khẩu cũ ở đâu cả nên không đặt lại được.",
			"tpan never stores the old password, so it cannot be put back.",
		),
	}
}

func (s *DBUserPassword) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *DBUserPassword) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	exists, locked, err := sdbLockState(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("user DB %s không có trong MariaDB", s.Name)
	}
	pw := s.Password.Reveal()
	if err := dbRun(ctx, sys, dbAlterUserSQL(s.Name, pw), pw, "đổi mật khẩu user DB "+s.Name); err != nil {
		return err
	}
	s.applied = true
	if locked {
		_, still, err := sdbLockState(ctx, sys, s.Name)
		if err != nil {
			return err
		}
		if !still {
			return sdbLock(ctx, sys, s.Name)
		}
	}
	return nil
}

func (s *DBUserPassword) Undo(context.Context, system.System) error { return nil }

// ================================================================= DBTrashDump

// DBTrashDump xuất DB rời Name ra account.DBTrashDump(Name) trước khi DROP
// (mục 6.12, quyết định 13 ở docs/KE-HOACH.md mục 6.12 ("Đã chốt khi làm M13")). Thư mục /var/lib/tpan/trash/db
// root 0700, file 0600. Check: chỉ true sau Apply của chính bước: một file
// xuất cũ còn sót (lần vào thùng rác trước) không bao giờ được coi là bản xuất
// của lần này, không thì DROP sẽ xoá dữ liệu mới mà chỉ còn bản cũ. Apply ghi
// đè file đó. Undo: xoá file vừa ghi.
type DBTrashDump struct {
	Name string

	applied bool
}

func (s *DBTrashDump) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	if p := account.DBTrashDump(s.Name); checkPath("file xuất", p) != nil || !importPathRe.MatchString(p) {
		return fmt.Errorf("file xuất %q không hợp lệ", p)
	}
	return nil
}

func (s *DBTrashDump) Info() engine.StepInfo {
	d := sdbDump(s.Name)
	return engine.StepInfo{
		ID:    "mariadb.trash-dump:" + s.Name,
		Title: i18n.T("Xuất DB "+s.Name+" ra "+d.Path, "Dump database "+s.Name+" to "+d.Path),
		Why: i18n.T(
			"DB rời vào thùng rác bằng cách xuất ra file rồi xoá DB. File này là thứ duy nhất để khôi phục trong số ngày giữ thùng rác; thiếu nó thì xoá DB là mất dữ liệu. Chỉ root đọc được file.",
			"A standalone database goes to the trash by dumping it to a file and then dropping it. This file is the only way to restore it during the trash retention period; without it dropping the database loses the data. Only root can read the file.",
		),
		Commands: []string{dbShow(measureDBSQL(s.Name)), d.cmd().String(), d.tail().String()},
	}
}

func (s *DBTrashDump) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *DBTrashDump) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	d := sdbDump(s.Name)
	dir := path.Dir(d.Path)
	if err := dumpSpaceOK(ctx, sys, s.Name, dir); err != nil {
		return err
	}
	if err := sys.MkdirAll(sdbTrashParent, 0o755, system.Root); err != nil {
		return fmt.Errorf("tạo thư mục %s: %w", sdbTrashParent, err)
	}
	if err := sys.MkdirAll(dir, 0o700, system.Root); err != nil {
		return fmt.Errorf("tạo thư mục %s: %w", dir, err)
	}
	if err := d.write(ctx, sys); err != nil {
		return err
	}
	s.applied = true
	return nil
}

func (s *DBTrashDump) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("xuất DB %s: %w", s.Name, errNotApplied)
	}
	if err := removeIfExists(sys, account.DBTrashDump(s.Name)); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// ====================================================================== DBDrop

// DBDrop xoá DB rời Name (DROP DATABASE), chỉ khi bản xuất trong thùng rác
// đã trọn vẹn. Check: DB không có. Undo: tạo lại DB với charset cũ rồi nhập
// bản xuất. Quyền GRANT của user DB nằm ở mysql.db, không mất khi xoá DB.
type DBDrop struct {
	Name string

	applied             bool
	oldCharset, oldColl string
}

func (s *DBDrop) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.drop:" + s.Name,
		Title: i18n.T("Xoá DB "+s.Name+" (bản xuất ở thùng rác)", "Drop database "+s.Name+" (dump kept in the trash)"),
		Why: i18n.T(
			"Xoá DB khỏi MariaDB để nó không còn dùng được và không chiếm chỗ; dữ liệu nằm trong file xuất ở thùng rác cho tới khi xoá hẳn. Ứng dụng đang dùng DB này sẽ lỗi.",
			"Drops the database from MariaDB so it can no longer be used and frees its space; the data stays in the dump in the trash until it is purged. Applications using this database will fail.",
		),
		Commands: []string{dbShow(sdbSchemaSQL(s.Name)), dbShow(dbDropDBSQL(s.Name))},
		Danger:   true,
	}
}

func (s *DBDrop) Check(ctx context.Context, sys system.System) (bool, error) {
	_, _, exists, err := DBCharsetOf(ctx, sys, s.Name)
	return !exists, err
}

func (s *DBDrop) Apply(ctx context.Context, sys system.System) error {
	cs, coll, exists, err := DBCharsetOf(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := sdbCheckCharset(cs, coll); err != nil {
		return fmt.Errorf("charset hiện tại của DB %s: %w", s.Name, err)
	}
	// Không có bản xuất trọn vẹn thì không xoá gì: không có đường lùi.
	if err := sdbDumpReady(ctx, sys, s.Name); err != nil {
		return fmt.Errorf("xoá DB %s: %w", s.Name, err)
	}
	if err := dbRun(ctx, sys, dbDropDBSQL(s.Name), "", "xoá DB "+s.Name); err != nil {
		return err
	}
	s.applied, s.oldCharset, s.oldColl = true, cs, coll
	return nil
}

func (s *DBDrop) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("xoá DB %s: %w", s.Name, errNotApplied)
	}
	if s.oldCharset == "" {
		// Apply thấy DB đã không có: không có gì để trả lại.
		s.applied = false
		return nil
	}
	if err := sdbLoad(ctx, sys, s.Name, s.oldCharset, s.oldColl, account.DBTrashDump(s.Name)); err != nil {
		return fmt.Errorf("hoàn tác xoá DB: %w", err)
	}
	s.applied = false
	return nil
}

// =================================================================== DBRestore

// DBRestore tạo lại DB rời Name (charset, collation đã lưu) và nhập bản xuất
// trong thùng rác. DB đã có thì lỗi (không ghi đè DB nào). Check: chỉ true sau
// Apply của chính bước (DB có chưa nói được dữ liệu đã nhập chưa). Undo: xoá DB.
type DBRestore struct {
	Name      string
	Charset   string
	Collation string

	applied bool
}

func (s *DBRestore) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	return sdbCheckCharset(s.Charset, s.Collation)
}

func (s *DBRestore) Info() engine.StepInfo {
	f := account.DBTrashDump(s.Name)
	return engine.StepInfo{
		ID:    "mariadb.restore:" + s.Name,
		Title: i18n.T("Tạo lại DB "+s.Name+" và nhập "+f, "Recreate database "+s.Name+" and import "+f),
		Why: i18n.T(
			"Khôi phục DB rời từ thùng rác: DB được tạo lại với đúng charset cũ rồi nhập bản xuất lúc xoá. Nhập lỗi giữa chừng thì DB vừa tạo bị xoá, file xuất vẫn giữ để thử lại.",
			"Restores the standalone database from the trash: it is recreated with its old charset and the dump taken at deletion is imported. If the import fails midway the new database is dropped and the dump is kept to try again.",
		),
		Commands: []string{dbShow(sdbCreateDBSQL(s.Name, s.Charset, s.Collation)), importCmd(s.Name, f).String()},
	}
}

func (s *DBRestore) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *DBRestore) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	_, _, exists, err := DBCharsetOf(ctx, sys, s.Name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("DB %s đã có trong MariaDB; tpan không nhập đè", s.Name)
	}
	if err := sdbDumpReady(ctx, sys, s.Name); err != nil {
		return fmt.Errorf("khôi phục DB %s: %w", s.Name, err)
	}
	if err := sdbLoad(ctx, sys, s.Name, s.Charset, s.Collation, account.DBTrashDump(s.Name)); err != nil {
		return err
	}
	s.applied = true
	return nil
}

func (s *DBRestore) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("khôi phục DB %s: %w", s.Name, errNotApplied)
	}
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	if err := dbRun(ctx, sys, dbDropDBSQL(s.Name), "", "xoá DB "+s.Name+" vừa khôi phục"); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// =========================================================== DBTrashDumpAbsent

// DBTrashDumpAbsent xoá file xuất của DB rời trong thùng rác: sau khi khôi
// phục (dữ liệu đã nằm lại trong DB) hoặc khi xoá hẳn. Check: file không có.
// Không có Undo.
type DBTrashDumpAbsent struct {
	Name string
	// Restored: bước cuối của khôi phục (chỉ đổi lời giải thích; xoá hẳn thì
	// đây là xoá dữ liệu, Danger).
	Restored bool
}

func (s *DBTrashDumpAbsent) Info() engine.StepInfo {
	f := account.DBTrashDump(s.Name)
	info := engine.StepInfo{
		ID:       "mariadb.trash-dump-absent:" + s.Name,
		Title:    i18n.T("Xoá file xuất "+f, "Delete the dump "+f),
		Commands: []string{"rm " + f},
	}
	if s.Restored {
		info.Why = i18n.T(
			"Dữ liệu đã nằm lại trong DB; để file xuất lại thì một bản sao toàn bộ dữ liệu nằm ở thùng rác mà không ai quản.",
			"The data is back in the database; leaving the dump would keep a full copy of the data in the trash that nobody manages.",
		)
		info.NoUndo = i18n.T("DB vừa khôi phục đã có đúng dữ liệu của file.", "The restored database already holds the file's data.")
		return info
	}
	info.Why = i18n.T(
		"Xoá hẳn DB rời: file xuất là bản dữ liệu cuối cùng còn lại.",
		"Purging the standalone database: the dump is the last remaining copy of the data.",
	)
	info.Danger = true
	info.NoUndo = i18n.T("File đã xoá thì dữ liệu mất hẳn.", "Once the file is deleted the data is gone for good.")
	return info
}

func (s *DBTrashDumpAbsent) Check(_ context.Context, sys system.System) (bool, error) {
	if err := sdbCheckName(s.Name); err != nil {
		return false, err
	}
	p := account.DBTrashDump(s.Name)
	_, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	return false, nil
}

func (s *DBTrashDumpAbsent) Apply(_ context.Context, sys system.System) error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	return removeIfExists(sys, account.DBTrashDump(s.Name))
}

func (s *DBTrashDumpAbsent) Undo(context.Context, system.System) error { return nil }

// ================================================================ DBUserAbsent

// DBUserAbsent xoá user 'Name'@'localhost' (DROP USER IF EXISTS; quyền đã cấp
// mất theo). Check: user không có. Không có Undo: tạo lại cần mật khẩu cũ.
type DBUserAbsent struct{ Name string }

func (s *DBUserAbsent) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.user-absent:" + s.Name,
		Title: i18n.T("Xoá user DB "+s.Name, "Drop the database user "+s.Name),
		Why: i18n.T(
			"Xoá hẳn DB rời thì user DB của nó cũng phải đi: để lại thì một tài khoản không ai quản vẫn nằm trong MariaDB, và tạo lại DB cùng tên sẽ bị từ chối.",
			"Purging a standalone database also removes its user: left behind, an unmanaged account stays in MariaDB and creating a database with the same name is refused.",
		),
		Commands: []string{dbShow(dbDropUserSQL(s.Name))},
		Danger:   true,
		NoUndo:   i18n.T("tpan không giữ mật khẩu nên không tạo lại được user cũ.", "tpan keeps no password, so the old user cannot be recreated."),
	}
}

func (s *DBUserAbsent) Check(ctx context.Context, sys system.System) (bool, error) {
	_, user, err := DBNameState(ctx, sys, s.Name)
	return !user, err
}

func (s *DBUserAbsent) Apply(ctx context.Context, sys system.System) error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	return dbRun(ctx, sys, dbDropUserSQL(s.Name), "", "xoá user DB "+s.Name)
}

func (s *DBUserAbsent) Undo(context.Context, system.System) error { return nil }
