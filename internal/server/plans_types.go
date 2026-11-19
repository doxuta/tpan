package server

// Hợp đồng M7: Plan chờ duyệt và cổng agent trên API (mục 4.2, 8.3, 9).
// Handler viết ở api_plans.go; đường dẫn thêm thành một khối ở NewHandler.
//
//	GET  /api/v1/plans[?status=pending|applied|rejected] → []engine.PendingPlan
//	                                       (mới nhất trước, tối đa 100; status lạ → 400 status_invalid)
//	GET  /api/v1/plans/{id}                → engine.PendingPlan          (404 plan_not_found)
//	POST /api/v1/plans/{id}/approve        → 202 JobStarted, chạy nền như tạo site
//	                                       (409 plan_changed: Plan đã được dựng lại với bản xem
//	                                        trước mới, GET lại để xem rồi duyệt lần nữa;
//	                                        409 plan_not_pending; 409 job_running; 404 plan_not_found)
//	                                       Plan tạo site thật sự cài WordPress thì mật khẩu quản trị
//	                                       hiện một lần ở GET /jobs/{id} như POST /sites.
//	POST /api/v1/plans/{id}/reject         → 204                          (409 plan_not_pending, 404)
//	GET  /api/v1/agent                     → AgentInfo
//
// Nhật ký: plan.approve, plan.reject (đối tượng là id của Plan). DecidedBy là
// "web:<ip>".

// AgentInfo cho mục "Agent và MCP" ở màn Cài đặt (chỉ xem; đổi bằng
// `sudo tpan mcp config` trên máy chủ, vì bật chế độ client là cho agent tự
// chạy Plan).
type AgentInfo struct {
	Approval string `json:"approval"` // "panel" | "client"
	Redact   string `json:"redact"`   // "all" | "secrets"
	Hostname string `json:"hostname"`
	// Lệnh thêm MCP server tpan vào Claude Code và Codex trên máy cá nhân, qua
	// SSH. <user> để người dùng tự thay; user không phải root thì cần sudo
	// không hỏi mật khẩu cho tpan.
	// Ví dụ: claude mcp add --transport stdio tpan -- ssh <user>@<hostname> sudo -n tpan mcp
	ClaudeCommand string `json:"claude_command"`
	CodexCommand  string `json:"codex_command"`
}
