package core

// Số liệu tài nguyên của site trong lồng và gợi ý trần (M17, mục 3.8 của
// docs/thay-doi/M17-thiet-ke.md). Hợp đồng đợt 0: chữ ký và tài liệu; thân hàm
// do thợ đợt 2 viết.

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

var errUsageTODO = errors.New("số liệu của lồng: chưa viết (M17 đợt 2)")

// Các mốc thời gian của số liệu.
const (
	UsageSampleEvery = 5 * time.Minute     // nhịp lấy mẫu
	UsageKeepSamples = 8 * 24 * time.Hour  // giữ mẫu 5 phút
	UsageKeepHours   = 30 * 24 * time.Hour // giữ dòng gộp theo giờ
	UsageSuggestOver = 7 * 24 * time.Hour  // cửa sổ của gợi ý
	UsageLowData     = 288                 // dưới chừng này mẫu (24 giờ) là ít dữ liệu
)

// Usages gom những gì phần số liệu cần. Dựng mới cho mỗi lệnh.
type Usages struct {
	Sys      system.System
	Store    SiteStore
	Accounts AccountStore // nil: mọi site thuộc gói default
	Samples  UsageStore   // nil: Site, List vẫn chạy (đọc cgroup); Sample, History, Suggest lỗi
	Now      func() time.Time
}

// Site đọc ngay số liệu của một site trong lồng (cage.ReadUsage). Site không
// có: site_not_found. Site không ở trong lồng: usage_not_caged (site ngoài
// lồng chạy chung một dịch vụ PHP, không có cgroup riêng để đọc).
func (u *Usages) Site(ctx context.Context, d validate.Domain) (SiteUsage, error) {
	return SiteUsage{}, errUsageTODO
}

// List đọc ngay số liệu của mọi site active trong lồng của owner ("" là mọi
// người), sắp theo domain.
func (u *Usages) List(ctx context.Context, owner string) ([]SiteUsage, error) {
	return nil, errUsageTODO
}

// History trả lịch sử của site: rng "day" (mẫu 5 phút của 24 giờ qua), "week"
// (dòng giờ của 7 ngày), "month" (dòng giờ của 30 ngày). rng khác: args_invalid.
func (u *Usages) History(ctx context.Context, d validate.Domain, rng string) (UsageHistory, error) {
	return UsageHistory{}, errUsageTODO
}

// Sample ghi một mẫu cho mọi site caged và active; trả số mẫu đã ghi. Site
// đọc lỗi thì bỏ qua site đó (ghi lỗi vào kết quả err gộp), các site khác vẫn ghi.
func (u *Usages) Sample(ctx context.Context) (int, error) {
	return 0, errUsageTODO
}

// Rollup gộp mẫu của mọi giờ đã trọn mà chưa gộp vào usage_hourly (tính lại
// giờ gần nhất mỗi lần là đủ: SaveUsageHour ghi đè), rồi xoá mẫu cũ hơn
// UsageKeepSamples và dòng giờ cũ hơn UsageKeepHours. Hàm thuần tính một giờ
// là rollupHour.
func (u *Usages) Rollup(ctx context.Context) error {
	return errUsageTODO
}

// Suggest tính gợi ý trần cho một site trong lồng từ mẫu của UsageSuggestOver
// gần nhất (hàm thuần suggest).
func (u *Usages) Suggest(ctx context.Context, d validate.Domain) (Suggestion, error) {
	return Suggestion{}, errUsageTODO
}

// Suggestions tính gợi ý cho mọi site trong lồng và cho từng gói (lớn nhất
// trong các site của gói).
func (u *Usages) Suggestions(ctx context.Context) (SuggestionReport, error) {
	return SuggestionReport{}, errUsageTODO
}

// suggest là phép tính của mục 3.8, hàm thuần: samples đã sắp theo thời gian,
// cur là trần hiện tại của gói. Có bảng test (ít mẫu, có oom_kill, bộ đếm về
// 0 giữa chừng, bị bóp CPU, không có mẫu nào).
func suggest(domain, pkg string, samples []cage.Sample, cur account.Limits) Suggestion {
	return Suggestion{Domain: domain, Package: pkg, Current: cur}
}

// rollupHour gộp các mẫu của một giờ (đã sắp, cùng domain; prev là mẫu ngay
// trước giờ đó nếu có, để tính phần tăng của mẫu đầu). Hàm thuần.
func rollupHour(domain string, hour time.Time, prev *cage.Sample, samples []cage.Sample) cage.Hour {
	return cage.Hour{Domain: domain, Hour: hour}
}
