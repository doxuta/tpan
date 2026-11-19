package steps

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// ================================================================= DBUserLock

func TestDBUserLock(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)
	open := func() *lockFake {
		f := newLockFake()
		f.users[l.DB] = false
		f.users["tp_other_site"] = false // user của site khác không được đụng
		return f
	}

	t.Run("mở rồi Apply thì Check true, Undo mở lại", func(t *testing.T) {
		f := open()
		before := f.state()
		s := &DBUserLock{Layout: l}
		if err := s.Undo(ctx, f); err == nil {
			t.Error("Undo trước Apply: muốn lỗi")
		}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		if f.state()["tp_other_site"] {
			t.Error("khoá nhầm user khác")
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, f.state()) {
			t.Errorf("sau Undo %v, trước %v", f.state(), before)
		}
	})

	t.Run("đã khoá thì chạy lại chỉ đọc, Undo không mở", func(t *testing.T) {
		f := open()
		if rep := runOne(t, f, &DBUserLock{Layout: l}); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu %s %s", rep.Job.Status, rep.Job.Error)
		}
		f.ResetRan()
		rep := runOne(t, f, &DBUserLock{Layout: l})
		if rep.Steps[0].Outcome != "skipped" {
			t.Errorf("lần hai %s", rep.Steps[0].Outcome)
		}
		for _, c := range f.Ran() {
			if !strings.HasPrefix(string(c.Stdin), "SELECT ") {
				t.Errorf("lần hai chạy %s (%s)", c.String(), c.Stdin)
			}
		}
		// Apply gọi thẳng trên user đã khoá: Undo không được mở khoá.
		s := &DBUserLock{Layout: l}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if err := s.Undo(ctx, f); err != nil || !f.state()[l.DB] {
			t.Errorf("Undo mở khoá user vốn đã khoá: %v", err)
		}
	})

	t.Run("user không có thì Check và Apply lỗi", func(t *testing.T) {
		f := newLockFake()
		s := &DBUserLock{Layout: l}
		if _, err := s.Check(ctx, f); err == nil || !strings.Contains(err.Error(), "không có") {
			t.Errorf("Check err = %v", err)
		}
		if err := s.Apply(ctx, f); err == nil {
			t.Error("Apply: muốn lỗi")
		}
		if len(f.state()) != 0 {
			t.Error("tạo user")
		}
	})

	t.Run("lệnh lỗi thì trả lỗi có ngữ cảnh", func(t *testing.T) {
		f := open()
		f.failOn = []string{"ALTER USER"}
		err := (&DBUserLock{Layout: l}).Apply(ctx, f)
		var ee *system.ExitError
		if !errors.As(err, &ee) || !strings.Contains(err.Error(), "khoá user DB "+l.DB) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("Layout sai", func(t *testing.T) {
		bad := l
		bad.DB = "tp_x'; DROP USER root; --"
		other := l
		other.DB = "tp_other_site"
		for _, lay := range []site.Layout{{}, bad, other} {
			f := open()
			s := &DBUserLock{Layout: lay}
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
		in := (&DBUserLock{Layout: l}).Info()
		if in.ID != "db.lock:"+l.Domain.String() || !in.Danger || in.Why.Vi == "" || in.Why.En == "" ||
			in.Title.Vi == "" || in.Title.En == "" || len(in.Commands) != 2 ||
			!strings.Contains(in.Commands[1], "ACCOUNT LOCK") || !in.NoUndo.IsZero() {
			t.Errorf("Info = %+v", in)
		}
	})
}

// =============================================================== SiteUserLock

// userLockFake: getent, passwd -S, usermod -L/-U có trạng thái. status là cột 2
// của passwd -S ("L", "NP", "P"); user không có trong map thì getent trả mã 2.
type userLockFake struct {
	*system.FakeSystem
	mu     sync.Mutex
	status map[string]string
	home   string
}

func newUserLockFake(l site.Layout, status string) *userLockFake {
	f := &userLockFake{FakeSystem: system.NewFake(), status: map[string]string{}, home: l.Dir}
	if status != "" {
		f.status[l.User] = status
	}
	f.Handle([]string{"getent", "passwd"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		name := c.Args[1]
		if _, ok := f.status[name]; !ok {
			return system.Result{ExitCode: 2}
		}
		return system.Result{Stdout: name + ":x:998:998::" + f.home + ":/usr/sbin/nologin\n"}
	})
	f.Handle([]string{"passwd", "-S"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		st, ok := f.status[c.Args[1]]
		if !ok {
			return system.Result{ExitCode: 1, Stderr: "passwd: user '" + c.Args[1] + "' does not exist\n"}
		}
		return system.Result{Stdout: c.Args[1] + " " + st + " 11/19/2026 0 99999 7 -1\n"}
	})
	f.Handle([]string{"usermod"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.status[c.Args[1]]; !ok {
			return system.Result{ExitCode: 6, Stderr: "usermod: user '" + c.Args[1] + "' does not exist\n"}
		}
		if c.Args[0] == "-L" {
			f.status[c.Args[1]] = "L"
		} else {
			f.status[c.Args[1]] = "NP"
		}
		return system.Result{}
	})
	return f
}

func (f *userLockFake) get(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status[name]
}

func (f *userLockFake) ranUsermod() bool {
	for _, c := range f.Ran() {
		if c.Name == "usermod" {
			return true
		}
	}
	return false
}

func TestSiteUserLock(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)

	for _, tc := range []struct {
		unlock         bool
		before, target string
	}{{false, "NP", "L"}, {true, "L", "NP"}} {
		t.Run("Unlock="+map[bool]string{false: "false", true: "true"}[tc.unlock], func(t *testing.T) {
			t.Run("Apply thì Check true, Undo trả về", func(t *testing.T) {
				f := newUserLockFake(l, tc.before)
				s := &SiteUserLock{Layout: l, Unlock: tc.unlock}
				if err := s.Undo(ctx, f); err == nil {
					t.Error("Undo trước Apply: muốn lỗi")
				}
				if done, err := s.Check(ctx, f); err != nil || done {
					t.Fatalf("Check trước = %v, %v", done, err)
				}
				if err := s.Apply(ctx, f); err != nil {
					t.Fatal(err)
				}
				if done, err := s.Check(ctx, f); err != nil || !done || f.get(l.User) != tc.target {
					t.Fatalf("Check sau = %v, %v (%s)", done, err, f.get(l.User))
				}
				if err := s.Undo(ctx, f); err != nil {
					t.Fatal(err)
				}
				if got := f.get(l.User); got != tc.before {
					t.Errorf("sau Undo %s, trước %s", got, tc.before)
				}
			})

			t.Run("đã đúng thì chạy lại chỉ đọc, Undo không đụng", func(t *testing.T) {
				f := newUserLockFake(l, tc.target)
				rep := runOne(t, f, &SiteUserLock{Layout: l, Unlock: tc.unlock})
				if rep.Steps[0].Outcome != "skipped" || f.ranUsermod() {
					t.Errorf("outcome %s, lệnh %v", rep.Steps[0].Outcome, ranStrings(f.Ran()))
				}
				s := &SiteUserLock{Layout: l, Unlock: tc.unlock}
				if err := s.Apply(ctx, f); err != nil {
					t.Fatal(err)
				}
				if err := s.Undo(ctx, f); err != nil || f.ranUsermod() || f.get(l.User) != tc.target {
					t.Errorf("Undo đổi user vốn đã đúng: %v, %v", err, ranStrings(f.Ran()))
				}
			})
		})
	}

	t.Run("user không có thì lỗi", func(t *testing.T) {
		f := newUserLockFake(l, "")
		s := &SiteUserLock{Layout: l}
		if _, err := s.Check(ctx, f); err == nil || !strings.Contains(err.Error(), "không có") {
			t.Errorf("Check err = %v", err)
		}
		if err := s.Apply(ctx, f); err == nil || f.ranUsermod() {
			t.Errorf("Apply err = %v, lệnh %v", err, ranStrings(f.Ran()))
		}
	})

	t.Run("user trùng tên không phải của tpan thì không đụng", func(t *testing.T) {
		f := newUserLockFake(l, "P")
		f.home = "/home/" + l.User
		if err := (&SiteUserLock{Layout: l}).Apply(ctx, f); err == nil || f.ranUsermod() {
			t.Errorf("err = %v, lệnh %v", err, ranStrings(f.Ran()))
		}
	})

	t.Run("passwd -S trả dòng lạ là lỗi", func(t *testing.T) {
		for _, out := range []string{"", "tp_other L\n", l.User + " X 1/1/2026\n", l.User + "\n"} {
			f := newUserLockFake(l, "NP")
			f.Handle([]string{"passwd", "-S"}, func(system.Cmd) system.Result { return system.Result{Stdout: out} })
			if _, err := (&SiteUserLock{Layout: l}).Check(ctx, f); err == nil {
				t.Errorf("%q: muốn lỗi", out)
			}
		}
	})

	t.Run("lệnh lỗi thì trả lỗi có ngữ cảnh", func(t *testing.T) {
		f := newUserLockFake(l, "NP")
		f.Handle([]string{"usermod"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 10, Stderr: "usermod: cannot lock /etc/passwd; try again later.\n"}
		})
		err := (&SiteUserLock{Layout: l}).Apply(ctx, f)
		var ee *system.ExitError
		if !errors.As(err, &ee) || !strings.Contains(err.Error(), "khoá user "+l.User) {
			t.Errorf("err = %v", err)
		}
		f = newUserLockFake(l, "NP")
		f.Handle([]string{"passwd", "-S"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 3} })
		if _, err := (&SiteUserLock{Layout: l}).Check(ctx, f); err == nil || !strings.Contains(err.Error(), "trạng thái khoá") {
			t.Errorf("Check err = %v", err)
		}
	})

	t.Run("Layout sai", func(t *testing.T) {
		root := l
		root.User = "root"
		opt := l
		opt.User = "-f"
		dir := l
		dir.Dir = "/etc"
		for _, lay := range []site.Layout{{}, root, opt, dir} {
			f := newUserLockFake(lay, "NP")
			s := &SiteUserLock{Layout: lay}
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %q: muốn lỗi", lay.User)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %q: muốn lỗi", lay.User)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("%q: chạy lệnh %v", lay.User, ranStrings(f.Ran()))
			}
		}
	})

	t.Run("Info", func(t *testing.T) {
		d := l.Domain.String()
		lock, unlock := (&SiteUserLock{Layout: l}).Info(), (&SiteUserLock{Layout: l, Unlock: true}).Info()
		if lock.ID != "user.lock:"+d || unlock.ID != "user.unlock:"+d {
			t.Errorf("ID %s, %s", lock.ID, unlock.ID)
		}
		for _, in := range []engine.StepInfo{lock, unlock} {
			if in.Why.Vi == "" || in.Why.En == "" || in.Title.Vi == "" || in.Title.En == "" ||
				len(in.Commands) != 2 || !in.NoUndo.IsZero() {
				t.Errorf("Info = %+v", in)
			}
		}
		if lock.Commands[1] != "usermod -L "+l.User || unlock.Commands[1] != "usermod -U "+l.User {
			t.Errorf("lệnh %v, %v", lock.Commands, unlock.Commands)
		}
	})
}

// =================================================================== MoveTree

func moveFake(l site.Layout) *system.FakeSystem {
	f := system.NewFake()
	f.SetDir("/var/www", 0o755, system.Root)
	f.SetDir(l.Dir, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
	f.SetFile(l.Public+"/index.php", []byte("<?php"), 0o640, system.Owner{User: l.User, Group: site.WebGroup})
	return f
}

func TestMoveTree(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)
	trash := site.TrashDir(l)
	toTrash := func() *MoveTree { return &MoveTree{From: l.Dir, To: trash} }

	t.Run("Apply thì Check true, Undo trả hệ thống file về y như cũ", func(t *testing.T) {
		f := moveFake(l)
		before := f.Snapshot()
		s := toTrash()
		if err := s.Undo(ctx, f); err == nil {
			t.Error("Undo trước Apply: muốn lỗi")
		}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		snap := f.Snapshot()
		if snap[trash+"/public/index.php"].Data != "<?php" {
			t.Error("file không đi theo thư mục")
		}
		if e := snap[site.TrashRoot]; e.Mode.Perm() != 0o700 || e.Owner != system.Root {
			t.Errorf("thùng rác %v %v, muốn 0700 root", e.Mode, e.Owner)
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		if after := f.Snapshot(); !reflect.DeepEqual(before, after) {
			t.Errorf("sau Undo khác trước:\n%v\n%v", after, before)
		}
	})

	t.Run("khôi phục: chuyển từ thùng rác về /var/www", func(t *testing.T) {
		f := moveFake(l)
		if err := toTrash().Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		rep := runOne(t, f, &MoveTree{From: trash, To: l.Dir})
		if rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s %s", rep.Job.Status, rep.Job.Error)
		}
		if f.Snapshot()[l.Public+"/index.php"].Data != "<?php" {
			t.Error("file không về chỗ cũ")
		}
	})

	t.Run("chạy lại thì bỏ qua, không đổi gì", func(t *testing.T) {
		f := moveFake(l)
		if rep := runOne(t, f, toTrash()); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := f.Snapshot()
		rep := runOne(t, f, toTrash())
		if rep.Steps[0].Outcome != "skipped" || !reflect.DeepEqual(before, f.Snapshot()) {
			t.Errorf("lần hai %s", rep.Steps[0].Outcome)
		}
	})

	t.Run("From và To cùng có thì lỗi, không đổi gì", func(t *testing.T) {
		f := moveFake(l)
		f.SetDir(trash, 0o700, system.Root)
		before := f.Snapshot()
		s := toTrash()
		if _, err := s.Check(ctx, f); err == nil || !strings.Contains(err.Error(), "hai bản") {
			t.Errorf("Check err = %v", err)
		}
		if err := s.Apply(ctx, f); err == nil || !reflect.DeepEqual(before, f.Snapshot()) {
			t.Errorf("Apply err = %v", err)
		}
	})

	t.Run("cả hai không có thì lỗi", func(t *testing.T) {
		f := system.NewFake()
		if _, err := toTrash().Check(ctx, f); err == nil {
			t.Error("muốn lỗi")
		}
	})

	t.Run("From là symlink hoặc To là file thì lỗi", func(t *testing.T) {
		f := system.NewFake()
		f.SetDir("/home/x", 0o755, system.Root)
		f.SetSymlink("/home/x", l.Dir)
		if err := toTrash().Apply(ctx, f); err == nil {
			t.Error("From symlink: muốn lỗi")
		}
		f = system.NewFake()
		f.SetFile(trash, nil, 0o600, system.Root)
		if _, err := toTrash().Check(ctx, f); err == nil {
			t.Error("To là file: muốn lỗi")
		}
	})

	t.Run("khác phân vùng thì lỗi nói rõ, dọn thư mục vừa tạo", func(t *testing.T) {
		f := moveFake(l)
		before := f.Snapshot()
		f.Fail("rename", l.Dir, &fs.PathError{Op: "rename", Path: l.Dir, Err: syscall.EXDEV})
		err := toTrash().Apply(ctx, f)
		if !errors.Is(err, syscall.EXDEV) || !strings.Contains(err.Error(), "hai phân vùng khác nhau") {
			t.Errorf("err = %v", err)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("Apply lỗi mà để lại thư mục")
		}
	})

	t.Run("hai site chung thùng rác: Undo một site không lỗi, giữ site kia", func(t *testing.T) {
		d, err := validate.ParseDomain("blog.example.com")
		if err != nil {
			t.Fatal(err)
		}
		lb := site.LayoutOf(d)
		f := moveFake(l)
		f.SetDir(lb.Dir, 0o2750, system.Owner{User: lb.User, Group: site.WebGroup})
		a, b := toTrash(), &MoveTree{From: lb.Dir, To: site.TrashDir(lb)}
		if err := a.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if err := b.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if err := a.Undo(ctx, f); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		snap := f.Snapshot()
		if _, ok := snap[l.Public+"/index.php"]; !ok {
			t.Error("site A không về /var/www")
		}
		if _, ok := snap[site.TrashDir(lb)]; !ok {
			t.Error("mất site B trong thùng rác")
		}
	})

	t.Run("Undo không đè khi From đã có lại", func(t *testing.T) {
		f := moveFake(l)
		s := toTrash()
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		f.SetDir(l.Dir, 0o755, system.Root)
		if err := s.Undo(ctx, f); err == nil {
			t.Error("muốn lỗi")
		}
	})

	t.Run("đường dẫn sai", func(t *testing.T) {
		for _, p := range []struct{ from, to string }{
			{"/var/www", trash},
			{l.Dir, site.TrashRoot},
			{l.Dir, l.Dir},
			{l.Dir, l.Dir + "/x"},
			{site.TrashRoot + "/a/b", site.TrashRoot + "/a"},
			{"/etc", trash},
			{l.Dir, "/root/x"},
			{"/var/wwwx/a", trash},
			{"/var/www/../etc", trash},
			{"/var/www/a/../../etc", trash},
			{"var/www/a", trash},
			{"", trash},
			{l.Dir, ""},
			{"/var/www/a\n", trash},
			{"/var/www/a/", trash},
			{"/var/www//a", trash},
			{"/var/lib/tpan/trashx/a", l.Dir},
		} {
			f := moveFake(l)
			before := f.Snapshot()
			s := &MoveTree{From: p.from, To: p.to}
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %q → %q: muốn lỗi", p.from, p.to)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %q → %q: muốn lỗi", p.from, p.to)
			}
			if !reflect.DeepEqual(before, f.Snapshot()) {
				t.Errorf("%q → %q: đổi hệ thống file", p.from, p.to)
			}
		}
	})

	t.Run("Info", func(t *testing.T) {
		in := toTrash().Info()
		if in.ID != "dir.move:"+l.Dir || !in.Danger || in.Why.Vi == "" || in.Why.En == "" ||
			in.Title.Vi == "" || in.Title.En == "" || !in.NoUndo.IsZero() {
			t.Errorf("Info = %+v", in)
		}
		own := &MoveTree{From: l.Dir, To: trash, Title: i18n.T("a", "b"), Why: i18n.T("c", "d")}
		if in := own.Info(); in.Title.Vi != "a" || in.Why.En != "d" {
			t.Errorf("Title, Why của người gọi bị bỏ: %+v", in)
		}
	})
}
