package server

// API của Plan chờ duyệt và cổng agent (M7, mục 4.2, 8.3, 9 của
// docs/KE-HOACH.md). Bảng đường dẫn và hợp đồng: plans_types.go.
//
// Duyệt một Plan đi theo đúng vòng đời job của tạo site (api_job.go): giành
// chỗ chạy job, gọi core.Planner.PrepareApprove ngay trong request (dựng lại
// Plan trên máy bây giờ và so dấu vân tay) để lời từ chối về thẳng người bấm,
// rồi jobStart chạy Plan ở nền.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
)

// apiPlansMax: danh sách Plan trả tối đa chừng này dòng (hợp đồng).
const apiPlansMax = 100

// apiPlanner dựng Planner trên Deps cho một request. Cả Sites lẫn Stack đều
// chờ khoá toàn cục có hạn (jobLockSystem) và có Journal: Plan sửa stack chạy
// qua Stack.Execute, mà Stack của apiStack() chỉ dùng để xem (không Journal).
func (s *Server) apiPlanner() *core.Planner {
	st := s.deps.Store
	return &core.Planner{
		Sites: s.apiSites(),
		Stack: &core.Stack{
			Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
			Settings: st, Journal: st,
			WPCLISHA512: s.deps.WPCLISHA512,
		},
		Plans: st, Checks: st, Files: st,
		NotFound:   store.ErrNotFound,
		NotPending: store.ErrPlanNotPending,
		Now:        s.now,
	}
}

// planErrorStatus là apiErrorStatus cộng hai mã của Plan: apiErrorStatus (M3)
// coi mọi lời từ chối là 409, nhưng "không có Plan này" và "không có kết quả
// của phép kiểm này" (Plan health.fix, M8) là 404, còn "loại Plan không có" là
// lỗi đầu vào (400). Bọc ở đây thay vì sửa api.go để không đụng
// file của M3.
func planErrorStatus(err error) (int, ErrorBody) {
	status, body := apiErrorStatus(err)
	var re *core.RefusedError
	if errors.As(err, &re) {
		switch re.Code {
		case "plan_not_found", "check_not_found":
			status = http.StatusNotFound
		case "plan_kind_invalid":
			status = http.StatusBadRequest
		}
	}
	return status, body
}

// planFail ghi err thành ErrorBody theo planErrorStatus.
func planFail(w http.ResponseWriter, r *http.Request, err error) {
	status, body := planErrorStatus(err)
	if status == http.StatusInternalServerError {
		slog.Error("API lỗi", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, body)
}

// planRefused ghi nhật ký lời từ chối (kết quả là mã lỗi) rồi trả lỗi.
func (s *Server) planRefused(w http.ResponseWriter, r *http.Request, action, id string, err error) {
	_, body := planErrorStatus(err)
	s.audit(r, action, id, body.Error.Code, "")
	planFail(w, r, err)
}

func planNotFound(w http.ResponseWriter, id string) {
	writeError(w, http.StatusNotFound, "plan_not_found", i18n.T("Không có Plan "+id+".", "No plan "+id+"."))
}

// planID đọc id ở đường dẫn. Id do core sinh luôn là 16 ký tự hex; chuỗi khác
// chắc chắn không có trong bảng, và chặn ở đây để chuỗi lạ (xuống dòng, ký tự
// điều khiển) không lọt vào nhật ký.
func planID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if len(id) != 16 || strings.Trim(id, "0123456789abcdef") != "" {
		writeError(w, http.StatusNotFound, "plan_not_found", i18n.T("Không có Plan này.", "No such plan."))
		return "", false
	}
	return id, true
}

// handlePlans: GET /api/v1/plans[?status=...], mới nhất trước.
func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	status := engine.PlanStatus(r.URL.Query().Get("status"))
	switch status {
	case "", engine.PlanPending, engine.PlanApplied, engine.PlanRejected:
	default:
		writeError(w, http.StatusBadRequest, "status_invalid", i18n.T(
			"status chỉ nhận pending, applied hoặc rejected.", "status must be pending, applied or rejected."))
		return
	}
	list, err := s.deps.Store.Plans(r.Context(), status, apiPlansMax)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(list))
}

// handlePlan: GET /api/v1/plans/{id}.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(w, r)
	if !ok {
		return
	}
	p, err := s.deps.Store.Plan(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		planNotFound(w, id)
		return
	}
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePlanApprove: POST /api/v1/plans/{id}/approve → 202 JobStarted.
func (s *Server) handlePlanApprove(w http.ResponseWriter, r *http.Request) {
	const action = "plan.approve"
	id, ok := planID(w, r)
	if !ok {
		return
	}
	// Giành chỗ chạy job TRƯỚC khi duyệt: PrepareApprove chuyển Plan sang
	// applied, nên duyệt xong mà không chạy được (đang có job khác) thì Plan
	// bị kẹt ở applied mà không có job nào.
	if !s.api.claim() {
		s.jobBusy(w, r, action, id)
		return
	}
	ap, err := s.apiPlanner().PrepareApprove(r.Context(), id, "web:"+clientIP(r))
	if err != nil {
		// plan_changed: Planner đã ghi bản xem trước mới; giao diện GET lại.
		s.api.release()
		s.planRefused(w, r, action, id, err)
		return
	}
	// kick "": Plan có thể đụng mọi site (sửa stack), nên sau job health kiểm
	// nhẹ lại mọi thứ thay vì chỉ một domain (M4, Scheduler.Kick).
	s.jobStart(w, r, jobOpts{action: action, target: id}, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		res, err := ap.Execute(ctx, onEvent)
		out := jobResult{status: res.Report.Job.Status}
		// Như POST /sites: kể cả khi err khác nil, mật khẩu vẫn phải tới
		// người dùng, mất nó là mất tài khoản quản trị.
		if res.WPInstalled {
			out.secret = &AdminSecret{
				URL:      "http://" + ap.Plan.Target + "/wp-admin/",
				User:     res.AdminUser,
				Password: res.AdminPassword.Reveal(),
			}
		}
		return out, err
	})
}

// handlePlanReject: POST /api/v1/plans/{id}/reject → 204.
func (s *Server) handlePlanReject(w http.ResponseWriter, r *http.Request) {
	const action = "plan.reject"
	id, ok := planID(w, r)
	if !ok {
		return
	}
	if err := s.apiPlanner().Reject(r.Context(), id, "web:"+clientIP(r)); err != nil {
		s.planRefused(w, r, action, id, err)
		return
	}
	s.audit(r, action, id, "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleAgent: GET /api/v1/agent. Chỉ xem: chế độ duyệt đổi bằng
// `sudo tpan mcp config` trên máy chủ (cần root), vì bật chế độ client là
// cho agent tự chạy Plan, không để một phiên web đổi được.
func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	cfg, err := core.AgentConfig(r.Context(), s.deps.Store)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	host := s.deps.Hostname
	if host == "" {
		host = "<hostname>"
	}
	// <user> giữ nguyên chữ để người dùng tự thay bằng user SSH của họ.
	// sudo -n: không hỏi mật khẩu (stdio là kênh MCP, không có ai gõ); user
	// không phải root thì cần sudo không mật khẩu cho tpan.
	remote := "ssh <user>@" + host + " sudo -n tpan mcp"
	writeJSON(w, http.StatusOK, AgentInfo{
		Approval:      string(cfg.Approval),
		Redact:        cfg.Redact,
		Hostname:      s.deps.Hostname,
		ClaudeCommand: "claude mcp add --transport stdio tpan -- " + remote,
		CodexCommand:  "codex mcp add tpan -- " + remote,
	})
}
