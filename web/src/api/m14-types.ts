// Kiểu của API M14 (mục 6.13 của KE-HOACH.md): cô lập sâu giữa các site và
// định tuyến có bằng chứng. Cùng nghĩa với internal/server/types_m14.go và
// internal/core/types_m14.go; đổi một bên thì đổi cả bên kia. Tách khỏi
// types.ts để các mốc chạy song song không sửa chung cuối một file.
//
//   GET  /api/v1/sites/{domain}/route              → RouteTrace
//   POST /api/v1/sites/{domain}/htaccess[?dry_run=1]   HtaccessBody → Preview | JobStarted
//   GET  /api/v1/security/isolation                → Isolation        (chỉ admin)
//   POST /api/v1/security/harden[?dry_run=1]       → HardenPreview | JobStarted (chỉ admin)

import type { CheckResult, Evidence, Preview, Status, Text, WebServer } from "./types";

/** Chế độ .htaccess của site trên Apache. "day-du" chỉ admin đặt được. */
export type HtaccessMode = "an-toan" | "tat" | "day-du";

export type HtaccessBody = { mode: HtaccessMode };

/** Một chỉ thị ngoài danh sách cho phép trong một file .htaccess. */
export type HtaccessFinding = { file: string; line: number; directive: string };

export type HtaccessKept = { domain: string; findings: HtaccessFinding[] | null };

/** Bản xem trước của "Áp bản vá cô lập": Plan cộng site nào đổi chế độ, site nào giữ day-du. */
export type HardenPreview = Preview & {
  switched: string[] | null;
  kept: HtaccessKept[] | null;
};

export type RouteHopKey = "dns" | "vhost" | "root" | "socket" | "pool" | "user" | "open_basedir" | "http";

/** Một mắt xích của đường đi request, kèm lệnh đã chạy và output thật. */
export type RouteHop = {
  key: RouteHopKey;
  title: Text;
  /** Điều tìm được ở chặng này (đường dẫn, file:dòng, tên user); rỗng khi không tìm được. */
  value: string;
  status: Status;
  summary: Text;
  evidence: Evidence[] | null;
};

export type RouteTrace = {
  domain: string;
  web: WebServer;
  hops: RouteHop[] | null;
};

/** Một ô của ma trận cô lập: phép kiểm "isolation:<from>:<to>" gần nhất; chưa chạy thì không có result. */
export type IsolationCell = { from: string; to: string; result?: CheckResult };

export type HtaccessRow = { domain: string; mode: HtaccessMode; result?: CheckResult };

/** Khối "Cô lập giữa các site" của màn Bảo mật. */
export type Isolation = {
  web: WebServer;
  sites: string[] | null;
  cells: IsolationCell[] | null;
  /** php.opcache:*, php.protectproc:*, web.server_name.unique, web.default.rejects */
  machine: CheckResult[] | null;
  /** log.perm:*, web.hardened:* của từng site */
  per_site: CheckResult[] | null;
  htaccess: HtaccessRow[] | null;
  /** Có phép kiểm không đạt mà "Áp bản vá cô lập" sửa được. */
  needs_harden: boolean;
};
