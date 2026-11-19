package steps

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

var (
	nginxTest   = &system.Cmd{Name: "nginx", Args: []string{"-t"}}
	nginxReload = &system.Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}}
)

const (
	availDefault = "/etc/nginx/sites-available/default"
	enDefault    = "/etc/nginx/sites-enabled/default"
	tpanConf     = "/etc/nginx/sites-available/tpan-default.conf"
	tpanLink     = "/etc/nginx/sites-enabled/tpan-default.conf"
)

// nginxBase là thư mục Nginx như sau khi apt cài: có site default của Ubuntu
// ở sites-available và symlink của nó ở sites-enabled.
func nginxBase(m *machine) {
	m.SetFile(availDefault, []byte("server { listen 80 default_server; }\n"), 0o644, system.Root)
	m.SetSymlink(availDefault, enDefault)
}

func pharSum() string {
	sum := sha512.Sum512(fakePhar)
	return hex.EncodeToString(sum[:])
}

func TestLifecycle(t *testing.T) {
	conf := []byte("# Do tpan quản lý.\nserver { return 444; }\n")
	cases := []lifecycle{
		{
			name:        "AptUpdate",
			setup:       func(*machine) {},
			step:        func() engine.Step { return &AptUpdate{Packages: []string{"nginx", "php-fpm"}} },
			beforeRerun: func(m *machine) { m.pkgs["nginx"], m.pkgs["php-fpm"] = true, true },
			noUndo:      true,
		},
		{
			name:   "AptInstall",
			setup:  func(*machine) {},
			step:   func() engine.Step { return &AptInstall{Packages: []string{"nginx", "php-fpm"}} },
			noUndo: true,
		},
		{
			name:  "Service",
			setup: func(m *machine) { m.units["nginx"] = unitState{} },
			step:  func() engine.Step { return &Service{Unit: "nginx"} },
		},
		{
			name:  "Service đang chạy nhưng chưa bật",
			setup: func(m *machine) { m.units["nginx"] = unitState{Active: true} },
			step:  func() engine.Step { return &Service{Unit: "nginx"} },
		},
		{
			name:  "Dir mới cùng thư mục cha còn thiếu",
			setup: func(*machine) {},
			step: func() engine.Step {
				return &Dir{Path: "/var/lib/tpan/acme", Perm: 0o755, Owner: system.Root}
			},
		},
		{
			name:  "Dir có sẵn sai quyền và chủ",
			setup: func(m *machine) { m.SetDir("/var/cache/tpan", 0o777, system.Owner{User: "nobody", Group: "nogroup"}) },
			step:  func() engine.Step { return &Dir{Path: "/var/cache/tpan", Perm: 0o755} },
		},
		{
			// Thư mục site (mục 5): 0750 có sẵn phải được nâng lên 2750, và
			// Undo phải trả lại đúng 0750 chứ không phải 2750.
			name:  "Dir setgid có sẵn thiếu bit setgid",
			setup: func(m *machine) { m.SetDir("/var/www/example.com", 0o750, system.Root) },
			step: func() engine.Step {
				return &Dir{Path: "/var/www/example.com", Perm: fs.ModeSetgid | 0o750, Owner: system.Owner{User: "tp_example_com_abc123", Group: "www-data"}}
			},
		},
		{
			name:  "File mới",
			setup: func(m *machine) { m.SetDir("/etc/nginx/sites-available", 0o755, system.Root) },
			step: func() engine.Step {
				return &File{Path: tpanConf, Content: conf, Perm: 0o644, Test: nginxTest, Reload: nginxReload}
			},
		},
		{
			name: "File ghi đè file cũ",
			setup: func(m *machine) {
				m.SetFile(tpanConf, []byte("cũ\n"), 0o600, system.Owner{User: "www-data", Group: "www-data"})
			},
			step: func() engine.Step {
				return &File{Path: tpanConf, Content: conf, Perm: 0o644, Owner: system.Root, Test: nginxTest, Reload: nginxReload}
			},
		},
		{
			name: "Symlink mới",
			setup: func(m *machine) {
				m.SetFile(tpanConf, conf, 0o644, system.Root)
				m.SetDir("/etc/nginx/sites-enabled", 0o755, system.Root)
			},
			step: func() engine.Step {
				return &Symlink{Target: tpanConf, Link: tpanLink, Test: nginxTest, Reload: nginxReload}
			},
		},
		{
			name: "Symlink trỏ sai chỗ",
			setup: func(m *machine) {
				m.SetFile(tpanConf, conf, 0o644, system.Root)
				m.SetSymlink("/tmp/khac.conf", tpanLink)
			},
			step: func() engine.Step {
				return &Symlink{Target: tpanConf, Link: tpanLink, Test: nginxTest, Reload: nginxReload}
			},
		},
		{
			name:  "Absent gỡ symlink",
			setup: nginxBase,
			step:  func() engine.Step { return &Absent{Path: enDefault, Test: nginxTest, Reload: nginxReload} },
		},
		{
			name:  "Absent gỡ file thường",
			setup: func(m *machine) { m.SetFile("/etc/apache2/sites-enabled/000-default.conf", conf, 0o640, system.Root) },
			step:  func() engine.Step { return &Absent{Path: "/etc/apache2/sites-enabled/000-default.conf"} },
		},
		{
			name:  "ApacheModules",
			setup: func(m *machine) { m.mods["mpm_prefork"] = true; m.mods["setenvif"] = true },
			step: func() engine.Step {
				return &ApacheModules{Enable: []string{"mpm_event", "proxy_fcgi", "setenvif"}, Disable: []string{"mpm_prefork"}}
			},
		},
		{
			name:   "MariaDBSecure",
			setup:  func(m *machine) { m.db = dbState{RootSocket: false, Anon: 2, Test: true} },
			step:   func() engine.Step { return &MariaDBSecure{} },
			noUndo: true,
		},
		{
			name: "WPCLI",
			setup: func(m *machine) {
				m.SetDir(wpCacheDir, 0o755, system.Root)
				m.SetDir("/usr/local/bin", 0o755, system.Root)
			},
			step: func() engine.Step {
				return &WPCLI{Version: "2.12.0", SHA512: strings.ToUpper(pharSum())}
			},
		},
		{
			name: "WPCLI thay bản cũ",
			setup: func(m *machine) {
				m.SetDir(wpCacheDir, 0o755, system.Root)
				m.SetFile(wpBin, []byte("bản cũ"), 0o755, system.Root)
			},
			step: func() engine.Step { return &WPCLI{Version: "2.12.0", SHA512: pharSum()} },
		},
	}
	for _, lc := range cases {
		t.Run(lc.name, lc.run)
	}
}

// DetectPHP không theo khuôn lifecycle: trên máy sạch Apply phải lỗi, và Undo
// không có gì để làm.
func TestDetectPHP(t *testing.T) {
	ctx := context.Background()
	m := newMachine(t)
	var v string
	s := &DetectPHP{Result: &v}

	if done, err := s.Check(ctx, m); err != nil || done {
		t.Fatalf("máy sạch: Check = %v, %v; muốn false, nil", done, err)
	}
	if err := s.Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "không tìm thấy PHP-FPM") {
		t.Fatalf("máy sạch: Apply = %v; muốn lỗi không tìm thấy PHP-FPM", err)
	}

	// Có cli mà không có fpm thì chưa tính; nhiều bản thì lấy bản cao nhất theo số.
	m.SetDir("/etc/php/9.0/cli", 0o755, system.Root)
	m.SetDir("/etc/php/8.9/fpm", 0o755, system.Root)
	m.SetDir("/etc/php/8.10/fpm", 0o755, system.Root)
	m.SetFile("/etc/php/7.4", nil, 0o644, system.Root) // file lạ, bỏ qua
	m.SetDir("/etc/php/mods-available", 0o755, system.Root)
	if err := s.Apply(ctx, m); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if v != "8.10" {
		t.Fatalf("Result = %q, muốn 8.10", v)
	}

	// Chạy lại: bước được bỏ qua mà Result vẫn có giá trị cho các bước sau.
	var v2 string
	m.ResetRan()
	rep := execute(t, m, &DetectPHP{Result: &v2})
	if rep.Steps[0].Outcome != "skipped" || v2 != "8.10" || len(m.Ran()) != 0 {
		t.Fatalf("chạy lại: outcome %q, Result %q, lệnh %v", rep.Steps[0].Outcome, v2, ranStrings(m.Ran()))
	}
	if s.Info().NoUndo.IsZero() {
		t.Error("Undo không làm gì nhưng NoUndo trống")
	}

	// /etc/php là file: lỗi có ngữ cảnh, không giả vờ là chưa cài.
	bad := newMachine(t)
	bad.SetFile("/etc/php", nil, 0o644, system.Root)
	if _, err := s.Check(ctx, bad); err == nil || !strings.Contains(err.Error(), "đọc /etc/php") {
		t.Errorf("Check khi /etc/php là file = %v; muốn lỗi có ngữ cảnh", err)
	}
	if _, err := (&DetectPHP{}).Check(ctx, m); err == nil {
		t.Error("Result nil mà Check không lỗi")
	}
}

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"8.9", "8.10", true}, {"8.10", "8.9", false}, {"7.4", "8.0", true}, {"8.3", "8.3", false}} {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// ---- Lệnh lỗi thì trả lỗi có ngữ cảnh, và máy về như cũ ----

// wantExit kiểm lỗi có ngữ cảnh và vẫn giữ *ExitError bên trong để ai cần thì
// lấy được mã thoát.
func wantExit(t *testing.T, err error, context string) {
	t.Helper()
	if err == nil {
		t.Fatal("muốn lỗi, được nil")
	}
	if !strings.Contains(err.Error(), context) {
		t.Errorf("lỗi %q thiếu ngữ cảnh %q", err, context)
	}
	var ee *system.ExitError
	if !errors.As(err, &ee) {
		t.Errorf("lỗi %q không bọc *system.ExitError", err)
	}
}

func TestCommandErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("AptUpdate", func(t *testing.T) {
		m := newMachine(t)
		m.failCmd([]string{"apt-get", "update"}, 100, "E: Could not get lock")
		wantExit(t, (&AptUpdate{Packages: []string{"nginx"}}).Apply(ctx, m), "làm mới danh sách gói")
	})
	t.Run("AptInstall", func(t *testing.T) {
		m := newMachine(t)
		m.failCmd([]string{"apt-get", "install", "-y"}, 100, "E: Unable to locate package nginx")
		wantExit(t, (&AptInstall{Packages: []string{"nginx"}}).Apply(ctx, m), "cài gói nginx")
	})
	t.Run("dpkg-query mã 2 là lỗi thật", func(t *testing.T) {
		m := newMachine(t)
		m.failCmd([]string{"dpkg-query"}, 2, "dpkg-query: error: parsing file")
		_, err := (&AptInstall{Packages: []string{"nginx"}}).Check(ctx, m)
		wantExit(t, err, "hỏi dpkg")
	})
	t.Run("Service: enable lỗi thì trả về như cũ", func(t *testing.T) {
		m := newMachine(t)
		m.units["nginx"] = unitState{}
		before := m.whole()
		m.failCmd([]string{"systemctl", "enable"}, 1, "Job for nginx.service failed")
		wantExit(t, (&Service{Unit: "nginx"}).Apply(ctx, m), "bật dịch vụ nginx")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy không về như cũ")
		}
	})
	t.Run("Dir: tạo lỗi thì xoá thư mục cha đã tạo", func(t *testing.T) {
		m := newMachine(t)
		before := m.whole()
		m.Fail("mkdir", "/var/lib/tpan/acme", syscall.EACCES)
		err := (&Dir{Path: "/var/lib/tpan/acme", Perm: 0o755}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "tạo thư mục /var/lib/tpan/acme") {
			t.Fatalf("Apply = %v; muốn lỗi có ngữ cảnh", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("thư mục cha đã tạo không được dọn")
		}
	})
	t.Run("Dir: Undo không xoá thư mục đã có thứ khác ghi vào", func(t *testing.T) {
		m := newMachine(t)
		s := &Dir{Path: "/srv/x", Perm: 0o755}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.SetFile("/srv/x/data", []byte("dữ liệu"), 0o644, system.Root)
		if err := s.Undo(ctx, m); err == nil || !strings.Contains(err.Error(), "chỉ xoá khi rỗng") {
			t.Fatalf("Undo = %v; muốn lỗi thư mục không rỗng", err)
		}
		if _, err := m.ReadFile("/srv/x/data"); err != nil {
			t.Errorf("dữ liệu bị xoá: %v", err)
		}
	})
	t.Run("File: Test lỗi thì file cũ được trả về", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(tpanConf, []byte("cũ\n"), 0o640, system.Root)
		before := m.whole()
		m.Handle([]string{"nginx", "-t"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stdout: "dòng stdout", Stderr: "nginx: [emerg] unknown directive \"sai\" in tpan-default.conf:2"}
		})
		err := (&File{Path: tpanConf, Content: []byte("sai\n"), Perm: 0o644, Test: nginxTest, Reload: nginxReload}).Apply(ctx, m)
		wantExit(t, err, "kiểm cấu hình sau khi ghi "+tpanConf)
		if !strings.Contains(err.Error(), "unknown directive") || !strings.Contains(err.Error(), "dòng stdout") {
			t.Errorf("lỗi thiếu output của nginx -t: %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("file cũ không được trả về")
		}
		for _, c := range m.Ran() {
			if c.String() == nginxReload.String() {
				t.Error("Test lỗi mà vẫn reload")
			}
		}
	})
	t.Run("File: Test lỗi thì file mới bị xoá", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir("/etc/nginx/sites-available", 0o755, system.Root)
		before := m.whole()
		m.failCmd([]string{"nginx", "-t"}, 1, "emerg")
		wantExit(t, (&File{Path: tpanConf, Content: []byte("x"), Perm: 0o644, Test: nginxTest}).Apply(ctx, m), "kiểm cấu hình")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("file mới không bị xoá")
		}
	})
	t.Run("File: ghi lỗi", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir("/etc/nginx/sites-available", 0o755, system.Root)
		m.Fail("write", tpanConf, syscall.ENOSPC)
		err := (&File{Path: tpanConf, Perm: 0o644}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "ghi "+tpanConf) || !errors.Is(err, syscall.ENOSPC) {
			t.Errorf("Apply = %v; muốn lỗi ghi có ngữ cảnh", err)
		}
	})
	t.Run("Symlink: Reload lỗi thì gỡ link và reload lại", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(tpanConf, nil, 0o644, system.Root)
		m.SetDir("/etc/nginx/sites-enabled", 0o755, system.Root)
		before := m.whole()
		calls := 0
		m.Handle([]string{"systemctl", "reload"}, func(system.Cmd) system.Result {
			calls++
			if calls == 1 {
				return system.Result{ExitCode: 1, Stderr: "Job for nginx.service failed"}
			}
			return system.Result{}
		})
		wantExit(t, (&Symlink{Target: tpanConf, Link: tpanLink, Test: nginxTest, Reload: nginxReload}).Apply(ctx, m), "nạp lại dịch vụ sau khi bật "+tpanLink)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("link không được gỡ")
		}
		if calls != 2 {
			t.Errorf("reload %d lần, muốn 2 (lần lỗi và lần nạp lại config cũ)", calls)
		}
	})
	t.Run("Symlink: không đè file thật", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(tpanLink, []byte("ai đó chép vào"), 0o644, system.Root)
		s := &Symlink{Target: tpanConf, Link: tpanLink}
		if _, err := s.Check(ctx, m); err == nil {
			t.Error("Check không báo lỗi")
		}
		if err := s.Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "không phải symlink") {
			t.Errorf("Apply = %v", err)
		}
	})
	t.Run("Absent: Test lỗi thì trả symlink về", func(t *testing.T) {
		m := newMachine(t)
		nginxBase(m)
		before := m.whole()
		m.failCmd([]string{"nginx", "-t"}, 1, "emerg")
		wantExit(t, (&Absent{Path: enDefault, Test: nginxTest, Reload: nginxReload}).Apply(ctx, m), "gỡ "+enDefault)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("symlink không được trả về")
		}
	})
	t.Run("Absent: không gỡ thư mục", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir("/etc/nginx/sites-enabled", 0o755, system.Root)
		if _, err := (&Absent{Path: "/etc/nginx/sites-enabled"}).Check(ctx, m); err == nil {
			t.Error("Check không báo lỗi khi là thư mục")
		}
		if err := (&Absent{Path: "/etc/nginx/sites-enabled"}).Apply(ctx, m); err == nil {
			t.Error("Apply không báo lỗi khi là thư mục")
		}
	})
	t.Run("ApacheModules: configtest lỗi thì trả module về, không restart", func(t *testing.T) {
		m := newMachine(t)
		m.mods["mpm_prefork"] = true
		before := m.whole()
		m.failCmd([]string{"apachectl", "configtest"}, 1, "AH00526: Syntax error")
		err := (&ApacheModules{Enable: []string{"mpm_event"}, Disable: []string{"mpm_prefork"}}).Apply(ctx, m)
		wantExit(t, err, "kiểm cấu hình Apache")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("module không về như cũ")
		}
		got := strings.Join(ranStrings(m.Ran()), "\n")
		if strings.Contains(got, "restart") {
			t.Errorf("configtest lỗi mà vẫn restart:\n%s", got)
		}
		// Tắt mpm_event trước rồi mới bật lại mpm_prefork (hai MPM xung đột).
		if i, j := strings.Index(got, "a2dismod -q mpm_event"), strings.Index(got, "a2enmod -q mpm_prefork"); i < 0 || j < i {
			t.Errorf("thứ tự trả module sai:\n%s", got)
		}
	})
	t.Run("ApacheModules: restart lỗi", func(t *testing.T) {
		m := newMachine(t)
		m.mods["mpm_prefork"] = true
		before := m.whole()
		calls := 0
		m.Handle([]string{"systemctl", "restart"}, func(system.Cmd) system.Result {
			if calls++; calls == 1 {
				return system.Result{ExitCode: 1, Stderr: "Job for apache2.service failed"}
			}
			return system.Result{}
		})
		wantExit(t, (&ApacheModules{Enable: []string{"mpm_event"}, Disable: []string{"mpm_prefork"}}).Apply(ctx, m), "khởi động lại apache2")
		if !reflect.DeepEqual(before, m.whole()) || calls != 2 {
			t.Errorf("module không về như cũ hoặc không restart lại (restart %d lần)", calls)
		}
	})
	t.Run("ApacheModules: Undo chỉ đảo module đã đổi", func(t *testing.T) {
		m := newMachine(t)
		m.mods["mpm_prefork"], m.mods["rewrite"] = true, true
		s := &ApacheModules{Enable: []string{"mpm_event", "rewrite"}, Disable: []string{"mpm_prefork"}}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.ResetRan()
		if err := s.Undo(ctx, m); err != nil {
			t.Fatal(err)
		}
		got := ranStrings(m.Ran())
		want := []string{"a2dismod -q mpm_event", "a2enmod -q mpm_prefork", "apachectl configtest", "systemctl restart apache2"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Undo chạy %v, muốn %v", got, want)
		}
	})
	t.Run("MariaDBSecure", func(t *testing.T) {
		m := newMachine(t)
		m.failCmd([]string{"mariadb", "-N", "-B"}, 1, "ERROR 2002 (HY000): Can't connect to local server through socket")
		_, err := (&MariaDBSecure{}).Check(ctx, m)
		wantExit(t, err, "đọc trạng thái bảo mật của MariaDB")
	})
	t.Run("MariaDBSecure: SQL đi qua stdin, chỉ câu cần thiết", func(t *testing.T) {
		m := newMachine(t)
		m.db = dbState{RootSocket: true, Anon: 1}
		if err := (&MariaDBSecure{}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		ran := m.Ran()
		last := ran[len(ran)-1]
		if len(last.Args) != 2 || string(last.Stdin) != sqlDropAnon+sqlFlush {
			t.Errorf("lệnh sửa: %s, stdin %q", last.String(), last.Stdin)
		}
	})
	t.Run("MariaDBSecure: kết quả lạ", func(t *testing.T) {
		m := newMachine(t)
		m.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result { return system.Result{Stdout: "1\t0\n"} })
		if _, err := (&MariaDBSecure{}).Check(ctx, m); err == nil || !strings.Contains(err.Error(), "kết quả lạ") {
			t.Errorf("Check = %v", err)
		}
	})
	t.Run("WPCLI: tải lỗi", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir(wpCacheDir, 0o755, system.Root)
		before := m.whole()
		m.failCmd([]string{"curl"}, 22, "curl: (22) The requested URL returned error: 404")
		wantExit(t, (&WPCLI{Version: "9.9.9", SHA512: pharSum()}).Apply(ctx, m), "tải WP-CLI 9.9.9")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy không về như cũ")
		}
	})
	t.Run("WPCLI: sha512 không khớp thì xoá file tải về, không cài", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir(wpCacheDir, 0o755, system.Root)
		before := m.whole()
		err := (&WPCLI{Version: DefaultWPCLIVersion, SHA512: DefaultWPCLISHA512}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "sha512 không khớp") {
			t.Fatalf("Apply = %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("file tải về chưa bị xoá hoặc wp đã bị ghi")
		}
	})
	t.Run("WPCLI: dùng lại file đã tải", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(wpCacheDir+"/wp-cli-2.12.0.phar", m.phar, 0o644, system.Root)
		m.SetDir("/usr/local/bin", 0o755, system.Root)
		if err := (&WPCLI{Version: "2.12.0", SHA512: pharSum()}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if len(m.Ran()) != 0 {
			t.Errorf("vẫn tải lại: %v", ranStrings(m.Ran()))
		}
		if fi, err := m.Stat(wpBin); err != nil || fi.Mode.Perm() != 0o755 {
			t.Errorf("wp: %v %v", fi, err)
		}
	})
}

// Undo khi chưa Apply thì lỗi, không "hoàn tác" bằng trạng thái rỗng (ví dụ
// xoá file mà bước chưa từng ghi).
func TestUndoWithoutApply(t *testing.T) {
	m := newMachine(t)
	m.SetFile(tpanConf, []byte("x"), 0o644, system.Root)
	for _, s := range []engine.Step{
		&Service{Unit: "nginx"}, &Dir{Path: "/x", Perm: 0o755}, &File{Path: tpanConf, Perm: 0o644},
		&Symlink{Target: "/a", Link: "/b"}, &Absent{Path: "/x"},
		&ApacheModules{Enable: []string{"rewrite"}}, &WPCLI{Version: "1.0.0", SHA512: strings.Repeat("a", 128)},
	} {
		if err := s.Undo(context.Background(), m); !errors.Is(err, errNotApplied) {
			t.Errorf("%s: Undo = %v", s.Info().ID, err)
		}
	}
	if _, err := m.ReadFile(tpanConf); err != nil {
		t.Errorf("file bị đụng: %v", err)
	}
}

// ---- Tham số sai: Check và Apply đều lỗi, không chạy lệnh nào ----

func TestBadParams(t *testing.T) {
	attacks := []string{
		"", "a.com; rm -rf /", "$(id)", "`id`", "../../etc/passwd", "nginx\nrm", "-o", "--allow-unauthenticated",
		strings.Repeat("a", 300) + ";", "tên-miền", "NGINX", "a b",
	}
	paths := []string{"", "relative/x", "/etc/nginx/../../root/.ssh", "/etc/nginx/", "//etc/x", "/etc/x\nrm", "/", "/a/./b"}

	var steps []engine.Step
	for _, a := range attacks {
		steps = append(steps,
			&AptUpdate{Packages: []string{"nginx", a}},
			&AptInstall{Packages: []string{a}},
			&ApacheModules{Enable: []string{a}},
			&ApacheModules{Disable: []string{a}},
		)
		if a != "NGINX" { // tên unit systemd được có chữ hoa
			steps = append(steps, &Service{Unit: a})
		}
	}
	for _, p := range paths {
		steps = append(steps,
			&Dir{Path: p, Perm: 0o755},
			&File{Path: p, Perm: 0o644},
			&Symlink{Target: p, Link: "/etc/x"},
			&Symlink{Target: "/etc/x", Link: p},
			&Absent{Path: p},
		)
	}
	steps = append(steps,
		&AptUpdate{}, &AptInstall{}, &ApacheModules{},
		// Gói kéo theo mod_php (mục 3).
		&AptInstall{Packages: []string{"nginx", "php"}},
		&AptInstall{Packages: []string{"libapache2-mod-php"}},
		&AptInstall{Packages: []string{"php8.3"}},
		&AptInstall{Packages: []string{"libapache2-mod-php8.3"}},
		&ApacheModules{Enable: []string{"rewrite"}, Disable: []string{"rewrite"}},
		&Dir{Path: "/x", Perm: fs.ModeSetuid | 0o750},
		&File{Path: "/x", Perm: fs.ModeSetuid | 0o755},
		&WPCLI{Version: "2.12", SHA512: strings.Repeat("a", 128)},
		&WPCLI{Version: "2.12.0; id", SHA512: strings.Repeat("a", 128)},
		&WPCLI{Version: "2.12.0", SHA512: "abc"},
		&WPCLI{Version: "2.12.0", SHA512: strings.Repeat("g", 128)},
	)

	ctx := context.Background()
	for _, s := range steps {
		m := newMachine(t)
		before := m.whole()
		if _, err := s.Check(ctx, m); err == nil {
			t.Errorf("%#v: Check không lỗi", s)
		}
		if err := s.Apply(ctx, m); err == nil {
			t.Errorf("%#v: Apply không lỗi", s)
		}
		if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("%#v: tham số sai mà vẫn chạy lệnh %v hoặc đổi máy", s, ranStrings(m.Ran()))
		}
	}

	// Gói hợp lệ có tên gần giống thì vẫn nhận.
	for _, ok := range []string{"php-fpm", "php8.3-fpm", "php-mysql", "libapache2-mod-fcgid", "g++", "libc6"} {
		if err := (&AptInstall{Packages: []string{ok}}).validate(); err != nil {
			t.Errorf("%s bị từ chối: %v", ok, err)
		}
	}
}

// ---- Info ----

func TestInfo(t *testing.T) {
	var v string
	all := []engine.Step{
		&AptUpdate{Packages: []string{"nginx"}}, &AptInstall{Packages: []string{"nginx", "curl"}},
		&Service{Unit: "nginx"}, &Dir{Path: "/var/lib/tpan", Perm: 0o755},
		&File{Path: tpanConf, Content: []byte("x"), Perm: 0o644, Test: nginxTest, Reload: nginxReload},
		&Symlink{Target: tpanConf, Link: tpanLink}, &Absent{Path: enDefault},
		&ApacheModules{Enable: []string{"mpm_event"}, Disable: []string{"mpm_prefork"}},
		&MariaDBSecure{}, &DetectPHP{Result: &v},
		&WPCLI{Version: DefaultWPCLIVersion, SHA512: DefaultWPCLISHA512},
	}
	seen := map[string]bool{}
	for _, s := range all {
		info := s.Info()
		if !strings.Contains(info.ID, ":") || seen[info.ID] {
			t.Errorf("ID %q không đúng dạng <loại>:<đối tượng> hoặc bị trùng", info.ID)
		}
		seen[info.ID] = true
		for _, txt := range []string{info.Title.Vi, info.Title.En, info.Why.Vi, info.Why.En} {
			if txt == "" {
				t.Errorf("%s: thiếu Title hoặc Why ở một ngôn ngữ", info.ID)
			}
		}
	}

	f := all[4].Info()
	if f.ID != "file:"+tpanConf || len(f.Files) != 1 || string(f.Files[0].New) != "x" ||
		!reflect.DeepEqual(f.Commands, []string{"nginx -t", "systemctl reload nginx"}) {
		t.Errorf("Info của File: %+v", f)
	}
	if a := all[6].Info(); len(a.Files) != 1 || !a.Files[0].Delete {
		t.Errorf("Info của Absent phải có FileChange Delete: %+v", a.Files)
	}
	if !all[7].Info().Danger || !all[8].Info().Danger {
		t.Error("ApacheModules (restart) và MariaDBSecure (DROP DATABASE) phải Danger")
	}
	if got := (&File{Path: "/x", Danger: true}).Info().Danger; !got {
		t.Error("File.Danger không đi vào Info")
	}
	if got := all[1].Info().Commands; !reflect.DeepEqual(got, []string{"apt-get install -y nginx curl"}) {
		t.Errorf("Commands của AptInstall: %v", got)
	}
}

func TestDefaultWPCLIConstants(t *testing.T) {
	s := &WPCLI{Version: DefaultWPCLIVersion, SHA512: DefaultWPCLISHA512}
	if err := s.validate(); err != nil {
		t.Fatal(err)
	}
	want := "curl -fsSL -o /var/cache/tpan/wp-cli-" + DefaultWPCLIVersion + ".phar https://github.com/wp-cli/wp-cli/releases/download/v" +
		DefaultWPCLIVersion + "/wp-cli-" + DefaultWPCLIVersion + ".phar"
	if got := s.download().String(); got != want {
		t.Errorf("lệnh tải:\n%s\nmuốn\n%s", got, want)
	}
}
