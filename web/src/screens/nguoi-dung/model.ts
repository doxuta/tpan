// Định dạng chung của màn Người dùng, màn Gói và khối "Hạn mức gói" trên Bảng
// điều khiển. Giới hạn 0 của gói là "không giới hạn" (mục 6.12).

import type { Package, Usage } from "../../api/types";
import type { Lang } from "../../i18n";
import { fmtBytes } from "../bang-dieu-khien/model";

const MiB = 2 ** 20;

/** Giới hạn dung lượng của gói (MiB) thành chữ; 0 thì trả null để bên gọi viết "không giới hạn". */
export function diskLimitText(mb: number, lang: Lang): string | null {
  return mb > 0 ? fmtBytes(mb * MiB, lang) : null;
}

/** Dung lượng đã dùng thành chữ; null khi chưa đo lần nào (disk_bytes = -1). */
export function diskUsedText(use: Usage, lang: Lang): string | null {
  return use.disk_bytes >= 0 ? fmtBytes(use.disk_bytes, lang) : null;
}

/** Phần trăm đã dùng so với giới hạn; null khi không giới hạn (0) hoặc chưa đo. */
export function percentOf(used: number, limit: number): number | null {
  return limit > 0 && used >= 0 ? (used / limit) * 100 : null;
}

export function diskPercent(use: Usage, pkg: Package): number | null {
  return percentOf(use.disk_bytes, pkg.disk_mb * MiB);
}
