// M37 (v0.2.17): đọc ô "IP được nối vào" của khối Kết nối từ xa: mỗi dòng (hoặc
// cách nhau bằng dấu phẩy, khoảng trắng) một IPv4. Kiểm như máy chủ
// (steps.CheckRemoteIP): IPv4 đơn, không phải loopback, 0.0.0.0, multicast hay
// 255.255.255.255; bỏ trùng; tối đa max. Máy chủ kiểm lại.

export type HostsParse = { hosts: string[]; error: string | null };

function ipv4(s: string): number[] | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s);
  if (!m) return null;
  const parts = m.slice(1).map((x) => (x.length > 1 && x.startsWith("0") ? NaN : Number(x)));
  return parts.every((n) => Number.isInteger(n) && n >= 0 && n <= 255) ? parts : null;
}

/** error là địa chỉ sai đầu tiên, "max" khi quá số máy cho phép. */
export function parseHosts(text: string, max: number): HostsParse {
  const hosts: string[] = [];
  for (const raw of text.split(/[\s,]+/)) {
    if (raw === "") continue;
    const p = ipv4(raw);
    if (!p || p[0] === 127 || p[0] === 0 || (p[0] >= 224 && p[0] <= 239) || p.every((n) => n === 255)) return { hosts, error: raw };
    const s = p.join(".");
    if (!hosts.includes(s)) hosts.push(s);
  }
  if (hosts.length > max) return { hosts, error: "max" };
  return { hosts, error: null };
}
