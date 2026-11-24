package templates

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// m19ZeroCase là một biến thể vhost cho B5: old là hàm cũ (chữ ký giữ nguyên).
type m19ZeroCase struct {
	name string
	old  func(l site.Layout) ([]byte, error)
}

func m19ZeroCases() []m19ZeroCase {
	var cs []m19ZeroCase
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			suffix := web
			if ssl {
				suffix += "-ssl"
			}
			web, ssl := web, ssl
			cs = append(cs,
				m19ZeroCase{"thuong-" + suffix, func(l site.Layout) ([]byte, error) {
					return SiteVhost(l, web, "8.3", acmeRoot, ssl, site.HtaccessSafe)
				}},
				m19ZeroCase{"long-" + suffix, func(l site.Layout) ([]byte, error) {
					return SiteVhostCaged(l, web, acmeRoot, ssl, site.HtaccessSafe)
				}},
				m19ZeroCase{"tamkhoa-" + suffix, func(l site.Layout) ([]byte, error) {
					return SuspendedVhost(l, web, acmeRoot, ssl)
				}},
			)
		}
	}
	return cs
}

// B5: site không tên phụ, không luật thì vhost giống từng byte bản trước M19.
// File vàng testdata/m19-golden-*.golden sinh MỘT LẦN bằng mã trước khi sửa
// template (M19_GOLDEN_WRITE=1), cố ý không theo cờ -update chung của gói: đổi
// chúng là phá bất biến B5, phải có lý do ghi trong commit.
func TestVhostZeroNamesGolden(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for _, c := range m19ZeroCases() {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.old(l)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "m19-golden-"+c.name+".golden")
			if os.Getenv("M19_GOLDEN_WRITE") == "1" {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("hàm cũ khác file vàng %s", path)
			}
		})
	}
}

// Hàm mới với site.Names{} phải ra đúng như hàm cũ (cùng file vàng).
func TestVhostZeroNamesNewFuncs(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	newF := map[string]func(web string, ssl bool) ([]byte, error){
		"thuong": func(web string, ssl bool) ([]byte, error) {
			return SiteVhostNames(l, web, "8.3", acmeRoot, ssl, site.HtaccessSafe, site.Names{})
		},
		"long": func(web string, ssl bool) ([]byte, error) {
			return SiteVhostCagedNames(l, web, acmeRoot, ssl, site.HtaccessSafe, site.Names{})
		},
		"tamkhoa": func(web string, ssl bool) ([]byte, error) {
			return SuspendedVhostNames(l, web, acmeRoot, ssl, site.Names{})
		},
	}
	for kind, f := range newF {
		for _, web := range []string{"nginx", "apache"} {
			for _, ssl := range []bool{false, true} {
				name := kind + "-" + web
				if ssl {
					name += "-ssl"
				}
				got, err := f(web, ssl)
				if err != nil {
					t.Fatal(name, err)
				}
				want, err := os.ReadFile(filepath.Join("testdata", "m19-golden-"+name+".golden"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s: hàm mới với Names{} khác file vàng", name)
				}
			}
		}
	}
}

func m19Doms(t *testing.T, ss ...string) []validate.Domain {
	t.Helper()
	var out []validate.Domain
	for _, s := range ss {
		d, err := validate.ParseDomain(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func TestSplitNames(t *testing.T) {
	p := m19Doms(t, "example.com")[0]
	cases := []struct {
		name string
		n    site.Names
		ssl  bool
		want nameSplit
	}{
		{"rỗng", site.Names{}, false, nameSplit{}},
		{"rỗng ssl", site.Names{Covered: m19Doms(t, "example.com")}, true, nameSplit{}},
		{"không ssl", site.Names{Serve: m19Doms(t, "a.example.net", "b.example.net"), Redirect: m19Doms(t, "www.example.com")}, false,
			nameSplit{Content: []string{"a.example.net", "b.example.net"}, Group80: []string{"www.example.com"}}},
		{"khử trùng, bỏ tên chính", site.Names{
			Serve:    m19Doms(t, "a.example.net", "a.example.net", "example.com"),
			Redirect: m19Doms(t, "a.example.net", "www.example.com", "www.example.com", "example.com")}, false,
			nameSplit{Content: []string{"a.example.net"}, Group80: []string{"www.example.com"}}},
		{"ssl đủ bốn ô", site.Names{
			Serve:    m19Doms(t, "xn--vitnam-jk8b.example.net", "uncovered.example.net"),
			Redirect: m19Doms(t, "www.example.com", "old.example.net"),
			Covered:  m19Doms(t, "example.com", "xn--vitnam-jk8b.example.net", "www.example.com")}, true,
			nameSplit{
				Content:  []string{"xn--vitnam-jk8b.example.net"},
				Group80:  []string{"www.example.com", "old.example.net", "uncovered.example.net"},
				Own80:    []string{"xn--vitnam-jk8b.example.net"},
				Group443: []string{"www.example.com"},
			}},
		{"không ssl thì bỏ qua Covered", site.Names{Serve: m19Doms(t, "a.example.net"), Covered: m19Doms(t, "a.example.net")}, false,
			nameSplit{Content: []string{"a.example.net"}}},
	}
	for _, c := range cases {
		got, err := splitNames(p, c.n, c.ssl)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

// Bộ tên dùng cho các test so nguyên văn với mục 3.4.
func m19SpecNames(t *testing.T) site.Names {
	return site.Names{
		Redirect: m19Doms(t, "www.example.com", "old.example.net"),
		Covered:  m19Doms(t, "example.com", "www.example.com"),
	}
}

const m19SpecNginx80 = `
# Tên phụ chuyển về https://example.com (M19, tpan site alias). Thử thách Let's
# Encrypt vẫn phục vụ thẳng, để xin được chứng chỉ có cả các tên này.
server {
    listen 80;
    listen [::]:80;
    server_name www.example.com old.example.net;

    access_log /var/log/tpan/sites/example.com/access.log;
    error_log /var/log/tpan/sites/example.com/error.log;

    location ^~ /.well-known/acme-challenge/ {
        root /var/lib/tpan/acme;
    }

    location / {
        return 301 https://example.com$request_uri;
    }
}
`

const m19SpecNginx443 = `
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name www.example.com;

    ssl_certificate /etc/letsencrypt/live/example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;

    access_log /var/log/tpan/sites/example.com/access.log;
    error_log /var/log/tpan/sites/example.com/error.log;

    location / {
        return 301 https://example.com$request_uri;
    }
}
`

const m19SpecApache80 = `
# Tên phụ chuyển về https://example.com (M19, tpan site alias).
<VirtualHost *:80>
    ServerName www.example.com
    ServerAlias old.example.net
    DocumentRoot "/var/www/example.com/public"

    ErrorLog "/var/log/tpan/sites/example.com/error.log"
    CustomLog "/var/log/tpan/sites/example.com/access.log" combined

    Alias /.well-known/acme-challenge/ "/var/lib/tpan/acme/.well-known/acme-challenge/"
    <Directory "/var/lib/tpan/acme/.well-known/acme-challenge">
        Options None
        AllowOverride None
        Require all granted
    </Directory>

    <Directory "/var/www/example.com">
        Options SymLinksIfOwnerMatch
        AllowOverride None
    </Directory>

    RewriteEngine On
    RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/
    RewriteRule ^/(.*)$ https://example.com/$1 [R=301,L]
</VirtualHost>
`

func m19Render(t *testing.T, web string, ssl bool, n site.Names) string {
	t.Helper()
	out, err := SiteVhostNames(siteTestLayout(t, "example.com"), web, "8.3", acmeRoot, ssl, site.HtaccessSafe, n)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestVhostNamesNginx(t *testing.T) {
	got := m19Render(t, "nginx", true, m19SpecNames(t))
	// Hai khối chuyển nằm cuối file, đúng thứ tự cổng 80 rồi 443.
	if !strings.HasSuffix(got, "}\n"+m19SpecNginx80+m19SpecNginx443) {
		t.Errorf("cuối file không đúng khối mục 3.4:\n%s", got)
	}
	// Khối nội dung 443 không có tên phụ (không có serve).
	if strings.Count(got, "server_name example.com;") != 2 {
		t.Errorf("server_name của tên chính phải giữ nguyên ở cả 80 và 443:\n%s", got)
	}
	// Không SSL: một tên serve vào khối nội dung, redirect về http://.
	got = m19Render(t, "nginx", false, site.Names{Serve: m19Doms(t, "a.example.net"), Redirect: m19Doms(t, "www.example.com")})
	for _, want := range []string{
		"    server_name example.com a.example.net;\n",
		"    server_name www.example.com;\n",
		"        return 301 http://example.com$request_uri;\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %q:\n%s", want, got)
		}
	}
}

func TestVhostNamesApache(t *testing.T) {
	got := m19Render(t, "apache", false, site.Names{Serve: m19Doms(t, "a.example.net", "b.example.net"), Redirect: m19Doms(t, "www.example.com", "old.example.net")})
	if !strings.Contains(got, "    ServerName example.com\n    ServerAlias a.example.net b.example.net\n    DocumentRoot") {
		t.Errorf("khối nội dung thiếu ServerAlias:\n%s", got)
	}
	want80 := strings.ReplaceAll(m19SpecApache80, "https://example.com", "http://example.com")
	if !strings.HasSuffix(got, "</VirtualHost>\n"+want80) {
		t.Errorf("cuối file không đúng vhost chuyển của mục 3.4:\n%s", got)
	}
	got = m19Render(t, "apache", true, m19SpecNames(t))
	want443 := `
# Tên phụ chuyển về https://example.com (M19, tpan site alias).
<VirtualHost *:443>
    ServerName www.example.com
    DocumentRoot "/var/www/example.com/public"

    SSLEngine on
    SSLCertificateFile "/etc/letsencrypt/live/example.com/fullchain.pem"
    SSLCertificateKeyFile "/etc/letsencrypt/live/example.com/privkey.pem"

    ErrorLog "/var/log/tpan/sites/example.com/error.log"
    CustomLog "/var/log/tpan/sites/example.com/access.log" combined

    <Directory "/var/www/example.com">
        Options SymLinksIfOwnerMatch
        AllowOverride None
    </Directory>

    RewriteEngine On
    RewriteRule ^/(.*)$ https://example.com/$1 [R=301,L]
</VirtualHost>
`
	if !strings.HasSuffix(got, "</VirtualHost>\n"+m19SpecApache80+want443) {
		t.Errorf("cuối file SSL không đúng mục 3.4:\n%s", got)
	}
}

// m19Blocks tách output thành từng khối server / VirtualHost (bỏ phần đầu file).
func m19Blocks(out, web string) []string {
	sep := "\nserver {"
	if web == "apache" {
		sep = "\n<VirtualHost "
	}
	parts := strings.Split(out, sep)
	return parts[1:]
}

func m19Is443(block, web string) bool {
	if web == "apache" {
		return strings.HasPrefix(block, "*:443>")
	}
	return strings.Contains(block, "listen 443 ssl;")
}

// B4: tên phụ chưa có trong chứng chỉ không bao giờ nằm ở cổng 443, và ở cổng
// 80 nó chuyển về https://<tên chính>.
func TestSSLUncoveredAliasNotOn443(t *testing.T) {
	n := site.Names{
		Serve:    m19Doms(t, "xn--vitnam-jk8b.example.net", "uncovered.example.net"),
		Redirect: m19Doms(t, "www.example.com", "redir-uncovered.example.net"),
		Covered:  m19Doms(t, "example.com", "xn--vitnam-jk8b.example.net", "www.example.com"),
	}
	l := siteTestLayout(t, "example.com")
	for _, web := range []string{"nginx", "apache"} {
		outs := map[string]string{"thuong": m19Render(t, web, true, n)}
		caged, err := SiteVhostCagedNames(l, web, acmeRoot, true, site.HtaccessSafe, n)
		if err != nil {
			t.Fatal(err)
		}
		outs["long"] = string(caged)
		susp, err := SuspendedVhostNames(l, web, acmeRoot, true, n)
		if err != nil {
			t.Fatal(err)
		}
		outs["tamkhoa"] = string(susp)
		for kind, out := range outs {
			seen80 := 0
			for _, b := range m19Blocks(out, web) {
				for _, un := range []string{"uncovered.example.net", "redir-uncovered.example.net"} {
					has := strings.Contains(b, " "+un+"\n") || strings.Contains(b, " "+un+";") || strings.Contains(b, " "+un+" ")
					if m19Is443(b, web) && has {
						t.Errorf("%s %s: %s nằm ở khối 443:\n%s", web, kind, un, b)
					}
					if !m19Is443(b, web) && has {
						seen80++
					}
				}
			}
			if seen80 != 2 {
				t.Errorf("%s %s: hai tên chưa phủ phải nằm ở cổng 80 (thấy %d lần)", web, kind, seen80)
			}
		}
		// Tên serve có phủ: ở khối nội dung 443, và có khối 80 riêng về https://<chính nó>.
		out := outs["thuong"]
		for _, want := range []string{"https://xn--vitnam-jk8b.example.net"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: thiếu chuyển về %s", web, want)
			}
		}
	}
}

// B8: mọi khối cổng 80 chuyển tên phụ vẫn phục vụ thẳng thử thách ACME.
func TestNameRedirectBlockServesACME(t *testing.T) {
	n := site.Names{
		Serve:    m19Doms(t, "a.example.net", "b.example.net"),
		Redirect: m19Doms(t, "www.example.com"),
		Covered:  m19Doms(t, "example.com", "a.example.net", "www.example.com"),
	}
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			blocks := 0
			for _, b := range m19Blocks(m19Render(t, web, ssl, n), web) {
				// Khối cổng 80 không phải của tên chính là khối chuyển tên phụ.
				if m19Is443(b, web) || strings.Contains(b, "server_name example.com") || strings.Contains(b, "ServerName example.com\n") {
					continue
				}
				blocks++
				var acme []string
				if web == "nginx" {
					acme = []string{"    location ^~ /.well-known/acme-challenge/ {\n        root /var/lib/tpan/acme;\n    }\n"}
				} else {
					acme = []string{
						`    Alias /.well-known/acme-challenge/ "/var/lib/tpan/acme/.well-known/acme-challenge/"`,
						`    RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/` + "\n    RewriteRule ",
					}
				}
				for _, a := range acme {
					if !strings.Contains(b, a) {
						t.Errorf("%s ssl=%v: khối chuyển thiếu %q:\n%s", web, ssl, a, b)
					}
				}
			}
			want := 1
			if ssl {
				want = 2 // khối nhóm + khối riêng của a.example.net
			}
			if blocks != want {
				t.Errorf("%s ssl=%v: chỉ thấy %d khối chuyển cổng 80", web, ssl, blocks)
			}
		}
	}
}

// B11: đích chuyển hướng không bao giờ lấy từ request. Bỏ dòng chú thích (chú
// thích sẵn có giải thích vì sao không dùng $host).
func TestNoHostVariables(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	n := site.Names{
		Serve:    m19Doms(t, "a.example.net", "uncovered.example.net"),
		Redirect: m19Doms(t, "www.example.com", "old.example.net"),
		Covered:  m19Doms(t, "example.com", "a.example.net", "www.example.com"),
	}
	for _, names := range []site.Names{{}, n} {
		for _, web := range []string{"nginx", "apache"} {
			for _, ssl := range []bool{false, true} {
				outs := []string{m19Render(t, web, ssl, names)}
				for _, f := range []func() ([]byte, error){
					func() ([]byte, error) { return SiteVhostCagedNames(l, web, acmeRoot, ssl, site.HtaccessSafe, names) },
					func() ([]byte, error) { return SuspendedVhostNames(l, web, acmeRoot, ssl, names) },
				} {
					out, err := f()
					if err != nil {
						t.Fatal(err)
					}
					outs = append(outs, string(out))
				}
				for _, out := range outs {
					for _, line := range strings.Split(out, "\n") {
						if strings.HasPrefix(strings.TrimSpace(line), "#") {
							continue
						}
						low := strings.ToLower(line)
						for _, bad := range []string{"$host", "$http_host", "$server_name", "%{http_host}", "%{server_name}"} {
							if strings.Contains(low, bad) {
								t.Errorf("%s ssl=%v: dòng lấy tên từ request: %q", web, ssl, line)
							}
						}
					}
				}
			}
		}
	}
}

// Lớp chặn cuối: tên ngoài [a-z0-9.-]{1,253} là ErrParam; luật (chưa vẽ ở đợt
// 1) là ErrParam ở vhost nội dung, bị bỏ qua ở bản tạm khoá.
func TestVhostNamesGuard(t *testing.T) {
	for _, s := range []string{"", "a.com; rm -rf /", "$(id)", "../../etc/passwd", "a.com\nserver {", "A.COM", "việt.vn", "a b", strings.Repeat("a", 254)} {
		if m19Name.MatchString(s) {
			t.Errorf("m19Name nhận %q", s)
		}
	}
	for _, s := range []string{"example.com", "xn--vitnam-jk8b.example.net", strings.Repeat("a", 253)} {
		if !m19Name.MatchString(s) {
			t.Errorf("m19Name từ chối %q", s)
		}
	}
	l := siteTestLayout(t, "example.com")
	n := site.Names{Rules: []site.Redirect{{ID: 1, Code: 301}}}
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			if _, err := SiteVhostNames(l, web, "8.3", acmeRoot, ssl, site.HtaccessSafe, n); !errors.Is(err, ErrParam) {
				t.Errorf("%s ssl=%v SiteVhostNames có luật: %v", web, ssl, err)
			}
			if _, err := SiteVhostCagedNames(l, web, acmeRoot, ssl, site.HtaccessSafe, n); !errors.Is(err, ErrParam) {
				t.Errorf("%s ssl=%v SiteVhostCagedNames có luật: %v", web, ssl, err)
			}
			// Bản tạm khoá không vẽ luật: site có luật vẫn tạm khoá được.
			if _, err := SuspendedVhostNames(l, web, acmeRoot, ssl, n); err != nil {
				t.Errorf("%s ssl=%v SuspendedVhostNames có luật: %v", web, ssl, err)
			}
		}
	}
	if _, err := SiteVhostNames(l, "caddy", "8.3", acmeRoot, false, site.HtaccessSafe, site.Names{}); !errors.Is(err, ErrParam) {
		t.Errorf("web lạ: %v", err)
	}
}

// Bản tạm khoá: cổng 80 mọi tên phụ, 443 chỉ tên có trong chứng chỉ, không khối chuyển.
func TestSuspendedVhostNames(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	n := site.Names{
		Serve:    m19Doms(t, "a.example.net", "uncovered.example.net"),
		Redirect: m19Doms(t, "www.example.com"),
		Covered:  m19Doms(t, "example.com", "a.example.net", "www.example.com"),
	}
	out, err := SuspendedVhostNames(l, "nginx", acmeRoot, true, n)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"    listen [::]:80;\n    server_name example.com a.example.net uncovered.example.net www.example.com;\n",
		"    listen [::]:443 ssl;\n    server_name example.com a.example.net www.example.com;\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	out, err = SuspendedVhostNames(l, "apache", acmeRoot, true, n)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<VirtualHost *:80>\n    ServerName example.com\n    ServerAlias a.example.net uncovered.example.net www.example.com\n",
		"<VirtualHost *:443>\n    ServerName example.com\n    ServerAlias a.example.net www.example.com\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "M19, tpan site alias") {
		t.Errorf("bản tạm khoá không được có khối chuyển")
	}
}

// TestM19DumpForVM ghi output của các hàm mới ra thư mục M19_VM_DUMP để chạy
// nginx -t / apachectl -t và bản thử t5-nginx-443 trên máy ảo (không chạy khi
// không đặt biến; không phải test thường).
func TestM19DumpForVM(t *testing.T) {
	dir := os.Getenv("M19_VM_DUMP")
	if dir == "" {
		t.Skip("đặt M19_VM_DUMP=<thư mục> để ghi")
	}
	l := siteTestLayout(t, "example.com")
	// Bộ tên của t5: một serve có trong chứng chỉ, một redirect có trong chứng
	// chỉ, một serve chưa có trong chứng chỉ.
	n := site.Names{
		Serve:    m19Doms(t, "xn--vitnam-jk8b.example.net", "uncovered.example.net"),
		Redirect: m19Doms(t, "www.example.com"),
		Covered:  m19Doms(t, "example.com", "www.example.com", "xn--vitnam-jk8b.example.net"),
	}
	write := func(name string, b []byte, err error) {
		if err != nil {
			t.Fatal(name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			sfx := web
			if ssl {
				sfx += "-ssl"
			}
			b, err := SiteVhostNames(l, web, "8.3", acmeRoot, ssl, site.HtaccessSafe, n)
			write("thuong-"+sfx+".conf", b, err)
			b, err = SiteVhostCagedNames(l, web, acmeRoot, ssl, site.HtaccessSafe, n)
			write("long-"+sfx+".conf", b, err)
			b, err = SuspendedVhostNames(l, web, acmeRoot, ssl, n)
			write("tamkhoa-"+sfx+".conf", b, err)
		}
	}
	b, err := NginxDefault(DefaultSite{AcmeRoot: acmeRoot})
	write("nginx-default.conf", b, err)
	b, err = NginxDefaultSSL()
	write("nginx-default-ssl.conf", b, err)
	b, err = ApacheDefault(DefaultSite{AcmeRoot: acmeRoot})
	write("apache-default.conf", b, err)
	b, err = ApacheDefaultSSL()
	write("apache-default-ssl.conf", b, err)
}
