// Phần không dính giao diện của màn Cơ sở dữ liệu: khoá truy vấn và đường dẫn
// API của ba thao tác (khớp internal/server/types_m9.go).
//
// Danh sách bản xuất dùng lại backupsKey và backupsPath của màn Backup (import,
// không sửa file đó). Khoá của một site là [...backupsKey, domain] nên khi
// màn invalidate backupsKey sau job, khoá này cũng được tính lại.

import { dbPath, dbSubPath, siteDbCharsetPath, siteDbSuspendPath } from "../../api/m13-dbs";
import type { Backup, DatabaseRow, DBCharset, DBEdit, DBImportRequest } from "../../api/types";
import type { Op } from "../../components/PreviewedOp";
import type { strings } from "../../i18n/co-so-du-lieu";
import { backupsKey, backupsPath } from "../backup/model";

export const databasesKey = ["databases"] as const;
export const databasesPath = "/api/v1/databases";

export type DbOp = "password" | "dump" | "import";

/**
 * Việc trên một hàng của bảng. "dump" chỉ có với DB của site (định danh bằng domain); DB rời có
 * "backup" (M33: sao lưu ra một bản .sql). "import" có ở cả hai: nhập một bản đã xuất hay đã sao lưu.
 */
export type RowAction = DbOp | "backup" | "charset" | "suspend" | "unsuspend" | "trash" | "trash-site";

type T = (typeof strings)["vi"];

/**
 * Dựng việc (đường dẫn, thân, lời cảnh báo) cho một nút trên một hàng. DB của
 * site gọi theo domain (/databases/{domain}/...), DB rời theo tên DB (/dbs/{name}/...):
 * hai họ đường dẫn của máy chủ (internal/server/types_m13.go) nên chọn ở đúng một chỗ này.
 */
export function rowOp(t: T, action: RowAction, r: DatabaseRow, extra: { backup?: Backup; when?: string; charset?: string; collation?: string } = {}): Op {
  const site = r.kind === "site";
  const id = `${action}:${r.db}`;
  switch (action) {
    case "password":
      return {
        id,
        method: "POST",
        path: site ? dbOpPath(r.domain, "password") : dbSubPath(r.db, "password"),
        // M29: site PHP trống không có wp-config.php để TPAN ghi mật khẩu vào; mật khẩu mới hiện một lần như DB rời.
        warning: site && r.app !== "php" ? t.passwordWarning(r.db, r.domain) : t.passwordDbWarning(r.db),
        errorHeading: site ? t.passwordErrorHeading(r.domain) : t.passwordDbErrorHeading(r.db),
      };
    case "dump":
      return { id, method: "POST", path: dbOpPath(r.domain, "dump"), warning: t.dumpWarning(r.db), errorHeading: t.dumpErrorHeading(r.domain) };
    case "backup":
      return { id, method: "POST", path: dbSubPath(r.db, "backups"), warning: t.backupDbWarning(r.db), errorHeading: t.backupDbErrorHeading(r.db) };
    case "import": {
      const body: DBImportRequest = { backup_id: extra.backup?.id ?? "" };
      if (!site) return { id, method: "POST", path: dbSubPath(r.db, "import"), body, warning: t.importDbWarning(r.db, extra.when ?? ""), errorHeading: t.importDbErrorHeading(r.db) };
      return { id, method: "POST", path: dbOpPath(r.domain, "import"), body, warning: t.importWarning(r.domain, extra.when ?? ""), errorHeading: t.importErrorHeading(r.domain) };
    }
    case "charset": {
      const cs = extra.charset ?? "";
      const co = extra.collation ?? "";
      const body: DBCharset | DBEdit = { charset: cs, collation: co };
      return {
        id,
        method: "POST",
        path: site ? siteDbCharsetPath(r.domain) : dbPath(r.db),
        body,
        warning: t.charsetWarning(r.db, cs, co),
        errorHeading: t.charsetErrorHeading(r.db),
      };
    }
    case "suspend":
    case "unsuspend": {
      const on = action === "suspend";
      return {
        id,
        method: "POST",
        path: site ? siteDbSuspendPath(r.domain, on) : dbSubPath(r.db, action),
        warning: on ? (site ? t.suspendSiteDbWarning(r.domain, r.db) : t.suspendDbWarning(r.db)) : t.unsuspendDbWarning(r.db),
        errorHeading: on ? t.suspendErrorHeading(r.db) : t.unsuspendErrorHeading(r.db),
      };
    }
    case "trash":
      return { id, method: "DELETE", path: dbPath(r.db), warning: t.trashDbWarning(r.db), errorHeading: t.trashErrorHeading(r.db) };
    case "trash-site":
      // DB đi kèm site không xoá riêng được: xoá là đưa cả site vào thùng rác (DELETE /sites/{domain}).
      return { id, method: "DELETE", path: `/api/v1/sites/${encodeURIComponent(r.domain)}`, warning: t.trashSiteWarning(r.db, r.domain), errorHeading: t.trashSiteErrorHeading(r.domain) };
  }
}

export const dbOpPath = (domain: string, op: DbOp) => `${databasesPath}/${encodeURIComponent(domain)}/${op}`;

export const siteBackupsKey = (domain: string) => [...backupsKey, domain] as const;
export const siteBackupsPath = (domain: string) => `${backupsPath}?domain=${encodeURIComponent(domain)}`;
