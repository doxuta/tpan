package store

// Hợp đồng M8: lượt hỏi của AI trong panel và các lần chuyển mục (migration
// 800). Mục 4.4, 8.4 của docs/KE-HOACH.md.
//
// Thợ đợt 1 viết ở ai_panel.go (cùng test), thay thân tạm:
//
//	// CreateAISession, AISession (ai.go): thêm cột brain_session.
//	// CreateAITurn thêm một lượt (Status thường là running). Trùng
//	// (SessionID, Seq) thì lỗi. Context, Events rỗng ghi "[]".
//	func (s *Store) CreateAITurn(ctx context.Context, t AITurn) error
//	// FinishAITurn ghi kết thúc của một lượt đang running: status (done,
//	// error, refused), answer, error, events, ended_at = at. Lượt không còn
//	// running thì ErrTurnNotRunning; không có thì ErrNotFound.
//	func (s *Store) FinishAITurn(ctx context.Context, sessionID string, seq int, f TurnEnd) error
//	// AITurns trả các lượt của một phiên theo Seq tăng dần.
//	func (s *Store) AITurns(ctx context.Context, sessionID string) ([]AITurn, error)
//	// AITurn đọc một lượt; không có thì ErrNotFound.
//	func (s *Store) AITurn(ctx context.Context, sessionID string, seq int) (AITurn, error)
//	// CountAITurnsSince đếm số lượt bắt đầu từ since trở đi (trần mỗi ngày).
//	func (s *Store) CountAITurnsSince(ctx context.Context, since time.Time) (int, error)
//	// FailRunningAITurns chuyển mọi lượt running sang error với lời msg,
//	// ended_at = at; trả số lượt đã chuyển.
//	func (s *Store) FailRunningAITurns(ctx context.Context, msg string, at time.Time) (int, error)
//	// AISessions liệt kê phiên (mọi lối vào), mới dùng nhất trước (last_at
//	// giảm dần), tối đa limit dòng, kèm số lượt, số lần gọi công cụ, số Plan
//	// đề xuất và câu hỏi đầu (cột question của ai_sessions).
//	func (s *Store) AISessions(ctx context.Context, limit int) ([]AISessionSummary, error)
//	// PlansBySession trả các Plan có session_id = id, cũ trước mới sau.
//	func (s *Store) PlansBySession(ctx context.Context, id string) ([]engine.PendingPlan, error)
//	// AddAIView ghi một lần chuyển mục rồi xoá bớt, chỉ giữ 50 dòng mới nhất.
//	// Path, Label rỗng thì lỗi. Time rỗng là bây giờ.
//	func (s *Store) AddAIView(ctx context.Context, v AIView) error
//	// AIViews trả tối đa limit lần chuyển mục mới nhất, MỚI TRƯỚC.
//	func (s *Store) AIViews(ctx context.Context, limit int) ([]AIView, error)

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrTurnNotRunning: lượt đã kết thúc. Dùng errors.Is để kiểm.
var ErrTurnNotRunning = errors.New("store: lượt hỏi AI đã kết thúc")

// Trạng thái của một lượt (cột ai_turns.status).
const (
	TurnRunning = "running"
	TurnDone    = "done"
	TurnError   = "error"
	TurnRefused = "refused"
)

// AITurn là một dòng của ai_turns. Mọi chữ ở dạng đã che (đúng thứ đã gửi và
// đúng thứ AI viết); đổi về tên thật là việc của gói ai lúc hiển thị.
type AITurn struct {
	SessionID string          `json:"session_id"`
	Seq       int             `json:"seq"`
	Lang      string          `json:"lang"`
	Question  string          `json:"question"`
	Context   json.RawMessage `json:"context"` // JSON []ai.Chip
	Playbook  string          `json:"playbook,omitempty"`
	Prompt    string          `json:"prompt"`
	Answer    string          `json:"answer"`
	Events    json.RawMessage `json:"events"` // JSON []ai.Event
	Status    string          `json:"status"`
	Error     string          `json:"error,omitempty"`
	Started   time.Time       `json:"started"`
	// Ended rỗng khi còn chạy.
	Ended time.Time `json:"ended"`
}

// TurnEnd là phần ghi lúc một lượt kết thúc.
type TurnEnd struct {
	Status string
	Answer string
	Error  string
	Events json.RawMessage
	At     time.Time
}

// AISessionSummary là một dòng của danh sách phiên AI.
type AISessionSummary struct {
	AISession
	Turns int `json:"turns"`
	Calls int `json:"calls"`
	Plans int `json:"plans"`
}

// AIView là một lần chuyển mục trên panel.
type AIView struct {
	ID    int64     `json:"id"`
	Time  time.Time `json:"time"`
	Path  string    `json:"path"`
	Label string    `json:"label"`
}
