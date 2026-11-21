// Mật khẩu tpan vừa sinh cho một user (tạo user, hoặc đổi mật khẩu), hiện MỘT
// lần: cùng hình thức với khối mật khẩu WordPress ở trang job (lich-su/SecretBox.tsx)
// và khối mật khẩu DB (components/DbSecretBox.tsx): một thẻ bản 2 có nút chép
// từng dòng. Mật khẩu chỉ nằm trong state của màn, không vào cache của TanStack
// Query; khối chỉ biến mất khi người dùng tự bấm "Đã lưu, ẩn đi", vì mất mật
// khẩu này là phải đặt lại.

import { useEffect, useId, useRef } from "react";
import { Button } from "../../components/Button";
import { cardClass } from "../../components/Card";
import { Code } from "../../components/MonoText";
import { useCopy } from "../../components/useCopy";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/nguoi-dung";

export type Secret = { name: string; password: string };

export function PasswordBox({ secret, onDismiss }: { secret: Secret; onDismiss: () => void }) {
  const t = useStrings(strings);
  const ref = useRef<HTMLElement>(null);
  const headingId = useId();
  // Mật khẩu về khi người dùng đang ở cuối trang: đưa khối vào tầm mắt và cho trình đọc màn hình nhảy tới.
  useEffect(() => {
    ref.current?.scrollIntoView({ block: "start" });
    ref.current?.focus({ preventScroll: true });
  }, [secret]);
  return (
    <section ref={ref} tabIndex={-1} aria-labelledby={headingId} className={`${cardClass} p-[16px] outline-offset-4`}>
      <div className="text-[12px] font-medium text-muted">{t.secretLabel}</div>
      <h2 id={headingId} className="m-0 mt-[4px] text-[16px] font-semibold [overflow-wrap:anywhere]">
        {t.secretTitle(secret.name)}
      </h2>
      <p className="mt-[6px] mb-0">{t.secretBody}</p>
      <dl className="m-0 mt-[14px]">
        <Row label={t.secretUser} value={secret.name} />
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
      <dt className="flex-[0_0_120px] text-muted">{label}</dt>
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
