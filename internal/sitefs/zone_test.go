package sitefs

import "testing"

func TestSplit(t *testing.T) {
	cases := []struct {
		p, anchor, rel string
		ok             bool
	}{
		{"/var/www/a.example.com/public/.htaccess", "/var/www/a.example.com", "public/.htaccess", true},
		{"/var/www/a.example.com/public", "/var/www/a.example.com", "public", true},
		{"/var/www/a.example.com/public/", "/var/www/a.example.com", "public", true},
		{"/var/www//a.example.com//public", "/var/www/a.example.com", "public", true},
		{"/var/www/a/../b/x", "/var/www/b", "x", true},
		// Chính neo, chính root, và ngoài root: không thuộc vùng.
		{"/var/www/a.example.com", "", "", false},
		{"/var/www/a.example.com/", "", "", false},
		{"/var/www", "", "", false},
		{"/var/www/", "", "", false},
		{"/var/wwwx/a/b", "", "", false},
		{"/var/www/a/../../etc/shadow", "", "", false},
		{"/etc/shadow", "", "", false},
		{"var/www/a/b", "", "", false},
		{"", "", "", false},
		{"/var/log/tpan/sites/a.example.com/access.log", "", "", false},
		{"/var/lib/tpan/trash/a_1/public/x", "", "", false},
	}
	for _, tc := range cases {
		a, r, ok := Split(Root, tc.p)
		if a != tc.anchor || r != tc.rel || ok != tc.ok {
			t.Errorf("Split(%q) = (%q, %q, %v), muốn (%q, %q, %v)", tc.p, a, r, ok, tc.anchor, tc.rel, tc.ok)
		}
	}
	if _, _, ok := Split("/", "/etc/x"); ok {
		t.Error("root \"/\" không được nhận mọi đường dẫn")
	}
	if !InZone("/var/www/a/public") || InZone("/var/www/a") {
		t.Error("InZone sai")
	}
}

func TestIsAnchor(t *testing.T) {
	for p, want := range map[string]bool{
		"/var/www/a.example.com":        true,
		"/var/www/a.example.com/":       true,
		"/var/www/a.example.com/public": false,
		"/var/www":                      false,
		"/var/www/":                     false,
		"/var/www/a/../b":               true,
		"/etc":                          false,
		"relative/x":                    false,
	} {
		if got := IsAnchor(Root, p); got != want {
			t.Errorf("IsAnchor(%q) = %v, muốn %v", p, got, want)
		}
	}
}

func TestRefused(t *testing.T) {
	if !Refused(&Error{Op: "đọc", Path: "/var/www/a/x", Err: ErrSymlink}) {
		t.Error("ErrSymlink bọc trong *Error phải là lời từ chối")
	}
	if Refused(&Error{Op: "đọc", Path: "/var/www/a/x", Err: errFake}) {
		t.Error("lỗi khác không phải lời từ chối")
	}
	e := &Error{Op: "đọc", Path: "/var/www/a/public/.htaccess", Err: ErrTooLarge}
	if e.Error() != "đọc /var/www/a/public/.htaccess: "+ErrTooLarge.Error() {
		t.Errorf("Error() = %q", e.Error())
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "lỗi khác" }

var errFake = fakeErr{}
