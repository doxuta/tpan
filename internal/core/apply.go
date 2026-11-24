package core

// Thao tác hàng loạt (mục 7 của docs/KE-HOACH.md): `tpan apply -f sites.yaml
// [--prune]`, `tpan export`, và "dán danh sách" trên màn Tạo site.
//
// Apply đưa máy về đúng danh sách: site thiếu thì tạo, site đã có (active) thì
// bỏ qua (không chạy lại Plan), site ngoài danh sách thì vào thùng rác khi
// Prune. Cả tạo lẫn đưa vào thùng rác nằm trong MỘT engine.Batch (một job):
//
//	Pre:    WPArchive (tải WordPress một lần cho cả lô), nếu có site mới
//	Unit tạo site, pha A: user, thư mục, DB, pool, WPFiles, WPConfig,
//	        cron của site (DISABLE_WP_CRON, file cron; M9),
//	        .htaccess (Apache), vhost: steps.WithoutReload; useradd và các bước
//	        ghi file có Test bọc steps.Serial (một mutex cho cả lô)
//	Unit đưa vào thùng rác, pha A: các bước của PrepareRemove, WithoutReload
//	Commit: steps.PHPReload cho mỗi bản PHP trong lô, rồi steps.WebReload
//	Unit tạo site, pha C: WPInstall (bọc như wpInstallStep để biết có cài thật
//	        không), Verify: siteChecks với các site active đã có trong store
//	Parallel: 3
//
// Pha D (SSL, lần lượt từng site, có kiểm trước) chạy SAU job hàng loạt, mỗi
// site có ssl một job SSL riêng (dùng lại ca sử dụng SSL của M5), chỉ cho site
// vừa tạo thành công. Cô lập giữa các site cùng lô kiểm sau job (checks.RunAll
// trên mọi cặp site mới thành công) và nằm trong ApplyResult.Isolation, không
// hoàn tác gì: lúc pha C chạy song song, site anh em có thể đang dở.
//
// Job hàng loạt thành công: mỗi unit tạo site succeeded được SaveSite (active),
// mỗi unit thùng rác succeeded được đánh dấu trashed như PrepareRemove.
//
// Unit tạo site được dựng bằng chính PrepareAdd (cùng điều kiện đầu, cùng lời
// từ chối, cùng addPlan) rồi tách ra pha: bỏ WPArchive (lên Pre) và bước cài
// WordPress (xuống pha C). Unit thùng rác dùng nguyên Plan của PrepareRemove.
// Nhờ vậy tạo một site và tạo mười site không thể lệch nhau về các bước.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- apply

// applyParallel: mục 7, "song song, tối đa 3 site một lúc".
const applyParallel = 3

// ApplyAction là việc Apply sẽ làm với một domain.
type ApplyAction string

const (
	ApplyCreate  ApplyAction = "create"  // chưa có: tạo
	ApplySkip    ApplyAction = "skip"    // đã có, active: bỏ qua
	ApplyTrash   ApplyAction = "trash"   // ngoài file và Prune: vào thùng rác
	ApplyRefused ApplyAction = "refused" // không làm được (đang ở thùng rác, máy đã có thứ của domain, thiếu email...)
)

// ApplyItem là một dòng của bản kế hoạch hàng loạt.
type ApplyItem struct {
	Domain string      `json:"domain"`
	Action ApplyAction `json:"action"`
	SSL    bool        `json:"ssl,omitempty"`
	// Code và Reason chỉ có khi Refused (Code như RefusedError.Code).
	Code   string    `json:"code,omitempty"`
	Reason i18n.Text `json:"reason,omitzero"`
}

// ApplyInput là đầu vào của Apply.
type ApplyInput struct {
	Sites []ApplySite
	Prune bool
	// Scope (M13): rỗng là mọi site (dòng lệnh, admin). Có tên user thì chỉ
	// site của user đó được tính là "đã có" và bị Prune; site mới thuộc user đó,
	// và dòng có domain thuộc người khác thì Refused (site_not_found, như không
	// tồn tại, để không dò được tên site của người khác). Mục 6.12.
	Scope string
}

// Apply là một lần áp danh sách đã qua điều kiện đầu. Dùng cho đúng một lần
// Execute (các Step giữ trạng thái để hoàn tác).
type Apply struct {
	Items []ApplyItem
	// Batch là job hàng loạt; Units rỗng khi không có gì để tạo hay xoá.
	Batch engine.Batch

	sites *Sites
	// units song song với Batch.Units (cùng chỉ số): nhờ đó kết quả
	// rep.Units[k] tìm lại được site nào, tạo hay đưa vào thùng rác.
	units []applyUnit
}

// applyUnit là phần core cần nhớ của một unit để làm việc sau job.
type applyUnit struct {
	add   *SiteAdd    // unit tạo site; nil với unit thùng rác
	rm    *SiteRemove // unit thùng rác; nil với unit tạo site
	ssl   bool
	email validate.Email
}

// refusedItem đổi lời từ chối của một dòng thành ApplyItem; lỗi khác (đọc DB
// hỏng...) trả lại để người gọi dừng cả lô.
func refusedItem(item ApplyItem, err error) (ApplyItem, error) {
	var re *RefusedError
	if !errors.As(err, &re) {
		return item, err
	}
	item.Action, item.Code, item.Reason = ApplyRefused, re.Code, re.Message
	return item, nil
}

// PrepareApply làm phần chỉ đọc: stack đã cài, phân loại từng domain (Create,
// Skip, Trash, Refused), sinh mật khẩu, dựng Batch. Từ chối cả lô (lỗi
// *RefusedError) chỉ khi không dựng được gì: stack chưa cài, đầu vào rỗng.
// Dòng nào không làm được thì thành ApplyRefused, các dòng khác vẫn đi.
func (s *Sites) PrepareApply(ctx context.Context, in ApplyInput) (*Apply, error) {
	if len(in.Sites) == 0 {
		if in.Prune {
			// Một file rỗng (gõ nhầm, file bị cắt cụt) cộng --prune sẽ đưa MỌI
			// site vào thùng rác. Không ai muốn điều đó qua một file rỗng.
			return nil, refuse("prune_empty", i18n.T(
				"danh sách site rỗng mà có --prune: làm vậy sẽ đưa mọi site vào thùng rác. Từ chối cả lô; muốn xoá site thì dùng: sudo tpan site rm <domain>",
				"the site list is empty and --prune is set: that would move every site to the trash. Refusing the whole batch; to remove sites use: sudo tpan site rm <domain>"))
		}
		return nil, refuse("apply_empty", i18n.T(
			"danh sách site rỗng: không có gì để áp",
			"the site list is empty: there is nothing to apply"))
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	stored := make(map[validate.Domain]site.Site, len(all))
	for _, st := range all {
		stored[st.Domain] = st
	}

	// M13: Scope rỗng là mọi site (dòng lệnh, admin). Có tên thì chỉ site của
	// user đó là "của mình": được bỏ qua, được prune; site của người khác như
	// không tồn tại với người gọi.
	scope := in.Scope
	mine := func(st site.Site) bool { return scope == "" || ownerOf(st) == scope }
	// pending đếm site mới đã nhận trong lô theo từng chủ: Usage của store
	// chưa thấy chúng, nên hạn mức phải cộng thêm (dòng thừa thành Refused).
	pending := map[string]int{}

	a := &Apply{sites: s}
	var creates []applyUnit
	listed := make(map[validate.Domain]bool, len(in.Sites))
	for _, as := range in.Sites {
		d := as.Input.Domain
		item := ApplyItem{Domain: d.String()}
		owner := as.Input.Owner
		if scope != "" && owner == "" {
			owner = scope
		}
		if owner == "" {
			owner = account.AdminName
		}
		as.Input.Owner = owner
		var err error
		switch st, found := stored[d]; {
		case d.IsZero():
			item, err = refusedItem(item, refuse(validate.CodeDomainInvalid, i18n.T("thiếu domain", "the domain is missing")))
		case listed[d]:
			// Resolve đã chặn trùng; API có thể gửi thẳng ApplyInput. Hai unit
			// cùng Target thì engine từ chối cả job, nên chặn từng dòng ở đây.
			item, err = refusedItem(item, refuse("domain_duplicate", i18n.T(
				"domain "+d.String()+" có hai lần trong danh sách",
				"domain "+d.String()+" is listed twice")))
		case scope != "" && owner != scope:
			// Người gọi theo phạm vi chỉ tạo được site cho chính mình; với họ
			// user khác như không tồn tại.
			item, err = refusedItem(item, refuse(CodeUserNotFound, i18n.T(
				"dòng của "+d.String()+" ghi owner khác tài khoản của bạn; bỏ owner hoặc ghi đúng tên của bạn",
				"the line for "+d.String()+" names an owner other than your account; drop owner or use your own name")))
		case found && !mine(st):
			item, err = refusedItem(item, refuse("site_not_found", i18n.T(
				"không tạo được site "+d.String()+" từ tài khoản này: domain đã được dùng",
				"site "+d.String()+" cannot be created from this account: the domain is already in use")))
		case found && st.Status == site.Active:
			// Mục 7: site đã có thì bỏ qua, không chạy lại Plan (sửa lệch là
			// việc của `tpan site add` hay health --fix).
			item.Action = ApplySkip
		default:
			// Chưa có, hoặc có mà không active (thùng rác): PrepareAdd kiểm
			// đúng như `tpan site add` (email, tiêu đề, site_trashed, thứ của
			// domain đã có trên máy) và dựng Plan.
			if !found {
				// PrepareAdd kiểm hạn mức theo Usage của store; ở đây cộng thêm
				// các site mới đã nhận trước đó trong cùng lô.
				if err = quotaCheck(ctx, s.Accounts, owner, QuotaSites, d, pending[owner]); err != nil {
					item, err = refusedItem(item, err)
					break
				}
			}
			var add *SiteAdd
			add, err = s.PrepareAdd(ctx, as.Input)
			if err != nil {
				item, err = refusedItem(item, err)
				break
			}
			pending[owner]++
			item.Action, item.SSL = ApplyCreate, as.SSL
			creates = append(creates, applyUnit{add: add, ssl: as.SSL, email: as.Input.Email})
		}
		if err != nil {
			return nil, fmt.Errorf("chuẩn bị %s: %w", d, err)
		}
		listed[d] = true
		a.Items = append(a.Items, item)
	}

	var trashes []applyUnit
	trashed := map[validate.Domain]bool{}
	if in.Prune {
		for _, st := range all {
			if st.Status != site.Active || listed[st.Domain] || !mine(st) {
				continue
			}
			item := ApplyItem{Domain: st.Domain.String(), Action: ApplyTrash}
			rm, err := s.PrepareRemove(ctx, st.Domain)
			if err != nil {
				if item, err = refusedItem(item, err); err != nil {
					return nil, fmt.Errorf("chuẩn bị đưa %s vào thùng rác: %w", st.Domain, err)
				}
			} else {
				trashes = append(trashes, applyUnit{rm: rm})
				trashed[st.Domain] = true
			}
			a.Items = append(a.Items, item)
		}
	}

	a.units = slices.Concat(creates, trashes)
	if len(a.units) == 0 {
		return a, nil
	}
	if a.Batch, err = s.applyBatch(ctx, cfg, all, trashed, a.units); err != nil {
		return nil, err
	}
	return a, nil
}

// applyBatch dựng engine.Batch cho các unit (đã theo đúng thứ tự: tạo theo
// thứ tự danh sách, rồi thùng rác theo domain).
func (s *Sites) applyBatch(ctx context.Context, cfg StackConfig, all []site.Site, trashed map[validate.Domain]bool, units []applyUnit) (engine.Batch, error) {
	// Một mutex cho cả lô: useradd/usermod khoá /etc/passwd, và `nginx -t`
	// kiểm cả config nên file dở của site này làm site kia lỗi oan.
	mu := &sync.Mutex{}

	// Kiểm cô lập của site mới với các site đang chạy, trừ site lô này đưa vào
	// thùng rác: thư mục và user của chúng đang được chuyển và khoá song song,
	// kiểm vào lúc đó có thể Fail và hoàn tác oan một site mới lành.
	var others []site.Site
	for _, st := range all {
		if st.Status == site.Active && !trashed[st.Domain] {
			others = append(others, st)
		}
	}

	b := engine.Batch{Kind: "apply", Parallel: applyParallel}
	php := map[string]bool{}
	creates, trashes := 0, 0
	for _, u := range units {
		switch {
		case u.add != nil:
			creates++
			add := u.add
			php[add.Site.PHPVersion] = true
			var prep []engine.Step
			for _, st := range add.Plan.Steps {
				// WPArchive lên Pre (một lần cho cả lô: trùng StepID giữa các
				// unit thì engine từ chối); cài WordPress xuống pha C, sau khi
				// web server đã nạp vhost ở pha B.
				if _, ok := st.(*steps.WPArchive); ok || st == engine.Step(add.install) {
					continue
				}
				prep = append(prep, st)
			}
			l := add.Layout
			// Site mới luôn sinh ra ở ngoài lồng (lim nil); đọc qua siteCageLimits
			// như mọi chỗ gọi siteChecks để không chỗ nào tự đoán trần.
			lim, err := siteCageLimits(ctx, s.Accounts, add.Site)
			if err != nil {
				return engine.Batch{}, err
			}
			b.Units = append(b.Units, engine.Unit{
				Target:  l.Domain.String(),
				Prepare: applySerial(mu, prep),
				Finish:  []engine.Step{add.install},
				Verify:  siteChecks(cfg.Web, add.Site.PHPVersion, add.Site, others, lim),
			})
		default:
			trashes++
			rm := u.rm
			php[rm.Site.PHPVersion] = true
			b.Units = append(b.Units, engine.Unit{
				Target:  rm.Site.Domain.String(),
				Prepare: applySerial(mu, rm.Plan.Steps),
				Verify:  rm.Plan.Verify,
			})
		}
	}
	if creates > 0 {
		version, hash := s.wordpress()
		b.Pre = []engine.Step{&steps.WPArchive{Version: version, Hash: hash}}
	}

	// Pha B: mỗi bản PHP một lần (sắp xếp để thứ tự ổn định giữa các lần
	// chạy), rồi web server. Pool trước vhost như Plan một site: web server
	// nạp vhost mới thì socket của pool đã có.
	versions := make([]string, 0, len(php))
	for v := range php {
		versions = append(versions, v)
	}
	slices.Sort(versions)
	for _, v := range versions {
		r, err := steps.PHPReload(v)
		if err != nil {
			return engine.Batch{}, fmt.Errorf("dựng job hàng loạt: %w", err)
		}
		b.Commit = append(b.Commit, r)
	}
	web, err := steps.WebReload(string(cfg.Web))
	if err != nil {
		return engine.Batch{}, fmt.Errorf("dựng job hàng loạt: %w", err)
	}
	b.Commit = append(b.Commit, web)

	b.Target = fmt.Sprintf("%d site", len(units))
	b.Title = i18n.T(
		fmt.Sprintf("Áp danh sách site: tạo %d, đưa vào thùng rác %d", creates, trashes),
		fmt.Sprintf("Apply the site list: create %d, move %d to the trash", creates, trashes))
	return b, nil
}

// applySerial bỏ Reload của các bước ghi file (pha B reload một lần cho cả
// lô) rồi bọc Serial các bước không được chạy cùng lúc với unit khác: đổi
// user Linux, và mọi bước có Test (pool, vhost, link, gỡ link). Phải xét kiểu
// trước khi bọc: bước đã bọc không còn là *steps.File.
func applySerial(mu *sync.Mutex, ss []engine.Step) []engine.Step {
	out := make([]engine.Step, 0, len(ss))
	for _, st := range steps.WithoutReload(ss) {
		serial := false
		switch st := st.(type) {
		case *steps.SiteUser, *steps.SiteUserAbsent, *steps.SiteUserLock:
			serial = true
		case *steps.File:
			serial = st.Test != nil
		case *steps.Symlink:
			serial = st.Test != nil
		case *steps.Absent:
			serial = st.Test != nil
		case *cageAbsentPool:
			// Bước pool của site ngoài lồng (sitePHPSteps) được bọc để từ chối khi
			// site đã vào lồng (N4 của soát cuối M17): bỏ reload và xếp hàng theo
			// bước bên trong, như một File thường.
			steps.WithoutReload([]engine.Step{st.Step})
			if f, ok := st.Step.(*steps.File); ok {
				serial = f.Test != nil
			}
		}
		if serial {
			st = steps.Serial(mu, st)
		}
		out = append(out, st)
	}
	return out
}

// Empty: không có gì để tạo hay xoá (mọi dòng Skip hoặc Refused). Execute khi
// Empty thì không tạo job nào.
func (a *Apply) Empty() bool { return len(a.Batch.Units) == 0 }

// ApplyPreview là bản xem trước của Apply.
type ApplyPreview struct {
	Items []ApplyItem `json:"items"`
	// Preview là bản xem trước phẳng của Batch (engine.PreviewBatch); nil khi Empty.
	Preview *engine.Preview `json:"preview,omitempty"`
}

// Preview chỉ đọc máy.
func (a *Apply) Preview(ctx context.Context) (ApplyPreview, error) {
	out := ApplyPreview{Items: a.Items}
	if a.Empty() {
		return out, nil
	}
	pv, err := engine.New(a.sites.Sys, engine.Options{}).PreviewBatch(ctx, a.Batch)
	if err != nil {
		return out, err
	}
	out.Preview = &pv
	return out, nil
}

// ApplySiteResult là kết quả của một site được tạo trong lô.
type ApplySiteResult struct {
	Domain      string            `json:"domain"`
	Status      engine.UnitStatus `json:"status"`
	WPInstalled bool              `json:"wp_installed"`
	AdminUser   string            `json:"admin_user,omitempty"`
	// AdminPassword chỉ có khi WPInstalled; JSON luôn "••••" (system.Secret),
	// người gọi tự Reveal đúng một lần, như SiteAddResult.
	AdminPassword system.Secret `json:"admin_password"`
	// SSLError là lời từ chối của pha D cho site này (DNS chưa trỏ về máy,
	// chưa có email cho Let's Encrypt...): job SSL không chạy, site vẫn được
	// tạo. Job SSL đã chạy (thành hay hỏng) thì nằm ở ApplyResult.SSL.
	SSLError *RefusedError `json:"ssl_error,omitempty"`
	// CageError (M17) là lời từ chối của job cage.enable sau lô cho site này
	// (gói của chủ bật lồng); site vẫn được tạo, ở ngoài lồng. Job đã chạy thì
	// nằm ở ApplyResult.Cage.
	CageError *RefusedError `json:"cage_error,omitempty"`
}

// ApplyResult là kết quả của Apply.Execute.
type ApplyResult struct {
	Items []ApplyItem `json:"items"`
	// Report là kết quả job hàng loạt; nil khi Empty.
	Report *engine.BatchReport `json:"report,omitempty"`
	// Created: một dòng cho mỗi unit tạo site, theo thứ tự Items.
	Created []ApplySiteResult `json:"created"`
	// Isolation: phép kiểm cô lập giữa các site mới cùng lô (sau job).
	Isolation []checks.Result `json:"isolation,omitempty"`
	// SSL: job SSL của pha D, theo thứ tự (site lỗi SSL không làm hỏng site).
	SSL []engine.Report `json:"ssl,omitempty"`
	// Cage (M17): job cage.enable chạy sau lô cho mỗi site mới mà gói của chủ
	// bật lồng, theo thứ tự (site hỏng lồng vẫn được tạo, ở ngoài lồng).
	Cage []engine.Report `json:"cage,omitempty"`
}

// Execute chạy job hàng loạt (engine.ExecuteBatch), ghi bảng sites cho từng
// unit thành công, kiểm cô lập trong lô, rồi pha D. onEvent nhận sự kiện của
// MỌI job chạy ở đây (job hàng loạt trước, rồi từng job SSL; mỗi job JobID
// riêng). error chỉ khác nil khi không chạy được hoặc không ghi được bảng
// sites; kể cả khi đó ApplyResult vẫn mang mật khẩu quản trị đã có.
func (a *Apply) Execute(ctx context.Context, onEvent func(engine.Event)) (ApplyResult, error) {
	s := a.sites
	res := ApplyResult{Items: a.Items, Created: []ApplySiteResult{}}
	if a.Empty() {
		return res, nil
	}
	if s.Journal == nil {
		return res, errors.New("áp danh sách site: thiếu Journal, không ghi được lịch sử job")
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).ExecuteBatch(ctx, a.Batch)
	if rep.Job.ID == "" {
		// Job chưa được tạo (khoá, Journal, Batch sai): máy chưa đổi gì.
		return res, fmt.Errorf("chạy job hàng loạt: %w", err)
	}
	res.Report = &rep
	var errs []error
	if err != nil {
		// Job đã chạy nhưng Journal hỏng giữa chừng: vẫn đi tiếp để ghi bảng
		// sites và trả mật khẩu của các site đã thành.
		errs = append(errs, fmt.Errorf("job hàng loạt %s: %w", rep.Job.ID, err))
	}

	type fresh struct {
		k int // chỉ số trong Created
		u applyUnit
	}
	var made []fresh
	for k, u := range a.units {
		status := engine.UnitNotRun
		if k < len(rep.Units) {
			status = rep.Units[k].Status
		}
		if u.add == nil {
			if status != engine.UnitSucceeded {
				continue
			}
			if err := s.markTrashed(ctx, u.rm.Site); err != nil {
				errs = append(errs, fmt.Errorf("job %s đã đưa site %s vào thùng rác nhưng không ghi được vào DB của tpan: %w. Chạy lại `tpan site rm %s` để ghi nốt",
					rep.Job.ID, u.rm.Site.Domain, err, u.rm.Site.Domain))
			}
			continue
		}
		r := ApplySiteResult{Domain: u.add.Site.Domain.String(), Status: status}
		if status == engine.UnitSucceeded {
			// Chỉ unit succeeded: unit bị hoàn tác ở pha C có thể đã chạy
			// `wp core install` (ran = true) nhưng WordPress đó đã bị gỡ.
			if u.add.install.ran {
				r.WPInstalled, r.AdminUser, r.AdminPassword = true, u.add.AdminUser, u.add.adminPassword
			}
			rec := u.add.Site
			rec.Created = s.now().UTC()
			if err := s.Store.SaveSite(ctx, rec); err != nil {
				errs = append(errs, fmt.Errorf("job %s xong nhưng không ghi được site %s vào DB của tpan: %w. Máy đã có site mà tpan chưa ghi nhận, nên `tpan site add %s` sẽ bị từ chối là site không do tpan quản lý",
					rep.Job.ID, rec.Domain, err, rec.Domain))
			} else {
				made = append(made, fresh{k: len(res.Created), u: u})
			}
		}
		res.Created = append(res.Created, r)
	}

	// Cô lập trong lô: mỗi cặp site mới, hai chiều. Chỉ báo, không hoàn tác.
	var iso []checks.Check
	for _, x := range made {
		for _, y := range made {
			if x.k != y.k {
				iso = append(iso, checks.Isolated(x.u.add.Layout, y.u.add.Layout))
			}
		}
	}
	if len(iso) > 0 {
		res.Isolation = checks.RunAll(ctx, s.Sys, iso)
	}

	// Pha D: SSL lần lượt, mỗi site một job, chỉ site vừa tạo và đã ghi vào
	// bảng sites (PrepareIssue đọc site từ đó).
	for _, x := range made {
		if !x.u.ssl {
			continue
		}
		rep, re, err := s.applySSL(ctx, x.u, onEvent)
		if rep != nil {
			res.SSL = append(res.SSL, *rep)
		}
		res.Created[x.k].SSLError = re
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Pha E (M17): như `site add`, mỗi site mới mà gói của chủ muốn lồng một
	// job cage.enable, lần lượt, sau SSL (PrepareEnable đọc lại bảng sites nên
	// thấy cờ SSL vừa ghi và ra vhost 443 của lồng) và sau kiểm cô lập trong lô
	// (Isolated không được chạy từ một site đã vào lồng). Site hỏng lồng vẫn
	// được tạo, ở ngoài lồng.
	for _, x := range made {
		rep, re := s.enableCageIfWanted(ctx, x.u.add.Site, onEvent)
		if rep != nil {
			res.Cage = append(res.Cage, *rep)
		}
		res.Created[x.k].CageError = re
	}
	return res, errors.Join(errs...)
}

// applySSL chạy Plan SSL cho một site vừa tạo, như `tpan site add --ssl`:
// ssl.email chưa đặt thì dùng email quản trị của site và ghi lại. Trả báo cáo
// khi job đã chạy; lời từ chối khi không chạy (re); err cho lỗi khác.
func (s *Sites) applySSL(ctx context.Context, u applyUnit, onEvent func(engine.Event)) (*engine.Report, *RefusedError, error) {
	d := u.add.Site.Domain
	ssl := &SSL{Sys: s.Sys, Settings: s.Settings, Store: s.Store, Journal: s.Journal, Accounts: s.Accounts}
	var re *RefusedError
	cfg, err := ssl.Config(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("SSL cho %s: %w", d, err)
	}
	if cfg.Email == "" && !u.email.IsZero() {
		if _, err := ssl.SetConfig(ctx, SSLSettingsInput{Email: &u.email}); errors.As(err, &re) {
			return nil, re, nil
		} else if err != nil {
			return nil, nil, fmt.Errorf("SSL cho %s: %w", d, err)
		}
	}
	iss, err := ssl.PrepareIssue(ctx, d)
	if errors.As(err, &re) {
		return nil, re, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("SSL cho %s: %w", d, err)
	}
	rep, err := iss.Execute(ctx, onEvent)
	if rep.Job.ID == "" {
		return nil, nil, fmt.Errorf("SSL cho %s: %w", d, err)
	}
	if err != nil {
		return &rep, nil, fmt.Errorf("SSL cho %s: %w", d, err)
	}
	return &rep, nil, nil
}
