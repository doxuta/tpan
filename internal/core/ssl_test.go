package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// selfSigned sinh một chứng chỉ PEM cho domain, còn hạn 80 ngày, để
// checks.TLSCert đọc được như chứng chỉ thật.
func selfSigned(t *testing.T, domain string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		Issuer:       pkix.Name{CommonName: "(STAGING) giả"},
		DNSNames:     []string{domain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(80 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// sslEnv là siteEnv cộng giả lập phần SSL: DNS (getent), địa chỉ máy (ip),
// curl của bước thử ACME (đọc lại file thử trong webroot), certbot, và hai
// tuỳ chọn home/siteurl của WordPress.
type sslEnv struct {
	*siteEnv
	ssl *SSL
	// a4 là output của `getent ahostsv4`; rỗng thì getent mã 2 (không có bản ghi).
	a4      string
	wpURL   map[string]string // user|tuỳ chọn → giá trị
	certbot []system.Cmd
}

const sslMachineIP = "203.0.113.10"

func newSSLEnv(t *testing.T, web WebServer) *sslEnv {
	t.Helper()
	return sslEnvOn(t, newSiteEnv(t, web))
}

// sslEnvOn thêm phần giả lập SSL vào một siteEnv có sẵn (M19: env tên phụ).
func sslEnvOn(t *testing.T, base *siteEnv) *sslEnv {
	t.Helper()
	se := &sslEnv{siteEnv: base, a4: sslMachineIP + " STREAM x\n", wpURL: map[string]string{}}
	m := se.m
	se.ssl = &SSL{Sys: m, Settings: se.st, Store: se.sites, Journal: se.st}
	m.SetDir(acmeRoot, 0o755, system.Root)
	ok := system.Result{}

	m.Handle([]string{"getent", "ahostsv4"}, func(system.Cmd) system.Result {
		if se.a4 == "" {
			return system.Result{ExitCode: 2}
		}
		return system.Result{Stdout: se.a4}
	})
	m.Handle([]string{"getent", "ahostsv6"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	m.Handle([]string{"ip", "-o", "addr"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "2: eth0    inet " + sslMachineIP + "/24 brd 203.0.113.255 scope global eth0\n"}
	})
	// Bước thử ACME: http://<d>/.well-known/acme-challenge/<tên> → file trong webroot.
	m.Handle([]string{"curl", "-sS", "-m", "10"}, func(c system.Cmd) system.Result {
		u := c.Args[len(c.Args)-1]
		_, name, _ := strings.Cut(u, "/.well-known/acme-challenge/")
		data, err := m.ReadFile(acmeRoot + "/.well-known/acme-challenge/" + name)
		if err != nil {
			return system.Result{Stdout: "<html>404</html>"}
		}
		return system.Result{Stdout: string(data)}
	})
	m.Handle([]string{"certbot", "certonly"}, func(c system.Cmd) system.Result {
		d := argAfter(c.Args, "-d")
		l := site.LayoutOf(mustDomain(t, d))
		m.SetFile(l.CertFullchain(), selfSigned(t, d), 0o644, system.Root)
		m.SetFile(l.CertPrivkey(), []byte("khoá giả"), 0o600, system.Root)
		m.SetFile(l.CertRenewal(), []byte("version = 2.9.0\nserver = "+argAfter(c.Args, "--server")+"\n"), 0o644, system.Root)
		// certbot tự tạo thư mục hook ở lần chạy đầu.
		m.SetDir("/etc/letsencrypt/renewal-hooks/deploy", 0o755, system.Root)
		se.certbot = append(se.certbot, c)
		return ok
	})
	m.Handle([]string{"wp", "option"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		u, pub := c.AsUser, argPrefix(c.Args, "--path=")
		opt := c.Args[2]
		switch {
		case opt == "permalink_structure": // của WPInstall, như siteSim
			return system.Result{Stdout: m.permalink[u] + "\n"}
		case c.Args[1] == "update":
			se.wpURL[u+"|"+opt] = c.Args[3]
			return ok
		}
		v, found := se.wpURL[u+"|"+opt]
		if !found {
			dom := strings.TrimSuffix(strings.TrimPrefix(pub, "/var/www/"), "/public")
			v = "http://" + dom
		}
		return system.Result{Stdout: v + "\n"}
	})
	return se
}

func (se *sslEnv) setEmail(t *testing.T) {
	t.Helper()
	mustSet(t, se.st, SettingSSLEmail, "admin@example.com")
}

func TestSSLConfig(t *testing.T) {
	se := newSSLEnv(t, Nginx)
	s := se.ssl

	cfg, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Chưa ghi gì: staging BẬT (mục 6.3).
	if !cfg.Staging || cfg.Email != "" || cfg.Server != "" || cfg.ServerURL() != steps.LetsEncryptStaging {
		t.Errorf("mặc định = %+v, %s", cfg, cfg.ServerURL())
	}

	em := mustEmail(t)
	off := false
	cfg, err = s.SetConfig(ctx, SSLSettingsInput{Email: &em, Staging: &off})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Email != "admin@example.com" || cfg.Staging || cfg.ServerURL() != steps.LetsEncryptProd {
		t.Errorf("sau khi ghi = %+v, %s", cfg, cfg.ServerURL())
	}

	srv := "https://127.0.0.1:14000/dir"
	if cfg, err = s.SetConfig(ctx, SSLSettingsInput{Server: &srv}); err != nil || cfg.ServerURL() != srv {
		t.Errorf("server tuỳ chỉnh = %+v, %v", cfg, err)
	}
	empty := ""
	if cfg, err = s.SetConfig(ctx, SSLSettingsInput{Server: &empty}); err != nil || cfg.Server != "" || cfg.ServerURL() != steps.LetsEncryptProd {
		t.Errorf("xoá server = %+v, %v", cfg, err)
	}

	// Server sai: từ chối, và không trường nào khác bị ghi dở.
	on := true
	for _, bad := range []string{
		"http://example.com/dir", "https://example.com/dir?x=1", "https://exa mple.com",
		"--server", "https://example.com/$(id)", "https://example.com\n", "ftp://example.com",
	} {
		_, err := s.SetConfig(ctx, SSLSettingsInput{Staging: &on, Server: &bad})
		if refusedCode(t, err) != "ssl_server_invalid" {
			t.Errorf("%q: muốn ssl_server_invalid", bad)
		}
	}
	if cfg, _ := s.Config(ctx); cfg.Staging {
		t.Error("server sai mà staging vẫn bị ghi")
	}

	mustSet(t, se.st, SettingSSLStaging, "có")
	if _, err := s.Config(ctx); err == nil {
		t.Error("ssl.staging lạ mà không lỗi")
	}
}

func TestSSLPrepareRefusals(t *testing.T) {
	const dom = "shop.example.com"
	d := func(t *testing.T) validate.Domain { return mustDomain(t, dom) }

	t.Run("site không có", func(t *testing.T) {
		se := newSSLEnv(t, Nginx)
		se.setEmail(t)
		_, err := se.ssl.PrepareIssue(ctx, d(t))
		if refusedCode(t, err) != "site_not_found" {
			t.Error("muốn site_not_found")
		}
	})
	t.Run("thùng rác", func(t *testing.T) {
		se := newSSLEnv(t, Nginx)
		se.setEmail(t)
		_ = se.sites.SaveSite(ctx, site.Site{Domain: d(t), Status: site.Trashed, PHPVersion: "8.3"})
		_, err := se.ssl.PrepareIssue(ctx, d(t))
		if refusedCode(t, err) != "site_trashed" {
			t.Error("muốn site_trashed")
		}
	})
	t.Run("stack chưa cài", func(t *testing.T) {
		se := newSSLEnv(t, "")
		se.setEmail(t)
		_ = se.sites.SaveSite(ctx, site.Site{Domain: d(t), Status: site.Active, PHPVersion: "8.3"})
		_, err := se.ssl.PrepareIssue(ctx, d(t))
		if refusedCode(t, err) != "stack_not_installed" {
			t.Error("muốn stack_not_installed")
		}
	})
	t.Run("chưa có email", func(t *testing.T) {
		se := newSSLEnv(t, Nginx)
		se.add(t, dom)
		_, err := se.ssl.PrepareIssue(ctx, d(t))
		if refusedCode(t, err) != "acme_email_missing" {
			t.Error("muốn acme_email_missing")
		}
	})
	t.Run("không có bản ghi A", func(t *testing.T) {
		se := newSSLEnv(t, Nginx)
		se.setEmail(t)
		se.add(t, dom)
		se.a4 = ""
		before := len(se.m.Ran())
		_, err := se.ssl.PrepareIssue(ctx, d(t))
		var re *RefusedError
		if refusedCode(t, err) != "precondition_failed" || !errors.As(err, &re) || len(re.Checks) != 1 || re.Checks[0].ID != "dns.a:"+dom {
			t.Errorf("muốn precondition_failed kèm dns.a: %v", err)
		}
		for _, c := range se.m.Ran()[before:] {
			if c.Name != "getent" && c.Name != "ip" {
				t.Errorf("điều kiện đầu chạy lệnh %s", c.String())
			}
		}
	})
	t.Run("A trỏ chỗ khác chỉ cảnh báo", func(t *testing.T) {
		se := newSSLEnv(t, Nginx)
		se.setEmail(t)
		se.add(t, dom)
		se.a4 = "198.51.100.7 STREAM x\n"
		iss, err := se.ssl.PrepareIssue(ctx, d(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(iss.Preconditions) != 1 || iss.Preconditions[0].Status != checks.Warn {
			t.Errorf("Preconditions = %+v", iss.Preconditions)
		}
	})
	t.Run("ssl.server hỏng trong DB", func(t *testing.T) {
		se := newSSLEnv(t, Nginx)
		se.setEmail(t)
		se.add(t, dom)
		mustSet(t, se.st, SettingSSLServer, "http://x")
		_, err := se.ssl.PrepareIssue(ctx, d(t))
		if refusedCode(t, err) != "ssl_server_invalid" {
			t.Error("muốn ssl_server_invalid")
		}
	})
}

func TestSSLPlanOrder(t *testing.T) {
	const dom = "shop.example.com"
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			se := newSSLEnv(t, web)
			se.setEmail(t)
			se.add(t, dom)
			iss, err := se.ssl.PrepareIssue(ctx, mustDomain(t, dom))
			if err != nil {
				t.Fatal(err)
			}
			l := site.LayoutOf(mustDomain(t, dom))
			conf, link := l.NginxConf, l.NginxLink
			if web == Apache {
				conf, link = l.ApacheConf, l.ApacheLink
			}
			def, _ := steps.DefaultSSL(string(web))
			want := []string{"dir:" + acmeRoot + "/.well-known/acme-challenge", "ssl.probe:" + dom, "ssl.cert:" + dom}
			want = append(want, stepIDs(engine.Plan{Steps: def})...)
			want = append(want, "file:"+conf, "symlink:"+link, "wp.siteurl:"+dom, "file:"+RenewHookPath)
			if got := stepIDs(iss.Plan); !reflect.DeepEqual(got, want) {
				t.Errorf("thứ tự bước:\n got %v\nwant %v", got, want)
			}
			if iss.Plan.Kind != "site.ssl" || iss.Plan.Target != dom {
				t.Errorf("Plan = %s/%s", iss.Plan.Kind, iss.Plan.Target)
			}
			// Bước thử và bước xin dùng chung một CertbotCert, server staging.
			probe := iss.Plan.Steps[1].(*steps.ACMEProbe)
			cert := iss.Plan.Steps[2].(*steps.CertbotCert)
			if probe.Cert != cert || cert.Server != steps.LetsEncryptStaging || cert.Webroot != acmeRoot {
				t.Errorf("probe.Cert=%p cert=%p server=%s", probe.Cert, cert, cert.Server)
			}
			for _, s := range iss.Plan.Steps {
				if u, ok := s.(*steps.WPSiteURL); ok && u.URL != "https://"+dom {
					t.Errorf("WPSiteURL = %s", u.URL)
				}
				if f, ok := s.(*steps.File); ok && f.Path == RenewHookPath && (f.Perm != 0o755 || !strings.Contains(string(f.Content), "/usr/local/bin/tpan internal reload-web")) {
					t.Errorf("hook = %o %q", f.Perm, f.Content)
				}
			}
			// Kiểm chứng: bộ SSL của site, certbot.timer, gia hạn thử.
			var ids []string
			for _, r := range checks.RunAll(ctx, se.m, iss.Plan.Verify) {
				ids = append(ids, r.ID)
			}
			for _, w := range []string{"tls.cert:" + dom, "http.redirect:" + dom, "https.site:" + dom, "service.active:certbot.timer", "tls.renew:" + dom} {
				if !slices.Contains(ids, w) {
					t.Errorf("kiểm chứng thiếu %s: %v", w, ids)
				}
			}
			if slices.Contains(ids, "http.site:"+dom) {
				t.Error("kiểm chứng còn http.site (cổng 80 giờ trả 301)")
			}
		})
	}
}

func TestSSLPreviewChangesNothing(t *testing.T) {
	const dom = "shop.example.com"
	se := newSSLEnv(t, Nginx)
	se.setEmail(t)
	se.add(t, dom)
	before := se.m.Snapshot()
	n := len(se.m.Ran())
	iss, err := se.ssl.PrepareIssue(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(se.m.Snapshot(), before) {
		t.Error("xem trước đổi hệ thống file")
	}
	for _, c := range se.m.Ran()[n:] {
		if c.Name == "certbot" || c.Name == "curl" || (c.Name == "wp" && slices.Contains(c.Args, "update")) {
			t.Errorf("xem trước chạy %s", c.String())
		}
	}
}

func TestSSLExecuteAndRerun(t *testing.T) {
	const dom = "shop.example.com"
	se := newSSLEnv(t, Nginx)
	se.setEmail(t)
	se.add(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)

	if _, err := (&SSL{Sys: se.m, Settings: se.st, Store: se.sites}).issue(t, d).Execute(ctx, nil); err == nil {
		t.Error("Execute không có Journal mà không lỗi")
	}

	iss := se.ssl.issue(t, d)
	// Kiểm chứng gọi HTTP thật; TestSSLPlanOrder đã xem danh sách của nó.
	iss.Plan.Verify = nil
	rep, err := iss.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s %s; bước %+v", err, rep.Job.Status, rep.Job.Error, rep.Steps)
	}
	st, _, _ := se.sites.Site(ctx, d)
	if !st.SSL {
		t.Error("job thành công mà site chưa ghi SSL")
	}
	if len(se.certbot) != 1 || argAfter(se.certbot[0].Args, "-m") != "admin@example.com" {
		t.Errorf("certbot = %v", se.certbot)
	}
	if v := se.wpURL[l.User+"|home"]; v != "https://"+dom {
		t.Errorf("home = %q", v)
	}
	conf, err := se.m.ReadFile(l.NginxConf)
	if err != nil || !strings.Contains(string(conf), "443") {
		t.Errorf("vhost chưa có 443: %v", err)
	}
	// File thử của bước thử ACME đã bị xoá.
	if names, _ := se.m.ReadDir(acmeRoot + "/.well-known/acme-challenge"); len(names) != 0 {
		t.Errorf("còn file thử: %v", names)
	}

	// Chạy lại: mọi bước đã đúng, certbot không bị gọi lại.
	pv, err := se.ssl.issue(t, d).Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range pv.Steps {
		if !s.Done {
			t.Errorf("chạy lại mà bước %s chưa đúng (%s)", s.Info.ID, s.CheckError)
		}
	}

	// Site có SSL: chạy lại site add giữ vhost 443 và URL https; health dùng bộ SSL.
	a, err := se.s.PrepareAdd(ctx, se.input(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	pv, err = a.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range pv.Steps {
		if !s.Done && s.Info.ID != "wp.install:"+dom {
			t.Errorf("chạy lại site add với site SSL mà bước %s sẽ chạy", s.Info.ID)
		}
	}
	for _, s := range a.Plan.Steps {
		if w, ok := s.(*wpInstallStep); ok && w.URL != "https://"+dom {
			t.Errorf("WPInstall URL = %s", w.URL)
		}
	}
	h, err := se.s.Health(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range h.Results {
		ids[r.ID] = true
	}
	if !ids["tls.cert:"+dom] || ids["http.site:"+dom] {
		t.Errorf("health của site SSL: %v", ids)
	}

	// List: site có SSL kèm phép kiểm chứng chỉ, site chưa có thì không.
	se.add(t, "blog.example.com")
	rows, err := se.ssl.List(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("List = %+v, %v", rows, err)
	}
	if rows[0].Domain.String() != "blog.example.com" || rows[0].SSL || rows[0].Cert != nil {
		t.Errorf("dòng 1 = %+v", rows[0])
	}
	if !rows[1].SSL || rows[1].Cert == nil || rows[1].Cert.Status != checks.Pass {
		t.Errorf("dòng 2 = %+v", rows[1])
	}

	// M6: site rm đưa vào thùng rác, chứng chỉ giữ nguyên để khôi phục; certbot
	// delete chỉ nằm ở xoá hẳn (bước cuối, xem TestSitePurge trong trash_test.go).
	rm, err := se.s.PrepareRemove(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(stepIDs(rm.Plan), "ssl.delete:"+dom) {
		t.Errorf("Plan thùng rác không được xoá chứng chỉ: %v", stepIDs(rm.Plan))
	}
}

func TestSSLListSkipsTrashed(t *testing.T) {
	se := newSSLEnv(t, Nginx)
	_ = se.sites.SaveSite(ctx, site.Site{Domain: mustDomain(t, "b.example.com"), Status: site.Trashed, SSL: true})
	_ = se.sites.SaveSite(ctx, site.Site{Domain: mustDomain(t, "a.example.com"), Status: site.Active, SSL: true})
	rows, err := se.ssl.List(ctx)
	if err != nil || len(rows) != 1 || rows[0].Cert == nil || rows[0].Cert.Status != checks.Fail {
		t.Errorf("List = %+v, %v", rows, err)
	}
}

// issue gọi PrepareIssue và đòi thành công.
func (s *SSL) issue(t *testing.T, d validate.Domain) *SSLIssue {
	t.Helper()
	iss, err := s.PrepareIssue(ctx, d)
	if err != nil {
		t.Fatalf("PrepareIssue %s: %v", d, err)
	}
	return iss
}

func TestReloadWeb(t *testing.T) {
	run := func(t *testing.T, web WebServer, fail bool) ([]string, error) {
		se := newSiteEnv(t, web)
		if fail {
			se.m.Handle([]string{"apachectl"}, func(system.Cmd) system.Result {
				return system.Result{ExitCode: 1, Stderr: "Syntax error on line 3\n"}
			})
		}
		err := ReloadWeb(ctx, se.m, se.st)
		var cmds []string
		for _, c := range se.m.Ran() {
			cmds = append(cmds, c.String())
		}
		return cmds, err
	}
	if cmds, err := run(t, "", false); err != nil || len(cmds) != 0 {
		t.Errorf("stack chưa cài: %v, %v", cmds, err)
	}
	if cmds, err := run(t, Nginx, false); err != nil || !reflect.DeepEqual(cmds, []string{"nginx -t", "systemctl reload nginx"}) {
		t.Errorf("nginx: %v, %v", cmds, err)
	}
	if cmds, err := run(t, Apache, false); err != nil || !reflect.DeepEqual(cmds, []string{"apachectl configtest", "systemctl reload apache2"}) {
		t.Errorf("apache: %v, %v", cmds, err)
	}
	cmds, err := run(t, Apache, true)
	if err == nil || !strings.Contains(err.Error(), "Syntax error") || len(cmds) != 1 {
		t.Errorf("config sai: %v, %v", cmds, err)
	}
}
