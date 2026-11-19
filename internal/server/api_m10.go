package server

// API của M10: chuyển LEMP ⇄ LAMP (mục 6.6; đường dẫn và quy ước ở
// types_m10.go). Đi qua planJob (api_ssl.go) như mọi POST chạy Plan.

import (
	"context"
	"net/http"

	"github.com/doxuta/tpan/internal/core"
)

// handleStackSwitch chuyển web server. Target của nhật ký là web server người
// gọi gửi, kể cả khi sai: nhật ký ghi đúng thứ đã bị từ chối.
func (s *Server) handleStackSwitch(w http.ResponseWriter, r *http.Request) {
	var body StackSwitchBody
	if !apiReadJSON(w, r, &body) {
		return
	}
	st := s.deps.Store
	sw := &core.Switch{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Sites: st, Journal: st,
	}
	s.planJob(w, r, "stack.switch", body.To, func(ctx context.Context) (apiPlan, error) {
		return s.withHealth(sw.Prepare(ctx, core.WebServer(body.To)))
	})
}
