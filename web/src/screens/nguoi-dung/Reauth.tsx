// Hỏi lại admin đang thao tác (F1 của M20): tạo user, nâng vai admin, đặt lại
// mật khẩu user khác, nới lỏng cài đặt đăng nhập. Máy chủ đòi mật khẩu của
// chính admin (current_password) và, nếu admin đã bật hai bước, thêm mã (code),
// để một phiên bị chiếm không tạo được tài khoản cửa sau. Dùng chung cho hộp
// của màn Người dùng và hai thẻ của màn Cài đặt. Mật khẩu và mã chỉ nằm trong
// state cục bộ của nơi gọi hook; hộp đóng (unmount) là mất.

import { useState } from "react";
import { ApiError } from "../../api/client";
import { useMFA } from "../../api/m20";
import { useWho } from "../../api/m13-users";
import type { Reauth } from "../../api/types";
import { TextField } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { useStrings } from "../../i18n";
import { strings } from "../../i18n/nguoi-dung";
import { strings as tk } from "../../i18n/tai-khoan";
import { ErrorNote } from "../site/ErrorNote";
import { MfaError } from "../tai-khoan/MfaError";
import { cleanCode, codeReady } from "../tai-khoan/model";

/** Hai ô và phần kiểm tại chỗ. `error` là lỗi lần gửi trước: máy chủ đòi mã mà ta chưa biết (truy vấn hai bước chưa về) thì hiện ô mã. */
export function useReauth(error: unknown) {
  const t = useStrings(strings);
  const mfa = useMFA();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const asked = error instanceof ApiError && error.code === "mfa_code_required";
  const needCode = mfa.data?.enabled === true || asked;
  return {
    password,
    code,
    needCode,
    setPassword,
    setCode,
    /** Câu báo thiếu, hoặc null khi đủ để gửi. */
    problem: (): string | null => (password === "" ? t.reauthPasswordMissing : needCode && !codeReady(code) ? t.reauthCodeMissing : null),
    /** Phần gộp vào thân yêu cầu (kiểu Reauth). */
    body: (): Reauth => ({ current_password: password, ...(needCode && { code: cleanCode(code) }) }),
    /** Mã dùng một lần: sai hay đã gửi thì xoá; mật khẩu sai thì xoá luôn. */
    afterError: (err: unknown) => {
      setCode("");
      if (err instanceof ApiError && err.code === "wrong_password") setPassword("");
    },
  };
}

type Fields = Pick<ReturnType<typeof useReauth>, "password" | "code" | "needCode" | "setPassword" | "setCode">;

/** `why`: một câu nói vì sao máy hỏi lại việc này. `idPrefix` để hai bộ ô trong một trang không trùng id. */
export function ReauthFields({ r, why, idPrefix, error, autoFocus = false }: { r: Fields; why: string; idPrefix: string; error?: unknown; autoFocus?: boolean }) {
  const t = useStrings(strings);
  const code = error instanceof ApiError && error.code.startsWith("mfa_code_");
  const pw = error instanceof ApiError && (error.code === "wrong_password" || error.code === "reauth_required");
  return (
    <div className="flex flex-col gap-[14px] border-t border-hair pt-[14px]">
      <p className="m-0 text-[13px] text-muted">{why}</p>
      <TextField
        id={`${idPrefix}-mat-khau-cua-ban`}
        type="password"
        label={t.reauthPassword}
        autoComplete="current-password"
        autoFocus={autoFocus}
        value={r.password}
        onChange={(e) => r.setPassword(e.target.value)}
        invalid={pw}
      />
      {r.needCode && (
        <TextField
          id={`${idPrefix}-ma-hai-buoc`}
          label={t.reauthCode}
          hint={t.reauthCodeHint}
          autoComplete="one-time-code"
          autoCapitalize="none"
          spellCheck={false}
          mono
          value={r.code}
          onChange={(e) => r.setCode(e.target.value)}
          invalid={code}
        />
      )}
    </div>
  );
}

/** Lỗi của lần gửi: các mã hỏi lại có câu riêng; mã khác để MfaError và ErrorNote lo (câu của máy chủ). */
export function ReauthError({ error }: { error: unknown }) {
  const t = useStrings(strings);
  const w = useStrings(tk);
  const { name } = useWho();
  if (error instanceof ApiError) {
    const text =
      error.code === "reauth_required"
        ? t.errReauthRequired
        : error.code === "login_locked"
          ? t.errLoginLocked(w.waitText(Math.max(1, Math.ceil((error.retryAfter ?? 900) / 60))))
          : null;
    if (text !== null) {
      return (
        <Notice kind="fail" role="alert">
          {text}
        </Notice>
      );
    }
    return <MfaError error={error} user={name} />;
  }
  return <ErrorNote error={error} />;
}
