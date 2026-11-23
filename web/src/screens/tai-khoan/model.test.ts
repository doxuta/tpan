// Test phần logic thuần của trang Tài khoản (chia nhóm bí mật, đường vẽ QR,
// phạm vi token). Chạy bằng `make test-web`.

import assert from "node:assert/strict";
import { test } from "node:test";
import { cleanCode, cleanRecovery, cleanTotp, codeReady, curlExample, defaultDays, recoveryLowNow, tokenProblem, groupSecret, qrMargin, qrPath, recoveryComplete, recoveryText, revocable, scopesOf, setRead, setWrite, totpComplete } from "./model.ts";

test("groupSecret chia nhóm 4 ký tự, nhóm cuối có thể ngắn hơn", () => {
  assert.equal(groupSecret("JBSWY3DPEHPK3PXP"), "JBSW Y3DP EHPK 3PXP");
  assert.equal(groupSecret("ABCDEF"), "ABCD EF");
  assert.equal(groupSecret(""), "");
});

test("qrPath gộp dãy ô liền nhau và cộng vùng trắng 4 ô", () => {
  const { size, d } = qrPath(["110", "010", "101"]);
  assert.equal(size, 3 + 2 * qrMargin);
  // Hàng 0: một dãy 2 ô từ cột 0; hàng 1: một ô ở cột 1; hàng 2: hai ô lẻ ở cột 0 và 2.
  assert.equal(d, "M4 4h2v1h-2zM5 5h1v1h-1zM4 6h1v1h-1zM6 6h1v1h-1z");
});

test("qrPath không vẽ gì cho ma trận trắng và không lệch với hàng ngắn", () => {
  assert.equal(qrPath(["000", "000"]).d, "");
  const { size, d } = qrPath(["1", "111"], 0);
  assert.equal(size, 3);
  assert.equal(d, "M0 0h1v1h-1zM0 1h3v1h-3z");
});

test("scopesOf: ghi thay cho đọc, nhóm không chọn bị bỏ, theo thứ tự nhóm", () => {
  assert.deepEqual(scopesOf(["sites", "databases", "jobs"], { jobs: "read", sites: "write" }), ["sites:write", "jobs:read"]);
  assert.deepEqual(scopesOf(["sites"], {}), []);
});

test("setRead và setWrite: ghi bao gồm đọc", () => {
  assert.equal(setWrite(true), "write");
  assert.equal(setWrite(false), "read");
  assert.equal(setRead("write", true), "write");
  assert.equal(setRead("none", true), "read");
  assert.equal(setRead("write", false), "none");
});

test("recoveryText có tên máy, ngày và mỗi mã một dòng", () => {
  assert.equal(recoveryText(["aaaa-bbbb-cccc-dddd", "eeee-ffff-gggg-hhhh"], "may1", "2026-11-21"), "TPAN may1\n2026-11-21\n\naaaa-bbbb-cccc-dddd\neeee-ffff-gggg-hhhh\n");
});

test("kiểm mã nhập: 6 số bỏ khoảng trắng, mã dự phòng đủ 16 ký tự", () => {
  assert.equal(cleanTotp(" 123 456 "), "123456");
  assert.equal(totpComplete("123 456"), true);
  assert.equal(totpComplete("12345"), false);
  assert.equal(totpComplete("12345a"), false);
  assert.equal(cleanRecovery("  abcd-efgh-ijkl-mnop "), "abcd-efgh-ijkl-mnop");
  assert.equal(recoveryComplete("abcd-efgh-ijkl-mnop"), true);
  assert.equal(recoveryComplete("abcdefghijklmnop"), true);
  assert.equal(recoveryComplete("abcd-efgh-ijkl"), false);
});

test("revocable chỉ cho token đang dùng", () => {
  assert.equal(revocable({ status: "active" }), true);
  assert.equal(revocable({ status: "expired" }), false);
  assert.equal(revocable({ status: "revoked" }), false);
});

test("curlExample ghép origin và token", () => {
  assert.equal(curlExample("x.example:8443", "tpan_abc"), 'curl -H "Authorization: Bearer tpan_abc" x.example:8443/api/v1/sites');
});

test("cleanCode: 6 số bỏ khoảng trắng, mã dự phòng giữ nguyên dấu gạch", () => {
  assert.equal(cleanCode(" 123 456 "), "123456");
  assert.equal(cleanCode(" abcd-efgh-ijkl-mnop "), "abcd-efgh-ijkl-mnop");
  assert.equal(codeReady("123 456"), true);
  assert.equal(codeReady("12345"), false);
  assert.equal(codeReady("abcd-efgh-ijkl-mnop"), true);
  assert.equal(codeReady("abcd-efgh-ijkl"), false);
});

test("recoveryLowNow nhắc từ 3 mã trở xuống", () => {
  assert.equal(recoveryLowNow(4), false);
  assert.equal(recoveryLowNow(3), true);
  assert.equal(recoveryLowNow(0), true);
});

test("defaultDays là 30 nhưng không vượt mức tối đa", () => {
  assert.equal(defaultDays(90), 30);
  assert.equal(defaultDays(7), 7);
  assert.equal(defaultDays(0), 1);
});

test("tokenProblem báo ô đầu tiên còn thiếu, theo thứ tự trên màn", () => {
  const ok = { name: "ci", scopes: ["sites:read"], days: "30", maxDays: 90, password: "x", needCode: false, code: "" };
  assert.equal(tokenProblem(ok), null);
  assert.equal(tokenProblem({ ...ok, name: "  " }), "name");
  assert.equal(tokenProblem({ ...ok, name: "a".repeat(65) }), "name");
  assert.equal(tokenProblem({ ...ok, scopes: [] }), "scopes");
  assert.equal(tokenProblem({ ...ok, days: "0" }), "days");
  assert.equal(tokenProblem({ ...ok, days: "91" }), "days");
  assert.equal(tokenProblem({ ...ok, days: "2.5" }), "days");
  assert.equal(tokenProblem({ ...ok, days: "" }), "days");
  assert.equal(tokenProblem({ ...ok, password: "" }), "password");
  assert.equal(tokenProblem({ ...ok, needCode: true }), "code");
  assert.equal(tokenProblem({ ...ok, needCode: true, code: "123456" }), null);
});
