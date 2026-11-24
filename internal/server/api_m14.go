package server

// API của M14 (mục 6.13 của docs/KE-HOACH.md; hợp đồng ở types_m14.go): đường
// đi của request, chế độ .htaccess của site, ma trận cô lập và Plan "Áp bản
// vá cô lập".

import (
	"context"
	"net/http"
	"strings"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// handleSiteRoute lần đường đi của một request tới site (core.RouteExplain).
// Chỉ đọc nhưng chạy lệnh trên máy (nginx -T, curl), và không đệm: người bấm
// nút này đang hỏi "bây giờ request đi đâu", câu trả lời cũ là câu trả lời sai.
//
// M19: ?host=<tên> hỏi về một tên phụ của site. Tên phải thuộc {domain}; tên
// của site khác hay không có đều là 404 name_unknown (core.RouteExplainName),
// để người chỉ có quyền trên {domain} không dò được tên của người khác.
func (s *Server) handleSiteRoute(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	var tr core.RouteTrace
	if q := r.URL.Query().Get("host"); q != "" && q != d.String() {
		var host validate.Domain
		if host, err = validate.ParseDomain(q); err == nil {
			tr, err = s.apiSites().RouteExplainName(r.Context(), d, host)
		}
	} else {
		tr, err = s.apiSites().RouteExplain(r.Context(), d)
	}
	if err != nil {
		apiFail(w, r, err)
		return
	}
	tr.Hops = apiNonNil(tr.Hops)
	for i := range tr.Hops {
		tr.Hops[i].Evidence = apiNonNil(tr.Hops[i].Evidence)
	}
	writeJSON(w, http.StatusOK, tr)
}

// handleSiteHtaccess đổi chế độ .htaccess của một site (Plan site.htaccess).
//
// Luật của route là owner:site, nhưng riêng "day-du" chỉ admin đặt được: ở
// chế độ đó site ghi được cấu hình Apache và, nếu bị chiếm, chạy được mã dưới
// user của site khác. Chủ của site không được tự mở lại lỗ đó cho cả máy, nên
// luật nằm ở đây (core không biết vai, quyết định 10 của đợt 0, mục 6.13 của docs/KE-HOACH.md, "Đã chốt khi làm M14").
func (s *Server) handleSiteHtaccess(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	var body HtaccessBody
	if !apiReadJSON(w, r, &body) {
		return
	}
	mode, err := site.ParseHtaccessMode(body.Mode)
	if err != nil {
		// Không in lại giá trị người gọi gửi: chỉ nêu ba giá trị nhận.
		writeError(w, http.StatusBadRequest, "htaccess_mode_invalid", i18n.T(
			"Chế độ .htaccess phải là một trong: an-toan, tat, day-du.",
			"The .htaccess mode must be one of: an-toan, tat, day-du."))
		return
	}
	if mode == site.HtaccessFull && !who(r).IsAdmin() {
		s.audit(r, "site.htaccess", d.String(), "forbidden", "")
		writeError(w, http.StatusForbidden, "forbidden", i18n.T(
			"Chỉ quản trị viên đặt được chế độ day-du: ở chế độ này site ghi được cấu hình Apache và, nếu bị chiếm, có thể chạy mã dưới user của site khác.",
			"Only an administrator can set the day-du mode: in this mode the site can write Apache configuration and, if compromised, may run code as another site's user."))
		return
	}
	sites := s.apiSites()
	s.planJob(w, r, "site.htaccess", d.String(), func(ctx context.Context) (apiPlan, error) {
		p, err := sites.PrepareHtaccess(ctx, d, mode)
		if err != nil {
			return nil, err // không trả *Planned nil bọc trong interface khác nil
		}
		return p, nil
	})
}

// Loại phép kiểm của khối "Cô lập giữa các site". m14HardenFixes là các loại
// mà Plan "Áp bản vá cô lập" sửa được: một kết quả không đạt trong số này thì
// giao diện hiện nút.
var (
	m14MachineKinds = []string{"php.opcache", "php.protectproc", "web.server_name.unique", "web.default.rejects"}
	m14SiteKinds    = []string{"log.perm", "web.hardened"}
	m14HardenFixes  = []string{"php.opcache", "php.protectproc", "log.perm", "web.hardened"}
)

// m14Kind là phần "<loại>" của ID phép kiểm "<loại>:<đối tượng>".
func m14Kind(id string) string {
	kind, _, _ := strings.Cut(id, ":")
	return kind
}

func m14In(kinds []string, id string) bool {
	k := m14Kind(id)
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// isolation dựng khối "Cô lập giữa các site" từ kết quả health đã lưu (bảng
// check_results). Không chạy lại phép kiểm nào: ma trận của n site là n×(n-1)
// lần chạy lệnh dưới user của site, quá nặng cho một lần mở màn Bảo mật; nút
// "Kiểm lại" (POST /health/run) mới làm việc đó. Ô chưa có kết quả thì Result nil.
func (s *Server) isolation(ctx context.Context) (core.Isolation, error) {
	cfg, err := s.apiStack().Config(ctx)
	if err != nil {
		return core.Isolation{}, err
	}
	all, err := s.deps.Store.Sites(ctx) // đã xếp theo tên
	if err != nil {
		return core.Isolation{}, err
	}
	stored, err := s.deps.Store.CheckResults(ctx)
	if err != nil {
		return core.Isolation{}, err
	}
	byID := make(map[string]*checks.Result, len(stored))
	iso := core.Isolation{
		Web: cfg.Web, Sites: []string{}, Cells: []core.IsolationCell{},
		Machine: []checks.Result{}, PerSite: []checks.Result{}, Htaccess: []core.HtaccessRow{},
	}
	for i := range stored {
		res := &stored[i].Result
		res.Evidence = apiNonNil(res.Evidence)
		byID[res.ID] = res
		switch {
		case m14In(m14MachineKinds, res.ID):
			iso.Machine = append(iso.Machine, *res)
		case m14In(m14SiteKinds, res.ID):
			iso.PerSite = append(iso.PerSite, *res)
		}
		if res.Status != checks.Pass && m14In(m14HardenFixes, res.ID) {
			iso.NeedsHarden = true
		}
	}
	for _, st := range all {
		if st.Status != site.Active {
			continue
		}
		dom := st.Domain.String()
		iso.Sites = append(iso.Sites, dom)
		row := core.HtaccessRow{Domain: dom, Mode: st.Htaccess.OrDefault()}
		if cfg.Web == core.Apache {
			row.Result = byID["web.htaccess:"+dom]
		}
		iso.Htaccess = append(iso.Htaccess, row)
		// Site còn ở day-du là site Plan chưa chuyển (hoặc đã quét và giữ lại):
		// nút vẫn hiện để người vận hành thấy còn việc, kể cả trên nginx (cột
		// này có hiệu lực ngay khi máy chuyển sang Apache).
		if row.Mode == site.HtaccessFull {
			iso.NeedsHarden = true
		}
	}
	for _, from := range iso.Sites {
		for _, to := range iso.Sites {
			if from != to {
				iso.Cells = append(iso.Cells, core.IsolationCell{From: from, To: to, Result: byID["isolation:"+from+":"+to]})
			}
		}
	}
	return iso, nil
}

func (s *Server) handleIsolation(w http.ResponseWriter, r *http.Request) {
	iso, err := s.isolation(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, iso)
}

// handleSecurityHarden là Plan "Áp bản vá cô lập". Bản xem trước kèm hai danh
// sách của core.Harden: site sẽ chuyển sang an-toan, và site giữ day-du cùng
// từng dòng .htaccess vướng, để người bấm biết trước site nào còn cảnh báo.
func (s *Server) handleSecurityHarden(w http.ResponseWriter, r *http.Request) {
	sec := s.apiSecurity()
	if apiDryRun(r) {
		h, err := sec.PrepareHarden(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		pv, err := h.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		// Route chỉ cho admin nên lọc không đổi gì; vẫn đi qua để luật "mọi
		// bản xem trước qua filterPreview" không có ngoại lệ (B12).
		if err := s.filterPreview(r, &pv); err != nil {
			apiFail(w, r, err)
			return
		}
		kept := apiNonNil(h.Kept)
		for i := range kept {
			kept[i].Findings = apiNonNil(kept[i].Findings)
		}
		writeJSON(w, http.StatusOK, HardenPreview{Preview: pv, Switched: apiNonNil(h.Switched), Kept: kept, Skipped: apiNonNil(h.Skipped)})
		return
	}
	s.planJob(w, r, "security.harden", "isolation", func(ctx context.Context) (apiPlan, error) {
		h, err := sec.PrepareHarden(ctx)
		if err != nil {
			return nil, err
		}
		return h, nil
	})
}
