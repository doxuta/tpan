package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// planPropose đề xuất một Plan xoá site dom qua đúng Planner mà API dùng (để
// dấu vân tay lúc đề xuất và lúc duyệt tính trên cùng một cách dựng).
func (e *apiEnv) planPropose(t *testing.T, dom string) engine.PendingPlan {
	t.Helper()
	pp, err := e.s.apiPlanner().Propose(context.Background(),
		core.PlanRequest{Kind: core.PlanSiteRemove, Domain: dom},
		core.Proposal{Origin: engine.OriginMCP, SessionID: "s1"})
	if err != nil {
		t.Fatalf("đề xuất Plan: %v", err)
	}
	return pp
}

func TestAPIPlansListAndGet(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	if got := apiCall(e.s.handlePlans, "GET", "/api/v1/plans", "").Body.String(); strings.TrimSpace(got) != "[]" {
		t.Errorf("chưa có Plan: %s, muốn []", got)
	}
	e.addSite(t, "a.example.com")
	e.addSite(t, "b.example.com")
	pa := e.planPropose(t, "a.example.com")
	e.advance(1)
	pb := e.planPropose(t, "b.example.com")
	if err := e.s.apiPlanner().Reject(context.Background(), pa.ID, "cli"); err != nil {
		t.Fatal(err)
	}

	all := apiDecode[[]engine.PendingPlan](t, apiCall(e.s.handlePlans, "GET", "/api/v1/plans", ""), 200)
	if len(all) != 2 || all[0].ID != pb.ID || all[1].ID != pa.ID {
		t.Fatalf("danh sách = %+v, muốn %s rồi %s (mới nhất trước)", all, pb.ID, pa.ID)
	}
	pending := apiDecode[[]engine.PendingPlan](t, apiCall(e.s.handlePlans, "GET", "/api/v1/plans?status=pending", ""), 200)
	if len(pending) != 1 || pending[0].ID != pb.ID {
		t.Fatalf("status=pending = %+v", pending)
	}
	for _, bad := range []string{"foo", "PENDING", "pending%3Bid"} {
		if got := apiErrCode(t, apiCall(e.s.handlePlans, "GET", "/api/v1/plans?status="+bad, ""), 400); got != "status_invalid" {
			t.Errorf("status=%s: code = %q", bad, got)
		}
	}

	rec := apiCall(e.s.handlePlan, "GET", "/", "", "id", pb.ID)
	got := apiDecode[engine.PendingPlan](t, rec, 200)
	if got.ID != pb.ID || got.Kind != core.PlanSiteRemove || got.Target != "b.example.com" ||
		got.Status != engine.PlanPending || got.Origin != engine.OriginMCP || len(got.Preview.Steps) == 0 {
		t.Fatalf("Plan = %+v", got)
	}
	// JSON khớp PendingPlan của web/src/api/types.ts: request là object.
	for _, k := range []string{`"request":{"kind":"site.rm"`, `"preview":{`, `"fingerprint":"`, `"untrusted":false`, `"session_id":"s1"`} {
		if !strings.Contains(rec.Body.String(), k) {
			t.Errorf("JSON thiếu %s", k)
		}
	}
	for _, id := range []string{"0123456789abcdef", "khongco", "../../etc/passwd", "0123456789ABCDEF"} {
		if got := apiErrCode(t, apiCall(e.s.handlePlan, "GET", "/", "", "id", id), 404); got != "plan_not_found" {
			t.Errorf("id %q: code = %q", id, got)
		}
	}
}

func TestAPIPlanApprove(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "old.example.com")
	pp := e.planPropose(t, "old.example.com")

	js := apiDecode[JobStarted](t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", pp.ID), 202)
	e.s.api.wg.Wait()
	// Từ M6, site.rm là đưa vào thùng rác: khoá user DB và user Linux đòi
	// chúng có trên máy, mà FakeSystem ở đây không có, nên job hoàn tác. Test
	// này kiểm đường duyệt (job đã chạy, Plan applied), không kiểm job thành công.
	job, err := e.st.Job(context.Background(), js.JobID)
	if err != nil || job.Status == engine.JobRunning || job.Kind != "site.rm" {
		t.Fatalf("job = %+v, %v", job, err)
	}
	got := apiDecode[engine.PendingPlan](t, apiCall(e.s.handlePlan, "GET", "/", "", "id", pp.ID), 200)
	if got.Status != engine.PlanApplied || got.JobID != js.JobID || got.DecidedBy != "web:192.0.2.10" {
		t.Fatalf("Plan sau khi duyệt = status %s, job %q, by %q", got.Status, got.JobID, got.DecidedBy)
	}

	// Duyệt lần hai: không còn chờ, và lời từ chối phải nhả chỗ chạy job.
	if code := apiErrCode(t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", pp.ID), 409); code != "plan_not_pending" {
		t.Fatalf("duyệt lại: code = %q", code)
	}
	if code := apiErrCode(t, apiCall(e.s.handlePlanReject, "POST", "/", "", "id", pp.ID), 409); code != "plan_not_pending" {
		t.Fatalf("từ chối Plan đã chạy: code = %q", code)
	}
	if !e.s.api.claim() {
		t.Fatal("lời từ chối không nhả chỗ chạy job")
	}
	e.s.api.release()
}

// TestAPIPlanChanged: máy đổi sau lúc đề xuất (user Linux của site xuất hiện)
// thì duyệt bị từ chối, Plan vẫn chờ, và GET thấy bản xem trước mới.
func TestAPIPlanChanged(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "old.example.com")
	pp := e.planPropose(t, "old.example.com")

	e.f.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "x:x:1001:1001::/var/www/x:/usr/sbin/nologin\n"}
	})
	if code := apiErrCode(t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", pp.ID), 409); code != "plan_changed" {
		t.Fatalf("code = %q, muốn plan_changed", code)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("Plan đã đổi mà vẫn chạy job: %v", jobs)
	}
	got := apiDecode[engine.PendingPlan](t, apiCall(e.s.handlePlan, "GET", "/", "", "id", pp.ID), 200)
	if got.Status != engine.PlanPending || got.Fingerprint == pp.Fingerprint || got.Fingerprint != engine.Fingerprint(got.Preview) {
		t.Fatalf("sau plan_changed: status %s, dấu vân tay cũ %s, mới %s", got.Status, pp.Fingerprint, got.Fingerprint)
	}
	if !e.s.api.claim() {
		t.Fatal("plan_changed không nhả chỗ chạy job")
	}
	e.s.api.release()
}

func TestAPIPlanApproveErrors(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	ctx := context.Background()

	if code := apiErrCode(t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", "0123456789abcdef"), 404); code != "plan_not_found" {
		t.Errorf("không có Plan: code = %q", code)
	}
	// Loại Plan lạ (ví dụ Plan của bản tpan mới hơn) là lỗi đầu vào: 400.
	bad := engine.PendingPlan{
		ID: "00000000000000aa", Kind: "x.y", Target: "a.example.com", Request: json.RawMessage(`{"kind":"x.y"}`),
		Origin: engine.OriginMCP, Status: engine.PlanPending, Created: e.clock(),
	}
	if err := e.st.CreatePlan(ctx, bad); err != nil {
		t.Fatal(err)
	}
	if code := apiErrCode(t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", bad.ID), 400); code != "plan_kind_invalid" {
		t.Errorf("loại lạ: code = %q", code)
	}
	if p, _ := e.st.Plan(ctx, bad.ID); p.Status != engine.PlanPending {
		t.Errorf("loại lạ mà Plan thành %s", p.Status)
	}

	// Đang có job khác: 409 job_running, Plan không bị đụng.
	e.addSite(t, "a.example.com")
	pp := e.planPropose(t, "a.example.com")
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if code := apiErrCode(t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", pp.ID), 409); code != "job_running" {
		t.Errorf("đang có job: code = %q", code)
	}
	e.s.api.release()
	if p, _ := e.st.Plan(ctx, pp.ID); p.Status != engine.PlanPending {
		t.Errorf("đang có job mà Plan thành %s", p.Status)
	}
}

// TestAPIPlanApproveLockHeld: khoá toàn cục bị giữ (CLI đang chạy job): 409
// job_running, và Plan trở lại chờ duyệt để bấm lại sau.
func TestAPIPlanApproveLockHeld(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	pp := e.planPropose(t, "a.example.com")
	unlock, err := e.f.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if code := apiErrCode(t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", pp.ID), 409); code != "job_running" {
		t.Fatalf("code = %q", code)
	}
	unlock()
	if p, _ := e.st.Plan(context.Background(), pp.ID); p.Status != engine.PlanPending || p.DecidedBy != "" {
		t.Fatalf("khoá bận mà Plan = %s, by %q", p.Status, p.DecidedBy)
	}
	apiDecode[JobStarted](t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", pp.ID), 202)
}

func TestAPIPlanReject(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	pp := e.planPropose(t, "a.example.com")
	rec := apiCall(e.s.handlePlanReject, "POST", "/", "", "id", pp.ID)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("từ chối: mã %d, thân %q", rec.Code, rec.Body)
	}
	got := apiDecode[engine.PendingPlan](t, apiCall(e.s.handlePlan, "GET", "/", "", "id", pp.ID), 200)
	if got.Status != engine.PlanRejected || got.DecidedBy != "web:192.0.2.10" || got.JobID != "" {
		t.Fatalf("Plan = %+v", got)
	}
	if code := apiErrCode(t, apiCall(e.s.handlePlanApprove, "POST", "/", "", "id", pp.ID), 409); code != "plan_not_pending" {
		t.Fatalf("duyệt Plan đã từ chối: code = %q", code)
	}
	if code := apiErrCode(t, apiCall(e.s.handlePlanReject, "POST", "/", "", "id", "fedcba9876543210"), 404); code != "plan_not_found" {
		t.Fatalf("không có Plan: code = %q", code)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("từ chối mà có job: %v", jobs)
	}
}

// TestAPIPlansRoutes đi qua NewHandler: đường dẫn đã gắn, cần đăng nhập, POST
// từ trang khác bị chặn, và thông tin agent mặc định.
func TestAPIPlansRoutes(t *testing.T) {
	e := newAuthEnv(t)
	const ip = "192.0.2.1"
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/plans"},
		{"GET", "/api/v1/plans/0123456789abcdef"},
		{"POST", "/api/v1/plans/0123456789abcdef/approve"},
		{"POST", "/api/v1/plans/0123456789abcdef/reject"},
		{"GET", "/api/v1/agent"},
	} {
		if rec := e.req(c.method, c.path, "", ip, nil); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "unauthorized" {
			t.Errorf("%s %s chưa đăng nhập: mã %d %s, muốn 401", c.method, c.path, rec.Code, rec.Body)
		}
	}

	cookie := e.mustLogin(ip)
	if rec := e.req("GET", "/api/v1/plans", "", ip, cookie); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("GET /plans: mã %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/api/v1/plans/0123456789abcdef/approve", "/api/v1/plans/0123456789abcdef/reject"} {
		rec := e.req("POST", path, "", ip, cookie, "Origin", "https://evil.example.com")
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "csrf" {
			t.Errorf("POST %s khác origin: mã %d %s, muốn 403 csrf", path, rec.Code, rec.Body)
		}
	}
	if rec := e.req("POST", "/api/v1/plans/0123456789abcdef/reject", "", ip, cookie, "Sec-Fetch-Site", "same-origin"); rec.Code != http.StatusNotFound || errCode(t, rec) != "plan_not_found" {
		t.Errorf("từ chối Plan không có: mã %d %s, muốn 404 plan_not_found", rec.Code, rec.Body)
	}

	rec := e.req("GET", "/api/v1/agent", "", ip, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /agent: mã %d %s", rec.Code, rec.Body)
	}
	var ai AgentInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &ai); err != nil {
		t.Fatal(err)
	}
	want := AgentInfo{
		Approval: "panel", Redact: "all", Hostname: "may-thu",
		ClaudeCommand: "claude mcp add --transport stdio tpan -- ssh <user>@may-thu sudo -n tpan mcp",
		CodexCommand:  "codex mcp add tpan -- ssh <user>@may-thu sudo -n tpan mcp",
	}
	if ai != want {
		t.Fatalf("agent = %+v\nmuốn    %+v", ai, want)
	}
}

func TestAPIAgentSettings(t *testing.T) {
	e := apiNewEnv(t)
	ctx := context.Background()
	if err := e.st.SetSetting(ctx, core.SettingMCPApproval, "client"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetSetting(ctx, core.SettingAIRedact, "secrets"); err != nil {
		t.Fatal(err)
	}
	ai := apiDecode[AgentInfo](t, apiCall(e.s.handleAgent, "GET", "/", ""), 200)
	if ai.Approval != "client" || ai.Redact != "secrets" || ai.Hostname != "vps-test" {
		t.Fatalf("agent = %+v", ai)
	}
	// Giá trị lạ trong settings là lỗi của máy chủ, không đoán thành mặc định.
	if err := e.st.SetSetting(ctx, core.SettingMCPApproval, "auto"); err != nil {
		t.Fatal(err)
	}
	if code := apiErrCode(t, apiCall(e.s.handleAgent, "GET", "/", ""), 500); code != "internal" {
		t.Fatalf("code = %q", code)
	}
}
