package store

// Hợp đồng M7: bảng plans (migration 700) và ai_sessions, ai_calls (701).
//
// Thợ của đợt 1 viết các hàm dưới đây ở plans.go và ai.go (cùng test):
//
//	// CreatePlan thêm một Plan chờ duyệt. p.ID do người gọi sinh; trùng id thì lỗi.
//	func (s *Store) CreatePlan(ctx context.Context, p engine.PendingPlan) error
//	// Plan đọc một Plan; không có thì lỗi thoả errors.Is(err, ErrNotFound).
//	func (s *Store) Plan(ctx context.Context, id string) (engine.PendingPlan, error)
//	// Plans liệt kê, mới nhất trước (created_at giảm dần), tối đa limit dòng.
//	// status rỗng là mọi trạng thái.
//	func (s *Store) Plans(ctx context.Context, status engine.PlanStatus, limit int) ([]engine.PendingPlan, error)
//	// UpdatePlanPreview thay bản xem trước và dấu vân tay của một Plan còn
//	// pending (khi dựng lại thấy khác). Không pending thì ErrPlanNotPending.
//	func (s *Store) UpdatePlanPreview(ctx context.Context, id string, pv engine.Preview, fingerprint string) error
//	// DecidePlan chuyển một Plan từ pending sang status (applied hoặc rejected),
//	// ghi decided_at và decided_by, trong MỘT câu UPDATE ... WHERE status='pending'
//	// để panel và CLI không cùng duyệt được một Plan. Không có thì ErrNotFound;
//	// có mà không còn pending thì ErrPlanNotPending.
//	func (s *Store) DecidePlan(ctx context.Context, id string, status engine.PlanStatus, by string, at time.Time) error
//	// ReopenPlan đưa một Plan applied mà chưa có job_id về pending (xoá
//	// decided_at, decided_by): duyệt rồi mà job không bắt đầu được (khoá đang bận).
//	// Đã có job_id thì ErrPlanNotPending.
//	func (s *Store) ReopenPlan(ctx context.Context, id string) error
//	// SetPlanJob ghi job_id cho một Plan đã applied.
//	func (s *Store) SetPlanJob(ctx context.Context, id, jobID string) error
//
//	// CreateAISession thêm một phiên. Aliases nil ghi là "{}".
//	func (s *Store) CreateAISession(ctx context.Context, a AISession) error
//	// AISession đọc một phiên; không có thì ErrNotFound.
//	func (s *Store) AISession(ctx context.Context, id string) (AISession, error)
//	// SaveAISessionState ghi bảng bí danh, cờ untrusted, client và last_at.
//	func (s *Store) SaveAISessionState(ctx context.Context, a AISession) error
//	// AddAICall ghi một lần gọi công cụ. Seq do người gọi đặt, tăng từ 1.
//	func (s *Store) AddAICall(ctx context.Context, c AICall) error
//	// AICalls trả các lần gọi của một phiên theo Seq tăng dần.
//	func (s *Store) AICalls(ctx context.Context, sessionID string) ([]AICall, error)

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrPlanNotPending: Plan đã được duyệt hoặc từ chối. Dùng errors.Is để kiểm.
var ErrPlanNotPending = errors.New("store: Plan không còn chờ duyệt")

// Lối vào của một phiên AI (cột ai_sessions.entry).
const (
	EntryMCP   = "mcp"   // agent nối trực tiếp qua `tpan mcp`
	EntryPanel = "panel" // câu hỏi gửi từ giao diện (M8)
)

// AISession là một dòng của ai_sessions.
type AISession struct {
	ID     string `json:"id"`
	Entry  string `json:"entry"`
	Client string `json:"client,omitempty"`
	// Question và Context dùng từ M8.
	Question string          `json:"question,omitempty"`
	Context  json.RawMessage `json:"context,omitempty"`
	// Aliases: bí danh → giá trị thật. Chỉ ở máy chủ.
	Aliases   map[string]string `json:"-"`
	Untrusted bool              `json:"untrusted"`
	Created   time.Time         `json:"created"`
	Last      time.Time         `json:"last"`
}

// AICall là một lần gọi công cụ trong phiên. Input và Output là đúng thứ đi
// qua lại với agent (đã che), để xem lại được AI đã thấy gì.
type AICall struct {
	SessionID string          `json:"session_id"`
	Seq       int             `json:"seq"`
	Time      time.Time       `json:"time"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Error     string          `json:"error,omitempty"`
	PlanID    string          `json:"plan_id,omitempty"`
}
