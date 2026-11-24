package store

import (
	"context"
	"testing"
)

func TestSetSessionHostChiGhiKhiRong(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	addSession(t, s, "tok", "an", "")
	must(t, s.SetSessionHost(ctx, "tok", "panel.example.com"))
	// Lần hai với tên khác không đổi được: phiên đã gắn tên là gắn mãi.
	must(t, s.SetSessionHost(ctx, "tok", "other.example.com"))
	ss, err := s.SessionByToken(ctx, "tok")
	must(t, err)
	if ss.Host != "panel.example.com" {
		t.Fatalf("host = %q, muốn panel.example.com", ss.Host)
	}
}

func TestAnyTOTP(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	if on, err := s.AnyTOTP(ctx); err != nil || on {
		t.Fatalf("bảng rỗng: %v, %v", on, err)
	}
	// Dòng đang chờ xác nhận cũng tính (bí mật đã niêm phong bằng file khoá).
	must(t, s.SetPendingTOTP(ctx, "an", []byte("sealed"), t0))
	if on, err := s.AnyTOTP(ctx); err != nil || !on {
		t.Fatalf("có dòng chờ: %v, %v", on, err)
	}
}
