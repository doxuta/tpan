// Test logic thuần của thẻ "Đích ngoài máy". Chạy bằng `make test-web`
// (node --test, bỏ kiểu TypeScript khi chạy).

import assert from "node:assert/strict";
import { test } from "node:test";
import type { TargetView } from "../../api/m18-types";
import { buildRequest, checkForm, formFromView, keyHint, parseRemoteKeep, type TargetForm } from "./target-model.ts";

const set: TargetView = {
  configured: true,
  endpoint: "https://s3.example.com",
  region: "us-east-1",
  bucket: "tpan-demo",
  prefix: "tpan/demo",
  path_style: true,
  keep: 7,
  has_ca: false,
  access_key_tail: "ABCD",
  has_backup_key: true,
  panel_due: false,
  remote_only: 0,
  other_target: 0,
  failed: 0,
};
const unset: TargetView = { configured: false, path_style: true, has_ca: false, has_backup_key: false, panel_due: false, remote_only: 0, other_target: 0, failed: 0 };

const filled = (over: Partial<TargetForm> = {}): TargetForm => ({ ...formFromView(set), ...over });

test("form bắt đầu từ đích đang đặt, hai ô khoá trống, CA giữ như cũ", () => {
  const f = formFromView(set);
  assert.equal(f.endpoint, "https://s3.example.com");
  assert.equal(f.keep, "7");
  assert.equal(f.vhost, false);
  assert.equal(f.ca, "keep");
  assert.equal(f.accessKey, "");
  assert.equal(f.secretKey, "");
});

test("chưa đặt đích: CA mặc định không dùng, kiểu đường dẫn", () => {
  const f = formFromView(unset);
  assert.equal(f.ca, "none");
  assert.equal(f.vhost, false);
  assert.equal(f.keep, "");
});

test("path_style là phủ định của ô bucket.host", () => {
  assert.equal(buildRequest(filled()).path_style, true);
  assert.equal(buildRequest(filled({ vhost: true })).path_style, false);
});

test("ba chế độ CA ra ba giá trị ca_pem", () => {
  assert.equal(buildRequest(filled({ ca: "keep", caPem: "bỏ qua" })).ca_pem, "");
  assert.equal(buildRequest(filled({ ca: "none", caPem: "bỏ qua" })).ca_pem, "none");
  assert.equal(buildRequest(filled({ ca: "custom", caPem: "-----BEGIN CERTIFICATE-----\nx\n" })).ca_pem, "-----BEGIN CERTIFICATE-----\nx\n");
});

test("khoá trống đi trống (giữ khoá đang lưu); khoá không bị cắt khoảng trắng", () => {
  const r = buildRequest(filled({ endpoint: "  https://s3.example.com " }));
  assert.equal(r.endpoint, "https://s3.example.com");
  assert.equal(r.access_key, "");
  assert.equal(r.secret_key, "");
  assert.equal(buildRequest(filled({ accessKey: " AK", secretKey: "sk " })).secret_key, "sk ");
});

test("số bản giữ: trống là 0 (mặc định của máy chủ), 1..365", () => {
  assert.equal(parseRemoteKeep(""), 0);
  assert.equal(parseRemoteKeep("1"), 1);
  assert.equal(parseRemoteKeep("365"), 365);
  for (const bad of ["0", "366", "2.5", "-1", "abc", "1000"]) assert.equal(parseRemoteKeep(bad), null, bad);
});

test("kiểm form: một khoá thiếu, chưa đặt đích mà không có khoá, CA riêng trống", () => {
  assert.deepEqual(checkForm(filled(), true), []);
  assert.deepEqual(checkForm(filled({ accessKey: "AK" }), true), ["keys"]);
  assert.deepEqual(checkForm(filled(), false), ["keysRequired"]);
  assert.deepEqual(checkForm(filled({ accessKey: "AK", secretKey: "secret123" }), false), []);
  assert.deepEqual(checkForm(filled({ ca: "custom", caPem: "  " }), true), ["caPem"]);
  assert.deepEqual(checkForm(filled({ endpoint: " ", bucket: "", keep: "0" }), true), ["endpoint", "bucket", "keep"]);
});

test("khoá cuối: …ABCD, hoặc số ký tự khi máy chủ không cho tail", () => {
  assert.deepEqual(keyHint(set), { tail: "…ABCD" });
  assert.deepEqual(keyHint({ ...set, access_key_tail: "", access_key_len: 8 }), { len: 8 });
});
