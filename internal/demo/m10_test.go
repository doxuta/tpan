package demo

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

func demoSwitch(sys system.System, st *store.Store) *core.Switch {
	return &core.Switch{Sys: sys, Settings: st, Sites: st, Journal: st}
}

// run chạy một lệnh trên máy demo và trả mã thoát cùng stdout.
func run(t *testing.T, sys system.System, name string, args ...string) (int, string) {
	t.Helper()
	res, err := sys.Run(t.Context(), system.Cmd{Name: name, Args: args})
	var ee *system.ExitError
	if err != nil && !errors.As(err, &ee) {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return res.ExitCode, res.Stdout
}

func wantUnit(t *testing.T, sys system.System, unit string, active, enabled bool) {
	t.Helper()
	a, _ := run(t, sys, "systemctl", "is-active", unit)
	e, _ := run(t, sys, "systemctl", "is-enabled", unit)
	if (a == 0) != active || (e == 0) != enabled {
		t.Errorf("%s: chạy=%v (mã %d), bật khi khởi động=%v (mã %d); muốn %v và %v", unit, a == 0, a, e == 0, e, active, enabled)
	}
}

func wantSetting(t *testing.T, st *store.Store, key, want string) {
	t.Helper()
	got, _, err := st.GetSetting(t.Context(), key)
	if err != nil || got != want {
		t.Errorf("setting %s = %q, %v; muốn %q", key, got, err, want)
	}
}

func fileExists(sys system.System, p string) bool {
	_, err := sys.Stat(p)
	return err == nil
}

// Vòng nginx → apache → nginx qua core.Switch, như nút "Đổi web server" của
// màn Stack: job thành công cả hai chiều, máy giả đổi đúng web server nào chạy,
// và sau mỗi chiều health không có Fail giả.
func TestSwitchRoundTripOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	sw := demoSwitch(sys, st)
	wantUnit(t, sys, "nginx", true, true)
	wantUnit(t, sys, "apache2", false, false)

	// Site wiki hỏng sẵn ở chặng PHP: chuyển web server bị từ chối, như máy thật
	// (core nói trước vì bước kiểm giữa chừng chắc chắn lỗi).
	_, err := sw.Prepare(ctx, core.Apache)
	var refused *core.RefusedError
	if !errors.As(err, &refused) || refused.Code != "precondition_failed" {
		t.Fatalf("Prepare khi wiki hỏng: %v; muốn precondition_failed", err)
	}
	if len(refused.Checks) != 1 || !strings.Contains(refused.Checks[0].ID, siteWiki) {
		t.Errorf("site hỏng nêu trong lời từ chối: %+v", refused.Checks)
	}
	// Chữa wiki: khởi động lại PHP-FPM dựng lại socket của pool.
	if code, _ := run(t, sys, "systemctl", "restart", "php"+phpVer+"-fpm"); code != 0 {
		t.Fatalf("restart php-fpm: mã %d", code)
	}
	if _, err := sw.Prepare(ctx, core.Nginx); err == nil {
		t.Fatal("Prepare sang đúng web server đang chạy phải bị từ chối")
	}

	// Chiều 1: nginx → apache.
	p, err := sw.Prepare(ctx, core.Apache)
	runOK(t, ctx, "nginx → apache")(p, err)

	wantSetting(t, st, core.SettingWeb, string(core.Apache))
	wantUnit(t, sys, "apache2", true, true)
	wantUnit(t, sys, "nginx", false, false)
	if fileExists(sys, policyRcFile) {
		t.Errorf("%s còn sót sau khi cài", policyRcFile)
	}
	if code, _ := run(t, sys, "a2query", "-m", "mpm_event"); code != 0 {
		t.Errorf("mpm_event chưa bật: mã %d", code)
	}
	if code, _ := run(t, sys, "a2query", "-m", "ssl"); code != 0 {
		t.Errorf("ssl chưa bật dù có site SSL: mã %d", code)
	}
	for _, p := range []string{"/etc/nginx/sites-enabled/tpan-default.conf", "/etc/nginx/sites-available/tpan-" + siteShop + ".conf"} {
		if fileExists(sys, p) {
			t.Errorf("file của nginx còn sót: %s", p)
		}
	}
	for _, p := range []string{"/etc/apache2/sites-enabled/000-tpan-default.conf", "/etc/apache2/sites-enabled/000-tpan-default-ssl.conf"} {
		if !fileExists(sys, p) {
			t.Errorf("thiếu file của apache: %s", p)
		}
	}
	if fileExists(sys, "/etc/apache2/sites-enabled/000-default.conf") {
		t.Error("site mặc định của distro chưa được gỡ")
	}
	if code, out := run(t, sys, "curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", "http://127.0.0.1/"); code != 0 || out != "403" {
		t.Errorf("site mặc định Apache: mã curl %d, trả %q; muốn 403", code, out)
	}
	if _, out := run(t, sys, "ss", "-tlnpH"); !strings.Contains(out, `"apache2"`) || strings.Contains(out, `"nginx"`) {
		t.Errorf("ss sau khi chuyển sang Apache:\n%s", out)
	}
	failed, warned := demoHealth(t, ctx, sys, st)
	if len(failed) > 0 || len(warned) > 0 {
		t.Errorf("health sau khi chuyển sang Apache: Fail %v, cảnh báo %v", failed, warned)
	}

	// Chiều 2: apache → nginx. Gói nginx vẫn còn trên máy.
	p, err = sw.Prepare(ctx, core.Nginx)
	runOK(t, ctx, "apache → nginx")(p, err)

	wantSetting(t, st, core.SettingWeb, string(core.Nginx))
	wantUnit(t, sys, "nginx", true, true)
	wantUnit(t, sys, "apache2", false, false)
	if fileExists(sys, "/etc/apache2/sites-enabled/000-tpan-default.conf") {
		t.Error("file của apache còn sót sau khi chuyển về")
	}
	if code, _ := run(t, sys, "curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", "http://127.0.0.1/"); code != 52 {
		t.Errorf("site mặc định nginx: mã curl %d; muốn 52 (đóng kết nối)", code)
	}
	failed, warned = demoHealth(t, ctx, sys, st)
	if len(failed) > 0 || len(warned) > 0 {
		t.Errorf("health sau khi chuyển về nginx: Fail %v, cảnh báo %v", failed, warned)
	}

	// Chạy lại chiều 1 lần nữa: gói apache2 đã có, mọi thứ vẫn thành công.
	p, err = sw.Prepare(ctx, core.Apache)
	runOK(t, ctx, "nginx → apache lần hai")(p, err)
	wantUnit(t, sys, "apache2", true, true)
}

// Web server mới tự chạy trong lúc cài (không có policy-rc.d chặn) thì tranh
// cổng 80 với web server đang chạy: máy giả báo lỗi như apt thật, để một bước
// chặn tự khởi động bị bỏ sót lộ ra ở test chứ không êm.
func TestInstallWithoutPolicyFails(t *testing.T) {
	_, sys, _ := newDemo(t)
	code, _ := run(t, sys, "apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=300", "apache2")
	if code == 0 {
		t.Fatal("cài apache2 khi nginx đang giữ cổng 80 mà không có policy-rc.d phải lỗi")
	}
	wantUnit(t, sys, "nginx", true, true)
	wantUnit(t, sys, "apache2", false, true) // gói đã cài và bật khi khởi động, nhưng không chạy được
	// Có policy-rc.d trả 101 thì cài êm và không chạy.
	sys.SetFile(policyRcFile, []byte("#!/bin/sh\nexit 101\n"), 0o755, rootOwner)
	if code, _ := run(t, sys, "apt-get", "install", "-y", "apache2"); code != 0 {
		t.Errorf("cài lại có policy-rc.d: mã %d", code)
	}
}

// Dừng web server đang chạy, rồi mới khởi động cái kia; khi không ai chạy thì
// request vào máy không nối được (mã 7), cả hai kiểu curl của phép kiểm.
func TestNoWebServerRunning(t *testing.T) {
	_, sys, _ := newDemo(t)
	if code, _ := run(t, sys, "systemctl", "stop", "nginx"); code != 0 {
		t.Fatalf("stop nginx: mã %d", code)
	}
	l, _ := layoutOf(siteBlog)
	for _, args := range [][]string{
		{"-sS", "-o", "/dev/null", "-w", "%{http_code}", "http://127.0.0.1/"},
		{"-sS", "--resolve", siteShop + ":80:127.0.0.1", "-w", "x%{http_code}", "http://" + siteShop + "/"},
		{"-sS", "-k", "--resolve", siteBlog + ":443:127.0.0.1", "https://" + l.Domain.String() + "/"},
	} {
		if code, _ := run(t, sys, "curl", args...); code != 7 {
			t.Errorf("curl %v khi không web server nào chạy: mã %d; muốn 7", args, code)
		}
	}
	// Apache chưa cài thì start lỗi mã 5, như systemd khi không có unit.
	if code, _ := run(t, sys, "systemctl", "start", "apache2"); code != 5 {
		t.Errorf("start apache2 chưa cài: mã %d; muốn 5", code)
	}
}

// apachectl configtest báo lỗi khi vhost dùng SSLEngine mà mod ssl chưa bật
// (thứ tự "bật ssl trước khi ghi file" của steps.DefaultSSL), và a2dismod đảo a2enmod.
func TestApacheConfigtestNeedsSSL(t *testing.T) {
	_, sys, _ := newDemo(t)
	if code, _ := run(t, sys, "apt-get", "install", "-y", "apache2"); code == 0 {
		t.Fatal("cài apache2 không có policy-rc.d lúc nginx chạy phải lỗi")
	}
	sys.SetFile(policyRcFile, []byte("exit 101\n"), 0o755, rootOwner)
	if code, _ := run(t, sys, "apt-get", "install", "-y", "apache2"); code != 0 {
		t.Fatalf("cài apache2: mã %d", code)
	}
	sys.SetFile("/etc/apache2/sites-available/tpan-x.conf", []byte("<VirtualHost *:443>\n    SSLEngine on\n</VirtualHost>\n"), 0o644, rootOwner)
	sys.SetSymlink("/etc/apache2/sites-available/tpan-x.conf", "/etc/apache2/sites-enabled/tpan-x.conf")
	if code, _ := run(t, sys, "apachectl", "configtest"); code != 1 {
		t.Errorf("configtest khi chưa bật ssl: mã %d; muốn 1", code)
	}
	run(t, sys, "a2enmod", "-q", "ssl")
	if code, _ := run(t, sys, "apachectl", "configtest"); code != 0 {
		t.Errorf("configtest sau a2enmod ssl: mã %d; muốn 0", code)
	}
	run(t, sys, "a2dismod", "-q", "ssl")
	if code, _ := run(t, sys, "a2query", "-m", "ssl"); code != 32 {
		t.Errorf("a2query ssl sau a2dismod: mã %d; muốn 32", code)
	}
}

// Bản xem trước của Plan hiện các bước chính theo đúng thứ tự: cài không cho
// chạy, đổi web server, bật web server mới và tắt cái cũ.
func TestSwitchPreviewSteps(t *testing.T) {
	ctx, sys, st := newDemo(t)
	run(t, sys, "systemctl", "restart", "php"+phpVer+"-fpm") // chữa wiki
	p, err := demoSwitch(sys, st).Prepare(ctx, core.Apache)
	if err != nil {
		t.Fatal(err)
	}
	pv, err := p.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range pv.Steps {
		ids = append(ids, s.Info.ID)
	}
	want := []string{"apt.install-nostart:apache2", "web.swap:nginx:apache2", "service:apache2", "service.disable:nginx"}
	last := -1
	for _, w := range want {
		i := slices.Index(ids, w)
		if i <= last {
			t.Errorf("bước %q không có hoặc sai thứ tự (vị trí %d sau %d) trong %v", w, i, last, ids)
		}
		last = i
	}
	if pv.Kind != "stack.switch" || pv.Target != "apache" {
		t.Errorf("Preview: kind %q, target %q", pv.Kind, pv.Target)
	}
	for _, s := range pv.Steps {
		if s.CheckError != "" {
			t.Errorf("bước %s: Check lỗi lúc xem trước: %s", s.Info.ID, s.CheckError)
		}
	}
}
