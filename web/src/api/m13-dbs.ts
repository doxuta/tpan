// API của M13 cho site, DB, thùng rác, tạm khoá (mục 6.12 của KE-HOACH.md;
// khớp internal/server/types_m13.go). Đường dẫn và hook nằm ở đây thay vì
// trong queries.ts để thợ giao diện không sửa chung file với nhau.

import { useQuery } from "@tanstack/react-query";
import { get } from "./client";
import { useSession } from "./queries";
import type { Collation } from "./types";

// ---- người đang đăng nhập ----

/**
 * Vai của người đang đăng nhập, đọc từ GET /api/v1/session (cùng khoá với khung
 * trang, không gọi thêm). Giao diện chỉ dùng vai để ẩn nút và cột mà user thường
 * không dùng được; ai được làm gì là do máy chủ quyết (luật của từng route), nên
 * đoán sai vai cũng chỉ làm nút hiện thừa chứ không mở thêm quyền.
 */
export function useIdentity(): { name: string; isAdmin: boolean } {
  const user = useSession().data?.user;
  return { name: user?.name ?? "", isAdmin: user?.role === "admin" };
}

// ---- đường dẫn ----

const enc = encodeURIComponent;

export const collationsPath = "/api/v1/collations";
export const dbsPath = "/api/v1/dbs";

/** Tạm khoá hay mở khoá một site. */
export const siteSuspendPath = (domain: string, on: boolean) => `/api/v1/sites/${enc(domain)}/${on ? "suspend" : "unsuspend"}`;
/** Tạm khoá hay mở khoá user DB của một site (DB đi kèm site, định danh bằng domain). */
export const siteDbSuspendPath = (domain: string, on: boolean) => `/api/v1/databases/${enc(domain)}/${on ? "suspend" : "unsuspend"}`;
export const siteDbCharsetPath = (domain: string) => `/api/v1/databases/${enc(domain)}/charset`;

/** DB rời, định danh bằng tên DB. */
export const dbPath = (name: string) => `${dbsPath}/${enc(name)}`;
export const dbSubPath = (name: string, sub: "password" | "restore" | "suspend" | "unsuspend") => `${dbPath(name)}/${sub}`;
/** Xoá hẳn một DB rời đang trong thùng rác. */
export const trashDbPath = (name: string) => `/api/v1/trash/dbs/${enc(name)}`;

// ---- truy vấn ----

export const collationsKey = ["collations"] as const;

/** Các cặp charset, collation MariaDB nhận (information_schema.COLLATIONS). Đổi hiếm khi nên đọc một lần. */
export function useCollations(enabled = true) {
  return useQuery({ queryKey: collationsKey, queryFn: () => get<Collation[]>(collationsPath), enabled, staleTime: 5 * 60_000 });
}

// Danh sách user cho ô chọn chủ lấy từ useUsers(enabled) của ./m13-users: một hook
// một khoá ["users"], để màn Người dùng và ô chọn chủ dùng chung một bản trong cache.
