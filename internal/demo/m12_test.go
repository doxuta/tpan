package demo

import (
	"context"
	"slices"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

func demoPHP(sys system.System, st *store.Store) *core.PHP {
	return &core.PHP{Sys: sys, Settings: st, Sites: st, Journal: st}
}

func demoUpdates(sys system.System, st *store.Store) *core.Updates {
	return &core.Updates{Sys: sys, Settings: st, Sites: st, Journal: st, Version: "v0.12.0-demo", WPCLISHA512: wpCLISHA512()}
}

// runOK(t, ctx, name)(Prepare...) xem trước rồi chạy Plan, đòi job thành
// công; trả bản xem trước. Hai lớp hàm vì Go chỉ nhận f(g()) khi g() là đối
// số duy nhất.
func runOK(t *testing.T, ctx context.Context, name string) func(*core.Planned, error) engine.Preview {
	return func(p *core.Planned, err error) engine.Preview {
		t.Helper()
		return runPlanned(t, ctx, name, p, err)
	}
}

func runPlanned(t *testing.T, ctx context.Context, name string, p *core.Planned, err error) engine.Preview {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: chuẩn bị: %v", name, err)
	}
	pv, err := p.Preview(ctx)
	if err != nil {
		t.Fatalf("%s: xem trước: %v", name, err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		var bad []string
		for _, c := range rep.Checks {
			if c.Status == checks.Fail {
				bad = append(bad, c.ID+": "+c.Summary.Vi)
			}
		}
		t.Fatalf("%s: job %s: %s, %v; kiểm chứng hỏng: %v", name, rep.Job.Status, rep.Job.Error, err, bad)
	}
	return pv
}

// demoHealth chạy health đầy đủ như bộ hẹn giờ của `tpan serve --demo` và trả
// các phép kiểm Fail (sắp theo id) và Warn/Unknown.
func demoHealth(t *testing.T, ctx context.Context, sys system.System, st *store.Store) (failed, warned []string) {
	t.Helper()
	_, wpcli := Hashes()
	rep, err := (&health.Runner{Sys: sys, Store: st, WPCLISHA512: wpcli, NoSelf: true}).Run(ctx, health.Options{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range rep.Groups {
		for _, x := range g.Results {
			switch x.Status {
			case checks.Fail:
				failed = append(failed, x.ID)
			case checks.Warn, checks.Unknown:
				if demoExpectedWarn(x) {
					continue
				}
				warned = append(warned, string(x.Status)+" "+x.ID+": "+x.Summary.Vi)
			}
		}
	}
	slices.Sort(failed)
	return failed, warned
}

// wikiOnly: chỉ được đỏ ở đúng site hỏng sẵn của demo (hoặc không đỏ gì, khi
// một job đã khởi động lại PHP-FPM 8.3 và chữa nó).
func wikiOnly(t *testing.T, when string, failed, warned []string) {
	t.Helper()
	wiki := []string{"http.site:" + siteWiki, "php.socket:" + siteWiki}
	if len(failed) > 0 && !slices.Equal(failed, wiki) {
		t.Errorf("%s: Fail = %v; muốn chỉ %v", when, failed, wiki)
	}
	if len(warned) > 0 {
		t.Errorf("%s: cảnh báo hoặc chưa kiểm được: %v", when, warned)
	}
}

func TestPHPListOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	list, err := demoPHP(sys, st).List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var vs []string
	for _, r := range list.Versions {
		vs = append(vs, r.Version)
	}
	if !slices.Equal(vs, []string{"8.1", "8.2", "8.3", "8.4", "8.5"}) {
		t.Fatalf("bản = %v", vs)
	}
	row := func(v string) core.PHPVersionRow { return list.Versions[slices.Index(vs, v)] }
	// siteOld (M6) nằm trong thùng rác nhưng vẫn tính là đang dùng 8.3: gỡ bản
	// đó thì khôi phục site sẽ không còn PHP để chạy.
	if r := row("8.3"); !r.Installed || !r.Default || r.Source != core.SourceUbuntu || !slices.Equal(r.Sites, []string{siteBlog, siteLab, siteOld, siteShop, siteWiki}) ||
		r.Support == nil || r.Support.State != "security" || r.Support.SecurityUntil != "2027-12-31" {
		t.Errorf("8.3 = %+v (support %+v)", r, r.Support)
	}
	if r := row("8.4"); r.Installed || r.Default || r.Source != core.SourceSury || len(r.Sites) != 0 || r.Support == nil || r.Support.State != "stable" {
		t.Errorf("8.4 = %+v", r)
	}
	if r := row("8.1"); r.Support == nil || r.Support.State != "eol" {
		t.Errorf("8.1 cũ hơn mọi nhánh php.net liệt kê, muốn eol: %+v", r.Support)
	}
	if !list.SuryEnabled || !list.NotesOK || list.WPRecommended != "8.3" {
		t.Errorf("SuryEnabled=%v NotesOK=%v WPRecommended=%q", list.SuryEnabled, list.NotesOK, list.WPRecommended)
	}
}

// Kịch bản của màn Stack và trang site trên demo: cài PHP 8.4 từ kho sury,
// chuyển một site sang 8.4 rồi về, gỡ 8.4. Mỗi bước là một job thật trên máy giả.
func TestPHPJobsOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	p := demoPHP(sys, st)

	pv := runOK(t, ctx, "php.install 8.4")(p.PrepareInstall(ctx, "8.4"))
	// Kho sury đã có nhưng ghim chưa có 8.4: bước kho (Danger) phải chạy, không bỏ qua.
	i := slices.IndexFunc(pv.Steps, func(s engine.StepPreview) bool { return s.Info.ID == "apt.repo:sury" })
	if i < 0 || pv.Steps[i].Done || !pv.Steps[i].Info.Danger {
		t.Errorf("bước kho sury trong bản xem trước: %+v", pv.Steps)
	}
	list, _ := p.List(ctx, false)
	if r := list.Versions[slices.IndexFunc(list.Versions, func(r core.PHPVersionRow) bool { return r.Version == "8.4" })]; !r.Installed {
		t.Fatalf("sau khi cài, 8.4 = %+v", r)
	}
	// Chạy lại: không còn gì để làm.
	pv = runOK(t, ctx, "php.install 8.4 lần hai")(p.PrepareInstall(ctx, "8.4"))
	for _, s := range pv.Steps {
		if !s.Done {
			t.Errorf("chạy lại php.install mà bước %s chưa xong", s.Info.ID)
		}
	}

	// Site không SSL và site có SSL đều chuyển được, rồi về lại.
	for _, dom := range []string{siteShop, siteBlog} {
		d := domain(t, dom)
		for _, v := range []string{"8.4", "8.3"} {
			runOK(t, ctx, "php.use "+dom+" "+v)(p.PrepareUse(ctx, d, v))
			s, _, _ := st.Site(ctx, d)
			if s.PHPVersion != v {
				t.Fatalf("%s: bảng sites ghi PHP %q, muốn %s", dom, s.PHPVersion, v)
			}
			h, err := NewSites(sys, st).Health(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			if bad := notPass(h.Results); len(bad) > 0 {
				t.Errorf("%s trên PHP %s: %v", dom, v, bad)
			}
		}
	}
	f, w := demoHealth(t, ctx, sys, st)
	wikiOnly(t, "sau khi đổi bản PHP", f, w)

	runOK(t, ctx, "php.remove 8.4")(p.PrepareRemove(ctx, "8.4"))
	list, _ = p.List(ctx, false)
	if r := list.Versions[slices.IndexFunc(list.Versions, func(r core.PHPVersionRow) bool { return r.Version == "8.4" })]; r.Installed {
		t.Errorf("sau khi gỡ, 8.4 vẫn cài: %+v", r)
	}
	f, w = demoHealth(t, ctx, sys, st)
	wikiOnly(t, "sau khi gỡ 8.4", f, w)
}

func TestUpdatesOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	u := demoUpdates(sys, st)
	s, err := u.Status(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range s.Packages {
		names = append(names, p.Name)
		if p.Security != (p.Name == "curl" || p.Name == "libcurl4t64") {
			t.Errorf("%s: Security = %v", p.Name, p.Security)
		}
	}
	want := []string{"curl", "libcurl4t64", "nginx", "nginx-common", "php8.3-cli", "php8.3-common", "php8.3-fpm", "php8.3-mysql"}
	if !slices.Equal(names, want) {
		t.Errorf("gói của stack = %v; muốn %v", names, want)
	}
	if s.Others != 3 || s.OthersSecurity != 0 {
		t.Errorf("gói khác = %d (%d bảo mật)", s.Others, s.OthersSecurity)
	}
	if s.Auto.Status != checks.Pass || !slices.Equal(s.AutoOrigins, []string{core.OriginSury}) {
		t.Errorf("Auto = %s %q, AutoOrigins = %v", s.Auto.Status, s.Auto.Summary.Vi, s.AutoOrigins)
	}
	var lines []string
	for _, l := range s.MariaDB.Upgrades {
		lines = append(lines, l.Line)
	}
	if s.MariaDB.Line != "10.11" || s.MariaDB.Source != core.SourceUbuntu || !slices.Equal(lines, []string{"11.4", "11.8", "12.3"}) {
		t.Errorf("MariaDB = %+v", s.MariaDB)
	}
	if s.Self.Version != "v0.12.0-demo" || s.Self.URL != core.DefaultSelfURL || !s.Self.HasOld {
		t.Errorf("Self = %+v", s.Self)
	}

	// Bật lại bản vá tự động (đã bật): job chạy được, vẫn đạt.
	runOK(t, ctx, "update.auto")(u.PrepareAuto(ctx))
}

func TestUpdateApplyOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	u := demoUpdates(sys, st)
	p, err := u.PrepareApply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pv, err := p.Preview(ctx)
	if err != nil || len(pv.Steps) != 4 { // apt-get update, nâng gói, khởi động lại nginx và php8.3-fpm
		t.Fatalf("bản xem trước update.apply: %+v, %v", pv.Steps, err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Gói đã nâng (nâng gói không hoàn tác) và PHP-FPM 8.3 đã khởi động lại,
	// nên dù job thế nào: không còn gói của stack chờ, và khởi động lại chữa
	// luôn pool hỏng của wiki (health không còn Fail nào).
	if s, _ := u.Status(ctx, false); len(s.Packages) != 0 || s.Others != 3 {
		t.Errorf("sau khi cập nhật còn %v, %d gói khác", s.Packages, s.Others)
	}
	if _, err := u.PrepareApply(ctx); err == nil {
		t.Error("hết gói chờ mà PrepareApply không từ chối nothing_to_update")
	}
	f, w := demoHealth(t, ctx, sys, st)
	wikiOnly(t, "sau update.apply", f, w)
	if len(f) != 0 {
		t.Errorf("khởi động lại php8.3-fpm phải chữa wiki, còn Fail: %v", f)
	}

	if rep.Job.Status != engine.JobSucceeded {
		var bad []string
		for _, c := range rep.Checks {
			if c.Status == checks.Fail {
				bad = append(bad, c.ID)
			}
		}
		t.Fatalf("update.apply: job %s: %s; kiểm chứng hỏng: %v", rep.Job.Status, rep.Job.Error, bad)
	}
}

func TestMariaDBUpgradeOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	u := demoUpdates(sys, st)
	pv := runOK(t, ctx, "mariadb.upgrade 11.8")(u.PrepareMariaDB(ctx, "11.8"))
	if pv.Kind != "mariadb.upgrade" || len(pv.Steps) != 7 {
		t.Errorf("bản xem trước: %+v", pv)
	}
	s, err := u.Status(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if s.MariaDB.Line != "11.8" || s.MariaDB.Source != core.SourceMariaDB || len(s.MariaDB.Upgrades) != 1 || s.MariaDB.Upgrades[0].Line != "12.3" {
		t.Errorf("sau khi nâng: %+v", s.MariaDB)
	}
	if v, _, _ := st.GetSetting(ctx, core.SettingDBLine); v != "11.8" {
		t.Errorf("settings %s = %q", core.SettingDBLine, v)
	}
	// Kho MariaDB mới thêm chưa nằm trong danh sách tự vá cho tới khi chạy lại
	// update.auto; màn Cập nhật phải nói đúng như vậy.
	if slices.Contains(s.AutoOrigins, core.OriginMariaDB) {
		t.Errorf("kho MariaDB chưa qua update.auto mà đã báo được tự vá: %v", s.AutoOrigins)
	}
	runOK(t, ctx, "update.auto")(u.PrepareAuto(ctx))
	if s, _ := u.Status(ctx, false); !slices.Contains(s.AutoOrigins, core.OriginMariaDB) {
		t.Errorf("sau update.auto kho MariaDB vẫn chưa được tự vá: %v", s.AutoOrigins)
	}
	// Không hạ xuống.
	if _, err := u.PrepareMariaDB(ctx, "11.4"); err == nil {
		t.Error("nâng xuống 11.4 mà không bị từ chối")
	}
	f, w := demoHealth(t, ctx, sys, st)
	wikiOnly(t, "sau mariadb.upgrade", f, w)
}

// self-update chỉ có ở CLI, nhưng màn Cập nhật đọc phần của nó, và bản xem
// trước phải dựng được trên máy giả.
func TestSelfUpdatePreviewOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	su := &core.SelfUpdate{Sys: sys, Settings: st, Arch: "amd64", Version: "v0.12.0-demo"}
	p, err := su.Prepare(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Preview(ctx); err != nil {
		t.Fatal(err)
	}
}
