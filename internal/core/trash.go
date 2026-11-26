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
//     M9: bước đầu tự backup site (mục 6.5, xem PreparePurge), và cả ba
//     Plan gỡ hoặc ghi lại file cron của site (mục 6.10).

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
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
	// M13 (quyết định 7): khôi phục ghi lại pool và mở khoá user DB, nên với
	// một dòng còn cờ tạm khoá thì đó là mở khoá lén. Đưa vào thùng rác đã bị
	// từ chối khi còn khoá (PrepareRemove), nên đây là chốt chặn cho dòng lạ.
	if st.Suspended != account.NotSuspended || st.DBSuspended != account.NotSuspended {
		return nil, refuseSuspendedTrash(st)
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
	lim, err := siteCageLimits(ctx, s.Accounts, st)
	if err != nil {
		return nil, err
	}
	ss, err := restoreSteps(ctx, s.Accounts, cfg.Web, php, st)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan khôi phục site %s: %w", d, err)
	}
	// Site vào thùng rác lúc còn chạy nginx rồi stack chuyển sang Apache
	// (stack switch chỉ ghi .htaccess cho site active): thiếu .htaccess thì mọi
	// permalink là 404. Ghi ngay sau khi chuyển thư mục về, trước vhost.
	// Chế độ "tat" (M14, mục 6.13): Apache không đọc .htaccess của site,
	// permalink nằm trong vhost, nên không ghi.
	if cfg.Web == Apache && st.Htaccess.OrDefault() != site.HtaccessOff {
		ht, err := htaccessIfMissing(s.Sys, l, site.TrashDir(l)+"/public", st.Plain())
		if err != nil {
			return nil, err
		}
		if ht != nil {
			ss = append(ss[:1], append([]engine.Step{ht}, ss[1:]...)...)
		}
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
		Verify: siteChecks(cfg.Web, php, st, others, lim),
	}}, nil
}

// restoreSteps làm ngược trashSteps: chuyển thư mục về, mở khoá user Linux,
// ghi lại cron của site (M9), mở khoá user DB, ghi lại pool, bật vhost. Pool và vhost dựng y như lúc tạo
// site (siteVhost chọn bản 443 khi site có SSL): file vhost vẫn nằm trong
// sites-available nên bước ghi nó tự bỏ qua, chỉ bước bật link chạy.
//
// M17, site trong lồng: hai bước của sitePHPSteps đặt vào hai chỗ. CageFiles
// (hàng rào) NGAY sau khi mở khoá user, trước mọi bước chạy lệnh dưới user của
// site (bước cron có `wp config set`), để không lệnh nào của site chạy ở ngoài
// lồng (soát thiết kế điểm 8); CageSocket bật ở chỗ của pool, trước vhost.
func restoreSteps(ctx context.Context, as AccountStore, web WebServer, php string, st site.Site) ([]engine.Step, error) {
	l := st.Layout()
	cfg := StackConfig{Web: web, PHPVersion: php}
	phpSteps, err := sitePHPStepsFor(ctx, as, st, cfg)
	if err != nil {
		return nil, err
	}
	// fence: các bước phải chạy ngay sau mở khoá user (CageFiles); pool: các
	// bước ở chỗ của pool (pool chung, hoặc CageSocket).
	var fence, pool []engine.Step
	if st.Caged {
		fence, pool = phpSteps[:1], phpSteps[1:]
	} else {
		pool = phpSteps
	}
	vhost, err := siteVhost(st, cfg)
	if err != nil {
		return nil, err
	}
	// M29: site PHP trống không có wp-config.php hay cron của WordPress.
	var cron []engine.Step
	if !st.Plain() {
		if cron, err = siteCronSteps(l); err != nil {
			return nil, err
		}
	}
	d := l.Domain.String()
	ss := []engine.Step{
		&steps.MoveTree{From: site.TrashDir(l), To: l.Dir,
			Title: i18n.T("Chuyển thư mục của "+d+" ra khỏi thùng rác", "Move the directory of "+d+" out of the trash")},
		&steps.SiteUserLock{Layout: l, Unlock: true},
	}
	ss = append(ss, fence...)
	// M9: cron của site sau khi thư mục về chỗ và user được mở khoá:
	// WPConstant chạy `wp` dưới user của site trên wp-config.php vừa về.
	ss = append(ss, cron...)
	// M28 đợt 2 (S16): site vào thùng rác trước khi máy chạy harden thì user
	// DB của nó chưa có trần kết nối; khôi phục ra là đặt luôn.
	ss = append(ss, &steps.DBUserUnlock{Layout: l},
		&steps.DBUserLimit{User: l.DB, Max: site.DBMaxUserConnections})
	ss = append(ss, pool...)
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
	// backup là bước tự backup trước khi xoá hẳn (mục 6.5); nil khi bỏ qua
	// (một lần xoá hẳn trước đã chạy dở, xem PreparePurge).
	backup *purgeBackup
}

// purgeIntact: dữ liệu của site trong thùng rác còn đủ, tức thư mục trong
// thùng rác và DB đều còn. Plan xoá hẳn xoá DB trước thư mục, nên thiếu một
// trong hai là dấu của một lần xoá hẳn trước đã chạy dở.
func purgeIntact(ctx context.Context, sys system.System, l site.Layout) (bool, error) {
	dir, err := pathExists(sys, site.TrashDir(l))
	if err != nil || !dir {
		return false, err
	}
	db, _, err := steps.SiteDBState(ctx, sys, l)
	return db, err
}

// PreparePurge dựng Plan "site.purge". Chỉ đọc.
//
// M9 (mục 6.5): bước đầu tiên là tự backup site (thư mục trong thùng rác và
// DB). Chặn đầy đĩa ở đây, trước khi dựng Plan: không đủ chỗ thì từ chối
// CodeDiskSpaceLow, tức là KHÔNG xoá hẳn khi chưa backup được. Bộ hẹn giờ dọn
// thùng rác (PurgeExpired) ghi lời từ chối vào kết quả của site đó, đi tiếp
// site sau, và thử lại ở lượt mỗi giờ kế tiếp; site cứ nằm trong thùng rác
// (vẫn khôi phục được) tới khi đĩa được dọn.
//
// Dữ liệu không còn đủ (purgeIntact false: lần xoá hẳn trước đã xoá DB hoặc
// thư mục rồi lỗi ở bước sau) thì bỏ bước backup: không còn gì nguyên vẹn để
// backup, và bản tự backup của lần trước đã được giữ và ghi (purgeBackup.Undo,
// Execute). Không bỏ thì chạy lại xoá hẳn không bao giờ xong được.
func (s *Sites) PreparePurge(ctx context.Context, d validate.Domain) (*SitePurge, error) {
	st, err := s.findTrashed(ctx, d)
	if err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	l := st.Layout()
	php := sitePHP(st, cfg)
	cage := st.Caged
	if !cage {
		if cage, err = cageLeft(s.Sys, l); err != nil {
			return nil, err
		}
	}
	ss, err := purgeSteps(cfg.Web, php, st, cage)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan xoá hẳn site %s: %w", d, err)
	}
	intact, err := purgeIntact(ctx, s.Sys, l)
	if err != nil {
		return nil, fmt.Errorf("kiểm dữ liệu của site %s trong thùng rác: %w", d, err)
	}
	var bk *purgeBackup
	if intact {
		dn := d.String()
		err := (&Backups{Sites: s}).backupSpace(ctx, l, site.TrashDir(l), i18n.T("tự backup "+dn+" trước khi xoá hẳn", "back up "+dn+" before deleting it for good"))
		var re *RefusedError
		if errors.As(err, &re) && re.Code == CodeDiskSpaceLow {
			re.Message = i18n.T(
				re.Message.Vi+" Chưa xoá hẳn: tpan không xoá site khi chưa backup được. Site vẫn ở thùng rác; bộ hẹn giờ thử lại mỗi giờ.",
				re.Message.En+" Not deleted: tpan does not delete a site it could not back up. The site stays in the trash; the timer tries again every hour.")
		}
		if err != nil {
			return nil, err
		}
		bk = &purgeBackup{BackupArchive: &steps.BackupArchive{Layout: l, Src: site.TrashDir(l), Dir: site.BackupDir(l), PHPVersion: php}}
		ss = append([]engine.Step{bk}, ss...)
	}
	return &SitePurge{Site: st, Layout: l, sites: s, backup: bk, Plan: engine.Plan{
		Kind:   "site.purge",
		Target: d.String(),
		Title:  i18n.T("Xoá hẳn site "+d.String(), "Permanently delete site "+d.String()),
		Steps:  ss,
	}}, nil
}

// purgeBackup là bước tự backup của Plan xoá hẳn. Khác BackupArchive ở Undo:
// engine hoàn tác ngược mọi bước khi một bước sau lỗi (CertbotDelete đứng
// cuối và có lúc lỗi), mà các bước xoá DB, thư mục không hoàn tác được. Lúc
// đó bản backup là bản duy nhất còn lại của site, nên chỉ xoá nó (như
// BackupArchive.Undo) khi dữ liệu gốc còn nguyên (purgeIntact); không thì giữ
// file và đánh dấu kept để Execute ghi dòng cho nó.
type purgeBackup struct {
	*steps.BackupArchive
	kept bool
}

func (p *purgeBackup) Info() engine.StepInfo {
	info := p.BackupArchive.Info()
	d := p.Layout.Domain.String()
	info.Title = i18n.T("Tự backup "+d+" trước khi xoá hẳn, vào "+p.Dir, "Back up "+d+" before deleting it for good, to "+p.Dir)
	info.Why = i18n.T(
		"Các bước sau xoá DB và file của site, không lấy lại được. Bản backup này là đường lùi cuối cùng: tpan không bao giờ tự dọn nó. Khi hoàn tác, file chỉ bị xoá nếu dữ liệu gốc còn nguyên.",
		"The next steps delete the site's database and files for good. This backup is the last way back: tpan never prunes it. On undo, the file is only removed if the original data is still intact.")
	return info
}

func (p *purgeBackup) Undo(ctx context.Context, sys system.System) error {
	intact, err := purgeIntact(ctx, sys, p.Layout)
	if err != nil {
		// Không biết dữ liệu gốc còn không: giữ file (hướng an toàn).
		p.kept = true
		return fmt.Errorf("giữ bản tự backup %s (không kiểm được dữ liệu gốc của %s: %w)", p.Path, p.Layout.Domain, err)
	}
	if intact {
		return p.BackupArchive.Undo(ctx, sys)
	}
	p.kept = true
	return nil
}

// purgeSteps là Plan xoá hẳn của M2 (mục 6.4) cho site đã ở thùng rác: gỡ
// link, file vhost và pool trước (link và pool đã gỡ lúc vào thùng rác nên tự
// bỏ qua), rồi mới xoá DB, file, log, user (không hoàn tác được). Không có
// phép kiểm chứng: kiểm chứng lỗi thì engine hoàn tác, mà hoàn tác lúc này chỉ
// trả lại file vhost cho một site đã mất DB và file, tệ hơn là để yên.
//
// M17: cage true (site có cờ caged, hoặc máy còn file của lồng: lồng dựng dở
// sau một lần panel bị tắt giữa cage.enable) thì thêm CageSocket{Disable} và
// CageFiles{Remove}, trước các bước xoá không hoàn tác được và trước khi xoá
// user (unit của lồng chạy dưới user đó). Site chưa từng có lồng: Plan y như
// trước M17.
func purgeSteps(web WebServer, php string, st site.Site, cage bool) ([]engine.Step, error) {
	l := st.Layout()
	ss, err := steps.SiteWebAbsent(l, string(web), php)
	if err != nil {
		return nil, err
	}
	// M9: file cron đã gỡ lúc vào thùng rác; còn (sửa tay) thì gỡ nốt.
	ss = append(ss, siteCronAbsent(l))
	if cage {
		ss = append(ss,
			&steps.CageSocket{Layout: l, PHP: php, Disable: true},
			&steps.CageFiles{Layout: l, PHP: php, Remove: true},
		)
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

// cageLeft: máy còn file hay thư mục nào của lồng của site l (đọc máy như lớp
// bọc lệnh, không tin cờ caged: cờ chỉ ghi khi job cage.enable thành công).
func cageLeft(sys system.System, l site.Layout) (bool, error) {
	for _, p := range append(l.CageFiles(), l.CageFenceDir(), l.CageConfDir()) {
		ok, err := pathExists(sys, p)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (p *SitePurge) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(p.sites.Sys, engine.Options{}).Preview(ctx, p.Plan)
}

// Execute chạy Plan; job thành công thì ghi dòng của bản tự backup (Kind full,
// ReasonBeforePurge) rồi mới xoá dòng của site khỏi bảng sites. Thứ tự đó để
// dừng giữa chừng thì bảng backups đã biết bản backup trước khi site biến mất.
// Job hỏng thì giữ dòng site (vẫn trashed) để chạy lại làm nốt; bản backup
// được giữ (purgeBackup.kept) thì vẫn ghi dòng cho nó.
//
// Không ghi được dòng backup (Sites.Backups nil, store lỗi) thì vẫn xoá dòng
// site (máy đã xoá hẳn rồi), và trả lỗi nêu đường dẫn file backup để người
// biết nó ở đâu.
func (p *SitePurge) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	s := p.sites
	rep, err := s.runPlan(ctx, "xoá hẳn site", p.Plan, onEvent)
	if err != nil {
		return rep, err
	}
	if rep.Job.Status != engine.JobSucceeded {
		if p.backup != nil && p.backup.kept {
			return rep, p.recordBackup(ctx, rep)
		}
		return rep, nil
	}
	var errs []error
	if p.backup != nil {
		errs = append(errs, p.recordBackup(ctx, rep))
	}
	if err := s.Store.DeleteSite(ctx, p.Site.Domain); err != nil {
		errs = append(errs, fmt.Errorf("job %s đã xoá hẳn site %s khỏi máy nhưng không xoá được dòng trong DB của tpan: %w", rep.Job.ID, p.Site.Domain, err))
	}
	// M17: số liệu của domain đi theo site, để site mới cùng tên không thừa
	// hưởng số cũ (mẫu, dòng giờ, gợi ý trần).
	if s.Samples != nil {
		if err := s.Samples.DeleteUsageOf(ctx, p.Site.Domain.String()); err != nil {
			errs = append(errs, fmt.Errorf("job %s đã xoá hẳn site %s nhưng không xoá được số liệu của nó: %w", rep.Job.ID, p.Site.Domain, err))
		}
	}
	return rep, errors.Join(errs...)
}

// recordBackup ghi dòng của bản tự backup trước khi xoá hẳn. Bản này không bao
// giờ bị dọn (Backups.prune bỏ qua ReasonBeforePurge).
func (p *SitePurge) recordBackup(ctx context.Context, rep engine.Report) error {
	s, d := p.sites, p.Site.Domain
	row, err := archived(p.backup.BackupArchive, d, ReasonBeforePurge, rep.Job.ID)
	if err != nil {
		return err
	}
	if s.Backups == nil {
		return fmt.Errorf("job %s: đã tự backup %s vào %s nhưng chưa có bảng backups để ghi dòng; file vẫn còn trên máy", rep.Job.ID, d, row.Path)
	}
	row.Created = s.now().UTC()
	if _, err := s.Backups.AddBackup(ctx, row); err != nil {
		return fmt.Errorf("job %s: đã tự backup %s vào %s nhưng không ghi được vào bảng backups (file vẫn còn trên máy): %w", rep.Job.ID, d, row.Path, err)
	}
	return nil
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

// goneWait, goneEvery: trần và nhịp gọi lại của goneCheck. Biến để test rút ngắn.
var (
	goneWait  = 5 * time.Second
	goneEvery = 200 * time.Millisecond
)

// Run gọi lại tới khi đạt hoặc hết goneWait. Lý do: `systemctl reload nginx`
// chỉ gửi tín hiệu cho tiến trình chính rồi trả về; request tới ngay sau đó
// vẫn được phục vụ theo cấu hình cũ. Đo trên máy ảo (M6): 0,012 giây sau
// reload site có SSL còn trả 301 (vhost cũ), 0,17 giây sau thì 444. Không
// gọi lại thì Plan đưa vào thùng rác của site có SSL bị hoàn tác oan (site
// không SSL thì cấu hình cũ trả 404 vì thư mục đã chuyển, nên lọt). Unknown
// (curl không chạy được) trả ngay.
func (g goneCheck) Run(ctx context.Context, sys system.System) checks.Result {
	deadline := time.Now().Add(goneWait)
	for {
		r := g.once(ctx, sys)
		if r.Status != checks.Fail || time.Now().After(deadline) {
			return r
		}
		select {
		case <-ctx.Done():
			return r
		case <-time.After(goneEvery):
		}
	}
}

// once là một lần gọi curl và phán.
func (g goneCheck) once(ctx context.Context, sys system.System) checks.Result {
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
