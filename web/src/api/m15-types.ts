// Kiểu của API M15 (mục 6.14 của KE-HOACH.md): nút cập nhật TPAN trong panel.
// Cùng nghĩa với internal/server/types_m15.go và internal/core/types_m15.go;
// đổi một bên thì đổi cả bên kia.
//
//   GET  /api/v1/self                           → SelfState            (chỉ admin)
//   POST /api/v1/self/update?dry_run=1[&auto=1] → SelfState            ("Kiểm bản mới")
//   POST /api/v1/self/update                    → 202 SelfStarted
//   POST /api/v1/self/rollback?dry_run=1        → SelfRollbackPreview
//   POST /api/v1/self/rollback                  → 202 SelfStarted
//
// Không route nào nhận thân request: trình duyệt không gửi URL hay phiên bản.
// Lời từ chối là 409 với mã: job_running, self_update_running, self_no_old,
// self_rollback_db, self_url_invalid, self_start_failed, demo_no_self_update.

import type { SelfStatus, Text } from "./types";

export const selfPath = "/api/v1/self";
export const selfUpdatePath = "/api/v1/self/update";
export const selfRollbackPath = "/api/v1/self/rollback";

/** Tên đơn vị systemd tạm chạy lượt cập nhật: `journalctl -u tpan-self-update`. */
export const selfUnit = "tpan-self-update";

export type SelfAction = "update" | "rollback";

/**
 * running: đơn vị tạm chưa ghi kết quả. ok: file chạy đã là bản khác. same: bản
 * tải về trùng bản đang chạy, không thay gì. failed: hỏng, file cũ còn nguyên,
 * lý do ở error. interrupted: đơn vị tạm không còn chạy mà chưa ghi kết quả.
 */
export type SelfRunStatus = "running" | "ok" | "same" | "failed" | "interrupted";

/** Một lượt cập nhật hoặc quay về chạy từ panel. */
export type SelfRun = {
  action: SelfAction;
  status: SelfRunStatus;
  /** Bản đang chạy lúc bấm. */
  from: string;
  /** Bản nằm ở file chạy sau lượt chạy; vắng khi còn đang chạy. */
  to?: string;
  /** Lỗi của job, nguyên văn của máy chủ (tiếng Việt); chỉ có khi failed. */
  error?: string;
  /** Job trong Lịch sử (/jobs/:id); vắng khi bị từ chối trước khi thành job. */
  job_id?: string;
  actor: string;
  ip: string;
  started: string;
  finished?: string;
};

/** Kết quả "Kiểm bản mới" và lúc kiểm. */
export type SelfCheck = {
  current: string;
  /** Tên bản ở nguồn tải; rỗng khi nguồn tải không có file VERSION (chỉ biết là có bản khác). */
  latest: string;
  url: string;
  /** File sẽ tải ở nguồn, ví dụ tpan-linux-amd64. */
  asset: string;
  /** sha256 mà file tải về phải khớp (lấy từ SHA256SUMS của nguồn tải). */
  sha256: string;
  /** true: đang là bản mới nhất, không đưa nút chạy. */
  up_to_date: boolean;
  checked_at: string;
  /** Có: không hỏi được nguồn tải; khi đó chỉ current và url có nghĩa. */
  error?: Text;
};

/** GET /api/v1/self, và câu trả lời của "Kiểm bản mới". */
export type SelfState = SelfStatus & {
  /** Đơn vị tạm đang chạy (có thể do trình duyệt khác bấm). */
  busy: boolean;
  last?: SelfRun;
  /** Lần kiểm gần nhất máy chủ còn nhớ; vắng khi chưa kiểm từ lúc panel khởi động. */
  check?: SelfCheck;
};

export type SelfStarted = { run: SelfRun };

/** Bản xem trước của "Quay về bản trước". */
export type SelfRollbackPreview = { current: string; old: string };
