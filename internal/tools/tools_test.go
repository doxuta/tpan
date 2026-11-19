package tools

import (
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// secretsOf là mọi chuỗi thật không được tới tay agent.
func secretsOf(t *testing.T) []string {
	out := []string{hostname, clientIP, clientEmail, dbPassword, authSalt, nonceSalt, domU}
	for _, s := range []string{domA, domU} {
		d := mustDomain(t, s)
		slug := validate.SlugOf(d).String()
		out = append(out, d.String(), slug, "tp_"+slug)
	}
	return out
}

func assertNoLeak(t *testing.T, where, text string, secrets []string) {
	t.Helper()
	low := strings.ToLower(text)
	for _, s := range secrets {
		if strings.Contains(low, strings.ToLower(s)) {
			t.Errorf("%s lộ %q:\n%s", where, s, text)
		}
	}
}

// 1. Gọi mọi công cụ (cả gọi lỗi), không chuỗi thật nào ra tới agent hay vào ai_calls.
func TestCatalogRedactsEverything(t *testing.T) {
	e := newEnv(t, core.ApprovalClient)
	s := e.open(t, "")
	secrets := secretsOf(t)
	a, u := alias(s, mustDomain(t, domA).String()), alias(s, mustDomain(t, domU).String())
	if a == domA || u == mustDomain(t, domU).String() {
		t.Fatalf("bộ che chưa biết các site: %s, %s", a, u)
	}

	seen := map[string]bool{}
	var texts []string
	do := func(name, args string, wantOK bool) json.RawMessage {
		t.Helper()
		seen[name] = true
		out, err := s.Call(ctx, name, json.RawMessage(args))
		if wantOK && err != nil {
			t.Fatalf("%s %s: %v", name, args, err)
		}
		if !wantOK && err == nil {
			t.Fatalf("%s %s: muốn lỗi, có %s", name, args, out)
		}
		if err != nil {
			texts = append(texts, name+" lỗi: "+err.Error())
		} else {
			texts = append(texts, name+": "+string(out))
		}
		return out
	}

	do("list_sites", `{}`, true)
	do("site_health", `{"domain":"`+a+`"}`, true)
	do("site_health", `{"domain":"`+u+`"}`, true)
	do("site_health", `{"domain":"nothere.example.org"}`, false)
	do("stack_status", `{}`, true)
	do("machine", `{}`, true)
	for _, l := range []string{"access", "error", "php-error"} {
		do("tail_log", `{"domain":"`+u+`","log":"`+l+`","lines":5}`, true)
	}
	do("tail_log", `{"domain":"`+a+`","log":"access"}`, false) // site A không có file log
	do("tail_log", `{"domain":"`+u+`","log":"../../etc/shadow"}`, false)
	do("managed_file", `{"path":"`+strings.Replace(e.wpConfig, mustDomain(t, domU).String(), u, 1)+`"}`, true)
	do("managed_file", `{"path":"/var/www/`+domA+`/public/index.php"}`, false) // agent gõ tên thật
	do("list_jobs", `{"limit":5}`, true)
	do("get_job", `{"id":"`+seededJobID+`"}`, true)
	do("get_job", `{"id":"ffffffffffffffff"}`, false)
	do("propose_site_add", `{"domain":"new-site.example.com","email":"`+clientEmail+`","title":"Shop of `+domA+`"}`, true)
	do("propose_site_add", `{"domain":"`+a+`","email":"not-an-email"}`, false)
	do("propose_site_repair", `{"domain":"`+a+`"}`, true)
	do("propose_stack_repair", `{}`, true)
	e.recordFail(t, domA, "web.vhost:"+mustDomain(t, domA).String())
	do("propose_health_fix", `{"check_id":"web.vhost:`+a+`"}`, true)
	do("propose_health_fix", `{"check_id":"web.vhost:nothere.example.org"}`, false)
	// Chưa đặt email Let's Encrypt: bị từ chối, nhưng lời từ chối vẫn phải đã che.
	do("propose_ssl_issue", `{"domain":"`+a+`"}`, false)
	// M9: một bản backup của site có dấu; tạo và khôi phục trên máy giả bị từ
	// chối (không đo được, không có bản đó) nhưng lời lỗi vẫn phải đã che.
	if _, err := e.st.AddBackup(ctx, core.Backup{Domain: mustDomain(t, domU).String(), Kind: core.BackupFull, Reason: core.ReasonManual,
		Path: "/var/lib/tpan/backups/" + mustDomain(t, domU).String() + "/x.tar.gz", Size: 10, Created: e.now}); err != nil {
		t.Fatal(err)
	}
	do("list_backups", `{}`, true)
	do("list_backups", `{"domain":"`+u+`"}`, true)
	do("propose_backup_create", `{"domain":"`+a+`"}`, false)
	do("propose_backup_restore", `{"domain":"`+u+`","backup_id":"ffffffffffffffff"}`, false)
	// current_view: đường dẫn và nhãn chứa domain ở mọi dạng giao diện có thể
	// báo (punycode, %xx của dạng có dấu, dạng có dấu) và tên máy.
	pu := mustDomain(t, domU).String()
	for _, v := range []store.AIView{
		{Path: "/sites/" + pu, Label: "Site " + pu},
		{Path: "/sites/" + url.PathEscape(domU), Label: "Site " + domU},
		{Path: "/stack", Label: "Stack " + hostname},
	} {
		if err := e.st.AddAIView(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	cv := do("current_view", `{}`, true)
	if strings.Contains(strings.ToLower(string(cv)), "%c3") {
		t.Errorf("current_view còn domain có dấu dạng %%xx: %s", cv)
	}
	rm := do("propose_site_remove", `{"domain":"`+u+`"}`, true)
	rmID := field[string](t, rm, "plan_id")
	do("list_plans", `{"status":"pending"}`, true)
	do("list_plans", `{"status":"bogus"}`, false)
	do("get_plan", `{"id":"`+rmID+`"}`, true)
	do("get_plan", `{"id":"0000000000000000"}`, false)
	ap := do("apply_plan", `{"plan_id":"`+rmID+`"}`, true)
	do("apply_plan", `{"plan_id":"0000000000000000"}`, false)
	if job := field[string](t, ap, "job_id"); job != "" {
		do("get_job", `{"id":"`+job+`"}`, true)
	}
	do("run_shell", `{"cmd":"cat `+e.wpConfig+`"}`, false)

	for _, tool := range Catalog() {
		if !seen[tool.Name] {
			t.Errorf("test chưa gọi công cụ %s", tool.Name)
		}
	}
	for _, x := range texts {
		assertNoLeak(t, "kết quả", x, secrets)
	}
	calls, err := e.st.AICalls(ctx, s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != len(texts) {
		t.Fatalf("ai_calls có %d dòng, đã gọi %d lần", len(calls), len(texts))
	}
	for _, c := range calls {
		assertNoLeak(t, "ai_calls#"+c.Tool, c.Tool+" "+string(c.Input)+" "+string(c.Output)+" "+c.Error, secrets)
	}
}

// readOnlyCmd: lệnh chỉ hỏi trạng thái máy.
func readOnlyCmd(c system.Cmd) bool {
	switch c.Name {
	case "tail", "getent", "test", "dpkg-query", "a2query", "ss":
		return true
	case "mariadb":
		return strings.HasPrefix(strings.TrimSpace(string(c.Stdin)), "SELECT")
	case "systemctl":
		return len(c.Args) > 0 && (c.Args[0] == "is-active" || c.Args[0] == "is-enabled")
	case "wp":
		a := strings.Join(c.Args, " ")
		return strings.HasPrefix(a, "core is-installed") || strings.HasPrefix(a, "option get")
	}
	return false
}

// 2. Log có cài câu lệnh: AI thấy nó trong thẻ untrusted, Plan mang cảnh báo,
// và máy không đổi gì khi chưa có người duyệt.
func TestPromptInjectionInLog(t *testing.T) {
	e := newEnv(t, "")
	s := e.open(t, "")
	u := alias(s, mustDomain(t, domU).String())
	before := e.f.Snapshot()
	e.f.ResetRan()

	out := callOK(t, s, "tail_log", `{"domain":"`+u+`","log":"access"}`)
	text := field[string](t, out, "text")
	if !strings.HasPrefix(text, "<untrusted") || !strings.HasSuffix(text, "</untrusted>") || !strings.Contains(text, injection) {
		t.Fatalf("log không nằm trong thẻ untrusted:\n%s", text)
	}
	if n := strings.Count(text, "</untrusted>"); n != 1 {
		t.Errorf("thẻ đóng cài sẵn trong log không bị vô hiệu hoá (%d thẻ đóng):\n%s", n, text)
	}
	rec, err := e.st.AISession(ctx, s.ID())
	if err != nil || !rec.Untrusted {
		t.Fatalf("phiên chưa bị đánh dấu untrusted: %+v %v", rec, err)
	}

	rm := callOK(t, s, "propose_site_remove", `{"domain":"`+u+`"}`)
	id := field[string](t, rm, "plan_id")
	pp, err := e.st.Plan(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if pp.Status != engine.PlanPending || !pp.Untrusted || pp.Origin != engine.OriginMCP || pp.SessionID != s.ID() {
		t.Errorf("Plan: status %s, untrusted %v, origin %s, session %s", pp.Status, pp.Untrusted, pp.Origin, pp.SessionID)
	}
	if !strings.Contains(field[string](t, rm, "note"), "Nothing on the server has changed") {
		t.Errorf("kết quả lập Plan không nói rõ máy chưa đổi gì: %s", rm)
	}
	if err := callErr(t, s, "apply_plan", `{"plan_id":"`+id+`"}`); err.Code != "approval_required" {
		t.Errorf("apply_plan ở chế độ panel: %v", err)
	}
	if pp, _ := e.st.Plan(ctx, id); pp.Status != engine.PlanPending {
		t.Errorf("Plan không còn pending: %s", pp.Status)
	}

	if after := e.f.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Error("hệ thống file giả đã đổi dù chưa có ai duyệt")
	}
	for _, c := range e.f.Ran() {
		if !readOnlyCmd(c) {
			t.Errorf("lệnh có thể đổi máy đã chạy: %s", c.String())
		}
	}
}

// 3. Danh mục đóng.
func TestCatalogClosed(t *testing.T) {
	want := []string{
		"apply_plan", "current_view", "get_job", "get_plan", "list_backups", "list_jobs", "list_plans", "list_sites", "machine", "managed_file",
		"propose_backup_create", "propose_backup_restore", "propose_health_fix", "propose_site_add", "propose_site_remove", "propose_site_repair", "propose_ssl_issue", "propose_stack_repair",
		"site_health", "stack_status", "tail_log",
	}
	var got []string
	for _, tool := range Catalog() {
		got = append(got, tool.Name)
		var sc struct {
			Type                 string          `json:"type"`
			AdditionalProperties json.RawMessage `json:"additionalProperties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &sc); err != nil || sc.Type != "object" || string(sc.AdditionalProperties) != "false" {
			t.Errorf("%s: schema %s (%v)", tool.Name, tool.InputSchema, err)
		}
		if tool.Description == "" || tool.Kind == "" || tool.run == nil {
			t.Errorf("%s thiếu mô tả, loại hoặc hàm chạy", tool.Name)
		}
		if tool.Kind == KindPlan && !strings.Contains(tool.Description, "ONLY creates a pending plan") {
			t.Errorf("%s: mô tả không nói rõ chỉ tạo Plan chờ duyệt", tool.Name)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("danh mục:\n có  %v\n muốn %v", got, want)
	}

	e := newEnv(t, core.ApprovalClient)
	s := e.open(t, "")
	a := alias(s, domA)
	e.f.ResetRan()
	for _, name := range []string{"run_shell", "write_file", "List_Sites", "list_sites ", ""} {
		if err := callErr(t, s, name, `{}`); err.Code != "tool_unknown" {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, c := range []struct{ name, args string }{
		{"list_sites", `{"x":1}`},
		{"site_health", `{"domain":"` + a + `","command":"id"}`},
		{"stack_status", `{} {"x":1}`},
		{"tail_log", `{"domain":"` + a + `","log":"access","lines":"5; id"}`},
		{"list_jobs", `[1]`},
		{"site_health", `{"domain":`},
	} {
		if err := callErr(t, s, c.name, c.args); err.Code != "invalid_arguments" {
			t.Errorf("%s %s: %v", c.name, c.args, err)
		}
	}
	attacks := []string{"a.com; rm -rf /", "$(id).example.com", "../../etc/passwd", "a.com\nb.com", "`id`.example.com", strings.Repeat("a", 300) + ".com", ""}
	for _, d := range attacks {
		dj, _ := json.Marshal(d)
		for _, name := range []string{"site_health", "propose_site_remove", "propose_site_repair", "propose_backup_create"} {
			if err := callErr(t, s, name, `{"domain":`+string(dj)+`}`); err.Code != validate.CodeDomainInvalid {
				t.Errorf("%s %q: %v", name, d, err)
			}
		}
		if err := callErr(t, s, "tail_log", `{"domain":`+string(dj)+`,"log":"access"}`); err.Code != validate.CodeDomainInvalid {
			t.Errorf("tail_log %q: %v", d, err)
		}
		if err := callErr(t, s, "propose_site_add", `{"domain":`+string(dj)+`,"email":"admin@example.com"}`); err.Code != validate.CodeDomainInvalid {
			t.Errorf("propose_site_add %q: %v", d, err)
		}
	}
	if ran := e.f.Ran(); len(ran) != 0 {
		t.Errorf("lời gọi bị từ chối vẫn chạy lệnh: %v", ran)
	}
	if ps, _ := e.st.Plans(ctx, "", 0); len(ps) != 0 {
		t.Errorf("lời gọi bị từ chối vẫn tạo %d Plan", len(ps))
	}
}

// 4. apply_plan ở chế độ client: chỉ Plan của chính phiên trực tiếp này.
func TestApplyPlanClientMode(t *testing.T) {
	e := newEnv(t, core.ApprovalClient)
	s := e.open(t, "")
	other := e.open(t, "")

	// Plan của phiên khác.
	op := callOK(t, other, "propose_site_repair", `{"domain":"`+alias(other, domA)+`"}`)
	if err := callErr(t, s, "apply_plan", `{"plan_id":"`+field[string](t, op, "plan_id")+`"}`); err.Code != "approval_required" {
		t.Errorf("Plan của phiên khác: %v", err)
	}
	// Plan người đề xuất (origin human).
	hp, err := s.planner().Propose(ctx, core.PlanRequest{Kind: core.PlanSiteRepair, Domain: mustDomain(t, domA).String()}, core.Proposal{Origin: engine.OriginHuman})
	if err != nil {
		t.Fatal(err)
	}
	if err := callErr(t, s, "apply_plan", `{"plan_id":"`+hp.ID+`"}`); err.Code != "approval_required" {
		t.Errorf("Plan của người: %v", err)
	}
	// Plan origin khác mà lại mang đúng session id (dòng bị sửa tay): vẫn từ chối.
	ap2, err := s.planner().Propose(ctx, core.PlanRequest{Kind: core.PlanSiteRepair, Domain: mustDomain(t, domA).String()}, core.Proposal{Origin: engine.OriginAI, SessionID: s.ID()})
	if err != nil {
		t.Fatal(err)
	}
	if err := callErr(t, s, "apply_plan", `{"plan_id":"`+ap2.ID+`"}`); err.Code != "approval_required" {
		t.Errorf("Plan origin ai: %v", err)
	}
	for _, id := range []string{field[string](t, op, "plan_id"), hp.ID, ap2.ID} {
		if pp, _ := e.st.Plan(ctx, id); pp.Status != engine.PlanPending {
			t.Errorf("Plan %s bị đổi trạng thái thành %s", id, pp.Status)
		}
	}

	// Plan tạo site của chính phiên vẫn phải qua người: mật khẩu quản trị chỉ hiện cho người.
	add := callOK(t, s, "propose_site_add", `{"domain":"another.example.com","email":"admin@example.com"}`)
	if err := callErr(t, s, "apply_plan", `{"plan_id":"`+field[string](t, add, "plan_id")+`"}`); err.Code != "approval_required" {
		t.Errorf("apply_plan cho Plan tạo site: %v", err)
	}

	// Plan của chính phiên: chạy.
	own := callOK(t, s, "propose_site_remove", `{"domain":"`+alias(s, mustDomain(t, domU).String())+`"}`)
	id := field[string](t, own, "plan_id")
	out := callOK(t, s, "apply_plan", `{"plan_id":"`+id+`"}`)
	jobID := field[string](t, out, "job_id")
	pp, err := e.st.Plan(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if jobID == "" || pp.Status != engine.PlanApplied || pp.DecidedBy != "mcp:"+s.ID() || pp.JobID != jobID {
		t.Errorf("apply_plan: job %q, Plan %s, by %q, job trong Plan %q", jobID, pp.Status, pp.DecidedBy, pp.JobID)
	}
	if strings.Contains(strings.ToLower(string(out)), "admin_password") {
		t.Errorf("kết quả apply_plan có trường mật khẩu: %s", out)
	}
	calls, _ := e.st.AICalls(ctx, s.ID())
	if last := calls[len(calls)-1]; last.Tool != "apply_plan" || last.PlanID != id {
		t.Errorf("ai_calls không ghi Plan đã chạy: %+v", last)
	}
	// Chạy lại: Plan không còn chờ.
	if err := callErr(t, s, "apply_plan", `{"plan_id":"`+id+`"}`); err.Code != "plan_not_pending" {
		t.Errorf("chạy lại: %v", err)
	}

	// Phiên nối bằng --session: luôn chế độ panel, kể cả Plan của chính nó.
	e.panelSession(t, "panel00000000001")
	ps := e.open(t, "panel00000000001")
	pr := callOK(t, ps, "propose_site_repair", `{"domain":"`+alias(ps, domA)+`"}`)
	pid := field[string](t, pr, "plan_id")
	if pp, _ := e.st.Plan(ctx, pid); pp.Origin != engine.OriginAI || pp.SessionID != ps.ID() {
		t.Errorf("Plan của phiên panel: origin %s, session %s", pp.Origin, pp.SessionID)
	}
	if err := callErr(t, ps, "apply_plan", `{"plan_id":"`+pid+`"}`); err.Code != "approval_required" {
		t.Errorf("phiên --session: %v", err)
	}

	// --session chỉ nối vào phiên panel có thật.
	for _, id := range []string{s.ID(), "khongco"} {
		if _, err := OpenSession(ctx, e.deps, SessionOptions{ID: id}); err == nil {
			t.Errorf("OpenSession(%q) không bị từ chối", id)
		}
	}
}

// 5. Bí danh ổn định qua hai tiến trình nối cùng phiên panel; đối số dùng bí
// danh được đổi về tên thật.
func TestAliasesStableAcrossProcesses(t *testing.T) {
	e := newEnv(t, "")
	e.panelSession(t, "panel00000000002")
	var lists []string
	for range 2 {
		s := e.open(t, "panel00000000002")
		lists = append(lists, string(callOK(t, s, "list_sites", `{}`)))
	}
	if lists[0] != lists[1] {
		t.Fatalf("bí danh đổi giữa hai lần mở:\n%s\n%s", lists[0], lists[1])
	}
	var rows []siteRow
	if err := json.Unmarshal([]byte(lists[0]), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Domain != "site-a.example" || rows[1].Domain != "site-b.example" {
		t.Fatalf("list_sites: %s", lists[0])
	}

	s := e.open(t, "panel00000000002")
	e.f.ResetRan()
	out := callOK(t, s, "tail_log", `{"domain":"site-b.example","log":"error","lines":1}`)
	if d := field[string](t, out, "domain"); d != "site-b.example" {
		t.Errorf("tail_log trả domain %q", d)
	}
	ran := e.f.Ran()
	realLog := site.LayoutOf(mustDomain(t, domU)).LogDir + "/error.log"
	if len(ran) != 1 || ran[0].Name != "tail" || ran[0].Args[len(ran[0].Args)-1] != realLog {
		t.Errorf("tail không chạy trên đường dẫn thật %s: %v", realLog, ran)
	}

	calls, err := e.st.AICalls(ctx, "panel00000000002")
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range calls {
		if c.Seq != i+1 {
			t.Errorf("seq lần gọi %d là %d", i+1, c.Seq)
		}
	}
	if len(calls) != 3 {
		t.Errorf("ai_calls có %d dòng, muốn 3", len(calls))
	}
	rec, _ := e.st.AISession(ctx, "panel00000000002")
	if rec.Aliases["site-a.example"] != domA {
		t.Errorf("bảng bí danh đã lưu: %v", rec.Aliases)
	}
}

func TestSetClient(t *testing.T) {
	e := newEnv(t, "")
	s := e.open(t, "")
	if err := s.SetClient(ctx, "claude-code 2.1"); err != nil {
		t.Fatal(err)
	}
	if rec, err := e.st.AISession(ctx, s.ID()); err != nil || rec.Client != "claude-code 2.1" || rec.Entry != "mcp" {
		t.Errorf("phiên sau SetClient: %+v %v", rec, err)
	}
}
