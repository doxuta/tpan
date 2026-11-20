// Màn Đăng nhập (mục 10 của KE-HOACH.md, màn 1). Không có khung trang (routes.tsx
// đánh dấu bare): chỉ logo, tên màn, một ô mật khẩu và nút Đăng nhập.
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
import { Logo } from "../../components/Logo";
import { MonoText } from "../../components/MonoText";
import { ScreenHeader } from "../../components/ScreenHeader";
import { SegmentButton } from "../../components/SegmentButton";
import { StatusMark } from "../../components/StatusMark";
import { useLang, useStrings, type Lang } from "../../i18n";
import { strings } from "../../i18n/dang-nhap";
import { strings as khung } from "../../i18n/khung";
import { useTheme, type Theme } from "../../theme";
import { Field } from "./Field";

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
    <main className="mx-auto box-border w-full max-w-[460px] px-[16px] pt-[32px] pb-[48px]">
      <div className="flex flex-wrap items-center justify-between gap-x-[16px] gap-y-[12px]">
        <div className="flex items-center gap-[10px]">
          <Logo status={null} />
          <span className="font-mono text-[22px] leading-none font-semibold tracking-[0.04em]">TPAN</span>
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

      <ScreenHeader title={t.title} summary={t.summary} />

      <form onSubmit={submit} className="mt-[20px]">
        <Card>
          <Field
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
          <div className="mt-[16px]" />
          <Field
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
          <div className="mt-[16px]">
            <Button type="submit" variant="primary" disabled={busy || password === ""}>
              {busy ? t.submitting : t.submit}
            </Button>
          </div>
          {failure && <FailureNote failure={failure} t={t} lang={lang} lockedAt={lockedAt} failLabel={k.status.fail} />}
        </Card>
      </form>

      <section className="pt-[24px]">
        <h2 className="m-0 text-[16px] font-semibold">{t.forgotTitle}</h2>
        <p className="mt-[6px] mb-[6px] text-[14px] text-muted">{t.forgotBody}</p>
        <MonoText bold>{resetCommand}</MonoText>
        <p className="mt-[10px] mb-0 text-[14px] text-muted">{t.forgotUser}</p>
      </section>

      {session.data?.demo && (
        <section className="pt-[24px]">
          <h2 className="m-0 text-[16px] font-semibold">{t.demoTitle}</h2>
          <p className="mt-[6px] mb-0 text-[14px] text-muted">{t.demoBody}</p>
        </section>
      )}
    </main>
  );
}

/** Thông báo lỗi: dấu lỗi (hình, không chỉ màu) rồi chữ đen, theo mục 2 và 9 của GIAO-DIEN.md. */
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
    <div role="alert" className="mt-[16px] flex items-start gap-[10px] border-t border-hair pt-[14px]">
      <span className="mt-[1px] flex-none">
        <StatusMark status="fail" label={failLabel} />
      </span>
      <div className="min-w-0">
        <p className="m-0">{body}</p>
        {failure.kind === "notSetUp" && (
          <p className="mt-[6px] mb-0">
            <MonoText bold>{setupCommand}</MonoText>
          </p>
        )}
      </div>
    </div>
  );
}

function lockedText(t: (typeof strings)["vi"], lang: Lang, from: number, retryAfter: number): string {
  const until = new Date(from + retryAfter * 1000);
  const time = until.toLocaleTimeString(lang === "vi" ? "vi-VN" : "en-GB", { hour: "2-digit", minute: "2-digit" });
  return t.locked(time, Math.max(1, Math.ceil(retryAfter / 60)));
}
