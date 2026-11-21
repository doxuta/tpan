package cage

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

const (
	tSlug = "a_example_com"
	tUser = "tp_a_example_com"
	tHex  = "0123456789abcdef"
	tUnit = "tpan-cage-a_example_com-run-0123456789abcdef.service"
)

// caged dựng FakeSystem có file hàng rào của tSlug và Sys bọc nó với tên unit cố định.
func caged(t *testing.T) (*system.FakeSystem, *Sys) {
	t.Helper()
	f := system.NewFake()
	f.SetFile(site.CageFenceOfSlug(tSlug), []byte("[Service]\n"), 0o644, system.Root)
	s := Wrap(f)
	s.Rand = func() (string, error) { return tHex, nil }
	return f, s
}

// wantArgs là argv đúng thứ tự của hợp đồng (wrap.go, bước 3).
func wantArgs(unitTail, secs, dir string, prog ...string) []string {
	a := []string{
		"--quiet", "--wait", "--pipe", "--expand-environment=no",
		"--service-type=oneshot", "--unit=tpan-cage-a_example_com-run-" + unitTail,
		"-p", "User=" + tUser, "-p", "Group=" + tUser, "-p", "NoNewPrivileges=yes",
		"-p", "TimeoutStartSec=" + secs, "-E", "LC_ALL=C",
	}
	if dir != "" {
		a = append(a, "--working-directory="+dir)
	}
	return append(append(a, "--"), prog...)
}

// showOut là output của `systemctl show` cho một unit tạm đã chạy.
func showOut(load, result, status, started string) func(system.Cmd) system.Result {
	return func(system.Cmd) system.Result {
		return system.Result{Stdout: "LoadState=" + load + "\nResult=" + result +
			"\nExecMainStatus=" + status + "\nExecMainStartTimestamp=" + started + "\n"}
	}
}

func exitWith(code int, stderr string) func(system.Cmd) system.Result {
	return func(system.Cmd) system.Result { return system.Result{ExitCode: code, Stderr: stderr} }
}

func argvOf(c system.Cmd) []string { return append([]string{c.Name}, c.Args...) }

var showArgv = []string{"systemctl", "show", tUnit, "-p", "LoadState", "-p", "Result",
	"-p", "ExecMainStatus", "-p", "ExecMainStartTimestamp"}
var resetArgv = []string{"systemctl", "reset-failed", tUnit}

func TestRunKhongAsUserDiThang(t *testing.T) {
	f, s := caged(t)
	cmd := system.Cmd{Name: "nginx", Args: []string{"-t"}}
	if _, err := s.Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	ran := f.Ran()
	if len(ran) != 1 || ran[0].Name != "nginx" || !slices.Equal(ran[0].Args, []string{"-t"}) || ran[0].AsUser != "" {
		t.Fatalf("lệnh đã chạy = %+v", ran)
	}
}

func TestRunAsUserKhacDangDiThang(t *testing.T) {
	for _, u := range []string{"root", "www-data", "tp_a;b", "tp_A", "tp_", "xtp_a_example_com", "tp_a_example_com\n"} {
		f, s := caged(t)
		if _, err := s.Run(context.Background(), system.Cmd{Name: "id", AsUser: u}); err != nil {
			t.Fatalf("%q: %v", u, err)
		}
		ran := f.Ran()
		if len(ran) != 1 || ran[0].Name != "id" || ran[0].AsUser != u {
			t.Fatalf("%q: lệnh đã chạy = %+v", u, ran)
		}
	}
}

func TestRunAsUserKemEnvTuChoi(t *testing.T) {
	for _, u := range []string{tUser, "www-data"} {
		f, s := caged(t)
		_, err := s.Run(context.Background(), system.Cmd{Name: "id", AsUser: u, Env: []string{"HOME=/root"}})
		if err == nil {
			t.Fatalf("%q: AsUser kèm Env phải bị từ chối", u)
		}
		if n := len(f.Ran()); n != 0 {
			t.Fatalf("%q: không lệnh nào được chạy, đã chạy %d", u, n)
		}
	}
}

func TestRunKhongHangRaoChayNhuCu(t *testing.T) {
	f := system.NewFake()
	s := Wrap(f)
	cmd := system.Cmd{Name: "wp", Args: []string{"core", "is-installed"}, AsUser: tUser, Dir: "/var/www/a.example.com"}
	if _, err := s.Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	ran := f.Ran()
	if len(ran) != 1 || ran[0].Name != "wp" || ran[0].AsUser != tUser || ran[0].Dir != cmd.Dir {
		t.Fatalf("lệnh đã chạy = %+v", ran)
	}
}

func TestRunCoHangRaoArgv(t *testing.T) {
	f, s := caged(t)
	f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "xin chao\n", Stderr: "canh bao\n"}
	})
	var lines []string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := s.Run(ctx, system.Cmd{
		Name: "wp", Args: []string{"option", "get", "home"}, AsUser: tUser,
		Dir: "/var/www/a.example.com", Stdin: []byte("bi-mat\n"),
		OnOutput: func(l string) { lines = append(lines, l) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ran := f.Ran()
	if len(ran) != 1 {
		t.Fatalf("mã 0 thì chỉ chạy systemd-run, đã chạy %+v", ran)
	}
	got := ran[0]
	want := wantArgs(tHex, "31", "/var/www/a.example.com", "wp", "option", "get", "home")
	if got.Name != "systemd-run" || !slices.Equal(got.Args, want) {
		t.Fatalf("argv =\n%q\nmuốn\n%q", argvOf(got), append([]string{"systemd-run"}, want...))
	}
	if got.AsUser != "" || got.Dir != "" {
		t.Fatalf("lệnh systemd-run chạy bằng root ở /: AsUser=%q Dir=%q", got.AsUser, got.Dir)
	}
	if string(got.Stdin) != "bi-mat\n" {
		t.Fatalf("Stdin = %q", got.Stdin)
	}
	if !slices.Equal(lines, []string{"xin chao", "canh bao"}) {
		t.Fatalf("OnOutput nhận %q", lines)
	}
	if res.Stdout != "xin chao\n" {
		t.Fatalf("Stdout = %q", res.Stdout)
	}
}

func TestRunKhongHanVaKhongDir(t *testing.T) {
	f, s := caged(t)
	if _, err := s.Run(context.Background(), system.Cmd{Name: "true", AsUser: tUser}); err != nil {
		t.Fatal(err)
	}
	if got, want := f.Ran()[0].Args, wantArgs(tHex, "3600", "", "true"); !slices.Equal(got, want) {
		t.Fatalf("argv = %q, muốn %q", got, want)
	}
}

func TestRunTenUnitMacDinh(t *testing.T) {
	f, s := caged(t)
	s.Rand = nil
	if _, err := s.Run(context.Background(), system.Cmd{Name: "true", AsUser: tUser}); err != nil {
		t.Fatal(err)
	}
	unit := f.Ran()[0].Args[5]
	if !regexp.MustCompile(`^--unit=tpan-cage-a_example_com-run-[0-9a-f]{16}$`).MatchString(unit) {
		t.Fatalf("tên unit = %q", unit)
	}
}

func TestRunRandLoiKhongChay(t *testing.T) {
	f, s := caged(t)
	s.Rand = func() (string, error) { return "", errors.New("hết entropy") }
	if _, err := s.Run(context.Background(), system.Cmd{Name: "true", AsUser: tUser}); err == nil {
		t.Fatal("Rand lỗi phải trả lỗi")
	}
	if n := len(f.Ran()); n != 0 {
		t.Fatalf("không lệnh nào được chạy, đã chạy %d", n)
	}
}

// statErrSys làm Stat lỗi khác ErrNotExist (ví dụ thiếu quyền).
type statErrSys struct{ *system.FakeSystem }

func (statErrSys) Stat(p string) (system.FileInfo, error) {
	return system.FileInfo{}, &fs.PathError{Op: "lstat", Path: p, Err: syscall.EACCES}
}

func TestRunStatLoiKhongChayGi(t *testing.T) {
	f := system.NewFake()
	s := Wrap(statErrSys{f})
	_, err := s.Run(context.Background(), system.Cmd{Name: "wp", AsUser: tUser})
	if err == nil || !errors.Is(err, syscall.EACCES) {
		t.Fatalf("lỗi = %v, muốn bọc EACCES", err)
	}
	if n := len(f.Ran()); n != 0 {
		t.Fatalf("không lệnh nào được chạy, đã chạy %d", n)
	}
}

func TestRunMaThoatThat(t *testing.T) {
	f, s := caged(t)
	f.Handle([]string{"systemd-run"}, exitWith(1, "Error: WordPress is not installed.\n"))
	f.Handle([]string{"systemctl", "show"}, showOut("loaded", "exit-code", "1", "Sat 2026-11-21 10:00:00 UTC"))
	res, err := s.Run(context.Background(), system.Cmd{Name: "wp", Args: []string{"core", "is-installed"}, AsUser: tUser})
	var ee *system.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("lỗi = %v, muốn *system.ExitError mã 1", err)
	}
	if errors.Is(err, ErrKilled) {
		t.Fatal("mã thoát thật không phải ErrKilled")
	}
	if !strings.Contains(ee.Stderr, "not installed") || res.ExitCode != 1 {
		t.Fatalf("Stderr = %q, ExitCode = %d", ee.Stderr, res.ExitCode)
	}
	assertTail(t, f, showArgv, resetArgv)
}

// assertTail kiểm các lệnh chạy sau systemd-run, đúng thứ tự.
func assertTail(t *testing.T, f *system.FakeSystem, want ...[]string) {
	t.Helper()
	ran := f.Ran()
	if len(ran) != len(want)+1 || ran[0].Name != "systemd-run" {
		t.Fatalf("lệnh đã chạy = %+v", ran)
	}
	for i, w := range want {
		c := ran[i+1]
		if !slices.Equal(argvOf(c), w) {
			t.Fatalf("lệnh %d = %q, muốn %q", i+1, argvOf(c), w)
		}
		if c.AsUser != "" {
			t.Fatalf("systemctl chạy bằng root, không qua hàng rào: AsUser=%q", c.AsUser)
		}
	}
}

func TestRunKhongDungDuocMoiTruong(t *testing.T) {
	f, s := caged(t)
	f.Handle([]string{"systemd-run"}, exitWith(226, "Failed to set up mount namespacing\n"))
	f.Handle([]string{"systemctl", "show"}, showOut("loaded", "exit-code", "226", ""))
	_, err := s.Run(context.Background(), system.Cmd{Name: "wp", AsUser: tUser})
	assertPlain(t, err)
	if !strings.Contains(err.Error(), "không chạy được") || !strings.Contains(err.Error(), "namespacing") {
		t.Fatalf("lỗi = %v", err)
	}
	assertTail(t, f, showArgv, resetArgv)
}

func TestRunUnitKhongCo(t *testing.T) {
	f, s := caged(t)
	f.Handle([]string{"systemd-run"}, exitWith(1, "Failed to find executable wpx\n"))
	f.Handle([]string{"systemctl", "show"}, showOut("not-found", "success", "0", ""))
	_, err := s.Run(context.Background(), system.Cmd{Name: "wpx", AsUser: tUser})
	assertPlain(t, err)
	if !strings.Contains(err.Error(), "không chạy được") {
		t.Fatalf("lỗi = %v", err)
	}
	assertTail(t, f, showArgv, resetArgv)
}

func TestRunShowLoiVanResetFailed(t *testing.T) {
	f, s := caged(t)
	f.Handle([]string{"systemd-run"}, exitWith(1, ""))
	f.Handle([]string{"systemctl", "show"}, exitWith(1, "Failed to connect to bus\n"))
	_, err := s.Run(context.Background(), system.Cmd{Name: "wp", AsUser: tUser})
	assertPlain(t, err)
	assertTail(t, f, showArgv, resetArgv)
}

// assertPlain: có lỗi, không phải ExitError (không đọc nhầm thành mã thoát có
// nghĩa) và không phải ErrKilled.
func assertPlain(t *testing.T, err error) {
	t.Helper()
	var ee *system.ExitError
	if err == nil || errors.As(err, &ee) || errors.Is(err, ErrKilled) {
		t.Fatalf("lỗi = %#v, muốn lỗi thường", err)
	}
}

func TestRunBiDung(t *testing.T) {
	for _, tc := range []struct {
		result, status string
		code           int
	}{
		{"oom-kill", "9", 1},
		{"timeout", "15", 1},
		{"signal", "15", 255},
		{"core-dump", "11", 255},
		{"start-limit-hit", "0", 1},
	} {
		f, s := caged(t)
		f.Handle([]string{"systemd-run"}, exitWith(tc.code, "Killed\n"))
		f.Handle([]string{"systemctl", "show"}, showOut("loaded", tc.result, tc.status, "Sat 2026-11-21 10:00:00 UTC"))
		_, err := s.Run(context.Background(), system.Cmd{Name: "php", AsUser: tUser})
		var ee *system.ExitError
		if !errors.Is(err, ErrKilled) || errors.As(err, &ee) {
			t.Fatalf("%s: lỗi = %#v, muốn ErrKilled và không phải ExitError", tc.result, err)
		}
		var ke *KilledError
		if !errors.As(err, &ke) || ke.Result != tc.result || ke.Unit != tUnit || strings.TrimSpace(ke.Stderr) != "Killed" {
			t.Fatalf("%s: KilledError = %+v", tc.result, ke)
		}
		if tc.status == "15" && ke.Status != 15 {
			t.Fatalf("%s: Status = %d", tc.result, ke.Status)
		}
		assertTail(t, f, showArgv, resetArgv)
	}
}

func TestRunCtxHuyThiStop(t *testing.T) {
	f, s := caged(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
		cancel() // tpan bị huỷ khi lệnh trong lồng đang chạy
		return system.Result{ExitCode: -1}
	})
	_, err := s.Run(ctx, system.Cmd{Name: "wp", AsUser: tUser})
	var ee *system.ExitError
	if !errors.Is(err, context.Canceled) || errors.As(err, &ee) {
		t.Fatalf("lỗi = %v, muốn context.Canceled và không phải ExitError", err)
	}
	ran := f.Ran()
	stop := []string{"systemctl", "stop", tUnit}
	if !slices.ContainsFunc(ran, func(c system.Cmd) bool { return slices.Equal(argvOf(c), stop) }) {
		t.Fatalf("thiếu %q trong %+v", stop, ran)
	}
	if slices.ContainsFunc(ran, func(c system.Cmd) bool { return slices.Equal(argvOf(c), showArgv) }) {
		t.Fatal("ctx huỷ thì không phân loại mã thoát")
	}
}

func TestRunThamSoKhoNguyenVen(t *testing.T) {
	hard := []string{"", "a b", "$HOME", "${PATH}", "%n", "$(id)", "dong1\ndong2", "-x", "'", `"`, "--", "/%postname%/"}
	f, s := caged(t)
	if _, err := s.Run(context.Background(), system.Cmd{Name: "printf", Args: hard, AsUser: tUser}); err != nil {
		t.Fatal(err)
	}
	args := f.Ran()[0].Args
	i := slices.Index(args, "--")
	if i < 0 || args[i+1] != "printf" || !slices.Equal(args[i+2:], hard) {
		t.Fatalf("sau -- = %q, muốn printf %q", args[i+1:], hard)
	}
	if !slices.Contains(args[:i], "--expand-environment=no") {
		t.Fatal("thiếu --expand-environment=no trước --")
	}
}

func TestDescribe(t *testing.T) {
	_, s := caged(t)
	cmd := system.Cmd{Name: "wp", Args: []string{"core", "verify-checksums"}, AsUser: tUser, Dir: "/var/www/a.example.com"}
	want := system.Cmd{Name: "systemd-run", Args: wantArgs("xem", "3600", "/var/www/a.example.com", "wp", "core", "verify-checksums")}.String()
	if got := s.Describe(cmd); got != want {
		t.Fatalf("Describe =\n%s\nmuốn\n%s", got, want)
	}
	if got := system.Describe(s, cmd); got != want {
		t.Fatalf("system.Describe = %s", got)
	}
	for _, c := range []system.Cmd{
		{Name: "nginx", Args: []string{"-t"}},
		{Name: "id", AsUser: "www-data"},
	} {
		if got := s.Describe(c); got != c.String() {
			t.Fatalf("Describe(%v) = %s", c, got)
		}
	}
	// Không có hàng rào: lệnh chạy bằng runuser như cũ.
	plain := Wrap(system.NewFake())
	if got := plain.Describe(cmd); got != cmd.String() {
		t.Fatalf("không hàng rào: Describe = %s", got)
	}
}
