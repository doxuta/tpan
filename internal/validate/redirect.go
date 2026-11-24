package validate

// Luật chuyển hướng của M19 (docs/thay-doi/M19-thiet-ke.md mục 3.3): đường dẫn
// nguồn và đích đã kiểm theo danh sách ký tự cho phép. Hai kiểu có trường ẩn
// nên chỉ ParseRedirectSource / ParseRedirectTarget tạo được giá trị khác zero.

import "errors"

// CodeRedirectInvalid: nguồn hoặc đích của luật chuyển hướng không hợp lệ.
const CodeRedirectInvalid = "redirect_invalid"

// errRedirectTODO: thân thật do đợt 2 viết.
var errRedirectTODO = errors.New("validate: luật chuyển hướng chưa làm (M19 đợt 2)")

// RedirectSource là đường dẫn nguồn đã kiểm. Chỉ ParseRedirectSource tạo được.
type RedirectSource struct{ s string }

// RedirectTarget là đích đã kiểm: đường dẫn trên cùng site, hoặc URL http(s).
type RedirectTarget struct{ s string }

// ParseRedirectSource kiểm đường dẫn nguồn (mục 3.3).
func ParseRedirectSource(s string) (RedirectSource, error) {
	return RedirectSource{}, errRedirectTODO
}

// ParseRedirectTarget kiểm đích (mục 3.3).
func ParseRedirectTarget(s string) (RedirectTarget, error) {
	return RedirectTarget{}, errRedirectTODO
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
func (t RedirectTarget) HasQuery() bool { return false }

// IsAbsolute cho biết đích là URL http(s) thay vì đường dẫn trên cùng site.
func (t RedirectTarget) IsAbsolute() bool { return false }
