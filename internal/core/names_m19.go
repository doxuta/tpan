package core

// M19 đợt 1: tên phụ của site (docs/thay-doi/M19-thiet-ke.md mục 3.2, 3.5 và
// "Trả lời soát lượt 1"). Ba Plan site.alias.add / mode / rm, cách core gọi
// NameStore, và cách dịch lời từ chối *site.NameError ra mã API.
//
// Luồng chung: Prepare chỉ đọc (CheckNames, không giữ chỗ); Execute giữ chỗ
// (alias.add) → job (vhost vẽ từ st.Names active cộng đúng thay đổi này, rồi
// NameProbe/NameGone) → Activate/SetAliasMode/DeleteAlias khi job thành công,
// ReleaseNames khi hỏng.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// WWWNone: SiteInput.WWW = "none" là không thêm tên www mặc định.
const WWWNone site.NameMode = "none"

// Mã từ chối riêng của tên phụ (mục 3.7).
const (
	CodeNameUnavailable  = "name_unavailable"
	CodeAliasLimit       = "alias_limit"
	CodeAliasPrimary     = "alias_primary"
	CodeAliasNotFound    = "alias_not_found"
	CodeSiteNotActive    = "site_not_active"
	CodeNameModeInvalid  = "name_mode_invalid"
	CodeNamesUnsupported = "names_unsupported"
)

type namesOpKind string

const (
	opAliasAdd  namesOpKind = "site.alias.add"
	opAliasMode namesOpKind = "site.alias.mode"
	opAliasRm   namesOpKind = "site.alias.rm"
)

// nameStore trả phần store giữ tên. Store giả của test cũ không cài nó.
func (s *Sites) nameStore() (NameStore, bool) {
	ns, ok := s.Store.(NameStore)
	return ns, ok
}

// nameUnavailable là câu chung cho user thường: không nói tên thuộc site nào,
// chủ nào (mục 3.2 "Không lộ thông tin").
func nameUnavailable(n validate.Domain) error {
	return refuse(CodeNameUnavailable, i18n.T(
		"tên "+n.String()+" không dùng được trên máy này: đã thuộc một site khác, hoặc tên miền gốc của nó thuộc người dùng khác",
		"the name "+n.String()+" cannot be used on this machine: it already belongs to another site, or its registered domain belongs to another user"))
}

// aliasRefusal dịch lời từ chối của CheckNames/ClaimNames ở đường tên phụ.
// Lỗi không phải *site.NameError trả nguyên.
func (s *Sites) aliasRefusal(err error) error {
	var ne *site.NameError
	if !errors.As(err, &ne) {
		return err
	}
	n := ne.Name.String()
	switch ne.Code {
	case site.CodeNamePublicSuffix:
		return refuse(ne.Code, i18n.T(
			"tên "+n+" là một đuôi công cộng (như com.vn, co.uk, github.io), không gắn được vào site",
			"the name "+n+" is a public suffix (such as com.vn, co.uk, github.io) and cannot be attached to a site"))
	case site.CodeNameReserved:
		return refuse(CodeDomainReserved, i18n.T(
			"tên "+n+" dành cho phép thử của tpan; chọn tên khác",
			"the name "+n+" is reserved for tpan's probe; choose another name"))
	case site.CodeAliasExists:
		return refuse(ne.Code, i18n.T("tên "+n+" đã là tên phụ của site này", "the name "+n+" is already an alias of this site"))
	case site.CodeNameTakenSelf:
		return refuse(ne.Code, i18n.T(
			"tên "+n+" đang thuộc site "+ne.Other.String()+" của bạn; gỡ nó ở site đó trước",
			"the name "+n+" belongs to your site "+ne.Other.String()+"; remove it there first"))
	case site.CodeNameTaken, site.CodeNameRootForeign:
		if s.NotAdmin {
			return nameUnavailable(ne.Name)
		}
		if ne.Code == site.CodeNameTaken {
			return refuse(ne.Code, i18n.T("tên "+n+" đã thuộc một site khác trên máy này", "the name "+n+" already belongs to another site on this machine"))
		}
		return refuse(ne.Code, i18n.T(
			"tên miền gốc của "+n+" đã có tên của người dùng khác (luật một tên miền gốc một chủ áp cho chủ của site)",
			"the registered domain of "+n+" already has names of another user (one registered domain, one owner applies to the site's owner)"))
	}
	return refuse(ne.Code, i18n.T("tên "+n+" không dùng được ("+ne.Code+")", "the name "+n+" cannot be used ("+ne.Code+")"))
}

// siteAddRefusal dịch lời từ chối ở đường tạo site: user thường giữ đúng lời
// 404 site_not_found và domain_invalid của M28 (P4); admin thấy mã chi tiết.
func (s *Sites) siteAddRefusal(err error) error {
	var ne *site.NameError
	if !errors.As(err, &ne) {
		return err
	}
	d := ne.Name.String()
	switch {
	case s.NotAdmin && (ne.Code == site.CodeNameTaken || ne.Code == site.CodeNameRootForeign):
		return refuse("site_not_found", i18n.T(
			"không tạo được site "+d+" từ tài khoản này: domain đã được dùng",
			"site "+d+" cannot be created from this account: the domain is already in use"))
	case s.NotAdmin && ne.Code == site.CodeNamePublicSuffix:
		return refuse(validate.CodeDomainInvalid, i18n.T(
			"tên miền "+d+" không có phần đăng ký được (nó là đuôi công cộng như com, co.uk, github.io); chỉ admin tạo được site với tên này",
			"the domain "+d+" has no registrable part (it is a public suffix such as com, co.uk, github.io); only the admin can create a site with this name"))
	}
	return s.aliasRefusal(err)
}

// checkClaimsLocal là đường của store giả (không cài NameStore): áp CheckClaims
// trên ảnh chụp dựng từ bảng sites, không ghi gì (mục 3.5 "Giao diện store").
func (s *Sites) checkClaimsLocal(ctx context.Context, cs []site.Claim) error {
	all, err := s.Store.Sites(ctx)
	if err != nil {
		return err
	}
	rows := make([]site.Name, 0, len(all))
	for _, st := range all {
		rows = append(rows, site.Name{Name: st.Domain, Site: st.Domain, Owner: ownerOf(st),
			Kind: site.KindPrimary, Mode: site.ModeServe, State: site.StateActive, Created: st.Created})
	}
	return site.CheckClaims(cs, rows)
}

// namesSite tìm site và áp điều kiện chung của mọi thao tác tên (S12).
func (s *Sites) namesSite(ctx context.Context, d validate.Domain) (site.Site, NameStore, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return site.Site{}, nil, err
	}
	ns, ok := s.nameStore()
	if !ok {
		return site.Site{}, nil, refuse(CodeNamesUnsupported, i18n.T(
			"store không giữ được tên phụ (chế độ chỉ xem hay bản thử)",
			"the store cannot hold aliases (read-only or demo mode)"))
	}
	ds := d.String()
	if st.Status != site.Active {
		return site.Site{}, nil, refuse(CodeSiteNotActive, i18n.T(
			"site "+ds+" đang ở thùng rác; khôi phục nó trước, hoặc xoá hẳn để nhả các tên của nó",
			"site "+ds+" is in the trash; restore it first, or purge it to release its names"))
	}
	if st.Suspended != account.NotSuspended {
		return site.Site{}, nil, refuse(CodeSiteSuspended, i18n.T(
			"site "+ds+" đang tạm khoá; mở khoá trước rồi mới đổi tên phụ: sudo tpan site unsuspend "+ds,
			"site "+ds+" is suspended; unsuspend it before changing its aliases: sudo tpan site unsuspend "+ds))
	}
	return st, ns, nil
}

func checkNameMode(m site.NameMode) error {
	if m == site.ModeServe || m == site.ModeRedirect {
		return nil
	}
	return refuse(CodeNameModeInvalid, i18n.T(
		"kiểu của tên phụ phải là serve hoặc redirect (nhận \""+string(m)+"\")",
		"the alias mode must be serve or redirect (got \""+string(m)+"\")"))
}

// withName trả bản sao n có thêm name ở đúng nhóm (bỏ khỏi nhóm kia trước).
// Slice luôn mới: không chung mảng với st.Names của dòng đang có.
func withName(n site.Names, name validate.Domain, m site.NameMode) site.Names {
	out := withoutName(n, name)
	if m == site.ModeServe {
		out.Serve = append(out.Serve, name)
	} else {
		out.Redirect = append(out.Redirect, name)
	}
	return out
}

func withoutName(n site.Names, name validate.Domain) site.Names {
	drop := func(xs []validate.Domain) []validate.Domain {
		return slices.DeleteFunc(slices.Clone(xs), func(x validate.Domain) bool { return x == name })
	}
	n.Serve, n.Redirect = drop(n.Serve), drop(n.Redirect)
	n.Covered, n.Rules = slices.Clone(n.Covered), slices.Clone(n.Rules)
	return n
}

// declaredElsewhere đọc cấu hình đang nạp và trả file khai name nếu file đó
// không phải vhost của l (S13). "" là không ai khác khai.
func declaredElsewhere(ctx context.Context, sys system.System, web WebServer, l site.Layout, name validate.Domain) (string, error) {
	cmd := system.Cmd{Name: "nginx", Args: []string{"-T"}}
	own := []string{l.NginxConf, l.NginxLink}
	if web == Apache {
		cmd = system.Cmd{Name: "apache2ctl", Args: []string{"-S"}}
		own = []string{l.ApacheConf, l.ApacheLink}
	}
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("đọc cấu hình web đang nạp (%s): %w", cmd.String(), err)
	}
	dump := res.Stdout
	if web == Apache {
		dump += "\n" + res.Stderr
	}
	decl, ok := checks.DeclaredNames(string(web), dump)
	if !ok {
		return "", fmt.Errorf("đọc cấu hình web đang nạp: không hiểu output của %s", cmd.String())
	}
	if f, found := decl[name.String()]; found && !slices.Contains(own, f) {
		return f, nil
	}
	return "", nil
}

// declaredRefusal: tên đang khai ở vhost khác. User thường nhận câu chung.
func (s *Sites) declaredRefusal(name validate.Domain, file string) error {
	if s.NotAdmin {
		return nameUnavailable(name)
	}
	return refuse(site.CodeNameTaken, i18n.T(
		"tên "+name.String()+" đang được khai ở "+file+" (vhost không do site này quản); gỡ nó ở đó trước",
		"the name "+name.String()+" is declared in "+file+" (a vhost this site does not own); remove it there first"))
}

// declaredGuard là bước đầu của site.alias.add: chạy dưới khoá của engine nên
// bắt được vhost khác vừa khai tên giữa Prepare và Execute (S13). Check đạt
// (bỏ qua Apply) khi không file lạ nào khai tên; không thì Apply từ chối.
type declaredGuard struct {
	sites *Sites
	web   WebServer
	l     site.Layout
	name  validate.Domain
}

func (g *declaredGuard) Info() engine.StepInfo {
	cmd := "nginx -T"
	if g.web == Apache {
		cmd = "apache2ctl -S"
	}
	return engine.StepInfo{
		Commands: []string{cmd},
		ID:       "name.declared:" + g.name.String(),
		Title:    i18n.T("Kiểm tên "+g.name.String()+" chưa được vhost khác khai", "Check no other vhost declares "+g.name.String()),
		Why: i18n.T(
			"Hai vhost cùng khai một tên thì web server chỉ dùng vhost nạp trước; tên phụ mới có thể lặng lẽ chiếm lưu lượng của một vhost không do tpan quản.",
			"When two vhosts declare one name the web server uses only the first loaded; a new alias could silently take traffic from a vhost tpan does not manage."),
		NoUndo: i18n.T("Bước này chỉ đọc cấu hình.", "This step only reads the configuration."),
	}
}

func (g *declaredGuard) Check(ctx context.Context, sys system.System) (bool, error) {
	f, err := declaredElsewhere(ctx, sys, g.web, g.l, g.name)
	return err == nil && f == "", err
}

func (g *declaredGuard) Apply(ctx context.Context, sys system.System) error {
	f, err := declaredElsewhere(ctx, sys, g.web, g.l, g.name)
	if err != nil {
		return err
	}
	if f != "" {
		return g.sites.declaredRefusal(g.name, f)
	}
	return nil
}

func (g *declaredGuard) Undo(context.Context, system.System) error { return nil }

// PrepareAliasAdd: site.alias.add (mục 3.5).
func (s *Sites) PrepareAliasAdd(ctx context.Context, siteD, name validate.Domain, m site.NameMode) (*NamesOp, error) {
	st, ns, err := s.namesSite(ctx, siteD)
	if err != nil {
		return nil, err
	}
	if err := checkNameMode(m); err != nil {
		return nil, err
	}
	if name.IsZero() {
		return nil, refuse(validate.CodeDomainInvalid, i18n.T("thiếu tên phụ", "the alias is missing"))
	}
	if name == st.Domain {
		return nil, refuse(CodeAliasPrimary, i18n.T("tên "+name.String()+" là tên chính của site", "the name "+name.String()+" is the site's primary name"))
	}
	rows, err := ns.NamesOf(ctx, siteD)
	if err != nil {
		return nil, err
	}
	if len(rows) >= site.MaxNames {
		return nil, refuse(CodeAliasLimit, i18n.T(
			fmt.Sprintf("site %s đã có %d tên (tối đa %d, giới hạn của một chứng chỉ Let's Encrypt)", siteD, len(rows), site.MaxNames),
			fmt.Sprintf("site %s already has %d names (at most %d, the limit of one Let's Encrypt certificate)", siteD, len(rows), site.MaxNames)))
	}
	claim := site.Claim{Name: name, Site: siteD, Owner: ownerOf(st), Kind: site.KindAlias, Mode: m, ByAdmin: !s.NotAdmin}
	if err := ns.CheckNames(ctx, []site.Claim{claim}); err != nil {
		return nil, s.aliasRefusal(err)
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	if f, err := declaredElsewhere(ctx, s.Sys, cfg.Web, st.Layout(), name); err != nil {
		return nil, err
	} else if f != "" {
		return nil, s.declaredRefusal(name, f)
	}
	next := st
	next.Names = withName(st.Names, name, m)
	op := &NamesOp{sites: s, ns: ns, op: opAliasAdd, site: st, next: next, name: name, mode: m, claim: &claim,
		needsCert: st.SSL}
	guard := &declaredGuard{sites: s, web: cfg.Web, l: st.Layout(), name: name}
	return op, op.build(ctx, cfg, guard, &steps.NameProbe{Layout: st.Layout(), Host: name, Mode: m, SSL: st.SSL, Covered: st.Names.Covered},
		i18n.T("Thêm tên phụ "+name.String()+" cho "+siteD.String(), "Add alias "+name.String()+" to "+siteD.String()))
}

// aliasRow tìm dòng tên phụ name của site; tên chính: alias_primary; không có:
// alias_not_found.
func aliasRow(ctx context.Context, ns NameStore, st site.Site, name validate.Domain) (site.Name, error) {
	if name == st.Domain {
		return site.Name{}, refuse(CodeAliasPrimary, i18n.T(
			"tên "+name.String()+" là tên chính của site; tên chính không gỡ hay đổi kiểu được",
			"the name "+name.String()+" is the site's primary name; it cannot be removed or change mode"))
	}
	rows, err := ns.NamesOf(ctx, st.Domain)
	if err != nil {
		return site.Name{}, err
	}
	for _, r := range rows {
		if r.Name == name && r.Kind == site.KindAlias {
			return r, nil
		}
	}
	return site.Name{}, refuse(CodeAliasNotFound, i18n.T(
		"site "+st.Domain.String()+" không có tên phụ "+name.String(),
		"site "+st.Domain.String()+" has no alias "+name.String()))
}

// PrepareAliasMode: site.alias.mode, không giữ chỗ; xong thì SetAliasMode.
func (s *Sites) PrepareAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) (*NamesOp, error) {
	st, ns, err := s.namesSite(ctx, siteD)
	if err != nil {
		return nil, err
	}
	if err := checkNameMode(m); err != nil {
		return nil, err
	}
	row, err := aliasRow(ctx, ns, st, name)
	if err != nil {
		return nil, err
	}
	if row.State != site.StateActive {
		return nil, refuse(CodeAliasNotFound, i18n.T(
			"tên phụ "+name.String()+" còn đang được thêm (job khác chưa xong)",
			"the alias "+name.String()+" is still being added (another job has not finished)"))
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	next := st
	next.Names = withName(st.Names, name, m)
	op := &NamesOp{sites: s, ns: ns, op: opAliasMode, site: st, next: next, name: name, mode: m,
		needsCert: st.SSL && !slices.Contains(st.Names.Covered, name)}
	return op, op.build(ctx, cfg, nil, &steps.NameProbe{Layout: st.Layout(), Host: name, Mode: m, SSL: st.SSL, Covered: st.Names.Covered},
		i18n.T("Đổi kiểu tên phụ "+name.String()+" sang "+string(m), "Change alias "+name.String()+" to "+string(m)))
}

// PrepareAliasRemove: site.alias.rm. Nhận cả dòng pending (mồ côi sau một
// tiến trình chết giữa job): khi đó vhost không đổi, xong thì ReleaseNames.
func (s *Sites) PrepareAliasRemove(ctx context.Context, siteD, name validate.Domain) (*NamesOp, error) {
	st, ns, err := s.namesSite(ctx, siteD)
	if err != nil {
		return nil, err
	}
	row, err := aliasRow(ctx, ns, st, name)
	if err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	next := st
	next.Names = withoutName(st.Names, name)
	// Tên đang trong chứng chỉ: xin lại không có nó (B9), không thì lần gia hạn
	// sau hỏng vì tên đã đi.
	op := &NamesOp{sites: s, ns: ns, op: opAliasRm, site: st, next: next, name: name,
		pending: row.State != site.StateActive, needsCert: st.SSL && slices.Contains(st.Names.Covered, name)}
	next.Names.Covered = slices.DeleteFunc(next.Names.Covered, func(x validate.Domain) bool { return x == name })
	op.next = next
	return op, op.build(ctx, cfg, nil, &steps.NameGone{Layout: st.Layout(), Host: name},
		i18n.T("Gỡ tên phụ "+name.String()+" khỏi "+siteD.String(), "Remove alias "+name.String()+" from "+siteD.String()))
}

// build dựng Plan: [guard], vhost của next, bước kiểm tên; Verify là chặng
// request của tên chính (tên chính vẫn đạt sau thay đổi).
func (o *NamesOp) build(ctx context.Context, cfg StackConfig, guard engine.Step, probe engine.Step, title i18n.Text) error {
	s, st := o.sites, o.site
	vhost, err := siteVhost(o.next, cfg)
	if err != nil {
		return fmt.Errorf("dựng Plan %s cho %s: %w", o.op, st.Domain, err)
	}
	lim, err := siteCageLimits(ctx, s.Accounts, st)
	if err != nil {
		return err
	}
	var ss []engine.Step
	if guard != nil {
		ss = append(ss, guard)
	}
	ss = append(ss, vhost...)
	ss = append(ss, probe)
	o.plan = engine.Plan{
		Kind: string(o.op), Target: st.Domain.String(), Title: title, Steps: ss,
		Verify: siteRequestChecks(cfg.Web, sitePHP(st, cfg), st, lim),
	}
	if o.needsCert {
		n := i18n.T(
			"Site có HTTPS: sau job này cần chạy job site.ssl để chứng chỉ khớp các tên: "+o.certNames(),
			"The site has HTTPS: after this job a site.ssl job is needed so the certificate matches the names: "+o.certNames())
		o.sslNote = &n
	}
	return nil
}

// certNames là bộ tên chứng chỉ nên phủ sau thay đổi (tên chính + mọi tên phụ
// active của next), để ghi chú xem trước.
func (o *NamesOp) certNames() string {
	names := []string{o.next.Domain.String()}
	for _, n := range slices.Concat(o.next.Names.Serve, o.next.Names.Redirect) {
		names = append(names, n.String())
	}
	return strings.Join(names, ", ")
}

// Preview trả bản xem trước của Plan, không giữ chỗ gì. Site HTTPS: thêm một
// dòng cuối báo job site.ssl chạy tiếp (S14; engine.Preview không có ô ghi chú).
func (o *NamesOp) Preview(ctx context.Context) (engine.Preview, error) {
	pv, err := engine.New(o.sites.Sys, engine.Options{}).Preview(ctx, o.plan)
	if err != nil || o.sslNote == nil {
		return pv, err
	}
	pv.Steps = append(pv.Steps, engine.StepPreview{Info: engine.StepInfo{
		ID:    "followup.ssl:" + o.site.Domain.String(),
		Title: *o.sslNote,
		Why: i18n.T(
			"Tên chưa có trong chứng chỉ chỉ được phục vụ ở cổng 80 và chuyển về https://"+o.site.Domain.String()+"; xin lại chứng chỉ tốn một lượt trong hạn mức của Let's Encrypt.",
			"A name not yet in the certificate is only served on port 80 and redirects to https://"+o.site.Domain.String()+"; reissuing uses one Let's Encrypt order."),
	}})
	return pv, nil
}

// Execute: giữ chỗ (alias.add), chạy job, rồi activate/đổi kiểu/xoá hoặc nhả.
// error chỉ khác nil khi không chạy được hay không ghi được DB; job hỏng thì
// xem Report.Job.Status.
func (o *NamesOp) Execute(ctx context.Context, onEvent func(engine.Event)) (NamesResult, error) {
	if o.rs != nil {
		return o.executeRedirect(ctx, onEvent)
	}
	s := o.sites
	var res NamesResult
	if s.Journal == nil {
		return res, fmt.Errorf("%s: thiếu Journal, không ghi được lịch sử job", o.op)
	}
	d, names := o.site.Domain, []validate.Domain{o.name}
	if o.claim != nil {
		if err := o.ns.ClaimNames(ctx, []site.Claim{*o.claim}); err != nil {
			return res, s.aliasRefusal(err)
		}
	}
	release := func(cause error) error {
		if o.claim == nil {
			return cause
		}
		if err := o.ns.ReleaseNames(ctx, d, names); err != nil {
			return errors.Join(cause, fmt.Errorf("nhả chỗ giữ tên %s: %w (dòng pending tự hết hạn sau 1 giờ)", o.name, err))
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
	switch {
	case o.op == opAliasAdd:
		dbErr = o.ns.ActivateNames(ctx, d, names)
	case o.op == opAliasMode:
		dbErr = o.ns.SetAliasMode(ctx, d, o.name, o.mode)
	case o.op == opAliasRm && o.pending:
		dbErr = o.ns.ReleaseNames(ctx, d, names)
	case o.op == opAliasRm:
		dbErr = o.ns.DeleteAlias(ctx, d, o.name)
	}
	if dbErr != nil {
		// S14: vhost đã đổi mà DB chưa; lần vẽ lại sau (từ DB) sẽ lệch. Báo lệnh sửa.
		return res, fmt.Errorf("job %s đã đổi vhost của %s nhưng không ghi được tên %s vào DB của tpan: %w. Chạy lại đúng lệnh này để ghi nốt, hoặc `sudo tpan health --fix` để vẽ lại vhost theo DB",
			rep.Job.ID, d, o.name, dbErr)
	}
	res.NeedsCert = o.needsCert
	if !o.needsCert {
		return res, nil
	}
	return res, o.followUpSSL(ctx, onEvent, &res)
}

// sslAutoLimit: số job site.ssl tối đa của một chủ trong sslAutoWindow mà
// thao tác tên phụ còn được tự chạy tiếp (S17). Gọi tay `tpan ssl issue` không
// bị giới hạn này.
const (
	sslAutoLimit  = 5
	sslAutoWindow = 24 * time.Hour
)

// jobCounter là phần store đếm job (store.CountJobs). Store giả không cài:
// không giới hạn.
type jobCounter interface {
	CountJobs(ctx context.Context, kind, owner string, since time.Time) (int, error)
}

// followUpSSL (mục 3.6, S14, S17): job tên phụ đã thành công trên site SSL;
// chạy tiếp job site.ssl để chứng chỉ khớp bộ tên mới, đặt FollowUpJob. Job
// SSL hỏng hay bị từ chối không gỡ thay đổi tên (chỉ ghi chú); error chỉ khác
// nil khi không chạy được hay không ghi được DB.
func (o *NamesOp) followUpSSL(ctx context.Context, onEvent func(engine.Event), res *NamesResult) error {
	s, d, owner := o.sites, o.site.Domain, ownerOf(o.site)
	manual := i18n.T("Chạy tay: sudo tpan ssl issue "+d.String(), "Run by hand: sudo tpan ssl issue "+d.String())
	note := func(vi, en string) {
		res.Notes = append(res.Notes, i18n.T(vi+" "+manual.Vi, en+" "+manual.En))
	}
	if jc, ok := s.Store.(jobCounter); ok {
		// ponytail: đếm mọi job site.ssl của chủ (cả chạy tay), không riêng job
		// tự chạy: bảng jobs không có cột phân biệt. Chặt hơn đặc tả, không lỏng hơn.
		n, err := jc.CountJobs(ctx, "site.ssl", owner, time.Now().Add(-sslAutoWindow))
		if err != nil {
			return err
		}
		if n >= sslAutoLimit {
			note(fmt.Sprintf("ssl_rate_limited: chủ site đã có %d job site.ssl trong 24 giờ qua (trần %d, để giữ hạn mức Let's Encrypt chung của máy); job site.ssl không tự chạy, thử lại sau vài giờ.", n, sslAutoLimit),
				fmt.Sprintf("ssl_rate_limited: the site owner already has %d site.ssl jobs in the last 24 hours (limit %d, to protect the machine's shared Let's Encrypt quota); the site.ssl job did not run automatically, try again in a few hours.", n, sslAutoLimit))
			return nil
		}
	}
	ssl := &SSL{Sys: s.Sys, Settings: s.Settings, Store: s.Store, Journal: s.Journal, Accounts: s.Accounts}
	iss, err := ssl.PrepareIssue(ctx, d)
	if err != nil {
		var re *RefusedError
		if !errors.As(err, &re) {
			return err
		}
		note("Chưa chạy được job site.ssl: "+re.Message.Vi+".", "The site.ssl job could not run: "+re.Message.En+".")
		return nil
	}
	rep, err := iss.Execute(engine.WithOwner(ctx, owner), onEvent)
	res.FollowUpJob = rep.Job.ID
	if err != nil {
		return fmt.Errorf("thay đổi tên của %s đã xong, nhưng job site.ssl chạy tiếp lỗi: %w", d, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		note("Job site.ssl "+rep.Job.ID+" hỏng ("+rep.Job.Error+"); thay đổi tên vẫn giữ, chứng chỉ cũ không đổi.",
			"Job site.ssl "+rep.Job.ID+" failed ("+rep.Job.Error+"); the name change stays and the old certificate is unchanged.")
		return nil
	}
	for _, n := range iss.Names {
		if n.Reason != nil {
			res.Notes = append(res.Notes, i18n.T("Tên "+n.Name.String()+" chưa vào chứng chỉ: "+n.Reason.Vi,
				"The name "+n.Name.String()+" is not in the certificate: "+n.Reason.En))
		}
	}
	res.Notes = append(res.Notes, iss.Notes...)
	return nil
}

// ---------------------------------------------------------------- site.add

func checkWWWMode(m site.NameMode) error {
	if m == "" || m == WWWNone {
		return nil
	}
	if err := checkNameMode(m); err != nil {
		return refuse(CodeNameModeInvalid, i18n.T(
			"--www phải là redirect, serve hoặc none (nhận \""+string(m)+"\")",
			"--www must be redirect, serve or none (got \""+string(m)+"\")"))
	}
	return nil
}

// defaultWWW trả tên phụ mặc định của d: tên gốc → www.<gốc>, www.<gốc> →
// <gốc>; tên con sâu hơn thì không có (mục 3.5).
func defaultWWW(d validate.Domain) (validate.Domain, bool) {
	rd, err := validate.RegisteredDomain(d)
	if err != nil {
		return validate.Domain{}, false
	}
	root := rd.String()
	var want string
	switch d.String() {
	case root:
		want = "www." + root
	case "www." + root:
		want = root
	default:
		return validate.Domain{}, false
	}
	w, err := validate.ParseDomain(want)
	return w, err == nil
}

func claimNames(cs []site.Claim) []validate.Domain {
	out := make([]validate.Domain, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

// addClaims kiểm tên chính của site mới rec (luật P4 duy nhất, mục 3.2) và
// chọn tên www mặc định. Tên chính bị từ chối thì từ chối tạo site; tên www
// không dùng được thì bỏ qua kèm ghi chú (site vẫn tạo được). Store không giữ
// được tên (store giả): chỉ kiểm tên chính trên bảng sites, không có www.
// Thêm tên www vào rec.Names để vhost của job có nó.
func (s *Sites) addClaims(ctx context.Context, rec *site.Site, www site.NameMode) ([]site.Claim, []i18n.Text, error) {
	primary := site.Claim{Name: rec.Domain, Site: rec.Domain, Owner: ownerOf(*rec), Kind: site.KindPrimary, Mode: site.ModeServe, ByAdmin: !s.NotAdmin}
	ns, ok := s.nameStore()
	if !ok {
		if err := s.checkClaimsLocal(ctx, []site.Claim{primary}); err != nil {
			return nil, nil, s.siteAddRefusal(err)
		}
		return nil, nil, nil
	}
	if err := ns.CheckNames(ctx, []site.Claim{primary}); err != nil {
		return nil, nil, s.siteAddRefusal(err)
	}
	claims := []site.Claim{primary}
	w, ok := defaultWWW(rec.Domain)
	if www == WWWNone || !ok {
		return claims, nil, nil
	}
	if www == "" {
		www = site.ModeRedirect
	}
	wc := site.Claim{Name: w, Site: rec.Domain, Owner: primary.Owner, Kind: site.KindAlias, Mode: www, ByAdmin: primary.ByAdmin}
	if err := ns.CheckNames(ctx, []site.Claim{primary, wc}); err != nil {
		var re *RefusedError
		if !errors.As(s.aliasRefusal(err), &re) {
			return nil, nil, err
		}
		note := i18n.T("Không thêm tên phụ mặc định "+w.String()+": "+re.Message.Vi,
			"The default alias "+w.String()+" was not added: "+re.Message.En)
		return claims, []i18n.Text{note}, nil
	}
	rec.Names = withName(rec.Names, w, www)
	return append(claims, wc), nil, nil
}

// ---------------------------------------------------------------- danh sách

// nameDNSTimeout: mỗi tên tra DNS tối đa 2 giây (S18), để một tên hết giờ
// không làm cả danh sách chậm hàng chục giây.
const nameDNSTimeout = 2 * time.Second

// NameList trả mọi tên của site (cả pending) kèm DNS tra lúc gọi (phép kiểm
// dns.a của M5: đạt là "ok", không có bản ghi A là "missing", còn lại "warn").
// Không đổi gì; quyền xem do server kiểm (owner:site).
func (s *Sites) NameList(ctx context.Context, siteD validate.Domain) ([]NameView, error) {
	if _, err := s.find(ctx, siteD); err != nil {
		return nil, err
	}
	ns, ok := s.nameStore()
	if !ok {
		return nil, refuse(CodeNamesUnsupported, i18n.T(
			"store không giữ được tên phụ (chế độ chỉ xem hay bản thử)",
			"the store cannot hold aliases (read-only or demo mode)"))
	}
	rows, err := ns.NamesOf(ctx, siteD)
	if err != nil {
		return nil, err
	}
	out := make([]NameView, 0, len(rows))
	for _, r := range rows {
		dctx, cancel := context.WithTimeout(ctx, nameDNSTimeout)
		res := checks.DNSPointsHere(site.LayoutOf(r.Name)).Run(dctx, s.Sys)
		cancel()
		dns := "warn"
		switch res.Status {
		case checks.Pass:
			dns = "ok"
		case checks.Fail:
			dns = "missing"
		}
		out = append(out, NameView{Name: r.Name.String(), Kind: r.Kind, Mode: r.Mode, State: r.State, DNS: dns, InCert: r.InCert})
	}
	return out, nil
}
