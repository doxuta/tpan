package templates

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -update ghi lại các file golden. Đổi template thì chạy:
//
//	go test ./internal/templates/ -update
//
// rồi đọc `git diff internal/templates/testdata` trước khi commit.
var update = flag.Bool("update", false, "ghi lại file golden")

const acmeRoot = "/var/lib/tpan/acme"

func TestGolden(t *testing.T) {
	cases := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"nginx-default", func() ([]byte, error) { return NginxDefault(DefaultSite{AcmeRoot: acmeRoot}) }},
		{"apache-default", func() ([]byte, error) { return ApacheDefault(DefaultSite{AcmeRoot: acmeRoot}) }},
		{"logrotate-nginx", func() ([]byte, error) { return LogrotateSites(Logrotate{Web: "nginx"}) }},
		{"logrotate-apache", func() ([]byte, error) { return LogrotateSites(Logrotate{Web: "apache"}) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.gen()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", c.name+".golden")
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("đọc golden (chạy với -update để tạo): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("khác golden %s (chạy với -update rồi đọc git diff):\n--- got ---\n%s", path, got)
			}
		})
	}
}

// Mọi file sinh ra phải bắt đầu đúng bằng dòng Header nguyên văn (mục 5).
func TestFirstLine(t *testing.T) {
	const want = "# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch."
	if Header != want {
		t.Fatalf("Header = %q", Header)
	}
	outs := map[string]func() ([]byte, error){
		"nginx":     func() ([]byte, error) { return NginxDefault(DefaultSite{AcmeRoot: acmeRoot}) },
		"apache":    func() ([]byte, error) { return ApacheDefault(DefaultSite{AcmeRoot: acmeRoot}) },
		"logrotate": func() ([]byte, error) { return LogrotateSites(Logrotate{Web: "nginx"}) },
	}
	for name, gen := range outs {
		got, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(string(got), "\n"); first != want {
			t.Errorf("%s: dòng đầu = %q", name, first)
		}
		if !bytes.HasSuffix(got, []byte("\n")) {
			t.Errorf("%s: thiếu xuống dòng ở cuối file", name)
		}
	}
}

// AcmeRoot khác mặc định phải đi vào đúng chỗ, không còn sót đường dẫn cũ.
func TestAcmeRootUsed(t *testing.T) {
	d := DefaultSite{AcmeRoot: "/srv/acme-x"}
	for name, gen := range map[string]func(DefaultSite) ([]byte, error){"nginx": NginxDefault, "apache": ApacheDefault} {
		got, err := gen(d)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(got, []byte("/srv/acme-x")) || bytes.Contains(got, []byte("/var/lib/tpan")) {
			t.Errorf("%s: AcmeRoot không vào đúng chỗ:\n%s", name, got)
		}
	}
}

// Nội dung cốt lõi theo mục 6.1 bước 5: những dòng mà bỏ đi là site mặc định
// mất tác dụng. Golden bắt mọi thay đổi, test này nói rõ dòng nào quan trọng.
func TestKeyDirectives(t *testing.T) {
	ng, _ := NginxDefault(DefaultSite{AcmeRoot: acmeRoot})
	for _, s := range []string{
		"listen 80 default_server;",
		"listen [::]:80 default_server;",
		"server_name _;",
		"location /.well-known/acme-challenge/ {",
		"root " + acmeRoot + ";",
		"return 444;",
	} {
		if !bytes.Contains(ng, []byte(s)) {
			t.Errorf("nginx thiếu %q", s)
		}
	}
	ap, _ := ApacheDefault(DefaultSite{AcmeRoot: acmeRoot})
	for _, s := range []string{
		"<VirtualHost *:80>",
		"ServerName ",
		"Require all denied",
		"Require all granted",
		`<Directory "` + acmeRoot + `/.well-known/acme-challenge">`,
		"Options None",
	} {
		if !bytes.Contains(ap, []byte(s)) {
			t.Errorf("apache thiếu %q", s)
		}
	}
	// Chú thích được phép nhắc tới Indexes; chỉ dòng lệnh mới không được bật nó.
	for _, line := range strings.Split(string(ap), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") && strings.Contains(line, "Indexes") {
			t.Errorf("apache không được bật Indexes: %q", line)
		}
	}
}

// Hai bản logrotate chỉ khác nhau ở cách báo web server mở lại log.
func TestLogrotateByWeb(t *testing.T) {
	ng, _ := LogrotateSites(Logrotate{Web: "nginx"})
	ap, _ := LogrotateSites(Logrotate{Web: "apache"})
	if !bytes.Contains(ng, []byte("invoke-rc.d nginx rotate")) || bytes.Contains(ng, []byte("apache2")) {
		t.Errorf("bản nginx sai:\n%s", ng)
	}
	if !bytes.Contains(ap, []byte("invoke-rc.d apache2 reload")) || bytes.Contains(ap, []byte("invoke-rc.d nginx")) {
		t.Errorf("bản apache sai:\n%s", ap)
	}
	for name, b := range map[string][]byte{"nginx": ng, "apache": ap} {
		for _, s := range []string{"/var/log/tpan/sites/*/php-error.log", "copytruncate", "sharedscripts"} {
			if !bytes.Contains(b, []byte(s)) {
				t.Errorf("%s: thiếu %q", name, s)
			}
		}
	}
}

// Tham số sai phải bị từ chối, và không được trả nội dung nào kèm lỗi.
func TestBadParams(t *testing.T) {
	badRoots := []string{
		"",
		"var/lib/tpan/acme",                  // tương đối
		"./acme",                             // tương đối
		"/",                                  // cả ổ đĩa
		"/var/lib/tpan/acme/",                // dấu / cuối
		"//var/lib",                          // đoạn rỗng
		"/var//lib",                          // đoạn rỗng
		"/var/lib/../etc",                    // thoát lên
		"/var/./lib",                         // đoạn "."
		"/var/lib/tpan/acme\n",               // xuống dòng
		"/var/lib/tpan/acme\nserver {",       // chèn cấu hình
		"/var/lib/tpan/acme\r",               // CR
		"/var/lib/tpan/acme ",                // khoảng trắng cuối
		"/var/lib/tpan acme",                 // khoảng trắng giữa
		"/var/lib/tpan/acme\t",               // tab
		"/var/lib/tpan/acme;",                // dấu ;
		"/var/lib/acme; return 200 'x'",      // chèn directive nginx
		"/var/lib/$HOME",                     // biến
		"/var/lib/{x}",                       // ngoặc nhọn
		`/var/lib/"x"`,                       // nháy kép (phá Directory của Apache)
		"/var/lib/a'b",                       // nháy đơn
		"/var/lib/a#b",                       // dấu #
		`/var/lib/a\b`,                       // backslash
		"/var/lib/a\x00b",                    // NUL
		"/var/lib/tạp",                       // ký tự không phải ASCII
		"/var/lib/{{.Header}}",               // cú pháp template
		"/var/lib/$(id)",                     // thay thế lệnh
		"/var/lib/a>b",                       // ký tự đặc biệt khác
		"/" + strings.Repeat("a/", 3) + "..", // .. ở cuối
	}
	gens := map[string]func(DefaultSite) ([]byte, error){"nginx": NginxDefault, "apache": ApacheDefault}
	for name, gen := range gens {
		for _, r := range badRoots {
			out, err := gen(DefaultSite{AcmeRoot: r})
			if err == nil || !errors.Is(err, ErrParam) {
				t.Errorf("%s AcmeRoot %q: muốn lỗi ErrParam, được err=%v", name, r, err)
			}
			if out != nil {
				t.Errorf("%s AcmeRoot %q: có lỗi mà vẫn trả nội dung", name, r)
			}
		}
	}

	badWeb := []string{"", "Nginx", "NGINX", "nginx ", " apache", "nginx\n", "apache2", "caddy", "nginx;", "nginx apache", "../nginx"}
	for _, w := range badWeb {
		out, err := LogrotateSites(Logrotate{Web: w})
		if err == nil || !errors.Is(err, ErrParam) {
			t.Errorf("Web %q: muốn lỗi ErrParam, được err=%v", w, err)
		}
		if out != nil {
			t.Errorf("Web %q: có lỗi mà vẫn trả nội dung", w)
		}
	}
}

// Đường dẫn hợp lệ nhưng khác thường vẫn được nhận.
func TestGoodRoots(t *testing.T) {
	for _, r := range []string{"/var/lib/tpan/acme", "/srv/acme", "/a", "/opt/tpan-1.2/acme_x", "/var/lib/.hidden"} {
		if _, err := NginxDefault(DefaultSite{AcmeRoot: r}); err != nil {
			t.Errorf("nginx %q: %v", r, err)
		}
		if _, err := ApacheDefault(DefaultSite{AcmeRoot: r}); err != nil {
			t.Errorf("apache %q: %v", r, err)
		}
	}
}
