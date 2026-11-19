package core

// Planner: lập Plan chờ duyệt, duyệt và chạy (mục 4.2, 8.1, 8.3 của
// docs/KE-HOACH.md). Hợp đồng ở plans_types.go.
//
// Ý chính: lúc đề xuất, lưu yêu cầu (PlanRequest) và dấu vân tay của bản xem
// trước. Lúc duyệt, dựng lại Plan từ yêu cầu trên trạng thái máy BÂY GIỜ rồi so
// dấu vân tay; khác thì người phải xem lại. Không lưu Plan đã dựng: Plan giữ
// mật khẩu vừa sinh, và máy có thể đã đổi trong lúc chờ duyệt.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// repairEmail là email quản trị đặt tạm khi dựng lại Plan tạo site để sửa.
// Nó không bao giờ được dùng: Prepare chỉ dựng Plan sửa khi WordPress ĐÃ cài,
// nên bước cài WordPress chỉ còn kiểm permalink, không chạy `wp core install`
// (lệnh duy nhất đọc email). repairInstallStep chặn thêm lúc chạy, phòng khi
// WordPress bị gỡ giữa lúc duyệt và lúc chạy. Bản xem trước vẫn hiện email
// này trong lệnh của bước cài, nên chọn tên miền .invalid (RFC 2606) để người
// đọc thấy ngay đó không phải địa chỉ thật.
const repairEmail = "unused@example.invalid"

func (p *Planner) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Prepare kiểm đầu vào và dựng Plan, chưa chạy gì. Xem hợp đồng ở plans_types.go.
func (p *Planner) Prepare(ctx context.Context, req PlanRequest) (*Prepared, error) {
	switch req.Kind {
	case PlanSiteAdd:
		return p.prepareSiteAdd(ctx, req)
	case PlanSiteRemove:
		return p.prepareSiteRemove(ctx, req)
	case PlanSiteRepair:
		return p.prepareSiteRepair(ctx, req)
	case PlanStackRepair:
		return p.prepareStackRepair(ctx)
	}
	return nil, refuse("plan_kind_invalid", i18n.T(
		fmt.Sprintf("loại Plan %q không có; chỉ nhận %s, %s, %s, %s", req.Kind, PlanSiteAdd, PlanSiteRemove, PlanSiteRepair, PlanStackRepair),
		fmt.Sprintf("there is no plan kind %q; only %s, %s, %s, %s", req.Kind, PlanSiteAdd, PlanSiteRemove, PlanSiteRepair, PlanStackRepair)))
}

// siteAddPrepared bọc một SiteAdd thành Prepared. req là yêu cầu đã chuẩn hoá.
func siteAddPrepared(req PlanRequest, a *SiteAdd) *Prepared {
	return &Prepared{
		Request: req,
		Plan:    a.Plan,
		preview: a.Preview,
		execute: func(ctx context.Context, onEvent func(engine.Event)) (RunResult, error) {
			res, err := a.Execute(ctx, onEvent)
			return RunResult{
				Report: res.Report, WPInstalled: res.WPInstalled,
				AdminUser: res.AdminUser, AdminPassword: res.AdminPassword,
			}, err
		},
	}
}

func (p *Planner) prepareSiteAdd(ctx context.Context, req PlanRequest) (*Prepared, error) {
	d, err := validate.ParseDomain(req.Domain)
	if err != nil {
		return nil, err
	}
	e, err := validate.ParseEmail(req.Email)
	if err != nil {
		return nil, err
	}
	a, err := p.Sites.PrepareAdd(ctx, SiteInput{Domain: d, Email: e, Title: req.Title})
	if err != nil {
		return nil, err
	}
	// Lưu dạng đã chuẩn hoá (chữ thường...), để lần dựng lại lúc duyệt đọc
	// đúng thứ đã kiểm.
	return siteAddPrepared(PlanRequest{Kind: PlanSiteAdd, Domain: d.String(), Email: e.String(), Title: req.Title}, a), nil
}

func (p *Planner) prepareSiteRemove(ctx context.Context, req PlanRequest) (*Prepared, error) {
	d, err := validate.ParseDomain(req.Domain)
	if err != nil {
		return nil, err
	}
	r, err := p.Sites.PrepareRemove(ctx, d)
	if err != nil {
		return nil, err
	}
	return &Prepared{
		Request: PlanRequest{Kind: PlanSiteRemove, Domain: d.String()},
		Plan:    r.Plan,
		preview: r.Preview,
		execute: func(ctx context.Context, onEvent func(engine.Event)) (RunResult, error) {
			rep, err := r.Execute(ctx, onEvent)
			return RunResult{Report: rep}, err
		},
	}, nil
}

// wpInstalledCmd là câu hỏi chỉ đọc `wp core is-installed`, giống hệt câu
// wpInstallStep.Apply dùng: mã 1 là "chưa cài".
func wpInstalledCmd(l site.Layout) system.Cmd {
	return system.Cmd{
		Name:   "wp",
		Args:   []string{"core", "is-installed", "--skip-plugins", "--skip-themes", "--path=" + l.Public},
		AsUser: l.User,
	}
}

// wpInstalled trả false khi WP-CLI thoát mã 1; mã khác 0 còn lại là lỗi.
func wpInstalled(ctx context.Context, sys system.System, l site.Layout) (bool, error) {
	_, err := sys.Run(ctx, wpInstalledCmd(l))
	var ee *system.ExitError
	if errors.As(err, &ee) && ee.Code == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("hỏi WordPress của site %s đã cài chưa: %w", l.Domain, err)
	}
	return true, nil
}

// prepareSiteRepair dựng lại đúng Plan tạo site của một site đang có: chạy lại
// Plan là sửa chỗ lệch (mục 4.2), bước đã đúng được bỏ qua.
func (p *Planner) prepareSiteRepair(ctx context.Context, req PlanRequest) (*Prepared, error) {
	d, err := validate.ParseDomain(req.Domain)
	if err != nil {
		return nil, err
	}
	st, err := p.Sites.find(ctx, d)
	if err != nil {
		return nil, err
	}
	if st.Status != site.Active {
		return nil, refuse("site_trashed", i18n.T(
			"site "+d.String()+" đang ở thùng rác; khôi phục nó trước rồi mới sửa",
			"site "+d.String()+" is in the trash; restore it before repairing it"))
	}
	email, err := validate.ParseEmail(repairEmail)
	if err != nil {
		return nil, fmt.Errorf("email tạm của Plan sửa site: %w", err)
	}
	// PrepareAdd trước câu hỏi WP-CLI: nó kiểm stack và lệch user DB với
	// wp-config.php. Thiếu wp-config.php thì WP-CLI cũng trả mã 1, và lời
	// "site_drift" đúng hơn lời "WordPress chưa cài".
	a, err := p.Sites.PrepareAdd(ctx, SiteInput{Domain: d, Email: email})
	if err != nil {
		return nil, err
	}
	ok, err := wpInstalled(ctx, p.Sites.Sys, a.Layout)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("wp_not_installed", i18n.T(
			"WordPress của site "+d.String()+" chưa được cài, nên chạy lại sẽ phải cài mới và cần email quản trị thật. Dùng Plan "+PlanSiteAdd+" kèm email (sudo tpan site add "+d.String()+" --email <email>).",
			"WordPress for site "+d.String()+" is not installed, so a re-run would install it fresh and needs a real admin email. Use a "+PlanSiteAdd+" plan with an email (sudo tpan site add "+d.String()+" --email <email>)."))
	}
	for i, s := range a.Plan.Steps {
		if s == engine.Step(a.install) {
			a.Plan.Steps[i] = &repairInstallStep{a.install}
		}
	}
	// Kind giữ "site.add" (đây đúng là Plan đó, lịch sử job lọc theo nó); chỉ
	// đổi tiêu đề để người duyệt không tưởng là tạo site mới.
	a.Plan.Title = i18n.T("Sửa site "+d.String()+" (chạy lại Plan tạo site)", "Repair site "+d.String()+" (re-run the site creation plan)")
	return siteAddPrepared(PlanRequest{Kind: PlanSiteRepair, Domain: d.String()}, a), nil
}

// repairInstallStep là bước cài WordPress trong Plan sửa: email quản trị là
// email tạm, nên không bao giờ được chạy `wp core install`. WordPress bị gỡ
// sau lúc duyệt thì bước lỗi (job hoàn tác) thay vì cài với email giả.
type repairInstallStep struct{ *wpInstallStep }

func (r *repairInstallStep) Apply(ctx context.Context, sys system.System) error {
	ok, err := wpInstalled(ctx, sys, r.Layout)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("site %s chưa cài WordPress: Plan sửa không cài mới (không có email quản trị thật); dùng `tpan site add %s --email <email>`", r.Layout.Domain, r.Layout.Domain)
	}
	return r.wpInstallStep.Apply(ctx, sys)
}

func (p *Planner) prepareStackRepair(ctx context.Context) (*Prepared, error) {
	cfg, err := p.Stack.Config(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Web == "" || cfg.PHPVersion == "" {
		return nil, refuse("stack_not_installed", i18n.T(
			"stack chưa được cài nên không có gì để sửa; cài bằng: sudo tpan stack install --web nginx|apache",
			"the stack is not installed so there is nothing to repair; install it with: sudo tpan stack install --web nginx|apache"))
	}
	in, err := p.Stack.PrepareInstall(ctx, cfg.Web)
	if err != nil {
		return nil, err
	}
	return &Prepared{
		Request: PlanRequest{Kind: PlanStackRepair},
		Plan:    in.Plan,
		preview: in.Preview,
		execute: func(ctx context.Context, onEvent func(engine.Event)) (RunResult, error) {
			rep, err := in.Execute(ctx, onEvent)
			return RunResult{Report: rep}, err
		},
	}, nil
}

// newPlanID sinh 16 ký tự hex ngẫu nhiên.
func newPlanID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh id Plan: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Propose dựng, xem trước và lưu một Plan chờ duyệt. Không đổi gì trên máy.
func (p *Planner) Propose(ctx context.Context, req PlanRequest, from Proposal) (engine.PendingPlan, error) {
	pr, err := p.Prepare(ctx, req)
	if err != nil {
		return engine.PendingPlan{}, err
	}
	pv, err := pr.Preview(ctx)
	if err != nil {
		return engine.PendingPlan{}, err
	}
	id, err := newPlanID()
	if err != nil {
		return engine.PendingPlan{}, err
	}
	raw, err := json.Marshal(pr.Request)
	if err != nil {
		return engine.PendingPlan{}, fmt.Errorf("mã hoá yêu cầu của Plan %s: %w", id, err)
	}
	pp := engine.PendingPlan{
		ID: id, Kind: pr.Request.Kind, Target: pr.Plan.Target, Title: pr.Plan.Title,
		Request: raw, Preview: pv, Fingerprint: engine.Fingerprint(pv),
		Origin: from.Origin, SessionID: from.SessionID, Untrusted: from.Untrusted,
		Status: engine.PlanPending, Created: p.now().UTC(),
	}
	if err := p.Plans.CreatePlan(ctx, pp); err != nil {
		return engine.PendingPlan{}, fmt.Errorf("lưu Plan chờ duyệt %s: %w", id, err)
	}
	return pp, nil
}

// storeErr đổi lỗi "không có" và "không còn chờ" của store thành lời từ chối
// có mã; lỗi khác giữ nguyên, kèm ngữ cảnh.
func (p *Planner) storeErr(id, what string, err error) error {
	if p.NotFound != nil && errors.Is(err, p.NotFound) {
		return refuse("plan_not_found", i18n.T(
			"không có Plan "+id+"; xem danh sách bằng: sudo tpan plan list",
			"there is no plan "+id+"; list plans with: sudo tpan plan list"))
	}
	if p.NotPending != nil && errors.Is(err, p.NotPending) {
		return refuse("plan_not_pending", i18n.T(
			"Plan "+id+" không còn chờ duyệt (đã được duyệt hoặc từ chối)",
			"plan "+id+" is no longer pending (it was already approved or rejected)"))
	}
	return fmt.Errorf("%s Plan %s: %w", what, id, err)
}

// pending đọc Plan và đòi nó đang chờ duyệt.
func (p *Planner) pending(ctx context.Context, id string) (engine.PendingPlan, error) {
	pp, err := p.Plans.Plan(ctx, id)
	if err != nil {
		return engine.PendingPlan{}, p.storeErr(id, "đọc", err)
	}
	if pp.Status != engine.PlanPending {
		return engine.PendingPlan{}, refuse("plan_not_pending", i18n.T(
			"Plan "+id+" không còn chờ duyệt (đang "+string(pp.Status)+")",
			"plan "+id+" is no longer pending (it is "+string(pp.Status)+")"))
	}
	return pp, nil
}

// PrepareApprove dựng lại Plan trên máy bây giờ, so dấu vân tay rồi mới duyệt.
func (p *Planner) PrepareApprove(ctx context.Context, id, by string) (*Approval, error) {
	pp, err := p.pending(ctx, id)
	if err != nil {
		return nil, err
	}
	var req PlanRequest
	if err := json.Unmarshal(pp.Request, &req); err != nil {
		return nil, fmt.Errorf("đọc yêu cầu của Plan %s: %w", id, err)
	}
	pr, err := p.Prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	pv, err := pr.Preview(ctx)
	if err != nil {
		return nil, err
	}
	if fp := engine.Fingerprint(pv); fp != pp.Fingerprint {
		if err := p.Plans.UpdatePlanPreview(ctx, id, pv, fp); err != nil {
			return nil, p.storeErr(id, "cập nhật bản xem trước của", err)
		}
		return nil, refuse("plan_changed", i18n.T(
			"máy đã đổi từ lúc Plan "+id+" được đề xuất, nên Plan dựng lại khác bản bạn đã xem. Bản xem trước đã được cập nhật; xem lại rồi duyệt lần nữa.",
			"the machine changed since plan "+id+" was proposed, so the rebuilt plan differs from what you reviewed. The preview has been updated; review it and approve again."))
	}
	at := p.now().UTC()
	if err := p.Plans.DecidePlan(ctx, id, engine.PlanApplied, by, at); err != nil {
		return nil, p.storeErr(id, "duyệt", err)
	}
	pp.Status, pp.Decided, pp.DecidedBy = engine.PlanApplied, at, by
	return &Approval{Plan: pp, prepared: pr, planner: p}, nil
}

// Execute chạy Plan đã duyệt. Xem hợp đồng ở plans_types.go.
func (a *Approval) Execute(ctx context.Context, onEvent func(engine.Event)) (RunResult, error) {
	p, id := a.planner, a.Plan.ID
	// Ghi store bằng ctx không bị huỷ: lỗi trước job.start hay gặp nhất là ctx
	// hết hạn khi chờ khoá, và lúc đó vẫn phải mở lại được Plan.
	sctx := context.WithoutCancel(ctx)
	started := false
	var jobErr error
	res, err := a.prepared.Execute(ctx, func(ev engine.Event) {
		if ev.Kind == engine.EventJobStart && !started {
			started = true
			a.Plan.JobID = ev.JobID
			// onEvent không trả lỗi được: giữ lại rồi gộp vào lỗi cuối. Job
			// vẫn chạy tiếp, vì dừng giữa chừng tệ hơn thiếu một liên kết.
			if err := p.Plans.SetPlanJob(sctx, id, ev.JobID); err != nil {
				jobErr = fmt.Errorf("ghi job %s cho Plan %s: %w", ev.JobID, id, err)
			}
		}
		if onEvent != nil {
			onEvent(ev)
		}
	})
	if !started {
		if err == nil {
			err = fmt.Errorf("chạy Plan %s: job không bắt đầu", id)
		}
		// Chưa có gì chạy: trả Plan về chờ duyệt để người duyệt lại sau.
		if rerr := p.Plans.ReopenPlan(sctx, id); rerr != nil {
			return res, errors.Join(err, fmt.Errorf("mở lại Plan %s: %w", id, rerr))
		}
		a.Plan.Status, a.Plan.Decided, a.Plan.DecidedBy = engine.PlanPending, time.Time{}, ""
		return res, err
	}
	return res, errors.Join(err, jobErr)
}

// Reject chuyển một Plan đang chờ sang rejected.
func (p *Planner) Reject(ctx context.Context, id, by string) error {
	if _, err := p.pending(ctx, id); err != nil {
		return err
	}
	if err := p.Plans.DecidePlan(ctx, id, engine.PlanRejected, by, p.now().UTC()); err != nil {
		return p.storeErr(id, "từ chối", err)
	}
	return nil
}

// AgentConfig đọc cấu hình cổng agent; trống thì mặc định (panel, all).
func AgentConfig(ctx context.Context, s Settings) (AgentSettings, error) {
	out := AgentSettings{Approval: ApprovalPanel, Redact: "all"}
	approval, _, err := s.GetSetting(ctx, SettingMCPApproval)
	if err != nil {
		return AgentSettings{}, fmt.Errorf("đọc setting %s: %w", SettingMCPApproval, err)
	}
	switch ApprovalMode(approval) {
	case "":
	case ApprovalPanel, ApprovalClient:
		out.Approval = ApprovalMode(approval)
	default:
		return AgentSettings{}, fmt.Errorf("setting %s có giá trị lạ %q (chỉ nhận panel hoặc client)", SettingMCPApproval, approval)
	}
	redact, _, err := s.GetSetting(ctx, SettingAIRedact)
	if err != nil {
		return AgentSettings{}, fmt.Errorf("đọc setting %s: %w", SettingAIRedact, err)
	}
	switch redact {
	case "":
	case "all", "secrets":
		out.Redact = redact
	default:
		return AgentSettings{}, fmt.Errorf("setting %s có giá trị lạ %q (chỉ nhận all hoặc secrets)", SettingAIRedact, redact)
	}
	return out, nil
}
