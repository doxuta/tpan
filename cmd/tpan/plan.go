package main

// Lệnh `tpan plan`: xem, duyệt và từ chối các Plan chờ duyệt (mục 4.2, 8.1,
// 8.3 và 9 của docs/KE-HOACH.md). Plan do agent (qua `tpan mcp`) hoặc AI trong
// panel đề xuất không tự chạy: người có root trên máy duyệt ở đây hoặc trên
// giao diện web. Mọi quyết định (dựng lại, so dấu vân tay) nằm ở core.Planner,
// nên CLI và API cho cùng một kết quả.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(planCmd) }

// planListLimit giống API (GET /api/v1/plans): danh sách dài hơn thì người
// lọc theo --status.
const planListLimit = 100

func planCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "plan",
		Short: "Xem, duyệt và từ chối các Plan chờ duyệt (do agent hoặc AI đề xuất)",
	}
	c.AddCommand(planListCmd(e), planShowCmd(e), planApproveCmd(e), planRejectCmd(e))
	return c
}

// newPlanner dựng core.Planner trên máy và DB thật. NotFound, NotPending phải
// đặt: thiếu chúng thì "không có Plan" thành lỗi trần thay vì lời từ chối có mã.
func newPlanner(e *env, st *store.Store) *core.Planner {
	return &core.Planner{
		Sites: &core.Sites{Sys: e.sys, Settings: st, Store: st, Journal: st, Backups: st, Accounts: st, Samples: st},
		Stack: &core.Stack{Sys: e.sys, Settings: st, Journal: st},
		Plans: st, Checks: st, Files: st, Backups: st,
		NotFound: store.ErrNotFound, NotPending: store.ErrPlanNotPending,
	}
}

// loadPlan đọc một Plan; không có thì lời từ chối plan_not_found (cùng mã và
// câu với core, để script nhận một mã dù đi đường nào).
func loadPlan(ctx context.Context, st *store.Store, id string) (engine.PendingPlan, error) {
	pp, err := st.Plan(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return pp, &core.RefusedError{Code: "plan_not_found", Message: i18n.T(
			"không có Plan "+id+"; xem danh sách bằng: sudo tpan plan list",
			"there is no plan "+id+"; list plans with: sudo tpan plan list")}
	}
	return pp, err
}

func planListCmd(e *env) *cobra.Command {
	var (
		status string
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "list [--status pending|applied|rejected]",
		Short: "Liệt kê các Plan, mới nhất trước",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch engine.PlanStatus(status) {
			case "", engine.PlanPending, engine.PlanApplied, engine.PlanRejected:
			default:
				return fail(e, asJSON, &core.RefusedError{Code: "status_invalid", Message: i18n.T(
					"--status chỉ nhận pending, applied hoặc rejected (nhận được "+quote(status)+")",
					"--status accepts only pending, applied or rejected (got "+quote(status)+")")})
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := openStore(cmd.Context(), e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			list, err := st.Plans(cmd.Context(), engine.PlanStatus(status), planListLimit)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, list)
			}
			printPlanList(e, list)
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", "", "chỉ lấy Plan ở trạng thái này: pending, applied hoặc rejected")
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func planShowCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Xem một Plan: nguồn gốc và bản xem trước từng bước",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := openStore(cmd.Context(), e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			pp, err := loadPlan(cmd.Context(), st, args[0])
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				return writeJSON(e.stdout, pp)
			}
			printPlanHeader(e, pp)
			printPreview(e, pp.Preview)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func planApproveCmd(e *env) *cobra.Command {
	var f planRun
	c := &cobra.Command{
		Use:   "approve <id>",
		Short: "Duyệt rồi chạy một Plan (dựng lại trên máy bây giờ; khác bản đã xem thì phải xem lại)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, id := cmd.Context(), args[0]
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)

			// Hỏi lại TRƯỚC PrepareApprove: PrepareApprove chuyển Plan sang
			// applied ngay, nên người trả lời "không" sau đó thì Plan kẹt ở
			// applied mà không bao giờ chạy. Bản đem ra hỏi là bản xem trước đã
			// lưu, đúng thứ panel cho người duyệt xem; PrepareApprove dựng lại
			// và so dấu vân tay với chính bản này.
			if !f.yes {
				if f.asJSON || !e.isTerminal() {
					return fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
						"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng sudo tpan plan show "+id+" rồi chạy lại với --yes",
						"cannot ask for confirmation (no terminal, or --json is set); review with sudo tpan plan show "+id+", then run again with --yes")})
				}
				pp, err := loadPlan(ctx, st, id)
				if err != nil {
					return fail(e, false, err)
				}
				if pp.Status != engine.PlanPending {
					return fail(e, false, &core.RefusedError{Code: "plan_not_pending", Message: i18n.T(
						"Plan "+id+" không còn chờ duyệt (đang "+string(pp.Status)+")",
						"plan "+id+" is no longer pending (it is "+string(pp.Status)+")")})
				}
				printPlanHeader(e, pp)
				printPreview(e, pp.Preview)
				if !confirm(e) {
					printf(e.stdout, "%s\n", e.tr("Đã huỷ, Plan vẫn chờ duyệt.", "Cancelled, the plan is still pending."))
					return &exitCode{1}
				}
			}

			appr, err := newPlanner(e, st).PrepareApprove(ctx, id, "cli")
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			out := e.stdout
			if f.asJSON {
				out = e.stderr
			}
			res, err := appr.Execute(ctx, previewProgress(out, e.lang, f.verbose, appr.Plan.Preview).onEvent)
			cliAudit(ctx, e, st, "plan.approve", id, res.Report.Job)
			if err != nil && res.Report.Job.ID == "" {
				// Job không bắt đầu: core đã trả Plan về chờ duyệt.
				return err
			}
			if f.asJSON {
				if werr := writeJSON(e.stdout, planApproveJSONOf(appr.Plan, res)); werr != nil {
					return werr
				}
			} else {
				printReport(e, res.Report)
				printPlanAdminPassword(e, appr.Plan, res)
			}
			if err != nil {
				return err
			}
			if res.Report.Job.Status != engine.JobSucceeded {
				return &exitCode{1}
			}
			return nil
		},
	}
	fl := c.Flags()
	fl.BoolVar(&f.yes, "yes", false, "không hỏi xác nhận")
	fl.BoolVar(&f.asJSON, "json", false, "in kết quả dạng JSON")
	fl.BoolVar(&f.verbose, "verbose", false, "in lệnh và output của mọi bước")
	return c
}

func planRejectCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "reject <id>",
		Short: "Từ chối một Plan đang chờ duyệt (không chạy gì)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, id := cmd.Context(), args[0]
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			if err := newPlanner(e, st).Reject(ctx, id, "cli"); err != nil {
				return fail(e, asJSON, err)
			}
			auditNoJob(ctx, e, st, "plan.reject", id)
			if asJSON {
				return writeJSON(e.stdout, map[string]string{"id": id, "status": string(engine.PlanRejected)})
			}
			printf(e.stdout, "%s %s\n", e.tr("Đã từ chối Plan", "Rejected plan"), id)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// auditNoJob ghi nhật ký cho thao tác không tạo job (từ chối Plan, đổi cấu
// hình). cliAudit bỏ qua khi chưa có job, vì với các lệnh chạy Plan đó nghĩa
// là chưa có gì xảy ra; ở đây thì thao tác đã xong mà không cần job. Ghi lỗi
// chỉ in ra stderr, như cliAudit.
func auditNoJob(ctx context.Context, e *env, w auditWriter, action, target string) {
	if err := w.AddAudit(ctx, store.AuditEntry{Time: time.Now(), IP: "cli", Action: action, Target: target, Result: "ok"}); err != nil {
		printf(e.stderr, "tpan: %v\n", err)
	}
}

// quote đặt giá trị người dùng gõ trong ngoặc kép, có thoát ký tự lạ (xuống
// dòng...), để lời từ chối không bị chính giá trị đó làm rối.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// previewProgress dựng bộ in tiến độ từ bản xem trước: Approval không lộ
// engine.Plan đã dựng (newProgress cần nó), còn bản xem trước có đủ id, tiêu
// đề và ghi chú hoàn tác của từng bước, và chính là bản vừa so dấu vân tay.
func previewProgress(w io.Writer, lang string, verbose bool, pv engine.Preview) *progress {
	infos := make([]engine.StepInfo, len(pv.Steps))
	for i, s := range pv.Steps {
		infos[i] = s.Info
	}
	return progressOf(w, lang, verbose, infos)
}

// planApproveJSON là output --json của plan approve. Như site add --json: mật
// khẩu quản trị WordPress ở dạng chữ thật đúng một lần, chỉ khi Plan vừa cài
// WordPress (core.RunResult giữ nó dạng Secret).
type planApproveJSON struct {
	Plan          engine.PendingPlan `json:"plan"`
	Report        engine.Report      `json:"report"`
	WPInstalled   bool               `json:"wp_installed"`
	AdminUser     string             `json:"admin_user,omitempty"`
	AdminPassword string             `json:"admin_password,omitempty"`
}

func planApproveJSONOf(pp engine.PendingPlan, res core.RunResult) planApproveJSON {
	out := planApproveJSON{Plan: pp, Report: res.Report, WPInstalled: res.WPInstalled}
	if res.WPInstalled {
		out.AdminUser, out.AdminPassword = res.AdminUser, res.AdminPassword.Reveal()
	}
	return out
}

// printPlanAdminPassword dùng lại printAdminPassword của site add. Domain lấy
// từ yêu cầu đã lưu của Plan (đã chuẩn hoá lúc đề xuất), cần cho đường đăng nhập.
func printPlanAdminPassword(e *env, pp engine.PendingPlan, res core.RunResult) {
	if !res.WPInstalled {
		return
	}
	var req core.PlanRequest
	_ = json.Unmarshal(pp.Request, &req) // Request đã giải được một lần trong PrepareApprove
	sr := core.SiteAddResult{Report: res.Report, WPInstalled: true, AdminUser: res.AdminUser, AdminPassword: res.AdminPassword}
	// Domain hỏng thì vẫn in mật khẩu (chỉ thiếu đường đăng nhập): mất nó là
	// không vào được WordPress.
	sr.Site.Domain, _ = validate.ParseDomain(req.Domain)
	printAdminPassword(e, sr)
}

func originText(e *env, o engine.Origin) string {
	switch o {
	case engine.OriginHuman:
		return e.tr("người", "a person")
	case engine.OriginAI:
		return e.tr("AI trong panel", "the panel AI")
	case engine.OriginMCP:
		return e.tr("agent qua MCP", "an agent over MCP")
	}
	return string(o)
}

// printPlanHeader in nguồn gốc của Plan trước bản xem trước: người duyệt cần
// biết ai đề xuất, và nhất là khi phiên đó đã đọc nội dung do người lạ gửi tới.
func printPlanHeader(e *env, pp engine.PendingPlan) {
	w := e.stdout
	row := func(k, v string) { printf(w, "%-14s %s\n", k, v) }
	row("Plan:", pp.ID)
	row(e.tr("Trạng thái:", "Status:"), string(pp.Status))
	from := originText(e, pp.Origin)
	if pp.SessionID != "" {
		from += " (" + e.tr("phiên", "session") + " " + pp.SessionID + ")"
	}
	row(e.tr("Đề xuất bởi:", "Proposed by:"), from)
	row(e.tr("Lúc:", "At:"), pp.Created.Local().Format("2006-01-02 15:04"))
	if pp.Status != engine.PlanPending {
		row(e.tr("Quyết định:", "Decided:"), pp.DecidedBy+" "+pp.Decided.Local().Format("2006-01-02 15:04"))
	}
	if pp.JobID != "" {
		row("Job:", pp.JobID)
	}
	if pp.Untrusted {
		printf(w, "\n%s\n", e.tr(
			"CẢNH BÁO: phiên đề xuất Plan này đã đọc nội dung do người lạ gửi tới (log truy cập, output lệnh). Nội dung đó có thể dụ AI đề xuất việc có hại: đọc kỹ từng bước trước khi duyệt.",
			"WARNING: the session that proposed this plan read content sent by strangers (access logs, command output). That content may trick the AI into proposing something harmful: read every step before approving."))
	}
	printf(w, "\n")
}

func printPlanList(e *env, list []engine.PendingPlan) {
	w := e.stdout
	if len(list) == 0 {
		printf(w, "%s\n", e.tr("Không có Plan nào.", "No plans."))
		return
	}
	printf(w, "%-16s %-8s %-6s %-12s %-30s %s\n", "ID", e.tr("TRẠNG THÁI", "STATUS"), e.tr("TỪ", "FROM"), e.tr("LOẠI", "KIND"), e.tr("ĐỐI TƯỢNG", "TARGET"), e.tr("TẠO LÚC", "CREATED"))
	for _, p := range list {
		mark := ""
		if p.Untrusted {
			mark = "  " + e.tr("[đã đọc nội dung lạ]", "[read untrusted content]")
		}
		printf(w, "%-16s %-8s %-6s %-12s %-30s %s%s\n", p.ID, p.Status, p.Origin, p.Kind, p.Target, p.Created.Local().Format("2006-01-02 15:04"), mark)
	}
}
