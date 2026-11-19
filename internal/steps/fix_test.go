package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// runOne chạy một Plan một bước qua engine thật trên sys.
func runOne(t *testing.T, sys system.System, s engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(sys, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: []engine.Step{s}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

// ============================================================== PHPFPMRestart

const fixSocket = "/run/php/tpan-shop.sock"

// phpFake: `systemctl restart php8.3-fpm` tạo lại socket (như PHP-FPM thật
// bind lại mọi pool khi khởi động). fail khác 0 thì lệnh thoát mã đó.
func phpFake(fail int) *system.FakeSystem {
	f := system.NewFake()
	f.SetDir("/run/php", 0o755, system.Root)
	f.Handle([]string{"systemctl", "restart", "php8.3-fpm"}, func(system.Cmd) system.Result {
		if fail != 0 {
			return system.Result{ExitCode: fail, Stderr: "Job for php8.3-fpm.service failed\n"}
		}
		f.SetSocket(fixSocket, 0o660, system.Owner{User: "www-data", Group: "www-data"})
		return system.Result{}
	})
	return f
}

func TestPHPFPMRestart(t *testing.T) {
	ctx := context.Background()
	step := func() *PHPFPMRestart { return &PHPFPMRestart{PHPVersion: "8.3", Socket: fixSocket} }

	t.Run("socket mất thì Apply tạo lại, Check true", func(t *testing.T) {
		f := phpFake(0)
		s := step()
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("socket có thì chạy lại không restart", func(t *testing.T) {
		f := phpFake(0)
		f.SetSocket(fixSocket, 0o660, system.Root)
		rep := runOne(t, f, step())
		if rep.Steps[0].Outcome != "skipped" || len(f.Ran()) != 0 {
			t.Errorf("outcome %s, lệnh %v", rep.Steps[0].Outcome, ranStrings(f.Ran()))
		}
	})

	t.Run("file thường ở chỗ socket thì chưa đạt", func(t *testing.T) {
		f := phpFake(0)
		f.SetFile(fixSocket, nil, 0o644, system.Root)
		if done, err := step().Check(ctx, f); err != nil || done {
			t.Errorf("Check = %v, %v", done, err)
		}
	})

	t.Run("restart lỗi thì trả lỗi có ngữ cảnh", func(t *testing.T) {
		err := step().Apply(ctx, phpFake(1))
		var ee *system.ExitError
		if !errors.As(err, &ee) || !strings.Contains(err.Error(), "khởi động lại php8.3-fpm") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("Info", func(t *testing.T) {
		in := step().Info()
		if !in.Danger || in.NoUndo.IsZero() || in.Why.Vi == "" || in.Why.En == "" || in.Title.En == "" ||
			len(in.Commands) != 1 || in.Commands[0] != "systemctl restart php8.3-fpm" {
			t.Errorf("Info = %+v", in)
		}
	})

	t.Run("tham số sai", func(t *testing.T) {
		for _, s := range []*PHPFPMRestart{
			{PHPVersion: "", Socket: fixSocket},
			{PHPVersion: "8.3; rm -rf /", Socket: fixSocket},
			{PHPVersion: "$(id)", Socket: fixSocket},
			{PHPVersion: "8.3", Socket: ""},
			{PHPVersion: "8.3", Socket: "run/php/a.sock"},
			{PHPVersion: "8.3", Socket: "/run/php/../../etc/passwd"},
			{PHPVersion: "8.3", Socket: "/etc/passwd"},
			{PHPVersion: "8.3", Socket: "/run/php/a.sock\n"},
			{PHPVersion: "8.3", Socket: "/run/php/sub/a.sock"},
			{PHPVersion: "8.3", Socket: "/run/php/a.conf"},
		} {
			f := phpFake(0)
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %+v: muốn lỗi", s)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %+v: muốn lỗi", s)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("%+v: chạy lệnh %v", s, ranStrings(f.Ran()))
			}
		}
	})
}

// =============================================================== DBUserUnlock

var (
	lockFakeState  = regexp.MustCompile(`^SELECT COUNT\(\*\), COALESCE\(MAX\(JSON_VALUE\(Priv, '\$\.account_locked'\) = 'true'\), 0\) FROM mysql\.global_priv WHERE User='([a-z0-9_]+)' AND Host='localhost';$`)
	lockFakeAlter  = regexp.MustCompile(`^ALTER USER '([a-z0-9_]+)'@'localhost' ACCOUNT (LOCK|UNLOCK);$`)
	lockFakeUnlock = []string{"ALTER USER"}
)

// lockFake là MariaDB giả chỉ biết user nào có và có bị khoá không.
type lockFake struct {
	*system.FakeSystem
	mu     sync.Mutex
	users  map[string]bool // tên → bị khoá
	failOn []string
}

func newLockFake() *lockFake {
	f := &lockFake{FakeSystem: system.NewFake(), users: map[string]bool{}}
	f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		st := strings.TrimSpace(string(c.Stdin))
		for _, p := range f.failOn {
			if strings.HasPrefix(st, p) {
				return system.Result{ExitCode: 1, Stderr: "ERROR 1396 (HY000): Operation ALTER USER failed\n"}
			}
		}
		if m := lockFakeState.FindStringSubmatch(st); m != nil {
			locked, ok := f.users[m[1]]
			return system.Result{Stdout: fmt.Sprintf("%d\t%d\n", b2i(ok), b2i(locked))}
		}
		if m := lockFakeAlter.FindStringSubmatch(st); m != nil {
			if _, ok := f.users[m[1]]; !ok {
				return system.Result{ExitCode: 1, Stderr: "ERROR 1396 (HY000): Operation ALTER USER failed\n"}
			}
			f.users[m[1]] = m[2] == "LOCK"
			return system.Result{}
		}
		return system.Result{ExitCode: 1, Stderr: "lockFake: câu SQL lạ: " + st}
	})
	return f
}

func (f *lockFake) state() map[string]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.users)
}

func TestDBUserUnlock(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)
	locked := func() *lockFake {
		f := newLockFake()
		f.users[l.DB] = true
		f.users["tp_other_site"] = true // user của site khác không được đụng
		return f
	}

	t.Run("khoá rồi Apply thì Check true", func(t *testing.T) {
		f := locked()
		s := &DBUserUnlock{Layout: l}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		if !f.state()["tp_other_site"] {
			t.Error("mở khoá nhầm user khác")
		}
	})

	t.Run("chạy lại chỉ đọc", func(t *testing.T) {
		f := locked()
		if rep := runOne(t, f, &DBUserUnlock{Layout: l}); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu %s %s", rep.Job.Status, rep.Job.Error)
		}
		f.ResetRan()
		before := f.state()
		rep := runOne(t, f, &DBUserUnlock{Layout: l})
		if rep.Steps[0].Outcome != "skipped" {
			t.Errorf("lần hai %s", rep.Steps[0].Outcome)
		}
		for _, c := range f.Ran() {
			if !strings.HasPrefix(string(c.Stdin), "SELECT ") {
				t.Errorf("lần hai chạy %s (%s)", c.String(), c.Stdin)
			}
		}
		if !reflect.DeepEqual(before, f.state()) {
			t.Error("lần hai đổi máy")
		}
	})

	t.Run("Apply rồi Undo thì khoá lại", func(t *testing.T) {
		f := locked()
		before := f.state()
		s := &DBUserUnlock{Layout: l}
		if err := s.Undo(ctx, f); err == nil {
			t.Error("Undo trước Apply: muốn lỗi")
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(before, f.state()) {
			t.Fatal("Apply không đổi gì")
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, f.state()) {
			t.Errorf("sau Undo %v, trước %v", f.state(), before)
		}
	})

	t.Run("user không có thì Apply lỗi, không tạo gì", func(t *testing.T) {
		f := newLockFake()
		err := (&DBUserUnlock{Layout: l}).Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "không có") {
			t.Errorf("err = %v", err)
		}
		if len(f.state()) != 0 {
			t.Error("tạo user")
		}
	})

	t.Run("lệnh lỗi thì trả lỗi có ngữ cảnh", func(t *testing.T) {
		f := locked()
		f.failOn = lockFakeUnlock
		err := (&DBUserUnlock{Layout: l}).Apply(ctx, f)
		var ee *system.ExitError
		if !errors.As(err, &ee) || !strings.Contains(err.Error(), "mở khoá user DB "+l.DB) {
			t.Errorf("err = %v", err)
		}
		f = locked()
		f.failOn = []string{"SELECT"}
		if _, err := (&DBUserUnlock{Layout: l}).Check(ctx, f); err == nil || !strings.Contains(err.Error(), "trạng thái khoá") {
			t.Errorf("Check err = %v", err)
		}
	})

	t.Run("kết quả lạ thì lỗi", func(t *testing.T) {
		f := newLockFake()
		f.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result { return system.Result{Stdout: "1\n"} })
		if _, err := (&DBUserUnlock{Layout: l}).Check(ctx, f); err == nil {
			t.Error("muốn lỗi")
		}
	})

	t.Run("Layout sai", func(t *testing.T) {
		bad := l
		bad.DB = "tp_x'; DROP USER root; --"
		other := l
		other.DB = "tp_other_site"
		for _, lay := range []site.Layout{{}, bad, other} {
			f := locked()
			s := &DBUserUnlock{Layout: lay}
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %q: muốn lỗi", lay.DB)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %q: muốn lỗi", lay.DB)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("%q: chạy lệnh %v", lay.DB, ranStrings(f.Ran()))
			}
		}
	})

	t.Run("Info", func(t *testing.T) {
		in := (&DBUserUnlock{Layout: l}).Info()
		if in.ID != "db.unlock:"+l.Domain.String() || in.Why.Vi == "" || in.Why.En == "" || len(in.Commands) != 2 ||
			!strings.Contains(in.Commands[1], "ACCOUNT UNLOCK") || !in.NoUndo.IsZero() {
			t.Errorf("Info = %+v", in)
		}
	})
}

// =============================================================== WPConfigPerm

// wpPermFake: chmod chạy dưới user nào thì chỉ đổi được file user đó làm chủ
// (như chmod thật), và đi theo symlink như chmod thật.
func wpPermFake(t *testing.T, l site.Layout) *system.FakeSystem {
	t.Helper()
	f := system.NewFake()
	f.SetDir(l.Public, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
	f.Handle([]string{"chmod"}, func(c system.Cmd) system.Result {
		mode, err := strconv.ParseUint(c.Args[0], 8, 32)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "chmod: invalid mode\n"}
		}
		p := c.Args[len(c.Args)-1]
		target := p
		if fi, err := f.Stat(p); err == nil && fi.Mode&fs.ModeSymlink != 0 {
			target, _ = f.Readlink(p)
		}
		fi, err := f.Stat(target)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: err.Error() + "\n"}
		}
		if c.AsUser != "" && fi.Owner.User != c.AsUser {
			return system.Result{ExitCode: 1, Stderr: "chmod: changing permissions of '" + p + "': Operation not permitted\n"}
		}
		if err := f.Chmod(target, fs.FileMode(mode)); err != nil {
			return system.Result{ExitCode: 1, Stderr: err.Error() + "\n"}
		}
		return system.Result{}
	})
	return f
}

func TestWPConfigPerm(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)
	own := system.Owner{User: l.User, Group: l.User}

	cases := []struct {
		name  string
		perm  fs.FileMode
		owner system.Owner
	}{
		{"quyền 0644", 0o644, own},
		{"chủ root", 0o600, system.Root},
		{"chủ root và quyền 0644", 0o644, system.Root},
		{"chủ đúng, nhóm www-data, quyền 0640", 0o640, system.Owner{User: l.User, Group: site.WebGroup}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("Apply thì Check true", func(t *testing.T) {
				f := wpPermFake(t, l)
				f.SetFile(l.WPConfig, []byte("<?php // giả\n"), tc.perm, tc.owner)
				s := &WPConfigPerm{Layout: l}
				if done, err := s.Check(ctx, f); err != nil || done {
					t.Fatalf("Check trước = %v, %v", done, err)
				}
				if err := s.Apply(ctx, f); err != nil {
					t.Fatal(err)
				}
				if done, err := s.Check(ctx, f); err != nil || !done {
					t.Fatalf("Check sau = %v, %v", done, err)
				}
				fi, _ := f.Stat(l.WPConfig)
				if fi.Mode.Perm() != 0o600 || fi.Owner.User != l.User {
					t.Errorf("sau Apply: %v %+v", fi.Mode, fi.Owner)
				}
				if data, _ := f.ReadFile(l.WPConfig); string(data) != "<?php // giả\n" {
					t.Error("đổi nội dung wp-config.php")
				}
			})
			t.Run("Apply rồi Undo về như cũ", func(t *testing.T) {
				f := wpPermFake(t, l)
				f.SetFile(l.WPConfig, []byte("<?php\n"), tc.perm, tc.owner)
				before := f.Snapshot()
				s := &WPConfigPerm{Layout: l}
				if err := s.Apply(ctx, f); err != nil {
					t.Fatal(err)
				}
				if err := s.Undo(ctx, f); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, f.Snapshot()) {
					t.Error("sau Undo khác trước Apply")
				}
			})
		})
	}

	t.Run("đã đúng thì chạy lại không đổi gì", func(t *testing.T) {
		f := wpPermFake(t, l)
		// Nhóm khác cũng là đạt: phép kiểm WPConfigPerm không đòi nhóm.
		f.SetFile(l.WPConfig, []byte("<?php\n"), 0o600, system.Owner{User: l.User, Group: site.WebGroup})
		before := f.Snapshot()
		rep := runOne(t, f, &WPConfigPerm{Layout: l})
		if rep.Steps[0].Outcome != "skipped" || len(f.Ran()) != 0 || !reflect.DeepEqual(before, f.Snapshot()) {
			t.Errorf("outcome %s, lệnh %v", rep.Steps[0].Outcome, ranStrings(f.Ran()))
		}
	})

	t.Run("file mất thì từ chối, không tạo file rỗng", func(t *testing.T) {
		f := wpPermFake(t, l)
		s := &WPConfigPerm{Layout: l}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Errorf("Check = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "không có") {
			t.Errorf("Apply err = %v", err)
		}
		if _, err := f.Stat(l.WPConfig); !errors.Is(err, fs.ErrNotExist) {
			t.Error("tạo wp-config.php")
		}
	})

	t.Run("symlink thì từ chối, không đổi đích", func(t *testing.T) {
		f := wpPermFake(t, l)
		f.SetFile("/etc/shadow", []byte("root:*:\n"), 0o640, system.Owner{User: "root", Group: "shadow"})
		f.SetSymlink("/etc/shadow", l.WPConfig)
		before := f.Snapshot()
		s := &WPConfigPerm{Layout: l}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Errorf("Check = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err == nil {
			t.Error("Apply qua symlink: muốn lỗi")
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("đổi máy qua symlink")
		}
	})

	t.Run("chmod lỗi thì trả chủ cũ và báo lỗi có ngữ cảnh", func(t *testing.T) {
		f := wpPermFake(t, l)
		f.SetFile(l.WPConfig, []byte("<?php\n"), 0o644, system.Root)
		f.Handle([]string{"chmod"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "chmod: giả lập lỗi\n"}
		})
		before := f.Snapshot()
		err := (&WPConfigPerm{Layout: l}).Apply(ctx, f)
		var ee *system.ExitError
		if !errors.As(err, &ee) || !strings.Contains(err.Error(), "đặt quyền 600 cho "+l.WPConfig) {
			t.Errorf("err = %v", err)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("Apply lỗi mà không trả chủ cũ")
		}
	})

	t.Run("Layout sai", func(t *testing.T) {
		bad := l
		bad.WPConfig = "/etc/passwd"
		odd := l
		odd.User = "root"
		for _, lay := range []site.Layout{{}, bad, odd} {
			f := wpPermFake(t, l)
			f.SetFile(l.WPConfig, []byte("<?php\n"), 0o644, own)
			s := &WPConfigPerm{Layout: lay}
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %+v: muốn lỗi", lay.Domain)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %+v: muốn lỗi", lay.Domain)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("chạy lệnh %v", ranStrings(f.Ran()))
			}
		}
	})

	t.Run("Info", func(t *testing.T) {
		in := (&WPConfigPerm{Layout: l}).Info()
		if in.ID != "wp.config.perm:"+l.Domain.String() || in.Why.Vi == "" || in.Why.En == "" || len(in.Files) != 0 || len(in.Commands) != 2 {
			t.Errorf("Info = %+v", in)
		}
	})
}
