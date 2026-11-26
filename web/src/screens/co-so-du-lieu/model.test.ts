// Test phần thuần của M34 ở màn Cơ sở dữ liệu (DB đã bị xoá ngoài TPAN). Chạy
// bằng `make test-web`. Chuỗi viết thẳng ở đây vì file chuỗi thật nạp
// i18n/index.tsx, Node không đọc được .tsx.

import assert from "node:assert/strict";
import { test } from "node:test";
import { trashWording } from "./missing.ts";

const t = {
  trashDbWarning: (db: string) => `thùng rác ${db}`,
  trashErrorHeading: (db: string) => `lỗi thùng rác ${db}`,
  forgetDbWarning: (db: string) => `gỡ ${db}`,
  forgetErrorHeading: (db: string) => `lỗi gỡ ${db}`,
};

test("DB rời còn trong MariaDB: nút Xoá nói là đưa vào thùng rác", () => {
  assert.deepEqual(trashWording(t, { db: "alice_shop" }), { warning: "thùng rác alice_shop", errorHeading: "lỗi thùng rác alice_shop" });
  assert.deepEqual(trashWording(t, { db: "alice_shop", missing: false }), { warning: "thùng rác alice_shop", errorHeading: "lỗi thùng rác alice_shop" });
});

test("DB rời đã bị xoá ngoài TPAN: nút Xoá nói là gỡ khỏi TPAN", () => {
  assert.deepEqual(trashWording(t, { db: "alice_shop", missing: true }), { warning: "gỡ alice_shop", errorHeading: "lỗi gỡ alice_shop" });
});
