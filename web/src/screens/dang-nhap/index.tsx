// Màn Đăng nhập (mục 10 của KE-HOACH.md, màn 1). Không có khung trang (routes.tsx
// đánh dấu bare): logo, hai nút đoạn (ngôn ngữ, sáng/tối), một thẻ giữa trang
// trên nền --paper; phần "Quên mật khẩu" (và lời nhắc chế độ demo) là chân thẻ (bản 2).
//
// Bước một: POST /api/v1/login. 204 là xong và đặt cookie; 200 {mfa_required}
// (M20) là user đã bật hai bước: sang bước hai ngay trong thẻ này. Bước hai: POST
// /api/v1/login/2fa {code}, mã 6 số hoặc mã dự phòng 16 ký tự. 204 → xong; 200
// {recovery_left} → xong bằng mã dự phòng, báo còn mấy mã rồi mới đi tiếp. Sau
// đó đọc lại phiên (GET /api/v1/session) rồi về ?next= (đã lọc bằng safeNext)
// hoặc "/". Tải lại trang đúng lúc ở bước hai thì phiên báo mfa_pending và thẻ
// mở thẳng bước hai. "Quay lại" gọi /logout để xoá phiếu chờ.
//
// Lỗi theo mã: wrong_password, login_locked (423, Retry-After), not_set_up (503);
// bước hai thêm mfa_code_wrong, mfa_code_used, mfa_challenge_invalid (về bước một),
// mfa_locked (423, Retry-After), mfa_secret_unavailable (503).

import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState, type FormEvent } from "react";
import { Navigate, useSearchParams } from "react-router";
import { ApiError, errorMessage, post, safeNext } from "../../api/client";
import { useLoginCode } from "../../api/m20";
import { resetCommand as reset2fa, type LoginResult } from "../../api/m20-types";
import { queryKeys, useSession } from "../../api/queries";
import { Button, buttonClass } from "../../components/Button";
import { Card } from "../../components/Card";
import { TextField } from "../../components/Field";
import { MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { ScreenHeader } from "../../components/ScreenHeader";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/dang-nhap";
import { strings as khung } from "../../i18n/khung";
import { cleanRecovery, cleanTotp, recoveryComplete, recoveryLowNow, totpComplete } from "../tai-khoan/model";
import { BareFrame } from "./Frame";

const setupCommand = "sudo tpan setup";
const resetCommand = "sudo tpan admin passwd";

type Stage = "password" | "code";
type Mode = "totp" | "recovery";

type Failure =
  | { kind: "wrong" }
  | { kind: "suspended" }
  | { kind: "locked"; retryAfter: number | null; message: string }
  | { kind: "mfaLocked"; retryAfter: number | null; message: string }
  | { kind: "notSetUp" }
  | { kind: "noSession" }
  | { kind: "codeWrong" }
  | { kind: "codeUsed" }
  | { kind: "challenge" }
  | { kind: "secret" }
  | { kind: "backFailed" }
  | { kind: "other"; message: string };

export default function DangNhap() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang } = useLang();
  const qc = useQueryClient();
  const session = useSession();
  const loginCode = useLoginCode();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));

  // Để trống tên là "admin" (máy một người dùng trước M13 chỉ có tài khoản này).
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  // Giờ khoá tính một lần lúc nhận lỗi, để chữ không nhảy mỗi lần vẽ lại.
  const [lockedAt, setLockedAt] = useState(0);
  // Bước hai: chọn tay hoặc theo phiên (tải lại trang giữa chừng thì phiên báo mfa_pending).
  const [stageState, setStage] = useState<Stage | null>(null);
  const stage: Stage = stageState ?? (session.data?.mfa_pending === true ? "code" : "password");
  const [mode, setMode] = useState<Mode>("totp");
  const [code, setCode] = useState("");
  // Tên vừa gõ ở bước một, để câu lỗi chỉ đúng lệnh gỡ hai bước; null khi tải lại giữa chừng (không biết tên).
  const [typedName, setTypedName] = useState<string | null>(null);
  // Vừa vào xong mà có điều cần đọc (còn N mã dự phòng, có người nhập sai mã): giữ lại để đọc trước khi đi tiếp.
  const [held, setHeld] = useState<{ left?: number; wrong?: number } | null>(null);
  const [after, setAfter] = useState<string | null>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);

  const readSession = () => qc.invalidateQueries({ queryKey: queryKeys.session });

  // Đã đăng nhập rồi (mở /dang-nhap bằng tay, hoặc vừa đăng nhập xong): đi tiếp. Đang đọc
  // thông báo thì chưa đi: phiên có thể đã đọc lại (cửa sổ lấy lại focus) trước khi người dùng đọc xong.
  if (session.data?.logged_in && held === null) return <Navigate to={after ?? next} replace />;

  const finish = async () => {
    // Đọc lại phiên: nếu logged_in thì dòng Navigate ở trên tự chạy.
    await readSession();
    const s = qc.getQueryData<{ logged_in: boolean }>(queryKeys.session);
    if (!s?.logged_in) setFailure({ kind: "noSession" });
  };

  const lockFailure = (err: unknown, kind: "locked" | "mfaLocked"): Failure => {
    setLockedAt(Date.now());
    return { kind, retryAfter: err instanceof ApiError ? err.retryAfter : null, message: errorMessage(err, lang) };
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setFailure(null);
    try {
      const res = await post<LoginResult | undefined>("/api/v1/login", { name: name.trim(), password });
      if (res?.mfa_required) {
        // Mật khẩu đúng, còn bước hai: không giữ mật khẩu trong ô nữa.
        setTypedName(name.trim() || "admin");
        setPassword("");
        setCode("");
        setMode("totp");
        setStage("code");
        void readSession();
        return;
      }
      await finish();
    } catch (err) {
      const c = err instanceof ApiError ? err.code : "";
      if (c === "wrong_password") {
        setFailure({ kind: "wrong" });
        setPassword("");
        passwordRef.current?.focus();
      } else if (c === "user_suspended") {
        setFailure({ kind: "suspended" });
        setPassword("");
      } else if (c === "login_locked") {
        setFailure(lockFailure(err, "locked"));
      } else if (c === "not_set_up") {
        setFailure({ kind: "notSetUp" });
      } else {
        setFailure({ kind: "other", message: errorMessage(err, lang) });
      }
    } finally {
      setBusy(false);
    }
  };

  const ready = mode === "totp" ? totpComplete(code) : recoveryComplete(code);

  const submitCode = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || !ready) return;
    setBusy(true);
    setFailure(null);
    try {
      const res = await loginCode.mutateAsync(mode === "totp" ? cleanTotp(code) : cleanRecovery(code));
      setCode("");
      if (res && (res.recovery_left !== undefined || (res.wrong_codes ?? 0) > 0)) {
        setHeld({ left: res.recovery_left, wrong: res.wrong_codes ?? 0 });
        return;
      }
      await finish();
    } catch (err) {
      const c = err instanceof ApiError ? err.code : "";
      if (c === "mfa_code_wrong" || c === "mfa_code_used") {
        setFailure({ kind: c === "mfa_code_wrong" ? "codeWrong" : "codeUsed" });
        setCode("");
        codeRef.current?.focus();
      } else if (c === "mfa_challenge_invalid") {
        // Phiếu hết hạn, sai IP hoặc đã dùng hết lượt nhập: phải nhập lại mật khẩu.
        setFailure({ kind: "challenge" });
        setCode("");
        setStage("password");
        void readSession();
      } else if (c === "mfa_locked") {
        setFailure(lockFailure(err, "mfaLocked"));
        setCode("");
      } else if (c === "login_locked") {
        setFailure(lockFailure(err, "locked"));
        setCode("");
      } else if (c === "mfa_secret_unavailable") {
        setFailure({ kind: "secret" });
        setCode("");
      } else {
        setFailure({ kind: "other", message: errorMessage(err, lang) });
      }
    } finally {
      setBusy(false);
    }
  };

  const back = async () => {
    if (busy) return;
    setBusy(true);
    setFailure(null);
    try {
      // Xoá phiếu chờ ở máy chủ rồi về bước một.
      await post<void>("/api/v1/logout");
      setCode("");
      setStage("password");
      await readSession();
    } catch {
      setFailure({ kind: "backFailed" });
    } finally {
      setBusy(false);
    }
  };

  const proceed = async (to: string | null) => {
    await readSession();
    setAfter(to);
    setHeld(null);
  };

  // ---- Vừa vào xong, còn điều cần đọc ----
  if (held !== null) {
    const wrong = held.wrong ?? 0;
    const low = held.left !== undefined && recoveryLowNow(held.left);
    const warn = wrong > 0 || low;
    return (
      <BareFrame>
        <Card>
          <ScreenHeader title={t.enteredTitle} />
          <div className="mt-[16px] flex flex-col gap-[16px]">
            {held.left !== undefined && (
              <Notice kind={low ? "warn" : "pass"} role="status">
                <p className="m-0">{t.recoveryUsed(held.left)}</p>
                {low && <p className="mt-[4px] mb-0">{t.recoveryRemind(held.left)}</p>}
              </Notice>
            )}
            {wrong > 0 && (
              <Notice kind="warn" role="status">
                {t.wrongCodes(wrong)}
              </Notice>
            )}
            <div className="flex flex-wrap gap-[8px]">
              {wrong > 0 && (
                <Button variant="primary" onClick={() => void proceed("/settings#mat-khau-tieu-de")}>
                  {t.changePassword}
                </Button>
              )}
              {low && (
                <Button variant={wrong > 0 ? "secondary" : "primary"} onClick={() => void proceed("/account#hai-buoc")}>
                  {t.recoveryRegen}
                </Button>
              )}
              <Button variant={warn ? "secondary" : "primary"} onClick={() => void proceed(null)}>
                {t.recoveryGo}
              </Button>
            </div>
          </div>
        </Card>
      </BareFrame>
    );
  }

  return (
    <BareFrame>
      {/* Một thẻ giữa trang (mục 3.7): quên mật khẩu và chế độ demo nằm ở chân thẻ. */}
      <Card
        footer={
          stage === "password" ? (
            <div className="flex flex-col gap-[6px]">
              <p className="m-0">
                <span className="font-medium text-ink">{t.forgotTitle}.</span> {t.forgotBody}
              </p>
              <div>
                <MonoText bold>{resetCommand}</MonoText>
              </div>
              <p className="m-0">{t.forgotUser}</p>
              {session.data?.demo && (
                <p className="m-0 border-t border-hair pt-[8px]">
                  <span className="font-medium text-ink">{t.demoTitle}.</span> {t.demoBody}
                </p>
              )}
            </div>
          ) : (
            <div className="flex flex-col gap-[6px]">
              <p className="m-0">
                <span className="font-medium text-ink">{t.lostTitle}.</span> {t.lostBody}
              </p>
              <div>
                <MonoText bold>{reset2fa(typedName ?? "<tên>")}</MonoText>
              </div>
            </div>
          )
        }
      >
        <ScreenHeader title={stage === "password" ? t.title : t.codeTitle} summary={stage === "password" ? t.summary : t.codeSummary} />

        {stage === "password" ? (
          <form onSubmit={submit} className="mt-[16px] flex flex-col gap-[16px]">
            <TextField
              id="ten-dang-nhap"
              type="text"
              label={t.nameLabel}
              hint={t.nameHint}
              autoComplete="username"
              autoCapitalize="none"
              spellCheck={false}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <TextField
              ref={passwordRef}
              id="mat-khau"
              type="password"
              label={t.passwordLabel}
              autoComplete="current-password"
              // Giữ thói quen cũ: mở trang là gõ mật khẩu luôn (tên để trống là admin).
              autoFocus
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              invalid={failure?.kind === "wrong"}
            />
            <Button type="submit" variant="primary" disabled={busy || password === ""} className="w-full">
              {busy ? t.submitting : t.submit}
            </Button>
            {failure && <FailureNote failure={failure} t={t} lang={lang} lockedAt={lockedAt} failLabel={k.status.fail} user={typedName} />}
          </form>
        ) : (
          <form onSubmit={submitCode} className="mt-[16px] flex flex-col gap-[16px]">
            {/* key đổi theo kiểu mã: ô dựng lại và tự lấy focus, thay vì giữ chữ của kiểu kia. */}
            <TextField
              key={mode}
              ref={codeRef}
              id="ma-hai-buoc"
              type="text"
              label={mode === "totp" ? t.codeLabel : t.recoveryLabel}
              hint={mode === "totp" ? t.codeHint : t.recoveryHint}
              inputMode={mode === "totp" ? "numeric" : "text"}
              autoComplete={mode === "totp" ? "one-time-code" : "off"}
              autoCapitalize="none"
              spellCheck={false}
              mono
              autoFocus
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
              invalid={failure?.kind === "codeWrong" || failure?.kind === "codeUsed"}
            />
            <Button type="submit" variant="primary" disabled={busy || !ready} className="w-full">
              {busy ? t.codeSubmitting : t.codeSubmit}
            </Button>
            {failure && <FailureNote failure={failure} t={t} lang={lang} lockedAt={lockedAt} failLabel={k.status.fail} user={typedName} />}
            <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[8px]">
              <button
                type="button"
                onClick={() => {
                  setMode(mode === "totp" ? "recovery" : "totp");
                  setCode("");
                  setFailure(null);
                }}
                className="min-h-[44px] bg-transparent p-0 text-left text-accent underline hover:text-accent-hover sm:min-h-[24px]"
              >
                {mode === "totp" ? t.useRecovery : t.useTotp}
              </button>
              <button type="button" onClick={() => void back()} disabled={busy} className={buttonClass("secondary")}>
                {busy ? t.backing : t.back}
              </button>
            </div>
          </form>
        )}
      </Card>
    </BareFrame>
  );
}

/** Thông báo lỗi: dấu lỗi (hình, không chỉ màu) rồi chữ thường, theo mục 2 và 9 của GIAO-DIEN.md; chữ "lỗi" ẩn cho trình đọc màn hình. */
function FailureNote({ failure, t, lang, lockedAt, failLabel, user }: { failure: Failure; t: (typeof strings)["vi"]; lang: Lang; lockedAt: number; failLabel: string; user: string | null }) {
  let body: string;
  switch (failure.kind) {
    case "wrong":
      body = t.wrongPassword;
      break;
    case "suspended":
      body = t.suspended;
      break;
    case "locked":
      body = failure.retryAfter === null ? failure.message : lockedText(t, lang, lockedAt, failure.retryAfter);
      break;
    case "mfaLocked":
      body = failure.retryAfter === null ? failure.message : mfaLockedText(t, lang, lockedAt, failure.retryAfter);
      break;
    case "notSetUp":
      body = t.notSetUp;
      break;
    case "noSession":
      body = t.noSession;
      break;
    case "codeWrong":
      body = t.codeWrong;
      break;
    case "codeUsed":
      body = t.codeUsed;
      break;
    case "challenge":
      body = t.challengeInvalid;
      break;
    case "secret":
      body = t.secretUnavailable;
      break;
    case "backFailed":
      body = t.backFailed;
      break;
    case "other":
      body = failure.message;
      break;
  }
  return (
    <Notice kind="fail" role="alert">
      <p className="m-0">
        <span className="sr-only">{failLabel}: </span>
        {body}
      </p>
      {failure.kind === "notSetUp" && (
        <p className="mt-[6px] mb-0">
          <MonoText bold>{setupCommand}</MonoText>
        </p>
      )}
      {failure.kind === "secret" && (
        <p className="mt-[6px] mb-0">
          <MonoText bold>{reset2fa(user ?? "<tên>")}</MonoText>
        </p>
      )}
    </Notice>
  );
}

function clock(lang: Lang, from: number, retryAfter: number): string {
  return new Date(from + retryAfter * 1000).toLocaleTimeString(lang === "vi" ? "vi-VN" : "en-GB", { hour: "2-digit", minute: "2-digit" });
}

function lockedText(t: (typeof strings)["vi"], lang: Lang, from: number, retryAfter: number): string {
  return t.locked(clock(lang, from, retryAfter), Math.max(1, Math.ceil(retryAfter / 60)));
}

function mfaLockedText(t: (typeof strings)["vi"], lang: Lang, from: number, retryAfter: number): string {
  return t.mfaLocked(clock(lang, from, retryAfter), t.waitText(Math.max(1, Math.ceil(retryAfter / 60))));
}
