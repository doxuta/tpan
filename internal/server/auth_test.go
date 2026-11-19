package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/doxuta/tpan/internal/store"
)

const authTestPassword = "mat-khau-dung-123"

var authT0 = time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)

// authEnv là một server có DB thật trong thư mục tạm và đồng hồ giả.
type authEnv struct {
	t   *testing.T
	h   http.Handler
	st  *store.Store
	now time.Time
	tls bool // request đi qua TLS (cookie __Host-tpan)
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	hash, err := store.HashPassword(authTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAdminPasswordHash(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	e := &authEnv{t: t, st: st, now: authT0, tls: true}
	e.h = NewHandler(Deps{Store: st, Hostname: "may-thu", Now: func() time.Time { return e.now }},
		fstest.MapFS{"index.html": {Data: []byte("<html>tpan</html>")}})
	return e
}

// req gửi một request; ip là IP của kết nối, cookie có thể nil.
func (e *authEnv) req(method, target, body, ip string, cookie *http.Cookie, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = ip + ":40000"
	if e.tls {
		r.TLS = &tls.ConnectionState{}
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

func (e *authEnv) login(pw, ip string) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(LoginRequest{Password: pw})
	return e.req(http.MethodPost, "/api/v1/login", string(b), ip, nil)
}

// mustLogin đăng nhập đúng và trả cookie phiên.
func (e *authEnv) mustLogin(ip string) *http.Cookie {
	e.t.Helper()
	rec := e.login(authTestPassword, ip)
	if rec.Code != http.StatusNoContent {
		e.t.Fatalf("đăng nhập đúng: mã %d, thân %s", rec.Code, rec.Body)
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		e.t.Fatalf("muốn đúng 1 cookie, có %d", len(cs))
	}
	return cs[0]
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("thân lỗi không phải ErrorBody: %v (%s)", err, rec.Body)
	}
	return body.Error.Code
}

func TestLoginCookie(t *testing.T) {
	for _, withTLS := range []bool{true, false} {
		e := newAuthEnv(t)
		e.tls = withTLS
		c := e.mustLogin("192.0.2.1")

		wantName, wantSecure := "__Host-tpan", true
		if !withTLS {
			// Không TLS: trình duyệt không nhận cookie Secure (xem auth.go).
			wantName, wantSecure = "tpan", false
		}
		if c.Name != wantName || c.Secure != wantSecure || !c.HttpOnly ||
			c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" {
			t.Fatalf("tls=%v: cookie sai thuộc tính: %+v", withTLS, c)
		}
		raw, _ := json.Marshal(c.Value)
		if len(c.Value) < 43 { // 32 byte base64 không đệm
			t.Fatalf("token quá ngắn: %s", raw)
		}

		// DB chỉ giữ sha256 của token.
		list, err := e.st.Sessions(context.Background())
		if err != nil || len(list) != 1 {
			t.Fatalf("Sessions = %v, %v", list, err)
		}
		sum := sha256.Sum256([]byte(c.Value))
		if list[0].TokenSHA256 != hex.EncodeToString(sum[:]) || strings.Contains(list[0].TokenSHA256, c.Value) {
			t.Fatal("DB phải giữ sha256 của token, không phải token")
		}

		rec := e.req(http.MethodGet, "/api/v1/session", "", "192.0.2.1", c)
		var info SessionInfo
		_ = json.Unmarshal(rec.Body.Bytes(), &info)
		if !info.LoggedIn || info.Hostname != "may-thu" || !info.Expires.Equal(authT0.Add(12*time.Hour)) {
			t.Fatalf("tls=%v: SessionInfo = %+v", withTLS, info)
		}
	}
}

func TestCookieKhongTLSKhongDungQuaTLS(t *testing.T) {
	e := newAuthEnv(t)
	e.tls = false
	c := e.mustLogin("192.0.2.1")
	e.tls = true
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("cookie đặt qua http dùng qua https: mã %d, muốn 401", rec.Code)
	}
}

func TestChuaDangNhap(t *testing.T) {
	e := newAuthEnv(t)
	rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", nil)
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "unauthorized" {
		t.Fatalf("mã %d, thân %s", rec.Code, rec.Body)
	}
	bad := &http.Cookie{Name: "__Host-tpan", Value: "gia-mao"}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", bad); rec.Code != http.StatusUnauthorized {
		t.Fatalf("cookie giả: mã %d", rec.Code)
	}
	var info SessionInfo
	rec = e.req(http.MethodGet, "/api/v1/session", "", "192.0.2.1", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if rec.Code != http.StatusOK || info.LoggedIn || info.Hostname != "" {
		t.Fatalf("session khi chưa đăng nhập: %d %+v", rec.Code, info)
	}
}

func TestDangNhapSaiVaKhoa(t *testing.T) {
	e := newAuthEnv(t)
	const ip = "198.51.100.7"
	for i := 1; i <= 4; i++ {
		rec := e.login("sai", ip)
		if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "wrong_password" {
			t.Fatalf("lần sai %d: mã %d %s", i, rec.Code, rec.Body)
		}
	}
	rec := e.login("sai", ip)
	if rec.Code != http.StatusLocked || errCode(t, rec) != "login_locked" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("lần sai 5: mã %d %s, muốn 423", rec.Code, rec.Body)
	}
	// Đang khoá thì mật khẩu đúng cũng bị từ chối.
	if rec := e.login(authTestPassword, ip); rec.Code != http.StatusLocked {
		t.Fatalf("đúng mật khẩu khi đang khoá: mã %d", rec.Code)
	}
	// IP khác không bị ảnh hưởng.
	e.mustLogin("203.0.113.5")

	e.now = e.now.Add(16 * time.Minute)
	e.mustLogin(ip)
	// Đăng nhập đúng xoá bộ đếm: lại được sai 4 lần mà không khoá.
	for i := 1; i <= 4; i++ {
		if rec := e.login("sai", ip); rec.Code != http.StatusUnauthorized {
			t.Fatalf("sau khi đếm lại, lần sai %d: mã %d", i, rec.Code)
		}
	}
}

func TestDangNhapThanSaiVaChuaSetup(t *testing.T) {
	e := newAuthEnv(t)
	if rec := e.req(http.MethodPost, "/api/v1/login", "{không phải json", "192.0.2.1", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("JSON hỏng: mã %d", rec.Code)
	}
	big := `{"password":"` + strings.Repeat("a", 10000) + `"}`
	if rec := e.req(http.MethodPost, "/api/v1/login", big, "192.0.2.1", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("thân quá lớn: mã %d", rec.Code)
	}

	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "rong.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	h := NewHandler(Deps{Store: st}, fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`{"password":"x"}`)))
	if rec.Code != http.StatusServiceUnavailable || errCode(t, rec) != "not_set_up" {
		t.Fatalf("chưa setup: mã %d %s", rec.Code, rec.Body)
	}
}

func TestPhienHetHan(t *testing.T) {
	e := newAuthEnv(t)
	c := e.mustLogin("192.0.2.1")
	// Dùng đều đặn thì phiên sống quá 12 giờ kể từ lúc đăng nhập.
	for range 3 {
		e.now = e.now.Add(11 * time.Hour)
		if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", c); rec.Code != http.StatusOK {
			t.Fatalf("phiên còn dùng: mã %d", rec.Code)
		}
	}
	e.now = e.now.Add(12*time.Hour + time.Minute)
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("12 giờ không dùng: mã %d, muốn 401", rec.Code)
	}
	if list, _ := e.st.Sessions(context.Background()); len(list) != 0 {
		t.Fatalf("phiên hết hạn phải bị xoá: %+v", list)
	}
}

func TestCSRF(t *testing.T) {
	e := newAuthEnv(t)
	body := `{"password":"` + authTestPassword + `"}`
	for _, hdr := range [][]string{
		{"Sec-Fetch-Site", "cross-site"},
		{"Sec-Fetch-Site", "same-site"},
		{"Origin", "https://evil.example.com"},
	} {
		rec := e.req(http.MethodPost, "/api/v1/login", body, "192.0.2.1", nil, hdr...)
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "csrf" {
			t.Fatalf("%v: mã %d %s, muốn 403 csrf", hdr, rec.Code, rec.Body)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("403 cũng phải có header bảo mật")
		}
	}
	if rec := e.req(http.MethodPost, "/api/v1/login", body, "192.0.2.1", nil, "Sec-Fetch-Site", "same-origin"); rec.Code != http.StatusNoContent {
		t.Fatalf("same-origin: mã %d", rec.Code)
	}
	// GET từ trang khác vẫn đi qua (không thay đổi gì).
	if rec := e.req(http.MethodGet, "/api/v1/session", "", "192.0.2.1", nil, "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusOK {
		t.Fatalf("GET cross-site: mã %d", rec.Code)
	}
}

func TestHeaderBaoMat(t *testing.T) {
	e := newAuthEnv(t)
	for _, target := range []string{"/", "/api/v1/health", "/api/v1/session"} {
		rec := e.req(http.MethodGet, target, "", "192.0.2.1", nil)
		want := map[string]string{
			"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'",
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s: %s = %q, muốn %q", target, k, got, v)
			}
		}
	}
}

func TestDoiMatKhauHuyPhienKhac(t *testing.T) {
	e := newAuthEnv(t)
	a := e.mustLogin("192.0.2.1")
	b := e.mustLogin("192.0.2.2")
	change := func(c *http.Cookie, cur, nw string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(PasswordChange{Current: cur, New: nw})
		return e.req(http.MethodPost, "/api/v1/account/password", string(raw), "192.0.2.1", c)
	}
	if rec := change(nil, authTestPassword, "mat-khau-moi-rat-dai"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("không phiên: mã %d", rec.Code)
	}
	if rec := change(a, authTestPassword, "ngắn"); rec.Code != http.StatusBadRequest || errCode(t, rec) != "password_too_short" {
		t.Fatalf("mật khẩu ngắn: mã %d %s", rec.Code, rec.Body)
	}
	// 12 ký tự có dấu: đếm theo ký tự chứ không theo byte.
	if rec := change(a, authTestPassword, "ắắắắắắắắắắắ"); rec.Code != http.StatusBadRequest {
		t.Fatalf("11 ký tự có dấu: mã %d, muốn 400", rec.Code)
	}
	if rec := change(a, "sai", "mat-khau-moi-rat-dai"); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "wrong_password" {
		t.Fatalf("sai mật khẩu hiện tại: mã %d %s", rec.Code, rec.Body)
	}
	if rec := change(a, authTestPassword, "mat-khau-moi-rat-dai"); rec.Code != http.StatusNoContent {
		t.Fatalf("đổi mật khẩu: mã %d %s", rec.Code, rec.Body)
	}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.2", b); rec.Code != http.StatusUnauthorized {
		t.Fatalf("phiên khác phải bị huỷ: mã %d", rec.Code)
	}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", a); rec.Code != http.StatusOK {
		t.Fatalf("phiên của người đổi phải còn: mã %d", rec.Code)
	}
	if rec := e.login(authTestPassword, "192.0.2.3"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("mật khẩu cũ: mã %d", rec.Code)
	}
	if rec := e.login("mat-khau-moi-rat-dai", "192.0.2.3"); rec.Code != http.StatusNoContent {
		t.Fatalf("mật khẩu mới: mã %d", rec.Code)
	}
}

func TestDanhSachVaXoaPhien(t *testing.T) {
	e := newAuthEnv(t)
	a := e.mustLogin("192.0.2.1")
	e.now = e.now.Add(5 * time.Minute)
	b := e.mustLogin("192.0.2.2")

	rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", a)
	var rows []SessionRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("danh sách phiên: %v %s", err, rec.Body)
	}
	var mine, other SessionRow
	for _, r := range rows {
		if r.Current {
			mine = r
		} else {
			other = r
		}
	}
	if mine.IP != "192.0.2.1" || other.IP != "192.0.2.2" || mine.ID == "" || strings.Contains(rec.Body.String(), a.Value) {
		t.Fatalf("hàng phiên sai hoặc lộ token: %s", rec.Body)
	}

	if rec := e.req(http.MethodDelete, "/api/v1/account/sessions/khong-co", "", "192.0.2.1", a); rec.Code != http.StatusNotFound {
		t.Fatalf("xoá id lạ: mã %d", rec.Code)
	}
	if rec := e.req(http.MethodDelete, "/api/v1/account/sessions/"+other.ID, "", "192.0.2.1", a); rec.Code != http.StatusNoContent {
		t.Fatalf("xoá phiên khác: mã %d", rec.Code)
	}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.2", b); rec.Code != http.StatusUnauthorized {
		t.Fatalf("phiên đã xoá vẫn dùng được: mã %d", rec.Code)
	}
}

func TestDangXuat(t *testing.T) {
	e := newAuthEnv(t)
	c := e.mustLogin("192.0.2.1")
	rec := e.req(http.MethodPost, "/api/v1/logout", "", "192.0.2.1", c)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("đăng xuất: mã %d", rec.Code)
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 || cs[0].MaxAge >= 0 {
		t.Fatalf("đăng xuất phải xoá cookie: %+v", cs)
	}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", "192.0.2.1", c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sau đăng xuất: mã %d", rec.Code)
	}
	// Đăng xuất khi không có phiên vẫn 204.
	if rec := e.req(http.MethodPost, "/api/v1/logout", "", "192.0.2.1", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("đăng xuất không phiên: mã %d", rec.Code)
	}
}
