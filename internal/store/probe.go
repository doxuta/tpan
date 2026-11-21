package store

// Sổ phép thử tấn công (M17, bảng probe_runs của migration 1700; mục 3.9 của
// docs/thay-doi/M17-thiet-ke.md). Các method ở đây cài đặt core.ProbeStore;
// store không import core nên khẳng định "đúng chữ ký" nằm trong probe_test.go.
//
// Dòng được ghi TRƯỚC khi phép thử tạo bất cứ thứ gì trên máy, và cleaned chỉ
// thành 1 khi đã dọn hết: nhờ vậy lượt thử chết giữa chừng vẫn để lại dấu để
// `tpan doctor` tìm ra và dọn.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/cage"
)

const probeColumns = "id, started_at, finished_at, state, domain_a, domain_b, mode, cleaned, report"

func scanProbeRun(r rowScanner) (cage.ProbeRun, error) {
	var (
		p        cage.ProbeRun
		started  string
		finished sql.NullString
		cleaned  int
		report   string
	)
	if err := r.Scan(&p.ID, &started, &finished, &p.State, &p.DomainA, &p.DomainB, &p.Mode, &cleaned, &report); err != nil {
		return cage.ProbeRun{}, err
	}
	var err error
	if p.Started, err = parseTime(started); err != nil {
		return cage.ProbeRun{}, fmt.Errorf("lượt thử %s: %w", p.ID, err)
	}
	if finished.Valid {
		if p.Finished, err = parseTime(finished.String); err != nil {
			return cage.ProbeRun{}, fmt.Errorf("lượt thử %s: %w", p.ID, err)
		}
	}
	p.Cleaned = cleaned != 0
	if report != "" {
		p.Report = []byte(report)
	}
	return p, nil
}

// StartProbeRun ghi dòng mới: State "running", Cleaned false, chưa có báo cáo,
// bất kể r mang gì ở ba trường đó (người gọi không được bắt đầu một lượt đã
// "xong").
func (s *Store) StartProbeRun(ctx context.Context, r cage.ProbeRun) error {
	if r.ID == "" {
		return errors.New("ghi lượt thử: thiếu id")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO probe_runs (id, started_at, state, domain_a, domain_b, mode)
		 VALUES (?, ?, 'running', ?, ?, ?)`,
		r.ID, fmtTime(r.Started), r.DomainA, r.DomainB, r.Mode)
	if err != nil {
		return fmt.Errorf("ghi lượt thử %s: %w", r.ID, err)
	}
	return nil
}

// FinishProbeRun đặt State ("done" hay "failed"), thời điểm xong và báo cáo.
// Báo cáo là JSON, lưu nguyên văn dạng TEXT (bảng STRICT không nhận BLOB vào
// cột TEXT, nên đổi []byte sang string trước khi ghi). Không có dòng thì
// ErrNotFound.
func (s *Store) FinishProbeRun(ctx context.Context, id, state string, report []byte, at time.Time) error {
	if state != "done" && state != "failed" {
		return fmt.Errorf("kết thúc lượt thử %s: trạng thái %q phải là done hoặc failed", id, state)
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE probe_runs SET state = ?, finished_at = ?, report = ? WHERE id = ?",
		state, fmtTime(at), string(report), id)
	if err != nil {
		return fmt.Errorf("kết thúc lượt thử %s: %w", id, err)
	}
	return needRow(res, "lượt thử "+id, "kết thúc lượt thử "+id)
}

// MarkProbeCleaned đặt Cleaned = true. Không có dòng thì ErrNotFound.
func (s *Store) MarkProbeCleaned(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE probe_runs SET cleaned = 1 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("đánh dấu lượt thử %s đã dọn: %w", id, err)
	}
	return needRow(res, "lượt thử "+id, "đánh dấu lượt thử "+id+" đã dọn")
}

// ProbeRun trả một dòng; ok=false khi không có.
func (s *Store) ProbeRun(ctx context.Context, id string) (r cage.ProbeRun, ok bool, err error) {
	r, err = scanProbeRun(s.db.QueryRowContext(ctx, "SELECT "+probeColumns+" FROM probe_runs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return cage.ProbeRun{}, false, nil
	}
	if err != nil {
		return cage.ProbeRun{}, false, fmt.Errorf("đọc lượt thử %s: %w", id, err)
	}
	return r, true, nil
}

// ProbeRuns trả tối đa limit dòng mới nhất, mới trước. Luôn là slice không nil.
func (s *Store) ProbeRuns(ctx context.Context, limit int) ([]cage.ProbeRun, error) {
	if limit < 0 {
		limit = 0 // SQLite coi LIMIT âm là không giới hạn; ở đây âm nghĩa là không lấy gì
	}
	out, err := s.probeRuns(ctx, "ORDER BY started_at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("đọc sổ phép thử: %w", err)
	}
	return out, nil
}

// PendingProbeRuns trả mọi dòng có Cleaned = false, cũ trước.
func (s *Store) PendingProbeRuns(ctx context.Context) ([]cage.ProbeRun, error) {
	out, err := s.probeRuns(ctx, "WHERE cleaned = 0 ORDER BY started_at, id")
	if err != nil {
		return nil, fmt.Errorf("đọc các lượt thử chưa dọn: %w", err)
	}
	return out, nil
}

// probeRuns chạy SELECT với phần đuôi (WHERE, ORDER BY, LIMIT) cho trước.
func (s *Store) probeRuns(ctx context.Context, tail string, args ...any) ([]cage.ProbeRun, error) {
	out := []cage.ProbeRun{}
	err := queryEach(ctx, s.db, "SELECT "+probeColumns+" FROM probe_runs "+tail, func(r rowScanner) error {
		p, err := scanProbeRun(r)
		if err != nil {
			return err
		}
		out = append(out, p)
		return nil
	}, args...)
	if err != nil {
		return nil, err
	}
	return out, nil
}
