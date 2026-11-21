package server

// Hợp đồng API của M17 (lồng theo site, trần theo gói, số liệu, thử tấn công;
// mục 3.10 của docs/thay-doi/M17-thiet-ke.md). Quy ước lỗi, dry_run,
// JobStarted như M3 (types.go); luật route như M13 (routes.go). Giao diện của
// các API này làm ở phần F (sau M16): chưa có kiểu TypeScript.
//
//	GET  /api/v1/cage                               admin       → []core.CageRow
//	POST /api/v1/sites/{domain}/cage[?dry_run=1]    admin       CageBody → Preview | JobStarted
//	GET  /api/v1/usage                              any         → []core.SiteUsage (lọc theo chủ)
//	GET  /api/v1/sites/{domain}/usage[?range=]      owner:site  → SiteUsageDetail
//	GET  /api/v1/usage/suggestions                  admin       → core.SuggestionReport
//	GET  /api/v1/security/probe                     admin       → ProbeStatus
//	POST /api/v1/security/probe[?dry_run=1]         admin       ProbeBody → ProbePreview | JobStarted
//	POST /api/v1/security/probe/cleanup[?dry_run=1] admin       → Preview | JobStarted | 409 nothing_to_do
//
// Đổi ở route có sẵn (M13):
//   - POST /api/v1/packages/{name}: thân là PackageEdit (bốn giới hạn cũ như
//     trước; bảy trường mới tuỳ chọn: vắng mặt là giữ nguyên, để giao diện cũ
//     không xoá trần khi sửa gói). Có site trong lồng dùng gói và trần đổi:
//     202 JobStarted (job cage.sync, dòng gói chỉ ghi khi job thành công);
//     ?dry_run=1 trả Preview. Không: 200 PackageRow như trước.
//   - POST /api/v1/packages: thân account.Package như trước, nhận thêm bảy trường.
//   - POST /api/v1/users/{name}: đổi gói của user có site trong lồng mà trần
//     khác: 202 JobStarted (mọi thay đổi của thân áp trong after của job).
//   - GET /api/v1/sites, /sites/{domain}: site có thêm "caged".
// Audit: "cage.enable", "cage.disable" (target domain), "isolation.probe",
// "isolation.probe.cleanup" (target id lượt thử), "package.edit" như cũ.

import (
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
)

// CageBody là thân của POST /api/v1/sites/{domain}/cage.
type CageBody struct {
	Enabled bool `json:"enabled"`
}

// SiteUsageDetail là câu trả lời của GET /api/v1/sites/{domain}/usage: số đọc
// ngay từ cgroup cộng lịch sử theo range ("day" mặc định, "week", "month").
type SiteUsageDetail struct {
	core.SiteUsage
	History core.UsageHistory `json:"history"`
}

// ProbeBody là thân của POST /api/v1/security/probe.
type ProbeBody struct {
	Only string `json:"only"` // "", "ngoai", "trong"
}

// ProbePreview là bản xem trước của một lượt thử kèm hai tên miền sẽ tạo.
type ProbePreview struct {
	engine.Preview
	Run core.ProbeRunInfo `json:"run"`
}

// ProbeStatus là GET /api/v1/security/probe: lượt gần nhất đã xong (có thể
// nil) và các lượt chưa dọn.
type ProbeStatus struct {
	Last    *core.ProbeReport `json:"last"`
	Pending []cage.ProbeRun   `json:"pending"`
}

// PackageEdit là thân của POST /api/v1/packages/{name} (tên lấy từ đường dẫn).
type PackageEdit struct {
	MaxSites   int   `json:"max_sites"`
	MaxDBs     int   `json:"max_dbs"`
	DiskMB     int   `json:"disk_mb"`
	MaxBackups int   `json:"max_backups"`
	Cage       *bool `json:"cage,omitempty"`
	CPUPercent *int  `json:"cpu_percent,omitempty"`
	RAMMB      *int  `json:"ram_mb,omitempty"`
	Tasks      *int  `json:"tasks,omitempty"`
	IOReadMB   *int  `json:"io_read_mb,omitempty"`
	IOWriteMB  *int  `json:"io_write_mb,omitempty"`
	PHPWorkers *int  `json:"php_workers,omitempty"`
}
