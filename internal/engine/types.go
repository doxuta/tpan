// Package engine chạy một Plan (danh sách Step) ở chế độ xem trước hoặc thực
// thi, ghi sự kiện của từng bước, và hoàn tác theo thứ tự ngược khi có bước lỗi.
// Mọi thay đổi trên máy chủ đều đi qua đây.
//
// Đặc tả: mục 4.2 của docs/KE-HOACH.md.
//
// Gói này import system, checks, i18n. Nó KHÔNG import store: việc ghi job và
// sự kiện đi qua giao diện Journal dưới đây, store cài đặt giao diện đó, core
// nối hai bên lại.
//
// Gói phải có (viết ở các file khác trong gói):
//
//	// New dựng engine chạy trên sys.
//	func New(sys system.System, opts Options) *Engine
//
//	// Preview gọi Check và Info cho từng bước, không gọi Apply, không lấy khoá,
//	// không ghi Journal. Với mỗi FileChange, đọc nội dung hiện có trên máy vào
//	// FileChange.Old (file chưa có thì Old = nil, OldExists = false) để vẽ diff.
//	func (e *Engine) Preview(ctx context.Context, p Plan) (Preview, error)
//
//	// Execute lấy khoá toàn cục (sys.Lock), tạo job, rồi chạy từng bước:
//	// Check → đã đúng thì bỏ qua → Apply → Check lại, vẫn sai thì coi là lỗi.
//	// Bước lỗi: dừng, gọi Undo cho các bước đã Apply theo thứ tự ngược; lỗi khi
//	// hoàn tác được ghi và báo, không nuốt. Mọi bước xong thì chạy p.Verify;
//	// có phép kiểm Fail thì cũng coi là lỗi và hoàn tác như trên (Warn và
//	// Unknown không làm job lỗi). Job thành công thì ghi sha256 của mọi file đã
//	// ghi vào Journal.RecordFile (Delete thì ForgetFile).
//	// error trả về chỉ khi không chạy được (không lấy được khoá, Journal lỗi);
//	// bước lỗi thì error là nil và Report.Status cho biết kết quả.
//	func (e *Engine) Execute(ctx context.Context, p Plan) (Report, error)
//
// Trong lúc Execute, System mà engine đưa cho Step.Check/Apply/Undo là một lớp
// bọc quanh sys: mỗi lệnh Run được ghi thành sự kiện EventCommand, và mỗi dòng
// output thành EventOutput (đặt Cmd.OnOutput nếu Step chưa đặt). Step không
// cần biết tới job hay Journal.
package engine

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Step là một thay đổi nhỏ, tự kiểm được, hoàn tác được.
type Step interface {
	// Info mô tả bước cho người đọc: tiêu đề, lý do, lệnh sẽ chạy, file sẽ ghi.
	// Không chạm vào máy; được gọi cả lúc xem trước.
	Info() StepInfo
	// Check chỉ đọc. Trả về true nếu máy đã ở trạng thái mong muốn, kể cả khi
	// trạng thái đó không do tpan tạo; khi đó bước được bỏ qua.
	Check(ctx context.Context, sys system.System) (done bool, err error)
	// Apply thực hiện thay đổi. Sau Apply, Check phải trả true.
	Apply(ctx context.Context, sys system.System) error
	// Undo đưa máy về trước Apply. Chỉ được gọi cho bước đã Apply thành công
	// trong cùng lần chạy.
	Undo(ctx context.Context, sys system.System) error
}

// StepInfo mô tả một bước. Giao diện và `--dry-run` hiển thị đúng những gì ở đây.
type StepInfo struct {
	// ID ổn định và duy nhất trong Plan, dạng "<loại>:<đối tượng>",
	// ví dụ "apt.install:nginx", "nginx.vhost:example.com".
	ID    string    `json:"id"`
	Title i18n.Text `json:"title"`
	// Why trả lời câu "bỏ bước này thì hỏng gì", viết cho người mới. Đây là câu
	// hiện ra khi bật Gợi ý (mục 10).
	Why i18n.Text `json:"why"`
	// Commands là các lệnh bước này sẽ chạy (system.Cmd.String()), chỉ để hiển thị.
	Commands []string     `json:"commands,omitempty"`
	Files    []FileChange `json:"files,omitempty"`
	// Danger: bước có thể làm gián đoạn dịch vụ hoặc xoá dữ liệu. Giao diện hỏi lại.
	Danger bool `json:"danger,omitempty"`
	// NoUndo khác rỗng nghĩa là Undo của bước này không làm gì, kèm lý do
	// (ví dụ "gỡ gói có thể phá gói khác đang phụ thuộc").
	NoUndo i18n.Text `json:"no_undo,omitzero"`
}

// FileChange là một file bước sẽ ghi hoặc xoá.
type FileChange struct {
	Path string `json:"path"`
	// New là nội dung sẽ ghi. Bỏ trống khi Delete.
	New []byte `json:"new,omitempty"`
	// Delete: bước sẽ xoá file này.
	Delete bool `json:"delete,omitempty"`
	// Old và OldExists do engine điền lúc Preview bằng cách đọc file trên máy.
	// Step để trống.
	Old       []byte `json:"old,omitempty"`
	OldExists bool   `json:"old_exists"`
}

// Plan là một thao tác hoàn chỉnh: các bước theo thứ tự, rồi các phép kiểm chứng.
type Plan struct {
	// Kind là loại thao tác, ví dụ "stack.install". Target là đối tượng, ví dụ
	// "nginx" hoặc "example.com". Hai trường này đi vào bảng jobs để lọc lịch sử.
	Kind   string    `json:"kind"`
	Target string    `json:"target"`
	Title  i18n.Text `json:"title"`
	Steps  []Step    `json:"-"`
	// Verify chạy sau khi mọi bước xong; kết quả là bằng chứng của job.
	Verify []checks.Check `json:"-"`
}

// Preview là kết quả xem trước của một Plan.
type Preview struct {
	Kind   string        `json:"kind"`
	Target string        `json:"target"`
	Title  i18n.Text     `json:"title"`
	Steps  []StepPreview `json:"steps"`
}

// StepPreview là một bước trong bản xem trước.
type StepPreview struct {
	Info StepInfo `json:"info"`
	// Done: Check trả true, bước sẽ được bỏ qua.
	Done bool `json:"done"`
	// CheckError khác rỗng: Check lỗi lúc xem trước (thường vì máy chưa có thứ
	// mà bước trước sẽ cài). Bước vẫn được coi là sẽ chạy.
	CheckError string `json:"check_error,omitempty"`
}

// JobStatus là trạng thái của một job.
type JobStatus string

const (
	JobRunning    JobStatus = "running"
	JobSucceeded  JobStatus = "succeeded"   // mọi bước xong, kiểm chứng không có Fail
	JobRolledBack JobStatus = "rolled_back" // có lỗi, đã hoàn tác sạch
	JobUndoFailed JobStatus = "undo_failed" // có lỗi, và hoàn tác cũng lỗi: máy có thể dở dang
)

// Job là một lần chạy Plan.
type Job struct {
	// ID do engine sinh: 16 ký tự hex ngẫu nhiên.
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Target   string    `json:"target"`
	Title    i18n.Text `json:"title"`
	Status   JobStatus `json:"status"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	// Error là lỗi đầu tiên làm job dừng, rỗng nếu thành công.
	Error string `json:"error,omitempty"`
}

// EventKind là loại sự kiện trong một job.
type EventKind string

const (
	EventJobStart  EventKind = "job.start"
	EventStepStart EventKind = "step.start"
	EventStepSkip  EventKind = "step.skip" // Check trả true, bỏ qua
	EventCommand   EventKind = "command"   // Message: lệnh vừa chạy (Cmd.String())
	EventOutput    EventKind = "output"    // Message: một dòng output
	EventStepDone  EventKind = "step.done"
	EventStepFail  EventKind = "step.fail" // Message: lỗi
	EventUndoStart EventKind = "undo.start"
	EventUndoDone  EventKind = "undo.done"
	EventUndoFail  EventKind = "undo.fail" // Message: lỗi khi hoàn tác
	EventCheck     EventKind = "check"     // Check: kết quả một phép kiểm chứng
	EventJobDone   EventKind = "job.done"  // Message: JobStatus
)

// Event là một dòng trong log của job. Được ghi vào Journal và phát cho
// Options.OnEvent (CLI in tiến độ, server đẩy qua SSE).
type Event struct {
	JobID string `json:"job_id"`
	// Seq tăng dần từ 1 trong một job.
	Seq     int            `json:"seq"`
	Time    time.Time      `json:"time"`
	Kind    EventKind      `json:"kind"`
	StepID  string         `json:"step_id,omitempty"`
	Message string         `json:"message,omitempty"`
	Check   *checks.Result `json:"check,omitempty"`
}

// StepResult là kết quả của một bước trong Report.
type StepResult struct {
	ID string `json:"id"`
	// Outcome: "skipped", "applied", "failed", "undone", "undo_failed", "not_run".
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}

// Report là kết quả của Execute.
type Report struct {
	Job    Job             `json:"job"`
	Steps  []StepResult    `json:"steps"`
	Checks []checks.Result `json:"checks"`
}

// Journal ghi job và sự kiện. store.Store cài đặt giao diện này; test của
// engine dùng một bản trong bộ nhớ.
type Journal interface {
	CreateJob(ctx context.Context, job Job) error
	AddEvent(ctx context.Context, ev Event) error
	// FinishJob cập nhật Status, Finished và Error của job đã tạo.
	FinishJob(ctx context.Context, job Job) error
	// RecordFile ghi lại sha256 (hex) của một file tpan vừa ghi, để health
	// phát hiện sửa tay (M4).
	RecordFile(ctx context.Context, path, sha256Hex, jobID string) error
	// ForgetFile bỏ một file khỏi danh sách tpan quản lý.
	ForgetFile(ctx context.Context, path string) error
}

// Options cấu hình engine.
type Options struct {
	// Journal nil thì không ghi gì (dùng cho test và --demo).
	Journal Journal
	// OnEvent khác nil thì được gọi cho mọi sự kiện, theo thứ tự, ngay khi có.
	OnEvent func(Event)
	// Now nil thì dùng time.Now. Test đặt để có thời điểm cố định.
	Now func() time.Time
}
