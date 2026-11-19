package main

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/system"
)

// pendingNginx: apt có bản mới cho nginx (bảo mật) và một gói ngoài stack.
// `apt-get install --only-upgrade` làm nginx hết chờ.
func (c *cli) pendingNginx() {
	const inst = "Inst nginx [1.24.0-2ubuntu7.4] (1.24.0-2ubuntu7.5 Ubuntu:24.04/noble-security [arm64])\n"
	var upgraded atomic.Bool
	c.fake.Handle([]string{"apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: inst + "Inst vim [1] (2 Ubuntu:24.04/noble-updates [arm64])\n"}
	})
	c.fake.Handle([]string{"apt-get", "-s", "install", "--only-upgrade"}, func(system.Cmd) system.Result {
		if upgraded.Load() {
			return system.Result{}
		}
		return system.Result{Stdout: inst}
	})
	c.fake.Handle([]string{"apt-get", "install", "-y", "--only-upgrade"}, func(system.Cmd) system.Result {
		upgraded.Store(true)
		return system.Result{}
	})
}

func TestUpdateFlags(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--check", "--dry-run"},
		{"update", "--check", "--yes"},
		{"update", "lạ"},
		{"update", "auto", "lạ"},
	} {
		c := newCLI(t)
		if code, _, errs := c.run(args...); code != 1 || errs == "" {
			t.Errorf("%q: mã %d, stderr %q", args, code, errs)
		}
		if len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: cờ sai mà vẫn chạy lệnh %v hoặc tạo DB", args, c.fake.Ran())
		}
	}
}

func TestUpdateNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--check"},
		{"update", "--dry-run"},
		{"update", "--yes"},
		{"update", "auto", "--dry-run"},
		{"mariadb", "upgrade", "--to", "11.8", "--dry-run"},
		{"self-update", "--dry-run"},
		{"self-update", "--rollback", "--yes"},
	} {
		c := newCLI(t)
		c.root = false
		code, out, _ := c.run(append(args, "--json")...)
		if code != 1 || refusalCode(t, out) != "root_required" || len(c.fake.Ran()) != 0 {
			t.Errorf("%q: mã %d, %s, lệnh %v", args, code, out, c.fake.Ran())
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	c := newCLI(t)
	c.pendingNginx()
	code, out, errs := c.run("update", "--check")
	if code != 0 || !strings.Contains(out, "nginx") || !strings.Contains(out, "1.24.0-2ubuntu7.4 -> 1.24.0-2ubuntu7.5") || !strings.Contains(out, "[bảo mật]") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(out, "đang chờ: 1") {
		t.Errorf("thiếu số gói khác:\n%s", out)
	}
	if c.dbExists() {
		t.Error("--check tạo DB")
	}
	var refreshed bool
	for _, m := range c.mutating() {
		if m != "apt-get update -o DPkg::Lock::Timeout=300" && !strings.HasPrefix(m, "apt-get -s ") && !strings.HasPrefix(m, "apt-config ") && !strings.HasPrefix(m, "curl ") {
			t.Errorf("--check chạy lệnh thay đổi: %s", m)
		}
		refreshed = refreshed || strings.HasPrefix(m, "apt-get update")
	}
	if !refreshed {
		t.Error("--check không làm mới danh sách gói")
	}

	code, out, _ = c.run("update", "--check", "--json")
	var st core.UpdateStatus
	if err := json.Unmarshal([]byte(out), &st); code != 0 || err != nil || len(st.Packages) != 1 || st.Others != 1 {
		t.Errorf("--json: mã %d, %v\n%s", code, err, out)
	}
}

func TestUpdateNothing(t *testing.T) {
	c := newCLI(t)
	code, out, _ := c.run("update", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "nothing_to_update" || c.dbExists() {
		t.Errorf("mã %d, %s", code, out)
	}
}

func TestUpdateDryRun(t *testing.T) {
	c := newCLI(t)
	c.pendingNginx()
	code, out, errs := c.run("update", "--dry-run")
	if code != 0 || !strings.Contains(out, "--only-upgrade") || !strings.Contains(out, "systemctl restart nginx") || strings.Contains(out, "vim") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	for _, m := range c.mutating() {
		if !strings.HasPrefix(m, "apt-get -s") && m != "nginx -t" {
			t.Errorf("--dry-run chạy lệnh thay đổi: %s", m)
		}
	}
	if c.dbExists() {
		t.Error("--dry-run tạo DB")
	}
	// Không --yes, không terminal: không hỏi được thì từ chối, không chạy gì.
	code, out, _ = c.run("update", "--json")
	if code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}
}

// Job chạy xong thì health chạy lại; site không đạt được nêu tên và mã thoát 1.
// Ở máy giả, phép kiểm HTTP của site không đạt nên kiểm chứng làm job hoàn tác,
// mà health vẫn phải chạy (gói đã nâng không hạ về được).
func TestUpdateRunsHealthAfterJob(t *testing.T) {
	c := newCLI(t)
	c.seed(stackNginx, "shop.example.com")
	c.pendingNginx()
	code, out, errs := c.run("update", "--yes")
	if code != 1 {
		t.Errorf("mã %d, muốn 1", code)
	}
	if !strings.Contains(out, "Kiểm lại toàn bộ health") || !strings.Contains(out, "Site shop.example.com: KHÔNG ĐẠT") {
		t.Errorf("thiếu tóm tắt health:\n%s\n%s", out, errs)
	}
	if c.audits("update.apply") != 1 {
		t.Error("không ghi audit update.apply")
	}

	// --json: stdout chỉ có một JSON (Report), tóm tắt health ra stderr.
	code, out, errs = c.run("update", "--yes", "--json")
	var rep map[string]any
	if err := json.Unmarshal([]byte(out), &rep); err != nil || code != 1 || !strings.Contains(errs, "KHÔNG ĐẠT") {
		t.Errorf("--json: mã %d, %v\nstdout %s\nstderr %s", code, err, out, errs)
	}
}

// Nhánh thành công: stack chưa ghi trong settings nên kiểm chứng trống, job
// xong; health chạy lại và không có gì không đạt thì mã 0.
func TestUpdateSucceeds(t *testing.T) {
	c := newCLI(t)
	c.pendingNginx()
	code, out, errs := c.run("update", "--yes")
	if code != 0 || !strings.Contains(out, "thành công") || !strings.Contains(out, "Stack và mọi site vẫn đạt") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
}

func TestUpdateAutoDryRun(t *testing.T) {
	c := newCLI(t)
	code, out, errs := c.run("update", "auto", "--dry-run")
	if code != 0 || !strings.Contains(out, "/etc/apt/apt.conf.d/52tpan-unattended-upgrades") || !strings.Contains(out, "unattended-upgrades") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if c.dbExists() {
		t.Error("--dry-run tạo DB")
	}
}
