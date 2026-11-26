package server

// M37 (v0.2.17): kết nối DB từ máy khác (core/db_remote_m37.go).
//
//   GET  /api/v1/db-remote                  chế độ của máy, ufw, cổng (ai cũng đọc được)
//   POST /api/v1/db-remote {on}             bật, tắt chế độ (admin; job)
//   POST /api/v1/db-remote/sync             tính lại luật tường lửa (admin; job)
//   POST /api/v1/dbs/{name}/remote {hosts}  danh sách IPv4 của DB rời (chủ, admin; job)
//   POST /api/v1/databases/{domain}/remote  như trên, cho DB của site

import (
	"context"
	"net/http"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/steps"
)

func (s *Server) handleDBRemoteStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.apiDBs().RemoteStatus(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DBRemoteStatus{On: st.On, UFWActive: st.UFWActive, Port: steps.DBRemotePort, Max: core.DBRemoteMax})
}

func (s *Server) handleDBRemoteMode(w http.ResponseWriter, r *http.Request) {
	var body DBRemoteModeRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	dbs := s.apiDBs()
	s.planJob(w, r, "db.remote-mode", "mariadb", func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareRemoteMode(ctx, body.On)
	})
}

func (s *Server) handleDBRemoteSync(w http.ResponseWriter, r *http.Request) {
	dbs := s.apiDBs()
	s.planJob(w, r, "db.remote-sync", "mariadb", func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareRemoteSync(ctx)
	})
}

func (s *Server) handleStandaloneDBRemote(w http.ResponseWriter, r *http.Request) {
	var body DBRemoteHostsRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	name, target := pathDBName(r)
	dbs := s.apiDBs()
	s.planJob(w, r, "db.remote", target, func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareRemoteHosts(ctx, name, body.Hosts)
	})
}

func (s *Server) handleSiteDBRemote(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	var body DBRemoteHostsRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	dbs := s.apiDBs()
	s.planJob(w, r, "db.remote", d.String(), func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareSiteRemoteHosts(ctx, d, body.Hosts)
	})
}
