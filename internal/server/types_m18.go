package server

// Hợp đồng API của M18 (mục 3.11 của docs/thay-doi/M18-thiet-ke.md). Thân ở
// api_m18.go (thợ đợt 3). Mọi route mới là luật admin; khai trong routes.go.
//
//	  GET    /api/v1/backups/target                    → core.TargetView
//	  POST   /api/v1/backups/target[?dry_run=1]        TargetRequest → Preview | JobStarted   (backup.target.set)
//	  DELETE /api/v1/backups/target[?dry_run=1]        → Preview | JobStarted                 (backup.target.rm)
//	  POST   /api/v1/backups/target/test               → JobStarted                           (backup.target.test)
//	  POST   /api/v1/backups/{id}/push[?dry_run=1]     → Preview | 202 QueuedResponse
//	  POST   /api/v1/backups/{id}/pull[?dry_run=1]     → Preview | 202 QueuedResponse
//	  GET    /api/v1/backups/remote                    → core.RemoteListing
//	  POST   /api/v1/cloud-init/render                 CloudInitRequest → CloudInitResponse
//
// Không có route nào nhận hay trả khoá sao lưu (S5 của bản soát). Đặt đích qua
// API không bao giờ sinh khoá sao lưu.
//
// Đọc thân: TargetRequest bằng apiReadJSONMax 128 KiB, CloudInitRequest 1,5
// MiB. secret_key đọc vào chuỗi rồi bọc system.Secret ngay trong handler; thân
// request không bao giờ vào nhật ký. Nhật ký ghi target là "<endpoint> <bucket>
// <prefix>" đã qua kiểm, hoặc "?" khi chưa qua kiểm.
//
// Mã HTTP (apiErrorStatus): s3_endpoint_invalid, s3_region_invalid,
// s3_bucket_invalid, s3_prefix_invalid, s3_keys_invalid, s3_keep_invalid,
// s3_ca_invalid, cloudinit_invalid, cloudinit_too_large: 400; còn lại 409.

import (
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
)

// TargetRequest là thân của POST /api/v1/backups/target. AccessKey,
// SecretKey rỗng: giữ hai khoá đang lưu. CAPEM: "" giữ CA đang có, "none" bỏ
// CA riêng, PEM thì đặt CA mới.
type TargetRequest struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	PathStyle *bool  `json:"path_style"` // nil: true (path-style mặc định)
	Keep      int    `json:"keep"`
	CAPEM     string `json:"ca_pem"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// QueuedResponse là câu trả lời 202 của push, pull: việc đã vào hàng chờ của
// bộ đẩy nền (mục 3.5); hàng chờ mất nếu panel khởi động lại trước khi chạy.
type QueuedResponse struct {
	Queued bool      `json:"queued"`
	Note   i18n.Text `json:"note"`
}

// CloudInitRequest là thân của POST /api/v1/cloud-init/render (chỉ chế độ
// site mới). SHA256AMD64, SHA256ARM64 bắt buộc: server không tải gì từ Base.
type CloudInitRequest struct {
	Web         string          `json:"web"`
	Private     bool            `json:"private"`
	Base        string          `json:"base"`
	SHA256AMD64 string          `json:"sha256_amd64"`
	SHA256ARM64 string          `json:"sha256_arm64"`
	MaxBytes    int             `json:"max_bytes"`
	Sites       *core.SitesFile `json:"sites"`
}

// CloudInitResponse là câu trả lời của render. AdminPassword có đúng một lần
// ở đây (bản rõ, không phải Secret): người gọi lưu ngay.
type CloudInitResponse struct {
	UserData      string `json:"user_data"`
	Bytes         int    `json:"bytes"`
	AdminPassword string `json:"admin_password"`
}
