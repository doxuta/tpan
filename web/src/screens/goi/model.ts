// Phần không dính giao diện của màn Gói (M17): kiểm ô mức tối đa của lồng,
// chữ tóm tắt các mức (dùng cả ở thẻ "Lồng" của trang một site), và khi nào
// sửa gói phải đi qua bản xem trước. Test: model.test.ts (make test-web).
//
// File này chỉ `import type` để node --test chạy thẳng được; khoảng hợp lệ
// (limitRange) và tasksSlack do bên gọi truyền vào qua Rules.

import type { CageRow, Limits } from "../../api/m17-types";

export type LimitKey = keyof Limits;

/** Giá trị trong các ô số, còn là chuỗi như người dùng gõ. */
export type LimitForm = Record<LimitKey, string>;

export type Rules = { range: Record<LimitKey, readonly [number, number]>; slack: number };

/**
 * Số worker khi gói để 0 (account.DefaultPHPWorkers). Server tính luật
 * "tiến trình ≥ worker + slack" với số này; ở đây chỉ để khớp, vì mức tối
 * thiểu của tiến trình (32) đã cao hơn 5 + 15.
 */
const defaultWorkers = 5;

export type LimitError =
  | { code: "notNumber" }
  | { code: "range"; min: number; max: number }
  | { code: "tasksLow"; need: number; workers: number; slack: number };

/** Lỗi theo từng ô. Có "needCage" khi gói không bật lồng mà vẫn có mức khác 0. */
export type LimitErrors = Partial<Record<LimitKey, LimitError>> & { needCage?: true };

/** Số nguyên không âm, tối đa 9 chữ số; còn lại là null. */
export function parseLimit(s: string): number | null {
  const v = s.trim();
  return /^\d{1,9}$/.test(v) ? Number(v) : null;
}

const keys: readonly LimitKey[] = ["cpu_percent", "ram_mb", "tasks", "io_read_mb", "io_write_mb", "php_workers"];

/** Cả sáu ô là số hợp lệ thì trả về số; ngược lại null. */
export function parseLimits(f: LimitForm): Limits | null {
  const out = {} as Limits;
  for (const k of keys) {
    const v = parseLimit(f[k]);
    if (v === null) return null;
    out[k] = v;
  }
  return out;
}

/** Có ô nào đang đặt mức khác 0 không (ô gõ dở, chưa là số, không tính). */
export function hasLimit(f: LimitForm): boolean {
  return keys.some((k) => (parseLimit(f[k]) ?? 0) > 0);
}

/** Sáu ô bị khoá: gói chưa bật lồng và chưa có mức nào. Có mức rồi thì ô mở để người ta đặt về 0. */
export function limitsLocked(cage: boolean, f: LimitForm): boolean {
  if (cage) return false;
  return keys.every((k) => parseLimit(f[k]) === 0);
}

/**
 * Kiểm theo đúng thứ tự của máy chủ (core.checkPackageLimits): từng ô
 * (là số, 0 hoặc trong khoảng), rồi tiến trình so với worker, rồi cờ lồng.
 * Số sai đến trước cờ thiếu, như máy chủ.
 */
export function checkLimits(f: LimitForm, cage: boolean, rules: Rules): LimitErrors {
  const errs: LimitErrors = {};
  for (const k of keys) {
    const v = parseLimit(f[k]);
    const [min, max] = rules.range[k];
    if (v === null) errs[k] = { code: "notNumber" };
    else if (v !== 0 && (v < min || v > max)) errs[k] = { code: "range", min, max };
  }
  const tasks = parseLimit(f.tasks);
  const workers = parseLimit(f.php_workers);
  if (errs.tasks === undefined && errs.php_workers === undefined && tasks !== null && tasks !== 0 && workers !== null) {
    const w = workers || defaultWorkers;
    if (tasks < w + rules.slack) errs.tasks = { code: "tasksLow", need: w + rules.slack, workers: w, slack: rules.slack };
  }
  if (Object.keys(errs).length === 0 && !cage && hasLimit(f)) errs.needCage = true;
  return errs;
}

/** Mức tối đa của một gói hay một site: trường vắng (server cũ) là 0. */
export function limitsOf(p: Partial<Limits> | undefined): Limits {
  return {
    cpu_percent: p?.cpu_percent ?? 0,
    ram_mb: p?.ram_mb ?? 0,
    tasks: p?.tasks ?? 0,
    io_read_mb: p?.io_read_mb ?? 0,
    io_write_mb: p?.io_write_mb ?? 0,
    php_workers: p?.php_workers ?? 0,
  };
}

/**
 * Sửa gói có phải đi qua bản xem trước (job cage.sync) không: có site trong
 * lồng dùng gói và một trong sáu mức đổi. Cờ lồng đổi một mình thì không:
 * máy chủ so sáu mức (account.Limits), không so cờ, nên chỉ ghi dòng gói.
 * Dự đoán sai không hỏng gì: máy chủ vẫn là bên quyết định (202 hay 200).
 */
export function needsPlan(old: Partial<Limits> | undefined, next: Limits, cagedSites: number): boolean {
  if (cagedSites === 0) return false;
  const o = limitsOf(old);
  return keys.some((k) => o[k] !== next[k]);
}

/** Tên miền của các site đang trong lồng và dùng gói pkg. */
export function cagedSitesOf(rows: readonly CageRow[] | undefined, pkg: string): string[] {
  return (rows ?? []).filter((r) => r.caged && r.package === pkg).map((r) => r.domain);
}

/** Chữ của từng phần trong dòng tóm tắt; bản chữ nằm ở i18n/goi.ts. */
export type SummaryFormat = {
  cpu: (n: number) => string;
  ram: (n: number) => string;
  tasks: (n: number) => string;
  read: (n: number) => string;
  write: (n: number) => string;
  workers: (n: number) => string;
  defaultWorkers: string;
  /** Khi cả năm mức đầu đều 0. */
  none: string;
  sep: string;
};

/**
 * "CPU 50% · RAM 200 MiB · 40 tiến trình · đọc 20 MB/s · ghi 20 MB/s · 4 worker".
 * Mức 0 bỏ qua; không còn mức nào thì nói "không giới hạn"; worker 0 là mặc định.
 */
export function limitSummary(l: Partial<Limits>, f: SummaryFormat): string {
  const v = limitsOf(l);
  const parts: string[] = [];
  if (v.cpu_percent > 0) parts.push(f.cpu(v.cpu_percent));
  if (v.ram_mb > 0) parts.push(f.ram(v.ram_mb));
  if (v.tasks > 0) parts.push(f.tasks(v.tasks));
  if (v.io_read_mb > 0) parts.push(f.read(v.io_read_mb));
  if (v.io_write_mb > 0) parts.push(f.write(v.io_write_mb));
  if (parts.length === 0) parts.push(f.none);
  parts.push(v.php_workers > 0 ? f.workers(v.php_workers) : f.defaultWorkers);
  return parts.join(f.sep);
}
