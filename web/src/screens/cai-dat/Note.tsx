// Một dòng báo kết quả: dấu trạng thái (hình, không chỉ màu) rồi chữ đen. Lỗi
// đọc ngay (role="alert"); thành công đọc lịch sự (role="status"). Màu chỉ nằm
// ở dấu trạng thái, theo mục 2 của docs/GIAO-DIEN.md.

import type { ReactNode } from "react";
import { StatusMark } from "../../components/StatusMark";
import { useStrings } from "../../i18n";
import { strings as khung } from "../../i18n/khung";

export function Note({ kind, children }: { kind: "pass" | "fail"; children: ReactNode }) {
  const k = useStrings(khung);
  return (
    <div role={kind === "fail" ? "alert" : "status"} className="flex items-start gap-[10px]">
      <span className="mt-[1px] flex-none">
        <StatusMark status={kind} label={k.status[kind]} />
      </span>
      <p className="m-0 min-w-0">{children}</p>
    </div>
  );
}
