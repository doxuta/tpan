// Logic thuần của tab Tên miền (không đụng React, để test bằng `make test-web`).

import type { NameMode, NameView, RedirectView } from "../../api/m19-types.ts";
import type { Status } from "../../api/types.ts";

/**
 * Dấu trạng thái của cột DNS. DNS của tên phụ chỉ tới mức cảnh báo (health
 * site.alias.dns không bao giờ đỏ, mục 3.8 của thiết kế M19): "missing" là
 * cảnh báo chứ không phải lỗi; "warn" của API là chưa tra được nên là "chưa kiểm được".
 */
export function dnsStatus(dns: NameView["dns"]): Status {
  return dns === "ok" ? "pass" : dns === "missing" ? "warn" : "unknown";
}

/** Tên www kiểu phục vụ: WordPress tự đưa về tên trong Site Address (S2 của M19), nên hiện một dòng nói trước. */
export function isWwwServe(name: string, mode: NameMode | "" | undefined): boolean {
  return mode === "serve" && name.trim().toLowerCase().startsWith("www.");
}

/** Kiểu còn lại của một tên phụ (nút "Đổi sang ..."). */
export function otherMode(mode: NameMode | "" | undefined): NameMode {
  return mode === "serve" ? "redirect" : "serve";
}

/** Một dòng của bảng "gõ địa chỉ nào thì ra gì": một tên miền, hoặc một đường dẫn được chuyển hướng. */
export type AddressRow = { key: string; kind: "name"; name: NameView } | { key: string; kind: "path"; rule: RedirectView };

/**
 * Gộp tên miền và đường dẫn chuyển hướng thành một bảng (v0.2.14). Thứ tự: tên
 * chính trước, rồi các tên khác theo thứ tự máy chủ trả, cuối cùng là các đường
 * dẫn. Khoá của dòng có tiền tố theo loại: một tên miền và một đường dẫn không
 * bao giờ trùng khoá.
 */
export function addressRows(names: NameView[], rules: RedirectView[]): AddressRow[] {
  const byName = (n: NameView): AddressRow => ({ key: `name:${n.name}`, kind: "name", name: n });
  return [
    ...names.filter((n) => n.kind === "primary").map(byName),
    ...names.filter((n) => n.kind !== "primary").map(byName),
    ...rules.map((r): AddressRow => ({ key: `path:${r.id}`, kind: "path", rule: r })),
  ];
}
