// Chân bảng của màn Cơ sở dữ liệu (mục 3.7 và 3.8 của M16-thiet-ke.md). Hai hàm
// thuần, chỉ `import type`, để đọc và thử riêng được.
//
// N là số hàng đang chạy (`status === "active"`), cùng định nghĩa với số ở menu
// và ở trang Công cụ. Hàng trong thùng rác (DB của site đã vào thùng rác) vẫn
// nằm trong bảng nhưng không tính vào N; chân bảng nói riêng.

import type { DatabaseRow } from "../../api/types";
import type { strings } from "../../i18n/co-so-du-lieu";

export type DbCounts = {
  /** Số hàng đang chạy (N). */
  active: number;
  /** Trong N: DB của site. */
  site: number;
  /** Trong N: DB rời. */
  standalone: number;
  /** Số hàng trong thùng rác. */
  trashed: number;
};

export function footerCounts(rows: Pick<DatabaseRow, "kind" | "status">[]): DbCounts {
  const running = rows.filter((r) => r.status === "active");
  const site = running.filter((r) => r.kind === "site").length;
  return { active: running.length, site, standalone: running.length - site, trashed: rows.length - running.length };
}

type T = (typeof strings)["vi"];

/** "5 cơ sở dữ liệu, đều là DB của site", "3 cơ sở dữ liệu, 2 của site, 1 DB rời, 1 trong thùng rác", hoặc "Chưa có cơ sở dữ liệu nào đang chạy". */
export function footerText(t: Pick<T, "footerNone" | "footerRunning" | "footerAllSite" | "footerAllStandalone" | "footerSplit" | "footerTrashed">, c: DbCounts): string {
  const head = c.active === 0 ? t.footerNone : t.footerRunning(c.active);
  const split = c.active === 0 ? "" : c.standalone === 0 ? t.footerAllSite : c.site === 0 ? t.footerAllStandalone : t.footerSplit(c.site, c.standalone);
  return [head, split, c.trashed > 0 ? t.footerTrashed(c.trashed) : ""].filter(Boolean).join(", ");
}
