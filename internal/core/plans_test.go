package core

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- PlanStore giả

var (
	errPlanMissing    = errors.New("memplans: không có")
	errPlanNotPending = errors.New("memplans: không còn pending")
)

// memPlans là PlanStore trong bộ nhớ, cùng ngữ nghĩa với store thật:
// UpdatePlanPreview và DecidePlan chỉ khớp Plan pending, ReopenPlan chỉ mở lại
// Plan applied chưa có job.
type memPlans struct {
	mu    sync.Mutex
	plans map[string]engine.PendingPlan
}

func newMemPlans() *memPlans { return &memPlans{plans: map[string]engine.PendingPlan{}} }

func (m *memPlans) CreatePlan(_ context.Context, p engine.PendingPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.plans[p.ID]; ok {
		return fmt.Errorf("Plan %s trùng id", p.ID)
	}
	m.plans[p.ID] = p
	return nil
}

func (m *memPlans) Plan(_ context.Context, id string) (engine.PendingPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plans[id]
	if !ok {
		return engine.PendingPlan{}, fmt.Errorf("Plan %s: %w", id, errPlanMissing)
	}
	return p, nil
}

// edit gọi fn với Plan id khi want(Plan) đúng; không có thì errPlanMissing,
// want sai thì errPlanNotPending.
func (m *memPlans) edit(id string, want func(engine.PendingPlan) bool, fn func(*engine.PendingPlan)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plans[id]
	if !ok {
		return fmt.Errorf("Plan %s: %w", id, errPlanMissing)
	}
	if !want(p) {
		return fmt.Errorf("Plan %s đang %s: %w", id, p.Status, errPlanNotPending)
	}
	fn(&p)
	m.plans[id] = p
	return nil
}

func isPending(p engine.PendingPlan) bool { return p.Status == engine.PlanPending }

func (m *memPlans) UpdatePlanPreview(_ context.Context, id string, pv engine.Preview, fp string) error {
	return m.edit(id, isPending, func(p *engine.PendingPlan) { p.Preview, p.Fingerprint = pv, fp })
}

func (m *memPlans) DecidePlan(_ context.Context, id string, st engine.PlanStatus, by string, at time.Time) error {
	return m.edit(id, isPending, func(p *engine.PendingPlan) { p.Status, p.DecidedBy, p.Decided = st, by, at })
}

func (m *memPlans) ReopenPlan(_ context.Context, id string) error {
	return m.edit(id, func(p engine.PendingPlan) bool { return p.Status == engine.PlanApplied && p.JobID == "" },
		func(p *engine.PendingPlan) { p.Status, p.DecidedBy, p.Decided = engine.PlanPending, "", time.Time{} })
}

func (m *memPlans) SetPlanJob(_ context.Context, id, jobID string) error {
	return m.edit(id, func(p engine.PendingPlan) bool { return p.Status == engine.PlanApplied },
		func(p *engine.PendingPlan) { p.JobID = jobID })
}

func (m *memPlans) get(t *testing.T, id string) engine.PendingPlan {
	t.Helper()
	p, err := m.Plan(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ---------------------------------------------------------------- dựng môi trường

type planEnv struct {
	*siteEnv
	p     *Planner
	plans *memPlans
}

func newPlanEnv(t *testing.T, web WebServer) *planEnv {
	t.Helper()
	se := newSiteEnv(t, web)
	mp := newMemPlans()
	return &planEnv{siteEnv: se, plans: mp, p: &Planner{
		Sites: se.s, Stack: &Stack{Sys: se.m, Settings: se.st, Journal: se.st},
		Plans: mp, NotFound: errPlanMissing, NotPending: errPlanNotPending,
	}}
}

func addReq(dom string) PlanRequest {
	return PlanRequest{Kind: PlanSiteAdd, Domain: dom, Email: "admin@example.com"}
}

func (pe *planEnv) layout(t *testing.T, dom string) site.Layout {
	return site.LayoutOf(mustDomain(t, dom))
}

// stepPreview tìm bước id trong bản xem trước.
func stepPreview(t *testing.T, pv engine.Preview, id string) engine.StepPreview {
	t.Helper()
	for _, s := range pv.Steps {
		if s.Info.ID == id {
			return s
		}
	}
	t.Fatalf("bản xem trước không có bước %s", id)
	return engine.StepPreview{}
}

// planSecrets lấy mọi mật khẩu đã sinh trong Plan, đọc thẳng từ các Step.
func planSecrets(t *testing.T, p engine.Plan) []string {
	t.Helper()
	var out []string
	for _, s := range p.Steps {
		switch s := s.(type) {
		case *steps.SiteDB:
			out = append(out, s.Password.Reveal())
		case *steps.WPConfig:
			out = append(out, s.DBPassword.Reveal())
		case *wpInstallStep:
			out = append(out, s.AdminPassword.Reveal())
		case *repairInstallStep:
			out = append(out, s.AdminPassword.Reveal())
		}
	}
	if len(out) != 3 {
		t.Fatalf("muốn 3 chỗ mang mật khẩu (SiteDB, WPConfig, WPInstall), có %d", len(out))
	}
	return out
}

// planReadOnly: lệnh chỉ hỏi trạng thái máy khi dựng và xem trước Plan site
// (siteReadOnly cộng các câu hỏi của WP-CLI).
func planReadOnly(c system.Cmd) bool {
	if c.Name == "wp" {
		a := strings.Join(c.Args, " ")
		return strings.HasPrefix(a, "core is-installed") || strings.HasPrefix(a, "option get")
	}
	return siteReadOnly(c)
}

// ---------------------------------------------------------------- 1. dấu vân tay

func TestPlanFingerprintStableAndNoSecrets(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	var fps []string
	var secrets [][]string
	for range 2 {
		pr, err := pe.p.Prepare(ctx, addReq("shop.example.com"))
		if err != nil {
			t.Fatal(err)
		}
		pv, err := pr.Preview(ctx)
		if err != nil {
			t.Fatal(err)
		}
		fps = append(fps, engine.Fingerprint(pv))
		sec := planSecrets(t, pr.Plan)
		secrets = append(secrets, sec)
		// Không so JSON của bản xem trước: New/Old là []byte, JSON mã base64,
		// mật khẩu lọt vào đó sẽ không bị chuỗi con nào bắt.
		for _, s := range pv.Steps {
			for _, pw := range sec {
				for _, c := range s.Info.Commands {
					if strings.Contains(c, pw) {
						t.Errorf("bước %s: lệnh lộ mật khẩu: %s", s.Info.ID, c)
					}
				}
				for _, f := range s.Info.Files {
					if strings.Contains(string(f.New), pw) || strings.Contains(string(f.Old), pw) {
						t.Errorf("bước %s: file %s lộ mật khẩu", s.Info.ID, f.Path)
					}
				}
			}
		}
	}
	for i := range secrets[0] {
		if secrets[0][i] == secrets[1][i] {
			t.Fatalf("hai lần Prepare ra cùng mật khẩu %d: phép so dấu vân tay dưới đây vô nghĩa", i)
		}
	}
	if fps[0] != fps[1] {
		t.Errorf("hai lần Prepare cùng yêu cầu ra hai dấu vân tay khác nhau:\n%s\n%s", fps[0], fps[1])
	}
}

// ---------------------------------------------------------------- 2. đề xuất không đổi máy

func TestProposeChangesNothing(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	before := pe.m.Snapshot()
	pe.m.ResetRan()
	pp, err := pe.p.Propose(ctx, addReq("Shop.Example.com"), Proposal{Origin: engine.OriginMCP, SessionID: "s1", Untrusted: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range pe.m.Ran() {
		if !planReadOnly(c) {
			t.Errorf("đề xuất chạy lệnh thay đổi máy: %s", c.String())
		}
	}
	if !reflect.DeepEqual(before, pe.m.Snapshot()) {
		t.Error("đề xuất làm đổi hệ thống file")
	}
	if pe.sites.count() != 0 {
		t.Error("đề xuất ghi bảng sites")
	}

	got := pe.plans.get(t, pp.ID)
	if !reflect.DeepEqual(got, pp) {
		t.Errorf("dòng đã lưu khác dòng trả về:\n%+v\n%+v", got, pp)
	}
	if len(pp.ID) != 16 || strings.Trim(pp.ID, "0123456789abcdef") != "" {
		t.Errorf("id = %q; muốn 16 hex", pp.ID)
	}
	if pp.Status != engine.PlanPending || pp.Kind != PlanSiteAdd || pp.Target != "shop.example.com" ||
		pp.Origin != engine.OriginMCP || pp.SessionID != "s1" || !pp.Untrusted || pp.Created.IsZero() {
		t.Errorf("dòng Plan sai: %+v", pp)
	}
	if pp.Fingerprint != engine.Fingerprint(pp.Preview) {
		t.Error("dấu vân tay không khớp bản xem trước đã lưu")
	}
	// Yêu cầu lưu dạng đã chuẩn hoá, và không mang mật khẩu.
	if want := `{"kind":"site.add","domain":"shop.example.com","email":"admin@example.com"}`; string(pp.Request) != want {
		t.Errorf("Request = %s; muốn %s", pp.Request, want)
	}
}

// ---------------------------------------------------------------- 3. duyệt

func TestApproveRebuildsAndDetectsChange(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	pp, err := pe.p.Propose(ctx, addReq("shop.example.com"), Proposal{Origin: engine.OriginAI})
	if err != nil {
		t.Fatal(err)
	}
	// Ai đó sửa tay máy trong lúc Plan chờ duyệt.
	l := pe.layout(t, "shop.example.com")
	pe.m.SetFile(l.NginxConf, []byte("# sửa tay\n"), 0o644, system.Root)

	pe.m.ResetRan()
	before := pe.m.Snapshot()
	_, err = pe.p.PrepareApprove(ctx, pp.ID, "cli")
	if code := refusedCode(t, err); code != "plan_changed" {
		t.Fatalf("mã = %s; muốn plan_changed", code)
	}
	for _, c := range pe.m.Ran() {
		if !planReadOnly(c) {
			t.Errorf("duyệt bị từ chối mà vẫn chạy lệnh thay đổi máy: %s", c.String())
		}
	}
	if !reflect.DeepEqual(before, pe.m.Snapshot()) {
		t.Error("duyệt bị từ chối mà hệ thống file đổi")
	}
	got := pe.plans.get(t, pp.ID)
	if got.Status != engine.PlanPending || got.Fingerprint == pp.Fingerprint || got.Fingerprint != engine.Fingerprint(got.Preview) {
		t.Fatalf("Plan sau plan_changed: status %s, vân tay cũ %s, mới %s", got.Status, pp.Fingerprint, got.Fingerprint)
	}
	if vh := stepPreview(t, got.Preview, "file:"+l.NginxConf); !vh.Info.Files[0].OldExists || string(vh.Info.Files[0].Old) != "# sửa tay\n" {
		t.Errorf("bản xem trước mới không thấy file vhost sửa tay: %+v", vh.Info.Files[0])
	}

	// Duyệt lần hai: bản xem trước mới chính là thứ sẽ chạy.
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	pe.p.Now = func() time.Time { return at }
	ap, err := pe.p.PrepareApprove(ctx, pp.ID, "web:192.0.2.1")
	if err != nil {
		t.Fatalf("duyệt lần hai: %v", err)
	}
	if ap.Plan.Status != engine.PlanApplied || ap.Plan.DecidedBy != "web:192.0.2.1" || !ap.Plan.Decided.Equal(at) {
		t.Errorf("Approval.Plan = %+v", ap.Plan)
	}
	var kinds []engine.EventKind
	res, err := ap.Execute(ctx, func(ev engine.Event) { kinds = append(kinds, ev.Kind) })
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %s (%s)%s", res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
	if !res.WPInstalled || res.AdminUser != siteAdminUser || res.AdminPassword.Reveal() == "" {
		t.Errorf("kết quả thiếu mật khẩu quản trị: %+v", res)
	}
	if len(kinds) == 0 || kinds[0] != engine.EventJobStart {
		t.Errorf("onEvent của người gọi không nhận đủ sự kiện: %v", kinds)
	}
	got = pe.plans.get(t, pp.ID)
	if got.Status != engine.PlanApplied || got.JobID != res.Report.Job.ID || ap.Plan.JobID != got.JobID {
		t.Errorf("Plan sau khi chạy: status %s, job %q; job thật %s", got.Status, got.JobID, res.Report.Job.ID)
	}
	if pe.sites.count() != 1 {
		t.Error("chạy xong mà bảng sites không có site")
	}

	// Duyệt lại, từ chối một Plan đã chạy, Plan không có.
	if _, err := pe.p.PrepareApprove(ctx, pp.ID, "cli"); refusedCode(t, err) != "plan_not_pending" {
		t.Errorf("duyệt hai lần: %v", err)
	}
	if err := pe.p.Reject(ctx, pp.ID, "cli"); refusedCode(t, err) != "plan_not_pending" {
		t.Errorf("từ chối Plan đã chạy: %v", err)
	}
	if _, err := pe.p.PrepareApprove(ctx, "0123456789abcdef", "cli"); refusedCode(t, err) != "plan_not_found" {
		t.Errorf("duyệt Plan không có: %v", err)
	}
}

func TestApproveRaceMapsNotPending(t *testing.T) {
	// Panel và CLI cùng duyệt: lúc đọc thì pending, lúc DecidePlan thì không.
	pe := newPlanEnv(t, Nginx)
	pp, err := pe.p.Propose(ctx, addReq("shop.example.com"), Proposal{Origin: engine.OriginHuman})
	if err != nil {
		t.Fatal(err)
	}
	pe.p.Plans = racePlans{memPlans: pe.plans}
	if _, err := pe.p.PrepareApprove(ctx, pp.ID, "cli"); refusedCode(t, err) != "plan_not_pending" {
		t.Errorf("duyệt tranh: %v", err)
	}
}

// racePlans: một bên khác vừa duyệt ngay trước DecidePlan của ta.
type racePlans struct{ *memPlans }

func (r racePlans) DecidePlan(c context.Context, id string, st engine.PlanStatus, by string, at time.Time) error {
	if err := r.memPlans.DecidePlan(c, id, engine.PlanRejected, "web:192.0.2.9", at); err != nil {
		return err
	}
	return r.memPlans.DecidePlan(c, id, st, by, at)
}

func TestReject(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	pp, err := pe.p.Propose(ctx, addReq("shop.example.com"), Proposal{Origin: engine.OriginMCP})
	if err != nil {
		t.Fatal(err)
	}
	if err := pe.p.Reject(ctx, pp.ID, "cli"); err != nil {
		t.Fatal(err)
	}
	if got := pe.plans.get(t, pp.ID); got.Status != engine.PlanRejected || got.DecidedBy != "cli" {
		t.Errorf("sau Reject: %+v", got)
	}
	if err := pe.p.Reject(ctx, pp.ID, "cli"); refusedCode(t, err) != "plan_not_pending" {
		t.Errorf("từ chối hai lần: %v", err)
	}
	if _, err := pe.p.PrepareApprove(ctx, pp.ID, "cli"); refusedCode(t, err) != "plan_not_pending" {
		t.Errorf("duyệt Plan đã từ chối: %v", err)
	}
	if err := pe.p.Reject(ctx, "ffffffffffffffff", "cli"); refusedCode(t, err) != "plan_not_found" {
		t.Errorf("từ chối Plan không có: %v", err)
	}
}

func TestExecuteLockBusyReopens(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	pp, err := pe.p.Propose(ctx, addReq("shop.example.com"), Proposal{Origin: engine.OriginMCP})
	if err != nil {
		t.Fatal(err)
	}
	ap, err := pe.p.PrepareApprove(ctx, pp.ID, "cli")
	if err != nil {
		t.Fatal(err)
	}
	// Một job khác đang giữ khoá; ctx của ta hết hạn khi chờ.
	unlock, err := pe.m.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	called := false
	if _, err := ap.Execute(short, func(engine.Event) { called = true }); err == nil {
		t.Fatal("khoá bận mà Execute không lỗi")
	}
	if called {
		t.Error("job chưa bắt đầu mà đã có sự kiện")
	}
	got := pe.plans.get(t, pp.ID)
	if got.Status != engine.PlanPending || got.DecidedBy != "" || got.JobID != "" || ap.Plan.Status != engine.PlanPending {
		t.Errorf("khoá bận thì Plan phải về pending: %+v", got)
	}
	unlock()
	// Mở lại rồi thì duyệt lại được (máy không đổi nên không plan_changed).
	if _, err := pe.p.PrepareApprove(ctx, pp.ID, "cli"); err != nil {
		t.Errorf("duyệt lại sau khi mở lại: %v", err)
	}
}

// ---------------------------------------------------------------- 4, 5. sửa

func TestSiteRepairFixesVhost(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			pe := newPlanEnv(t, web)
			pe.add(t, "shop.example.com")
			l := pe.layout(t, "shop.example.com")
			conf, want := l.NginxConf, func() ([]byte, error) { return templates.NginxSite(l, acmeRoot) }
			if web == Apache {
				conf, want = l.ApacheConf, func() ([]byte, error) { return templates.ApacheSite(l, acmeRoot) }
			}
			tmpl, err := want()
			if err != nil {
				t.Fatal(err)
			}
			pe.m.SetFile(conf, []byte("# sửa tay\n"), 0o644, system.Root)
			pe.m.ResetRan()

			pr, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "shop.example.com"})
			if err != nil {
				t.Fatal(err)
			}
			pv, err := pr.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range pv.Steps {
				wantDone := s.Info.ID != "file:"+conf
				if s.Done != wantDone {
					t.Errorf("bước %s: done = %v; muốn %v (%s)", s.Info.ID, s.Done, wantDone, s.CheckError)
				}
			}
			if pr.Plan.Kind != "site.add" || !strings.Contains(pr.Plan.Title.En, "Repair") {
				t.Errorf("Plan sửa: kind %s, tiêu đề %+v", pr.Plan.Kind, pr.Plan.Title)
			}

			// Đi trọn đường: đề xuất, duyệt, chạy.
			pp, err := pe.p.Propose(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "shop.example.com", Email: "bo-qua@example.com"}, Proposal{Origin: engine.OriginMCP})
			if err != nil {
				t.Fatal(err)
			}
			if string(pp.Request) != `{"kind":"site.repair","domain":"shop.example.com"}` {
				t.Errorf("Request = %s", pp.Request)
			}
			ap, err := pe.p.PrepareApprove(ctx, pp.ID, "cli")
			if err != nil {
				t.Fatal(err)
			}
			res, err := ap.Execute(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Report.Job.Status != engine.JobSucceeded {
				t.Fatalf("job: %s (%s)%s", res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
			}
			if res.WPInstalled || res.AdminPassword.Reveal() != "" {
				t.Error("sửa site không được báo là đã cài WordPress mới")
			}
			for _, s := range res.Report.Steps {
				wantOutcome := "skipped"
				if s.ID == "file:"+conf {
					wantOutcome = "applied"
				}
				if s.Outcome != wantOutcome {
					t.Errorf("bước %s: %s; muốn %s", s.ID, s.Outcome, wantOutcome)
				}
			}
			got, err := pe.m.ReadFile(conf)
			if err != nil || string(got) != string(tmpl) {
				t.Errorf("vhost sau khi sửa không về đúng template (lỗi %v)", err)
			}
			for _, c := range pe.m.Ran() {
				if c.Name == "wp" && len(c.Args) > 1 && c.Args[1] == "install" {
					t.Errorf("sửa site chạy `wp core install`: %s", c.String())
				}
			}
			if pp := pe.plans.get(t, pp.ID); pp.JobID != res.Report.Job.ID {
				t.Errorf("Plan không ghi job: %q", pp.JobID)
			}
		})
	}
}

func TestSiteRepairRefusals(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	pe.add(t, "shop.example.com")
	l := pe.layout(t, "shop.example.com")

	if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "blog.example.com"}); refusedCode(t, err) != "site_not_found" {
		t.Errorf("site không có: %v", err)
	}
	if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "a.com; rm -rf /"}); !errors.As(err, new(*validate.Error)) {
		t.Errorf("domain tấn công phải là lỗi validate: %v", err)
	}

	pe.m.mu.Lock()
	pe.m.installed[l.User] = false
	pe.m.mu.Unlock()
	pe.m.ResetRan()
	if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "shop.example.com"}); refusedCode(t, err) != "wp_not_installed" {
		t.Errorf("WordPress chưa cài: %v", err)
	}
	for _, c := range pe.m.Ran() {
		if !planReadOnly(c) {
			t.Errorf("Prepare chạy lệnh thay đổi máy: %s", c.String())
		}
	}

	// Lỗi khác mã 1 (PHP hỏng) là lỗi thật, không phải lời từ chối.
	pe.m.Handle([]string{"wp", "core", "is-installed"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 255, Stderr: "PHP Fatal error\n"}
	})
	if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "shop.example.com"}); err == nil || errors.As(err, new(*RefusedError)) {
		t.Errorf("WP-CLI lỗi 255 phải là lỗi thường: %v", err)
	}

	// Site ở thùng rác.
	s, _, _ := pe.sites.Site(ctx, l.Domain)
	s.Status = site.Trashed
	if err := pe.sites.SaveSite(ctx, s); err != nil {
		t.Fatal(err)
	}
	if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "shop.example.com"}); refusedCode(t, err) != "site_trashed" {
		t.Errorf("site ở thùng rác: %v", err)
	}
}

func TestRepairInstallStepRefusesFreshInstall(t *testing.T) {
	// WordPress bị gỡ giữa lúc duyệt và lúc chạy: bước cài không được cài với email tạm.
	pe := newPlanEnv(t, Nginx)
	pe.add(t, "shop.example.com")
	pr, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanSiteRepair, Domain: "shop.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var step *repairInstallStep
	for _, s := range pr.Plan.Steps {
		if r, ok := s.(*repairInstallStep); ok {
			step = r
		}
	}
	if step == nil {
		t.Fatal("Plan sửa không bọc bước cài WordPress")
	}
	pe.m.mu.Lock()
	pe.m.installed[step.Layout.User] = false
	pe.m.mu.Unlock()
	if err := step.Apply(ctx, pe.m); err == nil {
		t.Error("WordPress chưa cài mà bước cài của Plan sửa không lỗi")
	}
	pe.m.mu.Lock()
	defer pe.m.mu.Unlock()
	if pe.m.installed[step.Layout.User] {
		t.Error("bước cài của Plan sửa đã cài WordPress")
	}
}

func TestStackRepair(t *testing.T) {
	pe := newPlanEnv(t, "")
	if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanStackRepair}); refusedCode(t, err) != "stack_not_installed" {
		t.Errorf("stack chưa cài: %v", err)
	}
	if _, err := pe.p.Propose(ctx, PlanRequest{Kind: PlanStackRepair}, Proposal{Origin: engine.OriginMCP}); refusedCode(t, err) != "stack_not_installed" {
		t.Errorf("đề xuất sửa stack chưa cài: %v", err)
	}

	// Stack đã cài: dựng lại Plan cài với đúng web server đang dùng.
	s, m, st := newTestStack(t)
	mustSet(t, st, SettingWeb, string(Apache))
	mustSet(t, st, SettingPHPVersion, "8.3")
	p := &Planner{Stack: s, Plans: newMemPlans()}
	pr, err := p.Prepare(ctx, PlanRequest{Kind: PlanStackRepair, Domain: "bo-qua.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Plan.Kind != "stack.install" || pr.Plan.Target != string(Apache) {
		t.Errorf("Plan sửa stack: kind %s, target %s", pr.Plan.Kind, pr.Plan.Target)
	}
	if pr.Request != (PlanRequest{Kind: PlanStackRepair}) {
		t.Errorf("Request = %+v", pr.Request)
	}
	m.ResetRan()
	if _, err := pr.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.Ran() {
		if !readOnly(c) {
			t.Errorf("xem trước sửa stack chạy lệnh thay đổi máy: %s", c.String())
		}
	}
}

// memChecks là bảng check_results trong bộ nhớ (CheckLookup).
type memChecks map[string]checks.Result

func (m memChecks) CheckResultOf(_ context.Context, id string) (checks.Result, bool, error) {
	r, ok := m[id]
	return r, ok, nil
}

// health.fix: Prepare đọc kết quả ĐÃ LƯU của phép kiểm, không tin gì người gọi
// gửi ngoài id; đi trọn đường đề xuất, duyệt, chạy thì phép kiểm đạt lại.
func TestHealthFixPlan(t *testing.T) {
	const dom = "shop.example.com"
	pe := newPlanEnv(t, Nginx)
	pe.add(t, dom)
	l := pe.layout(t, dom)
	id := "web.vhost:" + dom
	if err := pe.m.Remove(l.NginxLink); err != nil {
		t.Fatal(err)
	}
	cs, err := pe.s.Checks(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	stored := memChecks{}
	for _, r := range checks.RunAll(ctx, pe.m, cs) {
		stored[r.ID] = r
	}
	if stored[id].Status != checks.Fail {
		t.Fatalf("xoá symlink mà %s = %s", id, stored[id].Status)
	}

	// Chưa có bảng kết quả (Checks nil), id trống, id không có: check_not_found.
	for _, tc := range []struct {
		name string
		c    CheckLookup
		id   string
	}{
		{"Checks nil", nil, id},
		{"id trống", stored, ""},
		{"id không có", stored, "web.vhost:none.example.com"},
	} {
		pe.p.Checks = tc.c
		if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanHealthFix, CheckID: tc.id}); refusedCode(t, err) != "check_not_found" {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	pe.p.Checks = stored

	// Kết quả đã lưu là Pass thì không có gì để sửa, dù người gọi muốn sửa.
	passID := "wp.config:" + dom
	if stored[passID].Status != checks.Pass {
		t.Fatalf("%s = %s", passID, stored[passID].Status)
	}
	if _, err := pe.p.Prepare(ctx, PlanRequest{Kind: PlanHealthFix, CheckID: passID}); refusedCode(t, err) != "no_auto_fix" {
		t.Errorf("kết quả đã lưu đạt: %v", err)
	}

	pp, err := pe.p.Propose(ctx, PlanRequest{Kind: PlanHealthFix, CheckID: id, Domain: "bo-qua.example.com"}, Proposal{Origin: engine.OriginAI})
	if err != nil {
		t.Fatal(err)
	}
	if string(pp.Request) != `{"kind":"health.fix","check_id":"`+id+`"}` {
		t.Errorf("Request = %s", pp.Request)
	}
	if pp.Kind != PlanHealthFix || pp.Target != dom || pp.Title.En == "" {
		t.Errorf("Plan: kind %s, target %s, tiêu đề %+v", pp.Kind, pp.Target, pp.Title)
	}
	ap, err := pe.p.PrepareApprove(ctx, pp.ID, "cli")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ap.Execute(ctx, nil)
	if err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %v %s (%s)%s", err, res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
	if res.Report.Job.Kind != "fix."+FixWebRewrite {
		t.Errorf("job kind %s", res.Report.Job.Kind)
	}
	for _, r := range checks.RunAll(ctx, pe.m, cs) {
		if r.ID == id && r.Status != checks.Pass {
			t.Errorf("sau khi sửa %s = %s", id, r.Status)
		}
	}
	if got := pe.plans.get(t, pp.ID); got.JobID != res.Report.Job.ID {
		t.Errorf("Plan không ghi job: %q", got.JobID)
	}
}

// ssl.issue: Prepare qua SSL.PrepareIssue (cùng lời từ chối với `tpan ssl
// issue`), Execute chạy SSLIssue.Execute.
func TestSSLIssuePlan(t *testing.T) {
	const dom = "shop.example.com"
	se := newSSLEnv(t, Nginx)
	se.add(t, dom)
	p := &Planner{Sites: se.s, Plans: newMemPlans(), NotFound: errPlanMissing, NotPending: errPlanNotPending}

	if _, err := p.Prepare(ctx, PlanRequest{Kind: PlanSSLIssue, Domain: dom}); refusedCode(t, err) != "acme_email_missing" {
		t.Errorf("chưa có email ACME: %v", err)
	}
	se.setEmail(t)
	if _, err := p.Prepare(ctx, PlanRequest{Kind: PlanSSLIssue, Domain: "none.example.com"}); refusedCode(t, err) != "site_not_found" {
		t.Errorf("site không có: %v", err)
	}
	if _, err := p.Prepare(ctx, PlanRequest{Kind: PlanSSLIssue, Domain: "a.com; rm -rf /"}); !errors.As(err, new(*validate.Error)) {
		t.Errorf("domain tấn công: %v", err)
	}

	pp, err := p.Propose(ctx, PlanRequest{Kind: PlanSSLIssue, Domain: "SHOP.example.com", Email: "bo-qua@example.com"}, Proposal{Origin: engine.OriginMCP})
	if err != nil {
		t.Fatal(err)
	}
	if string(pp.Request) != `{"kind":"ssl.issue","domain":"`+dom+`"}` {
		t.Errorf("Request = %s", pp.Request)
	}
	if pp.Kind != PlanSSLIssue || pp.Target != dom || !strings.Contains(pp.Title.En, "SSL") {
		t.Errorf("Plan: kind %s, target %s, tiêu đề %+v", pp.Kind, pp.Target, pp.Title)
	}
	if len(se.certbot) != 0 {
		t.Fatal("đề xuất đã chạy certbot")
	}
	ap, err := p.PrepareApprove(ctx, pp.ID, "cli")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ap.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Kiểm chứng của Plan SSL gọi HTTPS thật (máy giả không có), nên job có
	// thể hoàn tác; ở đây chỉ cần thấy đúng Plan xin SSL đã chạy.
	if res.Report.Job.ID == "" || res.Report.Job.Kind != "site.ssl" || len(se.certbot) != 1 {
		t.Errorf("job %+v; certbot %d lần", res.Report.Job, len(se.certbot))
	}
}

func TestPrepareInputs(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	for _, tc := range []struct {
		name string
		req  PlanRequest
		code string // rỗng: lỗi validate (không phải RefusedError)
	}{
		{"loại lạ", PlanRequest{Kind: "site.nuke", Domain: "shop.example.com"}, "plan_kind_invalid"},
		{"loại trống", PlanRequest{}, "plan_kind_invalid"},
		{"domain tấn công", PlanRequest{Kind: PlanSiteAdd, Domain: "$(id).example.com", Email: "admin@example.com"}, ""},
		{"domain đường dẫn", PlanRequest{Kind: PlanSiteRemove, Domain: "../../etc/passwd"}, ""},
		{"domain xuống dòng", PlanRequest{Kind: PlanSiteAdd, Domain: "a.com\nb.com", Email: "admin@example.com"}, ""},
		{"domain dài", PlanRequest{Kind: PlanSiteAdd, Domain: strings.Repeat("a", 300) + ".com", Email: "admin@example.com"}, ""},
		{"email trống", PlanRequest{Kind: PlanSiteAdd, Domain: "shop.example.com"}, ""},
		{"email tấn công", PlanRequest{Kind: PlanSiteAdd, Domain: "shop.example.com", Email: "a@example.com; rm -rf /"}, ""},
		{"tiêu đề xuống dòng", PlanRequest{Kind: PlanSiteAdd, Domain: "shop.example.com", Email: "admin@example.com", Title: "a\nb"}, "title_invalid"},
		{"xoá site không có", PlanRequest{Kind: PlanSiteRemove, Domain: "shop.example.com"}, "site_not_found"},
		{"sửa phép kiểm không id", PlanRequest{Kind: PlanHealthFix}, "check_not_found"},
		{"SSL domain tấn công", PlanRequest{Kind: PlanSSLIssue, Domain: "$(id).example.com"}, ""},
		{"SSL domain trống", PlanRequest{Kind: PlanSSLIssue}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pe.p.Prepare(ctx, tc.req)
			if err == nil {
				t.Fatal("muốn lỗi")
			}
			if tc.code != "" {
				if got := refusedCode(t, err); got != tc.code {
					t.Errorf("mã = %s; muốn %s", got, tc.code)
				}
			} else if !errors.As(err, new(*validate.Error)) {
				t.Errorf("muốn *validate.Error, có: %v", err)
			}
		})
	}
}

func TestSiteRemovePlanFlow(t *testing.T) {
	pe := newPlanEnv(t, Nginx)
	pe.add(t, "shop.example.com")
	pp, err := pe.p.Propose(ctx, PlanRequest{Kind: PlanSiteRemove, Domain: "shop.example.com"}, Proposal{Origin: engine.OriginMCP})
	if err != nil {
		t.Fatal(err)
	}
	ap, err := pe.p.PrepareApprove(ctx, pp.ID, "cli")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ap.Execute(ctx, nil)
	if err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá: %v, %s", err, res.Report.Job.Status)
	}
	if pe.sites.count() != 0 {
		t.Error("xoá xong mà bảng sites còn site")
	}
}

// ---------------------------------------------------------------- 6. cấu hình cổng agent

// mapSettings là Settings trong bộ nhớ.
type mapSettings map[string]string

func (m mapSettings) GetSetting(_ context.Context, k string) (string, bool, error) {
	v, ok := m[k]
	return v, ok, nil
}

func (m mapSettings) SetSetting(_ context.Context, k, v string) error { m[k] = v; return nil }

type failSettings struct{}

func (failSettings) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, errors.New("DB khoá")
}
func (failSettings) SetSetting(context.Context, string, string) error { return nil }

func TestAgentConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    Settings
		want AgentSettings
		err  bool
	}{
		{"chưa có DB", EmptySettings{}, AgentSettings{Approval: ApprovalPanel, Redact: "all", Brain: BrainOff}, false},
		{"trống", mapSettings{SettingMCPApproval: "", SettingAIRedact: "", SettingAIBrain: ""}, AgentSettings{Approval: ApprovalPanel, Redact: "all", Brain: BrainOff}, false},
		{"client, secrets", mapSettings{SettingMCPApproval: "client", SettingAIRedact: "secrets"}, AgentSettings{Approval: ApprovalClient, Redact: "secrets", Brain: BrainOff}, false},
		{"panel, all", mapSettings{SettingMCPApproval: "panel", SettingAIRedact: "all"}, AgentSettings{Approval: ApprovalPanel, Redact: "all", Brain: BrainOff}, false},
		{"bộ não bridge", mapSettings{SettingAIBrain: "bridge"}, AgentSettings{Approval: ApprovalPanel, Redact: "all", Brain: BrainBridge}, false},
		{"bộ não off", mapSettings{SettingAIBrain: "off"}, AgentSettings{Approval: ApprovalPanel, Redact: "all", Brain: BrainOff}, false},
		{"bộ não lạ", mapSettings{SettingAIBrain: "local"}, AgentSettings{}, true},
		{"bộ não hoa thường", mapSettings{SettingAIBrain: "Bridge"}, AgentSettings{}, true},
		{"chế độ lạ", mapSettings{SettingMCPApproval: "auto"}, AgentSettings{}, true},
		{"hoa thường", mapSettings{SettingMCPApproval: "Client"}, AgentSettings{}, true},
		{"mức che lạ", mapSettings{SettingAIRedact: "none"}, AgentSettings{}, true},
		{"đọc lỗi", failSettings{}, AgentSettings{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AgentConfig(ctx, tc.s)
			if (err != nil) != tc.err {
				t.Fatalf("lỗi = %v; muốn lỗi: %v", err, tc.err)
			}
			if got != tc.want {
				t.Errorf("= %+v; muốn %+v", got, tc.want)
			}
		})
	}
}
