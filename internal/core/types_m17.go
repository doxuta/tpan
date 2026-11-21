package core

// Hợp đồng M17 (lồng theo site, trần theo gói, số liệu, thử tấn công): kiểu dữ
// liệu, giao diện store, mã từ chối, tên cách sửa tự động. Đặc tả ở
// docs/thay-doi/M17-thiet-ke.md (mục 3); cuối mốc gộp vào mục 6.15 của
// docs/KE-HOACH.md. Ca sử dụng nằm ở cage.go (lồng), usage.go (số liệu và
// gợi ý), probe.go (thử tấn công).

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
)

// Mã từ chối mới của M17 (RefusedError.Code).
const (
	CodeCageNotInPackage      = "cage_not_in_package"      // gói của chủ site chưa bật lồng
	CodeCageNotCaged          = "cage_not_caged"           // ra lồng một site không ở trong lồng
	CodeCageCgroupV1          = "cage_cgroup_v1"           // máy không có cgroup v2
	CodeCageSystemdOld        = "cage_systemd_old"         // systemd dưới 254 (thiếu --expand-environment)
	CodeCageActive            = "cage_active"              // lùi bản tpan khi còn lồng hay lượt thử chưa dọn
	CodeCageDrift             = "cage_drift"               // pool hay vhost của site đang lệch template (vào lồng rồi ra sẽ không trả lại đúng từng byte)
	CodePackageLimitInvalid   = "package_limit_invalid"    // trần ngoài khoảng cho phép (mục 3.1)
	CodePackageLimitsNeedCage = "package_limits_need_cage" // có trần mà gói không bật lồng
	CodeDomainReserved        = "domain_reserved"          // tên miền dành cho phép thử (site.IsProbeDomain)
	CodeProbeRunning          = "probe_running"            // đang có một lượt thử chưa xong
	CodeUsageNotCaged         = "usage_not_caged"          // số liệu của site ngoài lồng (404 ở API)
)

// Tên cách sửa tự động mới (checks.Result.AutoFix; AutoFixName ở health.go).
const (
	// "cage.socket:<domain>" Fail (socket, unit hay dịch vụ hỏng): bật lại
	// socket và dịch vụ (steps.CageSocket).
	FixCageStart = "cage.start"
	// "cage.runtime:<domain>" Fail (lồng không thấy /run/mysqld) hoặc Warn
	// (đang chạy bản PHP đã bị thay): steps.CageRestart.
	FixCageRestart = "cage.restart"
	// "cage.limits:<domain>" Fail, "cage.fence:<domain>" Fail, "cage.state"
	// Fail khi caged mà thiếu file: ghi lại file của lồng với trần của gói
	// trong bảng (Plan cage.sync cho riêng site đó).
	FixCageSync = "cage.sync"
	// "cage.state:<domain>" Warn: gói muốn lồng mà site ở ngoài (cage.enable)
	// hoặc ngược lại (cage.disable).
	FixCageEnable  = "cage.enable"
	FixCageDisable = "cage.disable"
	// "probe.leftover:tpan" Fail: dọn các lượt thử tấn công chưa dọn.
	FixProbeCleanup = "probe.cleanup"
)

// Khoảng giá trị của trần (mục 3.1). 0 luôn hợp lệ (không giới hạn; riêng
// PHPWorkers: 0 là account.DefaultPHPWorkers).
const (
	CageCPUMin, CageCPUMax         = 5, 6400
	CageRAMMin, CageRAMMax         = 64, 1048576
	CageTasksMin, CageTasksMax     = 32, 100000
	CageIOMin, CageIOMax           = 1, 100000
	CageWorkersMin, CageWorkersMax = 1, 200
	// CageTasksSlack: trần tiến trình phải ≥ số worker + chừng này (tiến trình
	// chính của FPM, lệnh wp, cron, luồng của ImageMagick chạy cùng slice;
	// TasksMax đếm cả luồng).
	CageTasksSlack = 15
)

// UsageStore là phần của store cho số liệu (store.Store thoả; khẳng định chữ
// ký nằm trong test của store, vì store không import core).
type UsageStore interface {
	AddUsageSample(ctx context.Context, s cage.Sample) error
	// UsageSamples trả mẫu của domain có at trong [from, to), sắp theo at.
	UsageSamples(ctx context.Context, domain string, from, to time.Time) ([]cage.Sample, error)
	// UsageSampleDomains trả các domain có ít nhất một mẫu trong [from, to), sắp theo tên.
	UsageSampleDomains(ctx context.Context, from, to time.Time) ([]string, error)
	// SaveUsageHour thêm hoặc ghi đè dòng (domain, hour).
	SaveUsageHour(ctx context.Context, h cage.Hour) error
	// UsageHours trả dòng gộp của domain có hour trong [from, to), sắp theo hour.
	UsageHours(ctx context.Context, domain string, from, to time.Time) ([]cage.Hour, error)
	// PruneUsage xoá mẫu có at < samplesBefore và dòng giờ có hour < hoursBefore.
	PruneUsage(ctx context.Context, samplesBefore, hoursBefore time.Time) error
	// DeleteUsageOf xoá mọi mẫu và dòng giờ của một domain (xoá hẳn site: site
	// mới cùng tên không thừa hưởng số cũ).
	DeleteUsageOf(ctx context.Context, domain string) error
}

// ProbeStore là sổ phép thử tấn công (store.Store thoả).
type ProbeStore interface {
	// StartProbeRun ghi dòng mới (State "running", Cleaned false).
	StartProbeRun(ctx context.Context, r cage.ProbeRun) error
	// FinishProbeRun đặt State ("done" hay "failed"), Finished và Report.
	FinishProbeRun(ctx context.Context, id, state string, report []byte, at time.Time) error
	// SaveProbeReport ghi báo cáo dở khi một lượt (ngoài hay trong lồng) vừa
	// xong, State giữ "running" (mục 3.9: kết quả vào sổ ngay khi lượt xong).
	SaveProbeReport(ctx context.Context, id string, report []byte) error
	// MarkProbeCleaned đặt Cleaned = true.
	MarkProbeCleaned(ctx context.Context, id string) error
	// ProbeRun trả một dòng; ok=false khi không có.
	ProbeRun(ctx context.Context, id string) (r cage.ProbeRun, ok bool, err error)
	// ProbeRuns trả tối đa limit dòng mới nhất, mới trước.
	ProbeRuns(ctx context.Context, limit int) ([]cage.ProbeRun, error)
	// PendingProbeRuns trả mọi dòng có Cleaned = false, cũ trước.
	PendingProbeRuns(ctx context.Context) ([]cage.ProbeRun, error)
}

// CageRow là một dòng của `tpan cage status` và GET /api/v1/cage.
type CageRow struct {
	Domain  string `json:"domain"`
	Owner   string `json:"owner"`
	Package string `json:"package"`
	// Caged: đang ở trong lồng (sites.caged). Want: gói của chủ bật lồng.
	// Files: file hàng rào của site có trên máy (một lần Stat). Khác Caged khi
	// panel bị tắt giữa cage.enable hay cage.disable; `cage disable --all` chọn
	// theo Files (đọc máy), `--stale` là Files mà không Want, `cage enable
	// --all` là Want mà không Caged.
	Caged bool `json:"caged"`
	Want  bool `json:"want"`
	Files bool `json:"files"`
	// State: "trong" (caged và want), "ngoai" (cả hai false), "can-vao" (want
	// mà chưa caged), "can-ra" (caged mà gói không còn want).
	State     string         `json:"state"`
	Suspended bool           `json:"suspended"`
	Limits    account.Limits `json:"limits"` // trần của gói (mong muốn)
	// Usage chỉ có khi Caged: đọc ngay từ cgroup.
	Usage *cage.Usage `json:"usage,omitempty"`
}

// SiteUsage là số liệu của một site trong lồng (`tpan usage`, GET /usage).
type SiteUsage struct {
	Domain string         `json:"domain"`
	Owner  string         `json:"owner"`
	Usage  cage.Usage     `json:"usage"`
	Limits account.Limits `json:"limits"`
}

// UsagePoint là một điểm của biểu đồ (từ mẫu 5 phút cho "day", từ dòng giờ
// cho "week" và "month").
type UsagePoint struct {
	At       time.Time `json:"at"`
	MemAvg   int64     `json:"mem_avg"`
	MemMax   int64     `json:"mem_max"`
	CPUPct   float64   `json:"cpu_pct"` // trung bình của khoảng, % một nhân
	TasksMax int64     `json:"tasks_max"`
	OOMKills int64     `json:"oom_kills"` // tăng trong khoảng
}

// UsageHistory là lịch sử của một site (GET /sites/{domain}/usage?range=).
type UsageHistory struct {
	Domain string       `json:"domain"`
	Range  string       `json:"range"` // "day", "week", "month"
	Points []UsagePoint `json:"points"`
}

// Suggestion là gợi ý trần cho một site, kèm mọi số đã dùng để tính (mục 3.8).
type Suggestion struct {
	Domain  string    `json:"domain"`
	Package string    `json:"package"`
	Samples int       `json:"samples"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	// LowData: dưới 288 mẫu (24 giờ).
	LowData bool `json:"low_data"`

	MemPeak      int64   `json:"mem_peak"`      // byte
	CPUP95       float64 `json:"cpu_p95"`       // % một nhân
	TasksPeak    int64   `json:"tasks_peak"`    //
	OOMKills     int64   `json:"oom_kills"`     // tăng trong 7 ngày
	TasksHits    int64   `json:"tasks_hits"`    // tăng trong 7 ngày
	ThrottledPct float64 `json:"throttled_pct"` // % thời gian bị bóp

	Current   account.Limits `json:"current"`   // trần của gói
	Suggested account.Limits `json:"suggested"` // chỉ RAMMB, CPUPercent, Tasks được đặt
	// Reasons: mỗi trường một câu nói đã tính ra sao (công thức và số).
	Reasons []i18n.Text `json:"reasons"`
}

// PackageSuggestion là gợi ý cho một gói: lớn nhất trong các site của gói.
type PackageSuggestion struct {
	Package   string         `json:"package"`
	Sites     []string       `json:"sites"`
	Current   account.Limits `json:"current"`
	Suggested account.Limits `json:"suggested"`
}

// SuggestionReport là GET /api/v1/usage/suggestions và `tpan usage --suggest`.
type SuggestionReport struct {
	Sites    []Suggestion        `json:"sites"`
	Packages []PackageSuggestion `json:"packages"`
}

// Trạng thái một ô của phép thử tấn công.
const (
	ProbeBlocked   = "blocked"    // bị chặn
	ProbeExposed   = "exposed"    // lộ (đường lẽ ra phải bị chặn)
	ProbeNeedsCage = "needs_cage" // lộ ở ngoài lồng; chỉ lồng chặn được
	ProbeSkipped   = "skipped"    // không chạy (kèm lý do)
	ProbeNA        = "na"         // không thuộc stack này
)

// ProbeOptions là lựa chọn của một lượt thử.
type ProbeOptions struct {
	// Only: "" (cả hai lượt), "ngoai", "trong".
	Only string `json:"only"`
}

// ProbeCell là kết quả một đường trong một lượt.
type ProbeCell struct {
	Status   string            `json:"status"`
	Detail   i18n.Text         `json:"detail"`
	Evidence []checks.Evidence `json:"evidence,omitempty"`
}

// ProbeResult là một đường tấn công (mục 3.9: L1 đến L9, C1 đến C4).
type ProbeResult struct {
	Path    string    `json:"path"`
	Title   i18n.Text `json:"title"`
	Outside ProbeCell `json:"outside"`
	Inside  ProbeCell `json:"inside"`
}

// ProbeReport là kết quả của một lượt thử (`tpan isolation probe --json`,
// GET /api/v1/security/probe).
type ProbeReport struct {
	ID       string        `json:"id"`
	Started  time.Time     `json:"started"`
	Finished time.Time     `json:"finished,omitzero"`
	Web      WebServer     `json:"web"`
	DomainA  string        `json:"domain_a"`
	DomainB  string        `json:"domain_b"`
	Only     string        `json:"only"`
	Results  []ProbeResult `json:"results"`
	// Exposed: số ô "exposed" (lệnh thoát mã 1 khi khác 0).
	Exposed int  `json:"exposed"`
	Cleaned bool `json:"cleaned"`
}
