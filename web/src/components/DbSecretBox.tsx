// Mật khẩu DB rời vừa tạo hoặc vừa đổi, hiện MỘT lần (JobDetail.db_secret).
// Cùng hình thức khối SecretBox của tài khoản WordPress (viền 2 px --ink, có
// nút chép từng dòng, chỉ biến mất khi người dùng tự bấm "Đã lưu, ẩn đi").
//
// Chỗ gắn: trang job (screens/lich-su/JobDetail.tsx), cạnh SecretBox. Giữ
// db_secret trong một biến của module như useJob.ts làm với secret, không để nó
// vào cache của TanStack Query hay localStorage.

import { useEffect, useId, useRef } from "react";
import type { DBSecret } from "../api/types";
import { useStrings } from "../i18n";
import { strings } from "../i18n/co-so-du-lieu";
import { strings as khung } from "../i18n/khung";
import { Button } from "./Button";
import { useCopy } from "./useCopy";

type Props = {
  secret: DBSecret;
  onDismiss: () => void;
  /** Đưa khối vào tầm mắt và cho trình đọc màn hình nhảy tới. */
  focus?: boolean;
};

export function DbSecretBox({ secret, onDismiss, focus = true }: Props) {
  const t = useStrings(strings);
  const ref = useRef<HTMLElement>(null);
  const headingId = useId();
  // Mật khẩu về khi job vừa xong, lúc người dùng đang ở cuối log: không đưa vào tầm mắt thì họ rời trang mà chưa chép.
  useEffect(() => {
    if (!focus) return;
    ref.current?.scrollIntoView({ block: "start" });
    ref.current?.focus({ preventScroll: true });
  }, [focus]);
  return (
    <section ref={ref} tabIndex={-1} aria-labelledby={headingId} className="mt-[26px] border-2 border-ink bg-surface p-[18px] outline-offset-4">
      <div className="font-mono text-[11px] tracking-[0.08em] text-muted">{t.secretLabel}</div>
      <h2 id={headingId} className="m-0 mt-[4px] text-[16px] font-semibold [overflow-wrap:anywhere]">
        {t.secretTitle(secret.db)}
      </h2>
      <p className="mt-[6px] mb-0">{t.secretBody}</p>
      <dl className="m-0 mt-[14px]">
        <Row label={t.secretDb} value={secret.db} />
        <Row label={t.secretUser} value={secret.user} />
        <Row label={t.secretPassword} value={secret.password} />
      </dl>
      <div className="mt-[14px]">
        <Button variant="primary" onClick={onDismiss}>
          {t.secretDismiss}
        </Button>
      </div>
    </section>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const { state, copy } = useCopy();
  return (
    <div className="flex flex-wrap items-center gap-x-[12px] gap-y-[6px] border-t border-hair py-[8px]">
      <dt className="flex-[0_0_88px] text-muted">{label}</dt>
      <dd className="m-0 min-w-0 flex-[1_1_220px] select-text font-mono text-[13px] break-all">{value}</dd>
      <Button onClick={() => copy(value)} aria-label={t.secretCopyOf(label)}>
        {t.secretCopy}
      </Button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
      </span>
    </div>
  );
}
