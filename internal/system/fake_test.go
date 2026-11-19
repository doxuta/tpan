package system

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"sync"
	"testing"
	"time"
)

var _ System = (*FakeSystem)(nil)

func TestFakeSetFileReadFile(t *testing.T) {
	f := NewFake()
	f.SetFile("/etc/nginx/nginx.conf", []byte("worker 1;\n"), 0o644, Owner{})

	got, err := f.ReadFile("/etc/nginx/nginx.conf")
	if err != nil || string(got) != "worker 1;\n" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	// Thư mục cha được tạo tự động, chủ root.
	fi, err := f.Stat("/etc/nginx")
	if err != nil || !fi.Mode.IsDir() || fi.Owner != Root {
		t.Fatalf("Stat /etc/nginx = %+v, %v", fi, err)
	}
	// Owner rỗng được lưu thành root:root.
	fi, _ = f.Stat("/etc/nginx/nginx.conf")
	if fi.Owner != Root || fi.Mode != 0o644 || fi.Size != 10 {
		t.Fatalf("Stat file = %+v", fi)
	}
}

func TestFakeNotExist(t *testing.T) {
	f := NewFake()
	f.SetDir("/etc", 0o755, Root)
	errs := map[string]error{}
	_, errs["ReadFile"] = f.ReadFile("/etc/khong-co")
	_, errs["Stat"] = f.Stat("/etc/khong-co")
	_, errs["ReadDir"] = f.ReadDir("/khong-co")
	_, errs["Readlink"] = f.Readlink("/etc/khong-co")
	errs["Remove"] = f.Remove("/etc/khong-co")
	errs["Rename"] = f.Rename("/etc/khong-co", "/etc/x")
	errs["Chown"] = f.Chown("/etc/khong-co", Root)
	errs["Chmod"] = f.Chmod("/etc/khong-co", 0o600)
	errs["WriteFileAtomic"] = f.WriteFileAtomic("/khong-co/a.conf", nil, 0o644, Root)
	_, errs["StatFS"] = f.StatFS("/khong-co")
	for op, err := range errs {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: lỗi %v không thoả fs.ErrNotExist", op, err)
		}
	}
}

func TestFakeSymlink(t *testing.T) {
	f := NewFake()
	f.SetFile("/etc/nginx/sites-available/a.com", []byte("server{}"), 0o644, Root)
	f.SetDir("/etc/nginx/sites-enabled", 0o755, Root)
	if err := f.Symlink("../sites-available/a.com", "/etc/nginx/sites-enabled/a.com"); err != nil {
		t.Fatal(err)
	}
	// Tạo lại thì lỗi.
	if err := f.Symlink("x", "/etc/nginx/sites-enabled/a.com"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Symlink lần hai: %v", err)
	}
	// ReadFile đi theo symlink tương đối.
	got, err := f.ReadFile("/etc/nginx/sites-enabled/a.com")
	if err != nil || string(got) != "server{}" {
		t.Fatalf("ReadFile qua link = %q, %v", got, err)
	}
	// Stat không đi theo.
	fi, err := f.Stat("/etc/nginx/sites-enabled/a.com")
	if err != nil || fi.Mode&fs.ModeSymlink == 0 {
		t.Fatalf("Stat link = %+v, %v", fi, err)
	}
	target, err := f.Readlink("/etc/nginx/sites-enabled/a.com")
	if err != nil || target != "../sites-available/a.com" {
		t.Fatalf("Readlink = %q, %v", target, err)
	}
	// Readlink trên file thường thì lỗi.
	if _, err := f.Readlink("/etc/nginx/sites-available/a.com"); err == nil {
		t.Fatal("Readlink file thường phải lỗi")
	}
	// Symlink treo: Stat được, ReadFile ra ErrNotExist.
	f.SetSymlink("/khong-co", "/etc/treo")
	if _, err := f.Stat("/etc/treo"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadFile("/etc/treo"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile link treo: %v", err)
	}
	// Remove link không xoá đích.
	if err := f.Remove("/etc/nginx/sites-enabled/a.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadFile("/etc/nginx/sites-available/a.com"); err != nil {
		t.Fatal(err)
	}
	// Thư mục cha là symlink: đi theo như bản thật.
	f.SetFile("/var/www/releases/1/index.php", []byte("v1"), 0o644, Root)
	f.SetSymlink("releases/1", "/var/www/current")
	if got, err := f.ReadFile("/var/www/current/index.php"); err != nil || string(got) != "v1" {
		t.Fatalf("ReadFile qua thư mục link = %q, %v", got, err)
	}
	// Vòng lặp symlink thì lỗi, không treo.
	f.SetSymlink("/loop/b", "/loop/a")
	f.SetSymlink("/loop/a", "/loop/b")
	if _, err := f.ReadFile("/loop/a"); err == nil {
		t.Fatal("vòng symlink phải lỗi")
	}
}

func TestFakeWriteFileAtomic(t *testing.T) {
	f := NewFake()
	// Thư mục cha phải có sẵn.
	if err := f.WriteFileAtomic("/etc/tpan/a.conf", []byte("x"), 0o600, Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("thiếu thư mục cha: %v", err)
	}
	f.SetDir("/etc/tpan", 0o755, Root)
	owner := Owner{User: "www-data", Group: "www-data"}
	if err := f.WriteFileAtomic("/etc/tpan/a.conf", []byte("x"), 0o640, owner); err != nil {
		t.Fatal(err)
	}
	fi, _ := f.Stat("/etc/tpan/a.conf")
	if fi.Mode != 0o640 || fi.Owner != owner {
		t.Fatalf("Stat = %+v", fi)
	}
	// Ghi đè lên symlink thì thay chính link (rename không đi theo link).
	f.SetFile("/etc/goc", []byte("goc"), 0o644, Root)
	f.SetSymlink("/etc/goc", "/etc/tpan/link")
	if err := f.WriteFileAtomic("/etc/tpan/link", []byte("moi"), 0o644, Owner{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.ReadFile("/etc/goc"); string(got) != "goc" {
		t.Fatalf("đích của link bị đổi: %q", got)
	}
	if fi, _ := f.Stat("/etc/tpan/link"); fi.Mode&fs.ModeSymlink != 0 {
		t.Fatal("link vẫn còn là link")
	}
	// Ghi lên thư mục thì lỗi.
	if err := f.WriteFileAtomic("/etc/tpan", []byte("x"), 0o644, Root); err == nil {
		t.Fatal("ghi lên thư mục phải lỗi")
	}
}

func TestFakeMkdirAll(t *testing.T) {
	f := NewFake()
	f.SetDir("/var", 0o700, Root)
	owner := Owner{User: "site1", Group: "site1"}
	if err := f.MkdirAll("/var/www/site1/public", 0o750, owner); err != nil {
		t.Fatal(err)
	}
	// Thư mục đã có giữ nguyên quyền và chủ.
	if fi, _ := f.Stat("/var"); fi.Mode != fs.ModeDir|0o700 || fi.Owner != Root {
		t.Fatalf("/var bị đổi: %+v", fi)
	}
	for _, p := range []string{"/var/www", "/var/www/site1", "/var/www/site1/public"} {
		fi, err := f.Stat(p)
		if err != nil || fi.Mode != fs.ModeDir|0o750 || fi.Owner != owner {
			t.Fatalf("Stat %s = %+v, %v", p, fi, err)
		}
	}
	// Gọi lại không lỗi.
	if err := f.MkdirAll("/var/www/site1/public", 0o750, owner); err != nil {
		t.Fatal(err)
	}
	// Một phần là file thường thì lỗi.
	f.SetFile("/var/file", nil, 0o644, Root)
	if err := f.MkdirAll("/var/file/x", 0o755, Root); err == nil {
		t.Fatal("MkdirAll qua file thường phải lỗi")
	}
}

func TestFakeRemoveRenameReadDir(t *testing.T) {
	f := NewFake()
	f.SetFile("/d/b", nil, 0o644, Root)
	f.SetFile("/d/a", nil, 0o644, Root)
	f.SetDir("/d/c", 0o755, Root)
	f.SetFile("/d/c/x", nil, 0o644, Root)

	names, err := f.ReadDir("/d")
	if err != nil || !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatalf("ReadDir = %v, %v", names, err)
	}
	if _, err := f.ReadDir("/d/a"); err == nil {
		t.Fatal("ReadDir trên file phải lỗi")
	}
	// Thư mục không rỗng thì không xoá được.
	if err := f.Remove("/d/c"); err == nil {
		t.Fatal("Remove thư mục không rỗng phải lỗi")
	}
	// Rename thư mục mang theo cả con.
	if err := f.Rename("/d/c", "/d/e"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat("/d/e/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat("/d/c/x"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("con cũ còn: %v", err)
	}
	// Rename file đè file.
	if err := f.Rename("/d/a", "/d/b"); err != nil {
		t.Fatal(err)
	}
	names, _ = f.ReadDir("/d")
	if !reflect.DeepEqual(names, []string{"b", "e"}) {
		t.Fatalf("ReadDir sau rename = %v", names)
	}
	// Rename tới thư mục cha không có thì lỗi.
	if err := f.Rename("/d/b", "/khong/b"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("rename tới cha không có: %v", err)
	}
	if err := f.Remove("/d/e/x"); err != nil {
		t.Fatal(err)
	}
	if err := f.Remove("/d/e"); err != nil {
		t.Fatal(err)
	}
}

func TestFakeChownChmod(t *testing.T) {
	f := NewFake()
	f.SetFile("/a", nil, 0o644, Root)
	f.SetSymlink("/a", "/l")
	o := Owner{User: "u", Group: "g"}
	if err := f.Chown("/l", o); err != nil {
		t.Fatal(err)
	}
	// Chown không đi theo symlink.
	if fi, _ := f.Stat("/a"); fi.Owner != Root {
		t.Fatalf("Chown đi theo link: %+v", fi)
	}
	if fi, _ := f.Stat("/l"); fi.Owner != o {
		t.Fatalf("Chown link: %+v", fi)
	}
	if err := f.Chmod("/a", 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, _ := f.Stat("/a"); fi.Mode != 0o600 {
		t.Fatalf("Chmod: %+v", fi)
	}
	f.SetDir("/d", 0o755, Root)
	_ = f.Chmod("/d", 0o700)
	if fi, _ := f.Stat("/d"); fi.Mode != fs.ModeDir|0o700 {
		t.Fatalf("Chmod giữ bit thư mục: %+v", fi)
	}
}

func TestFakeFail(t *testing.T) {
	f := NewFake()
	f.SetDir("/etc", 0o755, Root)
	f.SetFile("/etc/a", []byte("cu"), 0o644, Root)
	boom := errors.New("đĩa đầy")
	for _, op := range []string{"write", "remove", "rename", "mkdir", "symlink", "chown", "chmod"} {
		f.Fail(op, "/etc/a", boom)
	}
	f.Fail("mkdir", "/etc/b", boom)
	before := f.Snapshot()

	checks := map[string]error{
		"write":   f.WriteFileAtomic("/etc/a", []byte("moi"), 0o644, Root),
		"remove":  f.Remove("/etc/a"),
		"rename":  f.Rename("/etc/a", "/etc/c"),
		"mkdir":   f.MkdirAll("/etc/b/c", 0o755, Root),
		"symlink": f.Symlink("/x", "/etc/a"),
		"chown":   f.Chown("/etc/a", Owner{User: "u"}),
		"chmod":   f.Chmod("/etc/a", 0o600),
	}
	for op, err := range checks {
		if !errors.Is(err, boom) {
			t.Errorf("%s: %v", op, err)
		}
	}
	if !reflect.DeepEqual(before, f.Snapshot()) {
		t.Fatal("thao tác lỗi vẫn đổi hệ thống file giả")
	}
}

func TestFakeSnapshot(t *testing.T) {
	f := NewFake()
	f.SetFile("/a/f", []byte("x"), 0o600, Owner{User: "u", Group: "g"})
	f.SetSymlink("/a/f", "/a/l")
	s := f.Snapshot()
	want := map[string]FakeEntry{
		"/":    {Mode: fs.ModeDir | 0o755, Owner: Root},
		"/a":   {Mode: fs.ModeDir | 0o755, Owner: Root},
		"/a/f": {Mode: 0o600, Owner: Owner{User: "u", Group: "g"}, Data: "x"},
		"/a/l": {Mode: fs.ModeSymlink | 0o777, Owner: Root, Target: "/a/f"},
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("Snapshot = %#v", s)
	}
	// Sửa bản chụp không làm đổi fake.
	delete(s, "/a/f")
	if _, err := f.Stat("/a/f"); err != nil {
		t.Fatal(err)
	}
}

func TestFakeRun(t *testing.T) {
	f := NewFake()
	ctx := context.Background()

	// Lệnh không khớp handler: mã 0, output rỗng.
	res, err := f.Run(ctx, Cmd{Name: "nginx", Args: []string{"-t"}})
	if err != nil || res != (Result{}) {
		t.Fatalf("Run không handler = %+v, %v", res, err)
	}

	f.Handle([]string{"systemctl"}, func(Cmd) Result { return Result{Stdout: "chung\n"} })
	f.Handle([]string{"systemctl", "is-active"}, func(Cmd) Result {
		return Result{ExitCode: 3, Stdout: "inactive\n", Stderr: "dong1\ndong2\n"}
	})
	// Handler được sửa hệ thống file giả (không deadlock).
	f.Handle([]string{"apt-get", "install"}, func(c Cmd) Result {
		f.SetDir("/etc/nginx", 0o755, Root)
		return Result{Stdout: "ok"}
	})

	var lines []string
	res, err = f.Run(ctx, Cmd{Name: "systemctl", Args: []string{"is-active", "nginx"}, OnOutput: func(l string) { lines = append(lines, l) }})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || res.ExitCode != 3 || res.Stdout != "inactive\n" {
		t.Fatalf("Run is-active = %+v, %v", res, err)
	}
	if ee.Cmd != "systemctl is-active nginx" || ee.Stderr != "dong1\ndong2" {
		t.Fatalf("ExitError = %+v", ee)
	}
	if !reflect.DeepEqual(lines, []string{"inactive", "dong1", "dong2"}) {
		t.Fatalf("OnOutput = %q", lines)
	}

	res, err = f.Run(ctx, Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}})
	if err != nil || res.Stdout != "chung\n" {
		t.Fatalf("prefix ngắn = %+v, %v", res, err)
	}
	// "systemctl" một mình không khớp prefix dài hơn nó.
	if _, err := f.Run(ctx, Cmd{Name: "systemctl"}); err != nil {
		t.Fatal(err)
	}

	stdin := []byte("bi mat")
	args := []string{"install", "-y", "nginx"}
	if _, err := f.Run(ctx, Cmd{Name: "apt-get", Args: args, Stdin: stdin}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat("/etc/nginx"); err != nil {
		t.Fatal("handler không sửa được fs giả")
	}
	// Ran lưu bản sao, sửa slice gốc không ảnh hưởng.
	args[0], stdin[0] = "X", 'X'
	ran := f.Ran()
	if len(ran) != 5 {
		t.Fatalf("Ran có %d lệnh", len(ran))
	}
	last := ran[4]
	if last.Name != "apt-get" || last.Args[0] != "install" || string(last.Stdin) != "bi mat" {
		t.Fatalf("Ran[4] = %+v", last)
	}
	f.ResetRan()
	if len(f.Ran()) != 0 {
		t.Fatal("ResetRan không xoá")
	}

	// ctx đã huỷ: lỗi, không phải *ExitError.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.Run(cctx, Cmd{Name: "true"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run với ctx huỷ: %v", err)
	}
}

func TestFakeStatFS(t *testing.T) {
	f := NewFake()
	f.SetDir("/var/lib/mysql", 0o755, Root)
	def, err := f.StatFS("/var/lib/mysql")
	if err != nil || def.Total == 0 {
		t.Fatalf("StatFS mặc định = %+v, %v", def, err)
	}
	f.SetDisk("/", DiskUsage{Total: 100, Free: 40})
	f.SetDisk("/var", DiskUsage{Total: 10, Free: 1})
	if du, _ := f.StatFS("/var/lib/mysql"); du != (DiskUsage{Total: 10, Free: 1}) {
		t.Fatalf("StatFS /var/lib/mysql = %+v", du)
	}
	if du, _ := f.StatFS("/"); du != (DiskUsage{Total: 100, Free: 40}) {
		t.Fatalf("StatFS / = %+v", du)
	}
	// "/variant" không thuộc "/var".
	f.SetDir("/variant", 0o755, Root)
	if du, _ := f.StatFS("/variant"); du.Total != 100 {
		t.Fatalf("StatFS /variant = %+v", du)
	}
}

func TestFakeLock(t *testing.T) {
	f := NewFake()
	unlock, err := f.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock khi đang bị giữ: %v", err)
	}
	got := make(chan struct{})
	go func() {
		u, err := f.Lock(context.Background())
		if err == nil {
			u()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("lấy được khoá khi chưa unlock")
	case <-time.After(30 * time.Millisecond):
	}
	unlock()
	unlock() // gọi thừa không làm hỏng khoá
	<-got
}

func TestFakeConcurrent(t *testing.T) {
	f := NewFake()
	f.SetDir("/d", 0o755, Root)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := "/d/" + string(rune('a'+i))
			_ = f.WriteFileAtomic(p, []byte("x"), 0o644, Root)
			_, _ = f.ReadFile(p)
			_, _ = f.Run(context.Background(), Cmd{Name: "true"})
			_ = f.Snapshot()
		}()
	}
	wg.Wait()
	if names, _ := f.ReadDir("/d"); len(names) != 20 {
		t.Fatalf("ReadDir = %v", names)
	}
	if len(f.Ran()) != 20 {
		t.Fatal("Ran thiếu lệnh")
	}
}
