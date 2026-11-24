package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// b1Cert sinh một chứng chỉ tự ký (PEM) có SAN là names, như tpan setup.
func b1Cert(t *testing.T, names ...string) []byte {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     names,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func wantReserved(t *testing.T, err error, what string) {
	t.Helper()
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != CodeDomainReserved {
		t.Errorf("%s: %v; muốn từ chối %s", what, err, CodeDomainReserved)
	}
}

// TestPrepareAddTenCuaPanelB1: tên trong chứng chỉ của panel và tên đã ghi từ
// lần admin đăng nhập là tên dành riêng, cho mọi chủ; tên khác vẫn qua; không
// có chứng chỉ thì không đổi gì; site đã có thì chạy lại vẫn được.
func TestPrepareAddTenCuaPanelB1(t *testing.T) {
	ctx := context.Background()
	se := newSiteEnv(t, Nginx)

	// Chưa có chứng chỉ (máy demo, máy giả): tên nào cũng qua.
	if _, err := se.s.PrepareAdd(ctx, se.input(t, "panel.example.net")); err != nil {
		t.Fatalf("không có cert: %v; muốn qua", err)
	}
	// Tạo thật trước khi có cert, để thử chạy lại site đã có ở dưới.
	se.add(t, "old.example.net")

	se.m.SetFile(panelCertPath, b1Cert(t, "Panel.Example.NET", "localhost", "old.example.net"), 0o644, system.Root)
	for _, owner := range []string{"", "admin", "khach"} {
		in := se.input(t, "panel.example.net")
		in.Owner = owner
		_, err := se.s.PrepareAdd(ctx, in)
		wantReserved(t, err, "panel.example.net chủ "+owner)
	}
	if _, err := se.s.PrepareAdd(ctx, se.input(t, "shop.example.net")); err != nil {
		t.Errorf("shop.example.net: %v; muốn qua", err)
	}
	if _, err := se.s.PrepareAdd(ctx, se.input(t, "old.example.net")); err != nil {
		t.Errorf("chạy lại site đã có old.example.net: %v; muốn qua", err)
	}

	// Cert hỏng: bỏ qua phần (a), không làm hỏng PrepareAdd.
	se.m.SetFile(panelCertPath, []byte("không phải PEM"), 0o644, system.Root)
	if _, err := se.s.PrepareAdd(ctx, se.input(t, "panel.example.net")); err != nil {
		t.Errorf("cert hỏng: %v; muốn qua", err)
	}

	// Phần (b): tên đã ghi từ lần admin đăng nhập.
	if err := RecordPanelHost(ctx, se.st, "Panel2.Example.ORG:8443"); err != nil {
		t.Fatal(err)
	}
	_, err := se.s.PrepareAdd(ctx, se.input(t, "panel2.example.org"))
	wantReserved(t, err, "panel2.example.org")
}

func TestPanelHostNameB1(t *testing.T) {
	for in, want := range map[string]string{
		"panel.example.net:8443": "panel.example.net",
		"PANEL.example.net":      "panel.example.net",
		"192.0.2.10:8443":        "",
		"192.0.2.10":             "",
		"[2001:db8::1]:8443":     "",
		"[::1]":                  "",
		"localhost:8443":         "",
		"a.com; rm -rf /":        "",
		"$(id).example.com":      "",
		"../../etc/passwd":       "",
		"a.example.com\n":        "a.example.com", // ParseDomain bỏ khoảng trắng đầu cuối
		"":                       "",
	} {
		d, ok := PanelHostName(in)
		got := ""
		if ok {
			got = d.String()
		}
		if got != want {
			t.Errorf("PanelHostName(%q) = %q, muốn %q", in, got, want)
		}
	}
}

// TestRecordPanelHostB1: mới nhất lên đầu, không trùng, có trần; IP không ghi.
func TestRecordPanelHostB1(t *testing.T) {
	ctx := context.Background()
	se := newSiteEnv(t, "")
	if err := RecordPanelHost(ctx, se.st, "192.0.2.1:8443"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := se.st.GetSetting(ctx, SettingPanelHosts); ok {
		t.Error("Host là IP mà vẫn ghi")
	}
	for i := range panelHostsMax + 5 {
		if err := RecordPanelHost(ctx, se.st, "p"+string(rune('a'+i))+".example.com"); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordPanelHost(ctx, se.st, "pc.example.com:8443"); err != nil {
		t.Fatal(err)
	}
	names, err := panelHosts(ctx, se.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != panelHostsMax || names[0] != "pc.example.com" || names[1] != "p"+string(rune('a'+panelHostsMax+4))+".example.com" {
		t.Errorf("danh sách = %v", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("trùng %s trong %v", n, names)
		}
		seen[n] = true
	}
}
