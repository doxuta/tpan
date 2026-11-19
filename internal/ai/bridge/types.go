// Package bridge nối bộ não ở máy người vận hành với panel (mục 8.4 của
// docs/KE-HOACH.md):
//
//	tpan serve (Hub, nghe /run/tpan/brain.sock, 0600, chỉ root)
//	    ▲  socket nội bộ
//	tpan brain-link (Link: chép stdin/stdout ⇄ socket; chạy qua SSH bằng root)
//	    ▲  ssh <máy> sudo -n tpan brain-link
//	tpan brain <máy> (Client, trên máy cá nhân: chạy claudecode.Brain)
//
// Giao thức: mỗi dòng một Msg JSON (đọc bằng json.Decoder, không giới hạn độ
// dài dòng: kết quả công cụ có thể dài). Thứ tự:
//
//  1. Client gửi {"type":"hello","hello":{...}}.
//  2. Hub trả {"type":"welcome"}; hoặc {"type":"refused","text":"..."} rồi
//     đóng khi đã có một bộ não khác nối (text nêu Host của bộ não đang nối).
//  3. Hub gửi {"type":"job","job":{...}}; Client chạy rồi gửi lần lượt
//     {"type":"event","job_id":"...","event":{...}} tới sự kiện kết thúc.
//     Mỗi lúc một job. Hub có thể gửi {"type":"cancel","job_id":"..."}.
//  4. Kết nối đứt giữa job: Hub phát EventError "mất kết nối với bộ não" cho
//     job đó; Client giết claude.
//
// Hợp đồng M8. Thợ đợt 2 viết ở hub.go, link.go, client.go (cùng test, nối
// Hub và Client bằng net.Pipe hoặc socket trong t.TempDir()):
//
//	// Listen tạo thư mục cha (0700) nếu thiếu, xoá socket cũ còn sót, nghe
//	// unix socket ở path, chmod 0600, rồi nhận kết nối tới khi ctx huỷ.
//	func Listen(ctx context.Context, path string) (*Hub, error)
//	// Run là ai.Brain: gửi job cho bộ não đang nối. Chưa có: ai.ErrBrainOffline;
//	// đang có job: ai.ErrBrainBusy. ctx huỷ: gửi cancel, phát EventError.
//	func (h *Hub) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error)
//	// Status cho biết bộ não nào đang nối.
//	func (h *Hub) Status() Status
//	// Link chép in → socket và socket → out tới khi một bên đóng.
//	func Link(ctx context.Context, path string, in io.Reader, out io.Writer) error
//	// Serve (Client) gửi hello, chờ welcome (refused: trả lỗi kèm lời của Hub),
//	// rồi nhận job, chạy Brain, gửi sự kiện; in ra Log mỗi câu hỏi (phần
//	// <question> của Prompt) và mỗi lần gọi công cụ, để người vận hành thấy
//	// bộ não của mình đang được dùng vào việc gì. Trả về khi Conn đóng hoặc
//	// ctx huỷ.
//	func (c *Client) Serve(ctx context.Context) error
package bridge

import (
	"io"
	"time"

	"github.com/doxuta/tpan/internal/ai"
)

// SocketPath là socket nội bộ giữa tpan serve và tpan brain-link (mục 5).
const SocketPath = "/run/tpan/brain.sock"

// Các loại Msg.
const (
	MsgHello   = "hello"
	MsgWelcome = "welcome"
	MsgRefused = "refused"
	MsgJob     = "job"
	MsgCancel  = "cancel"
	MsgEvent   = "event"
)

// Msg là một dòng của giao thức.
type Msg struct {
	Type  string    `json:"type"`
	Hello *Hello    `json:"hello,omitempty"`
	Job   *ai.Job   `json:"job,omitempty"`
	JobID string    `json:"job_id,omitempty"`
	Event *ai.Event `json:"event,omitempty"`
	Text  string    `json:"text,omitempty"`
}

// Hello là lời giới thiệu của bộ não khi nối.
type Hello struct {
	// Host là tên máy cá nhân của người vận hành (hostname).
	Host          string `json:"host"`
	ClaudeVersion string `json:"claude_version"`
	TestedVersion string `json:"tested_version"`
	Model         string `json:"model"`
	// AuthMethod là trường authMethod của `claude auth status` (ví dụ
	// "claude.ai"; khoá API thì mỗi lượt hỏi tính tiền API, panel cảnh báo).
	AuthMethod string `json:"auth_method"`
}

// Status là tình trạng bộ não ở phía máy chủ.
type Status struct {
	Connected bool      `json:"connected"`
	Hello     Hello     `json:"hello"`
	Since     time.Time `json:"since"`
	// Busy: đang trả lời một job.
	Busy bool `json:"busy"`
}

// Hub là phía máy chủ: giữ đúng một bộ não đang nối (hub.go).
type Hub struct {
	st hubState
}

// Client là phía máy người vận hành (`tpan brain`).
type Client struct {
	// Conn là ống tới `tpan brain-link` (stdin, stdout của ssh).
	Conn  io.ReadWriteCloser
	Brain ai.Brain
	Hello Hello
	// Log nhận từng dòng để in ra terminal của tpan brain; nil thì bỏ.
	Log func(line string)
}
