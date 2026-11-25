package server

// POST /api/v1/sites/{domain}/owner (admin): đổi chủ một site.
// Thân SiteOwnerInput. ?dry_run=1 trả bản xem trước và không ghi gì.
// Site ngoài lồng (hay gói chủ mới cùng trần): 200 SiteOwnerResult, chỉ ghi
// bảng. Site trong lồng mà trần đổi: 202 {job_id} (job ghi lại trần, chủ đổi
// trước job). Mã từ chối: site_not_found, site_trashed, site_suspended,
// owner_same, user_not_found, user_suspended, quota_sites, quota_disk.

import (
	"context"
	"net/http"
	"strings"

	"github.com/doxuta/tpan/internal/validate"
)

// SiteOwnerInput là thân của POST /sites/{domain}/owner.
type SiteOwnerInput struct {
	Owner string `json:"owner"`
}

// SiteOwnerResult là câu trả lời khi đổi chủ không cần job.
type SiteOwnerResult struct {
	Domain string `json:"domain"`
	From   string `json:"from"`
	To     string `json:"to"`
}

func (s *Server) handleSiteOwner(w http.ResponseWriter, r *http.Request) {
	const action = "site.owner"
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	var body SiteOwnerInput
	if !apiReadJSON(w, r, &body) {
		return
	}
	to := strings.TrimSpace(body.Owner)
	if to == "" {
		apiFail(w, r, userNotFound(""))
		return
	}
	ch, err := s.apiCages().PrepareSiteOwner(r.Context(), s.deps.Store, d, to)
	if err != nil {
		s.m17Refused(w, r, action, d.String(), err)
		return
	}
	if ch.Plan != nil {
		s.planJobKick(w, r, jobOpts{action: action, target: d.String(), kick: d.String()}, func(context.Context) (apiPlan, error) { return ch.Plan, nil })
		return
	}
	if apiDryRun(r) {
		writeJSON(w, http.StatusOK, emptyPreview(action, d.String(), ch.Title()))
		return
	}
	if !s.auditDone(w, r, action, d.String(), ch.Apply(r.Context())) {
		return
	}
	writeJSON(w, http.StatusOK, SiteOwnerResult{Domain: d.String(), From: ch.From, To: ch.To})
}
