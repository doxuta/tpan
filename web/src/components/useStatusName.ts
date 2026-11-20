// Tên của một trạng thái để đặt vào nhãn đọc màn hình và chữ cạnh dấu.
//
// Site hay DB đang tạm khoá vẽ bằng đúng hình "chưa kiểm được" (vòng rỗng xám,
// vẫn bốn hình như mục 2 của docs/GIAO-DIEN.md) nhưng nhãn phải là "tạm khoá":
// gọi nó là "chưa kiểm được" thì người xem tưởng máy có chuyện. Cờ suspended
// chỉ có nghĩa khi trạng thái là "unknown" (CheckResult.suspended).
//
//   const name = useStatusName();
//   <StatusMark status={c.status} label={k.statusLabel(title, name(c.status, c.suspended))} />

import type { Status } from "../api/types";
import { useStrings } from "../i18n";
import { strings as khung } from "../i18n/khung";
import { strings as chung } from "../i18n/m13-chung";

export function useStatusName(): (status: Status, suspended?: boolean) => string {
  const k = useStrings(khung);
  const m = useStrings(chung);
  return (status, suspended = false) => (suspended && status === "unknown" ? m.suspendedState : k.status[status]);
}
