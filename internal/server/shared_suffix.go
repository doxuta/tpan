package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/doxuta/tpan/internal/core"
)

// sharedRefreshEvery: tpan serve nạp lại danh sách tên miền dùng chung từ
// settings nhiều nhất mỗi 3 giây. Danh sách đổi bằng dòng lệnh (tiến trình
// khác), nên server không được báo; đọc một dòng settings mỗi 3 giây là đủ rẻ.
const sharedRefreshEvery = 3 * time.Second

// refreshShared nạp danh sách tên miền dùng chung trước khi một request có
// đăng nhập chạy. Lỗi đọc settings chỉ ghi log: giữ danh sách đang có.
func (s *Server) refreshShared(ctx context.Context) {
	if s.deps.Store == nil {
		return
	}
	now := time.Now().UnixNano()
	last := s.sharedAt.Load()
	if last != 0 && now-last < int64(sharedRefreshEvery) {
		return
	}
	if !s.sharedAt.CompareAndSwap(last, now) {
		return
	}
	if err := core.LoadSharedSuffixes(ctx, s.deps.Store); err != nil {
		slog.Error("nạp tên miền dùng chung", "err", err)
	}
}
