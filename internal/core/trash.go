package core

// Hợp đồng M6, đợt 0: thùng rác (mục 6.4 của docs/KE-HOACH.md). Thân viết ở
// đợt 2. Chữ ký ở đây là thứ CLI, API và bộ hẹn giờ trong `tpan serve` dựa vào.
//
// Ba thao tác, mỗi cái một Plan, cùng khuôn với tạo site (Prepare* chỉ đọc,
// Preview, Execute ghi bảng sites khi job thành công):
//
//   - Đưa vào thùng rác: Sites.PrepareRemove ở site.go (giữ tên cũ để
//     propose_site_remove của M7 và `site rm` không phải đổi chỗ gọi). Plan
//     "site.rm" đổi từ xoá hẳn sang các bước ở mục 6.4: gỡ link vhost và pool
//     (Test, Reload), khoá user DB, khoá user Linux, chuyển thư mục site vào
//     site.TrashDir. Mọi bước hoàn tác được, nên Plan có Verify: domain không
//     còn được phục vụ (curl --resolve trả 404 hoặc 444/403 của site mặc định).
//     Job thành công: Status trashed, TrashedAt = Now, PurgeAt = Now + số ngày
//     giữ (TrashDays).
//   - Khôi phục (PrepareRestore): làm ngược lại rồi chạy bước kiểm chứng của
//     6.2 (siteChecks, bản SSL nếu site.SSL; vhost qua siteVhost của M4/M5).
//     Từ chối site_not_trashed nếu site không ở thùng rác, site_exists_unmanaged
//     nếu /var/www/<domain> đã có (domain đã được tạo lại ngoài tpan). Job
//     thành công: Status active, TrashedAt và PurgeAt về zero.
//   - Xoá hẳn (PreparePurge): dùng lại Plan xoá hẳn của M2 (gỡ web nếu còn, xoá
//     DB và user DB, RemoveTree, userdel, certbot delete nếu SSL), chỉ khác:
//     RemoveTree{Path: site.TrashDir(l), Under: site.TrashRoot} thay cho
//     l.Dir. Không có Verify (lý do ở mục 6.4). Từ chối site_not_trashed nếu
//     site đang active: xoá hẳn chỉ đi qua thùng rác. Job thành công: xoá dòng.
//     Chưa có "tự backup trước khi xoá hẳn" (mục 6.5): backup là M9.

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// Khoá setting của thùng rác. Giá trị là số ngày, chuỗi thập phân.
const (
	SettingTrashDays = "trash.days"
	// DefaultTrashDays: mục 6.4, "mặc định 7 ngày".
	DefaultTrashDays = 7
	// MaxTrashDays: trần để một lỗi gõ (7000) không giữ dữ liệu vô hạn.
	MaxTrashDays = 365
)

var errTrashTODO = errors.New("core: thùng rác chưa viết (M6 đợt 2)")

// TrashDays đọc số ngày giữ thùng rác; chưa đặt thì DefaultTrashDays. 0 hợp lệ
// (hết hạn ngay, dùng cho e2e và cho người không muốn giữ).
func (s *Sites) TrashDays(ctx context.Context) (int, error) { return 0, errTrashTODO }

// SetTrashDays đặt số ngày giữ, 0..MaxTrashDays; ngoài khoảng thì từ chối
// trash_days_invalid. Chỉ áp cho site vào thùng rác SAU lần đặt.
func (s *Sites) SetTrashDays(ctx context.Context, days int) error { return errTrashTODO }

// Trash trả các site đang ở thùng rác, sắp theo PurgeAt (sắp hết hạn trước).
func (s *Sites) Trash(ctx context.Context) ([]site.Site, error) { return nil, errTrashTODO }

// Expired trả các site trong thùng rác có PurgeAt ≤ now, sắp theo PurgeAt.
func (s *Sites) Expired(ctx context.Context, now time.Time) ([]site.Site, error) {
	return nil, errTrashTODO
}

// SiteRestore là một lần khôi phục site từ thùng rác.
type SiteRestore struct {
	Site   site.Site
	Layout site.Layout
	Plan   engine.Plan

	sites *Sites
}

// PrepareRestore dựng Plan "site.restore". Chỉ đọc.
func (s *Sites) PrepareRestore(ctx context.Context, d validate.Domain) (*SiteRestore, error) {
	return nil, errTrashTODO
}

// Preview là bản xem trước. Chỉ đọc máy.
func (r *SiteRestore) Preview(ctx context.Context) (engine.Preview, error) {
	_, _ = r.sites, r.sites.now
	return engine.Preview{}, errTrashTODO
}

// Execute chạy Plan; job thành công thì site về active trong bảng sites.
func (r *SiteRestore) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	return engine.Report{}, errTrashTODO
}

// SitePurge là một lần xoá hẳn một site đang ở thùng rác.
type SitePurge struct {
	Site   site.Site
	Layout site.Layout
	Plan   engine.Plan

	sites *Sites
}

// PreparePurge dựng Plan "site.purge". Chỉ đọc.
func (s *Sites) PreparePurge(ctx context.Context, d validate.Domain) (*SitePurge, error) {
	return nil, errTrashTODO
}

// Preview là bản xem trước. Chỉ đọc máy.
func (p *SitePurge) Preview(ctx context.Context) (engine.Preview, error) {
	_ = p.sites
	return engine.Preview{}, errTrashTODO
}

// Execute chạy Plan; job thành công thì xoá dòng của site khỏi bảng sites.
// Job hỏng thì giữ dòng (vẫn trashed) để chạy lại làm nốt.
func (p *SitePurge) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	return engine.Report{}, errTrashTODO
}

// PurgeResult là kết quả xoá hẳn một site trong PurgeExpired.
type PurgeResult struct {
	Domain validate.Domain `json:"domain"`
	Report engine.Report   `json:"report"`
	// Error: không dựng hay không chạy được Plan (khác job lỗi: xem Report).
	Error string `json:"error,omitempty"`
}

// PurgeExpired xoá hẳn lần lượt mọi site của Expired(now), mỗi site một job.
// Site lỗi thì ghi vào kết quả rồi đi tiếp site sau. Dùng cho `tpan trash
// purge --expired` và bộ hẹn giờ mỗi giờ trong `tpan serve`.
func (s *Sites) PurgeExpired(ctx context.Context, now time.Time, onEvent func(engine.Event)) ([]PurgeResult, error) {
	return nil, errTrashTODO
}
