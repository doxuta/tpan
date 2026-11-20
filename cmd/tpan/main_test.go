package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// cli là một lần chạy CLI trên FakeSystem, với DB trong thư mục tạm.
type cli struct {
	t        *testing.T
	fake     *system.FakeSystem
	root     bool
	terminal bool
	stdin    string
	db       string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	f := system.NewFake()
	f.SetFile("/etc/os-release", []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\n"), 0o644, system.Root)
	// dpkg-query mã 1: gói chưa từng được cài (máy sạch).
	f.Handle([]string{"dpkg-query"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "dpkg-query: no packages found matching\n"}
	})
	return &cli{t: t, fake: f, root: true, db: filepath.Join(t.TempDir(), "tpan.db")}
}

func (c *cli) run(args ...string) (code int, stdout, stderr string) {
	c.t.Helper()
	var out, errb bytes.Buffer
	e := env{
		sys:        c.fake,
		isRoot:     func() bool { return c.root },
		isTerminal: func() bool { return c.terminal },
		fileExists: func(p string) bool { _, err := os.Stat(p); return err == nil },
		openStore:  store.Open,
		stdin:      strings.NewReader(c.stdin),
		stdout:     &out,
		stderr:     &errb,
	}
	code = run(context.Background(), append(args, "--db", c.db), e)
	return code, out.String(), errb.String()
}

// mutating trả các lệnh đã chạy mà không chỉ đọc.
func (c *cli) mutating() []string {
	var bad []string
	for _, cmd := range c.fake.Ran() {
		switch cmd.Name {
		case "dpkg-query", "ss", "a2query":
			continue
		case "systemctl":
			if cmd.Args[0] == "is-active" || cmd.Args[0] == "is-enabled" {
				continue
			}
		case "mariadb":
			if strings.HasPrefix(string(cmd.Stdin), "SELECT") {
				continue
			}
		}
		bad = append(bad, cmd.String())
	}
	return bad
}

func (c *cli) dbExists() bool { _, err := os.Stat(c.db); return err == nil }

func TestDryRunWritesNothing(t *testing.T) {
	c := newCLI(t)
	before := c.fake.Snapshot()
	code, out, errs := c.run("stack", "install", "--web", "nginx", "--dry-run")
	if code != 0 {
		t.Fatalf("mã thoát %d; stderr: %s", code, errs)
	}
	if !strings.Contains(out, "Kế hoạch:") || !strings.Contains(out, "apt-get update") {
		t.Errorf("output thiếu bản xem trước:\n%s", out)
	}
	if c.dbExists() {
		t.Error("--dry-run tạo file DB")
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("--dry-run chạy lệnh thay đổi máy: %v", m)
	}
	if len(c.fake.Snapshot()) != len(before) {
		t.Error("--dry-run đổi hệ thống file")
	}
}

func TestDryRunJSONAndEnglish(t *testing.T) {
	c := newCLI(t)
	code, out, errs := c.run("stack", "install", "--web", "apache", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("mã thoát %d; stderr: %s", code, errs)
	}
	var pv engine.Preview
	if err := json.Unmarshal([]byte(out), &pv); err != nil {
		t.Fatalf("stdout không phải JSON của Preview: %v\n%s", err, out)
	}
	if pv.Kind != "stack.install" || pv.Target != "apache" || len(pv.Steps) != 19 {
		t.Errorf("Preview = %s/%s, %d bước", pv.Kind, pv.Target, len(pv.Steps))
	}

	_, out, _ = c.run("stack", "install", "--web", "nginx", "--dry-run", "--lang", "en")
	if !strings.Contains(out, "Plan:") {
		t.Errorf("--lang en không in tiếng Anh:\n%s", out)
	}
	if code, _, errs := c.run("version", "--lang", "fr"); code == 0 || !strings.Contains(errs, "--lang") {
		t.Errorf("--lang fr: mã %d, stderr %q", code, errs)
	}
}

func TestDryRunReadsExistingDB(t *testing.T) {
	c := newCLI(t)
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(context.Background(), core.SettingWeb, "nginx"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	code, _, errs := c.run("stack", "install", "--web", "apache", "--dry-run")
	if code != 1 || !strings.Contains(errs, "tpan stack switch") {
		t.Errorf("DB đã ghi nginx, xin apache: mã %d, stderr %q", code, errs)
	}
}

func TestNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"stack", "install", "--web", "nginx"},
		{"stack", "install", "--web", "nginx", "--dry-run"},
		{"stack", "status"},
		{"health"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") {
			t.Errorf("%v không root: mã %d, stderr %q", args, code, errs)
		}
		if len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%v không root mà vẫn chạy lệnh hoặc tạo DB", args)
		}
	}
}

func TestBadWeb(t *testing.T) {
	c := newCLI(t)
	code, _, errs := c.run("stack", "install", "--web", "caddy; id")
	if code != 1 || !strings.Contains(errs, "nginx hoặc apache") || strings.Contains(errs, "goroutine") {
		t.Errorf("mã %d, stderr %q", code, errs)
	}
	if len(c.fake.Ran()) != 0 {
		t.Error("web sai mà vẫn chạy lệnh")
	}
	if code, _, _ := c.run("stack", "install"); code == 0 {
		t.Error("thiếu --web mà mã 0")
	}
}

func TestPreconditionRefusedJSON(t *testing.T) {
	c := newCLI(t)
	c.fake.SetFile("/etc/os-release", []byte("ID=debian\nVERSION_ID=\"12\"\n"), 0o644, system.Root)
	code, out, _ := c.run("stack", "install", "--web", "nginx", "--yes", "--json")
	if code != 1 {
		t.Fatalf("mã %d", code)
	}
	var got struct {
		Error core.RefusedError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout không phải JSON: %v\n%s", err, out)
	}
	if got.Error.Code != "precondition_failed" || len(got.Error.Checks) == 0 || len(got.Error.Checks[0].ManualFix) == 0 {
		t.Errorf("error = %+v", got.Error)
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("bị từ chối mà vẫn chạy lệnh thay đổi: %v", m)
	}

	// Bản chữ: in Summary và ManualFix của điều kiện hỏng.
	_, _, errs := c.run("stack", "install", "--web", "nginx", "--yes")
	if !strings.Contains(errs, "debian 12") || !strings.Contains(errs, "cat /etc/os-release") {
		t.Errorf("stderr thiếu Summary hoặc ManualFix:\n%s", errs)
	}
}

func TestConfirm(t *testing.T) {
	t.Run("không có terminal, không --yes", func(t *testing.T) {
		c := newCLI(t)
		code, _, errs := c.run("stack", "install", "--web", "nginx")
		if code != 1 || !strings.Contains(errs, "--yes") {
			t.Errorf("mã %d, stderr %q", code, errs)
		}
		if m := c.mutating(); len(m) > 0 {
			t.Errorf("chạy lệnh thay đổi: %v", m)
		}
	})
	t.Run("trả lời n", func(t *testing.T) {
		c := newCLI(t)
		c.terminal, c.stdin = true, "n\n"
		code, out, _ := c.run("stack", "install", "--web", "nginx")
		if code != 1 || !strings.Contains(out, "Chạy? [y/N]") || !strings.Contains(out, "Đã huỷ") {
			t.Errorf("mã %d, stdout:\n%s", code, out)
		}
		if m := c.mutating(); len(m) > 0 {
			t.Errorf("chạy lệnh thay đổi: %v", m)
		}
	})
}

func TestInstallFailurePrintsOutput(t *testing.T) {
	c := newCLI(t)
	c.fake.Handle([]string{"apt-get", "update"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 100, Stdout: "Hit:1 http://archive.ubuntu.com noble InRelease\n", Stderr: "E: giả lập lỗi mạng\n"}
	})
	code, out, errs := c.run("stack", "install", "--web", "nginx", "--yes")
	if code != 1 {
		t.Fatalf("job hỏng mà mã %d; stderr %s", code, errs)
	}
	for _, want := range []string{"Bắt đầu job", "LỖI:", "| E: giả lập lỗi mạng", "Job "} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout thiếu %q:\n%s", want, out)
		}
	}
	// stack.web đã ghi trước khi chạy; php_version thì không.
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if v, _, _ := st.GetSetting(context.Background(), core.SettingWeb); v != "nginx" {
		t.Errorf("stack.web = %q", v)
	}
	if jobs, _ := st.Jobs(context.Background(), 5); len(jobs) != 1 || jobs[0].Status == engine.JobSucceeded {
		t.Errorf("jobs = %+v", jobs)
	}
}

func TestStatusAndHealthNotInstalled(t *testing.T) {
	c := newCLI(t)
	code, out, errs := c.run("stack", "status", "--json")
	if code != 0 {
		t.Fatalf("status: mã %d, %s", code, errs)
	}
	var s core.Status
	if err := json.Unmarshal([]byte(out), &s); err != nil || s.Installed {
		t.Errorf("status JSON = %+v, %v", s, err)
	}
	code, out, _ = c.run("health")
	if code != 1 || !strings.Contains(out, "chưa cài") {
		t.Errorf("health khi chưa cài: mã %d, stdout %q", code, out)
	}
	if len(c.fake.Ran()) != 0 {
		t.Errorf("chưa cài mà vẫn chạy lệnh: %v", c.fake.Ran())
	}
}

func TestVersion(t *testing.T) {
	c := newCLI(t)
	c.root = false // version không cần root
	if code, out, _ := c.run("version"); code != 0 || strings.TrimSpace(out) == "" {
		t.Errorf("version: mã %d, %q", code, out)
	}
}
