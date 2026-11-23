package server

// Test của token API và các route tài khoản M20 (mục 3.5 đến 3.7, 4, 11.3,
// 11.5, 11.9).

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/mfa"
	"github.com/doxuta/tpan/internal/store"
)

// token ghi thẳng một token của user với phạm vi scopes, hết hạn sau ttl; trả
// token thật và id.
func (e *m20Env) token(user, scopes string, ttl time.Duration) (string, string) {
	e.t.Helper()
	raw, id := apitoken.New(), hexID(8)
	e.must(e.st.CreateToken(context.Background(), store.TokenRow{
		ID: id, User: user, Name: "ci", Scopes: scopes, Hint: apitoken.Hint(raw),
		Created: e.now, Expires: e.now.Add(ttl),
	}, apitoken.Hash(raw)))
	return raw, id
}

// allScopes là mọi nhóm với quyền ghi.
func allScopes() string {
	var out []string
	for _, g := range apitoken.Groups {
		out = append(out, string(g)+":write")
	}
	return strings.Join(out, ",")
}

// fakeHandler là bảng route thật với handler giả (chỉ kiểm guard).
func (e *m20Env) fakeHandler() (http.Handler, []route) {
	rs := e.s.routes(authzDist)
	for i := range rs {
		rs[i].h = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(authzStatus) }
	}
	mux := http.NewServeMux()
	e.s.mount(mux, rs)
	return e.s.secure(mux), rs
}

func bearer(raw string) []string { return []string{"Authorization", "Bearer " + raw} }

func (e *m20Env) post(path string, v any, c *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(v)
	return e.req(http.MethodPost, path, string(b), m20IP, []*http.Cookie{c})
}

func TestTokenXacThuc(t *testing.T) {
	e := newM20Env(t)
	h, _ := e.fakeHandler()
	do := func(hdr ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/sites", nil)
		r.RemoteAddr, r.Host = m20IP+":1", m20Host
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	good, _ := e.token(m20User, "sites:read", time.Hour)
	if rec := do(bearer(good)...); rec.Code != authzStatus {
		t.Fatalf("token đúng: %d %s", rec.Code, rec.Body)
	}
	bad := good[:len(good)-1] + map[bool]string{true: "a", false: "b"}[good[len(good)-1] != 'a']
	unknown := apitoken.New()
	for name, c := range map[string]struct{ hdr, code string }{
		"thiếu Bearer":      {good, "token_invalid"},
		"rỗng":              {"", "token_invalid"},
		"sai kiểm":          {"Bearer " + bad, "token_malformed"},
		"không có trong DB": {"Bearer " + unknown, "token_invalid"},
	} {
		rec := do("Authorization", c.hdr)
		if rec.Code != http.StatusUnauthorized || errCode(t, rec) != c.code {
			t.Errorf("%s: %d %s, muốn 401 %s", name, rec.Code, rec.Body, c.code)
		}
	}
	// Có Authorization thì không đọc cookie: cookie phiên hợp lệ không cứu được token sai.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/sites", nil)
	r.RemoteAddr, r.Host = m20IP+":1", m20Host
	r.Header.Set("Authorization", "Bearer x")
	r.AddCookie(&http.Cookie{Name: authCookiePlain, Value: "x"})
	r.AddCookie(e.session(m20User))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Authorization sai kèm cookie đúng: %d", rec.Code)
	}
	// Lần dùng được ghi.
	list, _ := e.st.Tokens(context.Background(), m20User)
	if len(list) != 1 || list[0].LastUsed.IsZero() || list[0].LastIP != m20IP {
		t.Fatalf("lần dùng chưa được ghi: %+v", list)
	}
}

// TestTokenTheoVaiHienTai (B4): quyền của token theo vai hiện tại của chủ.
func TestTokenTheoVaiHienTai(t *testing.T) {
	e := newM20Env(t)
	ctx := context.Background()
	h, _ := e.fakeHandler()
	e.must(e.st.CreateUser(ctx, account.User{Name: "boss", Role: account.RoleAdmin, Created: e.now}, "x"))
	raw, _ := e.token("boss", "system:write", time.Hour)
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/machine", nil)
		r.RemoteAddr, r.Host = m20IP+":1", m20Host
		r.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if rec := get(); rec.Code != authzStatus {
		t.Fatalf("admin: %d", rec.Code)
	}
	u, _, _ := e.st.User(ctx, "boss")
	u.Role = account.RoleUser
	e.must(e.st.UpdateUser(ctx, u))
	if rec := get(); rec.Code != http.StatusForbidden || errCode(t, rec) != "forbidden" {
		t.Fatalf("hạ vai: %d %s", rec.Code, rec.Body)
	}
	u.Role, u.Status = account.RoleAdmin, account.Suspended
	e.must(e.st.UpdateUser(ctx, u))
	if rec := get(); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "token_invalid" {
		t.Fatalf("khoá user: %d %s", rec.Code, rec.Body)
	}
	u.Status = account.Active
	e.must(e.st.UpdateUser(ctx, u))
	e.must(e.st.DeleteUser(ctx, "boss"))
	if rec := get(); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "token_invalid" {
		t.Fatalf("xoá user: %d %s", rec.Code, rec.Body)
	}
}

func TestTokenUserKhongChonDuocSystem(t *testing.T) {
	e := newM20Env(t)
	c := e.session(m20User)
	rec := e.post("/api/v1/account/tokens", TokenCreate{Name: "ci", Scopes: []string{"system:read"}, Days: 1, Password: authTestPassword}, c)
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "token_scope_invalid" {
		t.Fatalf("user chọn system: %d %s", rec.Code, rec.Body)
	}
	var mine MyTokens
	rec = e.req(http.MethodGet, "/api/v1/account/tokens", "", m20IP, []*http.Cookie{c})
	e.must(json.Unmarshal(rec.Body.Bytes(), &mine))
	if slices.Contains(mine.Groups, "system") || mine.MaxDays != apitoken.DefaultMaxDays || mine.Tokens == nil {
		t.Fatalf("MyTokens của user: %+v", mine)
	}
}

func TestTaoTokenCanMatKhau(t *testing.T) {
	e := newM20Env(t)
	c := e.session(m20User)
	in := TokenCreate{Name: "ci", Scopes: []string{"sites:read"}, Days: 30, Password: "sai-mat-khau-roi"}
	if rec := e.post("/api/v1/account/tokens", in, c); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "wrong_password" {
		t.Fatalf("sai mật khẩu: %d %s", rec.Code, rec.Body)
	}
	if n, _ := e.st.ActiveTokens(context.Background(), m20User, e.now); n != 0 {
		t.Fatal("sai mật khẩu mà vẫn tạo token")
	}
	in.Password = authTestPassword
	rec := e.post("/api/v1/account/tokens", in, c)
	var out TokenCreated
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusCreated || !apitoken.WellFormed(out.Token) || rec.Header().Get("Cache-Control") != "no-store" ||
		out.Row.Status != "active" || !out.Row.Expires.Equal(e.now.Add(30*24*time.Hour)) {
		t.Fatalf("tạo token: %d %s", rec.Code, rec.Body)
	}
	// Bằng token thì không tạo được token (B5).
	b, _ := json.Marshal(in)
	rec = e.req(http.MethodPost, "/api/v1/account/tokens", string(b), m20IP, nil, bearer(out.Token)...)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "token_forbidden" {
		t.Fatalf("token tạo token: %d %s", rec.Code, rec.Body)
	}
	// Thu hồi: của người khác là 404 như không có.
	other := e.session(account.AdminName)
	if rec := e.req(http.MethodDelete, "/api/v1/account/tokens/"+out.Row.ID, "", m20IP, []*http.Cookie{other}); rec.Code != http.StatusNotFound {
		t.Fatalf("thu hồi token của người khác: %d", rec.Code)
	}
	if rec := e.req(http.MethodDelete, "/api/v1/account/tokens/"+out.Row.ID, "", m20IP, []*http.Cookie{c}); rec.Code != http.StatusNoContent {
		t.Fatalf("thu hồi token của mình: %d", rec.Code)
	}
	rec = e.req(http.MethodGet, "/api/v1/sites", "", m20IP, nil, bearer(out.Token)...)
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "token_revoked" {
		t.Fatalf("token đã thu hồi: %d %s", rec.Code, rec.Body)
	}
}

func TestTaoTokenCanMaKhiCoHaiBuoc(t *testing.T) {
	e := newM20Env(t)
	secret, _ := e.enable(m20User)
	c := e.session(m20User)
	in := TokenCreate{Name: "ci", Scopes: []string{"sites:read"}, Days: 1, Password: authTestPassword}
	if rec := e.post("/api/v1/account/tokens", in, c); rec.Code != http.StatusBadRequest || errCode(t, rec) != "mfa_code_required" {
		t.Fatalf("thiếu mã: %d %s", rec.Code, rec.Body)
	}
	in.Code = e.wrongCode(secret)
	if rec := e.post("/api/v1/account/tokens", in, c); rec.Code != http.StatusBadRequest || errCode(t, rec) != "mfa_code_wrong" {
		t.Fatalf("sai mã: %d %s", rec.Code, rec.Body)
	}
	in.Code = e.code(secret)
	if rec := e.post("/api/v1/account/tokens", in, c); rec.Code != http.StatusCreated {
		t.Fatalf("đúng mã: %d %s", rec.Code, rec.Body)
	}
	// Đổi mật khẩu cũng cần mã; còn token sống thì 200 {active_tokens}.
	pc := PasswordChange{Current: authTestPassword, New: "mat-khau-moi-dai-du"}
	if rec := e.post("/api/v1/account/password", pc, c); rec.Code != http.StatusBadRequest || errCode(t, rec) != "mfa_code_required" {
		t.Fatalf("đổi mật khẩu thiếu mã: %d %s", rec.Code, rec.Body)
	}
	e.now = e.now.Add(mfa.Period * time.Second) // mã của bước sau (bước trước đã dùng)
	pc.Code = e.code(secret)
	rec := e.post("/api/v1/account/password", pc, c)
	var out PasswordChanged
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || out.ActiveTokens != 1 {
		t.Fatalf("đổi mật khẩu: %d %s", rec.Code, rec.Body)
	}
}

func TestBatHaiBuocCanMatKhau(t *testing.T) {
	e := newM20Env(t)
	c, other := e.session(m20User), e.session(m20User)
	rec := e.post("/api/v1/account/2fa/setup", struct{}{}, c)
	var setup MFASetup
	_ = json.Unmarshal(rec.Body.Bytes(), &setup)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || len(setup.QR) == 0 ||
		setup.Account != m20User+"@may-thu" || !strings.HasPrefix(setup.URI, "otpauth://totp/TPAN:") {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	e.must(err)
	if rec := e.post("/api/v1/account/2fa/enable", MFAConfirm{Password: "sai-mat-khau-roi", Code: e.code(secret)}, c); errCode(t, rec) != "wrong_password" {
		t.Fatalf("bật với mật khẩu sai: %d %s", rec.Code, rec.Body)
	}
	if on, _ := e.st.TOTPEnabled(context.Background(), m20User); on {
		t.Fatal("mật khẩu sai mà vẫn bật")
	}
	if rec := e.post("/api/v1/account/2fa/enable", MFAConfirm{Password: authTestPassword, Code: e.wrongCode(secret)}, c); rec.Code != http.StatusBadRequest || errCode(t, rec) != "mfa_code_wrong" {
		t.Fatalf("bật với mã sai: %d %s", rec.Code, rec.Body)
	}
	rec = e.post("/api/v1/account/2fa/enable", MFAConfirm{Password: authTestPassword, Code: e.code(secret)}, c)
	var codes RecoveryCodes
	_ = json.Unmarshal(rec.Body.Bytes(), &codes)
	if rec.Code != http.StatusOK || len(codes.Codes) != mfa.RecoveryCount || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bật: %d %s", rec.Code, rec.Body)
	}
	// Bật xong đăng xuất mọi phiên khác của user, giữ phiên đang bật.
	if rec := e.req(http.MethodGet, "/api/v1/account/2fa", "", m20IP, []*http.Cookie{other}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("phiên khác phải bị đăng xuất: %d", rec.Code)
	}
	rec = e.req(http.MethodGet, "/api/v1/account/2fa", "", m20IP, []*http.Cookie{c})
	var st MFAState
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.Enabled || st.RecoveryLeft != mfa.RecoveryCount || st.Policy != PolicyOff {
		t.Fatalf("MFAState: %+v", st)
	}
	// Tạo lại mã dự phòng rồi tắt: mật khẩu + mã.
	e.now = e.now.Add(mfa.Period * time.Second)
	rec = e.post("/api/v1/account/2fa/recovery", MFAConfirm{Password: authTestPassword, Code: e.code(secret)}, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("tạo lại mã dự phòng: %d %s", rec.Code, rec.Body)
	}
	if rec := e.post("/api/v1/account/2fa/disable", MFAConfirm{Password: authTestPassword, Code: codes.Codes[0]}, c); errCode(t, rec) != "mfa_code_wrong" {
		t.Fatalf("mã dự phòng của bộ cũ phải hết hiệu lực: %d %s", rec.Code, rec.Body)
	}
	// Chính sách bắt buộc thì không tắt được.
	e.must(e.st.SetSetting(context.Background(), settingRequire2FA, PolicyAll))
	if rec := e.post("/api/v1/account/2fa/disable", MFAConfirm{Password: authTestPassword, Code: "x"}, c); errCode(t, rec) != "mfa_required_by_policy" {
		t.Fatalf("tắt khi bị bắt buộc: %d %s", rec.Code, rec.Body)
	}
	e.must(e.st.SetSetting(context.Background(), settingRequire2FA, PolicyOff))
	e.now = e.now.Add(mfa.Period * time.Second)
	if rec := e.post("/api/v1/account/2fa/disable", MFAConfirm{Password: authTestPassword, Code: e.code(secret)}, c); rec.Code != http.StatusNoContent {
		t.Fatalf("tắt: %d %s", rec.Code, rec.Body)
	}
}

// TestDuaSetupVoiEnable (mục 11.5): một lần setup chen vào giữa lúc so mã và
// lúc bật thì không bật bí mật mà người dùng chưa từng quét.
func TestDuaSetupVoiEnable(t *testing.T) {
	e := newM20Env(t)
	c := e.session(m20User)
	setup := func() []byte {
		rec := e.post("/api/v1/account/2fa/setup", struct{}{}, c)
		var s MFASetup
		e.must(json.Unmarshal(rec.Body.Bytes(), &s))
		b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s.Secret)
		e.must(err)
		return b
	}
	first := setup()
	second := setup()
	// Mã của bí mật đã bị ghi đè không khớp bí mật chờ hiện tại.
	if rec := e.post("/api/v1/account/2fa/enable", MFAConfirm{Password: authTestPassword, Code: e.code(first)}, c); errCode(t, rec) != "mfa_code_wrong" {
		t.Fatalf("mã của bí mật cũ: %d %s", rec.Code, rec.Body)
	}
	// Setup chen vào đúng khe giữa so mã và EnableTOTP.
	e.s.beforeEnable = func() { e.s.beforeEnable = nil; setup() }
	if rec := e.post("/api/v1/account/2fa/enable", MFAConfirm{Password: authTestPassword, Code: e.code(second)}, c); rec.Code != http.StatusConflict || errCode(t, rec) != "mfa_not_pending" {
		t.Fatalf("setup chen giữa: %d %s", rec.Code, rec.Body)
	}
	if on, _ := e.st.TOTPEnabled(context.Background(), m20User); on {
		t.Fatal("không được bật bí mật chưa ai quét")
	}
}

// TestBatBuocChiVaoTrangBat (B8): admin chưa bật hai bước khi chính sách all,
// bằng phiên hay token, chỉ gọi được route có allowEnroll.
func TestBatBuocChiVaoTrangBat(t *testing.T) {
	e := newM20Env(t)
	e.must(e.st.SetSetting(context.Background(), settingRequire2FA, PolicyAll))
	h, rs := e.fakeHandler()
	c := e.session(account.AdminName)
	raw, _ := e.token(account.AdminName, allScopes(), time.Hour)
	ae := &authzEnv{t: t, incident: "1", backup: "x1"}
	for _, rt := range rs {
		if rt.rule.kind == rulePublic {
			continue
		}
		method, p := splitPattern(rt.pattern)
		path := ae.fill(t, rt, p, false)
		run := func(cs []*http.Cookie, hdr ...string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, strings.NewReader("{}"))
			r.RemoteAddr, r.Host = m20IP+":1", m20Host
			for _, ck := range cs {
				r.AddCookie(ck)
			}
			for i := 0; i+1 < len(hdr); i += 2 {
				r.Header.Set(hdr[i], hdr[i+1])
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			return rec
		}
		rec := run([]*http.Cookie{{Name: authCookiePlain, Value: c.Value}})
		if rt.allowEnroll {
			if rec.Code != authzStatus {
				t.Errorf("%s (allowEnroll) bằng phiên: %d", rt.pattern, rec.Code)
			}
		} else if rec.Code != http.StatusForbidden || errCode(t, rec) != "mfa_enroll_required" {
			t.Errorf("%s bằng phiên: %d %s", rt.pattern, rec.Code, rec.Body)
		}
		rec = run(nil, bearer(raw)...)
		want := "mfa_enroll_required"
		if rt.tok == "" {
			want = "token_forbidden"
		}
		if rec.Code != http.StatusForbidden || errCode(t, rec) != want {
			t.Errorf("%s bằng token: %d %s, muốn %s", rt.pattern, rec.Code, rec.Body, want)
		}
	}
	info := e.sessionInfo(&http.Cookie{Name: authCookieTLS, Value: c.Value})
	if !info.MFAEnrollRequired {
		t.Fatalf("SessionInfo phải báo mfa_enroll_required: %+v", info)
	}
}

func TestChinhSachAuth(t *testing.T) {
	e := newM20Env(t)
	c := e.session(account.AdminName)
	all := PolicyAll
	if rec := e.post("/api/v1/settings/auth", AuthSettingsInput{Require2FA: &all}, c); errCode(t, rec) != "mfa_enroll_self_first" {
		t.Fatalf("bật chính sách khi chưa bật cho mình: %d %s", rec.Code, rec.Body)
	}
	bad := "on"
	if rec := e.post("/api/v1/settings/auth", AuthSettingsInput{Require2FA: &bad}, c); errCode(t, rec) != "policy_invalid" {
		t.Fatalf("chính sách lạ: %d %s", rec.Code, rec.Body)
	}
	days := 400
	if rec := e.post("/api/v1/settings/auth", AuthSettingsInput{TokenMaxDays: &days}, c); errCode(t, rec) != "token_days_invalid" {
		t.Fatalf("trần ngày quá lớn: %d %s", rec.Code, rec.Body)
	}
	e.enable(account.AdminName) // bật xong mọi phiên khác bị đăng xuất
	c = e.session(account.AdminName)
	days = 30
	rec := e.post("/api/v1/settings/auth", AuthSettingsInput{Require2FA: &all, TokenMaxDays: &days}, c)
	var out AuthSettings
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || out.Require2FA != PolicyAll || out.TokenMaxDays != 30 || len(out.TokenGroups) != len(apitoken.Groups) {
		t.Fatalf("đặt chính sách: %d %s", rec.Code, rec.Body)
	}
}

// TestNhomTokenCuaRoute (B10): bảng nhóm token viết tay theo mục 3.7 của
// thiết kế (cộng route M18 trên dev). Route không public nào vắng ở đây, hay
// khác nhóm, là đỏ.
func TestNhomTokenCuaRoute(t *testing.T) {
	want := map[string]string{}
	add := func(g string, ps ...string) {
		for _, p := range ps {
			want[p] = g
		}
	}
	add("sites", "GET /api/v1/sites", "POST /api/v1/sites", "GET /api/v1/sites/{domain}", "DELETE /api/v1/sites/{domain}",
		"POST /api/v1/sites/{domain}/ssl", "POST /api/v1/sites/{domain}/restore", "POST /api/v1/sites/{domain}/php",
		"POST /api/v1/sites/{domain}/htaccess", "POST /api/v1/sites/{domain}/suspend", "POST /api/v1/sites/{domain}/unsuspend",
		"GET /api/v1/sites/{domain}/traffic", "GET /api/v1/sites/{domain}/log", "GET /api/v1/sites/{domain}/route",
		"GET /api/v1/trash", "DELETE /api/v1/trash/{domain}", "POST /api/v1/apply", "GET /api/v1/export", "GET /api/v1/php")
	add("databases", "GET /api/v1/databases", "POST /api/v1/databases/{domain}/password", "POST /api/v1/databases/{domain}/dump",
		"POST /api/v1/databases/{domain}/import", "POST /api/v1/databases/{domain}/charset", "POST /api/v1/databases/{domain}/suspend",
		"POST /api/v1/databases/{domain}/unsuspend", "GET /api/v1/collations", "POST /api/v1/dbs", "POST /api/v1/dbs/{name}",
		"POST /api/v1/dbs/{name}/password", "POST /api/v1/dbs/{name}/suspend", "POST /api/v1/dbs/{name}/unsuspend",
		"POST /api/v1/dbs/{name}/restore", "DELETE /api/v1/dbs/{name}", "DELETE /api/v1/trash/dbs/{name}")
	add("backups", "GET /api/v1/backups", "POST /api/v1/sites/{domain}/backups", "POST /api/v1/backups/{id}/restore",
		"POST /api/v1/backups/settings", "POST /api/v1/backups/{id}/push", "POST /api/v1/backups/{id}/pull", "GET /api/v1/backups/remote")
	add("cron", "GET /api/v1/cron")
	add("jobs", "GET /api/v1/jobs", "GET /api/v1/jobs/{id}", "GET /api/v1/jobs/{id}/events")
	add("health", "GET /api/v1/overview", "GET /api/v1/checks", "GET /api/v1/incidents", "GET /api/v1/incidents/{id}",
		"POST /api/v1/fixes", "POST /api/v1/files/accept", "POST /api/v1/health/run")
	add("ssl", "GET /api/v1/ssl", "POST /api/v1/ssl/settings")
	add("system", "GET /api/v1/machine", "GET /api/v1/stack", "POST /api/v1/stack/switch", "GET /api/v1/security",
		"POST /api/v1/security/ufw", "POST /api/v1/security/fail2ban", "POST /api/v1/security/harden", "GET /api/v1/security/isolation",
		"POST /api/v1/php/install", "POST /api/v1/php/remove", "GET /api/v1/updates", "POST /api/v1/updates", "POST /api/v1/updates/auto",
		"POST /api/v1/mariadb/upgrade", "POST /api/v1/trash/settings", "GET /api/v1/self", "POST /api/v1/self/update",
		"POST /api/v1/self/rollback", "GET /api/v1/panel", "POST /api/v1/panel/domain")
	// Chỉ phiên (nhóm rỗng): quản lý tài khoản, mật khẩu, hai bước, token,
	// user, gói, Plan, AI, chính sách (B5).
	add("", "POST /api/v1/account/password", "GET /api/v1/account/sessions", "DELETE /api/v1/account/sessions/{id}",
		"GET /api/v1/plans", "GET /api/v1/plans/{id}", "POST /api/v1/plans/{id}/approve", "POST /api/v1/plans/{id}/reject", "GET /api/v1/agent",
		"GET /api/v1/ai/brain", "POST /api/v1/ai/preview", "POST /api/v1/ai/ask", "GET /api/v1/ai/sessions", "GET /api/v1/ai/sessions/{id}",
		"GET /api/v1/ai/sessions/{id}/turns/{seq}/events", "POST /api/v1/ai/views",
		"GET /api/v1/users", "POST /api/v1/users", "GET /api/v1/users/{name}", "POST /api/v1/users/{name}", "DELETE /api/v1/users/{name}",
		"POST /api/v1/users/{name}/password", "POST /api/v1/users/{name}/suspend", "POST /api/v1/users/{name}/unsuspend",
		"POST /api/v1/users/{name}/trash-all", "POST /api/v1/users/{name}/view-as", "POST /api/v1/session/view-as/exit",
		"GET /api/v1/packages", "POST /api/v1/packages", "POST /api/v1/packages/{name}", "DELETE /api/v1/packages/{name}",
		"GET /api/v1/account/2fa", "POST /api/v1/account/2fa/setup", "POST /api/v1/account/2fa/enable", "POST /api/v1/account/2fa/disable",
		"POST /api/v1/account/2fa/recovery", "GET /api/v1/account/tokens", "POST /api/v1/account/tokens", "DELETE /api/v1/account/tokens/{id}",
		"GET /api/v1/settings/auth", "POST /api/v1/settings/auth", "GET /api/v1/tokens", "DELETE /api/v1/tokens/{id}",
		// Soát cuối F2: đổi đích sao lưu, cloud-init.
		"GET /api/v1/backups/target", "POST /api/v1/backups/target", "DELETE /api/v1/backups/target",
		"POST /api/v1/backups/target/test", "POST /api/v1/cloud-init/render")
	seen := map[string]bool{}
	for _, ri := range RouteTable() {
		seen[ri.Pattern] = true
		if ri.Rule == "public" {
			if ri.TokenGroup != "" {
				t.Errorf("%s public mà có nhóm token %q", ri.Pattern, ri.TokenGroup)
			}
			continue
		}
		g, ok := want[ri.Pattern]
		if !ok {
			t.Errorf("route %s chưa có quyết định token trong bảng của test", ri.Pattern)
			continue
		}
		if ri.TokenGroup != g {
			t.Errorf("%s: nhóm %q, muốn %q", ri.Pattern, ri.TokenGroup, g)
		}
	}
	for p := range want {
		if !seen[p] {
			t.Errorf("bảng của test có %s mà server không có", p)
		}
	}
}

// TestTokenChiDocKhongLayMatKhauJob (mục 11.3, R9).
func TestTokenChiDocKhongLayMatKhauJob(t *testing.T) {
	e := newM20Env(t)
	ctx := context.Background()
	const job = "00000000000000bb"
	e.must(e.st.CreateJob(ctx, engine.Job{ID: job, Kind: "site.add", Target: "x.example.com", Status: engine.JobSucceeded, Started: e.now}))
	rawA, idA := e.token(account.AdminName, "jobs:read,sites:write", time.Hour)
	rawB, _ := e.token(account.AdminName, "jobs:read", time.Hour)
	e.s.api.secrets[job] = heldSecret{one: &AdminSecret{User: "wp", Password: "bi-mat-wp"}, starterToken: idA}
	get := func(hdr []string, cs ...*http.Cookie) JobDetail {
		rec := e.req(http.MethodGet, "/api/v1/jobs/"+job, "", m20IP, cs, hdr...)
		var d JobDetail
		if rec.Code != http.StatusOK {
			t.Fatalf("GET job: %d %s", rec.Code, rec.Body)
		}
		e.must(json.Unmarshal(rec.Body.Bytes(), &d))
		return d
	}
	if d := get(nil, e.session(account.AdminName)); d.Secret != nil {
		t.Fatal("phiên lấy được mật khẩu của job do token khởi")
	}
	if d := get(bearer(rawB)); d.Secret != nil {
		t.Fatal("token khác lấy được mật khẩu")
	}
	if d := get(bearer(rawA)); d.Secret == nil || d.Secret.Password != "bi-mat-wp" {
		t.Fatal("token đã khởi job phải lấy được mật khẩu (các lần hỏi trước không được làm mất nó)")
	}
	if d := get(bearer(rawA)); d.Secret != nil {
		t.Fatal("mật khẩu chỉ hiện một lần")
	}
	// Job do phiên khởi: token không lấy được, phiên lấy được.
	e.s.api.secrets[job] = heldSecret{one: &AdminSecret{Password: "p2"}}
	if d := get(bearer(rawA)); d.Secret != nil {
		t.Fatal("token lấy được mật khẩu của job do phiên khởi")
	}
	if d := get(nil, e.session(account.AdminName)); d.Secret == nil {
		t.Fatal("phiên phải lấy được mật khẩu của job do phiên khởi")
	}
}

// TestJobGhiTokenKhoi: jobStart ghi token đã khởi job cạnh mật khẩu.
func TestJobGhiTokenKhoi(t *testing.T) {
	e := newM20Env(t)
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), tokenCtxKey{}, store.TokenRow{ID: "tok1"}))
	e.s.jobStart(httptest.NewRecorder(), r, jobOpts{action: "x"}, func(_ context.Context, on func(engine.Event)) (jobResult, error) {
		on(engine.Event{JobID: "j1", Kind: "job.start"})
		return jobResult{status: engine.JobSucceeded, secret: &AdminSecret{Password: "p"}}, nil
	})
	e.s.api.wg.Wait()
	if got := e.s.api.secrets["j1"].starterToken; got != "tok1" {
		t.Fatalf("starterToken = %q", got)
	}
}
