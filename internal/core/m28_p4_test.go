package core

import (
	"errors"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// TestRegistrableTaken: luật P4 (M28 đợt 2) trên danh sách site trong bộ nhớ.
// Hai khách không thể có hai tên con của cùng một domain gốc (eTLD+1): đó là
// ý muốn, không phải lỗi.
func TestRegistrableTaken(t *testing.T) {
	dom := func(s string) validate.Domain {
		t.Helper()
		d, err := validate.ParseDomain(s)
		if err != nil {
			t.Fatalf("ParseDomain(%q): %v", s, err)
		}
		return d
	}
	row := func(d, owner string, st site.Status) site.Site {
		return site.Site{Domain: dom(d), Owner: owner, Status: st}
	}
	all := []site.Site{
		row("b.example.net", "userb", site.Active),
		row("www.c.example.org", "userb", site.Active),
		row("shop.example.co.uk", "userb", site.Trashed),
		row("b.github.io", "userb", site.Active),
		row("admin.example.info", "", site.Active), // Owner rỗng là admin
		row("a.example.com", "usera", site.Active),
	}
	cases := []struct {
		domain string
		want   bool
	}{
		{"www.b.example.net", true},
		{"mail.b.example.net", true},
		{"example.net", true},
		{"c.example.org", true},          // ngược lại: B có www.c, A không lấy được gốc
		{"example.co.uk", true},          // site trong thùng rác vẫn tính
		{"x.shop.example.co.uk", true},   // eTLD là co.uk
		{"WWW.Admin.Example.Info", true}, // của admin cũng tính; hoa thường như nhau
		{"a.github.io", false},           // github.io là public suffix (phần private của PSL)
		{"www.a.example.com", false},     // cùng gốc với site của chính mình
		{"example.com", false},
		{"other.example.co.uk.example.com", false},
	}
	for _, c := range cases {
		got, err := RegistrableTaken(all, dom(c.domain), "usera")
		if err != nil || got != c.want {
			t.Errorf("RegistrableTaken(%s) = %v, %v; muốn %v", c.domain, got, err, c.want)
		}
	}
	// Owner rỗng của dòng cũ là admin (ownerOf). Server và apply không gọi
	// luật này cho admin; đây chỉ là tính chất của hàm.
	if got, _ := RegistrableTaken(all, dom("x.admin.example.info"), account.AdminName); got {
		t.Error("site Owner rỗng phải được tính là của admin")
	}

	// Tên không ra eTLD+1: chính là public suffix, hay chỉ một nhãn.
	for _, s := range []string{"github.io", "co.uk", "com"} {
		d, err := validate.ParseDomain(s)
		if err != nil {
			continue // validate đã chặn trước, càng tốt
		}
		_, err = RegistrableTaken(all, d, "usera")
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != validate.CodeDomainInvalid {
			t.Errorf("%s: lỗi %v, muốn RefusedError %s", s, err, validate.CodeDomainInvalid)
		}
	}
}
