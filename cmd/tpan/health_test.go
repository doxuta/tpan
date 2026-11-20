package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// installedCLI là CLI trên một máy giả đã cài stack nginx + PHP 8.3, mọi dịch
// vụ chạy trừ nginx (khi nginxDown true). Bật lại nginx bằng
// `systemctl enable --now nginx` thì nginx chạy.
func installedCLI(t *testing.T) (*cli, *atomic.Bool) {
	t.Helper()
	c := newCLI(t)
	withStore(t, c, func(st *store.Store) {
		for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
			if err := st.SetSetting(context.Background(), k, v); err != nil {
				t.Fatal(err)
			}
		}
	})
	// M14: stack cài bằng bản hiện tại thì OPcache và /proc của PHP-FPM đã được
	// siết (Plan cài stack có hai bước đó). Máy giả này phải ở đúng trạng thái
	// ấy, nếu không `health --fix` trong các test dưới sẽ sửa thêm hai thứ
	// không liên quan tới điều test đang thử (nginx dừng).
	c.fake.SetFile(templates.PHPHardeningINIPath("8.3"), templates.PHPHardeningINI(), 0o644, system.Root)
	c.fake.Handle([]string{"php-fpm8.3", "-i"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "opcache.restrict_api => " + templates.OPcacheAPIDir + " => " + templates.OPcacheAPIDir + "\n" +
			"opcache.validate_permission => On => On\n"}
	})
	c.fake.Handle([]string{"systemctl", "show", "php8.3-fpm", "-p", "ProtectProc"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "invisible\n"}
	})
	var down atomic.Bool
	down.Store(true)
	c.fake.Handle([]string{"systemctl", "is-active"}, func(cmd system.Cmd) system.Result {
		if cmd.Args[1] == "nginx" && down.Load() {
			return system.Result{ExitCode: 3, Stdout: "inactive\n"}
		}
		return system.Result{Stdout: "active\n"}
	})
	c.fake.Handle([]string{"systemctl", "enable", "--now", "nginx"}, func(system.Cmd) system.Result {
		down.Store(false)
		return system.Result{}
	})
	return c, &down
}

// healthMutating là mutating() bỏ thêm các lệnh chỉ đọc mà phép kiểm của
// health chạy: kiểm cú pháp (nginx -t, php-fpm -t) và gọi HTTP tới chính máy.
// M14 thêm: PHP in cấu hình đã gộp (php-fpm -i), systemd in một thuộc tính
// (systemctl show), web server in cấu hình đang nạp (nginx -T, apache2ctl -S),
// và quét .htaccess (find không có -delete, head).
func healthMutating(c *cli) []string {
	var bad []string
	for _, m := range c.mutating() {
		switch {
		case m == "nginx -t", m == "nginx -T", m == "apache2ctl -S",
			strings.HasPrefix(m, "php-fpm") && (strings.HasSuffix(m, " -t") || strings.HasSuffix(m, " -i")),
			strings.HasPrefix(m, "curl "), strings.HasPrefix(m, "systemctl show "), strings.HasPrefix(m, "head "),
			strings.HasPrefix(m, "find ") && !strings.Contains(m, "-delete"):
			continue
		}
		bad = append(bad, m)
	}
	return bad
}

func withStore(t *testing.T, c *cli, fn func(st *store.Store)) {
	t.Helper()
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	fn(st)
}

// result tìm một kết quả theo ID trong báo cáo.
func findResult(rep health.Report, id string) (found bool, status string, autoFix string) {
	for _, g := range rep.Groups {
		for _, r := range g.Results {
			if r.ID == id {
				return true, string(r.Status), r.AutoFix
			}
		}
	}
	return false, "", ""
}

func TestHealthRecordsAndOpensIncidents(t *testing.T) {
	c, _ := installedCLI(t)
	code, out, errs := c.run("health")
	if code != 1 {
		t.Fatalf("nginx dừng mà mã %d; stderr %s", code, errs)
	}
	for _, want := range []string{"== Máy và stack ==", "== tpan ==", "Sự cố vừa mở:", "(service.active:nginx)"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout thiếu %q:\n%s", want, out)
		}
	}
	// Chạy lần hai: sự cố đã mở, không mở lại.
	_, out, _ = c.run("health")
	if strings.Contains(out, "Sự cố vừa mở:") {
		t.Errorf("lần hai mở lại sự cố:\n%s", out)
	}
	withStore(t, c, func(st *store.Store) {
		incs, err := st.Incidents(context.Background(), true, 0)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, in := range incs {
			if in.CheckID == "service.active:nginx" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("sự cố đang mở của nginx: %d, muốn 1 (%+v)", n, incs)
		}
		if m := healthMutating(c); len(m) > 0 {
			t.Errorf("health không --fix mà chạy lệnh thay đổi máy: %v", m)
		}
	})
}

func TestHealthJSON(t *testing.T) {
	c, _ := installedCLI(t)
	code, out, _ := c.run("health", "--json")
	if code != 1 {
		t.Errorf("mã %d", code)
	}
	var rep health.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("stdout không phải JSON của health.Report: %v\n%s", err, out)
	}
	if !rep.Installed || len(rep.Changes.Opened) == 0 {
		t.Errorf("report = installed %v, %d sự cố mở", rep.Installed, len(rep.Changes.Opened))
	}
	if ok, st, fix := findResult(rep, "service.active:nginx"); !ok || st != "fail" || fix != core.FixServiceStart {
		t.Errorf("service.active:nginx = %v %s %q", ok, st, fix)
	}
}

func TestHealthDriftShowsPath(t *testing.T) {
	c, down := installedCLI(t)
	down.Store(false)
	const p = "/etc/nginx/sites-available/shop.example.com.conf"
	c.fake.SetFile(p, []byte("server { # sửa tay\n}\n"), 0o644, system.Root)
	withStore(t, c, func(st *store.Store) {
		if err := st.RecordFile(context.Background(), p, strings.Repeat("a", 64), ""); err != nil {
			t.Fatal(err)
		}
	})
	_, out, _ := c.run("health")
	if !strings.Contains(out, "File bị sửa ngoài tpan (lệch):\n  "+p+"\n") {
		t.Errorf("không nhắc đường dẫn file lệch:\n%s", out)
	}
	// File không thuộc site nào: core không dựng lại được nội dung, nên từ chối
	// sửa phép kiểm này, in lý do và đi tiếp (mã 0 vì --dry-run).
	code, _, errs := c.run("health", "--fix", "--dry-run")
	if code != 0 || !strings.Contains(errs, "Từ chối:") || !strings.Contains(errs, "drift:"+p) {
		t.Errorf("--fix --dry-run với file không dựng lại được: mã %d, stderr %q", code, errs)
	}
}

func TestHealthFixDryRun(t *testing.T) {
	c, _ := installedCLI(t)
	code, out, errs := c.run("health", "--fix", "--dry-run")
	if code != 0 {
		t.Fatalf("--fix --dry-run: mã %d; stderr %s", code, errs)
	}
	if !strings.Contains(out, "Sửa service.active:nginx (service.start)") || !strings.Contains(out, "$ systemctl enable --now nginx") {
		t.Errorf("thiếu bản xem trước:\n%s", out)
	}
	if strings.Contains(out, "Kiểm lại sau khi sửa") {
		t.Errorf("--dry-run mà vẫn kiểm lại như đã sửa:\n%s", out)
	}
	if m := healthMutating(c); len(m) > 0 {
		t.Errorf("--dry-run chạy lệnh thay đổi máy: %v", m)
	}
}

func TestHealthFixNeedsConfirm(t *testing.T) {
	c, _ := installedCLI(t)
	code, _, errs := c.run("health", "--fix")
	if code != 1 || !strings.Contains(errs, "--fix --yes") {
		t.Errorf("không terminal, không --yes: mã %d, stderr %q", code, errs)
	}
	if m := healthMutating(c); len(m) > 0 {
		t.Errorf("chưa xác nhận mà chạy lệnh thay đổi máy: %v", m)
	}

	c.terminal, c.stdin = true, "n\n"
	_, out, _ := c.run("health", "--fix")
	if !strings.Contains(out, "Chạy? [y/N]") || !strings.Contains(out, "Bỏ qua") {
		t.Errorf("trả lời n:\n%s", out)
	}
	if m := healthMutating(c); len(m) > 0 {
		t.Errorf("trả lời n mà chạy lệnh thay đổi máy: %v", m)
	}
}

func TestHealthFixYes(t *testing.T) {
	c, _ := installedCLI(t)
	c.run("health") // mở sự cố trước, như bộ hẹn giờ đã làm
	// Mã thoát vẫn 1: máy giả còn lỗi khác không có cách sửa tự động (WP-CLI,
	// cổng 3306...). Mã thoát được thử riêng ở TestHealthExit.
	_, out, _ := c.run("health", "--fix", "--yes")
	for _, want := range []string{"Bắt đầu job", "Kiểm lại sau khi sửa:", "Sự cố vừa đóng:"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout thiếu %q:\n%s", want, out)
		}
	}
	withStore(t, c, func(st *store.Store) {
		ctx := context.Background()
		incs, err := st.Incidents(ctx, false, 0)
		if err != nil {
			t.Fatal(err)
		}
		var jobID string
		for _, in := range incs {
			if in.CheckID == "service.active:nginx" {
				if in.Closed.IsZero() || len(in.Jobs) != 1 {
					t.Errorf("sự cố nginx: đóng %v, jobs %v", in.Closed, in.Jobs)
				} else {
					jobID = in.Jobs[0]
				}
			}
		}
		// Nhật ký: đọc thẳng bảng audit (store chưa có hàm đọc), như admin_test.
		db, err := sql.Open("sqlite", c.db)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		var ip, target, result, job string
		err = db.QueryRow("SELECT ip, target, result, job_id FROM audit WHERE action = 'fix.service.start'").Scan(&ip, &target, &result, &job)
		if err != nil || ip != "cli" || target != "service.active:nginx" || result != "ok" || job != jobID {
			t.Errorf("audit fix.service.start = %s %s %s %s (%v), job của sự cố %q", ip, target, result, job, err, jobID)
		}
	})
}

func TestHealthFixJSON(t *testing.T) {
	c, _ := installedCLI(t)
	_, out, errs := c.run("health", "--fix", "--yes", "--json")
	var got healthFixJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout không phải JSON: %v\n%s", err, out)
	}
	if len(got.Fixes) != 1 || got.Fixes[0].Report == nil || got.Fixes[0].Report.Job.Status != engine.JobSucceeded || got.After == nil {
		t.Fatalf("JSON: %d lần sửa, after %v", len(got.Fixes), got.After != nil)
	}
	if _, st, _ := findResult(*got.After, "service.active:nginx"); st != "pass" {
		t.Errorf("sau khi sửa service.active:nginx = %s", st)
	}
	if !strings.Contains(errs, "Bắt đầu job") {
		t.Errorf("--json: tiến độ phải ra stderr:\n%s", errs)
	}
}

func TestHealthFlagsNeedFix(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{{"health", "--dry-run"}, {"health", "--yes"}} {
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "--fix") {
			t.Errorf("%v: mã %d, stderr %q", args, code, errs)
		}
	}
	if c.dbExists() {
		t.Error("cờ sai mà vẫn mở DB")
	}
}

func TestHealthExit(t *testing.T) {
	group := func(st checks.Status) health.Report {
		return health.Report{Installed: true, Groups: []health.Group{{Results: []checks.Result{{ID: "x", Status: st}}}}}
	}
	for _, tc := range []struct {
		name string
		rep  health.Report
		want bool // true: mã 1
	}{
		{"đạt", group(checks.Pass), false},
		{"cảnh báo (lệch)", group(checks.Warn), false},
		{"chưa rõ", group(checks.Unknown), false},
		{"lỗi", group(checks.Fail), true},
		{"chưa cài", health.Report{}, true},
	} {
		if got := healthExit(tc.rep) != nil; got != tc.want {
			t.Errorf("%s: mã 1 = %v, muốn %v", tc.name, got, tc.want)
		}
	}
}

func TestLineDiff(t *testing.T) {
	old := []byte("a\nb\nc\nd\ne\nf\ng\nh\n")
	for _, tc := range []struct {
		name string
		new  string
		want []string
	}{
		{"giống hệt", "a\nb\nc\nd\ne\nf\ng\nh\n", nil},
		{"bỏ một dòng ở cuối", "a\nb\nc\nd\ne\nf\ng\n", []string{"  e", "  f", "  g", "- h"}},
		{"đổi một dòng giữa", "a\nb\nc\nX\ne\nf\ng\nh\n", []string{"  a", "  b", "  c", "- d", "+ X", "  e", "  f", "  g"}},
		{"thêm ở đầu", "Z\na\nb\nc\nd\ne\nf\ng\nh\n", []string{"+ Z", "  a", "  b", "  c"}},
	} {
		got := lineDiff(old, []byte(tc.new))
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: %q, muốn %q", tc.name, got, tc.want)
		}
	}
	// Bản sửa tay thêm dòng cuối, tpan sẽ ghi lại bản không có nó: dòng đó là "-".
	got := lineDiff([]byte("server {\n}\n# sửa tay\n"), []byte("server {\n}\n"))
	if strings.Join(got, "|") != "  server {|  }|- # sửa tay" {
		t.Errorf("sửa tay ở cuối: %q", got)
	}
	// File khác hoàn toàn: bị chặn ở maxDiffLines, kèm số dòng bị bỏ.
	big := lineDiff([]byte(strings.Repeat("x\n", 100)), []byte(strings.Repeat("y\n", 100)))
	if len(big) != maxDiffLines+1 || big[maxDiffLines] != "... (+120)" {
		t.Errorf("không chặn diff dài: %d dòng, dòng cuối %q", len(big), big[len(big)-1])
	}
}

func TestPrintFileDiffs(t *testing.T) {
	var out strings.Builder
	e := &env{stdout: &out, lang: "vi"}
	const p = "/etc/nginx/sites-available/tpan-shop.example.com.conf"
	pv := engine.Preview{Steps: []engine.StepPreview{
		{Info: engine.StepInfo{ID: "file:" + p, Files: []engine.FileChange{{Path: p, OldExists: true, Old: []byte("server {\n}\n# sửa tay\n"), New: []byte("server {\n}\n")}}}},
		// Bước đã đúng, file mới tạo, file giữ nguyên: không có diff.
		{Done: true, Info: engine.StepInfo{Files: []engine.FileChange{{Path: "/done", OldExists: true, Old: []byte("a\n"), New: []byte("b\n")}}}},
		{Info: engine.StepInfo{Files: []engine.FileChange{{Path: "/new", New: []byte("a\n")}}}},
		{Info: engine.StepInfo{Files: []engine.FileChange{{Path: "/same", OldExists: true, Old: []byte("a\n"), New: []byte("a\n")}}}},
	}}
	printFileDiffs(e, pv)
	want := "\n    --- " + p + " (đang có)\n    +++ " + p + " (tpan sẽ ghi)\n      server {\n      }\n    - # sửa tay\n"
	if out.String() != want {
		t.Errorf("printFileDiffs:\n%s\nmuốn:\n%s", out.String(), want)
	}
}
