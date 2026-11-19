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
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/buildinfo"
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
}

func newAuthState(*Server) *authState {
	return &authState{cop: http.NewCrossOriginProtection()}
}

// authCtxKey là khoá context giữ phiên của request đã qua requireSession.
type authCtxKey struct{}

// authSessionOf trả phiên mà requireSession đã gắn vào request.
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
				"Yêu cầu đến từ một trang khác nên bị chặn (chống CSRF). Hãy thao tác từ chính giao diện tpan.",
				"The request came from another site and was blocked (CSRF protection). Use the tpan interface itself."))
			return
		}
		h.ServeHTTP(w, r)
	})
}

// requireSession chỉ cho request có phiên hợp lệ đi qua; không có thì 401.
func (s *Server) requireSession(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ss, ok, err := s.authCurrent(r)
		if err != nil {
			authInternal(w, "đọc phiên", err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", i18n.T(
				"Chưa đăng nhập hoặc phiên đã hết hạn.", "Not logged in, or the session has expired."))
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), authCtxKey{}, ss)))
	})
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

// authLocked trả 423 nếu IP đang bị khoá; true nghĩa là đã trả lời.
func (s *Server) authLocked(w http.ResponseWriter, r *http.Request, ip string) bool {
	until, locked, err := s.deps.Store.LoginLockedUntil(r.Context(), ip, s.now())
	if err != nil {
		authInternal(w, "đọc khoá đăng nhập", err)
		return true
	}
	if locked {
		authWriteLocked(w, until.Sub(s.now()))
	}
	return locked
}

func authWriteLocked(w http.ResponseWriter, left time.Duration) {
	mins := int(left.Minutes()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
	writeError(w, http.StatusLocked, "login_locked", i18n.T(
		"Sai mật khẩu quá nhiều lần; địa chỉ IP này bị khoá thêm "+strconv.Itoa(mins)+" phút. Quên mật khẩu thì chạy trên máy chủ: sudo tpan admin passwd",
		"Too many wrong passwords; this IP address is locked for "+strconv.Itoa(mins)+" more minutes. If you forgot the password, run on the server: sudo tpan admin passwd"))
}

// authFailed đếm một lần sai mật khẩu của ip, ghi nhật ký, rồi trả 401 hoặc
// 423 (lần sai thứ 5 khoá ngay). action là thao tác ghi vào nhật ký.
func (s *Server) authFailed(w http.ResponseWriter, r *http.Request, ip, action string) {
	locked, until, err := s.deps.Store.RecordLoginFailure(r.Context(), ip, s.now(),
		authMaxFailures, authFailWindow, authLockFor)
	if err != nil {
		authInternal(w, "đếm đăng nhập sai", err)
		return
	}
	if locked {
		s.audit(r, action, "", "locked", "")
		authWriteLocked(w, until.Sub(s.now()))
		return
	}
	s.audit(r, action, "", "wrong_password", "")
	writeError(w, http.StatusUnauthorized, "wrong_password", i18n.T(
		"Sai mật khẩu.", "Wrong password."))
}

// authCheckPassword so pw với hash quản trị trong DB. Chưa có hash (chưa chạy
// tpan setup) thì trả 503 và done=true.
func (s *Server) authCheckPassword(w http.ResponseWriter, r *http.Request, pw string) (ok, done bool) {
	hash, set, err := s.deps.Store.AdminPasswordHash(r.Context())
	if err != nil {
		authInternal(w, "đọc hash mật khẩu", err)
		return false, true
	}
	if !set {
		writeError(w, http.StatusServiceUnavailable, "not_set_up", i18n.T(
			"Chưa có mật khẩu quản trị. Chạy trên máy chủ: sudo tpan setup",
			"There is no admin password yet. Run on the server: sudo tpan setup"))
		return false, true
	}
	ok, err = store.VerifyPassword(hash, pw)
	if err != nil {
		authInternal(w, "kiểm mật khẩu", err)
		return false, true
	}
	return ok, false
}

// handleLogin: POST /api/v1/login.
// Thứ tự: IP đang khoá thì từ chối ngay (không tốn argon2), rồi mới so mật khẩu.
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
	ip := clientIP(r)
	if s.authLocked(w, r, ip) {
		return
	}
	ok, done := s.authCheckPassword(w, r, req.Password)
	if done {
		return
	}
	if !ok {
		s.authFailed(w, r, ip, "login.fail")
		return
	}
	if err := st.ClearLoginFailures(r.Context(), ip); err != nil {
		authInternal(w, "xoá đếm đăng nhập sai", err)
		return
	}

	now := s.now()
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
	}
	if err := st.CreateSession(r.Context(), ss); err != nil {
		authInternal(w, "tạo phiên", err)
		return
	}
	authSetCookie(w, r, token, 0)
	s.audit(r, "login", "", "ok", "")
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
		s.audit(r, "logout", ss.ID, "ok", "")
	}
	authSetCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

// handleSession: GET /api/v1/session. Công khai: giao diện hỏi để biết hiện
// màn đăng nhập hay vào thẳng.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	info := SessionInfo{Demo: s.deps.Demo}
	ss, ok, err := s.authCurrent(r)
	if err != nil {
		authInternal(w, "đọc phiên", err)
		return
	}
	if ok {
		info.LoggedIn = true
		info.Hostname = s.deps.Hostname
		info.Version = buildinfo.Version
		info.Expires = ss.LastSeen.Add(authIdle)
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
	ip := clientIP(r)
	if s.authLocked(w, r, ip) {
		return
	}
	ok, done := s.authCheckPassword(w, r, req.Current)
	if done {
		return
	}
	if !ok {
		s.authFailed(w, r, ip, "account.password")
		return
	}
	hash, err := store.HashPassword(req.New)
	if err != nil {
		authInternal(w, "băm mật khẩu mới", err)
		return
	}
	if err := s.deps.Store.ChangeAdminPassword(r.Context(), hash, cur.TokenSHA256, s.now()); err != nil {
		authInternal(w, "đổi mật khẩu", err)
		return
	}
	s.audit(r, "account.password", "", "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleSessions: GET /api/v1/account/sessions. Phiên đã hết hạn mà chưa bị
// dọn thì không hiện.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	cur, _ := authSessionOf(r)
	list, err := s.deps.Store.Sessions(r.Context())
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
	err := s.deps.Store.DeleteSessionByID(r.Context(), id)
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
