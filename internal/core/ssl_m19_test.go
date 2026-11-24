package core

// M19 (docs/thay-doi/M19-thiet-ke.md mục 3.6, S3, S14, S17): job site.ssl xin
// chứng chỉ cho mọi tên của site, ghi in_cert, vẽ lại vhost khi bộ tên phủ đổi;
// thao tác tên phụ trên site SSL chạy tiếp job site.ssl.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// certFor sinh chứng chỉ PEM mang đúng các tên (SAN), còn hạn 80 ngày.
func certFor(t *testing.T, names ...string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(80 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// sslNamesEnv: env tên phụ (store thật) cộng phần SSL. noDNS: tên mà bước thử
// ACME không tới được máy này.
type sslNamesEnv struct {
	*sslEnv
	noDNS map[string]bool
	// badProbe: tên mà NameProbe không bao giờ đạt (vhost không trả lời đúng).
	badProbe map[string]bool
}

func newSSLNamesEnv(t *testing.T) *sslNamesEnv {
	t.Helper()
	se := &sslNamesEnv{sslEnv: sslEnvOn(t, newNamesEnv(t)), noDNS: map[string]bool{}, badProbe: map[string]bool{}}
	se.ssl.Store = se.st
	se.setEmail(t)
	m := se.m
	m.Handle([]string{"curl", "-sS", "-m", "10"}, func(c system.Cmd) system.Result {
		u := c.Args[len(c.Args)-1]
		host, _, _ := strings.Cut(strings.TrimPrefix(u, "http://"), "/")
		_, name, _ := strings.Cut(u, "/.well-known/acme-challenge/")
		data, err := m.ReadFile(acmeRoot + "/.well-known/acme-challenge/" + name)
		if err != nil || se.noDNS[host] {
			return system.Result{ExitCode: 6, Stderr: "curl: (6) Could not resolve host: " + host}
		}
		return system.Result{Stdout: string(data)}
	})
	// certbot giả: chứng chỉ mang đúng mọi tên -d.
	m.Handle([]string{"certbot", "certonly"}, func(c system.Cmd) system.Result {
		var names []string
		for i, a := range c.Args {
			if a == "-d" && i+1 < len(c.Args) {
				names = append(names, c.Args[i+1])
			}
		}
		l := site.LayoutOf(mustDomain(t, names[0]))
		pemData := certFor(t, names...)
		m.SetFile(l.CertFullchain(), pemData, 0o644, system.Root)
		m.SetFile(l.CertDir()+"/cert.pem", pemData, 0o644, system.Root)
		m.SetFile(l.CertPrivkey(), []byte("khoá giả"), 0o600, system.Root)
		m.SetFile(l.CertRenewal(), []byte("version = 2.9.0\nserver = "+argAfter(c.Args, "--server")+"\n"), 0o644, system.Root)
		m.SetDir("/etc/letsencrypt/renewal-hooks/deploy", 0o755, system.Root)
		se.certbot = append(se.certbot, c)
		return system.Result{}
	})
	// Kiểm chứng của job site.ssl chạy tiếp (không bỏ Verify được như issueOK).
	m.Handle([]string{"systemctl", "is-active", "certbot.timer"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "active\n"}
	})
	m.Handle([]string{"systemctl", "is-enabled", "certbot.timer"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "enabled\n"}
	})
	m.Handle([]string{"certbot", "renew"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "Congratulations, all simulated renewals succeeded\n"}
	})
	// curl của HTTPSite/NameProbe: như handler của newNamesEnv, nhưng khối
	// chuyển tên trả về https://<tên chính>/ khi site đã có SSL trong DB.
	m.Handle([]string{"curl", "-sS", "--max-time", "15"}, func(c system.Cmd) system.Result {
		r := argAfter(c.Args, "--resolve")
		host, _, _ := strings.Cut(r, ":")
		if !strings.Contains(argAfter(c.Args, "-w"), "TPAN_LOCATION") {
			if !m.served(host) {
				if slices.Contains(c.Args, "-o") {
					return system.Result{Stdout: "404"}
				}
				return system.Result{Stdout: "<html>404</html>TPAN_HTTP_CODE=404"}
			}
			if slices.Contains(c.Args, "-o") {
				return system.Result{Stdout: "200"}
			}
			return system.Result{Stdout: "<html><link href='http://" + host + "/wp-content/x.css'></html>TPAN_HTTP_CODE=200"}
		}
		names, _ := m.ReadDir("/etc/nginx/sites-enabled")
		for _, n := range names {
			data, err := m.ReadFile("/etc/nginx/sites-enabled/" + n)
			if err != nil || se.badProbe[host] {
				continue
			}
			primary := ""
			for _, ln := range strings.Split(string(data), "\n") {
				f := strings.Fields(strings.TrimSuffix(strings.TrimSpace(ln), ";"))
				if len(f) < 2 || f[0] != "server_name" {
					continue
				}
				if primary == "" {
					primary = f[1]
				}
				if !slices.Contains(f[1:], host) {
					continue
				}
				if f[1] == primary {
					return system.Result{Stdout: "<link href='/wp-content/x.css'>\nTPAN_LOCATION=\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=200"}
				}
				scheme := "http"
				if st, ok, _ := se.st.Site(context.Background(), mustDomain(t, primary)); ok && st.SSL {
					scheme = "https"
				}
				return system.Result{Stdout: "\nTPAN_LOCATION=" + scheme + "://" + primary + "/\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=301"}
			}
		}
		return system.Result{ExitCode: 52, Stderr: "curl: (52) Empty reply from server"}
	})
	return se
}

// issue chạy job site.ssl qua đường của tpan ssl issue (bỏ Verify gọi HTTP như
// TestSSLExecuteAndRerun) và đòi thành công.
func (se *sslNamesEnv) issueOK(t *testing.T, dom string) *SSLIssue {
	t.Helper()
	iss := se.ssl.issue(t, mustDomain(t, dom))
	iss.Plan.Verify = nil
	rep, err := iss.Execute(context.Background(), nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("ssl issue %s: %v %s %s", dom, err, rep.Job.Status, rep.Job.Error)
	}
	return iss
}

// do bọc execNames để gọi thẳng với (op, err) của Prepare.
func (se *sslNamesEnv) do(t *testing.T) func(*NamesOp, error) NamesResult {
	return func(op *NamesOp, err error) NamesResult { t.Helper(); return execNames(t, op, err) }
}

func certbotNames(c system.Cmd) []string {
	var out []string
	for i, a := range c.Args {
		if a == "-d" {
			out = append(out, c.Args[i+1])
		}
	}
	return out
}

func inCert(t *testing.T, se *sslNamesEnv, dom string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for n, r := range rowsOf(t, se.siteEnv, dom) {
		out[n] = r.InCert
	}
	return out
}

func certSAN(t *testing.T, se *sslNamesEnv, dom string) []string {
	t.Helper()
	data, err := se.m.ReadFile(site.LayoutOf(mustDomain(t, dom)).CertFullchain())
	if err != nil {
		t.Fatal(err)
	}
	names, err := steps.CertNames(data)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n.String()
	}
	slices.Sort(out)
	return out
}

func notesText(ns []i18n.Text) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(n.Vi + "\n")
	}
	return b.String()
}

// Site không tên phụ: đúng các bước và argv trước M19; in_cert của tên chính
// được ghi (đường bật SSL lần đầu).
func TestSSLIssueNoAliasUnchanged(t *testing.T) {
	se := newSSLNamesEnv(t)
	const dom = "shop.example.com"
	se.addSite(t, dom, "", WWWNone)
	iss := se.ssl.issue(t, mustDomain(t, dom))
	var probe *steps.ACMEProbe
	for _, s := range iss.Plan.Steps {
		switch x := s.(type) {
		case *steps.ACMEProbeNames:
			t.Error("site không tên phụ mà Plan có ACMEProbeNames")
		case *steps.ACMEProbe:
			probe = x
		case *steps.CertbotCert:
			if x.Names != nil || x.Probe != nil {
				t.Errorf("CertbotCert.Names/Probe khác nil: %+v", x)
			}
		}
	}
	if probe == nil || iss.probe != nil {
		t.Fatalf("Plan thiếu ACMEProbe (probe=%v)", iss.probe)
	}
	iss.Plan.Verify = nil
	if rep, err := iss.Execute(context.Background(), nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	want := []string{"certonly", "--webroot", "-w", acmeRoot, "-d", dom, "--cert-name", dom,
		"--non-interactive", "--agree-tos", "-m", "admin@example.com", "--server", steps.LetsEncryptStaging}
	if len(se.certbot) != 1 || !reflect.DeepEqual(se.certbot[0].Args, want) {
		t.Errorf("certbot = %v", se.certbot)
	}
	if !inCert(t, se, dom)[dom] {
		t.Error("tên chính chưa có in_cert sau khi bật SSL")
	}
	if iss.FollowUpJob != "" || len(iss.Names) != 1 || !iss.Names[0].InCert || iss.Names[0].Reason != nil {
		t.Errorf("FollowUpJob=%q Names=%+v", iss.FollowUpJob, iss.Names)
	}
}

// Site có tên phụ: một chứng chỉ phủ mọi tên, in_cert đủ, vhost 443 vẽ lại có
// các tên phụ (job site.vhost chạy tiếp).
func TestSSLIssueAliasesSAN(t *testing.T) {
	se := newSSLNamesEnv(t)
	const dom = "example.com"
	se.addSite(t, dom, "", "") // www.example.com chuyển hướng
	ctx := context.Background()
	se.do(t)(se.s.PrepareAliasAdd(ctx, mustDomain(t, dom), mustDomain(t, "old.example.net"), site.ModeServe))
	iss := se.issueOK(t, dom)
	if len(se.certbot) != 1 {
		t.Fatalf("certbot = %v", se.certbot)
	}
	got := certbotNames(se.certbot[0])
	if got[0] != dom || len(got) != 3 || !slices.Contains(got, "www.example.com") || !slices.Contains(got, "old.example.net") {
		t.Errorf("-d = %v", got)
	}
	if ic := inCert(t, se, dom); !ic[dom] || !ic["www.example.com"] || !ic["old.example.net"] {
		t.Errorf("in_cert = %v", ic)
	}
	if iss.FollowUpJob == "" {
		t.Fatalf("bộ tên phủ đổi mà không chạy job vẽ lại vhost; notes %s", notesText(iss.Notes))
	}
	if len(iss.Notes) != 0 {
		t.Errorf("notes: %s", notesText(iss.Notes))
	}
	if !slices.Contains(iss.Site.Names.Covered, mustDomain(t, "old.example.net")) {
		t.Errorf("Site sau job: %+v", iss.Site.Names)
	}
	// Chạy lại: chứng chỉ đã đủ tên, không xin lại, không vẽ lại.
	iss = se.issueOK(t, dom)
	if len(se.certbot) != 1 || iss.FollowUpJob != "" {
		t.Errorf("chạy lại: certbot %d lần, FollowUpJob %q", len(se.certbot), iss.FollowUpJob)
	}
}

// B4 và S3: tên phụ chưa trỏ DNS không làm chứng chỉ đang có ngừng chạy.
func TestSSLIssueAliasNoDNSKeepsCert(t *testing.T) {
	se := newSSLNamesEnv(t)
	const dom = "example.com"
	se.addSite(t, dom, "", WWWNone)
	ctx := context.Background()
	d := mustDomain(t, dom)
	se.do(t)(se.s.PrepareAliasAdd(ctx, d, mustDomain(t, "www.example.com"), site.ModeRedirect))
	se.issueOK(t, dom)

	// Tên mới chưa trỏ DNS: bị bỏ khỏi đơn; bộ tên đã qua đúng bằng chứng chỉ
	// đang có nên certbot không chạy.
	se.noDNS["new.example.org"] = true
	se.do(t)(se.s.PrepareAliasAdd(ctx, d, mustDomain(t, "new.example.org"), site.ModeRedirect))
	iss := se.issueOK(t, dom)
	if len(se.certbot) != 1 {
		t.Errorf("certbot chạy lại: %v", se.certbot)
	}
	if ic := inCert(t, se, dom); ic["new.example.org"] || !ic["www.example.com"] {
		t.Errorf("in_cert = %v", ic)
	}
	var reason *i18n.Text
	for _, n := range iss.Names {
		if n.Name.String() == "new.example.org" {
			reason = n.Reason
		}
	}
	if reason == nil || !strings.Contains(reason.Vi, "new.example.org") {
		t.Errorf("Names = %+v", iss.Names)
	}

	// S3: tên đang trong chứng chỉ hỏng bước thử: giữ chứng chỉ cũ, có ghi chú.
	se.noDNS["www.example.com"] = true
	cur, _ := se.m.ReadFile(site.LayoutOf(d).CertFullchain())
	iss = se.issueOK(t, dom)
	after, _ := se.m.ReadFile(site.LayoutOf(d).CertFullchain())
	if len(se.certbot) != 1 || string(after) != string(cur) {
		t.Errorf("chứng chỉ bị đổi: certbot %v", se.certbot)
	}
	if !iss.probe.Blocked || !strings.Contains(notesText(iss.Notes), "giữ chứng chỉ cũ") {
		t.Errorf("Blocked=%v notes %s", iss.probe.Blocked, notesText(iss.Notes))
	}
	if ic := inCert(t, se, dom); !ic["www.example.com"] {
		t.Errorf("in_cert sau S3 = %v", ic)
	}
}

// Thêm tên phụ cho site SSL: job site.ssl chạy tiếp, chứng chỉ có tên mới.
func TestAliasAddRunsFollowUpSSL(t *testing.T) {
	se := newSSLNamesEnv(t)
	const dom = "example.com"
	se.addSite(t, dom, "", WWWNone)
	se.issueOK(t, dom)
	ctx := context.Background()
	res := se.do(t)(se.s.PrepareAliasAdd(ctx, mustDomain(t, dom), mustDomain(t, "old.example.net"), site.ModeRedirect))
	if res.Report.Job.Status != engine.JobSucceeded || !res.NeedsCert {
		t.Fatalf("job %s %s needsCert=%v", res.Report.Job.Status, res.Report.Job.Error, res.NeedsCert)
	}
	if res.FollowUpJob == "" || res.FollowUpJob == res.Report.Job.ID {
		t.Fatalf("FollowUpJob = %q; notes %s", res.FollowUpJob, notesText(res.Notes))
	}
	if len(se.certbot) != 2 || !slices.Contains(certbotNames(se.certbot[1]), "old.example.net") {
		t.Errorf("certbot = %v; notes %s", se.certbot, notesText(res.Notes))
	}
	if ic := inCert(t, se, dom); !ic["old.example.net"] {
		t.Errorf("in_cert = %v; notes %s", ic, notesText(res.Notes))
	}
}

// B9: gỡ tên phụ đang trong chứng chỉ thì chứng chỉ được xin lại không có nó.
func TestAliasRmShrinksCert(t *testing.T) {
	se := newSSLNamesEnv(t)
	const dom = "example.com"
	se.addSite(t, dom, "", "")
	se.issueOK(t, dom)
	if got := certSAN(t, se, dom); !slices.Contains(got, "www.example.com") {
		t.Fatalf("SAN trước = %v", got)
	}
	ctx := context.Background()
	res := se.do(t)(se.s.PrepareAliasRemove(ctx, mustDomain(t, dom), mustDomain(t, "www.example.com")))
	if res.Report.Job.Status != engine.JobSucceeded || res.FollowUpJob == "" {
		t.Fatalf("job %s %s FollowUpJob=%q notes %s", res.Report.Job.Status, res.Report.Job.Error, res.FollowUpJob, notesText(res.Notes))
	}
	if got := certSAN(t, se, dom); !reflect.DeepEqual(got, []string{dom}) {
		t.Errorf("SAN sau = %v; certbot %v", got, se.certbot)
	}
	if ic := inCert(t, se, dom); !ic[dom] || len(ic) != 1 {
		t.Errorf("in_cert = %v", ic)
	}
}

// S17: chủ site đã có đủ 5 job site.ssl trong 24 giờ thì không tự chạy tiếp.
func TestSSLIssueRateLimit(t *testing.T) {
	se := newSSLNamesEnv(t)
	const dom = "example.com"
	se.addSite(t, dom, "", WWWNone)
	se.issueOK(t, dom) // job 1 trong 24 giờ
	ctx := context.Background()
	owner := ownerOf(site.Site{})
	// Ba job trong 24 giờ và một job cũ hơn 24 giờ (không tính).
	for i, age := range []time.Duration{time.Hour, time.Hour, time.Hour, 25 * time.Hour} {
		j := engine.Job{ID: "ssl-cu-" + string(rune('a'+i)), Kind: "site.ssl", Target: dom, Owner: owner,
			Status: engine.JobSucceeded, Started: time.Now().Add(-age)}
		if err := se.st.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	// 4 < 5: job site.ssl còn tự chạy tiếp (thành job thứ 5).
	res := se.do(t)(se.s.PrepareAliasAdd(ctx, mustDomain(t, dom), mustDomain(t, "old.example.net"), site.ModeRedirect))
	if res.FollowUpJob == "" || len(se.certbot) != 2 {
		t.Fatalf("dưới trần mà không chạy tiếp: FollowUpJob=%q certbot=%d notes %s", res.FollowUpJob, len(se.certbot), notesText(res.Notes))
	}
	if n, err := se.st.CountJobs(ctx, "site.ssl", owner, time.Now().Add(-sslAutoWindow)); err != nil || n != sslAutoLimit {
		t.Fatalf("CountJobs 24 giờ = %d, %v", n, err)
	}
	// Đủ 5: thao tác tên vẫn thành công, job site.ssl không chạy, có ghi chú.
	res = se.do(t)(se.s.PrepareAliasAdd(ctx, mustDomain(t, dom), mustDomain(t, "new.example.net"), site.ModeRedirect))
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s %s", res.Report.Job.Status, res.Report.Job.Error)
	}
	if res.FollowUpJob != "" || len(se.certbot) != 2 || !strings.Contains(notesText(res.Notes), "ssl_rate_limited") {
		t.Errorf("FollowUpJob=%q certbot=%d notes %s", res.FollowUpJob, len(se.certbot), notesText(res.Notes))
	}
	if r := rowsOf(t, se.siteEnv, dom)["new.example.net"]; r.State != site.StateActive || r.InCert {
		t.Errorf("dòng new.example.net = %+v", r)
	}
}

// Job vẽ lại vhost hỏng: vhost về như trước job đó, chứng chỉ mới và in_cert giữ
// nguyên (khớp file), kết quả có ghi chú.
func TestSSLIssueRedrawFailKeepsCert(t *testing.T) {
	se := newSSLNamesEnv(t)
	const dom = "example.com"
	se.addSite(t, dom, "", WWWNone)
	ctx := context.Background()
	d, old := mustDomain(t, dom), mustDomain(t, "old.example.net")
	se.do(t)(se.s.PrepareAliasAdd(ctx, d, old, site.ModeRedirect))
	se.badProbe["old.example.net"] = true

	iss := se.ssl.issue(t, d)
	iss.Plan.Verify = nil
	var first, before string
	rep, err := iss.Execute(ctx, func(ev engine.Event) {
		if ev.Kind != engine.EventJobStart {
			return
		}
		if first == "" {
			first = ev.JobID
			return
		}
		b, _ := se.m.ReadFile(site.LayoutOf(d).NginxConf)
		before = string(b)
	})
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	if iss.FollowUpJob == "" || iss.FollowUpJob == first || before == "" {
		t.Fatalf("FollowUpJob=%q first=%q", iss.FollowUpJob, first)
	}
	if !strings.Contains(notesText(iss.Notes), iss.FollowUpJob) {
		t.Errorf("thiếu ghi chú job vẽ lại hỏng: %s", notesText(iss.Notes))
	}
	if got := vhostOf(t, se.siteEnv, dom); got != before {
		t.Errorf("vhost không về như cũ:\n%s\n---\n%s", got, before)
	}
	if got := certSAN(t, se, dom); !slices.Contains(got, "old.example.net") {
		t.Errorf("SAN = %v", got)
	}
	if ic := inCert(t, se, dom); !ic["old.example.net"] {
		t.Errorf("in_cert = %v", ic)
	}
}
