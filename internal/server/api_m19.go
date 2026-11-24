package server

// API của M19 (docs/thay-doi/M19-thiet-ke.md mục 3.7; kiểu ở types_m19.go,
// luật ở routes.go): tên phụ của site, luật chuyển hướng và màn "Tên miền".
//
// Mọi POST/DELETE đi qua planJobKick như các Plan khác (?dry_run=1 trả bản
// xem trước, không thì 202 kèm job). core.Sites dựng cho M19 luôn mang
// NotAdmin theo vai của phiên: luật tên miền gốc và public suffix chỉ miễn
// cho admin (S1).

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// Mã lỗi riêng của tầng HTTP M19.
const (
	// codeNameUnavailable: lời chung cho user thường khi tên đã thuộc site
	// khác hay chung tên miền gốc với site của người khác (S1). Không nói tên
	// thuộc site nào, chủ nào.
	codeNameUnavailable = "name_unavailable"
	codeAliasNotFound   = "alias_not_found"
	codeAliasModeBad    = core.CodeNameModeInvalid
)

// m19Sites là apiSites với NotAdmin theo vai của người gọi.
func (s *Server) m19Sites(r *http.Request) *core.Sites {
	ss := s.apiSites()
	ss.NotAdmin = !who(r).IsAdmin()
	return ss
}

// nameRefusal dịch lời từ chối về tên cho người gọi. User thường không bao giờ
// thấy name_taken hay name_root_foreign (kèm tên site, chủ khác trong lời):
// mọi kiểu trùng với đồ của người khác thành một câu chung name_unavailable.
// Lớp này đứng sau core (core đã biết NotAdmin) để lời lộ thông tin không lọt
// ra kể cả khi core trả *site.NameError chưa dịch hay lời có tên người khác.
// *site.NameError chưa dịch cũng thành RefusedError (không thì apiErrorStatus
// coi là 500).
func nameRefusal(err error, admin bool) error {
	if err == nil {
		return nil
	}
	var ne *site.NameError
	var re *core.RefusedError
	var code string
	switch {
	case errors.As(err, &ne):
		code = ne.Code
	case errors.As(err, &re):
		code = re.Code
	default:
		return err
	}
	if !admin && (code == site.CodeNameTaken || code == site.CodeNameRootForeign || code == codeNameUnavailable) {
		return &core.RefusedError{Code: codeNameUnavailable, Message: i18n.T(
			"Tên này không dùng được: nó đã thuộc một site khác, hoặc cùng tên miền gốc với site của người khác.",
			"This name is not available: it already belongs to another site, or shares its registered domain with someone else's site.")}
	}
	if ne == nil {
		return err
	}
	n := ne.Name.String()
	var vi, en string
	switch ne.Code {
	case site.CodeNamePublicSuffix:
		// validate.Error để ra 400 (lỗi đầu vào), như mọi lỗi domain khác.
		return &validate.Error{Code: ne.Code, Message: i18n.T(
			n+" là hậu tố công cộng (như com, co.uk), không gắn được cho một site",
			n+" is a public suffix (like com, co.uk) and cannot be given to a site")}
	case site.CodeNameReserved:
		return &validate.Error{Code: ne.Code, Message: i18n.T(
			n+" là tên dành riêng, không gắn được cho một site", n+" is a reserved name and cannot be given to a site")}
	case site.CodeAliasExists:
		vi, en = n+" đã là tên phụ của site này", n+" is already a secondary name of this site"
	case site.CodeNameTakenSelf:
		// Site kia cũng của người gọi (Other chỉ điền ở mã này): nêu tên được.
		vi, en = n+" đang là tên của site "+ne.Other.String()+" (cũng của bạn); gỡ nó ở đó trước",
			n+" is a name of the site "+ne.Other.String()+" (also yours); remove it there first"
	case site.CodeNameRootForeign:
		vi, en = n+" cùng tên miền gốc với site của một user khác", n+" shares its registered domain with another user's site"
	default: // name_taken và mã chưa biết: không chép Other.
		vi, en = n+" đã là tên của một site khác", n+" is already a name of another site"
	}
	return &core.RefusedError{Code: ne.Code, Message: i18n.T(vi, en)}
}

// namesPlan bọc *core.NamesOp cho planJobKick: Execute của NamesOp trả
// NamesResult, planJobKick cần engine.Report. Preview lấy thẳng từ NamesOp
// (nhúng), để bộ quét B12 của M28 thấy mọi bản xem trước đi qua writePreview
// của planJobKick. Lời từ chối về tên nằm ở Prepare (đã dịch trong namesJob);
// Execute dịch lại vì ClaimNames trong transaction có thể từ chối khi đua.
type namesPlan struct {
	*core.NamesOp
	admin bool
}

// ponytail: FollowUpJob (S14) chưa tới được GET /jobs/{id}: jobResult
// (api_job.go) chưa có chỗ cho nó. Job site.ssl chạy tiếp vẫn có dòng riêng
// trong /jobs. Nâng cấp: thêm trường vào jobResult và JobView.
func (p namesPlan) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	res, err := p.NamesOp.Execute(ctx, onEvent)
	return res.Report, nameRefusal(err, p.admin)
}

// namesJob chạy một thao tác tên trên site d theo khuôn planJobKick.
func (s *Server) namesJob(w http.ResponseWriter, r *http.Request, action string, d validate.Domain,
	prepare func(ctx context.Context, ss *core.Sites) (*core.NamesOp, error)) {
	ss := s.m19Sites(r)
	admin := !ss.NotAdmin
	s.planJobKick(w, r, jobOpts{action: action, target: d.String(), kick: d.String()}, func(ctx context.Context) (apiPlan, error) {
		op, err := prepare(ctx, ss)
		if err != nil {
			return nil, nameRefusal(err, admin) // không trả *NamesOp nil bọc trong interface
		}
		return namesPlan{NamesOp: op, admin: admin}, nil
	})
}

// pathSiteDomain đọc {domain}.
func pathSiteDomain(w http.ResponseWriter, r *http.Request) (validate.Domain, bool) {
	d, err := validate.ParseDomain(r.PathValue("domain"))
	if err != nil {
		apiFail(w, r, err)
		return validate.Domain{}, false
	}
	return d, true
}

// pathAliasName đọc {name}: sai dạng là "không có tên phụ này", không in lại
// chuỗi lạ.
func pathAliasName(w http.ResponseWriter, r *http.Request) (validate.Domain, bool) {
	n, err := validate.ParseDomain(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, codeAliasNotFound, i18n.T(
			"Site không có tên phụ này.", "The site has no such secondary name."))
		return validate.Domain{}, false
	}
	return n, true
}

// parseAliasMode đọc kiểu phục vụ; rỗng là def. Sai thì ghi 400 (không in lại
// giá trị) và trả false.
func parseAliasMode(w http.ResponseWriter, s string, def site.NameMode) (site.NameMode, bool) {
	switch m := site.NameMode(s); {
	case s == "" && def != "":
		return def, true
	case m == site.ModeServe || m == site.ModeRedirect:
		return m, true
	}
	writeError(w, http.StatusBadRequest, codeAliasModeBad, i18n.T(
		"Kiểu của tên phụ phải là một trong: redirect, serve.",
		"The secondary name mode must be one of: redirect, serve."))
	return "", false
}

// handleSiteNames: GET /sites/{domain}/names, tên chính và tên phụ kèm DNS và
// chứng chỉ đọc lúc gọi.
func (s *Server) handleSiteNames(w http.ResponseWriter, r *http.Request) {
	d, ok := pathSiteDomain(w, r)
	if !ok {
		return
	}
	list, err := s.m19Sites(r).NameList(r.Context(), d)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(list))
}

// handleAliasAdd: POST /sites/{domain}/names.
func (s *Server) handleAliasAdd(w http.ResponseWriter, r *http.Request) {
	d, ok := pathSiteDomain(w, r)
	if !ok {
		return
	}
	var body AliasAdd
	if !apiReadJSON(w, r, &body) {
		return
	}
	n, err := validate.ParseDomain(body.Name)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	m, ok := parseAliasMode(w, body.Mode, site.ModeRedirect)
	if !ok {
		return
	}
	s.namesJob(w, r, "site.alias.add", d, func(ctx context.Context, ss *core.Sites) (*core.NamesOp, error) {
		return ss.PrepareAliasAdd(ctx, d, n, m)
	})
}

// handleAliasMode: POST /sites/{domain}/names/{name}/mode.
func (s *Server) handleAliasMode(w http.ResponseWriter, r *http.Request) {
	d, ok := pathSiteDomain(w, r)
	if !ok {
		return
	}
	n, ok := pathAliasName(w, r)
	if !ok {
		return
	}
	var body AliasMode
	if !apiReadJSON(w, r, &body) {
		return
	}
	m, ok := parseAliasMode(w, body.Mode, "")
	if !ok {
		return
	}
	s.namesJob(w, r, "site.alias.mode", d, func(ctx context.Context, ss *core.Sites) (*core.NamesOp, error) {
		return ss.PrepareAliasMode(ctx, d, n, m)
	})
}

// handleAliasRemove: DELETE /sites/{domain}/names/{name}.
func (s *Server) handleAliasRemove(w http.ResponseWriter, r *http.Request) {
	d, ok := pathSiteDomain(w, r)
	if !ok {
		return
	}
	n, ok := pathAliasName(w, r)
	if !ok {
		return
	}
	s.namesJob(w, r, "site.alias.rm", d, func(ctx context.Context, ss *core.Sites) (*core.NamesOp, error) {
		return ss.PrepareAliasRemove(ctx, d, n)
	})
}

// fieldRedirectCode: tên trường "code" trong lỗi của luật chuyển hướng
// (validate chỉ khai source và target, vì chỉ hai trường đó là chuỗi).
const fieldRedirectCode = "code"

// redirectInput kiểm thân POST .../redirects ở biên HTTP: nguồn, đích, mã (rỗng
// là 301). Lỗi là *validate.RedirectError nên apiFail trả 400 kèm "field".
// Core kiểm lại (luật cần biết site: vòng lặp, trùng, trần 50).
func redirectInput(b RedirectAdd) (core.RedirectInput, error) {
	src, err := validate.ParseRedirectSource(b.Source)
	if err != nil {
		return core.RedirectInput{}, err
	}
	tgt, err := validate.ParseRedirectTarget(b.Target)
	if err != nil {
		return core.RedirectInput{}, err
	}
	code := b.Code
	if code == 0 {
		code = 301
	}
	if code != 301 && code != 302 {
		return core.RedirectInput{}, &validate.RedirectError{Field: fieldRedirectCode, Err: &validate.Error{
			Code: validate.CodeRedirectInvalid,
			Message: i18n.T("mã chuyển hướng phải là 301 hoặc 302",
				"the redirect code must be 301 or 302")}}
	}
	return core.RedirectInput{Source: src.String(), Target: tgt.String(), Code: code, KeepQuery: b.KeepQuery}, nil
}

// handleRedirectList: GET /sites/{domain}/redirects.
func (s *Server) handleRedirectList(w http.ResponseWriter, r *http.Request) {
	d, ok := pathSiteDomain(w, r)
	if !ok {
		return
	}
	rules, err := s.m19Sites(r).RedirectList(r.Context(), d)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	out := make([]RedirectView, 0, len(rules))
	for _, x := range rules {
		out = append(out, RedirectView{ID: x.ID, Source: x.Source.String(), Target: x.Target.String(),
			Code: x.Code, KeepQuery: x.KeepQuery, State: "active"})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRedirectAdd: POST /sites/{domain}/redirects.
func (s *Server) handleRedirectAdd(w http.ResponseWriter, r *http.Request) {
	d, ok := pathSiteDomain(w, r)
	if !ok {
		return
	}
	var body RedirectAdd
	if !apiReadJSON(w, r, &body) {
		return
	}
	in, err := redirectInput(body)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	s.namesJob(w, r, "site.redirect.add", d, func(ctx context.Context, ss *core.Sites) (*core.NamesOp, error) {
		return ss.PrepareRedirectAdd(ctx, d, in)
	})
}

// handleRedirectRemove: DELETE /sites/{domain}/redirects/{id}. id không phải số
// dương là "không có luật này", không in lại chuỗi lạ.
func (s *Server) handleRedirectRemove(w http.ResponseWriter, r *http.Request) {
	d, ok := pathSiteDomain(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "redirect_not_found", i18n.T(
			"Site không có luật chuyển hướng này.", "The site has no such redirect rule."))
		return
	}
	s.namesJob(w, r, "site.redirect.rm", d, func(ctx context.Context, ss *core.Sites) (*core.NamesOp, error) {
		return ss.PrepareRedirectRemove(ctx, d, id)
	})
}

// handleDomains: GET /domains, mọi site trong phạm vi người xem kèm tên phụ
// active. Đọc từ bảng (site.Site.Names), không tra DNS: mỗi tên tra một lần
// tới 2 giây (S18), cả máy thì màn này treo.
func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Store.SitesOf(r.Context(), scopeOf(r))
	if err != nil {
		apiFail(w, r, err)
		return
	}
	out := make([]DomainRow, 0, len(list))
	for _, st := range list {
		names := make([]string, 0, len(st.Names.Serve)+len(st.Names.Redirect))
		for _, n := range slices.Concat(st.Names.Serve, st.Names.Redirect) {
			names = append(names, n.String())
		}
		out = append(out, DomainRow{Site: st.Domain.String(), Names: names, Redirects: len(st.Names.Rules)})
	}
	writeJSON(w, http.StatusOK, out)
}
