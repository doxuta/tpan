package templates

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Golden của template M12 (cập nhật bằng cờ -update như TestGolden).
func TestM12Golden(t *testing.T) {
	cases := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"php-idle-pool", func() ([]byte, error) { return PHPIdlePool("8.3") }},
		{"apt-sources-sury", func() ([]byte, error) { return AptSources(suryRepo) }},
		{"apt-sources-nginx", func() ([]byte, error) {
			return AptSources(AptSource{URIs: "https://nginx.org/packages/ubuntu", Suites: "noble", Components: "nginx", KeyPath: "/etc/apt/keyrings/tpan-nginx.gpg"})
		}},
		{"apt-pin-sury", func() ([]byte, error) { return AptPin(suryPin) }},
		{"unattended-upgrades", func() ([]byte, error) { return UnattendedUpgrades(nil) }},
		{"unattended-upgrades-origins", func() ([]byte, error) { return UnattendedUpgrades([]string{"deb.sury.org", "nginx"}) }},
		{"nginx-org-include", NginxOrgInclude},
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
			if !bytes.HasSuffix(got, []byte("\n")) || bytes.HasSuffix(got, []byte("\n\n")) {
				t.Errorf("cuối file phải có đúng một xuống dòng: %q", got[max(0, len(got)-10):])
			}
		})
	}
}

var (
	suryRepo = AptSource{URIs: "https://packages.sury.org/php/", Suites: "noble", Components: "main", KeyPath: "/etc/apt/keyrings/tpan-sury.gpg"}
	suryPin  = []AptPinRule{
		{Packages: []string{"*"}, Origin: "packages.sury.org", Priority: 100},
		{Packages: []string{"php-common", "php8.4*"}, Origin: "packages.sury.org", Priority: 500},
	}
)

// Dòng đầu: pool và unattended-upgrades có dấu chú thích riêng của định dạng,
// nhưng vẫn chứa Header nguyên văn để tpan health nhận ra file của tpan.
func TestM12FirstLine(t *testing.T) {
	cases := map[string]struct {
		gen    func() ([]byte, error)
		prefix string
	}{
		"pool":     {func() ([]byte, error) { return PHPIdlePool("8.3") }, "; "},
		"sources":  {func() ([]byte, error) { return AptSources(suryRepo) }, ""},
		"pin":      {func() ([]byte, error) { return AptPin(suryPin) }, ""},
		"unattend": {func() ([]byte, error) { return UnattendedUpgrades(nil) }, "// "},
		"nginxorg": {NginxOrgInclude, ""},
	}
	for name, c := range cases {
		got, err := c.gen()
		if err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(string(got), "\n"); first != c.prefix+Header {
			t.Errorf("%s: dòng đầu = %q", name, first)
		}
	}
}

// Những dòng mà bỏ đi là hỏng hợp đồng với Step và với phép kiểm (golden bắt
// mọi thay đổi, test này nói rõ dòng nào quan trọng).
func TestM12KeyDirectives(t *testing.T) {
	has := func(name string, got []byte, want ...string) {
		t.Helper()
		for _, s := range want {
			if !bytes.Contains(got, []byte(s)) {
				t.Errorf("%s thiếu %q:\n%s", name, s, got)
			}
		}
	}

	pool, _ := PHPIdlePool("8.4")
	has("pool", pool,
		"\n[tpan-idle]\n", "user = www-data\n", "group = www-data\n",
		"listen = /run/php/tpan-idle-8.4.sock\n",
		"listen.owner = root\n", "listen.group = root\n", "listen.mode = 0600\n",
		"pm = ondemand\n", "pm.max_children = 1\n", "pm.process_idle_timeout = 10s\n")
	// INI của PHP: dòng "# ..." làm php-fpm -t báo "value is NULL".
	for _, ln := range strings.Split(string(pool), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			t.Errorf("pool không được có dòng chú thích bắt đầu bằng #: %q", ln)
		}
	}

	src, _ := AptSources(suryRepo)
	has("sources", src,
		"\nTypes: deb\n", "URIs: https://packages.sury.org/php/\n", "Suites: noble\n",
		"Components: main\n", "Signed-By: /etc/apt/keyrings/tpan-sury.gpg\n")
	multi, err := AptSources(AptSource{URIs: "https://x.example.com", Suites: "noble", Components: "main universe", KeyPath: "/etc/apt/keyrings/a.gpg"})
	if err != nil {
		t.Fatal(err)
	}
	has("sources nhiều component", multi, "Components: main universe\n")

	pin, _ := AptPin(suryPin)
	has("pin", pin,
		"\nExplanation: ",
		"Package: *\nPin: origin packages.sury.org\nPin-Priority: 100\n\nPackage: php-common php8.4*\nPin: origin packages.sury.org\nPin-Priority: 500\n")
	if strings.Count(string(pin), "\n\n") != 1 || strings.Count(string(pin), "Explanation:") != 1 {
		t.Errorf("các đoạn phải cách nhau đúng một dòng trống, và chỉ một dòng Explanation:\n%s", pin)
	}

	un, _ := UnattendedUpgrades(nil)
	has("unattended", un, "APT::Periodic::Update-Package-Lists \"1\";\n", "APT::Periodic::Unattended-Upgrade \"1\";\n")
	if bytes.Contains(un, []byte("Origins-Pattern")) {
		t.Errorf("không có origin thì không được viết Origins-Pattern:\n%s", un)
	}
	un2, _ := UnattendedUpgrades([]string{"deb.sury.org", "Foo Bar:1"})
	has("unattended origins", un2,
		"Unattended-Upgrade::Origins-Pattern {\n", "    \"origin=deb.sury.org\";\n", "    \"origin=Foo Bar:1\";\n", "\n};\n")

	ng, _ := NginxOrgInclude()
	has("nginx.org", ng, "\ninclude /etc/nginx/sites-enabled/*;\n", "\nserver_names_hash_bucket_size 64;\n")
}

// Sửa template đã có (M12): cả hai vhost nginx dùng fastcgi_params cộng dòng
// SCRIPT_FILENAME thay cho fastcgi.conf (nginx.org không có file đó).
func TestM12NginxFastcgi(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	plain, _ := NginxSite(l, "8.3", acmeRoot)
	ssl, err := NginxSiteSSL(l, "8.3", acmeRoot)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"nginx": plain, "nginx-ssl": ssl} {
		if bytes.Contains(b, []byte("include fastcgi.conf;")) {
			t.Errorf("%s: còn include fastcgi.conf", name)
		}
		want := "include fastcgi_params;\n        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n        fastcgi_pass unix:" + l.SocketFor("8.3") + ";"
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("%s thiếu %q:\n%s", name, want, b)
		}
	}
}

// Bản apache của logrotate không đổi so với trước M12.
func TestM12LogrotateApacheUnchanged(t *testing.T) {
	ap, _ := LogrotateSites(Logrotate{Web: "apache"})
	if !bytes.Contains(ap, []byte("invoke-rc.d apache2 reload")) || bytes.Contains(ap, []byte("nginx.pid")) {
		t.Errorf("bản apache sai:\n%s", ap)
	}
}

// Tham số sai phải bị từ chối bằng ErrParam, và không được trả nội dung nào.
func TestM12BadParams(t *testing.T) {
	expect := func(t *testing.T, what string, out []byte, err error) {
		t.Helper()
		if err == nil || !errors.Is(err, ErrParam) {
			t.Errorf("%s: muốn lỗi ErrParam, được err=%v", what, err)
		}
		if out != nil {
			t.Errorf("%s: có lỗi mà vẫn trả nội dung", what)
		}
	}

	t.Run("PHPIdlePool", func(t *testing.T) {
		for _, v := range []string{"", "8", "8.", ".3", "8.3.1", "8.3\n", "8.3\nlisten = x", "8.3;", "$(id)", "8.3 ", "../8.3", "a.b", "8,3", "٨.٣", "{{.Header}}"} {
			out, err := PHPIdlePool(v)
			expect(t, "version "+v, out, err)
		}
	})

	t.Run("AptSources", func(t *testing.T) {
		mod := func(f func(*AptSource)) AptSource { s := suryRepo; f(&s); return s }
		bad := map[string]AptSource{
			"URIs rỗng":              mod(func(s *AptSource) { s.URIs = "" }),
			"URIs http":              mod(func(s *AptSource) { s.URIs = "http://packages.sury.org/php/" }),
			"URIs file":              mod(func(s *AptSource) { s.URIs = "file:///srv/repo" }),
			"URIs không scheme":      mod(func(s *AptSource) { s.URIs = "packages.sury.org/php/" }),
			"URIs khoảng trắng":      mod(func(s *AptSource) { s.URIs = "https://a.com/ https://evil.com/" }),
			"URIs xuống dòng":        mod(func(s *AptSource) { s.URIs = "https://a.com/\nSuites: evil" }),
			"URIs dấu ;":             mod(func(s *AptSource) { s.URIs = "https://a.com/;rm" }),
			"URIs $(...)":            mod(func(s *AptSource) { s.URIs = "https://a.com/$(id)" }),
			"URIs thoát lên":         mod(func(s *AptSource) { s.URIs = "https://a.com/../x" }),
			"URIs userinfo":          mod(func(s *AptSource) { s.URIs = "https://user:pw@a.com/" }),
			"URIs tên máy rỗng":      mod(func(s *AptSource) { s.URIs = "https:///php/" }),
			"URIs ký tự lạ":          mod(func(s *AptSource) { s.URIs = "https://a.com/tạp" }),
			"URIs cú pháp tmpl":      mod(func(s *AptSource) { s.URIs = "https://a.com/{{.Header}}" }),
			"Suites rỗng":            mod(func(s *AptSource) { s.Suites = "" }),
			"Suites hoa":             mod(func(s *AptSource) { s.Suites = "Noble" }),
			"Suites xuống dòng":      mod(func(s *AptSource) { s.Suites = "noble\nSigned-By: /x" }),
			"Suites hai từ":          mod(func(s *AptSource) { s.Suites = "noble jammy" }),
			"Suites $(...)":          mod(func(s *AptSource) { s.Suites = "$(id)" }),
			"Suites dấu ;":           mod(func(s *AptSource) { s.Suites = "noble;" }),
			"Components rỗng":        mod(func(s *AptSource) { s.Components = "" }),
			"Components hai space":   mod(func(s *AptSource) { s.Components = "main  universe" }),
			"Components cuối space":  mod(func(s *AptSource) { s.Components = "main " }),
			"Components xuống dòng":  mod(func(s *AptSource) { s.Components = "main\nTypes: deb-src" }),
			"Components tab":         mod(func(s *AptSource) { s.Components = "main\tuniverse" }),
			"Components $(...)":      mod(func(s *AptSource) { s.Components = "$(id)" }),
			"KeyPath rỗng":           mod(func(s *AptSource) { s.KeyPath = "" }),
			"KeyPath tương đối":      mod(func(s *AptSource) { s.KeyPath = "etc/apt/keyrings/x.gpg" }),
			"KeyPath ngoài keyrings": mod(func(s *AptSource) { s.KeyPath = "/etc/apt/trusted.gpg.d/x.gpg" }),
			"KeyPath chỉ thư mục":    mod(func(s *AptSource) { s.KeyPath = "/etc/apt/keyrings/" }),
			"KeyPath thoát lên":      mod(func(s *AptSource) { s.KeyPath = "/etc/apt/keyrings/../x.gpg" }),
			"KeyPath xuống dòng":     mod(func(s *AptSource) { s.KeyPath = "/etc/apt/keyrings/x.gpg\nURIs: evil" }),
			"KeyPath khoảng trắng":   mod(func(s *AptSource) { s.KeyPath = "/etc/apt/keyrings/a b.gpg" }),
			"KeyPath $(...)":         mod(func(s *AptSource) { s.KeyPath = "/etc/apt/keyrings/$(id).gpg" }),
			"zero value":             {},
		}
		for name, s := range bad {
			out, err := AptSources(s)
			expect(t, name, out, err)
		}
	})

	t.Run("AptPin", func(t *testing.T) {
		rule := func(pk []string, origin string, pri int) []AptPinRule {
			return []AptPinRule{{Packages: pk, Origin: origin, Priority: pri}}
		}
		bad := map[string][]AptPinRule{
			"nil":                   nil,
			"không luật":            {},
			"không gói":             rule(nil, "a.com", 100),
			"gói rỗng":              rule([]string{""}, "a.com", 100),
			"gói hoa":               rule([]string{"PHP"}, "a.com", 100),
			"gói có space":          rule([]string{"php8.4 evil"}, "a.com", 100),
			"gói xuống dòng":        rule([]string{"php\nPin: release a=x"}, "a.com", 100),
			"gói dấu ;":             rule([]string{"php;"}, "a.com", 100),
			"gói $(...)":            rule([]string{"$(id)"}, "a.com", 100),
			"gói bắt đầu bằng -":    rule([]string{"-php"}, "a.com", 100),
			"gói có /":              rule([]string{"../php"}, "a.com", 100),
			"gói có dấu :":          rule([]string{"php:amd64"}, "a.com", 100),
			"origin rỗng":           rule([]string{"*"}, "", 100),
			"origin có space":       rule([]string{"*"}, "a.com b.com", 100),
			"origin xuống dòng":     rule([]string{"*"}, "a.com\nPin-Priority: 1000", 100),
			"origin có scheme":      rule([]string{"*"}, "https://a.com", 100),
			"origin có /":           rule([]string{"*"}, "a.com/x", 100),
			"origin $(...)":         rule([]string{"*"}, "$(id)", 100),
			"origin bắt đầu bằng .": rule([]string{"*"}, ".a.com", 100),
			"priority 0":            rule([]string{"*"}, "a.com", 0),
			"priority âm":           rule([]string{"*"}, "a.com", -1),
			"priority 1001":         rule([]string{"*"}, "a.com", 1001),
			"một luật sai trong nhiều": {
				{Packages: []string{"*"}, Origin: "a.com", Priority: 100},
				{Packages: []string{"x"}, Origin: "a.com", Priority: 5000},
			},
		}
		for name, r := range bad {
			out, err := AptPin(r)
			expect(t, name, out, err)
		}
	})

	t.Run("UnattendedUpgrades", func(t *testing.T) {
		for _, o := range []string{
			"", " ", "a b ", " a", "a  b", "a\nb", "a\"; Unattended-Upgrade::Remove-Unused-Dependencies \"true", "a;", "a\"b", "a'b",
			"$(id)", "${distro_id}", "a,codename=x", "a=b", "a/b", "a\\b", "a\tb", "tạp", "{{.Header}}", "a#b", "a//b",
		} {
			out, err := UnattendedUpgrades([]string{"deb.sury.org", o})
			expect(t, "origin "+o, out, err)
		}
	})
}

// Tham số lạ nhưng hợp lệ vẫn được nhận.
func TestM12GoodParams(t *testing.T) {
	for _, v := range []string{"8.3", "8.4", "7.4", "10.12"} {
		if _, err := PHPIdlePool(v); err != nil {
			t.Errorf("version %q: %v", v, err)
		}
	}
	for _, u := range []string{"https://packages.sury.org/php/", "https://nginx.org/packages/ubuntu", "https://dlm.mariadb.com/repo/mariadb-server/11.8/repo/ubuntu", "https://a.example.com:8443/x_y~z+1"} {
		s := suryRepo
		s.URIs = u
		if _, err := AptSources(s); err != nil {
			t.Errorf("URIs %q: %v", u, err)
		}
	}
	if _, err := AptPin([]AptPinRule{{Packages: []string{"php8.4*", "php-common", "libapache2-mod-php8.4"}, Origin: "deb.sury.org", Priority: 1000}}); err != nil {
		t.Errorf("luật hợp lệ bị từ chối: %v", err)
	}
	if _, err := UnattendedUpgrades([]string{"Ubuntu", "LP-PPA-ondrej-php", "MariaDB", "a:b_c.d-e"}); err != nil {
		t.Errorf("origin hợp lệ bị từ chối: %v", err)
	}
}
