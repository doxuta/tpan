// Phần tính toán của Bảng điều khiển, không có giao diện: từ /api/v1/overview
// dựng ra các tuyến (mỗi site một tuyến sáu ga), danh sách sự cố, cảnh báo, và
// các hàm định dạng số, byte, giờ. Tách riêng để component chỉ còn việc vẽ.

import type { CheckResult, Overview, SiteRow, Stage, Status, Traffic } from "../../api/types";
import type { Lang } from "../../i18n";

/** Sáu ga trên một tuyến, theo thứ tự đường đi của request (mục 1 của GIAO-DIEN.md). */
export const stationStages = ["dns", "tls", "web", "php", "wordpress", "db"] as const;
export type StationStage = (typeof stationStages)[number];

/** Một phép kiểm; shared là phép kiểm của cả máy (stack_checks), không riêng một site. */
export type Item = { check: CheckResult; shared: boolean };

/**
 * Nét tuyến đi qua ga (mục 2): liền, đứt, hoặc "split" ở ga hỏng: liền nửa
 * trái (tới được ga), đứt nửa phải (từ ga trở đi).
 */
export type Line = "solid" | "dashed" | "split";

export type Cell = { stage: StationStage; status: Status; items: Item[]; line: Line };

export type Route = {
  domain: string;
  cells: Cell[];
  /** Cột HTTP lấy từ phép kiểm chặng e2e. code null: chưa có mã (chưa gọi được, hoặc chưa có phép kiểm). */
  http: { code: string | null; status: Status; items: Item[] };
  /** Chặng hỏng đầu tiên trên tuyến này, để viết dòng "Hỏng ở ..." dưới hàng. */
  broken: { stage: Stage; item: Item } | null;
  /** Request và 5xx một giờ qua của site; không có khi server chưa đọc được. */
  traffic?: Traffic;
};

// Xấu nhất thắng. "Chưa kiểm được" xếp trên "đạt" vì một phép kiểm chưa chạy
// được thì không được khẳng định cả chặng đạt; "cảnh báo" xếp trên "chưa kiểm
// được" vì cảnh báo là điều đã biết chắc.
const rank: Record<Status, number> = { pass: 0, unknown: 1, warn: 2, fail: 3 };

/** Trạng thái của một chặng. Chưa có phép kiểm nào (ví dụ DNS trước lần kiểm nặng đầu tiên) là "chưa kiểm được". */
export function worst(items: Item[]): Status {
  if (items.length === 0) return "unknown";
  return items.reduce<Status>((w, i) => (rank[i.check.status] > rank[w] ? i.check.status : w), "pass");
}

/** Mã HTTP ở dòng đầu bằng chứng của phép kiểm http.site ("HTTP 502, 80 byte"); "HTTP ?" là chưa có mã. */
function httpCode(items: Item[]): string | null {
  const out = items.find((i) => i.check.evidence?.length)?.check.evidence?.[0]?.output ?? "";
  return /^HTTP (\d{3})\b/.exec(out)?.[1] ?? null;
}

export function buildRoute(row: SiteRow, shared: CheckResult[]): Route {
  const itemsOf = (stage: Stage): Item[] => [
    ...shared.filter((c) => c.stage === stage).map((check) => ({ check, shared: true })),
    ...row.checks.filter((c) => c.stage === stage).map((check) => ({ check, shared: false })),
  ];

  const cells: Cell[] = [];
  let broken: Route["broken"] = null;
  for (const stage of stationStages) {
    const items = itemsOf(stage);
    const status = worst(items);
    // Từ ga hỏng trở đi nét luôn đứt, kể cả ga sau đó đạt; ga "chưa kiểm được" đứt ở chính nó.
    const line: Line = broken ? "dashed" : status === "fail" ? "split" : status === "unknown" ? "dashed" : "solid";
    if (!broken && status === "fail") {
      broken = { stage, item: items.find((i) => i.check.status === "fail")! };
    }
    cells.push({ stage, status, items, line });
  }

  const httpItems = itemsOf("e2e");
  const http = { code: httpCode(httpItems), status: worst(httpItems), items: httpItems };
  // Chỉ phép kiểm cuối hỏng (ví dụ trả 200 nhưng không phải WordPress): sáu ga đều ổn mà site vẫn hỏng, nên phải nói ra.
  if (!broken && http.status === "fail") {
    broken = { stage: "e2e", item: httpItems.find((i) => i.check.status === "fail")! };
  }
  return { domain: row.site.domain, cells, http, broken, traffic: row.traffic };
}

export type Entry = { check: CheckResult; /** null: phép kiểm của cả máy. */ domain: string | null };

/**
 * Mọi phép kiểm có trạng thái này: của máy, rồi của chính tpan (cũng domain
 * null, nhận ra bằng check.stage "tpan"), rồi của từng site. Dùng cho khối
 * "Cảnh báo"; khối "Sự cố đang mở" đọc thẳng o.incidents.
 */
export function checksWith(o: Overview, status: Status): Entry[] {
  return [
    ...o.stack_checks.map((check) => ({ check, domain: null })),
    ...o.tpan_checks.map((check) => ({ check, domain: null })),
    ...o.sites.flatMap((r) => r.checks.map((check) => ({ check, domain: r.site.domain }))),
  ].filter((e) => e.check.status === status);
}

// ---- Định dạng ----

const locale = (lang: Lang) => (lang === "vi" ? "vi-VN" : "en-GB");

export function fmtNumber(v: number, lang: Lang, digits = 1): string {
  return new Intl.NumberFormat(locale(lang), { maximumFractionDigits: digits }).format(v);
}

/** Byte thành GB (hoặc MB khi dưới 1 GB), hệ 1024 như `free -h`. */
export function fmtBytes(n: number, lang: Lang): string {
  return n >= 2 ** 30 ? `${fmtNumber(n / 2 ** 30, lang)} GB` : `${fmtNumber(n / 2 ** 20, lang)} MB`;
}

export function fmtClock(ms: number, lang: Lang): string {
  return new Intl.DateTimeFormat(locale(lang), { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(ms);
}

/** "14:02" nếu là hôm nay, "Hôm qua 17:20", còn lại "26/11 09:15". */
export function fmtWhen(iso: string, lang: Lang, yesterday: string, now = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const hm = new Intl.DateTimeFormat(locale(lang), { hour: "2-digit", minute: "2-digit", hour12: false }).format(d);
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diffDays = Math.round((day(now) - day(d)) / 86_400_000);
  if (diffDays === 0) return hm;
  if (diffDays === 1) return `${yesterday} ${hm}`;
  return `${new Intl.DateTimeFormat(locale(lang), { day: "2-digit", month: "2-digit" }).format(d)} ${hm}`;
}
