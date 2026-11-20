package core

// DB rời, sửa charset, tạm khoá DB của M13 (mục 6.12 của docs/KE-HOACH.md).
// Đợt 0 chỉ có chữ ký; thân hàm do thợ "DB rời" viết ở đợt 1.

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/validate"
)

// PrepareAdd dựng Plan tạo DB rời "<owner>_<tên>" cùng user DB trùng tên,
// mật khẩu 32 ký tự ngẫu nhiên. Kiểm hạn mức (QuotaDBs), tên trùng (kể cả DB
// đã có trong MariaDB mà không có trong bảng), cặp charset và collation.
func (d *DBs) PrepareAdd(ctx context.Context, in DBInput) (*DBAdd, error) {
	return nil, errM13NotYet
}

// PrepareCharset đổi charset và collation của DB rời (ALTER DATABASE).
func (d *DBs) PrepareCharset(ctx context.Context, name, charset, collation string) (*Planned, error) {
	return nil, errM13NotYet
}

// SetNote ghi chú cho DB rời; không đụng máy nên không có Plan.
func (d *DBs) SetNote(ctx context.Context, name, note string) error { return errM13NotYet }

// PreparePassword đổi mật khẩu user DB của DB rời (chỉ trong MariaDB).
func (d *DBs) PreparePassword(ctx context.Context, name string) (*DBPassword, error) {
	return nil, errM13NotYet
}

// PrepareSuspend khoá DB rời: ACCOUNT LOCK rồi KILL các kết nối đang mở của user DB.
func (d *DBs) PrepareSuspend(ctx context.Context, name string) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareUnsuspend mở khoá DB rời (ACCOUNT UNLOCK).
func (d *DBs) PrepareUnsuspend(ctx context.Context, name string) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareTrash xuất DB rời ra account.DBTrashDump(name), DROP DATABASE, khoá
// user DB; status trashed, purge_at theo trash.days. Từ chối DB đang khoá.
func (d *DBs) PrepareTrash(ctx context.Context, name string) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareRestore tạo lại DB, nhập bản xuất trong thùng rác, mở khoá user DB.
func (d *DBs) PrepareRestore(ctx context.Context, name string) (*Planned, error) {
	return nil, errM13NotYet
}

// PreparePurge xoá hẳn DB rời trong thùng rác: DROP USER, xoá file xuất, xoá dòng.
func (d *DBs) PreparePurge(ctx context.Context, name string) (*Planned, error) {
	return nil, errM13NotYet
}

// PurgeExpired xoá hẳn mọi DB rời trong thùng rác đã quá purge_at; trả tên đã xoá.
func (d *DBs) PurgeExpired(ctx context.Context, now time.Time, onEvent func(engine.Event)) ([]string, error) {
	return nil, errM13NotYet
}

// PrepareSiteDBSuspend khoá user DB của site (DBSuspended = SuspendSelf).
func (d *DBs) PrepareSiteDBSuspend(ctx context.Context, dom validate.Domain) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareSiteDBUnsuspend mở khoá user DB của site.
func (d *DBs) PrepareSiteDBUnsuspend(ctx context.Context, dom validate.Domain) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareSiteDBCharset đổi charset và collation của DB của site.
func (d *DBs) PrepareSiteDBCharset(ctx context.Context, dom validate.Domain, charset, collation string) (*Planned, error) {
	return nil, errM13NotYet
}

// Collations là các cặp charset, collation MariaDB đang có.
func (d *DBs) Collations(ctx context.Context) ([]Collation, error) { return nil, errM13NotYet }

// List là DB của site và DB rời của owner (rỗng là của mọi người), cho màn
// Cơ sở dữ liệu. Thay cho Sites.Databases ở server.
func (d *DBs) List(ctx context.Context, owner string) ([]Database, error) {
	return nil, errM13NotYet
}

// dbLockSteps là các bước khoá user DB user@localhost (ACCOUNT LOCK, KILL kết
// nối đang mở), dùng chung cho DB rời, DB của site và khoá user.
func dbLockSteps(user string) []engine.Step { return nil }

// dbUnlockSteps là các bước mở khoá user DB.
func dbUnlockSteps(user string) []engine.Step { return nil }
