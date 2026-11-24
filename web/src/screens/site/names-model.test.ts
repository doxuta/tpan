// Test logic thuần của tab Tên miền. Chạy bằng `make test-web` (node --test).

import assert from "node:assert/strict";
import { test } from "node:test";
import { dnsStatus, isWwwServe, otherMode } from "./names-model.ts";

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
