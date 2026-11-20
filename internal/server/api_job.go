package server

// Tạo site và đưa site vào thùng rác qua API: chạy job ở nền, trả 202 ngay
// khi có job id. Các job khác (SSL, sửa tự động, thùng rác, hàng loạt) dùng
// chung jobStart ở đây.
//
// Vòng đời của một job:
//  1. handler kiểm đầu vào (validate), giành chỗ "đang có job" (busy; có rồi
//     thì 409 job_running), rồi gọi core.Prepare* ngay trong request để lời
//     từ chối (409 kèm checks) về thẳng người gọi.
//  2. jobStart chạy Execute trong goroutine với context riêng: người dùng đóng
//     tab không được làm job dừng giữa chừng (job dừng giữa chừng thì phải
//     hoàn tác, mà hoàn tác bằng một context đã huỷ thì lỗi nốt).
//  3. Sự kiện đầu tiên (job.start) mang job id: handler trả 202 JobStarted.
//     Mọi sự kiện được phát qua bộ phát của job (sse.go) cho người đang nghe.
//  4. Goroutine xong: cất mật khẩu quản trị (nếu có), nhả busy, ghi nhật ký,
//     rồi xin health kiểm lại thứ job vừa chạm vào (Scheduler.Kick). Kick đi
//     SAU khi nhả busy: vòng kiểm thấy còn job thì bỏ qua.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// errJobBusy: khoá toàn cục đang bị tiến trình khác giữ quá lockWait.
var errJobBusy = errors.New("đang có job khác thay đổi máy")

// jobLockSystem bọc System để Lock chờ có hạn. Engine chờ khoá không giới hạn
// (đúng cho CLI); với API, chờ không giới hạn nghĩa là request treo tới khi job
// của CLI xong mà không có job id nào để trả. Context có hạn chỉ dùng lúc chờ:
// đã lấy được khoá thì giữ tới khi engine nhả, không phụ thuộc context nữa
// (cả LocalSystem lẫn FakeSystem đều vậy).
type jobLockSystem struct {
	system.System
	wait time.Duration
}

func (j jobLockSystem) Lock(ctx context.Context) (func(), error) {
	wctx, cancel := context.WithTimeoutCause(ctx, j.wait, errJobBusy)
	defer cancel()
	unlock, err := j.System.Lock(wctx)
	if err != nil && errors.Is(context.Cause(wctx), errJobBusy) {
		return nil, fmt.Errorf("%w: %w", errJobBusy, err)
	}
	return unlock, err
}

// jobRun là một job đang chạy trong tiến trình này.
type jobRun struct {
	id   string
	feed *sseFeed
	// done đóng khi goroutine của job xong hẳn (mật khẩu đã cất, busy đã nhả).
	done chan struct{}
}

// jobResult là phần kết quả mà jobStart cần từ core.
type jobResult struct {
	status engine.JobStatus
	secret *AdminSecret
	// secrets: mật khẩu của các site vừa tạo trong job hàng loạt (POST /apply).
	secrets []AdminSecret
	// dbSecret: mật khẩu DB rời vừa tạo hoặc vừa đổi (M13); chỉ đặt khi job thành công.
	dbSecret *DBSecret
}

type jobFunc func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error)

// jobOpts là phần khác nhau giữa các loại job.
type jobOpts struct {
	// action và target ghi vào nhật ký (audit).
	action, target string
	// kick là domain xin health kiểm lại khi job xong; "" là kiểm nhẹ mọi thứ.
	kick string
	// onStart (có thể nil) chạy trong request ngay khi có job id, trước khi trả
	// 202. Kick của job chờ nó xong: sửa tự động gắn job vào sự cố ở đây, và
	// vòng kiểm sau job không được đóng sự cố trước khi job kịp gắn vào.
	onStart func(ctx context.Context, id string)
}

// claim giành chỗ chạy job; false nếu tiến trình này đang có job.
func (a *apiState) claim() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return false
	}
	a.busy = true
	return true
}

func (a *apiState) release() {
	a.mu.Lock()
	a.busy = false
	a.mu.Unlock()
}

func (a *apiState) run(id string) *jobRun {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs[id]
}

// heldSecret là mật khẩu quản trị đang chờ người dùng lấy: one cho job
// site.add (JobDetail.Secret), many cho job hàng loạt (JobDetail.Secrets, một
// dòng mỗi site vừa cài WordPress). Hai trường riêng để giao diện cũ của
// site.add vẫn đọc đúng chỗ cũ.
type heldSecret struct {
	one  *AdminSecret
	many []AdminSecret
	db   *DBSecret // M13: JobDetail.DBSecret
}

// takeSecret trả mật khẩu của job rồi xoá khỏi bộ nhớ: lần sau không còn.
func (a *apiState) takeSecret(id string) heldSecret {
	a.mu.Lock()
	defer a.mu.Unlock()
	sec := a.secrets[id]
	delete(a.secrets, id)
	return sec
}

// jobBusy trả 409 job_running.
func (s *Server) jobBusy(w http.ResponseWriter, r *http.Request, action, target string) {
	s.audit(r, action, target, "job_running", "")
	writeError(w, http.StatusConflict, "job_running", i18n.T(
		"Đang có một thao tác khác thay đổi máy. Chờ nó xong rồi thử lại (xem ở Lịch sử).",
		"Another operation is changing the server. Wait for it to finish, then try again (see History)."))
}

// jobRefused ghi nhật ký lời từ chối rồi trả lỗi.
func (s *Server) jobRefused(w http.ResponseWriter, r *http.Request, action, target string, err error) {
	_, body := apiErrorStatus(err)
	s.audit(r, action, target, body.Error.Code, "")
	apiFail(w, r, err)
}

// jobStart chạy fn ở nền và trả 202 khi có job id. Người gọi đã claim; jobStart
// nhả busy khi job xong (hoặc không bắt đầu được).
func (s *Server) jobStart(w http.ResponseWriter, r *http.Request, o jobOpts, fn jobFunc) {
	a := s.api
	action, target := o.action, o.target
	// Nhật ký ghi lúc job xong, khi request đã kết thúc và r.Context() đã huỷ.
	ar := r.WithContext(context.WithoutCancel(r.Context()))
	run := &jobRun{done: make(chan struct{})}
	started := make(chan string, 1)
	notStarted := make(chan error, 1)
	// linked đóng khi handler đã chạy xong onStart (xem jobOpts.onStart).
	linked := make(chan struct{})

	// onEvent chạy trong engine, dưới khoá của engine, theo thứ tự Seq: phải
	// nhanh và không bao giờ chặn (bộ phát không chờ người nghe).
	onEvent := func(ev engine.Event) {
		if run.feed == nil {
			run.id, run.feed = ev.JobID, sseNewFeed()
			a.mu.Lock()
			a.runs[ev.JobID] = run
			a.mu.Unlock()
			started <- ev.JobID
		}
		run.feed.publish(ev)
	}

	a.wg.Go(func() {
		// ctx của job giữ giá trị của request (người chạy, engine.WithActor của
		// guard: job ghi cột actor) nhưng không bị huỷ theo request.
		res, err := fn(ar.Context(), onEvent)
		a.mu.Lock()
		if (res.secret != nil || len(res.secrets) > 0 || res.dbSecret != nil) && run.id != "" {
			a.secrets[run.id] = heldSecret{one: res.secret, many: res.secrets, db: res.dbSecret}
		}
		delete(a.runs, run.id)
		a.busy = false
		a.mu.Unlock()
		if run.feed != nil {
			run.feed.close()
		}
		close(run.done)

		if run.id == "" {
			if err == nil {
				err = errors.New("job không bắt đầu được (không có sự kiện job.start)")
			}
			notStarted <- err
			return
		}
		result := "ok"
		if res.status != engine.JobSucceeded {
			result = string(res.status)
		}
		if err != nil {
			slog.Error("job lỗi", "action", action, "target", target, "job", run.id, "err", err)
			// "": job đã bắt đầu mà không có kết quả (POST /apply khi job
			// hàng loạt không chạy xong).
			if result == "ok" || result == "" {
				result = "error"
			}
		}
		s.audit(ar, action, target, result, run.id)
		<-linked
		s.kick(o.kick)
	})

	select {
	case id := <-started:
		if o.onStart != nil {
			o.onStart(ar.Context(), id)
		}
		close(linked)
		writeJSON(w, http.StatusAccepted, JobStarted{JobID: id})
	case err := <-notStarted:
		if errors.Is(err, errJobBusy) {
			s.jobBusy(w, ar, action, target)
			return
		}
		s.jobRefused(w, ar, action, target, err)
	}
}

// ---------------------------------------------------------------- tạo site

func (s *Server) handleSiteCreate(w http.ResponseWriter, r *http.Request) {
	const action = "site.add"
	var body SiteCreate
	if !apiReadJSON(w, r, &body) {
		return
	}
	d, err := validate.ParseDomain(body.Domain)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	email, err := validate.ParseEmail(body.Email)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	owner, ok := s.siteCreateOwner(w, r, d, body.Owner)
	if !ok {
		return
	}
	in := core.SiteInput{Domain: d, Title: body.Title, Email: email, Owner: owner}
	sites := s.apiSites()

	if apiDryRun(r) {
		add, err := sites.PrepareAdd(r.Context(), in)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		p, err := add.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}

	if !s.api.claim() {
		s.jobBusy(w, r, action, d.String())
		return
	}
	add, err := sites.PrepareAdd(r.Context(), in)
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, d.String(), err)
		return
	}
	s.jobStart(w, r, jobOpts{action: action, target: d.String(), kick: d.String()}, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		res, err := add.Execute(ctx, onEvent)
		out := jobResult{status: res.Report.Job.Status}
		// Kể cả khi err khác nil (job xong mà không ghi được bảng sites), mật
		// khẩu vẫn phải tới người dùng: mất nó là mất tài khoản quản trị.
		if res.WPInstalled {
			out.secret = &AdminSecret{
				URL:      "http://" + d.String() + "/wp-admin/",
				User:     res.AdminUser,
				Password: res.AdminPassword.Reveal(),
			}
		}
		return out, err
	})
}

// ---------------------------------------------------------------- xoá site

// handleSiteDelete: từ M6, xoá một site là đưa nó vào thùng rác (mục 6.4): gỡ
// vhost và pool, khoá user DB và user Linux, chuyển thư mục vào
// site.TrashDir; dữ liệu còn nguyên tới hạn xoá hẳn. Vẫn gọi PrepareRemove
// (core giữ tên cũ). Xoá hẳn đi qua DELETE /trash/{domain} (api_trash.go).
func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) {
	const action = "site.rm"
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	sites := s.apiSites()

	if apiDryRun(r) {
		rm, err := sites.PrepareRemove(r.Context(), d)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		p, err := rm.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}

	if !s.api.claim() {
		s.jobBusy(w, r, action, d.String())
		return
	}
	rm, err := sites.PrepareRemove(r.Context(), d)
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, d.String(), err)
		return
	}
	// Kick "" chứ không phải domain: health không kiểm site trong thùng rác
	// (nó cố ý không còn được phục vụ, kiểm thì chỉ ra sự cố giả), nên kiểm theo
	// domain không còn gì để làm. Kiểm nhẹ mọi thứ thì thấy ngay web server và
	// PHP-FPM có còn ổn sau lần reload của job.
	s.jobStart(w, r, jobOpts{action: action, target: d.String()}, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		rep, err := rm.Execute(ctx, onEvent)
		return jobResult{status: rep.Job.Status}, err
	})
}
