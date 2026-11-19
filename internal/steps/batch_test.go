package steps

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// ============================================================== ConfigReload

func TestConfigReload(t *testing.T) {
	ctx := context.Background()

	t.Run("cùng một bước chạy hai lần thì reload hai lần", func(t *testing.T) {
		// Pha C của job hàng loạt chạy lại Commit trên chính đối tượng này để
		// web server thôi phục vụ site vừa hoàn tác: lần hai phải reload thật.
		f := system.NewFake()
		s, err := WebReload("nginx")
		if err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			if rep := runOne(t, f, s); rep.Job.Status != engine.JobSucceeded || rep.Steps[0].Outcome == "skipped" {
				t.Fatalf("lần %d: %s %s %s", i+1, rep.Job.Status, rep.Steps[0].Outcome, rep.Job.Error)
			}
		}
		want := []string{"nginx -t", "systemctl reload nginx", "nginx -t", "systemctl reload nginx"}
		if got := ranStrings(f.Ran()); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("lệnh %v, muốn %v", got, want)
		}
	})

	t.Run("kiểm lỗi thì không reload, lỗi kèm output", func(t *testing.T) {
		f := system.NewFake()
		f.Handle([]string{"nginx", "-t"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "nginx: [emerg] unknown directive \"x\" in /etc/nginx/sites-enabled/a.conf:3\n"}
		})
		s, _ := WebReload("nginx")
		err := s.Apply(ctx, f)
		var ee *system.ExitError
		if !errors.As(err, &ee) || !strings.Contains(err.Error(), "unknown directive") || !strings.Contains(err.Error(), "nginx") {
			t.Errorf("err = %v", err)
		}
		if got := ranStrings(f.Ran()); len(got) != 1 {
			t.Errorf("lệnh %v", got)
		}
		if done, _ := s.Check(ctx, f); done {
			t.Error("Apply lỗi mà Check true")
		}
	})

	t.Run("reload lỗi thì trả lỗi có ngữ cảnh", func(t *testing.T) {
		f := system.NewFake()
		f.Handle([]string{"systemctl", "reload"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 1} })
		s, _ := PHPReload("8.3")
		if err := s.Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "nạp lại") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("Undo không làm gì", func(t *testing.T) {
		f := system.NewFake()
		s, _ := WebReload("apache")
		if err := s.Undo(ctx, f); err != nil || len(f.Ran()) != 0 {
			t.Errorf("Undo %v, lệnh %v", err, ranStrings(f.Ran()))
		}
	})

	t.Run("dựng bằng WebReload, PHPReload", func(t *testing.T) {
		for _, tc := range []struct {
			build   func() (*ConfigReload, error)
			id, cmd string
		}{
			{func() (*ConfigReload, error) { return WebReload("nginx") }, "service.reload:nginx", "nginx -t|systemctl reload nginx"},
			{func() (*ConfigReload, error) { return WebReload("apache") }, "service.reload:apache2", "apachectl configtest|systemctl reload apache2"},
			{func() (*ConfigReload, error) { return PHPReload("8.3") }, "service.reload:php8.3-fpm", "php-fpm8.3 -t|systemctl reload php8.3-fpm"},
		} {
			s, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			in := s.Info()
			if in.ID != tc.id || strings.Join(in.Commands, "|") != tc.cmd || in.Danger || in.NoUndo.IsZero() ||
				in.Why.Vi == "" || in.Why.En == "" || in.Title.Vi == "" || in.Title.En == "" {
				t.Errorf("Info = %+v", in)
			}
		}
		for _, w := range []string{"", "caddy", "nginx; rm -rf /", "$(id)"} {
			if _, err := WebReload(w); err == nil {
				t.Errorf("WebReload(%q): muốn lỗi", w)
			}
		}
		for _, v := range []string{"", "8", "8.3; rm -rf /", "$(id)", "../8.3", "8.3\n"} {
			if _, err := PHPReload(v); err == nil {
				t.Errorf("PHPReload(%q): muốn lỗi", v)
			}
		}
	})
}

// ============================================================= WithoutReload

func TestWithoutReload(t *testing.T) {
	test := &system.Cmd{Name: "nginx", Args: []string{"-t"}}
	reload := &system.Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}}
	file := &File{Path: "/etc/nginx/sites-available/a.conf", Test: test, Reload: reload}
	link := &Symlink{Target: file.Path, Link: "/etc/nginx/sites-enabled/a.conf", Test: test, Reload: reload}
	gone := &Absent{Path: "/etc/nginx/sites-enabled/b.conf", Test: test, Reload: reload}
	svc := &ConfigReload{Service: "nginx", Test: test, Reload: reload}
	ss := []engine.Step{file, link, gone, svc}

	got := WithoutReload(ss)
	if &got[0] != &ss[0] {
		t.Error("không trả lại đúng slice đã nhận")
	}
	if file.Reload != nil || link.Reload != nil || gone.Reload != nil {
		t.Error("còn Reload")
	}
	if file.Test != test || link.Test != test || gone.Test != test {
		t.Error("mất Test")
	}
	if svc.Reload != reload {
		t.Error("đụng bước loại khác")
	}

	// Ghi file ở pha A chỉ kiểm cú pháp, không reload.
	f := system.NewFake()
	f.SetDir("/etc/nginx/sites-available", 0o755, system.Root)
	if err := file.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if got := ranStrings(f.Ran()); len(got) != 1 || got[0] != "nginx -t" {
		t.Errorf("lệnh %v", got)
	}
}

// ==================================================================== Serial

// countStep đếm số Apply/Undo đang chạy cùng lúc và ghi lại mức cao nhất.
type countStep struct {
	id        string
	now, peak *atomic.Int32
}

func (c *countStep) Info() engine.StepInfo { return engine.StepInfo{ID: c.id} }
func (c *countStep) Check(context.Context, system.System) (bool, error) {
	return false, nil
}

func (c *countStep) work() error {
	n := c.now.Add(1)
	for {
		p := c.peak.Load()
		if n <= p || c.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	c.now.Add(-1)
	return nil
}

func (c *countStep) Apply(context.Context, system.System) error { return c.work() }
func (c *countStep) Undo(context.Context, system.System) error  { return c.work() }

func TestSerial(t *testing.T) {
	ctx := context.Background()
	f := system.NewFake()

	run := func(mu *sync.Mutex) int32 {
		var now, peak atomic.Int32
		var wg sync.WaitGroup
		for i := range 4 {
			var s engine.Step = &countStep{id: "x", now: &now, peak: &peak}
			if mu != nil {
				s = Serial(mu, s)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 3 {
					if i%2 == 0 {
						_ = s.Apply(ctx, f)
					} else {
						_ = s.Undo(ctx, f)
					}
				}
			}()
		}
		wg.Wait()
		return peak.Load()
	}

	// Không bọc thì các goroutine chồng lên nhau: chứng minh phép đếm bắt được.
	if p := run(nil); p < 2 {
		t.Skipf("không bọc mà vẫn không chạy chồng (peak %d); máy quá chậm để kiểm", p)
	}
	if p := run(&sync.Mutex{}); p != 1 {
		t.Errorf("bọc Serial mà có %d bước chạy cùng lúc", p)
	}

	t.Run("Info và Check đi thẳng, không chờ khoá", func(t *testing.T) {
		var mu sync.Mutex
		inner := &countStep{id: "site.user:tp_a", now: new(atomic.Int32), peak: new(atomic.Int32)}
		s := Serial(&mu, inner)
		mu.Lock()
		defer mu.Unlock()
		done := make(chan struct{})
		go func() {
			_, _ = s.Check(ctx, f)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Check chờ khoá")
		}
		if s.Info().ID != "site.user:tp_a" {
			t.Errorf("ID %s", s.Info().ID)
		}
	})

	t.Run("lỗi của bước gốc đi qua nguyên vẹn", func(t *testing.T) {
		s := Serial(&sync.Mutex{}, &File{Path: "relative"})
		if err := s.Apply(ctx, f); err == nil {
			t.Error("muốn lỗi")
		}
	})
}
