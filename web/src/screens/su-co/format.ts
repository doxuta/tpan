// Định dạng khoảng thời gian của sự cố ("2 giờ 5 phút"). Giờ và ngày giờ dùng
// lại formatClock và formatDateTime của màn Lịch sử.

import type { strings } from "../../i18n/su-co";

type T = (typeof strings)["vi"];

/** Khoảng từ iso tới bây giờ (hoặc tới end), tính bằng ms; không âm. */
export function spanMs(iso: string, end?: string): number {
  const to = end ? new Date(end).getTime() : Date.now();
  return Math.max(0, to - new Date(iso).getTime());
}

/** Làm tròn xuống phút: sự cố kéo dài vài giờ thì giây là nhiễu. */
export function formatSpan(ms: number, t: Pick<T, "spanLessThanMinute" | "spanMinutes" | "spanHours" | "spanDays">): string {
  const min = Math.floor(ms / 60_000);
  if (!Number.isFinite(min) || min < 1) return t.spanLessThanMinute;
  if (min < 60) return t.spanMinutes(min);
  const h = Math.floor(min / 60);
  if (h < 24) return t.spanHours(h, min % 60);
  return t.spanDays(Math.floor(h / 24), h % 24);
}
