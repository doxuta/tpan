package sitefs

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// zone is a site root and one anchor below it, both owned by the test user.
type zone struct {
	root, anchor string
}

// newZone builds <tmp>/www (0755) and <tmp>/www/a (0750). short=true puts it
// under /tmp so a Unix socket path fits (104 bytes on macOS).
func newZone(t *testing.T, short bool) zone {
	t.Helper()
	base := t.TempDir()
	if short {
		d, err := os.MkdirTemp("/tmp", "c")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(d) })
		base = d
	}
	z := zone{root: filepath.Join(base, "www")}
	z.anchor = filepath.Join(z.root, "a")
	mustMkdir(t, z.root, 0o755)
	mustMkdir(t, z.anchor, 0o750)
	// macOS gives a new directory its parent's group (wheel under /tmp), and a
	// non-root user cannot set setgid or chown to a group it is not in. Use
	// our own group so the tree below behaves like a site tree.
	for _, p := range []string{z.root, z.anchor} {
		if err := os.Chown(p, os.Geteuid(), os.Getegid()); err != nil {
			t.Fatal(err)
		}
	}
	return z
}

func (z zone) p(rel string) string { return filepath.Join(z.anchor, rel) }

func mustMkdir(t *testing.T, p string, perm fs.FileMode) {
	t.Helper()
	if err := os.Mkdir(p, perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, perm); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, data string, perm fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, perm); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func mustFIFO(t *testing.T, p string) {
	t.Helper()
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSocket(t *testing.T, p string) {
	t.Helper()
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the socket file after Close.
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = l.Close()
}

// op is one function of the gate with fixed extra arguments, so a test can
// run "every function" on the same (anchor, rel).
type op struct {
	name string
	fn   func(anchor, rel string) error
}

func allOps() []op {
	uid, gid := os.Geteuid(), os.Getegid()
	return []op{
		{"ReadFile", func(a, r string) error { _, err := ReadFile(a, r, 0); return err }},
		{"Lstat", func(a, r string) error { _, err := Lstat(a, r); return err }},
		{"ReadDir", func(a, r string) error { _, err := ReadDir(a, r, 0); return err }},
		{"WriteFileAtomic", func(a, r string) error { return WriteFileAtomic(a, r, []byte("attack"), 0o640, uid, gid) }},
		{"MkdirAll", func(a, r string) error { return MkdirAll(a, r, 0o750, uid, gid) }},
		{"Remove", func(a, r string) error { return Remove(a, r) }},
		{"Rename", func(a, r string) error { return Rename(a, r, a, r+".moi") }},
		{"Symlink", func(a, r string) error { return Symlink("t", a, r) }},
		{"Readlink", func(a, r string) error { _, err := Readlink(a, r); return err }},
		{"Chown", func(a, r string) error { return Chown(a, r, uid, gid) }},
		{"Chmod", func(a, r string) error { return Chmod(a, r, 0o700) }},
		{"StatFS", func(a, r string) error { _, _, err := StatFS(a, r); return err }},
	}
}

// writeOps are the functions that change something (B4).
func writeOps() []op {
	var out []op
	for _, o := range allOps() {
		switch o.name {
		case "WriteFileAtomic", "MkdirAll", "Remove", "Rename", "Symlink", "Chown", "Chmod":
			out = append(out, o)
		}
	}
	return out
}

// snap records every entry under dir: mode, owner, content or link target.
func snap(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st := fi.Sys().(*syscall.Stat_t)
		line := strings.TrimPrefix(p, dir) + " " + fi.Mode().String() + " " + itoa(st.Uid) + ":" + itoa(st.Gid)
		switch {
		case fi.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			line += " " + string(b)
		case fi.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			line += " -> " + l
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func itoa(u uint32) string {
	var b [10]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
		if u == 0 {
			return string(b[i:])
		}
	}
}

// victim builds a directory outside the anchor that no site operation may
// touch: v (file), x/v (file), mode 0755 and 0644.
func victim(t *testing.T, z zone) string {
	t.Helper()
	v := filepath.Join(filepath.Dir(z.root), "victim")
	mustMkdir(t, v, 0o755)
	mustWrite(t, filepath.Join(v, "v"), "victim", 0o644)
	mustMkdir(t, filepath.Join(v, "x"), 0o755)
	mustWrite(t, filepath.Join(v, "x", "v"), "victim-x", 0o644)
	return v
}

// within fails the test when fn takes longer than d (C9: never hangs).
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s: chưa trả lời sau %v", what, d)
	}
}

func wantIs(t *testing.T, what string, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("%s: lỗi %v, muốn %v", what, err, target)
	}
	var e *Error
	if err != nil && !errors.As(err, &e) {
		t.Errorf("%s: lỗi %T không phải *sitefs.Error", what, err)
	}
}

func needRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("cần root: chạy trong máy ảo bằng make m28-test-vm")
	}
}
