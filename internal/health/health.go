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
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

var errNotWritten = errors.New("health: chưa viết")

// Giá trị mặc định của lịch (mục 6.7).
const (
	LightEvery = 5 * time.Minute
	FullEvery  = time.Hour
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
	// Full: chạy cả phép kiểm nặng (wp.checksums). Lần chạy đầy đủ với Domain
	// rỗng còn gọi Store.PruneResults và ghi SettingLastRun.
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
	// Installed false: stack chưa cài; Groups chỉ có phần của tpan (nếu có).
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
// target. Lần chạy nhẹ (Full false) bỏ các phép kiểm có ID bắt đầu bằng
// "wp.checksums:". Lỗi đọc store hay settings thì trả lỗi; phép kiểm không chạy
// được thì đã là Unknown trong Result, không phải lỗi.
func (r *Runner) Run(ctx context.Context, o Options) (Report, error) {
	return Report{}, errNotWritten
}

// Scheduler chạy Runner theo lịch trong `tpan serve`.
type Scheduler struct {
	Runner *Runner
	// LightEvery, FullEvery: 0 thì dùng hằng cùng tên.
	LightEvery, FullEvery time.Duration
	// Busy trả true khi đang có job thay đổi máy (trong tiến trình này, hoặc
	// khoá toàn cục đang bị tiến trình khác giữ). Khi đó vòng này bỏ qua: kiểm
	// một site đang dựng dở chỉ mở sự cố giả. nil là không bao giờ bận.
	Busy func() bool
	// OnReport khác nil thì được gọi sau mỗi lần chạy xong (server dùng để
	// bỏ bộ nhớ đệm, test dùng để chờ).
	OnReport func(Report)
}

// Run chặn tới khi ctx bị huỷ: chạy đầy đủ một lần ngay khi bắt đầu, rồi nhẹ
// mỗi LightEvery, đầy đủ mỗi FullEvery; mỗi vòng ghi SettingSchedulerBeat.
// Lỗi của một vòng được ghi log (slog) rồi đi tiếp, không dừng bộ hẹn giờ.
func (s *Scheduler) Run(ctx context.Context) {}

// Kick xin một lần chạy sớm cho domain (rỗng là mọi thứ, nhẹ), ví dụ ngay sau
// một job. Không chặn: nếu đã có yêu cầu đang chờ thì gộp lại.
func (s *Scheduler) Kick(domain string) {}
