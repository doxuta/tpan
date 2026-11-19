// Nhãn (LEMP, nginx 1.24, demo): chữ mono 12 px, viền --ink, đệm 2 px 8 px, không nền (mục 6).

import type { ReactNode } from "react";

export function Tag({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <span title={title} className="whitespace-nowrap border border-ink px-[8px] py-[2px] font-mono text-[12px] text-ink">
      {children}
    </span>
  );
}
