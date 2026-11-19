package ai

// Hợp đồng M8: AI trong panel (mục 8.4 của docs/KE-HOACH.md). Panel là lõi
// chung của mọi lối vào trên giao diện (ngăn AI, ⌘K, "Giải thích", "Chẩn
// đoán"): dựng khối ngữ cảnh, che, gửi cho bộ não, nhận sự kiện, lưu, phát lại
// cho trình duyệt sau khi đổi bí danh về tên thật.
//
// Thợ đợt 2 viết ở panel.go (cùng test, dùng fake.Brain và store thật trên
// file tạm):
//
//	// Preview dựng ĐÚNG thứ sẽ gửi cho câu hỏi in, không ghi gì, không gửi gì
//	// (nguyên tắc 3: người thấy dữ liệu sẽ gửi trước khi bấm gửi). Phiên mới
//	// (in.SessionID rỗng) dùng bộ che mới; phiên cũ nạp bảng bí danh đã lưu.
//	// Bộ che Learn mọi site và Hostname (như tools.OpenSession) nên bí danh ở
//	// Preview trùng bí danh lúc Ask.
//	func (p *Panel) Preview(ctx context.Context, in AskInput) (AskPreview, error)
//	// Ask kiểm, dựng lại đúng như Preview, mở phiên (nếu mới) và một lượt
//	// running, gửi Job cho Brain rồi trả ngay; một goroutine nhận sự kiện,
//	// lưu lượt khi kết thúc và phát cho người đang xem (Subscribe).
//	// Từ chối (*core.RefusedError): ai_off (Brain nil), question_empty,
//	// question_too_long (> MaxQuestionLen rune), ai_session_not_found,
//	// ai_session_not_panel (phiên của agent nối trực tiếp), ai_turn_running
//	// (phiên đang có lượt chạy), ai_daily_limit (MaxQuestionsPerDay trong 24
//	// giờ qua), brain_offline (Brain trả ErrBrainOffline), brain_busy.
//	// Brain.Run lỗi thì lượt được ghi error, không để running.
//	func (p *Panel) Ask(ctx context.Context, in AskInput) (AskStarted, error)
//	// Subscribe trả các sự kiện hiển thị đã có của lượt (đã đổi về tên thật)
//	// và một kênh cho sự kiện mới; kênh đóng sau sự kiện kết thúc. Lượt đã xong
//	// thì đọc từ DB và kênh đã đóng sẵn. cancel nhả người xem. Không có lượt
//	// thì ok=false.
//	func (p *Panel) Subscribe(ctx context.Context, sessionID string, seq int) (past []Event, live <-chan Event, cancel func(), ok bool, err error)
//	// Session trả một phiên (panel hoặc mcp) cho trang Phiên AI: các lượt (câu
//	// hỏi và khối ngữ cảnh đúng như đã gửi, tức bản đã che; câu trả lời đã đổi
//	// về tên thật), các lần gọi công cụ (ai_calls, bản đã che), các Plan được
//	// đề xuất trong phiên. Không có: store.ErrNotFound.
//	func (p *Panel) Session(ctx context.Context, id string) (SessionDetail, error)
//	// Recover đánh dấu error mọi lượt còn running (gọi khi serve khởi động, vì
//	// tiến trình trước có thể chết giữa lượt).
//	func (p *Panel) Recover(ctx context.Context) error
//
// Sự kiện hiển thị (Subscribe, SessionDetail) khác sự kiện của Brain ở một chỗ:
// EventText và EventDone mang TOÀN BỘ câu trả lời tới lúc đó, đã đổi về tên
// thật (không phải phần mới). Lý do: một bí danh có thể bị cắt giữa hai phần
// ("site-a.exa" + "mple"), chỉ đổi được trên chữ đã nối. Trình duyệt thay chữ
// cũ bằng chữ mới. Bảng bí danh được đọc lại từ DB sau mỗi EventToolResult,
// vì tiến trình `tpan mcp` có thể vừa thêm bí danh mới.
//
// Prompt gửi đi (Job.Prompt) có dạng, mọi phần đã qua bộ che:
//
//	<tpan-context lang="vi">
//	[view] ...            (mỗi Chip một khối, theo thứ tự ChipKeys, bỏ khoá trong Omit)
//	[evidence] ...        nội dung do người lạ viết (output lệnh, thân trang,
//	                      dòng log) đã bọc bằng Untrusted
//	</tpan-context>
//	<tpan-playbook name="site">...</tpan-playbook>   (chỉ khi gửi kèm, xem dưới)
//	<question>...</question>
//
// Bản hướng dẫn (internal/ai/playbooks) của mục đang mở được gửi kèm câu hỏi
// ĐẦU TIÊN ở mục đó trong phiên: lượt trước của cùng phiên đã gửi bản cùng tên
// thì lượt này không gửi lại.

import (
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
)

// MaxQuestionLen là độ dài tối đa của câu hỏi (rune).
const MaxQuestionLen = 4000

// MaxTrail là số lần chuyển mục gần nhất gửi kèm (mục 8.4).
const MaxTrail = 10

// View là một mục của panel: đường dẫn và tên người đọc được.
type View struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// Các loại "đang chỉ vào cái gì" (Pointer.Kind) và Ref tương ứng.
const (
	PointCheck    = "check"    // Ref: id phép kiểm (check_results.id)
	PointIncident = "incident" // Ref: id sự cố
	PointJob      = "job"      // Ref: id job
	PointJobStep  = "job_step" // Ref: "<job id>/<step id>"
	PointLogLine  = "log_line" // Ref: "<domain>/<access|error|php>"; Text: dòng log
	PointPlan     = "plan"     // Ref: id Plan
	PointSite     = "site"     // Ref: domain
)

// Pointer là thứ người dùng vừa bấm "Hỏi AI", "Giải thích", "Chẩn đoán" trên đó.
// Bằng chứng đi kèm do máy chủ tự đọc theo Kind và Ref (không tin bằng chứng
// trình duyệt gửi lên); riêng dòng log lấy từ Text vì đó là thứ người đang nhìn.
type Pointer struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Label string `json:"label,omitempty"`
	Text  string `json:"text,omitempty"`
}

// Các khoá của khối ngữ cảnh, theo đúng thứ tự trong prompt (bảng ở mục 8.4).
const (
	ChipView     = "view"     // Đang ở mục nào
	ChipPointer  = "pointer"  // Đang chỉ vào cái gì
	ChipTrail    = "trail"    // Vừa đi qua những đâu
	ChipEvidence = "evidence" // Bằng chứng đi kèm
	ChipMachine  = "machine"  // Bối cảnh máy
)

// ChipKeys là thứ tự các khối trong prompt.
var ChipKeys = []string{ChipView, ChipPointer, ChipTrail, ChipEvidence, ChipMachine}

// AskInput là một câu hỏi từ panel.
type AskInput struct {
	// SessionID rỗng: phiên mới. Khác rỗng: hỏi tiếp trong phiên panel đó.
	SessionID string   `json:"session_id,omitempty"`
	Question  string   `json:"question"`
	Lang      string   `json:"lang"` // "vi" | "en"; lạ thì "vi"
	View      View     `json:"view"`
	Pointer   *Pointer `json:"pointer,omitempty"`
	// Trail: các mục vừa đi qua, cũ trước mới sau; chỉ giữ MaxTrail mục cuối.
	Trail []View `json:"trail,omitempty"`
	// Omit: khoá các khối người dùng đã bỏ trước khi gửi.
	Omit []string `json:"omit,omitempty"`
}

// Chip là một khối của khối ngữ cảnh: nhãn hiện ở đầu ngăn AI và nội dung
// đúng như gửi đi (đã che).
type Chip struct {
	Key     string    `json:"key"`
	Label   i18n.Text `json:"label"`
	Content string    `json:"content"`
}

// AskPreview là thứ sẽ gửi (Preview). Mọi chữ đã che.
type AskPreview struct {
	SessionID string `json:"session_id,omitempty"`
	Question  string `json:"question"`
	// Chips: mọi khối dựng được, kể cả khối có khoá trong Omit (Omitted =
	// true), để người bỏ rồi bật lại được.
	Chips    []PreviewChip `json:"chips"`
	Playbook string        `json:"playbook,omitempty"`
	// Prompt là toàn văn sẽ đưa vào stdin của claude.
	Prompt string `json:"prompt"`
}

// PreviewChip là Chip kèm cờ đã bị bỏ.
type PreviewChip struct {
	Chip
	Omitted bool `json:"omitted"`
}

// AskStarted là kết quả Ask.
type AskStarted struct {
	SessionID string `json:"session_id"`
	Seq       int    `json:"seq"`
}

// TurnView là một lượt trên trang Phiên AI.
type TurnView struct {
	Seq      int    `json:"seq"`
	Lang     string `json:"lang"`
	Question string `json:"question"` // đã che, đúng như gửi
	Chips    []Chip `json:"chips"`    // đã che, đúng như gửi
	Playbook string `json:"playbook,omitempty"`
	Prompt   string `json:"prompt"` // đã che, đúng như gửi
	// Answer đã đổi về tên thật.
	Answer  string     `json:"answer"`
	Events  []Event    `json:"events"` // sự kiện hiển thị (đã đổi về tên thật)
	Status  string     `json:"status"`
	Error   string     `json:"error,omitempty"`
	Started time.Time  `json:"started"`
	Ended   *time.Time `json:"ended,omitempty"`
}

// SessionDetail là một phiên cho trang Phiên AI.
type SessionDetail struct {
	Session store.AISession      `json:"session"`
	Turns   []TurnView           `json:"turns"`
	Calls   []store.AICall       `json:"calls"`
	Plans   []engine.PendingPlan `json:"plans"`
}

// Panel là lõi của AI trong panel. Dựng một lần trong `tpan serve`.
type Panel struct {
	Store *store.Store
	// Brain nil: AI đang tắt (setting ai.brain = off).
	Brain    Brain
	Hostname string
	// Now nil thì dùng time.Now.
	Now func() time.Time

	// st giữ người xem đang mở và lượt đang chạy (khai báo ở panel.go).
	st panelState
}
