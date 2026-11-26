// Test logic thuần của tab Tên miền. Chạy bằng `make test-web` (node --test).

import assert from "node:assert/strict";
import { test } from "node:test";
import type { NameView, RedirectView } from "../../api/m19-types.ts";
import { addressRows, dnsStatus, isWwwServe, otherMode } from "./names-model.ts";

test("dnsStatus: không bao giờ đỏ", () => {
  assert.equal(dnsStatus("ok"), "pass");
  assert.equal(dnsStatus("missing"), "warn");
  assert.equal(dnsStatus("warn"), "unknown");
});

test("isWwwServe: chỉ tên www kiểu phục vụ", () => {
  assert.equal(isWwwServe("www.example.com", "serve"), true);
  assert.equal(isWwwServe(" WWW.Example.com ", "serve"), true);
  assert.equal(isWwwServe("www.example.com", "redirect"), false);
  assert.equal(isWwwServe("shop.example.com", "serve"), false);
  assert.equal(isWwwServe("www.example.com", undefined), false);
});

test("otherMode: đảo kiểu", () => {
  assert.equal(otherMode("serve"), "redirect");
  assert.equal(otherMode("redirect"), "serve");
});

test("addressRows: tên chính trước, rồi tên khác, cuối là đường dẫn; khoá không trùng", () => {
  const name = (n: string, kind: NameView["kind"], mode: NameView["mode"] = ""): NameView => ({ name: n, kind, mode, state: "active", dns: "ok", in_cert: false });
  const rule = (id: number, source: string): RedirectView => ({ id, source, target: "/moi", code: 301, keep_query: false, state: "active" });
  const rows = addressRows([name("www.example.com", "alias", "redirect"), name("example.com", "primary"), name("shop.example.com", "alias", "serve")], [rule(7, "/cu"), rule(2, "/cu-2")]);
  assert.deepEqual(
    rows.map((r) => r.key),
    ["name:example.com", "name:www.example.com", "name:shop.example.com", "path:7", "path:2"],
  );
  assert.deepEqual(
    rows.map((r) => r.kind),
    ["name", "name", "name", "path", "path"],
  );
  assert.equal(new Set(rows.map((r) => r.key)).size, rows.length);
  assert.deepEqual(addressRows([], []), []);
});
