package core

// Test phần ghép lồng (M17, đợt 3b) vào các thao tác đã có của core: mục 3.6
// của docs/thay-doi/M17-thiet-ke.md, bảng "Ghép với thao tác đã có" và bảng
// "Mọi chỗ trong internal/core đang giả định pool chung". Site ngoài lồng giữ
// đúng Plan cũ: điều đó do các test cũ (xanh nguyên) chứng minh.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// ghepPkg là gói default bật lồng, có trần RAM để thấy trần đi đúng vào bước.
var ghepPkg = account.Package{Name: account.DefaultPackage, Cage: true, RAMMB: 256}

// ghepCageOn ghi gói default bật lồng vào store thật và gắn nó làm Accounts.
func ghepCageOn(t *testing.T, se *siteEnv) account.Limits {
	t.Helper()
	if err := se.st.SavePackage(ctx, ghepPkg); err != nil {
		t.Fatal(err)
	}
	se.s.Accounts = se.st
	return ghepPkg.Limits()
}

// ghepCage đặt cờ caged (và mod) cho dòng site dom đang có.
func ghepCage(t *testing.T, se *siteEnv, dom string, mod func(*site.Site)) site.Site {
	t.Helper()
	st, ok, err := se.sites.Site(ctx, mustDomain(t, dom))
	if err != nil || !ok {
		t.Fatalf("site %s: %v %v", dom, ok, err)
	}
	st.Caged = true
	if mod != nil {
		mod(&st)
	}
	if err := se.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st
}

// ghepIdx là chỉ số của bước đầu tiên thoả pred, -1 khi không có.
func ghepIdx(ss []engine.Step, pred func(engine.Step) bool) int {
	return slices.IndexFunc(ss, pred)
}

func isCageFiles(remove bool) func(engine.Step) bool {
	return func(s engine.Step) bool { c, ok := s.(*steps.CageFiles); return ok && c.Remove == remove }
}

func isCageSocket(disable bool) func(engine.Step) bool {
	return func(s engine.Step) bool { c, ok := s.(*steps.CageSocket); return ok && c.Disable == disable }
}

// runsWP: bước chạy lệnh wp dưới user của site.
func runsWP(s engine.Step) bool {
	if _, ok := s.(*steps.WPConstant); ok {
		return true
	}
	return slices.ContainsFunc(s.Info().Commands, func(c string) bool { return strings.HasPrefix(c, "wp ") })
}

// ghepNoSharedPool: không bước nào ghi hay đổi tên pool chung (steps.SitePool,
// PoolSuspend, PoolUnsuspend). Bước gỡ (absent) được phép: purge giữ bước gỡ
// pool của M2, tự bỏ qua khi không có file.
func ghepNoSharedPool(t *testing.T, ss []engine.Step) {
	t.Helper()
	for _, s := range ss {
		id := s.Info().ID
		if strings.Contains(id, "/fpm/pool.d/") && !strings.HasPrefix(id, "absent:") {
			t.Errorf("site trong lồng mà Plan có bước pool chung %s", id)
		}
	}
}

// ghepCheckIDs chạy các phép kiểm trên máy giả, trả ID của kết quả.
func ghepCheckIDs(sys system.System, cs []checks.Check) []string {
	var ids []string
	for _, r := range checks.RunAll(ctx, sys, cs) {
		ids = append(ids, r.ID)
	}
	return ids
}

// ghepVerifyCaged: kiểm chứng là bộ của lồng (cage.socket, cage.limits), không
// có php.socket của pool chung.
func ghepVerifyCaged(t *testing.T, sys system.System, cs []checks.Check, dom string) {
	t.Helper()
	ids := ghepCheckIDs(sys, cs)
	for _, want := range []string{"cage.socket:" + dom, "cage.limits:" + dom} {
		if !slices.Contains(ids, want) {
			t.Errorf("kiểm chứng thiếu %s; có %v", want, ids)
		}
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "php.socket:"+dom) {
			t.Errorf("kiểm chứng của site trong lồng có %s", id)
		}
	}
}

func TestMoiThaoTacCuaChuGiuLong(t *testing.T) {
	const dom = "a.example.com"

	t.Run("chay lai site add", func(t *testing.T) {
		se := newSiteEnv(t, Nginx)
		se.add(t, dom)
		lim := ghepCageOn(t, se)
		ghepCage(t, se, dom, nil)
		a, err := se.s.PrepareAdd(ctx, se.input(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		ss := a.Plan.Steps
		f := ghepIdx(ss, isCageFiles(false))
		if f < 0 || ghepIdx(ss, isCageSocket(false)) < 0 {
			t.Fatalf("chạy lại site add của site trong lồng thiếu bước của lồng: %v", stepIDs(a.Plan))
		}
		if got := ss[f].(*steps.CageFiles).Limits; got != lim {
			t.Errorf("CageFiles mang trần %+v, muốn %+v", got, lim)
		}
		if w := ghepIdx(ss, runsWP); w >= 0 && w < f {
			t.Errorf("bước chạy wp %s đứng trước hàng rào", ss[w].Info().ID)
		}
		ghepNoSharedPool(t, ss)
		// Vhost của lồng trỏ socket của lồng, không trỏ socket pool chung.
		l := site.LayoutOf(mustDomain(t, dom))
		for _, s := range ss {
			if fs, ok := s.(*steps.File); ok && fs.Path == l.NginxConf && !strings.Contains(string(fs.Content), l.CageSocket()) {
				t.Errorf("vhost của site trong lồng không trỏ %s", l.CageSocket())
			}
		}
		ghepVerifyCaged(t, se.m, a.Plan.Verify, dom)
	})

	t.Run("suspend va unsuspend", func(t *testing.T) {
		se := newSiteEnv(t, Nginx)
		se.add(t, dom)
		ghepCageOn(t, se)
		ghepCage(t, se, dom, nil)
		p, err := se.s.PrepareSuspend(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		if ghepIdx(p.Plan.Steps, isCageSocket(true)) < 0 {
			t.Errorf("tạm khoá site trong lồng không tắt socket của lồng: %v", stepIDs(p.Plan))
		}
		ghepNoSharedPool(t, p.Plan.Steps)

		ghepCage(t, se, dom, func(s *site.Site) { s.Suspended = account.SuspendSelf })
		p, err = se.s.PrepareUnsuspend(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		ss := p.Plan.Steps
		f, k := ghepIdx(ss, isCageFiles(false)), ghepIdx(ss, isCageSocket(false))
		if f < 0 || k < f {
			t.Errorf("mở khoá site trong lồng phải ghi file của lồng rồi bật socket: %v", stepIDs(p.Plan))
		}
		ghepNoSharedPool(t, ss)
		if !slices.Contains(stepIDs(p.Plan), "site.unsuspend.check:"+dom) {
			t.Errorf("mở khoá mất bước kiểm 200: %v", stepIDs(p.Plan))
		}
		ghepVerifyCaged(t, se.m, p.Plan.Verify, dom)
	})

	t.Run("trash restore purge", func(t *testing.T) {
		be := newBktEnv(t, dom) // máy giả có du, df cho bước tự backup của xoá hẳn
		se := be.siteEnv
		se.s.Backups = be.bs
		ghepCageOn(t, se)
		ghepCage(t, se, dom, nil)
		d := mustDomain(t, dom)
		l := site.LayoutOf(d)

		// Đưa vào thùng rác: gỡ cron trước khi gỡ hàng rào.
		rm, err := se.s.PrepareRemove(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		ss := rm.Plan.Steps
		cron := slices.Index(stepIDs(rm.Plan), "absent:"+site.CronFile(l))
		sock, files := ghepIdx(ss, isCageSocket(true)), ghepIdx(ss, isCageFiles(true))
		if cron < 0 || sock < 0 || files < 0 || cron >= sock || sock >= files {
			t.Errorf("thùng rác của site trong lồng: muốn gỡ cron → tắt socket → gỡ file của lồng; có %v", stepIDs(rm.Plan))
		}
		if lock := ghepIdx(ss, func(s engine.Step) bool { _, ok := s.(*steps.SiteUserLock); return ok }); lock < files {
			t.Errorf("khoá user phải sau khi gỡ lồng: %v", stepIDs(rm.Plan))
		}
		ghepNoSharedPool(t, ss)
		for _, id := range stepIDs(rm.Plan) {
			if strings.HasPrefix(id, "absent:") && strings.Contains(id, "/fpm/pool.d/") {
				t.Errorf("site trong lồng không có pool chung để gỡ: %s", id)
			}
		}

		// Chạy thật lúc còn ở ngoài lồng (máy giả không mô phỏng systemd), rồi
		// đặt lại cờ caged cho dòng ở thùng rác để dựng Plan khôi phục.
		ghepCage(t, se, dom, func(s *site.Site) { s.Caged = false })
		se.trash(t, dom)
		ghepCage(t, se, dom, nil)
		rs, err := se.s.PrepareRestore(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		ss = rs.Plan.Steps
		unlock := ghepIdx(ss, func(s engine.Step) bool { u, ok := s.(*steps.SiteUserLock); return ok && u.Unlock })
		f := ghepIdx(ss, isCageFiles(false))
		if unlock < 0 || f != unlock+1 {
			t.Errorf("khôi phục: CageFiles phải đứng ngay sau mở khoá user; có %v", stepIDs(rs.Plan))
		}
		for i, s := range ss {
			if runsWP(s) && i < f {
				t.Errorf("khôi phục: bước chạy wp %s đứng trước hàng rào", s.Info().ID)
			}
		}
		k := ghepIdx(ss, isCageSocket(false))
		vh := slices.Index(stepIDs(rs.Plan), "symlink:"+l.NginxLink)
		dbu := ghepIdx(ss, func(s engine.Step) bool { _, ok := s.(*steps.DBUserUnlock); return ok })
		if k < 0 || dbu >= k || k >= vh {
			t.Errorf("khôi phục: muốn mở khoá DB → bật socket → vhost; có %v", stepIDs(rs.Plan))
		}
		ghepNoSharedPool(t, ss)
		ghepVerifyCaged(t, se.m, rs.Plan.Verify, dom)

		// Xoá hẳn: tắt và gỡ lồng trước khi xoá DB và xoá user.
		pg, err := se.s.PreparePurge(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		ids := stepIDs(pg.Plan)
		ss = pg.Plan.Steps
		sock, files = ghepIdx(ss, isCageSocket(true)), ghepIdx(ss, isCageFiles(true))
		dbAbs := slices.Index(ids, "mariadb.site-db-absent:"+dom)
		userAbs := slices.Index(ids, "site.user.absent:"+l.User)
		if sock < 0 || files < sock || dbAbs < files || userAbs < files {
			t.Errorf("xoá hẳn site trong lồng: muốn tắt socket → gỡ file của lồng trước khi xoá DB và user; có %v", ids)
		}
	})

	t.Run("php use", func(t *testing.T) {
		pe := newUseEnv(t)
		lim := ghepCageOn(t, pe.siteEnv)
		pe.p.Accounts = pe.st
		ghepCage(t, pe.siteEnv, useDom, nil)
		pl, err := pe.p.PrepareUse(ctx, mustDomain(t, useDom), "8.4")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"cage.files:" + useDom, "php.use.check:" + useDom}
		if got := stepIDs(pl.Plan); !slices.Equal(got, want) {
			t.Fatalf("php use của site trong lồng: có %v, muốn %v (không đụng vhost, không đụng pool chung)", got, want)
		}
		cf := pl.Plan.Steps[0].(*steps.CageFiles)
		if cf.PHP != "8.4" || cf.Limits != lim || cf.Remove {
			t.Errorf("CageFiles = %+v, muốn bản 8.4 và trần %+v", cf, lim)
		}
		ghepVerifyCaged(t, pe.m, pl.Plan.Verify, useDom)
	})

	t.Run("php install va harden", func(t *testing.T) {
		pe := newPHPEnv(t)
		pe.avail = append(pe.avail, "8.4")
		pe.from["8.4"] = "ubuntu"
		in := site.Site{Domain: mustDomain(t, dom), Status: site.Active, PHPVersion: "8.3", Caged: true}
		trashed := site.Site{Domain: mustDomain(t, "b.example.com"), Status: site.Trashed, PHPVersion: "8.3", Caged: true}
		out := site.Site{Domain: mustDomain(t, "c.example.com"), Status: site.Active, PHPVersion: "8.3"}
		for _, st := range []site.Site{in, trashed, out} {
			if err := pe.sites.SaveSite(ctx, st); err != nil {
				t.Fatal(err)
			}
		}
		pl, err := pe.p.PrepareInstall(ctx, "8.4")
		if err != nil {
			t.Fatal(err)
		}
		ghepRestartLast(t, pl.Plan, in.Layout())

		e := newM14Env(t, Nginx, dom, "c.example.com")
		ghepCage(t, e.siteEnv, dom, nil)
		h, err := e.security().PrepareHarden(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ghepRestartLast(t, h.Plan, in.Layout())
		// Không Isolated nào chạy dưới user của site trong lồng (bất biến 8).
		for _, id := range ghepCheckIDs(e.m, h.Plan.Verify) {
			if strings.HasPrefix(id, "isolation:"+dom+":") {
				t.Errorf("harden sinh %s từ site trong lồng", id)
			}
		}
	})

	t.Run("ssl issue", func(t *testing.T) {
		se := newSSLEnv(t, Nginx)
		se.add(t, dom)
		se.setEmail(t)
		ghepCageOn(t, se.siteEnv)
		se.ssl.Accounts = se.st
		ghepCage(t, se.siteEnv, dom, nil)
		iss, err := se.ssl.PrepareIssue(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		ghepNoSharedPool(t, iss.Plan.Steps)
		ghepVerifyCaged(t, se.m, iss.Plan.Verify, dom)
	})

	t.Run("backup restore", func(t *testing.T) {
		be := newBktEnv(t, dom)
		src := be.seedFull(t, dom, "20261101-000000", "từ backup", "db từ backup", ReasonManual)
		ghepCageOn(t, be.siteEnv)
		ghepCage(t, be.siteEnv, dom, nil)
		p, err := be.b.PrepareRestore(ctx, mustDomain(t, dom), src.ID)
		if err != nil {
			t.Fatal(err)
		}
		ghepNoSharedPool(t, p.Plan.Steps)
		ghepVerifyCaged(t, be.m, p.Plan.Verify, dom)
	})

	t.Run("apply prune", func(t *testing.T) {
		se := newApplyEnv(t, Nginx)
		se.add(t, dom)
		ghepCageOn(t, se)
		ghepCage(t, se, dom, nil)
		a, err := se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, "new.example.com"), Prune: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := actions(a.Items)[dom]; got != ApplyTrash {
			t.Fatalf("%s: %s, muốn trash", dom, got)
		}
		var unit *engine.Unit
		for i := range a.Batch.Units {
			if a.Batch.Units[i].Target == dom {
				unit = &a.Batch.Units[i]
			}
		}
		if unit == nil {
			t.Fatal("không có unit thùng rác của site trong lồng")
		}
		ids := stepIDs(engine.Plan{Steps: unit.Prepare})
		if !slices.Contains(ids, "cage.socket:"+dom) || !slices.Contains(ids, "cage.files:"+dom) {
			t.Errorf("unit thùng rác của site trong lồng thiếu bước của lồng: %v", ids)
		}
		ghepNoSharedPool(t, unit.Prepare)
	})

	t.Run("mo khoa user", func(t *testing.T) {
		ue := newUsrEnv(t)
		if err := ue.as.SavePackage(ctx, ghepPkg); err != nil {
			t.Fatal(err)
		}
		ue.site(t, dom, "bob", func(s *site.Site) { s.Suspended, s.Caged = account.SuspendByUser, true })
		p, err := ue.a.PrepareUnsuspendUser(ctx, "bob")
		if err != nil {
			t.Fatal(err)
		}
		ss := p.Plan.Steps
		if f, k := ghepIdx(ss, isCageFiles(false)), ghepIdx(ss, isCageSocket(false)); f < 0 || k < f {
			t.Errorf("mở khoá user: site trong lồng phải ghi file của lồng rồi bật socket; có %v", stepIDs(p.Plan))
		}
		ghepNoSharedPool(t, ss)
		ghepVerifyCaged(t, ue.m, p.Plan.Verify, dom)
	})
}

// ghepRestartLast: bước cuối của Plan là khởi động lại dịch vụ của đúng một
// lồng (site active trong lồng), chỉ khi nó đang chạy.
func ghepRestartLast(t *testing.T, p engine.Plan, l site.Layout) {
	t.Helper()
	var restarts []*steps.ServiceRestart
	for _, s := range p.Steps {
		if r, ok := s.(*steps.ServiceRestart); ok && strings.HasPrefix(r.Unit, "tpan-cage-") {
			restarts = append(restarts, r)
		}
	}
	last, ok := p.Steps[len(p.Steps)-1].(*steps.ServiceRestart)
	if len(restarts) != 1 || !ok || last.Unit != l.CageServiceUnit() || !last.OnlyIfActive {
		t.Errorf("%s: muốn đúng một bước cuối khởi động lại %s (OnlyIfActive); có %v", p.Kind, l.CageServiceUnit(), stepIDs(p))
	}
}

// Site ngoài lồng mà máy còn file của lồng (cage.enable dở dang): xoá hẳn vẫn
// dọn lồng. Không còn file nào: Plan y như trước M17 (TestSitePurge).
func TestPurgeDonLongDoDang(t *testing.T) {
	const dom = "a.example.com"
	se := newBktEnv(t, dom).siteEnv
	d := mustDomain(t, dom)
	se.trash(t, dom)
	pg, err := se.s.PreparePurge(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if ghepIdx(pg.Plan.Steps, isCageFiles(true)) >= 0 {
		t.Errorf("site chưa từng có lồng mà xoá hẳn có bước của lồng: %v", stepIDs(pg.Plan))
	}
	l := site.LayoutOf(d)
	se.m.SetFile(l.CageSliceFile(), []byte("[Slice]\n"), 0o644, system.Root)
	pg, err = se.s.PreparePurge(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if ghepIdx(pg.Plan.Steps, isCageSocket(true)) < 0 || ghepIdx(pg.Plan.Steps, isCageFiles(true)) < 0 {
		t.Errorf("máy còn file của lồng mà xoá hẳn không dọn: %v", stepIDs(pg.Plan))
	}
}

// ghepSamples ghi lại các lần DeleteUsageOf.
type ghepSamples struct {
	UsageStore
	deleted []string
}

func (g *ghepSamples) DeleteUsageOf(_ context.Context, d string) error {
	g.deleted = append(g.deleted, d)
	return nil
}

func TestPurgeXoaSoLieu(t *testing.T) {
	const dom = "a.example.com"
	be := newBktEnv(t, dom)
	se := be.siteEnv
	se.s.Backups = be.bs
	d := mustDomain(t, dom)
	se.trash(t, dom)
	sm := &ghepSamples{}
	se.s.Samples = sm
	pg, err := se.s.PreparePurge(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := pg.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá hẳn: %v %s", err, rep.Job.Error)
	}
	if !slices.Equal(sm.deleted, []string{dom}) {
		t.Errorf("DeleteUsageOf = %v, muốn [%s]", sm.deleted, dom)
	}
}

// Lần tạo đầu với gói muốn lồng chạy thêm cage.enable (job hay lời từ chối,
// máy giả không có cgroup v2 nên thường là lời từ chối); gói không muốn hoặc
// chạy lại thì không. Phần lồng hỏng không làm site add hỏng.
func TestSiteAddChayCageEnable(t *testing.T) {
	const dom = "a.example.com"
	se := newSiteEnv(t, Nginx)
	se.s.Accounts = se.st
	res := se.add(t, dom)
	if res.CageJob != nil || res.CageError != nil {
		t.Errorf("gói không muốn lồng mà có cage.enable: %+v %+v", res.CageJob, res.CageError)
	}

	ghepCageOn(t, se)
	res = se.add(t, "b.example.com")
	if res.CageJob == nil && res.CageError == nil {
		t.Error("gói muốn lồng mà lần tạo đầu không chạy cage.enable")
	}
	// Máy giả không có cgroup v2: lời từ chối của chính PrepareEnable, không
	// phải một lỗi khác bị gói thành cage_error.
	if res.CageError != nil && res.CageError.Code != CodeCageCgroupV1 {
		t.Errorf("CageError = %s, muốn %s", res.CageError, CodeCageCgroupV1)
	}
	if _, ok, _ := se.sites.Site(ctx, mustDomain(t, "b.example.com")); !ok {
		t.Error("site phải được tạo dù phần lồng hỏng")
	}
	res = se.add(t, dom) // chạy lại
	if res.CageJob != nil || res.CageError != nil {
		t.Errorf("chạy lại site add mà có cage.enable: %+v %+v", res.CageJob, res.CageError)
	}
}

func TestApplyChayCageEnable(t *testing.T) {
	se := newApplyEnv(t, Nginx)
	ghepCageOn(t, se)
	a, err := se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, "a.example.com")})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 1 || res.Created[0].Status != engine.UnitSucceeded {
		t.Fatalf("Created = %+v", res.Created)
	}
	if len(res.Cage) == 0 && res.Created[0].CageError == nil {
		t.Error("apply với gói muốn lồng không chạy cage.enable cho site mới")
	}
	if re := res.Created[0].CageError; re != nil && re.Code != CodeCageCgroupV1 {
		t.Errorf("CageError = %s, muốn %s", re, CodeCageCgroupV1)
	}
}

// Apply đi qua PrepareAdd nên tên miền của phép thử bị từ chối domain_reserved.
func TestApplyTuChoiTenPhepThu(t *testing.T) {
	se := newApplyEnv(t, Nginx)
	const probe = "tpan-probe-a-0123456789ab.test"
	if !site.IsProbeDomain(probe) {
		t.Fatalf("%s không phải tên của phép thử", probe)
	}
	a, err := se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, probe)})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 1 || a.Items[0].Action != ApplyRefused || a.Items[0].Code != CodeDomainReserved {
		t.Errorf("Items = %+v, muốn refused %s", a.Items, CodeDomainReserved)
	}
}

// ghepRuns là sổ phép thử chỉ có PendingProbeRuns.
type ghepRuns struct {
	ProbeStore
	pending []cage.ProbeRun
}

func (g ghepRuns) PendingProbeRuns(context.Context) ([]cage.ProbeRun, error) { return g.pending, nil }

func TestRollbackTuChoiKhiConLong(t *testing.T) {
	setup := func(t *testing.T) (*SelfUpdate, *system.FakeSystem) {
		s, f := newSelfEnv(t, "v1.1.0")
		f.SetFile(tpanOld, []byte("tpan v0.9.0"), 0o755, system.Root)
		f.SetDir(cageUnitDir, 0o755, system.Root)
		return s, f
	}
	cases := []struct {
		name string
		mod  func(t *testing.T, s *SelfUpdate, f *system.FakeSystem)
		want []string // chuỗi phải có trong lời từ chối; nil là không từ chối
	}{
		{"sach", func(*testing.T, *SelfUpdate, *system.FakeSystem) {}, nil},
		{"site caged (ca o thung rac)", func(t *testing.T, s *SelfUpdate, _ *system.FakeSystem) {
			ms := newMemSites()
			_ = ms.SaveSite(ctx, site.Site{Domain: mustDomain(t, "a.example.com"), Status: site.Trashed, Caged: true})
			_ = ms.SaveSite(ctx, site.Site{Domain: mustDomain(t, "b.example.com"), Status: site.Active})
			s.Sites = ms
		}, []string{"a.example.com", "sudo tpan cage disable --all --yes"}},
		{"file tpan-cage-*", func(_ *testing.T, _ *SelfUpdate, f *system.FakeSystem) {
			f.SetFile(cageUnitDir+"/tpan-cage-a_example_com_abc123-php.service", []byte("x"), 0o644, system.Root)
		}, []string{"tpan-cage-a_example_com_abc123-php.service", "sudo tpan cage disable --all --yes"}},
		{"luot thu chua don", func(_ *testing.T, s *SelfUpdate, _ *system.FakeSystem) {
			s.Runs = ghepRuns{pending: []cage.ProbeRun{{ID: "0123456789abcdef"}}}
		}, []string{"0123456789abcdef", "sudo tpan isolation probe --cleanup --yes"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, f := setup(t)
			c.mod(t, s, f)
			_, err := s.PrepareRollback(ctx)
			if c.want == nil {
				if err != nil {
					t.Fatalf("máy sạch mà bị từ chối: %v", err)
				}
				return
			}
			if refusedCode(t, err) != CodeCageActive {
				t.Fatalf("lỗi = %v, muốn %s", err, CodeCageActive)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("lời từ chối thiếu %q: %v", w, err)
				}
			}
		})
	}
}
