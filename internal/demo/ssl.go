package demo

// Phần SSL của máy giả (mục 6.3 của docs/KE-HOACH.md): DNS trỏ về máy, certbot
// cấp chứng chỉ thật về mặt cấu trúc (crypto/x509, tự ký) để phép kiểm TLSCert
// đọc được, đường thử ACME, và một site mẫu đã có SSL.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

const (
	// acmeRoot là webroot của certbot (core dùng cùng giá trị, không xuất).
	acmeRoot = "/var/lib/tpan/acme"
	// demoIP là địa chỉ của máy giả (TEST-NET-3, RFC 5737). Mọi domain trỏ về
	// đây, để xin SSL cho site tạo trên giao diện demo qua được kiểm DNS.
	demoIP = "203.0.113.10"
	// certLife là hạn của chứng chỉ Let's Encrypt.
	certLife = 90 * 24 * time.Hour
	// sslAge: chứng chỉ của site mẫu cấp cách đây 30 ngày, còn khoảng 60 ngày.
	sslAge = 30 * 24 * time.Hour
)

// sslFiles đặt các thư mục mà gói certbot tạo sẵn khi cài.
func (m *machine) sslFiles() {
	for _, d := range []string{"/etc/letsencrypt/live", "/etc/letsencrypt/renewal", "/etc/letsencrypt/renewal-hooks/deploy"} {
		m.SetDir(d, 0o755, rootOwner)
	}
}

func (m *machine) sslHandlers() {
	// getent: mọi domain chỉ có bản ghi A trỏ về máy; không có AAAA (mã 2:
	// không có bản ghi). In mỗi địa chỉ ba lần như getent thật.
	ahosts := func(c system.Cmd) system.Result {
		d := c.Args[len(c.Args)-1]
		return exit(0, demoIP+"    STREAM "+d+"\n"+demoIP+"    DGRAM  \n"+demoIP+"    RAW    \n", "")
	}
	m.Handle([]string{"getent", "ahostsv4"}, ahosts)
	m.Handle([]string{"getent", "ahosts"}, ahosts)
	m.Handle([]string{"getent", "ahostsv6"}, func(system.Cmd) system.Result { return exit(2, "", "") })
	m.Handle([]string{"ip"}, func(system.Cmd) system.Result {
		return exit(0, "2: eth0    inet "+demoIP+"/24 brd 203.0.113.255 scope global eth0\\       valid_lft forever preferred_lft forever\n", "")
	})
	m.Handle([]string{"certbot"}, m.certbot)
}

// certbot giả lập ba lệnh tpan dùng: certonly (cấp, hoặc giữ chứng chỉ cũ khi
// chưa tới hạn như certbot thật), renew --dry-run (luôn đạt), delete.
func (m *machine) certbot(c system.Cmd) system.Result {
	if len(c.Args) == 0 {
		return exit(2, "", "certbot: thiếu lệnh\n")
	}
	l, ok := siteOf(argAfter(c.Args, "--cert-name"))
	switch c.Args[0] {
	case "certonly":
		if !ok {
			return exit(1, "", "certbot: --cert-name không hợp lệ\n")
		}
		server := argAfter(c.Args, "--server")
		if old, err := m.ReadFile(l.CertRenewal()); err == nil && !slices.Contains(c.Args, "--force-renewal") &&
			strings.Contains(string(old), "server = "+server+"\n") {
			return exit(0, "Certificate not yet due for renewal; no action taken.\n", "")
		}
		if err := m.issueCert(l, server, time.Now()); err != nil {
			return exit(1, "", err.Error()+"\n")
		}
		return exit(0, "Successfully received certificate.\nCertificate is saved at: "+l.CertFullchain()+"\n", "")
	case "renew":
		return exit(0, "Congratulations, all simulated renewals succeeded:\n", "")
	case "delete":
		if !ok {
			return exit(1, "", "certbot: --cert-name không hợp lệ\n")
		}
		m.removeUnder(l.CertDir(), true)
		_ = m.Remove(l.CertRenewal())
		return exit(0, "Deleted all files relating to certificate "+l.Domain.String()+".\n", "")
	}
	return okResult
}

// issueCert ghi bộ file certbot để lại cho site l: chứng chỉ (fullchain.pem),
// khoá riêng, và file renewal có dòng server. Chứng chỉ có hiệu lực từ
// notBefore, trong 90 ngày, do "tổ chức cấp" mang tên giống Let's Encrypt
// (staging có chữ "(STAGING)" như thật). Khoá chỉ nằm trong bộ nhớ máy giả.
func (m *machine) issueCert(l site.Layout, server string, notBefore time.Time) error {
	d := l.Domain.String()
	issuer := "E6"
	if strings.Contains(server, "staging") {
		issuer = "(STAGING) Ersatz Edamame E1"
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("sinh khoá cho chứng chỉ giả của %s: %w", d, err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return fmt.Errorf("sinh số serial cho %s: %w", d, err)
	}
	leaf := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: d},
		DNSNames:     []string{d},
		NotBefore:    notBefore,
		NotAfter:     notBefore.Add(certLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	// parent chỉ để đặt tên tổ chức cấp; chứng chỉ ký bằng chính khoá của nó.
	parent := &x509.Certificate{Subject: pkix.Name{Country: []string{"US"}, Organization: []string{"Let's Encrypt"}, CommonName: issuer}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, parent, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("tạo chứng chỉ giả cho %s: %w", d, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("mã hoá khoá của %s: %w", d, err)
	}

	m.SetDir(l.CertDir(), 0o755, rootOwner)
	m.SetFile(l.CertFullchain(), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644, rootOwner)
	m.SetFile(l.CertPrivkey(), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600, rootOwner)
	live := l.CertDir()
	renewal := "# renew_before_expiry = 30 days\nversion = 2.9.0\narchive_dir = /etc/letsencrypt/archive/" + d +
		"\ncert = " + live + "/cert.pem\nprivkey = " + live + "/privkey.pem\nchain = " + live + "/chain.pem\nfullchain = " + live + "/fullchain.pem\n" +
		"\n# Options used in the renewal process\n[renewalparams]\nauthenticator = webroot\nwebroot_path = " + acmeRoot + ",\nserver = " + server + "\nkey_type = ecdsa\n" +
		"[[webroot_map]]\n" + d + " = " + acmeRoot + "\n"
	m.SetFile(l.CertRenewal(), []byte(renewal), 0o644, rootOwner)
	return nil
}

// acmeProbe trả lời curl của steps.ACMEProbe: http://<domain>/.well-known/
// acme-challenge/<tên>. Site mặc định của tpan phục vụ đường này cho mọi
// domain từ acmeRoot, nên trả nội dung file nếu có, không thì trang 404 (curl
// không có -f nên vẫn thoát mã 0). ok=false: không phải lệnh này.
func (m *machine) acmeProbe(args []string) (system.Result, bool) {
	const mark = "/.well-known/acme-challenge/"
	for _, a := range args {
		if !strings.HasPrefix(a, "http://") || !strings.Contains(a, mark) {
			continue
		}
		name := a[strings.LastIndex(a, "/")+1:]
		data, err := m.ReadFile(acmeRoot + mark + name)
		if err != nil || name == "" {
			return exit(0, "<html><head><title>404 Not Found</title></head><body>404 Not Found</body></html>", ""), true
		}
		return exit(0, string(data), ""), true
	}
	return system.Result{}, false
}

// vhostSSL cho biết vhost của site đã là bản có SSL (có khối nghe 443).
func (m *machine) vhostSSL(l site.Layout) bool {
	conf, err := m.ReadFile(l.NginxConf)
	return err == nil && strings.Contains(string(conf), "listen 443")
}

// seedSSL đưa site dom về trạng thái như sau một Plan SSL thành công (mục 6.3):
// email trong settings, chứng chỉ staging cấp cách đây sslAge, site mặc định
// 443, vhost bản SSL, URL WordPress là https, hook gia hạn, và site.SSL = true.
//
// Ghi thẳng trạng thái chứ không chạy core.SSL như addOK chạy core.Sites: Plan
// SSL cấp chứng chỉ vào lúc chạy, mà site mẫu cần chứng chỉ đã cũ 30 ngày để
// màn SSL có số ngày còn lại trông như thật. Vì vậy lịch sử không có job
// site.ssl cho site này.
func seedSSL(ctx context.Context, m *machine, st *store.Store, dom string) error {
	if err := st.SetSetting(ctx, core.SettingSSLEmail, demoEmail); err != nil {
		return fmt.Errorf("ghi setting %s: %w", core.SettingSSLEmail, err)
	}
	l, err := layoutOf(dom)
	if err != nil {
		return err
	}
	if err := m.issueCert(l, steps.LetsEncryptStaging, time.Now().Add(-sslAge)); err != nil {
		return err
	}

	def, err := templates.NginxDefaultSSL()
	if err != nil {
		return fmt.Errorf("sinh site mặc định 443: %w", err)
	}
	m.SetFile("/etc/nginx/sites-available/tpan-default-ssl.conf", def, 0o644, rootOwner)
	m.SetSymlink("/etc/nginx/sites-available/tpan-default-ssl.conf", "/etc/nginx/sites-enabled/tpan-default-ssl.conf")
	vhost, err := templates.SiteVhost(l, string(core.Nginx), phpVer, acmeRoot, true)
	if err != nil {
		return fmt.Errorf("sinh vhost SSL của %s: %w", dom, err)
	}
	m.SetFile(l.NginxConf, vhost, 0o644, rootOwner)
	hook, err := templates.RenewHook("/usr/local/bin/tpan")
	if err != nil {
		return fmt.Errorf("sinh hook gia hạn: %w", err)
	}
	m.SetFile(core.RenewHookPath, hook, 0o755, rootOwner)
	// Ghi mã băm như engine ghi sau một job thật: thiếu nó thì phép kiểm lệch
	// của M4 coi ba file này là file bị sửa tay ngay khi mở demo.
	for path, data := range map[string][]byte{
		"/etc/nginx/sites-available/tpan-default-ssl.conf": def,
		l.NginxConf:        vhost,
		core.RenewHookPath: hook,
	} {
		sum := sha256.Sum256(data)
		if err := st.RecordFile(ctx, path, hex.EncodeToString(sum[:]), "demo-ssl"); err != nil {
			return fmt.Errorf("ghi mã băm %s: %w", path, err)
		}
	}

	m.mu.Lock()
	m.setOption(l.User, "home", "https://"+dom)
	m.setOption(l.User, "siteurl", "https://"+dom)
	m.mu.Unlock()

	s, ok, err := st.Site(ctx, l.Domain)
	if err != nil {
		return fmt.Errorf("đọc site %s để bật SSL: %w", dom, err)
	}
	if !ok {
		return fmt.Errorf("bật SSL cho site mẫu %s: site không có trong bảng sites", dom)
	}
	s.SSL = true
	if err := st.SaveSite(ctx, s); err != nil {
		return fmt.Errorf("ghi SSL của site %s: %w", dom, err)
	}
	return nil
}
