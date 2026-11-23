package core

// Nguồn gói lúc cài stack và pool www thừa của PHP-FPM (mục 6.11, mục 16 dòng
// "Còn mở" ở M2). Hợp đồng M12 đợt 0; thân do thợ đợt 2 viết.
//
// PrepareInstallWith là PrepareInstall (stack.go) cộng lựa chọn nguồn:
//   - Packages(cfg) nhận thêm SourceNginxStable, SourceNginxMainline (chỉ với
//     web nginx) và SourceMariaDB; nguồn khác vẫn source_unsupported.
//   - Trước bước cài gói: steps.AptRepo của nginx.org (ghim 900 cho origin
//     nginx.org để gói của kho này thắng gói Ubuntu) và/hoặc của MariaDB (dòng
//     DBLine), mỗi cái Danger, xem trước hiện kho và vân tay khoá.
//   - nginx.org: sau cài gói, thích nghi bố cục: Dir sites-available,
//     sites-enabled; File /etc/nginx/conf.d/tpan.conf (templates.NginxOrgInclude);
//     Absent /etc/nginx/conf.d/default.conf; steps.UserInGroup{nginx, www-data}
//     rồi restart nginx (thành viên nhóm chỉ có hiệu lực với tiến trình mới).
//   - Mọi nguồn: sau DetectPHP thêm steps.PHPIdlePool{Version: php} (gỡ pool
//     www, thêm tpan-idle), và checks.PHPPoolWWWAbsent trong kiểm chứng.
//   - Chạy lại: InstallOptions phải trùng stack.*_source và stack.db_line đã ghi
//     (zero value = giữ như đã ghi); khác thì từ chối source_change_unsupported.
//   - stack.php_version đã có giá trị thì giữ nguyên (không lấy bản cao nhất
//     DetectPHP thấy: máy đã cài thêm 8.4 thì chạy lại stack install không được
//     đổi bản mặc định).
// PrepareInstall(ctx, web) giữ nguyên chữ ký, gọi PrepareInstallWith với zero
// InstallOptions.

import (
	"context"
	"errors"
	"fmt"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// PrepareInstallWith dựng Plan "stack.install" với nguồn gói đã chọn. Chỉ đọc máy.
func (s *Stack) PrepareInstallWith(ctx context.Context, web WebServer, opts InstallOptions) (*Install, error) {
	if _, err := ParseWeb(string(web)); err != nil {
		return nil, err
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	firstInstall := cfg.Web == ""
	if !firstInstall && cfg.Web != web {
		return nil, refuse("stack_switch_required", i18n.T(
			fmt.Sprintf("máy này đã được tpan cài với %s; muốn đổi sang %s thì dùng `tpan stack switch --to %s`", cfg.Web, web, web),
			fmt.Sprintf("tpan already installed this machine with %s; to change to %s use `tpan stack switch --to %s`", cfg.Web, web, web)))
	}
	cfg.Web = web
	if cfg, err = withSources(cfg, opts, firstInstall); err != nil {
		return nil, err
	}
	pkgs, err := Packages(cfg)
	if err != nil {
		return nil, err
	}

	// Điều kiện đầu. Lần đầu thì máy phải sạch. Chạy lại (stack.web đã ghi và
	// trùng) thì máy có gói và cổng bận là do chính tpan, kể cả sau một lần hỏng
	// giữa chừng, nên bỏ hai kiểm đó.
	pre := []checks.Check{checks.OSRelease("ubuntu", "24.04")}
	if firstInstall {
		pre = append(pre,
			checks.PortsFree(80, 443),
			checks.PackagesAbsent("nginx", "apache2", "mariadb-server", "mysql-server"))
	}
	results := checks.RunAll(ctx, s.Sys, pre)
	var failed []checks.Result
	for _, r := range results {
		if r.Status == checks.Fail {
			failed = append(failed, r)
		}
	}
	if len(failed) > 0 {
		e := refuse("precondition_failed", i18n.T(
			"máy chưa đạt điều kiện để cài stack",
			"the machine does not meet the conditions to install the stack"))
		e.Checks = failed
		return nil, e
	}

	repos, err := stackRepos(s.Sys, cfg)
	if err != nil {
		return nil, err
	}

	// Bản mặc định đã ghi thì Plan dùng đúng bản đó (installPlan cho DetectPHP
	// một biến riêng); chưa có thì DetectPHP ghi vào đây lúc chạy.
	php := new(string)
	*php = cfg.PHPVersion
	version, sha := s.wpcli()
	plan, err := installPlan(cfg, pkgs, php, version, sha)
	if err != nil {
		return nil, err
	}
	// Kho ngoài phải có trước apt-get update và apt-get install: gói nginx,
	// mariadb-server mới được lấy từ kho đã chọn.
	plan.Steps = append(repos, plan.Steps...)
	return &Install{Config: cfg, Preconditions: results, Plan: plan, stack: s, php: php}, nil
}

// withSources ghép nguồn người dùng chọn vào cấu hình đã ghi. Lần đầu thì lấy
// lựa chọn (trống là Ubuntu). Chạy lại thì lựa chọn trống là giữ như đã ghi,
// còn khác đã ghi là từ chối: đổi nguồn của một stack đang chạy (gỡ nginx của
// Ubuntu, cài nginx.org; nâng phiên bản MariaDB) là Plan khác, không phải chạy lại.
func withSources(cfg StackConfig, opts InstallOptions, first bool) (StackConfig, error) {
	if first {
		if opts.WebSource != "" {
			cfg.WebSource = opts.WebSource
		}
		if opts.DBSource != "" {
			cfg.DBSource = opts.DBSource
		}
		cfg.DBLine = opts.DBLine
		return cfg, nil
	}
	for _, c := range []struct{ what, got, want string }{
		{"nguồn web server", string(cfg.WebSource), string(opts.WebSource)},
		{"nguồn MariaDB", string(cfg.DBSource), string(opts.DBSource)},
		{"phiên bản MariaDB", cfg.DBLine, opts.DBLine},
	} {
		if c.want != "" && c.want != c.got {
			return StackConfig{}, refuse("source_change_unsupported", i18n.T(
				fmt.Sprintf("stack đã cài với %s %q; chạy lại không đổi được thành %q. Bỏ cờ đó đi để giữ như đã cài.", c.what, c.got, c.want),
				fmt.Sprintf("the stack was installed with %s %q; running again cannot change it to %q. Leave the flag out to keep what was installed.", sourceWhatEn[c.what], c.got, c.want)))
		}
	}
	return cfg, nil
}

var sourceWhatEn = map[string]string{
	"nguồn web server":  "web server source",
	"nguồn MariaDB":     "MariaDB source",
	"phiên bản MariaDB": "MariaDB series",
}

// checkSources là phần "nguồn" của Packages: chỉ nhận đúng các nguồn tpan biết
// cài, đúng với thành phần của nó.
func checkSources(cfg StackConfig) error {
	unsupported := func(what string, src Source, allowed string) error {
		return refuse("source_unsupported", i18n.T(
			fmt.Sprintf("nguồn gói %q cho %s không được hỗ trợ; chọn một trong: %s", src, what, allowed),
			fmt.Sprintf("package source %q for %s is not supported; choose one of: %s", src, what, allowed)))
	}
	switch cfg.WebSource {
	case "", SourceUbuntu:
	case SourceNginxStable, SourceNginxMainline:
		if cfg.Web != Nginx {
			return refuse("source_unsupported", i18n.T(
				fmt.Sprintf("nguồn %q chỉ dùng được với web nginx", cfg.WebSource),
				fmt.Sprintf("source %q only works with the nginx web server", cfg.WebSource)))
		}
	default:
		return unsupported("web", cfg.WebSource, "ubuntu, nginx.org, nginx.org-mainline")
	}
	switch cfg.DBSource {
	case "", SourceUbuntu:
		if cfg.DBLine != "" {
			return refuse("mariadb_line_invalid", i18n.T(
				"chỉ chọn được phiên bản MariaDB khi nguồn là mariadb.org (--db-source mariadb.org)",
				"a MariaDB series can only be chosen with the mariadb.org source (--db-source mariadb.org)"))
		}
	case SourceMariaDB:
		if !dbLineRE.MatchString(cfg.DBLine) {
			return refuse("mariadb_line_invalid", i18n.T(
				fmt.Sprintf("nguồn mariadb.org cần phiên bản MariaDB dạng 11.8 (--db-line), nhận được %q", cfg.DBLine),
				fmt.Sprintf("the mariadb.org source needs a MariaDB series like 11.8 (--db-line), got %q", cfg.DBLine)))
		}
	default:
		return unsupported("db", cfg.DBSource, "ubuntu, mariadb.org")
	}
	// PHP của stack luôn là của Ubuntu; bản khác cài riêng bằng `tpan php install`.
	if cfg.PHPSource != "" && cfg.PHPSource != SourceUbuntu {
		return unsupported("php", cfg.PHPSource, "ubuntu (bản PHP khác: tpan php install <bản>)")
	}
	return nil
}

// stackRepos dựng bước thêm kho cho các nguồn ngoài Ubuntu. Cần tên mã của bản
// Ubuntu (đọc /etc/os-release) nên chỉ đọc khi thật sự có kho ngoài.
func stackRepos(sys system.System, cfg StackConfig) ([]engine.Step, error) {
	org := nginxOrg(cfg)
	if !org && cfg.DBSource != SourceMariaDB {
		return nil, nil
	}
	codename, err := codenameOrRefuse(sys)
	if err != nil {
		return nil, err
	}
	var out []engine.Step
	if org {
		r, err := repoNginx(codename, cfg.WebSource == SourceNginxMainline)
		if err != nil {
			return nil, fmt.Errorf("dựng kho nginx.org: %w", err)
		}
		out = append(out, r)
	}
	if cfg.DBSource == SourceMariaDB {
		r, err := repoMariaDB(codename, cfg.DBLine)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// codenameOrRefuse là osCodename, nhưng lỗi thành lời từ chối: kho ngoài chọn
// bản gói theo tên mã, đoán sai là cài gói của bản Ubuntu khác.
func codenameOrRefuse(sys system.System) (string, error) {
	codename, err := osCodename(sys)
	if err != nil {
		return "", refuse("precondition_failed", i18n.T(
			"không đọc được tên mã bản Ubuntu (VERSION_CODENAME trong /etc/os-release) để chọn đúng kho: "+err.Error(),
			"could not read the Ubuntu codename (VERSION_CODENAME in /etc/os-release) to pick the right repository: "+err.Error()))
	}
	return codename, nil
}

// nginxOrgSteps là các bước thích nghi bố cục của gói nginx.org với cách tpan
// đặt vhost (sites-available, sites-enabled, nhóm www-data). Nằm sau bước cài
// gói, trước các bước ghi site mặc định.
//
// running false (chuyển web server, nginx đang tắt và chỉ chạy ở WebSwap):
// không có bước bật dịch vụ, và thêm user vào nhóm mà không khởi động lại
// (nhóm có hiệu lực khi nginx khởi động ở WebSwap). Xem webSteps.
func nginxOrgSteps(test, reload *system.Cmd, running bool) ([]engine.Step, error) {
	inc, err := templates.NginxOrgInclude()
	if err != nil {
		return nil, fmt.Errorf("dựng bước thích nghi nginx.org: %w", err)
	}
	var ss []engine.Step
	if running {
		// Gói nginx.org có bật dịch vụ sau khi cài hay không thì các bước dưới
		// đều reload nginx, nên bật nó trước (reload một dịch vụ đang tắt là lỗi).
		ss = append(ss, &steps.Service{Unit: "nginx"})
	}
	add := &steps.UserInGroup{User: "nginx", Group: "www-data"}
	var group engine.Step = add
	if running {
		group = &groupRestart{add: add, unit: "nginx", test: test}
	}
	return append(ss,
		&steps.Dir{Path: "/etc/nginx/sites-available", Perm: 0o755, Owner: system.Root},
		&steps.Dir{Path: "/etc/nginx/sites-enabled", Perm: 0o755, Owner: system.Root},
		&steps.File{Path: nginxOrgConf, Content: inc, Perm: 0o644, Owner: system.Root, Test: test, Reload: reload,
			Title: i18n.T("Cho nginx của nginx.org đọc sites-enabled", "Make nginx.org's nginx read sites-enabled"),
			Why: i18n.T(
				"Gói nginx của nginx.org chỉ đọc /etc/nginx/conf.d/*.conf, không biết sites-enabled là nơi tpan bật vhost. Thiếu file này thì site mặc định và mọi site của tpan không được nạp.",
				"The nginx.org package only reads /etc/nginx/conf.d/*.conf and knows nothing about sites-enabled, where tpan enables vhosts. Without this file tpan's default site and every site are never loaded.")},
		// Câu mặc định của Absent (site mặc định của distro) đúng với file này.
		&steps.Absent{Path: "/etc/nginx/conf.d/default.conf", Test: test, Reload: reload},
		group,
	), nil
}

const nginxOrgConf = "/etc/nginx/conf.d/tpan.conf"

// groupRestart thêm user vào nhóm rồi khởi động lại dịch vụ chạy dưới user
// đó, CHỈ khi bước này thật sự thêm. Không dùng steps.ServiceRestart đứng riêng:
// Check của nó sai cho tới khi chính nó chạy, nên chạy lại Plan trên máy đã cài
// sẽ khởi động lại nginx mỗi lần (gián đoạn mà không đổi gì).
type groupRestart struct {
	add  *steps.UserInGroup
	unit string
	test *system.Cmd
}

var _ engine.Step = (*groupRestart)(nil)

func (s *groupRestart) restart() *steps.ServiceRestart {
	return &steps.ServiceRestart{Unit: s.unit, Test: s.test}
}

func (s *groupRestart) Info() engine.StepInfo {
	in := s.add.Info()
	r := s.restart().Info()
	in.Title = i18n.T(in.Title.Vi+", khởi động lại "+s.unit, in.Title.En+", then restart "+s.unit)
	in.Why = i18n.T(
		in.Why.Vi+" Chỉ khởi động lại khi bước này thật sự thêm user vào nhóm; chạy lại trên máy đã đúng thì không gián đoạn gì.",
		in.Why.En+" The restart only happens when this step actually adds the user; running again on a correct machine interrupts nothing.")
	in.Commands = append(in.Commands, r.Commands...)
	in.Danger = true
	return in
}

func (s *groupRestart) Check(ctx context.Context, sys system.System) (bool, error) {
	return s.add.Check(ctx, sys)
}

func (s *groupRestart) Apply(ctx context.Context, sys system.System) error {
	if err := s.add.Apply(ctx, sys); err != nil {
		return err
	}
	if err := s.restart().Apply(ctx, sys); err != nil {
		// Engine không Undo bước có Apply lỗi: tự bỏ user ra khỏi nhóm.
		return errors.Join(err, s.add.Undo(ctx, sys))
	}
	return nil
}

// Undo bỏ user ra khỏi nhóm rồi khởi động lại để tiến trình đang chạy cũng mất nhóm.
func (s *groupRestart) Undo(ctx context.Context, sys system.System) error {
	if err := s.add.Undo(ctx, sys); err != nil {
		return err
	}
	return s.restart().Apply(ctx, sys)
}
