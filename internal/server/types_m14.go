package server

// Hợp đồng API của M14 (mục 6.13 của docs/KE-HOACH.md): cô lập sâu giữa các
// site và định tuyến có bằng chứng. Bản TypeScript cùng nghĩa nằm ở
// web/src/api/types.ts (khối "M14"); đổi một bên thì đổi cả bên kia. Quy ước
// lỗi, dry_run, JobStarted như M3 (types.go); luật route như M13 (routes.go).
//
//	GET  /api/v1/sites/{domain}/route             owner:site  → core.RouteTrace
//	POST /api/v1/sites/{domain}/htaccess[?dry_run=1]  owner:site  HtaccessBody → Preview | JobStarted
//	GET  /api/v1/security/isolation               admin       → core.Isolation
//	POST /api/v1/security/harden[?dry_run=1]      admin       → HardenPreview | JobStarted
//
// Ghi chú:
//   - /route chỉ đọc nhưng chạy lệnh trên máy (nginx -T, curl): không đệm.
//   - /htaccess: mode "day-du" chỉ admin đặt được; user thường (kể cả chủ của
//     site) nhận 403 forbidden. Hai chế độ còn lại theo luật owner:site. Mode
//     lạ: 400 htaccess_mode_invalid. Lời từ chối của core (htaccess_blocked,
//     htaccess_not_apache, htaccess_same, site_suspended) là 409 như mọi lời
//     từ chối khác. Audit: "site.htaccess", target là domain.
//   - /security/isolation dựng từ kết quả health đã lưu (bảng check_results),
//     không chạy lại phép kiểm; chưa có kết quả thì ô có result vắng mặt.
//   - /security/harden: audit "security.harden", target "isolation". Bản xem
//     trước kèm danh sách site sẽ chuyển sang an-toan và site giữ day-du.
//   - GET /api/v1/sites và /api/v1/sites/{domain}: site có thêm trường
//     "htaccess" (site.Site.Htaccess); layout có thêm "access_log", "error_log".

import (
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
)

// HtaccessBody là thân của POST /api/v1/sites/{domain}/htaccess.
type HtaccessBody struct {
	// Mode: "an-toan", "tat" hoặc "day-du" (site.HtaccessMode).
	Mode string `json:"mode"`
}

// HardenPreview là câu trả lời của POST /api/v1/security/harden?dry_run=1:
// bản xem trước như mọi Plan, cộng hai danh sách của core.Harden.
type HardenPreview struct {
	engine.Preview
	Switched []string            `json:"switched"`
	Kept     []core.HtaccessKept `json:"kept"`
}
