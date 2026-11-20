package server

// API của M15: nút cập nhật TPAN trong panel (mục 6.14 của docs/KE-HOACH.md;
// bảng đường dẫn và quy ước ở types_m15.go). Đợt 0 chỉ có khung; thợ đợt 2
// viết thân.

import (
	"net/http"
	"runtime"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
)

// apiSelf dựng ca sử dụng self-update. System không bọc jobLockSystem: Start
// không chạy Plan nào trong tiến trình này (đơn vị tạm tự lấy khoá của nó).
func (s *Server) apiSelf() *core.SelfUpdate {
	st := s.deps.Store
	return &core.SelfUpdate{Sys: s.deps.Sys, Settings: st, Journal: st, Arch: runtime.GOARCH, Version: buildinfo.Version}
}

// Giữ chỗ của đợt 0 (để lint không báo "unused" khi handler chưa có thân); thợ
// viết thân thì xoá hai dòng này.
var (
	_ = selfInterruptAfter
	_ = (*Server).apiSelf
)

func selfNotImplemented(w http.ResponseWriter) {
	writeError(w, http.StatusNotImplemented, "not_implemented", i18n.T("M15: chưa viết.", "M15: not written yet."))
}

// handleSelf: GET /api/v1/self.
func (s *Server) handleSelf(w http.ResponseWriter, _ *http.Request) { selfNotImplemented(w) }

// handleSelfUpdate: POST /api/v1/self/update (dry_run=1 là "Kiểm bản mới").
func (s *Server) handleSelfUpdate(w http.ResponseWriter, _ *http.Request) { selfNotImplemented(w) }

// handleSelfRollback: POST /api/v1/self/rollback.
func (s *Server) handleSelfRollback(w http.ResponseWriter, _ *http.Request) { selfNotImplemented(w) }
