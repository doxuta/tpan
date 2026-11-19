package demo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// newDemo dựng máy demo trong thư mục tạm. ctx huỷ khi test xong để goroutine
// /proc dừng trước khi thư mục bị xoá.
func newDemo(t *testing.T) (context.Context, *system.FakeSystem, *store.Store) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sys, st, err := New(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return ctx, sys, st
}

func domain(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatalf("ParseDomain(%q): %v", s, err)
	}
	return d
}

// notPass liệt kê các phép kiểm không đạt, để thông báo lỗi nói rõ cái nào.
func notPass(rs []checks.Result) []string {
	var out []string
	for _, r := range rs {
		if r.Status != checks.Pass {
			out = append(out, string(r.Status)+" "+r.ID+": "+r.Summary.Vi)
		}
	}
	return out
}

func statusOf(rs []checks.Result, idPrefix string) checks.Status {
	for _, r := range rs {
		if strings.HasPrefix(r.ID, idPrefix) {
			return r.Status
		}
	}
	return ""
}

func TestNewHasThreeSites(t *testing.T) {
	ctx, sys, st := newDemo(t)
	sites, err := NewSites(sys, st).List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sites {
		got = append(got, s.Domain.String())
	}
	if want := []string{siteBlog, siteShop, siteWiki}; !slices.Equal(got, want) {
		t.Fatalf("List = %v; muốn %v", got, want)
	}
	for _, s := range sites {
		if s.Created.IsZero() || time.Since(s.Created) < 24*time.Hour {
			t.Errorf("site %s có ngày tạo %v; muốn cách đây ít nhất một ngày", s.Domain, s.Created)
		}
	}
}

func TestSiteHealth(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)

	for _, dom := range []string{siteBlog, siteShop} {
		h, err := s.Health(ctx, domain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		if !h.Installed || len(h.Results) == 0 {
			t.Fatalf("%s: Health = %+v", dom, h)
		}
		if bad := notPass(h.Results); len(bad) > 0 {
			t.Errorf("%s phải đạt mọi phép kiểm, có: %v", dom, bad)
		}
	}

	// Site thứ ba hỏng đúng ở chặng PHP (socket mất) và chặng cuối (502); vhost,
	// mã nguồn, DB và cô lập vẫn đạt, như một sự cố thật của pool.
	h, err := s.Health(ctx, domain(t, siteWiki))
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	for _, r := range h.Results {
		if r.Status == checks.Fail {
			failed = append(failed, r.ID)
			if r.Stage == checks.StagePHP && len(r.ManualFix) == 0 {
				t.Errorf("%s Fail mà thiếu ManualFix", r.ID)
			}
		}
	}
	if want := []string{"php.socket:" + siteWiki, "http.site:" + siteWiki}; !slices.Equal(failed, want) {
		t.Errorf("phép kiểm Fail = %v; muốn %v (mọi phép kiểm: %v)", failed, want, notPass(h.Results))
	}
	if got := statusOf(h.Results, "web.vhost:"); got != checks.Pass {
		t.Errorf("web.vhost = %q; muốn pass", got)
	}
}

func TestStackHealth(t *testing.T) {
	ctx, sys, st := newDemo(t)
	h, err := NewStack(sys, st).Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Installed || h.Config.Web != core.Nginx || h.Config.PHPVersion != "8.3" {
		t.Fatalf("Health: %+v", h)
	}
	if bad := notPass(h.Results); len(bad) > 0 {
		t.Errorf("stack phải đạt mọi phép kiểm, có: %v", bad)
	}
	status, err := NewStack(sys, st).Status(ctx)
	if err != nil || len(status.Services) != 3 {
		t.Errorf("Status: %+v, %v", status, err)
	}
}

func TestJobHistory(t *testing.T) {
	ctx, _, st := newDemo(t)
	jobs, err := st.Jobs(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Mới nhất trước: wiki, lần thất bại, shop, blog.
	want := []struct {
		target string
		status engine.JobStatus
	}{
		{siteWiki, engine.JobSucceeded},
		{siteFail, engine.JobRolledBack},
		{siteShop, engine.JobSucceeded},
		{siteBlog, engine.JobSucceeded},
	}
	if len(jobs) != len(want) {
		t.Fatalf("có %d job; muốn %d: %+v", len(jobs), len(want), jobs)
	}
	for i, w := range want {
		j := jobs[i]
		if j.Kind != "site.add" || j.Target != w.target || j.Status != w.status {
			t.Errorf("job %d = %s %s %s; muốn site.add %s %s", i, j.Kind, j.Target, j.Status, w.target, w.status)
		}
		if j.Finished.IsZero() || j.Finished.Before(j.Started) {
			t.Errorf("job %d có thời gian lạ: %v → %v", i, j.Started, j.Finished)
		}
		evs, err := st.Events(ctx, j.ID, 0)
		if err != nil || len(evs) < 3 {
			t.Fatalf("job %d: %d sự kiện, %v", i, len(evs), err)
		}
		for _, ev := range evs {
			if !ev.Time.Before(time.Now().Add(-24 * time.Hour)) {
				t.Errorf("job %d: sự kiện %d có giờ %v, chưa lùi về quá khứ", i, ev.Seq, ev.Time)
			}
		}
		if w.status == engine.JobRolledBack {
			kinds := map[engine.EventKind]bool{}
			for _, ev := range evs {
				kinds[ev.Kind] = true
			}
			if !kinds[engine.EventStepFail] || !kinds[engine.EventUndoDone] || j.Error == "" {
				t.Errorf("job hoàn tác thiếu step.fail/undo.done hay Error: %v %q", kinds, j.Error)
			}
		}
	}
	// Lần thất bại không để lại site trong bảng.
	if _, ok, _ := st.Site(ctx, domain(t, siteFail)); ok {
		t.Errorf("site %s có trong bảng sites dù job đã hoàn tác", siteFail)
	}
}

// Kịch bản của giao diện demo: người dùng tạo một site mới, rồi site hỏng vẫn hỏng.
func TestAddSiteOnDemoMachine(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)
	in, err := input("moi.example.com", "Site mới")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.PrepareAdd(ctx, in)
	if err != nil {
		t.Fatalf("PrepareAdd: %v", err)
	}
	res, err := a.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s; bước %+v", res.Report.Job.Status, res.Report.Job.Error, res.Report.Steps)
	}
	if !res.WPInstalled || len(res.AdminPassword.Reveal()) != 32 {
		t.Errorf("WPInstalled=%v, độ dài mật khẩu=%d", res.WPInstalled, len(res.AdminPassword.Reveal()))
	}
	if list, _ := s.List(ctx); len(list) != 4 {
		t.Errorf("List có %d site; muốn 4", len(list))
	}

	h, err := s.Health(ctx, in.Domain)
	if err != nil {
		t.Fatal(err)
	}
	if bad := notPass(h.Results); len(bad) > 0 {
		t.Errorf("site mới phải đạt mọi phép kiểm, có: %v", bad)
	}

	// Reload PHP-FPM của lần tạo site không được chữa site hỏng.
	h, err = s.Health(ctx, domain(t, siteWiki))
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(h.Results, "php.socket:"); got != checks.Fail {
		t.Errorf("sau khi tạo site mới, php.socket của %s = %q; muốn fail", siteWiki, got)
	}

	// Xoá site (nút xoá trên giao diện) cũng chạy được trên máy giả.
	rm, err := s.PrepareRemove(ctx, in.Domain)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := rm.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá site: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	if list, _ := s.List(ctx); len(list) != 3 {
		t.Errorf("sau khi xoá List có %d site; muốn 3", len(list))
	}
}

// Domain của lần thất bại gõ lại được: chỗ chặn vhost đã được dọn.
func TestRetryFailedDomain(t *testing.T) {
	ctx, sys, st := newDemo(t)
	in, err := input(siteFail, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewSites(sys, st).PrepareAdd(ctx, in)
	if err != nil {
		t.Fatalf("PrepareAdd: %v", err)
	}
	res, err := a.Execute(ctx, nil)
	if err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("tạo lại %s: %v %s %s", siteFail, err, res.Report.Job.Status, res.Report.Job.Error)
	}
}

// Mật khẩu của site sinh lúc chạy chỉ được đi qua stdin của lệnh cần nó: không
// lọt vào sự kiện đã lưu trong DB, cũng không vào file nào của máy giả.
func TestSecretsStayInStdin(t *testing.T) {
	ctx, sys, st := newDemo(t)
	pwRE := regexp.MustCompile(`[A-Za-z0-9]{32}`)
	secrets := map[string]bool{}
	for _, c := range sys.Ran() {
		for _, pw := range pwRE.FindAllString(string(c.Stdin), -1) {
			secrets[pw] = true
		}
	}
	if len(secrets) < 6 { // 3 site, mỗi site một mật khẩu DB và một mật khẩu quản trị
		t.Fatalf("tìm thấy %d mật khẩu trong stdin; muốn ít nhất 6: test không kiểm được gì", len(secrets))
	}
	jobs, _ := st.Jobs(ctx, 0)
	for _, j := range jobs {
		evs, _ := st.Events(ctx, j.ID, 0)
		for _, ev := range evs {
			b, _ := json.Marshal(ev)
			for pw := range secrets {
				if strings.Contains(string(b), pw) {
					t.Errorf("mật khẩu lọt vào sự kiện %d của job %s", ev.Seq, j.Target)
				}
			}
		}
	}
	for p, e := range sys.Snapshot() {
		for pw := range secrets {
			if strings.Contains(e.Data, pw) {
				t.Errorf("mật khẩu lọt vào file %s của máy giả", p)
			}
		}
	}
}

func readProc(t *testing.T, sys system.System, name string) string {
	t.Helper()
	b, err := sys.ReadFile("/proc/" + name)
	if err != nil {
		t.Fatalf("đọc /proc/%s: %v", name, err)
	}
	return string(b)
}

// cpuTotals trả (tổng, nhàn rỗi) của dòng "cpu " trong /proc/stat.
func cpuTotals(t *testing.T, stat string) (total, idle uint64) {
	t.Helper()
	for _, ln := range strings.Split(stat, "\n") {
		f := strings.Fields(ln)
		if len(f) > 5 && f[0] == "cpu" {
			for i, v := range f[1:] {
				n, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					t.Fatalf("/proc/stat: %q: %v", ln, err)
				}
				total += n
				if i == 3 {
					idle = n
				}
			}
			return total, idle
		}
	}
	t.Fatalf("/proc/stat không có dòng cpu: %q", stat)
	return 0, 0
}

func TestProcNumbers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sys, st, err := New(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	stat := readProc(t, sys, "stat")
	cpus := 0
	for _, ln := range strings.Split(stat, "\n") {
		if f := strings.Fields(ln); len(f) > 0 && strings.HasPrefix(f[0], "cpu") && f[0] != "cpu" {
			cpus++
		}
	}
	if cpus != 4 {
		t.Errorf("/proc/stat có %d lõi; muốn 4", cpus)
	}
	if !strings.HasPrefix(stat, "cpu  ") {
		t.Errorf("/proc/stat phải mở đầu bằng \"cpu  \" (hai dấu cách) như kernel: %q", stat[:20])
	}

	mem := readProc(t, sys, "meminfo")
	for _, k := range []string{"MemTotal:", "MemAvailable:", "SwapTotal:", "SwapFree:"} {
		if !strings.Contains(mem, k) {
			t.Errorf("/proc/meminfo thiếu %s", k)
		}
	}
	if !strings.Contains(mem, "MemTotal:       4013828 kB") {
		t.Errorf("MemTotal không phải máy 4 GB: %q", mem)
	}
	if f := strings.Fields(readProc(t, sys, "loadavg")); len(f) != 5 {
		t.Errorf("/proc/loadavg có %d trường; muốn 5", len(f))
	}
	if f := strings.Fields(readProc(t, sys, "uptime")); len(f) != 2 {
		t.Errorf("/proc/uptime có %d trường; muốn 2", len(f))
	}
	du, err := sys.StatFS("/var/www")
	if err != nil || du.Total != 80<<30 || du.Free != 46<<30 {
		t.Errorf("StatFS(/var/www) = %+v, %v", du, err)
	}

	// Hai lần đọc cách nhau ngắn phải thấy CPU bận một phần nhỏ, không phải 0%.
	t0, i0 := cpuTotals(t, stat)
	var t1, i1 uint64
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(procTick * 3)
		if t1, i1 = cpuTotals(t, readProc(t, sys, "stat")); t1 > t0 {
			break
		}
	}
	if t1 <= t0 {
		t.Fatal("/proc/stat không đổi theo thời gian")
	}
	busy := 100 * float64((t1-t0)-(i1-i0)) / float64(t1-t0)
	if busy <= 0 || busy >= 60 {
		t.Errorf("CPU bận %.1f%%; muốn một số nhỏ khác 0", busy)
	}

	// Huỷ ctx thì goroutine dừng, số liệu đứng yên.
	cancel()
	time.Sleep(procTick * 3)
	a := readProc(t, sys, "stat")
	time.Sleep(procTick * 4)
	if b := readProc(t, sys, "stat"); a != b {
		t.Error("ctx đã huỷ mà /proc/stat vẫn đổi")
	}
}

func TestNewRefusesExistingDB(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "tpan.db")
	if err := os.WriteFile(db, []byte("của người khác"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := New(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "tpan.db") {
		t.Fatalf("New trên thư mục đã có tpan.db: %v; muốn lỗi nhắc tpan.db", err)
	}
	if b, _ := os.ReadFile(db); string(b) != "của người khác" {
		t.Error("New đã đụng vào tpan.db có sẵn")
	}
}

func TestNewCreatesMissingDir(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := filepath.Join(t.TempDir(), "chua", "co")
	_, st, err := New(ctx, dir)
	if err != nil {
		t.Fatalf("New với thư mục chưa có: %v", err)
	}
	_ = st.Close()
	if _, err := os.Stat(filepath.Join(dir, "tpan.db")); err != nil {
		t.Errorf("không có tpan.db: %v", err)
	}
}

func TestNewWithEmptyDirUsesTemp(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir()) // MkdirTemp dùng TMPDIR: thư mục tạm của demo nằm trong thư mục test và bị dọn theo
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, st, err := New(ctx, "")
	if err != nil {
		t.Fatalf("New với dir rỗng: %v", err)
	}
	_ = st.Close()
}

// Câu hỏi dung lượng DB của màn Cơ sở dữ liệu được trả lời cho mọi DB có thật.
func TestDatabaseSizes(t *testing.T) {
	ctx, sys, _ := newDemo(t)
	var names []string
	for _, dom := range []string{siteBlog, siteShop, siteWiki} {
		l, err := layoutOf(dom)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, "'"+l.DB+"'")
	}
	names = append(names, "'tp_khong_co'")
	sql := "SELECT s.SCHEMA_NAME, COUNT(t.TABLE_NAME), COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH), 0) " +
		"FROM information_schema.SCHEMATA s LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = s.SCHEMA_NAME " +
		"WHERE s.SCHEMA_NAME IN (" + strings.Join(names, ", ") + ") GROUP BY s.SCHEMA_NAME;\n"
	res, err := sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(sql)})
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if len(rows) != 3 {
		t.Fatalf("có %d dòng; muốn 3 (DB không có thì không có dòng): %q", len(rows), res.Stdout)
	}
	for _, r := range rows {
		f := strings.Split(r, "\t")
		size, err := strconv.ParseInt(f[2], 10, 64)
		if len(f) != 3 || f[1] != "12" || err != nil || size < 1<<20 || size > 8<<20 {
			t.Errorf("dòng lạ: %q", r)
		}
	}
}
