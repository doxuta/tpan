package validate

import "testing"

func TestRegisteredDomainTenMienDungChung(t *testing.T) {
	t.Cleanup(func() { SetSharedSuffixes(nil) })
	dom := func(s string) Domain {
		d, err := ParseDomain(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	root := func(s string) string {
		r, err := RegisteredDomain(dom(s))
		if err != nil {
			return "lỗi"
		}
		return r.String()
	}
	if got := root("a.khu1.lab.example.com"); got != "example.com" {
		t.Fatalf("chưa khai: %s", got)
	}
	SetSharedSuffixes([]Domain{dom("khu1.lab.example.com"), dom("example.com")})
	for in, want := range map[string]string{
		"a.khu1.lab.example.com":     "a.khu1.lab.example.com",
		"www.a.khu1.lab.example.com": "a.khu1.lab.example.com",
		"b.khu1.lab.example.com":     "b.khu1.lab.example.com",
		"khu1.lab.example.com":       "lỗi", // chính nó là đuôi dùng chung
		"shop.example.com":            "shop.example.com",
		"x.shop.example.com":          "shop.example.com",
		"blog.example.org":            "example.org",
	} {
		if got := root(in); got != want {
			t.Errorf("%s: muốn %s, được %s", in, want, got)
		}
	}
}
