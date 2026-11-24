package core

// Tên miền và chứng chỉ Let's Encrypt của panel (M20, docs/thay-doi/M20-thiet-ke.md
// mục 2.5, 3.8, 11.2, 11.4, 11.7). Plan "panel.domain":
//
//  1. Điều kiện đầu (chỉ đọc, trước khoá): tên hợp lệ; không site nào (mọi
//     trạng thái) mang tên đó → panel_domain_is_site; stack đã cài (cổng 80 do
//     vhost mặc định của tpan hứng thử thách) → stack_not_installed; có
//     ssl.email → acme_email_missing; không nhận staging khi ssl.server rỗng →
//     panel_staging; panel không ở chế độ riêng tư (unit có --listen
//     127.0.0.1:) → panel_private; checks.DNSPointsHere không Fail →
//     precondition_failed.
//  2. panelNameFree: kiểm lại trùng tên với site, lần này DƯỚI khoá toàn cục.
//  3. steps.Dir thư mục thử thách, steps.ACMEProbe (thử đường HTTP trước khi
//     đụng chứng chỉ cũ: tên mới sai DNS không làm mất chứng chỉ đang có).
//  4. steps.CertbotDelete{CertName: tpan-panel} CHỈ khi lineage đang có mà SAN
//     không chứa tên mới.
//  5. steps.CertbotCert{CertName: tpan-panel} (lineage riêng, không bao giờ trùng
//     lineage của một site vì tên miền luôn có dấu chấm).
//  6. panelSetting: ghi panel.domain trong job (Undo trả giá trị cũ), không ở
//     after: job hỏng ở kiểm chứng thì setting về như cũ và panel không phục vụ
//     chứng chỉ mới (server chọn chứng chỉ theo setting, mục 11.2 ý 5).
//  7. Kiểm chứng: PanelACMECert, certbot.timer, gia hạn thử lineage tpan-panel,
//     PanelServes.
//
// after: tên cũ khác rỗng và khác tên mới thì xoá mọi phiên có host là tên cũ
// (cookie đặt dưới tên cũ chết trước khi tên đó có thể thành tên một site).

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"slices"
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

const (
	// SettingPanelDomain: tên miền của panel; rỗng hoặc chưa ghi là chưa đặt.
	SettingPanelDomain = "panel.domain"
	// PanelLineage là tên lineage certbot của chứng chỉ panel.
	PanelLineage = "tpan-panel"
	// PanelCertDir là thư mục certbot đặt chứng chỉ của lineage đó.
	PanelCertDir = "/etc/letsencrypt/live/" + PanelLineage
)

// panelUnitPath là file unit do `tpan setup` ghi; cổng và chế độ riêng tư đọc
// từ cờ --listen của nó.
const panelUnitPath = "/etc/systemd/system/tpan.service"

// panelDefaultPort là cổng của panel khi unit không nói (setup luôn ghi 8443).
const panelDefaultPort = "8443"

// PanelSessions là phần của store xoá phiên theo tên máy.
type PanelSessions interface {
	DeleteSessionsByHost(ctx context.Context, host string) error
}

// Panel gom những gì ca sử dụng "tên miền của panel" cần. Dựng mới cho mỗi lệnh.
type Panel struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore
	Sessions PanelSessions
	// Journal nhận job khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
	// Now nil thì dùng time.Now.
	Now func() time.Time
}

func (p *Panel) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Domain đọc tên miền của panel; ok=false khi chưa đặt (chưa ghi hoặc rỗng).
func (p *Panel) Domain(ctx context.Context) (validate.Domain, bool, error) {
	return panelDomain(ctx, p.Settings)
}

func panelDomain(ctx context.Context, settings Settings) (validate.Domain, bool, error) {
	v, ok, err := settings.GetSetting(ctx, SettingPanelDomain)
	if err != nil {
		return validate.Domain{}, false, fmt.Errorf("đọc setting %s: %w", SettingPanelDomain, err)
	}
	if !ok || v == "" {
		return validate.Domain{}, false, nil
	}
	d, err := validate.ParseDomain(v)
	if err != nil {
		return validate.Domain{}, false, fmt.Errorf("setting %s có giá trị hỏng %q (bỏ bằng: sudo tpan panel domain set --clear): %w", SettingPanelDomain, v, err)
	}
	return d, true, nil
}

// RefusePanelDomain từ chối domain_is_panel khi d đúng là tên miền của panel.
// Sites.PrepareAdd gọi ở nhánh site mới.
func RefusePanelDomain(ctx context.Context, settings Settings, d validate.Domain) error {
	cur, ok, err := panelDomain(ctx, settings)
	if err != nil {
		return err
	}
	if ok && cur == d {
		return refusePanelName(d)
	}
	return nil
}

func refusePanelName(d validate.Domain) *RefusedError {
	return refuse("domain_is_panel", i18n.T(
		d.String()+" đang là tên miền của panel tpan; một site không được dùng chung tên với panel. Chọn tên khác, hoặc bỏ tên của panel trước: sudo tpan panel domain set --clear",
		d.String()+" is the tpan panel's domain; a site cannot share its name with the panel. Pick another name, or drop the panel's domain first: sudo tpan panel domain set --clear"))
}

func refuseNameIsSite(d validate.Domain, what i18n.Text) *RefusedError {
	return refuse("panel_domain_is_site", i18n.T(
		d.String()+" đã thuộc về một site ("+what.Vi+"); panel không dùng chung tên với site: mã PHP của site sẽ đọc được cookie của panel. Chọn tên khác cho panel, ví dụ panel."+d.String(),
		d.String()+" already belongs to a site ("+what.En+"); the panel cannot share a name with a site: the site's PHP could read the panel's cookies. Pick another name for the panel, for example panel."+d.String()))
}

// PrepareDomain làm phần chỉ đọc của "đặt tên miền cho panel": điều kiện đầu
// (đầu file), dựng Plan. Không ghi gì.
func (p *Panel) PrepareDomain(ctx context.Context, d validate.Domain) (*Planned, error) {
	if d.IsZero() {
		return nil, refuse(validate.CodeDomainInvalid, i18n.T("thiếu tên miền", "the domain is missing"))
	}
	if _, found, err := p.Store.Site(ctx, d); err != nil {
		return nil, err
	} else if found {
		return nil, refuseNameIsSite(d, i18n.T("có trong danh sách site của tpan", "it is in tpan's site list"))
	}
	sites := &Sites{Sys: p.Sys, Settings: p.Settings, Store: p.Store}
	if _, err := sites.stack(ctx); err != nil {
		return nil, err
	}
	cfg, err := (&SSL{Settings: p.Settings}).Config(ctx)
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
	// Mục 11.4: chứng chỉ staging không được trình duyệt tin, mà người dùng đặt
	// tên cho panel chính là để thôi bấm qua cảnh báo. ssl.server (máy chủ ACME
	// thử của e2e) thì cho qua.
	if cfg.Server == "" && cfg.Staging {
		return nil, refuse("panel_staging", i18n.T(
			"chứng chỉ staging của Let's Encrypt không được trình duyệt tin; tắt staging: sudo tpan ssl settings --staging=false",
			"Let's Encrypt staging certificates are not trusted by browsers; turn staging off: sudo tpan ssl settings --staging=false"))
	}
	port, private, err := panelListen(p.Sys)
	if err != nil {
		return nil, err
	}
	if private {
		return nil, refuse("panel_private", i18n.T(
			"panel đang ở chế độ riêng tư (chỉ nghe 127.0.0.1, vào qua ssh -L): tên miền không dùng được qua đường hầm đó mà vẫn bị công khai qua nhật ký Certificate Transparency",
			"the panel is in private mode (listening on 127.0.0.1 only, reached through ssh -L): a domain does not work through that tunnel and would still be published in Certificate Transparency logs"))
	}
	l := site.LayoutOf(d)
	results := checks.RunAll(ctx, p.Sys, []checks.Check{checks.DNSPointsHere(l)})
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
	stale, err := panelLineageStale(p.Sys, d)
	if err != nil {
		return nil, err
	}

	// Bước thử và bước xin dùng CHUNG một CertbotCert (như sslPlan): bước thử
	// hỏi nó "chứng chỉ đã đúng chưa" để bỏ qua khi chạy lại.
	cert := &steps.CertbotCert{Layout: l, Webroot: acmeRoot, Email: email, Server: cfg.ServerURL(), CertName: PanelLineage}
	ss := []engine.Step{
		&panelNameFree{store: p.Store, d: d},
		&steps.Dir{Path: acmeRoot + "/.well-known/acme-challenge", Perm: 0o755, Owner: system.Root},
		&steps.ACMEProbe{Layout: l, Webroot: acmeRoot, Cert: cert},
	}
	if stale {
		ss = append(ss, &steps.CertbotDelete{CertName: PanelLineage})
	}
	set := &panelSetting{settings: p.Settings, value: d.String()}
	ss = append(ss, cert, set)
	plan := engine.Plan{
		Kind:   "panel.domain",
		Target: d.String(),
		Title:  i18n.T("Đặt tên miền "+d.String()+" cho panel", "Set the panel domain to "+d.String()),
		Steps:  ss,
		Verify: []checks.Check{
			checks.PanelACMECert(d, p.now),
			checks.Service("certbot.timer", checks.StageTPAN),
			// cfg.Server, không phải ServerURL(): không có --server thì
			// certbot --dry-run dùng staging (xem sslPlan).
			checks.CertRenewDryRunName(PanelLineage, cfg.Server),
			checks.PanelServes(d, port),
		},
	}
	return newPlanned(p.Sys, p.Journal, plan, p.afterRename(set)), nil
}

// PrepareClear dựng "panel.domain.clear": ghi panel.domain rỗng rồi xoá lineage
// tpan-panel. Chưa đặt tên → panel_domain_not_set.
func (p *Panel) PrepareClear(ctx context.Context) (*Planned, error) {
	cur, ok, err := p.Domain(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("panel_domain_not_set", i18n.T(
			"panel chưa có tên miền nên không có gì để bỏ",
			"the panel has no domain, so there is nothing to drop"))
	}
	set := &panelSetting{settings: p.Settings, value: ""}
	plan := engine.Plan{
		Kind:   "panel.domain.clear",
		Target: cur.String(),
		Title:  i18n.T("Bỏ tên miền "+cur.String()+" của panel", "Drop the panel domain "+cur.String()),
		// Setting trước: panel chọn chứng chỉ theo setting (mục 11.2 ý 5), nên
		// bỏ tên là thôi phục vụ chứng chỉ ACME ngay, rồi mới xoá lineage.
		Steps: []engine.Step{set, &steps.CertbotDelete{CertName: PanelLineage}},
	}
	return newPlanned(p.Sys, p.Journal, plan, p.afterRename(set)), nil
}

// afterRename xoá phiên của tên cũ khi job đã đổi tên (mục 11.2 ý 3). Tên cũ
// lấy từ bước ghi setting (đọc dưới khoá); bước được bỏ qua là tên không đổi.
func (p *Panel) afterRename(set *panelSetting) func(context.Context, engine.Report) error {
	return func(ctx context.Context, _ engine.Report) error {
		if !set.applied || set.old == "" || set.old == set.value {
			return nil
		}
		if p.Sessions == nil {
			return errors.New("thiếu nơi xoá phiên theo tên máy")
		}
		if err := p.Sessions.DeleteSessionsByHost(ctx, set.old); err != nil {
			return fmt.Errorf("xoá phiên đăng nhập dưới tên cũ %s: %w", set.old, err)
		}
		return nil
	}
}

// panelListen đọc cờ --listen trong ExecStart của unit: cổng (không có thì
// 8443) và chế độ riêng tư (127.0.0.1:...). Không có file unit: 8443, không
// riêng tư.
func panelListen(sys system.System) (port string, private bool, err error) {
	data, err := sys.ReadFile(panelUnitPath)
	if errors.Is(err, fs.ErrNotExist) {
		return panelDefaultPort, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc %s: %w", panelUnitPath, err)
	}
	for ln := range strings.Lines(string(data)) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(ln), "ExecStart=")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		for i, a := range f {
			v, ok := strings.CutPrefix(a, "--listen=")
			if !ok && a == "--listen" && i+1 < len(f) {
				v, ok = f[i+1], true
			}
			if !ok {
				continue
			}
			_, port, err := net.SplitHostPort(v)
			if err != nil || port == "" {
				return "", false, fmt.Errorf("%s có --listen %q không đọc được: %w", panelUnitPath, v, err)
			}
			return port, strings.HasPrefix(v, "127.0.0.1:"), nil
		}
	}
	return panelDefaultPort, false, nil
}

// panelLineageStale: lineage tpan-panel đang có (thư mục live hay file
// renewal) mà chứng chỉ của nó không mang tên d (kể cả mất hay hỏng file).
// Khi đó Plan xoá lineage trước khi xin, để certbot ra một lineage mới đúng tên
// thay vì phải sửa bộ tên của lineage cũ ở chế độ không hỏi.
func panelLineageStale(sys system.System, d validate.Domain) (bool, error) {
	exists := false
	for _, p := range []string{PanelCertDir, "/etc/letsencrypt/renewal/" + PanelLineage + ".conf"} {
		_, err := sys.Stat(p)
		switch {
		case err == nil:
			exists = true
		case !errors.Is(err, fs.ErrNotExist):
			return false, fmt.Errorf("kiểm %s: %w", p, err)
		}
	}
	if !exists {
		return false, nil
	}
	data, err := sys.ReadFile(PanelCertDir + "/fullchain.pem")
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc chứng chỉ của lineage %s: %w", PanelLineage, err)
	}
	return !certCovers(data, d.String()), nil
}

// certCovers: chứng chỉ ĐẦU TIÊN của dữ liệu PEM có tên dom trong SAN. Đọc
// hỏng thì coi là không (lineage hỏng cũng phải xoá đi xin lại).
func certCovers(data []byte, dom string) bool {
	for {
		var blk *pem.Block
		if blk, data = pem.Decode(data); blk == nil {
			return false
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		return err == nil && slices.ContainsFunc(c.DNSNames, func(n string) bool { return strings.EqualFold(n, dom) })
	}
}

// ---------------------------------------------------------------- các bước

// siteOnMachine nói d có dấu vết của một site không: dòng trong bảng sites
// (mọi trạng thái), thư mục /var/www/<d>, hay vhost tpan-<d>.conf của nginx
// hoặc apache. what là thứ tìm thấy (để nói trong lời từ chối).
func siteOnMachine(ctx context.Context, sys system.System, store SiteStore, d validate.Domain) (what i18n.Text, found bool, err error) {
	if _, ok, err := store.Site(ctx, d); err != nil {
		return i18n.Text{}, false, err
	} else if ok {
		return i18n.T("có trong danh sách site của tpan", "it is in tpan's site list"), true, nil
	}
	l := site.LayoutOf(d)
	for _, p := range []string{l.Dir, l.NginxConf, l.NginxLink, l.ApacheConf, l.ApacheLink} {
		ok, err := pathExists(sys, p)
		if err != nil {
			return i18n.Text{}, false, err
		}
		if ok {
			return i18n.T("trên máy đã có "+p, "the machine already has "+p), true, nil
		}
	}
	return i18n.Text{}, false, nil
}

// panelNameFree là bước "chỉ kiểm" đầu Plan panel.domain: kiểm lại tên không
// thuộc site nào, lần này dưới khoá toàn cục của engine (điều kiện đầu đọc
// trước khoá, một lần tạo site có thể chen vào giữa). Không có gì thì Check
// trả true và bước được bỏ qua; có thì Check trả lỗi panel_domain_is_site và
// job dừng trước khi đụng chứng chỉ.
type panelNameFree struct {
	store SiteStore
	d     validate.Domain
}

func (s *panelNameFree) Info() engine.StepInfo {
	d := s.d.String()
	return engine.StepInfo{
		ID:    "panel.namefree:" + d,
		Title: i18n.T("Kiểm "+d+" không thuộc site nào", "Check that "+d+" belongs to no site"),
		Why: i18n.T(
			"Panel và site mà chung một tên thì mã PHP của site đọc được cookie đăng nhập của panel. Điều kiện đầu đã kiểm, nhưng kiểm lại ở đây vì lúc này job đang giữ khoá nên không lệnh tạo site nào chen vào được.",
			"If the panel and a site share a name, the site's PHP can read the panel's login cookies. The preconditions already checked this, but it is checked again here because the job now holds the lock, so no site creation can slip in."),
		NoUndo: i18n.T("Bước này chỉ kiểm, không đổi gì trên máy.", "This step only checks; it changes nothing on the machine."),
	}
}

func (s *panelNameFree) Check(ctx context.Context, sys system.System) (bool, error) {
	what, found, err := siteOnMachine(ctx, sys, s.store, s.d)
	if err != nil {
		return false, err
	}
	if found {
		return false, refuseNameIsSite(s.d, what)
	}
	return true, nil
}

// Apply không bao giờ được gọi (Check chỉ trả true hoặc lỗi).
func (s *panelNameFree) Apply(context.Context, system.System) error { return nil }
func (s *panelNameFree) Undo(context.Context, system.System) error  { return nil }

// siteNotPanel là bước "chỉ kiểm" đầu Plan tạo site mới: đọc panel.domain dưới
// khoá toàn cục; trùng tên site thì Check trả lỗi domain_is_panel.
type siteNotPanel struct {
	settings Settings
	d        validate.Domain
}

func (s *siteNotPanel) Info() engine.StepInfo {
	d := s.d.String()
	return engine.StepInfo{
		ID:    "site.notpanel:" + d,
		Title: i18n.T("Kiểm "+d+" không phải tên miền của panel", "Check that "+d+" is not the panel's domain"),
		Why: i18n.T(
			"Site mà mang tên của panel thì mã PHP của site đọc được cookie đăng nhập của panel. Kiểm lại dưới khoá của job vì lệnh đặt tên cho panel có thể chạy ngay trước.",
			"A site with the panel's name lets the site's PHP read the panel's login cookies. It is checked again under the job lock because a command setting the panel's name may have just run."),
		NoUndo: i18n.T("Bước này chỉ kiểm, không đổi gì trên máy.", "This step only checks; it changes nothing on the machine."),
	}
}

func (s *siteNotPanel) Check(ctx context.Context, _ system.System) (bool, error) {
	if err := RefusePanelDomain(ctx, s.settings, s.d); err != nil {
		return false, err
	}
	return true, nil
}

// Apply không bao giờ được gọi (Check chỉ trả true hoặc lỗi).
func (s *siteNotPanel) Apply(context.Context, system.System) error { return nil }
func (s *siteNotPanel) Undo(context.Context, system.System) error  { return nil }

// panelSetting ghi panel.domain trong job. value rỗng là bỏ tên. Giữ giá trị
// cũ (đọc lúc Apply, dưới khoá) để Undo trả lại và để after biết tên cũ.
type panelSetting struct {
	settings Settings
	value    string

	old     string
	applied bool
}

func (s *panelSetting) Info() engine.StepInfo {
	why := i18n.T(
		"Panel chỉ đưa chứng chỉ Let's Encrypt cho trình duyệt khi tên được hỏi trùng tên ghi ở đây. Ghi trong job (không phải sau job) để job hỏng thì tên cũ được trả lại và panel không dùng chứng chỉ chưa kiểm xong.",
		"The panel only hands the Let's Encrypt certificate to browsers when the requested name equals the name stored here. It is written inside the job (not after it) so a failed job restores the old name and the panel never uses an unverified certificate.")
	if s.value == "" {
		return engine.StepInfo{
			ID:    "setting:" + SettingPanelDomain,
			Title: i18n.T("Bỏ tên miền của panel", "Drop the panel domain"),
			Why: i18n.T(
				"Bỏ tên thì panel thôi dùng chứng chỉ Let's Encrypt và quay về chứng chỉ tự ký; phiên đăng nhập dưới tên cũ bị đăng xuất để cookie đó không còn dùng được nếu tên ấy sau này thành tên một site.",
				"Without a name the panel stops using the Let's Encrypt certificate and goes back to its self-signed one; sessions under the old name are signed out so their cookies are useless if that name later becomes a site."),
		}
	}
	return engine.StepInfo{
		ID:    "setting:" + SettingPanelDomain,
		Title: i18n.T("Ghi tên miền của panel: "+s.value, "Save the panel domain: "+s.value),
		Why:   why,
	}
}

func (s *panelSetting) Check(ctx context.Context, _ system.System) (bool, error) {
	cur, _, err := s.settings.GetSetting(ctx, SettingPanelDomain)
	if err != nil {
		return false, fmt.Errorf("đọc setting %s: %w", SettingPanelDomain, err)
	}
	return cur == s.value, nil
}

func (s *panelSetting) Apply(ctx context.Context, _ system.System) error {
	cur, _, err := s.settings.GetSetting(ctx, SettingPanelDomain)
	if err != nil {
		return fmt.Errorf("đọc setting %s: %w", SettingPanelDomain, err)
	}
	if err := s.settings.SetSetting(ctx, SettingPanelDomain, s.value); err != nil {
		return fmt.Errorf("ghi setting %s: %w", SettingPanelDomain, err)
	}
	s.old, s.applied = cur, true
	return nil
}

func (s *panelSetting) Undo(ctx context.Context, _ system.System) error {
	if !s.applied {
		return nil
	}
	if err := s.settings.SetSetting(ctx, SettingPanelDomain, s.old); err != nil {
		return fmt.Errorf("trả lại setting %s: %w", SettingPanelDomain, err)
	}
	s.applied = false
	return nil
}
