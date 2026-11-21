package sitefs

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ---- Ordinary use: what the steps rely on ----

func TestCuaThaoTacThuong(t *testing.T) {
	z := newZone(t, false)
	uid, gid := os.Geteuid(), os.Getegid()
	a := z.anchor

	if err := MkdirAll(a, "public/wp-content", 0o750, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(a, "public/.htaccess", []byte("Options -Indexes\n"), 0o640, uid, gid); err != nil {
		t.Fatal(err)
	}
	b, err := ReadFile(a, "public/.htaccess", 0)
	if err != nil || string(b) != "Options -Indexes\n" {
		t.Fatalf("ReadFile = %q, %v", b, err)
	}
	fi, err := os.Lstat(z.p("public/.htaccess"))
	if err != nil || fi.Mode() != 0o640 {
		t.Fatalf("mode sau WriteFileAtomic = %v, %v", fi.Mode(), err)
	}
	// Writing again replaces the file; no temp file stays behind.
	if err := WriteFileAtomic(a, "public/.htaccess", []byte("v2"), 0o600, uid, gid); err != nil {
		t.Fatal(err)
	}
	names, err := ReadDir(a, "public", 0)
	if err != nil || !slices.Equal(names, []string{".htaccess", "wp-content"}) {
		t.Fatalf("ReadDir = %v, %v", names, err)
	}
	if _, err := ReadDir(a, "public", 1); !errors.Is(err, ErrTooMany) {
		t.Errorf("ReadDir max 1 = %v, muốn ErrTooMany", err)
	}
	info, err := Lstat(a, "public")
	if err != nil || !info.Mode.IsDir() || info.UID != uint32(uid) {
		t.Fatalf("Lstat = %+v, %v", info, err)
	}
	if err := Symlink("../x", a, "public/l"); err != nil {
		t.Fatal(err)
	}
	if tg, err := Readlink(a, "public/l"); err != nil || tg != "../x" {
		t.Fatalf("Readlink = %q, %v", tg, err)
	}
	if _, err := Readlink(a, "public/.htaccess"); !errors.Is(err, unix.EINVAL) {
		t.Errorf("Readlink trên file thường = %v, muốn EINVAL", err)
	}
	if err := Symlink("y", a, "public/l"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("Symlink đè = %v, muốn EEXIST", err)
	}
	if err := Chmod(a, "public/.htaccess", 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Chown(a, "public/.htaccess", uid, gid); err != nil {
		t.Fatal(err)
	}
	if total, free, err := StatFS(a, "public"); err != nil || total == 0 || free > total {
		t.Fatalf("StatFS = %d, %d, %v", total, free, err)
	}
	if _, _, err := StatFS(a, "public/.htaccess"); err != nil {
		t.Fatalf("StatFS file: %v", err)
	}
	// Rename inside the zone, out of it and back in.
	if err := Rename(a, "public/.htaccess", a, "public/h2"); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(filepath.Dir(z.root), "out")
	if err := Rename(a, "public/h2", "", out); err != nil {
		t.Fatal(err)
	}
	if err := Rename("", out, a, "public/h3"); err != nil {
		t.Fatal(err)
	}
	if err := Rename("", "relative", a, "public/h4"); !errors.Is(err, ErrPath) {
		t.Errorf("Rename từ đường dẫn tương đối = %v, muốn ErrPath", err)
	}
	if err := Remove(a, "public/h3"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(a, "public/wp-content"); err != nil {
		t.Fatalf("Remove thư mục rỗng: %v", err)
	}
	if err := Remove(a, "public"); !errors.Is(err, unix.ENOTEMPTY) && !errors.Is(err, unix.EEXIST) {
		t.Errorf("Remove thư mục không rỗng = %v", err)
	}
	// Missing paths satisfy fs.ErrNotExist, whichever component is missing.
	for _, rel := range []string{"public/nope", "nope/x"} {
		_, err := ReadFile(a, rel, 0)
		wantIs(t, rel, err, fs.ErrNotExist)
	}
	_, err = ReadFile(filepath.Join(z.root, "nope"), "x", 0)
	wantIs(t, "neo không có", err, fs.ErrNotExist)
	// A regular file as a middle component keeps ENOTDIR (not a refusal).
	mustWrite(t, z.p("f"), "x", 0o640)
	_, err = ReadFile(a, "f/x", 0)
	wantIs(t, "cha là file", err, unix.ENOTDIR)
	if Refused(err) {
		t.Errorf("ENOTDIR bị coi là lời từ chối")
	}
	// MkdirAll on an existing tree leaves modes alone.
	mustMkdir(t, z.p("keep"), 0o711)
	if err := MkdirAll(a, "keep/new", 0o750, uid, gid); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(z.p("keep")); fi.Mode().Perm() != 0o711 {
		t.Errorf("MkdirAll đổi quyền thư mục có sẵn: %v", fi.Mode())
	}
}

// ---- B1: no symlink is ever followed ----

func TestCuaKhongDiTheoSymlink(t *testing.T) {
	t.Run("neo", func(t *testing.T) {
		for _, o := range allOps() {
			t.Run(o.name, func(t *testing.T) {
				z := newZone(t, false)
				v := victim(t, z)
				if err := os.Remove(z.anchor); err != nil {
					t.Fatal(err)
				}
				mustSymlink(t, v, z.anchor)
				before := snap(t, v)
				wantIs(t, o.name, o.fn(z.anchor, "x/v"), ErrSymlink)
				if snap(t, v) != before {
					t.Errorf("%s đổi thư mục nạn nhân qua neo là symlink", o.name)
				}
			})
		}
	})
	t.Run("giua", func(t *testing.T) {
		for _, o := range allOps() {
			t.Run(o.name, func(t *testing.T) {
				z := newZone(t, false)
				v := victim(t, z)
				mustSymlink(t, v, z.p("public"))
				before := snap(t, v)
				wantIs(t, o.name, o.fn(z.anchor, "public/x/v"), ErrSymlink)
				if snap(t, v) != before {
					t.Errorf("%s đổi thư mục nạn nhân qua thư mục cha là symlink", o.name)
				}
			})
		}
	})
	// Last name is a symlink: functions that would open it refuse; Lstat,
	// Readlink, Remove, Rename, WriteFileAtomic work on the link itself.
	refuse := map[string]bool{"ReadFile": true, "ReadDir": true, "MkdirAll": true, "Chown": true, "Chmod": true, "StatFS": true}
	for _, target := range []string{"v", "v/v"} {
		t.Run("cuoi->"+target, func(t *testing.T) {
			for _, o := range allOps() {
				t.Run(o.name, func(t *testing.T) {
					z := newZone(t, false)
					v := victim(t, z)
					mustMkdir(t, z.p("public"), 0o750)
					mustSymlink(t, filepath.Join(filepath.Dir(v), target), z.p("public/l"))
					before := snap(t, v)
					err := o.fn(z.anchor, "public/l")
					switch {
					case refuse[o.name]:
						wantIs(t, o.name, err, ErrSymlink)
					case o.name == "Symlink":
						wantIs(t, o.name, err, fs.ErrExist)
					case err != nil:
						t.Errorf("%s trên chính symlink: %v", o.name, err)
					}
					if snap(t, v) != before {
						t.Errorf("%s đổi thư mục nạn nhân qua tên cuối là symlink", o.name)
					}
				})
			}
		})
	}
}

// ---- B2: only regular files of the site user, capped, never hanging ----

func TestCuaChiDocFileThuong(t *testing.T) {
	z := newZone(t, true)
	mustMkdir(t, z.p("d"), 0o750)
	mustFIFO(t, z.p("fifo"))
	mustSocket(t, z.p("sock"))
	for _, rel := range []string{"d", "fifo", "sock"} {
		within(t, 2*time.Second, rel, func() {
			_, err := ReadFile(z.anchor, rel, 0)
			wantIs(t, rel, err, ErrNotRegular)
		})
	}
	// Chown, Chmod, StatFS do not open a FIFO or socket either.
	for _, rel := range []string{"fifo", "sock"} {
		wantIs(t, "Chmod "+rel, Chmod(z.anchor, rel, 0o600), ErrNotRegular)
		wantIs(t, "Chown "+rel, Chown(z.anchor, rel, os.Geteuid(), os.Getegid()), ErrNotRegular)
		if _, _, err := StatFS(z.anchor, rel); err != nil {
			t.Errorf("StatFS %s: %v", rel, err)
		}
	}
	t.Run("thiet-bi", func(t *testing.T) {
		needRoot(t)
		var st unix.Stat_t
		if err := unix.Stat("/dev/zero", &st); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mknod(z.p("zero"), unix.S_IFCHR|0o644, int(st.Rdev)); err != nil {
			t.Fatal(err)
		}
		within(t, 2*time.Second, "zero", func() {
			_, err := ReadFile(z.anchor, "zero", 0)
			wantIs(t, "zero", err, ErrNotRegular)
			wantIs(t, "Chmod zero", Chmod(z.anchor, "zero", 0o600), ErrNotRegular)
		})
	})
}

func TestCuaTranCo(t *testing.T) {
	z := newZone(t, false)
	big := z.p("big")
	mustWrite(t, big, "", 0o640)
	if err := os.Truncate(big, 2<<30); err != nil {
		t.Fatal(err)
	}
	within(t, 2*time.Second, "file thưa 2 GiB", func() {
		_, err := ReadFile(z.anchor, "big", 0)
		wantIs(t, "2 GiB", err, ErrTooLarge)
	})
	mustWrite(t, z.p("ten"), "0123456789", 0o640)
	if b, err := ReadFile(z.anchor, "ten", 10); err != nil || len(b) != 10 {
		t.Errorf("đúng trần: %q, %v", b, err)
	}
	_, err := ReadFile(z.anchor, "ten", 9)
	wantIs(t, "quá trần một byte", err, ErrTooLarge)

	// A file that grows after fstat: readLimit alone must stop at max+1.
	f, err := os.Open(z.p("ten"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := readLimit(int(f.Fd()), 4); !errors.Is(err, ErrTooLarge) {
		t.Errorf("readLimit file lớn dần = %v, muốn ErrTooLarge", err)
	}
}

func TestCuaKhongTreo(t *testing.T) {
	z := newZone(t, false)
	mustFIFO(t, z.p("fifo"))
	within(t, 2*time.Second, "ReadFile FIFO", func() {
		_, err := ReadFile(z.anchor, "fifo", 0)
		wantIs(t, "ReadFile FIFO", err, ErrNotRegular)
	})
	// A FIFO swapped in after fstatat: the O_NONBLOCK open returns at once and
	// the fstat on the fd refuses it.
	within(t, 2*time.Second, "openLeaf FIFO", func() {
		p, err := walk(z.anchor, "fifo")
		if err != nil {
			t.Error(err)
			return
		}
		defer closeFD(p.fd)
		fd, err := openLeaf(p.fd, p.name)
		if err != nil {
			t.Error(err)
			return
		}
		defer closeFD(fd)
		if err := checkRead(fd, &p.anchor, MaxFile); !errors.Is(err, ErrNotRegular) {
			t.Errorf("checkRead FIFO = %v", err)
		}
	})
	// A FIFO as a middle component.
	within(t, 2*time.Second, "FIFO ở giữa", func() {
		err := MkdirAll(z.anchor, "fifo/x", 0o750, os.Geteuid(), os.Getegid())
		wantIs(t, "MkdirAll qua FIFO", err, unix.ENOTDIR)
		_, err = ReadDir(z.anchor, "fifo", 0)
		wantIs(t, "ReadDir FIFO", err, unix.ENOTDIR)
	})
}

func TestCuaChuFile(t *testing.T) {
	needRoot(t)
	const nobody = 65534
	z := newZone(t, false)
	mustMkdir(t, z.p("public"), 0o750)
	for _, p := range []string{z.anchor, z.p("public")} {
		if err := os.Chown(p, nobody, nobody); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, z.p("public/root"), "root", 0o644)
	_, err := ReadFile(z.anchor, "public/root", 0)
	wantIs(t, "file của root trong neo của nobody", err, ErrOwner)

	mustWrite(t, z.p("public/mine"), "mine", 0o644)
	if err := os.Chown(z.p("public/mine"), nobody, nobody); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadFile(z.anchor, "public/mine", 0); err != nil || string(b) != "mine" {
		t.Errorf("file của chính site: %q, %v", b, err)
	}

	secret := filepath.Join(filepath.Dir(z.root), "secret")
	mustWrite(t, secret, "SECRET", 0o600)
	if err := os.Link(secret, z.p("public/hl")); err != nil {
		t.Fatal(err)
	}
	_, err = ReadFile(z.anchor, "public/hl", 0)
	wantIs(t, "hardlink tới file của root", err, ErrOwner)

	if err := os.Chown(z.anchor, 0, 0); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadFile(z.anchor, "public/root", 0); err != nil || string(b) != "root" {
		t.Errorf("neo của root bỏ luật chủ: %q, %v", b, err)
	}
}

// ---- B3: swapping while reading never yields bytes from outside ----

func TestCuaDua(t *testing.T) {
	const secretData = "SECRET-BYTES-OUTSIDE"
	z := newZone(t, false)
	pub := z.p("public")
	mustMkdir(t, pub, 0o750)
	secret := filepath.Join(filepath.Dir(z.root), "secret")
	mustWrite(t, secret, secretData, 0o600)
	h := filepath.Join(pub, ".htaccess")
	mustWrite(t, h, "site", 0o640)

	read := func(t *testing.T) {
		ok, refused := 0, 0
		for range 1000 {
			b, err := ReadFile(z.anchor, "public/.htaccess", 0)
			if bytes.Contains(b, []byte("SECRET")) {
				t.Fatalf("đọc ra byte của bí mật: %q", b)
			}
			if err == nil {
				ok++
			} else {
				refused++
			}
		}
		t.Logf("đọc được %d, từ chối %d", ok, refused)
	}

	t.Run("tra-file", func(t *testing.T) {
		stop := race(func() {
			tl, tf := filepath.Join(pub, ".l"), filepath.Join(pub, ".f")
			_ = os.Symlink(secret, tl)
			_ = os.Rename(tl, h)
			_ = os.WriteFile(tf, []byte("site"), 0o640)
			_ = os.Rename(tf, h)
		})
		defer stop()
		read(t)
	})
	t.Run("tra-thu-muc-cha", func(t *testing.T) {
		vd := filepath.Join(filepath.Dir(z.root), "vd")
		mustMkdir(t, vd, 0o755)
		mustWrite(t, filepath.Join(vd, ".htaccess"), secretData, 0o644)
		stop := race(func() {
			_ = os.Rename(pub, pub+".real")
			_ = os.Symlink(vd, pub)
			_ = os.Remove(pub)
			_ = os.Rename(pub+".real", pub)
		})
		defer stop()
		read(t)
	})
}

// race runs swap in a loop on its own goroutine until the returned stop is called.
func race(swap func()) (stop func()) {
	var halt atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !halt.Load() {
			swap()
		}
	}()
	return func() { halt.Store(true); <-done }
}

// ---- B4: root changes nothing outside the anchor, even under a race ----

func TestCuaKhongRaNgoai(t *testing.T) {
	for _, o := range writeOps() {
		t.Run(o.name, func(t *testing.T) {
			z := newZone(t, false)
			v := victim(t, z)
			mustSymlink(t, v, z.p("public"))
			before := snap(t, v)
			for _, rel := range []string{"public/v", "public/x", "public/x/v", "public/new", "public/x/new"} {
				wantIs(t, o.name+" "+rel, o.fn(z.anchor, rel), ErrSymlink)
			}
			if snap(t, v) != before {
				t.Errorf("%s đổi thư mục nạn nhân", o.name)
			}
		})
	}
}

func TestCuaDuaChmod(t *testing.T) {
	z := newZone(t, false)
	pub := z.p("public")
	mustMkdir(t, pub, 0o750)
	x := filepath.Join(pub, "x")
	mustMkdir(t, x, 0o750)
	v := victim(t, z)
	before := snap(t, v)
	stop := race(func() {
		_ = os.Rename(x, x+".real")
		_ = os.Symlink(v, x)
		_ = os.Remove(x)
		_ = os.Rename(x+".real", x)
	})
	for range 1000 {
		_ = Chmod(z.anchor, "public/x", fs.ModeSetgid|0o750)
	}
	stop()
	if snap(t, v) != before {
		t.Errorf("Chmod đổi quyền thư mục nạn nhân khi bị đua")
	}
}

func TestCuaDuaGhi(t *testing.T) {
	uid, gid := os.Geteuid(), os.Getegid()
	n := 0
	cases := []struct {
		name string
		fn   func(a string) error
	}{
		{"Chmod", func(a string) error { return Chmod(a, "public/x/v", 0o600) }},
		{"Chown", func(a string) error { return Chown(a, "public/x/v", uid, otherGID) }},
		{"WriteFileAtomic", func(a string) error {
			return WriteFileAtomic(a, "public/x/v", []byte("attack"), 0o600, uid, gid)
		}},
		{"MkdirAll", func(a string) error { n++; return MkdirAll(a, "public/x/n"+itoa(uint32(n)), 0o700, uid, gid) }},
		{"Remove", func(a string) error { return Remove(a, "public/x/v") }},
		{"Rename", func(a string) error { return Rename(a, "public/x/v", a, "public/x/w") }},
		{"Symlink", func(a string) error { n++; return Symlink("t", a, "public/x/s"+itoa(uint32(n))) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "Chown" && otherGID < 0 {
				t.Skip("user chạy test không có nhóm thứ hai để đổi sang")
			}
			z := newZone(t, false)
			pub, x := z.p("public"), z.p("public/x")
			mustMkdir(t, pub, 0o750)
			mustMkdir(t, x, 0o750)
			mustWrite(t, filepath.Join(x, "v"), "site", 0o644)
			v := victim(t, z)
			before := snap(t, v)
			stop := race(func() {
				for _, d := range []string{pub, x} {
					_ = os.Rename(d, d+".real")
					_ = os.Symlink(v, d)
					_ = os.Remove(d)
					_ = os.Rename(d+".real", d)
				}
				// Put the target file back if the operation removed or moved it.
				_ = os.Rename(filepath.Join(x, "w"), filepath.Join(x, "v"))
			})
			for range 1000 {
				_ = c.fn(z.anchor)
			}
			stop()
			if after := snap(t, v); after != before {
				t.Errorf("%s đổi thư mục nạn nhân khi bị đua:\ntrước:\n%s\nsau:\n%s", c.name, before, after)
			}
		})
	}
}

// otherGID is a group the test user can chown to that differs from its
// primary group (so a chown that escaped would show), or -1.
var otherGID = func() int {
	if os.Geteuid() == 0 {
		return 65534
	}
	gs, _ := os.Getgroups()
	for _, g := range gs {
		if g != os.Getegid() {
			return g
		}
	}
	return -1
}()

// ---- Names, modes, ownership ----

func TestCuaHardlink(t *testing.T) {
	z := newZone(t, false)
	mustWrite(t, z.p("f"), "mine", 0o640)
	if err := os.Link(z.p("f"), z.p("g")); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadFile(z.anchor, "g", 0); err != nil || string(b) != "mine" {
		t.Errorf("đọc file của chính mình có 2 tên: %q, %v", b, err)
	}
	wantIs(t, "Chown", Chown(z.anchor, "g", os.Geteuid(), os.Getegid()), ErrLinked)
	wantIs(t, "Chmod", Chmod(z.anchor, "f", 0o600), ErrLinked)
	if fi, _ := os.Lstat(z.p("f")); fi.Mode().Perm() != 0o640 {
		t.Errorf("Chmod qua hardlink vẫn đổi quyền: %v", fi.Mode())
	}
}

func TestCuaTenLa(t *testing.T) {
	z := newZone(t, false)
	uid, gid := os.Geteuid(), os.Getegid()
	mustMkdir(t, z.p("public"), 0o750)
	for _, name := range []string{"a\nb", "-rf", strings.Repeat("x", 255)} {
		rel := "public/" + name
		if err := WriteFileAtomic(z.anchor, rel, []byte("ok"), 0o640, uid, gid); err != nil {
			t.Fatalf("WriteFileAtomic %q: %v", name, err)
		}
		if b, err := ReadFile(z.anchor, rel, 0); err != nil || string(b) != "ok" {
			t.Errorf("ReadFile %q: %q, %v", name, b, err)
		}
		if err := Chmod(z.anchor, rel, 0o600); err != nil {
			t.Errorf("Chmod %q: %v", name, err)
		}
		if err := Remove(z.anchor, rel); err != nil {
			t.Errorf("Remove %q: %v", name, err)
		}
	}
	deep := strings.Repeat("d/", MaxDepth) + "x"
	for _, rel := range []string{"", ".", "..", "public/..", "public/../x", "public/./x", "/public", "public//x", "public/", "a\x00b", deep} {
		for _, o := range allOps() {
			wantIs(t, o.name+" "+strconvQ(rel), o.fn(z.anchor, rel), ErrPath)
		}
	}
	// Exactly MaxDepth components is allowed (it just does not exist).
	_, err := Lstat(z.anchor, strings.Repeat("d/", MaxDepth-1)+"x")
	wantIs(t, "MaxDepth", err, fs.ErrNotExist)
	for _, a := range []string{"", "rel/a", z.anchor + "/", "/", z.root + "/.."} {
		_, err := Lstat(a, "x")
		wantIs(t, "neo "+strconvQ(a), err, ErrPath)
	}
}

func strconvQ(s string) string {
	if len(s) > 20 {
		s = s[:20] + "…"
	}
	return "\"" + strings.ReplaceAll(s, "\x00", `\0`) + "\""
}

func TestCuaGiuSetgid(t *testing.T) {
	z := newZone(t, false)
	want := fs.ModeDir | fs.ModeSetgid | 0o750
	mustMkdir(t, z.p("public"), 0o700)
	if err := Chmod(z.anchor, "public", fs.ModeSetgid|0o750); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(z.anchor, "public/n/m", fs.ModeSetgid|0o750, os.Geteuid(), os.Getegid()); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"public", "public/n", "public/n/m"} {
		fi, err := os.Lstat(z.p(rel))
		if err != nil || fi.Mode() != want {
			t.Errorf("%s: os.Lstat = %v, %v; muốn %v", rel, fi.Mode(), err, want)
		}
		info, err := Lstat(z.anchor, rel)
		if err != nil || info.Mode != want {
			t.Errorf("%s: sitefs.Lstat = %v, %v; muốn %v", rel, info.Mode, err, want)
		}
	}
}

func TestCuaModeNhuOS(t *testing.T) {
	z := newZone(t, true)
	mustMkdir(t, z.p("d"), 0o750)
	// /tmp's group may not be ours on macOS, so setgid may not stick there;
	// compare with whatever os.Lstat says, after trying to set it.
	_ = os.Chmod(z.p("d"), fs.ModeSetgid|0o750)
	mustWrite(t, z.p("f"), "data", 0o640)
	mustSymlink(t, "f", z.p("l"))
	mustFIFO(t, z.p("p"))
	mustSocket(t, z.p("s"))
	for _, rel := range []string{"d", "f", "l", "p", "s"} {
		fi, err := os.Lstat(z.p(rel))
		if err != nil {
			t.Fatal(err)
		}
		info, err := Lstat(z.anchor, rel)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		st := fi.Sys().(*syscall.Stat_t)
		if info.Mode != fi.Mode() || info.UID != st.Uid || info.GID != st.Gid || info.Nlink != uint64(st.Nlink) {
			t.Errorf("%s: sitefs %+v, os %v uid %d gid %d nlink %d", rel, info, fi.Mode(), st.Uid, st.Gid, st.Nlink)
		}
		if !fi.IsDir() && info.Size != fi.Size() {
			t.Errorf("%s: Size %d, os %d", rel, info.Size, fi.Size())
		}
	}
	// And the other direction: toKernel(fs.ModeSetgid|0o750) is 02750.
	if k := toKernel(fs.ModeSetgid | fs.ModeSetuid | fs.ModeSticky | 0o750); k != 0o7750 {
		t.Errorf("toKernel = %#o, muốn 07750", k)
	}
}

func TestCuaSymlinkDoiChu(t *testing.T) {
	z := newZone(t, false)
	mustMkdir(t, z.p("public"), 0o750)
	if os.Geteuid() == 0 {
		for _, p := range []string{z.anchor, z.p("public")} {
			if err := os.Chown(p, 65534, 65534); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := Symlink("/etc/passwd", z.anchor, "public/l"); err != nil {
		t.Fatal(err)
	}
	var an, ln unix.Stat_t
	if err := unix.Lstat(z.anchor, &an); err != nil {
		t.Fatal(err)
	}
	if err := unix.Lstat(z.p("public/l"), &ln); err != nil {
		t.Fatal(err)
	}
	if ln.Uid != an.Uid || ln.Gid != an.Gid {
		t.Errorf("symlink thuộc %d:%d, neo thuộc %d:%d", ln.Uid, ln.Gid, an.Uid, an.Gid)
	}
}

func TestCuaGocKhongAnToan(t *testing.T) {
	for _, perm := range []fs.FileMode{0o777, 0o775, 0o757} {
		z := newZone(t, false)
		mustMkdir(t, z.p("public"), 0o750)
		mustWrite(t, z.p("public/f"), "x", 0o640)
		if err := os.Chmod(z.root, perm); err != nil {
			t.Fatal(err)
		}
		for _, o := range allOps() {
			wantIs(t, o.name+" gốc "+perm.String(), o.fn(z.anchor, "public/f"), ErrRootUnsafe)
		}
	}
	if os.Geteuid() == 0 {
		z := newZone(t, false)
		if err := os.Chown(z.root, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		_, err := Lstat(z.anchor, "x")
		wantIs(t, "gốc của nobody", err, ErrRootUnsafe)
	}
}

// ---- B14: errors never carry file content or symlink targets ----

func TestLoiCuaKhongLoNoiDung(t *testing.T) {
	const content, target = "NOI-DUNG-BI-MAT", "/etc/DICH-BI-MAT"
	z := newZone(t, false)
	mustMkdir(t, z.p("public"), 0o750)
	mustSymlink(t, target, z.p("public/l"))
	mustSymlink(t, target, z.p("dl"))
	mustWrite(t, z.p("public/big"), strings.Repeat(content, 10), 0o640)
	mustWrite(t, z.p("public/h1"), content, 0o640)
	if err := os.Link(z.p("public/h1"), z.p("public/h2")); err != nil {
		t.Fatal(err)
	}
	var errs []error
	for _, o := range allOps() {
		for _, rel := range []string{"public/l", "dl/x", "public/big", "public/h2"} {
			errs = append(errs, o.fn(z.anchor, rel))
		}
	}
	_, err := ReadFile(z.anchor, "public/big", 5)
	errs = append(errs, err)
	for _, err := range errs {
		if err == nil {
			continue
		}
		msg := err.Error()
		if strings.Contains(msg, content) || strings.Contains(msg, "DICH-BI-MAT") {
			t.Errorf("lỗi lộ nội dung hay đích symlink: %s", msg)
		}
	}
}

// ---- B17: no fd leaks ----

func TestCuaKhongRoFD(t *testing.T) {
	z := newZone(t, false)
	uid, gid := os.Geteuid(), os.Getegid()
	mustMkdir(t, z.p("d"), 0o750)
	mustMkdir(t, z.p("full"), 0o750)
	mustWrite(t, z.p("full/f"), "x", 0o640)
	mustWrite(t, z.p("f"), "data", 0o640)
	mustSymlink(t, "f", z.p("s"))
	mustSymlink(t, z.p("d"), z.p("lnk"))
	mustFIFO(t, z.p("fifo"))
	a := z.anchor
	good := []func() error{
		func() error { _, err := ReadFile(a, "f", 0); return err },
		func() error { _, err := Lstat(a, "f"); return err },
		func() error { _, err := ReadDir(a, "d", 0); return err },
		func() error { return WriteFileAtomic(a, "w", []byte("x"), 0o640, uid, gid) },
		func() error { return MkdirAll(a, "d/sub", 0o750, uid, gid) },
		func() error {
			if err := Symlink("t", a, "s2"); err != nil {
				return err
			}
			return Remove(a, "s2")
		},
		func() error {
			if err := Rename(a, "w", a, "w2"); err != nil {
				return err
			}
			return Rename(a, "w2", a, "w")
		},
		func() error { _, err := Readlink(a, "s"); return err },
		func() error { return Chown(a, "f", uid, gid) },
		func() error { return Chmod(a, "f", 0o640) },
		func() error { _, _, err := StatFS(a, "d"); return err },
	}
	countFD := func() int {
		ents, err := os.ReadDir("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(ents)
	}
	// Warm up: the runtime opens some fds lazily.
	for _, fn := range good {
		if err := fn(); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range allOps() {
		for _, rel := range []string{"nope/x", "lnk/x", "full", "fifo", "s"} {
			_ = o.fn(a, rel)
		}
	}
	n := 10000
	if testing.Short() {
		n = 500
	}
	before := countFD()
	for i, fn := range good {
		for range n {
			if err := fn(); err != nil {
				t.Fatalf("hàm %d: %v", i, err)
			}
		}
	}
	for _, o := range allOps() {
		for _, rel := range []string{"nope/x", "lnk/x", "full/f/x", "fifo", "lnk"} {
			for range n / 5 {
				_ = o.fn(a, rel)
			}
		}
	}
	if after := countFD(); after != before {
		t.Errorf("số fd trước %d, sau %d", before, after)
	}
}
