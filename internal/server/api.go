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

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/monitor"
	"github.com/doxuta/tpan/internal/s3"
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
	secrets map[string]heldSecret

	// wg đếm goroutine job nền, để test chờ chúng xong trước khi đóng DB.
	wg sync.WaitGroup
}

// newAPIState không đọc s.deps: server_test dựng Server với Deps rỗng.
func newAPIState(s *Server) *apiState {
	return &apiState{
		s:        s,
		lockWait: apiLockWait,
		runs:     map[string]*jobRun{},
		secrets:  map[string]heldSecret{},
	}
}

// apiSites dựng ca sử dụng site trên Deps. System được bọc để chờ khoá toàn
// cục có hạn (jobLockSystem, api_job.go).
func (s *Server) apiSites() *core.Sites {
	st := s.deps.Store
	return &core.Sites{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Journal: st, Backups: st,
		// Accounts (M13): thiếu thì tạo site bỏ qua hạn mức gói và không kiểm
		// chủ có thật, đang active.
		Accounts: st,
		Samples:  st, // M17: xoá hẳn site thì xoá số liệu của nó
		WPHash:   s.deps.WPHash,
	}
}

func (s *Server) apiStack() *core.Stack {
	return &core.Stack{Sys: s.deps.Sys, Settings: s.deps.Store, WPCLISHA512: s.deps.WPCLISHA512}
}

// ---------------------------------------------------------------- lỗi

// apiErrorStatus chọn mã HTTP và mã lỗi cho err (mục 9, quy ước ở types.go).
// Lời từ chối của core phần lớn là 409 (máy không đạt điều kiện), trừ các mã là
// lỗi đầu vào (400) và site_not_found (404): người gọi cần phân biệt "gõ sai"
// và "không có" với "máy đang lệch". M6 thêm hai mã đầu vào: sites_file_invalid
// (danh sách dán hay sites.yaml sai) và trash_days_invalid (số ngày giữ ngoài khoảng).
func apiErrorStatus(err error) (status int, body ErrorBody) {
	var ve *validate.Error
	var re *core.RefusedError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, ErrorBody{Error: ErrorDetail{Code: ve.Code, Message: ve.Message}}
	case errors.As(err, &re):
		status = http.StatusConflict
		switch re.Code {
		case validate.CodeDomainInvalid, validate.CodeEmailInvalid, "title_invalid", "ssl_server_invalid",
			"sites_file_invalid", "trash_days_invalid", "apply_empty", "domain_duplicate", "prune_empty",
			// M9: số bản giữ, giờ backup sai là lỗi đầu vào.
			"backup_keep_invalid", "backup_at_invalid",
			// M13: tên, mật khẩu, vai, giới hạn gói, charset, ghi chú sai.
			"args_invalid", "password_too_short", "note_invalid", core.CodeCollationInvalid,
			validate.CodeUserNameInvalid, validate.CodeUserNameReserved,
			validate.CodePackageNameInvalid, validate.CodeDBNameInvalid,
			// M18: trường của đích S3 và đầu vào của cloud-init sai. Mã M18
			// khác (s3_not_configured, s3_unreachable, backup_not_on_machine…)
			// là "máy chưa đạt điều kiện": 409 như mặc định.
			s3.CodeEndpointInvalid, s3.CodeRegionInvalid, s3.CodeBucketInvalid, s3.CodePrefixInvalid,
			s3.CodeKeysInvalid, s3.CodeKeepInvalid, s3.CodeCAInvalid,
			core.CodeCloudInitInvalid, core.CodeCloudInitTooLarge,
			// M17: trần của gói sai, có trần mà gói không bật lồng, tên miền
			// dành cho phép thử tấn công.
			core.CodePackageLimitInvalid, core.CodePackageLimitsNeedCage, core.CodeDomainReserved:
			status = http.StatusBadRequest
		case "site_not_found",
			// M9: id backup không có (hoặc không thuộc site đó) là "không có".
			"backup_not_found",
			// M13: lời 404 của guard (routes.go) và của core.
			"job_not_found", "incident_not_found",
			core.CodeUserNotFound, core.CodePackageNotFound, core.CodeDBNotFound,
			// M17: số liệu của site ngoài lồng (không có cgroup riêng để đọc).
			core.CodeUsageNotCaged:
			status = http.StatusNotFound
		}
		return status, ErrorBody{Error: ErrorDetail{Code: re.Code, Message: re.Message, Checks: re.Checks}}
	}
	// M28: lỗi của cửa sitefs (file trong thư mục site là symlink, FIFO, quá
	// lớn, không thuộc site...) là trạng thái site tự tạo ra, không phải lỗi của
	// tpan: 409 site_path_unsafe. "Không có" thì đi tiếp như cũ.
	if body, ok := sitePathUnsafe(err); ok {
		return http.StatusConflict, body
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, ErrorBody{Error: ErrorDetail{Code: "not_found", Message: i18n.T("Không có.", "Not found.")}}
	case errors.Is(err, store.ErrPasswordBusy):
		// M28 đợt 2, P2: argon2 hết chỗ ở mọi route băm mật khẩu (tạo user,
		// đặt lại mật khẩu, cloud-init). apiFail thêm Retry-After.
		return http.StatusTooManyRequests, authBusyBody()
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
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", authBusyAfter)
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

// handleChecks trả phép kiểm chung của máy. User thường không thấy phần máy
// (mục 6.12): chỉ trạng thái cài đặt, không chạy phép kiểm nào.
func (s *Server) handleChecks(w http.ResponseWriter, r *http.Request) {
	if !who(r).IsAdmin() {
		cfg, err := s.apiStack().Config(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, core.Health{Installed: cfg.PHPVersion != "", Config: cfg, Results: []checks.Result{}})
		return
	}
	h, err := s.apiStack().Health(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	h.Results = apiNonNil(h.Results)
	writeJSON(w, http.StatusOK, h)
}

// ---------------------------------------------------------------- site

// handleSites: lọc theo chủ ngay ở truy vấn (store.SitesOf).
func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Store.SitesOf(r.Context(), scopeOf(r))
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
	hide, err := s.foreignCheck(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	info.Checks = apiNonNil(keepChecks(info.Checks, hide))
	writeJSON(w, http.StatusOK, info)
}

// handleDatabases: DB của site và DB rời, lọc theo chủ (core.DBs.List).
func (s *Server) handleDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.apiDBs().List(r.Context(), scopeOf(r))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	out := make([]DatabaseRow, len(dbs))
	for i, d := range dbs {
		out[i] = databaseRow(d)
	}
	writeJSON(w, http.StatusOK, out)
}

func databaseRow(d core.Database) DatabaseRow {
	return DatabaseRow{
		Domain: d.Domain, DB: d.DB, User: d.User, SizeBytes: d.SizeBytes, Tables: d.Tables,
		Kind: d.Kind, Owner: d.Owner, Charset: d.Charset, Collation: d.Collation, Note: d.Note,
		Suspended: d.Suspended, Status: d.Status,
	}
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
	// M13: lọc theo chủ; admin thêm ?user= để lọc theo người chạy (cột "Ai
	// làm" của màn Lịch sử). User thường gửi ?user= thì bỏ qua: họ chỉ thấy
	// job của mình dù ai chạy.
	actor := ""
	if v := r.URL.Query().Get("user"); v != "" && who(r).IsAdmin() {
		if _, err := validate.ParseUserName(v); err != nil {
			apiFail(w, r, err)
			return
		}
		actor = v
	}
	jobs, err := s.deps.Store.JobsOf(r.Context(), scopeOf(r), actor, limit)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(jobs))
}

// handleJob trả job, mọi sự kiện, và mật khẩu quản trị nếu có (đúng một lần):
// Secret cho job site.add, Secrets cho job hàng loạt (POST /apply).
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
	hide, err := s.foreignCheck(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	d := JobDetail{Job: job, Events: apiNonNil(keepEvents(evs, hide))}
	// Phiên "xem như" chỉ đọc: không được lấy mất mật khẩu hiện một lần của
	// user (admin mở job vừa xong của A lúc đang xem như A thì A mất mật khẩu).
	if job.Status != engine.JobRunning && !who(r).ReadOnly() {
		sec := s.api.takeSecret(id)
		d.Secret, d.Secrets, d.DBSecret = sec.one, sec.many, sec.db
	}
	writeJSON(w, http.StatusOK, d)
}
