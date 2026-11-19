package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// smallArgon hạ tham số argon2 cho test chạy nhanh; trả lại khi test xong.
func smallArgon(t *testing.T) {
	t.Helper()
	old := argonParams
	argonParams.memory, argonParams.time, argonParams.threads = 64, 1, 1
	t.Cleanup(func() { argonParams = old })
}

func TestHashPasswordKhongBaoGioLaMatKhauThat(t *testing.T) {
	smallArgon(t)
	pw := GeneratePassword()
	if len(pw) < 20 {
		t.Fatalf("mật khẩu sinh ra chỉ %d ký tự, muốn ≥ 20", len(pw))
	}
	h1, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := HashPassword(pw)
	if strings.Contains(h1, pw) {
		t.Fatalf("hash chứa mật khẩu thật: %q", h1)
	}
	if h1 == h2 {
		t.Fatal("hai lần băm cùng mật khẩu ra cùng chuỗi: salt không ngẫu nhiên")
	}
	if !strings.HasPrefix(h1, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("hash không đúng dạng PHC: %q", h1)
	}
	if ok, err := VerifyPassword(h1, pw); err != nil || !ok {
		t.Fatalf("VerifyPassword đúng = %v, %v", ok, err)
	}
	if ok, err := VerifyPassword(h1, pw+"x"); err != nil || ok {
		t.Fatalf("VerifyPassword sai = %v, %v", ok, err)
	}

	// Hash lưu trong DB cũng không chứa mật khẩu thật.
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetAdminPasswordHash(ctx, h1); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.AdminPasswordHash(ctx)
	if err != nil || !ok || got != h1 || strings.Contains(got, pw) {
		t.Fatalf("AdminPasswordHash = %q, %v, %v", got, ok, err)
	}
}

func TestVerifyPasswordHashHong(t *testing.T) {
	for _, h := range []string{
		"", "matkhau", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=16$m=64,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=99999999,t=1,p=1$c2FsdA$aGFzaA", // đòi quá nhiều RAM
		"$argon2id$v=19$m=64,t=1,p=1$c2FsdA$",
		"$argon2id$v=19$m=64,t=1,p=1$!!$aGFzaA",
	} {
		if ok, err := VerifyPassword(h, "x"); err == nil || ok {
			t.Errorf("VerifyPassword(%q) = %v, %v; muốn lỗi", h, ok, err)
		}
	}
}

func TestPhien(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	a := Session{TokenSHA256: "aa", ID: "id-a", IP: "192.0.2.1", Created: t0}
	b := Session{TokenSHA256: "bb", ID: "id-b", IP: "192.0.2.2", Created: t0.Add(time.Minute)}
	for _, ss := range []Session{a, b} {
		if err := s.CreateSession(ctx, ss); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SessionByToken(ctx, "aa")
	if err != nil || got.ID != "id-a" || !got.LastSeen.Equal(t0) {
		t.Fatalf("SessionByToken = %+v, %v", got, err)
	}
	if _, err := s.SessionByToken(ctx, "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token lạ: %v, muốn ErrNotFound", err)
	}
	if err := s.TouchSession(ctx, "aa", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	list, err := s.Sessions(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "id-a" {
		t.Fatalf("Sessions = %+v, %v (muốn id-a lên đầu vì dùng gần nhất)", list, err)
	}

	if err := s.DeleteSessionByID(ctx, "khong-co"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("xoá id lạ: %v", err)
	}
	if err := s.DeleteSessionsIdleBefore(ctx, t0.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByToken(ctx, "bb"); !errors.Is(err, ErrNotFound) {
		t.Fatal("phiên bb nhàn rỗi lâu phải bị dọn")
	}

	if err := s.CreateSession(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeAdminPassword(ctx, "$h2", "aa", t0); err != nil {
		t.Fatal(err)
	}
	list, _ = s.Sessions(ctx)
	if len(list) != 1 || list[0].ID != "id-a" {
		t.Fatalf("đổi mật khẩu phải giữ đúng phiên của người đổi: %+v", list)
	}
	if h, _, _ := s.AdminPasswordHash(ctx); h != "$h2" {
		t.Fatalf("hash sau đổi = %q", h)
	}
}

func TestDemDangNhapSai(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	const ip = "198.51.100.7"
	win, lock := 15*time.Minute, 15*time.Minute
	now := t0
	for i := 1; i <= 4; i++ {
		locked, _, err := s.RecordLoginFailure(ctx, ip, now, 5, win, lock)
		if err != nil || locked {
			t.Fatalf("lần sai %d: locked=%v err=%v", i, locked, err)
		}
	}
	locked, until, err := s.RecordLoginFailure(ctx, ip, now, 5, win, lock)
	if err != nil || !locked || !until.Equal(now.Add(lock)) {
		t.Fatalf("lần sai 5: locked=%v until=%v err=%v", locked, until, err)
	}
	if _, l, _ := s.LoginLockedUntil(ctx, ip, now.Add(14*time.Minute)); !l {
		t.Fatal("phải còn khoá sau 14 phút")
	}
	if _, l, _ := s.LoginLockedUntil(ctx, ip, now.Add(16*time.Minute)); l {
		t.Fatal("hết khoá sau 15 phút")
	}
	if _, l, _ := s.LoginLockedUntil(ctx, "203.0.113.9", now); l {
		t.Fatal("IP khác không bị khoá")
	}
	// Khoá hết thì đếm lại từ 1.
	if l, _, _ := s.RecordLoginFailure(ctx, ip, now.Add(16*time.Minute), 5, win, lock); l {
		t.Fatal("sau khi hết khoá, một lần sai không được khoá ngay")
	}
	// Lần sai đầu đã quá cửa sổ thì cũng đếm lại.
	for i := 0; i < 3; i++ {
		_, _, _ = s.RecordLoginFailure(ctx, ip, now.Add(17*time.Minute), 5, win, lock)
	}
	if l, _, _ := s.RecordLoginFailure(ctx, ip, now.Add(40*time.Minute), 5, win, lock); l {
		t.Fatal("lần sai đầu quá 15 phút thì phải đếm lại")
	}
	if err := s.ClearLoginFailures(ctx, ip); err != nil {
		t.Fatal(err)
	}

	// ResetAdminPassword: hash mới, không còn phiên, không còn khoá.
	_, _, _ = s.RecordLoginFailure(ctx, ip, now, 1, win, lock)
	_ = s.CreateSession(ctx, Session{TokenSHA256: "aa", ID: "a", IP: ip, Created: now})
	if err := s.ResetAdminPassword(ctx, "$moi", now); err != nil {
		t.Fatal(err)
	}
	if _, l, _ := s.LoginLockedUntil(ctx, ip, now); l {
		t.Fatal("admin passwd phải gỡ khoá")
	}
	if list, _ := s.Sessions(ctx); len(list) != 0 {
		t.Fatalf("admin passwd phải xoá mọi phiên: %+v", list)
	}
	if h, _, _ := s.AdminPasswordHash(ctx); h != "$moi" {
		t.Fatalf("hash = %q", h)
	}
}
