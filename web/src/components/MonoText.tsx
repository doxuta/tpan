// Chữ mono (mục 4 của docs/GIAO-DIEN.md, bản 2): chỉ cho thứ người ta chép hoặc
// so từng ký tự (đường dẫn, lệnh, tên file, giá trị cấu hình, tên DB và user hệ
// thống, mã job, sha, số phiên bản). Tên miền KHÔNG dùng mono.
//
//   <MonoText>{layout.wp_config}</MonoText>     bấm là chép (là <button> thật, dùng được bằng bàn phím)
//   <Code>{job.id.slice(0, 8)}</Code>           chỉ để đọc, nằm trong dòng
//   <Code block>{dòng mã}</Code>                khối nhiều dòng không phải bằng chứng

import type { ReactNode } from "react";
import { useStrings } from "../i18n";
import { strings } from "../i18n/khung";
import { useCopy } from "./useCopy";

type Props = {
  children: string;
  /** 13 (mặc định), 12,5 hoặc 12 px. */
  size?: 13 | 12.5 | 12;
  muted?: boolean;
  bold?: boolean;
};

const sizeClass = { 13: "text-[13px]", 12.5: "text-[12.5px]", 12: "text-[12px]" } as const;

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
          // Màn hẹp: vùng bấm tối thiểu 24 × 24 px (WCAG 2.2 AA, 2.5.8; GIAO-DIEN.md).
          `bg-transparent p-0 text-left font-mono break-all select-text max-sm:min-h-[24px] max-sm:min-w-[24px] ${sizeClass[size]} ` +
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

/**
 * Chữ mono chỉ để đọc. block: khối nền --pre (dòng mã trong ngăn AI, output
 * không phải bằng chứng). className chỉ cho class bố cục hay chip nền (ví dụ mã
 * HTTP trên nền --fail).
 */
export function Code({ children, block = false, className = "" }: { children: ReactNode; block?: boolean; className?: string }) {
  if (block) {
    return <code className={`block rounded-ctl bg-pre p-[12px] font-mono text-[12.5px] leading-[1.55] whitespace-pre-wrap text-ink [overflow-wrap:anywhere] ${className}`.trim()}>{children}</code>;
  }
  return <code className={`font-mono text-[13px] [overflow-wrap:anywhere] ${className}`.trim()}>{children}</code>;
}
