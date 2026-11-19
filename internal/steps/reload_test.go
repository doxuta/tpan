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
