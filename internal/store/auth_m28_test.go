package store

// Test của M28 đợt 2 cho phần đăng nhập: trần argon2id (P2, bất biến E9) và
// phép trừ có điều kiện của bộ đếm sai (P1, S9).

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countArgon thay argonKey bằng bản đếm số lần chạy cùng lúc (giữ mỗi lần
// một chút để các lượt kịp chồng lên nhau) và trả con trỏ tới số lớn nhất.
func countArgon(t *testing.T) *atomic.Int32 {
	t.Helper()
	smallArgon(t)
	var cur, peak atomic.Int32
	old := argonKey
	argonKey = func(pw, salt []byte, tm, mem uint32, th uint8, n uint32) []byte {
		c := cur.Add(1)
		for {
			p := peak.Load()
			if c <= p || peak.CompareAndSwap(p, c) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		defer cur.Add(-1)
		return old(pw, salt, tm, mem, th, n)
	}
	t.Cleanup(func() { argonKey = old })
	return &peak
}

// E9: trong tiến trình không bao giờ có quá argonSlots lần argon2id cùng lúc,
// dù gọi từ HashPassword, HashPasswordWait hay VerifyPassword.
func TestArgonKhongQuaHaiCho(t *testing.T) {
	peak := countArgon(t)
	oldWait := argonWait
	argonWait = time.Minute // không lượt nào được hết hạn chờ trong test này
	t.Cleanup(func() { argonWait = oldWait })

	h, err := HashPasswordWait("mat-khau")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 30)
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _, err := HashPassword("x"); errs <- err }()
		go func() { defer wg.Done(); _, err := HashPasswordWait("x"); errs <- err }()
		go func() {
			defer wg.Done()
			ok, err := VerifyPassword(h, "mat-khau")
			if err == nil && !ok {
				err = errors.New("mật khẩu đúng mà VerifyPassword trả false")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if p := peak.Load(); p != argonSlots {
		t.Fatalf("số argon2id chạy cùng lúc lớn nhất = %d, muốn đúng %d", p, argonSlots)
	}
}

// Hết chỗ quá argonWait: HashPassword và VerifyPassword trả ErrPasswordBusy
// (không chạy argon2), HashPasswordWait chờ tới khi có chỗ.
func TestArgonBan(t *testing.T) {
	smallArgon(t)
	h, err := HashPassword("mat-khau")
	if err != nil {
		t.Fatal(err)
	}
	oldWait := argonWait
	argonWait = 20 * time.Millisecond
	t.Cleanup(func() { argonWait = oldWait })
	for range argonSlots {
		argonSem <- struct{}{}
	}
	release := func() {
		for range argonSlots {
			<-argonSem
		}
	}
	if _, err := HashPassword("x"); !errors.Is(err, ErrPasswordBusy) {
		release()
		t.Fatalf("HashPassword khi hết chỗ: %v, muốn ErrPasswordBusy", err)
	}
	if ok, err := VerifyPassword(h, "mat-khau"); ok || !errors.Is(err, ErrPasswordBusy) {
		release()
		t.Fatalf("VerifyPassword khi hết chỗ: %v %v, muốn ErrPasswordBusy", ok, err)
	}
	// Hash hỏng bị từ chối trước khi xin chỗ: lỗi của hash, không phải bận.
	if _, err := VerifyPassword("hong", "x"); err == nil || errors.Is(err, ErrPasswordBusy) {
		release()
		t.Fatalf("hash hỏng: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := HashPasswordWait("x"); done <- err }()
	select {
	case err := <-done:
		release()
		t.Fatalf("HashPasswordWait không chờ: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// S9 phía DB: ForgetLoginFailures chỉ trừ khi first_at còn trùng và chưa khoá,
// không trừ dưới 0, về 0 thì xoá dòng.
func TestForgetLoginFailures(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	const key = "198.51.100.7"
	win, lock := 15*time.Minute, 15*time.Minute
	rec := func(at time.Time) LoginFailure {
		t.Helper()
		f, err := s.RecordLoginFailure(ctx, key, at, 5, win, lock)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	count := func() int {
		t.Helper()
		var n int
		err := s.db.QueryRowContext(ctx, "SELECT failures FROM login_failures WHERE ip = ?", key).Scan(&n)
		if err != nil {
			return -1 // không có dòng
		}
		return n
	}

	var f LoginFailure
	for range 3 {
		f = rec(t0)
	}
	// first_at lệch (dòng đã đếm lại từ cửa sổ khác): không trừ gì.
	if err := s.ForgetLoginFailures(ctx, key, 2, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 3 {
		t.Fatalf("first_at lệch mà vẫn trừ: còn %d", n)
	}
	if err := s.ForgetLoginFailures(ctx, key, 2, f.FirstAt); err != nil || count() != 1 {
		t.Fatalf("trừ 2 từ 3: còn %d, %v", count(), err)
	}
	// Trừ quá số đang có: về 0 và xoá dòng, không âm.
	if err := s.ForgetLoginFailures(ctx, key, 9, f.FirstAt); err != nil || count() != -1 {
		t.Fatalf("trừ quá số: còn %d, %v; muốn xoá dòng", count(), err)
	}
	// Đang khoá: không trừ.
	for range 5 {
		f = rec(t0)
	}
	if !f.Locked {
		t.Fatalf("lần sai 5 phải khoá: %+v", f)
	}
	if err := s.ForgetLoginFailures(ctx, key, 4, f.FirstAt); err != nil || count() != 5 {
		t.Fatalf("đang khoá mà vẫn trừ: còn %d, %v", count(), err)
	}
	if _, l, _ := s.LoginLockedUntil(ctx, key, t0); !l {
		t.Fatal("trừ lúc đang khoá đã gỡ khoá")
	}
}

func TestClearAllLoginFailures(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, k := range []string{"192.0.2.1", "uid:1001", "2001:db8:1:2::/64"} {
		if _, err := s.RecordLoginFailure(ctx, k, t0, 1, time.Hour, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.ClearAllLoginFailures(ctx)
	if err != nil || n != 3 {
		t.Fatalf("gỡ hết: %d, %v; muốn 3", n, err)
	}
	if _, l, _ := s.LoginLockedUntil(ctx, "uid:1001", t0); l {
		t.Fatal("còn khoá sau khi gỡ hết")
	}
}
