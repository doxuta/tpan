package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// seedPlan lưu một Plan "sửa stack" chờ duyệt vào DB của c, đề xuất qua đúng
// core.Planner trên c.fake như agent làm, để dấu vân tay khớp lúc duyệt.
func (c *cli) seedPlan(from core.Proposal) engine.PendingPlan {
	c.t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
		if err := st.SetSetting(ctx, k, v); err != nil {
			c.t.Fatal(err)
		}
	}
	pp, err := newPlanner(&env{sys: c.fake}, st).Propose(ctx, core.PlanRequest{Kind: core.PlanStackRepair}, from)
	if err != nil {
		c.t.Fatalf("Propose: %v", err)
	}
	return pp
}

// plan đọc lại Plan từ DB.
func (c *cli) plan(id string) engine.PendingPlan {
	c.t.Helper()
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	pp, err := st.Plan(context.Background(), id)
	if err != nil {
		c.t.Fatal(err)
	}
	return pp
}

// audits đếm dòng nhật ký theo thao tác. store không có hàm đọc audit, nên đọc
// thẳng bằng driver sqlite mà store đã đăng ký.
func (c *cli) audits(action string) int {
	c.t.Helper()
	db, err := sql.Open("sqlite", c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit WHERE action = ? AND ip = 'cli'", action).Scan(&n); err != nil {
		c.t.Fatal(err)
	}
	return n
}

func refusalCode(t *testing.T, out string) string {
	t.Helper()
	var got struct {
		Error core.RefusedError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout không phải JSON lời từ chối: %v\n%s", err, out)
	}
	return got.Error.Code
}

var mcpFrom = core.Proposal{Origin: engine.OriginMCP, SessionID: "s1", Untrusted: true}

func TestPlanListShowReject(t *testing.T) {
	c := newCLI(t)
	pp := c.seedPlan(mcpFrom)

	code, out, errs := c.run("plan", "list")
	if code != 0 || !strings.Contains(out, pp.ID) || !strings.Contains(out, "đã đọc nội dung lạ") {
		t.Errorf("list: mã %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, _ = c.run("plan", "list", "--status", "pending", "--json")
	var list []engine.PendingPlan
	if err := json.Unmarshal([]byte(out), &list); err != nil || code != 0 || len(list) != 1 || list[0].ID != pp.ID {
		t.Errorf("list --json: mã %d, %+v (%v)", code, list, err)
	}
	if code, out, _ := c.run("plan", "list", "--status", "rejected", "--json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("list --status rejected: mã %d, %q", code, out)
	}
	if code, out, _ := c.run("plan", "list", "--status", "done; id", "--json"); code != 1 || refusalCode(t, out) != "status_invalid" {
		t.Errorf("--status lạ: mã %d, %q", code, out)
	}

	code, out, _ = c.run("plan", "show", pp.ID)
	for _, want := range []string{pp.ID, "agent qua MCP", "phiên s1", "CẢNH BÁO", "Kế hoạch:"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("show: mã %d, thiếu %q:\n%s", code, want, out)
		}
	}
	code, out, _ = c.run("plan", "show", pp.ID, "--json")
	var got engine.PendingPlan
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || got.Fingerprint != pp.Fingerprint || !got.Untrusted {
		t.Errorf("show --json: mã %d, %+v (%v)", code, got, err)
	}
	for _, args := range [][]string{
		{"plan", "show", "khongco", "--json"},
		{"plan", "reject", "khongco", "--json"},
		{"plan", "approve", "khongco", "--yes", "--json"},
	} {
		if code, out, _ := c.run(args...); code != 1 || refusalCode(t, out) != "plan_not_found" {
			t.Errorf("%q: mã %d, %q", args, code, out)
		}
	}

	if code, out, errs := c.run("plan", "reject", pp.ID); code != 0 || !strings.Contains(out, "Đã từ chối") {
		t.Fatalf("reject: mã %d, %q %q", code, out, errs)
	}
	if p := c.plan(pp.ID); p.Status != engine.PlanRejected || p.DecidedBy != "cli" {
		t.Errorf("sau reject: %s bởi %q", p.Status, p.DecidedBy)
	}
	if n := c.audits("plan.reject"); n != 1 {
		t.Errorf("audit plan.reject: %d dòng", n)
	}
	for _, args := range [][]string{
		{"plan", "reject", pp.ID, "--json"},
		{"plan", "approve", pp.ID, "--yes", "--json"},
	} {
		if code, out, _ := c.run(args...); code != 1 || refusalCode(t, out) != "plan_not_pending" {
			t.Errorf("%q với Plan đã từ chối: mã %d, %q", args, code, out)
		}
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("xem và từ chối mà chạy lệnh thay đổi máy: %v", m)
	}
}

func TestPlanApprove(t *testing.T) {
	c := newCLI(t)
	pp := c.seedPlan(mcpFrom)

	// Không terminal, không --yes: từ chối, Plan vẫn chờ.
	if code, _, errs := c.run("plan", "approve", pp.ID); code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("không terminal: mã %d, %q", code, errs)
	}
	// Trả lời "n": in nguồn gốc, cảnh báo và bản xem trước; Plan vẫn chờ, không job.
	c.terminal, c.stdin = true, "n\n"
	code, out, _ := c.run("plan", "approve", pp.ID)
	for _, want := range []string{"CẢNH BÁO", "Kế hoạch:", "Chạy? [y/N]", "Plan vẫn chờ duyệt"} {
		if code != 1 || !strings.Contains(out, want) {
			t.Errorf("trả lời n: mã %d, thiếu %q:\n%s", code, want, out)
		}
	}
	if p := c.plan(pp.ID); p.Status != engine.PlanPending || p.JobID != "" {
		t.Errorf("sau khi huỷ: %s, job %q", p.Status, p.JobID)
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("huỷ mà chạy lệnh thay đổi máy: %v", m)
	}

	// Duyệt thật. Máy giả không có gói nào nên job hỏng và hoàn tác; điều cần
	// kiểm là Plan đã duyệt, gắn job và có nhật ký.
	c.terminal = false
	code, out, errs := c.run("plan", "approve", pp.ID, "--yes", "--json")
	var res planApproveJSON
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("approve --json: mã %d, %v\n%s\n%s", code, err, out, errs)
	}
	if res.Report.Job.ID == "" || res.Plan.JobID != res.Report.Job.ID || res.Plan.Status != engine.PlanApplied {
		t.Errorf("approve --json = %+v", res)
	}
	if !strings.Contains(errs, "Bắt đầu job") {
		t.Errorf("--json: tiến độ phải ra stderr:\n%s", errs)
	}
	if code != 0 && res.Report.Job.Status == engine.JobSucceeded {
		t.Errorf("job thành công mà mã %d", code)
	}
	p := c.plan(pp.ID)
	if p.Status != engine.PlanApplied || p.DecidedBy != "cli" || p.JobID != res.Report.Job.ID {
		t.Errorf("sau duyệt: %+v", p)
	}
	if n := c.audits("plan.approve"); n != 1 {
		t.Errorf("audit plan.approve: %d dòng", n)
	}

	// Duyệt lần hai: từ chối, không chạy lại.
	before := len(c.fake.Ran())
	if code, out, _ := c.run("plan", "approve", pp.ID, "--yes", "--json"); code != 1 || refusalCode(t, out) != "plan_not_pending" {
		t.Errorf("duyệt lần hai: mã %d, %q", code, out)
	}
	if after := len(c.fake.Ran()); after != before {
		t.Errorf("duyệt lần hai chạy thêm %d lệnh", after-before)
	}
}

// Máy đổi giữa lúc đề xuất và lúc duyệt: từ chối plan_changed, Plan vẫn chờ
// với bản xem trước mới, không lệnh thay đổi nào chạy.
func TestPlanApproveChanged(t *testing.T) {
	c := newCLI(t)
	pp := c.seedPlan(core.Proposal{Origin: engine.OriginHuman})
	var path string
	for _, s := range pp.Preview.Steps {
		if len(s.Info.Files) > 0 {
			path = s.Info.Files[0].Path
			break
		}
	}
	if path == "" {
		t.Fatal("Plan sửa stack không ghi file nào")
	}
	c.fake.SetFile(path, []byte("# sửa tay sau lúc đề xuất\n"), 0o644, system.Root)

	code, out, _ := c.run("plan", "approve", pp.ID, "--yes", "--json")
	if code != 1 || refusalCode(t, out) != "plan_changed" {
		t.Fatalf("mã %d, %q", code, out)
	}
	p := c.plan(pp.ID)
	if p.Status != engine.PlanPending || p.Fingerprint == pp.Fingerprint {
		t.Errorf("sau plan_changed: %s, dấu vân tay đổi = %v", p.Status, p.Fingerprint != pp.Fingerprint)
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("plan_changed mà chạy lệnh thay đổi máy: %v", m)
	}
	if n := c.audits("plan.approve"); n != 0 {
		t.Errorf("chưa có job mà ghi audit: %d", n)
	}
}

func TestPlanNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "list"},
		{"plan", "show", "abc"},
		{"plan", "approve", "abc", "--yes"},
		{"plan", "reject", "abc"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") {
			t.Errorf("%q không root: mã %d, stderr %q", args, code, errs)
		}
		if c.dbExists() || len(c.fake.Ran()) != 0 {
			t.Errorf("%q không root mà vẫn mở DB hoặc chạy lệnh", args)
		}
	}
}

// Mật khẩu quản trị chỉ có trong --json của approve khi Plan vừa cài WordPress.
func TestPlanApproveJSONPassword(t *testing.T) {
	const pw = "Abcdefghijklmnopqrstuvwxyz012345"
	res := core.RunResult{WPInstalled: true, AdminUser: "admin", AdminPassword: system.NewSecret(pw)}
	pp := engine.PendingPlan{ID: "p1", Request: json.RawMessage(`{"kind":"site.add","domain":"shop.example.com"}`)}
	b, _ := json.Marshal(planApproveJSONOf(pp, res))
	if strings.Count(string(b), pw) != 1 {
		t.Errorf("--json phải có mật khẩu đúng một lần: %s", b)
	}
	var out strings.Builder
	printPlanAdminPassword(&env{stdout: &out, lang: "vi"}, pp, res)
	if strings.Count(out.String(), pw) != 1 || !strings.Contains(out.String(), "http://shop.example.com/wp-admin/") {
		t.Errorf("in mật khẩu:\n%s", out.String())
	}

	res.WPInstalled = false
	b, _ = json.Marshal(planApproveJSONOf(pp, res))
	out.Reset()
	printPlanAdminPassword(&env{stdout: &out, lang: "vi"}, pp, res)
	if strings.Contains(string(b), pw) || out.Len() != 0 {
		t.Errorf("không cài WordPress mà lộ mật khẩu: %s %q", b, out.String())
	}
}
