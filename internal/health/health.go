// Package health chạy các phép kiểm theo lịch, ghi kết quả và mở, đóng sự cố
// (mục 6.7 của docs/KE-HOACH.md). Một bộ phép kiểm dùng chung với kiểm chứng
// sau mỗi thao tác: health chỉ hỏi core "đối tượng này có những phép kiểm
// nào", chạy chúng, rồi ghi vào store.
//
// Hai đường gọi, cùng một Runner:
//   - `tpan health` (CLI): Runner.Run một lần, đầy đủ, rồi in.
//   - `tpan serve`: Scheduler chạy Runner theo lịch (nhẹ 5 phút, đầy đủ mỗi
//     giờ) và ngay sau mỗi job cho đối tượng job đó chạm vào.
//
// Gói import core, checks, store, system. Không ai trong số đó import health.
package health

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// Giá trị mặc định của lịch (mục 6.7).
const (
	LightEvery = 5 * time.Minute
	FullEvery  = time.Hour
	// BusyRetry: vòng đầy đủ bị bỏ vì đang có job thì thử lại sau chừng này.
	BusyRetry = 30 * time.Second
	// StuckAfter: job còn "running" lâu hơn chừng này là treo.
	StuckAfter = time.Hour
	// SchedulerMaxAge: nhịp của bộ hẹn giờ cũ hơn chừng này thì phép kiểm
	// tpan.scheduler cảnh báo (ba lần chạy nhẹ bị lỡ).
	SchedulerMaxAge = 3 * LightEvery
	// TPANMinFree: chỗ trống tối thiểu ở /var/lib/tpan.
	TPANMinFree = 200 << 20
)

// Khoá trong bảng settings.
const (
	// SettingLastRun: lần chạy đầy đủ gần nhất (RFC 3339, UTC), CLI hay serve đều ghi.
	SettingLastRun = "health.last_run"
	// SettingSchedulerBeat: lần gần nhất Scheduler trong `tpan serve` chạy
	// một vòng (nhẹ hay đầy đủ). Chỉ Scheduler ghi; phép kiểm
	// tpan.scheduler:health đọc khoá này.
	SettingSchedulerBeat = "health.scheduler_beat"
)

// Đường dẫn của chính tpan mà phép kiểm TPAN xem (mục 5).
const (
	PanelCertPath = "/etc/tpan/tls/cert.pem"
	PanelKeyPath  = "/etc/tpan/tls/key.pem"
	PanelConfDir  = "/etc/tpan"
	StateDir      = "/var/lib/tpan"
)

// Runner chạy phép kiểm và ghi kết quả. Dựng mới hay dùng lại đều được; không
// giữ trạng thái giữa hai lần Run.
type Runner struct {
	Sys   system.System
	Store *store.Store
	// DBPath là file tpan.db (phép kiểm quyền 0600). Rỗng: bỏ phép kiểm đó.
	DBPath string
	// WPCLISHA512 như core.Stack (rỗng là bản đã ghim).
	WPCLISHA512 string
	// NoSelf: không chạy phép kiểm của chính tpan (chứng chỉ, quyền
	// /etc/tpan, nhịp bộ hẹn giờ). Dùng cho --demo, nơi không có các file đó.
	NoSelf bool
	// Now nil thì dùng time.Now.
	Now func() time.Time
}

// Options chọn phần nào được kiểm.
type Options struct {
	// Domain khác rỗng: chỉ kiểm site đó (không stack, không tpan, không lệch).
	// Rỗng: mọi thứ.
	Domain string
	// Full: chạy cả phép kiểm nặng (wp.checksums, dns.a). Lần chạy đầy đủ với
	// Domain rỗng còn gọi Store.PruneResults và ghi SettingLastRun. Lượt kiểm
	// sau một job của một site (Scheduler.Kick với domain) là đầy đủ cho riêng
	// site đó.
	Full bool
}

// Group là kết quả của một đối tượng, đúng thứ tự phép kiểm.
type Group struct {
	// Target: "" máy và stack, domain của site, store.TargetTPAN.
	Target  string          `json:"target"`
	Results []checks.Result `json:"results"`
}

// Report là kết quả một lần Run.
type Report struct {
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	// Installed false: stack chưa cài; Groups chỉ có phần chung của máy (RAM, lệch) và của tpan.
	Installed bool             `json:"installed"`
	Groups    []Group          `json:"groups"`
	Changes   store.Transition `json:"changes"`
}

// HasFail cho biết có phép kiểm nào Fail.
func (r Report) HasFail() bool {
	for _, g := range r.Groups {
		for _, x := range g.Results {
			if x.Status == checks.Fail {
				return true
			}
		}
	}
	return false
}

// Run gom phép kiểm (core.Stack.Checks với target "", core.Sites.Checks cho
// từng site active, checks.Drift cho từng dòng managed_files với target là
// domain nếu path thuộc thư mục hay file của site đó, không thì "", và phép
// kiểm của tpan với store.TargetTPAN), chạy (checks.RunAll), điền
// Result.AutoFix bằng core.AutoFixName, rồi Store.RecordResults theo từng
// target. Lần chạy nhẹ (Full false) bỏ các phép kiểm đánh dấu checks.Heavy
// (wp.checksums, dns.a) trước khi chạy, trừ với site chưa từng qua một lượt
// đầy đủ (neverFull). Lỗi đọc store hay settings thì trả lỗi; phép kiểm không
// chạy được thì đã là Unknown trong Result, không phải lỗi.
func (r *Runner) Run(ctx context.Context, o Options) (Report, error) {
	now := r.clock()
	rep := Report{Started: now()}

	plan, installed, err := r.collect(ctx, o)
	if err != nil {
		return Report{}, err
	}
	rep.Installed = installed

	// Lần nhẹ lấp chỗ trống (v0.2.15): site chưa từng qua một lượt đầy đủ thì
	// lượt này chạy cả phép kiểm nặng của riêng nó. Dấu nhận biết là kết quả
	// "dns.a:<domain>": phép nặng mà site nào cũng có (core.siteEdgeChecks) và
	// lần đầy đủ nào cũng ghi. Không lấp thì site tạo bằng CLI, hay tạo lúc
	// tpan serve chưa chạy, thiếu kết quả DNS và wp.checksums tới vòng đầy đủ
	// kế tiếp (tới 1 giờ), và ga DNS của nó hiện "chưa kiểm được".
	var have map[string]bool
	if !o.Full {
		stored, err := r.Store.CheckResults(ctx)
		if err != nil {
			return Report{}, err
		}
		have = make(map[string]bool, len(stored))
		for _, s := range stored {
			have[s.ID] = true
		}
	}

	// seen: mỗi ID chỉ ghi một lần, lần gặp đầu tiên thắng. Phép kiểm cô lập
	// "isolation:a:b" nằm trong bộ của cả site a lẫn site b; ghi cả hai lần thì
	// target của dòng check_results đổi qua lại giữa a và b mỗi lần chạy.
	seen := map[string]bool{}
	var ids []string
	for _, g := range plan {
		var kept []checks.Result
		// Phép kiểm nặng (checks.Heavy) bỏ hẳn ở lần nhẹ, trước khi chạy.
		// ponytail: phép kiểm trùng ID (cô lập) vẫn chạy hai lần rồi bỏ một
		// kết quả, vì Check không cho biết ID trước khi chạy; nó rẻ (một lệnh test).
		cs := g.checks
		if !o.Full && !neverFull(g.target, have) {
			cs = nil
			for _, c := range g.checks {
				if !checks.IsHeavy(c) {
					cs = append(cs, c)
				}
			}
		}
		for _, res := range checks.RunAll(ctx, r.Sys, cs) {
			if seen[res.ID] {
				continue
			}
			seen[res.ID] = true
			ids = append(ids, res.ID)
			if res.Status != checks.Pass {
				res.AutoFix = core.AutoFixName(res)
			}
			kept = append(kept, res)
		}
		if len(kept) == 0 {
			continue
		}
		rep.Groups = append(rep.Groups, Group{Target: g.target, Results: kept})
		if err := r.record(ctx, o, g.target, kept, now(), &rep.Changes); err != nil {
			return Report{}, err
		}
	}

	if o.Domain != "" && len(plan) == 1 {
		tr, err := r.forgetSwapped(ctx, plan[0].target, seen, now())
		if err != nil {
			return Report{}, err
		}
		mergeTransition(&rep.Changes, tr)
	}

	if o.Full && o.Domain == "" {
		// Chỉ lần đầy đủ mới biết trọn bộ phép kiểm đang tồn tại; lần nhẹ thiếu
		// wp.checksums, lần một site thiếu mọi thứ khác, prune lúc đó là xoá nhầm.
		tr, err := r.Store.PruneResults(ctx, ids, now())
		if err != nil {
			return Report{}, err
		}
		mergeTransition(&rep.Changes, tr)
		if err := r.Store.SetSetting(ctx, SettingLastRun, now().UTC().Format(time.RFC3339)); err != nil {
			return Report{}, err
		}
	}
	rep.Finished = now()
	return rep, nil
}

// neverFull: target là một site chưa có kết quả dns.a nào trong store, tức là
// chưa từng qua một lượt kiểm đầy đủ (xem chỗ gọi ở Run).
func neverFull(target string, have map[string]bool) bool {
	return target != "" && target != store.TargetTPAN && !have["dns.a:"+target]
}

// swappedKinds là các loại phép kiểm nhẹ của một site có thể biến mất khỏi bộ
// phép kiểm khi site đổi trạng thái: tạm khoá, mở khoá, khoá DB (M13), hay
// xin SSL (http.site thành https.site và http.redirect). Không có loại nặng
// (wp.checksums, dns.a): lần chạy nhẹ cho một site vốn không chạy chúng, nên
// "không thấy" không có nghĩa là "không còn".
var swappedKinds = map[string]bool{
	"php.socket": true, "wp.config": true, "db.access": true,
	"http.site": true, "https.site": true, "http.redirect": true, "http.suspended": true,
}

// forgetSwapped dọn kết quả cũ của site domain ngay sau lần chạy cho riêng
// site đó (lần health chạy sau mỗi job): kết quả của phép kiểm không còn trong
// bộ (php.socket sau khi tạm khoá, suspended.php sau khi mở khoá) bị xoá, sự
// cố đang mở của nó đóng với lý do "gone". Không dọn thì chúng nằm lại tới
// lần chạy đầy đủ kế tiếp (tối đa 1 giờ): sau khi mở khoá, bản đồ tuyến vẫn
// xám vì kết quả "tạm khoá" cũ (Unknown) xấu hơn kết quả đạt mới.
//
// Chỉ đụng loại trong swappedKinds và "suspended.*" có đối tượng đúng domain;
// lệch file, cô lập, phép kiểm nặng để lần chạy đầy đủ lo như trước.
// ponytail: dùng PruneResults với danh sách giữ = mọi kết quả đang có trừ
// phần dọn, nên một lần chạy khác ghi kết quả mới đúng lúc giữa hai câu này
// có thể bị đóng sự cố oan (lần chạy sau mở lại). Nâng cấp: hàm store xoá
// đúng danh sách ID trong một transaction.
func (r *Runner) forgetSwapped(ctx context.Context, domain string, seen map[string]bool, now time.Time) (store.Transition, error) {
	stored, err := r.Store.CheckResults(ctx)
	if err != nil {
		return store.Transition{}, err
	}
	keep := make([]string, 0, len(stored))
	drop := false
	for _, s := range stored {
		kind, obj, _ := strings.Cut(s.ID, ":")
		if !seen[s.ID] && obj == domain && (swappedKinds[kind] || strings.HasPrefix(kind, "suspended.")) {
			drop = true
			continue
		}
		keep = append(keep, s.ID)
	}
	if !drop {
		return store.Transition{}, nil
	}
	return r.Store.PruneResults(ctx, keep, now)
}

// Scheduler chạy Runner theo lịch trong `tpan serve`.
type Scheduler struct {
	Runner *Runner
	// LightEvery, FullEvery: 0 thì dùng hằng cùng tên.
	LightEvery, FullEvery time.Duration
	// BusyRetry: 0 thì dùng hằng cùng tên.
	BusyRetry time.Duration
	// Busy trả true khi đang có job thay đổi máy (trong tiến trình này, hoặc
	// khoá toàn cục đang bị tiến trình khác giữ). Khi đó vòng này bỏ qua: kiểm
	// một site đang dựng dở chỉ mở sự cố giả. nil là không bao giờ bận.
	Busy func() bool
	// OnReport khác nil thì được gọi sau mỗi lần chạy xong (server dùng để
	// bỏ bộ nhớ đệm, test dùng để chờ).
	OnReport func(Report)

	// Trạng thái của Kick. Scheduler được dựng bằng struct literal nên kênh
	// tạo lười dưới mu: Kick có thể được gọi trước cả Run.
	mu         sync.Mutex
	kick       chan struct{}
	pending    string
	hasPending bool
}

// Run chặn tới khi ctx bị huỷ: chạy đầy đủ một lần ngay khi bắt đầu, rồi nhẹ
// mỗi LightEvery, đầy đủ mỗi FullEvery; mỗi vòng ghi SettingSchedulerBeat.
// Lỗi của một vòng được ghi log (slog) rồi đi tiếp, không dừng bộ hẹn giờ.
func (s *Scheduler) Run(ctx context.Context) {
	light, full := s.LightEvery, s.FullEvery
	if light <= 0 {
		light = LightEvery
	}
	if full <= 0 {
		full = FullEvery
	}
	busyRetry := s.BusyRetry
	if busyRetry <= 0 {
		busyRetry = BusyRetry
	}
	lightT := time.NewTicker(light)
	defer lightT.Stop()
	fullT := time.NewTicker(full)
	defer fullT.Stop()
	kick := s.kickChan()

	// retry: vòng đầy đủ bị bỏ vì đang có job thì thử lại sau BusyRetry, không
	// chờ tới giờ sau. Vòng đầu tiên ngay khi `tpan setup` bật tpan.service luôn
	// bị bỏ (setup còn giữ khoá); không có retry thì panel trống kết quả tới 5
	// phút, và wp.checksums tới 1 giờ (thấy ở e2e M4).
	var retry <-chan time.Time
	if !s.round(ctx, Options{Full: true}) {
		retry = time.After(busyRetry)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-retry:
			retry = nil
			if !s.round(ctx, Options{Full: true}) {
				retry = time.After(busyRetry)
			}
		case <-fullT.C:
			// Đầy đủ đã gồm mọi thứ của lần nhẹ: bỏ nhịp nhẹ trùng lúc.
			select {
			case <-lightT.C:
			default:
			}
			if !s.round(ctx, Options{Full: true}) && retry == nil {
				retry = time.After(busyRetry)
			}
		case <-lightT.C:
			o := Options{}
			select {
			case <-fullT.C:
				o.Full = true
			default:
			}
			s.round(ctx, o)
		case <-kick:
			s.mu.Lock()
			d, ok := s.pending, s.hasPending
			s.pending, s.hasPending = "", false
			s.mu.Unlock()
			if ok {
				// Lượt kiểm sau một job của riêng một site chạy cả phép kiểm
				// nặng của site đó (dns.a, wp.checksums, DNS của tên phụ). Trước
				// đây nó là lượt nhẹ, nên site vừa tạo không có kết quả DNS nào
				// và ga DNS trên trang site hiện "chưa kiểm được" tới vòng đầy đủ
				// kế tiếp (tới 1 giờ), trông như site đang có vấn đề. Một site thì
				// phép nặng chỉ là vài lệnh; hai đối tượng gộp lại (d rỗng, kiểm
				// mọi thứ) thì vẫn là lượt nhẹ, phần nặng của cả máy để vòng mỗi giờ.
				s.round(ctx, Options{Domain: d, Full: d != ""})
			}
		}
	}
}

func (s *Scheduler) kickChan() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kick == nil {
		// Đệm 1: một tín hiệu đang chờ là đủ, mọi Kick sau gộp vào nó.
		s.kick = make(chan struct{}, 1)
	}
	return s.kick
}

// round chạy một vòng; trả false khi vòng bị bỏ vì đang có job. Không lấy khoá toàn cục: phép kiểm chỉ đọc, và giữ khoá
// thì người bấm "tạo site" đúng lúc health chạy sẽ bị từ chối.
func (s *Scheduler) round(ctx context.Context, o Options) bool {
	r := s.Runner
	// Ghi nhịp TRƯỚC khi xem Busy: phép kiểm tpan.scheduler hỏi "bộ hẹn giờ còn
	// sống không", không hỏi "vừa kiểm xong chưa". Một job dài (cài stack, sao
	// lưu lớn) làm bỏ nhiều vòng liền; không ghi nhịp thì nó cảnh báo sai là
	// tpan serve đã chết.
	if err := r.Store.SetSetting(ctx, SettingSchedulerBeat, r.clock()().UTC().Format(time.RFC3339)); err != nil && ctx.Err() == nil {
		slog.Error("health: ghi nhịp bộ hẹn giờ", "err", err)
	}
	if s.Busy != nil && s.Busy() {
		// Kiểm một site đang dựng dở chỉ mở sự cố giả. Vòng sau (hoặc Kick của
		// chính job đó khi xong) sẽ kiểm.
		slog.Info("health: đang có job, bỏ vòng này", "full", o.Full, "domain", o.Domain)
		return false
	}
	rep, err := r.Run(ctx, o)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("health: một vòng kiểm lỗi", "full", o.Full, "domain", o.Domain, "err", err)
		}
		return true
	}
	if s.OnReport != nil {
		s.OnReport(rep)
	}
	return true
}

// Kick xin một lần chạy sớm, ví dụ ngay sau một job. domain khác rỗng: kiểm
// riêng site đó, cả phép kiểm nặng của nó (DNS, wp.checksums). domain rỗng:
// mọi thứ, lượt nhẹ. Không chặn: nếu đã có yêu cầu đang chờ thì gộp lại (hai
// domain khác nhau gộp thành "mọi thứ").
func (s *Scheduler) Kick(domain string) {
	ch := s.kickChan()
	s.mu.Lock()
	switch {
	case !s.hasPending:
		s.pending, s.hasPending = domain, true
	case s.pending != domain:
		// Hai đối tượng khác nhau đang chờ: kiểm mọi thứ thì phủ được cả hai.
		s.pending = ""
	}
	s.mu.Unlock()
	select {
	case ch <- struct{}{}:
	default: // đã có tín hiệu đang chờ; pending đã gộp ở trên
	}
}
