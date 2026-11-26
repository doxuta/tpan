package server

// Handler của M17 (hợp đồng ở types_m17.go, luật route ở routes.go). Mọi POST
// đổi máy đi qua planJobKick (api_ssl.go) hoặc jobStart (api_job.go): ?dry_run=1
// trả bản xem trước, không thì chạy job ở nền và trả 202; lời từ chối của core
// là 409 kèm checks; có audit.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

// m17Refused ghi nhật ký lời từ chối (trừ ?dry_run=1: xem trước không để lại
// dấu) rồi trả lỗi như apiFail (mã HTTP của các mã M17 nằm ở apiErrorStatus).
// Dùng cho hai handler của M13 đã đổi.
func (s *Server) m17Refused(w http.ResponseWriter, r *http.Request, action, target string, err error) {
	if !apiDryRun(r) {
		_, eb := apiErrorStatus(err)
		s.audit(r, action, target, eb.Error.Code, "")
	}
	apiFail(w, r, err)
}

// plannedOf đưa *core.Planned vào apiPlan. Con trỏ nil mà gói vào interface
// thì interface khác nil và Preview panic: nil không có lỗi là lỗi lập trình
// của ca sử dụng, nên báo lỗi chứ không đoán.
func plannedOf(p *core.Planned, err error) (apiPlan, error) {
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("ca sử dụng trả Plan rỗng mà không có lỗi")
	}
	return p, nil
}

// emptyPreview là bản xem trước của thao tác chỉ ghi một dòng bảng, không có
// bước nào chạm máy (sửa gói hay đổi gói không có site trong lồng): ?dry_run=1
// vẫn trả đúng hình Preview và không ghi gì.
func emptyPreview(kind, target string, title i18n.Text) engine.Preview {
	return engine.Preview{Kind: kind, Target: target, Title: title, Steps: []engine.StepPreview{}}
}

// ---------------------------------------------------------------- lồng

// handleCage trả trạng thái lồng của mọi site active (admin).
func (s *Server) handleCage(w http.ResponseWriter, r *http.Request) {
	rows, err := s.apiCages().Status(r.Context(), validate.Domain{})
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(rows))
}

// handleSiteCage đưa một site vào lồng hay ra khỏi lồng. "enabled" bắt buộc:
// thân {} mà ngầm hiểu là "ra khỏi lồng" thì một request thiếu trường hạ hàng
// rào của site.
func (s *Server) handleSiteCage(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"` // cùng JSON với CageBody
	}
	if !apiReadJSON(w, r, &body) {
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "bad_request", i18n.T(
			`Thiếu "enabled" (true: vào lồng, false: ra khỏi lồng).`,
			`Missing "enabled" (true: enter the cage, false: leave it).`))
		return
	}
	cages := s.apiCages()
	action, prepare := "cage.disable", cages.PrepareDisable
	if *body.Enabled {
		action, prepare = "cage.enable", cages.PrepareEnable
	}
	s.planJobKick(w, r, jobOpts{action: action, target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return plannedOf(prepare(ctx, d))
	})
}

// ---------------------------------------------------------------- số liệu

// handleUsage trả số liệu đọc ngay từ cgroup của các site trong lồng. User
// thường chỉ thấy site của mình (scopeOf).
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	list, err := s.apiUsages().List(r.Context(), scopeOf(r))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(list))
}

// handleSiteUsage trả số đọc ngay cộng lịch sử theo ?range= (day mặc định).
// Site ngoài lồng: 404 usage_not_caged. Quyền xem site đã qua guard (owner:site).
func (s *Server) handleSiteUsage(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "day"
	}
	u := s.apiUsages()
	cur, err := u.Site(r.Context(), d)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	h, err := u.History(r.Context(), d, rng)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	h.Points = apiNonNil(h.Points)
	writeJSON(w, http.StatusOK, SiteUsageDetail{SiteUsage: cur, History: h})
}

// handleUsageSuggestions trả gợi ý trần cho từng site và từng gói (admin).
func (s *Server) handleUsageSuggestions(w http.ResponseWriter, r *http.Request) {
	rep, err := s.apiUsages().Suggestions(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	rep.Sites, rep.Packages = apiNonNil(rep.Sites), apiNonNil(rep.Packages)
	for i := range rep.Sites {
		rep.Sites[i].Reasons = apiNonNil(rep.Sites[i].Reasons)
	}
	for i := range rep.Packages {
		rep.Packages[i].Sites = apiNonNil(rep.Packages[i].Sites)
	}
	writeJSON(w, http.StatusOK, rep)
}

// ---------------------------------------------------------------- thử tấn công

// handleProbe trả lượt gần nhất đã xong và các lượt chưa dọn.
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	last, err := s.apiProbe().Last(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	pending, err := s.deps.Store.PendingProbeRuns(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ProbeStatus{Last: last, Pending: apiNonNil(pending)})
}

// handleProbeRun chạy một lượt thử. ProbeJob.Execute trả báo cáo riêng nên nó
// không phải apiPlan: handler dựng job như handleSiteCreate. Audit ghi id của
// lượt thử, biết sau khi Prepare; lời từ chối hay bận trước đó ghi target rỗng.
func (s *Server) handleProbeRun(w http.ResponseWriter, r *http.Request) {
	const action = "isolation.probe"
	var body ProbeBody
	if !apiReadOptionalJSON(w, r, &body) {
		return
	}
	// Chặn ở biên: Only đi vào tên sổ và quyết định bước nào chạy.
	switch body.Only {
	case "", "ngoai", "trong":
	default:
		apiFail(w, r, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
			`"only" phải là rỗng, "ngoai" hoặc "trong".`, `"only" must be empty, "ngoai" or "trong".`)})
		return
	}
	probe, opts := s.apiProbe(), core.ProbeOptions{Only: body.Only}

	if apiDryRun(r) {
		job, err := probe.Prepare(r.Context(), opts)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		pv, err := job.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		// Route chỉ admin (filterPreview không bỏ gì), nhưng mọi bản xem trước
		// đều đi qua bộ lọc của M28: đổi luật route sau này không lộ file.
		if err := s.filterPreview(r, &pv); err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, ProbePreview{Preview: pv, Run: job.Run})
		return
	}

	if !s.api.claim() {
		s.jobBusy(w, r, action, "")
		return
	}
	job, err := probe.Prepare(r.Context(), opts)
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, "", err)
		return
	}
	// kick "": hai site thử không có dòng trong bảng sites; health chỉ cần
	// thấy lượt thử đã dọn (probe.leftover).
	s.jobStart(w, r, jobOpts{action: action, target: job.Run.ID}, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		_, rep, err := job.Execute(ctx, onEvent)
		return jobResult{status: rep.Job.Status}, err
	})
}

// handleProbeCleanup dọn các lượt thử còn sót. Không có gì để dọn: 409
// nothing_to_do (kể cả dry_run). Audit ghi id các lượt, cách nhau dấu phẩy.
func (s *Server) handleProbeCleanup(w http.ResponseWriter, r *http.Request) {
	pending, err := s.deps.Store.PendingProbeRuns(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	nothing := &core.RefusedError{Code: "nothing_to_do", Message: i18n.T(
		"Không có lượt thử nào chưa dọn.", "There is no probe run left to clean up.")}
	if len(pending) == 0 {
		s.audit(r, "isolation.probe.cleanup", "", nothing.Code, "")
		apiFail(w, r, nothing)
		return
	}
	ids := make([]string, len(pending))
	for i, p := range pending {
		ids[i] = p.ID
	}
	probe := s.apiProbe()
	s.planJobKick(w, r, jobOpts{action: "isolation.probe.cleanup", target: strings.Join(ids, ",")}, func(ctx context.Context) (apiPlan, error) {
		p, err := probe.PrepareCleanup(ctx)
		if err == nil && p == nil {
			return nil, nothing // người khác dọn xong giữa hai lần đọc
		}
		return plannedOf(p, err)
	})
}

// ---------------------------------------------------------------- gói

// over dựng gói đầy đủ từ gói đang có cur: trường nào có mặt trong thân thì
// đổi, vắng mặt là giữ nguyên. Nhờ vậy giao diện cũ không biết tới lồng mà sửa
// gói thì không xoá trần, và lệnh gọi API chỉ sửa một trần thì không gỡ hạn mức
// site, DB, đĩa, backup (muốn bỏ một hạn mức thì gửi rõ số 0).
func (e PackageEdit) over(cur account.Package) account.Package {
	if e.Cage != nil {
		cur.Cage = *e.Cage
	}
	for _, f := range []struct {
		in  *int
		out *int
	}{
		{e.MaxSites, &cur.MaxSites}, {e.MaxDBs, &cur.MaxDBs}, {e.DiskMB, &cur.DiskMB}, {e.MaxBackups, &cur.MaxBackups},
		{e.CPUPercent, &cur.CPUPercent}, {e.RAMMB, &cur.RAMMB}, {e.Tasks, &cur.Tasks},
		{e.IOReadMB, &cur.IOReadMB}, {e.IOWriteMB, &cur.IOWriteMB}, {e.PHPWorkers, &cur.PHPWorkers},
	} {
		if f.in != nil {
			*f.out = *f.in
		}
	}
	return cur
}
