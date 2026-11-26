// M34: DB có trong TPAN nhưng không còn trong MariaDB (đã bị xoá ngoài TPAN).
// Phần thuần của màn Cơ sở dữ liệu, tách riêng để `make test-web` (node --test)
// nạp được: model.ts kéo theo các module của trình duyệt.

import type { DatabaseRow } from "../../api/types";

type TrashStrings = {
  trashDbWarning: (db: string) => string;
  trashErrorHeading: (db: string) => string;
  forgetDbWarning: (db: string) => string;
  forgetErrorHeading: (db: string) => string;
};

/**
 * Lời cảnh báo và tiêu đề lỗi của nút Xoá trên một DB rời. Cùng một đường DELETE /dbs/{name}:
 * DB còn thì máy chủ đưa nó vào thùng rác, DB đã mất thì máy chủ gỡ dòng của nó (không có gì để
 * vào thùng rác), nên lời hiện cho người dùng phải nói đúng việc sẽ xảy ra.
 */
export function trashWording(t: TrashStrings, r: Pick<DatabaseRow, "db" | "missing">): { warning: string; errorHeading: string } {
  if (r.missing) return { warning: t.forgetDbWarning(r.db), errorHeading: t.forgetErrorHeading(r.db) };
  return { warning: t.trashDbWarning(r.db), errorHeading: t.trashErrorHeading(r.db) };
}
