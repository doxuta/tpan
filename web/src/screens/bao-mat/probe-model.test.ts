// Test phần thuần của thẻ "Thử tấn công". Chạy bằng `make test-web` (node --test).

import assert from "node:assert/strict";
import { test } from "node:test";
import type { ProbeCell, ProbeResult, ProbeRun, ProbeStatusValue } from "../../api/m17-types";
import { cellKey, cellOpen, cellStatus, countCells, countParts, isNotApplicable, probeDomains, splitPending, splitResults } from "./probe-model.ts";

const text = { vi: "x", en: "x" };
const cell = (status: ProbeStatusValue): ProbeCell => ({ status, detail: text });
const row = (path: string, outside: ProbeStatusValue, inside: ProbeStatusValue): ProbeResult => ({ path, title: text, outside: cell(outside), inside: cell(inside) });
const run = (id: string, state: ProbeRun["state"]): ProbeRun => ({ id, started: "", state, domain_a: `a-${id}.test`, domain_b: `b-${id}.test`, mode: "", cleaned: false });

test("năm trạng thái ô đổi sang bốn dấu trạng thái", () => {
  assert.equal(cellStatus("blocked"), "pass");
  assert.equal(cellStatus("exposed"), "fail");
  assert.equal(cellStatus("needs_cage"), "warn");
  assert.equal(cellStatus("skipped"), "unknown");
  assert.equal(cellStatus("na"), "unknown");
});

test("trạng thái lạ hoặc rỗng không bị coi là đạt", () => {
  assert.equal(cellKey(""), "skipped");
  assert.equal(cellKey(undefined), "skipped");
  assert.equal(cellKey("moi-la"), "skipped");
  assert.equal(cellStatus("moi-la"), "unknown");
});

test("đường na ở cả hai cột bị tách ra; na một cột thì vẫn hiện", () => {
  const rows = [row("L1", "blocked", "blocked"), row("L8", "na", "na"), row("C3", "needs_cage", "na")];
  assert.equal(isNotApplicable(rows[1]!), true);
  assert.equal(isNotApplicable(rows[2]!), false);
  const { shown, na } = splitResults(rows);
  assert.deepEqual(
    shown.map((r) => r.path),
    ["L1", "C3"],
  );
  assert.deepEqual(
    na.map((r) => r.path),
    ["L8"],
  );
  assert.deepEqual(splitResults(null), { shown: [], na: [] });
});

test("đếm theo ô, và dòng đếm bỏ trạng thái không có ô", () => {
  const rows = [row("L1", "blocked", "blocked"), row("C1", "needs_cage", "blocked"), row("C3", "skipped", "exposed")];
  const n = countCells(rows);
  assert.deepEqual(n, { exposed: 1, needs_cage: 1, blocked: 3, skipped: 1, na: 0 });
  assert.deepEqual(
    countParts(n).map((p) => p.key),
    ["exposed", "needs_cage", "blocked", "skipped"],
  );
  assert.deepEqual(countParts(countCells([])), []);
});

test("ô lộ mở sẵn, ô khác đóng; bấm là đảo lại", () => {
  assert.equal(cellOpen({}, "L1:inside", "exposed"), true);
  assert.equal(cellOpen({}, "L1:inside", "blocked"), false);
  assert.equal(cellOpen({ "L1:inside": false }, "L1:inside", "exposed"), false);
  assert.equal(cellOpen({ "L1:outside": true }, "L1:outside", "blocked"), true);
});

test("lượt đang chạy tách khỏi lượt cần dọn; tên site thử không lặp", () => {
  const { running, stale } = splitPending([run("1", "running"), run("2", "failed"), run("3", "done")]);
  assert.deepEqual(
    running.map((r) => r.id),
    ["1"],
  );
  assert.deepEqual(
    stale.map((r) => r.id),
    ["2", "3"],
  );
  assert.deepEqual(splitPending(null), { running: [], stale: [] });
  assert.deepEqual(probeDomains([run("2", "failed"), run("2", "failed")]), ["a-2.test", "b-2.test"]);
});
