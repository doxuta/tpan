// Phần không dính giao diện của màn Stack: khoá truy vấn và hai việc làm được
// trên bảng bản PHP (cài, gỡ). Cùng một nhịp với màn Cập nhật, SSL, Bảo mật:
// bấm → POST ?dry_run=1 trả Preview → PlanReview → POST thật trả job_id.

import type { PHPVersionBody } from "../../api/types";

export const stackKey = ["stack"] as const;
// Trang Site cũng đọc danh sách này (khối PHP) nên khoá nằm ở đây để hai nơi dùng chung một bản đệm.
export const phpKey = ["php"] as const;

export type Action = {
  /** Dùng làm id nút ("act-<id>") để trả focus về nút khi huỷ bản xem trước. */
  id: string;
  kind: "install" | "remove";
  path: string;
  body: PHPVersionBody;
};

export const installAction = (version: string): Action => ({ id: `install-${version}`, kind: "install", path: "/api/v1/php/install", body: { version } });
export const removeAction = (version: string): Action => ({ id: `remove-${version}`, kind: "remove", path: "/api/v1/php/remove", body: { version } });

/**
 * Nguồn gói hiển thị cho người đọc: "ubuntu" và "sury" có tên thường ngày, các
 * nguồn còn lại (nginx.org, mariadb.org...) là tên kho nên giữ nguyên.
 */
export function sourceName(source: string, names: { ubuntu: string; sury: string }): string {
  if (source === "ubuntu") return names.ubuntu;
  if (source === "sury") return names.sury;
  return source;
}
