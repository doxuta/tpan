// Một dòng báo kết quả (bản 2): dấu trạng thái rồi chữ, vẽ bằng Notice của
// components/. Giữ chữ ký cũ của bản 1 (kind, children) vì nhiều màn import:
// lỗi đọc ngay (role="alert"), thành công đọc lịch sự (role="status").

import type { ReactNode } from "react";
import { Notice } from "../../components/Notice";

export function Note({ kind, children }: { kind: "pass" | "fail"; children: ReactNode }) {
  return (
    <Notice kind={kind} role={kind === "fail" ? "alert" : "status"}>
      {children}
    </Notice>
  );
}
