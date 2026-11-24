// Gọi API của M20 (đăng nhập hai bước, token API, tên miền của panel). Đường
// dẫn và kiểu nằm ở m20-types.ts. Theo mẫu m15.ts: tách khỏi queries.ts để các
// mốc chạy song song không sửa chung một file.
//
// Bí mật (chuỗi bí mật TOTP, mã dự phòng, token) CHỈ đi qua các mutation dưới
// đây và về tới tay màn gọi bằng mutateAsync. Mutation đặt gcTime: 0 để
// MutationCache không giữ kết quả (và mật khẩu trong variables) sau khi xong;
// màn giữ bí mật trong state của chính nó và bỏ khi đóng. Không bí mật nào vào
// query cache, localStorage hay console.
//
//   const mfa = useMFA();                       // GET /account/2fa
//   const setup = useMFASetup();                // setup.mutateAsync() → MFASetup (một lần)
//   const enable = useMFAEnable();              // enable.mutateAsync({password, code}) → RecoveryCodes (một lần)
//   const tokens = useMyTokens();               // GET /account/tokens → MyTokens
//   const create = useCreateToken();            // create.mutateAsync(TokenCreate) → TokenCreated (một lần)

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { del, get, post } from "./client";
import {
  allTokensPath,
  authSettingsPath,
  loginCodePath,
  mfaDisablePath,
  mfaEnablePath,
  mfaPath,
  mfaRecoveryPath,
  mfaSetupPath,
  myTokensPath,
  panelPath,
  type AuthSettings,
  type AuthSettingsInput,
  type LoginResult,
  type MFAConfirm,
  type MFASetup,
  type MFAState,
  type MyTokens,
  type PanelState,
  type PasswordChanged,
  type RecoveryCodes,
  type TokenCreate,
  type TokenCreated,
  type TokenRow,
} from "./m20-types";
import { queryKeys } from "./queries";
import type { PasswordChange } from "./types";

export const mfaKey = ["account", "2fa"] as const;
export const myTokensKey = ["account", "tokens"] as const;
export const allTokensKey = ["tokens"] as const;
export const authSettingsKey = ["settings", "auth"] as const;
export const panelKey = ["panel"] as const;

const tokenPath = (base: string, id: string) => `${base}/${encodeURIComponent(id)}`;

// ---- Đăng nhập ----

/** Bước hai: 204 (undefined) hoặc 200 {recovery_left} khi dùng mã dự phòng. Không gọi gì khác; màn tự đọc lại phiên. */
export function useLoginCode() {
  return useMutation({ gcTime: 0, mutationFn: (code: string) => post<LoginResult | undefined>(loginCodePath, { code }) });
}

// ---- Hai bước của chính mình ----

export function useMFA(enabled = true) {
  return useQuery({ queryKey: mfaKey, queryFn: () => get<MFAState>(mfaPath), enabled });
}

/** Đọc lại hai bước và phiên (mfa_enroll_required): gọi khi người dùng đã bấm "Đã lưu", không sớm hơn. */
export function useRefreshMFA() {
  const qc = useQueryClient();
  return async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: mfaKey }), qc.invalidateQueries({ queryKey: queryKeys.session })]);
  };
}

export function useMFASetup() {
  return useMutation({ gcTime: 0, mutationFn: () => post<MFASetup>(mfaSetupPath, {}) });
}

export function useMFAEnable() {
  return useMutation({ gcTime: 0, mutationFn: (b: MFAConfirm) => post<RecoveryCodes>(mfaEnablePath, b) });
}

/** Tắt hai bước: xong thì đọc lại trạng thái. */
export function useMFADisable() {
  const refresh = useRefreshMFA();
  return useMutation({ gcTime: 0, mutationFn: (b: MFAConfirm) => post<void>(mfaDisablePath, b), onSuccess: () => refresh() });
}

/** Tạo lại mười mã dự phòng: mã mới hiện một lần; màn gọi đọc lại trạng thái sau khi người dùng bấm "Đã lưu". */
export function useMFARecovery() {
  return useMutation({ gcTime: 0, mutationFn: (b: MFAConfirm) => post<RecoveryCodes>(mfaRecoveryPath, b) });
}

// ---- Token ----

export function useMyTokens() {
  return useQuery({ queryKey: myTokensKey, queryFn: () => get<MyTokens>(myTokensPath) });
}

export function useCreateToken() {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (b: TokenCreate) => post<TokenCreated>(myTokensPath, b),
    // Danh sách chỉ có hint; token đầy đủ không bao giờ vào cache.
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: myTokensKey });
      void qc.invalidateQueries({ queryKey: allTokensKey });
    },
  });
}

/** Thu hồi token của mình (mine) hoặc của bất kỳ ai (all, chỉ admin). */
export function useRevokeToken(scope: "mine" | "all") {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (id: string) => del<void>(tokenPath(scope === "mine" ? myTokensPath : allTokensPath, id)),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: myTokensKey });
      void qc.invalidateQueries({ queryKey: allTokensKey });
    },
  });
}

/** Admin: mọi token của mọi user. */
export function useAllTokens(enabled = true) {
  return useQuery({ queryKey: allTokensKey, queryFn: () => get<TokenRow[]>(allTokensPath), enabled });
}

// ---- Cài đặt (admin) ----

export function useAuthSettings(enabled = true) {
  return useQuery({ queryKey: authSettingsKey, queryFn: () => get<AuthSettings>(authSettingsPath), enabled });
}

/** Lưu chính sách hay trần ngày: máy chủ trả cả bộ cài đặt mới, ghi thẳng vào bộ đệm. */
export function useSaveAuthSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: AuthSettingsInput) => post<AuthSettings>(authSettingsPath, b),
    onSuccess: (s) => {
      qc.setQueryData(authSettingsKey, s);
      // Chính sách đổi thì trạng thái "bắt buộc" của chính mình đổi theo.
      void qc.invalidateQueries({ queryKey: mfaKey });
    },
  });
}

export function usePanel(enabled = true) {
  return useQuery({ queryKey: panelKey, queryFn: () => get<PanelState>(panelPath), enabled });
}

// ---- Đổi mật khẩu có mã ----

/** POST /account/password: 204, hoặc 200 {active_tokens} khi còn token đang sống. */
export function useChangePassword() {
  return useMutation({ gcTime: 0, mutationFn: (b: PasswordChange) => post<PasswordChanged | undefined>("/api/v1/account/password", b) });
}
