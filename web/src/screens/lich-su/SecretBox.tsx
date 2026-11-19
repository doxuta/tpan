// Tài khoản quản trị WordPress, hiện MỘT lần (JobDetail.secret). Nổi rõ bằng
// viền 2 px màu --ink; mật khẩu không vào localStorage hay cache của TanStack
// Query (xem useJob.ts). Mất mật khẩu này là mất tài khoản quản trị, nên khối
// chỉ biến mất khi người dùng tự bấm "Đã lưu, ẩn đi".

import { useEffect, useRef } from "react";
import type { AdminSecret } from "../../api/types";
import { Button } from "../../components/Button";
import { useCopy } from "../../components/useCopy";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/lich-su";

export function SecretBox({ secret, onDismiss }: { secret: AdminSecret; onDismiss: () => void }) {
  const t = useStrings(strings);
  const ref = useRef<HTMLElement>(null);
  // Mật khẩu về khi job vừa xong, lúc người dùng đang ở cuối log: đưa khối này
  // vào tầm mắt và cho trình đọc màn hình nhảy tới, kẻo họ không thấy mà rời trang.
  useEffect(() => {
    ref.current?.scrollIntoView({ block: "start" });
    ref.current?.focus({ preventScroll: true });
  }, []);
  return (
    <section ref={ref} tabIndex={-1} aria-labelledby="secret-heading" className="mt-[26px] border-2 border-ink bg-surface p-[18px] outline-offset-4">
      <div className="font-mono text-[11px] tracking-[0.08em] text-muted">{t.secretLabel}</div>
      <h2 id="secret-heading" className="m-0 mt-[4px] text-[16px] font-semibold">
        {t.secretTitle}
      </h2>
      <p className="mt-[6px] mb-0">{t.secretBody}</p>
      <dl className="m-0 mt-[14px]">
        <SecretRow label={t.secretUrl} value={secret.url} />
        <SecretRow label={t.secretUser} value={secret.user} />
        <SecretRow label={t.secretPassword} value={secret.password} />
      </dl>
      <div className="mt-[14px]">
        <Button variant="primary" onClick={onDismiss}>
          {t.secretDismiss}
        </Button>
      </div>
    </section>
  );
}

function SecretRow({ label, value }: { label: string; value: string }) {
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
