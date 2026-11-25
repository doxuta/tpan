package server

// POST /api/v1/sites/{domain}/owner (admin): đổi chủ một site.
// Thân SiteOwnerInput. ?dry_run=1 trả bản xem trước và không ghi gì.
// Chạy thật: 202 {job_id} (job ghi chủ mới; site trong lồng mà gói chủ mới khác
// trần thì cùng job đó ghi lại trần). Mã từ chối: site_not_found, site_trashed, site_suspended,
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
	s.planJobKick(w, r, jobOpts{action: action, target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return plannedOf(s.apiCages().PrepareSiteOwner(ctx, s.deps.Store, d, to))
	})
}
