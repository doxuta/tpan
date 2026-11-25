// Logic thuần của mục "Địa chỉ IP" (Cài đặt): kiểm một IP mới trước khi chuyển
// máy sang IP đó. Chỉ tính toán trên dữ liệu GET /api/v1/machine/ips, không gọi
// gì; test: ip-model.test.ts (node --test).

import type { MachineIPs } from "../../api/types";

const ipv4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;
const ipv6 = /^[0-9a-fA-F:]+$/;

export type IPFamily = "ipv4" | "ipv6";

/** Họ của chuỗi IP, null nếu không phải IP. IPv6 chỉ kiểm thô (có ":" và chữ hex). */
export function ipFamily(s: string): IPFamily | null {
  const v = s.trim();
  if (ipv4.test(v)) return "ipv4";
  if (v.includes(":") && ipv6.test(v) && v.split("::").length <= 2) return "ipv6";
  return null;
}

export type MovePlan = {
  family: IPFamily;
  /** IP đã nằm trên card mạng của máy. false: công ty chưa gắn, hoặc là IP NAT chuyển vào máy. */
  onMachine: boolean;
  /** Site có bản ghi (A với IPv4, AAAA với IPv6) chưa trỏ về IP mới: phải sửa DNS. */
  toFix: string[];
  /** Site đã trỏ về IP mới. */
  ready: string[];
  /** Panel chỉ nghe 127.0.0.1: đổi IP xong vẫn không mở được panel bằng IP. */
  panelPrivate: boolean;
};

export function planMove(data: MachineIPs, raw: string): MovePlan | null {
  const ip = raw.trim();
  const family = ipFamily(ip);
  if (!family) return null;
  const norm = family === "ipv6" ? ip.toLowerCase() : ip;
  const toFix: string[] = [];
  const ready: string[] = [];
  for (const s of data.sites) {
    const recs = family === "ipv4" ? s.a : s.aaaa;
    if (recs.map((r) => r.toLowerCase()).includes(norm)) ready.push(s.domain);
    else toFix.push(s.domain);
  }
  return {
    family,
    onMachine: data.addrs.some((a) => a.ip.toLowerCase() === norm),
    toFix,
    ready,
    panelPrivate: data.panel_private,
  };
}
