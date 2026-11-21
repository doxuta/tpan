// Trạng thái trống (mục 6 của docs/GIAO-DIEN.md, bản 2): một câu --muted theo
// giọng chữ ("Chưa có site nào."), và tuỳ chọn một liên kết hoặc nút để làm
// việc đầu tiên.
//
//   <EmptyState action={<Link to="/sites/new">Tạo site đầu tiên</Link>}>Chưa có site nào.</EmptyState>

import type { ReactNode } from "react";

export function EmptyState({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <p className="m-0 flex flex-wrap items-center gap-x-[8px] gap-y-[4px] text-muted">
      <span>{children}</span>
      {action}
    </p>
  );
}
