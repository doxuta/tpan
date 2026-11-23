package system

// Test của phần M18: Streamer (OpenRead, CreateFile) ở cả hai bản cài đặt, và
// khoá có tên (FileLock, MemLock).

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"testing"
	"time"
)

// streamCase chạy cùng một bộ khẳng định trên một System: dir là thư mục có
// sẵn, o là chủ đặt được không cần root.
func streamCase(t *testing.T, s System, dir string, o Owner) {
	t.Helper()
	p := filepath.Join(dir, "a.bin")
	w, err := s.CreateFile(p, 0o640, o)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := io.WriteString(w, "xin "); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "chào"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	fi, err := s.Stat(p)
	if err != nil || fi.Mode != 0o640 || fi.Owner != o {
		t.Fatalf("Stat sau CreateFile = %+v, %v (muốn 0640, chủ %+v)", fi, err, o)
	}
	r, size, err := s.OpenRead(p)
	if err != nil {
		t.Fatalf("OpenRead: %v", err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(got) != "xin chào" || size != int64(len("xin chào")) {
		t.Fatalf("OpenRead đọc %q (cỡ %d), %v", got, size, err)
	}

	// Đã có thì không ghi đè.
	if _, err := s.CreateFile(p, 0o600, o); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("CreateFile lên file đã có: %v (muốn fs.ErrExist)", err)
	}
	// Symlink: CreateFile không đi theo (lỗi đã có), OpenRead từ chối.
	link := filepath.Join(dir, "lnk")
	if err := s.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFile(link, 0o600, o); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("CreateFile lên symlink: %v (muốn fs.ErrExist)", err)
	}
	if r, _, err := s.OpenRead(link); err == nil {
		_ = r.Close()
		t.Fatal("OpenRead đi theo symlink")
	}
	// Thư mục và file không có.
	if r, _, err := s.OpenRead(dir); err == nil {
		_ = r.Close()
		t.Fatal("OpenRead mở được thư mục")
	}
	if _, _, err := s.OpenRead(filepath.Join(dir, "khong-co")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("OpenRead file không có: %v (muốn fs.ErrNotExist)", err)
	}
	// Thư mục cha phải có sẵn.
	if _, err := s.CreateFile(filepath.Join(dir, "khong", "b"), 0o600, o); err == nil {
		t.Fatal("CreateFile khi thiếu thư mục cha phải lỗi")
	}
}

func TestLocalStreamer(t *testing.T) {
	streamCase(t, NewLocal(), t.TempDir(), currentOwner(t))
}

func TestFakeStreamer(t *testing.T) {
	f := NewFake()
	f.SetDir("/d", 0o755, Root)
	streamCase(t, f, "/d", Owner{User: "u", Group: "g"})

	// Fail("write") làm CreateFile lỗi, Fail("open") làm OpenRead lỗi.
	boom := errors.New("boom")
	f.Fail("write", "/d/c", boom)
	if _, err := f.CreateFile("/d/c", 0o600, Root); !errors.Is(err, boom) {
		t.Fatalf("Fail write: %v", err)
	}
	f.Fail("open", "/d/a.bin", boom)
	if _, _, err := f.OpenRead("/d/a.bin"); !errors.Is(err, boom) {
		t.Fatalf("Fail open: %v", err)
	}
	// Nội dung chỉ vào hệ thống file giả lúc Close; trước đó file rỗng.
	w, err := f.CreateFile("/d/e", 0o600, Root)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "abc")
	if fi, _ := f.Stat("/d/e"); fi.Size != 0 {
		t.Fatalf("trước Close cỡ là %d", fi.Size)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if b, _ := f.ReadFile("/d/e"); string(b) != "abc" {
		t.Fatalf("sau Close đọc %q", b)
	}
	if err := w.Close(); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Close lần hai: %v", err)
	}
}

func lockerCase(t *testing.T, a, b Locker) {
	t.Helper()
	ctx := context.Background()
	unlock, ok, err := a.Acquire(ctx, 0)
	if err != nil || !ok {
		t.Fatalf("lấy khoá lần đầu: ok=%v err=%v", ok, err)
	}
	if _, ok, err := b.Acquire(ctx, 0); err != nil || ok {
		t.Fatalf("khoá đang giữ mà lấy được: ok=%v err=%v", ok, err)
	}
	start := time.Now()
	if _, ok, err := b.Acquire(ctx, 250*time.Millisecond); err != nil || ok {
		t.Fatalf("chờ hết giờ: ok=%v err=%v", ok, err)
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Fatal("không chờ")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := b.Acquire(cctx, time.Second); err == nil {
		t.Fatal("ctx đã huỷ mà không lỗi")
	}
	unlock()
	unlock() // gọi lần hai không sao
	u2, ok, err := b.Acquire(ctx, 0)
	if err != nil || !ok {
		t.Fatalf("sau khi nhả: ok=%v err=%v", ok, err)
	}
	u2()
}

func TestFileLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "remote.lock")
	lockerCase(t, FileLock{Path: p}, FileLock{Path: p})
}

func TestMemLock(t *testing.T) {
	l := NewMemLock()
	lockerCase(t, l, l)
}
