package server

// Test của đăng nhập hai bước (M20, docs/thay-doi/M20-thiet-ke.md mục 4, 5,
// 11): luồng, phiếu chờ (B1), mã dùng một lần (B2), khoá theo user, phiên gắn
// tên máy. Token ở token_m20_test.go, 421 và chứng chỉ ở tls_m20_test.go.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/mfa"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	m20Host = "panel.example.com"
	m20IP   = "192.0.2.1"
	m20User = "usera"
)

// m20Env: store thật, FakeSystem (file khoá ở /etc/tpan/secret.key trong máy
// giả), đồng hồ giả, admin và usera cùng mật khẩu authTestPassword.
type m20Env struct {
	t   *testing.T
	st  *store.Store
	s   *Server
	h   http.Handler
	f   *system.FakeSystem
	now time.Time
}

func newM20Env(t *testing.T) *m20Env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	hash, err := store.HashPassword(authTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAdminPasswordHash(ctx, hash); err != nil {
		t.Fatal(err)
	}
	e := &m20Env{t: t, st: st, f: system.NewFake(), now: authT0}
	if err := st.CreateUser(ctx, account.User{Name: m20User, Role: account.RoleUser, Created: e.now}, hash); err != nil {
		t.Fatal(err)
	}
	e.s = newServer(Deps{Sys: e.f, Store: st, Hostname: "may-thu", Now: func() time.Time { return e.now }})
	e.h = e.s.handler(authzDist)
	t.Cleanup(e.s.api.wg.Wait)
	return e
}

// req gửi request qua TLS từ ip tới tên m20Host; hdr là cặp tên, giá trị
// ("Host" đổi tên máy).
func (e *m20Env) req(method, target, body, ip string, cookies []*http.Cookie, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = ip + ":40000"
	r.Host = m20Host
	r.TLS = &tls.ConnectionState{}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			r.Host = hdr[i+1]
			continue
		}
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

func (e *m20Env) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

// session ghi thẳng một phiên (không tốn argon2) gắn m20Host.
func (e *m20Env) session(user string) *http.Cookie {
	e.t.Helper()
	tok := "phien-" + user + "-" + hexID(4)
	e.must(e.st.CreateSession(context.Background(), store.Session{
		TokenSHA256: authTokenHash(tok), ID: hexID(8), IP: m20IP, Created: e.now, UserName: user, Host: m20Host,
	}))
	return &http.Cookie{Name: authCookieTLS, Value: tok}
}

// enable bật hai bước cho user thẳng qua store (bí mật niêm phong bằng file
// khoá thật của server), trả bí mật và mã dự phòng.
func (e *m20Env) enable(user string) ([]byte, []string) {
	e.t.Helper()
	ctx := context.Background()
	key, err := e.s.mfaKey(ctx, true)
	e.must(err)
	secret := mfa.NewSecret()
	sealed, err := mfa.Seal(key, user, secret)
	e.must(err)
	e.must(e.st.SetPendingTOTP(ctx, user, sealed, e.now))
	codes := mfa.NewRecoveryCodes()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = mfa.HashRecovery(mfa.NormalizeRecovery(c))
	}
	e.must(e.st.EnableTOTP(ctx, user, sealed, mfa.Step(e.now)-5, hashes, "", e.now))
	return secret, codes
}

// challenge ghi thẳng một phiếu của user từ ip, trả cookie của nó.
func (e *m20Env) challenge(user, ip string) *http.Cookie {
	e.t.Helper()
	tok := "phieu-" + hexID(8)
	e.must(e.st.CreateChallenge(context.Background(), store.Challenge{
		TokenSHA256: authTokenHash(tok), User: user, IP: ip, Host: m20Host,
		Created: e.now, Expires: e.now.Add(mfaChallengeTTL),
	}))
	return &http.Cookie{Name: mfaCookieTLS, Value: tok}
}

func (e *m20Env) code(secret []byte) string { return mfa.HOTP(secret, mfa.Step(e.now), mfa.Digits) }

// wrongCode là một mã 6 số không khớp bước nào trong cửa sổ ±1.
func (e *m20Env) wrongCode(secret []byte) string {
	for i := 0; ; i++ {
		c := fmt.Sprintf("%06d", i)
		if _, ok := mfa.Match(secret, c, e.now); !ok {
			return c
		}
	}
}

func (e *m20Env) step2(code, ip string, c *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(LoginCode{Code: code})
	return e.req(http.MethodPost, "/api/v1/login/2fa", string(b), ip, []*http.Cookie{c})
}

func (e *m20Env) loginAs(name, ip string) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(LoginRequest{Name: name, Password: authTestPassword})
	return e.req(http.MethodPost, "/api/v1/login", string(b), ip, nil)
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (e *m20Env) sessionInfo(cs ...*http.Cookie) SessionInfo {
	e.t.Helper()
	rec := e.req(http.MethodGet, "/api/v1/session", "", m20IP, cs)
	var info SessionInfo
	e.must(json.Unmarshal(rec.Body.Bytes(), &info))
	return info
}

func TestRequestHost(t *testing.T) {
	for in, want := range map[string]string{
		"panel.example.com":      "panel.example.com",
		"Panel.Example.COM":      "panel.example.com",
		"panel.example.com.":     "panel.example.com",
		"panel.example.com:443":  "panel.example.com",
		"panel.example.com.:443": "panel.example.com",
		"[::1]:8443":             "::1",
		"[::1]":                  "::1",
		"192.0.2.1:8443":         "192.0.2.1",
		"":                       "",
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = in
		if got := requestHost(r); got != want {
			t.Errorf("requestHost(%q) = %q, muốn %q", in, got, want)
		}
	}
}

func TestDangNhapThieuHost(t *testing.T) {
	e := newM20Env(t)
	rec := e.req(http.MethodPost, "/api/v1/login", `{"password":"x"}`, m20IP, nil, "Host", "")
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "host_missing" {
		t.Fatalf("mã %d, thân %s", rec.Code, rec.Body)
	}
}

// TestDangNhapHaiBuoc: luồng đủ qua HTTP.
func TestDangNhapHaiBuoc(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	rec := e.loginAs("", m20IP)
	var res LoginResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusOK || !res.MFARequired {
		t.Fatalf("bước một: mã %d, thân %s", rec.Code, rec.Body)
	}
	if cookieNamed(rec, authCookieTLS) != nil {
		t.Fatal("bước một không được đặt cookie phiên")
	}
	ch := cookieNamed(rec, mfaCookieTLS)
	if ch == nil || !ch.HttpOnly || !ch.Secure || ch.SameSite != http.SameSiteStrictMode || ch.Path != "/" || ch.MaxAge != 300 {
		t.Fatalf("cookie phiếu sai: %+v", ch)
	}
	if info := e.sessionInfo(ch); info.LoggedIn || !info.MFAPending {
		t.Fatalf("có phiếu: SessionInfo = %+v", info)
	}
	rec = e.step2(e.code(secret), m20IP, ch)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("bước hai: mã %d, thân %s", rec.Code, rec.Body)
	}
	sc := cookieNamed(rec, authCookieTLS)
	if sc == nil || sc.Value == "" || sc.Value == ch.Value {
		t.Fatalf("bước hai phải đặt cookie phiên MỚI: %+v", sc)
	}
	if gone := cookieNamed(rec, mfaCookieTLS); gone == nil || gone.MaxAge >= 0 {
		t.Fatalf("bước hai phải xoá cookie phiếu: %+v", gone)
	}
	if info := e.sessionInfo(sc); !info.LoggedIn || info.User.Name != account.AdminName {
		t.Fatalf("sau bước hai: %+v", info)
	}
	ss, err := e.st.SessionByToken(context.Background(), authTokenHash(sc.Value))
	if err != nil || ss.Host != m20Host {
		t.Fatalf("phiên mới phải gắn tên máy: %+v, %v", ss, err)
	}
	// Phiếu đã dùng: dùng lại là không hợp lệ.
	if rec := e.step2(e.code(secret), m20IP, ch); errCode(t, rec) != "mfa_challenge_invalid" {
		t.Fatalf("phiếu đã dùng: %d %s", rec.Code, rec.Body)
	}
}

// TestPhieuChoKhongVaoRouteNao (B1): chỉ mang cookie phiếu thì mọi route không
// public là 401; /session chỉ báo mfa_pending khi phiếu đúng IP, tên, còn hạn.
func TestPhieuChoKhongVaoRouteNao(t *testing.T) {
	e := newM20Env(t)
	e.enable(account.AdminName)
	ch := e.challenge(account.AdminName, m20IP)
	rs := e.s.routes(authzDist)
	for i := range rs {
		rs[i].h = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(authzStatus) }
	}
	mux := http.NewServeMux()
	e.s.mount(mux, rs)
	h := e.s.secure(mux)
	ae := &authzEnv{t: t, incident: "1", backup: "x1"}
	for _, rt := range rs {
		method, raw := splitPattern(rt.pattern)
		path := ae.fill(t, rt, raw, false)
		r := httptest.NewRequest(method, path, strings.NewReader("{}"))
		r.RemoteAddr, r.Host, r.TLS = m20IP+":40000", m20Host, &tls.ConnectionState{}
		r.AddCookie(ch)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		want := http.StatusUnauthorized
		if rt.rule.kind == rulePublic {
			want = authzStatus
		}
		if rec.Code != want {
			t.Errorf("%s chỉ với cookie phiếu: mã %d, muốn %d", rt.pattern, rec.Code, want)
		}
	}
	if info := e.sessionInfo(ch); !info.MFAPending || info.LoggedIn {
		t.Fatalf("phiếu đúng: %+v", info)
	}
	// Sai IP, sai tên máy, hết hạn: như không có phiếu.
	rec := e.req(http.MethodGet, "/api/v1/session", "", "198.51.100.7", []*http.Cookie{ch})
	if strings.Contains(rec.Body.String(), "mfa_pending") {
		t.Errorf("phiếu sai IP vẫn báo chờ: %s", rec.Body)
	}
	rec = e.req(http.MethodGet, "/api/v1/session", "", m20IP, []*http.Cookie{ch}, "Host", "192.0.2.50:8443")
	if strings.Contains(rec.Body.String(), "mfa_pending") {
		t.Errorf("phiếu sai tên máy vẫn báo chờ: %s", rec.Body)
	}
	e.now = e.now.Add(mfaChallengeTTL)
	if info := e.sessionInfo(ch); info.MFAPending {
		t.Errorf("phiếu hết hạn vẫn báo chờ")
	}
	// Đăng xuất xoá phiếu.
	e.now = authT0
	ch2 := e.challenge(account.AdminName, m20IP)
	if rec := e.req(http.MethodPost, "/api/v1/logout", "", m20IP, []*http.Cookie{ch2}); rec.Code != http.StatusNoContent {
		t.Fatalf("đăng xuất: %d", rec.Code)
	}
	if _, err := e.st.ChallengeByToken(context.Background(), authTokenHash(ch2.Value)); err == nil {
		t.Fatal("đăng xuất phải xoá phiếu")
	}
}

func TestMaTOTPKhongDungLai(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	code := e.code(secret)
	if rec := e.step2(code, m20IP, e.challenge(account.AdminName, m20IP)); rec.Code != http.StatusNoContent {
		t.Fatalf("lần đầu: %d %s", rec.Code, rec.Body)
	}
	rec := e.step2(code, m20IP, e.challenge(account.AdminName, m20IP))
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "mfa_code_used" {
		t.Fatalf("dùng lại mã: %d %s", rec.Code, rec.Body)
	}
}

func TestMaDuPhongMotLan(t *testing.T) {
	e := newM20Env(t)
	_, codes := e.enable(account.AdminName)
	rec := e.step2(strings.ToUpper(codes[0]), m20IP, e.challenge(account.AdminName, m20IP))
	var res LoginResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusOK || res.RecoveryLeft == nil || *res.RecoveryLeft != 9 {
		t.Fatalf("mã dự phòng lần đầu: %d %s", rec.Code, rec.Body)
	}
	rec = e.step2(codes[0], m20IP, e.challenge(account.AdminName, m20IP))
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "mfa_code_wrong" {
		t.Fatalf("mã dự phòng lần hai: %d %s", rec.Code, rec.Body)
	}
}

// TestBuocHaiSongSongKhongVuotTran (mục 11.1): 2000 request song song trên
// 400 phiếu của một user, mỗi phiếu một IP (để khoá IP không che khoá user):
// Match chạy không quá 10 lần rồi user bị khoá.
func TestBuocHaiSongSongKhongVuotTran(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	var matches atomic.Int32
	e.s.mfaMatch = func(s []byte, c string, now time.Time) (uint64, bool) {
		matches.Add(1)
		return mfa.Match(s, c, now)
	}
	wrong := e.wrongCode(secret)
	type job struct {
		ip string
		c  *http.Cookie
	}
	var jobs []job
	for i := range 400 {
		ip := fmt.Sprintf("10.0.%d.%d", i/250, i%250+1)
		jobs = append(jobs, job{ip, e.challenge(account.AdminName, ip)})
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[string]int{}
	for range 5 {
		for _, j := range jobs {
			wg.Go(func() {
				rec := e.step2(wrong, j.ip, j.c)
				var b ErrorBody
				_ = json.Unmarshal(rec.Body.Bytes(), &b)
				mu.Lock()
				codes[strconv.Itoa(rec.Code)+" "+b.Error.Code]++
				mu.Unlock()
			})
		}
	}
	wg.Wait()
	if n := matches.Load(); n > mfaUserMax {
		t.Fatalf("Match chạy %d lần trước khi khoá, trần %d (%v)", n, mfaUserMax, codes)
	}
	row, _, err := e.st.TOTP(context.Background(), account.AdminName)
	if err != nil || !row.LockedUntil.After(e.now) {
		t.Fatalf("user phải bị khoá: %+v, %v", row, err)
	}
	for k := range codes {
		switch k {
		case "401 mfa_code_wrong", "423 mfa_locked", "423 login_locked", "401 mfa_challenge_invalid":
		default:
			t.Errorf("câu trả lời lạ %q (%v)", k, codes)
		}
	}
}

// wrongTimes nhập sai n lần, mỗi 4 lần một IP mới (khoá IP là 5).
func (e *m20Env) wrongTimes(secret []byte, n int, salt string) *httptest.ResponseRecorder {
	e.t.Helper()
	var rec *httptest.ResponseRecorder
	for i := range n {
		ip := fmt.Sprintf("198.18.%s.%d", salt, i/4+1)
		rec = e.step2(e.wrongCode(secret), ip, e.challenge(account.AdminName, ip))
	}
	return rec
}

func TestKhoaLeoThang(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	var matches atomic.Int32
	e.s.mfaMatch = func(s []byte, c string, now time.Time) (uint64, bool) {
		matches.Add(1)
		return mfa.Match(s, c, now)
	}
	rec := e.wrongTimes(secret, mfaUserMax, "1")
	if rec.Code != http.StatusLocked || errCode(t, rec) != "mfa_locked" || rec.Header().Get("Retry-After") != "901" {
		t.Fatalf("lần sai thứ 10: %d %s Retry-After=%s", rec.Code, rec.Body, rec.Header().Get("Retry-After"))
	}
	n, err := e.st.CountAuditSince(context.Background(), "login.2fa_locked", "locked", authT0)
	if err != nil || n[account.AdminName] != 1 {
		t.Fatalf("audit login.2fa_locked: %v, %v", n, err)
	}
	// Đang khoá: mã đúng dạng 6 số bị 423 mà không chạy Match.
	before := matches.Load()
	rec = e.step2(e.code(secret), "198.18.9.9", e.challenge(account.AdminName, "198.18.9.9"))
	if rec.Code != http.StatusLocked || errCode(t, rec) != "mfa_locked" || matches.Load() != before {
		t.Fatalf("mã đúng lúc khoá: %d %s, Match %d→%d", rec.Code, rec.Body, before, matches.Load())
	}
	// Hết khoá, sai 10 lần nữa: khoá dài gấp đôi.
	e.now = e.now.Add(16 * time.Minute)
	rec = e.wrongTimes(secret, mfaUserMax, "2")
	if rec.Code != http.StatusLocked || rec.Header().Get("Retry-After") != "1801" {
		t.Fatalf("khoá lần hai: %d Retry-After=%s", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestMaDuPhongQuaKhoaUser(t *testing.T) {
	e := newM20Env(t)
	secret, codes := e.enable(account.AdminName)
	e.wrongTimes(secret, mfaUserMax, "3")
	rec := e.step2(codes[3], "198.18.8.8", e.challenge(account.AdminName, "198.18.8.8"))
	var res LoginResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusOK || res.WrongCodes != mfaUserMax {
		t.Fatalf("mã dự phòng lúc khoá: %d %s", rec.Code, rec.Body)
	}
	row, _, _ := e.st.TOTP(context.Background(), account.AdminName)
	if !row.LockedUntil.IsZero() || row.LockCount != 0 {
		t.Fatalf("mã dự phòng đúng phải gỡ khoá: %+v", row)
	}
}

// TestDoMaKhongResetBoDem: bước một đúng của user có hai bước không xoá bộ đếm
// sai của IP (quy tắc 2 ở 3.4).
func TestDoMaKhongResetBoDem(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	rec := e.loginAs("", m20IP)
	ch := cookieNamed(rec, mfaCookieTLS)
	for range 4 {
		if rec := e.step2(e.wrongCode(secret), m20IP, ch); rec.Code != http.StatusUnauthorized {
			t.Fatalf("sai mã: %d %s", rec.Code, rec.Body)
		}
	}
	rec = e.loginAs("", m20IP)
	if rec.Code != http.StatusOK {
		t.Fatalf("đăng nhập lại: %d %s", rec.Code, rec.Body)
	}
	rec = e.step2(e.wrongCode(secret), m20IP, cookieNamed(rec, mfaCookieTLS))
	if rec.Code != http.StatusLocked || errCode(t, rec) != "login_locked" {
		t.Fatalf("lần sai thứ 5 của IP phải khoá IP: %d %s", rec.Code, rec.Body)
	}
}

func TestPhieuSaiIP(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	ch := e.challenge(account.AdminName, m20IP)
	if rec := e.step2(e.code(secret), "198.51.100.9", ch); errCode(t, rec) != "mfa_challenge_invalid" {
		t.Fatalf("sai IP: %d %s", rec.Code, rec.Body)
	}
	b, _ := json.Marshal(LoginCode{Code: e.code(secret)})
	rec := e.req(http.MethodPost, "/api/v1/login/2fa", string(b), m20IP, []*http.Cookie{ch}, "Host", "192.0.2.80")
	if errCode(t, rec) != "mfa_challenge_invalid" {
		t.Fatalf("sai tên máy: %d %s", rec.Code, rec.Body)
	}
	if rec := e.step2(e.code(secret), m20IP, ch); rec.Code != http.StatusNoContent {
		t.Fatalf("đúng IP, đúng tên: %d %s", rec.Code, rec.Body)
	}
}

func TestBuocHaiChongCSRF(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	ch := e.challenge(account.AdminName, m20IP)
	b, _ := json.Marshal(LoginCode{Code: e.code(secret)})
	rec := e.req(http.MethodPost, "/api/v1/login/2fa", string(b), m20IP, []*http.Cookie{ch}, "Sec-Fetch-Site", "cross-site")
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "csrf" {
		t.Fatalf("bước hai từ trang lạ: %d %s", rec.Code, rec.Body)
	}
	rec = e.req(http.MethodPost, "/api/v1/account/tokens", `{}`, m20IP, []*http.Cookie{e.session(m20User)}, "Origin", "https://evil.example")
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "csrf" {
		t.Fatalf("tạo token từ trang lạ: %d %s", rec.Code, rec.Body)
	}
}

// TestPhienLechHostBiXoa (mục 11.2 ý 1).
func TestPhienLechHostBiXoa(t *testing.T) {
	e := newM20Env(t)
	c := e.session(m20User)
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", m20IP, []*http.Cookie{c}); rec.Code != http.StatusOK {
		t.Fatalf("đúng tên: %d", rec.Code)
	}
	rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", m20IP, []*http.Cookie{c}, "Host", "192.0.2.80:8443")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("lệch tên: %d", rec.Code)
	}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", m20IP, []*http.Cookie{c}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("phiên lệch tên phải bị xoá hẳn: %d", rec.Code)
	}
	n, _ := e.st.CountAuditSince(context.Background(), "session.host_mismatch", "deleted", authT0)
	if len(n) != 1 {
		t.Fatalf("audit session.host_mismatch: %v", n)
	}
}

// TestPhienCuKhongHostGanHost (R1): phiên của bản cũ được gắn tên ở lần dùng đầu.
func TestPhienCuKhongHostGanHost(t *testing.T) {
	e := newM20Env(t)
	ctx := context.Background()
	e.must(e.st.CreateSession(ctx, store.Session{TokenSHA256: authTokenHash("cu"), ID: "cu", IP: m20IP, Created: e.now, UserName: m20User}))
	c := &http.Cookie{Name: authCookieTLS, Value: "cu"}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", m20IP, []*http.Cookie{c}, "Host", "192.0.2.10:8443"); rec.Code != http.StatusOK {
		t.Fatalf("phiên cũ dùng lần đầu: %d", rec.Code)
	}
	ss, err := e.st.SessionByToken(ctx, authTokenHash("cu"))
	if err != nil || ss.Host != "192.0.2.10" {
		t.Fatalf("phiên cũ phải được gắn tên: %+v %v", ss, err)
	}
	if rec := e.req(http.MethodGet, "/api/v1/account/sessions", "", m20IP, []*http.Cookie{c}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sau khi gắn tên, tên khác phải bị từ chối: %d", rec.Code)
	}
}
