package validate

// Tên của M13 (mục 6.12 của docs/KE-HOACH.md): tên đăng nhập của user TPAN,
// tên gói, phần tên người dùng đặt cho DB rời. Ba thứ này đi vào câu SQL (tên
// DB, user DB) và vào đường dẫn file (bản xuất DB trong thùng rác), nên chỉ nhận
// [a-z0-9_] và kiểm ngay ở đây, giống Domain.

import (
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
)

// Mã lỗi trả ra API cho tên của M13.
const (
	CodeUserNameInvalid    = "user_name_invalid"
	CodeUserNameReserved   = "user_name_reserved"
	CodePackageNameInvalid = "package_name_invalid"
	CodeDBNameInvalid      = "db_name_invalid"
)

// Giới hạn độ dài tên của MariaDB (10.11): tên database 64 ký tự, tên user 80.
const (
	MaxDBNameLen   = 64
	MaxDBUserLen   = 80
	maxUserNameLen = 32
)

var (
	// userNameRe: mục 6.12, `^[a-z][a-z0-9_]{2,31}$`.
	userNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)
	// packageNameRe: như tên user nhưng cho phép 1 ký tự trở lên ("pro", "s1").
	packageNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// dbShortRe: phần tên người dùng tự đặt cho DB rời, ghép sau "<user>_".
	dbShortRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,62}$`)
)

// reservedUserNames là tên không cho đặt cho user TPAN dù đúng dạng. Tên user
// Linux đang có trên máy cũng bị chặn, nhưng phần đó cần đọc /etc/passwd nên
// nằm ở core (mục 6.12), không ở đây.
var reservedUserNames = map[string]bool{
	"root": true, "tpan": true, "mysql": true, "mariadb": true, "daemon": true,
	"nobody": true, "sys": true, "information_schema": true, "performance_schema": true,
}

// UserName là tên đăng nhập của một user TPAN đã kiểm.
type UserName struct{ s string }

// ParseUserName kiểm tên đăng nhập. Ngoài dạng của mục 6.12, chặn thêm tiền tố
// "tp_": DB rời tên "<user>_<tên>" của user "tp_shop" có thể trùng đúng tên DB
// "tp_<slug>" của một site (ví dụ "tp_shop" + "vn_3fa1c2").
func ParseUserName(s string) (UserName, error) {
	if len(s) > maxUserNameLen || !userNameRe.MatchString(s) {
		return UserName{}, &Error{Code: CodeUserNameInvalid, Message: i18n.T(
			"tên đăng nhập phải bắt đầu bằng chữ thường, dài 3 đến 32 ký tự, chỉ gồm a-z, 0-9 và _",
			"the user name must start with a lowercase letter, be 3 to 32 characters long, and use only a-z, 0-9 and _")}
	}
	if reservedUserNames[s] || strings.HasPrefix(s, "tp_") {
		return UserName{}, &Error{Code: CodeUserNameReserved, Message: i18n.T(
			"tên này dành riêng cho hệ thống (root, tpan, mysql..., hoặc bắt đầu bằng tp_); chọn tên khác",
			"this name is reserved for the system (root, tpan, mysql..., or starting with tp_); choose another")}
	}
	return UserName{s: s}, nil
}

func (u UserName) String() string { return u.s }

// IsZero cho biết đây là giá trị zero.
func (u UserName) IsZero() bool { return u.s == "" }

// MarshalText để UserName ra JSON như một chuỗi.
func (u UserName) MarshalText() ([]byte, error) { return []byte(u.s), nil }

// UnmarshalText đọc lại từ JSON, kiểm như ParseUserName. Chuỗi rỗng là zero.
func (u *UserName) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*u = UserName{}
		return nil
	}
	v, err := ParseUserName(string(b))
	if err != nil {
		return err
	}
	*u = v
	return nil
}

// ParsePackageName kiểm tên gói: chữ thường đầu, tối đa 32 ký tự [a-z0-9_].
func ParsePackageName(s string) (string, error) {
	if !packageNameRe.MatchString(s) {
		return "", &Error{Code: CodePackageNameInvalid, Message: i18n.T(
			"tên gói phải bắt đầu bằng chữ thường, tối đa 32 ký tự, chỉ gồm a-z, 0-9 và _",
			"the package name must start with a lowercase letter, be at most 32 characters, and use only a-z, 0-9 and _")}
	}
	return s, nil
}

// StandaloneDBName ghép tên DB rời (cũng là tên user DB của nó) từ chủ và phần
// tên người dùng đặt: "<owner>_<short>" (mục 6.12). Trả lỗi db_name_invalid khi
// phần tên sai dạng hoặc tổng dài quá MaxDBNameLen. owner phải là tên đã kiểm.
func StandaloneDBName(owner UserName, short string) (string, error) {
	bad := &Error{Code: CodeDBNameInvalid, Message: i18n.T(
		"tên DB chỉ gồm a-z, 0-9 và _, bắt đầu bằng chữ hoặc số, và cả \"<user>_<tên>\" không quá 64 ký tự",
		"the database name may use only a-z, 0-9 and _, must start with a letter or digit, and \"<user>_<name>\" must be at most 64 characters")}
	if owner.IsZero() || !dbShortRe.MatchString(short) {
		return "", bad
	}
	full := owner.String() + "_" + short
	if len(full) > MaxDBNameLen {
		return "", bad
	}
	return full, nil
}
