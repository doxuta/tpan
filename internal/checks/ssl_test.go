package checks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// Output mẫu của getent, ip, curl, certbot trong file này viết theo tài liệu của
// từng lệnh (man getent, ip-address, curl, certbot), chưa lấy từ máy ảo thật:
// phần này nằm trong mục "cần kiểm trên máy ảo".

// ---- DNSPointsHere ----

const (
	ipOut = "2: eth0    inet 203.0.113.7/24 brd 203.0.113.255 scope global eth0\\       valid_lft forever preferred_lft forever\n" +
		"2: eth0    inet6 2001:db8::7/64 scope global \\       valid_lft forever preferred_lft forever\n"
	// getent in mỗi địa chỉ ba lần (STREAM, DGRAM, RAW).
	a4Mine  = "203.0.113.7     STREAM shop.example.com\n203.0.113.7     DGRAM  \n203.0.113.7     RAW    \n"
	a4Other = "198.51.100.9    STREAM shop.example.com\n198.51.100.9    DGRAM  \n198.51.100.9    RAW    \n"
)

func dnsFake(a4 string, a4Exit int, a6 string, a6Exit int, ip string, ipExit int) *system.FakeSystem {
	f := system.NewFake()
	reply(f, []string{"getent", "ahostsv4"}, a4Exit, a4, "")
	reply(f, []string{"getent", "ahostsv6"}, a6Exit, a6, "")
	reply(f, []string{"ip"}, ipExit, ip, "")
	return f
}

func TestDNSPointsHere(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "dns.a:shop.example.com"
	cases := []struct {
		name    string
		f       *system.FakeSystem
		want    Status
		summary string
	}{
		{"A trỏ về máy, không có AAAA", dnsFake(a4Mine, 0, "", 2, ipOut, 0), Pass, "203.0.113.7"},
		{"A và AAAA đều của máy", dnsFake(a4Mine, 0, "2001:db8::7 STREAM shop.example.com\n", 0, ipOut, 0), Pass, "203.0.113.7"},
		{"getent ahostsv6 trả địa chỉ ánh xạ từ IPv4 thì không phải AAAA", dnsFake(a4Mine, 0, "::ffff:203.0.113.7 STREAM shop.example.com\n", 0, ipOut, 0), Pass, "203.0.113.7"},
		{"không có bản ghi A (getent mã 2)", dnsFake("", 2, "", 2, ipOut, 0), Fail, "chưa có bản ghi A"},
		{"A trỏ nơi khác", dnsFake(a4Other, 0, "", 2, ipOut, 0), Warn, "198.51.100.9"},
		{"một trong hai A là của máy", dnsFake(a4Other+a4Mine, 0, "", 2, ipOut, 0), Pass, "203.0.113.7"},
		{"AAAA trỏ nơi khác", dnsFake(a4Mine, 0, "2001:db8::99 STREAM shop.example.com\n", 0, ipOut, 0), Warn, "AAAA"},
		{"A trỏ nơi khác mà AAAA đúng vẫn Warn vì A", dnsFake(a4Other, 0, "2001:db8::7 STREAM x\n", 0, ipOut, 0), Warn, "198.51.100.9"},
		{"getent lỗi mã 1", dnsFake("", 1, "", 2, ipOut, 0), Unknown, "mã 1"},
		{"getent ahostsv6 lỗi mã 3", dnsFake(a4Mine, 0, "", 3, ipOut, 0), Unknown, "mã 3"},
		{"ip lỗi mà có A", dnsFake(a4Mine, 0, "", 2, "", 1), Unknown, "ip báo lỗi"},
		{"ip lỗi nhưng không có A vẫn là Fail", dnsFake("", 2, "", 2, "", 1), Fail, "chưa có bản ghi A"},
		{"output rác không làm panic", dnsFake("\n  \nkhông-phải-ip x\n", 0, "", 2, "inet\ninet x\n", 0), Fail, "chưa có bản ghi A"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := DNSPointsHere(l).Run(bg(), c.f)
			assertShape(t, r, id, StageDNS, c.want)
			assertEvidenceIsRan(t, r, c.f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.want == Fail || c.want == Warn {
				if !strings.HasPrefix(lastFix(r), "getent ahosts") {
					t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
				}
			}
		})
	}
}

func TestDNSPointsHereCommands(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := dnsFake(a4Mine, 0, "", 2, ipOut, 0)
	DNSPointsHere(l).Run(bg(), f)
	var got []string
	for _, c := range f.Ran() {
		got = append(got, c.String())
	}
	want := []string{"getent ahostsv4 shop.example.com", "getent ahostsv6 shop.example.com", "ip -o addr show scope global"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lệnh = %v, muốn %v", got, want)
	}
}

func TestDNSPointsHereCannotRun(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	r := DNSPointsHere(l).Run(cancelledCtx(), f)
	assertShape(t, r, "dns.a:shop.example.com", StageDNS, Unknown)
}

func TestSSLChecksRejectEmptyLayout(t *testing.T) {
	// Layout zero (quên ParseDomain): Unknown và không lệnh nào chạy.
	for name, c := range map[string]Check{
		"dns": DNSPointsHere(site.Layout{}), "tls": TLSCert(site.Layout{}), "redirect": HTTPRedirect(site.Layout{}),
		"https": HTTPSSite(site.Layout{}), "renew": CertRenewDryRun(site.Layout{}, ""),
	} {
		t.Run(name, func(t *testing.T) {
			f := system.NewFake()
			r := c.Run(bg(), f)
			if r.Status != Unknown {
				t.Errorf("Status = %q, muốn unknown", r.Status)
			}
			assertNoCmd(t, f)
		})
	}
}

// ---- TLSCert ----

var sslTestNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func fixNow(t *testing.T) {
	t.Helper()
	old := sslNow
	sslNow = func() time.Time { return sslTestNow }
	t.Cleanup(func() { sslNow = old })
}

// makeFullchain dựng fullchain.pem: chứng chỉ lá ký bởi một CA tên issuerCN,
// rồi chính CA đó (đúng thứ tự file của certbot: lá trước).
func makeFullchain(t *testing.T, names []string, issuerCN string, notBefore, notAfter time.Time) []byte {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: issuerCN},
		NotBefore: notBefore, NotAfter: notAfter.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cn := ""
	if len(names) > 0 {
		cn = names[0]
	}
	leafTpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: cn}, DNSNames: names,
		NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})...)
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...)
	return out
}

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

func TestTLSCert(t *testing.T) {
	fixNow(t)
	l := siteLayout(t, "shop.example.com")
	const id = "tls.cert:shop.example.com"
	names := []string{"shop.example.com"}
	cases := []struct {
		name    string
		pem     []byte
		want    Status
		summary string
	}{
		{"còn 60 ngày", makeFullchain(t, names, "R11", sslTestNow.Add(-days(30)), sslTestNow.Add(days(60))), Pass, "còn 60 ngày, do R11 cấp"},
		{"chứng chỉ staging", makeFullchain(t, names, "(STAGING) Pretend Pear X1", sslTestNow.Add(-days(1)), sslTestNow.Add(days(89))), Pass, "chứng chỉ thử"},
		{"đúng 14 ngày thì chưa cảnh báo", makeFullchain(t, names, "R11", sslTestNow.Add(-days(76)), sslTestNow.Add(days(14))), Pass, "còn 14 ngày"},
		{"còn 13 ngày", makeFullchain(t, names, "R11", sslTestNow.Add(-days(77)), sslTestNow.Add(days(13)+time.Hour)), Warn, "chỉ còn 13 ngày"},
		{"còn 2 ngày", makeFullchain(t, names, "R11", sslTestNow.Add(-days(88)), sslTestNow.Add(days(2))), Warn, "chỉ còn 2 ngày"},
		{"đã hết hạn", makeFullchain(t, names, "R11", sslTestNow.Add(-days(100)), sslTestNow.Add(-days(10))), Fail, "đã hết hạn từ 2026-11-09"},
		{"chưa có hiệu lực", makeFullchain(t, names, "R11", sslTestNow.Add(days(1)), sslTestNow.Add(days(90))), Fail, "chưa có hiệu lực"},
		{"cấp cho tên khác", makeFullchain(t, []string{"khac.example.net"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(90))), Fail, "khac.example.net"},
		{"tên miền là đuôi của tên được cấp", makeFullchain(t, []string{"myshop.example.com"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(90))), Fail, "không dành cho"},
		{"chứng chỉ wildcard", makeFullchain(t, []string{"*.example.com"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(90))), Pass, "còn 90 ngày"},
		{"nhiều tên, có tên của site", makeFullchain(t, []string{"www.shop.example.com", "shop.example.com"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(90))), Pass, "còn 90 ngày"},
		{"file rác", []byte("không phải PEM\n"), Fail, "không đọc được như một chứng chỉ"},
		{"khối PEM không phải chứng chỉ", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")}), Fail, "không đọc được như một chứng chỉ"},
		{"khối CERTIFICATE hỏng", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("hỏng")}), Fail, "không đọc được như một chứng chỉ"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			f.SetFile(l.CertFullchain(), c.pem, 0o644, system.Root)
			r := TLSCert(l).Run(bg(), f)
			assertShape(t, r, id, StageTLS, c.want)
			assertNoCmd(t, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if len(r.Evidence) != 1 || r.Evidence[0].Command != "read "+l.CertFullchain()+" (tpan)" {
				t.Errorf("Evidence = %+v", r.Evidence)
			}
			if c.want == Pass || c.want == Warn {
				out := r.Evidence[0].Output
				for _, w := range []string{"SAN:", "issuer:", "notAfter:"} {
					if !strings.Contains(out, w) {
						t.Errorf("Evidence.Output thiếu %q: %q", w, out)
					}
				}
			}
			if c.want != Pass && !strings.HasPrefix(lastFix(r), "certbot certificates --cert-name shop.example.com") {
				t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
			}
		})
	}
}

func TestTLSCertMissingFile(t *testing.T) {
	fixNow(t)
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	r := TLSCert(l).Run(bg(), f)
	assertShape(t, r, "tls.cert:shop.example.com", StageTLS, Fail)
	if !strings.Contains(r.Summary.Vi, "Chưa có chứng chỉ") || !strings.Contains(strings.Join(r.ManualFix, "\n"), "tpan ssl issue shop.example.com") {
		t.Errorf("Summary/ManualFix: %q / %q", r.Summary.Vi, r.ManualFix)
	}
	if r.Evidence[0].ExitCode != 1 {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
}

func TestTLSCertUnreadable(t *testing.T) {
	fixNow(t)
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	f.SetDir(l.CertFullchain(), 0o755, system.Root) // ReadFile trên thư mục lỗi, không phải "không tồn tại"
	r := TLSCert(l).Run(bg(), f)
	assertShape(t, r, "tls.cert:shop.example.com", StageTLS, Unknown)
	if r.Evidence[0].ExitCode != -1 {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
}

func TestTLSCertFollowsSymlink(t *testing.T) {
	// certbot để fullchain.pem trong live/ là symlink sang archive/.
	fixNow(t)
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	f.SetFile("/etc/letsencrypt/archive/shop.example.com/fullchain1.pem",
		makeFullchain(t, []string{"shop.example.com"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(89))), 0o644, system.Root)
	f.SetSymlink("../../archive/shop.example.com/fullchain1.pem", l.CertFullchain())
	assertShape(t, TLSCert(l).Run(bg(), f), "tls.cert:shop.example.com", StageTLS, Pass)
}

func TestTLSCertCleansIssuer(t *testing.T) {
	fixNow(t)
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	evil := "R11\n<script>x</script>" + strings.Repeat("Ồ", 200)
	f.SetFile(l.CertFullchain(), makeFullchain(t, []string{"shop.example.com"}, evil, sslTestNow.Add(-days(1)), sslTestNow.Add(days(89))), 0o644, system.Root)
	r := TLSCert(l).Run(bg(), f)
	assertShape(t, r, "tls.cert:shop.example.com", StageTLS, Pass)
	if strings.ContainsAny(r.Summary.Vi, "\n\r") || len(r.Summary.Vi) > 400 || strings.ContainsRune(r.Summary.Vi, '�') {
		t.Errorf("Summary không được sạch: %q", r.Summary.Vi)
	}
}

// ---- CertRenewDryRun ----

func TestCertRenewDryRun(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "tls.renew:shop.example.com"
	t.Run("đạt, không server", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"certbot"}, 0, "Congratulations, all simulated renewals succeeded:\n", "")
		r := CertRenewDryRun(l, "").Run(bg(), f)
		assertShape(t, r, id, StageTLS, Pass)
		assertEvidenceIsRan(t, r, f)
		got := f.Ran()[0].String()
		// --no-random-sleep-on-renew: không có terminal thì certbot tự chờ ngẫu
		// nhiên tới 8 phút trước khi gia hạn (kể cả --dry-run), giữ job và khoá
		// toàn cục của tpan suốt lúc đó (thấy trên máy ảo, docs/thay-doi/M5.md).
		want := "certbot renew --dry-run --no-random-sleep-on-renew --cert-name shop.example.com --non-interactive"
		if got != want {
			t.Errorf("lệnh = %q, muốn %q", got, want)
		}
	})
	t.Run("đạt, có server", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"certbot"}, 0, "", "")
		r := CertRenewDryRun(l, "https://acme.test:14000/dir").Run(bg(), f)
		assertShape(t, r, id, StageTLS, Pass)
		got := strings.Join(f.Ran()[0].Args, " ")
		if !strings.HasSuffix(got, "--non-interactive --server https://acme.test:14000/dir") {
			t.Errorf("Args = %q", got)
		}
	})
	t.Run("thất bại", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"certbot"}, 1, "", "Failed to renew certificate shop.example.com with error: Some challenges have failed.\n")
		r := CertRenewDryRun(l, "").Run(bg(), f)
		assertShape(t, r, id, StageTLS, Fail)
		assertEvidenceIsRan(t, r, f)
		if !strings.Contains(r.Summary.Vi, "Failed to renew") || lastFix(r) != f.Ran()[0].String() {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("không chạy được", func(t *testing.T) {
		assertShape(t, CertRenewDryRun(l, "").Run(cancelledCtx(), system.NewFake()), id, StageTLS, Unknown)
	})
	for _, bad := range []string{"--webroot", "-x", "ftp://acme.test/dir", "https://", "javascript:alert(1)", "https://a b/dir", "https://a\nb/dir", "acme.test/dir"} {
		t.Run("server không hợp lệ "+bad, func(t *testing.T) {
			f := system.NewFake()
			r := CertRenewDryRun(l, bad).Run(bg(), f)
			assertShape(t, r, id, StageTLS, Unknown)
			assertNoCmd(t, f)
		})
	}
}

// ---- HTTPRedirect ----

func redirOut(code, location string) string {
	h := "HTTP/1.1 " + code + " Moved Permanently\r\nServer: nginx\r\n"
	if location != "" {
		h += "Location: " + location + "\r\n"
	}
	return h + "\r\n" + "TPAN_HTTP_CODE=" + code
}

func TestHTTPRedirect(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "http.redirect:shop.example.com"
	recheckPrefix := "curl -sS --max-time 15 --resolve shop.example.com:80:127.0.0.1 -o /dev/null -D -"
	cases := []struct {
		name    string
		exit    int
		stdout  string
		stderr  string
		want    Status
		summary string
	}{
		{"301 sang https đúng", 0, redirOut("301", "https://shop.example.com/"), "", Pass, "301"},
		{"301 kèm đường dẫn", 0, redirOut("301", "https://shop.example.com/blog/"), "", Pass, "301"},
		{"tên header viết thường (HTTP/2)", 0, "HTTP/2 301 \r\nlocation: https://shop.example.com/\r\n\r\nTPAN_HTTP_CODE=301", "", Pass, "301"},
		{"301 sang http", 0, redirOut("301", "http://shop.example.com/"), "", Fail, "không phải https://shop.example.com/"},
		{"301 sang tên khác", 0, redirOut("301", "https://evil.example.net/"), "", Fail, "evil.example.net"},
		{"301 sang tên dài hơn", 0, redirOut("301", "https://shop.example.com.evil.net/"), "", Fail, "không phải"},
		{"301 không có Location", 0, redirOut("301", ""), "", Fail, "không phải"},
		{"302", 0, redirOut("302", "https://shop.example.com/"), "", Fail, "cần 301"},
		{"307", 0, redirOut("307", "https://shop.example.com/"), "", Fail, "cần 301"},
		{"200 không chuyển hướng", 0, "HTTP/1.1 200 OK\r\n\r\nTPAN_HTTP_CODE=200", "", Fail, "200"},
		{"không kết nối được", 7, "TPAN_HTTP_CODE=000", "curl: (7) Failed to connect", Fail, "cổng 80"},
		{"quá thời gian", 28, "TPAN_HTTP_CODE=000", "curl: (28) Operation timed out", Fail, "15 giây"},
		{"đóng kết nối không trả gì", 52, "TPAN_HTTP_CODE=000", "curl: (52) Empty reply from server", Fail, "đóng kết nối"},
		{"lỗi curl khác", 3, "", "curl: (3) URL using bad/illegal format", Unknown, "mã 3"},
		{"exit 0 mà không có mã", 0, "HTTP/1.1", "", Unknown, "Chưa kiểm được"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			reply(f, []string{"curl"}, c.exit, c.stdout, c.stderr)
			r := HTTPRedirect(l).Run(bg(), f)
			assertShape(t, r, id, StageWeb, c.want)
			assertEvidenceIsRan(t, r, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.want == Fail && (!strings.HasPrefix(lastFix(r), recheckPrefix) || !strings.HasSuffix(lastFix(r), "http://shop.example.com/")) {
				t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
			}
		})
	}
}

func TestHTTPRedirectCommand(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, redirOut("301", "https://shop.example.com/"), "")
	HTTPRedirect(l).Run(bg(), f)
	args := strings.Join(f.Ran()[0].Args, " ")
	for _, want := range []string{"--resolve shop.example.com:80:127.0.0.1", "-D -", "-o /dev/null", "http://shop.example.com/"} {
		if !strings.Contains(args, want) {
			t.Errorf("Args %q thiếu %q", args, want)
		}
	}
	if strings.Contains(args, " -L") || strings.Contains(args, " -k") {
		t.Errorf("không được theo chuyển hướng hay bỏ qua chứng chỉ: %q", args)
	}
}

// ---- HTTPSSite ----

func TestHTTPSSiteCommand(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut(wpPage, "200"), "")
	r := HTTPSSite(l).Run(bg(), f)
	assertShape(t, r, "https.site:shop.example.com", StageEndToEnd, Pass)
	assertEvidenceIsRan(t, r, f)
	args := strings.Join(f.Ran()[0].Args, " ")
	for _, want := range []string{"-sS", "-k", "--max-time 15", "--resolve shop.example.com:443:127.0.0.1", "https://shop.example.com/"} {
		if !strings.Contains(args, want) {
			t.Errorf("Args %q thiếu %q", args, want)
		}
	}
	if strings.Contains(args, " -L") {
		t.Errorf("không được theo chuyển hướng: %q", args)
	}
	if !strings.HasPrefix(r.Evidence[0].Output, "HTTP 200,") {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
}

func TestHTTPSSiteCases(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "https.site:shop.example.com"
	recheckPrefix := "curl -sS -k --max-time 15 --resolve shop.example.com:443:127.0.0.1 -o /dev/null"
	cases := []struct {
		name     string
		exit     int
		stdout   string
		stderr   string
		want     Status
		summary  string
		fixLines []string
	}{
		{"200 và là WordPress của site", 0, httpOut(wpPage, "200"), "", Pass, "200", nil},
		{"domain đứng cuối câu", 0, httpOut(`wp-content Chào mừng tới shop.example.com.`, "200"), "", Pass, "200", nil},
		{"200 nhưng không phải WordPress", 0, httpOut("<html>Welcome to nginx! shop.example.com</html>", "200"), "", Fail, "không phải WordPress", nil},
		{"tên domain nằm trong tên dài hơn (myshop)", 0, httpOut(`<link href="http://myshop.example.com/wp-content/x.css">`, "200"), "", Fail, "shop.example.com", nil},
		{"domain là đuôi của tên khác", 0, httpOut(`wp-content http://shop.example.com.evil.net/`, "200"), "", Fail, "shop.example.com", nil},
		{"502", 0, httpOut("Bad Gateway", "502"), "", Fail, "502", []string{"ls -l /run/php"}},
		{"504", 0, httpOut("", "504"), "", Fail, "504", nil},
		{"500", 0, httpOut("Error establishing a database connection", "500"), "", Fail, "500", []string{"tail -n 20 " + l.PHPErrorLog}},
		{"301 (vòng lặp chuyển hướng)", 0, httpOut("", "301"), "", Fail, "301", []string{"wp option update home https://shop.example.com"}},
		{"404", 0, httpOut("not found", "404"), "", Fail, "404", nil},
		{"không kết nối được cổng 443", 7, httpOut("", "000"), "curl: (7) Failed to connect", Fail, "cổng 443", []string{"ss -tlnp"}},
		{"quá thời gian", 28, httpOut("", "000"), "curl: (28) Operation timed out", Fail, "15 giây", nil},
		{"đóng kết nối không trả gì", 52, httpOut("", "000"), "curl: (52) Empty reply from server", Fail, "đóng kết nối", []string{"tpan site add shop.example.com"}},
		{"bắt tay TLS hỏng", 35, httpOut("", "000"), "curl: (35) error:0A000126:SSL routines::unexpected eof", Fail, "Bắt tay TLS", []string{"tpan ssl issue shop.example.com"}},
		{"lỗi curl khác", 3, "", "curl: (3) URL using bad/illegal format", Unknown, "mã 3", nil},
		{"exit 0 mà không có mã", 0, "<html>", "", Unknown, "Chưa kiểm được", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			reply(f, []string{"curl"}, c.exit, c.stdout, c.stderr)
			r := HTTPSSite(l).Run(bg(), f)
			assertShape(t, r, id, StageEndToEnd, c.want)
			assertEvidenceIsRan(t, r, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.want == Fail {
				if !strings.HasPrefix(lastFix(r), recheckPrefix) || !strings.HasSuffix(lastFix(r), "https://shop.example.com/") {
					t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
				}
				for _, w := range c.fixLines {
					if !strings.Contains(strings.Join(r.ManualFix, "\n"), w) {
						t.Errorf("ManualFix thiếu %q: %q", w, r.ManualFix)
					}
				}
			}
		})
	}
}

// ---- SiteChecksSSL ----

func TestSiteChecksSSLOrder(t *testing.T) {
	fixNow(t)
	l := siteLayout(t, "shop.example.com")
	want := []struct {
		id    string
		stage Stage
	}{
		{"tls.cert:shop.example.com", StageTLS},
		{"web.vhost:shop.example.com", StageWeb},
		{"php.socket:shop.example.com", StagePHP},
		{"wp.checksums:shop.example.com", StageWordPress},
		{"wp.config:shop.example.com", StageWordPress},
		{"db.access:shop.example.com", StageDB},
		{"http.redirect:shop.example.com", StageWeb},
		{"https.site:shop.example.com", StageEndToEnd},
	}
	cs := SiteChecksSSL("nginx", "8.3", l)
	if len(cs) != len(want) {
		t.Fatalf("có %d phép kiểm, muốn %d", len(cs), len(want))
	}
	for i, r := range RunAll(bg(), system.NewFake(), cs) {
		// HTTPSite (cổng 80, đòi 200) không được có mặt: site có SSL thì cổng 80 trả 301.
		if strings.HasPrefix(r.ID, "http.site:") {
			t.Errorf("không được có %s", r.ID)
		}
		if r.ID != want[i].id || r.Stage != want[i].stage {
			t.Errorf("phép kiểm %d: %s/%s, muốn %s/%s", i, r.ID, r.Stage, want[i].id, want[i].stage)
		}
		assertText(t, "Title "+r.ID, r.Title)
		if r.Status == Fail && len(r.ManualFix) == 0 {
			t.Errorf("%s: Fail mà không có ManualFix", r.ID)
		}
	}
}

// Mọi phép kiểm SSL chỉ đọc: không đổi hệ thống file, chỉ chạy lệnh trong danh sách.
func TestSiteChecksSSLOnlyRead(t *testing.T) {
	fixNow(t)
	l := siteLayout(t, "shop.example.com")
	f := vhostFixture(l, "nginx")
	f.SetFile(l.CertFullchain(), makeFullchain(t, []string{"shop.example.com"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(89))), 0o644, system.Root)
	f.SetFile(l.WPConfig, []byte("<?php"), 0o644, system.Root)
	reply(f, []string{"wp"}, 1, "", "Error: x")
	reply(f, []string{"curl"}, 0, redirOut("301", "https://shop.example.com/"), "")
	before := f.Snapshot()
	RunAll(bg(), f, append(SiteChecksSSL("nginx", "8.3", l), DNSPointsHere(l), CertRenewDryRun(l, "")))
	for _, c := range f.Ran() {
		switch c.Name {
		case "wp", "curl", "php-fpm8.3", "test", "getent", "ip", "certbot":
		default:
			t.Errorf("lệnh không mong đợi: %s", c)
		}
	}
	after := f.Snapshot()
	if len(before) != len(after) {
		t.Fatalf("hệ thống file đổi: %d -> %d mục", len(before), len(after))
	}
	for p, e := range before {
		if after[p] != e {
			t.Errorf("%s đổi: %+v -> %+v", p, e, after[p])
		}
	}
}
