package server

// Khung của đợt 0 M3. Thợ phần đăng nhập thay hẳn file này (xem types.go).

import "net/http"

type authState struct{}

func newAuthState(*Server) *authState { return &authState{} }

// requireSession chỉ cho request có phiên hợp lệ đi qua; không có thì 401.
func (s *Server) requireSession(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(notImplemented)
}

// secure thêm header bảo mật (mục 11) và chặn CSRF cho request có thay đổi.
func (s *Server) secure(h http.Handler) http.Handler { return h }

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request)          { notImplemented(w, r) }
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request)         { notImplemented(w, r) }
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request)        { notImplemented(w, r) }
func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request)       { notImplemented(w, r) }
func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
