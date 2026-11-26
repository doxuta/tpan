package steps

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

const (
	fpmTestPoolDir = "/etc/php/8.3/fpm/pool.d"
	fpmTestLog     = "/var/log/php8.3-fpm.log"
)

// fpmSim giả lập master PHP-FPM 8.3 đúng như đo trên máy ảo (fpm_reload.go):
// `systemctl reload` trả về ngay sau khi ghi "Reloading in progress"; master
// mới liệt kê pool.d sau listAfter, mở từng file sau thêm openAfter (journal
// trên máy ảo: lỗi mở file ghi 8 ms sau lệnh reload). File đã
// liệt kê mà lúc mở không còn thì master chết (không ghi "ready", is-active
// trả 3). `systemctl start` đọc lại pool.d ngay và chạy (đồng bộ).
type fpmSim struct {
	t                    *testing.T
	f                    *system.FakeSystem
	listAfter, openAfter time.Duration

	mu      sync.Mutex
	active  bool
	deaths  int
	reloads int
	starts  int
	busy    sync.WaitGroup
}

func newFPMSim(t *testing.T, conf string) *fpmSim {
	t.Helper()
	f := system.NewFake()
	if conf != "" {
		f.SetFile(fpmConfPath("8.3"), []byte(conf), 0o644, system.Root)
	}
	f.SetDir(fpmTestPoolDir, 0o755, system.Root)
	f.SetFile(fpmTestLog, []byte("[26-Nov-2026 16:00:00] NOTICE: ready to handle connections\n"), 0o600, system.Root)
	s := &fpmSim{t: t, f: f, listAfter: 3 * time.Millisecond, openAfter: 10 * time.Millisecond, active: true}
	// `php-fpm8.3 -t` mất cỡ 20 ms trên máy ảo: đủ để bước kế tiếp rơi đúng vào
	// lúc master đang đọc pool.d.
	f.Handle([]string{"php-fpm8.3", "-t"}, func(system.Cmd) system.Result {
		time.Sleep(20 * time.Millisecond)
		return system.Result{}
	})
	f.Handle([]string{"systemctl", "is-active", "php8.3-fpm"}, func(system.Cmd) system.Result {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.active {
			return system.Result{Stdout: "active\n"}
		}
		return system.Result{ExitCode: 3, Stdout: "failed\n"}
	})
	f.Handle([]string{"systemctl", "reload", "php8.3-fpm"}, func(system.Cmd) system.Result {
		s.mu.Lock()
		if !s.active {
			s.mu.Unlock()
			return system.Result{ExitCode: 1, Stderr: "php8.3-fpm.service is not active, cannot reload."}
		}
		s.reloads++
		s.mu.Unlock()
		s.log("NOTICE: Reloading in progress ...")
		s.busy.Add(1)
		go s.reexec()
		return system.Result{}
	})
	f.Handle([]string{"systemctl", "start", "php8.3-fpm"}, func(system.Cmd) system.Result {
		s.mu.Lock()
		s.starts++
		s.active = true
		s.mu.Unlock()
		s.log("NOTICE: ready to handle connections")
		return system.Result{}
	})
	t.Cleanup(s.busy.Wait)
	return s
}

func (s *fpmSim) log(line string) {
	b, _ := s.f.ReadFile(fpmTestLog)
	b = append(b, "[26-Nov-2026 16:00:01] "+line+"\n"...)
	s.f.SetFile(fpmTestLog, b, 0o600, system.Root)
}

func (s *fpmSim) reexec() {
	defer s.busy.Done()
	time.Sleep(s.listAfter)
	names, _ := s.f.ReadDir(fpmTestPoolDir)
	var listed []string
	for _, n := range names {
		if strings.HasSuffix(n, ".conf") {
			listed = append(listed, n)
		}
	}
	time.Sleep(s.openAfter)
	for _, n := range listed {
		if _, err := s.f.Stat(fpmTestPoolDir + "/" + n); err != nil {
			s.mu.Lock()
			s.active = false
			s.deaths++
			s.mu.Unlock()
			return
		}
	}
	s.log("NOTICE: ready to handle connections")
}

func (s *fpmSim) counts() (reloads, starts, deaths int, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloads, s.starts, s.deaths, s.active
}

func fastFPMWait(t *testing.T, maxWait time.Duration) {
	t.Helper()
	m, tick, every, settle := fpmWaitMax, fpmWaitTick, fpmAskEvery, fpmSettle
	fpmWaitMax, fpmWaitTick, fpmAskEvery, fpmSettle = maxWait, time.Millisecond, 3, 30*time.Millisecond
	t.Cleanup(func() { fpmWaitMax, fpmWaitTick, fpmAskEvery, fpmSettle = m, tick, every, settle })
}

const fpmTestConf = "[global]\npid = /run/php/php8.3-fpm.pid\nerror_log = /var/log/php8.3-fpm.log\ninclude=/etc/php/8.3/fpm/pool.d/*.conf\n"

func fpmPoolRename(name string) *Rename {
	test, reload, _ := webPHPCmds("8.3")
	return &Rename{From: fpmTestPoolDir + "/" + name + ".conf", To: fpmTestPoolDir + "/" + name + ".conf" + SuspendedSuffix, Test: test, Reload: reload}
}

func fpmPools(s *fpmSim, names ...string) {
	for _, n := range names {
		s.f.SetFile(fpmTestPoolDir+"/"+n+".conf", []byte("["+n+"]\n"), 0o644, system.Root)
	}
}

// Bộ giả lập bắt được lỗi: đổi tên pool thứ hai ngay sau `systemctl reload`
// (đúng việc TPAN v0.2.15 làm khi khoá một khách có hai site) làm master chết.
// Không có phép thử này thì phép thử dưới có thể đạt chỉ vì bộ giả lập hiền.
func TestFPMSimTaiHienLoiCu(t *testing.T) {
	ctx := context.Background()
	s := newFPMSim(t, fpmTestConf)
	fpmPools(s, "a", "b", "c")
	test, reload, _ := webPHPCmds("8.3")
	for _, n := range []string{"a", "b"} {
		if err := s.f.Rename(fpmTestPoolDir+"/"+n+".conf", fpmTestPoolDir+"/"+n+".conf.suspended"); err != nil {
			t.Fatal(err)
		}
		_, _ = s.f.Run(ctx, *test)
		_, _ = s.f.Run(ctx, *reload) // cách cũ: không chờ
		// Engine ghi trạng thái bước vào SQLite rồi mới sang bước kế: vài ms.
		time.Sleep(5 * time.Millisecond)
	}
	s.busy.Wait()
	if _, _, deaths, active := s.counts(); deaths == 0 || active {
		t.Fatalf("bộ giả lập không tái hiện lỗi cũ: chết %d lần, active=%v", deaths, active)
	}
}

// Sửa chính: hai bước đổi tên pool liền nhau qua Rename (đường của khoá user,
// khoá site), master không chết, và lúc bước thứ hai đổi tên thì master đã ghi
// "ready" của lần reload thứ nhất.
func TestReloadFPMChoMasterDocXong(t *testing.T) {
	fastFPMWait(t, 2*time.Second)
	ctx := context.Background()
	s := newFPMSim(t, fpmTestConf)
	fpmPools(s, "a", "b", "c")
	for _, n := range []string{"a", "b", "c"} {
		if err := fpmPoolRename(n).Apply(ctx, s.f); err != nil {
			t.Fatalf("đổi tên pool %s: %v", n, err)
		}
		time.Sleep(5 * time.Millisecond) // engine ghi trạng thái bước, như phép thử trên
	}
	s.busy.Wait()
	reloads, starts, deaths, active := s.counts()
	if deaths != 0 || starts != 0 || !active || reloads != 3 {
		t.Fatalf("reload %d, start %d, chết %d, active=%v; muốn 3, 0, 0, true", reloads, starts, deaths, active)
	}
	b, _ := s.f.ReadFile(fpmTestLog)
	if got := strings.Count(string(b), fpmLineReady); got != 4 {
		t.Errorf("log có %d dòng ready; muốn 4 (lúc đầu và sau mỗi reload):\n%s", got, b)
	}
}

// Master chết giữa lần reload vì thứ khác (không phải TPAN) đổi pool.d:
// reloadFPM bật lại dịch vụ, bước vẫn thành công vì master mới đọc cấu hình
// đã qua `php-fpm -t`.
func TestReloadFPMMasterChetThiBatLai(t *testing.T) {
	fastFPMWait(t, 2*time.Second)
	ctx := context.Background()
	s := newFPMSim(t, fpmTestConf)
	s.listAfter, s.openAfter = 2*time.Millisecond, 40*time.Millisecond
	fpmPools(s, "a", "b")
	// Ai đó xoá pool b đúng lúc master đang đọc (Apply chạy `php-fpm -t` 20 ms
	// rồi mới reload; master liệt kê ở +2 ms, mở file ở +42 ms).
	go func() {
		time.Sleep(35 * time.Millisecond)
		_ = s.f.Remove(fpmTestPoolDir + "/b.conf")
	}()
	if err := fpmPoolRename("a").Apply(ctx, s.f); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	reloads, starts, deaths, active := s.counts()
	if deaths != 1 || starts != 1 || !active || reloads != 1 {
		t.Fatalf("reload %d, start %d, chết %d, active=%v; muốn 1, 1, 1, true", reloads, starts, deaths, active)
	}
}

// Master chết và start cũng hỏng: trả lỗi nói rõ đã dừng và cách xem.
func TestReloadFPMBatLaiHong(t *testing.T) {
	fastFPMWait(t, 2*time.Second)
	ctx := context.Background()
	s := newFPMSim(t, fpmTestConf)
	s.listAfter, s.openAfter = 2*time.Millisecond, 40*time.Millisecond
	fpmPools(s, "a", "b")
	s.f.Handle([]string{"systemctl", "start", "php8.3-fpm"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "Job for php8.3-fpm.service failed"}
	})
	go func() {
		time.Sleep(15 * time.Millisecond)
		_ = s.f.Remove(fpmTestPoolDir + "/b.conf")
	}()
	_, err := Reload(ctx, s.f, system.Cmd{Name: "systemctl", Args: []string{"reload", "php8.3-fpm"}})
	if err == nil {
		t.Fatal("Reload không lỗi dù PHP-FPM chết và không bật lại được")
	}
	for _, want := range []string{"php8.3-fpm đã dừng sau khi nạp lại", "journalctl -u php8.3-fpm"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("lỗi thiếu %q: %v", want, err)
		}
	}
}

// Reload bị từ chối vì dịch vụ đã tắt từ trước: giữ nguyên lỗi, không tự bật.
func TestReloadFPMDichVuTatTuTruoc(t *testing.T) {
	fastFPMWait(t, time.Second)
	s := newFPMSim(t, fpmTestConf)
	s.active = false
	_, err := Reload(context.Background(), s.f, system.Cmd{Name: "systemctl", Args: []string{"reload", "php8.3-fpm"}})
	if err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("Reload = %v; muốn lỗi not active", err)
	}
	if _, starts, _, active := s.counts(); starts != 0 || active {
		t.Errorf("start %d lần, active=%v; muốn không bật", starts, active)
	}
}

// Không đọc được php-fpm.conf (máy giả, --demo): chạy lệnh như cũ, một lệnh.
func TestReloadFPMKhongCoConfThiKhongCho(t *testing.T) {
	s := newFPMSim(t, "")
	if _, err := Reload(context.Background(), s.f, system.Cmd{Name: "systemctl", Args: []string{"reload", "php8.3-fpm"}}); err != nil {
		t.Fatal(err)
	}
	if r := s.f.Ran(); len(r) != 1 {
		t.Errorf("chạy %v; muốn đúng lệnh reload", r)
	}
	s.busy.Wait()
}

// Log bị xoay giữa chừng (file mới nhỏ hơn chỗ đã chụp): đọc từ đầu file mới.
func TestReloadFPMLogBiXoay(t *testing.T) {
	fastFPMWait(t, 2*time.Second)
	s := newFPMSim(t, fpmTestConf)
	big := strings.Repeat("[26-Nov-2026 15:00:00] NOTICE: cũ\n", 200)
	s.f.SetFile(fpmTestLog, []byte(big), 0o600, system.Root)
	s.f.Handle([]string{"systemctl", "reload", "php8.3-fpm"}, func(system.Cmd) system.Result {
		s.f.SetFile(fpmTestLog, nil, 0o600, system.Root) // logrotate
		s.log("NOTICE: Reloading in progress ...")
		go func() { time.Sleep(10 * time.Millisecond); s.log("NOTICE: " + fpmLineReady) }()
		return system.Result{}
	})
	start := time.Now()
	if _, err := Reload(context.Background(), s.f, system.Cmd{Name: "systemctl", Args: []string{"reload", "php8.3-fpm"}}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d >= time.Second {
		t.Errorf("chờ %v: không nhận ra log đã xoay", d)
	}
}

// error_log = syslog: chờ cố định rồi kiểm dịch vụ còn chạy.
func TestReloadFPMLogSyslog(t *testing.T) {
	fastFPMWait(t, time.Second)
	s := newFPMSim(t, "[global]\nerror_log = syslog\n")
	start := time.Now()
	if _, err := Reload(context.Background(), s.f, system.Cmd{Name: "systemctl", Args: []string{"reload", "php8.3-fpm"}}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < fpmSettle {
		t.Errorf("trả về sau %v; muốn chờ ít nhất %v", d, fpmSettle)
	}
	s.busy.Wait()
}

func TestFPMReloadVersionVaLog(t *testing.T) {
	for cmd, want := range map[string]string{
		"systemctl reload php8.3-fpm": "8.3", "systemctl reload php8.4-fpm": "8.4",
		"systemctl reload nginx": "", "systemctl restart php8.3-fpm": "", "systemctl reload php8.3; x-fpm": "",
	} {
		f := strings.Fields(cmd)
		got, ok := fpmReloadVersion(system.Cmd{Name: f[0], Args: f[1:]})
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v; muốn %q", cmd, got, ok, want)
		}
	}
	f := system.NewFake()
	for conf, want := range map[string]string{
		"[global]\n;error_log = log/php-fpm.log\n":                          "/var/log/php8.3-fpm.log",
		"[global]\nerror_log = /var/log/khac.log\n":                         "/var/log/khac.log",
		"[global]\nerror_log = syslog\n":                                    "",
		"[global]\n[www]\nerror_log = /khong/phai/global.log\n":             "/var/log/php8.3-fpm.log",
		"[global]\nerror_log = \"/var/log/co ngoac.log\"\ninclude=x/*.conf": "/var/log/co ngoac.log",
	} {
		f.SetFile(fpmConfPath("8.3"), []byte(conf), 0o644, system.Root)
		if got, ok := fpmLogOf(f, "8.3"); got != want || !ok {
			t.Errorf("%q: %q %v; muốn %q", conf, got, ok, want)
		}
	}
}
