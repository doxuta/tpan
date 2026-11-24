package server

// Dựng ba ca sử dụng của M17 trên Deps (như apiSites, apiPHP): System bọc
// jobLockSystem để chờ khoá toàn cục có hạn, mọi bảng là s.deps.Store.

import "github.com/doxuta/tpan/internal/core"

// apiCages dựng ca sử dụng lồng. Accounts bắt buộc: thiếu nó thì mọi site coi
// như thuộc gói default và Plan ghi lại file của lồng với trần 0 (bất biến 7).
func (s *Server) apiCages() *core.Cages {
	st := s.deps.Store
	return &core.Cages{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Accounts: st, Journal: st, Now: s.now,
	}
}

// apiUsages dựng ca sử dụng số liệu. Samples là bảng mẫu (Sample, Rollup,
// History, gợi ý).
func (s *Server) apiUsages() *core.Usages {
	st := s.deps.Store
	return &core.Usages{
		Sys:   jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Store: st, Accounts: st, Samples: st, Now: s.now,
	}
}

// apiProbe dựng ca sử dụng thử tấn công. Store chỉ để đọc (site thử không vào
// bảng sites); Runs là sổ probe_runs.
func (s *Server) apiProbe() *core.Probe {
	st := s.deps.Store
	return &core.Probe{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Runs: st, Journal: st,
		WPHash: s.deps.WPHash, Now: s.now,
	}
}
