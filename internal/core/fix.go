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

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
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
		l := st.Layout()
		php := sitePHP(st, cfg)
		return engine.Plan{
			Target: l.Domain.String(),
			Title:  i18n.T("Khởi động lại PHP-FPM "+php+" để tạo lại socket của "+obj, "Restart PHP-FPM "+php+" to recreate the socket of "+obj),
			Steps:  []engine.Step{&steps.PHPFPMRestart{PHPVersion: php, Socket: l.SocketFor(php)}},
			Verify: []checks.Check{checks.PoolSocket(l, php)},
		}, nil
	case FixFileRewrite:
		file, err := f.managedFile(ctx, cfg, obj)
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
			Steps:  []engine.Step{file},
			Verify: []checks.Check{checks.Drift(obj, hex.EncodeToString(sum[:]), checks.DriftStage(obj))},
		}, nil
	case FixWebRewrite:
		if kind == "web.vhost" {
			return f.vhostRewrite(ctx, cfg, r.ID, obj)
		}
		return f.webRewrite(ctx, cfg, r.ID, kind)
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
		l := st.Layout()
		return engine.Plan{
			Target: l.Domain.String(),
			Title:  i18n.T("Mở khoá user DB của "+obj, "Unlock the database user of "+obj),
			Steps:  []engine.Step{&steps.DBUserUnlock{Layout: l}},
			Verify: []checks.Check{checks.DBAccess(l)},
		}, nil
	}
	return engine.Plan{}, noAutoFix(r.ID)
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
// pool, vhost, và .htaccess khi là Apache.
func siteFiles(cfg StackConfig, st site.Site) ([]*steps.File, error) {
	l := st.Layout()
	pool, err := steps.SitePool(l, sitePHP(st, cfg))
	if err != nil {
		return nil, err
	}
	ss := []engine.Step{pool}
	vhost, err := siteVhost(st, cfg)
	if err != nil {
		return nil, err
	}
	ss = append(ss, vhost...)
	if cfg.Web == Apache {
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
		if f, ok := s.(*steps.File); ok {
			out = append(out, f)
		}
	}
	return out
}

// managedFile tìm Step File ghi đúng path: của một site active, hoặc của stack.
// So bằng dấu bằng, nên path lạ (kể cả có "..") không bao giờ khớp. Không thấy:
// nil (file tpan không dựng lại được, như unit systemd của tpan setup).
func (f *Fixes) managedFile(ctx context.Context, cfg StackConfig, path string) (*steps.File, error) {
	sites, err := f.activeSites(ctx)
	if err != nil {
		return nil, err
	}
	for _, st := range sites {
		fl, err := siteFiles(cfg, st)
		if err != nil {
			return nil, fmt.Errorf("dựng lại file của site %s: %w", st.Domain, err)
		}
		for _, file := range fl {
			if file.Path == path {
				return file, nil
			}
		}
	}
	plan, err := f.stackPlan(cfg)
	if err != nil {
		return nil, fmt.Errorf("dựng lại file của stack: %w", err)
	}
	for _, file := range files(plan.Steps) {
		if file.Path == path {
			return file, nil
		}
	}
	return nil, nil
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
	if ss, err = f.dropAccepted(ctx, ss); err != nil {
		return engine.Plan{}, err
	}
	l := st.Layout()
	config := checks.NginxConfig()
	if cfg.Web == Apache {
		config = checks.ApacheConfig()
	}
	return engine.Plan{
		Target: obj,
		Title:  i18n.T("Ghi lại và bật lại vhost của "+obj, "Rewrite and re-enable the vhost of "+obj),
		Steps:  ss,
		Verify: []checks.Check{checks.VhostEnabled(string(cfg.Web), l), config},
	}, nil
}

// siteVhost dựng bước ghi vhost đúng loại của site: có SSL (M5) thì bản có
// cổng 443 và chuyển hướng 80, không thì bản chỉ cổng 80. Sửa tự động và
// phát hiện lệch phải dùng đúng bản này; dùng bản cổng 80 cho site có SSL là
// ghi đè vhost 443 và làm site mất HTTPS. Web server lấy từ cfg; bản PHP là
// của site (sitePHP), để vhost trỏ đúng socket của pool site đang dùng (M12).
func siteVhost(st site.Site, cfg StackConfig) ([]engine.Step, error) {
	web, php := string(cfg.Web), sitePHP(st, cfg)
	if st.SSL {
		return steps.SiteVhostSSL(st.Layout(), web, php, acmeRoot)
	}
	return steps.SiteVhost(st.Layout(), web, php, acmeRoot)
}
