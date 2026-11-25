package main

// tpan setup: khởi tạo panel trên máy chủ (mục 5, 9 và 11 của docs/KE-HOACH.md):
// thư mục, chứng chỉ tự ký, DB, mật khẩu quản trị, dịch vụ systemd.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

func init() { register(setupCmd) }

const (
	setupConfDir  = "/etc/tpan"
	setupTLSDir   = "/etc/tpan/tls"
	setupCertPath = "/etc/tpan/tls/cert.pem"
	setupKeyPath  = "/etc/tpan/tls/key.pem"
	setupStateDir = "/var/lib/tpan"
	setupUnitPath = "/etc/systemd/system/tpan.service"
	setupUnit     = "tpan.service"
	setupBinary   = "/usr/local/bin/tpan"
	setupPort     = "8443"
)

// Những thứ setup đọc từ máy mà không đi qua system.System (chỉ đọc, qua thư
// viện chuẩn của Go). Biến gói để test thay được; env nằm ở main.go nên không
// thêm trường vào đó.
var (
	setupHostname = os.Hostname
	setupAddrs    = net.InterfaceAddrs
	setupNow      = time.Now
)

func setupCmd(e *env) *cobra.Command {
	var (
		private bool
		f       planRun
	)
	c := &cobra.Command{
		Use:   "setup [--private]",
		Short: "Khởi tạo panel: thư mục, chứng chỉ, DB, mật khẩu quản trị, dịch vụ systemd (chạy lại không đổi gì)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// Cả --dry-run cũng cần root: phải đọc khoá riêng ở /etc/tpan/tls để
			// biết có cần sinh lại không.
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			sp, err := buildSetupPlan(e.sys, private)
			if err != nil {
				return err
			}

			if ok, err := f.previewOrConfirm(e, func() (engine.Preview, error) {
				return engine.New(e.sys, engine.Options{}).Preview(ctx, sp.plan)
			}); !ok {
				if err == nil && f.dryRun && !f.asJSON {
					printf(e.stdout, "\n%s\n", e.tr(
						"Sau đó: mở DB, tạo mật khẩu quản trị nếu chưa có (in ra một lần).",
						"Then: open the DB and generate an admin password if there is none (printed once)."))
				}
				return err
			}

			// Mở DB sau khi người dùng đã đồng ý (huỷ thì không để lại file DB),
			// và trước khi chạy Plan để job setup được ghi vào lịch sử.
			// store.Open tự tạo /var/lib/tpan với 0700 trên máy mới; bước Dir của
			// Plan sau đó đặt lại 0755 (core.StateDirPerm) để www-data tới được
			// thư mục ACME.
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			opts := engine.Options{Journal: st, OnEvent: f.progress(e, sp.plan).onEvent}
			rep, err := engine.New(e.sys, opts).Execute(ctx, sp.plan)
			if err != nil && rep.Job.ID == "" {
				return err
			}
			out := setupJSON{Report: rep, Fingerprint: sp.fingerprint, Listen: sp.listen}
			if err == nil && rep.Job.Status == engine.JobSucceeded {
				pw, created, perr := setupAdminPassword(ctx, st)
				if perr != nil {
					return perr
				}
				out.PasswordCreated, out.AdminPassword = created, pw
				if aerr := st.AddAudit(ctx, store.AuditEntry{
					Time: setupNow(), IP: "cli", Action: "setup", Target: sp.listen, Result: "ok", JobID: rep.Job.ID,
				}); aerr != nil {
					printf(e.stderr, "tpan: %v\n", aerr)
				}
				if rerr := recordSkippedFiles(ctx, st, sp.plan, rep); rerr != nil {
					printf(e.stderr, "tpan: %v\n", rerr)
				}
			}
			if f.asJSON {
				if werr := writeJSON(e.stdout, out); werr != nil {
					return werr
				}
			} else {
				printReport(e, rep)
				if rep.Job.Status == engine.JobSucceeded {
					printSetupResult(e, sp, out, rep)
				}
			}
			if err != nil {
				return err
			}
			if rep.Job.Status != engine.JobSucceeded {
				return &exitCode{1}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&private, "private", false, "chế độ riêng tư: chỉ nghe 127.0.0.1, vào bằng ssh -L 8443:127.0.0.1:8443 <máy>")
	f.flags(c)
	return c
}

// setupJSON là output --json của setup. Mật khẩu quản trị có mặt đúng một lần,
// khi setup vừa sinh nó (cùng lý do với site add --json).
type setupJSON struct {
	Report          engine.Report `json:"report"`
	Listen          string        `json:"listen"`
	Fingerprint     string        `json:"cert_sha256"`
	PasswordCreated bool          `json:"admin_password_created"`
	AdminPassword   string        `json:"admin_password,omitempty"`
}

// setupAdminPassword sinh mật khẩu quản trị nếu DB chưa có hash. Đã có thì
// không đụng: chạy lại setup không bao giờ đổi mật khẩu (muốn đổi thì
// tpan admin passwd).
func setupAdminPassword(ctx context.Context, st *store.Store) (pw string, created bool, err error) {
	if _, ok, err := st.AdminPasswordHash(ctx); err != nil || ok {
		return "", false, err
	}
	pw = store.GeneratePassword()
	hash, err := store.HashPasswordWait(pw)
	if err != nil {
		return "", false, err
	}
	if err := st.SetAdminPasswordHash(ctx, hash); err != nil {
		return "", false, err
	}
	return pw, true, nil
}

func printSetupResult(e *env, sp setupPlan, out setupJSON, rep engine.Report) {
	w := e.stdout
	printf(w, "\n")
	if out.PasswordCreated {
		printf(w, "%s\n    %s\n", e.tr(
			"Mật khẩu quản trị (chỉ hiện MỘT lần, hãy lưu lại ngay):",
			"Admin password (shown ONCE, save it now):"), out.AdminPassword)
	} else {
		printf(w, "%s\n", e.tr(
			"Mật khẩu quản trị đã có từ trước, giữ nguyên. Quên thì chạy: sudo tpan admin passwd",
			"The admin password already exists and was kept. If you forgot it, run: sudo tpan admin passwd"))
	}
	printf(w, "%s\n    %s\n", e.tr(
		"Dấu vân tay SHA-256 của chứng chỉ (đối chiếu với trình duyệt khi nó cảnh báo chứng chỉ tự ký):",
		"SHA-256 fingerprint of the certificate (compare it with the browser's self-signed warning):"), out.Fingerprint)
	if sp.private {
		printf(w, "%s\n    ssh -L %s:127.0.0.1:%s <%s>\n    https://127.0.0.1:%s\n", e.tr(
			"Chế độ riêng tư: panel chỉ nghe 127.0.0.1. Từ máy của bạn:",
			"Private mode: the panel only listens on 127.0.0.1. From your computer:"),
			setupPort, setupPort, e.tr("máy", "server"), setupPort)
	} else {
		printf(w, "%s https://<%s>:%s\n", e.tr("Mở:", "Open:"), e.tr("IP của máy", "server IP"), setupPort)
	}
	// File unit vừa đổi mà dịch vụ vốn đang chạy: systemd đã nạp unit mới
	// (daemon-reload) nhưng tiến trình cũ vẫn nghe theo cờ cũ.
	if stepOutcome(rep, "file:"+setupUnitPath) == "applied" && stepOutcome(rep, "service:"+setupUnit) == "skipped" {
		printf(w, "%s\n    systemctl restart %s\n", e.tr(
			"File dịch vụ đã đổi; khởi động lại để dùng cấu hình mới:",
			"The service file changed; restart to use the new configuration:"), setupUnit)
	}
}

func stepOutcome(rep engine.Report, id string) string {
	for _, s := range rep.Steps {
		if s.ID == id {
			return s.Outcome
		}
	}
	return ""
}

// recordSkippedFiles ghi sha256 của mọi *steps.File trong Plan mà engine bỏ
// qua vì file đã đúng. Engine chỉ ghi file của bước đã Apply, nên thiếu bước
// này thì một file đã đúng sẵn vẫn bị so với bản cũ trong managed_files. Ví dụ
// thật: máy từng chạy setup --private (unit --listen 127.0.0.1), người vận
// hành sửa tay thành 0.0.0.0 đúng như setup thường ghi; chạy lại setup báo
// "đã đúng, bỏ qua" mà phép kiểm lệch vẫn đỏ mãi. Khoá riêng (setupKeyFile)
// không phải *steps.File nên không vào đây, như lúc engine Apply nó.
func recordSkippedFiles(ctx context.Context, j interface {
	RecordFile(ctx context.Context, path, sha256Hex, jobID string) error
}, plan engine.Plan, rep engine.Report) error {
	for _, s := range plan.Steps {
		f, ok := s.(*steps.File)
		if !ok || stepOutcome(rep, "file:"+f.Path) != "skipped" {
			continue
		}
		sum := sha256.Sum256(f.Content)
		if err := j.RecordFile(ctx, f.Path, hex.EncodeToString(sum[:]), rep.Job.ID); err != nil {
			return fmt.Errorf("ghi sha256 của %s: %w", f.Path, err)
		}
	}
	return nil
}

// setupPlan là Plan của setup cùng những gì cần in sau khi chạy.
type setupPlan struct {
	plan        engine.Plan
	fingerprint string // sha256 của chứng chỉ sẽ dùng, dạng AB:CD:...
	listen      string
	private     bool
}

// buildSetupPlan dựng Plan từ các bước có sẵn của M1 (Dir, File, Service).
// Chứng chỉ đã có và hợp lệ thì dùng lại đúng nội dung đó: sinh mới mỗi lần
// chạy thì bước ghi chứng chỉ không bao giờ "đã đúng", và người dùng phải
// chấp nhận lại cảnh báo của trình duyệt sau mỗi lần setup.
func buildSetupPlan(sys system.System, private bool) (setupPlan, error) {
	certPEM, keyPEM, err := setupCert(sys)
	if err != nil {
		return setupPlan{}, err
	}
	fp, err := certFingerprint(certPEM)
	if err != nil {
		return setupPlan{}, err
	}
	listen := "0.0.0.0:" + setupPort
	if private {
		listen = "127.0.0.1:" + setupPort
	}
	reload := &system.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}}
	secretDir := func(p string) *steps.Dir { return &steps.Dir{Path: p, Perm: 0o700, Owner: system.Root} }
	plan := engine.Plan{
		Kind:   "panel.setup",
		Target: listen,
		Title:  i18n.T("Khởi tạo panel tpan", "Set up the tpan panel"),
		Steps: []engine.Step{
			secretDir(setupConfDir),
			secretDir(setupTLSDir),
			&steps.File{
				Path: setupCertPath, Content: certPEM, Perm: 0o600, Owner: system.Root,
				Title: i18n.T("Ghi chứng chỉ tự ký của panel", "Write the panel's self-signed certificate"),
				Why: i18n.T(
					"Panel nghe HTTPS ở cổng 8443. Không có chứng chỉ thì mật khẩu quản trị đi trên mạng dạng chữ thường, ai nghe lén cũng đọc được.",
					"The panel listens for HTTPS on port 8443. Without a certificate the admin password crosses the network in clear text."),
			},
			&setupKeyFile{File: &steps.File{
				Path: setupKeyPath, Content: keyPEM, Perm: 0o600, Owner: system.Root,
				Title: i18n.T("Ghi khoá riêng của chứng chỉ (chỉ root đọc)", "Write the certificate's private key (root only)"),
				Why: i18n.T(
					"Chứng chỉ không có khoá riêng thì không dùng được. Khoá lộ thì người khác giả được panel, nên file chỉ root đọc được và nội dung không hiện trong bản xem trước.",
					"A certificate is useless without its private key. If the key leaks someone can impersonate the panel, so only root can read it and the preview never shows it."),
			}},
			// Không phải secretDir: stack install đặt /var/lib/tpan 0755 (xem
			// core.StateDirPerm); tpan.db bên trong vẫn 0600.
			&steps.Dir{Path: setupStateDir, Perm: core.StateDirPerm, Owner: system.Root},
			&steps.File{
				Path: setupUnitPath, Content: setupUnitFile(listen), Perm: 0o644, Owner: system.Root,
				Reload: reload,
				Title:  i18n.T("Ghi file dịch vụ systemd của tpan", "Write tpan's systemd service file"),
				Why: i18n.T(
					"systemd chạy panel từ file này, tự khởi động lại khi panel chết và khi máy khởi động. Thiếu nó thì panel chỉ chạy khi có người gõ tpan serve.",
					"systemd runs the panel from this file and restarts it when it dies or the machine boots. Without it the panel only runs while someone keeps tpan serve open."),
			},
			&steps.Service{Unit: setupUnit},
		},
	}
	return setupPlan{plan: plan, fingerprint: fp, listen: listen, private: private}, nil
}

// setupKeyFile là steps.File cho khoá riêng, nhưng Info không liệt kê file:
// engine đưa nội dung mới (và lúc xem trước, cả nội dung cũ đọc từ máy) của mọi
// file trong Info vào bản xem trước, mà bản đó in ra màn hình và ra JSON.
// Đổi lại, sha256 của khoá không vào bảng managed_files.
type setupKeyFile struct{ *steps.File }

func (s *setupKeyFile) Info() engine.StepInfo {
	info := s.File.Info()
	info.Files = nil
	return info
}

// setupUnitFile là file unit systemd. Đường dẫn và cờ đều là hằng số của tpan,
// không có gì từ người dùng.
func setupUnitFile(listen string) []byte {
	return []byte(`# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
[Unit]
Description=tpan panel
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=` + setupBinary + ` serve --listen ` + listen + ` --tls-cert ` + setupCertPath + ` --tls-key ` + setupKeyPath + `
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=multi-user.target
`)
}

// setupCert trả chứng chỉ và khoá đang có nếu cả hai đọc được và khớp nhau;
// không thì sinh mới.
func setupCert(sys system.System) (certPEM, keyPEM []byte, err error) {
	certPEM, cerr := sys.ReadFile(setupCertPath)
	keyPEM, kerr := sys.ReadFile(setupKeyPath)
	if cerr == nil && kerr == nil {
		if _, err := tls.X509KeyPair(certPEM, keyPEM); err == nil {
			return certPEM, keyPEM, nil
		}
	}
	host, err := setupHostname()
	if err != nil {
		return nil, nil, fmt.Errorf("đọc hostname: %w", err)
	}
	addrs, err := setupAddrs()
	if err != nil {
		return nil, nil, fmt.Errorf("đọc địa chỉ IP của máy: %w", err)
	}
	return newSelfSignedCert(host, addrs, setupNow())
}

// dnsName: hostname chỉ vào SAN khi trông như tên DNS; tên lạ thì bỏ (chứng
// chỉ vẫn dùng được qua IP).
var dnsName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

// selfCertDays là hạn chứng chỉ tự ký mới sinh (M20, mục 3.8 của thiết kế):
// 825 ngày là trần Apple đặt cho chứng chỉ máy chủ TLS trên macOS và iOS;
// thiết kế chọn đúng trần đó thay cho 10 năm của bản cũ.
const selfCertDays = 825

// newSelfSignedCert sinh khoá ECDSA P-256 và chứng chỉ tự ký hạn 825 ngày cho
// hostname và các IP của máy. Tự ký nên trình duyệt sẽ cảnh báo; người dùng
// đối chiếu fingerprint setup in ra để biết đúng là panel của mình. Chứng chỉ
// đang có thì setupCert giữ nguyên (không bắt ai chấp nhận lại cảnh báo).
func newSelfSignedCert(host string, addrs []net.Addr, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("sinh khoá cho chứng chỉ: %w", err)
	}
	notBefore := now.Add(-time.Hour) // đồng hồ hai máy lệch nhau chút vẫn nhận
	tmpl := &x509.Certificate{
		// SerialNumber để nil: Go tự sinh số ngẫu nhiên.
		Subject:     pkix.Name{CommonName: "tpan"},
		NotBefore:   notBefore,
		NotAfter:    notBefore.Add(selfCertDays * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"},
	}
	if dnsName.MatchString(host) && !strings.EqualFold(host, "localhost") {
		tmpl.Subject.CommonName = host
		tmpl.DNSNames = append([]string{host}, tmpl.DNSNames...)
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		tmpl.IPAddresses = append(tmpl.IPAddresses, ipn.IP)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("tạo chứng chỉ tự ký: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("mã hoá khoá riêng: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// certFingerprint là sha256 của chứng chỉ (DER), dạng AB:CD:... như trình
// duyệt và `openssl x509 -fingerprint -sha256` hiển thị.
func certFingerprint(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("đọc chứng chỉ %s: không phải PEM CERTIFICATE", setupCertPath)
	}
	sum := sha256.Sum256(block.Bytes)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":"), nil
}
