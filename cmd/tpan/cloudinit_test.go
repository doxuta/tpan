package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/cloudinit"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/system"
)

// ciSumsFile ghi một SHA256SUMS hợp lệ vào thư mục tạm.
func ciSumsFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "SHA256SUMS")
	body := strings.Repeat("a", 64) + "  tpan-linux-amd64\n" + strings.Repeat("b", 64) + "  tpan-linux-arm64\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ciPasswordFromStderr lấy mật khẩu ở dòng "<nhãn>: <mật khẩu>".
func ciPasswordFromStderr(t *testing.T, stderr string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("stderr phải đúng một dòng, có %d: %q", len(lines), stderr)
	}
	i := strings.LastIndex(lines[0], ": ")
	if i < 0 {
		t.Fatalf("stderr không có dạng <nhãn>: <mật khẩu>: %q", stderr)
	}
	pw := lines[0][i+2:]
	if len(pw) != 32 {
		t.Fatalf("mật khẩu phải 32 ký tự: %q", pw)
	}
	return pw
}

// render --out: không cần root, file 0600 (kể cả khi file đã có với quyền
// rộng hơn), mật khẩu xuất hiện đúng một lần, ở stderr.
func TestCloudInitRenderOut(t *testing.T) {
	c := newCLI(t)
	c.root = false
	out := filepath.Join(t.TempDir(), "user-data.yaml")
	if err := os.WriteFile(out, []byte("cũ"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := c.run("cloud-init", "render", "--web", "nginx",
		"--base", "https://example.com/tpan", "--sums", ciSumsFile(t), "--out", out)
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("có --out thì stdout phải trống: %q", stdout)
	}
	pw := ciPasswordFromStderr(t, stderr)
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("quyền của --out = %o, muốn 600", fi.Mode().Perm())
	}
	ud, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(ud), "#cloud-config\n") {
		t.Errorf("--out không phải user-data:\n%s", ud)
	}
	if n := strings.Count(stdout+stderr+string(ud), pw); n != 1 {
		t.Errorf("mật khẩu xuất hiện %d lần, muốn đúng 1 (ở stderr)", n)
	}
	if ran := c.fake.Ran(); len(ran) != 0 {
		t.Errorf("có --sums thì không chạy lệnh nào: %v", ran)
	}
	if c.dbExists() {
		t.Error("render không được tạo DB")
	}

	// Không --out: user-data ra stdout, mật khẩu vẫn chỉ ở stderr. --lang en
	// đổi nhãn.
	code, stdout, stderr = c.run("cloud-init", "render", "--web", "apache",
		"--base", "https://example.com/tpan", "--sums", ciSumsFile(t), "--lang", "en")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, stderr)
	}
	pw = ciPasswordFromStderr(t, stderr)
	if !strings.HasPrefix(stderr, "Admin password") {
		t.Errorf("--lang en phải in nhãn tiếng Anh: %q", stderr)
	}
	if !strings.HasPrefix(stdout, "#cloud-config\n") || strings.Contains(stdout, pw) {
		t.Errorf("stdout phải là user-data không có mật khẩu:\n%s", stdout)
	}
}

func TestCloudInitRenderJSON(t *testing.T) {
	c := newCLI(t)
	c.root = false
	code, stdout, stderr := c.run("cloud-init", "render", "--web", "nginx",
		"--base", "https://example.com/tpan", "--sums", ciSumsFile(t), "--json")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, stderr)
	}
	var got struct {
		UserData      string `json:"user_data"`
		Bytes         int    `json:"bytes"`
		AdminPassword string `json:"admin_password"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout không phải JSON: %v\n%s", err, stdout)
	}
	if len(got.AdminPassword) != 32 || got.Bytes != len(got.UserData) || !strings.HasPrefix(got.UserData, "#cloud-config\n") {
		t.Errorf("JSON = %+v", got)
	}
	if strings.Count(stdout+stderr, got.AdminPassword) != 1 {
		t.Error("--json: mật khẩu phải xuất hiện đúng một lần (trường admin_password)")
	}

	// Lời từ chối ra JSON: http:// không --sums.
	code, stdout, _ = c.run("cloud-init", "render", "--web", "nginx", "--base", "http://example.com/tpan", "--json")
	if code != 1 || !strings.Contains(stdout, `"code": "`+core.CodeCloudInitInvalid+`"`) {
		t.Errorf("mã %d, stdout %s", code, stdout)
	}
	if ran := c.fake.Ran(); len(ran) != 0 {
		t.Errorf("http:// không được tải gì: %v", ran)
	}
}

func TestCloudInitStatus(t *testing.T) {
	c := newCLI(t)
	code, _, stderr := c.run("cloud-init", "status")
	if code != 1 || !strings.Contains(stderr, cloudinit.StatePath) {
		t.Errorf("chưa có file trạng thái: mã %d, stderr %q", code, stderr)
	}

	start := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	st := cloudinit.State{Started: start, Steps: []cloudinit.StepState{
		{Name: "setup", Status: "done", JobID: "abcd1234abcd1234", Seconds: 3.2},
		{Name: "stack", Status: "failed", Seconds: 61, Error: "apt-get lỗi"},
	}}
	b, _ := json.Marshal(st)
	c.fake.SetFile(cloudinit.StatePath, b, 0o600, system.Root)
	code, stdout, stderr := c.run("cloud-init", "status")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, stderr)
	}
	for _, s := range []string{"LỖI", "setup", "abcd1234abcd1234", "stack", "apt-get lỗi", "2026-11-21T08:00:00Z"} {
		if !strings.Contains(stdout, s) {
			t.Errorf("status thiếu %q:\n%s", s, stdout)
		}
	}
	code, stdout, _ = c.run("cloud-init", "status", "--json")
	var back cloudinit.State
	if code != 0 || json.Unmarshal([]byte(stdout), &back) != nil || len(back.Steps) != 2 || back.Steps[1].Error != "apt-get lỗi" {
		t.Errorf("--json: mã %d, %s", code, stdout)
	}
}

func TestCloudInitRunKhongConfig(t *testing.T) {
	c := newCLI(t)
	code, _, stderr := c.run("cloud-init", "run")
	if code != 1 || !strings.Contains(stderr, cloudinit.ConfigPath) {
		t.Errorf("mã %d, stderr %q", code, stderr)
	}
	_, err := loadFirstBootConfig(&env{sys: c.fake})
	var re *core.RefusedError
	if !errors.As(err, &re) || re.Code != core.CodeCloudInitNoConfig {
		t.Errorf("muốn %s, nhận %v", core.CodeCloudInitNoConfig, err)
	}
	if _, err := c.fake.ReadFile(cloudinit.StatePath); err == nil {
		t.Error("không có config thì không được tạo file trạng thái")
	}
	if len(c.fake.Ran()) != 0 || c.dbExists() {
		t.Errorf("không có config thì không làm gì: %v", c.fake.Ran())
	}

	// Config sai khuôn: cloudinit_invalid.
	c.fake.SetFile(cloudinit.ConfigPath, []byte(`{"version":1,"web":"caddy","private":false,"admin_hash":""}`), 0o600, system.Root)
	if _, err := loadFirstBootConfig(&env{sys: c.fake}); !errors.As(err, &re) || re.Code != core.CodeCloudInitInvalid {
		t.Errorf("muốn %s, nhận %v", core.CodeCloudInitInvalid, err)
	}

	// Cần root.
	c.root = false
	if code, _, stderr := c.run("cloud-init", "run"); code != 1 || !strings.Contains(stderr, "sudo") {
		t.Errorf("không root: mã %d, stderr %q", code, stderr)
	}
}

// Việc đầu lỗi: file trạng thái đã có (tạo trước việc đầu tiên), ghi việc lỗi
// kèm câu lỗi, ok=false, thoát 1, log có dòng có giờ; các việc sau không chạy.
func TestCloudInitRunViecLoiThiDung(t *testing.T) {
	c := newSetupCLI(t)
	c.fake.SetFile(cloudinit.ConfigPath, []byte(`{"version":1,"web":"nginx","private":false,"admin_hash":""}`), 0o600, system.Root)
	c.fake.Fail("mkdir", "/etc/tpan/tls", errors.New("đĩa đầy"))

	code, stdout, stderr := c.run("cloud-init", "run")
	if code != 1 {
		t.Fatalf("mã %d, stdout %s, stderr %s", code, stdout, stderr)
	}
	st, found, err := readFirstBootState(&env{sys: c.fake})
	if err != nil || !found {
		t.Fatalf("phải có file trạng thái: %v", err)
	}
	if st.OK || st.Finished.IsZero() || len(st.Steps) != 1 || st.Steps[0].Name != "setup" || st.Steps[0].Status != "failed" || st.Steps[0].Error == "" {
		t.Errorf("trạng thái = %+v", st)
	}
	fi, _ := c.fake.Stat(cloudinit.StatePath)
	if fi.Mode.Perm() != 0o600 {
		t.Errorf("quyền file trạng thái = %o", fi.Mode.Perm())
	}
	log, err := c.fake.ReadFile(cloudinit.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), " setup failed ") || !strings.Contains(stdout, " setup failed ") {
		t.Errorf("log và stdout phải có dòng của việc setup:\nlog:\n%s\nstdout:\n%s", log, stdout)
	}
	for _, l := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		if _, err := time.Parse(time.RFC3339, strings.Fields(l)[0]); err != nil {
			t.Errorf("dòng log phải bắt đầu bằng giờ: %q", l)
		}
	}
	if !strings.Contains(stderr, "setup") {
		t.Errorf("stderr phải nói việc nào lỗi: %q", stderr)
	}
}

// R14: lần trước chưa xong (ok=false) thì việc đã đạt không chạy lại khi không
// có --force; có --force thì chạy lại từ đầu. Việc setup bị cài lỗi: chạy lại
// là thấy "setup failed".
func TestCloudInitRunLamTiepViecChuaDat(t *testing.T) {
	c := newSetupCLI(t)
	c.fake.SetFile(cloudinit.ConfigPath, []byte(`{"version":1,"web":"nginx","private":false,"admin_hash":""}`), 0o600, system.Root)
	c.fake.Fail("mkdir", "/etc/tpan/tls", errors.New("đĩa đầy"))
	t0 := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	prev, _ := json.Marshal(cloudinit.State{Started: t0, Finished: t0.Add(time.Minute), OK: false, Steps: []cloudinit.StepState{
		{Name: "setup", Status: "done", Seconds: 12},
		{Name: "admin", Status: "failed", Error: "x"},
	}})
	c.fake.SetFile(cloudinit.StatePath, prev, 0o600, system.Root)

	_, stdout, _ := c.run("cloud-init", "run")
	st, _, err := readFirstBootState(&env{sys: c.fake})
	if err != nil || len(st.Steps) == 0 || st.Steps[0].Name != "setup" || st.Steps[0].Status != "done" || st.Steps[0].Seconds != 12 {
		t.Fatalf("việc setup đã đạt phải giữ nguyên, không chạy lại: %+v (%v)\n%s", st.Steps, err, stdout)
	}
	if strings.Contains(stdout, "setup failed") || !strings.Contains(stdout, "setup skipped 0.0s (đã đạt") {
		t.Errorf("stdout phải báo bỏ qua setup:\n%s", stdout)
	}

	code, stdout, _ := c.run("cloud-init", "run", "--force")
	st, _, _ = readFirstBootState(&env{sys: c.fake})
	if code != 1 || len(st.Steps) != 1 || st.Steps[0].Status != "failed" || !strings.Contains(stdout, "setup failed") {
		t.Errorf("--force phải chạy lại setup (và lỗi): mã %d, %+v\n%s", code, st.Steps, stdout)
	}
}

// S13: trạng thái ok mà không --force thì thoát 0, không chạy lệnh nào, không
// ghi gì (cả file trạng thái lẫn DB).
func TestCloudInitRunDaXong(t *testing.T) {
	c := newCLI(t)
	c.fake.SetFile(cloudinit.ConfigPath, []byte(`{"version":1,"web":"nginx","private":false,"admin_hash":""}`), 0o600, system.Root)
	fin := time.Date(2026, 11, 21, 8, 5, 0, 0, time.UTC)
	state, _ := json.Marshal(cloudinit.State{Started: fin.Add(-5 * time.Minute), Finished: fin, OK: true, Steps: []cloudinit.StepState{}})
	c.fake.SetFile(cloudinit.StatePath, state, 0o600, system.Root)
	before := c.fake.Snapshot()

	code, stdout, stderr := c.run("cloud-init", "run")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, stderr)
	}
	if !strings.Contains(stdout, "đã xong lúc 2026-11-21T08:05:00Z") {
		t.Errorf("stdout = %q", stdout)
	}
	if ran := c.fake.Ran(); len(ran) != 0 {
		t.Errorf("không được chạy lệnh nào: %v", ran)
	}
	if c.dbExists() {
		t.Error("không được mở hay tạo DB")
	}
	after := c.fake.Snapshot()
	if len(after) != len(before) || string(after[cloudinit.StatePath].Data) != string(state) {
		t.Error("hệ thống file giả bị đổi")
	}
}
