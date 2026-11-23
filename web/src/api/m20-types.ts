// Kiểu của API M20 (docs/thay-doi/M20-thiet-ke.md, mục 3.4 đến 3.8): đăng nhập
// hai bước, token API, chứng chỉ panel. Cùng nghĩa với
// internal/server/types_m20.go; đổi một bên thì đổi cả bên kia.
//
// Đăng nhập (public):
//   POST /api/v1/login        {name, password} → 204 (không có hai bước)
//                                              → 200 LoginResult {mfa_required: true} + cookie phiếu
//   POST /api/v1/login/2fa    {code}           → 204, hoặc 200 LoginResult {recovery_left, wrong_codes}
//   GET  /api/v1/session                       → SessionInfo (thêm mfa_pending, mfa_enroll_required, xem types.ts)
//
// Hai bước của chính mình:
//   GET  /api/v1/account/2fa                   → MFAState
//   POST /api/v1/account/2fa/setup     {}      → MFASetup      (hiện một lần)
//   POST /api/v1/account/2fa/enable    MFAConfirm → RecoveryCodes (hiện một lần)
//   POST /api/v1/account/2fa/disable   MFAConfirm → 204
//   POST /api/v1/account/2fa/recovery  MFAConfirm → RecoveryCodes (hiện một lần)
//
// Token của chính mình:
//   GET    /api/v1/account/tokens               → MyTokens
//   POST   /api/v1/account/tokens   TokenCreate → 201 TokenCreated (token hiện một lần)
//   DELETE /api/v1/account/tokens/{id}          → 204
//
// Admin:
//   GET    /api/v1/settings/auth                → AuthSettings
//   POST   /api/v1/settings/auth  AuthSettingsInput → AuthSettings
//   GET    /api/v1/tokens                       → TokenRow[] (mọi user)
//   DELETE /api/v1/tokens/{id}                  → 204
//   GET    /api/v1/panel                        → PanelState
//   POST   /api/v1/panel/domain?dry_run=1  PanelDomainInput → Preview (như mọi Plan)
//   POST   /api/v1/panel/domain            PanelDomainInput → 202 JobStarted
//
// Mã lỗi: mfa_challenge_invalid, mfa_code_wrong, mfa_code_used, mfa_locked (423,
// Retry-After), mfa_secret_unavailable (503), mfa_already_enabled, mfa_not_pending,
// mfa_not_enabled, mfa_required_by_policy, mfa_enroll_self_first, mfa_enroll_required,
// token_name_invalid, token_scope_invalid, token_days_invalid, policy_invalid,
// token_not_found, panel_domain_is_site, domain_is_panel, panel_staging, panel_private,
// mfa_code_required (thiếu mã khi user đã bật hai bước: tạo token, đổi mật khẩu),
// host_is_site (421: panel không phục vụ ở tên của bất kỳ site nào, kể cả site của admin; vào bằng IP hoặc tên miền panel); mật khẩu
// sai ở các thao tác đòi mật khẩu: wrong_password (401) hoặc login_locked (423).

import type { CheckResult } from "./types";

export const loginCodePath = "/api/v1/login/2fa";
export const mfaPath = "/api/v1/account/2fa";
export const mfaSetupPath = "/api/v1/account/2fa/setup";
export const mfaEnablePath = "/api/v1/account/2fa/enable";
export const mfaDisablePath = "/api/v1/account/2fa/disable";
export const mfaRecoveryPath = "/api/v1/account/2fa/recovery";
export const myTokensPath = "/api/v1/account/tokens";
export const allTokensPath = "/api/v1/tokens";
export const authSettingsPath = "/api/v1/settings/auth";
export const panelPath = "/api/v1/panel";
export const panelDomainPath = "/api/v1/panel/domain";

/** Lệnh root gõ trên máy khi user mất điện thoại và hết mã dự phòng. */
export const resetCommand = (user: string) => `sudo tpan user 2fa reset ${user}`;

export type LoginResult = {
  mfa_required?: boolean;
  /** Chỉ có khi vừa đăng nhập bằng một mã dự phòng. */
  recovery_left?: number;
  /** Số lần có người nhập sai mã hai bước kể từ lần đăng nhập đúng trước; khác 0 thì mật khẩu có thể đã lộ. */
  wrong_codes?: number;
};

export type Policy = "off" | "admin" | "all";

export type MFAState = {
  enabled: boolean;
  enabled_at?: string;
  /** Đã gọi setup mà chưa xác nhận bằng một mã. */
  pending: boolean;
  recovery_left: number;
  /** Chính sách hiện tại bắt user này bật (khi đó không tắt được). */
  required: boolean;
  policy: Policy;
};

export type MFASetup = {
  /** Base32 chữ hoa (32 ký tự) để gõ tay vào app. */
  secret: string;
  uri: string;
  /** Ma trận QR, mỗi chuỗi một hàng, "1" là module tối; chưa có vùng trắng (tự thêm 4 module mỗi bên). */
  qr: string[];
  issuer: string;
  account: string;
};

export type MFAConfirm = { password: string; code: string };

export type RecoveryCodes = { recovery_codes: string[] };

export type TokenStatus = "active" | "expired" | "revoked";

export type TokenRow = {
  id: string;
  user: string;
  name: string;
  /** Dạng "sites:read", "databases:write"... (ghi bao gồm đọc). */
  scopes: string[];
  /** "tpan_ab12cd…": đủ để nhận ra, không dùng được. */
  hint: string;
  created: string;
  expires: string;
  revoked?: string;
  last_used?: string;
  last_ip?: string;
  status: TokenStatus;
};

/** code bắt buộc khi user đã bật hai bước (TOTP hoặc mã dự phòng); thiếu thì 400 mfa_code_required. */
export type TokenCreate = { name: string; scopes: string[]; days: number; password: string; code?: string };

export type TokenCreated = { token: string; row: TokenRow };

/** GET /api/v1/account/tokens: token của mình, trần số ngày, các nhóm mình được chọn. */
export type MyTokens = { tokens: TokenRow[]; max_days: number; groups: TokenGroup[] };

/** POST /api/v1/account/password trả 200 thân này khi còn token đang sống (không còn thì 204). */
export type PasswordChanged = { active_tokens: number };

/** Nhóm phạm vi của token (mục 3.7 của thiết kế). "system" chỉ admin chọn được. */
export type TokenGroup = "sites" | "databases" | "backups" | "cron" | "jobs" | "health" | "ssl" | "system";

export type AuthSettings = {
  require_2fa: Policy;
  /** Trần số ngày của token mới (1-365). */
  token_max_days: number;
  token_groups: TokenGroup[];
};

export type AuthSettingsInput = { require_2fa?: Policy; token_max_days?: number };

export type PanelState = {
  /** Rỗng khi chưa đặt tên miền (panel chỉ có chứng chỉ tự ký). */
  domain: string;
  /** Phép kiểm tpan.panelcert:<domain>; không có khi chưa đặt tên. */
  cert?: CheckResult;
  /** Phép kiểm chứng chỉ tự ký của panel. */
  self_signed?: CheckResult;
};

export type PanelDomainInput = { domain: string };
