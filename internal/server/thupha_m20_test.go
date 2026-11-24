package server

// Hai phát hiện nhẹ của lượt thử phá trên máy ảo (docs/bang-chung/M20-thu-pha.log).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
)

// TestDangNhapKhongNhanAuthorization (V1): /login và /login/2fa có header
// Authorization thì 400, không tạo phiên. Header đó miễn 421, nên nếu /login
// nhận nó thì một phiên được tạo dưới tên của một site.
func TestDangNhapKhongNhanAuthorization(t *testing.T) {
	e := newM20Env(t)
	for _, p := range []string{"/api/v1/login", "/api/v1/login/2fa"} {
		rec := e.req(http.MethodPost, p, `{"password":"`+authTestPassword+`","code":"000000"}`, m20IP, nil, "Authorization", "Bearer x")
		if rec.Code != http.StatusBadRequest || errCode(t, rec) != "authorization_on_login" {
			t.Errorf("%s có Authorization: %d %s", p, rec.Code, rec.Body)
		}
		if cookieNamed(rec, authCookieTLS) != nil {
			t.Errorf("%s có Authorization vẫn đặt cookie phiên", p)
		}
	}
	// Không có header: đăng nhập như thường.
	if rec := e.loginAs("", m20IP); rec.Code != http.StatusNoContent {
		t.Fatalf("đăng nhập thường: %d %s", rec.Code, rec.Body)
	}
}

// TestKhoaRoiMoKhoaKhongTraLaiToken (V2): khoá user (đi qua core.Accounts,
// DeleteSessionsOf) thu hồi token; mở khoá rồi token cũ vẫn 401 token_revoked.
func TestKhoaRoiMoKhoaKhongTraLaiToken(t *testing.T) {
	e := newM20Env(t)
	ctx := context.Background()
	h, _ := e.fakeHandler()
	e.must(e.st.CreateUser(ctx, account.User{Name: "boss", Role: account.RoleAdmin, Created: e.now}, "x"))
	// Hạn xa: store thu hồi theo đồng hồ thật (bỏ token đã hết hạn), còn môi
	// trường thử chạy ở một mốc giờ cố định trong quá khứ.
	raw, _ := e.token("boss", "system:read", 20*365*24*time.Hour)
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/machine", nil)
		r.RemoteAddr, r.Host = m20IP+":1", m20Host
		r.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if rec := get(); rec.Code != authzStatus {
		t.Fatalf("trước khi khoá: %d %s", rec.Code, rec.Body)
	}
	// Đường của khoá user và đổi vai ở core: UpdateUser rồi DeleteSessionsOf.
	e.must(e.st.DeleteSessionsOf(ctx, "boss"))
	if rec := get(); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "token_revoked" {
		t.Fatalf("sau khi khoá/đổi vai: %d %s, muốn 401 token_revoked", rec.Code, rec.Body)
	}
}
