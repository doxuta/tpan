// Test phần không dính giao diện của màn Gói (M17): kiểm ô mức tối đa theo đúng
// luật của máy chủ, dòng tóm tắt, và khi nào sửa gói đi qua bản xem trước.
// Chạy bằng `make test-web` (node --test, bỏ kiểu TypeScript khi chạy).

import assert from "node:assert/strict";
import { test } from "node:test";
import { limitRange, tasksSlack } from "../../api/m17-types.ts";
import type { CageRow } from "../../api/m17-types";
import { cagedSitesOf, checkLimits, hasLimit, limitSummary, limitsLocked, limitsOf, needsPlan, parseLimits, type LimitForm, type SummaryFormat } from "./model.ts";

const rules = { range: limitRange, slack: tasksSlack };
const zero: LimitForm = { cpu_percent: "0", ram_mb: "0", tasks: "0", io_read_mb: "0", io_write_mb: "0", php_workers: "0" };
const form = (o: Partial<LimitForm>): LimitForm => ({ ...zero, ...o });

const fmt: SummaryFormat = {
  cpu: (n) => `CPU ${n}%`,
  ram: (n) => `RAM ${n} MiB`,
  tasks: (n) => `${n} tiến trình`,
  read: (n) => `đọc ${n} MB/s`,
  write: (n) => `ghi ${n} MB/s`,
  workers: (n) => `${n} worker`,
  defaultWorkers: "worker mặc định",
  none: "không giới hạn",
  sep: " · ",
};

test("tóm tắt: đủ sáu mức theo đúng thứ tự", () => {
  const s = limitSummary({ cpu_percent: 50, ram_mb: 200, tasks: 40, io_read_mb: 20, io_write_mb: 20, php_workers: 4 }, fmt);
  assert.equal(s, "CPU 50% · RAM 200 MiB · 40 tiến trình · đọc 20 MB/s · ghi 20 MB/s · 4 worker");
});

test("tóm tắt: mức 0 bỏ qua, worker 0 là mặc định, không mức nào thì nói không giới hạn", () => {
  assert.equal(limitSummary({ ram_mb: 256 }, fmt), "RAM 256 MiB · worker mặc định");
  assert.equal(limitSummary({}, fmt), "không giới hạn · worker mặc định");
  assert.equal(limitSummary({ php_workers: 8 }, fmt), "không giới hạn · 8 worker");
});

test("gói chưa bật lồng, chưa có mức nào: sáu ô khoá; có mức rồi thì ô mở", () => {
  assert.equal(limitsLocked(false, zero), true);
  assert.equal(limitsLocked(true, zero), false);
  assert.equal(limitsLocked(false, form({ ram_mb: "256" })), false);
  // Ô gõ dở (chưa là số) không bị khoá lại giữa chừng.
  assert.equal(limitsLocked(false, form({ ram_mb: "" })), false);
});

test("kiểm: toàn 0 luôn hợp lệ, kể cả khi không bật lồng", () => {
  assert.deepEqual(checkLimits(zero, false, rules), {});
  assert.deepEqual(checkLimits(zero, true, rules), {});
});

test("kiểm: không phải số, ngoài khoảng", () => {
  const e = checkLimits(form({ cpu_percent: "abc", ram_mb: "10", io_read_mb: "100001", php_workers: "201" }), true, rules);
  assert.deepEqual(e.cpu_percent, { code: "notNumber" });
  assert.deepEqual(e.ram_mb, { code: "range", min: 64, max: 1048576 });
  assert.deepEqual(e.io_read_mb, { code: "range", min: 1, max: 100000 });
  assert.deepEqual(e.php_workers, { code: "range", min: 1, max: 200 });
  assert.equal(e.tasks, undefined);
  assert.equal(checkLimits(form({ cpu_percent: "-5" }), true, rules).cpu_percent?.code, "notNumber");
  assert.equal(checkLimits(form({ cpu_percent: "5" }), true, rules).cpu_percent, undefined);
  assert.equal(checkLimits(form({ cpu_percent: "6400" }), true, rules).cpu_percent, undefined);
  assert.equal(checkLimits(form({ cpu_percent: "6401" }), true, rules).cpu_percent?.code, "range");
});

test("kiểm: tiến trình phải từ worker + 15 khi cả hai khác 0", () => {
  // 40 nằm trong khoảng của ô, nhưng 40 < 40 + 15.
  const low = checkLimits(form({ tasks: "40", php_workers: "40" }), true, rules);
  assert.deepEqual(low.tasks, { code: "tasksLow", need: 55, workers: 40, slack: 15 });
  assert.deepEqual(checkLimits(form({ tasks: "55", php_workers: "40" }), true, rules), {});
  // Worker 0 (mặc định 5) thì mức tối thiểu 32 của ô đã đủ.
  assert.deepEqual(checkLimits(form({ tasks: "32" }), true, rules), {});
  // Tiến trình 0 (không giới hạn) không bị luật này.
  assert.deepEqual(checkLimits(form({ php_workers: "200" }), true, rules), {});
});

test("kiểm: có mức mà gói không bật lồng thì cần bật lồng; số sai đến trước", () => {
  assert.equal(checkLimits(form({ ram_mb: "256" }), false, rules).needCage, true);
  assert.equal(checkLimits(form({ php_workers: "4" }), false, rules).needCage, true);
  const both = checkLimits(form({ ram_mb: "10" }), false, rules);
  assert.equal(both.needCage, undefined);
  assert.equal(both.ram_mb?.code, "range");
  assert.equal(hasLimit(form({ ram_mb: "256" })), true);
  assert.equal(hasLimit(zero), false);
});

test("parseLimits: đủ sáu số thì trả số, thiếu một thì null", () => {
  assert.deepEqual(parseLimits(form({ ram_mb: " 256 " })), { cpu_percent: 0, ram_mb: 256, tasks: 0, io_read_mb: 0, io_write_mb: 0, php_workers: 0 });
  assert.equal(parseLimits(form({ ram_mb: "" })), null);
});

test("sửa gói chỉ đi qua bản xem trước khi có site trong lồng và một mức đổi", () => {
  const old = { cpu_percent: 50, ram_mb: 200 };
  const same = limitsOf(old);
  assert.equal(needsPlan(old, same, 3), false); // mức không đổi (cờ lồng đổi một mình cũng rơi vào đây)
  assert.equal(needsPlan(old, { ...same, ram_mb: 300 }, 3), true);
  assert.equal(needsPlan(old, { ...same, ram_mb: 300 }, 0), false); // không có site trong lồng
  assert.equal(needsPlan(undefined, limitsOf(undefined), 2), false); // server cũ: vắng là 0
  assert.equal(needsPlan(undefined, { ...limitsOf(undefined), tasks: 40 }, 2), true);
});

test("cagedSitesOf: chỉ site đang trong lồng của đúng gói", () => {
  const row = (domain: string, pkg: string, caged: boolean): CageRow => ({
    domain,
    owner: "u",
    package: pkg,
    caged,
    want: caged,
    files: caged,
    state: caged ? "trong" : "ngoai",
    suspended: false,
    limits: limitsOf(undefined),
  });
  const rows = [row("a.example.com", "pro", true), row("b.example.com", "pro", false), row("c.example.com", "free", true)];
  assert.deepEqual(cagedSitesOf(rows, "pro"), ["a.example.com"]);
  assert.deepEqual(cagedSitesOf(undefined, "pro"), []);
});
