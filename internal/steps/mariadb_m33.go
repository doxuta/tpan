package steps

// Bước của M33: sao lưu DB rời (docs/thay-doi/M33.md).
//
// Trước M33, DB rời không nằm trong bản sao lưu nào: bản backup của site chỉ
// có mã nguồn và DB của chính site, còn xoá hẳn DB rời khỏi thùng rác là xoá
// luôn file xuất cuối cùng. Ba bước ở đây cho DB rời đúng những gì site đã có:
//
//   - SDBImport: nhập một bản sao lưu vào DB rời đang có (có đường lùi).
//   - SDBLoad: nhập một bản sao lưu vào DB rời vừa tạo lại (DB đã bị xoá hẳn).
//   - SDBKeepDump: lúc xoá hẳn, chuyển file xuất ở thùng rác sang thư mục sao
//     lưu thay vì xoá nó.
//
// Xuất thì dùng lại MariaDBDump (M9): nó nhận mọi tên DB khớp dbNameRe và mọi
// đường dẫn dưới site.BackupRoot.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*SDBImport)(nil)
	_ engine.Step = (*SDBLoad)(nil)
	_ engine.Step = (*SDBKeepDump)(nil)
)

// MeasureStandaloneDB trả cỡ DB rời name (DATA_LENGTH + INDEX_LENGTH), để core
// chặn đầy đĩa lúc dựng Plan như MeasureDB làm cho DB của site.
func MeasureStandaloneDB(ctx context.Context, sys system.System, name string) (uint64, error) {
	return measureDB(ctx, sys, name)
}

// StandaloneTrashDumpComplete cho core biết file xuất của DB rời name trong
// thùng rác có phải bản xuất trọn vẹn không. File dở (hoặc không phải bản
// xuất) thì không đáng giữ làm bản sao lưu: xoá hẳn xoá nó như trước M33.
func StandaloneTrashDumpComplete(ctx context.Context, sys system.System, name string) (bool, error) {
	if err := sdbCheckName(name); err != nil {
		return false, err
	}
	return sdbDump(name).complete(ctx, sys)
}

// sdbBackupPathOK: file sao lưu của DB rời phải nằm dưới site.BackupRoot (không
// nhận site.RestoreRoot như importPathOK: DB rời không có thư mục khôi phục).
func sdbBackupPathOK(what, p string) error {
	if err := checkPath(what, p); err != nil {
		return err
	}
	if !importPathRe.MatchString(p) {
		return fmt.Errorf("%s %q có ký tự không cho phép", what, p)
	}
	if !strings.HasPrefix(p, site.BackupRoot+"/") {
		return fmt.Errorf("%s %q không nằm dưới %s", what, p, site.BackupRoot)
	}
	return nil
}

// sdbFileReady: p là bản xuất trọn vẹn (file thường, dòng cuối "-- Dump completed").
func sdbFileReady(ctx context.Context, sys system.System, name, p string) error {
	ok, err := (&MariaDBDump{Path: p, DB: name}).complete(ctx, sys)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s không có hoặc không phải bản xuất trọn vẹn (thiếu dòng %q)", p, dumpDone)
	}
	return nil
}

// =================================================================== SDBImport

// SDBImport nhập File vào DB rời Name đang có, sao cho sau đó DB chỉ có đúng
// các bảng của File: xoá DB, tạo lại với Charset và Collation đang lưu, rồi
// nhập. User DB và quyền của nó không mất khi xoá DB (mysql.global_priv,
// mysql.db), nên ứng dụng vẫn đăng nhập được bằng mật khẩu cũ.
//
// Apply kiểm cả File lẫn UndoFile (bản xuất ngay trước bước này) là bản xuất
// trọn vẹn TRƯỚC khi xoá DB; nhập File lỗi thì tự nhập lại UndoFile rồi mới
// báo lỗi (engine không Undo bước có Apply lỗi). Check: chỉ true sau Apply của
// chính bước. Undo: nhập lại UndoFile.
type SDBImport struct {
	Name      string
	Charset   string
	Collation string
	File      string
	UndoFile  string

	applied bool
}

func (s *SDBImport) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	if err := sdbCheckCharset(s.Charset, s.Collation); err != nil {
		return err
	}
	if err := sdbBackupPathOK("file nhập", s.File); err != nil {
		return err
	}
	if err := sdbBackupPathOK("file hoàn tác", s.UndoFile); err != nil {
		return err
	}
	if s.File == s.UndoFile {
		return fmt.Errorf("file nhập và file hoàn tác trùng nhau (%s)", s.File)
	}
	return nil
}

func (s *SDBImport) load(ctx context.Context, sys system.System, file string) error {
	if err := dbRun(ctx, sys, dbDropDBSQL(s.Name), "", "xoá DB "+s.Name+" trước khi nhập"); err != nil {
		return err
	}
	return sdbLoad(ctx, sys, s.Name, s.Charset, s.Collation, file)
}

func (s *SDBImport) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.sdb-import:" + s.Name,
		Title: i18n.T("Nhập "+s.File+" vào DB "+s.Name, "Import "+s.File+" into database "+s.Name),
		Why: i18n.T(
			"Đưa DB về đúng nội dung của bản sao lưu. DB được xoá và tạo lại trước (cùng charset), để không còn bảng nào ngoài các bảng trong file; user DB và mật khẩu giữ nguyên. Hoàn tác là nhập lại "+s.UndoFile+", bản xuất ngay trước bước này.",
			"Returns the database to exactly the contents of the backup. The database is dropped and recreated first (same charset), so no table outside the file is left behind; the database user and its password are kept. Undo imports "+s.UndoFile+", the dump taken just before this step.",
		),
		Commands: []string{
			dbShow(dbDropDBSQL(s.Name) + sdbCreateDBSQL(s.Name, s.Charset, s.Collation)),
			importCmd(s.Name, s.File).String(),
		},
		Danger: true,
	}
}

func (s *SDBImport) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *SDBImport) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	// Kiểm cả hai file TRƯỚC khi xoá DB: không có đường lùi thì không xoá gì.
	for _, p := range []string{s.UndoFile, s.File} {
		if err := sdbFileReady(ctx, sys, s.Name, p); err != nil {
			return fmt.Errorf("nhập DB %s: %w", s.Name, err)
		}
	}
	if err := s.load(ctx, sys, s.File); err != nil {
		if rerr := s.load(ctx, sys, s.UndoFile); rerr != nil {
			return errors.Join(err, fmt.Errorf("trả DB về %s: %w", s.UndoFile, rerr))
		}
		return err
	}
	s.applied = true
	return nil
}

func (s *SDBImport) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("nhập DB %s: %w", s.Name, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.load(ctx, sys, s.UndoFile); err != nil {
		return fmt.Errorf("hoàn tác nhập DB: %w", err)
	}
	s.applied = false
	return nil
}

// ===================================================================== SDBLoad

// sdbTablesSQL đếm bảng (cả view) của DB. So bằng "=", không LIKE.
func sdbTablesSQL(name string) string {
	return "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='" + name + "';\n"
}

// SDBLoad nhập File vào DB rời Name vừa được StandaloneDB tạo (tạo lại DB đã
// xoá hẳn từ bản sao lưu cuối). DB phải đang có và chưa có bảng nào: bước này
// không bao giờ nhập đè lên dữ liệu. Check: chỉ true sau Apply của chính
// bước. Undo không làm gì: bước StandaloneDB đứng trước xoá cả DB khi hoàn tác.
type SDBLoad struct {
	Name string
	File string

	applied bool
}

func (s *SDBLoad) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	return sdbBackupPathOK("file nhập", s.File)
}

func (s *SDBLoad) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.sdb-load:" + s.Name,
		Title: i18n.T("Nhập "+s.File+" vào DB "+s.Name+" vừa tạo", "Import "+s.File+" into the new database "+s.Name),
		Why: i18n.T(
			"DB vừa tạo lại còn trống; bước này đưa dữ liệu của bản sao lưu vào. Nhập lỗi thì cả DB lẫn user DB vừa tạo bị gỡ, bản sao lưu vẫn còn để thử lại.",
			"The recreated database is still empty; this step loads the backup's data into it. If the import fails the new database and its user are removed, and the backup is kept to try again.",
		),
		Commands: []string{dbShow(sdbTablesSQL(s.Name)), importCmd(s.Name, s.File).String()},
		NoUndo: i18n.T(
			"Hoàn tác do bước tạo DB đứng trước làm: nó xoá cả DB vừa tạo.",
			"Undo is done by the preceding create step: it drops the new database.",
		),
	}
}

func (s *SDBLoad) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *SDBLoad) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := sdbFileReady(ctx, sys, s.Name, s.File); err != nil {
		return fmt.Errorf("nhập DB %s: %w", s.Name, err)
	}
	if _, _, exists, err := DBCharsetOf(ctx, sys, s.Name); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("nhập DB %s: DB chưa có (bước tạo DB phải chạy trước)", s.Name)
	}
	res, err := sys.Run(ctx, mariadbCmd(sdbTablesSQL(s.Name)))
	if err != nil {
		return withStdout(fmt.Errorf("đếm bảng của DB %s: %w", s.Name, err), res)
	}
	if n := strings.TrimSpace(res.Stdout); n != "0" {
		return fmt.Errorf("nhập DB %s: DB đã có %s bảng; tpan không nhập đè lên dữ liệu đang có", s.Name, n)
	}
	if res, err := sys.Run(ctx, importCmd(s.Name, s.File)); err != nil {
		return withStdout(fmt.Errorf("nhập %s vào DB %s: %w", s.File, s.Name, err), res)
	}
	s.applied = true
	return nil
}

func (s *SDBLoad) Undo(context.Context, system.System) error {
	s.applied = false
	return nil
}

// ================================================================= SDBKeepDump

// SDBKeepDump chuyển file xuất của DB rời Name từ thùng rác
// (account.DBTrashDump) sang To trong thư mục sao lưu, lúc xoá hẳn DB. File đó
// là bản dữ liệu cuối cùng; trước M33 bước xoá hẳn xoá luôn nó.
//
// Check: To là bản xuất trọn vẹn VÀ file trong thùng rác không còn. Apply: file
// trong thùng rác phải là bản xuất trọn vẹn; tạo thư mục của To (root 0700),
// đổi tên file sang (khác phân vùng thì `mv`), root 0600. Undo: chuyển về.
type SDBKeepDump struct {
	Name string
	To   string

	applied bool
}

func (s *SDBKeepDump) validate() error {
	if err := sdbCheckName(s.Name); err != nil {
		return err
	}
	return sdbBackupPathOK("file sao lưu", s.To)
}

func (s *SDBKeepDump) Info() engine.StepInfo {
	from := account.DBTrashDump(s.Name)
	return engine.StepInfo{
		ID:    "mariadb.sdb-keep-dump:" + s.Name,
		Title: i18n.T("Giữ bản xuất cuối của DB "+s.Name+" làm bản sao lưu", "Keep the last dump of database "+s.Name+" as a backup"),
		Why: i18n.T(
			"Xoá hẳn DB rời thì file xuất ở thùng rác là bản dữ liệu cuối cùng. Thay vì xoá, tpan chuyển nó sang thư mục sao lưu: DB vẫn tạo lại được từ màn Backup cho tới khi chính bạn xoá bản này.",
			"When a standalone database is purged, the dump in the trash is the last copy of its data. Instead of deleting it, tpan moves it to the backup directory: the database can still be recreated from the Backup screen until you delete this copy yourself.",
		),
		Commands: []string{"mkdir -p -m 700 " + path.Dir(s.To), "mv " + from + " " + s.To},
	}
}

func (s *SDBKeepDump) moved(ctx context.Context, sys system.System) (bool, error) {
	if _, err := sys.Stat(account.DBTrashDump(s.Name)); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("kiểm %s: %w", account.DBTrashDump(s.Name), err)
	}
	return (&MariaDBDump{Path: s.To, DB: s.Name}).complete(ctx, sys)
}

func (s *SDBKeepDump) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.moved(ctx, sys)
}

// sdbMove đổi tên from thành to; khác phân vùng (thư mục sao lưu là một đĩa
// gắn riêng) thì Rename lỗi và `mv` chép rồi xoá.
func sdbMove(ctx context.Context, sys system.System, from, to string) error {
	rerr := sys.Rename(from, to)
	if rerr == nil {
		return nil
	}
	if res, err := sys.Run(ctx, system.Cmd{Name: "mv", Args: []string{"--", from, to}}); err != nil {
		return withStdout(fmt.Errorf("chuyển %s sang %s: %w (đổi tên: %v)", from, to, err, rerr), res)
	}
	return nil
}

func (s *SDBKeepDump) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := sdbDumpReady(ctx, sys, s.Name); err != nil {
		return fmt.Errorf("giữ bản xuất cuối của DB %s: %w", s.Name, err)
	}
	if _, err := sys.Stat(s.To); err == nil {
		return fmt.Errorf("giữ bản xuất cuối của DB %s: %s đã có, không ghi đè", s.Name, s.To)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("kiểm %s: %w", s.To, err)
	}
	if err := backupMkdirs(sys, site.BackupRoot, path.Dir(s.To)); err != nil {
		return err
	}
	if err := sdbMove(ctx, sys, account.DBTrashDump(s.Name), s.To); err != nil {
		return err
	}
	if err := setPermOwner(sys, s.To, 0o600, system.Root); err != nil {
		return err
	}
	s.applied = true
	return nil
}

func (s *SDBKeepDump) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("giữ bản xuất cuối của DB %s: %w", s.Name, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := sdbMove(ctx, sys, s.To, account.DBTrashDump(s.Name)); err != nil {
		return fmt.Errorf("hoàn tác: %w", err)
	}
	s.applied = false
	return nil
}
