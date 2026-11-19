package tools

// Công cụ lập Plan và apply_plan (mục 8.1, 8.3). Lập Plan chỉ lưu một dòng
// pending: Planner.Propose dựng và xem trước, không chạy gì.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

type proposeOut struct {
	PlanID    string            `json:"plan_id"`
	Status    engine.PlanStatus `json:"status"`
	Kind      string            `json:"kind"`
	Target    string            `json:"target"`
	Title     i18n.Text         `json:"title"`
	Untrusted bool              `json:"untrusted"`
	Note      string            `json:"note"`
	Preview   previewText       `json:"preview"`
}

// canApply: phiên này được tự chạy Plan của chính nó (chế độ client, lối trực tiếp).
func (st *sessionState) canApply() bool {
	return st.approval == core.ApprovalClient && st.rec.Entry == store.EntryMCP
}

// howToApprove là câu chỉ người cách duyệt Plan id.
func howToApprove(id string) string {
	return "A human must review and approve it in the tpan panel at /plans/" + id +
		" or on the server with `sudo tpan plan approve " + id + "`."
}

func propose(ctx context.Context, c *call, req core.PlanRequest) (any, error) {
	st := &c.s.st
	pp, err := c.s.planner().Propose(ctx, req, core.Proposal{
		Origin: st.origin, SessionID: st.rec.ID,
		// Cờ của phiên lúc này: đã đọc log hay output lệnh thì người duyệt
		// thấy cảnh báo trên màn xem trước.
		Untrusted: st.rec.Untrusted || c.untrusted,
	})
	if err != nil {
		return nil, err
	}
	c.planID = pp.ID
	note := "Nothing on the server has changed. Plan " + pp.ID + " is pending. " + howToApprove(pp.ID)
	if st.canApply() {
		note += " This session may also run it with apply_plan, after asking the user."
	}
	if pp.Untrusted {
		note += " The plan carries a warning because this session read untrusted content (logs or web pages)."
	}
	return proposeOut{
		PlanID: pp.ID, Status: pp.Status, Kind: pp.Kind, Target: pp.Target, Title: pp.Title,
		Untrusted: pp.Untrusted, Note: note, Preview: textPreview(pp.Preview),
	}, nil
}

func proposeSiteAdd(ctx context.Context, c *call, in siteAddIn, d validate.Domain) (any, error) {
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanSiteAdd, Domain: d.String(), Email: in.Email, Title: in.Title})
}

func proposeSiteRemove(ctx context.Context, c *call, _ domainIn, d validate.Domain) (any, error) {
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanSiteRemove, Domain: d.String()})
}

func proposeSiteRepair(ctx context.Context, c *call, _ domainIn, d validate.Domain) (any, error) {
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanSiteRepair, Domain: d.String()})
}

func proposeStackRepair(ctx context.Context, c *call, _ noArgs, _ validate.Domain) (any, error) {
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanStackRepair})
}

func proposeHealthFix(ctx context.Context, c *call, in checkIDIn, _ validate.Domain) (any, error) {
	// Chỉ id đi tiếp: Planner đọc kết quả đã lưu của phép kiểm đó, nên agent
	// không bịa được trạng thái hay đối tượng cần sửa.
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanHealthFix, CheckID: in.CheckID})
}

func proposeSSLIssue(ctx context.Context, c *call, _ domainIn, d validate.Domain) (any, error) {
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanSSLIssue, Domain: d.String()})
}

// applyOut là kết quả apply_plan. Dựng từng trường, không trả nguyên
// core.RunResult: RunResult mang mật khẩu quản trị WordPress.
type applyOut struct {
	PlanID string              `json:"plan_id"`
	JobID  string              `json:"job_id"`
	Status engine.JobStatus    `json:"status"`
	Error  string              `json:"error,omitempty"`
	Steps  []engine.StepResult `json:"steps"`
	// AdminUser chỉ có khi Plan vừa cài WordPress.
	AdminUser string `json:"admin_user,omitempty"`
	Note      string `json:"note"`
}

func applyPlan(ctx context.Context, c *call, in planIDIn, _ validate.Domain) (any, error) {
	st := &c.s.st
	if !st.canApply() {
		return nil, &Error{Code: "approval_required", Message: "this session cannot run plans (approval mode is panel). " + howToApprove(in.PlanID)}
	}
	pp, err := st.d.Store.Plan(ctx, in.PlanID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &Error{Code: "plan_not_found", Message: "there is no plan " + in.PlanID + "; call list_plans"}
	}
	if err != nil {
		return nil, err
	}
	c.planID = pp.ID
	// Chế độ client tin vào bước hỏi quyền của agent, nên chỉ cho chạy đúng
	// Plan agent này vừa đề xuất (người dùng của agent đã thấy nó). Plan của
	// người, của phiên panel hay của agent khác vẫn phải qua người duyệt.
	if pp.Origin != engine.OriginMCP || pp.SessionID != st.rec.ID {
		return nil, &Error{Code: "approval_required", Message: "plan " + pp.ID + " was not proposed by this session. " + howToApprove(pp.ID)}
	}
	// Plan tạo site sinh mật khẩu quản trị WordPress chỉ được hiện cho người,
	// một lần (mục 11). Chạy từ đây thì mật khẩu không tới được ai, nên loại
	// Plan này luôn phải duyệt ở panel hoặc CLI, kể cả ở chế độ client.
	var req core.PlanRequest
	if err := json.Unmarshal(pp.Request, &req); err != nil {
		return nil, fmt.Errorf("đọc yêu cầu của Plan %s: %w", pp.ID, err)
	}
	if req.Kind == core.PlanSiteAdd {
		return nil, &Error{Code: "approval_required", Message: "a plan that creates a site makes a WordPress admin password that only a person may see, so it is always approved by a person. " + howToApprove(pp.ID)}
	}
	ap, err := c.s.planner().PrepareApprove(ctx, pp.ID, "mcp:"+st.rec.ID)
	if err != nil {
		return nil, err
	}
	res, err := ap.Execute(ctx, nil)
	if err != nil && res.Report.Job.ID == "" {
		// Job không bắt đầu được; Execute đã đưa Plan về pending.
		return nil, err
	}
	rep := res.Report
	out := applyOut{
		PlanID: pp.ID, JobID: rep.Job.ID, Status: rep.Job.Status, Error: rep.Job.Error, Steps: rep.Steps,
		Note: "The WordPress admin password is never sent to the AI. Call get_job with job_id for the full log and checks.",
	}
	if out.Steps == nil {
		out.Steps = []engine.StepResult{}
	}
	if err != nil && out.Error == "" {
		out.Error = err.Error()
	}
	if res.WPInstalled {
		out.AdminUser = res.AdminUser
		out.Note = "WordPress was installed with admin user " + res.AdminUser +
			". Its password is never sent to the AI and is not stored anywhere; the site owner sets a new one with the WordPress \"Lost your password\" link or WP-CLI on the server. " + out.Note
	}
	return out, nil
}
