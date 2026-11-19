package demo

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// TestSecurityPageOnDemo: trang bảo mật của máy demo có đúng tình trạng đã định
// (security.go): mỗi dòng một trạng thái, không dòng nào Unknown vì lệnh không
// được giả lập.
func TestSecurityPageOnDemo(t *testing.T) {
	ctx, sys, _ := newDemo(t)
	rs := checks.RunAll(ctx, sys, checks.SecurityChecks(core.PanelPort))
	want := map[string]checks.Status{
		"sec.ufw":          checks.Warn, // đã cài, đang tắt
		"sec.fail2ban":     checks.Warn, // chưa cài
		"sec.ssh.root":     checks.Pass, // prohibit-password
		"sec.ssh.password": checks.Warn, // yes
		"sec.swap":         checks.Pass,
		"sec.updates":      checks.Warn, // 2 gói
		"sec.panel":        checks.Warn, // 0.0.0.0:8443
	}
	if len(rs) != len(want) {
		t.Fatalf("có %d dòng, muốn %d", len(rs), len(want))
	}
	for _, r := range rs {
		if r.Status != want[r.ID] {
			t.Errorf("%s = %s (%s), muốn %s", r.ID, r.Status, r.Summary.Vi, want[r.ID])
		}
	}
	if s := statusSummary(rs, "sec.ufw"); !strings.Contains(s, "tắt") {
		t.Errorf("sec.ufw: %q, muốn nói ufw đang tắt", s)
	}
	if s := statusSummary(rs, "sec.updates"); !strings.HasPrefix(s, "2 ") {
		t.Errorf("sec.updates: %q, muốn 2 gói", s)
	}
	if s := statusSummary(rs, "sec.panel"); !strings.Contains(s, "0.0.0.0") || !strings.Contains(s, "8443") {
		t.Errorf("sec.panel: %q", s)
	}
}

func statusSummary(rs []checks.Result, id string) string {
	for _, r := range rs {
		if r.ID == id {
			return r.Summary.Vi
		}
	}
	return ""
}

// TestSecurityPlansOnDemo: hai nút của trang bảo mật chạy trọn Plan trên máy
// giả, rồi dòng tương ứng đạt; chạy lại thì không còn gì phải làm.
func TestSecurityPlansOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	sec := &core.Security{Sys: sys, Journal: st}

	for _, c := range []struct {
		name    string
		prepare func() (*core.SecurityPlan, error)
		check   checks.Check
	}{
		{"ufw", func() (*core.SecurityPlan, error) { return sec.PrepareUFW(ctx) }, checks.UFW()},
		{"fail2ban", func() (*core.SecurityPlan, error) { return sec.PrepareFail2ban(ctx) }, checks.Fail2banSSHD()},
	} {
		for run := range 2 {
			p, err := c.prepare()
			if err != nil {
				t.Fatalf("%s lần %d: chuẩn bị: %v", c.name, run+1, err)
			}
			rep, err := p.Execute(ctx, nil)
			if err != nil || rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("%s lần %d: job %s: %s, %v", c.name, run+1, rep.Job.Status, rep.Job.Error, err)
			}
			if r := c.check.Run(ctx, sys); r.Status != checks.Pass {
				t.Errorf("%s lần %d: sau khi chạy vẫn %s: %s", c.name, run+1, r.Status, r.Summary.Vi)
			}
		}
	}

	// Luật đã mở: cổng SSH, 80, 443, và cổng panel vì panel đang nghe ra ngoài.
	res, err := sys.Run(ctx, system.Cmd{Name: "ufw", Args: []string{"show", "added"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"22/tcp", "80/tcp", "443/tcp", "8443/tcp"} {
		if !strings.Contains(res.Stdout, "ufw allow "+r+"\n") {
			t.Errorf("thiếu luật %s:\n%s", r, res.Stdout)
		}
	}
	if _, err := sys.ReadFile(core.Fail2banJail); err != nil {
		t.Errorf("chưa có jail của tpan: %v", err)
	}
}
