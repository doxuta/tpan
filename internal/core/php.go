package core

// Ca sử dụng PHP theo từng site (mục 6.11 của docs/KE-HOACH.md). Hợp đồng M12
// đợt 0: chữ ký và tài liệu dưới đây cố định; thân hàm do thợ đợt 2 viết.
//
// Plan "php.install" (tpan php install <bản>):
//  1. Bản không có trong kho Ubuntu (apt-cache không thấy php<bản>-fpm khi chưa
//     có kho sury) thì: steps.AptRepo của kho sury (Danger, hiện rõ kho và khoá
//     sẽ thêm), ghim: cả kho 100, php-common và php<bản>* của mọi bản cài từ
//     kho này (kể cả bản đang cài) 500. Bản Ubuntu có thì không thêm kho.
//     Đi kèm kho sury: steps.HostsBlock chặn telemetry.sury.org trong
//     /etc/hosts (suryTelemetryHost trong repos.go).
//  2. AptInstall php<bản>-fpm và đúng bộ extension ở mục 3 (php<bản>-cli,
//     -mysql, -curl, -gd, -mbstring, -xml, -zip, -intl).
//  3. steps.PHPIdlePool cho bản đó (gỡ pool www của gói, thêm tpan-idle), rồi
//     steps.PHPHardening (M14, mục 6.13: file ini OPcache, drop-in ProtectProc).
//  4. Service php<bản>-fpm.
//  Kiểm chứng: dịch vụ php<bản>-fpm active, `php-fpm<bản> -t`,
//  checks.PHPPoolWWWAbsent(bản). Từ chối php_version_invalid (không dạng 8.3),
//  php_version_unavailable (kho Ubuntu và kho sury đều không có).
//
// Plan "php.use" (tpan php use <domain> <bản>), đổi không gián đoạn (6.11 ý 3):
//  1. Ghi pool của site vào bản mới (steps.SitePool(l, mới): socket mang số bản
//     mới, file /etc/php/<mới>/fpm/pool.d/tpan-<slug>.conf), kiểm, reload bản mới.
//  2. Chờ socket mới xuất hiện (tối đa 20 giây): reload trả về trước khi
//     PHP-FPM tạo xong socket (thấy trên máy ảo M12, khoảng 2 giây với 8.4).
//  3. Ghi lại vhost trỏ socket mới (siteVhost với site đã đổi PHPVersion),
//     kiểm config, reload web server.
//  4. Kiểm chứng giữa chừng: site trả 200 qua socket mới, HAI lần liên tiếp
//     (worker cũ của web server có thể còn trả lời qua socket cũ); Fail thì
//     Step lỗi và các bước trước hoàn tác: vhost về socket cũ, pool mới bị gỡ.
//  5. Cuối cùng mới gỡ pool ở bản cũ (Absent, reload bản cũ).
//  Kiểm chứng: siteChecks của site với bản mới. Thành công thì after ghi
//  site.PHPVersion mới vào bảng sites. Từ chối: site_not_found, site_not_active,
//  php_not_installed (bản mới chưa cài: chỉ sang `tpan php install`),
//  php_same_version (đã ở bản đó; không phải lỗi với CLI, in "không đổi gì").
//
// Plan "php.remove" (tpan php remove <bản>): chỉ khi không site nào dùng và
// không phải bản mặc định (php_in_use, php_default). AptRemove php<bản>-* đã
// cài (Danger, NoUndo). Bản gỡ là bản cuối cùng cài từ kho sury thì gỡ thêm
// dòng chặn telemetry trong /etc/hosts (lastSuryPHP).

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// List trả danh sách bản PHP (đã cài và có trong kho). notes: hỏi php.net và
// WordPress.org (curl qua Sys, timeout ngắn, lỗi thì bỏ ghi chú, không lỗi).
func (p *PHP) List(ctx context.Context, notes bool) (PHPList, error) {
	cfg, err := (&Stack{Settings: p.Settings}).Config(ctx)
	if err != nil {
		return PHPList{}, err
	}
	installed, err := phpInstalled(p.Sys)
	if err != nil {
		return PHPList{}, err
	}
	avail, err := phpAvailable(ctx, p.Sys)
	if err != nil {
		return PHPList{}, err
	}
	all := phpUnion(installed, avail)
	src, err := phpSources(ctx, p.Sys, all)
	if err != nil {
		return PHPList{}, err
	}
	sury, err := repoEnabled(p.Sys, RepoSury)
	if err != nil {
		return PHPList{}, err
	}
	used, err := p.sitesByVersion(ctx, cfg)
	if err != nil {
		return PHPList{}, err
	}

	list := PHPList{Versions: make([]PHPVersionRow, 0, len(all)), SuryEnabled: sury}
	for _, v := range all {
		list.Versions = append(list.Versions, PHPVersionRow{
			Version:   v,
			Installed: slices.Contains(installed, v),
			Default:   v == cfg.PHPVersion,
			Sites:     append([]string{}, used[v]...),
			Source:    src[v].source(),
		})
	}
	if notes {
		p.notes(ctx, &list, cfg.PHPVersion)
	}
	return list, nil
}

// PrepareInstall dựng Plan "php.install". Chỉ đọc máy.
func (p *PHP) PrepareInstall(ctx context.Context, version string) (*Planned, error) {
	if err := checkPHPVersion(version); err != nil {
		return nil, err
	}
	if _, err := (&Sites{Settings: p.Settings}).stack(ctx); err != nil {
		return nil, err
	}
	installed, err := phpInstalled(p.Sys)
	if err != nil {
		return nil, err
	}
	avail, err := phpAvailable(ctx, p.Sys)
	if err != nil {
		return nil, err
	}
	// madison chỉ hỏi gói apt đã biết (đã cài, hoặc có trong kho): gói lạ thì
	// không có dòng nào để đọc.
	ask := slices.Clone(installed)
	if slices.Contains(avail, version) {
		ask = phpUnion(ask, []string{version})
	}
	src, err := phpSources(ctx, p.Sys, ask)
	if err != nil {
		return nil, err
	}
	sury, err := repoEnabled(p.Sys, RepoSury)
	if err != nil {
		return nil, err
	}

	var ss []engine.Step
	pkgs := phpPackages(version)
	if src[version].ubuntu {
		// Bản của Ubuntu: không thêm kho nào. Danh sách gói có thể cũ hơn kho
		// (máy lâu không update) thì apt tải bản đã bị thay và lỗi 404.
		ss = append(ss, &steps.AptUpdate{Packages: pkgs})
	} else {
		if sury && !src[version].found {
			return nil, refuse("php_version_unavailable", i18n.T(
				"kho Ubuntu và kho PHP của Ondřej Surý đều không có PHP "+version+"; xem các bản có thể cài bằng: tpan php list",
				"neither the Ubuntu archive nor Ondřej Surý's PHP repository has PHP "+version+"; see the versions you can install with: tpan php list"))
		}
		// ponytail: kho sury chưa thêm thì chưa hỏi được nó có bản này không
		// (apt chỉ biết kho đã cấu hình). Bản không có thì apt-get install lỗi
		// và job hoàn tác bước thêm kho. Nâng cấp: tải Packages của kho bằng
		// curl để kiểm trước.
		codename, err := codenameOrRefuse(p.Sys)
		if err != nil {
			return nil, err
		}
		// Ghim liệt kê MỌI bản đang cài từ kho này cộng bản mới: ghi lại file
		// ghim mà thiếu một bản cũ thì bản đó tụt về 100 và lần nâng sau apt
		// đổi nó sang gói của Ubuntu (hoặc gỡ vì phụ thuộc php-common).
		fromSury := []string{version}
		for _, v := range installed {
			if f := src[v]; f.found && !f.ubuntu && v != version {
				fromSury = append(fromSury, v)
			}
		}
		repo, err := repoSury(codename, fromSury)
		if err != nil {
			return nil, fmt.Errorf("dựng kho sury: %w", err)
		}
		// Chặn telemetry TRƯỚC khi cài: postinst của php<bản>-fpm khởi động
		// dịch vụ, chỗ đầu tiên phải chờ truy vấn ra ngoài. Kho đã có từ trước
		// (máy cài trước khi có bước này) thì bước này vẫn chạy và bù dòng.
		ss = append(ss, repo, &steps.HostsBlock{Name: suryTelemetryHost})
	}
	v := version
	unit := "php" + v + "-fpm"
	// M14 (mục 6.13): file ini OPcache và drop-in ProtectProc của bản mới, để
	// site đầu tiên chuyển sang bản này đã được cô lập như ở bản mặc định.
	// Đứng TRƯỚC bước Service: gói vừa cài thường đã tự chạy dịch vụ, khi đó
	// bước drop-in restart nó (chưa site nào dùng bản này nên không ai bị ngắt);
	// còn nếu dịch vụ chưa chạy thì bước Service khởi động nó khi systemd đã
	// đọc drop-in. Cả hai đường, xong Plan là dịch vụ chạy với ProtectProc.
	harden, err := steps.PHPHardening(v)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan cài PHP %s: %w", v, err)
	}
	ss = append(ss,
		&steps.AptInstall{Packages: pkgs},
		&steps.PHPIdlePool{Version: &v, Render: templates.PHPIdlePool},
	)
	ss = append(ss, harden...)
	ss = append(ss, &steps.Service{Unit: unit})
	restarts, err := cageRestartSteps(ctx, p.Sites)
	if err != nil {
		return nil, err
	}
	ss = append(ss, restarts...)
	plan := engine.Plan{
		Kind:   "php.install",
		Target: version,
		Title:  i18n.T("Cài PHP "+version, "Install PHP "+version),
		Steps:  ss,
		Verify: []checks.Check{
			checks.Service(unit, checks.StagePHP),
			checks.PHPFPMConfig(version),
			checks.PHPPoolWWWAbsent(version),
		},
	}
	return newPlanned(p.Sys, p.Journal, plan, nil), nil
}

// cageRestartSteps là bước cuối của php.install và security.harden (M17, mục
// 3.6): khởi động lại dịch vụ PHP của mọi site trong lồng đang chạy. Hàng rào
// chỉ che những đường dẫn có lúc dịch vụ khởi động, nên thư mục pool.d của bản
// PHP mới cài (hay file vừa harden ghi) chỉ bị che sau lần khởi động lại này
// (soát thiết kế điểm 13). Dịch vụ đang nghỉ (chưa request nào đánh thức) thì
// bỏ qua: lần khởi động sau tự thấy đúng. store nil (xem trước khi chưa có
// tpan.db): không có site nào.
func cageRestartSteps(ctx context.Context, store SiteStore) ([]engine.Step, error) {
	if store == nil {
		return nil, nil
	}
	all, err := store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	var ss []engine.Step
	for _, st := range all {
		if st.Caged && st.Status == site.Active {
			ss = append(ss, &cageRestartStep{ServiceRestart: &steps.ServiceRestart{Unit: st.Layout().CageServiceUnit(), OnlyIfActive: true}})
		}
	}
	return ss, nil
}

// cageRestartStep bọc ServiceRestart của dịch vụ một lồng để lỗi khởi động lại
// không làm hỏng job của admin (php.install, security.harden là Plan chính sách,
// mục 3.6): mã của site lấp được số tiến trình của slice hay giết tiến trình
// chính, làm `systemctl restart` hỏng; site không được dùng đường đó để chặn
// việc cài bản PHP hay áp bản vá cho cả máy. Lệnh và output của nó vẫn vào log
// của job (engine ghi mọi lệnh); lồng không lên lại thì cage.socket của health
// báo, kèm sửa tự động cage.start.
type cageRestartStep struct {
	*steps.ServiceRestart
	tried bool
}

func (s *cageRestartStep) Check(ctx context.Context, sys system.System) (bool, error) {
	if s.tried {
		return true, nil
	}
	return s.ServiceRestart.Check(ctx, sys)
}

func (s *cageRestartStep) Apply(ctx context.Context, sys system.System) error {
	s.tried = true
	_ = s.ServiceRestart.Apply(ctx, sys) // lỗi bỏ qua có chủ ý: xem chú thích của kiểu
	return nil
}

// PrepareUse dựng Plan "php.use". Chỉ đọc máy.
func (p *PHP) PrepareUse(ctx context.Context, d validate.Domain, version string) (*Planned, error) {
	if err := checkPHPVersion(version); err != nil {
		return nil, err
	}
	sites := &Sites{Settings: p.Settings, Store: p.Sites}
	st, err := sites.find(ctx, d)
	if err != nil {
		return nil, err
	}
	if st.Status != site.Active {
		return nil, refuse("site_not_active", i18n.T(
			"site "+d.String()+" đang ở trạng thái "+string(st.Status)+"; chỉ đổi bản PHP cho site đang chạy (active)",
			"site "+d.String()+" is "+string(st.Status)+"; the PHP version can only be changed for an active site"))
	}
	// M13: site tạm khoá không có pool (ghi pool bản mới là mở khoá lén); DB
	// của site đang khoá thì bước kiểm site trả 200 chắc chắn hỏng.
	if err := refuseSuspendedPool(st); err != nil {
		return nil, err
	}
	if err := refuseDBSuspended(st); err != nil {
		return nil, err
	}
	cfg, err := sites.stack(ctx)
	if err != nil {
		return nil, err
	}
	old := sitePHP(st, cfg)
	if old == version {
		return nil, refuse("php_same_version", i18n.T(
			"site "+d.String()+" đã chạy PHP "+version+"; không đổi gì",
			"site "+d.String()+" already runs PHP "+version+"; nothing to change"))
	}
	if err := checkPHPVersion(old); err != nil {
		return nil, fmt.Errorf("bản PHP đang ghi cho site %s: %w", d, err)
	}
	ok, err := phpIsInstalled(p.Sys, version)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("php_not_installed", i18n.T(
			"PHP "+version+" chưa được cài; cài trước bằng: sudo tpan php install "+version,
			"PHP "+version+" is not installed; install it first with: sudo tpan php install "+version))
	}

	l := st.Layout()
	next := st
	next.PHPVersion = version
	others, err := sites.others(ctx, d)
	if err != nil {
		return nil, err
	}
	lim, err := siteCageLimits(ctx, p.Accounts, st)
	if err != nil {
		return nil, err
	}
	dom := d.String()
	after := func(ctx context.Context, _ engine.Report) error {
		if err := p.Sites.SaveSite(ctx, next); err != nil {
			return fmt.Errorf("ghi bản PHP %s cho site %s: %w", version, dom, err)
		}
		return nil
	}
	if st.Caged {
		plan, err := phpUseCaged(cfg, next, old, *lim)
		if err != nil {
			return nil, err
		}
		plan.Verify = siteChecks(cfg.Web, version, next, others, lim)
		return newPlanned(p.Sys, p.Journal, plan, after), nil
	}
	phpSteps, err := sitePHPSteps(next, cfg, account.Limits{})
	if err != nil {
		return nil, err
	}
	pool := phpSteps[0]
	vhost, err := siteVhost(next, cfg)
	if err != nil {
		return nil, err
	}
	http := checks.HTTPSite(l)
	if st.SSL {
		http = checks.HTTPSSite(l)
	}
	// Kiểm socket trước khi sửa vhost (mục 6.11 ý 3). `systemctl reload
	// php<bản>-fpm` chỉ gửi USR2 rồi trả về ngay; socket của pool mới chỉ có
	// khi master đã nạp lại xong. Trên máy ảo M12, php-fpm8.4 của kho sury mất
	// khoảng 2 giây mỗi lần khởi động (chờ một truy vấn DNS ra ngoài), nên
	// thiếu bước này thì nginx trỏ vào socket chưa có: 502 suốt 2 giây.
	ss := []engine.Step{pool, &midCheck{
		id:    "php.use.socket:" + dom,
		check: checks.PoolSocket(l, version),
		tries: socketTries,
		title: i18n.T("Chờ socket "+l.SocketFor(version)+" của PHP "+version, "Wait for socket "+l.SocketFor(version)+" of PHP "+version),
		why: i18n.T(
			"PHP-FPM nạp lại pool trong nền, sau khi lệnh reload đã trả về. Vhost chỉ được trỏ sang socket mới khi socket đó đã có, đúng chủ và quyền; không có thì job dừng ở đây, gỡ pool mới, site vẫn chạy PHP "+old+".",
			"PHP-FPM reloads its pools in the background, after the reload command has returned. The vhost only points to the new socket once it exists with the right owner and mode; otherwise the job stops here, removes the new pool, and the site keeps running PHP "+old+"."),
	}}
	ss = append(ss, vhost...)
	ss = append(ss,
		&midCheck{
			id:    "php.use.check:" + dom,
			check: http,
			// Hai lần đạt liên tiếp, cách nhau midCheckWait: `systemctl reload
			// nginx` cũng trả về trước khi worker mới nhận việc, nên lần gọi
			// đầu có thể do worker cũ (còn trỏ socket cũ) trả lời.
			need:  2,
			title: i18n.T("Kiểm "+dom+" trả 200 qua PHP "+version, "Check that "+dom+" answers 200 through PHP "+version),
			why: i18n.T(
				"Pool cũ chỉ được gỡ khi site đã chạy được trên bản mới. Site không trả 200 thì job dừng ở đây và hoàn tác: vhost về socket cũ, pool mới bị gỡ, site vẫn chạy PHP "+old+".",
				"The old pool is only removed once the site works on the new version. If the site does not answer 200 the job stops here and rolls back: the vhost points to the old socket again, the new pool is removed, and the site keeps running PHP "+old+"."),
		},
		&steps.Absent{
			Path:   l.PoolConf(old),
			Test:   &system.Cmd{Name: "php-fpm" + old, Args: []string{"-t"}},
			Reload: &system.Cmd{Name: "systemctl", Args: []string{"reload", "php" + old + "-fpm"}},
			Title:  i18n.T("Gỡ pool "+l.Pool+" ở PHP "+old, "Remove pool "+l.Pool+" from PHP "+old),
			Why: i18n.T(
				"Site đã chạy trên PHP "+version+". Pool ở PHP "+old+" không còn ai dùng mà vẫn là một cửa chạy PHP dưới user "+l.User+". Bước này hoàn tác được.",
				"The site now runs on PHP "+version+". The pool on PHP "+old+" is unused but is still a way to run PHP as "+l.User+". This step can be undone."),
		},
	)
	plan := engine.Plan{
		Kind:   "php.use",
		Target: dom,
		Title:  i18n.T("Chuyển "+dom+" từ PHP "+old+" sang "+version, "Move "+dom+" from PHP "+old+" to "+version),
		Steps:  ss,
		Verify: siteChecks(cfg.Web, version, st, others, nil),
	}
	return newPlanned(p.Sys, p.Journal, plan, after), nil
}

// phpUseCaged là Plan "php.use" của site trong lồng (mục 3.6 của
// docs/thay-doi/M17-thiet-ke.md). next là site đã mang bản mới. Khác hẳn Plan
// ngoài lồng: socket của lồng không mang số bản, nên không có pool mới, không
// đổi vhost, không đụng pool chung. CageFiles ghi lại file dịch vụ (dòng
// ExecStart của bản mới), chạy `php-fpm<mới> -t` trong lồng rồi try-restart
// (socket giữ nguyên, kết nối xếp hàng). Rồi kiểm 200 hai lần như nhánh ngoài
// lồng: đây là Plan của chủ site, site tự làm hỏng thì chỉ hỏng việc của mình.
// Hỏng thì đường trả lại của CageFiles ghi lại file cũ và chạy lại bản cũ.
func phpUseCaged(cfg StackConfig, next site.Site, old string, lim account.Limits) (engine.Plan, error) {
	l, version, dom := next.Layout(), next.PHPVersion, next.Domain.String()
	ss, err := sitePHPSteps(next, cfg, lim)
	if err != nil {
		return engine.Plan{}, err
	}
	http := checks.HTTPSite(l)
	if next.SSL {
		http = checks.HTTPSSite(l)
	}
	// Chỉ CageFiles: socket của lồng đang bật (site active, không tạm khoá),
	// bước CageSocket bật không có gì để làm ở Plan này.
	return engine.Plan{
		Kind:   "php.use",
		Target: dom,
		Title:  i18n.T("Chuyển "+dom+" (trong lồng) từ PHP "+old+" sang "+version, "Move "+dom+" (caged) from PHP "+old+" to "+version),
		Steps: []engine.Step{ss[0], &midCheck{
			id:    "php.use.check:" + dom,
			check: http,
			need:  2,
			title: i18n.T("Kiểm "+dom+" trả 200 qua PHP "+version+" trong lồng", "Check that "+dom+" answers 200 through PHP "+version+" in its cage"),
			why: i18n.T(
				"Lồng vừa khởi động lại với PHP "+version+". Site không trả 200 thì job dừng ở đây và hoàn tác: file của lồng về bản cũ và lồng chạy lại PHP "+old+".",
				"The cage was just restarted on PHP "+version+". If the site does not answer 200 the job stops here and rolls back: the cage files go back and the cage runs PHP "+old+" again."),
		}},
	}, nil
}

// PrepareRemove dựng Plan "php.remove". Chỉ đọc máy.
func (p *PHP) PrepareRemove(ctx context.Context, version string) (*Planned, error) {
	if err := checkPHPVersion(version); err != nil {
		return nil, err
	}
	cfg, err := (&Sites{Settings: p.Settings}).stack(ctx)
	if err != nil {
		return nil, err
	}
	if version == cfg.PHPVersion {
		return nil, refuse("php_default", i18n.T(
			"PHP "+version+" là bản mặc định của stack (site mới dùng bản này); không gỡ được",
			"PHP "+version+" is the stack's default version (new sites use it); it cannot be removed"))
	}
	used, err := p.sitesByVersion(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if doms := used[version]; len(doms) > 0 {
		return nil, refuse("php_in_use", i18n.T(
			"PHP "+version+" còn site dùng: "+strings.Join(doms, ", ")+". Chuyển các site đó sang bản khác trước (sudo tpan php use <domain> <bản>)",
			"PHP "+version+" is still used by: "+strings.Join(doms, ", ")+". Move those sites to another version first (sudo tpan php use <domain> <version>)"))
	}
	pkgs, err := phpInstalledPackages(ctx, p.Sys, version)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, refuse("php_not_installed", i18n.T(
			"PHP "+version+" không có gói nào đang cài; không có gì để gỡ",
			"PHP "+version+" has no installed packages; nothing to remove"))
	}
	ss := []engine.Step{&steps.AptRemove{Packages: pkgs}}
	last, err := lastSuryPHP(ctx, p.Sys, version)
	if err != nil {
		return nil, err
	}
	if last {
		ss = append(ss, &steps.HostsBlock{Name: suryTelemetryHost, Remove: true})
	}
	plan := engine.Plan{
		Kind:   "php.remove",
		Target: version,
		Title:  i18n.T("Gỡ PHP "+version, "Remove PHP "+version),
		Steps:  ss,
	}
	return newPlanned(p.Sys, p.Journal, plan, nil), nil
}

// lastSuryPHP: kho sury đã thêm và, bỏ bản version đi, không còn bản PHP nào
// cài từ kho đó. Khi đó dòng chặn telemetry của kho (suryTelemetryHost) hết
// việc và php.remove gỡ nó.
// ponytail: tpan chưa có thao tác gỡ kho, nên dòng đi theo bản PHP cuối cùng
// của kho chứ không theo file sources (file đó còn lại sau php.remove).
func lastSuryPHP(ctx context.Context, sys system.System, version string) (bool, error) {
	sury, err := repoEnabled(sys, RepoSury)
	if err != nil || !sury {
		return false, err
	}
	installed, err := phpInstalled(sys)
	if err != nil {
		return false, err
	}
	others := slices.DeleteFunc(installed, func(v string) bool { return v == version })
	src, err := phpSources(ctx, sys, others)
	if err != nil {
		return false, err
	}
	for _, v := range others {
		if f := src[v]; f.found && !f.ubuntu {
			return false, nil
		}
	}
	return true, nil
}

// ---------------------------------------------------------------- đọc máy

const phpConfRoot = "/etc/php"

// checkPHPVersion: bản PHP người dùng gõ phải dạng số.số (8.3). Nó đi vào tên
// gói, tên dịch vụ, đường dẫn và URL nên kiểm trước mọi thứ khác.
func checkPHPVersion(v string) error {
	if phpVersionRE.MatchString(v) && len(v) <= 7 {
		return nil
	}
	return refuse("php_version_invalid", i18n.T(
		fmt.Sprintf("bản PHP %q không hợp lệ; viết dạng số.số, ví dụ 8.3", v),
		fmt.Sprintf("PHP version %q is not valid; write it like 8.3", v)))
}

// phpVersionCmp so hai bản theo số từng phần (8.10 sau 8.9).
func phpVersionCmp(a, b string) int {
	am, an, _ := strings.Cut(a, ".")
	bm, bn, _ := strings.Cut(b, ".")
	x, _ := strconv.Atoi(am)
	y, _ := strconv.Atoi(bm)
	if c := cmp.Compare(x, y); c != 0 {
		return c
	}
	x, _ = strconv.Atoi(an)
	y, _ = strconv.Atoi(bn)
	return cmp.Compare(x, y)
}

// phpUnion gộp hai danh sách bản, bỏ trùng, sắp tăng dần theo số.
func phpUnion(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.SortFunc(out, phpVersionCmp)
	return slices.Compact(out)
}

// phpIsInstalled: bản v đã cài khi có cả thư mục /etc/php/<v>/fpm lẫn file chạy
// /usr/sbin/php-fpm<v>. Chỉ thư mục thì chưa đủ: `apt-get remove` (php.remove)
// giữ file cấu hình nên thư mục còn sau khi gỡ gói.
func phpIsInstalled(sys system.System, v string) (bool, error) {
	for _, p := range []string{phpConfRoot + "/" + v + "/fpm", "/usr/sbin/php-fpm" + v} {
		ok, err := pathExists(sys, p)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// phpInstalled trả mọi bản đã cài (phpIsInstalled), sắp tăng dần.
func phpInstalled(sys system.System) ([]string, error) {
	names, err := sys.ReadDir(phpConfRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", phpConfRoot, err)
	}
	var out []string
	for _, n := range names {
		if !phpVersionRE.MatchString(n) {
			continue
		}
		ok, err := phpIsInstalled(sys, n)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, n)
		}
	}
	return phpUnion(out, nil), nil
}

// phpAvailable hỏi các kho đang cấu hình có những bản nào: mỗi gói
// php<bản>-fpm là một bản. Không ghi cứng bản nào (mục 6.11).
func phpAvailable(ctx context.Context, sys system.System) ([]string, error) {
	cmd := system.Cmd{Name: "apt-cache", Args: []string{"search", "--names-only", `^php[0-9]+\.[0-9]+-fpm$`}}
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("hỏi kho các bản PHP (%s): %w", cmd.String(), err)
	}
	var out []string
	for line := range strings.Lines(res.Stdout) {
		name, _, _ := strings.Cut(line, " ")
		if v, ok := phpFromPkg(name); ok {
			out = append(out, v)
		}
	}
	return phpUnion(out, nil), nil
}

// phpFromPkg đọc bản từ tên gói "php8.4-fpm".
func phpFromPkg(name string) (string, bool) {
	v, ok := strings.CutPrefix(strings.TrimSpace(name), "php")
	if v, ok2 := strings.CutSuffix(v, "-fpm"); ok && ok2 && phpVersionRE.MatchString(v) {
		return v, true
	}
	return "", false
}

// phpFrom là kết quả madison cho một bản: found có ít nhất một dòng kho,
// ubuntu có dòng từ một kho không phải packages.sury.org.
type phpFrom struct{ found, ubuntu bool }

func (f phpFrom) source() Source {
	if f.ubuntu {
		return SourceUbuntu
	}
	return SourceSury
}

// phpSources hỏi `apt-cache madison php<bản>-fpm...` (một lệnh cho mọi bản).
// Mỗi dòng "php8.3-fpm | 8.3.6-0ubuntu0.24.04.13 | http://... noble-updates/...".
//
// Dòng từ bất kỳ kho nào khác packages.sury.org đều tính là Ubuntu, không chỉ
// *.ubuntu.com: VPS thường dùng mirror của nhà cung cấp (mirror.hetzner.com,
// mirrors.digitalocean.com...), coi đó là sury là thêm kho và ghim sai.
func phpSources(ctx context.Context, sys system.System, versions []string) (map[string]phpFrom, error) {
	out := map[string]phpFrom{}
	if len(versions) == 0 {
		return out, nil
	}
	args := []string{"madison"}
	for _, v := range versions {
		args = append(args, "php"+v+"-fpm")
	}
	cmd := system.Cmd{Name: "apt-cache", Args: args}
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("hỏi nguồn các bản PHP (%s): %w", cmd.String(), err)
	}
	for line := range strings.Lines(res.Stdout) {
		f := strings.Split(line, "|")
		if len(f) < 3 {
			continue
		}
		v, ok := phpFromPkg(f[0])
		if !ok {
			continue
		}
		from := out[v]
		from.found = true
		if !strings.Contains(f[2], "packages.sury.org") {
			from.ubuntu = true
		}
		out[v] = from
	}
	return out, nil
}

// phpPackages là bộ gói của một bản PHP, đúng bộ extension ở mục 3. Không có
// gói php<bản> trần (kéo theo mod_php).
func phpPackages(v string) []string {
	out := []string{"php" + v + "-fpm"}
	for _, ext := range []string{"cli", "mysql", "curl", "gd", "mbstring", "xml", "zip", "intl"} {
		out = append(out, "php"+v+"-"+ext)
	}
	return out
}

// phpInstalledPackages trả các gói php<v>-* đang cài (không tính gói đã gỡ chỉ
// còn file cấu hình). Không gói nào khớp thì dpkg-query thoát 1: không phải lỗi.
func phpInstalledPackages(ctx context.Context, sys system.System, v string) ([]string, error) {
	cmd := system.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f", `${Package}\t${db:Status-Status}\n`, "php" + v + "-*"}}
	res, err := sys.Run(ctx, cmd)
	var ee *system.ExitError
	if errors.As(err, &ee) && ee.Code == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("liệt kê gói của PHP %s: %w", v, err)
	}
	var out []string
	for line := range strings.Lines(res.Stdout) {
		name, status, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if status == "installed" && strings.HasPrefix(name, "php"+v+"-") {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// sitesByVersion: domain các site (mọi trạng thái: site trong thùng rác khôi
// phục lại vẫn cần bản PHP của nó) theo bản PHP, sắp theo chữ.
func (p *PHP) sitesByVersion(ctx context.Context, cfg StackConfig) (map[string][]string, error) {
	all, err := p.Sites.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	out := map[string][]string{}
	for _, st := range all {
		if v := sitePHP(st, cfg); v != "" {
			out[v] = append(out[v], st.Domain.String())
		}
	}
	for _, doms := range out {
		slices.Sort(doms)
	}
	return out, nil
}

// ---------------------------------------------------------------- ghi chú

const (
	phpStatesURL = "https://www.php.net/releases/states.php"
	wpServeHappy = "https://api.wordpress.org/core/serve-happy/1.0/?php_version="
)

// fetchJSON gọi url bằng curl (timeout 5 giây) và đọc JSON vào v.
func fetchJSON(ctx context.Context, sys system.System, url string, v any) error {
	res, err := sys.Run(ctx, system.Cmd{Name: "curl", Args: []string{"-fsS", "--proto", "=https", "--max-time", "5", url}})
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(res.Stdout), v)
}

// notes điền hạn hỗ trợ (php.net) và bản WordPress khuyên dùng. Không gọi được
// thì bỏ ghi chú: danh sách vẫn đủ khi máy không ra được mạng (mục 6.11).
func (p *PHP) notes(ctx context.Context, list *PHPList, def string) {
	// {"8": {"8.3": {"state": "security", "active_support_end": "2025-12-31T...", ...}}}
	var states map[string]map[string]struct {
		State    string `json:"state"`
		Active   string `json:"active_support_end"`
		Security string `json:"security_support_end"`
	}
	statesOK := fetchJSON(ctx, p.Sys, phpStatesURL, &states) == nil && len(states) > 0
	if statesOK {
		day := func(s string) string { return s[:min(len(s), 10)] }
		known := map[string]PHPSupport{}
		oldest := ""
		for _, branches := range states {
			for v, b := range branches {
				if !phpVersionRE.MatchString(v) {
					continue
				}
				known[v] = PHPSupport{State: b.State, ActiveUntil: day(b.Active), SecurityUntil: day(b.Security)}
				if oldest == "" || phpVersionCmp(v, oldest) < 0 {
					oldest = v
				}
			}
		}
		for i := range list.Versions {
			row := &list.Versions[i]
			if s, ok := known[row.Version]; ok {
				row.Support = &s
			} else if oldest != "" && phpVersionCmp(row.Version, oldest) < 0 {
				// php.net chỉ liệt kê nhánh còn hỗ trợ: bản cũ hơn nhánh cũ nhất
				// trong đó là đã hết hạn.
				row.Support = &PHPSupport{State: "eol"}
			}
		}
	}

	wpOK := false
	if phpVersionRE.MatchString(def) {
		var wp struct {
			Recommended string `json:"recommended_version"`
		}
		if fetchJSON(ctx, p.Sys, wpServeHappy+def, &wp) == nil && phpVersionRE.MatchString(wp.Recommended) {
			list.WPRecommended, wpOK = wp.Recommended, true
		}
	}
	list.NotesOK = statesOK && wpOK
}

// ---------------------------------------------------------------- kiểm giữa chừng

// midCheckTries, midCheckWait: phép kiểm giữa chừng thử lại vài lần. `systemctl
// reload php<bản>-fpm` gửi tín hiệu rồi trả về ngay (ExecReload của gói là
// kill -USR2), nên socket của pool mới có thể chưa có ngay lúc web server bắt
// đầu chuyển request sang. Test đặt wait = 0.
var (
	midCheckTries = 5
	midCheckWait  = time.Second
)

// socketTries: trần riêng cho bước chờ socket của pool mới. php-fpm của kho
// sury chờ một truy vấn DNS lúc khởi động (timeout của resolver là 5 giây), nên
// trên máy chặn DNS ra ngoài lần nạp lại có thể chậm hơn nhiều so với 2 giây
// đo được trên máy ảo.
const socketTries = 20

// midCheck là một phép kiểm chạy như một bước của Plan: không đạt thì bước lỗi
// và engine hoàn tác các bước trước. Không đổi gì trên máy.
type midCheck struct {
	id         string
	check      checks.Check
	title, why i18n.Text
	// tries: số lần thử tối đa, 0 là midCheckTries. need: số lần đạt liên
	// tiếp mới tính là đạt, 0 là 1.
	tries, need int

	passed bool
}

var _ engine.Step = (*midCheck)(nil)

func (s *midCheck) Info() engine.StepInfo {
	return engine.StepInfo{
		ID: s.id, Title: s.title, Why: s.why,
		NoUndo: i18n.T("Bước này chỉ gọi thử, không đổi gì trên máy.", "This step only makes a test request and changes nothing on the machine."),
	}
}

// Check sai cho tới khi chính bước này chạy đạt: lúc xem trước chưa có gì để
// kiểm (pool mới chưa có).
func (s *midCheck) Check(context.Context, system.System) (bool, error) { return s.passed, nil }

// Apply chạy phép kiểm, thử lại tới midCheckTries lần. Chỉ Pass hoặc Warn là
// đạt: Unknown (không gọi được) cũng không chứng minh site chạy, nên không
// được đi tiếp tới bước gỡ pool cũ.
func (s *midCheck) Apply(ctx context.Context, sys system.System) error {
	var r checks.Result
	tries, need, streak := cmp.Or(s.tries, midCheckTries), max(s.need, 1), 0
	for i := range tries {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%s: %w", s.id, ctx.Err())
			case <-time.After(midCheckWait):
			}
		}
		if r = s.check.Run(ctx, sys); r.Status != checks.Pass && r.Status != checks.Warn {
			streak = 0
			continue
		}
		if streak++; streak >= need {
			s.passed = true
			return nil
		}
	}
	if streak > 0 {
		return fmt.Errorf("%s: lần thử cuối đạt nhưng chưa đủ %d lần đạt liên tiếp sau %d lần thử", r.ID, need, tries)
	}
	err := fmt.Errorf("%s (%s) sau %d lần thử: %s", r.ID, r.Status, tries, r.Summary.Vi)
	if len(r.Evidence) > 0 {
		err = fmt.Errorf("%w; bằng chứng: %s", err, r.Evidence[len(r.Evidence)-1].Output)
	}
	return err
}

func (s *midCheck) Undo(context.Context, system.System) error { return nil }
