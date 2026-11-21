package core

// Thử tấn công: `tpan isolation probe` (M17, mục 3.9 của
// docs/thay-doi/M17-thiet-ke.md). Hợp đồng đợt 0: chữ ký và tài liệu; thân hàm
// do thợ đợt 3 viết.
//
// Luật không được phá: chỉ đụng vào thứ mang một trong hai tên miền thử của
// lượt đó (site.IsProbeDomain, và có trong sổ probe_runs); mọi đường dẫn suy
// từ site.LayoutOf(tên đó). Dòng sổ được ghi TRƯỚC khi tạo bất cứ thứ gì.

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

var errProbeTODO = errors.New("thử tấn công: chưa viết (M17 đợt 3)")

// Probe gom những gì phép thử cần. Dựng mới cho mỗi lệnh.
type Probe struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore // chỉ đọc (lấy stack, tránh tên trùng); site thử KHÔNG được ghi vào đây
	Runs     ProbeStore
	// Journal nhận job khi Execute. Plan của phép thử chạy với một Journal bọc
	// bỏ qua RecordFile, ForgetFile (file của site thử không vào managed_files).
	Journal engine.Journal
	// WPVersion, WPHash như Sites (bản WordPress của site thử).
	WPVersion, WPHash string
	Now               func() time.Time
	// Rand sinh 12 ký tự hex của tên miền thử và 16 ký tự hex của id; nil là crypto/rand.
	Rand func(n int) (string, error)
}

// ProbeJob là một lượt thử đã dựng Plan, dùng cho đúng một lần Execute. Thợ
// đợt 3 thêm các trường không xuất cần thiết (báo cáo đang ghi, *Probe).
type ProbeJob struct {
	Plan engine.Plan
	Run  ProbeRunInfo
}

// ProbeRunInfo là phần của lượt thử biết được trước khi chạy.
type ProbeRunInfo struct {
	ID      string `json:"id"`
	DomainA string `json:"domain_a"`
	DomainB string `json:"domain_b"`
	Only    string `json:"only"`
}

// Prepare dựng một lượt thử (mục 3.9). Từ chối: stack_not_installed,
// probe_running (còn dòng sổ "running" hoặc chưa dọn: chạy --cleanup trước),
// args_invalid (Only lạ). Chỉ đọc máy.
func (p *Probe) Prepare(ctx context.Context, o ProbeOptions) (*ProbeJob, error) {
	return nil, errProbeTODO
}

// Preview là bản xem trước của lượt thử. Chỉ đọc máy.
func (j *ProbeJob) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.Preview{}, errProbeTODO
}

// Execute ghi sổ, chạy Plan, ghi kết quả. Trả báo cáo kể cả khi job hỏng giữa
// chừng (các ô chưa chạy để trống) và Report của engine.
func (j *ProbeJob) Execute(ctx context.Context, onEvent func(engine.Event)) (ProbeReport, engine.Report, error) {
	return ProbeReport{}, engine.Report{}, errProbeTODO
}

// PrepareCleanup dựng Plan "probe.cleanup" dọn mọi lượt có Cleaned = false
// trong sổ. nil, nil khi không có gì để dọn. after: MarkProbeCleaned từng lượt.
func (p *Probe) PrepareCleanup(ctx context.Context) (*Planned, error) {
	return nil, errProbeTODO
}

// Last trả báo cáo của lượt gần nhất đã xong; nil khi chưa có lượt nào.
func (p *Probe) Last(ctx context.Context) (*ProbeReport, error) {
	return nil, errProbeTODO
}
