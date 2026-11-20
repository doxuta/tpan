package server

// API của M12: phiên bản PHP theo site, màn Cập nhật, nâng MariaDB (mục 6.11
// của docs/KE-HOACH.md; bảng đường dẫn và quy ước ở types_m12.go). Mọi POST đi
// qua planJob (api_ssl.go): ?dry_run=1 trả bản xem trước, không thì chạy job ở
// nền và trả 202, lời từ chối của core là 409 kèm checks, có audit.

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/validate"
)

// apiPHP dựng ca sử dụng PHP. System chờ khoá có hạn như apiSites.
func (s *Server) apiPHP() *core.PHP {
	st := s.deps.Store
	return &core.PHP{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Sites: st, Journal: st,
	}
}

// apiUpdates dựng ca sử dụng của màn Cập nhật.
func (s *Server) apiUpdates() *core.Updates {
	st := s.deps.Store
	return &core.Updates{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Sites: st, Journal: st, Backups: st,
		Version: buildinfo.Version, WPCLISHA512: s.deps.WPCLISHA512,
	}
}

// ---------------------------------------------------------------- GET /php

const (
	// phpNotesTTL: hạn hỗ trợ của PHP và bản WordPress khuyên dùng đổi vài lần
	// một năm, mà mỗi lần hỏi có thể chờ tới 5 giây mỗi nguồn khi mạng chậm.
	phpNotesTTL = time.Hour
	// phpNotesRetry: không hỏi được (máy không ra mạng) thì nhớ kết quả trống
	// ngắn hơn, để mạng có lại là ghi chú hiện sớm, mà vẫn không bắt mỗi lần
	// tải trang chờ hết thời gian chờ của curl.
	phpNotesRetry = 5 * time.Minute
)

// phpNotes đệm phần ghi chú của GET /api/v1/php (core.PHP.List với notes).
// Chỉ đệm ghi chú: danh sách bản PHP (đã cài, site nào dùng) đọc mới mỗi lần,
// nên vừa chạy xong php.install, php.remove hay site.php là thấy ngay.
type phpNotes struct {
	mu sync.Mutex
	at time.Time
	ok bool
	// def là bản mặc định lúc hỏi: WordPress.org được hỏi theo bản này.
	def string
	wp  string
	// support có một khoá cho MỌI bản có trong danh sách lúc hỏi; giá trị nil
	// là php.net không có ghi chú cho bản đó (khác với "chưa hỏi").
	support map[string]*core.PHPSupport
}

// fill chép ghi chú đã đệm vào list. false khi đệm hết hạn hoặc không phủ được
// list (bản mặc định đã đổi, hay có bản mới mà lúc hỏi chưa có).
func (n *phpNotes) fill(list *core.PHPList, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	ttl := phpNotesTTL
	if !n.ok {
		ttl = phpNotesRetry
	}
	if n.at.IsZero() || now.Sub(n.at) >= ttl || phpDefault(*list) != n.def {
		return false
	}
	for _, r := range list.Versions {
		if _, seen := n.support[r.Version]; !seen {
			return false
		}
	}
	for i := range list.Versions {
		if s := n.support[list.Versions[i].Version]; s != nil {
			c := *s
			list.Versions[i].Support = &c
		}
	}
	list.WPRecommended, list.NotesOK = n.wp, n.ok
	return true
}

// save nhớ ghi chú của một list vừa hỏi mạng.
func (n *phpNotes) save(list core.PHPList, now time.Time) {
	support := make(map[string]*core.PHPSupport, len(list.Versions))
	for _, r := range list.Versions {
		support[r.Version] = r.Support
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.at, n.ok, n.def, n.wp, n.support = now, list.NotesOK, phpDefault(list), list.WPRecommended, support
}

func phpDefault(list core.PHPList) string {
	if i := slices.IndexFunc(list.Versions, func(r core.PHPVersionRow) bool { return r.Default }); i >= 0 {
		return list.Versions[i].Version
	}
	return ""
}

// handlePHP trả danh sách bản PHP kèm ghi chú. Đệm còn hạn thì đọc danh sách
// không hỏi mạng rồi chép ghi chú vào; không thì hỏi đủ và đệm lại. Hai
// request cùng lúc lúc đệm hết hạn có thể cùng hỏi mạng: chấp nhận được, không
// giữ khoá trong lúc chờ curl.
func (s *Server) handlePHP(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), s.apiPHP()
	list, err := p.List(ctx, false)
	if err == nil && !s.phpNotes.fill(&list, s.now()) {
		if list, err = p.List(ctx, true); err == nil {
			s.phpNotes.save(list, s.now())
		}
	}
	if err != nil {
		apiFail(w, r, err)
		return
	}
	// M13: mỗi bản PHP liệt kê site đang dùng nó; user thường chỉ thấy site của
	// mình. Lọc trên bản chép: list có thể là bản đệm dùng chung.
	mine, err := s.ownedDomains(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if mine != nil {
		for i := range list.Versions {
			var own []string
			for _, d := range list.Versions[i].Sites {
				if mine[d] {
					own = append(own, d)
				}
			}
			list.Versions[i].Sites = apiNonNil(own)
		}
	}
	list.Versions = apiNonNil(list.Versions)
	writeJSON(w, http.StatusOK, list)
}

// ---------------------------------------------------------------- POST PHP

// Target của nhật ký là bản PHP người gọi gửi, kể cả khi sai: nhật ký phải ghi
// đúng thứ đã bị từ chối.
func (s *Server) handlePHPInstall(w http.ResponseWriter, r *http.Request) {
	var body PHPVersionBody
	if !apiReadJSON(w, r, &body) {
		return
	}
	php := s.apiPHP()
	s.planJob(w, r, "php.install", body.Version, func(ctx context.Context) (apiPlan, error) {
		return php.PrepareInstall(ctx, body.Version)
	})
}

func (s *Server) handlePHPRemove(w http.ResponseWriter, r *http.Request) {
	var body PHPVersionBody
	if !apiReadJSON(w, r, &body) {
		return
	}
	php := s.apiPHP()
	s.planJob(w, r, "php.remove", body.Version, func(ctx context.Context) (apiPlan, error) {
		return php.PrepareRemove(ctx, body.Version)
	})
}

// handleSitePHP đổi bản PHP của một site (Plan php.use). Kick của planJob là
// "" (mọi thứ): job chạm pool của hai bản PHP và vhost của site.
func (s *Server) handleSitePHP(w http.ResponseWriter, r *http.Request) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	var body PHPVersionBody
	if !apiReadJSON(w, r, &body) {
		return
	}
	php := s.apiPHP()
	s.planJob(w, r, "site.php", d.String(), func(ctx context.Context) (apiPlan, error) {
		return php.PrepareUse(ctx, d, body.Version)
	})
}

// ---------------------------------------------------------------- cập nhật

// handleUpdates trả nội dung màn Cập nhật. Không chạy apt-get update (refresh
// false): việc đó cần khoá toàn cục và có thể mất cả phút; danh sách gói do
// apt-daily của Ubuntu làm mới, và `sudo tpan update --check` làm mới ngay.
func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	st, err := s.apiUpdates().Status(r.Context(), false)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	st.Packages = apiNonNil(st.Packages)
	for i := range st.Packages {
		st.Packages[i].Origins = apiNonNil(st.Packages[i].Origins)
	}
	st.AutoOrigins = apiNonNil(st.AutoOrigins)
	st.MariaDB.Upgrades = apiNonNil(st.MariaDB.Upgrades)
	writeJSON(w, http.StatusOK, st)
}

// healthAfter bọc Plan đổi gói: job xong (thành công hay không) thì chạy health
// đầy đủ như nút "Kiểm lại", để site đang đạt mà sau khi đổi gói không đạt
// được mở sự cố ngay (mục 6.11, "Cập nhật"). Job hỏng vẫn kiểm: gói đã nâng
// không hoàn tác được. Chạy trong goroutine của job, trước khi nhả chỗ chạy
// job: bộ hẹn giờ health thấy đang có job nên không chạy chồng lên.
type healthAfter struct {
	*core.Planned
	s *Server
}

func (h healthAfter) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	rep, err := h.Planned.Execute(ctx, onEvent)
	if rep.Job.ID != "" {
		if _, herr := h.s.healthRunner().Run(ctx, health.Options{Full: true}); herr != nil {
			slog.Error("chạy lại health sau job", "job", rep.Job.ID, "kind", rep.Job.Kind, "err", herr)
		}
	}
	return rep, err
}

func (s *Server) withHealth(p *core.Planned, err error) (apiPlan, error) {
	if err != nil {
		return nil, err
	}
	return healthAfter{Planned: p, s: s}, nil
}

// handleUpdateApply nâng các gói của stack đang chờ (không có thân request).
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	u := s.apiUpdates()
	s.planJob(w, r, "update.apply", "stack", func(ctx context.Context) (apiPlan, error) {
		return s.withHealth(u.PrepareApply(ctx))
	})
}

// handleUpdateAuto bật bản vá bảo mật tự cài hằng ngày (không có thân request).
func (s *Server) handleUpdateAuto(w http.ResponseWriter, r *http.Request) {
	u := s.apiUpdates()
	s.planJob(w, r, "update.auto", "unattended-upgrades", func(ctx context.Context) (apiPlan, error) {
		return u.PrepareAuto(ctx)
	})
}

// handleMariaDBUpgrade nâng MariaDB lên dòng mới (một chiều; bản xem trước nói
// rõ đường lùi). Dòng sai hay không cao hơn dòng đang chạy là lời từ chối 409.
func (s *Server) handleMariaDBUpgrade(w http.ResponseWriter, r *http.Request) {
	var body MariaDBUpgradeBody
	if !apiReadJSON(w, r, &body) {
		return
	}
	u := s.apiUpdates()
	s.planJob(w, r, "mariadb.upgrade", body.Line, func(ctx context.Context) (apiPlan, error) {
		return s.withHealth(u.PrepareMariaDB(ctx, body.Line))
	})
}
