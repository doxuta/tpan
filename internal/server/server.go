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
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
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
		// ctx riêng: Run có thể trả về khi ctx ngoài còn sống (nghe cổng lỗi),
		// lúc đó vẫn phải dừng được bộ hẹn giờ.
		sctx, cancel := context.WithCancel(ctx)
		s.sched = &health.Scheduler{
			Runner: s.healthRunner(), Busy: s.healthBusy,
			// M13: đo dung lượng của từng user cùng vòng health, tối đa mỗi giờ
			// một lần (diskTimer.after).
			OnReport: func(health.Report) { s.disk.after(sctx, s) },
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.sched.Run(sctx)
		}()
		// Bộ hẹn giờ dọn thùng rác (M6, mục 6.4) cũng sống cùng serve.
		trashDone := make(chan struct{})
		go func() {
			defer close(trashDone)
			s.newTrashTimer().Run(sctx)
		}()
		// M18: bộ đẩy nền lên kho S3 (pusher.go), tắt ở --demo. Goroutine tải
		// về của POST /backups/{id}/pull dừng theo sctx (s.bg).
		s.bg = sctx
		pushDone := s.startPusher(sctx, syncFunc(func(ctx context.Context, opt core.SyncOptions, onEvent func(engine.Event)) (core.SyncResult, error) {
			return s.apiRemote().Sync(ctx, opt, onEvent)
		}))
		// Dừng rồi chờ vòng kiểm đang dở xong hẳn (ctx đã huỷ nên nó dừng
		// nhanh), vòng dọn thùng rác đang dở (nó không dừng giữa chừng, xem
		// trash_timer.go) và lượt đẩy đang dở trước khi trả về: người gọi đóng
		// store ngay sau Run.
		defer func() { cancel(); <-done; <-trashDone; <-pushDone }()
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
	// phpNotes đệm ghi chú của GET /api/v1/php (api_m12.go). Zero value dùng được.
	phpNotes phpNotes
	// disk nhớ lần đo dung lượng gần nhất (api_m13.go). Zero value dùng được.
	disk diskTimer
	// pusher là bộ đẩy nền lên kho S3 (M18, pusher.go). Zero value dùng được:
	// handler đưa id vào hàng chờ; chỉ Run (không --demo) mới chạy nó.
	pusher pusher
	// bg sống tới khi server dừng (Run gán): goroutine tải về của
	// POST /backups/{id}/pull dừng theo nó. nil (test): context.Background.
	bg context.Context
	// newS3Store dựng kho từ cấu hình (core.Remote.NewStore); nil là client
	// thật. Chỉ test đặt (đưa *s3.Mem vào).
	newS3Store func(s3.Config) (steps.ObjectStore, error)
	// remoteLock là khoá lượt đẩy; nil là system.FileLock trên
	// system.DefaultRemoteLockPath. Chỉ test đặt (system.MemLock): /run/tpan
	// không ghi được trên máy dev.
	remoteLock system.Locker
}

func (s *Server) bgCtx() context.Context {
	if s.bg != nil {
		return s.bg
	}
	return context.Background()
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
	s.auditAs(r.Context(), clientIP(r), action, target, result, jobID)
}

// auditAs ghi một dòng nhật ký với ip cho sẵn: dùng cho việc tpan serve tự
// làm, không có request nào (bộ hẹn giờ dọn thùng rác ghi ip là auditIPTimer).
func (s *Server) auditAs(ctx context.Context, ip, action, target, result, jobID string) {
	if s.deps.Store == nil {
		return
	}
	// Actor (M13): guard gắn người thật đang thao tác vào ctx (engine.WithActor);
	// bộ hẹn giờ không có ai nên rỗng.
	err := s.deps.Store.AddAudit(ctx, store.AuditEntry{
		Time: s.now(), IP: ip, Action: action, Target: target, Result: result, JobID: jobID,
		Actor: engine.ActorFrom(ctx),
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
// mà không cần mở cổng thật. Bảng đường dẫn và luật: routes.go; ý nghĩa:
// types.go, types_m5.go...
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
	// Mọi đường dẫn và luật của nó ở routes.go; mount panic với route quên luật.
	s.mount(mux, s.routes(dist))
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
