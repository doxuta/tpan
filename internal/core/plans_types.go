package core

// Hợp đồng M7: Plan chờ duyệt và hai chế độ duyệt (mục 4.2, 8.1, 8.3 của
// docs/KE-HOACH.md). CLI, API và MCP cùng đi qua Planner, nên "dựng lại rồi so
// trước khi chạy" chỉ viết một lần.
//
// Thợ của đợt 1 viết ở plans.go (cùng test):
//
//	// Prepare kiểm đầu vào của req (validate.ParseDomain, ParseEmail; sai thì
//	// *validate.Error hoặc *RefusedError) và dựng Plan, chưa chạy gì:
//	//   - PlanSiteAdd:     Sites.PrepareAdd(Domain, Email, Title).
//	//   - PlanSiteRemove:  Sites.PrepareRemove(Domain).
//	//   - PlanSiteRepair:  site phải có trong bảng sites và đang active; dựng
//	//     lại đúng Plan tạo site (chạy lại là sửa chỗ lệch, mục 4.2). Không bịa
//	//     email quản trị: WordPress chưa cài (`wp core is-installed` mã 1) thì
//	//     từ chối "wp_not_installed" và chỉ sang PlanSiteAdd kèm email.
//	//   - PlanStackRepair: stack phải đã cài; Stack.PrepareInstall(web đang dùng).
//	//   - Kind khác: từ chối "plan_kind_invalid".
//	func (p *Planner) Prepare(ctx context.Context, req PlanRequest) (*Prepared, error)
//	// Propose: Prepare, xem trước, tính engine.Fingerprint, lưu một dòng pending
//	// (id 16 hex ngẫu nhiên, Request là JSON của req) rồi trả dòng đó.
//	func (p *Planner) Propose(ctx context.Context, req PlanRequest, from Proposal) (engine.PendingPlan, error)
//	// PrepareApprove: đọc Plan (không có thì từ chối "plan_not_found"; không
//	// pending thì "plan_not_pending"), dựng lại từ Request, xem trước. Dấu vân
//	// tay khác bản đã lưu: UpdatePlanPreview bằng bản mới rồi từ chối
//	// "plan_changed" (người phải xem lại và duyệt lần nữa). Giống: DecidePlan
//	// (applied, by) rồi trả Approval sẵn sàng chạy.
//	func (p *Planner) PrepareApprove(ctx context.Context, id, by string) (*Approval, error)
//	// Execute chạy Plan đã duyệt; ghi job id vào plans ngay khi có sự kiện
//	// job.start. Job không bắt đầu được (lỗi trước job.start, ví dụ khoá bận)
//	// thì ReopenPlan để Plan về lại pending, rồi trả lỗi.
//	func (a *Approval) Execute(ctx context.Context, onEvent func(engine.Event)) (RunResult, error)
//	// Reject: pending → rejected. Lỗi như PrepareApprove.
//	func (p *Planner) Reject(ctx context.Context, id, by string) error
//	// AgentConfig đọc hai setting dưới đây; trống thì mặc định (panel, all);
//	// giá trị lạ thì lỗi.
//	func AgentConfig(ctx context.Context, s Settings) (AgentSettings, error)
//
// Mã từ chối mới: plan_not_found (API 404), plan_not_pending, plan_changed,
// wp_not_installed, plan_kind_invalid (409, riêng plan_kind_invalid là 400).

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// Các loại yêu cầu lập Plan.
const (
	PlanSiteAdd     = "site.add"
	PlanSiteRemove  = "site.rm"
	PlanSiteRepair  = "site.repair"
	PlanStackRepair = "stack.repair"
)

// PlanRequest đủ để dựng lại một Plan. Mang giá trị thật (đã đổi bí danh về
// tên thật trước khi tới đây); không bao giờ mang mật khẩu.
type PlanRequest struct {
	Kind   string `json:"kind"`
	Domain string `json:"domain,omitempty"`
	Email  string `json:"email,omitempty"`
	Title  string `json:"title,omitempty"`
}

// Proposal là nguồn gốc của một lần đề xuất.
type Proposal struct {
	Origin    engine.Origin
	SessionID string
	Untrusted bool
}

// PlanStore là phần của store mà Planner cần. *store.Store thoả giao diện này.
// Lỗi "không có" thoả errors.Is(err, store.ErrNotFound) và "không còn chờ" thoả
// errors.Is(err, store.ErrPlanNotPending); core không import store nên nhận
// hai lỗi đó qua NotFound và NotPending của Planner.
type PlanStore interface {
	CreatePlan(ctx context.Context, p engine.PendingPlan) error
	Plan(ctx context.Context, id string) (engine.PendingPlan, error)
	UpdatePlanPreview(ctx context.Context, id string, pv engine.Preview, fingerprint string) error
	DecidePlan(ctx context.Context, id string, status engine.PlanStatus, by string, at time.Time) error
	ReopenPlan(ctx context.Context, id string) error
	SetPlanJob(ctx context.Context, id, jobID string) error
}

// Planner gom những gì việc lập và duyệt Plan cần. Dựng mới cho mỗi lệnh.
type Planner struct {
	Sites *Sites
	Stack *Stack
	Plans PlanStore
	// NotFound và NotPending là store.ErrNotFound và store.ErrPlanNotPending
	// (người dựng Planner đặt), để đổi thành lời từ chối có mã.
	NotFound, NotPending error
	// Now nil thì dùng time.Now.
	Now func() time.Time
}

// Prepared là một Plan đã dựng từ PlanRequest, dùng cho đúng một lần Execute.
type Prepared struct {
	Request PlanRequest
	Plan    engine.Plan

	preview func(ctx context.Context) (engine.Preview, error)
	execute func(ctx context.Context, onEvent func(engine.Event)) (RunResult, error)
}

// Preview là bản xem trước. Chỉ đọc máy.
func (p *Prepared) Preview(ctx context.Context) (engine.Preview, error) { return p.preview(ctx) }

// Execute chạy Plan qua engine (và ghi bảng sites, settings như ca sử dụng gốc).
func (p *Prepared) Execute(ctx context.Context, onEvent func(engine.Event)) (RunResult, error) {
	return p.execute(ctx, onEvent)
}

// RunResult là kết quả chạy một Plan, chung cho mọi loại.
type RunResult struct {
	Report engine.Report
	// Chỉ có khi Plan tạo site thật sự cài WordPress (xem SiteAddResult).
	WPInstalled   bool
	AdminUser     string
	AdminPassword system.Secret
}

// Approval là một Plan đã được duyệt (đã chuyển sang applied), sẵn sàng chạy.
type Approval struct {
	Plan engine.PendingPlan

	// prepared là đúng Plan đã dựng và so dấu vân tay lúc duyệt; Execute chạy
	// nó, không dựng lại (dựng lại là chạy thứ người chưa xem).
	prepared *Prepared
	planner  *Planner
}

// Hai setting của cổng agent, trong bảng settings. Chỉ đổi được bằng
// `tpan mcp config` (cần root): bật chế độ client là cho agent tự chạy Plan.
const (
	SettingMCPApproval = "mcp.approval" // "panel" (mặc định) | "client"
	SettingAIRedact    = "ai.redact"    // "all" (mặc định) | "secrets"
)

// ApprovalMode là chế độ duyệt Plan của agent nối trực tiếp (mục 8.3).
type ApprovalMode string

const (
	ApprovalPanel  ApprovalMode = "panel"
	ApprovalClient ApprovalMode = "client"
)

// AgentSettings là cấu hình của cổng agent.
type AgentSettings struct {
	Approval ApprovalMode `json:"approval"`
	// Redact là mức che: "all" hoặc "secrets" (kiểu ai.Level; core không
	// import ai nên giữ dạng chuỗi).
	Redact string `json:"redact"`
}
