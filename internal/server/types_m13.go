package server

// Hợp đồng API của M13 (mục 6.12 của docs/KE-HOACH.md): user, gói, tạm khoá
// user, site, DB, DB rời, xem như user. Bản TypeScript cùng nghĩa nằm ở
// web/src/api/types.ts (khối "M13"); đổi một bên thì đổi cả bên kia.
//
// Luật truy cập của từng route (public, any, owner:<đối tượng>, admin) ghi ở
// docs/KE-HOACH.md mục 6.12 ("Đã chốt khi làm M13"), mục "Luật của từng route", và trong bảng route của
// routes.go. Mặc định là cấm: route không khai luật thì server từ chối khởi động.
//
// Đường dẫn mới (đổi máy thì nhận ?dry_run=1 và trả engine.Preview; chạy thật
// trả JobStarted, như mọi thao tác khác):
//
//	GET    /api/v1/users                        admin   → []UserRow
//	POST   /api/v1/users                        admin   UserCreate → UserCreated
//	GET    /api/v1/users/{name}                 admin   → UserRow
//	POST   /api/v1/users/{name}                 admin   UserEdit → UserRow
//	DELETE /api/v1/users/{name}                 admin   → 204 (từ chối khi còn site, DB rời, job đang chạy)
//	POST   /api/v1/users/{name}/password        admin   PasswordSet → PasswordSetResult
//	POST   /api/v1/users/{name}/suspend         admin   SuspendRequest, ?dry_run=1
//	POST   /api/v1/users/{name}/unsuspend       admin   ?dry_run=1
//	POST   /api/v1/users/{name}/trash-all       admin   ?dry_run=1 (đưa mọi site và DB rời của user vào thùng rác)
//	POST   /api/v1/users/{name}/view-as         admin   → SessionInfo (phiên hiện tại thành "xem như", chỉ đọc)
//	POST   /api/v1/session/view-as/exit         any     → SessionInfo (thoát "xem như"; chạy được trong phiên chỉ đọc)
//	GET    /api/v1/packages                     admin   → []PackageRow
//	POST   /api/v1/packages                     admin   account.Package → PackageRow
//	POST   /api/v1/packages/{name}              admin   account.Package → PackageRow
//	DELETE /api/v1/packages/{name}              admin   → 204 (từ chối với "default" và gói còn user dùng)
//	POST   /api/v1/sites/{domain}/suspend       admin   SuspendRequest, ?dry_run=1
//	POST   /api/v1/sites/{domain}/unsuspend     admin   ?dry_run=1
//	POST   /api/v1/databases/{domain}/suspend   admin   ?dry_run=1 (DB của site)
//	POST   /api/v1/databases/{domain}/unsuspend admin   ?dry_run=1
//	POST   /api/v1/databases/{domain}/charset   owner:site  DBCharset, ?dry_run=1
//	GET    /api/v1/collations                   any     → []Collation
//	POST   /api/v1/dbs                          any     DBCreate, ?dry_run=1 → JobStarted (mật khẩu ở JobDetail.DBSecret)
//	POST   /api/v1/dbs/{name}                   owner:db  DBEdit, ?dry_run=1 (chỉ đổi ghi chú thì 200 DatabaseRow, không có job)
//	POST   /api/v1/dbs/{name}/password          owner:db  ?dry_run=1 (mật khẩu mới ở JobDetail.DBSecret)
//	POST   /api/v1/dbs/{name}/suspend           admin   ?dry_run=1
//	POST   /api/v1/dbs/{name}/unsuspend         admin   ?dry_run=1
//	DELETE /api/v1/dbs/{name}                   owner:db  ?dry_run=1 (xuất .sql vào thùng rác rồi DROP)
//	POST   /api/v1/dbs/{name}/restore           owner:db  ?dry_run=1
//	DELETE /api/v1/trash/dbs/{name}             owner:db  ?dry_run=1 (xoá hẳn DB rời trong thùng rác)
//
// Đường dẫn cũ đổi nghĩa:
//
//	POST /api/v1/login        nhận thêm "name" (rỗng = admin). User bị khoá: 403 user_suspended.
//	GET  /api/v1/session      thêm "user".
//	GET  /api/v1/overview     user thường: chỉ site của mình, Quota thay số liệu máy.
//	GET  /api/v1/jobs         lọc theo chủ; admin thêm ?user=<tên> để lọc theo người chạy.
//	GET  /api/v1/databases    lọc theo chủ, gồm cả DB rời (DatabaseRow.Kind).
//	GET  /api/v1/trash        lọc theo chủ, thêm TrashList.DBs.
//	POST /api/v1/sites        nhận "owner" (chỉ admin), kiểm hạn mức gói.
//
// Mọi request thay đổi (không phải GET, HEAD) trong phiên "xem như" bị từ chối
// 403 read_only_view, trừ /api/v1/logout và /api/v1/session/view-as/exit.

import (
	"github.com/doxuta/tpan/internal/account"
)

// UserRow là một hàng của màn Người dùng.
type UserRow struct {
	User account.User `json:"user"`
	// Limits là gói của user (đọc lại từ bảng packages).
	Limits account.Package `json:"limits"`
	Usage  account.Usage   `json:"usage"`
	// OverQuota: đang vượt ít nhất một giới hạn (dung lượng đo gần nhất, số
	// site, số DB). Chỉ là cảnh báo; không có gì tự khoá.
	OverQuota bool `json:"over_quota"`
}

// UserCreate là thân của POST /api/v1/users.
type UserCreate struct {
	Name     string       `json:"name"`
	FullName string       `json:"full_name"`
	Email    string       `json:"email"`
	Role     account.Role `json:"role"`
	// Package rỗng là "default".
	Package string `json:"package"`
	// Password rỗng thì tpan sinh (như mật khẩu quản trị) và trả về đúng một
	// lần trong UserCreated.Password. Có thì tối thiểu 12 ký tự.
	Password string `json:"password,omitempty"`
	// CurrentPassword, Code (M20, soát cuối F1): mật khẩu của chính admin đang
	// làm, và mã hai bước khi admin đó đã bật. Việc tạo, nâng quyền hay đặt lại
	// mật khẩu một tài khoản đòi xác nhận lại, để một phiên bị chiếm (chỉ có
	// cookie) không tạo được cửa sau.
	CurrentPassword string `json:"current_password,omitempty"`
	Code            string `json:"code,omitempty"`
}

// UserCreated trả lời POST /api/v1/users.
type UserCreated struct {
	User UserRow `json:"user"`
	// Password chỉ có khi tpan sinh mật khẩu; hiện một lần rồi bỏ.
	Password string `json:"password,omitempty"`
}

// UserEdit là thân của POST /api/v1/users/{name}. Trường nil là giữ nguyên.
// Hạ vai admin cuối cùng xuống user bị từ chối (last_admin).
type UserEdit struct {
	FullName *string       `json:"full_name,omitempty"`
	Email    *string       `json:"email,omitempty"`
	Role     *account.Role `json:"role,omitempty"`
	Package  *string       `json:"package,omitempty"`
	// Bắt buộc khi Role đổi thành admin (M20, F1).
	CurrentPassword string `json:"current_password,omitempty"`
	Code            string `json:"code,omitempty"`
}

// PasswordSet là thân của POST /api/v1/users/{name}/password. Password rỗng
// thì tpan sinh. Mọi phiên của user đó bị huỷ.
type PasswordSet struct {
	Password string `json:"password,omitempty"`
	// CurrentPassword, Code (M20, soát cuối F1): mật khẩu của chính admin đang
	// làm, và mã hai bước khi admin đó đã bật. Việc tạo, nâng quyền hay đặt lại
	// mật khẩu một tài khoản đòi xác nhận lại, để một phiên bị chiếm (chỉ có
	// cookie) không tạo được cửa sau.
	CurrentPassword string `json:"current_password,omitempty"`
	Code            string `json:"code,omitempty"`
}

// PasswordSetResult: Password chỉ có khi tpan sinh; hiện một lần.
type PasswordSetResult struct {
	Password string `json:"password,omitempty"`
}

// SuspendRequest là thân (không bắt buộc) của các lệnh tạm khoá.
type SuspendRequest struct {
	// Reason là lý do ghi lại (user: suspend_reason). Tối đa 200 ký tự.
	Reason string `json:"reason,omitempty"`
}

// PackageRow là một hàng của màn Gói.
type PackageRow struct {
	Package account.Package `json:"package"`
	// Users là số user đang dùng gói này.
	Users int `json:"users"`
}

// Quota là phần "hạn mức gói" trên Bảng điều khiển của user thường.
type Quota struct {
	Package account.Package `json:"package"`
	Usage   account.Usage   `json:"usage"`
}

// Collation là một cặp charset, collation có trong information_schema.COLLATIONS
// của MariaDB (chỉ nhận cặp có trong danh sách này, mục 6.12).
type Collation struct {
	Charset   string `json:"charset"`
	Collation string `json:"collation"`
	// Default: collation mặc định của charset đó.
	Default bool `json:"default"`
}

// DBCreate là thân của POST /api/v1/dbs. Tên DB thật là "<owner>_<name>".
type DBCreate struct {
	// Owner chỉ admin được đặt; rỗng là chính người gọi.
	Owner string `json:"owner,omitempty"`
	Name  string `json:"name"`
	// Charset và Collation rỗng thì dùng utf8mb4 và collation mặc định của nó.
	Charset   string `json:"charset,omitempty"`
	Collation string `json:"collation,omitempty"`
	Note      string `json:"note,omitempty"`
	// Password (M36): mật khẩu tự đặt cho user DB; rỗng thì tpan sinh và trả
	// một lần. Tự đặt thì không trả lại.
	Password string `json:"password,omitempty"`
}

// DBPasswordRequest (M36) là thân tuỳ chọn của POST .../password: Password
// rỗng hay không gửi thân thì tpan sinh mật khẩu như trước.
type DBPasswordRequest struct {
	Password string `json:"password,omitempty"`
}

// DBEdit là thân của POST /api/v1/dbs/{name}. Trường nil là giữ nguyên.
// Đổi charset thì phải gửi cả Collation (và ngược lại).
type DBEdit struct {
	Charset   *string `json:"charset,omitempty"`
	Collation *string `json:"collation,omitempty"`
	Note      *string `json:"note,omitempty"`
}

// DBCharset là thân của POST /api/v1/databases/{domain}/charset.
type DBCharset struct {
	Charset   string `json:"charset"`
	Collation string `json:"collation"`
}

// DBSecret là mật khẩu DB vừa tạo hoặc vừa đổi, trong JobDetail ở lần GET đầu
// tiên sau khi job thành công; hiện một lần rồi bỏ khỏi bộ nhớ (như AdminSecret).
type DBSecret struct {
	DB       string `json:"db"`
	User     string `json:"user"`
	Password string `json:"password"`
}
