// Test phép tính dùng chung của các màn M17 (model.ts). Chạy bằng `make test-web`.

import assert from "node:assert/strict";
import { test } from "node:test";
import type { CageUsage, Limits, SiteUsage } from "../../api/m17-types";
import { byOwner, cap, changedLimits, cpuBetween, mib, pct } from "./model.ts";

const usage = (over: Partial<CageUsage> = {}): CageUsage => ({
  running: true,
  at: "2026-11-25T00:00:00Z",
  mem_current: 0,
  mem_peak: 0,
  oom_kills: 0,
  mem_max_hits: 0,
  cpu_usec: 0,
  throttled_usec: 0,
  nr_throttled: 0,
  tasks: 0,
  tasks_peak: 0,
  tasks_hits: 0,
  io_rbytes: 0,
  io_wbytes: 0,
  mem_max: 0,
  swap_max: -1,
  cpu_quota_us: 0,
  cpu_period_us: 100000,
  tasks_max: 0,
  io_rbps_max: 0,
  io_wbps_max: 0,
  ...over,
});

const limits = (over: Partial<Limits> = {}): Limits => ({
  cpu_percent: 0,
  ram_mb: 0,
  tasks: 0,
  io_read_mb: 0,
  io_write_mb: 0,
  php_workers: 0,
  ...over,
});

test("đổi byte ra MiB và mức 0 là không giới hạn", () => {
  assert.equal(mib(209715200), 200);
  assert.equal(cap(0), null);
  assert.equal(cap(undefined), null);
  assert.equal(cap(200), 200);
});

test("phần trăm kẹp trong 0 đến 100, không có mức tối đa thì null", () => {
  assert.equal(pct(50, 200), 25);
  assert.equal(pct(300, 200), 100);
  assert.equal(pct(5, null), null);
});

test("CPU giữa hai lần đọc: 0,5 giây CPU trong 1 giây là 50%", () => {
  const a = usage({ at: "2026-11-25T00:00:00Z", cpu_usec: 1_000_000 });
  const b = usage({ at: "2026-11-25T00:00:01Z", cpu_usec: 1_500_000 });
  assert.equal(cpuBetween(a, b), 50);
  // Bộ đếm lùi (lồng khởi động lại), cùng thời điểm, hoặc chưa có lần trước: không tính được.
  assert.equal(cpuBetween(b, a), null);
  assert.equal(cpuBetween(a, a), null);
  assert.equal(cpuBetween(undefined, b), null);
});

test("gộp theo user cộng số của các site, đỉnh lấy lớn nhất", () => {
  const rows: SiteUsage[] = [
    { domain: "b.example.com", owner: "khach", limits: limits(), usage: usage({ mem_current: 10, mem_peak: 50, oom_kills: 1, tasks: 2 }) },
    { domain: "a.example.com", owner: "khach", limits: limits(), usage: usage({ mem_current: 20, mem_peak: 90, tasks_hits: 2, tasks: 3 }) },
    { domain: "c.example.com", owner: "admin", limits: limits(), usage: usage({ mem_current: 5 }) },
  ];
  const got = byOwner(rows);
  assert.deepEqual(
    got.map((t) => [t.owner, t.sites, t.memCurrent, t.memPeakMax, t.tasks, t.oomKills, t.tasksHits]),
    [
      ["admin", 1, 5, 0, 0, 0, 0],
      ["khach", 2, 30, 90, 5, 1, 2],
    ],
  );
});

test("chỉ trường có gợi ý khác mức đang đặt mới là thay đổi", () => {
  const cur = limits({ ram_mb: 200, cpu_percent: 50, tasks: 40 });
  assert.deepEqual(changedLimits(cur, limits({ ram_mb: 320, cpu_percent: 50, tasks: 0 })), ["ram_mb"]);
});
