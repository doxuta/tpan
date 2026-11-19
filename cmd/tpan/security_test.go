package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

func TestSecurityNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"security", "status"},
		{"security", "ufw", "--dry-run"},
		{"security", "fail2ban", "--yes"},
		{"internal", "reload-web"},
		{"ssl", "list"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: mã %d, stderr %q, lệnh %v", args, code, errs, c.fake.Ran())
		}
	}
}

func TestSecurityStatusJSON(t *testing.T) {
	c := newCLI(t)
	code, out, _ := c.run("security", "status", "--json")
	var st core.SecurityStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil || code != 0 || len(st.Results) != 7 {
		t.Errorf("mã %d, %d dòng (%v)", code, len(st.Results), err)
	}
	if c.dbExists() {
		t.Error("security status tạo DB")
	}
}

func TestSecurityUFWDryRun(t *testing.T) {
	// sshd -T không chạy được: từ chối, không đoán cổng 22.
	c := newCLI(t)
	c.fake.Handle([]string{"sshd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 255} })
	code, out, _ := c.run("security", "ufw", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "ssh_port_unknown" {
		t.Errorf("sshd lỗi: mã %d, %s", code, out)
	}

	c = newCLI(t)
	c.fake.Handle([]string{"sshd"}, func(system.Cmd) system.Result { return system.Result{Stdout: "port 2222\n"} })
	code, out, errs := c.run("security", "ufw", "--dry-run")
	if code != 0 || !strings.Contains(out, "2222/tcp") || !strings.Contains(out, "ufw --force enable") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if c.dbExists() {
		t.Error("--dry-run tạo DB")
	}
	for _, m := range c.mutating() {
		if !strings.HasPrefix(m, "sshd ") && !strings.HasPrefix(m, "ufw show") && !strings.HasPrefix(m, "ufw status") {
			t.Errorf("--dry-run chạy lệnh thay đổi máy: %s", m)
		}
	}
}

func TestSecurityFail2banDryRunJSON(t *testing.T) {
	c := newCLI(t)
	code, out, _ := c.run("security", "fail2ban", "--dry-run", "--json")
	var pv engine.Preview
	if err := json.Unmarshal([]byte(out), &pv); err != nil || code != 0 || pv.Kind != "security.fail2ban" || len(pv.Steps) != 3 {
		t.Errorf("mã %d, %+v (%v)", code, pv, err)
	}
	// Không --yes, không terminal: không chạy gì.
	code, _, errs := c.run("security", "fail2ban")
	if code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("không xác nhận: mã %d, %q", code, errs)
	}
}

func TestInternalReloadWeb(t *testing.T) {
	c := newCLI(t)
	// Ẩn khỏi help.
	if _, out, _ := c.run("--help"); strings.Contains(out, "internal") {
		t.Errorf("help lộ lệnh internal:\n%s", out)
	}
	// Chưa có DB: không làm gì, không tạo DB.
	if code, _, errs := c.run("internal", "reload-web"); code != 0 || len(c.fake.Ran()) != 0 || c.dbExists() {
		t.Errorf("chưa có DB: mã %d, %q, %v", code, errs, c.fake.Ran())
	}
	c.seed(stackNginx)
	if code, _, errs := c.run("internal", "reload-web"); code != 0 {
		t.Errorf("nginx: mã %d, %q", code, errs)
	}
	var got []string
	for _, cmd := range c.fake.Ran() {
		got = append(got, cmd.String())
	}
	if strings.Join(got, "|") != "nginx -t|systemctl reload nginx" {
		t.Errorf("lệnh = %v", got)
	}
}
