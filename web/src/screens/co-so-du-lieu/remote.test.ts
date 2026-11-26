// Test ô IP của khối Kết nối từ xa (M37). Chạy bằng `make test-web`. Cùng luật
// với TestCheckRemoteIP của máy chủ.

import assert from "node:assert/strict";
import { test } from "node:test";
import { parseHosts } from "./remote.ts";

test("nhận IPv4 đơn, bỏ trùng, nhiều cách ngăn", () => {
  assert.deepEqual(parseHosts("203.0.113.5\n198.51.100.7, 203.0.113.5", 10), { hosts: ["203.0.113.5", "198.51.100.7"], error: null });
  assert.deepEqual(parseHosts("  \n", 10), { hosts: [], error: null });
});

test("từ chối địa chỉ không phải máy ngoài và quá số máy", () => {
  for (const bad of ["127.0.0.1", "0.0.0.0", "224.0.0.1", "255.255.255.255", "10.0.0.0/8", "2001:db8::1", "may.example.com", "1.2.3.256", "01.2.3.4"]) {
    assert.equal(parseHosts(bad, 10).error, bad, bad);
  }
  const eleven = Array.from({ length: 11 }, (_, i) => `198.51.100.${i + 1}`).join("\n");
  assert.equal(parseHosts(eleven, 10).error, "max");
});
