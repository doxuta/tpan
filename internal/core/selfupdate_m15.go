package core

// Thân các hàm của SelfUpdate thêm ở M15 (hợp đồng ở types_m15.go). Đợt 0 chỉ
// có chữ ký; thợ đợt 1 viết thân.

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/engine"
)

var errM15Stub = errors.New("M15: chưa viết thân hàm")

// Check hỏi gốc tải xem bản mới nhất là gì (hợp đồng ở types_m15.go).
func (s *SelfUpdate) Check(_ context.Context) (SelfCheck, error) {
	return SelfCheck{}, errM15Stub
}

// Busy: đơn vị tạm SelfUnit đang chạy?
func (s *SelfUpdate) Busy(_ context.Context) (bool, error) {
	return false, errM15Stub
}

// Last đọc lượt chạy gần nhất từ SettingSelfLast.
func (s *SelfUpdate) Last(_ context.Context) (SelfRun, bool, error) {
	return SelfRun{}, false, errM15Stub
}

// RollbackPreview trả tên bản đang chạy và bản sẽ quay về.
func (s *SelfUpdate) RollbackPreview(_ context.Context) (SelfRollbackPreview, error) {
	return SelfRollbackPreview{}, errM15Stub
}

// Start khởi đơn vị tạm chạy một lượt cập nhật hoặc quay về.
func (s *SelfUpdate) Start(_ context.Context, _, _, _ string, _ time.Time) (SelfRun, error) {
	return SelfRun{}, errM15Stub
}

// Run là thân của `tpan internal self-update`, chạy trong đơn vị tạm.
func (s *SelfUpdate) Run(_ context.Context, _ string, _ func() time.Time, _ func(engine.Event)) (SelfRun, engine.Report, error) {
	return SelfRun{}, engine.Report{}, errM15Stub
}
