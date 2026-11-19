// Phần không dính giao diện của màn Thùng rác: khoá truy vấn, đường dẫn API,
// kiểm số ngày giữ và cách tính "còn N ngày" từ hạn xoá hẳn.
//
// Khoá truy vấn khai báo ở đây chứ không thêm vào api/queries.ts (file đó của
// phiên chính). Khoá khác khoá overview nên màn tự invalidate cả hai sau job.

import type { Lang } from "../../i18n";

export const trashKey = ["trash"] as const;

export const trashPath = "/api/v1/trash";
export const trashSettingsPath = "/api/v1/trash/settings";
export const restorePath = (domain: string) => `/api/v1/sites/${encodeURIComponent(domain)}/restore`;
export const purgePath = (domain: string) => `/api/v1/trash/${encodeURIComponent(domain)}`;

/** Trần số ngày giữ: khớp core.MaxTrashDays, để máy chủ không phải từ chối một lỗi gõ (7000). */
export const maxDays = 365;

/** Số ngày giữ hợp lệ: số nguyên 0..365 (0 là hết hạn ngay). Chuỗi rỗng hay "7.5" thì null. */
export function parseDays(raw: string): number | null {
  const s = raw.trim();
  if (!/^\d{1,3}$/.test(s)) return null;
  const n = Number(s);
  return n <= maxDays ? n : null;
}

/** Hạn xoá hẳn so với bây giờ. "today": cùng ngày lịch; "overdue": đã quá giờ, bộ hẹn giờ sẽ dọn trong vòng một giờ. */
export type Due = { kind: "unknown" } | { kind: "overdue" } | { kind: "today" } | { kind: "days"; n: number };

const dayMs = 86_400_000;

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** Tính theo ngày lịch (giờ trình duyệt), để 23:00 hôm nay và 01:00 mai không cùng gọi là "còn 1 ngày" hay "hôm nay" sai chỗ. */
export function dueOf(purgeAt: string | undefined, now: Date): Due {
  if (!purgeAt) return { kind: "unknown" };
  const at = new Date(purgeAt);
  if (Number.isNaN(at.getTime())) return { kind: "unknown" };
  if (at.getTime() <= now.getTime()) return { kind: "overdue" };
  const n = Math.round((startOfDay(at) - startOfDay(now)) / dayMs);
  return n <= 0 ? { kind: "today" } : { kind: "days", n };
}

const formats = new Map<Lang, Intl.DateTimeFormat>();

/** "19/11/2026 14:02" ở cả hai ngôn ngữ (ghép tay để hai bản cùng một dạng); hạn tới 365 ngày nên có năm. */
export function formatWhen(iso: string | undefined, lang: Lang): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  let f = formats.get(lang);
  if (!f) {
    f = new Intl.DateTimeFormat(lang === "vi" ? "vi-VN" : "en-GB", {
      day: "2-digit",
      month: "2-digit",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    });
    formats.set(lang, f);
  }
  const p = Object.fromEntries(f.formatToParts(d).map((x) => [x.type, x.value]));
  return `${p.day}/${p.month}/${p.year} ${p.hour}:${p.minute}`;
}
