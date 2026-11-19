package core

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

// Đường dẫn cố định của stack (mục 5 và 6.1 của docs/KE-HOACH.md).
const (
	stateDir     = "/var/lib/tpan"
	acmeRoot     = "/var/lib/tpan/acme"
	cacheDir     = "/var/cache/tpan"
	mariadbCnf   = "/etc/mysql/mariadb.conf.d/99-tpan.cnf"
	logrotateCnf = "/etc/logrotate.d/tpan-sites"
)

// Settings là phần của bảng settings mà ca sử dụng cần. *store.Store thoả giao
// diện này. Tách ra giao diện để `--dry-run` chạy được khi chưa có file DB
// (EmptySettings) mà không phải tạo DB chỉ để đọc.
type Settings interface {
	GetSetting(ctx context.Context, key string) (value string, ok bool, err error)
	SetSetting(ctx context.Context, key, value string) error
}

// EmptySettings là bảng settings rỗng và chỉ đọc: máy chưa có tpan.db. Ghi vào
// thì lỗi, để một đường đi lỡ ghi lúc xem trước bị lộ ra ngay thay vì lặng lẽ mất.
type EmptySettings struct{}

func (EmptySettings) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (EmptySettings) SetSetting(_ context.Context, key, _ string) error {
	return fmt.Errorf("ghi setting %s: chưa có DB (chế độ chỉ xem)", key)
}

// RefusedError là lời từ chối có chủ ý: đầu vào sai hoặc máy không đạt điều
// kiện. Khác lỗi thường ở chỗ người dùng tự sửa được, nên CLI in nó thân thiện
// (không kèm chi tiết kỹ thuật) và API trả nó thành {"error": {...}} (mục 9).
type RefusedError struct {
	// Code ổn định cho script đọc: web_invalid, source_unsupported,
	// stack_switch_required, precondition_failed.
	Code    string    `json:"code"`
	Message i18n.Text `json:"message"`
	// Checks là các điều kiện đầu bị Fail, kèm Summary và ManualFix.
	Checks []checks.Result `json:"checks,omitempty"`
}

func (e *RefusedError) Error() string { return e.Code + ": " + e.Message.Vi }

func refuse(code string, msg i18n.Text) *RefusedError {
	return &RefusedError{Code: code, Message: msg}
}

// ParseWeb đọc tên web server người dùng gõ. Chỉ nhận đúng "nginx" hoặc "apache".
func ParseWeb(s string) (WebServer, error) {
	switch w := WebServer(s); w {
	case Nginx, Apache:
		return w, nil
	}
	return "", refuse("web_invalid", i18n.T(
		fmt.Sprintf("web server phải là nginx hoặc apache (nhận được %q)", s),
		fmt.Sprintf("the web server must be nginx or apache (got %q)", s)))
}

// webUnit là tên gói Ubuntu và cũng là tên dịch vụ systemd của web server.
// Apache là ngoại lệ dễ nhầm: tpan gọi nó "apache" (settings, template,
// checks.HTTPDefault), còn Ubuntu gọi "apache2" (gói, dịch vụ, /etc/apache2).
// Mọi chỗ cần tên phía Ubuntu đi qua hàm này.
func webUnit(w WebServer) string {
	if w == Apache {
		return "apache2"
	}
	return string(w)
}

// Packages suy danh sách gói apt từ cấu hình (mục 3). Đây là chỗ DUY NHẤT biết
// tên gói của stack: Step chỉ nhận danh sách. M12 thêm nguồn khác (nginx.org,
// kho MariaDB, kho PHP của Ondřej Surý) thì chỉ sửa hàm này.
//
// Không bao giờ có gói php, php<bản> hay libapache2-mod-php*: chúng kéo theo
// mod_php và buộc Apache về MPM prefork (mục 3).
func Packages(cfg StackConfig) ([]string, error) {
	if _, err := ParseWeb(string(cfg.Web)); err != nil {
		return nil, err
	}
	for _, s := range []struct {
		what string
		src  Source
	}{{"web", cfg.WebSource}, {"db", cfg.DBSource}, {"php", cfg.PHPSource}} {
		if s.src != "" && s.src != SourceUbuntu {
			return nil, refuse("source_unsupported", i18n.T(
				fmt.Sprintf("nguồn gói %q cho %s chưa hỗ trợ ở bản này; bản này chỉ cài từ kho của Ubuntu", s.src, s.what),
				fmt.Sprintf("package source %q for %s is not supported in this version; this version only installs from the Ubuntu archive", s.src, s.what)))
		}
	}
	return []string{
		"mariadb-server",
		"php-fpm", "php-cli", "php-mysql", "php-curl", "php-gd",
		"php-mbstring", "php-xml", "php-zip", "php-intl",
		"certbot", "curl",
		webUnit(cfg.Web),
	}, nil
}

// Stack gom những gì các ca sử dụng của stack cần. Dựng mới cho mỗi lệnh.
type Stack struct {
	Sys      system.System
	Settings Settings
	// Journal nhận job và sự kiện khi Execute; CLI đặt là chính store.
	// nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
	// Bản WP-CLI và sha512 của nó. Rỗng thì dùng steps.DefaultWPCLI*. Test đặt
	// sha512 của một file giả, vì không thể tải bản thật.
	WPCLIVersion string
	WPCLISHA512  string
}

func (s *Stack) wpcli() (version, sha string) {
	version, sha = s.WPCLIVersion, s.WPCLISHA512
	if version == "" {
		version = steps.DefaultWPCLIVersion
	}
	if sha == "" {
		sha = steps.DefaultWPCLISHA512
	}
	return version, sha
}

// Config đọc cấu hình stack từ settings. Nguồn gói rỗng hiểu là SourceUbuntu.
// Không kiểm nguồn có được hỗ trợ không: việc đó là của Packages, để
// `tpan stack status` vẫn hiện được một cấu hình tpan chưa hỗ trợ.
func (s *Stack) Config(ctx context.Context) (StackConfig, error) {
	var cfg StackConfig
	get := func(key string) (string, error) {
		v, _, err := s.Settings.GetSetting(ctx, key)
		if err != nil {
			return "", fmt.Errorf("đọc setting %s: %w", key, err)
		}
		return v, nil
	}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{SettingWeb, (*string)(&cfg.Web)},
		{SettingWebSource, (*string)(&cfg.WebSource)},
		{SettingDBSource, (*string)(&cfg.DBSource)},
		{SettingPHPSource, (*string)(&cfg.PHPSource)},
		{SettingPHPVersion, &cfg.PHPVersion},
	} {
		v, err := get(f.key)
		if err != nil {
			return StackConfig{}, err
		}
		*f.dst = v
	}
	if cfg.Web != "" && cfg.Web != Nginx && cfg.Web != Apache {
		return StackConfig{}, fmt.Errorf("setting %s có giá trị lạ %q (chỉ nhận nginx hoặc apache)", SettingWeb, cfg.Web)
	}
	for _, src := range []*Source{&cfg.WebSource, &cfg.DBSource, &cfg.PHPSource} {
		if *src == "" {
			*src = SourceUbuntu
		}
	}
	return cfg, nil
}

// Install là một lần cài stack đã qua điều kiện đầu, sẵn sàng xem trước hoặc
// chạy. Dùng cho đúng một lần Execute: các Step giữ trạng thái để hoàn tác.
type Install struct {
	// Config là cấu hình sẽ được ghi vào settings (PHPVersion rỗng tới khi xong).
	Config StackConfig
	// Preconditions là kết quả mọi điều kiện đầu, kể cả Pass và Unknown, để
	// người gọi hiện cho người dùng thấy.
	Preconditions []checks.Result
	Plan          engine.Plan

	stack *Stack
	// php do bước DetectPHP ghi lúc chạy; các phép kiểm Lazy đọc nó.
	php *string
}

// PrepareInstall làm phần chỉ đọc của "cài stack" (mục 6.1): đọc cấu hình, kiểm
// điều kiện đầu, dựng Plan. Không ghi gì, không lấy khoá. Bị từ chối thì trả
// *RefusedError.
func (s *Stack) PrepareInstall(ctx context.Context, web WebServer) (*Install, error) {
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
			fmt.Sprintf("máy này đã được tpan cài với %s; muốn đổi sang %s thì dùng `tpan stack switch --to %s` (chưa có ở bản này)", cfg.Web, web, web),
			fmt.Sprintf("tpan already installed this machine with %s; to change to %s use `tpan stack switch --to %s` (not in this version yet)", cfg.Web, web, web)))
	}
	cfg.Web = web
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

	php := new(string)
	version, sha := s.wpcli()
	plan, err := installPlan(cfg, pkgs, php, version, sha)
	if err != nil {
		return nil, err
	}
	return &Install{Config: cfg, Preconditions: results, Plan: plan, stack: s, php: php}, nil
}

// Preview là bản xem trước: từng bước, lý do, lệnh, file sẽ ghi, bước nào đã
// đúng sẽ bỏ qua. Chỉ đọc máy.
func (in *Install) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(in.stack.Sys, engine.Options{}).Preview(ctx, in.Plan)
}

// Execute ghi cấu hình rồi chạy Plan qua engine, với Journal của Stack.
//
// stack.web và các nguồn được ghi NGAY TRƯỚC khi chạy (mục 6.1): nếu job hỏng
// giữa chừng, máy đã có gói do tpan cài dở, và lần chạy lại cần biết "máy này
// thuộc tpan với web server X" để bỏ kiểm máy sạch. stack.php_version chỉ ghi
// khi job thành công; "stack đã cài" nghĩa là khoá này có giá trị.
//
// error chỉ khác nil khi không chạy được; job hỏng thì xem Report.Job.Status.
func (in *Install) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	s := in.stack
	if s.Journal == nil {
		return engine.Report{}, errors.New("cài stack: thiếu Journal, không ghi được lịch sử job")
	}
	for _, kv := range [][2]string{
		{SettingWeb, string(in.Config.Web)},
		{SettingWebSource, string(in.Config.WebSource)},
		{SettingDBSource, string(in.Config.DBSource)},
		{SettingPHPSource, string(in.Config.PHPSource)},
	} {
		if err := s.Settings.SetSetting(ctx, kv[0], kv[1]); err != nil {
			return engine.Report{}, fmt.Errorf("ghi setting %s trước khi cài: %w", kv[0], err)
		}
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(ctx, in.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s: %w", in.Plan.Kind, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return rep, nil
	}
	if *in.php == "" {
		// Không thể xảy ra: DetectPHP lỗi thì job đã hỏng. Báo thay vì ghi rỗng.
		return rep, fmt.Errorf("job %s thành công nhưng không dò được bản PHP", rep.Job.ID)
	}
	if err := s.Settings.SetSetting(ctx, SettingPHPVersion, *in.php); err != nil {
		return rep, fmt.Errorf("ghi setting %s sau job %s: %w", SettingPHPVersion, rep.Job.ID, err)
	}
	in.Config.PHPVersion = *in.php
	return rep, nil
}

// installPlan dựng Plan "stack.install" đúng thứ tự mục 6.1. Không Step nào cần
// bản PHP lúc dựng; chỉ phép kiểm cần, và chúng đọc *php lúc chạy qua checks.Lazy.
func installPlan(cfg StackConfig, pkgs []string, php *string, wpVersion, wpSHA string) (engine.Plan, error) {
	web := cfg.Web
	unit := webUnit(web)
	reloadWeb := &system.Cmd{Name: "systemctl", Args: []string{"reload", unit}}

	// Bước 1, 2: làm mới danh sách gói, cài gói.
	ss := []engine.Step{
		&steps.AptUpdate{Packages: pkgs},
		&steps.AptInstall{Packages: pkgs},
	}

	// Bước 3: MariaDB chạy, chỉ nghe 127.0.0.1, siết tài khoản. Tên 99- để file
	// này được đọc sau 50-server.cnf của Ubuntu và thắng nếu hai bên khác nhau.
	// Đổi bind-address không reload được nên phải restart: Danger.
	ss = append(ss,
		&steps.Service{Unit: "mariadb"},
		&steps.File{
			Path:    mariadbCnf,
			Content: []byte(templates.Header + "\n[mysqld]\nbind-address = 127.0.0.1\n"),
			Perm:    0o644,
			Owner:   system.Root,
			Reload:  &system.Cmd{Name: "systemctl", Args: []string{"restart", "mariadb"}},
			Danger:  true,
		},
		&steps.MariaDBSecure{},
	)

	// Bước 4: dò bản PHP-FPM vào biến cục bộ.
	ss = append(ss, &steps.DetectPHP{Result: php})

	// Bước 5: thư mục trạng thái và webroot ACME, rồi site mặc định. 0755 để
	// www-data đi qua được /var/lib/tpan tới acme; tpan.db tự có quyền 0600.
	ss = append(ss,
		&steps.Dir{Path: stateDir, Perm: 0o755, Owner: system.Root},
		&steps.Dir{Path: acmeRoot, Perm: 0o755, Owner: system.Root},
	)
	def := templates.DefaultSite{AcmeRoot: acmeRoot}
	switch web {
	case Nginx:
		conf, err := templates.NginxDefault(def)
		if err != nil {
			return engine.Plan{}, fmt.Errorf("dựng Plan cài stack: %w", err)
		}
		test := &system.Cmd{Name: "nginx", Args: []string{"-t"}}
		avail := "/etc/nginx/sites-available/tpan-default.conf"
		ss = append(ss,
			&steps.Absent{Path: "/etc/nginx/sites-enabled/default", Test: test, Reload: reloadWeb},
			&steps.File{Path: avail, Content: conf, Perm: 0o644, Owner: system.Root, Test: test, Reload: reloadWeb},
			&steps.Symlink{Target: avail, Link: "/etc/nginx/sites-enabled/tpan-default.conf", Test: test, Reload: reloadWeb},
		)
	case Apache:
		conf, err := templates.ApacheDefault(def)
		if err != nil {
			return engine.Plan{}, fmt.Errorf("dựng Plan cài stack: %w", err)
		}
		test := &system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
		// Tên 000- để Apache nạp file này đầu tiên và coi nó là site mặc định
		// (mục 6.1): ngoại lệ duy nhất của luật tiền tố tpan-.
		avail := "/etc/apache2/sites-available/000-tpan-default.conf"
		ss = append(ss,
			&steps.ApacheModules{
				// Chưa bật ssl ở M1: bật nó là Apache nghe cổng 443 mà chưa có vhost
				// nào hứng, nên trang mặc định của Ubuntu lộ ra ở 443 (thấy trên máy
				// ảo, docs/bang-chung/M1.log). M5 bật ssl cùng một vhost 443 mặc định.
				Enable:  apacheModules,
				Disable: []string{"mpm_prefork"},
			},
			&steps.Absent{Path: "/etc/apache2/sites-enabled/000-default.conf", Test: test, Reload: reloadWeb},
			&steps.File{Path: avail, Content: conf, Perm: 0o644, Owner: system.Root, Test: test, Reload: reloadWeb},
			&steps.Symlink{Target: avail, Link: "/etc/apache2/sites-enabled/000-tpan-default.conf", Test: test, Reload: reloadWeb},
		)
	}
	ss = append(ss, &steps.Service{Unit: unit})

	// Bước 6: WP-CLI, tải vào thư mục cache trước.
	ss = append(ss,
		&steps.Dir{Path: cacheDir, Perm: 0o755, Owner: system.Root},
		&steps.WPCLI{Version: wpVersion, SHA512: wpSHA},
	)

	// Bước 7: logrotate cho log của các site.
	lr, err := templates.LogrotateSites(templates.Logrotate{Web: string(web)})
	if err != nil {
		return engine.Plan{}, fmt.Errorf("dựng Plan cài stack: %w", err)
	}
	ss = append(ss, &steps.File{
		Path: logrotateCnf, Content: lr, Perm: 0o644, Owner: system.Root,
		Test: &system.Cmd{Name: "logrotate", Args: []string{"-d", logrotateCnf}},
	})

	return engine.Plan{
		Kind:   "stack.install",
		Target: string(web),
		Title:  i18n.T("Cài stack "+stackName(web), "Install the "+stackName(web)+" stack"),
		Steps:  ss,
		// Bước 8: kiểm chứng.
		Verify: stackChecks(web, func() string { return *php }, wpSHA),
	}, nil
}

func stackName(w WebServer) string {
	if w == Apache {
		return "LAMP (Apache)"
	}
	return "LEMP (Nginx)"
}

// stackChecks là bộ phép kiểm của stack, dùng chung cho kiểm chứng sau khi cài
// và cho health: "lúc cài đã đúng" và "bây giờ còn đúng" cùng một định nghĩa.
// php trả bản PHP lúc chạy; rỗng thì hai phép kiểm của PHP là Unknown.
func stackChecks(web WebServer, php func() string, wpSHA string) []checks.Check {
	config := checks.NginxConfig()
	if web == Apache {
		config = checks.ApacheConfig()
	}
	lazy := func(mk func(v string) checks.Check) checks.Check {
		return checks.Lazy(func() checks.Check {
			if v := php(); v != "" {
				return mk(v)
			}
			return nil
		})
	}
	return []checks.Check{
		checks.Service(webUnit(web), checks.StageWeb),
		lazy(func(v string) checks.Check { return checks.Service("php"+v+"-fpm", checks.StagePHP) }),
		checks.Service("mariadb", checks.StageDB),
		config,
		lazy(checks.PHPFPMConfig),
		checks.PortLocalOnly(3306, checks.StageDB),
		checks.HTTPDefault(string(web)),
		checks.MariaDBSecure(),
		checks.WPCLI(wpSHA),
		checks.Disk("/"),
	}
}

// Health là kết quả `tpan health` cho stack.
type Health struct {
	// Installed false thì stack chưa cài (chưa có stack.php_version) và không
	// phép kiểm nào được chạy.
	Installed bool            `json:"installed"`
	Config    StackConfig     `json:"config"`
	Results   []checks.Result `json:"results"`
}

// HasFail cho biết có phép kiểm nào Fail.
func (h Health) HasFail() bool {
	for _, r := range h.Results {
		if r.Status == checks.Fail {
			return true
		}
	}
	return false
}

// Health chạy bộ phép kiểm của stack (mục 6.7), dựng từ settings. Stack chưa cài
// thì không chạy gì: kiểm một stack không có chỉ ra một trang lỗi vô nghĩa.
func (s *Stack) Health(ctx context.Context) (Health, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return Health{}, err
	}
	h := Health{Installed: cfg.PHPVersion != "", Config: cfg}
	if !h.Installed {
		return h, nil
	}
	_, sha := s.wpcli()
	h.Results = checks.RunAll(ctx, s.Sys, stackChecks(cfg.Web, func() string { return cfg.PHPVersion }, sha))
	return h, nil
}

// Status là kết quả `tpan stack status`.
type Status struct {
	Config    StackConfig `json:"config"`
	Installed bool        `json:"installed"`
	// Services là trạng thái các dịch vụ đã biết tên: web server và MariaDB khi
	// đã có stack.web, PHP-FPM khi đã có stack.php_version.
	Services []checks.Result `json:"services"`
}

// Status cho biết cấu hình, đã cài hay chưa, và ba dịch vụ của stack.
// Cài dở (có stack.web, chưa có php_version) vẫn hiện được web và MariaDB.
func (s *Stack) Status(ctx context.Context) (Status, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{Config: cfg, Installed: cfg.PHPVersion != ""}
	var cs []checks.Check
	if cfg.Web != "" {
		cs = append(cs, checks.Service(webUnit(cfg.Web), checks.StageWeb))
	}
	if cfg.PHPVersion != "" {
		cs = append(cs, checks.Service("php"+cfg.PHPVersion+"-fpm", checks.StagePHP))
	}
	if cfg.Web != "" {
		cs = append(cs, checks.Service("mariadb", checks.StageDB))
	}
	if len(cs) > 0 {
		st.Services = checks.RunAll(ctx, s.Sys, cs)
	}
	return st, nil
}

// apacheModules là các mod Apache stack cần ở M1 (mục 6.1).
var apacheModules = []string{"mpm_event", "proxy_fcgi", "setenvif", "rewrite", "headers", "http2"}
