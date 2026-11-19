package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// sslDNS cho FakeSystem trả lời như máy có IP 203.0.113.10 và mọi domain trỏ
// về đó (chỉ có bản ghi A), để điều kiện đầu DNS của SSL đạt.
func (e *apiEnv) sslDNS() {
	e.f.Handle([]string{"getent", "ahostsv4"}, func(c system.Cmd) system.Result {
		return system.Result{Stdout: "203.0.113.10    STREAM " + c.Args[1] + "\n"}
	})
	e.f.Handle([]string{"getent", "ahostsv6"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	e.f.Handle([]string{"ip"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "2: eth0    inet 203.0.113.10/24 brd 203.0.113.255 scope global eth0\\       valid_lft forever preferred_lft forever\n"}
	})
}

func (e *apiEnv) sslEmail(t *testing.T) {
	t.Helper()
	if err := e.st.SetSetting(context.Background(), core.SettingSSLEmail, "admin@example.com"); err != nil {
		t.Fatal(err)
	}
}

// TestM5RoutesNeedSession: mọi đường dẫn mới có trong NewHandler (không rơi
// vào 404 của API) và đòi phiên đăng nhập.
func TestM5RoutesNeedSession(t *testing.T) {
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/ssl"},
		{"POST", "/api/v1/ssl/settings"},
		{"POST", "/api/v1/sites/a.example.com/ssl"},
		{"GET", "/api/v1/security"},
		{"POST", "/api/v1/security/ufw"},
		{"POST", "/api/v1/security/fail2ban"},
	} {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: mã = %d, muốn 401", c.method, c.path, rec.Code)
		}
	}
}

func TestAPIErrorStatusM5(t *testing.T) {
	for code, want := range map[string]int{
		"ssl_server_invalid": 400,
		"acme_email_missing": 409,
		"ssh_port_unknown":   409,
		"site_not_found":     404,
	} {
		if got, _ := apiErrorStatus(&core.RefusedError{Code: code}); got != want {
			t.Errorf("%s: mã HTTP = %d, muốn %d", code, got, want)
		}
	}
}

// ---------------------------------------------------------------- cấu hình

func TestAPISSLSettingsBadInput(t *testing.T) {
	e := apiNewEnv(t)
	cases := []struct{ name, body, code string }{
		{"không phải JSON", `{`, "bad_request"},
		// server chỉ đặt được ở CLI: gửi lên thì lỗi, không bỏ qua im lặng.
		{"trường server", `{"server":"https://acme.example.com/dir"}`, "bad_request"},
		{"trường lạ", `{"email":"a@example.com","x":1}`, "bad_request"},
		{"staging không phải bool", `{"staging":"yes"}`, "bad_request"},
		{"email sai", `{"email":"khong-phai-email"}`, validate.CodeEmailInvalid},
		{"email rỗng", `{"email":""}`, validate.CodeEmailInvalid},
		{"email chèn lệnh", `{"email":"a@example.com; rm -rf /"}`, validate.CodeEmailInvalid},
		{"email $(id)", `{"email":"$(id)@example.com"}`, validate.CodeEmailInvalid},
		{"email xuống dòng", `{"email":"a@example.com\nb"}`, validate.CodeEmailInvalid},
		{"email dài 300", `{"email":"` + strings.Repeat("a", 300) + `@example.com"}`, validate.CodeEmailInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := apiCall(e.s.handleSSLSettings, "POST", "/api/v1/ssl/settings", c.body)
			if got := apiErrCode(t, rec, 400); got != c.code {
				t.Fatalf("code = %q, muốn %q", got, c.code)
			}
		})
	}
	if all, _ := e.st.Settings(context.Background()); all[core.SettingSSLEmail] != "" {
		t.Fatalf("đầu vào sai mà email đã ghi: %q", all[core.SettingSSLEmail])
	}
}

func TestAPISSLSettingsAndOverview(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "b.example.com")
	e.addSite(t, "a.example.com")

	cfg := apiDecode[core.SSLConfig](t, apiCall(e.s.handleSSLSettings, "POST", "/", `{"email":"Admin@Example.com","staging":false}`), 200)
	if cfg.Email == "" || cfg.Staging {
		t.Fatalf("cấu hình sau khi đặt = %+v", cfg)
	}
	// Trường vắng mặt thì giữ nguyên.
	cfg2 := apiDecode[core.SSLConfig](t, apiCall(e.s.handleSSLSettings, "POST", "/", `{}`), 200)
	if cfg2 != cfg {
		t.Fatalf("thân rỗng mà cấu hình đổi: %+v → %+v", cfg, cfg2)
	}

	ov := apiDecode[SSLOverview](t, apiCall(e.s.handleSSL, "GET", "/api/v1/ssl", ""), 200)
	if ov.Config != cfg {
		t.Fatalf("Config = %+v, muốn %+v", ov.Config, cfg)
	}
	if len(ov.Sites) != 2 || ov.Sites[0].Domain.String() != "a.example.com" || ov.Sites[0].SSL || ov.Sites[0].Cert != nil {
		t.Fatalf("Sites = %+v", ov.Sites)
	}
}

func TestAPISSLOverviewEmptyIsArray(t *testing.T) {
	e := apiNewEnv(t)
	rec := apiCall(e.s.handleSSL, "GET", "/api/v1/ssl", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"sites":[]`) {
		t.Fatalf("mã %d, thân %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------- xin SSL

func TestAPISiteSSLErrors(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	for _, dom := range []string{"a.com;id", "$(id).example.com", "../../etc/passwd", "a.example.com\nb", strings.Repeat("a", 300) + ".com"} {
		if got := apiErrCode(t, apiCall(e.s.handleSiteSSL, "POST", "/", "", "domain", dom), 400); got != validate.CodeDomainInvalid {
			t.Errorf("%q: code = %q", dom, got)
		}
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteSSL, "POST", "/", "", "domain", "none.example.com"), 404); got != "site_not_found" {
		t.Errorf("site không có: code = %q", got)
	}
}

func TestAPISiteSSLRefusedWithoutEmail(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.sslDNS()
	e.addSite(t, "a.example.com")
	for range 2 {
		// Lần hai vẫn là acme_email_missing: lời từ chối phải nhả chỗ chạy job.
		if got := apiErrCode(t, apiCall(e.s.handleSiteSSL, "POST", "/", "", "domain", "a.example.com"), 409); got != "acme_email_missing" {
			t.Fatalf("code = %q", got)
		}
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("bị từ chối mà có job: %v", jobs)
	}
}

func TestAPISiteSSLDryRun(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.sslDNS()
	e.sslEmail(t)
	e.addSite(t, "a.example.com")
	p := apiDecode[engine.Preview](t, apiCall(e.s.handleSiteSSL, "POST", "/?dry_run=1", "", "domain", "A.Example.com"), 200)
	if p.Kind != "site.ssl" || p.Target != "a.example.com" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("xem trước mà có job: %v", jobs)
	}
}

// TestAPISiteSSLRunsJob: job chạy thật qua core trên FakeSystem (máy giả không
// có certbot nên job không thành công, không quan trọng ở đây). Kiểm 202, job
// site.ssl trong store và chỗ chạy job được nhả. (Bộ đệm phép kiểm của M3 đã bị M4 thay bằng kết quả health trong DB.)
func TestAPISiteSSLRunsJob(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.sslDNS()
	e.sslEmail(t)
	e.addSite(t, "a.example.com")

	js := apiDecode[JobStarted](t, apiCall(e.s.handleSiteSSL, "POST", "/", "", "domain", "a.example.com"), 202)
	e.s.api.wg.Wait()
	job, err := e.st.Job(context.Background(), js.JobID)
	if err != nil || job.Kind != "site.ssl" || job.Status == engine.JobRunning {
		t.Fatalf("job = %+v, %v", job, err)
	}
	if !e.s.api.claim() {
		t.Fatal("job xong mà chưa nhả chỗ chạy job")
	}
}

func TestAPISiteSSLJobRunning(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "a.example.com")
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	defer e.s.api.release()
	if got := apiErrCode(t, apiCall(e.s.handleSiteSSL, "POST", "/", "", "domain", "a.example.com"), 409); got != "job_running" {
		t.Fatalf("code = %q", got)
	}
}
