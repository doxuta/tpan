package steps

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// nginxGens giả lập master nginx PID 100 qua các lần reload: mỗi lần `ps`
// trả output kế tiếp trong outs (hết thì giữ cái cuối). reloads đếm số lần
// `systemctl reload nginx`.
type nginxGens struct {
	mu      sync.Mutex
	outs    []string
	ps      int
	reloads int
}

func newNginxGens(t *testing.T, outs ...string) (*system.FakeSystem, *nginxGens) {
	t.Helper()
	f := system.NewFake()
	f.SetFile(NginxPIDFile, []byte("100\n"), 0o644, system.Root)
	g := &nginxGens{outs: outs}
	f.Handle([]string{"ps"}, func(c system.Cmd) system.Result {
		if got := strings.Join(c.Args, " "); got != "-o pid=,args= --ppid 100" {
			t.Errorf("ps %s, muốn -o pid=,args= --ppid 100", got)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		out := g.outs[min(g.ps, len(g.outs)-1)]
		g.ps++
		return system.Result{Stdout: out}
	})
	f.Handle([]string{"systemctl", "reload", "nginx"}, func(system.Cmd) system.Result {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.reloads++
		return system.Result{}
	})
	return f, g
}

func fastReloadWait(t *testing.T, maxWait time.Duration) {
	t.Helper()
	m, tick := reloadWaitMax, reloadWaitTick
	reloadWaitMax, reloadWaitTick = maxWait, time.Millisecond
	t.Cleanup(func() { reloadWaitMax, reloadWaitTick = m, tick })
}

const (
	genOld   = "  11 nginx: worker process\n  12 nginx: worker process\n  13 nginx: cache manager process\n"
	genMixed = "  11 nginx: worker process\n  12 nginx: worker process\n  21 nginx: worker process\n  22 nginx: worker process\n"
	genDrain = "  11 nginx: worker process is shutting down\n  21 nginx: worker process\n  22 nginx: worker process\n"
	genNew   = "  21 nginx: worker process\n  22 nginx: worker process\n  23 nginx: cache manager process\n"
)

// Reload nginx chỉ trả về khi mọi worker còn nhận kết nối là worker mới;
// worker cũ "is shutting down" (đã đóng cổng nghe) không phải chờ.
func TestReloadNginxChoTheHeMoi(t *testing.T) {
	fastReloadWait(t, 2*time.Second)
	ctx := context.Background()
	reload := system.Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}}

	// Trước reload: cũ. Sau reload: lẫn cũ mới (hai lần), rồi cũ đang thôi.
	f, g := newNginxGens(t, genOld, genMixed, genMixed, genDrain)
	if _, err := Reload(ctx, f, reload); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if g.reloads != 1 || g.ps != 4 {
		t.Errorf("reload %d lần, ps %d lần; muốn 1 và 4 (trước, lẫn, lẫn, đã đổi)", g.reloads, g.ps)
	}
	if r := f.Ran(); r[0].Name != "ps" || r[1].String() != reload.String() {
		t.Errorf("thứ tự lệnh: %v; muốn ps (chụp worker cũ) trước reload", r[:2])
	}

	// Qua runReload, đường mọi Step File/Symlink/Absent đi.
	f, g = newNginxGens(t, genOld, genMixed, genNew)
	if err := runReload(ctx, f, &reload, "bật vhost"); err != nil {
		t.Fatalf("runReload: %v", err)
	}
	if g.ps != 3 {
		t.Errorf("ps %d lần, muốn 3", g.ps)
	}
}

// Master không áp dụng config mới (worker cũ còn mãi): lỗi sau thời hạn,
// không im lặng coi như đã reload.
func TestReloadNginxHetHan(t *testing.T) {
	fastReloadWait(t, 30*time.Millisecond)
	f, _ := newNginxGens(t, genOld, genMixed)
	start := time.Now()
	err := runReload(context.Background(), f, nginxReload, "bật vhost")
	if err == nil || !strings.Contains(err.Error(), "worker cũ vẫn nhận kết nối") {
		t.Fatalf("lỗi = %v, muốn báo worker cũ vẫn nhận kết nối", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("chờ %v, quá thời hạn", d)
	}
}

// Không có PID master (máy giả, --demo), lệnh không phải reload nginx, hay
// apache: chỉ chạy đúng lệnh reload, không ps, không chờ.
func TestReloadKhongCho(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		pid bool
		cmd system.Cmd
	}{
		"không có /run/nginx.pid": {false, system.Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}}},
		"apache":                  {true, system.Cmd{Name: "systemctl", Args: []string{"reload", "apache2"}}},
		"php-fpm":                 {true, system.Cmd{Name: "systemctl", Args: []string{"reload", "php8.3-fpm"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := system.NewFake()
			if tc.pid {
				f.SetFile(NginxPIDFile, []byte("100\n"), 0o644, system.Root)
			}
			if _, err := Reload(ctx, f, tc.cmd); err != nil {
				t.Fatalf("Reload: %v", err)
			}
			if r := f.Ran(); len(r) != 1 || r[0].String() != tc.cmd.String() {
				t.Errorf("lệnh đã chạy: %v; muốn đúng một lệnh %s", r, tc.cmd.String())
			}
		})
	}
}

// PID file hỏng không được lọt vào argv của ps.
func TestReloadNginxPIDLa(t *testing.T) {
	f := system.NewFake()
	f.SetFile(NginxPIDFile, []byte("1 --pid 2\n"), 0o644, system.Root)
	if _, err := Reload(context.Background(), f, *nginxReload); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if r := f.Ran(); len(r) != 1 {
		t.Errorf("lệnh đã chạy: %v; muốn chỉ systemctl reload nginx", r)
	}
}

// nginxRestart giả lập nginx bị tiến trình khác khởi động lại quanh lúc tpan
// reload: PID file đổi từ 100 sang 200 (có thể mất một lúc), `ps --ppid 100`
// không còn dòng nào, `ps --ppid 200` trả worker mới.
type nginxRestart struct {
	mu sync.Mutex
	// gone: số lần đọc ps sau reload mà nginx còn đang dừng (không có PID file).
	gone int
	// newWorkers: output của ps cho master mới.
	newWorkers string
	// reloadFails: `systemctl reload nginx` trả lỗi (unit không chạy).
	reloadFails bool
	// stays: không bao giờ có master mới.
	stays    bool
	reloaded bool
	ps       int
}

func newNginxRestart(t *testing.T, r *nginxRestart) *system.FakeSystem {
	t.Helper()
	f := system.NewFake()
	f.SetFile(NginxPIDFile, []byte("100\n"), 0o644, system.Root)
	f.Handle([]string{"ps"}, func(c system.Cmd) system.Result {
		r.mu.Lock()
		defer r.mu.Unlock()
		ppid := c.Args[len(c.Args)-1]
		if !r.reloaded {
			if ppid != "100" {
				t.Errorf("ps --ppid %s trước reload, muốn 100", ppid)
			}
			return system.Result{Stdout: genOld}
		}
		r.ps++
		if ppid == "200" {
			return system.Result{Stdout: r.newWorkers}
		}
		return system.Result{} // master cũ đã mất: không còn tiến trình con
	})
	f.Handle([]string{"systemctl", "reload", "nginx"}, func(system.Cmd) system.Result {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.reloaded = true
		// Ngay sau lệnh reload, nginx bị dừng: PID file mất.
		_ = f.Remove(NginxPIDFile)
		if r.reloadFails {
			return system.Result{ExitCode: 1, Stderr: "nginx.service is not active, cannot reload.\n"}
		}
		return system.Result{}
	})
	// Mỗi lần đọc PID file sau reload: gone lần đầu chưa có, rồi master mới 200.
	go func() {
		for i := 0; i < 2000; i++ {
			time.Sleep(time.Millisecond)
			r.mu.Lock()
			ready := r.reloaded && r.ps >= 0 && !r.stays
			if ready && r.gone > 0 {
				r.gone--
				ready = false
			}
			r.mu.Unlock()
			if ready {
				f.SetFile(NginxPIDFile, []byte("200\n"), 0o644, system.Root)
				return
			}
		}
	}()
	return f
}

// nginx bị khởi động lại trong lúc tpan chờ reload: master mới có worker là
// config mới đã được áp dụng, không chờ hết hạn rồi hoàn tác.
func TestReloadNginxBiKhoiDongLai(t *testing.T) {
	fastReloadWait(t, 2*time.Second)
	for name, r := range map[string]*nginxRestart{
		"master mới lên ngay":                {newWorkers: genNew},
		"nginx dừng một lúc rồi mới lên":     {gone: 15, newWorkers: genNew},
		"lệnh reload lỗi vì nginx đang dừng": {gone: 5, newWorkers: genNew, reloadFails: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newNginxRestart(t, r)
			start := time.Now()
			if err := runReload(context.Background(), f, nginxReload, "bật vhost"); err != nil {
				t.Fatalf("runReload: %v", err)
			}
			if d := time.Since(start); d > time.Second {
				t.Errorf("chờ %v: master mới đã có worker mà vẫn chờ", d)
			}
		})
	}
}

// Master cũ mất mà không có master mới (nginx không lên lại): vẫn là lỗi, và
// lỗi nói đúng chuyện đó. Lệnh reload lỗi thì trả lỗi của lệnh.
func TestReloadNginxKhongLenLai(t *testing.T) {
	fastReloadWait(t, 40*time.Millisecond)
	f := newNginxRestart(t, &nginxRestart{stays: true})
	err := runReload(context.Background(), f, nginxReload, "bật vhost")
	if err == nil || !strings.Contains(err.Error(), "nginx không chạy lại") {
		t.Fatalf("lỗi = %v, muốn báo nginx không chạy lại", err)
	}
	f = newNginxRestart(t, &nginxRestart{stays: true, reloadFails: true})
	err = runReload(context.Background(), f, nginxReload, "bật vhost")
	if err == nil || strings.Contains(err.Error(), "nginx không chạy lại") || !strings.Contains(err.Error(), "nạp lại dịch vụ") {
		t.Fatalf("lỗi = %v, muốn lỗi của lệnh reload", err)
	}
	// Master mới lên nhưng chưa có worker nào tới hết hạn: chưa nhận kết nối, vẫn lỗi.
	f = newNginxRestart(t, &nginxRestart{newWorkers: "  23 nginx: cache manager process\n"})
	if err := runReload(context.Background(), f, nginxReload, "bật vhost"); err == nil {
		t.Fatal("master mới chưa có worker mà coi là xong")
	}
}

// Lệnh reload lỗi mà master không đổi (cấu hình sai, systemd lỗi): trả lỗi
// của lệnh, không coi worker mới cùng master là thành công.
func TestReloadNginxLenhLoiCungMaster(t *testing.T) {
	fastReloadWait(t, 40*time.Millisecond)
	f := system.NewFake()
	f.SetFile(NginxPIDFile, []byte("100\n"), 0o644, system.Root)
	f.Handle([]string{"ps"}, func(system.Cmd) system.Result { return system.Result{Stdout: genNew} })
	f.Handle([]string{"systemctl", "reload", "nginx"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "Job for nginx.service failed.\n"}
	})
	if err := runReload(context.Background(), f, nginxReload, "bật vhost"); err == nil {
		t.Fatal("lệnh reload lỗi mà runReload trả nil")
	}
}
