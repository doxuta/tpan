package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// giaMac cho test chạy như trên macOS: HOME là thư mục tạm, file chạy tpan
// nằm ở đường dẫn có dấu cách (kiểm nháy trong file .command).
func giaMac(t *testing.T, goos string) (home, exe string) {
	t.Helper()
	oldOS, oldExe := brainInstallGOOS, brainInstallExe
	t.Cleanup(func() { brainInstallGOOS, brainInstallExe = oldOS, oldExe })
	home = t.TempDir()
	t.Setenv("HOME", home)
	exe = "/opt/ma nguon/bin/tpan"
	brainInstallGOOS = goos
	brainInstallExe = func() (string, error) { return exe, nil }
	return home, exe
}

func TestBrainInstallChiCoTrenMac(t *testing.T) {
	giaMac(t, "linux")
	c := newCLI(t)
	c.root = false
	code, _, errs := c.run("brain-install", "tpan")
	if code != 1 || !strings.Contains(errs, "chỉ có trên macOS") {
		t.Errorf("Linux: mã %d, stderr %q", code, errs)
	}
	if len(c.fake.Ran()) != 0 {
		t.Errorf("bị từ chối mà vẫn chạy lệnh: %v", c.brainRan())
	}
}

func TestBrainInstallTuChoiDichSai(t *testing.T) {
	giaMac(t, "darwin")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"brain-install"}, "cần đúng một máy chủ"},
		{[]string{"brain-install", "a", "b"}, "cần đúng một máy chủ"},
		{[]string{"brain-install", "host;id"}, "không hợp lệ"},
		{[]string{"brain-install", "-oProxyCommand=id"}, ""},
		{[]string{"brain-install", "x'y"}, "không hợp lệ"},
		{[]string{"brain-install", "tpan", "--remove"}, "--remove không nhận"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(tc.args...)
		if code == 0 || !strings.Contains(errs, tc.want) {
			t.Errorf("%v: mã %d, stderr %q", tc.args, code, errs)
		}
		if len(c.fake.Ran()) != 0 {
			t.Errorf("%v: bị từ chối mà vẫn chạy lệnh %v", tc.args, c.brainRan())
		}
	}
}

func TestBrainInstallCaiApp(t *testing.T) {
	home, exe := giaMac(t, "darwin")
	c := newCLI(t)
	c.root = false
	app := filepath.Join(home, "Applications", brainAppName)
	var script string
	c.fake.Handle([]string{"/usr/bin/osacompile"}, func(cmd system.Cmd) system.Result {
		b, err := os.ReadFile(cmd.Args[len(cmd.Args)-1])
		if err != nil {
			t.Errorf("osacompile không đọc được mã nguồn: %v", err)
		}
		script = string(b)
		return system.Result{}
	})
	code, out, errs := c.run("brain-install", "ubuntu@example.com")
	if code != 0 {
		t.Fatalf("mã %d, stderr %q", code, errs)
	}
	if !strings.Contains(out, "open tpan-brain://connect") {
		t.Errorf("không chỉ cách thử: %q", out)
	}

	// Thứ tự lệnh: dựng app, sửa Info.plist, ký lại, đăng ký với LaunchServices.
	want := []string{"/usr/bin/osacompile", "/usr/bin/plutil", "/usr/bin/plutil", "/usr/bin/plutil", "/usr/bin/codesign", lsregisterPath}
	if got := c.brainRan(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("lệnh đã chạy %v, muốn %v", got, want)
	}
	ran := c.fake.Ran()
	if a := ran[0].Args; len(a) != 3 || a[0] != "-o" || a[1] != app {
		t.Errorf("osacompile %v", a)
	}
	if !strings.Contains(strings.Join(ran[2].Args, " "), `"CFBundleURLSchemes":["tpan-brain"]`) {
		t.Errorf("không đăng ký link tpan-brain://: %v", ran[2].Args)
	}

	// Mã AppleScript: chỉ ASCII, không chứa đường dẫn file chạy, không điều
	// khiển Terminal bằng AppleScript (khỏi xin quyền Automation), không đọc gì từ link.
	for _, bad := range []string{"tell application", exe, "theURL &", "& theURL"} {
		if strings.Contains(script, bad) {
			t.Errorf("mã AppleScript chứa %q:\n%s", bad, script)
		}
	}
	for i, r := range script {
		if r > 127 {
			t.Fatalf("mã AppleScript có ký tự ngoài ASCII ở vị trí %d", i)
		}
	}
	if !strings.Contains(script, appleString(brainRunningPattern("tpan", "ubuntu@example.com"))) || !strings.Contains(script, `starts with "tpan-brain:"`) {
		t.Errorf("mã AppleScript thiếu phép kiểm đang chạy hoặc kiểm link:\n%s", script)
	}

	// File .command: exec đúng file chạy và đúng đích, đã bọc nháy.
	b, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", brainCommandFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\nexec '/opt/ma nguon/bin/tpan' brain 'ubuntu@example.com'\n") {
		t.Errorf("file .command:\n%s", b)
	}
	if fi, _ := os.Stat(filepath.Join(app, "Contents", "Resources", brainCommandFile)); fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("file .command không chạy được: %v", fi.Mode())
	}

	// Chạy lại với đích khác: thay app cũ của chính tpan.
	c.fake.ResetRan()
	if code, _, errs := c.run("brain-install", "tpan"); code != 0 {
		t.Fatalf("cài lại: mã %d, stderr %q", code, errs)
	}
	b, _ = os.ReadFile(filepath.Join(app, "Contents", "Resources", brainCommandFile))
	if !strings.Contains(string(b), "brain 'tpan'\n") {
		t.Errorf("cài lại không đổi đích:\n%s", b)
	}
}

func TestBrainInstallKhongDungAppLa(t *testing.T) {
	home, _ := giaMac(t, "darwin")
	app := filepath.Join(home, "Applications", brainAppName)
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"brain-install", "tpan"}, {"brain-install", "--remove"}} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "không phải app do tpan cài") {
			t.Errorf("%v với app lạ: mã %d, stderr %q", args, code, errs)
		}
		if len(c.fake.Ran()) != 0 {
			t.Errorf("%v: app lạ mà vẫn chạy lệnh %v", args, c.brainRan())
		}
	}
	if _, err := os.Stat(filepath.Join(app, "Contents")); err != nil {
		t.Errorf("app lạ bị đụng tới: %v", err)
	}
}

func TestBrainInstallGo(t *testing.T) {
	home, _ := giaMac(t, "darwin")
	c := newCLI(t)
	c.root = false
	if code, _, errs := c.run("brain-install", "tpan"); code != 0 {
		t.Fatalf("cài: mã %d, stderr %q", code, errs)
	}
	c.fake.ResetRan()
	code, out, errs := c.run("brain-install", "--remove")
	app := filepath.Join(home, "Applications", brainAppName)
	if code != 0 || !strings.Contains(out, "Đã gỡ") {
		t.Errorf("gỡ: mã %d, stdout %q, stderr %q", code, out, errs)
	}
	if _, err := os.Stat(app); !os.IsNotExist(err) {
		t.Errorf("app còn sau khi gỡ: %v", err)
	}
	if got := c.brainRan(); len(got) != 1 || got[0] != lsregisterPath {
		t.Errorf("gỡ chạy %v, muốn chỉ lsregister -u", got)
	}
	// Gỡ lần nữa: không có gì để gỡ, không lỗi.
	if code, out, _ := c.run("brain-install", "--remove"); code != 0 || !strings.Contains(out, "không có gì để gỡ") {
		t.Errorf("gỡ lần hai: mã %d, stdout %q", code, out)
	}
}

// Mẫu pgrep phải nhận ra bộ não gõ tay lẫn do app mở, và KHÔNG khớp chính
// shell đang chạy pgrep (nếu khớp, app tưởng bộ não đang chạy và không bao giờ mở).
func TestBrainRunningPattern(t *testing.T) {
	p := brainRunningPattern("tpan", "ubuntu@example.com")
	re := regexp.MustCompile(p)
	for _, s := range []string{
		"/Users/a/tpan/bin/tpan brain ubuntu@example.com",
		"bin/tpan brain ubuntu@example.com",
		"tpan brain ubuntu@example.com",
	} {
		if !re.MatchString(s) {
			t.Errorf("%q không khớp %q", p, s)
		}
	}
	shell := "/bin/sh -c /usr/bin/pgrep -f " + shq(p) + " >/dev/null && echo yes || echo no"
	for _, s := range []string{
		shell,
		"/Users/a/tpan/bin/tpan brain ubuntu@example.com.evil",
		"/Users/a/tpan/bin/tpan brain ubuntu@exampleXcom",
		"/Users/a/bin/mytpan brain ubuntu@example.com",
		"ssh -o BatchMode=yes ubuntu@example.com sudo -n tpan brain-link",
	} {
		if re.MatchString(s) {
			t.Errorf("%q khớp nhầm %q", p, s)
		}
	}
}
