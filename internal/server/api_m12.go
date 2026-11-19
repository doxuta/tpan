package server

// Handler của M12 (types_m12.go). Thân tạm của đợt 0: trả 501; thợ đợt 2 viết.

import (
	"net/http"

	"github.com/doxuta/tpan/internal/i18n"
)

func m12NotYet(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented",
		i18n.T("Chưa có ở bản này.", "Not in this version yet."))
}

func (s *Server) handlePHP(w http.ResponseWriter, r *http.Request)            { m12NotYet(w, r) }
func (s *Server) handlePHPInstall(w http.ResponseWriter, r *http.Request)     { m12NotYet(w, r) }
func (s *Server) handlePHPRemove(w http.ResponseWriter, r *http.Request)      { m12NotYet(w, r) }
func (s *Server) handleSitePHP(w http.ResponseWriter, r *http.Request)        { m12NotYet(w, r) }
func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request)        { m12NotYet(w, r) }
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request)    { m12NotYet(w, r) }
func (s *Server) handleUpdateAuto(w http.ResponseWriter, r *http.Request)     { m12NotYet(w, r) }
func (s *Server) handleMariaDBUpgrade(w http.ResponseWriter, r *http.Request) { m12NotYet(w, r) }
