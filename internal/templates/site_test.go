package templates

import (
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

func siteTestLayout(t *testing.T, domain string) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain(domain)
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

// Golden của bốn file site; cập nhật bằng cờ -update như TestGolden.
func TestSiteGolden(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	cases := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"site-pool", func() ([]byte, error) { return SitePool(l, "8.3") }},
		{"site-nginx", func() ([]byte, error) { return NginxSite(l, "8.3", acmeRoot) }},
		{"site-apache", func() ([]byte, error) { return ApacheSite(l, "8.3", acmeRoot, site.HtaccessSafe) }},
		{"site-htaccess", func() ([]byte, error) { return WPHtaccess(), nil }},
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

// Dòng đầu: Header nguyên văn cho vhost và .htaccess; pool là "; "+Header vì
// INI của PHP không nhận dấu # làm chú thích.
func TestSiteFirstLine(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	pool, _ := SitePool(l, "8.3")
	ng, _ := NginxSite(l, "8.3", acmeRoot)
	ap, _ := ApacheSite(l, "8.3", acmeRoot, site.HtaccessSafe)
	for name, c := range map[string]struct {
		out  []byte
		want string
	}{
		"pool":     {pool, "; " + Header},
		"nginx":    {ng, Header},
		"apache":   {ap, Header},
		"htaccess": {WPHtaccess(), Header},
	} {
		if first, _, _ := strings.Cut(string(c.out), "\n"); first != c.want {
			t.Errorf("%s: dòng đầu = %q, muốn %q", name, first, c.want)
		}
	}
	// Không dòng nào của pool bắt đầu bằng # (php-fpm -t sẽ lỗi "value is NULL").
	for _, line := range strings.Split(string(pool), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("pool có dòng bắt đầu bằng #: %q", line)
		}
	}
}

// Những dòng mà bỏ đi là mất cô lập hay mất tính năng (mục 5, 6.2).
func TestSiteKeyDirectives(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	pool, _ := SitePool(l, "8.3")
	for _, s := range []string{
		"[" + l.Pool + "]",
		"user = " + l.User,
		"group = " + l.User,
		"listen = " + l.SocketFor("8.3"),
		"listen.owner = www-data",
		"listen.group = www-data",
		"listen.mode = 0660",
		"pm = ondemand",
		"pm.max_children = 5",
		"pm.process_idle_timeout = 10s",
		"pm.max_requests = 500",
		"php_admin_value[open_basedir] = " + l.Dir + "/\n",
		"php_admin_value[upload_tmp_dir] = " + l.Tmp,
		"php_admin_value[session.save_path] = " + l.Tmp,
		"php_admin_value[error_log] = " + l.PHPErrorLog,
	} {
		if !bytes.Contains(pool, []byte(s)) {
			t.Errorf("pool thiếu %q", s)
		}
	}

	ng, _ := NginxSite(l, "8.3", acmeRoot)
	for _, s := range []string{
		"listen 80;",
		"listen [::]:80;",
		"server_name example.com;",
		"root " + l.Public + ";",
		"access_log " + l.LogDir + "/access.log;",
		"error_log " + l.LogDir + "/error.log;",
		"client_max_body_size 64m;",
		"location ^~ /.well-known/acme-challenge/ {\n        root " + acmeRoot + ";",
		"location ~ /\\.(?!well-known/) {\n        deny all;",
		"location ~* ^/wp-content/uploads/.*\\.php$ {\n        deny all;",
		"try_files $uri $uri/ /index.php?$args;",
		"fastcgi_pass unix:" + l.SocketFor("8.3") + ";",
	} {
		if !bytes.Contains(ng, []byte(s)) {
			t.Errorf("nginx thiếu %q", s)
		}
	}
	// Chỉ dòng lệnh mới bị xét; chú thích được nhắc tới default_server.
	for _, line := range strings.Split(string(ng), "\n") {
		if code := strings.TrimSpace(line); !strings.HasPrefix(code, "#") && strings.Contains(code, "default_server") {
			t.Errorf("vhost của site không được là default_server: %q", line)
		}
	}
	// Luật chặn phải đứng trước location PHP (regex xét theo thứ tự).
	php := bytes.Index(ng, []byte(`location ~ \.php$`))
	for _, s := range []string{"location ~ /\\.(?!", "location ~* ^/wp-content/uploads/"} {
		if i := bytes.Index(ng, []byte(s)); i < 0 || i > php {
			t.Errorf("nginx: %q phải đứng trước location PHP", s)
		}
	}

	ap, _ := ApacheSite(l, "8.3", acmeRoot, site.HtaccessSafe)
	for _, s := range []string{
		"<VirtualHost *:80>",
		"ServerName example.com\n",
		`DocumentRoot "` + l.Public + `"`,
		"DirectoryIndex index.php\n",
		`ErrorLog "` + l.LogDir + `/error.log"`,
		`CustomLog "` + l.LogDir + `/access.log" combined`,
		"LimitRequestBody 67108864",
		`Alias /.well-known/acme-challenge/ "` + acmeRoot + `/.well-known/acme-challenge/"`,
		"AllowOverride All",
		"Options SymLinksIfOwnerMatch",
		`<Directory "` + l.Public + `/wp-content/uploads">`,
		`SetHandler "proxy:unix:` + l.SocketFor("8.3") + `|fcgi://localhost"`,
		`<FilesMatch "^\.">`,
	} {
		if !bytes.Contains(ap, []byte(s)) {
			t.Errorf("apache thiếu %q", s)
		}
	}
	for _, line := range strings.Split(string(ap), "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "#") {
			continue
		}
		if strings.Contains(code, "Indexes") || strings.Contains(code, "<Proxy") || strings.Contains(code, "FollowSymLinks") {
			t.Errorf("apache có dòng không được có: %q", line)
		}
	}

	ht := WPHtaccess()
	for _, s := range []string{"# BEGIN WordPress", "RewriteEngine On", "RewriteRule . /index.php [L]", "# END WordPress"} {
		if !bytes.Contains(ht, []byte(s)) {
			t.Errorf(".htaccess thiếu %q", s)
		}
	}
}

// Log của vhost phải khớp mẫu glob trong /etc/logrotate.d/tpan-sites (M1),
// không thì log của site lớn mãi.
func TestSiteLogsMatchLogrotate(t *testing.T) {
	lr, err := LogrotateSites(Logrotate{Web: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	globs := []string{"/var/log/tpan/sites/*/access.log", "/var/log/tpan/sites/*/error.log", "/var/log/tpan/sites/*/php-error.log"}
	for _, g := range globs {
		if !bytes.Contains(lr, []byte(g)) {
			t.Fatalf("logrotate không còn mẫu %q", g)
		}
	}
	l := siteTestLayout(t, "shop.example.com")
	for i, p := range []string{l.LogDir + "/access.log", l.LogDir + "/error.log", l.PHPErrorLog} {
		if ok, err := path.Match(globs[i], p); err != nil || !ok {
			t.Errorf("%s không khớp %s", p, globs[i])
		}
	}
}

// Domain có dấu đi vào config ở dạng punycode, không ở dạng Unicode.
func TestSiteIDN(t *testing.T) {
	l := siteTestLayout(t, "cửahàng.vn")
	for name, gen := range map[string]func() ([]byte, error){
		"nginx":  func() ([]byte, error) { return NginxSite(l, "8.3", acmeRoot) },
		"apache": func() ([]byte, error) { return ApacheSite(l, "8.3", acmeRoot, site.HtaccessSafe) },
		"pool":   func() ([]byte, error) { return SitePool(l, "8.3") },
	} {
		out, err := gen()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Contains(out, []byte(l.Domain.String())) || !strings.HasPrefix(l.Domain.String(), "xn--") {
			t.Errorf("%s: không có domain punycode %q", name, l.Domain)
		}
		if bytes.Contains(out, []byte("cửa")) {
			t.Errorf("%s: còn domain Unicode", name)
		}
	}
}

// Layout zero, Layout bị sửa tay và AcmeRoot sai đều phải bị từ chối, không trả nội dung.
func TestSiteBadParams(t *testing.T) {
	good := siteTestLayout(t, "example.com")
	tamper := func(f func(*site.Layout)) site.Layout {
		l := good
		f(&l)
		return l
	}
	bad := map[string]site.Layout{
		"zero":            {},
		"public chèn":     tamper(func(l *site.Layout) { l.Public = "/var/www/example.com/public;\n}" }),
		"public thoát":    tamper(func(l *site.Layout) { l.Public = "/var/www/example.com/../../etc" }),
		"user rm":         tamper(func(l *site.Layout) { l.User = "root; rm -rf /" }),
		"user khác":       tamper(func(l *site.Layout) { l.User = "www-data" }),
		"pool xuống dòng": tamper(func(l *site.Layout) { l.Pool = "tpan-x]\n[www" }),
		"dir passwd":      tamper(func(l *site.Layout) { l.Dir = "../../etc/passwd" }),
		"log dir dài":     tamper(func(l *site.Layout) { l.LogDir = "/" + strings.Repeat("a", 300) }),
		"thiếu domain":    tamper(func(l *site.Layout) { l.Domain = validate.Domain{} }),
	}
	for name, l := range bad {
		gens := map[string]func() ([]byte, error){
			"pool":   func() ([]byte, error) { return SitePool(l, "8.3") },
			"nginx":  func() ([]byte, error) { return NginxSite(l, "8.3", acmeRoot) },
			"apache": func() ([]byte, error) { return ApacheSite(l, "8.3", acmeRoot, site.HtaccessSafe) },
		}
		for g, gen := range gens {
			out, err := gen()
			if !errors.Is(err, ErrParam) {
				t.Errorf("%s/%s: muốn ErrParam, được %v", name, g, err)
			}
			if out != nil {
				t.Errorf("%s/%s: lỗi mà vẫn trả nội dung", name, g)
			}
		}
	}
	for _, r := range []string{"", "acme", "/var/lib/acme;", "/var/lib/acme\nserver {", "/var/lib/../etc", "/var/lib/$(id)", `/var/lib/"x"`} {
		for g, gen := range map[string]func(site.Layout, string, string) ([]byte, error){"nginx": NginxSite, "apache": apacheSite} {
			if out, err := gen(good, "8.3", r); !errors.Is(err, ErrParam) || out != nil {
				t.Errorf("%s AcmeRoot %q: muốn ErrParam không nội dung, được %v", g, r, err)
			}
		}
	}
	// Bản PHP đi vào đường dẫn socket: chỉ nhận dạng 8.3.
	for _, php := range []string{"", "8", "8.3; id", "8.3\n", "../8.3", "$(id)"} {
		gens := map[string]func() ([]byte, error){
			"pool":   func() ([]byte, error) { return SitePool(good, php) },
			"nginx":  func() ([]byte, error) { return NginxSite(good, php, acmeRoot) },
			"apache": func() ([]byte, error) { return ApacheSite(good, php, acmeRoot, site.HtaccessSafe) },
		}
		for g, gen := range gens {
			if out, err := gen(); !errors.Is(err, ErrParam) || out != nil {
				t.Errorf("%s php %q: muốn ErrParam không nội dung, được %v", g, php, err)
			}
		}
	}
}
