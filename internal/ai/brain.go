package ai

// Hợp đồng M8: bộ não (mục 8.2 của docs/KE-HOACH.md). Bộ não là thứ trả lời một
// lượt hỏi và phát sự kiện ra dần. Có ba bản:
//
//   - claudecode (internal/ai/claudecode): chạy `claude -p` thật trên máy
//     người vận hành, trong `tpan brain`;
//   - bridge.Hub (internal/ai/bridge): phía máy chủ, chuyển Job xuống `tpan
//     brain` qua socket nội bộ rồi nhận sự kiện về; panel chỉ thấy bản này;
//   - fake (internal/ai/fake): phát lại kịch bản soạn sẵn, cho test và --demo.

import (
	"context"
	"encoding/json"
	"time"
)

// Brain trả lời một lượt hỏi. Kênh sự kiện đóng sau đúng một sự kiện kết thúc
// (EventDone, EventError hoặc EventRefused). ctx bị huỷ thì bộ não dừng (giết
// claude) và kênh đóng sau một EventError. Lỗi trả ngay (không kênh) khi chưa
// bắt đầu được, ví dụ chưa có bộ não nào nối: ErrBrainOffline.
type Brain interface {
	Run(ctx context.Context, job Job) (<-chan Event, error)
}

// ErrBrainOffline: chưa có `tpan brain` nào nối vào panel.
var ErrBrainOffline = errorString("chưa có bộ não nào nối vào panel (chạy tpan brain <máy> trên máy của bạn)")

// ErrBrainBusy: bộ não đang trả lời một lượt khác (mỗi lúc một lượt).
var ErrBrainBusy = errorString("bộ não đang trả lời một câu hỏi khác")

type errorString string

func (e errorString) Error() string { return string(e) }

// Giới hạn của AI trong panel (mục 8.1, nguyên tắc 6).
const (
	// MaxTurns là --max-turns của claude: số vòng gọi công cụ trong một lượt.
	MaxTurns = 12
	// TurnTimeout: một lượt chạy quá lâu thì bị dừng.
	TurnTimeout = 5 * time.Minute
	// MaxQuestionsPerDay: trần số câu hỏi từ panel trong 24 giờ qua.
	MaxQuestionsPerDay = 50
	// DefaultModel: model mặc định, để đỡ tốn hạn mức (mục 8.2).
	DefaultModel = "sonnet"
)

// Job là một lượt hỏi gửi cho bộ não. Mọi chữ trong Job đã che: Job đi qua
// SSH tới máy người vận hành rồi tới Anthropic.
type Job struct {
	// ID là "<SessionID>-<Seq>", duy nhất; sự kiện về mang lại ID này.
	ID string `json:"id"`
	// SessionID là ai_sessions.id: claude gọi `tpan mcp --session <SessionID>`.
	SessionID string `json:"session_id"`
	// BrainSession là UUID của phiên Claude Code (ai_sessions.brain_session).
	BrainSession string `json:"brain_session"`
	// Resume: lượt thứ hai trở đi của phiên, chạy --resume thay cho --session-id.
	Resume bool `json:"resume"`
	// Prompt là toàn văn đưa vào stdin của claude (ngữ cảnh, bản hướng dẫn,
	// câu hỏi), đã che.
	Prompt string `json:"prompt"`
	// Lang là ngôn ngữ giao diện ("vi" | "en"): AI trả lời bằng ngôn ngữ này.
	Lang string `json:"lang"`
}

// EventKind là loại sự kiện của một lượt.
type EventKind string

const (
	// EventInit: claude đã khởi động và qua cửa kiểm (Version, Model).
	EventInit EventKind = "init"
	// EventText: thêm chữ của câu trả lời (Text là phần mới, chưa nối).
	EventText EventKind = "text"
	// EventToolCall: AI gọi một công cụ (Tool là tên đầy đủ, ví dụ
	// mcp__tpan__site_health; Input là đối số JSON, dạng bí danh).
	EventToolCall EventKind = "tool_call"
	// EventToolResult: kết quả của công cụ vừa gọi (Text là kết quả rút gọn
	// tối đa 2000 ký tự; IsError; PlanID khi công cụ lập Plan trả plan_id).
	EventToolResult EventKind = "tool_result"
	// EventDone: xong. Text là trường result của claude, tức CHỈ tin nhắn cuối
	// (đo ở lần gọi thật #4); câu trả lời đầy đủ là các EventText nối lại, và
	// Panel dùng phần đó.
	EventDone EventKind = "done"
	// EventError: hỏng giữa chừng (claude thoát lỗi, hết giờ, mất kết nối).
	EventError EventKind = "error"
	// EventRefused: cửa kiểm sự kiện init từ chối phiên. Text nêu đúng thứ
	// thừa (công cụ, MCP server, plugin...). Không câu trả lời nào được hiện.
	EventRefused EventKind = "refused"
)

// Event là một sự kiện của một lượt. Chữ trong Event ở dạng bí danh (đúng như
// AI thấy và viết); panel đổi về tên thật ngay trước khi hiển thị.
type Event struct {
	Kind    EventKind       `json:"kind"`
	Time    time.Time       `json:"time"`
	Text    string          `json:"text,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	PlanID  string          `json:"plan_id,omitempty"`
	// Version, Model: chỉ ở EventInit (phiên bản Claude Code và model thật).
	Version string `json:"version,omitempty"`
	Model   string `json:"model,omitempty"`
}

// Final cho biết e có phải sự kiện kết thúc của lượt không.
func (e Event) Final() bool {
	return e.Kind == EventDone || e.Kind == EventError || e.Kind == EventRefused
}
