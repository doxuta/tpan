package core

// Số liệu tài nguyên của site trong lồng và gợi ý trần (M17, mục 3.8 của
// docs/thay-doi/M17-thiet-ke.md).
//
// Ba tầng, tách để test từng tầng:
//   - đọc: cage.ReadUsage đọc cgroup ngay lúc gọi (Site, List: bất biến 6, không
//     qua bảng mẫu);
//   - ghi: Sample (5 phút một lần) và Rollup (mỗi giờ) nuôi bảng mẫu;
//   - tính: suggest và rollupHour là hàm thuần, chỉ nhận mẫu, không chạm máy.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Các mốc thời gian của số liệu.
const (
	UsageSampleEvery = 5 * time.Minute     // nhịp lấy mẫu
	UsageKeepSamples = 8 * 24 * time.Hour  // giữ mẫu 5 phút
	UsageKeepHours   = 30 * 24 * time.Hour // giữ dòng gộp theo giờ
	UsageSuggestOver = 7 * 24 * time.Hour  // cửa sổ của gợi ý
	UsageLowData     = 288                 // dưới chừng này mẫu (24 giờ) là ít dữ liệu
)

// Khoảng giữa hai mẫu dùng được để tính phần trăm CPU: ngắn hơn thì phép chia
// cho một mẫu số quá nhỏ, dài hơn thì panel đã tắt hay đồng hồ bị chỉnh.
const (
	usageMinGap = 1 * time.Minute
	usageMaxGap = 15 * time.Minute
)

// Usages gom những gì phần số liệu cần. Dựng mới cho mỗi lệnh.
type Usages struct {
	Sys      system.System
	Store    SiteStore
	Accounts AccountStore // nil: mọi site thuộc gói default
	Samples  UsageStore   // nil: Site, List vẫn chạy (đọc cgroup); Sample, History, Suggest lỗi
	Now      func() time.Time
}

var errUsageNoSamples = errors.New("chưa có bảng mẫu số liệu (không có tpan.db)")

func (u *Usages) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// find đọc site; không có thì site_not_found như mọi ca sử dụng khác.
func (u *Usages) find(ctx context.Context, d validate.Domain) (site.Site, error) {
	if d.IsZero() {
		return site.Site{}, refuse(validate.CodeDomainInvalid, i18n.T("thiếu domain", "the domain is missing"))
	}
	st, ok, err := u.Store.Site(ctx, d)
	if err != nil {
		return site.Site{}, fmt.Errorf("đọc site %s: %w", d, err)
	}
	if !ok {
		return site.Site{}, refuse("site_not_found", i18n.T(
			"không có site "+d.String()+" trong tpan; xem danh sách bằng: sudo tpan site list",
			"tpan has no site "+d.String()+"; list sites with: sudo tpan site list"))
	}
	return st, nil
}

func refuseNotCaged(d validate.Domain) *RefusedError {
	return refuse(CodeUsageNotCaged, i18n.T(
		"site "+d.String()+" không ở trong lồng nên không có số liệu riêng (site ngoài lồng dùng chung một dịch vụ PHP); đưa vào lồng bằng: sudo tpan cage enable "+d.String(),
		"site "+d.String()+" is not in a cage so it has no usage of its own (sites outside a cage share one PHP service); put it in with: sudo tpan cage enable "+d.String()))
}

// cagedActive là site mà bộ lấy mẫu và gợi ý quan tâm.
func cagedActive(st site.Site) bool { return st.Caged && st.Status == site.Active }

// read đọc cgroup của st ngay lúc này, kèm trần của gói.
func (u *Usages) read(ctx context.Context, st site.Site, now time.Time) (SiteUsage, error) {
	use, err := cage.ReadUsage(u.Sys, st.Layout(), now)
	if err != nil {
		return SiteUsage{}, fmt.Errorf("đọc số liệu của site %s: %w", st.Domain, err)
	}
	lim, _, err := cageLimitsOf(ctx, u.Accounts, st)
	if err != nil {
		return SiteUsage{}, fmt.Errorf("đọc trần của gói cho site %s: %w", st.Domain, err)
	}
	return SiteUsage{Domain: st.Domain.String(), Owner: st.Owner, Usage: use, Limits: lim}, nil
}

// Site đọc ngay số liệu của một site trong lồng (cage.ReadUsage). Site không
// có: site_not_found. Site không ở trong lồng: usage_not_caged (site ngoài
// lồng chạy chung một dịch vụ PHP, không có cgroup riêng để đọc).
func (u *Usages) Site(ctx context.Context, d validate.Domain) (SiteUsage, error) {
	st, err := u.find(ctx, d)
	if err != nil {
		return SiteUsage{}, err
	}
	if !st.Caged {
		return SiteUsage{}, refuseNotCaged(d)
	}
	return u.read(ctx, st, u.now())
}

// List đọc ngay số liệu của mọi site active trong lồng của owner ("" là mọi
// người), sắp theo domain.
func (u *Usages) List(ctx context.Context, owner string) ([]SiteUsage, error) {
	all, err := u.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	now := u.now()
	out := []SiteUsage{}
	for _, st := range all {
		if !cagedActive(st) || (owner != "" && st.Owner != owner) {
			continue
		}
		su, err := u.read(ctx, st, now)
		if err != nil {
			return nil, err
		}
		out = append(out, su)
	}
	slices.SortFunc(out, func(a, b SiteUsage) int { return strings.Compare(a.Domain, b.Domain) })
	return out, nil
}

// History trả lịch sử của site: rng "day" (mẫu 5 phút của 24 giờ qua), "week"
// (dòng giờ của 7 ngày), "month" (dòng giờ của 30 ngày). rng khác: args_invalid.
func (u *Usages) History(ctx context.Context, d validate.Domain, rng string) (UsageHistory, error) {
	var span time.Duration
	switch rng {
	case "day":
		span = 24 * time.Hour
	case "week":
		span = 7 * 24 * time.Hour
	case "month":
		span = 30 * 24 * time.Hour
	default:
		return UsageHistory{}, refuse("args_invalid", i18n.T(
			fmt.Sprintf("khoảng thời gian phải là day, week hoặc month (nhận được %q)", rng),
			fmt.Sprintf("the range must be day, week or month (got %q)", rng)))
	}
	if u.Samples == nil {
		return UsageHistory{}, fmt.Errorf("lịch sử số liệu của %s: %w", d, errUsageNoSamples)
	}
	now := u.now()
	from, to := now.Add(-span), now.Add(time.Second) // to là mốc mở: mẫu vừa ghi đúng lúc now vẫn vào
	h := UsageHistory{Domain: d.String(), Range: rng, Points: []UsagePoint{}}

	if rng != "day" {
		hours, err := u.Samples.UsageHours(ctx, d.String(), from, to)
		if err != nil {
			return UsageHistory{}, fmt.Errorf("đọc dòng giờ của %s: %w", d, err)
		}
		for _, x := range hours {
			h.Points = append(h.Points, UsagePoint{
				At: x.Hour, MemAvg: x.MemAvg, MemMax: x.MemMax,
				// Trung bình của giờ: tổng CPU dùng chia 3600 giây (36e6 micro giây là 1%).
				CPUPct:   float64(x.CPUUsec) / 36e6,
				TasksMax: x.TasksMax, OOMKills: x.OOMKills,
			})
		}
		return h, nil
	}

	// Lấy thêm một nhịp trước 24 giờ: mẫu đó chỉ làm "mẫu trước" của điểm đầu,
	// không thành điểm.
	samples, err := u.Samples.UsageSamples(ctx, d.String(), from.Add(-UsageSampleEvery), to)
	if err != nil {
		return UsageHistory{}, fmt.Errorf("đọc mẫu của %s: %w", d, err)
	}
	for i, s := range samples {
		if s.At.Before(from) {
			continue
		}
		p := UsagePoint{At: s.At, MemAvg: s.MemCurrent, MemMax: s.MemCurrent, TasksMax: s.Tasks}
		if i > 0 {
			dl := deltaOf(samples[i-1], s)
			p.OOMKills = dl.oom
			if dl.cpuPctOK {
				p.CPUPct = dl.cpuPct()
			}
			if dl.memPeakRose {
				p.MemMax = max(p.MemMax, s.MemPeak)
			}
			if dl.tasksPeakRose {
				p.TasksMax = max(p.TasksMax, s.TasksPeak)
			}
		}
		h.Points = append(h.Points, p)
	}
	return h, nil
}

// Sample ghi một mẫu cho mọi site caged và active; trả số mẫu đã ghi. Site
// đọc lỗi thì bỏ qua site đó (ghi lỗi vào kết quả err gộp), các site khác vẫn ghi.
func (u *Usages) Sample(ctx context.Context) (int, error) {
	if u.Samples == nil {
		return 0, fmt.Errorf("lấy mẫu số liệu: %w", errUsageNoSamples)
	}
	all, err := u.Store.Sites(ctx)
	if err != nil {
		return 0, fmt.Errorf("đọc danh sách site: %w", err)
	}
	now := u.now()
	n := 0
	var errs []error
	for _, st := range all {
		if !cagedActive(st) {
			continue
		}
		use, err := cage.ReadUsage(u.Sys, st.Layout(), now)
		if err != nil {
			errs = append(errs, fmt.Errorf("đọc số liệu của site %s: %w", st.Domain, err))
			continue
		}
		// SampleOf mang theo trần đang áp (mem_max, cpu_quota_pct, tasks_max):
		// luật "đã chạm trần" của suggest dùng trần của lúc xảy ra, không dùng
		// trần hôm nay của gói.
		if err := u.Samples.AddUsageSample(ctx, cage.SampleOf(st.Domain.String(), use)); err != nil {
			errs = append(errs, fmt.Errorf("ghi mẫu của site %s: %w", st.Domain, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// Rollup gộp mẫu của mọi giờ đã trọn mà chưa gộp vào usage_hourly (tính lại
// giờ gần nhất mỗi lần là đủ: SaveUsageHour ghi đè), rồi xoá mẫu cũ hơn
// UsageKeepSamples và dòng giờ cũ hơn UsageKeepHours. Hàm thuần tính một giờ
// là rollupHour.
func (u *Usages) Rollup(ctx context.Context) error {
	if u.Samples == nil {
		return fmt.Errorf("gộp số liệu theo giờ: %w", errUsageNoSamples)
	}
	now := u.now().UTC()
	curHour := now.Truncate(time.Hour) // giờ đang chạy dở: chưa gộp
	// Hai giờ trọn gần nhất luôn tính lại: mẫu cuối của một giờ có thể ghi sau
	// lượt gộp đã chạy đúng ranh giới giờ. Giờ cũ hơn đã có dòng thì giữ nguyên.
	redoFrom := curHour.Add(-2 * time.Hour)
	// Bắt đầu từ đầu giờ của mốc giữ mẫu để giờ nào cũng có đủ mẫu của nó.
	from := now.Add(-UsageKeepSamples).Truncate(time.Hour)

	domains, err := u.Samples.UsageSampleDomains(ctx, from, curHour)
	if err != nil {
		return fmt.Errorf("liệt kê domain có mẫu: %w", err)
	}
	var errs []error
	for _, dom := range domains {
		if err := u.rollupDomain(ctx, dom, from, curHour, redoFrom); err != nil {
			errs = append(errs, fmt.Errorf("gộp số liệu của %s: %w", dom, err))
		}
	}
	// Dọn ngay cả khi một domain lỗi: không để một site hỏng làm bảng phình mãi.
	// Chỉ mẫu quá 8 ngày bị xoá, mà giờ nào gộp được thì đã gộp từ lâu.
	if err := u.Samples.PruneUsage(ctx, now.Add(-UsageKeepSamples), now.Add(-UsageKeepHours)); err != nil {
		errs = append(errs, fmt.Errorf("xoá số liệu cũ: %w", err))
	}
	return errors.Join(errs...)
}

// rollupDomain gộp các giờ trọn của một domain trong [from, curHour).
func (u *Usages) rollupDomain(ctx context.Context, dom string, from, curHour, redoFrom time.Time) error {
	samples, err := u.Samples.UsageSamples(ctx, dom, from, curHour)
	if err != nil {
		return fmt.Errorf("đọc mẫu: %w", err)
	}
	have, err := u.Samples.UsageHours(ctx, dom, from, curHour)
	if err != nil {
		return fmt.Errorf("đọc dòng giờ: %w", err)
	}
	done := map[time.Time]bool{}
	for _, h := range have {
		done[h.Hour.UTC()] = true
	}
	var prev *cage.Sample
	for i := 0; i < len(samples); {
		hour := samples[i].At.UTC().Truncate(time.Hour)
		j := i
		for j < len(samples) && samples[j].At.UTC().Truncate(time.Hour).Equal(hour) {
			j++
		}
		if !done[hour] || !hour.Before(redoFrom) {
			if err := u.Samples.SaveUsageHour(ctx, rollupHour(dom, hour, prev, samples[i:j])); err != nil {
				return fmt.Errorf("ghi giờ %s: %w", hour.Format(time.RFC3339), err)
			}
		}
		last := samples[j-1]
		prev = &last
		i = j
	}
	return nil
}

// Suggest tính gợi ý trần cho một site trong lồng từ mẫu của UsageSuggestOver
// gần nhất (hàm thuần suggest).
func (u *Usages) Suggest(ctx context.Context, d validate.Domain) (Suggestion, error) {
	st, err := u.find(ctx, d)
	if err != nil {
		return Suggestion{}, err
	}
	if !st.Caged {
		return Suggestion{}, refuseNotCaged(d)
	}
	return u.suggestFor(ctx, st)
}

func (u *Usages) suggestFor(ctx context.Context, st site.Site) (Suggestion, error) {
	if u.Samples == nil {
		return Suggestion{}, fmt.Errorf("gợi ý trần của %s: %w", st.Domain, errUsageNoSamples)
	}
	p, err := sitePackage(ctx, u.Accounts, st)
	if err != nil {
		return Suggestion{}, fmt.Errorf("đọc gói của site %s: %w", st.Domain, err)
	}
	now := u.now()
	samples, err := u.Samples.UsageSamples(ctx, st.Domain.String(), now.Add(-UsageSuggestOver), now.Add(time.Second))
	if err != nil {
		return Suggestion{}, fmt.Errorf("đọc mẫu của %s: %w", st.Domain, err)
	}
	return suggest(st.Domain.String(), p.Name, samples, p.Limits()), nil
}

// Suggestions tính gợi ý cho mọi site trong lồng và cho từng gói (lớn nhất
// trong các site của gói).
func (u *Usages) Suggestions(ctx context.Context) (SuggestionReport, error) {
	all, err := u.Store.Sites(ctx)
	if err != nil {
		return SuggestionReport{}, fmt.Errorf("đọc danh sách site: %w", err)
	}
	rep := SuggestionReport{Sites: []Suggestion{}, Packages: []PackageSuggestion{}}
	byPkg := map[string]*PackageSuggestion{}
	for _, st := range all {
		if !cagedActive(st) {
			continue
		}
		sg, err := u.suggestFor(ctx, st)
		if err != nil {
			return SuggestionReport{}, err
		}
		rep.Sites = append(rep.Sites, sg)
		ps := byPkg[sg.Package]
		if ps == nil {
			ps = &PackageSuggestion{Package: sg.Package, Sites: []string{}, Current: sg.Current}
			byPkg[sg.Package] = ps
		}
		ps.Sites = append(ps.Sites, sg.Domain)
		ps.Suggested.RAMMB = max(ps.Suggested.RAMMB, sg.Suggested.RAMMB)
		ps.Suggested.CPUPercent = max(ps.Suggested.CPUPercent, sg.Suggested.CPUPercent)
		ps.Suggested.Tasks = max(ps.Suggested.Tasks, sg.Suggested.Tasks)
	}
	for _, ps := range byPkg {
		rep.Packages = append(rep.Packages, *ps)
	}
	slices.SortFunc(rep.Packages, func(a, b PackageSuggestion) int { return strings.Compare(a.Package, b.Package) })
	return rep, nil
}

// ---------------------------------------------------------------- hàm thuần

// delta là phần thay đổi giữa hai mẫu liên tiếp của cùng một domain. Mọi chỗ
// cần "phần tăng" (suggest, rollupHour, History) đi qua đây, để luật về thế hệ
// cgroup chỉ có một bản.
type delta struct {
	// fresh: hai mẫu khác thế hệ (cpu_usec giảm: slice bị tạo lại hay máy khởi
	// động lại). Khi đó phần tăng của mọi bộ đếm là giá trị mới.
	fresh bool

	cpu, throttled, nrThrottled int64
	oom, tasksHits              int64
	ioR, ioW                    int64

	// dt là khoảng giữa hai mẫu; cpuPctOK khi dt trong [1 phút, 15 phút].
	dt       time.Duration
	cpuPctOK bool

	// memPeakRose, tasksPeakRose: đỉnh của mẫu sau xảy ra trong khoảng này (đỉnh
	// tăng so với mẫu trước, hay slice mới). Đỉnh không tăng có thể là của ba
	// tháng trước (memory.peak, pids.peak tính từ khi slice có): không dùng.
	memPeakRose, tasksPeakRose bool
}

func deltaOf(a, b cage.Sample) delta {
	d := delta{fresh: b.CPUUsec < a.CPUUsec, dt: b.At.Sub(a.At)}
	d.cpuPctOK = d.dt >= usageMinGap && d.dt <= usageMaxGap
	d.cpu = counterInc(a.CPUUsec, b.CPUUsec, d.fresh)
	d.throttled = counterInc(a.ThrottledUsec, b.ThrottledUsec, d.fresh)
	d.nrThrottled = counterInc(a.NrThrottled, b.NrThrottled, d.fresh)
	d.oom = counterInc(a.OOMKills, b.OOMKills, d.fresh)
	d.tasksHits = counterInc(a.TasksHits, b.TasksHits, d.fresh)
	d.ioR = counterInc(a.IORBytes, b.IORBytes, d.fresh)
	d.ioW = counterInc(a.IOWBytes, b.IOWBytes, d.fresh)
	d.memPeakRose = d.fresh || b.MemPeak > a.MemPeak
	d.tasksPeakRose = d.fresh || b.TasksPeak > a.TasksPeak
	return d
}

// counterInc là phần tăng của một bộ đếm. Khác thế hệ: giá trị mới. Cùng thế
// hệ mà bộ đếm giảm (tasks_hits trên kernel 6.8 là tổng của các cgroup con, mất
// đi khi lệnh tạm xong): coi là không tăng, không lấy giá trị còn lại làm phần tăng.
func counterInc(prev, cur int64, fresh bool) int64 {
	switch {
	case fresh:
		return cur
	case cur < prev:
		return 0
	}
	return cur - prev
}

// cpuPct là phần trăm của một nhân dùng trong khoảng. Chỉ có nghĩa khi cpuPctOK.
func (d delta) cpuPct() float64 {
	return float64(d.cpu) * 100 / float64(d.dt.Microseconds())
}

// percentile95 là phân vị 95 kiểu hạng gần nhất (nearest-rank) của xs.
func percentile95(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	return s[int(math.Ceil(0.95*float64(len(s))))-1]
}

// roundUpTo làm tròn x lên bội của m. Trừ một lượng nhỏ trước khi lấy trần để
// sai số dấu phẩy động (20,000000000000004) không đẩy một số tròn lên bội kế.
func roundUpTo(x float64, m int) int {
	return int(math.Ceil(x/float64(m)-1e-9)) * m
}

// suggest là phép tính của mục 3.8, hàm thuần: samples đã sắp theo thời gian,
// cur là trần hiện tại của gói. Có bảng test (ít mẫu, có oom_kill, bộ đếm về
// 0 giữa chừng, bị bóp CPU, không có mẫu nào).
func suggest(domain, pkg string, samples []cage.Sample, cur account.Limits) Suggestion {
	sg := Suggestion{
		Domain: domain, Package: pkg, Samples: len(samples), Current: cur,
		LowData: len(samples) < UsageLowData,
	}
	if len(samples) == 0 {
		sg.Reasons = []i18n.Text{i18n.T(
			"chưa có mẫu nào của site này trong 7 ngày qua nên chưa gợi ý được; bộ lấy mẫu chạy 5 phút một lần trong tpan serve",
			"there are no samples of this site in the last 7 days, so there is nothing to suggest yet; the sampler runs every 5 minutes inside tpan serve")}
		return sg
	}
	sg.From, sg.To = samples[0].At, samples[len(samples)-1].At

	// Đỉnh ban đầu: lớn nhất của mem_current và tasks (hiện tại) trên mọi mẫu.
	// memory.peak và pids.peak chỉ vào khi chúng tăng trong cửa sổ (deltaOf).
	var memPeak, tasksPeak int64
	var cpuPcts []float64
	var throttled, observed int64       // micro giây bị bóp, micro giây của các khoảng dùng được
	var oomMax, quotaMax, hitsMax int64 // trần của lúc xảy ra, lấy lớn nhất
	for i, s := range samples {
		memPeak = max(memPeak, s.MemCurrent)
		tasksPeak = max(tasksPeak, s.Tasks)
		if i == 0 {
			continue
		}
		d := deltaOf(samples[i-1], s)
		if d.memPeakRose {
			memPeak = max(memPeak, s.MemPeak)
		}
		if d.tasksPeakRose {
			tasksPeak = max(tasksPeak, s.TasksPeak)
		}
		sg.OOMKills += d.oom
		if d.oom > 0 {
			oomMax = max(oomMax, s.MemMax)
		}
		sg.TasksHits += d.tasksHits
		if d.tasksHits > 0 {
			hitsMax = max(hitsMax, s.TasksMax)
		}
		if d.cpuPctOK {
			cpuPcts = append(cpuPcts, d.cpuPct())
			throttled += d.throttled
			observed += d.dt.Microseconds()
			if d.throttled > 0 {
				quotaMax = max(quotaMax, s.CPUQuotaPct)
			}
		}
	}
	sg.MemPeak, sg.TasksPeak = memPeak, tasksPeak
	sg.CPUP95 = percentile95(cpuPcts)
	if observed > 0 {
		sg.ThrottledPct = float64(throttled) * 100 / float64(observed)
	}

	// RAM: đỉnh × 1,25 (tính trên số nguyên: ceil(MB × 5 / 4)), bội của 32, sàn 128.
	peakMB := int((memPeak + mib - 1) / mib)
	ram := (peakMB*5 + 3) / 4
	var ramNote i18n.Text
	if sg.OOMKills > 0 && oomMax > 0 {
		// Số đo đã bị chính trần cắt: đỉnh thật cao hơn nó.
		floor := int(math.Ceil(float64(oomMax) * 1.5 / mib))
		if floor > ram {
			ram = floor
		}
		ramNote = i18n.T(
			fmt.Sprintf(" Có %d lần bị giết vì chạm trần RAM (trần lúc đó %d MB) nên số đo đã bị cắt: không thấp hơn trần đó × 1,5 = %d MB.", sg.OOMKills, oomMax/mib, floor),
			fmt.Sprintf(" Killed %d times at the RAM limit (limit then %d MB), so the reading was cut off: not below that limit x 1.5 = %d MB.", sg.OOMKills, oomMax/mib, floor))
	}
	sg.Suggested.RAMMB = max(roundUpTo(float64(ram), 32), 128)

	// CPU: p95 × 1,5, bội của 10, sàn 25.
	cpu := sg.CPUP95 * 1.5
	var cpuNote i18n.Text
	if sg.ThrottledPct > 1 && quotaMax > 0 {
		floor := float64(quotaMax) * 1.5
		cpu = math.Max(cpu, floor)
		cpuNote = i18n.T(
			fmt.Sprintf(" Site bị bóp CPU %.1f%% thời gian (quá 1%%) nên không thấp hơn trần lúc đó (%d%%) × 1,5 = %.0f%%.", sg.ThrottledPct, quotaMax, floor),
			fmt.Sprintf(" The site was CPU-throttled %.1f%% of the time (over 1%%), so not below the limit then (%d%%) x 1.5 = %.0f%%.", sg.ThrottledPct, quotaMax, floor))
	}
	sg.Suggested.CPUPercent = max(roundUpTo(cpu, 10), 25)

	// Tiến trình: đỉnh × 2, bội của 10, không dưới max(32, worker + CageTasksSlack)
	// để gợi ý không bao giờ là thứ chính `package edit` từ chối.
	tasks := float64(tasksPeak) * 2
	var tasksNote i18n.Text
	if sg.TasksHits > 0 && hitsMax > 0 {
		floor := float64(hitsMax) * 1.5
		tasks = math.Max(tasks, floor)
		tasksNote = i18n.T(
			fmt.Sprintf(" Có %d lần chạm trần tiến trình (trần lúc đó %d) nên không thấp hơn trần đó × 1,5 = %.0f.", sg.TasksHits, hitsMax, floor),
			fmt.Sprintf(" Hit the task limit %d times (limit then %d), so not below that limit x 1.5 = %.0f.", sg.TasksHits, hitsMax, floor))
	}
	tasksMin := max(32, cur.Workers()+CageTasksSlack)
	sg.Suggested.Tasks = max(roundUpTo(tasks, 10), tasksMin)

	sg.Reasons = []i18n.Text{
		i18n.T(
			fmt.Sprintf("RAM %d MB: đỉnh %d MB trong %d mẫu × 1,25, làm tròn lên bội của 32, không dưới 128 MB.%s Số đo gồm cả page cache và OPcache nên nghiêng về phía an toàn.",
				sg.Suggested.RAMMB, peakMB, sg.Samples, ramNote.Vi),
			fmt.Sprintf("RAM %d MB: peak %d MB over %d samples x 1.25, rounded up to a multiple of 32, at least 128 MB.%s The reading includes page cache and OPcache, so it errs on the safe side.",
				sg.Suggested.RAMMB, peakMB, sg.Samples, ramNote.En)),
		i18n.T(
			fmt.Sprintf("CPU %d%%: phân vị 95 của mức dùng giữa các mẫu là %.1f%% × 1,5, làm tròn lên bội của 10, không dưới 25%%.%s",
				sg.Suggested.CPUPercent, sg.CPUP95, cpuNote.Vi),
			fmt.Sprintf("CPU %d%%: the 95th percentile of usage between samples is %.1f%% x 1.5, rounded up to a multiple of 10, at least 25%%.%s",
				sg.Suggested.CPUPercent, sg.CPUP95, cpuNote.En)),
		i18n.T(
			fmt.Sprintf("Số tiến trình %d: đỉnh %d × 2, làm tròn lên bội của 10, không dưới max(32, %d worker + %d) = %d.%s Trên kernel 6.8, lần chạm trần của lệnh tạm (cron) chỉ đếm được khi mẫu rơi đúng lúc lệnh đó chạy.",
				sg.Suggested.Tasks, tasksPeak, cur.Workers(), CageTasksSlack, tasksMin, tasksNote.Vi),
			fmt.Sprintf("Tasks %d: peak %d x 2, rounded up to a multiple of 10, at least max(32, %d workers + %d) = %d.%s On kernel 6.8 a hit by a short-lived command (cron) is only counted when a sample lands while it runs.",
				sg.Suggested.Tasks, tasksPeak, cur.Workers(), CageTasksSlack, tasksMin, tasksNote.En)),
	}
	return sg
}

// rollupHour gộp các mẫu của một giờ (đã sắp, cùng domain; prev là mẫu ngay
// trước giờ đó nếu có, để tính phần tăng của mẫu đầu). Hàm thuần.
func rollupHour(domain string, hour time.Time, prev *cage.Sample, samples []cage.Sample) cage.Hour {
	h := cage.Hour{Domain: domain, Hour: hour, Samples: len(samples)}
	if len(samples) == 0 {
		return h
	}
	var memSum int64
	for i, s := range samples {
		memSum += s.MemCurrent
		h.MemMax = max(h.MemMax, s.MemCurrent)
		h.TasksMax = max(h.TasksMax, s.Tasks)

		var before *cage.Sample
		switch {
		case i > 0:
			before = &samples[i-1]
		case prev != nil:
			before = prev
		}
		if before == nil {
			continue // mẫu đầu tiên của cả lịch sử: không có gì để trừ
		}
		d := deltaOf(*before, s)
		h.CPUUsec += d.cpu
		h.ThrottledUsec += d.throttled
		h.NrThrottled += d.nrThrottled
		h.OOMKills += d.oom
		h.TasksHits += d.tasksHits
		h.IORBytes += d.ioR
		h.IOWBytes += d.ioW
		if d.cpuPctOK {
			h.CPUPctMax = math.Max(h.CPUPctMax, d.cpuPct())
		}
		// memory.peak, pids.peak chỉ tính khi tăng: nếu không, một đỉnh cũ làm
		// biểu đồ tuần và tháng dính mãi ở mức đó.
		if d.memPeakRose {
			h.MemMax = max(h.MemMax, s.MemPeak)
		}
		if d.tasksPeakRose {
			h.TasksMax = max(h.TasksMax, s.TasksPeak)
		}
	}
	h.MemAvg = memSum / int64(len(samples))
	return h
}
