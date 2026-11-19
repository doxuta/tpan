package server

import (
	"context"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// secMachine cho FakeSystem trả lời như máy có sshd nghe cổng 22 và panel
// nghe 0.0.0.0:8443; các lệnh khác (ufw, apt-get...) trả mã 0, output rỗng.
func (e *apiEnv) secMachine() {
	e.f.Handle([]string{"sshd", "-T"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "port 22\npermitrootlogin prohibit-password\npasswordauthentication no\n"}
	})
	e.f.Handle([]string{"ss"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:* users:((\"tpan\",pid=800,fd=7))\n"}
	})
}

func TestAPISecurityStatus(t *testing.T) {
	e := apiNewEnv(t)
	e.secMachine()
	st := apiDecode[core.SecurityStatus](t, apiCall(e.s.handleSecurity, "GET", "/api/v1/security", ""), 200)
	// Đúng bảy dòng của checks.SecurityChecks, theo thứ tự trên trang.
	want := []string{"sec.ufw", "sec.fail2ban", "sec.ssh.root", "sec.ssh.password", "sec.swap", "sec.updates", "sec.panel"}
	if len(st.Results) != len(want) {
		t.Fatalf("Results = %+v", st.Results)
	}
	for i, id := range want {
		if st.Results[i].ID != id {
			t.Errorf("dòng %d = %q, muốn %q", i, st.Results[i].ID, id)
		}
	}
}

func TestAPISecurityDryRun(t *testing.T) {
	e := apiNewEnv(t)
	e.secMachine()
	for path, kind := range map[string]string{"ufw": "security.ufw", "fail2ban": "security.fail2ban"} {
		h := e.s.handleSecurityUFW
		if path == "fail2ban" {
			h = e.s.handleSecurityFail2ban
		}
		p := apiDecode[engine.Preview](t, apiCall(h, "POST", "/api/v1/security/"+path+"?dry_run=1", ""), 200)
		if p.Kind != kind || len(p.Steps) == 0 {
			t.Fatalf("%s: Preview = %+v", path, p)
		}
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("xem trước mà có job: %v", jobs)
	}
}

// TestAPISecurityUFWSSHPortUnknown: không đọc được cổng SSH thì từ chối, không
// đoán 22 (bật tường lửa với cổng đoán sai là mất đường vào máy).
func TestAPISecurityUFWSSHPortUnknown(t *testing.T) {
	e := apiNewEnv(t)
	e.f.Handle([]string{"sshd", "-T"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 255, Stderr: "sshd: no hostkeys available -- exiting.\n"}
	})
	for range 2 {
		if got := apiErrCode(t, apiCall(e.s.handleSecurityUFW, "POST", "/", ""), 409); got != "ssh_port_unknown" {
			t.Fatalf("code = %q", got)
		}
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("bị từ chối mà có job: %v", jobs)
	}
}

func TestAPISecurityRunsJob(t *testing.T) {
	e := apiNewEnv(t)
	e.secMachine()
	js := apiDecode[JobStarted](t, apiCall(e.s.handleSecurityFail2ban, "POST", "/", ""), 202)
	e.s.api.wg.Wait()
	job, err := e.st.Job(context.Background(), js.JobID)
	if err != nil || job.Kind != "security.fail2ban" || job.Status == engine.JobRunning {
		t.Fatalf("job = %+v, %v", job, err)
	}
	if !e.s.api.claim() {
		t.Fatal("job xong mà chưa nhả chỗ chạy job")
	}
	e.s.api.release()

	// Đang có job thì nút thứ hai trả 409.
	e.s.api.claim()
	defer e.s.api.release()
	if got := apiErrCode(t, apiCall(e.s.handleSecurityUFW, "POST", "/", ""), 409); got != "job_running" {
		t.Fatalf("code = %q", got)
	}
}
