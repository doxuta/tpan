package steps

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
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
	_ engine.Step = (*ACMEProbeNames)(nil)
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

	// Names (M19): bộ tên xin cho chứng chỉ. nil: chỉ tên chính (như trước
	// M19). Core gán ACMEProbeNames.CertbotNames: trước bước thử là mọi tên
	// muốn có (cho bản xem trước), sau bước thử là các tên đã qua. Tên chính
	// luôn đứng đầu lệnh dù Names trả nó ở đâu hay không trả.
	Names func() []validate.Domain
	// Probe (M19, S3): kết quả của ACMEProbeNames. Probe.Blocked mà chứng chỉ
	// đã có thì không chạy certbot, giữ chứng chỉ cũ. nil: không bao giờ chặn.
	Probe *ProbeResult
	// CertName (M20, mục 11.7 của docs/thay-doi/M20-thiet-ke.md) là tên lineage
	// của certbot. Rỗng: tên miền, đúng như chứng chỉ của site từ M5. Panel dùng
	// "tpan-panel": tên không có dấu chấm nên không bao giờ trùng lineage của một
	// site, và xoá hẳn site không xoá được chứng chỉ của panel.
	CertName string
}

// names là bộ tên đi vào lệnh certbot: tên chính đầu, không lặp.
func (s *CertbotCert) names() []validate.Domain {
	out := []validate.Domain{s.Layout.Domain}
	if s.Names == nil {
		return out
	}
	for _, n := range s.Names() {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// sslCertNameRe: tên lineage đi vào argv của certbot và vào đường dẫn dưới
// /etc/letsencrypt, nên chỉ nhận chữ thường, số, gạch ngang (không '/', không '.').
var sslCertNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

func sslCheckCertName(n string) error {
	if !sslCertNameRe.MatchString(n) {
		return fmt.Errorf("tên chứng chỉ %q không hợp lệ (chỉ nhận a-z, 0-9, '-', tối đa 64 ký tự)", n)
	}
	return nil
}

// Đường dẫn của một lineage đặt tên riêng (CertName khác rỗng).
func sslLiveDir(name string) string     { return "/etc/letsencrypt/live/" + name }
func sslRenewalConf(name string) string { return "/etc/letsencrypt/renewal/" + name + ".conf" }

// lineage, fullchain, renewal: tên và đường dẫn của chứng chỉ mà bước này lo.
// CertName rỗng thì đúng chuỗi của Layout như trước M20.
func (s *CertbotCert) lineage() string {
	if s.CertName != "" {
		return s.CertName
	}
	return s.Layout.Domain.String()
}

func (s *CertbotCert) fullchain() string {
	if s.CertName != "" {
		return sslLiveDir(s.CertName) + "/fullchain.pem"
	}
	return s.Layout.CertFullchain()
}

func (s *CertbotCert) renewal() string {
	if s.CertName != "" {
		return sslRenewalConf(s.CertName)
	}
	return s.Layout.CertRenewal()
}

func (s *CertbotCert) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if s.CertName != "" {
		if err := sslCheckCertName(s.CertName); err != nil {
			return err
		}
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
	if n := len(s.names()); n > site.MaxNames {
		return fmt.Errorf("chứng chỉ của %s xin %d tên, quá trần %d", s.Layout.Domain, n, site.MaxNames)
	}
	return nil
}

// cmd: tên chính là -d đầu tiên, tên phụ nối ngay sau (bản thử C2: cùng
// --cert-name thì certbot tự xin lại khi bộ tên đổi, không cần --expand).
// Names nil cho đúng argv trước M19.
func (s *CertbotCert) cmd(force bool) system.Cmd {
	args := []string{"certonly", "--webroot", "-w", s.Webroot}
	for _, n := range s.names() {
		args = append(args, "-d", n.String())
	}
	args = append(args, "--cert-name", s.lineage(),
		"--non-interactive", "--agree-tos", "-m", s.Email.String(), "--server", s.Server)
	if force {
		args = append(args, "--force-renewal")
	}
	return system.Cmd{Name: "certbot", Args: args}
}

func (s *CertbotCert) Info() engine.StepInfo {
	d := s.Layout.Domain.String()
	if s.CertName != "" {
		return engine.StepInfo{
			ID:    "ssl.cert:" + d,
			Title: i18n.T("Xin chứng chỉ Let's Encrypt cho panel ở "+d, "Get a Let's Encrypt certificate for the panel at "+d),
			Why: i18n.T(
				"Panel đang dùng chứng chỉ tự ký nên trình duyệt cảnh báo mỗi lần vào, và người dùng quen bấm \"vẫn vào\" thì không còn nhận ra khi có kẻ đứng giữa. Có chứng chỉ do Let's Encrypt ký cho "+d+" thì trình duyệt tin ngay. Chứng chỉ nằm ở lineage riêng "+s.CertName+", không chung thư mục với site nào.",
				"The panel uses a self-signed certificate, so browsers warn on every visit, and people used to clicking \"proceed anyway\" stop noticing a man in the middle. A Let's Encrypt certificate for "+d+" is trusted right away. It lives in its own lineage "+s.CertName+", never in a site's folder.",
			),
			Commands: []string{s.cmd(false).String()},
			NoUndo: i18n.T(
				"Giữ chứng chỉ để lần chạy lại không tốn lượt của Let's Encrypt. Panel chỉ dùng nó khi tên miền của panel đã được ghi, nên để trên đĩa không ảnh hưởng gì.",
				"The certificate is kept so a retry does not use up Let's Encrypt's quota. The panel only uses it once the panel domain is saved, so leaving it on disk changes nothing.",
			),
		}
	}
	why := i18n.T(
		"Không có chứng chỉ thì trình duyệt không mở được https://"+d+" mà không báo lỗi, và mật khẩu đăng nhập WordPress đi qua mạng ở dạng đọc được. Hoàn tác không xoá chứng chỉ: xoá đi thì mỗi lần thử lại tốn thêm một lượt trong hạn mức 5 chứng chỉ cùng tên mỗi 7 ngày của Let's Encrypt; chứng chỉ nằm trên đĩa không làm gì khi vhost chưa dùng nó.",
		"Without a certificate browsers cannot open https://"+d+" without an error, and WordPress login passwords cross the network readable. Undo keeps the certificate: deleting it would cost another of Let's Encrypt's 5 certificates per name per 7 days on every retry; a certificate on disk does nothing while no vhost uses it.",
	)
	if s.Names != nil {
		// Bản xem trước chưa biết tên nào qua bước thử (bước thử chạy lúc áp dụng).
		why.Vi += " Tên nào không qua bước thử sẽ bị bỏ khỏi lệnh; nếu tên đó đang nằm trong chứng chỉ thì không xin lại, giữ chứng chỉ cũ."
		why.En += " Names that fail the test step are dropped from the command; if such a name is already in the certificate, nothing is requested and the current certificate is kept."
	}
	return engine.StepInfo{
		ID:       "ssl.cert:" + d,
		Title:    i18n.T("Xin chứng chỉ Let's Encrypt cho "+d, "Get a Let's Encrypt certificate for "+d),
		Why:      why,
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
	p := s.renewal()
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
	p := s.fullchain()
	data, err := sys.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("đọc %s: %w", p, err)
	}
	// Lineage đặt tên riêng (M20) không mang tên miền trong đường dẫn: đổi tên
	// panel thì file vẫn ở đó mà cấp cho tên cũ. Phải đọc SAN mới biết đúng chưa.
	if s.CertName != "" && !sslCertCovers(data, s.Layout.Domain.String()) {
		return false, nil
	}
	// S3 (M19): một tên đang nằm trong chứng chỉ hỏng bước thử. Xin lại thì hoặc
	// certbot hỏng (có tên đó) hoặc chứng chỉ mất tên đó (bỏ nó): giữ nguyên.
	if s.held() {
		return true, nil
	}
	server, _, err := s.renewalServer(sys)
	if err != nil || server != s.Server {
		return false, err
	}
	if s.Names == nil {
		return true, nil
	}
	// Đủ tên mới là xong (M19): so SAN của cert.pem với bộ tên (so tập).
	cp := sslCertPem(s.Layout)
	pem, err := sys.ReadFile(cp)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("đọc %s: %w", cp, err)
	}
	have, err := CertNames(pem)
	if err != nil {
		return false, fmt.Errorf("đọc tên trong %s: %w", cp, err)
	}
	return sslSameNames(have, s.names()), nil
}

// held: bước thử đã chặn theo S3. Chỉ gọi sau khi đã thấy fullchain: chưa có
// chứng chỉ thì không có gì để giữ, xin với các tên đã qua.
func (s *CertbotCert) held() bool { return s.Probe != nil && s.Probe.Blocked }

// sslCertPem là chứng chỉ của riêng site (không kèm trung gian); SAN đọc ở đây.
// ponytail: site.Layout chưa có hàm cho file này (internal/site ngoài vùng M19 3a).
func sslCertPem(l site.Layout) string { return l.CertDir() + "/cert.pem" }

// sslSameNames so hai bộ tên như hai tập.
func sslSameNames(a, b []validate.Domain) bool {
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	for _, x := range b {
		if !slices.Contains(a, x) {
			return false
		}
	}
	return true
}

func (s *CertbotCert) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	if s.held() {
		p := s.Layout.CertFullchain()
		if _, err := sys.ReadFile(p); err == nil {
			return nil // giữ chứng chỉ cũ; core đọc Probe để báo lý do
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("đọc %s: %w", p, err)
		}
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

// sslCertCovers: chứng chỉ ĐẦU TIÊN trong dữ liệu PEM (chứng chỉ của máy chủ;
// các khối sau là CA trung gian) có tên d trong SAN. Đọc hỏng thì coi là không:
// Check trả false và bước xin lại, đúng việc phải làm với một file hỏng.
func sslCertCovers(data []byte, d string) bool {
	for {
		var blk *pem.Block
		if blk, data = pem.Decode(data); blk == nil {
			return false
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		return err == nil && slices.ContainsFunc(c.DNSNames, func(n string) bool { return strings.EqualFold(n, d) })
	}
}

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

func (s *ACMEProbe) curl(name string) system.Cmd { return sslProbeCurl(s.Layout.Domain, name) }

// sslProbeCurl gọi file thử qua đúng tên host, KHÔNG --resolve: đi đường DNS
// như Let's Encrypt sẽ đi.
func sslProbeCurl(host validate.Domain, name string) system.Cmd {
	url := "http://" + host.String() + "/.well-known/acme-challenge/" + name
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

func (s *ACMEProbe) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	err := sslWithChallengeDir(sys, s.Webroot, func(dir string) error {
		return sslProbeOne(ctx, sys, dir, s.Layout.Domain)
	})
	s.passed = err == nil
	return err
}

// sslWithChallengeDir bảo đảm thư mục thử thách có, chạy fn, rồi xoá đúng
// các thư mục nó đã tạo.
func sslWithChallengeDir(sys system.System, webroot string, fn func(dir string) error) (err error) {
	// Webroot do bước cài stack tạo; thiếu thì vhost cũng không trỏ tới đâu.
	fi, err := sys.Stat(webroot)
	if err != nil {
		return fmt.Errorf("thư mục %s chưa có (stack chưa cài xong?): %w", webroot, err)
	}
	if !fi.Mode.IsDir() {
		return fmt.Errorf("%s không phải thư mục", webroot)
	}
	dir := sslChallengeDir(webroot)
	created, err := missingDirs(sys, dir)
	if err != nil {
		return err
	}
	if err := sys.MkdirAll(dir, 0o755, system.Root); err != nil {
		err = fmt.Errorf("tạo %s: %w", dir, err)
		return errors.Join(err, removeDirs(sys, created))
	}
	defer func() { err = errors.Join(err, removeDirs(sys, created)) }()
	return fn(dir)
}

// sslProbeOne ghi một file ngẫu nhiên vào dir, gọi nó qua host, luôn xoá file.
// Lỗi nói host đang trỏ về đâu và lệnh đã chạy.
func sslProbeOne(ctx context.Context, sys system.System, dir string, host validate.Domain) (err error) {
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

	cmd := sslProbeCurl(host, name)
	res, cerr := sys.Run(ctx, cmd)
	got := strings.TrimSpace(res.Stdout)
	if cerr == nil && got == want {
		return nil
	}
	d := host.String()
	where := fmt.Sprintf("%s theo DNS của máy; lệnh đã chạy: %s", sslResolve(ctx, sys, d), cmd.String())
	if cerr != nil {
		return fmt.Errorf("thử http://%s/.well-known/acme-challenge/ không tới được máy này (Let's Encrypt cũng sẽ thất bại): %w; %s", d, cerr, where)
	}
	return fmt.Errorf("thử http://%s/.well-known/acme-challenge/ không về đúng máy này (Let's Encrypt cũng sẽ thất bại): nội dung khác file thử: %s; %s",
		d, sslExcerpt(got), where)
}

// ============================================================= ACMEProbeNames

// ProbeResult là kết quả của ACMEProbeNames, CertbotCert và core đọc.
type ProbeResult struct {
	// Passed: tên qua bước thử, tên chính đầu.
	Passed []validate.Domain
	// Failed: tên phụ hỏng bước thử (tên chính hỏng thì bước hỏng).
	Failed []ProbeFail
	// Blocked (S3): có tên hỏng mà đang nằm trong chứng chỉ; không chạy
	// certbot, giữ chứng chỉ cũ.
	Blocked bool
}

// ProbeFail là một tên phụ hỏng bước thử.
type ProbeFail struct {
	Name   validate.Domain
	Reason i18n.Text
	// Covered: tên đang nằm trong chứng chỉ (lý do của Blocked).
	Covered bool
}

// ACMEProbeNames thử đường HTTP của Let's Encrypt cho TỪNG tên của site trước
// khi xin chứng chỉ nhiều tên (M19, mục 3.6 bước 3). Một tên hỏng làm cả đơn
// certbot hỏng (bản thử C3), nên chỉ tên qua mới vào lệnh.
// Tên chính hỏng: bước hỏng. Tên phụ hỏng: ghi Result.Failed, bước vẫn đạt;
// tên đó nằm trong Covered thì Result.Blocked (S3).
type ACMEProbeNames struct {
	Layout  site.Layout
	Webroot string
	// Names: tên muốn có (tên chính được thêm vào đầu nếu thiếu).
	Names []validate.Domain
	// Covered: tên đang nằm trong chứng chỉ (in_cert = 1).
	Covered []validate.Domain
	// Result: nơi ghi kết quả; bắt buộc.
	Result *ProbeResult
	// Cert: khác nil thì chứng chỉ đã đúng đủ mọi tên muốn có là bỏ qua bước thử.
	Cert *CertbotCert

	ran bool // Apply của chính bước này đã thành công
}

// want: tên chính đầu, không lặp.
func (s *ACMEProbeNames) want() []validate.Domain {
	out := []validate.Domain{s.Layout.Domain}
	for _, n := range s.Names {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// CertbotNames là giá trị cho CertbotCert.Names: sau Apply là các tên đã qua,
// trước đó (bản xem trước, hay khi bước được bỏ qua) là mọi tên muốn có.
func (s *ACMEProbeNames) CertbotNames() []validate.Domain {
	if s.ran && s.Result != nil {
		return slices.Clone(s.Result.Passed)
	}
	return s.want()
}

func (s *ACMEProbeNames) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := sslCheckWebroot(s.Webroot); err != nil {
		return err
	}
	if s.Result == nil {
		return errors.New("bước thử nhiều tên thiếu chỗ ghi kết quả")
	}
	if n := len(s.want()); n > site.MaxNames {
		return fmt.Errorf("thử %d tên cho %s, quá trần %d", n, s.Layout.Domain, site.MaxNames)
	}
	if s.Cert != nil {
		if s.Cert.Layout != s.Layout || s.Cert.Webroot != s.Webroot {
			return errors.New("bước xin chứng chỉ đi kèm không cùng site hoặc cùng webroot với bước thử")
		}
		return s.Cert.validate()
	}
	return nil
}

func (s *ACMEProbeNames) Info() engine.StepInfo {
	d := s.Layout.Domain.String()
	var cmds []string
	for _, n := range s.want() {
		cmds = append(cmds, sslProbeCurl(n, "<tên-ngẫu-nhiên>").String())
	}
	return engine.StepInfo{
		ID:    "ssl.probe:" + d,
		Title: i18n.T("Thử đường HTTP mà Let's Encrypt sẽ kiểm cho từng tên của "+d, "Test the HTTP path Let's Encrypt will check for each name of "+d),
		Why: i18n.T(
			"Let's Encrypt chỉ cấp chứng chỉ khi gọi được http://<tên>/.well-known/acme-challenge/ tới đúng máy này cho MỌI tên trong đơn; một tên hỏng làm cả đơn hỏng. Thử trước từng tên bằng một file ngẫu nhiên: tên chính hỏng thì dừng; tên phụ hỏng thì bị bỏ khỏi đơn (nếu nó đang nằm trong chứng chỉ thì giữ chứng chỉ cũ, không xin lại).",
			"Let's Encrypt only issues a certificate when http://<name>/.well-known/acme-challenge/ reaches this machine for EVERY name in the order; one bad name fails the whole order. Each name is tested first with a random file: if the main name fails we stop; a failing extra name is dropped from the order (if it is already in the certificate, the current certificate is kept and nothing is requested).",
		),
		Commands: cmds,
		NoUndo: i18n.T(
			"Bước này không để lại gì trên máy: file thử luôn bị xoá ngay sau khi gọi.",
			"This step leaves nothing behind: the test files are always deleted right after the request.",
		),
	}
}

// Check: đã thử xong, hoặc chứng chỉ đã đúng máy chủ và đủ mọi tên muốn có
// (khi đó ghi Result như mọi tên đều qua). Không đặt ran: bản xem trước và lúc
// chạy dùng chung bước, lúc chạy phải xét lại.
func (s *ACMEProbeNames) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	if s.ran {
		return true, nil
	}
	if s.Cert == nil {
		return false, nil
	}
	// Bản sao: so với mọi tên muốn có, không theo cách core nối Names/Probe.
	c := *s.Cert
	want := s.want()
	c.Names = func() []validate.Domain { return want }
	c.Probe = nil
	ok, err := c.Check(ctx, sys)
	if ok {
		*s.Result = ProbeResult{Passed: want}
	}
	return ok, err
}

func (s *ACMEProbeNames) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	var r ProbeResult
	err := sslWithChallengeDir(sys, s.Webroot, func(dir string) error {
		for i, n := range s.want() {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("thử tên %s: %w", n, err)
			}
			perr := sslProbeOne(ctx, sys, dir, n)
			if perr == nil {
				r.Passed = append(r.Passed, n)
				continue
			}
			if i == 0 {
				return perr
			}
			covered := slices.Contains(s.Covered, n)
			r.Failed = append(r.Failed, ProbeFail{Name: n, Covered: covered, Reason: i18n.T(perr.Error(),
				"http://"+n.String()+"/.well-known/acme-challenge/ does not reach this machine (check the name's DNS record)")})
			r.Blocked = r.Blocked || covered
		}
		return nil
	})
	if err != nil {
		return err
	}
	*s.Result, s.ran = r, true
	return nil
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *ACMEProbeNames) Undo(context.Context, system.System) error { return nil }

// CertNames đọc các tên (SAN kiểu DNS) của chứng chỉ đầu tiên trong pem, như
// /etc/letsencrypt/live/<tên>/cert.pem. Tên không hợp lệ (kể cả tên đại diện
// "*.") là lỗi: TPAN không bao giờ xin chúng.
func CertNames(data []byte) ([]validate.Domain, error) {
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			return nil, errors.New("không có khối CERTIFICATE nào")
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return nil, fmt.Errorf("đọc chứng chỉ: %w", err)
		}
		out := make([]validate.Domain, 0, len(c.DNSNames))
		for _, n := range c.DNSNames {
			d, err := validate.ParseDomain(n)
			if err != nil {
				return nil, fmt.Errorf("tên %q trong chứng chỉ: %w", sslExcerpt(n), err)
			}
			out = append(out, d)
		}
		return out, nil
	}
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

// CertbotDelete xoá chứng chỉ khi xoá hẳn site (mục 6.4). M20: có CertName thì
// xoá lineage mang tên đó (chứng chỉ panel khi đổi hay bỏ tên miền của panel),
// Layout bỏ trống được.
type CertbotDelete struct {
	Layout   site.Layout
	CertName string
}

// validate: có CertName thì chỉ cần tên lineage đúng dạng; không thì Layout của site.
func (s *CertbotDelete) validate() error {
	if s.CertName != "" {
		return sslCheckCertName(s.CertName)
	}
	return webCheckLayout(s.Layout)
}

// name là tên lineage sẽ xoá.
func (s *CertbotDelete) name() string {
	if s.CertName != "" {
		return s.CertName
	}
	return s.Layout.Domain.String()
}

func (s *CertbotDelete) cmd() system.Cmd {
	return system.Cmd{Name: "certbot", Args: []string{"delete", "--cert-name", s.name(), "--non-interactive"}}
}

func (s *CertbotDelete) Info() engine.StepInfo {
	if n := s.CertName; n != "" {
		return engine.StepInfo{
			ID:    "ssl.delete:" + n,
			Title: i18n.T("Xoá chứng chỉ "+n+" đang có", "Delete the existing certificate "+n),
			Why: i18n.T(
				"Chứng chỉ "+n+" đang cấp cho tên miền khác, hoặc tên miền đó vừa bị bỏ. Xoá trước thì bước xin sau (nếu có) ra một chứng chỉ mới đúng tên, không phải sửa bộ tên của chứng chỉ cũ; và certbot thôi gia hạn một tên không còn dùng. Từ lúc xoá tới khi có chứng chỉ mới, panel dùng lại chứng chỉ tự ký (trình duyệt cảnh báo như lúc mới cài); bước xin sau đó mà hỏng thì panel ở lại chứng chỉ tự ký cho tới khi chạy lại thành công.",
				"The certificate "+n+" covers another domain, or that domain was just dropped. Deleting it first lets the next request (if any) produce a new certificate with the right name instead of editing the old one's names, and stops certbot renewing a name nobody uses. Between the delete and the new certificate the panel falls back to its self-signed certificate (browsers warn as on a fresh install); if the request then fails the panel stays on the self-signed certificate until a rerun succeeds.",
			),
			Commands: []string{s.cmd().String()},
			Danger:   true,
			NoUndo: i18n.T(
				"Chứng chỉ đã xoá không lấy lại được; cần thì xin lại.",
				"A deleted certificate cannot be restored; request a new one if needed.",
			),
		}
	}
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
	if err := s.validate(); err != nil {
		return false, err
	}
	paths := []string{s.Layout.CertRenewal(), s.Layout.CertDir()}
	if s.CertName != "" {
		paths = []string{sslRenewalConf(s.CertName), sslLiveDir(s.CertName)}
	}
	for _, p := range paths {
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
	if err := s.validate(); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("xoá chứng chỉ của %s: %w", s.name(), err), res)
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
func SiteVhostSSL(l site.Layout, web, phpVersion, acmeRoot string, mode site.HtaccessMode, n site.Names) ([]engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước vhost SSL: %w", err)
	}
	v, err := webVhostOf(l, web)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost SSL: %w", err)
	}
	conf, err := templates.SiteVhostNames(l, web, phpVersion, acmeRoot, true, mode, n)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost SSL %s: %w", web, err)
	}
	d := l.Domain.String()
	return []engine.Step{
		&File{Path: v.conf, Content: conf, Perm: 0o644, Owner: system.Root, Test: v.test, Reload: v.reload, Before: v.before,
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
