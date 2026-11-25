package server

import (
	"net/http"

	"github.com/doxuta/tpan/internal/core"
)

// handleMachineIPs: GET /api/v1/machine/ips (admin). Chỉ đọc: IP trên card
// mạng, panel nghe ở đâu, DNS của từng site trỏ về IP nào (core/ips.go).
func (s *Server) handleMachineIPs(w http.ResponseWriter, r *http.Request) {
	sites, err := s.deps.Store.Sites(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	domains := make([]string, 0, len(sites))
	for _, st := range sites {
		domains = append(domains, st.Domain.String())
	}
	out, err := core.ReadMachineIPs(r.Context(), s.deps.Sys, s.deps.Hostname, domains)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
