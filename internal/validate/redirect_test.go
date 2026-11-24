package validate

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// redirectAllowed: lớp chặn cuối của template (mục 3.4), cộng độ dài 1..1024
// kiểm riêng (regexp của Go không nhận số lặp quá 1000). Mọi chuỗi Parse nhận
// phải khớp nó (B3).
var redirectAllowed = regexp.MustCompile(`^[A-Za-z0-9._~/:?=&-]+$`)

// redirectBad là bảng chuỗi tấn công chung cho nguồn và đích (mục 7.1).
var redirectBad = []string{
	"",
	"/a\n",
	"/a\nreturn 200;",
	"/a\r",
	"/a;",
	"/a}",
	"/a{",
	" /a",
	"/a b",
	"/a ",
	"/a\t",
	"/$host",
	"/a$1",
	"/a%2F",
	"/%2e%2e/etc",
	`/a\b`,
	`/a"`,
	"/a'",
	"/a#frag",
	"/a@b",
	"/a*",
	"javascript:alert(1)",
	"//evil.example.org",
	"//evil.example.org/x",
	"/.well-known/x",
	"/.well-known/acme-challenge/t",
	"/.env",
	"/a/../b",
	"/a/./b",
	"/..",
	"/a//b",
	"//",
	"/a//",
	"/việt",
	"a",
	"a/b",
	"/a\x00",
	"/a\x7f",
	"/a\u00a0b",
	"/a\u200bb",
	"/" + strings.Repeat("a", 1100),
	"/%{HTTP_HOST}",
	"/a;rm -rf /",
	"/$(id)",
	"/../../etc/passwd",
}

func wantRedirectErr(t *testing.T, what, in string, err error, code, field string) {
	t.Helper()
	var ve *Error
	if !errors.As(err, &ve) || ve.Code != code {
		t.Errorf("%s(%q): lỗi %v, muốn *Error mã %s", what, in, err, code)
		return
	}
	var fe *RedirectError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Errorf("%s(%q): lỗi %v, muốn RedirectError trường %s", what, in, err, field)
	}
	// Lời lỗi không chép lại đầu vào: mọi đầu vào hỏng cùng trường, cùng mã
	// cho đúng một lời.
	if code == CodeRedirectInvalid {
		_, canon := ParseRedirectSource("x")
		if field == FieldTarget {
			_, canon = ParseRedirectTarget("x")
		}
		if err.Error() != canon.Error() {
			t.Errorf("%s(%q): lời lỗi khác lời chung: %q", what, in, err.Error())
		}
	}
}

func TestParseRedirectSource(t *testing.T) {
	good := []string{
		"/old-page",
		"/old-page/",
		"/a/b/c",
		"/a/b/c/",
		"/A_b~c-d.html",
		"/wp-content/uploads/x.php",
		"/index.php/",
		"/INDEX.PHP",
		"/a.b",
		"/" + strings.Repeat("a", 254),
	}
	for _, s := range good {
		r, err := ParseRedirectSource(s)
		if err != nil {
			t.Errorf("ParseRedirectSource(%q): %v", s, err)
			continue
		}
		if r.String() != s || r.IsZero() {
			t.Errorf("ParseRedirectSource(%q) = %q", s, r)
		}
	}
	bad := append([]string{
		"/old?x=1",
		"/old?",
		"http://example.com/a",
		"/" + strings.Repeat("a", 255), // 256 byte
	}, redirectBad...)
	for _, s := range bad {
		r, err := ParseRedirectSource(s)
		if !r.IsZero() {
			t.Errorf("ParseRedirectSource(%q) trả giá trị khác zero khi lỗi", s)
		}
		wantRedirectErr(t, "ParseRedirectSource", s, err, CodeRedirectInvalid, FieldSource)
	}
}

func TestParseRedirectTarget(t *testing.T) {
	cases := []struct {
		in, want   string
		abs, query bool
	}{
		{"/", "/", false, false},
		{"/new-page", "/new-page", false, false},
		{"/new-page/", "/new-page/", false, false},
		{"/a?b=1&c=d", "/a?b=1&c=d", false, true},
		{"/a?x", "/a?x", false, true},
		{"/index.php", "/index.php", false, false},
		{"https://example.net", "https://example.net", true, false},
		{"https://example.net/", "https://example.net/", true, false},
		{"http://example.net/target", "http://example.net/target", true, false},
		{"https://example.net/t?a=1", "https://example.net/t?a=1", true, true},
		{"https://example.net?a=1", "https://example.net?a=1", true, true},
		{"https://example.net:8443/x", "https://example.net:8443/x", true, false},
		{"https://example.net:1", "https://example.net:1", true, false},
		{"https://example.net:65535/", "https://example.net:65535/", true, false},
		// Host chuẩn hoá như ParseDomain: chữ thường, punycode.
		{"https://Example.NET/A", "https://example.net/A", true, false},
		{"https://việtnam.example.net/x", "https://xn--vitnam-jk8b.example.net/x", true, false},
	}
	for _, c := range cases {
		r, err := ParseRedirectTarget(c.in)
		if err != nil {
			t.Errorf("ParseRedirectTarget(%q): %v", c.in, err)
			continue
		}
		if r.String() != c.want || r.IsAbsolute() != c.abs || r.HasQuery() != c.query {
			t.Errorf("ParseRedirectTarget(%q) = %q abs=%v query=%v; muốn %q abs=%v query=%v",
				c.in, r, r.IsAbsolute(), r.HasQuery(), c.want, c.abs, c.query)
		}
	}
	bad := append([]string{
		"HTTP://example.net/",
		"Https://example.net/",
		"ftp://example.net/",
		"http:/example.net",
		"http://",
		"https://",
		"http://user@example.net",
		"http://user:pw@example.net/",
		"http://example.net:0/",
		"http://example.net:00080/",
		"http://example.net:080/",
		"http://example.net:65536/",
		"http://example.net:/",
		"http://example.net:abc/",
		"http://example.net:123456/",
		"http://127.0.0.1/",
		"http://[::1]/",
		"http://10.0.0.1:8080/",
		"http://example.net./",
		"http://.example.net/",
		"http:// example.net/",
		"http://example.net /",
		"http://example.net\n/",
		"http://exa mple.net/",
		"http://example.net/a b",
		"http://example.net/.env",
		"http://example.net/a/../b",
		"http://example.net//a",
		"http://example.net/a?",
		"http://example.net/a?b c",
		"http://example.net/a?b;c",
		"http://example.net/a?b%20",
		"http://example.net/a?$args",
		"http://example.net/a?b#c",
		"http://example.net/a?b?c",
		"http://example.net/a?" + strings.Repeat("a", 256),
		"http://example.net/" + strings.Repeat("a", 1020),
		"https://evil.example.org\\@example.net/",
		"/a?",
		"/a?b c",
		"/a?" + strings.Repeat("q", 256),
	}, redirectBad...)
	for _, s := range bad {
		r, err := ParseRedirectTarget(s)
		if !r.IsZero() {
			t.Errorf("ParseRedirectTarget(%q) trả giá trị khác zero khi lỗi", s)
		}
		wantRedirectErr(t, "ParseRedirectTarget", s, err, CodeRedirectInvalid, FieldTarget)
	}
}

// B8: nguồn không có đoạn bắt đầu bằng "." nên không che được thử thách ACME
// hay luật chặn file ẩn.
func TestRedirectSourceRejectsDotSegment(t *testing.T) {
	for _, s := range []string{
		"/.well-known/acme-challenge/x", "/.well-known/", "/.well-known",
		"/.env", "/.git/config", "/a/.htaccess", "/a/.", "/a/..", "/.", "/..", "/a/./b", "/a/../b",
	} {
		_, err := ParseRedirectSource(s)
		wantRedirectErr(t, "ParseRedirectSource", s, err, CodeRedirectInvalid, FieldSource)
	}
	// Dấu chấm ở giữa hay cuối đoạn vẫn được.
	for _, s := range []string{"/a.html", "/a./b", "/well-known.txt"} {
		if _, err := ParseRedirectSource(s); err != nil {
			t.Errorf("ParseRedirectSource(%q): %v", s, err)
		}
	}
}

// S11: "/" và "/index.php" là đường đi của mọi trang WordPress (try_files của
// nginx đổi request về /index.php), nên không được làm nguồn. Đích thì được.
func TestRedirectReserved(t *testing.T) {
	for _, s := range []string{"/", "/index.php"} {
		r, err := ParseRedirectSource(s)
		if !r.IsZero() {
			t.Errorf("ParseRedirectSource(%q) trả giá trị khác zero", s)
		}
		wantRedirectErr(t, "ParseRedirectSource", s, err, CodeRedirectReserved, FieldSource)
		if _, err := ParseRedirectTarget(s); err != nil {
			t.Errorf("ParseRedirectTarget(%q): %v", s, err)
		}
	}
	var r RedirectSource
	if err := r.UnmarshalText([]byte("/")); err == nil || !r.IsZero() {
		t.Errorf("UnmarshalText(\"/\") = %v, %q", err, r)
	}
}

func TestRedirectTextRoundTrip(t *testing.T) {
	var s RedirectSource
	if err := s.UnmarshalText([]byte("/old")); err != nil || s.String() != "/old" {
		t.Fatalf("UnmarshalText nguồn: %v %q", err, s)
	}
	if err := s.UnmarshalText([]byte("/a;b")); err == nil || s.String() != "/old" {
		t.Errorf("UnmarshalText lỗi phải giữ nguyên giá trị cũ: %v %q", err, s)
	}
	var tg RedirectTarget
	if err := tg.UnmarshalText([]byte("https://Example.net/x?y=1")); err != nil {
		t.Fatal(err)
	}
	b, _ := tg.MarshalText()
	if string(b) != "https://example.net/x?y=1" {
		t.Errorf("MarshalText = %q", b)
	}
}

// FuzzParseRedirect (B3): mọi chuỗi được nhận chỉ gồm ký tự cho phép, không có
// đoạn bắt đầu bằng ".", không "//" ngoài scheme, và đọc lại cho đúng chính nó
// (store nạp lại từ DB qua UnmarshalText). Lỗi luôn là *Error có trường.
func FuzzParseRedirect(f *testing.F) {
	for _, s := range redirectBad {
		f.Add(s)
	}
	for _, s := range []string{
		"/old-page", "/a/b/", "/a?b=1&c=2", "https://example.net:8443/a?b=c",
		"http://việtnam.example.net/x", "https://EXAMPLE.net", "/index.php", "/",
		"http://example.net:0/", "https://a.b.c.example.org/x/y/z/",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		check := func(field, out string, err error) {
			if err != nil {
				var fe *RedirectError
				var ve *Error
				if !errors.As(err, &fe) || fe.Field != field || !errors.As(err, &ve) ||
					(ve.Code != CodeRedirectInvalid && ve.Code != CodeRedirectReserved) {
					t.Fatalf("%s %q: lỗi lạ %#v", field, in, err)
				}
				return
			}
			if !redirectAllowed.MatchString(out) || len(out) > 1024 {
				t.Fatalf("%s %q được nhận thành %q, có ký tự ngoài danh sách", field, in, out)
			}
			path := out
			if i := strings.Index(path, "://"); i >= 0 {
				path = path[i+3:]
			}
			if strings.Contains(path, "//") || strings.Contains(path, "/.") {
				t.Fatalf("%s %q được nhận thành %q", field, in, out)
			}
		}
		if r, err := ParseRedirectSource(in); err == nil {
			check(FieldSource, r.String(), nil)
			if len(r.String()) > 255 || strings.Contains(r.String(), "?") || r.String() != in {
				t.Fatalf("nguồn %q được nhận thành %q", in, r)
			}
			if r2, err := ParseRedirectSource(r.String()); err != nil || r2 != r {
				t.Fatalf("nguồn %q không đọc lại được: %v", r, err)
			}
		} else {
			check(FieldSource, "", err)
		}
		if r, err := ParseRedirectTarget(in); err == nil {
			check(FieldTarget, r.String(), nil)
			if r2, err := ParseRedirectTarget(r.String()); err != nil || r2 != r {
				t.Fatalf("đích %q không đọc lại được: %v", r, err)
			}
			if r.IsAbsolute() != !strings.HasPrefix(r.String(), "/") {
				t.Fatalf("đích %q: IsAbsolute sai", r)
			}
		} else {
			check(FieldTarget, "", err)
		}
	})
}
