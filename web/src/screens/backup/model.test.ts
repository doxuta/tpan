// Test phần kho của màn Backup (M18): cột "Ở đâu" chỉ hiện khi cần (B11), việc
// kho nào hiện ở hàng nào, và khi nào bảng tự tải lại sau một lần tải về.
// Chạy bằng `make test-web` (node --test, bỏ kiểu TypeScript khi chạy).

import assert from "node:assert/strict";
import { test } from "node:test";
import type { Backup, BackupLocation } from "../../api/types";
import { ownSqlBackups, pullPoll, pullPollMs, pullPollWindowMs, restoreKind, showWhere, storageWork } from "./model.ts";

const row = (id: string, location: BackupLocation | undefined, kind: Backup["kind"] = "full"): Backup => ({
  id,
  domain: "a.example.com",
  kind,
  reason: "manual",
  path: `/var/backups/tpan/a.example.com/${id}.tar.gz`,
  size: 1,
  sha256: "0".repeat(64),
  created: "2026-11-21T00:00:00Z",
  local: location !== "remote",
  remote: "",
  location,
});

test("máy không dùng kho thì không có cột Ở đâu", () => {
  assert.equal(showWhere([], undefined), false);
  assert.equal(showWhere([row("a", "machine"), row("b", "machine")], undefined), false);
  assert.equal(showWhere([row("a", "machine")], false), false);
  // location vắng (setting đích hỏng) không tự làm hiện cột.
  assert.equal(showWhere([row("a", undefined)], undefined), false);
});

test("cột hiện khi đã đặt đích hoặc có dòng không phải chỉ trên máy", () => {
  assert.equal(showWhere([], true), true);
  assert.equal(showWhere([row("a", "machine")], true), true);
  for (const loc of ["remote", "both", "other_target", "push_failed"] as const) {
    assert.equal(showWhere([row("a", "machine"), row("b", loc)], undefined), true, loc);
  }
});

test("user thường không có việc kho nào", () => {
  for (const loc of ["machine", "remote", "both", "other_target", "push_failed"] as const) {
    assert.equal(storageWork(row("a", loc), false, true), null, loc);
  }
});

test("admin: tải về cho bản chỉ còn trên kho, kể cả khi chưa đọc được đích", () => {
  assert.equal(storageWork(row("a", "remote"), true, true), "pull");
  assert.equal(storageWork(row("a", "remote"), true, undefined), "pull");
});

test("admin: đẩy lên kho cho bản chỉ trên máy hoặc đẩy lỗi, loại full, đã đặt đích", () => {
  assert.equal(storageWork(row("a", "machine"), true, true), "push");
  assert.equal(storageWork(row("a", "push_failed"), true, true), "push");
  assert.equal(storageWork(row("a", "machine"), true, false), null);
  assert.equal(storageWork(row("a", "machine"), true, undefined), null);
  assert.equal(storageWork(row("a", "machine", "sql"), true, true), null);
});

test("admin: bản ở cả hai nơi, bản ở kho khác, bản không rõ nơi thì không có việc kho", () => {
  assert.equal(storageWork(row("a", "both"), true, true), null);
  assert.equal(storageWork(row("a", "other_target"), true, true), null);
  assert.equal(storageWork(row("a", undefined), true, true), null);
});

test("hạn tự tải lại khớp hạn chờ khoá kho của máy chủ: 15 giây một lần, tối đa 30 phút", () => {
  assert.equal(pullPollMs, 15_000);
  assert.equal(pullPollWindowMs, 30 * 60 * 1000);
});

test("tải lại chỉ khi đang chờ, dòng còn là kho, chưa quá hạn", () => {
  const watch = { id: "a", until: 1000 };
  assert.equal(pullPoll([row("a", "remote")], watch, 500), pullPollMs);
  assert.equal(pullPoll([row("a", "remote")], watch, 1000), pullPollMs);
  // quá hạn
  assert.equal(pullPoll([row("a", "remote")], watch, 1001), false);
  // dòng đã về máy
  assert.equal(pullPoll([row("a", "both")], watch, 500), false);
  // dòng không còn, chưa có dữ liệu, không chờ gì
  assert.equal(pullPoll([row("b", "remote")], watch, 500), false);
  assert.equal(pullPoll(undefined, watch, 500), false);
  assert.equal(pullPoll([row("a", "remote")], null, 500), false);
});

// ---- M33: bản sao lưu của DB rời ----

const dbRow = (id: string, name = "alice_shop"): Backup => ({ ...row(id, "machine", "sql"), domain: name, db: true });

test("nút khôi phục của từng loại hàng", () => {
  // Site: bản full khôi phục được, site đã xoá hẳn thì tạo lại, site trong thùng rác thì không có nút.
  assert.equal(restoreKind(row("a", "machine"), "active", "active"), "restore");
  assert.equal(restoreKind(row("a", "machine"), "gone", "active"), "recreate");
  assert.equal(restoreKind(row("a", "machine"), "trashed", "active"), null);
  // Bản sql của site nhập ở màn Cơ sở dữ liệu, không có nút ở bảng này.
  assert.equal(restoreKind(row("a", "machine", "sql"), "active", "active"), null);
  // DB rời: tình trạng site của chuỗi đó không liên quan (tên DB không bao giờ là tên miền).
  assert.equal(restoreKind(dbRow("d"), "gone", "active"), "db-import");
  assert.equal(restoreKind(dbRow("d"), "gone", "gone"), "db-recreate");
  assert.equal(restoreKind(dbRow("d"), "gone", "trashed"), null);
});

test("màn quản lý DB chỉ lấy bản sql của đúng DB đó", () => {
  const items = [row("full", "machine"), row("sql", "machine", "sql"), dbRow("d1"), dbRow("d2", "alice_blog")];
  assert.deepEqual(ownSqlBackups(items, "a.example.com", false).map((b) => b.id), ["sql"]);
  assert.deepEqual(ownSqlBackups(items, "alice_shop", true).map((b) => b.id), ["d1"]);
  // Cùng chuỗi nhưng khác loại thì không lẫn.
  assert.deepEqual(ownSqlBackups(items, "alice_shop", false), []);
  assert.deepEqual(ownSqlBackups(items, "a.example.com", true), []);
});
