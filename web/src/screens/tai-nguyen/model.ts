// Phép tính thuần dùng chung cho các màn của M17 (màn Tài nguyên, khối lồng ở
// trang một site, cột mức tối đa ở màn Gói): đổi đơn vị, phần trăm so với mức
// tối đa, CPU giữa hai lần đọc. Không phụ thuộc React để test bằng node --test
// (model.test.ts).

import type { CageUsage, Limits, SiteUsage } from "../../api/m17-types";

const MiB = 1024 * 1024;

/** Byte ra MiB, làm tròn (số liệu RAM của lồng luôn hiện bằng MiB như `tpan usage`). */
export function mib(bytes: number): number {
  return Math.round(bytes / MiB);
}

/** Một mức tối đa: 0 là không giới hạn (null). */
export function cap(v: number | undefined): number | null {
  return v && v > 0 ? v : null;
}

/** Phần trăm của used trên max (0 đến 100), null khi không có mức tối đa. */
export function pct(used: number, max: number | null): number | null {
  if (max === null || max <= 0) return null;
  return Math.min(100, Math.max(0, Math.round((used / max) * 100)));
}

/**
 * CPU trung bình giữa hai lần đọc, % của một nhân. usage_usec là bộ đếm dồn:
 * lần đọc sau nhỏ hơn (lồng vừa khởi động lại) hoặc cùng thời điểm thì không
 * tính được (null).
 */
export function cpuBetween(prev: CageUsage | undefined, cur: CageUsage): number | null {
  if (!prev || !prev.running || !cur.running) return null;
  const dt = (Date.parse(cur.at) - Date.parse(prev.at)) * 1000; // micro giây
  const du = cur.cpu_usec - prev.cpu_usec;
  if (!(dt > 0) || du < 0) return null;
  return Math.round((du / dt) * 1000) / 10;
}

/** Số lần chạm mức tối đa (RAM và tiến trình) từ khi lồng chạy. */
export function hits(u: CageUsage): number {
  return u.oom_kills + u.tasks_hits;
}

/** Một dòng gộp theo chủ (màn Tài nguyên, nhóm "theo user"): cộng số của các site. */
export type OwnerTotal = {
  owner: string;
  sites: number;
  memCurrent: number; // byte
  memPeakMax: number; // byte, đỉnh lớn nhất trong các site
  tasks: number;
  oomKills: number;
  tasksHits: number;
};

/** Gộp số liệu theo chủ, sắp theo tên. Mỗi site một slice, nên số của một user là tổng các site của họ. */
export function byOwner(rows: SiteUsage[]): OwnerTotal[] {
  const m = new Map<string, OwnerTotal>();
  for (const r of rows) {
    const t = m.get(r.owner) ?? { owner: r.owner, sites: 0, memCurrent: 0, memPeakMax: 0, tasks: 0, oomKills: 0, tasksHits: 0 };
    t.sites++;
    t.memCurrent += r.usage.mem_current;
    t.memPeakMax = Math.max(t.memPeakMax, r.usage.mem_peak);
    t.tasks += r.usage.tasks;
    t.oomKills += r.usage.oom_kills;
    t.tasksHits += r.usage.tasks_hits;
    m.set(r.owner, t);
  }
  return [...m.values()].sort((a, b) => a.owner.localeCompare(b.owner));
}

/** Các trường mức tối đa theo thứ tự hiện trên giao diện. */
export const limitKeys = ["cpu_percent", "ram_mb", "tasks", "io_read_mb", "io_write_mb", "php_workers"] as const satisfies readonly (keyof Limits)[];

/** Các trường gợi ý đặt (core: chỉ RAM, CPU, tiến trình). */
export const suggestedKeys = ["ram_mb", "cpu_percent", "tasks"] as const satisfies readonly (keyof Limits)[];

/** Trường có gợi ý khác mức đang đặt (bỏ trường gợi ý là 0: không có số liệu để gợi ý). */
export function changedLimits(current: Limits, suggested: Limits): (typeof suggestedKeys)[number][] {
  return suggestedKeys.filter((k) => suggested[k] > 0 && suggested[k] !== current[k]);
}
