package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// selfPressed ghi dấu "đang chạy" như core.SelfUpdate.Start ghi khi admin bấm
// nút trong panel: lệnh nội bộ đọc người bấm và IP từ đây.
func (c *cli) selfPressed(action string) {
	c.t.Helper()
	run := core.SelfRun{Action: action, Status: core.SelfRunning, From: buildinfo.Version, Actor: "admin", IP: "203.0.113.7", Started: time.Now()}
	b, err := json.Marshal(run)
	if err != nil {
		c.t.Fatal(err)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if err := st.SetSetting(ctx, core.SettingSelfLast, string(b)); err != nil {
			c.t.Fatal(err)
		}
	})
}

// selfLast đọc lượt chạy mà lệnh nội bộ đã ghi vào settings.
func (c *cli) selfLast() core.SelfRun {
	c.t.Helper()
	var run core.SelfRun
	if err := json.Unmarshal([]byte(c.setting(core.SettingSelfLast)), &run); err != nil {
		c.t.Fatalf("settings %s: %v", core.SettingSelfLast, err)
	}
	return run
}

// selfAudits trả mọi dòng audit của thao tác action, mỗi dòng dạng
// "ip|actor|target|result|có job hay không".
func (c *cli) selfAudits(action string) []string {
	c.t.Helper()
	db, err := sql.Open("sqlite", c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT ip, actor, target, result, job_id FROM audit WHERE action = ? ORDER BY rowid", action)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var ip, actor, target, result, job string
		if err := rows.Scan(&ip, &actor, &target, &result, &job); err != nil {
			c.t.Fatal(err)
		}
		hasJob := "không job"
		if job != "" {
			hasJob = "có job"
		}
		out = append(out, strings.Join([]string{ip, actor, target, result, hasJob}, "|"))
	}
	if err := rows.Err(); err != nil {
		c.t.Fatal(err)
	}
	return out
}

func TestInternalSelfUpdateFromPanel(t *testing.T) {
	c := newCLI(t)
	c.selfRelease("v9.9.9")
	c.selfPressed(core.SelfActionUpdate)
	code, out, errs := c.run("internal", "self-update")
	if code != 0 {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	// stdout của đơn vị tạm là thứ `journalctl -u tpan-self-update` cho xem:
	// tiến độ từng bước, báo cáo job, một dòng kết luận.
	for _, want := range []string{"Bắt đầu job", "Tải bản tpan mới", "thành công", "tpan đã đổi: " + buildinfo.Version + " -> v9.9.9"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout thiếu %q:\n%s", want, out)
		}
	}
	want := []string{"203.0.113.7|admin|" + buildinfo.Version + " -> v9.9.9|ok|có job"}
	if got := c.selfAudits("self.update"); !slices.Equal(got, want) {
		t.Errorf("audit = %q\nmuốn  %q", got, want)
	}
	run := c.selfLast()
	if run.Status != core.SelfOK || run.To != "v9.9.9" || run.Actor != "admin" || run.IP != "203.0.113.7" || run.JobID == "" || run.Finished.IsZero() {
		t.Errorf("settings = %+v", run)
	}
	if b, _ := c.fake.ReadFile(core.TpanBin); string(b) != "tpan v9.9.9" {
		t.Errorf("TpanBin = %q", b)
	}
	// Gốc tải là hằng hoặc settings, không lấy từ dòng lệnh.
	for _, cmd := range c.fake.Ran() {
		if cmd.Name == "curl" && !strings.HasPrefix(cmd.Args[len(cmd.Args)-1], core.DefaultSelfURL+"/") {
			t.Errorf("curl tải từ chỗ lạ: %s", cmd.String())
		}
	}
}

func TestInternalSelfUpdateSameVersion(t *testing.T) {
	c := newCLI(t)
	c.selfRelease(buildinfo.Version)
	c.selfPressed(core.SelfActionUpdate)
	code, out, errs := c.run("internal", "self-update")
	if code != 0 || !strings.Contains(out, "Đang là bản mới nhất") {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	want := []string{"203.0.113.7|admin|" + buildinfo.Version + " -> " + buildinfo.Version + "|same|có job"}
	if got := c.selfAudits("self.update"); !slices.Equal(got, want) {
		t.Errorf("audit = %q\nmuốn  %q", got, want)
	}
	if run := c.selfLast(); run.Status != core.SelfSame {
		t.Errorf("settings = %+v", run)
	}
}

func TestInternalSelfUpdateBadChecksum(t *testing.T) {
	c := newCLI(t)
	c.selfRelease("v9.9.9")
	c.fake.Handle([]string{"curl", "-fsSL", "--max-time", "300"}, func(cmd system.Cmd) system.Result {
		c.fake.SetFile(cmd.Args[slices.Index(cmd.Args, "-o")+1], []byte("file đã bị sửa giữa đường"), 0o644, system.Root)
		return system.Result{}
	})
	c.selfPressed(core.SelfActionUpdate)
	code, out, errs := c.run("internal", "self-update")
	if code != 1 || !strings.Contains(out, "sha256") || !strings.Contains(out, "hỏng, đã hoàn tác sạch") {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	want := []string{"203.0.113.7|admin|" + buildinfo.Version + " -> " + buildinfo.Version + "|rolled_back|có job"}
	if got := c.selfAudits("self.update"); !slices.Equal(got, want) {
		t.Errorf("audit = %q\nmuốn  %q", got, want)
	}
	if run := c.selfLast(); run.Status != core.SelfFailed || !strings.Contains(run.Error, "sha256") {
		t.Errorf("settings = %+v", run)
	}
	if b, _ := c.fake.ReadFile(core.TpanBin); string(b) != "tpan "+buildinfo.Version {
		t.Errorf("file chạy cũ bị đổi: %q", b)
	}
}

func TestInternalSelfUpdateRollback(t *testing.T) {
	// Bị từ chối trước khi thành job: không có file cũ.
	c := newCLI(t)
	c.selfRelease("v9.9.9")
	c.selfPressed(core.SelfActionRollback)
	code, out, errs := c.run("internal", "self-update", "--rollback")
	if code != 1 || !strings.Contains(errs, "Từ chối") || !strings.Contains(errs, core.TpanBin+".old") {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	want := []string{"203.0.113.7|admin|" + buildinfo.Version + " -> ?|self_no_old|không job"}
	if got := c.selfAudits("self.rollback"); !slices.Equal(got, want) {
		t.Errorf("audit = %q\nmuốn  %q", got, want)
	}
	if run := c.selfLast(); run.Status != core.SelfFailed || run.Error == "" || run.JobID != "" {
		t.Errorf("settings = %+v", run)
	}

	// Có file cũ (bản v0.0.1), và lần này gõ tay trên máy chủ: IP cli, không
	// có người bấm.
	c = newCLI(t)
	c.selfRelease("v9.9.9")
	c.fake.SetFile(core.TpanBin+".old", []byte("tpan v0.0.1"), 0o755, system.Root)
	code, out, errs = c.run("internal", "self-update", "--rollback")
	if code != 0 || !strings.Contains(out, "tpan đã quay về bản trước: "+buildinfo.Version+" -> v0.0.1") {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	want = []string{"cli||" + buildinfo.Version + " -> v0.0.1|ok|có job"}
	if got := c.selfAudits("self.rollback"); !slices.Equal(got, want) {
		t.Errorf("audit = %q\nmuốn  %q", got, want)
	}
	if run := c.selfLast(); run.Status != core.SelfOK || run.IP != "cli" || run.Actor != "" || run.To != "v0.0.1" {
		t.Errorf("settings = %+v", run)
	}
	if b, _ := c.fake.ReadFile(core.TpanBin); string(b) != "tpan v0.0.1" {
		t.Errorf("TpanBin = %q", b)
	}
}

func TestInternalSelfUpdateArgs(t *testing.T) {
	// Lệnh nội bộ không nhận gì ngoài --rollback: gốc tải, người bấm, IP đều
	// không đi qua dòng lệnh.
	for _, args := range [][]string{
		{"internal", "self-update", "https://example.com/rel"},
		{"internal", "self-update", "--url", "https://example.com/rel"},
		{"internal", "self-update", "--yes"},
		{"internal", "self-update", "--actor", "admin"},
		{"internal", "self-update", "--rollback", "v0.0.1"},
	} {
		c := newCLI(t)
		c.selfRelease("v9.9.9")
		if code, out, _ := c.run(args...); code != 1 || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%v: mã %d, lệnh %v, có DB %v\n%s", args, code, c.fake.Ran(), c.dbExists(), out)
		}
	}

	// Không phải root: từ chối trước khi mở DB.
	c := newCLI(t)
	c.root = false
	c.selfRelease("v9.9.9")
	code, _, errs := c.run("internal", "self-update")
	if code != 1 || !strings.Contains(errs, "sudo") || len(c.fake.Ran()) != 0 || c.dbExists() {
		t.Errorf("không root: mã %d, %q, lệnh %v", code, errs, c.fake.Ran())
	}

	// Ẩn khỏi help như mọi lệnh nội bộ.
	c = newCLI(t)
	if _, out, _ := c.run("--help"); strings.Contains(out, "internal") {
		t.Errorf("help lộ lệnh nội bộ:\n%s", out)
	}
}
