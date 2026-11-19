// Package server chạy HTTP server của tpan: API dưới /api/v1 và giao diện web
// (đã được nhúng sẵn vào file chạy, xem package web).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/web"
)

// RunOptions cấu hình Run.
type RunOptions struct {
	Listen string
	// CertFile và KeyFile là chứng chỉ của panel (tpan setup sinh ở
	// /etc/tpan/tls). Cả hai rỗng thì nghe HTTP thường: chỉ dùng cho
	// make dev-api và --demo trên máy dev, không bao giờ trên máy chủ.
	CertFile, KeyFile string
	Deps              Deps
}

// Run nghe cho tới khi ctx bị huỷ, rồi tắt êm (chờ các request đang chạy tối
// đa 10 giây).
func Run(ctx context.Context, o RunOptions) error {
	s := newServer(o.Deps)
	if o.Deps.Store != nil {
		// Health định kỳ (mục 6.7) sống cùng tiến trình serve. Dựng trước khi
		// nghe để mọi request thấy s.sched (Kick sau mỗi job).
		s.sched = &health.Scheduler{Runner: s.healthRunner(), Busy: s.healthBusy}
		// ctx riêng: Run có thể trả về khi ctx ngoài còn sống (nghe cổng lỗi),
		// lúc đó vẫn phải dừng được bộ hẹn giờ.
		sctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.sched.Run(sctx)
		}()
		// Dừng rồi chờ vòng kiểm đang dở xong hẳn (ctx đã huỷ nên nó dừng
		// nhanh) trước khi trả về: người gọi đóng store ngay sau Run.
		defer func() { cancel(); <-done }()
	}
	srv := &http.Server{
		Addr:              o.Listen,
		Handler:           s.handler(web.DistFS()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("tpan đang chạy", "listen", o.Listen, "tls", o.CertFile != "", "demo", o.Deps.Demo, "version", buildinfo.Version)
	var err error
	if o.CertFile != "" || o.KeyFile != "" {
		err = srv.ListenAndServeTLS(o.CertFile, o.KeyFile)
	} else {
		err = srv.ListenAndServe()
	}
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Server giữ trạng thái của hai phần: đăng nhập (authState, auth.go) và API
// (apiState, api.go). Mỗi phần tự định nghĩa kiểu trạng thái của mình.
type Server struct {
	deps Deps
	auth *authState
	api  *apiState
	// sched là bộ hẹn giờ health; nil khi không chạy bằng Run (test dùng
	// NewHandler), khi đó kick không làm gì.
	sched *health.Scheduler
}

func (s *Server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now()
}

// audit ghi một dòng nhật ký cho request r (mục 11). Lỗi ghi nhật ký chỉ được
// log ra, không làm hỏng thao tác đã xong. Deps.Store nil (test) thì bỏ qua.
func (s *Server) audit(r *http.Request, action, target, result, jobID string) {
	if s.deps.Store == nil {
		return
	}
	err := s.deps.Store.AddAudit(r.Context(), store.AuditEntry{
		Time: s.now(), IP: clientIP(r), Action: action, Target: target, Result: result, JobID: jobID,
	})
	if err != nil {
		slog.Error("ghi nhật ký", "action", action, "err", err)
	}
}

// clientIP là IP của kết nối trực tiếp. Không đọc X-Forwarded-For: panel không
// đứng sau proxy, và tin header đó thì ai cũng tự đổi IP để né khoá đăng nhập.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// NewHandler ghép API và giao diện web vào một handler. Tách riêng để test được
// mà không cần mở cổng thật. Bảng đường dẫn và ý nghĩa: types.go, types_m5.go.
func NewHandler(d Deps, dist fs.FS) http.Handler {
	return newServer(d).handler(dist)
}

// newServer dựng Server; tách khỏi NewHandler để Run giữ được *Server (gắn bộ
// hẹn giờ health vào nó).
func newServer(d Deps) *Server {
	s := &Server{deps: d}
	s.auth = newAuthState(s)
	s.api = newAPIState(s)
	return s
}

func (s *Server) handler(dist fs.FS) http.Handler {
	mux := http.NewServeMux()
	// Công khai.
	mux.HandleFunc("GET /api/v1/health", handleHealth)
	mux.HandleFunc("POST /api/v1/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/session", s.handleSession)

	// Cần phiên đăng nhập.
	auth := s.requireSession
	mux.Handle("POST /api/v1/account/password", auth(s.handlePasswordChange))
	mux.Handle("GET /api/v1/account/sessions", auth(s.handleSessions))
	mux.Handle("DELETE /api/v1/account/sessions/{id}", auth(s.handleSessionDelete))

	mux.Handle("GET /api/v1/overview", auth(s.handleOverview))
	mux.Handle("GET /api/v1/machine", auth(s.handleMachine))
	mux.Handle("GET /api/v1/stack", auth(s.handleStack))
	mux.Handle("GET /api/v1/checks", auth(s.handleChecks))
	mux.Handle("GET /api/v1/sites", auth(s.handleSites))
	mux.Handle("POST /api/v1/sites", auth(s.handleSiteCreate))
	mux.Handle("GET /api/v1/sites/{domain}", auth(s.handleSite))
	mux.Handle("DELETE /api/v1/sites/{domain}", auth(s.handleSiteDelete))
	mux.Handle("GET /api/v1/jobs", auth(s.handleJobs))
	mux.Handle("GET /api/v1/jobs/{id}", auth(s.handleJob))
	mux.Handle("GET /api/v1/jobs/{id}/events", auth(s.handleJobEvents))
	mux.Handle("GET /api/v1/databases", auth(s.handleDatabases))
	// M5: SSL và trang bảo mật (types_m5.go).
	mux.Handle("GET /api/v1/ssl", auth(s.handleSSL))
	mux.Handle("POST /api/v1/ssl/settings", auth(s.handleSSLSettings))
	mux.Handle("POST /api/v1/sites/{domain}/ssl", auth(s.handleSiteSSL))
	mux.Handle("GET /api/v1/security", auth(s.handleSecurity))
	mux.Handle("POST /api/v1/security/ufw", auth(s.handleSecurityUFW))
	mux.Handle("POST /api/v1/security/fail2ban", auth(s.handleSecurityFail2ban))

	// Health (M4, api_health.go).
	mux.Handle("GET /api/v1/incidents", auth(s.handleIncidents))
	mux.Handle("GET /api/v1/incidents/{id}", auth(s.handleIncident))
	mux.Handle("POST /api/v1/fixes", auth(s.handleFix))
	mux.Handle("POST /api/v1/files/accept", auth(s.handleAcceptFile))
	mux.Handle("POST /api/v1/health/run", auth(s.handleHealthRun))
	mux.Handle("GET /api/v1/sites/{domain}/traffic", auth(s.handleSiteTraffic))
	mux.Handle("GET /api/v1/sites/{domain}/log", auth(s.handleSiteLog))
	// M7: Plan chờ duyệt và cổng agent (plans_types.go, api_plans.go).
	mux.Handle("GET /api/v1/plans", auth(s.handlePlans))
	mux.Handle("GET /api/v1/plans/{id}", auth(s.handlePlan))
	mux.Handle("POST /api/v1/plans/{id}/approve", auth(s.handlePlanApprove))
	mux.Handle("POST /api/v1/plans/{id}/reject", auth(s.handlePlanReject))
	mux.Handle("GET /api/v1/agent", auth(s.handleAgent))
	// M6: thùng rác và hàng loạt (types_m6.go, api_trash.go).
	mux.Handle("POST /api/v1/sites/{domain}/restore", auth(s.handleSiteRestore))
	mux.Handle("GET /api/v1/trash", auth(s.handleTrash))
	mux.Handle("POST /api/v1/trash/settings", auth(s.handleTrashSettings))
	mux.Handle("DELETE /api/v1/trash/{domain}", auth(s.handleTrashPurge))
	mux.Handle("POST /api/v1/apply", auth(s.handleApply))
	mux.Handle("GET /api/v1/export", auth(s.handleExport))

	mux.HandleFunc("/api/", handleAPINotFound)
	mux.Handle("/", spa(dist))
	// secure thêm header bảo mật và chặn CSRF cho mọi request (auth.go).
	return s.secure(mux)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": buildinfo.Version,
		"commit":  buildinfo.Commit,
	})
}

func handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", i18n.T("Không có đường dẫn API này.", "No such API path."))
}

// writeError ghi lỗi theo dạng chung ErrorBody (mục 9).
func writeError(w http.ResponseWriter, status int, code string, msg i18n.Text) {
	writeJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: msg}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// spa phục vụ giao diện một trang: đường dẫn nào trùng file thật thì trả file đó,
// còn lại trả index.html để router phía trình duyệt tự xử lý.
func spa(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "Giao diện chưa được build. Chạy: make web", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
