package server

// Phần health của API (M4, mục 6.7 của docs/KE-HOACH.md): tổng quan đọc từ
// check_results, sự cố, sửa tự động, nhận bản sửa tay, chạy health ngay, số
// request theo site, log trực tiếp, và bộ hẹn giờ chạy trong `tpan serve`.
// Bảng đường dẫn và kiểu: types.go.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/monitor"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// healthBusyProbe: thử khoá toàn cục trong chừng này để biết tiến trình
	// khác (CLI) có đang chạy job không. Ngắn: chỉ cần biết "đang bị giữ".
	healthBusyProbe     = 50 * time.Millisecond
	apiIncidentsDefault = 100
	apiIncidentsMax     = 500
)

// ---------------------------------------------------------------- bộ hẹn giờ

// healthRunner dựng Runner trên Deps. Máy demo không có chứng chỉ, /etc/tpan
// của panel: bỏ phép kiểm của chính tpan (NoSelf), không thì luôn đỏ.
func (s *Server) healthRunner() *health.Runner {
	return &health.Runner{
		Sys: s.deps.Sys, Store: s.deps.Store, DBPath: s.deps.DBPath,
		WPCLISHA512: s.deps.WPCLISHA512, NoSelf: s.deps.Demo, Now: s.deps.Now,
	}
}

// healthBusy là Scheduler.Busy: có job thay đổi máy trong tiến trình này, hoặc
// khoá toàn cục đang bị tiến trình khác giữ (job gõ ở CLI). Lấy được khoá thì
// nhả ngay: không giữ khoá trong lúc kiểm, để người bấm "tạo site" đúng lúc
// health chạy không bị 409.
func (s *Server) healthBusy() bool {
	a := s.api
	a.mu.Lock()
	busy := a.busy
	a.mu.Unlock()
	if busy {
		return true
	}
	return s.lockHeldElsewhere()
}

// lockHeldElsewhere thử khoá toàn cục trong healthBusyProbe rồi nhả ngay: true
// khi tiến trình khác (CLI) đang giữ nó. Dùng chung cho health và bộ hẹn giờ
// dọn thùng rác (trash_timer.go).
func (s *Server) lockHeldElsewhere() bool {
	ctx, cancel := context.WithTimeout(context.Background(), healthBusyProbe)
	defer cancel()
	unlock, err := s.deps.Sys.Lock(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return true
		}
		// Khoá hỏng (không tạo được /run/tpan...) thì job cũng không chạy được,
		// nên không có gì đang đổi máy: cứ kiểm.
		slog.Warn("không thử được khoá toàn cục", "err", err)
		return false
	}
	unlock()
	return false
}

// kick xin health kiểm lại sớm (Scheduler.Kick). Không có bộ hẹn giờ (test
// dựng Server bằng NewHandler) thì không làm gì.
func (s *Server) kick(domain string) {
	if s.sched != nil {
		s.sched.Kick(domain)
	}
}

// ---------------------------------------------------------------- tiện ích

// apiReadJSON đọc thân JSON (tối đa 64 KiB, không nhận trường lạ) vào v; sai
// thì đã trả 400 bad_request và trả false.
func apiReadJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return apiReadJSONMax(w, r, v, 64<<10)
}

// apiReadJSONMax là apiReadJSON với trần limit byte: POST /apply nhận tới
// core.MaxSitesEntries dòng, quá 64 KiB.
func apiReadJSONMax(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", i18n.T(
			"Thân request không phải JSON hợp lệ: "+err.Error(), "The request body is not valid JSON: "+err.Error()))
		return false
	}
	return true
}

// apiFindSite đọc domain raw (từ đường dẫn hay query), đòi nó hợp lệ và có
// trong store; không thì đã trả 400 hoặc 404 và trả false.
func (s *Server) apiFindSite(w http.ResponseWriter, r *http.Request, raw string) (site.Site, bool) {
	d, err := validate.ParseDomain(raw)
	if err != nil {
		apiFail(w, r, err)
		return site.Site{}, false
	}
	st, ok, err := s.deps.Store.Site(r.Context(), d)
	if err != nil {
		apiFail(w, r, err)
		return site.Site{}, false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "site_not_found", i18n.T(
			"Không có site "+d.String()+" trong tpan.", "tpan has no site "+d.String()+"."))
		return site.Site{}, false
	}
	return st, true
}

// stageOrder là thứ tự chặng trên đường đi của request (mục 6.7), rồi tới máy
// và chính tpan. Giao diện vẽ phép kiểm theo đúng thứ tự này.
var stageOrder = map[checks.Stage]int{
	checks.StageDNS: 0, checks.StageTLS: 1, checks.StageWeb: 2, checks.StagePHP: 3,
	checks.StageWordPress: 4, checks.StageDB: 5, checks.StageEndToEnd: 6,
	checks.StageMachine: 7, checks.StageTPAN: 8,
}

func stageRank(st checks.Stage) int {
	if n, ok := stageOrder[st]; ok {
		return n
	}
	return len(stageOrder) // chặng lạ (dữ liệu cũ) xuống cuối, không mất
}

// apiGroupResults chia kết quả theo target, mỗi nhóm xếp theo chặng rồi theo id.
func apiGroupResults(rs []store.StoredResult) map[string][]checks.Result {
	out := map[string][]checks.Result{}
	for _, r := range rs {
		out[r.Target] = append(out[r.Target], r.Result)
	}
	for _, g := range out {
		slices.SortFunc(g, func(a, b checks.Result) int {
			if d := stageRank(a.Stage) - stageRank(b.Stage); d != 0 {
				return d
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	return out
}

// ---------------------------------------------------------------- tổng quan

// handleOverview gom mọi thứ Bảng điều khiển cần. Phép kiểm đọc từ
// check_results (health định kỳ ghi), không chạy lúc gọi: Bảng điều khiển gọi
// trang này thường xuyên, chạy lại mọi phép kiểm mỗi lần thì chậm và tốn tải.
// ?refresh=1 (nút "Kiểm lại") chạy health đầy đủ trước rồi mới đọc.
//
// M13: mọi phần lọc theo chủ ở truy vấn. User thường không thấy phần máy
// (Machine, StackChecks, TPANChecks rỗng) mà thấy Quota; ?refresh=1 của họ bị
// bỏ qua (chạy health đầy đủ là việc của admin, quyết định 12).
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := who(r)
	scope := scopeOf(r)
	if r.URL.Query().Get("refresh") == "1" && id.IsAdmin() {
		if _, err := s.healthRunner().Run(ctx, health.Options{Full: true}); err != nil {
			apiFail(w, r, err)
			return
		}
	}
	cfg, err := s.apiStack().Config(ctx)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	sites, err := s.deps.Store.SitesOf(ctx, scope)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	jobs, err := s.deps.Store.JobsOf(ctx, scope, "", apiOverviewJobs)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	results, err := s.deps.Store.CheckResultsOf(ctx, scope)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	incs, err := s.deps.Store.IncidentsOf(ctx, scope, true, 0)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	hide, err := s.foreignCheck(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	results = slices.DeleteFunc(results, func(sr store.StoredResult) bool { return hide != nil && hide(sr.ID) })
	incs = keepIncidents(incs, hide)
	groups := apiGroupResults(results)
	ov := Overview{
		Hostname:    s.deps.Hostname,
		Stack:       StackSummary{Installed: cfg.PHPVersion != "", Web: string(cfg.Web), PHPVersion: cfg.PHPVersion},
		Sites:       make([]SiteRow, len(sites)),
		Jobs:        apiNonNil(jobs),
		StackChecks: []checks.Result{},
		TPANChecks:  []checks.Result{},
		Incidents:   apiNonNil(incs),
		LastRun:     s.apiLastRun(ctx),
	}
	if id.IsAdmin() {
		ov.StackChecks = apiNonNil(groups[""])
		ov.TPANChecks = apiNonNil(groups[store.TargetTPAN])
		// Số liệu máy đo trước, một mình: CPU% là phần bận giữa hai lần đọc
		// /proc/stat, mà đọc access.log (tail) bên dưới cũng là tải. Đo song song
		// thì Bảng điều khiển thấy CPU bận vì chính tpan (thấy trên máy ảo ở e2e M3).
		ov.Machine = s.apiMachine(ctx)
	} else {
		sum, err := s.apiAccounts().Summary(ctx, id.Name)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		ov.Quota = &Quota{Package: sum.Limits, Usage: sum.Usage}
	}
	var wg sync.WaitGroup
	for i, st := range sites {
		ov.Sites[i] = SiteRow{Site: st, Checks: apiNonNil(groups[st.Domain.String()])}
		wg.Go(func() { ov.Sites[i].Traffic = s.apiTraffic(ctx, st) })
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, ov)
}

// apiLastRun đọc lần health chạy đầy đủ gần nhất; chưa có hay đọc lỗi thì zero
// (một con số trên trang không đáng làm hỏng cả Bảng điều khiển).
func (s *Server) apiLastRun(ctx context.Context) time.Time {
	v, ok, err := s.deps.Store.GetSetting(ctx, health.SettingLastRun)
	if err != nil || !ok {
		if err != nil {
			slog.Warn("đọc lần chạy health gần nhất", "err", err)
		}
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		slog.Warn("đọc lần chạy health gần nhất", "value", v, "err", err)
		return time.Time{}
	}
	return t
}

// apiTraffic đếm request và 5xx một giờ gần nhất của site; lỗi thì nil (giao
// diện hiện "chưa đọc được") và ghi log.
func (s *Server) apiTraffic(ctx context.Context, st site.Site) *monitor.Traffic {
	t, err := monitor.SiteTraffic(ctx, s.deps.Sys, st.Layout().LogDir+"/access.log", s.now(), 0)
	if err != nil {
		slog.Warn("đếm request của site", "domain", st.Domain.String(), "err", err)
		return nil
	}
	return &t
}

// ---------------------------------------------------------------- sự cố

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	openOnly := true
	switch q.Get("state") {
	case "", "open":
	case "all":
		openOnly = false
	default:
		writeError(w, http.StatusBadRequest, "bad_request", i18n.T(
			"state phải là open hoặc all.", "state must be open or all."))
		return
	}
	limit := apiIncidentsDefault
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > apiIncidentsMax {
			writeError(w, http.StatusBadRequest, "limit_invalid", i18n.T(
				"limit phải là số từ 1 tới "+strconv.Itoa(apiIncidentsMax)+".",
				"limit must be a number from 1 to "+strconv.Itoa(apiIncidentsMax)+"."))
			return
		}
		limit = n
	}
	incs, err := s.deps.Store.IncidentsOf(r.Context(), scopeOf(r), openOnly, limit)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	hide, err := s.foreignCheck(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(keepIncidents(incs, hide)))
}

func (s *Server) handleIncident(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := r.PathValue("id")
	notFound := func() {
		writeError(w, http.StatusNotFound, "incident_not_found", i18n.T("Không có sự cố "+raw+".", "No incident "+raw+"."))
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		notFound()
		return
	}
	inc, err := s.deps.Store.Incident(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		notFound()
		return
	}
	if err != nil {
		apiFail(w, r, err)
		return
	}
	hide, err := s.foreignCheck(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if hide != nil && hide(inc.CheckID) {
		notFound()
		return
	}
	d := IncidentDetail{Incident: inc, Jobs: []engine.Job{}}
	cur, ok, err := s.deps.Store.CheckResult(ctx, inc.CheckID)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if ok {
		d.Current = &cur
	}
	for _, jid := range inc.Jobs {
		job, err := s.deps.Store.Job(ctx, jid)
		if errors.Is(err, store.ErrNotFound) {
			continue // job cũ đã bị dọn khỏi lịch sử; sự cố vẫn giữ id của nó
		}
		if err != nil {
			apiFail(w, r, err)
			return
		}
		d.Jobs = append(d.Jobs, job)
	}
	writeJSON(w, http.StatusOK, d)
}

// ---------------------------------------------------------------- sửa tự động

func (s *Server) apiFixes() *core.Fixes {
	st := s.deps.Store
	return &core.Fixes{
		// Bọc như apiSites: chờ khoá toàn cục có hạn rồi trả 409.
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Journal: st, Files: st,
		Accounts: st, Runs: st, // M17: cách sửa của lồng và của phép thử
		WPCLISHA512: s.deps.WPCLISHA512,
	}
}

// handleFix sửa tự động theo kết quả gần nhất của phép kiểm (không theo kết
// quả người gọi gửi lên: cách sửa phải khớp đúng điều health đã thấy).
func (s *Server) handleFix(w http.ResponseWriter, r *http.Request) {
	var body FixRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	sr, ok, err := s.deps.Store.CheckResult(r.Context(), body.CheckID)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "check_not_found", i18n.T(
			"Không có kết quả phép kiểm "+body.CheckID+". Chạy health lại rồi thử.",
			"No result for check "+body.CheckID+". Run health again, then retry."))
		return
	}
	fixes := s.apiFixes()

	if apiDryRun(r) {
		fix, err := fixes.Prepare(r.Context(), sr.Result)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		p, err := fix.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}

	action := "fix"
	if name := core.AutoFixName(sr.Result); name != "" {
		action += "." + name
	}
	if !s.api.claim() {
		s.jobBusy(w, r, action, body.CheckID)
		return
	}
	fix, err := fixes.Prepare(r.Context(), sr.Result)
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, action, body.CheckID, err)
		return
	}
	o := jobOpts{
		action: "fix." + fix.Name, target: body.CheckID,
		// Kick "" (mọi thứ, nhẹ) chứ không phải domain: lần kiểm theo domain
		// bỏ phép kiểm lệch file, và nhiều cách sửa chạm thứ dùng chung (khởi
		// động lại PHP-FPM, ghi lại mọi vhost).
		onStart: func(ctx context.Context, id string) {
			// Gắn ngay để màn Sự cố thấy job sửa trong lúc nó chạy.
			if err := s.deps.Store.LinkIncidentJob(ctx, body.CheckID, id, s.now()); err != nil {
				slog.Error("gắn job sửa vào sự cố", "check", body.CheckID, "job", id, "err", err)
			}
		},
	}
	s.jobStart(w, r, o, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		rep, err := fix.Execute(ctx, onEvent)
		return jobResult{status: rep.Job.Status}, err
	})
}

// ---------------------------------------------------------------- nhận bản sửa tay

// handleAcceptFile nhận nội dung đang có của một file tpan quản lý làm chuẩn
// mới: phép kiểm lệch so với sha256 này từ giờ.
func (s *Server) handleAcceptFile(w http.ResponseWriter, r *http.Request) {
	const action = "file.accept"
	var body AcceptFileRequest
	if !apiReadJSON(w, r, &body) {
		return
	}
	// Chỉ path trùng đúng một dòng managed_files: đây là dữ liệu từ ngoài, và
	// nhận một path bất kỳ là cho người gọi bắt tpan "quản lý" file lạ.
	files, err := s.deps.Store.ManagedFiles(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if body.Path == "" || !slices.ContainsFunc(files, func(f store.ManagedFile) bool { return f.Path == body.Path }) {
		writeError(w, http.StatusBadRequest, "path_invalid", i18n.T(
			"File này không do tpan quản lý.", "This file is not managed by tpan."))
		return
	}
	// Không nhận khi đang có job: job có thể đang ghi đúng file này, và nhận
	// một bản ghi dở làm chuẩn thì lệch thật sẽ không bao giờ được báo.
	if !s.api.claim() {
		s.jobBusy(w, r, action, body.Path)
		return
	}
	unlock, err := jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait}.Lock(r.Context())
	if err != nil {
		s.api.release()
		if errors.Is(err, errJobBusy) {
			s.jobBusy(w, r, action, body.Path)
			return
		}
		apiFail(w, r, err)
		return
	}
	err = s.acceptFile(r.Context(), body.Path)
	// Nhả trước khi kick: vòng kiểm thấy còn job (hay khoá bị giữ) thì bỏ qua.
	unlock()
	s.api.release()
	if err != nil {
		s.audit(r, action, body.Path, "error", "")
		apiFail(w, r, err)
		return
	}
	s.audit(r, action, body.Path, "ok", "")
	s.kick("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) acceptFile(ctx context.Context, path string) error {
	data, err := s.deps.Sys.ReadFile(path)
	if err != nil {
		return fmt.Errorf("đọc %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return s.deps.Store.RecordFile(ctx, path, hex.EncodeToString(sum[:]), "manual")
}

// ---------------------------------------------------------------- chạy ngay

// handleHealthRun chạy health đầy đủ ngay (đồng bộ) và trả báo cáo. Có
// ?domain=D thì chỉ kiểm site đó.
func (s *Server) handleHealthRun(w http.ResponseWriter, r *http.Request) {
	o := health.Options{Full: true}
	if raw := r.URL.Query().Get("domain"); raw != "" {
		st, ok := s.apiFindSite(w, r, raw)
		if !ok {
			return
		}
		o.Domain = st.Domain.String()
	}
	rep, err := s.healthRunner().Run(r.Context(), o)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	rep.Groups = apiNonNil(rep.Groups)
	rep.Changes.Opened = apiNonNil(rep.Changes.Opened)
	rep.Changes.Closed = apiNonNil(rep.Changes.Closed)
	writeJSON(w, http.StatusOK, rep)
}

// ---------------------------------------------------------------- số request

func (s *Server) handleSiteTraffic(w http.ResponseWriter, r *http.Request) {
	st, ok := s.apiFindSite(w, r, r.PathValue("domain"))
	if !ok {
		return
	}
	t, err := monitor.SiteTraffic(r.Context(), s.deps.Sys, st.Layout().LogDir+"/access.log", s.now(), 0)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ---------------------------------------------------------------- log trực tiếp

// apiLogPath trả đường dẫn log tên name của site. Đường dẫn chỉ lấy từ Layout
// với ba tên cố định: người gọi không bao giờ đưa được đường dẫn vào lệnh tail.
func apiLogPath(l site.Layout, name string) (string, bool) {
	switch name {
	case "access":
		return l.LogDir + "/access.log", true
	case "error":
		return l.LogDir + "/error.log", true
	case "php":
		return l.PHPErrorLog, true
	}
	return "", false
}

// handleSiteLog gửi log của site theo thời gian thực (SSE, định dạng ở
// types.go): chạy `tail -n 100 -F <file>` và gửi từng dòng một.
//
// Luồng sống tối đa LogStreamMax (LocalSystem.Run giữ output trong bộ nhớ tới
// khi lệnh xong). Client rời đi thì r.Context() huỷ, LocalSystem giết tail
// (exec.CommandContext). tail tự thoát trước (file lỗi, hay máy giả của --demo)
// thì gửi "end" để giao diện thôi tự nối lại.
func (s *Server) handleSiteLog(w http.ResponseWriter, r *http.Request) {
	st, ok := s.apiFindSite(w, r, r.PathValue("domain"))
	if !ok {
		return
	}
	p, ok := apiLogPath(st.Layout(), r.URL.Query().Get("file"))
	if !ok {
		writeError(w, http.StatusBadRequest, "file_invalid", i18n.T(
			"file phải là access, error hoặc php.", "file must be access, error or php."))
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		return
	}
	// Chỉ một goroutine ghi: OnOutput được gọi tuần tự (system đảm bảo) và
	// không còn được gọi sau khi Run trả về, rồi mới tới sự kiện "end".
	send := func(event string, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return rc.Flush()
	}

	ctx, cancel := context.WithTimeout(r.Context(), LogStreamMax)
	defer cancel()
	_, err := s.deps.Sys.Run(ctx, system.Cmd{
		Name: "tail",
		Args: []string{"-n", "100", "-F", p},
		OnOutput: func(line string) {
			// Ghi lỗi nghĩa là client đã ngắt: dừng tail luôn.
			if send("line", struct {
				Line string `json:"line"`
			}{line}) != nil {
				cancel()
			}
		},
	})
	if ctx.Err() != nil {
		// Client rời đi, hoặc hết LogStreamMax: đóng lặng lẽ, trình duyệt tự nối lại.
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_ = send("end", struct {
		Error string `json:"error"`
	}{msg})
}
