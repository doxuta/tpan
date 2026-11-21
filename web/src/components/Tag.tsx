// Nhãn nhỏ (LEMP, nginx, PHP 8.3, "bạn", "tạm khoá"): bo tròn, không viền, chữ
// 12 px --ink-soft (mục 6 của docs/GIAO-DIEN.md, bản 2). Nền --paper khi đặt
// trên thẻ; tone="surface" khi đặt thẳng trên nền trang (để vẫn thấy được).

import type { ReactNode } from "react";

export function Tag({ children, title, tone = "paper" }: { children: ReactNode; title?: string; tone?: "paper" | "surface" }) {
  return (
    <span title={title} className={`inline-flex items-center rounded-full px-[8px] py-[1px] text-[12px] leading-[1.5] whitespace-nowrap text-ink-soft ${tone === "surface" ? "bg-surface" : "bg-paper"}`}>
      {children}
    </span>
  );
}
