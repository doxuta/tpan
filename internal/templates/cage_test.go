package templates

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// cageFull là gói có đủ sáu trần (số trong khoảng hợp lệ của mục 3.1).
var cageFull = account.Limits{CPUPercent: 150, RAMMB: 512, Tasks: 64, IOReadMB: 20, IOWriteMB: 10, PHPWorkers: 12}

// Golden của năm file lồng cho a.example.com: trần bằng 0 hết và đủ sáu trần.
// Cập nhật bằng: go test ./internal/templates/ -run TestCage -update (chỉ chạy
// test của lồng, để không ghi đè golden cũ của gói).
func TestCageGolden(t *testing.T) {
	l := siteTestLayout(t, "a.example.com")
	cases := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"cage-slice-0", func() ([]byte, error) { return CageSlice(l, account.Limits{}) }},
		{"cage-slice-full", func() ([]byte, error) { return CageSlice(l, cageFull) }},
		{"cage-socket", func() ([]byte, error) { return CageSocketUnit(l) }},
		{"cage-service", func() ([]byte, error) { return CageServiceUnit(l, "8.3") }},
		{"cage-fence", func() ([]byte, error) { return CageFence(l) }},
		{"cage-fpm-0", func() ([]byte, error) { return CageFPMConf(l, account.Limits{}.Workers()) }},
		{"cage-fpm-full", func() ([]byte, error) { return CageFPMConf(l, cageFull.Workers()) }},
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
				t.Error("cuối file phải có đúng một dấu xuống dòng")
			}
		})
	}
}

// cageLines trả các dòng không phải chú thích (systemd: # và ;).
func cageLines(out []byte) []string {
	var r []string
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" && !strings.HasPrefix(s, "#") && !strings.HasPrefix(s, ";") {
			r = append(r, s)
		}
	}
	return r
}

func cageHas(lines []string, s string) bool {
	for _, l := range lines {
		if l == s {
			return true
		}
	}
	return false
}

func cageAll(t *testing.T, l site.Layout) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for name, gen := range map[string]func() ([]byte, error){
		"slice-0":    func() ([]byte, error) { return CageSlice(l, account.Limits{}) },
		"slice-full": func() ([]byte, error) { return CageSlice(l, cageFull) },
		"socket":     func() ([]byte, error) { return CageSocketUnit(l) },
		"service":    func() ([]byte, error) { return CageServiceUnit(l, "8.3") },
		"fence":      func() ([]byte, error) { return CageFence(l) },
		"fpm":        func() ([]byte, error) { return CageFPMConf(l, 5) },
	} {
		b, err := gen()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = b
	}
	return out
}

// Dòng đầu là Header theo dấu chú thích của loại file; không file nào có
// MemoryHigh hay PrivateUsers (mục 2.3, 2.4).
func TestCageFirstLineAndForbidden(t *testing.T) {
	l := siteTestLayout(t, "a.example.com")
	for name, out := range cageAll(t, l) {
		want := Header
		if name == "fpm" {
			want = sitePoolComment
		}
		if first, _, _ := strings.Cut(string(out), "\n"); first != want {
			t.Errorf("%s: dòng đầu = %q, muốn %q", name, first, want)
		}
		// Xét cả chú thích: phép kiểm khác có thể tìm chuỗi trên cả file.
		for _, bad := range []string{"MemoryHigh", "PrivateUsers"} {
			if bytes.Contains(out, []byte(bad)) {
				t.Errorf("%s: có chuỗi %q", name, bad)
			}
		}
	}
}

// Mỗi trần một dòng khi khác 0, không có dòng khi bằng 0; MemoryMax đi kèm
// MemorySwapMax=0. Riêng hai dòng đĩa luôn có ("infinity" khi bằng 0): systemd
// không đặt lại io.max của thiết bị đã bị bỏ khỏi file, nên gỡ trần đĩa phải
// là đổi giá trị chứ không phải xoá dòng (e2e M17).
func TestCageSliceLimits(t *testing.T) {
	l := siteTestLayout(t, "a.example.com")
	acct := []string{"[Slice]", "MemoryAccounting=yes", "TasksAccounting=yes", "CPUAccounting=yes", "IOAccounting=yes"}
	ioNone := []string{"IOReadBandwidthMax=/var/www infinity", "IOWriteBandwidthMax=/var/www infinity"}
	type one struct {
		lim  account.Limits
		want []string
	}
	cases := map[string]one{
		"cpu":   {account.Limits{CPUPercent: 50}, append([]string{"CPUQuota=50%"}, ioNone...)},
		"ram":   {account.Limits{RAMMB: 200}, append([]string{"MemoryMax=200M", "MemorySwapMax=0"}, ioNone...)},
		"tasks": {account.Limits{Tasks: 30}, append([]string{"TasksMax=30"}, ioNone...)},
		"read":  {account.Limits{IOReadMB: 4}, []string{"IOReadBandwidthMax=/var/www 4M", "IOWriteBandwidthMax=/var/www infinity"}},
		"write": {account.Limits{IOWriteMB: 4}, []string{"IOReadBandwidthMax=/var/www infinity", "IOWriteBandwidthMax=/var/www 4M"}},
		// Worker không phải trần của slice: không sinh dòng nào ở đây.
		"workers": {account.Limits{PHPWorkers: 9}, ioNone},
		"không":   {account.Limits{}, ioNone},
	}
	for name, c := range cases {
		out, err := CageSlice(l, c.lim)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := cageLines(out)
		want := append(append([]string{}, acct...), c.want...)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: dòng = %q, muốn %q", name, got, want)
		}
	}
	out, _ := CageSlice(l, account.Limits{RAMMB: 64, CPUPercent: 5})
	if lines := cageLines(out); !cageHas(lines, "MemorySwapMax=0") || !cageHas(lines, "MemoryMax=64M") {
		t.Errorf("MemoryMax phải đi kèm MemorySwapMax=0: %q", lines)
	}
}

// Những dòng mà bỏ đi là mất cô lập hay hỏng dịch vụ (mục 2.1, 2.8, 3.3).
func TestCageKeyDirectives(t *testing.T) {
	l := siteTestLayout(t, "a.example.com")
	sock := l.CageSocket()
	if !strings.HasPrefix(sock, site.CageRunRoot+"/") || len(strings.TrimSuffix(strings.TrimPrefix(sock, site.CageRunRoot+"/"), "/php.sock")) != 16 {
		t.Fatalf("socket %q không có dạng /run/tpan-cage/<16 hex>/php.sock", sock)
	}
	all := cageAll(t, l)
	want := map[string][]string{
		"socket": {
			"ExecStartPre=-/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld",
			"ListenStream=" + sock,
			"SocketUser=www-data", "SocketGroup=www-data", "SocketMode=0660",
			"DirectoryMode=0755", "RemoveOnStop=yes", "[Install]", "WantedBy=sockets.target",
		},
		"service": {
			"StartLimitIntervalSec=30", "StartLimitBurst=10", "RequiresMountsFor=" + l.Dir,
			"Type=notify", "User=" + l.User, "Group=" + l.User, "Slice=" + l.CageSlice(),
			"NoNewPrivileges=yes", "Environment=FPM_SOCKETS=" + sock + "=3",
			"ExecStart=/usr/sbin/php-fpm8.3 --nodaemonize --fpm-config " + l.CageFPMConf(),
			"ExecReload=/bin/kill -USR2 $MAINPID", "KillSignal=SIGQUIT", "TimeoutStopSec=30",
			"OOMPolicy=continue", "Restart=on-failure", "RestartSec=5s",
		},
		"fence": {
			"[Service]", "User=" + l.User, "Group=" + l.User, "Slice=" + l.CageSlice(),
			"SystemCallFilter=@system-service", "SystemCallFilter=~@privileged @resources",
			"SystemCallErrorNumber=EPERM", "BindPaths=" + l.Dir, "BindPaths=" + l.LogDir,
			"BindReadOnlyPaths=-/run/mysqld:/var/run/mysqld", "BindReadOnlyPaths=-/run/mysqld",
			"BindReadOnlyPaths=-/run/systemd/resolve", "BindReadOnlyPaths=/run/systemd/notify",
			"BindReadOnlyPaths=/run/systemd/journal/dev-log",
			"BindReadOnlyPaths=" + l.CageConfDir(),
			"BindReadOnlyPaths=" + l.CagePasswd() + ":/etc/passwd",
			"BindReadOnlyPaths=" + l.CageGroup() + ":/etc/group",
		},
		"fpm": {
			"[global]", "error_log = syslog", "syslog.ident = php-fpm-" + l.Slug.String(),
			"daemonize = no", "process_control_timeout = 25s", "[" + l.Pool + "]",
			"listen = " + sock, "pm = ondemand", "pm.max_children = 5",
			"pm.process_idle_timeout = 10s", "pm.max_requests = 500",
		},
	}
	for name, ws := range want {
		lines := cageLines(all[name])
		for _, w := range ws {
			if !cageHas(lines, w) {
				t.Errorf("%s thiếu dòng %q", name, w)
			}
		}
	}
	// Dịch vụ không có [Install]: chỉ chạy khi socket đánh thức.
	if cageHas(cageLines(all["service"]), "[Install]") {
		t.Error("service không được có [Install]")
	}
	// php-fpm.conf: không dòng nào bắt đầu bằng # (php-fpm -t lỗi), không có
	// các khoá chỉ có nghĩa khi chạy bằng root.
	for _, line := range strings.Split(string(all["fpm"]), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("fpm có dòng bắt đầu bằng #: %q", line)
		}
		key, _, _ := strings.Cut(line, "=")
		switch strings.TrimSpace(key) {
		case "user", "group", "listen.owner", "listen.group", "listen.mode", "pid":
			t.Errorf("fpm không được có %q", line)
		}
	}
}

// Hàng rào: mọi bản PHP trong CagePHPVersions đúng một mục pool.d; chỉ ba lệnh
// gắn có dấu "-"; mọi giá trị khác giống hàm hang_rao của lib.sh.
func TestCageFencePoolDirs(t *testing.T) {
	l := siteTestLayout(t, "a.example.com")
	out, err := CageFence(l)
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	for _, line := range cageLines(out) {
		if v, ok := strings.CutPrefix(line, "InaccessiblePaths="); ok {
			fields = append(fields, strings.Fields(v)...)
		}
	}
	for _, v := range CagePHPVersions {
		if !phpVersion.MatchString(v) {
			t.Errorf("CagePHPVersions có %q không đúng dạng", v)
		}
		n := 0
		for _, f := range fields {
			if f == "-/etc/php/"+v+"/fpm/pool.d" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("pool.d của %s xuất hiện %d lần, muốn 1", v, n)
		}
	}
	for _, f := range fields {
		if !strings.HasPrefix(f, "-/") {
			t.Errorf("InaccessiblePaths %q thiếu dấu - (thư mục chưa có thì dịch vụ chết)", f)
		}
	}
	dash := 0
	for _, line := range cageLines(out) {
		if strings.HasPrefix(line, "BindReadOnlyPaths=-") || strings.HasPrefix(line, "BindPaths=-") {
			dash++
		}
	}
	if dash != 3 {
		t.Errorf("có %d lệnh gắn có dấu -, muốn đúng 3 (hai /run/mysqld, một resolve)", dash)
	}
}

// Các dòng php_admin_* của lồng giống hệt pool ngoài lồng: vào hay ra lồng thì
// PHP của site thấy cùng giới hạn.
func TestCageFPMMatchesPool(t *testing.T) {
	l := siteTestLayout(t, "a.example.com")
	pool, err := SitePool(l, "8.3")
	if err != nil {
		t.Fatal(err)
	}
	fpm, err := CageFPMConf(l, 5)
	if err != nil {
		t.Fatal(err)
	}
	admin := func(b []byte) string {
		var r []string
		for _, line := range cageLines(b) {
			if strings.HasPrefix(line, "php_admin_") {
				r = append(r, line)
			}
		}
		return strings.Join(r, "\n")
	}
	if a, b := admin(pool), admin(fpm); a == "" || a != b {
		t.Errorf("php_admin_* lệch:\n--- pool ---\n%s\n--- lồng ---\n%s", a, b)
	}
}

// SiteVhostCaged giống SiteVhost từng byte trừ chuỗi socket, cho cả hai web
// server, có và không SSL, mọi chế độ .htaccess, với bản PHP nào cũng được.
func TestSiteVhostCagedMatchesSiteVhost(t *testing.T) {
	for _, dom := range []string{"a.example.com", "cửahàng.vn"} {
		l := siteTestLayout(t, dom)
		for _, web := range []string{"nginx", "apache"} {
			for _, ssl := range []bool{false, true} {
				for _, mode := range []site.HtaccessMode{site.HtaccessSafe, site.HtaccessOff, site.HtaccessFull, ""} {
					got, err := SiteVhostCaged(l, web, acmeRoot, ssl, mode)
					if err != nil {
						t.Fatalf("%s %s ssl=%v %q: %v", dom, web, ssl, mode, err)
					}
					if bytes.Contains(got, []byte("/run/php/")) {
						t.Errorf("%s %s ssl=%v: còn socket của PHP-FPM chung", dom, web, ssl)
					}
					for _, php := range []string{"8.3", "7.4"} {
						base, err := SiteVhost(l, web, php, acmeRoot, ssl, mode)
						if err != nil {
							t.Fatal(err)
						}
						old := l.SocketFor(php)
						if !bytes.Contains(base, []byte(old)) {
							t.Fatalf("vhost gốc không chứa %q: phép so vô nghĩa", old)
						}
						want := strings.ReplaceAll(string(base), old, l.CageSocket())
						if string(got) != want {
							t.Errorf("%s %s ssl=%v %q php %s: khác SiteVhost ngoài chuỗi socket", dom, web, ssl, mode, php)
						}
					}
				}
			}
		}
	}
}

// Tham số lạ: lỗi thoả errors.Is(err, ErrParam) và không trả nội dung.
func TestCageBadParams(t *testing.T) {
	good := siteTestLayout(t, "a.example.com")
	tamper := func(f func(*site.Layout)) site.Layout {
		l := good
		f(&l)
		return l
	}
	other := siteTestLayout(t, "b.example.com")
	layouts := map[string]site.Layout{
		"zero":            {},
		"thiếu domain":    tamper(func(l *site.Layout) { l.Domain = validate.Domain{} }),
		"slug khác":       tamper(func(l *site.Layout) { l.Slug = other.Slug }),
		"dir chèn":        tamper(func(l *site.Layout) { l.Dir = "/var/www/a.example.com\nUser=root" }),
		"dir thoát":       tamper(func(l *site.Layout) { l.Dir = "/var/www/../../etc" }),
		"log dir dài":     tamper(func(l *site.Layout) { l.LogDir = "/" + strings.Repeat("a", 300) }),
		"user root":       tamper(func(l *site.Layout) { l.User = "root" }),
		"user rm":         tamper(func(l *site.Layout) { l.User = "root; rm -rf /" }),
		"pool xuống dòng": tamper(func(l *site.Layout) { l.Pool = "tpan-x]\n[www" }),
		"tmp $(id)":       tamper(func(l *site.Layout) { l.Tmp = "/var/www/a.example.com/$(id)" }),
	}
	check := func(name string, out []byte, err error) {
		t.Helper()
		if !errors.Is(err, ErrParam) {
			t.Errorf("%s: muốn ErrParam, được %v", name, err)
		}
		if out != nil {
			t.Errorf("%s: lỗi mà vẫn trả nội dung", name)
		}
	}
	for name, l := range layouts {
		gens := map[string]func() ([]byte, error){
			"slice":   func() ([]byte, error) { return CageSlice(l, account.Limits{}) },
			"socket":  func() ([]byte, error) { return CageSocketUnit(l) },
			"service": func() ([]byte, error) { return CageServiceUnit(l, "8.3") },
			"fence":   func() ([]byte, error) { return CageFence(l) },
			"fpm":     func() ([]byte, error) { return CageFPMConf(l, 5) },
			"vhost":   func() ([]byte, error) { return SiteVhostCaged(l, "nginx", acmeRoot, false, "") },
		}
		for g, gen := range gens {
			out, err := gen()
			check(name+"/"+g, out, err)
		}
	}
	for name, lim := range map[string]account.Limits{
		"cpu âm":     {CPUPercent: -1},
		"ram âm":     {RAMMB: -64},
		"tasks âm":   {Tasks: -1},
		"read âm":    {IOReadMB: -1},
		"write âm":   {IOWriteMB: -1},
		"workers âm": {PHPWorkers: -5},
	} {
		out, err := CageSlice(good, lim)
		check("slice "+name, out, err)
	}
	for _, w := range []int{-1, 0, 201} {
		out, err := CageFPMConf(good, w)
		check("fpm workers", out, err)
	}
	for _, w := range []int{1, 200} {
		if _, err := CageFPMConf(good, w); err != nil {
			t.Errorf("workers %d phải nhận: %v", w, err)
		}
	}
	for _, php := range []string{"", "8", "8.3; x", "8.3\n", "../8.3", "$(id)"} {
		out, err := CageServiceUnit(good, php)
		check("service php "+php, out, err)
	}
	for _, web := range []string{"", "caddy", "nginx; x"} {
		out, err := SiteVhostCaged(good, web, acmeRoot, false, "")
		check("vhost web "+web, out, err)
	}
	for _, r := range []string{"", "acme", "/var/lib/acme;", "/var/lib/acme\nserver {"} {
		out, err := SiteVhostCaged(good, "apache", r, true, "")
		check("vhost acme "+r, out, err)
	}
	out, err := SiteVhostCaged(good, "apache", acmeRoot, false, "tat-het")
	check("vhost mode lạ", out, err)
}
