package server

// Khung của đợt 0 M3. Thợ phần API thay hẳn file này (xem types.go).

import "net/http"

type apiState struct{}

func newAPIState(*Server) *apiState { return &apiState{} }

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func (s *Server) handleMachine(w http.ResponseWriter, r *http.Request)    { notImplemented(w, r) }
func (s *Server) handleStack(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (s *Server) handleChecks(w http.ResponseWriter, r *http.Request)     { notImplemented(w, r) }
func (s *Server) handleSites(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (s *Server) handleSiteCreate(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (s *Server) handleSite(w http.ResponseWriter, r *http.Request)       { notImplemented(w, r) }
func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request)       { notImplemented(w, r) }
func (s *Server) handleJob(w http.ResponseWriter, r *http.Request)        { notImplemented(w, r) }
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (s *Server) handleDatabases(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
