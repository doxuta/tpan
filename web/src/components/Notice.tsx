// Một dòng báo (mục 6 của docs/GIAO-DIEN.md, bản 2): dấu trạng thái rồi chữ.
// Dùng cho kết quả của một việc vừa làm ("Đã đổi mật khẩu.") và lời nhắc cần
// chú ý. Màu chỉ nằm ở dấu, chữ giữ màu chữ thường.
//
//   <Notice kind="pass">Đã lưu.</Notice>
//   <Notice kind="fail" role="alert">Không lưu được: …</Notice>

import type { ReactNode } from "react";
import { StatusMark } from "./StatusMark";

type Props = {
  kind: "pass" | "fail" | "warn";
  children: ReactNode;
  /** "alert" khi câu báo hiện ra sau một thao tác và cần được đọc ngay. */
  role?: "alert" | "status";
};

export function Notice({ kind, children, role }: Props) {
  return (
    <div role={role} className="flex items-start gap-[10px]">
      <span className="flex h-[20px] w-[20px] flex-none items-center justify-center">
        <StatusMark status={kind} />
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
