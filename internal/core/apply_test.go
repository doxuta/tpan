package core

import (
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// applyNow là giờ cố định của Sites.Now trong test apply: Created của site mới
// phải lấy từ s.now(), không từ time.Now.
var applyNow = time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)

func newApplyEnv(t *testing.T, web WebServer) *siteEnv {
	t.Helper()
	se := newSiteEnv(t, web)
	se.s.Now = func() time.Time { return applyNow }
	return se
}

// applyList dựng đầu vào có email cho các domain.
func (se *siteEnv) applyList(t *testing.T, doms ...string) []ApplySite {
	t.Helper()
	out := make([]ApplySite, 0, len(doms))
	for _, d := range doms {
		out = append(out, ApplySite{Input: se.input(t, d)})
	}
	return out
}

// countRan đếm số lần lệnh argv đúng bằng want đã chạy.
func countRan(m *system.FakeSystem, want ...string) int {
	n := 0
	for _, c := range m.Ran() {
		if slices.Equal(append([]string{c.Name}, c.Args...), want) {
			n++
		}
	}
	return n
}

func actions(items []ApplyItem) map[string]ApplyAction {
	out := map[string]ApplyAction{}
	for _, it := range items {
		out[it.Domain] = it.Action
	}
	return out
}

// applyRun chuẩn bị rồi chạy, đòi job hàng loạt có báo cáo.
func (se *siteEnv) applyRun(t *testing.T, in ApplyInput) ApplyResult {
	t.Helper()
	a, err := se.s.PrepareApply(ctx, in)
	if err != nil {
		t.Fatalf("PrepareApply: %v", err)
	}
	res, err := a.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Report == nil {
		t.Fatal("Execute không có báo cáo job")
	}
	return res
}

func TestApplyCreatesInOneJobOneReload(t *testing.T) {
	doms := []string{"a.example.com", "b.example.com", "c.example.com"}
	for _, tc := range []struct {
		web    WebServer
		reload string
	}{{Nginx, "nginx"}, {Apache, "apache2"}} {
		t.Run(string(tc.web), func(t *testing.T) {
			se := newApplyEnv(t, tc.web)
			// Một site đã có từ trước: site mới phải kiểm cô lập với nó.
			se.add(t, "old.example.com")

			a, err := se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, append(doms, "old.example.com")...)})
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]ApplyAction{"a.example.com": ApplyCreate, "b.example.com": ApplyCreate, "c.example.com": ApplyCreate, "old.example.com": ApplySkip}
			if got := actions(a.Items); !reflect.DeepEqual(got, want) {
				t.Fatalf("Items = %v", got)
			}
			if a.Empty() || len(a.Batch.Units) != 3 || len(a.Batch.Pre) != 1 || a.Batch.Parallel != 3 {
				t.Fatalf("Batch: %d unit, %d Pre, Parallel %d", len(a.Batch.Units), len(a.Batch.Pre), a.Batch.Parallel)
			}
			for _, u := range a.Batch.Units {
				if len(u.Finish) != 1 || u.Finish[0].Info().ID != "wp.install:"+u.Target {
					t.Errorf("unit %s: Finish phải đúng một bước wp.install", u.Target)
				}
				cron := false
				for _, st := range u.Prepare {
					if _, ok := st.(*steps.WPArchive); ok {
						t.Errorf("unit %s còn WPArchive ở pha A", u.Target)
					}
					cron = cron || st.Info().ID == "wp.constant:"+u.Target+":DISABLE_WP_CRON"
				}
				// M9: unit dựng từ addPlan nên có cron của site ở pha A.
				if !cron {
					t.Errorf("unit %s thiếu bước DISABLE_WP_CRON ở pha A", u.Target)
				}
			}

			before := se.m.Snapshot()
			pv, err := a.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if pv.Preview == nil || len(pv.Items) != 4 {
				t.Fatalf("Preview: %+v", pv)
			}
			if !reflect.DeepEqual(before, se.m.Snapshot()) {
				t.Error("xem trước đổi hệ thống file")
			}

			se.m.ResetRan()
			res, err := a.Execute(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			rep := res.Report
			if rep == nil || rep.Job.Status != engine.JobSucceeded || rep.Job.Kind != "apply" {
				t.Fatalf("job: %+v", rep)
			}
			for _, u := range rep.Units {
				if u.Status != engine.UnitSucceeded {
					t.Errorf("unit %s: %s ở %s (%s); kiểm %s", u.Target, u.Status, u.Phase, u.Error, failedChecks(u.Checks))
				}
			}
			// Mục 13 của M6: tạo nhiều site một lượt chỉ reload một lần.
			if n := countRan(se.m.FakeSystem, "systemctl", "reload", tc.reload); n != 1 {
				t.Errorf("reload %s chạy %d lần; muốn 1", tc.reload, n)
			}
			if n := countRan(se.m.FakeSystem, "systemctl", "reload", "php8.3-fpm"); n != 1 {
				t.Errorf("reload php8.3-fpm chạy %d lần; muốn 1", n)
			}

			if len(res.Created) != 3 {
				t.Fatalf("Created = %d dòng", len(res.Created))
			}
			seen := map[string]bool{}
			for i, c := range res.Created {
				pw := c.AdminPassword.Reveal()
				if c.Domain != doms[i] || c.Status != engine.UnitSucceeded || !c.WPInstalled || c.AdminUser != "admin" || len(pw) != 32 || seen[pw] {
					t.Errorf("Created[%d] = %+v (độ dài mật khẩu %d)", i, c, len(pw))
				}
				seen[pw] = true
				saved, ok, _ := se.sites.Site(ctx, mustDomain(t, c.Domain))
				if !ok || saved.Status != site.Active || saved.PHPVersion != "8.3" || !saved.Created.Equal(applyNow) {
					t.Errorf("Store %s: ok=%v %+v", c.Domain, ok, saved)
				}
				if !se.exists(site.CronFile(site.LayoutOf(mustDomain(t, c.Domain)))) {
					t.Errorf("site %s tạo trong lô mà không có file cron", c.Domain)
				}
			}
			// 3 site mới: 6 cặp hai chiều.
			if len(res.Isolation) != 6 {
				t.Errorf("Isolation = %d phép kiểm; muốn 6", len(res.Isolation))
			}
			for _, r := range res.Isolation {
				if r.Status != checks.Pass {
					t.Errorf("cô lập %s: %s", r.ID, r.Status)
				}
			}
			// Verify của site mới có kiểm cô lập với site cũ.
			found := false
			for _, r := range rep.Checks {
				found = found || r.ID == "isolation:a.example.com:old.example.com"
			}
			if !found {
				t.Error("Verify của site mới không kiểm cô lập với site đã có")
			}
		})
	}
}

func TestApplyOneFailsOthersStay(t *testing.T) {
	se := newApplyEnv(t, Nginx)
	doms := []string{"a.example.com", "bad.example.com", "c.example.com"}
	bad := site.LayoutOf(mustDomain(t, "bad.example.com"))
	// Một thư mục nằm ở chỗ file vhost của riêng bad: pha A của nó lỗi.
	se.m.SetDir(bad.NginxConf, 0o755, system.Root)

	res := se.applyRun(t, ApplyInput{Sites: se.applyList(t, doms...)})
	if res.Report.Job.Status != engine.JobSucceeded || res.Report.Job.Error == "" {
		t.Fatalf("job = %s, Error %q; muốn succeeded kèm lỗi của bad", res.Report.Job.Status, res.Report.Job.Error)
	}
	for _, u := range res.Report.Units {
		want := engine.UnitSucceeded
		if u.Target == "bad.example.com" {
			want = engine.UnitRolledBack
			if u.Phase != "prepare" {
				t.Errorf("bad lỗi ở pha %q; muốn prepare", u.Phase)
			}
		}
		if u.Status != want {
			t.Errorf("unit %s: %s (%s)", u.Target, u.Status, u.Error)
		}
	}
	if se.sites.count() != 2 {
		t.Errorf("Store có %d site; muốn 2", se.sites.count())
	}
	if _, ok, _ := se.sites.Site(ctx, bad.Domain); ok {
		t.Error("site lỗi vẫn vào Store")
	}
	for _, c := range res.Created {
		if c.Domain == "bad.example.com" && (c.WPInstalled || c.AdminPassword.Reveal() != "") {
			t.Error("site lỗi mà trả mật khẩu quản trị")
		}
	}
	if _, ok := se.m.users[bad.User]; ok || se.m.dbs[bad.DB] || se.m.dbUsers[bad.DB] {
		t.Error("hoàn tác bad chưa sạch: còn user hoặc DB")
	}
	if _, err := se.m.Stat(bad.Dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("hoàn tác bad còn để lại %s", bad.Dir)
	}
	if len(res.Isolation) != 2 {
		t.Errorf("Isolation = %d; muốn 2 (một cặp hai chiều)", len(res.Isolation))
	}

	// Gỡ chỗ chặn rồi chạy lại cùng danh sách: bad được tạo (hoàn tác sạch nên
	// không bị coi là thứ ngoài tpan), hai site kia bỏ qua.
	if err := se.m.Remove(bad.NginxConf); err != nil {
		t.Fatal(err)
	}
	a, err := se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, doms...)})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ApplyAction{"a.example.com": ApplySkip, "bad.example.com": ApplyCreate, "c.example.com": ApplySkip}
	if got := actions(a.Items); !reflect.DeepEqual(got, want) {
		t.Errorf("chạy lại: Items = %v", got)
	}
}

func TestApplyRerunIsEmpty(t *testing.T) {
	se := newApplyEnv(t, Nginx)
	in := ApplyInput{Sites: se.applyList(t, "a.example.com", "b.example.com")}
	se.applyRun(t, in)

	a, err := se.s.PrepareApply(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Empty() {
		t.Fatalf("chạy lại mà không Empty: %+v", a.Items)
	}
	for _, it := range a.Items {
		if it.Action != ApplySkip {
			t.Errorf("%s: %s; muốn skip", it.Domain, it.Action)
		}
	}
	pv, err := a.Preview(ctx)
	if err != nil || pv.Preview != nil || len(pv.Items) != 2 {
		t.Errorf("Preview khi Empty: %+v, %v", pv, err)
	}
	se.m.ResetRan()
	res, err := a.Execute(ctx, nil)
	if err != nil || res.Report != nil || len(res.Created) != 0 || res.Created == nil {
		t.Errorf("Execute khi Empty: %+v, %v", res, err)
	}
	if ran := se.m.Ran(); len(ran) != 0 {
		t.Errorf("Empty mà vẫn chạy lệnh: %v", ran)
	}
	jobs, err := se.st.Jobs(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Errorf("Empty mà tạo job: %d job, %v", len(jobs), err)
	}
}

func TestApplyRefusals(t *testing.T) {
	se := newApplyEnv(t, Nginx)
	trashed := mustDomain(t, "trashed.example.com")
	if err := se.sites.SaveSite(ctx, site.Site{Domain: trashed, Status: site.Trashed, PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}
	// Máy đã có user Linux của domain mà tpan chưa biết.
	se.m.users[site.LayoutOf(mustDomain(t, "foreign.example.com")).User] = "/var/www/foreign.example.com"

	in := se.applyList(t, "trashed.example.com", "foreign.example.com", "ok.example.com", "ok.example.com")
	noMail := se.input(t, "nomail.example.com")
	noMail.Email = validate.Email{}
	in = append(in, ApplySite{Input: noMail})

	a, err := se.s.PrepareApply(ctx, ApplyInput{Sites: in})
	if err != nil {
		t.Fatal(err)
	}
	want := []ApplyItem{
		{Domain: "trashed.example.com", Action: ApplyRefused, Code: "site_trashed"},
		{Domain: "foreign.example.com", Action: ApplyRefused, Code: "site_exists_unmanaged"},
		{Domain: "ok.example.com", Action: ApplyCreate},
		{Domain: "ok.example.com", Action: ApplyRefused, Code: "domain_duplicate"},
		{Domain: "nomail.example.com", Action: ApplyRefused, Code: "email_invalid"},
	}
	if len(a.Items) != len(want) {
		t.Fatalf("Items = %+v", a.Items)
	}
	for i, w := range want {
		got := a.Items[i]
		if got.Domain != w.Domain || got.Action != w.Action || got.Code != w.Code {
			t.Errorf("Items[%d] = %s %s %s; muốn %s %s %s", i, got.Domain, got.Action, got.Code, w.Domain, w.Action, w.Code)
		}
		if got.Action == ApplyRefused && got.Reason.Vi == "" {
			t.Errorf("Items[%d] từ chối mà không có lý do", i)
		}
	}
	if len(a.Batch.Units) != 1 {
		t.Errorf("Batch có %d unit; muốn 1 (ok.example.com)", len(a.Batch.Units))
	}

	// Mọi dòng bị từ chối: Empty, không job.
	a, err = se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, "trashed.example.com")})
	if err != nil || !a.Empty() {
		t.Errorf("chỉ có site trong thùng rác: Empty=%v, %v", a != nil && a.Empty(), err)
	}

	// Từ chối cả lô.
	if _, err := se.s.PrepareApply(ctx, ApplyInput{}); refusedCode(t, err) != "apply_empty" {
		t.Errorf("danh sách rỗng: %v", err)
	}
	if _, err := se.s.PrepareApply(ctx, ApplyInput{Prune: true}); refusedCode(t, err) != "prune_empty" {
		t.Errorf("prune danh sách rỗng: %v", err)
	}
	noStack := newApplyEnv(t, "")
	if _, err := noStack.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, "a.example.com")}); refusedCode(t, err) != "stack_not_installed" {
		t.Errorf("stack chưa cài: %v", err)
	}
}

func TestApplyPrune(t *testing.T) {
	se := newApplyEnv(t, Nginx)
	se.applyRun(t, ApplyInput{Sites: se.applyList(t, "a.example.com", "b.example.com")})

	// Không --prune: site ngoài danh sách được giữ nguyên.
	a, err := se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, "a.example.com")})
	if err != nil || !a.Empty() || len(a.Items) != 1 {
		t.Fatalf("không prune: %+v, %v", a, err)
	}

	a, err = se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, "a.example.com", "n.example.com"), Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ApplyAction{"a.example.com": ApplySkip, "n.example.com": ApplyCreate, "b.example.com": ApplyTrash}
	if got := actions(a.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("Items = %v", got)
	}
	if len(a.Batch.Units) != 2 {
		t.Fatalf("Batch có %d unit", len(a.Batch.Units))
	}
	tu := a.Batch.Units[1]
	if tu.Target != "b.example.com" || len(tu.Finish) != 0 || len(tu.Prepare) == 0 {
		t.Errorf("unit thùng rác: %s, %d bước A, %d bước C", tu.Target, len(tu.Prepare), len(tu.Finish))
	}
	// Site mới không kiểm cô lập với site đang bị đưa vào thùng rác cùng lô.
	for _, c := range a.Batch.Units[0].Verify {
		if id := c.Run(ctx, system.NewFake()).ID; id == "isolation:n.example.com:b.example.com" {
			t.Error("Verify của site mới kiểm cô lập với site đang vào thùng rác")
		}
	}

	// Chỉ đưa vào thùng rác: không có Pre (không tải WordPress).
	a, err = se.s.PrepareApply(ctx, ApplyInput{Sites: se.applyList(t, "a.example.com"), Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Batch.Pre) != 0 || len(a.Batch.Units) != 1 {
		t.Fatalf("prune không tạo site: %d Pre, %d unit", len(a.Batch.Pre), len(a.Batch.Units))
	}
	se.m.ResetRan()
	res, err := a.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job prune: %s (%s); bước %+v", res.Report.Job.Status, res.Report.Job.Error, res.Report.Steps)
	}
	if n := countRan(se.m.FakeSystem, "systemctl", "reload", "nginx"); n != 1 {
		t.Errorf("reload nginx chạy %d lần; muốn 1", n)
	}
	b, _, _ := se.sites.Site(ctx, mustDomain(t, "b.example.com"))
	if b.Status != site.Trashed {
		t.Errorf("b sau prune: %s; muốn trashed", b.Status)
	}
	if aa, _, _ := se.sites.Site(ctx, mustDomain(t, "a.example.com")); aa.Status != site.Active {
		t.Errorf("a sau prune: %s; muốn active", aa.Status)
	}
	lb := site.LayoutOf(mustDomain(t, "b.example.com"))
	if _, err := se.m.Stat(lb.Dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("thư mục của b vẫn ở %s", lb.Dir)
	}
	if _, err := se.m.Stat(site.TrashDir(lb)); err != nil {
		t.Errorf("thư mục của b không ở thùng rác: %v", err)
	}
}

func TestApplySSLRefusalKeepsSite(t *testing.T) {
	// Pha D: SSL bị từ chối (máy giả không có IP public nên kiểm DNS không
	// qua) thì site vẫn được tạo, lời từ chối nằm ở dòng của site đó. ssl.email
	// chưa đặt thì lấy email quản trị của site, như `site add --ssl`.
	se := newApplyEnv(t, Nginx)
	in := se.applyList(t, "s.example.com", "p.example.com")
	in[0].SSL = true
	res := se.applyRun(t, ApplyInput{Sites: in})
	if res.Report.Job.Status != engine.JobSucceeded || len(res.Created) != 2 {
		t.Fatalf("job: %s, %d site", res.Report.Job.Status, len(res.Created))
	}
	s, p := res.Created[0], res.Created[1]
	if s.SSLError == nil || s.SSLError.Code != "precondition_failed" || len(res.SSL) != 0 {
		t.Errorf("SSL của s: lời từ chối %+v, %d job; muốn precondition_failed, không job", s.SSLError, len(res.SSL))
	}
	if p.SSLError != nil {
		t.Errorf("site không có ssl mà vẫn chạy pha D: %+v %+v", p.SSLError, res.SSL)
	}
	if st, ok, _ := se.sites.Site(ctx, mustDomain(t, "s.example.com")); !ok || st.Status != site.Active {
		t.Errorf("SSL lỗi làm hỏng site: ok=%v %+v", ok, st)
	}
	if v, _, _ := se.st.GetSetting(ctx, SettingSSLEmail); v != "admin@example.com" {
		t.Errorf("ssl.email = %q; muốn email quản trị của site", v)
	}
}

// ---------------------------------------------------------------- M13: phạm vi, chủ, hạn mức cả lô

func TestApplyScopeQuotaM13(t *testing.T) {
	ue := newUsrEnv(t)
	ue.as.pkgs["s3"] = account.Package{Name: "s3", MaxSites: 3}
	ue.as.put(account.User{Name: "bob", Role: account.RoleUser, Package: "s3"})
	ue.as.put(account.User{Name: "carol", Role: account.RoleUser})
	ue.site(t, "own.example.com", "bob", nil)
	ue.site(t, "old.example.com", "bob", nil)
	ue.site(t, "other.example.com", "carol", nil)
	ue.site(t, "root.example.com", "", nil)

	in := ue.applyList(t, "own.example.com", "other.example.com", "n1.example.com", "n2.example.com", "n3.example.com")
	in[4].Input.Owner = "carol"
	a, err := ue.s.PrepareApply(ctx, ApplyInput{Sites: in, Prune: true, Scope: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ApplyItem{
		{Domain: "own.example.com", Action: ApplySkip},
		{Domain: "other.example.com", Action: ApplyRefused, Code: "site_not_found"},
		{Domain: "n1.example.com", Action: ApplyCreate},
		// 2 site đã có + n1 trong lô = 3/3.
		{Domain: "n2.example.com", Action: ApplyRefused, Code: CodeQuotaSites},
		{Domain: "n3.example.com", Action: ApplyRefused, Code: CodeUserNotFound},
		// Prune chỉ chạm site của bob.
		{Domain: "old.example.com", Action: ApplyTrash},
	}
	if len(a.Items) != len(want) {
		t.Fatalf("Items = %+v", a.Items)
	}
	for i, w := range want {
		if g := a.Items[i]; g.Domain != w.Domain || g.Action != w.Action || g.Code != w.Code {
			t.Errorf("Items[%d] = %s %s %s; muốn %s %s %s", i, g.Domain, g.Action, g.Code, w.Domain, w.Action, w.Code)
		}
	}
	if a.units[0].add == nil || a.units[0].add.Site.Owner != "bob" {
		t.Errorf("site mới phải thuộc bob: %+v", a.units[0].add)
	}

	// Không phạm vi (admin): owner trong file được dùng, rỗng là admin.
	in = ue.applyList(t, "x.example.com", "y.example.com")
	in[0].Input.Owner = "carol"
	if a, err = ue.s.PrepareApply(ctx, ApplyInput{Sites: in}); err != nil {
		t.Fatal(err)
	}
	if len(a.units) != 2 || a.units[0].add.Site.Owner != "carol" || a.units[1].add.Site.Owner != account.AdminName {
		t.Errorf("chủ theo file: %+v", a.Items)
	}
}
