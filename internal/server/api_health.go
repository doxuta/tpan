package server

// Phần health của API (M4, mục 6.7 của docs/KE-HOACH.md): sự cố, sửa tự động,
// nhận bản sửa tay, chạy health ngay, số request theo site, log trực tiếp.
// Bảng đường dẫn và kiểu: types.go. Thân hàm do thợ của phần server viết.

import (
	"net/http"

	"github.com/doxuta/tpan/internal/i18n"
)

func notWritten(w http.ResponseWriter) {
	writeError(w, http.StatusNotImplemented, "not_implemented", i18n.T("chưa viết", "not implemented"))
}

func (s *Server) handleIncidents(w http.ResponseWriter, _ *http.Request)   { notWritten(w) }
func (s *Server) handleIncident(w http.ResponseWriter, _ *http.Request)    { notWritten(w) }
func (s *Server) handleFix(w http.ResponseWriter, _ *http.Request)         { notWritten(w) }
func (s *Server) handleAcceptFile(w http.ResponseWriter, _ *http.Request)  { notWritten(w) }
func (s *Server) handleHealthRun(w http.ResponseWriter, _ *http.Request)   { notWritten(w) }
func (s *Server) handleSiteTraffic(w http.ResponseWriter, _ *http.Request) { notWritten(w) }
func (s *Server) handleSiteLog(w http.ResponseWriter, _ *http.Request)     { notWritten(w) }
