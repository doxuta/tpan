package server

// Phần đăng nhập của API (mục 11 của docs/KE-HOACH.md): đăng nhập, phiên,
// CSRF, header bảo mật, đổi mật khẩu, danh sách phiên. Hash mật khẩu và các
// truy vấn nằm ở internal/store/auth.go.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
)

const (
	// authIdle: phiên không dùng quá 12 giờ thì hết hạn (mục 11).
	authIdle = 12 * time.Hour
	// Sai authMaxFailures lần trong authFailWindow thì khoá IP authLockFor.
	authMaxFailures = 5
	authFailWindow  = 15 * time.Minute
	authLockFor     = 15 * time.Minute
	// authTouchEvery: chỉ ghi last_seen khi lần ghi trước đã cũ hơn chừng này.
	// Giao diện hỏi /overview liên tục; ghi DB mỗi request là phí mà không
	// làm phiên chính xác hơn đáng kể so với 12 giờ.
	authTouchEvery = time.Minute
	// authMinPassword là độ dài tối thiểu (tính theo ký tự) của mật khẩu mới.
	authMinPassword = 12
	// authMaxBody chặn thân request quá lớn trước khi đọc JSON: chỉ có một
	// mật khẩu trong đó, và argon2 trên vài MB là cách làm chậm máy rẻ tiền.
	authMaxBody = 4 << 10

	// Tên cookie khi có TLS. Tiền tố __Host- bắt trình duyệt chỉ nhận cookie có
	// Secure, Path=/ và không Domain, nên trang khác trên cùng tên miền cha
	// không ghi đè được.
	authCookieTLS = "__Host-tpan"
	// Tên cookie khi KHÔNG có TLS (make dev-api, --demo trên 127.0.0.1).
	// Trang http:// không đặt được cookie Secure (và vì thế cũng không đặt được
	// __Host-); trình duyệt chỉ miễn cho "localhost" và không phải trình duyệt
	// nào cũng miễn (MDN, Set-Cookie). Nên khi không TLS dùng tên thường,
	// không Secure. Máy chủ thật luôn chạy TLS (tpan setup).
	authCookiePlain = "tpan"
)

// authState là trạng thái của phần đăng nhập.
type authState struct {
	// cop chặn request có thay đổi đến từ trang khác (CSRF), dựa vào header
	// Sec-Fetch-Site và Origin mà trình duyệt tự gửi (Go 1.25+).
	cop *http.CrossOriginProtection
	// gates: các lần thử của một khoá nguồn chạy lần lượt (auth_m28.go).
	gates authGates
	// names: phần lần sai của từng tên trong mỗi khoá nguồn (auth_m28.go).
	names *authNames
	// rate: giới hạn số lần so mật khẩu mỗi phút (auth_m28.go, D6).
	rate authRate
	// verify và hash là store.VerifyPassword, store.HashPassword; test thay để
	// đếm số lần so mật khẩu hay giả lúc argon2 bận.
	verify func(hash, pw string) (bool, error)
	hash   func(pw string) (string, error)
}

func newAuthState(*Server) *authState {
	return &authState{
		cop: http.NewCrossOriginProtection(), names: newAuthNames(),
		verify: store.VerifyPassword, hash: store.HashPassword,
	}
}

// authCtxKey là khoá context giữ phiên của request đã qua guard (routes.go).
type authCtxKey struct{}

// authSessionOf trả phiên mà guard đã gắn vào request.
func authSessionOf(r *http.Request) (store.Session, bool) {
	ss, ok := r.Context().Value(authCtxKey{}).(store.Session)
	return ss, ok
}

// secure thêm header bảo mật (mục 11) cho mọi response và chặn CSRF cho
// request có thay đổi. Không bọc ResponseWriter, để SSE vẫn Flush được.
func (s *Server) secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		// default-src 'self': trang chỉ nạp script, style, font, ảnh từ chính
		// panel; một đoạn script lọt vào trang cũng không gửi dữ liệu đi đâu được.
		// frame-ancestors 'none': không trang nào nhúng panel vào iframe để lừa bấm.
		hd.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		// GET, HEAD, OPTIONS luôn qua; POST, DELETE từ origin khác thì bị chặn.
		if err := s.auth.cop.Check(r); err != nil {
			writeError(w, http.StatusForbidden, "csrf", i18n.T(
				"Yêu cầu đến từ một trang khác nên bị chặn (chống CSRF). Làm lại từ chính giao diện tpan.",
				"The request came from another site and was blocked (CSRF protection). Use the tpan interface itself."))
			return
		}
		// sourceKey (auth_m28.go) cần System để tra UID của kết nối loopback.
		if s.deps.Sys != nil {
			r = r.WithContext(context.WithValue(r.Context(), authSysKey{}, s.deps.Sys))
		}
		h.ServeHTTP(w, r)
	})
}

// authResolve đọc phiên của request và dựng danh tính (M13): user của phiên
// phải còn trong bảng users và đang active, không thì xoá phiên và coi như
// chưa đăng nhập (khoá user là đăng xuất ngay, kể cả phiên chưa bị huỷ kịp).
// Phiên "xem như" (ViewAs): Name, Role là của user được xem, ViewedBy là admin
// thật; admin đó không còn vai admin thì cũng xoá phiên, user được xem không
// còn thì bỏ "xem như" và quay về chính admin.
//
// Danh tính của user thường luôn dựng từ một dòng users có thật, nên Name
// không bao giờ rỗng (scopeOf dựa vào điều này).
func (s *Server) authResolve(r *http.Request) (store.Session, account.Identity, bool, error) {
	ss, ok, err := s.authCurrent(r)
	if err != nil || !ok {
		return store.Session{}, account.Identity{}, false, err
	}
	ctx := r.Context()
	st := s.deps.Store
	drop := func() (store.Session, account.Identity, bool, error) {
		return store.Session{}, account.Identity{}, false, st.DeleteSessionByToken(ctx, ss.TokenSHA256)
	}
	u, found, err := st.User(ctx, ss.UserName)
	if err != nil {
		return store.Session{}, account.Identity{}, false, err
	}
	if !found || u.Name == "" || u.Status != account.Active {
		return drop()
	}
	id := account.Identity{Name: u.Name, Role: u.Role}
	if ss.ViewAs == "" {
		return ss, id, true, nil
	}
	if u.Role != account.RoleAdmin {
		return drop()
	}
	v, found, err := st.User(ctx, ss.ViewAs)
	if err != nil {
		return store.Session{}, account.Identity{}, false, err
	}
	if !found || v.Name == "" {
		if err := st.SetViewAs(ctx, ss.TokenSHA256, ""); err != nil {
			return store.Session{}, account.Identity{}, false, err
		}
		ss.ViewAs = ""
		return ss, id, true, nil
	}
	return ss, account.Identity{Name: v.Name, Role: v.Role, ViewedBy: u.Name}, true, nil
}

// authCurrent tìm phiên của request theo cookie. Phiên quá 12 giờ không dùng
// thì xoá và coi như không có. Phiên còn sống thì cập nhật last_seen.
func (s *Server) authCurrent(r *http.Request) (store.Session, bool, error) {
	st := s.deps.Store
	if st == nil {
		return store.Session{}, false, nil
	}
	c, err := r.Cookie(authCookieName(r))
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return store.Session{}, false, nil
	}
	sum := authTokenHash(c.Value)
	ss, err := st.SessionByToken(r.Context(), sum)
	if errors.Is(err, store.ErrNotFound) {
		return store.Session{}, false, nil
	}
	if err != nil {
		return store.Session{}, false, err
	}
	now := s.now()
	if now.Sub(ss.LastSeen) > authIdle {
		if err := st.DeleteSessionByToken(r.Context(), sum); err != nil {
			return store.Session{}, false, err
		}
		return store.Session{}, false, nil
	}
	if now.Sub(ss.LastSeen) > authTouchEvery {
		if err := st.TouchSession(r.Context(), sum, now); err != nil {
			return store.Session{}, false, err
		}
		ss.LastSeen = now
	}
	return ss, true, nil
}

// authCookieName: có TLS thì __Host-tpan, không thì tpan (lý do ở hằng số).
// Chọn theo từng request, nên cookie đặt qua http không được nhận qua https.
func authCookieName(r *http.Request) string {
	if r.TLS != nil {
		return authCookieTLS
	}
	return authCookiePlain
}

// authSetCookie đặt (maxAge 0: cookie phiên của trình duyệt) hoặc xoá
// (maxAge < 0) cookie đăng nhập. Không đặt Expires: phiên hết hạn do server
// quyết (12 giờ không dùng), cookie mất khi đóng trình duyệt.
func authSetCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName(r),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   r.TLS != nil,
		HttpOnly: true, // script trên trang không đọc được token
		SameSite: http.SameSiteStrictMode,
	})
}

// authTokenHash là sha256 (hex) của token: thứ duy nhất DB giữ. Lộ DB thì kẻ
// đọc được cũng không dựng lại được cookie.
func authTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// authRandom trả n byte ngẫu nhiên từ crypto/rand.
func authRandom(n int) []byte {
	b := make([]byte, n)
	// crypto/rand.Read không bao giờ trả lỗi từ Go 1.24 (nó tự dừng chương
	// trình nếu hệ điều hành không cho số ngẫu nhiên).
	_, _ = rand.Read(b)
	return b
}

// authNoStore: Deps.Store nil chỉ xảy ra trong test của các phần khác.
func authNoStore(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "no_store", i18n.T(
		"Server chưa mở DB.", "The server has no database open."))
}

// authInternal ghi log lỗi và trả 500. Không bao giờ có mật khẩu trong err:
// các lỗi ở đây đến từ DB và JSON.
func authInternal(w http.ResponseWriter, what string, err error) {
	slog.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", i18n.T(
		"Lỗi trong server; xem log của tpan.", "Server error; see the tpan log."))
}

// authDecode đọc JSON từ thân request, giới hạn authMaxBody.
func authDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, authMaxBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", i18n.T(
			"Thân yêu cầu không phải JSON hợp lệ.", "The request body is not valid JSON."))
		return false
	}
	return true
}

// authLocked trả 423 nếu khoá nguồn src đang bị khoá; true nghĩa là đã trả
// lời. Phải gọi trong lượt của src (s.auth.gates): kiểm ngoài lượt thì các
// lượt đang chờ đã qua kiểm, lọt tới 9 lần so mật khẩu.
func (s *Server) authLocked(w http.ResponseWriter, r *http.Request, src string) bool {
	until, locked, err := s.deps.Store.LoginLockedUntil(r.Context(), src, s.now())
	if err != nil {
		authInternal(w, "đọc khoá đăng nhập", err)
		return true
	}
	if !locked && !until.IsZero() {
		// Khoá cũ đã hết hạn: phần theo tên của cửa sổ đó bỏ hết (S9).
		s.auth.names.dropSource(src)
	}
	if locked {
		authWriteLocked(w, src, until.Sub(s.now()))
	}
	return locked
}

func authWriteLocked(w http.ResponseWriter, src string, left time.Duration) {
	mins := int(left.Minutes()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
	writeError(w, http.StatusLocked, "login_locked", i18n.T(
		"Sai mật khẩu quá nhiều lần từ nơi gửi này ("+src+"); bị khoá thêm "+strconv.Itoa(mins)+" phút. Gỡ khoá ngay (không đổi mật khẩu), chạy trên máy chủ: sudo tpan admin unlock "+src+". Quên mật khẩu quản trị: sudo tpan admin passwd",
		"Too many wrong passwords from this source ("+src+"); locked for "+strconv.Itoa(mins)+" more minutes. To unlock now (password unchanged), run on the server: sudo tpan admin unlock "+src+". Forgot the admin password: sudo tpan admin passwd"))
}

// authFailed đếm một lần sai mật khẩu của tên name từ khoá nguồn src, ghi
// nhật ký, rồi trả 401 hoặc 423 (lần sai thứ 5 khoá ngay). action là thao tác
// ghi vào nhật ký.
func (s *Server) authFailed(w http.ResponseWriter, r *http.Request, src, name, action string) {
	now := s.now()
	f, err := s.deps.Store.RecordLoginFailure(r.Context(), src, now,
		authMaxFailures, authFailWindow, authLockFor)
	if err != nil {
		authInternal(w, "đếm đăng nhập sai", err)
		return
	}
	s.auth.names.failed(src, name, f, now)
	// Target là khoá nguồn (D12): với kết nối từ chính máy nó là uid:<n>, nên
	// admin đọc nhật ký tra ra được site nào đang dò mật khẩu (cột ip chỉ có
	// 127.0.x.y). Không bao giờ ghi tên đã gõ.
	if f.Locked {
		s.audit(r, action, src, "locked", "")
		authWriteLocked(w, src, f.Until.Sub(now))
		return
	}
	s.audit(r, action, src, "wrong_password", "")
	writeError(w, http.StatusUnauthorized, "wrong_password", i18n.T(
		"Sai mật khẩu.", "Wrong password."))
}

// authDummy là một hash argon2id thật của một chuỗi ngẫu nhiên, tính một lần.
// Đăng nhập bằng tên không có thật vẫn chạy argon2 trên hash này, để thời gian
// trả lời không cho biết tên nào tồn tại. Phải là hash thật: VerifyPassword trả
// lỗi ngay với hash hỏng, mà trả nhanh chính là lộ.
//
// Run tính nó trước khi nghe cổng (S11 của M28 đợt 2), bằng HashPasswordWait
// nên không bao giờ gặp ErrPasswordBusy. Lỗi không được nhớ: trước đây
// sync.Once giữ luôn lỗi, một lần bận lúc mới khởi động là mọi tên lạ trả 500
// mãi, còn tên thật trả 401: lộ tên user. Đường NewHandler (test) tính lười.
var authDummy struct {
	mu   sync.Mutex
	hash string
}

func authDummyHash() (string, error) {
	authDummy.mu.Lock()
	defer authDummy.mu.Unlock()
	if authDummy.hash == "" {
		h, err := store.HashPasswordWait(hex.EncodeToString(authRandom(16)))
		if err != nil {
			return "", err
		}
		authDummy.hash = h
	}
	return authDummy.hash, nil
}

// authCheckPassword so pw với hash của user name. Không có user đó thì vẫn so
// với hash giả (cùng thời gian) và trả ok=false. User admin chưa có hash (chưa
// chạy tpan setup) thì trả 503 và done=true.
func (s *Server) authCheckPassword(w http.ResponseWriter, r *http.Request, name, pw string) (ok, done bool) {
	hash, found, err := s.deps.Store.PasswordHashOf(r.Context(), name)
	if err != nil {
		authInternal(w, "đọc hash mật khẩu", err)
		return false, true
	}
	if !found && name == account.AdminName {
		writeError(w, http.StatusServiceUnavailable, "not_set_up", i18n.T(
			"Chưa có mật khẩu quản trị. Chạy trên máy chủ: sudo tpan setup",
			"There is no admin password yet. Run on the server: sudo tpan setup"))
		return false, true
	}
	if !found {
		if hash, err = authDummyHash(); err != nil {
			authInternal(w, "dựng hash giả", err)
			return false, true
		}
	}
	ok, err = s.auth.verify(hash, pw)
	if errors.Is(err, store.ErrPasswordBusy) {
		// Chưa so được: không tính là một lần sai.
		authWriteBusy(w)
		return false, true
	}
	if err != nil {
		authInternal(w, "kiểm mật khẩu", err)
		return false, true
	}
	return ok && found, false
}

// authMaxName: tên dài hơn mọi tên hợp lệ (32 ký tự) thì không cần hỏi DB,
// nhưng vẫn đi qua hash giả như tên không có thật.
const authMaxName = 64

// handleLogin: POST /api/v1/login.
// Thứ tự: chờ tới lượt của khoá nguồn (các lần thử của một nguồn chạy lần
// lượt), nguồn đang khoá thì từ chối ngay (không tốn argon2), rồi mới so mật
// khẩu (tên lạ cũng tốn argon2 như tên thật), rồi mới xem user có bị khoá:
// chỉ người biết đúng mật khẩu mới biết được tài khoản đang bị khoá.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	st := s.deps.Store
	if st == nil {
		authNoStore(w)
		return
	}
	var req LoginRequest
	if !authDecode(w, r, &req) {
		return
	}
	ip, src := clientIP(r), sourceKey(r)
	if src == authLookupBusy {
		authWriteBusy(w) // chưa biết khoá nguồn: không so, không đếm gì
		return
	}
	leave, ok := s.auth.gates.enter(r.Context(), src)
	if !ok {
		authWriteBusy(w)
		return
	}
	defer leave()
	if s.authLocked(w, r, src) {
		return
	}
	name := req.Name
	if name == "" {
		name = account.AdminName // thói quen và script cũ chỉ gửi mật khẩu (mục 6.12)
	}
	if len(name) > authMaxName {
		name = "\x00" // không có user nào tên này: đi đường hash giả
	}
	if until, ok := s.auth.rate.allow(src, name, s.now()); !ok {
		authWriteRate(w, until.Sub(s.now()))
		return
	}
	ok, done := s.authCheckPassword(w, r, name, req.Password)
	if done {
		return
	}
	s.auth.rate.compared(src, s.now())
	if !ok {
		// Không ghi tên đã gõ vào nhật ký: người ta hay gõ nhầm mật khẩu vào ô tên.
		s.authFailed(w, r, src, name, "login.fail")
		return
	}
	u, found, err := st.User(r.Context(), name)
	if err != nil {
		authInternal(w, "đọc user", err)
		return
	}
	// Từ đây nhật ký ghi tên người đăng nhập (đã đúng mật khẩu nên là tên thật).
	r = r.WithContext(engine.WithActor(r.Context(), name))
	if !found || u.Status != account.Active {
		s.audit(r, "login", name, core.CodeUserSuspended, "")
		writeError(w, http.StatusForbidden, core.CodeUserSuspended, i18n.T(
			"Tài khoản "+name+" đang bị tạm khoá. Liên hệ quản trị viên.",
			"Account "+name+" is suspended. Contact the administrator."))
		return
	}
	now := s.now()
	s.auth.rate.succeeded(name, now)
	// Chỉ trừ phần lần sai của chính tên này; lần sai của tên khác từ cùng
	// nguồn vẫn còn (trước đây xoá cả dòng: khách xen một lần đăng nhập đúng của
	// mình sau mỗi 4 lần đoán mật khẩu admin là không bao giờ bị khoá).
	if n, firstAt, found := s.auth.names.take(src, name, now); found {
		if err := st.ForgetLoginFailures(r.Context(), src, n, firstAt); err != nil {
			authInternal(w, "trừ đếm đăng nhập sai", err)
			return
		}
	}
	// Dọn phiên đã hết hạn lúc có người đăng nhập: rẻ, và bảng không phình.
	if err := st.DeleteSessionsIdleBefore(r.Context(), now.Add(-authIdle)); err != nil {
		slog.Error("dọn phiên hết hạn", "err", err)
	}
	token := base64.RawURLEncoding.EncodeToString(authRandom(32))
	ss := store.Session{
		TokenSHA256: authTokenHash(token),
		ID:          hex.EncodeToString(authRandom(8)),
		IP:          ip,
		Created:     now,
		UserName:    name,
	}
	if err := st.CreateSession(r.Context(), ss); err != nil {
		authInternal(w, "tạo phiên", err)
		return
	}
	authSetCookie(w, r, token, 0)
	s.audit(r, "login", name, "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout: POST /api/v1/logout. Luôn 204 và luôn xoá cookie, kể cả khi
// phiên đã hết hạn từ trước.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ss, ok, err := s.authCurrent(r)
	if err != nil {
		authInternal(w, "đọc phiên", err)
		return
	}
	if ok {
		if err := s.deps.Store.DeleteSessionByToken(r.Context(), ss.TokenSHA256); err != nil {
			authInternal(w, "xoá phiên", err)
			return
		}
		s.audit(r.WithContext(engine.WithActor(r.Context(), ss.UserName)), "logout", ss.ID, "ok", "")
	}
	authSetCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

// handleSession: GET /api/v1/session. Công khai: giao diện hỏi để biết hiện
// màn đăng nhập hay vào thẳng.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	s.writeSessionInfo(w, r)
}

// writeSessionInfo trả SessionInfo của request (GET /session, và sau khi vào
// hay thoát "xem như").
func (s *Server) writeSessionInfo(w http.ResponseWriter, r *http.Request) {
	info := SessionInfo{Demo: s.deps.Demo}
	ss, id, ok, err := s.authResolve(r)
	if err != nil {
		authInternal(w, "đọc phiên", err)
		return
	}
	if ok {
		info.LoggedIn = true
		info.Hostname = s.deps.Hostname
		info.Version = buildinfo.Version
		info.Expires = ss.LastSeen.Add(authIdle)
		info.User = &id
	}
	writeJSON(w, http.StatusOK, info)
}

// handlePasswordChange: POST /api/v1/account/password. Kiểm mật khẩu hiện tại
// (sai thì đếm như đăng nhập sai, để phiên bị chiếm cũng không dò mật khẩu
// được), rồi đổi và huỷ mọi phiên khác.
func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	cur, _ := authSessionOf(r)
	var req PasswordChange
	if !authDecode(w, r, &req) {
		return
	}
	if utf8.RuneCountInString(req.New) < authMinPassword {
		writeError(w, http.StatusBadRequest, "password_too_short", i18n.T(
			"Mật khẩu mới phải có ít nhất "+strconv.Itoa(authMinPassword)+" ký tự.",
			"The new password must be at least "+strconv.Itoa(authMinPassword)+" characters long."))
		return
	}
	src := sourceKey(r)
	if src == authLookupBusy {
		authWriteBusy(w)
		return
	}
	leave, ok := s.auth.gates.enter(r.Context(), src)
	if !ok {
		authWriteBusy(w)
		return
	}
	defer leave()
	if s.authLocked(w, r, src) {
		return
	}
	if until, ok := s.auth.rate.allow(src, "", s.now()); !ok {
		authWriteRate(w, until.Sub(s.now()))
		return
	}
	// Mật khẩu của chính người đăng nhập (phiên "xem như" đã bị guard chặn POST).
	ok, done := s.authCheckPassword(w, r, cur.UserName, req.Current)
	if done {
		return
	}
	s.auth.rate.compared(src, s.now())
	if !ok {
		s.authFailed(w, r, src, cur.UserName, "account.password")
		return
	}
	hash, err := s.auth.hash(req.New)
	if errors.Is(err, store.ErrPasswordBusy) {
		authWriteBusy(w)
		return
	}
	if err != nil {
		authInternal(w, "băm mật khẩu mới", err)
		return
	}
	if err := s.deps.Store.ChangeUserPassword(r.Context(), cur.UserName, hash, cur.TokenSHA256, s.now()); err != nil {
		authInternal(w, "đổi mật khẩu", err)
		return
	}
	s.audit(r, "account.password", "", "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleSessions: GET /api/v1/account/sessions. Chỉ phiên của chính người
// đăng nhập (M13). Phiên đã hết hạn mà chưa bị dọn thì không hiện.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	cur, _ := authSessionOf(r)
	list, err := s.deps.Store.SessionsOf(r.Context(), cur.UserName)
	if err != nil {
		authInternal(w, "đọc danh sách phiên", err)
		return
	}
	now := s.now()
	out := []SessionRow{}
	for _, ss := range list {
		if now.Sub(ss.LastSeen) > authIdle {
			continue
		}
		out = append(out, SessionRow{
			ID: ss.ID, Current: ss.TokenSHA256 == cur.TokenSHA256, IP: ss.IP,
			Created: ss.Created, LastSeen: ss.LastSeen,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionDelete: DELETE /api/v1/account/sessions/{id}.
func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	cur, _ := authSessionOf(r)
	id := r.PathValue("id")
	// Chỉ phiên của mình: phiên của người khác ra cùng 404 như id không có.
	err := s.deps.Store.DeleteSessionOf(r.Context(), cur.UserName, id)
	if errors.Is(err, store.ErrNotFound) {
		s.audit(r, "session.delete", id, "not_found", "")
		writeError(w, http.StatusNotFound, "session_not_found", i18n.T(
			"Không có phiên này.", "No such session."))
		return
	}
	if err != nil {
		authInternal(w, "xoá phiên", err)
		return
	}
	s.audit(r, "session.delete", id, "ok", "")
	if id == cur.ID {
		authSetCookie(w, r, "", -1)
	}
	w.WriteHeader(http.StatusNoContent)
}
