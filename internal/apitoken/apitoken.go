// Package apitoken là phần thuần của token API (M20, docs/thay-doi/M20-thiet-ke.md
// mục 3.1 và 3.7): dạng token có tiền tố và phần kiểm, băm, phạm vi theo nhóm
// tài nguyên. Gói lá: chỉ thư viện chuẩn.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Prefix cho công cụ quét bí mật nhận ra token của TPAN.
const Prefix = "tpan_"

// Len là độ dài của mọi token: 5 (tiền tố) + 32 (20 byte ngẫu nhiên, base32
// chữ thường không đệm) + 7 (crc32 IEEE của 32 ký tự giữa, 4 byte big-endian,
// base32 chữ thường không đệm).
const Len = 44

const bodyLen = 32 // 20 byte ngẫu nhiên → 32 ký tự base32

var b32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// checksum là 7 ký tự kiểm của phần giữa. So bằng chuỗi (không giải base32
// rồi so byte): 4 byte = 32 bit chỉ lấp 2 bit của ký tự cuối, giải ngược thì
// vài cách sửa ký tự cuối ra cùng một CRC mà vẫn lọt.
func checksum(body string) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], crc32.ChecksumIEEE([]byte(body)))
	return b32.EncodeToString(b[:])
}

// New sinh một token mới.
func New() string {
	var raw [20]byte
	_, _ = rand.Read(raw[:]) // từ Go 1.24 Read không bao giờ trả lỗi
	body := b32.EncodeToString(raw[:])
	return Prefix + body + checksum(body)
}

// WellFormed: đúng Len, đúng tiền tố, chỉ a-z2-7 sau tiền tố, phần kiểm khớp.
func WellFormed(t string) bool {
	if len(t) != Len || !strings.HasPrefix(t, Prefix) {
		return false
	}
	for i := len(Prefix); i < Len; i++ {
		if c := t[i]; (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	body := t[len(Prefix) : len(Prefix)+bodyLen]
	return t[len(Prefix)+bodyLen:] == checksum(body)
}

// Hash là sha256 (hex) của token: thứ duy nhất DB giữ.
func Hash(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// Hint là "tpan_" + 6 ký tự đầu của phần ngẫu nhiên + "…": nhận ra được, không dùng được.
func Hint(t string) string {
	if len(t) < len(Prefix)+6 {
		return Prefix + "…"
	}
	return Prefix + t[len(Prefix):len(Prefix)+6] + "…"
}

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

func (sc Scope) text() string {
	if sc.Write {
		return string(sc.Group) + ":write"
	}
	return string(sc.Group) + ":read"
}

// Scopes là tập phạm vi của một token, đã bỏ trùng và sắp xếp.
type Scopes []Scope

func scopeErr(item string) *InvalidError {
	return &InvalidError{
		Code: "token_scope_invalid",
		Vi:   fmt.Sprintf("Phạm vi %q không hợp lệ: phải là <nhóm>:read hoặc <nhóm>:write, nhóm là một trong %s.", item, groupList()),
		En:   fmt.Sprintf("Invalid scope %q: must be <group>:read or <group>:write, group one of %s.", item, groupList()),
	}
}

func groupList() string {
	s := make([]string, len(Groups))
	for i, g := range Groups {
		s[i] = string(g)
	}
	return strings.Join(s, ", ")
}

// ParseScopes đọc danh sách dạng chữ; rỗng, nhóm lạ, quyền lạ là lỗi. Có cả
// đọc lẫn ghi của cùng nhóm thì chỉ giữ ghi.
//
// Kết quả sắp theo thứ tự trong Groups (không theo chữ cái): dạng lưu trong DB
// và giao diện cùng một thứ tự, và hai tập bằng nhau luôn ra cùng một chuỗi.
func ParseScopes(list []string) (Scopes, error) {
	if len(list) == 0 {
		return nil, &InvalidError{
			Code: "token_scope_invalid",
			Vi:   "Chọn ít nhất một phạm vi cho token.",
			En:   "Choose at least one scope for the token.",
		}
	}
	write := map[Group]bool{}
	for _, item := range list {
		g, perm, _ := strings.Cut(item, ":")
		if !slices.Contains(Groups, Group(g)) || (perm != "read" && perm != "write") {
			return nil, scopeErr(item)
		}
		write[Group(g)] = write[Group(g)] || perm == "write"
	}
	out := Scopes{}
	for _, g := range Groups {
		if w, ok := write[g]; ok {
			out = append(out, Scope{Group: g, Write: w})
		}
	}
	return out, nil
}

// String là dạng lưu trong DB: "backups:read,sites:write".
func (s Scopes) String() string { return strings.Join(s.List(), ",") }

// List là dạng chữ từng phạm vi (cho API). Tập rỗng ra [] (không phải nil) để
// JSON là [] chứ không phải null.
func (s Scopes) List() []string {
	out := make([]string, len(s))
	for i, sc := range s {
		out[i] = sc.text()
	}
	return out
}

// Allows: phạm vi cho phép đọc (write=false) hay ghi nhóm g. Ghi bao gồm đọc.
func (s Scopes) Allows(g Group, write bool) bool {
	for _, sc := range s {
		if sc.Group == g && (sc.Write || !write) {
			return true
		}
	}
	return false
}

// GroupsFor là các nhóm người có vai này được chọn khi tạo token (admin: mọi
// nhóm; user thường: mọi nhóm trừ AdminOnly). Luôn là mảng mới: người gọi sửa
// nó không làm hỏng Groups.
func GroupsFor(isAdmin bool) []Group {
	out := make([]Group, 0, len(Groups))
	for _, g := range Groups {
		if isAdmin || !AdminOnly(g) {
			out = append(out, g)
		}
	}
	return out
}

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
func CheckNew(isAdmin bool, name string, scopes Scopes, days, maxDays int) error {
	// Tên hiện ở danh sách token và trong nhật ký: ký tự điều khiển (xuống
	// dòng, ESC) làm giả được dòng log hay đổi màu terminal của `tpan token list`.
	if !utf8.ValidString(name) || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 64 ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return &InvalidError{
			Code: "token_name_invalid",
			Vi:   "Tên token phải có 1 đến 64 ký tự, không chỉ toàn khoảng trắng, không có ký tự điều khiển (như xuống dòng).",
			En:   "Token name must be 1 to 64 characters, not only spaces, with no control characters (such as newlines).",
		}
	}
	if len(scopes) == 0 {
		return &InvalidError{
			Code: "token_scope_invalid",
			Vi:   "Chọn ít nhất một phạm vi cho token.",
			En:   "Choose at least one scope for the token.",
		}
	}
	allowed := GroupsFor(isAdmin)
	for _, sc := range scopes {
		if !slices.Contains(Groups, sc.Group) {
			return scopeErr(sc.text())
		}
		// Token không bao giờ làm được việc người tạo không làm được (B4).
		if !slices.Contains(allowed, sc.Group) {
			return &InvalidError{
				Code: "token_scope_invalid",
				Vi:   fmt.Sprintf("Chỉ quản trị viên tạo được token có phạm vi %q.", sc.text()),
				En:   fmt.Sprintf("Only an administrator can create a token with scope %q.", sc.text()),
			}
		}
	}
	// Trần tuyệt đối áp cả khi setting ghi lớn hơn (sửa tay trong DB).
	limit := min(maxDays, MaxDaysLimit)
	if days < 1 || days > limit {
		return &InvalidError{
			Code: "token_days_invalid",
			Vi:   fmt.Sprintf("Thời hạn token phải từ 1 đến %d ngày (trần do quản trị viên đặt).", max(limit, 1)),
			En:   fmt.Sprintf("Token lifetime must be between 1 and %d days (limit set by the administrator).", max(limit, 1)),
		}
	}
	return nil
}
