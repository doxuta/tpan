package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/monitor"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func hRes(id string, stage checks.Stage, status checks.Status) checks.Result {
	r := checks.Result{ID: id, Stage: stage, Status: status, Title: i18n.T("kiểm "+id, "check "+id)}
	if status != checks.Pass {
		r.AutoFix = core.AutoFixName(r)
	}
	return r
}

func (e *apiEnv) record(t *testing.T, target string, rs ...checks.Result) {
	t.Helper()
	if _, err := e.st.RecordResults(context.Background(), target, rs, e.clock()); err != nil {
		t.Fatal(err)
	}
}

// hAccessLine là một dòng access.log "combined" lúc at với mã status.
func hAccessLine(at time.Time, status string) string {
	return `203.0.113.5 - - [` + at.Format("02/Jan/2006:15:04:05 -0700") + `] "GET / HTTP/1.1" ` + status + ` 512 "-" "test"`
}

// hAudit đọc nhật ký bằng một kết nối riêng (store không có hàm đọc audit).
func hAudit(t *testing.T, e *apiEnv, action string) []store.AuditEntry {
	t.Helper()
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT action, target, result, job_id FROM audit WHERE action = ? ORDER BY rowid", action)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.AuditEntry
	for rows.Next() {
		var a store.AuditEntry
		if err := rows.Scan(&a.Action, &a.Target, &a.Result, &a.JobID); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// ---------------------------------------------------------------- tổng quan

type hOverviewJSON struct {
	Sites []struct {
		Site    apiSiteJSON      `json:"site"`
		Checks  []checks.Result  `json:"checks"`
		Traffic *monitor.Traffic `json:"traffic"`
	} `json:"sites"`
	StackChecks []checks.Result  `json:"stack_checks"`
	TPANChecks  []checks.Result  `json:"tpan_checks"`
	Incidents   []store.Incident `json:"incidents"`
	LastRun     time.Time        `json:"last_run"`
}

func hIDs(rs []checks.Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func TestHealthOverviewFromStore(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	e.addSite(t, "b.example.com")
	ctx := context.Background()
	// Ghi lộn thứ tự: tổng quan phải xếp lại theo chặng rồi theo id.
	e.record(t, "", hRes("memory:ram", checks.StageMachine, checks.Pass), hRes("service.active:nginx", checks.StageWeb, checks.Pass))
	e.record(t, "a.example.com",
		hRes("http.site:a.example.com", checks.StageEndToEnd, checks.Fail),
		hRes("php.socket:a.example.com", checks.StagePHP, checks.Fail),
		hRes("web.vhost:a.example.com", checks.StageWeb, checks.Pass),
		hRes("db.access:a.example.com", checks.StageDB, checks.Pass),
		hRes("dns.a:a.example.com", checks.StageDNS, checks.Pass))
	e.record(t, store.TargetTPAN, hRes("tpan.db", checks.StageTPAN, checks.Pass))
	if err := e.st.SetSetting(ctx, health.SettingLastRun, "2026-11-19T07:55:00Z"); err != nil {
		t.Fatal(err)
	}
	now := e.clock()
	e.f.Handle([]string{"tail"}, func(c system.Cmd) system.Result {
		switch c.Args[len(c.Args)-1] {
		case "/var/log/tpan/sites/a.example.com/access.log":
			return system.Result{Stdout: strings.Join([]string{
				hAccessLine(now.Add(-2*time.Hour), "200"), // ngoài một giờ: không đếm
				hAccessLine(now.Add(-10*time.Minute), "200"),
				hAccessLine(now.Add(-5*time.Minute), "502"),
			}, "\n") + "\n"}
		}
		return system.Result{ExitCode: 2, Stderr: "tail: lỗi đọc\n"}
	})

	ov := apiDecode[hOverviewJSON](t, apiCall(e.s.handleOverview, "GET", "/", ""), 200)
	if got := hIDs(ov.StackChecks); !slices.Equal(got, []string{"service.active:nginx", "memory:ram"}) {
		t.Errorf("stack_checks = %v", got)
	}
	want := []string{"dns.a:a.example.com", "web.vhost:a.example.com", "php.socket:a.example.com", "db.access:a.example.com", "http.site:a.example.com"}
	if got := hIDs(ov.Sites[0].Checks); !slices.Equal(got, want) {
		t.Errorf("checks của a = %v; muốn %v", got, want)
	}
	if len(ov.Sites[1].Checks) != 0 || ov.Sites[1].Traffic != nil {
		t.Errorf("site b: checks %v, traffic %+v (tail lỗi thì traffic phải nil)", ov.Sites[1].Checks, ov.Sites[1].Traffic)
	}
	if tr := ov.Sites[0].Traffic; tr == nil || tr.Requests != 2 || tr.Errors5xx != 1 {
		t.Errorf("traffic của a = %+v", tr)
	}
	if got := hIDs(ov.TPANChecks); !slices.Equal(got, []string{"tpan.db"}) {
		t.Errorf("tpan_checks = %v", got)
	}
	if len(ov.Incidents) != 2 || ov.Incidents[0].Target != "a.example.com" {
		t.Errorf("incidents = %+v", ov.Incidents)
	}
	if !ov.LastRun.Equal(time.Date(2026, 11, 19, 7, 55, 0, 0, time.UTC)) {
		t.Errorf("last_run = %v", ov.LastRun)
	}
	for _, c := range e.f.Ran() {
		if c.Name != "tail" {
			t.Errorf("tổng quan chạy lệnh %s", c.String())
		}
	}
}

// ---------------------------------------------------------------- sự cố

func TestHealthIncidents(t *testing.T) {
	e := apiNewEnv(t)
	ctx := context.Background()
	e.record(t, "", hRes("service.active:nginx", checks.StageWeb, checks.Fail))
	e.advance(time.Minute)
	e.record(t, "", hRes("disk.usage:/", checks.StageMachine, checks.Fail))
	// Một job thật và một job đã bị dọn khỏi lịch sử, gắn vào sự cố đầu.
	rep, err := engine.New(e.f, engine.Options{Journal: e.st}).Execute(ctx, engine.Plan{Kind: "fix.service.start", Target: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range []string{"da-don", rep.Job.ID} {
		if err := e.st.LinkIncidentJob(ctx, "service.active:nginx", j, e.clock()); err != nil {
			t.Fatal(err)
		}
		e.advance(time.Second)
	}
	e.record(t, "", hRes("service.active:nginx", checks.StageWeb, checks.Pass)) // đóng sự cố đầu

	list := func(q string) []store.Incident {
		return apiDecode[[]store.Incident](t, apiCall(e.s.handleIncidents, "GET", "/api/v1/incidents"+q, ""), 200)
	}
	if got := list(""); len(got) != 1 || got[0].CheckID != "disk.usage:/" {
		t.Errorf("mặc định (open) = %+v", got)
	}
	all := list("?state=all")
	if len(all) != 2 || all[0].CheckID != "disk.usage:/" || all[1].CheckID != "service.active:nginx" {
		t.Errorf("all = %+v", all)
	}
	if got := list("?state=all&limit=1"); len(got) != 1 {
		t.Errorf("limit=1 trả %d", len(got))
	}
	for q, code := range map[string]string{"?state=x": "bad_request", "?limit=0": "limit_invalid", "?limit=501": "limit_invalid", "?limit=a": "limit_invalid"} {
		if got := apiErrCode(t, apiCall(e.s.handleIncidents, "GET", "/"+q, ""), 400); got != code {
			t.Errorf("%s: code = %q, muốn %q", q, got, code)
		}
	}

	closed := all[1]
	d := apiDecode[IncidentDetail](t, apiCall(e.s.handleIncident, "GET", "/", "", "id", jsonID(closed.ID)), 200)
	if d.Incident.ID != closed.ID || d.Incident.Closed.IsZero() || len(d.Incident.Jobs) != 2 {
		t.Errorf("sự cố = %+v", d.Incident)
	}
	if len(d.Jobs) != 1 || d.Jobs[0].ID != rep.Job.ID {
		t.Errorf("jobs = %+v (job đã dọn phải bị bỏ qua)", d.Jobs)
	}
	if d.Current == nil || d.Current.Status != checks.Pass {
		t.Errorf("current = %+v", d.Current)
	}
	for _, bad := range []string{"0", "-1", "abc", "999", "1;id"} {
		if got := apiErrCode(t, apiCall(e.s.handleIncident, "GET", "/", "", "id", bad), 404); got != "incident_not_found" {
			t.Errorf("id %q: code = %q", bad, got)
		}
	}
}

func jsonID(id int64) string {
	b, _ := json.Marshal(id)
	return string(b)
}

// ---------------------------------------------------------------- sửa tự động

func TestHealthFixErrors(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.record(t, "", hRes("disk.usage:/", checks.StageMachine, checks.Fail),
		hRes("service.active:nginx", checks.StageWeb, checks.Fail))
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"không phải JSON", `{`, "bad_request", 400},
		{"trường lạ", `{"check_id":"x","auto_fix":"service.start"}`, "bad_request", 400},
		{"không có kết quả", `{"check_id":"service.active:x; rm -rf /"}`, "check_not_found", 404},
		{"không có cách sửa", `{"check_id":"disk.usage:/"}`, "no_auto_fix", 409},
	}
	for _, c := range cases {
		for _, q := range []string{"", "?dry_run=1"} {
			if got := apiErrCode(t, apiCall(e.s.handleFix, "POST", "/api/v1/fixes"+q, c.body), c.status); got != c.code {
				t.Errorf("%s%s: code = %q, muốn %q", c.name, q, got, c.code)
			}
		}
	}
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if got := apiErrCode(t, apiCall(e.s.handleFix, "POST", "/", `{"check_id":"service.active:nginx"}`), 409); got != "job_running" {
		t.Errorf("đang có job: code = %q", got)
	}
	e.s.api.release()
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("lỗi mà vẫn có job: %v", jobs)
	}
}

// Sửa tự động chạy thật: xem trước không đổi gì; chạy thật trả 202, job gắn
// vào sự cố, có nhật ký, và bộ hẹn giờ được xin kiểm lại sau khi job xong.
func TestHealthFixRuns(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	ctx := context.Background()
	e.record(t, "", hRes("service.active:nginx", checks.StageWeb, checks.Fail))

	p := apiDecode[engine.Preview](t, apiCall(e.s.handleFix, "POST", "/?dry_run=1", `{"check_id":"service.active:nginx"}`), 200)
	if p.Kind != "fix."+core.FixServiceStart || p.Target != "nginx" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	if jobs, _ := e.st.Jobs(ctx, 0); len(jobs) != 0 {
		t.Fatalf("xem trước mà có job: %v", jobs)
	}

	// Bộ hẹn giờ thật, nhịp dài: chỉ chạy lần đầu và các lần Kick.
	reports := make(chan health.Report, 4)
	e.s.sched = &health.Scheduler{
		Runner: e.s.healthRunner(), Busy: e.s.healthBusy,
		LightEvery: time.Hour, FullEvery: time.Hour,
		OnReport: func(r health.Report) { reports <- r },
	}
	sctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); e.s.sched.Run(sctx) }()
	t.Cleanup(func() { cancel(); <-done })
	waitReport := func(what string) {
		t.Helper()
		select {
		case <-reports:
		case <-time.After(10 * time.Second):
			t.Fatalf("không có vòng kiểm %s", what)
		}
	}
	waitReport("đầu tiên")
	// Vòng đầu chạy trên máy giả có thể đã đổi kết quả: ghi lại Fail để chắc
	// còn sự cố mở cho job gắn vào.
	e.record(t, "", hRes("service.active:nginx", checks.StageWeb, checks.Fail))

	js := apiDecode[JobStarted](t, apiCall(e.s.handleFix, "POST", "/", `{"check_id":"service.active:nginx"}`), 202)
	e.s.api.wg.Wait()
	waitReport("sau job sửa")

	incs, err := e.st.Incidents(ctx, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	var linked bool
	for _, inc := range incs {
		if inc.CheckID == "service.active:nginx" && slices.Contains(inc.Jobs, js.JobID) {
			linked = true
		}
	}
	if !linked {
		t.Errorf("job %s không gắn vào sự cố: %+v", js.JobID, incs)
	}
	job, err := e.st.Job(ctx, js.JobID)
	if err != nil || job.Kind != "fix."+core.FixServiceStart {
		t.Errorf("job = %+v, %v", job, err)
	}
	a := hAudit(t, e, "fix."+core.FixServiceStart)
	if len(a) != 1 || a[0].Target != "service.active:nginx" || a[0].JobID != js.JobID {
		t.Errorf("audit = %+v", a)
	}
	if !e.s.api.claim() {
		t.Fatal("job xong mà chưa nhả chỗ chạy job")
	}
	e.s.api.release()
}

// ---------------------------------------------------------------- nhận bản sửa tay

func TestHealthAcceptFile(t *testing.T) {
	e := apiNewEnv(t)
	ctx := context.Background()
	const path = "/etc/nginx/sites-available/tpan-a.example.com.conf"
	if err := e.st.RecordFile(ctx, path, "cu", "job-cu"); err != nil {
		t.Fatal(err)
	}
	content := []byte("server { listen 80; } # sửa tay\n")
	e.f.SetFile(path, content, 0o644, system.Root)
	e.f.SetFile("/etc/passwd", []byte("root:x:0:0\n"), 0o644, system.Root)

	for _, bad := range []string{"", "/etc/passwd", "/etc/nginx/sites-available/../../passwd", path + "\n", "relative/path"} {
		body, _ := json.Marshal(AcceptFileRequest{Path: bad})
		if got := apiErrCode(t, apiCall(e.s.handleAcceptFile, "POST", "/", string(body)), 400); got != "path_invalid" {
			t.Errorf("%q: code = %q", bad, got)
		}
	}
	if got := apiErrCode(t, apiCall(e.s.handleAcceptFile, "POST", "/", `{"path":"`+path+`","x":1}`), 400); got != "bad_request" {
		t.Errorf("trường lạ: code = %q", got)
	}

	// Đang có job (trong tiến trình, hoặc khoá bị tiến trình khác giữ): từ chối.
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if got := apiErrCode(t, apiCall(e.s.handleAcceptFile, "POST", "/", `{"path":"`+path+`"}`), 409); got != "job_running" {
		t.Errorf("đang có job: code = %q", got)
	}
	e.s.api.release()
	unlock, err := e.f.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := apiErrCode(t, apiCall(e.s.handleAcceptFile, "POST", "/", `{"path":"`+path+`"}`), 409); got != "job_running" {
		t.Errorf("khoá bị giữ: code = %q", got)
	}
	unlock()

	rec := apiCall(e.s.handleAcceptFile, "POST", "/", `{"path":"`+path+`"}`)
	if rec.Code != 204 {
		t.Fatalf("mã = %d: %s", rec.Code, rec.Body.String())
	}
	files, _ := e.st.ManagedFiles(ctx)
	sum := sha256.Sum256(content)
	if len(files) != 1 || files[0].SHA256 != hex.EncodeToString(sum[:]) || files[0].JobID != "manual" {
		t.Errorf("managed_files = %+v", files)
	}
	if a := hAudit(t, e, "file.accept"); len(a) != 3 || a[2].Result != "ok" || a[2].Target != path {
		t.Errorf("audit = %+v", a)
	}
	// Đã nhả chỗ chạy job và khoá.
	if !e.s.api.claim() {
		t.Fatal("chưa nhả chỗ chạy job")
	}
	e.s.api.release()
	if e.s.healthBusy() {
		t.Fatal("chưa nhả khoá toàn cục")
	}
}

// ---------------------------------------------------------------- chạy ngay

func TestHealthRunNow(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	if got := apiErrCode(t, apiCall(e.s.handleHealthRun, "POST", "/?domain=a.com%3Bid", ""), 400); got != validate.CodeDomainInvalid {
		t.Errorf("code = %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleHealthRun, "POST", "/?domain=none.example.com", ""), 404); got != "site_not_found" {
		t.Errorf("code = %q", got)
	}

	rep := apiDecode[health.Report](t, apiCall(e.s.handleHealthRun, "POST", "/?domain=A.example.com", ""), 200)
	if len(rep.Groups) != 1 || rep.Groups[0].Target != "a.example.com" || len(rep.Groups[0].Results) == 0 {
		t.Errorf("một site: groups = %+v", rep.Groups)
	}
	if _, ok, _ := e.st.GetSetting(context.Background(), health.SettingLastRun); ok {
		t.Error("chạy một site mà đã ghi last_run")
	}

	rec := apiCall(e.s.handleHealthRun, "POST", "/", "")
	rep = apiDecode[health.Report](t, rec, 200)
	targets := map[string]bool{}
	for _, g := range rep.Groups {
		targets[g.Target] = true
	}
	if !targets[""] || !targets["a.example.com"] || !targets[store.TargetTPAN] {
		t.Errorf("đầy đủ: targets = %v", targets)
	}
	if !strings.Contains(rec.Body.String(), `"opened":[`) || !strings.Contains(rec.Body.String(), `"closed":[]`) {
		t.Errorf("changes phải là mảng: %s", rec.Body.String())
	}
	if _, ok, _ := e.st.GetSetting(context.Background(), health.SettingLastRun); !ok {
		t.Error("chạy đầy đủ mà không ghi last_run")
	}
}

// ---------------------------------------------------------------- số request

func TestHealthSiteTraffic(t *testing.T) {
	e := apiNewEnv(t)
	e.addSite(t, "a.example.com")
	now := e.clock()
	e.f.Handle([]string{"tail"}, func(c system.Cmd) system.Result {
		return system.Result{Stdout: hAccessLine(now.Add(-time.Minute), "500") + "\n" + hAccessLine(now.Add(-time.Minute), "200") + "\n"}
	})
	tr := apiDecode[monitor.Traffic](t, apiCall(e.s.handleSiteTraffic, "GET", "/", "", "domain", "a.example.com"), 200)
	if tr.Requests != 2 || tr.Errors5xx != 1 || !tr.Since.Equal(now.Add(-time.Hour)) {
		t.Errorf("traffic = %+v", tr)
	}
	ran := e.f.Ran()
	if len(ran) != 1 || ran[0].Args[len(ran[0].Args)-1] != "/var/log/tpan/sites/a.example.com/access.log" {
		t.Errorf("lệnh = %v", ran)
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteTraffic, "GET", "/", "", "domain", "none.example.com"), 404); got != "site_not_found" {
		t.Errorf("code = %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteTraffic, "GET", "/", "", "domain", "../../etc/passwd"), 400); got != validate.CodeDomainInvalid {
		t.Errorf("code = %q", got)
	}
}

// ---------------------------------------------------------------- log trực tiếp

type hSSEEvent struct {
	event string
	data  map[string]string
}

func hParseSSE(t *testing.T, body string) []hSSEEvent {
	t.Helper()
	var out []hSSEEvent
	for block := range strings.SplitSeq(strings.TrimSpace(body), "\n\n") {
		var ev hSSEEvent
		for line := range strings.SplitSeq(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				ev.event = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				if err := json.Unmarshal([]byte(v), &ev.data); err != nil {
					t.Fatalf("data không phải JSON: %q", v)
				}
			}
		}
		out = append(out, ev)
	}
	return out
}

func TestHealthSiteLog(t *testing.T) {
	e := apiNewEnv(t)
	d := e.addSite(t, "a.example.com")
	l := site.LayoutOf(d)
	exit := 0
	e.f.Handle([]string{"tail"}, func(c system.Cmd) system.Result {
		if exit != 0 {
			return system.Result{ExitCode: exit, Stderr: "tail: cannot open '" + c.Args[len(c.Args)-1] + "'\n"}
		}
		// Dòng log là dữ liệu của khách lạ: có cả thẻ HTML và nháy kép.
		return system.Result{Stdout: "dòng 1 <script>alert(1)</script>\n\"GET / HTTP/1.1\" 200\n"}
	})

	for file, path := range map[string]string{"access": l.LogDir + "/access.log", "error": l.LogDir + "/error.log", "php": l.PHPErrorLog} {
		e.f.ResetRan()
		rec := apiCall(e.s.handleSiteLog, "GET", "/?file="+file, "", "domain", "a.example.com")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/event-stream" {
			t.Fatalf("%s: mã %d, %s", file, rec.Code, rec.Header().Get("Content-Type"))
		}
		evs := hParseSSE(t, rec.Body.String())
		if len(evs) != 3 || evs[0].event != "line" || evs[0].data["line"] != "dòng 1 <script>alert(1)</script>" ||
			evs[1].data["line"] != `"GET / HTTP/1.1" 200` || evs[2].event != "end" || evs[2].data["error"] != "" {
			t.Errorf("%s: sự kiện = %+v", file, evs)
		}
		ran := e.f.Ran()
		if len(ran) != 1 || ran[0].Name != "tail" || !slices.Equal(ran[0].Args, []string{"-n", "100", "-F", path}) {
			t.Errorf("%s: lệnh = %v", file, ran)
		}
	}

	// tail lỗi: dòng stderr vẫn tới, rồi "end" kèm lý do.
	exit = 1
	evs := hParseSSE(t, apiCall(e.s.handleSiteLog, "GET", "/?file=error", "", "domain", "a.example.com").Body.String())
	if len(evs) != 2 || evs[0].event != "line" || evs[1].event != "end" || !strings.Contains(evs[1].data["error"], "mã thoát 1") {
		t.Errorf("tail lỗi: sự kiện = %+v", evs)
	}

	// Client đã rời đi: không chạy được tail, không gửi "end".
	r := httptest.NewRequest("GET", "/?file=access", nil)
	r.SetPathValue("domain", "a.example.com")
	cctx, cancel := context.WithCancel(r.Context())
	cancel()
	rec := httptest.NewRecorder()
	e.s.handleSiteLog(rec, r.WithContext(cctx))
	if strings.Contains(rec.Body.String(), "event: end") {
		t.Errorf("client đã đi mà vẫn gửi end: %q", rec.Body.String())
	}

	for _, bad := range []string{"", "x", "../../etc/passwd", "access.log", "/etc/shadow"} {
		if got := apiErrCode(t, apiCall(e.s.handleSiteLog, "GET", "/?file="+bad, "", "domain", "a.example.com"), 400); got != "file_invalid" {
			t.Errorf("file=%q: code = %q", bad, got)
		}
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteLog, "GET", "/?file=access", "", "domain", "none.example.com"), 404); got != "site_not_found" {
		t.Errorf("code = %q", got)
	}
}

// ---------------------------------------------------------------- bận

// Bộ hẹn giờ bỏ vòng khi có job trong tiến trình hoặc khoá bị giữ ở nơi khác,
// và hỏi xong thì không giữ khoá.
func TestHealthBusy(t *testing.T) {
	e := apiNewEnv(t)
	if e.s.healthBusy() {
		t.Fatal("rảnh mà báo bận")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	unlock, err := e.f.Lock(ctx)
	cancel()
	if err != nil {
		t.Fatalf("healthBusy còn giữ khoá: %v", err)
	}
	if !e.s.healthBusy() {
		t.Error("khoá bị giữ mà báo rảnh")
	}
	unlock()
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if !e.s.healthBusy() {
		t.Error("có job trong tiến trình mà báo rảnh")
	}
	e.s.api.release()
	// Không có bộ hẹn giờ (NewHandler): kick không làm gì, không panic.
	e.s.kick("a.example.com")
}

// NewHandler đăng ký đủ đường dẫn của health (cần đăng nhập: 401 chứ không 404).
func TestHealthRoutes(t *testing.T) {
	h := NewHandler(Deps{}, nil)
	for _, rt := range [][2]string{
		{"GET", "/api/v1/incidents"}, {"GET", "/api/v1/incidents/1"}, {"POST", "/api/v1/fixes"},
		{"POST", "/api/v1/files/accept"}, {"POST", "/api/v1/health/run"},
		{"GET", "/api/v1/sites/a.example.com/traffic"}, {"GET", "/api/v1/sites/a.example.com/log?file=access"},
	} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(rt[0], rt[1], nil)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		h.ServeHTTP(rec, r)
		if rec.Code == 404 || rec.Code == 405 {
			t.Errorf("%s %s: %d", rt[0], rt[1], rec.Code)
		}
	}
}

// Run nghe cổng lỗi (cổng đã bị chiếm) thì trả lỗi ngay, không treo chờ bộ
// hẹn giờ health (ctx ngoài vẫn còn sống).
func TestRunListenErrorStopsScheduler(t *testing.T) {
	e := apiNewEnv(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	got := make(chan error, 1)
	go func() {
		got <- Run(context.Background(), RunOptions{Listen: ln.Addr().String(), Deps: Deps{Sys: e.f, Store: e.st, Now: e.clock}})
	}()
	select {
	case err := <-got:
		if err == nil {
			t.Fatal("cổng bị chiếm mà Run không lỗi")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run treo khi nghe cổng lỗi")
	}
}
