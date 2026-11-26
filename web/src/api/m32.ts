// M32 (v0.2.14): quản lý dữ liệu của một DB. Cùng nghĩa với internal/core/dbdata.go
// và internal/server/api_m32.go; đổi một bên thì đổi cả bên kia.
//
//   DB của site:  /api/v1/databases/{domain}/...
//   DB rời:       /api/v1/dbs/{name}/...
//
//   GET  .../data                          → DBInfo
//   GET  .../data/{table}?offset&limit&order&dir → DBRowsPage
//   GET  .../data/{table}?row=1&k.<cột>=<giá trị> → DBRowsPage (một dòng, ô không cắt)
//   POST .../data/{table}                  DBRowChange → DBChanged
//   POST .../data/{table}/maintain         { op } → DBSQLResult
//   POST .../sql                           { sql } → DBSQLResult

export type DBKind = "site" | "standalone";

export type DBTable = { name: string; engine: string; rows: number; bytes: number; collation: string; view: boolean };

export type DBInfo = {
  db: string;
  user: string;
  kind: DBKind;
  domain?: string;
  owner: string;
  suspended: boolean;
  conn: { host: string; port: number; socket: string };
  tables: DBTable[];
  max_rows: number;
  max_seconds: number;
};

export type DBColumn = { name: string; type: string; nullable: boolean; key: string; default: string | null; extra: string; binary: boolean };

export type DBCell = string | null;

export type DBRowsPage = {
  table: string;
  columns: DBColumn[];
  primary: string[];
  rows: DBCell[][];
  clipped: boolean[][];
  offset: number;
  limit: number;
  more: boolean;
};

export type DBRowChange = { op: "insert" | "update" | "delete"; key?: Record<string, DBCell>; values?: Record<string, DBCell> };

export type DBChanged = { affected: number };

export type DBToolOp = "check" | "repair" | "optimize" | "analyze";

export type DBSQLResult = { columns: string[]; rows: DBCell[][]; affected: number; truncated: boolean; error?: string; ms: number };

/** Gốc đường dẫn API của một DB: theo tên miền (DB của site) hoặc theo tên DB (DB rời). */
export function dbDataBase(kind: DBKind, id: string): string {
  return kind === "site" ? `/api/v1/databases/${encodeURIComponent(id)}` : `/api/v1/dbs/${encodeURIComponent(id)}`;
}

/** Đường dẫn màn quản lý của một DB (routes.tsx). */
export function dbManagePath(kind: DBKind, id: string): string {
  return kind === "site" ? `/databases/site/${encodeURIComponent(id)}` : `/databases/db/${encodeURIComponent(id)}`;
}

export const dbInfoKey = (base: string) => ["dbdata", base] as const;
export const dbRowsKey = (base: string, table: string) => ["dbdata", base, "rows", table] as const;

/** Tham số ?row=1&k.<cột>=<giá trị> của một dòng theo khoá chính. */
export function rowKeyQuery(key: Record<string, DBCell>): string {
  const p = new URLSearchParams({ row: "1" });
  for (const [k, v] of Object.entries(key)) p.set(`k.${k}`, v ?? "");
  return p.toString();
}

/** Khoá chính của một dòng trong trang; null khi bảng không có khoá chính hoặc một cột khoá là NULL. */
export function rowKeyOf(page: DBRowsPage, row: DBCell[]): Record<string, string> | null {
  if (page.primary.length === 0) return null;
  const key: Record<string, string> = {};
  for (const name of page.primary) {
    const i = page.columns.findIndex((c) => c.name === name);
    const v = i < 0 ? null : row[i];
    if (v === null || v === undefined) return null;
    key[name] = v;
  }
  return key;
}

/** Chữ ngắn chỉ một dòng cho người đọc: "id = 5", "ma = A1, kho = 2". */
export function rowKeyText(key: Record<string, string>): string {
  return Object.entries(key)
    .map(([k, v]) => `${k} = ${v}`)
    .join(", ");
}

/**
 * Các ô đã đổi của form sửa: chỉ gửi ô khác giá trị cũ. edits[c] === undefined là
 * chưa đụng tới ô đó.
 */
export function changedValues(columns: DBColumn[], before: DBCell[], edits: Record<string, DBCell | undefined>): Record<string, DBCell> {
  const out: Record<string, DBCell> = {};
  columns.forEach((c, i) => {
    const v = edits[c.name];
    if (v !== undefined && v !== before[i]) out[c.name] = v;
  });
  return out;
}
