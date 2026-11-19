// Phần không dính giao diện của màn Cập nhật: khoá truy vấn và ba việc làm được
// từ màn. Cả ba theo một nhịp (xem trước bằng ?dry_run=1, rồi chạy thật), chỉ
// khác đường dẫn và thân, nên mô tả bằng một kiểu Action để index.tsx lo chung.

import type { ReactNode } from "react";
import type { MariaDBUpgradeBody } from "../../api/types";

export const updatesKey = ["updates"] as const;

export type Action = {
  /** Dùng làm id nút ("act-<id>") để trả focus về nút khi huỷ bản xem trước. */
  id: string;
  path: string;
  body?: MariaDBUpgradeBody;
};

export const packagesAction: Action = { id: "packages", path: "/api/v1/updates" };
// id "auto" trùng với RowAction.kind của CheckRow, nó đặt id nút là "act-auto".
export const autoAction: Action = { id: "auto", path: "/api/v1/updates/auto" };
export const mariadbAction = (line: string): Action => ({ id: `mariadb-${line}`, path: "/api/v1/mariadb/upgrade", body: { line } });

/** Những gì index.tsx đưa cho từng khối để khối bấm được nút và hiện được lỗi. */
export type Flow = {
  start: (a: Action) => void;
  /** Id việc đang dựng bản xem trước; null khi không có. */
  pending: string | null;
  /** Đang dựng bản xem trước hoặc đang gửi lệnh chạy: khoá mọi nút việc. */
  busy: boolean;
  /** Lỗi của lần xem trước vừa rồi nếu nó thuộc việc này (ErrorNote), không thì undefined. */
  errorFor: (a: Action) => ReactNode;
};
