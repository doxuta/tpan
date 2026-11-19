package steps

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// machine là FakeSystem cộng các handler giả lập dpkg, systemd, a2query,
// MariaDB và curl có trạng thái. Không có các handler này thì mọi lệnh trên
// FakeSystem trả mã 0 với output rỗng, và test "máy sạch" đạt mà không kiểm gì.
type machine struct {
	*system.FakeSystem

	mu      sync.Mutex
	pkgs    map[string]bool
	units   map[string]unitState
	mods    map[string]bool
	db      dbState
	phar    []byte // nội dung curl "tải" về
	updates int
}

type unitState struct{ Active, Enabled bool }

type dbState struct {
	RootSocket bool
	Anon       int
	Test       bool
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	m := &machine{
		FakeSystem: system.NewFake(),
		pkgs:       map[string]bool{},
		units:      map[string]unitState{},
		mods:       map[string]bool{},
		phar:       fakePhar,
	}
	ok := system.Result{}
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}

	m.Handle([]string{"dpkg-query"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		var out, errs strings.Builder
		for _, p := range c.Args[2:] {
			if m.pkgs[p] {
				fmt.Fprintf(&out, "%s\t%s\n", p, dpkgInstalled)
			} else {
				fmt.Fprintf(&errs, "dpkg-query: no packages found matching %s\n", p)
			}
		}
		if errs.Len() > 0 {
			return exit(1, out.String(), errs.String())
		}
		return exit(0, out.String(), "")
	})
	m.Handle([]string{"apt-get", "update"}, func(system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.updates++
		return ok
	})
	m.Handle([]string{"apt-get", "install", "-y"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, p := range c.Args[2:] {
			m.pkgs[p] = true
		}
		return ok
	})

	m.Handle([]string{"systemctl"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		unit := c.Args[len(c.Args)-1]
		u := m.units[unit]
		switch c.Args[0] {
		case "is-active":
			if u.Active {
				return exit(0, "active\n", "")
			}
			return exit(3, "inactive\n", "")
		case "is-enabled":
			if u.Enabled {
				return exit(0, "enabled\n", "")
			}
			return exit(1, "disabled\n", "")
		case "enable":
			u.Enabled, u.Active = true, true
		case "disable":
			u.Enabled = false
		case "stop":
			u.Active = false
		default: // reload, restart: không đổi trạng thái
			return ok
		}
		m.units[unit] = u
		return ok
	})

	m.Handle([]string{"a2query", "-m"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.mods[c.Args[1]] {
			return exit(0, c.Args[1]+" (enabled by site administrator)\n", "")
		}
		return exit(32, "", "No module matches "+c.Args[1]+"\n")
	})
	setMods := func(on bool) func(system.Cmd) system.Result {
		return func(c system.Cmd) system.Result {
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, mod := range c.Args[1:] { // bỏ "-q"
				if on {
					m.mods[mod] = true
				} else {
					delete(m.mods, mod)
				}
			}
			return ok
		}
	}
	m.Handle([]string{"a2enmod"}, setMods(true))
	m.Handle([]string{"a2dismod"}, setMods(false))
	m.Handle([]string{"apachectl", "configtest"}, func(system.Cmd) system.Result { return exit(0, "", "Syntax OK\n") })

	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		sql := string(c.Stdin)
		if sql == mariadbStateSQL {
			return exit(0, fmt.Sprintf("%d\t%d\t%d\n", b2i(m.db.RootSocket), m.db.Anon, b2i(m.db.Test)), "")
		}
		if strings.Contains(sql, sqlDropAnon) {
			m.db.Anon = 0
		}
		if strings.Contains(sql, sqlDropTest) {
			m.db.Test = false
		}
		if strings.Contains(sql, sqlRootSocket) {
			m.db.RootSocket = true
		}
		return ok
	})

	m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		i := slices.Index(c.Args, "-o")
		m.mu.Lock()
		data := slices.Clone(m.phar)
		m.mu.Unlock()
		// Handler chạy khi FakeSystem đã nhả khoá nên gọi SetFile được.
		m.SetFile(c.Args[i+1], data, 0o644, system.Root)
		return ok
	})
	return m
}

// fakePhar là nội dung curl giả "tải" về.
var fakePhar = []byte("<?php // wp-cli giả\n")

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// state chụp phần trạng thái không nằm trong hệ thống file giả.
func (m *machine) state() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []any{maps.Clone(m.pkgs), maps.Clone(m.units), maps.Clone(m.mods), m.db}
}

// whole là hệ thống file giả cộng trạng thái dịch vụ, để so trước và sau Undo.
func (m *machine) whole() any { return []any{m.Snapshot(), m.state()} }

// failCmd làm cho mọi lệnh có argv bắt đầu bằng prefix trả mã thoát code.
// Handler đăng ký sau thắng khi cùng độ dài prefix (xem FakeSystem.Handle).
func (m *machine) failCmd(prefix []string, code int, stderr string) {
	m.Handle(prefix, func(system.Cmd) system.Result {
		return system.Result{ExitCode: code, Stderr: stderr}
	})
}

// readOnly: lệnh chỉ hỏi trạng thái, được phép chạy khi engine Check một bước đã xong.
func readOnly(c system.Cmd) bool {
	switch c.Name {
	case "dpkg-query", "a2query":
		return true
	case "systemctl":
		return c.Args[0] == "is-active" || c.Args[0] == "is-enabled"
	case "mariadb":
		return string(c.Stdin) == mariadbStateSQL
	}
	return false
}

func execute(t *testing.T, m *machine, steps ...engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(m.FakeSystem, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: steps})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

func ranStrings(cmds []system.Cmd) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.String()
	}
	return out
}

// lifecycle là ba trường hợp skill tpan-step đòi cho mọi Step.
type lifecycle struct {
	name  string
	setup func(m *machine) // đưa máy về "sạch" theo nghĩa của bước
	step  func() engine.Step
	// beforeRerun chuẩn bị máy cho lần chạy lại (ví dụ AptUpdate cần gói đã cài).
	beforeRerun func(m *machine)
	// noUndo: Undo của bước không làm gì (Info().NoUndo khác rỗng).
	noUndo bool
}

func (lc lifecycle) run(t *testing.T) {
	ctx := context.Background()

	t.Run("máy sạch rồi Apply thì Check true", func(t *testing.T) {
		m := newMachine(t)
		lc.setup(m)
		s := lc.step()
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
		m := newMachine(t)
		lc.setup(m)
		if rep := execute(t, m, lc.step()); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		if lc.beforeRerun != nil {
			lc.beforeRerun(m)
		}
		before := m.whole()
		m.ResetRan()
		rep := execute(t, m, lc.step())
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q, muốn skipped (%s)", got, rep.Steps[0].Error)
		}
		for _, c := range m.Ran() {
			if !readOnly(c) {
				t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})

	if lc.noUndo {
		t.Run("NoUndo có lý do", func(t *testing.T) {
			if lc.step().Info().NoUndo.IsZero() {
				t.Error("Undo không làm gì nhưng Info().NoUndo trống")
			}
		})
		return
	}
	t.Run("Apply rồi Undo thì máy về như cũ", func(t *testing.T) {
		m := newMachine(t)
		lc.setup(m)
		before := m.whole()
		s := lc.step()
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
