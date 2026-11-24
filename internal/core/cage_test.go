package core

// Test của các Plan của lồng (M17 đợt 3a, mục 3.6 của docs/thay-doi/M17-thiet-ke.md):
// vào, ra, khởi động lại, áp trần (sửa gói, đổi gói của user), trạng thái.
// Chạy trên siteSim của site_test.go (site đã tạo thật qua PrepareAdd) cộng một
// systemd nhỏ của lồng (cgEnv). Không bọc cage.Wrap: lệnh dưới user của site
// đi thẳng tới handler của siteSim (wp, test), đủ cho kiểm chứng; lớp bọc có
// test riêng ở internal/cage.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	cgDom   = "shop.example.com"
	cgOther = "blog.example.com"
	cgPkg   = "longa"
)

// cgEnv là fixEnv cộng: bảng user và gói (admin dùng gói cgPkg bật lồng, trần
// RAM 256), systemd của lồng (socket tạo file khi bật, mất khi tắt), máy có
// cgroup v2 và systemd 255, PHP 8.3 đã cài.
type cgEnv struct {
	*fixEnv
	acc    *usrStore
	l      site.Layout
	sysVer string
}

func newCgEnv(t *testing.T, web WebServer, doms ...string) *cgEnv {
	t.Helper()
	fe := newFixEnv(t, web, doms...)
	acc := newUsrStore(fe.sites, newUsrDBs())
	acc.pkgs[cgPkg] = account.Package{Name: cgPkg, Cage: true, RAMMB: 256}
	acc.put(account.User{Name: account.AdminName, Role: account.RoleAdmin, Package: cgPkg})
	e := &cgEnv{fixEnv: fe, acc: acc, l: site.LayoutOf(mustDomain(t, doms[0])), sysVer: "systemd 255 (255.4-1ubuntu8)\n+PAM +AUDIT\n"}
	e.f.Accounts = acc
	e.s.Accounts = acc // Sites.Checks đọc trần của site trong lồng (fixEnv.recheck)
	m := fe.m

	// Như sau tpan setup và stack install.
	m.SetDir("/etc/tpan", 0o700, system.Root)
	m.SetDir(site.SystemdDir, 0o755, system.Root)
	m.SetDir(site.CageRunRoot, 0o755, system.Root)
	m.SetDir("/proc", 0o555, system.Root)
	m.SetFile(cgroupControllers, []byte("cpuset cpu io memory pids\n"), 0o444, system.Root)
	m.SetFile("/usr/sbin/php-fpm8.3", []byte("ELF"), 0o755, system.Root)

	// socket của lồng: unit → đường dẫn file socket, của mọi site.
	socks := map[string]string{}
	for _, d := range doms {
		l := site.LayoutOf(mustDomain(t, d))
		socks[l.CageSocketUnit()] = l.CageSocket()
	}
	start := func(unit string) {
		u := e.units[unit]
		u.active = true
		e.units[unit] = u
		if p, ok := socks[unit]; ok {
			m.SetSocket(p, 0o660, system.Owner{User: "www-data", Group: "www-data"})
		}
	}
	stop := func(unit string) {
		u := e.units[unit]
		u.active = false
		e.units[unit] = u
		if p, ok := socks[unit]; ok {
			_ = m.Remove(p) // RemoveOnStop
		}
	}
	m.Handle([]string{"systemctl"}, func(c system.Cmd) system.Result {
		e.mu.Lock()
		defer e.mu.Unlock()
		a := c.Args
		unit := a[len(a)-1]
		now := slices.Contains(a, "--now")
		switch a[0] {
		case "--version":
			return system.Result{Stdout: e.sysVer}
		case "is-active":
			if e.units[unit].active {
				return system.Result{Stdout: "active\n"}
			}
			return system.Result{ExitCode: 3, Stdout: "inactive\n"}
		case "is-enabled":
			if e.units[unit].enabled {
				return system.Result{Stdout: "enabled\n"}
			}
			return system.Result{ExitCode: 1, Stdout: "disabled\n"}
		case "enable":
			u := e.units[unit]
			u.enabled = true
			e.units[unit] = u
			if now {
				start(unit)
			}
		case "disable":
			u := e.units[unit]
			u.enabled = false
			e.units[unit] = u
			if now {
				stop(unit)
			}
		case "start":
			start(unit)
		case "stop":
			stop(unit)
		case "try-restart":
			if e.units[unit].active {
				start(unit)
			}
		case "show":
			if slices.Contains(a, "--value") {
				return system.Result{Stdout: "0\n"}
			}
			return system.Result{Stdout: "MainPID=0\nActiveEnterTimestamp=\n"}
		}
		return system.Result{}
	})
	// cage.fence chạy `getent passwd` không tên trong lồng (passwd riêng 4 dòng);
	// handler của siteSim đòi một tên. Có tên thì trả như siteSim.
	m.Handle([]string{"getent", "passwd"}, func(c system.Cmd) system.Result {
		if len(c.Args) < 2 {
			return system.Result{Stdout: "root:x:0:0::/root:/bin/bash\nwww-data:x:33:33::/var/www:/usr/sbin/nologin\n" +
				c.AsUser + ":x:998:998::/var/www/x:/usr/sbin/nologin\nnobody:x:65534:65534::/nonexistent:/usr/sbin/nologin\n"}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		home, ok := m.users[c.Args[1]]
		if !ok {
			return system.Result{ExitCode: 2}
		}
		return system.Result{Stdout: c.Args[1] + ":x:998:998::" + home + ":/usr/sbin/nologin\n"}
	})
	// CageFiles đọc đúng bốn dòng getent (siteSim chỉ trả dòng của tên đầu).
	m.Handle([]string{"getent", "passwd", "root"}, func(c system.Cmd) system.Result {
		u := c.Args[3]
		return system.Result{Stdout: "root:x:0:0:root:/root:/bin/bash\nwww-data:x:33:33:www-data:/var/www:/usr/sbin/nologin\n" +
			u + ":x:998:998::/var/www/x:/usr/sbin/nologin\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n"}
	})
	m.Handle([]string{"getent", "group", "root"}, func(c system.Cmd) system.Result {
		return system.Result{Stdout: "root:x:0:\nwww-data:x:33:\n" + c.Args[3] + ":x:998:\nnogroup:x:65534:\n"}
	})
	m.Handle([]string{"id", "-u"}, func(system.Cmd) system.Result { return system.Result{Stdout: "998\n"} })
	return e
}

func (e *cgEnv) cages() *Cages {
	return &Cages{Sys: e.m, Settings: e.st, Store: e.sites, Accounts: e.acc, Journal: e.st}
}

func (e *cgEnv) site(t *testing.T, dom string) site.Site {
	t.Helper()
	st, ok, err := e.sites.Site(ctx, mustDomain(t, dom))
	if err != nil || !ok {
		t.Fatalf("site %s: %v %v", dom, ok, err)
	}
	return st
}

func (e *cgEnv) setSite(t *testing.T, dom string, mod func(*site.Site)) {
	t.Helper()
	st := e.site(t, dom)
	mod(&st)
	if err := e.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
}

// run chạy Planned và trả Report (lỗi Execute là lỗi test).
func cgRun(t *testing.T, pl *Planned) engine.Report {
	t.Helper()
	if _, err := pl.Preview(ctx); err != nil {
		t.Fatalf("Preview %s: %v", pl.Plan.Kind, err)
	}
	rep, err := pl.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute %s: %v", pl.Plan.Kind, err)
	}
	return rep
}

func cgOK(t *testing.T, rep engine.Report) {
	t.Helper()
	if rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s %s (%s); bước %+v; kiểm %s", rep.Job.Kind, rep.Job.Status, rep.Job.Error, rep.Steps, failedChecks(rep.Checks))
	}
}

func (e *cgEnv) enable(t *testing.T, dom string) engine.Report {
	t.Helper()
	pl, err := e.cages().PrepareEnable(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatalf("PrepareEnable %s: %v", dom, err)
	}
	rep := cgRun(t, pl)
	cgOK(t, rep)
	return rep
}

func (e *cgEnv) disable(t *testing.T, dom string) engine.Report {
	t.Helper()
	pl, err := e.cages().PrepareDisable(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatalf("PrepareDisable %s: %v", dom, err)
	}
	rep := cgRun(t, pl)
	cgOK(t, rep)
	return rep
}

// cgSnap là hệ thống file giả trừ /proc.
func cgSnap(m *siteSim) map[string]system.FakeEntry {
	s := m.Snapshot()
	maps.DeleteFunc(s, func(p string, _ system.FakeEntry) bool { return strings.HasPrefix(p, "/proc") })
	return s
}

func cgDiff(a, b map[string]system.FakeEntry) string {
	var out []string
	for p, x := range a {
		if y, ok := b[p]; !ok {
			out = append(out, "mất "+p)
		} else if !reflect.DeepEqual(x, y) {
			out = append(out, "đổi "+p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			out = append(out, "thêm "+p)
		}
	}
	slices.Sort(out)
	return strings.Join(out, "; ")
}

func (e *cgEnv) noCageFiles(t *testing.T, l site.Layout) {
	t.Helper()
	for _, p := range append(l.CageFiles(), l.CageConfDir(), l.CageFenceDir(), l.CageSocket()) {
		if _, err := e.m.Stat(p); err == nil {
			t.Errorf("còn %s", p)
		}
	}
}

func resultOf(rs []checks.Result, prefix string) (checks.Result, bool) {
	for _, r := range rs {
		if strings.HasPrefix(r.ID, prefix) {
			return r, true
		}
	}
	return checks.Result{}, false
}

// ---------------------------------------------------------------- policyVerify

type cgStatic checks.Result

func (c cgStatic) Run(_ context.Context, _ system.System) checks.Result { return checks.Result(c) }

func TestPolicyVerifyBang(t *testing.T) {
	for _, tc := range []struct {
		id    string
		stage checks.Stage
		in    checks.Status
		want  checks.Status
	}{
		{"web.vhost:a.example.com", checks.StageWeb, checks.Fail, checks.Fail},
		{"config.nginx:nginx", checks.StageWeb, checks.Fail, checks.Fail},
		{"web.hardened:a.example.com", checks.StageWeb, checks.Fail, checks.Fail},
		{"cage.socket:a.example.com", checks.StagePHP, checks.Fail, checks.Fail},
		{"cage.limits:a.example.com", checks.StagePHP, checks.Fail, checks.Fail},
		{"php.socket:a.example.com", checks.StagePHP, checks.Fail, checks.Fail},
		{"cage.fence:a.example.com", checks.StagePHP, checks.Fail, checks.Warn},
		{"cage.runtime:a.example.com", checks.StagePHP, checks.Fail, checks.Warn},
		{"http.site:a.example.com", checks.StageEndToEnd, checks.Fail, checks.Warn},
		{"db.access:a.example.com", checks.StageDB, checks.Fail, checks.Warn},
		{"wp.checksums:a.example.com", checks.StageWordPress, checks.Fail, checks.Warn},
		{"isolation:a.example.com:b.example.com", checks.StageEndToEnd, checks.Fail, checks.Warn},
		// Không phải Fail thì giữ nguyên, kể cả phép mềm.
		{"http.site:a.example.com", checks.StageEndToEnd, checks.Pass, checks.Pass},
		{"http.site:a.example.com", checks.StageEndToEnd, checks.Warn, checks.Warn},
		{"http.site:a.example.com", checks.StageEndToEnd, checks.Unknown, checks.Unknown},
		{"cage.socket:a.example.com", checks.StagePHP, checks.Unknown, checks.Unknown},
		// ID giống tiền tố nhưng không phải: mềm.
		{"cage.socketx:a.example.com", checks.StagePHP, checks.Fail, checks.Warn},
	} {
		cs := policyVerify([]checks.Check{cgStatic{ID: tc.id, Stage: tc.stage, Status: tc.in}})
		if got := cs[0].Run(ctx, nil).Status; got != tc.want {
			t.Errorf("%s (%s) %s: có %s, muốn %s", tc.id, tc.stage, tc.in, got, tc.want)
		}
	}
	// Dấu "nặng" giữ qua lớp bọc (health chọn phép nặng theo dấu này).
	cs := policyVerify([]checks.Check{checks.Heavy(cgStatic{}), cgStatic{}})
	if !checks.IsHeavy(cs[0]) || checks.IsHeavy(cs[1]) {
		t.Error("policyVerify làm mất hay thêm dấu Heavy")
	}
}

// ---------------------------------------------------------------- enable, disable

func TestCageEnableDisableTraLaiTungByte(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newCgEnv(t, web, cgDom, cgOther)
			before := cgSnap(e.m)

			e.enable(t, cgDom)
			if !e.site(t, cgDom).Caged {
				t.Fatal("enable xong mà caged = 0")
			}
			if e.site(t, cgOther).Caged {
				t.Fatal("site khác bị đổi caged")
			}
			// Trong lồng: không còn pool chung, có hàng rào, vhost trỏ socket của lồng.
			l := e.l
			if _, err := e.m.Stat(l.PoolConf("8.3")); err == nil {
				t.Error("còn pool chung sau khi vào lồng")
			}
			if _, err := e.m.Stat(l.CageFence()); err != nil {
				t.Errorf("thiếu hàng rào: %v", err)
			}
			conf := l.NginxConf
			if web == Apache {
				conf = l.ApacheConf
			}
			if b, _ := e.m.ReadFile(conf); !strings.Contains(string(b), l.CageSocket()) {
				t.Errorf("vhost không trỏ socket của lồng:\n%s", b)
			}

			e.disable(t, cgDom)
			if e.site(t, cgDom).Caged {
				t.Fatal("disable xong mà caged = 1")
			}
			e.noCageFiles(t, l)
			if d := cgDiff(before, cgSnap(e.m)); d != "" {
				t.Errorf("vào rồi ra lồng không trả lại đúng từng byte: %s", d)
			}
		})
	}
}

func TestCageEnableLoiVhostHoanTacSach(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	before := cgSnap(e.m)
	pl, err := e.cages().PrepareEnable(ctx, mustDomain(t, cgDom))
	if err != nil {
		t.Fatal(err)
	}
	e.m.Fail("write", e.l.NginxConf, errors.New("đĩa đầy (giả)"))
	rep, err := pl.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Job.Status != engine.JobRolledBack {
		t.Fatalf("muốn rolled_back, có %s (%s)", rep.Job.Status, rep.Job.Error)
	}
	if e.site(t, cgDom).Caged {
		t.Error("job hoàn tác mà caged = 1")
	}
	e.noCageFiles(t, e.l)
	if d := cgDiff(before, cgSnap(e.m)); d != "" {
		t.Errorf("hoàn tác không sạch: %s", d)
	}
	if _, err := e.m.Stat(e.l.SocketFor("8.3")); err != nil {
		t.Errorf("socket pool chung chưa về: %v", err)
	}
}

// Bất biến 11: HTTP trả 500 không làm job chính sách của admin hỏng.
func TestCagePolicyKhongPhuThuocHTTP(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		if slices.Contains(c.Args, "-o") && !slices.Contains(c.Args, "-D") {
			return system.Result{Stdout: "500"}
		}
		return system.Result{Stdout: "<html>lỗi</html>TPAN_HTTP_CODE=500"}
	})
	rep := e.enable(t, cgDom)
	if r, ok := resultOf(rep.Checks, "http.site:"); !ok || r.Status != checks.Warn {
		t.Errorf("enable: http.site phải có và là Warn, có %+v", r)
	}

	// sync qua sửa gói: không có phép gọi HTTP nào trong kiểm chứng.
	p := e.acc.pkgs[cgPkg]
	p.RAMMB = 512
	pl, err := e.cages().PrepareEditPackage(ctx, p)
	if err != nil || pl == nil {
		t.Fatalf("PrepareEditPackage: %v %v", pl, err)
	}
	rep = cgRun(t, pl)
	cgOK(t, rep)
	if _, ok := resultOf(rep.Checks, "http."); ok {
		t.Error("cage.sync có phép gọi HTTP")
	}

	rep = e.disable(t, cgDom)
	if r, ok := resultOf(rep.Checks, "http.site:"); !ok || r.Status != checks.Warn {
		t.Errorf("disable: http.site phải có và là Warn, có %+v", r)
	}
}

func TestCageEnableTuChoi(t *testing.T) {
	for _, tc := range []struct {
		name string
		dom  string
		mod  func(t *testing.T, e *cgEnv)
		code string
	}{
		{"không có site", "none.example.com", nil, "site_not_found"},
		{"thùng rác", cgDom, func(t *testing.T, e *cgEnv) {
			e.setSite(t, cgDom, func(s *site.Site) { s.Status = site.Trashed })
		}, "site_trashed"},
		{"tạm khoá", cgDom, func(t *testing.T, e *cgEnv) {
			e.setSite(t, cgDom, func(s *site.Site) { s.Suspended = account.SuspendSelf })
		}, CodeSiteSuspended},
		{"gói không bật lồng", cgDom, func(t *testing.T, e *cgEnv) {
			e.acc.put(account.User{Name: account.AdminName, Role: account.RoleAdmin, Package: account.DefaultPackage})
		}, CodeCageNotInPackage},
		{"cgroup v1", cgDom, func(t *testing.T, e *cgEnv) {
			if err := e.m.Remove(cgroupControllers); err != nil {
				t.Fatal(err)
			}
		}, CodeCageCgroupV1},
		{"systemd cũ", cgDom, func(t *testing.T, e *cgEnv) { e.sysVer = "systemd 253 (253.5)\n" }, CodeCageSystemdOld},
		{"systemd lạ", cgDom, func(t *testing.T, e *cgEnv) { e.sysVer = "không phải systemd\n" }, CodeCageSystemdOld},
		{"PHP chưa cài", cgDom, func(t *testing.T, e *cgEnv) {
			if err := e.m.Remove("/usr/sbin/php-fpm8.3"); err != nil {
				t.Fatal(err)
			}
		}, "php_not_installed"},
		{"pool sửa tay", cgDom, func(t *testing.T, e *cgEnv) {
			p := e.l.PoolConf("8.3")
			b, _ := e.m.ReadFile(p)
			e.m.SetFile(p, append(b, "; sửa tay\n"...), 0o644, system.Root)
		}, CodeCageDrift},
		{"vhost sửa tay", cgDom, func(t *testing.T, e *cgEnv) {
			b, _ := e.m.ReadFile(e.l.NginxConf)
			e.m.SetFile(e.l.NginxConf, append(b, "# sửa tay\n"...), 0o644, system.Root)
		}, CodeCageDrift},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newCgEnv(t, Nginx, cgDom)
			if tc.mod != nil {
				tc.mod(t, e)
			}
			_, err := e.cages().PrepareEnable(ctx, mustDomain(t, tc.dom))
			wantRefused(t, err, tc.code)
		})
	}

	t.Run("cage_drift chỉ sang lệnh sửa", func(t *testing.T) {
		e := newCgEnv(t, Nginx, cgDom)
		p := e.l.PoolConf("8.3")
		e.m.SetFile(p, []byte("; khác hẳn\n"), 0o644, system.Root)
		_, err := e.cages().PrepareEnable(ctx, mustDomain(t, cgDom))
		wantRefused(t, err, CodeCageDrift, "sudo tpan site add "+cgDom, "sudo tpan health --fix", p)
	})
}

// Site đã trong lồng: không từ chối (cả khi không có pool chung để so), Plan
// chạy lại mà mọi bước đã đúng.
func TestCageEnableDaTrongLongChayLai(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.enable(t, cgDom)
	pl, err := e.cages().PrepareEnable(ctx, mustDomain(t, cgDom))
	if err != nil {
		t.Fatalf("chạy lại enable: %v", err)
	}
	pv, err := pl.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range pv.Steps {
		if !s.Done {
			t.Errorf("bước %s chưa đúng khi chạy lại (%s)", s.Info.ID, s.CheckError)
		}
	}
}

func TestCageEnableApacheDayDuCanhBao(t *testing.T) {
	e := newCgEnv(t, Apache, cgDom)
	e.setSite(t, cgDom, func(s *site.Site) { s.Htaccess = site.HtaccessFull })
	// Vhost chế độ day-du khác bản đang có: ghi lại cho khớp để qua cage_drift.
	vh, err := siteVhost(e.site(t, cgDom), StackConfig{Web: Apache, PHPVersion: "8.3"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range vh {
		if err := s.Apply(ctx, e.m); err != nil {
			t.Fatal(err)
		}
	}
	pl, err := e.cages().PrepareEnable(ctx, mustDomain(t, cgDom))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pl.Plan.Title.Vi, "day-du") || !strings.Contains(pl.Plan.Title.En, "day-du") {
		t.Errorf("thiếu cảnh báo day-du: %+v", pl.Plan.Title)
	}
	e2 := newCgEnv(t, Apache, cgDom)
	pl, err = e2.cages().PrepareEnable(ctx, mustDomain(t, cgDom))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pl.Plan.Title.Vi, "day-du") {
		t.Errorf("chế độ an-toan mà có cảnh báo: %s", pl.Plan.Title.Vi)
	}
}

func TestCageDisableDonTheoMay(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	_, err := e.cages().PrepareDisable(ctx, mustDomain(t, cgDom))
	wantRefused(t, err, CodeCageNotCaged)

	// Panel bị tắt giữa enable: máy có lồng, bảng vẫn caged=0. Disable dọn được
	// mà không đòi caged=1 hay cờ của gói.
	e.enable(t, cgDom)
	e.setSite(t, cgDom, func(s *site.Site) { s.Caged = false })
	e.acc.put(account.User{Name: account.AdminName, Role: account.RoleAdmin, Package: account.DefaultPackage})
	e.disable(t, cgDom)
	e.noCageFiles(t, e.l)
	if _, err := e.m.Stat(e.l.PoolConf("8.3")); err != nil {
		t.Errorf("pool chung chưa về: %v", err)
	}

	// Chỉ còn một file lẻ (passwd) cũng là "có file của lồng".
	e.m.SetFile(e.l.CagePasswd(), []byte("x\n"), 0o644, system.Root)
	if _, err := e.cages().PrepareDisable(ctx, mustDomain(t, cgDom)); err != nil {
		t.Errorf("còn passwd mà từ chối: %v", err)
	}
}

func TestCageRestart(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	_, err := e.cages().PrepareRestart(ctx, mustDomain(t, cgDom))
	wantRefused(t, err, CodeCageNotCaged)
	e.enable(t, cgDom)
	pl, err := e.cages().PrepareRestart(ctx, mustDomain(t, cgDom))
	if err != nil {
		t.Fatal(err)
	}
	if got := stepIDs(pl.Plan); len(got) != 1 || pl.Plan.Kind != "cage.restart" {
		t.Errorf("Plan %s bước %v", pl.Plan.Kind, got)
	}
	if len(pl.Plan.Verify) != 1 {
		t.Errorf("Verify phải đúng một phép cage.runtime, có %d", len(pl.Plan.Verify))
	}
	cgOK(t, cgRun(t, pl))
}

// ---------------------------------------------------------------- sửa gói, đổi gói

func TestCageEditPackage(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom, cgOther)
	c := e.cages()
	p := e.acc.pkgs[cgPkg]

	// Không có site trong lồng: nil (người gọi dùng Accounts.EditPackage).
	q := p
	q.RAMMB = 512
	if pl, err := c.PrepareEditPackage(ctx, q); err != nil || pl != nil {
		t.Fatalf("không site trong lồng: muốn nil, có %v %v", pl, err)
	}
	e.enable(t, cgDom)
	// Trần không đổi (chỉ đổi số site): nil.
	same := p
	same.MaxSites = 9
	if pl, err := c.PrepareEditPackage(ctx, same); err != nil || pl != nil {
		t.Fatalf("trần không đổi: muốn nil, có %v %v", pl, err)
	}
	// Trần sai: từ chối như EditPackage.
	bad := p
	bad.RAMMB = 10
	_, err := c.PrepareEditPackage(ctx, bad)
	wantRefused(t, err, CodePackageLimitInvalid)

	pl, err := c.PrepareEditPackage(ctx, q)
	if err != nil || pl == nil {
		t.Fatalf("PrepareEditPackage: %v %v", pl, err)
	}
	if pl.Plan.Kind != "cage.sync" || len(pl.Plan.Steps) != 1 || len(pl.Plan.Verify) != 1 {
		t.Fatalf("Plan %s, %d bước, %d kiểm", pl.Plan.Kind, len(pl.Plan.Steps), len(pl.Plan.Verify))
	}
	if cf := pl.Plan.Steps[0].(*steps.CageFiles); cf.Limits.RAMMB != 512 || cf.Layout.Domain.String() != cgDom {
		t.Errorf("bước %+v", cf)
	}
	if _, err := pl.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if e.acc.pkgs[cgPkg].RAMMB != 256 {
		t.Fatal("Preview đã ghi gói")
	}

	// Job hỏng (ghi file slice lỗi): gói vẫn là trần mới (before ghi trước engine).
	e.m.Fail("write", e.l.CageSliceFile(), errors.New("đĩa đầy (giả)"))
	rep, err := pl.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatal("ghi slice lỗi mà job thành công")
	}
	if e.acc.pkgs[cgPkg].RAMMB != 512 {
		t.Errorf("job hỏng làm mất trần mới: %+v", e.acc.pkgs[cgPkg])
	}

}

// Gói tắt lồng, trần về 0: site vẫn trong lồng, bản xem trước nói rõ.
func TestCageEditPackageTatLong(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	c := e.cages()
	e.enable(t, cgDom)
	pl, err := c.PrepareEditPackage(ctx, account.Package{Name: cgPkg})
	if err != nil || pl == nil {
		t.Fatalf("tắt lồng: %v %v", pl, err)
	}
	if !strings.Contains(pl.Plan.Title.Vi, "không bật lồng") {
		t.Errorf("tiêu đề không nói gói tắt lồng: %s", pl.Plan.Title.Vi)
	}
	cgOK(t, cgRun(t, pl))
	if got := e.acc.pkgs[cgPkg]; got.Cage || got.RAMMB != 0 {
		t.Errorf("gói sau khi sửa %+v", got)
	}
	if !e.site(t, cgDom).Caged {
		t.Error("sửa gói đưa site ra khỏi lồng")
	}
}

func TestCageEditPackageCanhBaoRAM(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.enable(t, cgDom)
	cg := "/sys/fs/cgroup/tpan.slice/" + e.l.CageSlice()
	e.m.SetDir(cg, 0o755, system.Root)
	e.m.SetFile(cg+"/memory.current", []byte(fmt.Sprint(300<<20)+"\n"), 0o444, system.Root)
	p := e.acc.pkgs[cgPkg]
	p.RAMMB = 128
	pl, err := e.cages().PrepareEditPackage(ctx, p)
	if err != nil || pl == nil {
		t.Fatal(err)
	}
	if !strings.Contains(pl.Plan.Title.Vi, cgDom+" đang dùng 300 MiB") {
		t.Errorf("thiếu cảnh báo RAM: %s", pl.Plan.Title.Vi)
	}
}

func TestCageUserEdit(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	c := e.cages()
	e.acc.pkgs["nho"] = account.Package{Name: "nho", Cage: true, RAMMB: 128}
	e.acc.pkgs["giong"] = account.Package{Name: "giong", Cage: true, RAMMB: 256}
	by := account.Identity{Name: account.AdminName, Role: account.RoleAdmin}
	pkg := func(s string) UserChange { return UserChange{Package: &s} }

	if pl, err := c.PrepareUserEdit(ctx, account.AdminName, UserChange{}, by); err != nil || pl != nil {
		t.Fatalf("không đổi gói: %v %v", pl, err)
	}
	// Chưa có site trong lồng: nil.
	if pl, err := c.PrepareUserEdit(ctx, account.AdminName, pkg("nho"), by); err != nil || pl != nil {
		t.Fatalf("không site trong lồng: %v %v", pl, err)
	}
	e.enable(t, cgDom)
	if pl, err := c.PrepareUserEdit(ctx, account.AdminName, pkg(cgPkg), by); err != nil || pl != nil {
		t.Fatalf("cùng gói: %v %v", pl, err)
	}
	if pl, err := c.PrepareUserEdit(ctx, account.AdminName, pkg("giong"), by); err != nil || pl != nil {
		t.Fatalf("gói khác cùng trần: %v %v", pl, err)
	}
	_, err := c.PrepareUserEdit(ctx, account.AdminName, pkg("khong_co"), by)
	wantRefused(t, err, CodePackageNotFound)

	pl, err := c.PrepareUserEdit(ctx, account.AdminName, pkg("nho"), by)
	if err != nil || pl == nil {
		t.Fatalf("PrepareUserEdit: %v %v", pl, err)
	}
	if _, err := pl.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if e.acc.get(account.AdminName).Package != cgPkg {
		t.Fatal("Preview đã đổi gói của user")
	}
	// HTTP trả 500 và job hỏng ở ghi file: gói của user vẫn đổi.
	e.m.Fail("write", e.l.CageSliceFile(), errors.New("đĩa đầy (giả)"))
	rep, err := pl.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatal("ghi slice lỗi mà job thành công")
	}
	if e.acc.get(account.AdminName).Package != "nho" {
		t.Errorf("job hỏng làm mất gói mới: %+v", e.acc.get(account.AdminName))
	}
}

// Bất biến 11: hạ gói của user khi site trả 500 vẫn ghi gói và áp trần.
func TestHaGoiKhiSiteTra500(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.enable(t, cgDom)
	e.m.Handle([]string{"curl"}, func(system.Cmd) system.Result { return system.Result{Stdout: "500"} })
	e.acc.pkgs["nho"] = account.Package{Name: "nho", Cage: true, RAMMB: 128}
	s := "nho"
	pl, err := e.cages().PrepareUserEdit(ctx, account.AdminName, UserChange{Package: &s}, account.Identity{Name: account.AdminName, Role: account.RoleAdmin})
	if err != nil || pl == nil {
		t.Fatal(err)
	}
	cgOK(t, cgRun(t, pl))
	b, _ := e.m.ReadFile(e.l.CageSliceFile())
	if !strings.Contains(string(b), "MemoryMax=128M") {
		t.Errorf("slice chưa mang trần mới:\n%s", b)
	}
}

// ---------------------------------------------------------------- trạng thái

func TestCageStatus(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom, cgOther)
	c := e.cages()
	rows, err := c.Status(ctx, mustDomain(t, cgDom))
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
	if r := rows[0]; r.Caged || !r.Want || r.Files || r.State != cageStateWantIn || r.Package != cgPkg || r.Limits.RAMMB != 256 || r.Usage != nil {
		t.Errorf("trước khi vào lồng: %+v", r)
	}
	e.enable(t, cgDom)
	e.setSite(t, cgOther, func(s *site.Site) { s.Status = site.Trashed })
	// Không có domain: mọi site active (cgOther đã vào thùng rác).
	rows, err = c.Status(ctx, validate.Domain{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
	if r := rows[0]; r.Domain != cgDom || !r.Caged || !r.Files || r.State != cageStateIn || r.Usage == nil || r.Owner != account.AdminName {
		t.Errorf("trong lồng: %+v", r)
	}
	_, err = c.Status(ctx, mustDomain(t, "none.example.com"))
	wantRefused(t, err, "site_not_found")

	for _, tc := range []struct {
		caged, want bool
		state       string
	}{{true, true, "trong"}, {false, false, "ngoai"}, {false, true, "can-vao"}, {true, false, "can-ra"}} {
		if got := cageStateOf(tc.caged, tc.want); got != tc.state {
			t.Errorf("cageStateOf(%v, %v) = %s", tc.caged, tc.want, got)
		}
	}
}
