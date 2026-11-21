// Kiểu của API M18 (mục 6.16 của KE-HOACH.md): sao lưu ra kho S3, khôi phục từ
// kho, sinh user-data cho máy mới. Cùng nghĩa với internal/server/types_m18.go
// và internal/core/types_m18.go; đổi một bên thì đổi cả bên kia. Mọi route
// dưới đây chỉ admin (user thường nhận 403).
//
//   GET    /api/v1/backups/target               → TargetView
//   POST   /api/v1/backups/target?dry_run=1     → Preview    (thân TargetRequest)
//   POST   /api/v1/backups/target               → 202 JobStarted
//   DELETE /api/v1/backups/target?dry_run=1     → Preview
//   DELETE /api/v1/backups/target               → 202 JobStarted
//   POST   /api/v1/backups/target/test          → 202 JobStarted
//   POST   /api/v1/backups/{id}/push?dry_run=1  → Preview
//   POST   /api/v1/backups/{id}/push            → 202 QueuedResponse (chạy ở lượt đẩy kế tiếp; không job_id)
//   POST   /api/v1/backups/{id}/pull?dry_run=1  → Preview
//   POST   /api/v1/backups/{id}/pull            → 202 QueuedResponse (thường có job_id)
//   GET    /api/v1/backups/remote               → RemoteListing
//   POST   /api/v1/cloud-init/render            → 200 CloudInitResponse (thân CloudInitRequest)
//
// Khôi phục giữ route của M9 (POST /api/v1/backups/{id}/restore): bản chỉ còn
// trên kho thì 409 backup_not_on_machine; tải về trước bằng .../pull.
//
// Lời từ chối mới (409 trừ khi ghi 400): s3_not_configured, s3_endpoint_invalid
// (400), s3_region_invalid (400), s3_bucket_invalid (400), s3_prefix_invalid
// (400), s3_keys_invalid (400), s3_keep_invalid (400), s3_ca_invalid (400),
// s3_unreachable, s3_auth_failed, s3_bucket_not_found, s3_error,
// backup_not_on_machine, backup_already_local, backup_key_missing,
// backup_outside_keep, cloudinit_invalid (400), cloudinit_too_large (400),
// job_running (khoá lượt đẩy bận quá 10 giây).
//
// BÍ MẬT: access_key, secret_key chỉ đi trong thân của POST target, không bao
// giờ quay lại trong câu trả lời nào. Giao diện không lưu chúng vào bộ nhớ
// đệm của TanStack Query, localStorage hay địa chỉ trang, và xoá ô nhập sau
// khi gửi. admin_password của cloud-init chỉ có một lần trong câu trả lời.

import type { Backup, SitesDefaults, SitesEntry, Text } from "./types";

/** core.SitesFile ở dạng JSON (cùng trường với sites.yaml). */
export type SitesFile = { defaults: SitesDefaults; sites: SitesEntry[] };

export const targetPath = "/api/v1/backups/target";
export const targetTestPath = "/api/v1/backups/target/test";
export const remoteListPath = "/api/v1/backups/remote";
export const cloudInitRenderPath = "/api/v1/cloud-init/render";
export const pushPath = (id: string) => `/api/v1/backups/${encodeURIComponent(id)}/push`;
export const pullPath = (id: string) => `/api/v1/backups/${encodeURIComponent(id)}/pull`;

/** core.PanelInfo: gói panel mới nhất đã lên kho. */
export type PanelInfo = { name: string; at: string; sha256: string };

/** core.TargetView: phần xem của đích. Không trường nào mang khoá. */
export type TargetView = {
  configured: boolean;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  path_style: boolean;
  keep?: number;
  target_id?: string;
  has_ca: boolean;
  /** Bốn ký tự cuối của access key (khi access key dài từ 12 ký tự). Rỗng thì hiện access_key_len. */
  access_key_tail?: string;
  access_key_len?: number;
  has_backup_key: boolean;
  /** Dấu vân tay của khoá sao lưu (8 ký tự hex). */
  key_fingerprint?: string;
  panel_last?: PanelInfo;
  panel_error?: string;
  panel_due: boolean;
  /** Lỗi gần nhất của đích (đã làm sạch); có thì lượt đẩy đang lùi tới retry_at. */
  target_error?: string;
  retry_at?: string;
  /** Số dòng: chỉ còn trên kho (đích này), stored ở đích khác, đẩy lỗi. */
  remote_only: number;
  other_target: number;
  failed: number;
};

/**
 * server.TargetRequest (POST /api/v1/backups/target). Hai khoá rỗng: giữ khoá
 * đang lưu, chỉ được khi endpoint, vùng, bucket, kiểu địa chỉ và CA không đổi
 * (không thì 400 s3_keys_invalid). ca_pem "": giữ CA đang có; "none": bỏ CA;
 * còn lại: nội dung PEM. path_style bỏ trống: true.
 */
export type TargetRequest = {
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  path_style?: boolean;
  keep: number;
  ca_pem: string;
  access_key: string;
  secret_key: string;
};

/** server.QueuedResponse: việc đã nhận, chạy ở nền. job_id có khi job đã bắt đầu (tải về). */
export type QueuedResponse = { queued: boolean; note: Text; job_id?: string };

/** core.RemoteObject: một object trên kho; backup_id rỗng là kho có mà tpan.db không biết. */
export type RemoteObject = { key: string; size: number; modified: string; backup_id?: string; domain?: string };

/** core.RemoteListing (GET /api/v1/backups/remote). */
export type RemoteListing = { objects: RemoteObject[]; missing: Backup[]; other_target: number };

/**
 * server.CloudInitRequest (POST /api/v1/cloud-init/render). sha256_amd64,
 * sha256_arm64 bắt buộc (64 hex): server không tải gì từ base. base rỗng: bản
 * phát hành của chính bản đang chạy. max_bytes 0: 16384. sites null: máy mới
 * không có site nào.
 */
export type CloudInitRequest = {
  web: "nginx" | "apache";
  private: boolean;
  base: string;
  sha256_amd64: string;
  sha256_arm64: string;
  max_bytes: number;
  sites: SitesFile | null;
};

/** server.CloudInitResponse: admin_password (32 ký tự) chỉ có trong câu trả lời này, một lần. */
export type CloudInitResponse = { user_data: string; bytes: number; admin_password: string };
