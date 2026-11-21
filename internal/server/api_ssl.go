package server

// API của SSL (mục 6.3, 9 của docs/KE-HOACH.md; hợp đồng ở types_m5.go):
// màn SSL, cấu hình email và staging, xin chứng chỉ cho một site. Job chạy ở
// nền giống tạo và xoá site (api_job.go).

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

// apiSSL dựng ca sử dụng SSL. System chờ khoá có hạn như apiSites.
func (s *Server) apiSSL() *core.SSL {
	st := s.deps.Store
	return &core.SSL{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Journal: st,
	}
}

// apiPlan là phần chung của core.SSLIssue và core.SecurityPlan: một Plan đã
// qua điều kiện đầu, xem trước được hoặc chạy được đúng một lần.
type apiPlan interface {
	Preview(ctx context.Context) (engine.Preview, error)
	Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error)
}

// planJob là khuôn của mọi POST chạy một Plan của core: ?dry_run=1 thì trả
// bản xem trước; không thì giành chỗ chạy job, chuẩn bị (lời từ chối về thẳng
// người gọi kèm checks), rồi chạy ở nền và trả 202. Giống handleSiteDelete,
// tách ra vì SSL, hai nút của trang bảo mật, khôi phục và xoá hẳn site trong
// thùng rác (M6) cùng một hình dạng.
//
// kick "": job SSL đổi vhost (chặng TLS và web), job bảo mật đổi ufw,
// fail2ban (phép kiểm của máy); kiểm nhẹ lại mọi thứ sau job (M4).
func (s *Server) planJob(w http.ResponseWriter, r *http.Request, action, target string, prepare func(context.Context) (apiPlan, error)) {
	s.planJobKick(w, r, jobOpts{action: action, target: target}, prepare)
}

// planJobKick là planJob với kick (và onStart) tuỳ chọn: khôi phục xin kiểm
// lại đúng site vừa sống lại.
func (s *Server) planJobKick(w http.ResponseWriter, r *http.Request, o jobOpts, prepare func(context.Context) (apiPlan, error)) {
	action, target := o.action, o.target
	if apiDryRun(r) {
		p, err := prepare(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		pv, err := p.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		s.writePreview(w, r, pv)
		return
	}

	if !s.api.claim() {
		s.jobBusy(w, r, action, target)
		return
	}
	p, err := prepare(r.Context())
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, target, err)
		return
	}
	s.jobStart(w, r, o, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		rep, err := p.Execute(ctx, onEvent)
		return jobResult{status: rep.Job.Status}, err
	})
}

// handleSSL trả nội dung màn SSL: cấu hình và mọi site active kèm phép kiểm
// chứng chỉ. Không đệm: TLSCert chỉ đọc một file trên đĩa, nhanh.
func (s *Server) handleSSL(w http.ResponseWriter, r *http.Request) {
	ssl := s.apiSSL()
	cfg, err := ssl.Config(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	rows, err := ssl.List(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	// M13: core.SSL.List không lọc theo chủ; lọc theo tập site của người gọi.
	mine, err := s.ownedDomains(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if mine != nil {
		rows = slices.DeleteFunc(rows, func(x core.SSLSiteRow) bool { return !mine[x.Domain.String()] })
	}
	writeJSON(w, http.StatusOK, SSLOverview{Config: cfg, Sites: apiNonNil(rows)})
}

// handleSSLSettings đổi email và staging. Không phải job: chỉ ghi bảng
// settings, không đụng máy, nên không giành chỗ chạy job.
func (s *Server) handleSSLSettings(w http.ResponseWriter, r *http.Request) {
	const action = "ssl.settings"
	var body SSLSettings
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	// Trường lạ là lỗi: nhất là "server", địa chỉ ACME tuỳ chỉnh chỉ đặt được
	// ở CLI (types_m5.go). Bỏ qua im lặng thì người gửi tưởng đã đổi.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", i18n.T(
			"Thân request không phải JSON hợp lệ: "+err.Error(), "The request body is not valid JSON: "+err.Error()))
		return
	}
	in := core.SSLSettingsInput{Staging: body.Staging}
	if body.Email != nil {
		e, err := validate.ParseEmail(*body.Email)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		in.Email = &e
	}
	cfg, err := s.apiSSL().SetConfig(r.Context(), in)
	if err != nil {
		_, eb := apiErrorStatus(err)
		s.audit(r, action, "", eb.Error.Code, "")
		apiFail(w, r, err)
		return
	}
	s.audit(r, action, "", "ok", "")
	writeJSON(w, http.StatusOK, cfg)
}

// handleSiteSSL xin (hoặc chạy lại) chứng chỉ cho một site. Job xong thì
// jobStart bỏ đệm phép kiểm của domain này: site giờ kiểm bằng bộ có SSL.
func (s *Server) handleSiteSSL(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	ssl := s.apiSSL()
	s.planJob(w, r, "site.ssl", d.String(), func(ctx context.Context) (apiPlan, error) {
		return ssl.PrepareIssue(ctx, d)
	})
}
