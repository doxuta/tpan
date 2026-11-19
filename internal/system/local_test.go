package system

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var _ System = (*LocalSystem)(nil)

// currentOwner là user và nhóm chính của tiến trình test: chown về chính mình
// thì không cần root.
func currentOwner(t *testing.T) Owner {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Fatal(err)
	}
	return Owner{User: u.Username, Group: g.Name}
}

func TestLocalWriteFileAtomic(t *testing.T) {
	s := NewLocal()
	dir := t.TempDir()
	o := currentOwner(t)
	p := filepath.Join(dir, "a.conf")

	if err := s.WriteFileAtomic(p, []byte("v1"), 0o640, o); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFileAtomic(p, []byte("v2"), 0o600, o); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFile(p)
	if err != nil || string(got) != "v2" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	fi, err := s.Stat(p)
	if err != nil || fi.Mode != 0o600 || fi.Size != 2 || fi.Owner != o {
		t.Fatalf("Stat = %+v, %v (muốn chủ %+v)", fi, err, o)
	}
	// Không còn file tạm.
	if names, _ := s.ReadDir(dir); !reflect.DeepEqual(names, []string{"a.conf"}) {
		t.Fatalf("còn file tạm: %v", names)
	}
	// Thư mục cha không có.
	if err := s.WriteFileAtomic(filepath.Join(dir, "khong", "a"), nil, 0o644, o); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("thiếu thư mục cha: %v", err)
	}
	// Chủ không tồn tại: lỗi trước cả khi tạo file tạm.
	if err := s.WriteFileAtomic(p, []byte("v3"), 0o644, Owner{User: "tpan-khong-co-user-nay"}); err == nil {
		t.Fatal("chủ không tồn tại phải lỗi")
	}
	// Lỗi sau khi đã có file tạm: user thường không chown được sang root.
	// File cũ phải giữ nguyên và file tạm phải bị xoá.
	if os.Geteuid() == 0 {
		t.Skip("root chown được, không tạo được lỗi giữa chừng")
	}
	if err := s.WriteFileAtomic(p, []byte("v3"), 0o644, Owner{User: "root", Group: o.Group}); err == nil {
		t.Fatal("chown sang root bằng user thường phải lỗi")
	}
	if got, _ := s.ReadFile(p); string(got) != "v2" {
		t.Fatalf("file cũ bị đổi: %q", got)
	}
	if names, _ := s.ReadDir(dir); !reflect.DeepEqual(names, []string{"a.conf"}) {
		t.Fatalf("còn file tạm sau lỗi: %v", names)
	}
}

func TestLocalSymlinkStat(t *testing.T) {
	s := NewLocal()
	dir := t.TempDir()
	o := currentOwner(t)
	target := filepath.Join(dir, "goc")
	link := filepath.Join(dir, "link")
	if err := s.WriteFileAtomic(target, []byte("noi dung"), 0o644, o); err != nil {
		t.Fatal(err)
	}
	if err := s.Symlink("goc", link); err != nil {
		t.Fatal(err)
	}
	if err := s.Symlink("goc", link); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Symlink lần hai: %v", err)
	}
	if got, err := s.Readlink(link); err != nil || got != "goc" {
		t.Fatalf("Readlink = %q, %v", got, err)
	}
	fi, err := s.Stat(link)
	if err != nil || fi.Mode&fs.ModeSymlink == 0 {
		t.Fatalf("Stat link = %+v, %v", fi, err)
	}
	if got, err := s.ReadFile(link); err != nil || string(got) != "noi dung" {
		t.Fatalf("ReadFile qua link = %q, %v", got, err)
	}
	if err := s.Chown(link, o); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat(target); err != nil {
		t.Fatal("Remove link xoá luôn đích")
	}
}

func TestLocalMkdirAllReadDirRename(t *testing.T) {
	s := NewLocal()
	dir := t.TempDir()
	o := currentOwner(t)
	if err := s.Chmod(dir, 0o711); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(dir, "a", "b", "c")
	if err := s.MkdirAll(deep, 0o750, o); err != nil {
		t.Fatal(err)
	}
	if err := s.MkdirAll(deep, 0o700, o); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, "a"), filepath.Join(dir, "a", "b"), deep} {
		fi, err := s.Stat(p)
		// Quyền đúng perm, không bị umask cắt; gọi lần hai không đổi.
		if err != nil || fi.Mode != fs.ModeDir|0o750 || fi.Owner != o {
			t.Fatalf("Stat %s = %+v, %v", p, fi, err)
		}
	}
	if fi, _ := s.Stat(dir); fi.Mode != fs.ModeDir|0o711 {
		t.Fatalf("thư mục đã có bị đổi quyền: %+v", fi)
	}
	for _, n := range []string{"b", "c", "a2"} {
		if err := s.WriteFileAtomic(filepath.Join(dir, n), nil, 0o644, o); err != nil {
			t.Fatal(err)
		}
	}
	names, err := s.ReadDir(dir)
	if err != nil || !sort.StringsAreSorted(names) || len(names) != 4 {
		t.Fatalf("ReadDir = %v, %v", names, err)
	}
	if err := s.Rename(filepath.Join(dir, "a2"), filepath.Join(dir, "d")); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(filepath.Join(dir, "a")); err == nil {
		t.Fatal("Remove thư mục không rỗng phải lỗi")
	}
	// Một phần đường dẫn là file thường.
	if err := s.MkdirAll(filepath.Join(dir, "b", "x"), 0o755, o); err == nil {
		t.Fatal("MkdirAll qua file thường phải lỗi")
	}
}

func TestLocalNotExist(t *testing.T) {
	s := NewLocal()
	p := filepath.Join(t.TempDir(), "khong-co")
	errs := map[string]error{}
	_, errs["ReadFile"] = s.ReadFile(p)
	_, errs["Stat"] = s.Stat(p)
	_, errs["ReadDir"] = s.ReadDir(p)
	_, errs["Readlink"] = s.Readlink(p)
	_, errs["StatFS"] = s.StatFS(p)
	errs["Remove"] = s.Remove(p)
	errs["Rename"] = s.Rename(p, p+"2")
	errs["Chmod"] = s.Chmod(p, 0o600)
	errs["Chown"] = s.Chown(p, currentOwner(t))
	for op, err := range errs {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: lỗi %v không thoả fs.ErrNotExist", op, err)
		}
	}
}

func TestLocalStatFS(t *testing.T) {
	du, err := NewLocal().StatFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if du.Total == 0 || du.Free > du.Total {
		t.Fatalf("StatFS = %+v", du)
	}
}

func TestLocalRun(t *testing.T) {
	s := NewLocal()
	ctx := context.Background()

	res, err := s.Run(ctx, Cmd{Name: "true"})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("true = %+v, %v", res, err)
	}

	res, err = s.Run(ctx, Cmd{Name: "false"})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 1 || res.ExitCode != 1 || ee.Cmd != "false" {
		t.Fatalf("false = %+v, %v", res, err)
	}

	// Stdin vào, từng dòng ra qua OnOutput.
	var lines []string
	res, err = s.Run(ctx, Cmd{
		Name:     "cat",
		Stdin:    []byte("mot\nhai\nba"),
		OnOutput: func(l string) { lines = append(lines, l) },
	})
	if err != nil || res.Stdout != "mot\nhai\nba" {
		t.Fatalf("cat = %+v, %v", res, err)
	}
	if !reflect.DeepEqual(lines, []string{"mot", "hai", "ba"}) {
		t.Fatalf("OnOutput = %q", lines)
	}

	// stderr cũng qua OnOutput, và vào ExitError.
	lines = nil
	missing := "/tpan-khong-co-duong-dan-nay"
	res, err = s.Run(ctx, Cmd{Name: "ls", Args: []string{missing}, OnOutput: func(l string) { lines = append(lines, l) }})
	if !errors.As(err, &ee) || ee.Code == 0 || !strings.Contains(ee.Stderr, missing) || !strings.Contains(res.Stderr, missing) {
		t.Fatalf("ls = %+v, %v", res, err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], missing) {
		t.Fatalf("OnOutput stderr = %q", lines)
	}

	// Môi trường cố định, không kế thừa từ tiến trình gọi.
	t.Setenv("TPAN_TEST_RO_RI", "1")
	res, err = s.Run(ctx, Cmd{Name: "env"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	sort.Strings(got)
	want := []string{"DEBIAN_FRONTEND=noninteractive", "LC_ALL=C", "PATH=" + fixedPATH}
	if os.Geteuid() == 0 {
		want = append(want, "HOME=/root")
		sort.Strings(want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %q", got)
	}

	// Không có chương trình: lỗi, không phải ExitError.
	_, err = s.Run(ctx, Cmd{Name: "tpan-khong-co-lenh-nay"})
	if err == nil || errors.As(err, &ee) || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("lệnh không có: %v", err)
	}
}

func TestLocalRunContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := NewLocal().Run(ctx, Cmd{Name: "sleep", Args: []string{"10"}})
	var ee *ExitError
	if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ee) {
		t.Fatalf("sleep hết giờ: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("Run không dừng khi ctx hết hạn")
	}
}

func TestRunArgv(t *testing.T) {
	name, args := runArgv(Cmd{Name: "wp", Args: []string{"core", "version"}, AsUser: "site1"})
	if name != "runuser" || !reflect.DeepEqual(args, []string{"-u", "site1", "--", "wp", "core", "version"}) {
		t.Fatalf("runArgv = %s %q", name, args)
	}
	name, args = runArgv(Cmd{Name: "nginx", Args: []string{"-t"}})
	if name != "nginx" || !reflect.DeepEqual(args, []string{"-t"}) {
		t.Fatalf("runArgv = %s %q", name, args)
	}
}

func TestLocalLock(t *testing.T) {
	s := NewLocal()
	s.LockPath = filepath.Join(t.TempDir(), "run", "tpan.lock")

	unlock, err := s.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := s.Stat(filepath.Dir(s.LockPath)); err != nil || fi.Mode != fs.ModeDir|0o700 {
		t.Fatalf("thư mục khoá = %+v, %v", fi, err)
	}

	// Đang bị giữ: hết hạn ctx thì trả lỗi.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := s.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock khi đang bị giữ: %v", err)
	}

	// Lần hai chờ tới khi lần một unlock.
	got := make(chan error, 1)
	go func() {
		u, err := s.Lock(context.Background())
		if err == nil {
			u()
		}
		got <- err
	}()
	select {
	case err := <-got:
		t.Fatalf("lấy được khoá khi chưa unlock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	unlock() // gọi thừa không sao
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("không lấy được khoá sau khi unlock")
	}
}

// M8: Dir, Env (cộng vào môi trường cố định, PATH trong Env dùng để tìm lệnh)
// và StartPipe (ống hai chiều cho tpan brain).
func TestLocalRunDirEnvPipe(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := NewLocal()
	res, err := s.Run(ctx, Cmd{Name: "pwd", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	// macOS: /var là symlink tới /private/var.
	want, _ := filepath.EvalSymlinks(dir)
	if got := strings.TrimSpace(res.Stdout); got != dir && got != want {
		t.Errorf("pwd = %q, muốn %q", got, dir)
	}
	res, err = s.Run(ctx, Cmd{Name: "env", Env: []string{"TPAN_X=1", "LC_ALL=en_US.UTF-8"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stdout, "TPAN_X=1\n") || !strings.Contains(res.Stdout, "LC_ALL=en_US.UTF-8\n") || strings.Contains(res.Stdout, "LC_ALL=C\n") {
		t.Errorf("env thiếu biến thêm hoặc không đè được biến trùng tên:\n%s", res.Stdout)
	}
	// Lệnh chỉ có trong PATH của Env thì vẫn tìm được.
	bin := filepath.Join(dir, "tpan-only-here")
	if err := os.WriteFile(bin, []byte("#!/bin/cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(ctx, Cmd{Name: "tpan-only-here"}); err == nil {
		t.Error("lệnh ngoài PATH cố định mà vẫn chạy được")
	}
	if _, err := s.Run(ctx, Cmd{Name: "tpan-only-here", Env: []string{"PATH=" + dir}}); err != nil {
		t.Errorf("lệnh trong PATH của Env: %v", err)
	}

	p, err := StartPipe(ctx, Cmd{Name: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stdin.Write([]byte("xin chào\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := p.Stdout.Read(buf)
	if err != nil || string(buf[:n]) != "xin chào\n" {
		t.Errorf("đọc lại qua ống = %q, %v", buf[:n], err)
	}
	_ = p.Stdin.Close()
	if err := p.Wait(); err != nil {
		t.Errorf("Wait: %v", err)
	}
}
