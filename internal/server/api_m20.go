package server

// Route của M20 dùng phiên (hợp đồng ở types_m20.go; thiết kế mục 3.5, 3.6,
// 3.8, 11.5, 11.9): hai bước của chính mình, chính sách bắt buộc, cài đặt
// đăng nhập, tên miền của panel. Token API ở token_m20.go.
//
// Mọi route /account/2fa* gắn với user THẬT của phiên (store.Session.UserName),
// không phải who(r).Name: lúc admin "xem như" user khác hai tên này khác nhau.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/mfa"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

// Khoá settings của M20 (mục 3.2).
const (
	settingRequire2FA   = "auth.require_2fa"
	settingTokenMaxDays = "auth.token_max_days"
)

// ---------------------------------------------------------------- chính sách

// policy đọc auth.require_2fa. Chưa ghi, rỗng hay giá trị lạ là tắt (chỉ API
// và CLI ghi khoá này, cả hai đều kiểm giá trị).
func (s *Server) policy(ctx context.Context) (string, error) {
	v, _, err := s.deps.Store.GetSetting(ctx, settingRequire2FA)
	if err != nil {
		return "", err
	}
	if v == PolicyAdmin || v == PolicyAll {
		return v, nil
	}
	return PolicyOff, nil
}

func policyCovers(policy string, role account.Role) bool {
	return policy == PolicyAll || (policy == PolicyAdmin && role == account.RoleAdmin)
}

// enrollRequired: chính sách bắt user (vai role) bật hai bước mà họ chưa bật.
// Đọc mỗi request (mục 3.6): bật chính sách là áp ngay cho phiên và token đang có.
func (s *Server) enrollRequired(ctx context.Context, user string, role account.Role) (bool, error) {
	p, err := s.policy(ctx)
	if err != nil || !policyCovers(p, role) {
		return false, err
	}
	on, err := s.deps.Store.TOTPEnabled(ctx, user)
	return !on, err
}

func writeEnrollRequired(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "mfa_enroll_required", i18n.T(
		"Quản trị viên yêu cầu bật đăng nhập hai bước. Bật nó ở trang Tài khoản rồi dùng tiếp.",
		"The administrator requires two-step login. Turn it on in the Account page to continue."))
}

// tokenMaxDays đọc auth.token_max_days (1..MaxDaysLimit; chưa ghi hay hỏng là 90).
func (s *Server) tokenMaxDays(ctx context.Context) (int, error) {
	v, ok, err := s.deps.Store.GetSetting(ctx, settingTokenMaxDays)
	if err != nil {
		return 0, err
	}
	n, perr := strconv.Atoi(v)
	if !ok || perr != nil || n < 1 {
		return apitoken.DefaultMaxDays, nil
	}
	return min(n, apitoken.MaxDaysLimit), nil
}

// ---------------------------------------------------------------- mật khẩu, mã

// accountPassword kiểm mật khẩu của user cho thao tác đòi mật khẩu:
// authLocked → authCheckPassword → (sai) authFailed. true nghĩa là đúng.
func (s *Server) accountPassword(w http.ResponseWriter, r *http.Request, user, pw, action string) bool {
	ip := clientIP(r)
	if s.authLocked(w, r, ip) {
		return false
	}
	ok, done := s.authCheckPassword(w, r, user, pw)
	if done {
		return false
	}
	if !ok {
		s.authFailed(w, r, ip, action)
		return false
	}
	return true
}

// mfaCodeNeeded: user đã bật hai bước thì thao tác (tạo token, đổi mật khẩu)
// cần thêm mã (mục 11.5); thiếu thì 400 mfa_code_required ngay, trước argon2.
func (s *Server) mfaCodeNeeded(w http.ResponseWriter, r *http.Request, user, code string) (need, ok bool) {
	on, err := s.deps.Store.TOTPEnabled(r.Context(), user)
	if err != nil {
		authInternal(w, "đọc hai bước", err)
		return false, false
	}
	if on && strings.TrimSpace(code) == "" {
		writeError(w, http.StatusBadRequest, "mfa_code_required", i18n.T(
			"Tài khoản đã bật đăng nhập hai bước: nhập thêm mã 6 số trong app (hoặc một mã dự phòng).",
			"Two-step login is on: also enter the 6-digit code from your app (or a recovery code)."))
		return false, false
	}
	return on, true
}

// reauth đòi admin THẬT của phiên (store.Session.UserName, không phải người
// đang "xem như") xác nhận lại trước thao tác tạo quyền sống lâu hơn phiên:
// tạo user, nâng vai admin, đặt mật khẩu người khác, nới chính sách đăng nhập
// (soát cuối M20, F1). Phiên bị chiếm (chỉ có cookie) dừng ở đây, không tạo
// được cửa sau sống qua `tpan admin passwd`.
//
// Thứ tự: thiếu mật khẩu → 400 reauth_required; đã bật hai bước mà thiếu mã →
// 400 mfa_code_required (cả hai trước argon2); mật khẩu sai → 401
// wrong_password (đếm như đăng nhập sai); mã sai → 400 mfa_code_wrong (giao
// diện đưa mọi 401 khác wrong_password về màn đăng nhập). true là đã xác nhận.
func (s *Server) reauth(w http.ResponseWriter, r *http.Request, pw, code, action string) bool {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return false
	}
	if pw == "" {
		writeError(w, http.StatusBadRequest, "reauth_required", i18n.T(
			"Việc này cần nhập lại mật khẩu của bạn (và mã hai bước nếu đã bật) để xác nhận.",
			"This needs your password again (and your two-step code if it is on) to confirm."))
		return false
	}
	needCode, ok := s.mfaCodeNeeded(w, r, cur.UserName, code)
	if !ok {
		return false
	}
	if !s.accountPassword(w, r, cur.UserName, pw, action) {
		return false
	}
	if needCode {
		_, ok := s.checkSecondFactor(w, r, cur.UserName, code, sfAccount(action))
		return ok
	}
	return true
}

// writePasswordChanged trả lời đổi mật khẩu thành công: còn token đang sống
// thì 200 {active_tokens} để giao diện nhắc thu hồi nếu nghi bị lộ; không thì 204.
func (s *Server) writePasswordChanged(w http.ResponseWriter, r *http.Request, user string) {
	n, err := s.deps.Store.ActiveTokens(r.Context(), user, s.now())
	if err != nil {
		authInternal(w, "đếm token", err)
		return
	}
	if n == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, PasswordChanged{ActiveTokens: n})
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

// ---------------------------------------------------------------- hai bước của mình

// handleMFAState: GET /api/v1/account/2fa.
func (s *Server) handleMFAState(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	ctx, st := r.Context(), s.deps.Store
	row, found, err := st.TOTP(ctx, cur.UserName)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	left, err := st.RecoveryLeft(ctx, cur.UserName)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	p, err := s.policy(ctx)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MFAState{
		Enabled: found && !row.Enabled.IsZero(), EnabledAt: row.Enabled,
		Pending: found && row.Enabled.IsZero(), RecoveryLeft: left,
		Required: policyCovers(p, realRole(who(r))), Policy: p,
	})
}

func mfaAlreadyEnabled(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "mfa_already_enabled", i18n.T(
		"Đăng nhập hai bước đã bật. Muốn đổi điện thoại thì tắt rồi bật lại.",
		"Two-step login is already on. To move to a new phone, turn it off and on again."))
}

// handleMFASetup: POST /api/v1/account/2fa/setup. Sinh bí mật mới, niêm phong,
// ghi làm bí mật chờ (ghi đè bí mật chờ cũ); trả bí mật đúng một lần.
// Không đòi mật khẩu: bí mật chờ chưa làm gì cho tới khi enable (đòi mật khẩu).
func (s *Server) handleMFASetup(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	ctx, st, user := r.Context(), s.deps.Store, cur.UserName
	if on, err := st.TOTPEnabled(ctx, user); err != nil {
		apiFail(w, r, err)
		return
	} else if on {
		mfaAlreadyEnabled(w)
		return
	}
	key, err := s.mfaKey(ctx, true)
	if err != nil {
		mfaSecretUnavailable(w, user, err)
		return
	}
	secret := mfa.NewSecret()
	sealed, err := mfa.Seal(key, user, secret)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if err := st.SetPendingTOTP(ctx, user, sealed, s.now()); errors.Is(err, store.ErrTOTPEnabled) {
		mfaAlreadyEnabled(w)
		return
	} else if err != nil {
		apiFail(w, r, err)
		return
	}
	host := s.deps.Hostname
	if host == "" {
		host = "tpan"
	}
	acct := user + "@" + host
	uri := mfa.URI("TPAN", acct, secret)
	qr, err := mfa.QR(uri)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	s.audit(r, "account.2fa.setup", user, "ok", "")
	noStore(w)
	writeJSON(w, http.StatusOK, MFASetup{
		Secret: mfa.EncodeSecret(secret), URI: uri, QR: qr, Issuer: "TPAN", Account: acct,
	})
}

func mfaNotPending(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "mfa_not_pending", i18n.T(
		"Chưa có mã QR nào chờ xác nhận (hoặc vừa được tạo lại ở nơi khác). Bấm Bật lại để lấy mã mới.",
		"There is no QR code waiting to be confirmed (or it was just created again elsewhere). Start again to get a new one."))
}

// handleMFAEnable: POST /api/v1/account/2fa/enable {password, code}. Mật khẩu
// TRƯỚC, rồi mới đọc bí mật chờ (mục 11.5): bật đúng bản đã so mã, nhờ
// EnableTOTP so cả secret_enc. Mã sai ở đây không đếm (người đang bật nhập
// nhầm là chuyện thường, và mật khẩu đã được đếm).
func (s *Server) handleMFAEnable(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	var req MFAConfirm
	if !authDecode(w, r, &req) {
		return
	}
	ctx, st, user := r.Context(), s.deps.Store, cur.UserName
	if !s.accountPassword(w, r, user, req.Password, "account.2fa.enable") {
		return
	}
	row, found, err := st.TOTP(ctx, user)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if found && !row.Enabled.IsZero() {
		mfaAlreadyEnabled(w)
		return
	}
	if !found {
		mfaNotPending(w)
		return
	}
	key, err := s.mfaKey(ctx, false)
	if err != nil {
		mfaSecretUnavailable(w, user, err)
		return
	}
	secret, err := mfa.Open(key, user, row.SecretEnc)
	if err != nil {
		mfaSecretUnavailable(w, user, err)
		return
	}
	now := s.now()
	step, ok := mfa.Match(secret, strings.TrimSpace(req.Code), now)
	if !ok {
		writeError(w, http.StatusBadRequest, "mfa_code_wrong", i18n.T(
			"Mã không khớp mã QR vừa quét. Kiểm giờ của điện thoại rồi nhập mã đang hiện.",
			"The code does not match the QR code you just scanned. Check your phone's clock and enter the current code."))
		return
	}
	if s.beforeEnable != nil {
		s.beforeEnable()
	}
	codes := mfa.NewRecoveryCodes()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = mfa.HashRecovery(mfa.NormalizeRecovery(c))
	}
	err = st.EnableTOTP(ctx, user, row.SecretEnc, step, hashes, cur.TokenSHA256, now)
	if errors.Is(err, store.ErrTOTPNotPending) {
		mfaNotPending(w)
		return
	}
	if err != nil {
		apiFail(w, r, err)
		return
	}
	s.audit(r, "account.2fa.enable", user, "ok", "")
	noStore(w)
	writeJSON(w, http.StatusOK, RecoveryCodes{Codes: codes})
}

// mfaConfirmEnabled là phần chung của disable và recovery: đã bật, mật khẩu,
// rồi một mã (TOTP hoặc dự phòng) qua checkSecondFactor (sai thì đếm như bước hai).
func (s *Server) mfaConfirmEnabled(w http.ResponseWriter, r *http.Request, user, action string, req MFAConfirm) bool {
	on, err := s.deps.Store.TOTPEnabled(r.Context(), user)
	if err != nil {
		apiFail(w, r, err)
		return false
	}
	if !on {
		writeError(w, http.StatusConflict, "mfa_not_enabled", i18n.T(
			"Tài khoản này chưa bật đăng nhập hai bước.", "Two-step login is not on for this account."))
		return false
	}
	if !s.accountPassword(w, r, user, req.Password, action) {
		return false
	}
	_, ok := s.checkSecondFactor(w, r, user, req.Code, sfAccount(action))
	return ok
}

// handleMFADisable: POST /api/v1/account/2fa/disable {password, code}.
func (s *Server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	var req MFAConfirm
	if !authDecode(w, r, &req) {
		return
	}
	const action = "account.2fa.disable"
	p, err := s.policy(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if policyCovers(p, realRole(who(r))) {
		writeError(w, http.StatusConflict, "mfa_required_by_policy", i18n.T(
			"Quản trị viên bắt buộc đăng nhập hai bước nên không tắt được. Đổi điện thoại thì tạo lại mã dự phòng hoặc nhờ quản trị viên đặt lại.",
			"The administrator requires two-step login, so it cannot be turned off. To move to a new phone, ask an administrator to reset it."))
		return
	}
	if !s.mfaConfirmEnabled(w, r, cur.UserName, action, req) {
		return
	}
	if err := s.deps.Store.DisableTOTP(r.Context(), cur.UserName); err != nil {
		apiFail(w, r, err)
		return
	}
	s.audit(r, action, cur.UserName, "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleMFARecovery: POST /api/v1/account/2fa/recovery {password, code}: tạo
// lại 10 mã dự phòng (bộ cũ mất hiệu lực), trả đúng một lần.
func (s *Server) handleMFARecovery(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	var req MFAConfirm
	if !authDecode(w, r, &req) {
		return
	}
	const action = "account.2fa.recovery"
	if !s.mfaConfirmEnabled(w, r, cur.UserName, action, req) {
		return
	}
	codes := mfa.NewRecoveryCodes()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = mfa.HashRecovery(mfa.NormalizeRecovery(c))
	}
	if err := s.deps.Store.ReplaceRecoveryCodes(r.Context(), cur.UserName, hashes); err != nil {
		apiFail(w, r, err)
		return
	}
	s.audit(r, action, cur.UserName, "ok", "")
	noStore(w)
	writeJSON(w, http.StatusOK, RecoveryCodes{Codes: codes})
}

// ---------------------------------------------------------------- cài đặt đăng nhập

func (s *Server) authSettings(ctx context.Context) (AuthSettings, error) {
	p, err := s.policy(ctx)
	if err != nil {
		return AuthSettings{}, err
	}
	days, err := s.tokenMaxDays(ctx)
	if err != nil {
		return AuthSettings{}, err
	}
	return AuthSettings{Require2FA: p, TokenMaxDays: days, TokenGroups: groupNames(apitoken.Groups)}, nil
}

func groupNames(gs []apitoken.Group) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = string(g)
	}
	return out
}

// handleAuthSettings: GET /api/v1/settings/auth (admin, chỉ phiên).
func (s *Server) handleAuthSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := sessionOnly(w, r); !ok {
		return
	}
	out, err := s.authSettings(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAuthSettingsSet: POST /api/v1/settings/auth (admin, chỉ phiên). Bật
// chính sách khi chính người bật chưa bật hai bước thì từ chối (không tự nhốt mình).
func (s *Server) handleAuthSettingsSet(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	var in AuthSettingsInput
	if !authDecode(w, r, &in) {
		return
	}
	ctx, st := r.Context(), s.deps.Store
	if in.Require2FA != nil {
		switch *in.Require2FA {
		case PolicyOff, PolicyAdmin, PolicyAll:
		default:
			writeError(w, http.StatusBadRequest, "policy_invalid", i18n.T(
				"Chính sách phải là off, admin hoặc all.", "The policy must be off, admin or all."))
			return
		}
		if *in.Require2FA != PolicyOff {
			on, err := st.TOTPEnabled(ctx, cur.UserName)
			if err != nil {
				apiFail(w, r, err)
				return
			}
			if !on {
				writeError(w, http.StatusConflict, "mfa_enroll_self_first", i18n.T(
					"Bật đăng nhập hai bước cho chính bạn trước, rồi mới bắt buộc người khác.",
					"Turn on two-step login for yourself first, then require it for others."))
				return
			}
		}
	}
	if in.TokenMaxDays != nil && (*in.TokenMaxDays < 1 || *in.TokenMaxDays > apitoken.MaxDaysLimit) {
		writeError(w, http.StatusBadRequest, "token_days_invalid", i18n.T(
			"Trần thời hạn token phải từ 1 đến "+strconv.Itoa(apitoken.MaxDaysLimit)+" ngày.",
			"The token lifetime limit must be between 1 and "+strconv.Itoa(apitoken.MaxDaysLimit)+" days."))
		return
	}
	// Nới lỏng (hạ chính sách, tăng trần ngày) mở đường cho cửa sau: đòi xác
	// nhận lại (F1). Siết chặt thì không: phiên bị chiếm siết chỉ hại chính nó.
	loosen, err := s.authLoosens(ctx, in)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if loosen && !s.reauth(w, r, in.CurrentPassword, in.Code, "settings.auth") {
		return
	}
	if in.Require2FA != nil {
		if !s.auditDone(w, r, settingRequire2FA, *in.Require2FA, st.SetSetting(ctx, settingRequire2FA, *in.Require2FA)) {
			return
		}
	}
	if in.TokenMaxDays != nil {
		v := strconv.Itoa(*in.TokenMaxDays)
		if !s.auditDone(w, r, settingTokenMaxDays, v, st.SetSetting(ctx, settingTokenMaxDays, v)) {
			return
		}
	}
	s.handleAuthSettings(w, r)
}

// policyRank xếp chính sách từ lỏng tới chặt: off < admin < all.
var policyRank = map[string]int{PolicyOff: 0, PolicyAdmin: 1, PolicyAll: 2}

// authLoosens: in hạ chính sách hai bước hoặc tăng trần ngày của token so với
// giá trị đang áp (in đã qua kiểm giá trị).
func (s *Server) authLoosens(ctx context.Context, in AuthSettingsInput) (bool, error) {
	cur, err := s.authSettings(ctx)
	if err != nil {
		return false, err
	}
	return (in.Require2FA != nil && policyRank[*in.Require2FA] < policyRank[cur.Require2FA]) ||
		(in.TokenMaxDays != nil && *in.TokenMaxDays > cur.TokenMaxDays), nil
}

// ---------------------------------------------------------------- tên miền của panel

// apiPanel dựng ca sử dụng "tên miền của panel". System chờ khoá có hạn như apiSSL.
func (s *Server) apiPanel() *core.Panel {
	st := s.deps.Store
	return &core.Panel{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Sessions: st, Journal: st, Now: s.now,
	}
}

// handlePanel: GET /api/v1/panel.
func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, ok, err := s.apiPanel().Domain(ctx)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	var out PanelState
	if ok {
		out.Domain = d.String()
		res := checks.PanelACMECert(d, s.now).Run(ctx, s.deps.Sys)
		out.Cert = &res
	}
	self := checks.PanelCert(health.PanelCertPath, s.now).Run(ctx, s.deps.Sys)
	out.SelfSigned = &self
	writeJSON(w, http.StatusOK, out)
}

// panelPlan bọc Plan của panel: job xong (dù hỏng) thì bỏ đệm tên panel của
// GetCertificate (tls_m20.go), để panel đổi chứng chỉ ngay, không chờ 2 giây.
type panelPlan struct {
	*core.Planned
	after func()
}

func (p panelPlan) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	rep, err := p.Planned.Execute(ctx, onEvent)
	p.after()
	return rep, err
}

// handlePanelDomain: POST /api/v1/panel/domain[?dry_run=1] {domain}; domain
// rỗng là bỏ tên (về chỉ chứng chỉ tự ký).
func (s *Server) handlePanelDomain(w http.ResponseWriter, r *http.Request) {
	var body PanelDomainInput
	if !apiReadJSON(w, r, &body) {
		return
	}
	p := s.apiPanel()
	wrap := func(pl *core.Planned, err error) (apiPlan, error) {
		if err != nil {
			return nil, err // không bọc *Planned nil vào interface khác nil
		}
		return panelPlan{Planned: pl, after: s.panelDom.invalidate}, nil
	}
	if strings.TrimSpace(body.Domain) == "" {
		s.planJob(w, r, "panel.domain.clear", "", func(ctx context.Context) (apiPlan, error) {
			return wrap(p.PrepareClear(ctx))
		})
		return
	}
	d, err := validate.ParseDomain(body.Domain)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	s.planJob(w, r, "panel.domain", d.String(), func(ctx context.Context) (apiPlan, error) {
		return wrap(p.PrepareDomain(ctx, d))
	})
}
