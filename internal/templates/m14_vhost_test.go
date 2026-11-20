package templates

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/site"
)

// Test của các dòng chặn M14 trong vhost của site (hợp đồng ở m14_types.go).
// Phép kiểm checks.VhostHardened so "có chứa" với đúng các hằng dùng ở đây, nên
// test này là thứ giữ cho template và phép kiểm không lệch nhau.

// Vhost nginx: dòng chặn symlink đứng ngay sau dòng root của site, đúng một
// lần; ở bản SSL nó nằm trong khối cổng 443 (khối cổng 80 không có root).
func TestNginxSymlinkGuard(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for name, gen := range map[string]func(site.Layout, string, string) ([]byte, error){
		"cổng 80": NginxSite,
		"ssl":     NginxSiteSSL,
	} {
		out, err := gen(l, "8.3", acmeRoot)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		lines := codeLines(out)
		if n := countLines(lines, NginxSymlinkGuard); n != 1 {
			t.Errorf("%s: dòng %q xuất hiện %d lần, muốn 1", name, NginxSymlinkGuard, n)
		}
		i := slices.Index(lines, "root "+l.Public+";")
		if i < 0 || i+1 >= len(lines) || lines[i+1] != NginxSymlinkGuard {
			t.Errorf("%s: dòng lệnh ngay sau root phải là %q", name, NginxSymlinkGuard)
		}
		// Cấp server: thụt đúng 4 dấu cách, không nằm trong location nào.
		if !bytes.Contains(out, []byte("\n    "+NginxSymlinkGuard+"\n")) {
			t.Errorf("%s: dòng chặn symlink phải ở cấp server (thụt 4 dấu cách)", name)
		}
	}
	ssl, _ := NginxSiteSSL(l, "8.3", acmeRoot)
	if i443 := bytes.Index(ssl, []byte("listen 443 ssl;")); bytes.Contains(ssl[:i443], []byte("disable_symlinks")) {
		t.Error("khối cổng 80 của bản SSL chỉ chuyển hướng, không có root: không thêm disable_symlinks")
	}
}

// apacheGens là hai hàm sinh vhost Apache; ở bản SSL các luật của site nằm
// trong <VirtualHost *:443>.
var apacheGens = map[string]func(site.Layout, string, string, site.HtaccessMode) ([]byte, error){
	"cổng 80": ApacheSite,
	"ssl":     ApacheSiteSSL,
}

// publicDir trả nội dung khối <Directory "<public>"> cuối cùng của vhost (bản
// SSL chỉ có khối này trong vhost 443).
func publicDir(t *testing.T, out []byte, l site.Layout) []byte {
	t.Helper()
	open := []byte(`<Directory "` + l.Public + `">`)
	if n := bytes.Count(out, open); n != 1 {
		t.Fatalf("khối %s xuất hiện %d lần, muốn 1", open, n)
	}
	rest := out[bytes.Index(out, open):]
	return rest[:bytes.Index(rest, []byte("</Directory>"))]
}

func countLines(lines []string, want string) int {
	n := 0
	for _, ln := range lines {
		if ln == want {
			n++
		}
	}
	return n
}

// Vhost Apache: hai dòng gỡ biến có ở cả ba chế độ, ở cấp VirtualHost, và (bản
// SSL) trong vhost 443.
func TestApacheFCGIGuards(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for name, gen := range apacheGens {
		for _, mode := range []site.HtaccessMode{"", site.HtaccessSafe, site.HtaccessOff, site.HtaccessFull} {
			out, err := gen(l, "8.3", acmeRoot, mode)
			if err != nil {
				t.Fatalf("%s %q: %v", name, mode, err)
			}
			lines := codeLines(out)
			for _, g := range ApacheFCGIGuards {
				if n := countLines(lines, g); n != 1 {
					t.Errorf("%s %q: dòng %q xuất hiện %d lần, muốn 1", name, mode, g, n)
				}
				// Cấp VirtualHost: thụt đúng 4 dấu cách. Trong <Directory> hay
				// <FilesMatch> thì .htaccess cùng cấp có cơ hội đè lại.
				i := bytes.Index(out, []byte("\n    "+g+"\n"))
				if i < 0 {
					t.Errorf("%s %q: dòng %q phải ở cấp VirtualHost (thụt 4 dấu cách)", name, mode, g)
				}
				if i443 := bytes.Index(out, []byte("<VirtualHost *:443>")); i443 >= 0 && i < i443 {
					t.Errorf("%s %q: dòng %q phải nằm trong vhost 443", name, mode, g)
				}
			}
		}
	}
}

// Khối <Directory "<public>"> theo chế độ .htaccess (mục 6.13).
func TestApacheHtaccessModes(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for name, gen := range apacheGens {
		gen1 := func(mode site.HtaccessMode) ([]byte, []string) {
			t.Helper()
			out, err := gen(l, "8.3", acmeRoot, mode)
			if err != nil {
				t.Fatalf("%s %q: %v", name, mode, err)
			}
			return out, codeLines(publicDir(t, out, l))
		}

		// an-toan: Options đi qua AllowOverride với đúng ba tuỳ chọn, mọi chỉ
		// thị khác qua MỘT dòng AllowOverrideList có đủ và đúng các tên.
		safe, dir := gen1(site.HtaccessSafe)
		if n := countLines(dir, "AllowOverride "+htaccess.OverrideOptions()); n != 1 {
			t.Errorf("%s an-toan: thiếu dòng AllowOverride %s", name, htaccess.OverrideOptions())
		}
		var lists [][]string
		for _, ln := range codeLines(safe) {
			if f := strings.Fields(ln); f[0] == "AllowOverrideList" {
				lists = append(lists, f[1:])
			}
		}
		// Apache: dòng AllowOverrideList sau THAY dòng trước, nên hai dòng là mất tên.
		if len(lists) != 1 {
			t.Fatalf("%s an-toan: có %d dòng AllowOverrideList, muốn đúng 1", name, len(lists))
		}
		if len(lists[0]) != len(htaccess.Allowed) || !slices.Equal(lists[0], htaccess.Allowed) {
			t.Errorf("%s an-toan: AllowOverrideList có %d tên, muốn đúng %d tên của htaccess.Allowed:\n%q", name, len(lists[0]), len(htaccess.Allowed), lists[0])
		}
		if countLines(dir, "AllowOverrideList "+htaccess.List()) != 1 {
			t.Errorf("%s an-toan: dòng AllowOverrideList phải nằm trong khối <Directory> của public", name)
		}
		if slices.Contains(lists[0], "Options") {
			t.Errorf("%s an-toan: AllowOverrideList không được có Options (lỗ L9)", name)
		}

		// Chế độ rỗng là an-toan.
		if empty, _ := gen1(""); !bytes.Equal(empty, safe) {
			t.Errorf("%s: chế độ rỗng phải cho đúng bản an-toan", name)
		}

		// tat: không đọc .htaccess, permalink bằng FallbackResource.
		off, dir := gen1(site.HtaccessOff)
		if countLines(dir, "AllowOverride None") != 1 || countLines(dir, "FallbackResource /index.php") != 1 {
			t.Errorf("%s tat: khối public phải có AllowOverride None và FallbackResource /index.php, được %q", name, dir)
		}

		// day-du: như trước M14.
		full, dir := gen1(site.HtaccessFull)
		if countLines(dir, "AllowOverride All") != 1 {
			t.Errorf("%s day-du: khối public phải có AllowOverride All, được %q", name, dir)
		}

		// Các chuỗi mà checks.VhostHardened so "có chứa" trên cả file: không
		// được xuất hiện ở chế độ khác, KỂ CẢ trong chú thích.
		for _, c := range []struct {
			mode string
			out  []byte
			bad  []string
		}{
			{"an-toan", safe, []string{"AllowOverride All", "FallbackResource"}},
			{"tat", off, []string{"AllowOverride All", "AllowOverrideList"}},
			{"day-du", full, []string{"AllowOverrideList", "FallbackResource"}},
		} {
			for _, bad := range c.bad {
				if bytes.Contains(c.out, []byte(bad)) {
					t.Errorf("%s %s: không được chứa %q (kể cả trong chú thích)", name, c.mode, bad)
				}
			}
		}

		// Thứ không đổi theo chế độ: Options của vhost, và uploads vẫn khoá.
		for mode, out := range map[string][]byte{"an-toan": safe, "tat": off, "day-du": full} {
			if countLines(codeLines(publicDir(t, out, l)), "Options SymLinksIfOwnerMatch") != 1 {
				t.Errorf("%s %s: khối public mất Options SymLinksIfOwnerMatch", name, mode)
			}
			up := out[bytes.Index(out, []byte(`<Directory "`+l.Public+`/wp-content/uploads">`)):]
			if lines := codeLines(up); len(lines) < 2 || lines[1] != "AllowOverride None" {
				t.Errorf("%s %s: khối uploads phải giữ AllowOverride None", name, mode)
			}
		}
	}
}

// Bản SSL có đủ mọi dòng lệnh của bản cổng 80 ở cả ba chế độ (TestSSLKeepsPlainRules
// chỉ xét chế độ mặc định).
func TestApacheSSLKeepsPlainRulesEveryMode(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for _, mode := range []site.HtaccessMode{site.HtaccessSafe, site.HtaccessOff, site.HtaccessFull} {
		plain, err := ApacheSite(l, "8.3", acmeRoot, mode)
		if err != nil {
			t.Fatal(err)
		}
		ssl, err := ApacheSiteSSL(l, "8.3", acmeRoot, mode)
		if err != nil {
			t.Fatal(err)
		}
		have := codeLines(ssl[bytes.Index(ssl, []byte("<VirtualHost *:443>")):])
		for _, line := range codeLines(plain) {
			if line != "<VirtualHost *:80>" && !slices.Contains(have, line) {
				t.Errorf("%s: vhost 443 mất dòng của bản cổng 80: %q", mode, line)
			}
		}
	}
}

// Chế độ lạ bị từ chối, không trả nội dung: chuỗi này đi vào file cấu hình Apache.
func TestApacheBadHtaccessMode(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for _, mode := range []site.HtaccessMode{
		"All", "None", "an toan", "AN-TOAN", "an-toan ", " tat", "day_du", "full",
		"tat\n", "day-du\n        AllowOverride All", "an-toan; rm -rf /", "$(id)", "../../etc/passwd",
		site.HtaccessMode(strings.Repeat("a", 300)),
	} {
		gens := map[string]func() ([]byte, error){
			"ApacheSite":    func() ([]byte, error) { return ApacheSite(l, "8.3", acmeRoot, mode) },
			"ApacheSiteSSL": func() ([]byte, error) { return ApacheSiteSSL(l, "8.3", acmeRoot, mode) },
			"SiteVhost":     func() ([]byte, error) { return SiteVhost(l, "apache", "8.3", acmeRoot, false, mode) },
			"SiteVhost ssl": func() ([]byte, error) { return SiteVhost(l, "apache", "8.3", acmeRoot, true, mode) },
		}
		for g, gen := range gens {
			if out, err := gen(); !errors.Is(err, ErrParam) || out != nil {
				t.Errorf("%s chế độ %q: muốn ErrParam không nội dung, được %v", g, mode, err)
			}
		}
	}
}

// SiteVhost đưa chế độ tới đúng hàm sinh vhost Apache.
func TestSiteVhostPassesHtaccessMode(t *testing.T) {
	l := siteTestLayout(t, "example.com")
	for _, mode := range []site.HtaccessMode{"", site.HtaccessSafe, site.HtaccessOff, site.HtaccessFull} {
		for _, ssl := range []bool{false, true} {
			gen := ApacheSite
			if ssl {
				gen = ApacheSiteSSL
			}
			want, err := gen(l, "8.3", acmeRoot, mode)
			if err != nil {
				t.Fatal(err)
			}
			got, err := SiteVhost(l, "apache", "8.3", acmeRoot, ssl, mode)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("chế độ %q ssl=%v: SiteVhost không khớp hàm tương ứng (lỗi %v)", mode, ssl, err)
			}
		}
	}
}
