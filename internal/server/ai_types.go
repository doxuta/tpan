package server

// Hợp đồng M8: API của AI trong panel (mục 8.4, 9 của docs/KE-HOACH.md). Kiểu
// TypeScript tương ứng ở web/src/api/types.ts (phần "AI trong panel").
//
// Mọi đường dẫn cần phiên đăng nhập:
//
//	GET  /api/v1/ai/brain                    → BrainStatus
//	POST /api/v1/ai/preview  (ai.AskInput)   → ai.AskPreview (không ghi gì)
//	POST /api/v1/ai/ask      (ai.AskInput)   → 202 ai.AskStarted; ghi audit "ai.ask"
//	GET  /api/v1/ai/sessions/{id}/turns/{seq}/events
//	                                         → SSE: mỗi sự kiện tên "ai", data là
//	                                           ai.Event hiển thị; sự kiện cuối có
//	                                           Final() thì đóng luồng
//	GET  /api/v1/ai/sessions?limit=N         → []store.AISessionSummary (N mặc định 50, tối đa 200)
//	GET  /api/v1/ai/sessions/{id}            → ai.SessionDetail (không có: 404 ai_session_not_found)
//	POST /api/v1/ai/views    (ai.View)       → 204; ghi một lần chuyển mục cho
//	                                           công cụ current_view
//
// Lời từ chối của Panel (*core.RefusedError) đi qua apiErrorStatus như mọi
// lời từ chối khác (409), trừ: question_empty, question_too_long → 400;
// ai_session_not_found → 404; ai_daily_limit → 429. Thêm khi làm (đợt 3):
// ai_turn_not_found (404, SSE của lượt không có); label_invalid, path_invalid
// (400, POST /ai/views). Lượt bị Recover đóng (serve chết giữa lượt) thì luồng
// SSE gửi một sự kiện error lấy từ lời lỗi đã lưu, để trình duyệt không nối
// lại mãi.
//
// Thợ đợt 3 thêm vào Deps hai trường dưới đây, viết api_ai.go (cùng test với
// fake.Brain) và nối trong `tpan serve`:
//
//	// AI là lõi AI trong panel; nil khi AI tắt (setting ai.brain = off).
//	AI *ai.Panel
//	// Brain cho biết tình trạng bộ não (bridge.Hub); nil khi tắt hoặc demo.
//	Brain interface{ Status() bridge.Status }

import (
	"time"

	"github.com/doxuta/tpan/internal/i18n"
)

// BrainStatus là GET /api/v1/ai/brain.
type BrainStatus struct {
	// Mode: "off" (AI tắt), "bridge" (chờ hoặc đang nối tpan brain), "demo"
	// (bộ não giả của --demo).
	Mode          string      `json:"mode"`
	Connected     bool        `json:"connected"`
	Host          string      `json:"host,omitempty"`
	ClaudeVersion string      `json:"claude_version,omitempty"`
	TestedVersion string      `json:"tested_version,omitempty"`
	Model         string      `json:"model,omitempty"`
	AuthMethod    string      `json:"auth_method,omitempty"`
	Warnings      []i18n.Text `json:"warnings,omitempty"`
	Since         *time.Time  `json:"since,omitempty"`
	// ConnectCommand là lệnh người vận hành gõ trên máy của mình để nối, ví dụ
	// "tpan brain ubuntu@<hostname>".
	ConnectCommand string `json:"connect_command"`
}
