package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// bkSites ghi stack và các site active vào DB, và dựng trên máy giả đủ những
// gì backup cần: thư mục public, câu đo cỡ DB, du, sha256sum, xuất DB, tar giả.
func (c *cli) bkSites(domains ...string) {
	c.t.Helper()
	var ss []site.Site
	for _, d := range domains {
		st := site.Site{Domain: domainOf(c.t, d), Status: site.Active, PHPVersion: "8.3"}
		ss = append(ss, st)
		l := st.Layout()
		c.fake.SetDir(l.Public, 0o750, system.Owner{User: l.User, Group: site.WebGroup})
	}
	c.trashSeed(true, ss...)
	c.fake.SetDir("/var/lib/tpan", 0o755, system.Root)
	c.fake.SetDir("/etc/cron.d", 0o755, system.Root)
	f := c.fake
	f.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result { return system.Result{Stdout: "1000\n"} })
	f.Handle([]string{"du"}, func(cmd system.Cmd) system.Result {
		return system.Result{Stdout: "5000\t" + cmd.Args[len(cmd.Args)-1] + "\n"}
	})
	f.Handle([]string{"sha256sum"}, func(cmd system.Cmd) system.Result {
		p := cmd.Args[len(cmd.Args)-1]
		data, err := f.ReadFile(p)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: err.Error()}
		}
		sum := sha256.Sum256(data)
		return system.Result{Stdout: hex.EncodeToString(sum[:]) + "  " + p + "\n"}
	})
	f.Handle([]string{"mariadb-dump"}, func(cmd system.Cmd) system.Result {
		for _, a := range cmd.Args {
			if p, ok := strings.CutPrefix(a, "--result-file="); ok {
				f.SetFile(p, []byte("-- Dump completed on 2026-11-20\n"), 0o600, system.Root)
			}
		}
		return system.Result{}
	})
	f.Handle([]string{"tail", "-n", "1"}, func(system.Cmd) system.Result { return system.Result{Stdout: "-- Dump completed on 2026-11-20\n"} })
	f.Handle([]string{"tar", "--create"}, func(cmd system.Cmd) system.Result {
		for _, a := range cmd.Args {
			if p, ok := strings.CutPrefix(a, "--file="); ok {
				f.SetFile(p, []byte("tar giả"), 0o600, system.Root)
			}
		}
		return system.Result{}
	})
}

// bkRows đọc bảng backups của DB CLI.
func (c *cli) bkRows(domain string) []core.Backup {
	c.t.Helper()
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	rows, err := st.Backups(context.Background(), domain)
	if err != nil {
		c.t.Fatal(err)
	}
	return rows
}

// bkRan: tar, mariadb-dump hay rm đã chạy (xem trước chỉ được đo).
func (c *cli) bkRan() []string {
	var bad []string
	for _, cmd := range c.fake.Ran() {
		switch cmd.Name {
		case "tar", "mariadb-dump", "rm", "wp":
			bad = append(bad, cmd.String())
		}
	}
	return bad
}

func (c *cli) bkExists(p string) bool { _, err := c.fake.Stat(p); return err == nil }

func TestBackupDomainRejectedBeforeAnything(t *testing.T) {
	for _, bad := range []string{"a.com; rm -rf /", "$(id).com", "../../etc/passwd", "a.com\nb.com"} {
		for _, args := range [][]string{
			{"backup", "create", bad, "--yes"},
			{"backup", "create", bad, "--dry-run"},
			{"backup", "restore", bad, "--yes"},
			{"backup", "restore", bad, "--dry-run"},
			{"backup", "list", bad},
		} {
			c := newCLI(t)
			code, out, errs := c.run(args...)
			if code != 1 || !strings.Contains(errs, "Từ chối:") || out != "" {
				t.Errorf("%q: mã %d, stdout %q, stderr %q", args, code, out, errs)
			}
			if len(c.fake.Ran()) != 0 || c.dbExists() {
				t.Errorf("%q: chạy lệnh %v hoặc tạo DB", args, c.fake.Ran())
			}
		}
	}
}

func TestBackupNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"backup", "create", "shop.example.com", "--yes"},
		{"backup", "create", "--all", "--yes"},
		{"backup", "restore", "shop.example.com", "--yes"},
		{"backup", "list"},
		{"backup", "settings"},
		{"backup", "settings", "--keep", "3", "--yes"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") || c.dbExists() {
			t.Errorf("%q: mã %d, stderr %q, DB %v", args, code, errs, c.dbExists())
		}
	}
}

func TestBackupArgs(t *testing.T) {
	for _, args := range [][]string{
		{"backup", "create", "--json"},
		{"backup", "create", "shop.example.com", "--all", "--json"},
		{"backup", "create", "shop.example.com", "--scheduled", "--json"},
		{"backup", "settings", "--at", "03:30", "--off", "--json"},
	} {
		c := newCLI(t)
		code, out, _ := c.run(args...)
		if code != 1 || refusalCode(t, out) != "args_invalid" || len(c.fake.Ran()) != 0 {
			t.Errorf("%q: mã %d, %s", args, code, out)
		}
	}
}

func TestBackupCreate(t *testing.T) {
	const dom = "shop.example.com"
	c := newCLI(t)
	c.bkSites(dom)

	// Xem trước: không tar, không xuất DB.
	code, out, errs := c.run("backup", "create", dom, "--dry-run")
	if code != 0 || !strings.Contains(out, "Backup site "+dom) || !strings.Contains(out, "tar --create") {
		t.Fatalf("--dry-run: mã %d\n%s\n%s", code, out, errs)
	}
	for _, m := range c.bkRan() {
		t.Errorf("--dry-run chạy %s", m)
	}
	// Không --yes, không terminal: không chạy gì.
	if code, out, _ := c.run("backup", "create", dom, "--json"); code != 1 || refusalCode(t, out) != "confirm_required" || len(c.bkRan()) != 0 {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}

	code, out, errs = c.run("backup", "create", dom, "--yes", "--json")
	if code != 0 {
		t.Fatalf("chạy: mã %d\n%s\n%s", code, out, errs)
	}
	rows := c.bkRows(dom)
	if len(rows) != 1 || rows[0].Kind != core.BackupFull || rows[0].Reason != core.ReasonManual || !c.bkExists(rows[0].Path) {
		t.Errorf("bảng backups = %+v", rows)
	}
	if n := c.trashAudits("backup.create", "ok"); n != 1 {
		t.Errorf("audit backup.create = %d", n)
	}

	// list: JSON và bảng.
	_, out, _ = c.run("backup", "list", "--json")
	var list []core.Backup
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list) != 1 || list[0].ID != rows[0].ID {
		t.Errorf("list --json = %s, %v", out, err)
	}
	if _, out, _ := c.run("backup", "list", dom); !strings.Contains(out, rows[0].ID) || !strings.Contains(out, rows[0].Path) {
		t.Errorf("list:\n%s", out)
	}
	if _, out, _ := c.run("backup", "list", "blog.example.com"); !strings.Contains(out, "Chưa có bản backup nào") {
		t.Errorf("list site không có backup:\n%s", out)
	}
}

func TestBackupCreateDiskLow(t *testing.T) {
	c := newCLI(t)
	c.bkSites("shop.example.com")
	c.fake.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 9 << 30})
	code, out, _ := c.run("backup", "create", "shop.example.com", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != core.CodeDiskSpaceLow || !strings.Contains(out, "GiB") || len(c.bkRan()) != 0 {
		t.Errorf("đĩa gần đầy: mã %d, %s", code, out)
	}
}

func TestBackupCreateAllDryRun(t *testing.T) {
	c := newCLI(t)
	c.bkSites("a.example.com", "b.example.com")
	code, out, errs := c.run("backup", "create", "--all", "--scheduled", "--dry-run")
	if code != 0 || !strings.Contains(out, "== a.example.com") || !strings.Contains(out, "== b.example.com") || !strings.Contains(out, site.TPANDBBackupDir) {
		t.Errorf("--all --dry-run: mã %d\n%s\n%s", code, out, errs)
	}
	_, out, _ = c.run("backup", "create", "--all", "--dry-run", "--json")
	var rows []backupPreviewRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 || rows[0].Preview == nil {
		t.Errorf("--all --dry-run --json = %s, %v", out, err)
	}
	for _, m := range c.bkRan() {
		t.Errorf("--dry-run chạy %s", m)
	}
}

// --all chạy thật: mỗi site một job và một dòng audit; không chép được
// tpan.db thì báo và thoát 1 (thư mục bản sao bị chặn tạo, nên không đụng
// tới SnapshotDB của store, vốn ghi vào đĩa thật).
func TestBackupCreateAllRun(t *testing.T) {
	c := newCLI(t)
	c.bkSites("a.example.com")
	c.fake.Fail("mkdir", site.TPANDBBackupDir, errors.New("giả lập: đĩa chỉ đọc"))
	code, out, errs := c.run("backup", "create", "--all", "--yes")
	if code != 1 || !strings.Contains(out, "== a.example.com") || !strings.Contains(out, "Không chép được tpan.db") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if n := c.trashAudits("backup.create", "ok"); n != 1 {
		t.Errorf("audit backup.create = %d", n)
	}
	if rows := c.bkRows("a.example.com"); len(rows) != 1 || rows[0].Reason != core.ReasonManual {
		t.Errorf("bảng backups = %+v", rows)
	}
}

func TestBackupRestoreRefused(t *testing.T) {
	const dom = "shop.example.com"
	c := newCLI(t)
	c.bkSites(dom)
	if code, out, _ := c.run("backup", "restore", dom, "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "backup_not_found" {
		t.Errorf("chưa có backup: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("backup", "restore", dom, "--id", "$(id)", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "backup_not_found" {
		t.Errorf("id lạ: mã %d, %s", code, out)
	}
	c.trashSeed(false, site.Site{Domain: domainOf(t, "t.example.com"), Status: site.Trashed, PHPVersion: "8.3"})
	if code, out, _ := c.run("backup", "restore", "t.example.com", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "site_trashed" {
		t.Errorf("site trong thùng rác: mã %d, %s", code, out)
	}
}

func TestBackupSettings(t *testing.T) {
	c := newCLI(t)
	c.bkSites()
	if code, out, _ := c.run("backup", "settings"); code != 0 || !strings.Contains(out, "Số bản giữ mỗi site: 3") || !strings.Contains(out, "tắt") {
		t.Errorf("xem mặc định: mã %d\n%s", code, out)
	}
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"--keep", "0"}, "backup_keep_invalid"},
		{[]string{"--keep", "31"}, "backup_keep_invalid"},
		{[]string{"--at", "25:00"}, "backup_at_invalid"},
		{[]string{"--at", "03:30; rm -rf /"}, "backup_at_invalid"},
	} {
		code, out, _ := c.run(append([]string{"backup", "settings", "--dry-run", "--json"}, tc.args...)...)
		if code != 1 || refusalCode(t, out) != tc.code {
			t.Errorf("%q: mã %d, %s", tc.args, code, out)
		}
	}

	// Bật lịch.
	if code, out, errs := c.run("backup", "settings", "--keep", "5", "--at", "03:30", "--yes"); code != 0 {
		t.Fatalf("đặt: mã %d\n%s\n%s", code, out, errs)
	}
	data, err := c.fake.ReadFile(site.BackupCronFile)
	if err != nil || !strings.Contains(string(data), "30 03 * * * root /usr/local/bin/tpan backup create --all --scheduled --yes") {
		t.Errorf("file cron: %v\n%s", err, data)
	}
	if c.setting(core.SettingBackupKeep) != "5" || c.setting(core.SettingBackupAt) != "03:30" || c.trashAudits("backup.settings", "ok") != 1 {
		t.Errorf("setting %q %q, audit %d", c.setting(core.SettingBackupKeep), c.setting(core.SettingBackupAt), c.trashAudits("backup.settings", "ok"))
	}
	// Chỉ --off: giữ số bản đã đặt.
	if code, out, errs := c.run("backup", "settings", "--off", "--yes"); code != 0 {
		t.Fatalf("tắt: mã %d\n%s\n%s", code, out, errs)
	}
	if c.bkExists(site.BackupCronFile) {
		t.Error("tắt mà file cron còn")
	}
	_, out, _ := c.run("backup", "settings", "--json")
	var got core.BackupSettings
	if err := json.Unmarshal([]byte(out), &got); err != nil || got != (core.BackupSettings{Keep: 5}) {
		t.Errorf("xem sau khi tắt: %s, %v", out, err)
	}
}
