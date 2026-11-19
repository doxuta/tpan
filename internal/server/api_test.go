package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// apiEnv là một Server chỉ có phần API, trên FakeSystem và store thật trong
// thư mục tạm. Test gọi thẳng handler, không qua NewHandler (requireSession là
// việc của phần đăng nhập).
type apiEnv struct {
	s   *Server
	f   *system.FakeSystem
	st  *store.Store
	mu  sync.Mutex
	now time.Time
	// dbPath là file của st, để test đọc bảng mà store không có hàm đọc (audit).
	dbPath string
}

func apiNewEnv(t *testing.T) *apiEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tpan.db")
	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("mở store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &apiEnv{f: system.NewFake(), st: st, dbPath: dbPath, now: time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)}
	e.s = &Server{deps: Deps{Sys: e.f, Store: st, Hostname: "vps-test", Now: e.clock}}
	e.s.api = newAPIState(e.s)
	e.s.api.lockWait = 100 * time.Millisecond
	// Chạy trước khi đóng store (t.Cleanup chạy ngược thứ tự đăng ký).
	t.Cleanup(e.s.api.wg.Wait)
	return e
}

func (e *apiEnv) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *apiEnv) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

// stack đặt settings "stack đã cài: nginx, PHP 8.3" và cho FakeSystem trả lời
// như máy chưa có gì của site: getent mã 2 (không có user), mariadb "0 0"
// (không DB, không user DB).
func (e *apiEnv) stack(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
		if err := e.st.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.f.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	e.f.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })
}

func (e *apiEnv) addSite(t *testing.T, dom string) validate.Domain {
	t.Helper()
	d := apiDomain(t, dom)
	if err := e.st.SaveSite(context.Background(), site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Created: e.now}); err != nil {
		t.Fatal(err)
	}
	return d
}

func apiDomain(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// apiCall gọi handler h với request đã dựng; pathValues là cặp tên, giá trị.
func apiCall(h http.HandlerFunc, method, target, body string, pathValues ...string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequest(method, target, rd)
	r.RemoteAddr = "192.0.2.10:40000"
	for i := 0; i+1 < len(pathValues); i += 2 {
		r.SetPathValue(pathValues[i], pathValues[i+1])
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func apiDecode[T any](t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) T {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("mã = %d, muốn %d; thân: %s", rec.Code, wantStatus, rec.Body.String())
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("thân không phải JSON đúng kiểu: %v\n%s", err, rec.Body.String())
	}
	return v
}

func apiErrCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) string {
	t.Helper()
	return apiDecode[ErrorBody](t, rec, wantStatus).Error.Code
}

// ---------------------------------------------------------------- tạo site

func TestAPISiteCreateBadInput(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"không phải JSON", `{`, "bad_request", 400},
		{"trường lạ", `{"domain":"a.example.com","email":"a@example.com","x":1}`, "bad_request", 400},
		{"chèn lệnh", `{"domain":"a.com; rm -rf /","email":"a@example.com"}`, validate.CodeDomainInvalid, 400},
		{"$(id)", `{"domain":"$(id).example.com","email":"a@example.com"}`, validate.CodeDomainInvalid, 400},
		{"đường dẫn", `{"domain":"../../etc/passwd","email":"a@example.com"}`, validate.CodeDomainInvalid, 400},
		{"xuống dòng", `{"domain":"a.example.com\nb","email":"a@example.com"}`, validate.CodeDomainInvalid, 400},
		{"dài 300", `{"domain":"` + strings.Repeat("a", 300) + `.com","email":"a@example.com"}`, validate.CodeDomainInvalid, 400},
		{"email sai", `{"domain":"a.example.com","email":"khong-phai-email"}`, validate.CodeEmailInvalid, 400},
		{"tiêu đề có xuống dòng", `{"domain":"a.example.com","email":"a@example.com","title":"a\nb"}`, "title_invalid", 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := apiCall(e.s.handleSiteCreate, "POST", "/api/v1/sites", c.body)
			if got := apiErrCode(t, rec, c.status); got != c.code {
				t.Fatalf("code = %q, muốn %q", got, c.code)
			}
		})
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("đầu vào sai mà vẫn có job: %v", jobs)
	}
}

func TestAPISiteCreateDryRun(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	rec := apiCall(e.s.handleSiteCreate, "POST", "/api/v1/sites?dry_run=1", `{"domain":"Shop.Example.com","email":"a@example.com","title":"Shop"}`)
	p := apiDecode[engine.Preview](t, rec, 200)
	if p.Kind != "site.add" || p.Target != "shop.example.com" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("xem trước mà có job: %v", jobs)
	}
}

func TestAPISiteCreateRefused(t *testing.T) {
	e := apiNewEnv(t) // stack chưa cài
	for range 2 {
		rec := apiCall(e.s.handleSiteCreate, "POST", "/api/v1/sites", `{"domain":"a.example.com","email":"a@example.com"}`)
		// Lần hai vẫn là stack_not_installed: lời từ chối phải nhả chỗ chạy job.
		if got := apiErrCode(t, rec, 409); got != "stack_not_installed" {
			t.Fatalf("code = %q", got)
		}
	}
}

// TestAPISiteCreateRunsJob: job chạy thật qua core trên FakeSystem. Bước tạo
// user Linux không qua được Check lại (getent vẫn trả "không có"), nên job
// hoàn tác: đủ để kiểm 202, job trong store, không có mật khẩu.
func TestAPISiteCreateRunsJob(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	rec := apiCall(e.s.handleSiteCreate, "POST", "/api/v1/sites", `{"domain":"a.example.com","email":"a@example.com"}`)
	js := apiDecode[JobStarted](t, rec, 202)
	e.s.api.wg.Wait()
	d := apiDecode[JobDetail](t, apiCall(e.s.handleJob, "GET", "/", "", "id", js.JobID), 200)
	if d.Job.Kind != "site.add" || d.Job.Status == engine.JobRunning || d.Job.Status == engine.JobSucceeded {
		t.Fatalf("job = %+v", d.Job)
	}
	if d.Secret != nil {
		t.Fatal("job hỏng mà có mật khẩu")
	}
	if len(d.Events) == 0 || d.Events[0].Kind != engine.EventJobStart {
		t.Fatalf("sự kiện = %+v", d.Events)
	}
	if !e.s.api.claim() {
		t.Fatal("job xong mà chưa nhả chỗ chạy job")
	}
}

// ---------------------------------------------------------------- xoá site

// DELETE /sites/{domain} chạy thật (từ M6 là đưa vào thùng rác):
// TestAPISiteDeleteTrashes ở api_trash_test.go.

func TestAPISiteDeleteErrors(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	if got := apiErrCode(t, apiCall(e.s.handleSiteDelete, "DELETE", "/", "", "domain", "a.com;id"), 400); got != validate.CodeDomainInvalid {
		t.Errorf("code = %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteDelete, "DELETE", "/", "", "domain", "none.example.com"), 404); got != "site_not_found" {
		t.Errorf("code = %q", got)
	}
}

func TestAPIJobRunning(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteDelete, "DELETE", "/", "", "domain", "a.example.com"), 409); got != "job_running" {
		t.Fatalf("code = %q", got)
	}
	e.s.api.release()
}

// TestAPIJobLockHeldElsewhere: khoá toàn cục bị giữ (như CLI đang chạy job):
// trả 409 sau lockWait, không tạo job, rồi chạy được khi khoá được nhả.
func TestAPIJobLockHeldElsewhere(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	unlock, err := e.f.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteDelete, "DELETE", "/", "", "domain", "a.example.com"), 409); got != "job_running" {
		t.Fatalf("code = %q", got)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("khoá bị giữ mà vẫn có job: %v", jobs)
	}
	unlock()
	apiDecode[JobStarted](t, apiCall(e.s.handleSiteDelete, "DELETE", "/", "", "domain", "a.example.com"), 202)
}

// ---------------------------------------------------------------- mật khẩu một lần

// apiEmptyJob chạy một Plan rỗng qua engine thật (journal là store), rồi chờ
// release trước khi trả: giả lập đoạn "job.done đã phát, core còn đang ghi
// bảng sites".
func apiEmptyJob(e *apiEnv, secret *AdminSecret, release <-chan struct{}) jobFunc {
	return func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		rep, err := engine.New(e.f, engine.Options{Journal: e.st, OnEvent: onEvent}).
			Execute(ctx, engine.Plan{Kind: "site.add", Target: "a.example.com"})
		if release != nil {
			<-release
		}
		return jobResult{status: rep.Job.Status, secret: secret}, err
	}
}

func TestAPIJobSecretOnce(t *testing.T) {
	e := apiNewEnv(t)
	const pw = "Abc123SecretValueXyz789"
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock) // chạy trước wg.Wait, kể cả khi test dừng giữa chừng
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	r := httptest.NewRequest("POST", "/api/v1/sites", nil)
	rec := httptest.NewRecorder()
	e.s.jobStart(rec, r, jobOpts{action: "site.add", target: "a.example.com"}, apiEmptyJob(e, &AdminSecret{URL: "http://a.example.com/wp-admin/", User: "admin", Password: pw}, release))
	js := apiDecode[JobStarted](t, rec, 202)

	// Giao diện gọi GET ngay sau job.done, lúc goroutine chưa cất mật khẩu:
	// GET phải chờ chứ không trả job "xong" mà thiếu mật khẩu.
	run := e.s.api.run(js.JobID)
	for deadline := time.Now().Add(5 * time.Second); !run.feed.finished(); {
		if time.Now().After(deadline) {
			t.Fatal("job không phát job.done")
		}
		time.Sleep(time.Millisecond)
	}
	got := make(chan *httptest.ResponseRecorder)
	go func() { got <- apiCall(e.s.handleJob, "GET", "/", "", "id", js.JobID) }()
	select {
	case <-got:
		t.Fatal("GET trả về trước khi job cất xong mật khẩu")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	first := apiDecode[JobDetail](t, <-got, 200)
	if first.Secret == nil || first.Secret.Password != pw || first.Job.Status != engine.JobSucceeded {
		t.Fatalf("lần GET đầu = %+v", first)
	}
	second := apiDecode[JobDetail](t, apiCall(e.s.handleJob, "GET", "/", "", "id", js.JobID), 200)
	if second.Secret != nil {
		t.Fatal("mật khẩu hiện lần thứ hai")
	}
	evs, _ := e.st.Events(context.Background(), js.JobID, 0)
	b, _ := json.Marshal(evs)
	if bytes.Contains(b, []byte(pw)) {
		t.Fatal("mật khẩu nằm trong sự kiện của job")
	}
}

func TestAPIJobNotFound(t *testing.T) {
	e := apiNewEnv(t)
	if got := apiErrCode(t, apiCall(e.s.handleJob, "GET", "/", "", "id", "khongco"), 404); got != "job_not_found" {
		t.Fatalf("code = %q", got)
	}
}

func TestAPIJobs(t *testing.T) {
	e := apiNewEnv(t)
	if got := apiCall(e.s.handleJobs, "GET", "/api/v1/jobs", "").Body.String(); strings.TrimSpace(got) != "[]" {
		t.Errorf("chưa có job: %s, muốn []", got)
	}
	for range 3 {
		if _, err := engine.New(e.f, engine.Options{Journal: e.st}).Execute(context.Background(), engine.Plan{Kind: "site.add", Target: "a.example.com"}); err != nil {
			t.Fatal(err)
		}
	}
	jobs := apiDecode[[]engine.Job](t, apiCall(e.s.handleJobs, "GET", "/api/v1/jobs?limit=2", ""), 200)
	if len(jobs) != 2 {
		t.Fatalf("limit=2 trả %d job", len(jobs))
	}
	for _, bad := range []string{"0", "-1", "abc", "501"} {
		if got := apiErrCode(t, apiCall(e.s.handleJobs, "GET", "/api/v1/jobs?limit="+bad, ""), 400); got != "limit_invalid" {
			t.Errorf("limit=%s: code = %q", bad, got)
		}
	}
}

// ---------------------------------------------------------------- xem

// site.Site không đọc ngược từ JSON được (validate.Slug không có
// UnmarshalText), nên test đọc vào các kiểu rút gọn này.
type apiSiteJSON struct {
	Domain string `json:"domain"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

type apiSiteInfoJSON struct {
	Site   apiSiteJSON `json:"site"`
	Layout struct {
		DB string `json:"db"`
	} `json:"layout"`
	Web    string          `json:"web"`
	Checks []checks.Result `json:"checks"`
}

type apiOverviewJSON struct {
	Hostname string       `json:"hostname"`
	Stack    StackSummary `json:"stack"`
	Sites    []struct {
		Site   apiSiteJSON     `json:"site"`
		Checks []checks.Result `json:"checks"`
	} `json:"sites"`
	StackChecks []checks.Result `json:"stack_checks"`
	LastRun     time.Time       `json:"last_run"`
}

// GET /sites/{domain} chạy phép kiểm của site ngay mỗi lần gọi (core.Sites.Info).
func TestAPISiteRunsChecks(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	for range 2 {
		e.f.ResetRan()
		info := apiDecode[apiSiteInfoJSON](t, apiCall(e.s.handleSite, "GET", "/", "", "domain", "a.example.com"), 200)
		if info.Site.Domain != "a.example.com" || info.Layout.DB == "" || info.Web != "nginx" || len(info.Checks) == 0 {
			t.Fatalf("SiteInfo = %+v", info)
		}
		if len(e.f.Ran()) == 0 {
			t.Fatal("không chạy phép kiểm nào")
		}
	}
	if got := apiErrCode(t, apiCall(e.s.handleSite, "GET", "/", "", "domain", "none.example.com"), 404); got != "site_not_found" {
		t.Fatalf("code = %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleSite, "GET", "/", "", "domain", "a.com;id"), 400); got != validate.CodeDomainInvalid {
		t.Fatalf("code = %q", got)
	}
}

func TestAPIReadOnly(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	e.f.SetFile("/proc/meminfo", []byte("MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"), 0o444, system.Root)

	sites := apiDecode[[]apiSiteJSON](t, apiCall(e.s.handleSites, "GET", "/", ""), 200)
	if len(sites) != 1 {
		t.Errorf("sites = %+v", sites)
	}
	st := apiDecode[core.Status](t, apiCall(e.s.handleStack, "GET", "/", ""), 200)
	if !st.Installed || st.Config.Web != core.Nginx {
		t.Errorf("stack = %+v", st)
	}
	h := apiDecode[core.Health](t, apiCall(e.s.handleChecks, "GET", "/", ""), 200)
	if !h.Installed || len(h.Results) == 0 {
		t.Errorf("checks = %+v", h)
	}
	m := apiDecode[Machine](t, apiCall(e.s.handleMachine, "GET", "/", ""), 200)
	if m.MemTotal != 1000*1024 || m.MemUsed != 600*1024 || m.CPUPercent != -1 || len(m.Errors) == 0 || !m.Measured.Equal(e.clock()) {
		t.Errorf("machine = %+v", m)
	}

	e.f.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: site.LayoutOf(apiDomain(t, "a.example.com")).DB + "\t3\t49152\n"}
	})
	dbs := apiDecode[[]DatabaseRow](t, apiCall(e.s.handleDatabases, "GET", "/", ""), 200)
	if len(dbs) != 1 || dbs[0].Domain != "a.example.com" || dbs[0].Tables != 3 || dbs[0].SizeBytes != 49152 || dbs[0].User != dbs[0].DB {
		t.Errorf("databases = %+v", dbs)
	}
}

// Tổng quan đọc phép kiểm từ check_results, không chạy chúng; ?refresh=1 chạy
// health đầy đủ trước.
func TestAPIOverview(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	e.addSite(t, "b.example.com")
	rec := apiCall(e.s.handleOverview, "GET", "/", "")
	ov := apiDecode[apiOverviewJSON](t, rec, 200)
	if ov.Hostname != "vps-test" || !ov.Stack.Installed || ov.Stack.Web != "nginx" || ov.Stack.PHPVersion != "8.3" {
		t.Errorf("đầu trang = %+v", ov)
	}
	if len(ov.Sites) != 2 || ov.Sites[0].Site.Domain != "a.example.com" || len(ov.Sites[1].Checks) != 0 || len(ov.StackChecks) != 0 {
		t.Errorf("chưa chạy health mà đã có phép kiểm: %+v", ov)
	}
	for _, k := range []string{`"jobs":[]`, `"sites":[`, `"machine":{`, `"stack_checks":[]`, `"tpan_checks":[]`, `"incidents":[]`, `"checks":[]`} {
		if !strings.Contains(rec.Body.String(), k) {
			t.Errorf("JSON thiếu %s", k)
		}
	}
	if strings.Contains(rec.Body.String(), `"last_run"`) {
		t.Error("chưa chạy health mà có last_run")
	}
	for _, c := range e.f.Ran() {
		if c.Name != "tail" {
			t.Fatalf("tổng quan chạy lệnh ngoài đếm request: %s", c.String())
		}
	}

	ov = apiDecode[apiOverviewJSON](t, apiCall(e.s.handleOverview, "GET", "/?refresh=1", ""), 200)
	if len(ov.StackChecks) == 0 || len(ov.Sites[1].Checks) == 0 || !ov.LastRun.Equal(e.clock()) {
		t.Errorf("refresh=1: stack %d, site %d, last_run %v", len(ov.StackChecks), len(ov.Sites[1].Checks), ov.LastRun)
	}

	// Lần sau: vẫn có kết quả (đọc từ store) mà không chạy phép kiểm nào.
	e.f.ResetRan()
	ov2 := apiDecode[apiOverviewJSON](t, apiCall(e.s.handleOverview, "GET", "/", ""), 200)
	if len(ov2.Sites[1].Checks) != len(ov.Sites[1].Checks) || len(ov2.StackChecks) != len(ov.StackChecks) {
		t.Errorf("đọc lại khác lần refresh: %+v", ov2)
	}
	for _, c := range e.f.Ran() {
		if c.Name != "tail" {
			t.Fatalf("tổng quan chạy phép kiểm: %s", c.String())
		}
	}
}

// apiSampleSystem đếm số lệnh chạy trong lúc monitor đang đo CPU (giữa hai lần
// đọc /proc/stat).
type apiSampleSystem struct {
	*system.FakeSystem
	mu        sync.Mutex
	reads     int // số lần đọc /proc/stat
	duringCPU []string
}

func (a *apiSampleSystem) ReadFile(p string) ([]byte, error) {
	if p == "/proc/stat" {
		a.mu.Lock()
		a.reads++
		a.mu.Unlock()
	}
	return a.FakeSystem.ReadFile(p)
}

func (a *apiSampleSystem) Run(ctx context.Context, cmd system.Cmd) (system.Result, error) {
	a.mu.Lock()
	if a.reads%2 == 1 {
		a.duringCPU = append(a.duringCPU, cmd.String())
	}
	a.mu.Unlock()
	return a.FakeSystem.Run(ctx, cmd)
}

// Số liệu máy của /overview không được đo trong lúc phép kiểm đang chạy: phép
// kiểm là tải, đo chung thì CPU% luôn cao vì chính tpan (thấy trên máy ảo ở e2e M3).
func TestAPIOverviewCPUNotDuringChecks(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	e.addSite(t, "b.example.com")
	e.f.SetFile("/proc/stat", []byte("cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 100 0 100 800 0 0 0 0 0 0\n"), 0o444, system.Root)
	ss := &apiSampleSystem{FakeSystem: e.f}
	e.s.deps.Sys = ss
	// refresh=1: phép kiểm chạy trong cùng request, trước khi đo.
	apiDecode[apiOverviewJSON](t, apiCall(e.s.handleOverview, "GET", "/?refresh=1", ""), 200)
	if ss.reads != 2 {
		t.Fatalf("đọc /proc/stat %d lần, muốn 2 (test không đo được gì)", ss.reads)
	}
	if len(e.f.Ran()) == 0 {
		t.Fatal("tổng quan không chạy lệnh nào (test không đo được gì)")
	}
	if len(ss.duringCPU) != 0 {
		t.Errorf("%d lệnh chạy trong lúc đo CPU, ví dụ %q", len(ss.duringCPU), ss.duringCPU[0])
	}
}
