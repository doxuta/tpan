package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// trashSeed ghi stack và các site vào DB của CLI trước khi chạy lệnh.
func (c *cli) trashSeed(stack bool, sites ...site.Site) {
	c.t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if stack {
		for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
			if err := st.SetSetting(ctx, k, v); err != nil {
				c.t.Fatal(err)
			}
		}
	}
	for _, s := range sites {
		if err := st.SaveSite(ctx, s); err != nil {
			c.t.Fatal(err)
		}
	}
}

// trashAudits đếm dòng nhật ký theo action và result (audits của plan_test chỉ theo action).
func (c *cli) trashAudits(action, result string) int {
	c.t.Helper()
	db, err := sql.Open("sqlite", c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit WHERE action = ? AND result = ?", action, result).Scan(&n); err != nil {
		c.t.Fatal(err)
	}
	return n
}

func domainOf(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTrashDomainRejectedBeforeAnything(t *testing.T) {
	for _, bad := range []string{"a.com; rm -rf /", "$(id).com", "../../etc/passwd", "a.com\nb.com"} {
		for _, args := range [][]string{
			{"site", "restore", bad, "--yes"},
			{"site", "restore", bad, "--dry-run"},
			{"trash", "purge", bad, "--yes"},
			{"trash", "purge", bad, "--dry-run"},
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

func TestTrashNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"site", "restore", "shop.example.com", "--yes"},
		{"trash", "list"},
		{"trash", "settings"},
		{"trash", "settings", "--days", "3"},
		{"trash", "purge", "shop.example.com", "--yes"},
		{"trash", "purge", "--expired", "--yes"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") || c.dbExists() {
			t.Errorf("%q: mã %d, stderr %q, DB %v", args, code, errs, c.dbExists())
		}
	}
}

func TestTrashPurgeArgs(t *testing.T) {
	for _, args := range [][]string{
		{"trash", "purge", "--json"},
		{"trash", "purge", "shop.example.com", "--expired", "--json"},
	} {
		c := newCLI(t)
		code, out, _ := c.run(args...)
		if code != 1 || refusalCode(t, out) != "args_invalid" || len(c.fake.Ran()) != 0 {
			t.Errorf("%q: mã %d, %s", args, code, out)
		}
	}
}

func TestTrashListAndSettings(t *testing.T) {
	c := newCLI(t)
	if code, out, _ := c.run("trash", "list"); code != 0 || !strings.Contains(out, "Thùng rác trống") {
		t.Errorf("list rỗng: mã %d, %q", code, out)
	}
	if code, out, _ := c.run("trash", "list", "--json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("list rỗng --json: mã %d, %q", code, out)
	}

	now := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	c.trashSeed(false,
		site.Site{Domain: domainOf(t, "a.example.com"), Status: site.Trashed, PHPVersion: "8.3", TrashedAt: now, PurgeAt: now.AddDate(0, 0, 7)},
		site.Site{Domain: domainOf(t, "b.example.com"), Status: site.Trashed, PHPVersion: "8.3", TrashedAt: now, PurgeAt: now.AddDate(0, 0, 1)},
		site.Site{Domain: domainOf(t, "c.example.com"), Status: site.Active, PHPVersion: "8.3"},
	)
	code, out, _ := c.run("trash", "list")
	ia, ib := strings.Index(out, "a.example.com"), strings.Index(out, "b.example.com")
	if code != 0 || ia < 0 || ib < 0 || ib > ia || strings.Contains(out, "c.example.com") || !strings.Contains(out, "HẠN XOÁ HẲN") {
		t.Errorf("list: mã %d; muốn b (hết hạn trước) rồi a, không có site active\n%s", code, out)
	}
	_, out, _ = c.run("trash", "list", "--json")
	var list []site.Site
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list) != 2 || list[0].Domain.String() != "b.example.com" {
		t.Errorf("list --json = %+v, %v", list, err)
	}

	if code, out, _ := c.run("trash", "settings"); code != 0 || !strings.Contains(out, ": 7 ") {
		t.Errorf("settings mặc định: mã %d, %q", code, out)
	}
	if code, out, _ := c.run("trash", "settings", "--days", "0", "--json"); code != 0 || strings.TrimSpace(out) != "{\n  \"days\": 0\n}" {
		t.Errorf("settings --days 0: mã %d, %q", code, out)
	}
	if code, out, _ := c.run("trash", "settings", "--days", "366", "--json"); code != 1 || refusalCode(t, out) != "trash_days_invalid" {
		t.Errorf("settings --days 366: mã %d, %q", code, out)
	}
	if code, out, _ := c.run("trash", "settings", "--json"); code != 0 || !strings.Contains(out, `"days": 0`) {
		t.Errorf("số bị từ chối mà vẫn ghi: mã %d, %q", code, out)
	}
	if c.trashAudits("trash.settings", "ok") != 1 || c.trashAudits("trash.settings", "trash_days_invalid") != 1 {
		t.Error("đặt số ngày giữ không vào audit")
	}
	if len(c.fake.Ran()) != 0 {
		t.Errorf("list/settings chạy lệnh trên máy: %v", c.fake.Ran())
	}
}

func TestTrashPurgeAndRestoreRefusals(t *testing.T) {
	c := newCLI(t)
	c.trashSeed(true, site.Site{Domain: domainOf(t, "live.example.com"), Status: site.Active, PHPVersion: "8.3"})
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"trash", "purge", "nothere.example.com", "--yes"}, "site_not_found"},
		{[]string{"trash", "purge", "live.example.com", "--yes"}, "site_not_trashed"},
		{[]string{"trash", "purge", "live.example.com", "--dry-run"}, "site_not_trashed"},
		{[]string{"site", "restore", "live.example.com", "--yes"}, "site_not_trashed"},
		{[]string{"site", "restore", "nothere.example.com", "--dry-run"}, "site_not_found"},
	} {
		code, out, _ := c.run(append(tc.args, "--json")...)
		if code != 1 || refusalCode(t, out) != tc.code {
			t.Errorf("%q: mã %d, %s; muốn %s", tc.args, code, out, tc.code)
		}
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("từ chối mà chạy lệnh thay đổi máy: %v", m)
	}
}

// --expired: xem trước liệt kê đúng site quá hạn; không --yes và không có
// terminal thì từ chối; --yes thì xoá hẳn, xoá dòng và ghi audit.
func TestTrashPurgeExpired(t *testing.T) {
	c := newCLI(t)
	if code, out, _ := c.run("trash", "purge", "--expired", "--yes"); code != 0 || !strings.Contains(out, "Không có site nào quá hạn") {
		t.Errorf("không có gì quá hạn: mã %d, %q", code, out)
	}

	past := time.Now().Add(-time.Hour)
	c.trashSeed(true,
		site.Site{Domain: domainOf(t, "old.example.com"), Status: site.Trashed, PHPVersion: "8.3", TrashedAt: past, PurgeAt: past},
		site.Site{Domain: domainOf(t, "new.example.com"), Status: site.Trashed, PHPVersion: "8.3", TrashedAt: past, PurgeAt: time.Now().Add(48 * time.Hour)},
	)
	// Máy giả: user, DB, thư mục của site đều đã không còn, mọi bước tự bỏ qua.
	c.fake.Handle([]string{"getent"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	c.fake.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })

	code, out, errs := c.run("trash", "purge", "--expired", "--dry-run", "--json")
	var rows []struct {
		Domain  string          `json:"domain"`
		Preview json.RawMessage `json:"preview"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 || len(rows) != 1 || rows[0].Domain != "old.example.com" || len(rows[0].Preview) == 0 {
		t.Fatalf("--dry-run --json: mã %d, %v, %q, %s", code, err, out, errs)
	}
	code, out, _ = c.run("trash", "purge", "--expired", "--dry-run")
	if code != 0 || !strings.Contains(out, "== old.example.com") || strings.Contains(out, "new.example.com") || !strings.Contains(out, "tự backup site") {
		t.Errorf("--dry-run: mã %d\n%s", code, out)
	}
	if code, _, errs := c.run("trash", "purge", "--expired"); code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("không --yes, không terminal: mã %d, %q", code, errs)
	}
	for _, cmd := range c.fake.Ran() {
		if cmd.Name != "getent" && cmd.Name != "mariadb" {
			t.Errorf("chưa xác nhận mà chạy %s", cmd.String())
		}
	}

	code, out, errs = c.run("trash", "purge", "--expired", "--yes", "--json")
	var res []core.PurgeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 || len(res) != 1 || res[0].Domain.String() != "old.example.com" || res[0].Error != "" {
		t.Fatalf("--yes: mã %d, %v, %q, %s", code, err, out, errs)
	}
	if c.trashAudits("site.purge", "ok") != 1 {
		t.Error("xoá hẳn không vào audit")
	}
	_, out, _ = c.run("trash", "list", "--json")
	if !strings.Contains(out, "new.example.com") || strings.Contains(out, "old.example.com") {
		t.Errorf("sau --expired, thùng rác = %s", out)
	}
}

func TestSiteRmHelpSaysTrash(t *testing.T) {
	c := newCLI(t)
	_, out, _ := c.run("site", "rm", "--help")
	if !strings.Contains(out, "thùng rác") || !strings.Contains(out, "site restore") {
		t.Errorf("help của site rm không nói thùng rác:\n%s", out)
	}
}
