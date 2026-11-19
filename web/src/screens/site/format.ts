// Định dạng ngày giờ theo ngôn ngữ đang chọn.

import type { Lang } from "../../i18n";

const locale: Record<Lang, string> = { vi: "vi-VN", en: "en-GB" };

export function formatDate(iso: string, lang: Lang): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString(locale[lang], { year: "numeric", month: "2-digit", day: "2-digit" });
}

export function formatTime(ms: number, lang: Lang): string {
  return new Date(ms).toLocaleTimeString(locale[lang], { hour12: false });
}
