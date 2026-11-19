package steps

import (
	"context"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// Trần chờ ngắn cho cả gói test: các test pool khác chạy trên FakeSystem không
// tạo socket, chờ 5 giây mỗi lần thì gói test chậm vô cớ.
func init() { waitForTimeout = 300 * time.Millisecond }

var wwwSock = system.Owner{User: "www-data", Group: "www-data"}

// Pool PHP-FPM: socket chỉ có SAU lần reload (như PHP-FPM thật, USR2 rồi trả
// về ngay). Apply phải chờ tới khi socket có rồi mới trả về.
func TestSitePoolWaitsForSocket(t *testing.T) {
	l := webLayout(t)
	m := system.NewFake()
	m.SetDir("/etc/php/8.3/fpm/pool.d", 0o755, system.Root)
	m.SetDir("/run/php", 0o755, system.Root)
	m.Handle([]string{"systemctl", "reload", "php8.3-fpm"}, func(system.Cmd) system.Result {
		// Socket có sau lần reload, ở một goroutine khác, như tiến trình thật.
		go func() {
			time.Sleep(50 * time.Millisecond)
			m.SetSocket(l.SocketFor("8.3"), 0o660, wwwSock)
		}()
		return system.Result{}
	})
	p := webStepOK(t)(SitePool(l, "8.3")).(*File)
	if p.WaitFor != l.SocketFor("8.3") {
		t.Fatalf("WaitFor = %q, muốn %q", p.WaitFor, l.SocketFor("8.3"))
	}
	if err := p.Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stat(l.SocketFor("8.3")); err != nil {
		t.Fatalf("Apply trả về trước khi socket có: %v", err)
	}
}

// Socket không bao giờ có: Apply vẫn thành công sau trần chờ (kiểm chứng của
// Plan mới là chỗ báo socket thiếu), không treo.
func TestFileWaitForTimeout(t *testing.T) {
	m := system.NewFake()
	m.SetDir("/etc/x", 0o755, system.Root)
	f := &File{Path: "/etc/x/a.conf", Content: []byte("a\n"), Perm: 0o644, Owner: system.Root,
		Reload: &system.Cmd{Name: "systemctl", Args: []string{"reload", "x"}}, WaitFor: "/run/x.sock"}
	start := time.Now()
	if err := f.Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < waitForTimeout || d > waitForTimeout+2*time.Second {
		t.Fatalf("Apply chờ %v, muốn khoảng %v", d, waitForTimeout)
	}
}

// Pha A của job hàng loạt (WithoutReload): không reload thì không chờ socket.
func TestFileWaitForSkippedWithoutReload(t *testing.T) {
	l := webLayout(t)
	m := system.NewFake()
	m.SetDir("/etc/php/8.3/fpm/pool.d", 0o755, system.Root)
	p := WithoutReload([]engine.Step{webStepOK(t)(SitePool(l, "8.3"))})[0].(*File)
	start := time.Now()
	if err := p.Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d >= waitForTimeout {
		t.Fatalf("Apply không reload mà vẫn chờ %v", d)
	}
	for _, c := range m.Ran() {
		if c.Name == "systemctl" {
			t.Fatalf("pha A không được reload: %v", c.String())
		}
	}
}
