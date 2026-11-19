package core

import (
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// newSecSim: sshd -T in sshd (rỗng thì sshd lỗi), ss in listen cho cổng panel.
func newSecSim(sshd, ss string) *system.FakeSystem {
	f := system.NewFake()
	f.Handle([]string{"sshd", "-T"}, func(system.Cmd) system.Result {
		if sshd == "" {
			return system.Result{ExitCode: 1, Stderr: "sshd: no hostkeys available -- exiting.\n"}
		}
		return system.Result{Stdout: sshd}
	})
	f.Handle([]string{"ss"}, func(system.Cmd) system.Result { return system.Result{Stdout: ss} })
	return f
}

func TestSecurityUFWPlan(t *testing.T) {
	const (
		loopback = "LISTEN 0 4096 127.0.0.1:8443 0.0.0.0:*\n"
		public   = "LISTEN 0 4096 *:8443 *:*\n"
	)
	for _, tc := range []struct {
		name, sshd, ss string
		rules          []string
	}{
		{"một cổng, panel riêng tư", "port 22\npermitrootlogin no\n", loopback, []string{"22/tcp", "80/tcp", "443/tcp"}},
		{"hai cổng, panel mở", "port 2222\nport 22\n", public, []string{"2222/tcp", "22/tcp", "80/tcp", "443/tcp", "8443/tcp"}},
		{"cổng SSH trùng 443", "port 443\n", "", []string{"443/tcp", "80/tcp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Security{Sys: newSecSim(tc.sshd, tc.ss)}
			p, err := s.PrepareUFW(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if p.Plan.Kind != "security.ufw" || len(p.Plan.Steps) != 3 || len(p.Plan.Verify) != 1 {
				t.Fatalf("Plan = %s, %d bước, %d kiểm", p.Plan.Kind, len(p.Plan.Steps), len(p.Plan.Verify))
			}
			if a, ok := p.Plan.Steps[0].(*steps.AptInstall); !ok || !reflect.DeepEqual(a.Packages, []string{"ufw"}) {
				t.Errorf("bước 1 = %#v", p.Plan.Steps[0])
			}
			allow, ok := p.Plan.Steps[1].(*steps.UFWAllow)
			if !ok || !reflect.DeepEqual(allow.Rules, tc.rules) {
				t.Errorf("luật = %#v, muốn %v", p.Plan.Steps[1], tc.rules)
			}
			if _, ok := p.Plan.Steps[2].(*steps.UFWEnable); !ok {
				t.Errorf("bước cuối = %#v", p.Plan.Steps[2])
			}
			if p.Plan.Title.Vi == "" || p.Plan.Title.En == "" {
				t.Error("tiêu đề thiếu một ngôn ngữ")
			}
		})
	}

	for _, sshd := range []string{"", "permitrootlogin no\n", "port abc\n", "port 0\n", "port 70000\n", "port 22; rm -rf /\n"} {
		s := &Security{Sys: newSecSim(sshd, "")}
		_, err := s.PrepareUFW(ctx)
		if refusedCode(t, err) != "ssh_port_unknown" {
			t.Errorf("sshd -T = %q: muốn ssh_port_unknown", sshd)
		}
	}

	// ss lỗi: không biết panel có mở không thì dừng, không đoán.
	f := newSecSim("port 22\n", "")
	f.Handle([]string{"ss"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 1} })
	if _, err := (&Security{Sys: f}).PrepareUFW(ctx); err == nil || !strings.Contains(err.Error(), "8443") {
		t.Errorf("ss lỗi: %v", err)
	}
}

func TestSecurityFail2banPlan(t *testing.T) {
	s := &Security{Sys: system.NewFake()}
	p, err := s.PrepareFail2ban(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"", "file:" + Fail2banJail, "service:fail2ban"}
	got := stepIDs(p.Plan)
	got[0] = ""
	if p.Plan.Kind != "security.fail2ban" || !reflect.DeepEqual(got, want) || len(p.Plan.Verify) != 1 {
		t.Errorf("Plan = %s %v", p.Plan.Kind, got)
	}
	f := p.Plan.Steps[1].(*steps.File)
	if f.Perm != 0o644 || f.Test.String() != "fail2ban-client -t" || f.Reload.String() != "fail2ban-client reload" || !strings.Contains(string(f.Content), "sshd") {
		t.Errorf("jail = %o %v %v", f.Perm, f.Test, f.Reload)
	}

	// Xem trước chỉ đọc; chạy không có Journal thì lỗi.
	if _, err := p.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(ctx, nil); err == nil {
		t.Error("Execute không có Journal mà không lỗi")
	}
}

func TestSecurityStatusAndExecute(t *testing.T) {
	m := newSim(t)
	st, err := (&Security{Sys: m}).Status(ctx)
	if err != nil || len(st.Results) != 7 || st.Results[0].ID != "sec.ufw" {
		t.Fatalf("Status = %+v, %v", st, err)
	}

	// Chạy thật Plan fail2ban trên máy giả: gói được cài, jail được ghi, dịch vụ bật.
	_, _, db := newTestStack(t)
	p, err := (&Security{Sys: m, Journal: db}).PrepareFail2ban(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m.Handle([]string{"fail2ban-client", "status"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "Status for the jail: sshd\n|- Filter\n|  |- Currently failed:\t0\n`- Actions\n   |- Currently banned:\t0\n"}
	})
	m.SetDir("/etc/fail2ban/jail.d", 0o755, system.Root)
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s %s %+v", err, rep.Job.Status, rep.Job.Error, rep.Checks)
	}
	if data, err := m.ReadFile(Fail2banJail); err != nil || !strings.Contains(string(data), "backend") {
		t.Errorf("jail: %v", err)
	}
}
