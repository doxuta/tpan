package templates

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

const testTpanBin = "/usr/local/bin/tpan"

// Golden của sáu file SSL; cập nhật bằng cờ -update như TestGolden.
func TestSSLGolden(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	cases := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"site-nginx-ssl", func() ([]byte, error) { return NginxSiteSSL(l, "8.3", acmeRoot) }},
		{"site-apache-ssl", func() ([]byte, error) { return ApacheSiteSSL(l, "8.3", acmeRoot, site.HtaccessSafe) }},
		{"nginx-default-ssl", NginxDefaultSSL},
		{"apache-default-ssl", ApacheDefaultSSL},
		{"renew-hook", func() ([]byte, error) { return RenewHook(testTpanBin) }},
		{"fail2ban-sshd", Fail2banSSHD},
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

// Dòng đầu là Header nguyên văn; riêng hook là shebang rồi Header ở dòng 2.
func TestSSLFirstLine(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for name, gen := range map[string]func() ([]byte, error){
		"nginx":          func() ([]byte, error) { return NginxSiteSSL(l, "8.3", acmeRoot) },
		"apache":         func() ([]byte, error) { return ApacheSiteSSL(l, "8.3", acmeRoot, site.HtaccessSafe) },
		"nginx-default":  NginxDefaultSSL,
		"apache-default": ApacheDefaultSSL,
		"fail2ban":       Fail2banSSHD,
	} {
		out, err := gen()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if first, _, _ := strings.Cut(string(out), "\n"); first != Header {
			t.Errorf("%s: dòng đầu = %q", name, first)
		}
	}
	hook, err := RenewHook(testTpanBin)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(hook), "\n")
	if lines[0] != "#!/bin/sh" || lines[1] != Header {
		t.Errorf("hook: hai dòng đầu = %q, %q", lines[0], lines[1])
	}
	if last := lines[len(lines)-2]; last != "exec "+testTpanBin+" internal reload-web" {
		t.Errorf("hook: dòng lệnh cuối = %q", last)
	}
}

// codeLines bỏ dòng chú thích và dòng trống, để test chỉ xét dòng lệnh.
func codeLines(b []byte) []string {
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(line); s != "" && !strings.HasPrefix(s, "#") {
			out = append(out, s)
		}
	}
	return out
}

// Vhost SSL của Nginx: cổng 80 chỉ còn thử thách và chuyển hướng; cổng 443 có
// chứng chỉ và đủ luật bảo mật của bản cổng 80.
func TestNginxSiteSSLDirectives(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	ng, err := NginxSiteSSL(l, "8.3", acmeRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"listen 80;",
		"listen [::]:80;",
		"listen 443 ssl;",
		"listen [::]:443 ssl;",
		"server_name example.com;",
		"ssl_certificate /etc/letsencrypt/live/example.com/fullchain.pem;",
		"ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;",
		"location ^~ /.well-known/acme-challenge/ {\n        root " + acmeRoot + ";",
		"return 301 https://example.com$request_uri;",
		// Luật của bản cổng 80, phải còn nguyên ở 443.
		"root " + l.Public + ";",
		"access_log " + l.LogDir + "/access.log;",
		"error_log " + l.LogDir + "/error.log;",
		"client_max_body_size 64m;",
		"location ~ /\\.(?!well-known/) {\n        deny all;",
		"location ~* ^/wp-content/uploads/.*\\.php$ {\n        deny all;",
		"try_files $uri $uri/ /index.php?$args;",
		"fastcgi_pass unix:" + l.SocketFor("8.3") + ";",
	} {
		if !bytes.Contains(ng, []byte(s)) {
			t.Errorf("nginx SSL thiếu %q", s)
		}
	}
	// Hai server: cổng 80 chuyển hướng, cổng 443 chạy site. Chuyển hướng không
	// được nằm trong server 443 (sẽ thành vòng lặp), try_files không nằm trong 80.
	i443 := bytes.Index(ng, []byte("listen 443 ssl;"))
	if i443 < 0 || bytes.Contains(ng[i443:], []byte("return 301")) {
		t.Error("server 443 không được có return 301")
	}
	if bytes.Contains(ng[:i443], []byte("try_files")) || bytes.Contains(ng[:i443], []byte("fastcgi_pass")) {
		t.Error("server 80 không được chạy site (try_files, fastcgi_pass)")
	}
	// Luật chặn phải đứng trước location PHP của server 443.
	php := bytes.Index(ng, []byte(`location ~ \.php$`))
	for _, s := range []string{"location ~ /\\.(?!", "location ~* ^/wp-content/uploads/"} {
		if i := bytes.Index(ng[i443:], []byte(s)); i < 0 || i443+i > php {
			t.Errorf("nginx SSL: %q phải đứng trước location PHP", s)
		}
	}
	// Nginx 1.24: không có "http2 on;". Không HSTS. Không default_server (dòng lệnh).
	for _, line := range codeLines(ng) {
		if strings.Contains(line, "http2") || strings.Contains(line, "Strict-Transport-Security") || strings.Contains(line, "default_server") {
			t.Errorf("nginx SSL có dòng không được có: %q", line)
		}
	}
}

// Vhost SSL của Apache: cổng 80 chuyển hướng KHÔNG nuốt thử thách, cổng 443 có
// chứng chỉ và đủ luật của bản cổng 80.
func TestApacheSiteSSLDirectives(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	ap, err := ApacheSiteSSL(l, "8.3", acmeRoot, site.HtaccessSafe)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"<VirtualHost *:80>",
		"<VirtualHost *:443>",
		"SSLEngine on",
		`SSLCertificateFile "/etc/letsencrypt/live/example.com/fullchain.pem"`,
		`SSLCertificateKeyFile "/etc/letsencrypt/live/example.com/privkey.pem"`,
		"RewriteEngine On",
		`RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/`,
		"RewriteRule ^/(.*)$ https://example.com/$1 [R=301,L]",
		`Alias /.well-known/acme-challenge/ "` + acmeRoot + `/.well-known/acme-challenge/"`,
		// Luật của bản cổng 80, phải còn nguyên ở 443.
		"ServerName example.com\n",
		`DocumentRoot "` + l.Public + `"`,
		"DirectoryIndex index.php\n",
		`ErrorLog "` + l.LogDir + `/error.log"`,
		`CustomLog "` + l.LogDir + `/access.log" combined`,
		"LimitRequestBody 67108864",
		"AllowOverride All",
		"Options SymLinksIfOwnerMatch",
		`<Directory "` + l.Public + `/wp-content/uploads">`,
		`<FilesMatch "^\.">`,
		`<DirectoryMatch "/\.(?!well-known(/|$))">`,
		`SetHandler "proxy:unix:` + l.SocketFor("8.3") + `|fcgi://localhost"`,
	} {
		if !bytes.Contains(ap, []byte(s)) {
			t.Errorf("apache SSL thiếu %q", s)
		}
	}
	// Thứ tự: RewriteCond đứng ngay trước RewriteRule (điều kiện chỉ áp cho luật
	// liền sau nó), và cả hai nằm trong vhost 80.
	i443 := bytes.Index(ap, []byte("<VirtualHost *:443>"))
	cond := bytes.Index(ap, []byte("RewriteCond"))
	rule := bytes.Index(ap, []byte("RewriteRule"))
	if cond <= 0 || cond >= rule || rule >= i443 {
		t.Errorf("RewriteCond (%d) phải đứng trước RewriteRule (%d) và cả hai trong vhost 80 (443 bắt đầu ở %d)", cond, rule, i443)
	}
	if bytes.Contains(ap[i443:], []byte("Rewrite")) {
		t.Error("vhost 443 không được có luật Rewrite (sẽ thành vòng lặp chuyển hướng)")
	}
	// Redirect thường của mod_alias xử lý trước Alias nên nuốt thử thách: cấm.
	for _, line := range codeLines(ap) {
		if strings.HasPrefix(line, "Redirect") || strings.Contains(line, "Indexes") || strings.Contains(line, "<Proxy") ||
			strings.Contains(line, "FollowSymLinks") || strings.Contains(line, "Strict-Transport-Security") {
			t.Errorf("apache SSL có dòng không được có: %q", line)
		}
	}
}

// Bản SSL không được làm mất luật bảo mật nào của bản cổng 80: mỗi dòng lệnh của
// bản cổng 80 (trừ listen và chuyển hướng, vốn đổi theo thiết kế) còn trong bản SSL.
func TestSSLKeepsPlainRules(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	pairs := map[string]struct {
		plain, ssl func(site.Layout, string) ([]byte, error)
	}{
		"nginx":  {php83(NginxSite), php83(NginxSiteSSL)},
		"apache": {php83(apacheSite), php83(apacheSiteSSL)},
	}
	for name, p := range pairs {
		plain, err := p.plain(l, acmeRoot)
		if err != nil {
			t.Fatal(err)
		}
		withSSL, err := p.ssl(l, acmeRoot)
		if err != nil {
			t.Fatal(err)
		}
		have := map[string]bool{}
		for _, line := range codeLines(withSSL) {
			have[line] = true
		}
		for _, line := range codeLines(plain) {
			if strings.HasPrefix(line, "listen ") {
				continue
			}
			if !have[line] {
				t.Errorf("%s: bản SSL mất dòng của bản cổng 80: %q", name, line)
			}
		}
	}
}

// Site mặc định của cổng 443.
func TestDefaultSSL(t *testing.T) {
	ng, err := NginxDefaultSSL()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"listen 443 ssl default_server;",
		"listen [::]:443 ssl default_server;",
		"server_name _;",
		"ssl_reject_handshake on;",
	} {
		if !bytes.Contains(ng, []byte(s)) {
			t.Errorf("nginx default SSL thiếu %q", s)
		}
	}
	for _, line := range codeLines(ng) {
		if strings.HasPrefix(line, "ssl_certificate") {
			t.Errorf("nginx default SSL không cần chứng chỉ: %q", line)
		}
	}

	ap, err := ApacheDefaultSSL()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"<VirtualHost *:443>",
		"ServerName tpan-default.invalid",
		"SSLEngine on",
		`SSLCertificateFile "` + SnakeoilCert + `"`,
		`SSLCertificateKeyFile "` + SnakeoilKey + `"`,
		"Require all denied",
	} {
		if !bytes.Contains(ap, []byte(s)) {
			t.Errorf("apache default SSL thiếu %q", s)
		}
	}
	if bytes.Contains(ap, []byte("Require all granted")) {
		t.Error("apache default SSL không được mở đường dẫn nào")
	}
}

func TestFail2banSSHD(t *testing.T) {
	out, err := Fail2banSSHD()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"[sshd]", "enabled = true", "backend = systemd"}
	got := codeLines(out)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("dòng lệnh = %q, muốn %q", got, want)
	}
}

// SiteVhost chọn đúng một trong bốn hàm, và web lạ bị từ chối.
func TestSiteVhost(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	cases := []struct {
		web  string
		ssl  bool
		want func(site.Layout, string) ([]byte, error)
	}{
		{"nginx", false, php83(NginxSite)},
		{"nginx", true, php83(NginxSiteSSL)},
		{"apache", false, php83(apacheSite)},
		{"apache", true, php83(apacheSiteSSL)},
	}
	for _, c := range cases {
		got, err := SiteVhost(l, c.web, "8.3", acmeRoot, c.ssl, site.HtaccessSafe)
		if err != nil {
			t.Fatalf("%s ssl=%v: %v", c.web, c.ssl, err)
		}
		want, _ := c.want(l, acmeRoot)
		if !bytes.Equal(got, want) {
			t.Errorf("%s ssl=%v: không khớp hàm tương ứng", c.web, c.ssl)
		}
	}
	for _, web := range []string{"", "Nginx", "nginx ", "apache2", "caddy", "nginx\n", "nginx;"} {
		for _, ssl := range []bool{false, true} {
			if out, err := SiteVhost(l, web, "8.3", acmeRoot, ssl, site.HtaccessSafe); !errors.Is(err, ErrParam) || out != nil {
				t.Errorf("web %q ssl=%v: muốn ErrParam không nội dung, được %v", web, ssl, err)
			}
		}
	}
}

// Layout hỏng, AcmeRoot sai và tpanBin sai đều bị từ chối, không trả nội dung.
func TestSSLBadParams(t *testing.T) {
	good := siteTestLayout(t, "example.com")
	tamper := func(f func(*site.Layout)) site.Layout {
		l := good
		f(&l)
		return l
	}
	bad := map[string]site.Layout{
		"zero":         {},
		"public chèn":  tamper(func(l *site.Layout) { l.Public = "/var/www/example.com/public;\n}" }),
		"user rm":      tamper(func(l *site.Layout) { l.User = "root; rm -rf /" }),
		"dir passwd":   tamper(func(l *site.Layout) { l.Dir = "../../etc/passwd" }),
		"log dir dài":  tamper(func(l *site.Layout) { l.LogDir = "/" + strings.Repeat("a", 300) }),
		"thiếu domain": tamper(func(l *site.Layout) { l.Domain = validate.Domain{} }),
	}
	gens := map[string]func(site.Layout, string) ([]byte, error){
		"nginx SSL":  php83(NginxSiteSSL),
		"apache SSL": php83(apacheSiteSSL),
		"SiteVhost n": func(l site.Layout, r string) ([]byte, error) {
			return SiteVhost(l, "nginx", "8.3", r, true, site.HtaccessSafe)
		},
		"SiteVhost a": func(l site.Layout, r string) ([]byte, error) {
			return SiteVhost(l, "apache", "8.3", r, true, site.HtaccessSafe)
		},
	}
	for name, l := range bad {
		for g, gen := range gens {
			if out, err := gen(l, acmeRoot); !errors.Is(err, ErrParam) || out != nil {
				t.Errorf("%s/%s: muốn ErrParam không nội dung, được %v", name, g, err)
			}
		}
	}
	for _, r := range []string{"", "acme", "/var/lib/acme;", "/var/lib/acme\nserver {", "/var/lib/../etc", "/var/lib/$(id)", `/var/lib/"x"`} {
		for g, gen := range gens {
			if out, err := gen(good, r); !errors.Is(err, ErrParam) || out != nil {
				t.Errorf("%s AcmeRoot %q: muốn ErrParam không nội dung, được %v", g, r, err)
			}
		}
	}
	badBins := []string{
		"", "tpan", "./tpan", "/", "/usr/local/bin/", "/usr//bin/tpan", "/usr/../bin/tpan",
		"/usr/local/bin/tpan; rm -rf /", "/usr/local/bin/tpan\nrm -rf /", "/usr/local/bin/tpan && id",
		"/usr/local/bin/$(id)", "/usr/local/bin/`id`", "/usr/local/bin/tp an", "/usr/local/bin/tpan\t",
		`/usr/local/bin/"tpan"`, "/usr/local/bin/tpan'", "/usr/local/bin/tpan|id", "/usr/local/bin/tạp",
		"/usr/local/bin/{{.Header}}", "/" + strings.Repeat("a", 300) + "/;",
	}
	for _, b := range badBins {
		if out, err := RenewHook(b); !errors.Is(err, ErrParam) || out != nil {
			t.Errorf("RenewHook %q: muốn ErrParam không nội dung, được %v", b, err)
		}
	}
}

// Domain có dấu đi vào vhost SSL ở dạng punycode, kể cả trong đường dẫn chứng chỉ
// (certbot đặt tên lineage theo domain đã chuyển).
func TestSSLIDN(t *testing.T) {
	l := siteTestLayout(t, "cửahàng.vn")
	for name, gen := range map[string]func(site.Layout, string) ([]byte, error){"nginx": php83(NginxSiteSSL), "apache": php83(apacheSiteSSL)} {
		out, err := gen(l, acmeRoot)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Contains(out, []byte("/etc/letsencrypt/live/"+l.Domain.String()+"/fullchain.pem")) || bytes.Contains(out, []byte("cửa")) {
			t.Errorf("%s: domain không ở dạng punycode:\n%s", name, out)
		}
	}
}

// php83 cố định bản PHP 8.3 cho các hàm sinh vhost, để bảng test giữ dạng
// (Layout, acmeRoot) như trước M12.
func php83(f func(site.Layout, string, string) ([]byte, error)) func(site.Layout, string) ([]byte, error) {
	return func(l site.Layout, acmeRoot string) ([]byte, error) { return f(l, "8.3", acmeRoot) }
}
