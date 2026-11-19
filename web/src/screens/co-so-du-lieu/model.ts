// Phần không dính giao diện của màn Cơ sở dữ liệu: khoá truy vấn và đường dẫn
// API của ba thao tác (khớp internal/server/types_m9.go).
//
// Danh sách bản xuất dùng lại backupsKey và backupsPath của màn Backup (import,
// không sửa file đó). Khoá của một site là [...backupsKey, domain] nên khi
// màn invalidate backupsKey sau job, khoá này cũng được tính lại.

import { backupsKey, backupsPath } from "../backup/model";

export const databasesKey = ["databases"] as const;
export const databasesPath = "/api/v1/databases";

export type DbOp = "password" | "dump" | "import";

export const dbOpPath = (domain: string, op: DbOp) => `${databasesPath}/${encodeURIComponent(domain)}/${op}`;

export const siteBackupsKey = (domain: string) => [...backupsKey, domain] as const;
export const siteBackupsPath = (domain: string) => `${backupsPath}?domain=${encodeURIComponent(domain)}`;
