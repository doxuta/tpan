// Package validate giữ các kiểu đầu vào đã kiểm: Domain, Email và Slug.
//
// Vì sao có gói này: TPAN chạy bằng quyền root, nên một chuỗi lạ lọt vào tên
// file, tên user hay tham số lệnh là có thể thành lỗ hổng chiếm máy (mục 11
// của KE-HOACH.md). Cách chặn là mỗi giá trị từ ngoài vào (API, CLI, MCP, AI)
// phải đi qua một hàm Parse duy nhất để thành một kiểu riêng. Các kiểu này có
// trường không xuất, nên ngoài gói này không ai dựng được một Domain mà bỏ qua
// bước kiểm. Phần còn lại của mã (Step, template, store) chỉ nhận kiểu đã kiểm,
// không nhận string trần.
//
// Gói này là gói lá: chỉ dùng thư viện chuẩn, golang.org/x/net/idna và
// internal/i18n.
package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"net/mail"
	"strings"
	"unicode"

	"golang.org/x/net/idna"

	"github.com/doxuta/tpan/internal/i18n"
)

// Mã lỗi trả ra API (mục 9). Đổi chuỗi này là đổi hợp đồng với giao diện.
const (
	CodeDomainInvalid = "domain_invalid"
	CodeEmailInvalid  = "email_invalid"
)

// Giới hạn theo RFC 1035 và RFC 5321.
const (
	maxDomainLen = 253
	maxLabelLen  = 63
	maxEmailLen  = 254
	maxLocalLen  = 64

	// Chặn sớm đầu vào khổng lồ trước khi đưa vào idna (đỡ tốn CPU).
	// Tên miền 253 ký tự ASCII; có dấu thì mỗi ký tự tối đa 4 byte.
	maxRawLen = 4 * maxDomainLen
)

// Error là lỗi kiểm đầu vào. Code là mã ổn định để API trả về; Message là
// câu ngắn hai ngôn ngữ nói lý do. Message cố ý không chép lại chuỗi người
// dùng gửi lên, vì chuỗi đó có thể chứa xuống dòng hay ký tự điều khiển và
// sẽ làm bẩn log hoặc giao diện.
type Error struct {
	Code    string
	Message i18n.Text
}

// Error trả về dạng "code: lý do" bằng tiếng Việt cho log.
func (e *Error) Error() string { return e.Code + ": " + e.Message.Vi }

func domainErr(vi, en string) error {
	return &Error{Code: CodeDomainInvalid, Message: i18n.T(vi, en)}
}

func emailErr(vi, en string) error {
	return &Error{Code: CodeEmailInvalid, Message: i18n.T(vi, en)}
}

// ---------------------------------------------------------------- Domain

// Domain là tên miền đã kiểm, luôn ở dạng ASCII chữ thường (tên có dấu đã đổi
// sang punycode). Giá trị zero là "chưa có tên miền"; IsZero nhận ra nó.
type Domain struct{ s string }

// ParseDomain là cách duy nhất để có một Domain.
func ParseDomain(s string) (Domain, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return Domain{}, domainErr("tên miền đang để trống", "the domain is empty")
	}
	if len(s) > maxRawLen {
		return Domain{}, domainErr("tên miền quá dài", "the domain is too long")
	}
	if strings.HasSuffix(s, ".") {
		return Domain{}, domainErr(
			"tên miền không được có dấu chấm ở cuối",
			"the domain must not end with a dot")
	}

	// Lọc ký tự trước khi nhờ idna: ASCII chỉ nhận [a-z0-9.-]; ngoài ASCII chỉ
	// nhận chữ, số và dấu ghép (chữ có dấu tiếng Việt). Làm vậy vì idna theo
	// UTS #46 âm thầm xoá một số ký tự vô hình (như U+200B) và đổi dấu chấm
	// toàn chiều rộng thành dấu chấm thường; ta muốn từ chối chúng, không sửa hộ.
	for _, r := range s {
		switch {
		case r < unicode.MaxASCII:
			if !isLowerAlnum(r) && r != '.' && r != '-' {
				return Domain{}, domainErr(
					"tên miền chỉ được chứa chữ, số, dấu gạch ngang và dấu chấm",
					"the domain may only contain letters, digits, hyphens and dots")
			}
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsMark(r):
			// chữ có dấu: để idna đổi sang punycode.
		default:
			return Domain{}, domainErr(
				"tên miền có ký tự không được phép",
				"the domain contains a character that is not allowed")
		}
	}

	// Lookup chuẩn hoá Unicode (NFC), đổi từng nhãn có dấu sang "xn--...",
	// và kiểm punycode hỏng. Không dùng profile Registration vì nó từ chối
	// chữ hoa và chuỗi chưa chuẩn hoá thay vì chuẩn hoá giúp.
	ascii, err := idna.Lookup.ToASCII(s)
	if err != nil {
		return Domain{}, domainErr(
			"tên miền không hợp lệ (không đổi được sang dạng punycode)",
			"the domain is not valid (cannot be converted to punycode)")
	}
	if err := checkASCIIDomain(ascii); err != nil {
		return Domain{}, err
	}
	return Domain{s: ascii}, nil
}

// checkASCIIDomain là lớp kiểm cuối, tự viết, chạy trên kết quả của idna. Ta
// không tin hoàn toàn vào idna cho các luật này vì đây là chỗ cuối cùng trước
// khi tên đi vào đường dẫn file và config.
func checkASCIIDomain(d string) error {
	if len(d) > maxDomainLen {
		return domainErr(
			"tên miền dài quá 253 ký tự",
			"the domain is longer than 253 characters")
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return domainErr(
			"tên miền cần ít nhất hai phần, ví dụ example.com",
			"the domain needs at least two labels, for example example.com")
	}
	for _, l := range labels {
		if l == "" {
			return domainErr(
				"tên miền có phần rỗng (hai dấu chấm liền nhau hoặc dấu chấm ở đầu hoặc cuối)",
				"the domain has an empty label (consecutive dots, or a leading or trailing dot)")
		}
		if len(l) > maxLabelLen {
			return domainErr(
				"mỗi phần của tên miền dài tối đa 63 ký tự",
				"each label of the domain can be at most 63 characters")
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return domainErr(
				"mỗi phần của tên miền không được bắt đầu hay kết thúc bằng dấu gạch ngang",
				"a label of the domain must not start or end with a hyphen")
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !isLowerAlnum(rune(c)) && c != '-' {
				return domainErr(
					"tên miền chỉ được chứa chữ, số, dấu gạch ngang và dấu chấm",
					"the domain may only contain letters, digits, hyphens and dots")
			}
		}
	}
	// Phần cuối toàn chữ số thì không thể là tên miền thật (không TLD nào như
	// vậy); ngược lại nó trông như địa chỉ IPv4, kể cả dạng rút gọn "127.1".
	// Một điều kiện này chặn mọi dạng IP.
	if allDigits(labels[len(labels)-1]) {
		return domainErr(
			"địa chỉ IP không phải là tên miền",
			"an IP address is not a domain name")
	}
	return nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// String trả về tên miền dạng ASCII chữ thường ("" nếu là giá trị zero).
func (d Domain) String() string { return d.s }

// IsZero cho biết đây là giá trị zero (chưa qua ParseDomain).
func (d Domain) IsZero() bool { return d.s == "" }

// MarshalText để Domain ra JSON và TOML dưới dạng chuỗi.
func (d Domain) MarshalText() ([]byte, error) { return []byte(d.s), nil }

// UnmarshalText kiểm như ParseDomain. Nhờ vậy dữ liệu đọc từ JSON, SQLite hay
// cấu hình cũng không thể mang vào một Domain chưa kiểm. Lỗi thì giữ nguyên d.
func (d *Domain) UnmarshalText(b []byte) error {
	v, err := ParseDomain(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// ----------------------------------------------------------------- Email

// Email là địa chỉ email trần đã kiểm: "phần-trước@tên-miền". Phần tên miền
// đã qua ParseDomain; phần trước @ giữ nguyên hoa thường.
type Email struct{ s string }

// ParseEmail là cách duy nhất để có một Email.
func ParseEmail(s string) (Email, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Email{}, emailErr("email đang để trống", "the email is empty")
	}
	if len(s) > maxRawLen {
		return Email{}, emailErr("email quá dài", "the email is too long")
	}

	// net/mail nhận cả "Tên <a@b.com>", chú thích, danh sách và chuỗi trong
	// ngoặc kép. Ta chỉ cần địa chỉ trần, nên loại mọi ký tự mở đường cho các
	// dạng đó (và mọi khoảng trắng, ký tự điều khiển, xuống dòng) trước.
	for _, r := range s {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`<>()[]{},;:"\|&$`+"`'", r) {
			return Email{}, emailErr(
				"email chỉ nhận địa chỉ trần, không có tên hiển thị, khoảng trắng hay ký tự đặc biệt",
				"the email must be a bare address, with no display name, spaces or special characters")
		}
	}

	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return Email{}, emailErr("email không hợp lệ", "the email is not valid")
	}

	at := strings.LastIndexByte(s, '@')
	if at <= 0 {
		return Email{}, emailErr("email thiếu phần trước dấu @", "the email has no part before the @")
	}
	local, host := s[:at], s[at+1:]
	if len(local) > maxLocalLen {
		return Email{}, emailErr(
			"phần trước dấu @ dài tối đa 64 ký tự",
			"the part before the @ can be at most 64 characters")
	}
	for i := 0; i < len(local); i++ {
		if local[i] >= 0x80 {
			return Email{}, emailErr(
				"phần trước dấu @ chỉ nhận ký tự ASCII",
				"the part before the @ may only contain ASCII characters")
		}
	}

	d, err := ParseDomain(host)
	if err != nil {
		return Email{}, emailErr(
			"tên miền sau dấu @ không hợp lệ",
			"the domain after the @ is not valid")
	}
	out := local + "@" + d.String()
	if len(out) > maxEmailLen {
		return Email{}, emailErr("email dài quá 254 ký tự", "the email is longer than 254 characters")
	}
	return Email{s: out}, nil
}

// String trả về địa chỉ đã chuẩn hoá ("" nếu là giá trị zero).
func (e Email) String() string { return e.s }

// IsZero cho biết đây là giá trị zero.
func (e Email) IsZero() bool { return e.s == "" }

// MarshalText để Email ra JSON và TOML dưới dạng chuỗi.
func (e Email) MarshalText() ([]byte, error) { return []byte(e.s), nil }

// UnmarshalText kiểm như ParseEmail. Lỗi thì giữ nguyên e.
func (e *Email) UnmarshalText(b []byte) error {
	v, err := ParseEmail(string(b))
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// ------------------------------------------------------------------ Slug

// Slug là tên ngắn suy ra từ domain (mục 5, "Cô lập từng site"). Tên user
// Linux, tên DB và tên pool PHP đều sinh từ slug chứ không lấy từ người dùng.
// Chỉ gồm [a-z0-9_], tối đa 23 ký tự.
type Slug struct{ s string }

// SlugOf = tối đa 16 ký tự đầu của domain (ký tự ngoài a-z0-9 đổi thành '_')
// + '_' + 6 ký tự hex đầu của sha256(domain).
//
// Phần hash để hai domain có 16 ký tự đầu giống nhau, hoặc chỉ khác nhau ở dấu
// chấm và dấu gạch ngang (a-b.com và a.b.com), vẫn ra slug khác nhau.
// Domain zero cho Slug zero, để một lỗi quên ParseDomain không sinh ra user "tp__...".
func SlugOf(d Domain) Slug {
	if d.IsZero() {
		return Slug{}
	}
	head := d.s
	if len(head) > 16 {
		head = head[:16]
	}
	b := []byte(head)
	for i, c := range b {
		if !isLowerAlnum(rune(c)) {
			b[i] = '_'
		}
	}
	sum := sha256.Sum256([]byte(d.s))
	return Slug{s: string(b) + "_" + hex.EncodeToString(sum[:3])}
}

// String trả về slug ("" nếu là giá trị zero).
func (s Slug) String() string { return s.s }

// IsZero cho biết đây là giá trị zero.
func (s Slug) IsZero() bool { return s.s == "" }

// LinuxUser là tên user Linux của site. Dài tối đa 3+23 = 26 ký tự, dưới giới
// hạn 32 của useradd.
func (s Slug) LinuxUser() string { return "tp_" + s.s }

// DBName là tên database và user MySQL của site. Giới hạn của MySQL cho tên
// user là 32 ký tự (MariaDB 80), nên 26 ký tự là đủ chỗ.
func (s Slug) DBName() string { return "tp_" + s.s }

// MarshalText để Slug ra JSON như một chuỗi (không có nó, struct có trường
// không xuất sẽ ra "{}").
func (s Slug) MarshalText() ([]byte, error) { return []byte(s.s), nil }

// isLowerAlnum: chữ thường a-z hoặc chữ số, tập ký tự duy nhất được phép trong nhãn tên miền.
func isLowerAlnum(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }
