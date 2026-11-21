// Tài khoản quản trị WordPress, hiện MỘT lần (JobDetail.secret, hoặc một phần
// tử của JobDetail.secrets với job hàng loạt: mỗi site một khối). Bản 2: cùng
// hình thức với components/DbSecretBox.tsx và screens/nguoi-dung/PasswordBox.tsx
// (một thẻ, nút chép từng dòng). Mật khẩu không vào localStorage hay cache của
// TanStack Query (xem useJob.ts). Mất mật khẩu này là mất tài khoản quản trị,
// nên khối chỉ biến mất khi người dùng tự bấm "Đã lưu, ẩn đi".

import { useEffect, useId, useRef } from "react";
import type { AdminSecret } from "../../api/types";
import { Button } from "../../components/Button";
import { cardClass } from "../../components/Card";
import { Code } from "../../components/MonoText";
import { useCopy } from "../../components/useCopy";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/lich-su";

type Props = {
  secret: AdminSecret;
  onDismiss: () => void;
  /** Job hàng loạt có nhiều khối: tiêu đề nói tài khoản của site nào. */
  many?: boolean;
  /** Đưa khối này vào tầm mắt và cho trình đọc màn hình nhảy tới; chỉ khối đầu làm việc đó. */
  focus?: boolean;
};

/** Phần máy chủ của địa chỉ để làm tiêu đề; địa chỉ lạ thì dùng nguyên chuỗi. */
function hostOf(url: string): string {
  try {
    return new URL(url).host || url;
  } catch {
    return url;
  }
}

export function SecretBox({ secret, onDismiss, many = false, focus = true }: Props) {
  const t = useStrings(strings);
  const ref = useRef<HTMLElement>(null);
  const headingId = useId();
  // Mật khẩu về khi job vừa xong, lúc người dùng đang ở cuối log: đưa khối này
  // vào tầm mắt và cho trình đọc màn hình nhảy tới, kẻo họ không thấy mà rời trang.
  useEffect(() => {
    if (!focus) return;
    ref.current?.scrollIntoView({ block: "start" });
    ref.current?.focus({ preventScroll: true });
  }, [focus]);
  return (
    <section ref={ref} tabIndex={-1} aria-labelledby={headingId} className={`${cardClass} p-[16px] outline-offset-4`}>
      <div className="text-[12px] font-medium text-muted">{t.secretLabel}</div>
      <h2 id={headingId} className="m-0 mt-[4px] text-[16px] font-semibold [overflow-wrap:anywhere]">
        {many ? t.secretTitleOf(hostOf(secret.url)) : t.secretTitle}
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
      <dd className="m-0 min-w-0 flex-[1_1_220px] select-text">
        <Code>{value}</Code>
      </dd>
      <Button size="sm" icon={state === "copied" ? "check" : "copy"} onClick={() => copy(value)} aria-label={t.secretCopyOf(label)}>
        {t.secretCopy}
      </Button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
      </span>
    </div>
  );
}
