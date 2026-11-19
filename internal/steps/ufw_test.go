package steps

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// ufwSim là FakeSystem cộng một ufw giả có trạng thái: danh sách luật đã thêm
// và cờ đang bật. Output giống ufw 0.36 trên Ubuntu 24.04.
type ufwSim struct {
	*system.FakeSystem

	mu     sync.Mutex
	rules  []string
	active bool
}

func ufwNewSim() *ufwSim {
	m := &ufwSim{FakeSystem: system.NewFake()}
	m.Handle([]string{"ufw"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		a := c.Args
		switch {
		case slices.Equal(a, []string{"show", "added"}):
			var b strings.Builder
			b.WriteString("Added user rules (see 'ufw status' for running firewall):\n")
			if len(m.rules) == 0 {
				b.WriteString("(None)\n")
			}
			for _, r := range m.rules {
				b.WriteString("ufw allow " + r + "\n")
			}
			return system.Result{Stdout: b.String()}
		case slices.Equal(a, []string{"status"}):
			if m.active {
				return system.Result{Stdout: "Status: active\n\nTo                         Action      From\n--                         ------      ----\n"}
			}
			return system.Result{Stdout: "Status: inactive\n"}
		case len(a) == 2 && a[0] == "allow":
			if !slices.Contains(m.rules, a[1]) {
				m.rules = append(m.rules, a[1])
			}
			return system.Result{Stdout: "Rule added\nRule added (v6)\n"}
		case len(a) == 3 && a[0] == "delete" && a[1] == "allow":
			m.rules = slices.DeleteFunc(m.rules, func(r string) bool { return r == a[2] })
			return system.Result{Stdout: "Rule deleted\nRule deleted (v6)\n"}
		case slices.Equal(a, []string{"--force", "enable"}):
			m.active = true
			return system.Result{Stdout: "Firewall is active and enabled on system startup\n"}
		case slices.Equal(a, []string{"disable"}):
			m.active = false
			return system.Result{Stdout: "Firewall stopped and disabled on system startup\n"}
		}
		return system.Result{ExitCode: 1, Stderr: "ERROR: lệnh ufw lạ " + strings.Join(a, " ")}
	})
	return m
}

func (m *ufwSim) whole() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []any{m.Snapshot(), strings.Join(m.rules, ","), m.active}
}

func (m *ufwSim) fail(prefix []string, stderr string) {
	m.Handle(prefix, func(system.Cmd) system.Result { return system.Result{ExitCode: 1, Stderr: stderr} })
}

// ufwReadOnly: lệnh chỉ hỏi trạng thái, được phép chạy khi Check một bước đã xong.
func ufwReadOnly(c system.Cmd) bool {
	return c.Name == "ufw" && (slices.Equal(c.Args, []string{"show", "added"}) || slices.Equal(c.Args, []string{"status"}))
}

func ufwExecute(t *testing.T, m *ufwSim, s engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(m.FakeSystem, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: []engine.Step{s}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

// ufwLifecycle là ba trường hợp của skill tpan-step (lệnh lỗi ở test riêng).
// Không dùng lifecycle chung: readOnly và whole của nó không biết ufw.
func ufwLifecycle(t *testing.T, setup func(m *ufwSim), step func() engine.Step) {
	ctx := context.Background()
	prep := func() *ufwSim {
		m := ufwNewSim()
		setup(m)
		return m
	}
	t.Run("máy sạch rồi Apply thì Check true", func(t *testing.T) {
		m := prep()
		s := step()
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v; muốn false, nil", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
		}
	})
	t.Run("chạy lại thì không lệnh thay đổi nào chạy", func(t *testing.T) {
		m := prep()
		if rep := ufwExecute(t, m, step()); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := m.whole()
		m.ResetRan()
		rep := ufwExecute(t, m, step())
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q, muốn skipped (%s)", got, rep.Steps[0].Error)
		}
		for _, c := range m.Ran() {
			if !ufwReadOnly(c) {
				t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})
	t.Run("Apply rồi Undo thì máy về như cũ", func(t *testing.T) {
		m := prep()
		before := m.whole()
		s := step()
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if reflect.DeepEqual(before, m.whole()) {
			t.Fatal("Apply không đổi gì; test không kiểm được Undo")
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("sau Undo máy khác trước Apply:\ntrước %v\nsau   %v", before, m.whole())
		}
	})
}

func TestUFWLifecycle(t *testing.T) {
	rules := []string{"22/tcp", "80/tcp", "443/tcp", "8443/tcp"}
	t.Run("UFWAllow máy chưa có luật", func(t *testing.T) {
		ufwLifecycle(t, func(*ufwSim) {}, func() engine.Step { return &UFWAllow{Rules: slices.Clone(rules)} })
	})
	// Luật có sẵn từ trước: Undo chỉ gỡ luật bước này thêm.
	t.Run("UFWAllow đã có 22/tcp", func(t *testing.T) {
		ufwLifecycle(t, func(m *ufwSim) { m.rules = []string{"22/tcp"} },
			func() engine.Step { return &UFWAllow{Rules: slices.Clone(rules)} })
	})
	t.Run("UFWEnable", func(t *testing.T) {
		ufwLifecycle(t, func(m *ufwSim) { m.rules = []string{"22/tcp"} }, func() engine.Step { return &UFWEnable{} })
	})
}

func TestUFWAllowCommands(t *testing.T) {
	ctx := context.Background()
	m := ufwNewSim()
	m.rules = []string{"22/tcp"}
	s := &UFWAllow{Rules: []string{"22/tcp", "443/tcp"}}
	if err := s.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	want := []string{"ufw show added", "ufw allow 443/tcp"}
	if got := ranStrings(m.Ran()); !reflect.DeepEqual(got, want) {
		t.Errorf("lệnh = %q, muốn %q", got, want)
	}
	m.ResetRan()
	if err := s.Undo(ctx, m); err != nil {
		t.Fatal(err)
	}
	if got := ranStrings(m.Ran()); !reflect.DeepEqual(got, []string{"ufw delete allow 443/tcp"}) {
		t.Errorf("Undo chạy %q, muốn chỉ gỡ 443/tcp", got)
	}
}

// Luật có chú thích của người quản trị vẫn được tính là đã có.
func TestUFWAllowCommentedRule(t *testing.T) {
	ctx := context.Background()
	m := ufwNewSim()
	m.Handle([]string{"ufw", "show", "added"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "Added user rules (see 'ufw status' for running firewall):\nufw allow 22/tcp comment 'ssh'\n"}
	})
	if done, err := (&UFWAllow{Rules: []string{"22/tcp"}}).Check(ctx, m); err != nil || !done {
		t.Errorf("Check = %v, %v; muốn true", done, err)
	}
}

// ufw báo luật đã có (không thêm gì) thì Undo không được gỡ nó: có thể là
// luật SSH của người quản trị.
func TestUFWAllowSkippedRuleNotUndone(t *testing.T) {
	ctx := context.Background()
	m := ufwNewSim()
	m.Handle([]string{"ufw", "allow", "22/tcp"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "Skipping adding existing rule\nSkipping adding existing rule (v6)\n"}
	})
	s := &UFWAllow{Rules: []string{"22/tcp"}}
	if err := s.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	m.ResetRan()
	if err := s.Undo(ctx, m); err != nil {
		t.Fatal(err)
	}
	if len(m.Ran()) > 0 {
		t.Errorf("Undo gỡ luật bước này không thêm: %q", ranStrings(m.Ran()))
	}
}

func TestUFWErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("allow lỗi giữa chừng thì gỡ luật đã thêm", func(t *testing.T) {
		m := ufwNewSim()
		m.fail([]string{"ufw", "allow", "443/tcp"}, "ERROR: Could not update running firewall")
		before := m.whole()
		err := (&UFWAllow{Rules: []string{"22/tcp", "80/tcp", "443/tcp"}}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "mở 443/tcp trên ufw") || !strings.Contains(err.Error(), "Could not update") {
			t.Fatalf("lỗi = %v; muốn có ngữ cảnh và stderr", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("Apply lỗi mà luật vẫn còn: %v", m.rules)
		}
	})

	t.Run("show added lỗi", func(t *testing.T) {
		m := ufwNewSim()
		m.fail([]string{"ufw", "show"}, "ERROR: You need to be root to run this script")
		_, err := (&UFWAllow{Rules: []string{"22/tcp"}}).Check(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đọc luật ufw đã thêm") {
			t.Errorf("lỗi = %v", err)
		}
	})

	t.Run("status lỗi", func(t *testing.T) {
		m := ufwNewSim()
		m.fail([]string{"ufw", "status"}, "ERROR: problem running")
		if _, err := (&UFWEnable{}).Check(ctx, m); err == nil || !strings.Contains(err.Error(), "đọc trạng thái ufw") {
			t.Errorf("lỗi = %v", err)
		}
	})

	t.Run("enable lỗi", func(t *testing.T) {
		m := ufwNewSim()
		m.fail([]string{"ufw", "--force", "enable"}, "ERROR: problem running ufw-init")
		err := (&UFWEnable{}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "bật ufw") || !strings.Contains(err.Error(), "ufw-init") {
			t.Errorf("lỗi = %v", err)
		}
		if m.active {
			t.Error("enable lỗi mà ufw vẫn bật")
		}
	})

	t.Run("Undo khi chưa Apply", func(t *testing.T) {
		m := ufwNewSim()
		for _, s := range []engine.Step{&UFWAllow{Rules: []string{"22/tcp"}}, &UFWEnable{}} {
			if err := s.Undo(ctx, m); !errors.Is(err, errNotApplied) {
				t.Errorf("%T Undo = %v, muốn errNotApplied", s, err)
			}
		}
		if len(m.Ran()) > 0 {
			t.Errorf("Undo khi chưa Apply chạy lệnh: %q", ranStrings(m.Ran()))
		}
	})
}

func TestUFWBadRules(t *testing.T) {
	ctx := context.Background()
	bad := [][]string{
		nil,
		{""},
		{"22"},
		{"22/udp"},
		{"0/tcp"},
		{"65536/tcp"},
		{"99999/tcp"},
		{"022/tcp"},
		{"-22/tcp"},
		{"22/tcp; rm -rf /"},
		{"$(id)/tcp"},
		{"22/tcp\n"},
		{"22/tcp\nufw disable"},
		{"../../etc/passwd"},
		{strings.Repeat("1", 300) + "/tcp"},
		{"２２/tcp"},
		{"22/tcp", "22/tcp"},
		{"22/tcp", "--dry-run"},
	}
	for _, rules := range bad {
		m := ufwNewSim()
		s := &UFWAllow{Rules: rules}
		if _, err := s.Check(ctx, m); err == nil {
			t.Errorf("Check(%q) không lỗi", rules)
		}
		if err := s.Apply(ctx, m); err == nil {
			t.Errorf("Apply(%q) không lỗi", rules)
		}
		if len(m.Ran()) > 0 {
			t.Errorf("luật %q sai mà vẫn chạy lệnh: %q", rules, ranStrings(m.Ran()))
		}
	}
	for _, ok := range []string{"1/tcp", "22/tcp", "65535/tcp"} {
		if err := (&UFWAllow{Rules: []string{ok}}).validate(); err != nil {
			t.Errorf("%q bị từ chối: %v", ok, err)
		}
	}
}

func TestUFWInfo(t *testing.T) {
	a := (&UFWAllow{Rules: []string{"22/tcp", "443/tcp"}}).Info()
	if a.Danger || a.Title.IsZero() || a.Why.IsZero() || !reflect.DeepEqual(a.Commands, []string{"ufw allow 22/tcp", "ufw allow 443/tcp"}) {
		t.Errorf("UFWAllow.Info = %+v", a)
	}
	e := (&UFWEnable{}).Info()
	if !e.Danger || e.Title.IsZero() || e.Why.IsZero() || !reflect.DeepEqual(e.Commands, []string{"ufw --force enable"}) {
		t.Errorf("UFWEnable.Info = %+v", e)
	}
}
