package store

// Truy vấn cho bảng ai_sessions và ai_calls (migration 701): phiên AI và nhật ký
// các lần gọi công cụ (mục 4.4, 8.1 và 8.3 của docs/KE-HOACH.md). Hợp đồng nằm ở
// chú thích đầu ai_types.go.
//
// Store lưu đúng thứ được đưa. Việc che dữ liệu (bí danh, che bí mật) làm ở gói
// khác trước khi gọi vào đây; riêng bảng Aliases là thứ KHÔNG bao giờ được gửi
// cho AI nên chỉ nằm ở cột aliases của máy chủ.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const aiSessionColumns = "id, entry, client, question, context, aliases, untrusted, brain_session, created_at, last_at"

// rawOrNull: JSON rỗng lưu thành "null" để cột NOT NULL luôn chứa JSON hợp lệ.
// Thứ không phải JSON thì từ chối: nó sẽ làm hỏng việc trả lại qua API
// (json.RawMessage không hợp lệ khiến Marshal lỗi).
func rawOrNull(raw json.RawMessage, what string) (string, error) {
	if len(raw) == 0 {
		return "null", nil
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("%s không phải JSON hợp lệ", what)
	}
	return string(raw), nil
}

func marshalAliases(m map[string]string) (string, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("mã hoá bảng bí danh: %w", err)
	}
	return string(b), nil
}

// CreateAISession thêm một phiên. Aliases nil ghi là "{}". Created bỏ trống là
// bây giờ; Last bỏ trống là bằng Created.
func (s *Store) CreateAISession(ctx context.Context, a AISession) error {
	aliases, err := marshalAliases(a.Aliases)
	if err != nil {
		return fmt.Errorf("tạo phiên AI %s: %w", a.ID, err)
	}
	var sctx string // cột context là TEXT '' khi chưa có (khác với ai_calls: không phải JSON "null")
	if len(a.Context) > 0 {
		if !json.Valid(a.Context) {
			return fmt.Errorf("tạo phiên AI %s: context không phải JSON hợp lệ", a.ID)
		}
		sctx = string(a.Context)
	}
	if a.Created.IsZero() {
		a.Created = s.now()
	}
	if a.Last.IsZero() {
		a.Last = a.Created
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ai_sessions (id, entry, client, question, context, aliases, untrusted, brain_session, created_at, last_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Entry, a.Client, a.Question, sctx, aliases, siteBool(a.Untrusted), a.BrainSession,
		fmtTime(a.Created), fmtTime(a.Last))
	if err != nil {
		return fmt.Errorf("tạo phiên AI %s: %w", a.ID, err)
	}
	return nil
}

// aiSessionScan đọc một dòng ai_sessions theo thứ tự aiSessionColumns. Dùng chung
// cho AISession (một dòng) và AISessions (danh sách của panel). Aliases đọc ra
// luôn khác nil để người gọi ghi thêm vào được.
func aiSessionScan(r rowScanner, a *AISession) error {
	var (
		sctx, aliases string
		untrusted     int
		created, last string
	)
	if err := r.Scan(&a.ID, &a.Entry, &a.Client, &a.Question, &sctx, &aliases, &untrusted,
		&a.BrainSession, &created, &last); err != nil {
		return err
	}
	if sctx != "" {
		a.Context = json.RawMessage(sctx)
	}
	a.Aliases = map[string]string{}
	if err := json.Unmarshal([]byte(aliases), &a.Aliases); err != nil {
		return fmt.Errorf("giải mã bảng bí danh của phiên AI %s: %w", a.ID, err)
	}
	if a.Aliases == nil { // cột chứa chữ "null"
		a.Aliases = map[string]string{}
	}
	a.Untrusted = untrusted != 0
	var err error
	if a.Created, err = parseTime(created); err != nil {
		return fmt.Errorf("phiên AI %s: %w", a.ID, err)
	}
	if a.Last, err = parseTime(last); err != nil {
		return fmt.Errorf("phiên AI %s: %w", a.ID, err)
	}
	return nil
}

// AISession đọc một phiên; không có thì lỗi thoả errors.Is(err, ErrNotFound).
func (s *Store) AISession(ctx context.Context, id string) (AISession, error) {
	var a AISession
	err := aiSessionScan(s.db.QueryRowContext(ctx, "SELECT "+aiSessionColumns+" FROM ai_sessions WHERE id = ?", id), &a)
	if errors.Is(err, sql.ErrNoRows) {
		return AISession{}, fmt.Errorf("phiên AI %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return AISession{}, fmt.Errorf("đọc phiên AI %s: %w", id, err)
	}
	return a, nil
}

// SaveAISessionState ghi bảng bí danh, cờ untrusted, client và last_at của một
// phiên có sẵn. Mỗi lượt `tpan mcp` là một tiến trình mới nên trạng thái phải
// được lưu lại sau mỗi lần gọi. Last bỏ trống là bây giờ. Phiên không có thì ErrNotFound.
func (s *Store) SaveAISessionState(ctx context.Context, a AISession) error {
	aliases, err := marshalAliases(a.Aliases)
	if err != nil {
		return fmt.Errorf("lưu trạng thái phiên AI %s: %w", a.ID, err)
	}
	if a.Last.IsZero() {
		a.Last = s.now()
	}
	n, err := affected(s.db.ExecContext(ctx,
		"UPDATE ai_sessions SET aliases = ?, untrusted = ?, client = ?, last_at = ? WHERE id = ?",
		aliases, siteBool(a.Untrusted), a.Client, fmtTime(a.Last), a.ID))
	if err != nil {
		return fmt.Errorf("lưu trạng thái phiên AI %s: %w", a.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("lưu trạng thái phiên AI %s: %w", a.ID, ErrNotFound)
	}
	return nil
}

// AddAICall ghi một lần gọi công cụ. Seq do người gọi đặt, tăng từ 1; trùng
// (session, seq) hoặc phiên không tồn tại thì lỗi (khoá ngoại bật ở Open).
// Input và Output rỗng lưu thành JSON null.
func (s *Store) AddAICall(ctx context.Context, c AICall) error {
	in, err := rawOrNull(c.Input, "input")
	if err != nil {
		return fmt.Errorf("ghi lần gọi %s#%d: %w", c.SessionID, c.Seq, err)
	}
	out, err := rawOrNull(c.Output, "output")
	if err != nil {
		return fmt.Errorf("ghi lần gọi %s#%d: %w", c.SessionID, c.Seq, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ai_calls (session_id, seq, time, tool, input, output, error, plan_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.SessionID, c.Seq, fmtTime(c.Time), c.Tool, in, out, c.Error, c.PlanID)
	if err != nil {
		return fmt.Errorf("ghi lần gọi %s#%d: %w", c.SessionID, c.Seq, err)
	}
	return nil
}

// AICalls trả các lần gọi của một phiên theo Seq tăng dần. Phiên không có lần
// gọi nào (hoặc không tồn tại) cho danh sách rỗng, không phải lỗi.
func (s *Store) AICalls(ctx context.Context, sessionID string) ([]AICall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, seq, time, tool, input, output, error, plan_id
		 FROM ai_calls WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("đọc các lần gọi của phiên %s: %w", sessionID, err)
	}
	defer func() { _ = rows.Close() }()
	out := []AICall{}
	for rows.Next() {
		var (
			c        AICall
			at       string
			in, outp string
		)
		if err := rows.Scan(&c.SessionID, &c.Seq, &at, &c.Tool, &in, &outp, &c.Error, &c.PlanID); err != nil {
			return nil, fmt.Errorf("đọc dòng lần gọi của phiên %s: %w", sessionID, err)
		}
		if c.Time, err = parseTime(at); err != nil {
			return nil, fmt.Errorf("lần gọi %s#%d: %w", sessionID, c.Seq, err)
		}
		c.Input, c.Output = json.RawMessage(in), json.RawMessage(outp)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc các lần gọi của phiên %s: %w", sessionID, err)
	}
	return out, nil
}
