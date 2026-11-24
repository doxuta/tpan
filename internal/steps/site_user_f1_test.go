package steps

// F1 của rà soát v0.2.0 (M28 đợt 2): UID của user site không bao giờ được cấp
// lại, và xoá hẳn dọn crontab, tiến trình, file tạm của UID trước userdel.

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// memUIDs là UIDReserver trong bộ nhớ, cùng luật với store.ReserveSiteUID.
type memUIDs struct {
	mu    sync.Mutex
	max   int
	calls int
}

func (u *memUIDs) ReserveSiteUID(_ context.Context, floor int) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	u.max = max(u.max+1, floor)
	return u.max, nil
}

func f1Layout(t *testing.T, domain string) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain(domain)
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

// f1UID trả UID của user name trong /etc/passwd giả.
func f1UID(t *testing.T, m *machine, name string) string {
	t.Helper()
	b, _ := m.ReadFile(userPasswd)
	for line := range strings.Lines(string(b)) {
		if f := strings.Split(line, ":"); f[0] == name {
			return f[2]
		}
	}
	t.Fatalf("không có user %s trong passwd giả", name)
	return ""
}

func TestSiteUserCapUIDKhongDungLai(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a, b := f1Layout(t, "a.example.com"), f1Layout(t, "b.example.com")
	m := userMachine(t)

	// Site A tạo, rồi bị xoá hẳn.
	if rep := execute(t, m, &SiteUser{Layout: a, UIDs: st}); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("tạo A: %s", rep.Job.Error)
	}
	if got := f1UID(t, m, a.User); got != "65536" {
		t.Fatalf("UID của A = %s, muốn 65536", got)
	}
	if rep := execute(t, m, &SiteUserAbsent{Layout: a}); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá hẳn A: %s", rep.Job.Error)
	}
	// Site B của người khác: không nhận lại 65536 dù số đó đã trống trên máy.
	if rep := execute(t, m, &SiteUser{Layout: b, UIDs: st}); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("tạo B: %s", rep.Job.Error)
	}
	if got := f1UID(t, m, b.User); got != "65537" {
		t.Errorf("UID của B = %s, muốn 65537 (không dùng lại UID của A)", got)
	}
	// Tạo lại A cùng domain: số mới nữa.
	if rep := execute(t, m, &SiteUser{Layout: a, UIDs: st}); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("tạo lại A: %s", rep.Job.Error)
	}
	if got := f1UID(t, m, a.User); got != "65538" {
		t.Errorf("UID của A tạo lại = %s, muốn 65538", got)
	}
	if v, _, _ := st.GetSetting(ctx, store.SiteUIDKey); v != "65538" {
		t.Errorf("tpan.db giữ %q, muốn 65538", v)
	}
}

func TestSiteUserCapUID(t *testing.T) {
	ctx := context.Background()
	l := userTestLayout(t)

	t.Run("máy đã có UID, GID cao hơn trong dải thì nhảy lên trên", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userPasswd, []byte(userRoot+"khac:x:70000:100::/home/khac:/bin/bash\nnobody:x:65534:65534::/:/usr/sbin/nologin\n"), 0o644, system.Root)
		m.SetFile(userGroup, []byte("root:x:0:\nnhom:x:70005:\nngoai:x:200000:\n"), 0o644, system.Root)
		if err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := f1UID(t, m, l.User); got != "70006" {
			t.Errorf("UID = %s, muốn 70006", got)
		}
	})

	t.Run("tpan.db từng cấp số cao hơn thì đi tiếp từ đó", func(t *testing.T) {
		m := userMachine(t)
		if err := (&SiteUser{Layout: l, UIDs: &memUIDs{max: 66000}}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := f1UID(t, m, l.User); got != "66001" {
			t.Errorf("UID = %s, muốn 66001", got)
		}
	})

	t.Run("số đã có user hay nhóm theo getent (LDAP) thì nhảy tiếp", func(t *testing.T) {
		m := userMachine(t)
		m.Handle([]string{"getent", "passwd", "65536"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "ldap1:x:65536:65536::/home/ldap1:/bin/bash\n"}
		})
		m.Handle([]string{"getent", "group", "65537"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "ldapg:x:65537:\n"}
		})
		u := &memUIDs{}
		if err := (&SiteUser{Layout: l, UIDs: u}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := f1UID(t, m, l.User); got != "65538" {
			t.Errorf("UID = %s, muốn 65538", got)
		}
		if u.calls != 3 {
			t.Errorf("cấp %d lần, muốn 3 (hai số bỏ phí)", u.calls)
		}
	})

	t.Run("chạy lại Apply không cấp số mới", func(t *testing.T) {
		m := userMachine(t)
		u := &memUIDs{}
		execute(t, m, &SiteUser{Layout: l, UIDs: u})
		rep := execute(t, m, &SiteUser{Layout: l, UIDs: u})
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: %q", got)
		}
		// Apply gọi thẳng (user có sẵn đúng dạng) cũng không cấp.
		if err := (&SiteUser{Layout: l, UIDs: u}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if u.calls != 1 || u.max != 65536 {
			t.Errorf("cấp %d lần, max %d; muốn 1 lần, 65536", u.calls, u.max)
		}
	})

	t.Run("useradd lỗi thì gỡ nhóm vừa tạo, lần sau lấy số mới", func(t *testing.T) {
		m := userMachine(t)
		before := m.whole()
		u := &memUIDs{}
		m.failCmd([]string{"useradd"}, 10, "useradd: cannot lock /etc/passwd")
		wantExit(t, (&SiteUser{Layout: l, UIDs: u}).Apply(ctx, m), "UID 65536")
		if !userRanString(m, "groupdel "+l.User) {
			t.Errorf("không gỡ nhóm: %v", ranStrings(m.Ran()))
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy không về như cũ")
		}
		// Lần chạy sau (useradd chạy được): 65536 đã bỏ phí, lần này 65537.
		m2 := userMachine(t)
		if err := (&SiteUser{Layout: l, UIDs: u}).Apply(ctx, m2); err != nil {
			t.Fatal(err)
		}
		if got := f1UID(t, m2, l.User); got != "65537" {
			t.Errorf("lần sau UID %s, muốn 65537", got)
		}
	})

	t.Run("nhóm còn từ lần chạy dở thì dùng lại số của nó", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userGroup, []byte("root:x:0:\n"+l.User+":x:65540:\n"), 0o644, system.Root)
		u := &memUIDs{max: 65540}
		if err := (&SiteUser{Layout: l, UIDs: u}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := f1UID(t, m, l.User); got != "65540" {
			t.Errorf("UID = %s, muốn 65540", got)
		}
		if u.calls != 0 {
			t.Errorf("cấp số mới %d lần", u.calls)
		}
		for _, c := range ranStrings(m.Ran()) {
			if strings.HasPrefix(c, "groupadd") {
				t.Errorf("chạy lại groupadd: %s", c)
			}
		}
	})

	t.Run("nhóm cùng tên GID ngoài dải thì không đụng", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userGroup, []byte("root:x:0:\n"+l.User+":x:998:\n"), 0o644, system.Root)
		err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "không phải nhóm tpan để lại") {
			t.Errorf("Apply = %v", err)
		}
		userOnlyGetent(t, m)
	})

	t.Run("không có nơi ghi số thì lỗi, không useradd", func(t *testing.T) {
		m := userMachine(t)
		err := (&SiteUser{Layout: l}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "tpan.db") {
			t.Errorf("Apply = %v", err)
		}
		userOnlyGetent(t, m)
	})

	t.Run("hết dải thì lỗi", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userPasswd, []byte(userRoot+"cuoi:x:99999:99999::/:/usr/sbin/nologin\n"), 0o644, system.Root)
		err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "hết dải UID") {
			t.Errorf("Apply = %v", err)
		}
		userOnlyGetent(t, m)
	})
}

// ============================================================ SiteUserAbsent

// f1Purge là máy có user của site (UID 65536), crontab riêng, ba thư mục tạm
// (trừ /dev/shm), và pgrep thấy hai tiến trình ở lần hỏi đầu.
func f1Purge(t *testing.T, l site.Layout) *machine {
	t.Helper()
	m := userMachine(t)
	if err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	m.SetDir("/tmp", 0o1777|0o777, system.Root)
	m.SetDir("/var/tmp", 0o1777|0o777, system.Root)
	m.SetFile(cageCrontab(l), []byte("* * * * * /var/tmp/x\n"), 0o600, system.Owner{User: l.User, Group: "crontab"})
	m.Handle([]string{"crontab", "-r"}, func(system.Cmd) system.Result {
		_ = m.Remove(cageCrontab(l))
		return system.Result{}
	})
	var mu sync.Mutex
	alive := true
	m.Handle([]string{"pgrep"}, func(system.Cmd) system.Result {
		mu.Lock()
		defer mu.Unlock()
		if alive {
			return system.Result{Stdout: "1234\n1240\n"}
		}
		return system.Result{ExitCode: 1}
	})
	m.Handle([]string{"pkill"}, func(system.Cmd) system.Result {
		mu.Lock()
		defer mu.Unlock()
		alive = false
		return system.Result{}
	})
	m.ResetRan()
	return m
}

func TestSiteUserAbsentDonDauVet(t *testing.T) {
	ctx := context.Background()
	l := userTestLayout(t)
	u := l.User
	old := userKillWait
	userKillWait = 300 * time.Millisecond
	t.Cleanup(func() { userKillWait = old })

	t.Run("dọn đúng thứ tự, đúng argv, trước userdel", func(t *testing.T) {
		m := f1Purge(t, l)
		s := &SiteUserAbsent{Layout: l}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"getent passwd " + u,
			"crontab -r -u " + u,
			"pgrep -U 65536", "pgrep -u 65536",
			"runuser -u " + u + " -- kill -KILL -1",
			"pkill -KILL -U 65536", "pkill -KILL -u 65536",
			"pgrep -U 65536", "pgrep -u 65536",
			"find /tmp -xdev -uid 65536 '!' -type d -delete",
			"find /tmp -xdev -uid 65536 -type d -empty -delete",
			"find /var/tmp -xdev -uid 65536 '!' -type d -delete",
			"find /var/tmp -xdev -uid 65536 -type d -empty -delete",
			"userdel " + u,
		}
		var got []string
		for _, c := range ranStrings(m.Ran()) {
			if !strings.HasPrefix(c, "systemctl") { // gắn lại passwd của lồng
				got = append(got, c)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("lệnh:\n%s\nmuốn:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Errorf("Check sau Apply = %v, %v", done, err)
		}
		info := s.Info()
		if !info.Danger || info.NoUndo.IsZero() {
			t.Errorf("Danger %v, NoUndo %v", info.Danger, info.NoUndo)
		}
		for _, w := range []string{"crontab -r -u " + u, "pkill -KILL -U '<UID>'", "pkill -KILL -u '<UID>'", "find /dev/shm -xdev -uid '<UID>' '!' -type d -delete", "userdel " + u} {
			if !slices.Contains(info.Commands, w) {
				t.Errorf("Info thiếu %q: %v", w, info.Commands)
			}
		}
		if last := info.Commands[len(info.Commands)-1]; last != "userdel "+u {
			t.Errorf("userdel không đứng cuối trong Info: %q", last)
		}
	})

	t.Run("crontab -r báo không có crontab (mã 1) mà file đã mất thì đạt", func(t *testing.T) {
		m := f1Purge(t, l)
		m.Handle([]string{"crontab", "-r"}, func(system.Cmd) system.Result {
			_ = m.Remove(cageCrontab(l))
			return system.Result{ExitCode: 1, Stderr: "no crontab for " + u + "\n"}
		})
		if err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply = %v", err)
		}
	})

	t.Run("không có crontab thì không chạy crontab", func(t *testing.T) {
		m := f1Purge(t, l)
		_ = m.Remove(cageCrontab(l))
		if err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if userRanString(m, "crontab -r -u "+u) {
			t.Error("chạy crontab khi không có file")
		}
	})

	t.Run("crontab -r lỗi mà file còn thì lỗi, không userdel", func(t *testing.T) {
		m := f1Purge(t, l)
		m.failCmd([]string{"crontab", "-r"}, 1, "crontab: cannot unlink")
		err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m)
		wantExit(t, err, "gỡ crontab riêng của "+u)
		if userRanString(m, "userdel "+u) {
			t.Error("vẫn userdel")
		}
	})

	t.Run("tiến trình không chết thì lỗi sau thời hạn, không userdel", func(t *testing.T) {
		m := f1Purge(t, l)
		m.Handle([]string{"pkill"}, func(system.Cmd) system.Result { return system.Result{} })
		err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "vẫn còn tiến trình") || !strings.Contains(err.Error(), "1234 1240") {
			t.Fatalf("Apply = %v", err)
		}
		if userRanString(m, "userdel "+u) {
			t.Error("vẫn userdel")
		}
	})

	t.Run("pkill lỗi thật (mã 3) thì lỗi", func(t *testing.T) {
		m := f1Purge(t, l)
		m.failCmd([]string{"pkill"}, 3, "pkill: fatal")
		wantExit(t, (&SiteUserAbsent{Layout: l}).Apply(ctx, m), "giết tiến trình của "+u)
	})

	t.Run("find lỗi mà không còn file của UID thì đạt", func(t *testing.T) {
		m := f1Purge(t, l)
		m.failCmd([]string{"find", "/tmp", "-xdev", "-uid", "65536", "!", "-type", "d", "-delete"}, 1, "find: '/tmp/x': No such file or directory")
		if err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply = %v", err)
		}
		if !userRanString(m, "find /tmp -xdev -uid 65536 '!' -type d -print -quit") {
			t.Errorf("không hỏi lại: %v", ranStrings(m.Ran()))
		}
	})

	t.Run("find lỗi mà còn file của UID thì lỗi, không userdel", func(t *testing.T) {
		m := f1Purge(t, l)
		m.failCmd([]string{"find", "/var/tmp", "-xdev", "-uid", "65536", "!", "-type", "d", "-delete"}, 1, "find: cannot delete")
		m.Handle([]string{"find", "/var/tmp", "-xdev", "-uid", "65536", "!", "-type", "d", "-print"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "/var/tmp/bash\n"}
		})
		err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m)
		wantExit(t, err, "/var/tmp/bash")
		if userRanString(m, "userdel "+u) {
			t.Error("vẫn userdel")
		}
	})

	t.Run("UID 0 thì từ chối, không giết gì", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(userPasswd, []byte(userRoot+u+":x:0:0::"+l.Dir+":"+userShell+"\n"), 0o644, system.Root)
		err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "không phải số khác 0") {
			t.Fatalf("Apply = %v", err)
		}
		userOnlyGetent(t, m)
	})

	t.Run("crontab mồ côi của user đã xoá thì Check false, Apply xoá file", func(t *testing.T) {
		m := userMachine(t)
		m.SetFile(cageCrontab(l), []byte("* * * * * id\n"), 0o600, system.Root)
		s := &SiteUserAbsent{Layout: l}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v; muốn false", done, err)
		}
		if rep := execute(t, m, s); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("Execute: %s", rep.Job.Error)
		}
		if ok, _ := userNoCrontab(m, l); !ok {
			t.Error("file crontab còn")
		}
	})
}
