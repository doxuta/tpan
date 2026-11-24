package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// fakeMachine đặt hostname, IP và đồng hồ cố định cho setup.
func fakeMachine(t *testing.T) {
	t.Helper()
	oh, oa, on := setupHostname, setupAddrs, setupNow
	setupHostname = func() (string, error) { return "may-thu", nil }
	setupAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
		}, nil
	}
	setupNow = func() time.Time { return time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { setupHostname, setupAddrs, setupNow = oh, oa, on })
}

// fakeSystemd giả lập systemctl: tpan.service chưa chạy, chưa bật cho tới khi
// có `systemctl enable --now tpan.service`.
func fakeSystemd(c *cli) {
	on := false
	ask := func(code int) func(system.Cmd) system.Result {
		return func(system.Cmd) system.Result {
			if on {
				return system.Result{}
			}
			return system.Result{ExitCode: code}
		}
	}
	c.fake.Handle([]string{"systemctl", "is-active"}, ask(3))
	c.fake.Handle([]string{"systemctl", "is-enabled"}, ask(1))
	c.fake.Handle([]string{"systemctl", "enable", "--now"}, func(system.Cmd) system.Result {
		on = true
		return system.Result{}
	})
}

func newSetupCLI(t *testing.T) *cli {
	t.Helper()
	fakeMachine(t)
	c := newCLI(t)
	// Máy có systemd luôn có thư mục này; setup không tạo nó.
	c.fake.SetDir("/etc/systemd/system", 0o755, system.Root)
	fakeSystemd(c)
	return c
}

func TestSetupLanDauVaChayLai(t *testing.T) {
	c := newSetupCLI(t)
	code, out, errb := c.run("setup", "--yes", "--json")
	if code != 0 {
		t.Fatalf("setup: mã %d\n%s\n%s", code, out, errb)
	}
	var res setupJSON
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if res.Report.Job.Status != engine.JobSucceeded || !res.PasswordCreated || len(res.AdminPassword) < 20 {
		t.Fatalf("kết quả: %+v", res)
	}

	// Các bước đúng thứ tự.
	var ids []string
	for _, s := range res.Report.Steps {
		ids = append(ids, s.ID)
		if s.Outcome != "applied" {
			t.Errorf("bước %s: %s, muốn applied", s.ID, s.Outcome)
		}
	}
	want := []string{
		"dir:/etc/tpan", "dir:/etc/tpan/tls", "file:/etc/tpan/tls/cert.pem", "file:/etc/tpan/tls/key.pem",
		"dir:/var/lib/tpan", "file:/etc/systemd/system/tpan.service", "service:tpan.service",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("thứ tự bước:\n%v\nmuốn\n%v", ids, want)
	}
	var cmds []string
	for _, cmd := range c.fake.Ran() {
		if cmd.Args[0] != "is-active" && cmd.Args[0] != "is-enabled" {
			cmds = append(cmds, cmd.String())
		}
	}
	// reset-failed trước enable: xoá bộ đếm start rate limit mà một file chạy
	// cũ chết liên tục để lại (steps.Service.Apply).
	if !reflect.DeepEqual(cmds, []string{"systemctl daemon-reload", "systemctl reset-failed tpan.service", "systemctl enable --now tpan.service"}) {
		t.Fatalf("lệnh: %v", cmds)
	}

	// Quyền và nội dung trên máy giả.
	snap := c.fake.Snapshot()
	for p, perm := range map[string]uint32{
		"/etc/tpan": 0o700, "/etc/tpan/tls": 0o700, "/var/lib/tpan": 0o755,
		"/etc/tpan/tls/cert.pem": 0o600, "/etc/tpan/tls/key.pem": 0o600,
		"/etc/systemd/system/tpan.service": 0o644,
	} {
		if got := uint32(snap[p].Mode.Perm()); got != perm {
			t.Errorf("%s: quyền %o, muốn %o", p, got, perm)
		}
	}
	unit := snap["/etc/systemd/system/tpan.service"].Data
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/tpan serve --listen 0.0.0.0:8443 --tls-cert /etc/tpan/tls/cert.pem --tls-key /etc/tpan/tls/key.pem\n") {
		t.Fatalf("file unit:\n%s", unit)
	}

	// Chứng chỉ: P-256, SAN có hostname và IP (bỏ link-local).
	block, _ := pem.Decode([]byte(snap["/etc/tpan/tls/cert.pem"].Data))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	// M20: hạn không quá 825 ngày (trần của Apple cho chứng chỉ máy chủ).
	if cert.PublicKeyAlgorithm != x509.ECDSA || cert.NotAfter.Sub(cert.NotBefore) > 825*24*time.Hour ||
		!cert.NotAfter.Equal(cert.NotBefore.Add(825*24*time.Hour)) {
		t.Fatalf("chứng chỉ: %v, hết hạn %v", cert.PublicKeyAlgorithm, cert.NotAfter)
	}
	if !reflect.DeepEqual(cert.DNSNames, []string{"may-thu", "localhost"}) || len(cert.IPAddresses) != 2 {
		t.Fatalf("SAN: %v %v", cert.DNSNames, cert.IPAddresses)
	}
	if fp, _ := certFingerprint([]byte(snap["/etc/tpan/tls/cert.pem"].Data)); fp != res.Fingerprint || len(fp) != 95 {
		t.Fatalf("fingerprint %q, in ra %q", fp, res.Fingerprint)
	}

	// DB giữ hash, không giữ mật khẩu thật.
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		t.Fatal(err)
	}
	hash, ok, _ := st.AdminPasswordHash(context.Background())
	_ = st.Close()
	if !ok || strings.Contains(hash, res.AdminPassword) {
		t.Fatalf("hash: %q", hash)
	}
	if ok, err := store.VerifyPassword(hash, res.AdminPassword); !ok || err != nil {
		t.Fatalf("mật khẩu in ra không khớp hash: %v %v", ok, err)
	}

	// Chạy lại: không đổi gì, không sinh mật khẩu mới.
	c.fake.ResetRan()
	code, out, errb = c.run("setup", "--yes", "--json")
	if code != 0 {
		t.Fatalf("chạy lại: mã %d\n%s\n%s", code, out, errb)
	}
	var again setupJSON
	_ = json.Unmarshal([]byte(out), &again)
	for _, s := range again.Report.Steps {
		if s.Outcome != "skipped" {
			t.Errorf("chạy lại, bước %s: %s, muốn skipped", s.ID, s.Outcome)
		}
	}
	if again.PasswordCreated || again.AdminPassword != "" || strings.Contains(out, res.AdminPassword) {
		t.Fatal("chạy lại không được sinh hay in mật khẩu")
	}
	if again.Fingerprint != res.Fingerprint {
		t.Fatal("chạy lại không được đổi chứng chỉ")
	}
	if bad := c.mutating(); len(bad) != 0 {
		t.Fatalf("chạy lại mà vẫn chạy lệnh thay đổi: %v", bad)
	}
	if !reflect.DeepEqual(c.fake.Snapshot(), snap) {
		t.Fatal("chạy lại làm đổi hệ thống file")
	}
	st, _ = store.Open(context.Background(), c.db)
	hash2, _, _ := st.AdminPasswordHash(context.Background())
	_ = st.Close()
	if hash2 != hash {
		t.Fatal("chạy lại làm đổi hash mật khẩu")
	}

	// Xem trước khi đã có khoá: khoá không lọt ra output.
	key := snap["/etc/tpan/tls/key.pem"].Data
	_, out, _ = c.run("setup", "--dry-run", "--json")
	if strings.Contains(out, base64.StdEncoding.EncodeToString([]byte(key))) || strings.Contains(out, "PRIVATE KEY") {
		t.Fatal("bản xem trước làm lộ khoá riêng")
	}
}

// Máy đã có stack (stack install để /var/lib/tpan 0755 cho acme): setup không
// được đặt lại quyền khác. Lỗi M11: setup đặt 0700, stack 0755, nên mỗi lần
// install.sh chạy lại (setup rồi stack install) bước dir:/var/lib/tpan lại chạy
// ở cả hai, và tpan setup chạy riêng làm www-data không tới được acme.
func TestSetupGiuQuyenThuMucTrangThaiCuaStack(t *testing.T) {
	c := newSetupCLI(t)
	c.fake.SetDir("/var/lib/tpan", core.StateDirPerm, system.Root)
	code, out, errb := c.run("setup", "--yes", "--json")
	if code != 0 {
		t.Fatalf("setup: mã %d\n%s\n%s", code, out, errb)
	}
	var res setupJSON
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	for _, s := range res.Report.Steps {
		if s.ID == "dir:/var/lib/tpan" && s.Outcome != "skipped" {
			t.Errorf("bước %s: %s, muốn skipped (quyền đã đúng như stack đặt)", s.ID, s.Outcome)
		}
	}
	if got := c.fake.Snapshot()["/var/lib/tpan"].Mode.Perm(); got != 0o755 {
		t.Fatalf("/var/lib/tpan: quyền %o, muốn 755", got)
	}
}

// Đường in cho người đọc (không --json): mật khẩu in đúng một lần ở lần đầu,
// lần sau không in.
func TestSetupInChoNguoiDoc(t *testing.T) {
	c := newSetupCLI(t)
	code, out, errb := c.run("setup", "--yes")
	if code != 0 {
		t.Fatalf("mã %d\n%s\n%s", code, out, errb)
	}
	_, after, ok := strings.Cut(out, "Mật khẩu quản trị (chỉ hiện MỘT lần, hãy lưu lại ngay):\n")
	if !ok {
		t.Fatalf("không thấy mật khẩu:\n%s", out)
	}
	pw, _, _ := strings.Cut(strings.TrimSpace(after), "\n")
	if len(pw) < 20 || strings.Count(out, pw) != 1 {
		t.Fatalf("mật khẩu %q phải in đúng một lần:\n%s", pw, out)
	}
	fp, _ := certFingerprint([]byte(c.fake.Snapshot()["/etc/tpan/tls/cert.pem"].Data))
	if !strings.Contains(out, fp) || !strings.Contains(out, "https://") {
		t.Fatalf("thiếu fingerprint hoặc địa chỉ:\n%s", out)
	}

	_, out, _ = c.run("setup", "--yes")
	if strings.Contains(out, pw) || !strings.Contains(out, "đã có từ trước") || !strings.Contains(out, fp) {
		t.Fatalf("chạy lại:\n%s", out)
	}
}

func TestSetupHuyKhongTaoDB(t *testing.T) {
	c := newSetupCLI(t)
	c.terminal, c.stdin = true, "n\n"
	if code, _, _ := c.run("setup"); code != 1 || c.dbExists() {
		t.Fatalf("huỷ: mã %d, DB tồn tại=%v", code, c.dbExists())
	}
}

func TestSetupDryRunKhongDeLaiGi(t *testing.T) {
	c := newSetupCLI(t)
	code, out, errb := c.run("setup", "--dry-run")
	if code != 0 {
		t.Fatalf("mã %d\n%s\n%s", code, out, errb)
	}
	if c.dbExists() {
		t.Fatal("--dry-run tạo DB")
	}
	if bad := c.mutating(); len(bad) != 0 {
		t.Fatalf("--dry-run chạy lệnh thay đổi: %v", bad)
	}
	if _, err := c.fake.Stat("/etc/tpan"); err == nil {
		t.Fatal("--dry-run tạo /etc/tpan")
	}
	if !strings.Contains(out, "dir:/etc/tpan") || strings.Contains(out, "PRIVATE KEY") {
		t.Fatalf("bản xem trước:\n%s", out)
	}
}

func TestSetupPrivateVaCanRoot(t *testing.T) {
	c := newSetupCLI(t)
	if code, _, _ := c.run("setup", "--private", "--yes"); code != 0 {
		t.Fatal("setup --private lỗi")
	}
	unit := c.fake.Snapshot()["/etc/systemd/system/tpan.service"].Data
	if !strings.Contains(unit, "--listen 127.0.0.1:8443 ") {
		t.Fatalf("--private phải nghe 127.0.0.1:\n%s", unit)
	}

	c = newSetupCLI(t)
	c.root = false
	code, _, errb := c.run("setup", "--yes")
	if code != 1 || !strings.Contains(errb, "sudo tpan setup") || c.dbExists() {
		t.Fatalf("không root: mã %d\n%s", code, errb)
	}
}

func TestIsLoopbackListen(t *testing.T) {
	for in, want := range map[string]bool{
		"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true,
		"0.0.0.0:8443": false, ":8080": false, "192.0.2.1:80": false, "rác": false,
	} {
		if got := isLoopbackListen(in); got != want {
			t.Errorf("isLoopbackListen(%q) = %v, muốn %v", in, got, want)
		}
	}
}

func TestServeTuChoiHTTPNgoaiLoopback(t *testing.T) {
	c := newCLI(t)
	if code, _, errb := c.run("serve", "--listen", "0.0.0.0:8080"); code != 1 || !strings.Contains(errb, "127.0.0.1") {
		t.Fatalf("serve HTTP trên 0.0.0.0: mã %d %s", code, errb)
	}
	if code, _, errb := c.run("serve", "--tls-cert", "/x.pem"); code != 1 || !strings.Contains(errb, "--tls-key") {
		t.Fatalf("thiếu --tls-key: mã %d %s", code, errb)
	}
}

// M20: máy nâng từ bản cũ giữ chứng chỉ tự ký 10 năm đang có (không bắt ai
// chấp nhận lại cảnh báo); chỉ chứng chỉ mới sinh mới có hạn 825 ngày.
func TestSetupGiuChungChiCu(t *testing.T) {
	fakeMachine(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := setupNow()
	tpl := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "tpan"},
		NotBefore: now.AddDate(-1, 0, 0), NotAfter: now.AddDate(9, 0, 0)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	f := system.NewFake()
	f.SetFile(setupCertPath, certPEM, 0o600, system.Root)
	f.SetFile(setupKeyPath, keyPEM, 0o600, system.Root)
	gotCert, gotKey, err := setupCert(f)
	if err != nil || !bytes.Equal(gotCert, certPEM) || !bytes.Equal(gotKey, keyPEM) {
		t.Fatalf("chứng chỉ đang có bị thay: %v", err)
	}
}
