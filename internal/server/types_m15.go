package server

// Hợp đồng API của M15 (mục 6.14 của docs/KE-HOACH.md): nút cập nhật TPAN
// trong panel. Bản TypeScript cùng nghĩa nằm ở web/src/api/m15-types.ts; đổi
// một bên thì đổi cả bên kia. Quy ước lỗi như M3 (types.go); luật route như
// M13 (routes.go): cả ba route (năm cách gọi dưới đây) chỉ admin, và mọi POST đi qua lớp chống CSRF
// chung (secure, auth.go).
//
//	GET  /api/v1/self                           admin → SelfState
//	POST /api/v1/self/update?dry_run=1[&auto=1] admin → SelfState (kèm check vừa làm)
//	POST /api/v1/self/update                    admin → 202 SelfStarted
//	POST /api/v1/self/rollback?dry_run=1        admin → core.SelfRollbackPreview
//	POST /api/v1/self/rollback                  admin → 202 SelfStarted
//
// KHÔNG route nào đọc thân request: nguồn tải là settings update.self_url hoặc
// hằng core.DefaultSelfURL, phiên bản là thứ nằm ở nguồn tải. Trình duyệt chỉ
// nói "kiểm", "chạy", "quay về".
//
// Ghi chú:
//   - GET /self không ra mạng và không chạy file cũ: đọc settings, kiểm file
//     .old có không, hỏi systemd đơn vị tạm có đang chạy không, kèm lần kiểm
//     gần nhất còn trong bộ nhớ của tiến trình (mất khi panel restart).
//   - dry_run=1 của /self/update là nút "Kiểm bản mới": gọi core.SelfUpdate.Check
//     (hai lệnh curl tới nguồn tải), nhớ kết quả, trả SelfState. Không hỏi được
//     nguồn tải vẫn là 200: lý do nằm ở check.error (giao diện hiện ngay dưới
//     nút). Thêm auto=1 (dòng phiên bản cuối trang tự gọi): lần kiểm gần nhất,
//     kể cả lần lỗi, chưa quá SelfCheckTTL thì trả lại nó, không ra mạng.
//     Lần kiểm chỉ được nhớ cho đúng nguồn tải nó đã hỏi: đổi update.self_url
//     thì coi như chưa kiểm.
//   - Chạy thật (/self/update, /self/rollback không có dry_run), theo thứ tự:
//     chế độ --demo thì 409 demo_no_self_update; tiến trình này đang có job
//     thì 409 job_running (job đó sẽ bị cắt khi panel restart); rồi
//     core.SelfUpdate.Start. Lời từ chối của core là 409: self_update_running,
//     self_no_old, self_rollback_db, self_url_invalid, self_start_failed.
//   - Audit: "self.update" | "self.rollback", target là bản đang chạy, result
//     "started" hoặc mã từ chối. Kết quả của lượt chạy do chính đơn vị tạm ghi
//     thành một dòng audit nữa (target "<bản cũ> -> <bản mới>").
//   - Sau 202, panel sẽ restart trong vài giây. Giao diện hỏi lại GET
//     /api/v1/session (version đổi là đã lên) và GET /api/v1/self (last.status
//     "failed" là hỏng, kèm lý do); mất kết nối trong lúc đó là bình thường.

import (
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
)

// SelfCheckTTL: với auto=1, lần kiểm bản mới chưa cũ quá chừng này thì dùng
// lại (dòng phiên bản cuối trang kiểm tối đa một lần mỗi 6 giờ).
const SelfCheckTTL = 6 * time.Hour

// selfInterruptAfter: lượt chạy còn ghi "running" mà đơn vị tạm không chạy
// quá chừng này thì GET /self trả "interrupted". Khoảng chờ để không báo nhầm
// trong tích tắc giữa lúc Start ghi settings và lúc systemd khởi xong đơn vị.
const selfInterruptAfter = 15 * time.Second

// SelfState là câu trả lời của GET /api/v1/self và của "Kiểm bản mới".
type SelfState struct {
	// version, url, has_old: như khối self của GET /api/v1/updates.
	core.SelfStatus
	// Busy: đơn vị tạm tpan-self-update đang chạy (ai đó vừa bấm, có thể ở
	// trình duyệt khác).
	Busy bool `json:"busy"`
	// Last là lượt chạy gần nhất từ panel; vắng khi chưa có lượt nào. Status
	// "running" mà đơn vị tạm không còn chạy quá selfInterruptAfter thì trả
	// "interrupted" (core.SelfInterrupted).
	Last *core.SelfRun `json:"last,omitempty"`
	// Check là lần "Kiểm bản mới" gần nhất còn nhớ; vắng khi chưa kiểm lần nào
	// từ lúc panel khởi động.
	Check *SelfCheckInfo `json:"check,omitempty"`
}

// SelfCheckInfo là một lần kiểm bản mới và lúc kiểm.
type SelfCheckInfo struct {
	core.SelfCheck
	CheckedAt time.Time `json:"checked_at"`
	// Error khác nil: không hỏi được nguồn tải (mất mạng, nguồn không có
	// SHA256SUMS). Khi đó chỉ current và url có nghĩa.
	Error *i18n.Text `json:"error,omitempty"`
}

// SelfStarted là câu trả lời 202 của /self/update và /self/rollback: lượt chạy
// vừa ghi (status "running").
type SelfStarted struct {
	Run core.SelfRun `json:"run"`
}
