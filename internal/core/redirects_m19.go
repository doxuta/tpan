package core

// M19 đợt 2: luật chuyển hướng theo đường dẫn của site (docs/thay-doi/
// M19-thiet-ke.md mục 3.3, 3.5; S12, S15, S16, S20 của "Trả lời soát lượt 1").
// Hai Plan site.redirect.add / rm cùng khuôn với tên phụ (names_m19.go):
// Prepare chỉ đọc; Execute giữ chỗ (AddRedirectPending) → job (vhost vẽ từ
// st.Names.Rules active cộng đúng luật này, rồi RedirectProbe) → Activate khi
// job thành công, Release khi hỏng.
//
// Kiểm ký tự của nguồn và đích ở validate (danh sách cho phép). Ở đây là luật
// cần biết site: vòng lặp, trùng, giới hạn, giữ query khi đích đã có query.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/validate"
)

// Mã từ chối của luật chuyển hướng (mục 3.7). redirect_invalid và
// redirect_reserved ở validate.
const (
	CodeRedirectLoop          = "redirect_loop"
	CodeRedirectQueryConflict = "redirect_query_conflict"
	CodeRedirectExists        = "redirect_exists"
	CodeRedirectLimit         = "redirect_limit"
	CodeRedirectNotFound      = "redirect_not_found"
)

const (
	opRedirectAdd namesOpKind = "site.redirect.add"
	opRedirectRm  namesOpKind = "site.redirect.rm"
)

// redirectSite tìm site và áp điều kiện chung của mọi thao tác ghi luật (S12):
// store giữ được luật, site active, không tạm khoá.
func (s *Sites) redirectSite(ctx context.Context, d validate.Domain) (site.Site, RedirectStore, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return site.Site{}, nil, err
	}
	rs, ok := s.Store.(RedirectStore)
	if !ok {
		return site.Site{}, nil, refuse(CodeNamesUnsupported, i18n.T(
			"store không giữ được luật chuyển hướng (chế độ chỉ xem hay bản thử)",
			"the store cannot hold redirect rules (read-only or demo mode)"))
	}
	ds := d.String()
	if st.Status != site.Active {
		return site.Site{}, nil, refuse(CodeSiteNotActive, i18n.T(
			"site "+ds+" đang ở thùng rác; khôi phục nó trước rồi mới đổi luật chuyển hướng",
			"site "+ds+" is in the trash; restore it before changing its redirect rules"))
	}
	if st.Suspended != account.NotSuspended {
		return site.Site{}, nil, refuse(CodeSiteSuspended, i18n.T(
			"site "+ds+" đang tạm khoá; mở khoá trước rồi mới đổi luật chuyển hướng: sudo tpan site unsuspend "+ds,
			"site "+ds+" is suspended; unsuspend it before changing its redirect rules: sudo tpan site unsuspend "+ds))
	}
	return st, rs, nil
}

// sitePath trả phần đường dẫn (bỏ query) của đích t khi t trỏ về chính site
// st: đích dạng đường dẫn, hoặc URL có host là tên chính hay một tên phụ
// active (cổng bỏ qua: http://example.com:80/x vẫn là site này; coi rộng ra là
// từ chối thừa chứ không lọt vòng lặp). ok=false: đích ở chỗ khác.
func sitePath(st site.Site, t validate.RedirectTarget) (string, bool) {
	s := t.String()
	if t.IsAbsolute() {
		_, rest, _ := strings.Cut(s, "://")
		host, path := rest, "/"
		if i := strings.IndexAny(rest, "/?"); i >= 0 {
			host, path = rest[:i], rest[i:]
		}
		host, _, _ = strings.Cut(host, ":")
		own := host == st.Domain.String() ||
			slices.ContainsFunc(slices.Concat(st.Names.Serve, st.Names.Redirect), func(n validate.Domain) bool { return n.String() == host })
		if !own {
			return "", false
		}
		s = path
	}
	p, _, _ := strings.Cut(s, "?")
	if p == "" {
		p = "/"
	}
	return p, true
}

// checkRedirect áp luật cần biết site (mục 3.3) cho luật mới r trên các luật
// active rules của st: giữ query khi đích có query; đích về chính nguồn (S15);
// nguồn đã có luật; hai luật chuyển qua lại (S20); đủ MaxRedirects.
func checkRedirect(st site.Site, rules []site.Redirect, r site.Redirect) error {
	src := r.Source.String()
	if r.KeepQuery && r.Target.HasQuery() {
		return refuse(CodeRedirectQueryConflict, i18n.T(
			"đích đã có query (?…); bỏ --keep-query, hoặc bỏ query khỏi đích",
			"the target already has a query (?…); drop --keep-query, or drop the query from the target"))
	}
	loop := func(why string, whyEn string) error {
		return refuse(CodeRedirectLoop, i18n.T(
			"luật "+src+" → "+r.Target.String()+" tạo vòng chuyển hướng: "+why,
			"the rule "+src+" → "+r.Target.String()+" makes a redirect loop: "+whyEn))
	}
	p, own := sitePath(st, r.Target)
	if own && p == src {
		return loop("đích là chính nguồn trên site này", "the target is the source itself on this site")
	}
	for _, x := range rules {
		if x.Source == r.Source {
			return refuse(CodeRedirectExists, i18n.T(
				fmt.Sprintf("site %s đã có luật cho nguồn %s (số %d); gỡ nó trước: tpan site redirect rm %s %d", st.Domain, src, x.ID, st.Domain, x.ID),
				fmt.Sprintf("site %s already has a rule for the source %s (id %d); remove it first: tpan site redirect rm %s %d", st.Domain, src, x.ID, st.Domain, x.ID)))
		}
		if xp, xown := sitePath(st, x.Target); own && xown && p == x.Source.String() && xp == src {
			return loop(
				fmt.Sprintf("luật số %d đang chuyển %s về %s", x.ID, x.Source, src),
				fmt.Sprintf("rule %d already redirects %s to %s", x.ID, x.Source, src))
		}
	}
	if len(rules) >= site.MaxRedirects {
		return refuse(CodeRedirectLimit, i18n.T(
			fmt.Sprintf("site %s đã có %d luật chuyển hướng (tối đa %d)", st.Domain, len(rules), site.MaxRedirects),
			fmt.Sprintf("site %s already has %d redirect rules (at most %d)", st.Domain, len(rules), site.MaxRedirects)))
	}
	return nil
}

// redirectLocation là Location mà vhost trả cho luật r (S16): đích dạng đường
// dẫn thành <scheme>://<tên chính><đích>, scheme theo site có SSL hay không.
func redirectLocation(st site.Site, t validate.RedirectTarget) string {
	if t.IsAbsolute() {
		return t.String()
	}
	scheme := "http://"
	if st.SSL {
		scheme = "https://"
	}
	return scheme + st.Domain.String() + t.String()
}

// PrepareRedirectAdd: site.redirect.add (mục 3.5). Nguồn hay đích sai trả
// *validate.RedirectError nguyên (API trả 400 kèm field).
func (s *Sites) PrepareRedirectAdd(ctx context.Context, siteD validate.Domain, in RedirectInput) (*NamesOp, error) {
	st, rs, err := s.redirectSite(ctx, siteD)
	if err != nil {
		return nil, err
	}
	src, err := validate.ParseRedirectSource(in.Source)
	if err != nil {
		return nil, err
	}
	tgt, err := validate.ParseRedirectTarget(in.Target)
	if err != nil {
		return nil, err
	}
	if in.Code != 301 && in.Code != 302 {
		return nil, &validate.RedirectError{Field: "code", Err: &validate.Error{Code: validate.CodeRedirectInvalid, Message: i18n.T(
			"mã chuyển hướng phải là 301 (vĩnh viễn) hoặc 302 (tạm thời)",
			"the redirect code must be 301 (permanent) or 302 (temporary)")}}
	}
	r := site.Redirect{Source: src, Target: tgt, Code: in.Code, KeepQuery: in.KeepQuery}
	if err := checkRedirect(st, st.Names.Rules, r); err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	next := st
	next.Names.Rules = append(slices.Clone(st.Names.Rules), r)
	op := &NamesOp{sites: s, op: opRedirectAdd, site: st, next: next, rs: rs, rule: r}
	probe := &steps.RedirectProbe{Layout: st.Layout(), SSL: st.SSL, Source: src, Code: r.Code, Location: redirectLocation(st, tgt)}
	return op, op.build(ctx, cfg, nil, probe, i18n.T(
		"Thêm luật chuyển hướng "+src.String()+" → "+tgt.String()+" cho "+siteD.String(),
		"Add redirect "+src.String()+" → "+tgt.String()+" to "+siteD.String()))
}

// PrepareRedirectRemove: site.redirect.rm. Chỉ nhận luật active (luật pending
// là của job đang chạy, hay mồ côi: chạy lại đúng lệnh add thì nhận lại, không
// thì hết hạn sau 1 giờ). Bước kiểm: tên chính vẫn trả trang WordPress.
func (s *Sites) PrepareRedirectRemove(ctx context.Context, siteD validate.Domain, id int64) (*NamesOp, error) {
	st, rs, err := s.redirectSite(ctx, siteD)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(st.Names.Rules, func(r site.Redirect) bool { return r.ID == id })
	if i < 0 {
		return nil, refuse(CodeRedirectNotFound, i18n.T(
			fmt.Sprintf("site %s không có luật chuyển hướng số %d; xem danh sách: tpan site redirect list %s", siteD, id, siteD),
			fmt.Sprintf("site %s has no redirect rule %d; list them with: tpan site redirect list %s", siteD, id, siteD)))
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	r := st.Names.Rules[i]
	next := st
	next.Names.Rules = slices.Delete(slices.Clone(st.Names.Rules), i, i+1)
	op := &NamesOp{sites: s, op: opRedirectRm, site: st, next: next, rs: rs, rule: r}
	probe := &steps.NameProbe{Layout: st.Layout(), Host: st.Domain, Mode: site.ModeServe, SSL: st.SSL, Covered: st.Names.Covered}
	return op, op.build(ctx, cfg, nil, probe, i18n.T(
		fmt.Sprintf("Gỡ luật chuyển hướng số %d (%s) khỏi %s", id, r.Source, siteD),
		fmt.Sprintf("Remove redirect %d (%s) from %s", id, r.Source, siteD)))
}

// RedirectList trả các luật active của site (đúng các luật đang có trong
// vhost). Không đổi gì; quyền xem do server kiểm (owner:site).
func (s *Sites) RedirectList(ctx context.Context, siteD validate.Domain) ([]site.Redirect, error) {
	if _, err := s.find(ctx, siteD); err != nil {
		return nil, err
	}
	rs, ok := s.Store.(RedirectStore)
	if !ok {
		return nil, refuse(CodeNamesUnsupported, i18n.T(
			"store không giữ được luật chuyển hướng (chế độ chỉ xem hay bản thử)",
			"the store cannot hold redirect rules (read-only or demo mode)"))
	}
	return rs.RedirectsOf(ctx, siteD)
}

// redirectRefusal dịch lời từ chối của AddRedirectPending (kiểm lại trong
// transaction, khi một request khác chen giữa Prepare và Execute).
func redirectRefusal(err error) error {
	var ve *validate.Error
	if errors.As(err, &ve) && (ve.Code == CodeRedirectExists || ve.Code == CodeRedirectLimit) {
		return refuse(ve.Code, ve.Message)
	}
	return err
}

// executeRedirect là Execute của site.redirect.add/rm: giữ chỗ (add), chạy
// job, rồi Activate/Delete hoặc Release. NamesOp.Execute (names_m19.go) gọi nó
// khi o.rs khác nil.
func (o *NamesOp) executeRedirect(ctx context.Context, onEvent func(engine.Event)) (NamesResult, error) {
	s := o.sites
	var res NamesResult
	if s.Journal == nil {
		return res, fmt.Errorf("%s: thiếu Journal, không ghi được lịch sử job", o.op)
	}
	d, add := o.site.Domain, o.op == opRedirectAdd
	if add {
		id, err := o.rs.AddRedirectPending(ctx, d, o.rule)
		if err != nil {
			return res, redirectRefusal(err)
		}
		o.rule.ID = id
	}
	release := func(cause error) error {
		if !add {
			return cause
		}
		if err := o.rs.ReleaseRedirect(ctx, o.rule.ID); err != nil {
			return errors.Join(cause, fmt.Errorf("nhả chỗ giữ luật %s: %w (dòng pending tự hết hạn sau 1 giờ)", o.rule.Source, err))
		}
		return cause
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(engine.WithOwner(ctx, ownerOf(o.site)), o.plan)
	res.Report = rep
	if err != nil {
		return res, release(fmt.Errorf("chạy %s: %w", o.plan.Kind, err))
	}
	if rep.Job.Status != engine.JobSucceeded {
		return res, release(nil)
	}
	var dbErr error
	if add {
		dbErr = o.rs.ActivateRedirect(ctx, o.rule.ID)
	} else {
		dbErr = o.rs.DeleteRedirect(ctx, d, o.rule.ID)
	}
	if dbErr != nil {
		return res, fmt.Errorf("job %s đã đổi vhost của %s nhưng không ghi được luật %s vào DB của tpan: %w. Chạy lại đúng lệnh này để ghi nốt, hoặc `sudo tpan health --fix` để vẽ lại vhost theo DB",
			rep.Job.ID, d, o.rule.Source, dbErr)
	}
	return res, nil
}
