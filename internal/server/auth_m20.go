package server

// Đăng nhập hai bước (M20, docs/thay-doi/M20-thiet-ke.md mục 2.4, 3.4, 11.1,
// 11.2, 11.6, 11.8, 11.12): tên máy của request, phiếu chờ bước hai, kiểm mã
// (checkSecondFactor), file khoá niêm phong bí mật. auth.go chỉ gọi vào đây ở
// vài chỗ, để phần đăng nhập của M28 (authFailed, authLocked) sửa song song
// không đụng nhau.
//
// Phiếu chờ KHÔNG phải phiên: nó nằm ở bảng login_challenges và cookie riêng,
// authResolve (chỗ duy nhất dựng danh tính cho guard) không bao giờ đọc nó.
// Chỉ ba chỗ đọc phiếu: POST /login/2fa, POST /logout, GET /session.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/mfa"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// Cookie của phiếu chờ: cùng luật đặt tên với cookie phiên (auth.go).
	mfaCookieTLS   = "__Host-tpan-2fa"
	mfaCookiePlain = "tpan-2fa"
	// mfaChallengeTTL: phiếu sống 5 phút; quá thì nhập lại mật khẩu.
	mfaChallengeTTL = 5 * time.Minute
	// mfaChallengeTries: mỗi phiếu tối đa 5 lần nhập mã (quy tắc 4 ở 3.4): quá
	// thì xoá phiếu, kẻ dò mã phải trả lại một lần argon2.
	mfaChallengeTries = 5
	// Khoá theo user ở bước hai (mục 11.1): 10 lần sai liên tiếp thì khoá
	// 15 phút × 2^(n−1), trần 24 giờ.
	mfaUserMax      = 10
	mfaUserLockBase = 15 * time.Minute
	mfaUserLockCap  = 24 * time.Hour
)

// requestHost là tên máy của request (mục 11.12 R7): phần tên của r.Host, bỏ
// cổng, bỏ một dấu chấm cuối, chữ thường; địa chỉ IPv6 bỏ ngoặc vuông. Rỗng
// khi request không có Host (HTTP/1.0).
func requestHost(r *http.Request) string {
	h := r.Host
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	} else if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}

// hasAuthorization: request mang header Authorization (kể cả rỗng hay sai
// dạng). Có header thì request CHỈ được xác thực bằng token (mục 3.7): mọi chỗ
// đọc cookie (phiên, phiếu) coi như không có cookie. Nhờ vậy miễn 421 cho
// request có Authorization (R12) không mở đường phát lại cookie.
func hasAuthorization(r *http.Request) bool {
	return len(r.Header.Values("Authorization")) > 0
}

// refuseAuthorizationOnLogin: đăng nhập (cả bước hai) không bao giờ đi cùng
// header Authorization. Request có header đó được miễn 421 (không dùng cookie
// nên không có gì để lộ); nếu /login nhận nó thì phiên được tạo dưới tên của
// một site, đúng chỗ 421 muốn chặn (soát cuối trên máy ảo, V1). true là đã trả lời.
func refuseAuthorizationOnLogin(w http.ResponseWriter, r *http.Request) bool {
	if !hasAuthorization(r) {
		return false
	}
	writeError(w, http.StatusBadRequest, "authorization_on_login", i18n.T(
		"Đăng nhập không nhận header Authorization. Token dùng thẳng cho API, không cần đăng nhập.",
		"Login does not accept an Authorization header. Tokens are used directly on the API, without logging in."))
	return true
}

// loginHost: POST /login cần tên máy để gắn phiên (R10); không có thì 400
// trước khi tốn argon2. false nghĩa là đã trả lời.
func loginHost(w http.ResponseWriter, r *http.Request) (string, bool) {
	h := requestHost(r)
	if h == "" {
		writeError(w, http.StatusBadRequest, "host_missing", i18n.T(
			"Yêu cầu không có tên máy (header Host); trình duyệt luôn gửi nó. Dùng HTTP/1.1 trở lên.",
			"The request has no host name (Host header); browsers always send it. Use HTTP/1.1 or later."))
		return "", false
	}
	return h, true
}

// ---------------------------------------------------------------- phiên và tên máy

// sessionHost áp luật tên máy cho phiên ss (mục 11.2 ý 1, R1). Phiên của bản
// cũ (host rỗng) được gắn tên máy hiện tại ở lần dùng đầu. Phiên đã gắn tên mà
// request đến từ tên khác: cookie đã bị chép đi (trình duyệt chỉ gửi cookie
// host-only về đúng tên đã đặt nó), nên xoá phiên; false nghĩa là đã xoá.
func (s *Server) sessionHost(r *http.Request, ss *store.Session) (bool, error) {
	st, h := s.deps.Store, requestHost(r)
	if ss.Host == "" {
		if h != "" {
			if err := st.SetSessionHost(r.Context(), ss.TokenSHA256, h); err != nil {
				return false, err
			}
			ss.Host = h
		}
		return true, nil
	}
	if ss.Host == h {
		return true, nil
	}
	if err := st.DeleteSessionByToken(r.Context(), ss.TokenSHA256); err != nil {
		return false, err
	}
	// Không ghi tên máy lạ vào nhật ký: nó do người gửi tự đặt.
	s.audit(r.WithContext(engine.WithActor(r.Context(), ss.UserName)), "session.host_mismatch", ss.ID, "deleted", "")
	return false, nil
}

// startSession tạo phiên MỚI (token mới) cho name gắn tên máy host, đặt
// cookie. Dùng ở bước hai: không nâng phiếu thành phiên (chống cố định phiên).
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, name, ip, host string) error {
	st, now := s.deps.Store, s.now()
	if err := st.DeleteSessionsIdleBefore(r.Context(), now.Add(-authIdle)); err != nil {
		slog.Error("dọn phiên hết hạn", "err", err)
	}
	token := base64.RawURLEncoding.EncodeToString(authRandom(32))
	if err := st.CreateSession(r.Context(), store.Session{
		TokenSHA256: authTokenHash(token), ID: hexID(8), IP: ip, Created: now, UserName: name, Host: host,
	}); err != nil {
		return err
	}
	authSetCookie(w, r, token, 0)
	return nil
}

// hexID là n byte ngẫu nhiên dạng hex (id ngắn của phiên, token).
func hexID(n int) string { return hex.EncodeToString(authRandom(n)) }

// sessionOnly lấy phiên của request cho handler chỉ dùng được bằng phiên
// đăng nhập (R3, mục 11.8): request bằng token không có phiên trong context.
// Bảng nhóm token đã chặn trước (token_forbidden); đây là lớp phòng thủ thứ hai.
func sessionOnly(w http.ResponseWriter, r *http.Request) (store.Session, bool) {
	cur, ok := authSessionOf(r)
	if !ok || cur.TokenSHA256 == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", i18n.T(
			"Việc này chỉ làm được khi đăng nhập bằng mật khẩu trên giao diện.",
			"This can only be done when signed in with a password in the interface."))
		return store.Session{}, false
	}
	return cur, true
}

// realRole là vai của người thật đứng sau danh tính: đang "xem như" thì là
// admin (authResolve chỉ giữ phiên xem như của admin), không thì vai của id.
func realRole(id account.Identity) account.Role {
	if id.ViewedBy != "" {
		return account.RoleAdmin
	}
	return id.Role
}

// sessionInfoM20 điền hai trường M20 của SessionInfo: mfa_enroll_required khi
// có phiên, mfa_pending khi chưa có phiên mà có phiếu hợp lệ.
func (s *Server) sessionInfoM20(r *http.Request, info *SessionInfo, ss store.Session, id account.Identity, loggedIn bool) error {
	if s.deps.Store == nil {
		return nil
	}
	var err error
	if loggedIn {
		info.MFAEnrollRequired, err = s.enrollRequired(r.Context(), ss.UserName, realRole(id))
		return err
	}
	_, info.MFAPending, err = s.mfaChallenge(r)
	return err
}

// ---------------------------------------------------------------- phiếu chờ

func mfaCookieName(r *http.Request) string {
	if r.TLS != nil {
		return mfaCookieTLS
	}
	return mfaCookiePlain
}

// mfaSetCookie đặt (maxAge > 0) hoặc xoá (maxAge < 0) cookie phiếu.
func mfaSetCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: mfaCookieName(r), Value: value, Path: "/", MaxAge: maxAge,
		Secure: r.TLS != nil, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

// mfaChallenge đọc phiếu của request: có cookie, có trong DB, còn hạn, đúng
// IP và đúng tên máy (quy tắc 5 ở 3.4, mục 11.2). Sai bất kỳ điều nào thì như
// không có phiếu.
func (s *Server) mfaChallenge(r *http.Request) (store.Challenge, bool, error) {
	if s.deps.Store == nil || hasAuthorization(r) {
		return store.Challenge{}, false, nil
	}
	c, err := r.Cookie(mfaCookieName(r))
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return store.Challenge{}, false, nil
	}
	ch, err := s.deps.Store.ChallengeByToken(r.Context(), authTokenHash(c.Value))
	if errors.Is(err, store.ErrNotFound) {
		return store.Challenge{}, false, nil
	}
	if err != nil {
		return store.Challenge{}, false, err
	}
	if !s.now().Before(ch.Expires) || ch.IP != clientIP(r) || ch.Host != requestHost(r) {
		return store.Challenge{}, false, nil
	}
	return ch, true, nil
}

// loginChallenge là nhánh M20 của handleLogin (điểm ghép 3.4.1): user đã bật
// hai bước thì tạo phiếu, đặt cookie phiếu, trả 200 {mfa_required:true}. KHÔNG
// xoá bộ đếm đăng nhập sai của IP (quy tắc 2): chỉ bước hai đúng mới xoá.
// true nghĩa là đã trả lời.
func (s *Server) loginChallenge(w http.ResponseWriter, r *http.Request, name, ip, host string) bool {
	st := s.deps.Store
	on, err := st.TOTPEnabled(r.Context(), name)
	if err != nil {
		authInternal(w, "đọc hai bước", err)
		return true
	}
	if !on {
		return false
	}
	now := s.now()
	if err := st.DeleteExpiredChallenges(r.Context(), now); err != nil {
		slog.Error("dọn phiếu bước hai hết hạn", "err", err)
	}
	token := base64.RawURLEncoding.EncodeToString(authRandom(32))
	if err := st.CreateChallenge(r.Context(), store.Challenge{
		TokenSHA256: authTokenHash(token), User: name, IP: ip, Host: host,
		Created: now, Expires: now.Add(mfaChallengeTTL),
	}); err != nil {
		authInternal(w, "tạo phiếu bước hai", err)
		return true
	}
	mfaSetCookie(w, r, token, int(mfaChallengeTTL/time.Second))
	s.audit(r, "login", name, "mfa_required", "")
	writeJSON(w, http.StatusOK, LoginResult{MFARequired: true})
	return true
}

// dropChallenge xoá phiếu của request (đăng xuất, "Quay lại" ở màn bước hai).
func (s *Server) dropChallenge(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(mfaCookieName(r))
	if err != nil || c.Value == "" {
		return nil
	}
	mfaSetCookie(w, r, "", -1)
	if len(c.Value) > 128 || s.deps.Store == nil {
		return nil
	}
	_, err = s.deps.Store.ConsumeChallenge(r.Context(), authTokenHash(c.Value))
	return err
}

func challengeInvalid(w http.ResponseWriter, r *http.Request) {
	mfaSetCookie(w, r, "", -1)
	writeError(w, http.StatusUnauthorized, "mfa_challenge_invalid", i18n.T(
		"Phiên chờ nhập mã đã hết hạn hoặc không còn đúng. Đăng nhập lại bằng mật khẩu.",
		"The code prompt has expired or is no longer valid. Sign in again with your password."))
}

// handleLogin2FA: POST /api/v1/login/2fa (public). Thứ tự: IP đang khoá thì
// từ chối ngay; phiếu phải hợp lệ; user còn active; rồi mới kiểm mã.
func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	st := s.deps.Store
	if st == nil {
		authNoStore(w)
		return
	}
	if refuseAuthorizationOnLogin(w, r) {
		return
	}
	var req LoginCode
	if !authDecode(w, r, &req) {
		return
	}
	ip, src := clientIP(r), sourceKey(r)
	if src == authLookupBusy {
		authWriteBusy(w)
		return
	}
	if s.authLocked(w, r, src) {
		return
	}
	ch, ok, err := s.mfaChallenge(r)
	if err != nil {
		authInternal(w, "đọc phiếu bước hai", err)
		return
	}
	if !ok {
		challengeInvalid(w, r)
		return
	}
	u, found, err := st.User(r.Context(), ch.User)
	if err != nil {
		authInternal(w, "đọc user", err)
		return
	}
	if !found || u.Status != account.Active {
		_, _ = st.ConsumeChallenge(r.Context(), ch.TokenSHA256)
		challengeInvalid(w, r)
		return
	}
	r = r.WithContext(engine.WithActor(r.Context(), ch.User))
	method, ok := s.checkSecondFactor(w, r, ch.User, req.Code, sfPurpose{
		action: "login.2fa", challenge: ch.TokenSHA256, status: http.StatusUnauthorized,
	})
	if !ok {
		return
	}
	// Hai request cùng phiếu, cùng mã đúng (TOTP thì UseTOTPStep đã chặn; hai
	// mã dự phòng khác nhau thì không): chỉ bên xoá được phiếu có phiên.
	won, err := st.ConsumeChallenge(r.Context(), ch.TokenSHA256)
	if err != nil {
		authInternal(w, "dùng phiếu bước hai", err)
		return
	}
	if !won {
		challengeInvalid(w, r)
		return
	}
	// Bước hai đúng mới trừ bộ đếm sai (điểm ghép 3.4.1 với M28): đúng cách
	// handleLogin làm với user không có hai bước.
	now := s.now()
	s.auth.rate.succeeded(ch.User, now)
	if n, firstAt, found := s.auth.names.take(src, ch.User, now); found {
		if err := st.ForgetLoginFailures(r.Context(), src, n, firstAt); err != nil {
			authInternal(w, "trừ đếm đăng nhập sai", err)
			return
		}
	}
	wrong, err := st.TakeWrongSinceOK(r.Context(), ch.User)
	if err != nil {
		authInternal(w, "đọc số mã sai", err)
		return
	}
	if err := s.startSession(w, r, ch.User, ip, ch.Host); err != nil {
		authInternal(w, "tạo phiên", err)
		return
	}
	rememberPanelHost(r, st, u.Role)
	mfaSetCookie(w, r, "", -1)
	result := "ok"
	if method == "recovery" {
		result = "ok_recovery" // mã dự phòng là dấu hiệu đáng xem lại trong nhật ký
	}
	s.audit(r, "login", ch.User, result, "")
	if method != "recovery" && wrong == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	res := LoginResult{WrongCodes: wrong}
	if method == "recovery" {
		left, err := st.RecoveryLeft(r.Context(), ch.User)
		if err != nil {
			authInternal(w, "đếm mã dự phòng", err)
			return
		}
		res.RecoveryLeft = &left
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------------------------------------------------------------- kiểm mã

// sfPurpose là chỗ gọi checkSecondFactor.
type sfPurpose struct {
	// action là thao tác ghi nhật ký cho lần sai ("login.2fa",
	// "account.2fa.disable"...).
	action string
	// challenge: sha256 của phiếu (chỉ ở bước hai đăng nhập); sai thì đếm vào phiếu.
	challenge string
	// status là mã HTTP của mã sai và mã đã dùng: 401 ở bước hai, 400 ở các
	// route /account (giao diện đưa mọi 401 khác wrong_password về màn đăng nhập).
	status int
}

func sfAccount(action string) sfPurpose {
	return sfPurpose{action: action, status: http.StatusBadRequest}
}

// mfaLocks là khoá trong tiến trình theo user (mục 11.1, R6): mọi lần kiểm mã
// của một user nối đuôi nhau, nên bộ đếm sai đếm đúng từng lần đoán dù có
// hàng nghìn request song song. Mỗi user một kênh một chỗ: chờ được theo ctx.
// Chỉ tpan serve kiểm mã (CLI không bao giờ), nên khoá trong tiến trình là đủ.
type mfaLocks struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}

func (l *mfaLocks) lock(ctx context.Context, user string) (func(), bool) {
	l.mu.Lock()
	if l.m == nil {
		l.m = map[string]chan struct{}{}
	}
	ch := l.m[user]
	if ch == nil {
		// ponytail: map chỉ lớn lên theo số user đã qua được mật khẩu; không dọn.
		ch = make(chan struct{}, 1)
		l.m[user] = ch
	}
	l.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, true
	case <-ctx.Done():
		return nil, false
	}
}

// match là mfa.Match, hoặc hàm thay thế của test (đếm số lần Match chạy).
func (s *Server) match(secret []byte, code string, now time.Time) (uint64, bool) {
	if s.mfaMatch != nil {
		return s.mfaMatch(secret, code, now)
	}
	return mfa.Match(secret, code, now)
}

// checkSecondFactor kiểm một mã của user (TOTP 6 số hoặc mã dự phòng) và
// trả cách đã dùng ("totp", "recovery") cùng ok=true; ok=false nghĩa là đã
// trả lời. Luôn gọi SAU khi mật khẩu đã đúng (R6), để argon2 chạy ngoài khoá.
//
// Trong khoá theo user: phiếu còn lượt và IP chưa khoá (đọc lại, F4) → đọc
// trạng thái khoá (đọc SAU khi giữ khoá, không thì
// các request đang chờ đều thấy "chưa khoá") → đang khoá mà mã dạng 6 số thì
// 423 mfa_locked, không chạy Match; mã dự phòng (80 bit, không dò được) vẫn
// được kiểm → Match + UseTOTPStep hoặc UseRecoveryCode → sai thì ghi lần sai
// (phiếu, user, IP) rồi mới nhả khoá.
func (s *Server) checkSecondFactor(w http.ResponseWriter, r *http.Request, user, code string, p sfPurpose) (string, bool) {
	ctx, st := r.Context(), s.deps.Store
	release, ok := s.mfaUsers.lock(ctx, user)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "mfa_busy", i18n.T(
			"Máy chủ đang bận kiểm mã khác của tài khoản này; thử lại.",
			"The server is busy checking another code for this account; try again."))
		return "", false
	}
	defer release()
	now := s.now()
	// Trần 5 lần mỗi phiếu và khoá IP được ghi bởi request đi trước trong
	// khoá: đọc lại ở đây, SAU khi giữ khoá (soát cuối F4). Kiểm ở đầu handler
	// thì N request song song cùng thấy "còn lượt" rồi lần lượt vào Match.
	if p.challenge != "" {
		ch, err := st.ChallengeByToken(ctx, p.challenge)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			authInternal(w, "đọc phiếu bước hai", err)
			return "", false
		}
		if err != nil || !now.Before(ch.Expires) || ch.Attempts >= mfaChallengeTries {
			_, _ = st.ConsumeChallenge(ctx, p.challenge)
			challengeInvalid(w, r)
			return "", false
		}
	}
	if src := sourceKey(r); src == authLookupBusy {
		authWriteBusy(w)
		return "", false
	} else if s.authLocked(w, r, src) {
		return "", false
	}
	row, found, err := st.TOTP(ctx, user)
	if err != nil {
		authInternal(w, "đọc hai bước", err)
		return "", false
	}
	if !found || row.Enabled.IsZero() {
		s.mfaGone(w, r, p)
		return "", false
	}
	code = strings.TrimSpace(code)
	if mfa.IsCode(code) {
		if row.LockedUntil.After(now) {
			mfaWriteLocked(w, row.LockedUntil.Sub(now))
			return "", false
		}
		key, err := s.mfaKey(ctx, false)
		if err != nil {
			mfaSecretUnavailable(w, user, err)
			return "", false
		}
		secret, err := mfa.Open(key, user, row.SecretEnc)
		if err != nil {
			mfaSecretUnavailable(w, user, err)
			return "", false
		}
		if step, ok := s.match(secret, code, now); ok {
			used, err := st.UseTOTPStep(ctx, user, step)
			if err != nil {
				authInternal(w, "nhận mã hai bước", err)
				return "", false
			}
			if !used {
				// Mã đúng nhưng đã dùng (nghe lén rồi phát lại, hoặc gõ lại cùng
				// mã trong 30 giây): không tính là đoán sai.
				s.audit(r, p.action, user, "code_used", "")
				writeError(w, p.status, "mfa_code_used", i18n.T(
					"Mã này vừa được dùng rồi. Chờ app hiện mã mới (30 giây) rồi nhập lại.",
					"This code was just used. Wait for the app to show a new code (30 seconds) and enter it."))
				return "", false
			}
			return "totp", true
		}
	} else if n := mfa.NormalizeRecovery(code); n != "" {
		used, err := st.UseRecoveryCode(ctx, user, mfa.HashRecovery(n), now)
		if err != nil {
			authInternal(w, "dùng mã dự phòng", err)
			return "", false
		}
		if used {
			return "recovery", true
		}
	}
	// Mã sai, mã dự phòng đã dùng, hay chuỗi không ra dạng mã nào: cùng một
	// lần đoán sai (chuỗi lạ thì không chạy Match).
	s.mfaWrong(w, r, user, p, now)
	return "", false
}

// mfaGone: user không còn bật hai bước giữa chừng (tắt, root đặt lại).
func (s *Server) mfaGone(w http.ResponseWriter, r *http.Request, p sfPurpose) {
	if p.challenge != "" {
		_, _ = s.deps.Store.ConsumeChallenge(r.Context(), p.challenge)
		challengeInvalid(w, r)
		return
	}
	writeError(w, http.StatusConflict, "mfa_not_enabled", i18n.T(
		"Tài khoản này chưa bật đăng nhập hai bước.", "Two-step login is not on for this account."))
}

// mfaWrong ghi một lần sai vào ba bộ đếm (phiếu, user, IP) rồi trả lời.
//
// Đếm theo IP đi qua authFailed (để mọi bản vá của M28 cho nó tự áp ở đây) với
// một ResponseWriter thu tạm: authFailed viết 401 wrong_password, bước hai cần
// mfa_code_wrong. Nó viết mã khác 401 (423 khoá IP, 500) thì chép nguyên ra.
// authFailed tự ghi một dòng nhật ký với thao tác p.action+".ip" (kết quả
// wrong_password hay locked của IP); dòng của lần sai mã là p.action /
// wrong_code, và lần khoá user là login.2fa_locked / locked (phép kiểm
// tpan.2fa:attempts đếm đúng dòng đó, nên khoá IP không được mang tên này).
func (s *Server) mfaWrong(w http.ResponseWriter, r *http.Request, user string, p sfPurpose, now time.Time) {
	ctx, st := r.Context(), s.deps.Store
	if p.challenge != "" {
		n, err := st.BumpChallenge(ctx, p.challenge)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			authInternal(w, "đếm lần sai của phiếu", err)
			return
		}
		if err == nil && n >= mfaChallengeTries {
			if _, err := st.ConsumeChallenge(ctx, p.challenge); err != nil {
				authInternal(w, "xoá phiếu", err)
				return
			}
			mfaSetCookie(w, r, "", -1)
		}
	}
	locked, until, _, err := st.RecordTOTPFailure(ctx, user, now, mfaUserMax, mfaUserLockBase, mfaUserLockCap)
	if errors.Is(err, store.ErrNotFound) {
		s.mfaGone(w, r, p)
		return
	}
	if err != nil {
		authInternal(w, "ghi mã hai bước sai", err)
		return
	}
	if locked {
		s.audit(r, "login.2fa_locked", user, "locked", "")
	}
	s.audit(r, p.action, user, "wrong_code", "")
	rec := &failRecorder{h: http.Header{}}
	s.authFailed(rec, r, sourceKey(r), user, p.action+".ip")
	switch {
	case rec.code != http.StatusUnauthorized:
		rec.copyTo(w)
	case locked:
		mfaWriteLocked(w, until.Sub(now))
	default:
		writeError(w, p.status, "mfa_code_wrong", i18n.T(
			"Sai mã. Nhập mã 6 số đang hiện trong app xác thực, hoặc một mã dự phòng.",
			"Wrong code. Enter the 6-digit code shown in your authenticator app, or a recovery code."))
	}
}

func mfaWriteLocked(w http.ResponseWriter, left time.Duration) {
	mins := int(left.Minutes()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
	writeError(w, http.StatusLocked, "mfa_locked", i18n.T(
		"Nhập sai mã hai bước quá nhiều lần; mã 6 số bị khoá thêm "+strconv.Itoa(mins)+" phút. Mã dự phòng vẫn dùng được. Không phải bạn nhập sai thì mật khẩu đã lộ: vào được rồi thì đổi mật khẩu ngay.",
		"Too many wrong two-step codes; 6-digit codes are locked for "+strconv.Itoa(mins)+" more minutes. Recovery codes still work. If it was not you, your password has leaked: change it as soon as you are in."))
}

func mfaSecretUnavailable(w http.ResponseWriter, user string, err error) {
	slog.Error("không mở được bí mật hai bước", "user", user, "err", err)
	writeError(w, http.StatusServiceUnavailable, "mfa_secret_unavailable", i18n.T(
		"Máy chủ không mở được bí mật hai bước (file khoá /etc/tpan/secret.key mất hoặc hỏng). Dùng một mã dự phòng, hoặc nhờ người có quyền root chạy: sudo tpan user 2fa reset "+user,
		"The server cannot open the two-step secret (the key file /etc/tpan/secret.key is missing or damaged). Use a recovery code, or ask someone with root to run: sudo tpan user 2fa reset "+user))
}

// failRecorder thu câu trả lời của authFailed để bước hai đổi 401 thành
// mfa_code_wrong. Header riêng: chỉ chép ra khi câu trả lời được giữ nguyên.
type failRecorder struct {
	h    http.Header
	code int
	body bytes.Buffer
}

func (f *failRecorder) Header() http.Header { return f.h }

func (f *failRecorder) WriteHeader(code int) {
	if f.code == 0 {
		f.code = code
	}
}

func (f *failRecorder) Write(b []byte) (int, error) {
	if f.code == 0 {
		f.code = http.StatusOK
	}
	return f.body.Write(b)
}

func (f *failRecorder) copyTo(w http.ResponseWriter) {
	for k, v := range f.h {
		w.Header()[k] = v
	}
	w.WriteHeader(f.code)
	_, _ = w.Write(f.body.Bytes())
}

// ---------------------------------------------------------------- file khoá

// mfaKeyPath là file khoá AES niêm phong bí mật TOTP (Deps.MFAKeyFile; rỗng
// là mfa.KeyFile).
func (s *Server) mfaKeyPath() string {
	if s.deps.MFAKeyFile != "" {
		return s.deps.MFAKeyFile
	}
	return mfa.KeyFile
}

// mfaKey đọc file khoá (mục 11.6, R4). Đi thẳng qua System (Stat, ReadFile,
// WriteFileAtomic), KHÔNG qua engine hay steps.File: khoá không bao giờ vào
// managed_files, bản xem trước hay sự kiện job.
//
// create (chỉ ở setup): file chưa có VÀ bảng user_totp chưa có dòng nào thì
// sinh khoá mới (0600 root), dưới mutex để hai lần setup đầu tiên không sinh
// hai khoá. Đã có dòng mà file mất thì không sinh lại: bí mật cũ vẫn không mở
// được, khoá mới chỉ che lỗi. File phải là file thường (không symlink), chủ
// root, quyền 0600, đúng 32 byte.
func (s *Server) mfaKey(ctx context.Context, create bool) (mfa.Key, error) {
	s.mfaKeyMu.Lock()
	defer s.mfaKeyMu.Unlock()
	sys, p := s.deps.Sys, s.mfaKeyPath()
	if sys == nil {
		return mfa.Key{}, errors.New("server không có System")
	}
	info, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		if !create {
			return mfa.Key{}, errors.New("không có file khoá " + p)
		}
		used, err := s.deps.Store.AnyTOTP(ctx)
		if err != nil {
			return mfa.Key{}, err
		}
		if used {
			return mfa.Key{}, errors.New("đã có bí mật hai bước mà mất file khoá " + p + "; không sinh khoá mới")
		}
		k := mfa.NewKey()
		if err := sys.MkdirAll(path.Dir(p), 0o700, system.Root); err != nil {
			return mfa.Key{}, err
		}
		if err := sys.WriteFileAtomic(p, k[:], 0o600, system.Root); err != nil {
			return mfa.Key{}, err
		}
		slog.Info("đã tạo file khoá đăng nhập hai bước", "path", p)
		return k, nil
	}
	if err != nil {
		return mfa.Key{}, err
	}
	rootOr := func(v string) bool { return v == "" || v == "root" }
	if !info.Mode.IsRegular() || info.Mode.Perm() != 0o600 || !rootOr(info.Owner.User) || !rootOr(info.Owner.Group) || info.Size != int64(len(mfa.Key{})) {
		return mfa.Key{}, errors.New("file khoá " + p + " phải là file thường 32 byte, 0600, root:root")
	}
	b, err := sys.ReadFile(p)
	if err != nil {
		return mfa.Key{}, err
	}
	return mfa.ParseKey(b)
}
