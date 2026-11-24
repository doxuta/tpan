package core

// Dựng Plan sửa tự động cho một kết quả phép kiểm (mục 6.7 của
// docs/KE-HOACH.md). Mỗi cách sửa chỉ dùng Step có sẵn hoặc Step nhỏ của
// steps/fix.go, và Verify là đúng phép kiểm đã báo lỗi, dựng lại từ ID: job
// sửa chỉ thành công khi phép kiểm đó đã đạt.
//
// Đối tượng trong ID (domain, unit, đường dẫn) đến từ bên ngoài (API, CLI),
// nên không bao giờ đi thẳng vào Step: domain qua validate.ParseDomain và phải
// là site active trong Store; unit phải nằm trong bộ dịch vụ của stack; đường
// dẫn phải trùng đúng một file tpan tự dựng lại được.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// noAutoFix là lời từ chối khi không có cách sửa tự động cho id.
func noAutoFix(id string) *RefusedError {
	return refuse("no_auto_fix", i18n.T(
		"không có cách sửa tự động cho phép kiểm "+id+"; xem cách sửa tay trong kết quả phép kiểm",
		"there is no automatic fix for check "+id+"; see the manual fix in the check result"))
}

// plan dựng Plan (chưa có Kind) cho cách sửa name của r.
func (f *Fixes) plan(ctx context.Context, name string, r checks.Result) (engine.Plan, error) {
	// Tách ở dấu ":" đầu tiên: phần đối tượng (đường dẫn) có thể chứa thêm ":".
	kind, obj, _ := strings.Cut(r.ID, ":")
	cfg, err := (&Sites{Settings: f.Settings}).stack(ctx)
	if err != nil {
		return engine.Plan{}, err
	}
	switch name {
	case FixServiceStart:
		return f.serviceStart(ctx, cfg, r.ID, obj)
	case FixPHPRestart:
		st, err := f.activeSite(ctx, r.ID, obj)
		if err != nil {
			return engine.Plan{}, err
		}
		// Site tạm khoá không có pool: khởi động lại PHP-FPM không tạo được
		// socket của nó, và kiểm chứng PoolSocket chắc chắn hỏng.
		if err := refuseSuspendedPool(st); err != nil {
			return engine.Plan{}, err
		}
		// M17: site trong lồng không có pool chung, php.socket không sinh cho
		// nó; khởi động lại PHP-FPM chung không chạm gì của lồng.
		if st.Caged {
			return engine.Plan{}, noAutoFix(r.ID)
		}
		l := st.Layout()
		php := sitePHP(st, cfg)
		return engine.Plan{
			Target: l.Domain.String(),
			Title:  i18n.T("Khởi động lại PHP-FPM "+php+" để tạo lại socket của "+obj, "Restart PHP-FPM "+php+" to recreate the socket of "+obj),
			Steps:  []engine.Step{&steps.PHPFPMRestart{PHPVersion: php, Socket: l.SocketFor(php)}},
			Verify: []checks.Check{checks.PoolSocket(l, php)},
		}, nil
	case FixFileRewrite:
		// M17: file của lồng ghi lại bằng chính bước CageFiles (daemon-reload,
		// php-fpm -t trong lồng, try-restart), không bằng một bước File trần.
		if plan, ok, err := f.cageFileRewrite(ctx, cfg, r.ID, obj); ok || err != nil {
			return plan, err
		}
		step, file, err := f.managedFile(ctx, cfg, obj)
		if err != nil {
			return engine.Plan{}, err
		}
		if file == nil {
			return engine.Plan{}, noAutoFix(r.ID)
		}
		sum := sha256.Sum256(file.Content)
		return engine.Plan{
			Target: obj,
			Title:  i18n.T("Ghi lại "+obj+" từ bản của tpan", "Rewrite "+obj+" from tpan's version"),
			Steps:  []engine.Step{step},
			Verify: []checks.Check{checks.Drift(obj, hex.EncodeToString(sum[:]), checks.DriftStage(obj))},
		}, nil
	case FixWebRewrite:
		if kind == "web.vhost" || kind == "web.hardened" {
			return f.vhostRewrite(ctx, cfg, r.ID, obj)
		}
		return f.webRewrite(ctx, cfg, r.ID, kind)
	case FixPHPHarden:
		// obj là bản PHP, lấy từ ID của phép kiểm: kiểm dạng trước khi nó đi
		// vào đường dẫn và tên dịch vụ.
		if err := checkPHPVersion(obj); err != nil {
			return engine.Plan{}, noAutoFix(r.ID)
		}
		ss, err := steps.PHPHardening(obj)
		if err != nil {
			return engine.Plan{}, err
		}
		return engine.Plan{
			Target: obj,
			Title:  i18n.T("Siết OPcache và /proc của PHP-FPM "+obj, "Harden OPcache and /proc for PHP-FPM "+obj),
			Steps:  ss,
			Verify: []checks.Check{checks.OPcacheHardened(obj), checks.PHPProtectProc(obj)},
		}, nil
	case FixLogPerms:
		st, err := f.activeSite(ctx, r.ID, obj)
		if err != nil {
			return engine.Plan{}, err
		}
		l := st.Layout()
		// File logrotate đi cùng quyền log: thư mục log 0750 mà postrotate của
		// bản trước M14 còn gửi USR1 cho nginx thì sau lần xoay vòng đầu, worker
		// không mở lại được file và log mới rơi vào access.log.1 (t8 ở
		// docs/bang-chung/M14-nghien-cuu/). File đã đúng thì bước được bỏ qua.
		lr, err := logrotateStep(cfg.Web)
		if err != nil {
			return engine.Plan{}, err
		}
		return engine.Plan{
			Target: l.Domain.String(),
			Title:  i18n.T("Đặt lại quyền thư mục và file log của "+obj, "Reset the log directory and file permissions of "+obj),
			Steps:  append([]engine.Step{lr}, steps.SiteLogs(l)...),
			Verify: []checks.Check{checks.LogPerms(l)},
		}, nil
	case FixWPConfigPerm:
		st, err := f.activeSite(ctx, r.ID, obj)
		if err != nil {
			return engine.Plan{}, err
		}
		l := st.Layout()
		return engine.Plan{
			Target: l.Domain.String(),
			Title:  i18n.T("Đặt lại quyền wp-config.php của "+obj, "Reset the permissions of wp-config.php for "+obj),
			Steps:  []engine.Step{&steps.WPConfigPerm{Layout: l}},
			Verify: []checks.Check{checks.WPConfigPerm(l)},
		}, nil
	case FixDBUnlock:
		st, err := f.activeSite(ctx, r.ID, obj)
		if err != nil {
			return engine.Plan{}, err
		}
		// DB đang tạm khoá có chủ ý (M13): mở khoá user DB ở đây là mở khoá
		// lén, từ một kết quả db.access cũ còn nằm trong DB.
		if err := refuseDBSuspended(st); err != nil {
			return engine.Plan{}, err
		}
		l := st.Layout()
		return engine.Plan{
			Target: l.Domain.String(),
			Title:  i18n.T("Mở khoá user DB của "+obj, "Unlock the database user of "+obj),
			Steps:  []engine.Step{&steps.DBUserUnlock{Layout: l}},
			Verify: []checks.Check{checks.DBAccess(l)},
		}, nil
	case FixCageStart, FixCageRestart, FixCageSync:
		st, err := f.cagedSite(ctx, r.ID, obj)
		if err != nil {
			return engine.Plan{}, err
		}
		l := st.Layout()
		switch name {
		case FixCageStart:
			return engine.Plan{
				Target: obj,
				Title:  i18n.T("Bật lại socket của lồng của "+obj, "Start the cage socket of "+obj+" again"),
				Steps:  []engine.Step{&steps.CageSocket{Layout: l, PHP: sitePHP(st, cfg)}},
				Verify: []checks.Check{checks.CageSocket(l)},
			}, nil
		case FixCageRestart:
			pl, err := f.cages().PrepareRestart(ctx, st.Domain)
			if err != nil {
				return engine.Plan{}, err
			}
			return pl.Plan, nil
		}
		// cage.sync: trần của gói trong bảng (cageLimitsOf báo lỗi khi thiếu
		// Accounts: trần 0 sẽ xoá trần của site).
		lim, _, err := cageLimitsOf(ctx, f.Accounts, st)
		if err != nil {
			return engine.Plan{}, err
		}
		return f.cages().syncPlan(cfg, obj, []site.Site{st}, lim,
			i18n.T("Ghi lại file của lồng của "+obj+" với trần của gói", "Rewrite the cage files of "+obj+" with the package limits")), nil
	}
	return engine.Plan{}, noAutoFix(r.ID)
}

// cages dựng Cages từ các trường của Fixes.
func (f *Fixes) cages() *Cages {
	return &Cages{Sys: f.Sys, Settings: f.Settings, Store: f.Store, Accounts: f.Accounts, Journal: f.Journal}
}

// cagedSite như activeSite, thêm: site phải đang ở trong lồng và không tạm khoá
// (socket của lồng bị tắt có chủ ý khi khoá; bật lại là mở khoá lén).
func (f *Fixes) cagedSite(ctx context.Context, id, obj string) (site.Site, error) {
	st, err := f.activeSite(ctx, id, obj)
	if err != nil {
		return site.Site{}, err
	}
	if !st.Caged {
		return site.Site{}, noAutoFix(id)
	}
	if err := refuseSuspendedPool(st); err != nil {
		return site.Site{}, err
	}
	return st, nil
}

// plannedFix dựng sửa tự động mà Plan có after (ghi bảng sau khi job xong):
// cage.enable, cage.disable (ghi sites.caged) và probe.cleanup (đánh dấu lượt
// thử đã dọn). Fix.Execute chạy qua Planned để after không bị bỏ.
func (f *Fixes) plannedFix(ctx context.Context, name string, r checks.Result) (*Planned, error) {
	kind, obj, _ := strings.Cut(r.ID, ":")
	if name == FixProbeCleanup {
		if f.Runs == nil || r.ID != "probe.leftover:tpan" {
			return nil, noAutoFix(r.ID)
		}
		pl, err := (&Probe{Sys: f.Sys, Settings: f.Settings, Store: f.Store, Runs: f.Runs, Journal: f.Journal}).PrepareCleanup(ctx)
		if err != nil {
			return nil, err
		}
		if pl == nil {
			return nil, refuse("no_auto_fix", i18n.T(
				"không còn lượt thử tấn công nào chưa dọn; chạy lại health để cập nhật kết quả",
				"no attack probe run is left to clean up; run health again to refresh the result"))
		}
		return pl, nil
	}
	st, err := f.activeSite(ctx, r.ID, obj)
	if err != nil {
		return nil, err
	}
	c := f.cages()
	var pl *Planned
	if name == FixCageEnable {
		pl, err = c.PrepareEnable(ctx, st.Domain)
	} else {
		pl, err = c.PrepareDisable(ctx, st.Domain)
	}
	if err != nil {
		return nil, err
	}
	// Sửa của cage.state: kiểm chứng có cả chính phép kiểm đó (cứng), với trạng
	// thái mà after sắp ghi.
	if kind == "cage.state" {
		p, err := sitePackage(ctx, f.Accounts, st)
		if err != nil {
			return nil, err
		}
		pl.Plan.Verify = append(pl.Plan.Verify, checks.CageState(st.Layout(), name == FixCageEnable, p.Cage))
	}
	return pl, nil
}

// cageFileRewrite: obj là một trong năm file quản lý của lồng của một site
// active đang trong lồng thì dựng Plan ghi lại bằng CageFiles (trần của gói);
// sha256 mong đợi lấy từ CageFiles.Want (mục 3.3). ok=false khi obj không phải
// file của lồng nào: người gọi tìm tiếp trong các file khác.
func (f *Fixes) cageFileRewrite(ctx context.Context, cfg StackConfig, id, obj string) (engine.Plan, bool, error) {
	sites, err := f.activeSites(ctx)
	if err != nil {
		return engine.Plan{}, false, err
	}
	for _, st := range sites {
		l := st.Layout()
		if !st.Caged || !l.OwnsCagePath(obj) {
			continue
		}
		lim, _, err := cageLimitsOf(ctx, f.Accounts, st)
		if err != nil {
			return engine.Plan{}, true, err
		}
		cf := &steps.CageFiles{Layout: l, PHP: sitePHP(st, cfg), Limits: lim}
		want, ok := cf.Want(obj)
		if !ok {
			// passwd, group: nội dung lấy từ getent lúc chạy, không có sha256 để so.
			return engine.Plan{}, true, noAutoFix(id)
		}
		sum := sha256.Sum256(want)
		return engine.Plan{
			Target: obj,
			Title:  i18n.T("Ghi lại "+obj+" từ bản của tpan", "Rewrite "+obj+" from tpan's version"),
			Steps:  []engine.Step{cf},
			Verify: []checks.Check{checks.Drift(obj, hex.EncodeToString(sum[:]), checks.DriftStage(obj))},
		}, true, nil
	}
	return engine.Plan{}, false, nil
}

// dropAccepted bỏ khỏi ss các bước ghi file mà nội dung đang có khớp đúng
// sha256 trong managed_files nhưng khác template: đó là bản sửa tay người dùng
// đã nhận làm chuẩn (POST /api/v1/files/accept). web.rewrite chỉ sửa file bị
// lệch hay hỏng; ghi đè bản đã nhận là xoá việc người dùng vừa chọn, và bước
// đó còn chạy `nginx -t` trong lúc file hỏng thật chưa được ghi lại, nên cả
// job hỏng và hoàn tác (tìm thấy ở e2e M4: nhận bản sửa tay của site a rồi
// gây lỗi vhost site b). File không có dòng, không đọc được, hay lệch thì giữ.
func (f *Fixes) dropAccepted(ctx context.Context, ss []engine.Step) ([]engine.Step, error) {
	if f.Files == nil {
		return ss, nil
	}
	out := ss[:0:0]
	for _, s := range ss {
		if x, ok := s.(*steps.File); ok {
			want, rec, err := f.Files.FileSHA256(ctx, x.Path)
			if err != nil {
				return nil, err
			}
			if rec {
				if cur, err := f.Sys.ReadFile(x.Path); err == nil && !bytes.Equal(cur, x.Content) {
					sum := sha256.Sum256(cur)
					if strings.EqualFold(hex.EncodeToString(sum[:]), want) {
						continue
					}
				}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// activeSite đọc domain từ ID, đòi nó đúng dạng chuẩn và là site active.
func (f *Fixes) activeSite(ctx context.Context, id, obj string) (site.Site, error) {
	d, err := validate.ParseDomain(obj)
	if err != nil {
		return site.Site{}, err
	}
	// ParseDomain hạ chữ hoa, bỏ khoảng trắng: ID của phép kiểm luôn ở dạng
	// chuẩn, nên khác dạng chuẩn là ID không do tpan sinh.
	if d.String() != obj {
		return site.Site{}, noAutoFix(id)
	}
	st, err := (&Sites{Store: f.Store}).find(ctx, d)
	if err != nil {
		return site.Site{}, err
	}
	if st.Status != site.Active {
		return site.Site{}, refuse("no_auto_fix", i18n.T(
			"site "+obj+" không ở trạng thái active; không sửa tự động",
			"site "+obj+" is not active; no automatic fix"))
	}
	return st, nil
}

// sitePHP là bản PHP của site, hoặc của stack khi trống (site của M2).
func sitePHP(st site.Site, cfg StackConfig) string {
	if st.PHPVersion != "" {
		return st.PHPVersion
	}
	return cfg.PHPVersion
}

// activeSites trả mọi site active.
func (f *Fixes) activeSites(ctx context.Context) ([]site.Site, error) {
	all, err := f.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	var out []site.Site
	for _, st := range all {
		if st.Status == site.Active {
			out = append(out, st)
		}
	}
	return out, nil
}

// serviceStart: chỉ unit thuộc bộ dịch vụ của stack (web server, MariaDB,
// php<v>-fpm của stack và của từng site active). Unit lạ trong ID thì không:
// `systemctl enable --now <unit bất kỳ>` bằng root là bật được thứ không phải
// của tpan.
func (f *Fixes) serviceStart(ctx context.Context, cfg StackConfig, id, unit string) (engine.Plan, error) {
	stages := map[string]checks.Stage{
		webUnit(cfg.Web):                checks.StageWeb,
		"mariadb":                       checks.StageDB,
		"php" + cfg.PHPVersion + "-fpm": checks.StagePHP,
	}
	sites, err := f.activeSites(ctx)
	if err != nil {
		return engine.Plan{}, err
	}
	for _, st := range sites {
		stages["php"+sitePHP(st, cfg)+"-fpm"] = checks.StagePHP
	}
	stage, ok := stages[unit]
	if !ok {
		return engine.Plan{}, noAutoFix(id)
	}
	return engine.Plan{
		Target: unit,
		Title:  i18n.T("Chạy lại dịch vụ "+unit, "Start the "+unit+" service again"),
		Steps:  []engine.Step{&steps.Service{Unit: unit}},
		Verify: []checks.Check{checks.Service(unit, stage)},
	}, nil
}

// stackPlan dựng lại Plan cài stack đúng như lúc cài, để lấy các Step File
// của stack mà không chép lại danh sách (hai nơi khai sẽ lệch nhau).
func (f *Fixes) stackPlan(cfg StackConfig) (engine.Plan, error) {
	pkgs, err := Packages(cfg)
	if err != nil {
		return engine.Plan{}, err
	}
	php := cfg.PHPVersion
	version, sha := (&Stack{WPCLISHA512: f.WPCLISHA512}).wpcli()
	return installPlan(cfg, pkgs, &php, version, sha)
}

// siteFiles trả các Step File tpan ghi cho site st với web server hiện tại:
// pool, vhost, và .htaccess khi là Apache. Site tạm khoá (M13) không có pool:
// file pool đã đổi tên .suspended, và ghi lại nó là mở khoá lén. Site trong
// lồng cần trần của gói: dùng siteFilesLim.
func siteFiles(cfg StackConfig, st site.Site) ([]*steps.File, error) {
	return siteFilesLim(cfg, st, nil)
}

// siteFilesLim như siteFiles; lim là trần của lồng (siteCageLimits), bắt buộc
// khi st.Caged. Site trong lồng (M17): năm file quản lý của lồng (nội dung từ
// CageFiles.Want, kể cả khi tạm khoá: khoá chỉ tắt socket) thay cho file pool.
// Các File này chỉ để đối chiếu; ghi lại file của lồng đi qua CageFiles
// (Fixes.cageFileRewrite), vì cần daemon-reload và php-fpm -t trong lồng.
func siteFilesLim(cfg StackConfig, st site.Site, lim *account.Limits) ([]*steps.File, error) {
	l := st.Layout()
	var ss []engine.Step
	switch {
	case st.Caged:
		if lim == nil {
			return nil, fmt.Errorf("dựng file của lồng %s: thiếu trần của gói", st.Domain)
		}
		cf := &steps.CageFiles{Layout: l, PHP: sitePHP(st, cfg), Limits: *lim}
		for _, p := range l.CageManagedFiles() {
			if b, ok := cf.Want(p); ok {
				ss = append(ss, &steps.File{Path: p, Content: b, Perm: 0o644, Owner: system.Root})
			}
		}
	case st.Suspended == account.NotSuspended:
		pool, err := sitePHPSteps(st, cfg, account.Limits{})
		if err != nil {
			return nil, err
		}
		ss = append(ss, pool...)
	}
	vhost, err := siteVhost(st, cfg)
	if err != nil {
		return nil, err
	}
	ss = append(ss, vhost...)
	// Chế độ "tat" (M14): tpan không ghi .htaccess cho site.
	if cfg.Web == Apache && st.Htaccess.OrDefault() != site.HtaccessOff {
		ht, err := steps.SiteHtaccess(l)
		if err != nil {
			return nil, err
		}
		ss = append(ss, ht)
	}
	return files(ss), nil
}

// files lọc các Step File.
func files(ss []engine.Step) []*steps.File {
	var out []*steps.File
	for _, s := range ss {
		// Pool của site ngoài lồng đi qua sitePHPSteps nên được bọc cageAbsentPool.
		if g, ok := s.(*cageAbsentPool); ok {
			s = g.Step
		}
		if f, ok := s.(*steps.File); ok {
			out = append(out, f)
		}
	}
	return out
}

// managedFile tìm Step File ghi đúng path: của một site active, hoặc của stack.
// So bằng dấu bằng, nên path lạ (kể cả có "..") không bao giờ khớp. Không thấy:
// nil (file tpan không dựng lại được, như unit systemd của tpan setup).
//
// Trả thêm bước dùng trong Plan sửa: với file pool chung của site ngoài lồng là
// bước bọc cageAbsentPool, để Plan sửa dựng trước khi admin đưa site vào lồng
// không ghi lại pool chung sau đó (N4 của soát cuối M17).
func (f *Fixes) managedFile(ctx context.Context, cfg StackConfig, path string) (engine.Step, *steps.File, error) {
	sites, err := f.activeSites(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, st := range sites {
		lim, err := siteCageLimits(ctx, f.Accounts, st)
		if err != nil {
			return nil, nil, err
		}
		fl, err := siteFilesLim(cfg, st, lim)
		if err != nil {
			return nil, nil, fmt.Errorf("dựng lại file của site %s: %w", st.Domain, err)
		}
		for _, file := range fl {
			if file.Path != path {
				continue
			}
			if !st.Caged && path == st.Layout().PoolConf(sitePHP(st, cfg)) {
				return &cageAbsentPool{Step: file, Layout: st.Layout()}, file, nil
			}
			return file, file, nil
		}
	}
	// Trang tạm khoá chung (M13): không thuộc site nào, cũng không thuộc stack.
	if page := steps.SuspendedPageFile(); page.Path == path {
		return page, page, nil
	}
	plan, err := f.stackPlan(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("dựng lại file của stack: %w", err)
	}
	for _, file := range files(plan.Steps) {
		if file.Path == path {
			return file, file, nil
		}
	}
	return nil, nil, nil
}

// webRewrite ghi lại mọi file web của tpan cho web server hiện tại: site mặc
// định rồi vhost của từng site active. Step File và Symlink bỏ qua thứ đã
// đúng, nên chỉ file bị sửa hỏng được ghi (và mỗi lần ghi đều qua nginx -t
// hoặc apachectl configtest trước khi reload).
func (f *Fixes) webRewrite(ctx context.Context, cfg StackConfig, id, kind string) (engine.Plan, error) {
	verify, dir := checks.NginxConfig(), "/etc/nginx/"
	want := Nginx
	if kind == "config.apache" {
		verify, dir, want = checks.ApacheConfig(), "/etc/apache2/", Apache
	}
	// Phép kiểm của web server khác với stack hiện tại: ghi file của web
	// server hiện tại không làm phép kiểm kia đạt.
	if cfg.Web != want {
		return engine.Plan{}, noAutoFix(id)
	}
	plan, err := f.stackPlan(cfg)
	if err != nil {
		return engine.Plan{}, fmt.Errorf("dựng lại site mặc định: %w", err)
	}
	var ss []engine.Step
	for _, s := range plan.Steps {
		switch x := s.(type) {
		case *steps.File:
			if strings.HasPrefix(x.Path, dir) {
				ss = append(ss, x)
			}
		case *steps.Symlink:
			if strings.HasPrefix(x.Link, dir) {
				ss = append(ss, x)
			}
		}
	}
	sites, err := f.activeSites(ctx)
	if err != nil {
		return engine.Plan{}, err
	}
	for _, st := range sites {
		vhost, err := siteVhost(st, cfg)
		if err != nil {
			return engine.Plan{}, fmt.Errorf("dựng lại vhost của %s: %w", st.Domain, err)
		}
		ss = append(ss, vhost...)
	}
	if ss, err = f.dropAccepted(ctx, ss); err != nil {
		return engine.Plan{}, err
	}
	unit := webUnit(cfg.Web)
	return engine.Plan{
		Target: unit,
		Title:  i18n.T("Ghi lại các file cấu hình "+unit+" của tpan", "Rewrite tpan's "+unit+" configuration files"),
		Steps:  ss,
		Verify: []checks.Check{verify},
	}, nil
}

// vhostRewrite sửa phép kiểm web.vhost của MỘT site (file vhost mất, bị thay,
// hoặc symlink ở sites-enabled bị xoá): ghi lại file vhost và symlink của
// riêng site đó. Không ghi lại vhost của mọi site như webRewrite: một vhost
// hỏng của site khác làm `nginx -t` của bước này lỗi và cả job hoàn tác, dù
// site được sửa không liên quan. Web server lấy từ stack (ID web.vhost không
// mang tên web server). Verify có VhostEnabled (đúng phép kiểm đã báo lỗi) và
// phép kiểm cú pháp của web server: bật lại một vhost là nạp nó vào cấu hình.
func (f *Fixes) vhostRewrite(ctx context.Context, cfg StackConfig, id, obj string) (engine.Plan, error) {
	st, err := f.activeSite(ctx, id, obj)
	if err != nil {
		return engine.Plan{}, err
	}
	ss, err := siteVhost(st, cfg)
	if err != nil {
		return engine.Plan{}, fmt.Errorf("dựng lại vhost của %s: %w", obj, err)
	}
	// File vhost người dùng đã nhận bản sửa tay làm chuẩn thì giữ; bước
	// symlink vẫn chạy nên vhost vẫn được bật lại.
	//
	// Trừ khi đang sửa web.hardened (M14): phép kiểm đó nói chính file vhost
	// thiếu dòng chặn, nên phải ghi lại nó. Mà "khớp sha256 đã ghi nhưng khác
	// template" cũng là hình dạng của một vhost do bản tpan cũ ghi (thấy trên
	// máy ảo, nâng từ v0.1.2: dropAccepted bỏ bước ghi và job hỏng ở kiểm
	// chứng). Bản xem trước hiện diff, người dùng thấy điều sẽ bị ghi đè.
	if kind, _, _ := strings.Cut(id, ":"); kind != "web.hardened" {
		if ss, err = f.dropAccepted(ctx, ss); err != nil {
			return engine.Plan{}, err
		}
	}
	l := st.Layout()
	config := checks.NginxConfig()
	if cfg.Web == Apache {
		config = checks.ApacheConfig()
	}
	verify := []checks.Check{checks.VhostEnabled(string(cfg.Web), l), config}
	// M14: vhost thường phải mang các dòng chặn (web.hardened cũng sửa bằng
	// Plan này). Vhost tạm khoá không trỏ vào public nên không có dòng nào.
	if st.Suspended == account.NotSuspended {
		verify = append(verify, checks.VhostHardened(string(cfg.Web), l, st.Htaccess))
	}
	return engine.Plan{
		Target: obj,
		Title:  i18n.T("Ghi lại và bật lại vhost của "+obj, "Rewrite and re-enable the vhost of "+obj),
		Steps:  ss,
		Verify: verify,
	}, nil
}

// siteVhost dựng bước ghi vhost đúng loại của site: có SSL (M5) thì bản có
// cổng 443 và chuyển hướng 80, không thì bản chỉ cổng 80. Sửa tự động và
// phát hiện lệch phải dùng đúng bản này; dùng bản cổng 80 cho site có SSL là
// ghi đè vhost 443 và làm site mất HTTPS. Web server lấy từ cfg; bản PHP là
// của site (sitePHP), để vhost trỏ đúng socket của pool site đang dùng (M12).
//
// M13: đây là chỗ DUY NHẤT chọn giữa bản thường, bản 443 và bản tạm khoá
// (site.Suspended khác rỗng; có 443 khi site.SSL). Chuyển LEMP ⇄ LAMP, sửa tự
// động của health, xin SSL đều đi qua đây, nên không đường nào lén ghi vhost
// thường cho một site đang khoá.
func siteVhost(st site.Site, cfg StackConfig) ([]engine.Step, error) {
	web, php := string(cfg.Web), sitePHP(st, cfg)
	switch {
	case st.Suspended != account.NotSuspended:
		return steps.SiteVhostSuspended(st.Layout(), web, acmeRoot, st.SSL)
	case st.Caged:
		// M17: site trong lồng, vhost trỏ socket của lồng (mục 3.6). Sau nhánh
		// tạm khoá: site trong lồng đang khoá vẫn ra bản tạm khoá.
		return steps.SiteVhostCaged(st.Layout(), web, acmeRoot, st.SSL, st.Htaccess.OrDefault())
	case st.SSL:
		return steps.SiteVhostSSL(st.Layout(), web, php, acmeRoot, st.Htaccess.OrDefault())
	}
	return steps.SiteVhost(st.Layout(), web, php, acmeRoot, st.Htaccess.OrDefault())
}
