package store

// Truy vấn cho bảng plans (migration 700): các Plan do AI hoặc agent đề xuất và
// đang chờ người duyệt (mục 4.2, 4.4 và 8.1 của docs/KE-HOACH.md). Hợp đồng nằm
// ở chú thích đầu ai_types.go.
//
// Điểm cốt lõi của file này: mọi chuyển trạng thái (duyệt, từ chối, mở lại) là
// MỘT câu UPDATE có điều kiện trên cột status. Panel và CLI có thể cùng bấm
// "duyệt" một Plan; SQLite chạy hai câu UPDATE lần lượt, câu thứ hai thấy status
// đã đổi nên khớp 0 dòng và trả ErrPlanNotPending. Nếu đọc status ra rồi mới ghi
// thì hai bên cùng thấy "pending" và cùng chạy Plan: đó là lỗi cần chặn.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/engine"
)

const planColumns = "id, kind, target, title_vi, title_en, request, preview, fingerprint, " +
	"origin, session_id, untrusted, status, created_at, decided_at, decided_by, job_id"

// planScan đọc một dòng plans. Preview là JSON do chính store ghi, nhưng vẫn
// giải mã có kiểm lỗi: file DB có thể bị sửa tay hoặc do bản tpan khác ghi.
func planScan(r rowScanner) (engine.PendingPlan, error) {
	var (
		p                engine.PendingPlan
		request, preview string
		origin, status   string
		untrusted        int
		created          string
		decided          sql.NullString
	)
	if err := r.Scan(&p.ID, &p.Kind, &p.Target, &p.Title.Vi, &p.Title.En, &request, &preview,
		&p.Fingerprint, &origin, &p.SessionID, &untrusted, &status, &created, &decided,
		&p.DecidedBy, &p.JobID); err != nil {
		return engine.PendingPlan{}, err
	}
	p.Request = json.RawMessage(request)
	if err := json.Unmarshal([]byte(preview), &p.Preview); err != nil {
		return engine.PendingPlan{}, fmt.Errorf("giải mã bản xem trước của Plan %s: %w", p.ID, err)
	}
	p.Origin = engine.Origin(origin)
	p.Untrusted = untrusted != 0
	p.Status = engine.PlanStatus(status)
	var err error
	if p.Created, err = parseTime(created); err != nil {
		return engine.PendingPlan{}, fmt.Errorf("Plan %s: %w", p.ID, err)
	}
	if decided.Valid {
		if p.Decided, err = parseTime(decided.String); err != nil {
			return engine.PendingPlan{}, fmt.Errorf("Plan %s: %w", p.ID, err)
		}
	}
	return p, nil
}

// CreatePlan thêm một Plan chờ duyệt. p.ID do người gọi sinh; trùng id thì lỗi.
// Status bỏ trống là pending; Plan mới chỉ được vào ở trạng thái pending, còn
// duyệt hay từ chối đi qua DecidePlan để luôn có decided_at và decided_by.
func (s *Store) CreatePlan(ctx context.Context, p engine.PendingPlan) error {
	if p.Status != "" && p.Status != engine.PlanPending {
		return fmt.Errorf("tạo Plan %s: Plan mới phải ở trạng thái pending, không phải %q", p.ID, p.Status)
	}
	// Request là thứ duy nhất để dựng lại Plan lúc duyệt; thiếu hay hỏng thì
	// Plan này không bao giờ chạy được, nên chặn ngay từ lúc lưu.
	if !json.Valid(p.Request) {
		return fmt.Errorf("tạo Plan %s: request rỗng hoặc không phải JSON hợp lệ", p.ID)
	}
	preview, err := json.Marshal(p.Preview)
	if err != nil {
		return fmt.Errorf("tạo Plan %s: mã hoá bản xem trước: %w", p.ID, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO plans (id, kind, target, title_vi, title_en, request, preview, fingerprint,
		                    origin, session_id, untrusted, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)`,
		p.ID, p.Kind, p.Target, p.Title.Vi, p.Title.En, string(p.Request), string(preview),
		p.Fingerprint, string(p.Origin), p.SessionID, siteBool(p.Untrusted), fmtTime(p.Created))
	if err != nil {
		return fmt.Errorf("tạo Plan %s: %w", p.ID, err)
	}
	return nil
}

// Plan đọc một Plan; không có thì lỗi thoả errors.Is(err, ErrNotFound).
func (s *Store) Plan(ctx context.Context, id string) (engine.PendingPlan, error) {
	p, err := planScan(s.db.QueryRowContext(ctx, "SELECT "+planColumns+" FROM plans WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return engine.PendingPlan{}, fmt.Errorf("Plan %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return engine.PendingPlan{}, fmt.Errorf("đọc Plan %s: %w", id, err)
	}
	return p, nil
}

// Plans liệt kê, mới nhất trước (created_at giảm dần), tối đa limit dòng;
// limit <= 0 là không giới hạn. status rỗng là mọi trạng thái.
func (s *Store) Plans(ctx context.Context, status engine.PlanStatus, limit int) ([]engine.PendingPlan, error) {
	if limit <= 0 {
		limit = -1 // SQLite: LIMIT âm là không giới hạn
	}
	// rowid làm khoá phụ để hai Plan cùng created_at có thứ tự ổn định.
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+planColumns+` FROM plans WHERE (? = '' OR status = ?)
		 ORDER BY created_at DESC, rowid DESC LIMIT ?`,
		string(status), string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách Plan: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []engine.PendingPlan{}
	for rows.Next() {
		p, err := planScan(rows)
		if err != nil {
			return nil, fmt.Errorf("đọc dòng Plan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc danh sách Plan: %w", err)
	}
	return out, nil
}

// planStatus đọc status của một Plan, để biết vì sao một UPDATE có điều kiện
// khớp 0 dòng: Plan không tồn tại (ErrNotFound) hay có mà sai trạng thái.
func (s *Store) planStatus(ctx context.Context, id string) (engine.PlanStatus, error) {
	var status string
	err := s.db.QueryRowContext(ctx, "SELECT status FROM plans WHERE id = ?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("Plan %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("đọc trạng thái Plan %s: %w", id, err)
	}
	return engine.PlanStatus(status), nil
}

// affected trả số dòng một câu UPDATE đã đổi.
func affected(res sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdatePlanPreview thay bản xem trước và dấu vân tay của một Plan còn pending
// (khi dựng lại thấy khác). Không pending thì ErrPlanNotPending; không có thì ErrNotFound.
func (s *Store) UpdatePlanPreview(ctx context.Context, id string, pv engine.Preview, fingerprint string) error {
	b, err := json.Marshal(pv)
	if err != nil {
		return fmt.Errorf("cập nhật bản xem trước của Plan %s: mã hoá: %w", id, err)
	}
	n, err := affected(s.db.ExecContext(ctx,
		"UPDATE plans SET preview = ?, fingerprint = ? WHERE id = ? AND status = 'pending'",
		string(b), fingerprint, id))
	if err != nil {
		return fmt.Errorf("cập nhật bản xem trước của Plan %s: %w", id, err)
	}
	if n == 0 {
		return s.notPending(ctx, "cập nhật bản xem trước của Plan", id)
	}
	return nil
}

// notPending dựng lỗi cho một thao tác đòi Plan phải đang pending mà không khớp dòng nào.
func (s *Store) notPending(ctx context.Context, what, id string) error {
	st, err := s.planStatus(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", what, id, err)
	}
	return fmt.Errorf("%s %s (đang %s): %w", what, id, st, ErrPlanNotPending)
}

// DecidePlan chuyển một Plan từ pending sang status (applied hoặc rejected),
// ghi decided_at và decided_by, trong MỘT câu UPDATE ... WHERE status='pending'
// để panel và CLI không cùng duyệt được một Plan. Không có thì ErrNotFound;
// có mà không còn pending thì ErrPlanNotPending.
func (s *Store) DecidePlan(ctx context.Context, id string, status engine.PlanStatus, by string, at time.Time) error {
	if status != engine.PlanApplied && status != engine.PlanRejected {
		return fmt.Errorf("quyết định Plan %s: trạng thái %q không hợp lệ (chỉ applied hoặc rejected)", id, status)
	}
	n, err := affected(s.db.ExecContext(ctx,
		"UPDATE plans SET status = ?, decided_at = ?, decided_by = ? WHERE id = ? AND status = 'pending'",
		string(status), fmtTime(at), by, id))
	if err != nil {
		return fmt.Errorf("quyết định Plan %s: %w", id, err)
	}
	if n == 0 {
		return s.notPending(ctx, "quyết định Plan", id)
	}
	return nil
}

// ReopenPlan đưa một Plan applied mà chưa có job_id về pending (xoá decided_at,
// decided_by): duyệt rồi mà job không bắt đầu được (khoá đang bận). Đã có job_id
// thì ErrPlanNotPending (Plan đã chạy, không mở lại được); không có thì ErrNotFound.
func (s *Store) ReopenPlan(ctx context.Context, id string) error {
	n, err := affected(s.db.ExecContext(ctx,
		`UPDATE plans SET status = 'pending', decided_at = NULL, decided_by = ''
		 WHERE id = ? AND status = 'applied' AND job_id = ''`, id))
	if err != nil {
		return fmt.Errorf("mở lại Plan %s: %w", id, err)
	}
	if n == 0 {
		return s.notPending(ctx, "mở lại Plan", id)
	}
	return nil
}

// SetPlanJob ghi job_id cho một Plan đã applied. Plan không có thì ErrNotFound;
// Plan chưa applied thì lỗi thường, vì gán job cho Plan chưa được duyệt là lỗi lập trình.
func (s *Store) SetPlanJob(ctx context.Context, id, jobID string) error {
	// job_id rỗng nghĩa là "chưa có job" (ReopenPlan dựa vào đó), nên không ghi rỗng.
	if jobID == "" {
		return fmt.Errorf("gán job cho Plan %s: job id rỗng", id)
	}
	n, err := affected(s.db.ExecContext(ctx,
		"UPDATE plans SET job_id = ? WHERE id = ? AND status = 'applied'", jobID, id))
	if err != nil {
		return fmt.Errorf("gán job cho Plan %s: %w", id, err)
	}
	if n == 0 {
		st, err := s.planStatus(ctx, id)
		if err != nil {
			return fmt.Errorf("gán job cho Plan %s: %w", id, err)
		}
		return fmt.Errorf("gán job cho Plan %s: Plan đang %s, chỉ gán được cho Plan applied", id, st)
	}
	return nil
}
