// Màn Đăng nhập (mục 10 của KE-HOACH.md, màn 1). Không có khung trang (routes.tsx
// đánh dấu bare): logo, hai nút đoạn (ngôn ngữ, sáng/tối), một thẻ giữa trang
// trên nền --paper có tên đăng nhập, mật khẩu và nút Đăng nhập; phần "Quên mật
// khẩu" (và lời nhắc chế độ demo) là thẻ riêng bên dưới (bản 2).
//
// POST /api/v1/login trả 204 và đặt cookie. Sau đó đọc lại phiên (GET
// /api/v1/session) rồi về ?next= (đã lọc bằng safeNext) hoặc "/". Lỗi theo mã:
// wrong_password, login_locked (423, kèm Retry-After), not_set_up (503).

import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState, type FormEvent } from "react";
import { Navigate, useSearchParams } from "react-router";
import { ApiError, errorMessage, post, safeNext } from "../../api/client";
import { queryKeys, useSession } from "../../api/queries";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { TextField } from "../../components/Field";
import { Logo, Wordmark } from "../../components/Logo";
import { MonoText } from "../../components/MonoText";
import { Notice } from "../../components/Notice";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/dang-nhap";
import { strings as khung } from "../../i18n/khung";
import { useTheme, type Theme } from "../../theme";

const setupCommand = "sudo tpan setup";
const resetCommand = "sudo tpan admin passwd";

type Failure =
  | { kind: "wrong" }
  | { kind: "suspended" }
  | { kind: "locked"; retryAfter: number | null; message: string }
  | { kind: "notSetUp" }
  | { kind: "noSession" }
  | { kind: "other"; message: string };

export default function DangNhap() {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { lang, setLang } = useLang();
  const { theme, setTheme } = useTheme();
  const qc = useQueryClient();
  const session = useSession();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));

  // Để trống tên là "admin" (máy một người dùng trước M13 chỉ có tài khoản này).
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  // Giờ khoá tính một lần lúc nhận lỗi, để chữ không nhảy mỗi lần vẽ lại.
  const [lockedAt, setLockedAt] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  // Đã đăng nhập rồi (mở /dang-nhap bằng tay, hoặc vừa đăng nhập xong): đi tiếp.
  if (session.data?.logged_in) return <Navigate to={next} replace />;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setFailure(null);
    try {
      await post<void>("/api/v1/login", { name: name.trim(), password });
      // Đọc lại phiên: nếu logged_in thì dòng Navigate ở trên tự chạy.
      await qc.invalidateQueries({ queryKey: queryKeys.session });
      const s = qc.getQueryData<{ logged_in: boolean }>(queryKeys.session);
      if (!s?.logged_in) setFailure({ kind: "noSession" });
    } catch (err) {
      const code = err instanceof ApiError ? err.code : "";
      if (code === "wrong_password") {
        setFailure({ kind: "wrong" });
        setPassword("");
        input.current?.focus();
      } else if (code === "user_suspended") {
        setFailure({ kind: "suspended" });
        setPassword("");
      } else if (code === "login_locked") {
        setLockedAt(Date.now());
        setFailure({ kind: "locked", retryAfter: err instanceof ApiError ? err.retryAfter : null, message: errorMessage(err, lang) });
      } else if (code === "not_set_up") {
        setFailure({ kind: "notSetUp" });
      } else {
        setFailure({ kind: "other", message: errorMessage(err, lang) });
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="mx-auto box-border flex min-h-screen w-full max-w-[460px] flex-col justify-center gap-[20px] px-[16px] py-[32px]">
      <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[12px]">
        <div className="flex items-center gap-[10px]">
          <Logo status={null} />
          <Wordmark />
        </div>
        <div className="flex flex-wrap gap-[10px]">
          <SegmentButton<Lang>
            label={k.langGroup}
            value={lang}
            onChange={setLang}
            options={[
              { value: "vi", label: "VI" },
              { value: "en", label: "EN" },
            ]}
          />
          <SegmentButton<Theme>
            label={k.themeGroup}
            value={theme}
            onChange={setTheme}
            options={[
              { value: "light", label: k.light },
              { value: "dark", label: k.dark },
            ]}
          />
        </div>
      </div>

      {/* Một thẻ giữa trang (mục 3.7): quên mật khẩu và chế độ demo nằm ở chân thẻ. */}
      <Card
        footer={
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
        }
      >
        <ScreenHeader title={t.title} summary={t.summary} />
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
            ref={input}
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
          {failure && <FailureNote failure={failure} t={t} lang={lang} lockedAt={lockedAt} failLabel={k.status.fail} />}
        </form>
      </Card>
    </main>
  );
}

/** Thông báo lỗi: dấu lỗi (hình, không chỉ màu) rồi chữ thường, theo mục 2 và 9 của GIAO-DIEN.md; chữ "lỗi" ẩn cho trình đọc màn hình. */
function FailureNote({ failure, t, lang, lockedAt, failLabel }: { failure: Failure; t: (typeof strings)["vi"]; lang: Lang; lockedAt: number; failLabel: string }) {
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
    case "notSetUp":
      body = t.notSetUp;
      break;
    case "noSession":
      body = t.noSession;
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
    </Notice>
  );
}

function lockedText(t: (typeof strings)["vi"], lang: Lang, from: number, retryAfter: number): string {
  const until = new Date(from + retryAfter * 1000);
  const time = until.toLocaleTimeString(lang === "vi" ? "vi-VN" : "en-GB", { hour: "2-digit", minute: "2-digit" });
  return t.locked(time, Math.max(1, Math.ceil(retryAfter / 60)));
}
