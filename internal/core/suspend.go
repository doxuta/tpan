package core

// Tạm khoá site của M13 (mục 6.12 của docs/KE-HOACH.md). Đợt 0 chỉ có chữ
// ký; thân hàm do thợ "tạm khoá site" viết ở đợt 1.

import (
	"context"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// PrepareSuspend dựng Plan tạm khoá một site (Suspended = SuspendSelf): vhost
// bản "tạm khoá", pool và cron đổi tên .suspended, kiểm chứng ra 403 đúng trang
// tạm khoá. Từ chối site trong thùng rác hoặc đã khoá.
func (s *Sites) PrepareSuspend(ctx context.Context, d validate.Domain) (*Planned, error) {
	return nil, errM13NotYet
}

// PrepareUnsuspend làm ngược lại và kiểm site trả 200. Mở cả site bị khoá vì
// user (SuspendByUser): admin bấm mở một site là ý của admin.
func (s *Sites) PrepareUnsuspend(ctx context.Context, d validate.Domain) (*Planned, error) {
	return nil, errM13NotYet
}

// suspendSteps là các bước tạm khoá site st (dùng cho PrepareSuspend và cho
// khoá user). Không ghi bảng sites: người gọi ghi sau khi job thành công.
func (s *Sites) suspendSteps(ctx context.Context, st site.Site) ([]engine.Step, error) {
	return nil, errM13NotYet
}

// unsuspendSteps là các bước mở khoá site st.
func (s *Sites) unsuspendSteps(ctx context.Context, st site.Site) ([]engine.Step, error) {
	return nil, errM13NotYet
}

// suspendedChecks là phép kiểm chứng của site đang tạm khoá: gọi trên máy ra
// 403 và đúng trang tạm khoá.
func suspendedChecks(st site.Site) []checks.Check { return nil }
