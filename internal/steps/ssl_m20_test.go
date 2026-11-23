package steps

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
	"github.com/doxuta/tpan/internal/system"
)

// M20 (mục 11.7 của docs/thay-doi/M20-thiet-ke.md): CertName cho chứng chỉ của
// panel. Chứng chỉ ở đây là chứng chỉ thật (x509) vì Check có CertName đọc SAN.

const panelLineage = "tpan-panel"

// m20CertPEM sinh chứng chỉ tự ký có SAN là names.
func m20CertPEM(t *testing.T, names ...string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
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

// m20SetLineage đặt lineage name như certbot để lại: live/<name>/fullchain.pem
// là symlink sang archive/, renewal/<name>.conf có dòng server.
func m20SetLineage(t *testing.T, m *machine, name, server string, sans ...string) {
	t.Helper()
	m.SetFile("/etc/letsencrypt/archive/"+name+"/fullchain1.pem", m20CertPEM(t, sans...), 0o644, system.Root)
	m.SetSymlink("../../archive/"+name+"/fullchain1.pem", "/etc/letsencrypt/live/"+name+"/fullchain.pem")
	m.SetFile("/etc/letsencrypt/renewal/"+name+".conf", []byte("version = 2.9.0\n\n[renewalparams]\nserver = "+server+"\n"), 0o644, system.Root)
}

// m20Certbot giả certbot ghi và xoá theo --cert-name (không theo -d như
// sslCertbot), SAN là giá trị của -d. Trả các argv đã gọi.
func m20Certbot(t *testing.T, m *machine) *[][]string {
	var calls [][]string
	m.Handle([]string{"certbot", "certonly"}, func(c system.Cmd) system.Result {
		calls = append(calls, slices.Clone(c.Args))
		at := func(f string) string { return c.Args[slices.Index(c.Args, f)+1] }
		m20SetLineage(t, m, at("--cert-name"), at("--server"), at("-d"))
		return system.Result{Stdout: "Successfully received certificate.\n"}
	})
	m.Handle([]string{"certbot", "delete"}, func(c system.Cmd) system.Result {
		calls = append(calls, slices.Clone(c.Args))
		n := c.Args[slices.Index(c.Args, "--cert-name")+1]
		for _, p := range []string{
			"/etc/letsencrypt/live/" + n + "/fullchain.pem", "/etc/letsencrypt/live/" + n,
			"/etc/letsencrypt/archive/" + n + "/fullchain1.pem", "/etc/letsencrypt/archive/" + n,
			"/etc/letsencrypt/renewal/" + n + ".conf",
		} {
			if err := m.Remove(p); err != nil {
				return system.Result{ExitCode: 1, Stderr: err.Error()}
			}
		}
		return system.Result{}
	})
	return &calls
}

func panelCert(t *testing.T, server string) *CertbotCert {
	c := sslCert(t, server)
	c.CertName = panelLineage
	return c
}

func TestCertbotCertTheoCertName(t *testing.T) {
	ctx := context.Background()

	t.Run("vòng đời", func(t *testing.T) {
		lifecycle{
			name:   "CertbotCert CertName",
			setup:  func(m *machine) { m20Certbot(t, m) },
			step:   func() engine.Step { return panelCert(t, LetsEncryptProd) },
			noUndo: true,
		}.run(t)
	})

	t.Run("cờ và đường dẫn", func(t *testing.T) {
		m := newMachine(t)
		calls := m20Certbot(t, m)
		s := panelCert(t, LetsEncryptProd)
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		want := []string{"certonly", "--webroot", "-w", sslWebroot, "-d", "example.com", "--cert-name", panelLineage,
			"--non-interactive", "--agree-tos", "-m", "admin@example.com", "--server", LetsEncryptProd}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("argv = %q, muốn %q", *calls, want)
		}
		// Chứng chỉ ở live/tpan-panel, không ở live/<tên miền>.
		if _, err := m.Stat("/etc/letsencrypt/live/example.com"); err == nil {
			t.Error("có live/example.com: bước dùng nhầm lineage của site")
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Errorf("Check = %v, %v", done, err)
		}
		if i := s.Info(); i.ID != "ssl.cert:example.com" || !strings.Contains(i.Commands[0], "--cert-name tpan-panel") || i.NoUndo.IsZero() {
			t.Errorf("Info = %+v", i)
		}
	})

	t.Run("SAN không có tên miền thì Check false", func(t *testing.T) {
		m := newMachine(t)
		m20SetLineage(t, m, panelLineage, LetsEncryptProd, "cu.example.com")
		if done, err := panelCert(t, LetsEncryptProd).Check(ctx, m); err != nil || done {
			t.Errorf("Check = %v, %v; muốn false, nil", done, err)
		}
	})

	t.Run("file không đọc được như chứng chỉ thì Check false", func(t *testing.T) {
		m := newMachine(t)
		sslSetCert(m, panelLineage, LetsEncryptProd) // nội dung "-----BEGIN CERTIFICATE-----" cụt
		if done, err := panelCert(t, LetsEncryptProd).Check(ctx, m); err != nil || done {
			t.Errorf("Check = %v, %v; muốn false, nil", done, err)
		}
	})

	t.Run("chứng chỉ của site cùng tên không làm bước panel coi là xong", func(t *testing.T) {
		m := newMachine(t)
		m20SetLineage(t, m, "example.com", LetsEncryptProd, "example.com")
		if done, err := panelCert(t, LetsEncryptProd).Check(ctx, m); err != nil || done {
			t.Errorf("Check = %v, %v; muốn false, nil", done, err)
		}
	})

	t.Run("tên lineage lạ bị từ chối", func(t *testing.T) {
		for _, n := range []string{"a.b", "../x", "A", "a b", "-x\n", strings.Repeat("a", 65), "$(id)"} {
			s := panelCert(t, LetsEncryptProd)
			s.CertName = n
			if _, err := s.Check(ctx, newMachine(t)); err == nil {
				t.Errorf("CertName %q được nhận", n)
			}
		}
	})
}

func TestCertbotDeleteTheoCertName(t *testing.T) {
	ctx := context.Background()

	t.Run("vòng đời", func(t *testing.T) {
		lifecycle{
			name: "CertbotDelete CertName",
			setup: func(m *machine) {
				m20Certbot(t, m)
				m20SetLineage(t, m, panelLineage, LetsEncryptProd, "panel.example.com")
			},
			step:   func() engine.Step { return &CertbotDelete{CertName: panelLineage} },
			noUndo: true,
		}.run(t)
	})

	t.Run("có live/tpan-panel mà không có live/<tên miền> thì Check false", func(t *testing.T) {
		m := newMachine(t)
		m20SetLineage(t, m, panelLineage, LetsEncryptProd, "example.com")
		s := &CertbotDelete{CertName: panelLineage}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v; muốn false, nil", done, err)
		}
		// Ngược lại: lineage của site có mà của panel không thì panel đã sạch.
		m2 := newMachine(t)
		m20SetLineage(t, m2, "example.com", LetsEncryptProd, "example.com")
		if done, err := s.Check(ctx, m2); err != nil || !done {
			t.Fatalf("Check (chỉ có lineage site) = %v, %v; muốn true, nil", done, err)
		}
	})

	t.Run("lệnh, ID và không cần Layout", func(t *testing.T) {
		m := newMachine(t)
		calls := m20Certbot(t, m)
		m20SetLineage(t, m, panelLineage, LetsEncryptProd, "example.com")
		m20SetLineage(t, m, "example.com", LetsEncryptProd, "example.com")
		s := &CertbotDelete{CertName: panelLineage}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		want := []string{"delete", "--cert-name", panelLineage, "--non-interactive"}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("argv = %q, muốn %q", *calls, want)
		}
		if _, err := m.Stat("/etc/letsencrypt/live/example.com"); err != nil {
			t.Errorf("chứng chỉ của site bị xoá theo: %v", err)
		}
		if i := s.Info(); i.ID != "ssl.delete:tpan-panel" || !i.Danger || i.NoUndo.IsZero() {
			t.Errorf("Info = %+v", i)
		}
	})

	t.Run("tên lineage lạ bị từ chối", func(t *testing.T) {
		for _, n := range []string{"a.com", "../../etc", "x/y", "a;rm -rf /", strings.Repeat("a", 65)} {
			s := &CertbotDelete{CertName: n}
			if _, err := s.Check(ctx, newMachine(t)); err == nil {
				t.Errorf("Check nhận CertName %q", n)
			}
			m := newMachine(t)
			if err := s.Apply(ctx, m); err == nil || len(m.Ran()) != 0 {
				t.Errorf("Apply nhận CertName %q (lệnh: %v)", n, m.Ran())
			}
		}
	})
}
