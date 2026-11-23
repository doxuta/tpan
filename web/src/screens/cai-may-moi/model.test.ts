// Test logic thuần của trang "Cài lên máy ảo mới". Chạy bằng `make test-web`.

import assert from "node:assert/strict";
import { test } from "node:test";
import { buildRequest, groupDigits, isSum, normSum, problems, sumProblem, sumsCommand, type Form } from "./model.ts";

const a = "a".repeat(64);
const b = "0123456789abcdef".repeat(4);

const form = (over: Partial<Form> = {}): Form => ({
  web: "nginx",
  private: false,
  base: "",
  amd64: a,
  arm64: b,
  domains: [],
  email: "",
  ...over,
});

test("sha256: bỏ khoảng trắng, hạ chữ hoa rồi mới kiểm", () => {
  assert.equal(normSum(`  ${b.toUpperCase()}\n`), b);
  assert.equal(sumProblem(`  ${b.toUpperCase()} `), null);
  assert.equal(isSum(b), true);
});

test("sha256: rỗng, thiếu một ký tự, ký tự không phải hex", () => {
  assert.equal(sumProblem("   "), "required");
  assert.equal(sumProblem(a.slice(1)), "invalid");
  assert.equal(sumProblem(a + "0"), "invalid");
  assert.equal(sumProblem("g" + a.slice(1)), "invalid");
  assert.equal(sumProblem(`${a.slice(0, 32)} ${a.slice(32)}`), "invalid");
});

test("không chọn site: sites null, email không bắt buộc, max_bytes 0", () => {
  const f = form({ web: "apache", private: true, base: " https://dl.example.com/tpan/v1 ", amd64: a.toUpperCase() });
  assert.deepEqual(problems(f), []);
  assert.deepEqual(buildRequest(f), {
    web: "apache",
    private: true,
    base: "https://dl.example.com/tpan/v1",
    sha256_amd64: a,
    sha256_arm64: b,
    max_bytes: 0,
    sites: null,
  });
});

test("có site: mỗi site chỉ có domain, email ở defaults, không có ssl", () => {
  const req = buildRequest(form({ domains: ["shop.example.com", "blog.example.com"], email: " admin@example.com " }));
  assert.deepEqual(req.sites, { defaults: { admin_email: "admin@example.com" }, sites: [{ domain: "shop.example.com" }, { domain: "blog.example.com" }] });
  assert.equal(JSON.stringify(req).includes("ssl"), false);
});

test("ô chưa gửi được theo thứ tự trên trang; email chỉ bắt buộc khi có site", () => {
  assert.deepEqual(problems(form({ amd64: "", arm64: "x", domains: ["a.example.com"], email: " " })), ["amd64", "arm64", "email"]);
  assert.deepEqual(problems(form({ email: "" })), []);
});

test("lệnh lấy SHA256SUMS và cách nhóm chữ số", () => {
  assert.equal(sumsCommand(""), "curl -fsSL <base>/SHA256SUMS");
  assert.equal(sumsCommand(" https://dl.example.com/v1/ "), "curl -fsSL https://dl.example.com/v1/SHA256SUMS");
  // Thân gửi đi bỏ "/" cuối giống hệt lệnh gợi ý, không thì máy chủ từ chối.
  assert.equal(buildRequest(form({ base: "https://dl.example.com/v1//" })).base, "https://dl.example.com/v1");
  assert.equal(groupDigits(16384), "16 384");
  assert.equal(groupDigits(987), "987");
  assert.equal(groupDigits(1234567), "1 234 567");
});
