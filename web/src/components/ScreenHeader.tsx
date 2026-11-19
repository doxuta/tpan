// Đầu mỗi màn: tên màn (h1) và một dòng tóm tắt (mục 7). Số đo lấy từ trang
// mẫu: h1 30 px, 600, giãn chữ −0,015em; dòng tóm tắt 14 px màu --muted.

import type { ReactNode } from "react";

export function ScreenHeader({ title, summary }: { title: ReactNode; summary?: ReactNode }) {
  return (
    <div className="pt-[24px]">
      <h1 className="m-0 text-[30px] leading-[1.15] font-semibold tracking-[-0.015em]">{title}</h1>
      {summary && <p className="mt-[6px] mb-0 text-[14px] text-muted">{summary}</p>}
    </div>
  );
}
