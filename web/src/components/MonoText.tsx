// Chữ mono bấm là chép: domain, đường dẫn, lệnh, mã job (mục 4 và mục 10 của
// KE-HOACH.md). Là <button> thật nên dùng được bằng bàn phím.
//
//   <MonoText>{site.domain}</MonoText>
//   <MonoText size={12} muted>{job.id}</MonoText>

import { useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { useCopy } from "./useCopy";

type Props = {
  children: string;
  /** 12 hoặc 13 px (mục 4); mặc định 13. */
  size?: 12 | 13;
  muted?: boolean;
  bold?: boolean;
};

export function MonoText({ children, size = 13, muted = false, bold = false }: Props) {
  const t = useStrings(strings);
  const { state, copy } = useCopy();
  return (
    <span className="inline-flex flex-wrap items-baseline gap-x-[8px]">
      <button
        type="button"
        title={t.copy}
        onClick={() => copy(children)}
        className={
          `select-text break-all text-left font-mono ${size === 12 ? "text-[12px]" : "text-[13px]"} ` +
          `${muted ? "text-muted" : "text-ink"} ${bold ? "font-semibold" : ""} hover:underline`
        }
      >
        {children}
      </button>
      <span aria-live="polite" className={`text-[12px] ${state === "idle" ? "sr-only" : "text-muted"}`}>
        {state === "copied" ? t.copied : state === "failed" ? t.copyFailed : ""}
      </span>
    </span>
  );
}
