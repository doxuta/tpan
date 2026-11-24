package core

// Test tái hiện các phát hiện của đợt soát cuối M17 (N4, V3, L1): mỗi test đỏ
// trên mã trước bản sửa, xanh sau bản sửa.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// cageJustEnabled giả lập một job cage.enable của admin chạy xong giữa lúc
// một Plan ngoài lồng được dựng và lúc nó chạy (N4): hàng rào có, pool chung
// của bản đang dùng đã gỡ, bảng sites ghi caged=1.
func cageJustEnabled(t *testing.T, m *siteSim, ms *memSites, dom string) {
	t.Helper()
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	m.SetFile(l.CageFence(), []byte("[Service]\n"), 0o644, system.Root)
	_ = m.Remove(l.PoolConf("8.3"))
	st, ok, err := ms.Site(ctx, d)
	if err != nil || !ok {
		t.Fatalf("site %s: %v %v", dom, ok, err)
	}
	st.Caged = true
	if err := ms.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
}

// n4After kiểm máy và bảng sites sau khi Plan cũ bị chặn: không pool chung nào
// được ghi, vhost giữ nguyên từng byte, dòng site vẫn caged=1 với bản PHP cũ
// (móc after không chạy).
func n4After(t *testing.T, m *siteSim, ms *memSites, dom string, vhBefore []byte, rep engine.Report, poolStep string) {
	t.Helper()
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatalf("job chạy xong dù site đã vào lồng: %+v", rep.Steps)
	}
	for _, s := range rep.Steps {
		if s.ID == poolStep && s.Outcome != "failed" {
			t.Errorf("bước %s: %s, muốn failed", poolStep, s.Outcome)
		}
	}
	if !strings.Contains(rep.Job.Error, "lồng") {
		t.Errorf("lỗi job không nói site đang ở trong lồng: %s", rep.Job.Error)
	}
	for _, v := range []string{"8.3", "8.4"} {
		if _, err := m.Stat(l.PoolConf(v)); err == nil {
			t.Errorf("pool chung %s được ghi cho site trong lồng", l.PoolConf(v))
		}
	}
	if vh, _ := m.ReadFile(l.NginxConf); !reflect.DeepEqual(vh, vhBefore) {
		t.Error("vhost đổi")
	}
	st, _, _ := ms.Site(ctx, d)
	if !st.Caged || st.PHPVersion != "8.3" {
		t.Errorf("bảng sites sau job: caged=%v php=%s; muốn caged=1, 8.3", st.Caged, st.PHPVersion)
	}
}

// N4: Plan php.use dựng khi site còn ngoài lồng, chạy sau một lần cage.enable.
func TestPlanCuNgoaiLongKhongDuaSiteRaLong(t *testing.T) {
	pe := newUseEnv(t)
	d := mustDomain(t, useDom)
	pl, err := pe.p.PrepareUse(ctx, d, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	cageJustEnabled(t, pe.m, pe.sites, useDom)
	vh, _ := pe.m.ReadFile(site.LayoutOf(d).NginxConf)
	rep, err := pl.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	n4After(t, pe.m, pe.sites, useDom, vh, rep, "file:"+site.LayoutOf(d).PoolConf("8.4"))
}

// N4, biến thể: chạy lại site add dựng khi site còn ngoài lồng.
func TestChayLaiSiteAddCuKhongDuaSiteRaLong(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	se.add(t, useDom)
	d := mustDomain(t, useDom)
	a, err := se.s.PrepareAdd(context.Background(), se.input(t, useDom))
	if err != nil {
		t.Fatal(err)
	}
	cageJustEnabled(t, se.m, se.sites, useDom)
	vh, _ := se.m.ReadFile(site.LayoutOf(d).NginxConf)
	res, err := a.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	n4After(t, se.m, se.sites, useDom, vh, res.Report, "file:"+site.LayoutOf(d).PoolConf("8.3"))
}

// V3: pool chung sót ở bản PHP khác (8.1 đã cài; 7.4 chỉ còn file, gói đã gỡ)
// được gỡ khi vào lồng, nên cage.state đạt ngay sau đó.
func TestCageEnableGoPoolChungBanKhac(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	l := e.l
	pool, err := e.m.ReadFile(l.PoolConf("8.3"))
	if err != nil {
		t.Fatal(err)
	}
	e.m.SetFile("/usr/sbin/php-fpm8.1", []byte("ELF"), 0o755, system.Root)
	e.m.SetFile(l.PoolConf("8.1"), pool, 0o644, system.Root)
	e.m.SetFile(l.PoolConf("7.4"), pool, 0o644, system.Root)
	e.m.ResetRan()

	e.enable(t, cgDom)
	for _, v := range []string{"8.3", "8.1", "7.4"} {
		if _, err := e.m.Stat(l.PoolConf(v)); err == nil {
			t.Errorf("còn pool chung %s sau khi vào lồng", l.PoolConf(v))
		}
	}
	var reload81, test74 bool
	for _, c := range e.m.Ran() {
		reload81 = reload81 || c.String() == "systemctl reload php8.1-fpm"
		test74 = test74 || c.Name == "php-fpm7.4"
	}
	if !reload81 {
		t.Error("gỡ pool 8.1 mà không reload php8.1-fpm (tiến trình con của pool vẫn chạy)")
	}
	if test74 {
		t.Error("chạy php-fpm7.4 dù bản 7.4 chưa cài")
	}
	if r := checks.CageState(l, true, true).Run(ctx, e.m); r.Status != checks.Pass {
		t.Errorf("cage.state sau khi vào lồng: %s %s", r.Status, r.Summary.Vi)
	}
}

// V3 qua đường của health: site đã trong lồng, pool chung ở bản khác xuất hiện;
// cage.state báo lỗi, sửa tự động (cage.enable) gỡ nó và lần kiểm sau đạt.
func TestHealthFixGoPoolChungBanKhac(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	pool, err := e.m.ReadFile(e.l.PoolConf("8.3"))
	if err != nil {
		t.Fatal(err)
	}
	e.enable(t, cgDom)
	e.m.SetFile("/usr/sbin/php-fpm8.1", []byte("ELF"), 0o755, system.Root)
	e.m.SetFile(e.l.PoolConf("8.1"), pool, 0o644, system.Root)
	r := e.recheck(t, cgDom, "cage.state:"+cgDom)
	if r.Status != checks.Fail || AutoFixName(r) != FixCageEnable {
		t.Fatalf("pool sót: cage.state %s, sửa %q", r.Status, AutoFixName(r))
	}
	e.fix(t, r)
	if r := e.recheck(t, cgDom, "cage.state:"+cgDom); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}
}

// V1: còn thư mục cấu hình của lồng mà không còn file nào. Lớp bọc lệnh từ
// chối mọi lệnh của site và chỉ tới `cage disable`, nên lệnh đó phải nhận và
// dọn được.
func TestCageDisableDonThuMucLongConSot(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.m.SetDir(e.l.CageConfDir(), 0o755, system.Root)
	e.disable(t, cgDom)
	if _, err := e.m.Stat(e.l.CageConfDir()); err == nil {
		t.Errorf("còn %s sau cage disable", e.l.CageConfDir())
	}
}

// cgLookup trả đúng một kết quả phép kiểm đã lưu.
type cgLookup checks.Result

func (c cgLookup) CheckResultOf(_ context.Context, id string) (checks.Result, bool, error) {
	return checks.Result(c), id == c.ID, nil
}

// L1: sửa tự động do agent đề xuất (Planner, PlanHealthFix) cho site trong
// lồng đọc được trần của gói.
func TestPlannerSuaChoSiteTrongLong(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	e.enable(t, cgDom)
	id := "cage.limits:" + cgDom
	p := &Planner{Sites: e.s, Checks: cgLookup{ID: id, Status: checks.Fail}}
	pp, err := p.Prepare(ctx, PlanRequest{Kind: PlanHealthFix, CheckID: id})
	if err != nil {
		t.Fatalf("Prepare %s: %v", id, err)
	}
	if got := stepIDs(pp.Plan); len(got) != 1 || got[0] != "cage.files:"+cgDom {
		t.Errorf("bước %v", got)
	}
}

// N4 ở đường sửa tự động: Plan "ghi lại file pool chung" dựng khi site còn ở
// ngoài lồng phải mang bước có gác hàng rào; site vào lồng trước khi Plan chạy
// thì job hỏng ở bước đó, không ghi lại pool chung.
func TestFixGhiLaiPoolChungCoGacLong(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	pool := l.PoolConf("8.3")
	e.m.SetFile(pool, []byte("; sửa tay\n"), 0o644, system.Root)
	x, err := e.f.Prepare(ctx, checks.Result{ID: "drift:" + pool, Status: checks.Warn})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := x.Plan.Steps[0].(*cageAbsentPool); !ok {
		t.Fatalf("bước ghi lại pool chung phải có gác hàng rào, có %T", x.Plan.Steps[0])
	}
	// Admin vừa đưa site vào lồng: hàng rào có, pool chung đã gỡ.
	e.m.SetFile(l.CageFence(), []byte("[Service]\n"), 0o644, system.Root)
	_ = e.m.Remove(pool)
	rep, err := x.Execute(ctx, nil)
	if err == nil && rep.Job.Status == engine.JobSucceeded {
		t.Fatal("job ghi lại pool chung thành công dù site đã vào lồng")
	}
	if _, err := e.m.ReadFile(pool); err == nil {
		t.Error("pool chung bị ghi lại cho site đã vào lồng")
	}
}
