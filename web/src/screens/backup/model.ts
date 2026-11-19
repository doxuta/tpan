// Phần không dính giao diện của màn Backup: khoá truy vấn, đường dẫn API
// (khớp internal/server/types_m9.go), kiểm đầu vào cài đặt, đổi cỡ file ra chữ.
//
// Khoá truy vấn khai báo ở đây chứ không thêm vào api/queries.ts (file đó của
// phiên chính). Sau job, màn tự invalidate cả backupsKey lẫn overview.

import type { Lang } from "../../i18n";

export const backupsKey = ["backups"] as const;

export const backupsPath = "/api/v1/backups";
export const backupSettingsPath = "/api/v1/backups/settings";
export const siteBackupPath = (domain: string) => `/api/v1/sites/${encodeURIComponent(domain)}/backups`;
export const restorePath = (id: string) => `/api/v1/backups/${encodeURIComponent(id)}/restore`;

/** Trần số bản giữ: khớp core.MaxBackupKeep, để máy chủ không phải từ chối một lỗi gõ. */
export const maxKeep = 30;

/** Số bản giữ hợp lệ: số nguyên 1..30. Chuỗi rỗng, "0", "2.5" thì null. */
export function parseKeep(raw: string): number | null {
  const s = raw.trim();
  if (!/^\d{1,2}$/.test(s)) return null;
  const n = Number(s);
  return n >= 1 && n <= maxKeep ? n : null;
}

/** "12 MB", "1,4 GB" (dấu thập phân theo ngôn ngữ), cơ số 1024 như `df -h` và `du -h` mà người vận hành quen. */
export function formatSize(bytes: number, lang: Lang): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const f = new Intl.NumberFormat(lang === "vi" ? "vi-VN" : "en-GB", { maximumFractionDigits: i === 0 ? 0 : 1 });
  return `${f.format(n)} ${units[i]}`;
}

/** Mười hai ký tự đầu của sha256: đủ để người so bằng mắt, bấm vào mới chép cả chuỗi. */
export function shortSha(sha: string): string {
  return sha.slice(0, 12);
}

/** Tình trạng site của một dòng backup, tính từ overview (site đang chạy, trong thùng rác, hay không còn). */
export type SiteState = "active" | "trashed" | "gone";
