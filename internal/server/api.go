package server

// Phần API của M3 (mục 9, 10 của docs/KE-HOACH.md): site, stack, job, máy,
// cơ sở dữ liệu, tổng quan. Bảng đường dẫn ở types.go và NewHandler.
//
// Chia file:
//   - api.go:        trạng thái chung, đổi lỗi sang ErrorBody, các handler chỉ đọc.
//   - api_job.go:    tạo và xoá site: chạy job ở nền, giữ mật khẩu quản trị một lần.
//   - api_health.go: health (M4): tổng quan đọc từ check_results, sự cố, sửa
//     tự động, nhận bản sửa tay, chạy health ngay, số request, log trực tiếp,
//     bộ hẹn giờ.
//   - sse.go:        luồng sự kiện của job (Server-Sent Events).

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/monitor"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// apiLockWait: chờ khoá toàn cục bao lâu trước khi trả 409 job_running.
	// Khoá đang bị giữ nghĩa là một job khác (thường gõ ở CLI, tiến trình
	// khác) đang chạy; chờ lâu hơn chỉ làm request treo.
	apiLockWait = 2 * time.Second
	// apiOverviewJobs: số job mới nhất trên Bảng điều khiển.
	apiOverviewJobs = 10
	apiJobsDefault  = 50
	apiJobsMax      = 500
)

// apiState là trạng thái của phần API, sống cùng Server.
type apiState struct {
	s *Server
	// lockWait là hằng ở trên; test đặt nhỏ lại.
	lockWait time.Duration

	mu sync.Mutex
	// busy: tiến trình này đang có một job thay đổi máy (từ lúc nhận request tới
	// khi goroutine của job xong). Cùng lúc chỉ một job (mục 4.2).
	busy bool
	// runs là các job đang chạy trong tiến trình này, theo job id.
	runs map[string]*jobRun
	// secrets giữ mật khẩu quản trị WordPress theo job id cho tới lần
	// GET /jobs/{id} đầu tiên sau khi job xong (mục 11, "Bí mật"). Chỉ trong
	// bộ nhớ: không ghi DB, không log; tpan khởi động lại thì mất.
	secrets map[string]AdminSecret

	// wg đếm goroutine job nền, để test chờ chúng xong trước khi đóng DB.
	wg sync.WaitGroup
}

// newAPIState không đọc s.deps: server_test dựng Server với Deps rỗng.
func newAPIState(s *Server) *apiState {
	return &apiState{
		s:        s,
		lockWait: apiLockWait,
		runs:     map[string]*jobRun{},
		secrets:  map[string]AdminSecret{},
	}
}

// apiSites dựng ca sử dụng site trên Deps. System được bọc để chờ khoá toàn
// cục có hạn (jobLockSystem, api_job.go).
func (s *Server) apiSites() *core.Sites {
	st := s.deps.Store
	return &core.Sites{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Journal: st,
		WPHash: s.deps.WPHash,
	}
}

func (s *Server) apiStack() *core.Stack {
	return &core.Stack{Sys: s.deps.Sys, Settings: s.deps.Store, WPCLISHA512: s.deps.WPCLISHA512}
}

// ---------------------------------------------------------------- lỗi

// apiErrorStatus chọn mã HTTP và mã lỗi cho err (mục 9, quy ước ở types.go).
// Lời từ chối của core phần lớn là 409 (máy không đạt điều kiện), trừ bốn mã là
// lỗi đầu vào (400) và site_not_found (404): người gọi cần phân biệt "gõ sai"
// và "không có" với "máy đang lệch".
func apiErrorStatus(err error) (status int, body ErrorBody) {
	var ve *validate.Error
	var re *core.RefusedError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, ErrorBody{Error: ErrorDetail{Code: ve.Code, Message: ve.Message}}
	case errors.As(err, &re):
		status = http.StatusConflict
		switch re.Code {
		case validate.CodeDomainInvalid, validate.CodeEmailInvalid, "title_invalid", "ssl_server_invalid":
			status = http.StatusBadRequest
		case "site_not_found":
			status = http.StatusNotFound
		}
		return status, ErrorBody{Error: ErrorDetail{Code: re.Code, Message: re.Message, Checks: re.Checks}}
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, ErrorBody{Error: ErrorDetail{Code: "not_found", Message: i18n.T("Không có.", "Not found.")}}
	}
	// Lỗi khác: lỗi của tpan hoặc của máy. Thông báo của core đã có ngữ cảnh và
	// không chứa mật khẩu (Secret luôn in "••••"); người đọc là quản trị viên,
	// nên trả nguyên văn để họ tự xem được, kèm log.
	return http.StatusInternalServerError, ErrorBody{Error: ErrorDetail{
		Code:    "internal",
		Message: i18n.T(err.Error(), "internal error: "+err.Error()),
	}}
}

// apiFail ghi err thành ErrorBody.
func apiFail(w http.ResponseWriter, r *http.Request, err error) {
	status, body := apiErrorStatus(err)
	if status == http.StatusInternalServerError {
		slog.Error("API lỗi", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, body)
}

func apiDryRun(r *http.Request) bool { return r.URL.Query().Get("dry_run") == "1" }

// apiNonNil để danh sách rỗng ra JSON là [] chứ không phải null (giao diện
// không phải kiểm null).
func apiNonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// ---------------------------------------------------------------- máy, stack

func (s *Server) apiMachine(ctx context.Context) Machine {
	m := monitor.Read(ctx, s.deps.Sys)
	return Machine{
		CPUPercent: m.CPUPercent, CPUs: m.CPUs, Load1: m.Load1,
		MemTotal: m.MemTotal, MemUsed: m.MemUsed,
		SwapTotal: m.SwapTotal, SwapUsed: m.SwapUsed,
		Disk:     DiskUsage{Path: m.DiskPath, Total: m.DiskTotal, Used: m.DiskUsed},
		Uptime:   m.Uptime,
		Measured: s.now(),
		Errors:   m.Errors,
	}
}

func (s *Server) handleMachine(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.apiMachine(r.Context()))
}

func (s *Server) handleStack(w http.ResponseWriter, r *http.Request) {
	st, err := s.apiStack().Status(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	st.Services = apiNonNil(st.Services)
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleChecks(w http.ResponseWriter, r *http.Request) {
	h, err := s.apiStack().Health(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	h.Results = apiNonNil(h.Results)
	writeJSON(w, http.StatusOK, h)
}

// ---------------------------------------------------------------- site

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	list, err := s.apiSites().List(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(list))
}

// handleSite trả core.SiteInfo, chạy phép kiểm của site ngay lúc gọi: trang
// một site là nơi người dùng xem kỹ, cần kết quả mới. Bảng điều khiển thì đọc
// kết quả health đã ghi (handleOverview).
func (s *Server) handleSite(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	info, err := s.apiSites().Info(r.Context(), d)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	info.Checks = apiNonNil(info.Checks)
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.apiSites().Databases(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	out := make([]DatabaseRow, len(dbs))
	for i, d := range dbs {
		out[i] = DatabaseRow{Domain: d.Domain, DB: d.DB, User: d.User, SizeBytes: d.SizeBytes, Tables: d.Tables}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- job

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	limit := apiJobsDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > apiJobsMax {
			writeError(w, http.StatusBadRequest, "limit_invalid", i18n.T(
				"limit phải là số từ 1 tới "+strconv.Itoa(apiJobsMax)+".",
				"limit must be a number from 1 to "+strconv.Itoa(apiJobsMax)+"."))
			return
		}
		limit = n
	}
	jobs, err := s.deps.Store.Jobs(r.Context(), limit)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(jobs))
}

// handleJob trả job, mọi sự kiện, và mật khẩu quản trị nếu có (đúng một lần).
func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Job của tiến trình này đã phát job.done nhưng goroutine chưa xong (đang
	// ghi bảng sites, chưa cất mật khẩu): chờ nó, nếu không giao diện gọi ngay
	// sau job.done sẽ thấy job "xong" mà không có mật khẩu, và mất luôn.
	if run := s.api.run(id); run != nil && run.feed.finished() {
		select {
		case <-run.done:
		case <-r.Context().Done():
			return
		}
	}
	job, err := s.deps.Store.Job(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job_not_found", i18n.T("Không có job "+id+".", "No job "+id+"."))
			return
		}
		apiFail(w, r, err)
		return
	}
	evs, err := s.deps.Store.Events(r.Context(), id, 0)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	d := JobDetail{Job: job, Events: apiNonNil(evs)}
	if job.Status != engine.JobRunning {
		d.Secret = s.api.takeSecret(id)
	}
	writeJSON(w, http.StatusOK, d)
}
