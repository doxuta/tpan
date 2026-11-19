package core

// Thùng rác (mục 6.4 của docs/KE-HOACH.md). CLI, API và bộ hẹn giờ trong
// `tpan serve` dựa vào các chữ ký ở đây.
//
// Ba thao tác, mỗi cái một Plan, cùng khuôn với tạo site (Prepare* chỉ đọc,
// Preview, Execute ghi bảng sites khi job thành công):
//
//   - Đưa vào thùng rác: Sites.PrepareRemove ở site.go (giữ tên cũ để
//     propose_site_remove của M7 và `site rm` không phải đổi chỗ gọi). Plan
//     "site.rm": gỡ link vhost và pool (Test, Reload), khoá user DB, khoá user
//     Linux, chuyển thư mục site vào site.TrashDir (trashSteps). Mọi bước hoàn
//     tác được, nên Plan có Verify: domain không còn được phục vụ (siteGone).
//     Job thành công: Status trashed, TrashedAt = Now, PurgeAt = Now + số ngày
//     giữ (TrashDays).
//   - Khôi phục (PrepareRestore): làm ngược lại rồi chạy bước kiểm chứng của
//     6.2 (siteChecks, bản SSL nếu site.SSL; vhost qua siteVhost của M4/M5).
//     Từ chối site_not_trashed nếu site không ở thùng rác, site_exists_unmanaged
//     nếu /var/www/<domain> đã có (domain đã được tạo lại ngoài tpan). Job
//     thành công: Status active, TrashedAt và PurgeAt về zero.
//   - Xoá hẳn (PreparePurge): Plan xoá hẳn của M2 (gỡ web nếu còn, xoá DB và
//     user DB, RemoveTree, userdel, certbot delete nếu SSL), chỉ khác:
//     RemoveTree{Path: site.TrashDir(l), Under: site.TrashRoot} thay cho
//     l.Dir. Không có Verify (lý do ở mục 6.4). Từ chối site_not_trashed nếu
//     site đang active: xoá hẳn chỉ đi qua thùng rác. Job thành công: xoá dòng.
//     Chưa có "tự backup trước khi xoá hẳn" (mục 6.5): backup là M9.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Khoá setting của thùng rác. Giá trị là số ngày, chuỗi thập phân.
const (
	SettingTrashDays = "trash.days"
	// DefaultTrashDays: mục 6.4, "mặc định 7 ngày".
	DefaultTrashDays = 7
	// MaxTrashDays: trần để một lỗi gõ (7000) không giữ dữ liệu vô hạn.
	MaxTrashDays = 365
)

// TrashDays đọc số ngày giữ thùng rác; chưa đặt thì DefaultTrashDays. 0 hợp lệ
// (hết hạn ngay, dùng cho e2e và cho người không muốn giữ).
func (s *Sites) TrashDays(ctx context.Context) (int, error) {
	v, ok, err := s.Settings.GetSetting(ctx, SettingTrashDays)
	if err != nil {
		return 0, fmt.Errorf("đọc số ngày giữ thùng rác: %w", err)
	}
	if !ok || v == "" {
		return DefaultTrashDays, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > MaxTrashDays {
		// Giá trị hỏng trong DB: báo, không đoán (đoán sai là xoá hẳn sớm).
		return 0, fmt.Errorf("setting %s = %q không hợp lệ (cần số nguyên 0..%d); đặt lại bằng: sudo tpan trash settings --days N", SettingTrashDays, v, MaxTrashDays)
	}
	return n, nil
}

// SetTrashDays đặt số ngày giữ, 0..MaxTrashDays; ngoài khoảng thì từ chối
// trash_days_invalid. Chỉ áp cho site vào thùng rác SAU lần đặt.
func (s *Sites) SetTrashDays(ctx context.Context, days int) error {
	if days < 0 || days > MaxTrashDays {
		return refuse("trash_days_invalid", i18n.T(
			fmt.Sprintf("số ngày giữ thùng rác phải từ 0 tới %d (nhận được %d)", MaxTrashDays, days),
			fmt.Sprintf("the trash retention must be 0 to %d days (got %d)", MaxTrashDays, days)))
	}
	if err := s.Settings.SetSetting(ctx, SettingTrashDays, strconv.Itoa(days)); err != nil {
		return fmt.Errorf("ghi số ngày giữ thùng rác: %w", err)
	}
	return nil
}

// markTrashed ghi site vào bảng sites ở trạng thái trashed: TrashedAt = now,
// PurgeAt = now + TrashDays. Dùng chung cho SiteRemove.Execute và cho unit
// "đưa vào thùng rác" của job hàng loạt (apply.go), để hai đường ghi cùng một
// cách. st là bản ghi đọc từ store trước khi chạy.
func (s *Sites) markTrashed(ctx context.Context, st site.Site) error {
	days, err := s.TrashDays(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	st.Status, st.TrashedAt, st.PurgeAt = site.Trashed, now, now.AddDate(0, 0, days)
	if err := s.Store.SaveSite(ctx, st); err != nil {
		return fmt.Errorf("ghi site %s là đã vào thùng rác: %w", st.Domain, err)
	}
	return nil
}

// Trash trả các site đang ở thùng rác, sắp theo PurgeAt (sắp hết hạn trước).
func (s *Sites) Trash(ctx context.Context) ([]site.Site, error) {
	all, err := s.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	out := []site.Site{}
	for _, st := range all {
		if st.Status == site.Trashed {
			out = append(out, st)
		}
	}
	// Ổn định: cùng hạn thì giữ thứ tự domain của Store.
	slices.SortStableFunc(out, func(a, b site.Site) int { return a.PurgeAt.Compare(b.PurgeAt) })
	return out, nil
}

// Expired trả các site trong thùng rác có PurgeAt ≤ now, sắp theo PurgeAt.
// Site không có hạn (PurgeAt zero, dòng hỏng) không bao giờ tự bị xoá hẳn: xoá
// dữ liệu khi không biết hạn là đoán; người vận hành vẫn xoá tay được.
func (s *Sites) Expired(ctx context.Context, now time.Time) ([]site.Site, error) {
	trash, err := s.Trash(ctx)
	if err != nil {
		return nil, err
	}
	out := []site.Site{}
	for _, st := range trash {
		if !st.PurgeAt.IsZero() && !st.PurgeAt.After(now) {
			out = append(out, st)
		}
	}
	return out, nil
}

// findTrashed đọc site và đòi nó đang ở thùng rác.
func (s *Sites) findTrashed(ctx context.Context, d validate.Domain) (site.Site, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return site.Site{}, err
	}
	if st.Status != site.Trashed {
		return site.Site{}, refuse("site_not_trashed", i18n.T(
			"site "+d.String()+" không ở thùng rác; đưa vào thùng rác trước bằng: sudo tpan site rm "+d.String(),
			"site "+d.String()+" is not in the trash; move it there first with: sudo tpan site rm "+d.String()))
	}
	return st, nil
}

// runPlan chạy một Plan của thùng rác qua engine; what dùng cho câu lỗi.
func (s *Sites) runPlan(ctx context.Context, what string, p engine.Plan, onEvent func(engine.Event)) (engine.Report, error) {
	if s.Journal == nil {
		return engine.Report{}, errors.New(what + ": thiếu Journal, không ghi được lịch sử job")
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(ctx, p)
	if err != nil {
		return rep, fmt.Errorf("chạy %s: %w", p.Kind, err)
	}
	return rep, nil
}

// ---------------------------------------------------------------- khôi phục

// SiteRestore là một lần khôi phục site từ thùng rác.
type SiteRestore struct {
	Site   site.Site
	Layout site.Layout
	Plan   engine.Plan

	sites *Sites
}

// PrepareRestore dựng Plan "site.restore". Chỉ đọc.
func (s *Sites) PrepareRestore(ctx context.Context, d validate.Domain) (*SiteRestore, error) {
	st, err := s.findTrashed(ctx, d)
	if err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	l := st.Layout()
	// Mục 6.4: "Từ chối nếu domain đã được tạo lại". MoveTree cũng dừng khi cả
	// hai chỗ đều có, nhưng từ chối ở đây thì người dùng biết trước khi bấm.
	exists, err := pathExists(s.Sys, l.Dir)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, refuse("site_exists_unmanaged", i18n.T(
			l.Dir+" đã có: domain "+d.String()+" đã được tạo lại ngoài tpan sau khi vào thùng rác. tpan không đè lên nó; xem và dọn bằng tay rồi khôi phục lại.",
			l.Dir+" already exists: domain "+d.String()+" was created again outside tpan after it went to the trash. tpan will not overwrite it; inspect and clean it up by hand, then restore again."))
	}
	php := sitePHP(st, cfg)
	ss, err := restoreSteps(cfg.Web, php, st)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan khôi phục site %s: %w", d, err)
	}
	others, err := s.others(ctx, d)
	if err != nil {
		return nil, err
	}
	return &SiteRestore{Site: st, Layout: l, sites: s, Plan: engine.Plan{
		Kind:   "site.restore",
		Target: d.String(),
		Title:  i18n.T("Khôi phục site "+d.String()+" từ thùng rác", "Restore site "+d.String()+" from the trash"),
		Steps:  ss,
		Verify: siteChecks(cfg.Web, php, l, others, st.SSL),
	}}, nil
}

// restoreSteps làm ngược trashSteps: chuyển thư mục về, mở khoá user Linux,
// mở khoá user DB, ghi lại pool, bật vhost. Pool và vhost dựng y như lúc tạo
// site (siteVhost chọn bản 443 khi site có SSL): file vhost vẫn nằm trong
// sites-available nên bước ghi nó tự bỏ qua, chỉ bước bật link chạy.
func restoreSteps(web WebServer, php string, st site.Site) ([]engine.Step, error) {
	l := st.Layout()
	pool, err := steps.SitePool(l, php)
	if err != nil {
		return nil, err
	}
	vhost, err := siteVhost(st, string(web))
	if err != nil {
		return nil, err
	}
	d := l.Domain.String()
	ss := []engine.Step{
		&steps.MoveTree{From: site.TrashDir(l), To: l.Dir,
			Title: i18n.T("Chuyển thư mục của "+d+" ra khỏi thùng rác", "Move the directory of "+d+" out of the trash")},
		&steps.SiteUserLock{Layout: l, Unlock: true},
		&steps.DBUserUnlock{Layout: l},
		pool,
	}
	return append(ss, vhost...), nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (r *SiteRestore) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(r.sites.Sys, engine.Options{}).Preview(ctx, r.Plan)
}

// Execute chạy Plan; job thành công thì site về active trong bảng sites.
func (r *SiteRestore) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	s := r.sites
	rep, err := s.runPlan(ctx, "khôi phục site", r.Plan, onEvent)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		return rep, err
	}
	st := r.Site
	st.Status, st.TrashedAt, st.PurgeAt = site.Active, time.Time{}, time.Time{}
	if err := s.Store.SaveSite(ctx, st); err != nil {
		// Chạy lại thì mọi bước đã đúng được bỏ qua, chỉ còn ghi DB.
		return rep, fmt.Errorf("job %s đã khôi phục site %s nhưng không ghi được vào DB của tpan: %w. Chạy lại `tpan site restore %s` để ghi nốt",
			rep.Job.ID, st.Domain, err, st.Domain)
	}
	return rep, nil
}

// ---------------------------------------------------------------- xoá hẳn

// SitePurge là một lần xoá hẳn một site đang ở thùng rác.
type SitePurge struct {
	Site   site.Site
	Layout site.Layout
	Plan   engine.Plan

	sites *Sites
}

// PreparePurge dựng Plan "site.purge". Chỉ đọc.
func (s *Sites) PreparePurge(ctx context.Context, d validate.Domain) (*SitePurge, error) {
	st, err := s.findTrashed(ctx, d)
	if err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	ss, err := purgeSteps(cfg.Web, sitePHP(st, cfg), st)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan xoá hẳn site %s: %w", d, err)
	}
	return &SitePurge{Site: st, Layout: st.Layout(), sites: s, Plan: engine.Plan{
		Kind:   "site.purge",
		Target: d.String(),
		Title:  i18n.T("Xoá hẳn site "+d.String(), "Permanently delete site "+d.String()),
		Steps:  ss,
	}}, nil
}

// purgeSteps là Plan xoá hẳn của M2 (mục 6.4) cho site đã ở thùng rác: gỡ
// link, file vhost và pool trước (link và pool đã gỡ lúc vào thùng rác nên tự
// bỏ qua), rồi mới xoá DB, file, log, user (không hoàn tác được). Không có
// phép kiểm chứng: kiểm chứng lỗi thì engine hoàn tác, mà hoàn tác lúc này chỉ
// trả lại file vhost cho một site đã mất DB và file, tệ hơn là để yên.
func purgeSteps(web WebServer, php string, st site.Site) ([]engine.Step, error) {
	l := st.Layout()
	ss, err := steps.SiteWebAbsent(l, string(web), php)
	if err != nil {
		return nil, err
	}
	ss = append(ss,
		&steps.SiteDBAbsent{Layout: l},
		&steps.RemoveTree{Path: site.TrashDir(l), Under: site.TrashRoot},
		&steps.RemoveTree{Path: l.LogDir, Under: siteLogRoot},
		&steps.SiteUserAbsent{Layout: l},
	)
	if st.SSL {
		// Xoá chứng chỉ ở CUỐI (mục 6.4): bước nào trước đó lỗi thì engine hoàn
		// tác trả lại file vhost 443, mà vhost đó trỏ tới chứng chỉ; chứng chỉ đã
		// mất thì kiểm config lúc hoàn tác lỗi theo. Để cuối thì lỗi ở đây chỉ
		// còn sót chứng chỉ, chạy lại `trash purge` dọn nốt.
		ss = append(ss, &steps.CertbotDelete{Layout: l})
	}
	return ss, nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (p *SitePurge) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(p.sites.Sys, engine.Options{}).Preview(ctx, p.Plan)
}

// Execute chạy Plan; job thành công thì xoá dòng của site khỏi bảng sites.
// Job hỏng thì giữ dòng (vẫn trashed) để chạy lại làm nốt.
func (p *SitePurge) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	s := p.sites
	rep, err := s.runPlan(ctx, "xoá hẳn site", p.Plan, onEvent)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		return rep, err
	}
	if err := s.Store.DeleteSite(ctx, p.Site.Domain); err != nil {
		return rep, fmt.Errorf("job %s đã xoá hẳn site %s khỏi máy nhưng không xoá được dòng trong DB của tpan: %w", rep.Job.ID, p.Site.Domain, err)
	}
	return rep, nil
}

// PurgeResult là kết quả xoá hẳn một site trong PurgeExpired.
type PurgeResult struct {
	Domain validate.Domain `json:"domain"`
	Report engine.Report   `json:"report"`
	// Error: không dựng hay không chạy được Plan (khác job lỗi: xem Report).
	Error string `json:"error,omitempty"`
}

// PurgeExpired xoá hẳn lần lượt mọi site của Expired(now), mỗi site một job.
// Site lỗi thì ghi vào kết quả rồi đi tiếp site sau. Dùng cho `tpan trash
// purge --expired` và bộ hẹn giờ mỗi giờ trong `tpan serve`.
func (s *Sites) PurgeExpired(ctx context.Context, now time.Time, onEvent func(engine.Event)) ([]PurgeResult, error) {
	expired, err := s.Expired(ctx, now)
	if err != nil {
		return nil, err
	}
	out := []PurgeResult{}
	for _, st := range expired {
		r := PurgeResult{Domain: st.Domain}
		p, err := s.PreparePurge(ctx, st.Domain)
		if err == nil {
			r.Report, err = p.Execute(ctx, onEvent)
		}
		if err != nil {
			r.Error = err.Error()
		}
		out = append(out, r)
	}
	return out, nil
}

// ---------------------------------------------------------------- kiểm chứng

// siteGone là phép kiểm chứng của Plan đưa vào thùng rác (mục 6.4): gọi
// http://<domain>/ ngay trên máy (curl --resolve, như checks.HTTPSite) và đòi
// domain không còn được phục vụ. Đạt khi:
//
//   - trả 404 (không còn file nào để phục vụ), hoặc
//   - nginx: site mặc định đóng kết nối không trả gì (444, curl mã thoát 52);
//   - apache: site mặc định trả 403.
//
// checks.HTTPDefault không dùng được: nó gọi 127.0.0.1 không kèm tên miền,
// nên không biết vhost của chính domain này còn bật không. Viết ở core thay
// vì ở checks vì chỉ Plan thùng rác cần nó; chuyển sang checks khi health cần.
func siteGone(web WebServer, l site.Layout) checks.Check {
	d := l.Domain.String()
	url := "http://" + d + "/"
	cmd := system.Cmd{Name: "curl", Args: []string{
		"-sS", "--max-time", "5", "--resolve", d + ":80:127.0.0.1", "-o", "/dev/null", "-w", "%{http_code}", url,
	}}
	return goneCheck{
		id:    "http.gone:" + d,
		title: i18n.T("Domain "+d+" không còn được phục vụ", "Domain "+d+" is no longer served"),
		web:   web,
		cmd:   cmd,
		link:  map[WebServer]string{Nginx: l.NginxLink, Apache: l.ApacheLink}[web],
	}
}

type goneCheck struct {
	id    string
	title i18n.Text
	web   WebServer
	cmd   system.Cmd
	link  string
}

func (g goneCheck) Run(ctx context.Context, sys system.System) checks.Result {
	r := checks.Result{ID: g.id, Stage: checks.StageEndToEnd, Title: g.title}
	res, err := sys.Run(ctx, g.cmd)
	ev := checks.Evidence{Command: g.cmd.String(), ExitCode: res.ExitCode, Output: strings.TrimSpace(res.Stdout + "\n" + res.Stderr)}
	var ee *system.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		// Mã thoát khác 0 của curl là câu trả lời (52: đóng kết nối), không phải lỗi.
		res.ExitCode, ev.ExitCode = ee.Code, ee.Code
	default:
		ev.ExitCode = -1
		r.Status, r.Evidence = checks.Unknown, []checks.Evidence{ev}
		r.Summary = i18n.T("Chưa kiểm được: không chạy được curl ("+err.Error()+")", "Could not check: curl did not run ("+err.Error()+")")
		return r
	}
	r.Evidence = []checks.Evidence{ev}
	code := strings.TrimSpace(res.Stdout)
	switch {
	case res.ExitCode == 0 && code == "404",
		g.web == Nginx && res.ExitCode == 52,
		g.web == Apache && res.ExitCode == 0 && code == "403":
		r.Status = checks.Pass
		r.Summary = i18n.T("Domain không còn được phục vụ (curl mã thoát "+strconv.Itoa(res.ExitCode)+", HTTP "+code+")",
			"The domain is no longer served (curl exit "+strconv.Itoa(res.ExitCode)+", HTTP "+code+")")
		return r
	}
	r.Status = checks.Fail
	r.Summary = i18n.T("Domain vẫn trả lời (curl mã thoát "+strconv.Itoa(res.ExitCode)+", HTTP "+code+"), đáng lẽ phải 404 hoặc rơi vào site mặc định",
		"The domain still answers (curl exit "+strconv.Itoa(res.ExitCode)+", HTTP "+code+"); it should give 404 or fall into the default site")
	r.Cause = i18n.T("Link vhost của site còn bật, web server chưa reload, hoặc một vhost khác đang nhận tên miền này.",
		"The site's vhost link is still enabled, the web server was not reloaded, or another vhost answers for this name.")
	r.ManualFix = []string{
		system.Cmd{Name: "ls", Args: []string{"-l", g.link}}.String(),
		system.Cmd{Name: "grep", Args: []string{"-rl", strings.TrimPrefix(g.id, "http.gone:"), "/etc/nginx/sites-enabled", "/etc/apache2/sites-enabled"}}.String(),
		g.cmd.String(),
	}
	return r
}
