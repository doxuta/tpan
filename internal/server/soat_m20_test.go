package server

// Test của lượt soát cuối M20 (docs/thay-doi/M20.md, mục "Soát cuối (đọc
// mã)"): F1 xác nhận lại trước thao tác tạo quyền sống lâu, F3 token không
// chạy health đầy đủ, F4 trần 5 lần mỗi phiếu khi request song song. F2 nằm
// trong TestNhomTokenCuaRoute, F5 trong TestHostLaSite421.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/mfa"
	"github.com/doxuta/tpan/internal/system"
)

const soatWrongPW = "sai-mat-khau-roi"

// expect đòi mã HTTP và mã lỗi (code rỗng: không xét mã lỗi).
func (e *m20Env) expect(what string, rec *httptest.ResponseRecorder, status int, code string) {
	e.t.Helper()
	r := rec.Result()
	var b ErrorBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	if r.StatusCode != status || (code != "" && b.Error.Code != code) {
		e.t.Fatalf("%s: %d %q, muốn %d %q", what, r.StatusCode, b.Error.Code, status, code)
	}
}

func (e *m20Env) postRaw(path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.req(http.MethodPost, path, body, m20IP, []*http.Cookie{c})
}

func (e *m20Env) userExists(name string) (account.User, bool) {
	e.t.Helper()
	u, found, err := e.st.User(context.Background(), name)
	e.must(err)
	return u, found
}

// TestTaoUserCanXacNhan (F1): POST /users (mọi vai) đòi mật khẩu của admin
// đang làm, rồi mã khi admin đã bật hai bước. Từ chối thì không có user nào.
func TestTaoUserCanXacNhan(t *testing.T) {
	e := newM20Env(t)
	e.f.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	secret, _ := e.enable(account.AdminName)
	c := e.session(account.AdminName)
	const base = `{"name":"cuasau","role":"admin","password":"mat-khau-cua-sau-123"`
	e.expect("chỉ cookie", e.postRaw("/api/v1/users", base+`}`, c), 400, "reauth_required")
	e.expect("vai user, chỉ cookie", e.postRaw("/api/v1/users", `{"name":"cuasau","role":"user"}`, c), 400, "reauth_required")
	e.expect("sai mật khẩu", e.postRaw("/api/v1/users", base+`,"current_password":"`+soatWrongPW+`","code":"`+e.code(secret)+`"}`, c), 401, "wrong_password")
	e.expect("thiếu mã", e.postRaw("/api/v1/users", base+`,"current_password":"`+authTestPassword+`"}`, c), 400, "mfa_code_required")
	e.expect("sai mã", e.postRaw("/api/v1/users", base+`,"current_password":"`+authTestPassword+`","code":"`+e.wrongCode(secret)+`"}`, c), 400, "mfa_code_wrong")
	if _, found := e.userExists("cuasau"); found {
		t.Fatal("bị từ chối mà vẫn tạo user")
	}
	rec := e.req(http.MethodPost, "/api/v1/users", base+`,"current_password":"`+authTestPassword+`","code":"`+e.code(secret)+`"}`, m20IP, []*http.Cookie{c})
	if rec.Code != http.StatusOK {
		t.Fatalf("đủ mật khẩu và mã: %d %s", rec.Code, rec.Body)
	}
	if u, found := e.userExists("cuasau"); !found || u.Role != account.RoleAdmin {
		t.Fatalf("user mới: %+v %v", u, found)
	}
}

// TestNangAdminCanXacNhan (F1): đổi vai thành admin đòi xác nhận; sửa trường
// khác, hay gửi lại role admin cho người đã là admin, thì không.
func TestNangAdminCanXacNhan(t *testing.T) {
	e := newM20Env(t)
	c := e.session(account.AdminName)
	p := "/api/v1/users/" + m20User
	e.expect("sửa email", e.postRaw(p, `{"email":"a@example.com"}`, c), 200, "")
	e.expect("nâng admin, chỉ cookie", e.postRaw(p, `{"role":"admin"}`, c), 400, "reauth_required")
	e.expect("nâng admin, sai mật khẩu", e.postRaw(p, `{"role":"admin","current_password":"`+soatWrongPW+`"}`, c), 401, "wrong_password")
	if u, _ := e.userExists(m20User); u.Role != account.RoleUser {
		t.Fatalf("bị từ chối mà vai đã đổi: %s", u.Role)
	}
	e.expect("nâng admin, đúng mật khẩu", e.postRaw(p, `{"role":"admin","current_password":"`+authTestPassword+`"}`, c), 200, "")
	if u, _ := e.userExists(m20User); u.Role != account.RoleAdmin {
		t.Fatalf("vai sau khi nâng: %s", u.Role)
	}
	e.expect("đã là admin, gửi lại role", e.postRaw(p, `{"role":"admin","email":"b@example.com"}`, c), 200, "")
}

// TestDatMatKhauNguoiKhacCanXacNhan (F1): đặt mật khẩu cho user khác đòi
// xác nhận; từ chối thì mật khẩu (và phiên) của người đó còn nguyên.
func TestDatMatKhauNguoiKhacCanXacNhan(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	c, cu := e.session(account.AdminName), e.session(m20User)
	p := "/api/v1/users/" + m20User + "/password"
	e.expect("thân rỗng", e.postRaw(p, "", c), 400, "reauth_required")
	e.expect("chỉ mật khẩu mới", e.postRaw(p, `{"password":"mat-khau-cua-sau-123"}`, c), 400, "reauth_required")
	e.expect("thiếu mã", e.postRaw(p, `{"current_password":"`+authTestPassword+`"}`, c), 400, "mfa_code_required")
	e.expect("sai mã", e.postRaw(p, `{"current_password":"`+authTestPassword+`","code":"`+e.wrongCode(secret)+`"}`, c), 400, "mfa_code_wrong")
	if !e.sessionInfo(cu).LoggedIn {
		t.Fatal("bị từ chối mà phiên của user đã bị huỷ (mật khẩu đã đặt)")
	}
	rec := e.req(http.MethodPost, p, `{"current_password":"`+authTestPassword+`","code":"`+e.code(secret)+`"}`, m20IP, []*http.Cookie{c})
	var out PasswordSetResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || out.Password == "" {
		t.Fatalf("đủ xác nhận: %d %s", rec.Code, rec.Body)
	}
	if e.sessionInfo(cu).LoggedIn {
		t.Fatal("đặt mật khẩu xong phiên cũ của user phải bị huỷ")
	}
}

// TestNoiChinhSachCanXacNhan (F1): siết không cần xác nhận; hạ chính sách hay
// tăng trần ngày của token thì cần mật khẩu và mã.
func TestNoiChinhSachCanXacNhan(t *testing.T) {
	e := newM20Env(t)
	ctx := context.Background()
	secret, _ := e.enable(account.AdminName)
	c := e.session(account.AdminName)
	const p = "/api/v1/settings/auth"
	e.expect("siết: all, 30 ngày", e.postRaw(p, `{"require_2fa":"all","token_max_days":30}`, c), 200, "")
	e.expect("hạ về admin", e.postRaw(p, `{"require_2fa":"admin"}`, c), 400, "reauth_required")
	e.expect("tắt", e.postRaw(p, `{"require_2fa":"off","token_max_days":365}`, c), 400, "reauth_required")
	e.expect("tăng trần ngày", e.postRaw(p, `{"token_max_days":31}`, c), 400, "reauth_required")
	e.expect("tắt, thiếu mã", e.postRaw(p, `{"require_2fa":"off","current_password":"`+authTestPassword+`"}`, c), 400, "mfa_code_required")
	e.expect("tắt, sai mật khẩu", e.postRaw(p, `{"require_2fa":"off","current_password":"`+soatWrongPW+`","code":"`+e.code(secret)+`"}`, c), 401, "wrong_password")
	e.expect("tắt, sai mã", e.postRaw(p, `{"require_2fa":"off","current_password":"`+authTestPassword+`","code":"`+e.wrongCode(secret)+`"}`, c), 400, "mfa_code_wrong")
	if pol, _ := e.s.policy(ctx); pol != PolicyAll {
		t.Fatalf("bị từ chối mà chính sách đã đổi: %q", pol)
	}
	if d, _ := e.s.tokenMaxDays(ctx); d != 30 {
		t.Fatalf("bị từ chối mà trần ngày đã đổi: %d", d)
	}
	e.expect("siết thêm: 7 ngày", e.postRaw(p, `{"require_2fa":"all","token_max_days":7}`, c), 200, "")
	e.expect("tắt, đủ xác nhận", e.postRaw(p, `{"require_2fa":"off","token_max_days":365,"current_password":"`+authTestPassword+`","code":"`+e.code(secret)+`"}`, c), 200, "")
	if pol, _ := e.s.policy(ctx); pol != PolicyOff {
		t.Fatalf("chính sách sau khi tắt: %q", pol)
	}
	if d, _ := e.s.tokenMaxDays(ctx); d != 365 {
		t.Fatalf("trần ngày sau khi nới: %d", d)
	}
}

// TestPhieuSongSongKhongVuotTran5 (F4): 16 request song song, MỘT phiếu, MỘT
// IP, mã sai: Match chạy và lần sai được ghi không quá 5 lần (trần của phiếu,
// cũng là trần của IP).
func TestPhieuSongSongKhongVuotTran5(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(account.AdminName)
	var matches atomic.Int32
	e.s.mfaMatch = func(s []byte, c string, now time.Time) (uint64, bool) {
		matches.Add(1)
		return mfa.Match(s, c, now)
	}
	ch := e.challenge(account.AdminName, m20IP)
	wrong := e.wrongCode(secret)
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[string]int{}
	for range 16 {
		wg.Go(func() {
			rec := e.step2(wrong, m20IP, ch)
			var b ErrorBody
			_ = json.Unmarshal(rec.Body.Bytes(), &b)
			mu.Lock()
			got[strconv.Itoa(rec.Code)+" "+b.Error.Code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	n, err := e.st.CountAuditSince(context.Background(), "login.2fa", "wrong_code", authT0)
	e.must(err)
	if n[account.AdminName] > mfaChallengeTries || matches.Load() > mfaChallengeTries {
		t.Fatalf("lần sai ghi %d, Match %d, trần %d (%v)", n[account.AdminName], matches.Load(), mfaChallengeTries, got)
	}
	for k := range got {
		switch k {
		case "401 mfa_code_wrong", "401 mfa_challenge_invalid", "423 login_locked", "423 mfa_locked":
		default:
			t.Errorf("câu trả lời lạ %q (%v)", k, got)
		}
	}
}

// TestTongQuanTokenKhongChayHealth (F3): token chỉ đọc gọi ?refresh=1 thì
// được tổng quan nhưng không chạy lượt health đầy đủ.
func TestTongQuanTokenKhongChayHealth(t *testing.T) {
	e := newM20Env(t)
	raw, _ := e.token(account.AdminName, "health:read", time.Hour)
	rec := e.req(http.MethodGet, "/api/v1/overview?refresh=1", "", m20IP, nil, bearer(raw)...)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview bằng token: %d %s", rec.Code, rec.Body)
	}
	if _, ran, err := e.st.GetSetting(context.Background(), health.SettingLastRun); err != nil || ran {
		t.Fatalf("token health:read đã chạy health đầy đủ (%v)", err)
	}
}
