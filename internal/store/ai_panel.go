package store

// Truy vấn cho ai_turns và ai_views (migration 800): các lượt hỏi AI trong panel
// và các lần chuyển mục (mục 4.4 và 8.4 của docs/KE-HOACH.md). Hợp đồng nằm ở
// chú thích đầu ai_panel_types.go.
//
// Như ai.go: store lưu đúng thứ được đưa. Chữ trong lượt đã được che trước khi
// tới đây; store không che, không đổi bí danh.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/engine"
)

const aiTurnColumns = "session_id, seq, lang, question, context, playbook, prompt, answer, events, status, error, started_at, ended_at"

// maxAIViews là số lần chuyển mục store giữ lại (mục 8.4: công cụ current_view
// chỉ cần mười lần gần nhất, năm mươi là dư để không phải xoá mỗi lần ghi).
const maxAIViews = 50

// rawOrEmptyList: JSON rỗng lưu thành "[]" (cột NOT NULL của ai_turns luôn là
// một mảng JSON hợp lệ). Thứ không phải JSON thì từ chối, vì json.RawMessage
// hỏng làm Marshal của API lỗi lúc trả lại.
func rawOrEmptyList(raw json.RawMessage, what string) (string, error) {
	if len(raw) == 0 {
		return "[]", nil
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("%s không phải JSON hợp lệ", what)
	}
	return string(raw), nil
}

// CreateAITurn thêm một lượt. Status bỏ trống là running; Started bỏ trống là
// bây giờ. Trùng (SessionID, Seq), phiên không tồn tại (khoá ngoại) hoặc lang
// ngoài vi/en thì lỗi.
func (s *Store) CreateAITurn(ctx context.Context, t AITurn) error {
	sctx, err := rawOrEmptyList(t.Context, "context")
	if err != nil {
		return fmt.Errorf("tạo lượt AI %s#%d: %w", t.SessionID, t.Seq, err)
	}
	events, err := rawOrEmptyList(t.Events, "events")
	if err != nil {
		return fmt.Errorf("tạo lượt AI %s#%d: %w", t.SessionID, t.Seq, err)
	}
	if t.Status == "" {
		t.Status = TurnRunning
	}
	if t.Started.IsZero() {
		t.Started = s.now()
	}
	// ended_at là NULL khi còn chạy; chuỗi rỗng sẽ không đọc ngược được.
	var ended sql.NullString
	if !t.Ended.IsZero() {
		ended = sql.NullString{String: fmtTime(t.Ended), Valid: true}
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ai_turns (`+aiTurnColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.SessionID, t.Seq, t.Lang, t.Question, sctx, t.Playbook, t.Prompt, t.Answer, events,
		t.Status, t.Error, fmtTime(t.Started), ended)
	if err != nil {
		return fmt.Errorf("tạo lượt AI %s#%d: %w", t.SessionID, t.Seq, err)
	}
	return nil
}

// FinishAITurn ghi kết thúc của một lượt đang running. Là MỘT câu UPDATE có
// điều kiện status = 'running' (như DecidePlan): bộ não báo xong và lúc khởi
// động dọn lượt treo (FailRunningAITurns) không thể cùng ghi một lượt. Lượt
// không còn running thì ErrTurnNotRunning; không có thì ErrNotFound.
func (s *Store) FinishAITurn(ctx context.Context, sessionID string, seq int, f TurnEnd) error {
	switch f.Status {
	case TurnDone, TurnError, TurnRefused:
	default:
		return fmt.Errorf("kết thúc lượt AI %s#%d: trạng thái %q không hợp lệ (done, error hoặc refused)", sessionID, seq, f.Status)
	}
	events, err := rawOrEmptyList(f.Events, "events")
	if err != nil {
		return fmt.Errorf("kết thúc lượt AI %s#%d: %w", sessionID, seq, err)
	}
	if f.At.IsZero() {
		f.At = s.now()
	}
	n, err := affected(s.db.ExecContext(ctx,
		`UPDATE ai_turns SET status = ?, answer = ?, error = ?, events = ?, ended_at = ?
		 WHERE session_id = ? AND seq = ? AND status = 'running'`,
		f.Status, f.Answer, f.Error, events, fmtTime(f.At), sessionID, seq))
	if err != nil {
		return fmt.Errorf("kết thúc lượt AI %s#%d: %w", sessionID, seq, err)
	}
	if n == 0 {
		// Khớp 0 dòng: không có lượt, hay có mà đã kết thúc?
		if _, err := s.AITurn(ctx, sessionID, seq); err != nil {
			return fmt.Errorf("kết thúc lượt AI %s#%d: %w", sessionID, seq, err)
		}
		return fmt.Errorf("kết thúc lượt AI %s#%d: %w", sessionID, seq, ErrTurnNotRunning)
	}
	return nil
}

func aiTurnScan(r rowScanner) (AITurn, error) {
	var (
		t            AITurn
		sctx, events string
		started      string
		ended        sql.NullString
	)
	if err := r.Scan(&t.SessionID, &t.Seq, &t.Lang, &t.Question, &sctx, &t.Playbook, &t.Prompt,
		&t.Answer, &events, &t.Status, &t.Error, &started, &ended); err != nil {
		return AITurn{}, err
	}
	t.Context, t.Events = json.RawMessage(sctx), json.RawMessage(events)
	var err error
	if t.Started, err = parseTime(started); err != nil {
		return AITurn{}, fmt.Errorf("lượt AI %s#%d: %w", t.SessionID, t.Seq, err)
	}
	if ended.Valid {
		if t.Ended, err = parseTime(ended.String); err != nil {
			return AITurn{}, fmt.Errorf("lượt AI %s#%d: %w", t.SessionID, t.Seq, err)
		}
	}
	return t, nil
}

// AITurns trả các lượt của một phiên theo Seq tăng dần. Phiên không có lượt
// nào (hoặc không tồn tại) cho danh sách rỗng, không phải lỗi.
func (s *Store) AITurns(ctx context.Context, sessionID string) ([]AITurn, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+aiTurnColumns+" FROM ai_turns WHERE session_id = ? ORDER BY seq", sessionID)
	if err != nil {
		return nil, fmt.Errorf("đọc các lượt của phiên AI %s: %w", sessionID, err)
	}
	defer func() { _ = rows.Close() }()
	out := []AITurn{}
	for rows.Next() {
		t, err := aiTurnScan(rows)
		if err != nil {
			return nil, fmt.Errorf("đọc dòng lượt AI của phiên %s: %w", sessionID, err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc các lượt của phiên AI %s: %w", sessionID, err)
	}
	return out, nil
}

// AITurn đọc một lượt; không có thì lỗi thoả errors.Is(err, ErrNotFound).
func (s *Store) AITurn(ctx context.Context, sessionID string, seq int) (AITurn, error) {
	t, err := aiTurnScan(s.db.QueryRowContext(ctx,
		"SELECT "+aiTurnColumns+" FROM ai_turns WHERE session_id = ? AND seq = ?", sessionID, seq))
	if errors.Is(err, sql.ErrNoRows) {
		return AITurn{}, fmt.Errorf("lượt AI %s#%d: %w", sessionID, seq, ErrNotFound)
	}
	if err != nil {
		return AITurn{}, fmt.Errorf("đọc lượt AI %s#%d: %w", sessionID, seq, err)
	}
	return t, nil
}

// CountAITurnsSince đếm số lượt bắt đầu từ since trở đi (trần mỗi ngày). So
// chuỗi được vì fmtTime có độ rộng cố định, chuỗi xếp đúng thứ tự thời gian.
func (s *Store) CountAITurnsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ai_turns WHERE started_at >= ?", fmtTime(since)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("đếm lượt AI từ %s: %w", fmtTime(since), err)
	}
	return n, nil
}

// FailRunningAITurns chuyển mọi lượt running sang error với lời msg, ended_at
// = at (zero là bây giờ); trả số lượt đã chuyển. Gọi lúc serve khởi động: tiến
// trình cũ chết thì không còn ai ghi kết thúc cho các lượt đó.
func (s *Store) FailRunningAITurns(ctx context.Context, msg string, at time.Time) (int, error) {
	if at.IsZero() {
		at = s.now()
	}
	n, err := affected(s.db.ExecContext(ctx,
		"UPDATE ai_turns SET status = 'error', error = ?, ended_at = ? WHERE status = 'running'",
		msg, fmtTime(at)))
	if err != nil {
		return 0, fmt.Errorf("đóng các lượt AI đang chạy: %w", err)
	}
	return int(n), nil
}

// AISessions liệt kê phiên (mọi lối vào), mới dùng nhất trước (last_at giảm
// dần), tối đa limit dòng (limit <= 0 là không giới hạn), kèm số lượt, số lần
// gọi công cụ và số Plan đề xuất. Đếm bằng truy vấn con để phiên không có lượt
// nào (phiên mcp) vẫn có mặt với số 0.
func (s *Store) AISessions(ctx context.Context, limit int) ([]AISessionSummary, error) {
	if limit <= 0 {
		limit = -1 // SQLite: LIMIT âm là không giới hạn
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+aiSessionColumns+`,
		   (SELECT COUNT(*) FROM ai_turns t WHERE t.session_id = ai_sessions.id),
		   (SELECT COUNT(*) FROM ai_calls c WHERE c.session_id = ai_sessions.id),
		   (SELECT COUNT(*) FROM plans p WHERE p.session_id = ai_sessions.id)
		 FROM ai_sessions ORDER BY last_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách phiên AI: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []AISessionSummary{}
	for rows.Next() {
		var sum AISessionSummary
		// aiSessionScan đọc đúng 10 cột đầu; ba số đếm đi sau, nên gói rows để
		// hàm chung nhận cột của nó và chừa phần còn lại cho ta.
		if err := aiSessionScan(&prefixScanner{rows: rows, extra: []any{&sum.Turns, &sum.Calls, &sum.Plans}}, &sum.AISession); err != nil {
			return nil, fmt.Errorf("đọc dòng phiên AI: %w", err)
		}
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc danh sách phiên AI: %w", err)
	}
	return out, nil
}

// prefixScanner cho aiSessionScan đọc các cột đầu của một dòng, còn các cột
// đếm theo sau được quét vào extra trong cùng một lần Scan.
type prefixScanner struct {
	rows  *sql.Rows
	extra []any
}

func (p *prefixScanner) Scan(dest ...any) error {
	return p.rows.Scan(append(dest, p.extra...)...)
}

// PlansBySession trả các Plan có session_id = id, cũ trước mới sau.
func (s *Store) PlansBySession(ctx context.Context, id string) ([]engine.PendingPlan, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+planColumns+" FROM plans WHERE session_id = ? ORDER BY created_at, rowid", id)
	if err != nil {
		return nil, fmt.Errorf("đọc các Plan của phiên AI %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	out := []engine.PendingPlan{}
	for rows.Next() {
		p, err := planScan(rows)
		if err != nil {
			return nil, fmt.Errorf("đọc dòng Plan của phiên AI %s: %w", id, err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc các Plan của phiên AI %s: %w", id, err)
	}
	return out, nil
}

// AddAIView ghi một lần chuyển mục rồi xoá bớt, chỉ giữ maxAIViews dòng mới
// nhất. Ghi và xoá trong một transaction để hai lần chuyển mục sát nhau không
// để lại số dòng lệch. Path hoặc Label rỗng thì lỗi (current_view không có gì
// để nói); Time rỗng là bây giờ.
func (s *Store) AddAIView(ctx context.Context, v AIView) error {
	if v.Path == "" || v.Label == "" {
		return fmt.Errorf("ghi lần chuyển mục: path %q và label %q không được rỗng", v.Path, v.Label)
	}
	if v.Time.IsZero() {
		v.Time = s.now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ghi lần chuyển mục %s: %w", v.Path, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "INSERT INTO ai_views (time, path, label) VALUES (?, ?, ?)",
		fmtTime(v.Time), v.Path, v.Label); err != nil {
		return fmt.Errorf("ghi lần chuyển mục %s: %w", v.Path, err)
	}
	// Giữ theo id (thứ tự ghi), không theo time: đồng hồ lùi thì dòng mới vẫn là dòng mới.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM ai_views WHERE id NOT IN (SELECT id FROM ai_views ORDER BY id DESC LIMIT ?)", maxAIViews); err != nil {
		return fmt.Errorf("xoá bớt các lần chuyển mục cũ: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ghi lần chuyển mục %s: %w", v.Path, err)
	}
	return nil
}

// AIViews trả tối đa limit lần chuyển mục mới nhất, MỚI TRƯỚC (limit <= 0 là
// mọi dòng đang giữ).
func (s *Store) AIViews(ctx context.Context, limit int) ([]AIView, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, time, path, label FROM ai_views ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("đọc các lần chuyển mục: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []AIView{}
	for rows.Next() {
		var (
			v  AIView
			at string
		)
		if err := rows.Scan(&v.ID, &at, &v.Path, &v.Label); err != nil {
			return nil, fmt.Errorf("đọc dòng lần chuyển mục: %w", err)
		}
		if v.Time, err = parseTime(at); err != nil {
			return nil, fmt.Errorf("lần chuyển mục %d: %w", v.ID, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc các lần chuyển mục: %w", err)
	}
	return out, nil
}
