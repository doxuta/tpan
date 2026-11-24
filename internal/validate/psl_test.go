package validate

import (
	"errors"
	"testing"
)

// TestRegisteredDomain: bảng t4 của mục 2.5 (docs/thay-doi/M19-thiet-ke.md).
func TestRegisteredDomain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"www.example.com", "example.com"},
		{"a.b.example.com", "example.com"},
		{"example.com", "example.com"},
		{"shop.example.co.uk", "example.co.uk"},
		{"www.example.com.vn", "example.com.vn"},
		{"www.tênmiền.vn", "xn--tnmin-hsa0954c.vn"},
		// Phần private của PSL: hai người thuê hai tên dưới github.io là hai gốc.
		{"alice.github.io", "alice.github.io"},
		{"www.bob.github.io", "bob.github.io"},
		// TLD lạ hay dành riêng: luật mặc định "*", hai nhãn cuối.
		{"x.example.test", "example.test"},
		{"example.invalid", "example.invalid"},
		{"a.example.cromulent", "example.cromulent"},
	}
	for _, c := range cases {
		d, err := ParseDomain(c.in)
		if err != nil {
			t.Fatalf("ParseDomain(%q): %v", c.in, err)
		}
		got, err := RegisteredDomain(d)
		if err != nil || got.String() != c.want {
			t.Errorf("RegisteredDomain(%s) = %q, %v; muốn %q", c.in, got, err, c.want)
		}
	}
}

// TestRegisteredDomainPublicSuffix (B7): chính hậu tố công cộng không có tên
// miền gốc; giá trị zero cũng vậy.
func TestRegisteredDomainPublicSuffix(t *testing.T) {
	ds := []Domain{{}}
	for _, s := range []string{"co.uk", "com.vn", "github.io", "com"} {
		d, err := ParseDomain(s)
		if err != nil {
			continue // ParseDomain đã chặn trước, càng tốt
		}
		ds = append(ds, d)
	}
	if len(ds) < 4 {
		t.Fatalf("chỉ dựng được %d tên để thử", len(ds))
	}
	for _, d := range ds {
		got, err := RegisteredDomain(d)
		var ve *Error
		if !errors.As(err, &ve) || ve.Code != CodeDomainPublicSuffix || !got.IsZero() {
			t.Errorf("RegisteredDomain(%q) = %q, %v; muốn lỗi %s", d, got, err, CodeDomainPublicSuffix)
		}
	}
}
