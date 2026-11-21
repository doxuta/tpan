package core

// Test của sửa tự động và phép kiểm của health cho lồng (M17 đợt 3a): mỗi tên
// sửa mới dựng đúng Plan và Verify, AutoFixName đủ nhánh, Sites.Checks chọn
// đúng bộ phép kiểm. Dùng cgEnv của cage_test.go.

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

func TestAutoFixNameM17(t *testing.T) {
	d := "shop.example.com"
	for _, tc := range []struct {
		id     string
		status checks.Status
		want   string
	}{
		{"cage.socket:" + d, checks.Fail, FixCageStart},
		{"cage.socket:" + d, checks.Warn, ""},
		{"cage.runtime:" + d, checks.Fail, FixCageRestart},
		{"cage.runtime:" + d, checks.Warn, FixCageRestart},
		{"cage.runtime:" + d, checks.Unknown, ""},
		{"cage.fence:" + d, checks.Fail, FixCageSync},
		{"cage.fence:" + d, checks.Warn, ""},
		{"cage.limits:" + d, checks.Fail, FixCageSync},
		{"cage.limits:" + d, checks.Warn, ""}, // trần đĩa không áp được: không có sửa tự động
		{"cage.limits:" + d, checks.Pass, ""},
		{"probe.leftover:tpan", checks.Fail, FixProbeCleanup},
		{"probe.leftover:tpan", checks.Pass, ""},
		{"cage.state:" + d, checks.Fail, ""}, // không có ManualFix: không đoán hướng
	} {
		if got := AutoFixName(checks.Result{ID: tc.id, Status: tc.status}); got != tc.want {
			t.Errorf("AutoFixName(%q, %s) = %q, muốn %q", tc.id, tc.status, got, tc.want)
		}
	}

	// cage.state: hướng đọc từ chính kết quả của checks.CageState, trên máy giả.
	l := site.LayoutOf(mustDomain(t, d))
	for _, tc := range []struct {
		name        string
		caged, want bool
		fence       bool
		status      checks.Status
		fix         string
	}{
		{"caged=1 thiếu hàng rào", true, true, false, checks.Fail, FixCageEnable},
		{"caged=1 thiếu hàng rào, gói không muốn", true, false, false, checks.Fail, FixCageEnable},
		{"caged=0 còn hàng rào", false, false, true, checks.Fail, FixCageDisable},
		{"caged=0 còn hàng rào, gói muốn", false, true, true, checks.Fail, FixCageDisable},
		{"gói muốn, site ngoài", false, true, false, checks.Warn, FixCageEnable},
		{"gói không muốn, site trong", true, false, true, checks.Warn, FixCageDisable},
		{"khớp", true, true, true, checks.Pass, ""},
	} {
		m := system.NewFake()
		m.SetDir("/etc/php/8.3/fpm/pool.d", 0o755, system.Root)
		if tc.fence {
			m.SetFile(l.CageFence(), []byte("[Service]\n"), 0o644, system.Root)
		}
		r := checks.CageState(l, tc.caged, tc.want).Run(ctx, m)
		if r.Status != tc.status {
			t.Errorf("%s: CageState = %s (%s), muốn %s", tc.name, r.Status, r.Summary.Vi, tc.status)
			continue
		}
		if got := AutoFixName(r); got != tc.fix {
			t.Errorf("%s: AutoFixName = %q, muốn %q (ManualFix %v)", tc.name, got, tc.fix, r.ManualFix)
		}
	}
	// ManualFix của domain khác (kết quả bị sửa): không có sửa tự động.
	r := checks.Result{ID: "cage.state:" + d, Status: checks.Warn, ManualFix: []string{"tpan cage enable other.example.com"}}
	if got := AutoFixName(r); got != "" {
		t.Errorf("ManualFix của domain khác mà AutoFixName = %q", got)
	}
}

func TestFixCageStart(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	// Site ngoài lồng: không có cách sửa (cage.socket không sinh cho nó).
	_, err := e.f.Prepare(ctx, checks.Result{ID: "cage.socket:" + cgDom, Status: checks.Fail})
	wantRefused(t, err, "no_auto_fix")

	e.enable(t, cgDom)
	e.mu.Lock()
	e.units[e.l.CageSocketUnit()] = unitState{}
	e.mu.Unlock()
	_ = e.m.Remove(e.l.CageSocket())
	r := e.recheck(t, cgDom, "cage.socket:"+cgDom)
	if r.Status != checks.Fail || AutoFixName(r) != FixCageStart {
		t.Fatalf("trước khi sửa: %s %q", r.Status, AutoFixName(r))
	}
	x := e.fix(t, r)
	if got := stepIDsOf(x); !slices.Equal(got, []string{"cage.socket:" + cgDom}) {
		t.Errorf("bước %v", got)
	}
	if r := e.recheck(t, cgDom, "cage.socket:"+cgDom); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}
}

func TestFixCageRestart(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.enable(t, cgDom)
	x := e.fix(t, checks.Result{ID: "cage.runtime:" + cgDom, Status: checks.Warn})
	if got := stepIDsOf(x); !slices.Equal(got, []string{"cage.restart:" + cgDom}) {
		t.Errorf("bước %v", got)
	}
	if len(x.Plan.Verify) != 1 {
		t.Errorf("Verify %d phép, muốn đúng cage.runtime", len(x.Plan.Verify))
	}
}

func TestFixCageSync(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.enable(t, cgDom)
	p := e.acc.pkgs[cgPkg]
	p.RAMMB = 512
	e.acc.pkgs[cgPkg] = p // gói đổi trong bảng mà máy chưa theo
	x := e.fix(t, checks.Result{ID: "cage.limits:" + cgDom, Status: checks.Fail})
	if got := stepIDsOf(x); !slices.Equal(got, []string{"cage.files:" + cgDom}) {
		t.Errorf("bước %v", got)
	}
	if cf := x.Plan.Steps[0].(*steps.CageFiles); cf.Limits.RAMMB != 512 {
		t.Errorf("trần của bước %+v", cf.Limits)
	}
	if b, _ := e.m.ReadFile(e.l.CageSliceFile()); !strings.Contains(string(b), "MemoryMax=512M") {
		t.Errorf("slice sau khi sửa:\n%s", b)
	}
	// cage.fence cũng là cage.sync.
	if _, err := e.f.Prepare(ctx, checks.Result{ID: "cage.fence:" + cgDom, Status: checks.Fail}); err != nil {
		t.Errorf("cage.fence: %v", err)
	}
}

func TestFixCageEnableDisableTuCageState(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	r := e.recheck(t, cgDom, "cage.state:"+cgDom)
	if r.Status != checks.Warn || AutoFixName(r) != FixCageEnable {
		t.Fatalf("gói muốn lồng, site ngoài: %s %q", r.Status, AutoFixName(r))
	}
	x := e.fix(t, r)
	if x.Plan.Kind != "fix.cage.enable" {
		t.Errorf("Kind %s", x.Plan.Kind)
	}
	if !e.site(t, cgDom).Caged {
		t.Fatal("sửa cage.enable xong mà caged = 0 (after bị bỏ)")
	}
	if r := e.recheck(t, cgDom, "cage.state:"+cgDom); r.Status != checks.Pass {
		t.Errorf("sau khi vào: %s %s", r.Status, r.Summary.Vi)
	}

	e.acc.put(account.User{Name: account.AdminName, Role: account.RoleAdmin, Package: account.DefaultPackage})
	r = e.recheck(t, cgDom, "cage.state:"+cgDom)
	if r.Status != checks.Warn || AutoFixName(r) != FixCageDisable {
		t.Fatalf("gói không muốn, site trong: %s %q", r.Status, AutoFixName(r))
	}
	e.fix(t, r)
	if e.site(t, cgDom).Caged {
		t.Fatal("sửa cage.disable xong mà caged = 1")
	}
	// Site thường, gói không muốn, không còn hàng rào: không sinh cage.state.
	cs, err := e.s.Checks(ctx, mustDomain(t, cgDom))
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range checks.RunAll(ctx, e.m, cs) {
		if strings.HasPrefix(res.ID, "cage.") {
			t.Errorf("site thường mà có %s", res.ID)
		}
	}
}

func TestFixKhongCoChoLong(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	// probe.cleanup khi không có sổ phép thử.
	_, err := e.f.Prepare(ctx, checks.Result{ID: "probe.leftover:tpan", Status: checks.Fail})
	wantRefused(t, err, "no_auto_fix")

	e.enable(t, cgDom)
	// php.restart cho site trong lồng: không có pool chung để khởi động lại.
	_, err = e.f.Prepare(ctx, checks.Result{ID: "php.socket:" + cgDom, Status: checks.Fail})
	wantRefused(t, err, "no_auto_fix")
	// passwd của lồng lấy từ getent lúc chạy: không có sha256 để ghi lại.
	_, err = e.f.Prepare(ctx, checks.Result{ID: "drift:" + e.l.CagePasswd(), Status: checks.Fail})
	wantRefused(t, err, "no_auto_fix")
	// Domain không ở dạng chuẩn trong ID.
	_, err = e.f.Prepare(ctx, checks.Result{ID: "cage.socket:SHOP.example.com", Status: checks.Fail})
	wantRefused(t, err, "no_auto_fix")
}

func TestFixFileRewriteCuaLong(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.enable(t, cgDom)
	p := e.l.CageSliceFile()
	orig, err := e.m.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	e.m.SetFile(p, []byte("[Slice]\nMemoryMax=8M\n"), 0o644, system.Root)
	x := e.fix(t, checks.Result{ID: "drift:" + p, Status: checks.Fail})
	if got := stepIDsOf(x); !slices.Equal(got, []string{"cage.files:" + cgDom}) {
		t.Errorf("bước %v", got)
	}
	if got, _ := e.m.ReadFile(p); string(got) != string(orig) {
		t.Errorf("slice sau khi sửa:\n%s", got)
	}
	// siteFiles của site trong lồng: file của lồng thay cho pool chung.
	st := e.site(t, cgDom)
	lim := e.acc.pkgs[cgPkg].Limits()
	fl, err := siteFilesLim(StackConfig{Web: Nginx, PHPVersion: "8.3"}, st, &lim)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range fl {
		paths = append(paths, f.Path)
	}
	if slices.Contains(paths, e.l.PoolConf("8.3")) || !slices.Contains(paths, p) || !slices.Contains(paths, e.l.CageFence()) {
		t.Errorf("siteFiles của site trong lồng: %v", paths)
	}
	if _, err := siteFiles(StackConfig{Web: Nginx, PHPVersion: "8.3"}, st); err == nil {
		t.Error("siteFiles không trần cho site trong lồng mà không lỗi")
	}
}

func TestSiteChecksCuaLong(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom, cgOther)
	ids := func(dom string) []string {
		t.Helper()
		cs, err := e.s.Checks(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range checks.RunAll(ctx, e.m, cs) {
			kind, _, _ := strings.Cut(r.ID, ":")
			out = append(out, kind)
		}
		return out
	}
	// Gói muốn lồng: site ngoài lồng có cage.state (cảnh báo), vẫn php.socket.
	if got := ids(cgDom); !slices.Contains(got, "cage.state") || !slices.Contains(got, "php.socket") {
		t.Errorf("site ngoài lồng, gói muốn: %v", got)
	}
	e.enable(t, cgDom)
	got := ids(cgDom)
	for _, k := range []string{"cage.socket", "cage.runtime", "cage.fence", "cage.limits", "cage.state"} {
		if !slices.Contains(got, k) {
			t.Errorf("site trong lồng thiếu %s: %v", k, got)
		}
	}
	if slices.Contains(got, "php.socket") {
		t.Errorf("site trong lồng có php.socket: %v", got)
	}

	// DB đang khoá: cage.socket, cage.runtime thay cho php.socket.
	e.setSite(t, cgDom, func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	got = ids(cgDom)
	if !slices.Contains(got, "cage.socket") || !slices.Contains(got, "cage.runtime") || slices.Contains(got, "php.socket") {
		t.Errorf("DB khoá, site trong lồng: %v", got)
	}
	// Site tạm khoá vẫn có cage.state.
	e.setSite(t, cgDom, func(s *site.Site) { s.DBSuspended = account.NotSuspended; s.Suspended = account.SuspendSelf })
	if got := ids(cgDom); !slices.Contains(got, "cage.state") || slices.Contains(got, "cage.socket") {
		t.Errorf("site tạm khoá trong lồng: %v", got)
	}
}
