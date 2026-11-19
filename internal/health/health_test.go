package health

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

var t0 = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

const (
	dA = "a.example.com"
	dB = "b.example.com"
	dC = "c.example.com" // trong thùng rác
)

type env struct {
	r     *Runner
	st    *store.Store
	sys   *system.FakeSystem
	now   time.Time
	nginx atomic.Bool // systemctl is-active nginx trả "active"
}

func dom(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// newEnv dựng máy giả có stack nginx 8.3 đã cài (theo settings), hai site
// active và một site trong thùng rác, cùng vài file tpan quản lý.
func newEnv(t *testing.T, installed bool) *env {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tpan.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	e := &env{st: st, sys: system.NewFake(), now: t0}
	// Mặc định FakeSystem trả mã 0 với stdout rỗng: is-active ra "" là Fail.
	e.sys.Handle([]string{"systemctl", "is-active", "nginx"}, func(system.Cmd) system.Result {
		if e.nginx.Load() {
			return system.Result{Stdout: "active\n"}
		}
		return system.Result{ExitCode: 3, Stdout: "inactive\n"}
	})
	e.r = &Runner{Sys: e.sys, Store: st, DBPath: dbPath, Now: func() time.Time { return e.now }}

	if installed {
		for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
			if err := st.SetSetting(ctx, k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range []struct {
		d      string
		status site.Status
	}{{dA, site.Active}, {dB, site.Active}, {dC, site.Trashed}} {
		if err := st.SaveSite(ctx, site.Site{Domain: dom(t, s.d), Status: s.status, PHPVersion: "8.3", Created: t0}); err != nil {
			t.Fatal(err)
		}
	}
	la, lb, lc := site.LayoutOf(dom(t, dA)), site.LayoutOf(dom(t, dB)), site.LayoutOf(dom(t, dC))
	for _, p := range []string{la.NginxConf, la.NginxLink, lb.PoolConf("8.2"), lc.Public + "/wp-config.php",
		"/etc/logrotate.d/tpan-sites", "/etc/nginx/sites-available/tpan-default.conf"} {
		if err := st.RecordFile(ctx, p, strings.Repeat("0", 64), "job-1"); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *env) run(t *testing.T, o Options) Report {
	t.Helper()
	rep, err := e.r.Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(%+v): %v", o, err)
	}
	return rep
}

// targets trả ID -> target của mọi kết quả trong báo cáo; ID trùng là lỗi.
func targets(t *testing.T, rep Report) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, g := range rep.Groups {
		for _, r := range g.Results {
			if old, dup := out[r.ID]; dup {
				t.Errorf("ID %s xuất hiện hai lần (%q và %q)", r.ID, old, g.Target)
			}
			out[r.ID] = g.Target
		}
	}
	return out
}

func TestRunDayDuGomVaGanTarget(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	rep := e.run(t, Options{Full: true})
	if !rep.Installed || !rep.Started.Equal(t0) || !rep.Finished.Equal(t0) {
		t.Errorf("Report = installed %v, %v..%v", rep.Installed, rep.Started, rep.Finished)
	}
	got := targets(t, rep)
	la, lb, lc := site.LayoutOf(dom(t, dA)), site.LayoutOf(dom(t, dB)), site.LayoutOf(dom(t, dC))
	want := map[string]string{
		"service.active:nginx":                               "",
		"memory:ram":                                         "",
		"web.vhost:" + dA:                                    dA,
		"wp.checksums:" + dA:                                 dA,
		"http.site:" + dB:                                    dB,
		"isolation:" + dA + ":" + dB:                         dA, // lần gặp đầu (site a) thắng
		"isolation:" + dB + ":" + dA:                         dA,
		"drift:" + la.NginxConf:                              dA,
		"drift:" + la.NginxLink:                              dA,
		"drift:" + lb.PoolConf("8.2"):                        dB,
		"drift:" + lc.Public + "/wp-config.php":              dC, // site trong thùng rác vẫn sở hữu file
		"drift:/etc/logrotate.d/tpan-sites":                  "",
		"drift:/etc/nginx/sites-available/tpan-default.conf": "",
		"tpan.db:integrity":                                  store.TargetTPAN,
		"tpan.perm:" + e.r.DBPath:                            store.TargetTPAN,
		"tpan.perm:" + PanelKeyPath:                          store.TargetTPAN,
		"tpan.perm:" + PanelConfDir:                          store.TargetTPAN,
		"tpan.cert:" + PanelCertPath:                         store.TargetTPAN,
		"disk.free:" + StateDir:                              store.TargetTPAN,
		"tpan.jobs:stuck":                                    store.TargetTPAN,
		"tpan.scheduler:health":                              store.TargetTPAN,
	}
	for id, tg := range want {
		if g, ok := got[id]; !ok || g != tg {
			t.Errorf("%s: target %q (có %v), muốn %q", id, g, ok, tg)
		}
	}
	for id := range got {
		if strings.HasSuffix(id, ":"+dC) {
			t.Errorf("site trong thùng rác không được kiểm: %s", id)
		}
	}

	// Đã ghi vào DB đúng target, và nhịp lần chạy đầy đủ.
	r, ok, err := e.st.CheckResult(ctx, "isolation:"+dB+":"+dA)
	if err != nil || !ok || r.Target != dA {
		t.Errorf("dòng isolation:b:a = %+v, %v, %v", r, ok, err)
	}
	if v, ok, _ := e.st.GetSetting(ctx, SettingLastRun); !ok || v != t0.Format(time.RFC3339) {
		t.Errorf("%s = %q, %v", SettingLastRun, v, ok)
	}

	// nginx không chạy: sự cố mở, và nằm trong Changes.
	found := false
	for _, inc := range rep.Changes.Opened {
		if inc.CheckID == "service.active:nginx" && inc.Target == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("chưa mở sự cố cho nginx: %+v", rep.Changes.Opened)
	}
	if !rep.HasFail() {
		t.Error("HasFail = false")
	}
}

func TestRunNheBoChecksumVaKhongPrune(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.run(t, Options{Full: true})
	full, _ := e.st.CheckResults(ctx)

	e.now = t0.Add(5 * time.Minute)
	rep := e.run(t, Options{})
	got := targets(t, rep)
	for id := range got {
		if strings.HasPrefix(id, "wp.checksums:") {
			t.Errorf("lần nhẹ có %s", id)
		}
	}
	if _, ok := got["service.active:nginx"]; !ok {
		t.Error("lần nhẹ thiếu service.active:nginx")
	}
	// Lần nhẹ không xoá kết quả wp.checksums của lần đầy đủ, không ghi last_run.
	after, _ := e.st.CheckResults(ctx)
	if len(after) != len(full) {
		t.Errorf("lần nhẹ đổi số dòng: %d -> %d", len(full), len(after))
	}
	r, ok, _ := e.st.CheckResult(ctx, "wp.checksums:"+dA)
	if !ok || !r.Checked.Equal(t0) {
		t.Errorf("wp.checksums sau lần nhẹ = %+v, %v", r, ok)
	}
	if v, _, _ := e.st.GetSetting(ctx, SettingLastRun); v != t0.Format(time.RFC3339) {
		t.Errorf("lần nhẹ ghi %s = %q", SettingLastRun, v)
	}
}

func TestRunMoDongSuCo(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.run(t, Options{Full: true})

	e.nginx.Store(true)
	e.now = t0.Add(5 * time.Minute)
	rep := e.run(t, Options{})
	closed := false
	for _, inc := range rep.Changes.Closed {
		if inc.CheckID == "service.active:nginx" && inc.CloseReason == "pass" {
			closed = true
		}
	}
	if !closed {
		t.Errorf("nginx chạy lại mà sự cố chưa đóng: %+v", rep.Changes.Closed)
	}
	open, _ := e.st.Incidents(ctx, true, 0)
	for _, inc := range open {
		if inc.CheckID == "service.active:nginx" {
			t.Errorf("sự cố nginx còn mở: %+v", inc)
		}
	}
}

func TestRunPruneKhiSiteMat(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.run(t, Options{Full: true})
	open, _ := e.st.Incidents(ctx, true, 0)
	var ofB []int64
	for _, inc := range open {
		// Lệch của file vẫn còn trong managed_files thì vẫn được kiểm: không tính.
		if inc.Target == dB && !strings.HasPrefix(inc.CheckID, "drift:") {
			ofB = append(ofB, inc.ID)
		}
	}
	if len(ofB) == 0 {
		t.Fatal("cần ít nhất một sự cố của site b để kiểm đóng vì gone")
	}

	if err := e.st.DeleteSite(ctx, dom(t, dB)); err != nil {
		t.Fatal(err)
	}
	e.now = t0.Add(time.Hour)
	rep := e.run(t, Options{Full: true})

	rs, _ := e.st.CheckResults(ctx)
	for _, r := range rs {
		if strings.Contains(r.ID, dB) && !strings.HasPrefix(r.ID, "drift:") {
			t.Errorf("còn kết quả của site đã xoá: %s (target %q)", r.ID, r.Target)
		}
	}
	gone := 0
	for _, inc := range rep.Changes.Closed {
		if inc.Target == dB && inc.CloseReason == "gone" {
			gone++
		}
	}
	if gone < len(ofB) {
		t.Errorf("đóng vì gone %d sự cố của b, muốn ít nhất %d", gone, len(ofB))
	}
}

func TestRunMotSite(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.run(t, Options{Full: true})

	e.now = t0.Add(time.Minute)
	rep := e.run(t, Options{Domain: dB})
	if len(rep.Groups) != 1 || rep.Groups[0].Target != dB {
		t.Fatalf("Groups = %+v", rep.Groups)
	}
	for id := range targets(t, rep) {
		if strings.HasPrefix(id, "drift:") || strings.HasPrefix(id, "tpan.") || id == "memory:ram" ||
			strings.HasPrefix(id, "service.") || strings.HasPrefix(id, "wp.checksums:") {
			t.Errorf("lần một site có %s", id)
		}
	}
	// Dòng cô lập vẫn thuộc site a (target của lần đầy đủ), dù vừa được kiểm lại.
	for _, id := range []string{"isolation:" + dA + ":" + dB, "isolation:" + dB + ":" + dA} {
		r, ok, err := e.st.CheckResult(ctx, id)
		if err != nil || !ok || r.Target != dA || !r.Checked.Equal(e.now) {
			t.Errorf("%s = target %q checked %v (%v, %v)", id, r.Target, r.Checked, ok, err)
		}
	}
	if v, _, _ := e.st.GetSetting(ctx, SettingLastRun); v != t0.Format(time.RFC3339) {
		t.Errorf("lần một site ghi %s = %q", SettingLastRun, v)
	}
}

func TestRunDomainKhongHopLe(t *testing.T) {
	e := newEnv(t, true)
	for _, d := range []string{"a.com; rm -rf /", "$(id)", "../../etc/passwd", "a.com\nb.com", strings.Repeat("a", 300) + ".com"} {
		if _, err := e.r.Run(context.Background(), Options{Domain: d}); err == nil {
			t.Errorf("Run(%q) không lỗi", d)
		}
	}
	if _, err := e.r.Run(context.Background(), Options{Domain: "khongco.example.com"}); err == nil {
		t.Error("site không có mà không lỗi")
	}
}

func TestRunChuaCaiStack(t *testing.T) {
	e := newEnv(t, false)
	e.r.NoSelf = true
	rep := e.run(t, Options{Full: true})
	if rep.Installed {
		t.Error("Installed = true")
	}
	got := targets(t, rep)
	if got["memory:ram"] != "" {
		t.Error("thiếu memory:ram khi chưa cài stack")
	}
	for id, tg := range got {
		if tg == store.TargetTPAN {
			t.Errorf("NoSelf mà có %s", id)
		}
		if strings.HasPrefix(id, "service.") || strings.HasPrefix(id, "web.vhost:") {
			t.Errorf("chưa cài stack mà có %s", id)
		}
	}
}

func TestSchedulerChayDayDuRoiKick(t *testing.T) {
	e := newEnv(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reps := make(chan Report, 16)
	s := &Scheduler{Runner: e.r, LightEvery: time.Hour, FullEvery: time.Hour,
		OnReport: func(r Report) { reps <- r }}
	// Kick trước khi Run: không chặn, không panic, gộp hai domain thành "mọi thứ".
	s.Kick(dA)
	s.Kick(dB)

	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	first := wait(t, reps)
	if _, ok := targets(t, first)["wp.checksums:"+dA]; !ok {
		t.Error("lần đầu phải là đầy đủ (có wp.checksums)")
	}
	kicked := wait(t, reps)
	if len(kicked.Groups) < 2 {
		t.Errorf("hai Kick khác domain phải gộp thành mọi thứ: %d nhóm", len(kicked.Groups))
	}
	if _, ok := targets(t, kicked)["wp.checksums:"+dA]; ok {
		t.Error("Kick phải là lần nhẹ")
	}

	s.Kick(dB)
	one := wait(t, reps)
	if len(one.Groups) != 1 || one.Groups[0].Target != dB {
		t.Errorf("Kick(b) = %+v", one.Groups)
	}
	if v, ok, _ := e.st.GetSetting(ctx, SettingSchedulerBeat); !ok || v == "" {
		t.Error("chưa ghi nhịp")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run không dừng khi ctx huỷ")
	}
}

func TestSchedulerNhipNheVaBan(t *testing.T) {
	e := newEnv(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var busy atomic.Bool
	var rounds atomic.Int32
	reps := make(chan Report, 64)
	s := &Scheduler{Runner: e.r, LightEvery: 10 * time.Millisecond, FullEvery: time.Hour,
		Busy: func() bool { rounds.Add(1); return busy.Load() },
		OnReport: func(r Report) {
			select {
			case reps <- r:
			default:
			}
		}}
	busy.Store(true)
	go s.Run(ctx)

	// Bận: không có báo cáo nào, nhưng nhịp vẫn được ghi (bộ hẹn giờ còn sống).
	deadline := time.Now().Add(5 * time.Second)
	for rounds.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if rounds.Load() < 3 {
		t.Fatal("bộ hẹn giờ không chạy vòng nhẹ")
	}
	select {
	case r := <-reps:
		t.Fatalf("đang bận mà vẫn chạy: %+v", r.Started)
	default:
	}
	if _, ok, _ := e.st.GetSetting(ctx, SettingSchedulerBeat); !ok {
		t.Error("bận mà không ghi nhịp")
	}

	busy.Store(false)
	wait(t, reps)
}

func wait(t *testing.T, ch <-chan Report) Report {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("chờ báo cáo quá lâu")
		return Report{}
	}
}
