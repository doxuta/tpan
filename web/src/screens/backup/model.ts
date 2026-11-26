// Phần không dính giao diện của màn Backup: khoá truy vấn, đường dẫn API
// (khớp internal/server/types_m9.go), kiểm đầu vào cài đặt, đổi cỡ file ra chữ.
//
// Khoá truy vấn khai báo ở đây chứ không thêm vào api/queries.ts (file đó của
// phiên chính). Sau job, màn tự invalidate cả backupsKey lẫn overview.

import type { Backup } from "../../api/types";
import type { Lang } from "../../i18n";

export const backupsKey = ["backups"] as const;
/** Đích ngoài máy (GET /backups/target): chỉ admin đọc. Bắt đầu bằng backupsKey nên job xong thì tự tải lại cùng bảng. */
export const targetKey = [...backupsKey, "target"] as const;

export const backupsPath = "/api/v1/backups";
export const backupSettingsPath = "/api/v1/backups/settings";
export const siteBackupPath = (domain: string) => `/api/v1/sites/${encodeURIComponent(domain)}/backups`;
export const restorePath = (id: string) => `/api/v1/backups/${encodeURIComponent(id)}/restore`;
export const backupPath = (id: string) => `/api/v1/backups/${encodeURIComponent(id)}`;

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

// ---- DB rời (M33) ----

/** Tình trạng của DB rời mà một dòng backup thuộc về (đang có, trong thùng rác, đã xoá hẳn). */
export type DbState = "active" | "trashed" | "gone";

/** Các bản sao lưu của một DB rời: dòng của nó mang tên DB ở trường domain (GET /backups?db=<tên>). */
export const dbBackupsPath = (name: string) => `${backupsPath}?db=${encodeURIComponent(name)}`;
export const dbBackupsKey = (name: string) => [...backupsKey, "db", name] as const;

/**
 * Các bản .sql của đúng một DB (màn quản lý DB): `id` là tên miền (DB của site) hoặc tên DB (DB rời).
 * Lọc lại phòng máy chủ trả thừa: nhập bản của DB khác vào DB này là sai, và một dòng của site không
 * bao giờ được tính cho DB rời trùng chuỗi (hay ngược lại).
 */
export function ownSqlBackups(items: Backup[], id: string, standalone: boolean): Backup[] {
  return items.filter((b) => b.kind === "sql" && b.domain === id && Boolean(b.db) === standalone);
}

/**
 * Nút "khôi phục" của một hàng là việc gì (máy chủ quyết theo cùng luật ở POST /backups/{id}/restore):
 *   restore      site đang chạy, bản full: ghi đè site bằng bản đó
 *   recreate     site đã xoá hẳn, bản full: tạo lại site rồi khôi phục
 *   db-import    DB rời đang có: nhập bản sao lưu vào DB đó
 *   db-recreate  DB rời đã xoá hẳn: tạo lại DB (mật khẩu mới) rồi nhập
 *   null         không có nút: bản sql của site (nhập ở màn Cơ sở dữ liệu), site hay DB đang ở thùng rác
 */
export type RestoreKind = "restore" | "recreate" | "db-import" | "db-recreate" | null;
export function restoreKind(b: Backup, site: SiteState, db: DbState): RestoreKind {
  if (b.db) return db === "active" ? "db-import" : db === "gone" ? "db-recreate" : null;
  if (b.kind === "sql") return null;
  return site === "gone" ? "recreate" : site === "trashed" ? null : "restore";
}

// ---- Kho ngoài máy (M18) ----

/** Việc mở từ một hàng của bảng: để khoá nút và đổi chữ nút vừa bấm. */
export type RowWork = "restore" | "push" | "pull" | "delete";
export type PendingRow = { action: RowWork; id: string } | null;

/**
 * Cột "Ở đâu" chỉ hiện khi máy có dùng kho (B11): đã đặt đích, hoặc có ít nhất
 * một dòng không phải "chỉ trên máy". Máy không dùng kho thì bảng y như trước M18.
 * `configured` là undefined với user thường (họ không đọc được đích): chỉ dựa vào các dòng.
 */
export function showWhere(items: Backup[], configured: boolean | undefined): boolean {
  return configured === true || items.some((b) => b.location !== undefined && b.location !== "machine");
}

/**
 * Việc kho của một hàng, chỉ admin: tải về cho bản chỉ còn trên kho; đẩy lên kho
 * cho bản chỉ trên máy hay đẩy lỗi, loại full, và chỉ khi đã đặt đích (máy chủ
 * cũng kiểm lại, kể cả luật s3.keep). `location` vắng (setting đích hỏng) thì
 * không đoán: không có việc nào.
 */
export function storageWork(b: Backup, isAdmin: boolean, configured: boolean | undefined): "push" | "pull" | null {
  if (!isAdmin) return null;
  if (b.location === "remote") return "pull";
  if (configured === true && b.kind === "full" && (b.location === "machine" || b.location === "push_failed")) return "push";
  return null;
}

/**
 * Tải về không có job_id: một lượt đẩy đang giữ khoá kho, và máy chủ chờ khoá đó
 * tới 30 phút (core.RemoteLockWait) rồi mới bắt đầu job. Bảng tự tải lại trong
 * đúng hạn đó, thưa (15 giây một lần) vì việc chờ có thể dài.
 */
export const pullPollMs = 15_000;
export const pullPollWindowMs = 30 * 60 * 1000;
export type PullWatch = { id: string; until: number };

/**
 * Khoảng tải lại của bảng (cho refetchInterval): bật khi đang chờ một bản tải về
 * mà dòng đó còn là "chỉ trên kho" và chưa quá hạn; hết điều kiện thì false.
 */
export function pullPoll(items: Backup[] | undefined, watch: PullWatch | null, now: number): number | false {
  if (!watch || now > watch.until) return false;
  return items?.find((b) => b.id === watch.id)?.location === "remote" ? pullPollMs : false;
}
