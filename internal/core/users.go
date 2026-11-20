package core

// User, gói, hạn mức, tạm khoá user của M13 (mục 6.12 của docs/KE-HOACH.md).
// Đợt 0 chỉ có chữ ký; thân hàm do thợ "core user" viết ở đợt 1.

import (
	"context"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/validate"
)

// AddUser thêm user (kiểm tên trùng, tên user Linux có sẵn, gói có thật, vai
// hợp lệ). generated là mật khẩu tpan sinh khi in.Password rỗng, hiện một lần.
func (a *Accounts) AddUser(ctx context.Context, in UserInput) (u account.User, generated string, err error) {
	return account.User{}, "", errM13NotYet
}

// EditUser sửa tên đầy đủ, email, vai, gói. by là người sửa: hạ vai admin cuối
// cùng (kể cả tự hạ mình khi là admin cuối) thì từ chối last_admin.
func (a *Accounts) EditUser(ctx context.Context, name string, ch UserChange, by account.Identity) (account.User, error) {
	return account.User{}, errM13NotYet
}

// SetPassword đặt mật khẩu mới (pw rỗng thì sinh, trả về một lần) và huỷ mọi
// phiên của user. Mật khẩu tối thiểu 12 ký tự như đổi mật khẩu quản trị.
func (a *Accounts) SetPassword(ctx context.Context, name, pw string) (generated string, err error) {
	return "", errM13NotYet
}

// PrepareSuspendUser dựng Plan khoá user: tạm khoá mọi site (Sites.suspendSteps)
// và DB rời (dbLockSteps) đang chưa khoá của user. Sau job: đánh dấu từng site,
// DB là SuspendByUser, user thành Suspended, huỷ mọi phiên. Từ chối khoá chính
// mình (by.Name) và admin cuối cùng (last_admin).
func (a *Accounts) PrepareSuspendUser(ctx context.Context, name, reason string, by account.Identity) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareUnsuspendUser mở khoá user và mở lại đúng những site, DB rời có
// Suspended == SuspendByUser; thứ bị khoá riêng (SuspendSelf) giữ khoá.
func (a *Accounts) PrepareUnsuspendUser(ctx context.Context, name string) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareTrashAll đưa mọi site active và DB rời active của user vào thùng rác
// (nút trước khi xoá user). Từ chối khi có site hay DB đang tạm khoá.
func (a *Accounts) PrepareTrashAll(ctx context.Context, name string) (*Planned, error) {
	return nil, errM13NotYet
}

// DeleteUser xoá user. Từ chối khi còn site (kể cả thùng rác), DB rời, hoặc
// job đang chạy (user_not_empty); khi là admin cuối cùng hoặc chính by (last_admin).
func (a *Accounts) DeleteUser(ctx context.Context, name string, by account.Identity) error {
	return errM13NotYet
}

// Summary là một user kèm gói và phần đã dùng.
func (a *Accounts) Summary(ctx context.Context, name string) (UserSummary, error) {
	return UserSummary{}, errM13NotYet
}

// Summaries là Summary của mọi user, sắp theo tên.
func (a *Accounts) Summaries(ctx context.Context) ([]UserSummary, error) {
	return nil, errM13NotYet
}

// AddPackage thêm gói mới (package_exists nếu trùng tên).
func (a *Accounts) AddPackage(ctx context.Context, p account.Package) error { return errM13NotYet }

// EditPackage sửa giới hạn của gói đã có. Hạ giới hạn xuống dưới phần user
// đang dùng vẫn được: chỉ chặn tạo mới, không xoá gì.
func (a *Accounts) EditPackage(ctx context.Context, p account.Package) error { return errM13NotYet }

// DeletePackage xoá gói; từ chối với "default" và gói còn user dùng.
func (a *Accounts) DeletePackage(ctx context.Context, name string) error { return errM13NotYet }

// Packages là mọi gói kèm số user.
func (a *Accounts) Packages(ctx context.Context) ([]PackageSummary, error) { return nil, errM13NotYet }

// MeasureDisk đo dung lượng của mọi user (du thư mục site + kích thước DB của
// site và DB rời) rồi ghi SetDiskUsage. Chạy trong phép kiểm nặng mỗi giờ.
func (a *Accounts) MeasureDisk(ctx context.Context) error { return errM13NotYet }

// checkQuota kiểm hạn mức gói của owner trước khi tạo mới một thứ loại k
// (cho QuotaBackups thì d là site sẽ backup). Vượt thì *RefusedError
// (CodeQuota*); dung lượng đo gần nhất đã vượt disk_mb cũng chặn mọi loại tạo
// mới (CodeQuotaDisk). as nil thì luôn qua. Dùng ở Sites.PrepareAdd,
// DBs.PrepareAdd, Backups.PrepareCreate, Sites.PrepareApply.
func checkQuota(ctx context.Context, as AccountStore, owner string, k QuotaKind, d validate.Domain) error {
	return nil
}
