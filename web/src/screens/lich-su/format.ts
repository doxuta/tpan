// Định dạng giờ và thời lượng cho màn Lịch sử. Giờ theo múi giờ của trình duyệt.

import type { Lang } from "../../i18n";
import type { strings } from "../../i18n/lich-su";

type T = (typeof strings)["vi"];

const locale = (lang: Lang) => (lang === "vi" ? "vi-VN" : "en-GB");

const clock = new Map<Lang, Intl.DateTimeFormat>();
const dayTime = new Map<Lang, Intl.DateTimeFormat>();

/** "19/11 14:02": ngày giờ cho hàng lịch sử (job trải nhiều ngày nên phải có ngày). Ghép tay từ các phần để hai ngôn ngữ cùng một dạng. */
export function formatDateTime(iso: string, lang: Lang): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  let f = dayTime.get(lang);
  if (!f) {
    f = new Intl.DateTimeFormat(locale(lang), { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
    dayTime.set(lang, f);
  }
  const p = Object.fromEntries(f.formatToParts(d).map((x) => [x.type, x.value]));
  return `${p.day}/${p.month} ${p.hour}:${p.minute}`;
}

/** "14:02:05": giờ của từng bước. */
export function formatClock(iso: string, lang: Lang): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  let f = clock.get(lang);
  if (!f) {
    f = new Intl.DateTimeFormat(locale(lang), { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
    clock.set(lang, f);
  }
  return f.format(d);
}

export function formatDuration(ms: number, t: Pick<T, "seconds" | "minutes" | "millis">, lang: Lang): string {
  if (ms < 1000) return t.millis(Math.round(ms));
  if (ms < 60_000) return t.seconds(new Intl.NumberFormat(locale(lang), { maximumFractionDigits: 1 }).format(ms / 1000));
  const s = Math.round(ms / 1000);
  return t.minutes(Math.floor(s / 60), s % 60);
}
