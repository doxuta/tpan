package server

// Handler của M17 (types_m17.go). Hợp đồng đợt 0: route và luật đã chốt ở
// routes.go; thân handler do thợ đợt 3 viết (tạm trả 501).

import (
	"net/http"

	"github.com/doxuta/tpan/internal/i18n"
)

func notYetM17(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", i18n.T(
		"Chưa viết (M17).", "Not implemented yet (M17)."))
}

func (s *Server) handleCage(w http.ResponseWriter, r *http.Request)             { notYetM17(w, r) }
func (s *Server) handleSiteCage(w http.ResponseWriter, r *http.Request)         { notYetM17(w, r) }
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request)            { notYetM17(w, r) }
func (s *Server) handleSiteUsage(w http.ResponseWriter, r *http.Request)        { notYetM17(w, r) }
func (s *Server) handleUsageSuggestions(w http.ResponseWriter, r *http.Request) { notYetM17(w, r) }
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request)            { notYetM17(w, r) }
func (s *Server) handleProbeRun(w http.ResponseWriter, r *http.Request)         { notYetM17(w, r) }
func (s *Server) handleProbeCleanup(w http.ResponseWriter, r *http.Request)     { notYetM17(w, r) }
