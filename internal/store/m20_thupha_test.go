package store

import (
	"context"
	"testing"
	"time"
)

// TestDoiVaiHayKhoaThuHoiToken (soát cuối trên máy ảo, V2): DeleteSessionsOf
// (gọi khi đổi vai trò và khi tạm khoá user) thu hồi mọi token còn sống của
// user đó, nên nâng vai lại hay mở khoá không làm token cũ sống lại. Token
// của user khác không bị đụng.
func TestDoiVaiHayKhoaThuHoiToken(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	now := t0.Add(time.Hour)
	s.now = func() time.Time { return now }
	exp := t0.Add(24 * time.Hour)
	addToken(t, s, "an1", "an", t0, exp)
	addToken(t, s, "an2", "an", t0, exp)
	addToken(t, s, "adm1", "admin", t0, exp)

	must(t, s.DeleteSessionsOf(ctx, "an"))
	if n, _ := s.ActiveTokens(ctx, "an", now); n != 0 {
		t.Errorf("DeleteSessionsOf còn %d token sống của an", n)
	}
	if tk, _ := s.TokenByHash(ctx, "sha-an1"); !tk.Revoked.Equal(now) {
		t.Errorf("revoked_at = %v, muốn %v", tk.Revoked, now)
	}
	if n, _ := s.ActiveTokens(ctx, "admin", now); n != 1 {
		t.Errorf("DeleteSessionsOf(an) thu hồi nhầm token của admin (còn %d)", n)
	}
}
