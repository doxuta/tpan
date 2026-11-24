package demo

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func m17Cages(sys system.System, st *store.Store) *core.Cages {
	return &core.Cages{Sys: sys, Settings: st, Store: st, Accounts: st, Journal: st}
}

func m17Usages(sys system.System, st *store.Store) *core.Usages {
	return &core.Usages{Sys: sys, Store: st, Accounts: st, Samples: st}
}

// Màn Lồng: mỗi site active ở đúng trạng thái mong đợi, site trong lồng có
// file hàng rào trên máy và số liệu đọc từ cgroup.
func TestM17CageStatus(t *testing.T) {
	ctx, sys, st := newDemo(t)
	rows, err := m17Cages(sys, st).Status(ctx, validate.Domain{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]core.CageRow{}
	for _, r := range rows {
		got[r.Domain] = r
	}
	want := map[string]string{
		siteLongShop: "trong", siteLongNews: "trong",
		siteShop: "can-vao", siteWiki: "can-vao", siteLab: "can-vao",
		siteBlog: "ngoai", siteLongEdu: "ngoai",
	}
	for dom, state := range want {
		r, ok := got[dom]
		if !ok || r.State != state {
			t.Errorf("%s: trạng thái %q (có=%v); muốn %q", dom, r.State, ok, state)
			continue
		}
		caged := state == "trong"
		if r.Caged != caged || r.Files != caged || (r.Usage != nil) != caged {
			t.Errorf("%s: Caged=%v Files=%v Usage=%v; muốn %v", dom, r.Caged, r.Files, r.Usage != nil, caged)
		}
		if caged && !r.Usage.Running {
			t.Errorf("%s: cgroup của lồng chưa có", dom)
		}
	}
	if got[siteLongShop].Owner == got[siteLongNews].Owner {
		t.Errorf("hai site trong lồng cùng chủ %s; muốn hai chủ khác nhau", got[siteLongShop].Owner)
	}

	pb, _, _ := st.Package(ctx, pkgBasic)
	pp, _, _ := st.Package(ctx, pkgPlus)
	if !pb.Cage || pb.Limits() != basicLimits || !pp.Cage || pp.Limits() != plusLimits {
		t.Errorf("gói lồng: %s %+v, %s %+v", pkgBasic, pb, pkgPlus, pp)
	}
	if def := got[siteBlog]; def.Package != "default" || def.Want {
		t.Errorf("gói default phải không lồng: %+v", def)
	}
}

// Số liệu đọc ngay (List) là đúng số trong file cgroup, và site chạm trần RAM
// có oom_kill 2 với đỉnh sát trần.
func TestM17UsageMatchesCgroup(t *testing.T) {
	ctx, sys, st := newDemo(t)
	list, err := m17Usages(sys, st).List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var doms []string
	for _, u := range list {
		doms = append(doms, u.Domain)
		l, _ := layoutOf(u.Domain)
		// Các số không đổi theo nhịp sống của máy giả (đỉnh, bộ đếm oom, trần):
		// so thẳng với file.
		for name, v := range map[string]int64{
			"memory.peak": u.Usage.MemPeak, "memory.max": u.Usage.MemMax, "pids.max": u.Usage.TasksMax,
		} {
			b, err := sys.ReadFile(l.CageCgroup() + "/" + name)
			if err != nil || strings.TrimSpace(string(b)) != strconv.FormatInt(v, 10) {
				t.Errorf("%s %s: file %q, API %d (%v)", u.Domain, name, b, v, err)
			}
		}
		if u.Usage.MemMax != int64(u.Limits.RAMMB)<<20 || u.Usage.TasksMax != int64(u.Limits.Tasks) ||
			u.Usage.CPUQuotaUs*100/u.Usage.CPUPeriod != int64(u.Limits.CPUPercent) || u.Usage.IORBps != int64(u.Limits.IOReadMB)*1_000_000 {
			t.Errorf("%s: trần trong cgroup %+v khác gói %+v", u.Domain, u.Usage, u.Limits)
		}
		if u.Usage.MemCurrent <= 0 || u.Usage.CPUUsec <= 0 || u.Usage.Tasks <= 0 || u.Usage.IOWBytes <= 0 {
			t.Errorf("%s: số liệu rỗng %+v", u.Domain, u.Usage)
		}
	}
	if !slices.Equal(doms, []string{siteLongShop, siteLongNews}) {
		t.Fatalf("site có số liệu = %v", doms)
	}
	hot := list[0].Usage
	if hot.OOMKills != 2 || hot.MemPeak < hot.MemMax-(2<<20) || hot.MemPeak > hot.MemMax {
		t.Errorf("%s: oom_kill %d, đỉnh %d, trần %d; muốn 2 lần và đỉnh sát trần", siteLongShop, hot.OOMKills, hot.MemPeak, hot.MemMax)
	}
}

// Lịch sử và gợi ý: đủ mẫu (không low_data), có lý do, và ít nhất một gói
// được gợi ý khác mức đang đặt.
func TestM17HistoryAndSuggestions(t *testing.T) {
	ctx, sys, st := newDemo(t)
	u := m17Usages(sys, st)
	for _, dom := range []string{siteLongShop, siteLongNews} {
		d := domain(t, dom)
		day, err := u.History(ctx, d, "day")
		if err != nil || len(day.Points) < 280 {
			t.Errorf("%s day: %d điểm, %v", dom, len(day.Points), err)
		}
		week, err := u.History(ctx, d, "week")
		if err != nil || len(week.Points) < 24 {
			t.Errorf("%s week: %d điểm, %v", dom, len(week.Points), err)
		}
		var cpu float64
		for _, p := range day.Points {
			cpu = max(cpu, p.CPUPct)
		}
		if cpu <= 0 {
			t.Errorf("%s: CPU của ngày luôn 0", dom)
		}
	}

	rep, err := u.Suggestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sites) != 2 || len(rep.Packages) != 2 {
		t.Fatalf("gợi ý: %d site, %d gói", len(rep.Sites), len(rep.Packages))
	}
	for _, s := range rep.Sites {
		if s.LowData || s.Samples < core.UsageLowData || len(s.Reasons) != 3 {
			t.Errorf("%s: low_data=%v, %d mẫu, %d lý do", s.Domain, s.LowData, s.Samples, len(s.Reasons))
		}
		if s.Domain == siteLongShop && (s.OOMKills != 2 || s.Suggested.RAMMB <= basicLimits.RAMMB) {
			t.Errorf("%s: oom %d, RAM gợi ý %d; muốn nâng RAM vì chạm trần", s.Domain, s.OOMKills, s.Suggested.RAMMB)
		}
	}
	changed := 0
	for _, p := range rep.Packages {
		c, s := p.Current, p.Suggested
		if c.RAMMB != s.RAMMB || c.CPUPercent != s.CPUPercent || c.Tasks != s.Tasks {
			changed++
		}
	}
	if changed == 0 {
		t.Errorf("không gói nào có gợi ý khác mức đang đặt: %+v", rep.Packages)
	}
}

// Sổ thử tấn công: lượt gần nhất đủ 14 đường, không ô nào lộ, đã dọn; một
// lượt hỏng chưa dọn (mới 40 phút, nên health chưa báo).
func TestM17Probe(t *testing.T) {
	ctx, sys, st := newDemo(t)
	last, err := (&core.Probe{Sys: sys, Runs: st}).Last(ctx)
	if err != nil || last == nil {
		t.Fatalf("Last: %v, %v", last, err)
	}
	var paths []string
	for _, r := range last.Results {
		paths = append(paths, r.Path)
		for _, c := range []core.ProbeCell{r.Outside, r.Inside} {
			if c.Detail.Vi == "" || c.Detail.En == "" || (c.Status != core.ProbeNA && c.Status != core.ProbeSkipped && len(c.Evidence) == 0) {
				t.Errorf("%s: ô thiếu chi tiết hay bằng chứng: %+v", r.Path, c)
			}
		}
		if r.Inside.Status != core.ProbeBlocked && r.Inside.Status != core.ProbeNA {
			t.Errorf("%s trong lồng = %s; muốn blocked", r.Path, r.Inside.Status)
		}
		if r.Outside.Status == core.ProbeExposed {
			t.Errorf("%s ngoài lồng lộ", r.Path)
		}
	}
	want := []string{"L1", "L2", "L3", "L4", "L5", "L6", "L7", "L8", "L9", "C1", "C2", "C3", "C4", "C5"}
	if !slices.Equal(paths, want) || last.Exposed != 0 || !last.Cleaned || last.Web != core.Nginx {
		t.Errorf("Last: đường %v, exposed %d, cleaned %v, web %s", paths, last.Exposed, last.Cleaned, last.Web)
	}
	if !site.IsProbeDomain(last.DomainA) || !site.IsProbeDomain(last.DomainB) {
		t.Errorf("tên miền thử %s, %s không theo mẫu dành riêng", last.DomainA, last.DomainB)
	}
	pending, err := st.PendingProbeRuns(ctx)
	if err != nil || len(pending) != 1 || pending[0].State != "failed" || !site.IsProbeDomain(pending[0].DomainA) ||
		time.Since(pending[0].Started) >= health.ProbeLeftoverAfter {
		t.Errorf("lượt chưa dọn: %+v, %v", pending, err)
	}
}

// Health đầy đủ trên máy demo: mọi phép kiểm cage.* của site trong lồng đạt;
// site gói muốn lồng mà chưa vào chỉ có cage.state Warn.
func TestM17HealthOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	_, wpcli := Hashes()
	rep, err := (&health.Runner{Sys: sys, Store: st, WPCLISHA512: wpcli, NoSelf: true}).Run(ctx, health.Options{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]checks.Status{}
	for _, g := range rep.Groups {
		for _, x := range g.Results {
			seen[x.ID] = x.Status
			_, dom, _ := strings.Cut(x.ID, ":")
			switch {
			case dom == siteLongShop || dom == siteLongNews:
				if x.Status != checks.Pass && !strings.HasPrefix(x.ID, "tls.cert:") && !strings.HasPrefix(x.ID, "dns.") {
					t.Errorf("%s = %s: %s", x.ID, x.Status, x.Summary.Vi)
				}
			case strings.HasPrefix(x.ID, "cage."):
				if x.ID != "cage.state:"+dom || x.Status != checks.Warn {
					t.Errorf("%s = %s: %s", x.ID, x.Status, x.Summary.Vi)
				}
			case x.ID == "probe.leftover:tpan" && x.Status != checks.Pass:
				t.Errorf("%s = %s", x.ID, x.Status)
			}
		}
	}
	for _, dom := range []string{siteLongShop, siteLongNews} {
		for _, k := range []string{"cage.socket", "cage.runtime", "cage.fence", "cage.limits", "cage.state", "http.site"} {
			if seen[k+":"+dom] != checks.Pass {
				t.Errorf("%s:%s = %q; muốn pass", k, dom, seen[k+":"+dom])
			}
		}
	}
	for _, dom := range []string{siteShop, siteWiki, siteLab} {
		if seen["cage.state:"+dom] != checks.Warn {
			t.Errorf("cage.state:%s = %q; muốn warn (gói muốn lồng, site chưa vào)", dom, seen["cage.state:"+dom])
		}
	}
}

// Hai nút của màn Lồng chạy được trên máy giả: đưa một site ra khỏi lồng, đưa
// site "can-vao" vào lồng; cả hai job xong và site vẫn trả 200.
func TestM17CageButtonsOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	c := m17Cages(sys, st)
	run := func(what string, p *core.Planned, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		rep, err := p.Execute(ctx, nil)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s: job %s %s, %v", what, rep.Job.Status, rep.Job.Error, err)
		}
	}
	news, shop := domain(t, siteLongNews), domain(t, siteShop)
	p, err := c.PrepareDisable(ctx, news)
	run("cage.disable", p, err)
	p, err = c.PrepareEnable(ctx, shop)
	run("cage.enable", p, err)

	for dom, caged := range map[string]bool{siteLongNews: false, siteShop: true} {
		s, _, _ := st.Site(ctx, domain(t, dom))
		if s.Caged != caged {
			t.Errorf("%s: caged=%v; muốn %v", dom, s.Caged, caged)
		}
		cs, err := (&core.Sites{Sys: sys, Settings: st, Store: st, Accounts: st}).Checks(ctx, s.Domain)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range checks.RunAll(ctx, sys, cs) {
			ok := r.Status == checks.Pass || strings.HasPrefix(r.ID, "tls.cert:") || strings.HasPrefix(r.ID, "dns.") ||
				(r.ID == "cage.state:"+dom && !caged && r.Status == checks.Warn)
			if !ok {
				t.Errorf("%s sau khi đổi lồng: %s = %s: %s", dom, r.ID, r.Status, r.Summary.Vi)
			}
		}
	}
}

// Dựng máy demo phải nhanh: `tpan serve --demo` dựng lại từ đầu mỗi lần chạy.
func TestM17DemoStartsFast(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	_, st, err := New(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	took := time.Since(start)
	t.Logf("dựng máy demo mất %v", took)
	n := 0
	for _, dom := range []string{siteLongShop, siteLongNews} {
		s, _ := st.UsageSamples(ctx, dom, time.Now().Add(-8*24*time.Hour), time.Now().Add(time.Hour))
		n += len(s)
	}
	t.Logf("%d mẫu 5 phút", n)
	if took > 5*time.Second {
		t.Errorf("dựng máy demo mất %v", took)
	}
}
