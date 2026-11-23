package server

// Hợp đồng API của M20 (docs/thay-doi/M20-thiet-ke.md, mục 3.4 đến 3.8):
// đăng nhập hai bước, token API, chứng chỉ panel. Bản TypeScript cùng nghĩa
// nằm ở web/src/api/m20-types.ts; đổi một bên thì đổi cả bên kia. Quy ước lỗi
// như M3 (types.go). Luật route như M13 (routes.go), thêm trường nhóm token
// (rỗng là chỉ phiên, bảng ở mục 3.7 của thiết kế).
//
// Đăng nhập (public):
//
//	POST /api/v1/login            {name, password} → 204 (không có hai bước, như cũ)
//	                                                → 200 LoginResult{mfa_required:true} + cookie phiếu
//	POST /api/v1/login/2fa        {code}           → 204, hoặc 200 LoginResult{recovery_left} khi dùng mã dự phòng
//	GET  /api/v1/session                           → SessionInfo (thêm mfa_pending, mfa_enroll_required)
//	POST /api/v1/logout                            → 204 (xoá cả phiên lẫn phiếu)
//
// Hai bước của chính mình (any, chỉ phiên, user thật của phiên):
//
//	GET  /api/v1/account/2fa                       → MFAState            (allowEnroll)
//	POST /api/v1/account/2fa/setup     {}          → MFASetup            (allowEnroll, no-store)
//	POST /api/v1/account/2fa/enable    MFAConfirm  → RecoveryCodes       (allowEnroll, no-store)
//	POST /api/v1/account/2fa/disable   MFAConfirm  → 204
//	POST /api/v1/account/2fa/recovery  MFAConfirm  → RecoveryCodes       (no-store)
//
// Token của chính mình (any, chỉ phiên):
//
//	GET    /api/v1/account/tokens                  → MyTokens
//	POST   /api/v1/account/tokens      TokenCreate → 201 TokenCreated   (no-store)
//	DELETE /api/v1/account/tokens/{id}             → 204
//
// Admin (chỉ phiên):
//
//	GET    /api/v1/settings/auth                   → AuthSettings
//	POST   /api/v1/settings/auth  AuthSettingsInput → AuthSettings
//	GET    /api/v1/tokens                          → []TokenRow (mọi user)
//	DELETE /api/v1/tokens/{id}                     → 204
//
// Admin, nhóm token "system":
//
//	GET  /api/v1/panel                             → PanelState
//	POST /api/v1/panel/domain?dry_run=1  PanelDomainInput → bản xem trước (như mọi Plan)
//	POST /api/v1/panel/domain            PanelDomainInput → 202 {job_id} (như planJob)
//
// Mã lỗi mới: mfa_challenge_invalid (401), mfa_code_wrong (401 ở bước hai,
// 400 ở bật/tắt), mfa_code_used (401), mfa_locked (423, Retry-After),
// mfa_secret_unavailable (503), mfa_already_enabled, mfa_not_pending,
// mfa_not_enabled, mfa_required_by_policy, mfa_enroll_self_first (409),
// mfa_enroll_required (403), token_invalid, token_malformed, token_expired,
// token_revoked (401), token_forbidden, token_scope (403), token_name_invalid,
// token_scope_invalid, token_days_invalid, policy_invalid (400),
// token_not_found (404), panel_domain_is_site, domain_is_panel, panel_staging,
// panel_private (409), host_is_site (421). Mật khẩu sai ở thao tác đòi mật
// khẩu: wrong_password (401) / login_locked (423) như đăng nhập. Thiếu mã khi
// user đã bật hai bước: mfa_code_required (400).
//
// Bổ sung khi viết server (đè lên câu trên khi khác):
//   - mfa_code_wrong và mfa_code_used là 401 CHỈ ở POST /login/2fa; ở mọi
//     route /account/* (2fa/*, tokens, password) là 400.
//   - Lần sai làm khoá user (lần thứ 10) trả luôn 423 mfa_locked (Retry-After).
//   - mfa_not_enabled (409) có thể về từ disable, recovery, tạo token, đổi mật
//     khẩu khi hai bước bị tắt giữa chừng.
//   - mfa_busy (503): request bị huỷ khi đang chờ lượt kiểm mã của user.
//   - host_missing (400): POST /login không có header Host.
//   - host_is_site (421) ở MỌI đường dẫn khi tên máy là tên một site: JSON với
//     /api/, trang HTML ngắn với đường dẫn khác.
//   - token_scope, token_forbidden (403); token_* (401) như trên.

import (
	"time"

	"github.com/doxuta/tpan/internal/checks"
)

// LoginResult là thân 200 của POST /login (khi cần bước hai) và của POST
// /login/2fa (khi vừa dùng một mã dự phòng). 204 không có thân.
type LoginResult struct {
	MFARequired bool `json:"mfa_required,omitempty"`
	// RecoveryLeft: số mã dự phòng còn lại, chỉ có khi vừa đăng nhập bằng một mã dự phòng.
	RecoveryLeft *int `json:"recovery_left,omitempty"`
	// WrongCodes: số lần có người nhập sai mã hai bước cho tài khoản này kể từ
	// lần đăng nhập đúng trước (R5 của soát lượt hai): khác 0 nghĩa là mật khẩu
	// có thể đã lộ. Có thì POST /login/2fa trả 200 thay vì 204.
	WrongCodes int `json:"wrong_codes,omitempty"`
}

// LoginCode là thân của POST /login/2fa: 6 chữ số TOTP, hoặc một mã dự phòng
// (16 ký tự, có hay không có dấu gạch và khoảng trắng).
type LoginCode struct {
	Code string `json:"code"`
}

// Chính sách bắt buộc hai bước (settings auth.require_2fa).
const (
	PolicyOff   = "off"
	PolicyAdmin = "admin"
	PolicyAll   = "all"
)

// MFAState là trạng thái hai bước của user đang đăng nhập.
type MFAState struct {
	Enabled   bool      `json:"enabled"`
	EnabledAt time.Time `json:"enabled_at,omitzero"`
	// Pending: đã gọi setup mà chưa xác nhận bằng một mã.
	Pending      bool `json:"pending"`
	RecoveryLeft int  `json:"recovery_left"`
	// Required: chính sách hiện tại bắt user này bật (thì không tắt được).
	Required bool   `json:"required"`
	Policy   string `json:"policy"`
}

// MFASetup là bí mật mới, hiện đúng một lần (Cache-Control: no-store).
type MFASetup struct {
	// Secret: base32 chữ hoa không đệm (32 ký tự) để gõ tay vào app.
	Secret string `json:"secret"`
	// URI: otpauth://totp/TPAN:<user>@<máy>?secret=...&issuer=TPAN&algorithm=SHA1&digits=6&period=30
	URI string `json:"uri"`
	// QR: ma trận QR của URI, mỗi chuỗi một hàng, '1' là module tối. Không kèm
	// vùng trắng: giao diện tự thêm 4 module mỗi bên.
	QR []string `json:"qr"`
	// Issuer và Account để hiện cạnh QR (tên app sẽ ghi).
	Issuer  string `json:"issuer"`
	Account string `json:"account"`
}

// MFAConfirm là thân của enable, disable, recovery: mật khẩu hiện tại và một
// mã (enable: chỉ TOTP từ bí mật đang chờ; disable, recovery: TOTP hoặc mã dự phòng).
type MFAConfirm struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// RecoveryCodes là 10 mã dự phòng, hiện đúng một lần (no-store).
type RecoveryCodes struct {
	Codes []string `json:"recovery_codes"`
}

// TokenRow là một token trong danh sách. Không bao giờ có token thật.
type TokenRow struct {
	ID       string    `json:"id"`
	User     string    `json:"user"`
	Name     string    `json:"name"`
	Scopes   []string  `json:"scopes"`
	Hint     string    `json:"hint"`
	Created  time.Time `json:"created"`
	Expires  time.Time `json:"expires"`
	Revoked  time.Time `json:"revoked,omitzero"`
	LastUsed time.Time `json:"last_used,omitzero"`
	LastIP   string    `json:"last_ip,omitempty"`
	// Status tính lúc trả lời: "active", "expired", "revoked".
	Status string `json:"status"`
}

// TokenCreate là thân của POST /account/tokens. Code bắt buộc khi user đã bật
// hai bước (TOTP hoặc mã dự phòng; mục 11.5 của thiết kế).
type TokenCreate struct {
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	Days     int      `json:"days"`
	Password string   `json:"password"`
	Code     string   `json:"code,omitempty"`
}

// MyTokens là câu trả lời của GET /account/tokens: token của chính mình, kèm
// trần số ngày và các nhóm người gọi được chọn (để vẽ hộp tạo token).
type MyTokens struct {
	Tokens  []TokenRow `json:"tokens"`
	MaxDays int        `json:"max_days"`
	Groups  []string   `json:"groups"`
}

// PasswordChanged là thân 200 của POST /account/password khi user còn token
// đang sống (giao diện nhắc thu hồi nếu nghi bị lộ); không còn token thì 204.
type PasswordChanged struct {
	ActiveTokens int `json:"active_tokens"`
}

// TokenCreated: Token chỉ có ở đúng câu trả lời này (no-store).
type TokenCreated struct {
	Token string   `json:"token"`
	Row   TokenRow `json:"row"`
}

// AuthSettings là cài đặt đăng nhập của cả máy (admin).
type AuthSettings struct {
	Require2FA string `json:"require_2fa"` // PolicyOff | PolicyAdmin | PolicyAll
	// TokenMaxDays: trần số ngày của token mới (1-365; chưa đặt là 90).
	TokenMaxDays int `json:"token_max_days"`
	// TokenGroups: các nhóm phạm vi có thể chọn (để giao diện vẽ ô chọn).
	TokenGroups []string `json:"token_groups"`
}

// AuthSettingsInput: trường nil thì giữ nguyên.
type AuthSettingsInput struct {
	Require2FA   *string `json:"require_2fa,omitempty"`
	TokenMaxDays *int    `json:"token_max_days,omitempty"`
	// CurrentPassword, Code (soát cuối F1): bắt buộc khi nới lỏng (hạ chính
	// sách hai bước, tăng token_max_days).
	CurrentPassword string `json:"current_password,omitempty"`
	Code            string `json:"code,omitempty"`
}

// PanelState là câu trả lời của GET /api/v1/panel.
type PanelState struct {
	// Domain: tên miền của panel; rỗng khi chưa đặt (chỉ chứng chỉ tự ký).
	Domain string `json:"domain"`
	// Cert: phép kiểm tpan.panelcert:<domain>; nil khi chưa đặt tên.
	Cert *checks.Result `json:"cert,omitempty"`
	// SelfSigned: phép kiểm chứng chỉ tự ký (tpan.cert:/etc/tpan/tls/cert.pem).
	SelfSigned *checks.Result `json:"self_signed,omitempty"`
}

// PanelDomainInput: Domain rỗng là xoá tên (về chỉ tự ký).
type PanelDomainInput struct {
	Domain string `json:"domain"`
}
