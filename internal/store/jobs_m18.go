package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/engine"
)

// AbortStaleJobs đóng các job thuộc kinds còn ở trạng thái running: status
// rolled_back, finished_at là giờ hiện tại, error là reason (M18, R12 của bản
// soát hẹp). Trả số job đã đóng.
//
// Chỉ gọi khi chắc không job nào của các loại đó còn sống. Lượt đẩy lên kho
// gọi hàm này lúc đang giữ khoá lượt đẩy, mà mọi job backup.push, backup.pull,
// backup.panel chỉ chạy dưới khoá đó: job nào còn running là của một tiến
// trình đã chết (tpan serve khởi động lại giữa lượt). Không đóng thì Lịch sử
// giữ một job "đang chạy" vĩnh viễn.
func (s *Store) AbortStaleJobs(ctx context.Context, kinds []string, reason string) (int, error) {
	if len(kinds) == 0 {
		return 0, nil
	}
	args := []any{string(engine.JobRolledBack), fmtTime(s.now()), reason, string(engine.JobRunning)}
	for _, k := range kinds {
		args = append(args, k)
	}
	q := "UPDATE jobs SET status = ?, finished_at = ?, error = ? WHERE status = ? AND kind IN (?" +
		strings.Repeat(", ?", len(kinds)-1) + ")"
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("đóng job đứt của %v: %w", kinds, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("đóng job đứt của %v: %w", kinds, err)
	}
	return int(n), nil
}

// CountJobs đếm job loại kind của owner bắt đầu từ since trở đi, mọi trạng
// thái (M19, S17: trần job site.ssl tự chạy sau thay đổi tên phụ). started_at
// ghi bằng fmtTime (UTC, độ dài cố định) nên so chuỗi là so thời gian.
func (s *Store) CountJobs(ctx context.Context, kind, owner string, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM jobs WHERE kind = ? AND owner = ? AND started_at >= ?",
		kind, owner, fmtTime(since)).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm job %s của %s: %w", kind, owner, err)
	}
	return n, nil
}
