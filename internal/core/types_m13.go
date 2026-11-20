package core

// Hợp đồng chung của M13 (mục 6.12 của docs/KE-HOACH.md): giao diện store cho
// user, gói, DB rời; các struct ca sử dụng; hạn mức. Thân hàm nằm ở:
//
//	users.go    user, gói, hạn mức, tạm khoá user (thợ "core user")
//	suspend.go  tạm khoá site (thợ "tạm khoá site")
//	dbs.go      DB rời, sửa charset, tạm khoá DB (thợ "DB rời")
//
// Quyết định chung (lý do ở docs/thay-doi/M13.md):
//   - core không biết vai. Ai được gọi gì là việc của server (luật của route);
//     core chỉ nhận chủ (owner) đã được server quyết.
//   - Site hay DB đang tạm khoá thì không đưa vào thùng rác được (site_suspended,
//     db_suspended): khôi phục từ thùng rác ghi lại pool và mở khoá user DB, tức
//     là mở khoá lén. Mở khoá trước rồi mới xoá.

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// AccountStore là phần store mà ca sử dụng user và gói cần. *store.Store thoả.
type AccountStore interface {
	User(ctx context.Context, name string) (u account.User, ok bool, err error)
	// Users trả mọi user, sắp theo tên.
	Users(ctx context.Context) ([]account.User, error)
	// CreateUser thêm user mới với hash mật khẩu (argon2id, store.HashPassword).
	CreateUser(ctx context.Context, u account.User, passwordHash string) error
	// UpdateUser ghi đè full_name, email, role, package, status, suspended_at,
	// suspend_reason của user đã có (không đụng hash, không đụng phiên).
	UpdateUser(ctx context.Context, u account.User) error
	// SetUserPassword đổi hash và huỷ mọi phiên của user đó, trong một transaction.
	SetUserPassword(ctx context.Context, name, passwordHash string) error
	// DeleteSessionsOf huỷ mọi phiên của user (kể cả phiên admin đang "xem như" user đó).
	DeleteSessionsOf(ctx context.Context, name string) error
	// DeleteUser xoá dòng user và phiên của họ. Job, audit cũ giữ nguyên tên.
	DeleteUser(ctx context.Context, name string) error
	// RunningJobsOf đếm job đang chạy (status running) có owner hoặc actor là name.
	RunningJobsOf(ctx context.Context, name string) (int, error)

	Package(ctx context.Context, name string) (p account.Package, ok bool, err error)
	Packages(ctx context.Context) ([]account.Package, error)
	// SavePackage thêm hoặc ghi đè gói cùng tên.
	SavePackage(ctx context.Context, p account.Package) error
	DeletePackage(ctx context.Context, name string) error
	// PackageUsers đếm user đang dùng gói.
	PackageUsers(ctx context.Context, name string) (int, error)

	// Usage đếm site (mọi trạng thái, kể cả thùng rác) và DB rời (mọi trạng
	// thái) của user, kèm lần đo dung lượng gần nhất.
	Usage(ctx context.Context, name string) (account.Usage, error)
	// SetDiskUsage ghi lần đo dung lượng của user (thư mục site + DB, byte).
	SetDiskUsage(ctx context.Context, name string, bytes int64, at time.Time) error
}

// DBStore là phần store mà ca sử dụng DB rời cần (bảng databases). *store.Store thoả.
type DBStore interface {
	StandaloneDB(ctx context.Context, name string) (d account.Database, ok bool, err error)
	// StandaloneDBs trả DB rời của owner (rỗng là của mọi người), sắp theo tên.
	StandaloneDBs(ctx context.Context, owner string) ([]account.Database, error)
	// SaveStandaloneDB thêm hoặc ghi đè DB rời cùng tên.
	SaveStandaloneDB(ctx context.Context, d account.Database) error
	DeleteStandaloneDB(ctx context.Context, name string) error
}

// QuotaKind là loại giới hạn của gói được kiểm trước khi tạo mới.
type QuotaKind string

const (
	QuotaSites   QuotaKind = "sites"
	QuotaDBs     QuotaKind = "dbs"
	QuotaBackups QuotaKind = "backups"
)

// Mã từ chối của M13 (RefusedError.Code). Giao diện dịch theo mã.
const (
	CodeQuotaSites       = "quota_sites"
	CodeQuotaDBs         = "quota_dbs"
	CodeQuotaDisk        = "quota_disk"
	CodeQuotaBackups     = "quota_backups"
	CodeUserNotFound     = "user_not_found"
	CodeUserExists       = "user_exists"
	CodeUserSuspended    = "user_suspended"
	CodeUserNotEmpty     = "user_not_empty" // còn site, DB rời hoặc job đang chạy
	CodeLastAdmin        = "last_admin"     // khoá, xoá, hạ vai admin cuối cùng, hoặc tự khoá mình
	CodePackageNotFound  = "package_not_found"
	CodePackageInUse     = "package_in_use"
	CodePackageExists    = "package_exists"
	CodeSiteSuspended    = "site_suspended" // thao tác không làm được trên site tạm khoá
	CodeSiteNotSuspended = "site_not_suspended"
	CodeDBNotFound       = "db_not_found"
	CodeDBExists         = "db_exists"
	CodeDBSuspended      = "db_suspended"
	CodeDBNotSuspended   = "db_not_suspended"
	CodeCollationInvalid = "collation_invalid"
)

// ---------------------------------------------------------------- user, gói

// Accounts gom những gì ca sử dụng user, gói, hạn mức cần (users.go).
type Accounts struct {
	Sys     system.System
	Store   AccountStore
	Sites   *Sites
	DBs     *DBs
	Journal engine.Journal
	Now     func() time.Time
	// Hash băm mật khẩu (server, CLI gắn store.HashPassword) và Generate sinh
	// mật khẩu khi người gọi để trống (store.GeneratePassword). Là trường vì
	// core không import store; nil thì AddUser, SetPassword trả lỗi.
	Hash     func(pw string) (string, error)
	Generate func() string
}

// UserInput là đầu vào của "thêm user".
type UserInput struct {
	Name     validate.UserName
	FullName string
	Email    validate.Email // zero là không có email
	Role     account.Role
	Package  string // rỗng là account.DefaultPackage
	// Password rỗng thì sinh (store.GeneratePassword) và trả về một lần.
	Password string
}

// UserChange là phần sửa của "sửa user"; nil là giữ nguyên.
type UserChange struct {
	FullName *string
	Email    *validate.Email
	Role     *account.Role
	Package  *string
}

// UserSummary là một user cùng gói và phần đã dùng (màn Người dùng, Bảng điều khiển của user).
type UserSummary struct {
	User      account.User
	Limits    account.Package
	Usage     account.Usage
	OverQuota bool
}

// PackageSummary là một gói cùng số user đang dùng.
type PackageSummary struct {
	Package account.Package
	Users   int
}

// ---------------------------------------------------------------- DB rời

// DBs gom những gì ca sử dụng DB rời và thao tác DB của M13 cần (dbs.go).
type DBs struct {
	Sys      system.System
	Settings Settings
	Store    DBStore
	Sites    SiteStore
	// Accounts nil thì không kiểm hạn mức (test, máy chưa có tpan.db).
	Accounts AccountStore
	Journal  engine.Journal
	Now      func() time.Time
}

// DBInput là đầu vào của "tạo DB rời".
type DBInput struct {
	Owner validate.UserName
	// Name là phần tên người dùng đặt; tên thật là validate.StandaloneDBName(Owner, Name).
	Name string
	// Charset, Collation rỗng là utf8mb4 và collation mặc định của nó.
	Charset   string
	Collation string
	Note      string
}

// DBAdd là "tạo DB rời" đã qua điều kiện đầu. Password là mật khẩu của user DB,
// hiện một lần (JSON luôn "••••", người gọi tự Reveal).
type DBAdd struct {
	*Planned
	DB       account.Database
	Password system.Secret
}

// DBPassword là "đổi mật khẩu DB rời" đã qua điều kiện đầu.
type DBPassword struct {
	*Planned
	Name     string
	Password system.Secret
}

// Collation là một cặp charset, collation có trong information_schema.COLLATIONS.
type Collation struct {
	Charset   string
	Collation string
	Default   bool
}
