package templates

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
)

// Golden của bốn vhost tạm khoá và trang tạm khoá; cập nhật bằng
// go test ./internal/templates/ -run TestSuspendGolden -update
func TestSuspendGolden(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	cases := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"site-nginx-suspended", func() ([]byte, error) { return SuspendedVhost(l, "nginx", acmeRoot, false) }},
		{"site-nginx-suspended-ssl", func() ([]byte, error) { return SuspendedVhost(l, "nginx", acmeRoot, true) }},
		{"site-apache-suspended", func() ([]byte, error) { return SuspendedVhost(l, "apache", acmeRoot, false) }},
		{"site-apache-suspended-ssl", func() ([]byte, error) { return SuspendedVhost(l, "apache", acmeRoot, true) }},
		{"suspended-page", func() ([]byte, error) { return SuspendedHTML(), nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.gen()
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join("testdata", c.name+".golden")
			if *update {
				if err := os.WriteFile(p, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("đọc golden (chạy với -update để tạo): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("khác golden %s (chạy với -update rồi đọc git diff):\n--- got ---\n%s", p, got)
			}
			if !bytes.HasSuffix(got, []byte("\n")) {
				t.Error("thiếu xuống dòng ở cuối file")
			}
		})
	}
}

// Những dòng mà bỏ đi là bản tạm khoá mất tác dụng: dòng đầu, log riêng của
// site, thử thách ACME ở cổng 80, 403 kèm trang chung, không còn đường nào tới
// public hay socket PHP của site, và cổng 80 không chuyển hướng.
func TestSuspendedVhostDirectives(t *testing.T) {
	l := siteTestLayout(t, "shop.example.com")
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			got, err := SuspendedVhost(l, web, acmeRoot, ssl)
			if err != nil {
				t.Fatal(err)
			}
			s := string(got)
			name := web
			if ssl {
				name += "-ssl"
			}
			if first, _, _ := strings.Cut(s, "\n"); first != Header {
				t.Errorf("%s: dòng đầu = %q", name, first)
			}
			want := []string{l.LogDir + "/access.log", l.LogDir + "/error.log", acmeRoot, SuspendedDir, SuspendedURI, "shop.example.com"}
			if web == "nginx" {
				want = append(want, "return 403;", "error_page 403 "+SuspendedURI+";", "location ^~ /.well-known/acme-challenge/", "internal;")
			} else {
				want = append(want, "ErrorDocument 403 "+SuspendedURI, "RewriteRule ^ - [F]", "RewriteCond %{REQUEST_URI} !^/\\.well-known/acme-challenge/", "AllowOverride None")
			}
			if ssl {
				want = append(want, l.CertFullchain(), l.CertPrivkey(), "443")
			}
			for _, w := range want {
				if !strings.Contains(s, w) {
					t.Errorf("%s: thiếu %q", name, w)
				}
			}
			for _, bad := range []string{l.Public, ".sock", "fastcgi", "SetHandler", "301", "https://", "AllowOverride All"} {
				if strings.Contains(s, bad) {
					t.Errorf("%s: không được có %q (site khoá không được trỏ tới mã nguồn, PHP hay chuyển hướng)", name, bad)
				}
			}
			if !ssl && strings.Contains(s, "443") {
				t.Errorf("%s: bản cổng 80 có 443", name)
			}
		}
	}
}

func TestSuspendedHTML(t *testing.T) {
	got := string(SuspendedHTML())
	if first, _, _ := strings.Cut(got, "\n"); !strings.Contains(first, Header) || !strings.HasPrefix(first, "<!--") {
		t.Errorf("dòng đầu = %q; muốn Header trong chú thích HTML", first)
	}
	for _, w := range []string{SuspendedMarker, "Site đang tạm khoá", "This site is suspended", `<meta charset="utf-8">`} {
		if !strings.Contains(got, w) {
			t.Errorf("trang tạm khoá thiếu %q", w)
		}
	}
	if !strings.HasSuffix(SuspendedPage, SuspendedURI) || !strings.HasPrefix(SuspendedPage, SuspendedDir+"/") {
		t.Errorf("SuspendedPage %q không khớp SuspendedDir + SuspendedURI", SuspendedPage)
	}
}

func TestSuspendedVhostBadParams(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	bad := l
	bad.LogDir = "/var/log/x; rm -rf /"
	for name, gen := range map[string]func() ([]byte, error){
		"web lạ":         func() ([]byte, error) { return SuspendedVhost(l, "caddy", acmeRoot, false) },
		"layout zero":    func() ([]byte, error) { return SuspendedVhost(site.Layout{}, "nginx", acmeRoot, false) },
		"layout sửa":     func() ([]byte, error) { return SuspendedVhost(bad, "nginx", acmeRoot, true) },
		"acme rỗng":      func() ([]byte, error) { return SuspendedVhost(l, "nginx", "", false) },
		"acme tương đối": func() ([]byte, error) { return SuspendedVhost(l, "apache", "var/lib/tpan/acme", false) },
		"acme có ..":     func() ([]byte, error) { return SuspendedVhost(l, "apache", "/var/../etc", true) },
		"acme xuống dòng": func() ([]byte, error) {
			return SuspendedVhost(l, "nginx", "/var/lib/tpan/acme\n}", false)
		},
	} {
		if _, err := gen(); !errors.Is(err, ErrParam) {
			t.Errorf("%s: lỗi = %v; muốn ErrParam", name, err)
		}
	}
}
