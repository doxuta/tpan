// Package claudecode là bộ não thật: chạy `claude -p` (Claude Code của người
// vận hành, đã đăng nhập sẵn bằng gói Claude của họ) trên máy cá nhân, chỉ nạp
// MCP server tpan, đọc luồng stream-json thành ai.Event (mục 8.2 của
// docs/KE-HOACH.md).
//
// Gói này KHÔNG đọc, lưu hay chuyển tiếp đăng nhập Claude (luật 9 của
// AGENTS.md): không mở ~/.claude, không đọc Keychain. Nó chỉ chạy lệnh
// `claude` với HOME của người vận hành để chính claude tìm đăng nhập của nó, và
// chỉ đọc hai trường loggedIn, authMethod của `claude auth status`.
//
// Hợp đồng M8. Thợ đợt 1 viết ở claudecode.go, gate.go, stream.go (cùng test;
// test chạy một `claude` giả là chính file test, qua system.LocalSystem với
// Cmd.Env, không import os/exec):
//
//	// New kiểm cfg (Claude, MCP.Command, WorkRoot khác rỗng; Model rỗng là
//	// ai.DefaultModel), tạo WorkRoot (0700) và ghi file system prompt
//	// (SystemPrompt) vào <WorkRoot>/system-prompt.md.
//	func New(cfg Config) (*Brain, error)
//	// Run: tạo thư mục làm việc RỖNG và ỔN ĐỊNH của phiên <WorkRoot>/<job.SessionID>
//	// (0700; Claude Code lưu phiên theo thư mục làm việc, nên --resume ở lượt
//	// sau chỉ tìm thấy phiên khi cùng thư mục), chạy cfg.Claude với Args,
//	// stdin là job.Prompt, Dir là thư mục đó, Env là cfg.Env; đọc từng dòng
//	// output (dòng không phải JSON thì bỏ: stdout và stderr đi chung OnOutput).
//	// Dòng đầu tiên là JSON phải là sự kiện init: Gate lỗi thì huỷ lệnh (giết
//	// claude) và phát EventRefused với lời của Gate, rồi đóng kênh; không câu
//	// trả lời nào được phát. Qua cửa thì phát EventInit rồi các sự kiện còn
//	// lại theo Parse. Lệnh xong mà chưa có sự kiện result: EventError (kèm vài
//	// dòng stderr cuối). Hết ai.TurnTimeout hoặc ctx huỷ: EventError.
//	func (b *Brain) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error)
//	// Args là argv (không gồm tên chương trình) cho job, đúng bảng cờ ở mục 8.2:
//	//   -p --output-format stream-json --verbose --include-partial-messages
//	//   --tools "" --strict-mcp-config --mcp-config <json> --allowedTools mcp__tpan
//	//   --permission-mode dontAsk --permission-prompts none
//	//   --setting-sources "" --disable-slash-commands
//	//   --append-system-prompt-file <promptFile> --max-turns <cfg.MaxTurns, trần ai.MaxTurns>
//	//   --model <cfg.Model> và --session-id <job.BrainSession> (lượt đầu)
//	//   hoặc --resume <job.BrainSession> (job.Resume).
//	// <json> = {"mcpServers":{"tpan":{"command":MCP.Command,"args":MCP.Args+["--session",job.SessionID]}}}
//	// (encoding/json, không ghép chuỗi). Không bao giờ có --bare (mục 8.2).
//	func Args(cfg Config, job ai.Job, promptFile string) []string
//	// Gate kiểm sự kiện init; nil là qua. Từ chối (lỗi nêu đúng từng thứ thừa)
//	// khi: một công cụ không bắt đầu bằng "mcp__tpan__" và không nằm trong
//	// AllowedBuiltinTools; MCP server khác "tpan", hoặc tpan không ở trạng thái
//	// "connected"; plugin có Path khác "builtin"; skills hay slash_commands
//	// không rỗng. Trường agents bỏ qua: không có công cụ nào để gọi agent.
//	func Gate(in Init) error
//	// Parse đổi một dòng stream-json (đã giải) thành 0..n ai.Event, dùng
//	// trạng thái của p (công cụ đang gọi theo tool_use id):
//	//   stream_event/content_block_delta text_delta → EventText (phần mới);
//	//   assistant: mỗi khối tool_use → EventToolCall (Tool, Input);
//	//   user: mỗi khối tool_result → EventToolResult (Text rút gọn 2000 ký tự,
//	//     IsError; PlanID nếu kết quả là JSON có "plan_id");
//	//   result: subtype success → EventDone (Text = trường result);
//	//     khác → EventError (Text nêu subtype và lỗi).
//	//   Loại khác (system status, rate_limit_event...) bỏ qua.
//	func (p *Parser) Line(line []byte) []ai.Event
//	// Version chạy `claude --version`, trả chuỗi phiên bản (ví dụ "2.1.328").
//	func Version(ctx context.Context, cfg Config) (string, error)
//	// Auth chạy `claude auth status --json` và CHỈ đọc loggedIn, authMethod.
//	// Không trả, không in, không lưu trường nào khác (email, tổ chức...).
//	func Auth(ctx context.Context, cfg Config) (AuthStatus, error)
package claudecode

import "github.com/doxuta/tpan/internal/system"

// TestedVersion là phiên bản Claude Code đã chạy bài kiểm ở mục 16 của
// KE-HOACH.md. `tpan brain` cảnh báo khi gặp bản khác (mục 8.2, "Không dùng
// --bare").
const TestedVersion = "2.1.328"

// AllowedBuiltinTools là công cụ có sẵn của Claude Code không tắt được bằng
// --tools "", lấy từ sự kiện init THẬT ghi ở docs/bang-chung/M8-claude-init.log
// (không viết theo phỏng đoán).
var AllowedBuiltinTools = []string{}

// Config cấu hình bộ não thật trên máy người vận hành.
type Config struct {
	// Sys chạy lệnh (system.NewLocal() trong tpan brain).
	Sys system.System
	// Claude là tên hoặc đường dẫn của lệnh claude ("claude": tìm theo PATH
	// trong Env).
	Claude string
	// Env truyền cho claude qua system.Cmd.Env: đúng HOME và PATH của tiến
	// trình tpan brain, không gì khác.
	Env []string
	// Model rỗng là ai.DefaultModel.
	Model string
	// MaxTurns là --max-turns; 0 hoặc lớn hơn ai.MaxTurns thì là ai.MaxTurns
	// (trần của mục 8.1 nguyên tắc 6, chỉ hạ được, không nâng được).
	MaxTurns int
	// MCP là lệnh mở MCP server của tpan, chưa có "--session <id>" (Run thêm
	// vào cuối). Ví dụ: ssh -o BatchMode=yes ubuntu@<máy> sudo -n tpan mcp.
	MCP MCPServer
	// WorkRoot là thư mục cha của thư mục làm việc của từng phiên.
	WorkRoot string
}

// MCPServer là một lệnh stdio (command + args), như trong --mcp-config.
type MCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Brain là bộ não thật. Mỗi lúc chạy một lượt (bridge.Client đảm bảo).
type Brain struct {
	cfg        Config
	promptFile string
}

// Init là phần của sự kiện init mà Gate cần.
type Init struct {
	Tools         []string    `json:"tools"`
	MCPServers    []MCPStatus `json:"mcp_servers"`
	Plugins       []Plugin    `json:"plugins"`
	Skills        []string    `json:"skills"`
	SlashCommands []string    `json:"slash_commands"`
	Model         string      `json:"model"`
	Version       string      `json:"claude_code_version"`
}

type MCPStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Plugin struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

// AuthStatus là hai trường duy nhất tpan đọc từ `claude auth status --json`.
type AuthStatus struct {
	LoggedIn   bool   `json:"loggedIn"`
	AuthMethod string `json:"authMethod"`
}

// Parser giữ trạng thái giữa các dòng của một lượt (claudecode.go).
type Parser struct {
	// tools: tool_use id → tên công cụ, để gắn tên vào tool_result.
	tools map[string]string
}
