import assert from "node:assert/strict";
import { test } from "node:test";
import type { MachineIPs } from "../../api/types";
import { ipFamily, planMove } from "./ip-model.ts";

const data: MachineIPs = {
  hostname: "may-thu-1",
  addrs: [{ iface: "eth0", ip: "203.0.113.74", prefix: 24, family: "ipv4", public: true }],
  panel_port: "8443",
  panel_private: false,
  sites: [
    { domain: "a.example", a: ["203.0.113.74"], aaaa: [], here: true },
    { domain: "b.example", a: ["203.0.113.9"], aaaa: [], here: false },
  ],
};

test("ipFamily nhận IPv4, IPv6, từ chối chuỗi lạ", () => {
  assert.equal(ipFamily(" 203.0.113.74 "), "ipv4");
  assert.equal(ipFamily("2001:db8::5"), "ipv6");
  assert.equal(ipFamily("256.1.1.1"), null);
  assert.equal(ipFamily("1.2.3"), null);
  assert.equal(ipFamily("doan.example"), null);
  assert.equal(ipFamily("1::2::3"), null);
});

test("planMove: IP mới chưa trên máy, site nào còn trỏ IP cũ", () => {
  const p = planMove(data, "203.0.113.9");
  assert.ok(p);
  assert.equal(p.onMachine, false);
  assert.deepEqual(p.toFix, ["a.example"]);
  assert.deepEqual(p.ready, ["b.example"]);
});

test("planMove: IP đang có trên máy", () => {
  const p = planMove(data, "203.0.113.74");
  assert.ok(p);
  assert.equal(p.onMachine, true);
  assert.deepEqual(p.toFix, ["b.example"]);
});

test("planMove: chuỗi không phải IP thì null", () => {
  assert.equal(planMove(data, "abc"), null);
});
