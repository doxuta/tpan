// Package apitoken là phần thuần của token API (M20, docs/thay-doi/M20-thiet-ke.md
// mục 3.1 và 3.7): dạng token có tiền tố và phần kiểm, băm, phạm vi theo nhóm
// tài nguyên. Gói lá: chỉ thư viện chuẩn.
//
// HỢP ĐỒNG ĐỢT 0: chữ ký dưới đây là thứ store, server, cmd dựa vào; thân hàm
// do thợ của đợt 1 viết (cùng test).
package apitoken

import "errors"

var errTodo = errors.New("apitoken: chưa viết (hợp đồng đợt 0)")

// Prefix cho công cụ quét bí mật nhận ra token của TPAN.
const Prefix = "tpan_"

// Len là độ dài của mọi token: 5 (tiền tố) + 32 (20 byte ngẫu nhiên, base32
// chữ thường không đệm) + 7 (crc32 IEEE của 32 ký tự giữa, 4 byte big-endian,
// base32 chữ thường không đệm).
const Len = 44

// New sinh một token mới.
func New() string { panic(errTodo) }

// WellFormed: đúng Len, đúng tiền tố, chỉ a-z2-7 sau tiền tố, phần kiểm khớp.
func WellFormed(t string) bool { panic(errTodo) }

// Hash là sha256 (hex) của token: thứ duy nhất DB giữ.
func Hash(t string) string { panic(errTodo) }

// Hint là "tpan_" + 6 ký tự đầu của phần ngẫu nhiên + "…": nhận ra được, không dùng được.
func Hint(t string) string { panic(errTodo) }

// Group là một nhóm tài nguyên của phạm vi (bảng ở mục 3.7 của thiết kế).
type Group string

const (
	GroupSites     Group = "sites"
	GroupDatabases Group = "databases"
	GroupBackups   Group = "backups"
	GroupCron      Group = "cron"
	GroupJobs      Group = "jobs"
	GroupHealth    Group = "health"
	GroupSSL       Group = "ssl"
	GroupSystem    Group = "system"
)

// Groups là mọi nhóm, theo thứ tự hiện trên giao diện.
var Groups = []Group{GroupSites, GroupDatabases, GroupBackups, GroupCron, GroupJobs, GroupHealth, GroupSSL, GroupSystem}

// AdminOnly: nhóm chỉ admin chọn được khi tạo token.
func AdminOnly(g Group) bool { return g == GroupSystem }

// Scope là một phạm vi: đọc hoặc ghi một nhóm. Dạng chữ "sites:read", "sites:write".
type Scope struct {
	Group Group
	Write bool
}

// Scopes là tập phạm vi của một token, đã bỏ trùng và sắp xếp.
type Scopes []Scope

// ParseScopes đọc danh sách dạng chữ; rỗng, nhóm lạ, quyền lạ là lỗi. Có cả
// đọc lẫn ghi của cùng nhóm thì chỉ giữ ghi.
func ParseScopes(list []string) (Scopes, error) { panic(errTodo) }

// String là dạng lưu trong DB: "backups:read,sites:write".
func (s Scopes) String() string { panic(errTodo) }

// List là dạng chữ từng phạm vi (cho API).
func (s Scopes) List() []string { panic(errTodo) }

// Allows: phạm vi cho phép đọc (write=false) hay ghi nhóm g. Ghi bao gồm đọc.
func (s Scopes) Allows(g Group, write bool) bool { panic(errTodo) }

// GroupsFor là các nhóm người có vai này được chọn khi tạo token (admin: mọi
// nhóm; user thường: mọi nhóm trừ AdminOnly).
func GroupsFor(isAdmin bool) []Group { panic(errTodo) }

// MaxDaysLimit là trần tuyệt đối của auth.token_max_days; DefaultMaxDays là
// giá trị khi chưa đặt.
const (
	MaxDaysLimit   = 365
	DefaultMaxDays = 90
)

// InvalidError là lời từ chối khi tạo token: Code là mã lỗi API
// (token_name_invalid, token_scope_invalid, token_days_invalid), Vi và En là câu nói.
type InvalidError struct{ Code, Vi, En string }

func (e *InvalidError) Error() string { return e.Vi }

// CheckNew là luật chung của mọi đường tạo token (API và `tpan token create`):
// tên 1-64 ký tự, không ký tự điều khiển; ít nhất một phạm vi; user thường
// không chọn được nhóm AdminOnly; 1 ≤ days ≤ maxDays. Sai thì trả *InvalidError.
func CheckNew(isAdmin bool, name string, scopes Scopes, days, maxDays int) error { panic(errTodo) }
