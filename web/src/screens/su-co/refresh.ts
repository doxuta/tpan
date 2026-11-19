// Sau khi đổi thứ gì liên quan tới sự cố (kiểm lại, nhận bản sửa tay, chạy
// sửa tự động): đọc lại sự cố, các job và overview (menu đếm sự cố, logo, bảng
// điều khiển đều đọc từ overview).

import type { QueryClient } from "@tanstack/react-query";
import { queryKeys } from "../../api/queries";

export function refreshIncidents(qc: QueryClient): Promise<unknown> {
  return Promise.all([
    qc.invalidateQueries({ queryKey: ["incidents"] }),
    qc.invalidateQueries({ queryKey: ["incident"] }),
    qc.invalidateQueries({ queryKey: ["jobs"] }),
    qc.invalidateQueries({ queryKey: queryKeys.overview }),
  ]);
}
