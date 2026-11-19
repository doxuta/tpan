package core

// Ca sử dụng SSL (mục 6.3 của docs/KE-HOACH.md). Hợp đồng M5 (đợt 0): chữ ký,
// kiểu và hằng dưới đây là thứ server và cmd/tpan dựa vào.
//
// Plan "site.ssl" cho site <d>, đúng thứ tự:
//  1. Điều kiện đầu (chỉ đọc, trong PrepareIssue): site có trong bảng sites và
//     active, stack đã cài, có email (ssl.email), checks.DNSPointsHere không Fail.
//  2. Dir <acmeRoot>/.well-known/acme-challenge (0755, root).
//  3. steps.ACMEProbe (thử đường HTTP; bỏ qua khi chứng chỉ đã đúng).
//  4. steps.CertbotCert (Server theo SSLConfig.ServerURL).
//  5. steps.DefaultSSL(web): site mặc định 443 (và mod ssl của Apache).
//  6. steps.SiteVhostSSL: vhost có 443, cổng 80 chuyển hướng.
//  7. steps.WPSiteURL https://<d>.
//  8. File hook gia hạn /etc/letsencrypt/renewal-hooks/deploy/tpan (0755,
//     templates.RenewHook("/usr/local/bin/tpan")).
//  9. Kiểm chứng: checks.SiteChecksSSL, Service certbot.timer,
//     checks.CertRenewDryRun(l, server tuỳ chỉnh nếu có).
// Job thành công thì ghi site.SSL = true vào bảng sites.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// Khoá của SSL trong bảng settings (mục 4.4).
const (
	// SettingSSLEmail: email đăng ký với Let's Encrypt (thư báo sắp hết hạn).
	SettingSSLEmail = "ssl.email"
	// SettingSSLStaging: "1" bật, "0" tắt; chưa ghi thì hiểu là BẬT (mục 6.3).
	SettingSSLStaging = "ssl.staging"
	// SettingSSLServer: ẩn, chỉ cho test. Địa chỉ thư mục ACME (https://...)
	// thay cho Let's Encrypt; có giá trị thì thắng staging.
	SettingSSLServer = "ssl.server"
)

// RenewHookPath là hook certbot chạy sau mỗi lần gia hạn thành công.
const RenewHookPath = "/etc/letsencrypt/renewal-hooks/deploy/tpan"

// tpanBin là chỗ `tpan setup` đặt chương trình; hook gia hạn gọi đường dẫn này.
const tpanBin = "/usr/local/bin/tpan"

// sslServerRe là cùng luật steps.CertbotCert dùng cho địa chỉ máy chủ ACME
// (https://host[:cổng][/đường-dẫn], không khoảng trắng, không query). Chép
// sang đây để từ chối ngay lúc ghi setting: nhận một giá trị mà bước certbot
// từ chối thì mọi lần xin chứng chỉ sau đó đều hỏng giữa chừng.
var sslServerRe = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]*)?$`)

// SSLConfig là cấu hình SSL đọc từ settings.
type SSLConfig struct {
	Email   string `json:"email"`
	Staging bool   `json:"staging"`
	// Server là ssl.server (rỗng khi dùng Let's Encrypt). Không hiện trên giao diện.
	Server string `json:"server,omitempty"`
}

// ServerURL là địa chỉ ACME đưa cho certbot: Server nếu có, không thì
// steps.LetsEncryptStaging hoặc steps.LetsEncryptProd theo Staging.
func (c SSLConfig) ServerURL() string {
	switch {
	case c.Server != "":
		return c.Server
	case c.Staging:
		return steps.LetsEncryptStaging
	}
	return steps.LetsEncryptProd
}

// SSLSiteRow là một dòng của màn SSL: một site active và phép kiểm chứng chỉ
// của nó (checks.TLSCert; site chưa có SSL thì Cert là nil).
type SSLSiteRow struct {
	Domain validate.Domain `json:"domain"`
	SSL    bool            `json:"ssl"`
	Cert   *checks.Result  `json:"cert,omitempty"`
}

// SSLSettingsInput là thay đổi cấu hình; trường nil thì giữ nguyên.
type SSLSettingsInput struct {
	Email   *validate.Email
	Staging *bool
	// Server: chỉ CLI (cờ ẩn) đặt; chuỗi rỗng là xoá. Phải là https://...
	Server *string
}

// SSL gom những gì ca sử dụng SSL cần. Dựng mới cho mỗi lệnh.
type SSL struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore
	// Journal nhận job và sự kiện khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
}

// Config đọc cấu hình SSL.
func (s *SSL) Config(ctx context.Context) (SSLConfig, error) {
	get := func(key string) (string, bool, error) {
		v, ok, err := s.Settings.GetSetting(ctx, key)
		if err != nil {
			return "", false, fmt.Errorf("đọc setting %s: %w", key, err)
		}
		return v, ok, nil
	}
	var cfg SSLConfig
	var err error
	if cfg.Email, _, err = get(SettingSSLEmail); err != nil {
		return SSLConfig{}, err
	}
	if cfg.Server, _, err = get(SettingSSLServer); err != nil {
		return SSLConfig{}, err
	}
	staging, ok, err := get(SettingSSLStaging)
	if err != nil {
		return SSLConfig{}, err
	}
	// Chưa ghi thì BẬT: lúc phát triển luôn dùng staging (mục 6.3), nên muốn
	// xin chứng chỉ thật phải tắt có chủ ý.
	switch {
	case !ok || staging == "1":
		cfg.Staging = true
	case staging == "0":
	default:
		return SSLConfig{}, fmt.Errorf("setting %s có giá trị lạ %q (chỉ nhận 1 hoặc 0)", SettingSSLStaging, staging)
	}
	return cfg, nil
}

// SetConfig ghi thay đổi cấu hình. Server sai dạng thì từ chối ssl_server_invalid.
// Kiểm hết rồi mới ghi, để một trường sai không để lại cấu hình ghi dở.
func (s *SSL) SetConfig(ctx context.Context, in SSLSettingsInput) (SSLConfig, error) {
	var kv [][2]string
	if in.Email != nil {
		if in.Email.IsZero() {
			return SSLConfig{}, refuse(validate.CodeEmailInvalid, i18n.T("email đang trống", "the email is empty"))
		}
		kv = append(kv, [2]string{SettingSSLEmail, in.Email.String()})
	}
	if in.Staging != nil {
		v := "0"
		if *in.Staging {
			v = "1"
		}
		kv = append(kv, [2]string{SettingSSLStaging, v})
	}
	if in.Server != nil {
		if *in.Server != "" && !sslServerRe.MatchString(*in.Server) {
			return SSLConfig{}, refuse("ssl_server_invalid", i18n.T(
				fmt.Sprintf("địa chỉ máy chủ ACME %q không hợp lệ: chỉ nhận https://<máy>[:cổng][/đường-dẫn], không khoảng trắng, không dấu ?", *in.Server),
				fmt.Sprintf("the ACME server address %q is not valid: only https://<host>[:port][/path] is accepted, without spaces or '?'", *in.Server)))
		}
		kv = append(kv, [2]string{SettingSSLServer, *in.Server})
	}
	for _, p := range kv {
		if err := s.Settings.SetSetting(ctx, p[0], p[1]); err != nil {
			return SSLConfig{}, fmt.Errorf("ghi setting %s: %w", p[0], err)
		}
	}
	return s.Config(ctx)
}

// List trả mọi site active, sắp theo domain, kèm phép kiểm chứng chỉ của site có SSL.
func (s *SSL) List(ctx context.Context) ([]SSLSiteRow, error) {
	all, err := s.Store.Sites(ctx)
	if err != nil {
		return nil, err
	}
	rows := []SSLSiteRow{}
	for _, st := range all {
		if st.Status != site.Active {
			continue
		}
		row := SSLSiteRow{Domain: st.Domain, SSL: st.SSL}
		if st.SSL {
			r := checks.TLSCert(st.Layout()).Run(ctx, s.Sys)
			row.Cert = &r
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b SSLSiteRow) int { return strings.Compare(a.Domain.String(), b.Domain.String()) })
	return rows, nil
}

// SSLIssue là một lần xin (hoặc chạy lại) chứng chỉ đã qua điều kiện đầu. Dùng
// cho đúng một lần Execute.
type SSLIssue struct {
	Site   site.Site
	Layout site.Layout
	Config SSLConfig
	// Preconditions là kết quả mọi điều kiện đầu (kể cả Pass, Warn) để hiện ra.
	Preconditions []checks.Result
	Plan          engine.Plan

	ssl *SSL
}

// PrepareIssue làm phần chỉ đọc của "xin SSL": kiểm điều kiện đầu, dựng Plan.
// Từ chối thì trả *RefusedError: site_not_found, site_trashed,
// stack_not_installed, acme_email_missing, precondition_failed (kèm Checks).
func (s *SSL) PrepareIssue(ctx context.Context, d validate.Domain) (*SSLIssue, error) {
	// Dùng lại cửa kiểm của ca sử dụng site để lời từ chối giống hệt `tpan site`.
	sites := &Sites{Sys: s.Sys, Settings: s.Settings, Store: s.Store}
	st, err := sites.find(ctx, d)
	if err != nil {
		return nil, err
	}
	if st.Status == site.Trashed {
		return nil, refuse("site_trashed", i18n.T(
			"site "+d.String()+" đang ở thùng rác; khôi phục nó trước khi xin SSL",
			"site "+d.String()+" is in the trash; restore it before requesting SSL"))
	}
	if st.Status != site.Active {
		return nil, fmt.Errorf("site %s có trạng thái lạ %q trong DB", d, st.Status)
	}
	stack, err := sites.stack(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Email == "" {
		return nil, refuse("acme_email_missing", i18n.T(
			"chưa có email cho Let's Encrypt (dùng để báo chứng chỉ sắp hết hạn); đặt bằng: sudo tpan ssl settings --email <email>",
			"there is no email for Let's Encrypt (used for expiry notices); set it with: sudo tpan ssl settings --email <email>"))
	}
	email, err := validate.ParseEmail(cfg.Email)
	if err != nil {
		return nil, fmt.Errorf("setting %s có email không hợp lệ (đặt lại bằng tpan ssl settings --email): %w", SettingSSLEmail, err)
	}
	if cfg.Server != "" && !sslServerRe.MatchString(cfg.Server) {
		return nil, refuse("ssl_server_invalid", i18n.T(
			fmt.Sprintf("setting %s có địa chỉ máy chủ ACME không hợp lệ %q; xoá bằng: sudo tpan ssl settings --server ''", SettingSSLServer, cfg.Server),
			fmt.Sprintf("setting %s holds an invalid ACME server address %q; clear it with: sudo tpan ssl settings --server ''", SettingSSLServer, cfg.Server)))
	}

	l := st.Layout()
	// DNS không có bản ghi A thì Let's Encrypt chắc chắn hỏng: dừng ở đây, không
	// tốn lượt. Warn (máy sau NAT, AAAA lạ) không chặn: bước thử HTTP quyết.
	results := checks.RunAll(ctx, s.Sys, []checks.Check{checks.DNSPointsHere(l)})
	var failed []checks.Result
	for _, r := range results {
		if r.Status == checks.Fail {
			failed = append(failed, r)
		}
	}
	if len(failed) > 0 {
		e := refuse("precondition_failed", i18n.T(
			"tên miền "+d.String()+" chưa sẵn sàng để xin chứng chỉ",
			"the domain "+d.String()+" is not ready for a certificate"))
		e.Checks = failed
		return nil, e
	}

	php := st.PHPVersion
	if php == "" {
		php = stack.PHPVersion
	}
	plan, err := sslPlan(string(stack.Web), php, l, email, cfg)
	if err != nil {
		return nil, err
	}
	return &SSLIssue{Site: st, Layout: l, Config: cfg, Preconditions: results, Plan: plan, ssl: s}, nil
}

// sslPlan dựng Plan "site.ssl" đúng thứ tự ở đầu file.
func sslPlan(web, php string, l site.Layout, email validate.Email, cfg SSLConfig) (engine.Plan, error) {
	d := l.Domain.String()
	wrap := func(err error) error { return fmt.Errorf("dựng Plan SSL cho %s: %w", d, err) }

	// Bước thử và bước xin dùng CHUNG một CertbotCert: bước thử hỏi nó "chứng
	// chỉ đã đúng chưa" để bỏ qua khi chạy lại.
	cert := &steps.CertbotCert{Layout: l, Webroot: acmeRoot, Email: email, Server: cfg.ServerURL()}
	ss := []engine.Step{
		&steps.Dir{Path: acmeRoot + "/.well-known/acme-challenge", Perm: 0o755, Owner: system.Root},
		&steps.ACMEProbe{Layout: l, Webroot: acmeRoot, Cert: cert},
		cert,
	}
	def, err := steps.DefaultSSL(web)
	if err != nil {
		return engine.Plan{}, wrap(err)
	}
	vhost, err := steps.SiteVhostSSL(l, web, acmeRoot)
	if err != nil {
		return engine.Plan{}, wrap(err)
	}
	hook, err := templates.RenewHook(tpanBin)
	if err != nil {
		return engine.Plan{}, wrap(err)
	}
	ss = append(ss, def...)
	ss = append(ss, vhost...)
	ss = append(ss,
		&steps.WPSiteURL{Layout: l, URL: "https://" + d},
		&steps.File{
			Path: RenewHookPath, Content: hook, Perm: 0o755, Owner: system.Root,
			Title: i18n.T("Ghi hook gia hạn chứng chỉ", "Write the certificate renewal hook"),
			Why: i18n.T(
				"certbot tự gia hạn chứng chỉ nhưng web server vẫn giữ chứng chỉ cũ trong bộ nhớ tới khi được reload. Hook này cho certbot gọi `tpan internal reload-web` sau mỗi lần gia hạn; thiếu nó thì chứng chỉ trên đĩa mới mà trình duyệt vẫn thấy chứng chỉ cũ hết hạn.",
				"certbot renews the certificate, but the web server keeps the old one in memory until it is reloaded. This hook makes certbot run `tpan internal reload-web` after every renewal; without it the file on disk is new while browsers still see the old, expired certificate."),
		},
	)

	verify := checks.SiteChecksSSL(web, php, l)
	verify = append(verify,
		checks.Service("certbot.timer", checks.StageTLS),
		// Chỉ server tuỳ chỉnh: không có --server thì certbot --dry-run luôn
		// dùng máy chủ staging của Let's Encrypt (kể cả khi chứng chỉ là bản
		// thật), không đọc file renewal; đó là cách thử gia hạn không tốn hạn
		// mức. Server tuỳ chỉnh (pebble trong e2e) thì phải nói rõ, không
		// certbot đi ra Let's Encrypt (thấy trên máy ảo, docs/thay-doi/M5.md).
		checks.CertRenewDryRun(l, cfg.Server),
	)
	return engine.Plan{
		Kind:   "site.ssl",
		Target: d,
		Title:  i18n.T("Xin chứng chỉ SSL cho "+d, "Get an SSL certificate for "+d),
		Steps:  ss,
		Verify: verify,
	}, nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (i *SSLIssue) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(i.ssl.Sys, engine.Options{}).Preview(ctx, i.Plan)
}

// Execute chạy Plan; job thành công thì ghi site.SSL = true. error chỉ khác nil
// khi không chạy được hoặc không ghi được bảng sites; job hỏng thì xem
// Report.Job.Status.
func (i *SSLIssue) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	s := i.ssl
	if s.Journal == nil {
		return engine.Report{}, errors.New("xin SSL: thiếu Journal, không ghi được lịch sử job")
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(ctx, i.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s: %w", i.Plan.Kind, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return rep, nil
	}
	st := i.Site
	st.SSL = true
	if err := s.Store.SaveSite(ctx, st); err != nil {
		// Không ghi được thì lần chạy lại `tpan site add` sẽ ghi vhost cổng 80 đè
		// lên vhost 443: nói rõ để người đọc chạy lại `tpan ssl issue`.
		return rep, fmt.Errorf("job %s đã cài chứng chỉ cho %s nhưng không ghi được vào DB của tpan: %w. Chạy lại `tpan ssl issue %s` (các bước đã đúng sẽ được bỏ qua)",
			rep.Job.ID, st.Domain, err, st.Domain)
	}
	i.Site = st
	return rep, nil
}

// ReloadWeb là việc của `tpan internal reload-web` (hook gia hạn gọi): đọc
// stack.web, kiểm config (nginx -t hoặc apachectl configtest), đúng thì
// `systemctl reload` web server đó. Stack chưa cài thì không làm gì.
func ReloadWeb(ctx context.Context, sys system.System, settings Settings) error {
	cfg, err := (&Stack{Settings: settings}).Config(ctx)
	if err != nil {
		return err
	}
	test := system.Cmd{Name: "nginx", Args: []string{"-t"}}
	switch cfg.Web {
	case "":
		return nil
	case Apache:
		test = system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
	}
	// Config sai thì reload sẽ làm web server từ chối nạp; giữ nguyên bản đang
	// chạy (vẫn phục vụ được) và báo lỗi để certbot ghi vào log của nó.
	if _, err := sys.Run(ctx, test); err != nil {
		return fmt.Errorf("kiểm config %s trước khi reload: %w", cfg.Web, err)
	}
	reload := system.Cmd{Name: "systemctl", Args: []string{"reload", webUnit(cfg.Web)}}
	// steps.Reload: với nginx, chỉ trả về khi worker cũ đã thôi nhận kết nối.
	if _, err := steps.Reload(ctx, sys, reload); err != nil {
		return fmt.Errorf("reload %s: %w", webUnit(cfg.Web), err)
	}
	return nil
}
