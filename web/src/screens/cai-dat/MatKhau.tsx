// Đổi mật khẩu quản trị: POST /api/v1/account/password. Máy chủ kiểm mật khẩu
// hiện tại (sai thì đếm như đăng nhập sai), đổi mật khẩu rồi huỷ mọi phiên
// khác. Độ dài tối thiểu (12) phải khớp authMinPassword ở internal/server/auth.go;
// máy chủ đếm theo ký tự (rune), nên ở đây cũng đếm theo ký tự chứ không theo
// đơn vị UTF-16 của string.length.

import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { ApiError, errorMessage } from "../../api/client";
import { useChangePassword, useMFA } from "../../api/m20";
import { useWho } from "../../api/m13-users";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { TextField } from "../../components/Field";
import { Notice } from "../../components/Notice";
import { useLang, useStrings } from "../../i18n";
import { strings } from "../../i18n/cai-dat";
import { Note } from "./Note";
import { sessionsKey } from "./Phien";
import { MfaError } from "../tai-khoan/MfaError";
import { cleanCode, codeReady } from "../tai-khoan/model";

const minLength = 12;

export function MatKhau() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const qc = useQueryClient();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [repeat, setRepeat] = useState("");
  const [code, setCode] = useState("");
  // Lỗi bắt được ngay trong trình duyệt, trước khi gửi.
  const [localError, setLocalError] = useState<string | null>(null);
  const currentRef = useRef<HTMLInputElement>(null);
  const nextRef = useRef<HTMLInputElement>(null);
  const repeatRef = useRef<HTMLInputElement>(null);

  const { name } = useWho();
  const mfa = useMFA();
  const change = useChangePassword();
  // Đã bật hai bước thì đổi mật khẩu cần thêm mã; máy chủ đòi mà ta chưa biết (truy vấn chưa về) thì hiện ô mã.
  const codeAsked = change.error instanceof ApiError && change.error.code === "mfa_code_required";
  const needCode = mfa.data?.enabled === true || codeAsked;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    change.reset();
    if ([...next].length < minLength) {
      setLocalError(t.pwTooShort(minLength));
      nextRef.current?.focus();
      return;
    }
    if (next !== repeat) {
      setLocalError(t.pwMismatch);
      repeatRef.current?.focus();
      return;
    }
    if (needCode && !codeReady(code)) {
      setLocalError(t.pwCodeHint);
      return;
    }
    setLocalError(null);
    change.mutate(
      { current, new: next, ...(needCode && { code: cleanCode(code) }) },
      {
        onSuccess: () => {
          setCurrent("");
          setNext("");
          setRepeat("");
          setCode("");
          // Các phiên khác vừa bị huỷ: đọc lại danh sách.
          void qc.invalidateQueries({ queryKey: sessionsKey });
        },
        onError: (err) => {
          setCode("");
          if (err instanceof ApiError && err.code === "wrong_password") {
            setCurrent("");
            currentRef.current?.focus();
          }
        },
      },
    );
  };

  const wrongPassword = change.error instanceof ApiError && change.error.code === "wrong_password";
  const mfaCodeError = change.error instanceof ApiError && change.error.code.startsWith("mfa_");
  const serverError = change.error && !mfaCodeError ? (wrongPassword ? t.pwWrongCurrent : errorMessage(change.error, lang)) : null;
  const error = localError ?? serverError;
  // 200 {active_tokens}: còn token đang sống sau khi tự đổi mật khẩu (đặt lại bởi admin thì token đã bị thu hồi).
  const activeTokens = change.data?.active_tokens ?? 0;

  return (
    <Card title={t.pwHeading} titleId="mat-khau-tieu-de">
      <p className="mt-0 mb-[14px] max-w-[72ch] text-muted">{t.pwIntro(minLength)}</p>

      <form onSubmit={submit} className="flex max-w-[420px] flex-col gap-[16px]">
        <TextField
          ref={currentRef}
          id="mk-hien-tai"
          type="password"
          label={t.pwCurrent}
          autoComplete="current-password"
          required
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          invalid={wrongPassword}
        />
        <TextField
          ref={nextRef}
          id="mk-moi"
          type="password"
          label={t.pwNew}
          autoComplete="new-password"
          required
          value={next}
          onChange={(e) => setNext(e.target.value)}
        />
        <TextField
          ref={repeatRef}
          id="mk-nhap-lai"
          type="password"
          label={t.pwRepeat}
          autoComplete="new-password"
          required
          value={repeat}
          onChange={(e) => setRepeat(e.target.value)}
        />
        {needCode && (
          <TextField
            id="mk-ma"
            label={t.pwCode}
            hint={t.pwCodeHint}
            autoComplete="one-time-code"
            autoCapitalize="none"
            spellCheck={false}
            mono
            required
            value={code}
            onChange={(e) => setCode(e.target.value)}
            invalid={mfaCodeError}
          />
        )}
        <div>
          <Button type="submit" variant="primary" disabled={change.isPending}>
            {change.isPending ? t.pwBusy : t.pwSubmit}
          </Button>
        </div>
        {error && <Note kind="fail">{error}</Note>}
        {mfaCodeError && <MfaError error={change.error} user={name} />}
        {change.isSuccess && !error && <Note kind="pass">{t.pwDone}</Note>}
        {change.isSuccess && !error && activeTokens > 0 && (
          <Notice kind="warn" role="status">
            {t.pwTokens(activeTokens)} <Link to="/account#token">{t.pwTokensLink}</Link>
          </Notice>
        )}
      </form>
    </Card>
  );
}
