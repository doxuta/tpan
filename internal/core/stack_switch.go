package core

// Chuyển LEMP ⇄ LAMP tại chỗ (mục 6.6 của docs/KE-HOACH.md). Hợp đồng M10 đợt 0;
// thân của Prepare do thợ core đợt 2 viết.
//
// Prepare(ctx, to) chỉ đọc máy và DB. Thứ tự:
//
// Điều kiện đầu (từ chối bằng *RefusedError, không Plan nào được dựng):
//   - to qua ParseWeb (web_invalid).
//   - Stack đã cài (stack.php_version có giá trị), không thì stack_not_installed.
//   - to khác stack.web, không thì stack_same_web.
//   - Mọi site active đang trả lời đúng trên web server CŨ: checks.HTTPSite,
//     hoặc checks.HTTPSSite khi site.SSL. Site nào Fail thì từ chối
//     precondition_failed, Checks là các kết quả Fail (một site hỏng sẵn thì
//     bước kiểm giữa chừng chắc chắn lỗi và cả job hoàn tác; nói trước rẻ hơn).
//
// Nguồn của web server mới: Apache luôn là SourceUbuntu. Nginx: đọc từ máy,
// không từ settings (đổi sang apache đã ghi ubuntu, mà gói nginx của nginx.org
// và kho của nó, ghim 900, vẫn nằm trên máy): kho nginx.org (repoNginx của
// repos.go, stable hay mainline) đã có trên máy (Check của AptRepo đạt) thì
// nguồn là kho đó, không thì SourceUbuntu.
//
// Plan "stack.switch", Target là to, Title "Chuyển LEMP (Nginx) sang LAMP
// (Apache)" (stackName). Các bước, đúng thứ tự:
//
//  1. steps.AptUpdate và steps.AptInstallNoStart{Packages: [webUnit(to)],
//     Disable: [webUnit(to)]} (mục 6.6 bước 1, mục 16).
//  2. Cấu hình của web server mới, KHÔNG reload, KHÔNG restart, KHÔNG start
//     (web server mới đang tắt; web server cũ vẫn giữ cổng). Dựng từ danh sách
//     cho phép tường minh, không lọc từ installPlan (installPlan có Service
//     enable --now, ApacheModules restart, groupRestart). Tách phần web của
//     installPlan thành một hàm dùng chung nếu cần, để hai nơi không lệch nhau.
//     Mọi *steps.File, *Symlink, *Absent qua steps.WithoutReload (giữ Test:
//     `nginx -t`, `apachectl configtest` chạy được khi dịch vụ tắt); mọi
//     *steps.ApacheModules đặt NoRestart. Thứ tự trong bước này:
//     a. nginx.org: Dir sites-available, sites-enabled; File conf.d/tpan.conf;
//        Absent conf.d/default.conf; steps.UserInGroup{nginx, www-data} (KHÔNG
//        groupRestart: nhóm có hiệu lực khi nginx khởi động ở WebSwap).
//        Apache: ApacheModules{Enable: apacheModules, Disable: mpm_prefork}.
//     b. Gỡ site mặc định của distro, ghi site mặc định cổng 80 của tpan
//        (file + symlink), như installPlan.
//     c. Có site nào trong bảng sites (MỌI trạng thái, kể cả thùng rác: khôi
//        phục một site SSL về sau cần mod ssl và site mặc định 443) có SSL thì
//        steps.DefaultSSL(to) (M5), trước vhost của các site.
//     d. Mỗi site active: siteVhost(st, cfgMới) (fix.go: bản 443 khi site.SSL,
//        socket theo bản PHP của từng site qua sitePHP). Apache: thêm
//        steps.SiteHtaccess khi <public>/.htaccess CHƯA có (đọc lúc Prepare);
//        có rồi thì để nguyên (WordPress tự ghi lại đoạn của nó). Site dựng
//        trên nginx không có .htaccess: thiếu nó thì trang chủ vẫn 200 nhưng
//        mọi permalink /%postname%/ là 404 trên Apache.
//     e. File /etc/logrotate.d/tpan-sites theo web server mới
//        (templates.LogrotateSites), Test `logrotate -d`.
//  3. steps.WebSwap{From: webUnit(cũ), To: webUnit(to)} (mục 6.6 bước 3).
//  4. midCheck (php.go) cho mỗi site active: HTTPSite hoặc HTTPSSite, thử lại
//     như php.use (mục 6.6 bước 4). Không đạt thì job dừng, engine hoàn tác
//     ngược: web server cũ chạy lại với đúng file cũ (mục 6.6 bước 5).
//  5. steps.Service{Unit: webUnit(to)} (enable --now; Undo của nó disable lại),
//     rồi steps.ServiceDisable{Unit: webUnit(cũ)} (mục 6.6 bước 5).
//  6. Gỡ file tpan của web server CŨ (Absent, không Test, không Reload: dịch vụ
//     cũ đã dừng): link và file vhost của mọi site trong bảng sites (mọi trạng
//     thái), site mặc định 80 và 443 (file và link), và với nginx.org
//     conf.d/tpan.conf. Absent khai FileChange Delete nên engine bỏ chúng khỏi
//     managed_files: health không còn kiểm lệch file của web server không chạy,
//     và lần chuyển ngược lại ghi lại chúng từ template (được ghi nhận lại).
//
// Verify: Service(webUnit(to)), NginxConfig/ApacheConfig, HTTPDefault(to), và
// mỗi site active VhostEnabled(to, l) cùng HTTPSite/HTTPSSite. Không chạy cả bộ
// siteChecks: một site hỏng ở chặng không liên quan tới web server (DB, mã
// nguồn) không được kéo hoàn tác một lần chuyển đúng; health đầy đủ chạy ngay
// sau job (CLI và API) và mở sự cố cho nó.
//
// Sau khi job thành công (after của Planned):
//   - Ghi stack.web = to và stack.web_source = nguồn mới (ubuntu với apache).
//     Không ghi được thì báo lỗi nói rõ máy đang chạy web server nào và lệnh
//     chạy lại (`sudo tpan stack switch --to <to>`: các bước đã đúng được bỏ
//     qua, chỉ còn ghi settings). Vì vậy Prepare không được từ chối khi máy đã
//     chạy web server mới mà settings còn ghi cũ.
//   - Ghi sha256 của MỌI *steps.File của web server mới ở bước 2 vào
//     managed_files (Journal.RecordFile), kể cả bước được bỏ qua vì file đã
//     đúng: engine chỉ ghi file của bước đã Apply, nên một file đúng sẵn mà
//     không có dòng sẽ không bao giờ được kiểm lệch. Đây là "phép kiểm lệch
//     nhận các file mới làm chuẩn".
//
// Mã từ chối mới: stack_not_installed, stack_same_web (cộng web_invalid,
// precondition_failed đã có). Câu từ chối stack_switch_required của
// stack_sources.go bỏ đuôi "(chưa có ở bản này)".

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// Switch gom những gì Plan "stack.switch" cần. Dựng mới cho mỗi lệnh.
type Switch struct {
	Sys      system.System
	Settings Settings
	Sites    SiteStore
	// Journal nhận job khi Execute và các dòng managed_files sau khi xong;
	// nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
}

// Prepare dựng Plan "stack.switch" chuyển web server sang to (chú thích đầu
// file). Chỉ đọc máy. Bị từ chối thì trả *RefusedError.
func (s *Switch) Prepare(ctx context.Context, to WebServer) (*Planned, error) {
	if _, err := ParseWeb(string(to)); err != nil {
		return nil, err
	}
	old, err := (&Sites{Settings: s.Settings}).stack(ctx)
	if err != nil {
		return nil, err
	}
	if old.Web == to {
		return nil, refuse("stack_same_web", i18n.T(
			"stack đang chạy "+stackName(to)+"; không có gì để chuyển",
			"the stack already runs "+stackName(to)+"; nothing to switch"))
	}
	all, err := s.Sites.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	var active []site.Site
	anySSL := false
	for _, st := range all {
		if st.Status == site.Active {
			active = append(active, st)
		}
		anySSL = anySSL || st.SSL
	}
	if err := s.sitesAnswer(ctx, active); err != nil {
		return nil, err
	}

	src, err := s.source(ctx, to)
	if err != nil {
		return nil, err
	}
	cfg := old
	cfg.Web, cfg.WebSource = to, src
	from, unit := webUnit(old.Web), webUnit(to)

	// Bước 1: cài gói mà không cho chạy, rồi tắt bật-khi-khởi-động.
	//
	// Ngoại lệ: web server mới đã chạy và bật sẵn (lần trước job xong mà không
	// ghi được settings) thì không tắt nó: `disable --now` lúc này là dừng chính
	// web server đang phục vụ mọi site, rồi WebSwap phải bật lại. Bỏ Disable thì
	// mọi bước tới WebSwap đều đã đúng và được bỏ qua, chỉ còn ghi settings.
	disable := []string{unit}
	running, err := (&steps.Service{Unit: unit}).Check(ctx, s.Sys)
	if err != nil {
		return nil, err
	}
	if running {
		disable = nil
	}
	ss := []engine.Step{
		&steps.AptUpdate{Packages: []string{unit}},
		&steps.AptInstallNoStart{Packages: []string{unit}, Disable: disable},
	}

	// Bước 2: cấu hình của web server mới, không start, restart hay reload nó.
	conf, err := s.newConfig(cfg, active, anySSL)
	if err != nil {
		return nil, err
	}
	newFiles := files(conf)
	ss = append(ss, conf...)

	// Bước 3, 4: đổi web server, rồi kiểm từng site trên web server mới.
	ss = append(ss, &steps.WebSwap{From: from, To: unit})
	for _, st := range active {
		check := siteHTTP(st)
		if check == nil {
			continue
		}
		dom := st.Domain.String()
		wantVi, wantEn := "200", "200"
		if st.Suspended != account.NotSuspended {
			wantVi, wantEn = "403 (trang tạm khoá)", "403 (suspension page)"
		}
		ss = append(ss, &midCheck{
			id:    "stack.switch.check:" + dom,
			check: check,
			// Hai lần đạt liên tiếp như php.use: lần gọi đầu ngay sau start chưa
			// chắc đã là cấu hình cuối cùng (worker mới vừa nhận việc).
			need:  2,
			title: i18n.T("Kiểm "+dom+" trả "+wantVi+" qua "+unit, "Check that "+dom+" answers "+wantEn+" through "+unit),
			why: i18n.T(
				from+" chỉ bị tắt hẳn khi mọi site đã chạy được trên "+unit+". Site không trả lời đúng thì job dừng ở đây và hoàn tác: "+unit+" dừng, "+from+" chạy lại với đúng các file cũ.",
				from+" is only turned off for good once every site works on "+unit+". If the site does not answer correctly the job stops here and rolls back: "+unit+" stops and "+from+" starts again with exactly its old files."),
		})
	}

	// Bước 5: bật web server mới khi khởi động, tắt web server cũ khi khởi động.
	ss = append(ss, &steps.Service{Unit: unit}, &steps.ServiceDisable{Unit: from})

	// Bước 6: gỡ file tpan của web server cũ.
	gone, err := oldWebFiles(old, all)
	if err != nil {
		return nil, err
	}
	for _, p := range gone {
		ss = append(ss, &steps.Absent{
			Path:  p,
			Title: i18n.T("Gỡ "+p+" của "+from, "Remove "+from+"'s "+p),
			Why: i18n.T(
				from+" đã dừng và không bật khi khởi động. File cấu hình của nó để lại thì health vẫn kiểm lệch file của một web server không chạy; lần chuyển ngược lại tpan ghi lại file này từ template. Bước này hoàn tác được.",
				from+" is stopped and disabled at boot. Leaving its configuration would keep health checking files of a web server that does not run; switching back writes this file again from the template. This step can be undone."),
		})
	}

	config := checks.NginxConfig()
	if to == Apache {
		config = checks.ApacheConfig()
	}
	verify := []checks.Check{checks.Service(unit, checks.StageWeb), config, checks.HTTPDefault(string(to))}
	for _, st := range active {
		verify = append(verify, checks.VhostEnabled(string(to), st.Layout()))
		if c := siteHTTP(st); c != nil {
			verify = append(verify, c)
		}
	}

	plan := engine.Plan{
		Kind:   "stack.switch",
		Target: string(to),
		Title:  i18n.T("Chuyển "+stackName(old.Web)+" sang "+stackName(to), "Switch from "+stackName(old.Web)+" to "+stackName(to)),
		Steps:  ss,
		Verify: verify,
	}
	after := func(ctx context.Context, rep engine.Report) error {
		for _, kv := range [][2]string{{SettingWeb, string(to)}, {SettingWebSource, string(src)}} {
			if err := s.Settings.SetSetting(ctx, kv[0], kv[1]); err != nil {
				return fmt.Errorf("máy đã chạy %s nhưng chưa ghi được setting %s (%w); chạy lại `sudo tpan stack switch --to %s`: các bước đã đúng được bỏ qua, chỉ còn ghi settings", unit, kv[0], err, to)
			}
		}
		// Engine chỉ ghi file của bước đã Apply; file đã đúng sẵn (bước bỏ qua)
		// cũng phải có dòng, không thì health không bao giờ kiểm lệch nó.
		var errs []error
		for _, f := range newFiles {
			sum := sha256.Sum256(f.Content)
			if err := s.Journal.RecordFile(ctx, f.Path, hex.EncodeToString(sum[:]), rep.Job.ID); err != nil {
				errs = append(errs, fmt.Errorf("ghi sha256 của %s: %w", f.Path, err))
			}
		}
		// Bước gỡ đã bỏ qua (file vốn không còn) thì engine không bỏ dòng của nó.
		for _, p := range gone {
			if err := s.Journal.ForgetFile(ctx, p); err != nil {
				errs = append(errs, fmt.Errorf("bỏ %s khỏi file tpan quản lý: %w", p, err))
			}
		}
		return errors.Join(errs...)
	}
	return newPlanned(s.Sys, s.Journal, plan, after), nil
}

// siteHTTP là phép kiểm đầu cuối của site: https khi site có SSL. M13: site
// tạm khoá thì 403 đúng trang tạm khoá (siteVhost ghi bản tạm khoá cho nó);
// DB của site đang khoá thì nil (không kiểm HTTP): WordPress báo lỗi kết nối
// DB với mọi web server, và một site đã biết là không trả 200 không được chặn
// hay kéo hoàn tác lần chuyển web server của mọi site khác. VhostEnabled của
// site đó vẫn nằm trong kiểm chứng.
func siteHTTP(st site.Site) checks.Check {
	switch {
	case st.Suspended != account.NotSuspended:
		return checks.HTTPSuspended(st.Layout(), st.SSL)
	case st.DBSuspended != account.NotSuspended:
		return nil
	}
	if st.SSL {
		return checks.HTTPSSite(st.Layout())
	}
	return checks.HTTPSite(st.Layout())
}

// sitesAnswer là điều kiện đầu: mọi site active đang trả lời đúng trên web
// server cũ. Một site hỏng sẵn thì bước kiểm giữa chừng chắc chắn lỗi và cả
// job hoàn tác; nói trước rẻ hơn.
func (s *Switch) sitesAnswer(ctx context.Context, active []site.Site) error {
	var cs []checks.Check
	for _, st := range active {
		if c := siteHTTP(st); c != nil {
			cs = append(cs, c)
		}
	}
	var failed []checks.Result
	for _, r := range checks.RunAll(ctx, s.Sys, cs) {
		if r.Status == checks.Fail {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	e := refuse("precondition_failed", i18n.T(
		"có site đang không trả lời đúng trên web server hiện tại; sửa nó trước (xem tpan health), không thì bước kiểm sau khi đổi chắc chắn lỗi và cả job hoàn tác",
		"some sites do not answer correctly on the current web server; fix them first (see tpan health), otherwise the check after the swap is bound to fail and the whole job rolls back"))
	e.Checks = failed
	return e
}

// source là nguồn gói của web server mới, đọc từ máy chứ không từ settings
// (đổi sang Apache ghi web_source ubuntu, mà kho nginx.org và gói của nó vẫn
// nằm trên máy, ghim 900). Apache luôn của Ubuntu. Tên mã Ubuntu chỉ đọc khi
// file sources của kho nginx.org có mặt.
func (s *Switch) source(ctx context.Context, to WebServer) (Source, error) {
	if to == Apache {
		return SourceUbuntu, nil
	}
	on, err := repoEnabled(s.Sys, RepoNginx)
	if err != nil || !on {
		return SourceUbuntu, err
	}
	codename, err := codenameOrRefuse(s.Sys)
	if err != nil {
		return "", err
	}
	for _, src := range []Source{SourceNginxStable, SourceNginxMainline} {
		r, err := repoNginx(codename, src == SourceNginxMainline)
		if err != nil {
			return "", fmt.Errorf("dựng kho nginx.org: %w", err)
		}
		ok, err := r.Check(ctx, s.Sys)
		if err != nil {
			return "", fmt.Errorf("kiểm kho nginx.org trên máy: %w", err)
		}
		if ok {
			return src, nil
		}
	}
	return SourceUbuntu, nil
}

// newConfig là bước 2: mọi file của web server mới (cfg.Web), không bước nào
// start, restart hay reload nó. Test (`nginx -t`, `apachectl configtest`) giữ
// nguyên: chạy được khi dịch vụ tắt.
func (s *Switch) newConfig(cfg StackConfig, active []site.Site, anySSL bool) ([]engine.Step, error) {
	ss, err := webSteps(cfg, false)
	if err != nil {
		return nil, fmt.Errorf("dựng bước web server mới: %w", err)
	}
	// Site SSL ở mọi trạng thái, kể cả thùng rác: khôi phục nó về sau cần
	// mod ssl và site mặc định 443 của web server mới.
	if anySSL {
		ssl, err := steps.DefaultSSL(string(cfg.Web))
		if err != nil {
			return nil, err
		}
		// Bước bật mod ssl của DefaultSSL có cùng ID "apache.modules:apache2"
		// với bước bật mod của stack (ID phải duy nhất trong Plan: tiến độ CLI
		// và job_events gọi bước theo ID). Gộp ssl vào bước của stack, nằm
		// trước, nên vẫn bật trước khi ghi site mặc định 443.
		for _, st := range ssl {
			if m, ok := st.(*steps.ApacheModules); ok {
				i := slices.IndexFunc(ss, func(x engine.Step) bool { _, ok := x.(*steps.ApacheModules); return ok })
				if i < 0 {
					return nil, errors.New("dựng bước Apache: thiếu bước bật mod của stack để gộp mod ssl")
				}
				base := ss[i].(*steps.ApacheModules)
				base.Enable = append(slices.Clone(base.Enable), m.Enable...)
				continue
			}
			ss = append(ss, st)
		}
	}
	for _, st := range active {
		v, err := siteVhost(st, cfg)
		if err != nil {
			return nil, fmt.Errorf("dựng vhost của %s: %w", st.Domain, err)
		}
		ss = append(ss, v...)
		if cfg.Web != Apache {
			continue
		}
		l := st.Layout()
		ht, err := htaccessIfMissing(s.Sys, l, l.Public)
		if err != nil {
			return nil, err
		}
		if ht != nil {
			ss = append(ss, ht)
		}
	}
	lr, err := logrotateStep(cfg.Web)
	if err != nil {
		return nil, fmt.Errorf("dựng file logrotate: %w", err)
	}
	ss = append(ss, lr)
	// DefaultSSL của M5 bật mod ssl bằng ApacheModules có restart.
	for _, st := range ss {
		if m, ok := st.(*steps.ApacheModules); ok {
			m.NoRestart = true
		}
	}
	return steps.WithoutReload(ss), nil
}

// htaccessIfMissing là bước ghi .htaccess của WordPress cho site l khi
// <public>/.htaccess chưa có (đọc lúc dựng Plan), nil khi đã có. public là nơi
// thư mục public của site đang nằm lúc dựng Plan: l.Public, hoặc trong thùng
// rác khi khôi phục; bước luôn ghi vào l.Public. Site dựng trên nginx không có
// .htaccess: thiếu nó thì trang chủ vẫn 200 mà mọi permalink /%postname%/ là
// 404 trên Apache. Có rồi thì để nguyên: WordPress tự ghi lại đoạn của nó.
// Dùng khi chuyển sang Apache (site active) và khi khôi phục trên Apache một
// site đã vào thùng rác lúc còn chạy nginx (trash.go).
func htaccessIfMissing(sys system.System, l site.Layout, public string) (engine.Step, error) {
	_, err := sys.Stat(public + "/.htaccess")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return steps.SiteHtaccess(l)
	case err != nil:
		return nil, fmt.Errorf("kiểm .htaccess của %s: %w", l.Domain, err)
	}
	return nil, nil
}

// oldWebFiles là các file tpan đã ghi cho web server cũ, link trước file:
// vhost của mọi site trong bảng sites (mọi trạng thái), site mặc định cổng 80
// và 443, và với nginx.org conf.d/tpan.conf. Dựng lại từ đúng các hàm đã ghi
// chúng nên không chép đường dẫn nào.
func oldWebFiles(old StackConfig, all []site.Site) ([]string, error) {
	ss, err := webSteps(old, false)
	if err != nil {
		return nil, fmt.Errorf("dựng lại file của web server cũ: %w", err)
	}
	ssl, err := steps.DefaultSSL(string(old.Web))
	if err != nil {
		return nil, err
	}
	var vh []engine.Step
	for _, st := range all {
		v, err := siteVhost(st, old)
		if err != nil {
			return nil, fmt.Errorf("dựng lại vhost cũ của %s: %w", st.Domain, err)
		}
		vh = append(vh, v...)
	}
	var links, paths []string
	for _, st := range append(append(vh, ss...), ssl...) {
		switch x := st.(type) {
		case *steps.Symlink:
			links = append(links, x.Link)
		case *steps.File:
			paths = append(paths, x.Path)
		}
	}
	return append(links, paths...), nil
}
