package server

// API của M13 (mục 6.12 của docs/KE-HOACH.md; đường dẫn và kiểu ở
// types_m13.go, luật của từng route ở routes.go): user, gói, tạm khoá site, DB
// của site, DB rời, user, xem như user, DB rời.
//
// Thao tác đổi máy (tạm khoá, DB rời, đưa mọi thứ của user vào thùng rác) đi
// qua planJob/planJobKick như mọi Plan khác: ?dry_run=1 trả bản xem trước, chạy
// thật trả 202 JobStarted. Thao tác chỉ ghi tpan.db (user, gói, ghi chú DB) trả
// kết quả ngay và ghi nhật ký. Lời từ chối mới của core đổi ra HTTP ở
// apiErrorStatus (api.go).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- dựng ca sử dụng

// apiDBs dựng ca sử dụng DB rời và DB của site. Accounts để kiểm hạn mức gói.
func (s *Server) apiDBs() *core.DBs {
	st := s.deps.Store
	return &core.DBs{
		Sys:      jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait},
		Settings: st, Store: st, Sites: st, Accounts: st, Journal: st, Now: s.now,
	}
}

// apiAccounts dựng ca sử dụng user và gói. core không import store nên hàm băm
// và sinh mật khẩu được gắn từ đây.
func (s *Server) apiAccounts() *core.Accounts {
	st := s.deps.Store
	return &core.Accounts{
		Sys: jobLockSystem{System: s.deps.Sys, wait: s.api.lockWait}, Store: st,
		Sites: s.apiSites(), DBs: s.apiDBs(), Journal: st, Now: s.now,
		Hash: store.HashPassword, Generate: store.GeneratePassword,
	}
}

// ---------------------------------------------------------------- tiện ích

// ownedDomains: nil cho admin (không lọc); user thường thì tập domain site của
// mình (khác nil kể cả khi rỗng). Dùng cho các danh sách mà core không lọc
// theo chủ (SSL, cron, PHP).
func (s *Server) ownedDomains(r *http.Request) (map[string]bool, error) {
	scope := scopeOf(r)
	if scope == "" {
		return nil, nil
	}
	list, err := s.deps.Store.SitesOf(r.Context(), scope)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(list))
	for _, st := range list {
		m[st.Domain.String()] = true
	}
	return m, nil
}

// foreignCheck trả hàm cho biết một phép kiểm (theo ID) có nêu tên site của
// người khác không, để giấu nó với user thường; admin thì nil (không giấu gì).
// Phép kiểm cô lập "isolation:<x>:<y>" chạy cho mọi cặp site trên máy, bất kể
// chủ, nên nằm trong kết quả health, phép kiểm của trang site và sự kiện job
// site.add của A với tên, đường dẫn, user Linux của site B: không giấu thì A
// dò được tên site của B (e2e M13 trên máy ảo thấy ở GET /overview, GET
// /sites/{của A}, GET /jobs/{site.add của A}). Cặp mà cả hai site đều của
// người gọi vẫn hiện.
func (s *Server) foreignCheck(r *http.Request) (func(id string) bool, error) {
	owned, err := s.ownedDomains(r)
	if err != nil || owned == nil {
		return nil, err
	}
	return func(id string) bool {
		rest, ok := strings.CutPrefix(id, "isolation:")
		if !ok {
			return false
		}
		for d := range strings.SplitSeq(rest, ":") {
			if !owned[d] {
				return true
			}
		}
		return false
	}, nil
}

// keepChecks bỏ các kết quả hide giấu (hide nil: giữ hết).
func keepChecks(rs []checks.Result, hide func(string) bool) []checks.Result {
	if hide == nil {
		return rs
	}
	return slices.DeleteFunc(rs, func(c checks.Result) bool { return hide(c.ID) })
}

// keepEvents bỏ sự kiện "check" của phép kiểm hide giấu.
func keepEvents(evs []engine.Event, hide func(string) bool) []engine.Event {
	if hide == nil {
		return evs
	}
	return slices.DeleteFunc(evs, func(e engine.Event) bool { return e.Check != nil && hide(e.Check.ID) })
}

// keepIncidents bỏ sự cố của phép kiểm hide giấu.
func keepIncidents(incs []store.Incident, hide func(string) bool) []store.Incident {
	if hide == nil {
		return incs
	}
	return slices.DeleteFunc(incs, func(i store.Incident) bool { return hide(i.CheckID) })
}

// apiReadOptionalJSON là apiReadJSON nhưng thân rỗng là giá trị zero: các lệnh
// tạm khoá, đổi mật khẩu có thân không bắt buộc.
func apiReadOptionalJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", i18n.T(
			"Thân request không phải JSON hợp lệ: "+err.Error(), "The request body is not valid JSON: "+err.Error()))
		return false
	}
	return true
}

// writeOwnerForbidden: user thường xin đặt chủ khác mình (POST /sites, /dbs).
func writeOwnerForbidden(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "forbidden", i18n.T(
		"Chỉ quản trị viên được chọn chủ khác mình.", "Only an administrator can choose another owner."))
}

// auditDone ghi nhật ký cho thao tác không phải job; err khác nil thì trả lỗi
// và false.
func (s *Server) auditDone(w http.ResponseWriter, r *http.Request, action, target string, err error) bool {
	if err != nil {
		_, eb := apiErrorStatus(err)
		s.audit(r, action, target, eb.Error.Code, "")
		apiFail(w, r, err)
		return false
	}
	s.audit(r, action, target, "ok", "")
	return true
}

// userNotFound là lời "không có user" (404 qua apiErrorStatus).
func userNotFound(name string) error {
	return &core.RefusedError{Code: core.CodeUserNotFound, Message: i18n.T(
		"không có user "+name+" trong tpan", "tpan has no user "+name)}
}

// pathUserName đọc {name} của /users/...; sai dạng là "không có" (không in lại
// chuỗi lạ).
func pathUserName(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := r.PathValue("name")
	if _, err := validate.ParseUserName(raw); err != nil {
		apiFail(w, r, userNotFound("?"))
		return "", false
	}
	return raw, true
}

// pathDBName đọc {name} của /dbs/...: tên đúng dạng thì giữ, không thì "?" (cho
// nhật ký). core tự kiểm lại và trả db_not_found với tên lạ.
func pathDBName(r *http.Request) (raw, target string) {
	raw = r.PathValue("name")
	if dbNamePathRe.MatchString(raw) {
		return raw, raw
	}
	return raw, "?"
}

// siteCreateOwner quyết chủ của site trong POST /sites (cả xem trước, để bản
// xem trước không thành chỗ dò tên site). Lỗ phải chặn: route này là any, và
// core chạy lại site đã có mà bỏ qua Owner, nên user thường gửi domain của
// người khác là chạy lại site của người đó. Ở đây: user thường đặt chủ khác
// mình thì 403; domain đã có mà không phải của mình thì 404 như không tồn tại.
// Admin rỗng là chính mình (dòng lệnh không tên: core hiểu là admin).
func (s *Server) siteCreateOwner(w http.ResponseWriter, r *http.Request, d validate.Domain, asked string) (string, bool) {
	id := who(r)
	if id.IsAdmin() {
		if asked == "" {
			asked = id.Name
		}
		return asked, true
	}
	if asked != "" && asked != id.Name {
		if !apiDryRun(r) {
			s.audit(r, "site.add", d.String(), "forbidden", "")
		}
		writeOwnerForbidden(w)
		return "", false
	}
	owner, found, err := s.deps.Store.SiteOwner(r.Context(), d.String())
	if err != nil {
		apiFail(w, r, err)
		return "", false
	}
	if found && owner != id.Name {
		if !apiDryRun(r) {
			s.audit(r, "site.add", d.String(), "site_not_found", "")
		}
		apiFail(w, r, siteNotFound(d))
		return "", false
	}
	return id.Name, true
}

// secretJob là planJobKick cho job sinh mật khẩu DB (tạo DB rời, đổi mật khẩu
// DB rời): job thành công thì giữ DBSecret cho lần GET /jobs/{id} đầu tiên. Job
// thành công mà ghi tpan.db lỗi (err khác nil) vẫn giữ: mật khẩu đã đổi trong
// MariaDB, mất nó là mất quyền vào DB.
func (s *Server) secretJob(w http.ResponseWriter, r *http.Request, o jobOpts, prepare func(context.Context) (*core.Planned, *DBSecret, error)) {
	if apiDryRun(r) {
		p, _, err := prepare(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		pv, err := p.Preview(r.Context())
		if err != nil {
			apiFail(w, r, err)
			return
		}
		s.writePreview(w, r, pv)
		return
	}
	if !s.api.claim() {
		s.jobBusy(w, r, o.action, o.target)
		return
	}
	p, sec, err := prepare(r.Context())
	if err != nil {
		s.api.release()
		s.jobRefused(w, r, o.action, o.target, err)
		return
	}
	s.jobStart(w, r, o, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		rep, err := p.Execute(ctx, onEvent)
		out := jobResult{status: rep.Job.Status}
		if rep.Job.Status == engine.JobSucceeded {
			out.dbSecret = sec
		}
		return out, err
	})
}

// ---------------------------------------------------------------- user

func userRow(s core.UserSummary) UserRow {
	return UserRow{User: s.User, Limits: s.Limits, Usage: s.Usage, OverQuota: s.OverQuota}
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.apiAccounts().Summaries(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	out := make([]UserRow, len(list))
	for i, u := range list {
		out[i] = userRow(u)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	sum, err := s.apiAccounts().Summary(r.Context(), name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userRow(sum))
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	const action = "user.add"
	var body UserCreate
	if !apiReadJSON(w, r, &body) {
		return
	}
	name, err := validate.ParseUserName(body.Name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	var email validate.Email
	if body.Email != "" {
		if email, err = validate.ParseEmail(body.Email); err != nil {
			apiFail(w, r, err)
			return
		}
	}
	a := s.apiAccounts()
	u, generated, err := a.AddUser(r.Context(), core.UserInput{
		Name: name, FullName: body.FullName, Email: email, Role: body.Role,
		Package: body.Package, Password: body.Password,
	})
	if !s.auditDone(w, r, action, name.String(), err) {
		return
	}
	sum, err := a.Summary(r.Context(), u.Name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UserCreated{User: userRow(sum), Password: generated})
}

func (s *Server) handleUserEdit(w http.ResponseWriter, r *http.Request) {
	const action = "user.edit"
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	var body UserEdit
	if !apiReadJSON(w, r, &body) {
		return
	}
	ch := core.UserChange{FullName: body.FullName, Role: body.Role, Package: body.Package}
	if body.Email != nil {
		// Chuỗi rỗng là xoá email (Email zero).
		var e validate.Email
		if *body.Email != "" {
			var err error
			if e, err = validate.ParseEmail(*body.Email); err != nil {
				apiFail(w, r, err)
				return
			}
		}
		ch.Email = &e
	}
	a := s.apiAccounts()
	_, err := a.EditUser(r.Context(), name, ch, who(r))
	if !s.auditDone(w, r, action, name, err) {
		return
	}
	sum, err := a.Summary(r.Context(), name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userRow(sum))
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	if !s.auditDone(w, r, "user.rm", name, s.apiAccounts().DeleteUser(r.Context(), name, who(r))) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUserPassword đặt mật khẩu mới cho user (rỗng thì sinh, trả một lần).
// Mọi phiên của user đó bị huỷ (core, store).
func (s *Server) handleUserPassword(w http.ResponseWriter, r *http.Request) {
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	var body PasswordSet
	if !apiReadOptionalJSON(w, r, &body) {
		return
	}
	generated, err := s.apiAccounts().SetPassword(r.Context(), name, body.Password)
	if !s.auditDone(w, r, "user.passwd", name, err) {
		return
	}
	writeJSON(w, http.StatusOK, PasswordSetResult{Password: generated})
}

// Khoá, mở khoá user và đưa mọi thứ của user vào thùng rác: job (đổi vhost,
// pool, user DB của mọi site, DB rời của họ). Kick "" vì chạm nhiều site.

func (s *Server) handleUserSuspend(w http.ResponseWriter, r *http.Request) {
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	var body SuspendRequest
	if !apiReadOptionalJSON(w, r, &body) {
		return
	}
	a, by := s.apiAccounts(), who(r)
	s.planJob(w, r, "user.suspend", name, func(ctx context.Context) (apiPlan, error) {
		return a.PrepareSuspendUser(ctx, name, body.Reason, by)
	})
}

func (s *Server) handleUserUnsuspend(w http.ResponseWriter, r *http.Request) {
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	a := s.apiAccounts()
	s.planJob(w, r, "user.unsuspend", name, func(ctx context.Context) (apiPlan, error) {
		return a.PrepareUnsuspendUser(ctx, name)
	})
}

func (s *Server) handleUserTrashAll(w http.ResponseWriter, r *http.Request) {
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	a := s.apiAccounts()
	s.planJob(w, r, "user.trash_all", name, func(ctx context.Context) (apiPlan, error) {
		return a.PrepareTrashAll(ctx, name)
	})
}

// ---------------------------------------------------------------- xem như

// handleViewAs biến phiên hiện tại thành "xem như" user name: từ request sau,
// guard dựng danh tính của user đó và chặn mọi thao tác thay đổi.
func (s *Server) handleViewAs(w http.ResponseWriter, r *http.Request) {
	name, ok := pathUserName(w, r)
	if !ok {
		return
	}
	_, found, err := s.deps.Store.User(r.Context(), name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if !found {
		apiFail(w, r, userNotFound(name))
		return
	}
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	if !s.auditDone(w, r, "session.view_as", name, s.deps.Store.SetViewAs(r.Context(), cur.TokenSHA256, name)) {
		return
	}
	s.writeSessionInfo(w, r)
}

// handleViewAsExit thoát "xem như" (chạy được trong phiên chỉ đọc, routes.go).
// Phiên không ở chế độ xem như thì không làm gì.
func (s *Server) handleViewAsExit(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	if cur.ViewAs != "" {
		if !s.auditDone(w, r, "session.view_as_exit", cur.ViewAs, s.deps.Store.SetViewAs(r.Context(), cur.TokenSHA256, "")) {
			return
		}
	}
	s.writeSessionInfo(w, r)
}

// ---------------------------------------------------------------- gói

func (s *Server) handlePackages(w http.ResponseWriter, r *http.Request) {
	list, err := s.apiAccounts().Packages(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	out := make([]PackageRow, len(list))
	for i, p := range list {
		out[i] = PackageRow{Package: p.Package, Users: p.Users}
	}
	writeJSON(w, http.StatusOK, out)
}

// packageTarget là tên gói cho nhật ký; sai dạng thì "?".
func packageTarget(name string) string {
	if _, err := validate.ParsePackageName(name); err != nil {
		return "?"
	}
	return name
}

// packageRow đọc lại gói vừa ghi (Created do core đặt) kèm số user.
func (s *Server) packageRow(w http.ResponseWriter, r *http.Request, name string) {
	p, ok, err := s.deps.Store.Package(r.Context(), name)
	if err == nil && !ok {
		err = &core.RefusedError{Code: core.CodePackageNotFound, Message: i18n.T("không có gói "+name, "no package "+name)}
	}
	if err != nil {
		apiFail(w, r, err)
		return
	}
	n, err := s.deps.Store.PackageUsers(r.Context(), name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, PackageRow{Package: p, Users: n})
}

func (s *Server) handlePackageCreate(w http.ResponseWriter, r *http.Request) {
	var body account.Package
	if !apiReadJSON(w, r, &body) {
		return
	}
	if !s.auditDone(w, r, "package.add", packageTarget(body.Name), s.apiAccounts().AddPackage(r.Context(), body)) {
		return
	}
	s.packageRow(w, r, body.Name)
}

// handlePackageEdit: tên gói lấy từ đường dẫn, không từ thân.
func (s *Server) handlePackageEdit(w http.ResponseWriter, r *http.Request) {
	var body account.Package
	if !apiReadJSON(w, r, &body) {
		return
	}
	body.Name = r.PathValue("name")
	if !s.auditDone(w, r, "package.edit", packageTarget(body.Name), s.apiAccounts().EditPackage(r.Context(), body)) {
		return
	}
	s.packageRow(w, r, body.Name)
}

func (s *Server) handlePackageDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !s.auditDone(w, r, "package.rm", packageTarget(name), s.apiAccounts().DeletePackage(r.Context(), name)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- tạm khoá site, DB của site

// Sau job tạm khoá hay mở khoá, health kiểm lại riêng site đó (kick domain) để
// bản đồ tuyến đổi sang dòng xám (hoặc về bình thường) ngay.
//
// SuspendRequest.Reason chỉ có chỗ lưu cho user (suspend_reason); site và DB
// không có cột lý do, nên với chúng thân được nhận cho đúng hợp đồng rồi bỏ qua.

func (s *Server) handleSiteSuspend(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	var body SuspendRequest
	if !apiReadOptionalJSON(w, r, &body) {
		return
	}
	sites := s.apiSites()
	s.planJobKick(w, r, jobOpts{action: "site.suspend", target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return sites.PrepareSuspend(ctx, d)
	})
}

func (s *Server) handleSiteUnsuspend(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	sites := s.apiSites()
	s.planJobKick(w, r, jobOpts{action: "site.unsuspend", target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return sites.PrepareUnsuspend(ctx, d)
	})
}

func (s *Server) handleSiteDBSuspend(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	var body SuspendRequest
	if !apiReadOptionalJSON(w, r, &body) {
		return
	}
	dbs := s.apiDBs()
	s.planJobKick(w, r, jobOpts{action: "db.suspend", target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareSiteDBSuspend(ctx, d)
	})
}

func (s *Server) handleSiteDBUnsuspend(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	dbs := s.apiDBs()
	s.planJobKick(w, r, jobOpts{action: "db.unsuspend", target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareSiteDBUnsuspend(ctx, d)
	})
}

func (s *Server) handleSiteDBCharset(w http.ResponseWriter, r *http.Request) {
	d, ok := dbDomain(w, r)
	if !ok {
		return
	}
	var body DBCharset
	if !apiReadJSON(w, r, &body) {
		return
	}
	dbs := s.apiDBs()
	s.planJob(w, r, "db.charset", d.String(), func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareSiteDBCharset(ctx, d, body.Charset, body.Collation)
	})
}

// ---------------------------------------------------------------- DB rời

func (s *Server) handleCollations(w http.ResponseWriter, r *http.Request) {
	list, err := s.apiDBs().Collations(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	out := make([]Collation, len(list))
	for i, c := range list {
		out[i] = Collation{Charset: c.Charset, Collation: c.Collation, Default: c.Default}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDBCreate tạo DB rời "<chủ>_<tên>". Chủ: user thường luôn là chính mình
// (gửi tên khác thì 403); admin rỗng là chính mình. Mật khẩu ở JobDetail.DBSecret.
func (s *Server) handleDBCreate(w http.ResponseWriter, r *http.Request) {
	var body DBCreate
	if !apiReadJSON(w, r, &body) {
		return
	}
	id := who(r)
	owner := body.Owner
	switch {
	case !id.IsAdmin() && owner != "" && owner != id.Name:
		if !apiDryRun(r) {
			s.audit(r, "dbs.add", "?", "forbidden", "")
		}
		writeOwnerForbidden(w)
		return
	case !id.IsAdmin(), owner == "":
		owner = id.Name
	}
	if owner == "" {
		owner = account.AdminName // dòng lệnh, MCP: root là admin
	}
	on, err := validate.ParseUserName(owner)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	full, err := validate.StandaloneDBName(on, body.Name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	in := core.DBInput{Owner: on, Name: body.Name, Charset: body.Charset, Collation: body.Collation, Note: body.Note}
	dbs := s.apiDBs()
	s.secretJob(w, r, jobOpts{action: "dbs.add", target: full}, func(ctx context.Context) (*core.Planned, *DBSecret, error) {
		add, err := dbs.PrepareAdd(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return add.Planned, &DBSecret{DB: add.DB.Name, User: add.DB.Name, Password: add.Password.Reveal()}, nil
	})
}

// standaloneRow đọc một dòng DB rời như màn Cơ sở dữ liệu thấy.
func (s *Server) standaloneRow(ctx context.Context, name string) (DatabaseRow, error) {
	db, ok, err := s.deps.Store.StandaloneDB(ctx, name)
	if err != nil {
		return DatabaseRow{}, err
	}
	if !ok {
		return DatabaseRow{}, dbNotFound(name)
	}
	list, err := s.apiDBs().List(ctx, db.Owner)
	if err != nil {
		return DatabaseRow{}, err
	}
	for _, d := range list {
		if d.Kind == core.DBKindStandalone && d.DB == name {
			return databaseRow(d), nil
		}
	}
	return DatabaseRow{}, dbNotFound(name)
}

// handleDBEdit sửa DB rời. Chỉ ghi chú: ghi ngay, trả 200 DatabaseRow, không
// có job (xem trước thì không ghi gì). Có charset: job (ALTER DATABASE); ghi
// chú gửi kèm được ghi trước khi chạy job.
func (s *Server) handleDBEdit(w http.ResponseWriter, r *http.Request) {
	name, target := pathDBName(r)
	var body DBEdit
	if !apiReadJSON(w, r, &body) {
		return
	}
	if (body.Charset == nil) != (body.Collation == nil) {
		apiFail(w, r, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
			"đổi charset thì gửi cả collation (và ngược lại)", "send both charset and collation to change either")})
		return
	}
	if body.Charset == nil && body.Note == nil {
		apiFail(w, r, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
			"không có gì để đổi: gửi note, hoặc charset cùng collation", "nothing to change: send note, or charset with collation")})
		return
	}
	dbs := s.apiDBs()
	if body.Note != nil && !apiDryRun(r) {
		if !s.auditDone(w, r, "dbs.note", target, dbs.SetNote(r.Context(), name, *body.Note)) {
			return
		}
	}
	if body.Charset == nil {
		row, err := s.standaloneRow(r.Context(), name)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
		return
	}
	cs, coll := *body.Charset, *body.Collation
	s.planJob(w, r, "dbs.charset", target, func(ctx context.Context) (apiPlan, error) {
		return dbs.PrepareCharset(ctx, name, cs, coll)
	})
}

func (s *Server) handleStandaloneDBPassword(w http.ResponseWriter, r *http.Request) {
	name, target := pathDBName(r)
	dbs := s.apiDBs()
	s.secretJob(w, r, jobOpts{action: "dbs.passwd", target: target}, func(ctx context.Context) (*core.Planned, *DBSecret, error) {
		p, err := dbs.PreparePassword(ctx, name)
		if err != nil {
			return nil, nil, err
		}
		return p.Planned, &DBSecret{DB: p.Name, User: p.Name, Password: p.Password.Reveal()}, nil
	})
}

// dbJob là khuôn chung của các thao tác một Plan trên DB rời {name}.
func (s *Server) dbJob(w http.ResponseWriter, r *http.Request, action string, prepare func(d *core.DBs, ctx context.Context, name string) (*core.Planned, error)) {
	name, target := pathDBName(r)
	dbs := s.apiDBs()
	s.planJob(w, r, action, target, func(ctx context.Context) (apiPlan, error) {
		return prepare(dbs, ctx, name)
	})
}

func (s *Server) handleDBSuspend(w http.ResponseWriter, r *http.Request) {
	var body SuspendRequest
	if !apiReadOptionalJSON(w, r, &body) {
		return
	}
	s.dbJob(w, r, "dbs.suspend", (*core.DBs).PrepareSuspend)
}

func (s *Server) handleDBUnsuspend(w http.ResponseWriter, r *http.Request) {
	s.dbJob(w, r, "dbs.unsuspend", (*core.DBs).PrepareUnsuspend)
}

// handleDBTrash: xuất .sql vào thùng rác rồi DROP (core.DBs.PrepareTrash).
func (s *Server) handleDBTrash(w http.ResponseWriter, r *http.Request) {
	s.dbJob(w, r, "dbs.trash", (*core.DBs).PrepareTrash)
}

func (s *Server) handleDBRestore(w http.ResponseWriter, r *http.Request) {
	s.dbJob(w, r, "dbs.restore", (*core.DBs).PrepareRestore)
}

// handleDBPurge xoá hẳn DB rời đang ở thùng rác (không hoàn tác được).
func (s *Server) handleDBPurge(w http.ResponseWriter, r *http.Request) {
	s.dbJob(w, r, "dbs.purge", (*core.DBs).PreparePurge)
}

// ---------------------------------------------------------------- đo dung lượng

// diskEvery: mục 6.12, dung lượng của từng user đo mỗi giờ.
const diskEvery = time.Hour

// diskTimer đo dung lượng (core.Accounts.MeasureDisk) sau một vòng health,
// tối đa mỗi diskEvery một lần. Gắn vào Scheduler.OnReport (server.go): vòng
// đầu ngay khi serve khởi động là vòng đầy đủ, rồi cứ hết một giờ thì vòng
// health kế tiếp đo. Lỗi chỉ ghi log: lần đo lỗi giữ số cũ (core).
type diskTimer struct {
	mu   sync.Mutex
	last time.Time
}

func (t *diskTimer) after(ctx context.Context, s *Server) {
	now := s.now()
	t.mu.Lock()
	if !t.last.IsZero() && now.Sub(t.last) < diskEvery {
		t.mu.Unlock()
		return
	}
	t.last = now
	t.mu.Unlock()
	if err := s.apiAccounts().MeasureDisk(ctx); err != nil {
		slog.Warn("đo dung lượng của user", "err", err)
	}
}
