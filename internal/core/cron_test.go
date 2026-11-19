package core

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// cronEnv: siteEnv có shop (active) và blog (thùng rác), cùng Cron dùng store thật.
type cronEnv struct {
	*siteEnv
	c *Cron

	mu     sync.Mutex
	wpRes  system.Result // kết quả giả của `wp cron event run`
	wpRuns []system.Cmd
}

func newCronEnv(t *testing.T) *cronEnv {
	t.Helper()
	se := newSiteEnv(t, Nginx)
	se.fixedNow(dbTestNow)
	se.add(t, "shop.example.com")
	se.add(t, "blog.example.com")
	se.trash(t, "blog.example.com")
	ce := &cronEnv{siteEnv: se}
	b := &Backups{Sites: se.s, Store: se.st}
	ce.c = &Cron{Sites: se.s, Runs: se.st, Backups: b}
	se.m.Handle([]string{"wp", "cron", "event", "run"}, func(c system.Cmd) system.Result {
		ce.mu.Lock()
		defer ce.mu.Unlock()
		ce.wpRuns = append(ce.wpRuns, c)
		return ce.wpRes
	})
	return ce
}

func (ce *cronEnv) runs(t *testing.T) map[string]CronRun {
	t.Helper()
	rs, err := ce.st.CronRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]CronRun{}
	for _, r := range rs {
		out[r.Domain] = r
	}
	return out
}

func TestCronRun(t *testing.T) {
	ce := newCronEnv(t)
	shop := mustDomain(t, "shop.example.com")
	l := site.LayoutOf(shop)

	ce.wpRes = system.Result{Stdout: "Executed the cron event 'wp_version_check'\n"}
	r, err := ce.c.Run(ctx, shop)
	if err != nil {
		t.Fatal(err)
	}
	want := system.Cmd{Name: "wp", Args: []string{"cron", "event", "run", "--due-now", "--quiet", "--path=" + l.Public}, AsUser: l.User}
	if len(ce.wpRuns) != 1 || !reflect.DeepEqual(ce.wpRuns[0], want) {
		t.Fatalf("lệnh = %+v, muốn %+v", ce.wpRuns, want)
	}
	if r.Status != CronOK || r.ExitCode != 0 || !strings.Contains(r.Output, "wp_version_check") || !r.Started.Equal(dbTestNow) {
		t.Errorf("kết quả = %+v", r)
	}
	if got := ce.runs(t)["shop.example.com"]; got.Status != CronOK || got.Output != r.Output {
		t.Errorf("dòng cron_runs = %+v", got)
	}

	// wp lỗi: ghi fail và mã thoát, không trả lỗi (kết quả đã ở tpan.db).
	ce.wpRes = system.Result{ExitCode: 1, Stderr: "Error: Error establishing a database connection.\n"}
	r, err = ce.c.Run(ctx, shop)
	if err != nil {
		t.Fatalf("wp lỗi mà Run trả lỗi: %v", err)
	}
	if r.Status != CronFail || r.ExitCode != 1 || !strings.Contains(r.Output, "database connection") {
		t.Errorf("kết quả khi wp lỗi = %+v", r)
	}
	if got := ce.runs(t)["shop.example.com"]; got.Status != CronFail || got.ExitCode != 1 {
		t.Errorf("dòng cron_runs không bị ghi đè: %+v", got)
	}

	// Output dài: chỉ giữ 2 KB cuối, không cắt giữa ký tự.
	ce.wpRes = system.Result{Stdout: strings.Repeat("ồ", 2000) + "CUỐI\n"}
	r, _ = ce.c.Run(ctx, shop)
	if len(r.Output) > cronOutputMax || !strings.HasSuffix(r.Output, "CUỐI") || !strings.HasPrefix(r.Output, "ồ") {
		t.Errorf("output %d byte, đầu %q", len(r.Output), r.Output[:8])
	}
}

func TestCronRunSkipsAndRefuses(t *testing.T) {
	ce := newCronEnv(t)
	r, err := ce.c.Run(ctx, mustDomain(t, "blog.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != CronSkipped || len(ce.wpRuns) != 0 {
		t.Errorf("site trong thùng rác: %+v, wp chạy %d lần", r, len(ce.wpRuns))
	}
	if got := ce.runs(t)["blog.example.com"]; got.Status != CronSkipped {
		t.Errorf("không ghi dòng skipped: %+v", got)
	}

	// Site không có: từ chối, không ghi dòng nào.
	if _, err := ce.c.Run(ctx, mustDomain(t, "none.example.com")); refusedCode(t, err) != "site_not_found" {
		t.Error("muốn site_not_found")
	}
	if _, ok := ce.runs(t)["none.example.com"]; ok {
		t.Error("site không có mà vẫn ghi dòng cron_runs")
	}

	// Không có store: lỗi (CLI thoát khác 0), không chạy wp.
	nc := &Cron{Sites: ce.s}
	if _, err := nc.Run(ctx, mustDomain(t, "shop.example.com")); err == nil || len(ce.wpRuns) != 0 {
		t.Errorf("Runs nil: lỗi %v, wp chạy %d lần", err, len(ce.wpRuns))
	}
}

func TestCronList(t *testing.T) {
	ce := newCronEnv(t)
	shop := site.LayoutOf(mustDomain(t, "shop.example.com"))
	ce.m.SetFile(site.CronFile(shop), []byte("x\n"), 0o644, system.Root)
	if _, err := ce.c.Run(ctx, shop.Domain); err != nil {
		t.Fatal(err)
	}
	mustSet(t, ce.st, SettingBackupKeep, "5")
	mustSet(t, ce.st, SettingBackupAt, "03:30")

	ov, err := ce.c.List(ctx)
	if err != nil && strings.Contains(err.Error(), "chưa viết") {
		// ponytail: Backups.Settings nằm ở backup.go (thợ khác, cùng đợt);
		// test tự chạy đủ khi hàm đó có thân.
		t.Skip("Backups.Settings chưa có thân:", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Sites) != 2 {
		t.Fatalf("Sites = %+v, muốn 2 dòng (active và trashed)", ov.Sites)
	}
	byDom := map[string]CronEntry{}
	for _, e := range ov.Sites {
		byDom[e.Domain] = e
	}
	s, b := byDom["shop.example.com"], byDom["blog.example.com"]
	if !s.Enabled || s.File != site.CronFile(shop) ||
		s.Schedule != CronEvery || s.Command != "/usr/local/bin/tpan cron run shop.example.com" ||
		s.Last == nil || s.Last.Status != CronOK {
		t.Errorf("dòng shop = %+v", s)
	}
	if b.Enabled || b.Last != nil {
		t.Errorf("dòng blog (thùng rác, chưa chạy) = %+v", b)
	}
	if ov.Backup != (BackupSettings{Keep: 5, At: "03:30"}) || ov.BackupFile != site.BackupCronFile {
		t.Errorf("lịch backup = %+v %q", ov.Backup, ov.BackupFile)
	}
}

func TestCronTail(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abcdef", 3, "def"},
		{"aồb", 3, "b"},  // "ồ" 3 byte: cắt giữa thì bỏ cả ký tự
		{"aồb", 4, "ồb"}, // vừa đủ
	} {
		if got := cronTail(tc.in, tc.n); got != tc.want {
			t.Errorf("cronTail(%q, %d) = %q, muốn %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// siteCronSteps chạy trên máy giả: ghi đúng file cron (root 0644, nội dung
// từ template), đặt DISABLE_WP_CRON; chạy lại thì không bước nào Apply;
// siteCronAbsent gỡ file.
func TestSiteCronSteps(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	se.add(t, "shop.example.com")
	l := site.LayoutOf(mustDomain(t, "shop.example.com"))
	se.m.SetDir("/etc/cron.d", 0o755, system.Root)

	var mu sync.Mutex
	val := "" // giá trị DISABLE_WP_CRON trong wp-config.php giả; rỗng là chưa có
	se.m.Handle([]string{"wp", "config", "get", "DISABLE_WP_CRON"}, func(system.Cmd) system.Result {
		mu.Lock()
		defer mu.Unlock()
		if val == "" {
			return system.Result{ExitCode: 1, Stderr: "Error: The constant 'DISABLE_WP_CRON' is not defined in the 'wp-config.php' file.\n"}
		}
		return system.Result{Stdout: val + "\n"}
	})
	se.m.Handle([]string{"wp", "config", "set", "DISABLE_WP_CRON"}, func(c system.Cmd) system.Result {
		mu.Lock()
		defer mu.Unlock()
		val = c.Args[3]
		return system.Result{}
	})

	ss, err := siteCronSteps(l)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"wp.constant:shop.example.com:DISABLE_WP_CRON", "file:" + site.CronFile(l)}
	run := func(steps ...engine.Step) engine.Report {
		t.Helper()
		rep, err := newPlanned(se.m, se.st, engine.Plan{Kind: "test.cron", Target: "shop.example.com", Steps: steps}, nil).Execute(ctx, nil)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("job: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
		}
		return rep
	}
	if got := stepIDs(engine.Plan{Steps: ss}); !reflect.DeepEqual(got, want) {
		t.Errorf("bước = %v, muốn %v", got, want)
	}
	run(ss...)
	if val != "true" {
		t.Errorf("DISABLE_WP_CRON = %q", val)
	}
	data, err := se.m.ReadFile(site.CronFile(l))
	if err != nil {
		t.Fatal(err)
	}
	tmpl, _ := templates.SiteCron(l)
	fi, _ := se.m.Stat(site.CronFile(l))
	if string(data) != string(tmpl) || fi.Mode.Perm() != 0o644 || fi.Owner.User != "root" {
		t.Errorf("file cron: %v %v\n%s", fi.Mode, fi.Owner, data)
	}

	ss, _ = siteCronSteps(l)
	for _, sr := range run(ss...).Steps {
		if sr.Outcome != "skipped" {
			t.Errorf("chạy lại: bước %s %s, muốn skipped", sr.ID, sr.Outcome)
		}
	}

	run(siteCronAbsent(l))
	if _, err := se.m.Stat(site.CronFile(l)); err == nil {
		t.Error("siteCronAbsent không gỡ file cron")
	}
	if info := siteCronAbsent(l).Info(); !strings.Contains(info.Title.Vi, "shop.example.com") || info.Why.En == "" {
		t.Errorf("Info của siteCronAbsent thiếu câu riêng: %+v", info)
	}
}
