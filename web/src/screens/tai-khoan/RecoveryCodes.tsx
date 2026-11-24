// Mười mã dự phòng vừa tạo (bật hai bước, hoặc tạo lại), hiện MỘT lần: cùng hình
// thức với khối mật khẩu vừa tạo của màn Người dùng (một thẻ, chỉ biến mất khi
// người dùng tự bấm "Đã lưu mã"). Mã chỉ nằm trong props, từ state của màn gọi;
// không vào cache, localStorage hay console.
//
//   {codes && <RecoveryCodes codes={codes} onSaved={() => …}/>}
//
// Màn gọi giữ `codes` trong state của một component luôn có mặt (không dựng có
// điều kiện theo kết quả truy vấn), vì truy vấn tự đọc lại khi cửa sổ lấy lại
// focus: lúc đó khối này không được biến mất giữa chừng.

import { useEffect, useId, useRef } from "react";
import { useSession } from "../../api/queries";
import { Button } from "../../components/Button";
import { cardClass } from "../../components/Card";
import { Code } from "../../components/MonoText";
import { useCopy } from "../../components/useCopy";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";
import { strings } from "../../i18n/tai-khoan";
import { recoveryText } from "./model";

/** Tải một đoạn chữ về máy bằng Blob; không gọi ra mạng. */
function download(text: string, filename: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function RecoveryCodes({ codes, onSaved, busy = false }: { codes: string[]; onSaved: () => void; busy?: boolean }) {
  const t = useStrings(strings);
  const k = useStrings(khung);
  const host = useSession().data?.hostname ?? "";
  const { state, copy } = useCopy();
  const ref = useRef<HTMLElement>(null);
  const headingId = useId();
  // Mã về khi người dùng đang ở giữa trang: đưa khối vào tầm mắt và cho trình đọc màn hình nhảy tới.
  useEffect(() => {
    ref.current?.scrollIntoView({ block: "start" });
    ref.current?.focus({ preventScroll: true });
  }, []);
  const text = recoveryText(codes, host, new Date().toISOString().slice(0, 10));
  return (
    <section ref={ref} tabIndex={-1} aria-labelledby={headingId} className={`${cardClass} p-[16px] outline-offset-4`}>
      <div className="text-[12px] font-medium text-muted">{t.revealLabel}</div>
      <h2 id={headingId} className="m-0 mt-[4px] text-[16px] font-semibold">
        {t.codesTitle}
      </h2>
      <p className="mt-[6px] mb-0 max-w-[72ch]">{t.codesBody}</p>
      <ul aria-label={t.codesList(codes.length)} className="m-0 mt-[14px] grid list-none grid-cols-1 gap-x-[24px] gap-y-[6px] rounded-ctl bg-pre p-[12px] sm:grid-cols-2">
        {codes.map((c) => (
          <li key={c} className="select-text">
            <Code>{c}</Code>
          </li>
        ))}
      </ul>
      <div className="mt-[14px] flex flex-wrap items-center gap-[8px]">
        <Button size="sm" icon={state === "copied" ? "check" : "copy"} onClick={() => copy(codes.join("\n"))}>
          {t.codesCopyAll}
        </Button>
        <Button size="sm" onClick={() => download(text, t.codesFile)}>
          {t.codesDownload}
        </Button>
        <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
          {state === "copied" ? k.copied : state === "failed" ? k.copyFailed : ""}
        </span>
      </div>
      <div className="mt-[14px]">
        <Button variant="primary" onClick={onSaved} disabled={busy}>
          {t.codesSaved}
        </Button>
      </div>
    </section>
  );
}
