package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/system"
)

func TestM10RouteNeedsSession(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/stack/switch", strings.NewReader(`{"to":"apache"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("mã = %d, muốn 401", rec.Code)
	}
}

func TestAPIStackSwitchBadBody(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	for _, body := range []string{`{`, `{"to":"apache","x":1}`, `{"to":5}`, `[]`} {
		if got := apiErrCode(t, apiCall(e.s.handleStackSwitch, "POST", "/", body), 400); got != "bad_request" {
			t.Errorf("%s: code = %q", body, got)
		}
	}
	// Web server sai là lời từ chối của core: 409, cả xem trước lẫn chạy thật.
	for _, to := range []string{"caddy", "Apache", "nginx; rm -rf /", "$(id)", "", "nginx\napache"} {
		b, _ := json.Marshal(StackSwitchBody{To: to})
		for _, q := range []string{"/", "/?dry_run=1"} {
			if got := apiErrCode(t, apiCall(e.s.handleStackSwitch, "POST", q, string(b)), 409); got != "web_invalid" {
				t.Errorf("%q %s: code = %q", to, q, got)
			}
		}
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("đầu vào sai mà có job: %v", jobs)
	}
	if a := hAudit(t, e, "stack.switch"); len(a) == 0 || a[0].Result != "web_invalid" || a[0].Target != "caddy" {
		t.Errorf("audit = %+v", a)
	}
}

func TestAPIStackSwitchRefusals(t *testing.T) {
	e := apiNewEnv(t) // stack chưa cài
	if got := apiErrCode(t, apiCall(e.s.handleStackSwitch, "POST", "/", `{"to":"apache"}`), 409); got != "stack_not_installed" {
		t.Errorf("code = %q", got)
	}
	e.stack(t)
	e.addSite(t, "shop.example.com")
	if got := apiErrCode(t, apiCall(e.s.handleStackSwitch, "POST", "/", `{"to":"nginx"}`), 409); got != "stack_same_web" {
		t.Errorf("code = %q", got)
	}
	// Site đang hỏng: 409 kèm checks là kết quả của site đó.
	e.f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "<html>lỗi</html>TPAN_HTTP_CODE=500"}
	})
	for _, q := range []string{"/", "/?dry_run=1"} {
		eb := apiDecode[ErrorBody](t, apiCall(e.s.handleStackSwitch, "POST", q, `{"to":"apache"}`), 409)
		if eb.Error.Code != "precondition_failed" || len(eb.Error.Checks) != 1 || eb.Error.Checks[0].ID != "http.site:shop.example.com" {
			t.Errorf("%s: %+v", q, eb.Error)
		}
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("từ chối mà có job: %v", jobs)
	}
	// Lời từ chối nhả chỗ chạy job.
	if !e.s.api.claim() {
		t.Fatal("lời từ chối giữ chỗ chạy job")
	}
	e.s.api.release()
}

func TestAPIStackSwitchDryRunAndJob(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "shop.example.com")
	p := apiDecode[engine.Preview](t, apiCall(e.s.handleStackSwitch, "POST", "/?dry_run=1", `{"to":"apache"}`), 200)
	if p.Kind != "stack.switch" || p.Target != "apache" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("xem trước mà có job: %v", jobs)
	}

	js := apiDecode[JobStarted](t, apiCall(e.s.handleStackSwitch, "POST", "/", `{"to":"apache"}`), 202)
	e.s.api.wg.Wait()
	job, err := e.st.Job(context.Background(), js.JobID)
	if err != nil || job.Kind != "stack.switch" || job.Target != "apache" || job.Status == engine.JobRunning {
		t.Fatalf("job = %+v, %v", job, err)
	}
	if a := hAudit(t, e, "stack.switch"); len(a) != 1 || a[0].JobID != js.JobID || a[0].Target != "apache" {
		t.Errorf("audit = %+v", a)
	}
	// Job xong (thành công hay không) thì health đầy đủ đã chạy.
	if v, ok, _ := e.st.GetSetting(context.Background(), health.SettingLastRun); !ok || v == "" {
		t.Error("health đầy đủ chưa chạy sau stack.switch")
	}
	// Máy giả không cài được gói nên job hoàn tác: settings giữ nguyên.
	if v, _, _ := e.st.GetSetting(context.Background(), core.SettingWeb); job.Status != engine.JobSucceeded && v != "nginx" {
		t.Errorf("job %s mà stack.web = %q", job.Status, v)
	}

	// Đang có job thì 409 job_running.
	e.s.api.claim()
	defer e.s.api.release()
	if got := apiErrCode(t, apiCall(e.s.handleStackSwitch, "POST", "/", `{"to":"apache"}`), 409); got != "job_running" {
		t.Fatalf("code = %q", got)
	}
}
