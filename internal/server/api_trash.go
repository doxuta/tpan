package server

// API của M6: thùng rác và hàng loạt (đường dẫn và quy ước ở types_m6.go).
// Hợp đồng đợt 0: handler trả 501; thân viết ở đợt 2.

import (
	"net/http"

	"github.com/doxuta/tpan/internal/i18n"
)

func m6TODO(w http.ResponseWriter) {
	writeError(w, http.StatusNotImplemented, "not_implemented", i18n.T("chưa có trong bản này", "not in this build yet"))
}

func (s *Server) handleSiteRestore(w http.ResponseWriter, _ *http.Request)   { m6TODO(w) }
func (s *Server) handleTrash(w http.ResponseWriter, _ *http.Request)         { m6TODO(w) }
func (s *Server) handleTrashSettings(w http.ResponseWriter, _ *http.Request) { m6TODO(w) }
func (s *Server) handleTrashPurge(w http.ResponseWriter, _ *http.Request)    { m6TODO(w) }
func (s *Server) handleApply(w http.ResponseWriter, _ *http.Request)         { m6TODO(w) }
func (s *Server) handleExport(w http.ResponseWriter, _ *http.Request)        { m6TODO(w) }
