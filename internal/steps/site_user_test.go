package steps

import (
	"context"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	userPasswd = "/etc/passwd"
	userGroup  = "/etc/group"
	userRoot   = "root:x:0:0:root:/root:/bin/bash\n"
)

func userTestLayout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

// userMachine là machine cộng getent, useradd, userdel, rm có trạng thái. User
// nằm trong /etc/passwd và /etc/group giả, nên Snapshot (và m.whole) thấy được
// useradd, userdel: test "Undo về như cũ" so được cả user.
func userMachine(t *testing.T) *machine {
	t.Helper()
	m := newMachine(t)
	m.SetFile(userPasswd, []byte(userRoot), 0o644, system.Root)
	m.SetFile(userGroup, []byte("root:x:0:\n"), 0o644, system.Root)
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}
	read := func(p string) string {
		b, err := m.ReadFile(p)
		if err != nil {
			t.Errorf("đọc %s giả: %v", p, err)
		}
		return string(b)
	}
	find := func(name string) (string, bool) {
		for line := range strings.Lines(read(userPasswd)) {
			if strings.HasPrefix(line, name+":") {
				return line, true
			}
		}
		return "", false
	}
	drop := func(p, name string) {
		var out strings.Builder
		for line := range strings.Lines(read(p)) {
			if !strings.HasPrefix(line, name+":") {
				out.WriteString(line)
			}
		}
		m.SetFile(p, []byte(out.String()), 0o644, system.Root)
	}

	m.Handle([]string{"getent", "passwd"}, func(c system.Cmd) system.Result {
		if line, ok := find(c.Args[1]); ok {
			return exit(0, line, "")
		}
		return exit(2, "", "")
	})
	m.Handle([]string{"useradd"}, func(c system.Cmd) system.Result {
		name := c.Args[len(c.Args)-1]
		if _, ok := find(name); ok {
			return exit(9, "", "useradd: user '"+name+"' already exists")
		}
		arg := func(flag string) string { return c.Args[slices.Index(c.Args, flag)+1] }
		m.SetFile(userPasswd, []byte(read(userPasswd)+name+":x:999:999::"+arg("--home-dir")+":"+arg("--shell")+"\n"), 0o644, system.Root)
		m.SetFile(userGroup, []byte(read(userGroup)+name+":x:999:\n"), 0o644, system.Root)
		return exit(0, "", "")
	})
	m.Handle([]string{"userdel"}, func(c system.Cmd) system.Result {
		name := c.Args[len(c.Args)-1]
		if _, ok := find(name); !ok {
			return exit(6, "", "userdel: user '"+name+"' does not exist")
		}
		drop(userPasswd, name)
		drop(userGroup, name)
		return exit(0, "", "")
	})
	// rm -r: xoá p và mọi thứ bên dưới, từ trong ra ngoài.
	m.Handle([]string{"rm", "-r"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		var victims []string
		for k := range m.Snapshot() {
			if k == p || strings.HasPrefix(k, p+"/") {
				victims = append(victims, k)
			}
		}
		if len(victims) == 0 {
			return exit(1, "", "rm: cannot remove '"+p+"': No such file or directory")
		}
		slices.SortFunc(victims, func(a, b string) int { return len(b) - len(a) })
		for _, v := range victims {
			if err := m.Remove(v); err != nil {
				return exit(1, "", err.Error())
			}
		}
		return exit(0, "", "")
	})
	return m
}

// userOnlyGetent: lần chạy lại chỉ được hỏi, không được đổi user.
func userOnlyGetent(t *testing.T, m *machine) {
	t.Helper()
	for _, c := range m.Ran() {
		if c.Name != "getent" {
			t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
		}
	}
}

func userRanString(m *machine, want string) bool {
	return slices.Contains(ranStrings(m.Ran()), want)
}

// ================================================================== SiteUser

func TestSiteUser(t *testing.T) {
	ctx := context.Background()
	l := userTestLayout(t)
	useradd := "useradd --system --user-group --home-dir /var/www/example.com --no-create-home --shell /usr/sbin/nologin " + l.User

	t.Run("máy sạch rồi Apply thì Check true", func(t *testing.T) {
		m := userMachine(t)
		s := &SiteUser{Layout: l}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v; muốn false, nil", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
		}
		if !userRanString(m, useradd) {
			t.Errorf("không thấy lệnh %q trong %v", useradd, ranStrings(m.Ran()))
		}
		if g, _ := m.ReadFile(userGroup); !strings.Contains(string(g), l.User+":") {
			t.Error("useradd không tạo nhóm cùng tên")
		}
		if got := s.Info().Commands; !reflect.DeepEqual(got, []string{useradd}) {
			t.Errorf("Info().Commands = %v", got)
		}
	})

	t.Run("chạy lại thì chỉ hỏi getent", func(t *testing.T) {
		m := userMachine(t)
		if rep := execute(t, m, &SiteUser{Layout: l}); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := m.whole()
		m.ResetRan()
		rep := execute(t, m, &SiteUser{Layout: l})
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q (%s)", got, rep.Steps[0].Error)
		}
		userOnlyGetent(t, m)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})

	t.Run("Apply rồi Undo thì máy về như cũ", func(t *testing.T) {
		m := userMachine(t)
		before := m.whole()
		s := &SiteUser{Layout: l}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(before, m.whole()) {
			t.Fatal("Apply không đổi gì")
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("sau Undo máy khác trước Apply:\n%v\n%v", before, m.whole())
		}
	})

	t.Run("user có sẵn đúng dạng thì Undo không xoá", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userPasswd, []byte(userRoot+l.User+":x:998:998::"+l.Dir+":/usr/sbin/nologin\n"), 0o644, system.Root)
		before := m.whole()
		s := &SiteUser{Layout: l}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatal(err)
		}
		userOnlyGetent(t, m)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("đụng vào user không do bước này tạo")
		}
	})

	t.Run("user cùng tên mà home khác thì từ chối", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userPasswd, []byte(userRoot+l.User+":x:998:998::/home/khac:/bin/bash\n"), 0o644, system.Root)
		s := &SiteUser{Layout: l}
		if _, err := s.Check(ctx, m); err == nil || !strings.Contains(err.Error(), "không phải user do tpan tạo") {
			t.Errorf("Check = %v; muốn lỗi user lạ", err)
		}
		if err := s.Apply(ctx, m); err == nil {
			t.Error("Apply trên user lạ không lỗi")
		}
		userOnlyGetent(t, m)
	})

	t.Run("useradd lỗi thì lỗi có ngữ cảnh, máy như cũ", func(t *testing.T) {
		m := userMachine(t)
		before := m.whole()
		m.failCmd([]string{"useradd"}, 10, "useradd: cannot lock /etc/group")
		wantExit(t, (&SiteUser{Layout: l}).Apply(ctx, m), "tạo user "+l.User)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy bị đổi")
		}
	})

	t.Run("getent mã khác 2 là lỗi thật", func(t *testing.T) {
		m := userMachine(t)
		m.failCmd([]string{"getent", "passwd"}, 1, "")
		_, err := (&SiteUser{Layout: l}).Check(ctx, m)
		wantExit(t, err, "hỏi user "+l.User)
	})

	t.Run("getent trả dòng lạ là lỗi", func(t *testing.T) {
		m := userMachine(t)
		m.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "rác\n"}
		})
		if _, err := (&SiteUser{Layout: l}).Check(ctx, m); err == nil || !strings.Contains(err.Error(), "dòng lạ") {
			t.Errorf("Check = %v; muốn lỗi dòng lạ", err)
		}
	})

	t.Run("Undo khi chưa Apply thì lỗi", func(t *testing.T) {
		if err := (&SiteUser{Layout: l}).Undo(ctx, userMachine(t)); err == nil {
			t.Error("muốn lỗi")
		}
	})
}

// ============================================================ SiteUserAbsent

func TestSiteUserAbsent(t *testing.T) {
	ctx := context.Background()
	l := userTestLayout(t)
	withUser := func(t *testing.T) *machine {
		m := userMachine(t)
		if err := (&SiteUser{Layout: l}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.ResetRan()
		return m
	}

	t.Run("có user rồi Apply thì Check true, nhóm cũng mất", func(t *testing.T) {
		m := withUser(t)
		s := &SiteUserAbsent{Layout: l}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v", done, err)
		}
		if !userRanString(m, "userdel "+l.User) {
			t.Errorf("không có lệnh userdel: %v", ranStrings(m.Ran()))
		}
		if g, _ := m.ReadFile(userGroup); strings.Contains(string(g), l.User) {
			t.Error("nhóm cùng tên còn")
		}
	})

	t.Run("chạy lại thì chỉ hỏi getent", func(t *testing.T) {
		m := withUser(t)
		execute(t, m, &SiteUserAbsent{Layout: l})
		before := m.whole()
		m.ResetRan()
		rep := execute(t, m, &SiteUserAbsent{Layout: l})
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q (%s)", got, rep.Steps[0].Error)
		}
		userOnlyGetent(t, m)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})

	t.Run("Danger và NoUndo có lý do", func(t *testing.T) {
		info := (&SiteUserAbsent{Layout: l}).Info()
		if !info.Danger || info.NoUndo.IsZero() {
			t.Errorf("Danger %v, NoUndo %v", info.Danger, info.NoUndo)
		}
	})

	t.Run("còn tiến trình thì lỗi nói rõ", func(t *testing.T) {
		m := withUser(t)
		m.failCmd([]string{"userdel"}, 8, "userdel: user "+l.User+" is currently used by process 1234")
		err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m)
		wantExit(t, err, "còn tiến trình đang chạy")
		if !strings.Contains(err.Error(), "process 1234") {
			t.Errorf("lỗi thiếu stderr của userdel: %v", err)
		}
	})

	t.Run("userdel mã 6 (không có user) là đạt", func(t *testing.T) {
		m := withUser(t)
		m.failCmd([]string{"userdel"}, 6, "userdel: user does not exist")
		if err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m); err != nil {
			t.Errorf("Apply = %v", err)
		}
	})

	t.Run("userdel lỗi khác thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := withUser(t)
		m.failCmd([]string{"userdel"}, 10, "userdel: cannot lock /etc/group")
		wantExit(t, (&SiteUserAbsent{Layout: l}).Apply(ctx, m), "xoá user "+l.User)
	})

	t.Run("user cùng tên không phải của tpan thì không xoá", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userPasswd, []byte(userRoot+l.User+":x:1000:1000::/home/ai:/bin/bash\n"), 0o644, system.Root)
		s := &SiteUserAbsent{Layout: l}
		if _, err := s.Check(ctx, m); err == nil {
			t.Error("Check không lỗi")
		}
		if err := s.Apply(ctx, m); err == nil {
			t.Error("Apply không lỗi")
		}
		userOnlyGetent(t, m)
	})
}

// Layout zero hay bị dựng tay với tên, đường dẫn lạ: Check và Apply đều lỗi,
// không lệnh nào chạy.
func TestUserBadLayout(t *testing.T) {
	ctx := context.Background()
	good := userTestLayout(t)
	with := func(f func(*site.Layout)) site.Layout {
		l := good
		f(&l)
		return l
	}
	cases := map[string]site.Layout{
		"Layout zero":             {},
		"user root":               with(func(l *site.Layout) { l.User = "root" }),
		"user là tuỳ chọn":        with(func(l *site.Layout) { l.User = "-f" }),
		"user chèn lệnh":          with(func(l *site.Layout) { l.User = "tp_x; rm -rf /" }),
		"user có $(id)":           with(func(l *site.Layout) { l.User = "tp_$(id)" }),
		"user có xuống dòng":      with(func(l *site.Layout) { l.User = "tp_x\nroot" }),
		"user dài":                with(func(l *site.Layout) { l.User = "tp_" + strings.Repeat("a", 300) }),
		"user rỗng":               with(func(l *site.Layout) { l.User = "" }),
		"dir có ..":               with(func(l *site.Layout) { l.Dir = "/var/www/../../etc/passwd" }),
		"dir tương đối":           with(func(l *site.Layout) { l.Dir = "var/www/example.com" }),
		"dir rỗng":                with(func(l *site.Layout) { l.Dir = "" }),
		"dir có ký tự điều khiển": with(func(l *site.Layout) { l.Dir = "/var/www/a\nb" }),
	}
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			for _, s := range []engine.Step{&SiteUser{Layout: l}, &SiteUserAbsent{Layout: l}} {
				m := userMachine(t)
				if _, err := s.Check(ctx, m); err == nil {
					t.Errorf("%T.Check không lỗi", s)
				}
				if err := s.Apply(ctx, m); err == nil {
					t.Errorf("%T.Apply không lỗi", s)
				}
				if len(m.Ran()) != 0 {
					t.Errorf("%T chạy lệnh: %v", s, ranStrings(m.Ran()))
				}
			}
		})
	}
	t.Run("SiteDirs của Layout zero", func(t *testing.T) {
		m := userMachine(t)
		for _, s := range SiteDirs(site.Layout{}) {
			if _, err := s.Check(ctx, m); err == nil {
				t.Errorf("%s: Check không lỗi", s.Info().ID)
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Errorf("%s: Apply không lỗi", s.Info().ID)
			}
		}
	})
}

// ================================================================== SiteDirs

func TestSiteDirs(t *testing.T) {
	ctx := context.Background()
	l := userTestLayout(t)
	web := system.Owner{User: l.User, Group: "www-data"}
	own := system.Owner{User: l.User, Group: l.User}

	t.Run("đúng thứ tự", func(t *testing.T) {
		var ids []string
		for _, s := range SiteDirs(l) {
			ids = append(ids, s.Info().ID)
		}
		want := []string{
			"dir:" + l.Dir, "dir:" + l.Public, "dir:" + l.Tmp,
			"dir:" + l.LogDir, "owned-file:" + l.AccessLog, "owned-file:" + l.ErrorLog, "owned-file:" + l.PHPErrorLog,
		}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("thứ tự %v, muốn %v", ids, want)
		}
	})

	t.Run("SiteLogs là đúng bốn bước log ở cuối SiteDirs", func(t *testing.T) {
		var ids []string
		for _, s := range SiteLogs(l) {
			ids = append(ids, s.Info().ID)
		}
		want := []string{"dir:" + l.LogDir, "owned-file:" + l.AccessLog, "owned-file:" + l.ErrorLog, "owned-file:" + l.PHPErrorLog}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("thứ tự %v, muốn %v", ids, want)
		}
	})

	// Site có từ trước M14: thư mục log root:root 0755, access.log và error.log
	// do web server tự tạo với 0644 và đã có nội dung. SiteLogs siết quyền mà
	// không mất dòng log nào; Undo trả đúng quyền cũ.
	t.Run("SiteLogs trên site cũ: siết quyền, giữ nội dung, Undo về như cũ", func(t *testing.T) {
		m := userMachine(t)
		m.SetDir(l.LogDir, 0o755, system.Root)
		m.SetFile(l.AccessLog, []byte("192.0.2.1 - - GET /\n"), 0o644, system.Root)
		m.SetFile(l.ErrorLog, []byte("lỗi cũ\n"), 0o644, system.Root)
		m.SetFile(l.PHPErrorLog, []byte("PHP Warning\n"), 0o640, own)
		before := m.whole()

		ss := SiteLogs(l)
		for _, s := range ss {
			done, err := s.Check(ctx, m)
			if err != nil {
				t.Fatalf("Check %s: %v", s.Info().ID, err)
			}
			if done != (s.Info().ID == "owned-file:"+l.PHPErrorLog) {
				t.Errorf("Check %s = %v trên site cũ", s.Info().ID, done)
			}
			if !done {
				if err := s.Apply(ctx, m); err != nil {
					t.Fatalf("Apply %s: %v", s.Info().ID, err)
				}
			}
		}
		grp := system.Owner{User: "root", Group: l.User}
		snap := m.Snapshot()
		want := map[string]system.FakeEntry{
			l.LogDir:      {Mode: fs.ModeDir | 0o750, Owner: grp},
			l.AccessLog:   {Mode: 0o640, Owner: grp, Data: "192.0.2.1 - - GET /\n"},
			l.ErrorLog:    {Mode: 0o640, Owner: grp, Data: "lỗi cũ\n"},
			l.PHPErrorLog: {Mode: 0o640, Owner: own, Data: "PHP Warning\n"},
		}
		for p, w := range want {
			if got := snap[p]; got != w {
				t.Errorf("%s = %+v, muốn %+v", p, got, w)
			}
		}
		if len(m.Ran()) != 0 {
			t.Errorf("SiteLogs chạy lệnh: %v", ranStrings(m.Ran()))
		}

		for _, s := range slices.Backward(ss[:3]) {
			if err := s.Undo(ctx, m); err != nil {
				t.Fatalf("Undo %s: %v", s.Info().ID, err)
			}
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("sau Undo máy khác trước:\n%v\n%v", before, m.whole())
		}
	})

	t.Run("máy sạch: đúng quyền và chủ theo mục 5, chạy lại không đổi gì", func(t *testing.T) {
		m := userMachine(t)
		if rep := execute(t, m, SiteDirs(l)...); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s %s", rep.Job.Status, rep.Job.Error)
		}
		snap := m.Snapshot()
		want := map[string]system.FakeEntry{
			l.Dir:    {Mode: fs.ModeDir | fs.ModeSetgid | 0o750, Owner: web},
			l.Public: {Mode: fs.ModeDir | fs.ModeSetgid | 0o750, Owner: web},
			l.Tmp:    {Mode: fs.ModeDir | 0o700, Owner: own},
			// M14 (lỗ L6): log theo nhóm riêng của site, "khác" không có quyền gì.
			l.LogDir:      {Mode: fs.ModeDir | 0o750, Owner: system.Owner{User: "root", Group: l.User}},
			l.AccessLog:   {Mode: 0o640, Owner: system.Owner{User: "root", Group: l.User}},
			l.ErrorLog:    {Mode: 0o640, Owner: system.Owner{User: "root", Group: l.User}},
			l.PHPErrorLog: {Mode: 0o640, Owner: own},
		}
		for p, w := range want {
			if got := snap[p]; got != w {
				t.Errorf("%s = %+v, muốn %+v", p, got, w)
			}
		}

		before := m.whole()
		m.ResetRan()
		rep := execute(t, m, SiteDirs(l)...)
		for _, r := range rep.Steps {
			if r.Outcome != "skipped" {
				t.Errorf("lần hai %s: %s %s", r.ID, r.Outcome, r.Error)
			}
		}
		if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("lần hai đổi máy hoặc chạy lệnh: %v", ranStrings(m.Ran()))
		}
	})

	t.Run("Apply hết rồi Undo ngược thì máy về như cũ", func(t *testing.T) {
		m := userMachine(t)
		before := m.whole()
		ss := SiteDirs(l)
		for _, s := range ss {
			if err := s.Apply(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
		for _, s := range slices.Backward(ss) {
			if err := s.Undo(ctx, m); err != nil {
				t.Fatalf("Undo %s: %v", s.Info().ID, err)
			}
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("sau Undo máy khác trước:\n%v\n%v", before, m.whole())
		}
	})
}

// ================================================================= OwnedFile

func TestOwnedFile(t *testing.T) {
	ctx := context.Background()
	const logDir = "/var/log/tpan/sites/example.com"
	const p = logDir + "/php-error.log"
	own := system.Owner{User: "tp_example_com_abc123", Group: "tp_example_com_abc123"}
	step := func() engine.Step { return &OwnedFile{Path: p, Perm: 0o640, Owner: own} }

	for _, lc := range []lifecycle{
		{name: "chưa có", setup: func(m *machine) { m.SetDir(logDir, 0o755, system.Root) }, step: step},
		{name: "có sẵn sai quyền và chủ", setup: func(m *machine) { m.SetFile(p, []byte("log cũ\n"), 0o644, system.Root) }, step: step},
	} {
		t.Run(lc.name, lc.run)
	}

	t.Run("có sẵn thì giữ nguyên nội dung", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(p, []byte("PHP Warning: x\n"), 0o600, system.Root)
		if err := step().Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if b, _ := m.ReadFile(p); string(b) != "PHP Warning: x\n" {
			t.Errorf("nội dung bị đổi: %q", b)
		}
	})

	t.Run("mới tạo thì rỗng", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir(logDir, 0o755, system.Root)
		if err := step().Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := m.Snapshot()[p]; got != (system.FakeEntry{Mode: 0o640, Owner: own}) {
			t.Errorf("file = %+v", got)
		}
	})

	t.Run("symlink hay thư mục ở chỗ file thì từ chối, không chmod qua link", func(t *testing.T) {
		for name, setup := range map[string]func(*machine){
			"symlink": func(m *machine) {
				m.SetFile("/etc/shadow", []byte("bí mật"), 0o640, system.Root)
				m.SetSymlink("/etc/shadow", p)
			},
			"thư mục": func(m *machine) { m.SetDir(p, 0o755, system.Root) },
		} {
			m := newMachine(t)
			setup(m)
			before := m.whole()
			s := step()
			if _, err := s.Check(ctx, m); err == nil || !strings.Contains(err.Error(), "không phải file thường") {
				t.Errorf("%s: Check = %v", name, err)
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Errorf("%s: Apply không lỗi", name)
			}
			if !reflect.DeepEqual(before, m.whole()) {
				t.Errorf("%s: máy bị đổi", name)
			}
		}
	})

	t.Run("chmod lỗi thì lỗi có ngữ cảnh, máy như cũ", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(p, []byte("x"), 0o644, system.Root)
		before := m.whole()
		m.Fail("chmod", p, syscall.EACCES)
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đổi quyền "+p) {
			t.Fatalf("Apply = %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy bị đổi")
		}
	})

	t.Run("ghi lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir(logDir, 0o755, system.Root)
		m.Fail("write", p, syscall.ENOSPC)
		if err := step().Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "tạo "+p) {
			t.Fatalf("Apply = %v", err)
		}
	})

	t.Run("tham số xấu", func(t *testing.T) {
		for _, s := range []*OwnedFile{
			{Path: "", Perm: 0o640},
			{Path: "var/log/x.log", Perm: 0o640},
			{Path: "/var/log/../../etc/passwd", Perm: 0o640},
			{Path: p, Perm: fs.ModeSetuid | 0o640},
		} {
			m := newMachine(t)
			if _, err := s.Check(ctx, m); err == nil {
				t.Errorf("%q %v: Check không lỗi", s.Path, s.Perm)
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Errorf("%q %v: Apply không lỗi", s.Path, s.Perm)
			}
		}
	})
}

// ================================================================ RemoveTree

func TestRemoveTree(t *testing.T) {
	ctx := context.Background()
	// Thùng rác (chỗ RemoveTree thật sự chạy), không phải thư mục site: từ M28
	// lệnh root không được nhận đường dẫn trong /var/www/<site>/.
	const dir = "/var/lib/tpan/trash/example_com_1"
	setup := func(t *testing.T) *machine {
		m := userMachine(t)
		m.SetFile(dir+"/public/index.php", []byte("<?php"), 0o640, system.Root)
		m.SetDir(dir+"/tmp", 0o700, system.Root)
		m.SetFile("/var/lib/tpan/trash/example_com_1.bak/giu", []byte("giữ"), 0o644, system.Root)
		m.ResetRan()
		return m
	}
	step := func() engine.Step { return &RemoveTree{Path: dir, Under: "/var/lib/tpan/trash"} }

	t.Run("xoá đúng cây, chạy lại không làm gì", func(t *testing.T) {
		m := setup(t)
		s := step()
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		if got, want := ranStrings(m.Ran()), []string{"rm -r --one-file-system -- " + dir}; !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh %v, muốn %v", got, want)
		}
		if _, err := m.ReadFile("/var/lib/tpan/trash/example_com_1.bak/giu"); err != nil {
			t.Errorf("xoá lan sang thư mục bên cạnh: %v", err)
		}
		if _, err := m.Stat("/var/lib/tpan/trash"); err != nil {
			t.Errorf("xoá cả Under: %v", err)
		}

		before := m.whole()
		m.ResetRan()
		rep := execute(t, m, step())
		if rep.Steps[0].Outcome != "skipped" || len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("lần hai: %s, lệnh %v", rep.Steps[0].Outcome, ranStrings(m.Ran()))
		}
	})

	t.Run("Danger và NoUndo có lý do", func(t *testing.T) {
		info := step().Info()
		if !info.Danger || info.NoUndo.IsZero() || len(info.Files) != 0 {
			t.Errorf("Danger %v, NoUndo %v, Files %v", info.Danger, info.NoUndo, info.Files)
		}
	})

	t.Run("rm lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := setup(t)
		m.failCmd([]string{"rm", "-r"}, 1, "rm: cannot remove '"+dir+"/x': Device or resource busy")
		wantExit(t, step().Apply(ctx, m), "xoá "+dir)
	})

	t.Run("đường dẫn xấu thì từ chối, không chạy rm", func(t *testing.T) {
		for _, c := range []RemoveTree{
			{Path: "", Under: "/var/www"},
			{Path: dir, Under: ""},
			{Path: "/var/www", Under: "/var/www"},
			{Path: "/var/wwwx/a", Under: "/var/www"},
			{Path: "/etc", Under: "/var/www"},
			{Path: "/var/www/../etc", Under: "/var/www"},
			{Path: "/var/www/../../etc/passwd", Under: "/var/www"},
			{Path: "/var/www/a/", Under: "/var/www"},
			{Path: "/var/www//a", Under: "/var/www"},
			{Path: "var/www/a", Under: "/var/www"},
			{Path: "/var/www/a\n", Under: "/var/www"},
			{Path: dir, Under: "/var/www/"},
			{Path: dir, Under: "/"},
			{Path: "/", Under: "/"},
			{Path: dir, Under: "var/www"},
		} {
			m := setup(t)
			before := m.whole()
			if _, err := c.Check(ctx, m); err == nil {
				t.Errorf("%q dưới %q: Check không lỗi", c.Path, c.Under)
			}
			if err := c.Apply(ctx, m); err == nil {
				t.Errorf("%q dưới %q: Apply không lỗi", c.Path, c.Under)
			}
			if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
				t.Errorf("%q dưới %q: chạy %v", c.Path, c.Under, ranStrings(m.Ran()))
			}
		}
	})
}

// Mọi bước của file này có tiêu đề và lý do đủ hai thứ tiếng.
func TestUserInfo(t *testing.T) {
	l := userTestLayout(t)
	steps := append([]engine.Step{
		&SiteUser{Layout: l}, &SiteUserAbsent{Layout: l},
		&RemoveTree{Path: l.Dir, Under: "/var/www"},
	}, SiteDirs(l)...)
	for _, s := range steps {
		info := s.Info()
		if info.ID == "" || info.Title.Vi == "" || info.Title.En == "" || info.Why.Vi == "" || info.Why.En == "" {
			t.Errorf("%T: Info thiếu: %+v", s, info)
		}
	}
}
