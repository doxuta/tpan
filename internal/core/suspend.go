package core

// Tạm khoá site của M13 (mục 6.12 của docs/KE-HOACH.md).
//
// Plan "site.suspend", đúng thứ tự:
//  1. Trang tạm khoá chung (steps.SuspendedPage), để vhost ở bước 2 có trang trả về.
//  2. Vhost bản tạm khoá qua siteVhost (bản 443 khi site có SSL), kiểm config,
//     reload web server. Trước pool: không lúc nào web server còn chuyển .php
//     tới một socket đã mất (khách sẽ thấy 502 thay vì trang tạm khoá).
//  3. Pool PHP-FPM đổi tên .suspended, `php-fpm<v> -t`, reload.
//  4. Cron của site đổi tên .suspended.
//  Kiểm chứng: vhost bật, config hợp lệ, gọi site trên máy ra 403 và đúng trang
//  tạm khoá (thử lại tới 5 giây như http.gone). Job thành công thì ghi
//  Suspended = self, SuspendedAt vào bảng sites.
//
// Plan "site.unsuspend" làm ngược lại: pool về .conf (chờ socket), cron về,
// vhost thường (hoặc 443). Kiểm chứng là phép kiểm của site thường (trả 200,
// đúng WordPress), bỏ chặng DB và đầu cuối khi DB của site đang khoá riêng.
//
// M17, site trong lồng: bước 3 là CageSocket{Disable} (file của lồng giữ
// nguyên); mở khoá dùng sitePHPSteps (CageFiles rồi CageSocket bật) thay cho
// đưa pool về.
//
// User DB không bị đụng: tạm khoá site và tạm khoá DB là hai thao tác riêng
// (site.Site.DBSuspended).

import (
	"context"
	"fmt"
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

// PrepareSuspend dựng Plan tạm khoá một site (Suspended = SuspendSelf): vhost
// bản "tạm khoá", pool và cron đổi tên .suspended, kiểm chứng ra 403 đúng trang
// tạm khoá. Từ chối site trong thùng rác hoặc đã khoá.
func (s *Sites) PrepareSuspend(ctx context.Context, d validate.Domain) (*Planned, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	dom := d.String()
	if st.Status != site.Active {
		return nil, refuse("site_trashed", i18n.T(
			"site "+dom+" đang ở thùng rác; không có gì để tạm khoá",
			"site "+dom+" is in the trash; there is nothing to suspend"))
	}
	if st.Suspended != account.NotSuspended {
		return nil, refuse(CodeSiteSuspended, i18n.T(
			"site "+dom+" đang tạm khoá rồi",
			"site "+dom+" is already suspended"))
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	ss, err := s.suspendSteps(ctx, st)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan tạm khoá site %s: %w", dom, err)
	}
	verify := append([]checks.Check{checks.VhostEnabled(string(cfg.Web), st.Layout()), webConfigCheck(cfg.Web)}, suspendedChecks(st)...)
	plan := engine.Plan{
		Kind:   "site.suspend",
		Target: dom,
		Title:  i18n.T("Tạm khoá site "+dom, "Suspend site "+dom),
		Steps:  ss,
		Verify: verify,
	}
	return newPlanned(s.Sys, s.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		return s.markSuspended(ctx, d, account.SuspendSelf, s.now().UTC())
	}), nil
}

// PrepareUnsuspend làm ngược lại và kiểm site trả 200. Mở cả site bị khoá vì
// user (SuspendByUser): admin bấm mở một site là ý của admin.
func (s *Sites) PrepareUnsuspend(ctx context.Context, d validate.Domain) (*Planned, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	dom := d.String()
	if st.Status != site.Active {
		return nil, refuse("site_trashed", i18n.T(
			"site "+dom+" đang ở thùng rác; khôi phục bị từ chối khi site còn khoá, nên trạng thái này là dòng lạ trong DB",
			"site "+dom+" is in the trash; restore is refused for suspended sites, so this is an odd row in the database"))
	}
	if st.Suspended == account.NotSuspended {
		return nil, refuse(CodeSiteNotSuspended, i18n.T(
			"site "+dom+" không bị tạm khoá",
			"site "+dom+" is not suspended"))
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	ss, err := s.unsuspendSteps(ctx, st)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan mở khoá site %s: %w", dom, err)
	}
	open := st
	open.Suspended, open.SuspendedAt = account.NotSuspended, time.Time{}
	others, err := s.others(ctx, d)
	if err != nil {
		return nil, err
	}
	lim, err := siteCageLimits(ctx, s.Accounts, open)
	if err != nil {
		return nil, err
	}
	plan := engine.Plan{
		Kind:   "site.unsuspend",
		Target: dom,
		Title:  i18n.T("Mở khoá site "+dom, "Unsuspend site "+dom),
		Steps:  ss,
		Verify: siteStateChecks(open, cfg, others, lim, true),
	}
	return newPlanned(s.Sys, s.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		return s.markSuspended(ctx, d, account.NotSuspended, time.Time{})
	}), nil
}

// markSuspended đọc lại dòng của site rồi chỉ đổi hai trường tạm khoá: giữa lúc
// dựng Plan và lúc job xong, dòng có thể đã đổi trường khác.
func (s *Sites) markSuspended(ctx context.Context, d validate.Domain, how account.Suspension, at time.Time) error {
	st, err := s.find(ctx, d)
	if err == nil {
		st.Suspended, st.SuspendedAt = how, at
		err = s.Store.SaveSite(ctx, st)
	}
	if err != nil {
		return fmt.Errorf("ghi trạng thái tạm khoá của site %s (máy đã đổi xong; chạy lại lệnh để ghi nốt): %w", d, err)
	}
	return nil
}

// suspendSteps là các bước tạm khoá site st (dùng cho PrepareSuspend và cho
// khoá user). Không ghi bảng sites: người gọi ghi sau khi job thành công.
// st.Suspended không cần đã đặt: bước luôn dựng bản tạm khoá.
func (s *Sites) suspendSteps(ctx context.Context, st site.Site) ([]engine.Step, error) {
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	l := st.Layout()
	page, err := steps.SuspendedPage(l)
	if err != nil {
		return nil, err
	}
	sus := st
	if sus.Suspended == account.NotSuspended {
		sus.Suspended = account.SuspendSelf
	}
	vhost, err := siteVhost(sus, cfg)
	if err != nil {
		return nil, err
	}
	// M17: site trong lồng không có file pool chung để đổi tên; tắt socket của
	// lồng (disable: máy khởi động lại thì site vẫn nằm yên) và dừng dịch vụ.
	// File của lồng giữ nguyên, mở khoá ghi lại nếu chúng mất.
	var pool engine.Step = &steps.CageSocket{Layout: l, PHP: sitePHP(st, cfg), Disable: true}
	if !st.Caged {
		if pool, err = steps.PoolSuspend(l, sitePHP(st, cfg)); err != nil {
			return nil, err
		}
	}
	cron, err := steps.CronSuspend(l)
	if err != nil {
		return nil, err
	}
	ss := append([]engine.Step{page}, vhost...)
	return append(ss, pool, cron), nil
}

// unsuspendSteps là các bước mở khoá site st: pool về (chờ socket), cron về,
// rồi vhost thường. Pool trước vhost để vhost không chuyển .php tới socket
// chưa có.
//
// File pool .suspended không còn (xoá tay, hay site bị đánh dấu khoá mà bước
// đổi tên chưa từng chạy) thì ghi pool từ template như lúc tạo site: không
// thì site mở khoá chỉ trả 502, mà `tpan site add` lại bị từ chối khi site còn
// khoá. Cron .suspended không còn thì thôi: site tạo trước M9 vốn không có cron.
func (s *Sites) unsuspendSteps(ctx context.Context, st site.Site) ([]engine.Step, error) {
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	l, php := st.Layout(), sitePHP(st, cfg)
	var pool []engine.Step
	if st.Caged {
		// M17: sitePHPSteps ra CageFiles (file của lồng mất trong lúc khoá thì
		// được ghi lại, như pool ghi lại từ template bên dưới) rồi CageSocket bật.
		if pool, err = sitePHPStepsFor(ctx, s.Accounts, st, cfg); err != nil {
			return nil, err
		}
	} else {
		parked, err := pathExists(s.Sys, l.PoolConf(php)+steps.SuspendedSuffix)
		if err != nil {
			return nil, err
		}
		p, err := steps.PoolUnsuspend(l, php)
		if err != nil {
			return nil, err
		}
		pool = []engine.Step{p}
		if !parked {
			// Không có file pool đã đổi tên (mất trong lúc khoá): ghi lại pool
			// từ template qua hàm chọn (site ngoài lồng nên ra đúng pool chung).
			if pool, err = sitePHPSteps(st, cfg, account.Limits{}); err != nil {
				return nil, err
			}
		}
	}
	cron, err := steps.CronUnsuspend(l)
	if err != nil {
		return nil, err
	}
	open := st
	open.Suspended = account.NotSuspended
	vhost, err := siteVhost(open, cfg)
	if err != nil {
		return nil, err
	}
	ss := append(append(pool, cron), vhost...)
	// Kiểm giữa chừng như php.use và stack.switch: `systemctl reload apache2`
	// trả về trước khi cấu hình mới có hiệu lực (steps.Reload chỉ chờ nginx),
	// nên request đầu tiên có thể còn gặp vhost tạm khoá và kiểm chứng của Plan
	// hỏng oan. Hai lần đạt liên tiếp, thử lại tới midCheckTries lần. DB của
	// site đang khoá thì không có phép gọi nào trả 200 được (siteHTTP nil).
	if check := siteHTTP(open); check != nil {
		dom := l.Domain.String()
		ss = append(ss, &midCheck{
			id:    "site.unsuspend.check:" + dom,
			check: check,
			need:  2,
			title: i18n.T("Kiểm "+dom+" trả 200 sau khi mở khoá", "Check that "+dom+" answers 200 after unsuspending"),
			why: i18n.T(
				"Web server nạp vhost mới trong nền, sau khi lệnh reload đã trả về. Bước này chờ tới khi site trả lời đúng; không được thì job dừng ở đây và hoàn tác: site về lại trạng thái tạm khoá.",
				"The web server loads the new vhost in the background, after the reload command has returned. This step waits until the site answers correctly; if it never does the job stops here and rolls back: the site goes back to suspended."),
		})
	}
	return ss, nil
}

// suspendedChecks là phép kiểm chứng của site đang tạm khoá: gọi trên máy ra
// 403 và đúng trang tạm khoá. Chỉ dựa vào Layout và st.SSL (khoá user gọi khi
// st.Suspended còn rỗng).
func suspendedChecks(st site.Site) []checks.Check {
	return []checks.Check{retryCheck{c: checks.HTTPSuspended(st.Layout(), st.SSL)}}
}

// retryCheck gọi lại c tới khi đạt hoặc hết goneWait, mỗi goneEvery (cùng
// nhịp với http.gone ở trash.go). Lý do như goneCheck: `systemctl reload`
// trả về trước khi cấu hình mới có hiệu lực, request ngay sau đó còn được
// phục vụ theo vhost cũ (đo ở M6: tới 0,17 giây). Unknown (curl không chạy
// được) trả ngay. Chỉ dùng cho kiểm chứng sau thao tác; health gọi thẳng
// phép kiểm, không chờ.
type retryCheck struct{ c checks.Check }

func (r retryCheck) Run(ctx context.Context, sys system.System) checks.Result {
	deadline := time.Now().Add(goneWait)
	for {
		res := r.c.Run(ctx, sys)
		if res.Status != checks.Fail || time.Now().After(deadline) {
			return res
		}
		select {
		case <-ctx.Done():
			return res
		case <-time.After(goneEvery):
		}
	}
}

// webConfigCheck là phép kiểm cú pháp của web server đang dùng.
func webConfigCheck(web WebServer) checks.Check {
	if web == Apache {
		return checks.ApacheConfig()
	}
	return checks.NginxConfig()
}

// refuseSuspendedPool từ chối thao tác cần pool PHP của site (đổi bản PHP,
// chạy lại site add, khôi phục backup, nhập DB) khi site đang tạm khoá:
// thao tác đó ghi lại pool, tức là mở khoá lén (quyết định 8 của M13).
func refuseSuspendedPool(st site.Site) error {
	if st.Suspended == account.NotSuspended {
		return nil
	}
	d := st.Domain.String()
	return refuse(CodeSiteSuspended, i18n.T(
		"site "+d+" đang tạm khoá; mở khoá trước bằng: sudo tpan site unsuspend "+d,
		"site "+d+" is suspended; unsuspend it first with: sudo tpan site unsuspend "+d))
}

// refuseDBSuspended từ chối thao tác mà kiểm chứng cần WordPress vào được DB
// (đổi bản PHP, xin SSL) khi user DB của site đang tạm khoá: kiểm chứng chắc
// chắn hỏng và engine hoàn tác.
func refuseDBSuspended(st site.Site) error {
	if st.DBSuspended == account.NotSuspended {
		return nil
	}
	d := st.Domain.String()
	return refuse(CodeDBSuspended, i18n.T(
		"DB của site "+d+" đang tạm khoá nên WordPress không vào được DB và kiểm chứng của việc này chắc chắn hỏng; mở khoá DB của site trước",
		"the database of site "+d+" is suspended, so WordPress cannot reach it and this operation's verification is bound to fail; unsuspend the site's database first"))
}
