package store

// Đổi chủ một site (admin). Chủ của site nằm ở ba bảng: sites.owner,
// site_names.owner (bản chép cho mọi tên của site) và backups.owner (để chủ
// thấy và khôi phục được bản backup). Cả ba đổi trong một transaction: lệch
// nhau thì luật "đúng chủ" của server trả lời khác nhau tuỳ màn.
//
// Không đổi: jobs.owner (lịch sử: job cũ vẫn thuộc người đã chạy nó) và DB rời
// (thuộc user, không thuộc site). DB đi kèm site, user Linux, pool PHP đi theo
// slug của domain nên không phụ thuộc chủ.

import (
	"context"
	"fmt"

	"github.com/doxuta/tpan/internal/validate"
)

// SetSiteOwner ghi chủ mới cho site d. Không có site thì lỗi thoả
// errors.Is(err, ErrNotFound). owner rỗng là admin.
func (s *Store) SetSiteOwner(ctx context.Context, d validate.Domain, owner string) error {
	if d.IsZero() {
		return fmt.Errorf("đổi chủ site: thiếu domain")
	}
	if owner == "" {
		owner = adminName
	}
	dom := d.String()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("đổi chủ site %s: %w", dom, err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, "UPDATE sites SET owner = ? WHERE domain = ?", owner, dom)
	if err != nil {
		return fmt.Errorf("đổi chủ site %s: %w", dom, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("đổi chủ site %s: %w", dom, err)
	} else if n == 0 {
		return fmt.Errorf("đổi chủ site %s: %w", dom, ErrNotFound)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE site_names SET owner = ? WHERE site = ?", owner, dom); err != nil {
		return fmt.Errorf("đổi chủ các tên của site %s: %w", dom, err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE backups SET owner = ? WHERE domain = ?", owner, dom); err != nil {
		return fmt.Errorf("đổi chủ backup của site %s: %w", dom, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("đổi chủ site %s: %w", dom, err)
	}
	return nil
}
