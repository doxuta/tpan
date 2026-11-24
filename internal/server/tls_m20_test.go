package server

// Test của 421 cho tên site và chứng chỉ panel theo SNI (M20, mục 11.2, B9),
// và quét bí mật trong câu trả lời API (B3).

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// TestHostLaSite421 (mục 11.2 ý 2, R1, R2, R7, R12).
func TestHostLaSite421(t *testing.T) {
	e := newM20Env(t)
	ctx := context.Background()
	for _, s := range []site.Site{
		{Domain: mustDomain(t, "shop.example.com"), Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: m20User},
		{Domain: mustDomain(t, "boss.example.com"), Status: site.Trashed, PHPVersion: "8.3", Created: e.now, Owner: account.AdminName},
	} {
		e.must(e.st.SaveSite(ctx, s))
	}
	// Soát cuối F5: trang 421 trả lời người lạ, không được in tên panel.
	const panelName = "quan-tri.example.org"
	e.must(e.st.SetSetting(ctx, core.SettingPanelDomain, panelName))
	noLeak := func(what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		for _, s := range []string{"shop", "boss", panelName} {
			if strings.Contains(strings.ToLower(rec.Body.String()), s) {
				t.Errorf("%s: trang 421 lộ %q: %s", what, s, rec.Body)
			}
		}
	}
	c := e.session(m20User)
	rec := e.req(http.MethodGet, "/api/v1/sites", "", m20IP, []*http.Cookie{c}, "Host", "SHOP.example.com.:8443")
	if rec.Code != http.StatusMisdirectedRequest || errCode(t, rec) != "host_is_site" {
		t.Fatalf("tên site của user: %d %s", rec.Code, rec.Body)
	}
	noLeak("JSON", rec)
	if gone := cookieNamed(rec, authCookieTLS); gone == nil || gone.MaxAge >= 0 {
		t.Fatalf("421 phải xoá cookie phiên: %+v", gone)
	}
	if _, err := e.st.SessionByToken(ctx, authTokenHash(c.Value)); err == nil {
		t.Fatal("421 phải xoá phiên đã lộ dưới tên site")
	}
	// Site của admin, ở thùng rác: vẫn 421 (R2); đường dẫn giao diện ra HTML.
	rec = e.req(http.MethodGet, "/", "", m20IP, nil, "Host", "boss.example.com")
	if rec.Code != http.StatusMisdirectedRequest || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("tên site của admin: %d %s", rec.Code, rec.Header())
	}
	noLeak("HTML", rec)
	// Vào bằng IP, bằng tên panel: bình thường.
	if rec := e.req(http.MethodGet, "/api/v1/health", "", m20IP, nil, "Host", "192.0.2.10:8443"); rec.Code != http.StatusOK {
		t.Fatalf("vào bằng IP: %d", rec.Code)
	}
	if rec := e.req(http.MethodGet, "/api/v1/health", "", m20IP, nil); rec.Code != http.StatusOK {
		t.Fatalf("vào bằng tên panel: %d", rec.Code)
	}
	// Có Authorization: miễn 421 (không dùng cookie).
	raw, _ := e.token(m20User, "sites:read", time.Hour)
	if rec := e.req(http.MethodGet, "/api/v1/sites", "", m20IP, nil, "Host", "shop.example.com", "Authorization", "Bearer "+raw); rec.Code != http.StatusOK {
		t.Fatalf("token ở tên site: %d %s", rec.Code, rec.Body)
	}
	// Site mới thấy sau khi đệm 5 giây hết hạn.
	e.must(e.st.SaveSite(ctx, site.Site{Domain: mustDomain(t, "new.example.com"), Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: m20User}))
	e.now = e.now.Add(siteNamesTTL)
	if rec := e.req(http.MethodGet, "/api/v1/health", "", m20IP, nil, "Host", "new.example.com"); rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("site mới sau đệm: %d", rec.Code)
	}
}

func mustDomain(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// makeCert ghi chứng chỉ tự ký (ECDSA) cho names vào dir, trả serial.
func makeCert(t *testing.T, dir, certName, keyName string, names []string, notAfter time.Time, serial int64) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names,
		NotBefore: authT0.Add(-time.Hour), NotAfter: notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, typ string, b []byte) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}), 0o600); err != nil {
			t.Fatal(err)
		}
		// mtime khác hẳn lần ghi trước (vài hệ thống file làm tròn mtime).
		mt := time.Unix(1_700_000_000+serial*10, 0)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	write(certName, "CERTIFICATE", der)
	write(keyName, "PRIVATE KEY", kb)
}

// TestChungChiTheoSNI (B9, mục 11.2 ý 5, 6).
func TestChungChiTheoSNI(t *testing.T) {
	e := newM20Env(t)
	ctx := context.Background()
	self, acme := t.TempDir(), t.TempDir()
	makeCert(t, self, "cert.pem", "key.pem", []string{"tpan"}, authT0.AddDate(2, 0, 0), 1)
	makeCert(t, acme, "fullchain.pem", "privkey.pem", []string{m20Host}, authT0.AddDate(0, 3, 0), 2)
	pc := e.s.newPanelCerts(filepath.Join(self, "cert.pem"), filepath.Join(self, "key.pem"), acme)
	serial := func(sni string) int64 {
		t.Helper()
		c, err := pc.GetCertificate(&tls.ClientHelloInfo{ServerName: sni})
		if err != nil {
			t.Fatal(err)
		}
		return c.Leaf.SerialNumber.Int64()
	}
	// Chưa đặt panel.domain: mọi SNI ra tự ký, kể cả tên có chứng chỉ trên đĩa.
	for _, sni := range []string{"", m20Host, "other.example.com"} {
		if got := serial(sni); got != 1 {
			t.Errorf("chưa đặt tên, SNI %q: serial %d, muốn tự ký", sni, got)
		}
	}
	e.must(e.st.SetSetting(ctx, core.SettingPanelDomain, m20Host))
	e.s.panelDom.invalidate()
	for sni, want := range map[string]int64{m20Host: 2, "PANEL.example.com.": 2, "other.example.com": 1, "": 1} {
		if got := serial(sni); got != want {
			t.Errorf("đặt tên, SNI %q: serial %d, muốn %d", sni, got, want)
		}
	}
	// Setting là tên khác: chứng chỉ trên đĩa không được phục vụ.
	e.must(e.st.SetSetting(ctx, core.SettingPanelDomain, "other.example.com"))
	e.s.panelDom.invalidate()
	if serial(m20Host) != 1 || serial("other.example.com") != 1 {
		t.Error("setting khác tên (hay SAN không có tên) phải ra tự ký")
	}
	e.must(e.st.SetSetting(ctx, core.SettingPanelDomain, m20Host))
	e.now = e.now.Add(panelDomainTTL) // hết đệm 2 giây, không cần xoá đệm
	if serial(m20Host) != 2 {
		t.Fatal("setting đổi phải có hiệu lực sau 2 giây")
	}
	// Gia hạn: file đổi thì nạp lại, không cần restart.
	makeCert(t, acme, "fullchain.pem", "privkey.pem", []string{m20Host}, authT0.AddDate(0, 3, 0), 3)
	if serial(m20Host) != 3 {
		t.Fatal("chứng chỉ gia hạn phải được nạp lại")
	}
	// Nạp hỏng: giữ bản đang dùng.
	if err := os.WriteFile(filepath.Join(acme, "fullchain.pem"), []byte("hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	if serial(m20Host) != 3 {
		t.Fatal("file hỏng phải giữ bản đang dùng")
	}
	// Hết hạn: về tự ký.
	makeCert(t, acme, "fullchain.pem", "privkey.pem", []string{m20Host}, authT0.AddDate(0, 3, 0), 4)
	e.now = authT0.AddDate(0, 4, 0)
	if serial(m20Host) != 1 {
		t.Fatal("chứng chỉ hết hạn phải về tự ký")
	}
	// File mất (bỏ tên, xin lại hỏng sau khi xoá): về tự ký.
	e.now = authT0.Add(time.Minute)
	e.s.panelDom.invalidate()
	_ = os.Remove(filepath.Join(acme, "fullchain.pem"))
	if serial(m20Host) != 1 {
		t.Fatal("lineage mất file phải về tự ký")
	}
}

// TestAPIKhongTraLaiBiMat (B3): bí mật TOTP, mã dự phòng, token chỉ xuất hiện
// ở đúng câu trả lời lần đầu của một luồng đầy đủ.
func TestAPIKhongTraLaiBiMat(t *testing.T) {
	e := newM20Env(t)
	c := e.session(account.AdminName)
	var bodies []string // theo thứ tự
	keep := func(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		bodies = append(bodies, rec.Body.String())
		return rec
	}
	get := func(p string) { keep(e.req(http.MethodGet, p, "", m20IP, []*http.Cookie{c})) }

	rec := keep(e.post("/api/v1/account/2fa/setup", struct{}{}, c))
	iSetup := len(bodies) - 1
	var setup MFASetup
	e.must(json.Unmarshal(rec.Body.Bytes(), &setup))
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	e.must(err)
	get("/api/v1/account/2fa")
	rec = keep(e.post("/api/v1/account/2fa/enable", MFAConfirm{Password: authTestPassword, Code: e.code(secret)}, c))
	iEnable := len(bodies) - 1
	var codes RecoveryCodes
	e.must(json.Unmarshal(rec.Body.Bytes(), &codes))
	get("/api/v1/account/2fa")
	e.now = e.now.Add(30 * time.Second)
	rec = keep(e.post("/api/v1/account/tokens", TokenCreate{Name: "ci", Scopes: []string{"sites:read"}, Days: 1, Password: authTestPassword, Code: e.code(secret)}, c))
	iToken := len(bodies) - 1
	var tok TokenCreated
	e.must(json.Unmarshal(rec.Body.Bytes(), &tok))
	get("/api/v1/account/tokens")
	get("/api/v1/tokens")
	get("/api/v1/session")
	get("/api/v1/settings/auth")
	keep(e.req(http.MethodGet, "/api/v1/sites", "", m20IP, nil, bearer(tok.Token)...))
	// Đăng nhập lại bằng mật khẩu và mã dự phòng.
	rec = keep(e.loginAs("", m20IP))
	keep(e.step2(codes.Codes[1], m20IP, cookieNamed(rec, mfaCookieTLS)))

	if tok.Token == "" || setup.Secret == "" || len(codes.Codes) == 0 {
		t.Fatal("luồng không lấy được bí mật")
	}
	check := func(what, s string, only int) {
		for i, b := range bodies {
			if strings.Contains(b, s) != (i == only) {
				t.Errorf("%s ở câu trả lời %d (chỉ được ở %d): %s", what, i, only, b)
			}
		}
	}
	check("bí mật TOTP", setup.Secret, iSetup)
	check("token", tok.Token, iToken)
	for _, cd := range codes.Codes {
		check("mã dự phòng", cd, iEnable)
	}
}

// TestFileKhoaMatHayHong (mục 11.6, R4): đã có bí mật mà file khoá mất hay
// sai quyền thì 503 mfa_secret_unavailable, không sinh khoá mới; mã dự phòng
// vẫn dùng được.
func TestFileKhoaMatHayHong(t *testing.T) {
	e := newM20Env(t)
	secret, codes := e.enable(account.AdminName)
	p := e.s.mfaKeyPath()
	info, err := e.f.Stat(p)
	if err != nil || info.Mode.Perm() != 0o600 || info.Size != 32 {
		t.Fatalf("file khoá: %+v, %v", info, err)
	}
	e.must(e.f.Chmod(p, 0o644))
	if rec := e.step2(e.code(secret), m20IP, e.challenge(account.AdminName, m20IP)); errCode(t, rec) != "mfa_secret_unavailable" {
		t.Fatalf("khoá sai quyền: %d %s", rec.Code, rec.Body)
	}
	e.must(e.f.Remove(p))
	if rec := e.step2(e.code(secret), m20IP, e.challenge(account.AdminName, m20IP)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("khoá mất: %d %s", rec.Code, rec.Body)
	}
	if rec := e.post("/api/v1/account/2fa/setup", struct{}{}, e.session(m20User)); errCode(t, rec) != "mfa_secret_unavailable" {
		t.Fatalf("setup khi có bí mật mà mất khoá: %d %s", rec.Code, rec.Body)
	}
	if _, err := e.f.Stat(p); err == nil {
		t.Fatal("không được sinh khoá mới khi đã có bí mật")
	}
	if rec := e.step2(codes[0], m20IP, e.challenge(account.AdminName, m20IP)); rec.Code != http.StatusOK {
		t.Fatalf("mã dự phòng khi mất khoá: %d %s", rec.Code, rec.Body)
	}
	// Không ai có dòng hai bước: setup sinh khoá mới.
	e.must(e.st.DisableTOTP(context.Background(), account.AdminName))
	if rec := e.post("/api/v1/account/2fa/setup", struct{}{}, e.session(m20User)); rec.Code != http.StatusOK {
		t.Fatalf("setup đầu tiên: %d %s", rec.Code, rec.Body)
	}
}

// TestPanelRoute: GET /panel và bỏ tên khi chưa đặt (qua khuôn planJob).
func TestPanelRoute(t *testing.T) {
	e := newM20Env(t)
	c := e.session(account.AdminName)
	rec := e.req(http.MethodGet, "/api/v1/panel", "", m20IP, []*http.Cookie{c})
	var st PanelState
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != http.StatusOK || st.Domain != "" || st.Cert != nil || st.SelfSigned == nil {
		t.Fatalf("GET /panel: %d %s", rec.Code, rec.Body)
	}
	rec = e.req(http.MethodPost, "/api/v1/panel/domain?dry_run=1", `{"domain":""}`, m20IP, []*http.Cookie{c})
	if rec.Code != http.StatusConflict || errCode(t, rec) != "panel_domain_not_set" {
		t.Fatalf("bỏ tên khi chưa đặt: %d %s", rec.Code, rec.Body)
	}
	rec = e.req(http.MethodPost, "/api/v1/panel/domain?dry_run=1", `{"domain":"a.com; rm -rf /"}`, m20IP, []*http.Cookie{c})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tên miền tấn công: %d %s", rec.Code, rec.Body)
	}
}
