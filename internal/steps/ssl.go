package steps

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// Các bước SSL của một site (mục 6.3 của docs/KE-HOACH.md) và bước xoá chứng
// chỉ khi xoá hẳn site (mục 6.4). Hợp đồng: ssl_types.go.

// ACME directory của Let's Encrypt. Core chọn một trong hai (hoặc settings
// ssl.server) rồi đưa vào CertbotCert.Server.
const (
	LetsEncryptProd    = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// Mọi Step của file này phải thoả engine.Step.
var (
	_ engine.Step = (*CertbotCert)(nil)
	_ engine.Step = (*ACMEProbe)(nil)
	_ engine.Step = (*CertbotDelete)(nil)
	_ engine.Step = (*WPSiteURL)(nil)
)

// sslServerRe: địa chỉ máy chủ ACME chỉ là https://host[:cổng][/đường-dẫn],
// không khoảng trắng, không ký tự điều khiển, không query. Chuỗi này đi thẳng
// vào argv của certbot và vào so sánh với file renewal, nên chặn mọi thứ lạ.
var sslServerRe = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]*)?$`)

// sslChallengeDir là thư mục certbot --webroot ghi file thử thách vào; vhost
// cổng 80 của mọi site và site mặc định phục vụ đúng thư mục này.
func sslChallengeDir(webroot string) string { return webroot + "/.well-known/acme-challenge" }

// sslCheckWebroot: webroot là đường dẫn tuyệt đối đã chuẩn.
func sslCheckWebroot(w string) error { return checkPath("thư mục webroot của ACME", w) }

// ================================================================ CertbotCert

// CertbotCert xin chứng chỉ cho một site bằng webroot (mục 6.3, bước 2). Xem
// ssl_types.go về --force-renewal và vì sao Undo không xoá chứng chỉ.
type CertbotCert struct {
	Layout  site.Layout
	Webroot string // /var/lib/tpan/acme
	Email   validate.Email
	Server  string
}

func (s *CertbotCert) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := sslCheckWebroot(s.Webroot); err != nil {
		return err
	}
	if s.Email.IsZero() {
		return errors.New("chưa có email cho Let's Encrypt")
	}
	// Email đã qua validate, nhưng "-x@example.com" vẫn hợp lệ theo chuẩn mail;
	// đứng sau -m thì certbot đọc nó thành tuỳ chọn. Chặn ở lớp cuối.
	if strings.HasPrefix(s.Email.String(), "-") {
		return fmt.Errorf("email %q bắt đầu bằng dấu '-'; certbot sẽ đọc nhầm thành tuỳ chọn", s.Email)
	}
	if !sslServerRe.MatchString(s.Server) {
		return fmt.Errorf("địa chỉ máy chủ ACME %q không hợp lệ (chỉ nhận https://host[:cổng]/đường-dẫn)", s.Server)
	}
	return nil
}

func (s *CertbotCert) cmd(force bool) system.Cmd {
	d := s.Layout.Domain.String()
	args := []string{"certonly", "--webroot", "-w", s.Webroot, "-d", d, "--cert-name", d,
		"--non-interactive", "--agree-tos", "-m", s.Email.String(), "--server", s.Server}
	if force {
		args = append(args, "--force-renewal")
	}
	return system.Cmd{Name: "certbot", Args: args}
}

func (s *CertbotCert) Info() engine.StepInfo {
	d := s.Layout.Domain.String()
	return engine.StepInfo{
		ID:    "ssl.cert:" + d,
		Title: i18n.T("Xin chứng chỉ Let's Encrypt cho "+d, "Get a Let's Encrypt certificate for "+d),
		Why: i18n.T(
			"Không có chứng chỉ thì trình duyệt không mở được https://"+d+" mà không báo lỗi, và mật khẩu đăng nhập WordPress đi qua mạng ở dạng đọc được. Hoàn tác không xoá chứng chỉ: xoá đi thì mỗi lần thử lại tốn thêm một lượt trong hạn mức 5 chứng chỉ cùng tên mỗi 7 ngày của Let's Encrypt; chứng chỉ nằm trên đĩa không làm gì khi vhost chưa dùng nó.",
			"Without a certificate browsers cannot open https://"+d+" without an error, and WordPress login passwords cross the network readable. Undo keeps the certificate: deleting it would cost another of Let's Encrypt's 5 certificates per name per 7 days on every retry; a certificate on disk does nothing while no vhost uses it.",
		),
		Commands: []string{s.cmd(false).String()},
		NoUndo: i18n.T(
			"Giữ chứng chỉ để lần chạy lại không tốn lượt của Let's Encrypt. Chứng chỉ chỉ bị xoá khi xoá hẳn site.",
			"The certificate is kept so a retry does not use up Let's Encrypt's quota. It is only deleted when the site is purged.",
		),
	}
}

// renewalServer đọc dòng `server = ...` trong file renewal của site. exists
// false: chưa có lineage nào cho domain này.
func (s *CertbotCert) renewalServer(sys system.System) (server string, exists bool, err error) {
	p := s.Layout.CertRenewal()
	data, err := sys.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc %s: %w", p, err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "server" {
			return strings.TrimSpace(v), true, nil
		}
	}
	return "", true, nil
}

// Check: có fullchain VÀ chứng chỉ do đúng máy chủ ACME này cấp. Đọc fullchain
// bằng ReadFile (đi theo symlink): trên máy thật live/<domain>/fullchain.pem là
// symlink sang archive/, Stat (không đi theo symlink) không nói được đích còn không.
func (s *CertbotCert) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	p := s.Layout.CertFullchain()
	if _, err := sys.ReadFile(p); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("đọc %s: %w", p, err)
	}
	server, _, err := s.renewalServer(sys)
	if err != nil {
		return false, err
	}
	return server == s.Server, nil
}

func (s *CertbotCert) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	server, exists, err := s.renewalServer(sys)
	if err != nil {
		return err
	}
	// Đã có chứng chỉ của máy chủ khác (staging → thật): certbot thấy chứng
	// chỉ cũ chưa tới hạn thì không xin lại, nên phải ép.
	force := exists && server != s.Server
	if res, err := sys.Run(ctx, s.cmd(force)); err != nil {
		return withStdout(fmt.Errorf("xin chứng chỉ cho %s: %w", s.Layout.Domain, err), res)
	}
	return nil
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *CertbotCert) Undo(context.Context, system.System) error { return nil }

// ================================================================== ACMEProbe

// ACMEProbe thử đường HTTP mà Let's Encrypt sẽ đi trước khi xin chứng chỉ
// (mục 6.3, bước 1, ý 2). Xem ssl_types.go.
type ACMEProbe struct {
	Layout  site.Layout
	Webroot string
	Cert    *CertbotCert

	passed bool // Apply của chính bước này đã thành công
}

// sslProbeBodyMax là số byte tối đa của câu trả lời curl đưa vào lỗi: đủ để
// thấy trang 404 hay trang của site khác, không làm thông báo dài cả trang.
const sslProbeBodyMax = 300

func (s *ACMEProbe) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := sslCheckWebroot(s.Webroot); err != nil {
		return err
	}
	if s.Cert != nil {
		// Không bỏ qua bước thử chỉ vì chứng chỉ của một site khác đã có.
		if s.Cert.Layout != s.Layout || s.Cert.Webroot != s.Webroot {
			return errors.New("bước xin chứng chỉ đi kèm không cùng site hoặc cùng webroot với bước thử")
		}
		return s.Cert.validate()
	}
	return nil
}

func (s *ACMEProbe) curl(name string) system.Cmd {
	url := "http://" + s.Layout.Domain.String() + "/.well-known/acme-challenge/" + name
	return system.Cmd{Name: "curl", Args: []string{"-sS", "-m", "10", url}}
}

func (s *ACMEProbe) Info() engine.StepInfo {
	d := s.Layout.Domain.String()
	return engine.StepInfo{
		ID:    "ssl.probe:" + d,
		Title: i18n.T("Thử đường HTTP mà Let's Encrypt sẽ kiểm cho "+d, "Test the HTTP path Let's Encrypt will check for "+d),
		Why: i18n.T(
			"Let's Encrypt chỉ cấp chứng chỉ khi gọi được http://"+d+"/.well-known/acme-challenge/ tới đúng máy này. Tự thử trước bằng một file ngẫu nhiên thì lỗi DNS hay vhost hiện ra ngay, không tốn một lượt trong hạn mức của Let's Encrypt.",
			"Let's Encrypt only issues a certificate when http://"+d+"/.well-known/acme-challenge/ reaches this machine. Testing first with a random file shows DNS or vhost problems right away without using up Let's Encrypt's quota.",
		),
		Commands: []string{s.curl("<tên-ngẫu-nhiên>").String()},
		NoUndo: i18n.T(
			"Bước này không để lại gì trên máy: file thử luôn bị xoá ngay sau khi gọi.",
			"This step leaves nothing behind: the test file is always deleted right after the request.",
		),
	}
}

func (s *ACMEProbe) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	if s.passed {
		return true, nil
	}
	if s.Cert == nil {
		return false, nil
	}
	return s.Cert.Check(ctx, sys)
}

func (s *ACMEProbe) Apply(ctx context.Context, sys system.System) (err error) {
	if err := s.validate(); err != nil {
		return err
	}
	// Webroot do bước cài stack tạo; thiếu thì vhost cũng không trỏ tới đâu.
	fi, err := sys.Stat(s.Webroot)
	if err != nil {
		return fmt.Errorf("thư mục %s chưa có (stack chưa cài xong?): %w", s.Webroot, err)
	}
	if !fi.Mode.IsDir() {
		return fmt.Errorf("%s không phải thư mục", s.Webroot)
	}
	dir := sslChallengeDir(s.Webroot)
	created, err := missingDirs(sys, dir)
	if err != nil {
		return err
	}
	if err := sys.MkdirAll(dir, 0o755, system.Root); err != nil {
		err = fmt.Errorf("tạo %s: %w", dir, err)
		return errors.Join(err, removeDirs(sys, created))
	}
	defer func() { err = errors.Join(err, removeDirs(sys, created)) }()

	// Tên và nội dung ngẫu nhiên: không đoán trước được, nên câu trả lời khớp
	// chỉ có thể đến từ đúng file này trên đúng máy này.
	name := strings.ToLower(rand.Text())
	want := rand.Text()
	file := dir + "/" + name
	if err := sys.WriteFileAtomic(file, []byte(want), 0o644, system.Root); err != nil {
		return fmt.Errorf("ghi file thử %s: %w", file, err)
	}
	defer func() {
		if rerr := sys.Remove(file); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("xoá file thử %s: %w", file, rerr))
		}
	}()

	cmd := s.curl(name)
	res, cerr := sys.Run(ctx, cmd)
	got := strings.TrimSpace(res.Stdout)
	if cerr == nil && got == want {
		s.passed = true
		return nil
	}
	d := s.Layout.Domain.String()
	where := fmt.Sprintf("%s theo DNS của máy; lệnh đã chạy: %s", sslResolve(ctx, sys, d), cmd.String())
	if cerr != nil {
		return fmt.Errorf("thử http://%s/.well-known/acme-challenge/ không tới được máy này (Let's Encrypt cũng sẽ thất bại): %w; %s", d, cerr, where)
	}
	return fmt.Errorf("thử http://%s/.well-known/acme-challenge/ không về đúng máy này (Let's Encrypt cũng sẽ thất bại): nội dung khác file thử: %s; %s",
		d, sslExcerpt(got), where)
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *ACMEProbe) Undo(context.Context, system.System) error { return nil }

// sslExcerpt cắt câu trả lời dài và thay ký tự điều khiển bằng dấu cách để
// thông báo lỗi luôn là một dòng đọc được.
func sslExcerpt(s string) string {
	if s == "" {
		return "(rỗng)"
	}
	if len(s) > sslProbeBodyMax {
		s = s[:sslProbeBodyMax] + "…"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// sslResolve nói domain đang trỏ về đâu theo resolver của máy (getent đọc cả
// /etc/hosts lẫn DNS, giống curl). Chỉ để làm rõ lỗi: lookup hỏng thì nói vậy.
func sslResolve(ctx context.Context, sys system.System, domain string) string {
	res, err := sys.Run(ctx, system.Cmd{Name: "getent", Args: []string{"ahosts", domain}})
	if err != nil {
		return domain + " không phân giải được (" + err.Error() + ")"
	}
	var ips []string
	for line := range strings.SplitSeq(res.Stdout, "\n") {
		if f := strings.Fields(line); len(f) > 0 && !slices.Contains(ips, f[0]) {
			ips = append(ips, f[0])
		}
	}
	if len(ips) == 0 {
		return domain + " không có địa chỉ nào"
	}
	return domain + " đang trỏ về " + strings.Join(ips, ", ")
}

// ============================================================== CertbotDelete

// CertbotDelete xoá chứng chỉ khi xoá hẳn site (mục 6.4).
type CertbotDelete struct{ Layout site.Layout }

func (s *CertbotDelete) cmd() system.Cmd {
	return system.Cmd{Name: "certbot", Args: []string{"delete", "--cert-name", s.Layout.Domain.String(), "--non-interactive"}}
}

func (s *CertbotDelete) Info() engine.StepInfo {
	d := s.Layout.Domain.String()
	return engine.StepInfo{
		ID:    "ssl.delete:" + d,
		Title: i18n.T("Xoá chứng chỉ của "+d, "Delete the certificate of "+d),
		Why: i18n.T(
			"Site đã bị xoá hẳn. Để lại chứng chỉ thì certbot vẫn gia hạn nó hai lần mỗi ngày, mà vhost không còn nên lần gia hạn nào cũng lỗi và tốn lượt của Let's Encrypt.",
			"The site has been purged. If the certificate stays, certbot keeps trying to renew it twice a day, and with no vhost left every attempt fails and uses up Let's Encrypt's quota.",
		),
		Commands: []string{s.cmd().String()},
		Danger:   true,
		NoUndo: i18n.T(
			"Chứng chỉ đã xoá không lấy lại được; site cần thì xin lại.",
			"A deleted certificate cannot be restored; request a new one if the site needs it.",
		),
	}
}

func (s *CertbotDelete) Check(_ context.Context, sys system.System) (bool, error) {
	if err := webCheckLayout(s.Layout); err != nil {
		return false, err
	}
	for _, p := range []string{s.Layout.CertRenewal(), s.Layout.CertDir()} {
		_, err := sys.Stat(p)
		if err == nil {
			return false, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("kiểm %s: %w", p, err)
		}
	}
	return true, nil
}

func (s *CertbotDelete) Apply(ctx context.Context, sys system.System) error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("xoá chứng chỉ của %s: %w", s.Layout.Domain, err), res)
	}
	return nil
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *CertbotDelete) Undo(context.Context, system.System) error { return nil }

// ================================================================== WPSiteURL

// WPSiteURL đặt hai tuỳ chọn home và siteurl của WordPress về URL. Không có
// bước này thì WordPress chuyển https về http, web server chuyển http về
// https: vòng lặp chuyển hướng.
type WPSiteURL struct {
	Layout site.Layout
	URL    string

	prev    map[string]string // tuỳ chọn Apply đã đổi → giá trị cũ
	applied bool
}

// sslWPOptions là hai tuỳ chọn giữ địa chỉ của site, theo thứ tự Apply đổi.
var sslWPOptions = []string{"home", "siteurl"}

func (s *WPSiteURL) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := wpCheckLayout(s.Layout); err != nil {
		return err
	}
	d := s.Layout.Domain.String()
	if s.URL != "http://"+d && s.URL != "https://"+d {
		return fmt.Errorf("địa chỉ site %q phải là http://%s hoặc https://%s", s.URL, d, d)
	}
	return nil
}

func (s *WPSiteURL) getCmd(opt string) system.Cmd {
	return wpRun(s.Layout, nil, "option", "get", opt, "--skip-plugins", "--skip-themes")
}

func (s *WPSiteURL) updateCmd(opt, val string) system.Cmd {
	return wpRun(s.Layout, nil, "option", "update", opt, val, "--skip-plugins", "--skip-themes", "--quiet")
}

func (s *WPSiteURL) Info() engine.StepInfo {
	d := s.Layout.Domain.String()
	var cmds []string
	for _, o := range sslWPOptions {
		c := s.updateCmd(o, s.URL)
		cmds = append(cmds, c.String())
	}
	return engine.StepInfo{
		ID:    "wp.siteurl:" + d,
		Title: i18n.T("Đặt địa chỉ WordPress của "+d+" là "+s.URL, "Set the WordPress address of "+d+" to "+s.URL),
		Why: i18n.T(
			"WordPress tự chuyển khách về địa chỉ ghi trong home và siteurl. Địa chỉ còn là http trong khi web server chuyển http sang https thì trình duyệt bị chuyển qua lại mãi và báo lỗi quá nhiều lần chuyển hướng.",
			"WordPress redirects visitors to the address stored in home and siteurl. If that is still http while the web server redirects http to https, the browser bounces back and forth and reports too many redirects.",
		),
		Commands: cmds,
	}
}

func (s *WPSiteURL) get(ctx context.Context, sys system.System, opt string) (string, error) {
	res, err := sys.Run(ctx, s.getCmd(opt))
	if err != nil {
		return "", withStdout(fmt.Errorf("đọc tuỳ chọn %s của WordPress: %w", opt, err), res)
	}
	// wp option get in giá trị rồi xuống dòng; chỉ bỏ dấu xuống dòng đó.
	return strings.TrimRight(res.Stdout, "\r\n"), nil
}

func (s *WPSiteURL) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	for _, o := range sslWPOptions {
		v, err := s.get(ctx, sys, o)
		if err != nil {
			return false, err
		}
		if v != s.URL {
			return false, nil
		}
	}
	return true, nil
}

// sslRestorable: giá trị cũ sẽ được đặt lại qua argv khi hoàn tác. Giá trị
// bắt đầu bằng '-' sẽ bị WP-CLI đọc thành tuỳ chọn, có ký tự điều khiển thì
// không phải một URL; gặp chúng thì không đổi gì để không mất đường quay lại.
func sslRestorable(v string) bool {
	return !strings.HasPrefix(v, "-") && !strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func (s *WPSiteURL) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	old := map[string]string{}
	for _, o := range sslWPOptions {
		v, err := s.get(ctx, sys, o)
		if err != nil {
			return err
		}
		if v == s.URL {
			continue
		}
		if !sslRestorable(v) {
			return fmt.Errorf("tuỳ chọn %s của WordPress đang là %q, không đặt lại được khi hoàn tác; sửa tay bằng: %s",
				o, sslExcerpt(v), s.updateCmd(o, s.URL).String())
		}
		old[o] = v
	}
	changed := map[string]string{}
	for _, o := range sslWPOptions {
		v, ok := old[o]
		if !ok {
			continue
		}
		if res, err := sys.Run(ctx, s.updateCmd(o, s.URL)); err != nil {
			err = withStdout(fmt.Errorf("đặt tuỳ chọn %s của WordPress thành %s: %w", o, s.URL, err), res)
			// Engine không Undo bước có Apply lỗi: tự trả các tuỳ chọn đã đổi.
			return errors.Join(err, s.restore(ctx, sys, changed))
		}
		changed[o] = v
	}
	s.prev, s.applied = changed, true
	return nil
}

func (s *WPSiteURL) restore(ctx context.Context, sys system.System, prev map[string]string) error {
	var errs []error
	for _, o := range slices.Backward(sslWPOptions) {
		v, ok := prev[o]
		if !ok {
			continue
		}
		if res, err := sys.Run(ctx, s.updateCmd(o, v)); err != nil {
			errs = append(errs, withStdout(fmt.Errorf("trả lại tuỳ chọn %s của WordPress: %w", o, err), res))
		}
	}
	return errors.Join(errs...)
}

// Undo đặt lại giá trị cũ của đúng những tuỳ chọn Apply đã đổi.
func (s *WPSiteURL) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("địa chỉ WordPress của %s: %w", s.Layout.Domain, errNotApplied)
	}
	return s.restore(ctx, sys, s.prev)
}

// ============================================================== Vhost và mặc định

// SiteVhostSSL giống SiteVhost nhưng ghi bản có cổng 443 (templates.SiteVhost
// với ssl true). File của M1 giữ vhost cổng 80 cũ nên Undo trả lại được.
func SiteVhostSSL(l site.Layout, web, phpVersion, acmeRoot string) ([]engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước vhost SSL: %w", err)
	}
	v, err := webVhostOf(l, web)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost SSL: %w", err)
	}
	conf, err := templates.SiteVhost(l, web, phpVersion, acmeRoot, true)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost SSL %s: %w", web, err)
	}
	d := l.Domain.String()
	return []engine.Step{
		&File{Path: v.conf, Content: conf, Perm: 0o644, Owner: system.Root, Test: v.test, Reload: v.reload,
			Title: i18n.T("Ghi vhost "+web+" có HTTPS cho "+d, "Write "+web+" HTTPS vhost for "+d),
			Why: i18n.T(
				"Vhost này cho "+web+" phục vụ https://"+d+" bằng chứng chỉ vừa xin và chuyển http sang https (trừ đường thử thách của Let's Encrypt, để gia hạn vẫn chạy). Thiếu nó thì chứng chỉ nằm trên đĩa mà không ai dùng.",
				"This vhost makes "+web+" serve https://"+d+" with the new certificate and redirect http to https (except Let's Encrypt's challenge path, so renewals keep working). Without it the certificate sits on disk unused.")},
		&Symlink{Target: v.conf, Link: v.link, Test: v.test, Reload: v.reload},
	}, nil
}

// Đường dẫn site mặc định cổng 443 (ssl_types.go).
const (
	sslNginxDefaultConf  = "/etc/nginx/sites-available/tpan-default-ssl.conf"
	sslNginxDefaultLink  = "/etc/nginx/sites-enabled/tpan-default-ssl.conf"
	sslApacheDefaultConf = "/etc/apache2/sites-available/000-tpan-default-ssl.conf"
	sslApacheDefaultLink = "/etc/apache2/sites-enabled/000-tpan-default-ssl.conf"
)

// DefaultSSL trả các bước của site mặc định cổng 443: request tới 443 với tên
// không có vhost bị từ chối thay vì rơi vào site đầu tiên có SSL.
func DefaultSSL(web string) ([]engine.Step, error) {
	why := i18n.T(
		"Khi đã mở cổng 443, request tới một tên không có vhost (quét IP, tên miền lạ trỏ về máy) phải bị từ chối. Thiếu site mặc định này thì "+web+" đưa chúng vào site SSL đầu tiên và lộ site đó cùng chứng chỉ của nó.",
		"Once port 443 is open, requests for a name without a vhost (IP scans, stray domains pointing here) must be refused. Without this default site "+web+" hands them to the first SSL site and exposes it and its certificate.")
	switch web {
	case "nginx":
		conf, err := templates.NginxDefaultSSL()
		if err != nil {
			return nil, fmt.Errorf("dựng bước site mặc định 443: %w", err)
		}
		test := &system.Cmd{Name: "nginx", Args: []string{"-t"}}
		reload := &system.Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}}
		return []engine.Step{
			&File{Path: sslNginxDefaultConf, Content: conf, Perm: 0o644, Owner: system.Root, Test: test, Reload: reload,
				Title: i18n.T("Ghi site mặc định cổng 443 của Nginx", "Write Nginx's default site on port 443"), Why: why},
			&Symlink{Target: sslNginxDefaultConf, Link: sslNginxDefaultLink, Test: test, Reload: reload},
		}, nil
	case "apache":
		conf, err := templates.ApacheDefaultSSL()
		if err != nil {
			return nil, fmt.Errorf("dựng bước site mặc định 443: %w", err)
		}
		test := &system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
		reload := &system.Cmd{Name: "systemctl", Args: []string{"reload", "apache2"}}
		return []engine.Step{
			// ssl-cert tạo cặp chứng chỉ tự ký snakeoil mà vhost mặc định 443 dùng.
			&AptInstall{Packages: []string{"ssl-cert"}},
			// Bật ssl trước khi ghi file: SSLEngine là lệnh của mod_ssl, configtest
			// báo sai cú pháp nếu module chưa bật.
			&ApacheModules{Enable: []string{"ssl"}},
			&File{Path: sslApacheDefaultConf, Content: conf, Perm: 0o644, Owner: system.Root, Test: test, Reload: reload,
				Title: i18n.T("Ghi site mặc định cổng 443 của Apache", "Write Apache's default site on port 443"), Why: why},
			&Symlink{Target: sslApacheDefaultConf, Link: sslApacheDefaultLink, Test: test, Reload: reload},
		}, nil
	}
	return nil, fmt.Errorf("dựng bước site mặc định 443: web server %q không hợp lệ (chỉ nhận nginx hoặc apache)", web)
}
