package server

// POST /api/v1/sites/{domain}/wp-password (chủ site hoặc admin): đặt lại mật
// khẩu tài khoản quản trị WordPress của site. ?dry_run=1 trả bản xem trước.
// Chạy thật: 202 {job_id}; mật khẩu mới nằm ở JobDetail.Secret, đúng một lần
// (như lúc tạo site).

import (
	"context"
	"net/http"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/validate"
)

func (s *Server) handleWPPassword(w http.ResponseWriter, r *http.Request) {
	const action = "wp.password"
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	sites := s.apiSites()
	if apiDryRun(r) {
		p, err := sites.PrepareWPPassword(r.Context(), d)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		pv, err := p.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		s.writePreview(w, r, pv, d)
		return
	}
	if !s.api.claim() {
		s.jobBusy(w, r, action, d.String())
		return
	}
	p, err := sites.PrepareWPPassword(r.Context(), d)
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, d.String(), err)
		return
	}
	s.jobStart(w, r, jobOpts{action: action, target: d.String()}, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		rep, err := p.Execute(ctx, onEvent)
		out := jobResult{status: rep.Job.Status}
		// Bước đã chạy thì mật khẩu đã đổi: đưa tới người dùng kể cả khi job
		// báo lỗi sau đó, không thì họ mất đường vào.
		if p.Login() != "" {
			out.secret = &AdminSecret{URL: p.URL, User: p.Login(), Password: p.Password().Reveal()}
		}
		return out, err
	})
}
