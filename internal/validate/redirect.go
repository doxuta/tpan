package validate

// Luật chuyển hướng của M19 (docs/thay-doi/M19-thiet-ke.md mục 3.3): đường dẫn
// nguồn và đích đã kiểm theo danh sách ký tự cho phép. Hai kiểu có trường ẩn
// nên chỉ ParseRedirectSource / ParseRedirectTarget tạo được giá trị khác zero.
//
// Vì sao danh sách cho phép chứ không danh sách cấm: nguồn và đích được chèn
// thẳng vào vhost nginx (location =, return) và Apache (RewriteCond, RewriteRule)
// mà không trích dẫn. Mọi ký tự có nghĩa trong hai cú pháp đó (; { } $ # " ' \ %
// khoảng trắng, xuống dòng) nằm ngoài [A-Za-z0-9._~-] cộng / : ? = &, nên không
// chuỗi nào được nhận lại thêm được chỉ thị vào cấu hình.

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/doxuta/tpan/internal/i18n"
)

const (
	// CodeRedirectInvalid: nguồn hoặc đích của luật chuyển hướng không hợp lệ.
	CodeRedirectInvalid = "redirect_invalid"
	// CodeRedirectReserved: nguồn là "/" hoặc "/index.php" (S11).
	CodeRedirectReserved = "redirect_reserved"
)

// Trường hỏng, trong RedirectError.Field (API trả "field").
const (
	FieldSource = "source"
	FieldTarget = "target"
)

const (
	maxRedirectPath   = 255
	maxRedirectQuery  = 255
	maxRedirectTarget = 1024
)

// RedirectError là lỗi của ParseRedirectSource / ParseRedirectTarget: Err mang
// mã và lời như mọi lỗi kiểm đầu vào, Field cho biết trường hỏng ("source" |
// "target"). Unwrap trả Err, nên errors.As(err, &*validate.Error) vẫn thấy mã
// (server trả 400); muốn trường thì errors.As sang *RedirectError.
type RedirectError struct {
	Field string
	Err   *Error
}

func (e *RedirectError) Error() string { return e.Field + ": " + e.Err.Error() }

// Unwrap để errors.As tìm được *Error bên trong.
func (e *RedirectError) Unwrap() error { return e.Err }

func redirectErr(field, code, vi, en string) error {
	return &RedirectError{Field: field, Err: &Error{Code: code, Message: i18n.T(vi, en)}}
}

func sourceErr() error {
	return redirectErr(FieldSource, CodeRedirectInvalid,
		"nguồn phải là đường dẫn bắt đầu bằng /, chỉ gồm chữ, số và . _ ~ -, không đoạn nào bắt đầu bằng dấu chấm, không có // hay ?, dài tối đa 255 ký tự",
		"the source must be a path starting with /, made of letters, digits and . _ ~ -, with no segment starting with a dot, no // or ?, at most 255 characters")
}

func targetErr() error {
	return redirectErr(FieldTarget, CodeRedirectInvalid,
		"đích phải là đường dẫn bắt đầu bằng / hoặc URL http:// hay https:// tới một tên miền (có thể kèm cổng); chỉ gồm chữ, số và . _ ~ -, query thêm = và &; không đoạn nào bắt đầu bằng dấu chấm; dài tối đa 1024 ký tự",
		"the target must be a path starting with / or an http:// or https:// URL to a domain name (a port is allowed); only letters, digits and . _ ~ -, plus = and & in the query; no segment starting with a dot; at most 1024 characters")
}

// RedirectSource là đường dẫn nguồn đã kiểm. Chỉ ParseRedirectSource tạo được.
type RedirectSource struct{ s string }

// RedirectTarget là đích đã kiểm: đường dẫn trên cùng site, hoặc URL http(s).
type RedirectTarget struct{ s string }

// ParseRedirectSource kiểm đường dẫn nguồn (mục 3.3): "/" + các đoạn nối bằng
// "/", được một "/" cuối, không query. "/" và "/index.php" bị giữ lại (S11):
// nginx đổi mọi permalink về /index.php (try_files), luật trên đó chuyển hướng
// cả site.
func ParseRedirectSource(s string) (RedirectSource, error) {
	if !isRedirectPath(s) {
		return RedirectSource{}, sourceErr()
	}
	if s == "/" || s == "/index.php" {
		return RedirectSource{}, redirectErr(FieldSource, CodeRedirectReserved,
			"không đặt được luật cho / hay /index.php: mọi trang của WordPress đi qua hai đường dẫn này",
			"/ and /index.php cannot be redirected: every WordPress page goes through them")
	}
	return RedirectSource{s: s}, nil
}

// ParseRedirectTarget kiểm đích (mục 3.3). Đích dạng đường dẫn giữ nguyên; đích
// dạng URL được dựng lại từ các phần đã kiểm, host ở dạng ParseDomain (chữ
// thường, punycode), nên String() luôn đọc lại được chính nó.
func ParseRedirectTarget(s string) (RedirectTarget, error) {
	if len(s) > maxRedirectTarget {
		return RedirectTarget{}, targetErr()
	}
	if strings.HasPrefix(s, "/") {
		path, query, hasQ := strings.Cut(s, "?")
		if !isRedirectPath(path) || hasQ && !isRedirectQuery(query) {
			return RedirectTarget{}, targetErr()
		}
		return RedirectTarget{s: s}, nil
	}
	var scheme string
	switch {
	case strings.HasPrefix(s, "http://"):
		scheme = "http://"
	case strings.HasPrefix(s, "https://"):
		scheme = "https://"
	default:
		return RedirectTarget{}, targetErr()
	}
	rest := s[len(scheme):]
	end := strings.IndexAny(rest, "/?:")
	if end < 0 {
		end = len(rest)
	}
	host := rest[:end]
	rest = rest[end:]
	// ParseDomain tự TrimSpace và ToLower: chặn khoảng trắng ở đây để nó không
	// lặng lẽ bỏ đi. Ký tự còn lại (@, %, \, điều khiển...) ParseDomain từ chối.
	if host == "" || strings.IndexFunc(host, unicode.IsSpace) >= 0 {
		return RedirectTarget{}, targetErr()
	}
	d, err := ParseDomain(host)
	if err != nil {
		return RedirectTarget{}, targetErr()
	}
	out := scheme + d.String()
	if strings.HasPrefix(rest, ":") {
		end := strings.IndexAny(rest, "/?")
		if end < 0 {
			end = len(rest)
		}
		if !isPort(rest[1:end]) {
			return RedirectTarget{}, targetErr()
		}
		out += rest[:end]
		rest = rest[end:]
	}
	path, query, hasQ := strings.Cut(rest, "?")
	if path != "" && !isRedirectPath(path) || hasQ && !isRedirectQuery(query) {
		return RedirectTarget{}, targetErr()
	}
	out += rest
	if len(out) > maxRedirectTarget {
		return RedirectTarget{}, targetErr()
	}
	return RedirectTarget{s: out}, nil
}

// isRedirectPath: "/" hoặc "/" + đoạn ("/" đoạn)*, được một "/" cuối, tối đa
// 255 byte. Đoạn rỗng (tức "//") bị từ chối.
func isRedirectPath(p string) bool {
	if p == "" || p[0] != '/' || len(p) > maxRedirectPath {
		return false
	}
	if p == "/" {
		return true
	}
	for _, seg := range strings.Split(strings.TrimSuffix(p[1:], "/"), "/") {
		if !isRedirectSegment(seg) {
			return false
		}
	}
	return true
}

// isRedirectSegment: [A-Za-z0-9._~-]+, không bắt đầu bằng "." (chặn ".", "..",
// ".well-known", ".env": luật người dùng không che được thử thách ACME hay luật
// chặn file ẩn, B8).
func isRedirectSegment(seg string) bool {
	if seg == "" || seg[0] == '.' {
		return false
	}
	for i := 0; i < len(seg); i++ {
		if !isRedirectChar(seg[i]) {
			return false
		}
	}
	return true
}

// isRedirectQuery: [A-Za-z0-9._~=&-]{1,255}.
func isRedirectQuery(q string) bool {
	if q == "" || len(q) > maxRedirectQuery {
		return false
	}
	for i := 0; i < len(q); i++ {
		if c := q[i]; !isRedirectChar(c) && c != '=' && c != '&' {
			return false
		}
	}
	return true
}

func isRedirectChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '~' || c == '-'
}

// isPort: 1..65535, chỉ chữ số, không số 0 đứng đầu.
func isPort(p string) bool {
	if p == "" || len(p) > 5 || p[0] == '0' {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(p)
	return err == nil && n <= 65535
}

// String trả về nguồn đã kiểm ("" nếu là giá trị zero).
func (r RedirectSource) String() string { return r.s }

// IsZero cho biết đây là giá trị zero.
func (r RedirectSource) IsZero() bool { return r.s == "" }

// MarshalText để RedirectSource ra JSON dưới dạng chuỗi.
func (r RedirectSource) MarshalText() ([]byte, error) { return []byte(r.s), nil }

// UnmarshalText kiểm như ParseRedirectSource. Lỗi thì giữ nguyên r.
func (r *RedirectSource) UnmarshalText(b []byte) error {
	v, err := ParseRedirectSource(string(b))
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// String trả về đích đã kiểm ("" nếu là giá trị zero).
func (t RedirectTarget) String() string { return t.s }

// IsZero cho biết đây là giá trị zero.
func (t RedirectTarget) IsZero() bool { return t.s == "" }

// MarshalText để RedirectTarget ra JSON dưới dạng chuỗi.
func (t RedirectTarget) MarshalText() ([]byte, error) { return []byte(t.s), nil }

// UnmarshalText kiểm như ParseRedirectTarget. Lỗi thì giữ nguyên t.
func (t *RedirectTarget) UnmarshalText(b []byte) error {
	v, err := ParseRedirectTarget(string(b))
	if err != nil {
		return err
	}
	*t = v
	return nil
}

// HasQuery cho biết đích có phần query ("?...").
func (t RedirectTarget) HasQuery() bool { return strings.Contains(t.s, "?") }

// IsAbsolute cho biết đích là URL http(s) thay vì đường dẫn trên cùng site.
func (t RedirectTarget) IsAbsolute() bool { return t.s != "" && t.s[0] != '/' }
