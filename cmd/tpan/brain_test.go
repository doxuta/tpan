package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/ai/claudecode"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/system"
)

// brainRan trả tên các chương trình đã chạy qua FakeSystem.
func (c *cli) brainRan() []string {
	var names []string
	for _, cmd := range c.fake.Ran() {
		names = append(names, cmd.Name)
	}
	return names
}

func TestBrainLinkTuChoiKhiTat(t *testing.T) {
	c := newCLI(t)
	code, out, errs := c.run("brain-link")
	if code != 1 || !strings.Contains(errs, "sudo tpan mcp config --brain bridge") {
		t.Errorf("ai.brain off: mã %d, stderr %q", code, errs)
	}
	// stdout chở giao thức: lời từ chối không được lọt ra đó.
	if out != "" {
		t.Errorf("lời từ chối ra stdout: %q", out)
	}
}

func TestBrainLinkCanRoot(t *testing.T) {
	c := newCLI(t)
	c.root = false
	code, out, errs := c.run("brain-link")
	if code != 1 || !strings.Contains(errs, "sudo") || out != "" || c.dbExists() {
		t.Errorf("không root: mã %d, stdout %q, stderr %q, mở DB %v", code, out, errs, c.dbExists())
	}
}

func TestBrainLinkKhongCoSocket(t *testing.T) {
	// Đã bật bridge mà tpan serve chưa mở socket (máy test không có
	// /run/tpan/brain.sock): lỗi ra stderr, stdout vẫn sạch.
	c := newCLI(t)
	if code, _, errs := c.run("mcp", "config", "--brain", "bridge"); code != 0 {
		t.Fatalf("bật bridge: %d %s", code, errs)
	}
	code, out, errs := c.run("brain-link")
	if code != 1 || !strings.Contains(errs, "brain.sock") || out != "" {
		t.Errorf("không có socket: mã %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestBrainTuChoiDauVao(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"brain"}, "cần đúng một máy chủ"},
		{[]string{"brain", "a.example", "b.example"}, "cần đúng một máy chủ"},
		{[]string{"brain", "host;id"}, "không hợp lệ"},
		{[]string{"brain", "a b"}, "không hợp lệ"},
		{[]string{"brain", "$(id)"}, "không hợp lệ"},
		{[]string{"brain", "host\nProxyCommand=id"}, "không hợp lệ"},
		{[]string{"brain", strings.Repeat("a", 300)}, "không hợp lệ"},
		{[]string{"brain", "ubuntu@example.com", "--model=-x"}, "--model"},
		{[]string{"brain", "ubuntu@example.com", "--model", "sonnet --tools Bash"}, "--model"},
	} {
		c := newCLI(t)
		c.root = false // tpan brain chạy bằng user thường
		code, out, errs := c.run(tc.args...)
		if code != 1 || !strings.Contains(errs, tc.want) {
			t.Errorf("%q: mã %d, stdout %q, stderr %q", tc.args, code, out, errs)
		}
		if r := c.brainRan(); len(r) != 0 {
			t.Errorf("%q: từ chối mà vẫn chạy %v", tc.args, r)
		}
	}
}

func TestSSHTargetRe(t *testing.T) {
	for s, ok := range map[string]bool{
		"ubuntu@example.com": true,
		"tpan-test":          true,
		"root@10.0.0.1":      true,
		"-oProxyCommand=id":  false,
		"":                   false,
		"a/b":                false,
	} {
		if sshTargetRe.MatchString(s) != ok {
			t.Errorf("sshTargetRe(%q) = %v", s, !ok)
		}
	}
}

func TestBrainKhongCoClaude(t *testing.T) {
	c := newCLI(t)
	c.root = false
	// Không handler: claude --version không in gì, như khi lệnh lỗi lạ.
	code, _, errs := c.run("brain", "ubuntu@example.com")
	if code != 1 || !strings.Contains(errs, "Cài Claude Code") {
		t.Errorf("không có claude: mã %d, stderr %q", code, errs)
	}
}

func TestBrainChuaDangNhap(t *testing.T) {
	c := newCLI(t)
	c.root = false
	c.fake.Handle([]string{"claude", "--version"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "9.9.9 (Claude Code)\n"}
	})
	c.fake.Handle([]string{"claude", "auth", "status"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stdout: `{"loggedIn":false,"authMethod":"none","email":"someone@example.com"}`}
	})
	code, out, errs := c.run("brain", "ubuntu@example.com")
	if code != 1 || !strings.Contains(errs, "claude auth login") {
		t.Errorf("chưa đăng nhập: mã %d, stderr %q", code, errs)
	}
	if !strings.Contains(errs, "CẢNH BÁO") || !strings.Contains(errs, claudecode.TestedVersion) {
		t.Errorf("bản claude khác bản đã kiểm mà không cảnh báo: %q", errs)
	}
	// Chỉ đọc loggedIn, authMethod: email không bao giờ được in.
	if strings.Contains(out+errs, "someone@example.com") {
		t.Error("in ra email của tài khoản Claude")
	}
	for _, cmd := range c.fake.Ran() {
		if cmd.Name == "ssh" {
			t.Error("chưa đăng nhập mà vẫn mở ssh")
		}
		// claude chạy với HOME, PATH... của người vận hành, không gì khác.
		for _, kv := range cmd.Env {
			k, _, _ := strings.Cut(kv, "=")
			if !slices.Contains(brainEnvKeys, k) {
				t.Errorf("claude nhận biến %s ngoài danh sách", k)
			}
		}
	}
}

func TestSSHArgs(t *testing.T) {
	got := strings.Join(sshArgs([]string{"-oPort=2222"}, "ubuntu@example.com", "sudo", "-n", "tpan", "brain-link"), " ")
	want := "-oPort=2222 -o BatchMode=yes ubuntu@example.com sudo -n tpan brain-link"
	if got != want {
		t.Errorf("sshArgs = %q, muốn %q", got, want)
	}
}

func TestMCPConfigBrain(t *testing.T) {
	c := newCLI(t)
	code, out, _ := c.run("mcp", "config")
	if code != 0 || !strings.Contains(out, "Bộ não:") || !strings.Contains(out, core.BrainOff) {
		t.Errorf("mặc định: mã %d\n%s", code, out)
	}
	code, out, errs := c.run("mcp", "config", "--brain", "bridge", "--json")
	if code != 0 || !strings.Contains(out, `"brain": "bridge"`) {
		t.Errorf("--brain bridge: mã %d, %q, %q", code, out, errs)
	}
	if v := c.setting(core.SettingAIBrain); v != core.BrainBridge {
		t.Errorf("ai.brain = %q", v)
	}
	if n := c.audits("mcp.config"); n != 1 {
		t.Errorf("audit mcp.config: %d dòng", n)
	}
	if code, _, _ := c.run("mcp", "config", "--brain", "off"); code != 0 || c.setting(core.SettingAIBrain) != core.BrainOff {
		t.Errorf("--brain off: mã %d, ai.brain = %q", code, c.setting(core.SettingAIBrain))
	}
}
