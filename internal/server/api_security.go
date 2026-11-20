package server

// API của trang bảo mật (mục 6.8, 9 của docs/KE-HOACH.md; hợp đồng ở
// types_m5.go): các dòng kiểm và hai nút "bật ufw", "cài fail2ban".

import (
	"context"
	"net/http"

	"github.com/doxuta/tpan/internal/core"
)

// apiSecurity dựng ca sử dụng của trang bảo mật. System chờ khoá có hạn như
// apiSites: ufw và fail2ban là job thay đổi máy như mọi job khác. Settings và
// Sites là thứ Plan "Áp bản vá cô lập" (M14, api_m14.go) cần.
func (s *Server) apiSecurity() *core.Security {
	return &core.Security{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Journal:  s.deps.Store,
		Settings: s.deps.Store,
		Sites:    s.deps.Store,
		// Kiểm chứng của "Áp bản vá cô lập" có phép kiểm WP-CLI của stack.
		WPCLISHA512: s.deps.WPCLISHA512,
	}
}

// handleSecurity chạy các dòng kiểm của trang bảo mật. Không đệm: trang này
// mở ít, và người dùng vừa bấm nút sửa thì phải thấy ngay kết quả mới.
func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	st, err := s.apiSecurity().Status(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	st.Results = apiNonNil(st.Results)
	writeJSON(w, http.StatusOK, st)
}

// Target của nhật ký là tên việc: hai thao tác này thuộc cả máy, không thuộc
// site nào.
func (s *Server) handleSecurityUFW(w http.ResponseWriter, r *http.Request) {
	sec := s.apiSecurity()
	s.planJob(w, r, "security.ufw", "ufw", func(ctx context.Context) (apiPlan, error) {
		return sec.PrepareUFW(ctx)
	})
}

func (s *Server) handleSecurityFail2ban(w http.ResponseWriter, r *http.Request) {
	sec := s.apiSecurity()
	s.planJob(w, r, "security.fail2ban", "fail2ban", func(ctx context.Context) (apiPlan, error) {
		return sec.PrepareFail2ban(ctx)
	})
}
