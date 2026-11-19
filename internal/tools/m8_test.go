package tools

// Test cho các công cụ thêm ở M8: propose_health_fix, propose_ssl_issue,
// current_view.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
)

// recordFail ghi vào check_results một kết quả Fail của phép kiểm id, như một
// lần health đã chạy và thấy lỗi.
func (e *env) recordFail(t *testing.T, target, id string) {
	t.Helper()
	r := checks.Result{ID: id, Stage: checks.StageWeb, Status: checks.Fail,
		Title: i18n.T("kiểm", "check"), Summary: i18n.T("hỏng", "broken")}
	r.AutoFix = core.AutoFixName(r)
	if _, err := e.st.RecordResults(ctx, target, []checks.Result{r}, e.now); err != nil {
		t.Fatal(err)
	}
}

// Agent đọc id phép kiểm ở dạng đã che (bí danh trong domain, slug trong
// đường dẫn) rồi gửi lại nguyên văn: Planner phải tìm được đúng kết quả đã lưu.
func TestProposeHealthFixAliasRoundTrip(t *testing.T) {
	e := newEnv(t, "")
	s := e.open(t, "")
	u := mustDomain(t, domU).String()
	l := site.LayoutOf(mustDomain(t, domU))
	for _, id := range []string{
		"web.vhost:" + u,
		"drift:" + l.NginxConf, // đường dẫn có slug của site
		"service.active:nginx",
	} {
		e.recordFail(t, u, id)
		seen := alias(s, id)
		if strings.Contains(id, u) && strings.Contains(seen, u) {
			t.Fatalf("id %s chưa được che: %s", id, seen)
		}
		out := callOK(t, s, "propose_health_fix", `{"check_id":"`+seen+`"}`)
		pid := field[string](t, out, "plan_id")
		pp, err := e.st.Plan(ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		var req core.PlanRequest
		if err := json.Unmarshal(pp.Request, &req); err != nil {
			t.Fatal(err)
		}
		if pp.Kind != core.PlanHealthFix || req.CheckID != id || pp.Status != engine.PlanPending {
			t.Errorf("%s: Plan kind %s, check_id %q, status %s", id, pp.Kind, req.CheckID, pp.Status)
		}
		if strings.Contains(string(out), u) {
			t.Errorf("%s: kết quả lộ domain thật: %s", id, out)
		}
	}

	// Chỉ nhận check_id: không gửi kèm được trạng thái hay cách sửa.
	if err := callErr(t, s, "propose_health_fix", `{"check_id":"web.vhost:`+alias(s, u)+`","status":"fail","auto_fix":"service.start"}`); err.Code != "invalid_arguments" {
		t.Errorf("trường lạ: %v", err)
	}
	if err := callErr(t, s, "propose_health_fix", `{}`); err.Code != "check_not_found" {
		t.Errorf("thiếu check_id: %v", err)
	}
	// Đã lưu nhưng không có cách sửa tự động.
	e.recordFail(t, u, "http.site:"+u)
	if err := callErr(t, s, "propose_health_fix", `{"check_id":"http.site:`+alias(s, u)+`"}`); err.Code != "no_auto_fix" {
		t.Errorf("http.site: %v", err)
	}
}

// propose_ssl_issue đi qua SSL.PrepareIssue: site không có, chưa đặt email,
// domain tấn công đều bị từ chối, không Plan nào được tạo.
func TestProposeSSLIssueRefusals(t *testing.T) {
	e := newEnv(t, "")
	s := e.open(t, "")
	for _, tc := range []struct{ args, code string }{
		{`{"domain":"` + alias(s, domA) + `"}`, "acme_email_missing"},
		{`{"domain":"nothere.example.org"}`, "site_not_found"},
		{`{"domain":"a.com; rm -rf /"}`, "domain_invalid"},
	} {
		if err := callErr(t, s, "propose_ssl_issue", tc.args); err.Code != tc.code {
			t.Errorf("%s: %v; muốn %s", tc.args, err, tc.code)
		}
	}
	if ps, _ := e.st.Plans(ctx, "", 0); len(ps) != 0 {
		t.Errorf("lời gọi bị từ chối vẫn tạo %d Plan", len(ps))
	}
}

// apply_plan ở chế độ client chạy được Plan health.fix của chính phiên (chỉ
// site.add bị chặn).
func TestApplyPlanHealthFix(t *testing.T) {
	e := newEnv(t, core.ApprovalClient)
	s := e.open(t, "")
	a := mustDomain(t, domA).String()
	e.recordFail(t, a, "web.vhost:"+a)
	out := callOK(t, s, "propose_health_fix", `{"check_id":"web.vhost:`+alias(s, a)+`"}`)
	id := field[string](t, out, "plan_id")
	res := callOK(t, s, "apply_plan", `{"plan_id":"`+id+`"}`)
	jobID := field[string](t, res, "job_id")
	pp, err := e.st.Plan(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if jobID == "" || pp.Status != engine.PlanApplied || pp.JobID != jobID {
		t.Errorf("apply_plan health.fix: job %q, Plan %s, job trong Plan %q; kết quả %s", jobID, pp.Status, pp.JobID, res)
	}
	job, err := e.st.Job(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Kind != "fix."+core.FixWebRewrite || job.Status != engine.JobSucceeded {
		t.Errorf("job %s: %s (%s)", job.Kind, job.Status, job.Error)
	}
}

type viewOut struct {
	Current *viewRow  `json:"current"`
	Recent  []viewRow `json:"recent"`
	Note    string    `json:"note"`
}

func TestCurrentView(t *testing.T) {
	e := newEnv(t, "")
	s := e.open(t, "")

	// Panel chưa báo gì: current null, recent là mảng rỗng, có lời nhắn.
	raw := callOK(t, s, "current_view", `{}`)
	var empty viewOut
	if err := json.Unmarshal(raw, &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Current != nil || empty.Recent == nil || len(empty.Recent) != 0 || empty.Note == "" {
		t.Errorf("chưa có mục nào: %s", raw)
	}
	if !strings.Contains(string(raw), `"recent":[]`) {
		t.Errorf("recent phải là [] chứ không phải null: %s", raw)
	}

	// 12 lần chuyển mục, lần sau mới hơn; lần cuối là trang site domA.
	a := mustDomain(t, domA).String()
	for i := range 12 {
		v := store.AIView{Time: e.now.Add(time.Duration(i) * time.Minute), Path: "/history?n=" + strconv.Itoa(i), Label: "Lịch sử " + strconv.Itoa(i)}
		if i == 11 {
			v.Path, v.Label = "/sites/"+a, "Site "+a
		}
		if err := e.st.AddAIView(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	raw = callOK(t, s, "current_view", `{}`)
	if strings.Contains(string(raw), a) {
		t.Errorf("current_view lộ domain thật: %s", raw)
	}
	var got viewOut
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Current == nil || got.Current.Path != "/sites/"+alias(s, a) {
		t.Fatalf("mục đang mở: %s", raw)
	}
	if len(got.Recent) != maxRecentViews {
		t.Fatalf("recent có %d mục; muốn %d", len(got.Recent), maxRecentViews)
	}
	// Mới trước: ngay trước trang site là lần 10, cũ nhất được giữ là lần 1.
	if got.Recent[0].Path != "/history?n=10" || got.Recent[maxRecentViews-1].Path != "/history?n=1" {
		t.Errorf("thứ tự recent: %+v", got.Recent)
	}
	calls, _ := e.st.AICalls(ctx, s.ID())
	if last := calls[len(calls)-1]; last.Tool != "current_view" || strings.Contains(string(last.Output), a) {
		t.Errorf("ai_calls: %+v", last)
	}
}
