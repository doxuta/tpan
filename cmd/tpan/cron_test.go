package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

func (c *cli) cronRuns() map[string]core.CronRun {
	c.t.Helper()
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	rs, err := st.CronRuns(context.Background())
	if err != nil {
		c.t.Fatal(err)
	}
	out := map[string]core.CronRun{}
	for _, r := range rs {
		out[r.Domain] = r
	}
	return out
}

// Dòng cron gọi `tpan cron run`: im lặng và thoát 0 kể cả khi wp lỗi (kết
// quả đã ở tpan.db); site không có thì in và thoát 1.
func TestCronRunCLI(t *testing.T) {
	c := newCLI(t)
	c.trashSeed(true,
		site.Site{Domain: domainOf(t, "shop.example.com"), Status: site.Active, PHPVersion: "8.3"},
		site.Site{Domain: domainOf(t, "old.example.com"), Status: site.Trashed, PHPVersion: "8.3"})
	wpCode := 0
	c.fake.Handle([]string{"wp", "cron", "event", "run"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: wpCode, Stderr: "Error: giả lập\n"}
	})

	for _, code := range []int{0, 1} {
		wpCode = code
		got, out, errs := c.run("cron", "run", "shop.example.com")
		if got != 0 || out != "" || errs != "" {
			t.Errorf("wp thoát %d: tpan thoát %d, stdout %q, stderr %q", code, got, out, errs)
		}
		r := c.cronRuns()["shop.example.com"]
		if (code == 0) != (r.Status == core.CronOK) || r.ExitCode != code {
			t.Errorf("wp thoát %d: dòng cron_runs %+v", code, r)
		}
	}
	if got, out, errs := c.run("cron", "run", "old.example.com"); got != 0 || out+errs != "" || c.cronRuns()["old.example.com"].Status != core.CronSkipped {
		t.Errorf("site trong thùng rác: mã %d, %q", got, out+errs)
	}
	if got, _, errs := c.run("cron", "run", "none.example.com"); got != 1 || !strings.Contains(errs, "none.example.com") {
		t.Errorf("site không có: mã %d, %q", got, errs)
	}

	c.root = false
	if got, _, errs := c.run("cron", "run", "shop.example.com"); got != 1 || !strings.Contains(errs, "sudo") {
		t.Errorf("không root: mã %d, %q", got, errs)
	}
}

func TestCronListCLI(t *testing.T) {
	c := newCLI(t)
	if code, out, errs := c.run("cron", "list"); code != 0 || !strings.Contains(out, "Chưa có site") || c.dbExists() {
		t.Errorf("rỗng: mã %d, DB %v\n%s%s", code, c.dbExists(), out, errs)
	}
	c.trashSeed(true, site.Site{Domain: domainOf(t, "shop.example.com"), Status: site.Active, PHPVersion: "8.3"})
	l := site.LayoutOf(domainOf(t, "shop.example.com"))
	c.fake.SetFile(site.CronFile(l), []byte("x\n"), 0o644, system.Root)
	if code, _, errs := c.run("cron", "run", "shop.example.com"); code != 0 {
		t.Fatalf("cron run: %d %s", code, errs)
	}

	code, out, errs := c.run("cron", "list", "--json")
	if code != 0 {
		t.Fatalf("mã %d: %s", code, errs)
	}
	var ov core.CronOverview
	if err := json.Unmarshal([]byte(out), &ov); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(ov.Sites) != 1 || !ov.Sites[0].Enabled || ov.Sites[0].Last == nil || ov.Backup.Keep != core.DefaultBackupKeep {
		t.Errorf("JSON = %s", out)
	}
	if code, out, _ := c.run("cron", "list"); code != 0 || !strings.Contains(out, "shop.example.com") || !strings.Contains(out, "Lịch backup: tắt") {
		t.Errorf("bảng: mã %d\n%s", code, out)
	}

	c.root = false
	if code, _, _ := c.run("cron", "list"); code != 1 {
		t.Errorf("không root: mã %d", code)
	}
}
