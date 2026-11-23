// Phần logic thuần của trang Tài khoản và của luồng bật hai bước (M20): chia
// nhóm chuỗi bí mật, dựng đường vẽ QR, đổi bảng chọn phạm vi thành danh sách
// phạm vi của token, dựng văn bản mã dự phòng. File .ts thuần chỉ import kiểu,
// để `make test-web` (node --test) chạy được không cần trình đóng gói.

import type { TokenGroup, TokenRow } from "../../api/m20-types";

/** "JBSWY3DPEHPK3PXP" → "JBSW Y3DP EHPK 3PXP": nhóm 4 ký tự để gõ tay vào app cho dễ. */
export function groupSecret(secret: string): string {
  return (secret.match(/.{1,4}/g) ?? []).join(" ");
}

/** Vùng trắng quanh QR theo chuẩn: 4 ô mỗi bên. Có vùng này nhiều app mới đọc được. */
export const qrMargin = 4;

/**
 * Đường vẽ của QR: mỗi dãy ô '1' liền nhau trên một hàng là một hình chữ nhật
 * cao 1 (M x y h n v1 h-n z), nên cả mã là một <path>. size là cạnh của vùng
 * vẽ gồm cả vùng trắng. Hàng ngắn hơn hàng dài nhất coi như ô trắng.
 */
export function qrPath(rows: string[], margin = qrMargin): { size: number; d: string } {
  const n = Math.max(rows.length, ...rows.map((r) => r.length));
  const parts: string[] = [];
  rows.forEach((row, y) => {
    let x = 0;
    while (x < row.length) {
      if (row[x] !== "1") {
        x++;
        continue;
      }
      const start = x;
      while (x < row.length && row[x] === "1") x++;
      const len = x - start;
      parts.push(`M${start + margin} ${y + margin}h${len}v1h-${len}z`);
    }
  });
  return { size: n + 2 * margin, d: parts.join("") };
}

/** Quyền chọn cho một nhóm: "write" bao gồm "read". */
export type Level = "none" | "read" | "write";

/** Bảng chọn → danh sách phạm vi gửi cho máy chủ ("sites:write", "jobs:read"), theo thứ tự của groups. */
export function scopesOf(groups: TokenGroup[], pick: Partial<Record<TokenGroup, Level>>): string[] {
  const out: string[] = [];
  for (const g of groups) {
    const l = pick[g] ?? "none";
    if (l !== "none") out.push(`${g}:${l}`);
  }
  return out;
}

/** Đặt ô "Đọc" của một nhóm: bỏ đọc thì bỏ cả ghi (ghi bao gồm đọc). */
export function setRead(cur: Level, on: boolean): Level {
  return on ? (cur === "write" ? "write" : "read") : "none";
}

/** Đặt ô "Ghi": bật ghi thì đọc đi theo; tắt ghi thì còn đọc. */
export function setWrite(on: boolean): Level {
  return on ? "write" : "read";
}

/** Nội dung file .txt của mười mã dự phòng: mỗi mã một dòng, kèm tên máy và ngày để phân biệt các bộ. */
export function recoveryText(codes: string[], host: string, date: string): string {
  return [`TPAN ${host}`, date, "", ...codes, ""].join("\n");
}

/** Trạng thái hiện của hàng token: máy chủ tính sẵn, nhưng hết hạn mà chưa thu hồi thì vẫn thu hồi được. */
export function revocable(row: Pick<TokenRow, "status">): boolean {
  return row.status === "active";
}

/** Còn từng này mã dự phòng thì nhắc tạo lại. */
export const recoveryLow = 3;

/** Mã 6 số: bỏ khoảng trắng người dùng dán vào ("123 456"). */
export function cleanTotp(code: string): string {
  return code.replace(/\s+/g, "");
}

/** Mã dự phòng 16 ký tự (xxxx-xxxx-xxxx-xxxx): chỉ cắt khoảng trắng hai đầu, dấu gạch giữ nguyên cho máy chủ. */
export function cleanRecovery(code: string): string {
  return code.trim();
}

/** Mã dự phòng đủ dài chưa: 16 ký tự không kể dấu gạch và khoảng trắng. */
export function recoveryComplete(code: string): boolean {
  return code.replace(/[-\s]/g, "").length === 16;
}

/** Mã 6 số đủ chưa. */
export function totpComplete(code: string): boolean {
  return /^\d{6}$/.test(cleanTotp(code));
}

/** Ví dụ gọi API bằng token; origin là địa chỉ panel đang mở (window.location.origin). */
export function curlExample(origin: string, token: string): string {
  return `curl -H "Authorization: Bearer ${token}" ${origin}/api/v1/sites`;
}
