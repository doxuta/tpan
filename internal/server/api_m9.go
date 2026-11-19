package server

// API của M9 (đường dẫn và quy ước ở types_m9.go). File tạm của đợt 0: mọi
// đường trả 501 cho tới khi thợ đợt 3 viết thân.

import (
	"net/http"

	"github.com/doxuta/tpan/internal/i18n"
)

func m9NotYet(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", i18n.T("Chưa có trong bản này.", "Not in this build yet."))
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request)        { m9NotYet(w, r) }
func (s *Server) handleSiteBackup(w http.ResponseWriter, r *http.Request)     { m9NotYet(w, r) }
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request)  { m9NotYet(w, r) }
func (s *Server) handleBackupSettings(w http.ResponseWriter, r *http.Request) { m9NotYet(w, r) }
func (s *Server) handleCron(w http.ResponseWriter, r *http.Request)           { m9NotYet(w, r) }
func (s *Server) handleDBPassword(w http.ResponseWriter, r *http.Request)     { m9NotYet(w, r) }
func (s *Server) handleDBDump(w http.ResponseWriter, r *http.Request)         { m9NotYet(w, r) }
func (s *Server) handleDBImport(w http.ResponseWriter, r *http.Request)       { m9NotYet(w, r) }
