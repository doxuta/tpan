// Logo TPAN (mục 11 của docs/GIAO-DIEN.md): tuyến chữ T có ba ga. Ga cuối là
// dấu trạng thái của cả máy, dùng đúng hình ở mục 2. Vẽ lại từ docs/logo/ bằng
// biến màu để đổi theo sáng/tối; ruột hai ga trên cùng màu nền thanh đầu trang
// (--surface, bản 2).
//
// Logo cũng đổi biểu tượng tab: đặt href của <link rel="icon"> sang một trong
// các file SVG tĩnh ở web/public/ (CSP chặn địa chỉ data:), nên tab nằm nền
// vẫn cho biết máy có chuyện.

import { useEffect } from "react";
import type { Status } from "../api/types";

const favicon: Record<Status | "none", string> = {
  pass: "/tpan-dat.svg",
  warn: "/tpan-canh-bao.svg",
  fail: "/tpan-loi.svg",
  unknown: "/tpan.svg",
  none: "/tpan.svg",
};

/** Chữ TPAN cạnh logo (mục 11 của GIAO-DIEN.md): mono 18 px đậm 600, giãn chữ 0,04em. Thanh đầu trang và trang Đăng nhập dùng chung. */
export function Wordmark() {
  return <span className="font-mono text-[18px] leading-none font-semibold tracking-[0.04em]">TPAN</span>;
}

/** status null hoặc "unknown": bản một màu, vì chưa có bằng chứng để nói máy đạt hay hỏng. */
export function Logo({ status, size = 32 }: { status: Status | null; size?: number }) {
  const key = status ?? "none";

  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (link) link.href = favicon[key];
  }, [key]);

  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 32 32" fill="none" className="flex-none">
      <path d="M6 9H26M16 9V21" className="stroke-ink" strokeWidth="4" />
      <circle cx="6" cy="9" r="3.5" className="fill-surface stroke-ink" strokeWidth="2" />
      <circle cx="26" cy="9" r="3.5" className="fill-surface stroke-ink" strokeWidth="2" />
      {key === "pass" && <circle cx="16" cy="25" r="5" className="fill-ok" />}
      {key === "warn" && <rect x="12" y="21" width="8" height="8" transform="rotate(45 16 25)" className="fill-warn stroke-warn-border" strokeWidth="2" />}
      {key === "fail" && <rect x="11" y="20" width="10" height="10" className="fill-fail" />}
      {(key === "unknown" || key === "none") && <circle cx="16" cy="25" r="5" className="fill-ink" />}
    </svg>
  );
}
