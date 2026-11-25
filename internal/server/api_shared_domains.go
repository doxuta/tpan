package server

// Tên miền dùng chung (core/shared_suffix.go), cho thẻ cùng tên ở màn Cài đặt:
//
//	GET    /api/v1/settings/shared-domains            admin → SharedDomains
//	POST   /api/v1/settings/shared-domains            admin  SharedDomainInput → SharedDomains
//	DELETE /api/v1/settings/shared-domains/{domain}   admin → SharedDomains
//
// Chỉ ghi một dòng settings, không đụng máy, nên không có job.

import (
	"net/http"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/validate"
)

// SharedDomains là danh sách tên miền dùng chung, theo thứ tự chữ cái.
type SharedDomains struct {
	Domains []string `json:"domains"`
}

// SharedDomainInput là thân của POST.
type SharedDomainInput struct {
	Domain string `json:"domain"`
}

func sharedDomainsOf(ds []validate.Domain) SharedDomains {
	out := SharedDomains{Domains: make([]string, 0, len(ds))}
	for _, d := range ds {
		out.Domains = append(out.Domains, d.String())
	}
	return out
}

func (s *Server) handleSharedDomains(w http.ResponseWriter, r *http.Request) {
	ds, err := core.SharedSuffixes(r.Context(), s.deps.Store)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sharedDomainsOf(ds))
}

func (s *Server) handleSharedDomainAdd(w http.ResponseWriter, r *http.Request) {
	var body SharedDomainInput
	if !apiReadJSON(w, r, &body) {
		return
	}
	s.sharedDomainSet(w, r, body.Domain, true)
}

func (s *Server) handleSharedDomainRemove(w http.ResponseWriter, r *http.Request) {
	s.sharedDomainSet(w, r, r.PathValue("domain"), false)
}

func (s *Server) sharedDomainSet(w http.ResponseWriter, r *http.Request, raw string, on bool) {
	action := "shared-domain.rm"
	if on {
		action = "shared-domain.add"
	}
	d, err := validate.ParseDomain(raw)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	ds, err := core.SetSharedSuffix(r.Context(), s.deps.Store, d, on)
	if !s.auditDone(w, r, action, d.String(), err) {
		return
	}
	writeJSON(w, http.StatusOK, sharedDomainsOf(ds))
}
