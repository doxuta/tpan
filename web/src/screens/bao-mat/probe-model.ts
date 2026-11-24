// Phần thuần của thẻ "Thử tấn công" (Probe.tsx): đổi trạng thái ô sang dấu
// trạng thái, đếm tóm tắt, tách đường không áp dụng, chia các lượt chưa dọn.
// Test: probe-model.test.ts (chạy bằng `make test-web`). Chỉ import kiểu, nên
// node chạy thẳng được.

import type { ProbePreview, ProbeResult, ProbeRun, ProbeStatusValue } from "../../api/m17-types";
import type { Preview, Status } from "../../api/types";

export type ProbeColumn = "outside" | "inside";

/** Thứ tự hiện trong dòng đếm: xấu nhất trước. */
export const probeOrder: readonly ProbeStatusValue[] = ["exposed", "needs_cage", "blocked", "skipped", "na"];

/** Trạng thái lạ (máy chủ mới hơn giao diện) hoặc rỗng (đường chưa chạy tới) coi như bỏ qua. */
export function cellKey(s: string | undefined): ProbeStatusValue {
  return probeOrder.find((k) => k === s) ?? "skipped";
}

/** Dấu trạng thái của một ô: bị chặn là đạt, lộ là lỗi, chỉ lồng chặn được là cảnh báo, còn lại là chưa kiểm được. */
export function cellStatus(s: string | undefined): Status {
  switch (cellKey(s)) {
    case "blocked":
      return "pass";
    case "exposed":
      return "fail";
    case "needs_cage":
      return "warn";
    default:
      return "unknown";
  }
}

/** Đường không thuộc stack của máy này: cả hai ô đều "na". */
export function isNotApplicable(r: ProbeResult): boolean {
  return r.outside.status === "na" && r.inside.status === "na";
}

/** Tách các đường thuộc stack (hiện thành bảng) khỏi các đường không áp dụng (gập lại). */
export function splitResults(results: ProbeResult[] | null | undefined): { shown: ProbeResult[]; na: ProbeResult[] } {
  const all = results ?? [];
  return { shown: all.filter((r) => !isNotApplicable(r)), na: all.filter(isNotApplicable) };
}

/** Đếm theo trạng thái trên mọi ô của các đường đã cho (mỗi đường hai ô). */
export function countCells(results: ProbeResult[]): Record<ProbeStatusValue, number> {
  const n: Record<ProbeStatusValue, number> = { exposed: 0, needs_cage: 0, blocked: 0, skipped: 0, na: 0 };
  for (const r of results) {
    n[cellKey(r.outside.status)]++;
    n[cellKey(r.inside.status)]++;
  }
  return n;
}

/** Các phần của dòng đếm: chỉ trạng thái có ô, theo thứ tự probeOrder. */
export function countParts(n: Record<ProbeStatusValue, number>): { key: ProbeStatusValue; n: number }[] {
  return probeOrder.filter((k) => n[k] > 0).map((k) => ({ key: k, n: n[k] }));
}

/** Ô lộ mở sẵn, các ô khác đóng (bằng chứng đóng sẵn trừ dòng lỗi); `over` là phần người dùng đã bấm. */
export function cellOpen(over: Record<string, boolean>, key: string, status: string | undefined): boolean {
  return over[key] ?? cellKey(status) === "exposed";
}

/** Lượt đang chạy (không được dọn, không được chạy thêm) tách khỏi lượt bỏ dở cần dọn. */
export function splitPending(pending: ProbeRun[] | null | undefined): { running: ProbeRun[]; stale: ProbeRun[] } {
  const all = pending ?? [];
  return { running: all.filter((p) => p.state === "running"), stale: all.filter((p) => p.state !== "running") };
}

/** Tên các site thử của những lượt này, theo thứ tự, không lặp. */
export function probeDomains(runs: ProbeRun[]): string[] {
  const out: string[] = [];
  for (const r of runs) {
    for (const d of [r.domain_a, r.domain_b]) if (d && !out.includes(d)) out.push(d);
  }
  return out;
}

/** Bản xem trước của lượt thử: Preview thường cộng trường run (có thể vắng ở máy chủ cũ). */
export function asProbePreview(p: Preview): Preview & Partial<Pick<ProbePreview, "run">> {
  return p;
}
