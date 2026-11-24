package steps

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

const sslWebroot = "/var/lib/tpan/acme"

func sslLayout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

func sslEmail(t *testing.T) validate.Email {
	t.Helper()
	e, err := validate.ParseEmail("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func sslCert(t *testing.T, server string) *CertbotCert {
	return &CertbotCert{Layout: sslLayout(t), Webroot: sslWebroot, Email: sslEmail(t), Server: server}
}

// sslSetCert đặt chứng chỉ đúng hình dạng certbot để lại trên máy thật:
// live/<d>/fullchain.pem là symlink tương đối sang archive/<d>/fullchainN.pem,
// renewal/<d>.conf có dòng server trong [renewalparams].
func sslSetCert(m *machine, d, server string) {
	m.SetFile("/etc/letsencrypt/archive/"+d+"/fullchain1.pem", []byte("-----BEGIN CERTIFICATE-----\n"), 0o644, system.Root)
	m.SetFile("/etc/letsencrypt/archive/"+d+"/privkey1.pem", []byte("-----BEGIN PRIVATE KEY-----\n"), 0o600, system.Root)
	m.SetSymlink("../../archive/"+d+"/fullchain1.pem", "/etc/letsencrypt/live/"+d+"/fullchain.pem")
	m.SetSymlink("../../archive/"+d+"/privkey1.pem", "/etc/letsencrypt/live/"+d+"/privkey.pem")
	m.SetFile("/etc/letsencrypt/renewal/"+d+".conf", []byte(
		"# renew_before_expiry = 30 days\nversion = 2.9.0\narchive_dir = /etc/letsencrypt/archive/"+d+
			"\n\n[renewalparams]\naccount = 0123abcd\nauthenticator = webroot\nwebroot_path = "+sslWebroot+
			",\nserver = "+server+"\n"), 0o644, system.Root)
}

// sslCertbot giả certbot có trạng thái: certonly ghi chứng chỉ, delete xoá.
// Trả con trỏ tới danh sách argv của các lần certonly để test xem cờ.
func sslCertbot(m *machine) *[][]string {
	var mu sync.Mutex
	var calls [][]string
	m.Handle([]string{"certbot", "certonly"}, func(c system.Cmd) system.Result {
		mu.Lock()
		calls = append(calls, slices.Clone(c.Args))
		mu.Unlock()
		d := c.Args[slices.Index(c.Args, "-d")+1]
		server := c.Args[slices.Index(c.Args, "--server")+1]
		sslSetCert(m, d, server)
		return system.Result{Stdout: "Successfully received certificate.\n"}
	})
	m.Handle([]string{"certbot", "delete"}, func(c system.Cmd) system.Result {
		d := c.Args[slices.Index(c.Args, "--cert-name")+1]
		for _, p := range []string{
			"/etc/letsencrypt/live/" + d + "/fullchain.pem", "/etc/letsencrypt/live/" + d + "/privkey.pem",
			"/etc/letsencrypt/live/" + d,
			"/etc/letsencrypt/archive/" + d + "/fullchain1.pem", "/etc/letsencrypt/archive/" + d + "/privkey1.pem",
			"/etc/letsencrypt/archive/" + d,
			"/etc/letsencrypt/renewal/" + d + ".conf",
		} {
			if err := m.Remove(p); err != nil {
				return system.Result{ExitCode: 1, Stderr: err.Error()}
			}
		}
		return system.Result{Stdout: "Deleted all files relating to certificate " + d + ".\n"}
	})
	return &calls
}

// =============================================================== CertbotCert

func TestCertbotLifecycle(t *testing.T) {
	l := sslLayout(t)
	d := l.Domain.String()
	cases := []lifecycle{
		{
			name:   "CertbotCert máy chưa có chứng chỉ",
			setup:  func(m *machine) { sslCertbot(m) },
			step:   func() engine.Step { return sslCert(t, LetsEncryptStaging) },
			noUndo: true,
		},
		{
			name:   "CertbotCert đổi staging sang thật",
			setup:  func(m *machine) { sslCertbot(m); sslSetCert(m, d, LetsEncryptStaging) },
			step:   func() engine.Step { return sslCert(t, LetsEncryptProd) },
			noUndo: true,
		},
		{
			name:   "CertbotDelete",
			setup:  func(m *machine) { sslCertbot(m); sslSetCert(m, d, LetsEncryptProd) },
			step:   func() engine.Step { return &CertbotDelete{Layout: l} },
			noUndo: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}

func TestCertbotCertArgs(t *testing.T) {
	ctx := context.Background()
	l := sslLayout(t)
	base := []string{"certonly", "--webroot", "-w", sslWebroot, "-d", "example.com", "--cert-name", "example.com",
		"--non-interactive", "--agree-tos", "-m", "admin@example.com", "--server", LetsEncryptStaging}

	t.Run("lần đầu không ép gia hạn", func(t *testing.T) {
		m := newMachine(t)
		calls := sslCertbot(m)
		if err := sslCert(t, LetsEncryptStaging).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], base) {
			t.Errorf("argv = %q, muốn %q", *calls, base)
		}
	})

	t.Run("đã có chứng chỉ của máy chủ khác thì --force-renewal", func(t *testing.T) {
		m := newMachine(t)
		calls := sslCertbot(m)
		sslSetCert(m, l.Domain.String(), LetsEncryptProd)
		if err := sslCert(t, LetsEncryptStaging).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		want := append(slices.Clone(base), "--force-renewal")
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("argv = %q, muốn %q", *calls, want)
		}
	})

	t.Run("có fullchain nhưng renewal không có dòng server", func(t *testing.T) {
		m := newMachine(t)
		sslSetCert(m, l.Domain.String(), LetsEncryptStaging)
		m.SetFile(l.CertRenewal(), []byte("version = 2.9.0\n"), 0o644, system.Root)
		if done, err := sslCert(t, LetsEncryptStaging).Check(ctx, m); err != nil || done {
			t.Errorf("Check = %v, %v; muốn false, nil", done, err)
		}
	})

	t.Run("symlink fullchain trỏ vào file đã mất thì chưa có chứng chỉ", func(t *testing.T) {
		m := newMachine(t)
		sslSetCert(m, l.Domain.String(), LetsEncryptStaging)
		if err := m.Remove("/etc/letsencrypt/archive/example.com/fullchain1.pem"); err != nil {
			t.Fatal(err)
		}
		if done, err := sslCert(t, LetsEncryptStaging).Check(ctx, m); err != nil || done {
			t.Errorf("Check = %v, %v; muốn false, nil", done, err)
		}
	})
}

func TestCertbotErrors(t *testing.T) {
	ctx := context.Background()
	l := sslLayout(t)

	t.Run("certonly lỗi", func(t *testing.T) {
		m := newMachine(t)
		m.Handle([]string{"certbot", "certonly"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stdout: "Certbot failed to authenticate some domains\n", Stderr: "Some challenges have failed.\n"}
		})
		err := sslCert(t, LetsEncryptStaging).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "xin chứng chỉ cho example.com") ||
			!strings.Contains(err.Error(), "failed to authenticate") || !strings.Contains(err.Error(), "challenges have failed") {
			t.Errorf("lỗi = %v; muốn có ngữ cảnh, stdout và stderr", err)
		}
	})

	t.Run("delete lỗi", func(t *testing.T) {
		m := newMachine(t)
		m.failCmd([]string{"certbot", "delete"}, 1, "No certificate found with name example.com")
		err := (&CertbotDelete{Layout: l}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "xoá chứng chỉ của example.com") || !strings.Contains(err.Error(), "No certificate") {
			t.Errorf("lỗi = %v", err)
		}
	})

	t.Run("đọc renewal lỗi", func(t *testing.T) {
		m := newMachine(t)
		sslSetCert(m, l.Domain.String(), LetsEncryptStaging)
		// Thư mục ở chỗ file: ReadFile lỗi khác ErrNotExist.
		if err := m.Remove(l.CertRenewal()); err != nil {
			t.Fatal(err)
		}
		m.SetDir(l.CertRenewal(), 0o755, system.Root)
		if _, err := sslCert(t, LetsEncryptStaging).Check(ctx, m); err == nil || !strings.Contains(err.Error(), "đọc "+l.CertRenewal()) {
			t.Errorf("lỗi = %v", err)
		}
	})
}

// ================================================================= ACMEProbe

const sslChallengeURL = "http://example.com/.well-known/acme-challenge/"

// sslProbeMachine là máy có webroot, một curl giả đọc file thử thách từ hệ
// thống file giả (như vhost cổng 80 phục vụ nó), và getent giả.
func sslProbeMachine(t *testing.T) *machine {
	m := newMachine(t)
	m.SetDir(sslWebroot, 0o755, system.Root)
	// Prefix dài hơn handler curl chung của newMachine (vốn giả curl -o) nên thắng.
	m.Handle([]string{"curl", "-sS", "-m", "10"}, func(c system.Cmd) system.Result {
		name := strings.TrimPrefix(c.Args[len(c.Args)-1], sslChallengeURL)
		data, err := m.ReadFile(sslChallengeDir(sslWebroot) + "/" + name)
		if err != nil {
			return system.Result{Stdout: "<html><body>404 Not Found</body></html>\n"}
		}
		return system.Result{Stdout: string(data)}
	})
	m.Handle([]string{"getent", "ahosts"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "203.0.113.5     STREAM example.com\n203.0.113.5     DGRAM  \n203.0.113.5     RAW    \n"}
	})
	return m
}

func sslCurls(m *machine) []system.Cmd {
	return slices.DeleteFunc(m.Ran(), func(c system.Cmd) bool { return c.Name != "curl" })
}

func TestACMEProbe(t *testing.T) {
	ctx := context.Background()
	l := sslLayout(t)
	nameRe := regexp.MustCompile(`^` + regexp.QuoteMeta(sslChallengeURL) + `[a-z2-7]{26,}$`)

	for _, pre := range []bool{false, true} {
		name := "thư mục thử thách chưa có"
		if pre {
			name = "thư mục thử thách có sẵn"
		}
		t.Run(name, func(t *testing.T) {
			m := sslProbeMachine(t)
			if pre {
				m.SetDir(sslChallengeDir(sslWebroot), 0o755, system.Root)
			}
			before := m.Snapshot()
			s := &ACMEProbe{Layout: l, Webroot: sslWebroot}
			if done, err := s.Check(ctx, m); err != nil || done {
				t.Fatalf("Check trước Apply = %v, %v", done, err)
			}
			if err := s.Apply(ctx, m); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if done, err := s.Check(ctx, m); err != nil || !done {
				t.Fatalf("Check sau Apply = %v, %v", done, err)
			}
			// Apply không để lại gì: file thử và thư mục nó tạo đều đã xoá.
			if !reflect.DeepEqual(before, m.Snapshot()) {
				t.Errorf("Apply để lại thay đổi:\ntrước %v\nsau   %v", before, m.Snapshot())
			}
			curls := sslCurls(m)
			if len(curls) != 1 {
				t.Fatalf("curl chạy %d lần", len(curls))
			}
			a := curls[0].Args
			if !slices.Equal(a[:3], []string{"-sS", "-m", "10"}) || len(a) != 4 || !nameRe.MatchString(a[3]) {
				t.Errorf("curl sai: %s", curls[0].String())
			}
			if slices.ContainsFunc(a, func(x string) bool { return strings.Contains(x, "resolve") }) {
				t.Errorf("curl không được có --resolve: %s", curls[0].String())
			}
		})
	}

	t.Run("hai lần thử dùng hai tên khác nhau", func(t *testing.T) {
		m := sslProbeMachine(t)
		for range 2 {
			if err := (&ACMEProbe{Layout: l, Webroot: sslWebroot}).Apply(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
		c := sslCurls(m)
		if c[0].Args[3] == c[1].Args[3] {
			t.Error("tên file thử lặp lại")
		}
	})

	t.Run("đã có chứng chỉ thì bỏ qua, không chạy lệnh nào", func(t *testing.T) {
		m := sslProbeMachine(t)
		sslSetCert(m, "example.com", LetsEncryptStaging)
		before := m.Snapshot()
		rep := execute(t, m, &ACMEProbe{Layout: l, Webroot: sslWebroot, Cert: sslCert(t, LetsEncryptStaging)})
		if rep.Steps[0].Outcome != "skipped" {
			t.Errorf("outcome %q, muốn skipped", rep.Steps[0].Outcome)
		}
		if len(m.Ran()) > 0 || !reflect.DeepEqual(before, m.Snapshot()) {
			t.Errorf("bước bị bỏ qua mà vẫn chạy lệnh hoặc đổi máy: %q", ranStrings(m.Ran()))
		}
	})

	// Plan SSL thật: thử rồi xin. Chạy lại cả Plan thì không lệnh thay đổi nào chạy.
	t.Run("Plan thử rồi xin, chạy lại thì bỏ qua cả hai", func(t *testing.T) {
		m := sslProbeMachine(t)
		sslCertbot(m)
		plan := func() []engine.Step {
			cert := sslCert(t, LetsEncryptStaging)
			return []engine.Step{&ACMEProbe{Layout: l, Webroot: sslWebroot, Cert: cert}, cert}
		}
		if rep := execute(t, m, plan()...); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		m.ResetRan()
		rep := execute(t, m, plan()...)
		for _, st := range rep.Steps {
			if st.Outcome != "skipped" {
				t.Errorf("lần hai: %s outcome %q", st.ID, st.Outcome)
			}
		}
		if len(m.Ran()) > 0 {
			t.Errorf("lần hai chạy lệnh: %q", ranStrings(m.Ran()))
		}
	})

	t.Run("câu trả lời không khớp", func(t *testing.T) {
		m := sslProbeMachine(t)
		m.Handle([]string{"curl", "-sS", "-m", "10"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "<html>trang của máy khác</html>\n"}
		})
		before := m.Snapshot()
		s := &ACMEProbe{Layout: l, Webroot: sslWebroot}
		err := s.Apply(ctx, m)
		for _, want := range []string{"không về đúng máy này", "trang của máy khác", "đang trỏ về 203.0.113.5", "curl -sS -m 10 " + sslChallengeURL} {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("lỗi = %v; thiếu %q", err, want)
			}
		}
		if strings.Count(err.Error(), "203.0.113.5") != 1 {
			t.Errorf("IP lặp trong lỗi: %v", err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("Apply lỗi mà còn để lại file thử hoặc thư mục")
		}
		if done, _ := s.Check(ctx, m); done {
			t.Error("Apply lỗi mà Check vẫn true")
		}
	})

	t.Run("curl không kết nối được, DNS không phân giải", func(t *testing.T) {
		m := sslProbeMachine(t)
		m.failCmd([]string{"curl", "-sS", "-m", "10"}, 7, "curl: (7) Failed to connect to example.com port 80")
		m.failCmd([]string{"getent", "ahosts"}, 2, "")
		before := m.Snapshot()
		err := (&ACMEProbe{Layout: l, Webroot: sslWebroot}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "Failed to connect") || !strings.Contains(err.Error(), "không phân giải được") {
			t.Errorf("lỗi = %v", err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("Apply lỗi mà còn để lại file thử hoặc thư mục")
		}
	})

	t.Run("chưa có webroot", func(t *testing.T) {
		m := newMachine(t)
		err := (&ACMEProbe{Layout: l, Webroot: sslWebroot}).Apply(ctx, m)
		if err == nil || !errors.Is(err, fs.ErrNotExist) || len(m.Ran()) > 0 {
			t.Errorf("lỗi = %v, lệnh %q", err, ranStrings(m.Ran()))
		}
	})

	t.Run("Cert của site khác hoặc webroot khác", func(t *testing.T) {
		m := sslProbeMachine(t)
		other, _ := validate.ParseDomain("other.example.com")
		c1 := sslCert(t, LetsEncryptStaging)
		c1.Layout = site.LayoutOf(other)
		c2 := sslCert(t, LetsEncryptStaging)
		c2.Webroot = "/var/lib/tpan/khac"
		for _, c := range []*CertbotCert{c1, c2} {
			s := &ACMEProbe{Layout: l, Webroot: sslWebroot, Cert: c}
			if _, err := s.Check(ctx, m); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Error("Apply không lỗi")
			}
		}
		if len(m.Ran()) > 0 {
			t.Errorf("chạy lệnh: %q", ranStrings(m.Ran()))
		}
	})
}

// ================================================================== WPSiteURL

// sslWPSim là wpSim cộng `wp option get|update` có trạng thái. Giá trị nằm
// trong hệ thống file giả (/wpdb/<tên>) để Snapshot thấy được.
func sslWPSim(t *testing.T, home, siteurl string) *wpSim {
	m := wpNewSim(t)
	l := m.l
	m.SetFile("/wpdb/home", []byte(home), 0o600, system.Root)
	m.SetFile("/wpdb/siteurl", []byte(siteurl), 0o600, system.Root)
	m.Handle([]string{"wp", "option"}, func(c system.Cmd) system.Result {
		if c.AsUser != l.User || !slices.Contains(c.Args, "--path="+l.Public) ||
			!slices.Contains(c.Args, "--skip-plugins") || !slices.Contains(c.Args, "--skip-themes") {
			return system.Result{ExitCode: 1, Stderr: "Error: chạy wp sai user hoặc thiếu cờ"}
		}
		opt := c.Args[2]
		switch c.Args[1] {
		case "get":
			data, err := m.ReadFile("/wpdb/" + opt)
			if err != nil {
				return system.Result{ExitCode: 1, Stderr: "Error: Could not get '" + opt + "' option. Does it exist?"}
			}
			return system.Result{Stdout: string(data) + "\n"}
		case "update":
			if !slices.Contains(c.Args, "--quiet") {
				return system.Result{ExitCode: 1, Stderr: "Error: thiếu --quiet"}
			}
			m.SetFile("/wpdb/"+opt, []byte(c.Args[3]), 0o600, system.Root)
			return system.Result{}
		}
		return system.Result{ExitCode: 1, Stderr: "Error: lệnh wp option lạ"}
	})
	return m
}

func TestWPSiteURLLifecycle(t *testing.T) {
	for _, c := range []struct{ name, home, siteurl string }{
		{"cả hai còn http", "http://example.com", "http://example.com"},
		{"chỉ home còn http", "http://example.com", "https://example.com"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sslWPLifecycle(t, func(t *testing.T) *wpSim { return sslWPSim(t, c.home, c.siteurl) })
		})
	}
}

// sslWPLifecycle là ba trường hợp của skill tpan-step cho WPSiteURL. Không dùng
// wpLifecycle vì máy cần handler wp option có trạng thái (dựng bằng mk).
func sslWPLifecycle(t *testing.T, mk func(t *testing.T) *wpSim) {
	step := func(m *wpSim) engine.Step { return &WPSiteURL{Layout: m.l, URL: "https://example.com"} }
	ctx := context.Background()
	t.Run("máy sạch rồi Apply thì Check true", func(t *testing.T) {
		m := mk(t)
		s := step(m)
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v", done, err)
		}
	})
	t.Run("chạy lại thì không lệnh thay đổi nào chạy", func(t *testing.T) {
		m := mk(t)
		if rep := wpExecute(t, m, step(m)); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := m.whole()
		m.ResetRan()
		if rep := wpExecute(t, m, step(m)); rep.Steps[0].Outcome != "skipped" {
			t.Fatalf("lần hai: outcome %q", rep.Steps[0].Outcome)
		}
		for _, c := range m.Ran() {
			if !wpReadOnly(c) {
				t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})
	t.Run("Apply rồi Undo thì máy về như cũ", func(t *testing.T) {
		m := mk(t)
		before := m.whole()
		s := step(m)
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if reflect.DeepEqual(before, m.whole()) {
			t.Fatal("Apply không đổi gì")
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("máy không về như cũ\ntrước %v\nsau   %v", before, m.whole())
		}
	})
}

func sslWPUpdates(m *wpSim) []string {
	var out []string
	for _, c := range m.Ran() {
		if c.Name == "wp" && len(c.Args) > 3 && c.Args[1] == "update" {
			out = append(out, c.Args[2]+"="+c.Args[3])
		}
	}
	return out
}

func TestWPSiteURLApply(t *testing.T) {
	ctx := context.Background()

	t.Run("chỉ đổi tuỳ chọn còn khác, Undo chỉ trả tuỳ chọn đó", func(t *testing.T) {
		m := sslWPSim(t, "http://example.com", "https://example.com")
		s := &WPSiteURL{Layout: m.l, URL: "https://example.com"}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := sslWPUpdates(m); !reflect.DeepEqual(got, []string{"home=https://example.com"}) {
			t.Errorf("Apply cập nhật %q", got)
		}
		m.ResetRan()
		if err := s.Undo(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := sslWPUpdates(m); !reflect.DeepEqual(got, []string{"home=http://example.com"}) {
			t.Errorf("Undo cập nhật %q", got)
		}
	})

	t.Run("update siteurl lỗi thì trả home về", func(t *testing.T) {
		m := sslWPSim(t, "http://example.com", "http://example.com")
		m.failCmd([]string{"wp", "option", "update", "siteurl"}, 1, "Error: Database connection lost")
		before := m.whole()
		err := (&WPSiteURL{Layout: m.l, URL: "https://example.com"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đặt tuỳ chọn siteurl") || !strings.Contains(err.Error(), "connection lost") {
			t.Errorf("lỗi = %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà home không được trả về")
		}
	})

	t.Run("get lỗi", func(t *testing.T) {
		m := sslWPSim(t, "http://example.com", "http://example.com")
		m.failCmd([]string{"wp", "option", "get"}, 255, "PHP Fatal error")
		if _, err := (&WPSiteURL{Layout: m.l, URL: "https://example.com"}).Check(ctx, m); err == nil ||
			!strings.Contains(err.Error(), "đọc tuỳ chọn home") || !strings.Contains(err.Error(), "PHP Fatal") {
			t.Errorf("lỗi = %v", err)
		}
	})

	t.Run("giá trị cũ không đặt lại được thì không đổi gì", func(t *testing.T) {
		m := sslWPSim(t, "--url=evil", "http://example.com")
		before := m.whole()
		err := (&WPSiteURL{Layout: m.l, URL: "https://example.com"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "không đặt lại được") {
			t.Errorf("lỗi = %v", err)
		}
		if len(sslWPUpdates(m)) > 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Error("đã đổi tuỳ chọn dù giá trị cũ không trả lại được")
		}
	})

	t.Run("Undo khi chưa Apply", func(t *testing.T) {
		m := sslWPSim(t, "http://example.com", "http://example.com")
		if err := (&WPSiteURL{Layout: m.l, URL: "https://example.com"}).Undo(ctx, m); !errors.Is(err, errNotApplied) {
			t.Errorf("Undo = %v", err)
		}
	})
}

// ======================================================= Tham số sai, Info

func TestSSLBadParams(t *testing.T) {
	ctx := context.Background()
	l := sslLayout(t)
	tampered := l
	tampered.Public = "/etc"
	badLayouts := map[string]site.Layout{"zero": {}, "sửa tay": tampered}

	var stepsBad []engine.Step
	for _, bl := range badLayouts {
		c := sslCert(t, LetsEncryptStaging)
		c.Layout = bl
		stepsBad = append(stepsBad, c,
			&ACMEProbe{Layout: bl, Webroot: sslWebroot},
			&CertbotDelete{Layout: bl},
			&WPSiteURL{Layout: bl, URL: "https://example.com"})
	}
	for _, srv := range []string{
		"", "http://acme.example.com/directory", "https://", "https://a.com/dir; rm -rf /",
		"https://$(id)/", "https://a.com/`id`", "https://a.com/dir\n--staging", "https://a.com/dir?x=1",
		"https://a.com:123456/", "--server=https://a.com", "https://a.com dir", "https://ác.com/",
		"https://" + strings.Repeat("a", 300) + ".com/\x00", "ftp://a.com/",
	} {
		stepsBad = append(stepsBad, sslCert(t, srv))
	}
	for _, w := range []string{"", "acme", "/var/lib/tpan/../../etc", "/", "/var/lib/tpan/acme/", "/var/lib/tpan/acme\n"} {
		c := sslCert(t, LetsEncryptStaging)
		c.Webroot = w
		stepsBad = append(stepsBad, c, &ACMEProbe{Layout: l, Webroot: w})
	}
	noEmail := sslCert(t, LetsEncryptStaging)
	noEmail.Email = validate.Email{}
	stepsBad = append(stepsBad, noEmail)
	if dash, err := validate.ParseEmail("-n@example.com"); err == nil {
		c := sslCert(t, LetsEncryptStaging)
		c.Email = dash
		stepsBad = append(stepsBad, c)
	}
	for _, u := range []string{
		"", "example.com", "http://example.com/", "https://other.com", "https://example.com; rm -rf /",
		"$(id)", "--url=https://example.com", "https://example.com\n", "javascript:alert(1)",
		"https://example.com/../../etc/passwd", "https://" + strings.Repeat("a", 300) + ".com",
	} {
		stepsBad = append(stepsBad, &WPSiteURL{Layout: l, URL: u})
	}

	for _, s := range stepsBad {
		m := sslProbeMachine(t)
		if _, err := s.Check(ctx, m); err == nil {
			t.Errorf("%T %+v: Check không lỗi", s, s)
		}
		if err := s.Apply(ctx, m); err == nil {
			t.Errorf("%T %+v: Apply không lỗi", s, s)
		}
		if len(m.Ran()) > 0 {
			t.Errorf("%T %+v: tham số sai mà vẫn chạy lệnh %q", s, s, ranStrings(m.Ran()))
		}
	}

	for _, srv := range []string{LetsEncryptProd, LetsEncryptStaging, "https://localhost:14000/dir", "https://127.0.0.1:14000/dir"} {
		if err := sslCert(t, srv).validate(); err != nil {
			t.Errorf("%q bị từ chối: %v", srv, err)
		}
	}

	if _, err := SiteVhostSSL(l, "caddy", "8.3", sslWebroot, site.HtaccessSafe, site.Names{}); err == nil {
		t.Error("SiteVhostSSL nhận web server lạ")
	}
	if _, err := SiteVhostSSL(tampered, "nginx", "8.3", sslWebroot, site.HtaccessSafe, site.Names{}); err == nil {
		t.Error("SiteVhostSSL nhận Layout sửa tay")
	}
	if _, err := DefaultSSL("caddy"); err == nil {
		t.Error("DefaultSSL nhận web server lạ")
	}
}

func TestSSLInfo(t *testing.T) {
	l := sslLayout(t)
	for _, s := range []engine.Step{
		sslCert(t, LetsEncryptStaging), &ACMEProbe{Layout: l, Webroot: sslWebroot},
		&CertbotDelete{Layout: l}, &WPSiteURL{Layout: l, URL: "https://example.com"},
	} {
		i := s.Info()
		if i.ID == "" || i.Title.IsZero() || i.Why.IsZero() || len(i.Commands) == 0 {
			t.Errorf("%T Info thiếu: %+v", s, i)
		}
	}
	if i := sslCert(t, LetsEncryptStaging).Info(); i.NoUndo.IsZero() || i.Danger {
		t.Errorf("CertbotCert.Info = %+v", i)
	}
	if i := (&CertbotDelete{Layout: l}).Info(); !i.Danger || i.NoUndo.IsZero() {
		t.Errorf("CertbotDelete.Info = %+v", i)
	}
	if i := (&ACMEProbe{Layout: l, Webroot: sslWebroot}).Info(); i.NoUndo.IsZero() {
		t.Errorf("ACMEProbe.Info = %+v", i)
	}
	want := "runuser -u " + l.User + " -- wp option update home https://example.com --skip-plugins --skip-themes --quiet --path=" + l.Public
	if i := (&WPSiteURL{Layout: l, URL: "https://example.com"}).Info(); !slices.Contains(i.Commands, want) {
		t.Errorf("WPSiteURL.Commands = %q, muốn có %q", i.Commands, want)
	}
}

// ======================================================= Vhost SSL, mặc định 443

func TestSiteVhostSSL(t *testing.T) {
	l := sslLayout(t)
	apTest := &system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
	apReload := &system.Cmd{Name: "systemctl", Args: []string{"reload", "apache2"}}
	for web, w := range map[string]struct {
		conf, link   string
		test, reload *system.Cmd
	}{
		"nginx":  {l.NginxConf, l.NginxLink, nginxTest, nginxReload},
		"apache": {l.ApacheConf, l.ApacheLink, apTest, apReload},
	} {
		want, err := templates.SiteVhost(l, web, "8.3", sslWebroot, true, site.HtaccessSafe)
		if err != nil {
			t.Fatalf("templates.SiteVhost %s: %v", web, err)
		}
		ss, err := SiteVhostSSL(l, web, "8.3", sslWebroot, site.HtaccessSafe, site.Names{})
		if err != nil || len(ss) != 2 {
			t.Fatalf("SiteVhostSSL %s = %v, %v", web, ss, err)
		}
		f, ok1 := ss[0].(*File)
		sl, ok2 := ss[1].(*Symlink)
		if !ok1 || !ok2 {
			t.Fatalf("%s: kiểu bước %T %T", web, ss[0], ss[1])
		}
		if f.Path != w.conf || string(f.Content) != string(want) || f.Perm != 0o644 || f.Owner != system.Root ||
			!reflect.DeepEqual(f.Test, w.test) || !reflect.DeepEqual(f.Reload, w.reload) {
			t.Errorf("%s: File sai: %+v", web, f)
		}
		if sl.Target != w.conf || sl.Link != w.link || !reflect.DeepEqual(sl.Test, w.test) || !reflect.DeepEqual(sl.Reload, w.reload) {
			t.Errorf("%s: Symlink sai: %+v", web, sl)
		}
	}

	// Ghi đè vhost cổng 80 đang có: Undo trả lại đúng bản cổng 80.
	port80 := func(m *machine) {
		webBase(m, l)
		execute(t, m, webStepsOK(t)(SiteVhost(l, "nginx", "8.3", sslWebroot, site.HtaccessSafe, site.Names{}))...)
	}
	lc := lifecycle{
		name:  "SiteVhostSSL nginx ghi đè cổng 80",
		setup: port80,
		step: func() engine.Step {
			return webStepsOK(t)(SiteVhostSSL(l, "nginx", "8.3", sslWebroot, site.HtaccessSafe, site.Names{}))[0]
		},
	}
	t.Run(lc.name, lc.run)
}

func TestDefaultSSL(t *testing.T) {
	ng := webStepsOK(t)(DefaultSSL("nginx"))
	ngConf, err := templates.NginxDefaultSSL()
	if err != nil {
		t.Fatalf("templates.NginxDefaultSSL: %v", err)
	}
	if len(ng) != 2 {
		t.Fatalf("nginx: %d bước", len(ng))
	}
	if f := ng[0].(*File); f.Path != "/etc/nginx/sites-available/tpan-default-ssl.conf" || string(f.Content) != string(ngConf) ||
		!reflect.DeepEqual(f.Test, nginxTest) || !reflect.DeepEqual(f.Reload, nginxReload) {
		t.Errorf("nginx File sai: %+v", f)
	}
	if s := ng[1].(*Symlink); s.Target != "/etc/nginx/sites-available/tpan-default-ssl.conf" ||
		s.Link != "/etc/nginx/sites-enabled/tpan-default-ssl.conf" || !reflect.DeepEqual(s.Reload, nginxReload) {
		t.Errorf("nginx Symlink sai: %+v", s)
	}

	ap := webStepsOK(t)(DefaultSSL("apache"))
	apConf, err := templates.ApacheDefaultSSL()
	if err != nil {
		t.Fatalf("templates.ApacheDefaultSSL: %v", err)
	}
	if len(ap) != 4 {
		t.Fatalf("apache: %d bước", len(ap))
	}
	if a := ap[0].(*AptInstall); !slices.Equal(a.Packages, []string{"ssl-cert"}) {
		t.Errorf("apache bước 1: %+v", a)
	}
	if a := ap[1].(*ApacheModules); !slices.Equal(a.Enable, []string{"ssl"}) || len(a.Disable) > 0 {
		t.Errorf("apache bước 2: %+v", a)
	}
	apTest := &system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
	if f := ap[2].(*File); f.Path != "/etc/apache2/sites-available/000-tpan-default-ssl.conf" || string(f.Content) != string(apConf) ||
		!reflect.DeepEqual(f.Test, apTest) {
		t.Errorf("apache File sai: %+v", f)
	}
	if s := ap[3].(*Symlink); s.Target != "/etc/apache2/sites-available/000-tpan-default-ssl.conf" ||
		s.Link != "/etc/apache2/sites-enabled/000-tpan-default-ssl.conf" {
		t.Errorf("apache Symlink sai: %+v", s)
	}

	base := func(m *machine) {
		m.SetDir("/etc/nginx/sites-available", 0o755, system.Root)
		m.SetDir("/etc/nginx/sites-enabled", 0o755, system.Root)
	}
	for _, lc := range []lifecycle{
		{name: "DefaultSSL nginx file", setup: base, step: func() engine.Step { return webStepsOK(t)(DefaultSSL("nginx"))[0] }},
		{name: "DefaultSSL nginx link", setup: func(m *machine) { base(m); execute(t, m, webStepsOK(t)(DefaultSSL("nginx"))[0]) },
			step: func() engine.Step { return webStepsOK(t)(DefaultSSL("nginx"))[1] }},
	} {
		t.Run(lc.name, lc.run)
	}
}

// ================================================ Chứng chỉ nhiều tên (M19 3a)

func sslNames(t *testing.T, names ...string) []validate.Domain {
	t.Helper()
	out := make([]validate.Domain, len(names))
	for i, n := range names {
		d, err := validate.ParseDomain(n)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = d
	}
	return out
}

// sslPEM sinh một chứng chỉ tự ký có SAN là names, như cert.pem của certbot.
func sslPEM(names ...string) ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tpan test"},
		DNSNames:     names,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func sslCertPath(d string) string { return "/etc/letsencrypt/live/" + d + "/cert.pem" }

// sslSetSAN đặt chứng chỉ của d như sslSetCert, thêm cert.pem có SAN là names.
func sslSetSAN(t *testing.T, m *machine, d, server string, names ...string) {
	t.Helper()
	sslSetCert(m, d, server)
	data, err := sslPEM(names...)
	if err != nil {
		t.Fatal(err)
	}
	m.SetFile(sslCertPath(d), data, 0o644, system.Root)
}

// sslCertbotNames giả certbot nhiều tên: certonly ghi chứng chỉ của
// --cert-name với SAN là mọi -d, như bản thử C2 của thiết kế M19.
func sslCertbotNames(m *machine) *[][]string {
	var mu sync.Mutex
	var calls [][]string
	m.Handle([]string{"certbot", "certonly"}, func(c system.Cmd) system.Result {
		mu.Lock()
		calls = append(calls, slices.Clone(c.Args))
		mu.Unlock()
		var names []string
		for i, a := range c.Args {
			if a == "-d" && i+1 < len(c.Args) {
				names = append(names, c.Args[i+1])
			}
		}
		d := c.Args[slices.Index(c.Args, "--cert-name")+1]
		data, err := sslPEM(names...)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: err.Error()}
		}
		sslSetCert(m, d, c.Args[slices.Index(c.Args, "--server")+1])
		m.SetFile(sslCertPath(d), data, 0o644, system.Root)
		return system.Result{Stdout: "Successfully received certificate.\n"}
	})
	return &calls
}

// sslNamesProbeMachine như sslProbeMachine nhưng curl đọc host từ URL, và
// các tên trong bad không tới được máy này (curl lỗi kết nối).
func sslNamesProbeMachine(t *testing.T, bad ...string) *machine {
	m := sslProbeMachine(t)
	m.Handle([]string{"curl", "-sS", "-m", "10"}, func(c system.Cmd) system.Result {
		host, name, _ := strings.Cut(strings.TrimPrefix(c.Args[len(c.Args)-1], "http://"), "/.well-known/acme-challenge/")
		if slices.Contains(bad, host) {
			return system.Result{ExitCode: 7, Stderr: "curl: (7) Failed to connect to " + host + " port 80"}
		}
		data, err := m.ReadFile(sslChallengeDir(sslWebroot) + "/" + name)
		if err != nil {
			return system.Result{Stdout: "<html><body>404 Not Found</body></html>\n"}
		}
		return system.Result{Stdout: string(data)}
	})
	return m
}

// sslCurlHosts là các host mà curl đã gọi, theo thứ tự.
func sslCurlHosts(t *testing.T, m *machine) []string {
	t.Helper()
	var out []string
	for _, c := range sslCurls(m) {
		if slices.ContainsFunc(c.Args, func(x string) bool { return strings.Contains(x, "resolve") }) {
			t.Errorf("curl không được có --resolve: %s", c.String())
		}
		host, _, _ := strings.Cut(strings.TrimPrefix(c.Args[len(c.Args)-1], "http://"), "/")
		out = append(out, host)
	}
	return out
}

func sslCertbotCalls(m *machine) int {
	n := 0
	for _, c := range m.Ran() {
		if c.Name == "certbot" {
			n++
		}
	}
	return n
}

// sslNamesPlan nối bước thử và bước xin như core sẽ nối (mục 3.6 của thiết
// kế M19): Names của certbot đọc kết quả thử, Probe cho S3.
func sslNamesPlan(t *testing.T, names, covered []validate.Domain) (*ACMEProbeNames, *CertbotCert) {
	cert := sslCert(t, LetsEncryptStaging)
	probe := &ACMEProbeNames{Layout: cert.Layout, Webroot: sslWebroot, Names: names, Covered: covered,
		Result: &ProbeResult{}, Cert: cert}
	cert.Names = probe.CertbotNames
	cert.Probe = probe.Result
	return probe, cert
}

func TestCertbotCertNames(t *testing.T) {
	ctx := context.Background()
	base := []string{"certonly", "--webroot", "-w", sslWebroot, "-d", "example.com", "--cert-name", "example.com",
		"--non-interactive", "--agree-tos", "-m", "admin@example.com", "--server", LetsEncryptStaging}

	t.Run("tên chính đứng đầu, không lặp, tên phụ giữ thứ tự", func(t *testing.T) {
		m := newMachine(t)
		calls := sslCertbotNames(m)
		c := sslCert(t, LetsEncryptStaging)
		extra := sslNames(t, "www.example.com", "example.com", "www.example.com", "alias.example.net")
		c.Names = func() []validate.Domain { return extra }
		if err := c.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		want := []string{"certonly", "--webroot", "-w", sslWebroot,
			"-d", "example.com", "-d", "www.example.com", "-d", "alias.example.net", "--cert-name", "example.com",
			"--non-interactive", "--agree-tos", "-m", "admin@example.com", "--server", LetsEncryptStaging}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("argv = %q, muốn %q", *calls, want)
		}
		if got := c.Info().Commands; len(got) != 1 || !strings.Contains(got[0], "-d example.com -d www.example.com -d alias.example.net --cert-name example.com") {
			t.Errorf("Info.Commands = %q", got)
		}
		if done, err := c.Check(ctx, m); err != nil || !done {
			t.Errorf("Check sau Apply = %v, %v", done, err)
		}
	})

	t.Run("Names trả rỗng hay chỉ tên chính thì argv như trước M19", func(t *testing.T) {
		for _, names := range [][]validate.Domain{nil, sslNames(t, "example.com")} {
			m := newMachine(t)
			calls := sslCertbotNames(m)
			c := sslCert(t, LetsEncryptStaging)
			c.Names = func() []validate.Domain { return names }
			if err := c.Apply(ctx, m); err != nil {
				t.Fatal(err)
			}
			if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], base) {
				t.Errorf("Names %v: argv = %q, muốn %q", names, *calls, base)
			}
		}
	})

	t.Run("quá trần MaxNames thì không chạy lệnh", func(t *testing.T) {
		var many []string
		for i := range site.MaxNames {
			many = append(many, fmt.Sprintf("a%d.example.com", i))
		}
		over := sslNames(t, many...)
		m := newMachine(t)
		c := sslCert(t, LetsEncryptStaging)
		c.Names = func() []validate.Domain { return over }
		if _, err := c.Check(ctx, m); err == nil || !strings.Contains(err.Error(), "quá trần") {
			t.Errorf("Check: lỗi = %v", err)
		}
		if err := c.Apply(ctx, m); err == nil {
			t.Error("Apply không lỗi")
		}
		if len(m.Ran()) > 0 {
			t.Errorf("chạy lệnh: %q", ranStrings(m.Ran()))
		}
		// Đúng trần (tên chính + 24 tên phụ) thì nhận.
		atCap := over[:site.MaxNames-1]
		c.Names = func() []validate.Domain { return atCap }
		if err := c.validate(); err != nil {
			t.Errorf("đúng trần bị từ chối: %v", err)
		}
	})

	t.Run("Info có Names thì nói tên hỏng bị bỏ khỏi lệnh", func(t *testing.T) {
		c := sslCert(t, LetsEncryptStaging)
		if strings.Contains(c.Info().Why.Vi, "bị bỏ khỏi lệnh") {
			t.Error("Names nil mà Why vẫn nói về bước thử")
		}
		c.Names = func() []validate.Domain { return nil }
		if w := c.Info().Why; !strings.Contains(w.Vi, "bị bỏ khỏi lệnh") || !strings.Contains(w.En, "dropped from the command") {
			t.Errorf("Why = %+v", w)
		}
	})

	lc := lifecycle{
		name:  "CertbotCert nhiều tên",
		setup: func(m *machine) { sslCertbotNames(m) },
		step: func() engine.Step {
			c := sslCert(t, LetsEncryptStaging)
			names := sslNames(t, "www.example.com", "alias.example.net")
			c.Names = func() []validate.Domain { return names }
			return c
		},
		noUndo: true,
	}
	t.Run(lc.name, lc.run)
}

func TestCertbotCertCheckSAN(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		server string   // máy chủ ghi trong renewal
		san    []string // SAN của cert.pem
		names  []string // Names (nil: không đặt Names)
		want   bool
	}{
		{"đủ tên, khác thứ tự", LetsEncryptStaging, []string{"www.example.com", "example.com"}, []string{"www.example.com"}, true},
		{"chứng chỉ thiếu tên", LetsEncryptStaging, []string{"example.com"}, []string{"www.example.com"}, false},
		{"chứng chỉ thừa tên (tên đã gỡ)", LetsEncryptStaging, []string{"example.com", "www.example.com", "old.example.net"}, []string{"www.example.com"}, false},
		{"đủ tên nhưng khác máy chủ ACME", LetsEncryptProd, []string{"example.com", "www.example.com"}, []string{"www.example.com"}, false},
		{"Names nil không đọc SAN (như trước M19)", LetsEncryptStaging, []string{"example.com", "www.example.com"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			sslSetSAN(t, m, "example.com", tc.server, tc.san...)
			c := sslCert(t, LetsEncryptStaging)
			if tc.names != nil {
				names := sslNames(t, tc.names...)
				c.Names = func() []validate.Domain { return names }
			}
			if done, err := c.Check(ctx, m); err != nil || done != tc.want {
				t.Errorf("Check = %v, %v; muốn %v, nil", done, err, tc.want)
			}
			if len(m.Ran()) > 0 {
				t.Errorf("Check chạy lệnh: %q", ranStrings(m.Ran()))
			}
		})
	}

	names := sslNames(t, "www.example.com")
	t.Run("có fullchain mà thiếu cert.pem thì chưa xong", func(t *testing.T) {
		m := newMachine(t)
		sslSetCert(m, "example.com", LetsEncryptStaging)
		c := sslCert(t, LetsEncryptStaging)
		c.Names = func() []validate.Domain { return names }
		if done, err := c.Check(ctx, m); err != nil || done {
			t.Errorf("Check = %v, %v; muốn false, nil", done, err)
		}
	})

	t.Run("cert.pem hỏng thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := newMachine(t)
		sslSetCert(m, "example.com", LetsEncryptStaging)
		m.SetFile(sslCertPath("example.com"), []byte("không phải PEM\n"), 0o644, system.Root)
		c := sslCert(t, LetsEncryptStaging)
		c.Names = func() []validate.Domain { return names }
		if _, err := c.Check(ctx, m); err == nil || !strings.Contains(err.Error(), "đọc tên trong "+sslCertPath("example.com")) {
			t.Errorf("lỗi = %v", err)
		}
	})
}

func TestACMEProbeNames(t *testing.T) {
	ctx := context.Background()
	l := sslLayout(t)
	extra := sslNames(t, "www.example.com", "alias.example.net")
	all := sslNames(t, "example.com", "www.example.com", "alias.example.net")

	t.Run("mọi tên qua", func(t *testing.T) {
		m := sslNamesProbeMachine(t)
		before := m.Snapshot()
		r := &ProbeResult{}
		s := &ACMEProbeNames{Layout: l, Webroot: sslWebroot, Names: extra, Result: r}
		if got := s.CertbotNames(); !slices.Equal(got, all) {
			t.Errorf("CertbotNames trước Apply = %v, muốn mọi tên %v", got, all)
		}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v", done, err)
		}
		if !slices.Equal(r.Passed, all) || len(r.Failed) != 0 || r.Blocked {
			t.Errorf("Result = %+v", *r)
		}
		if got := s.CertbotNames(); !slices.Equal(got, r.Passed) {
			t.Errorf("CertbotNames = %v, muốn %v", got, r.Passed)
		}
		if got := sslCurlHosts(t, m); !slices.Equal(got, []string{"example.com", "www.example.com", "alias.example.net"}) {
			t.Errorf("curl gọi %q", got)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("Apply để lại file thử hoặc thư mục")
		}
	})

	for _, covered := range []bool{false, true} {
		name := "tên phụ hỏng, chưa nằm trong chứng chỉ: bỏ khỏi đơn"
		var cov []validate.Domain
		if covered {
			name = "tên phụ hỏng, đang nằm trong chứng chỉ: Blocked"
			cov = sslNames(t, "example.com", "alias.example.net")
		}
		t.Run(name, func(t *testing.T) {
			m := sslNamesProbeMachine(t, "alias.example.net")
			before := m.Snapshot()
			r := &ProbeResult{}
			s := &ACMEProbeNames{Layout: l, Webroot: sslWebroot, Names: extra, Covered: cov, Result: r}
			if err := s.Apply(ctx, m); err != nil {
				t.Fatalf("tên phụ hỏng mà bước hỏng: %v", err)
			}
			if !slices.Equal(r.Passed, all[:2]) || r.Blocked != covered || len(r.Failed) != 1 {
				t.Fatalf("Result = %+v", *r)
			}
			f := r.Failed[0]
			if f.Name.String() != "alias.example.net" || f.Covered != covered ||
				!strings.Contains(f.Reason.Vi, "Failed to connect") || !strings.Contains(f.Reason.En, "alias.example.net") {
				t.Errorf("Failed = %+v", f)
			}
			if got := s.CertbotNames(); !slices.Equal(got, all[:2]) {
				t.Errorf("CertbotNames = %v", got)
			}
			if !reflect.DeepEqual(before, m.Snapshot()) {
				t.Error("Apply để lại file thử hoặc thư mục")
			}
		})
	}

	t.Run("tên chính hỏng thì bước hỏng, không thử tiếp", func(t *testing.T) {
		m := sslNamesProbeMachine(t, "example.com")
		before := m.Snapshot()
		r := &ProbeResult{}
		s := &ACMEProbeNames{Layout: l, Webroot: sslWebroot, Names: extra, Result: r}
		err := s.Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "Failed to connect to example.com") {
			t.Errorf("lỗi = %v", err)
		}
		if !reflect.DeepEqual(*r, ProbeResult{}) {
			t.Errorf("bước hỏng mà vẫn ghi Result: %+v", *r)
		}
		if got := sslCurlHosts(t, m); !slices.Equal(got, []string{"example.com"}) {
			t.Errorf("curl gọi %q", got)
		}
		if done, _ := s.Check(ctx, m); done {
			t.Error("Apply lỗi mà Check vẫn true")
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("Apply lỗi mà còn để lại file thử hoặc thư mục")
		}
	})

	t.Run("tham số sai thì không chạy lệnh", func(t *testing.T) {
		var many []string
		for i := range site.MaxNames {
			many = append(many, fmt.Sprintf("a%d.example.com", i))
		}
		other := sslCert(t, LetsEncryptStaging)
		other.Layout = site.LayoutOf(sslNames(t, "other.example.com")[0])
		badServer := sslCert(t, "https://a.com/dir; rm -rf /")
		for name, s := range map[string]*ACMEProbeNames{
			"thiếu Result":       {Layout: l, Webroot: sslWebroot, Names: extra},
			"quá trần":           {Layout: l, Webroot: sslWebroot, Names: sslNames(t, many...), Result: &ProbeResult{}},
			"webroot sai":        {Layout: l, Webroot: "/var/lib/tpan/../../etc", Names: extra, Result: &ProbeResult{}},
			"Layout rỗng":        {Webroot: sslWebroot, Names: extra, Result: &ProbeResult{}},
			"Cert của site khác": {Layout: l, Webroot: sslWebroot, Names: extra, Result: &ProbeResult{}, Cert: other},
			"Cert tham số sai":   {Layout: l, Webroot: sslWebroot, Names: extra, Result: &ProbeResult{}, Cert: badServer},
		} {
			m := sslNamesProbeMachine(t)
			if _, err := s.Check(ctx, m); err == nil {
				t.Errorf("%s: Check không lỗi", name)
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Errorf("%s: Apply không lỗi", name)
			}
			if len(m.Ran()) > 0 {
				t.Errorf("%s: chạy lệnh %q", name, ranStrings(m.Ran()))
			}
		}
	})

	t.Run("Info", func(t *testing.T) {
		i := (&ACMEProbeNames{Layout: l, Webroot: sslWebroot, Names: extra, Result: &ProbeResult{}}).Info()
		if i.ID != "ssl.probe:example.com" || i.Title.IsZero() || i.Why.IsZero() || i.NoUndo.IsZero() || i.Danger || len(i.Commands) != 3 {
			t.Errorf("Info = %+v", i)
		}
		if !strings.Contains(i.Commands[2], "http://alias.example.net/.well-known/acme-challenge/") {
			t.Errorf("Commands = %q", i.Commands)
		}
	})
}

// S3 của thiết kế M19: tên đang nằm trong chứng chỉ mà hỏng bước thử thì không
// xin lại (xin lại thì hoặc certbot hỏng, hoặc chứng chỉ mất tên đó).
func TestCertbotKeepsCoveredNameOnProbeFail(t *testing.T) {
	want := sslNames(t, "www.example.com", "new.example.net")
	covered := sslNames(t, "example.com", "www.example.com")

	t.Run("tên đang có trong chứng chỉ hỏng: giữ chứng chỉ cũ", func(t *testing.T) {
		m := sslNamesProbeMachine(t, "www.example.com")
		sslCertbotNames(m)
		sslSetSAN(t, m, "example.com", LetsEncryptStaging, "example.com", "www.example.com")
		before := m.Snapshot()
		probe, cert := sslNamesPlan(t, want, covered)
		if rep := execute(t, m, probe, cert); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
		}
		if n := sslCertbotCalls(m); n != 0 {
			t.Errorf("certbot chạy %d lần, muốn 0", n)
		}
		r := probe.Result
		if !r.Blocked || len(r.Failed) != 1 || r.Failed[0].Name.String() != "www.example.com" || !r.Failed[0].Covered {
			t.Errorf("Result = %+v", *r)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("chứng chỉ cũ bị đổi")
		}
	})

	t.Run("tên mới hỏng, chứng chỉ đã đủ tên đã qua: không xin", func(t *testing.T) {
		m := sslNamesProbeMachine(t, "new.example.net")
		sslCertbotNames(m)
		sslSetSAN(t, m, "example.com", LetsEncryptStaging, "example.com", "www.example.com")
		probe, cert := sslNamesPlan(t, want, covered)
		if rep := execute(t, m, probe, cert); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
		}
		if n := sslCertbotCalls(m); n != 0 || probe.Result.Blocked {
			t.Errorf("certbot chạy %d lần, Blocked %v; muốn 0, false", n, probe.Result.Blocked)
		}
	})

	t.Run("bị chặn mà chưa có chứng chỉ thì vẫn xin với tên đã qua", func(t *testing.T) {
		m := sslNamesProbeMachine(t, "www.example.com")
		calls := sslCertbotNames(m)
		probe, cert := sslNamesPlan(t, want, covered)
		if rep := execute(t, m, probe, cert); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
		}
		if !probe.Result.Blocked || len(*calls) != 1 {
			t.Fatalf("Blocked %v, certbot %q", probe.Result.Blocked, *calls)
		}
		if a := strings.Join((*calls)[0], " "); !strings.Contains(a, "-d example.com -d new.example.net --cert-name") {
			t.Errorf("argv = %s", a)
		}
	})
}

// Plan thử rồi xin chạy lại: bỏ qua khi chứng chỉ đã đúng; thêm tên thì xin lại.
func TestACMEProbeNamesPlanRerun(t *testing.T) {
	extra := sslNames(t, "www.example.com", "alias.example.net")

	t.Run("mọi tên qua, chạy lại thì bỏ qua cả hai, không lệnh nào", func(t *testing.T) {
		m := sslNamesProbeMachine(t)
		calls := sslCertbotNames(m)
		probe, cert := sslNamesPlan(t, extra, nil)
		if rep := execute(t, m, probe, cert); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		if a := strings.Join((*calls)[0], " "); len(*calls) != 1 ||
			!strings.Contains(a, "-d example.com -d www.example.com -d alias.example.net --cert-name example.com") {
			t.Fatalf("certbot %q", *calls)
		}
		m.ResetRan()
		probe, cert = sslNamesPlan(t, extra, nil)
		rep := execute(t, m, probe, cert)
		for _, st := range rep.Steps {
			if st.Outcome != "skipped" {
				t.Errorf("lần hai: %s outcome %q", st.ID, st.Outcome)
			}
		}
		if len(m.Ran()) > 0 {
			t.Errorf("lần hai chạy lệnh: %q", ranStrings(m.Ran()))
		}
		if !slices.Equal(probe.Result.Passed, sslNames(t, "example.com", "www.example.com", "alias.example.net")) {
			t.Errorf("bỏ qua mà Result = %+v", *probe.Result)
		}
	})

	t.Run("tên phụ hỏng: xin không có nó, chạy lại thử lại nhưng không xin", func(t *testing.T) {
		m := sslNamesProbeMachine(t, "alias.example.net")
		calls := sslCertbotNames(m)
		probe, cert := sslNamesPlan(t, extra, nil)
		if rep := execute(t, m, probe, cert); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		if a := strings.Join((*calls)[0], " "); len(*calls) != 1 || strings.Contains(a, "alias") ||
			!strings.Contains(a, "-d example.com -d www.example.com --cert-name") {
			t.Fatalf("certbot %q", *calls)
		}
		probe, cert = sslNamesPlan(t, extra, sslNames(t, "example.com", "www.example.com"))
		if rep := execute(t, m, probe, cert); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần hai: %s %s", rep.Job.Status, rep.Job.Error)
		}
		if len(*calls) != 1 {
			t.Errorf("lần hai xin lại: %q", *calls)
		}
	})

	t.Run("thêm tên thì xin lại cùng --cert-name, không ép gia hạn", func(t *testing.T) {
		m := sslNamesProbeMachine(t)
		calls := sslCertbotNames(m)
		sslSetSAN(t, m, "example.com", LetsEncryptStaging, "example.com", "www.example.com")
		probe, cert := sslNamesPlan(t, extra, sslNames(t, "example.com", "www.example.com"))
		if rep := execute(t, m, probe, cert); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
		}
		if len(*calls) != 1 || slices.Contains((*calls)[0], "--force-renewal") ||
			!strings.Contains(strings.Join((*calls)[0], " "), "-d alias.example.net --cert-name example.com") {
			t.Errorf("certbot %q", *calls)
		}
	})
}

func TestCertNames(t *testing.T) {
	mk := func(names ...string) []byte {
		t.Helper()
		b, err := sslPEM(names...)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")})

	for _, tc := range []struct {
		name string
		data []byte
		want []string
	}{
		{"hai tên", mk("example.com", "www.example.com"), []string{"example.com", "www.example.com"}},
		{"khối khác đứng trước thì bỏ qua", append(slices.Clone(key), mk("example.com")...), []string{"example.com"}},
		{"fullchain: chỉ đọc chứng chỉ đầu", append(mk("example.com"), mk("other.example.org")...), []string{"example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CertNames(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, sslNames(t, tc.want...)) {
				t.Errorf("= %v, muốn %v", got, tc.want)
			}
		})
	}

	for name, data := range map[string][]byte{
		"rỗng":           nil,
		"không phải PEM": []byte("example.com\n"),
		"chỉ có khoá":    key,
		"DER hỏng":       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")}),
		"tên đại diện":   mk("example.com", "*.example.com"),
		"tên có ;":       mk("example.com", "a.com;id"),
	} {
		if got, err := CertNames(data); err == nil {
			t.Errorf("%s: không lỗi, tên %v", name, got)
		}
	}
}
