package checks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func names(ns ...string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) { return ns, nil }
}

func listErr(context.Context) ([]string, error) { return nil, errors.New("store: M20 chưa viết") }

func enabled(on bool) func(context.Context) (bool, error) {
	return func(context.Context) (bool, error) { return on, nil }
}

func enabledErr(context.Context) (bool, error) { return false, errors.New("đĩa hỏng") }

func TestAdminsWithout2FA(t *testing.T) {
	f := system.NewFake()
	r := AdminsWithout2FA(names()).Run(bg(), f)
	assertShape(t, r, "tpan.2fa:admins", StageTPAN, Pass)

	r = AdminsWithout2FA(names("admin", "an\nna")).Run(bg(), f)
	assertShape(t, r, "tpan.2fa:admins", StageTPAN, Warn)
	if !strings.Contains(r.Summary.Vi, "admin, an na") || !strings.Contains(r.Cause.Vi, "trang Tài khoản") {
		t.Errorf("Warn phải liệt kê tên đã làm sạch và cách bật: %q / %q", r.Summary.Vi, r.Cause.Vi)
	}
	if lastFix(r) != "tpan user 2fa status" {
		t.Errorf("dòng cuối = %q", lastFix(r))
	}

	// Nhiều tên: Summary cắt ở 5, nói số còn lại.
	r = AdminsWithout2FA(names("a1", "a2", "a3", "a4", "a5", "a6", "a7")).Run(bg(), f)
	if !strings.Contains(r.Summary.Vi, "và 2 người nữa") || strings.Contains(r.Summary.Vi, "a6") || !strings.Contains(r.Evidence[0].Output, "a7") {
		t.Errorf("Summary = %q, Evidence = %q", r.Summary.Vi, r.Evidence[0].Output)
	}

	assertShape(t, AdminsWithout2FA(listErr).Run(bg(), f), "tpan.2fa:admins", StageTPAN, Unknown)
	assertShape(t, AdminsWithout2FA(nil).Run(bg(), f), "tpan.2fa:admins", StageTPAN, Unknown)
	assertNoCmd(t, f)
}

func TestTwoFAAttempts(t *testing.T) {
	f := system.NewFake()
	assertShape(t, TwoFAAttempts(names()).Run(bg(), f), "tpan.2fa:attempts", StageTPAN, Pass)
	r := TwoFAAttempts(names("binh")).Run(bg(), f)
	assertShape(t, r, "tpan.2fa:attempts", StageTPAN, Warn)
	want := "Có người nhập sai mã hai bước nhiều lần cho binh: mật khẩu của họ có thể đã lộ. Đổi mật khẩu."
	if r.Summary.Vi != want {
		t.Errorf("Summary = %q", r.Summary.Vi)
	}
	assertShape(t, TwoFAAttempts(listErr).Run(bg(), f), "tpan.2fa:attempts", StageTPAN, Unknown)
	assertNoCmd(t, f)
}

func TestClock(t *testing.T) {
	cases := []struct {
		name string
		out  string
		code int
		on   func(context.Context) (bool, error)
		want Status
	}{
		{"đồng bộ", "yes\n", 0, enabled(true), Pass},
		{"lệch, có người bật", "no\n", 0, enabled(true), Warn},
		{"output rỗng", "", 0, enabled(true), Unknown},
		{"output lạ", "maybe\n", 0, enabled(true), Unknown},
		{"mã lỗi", "", 1, enabled(true), Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			reply(f, []string{"timedatectl"}, c.code, c.out, "")
			r := Clock(c.on).Run(bg(), f)
			assertShape(t, r, "tpan.clock", StageTPAN, c.want)
			assertEvidenceIsRan(t, r, f)
			if ran := f.Ran(); len(ran) != 1 || ran[0].String() != "timedatectl show -p NTPSynchronized --value" {
				t.Errorf("lệnh = %v", ran)
			}
			if c.want == Warn && lastFix(r) != "timedatectl show -p NTPSynchronized --value" {
				t.Errorf("dòng cuối = %q", lastFix(r))
			}
		})
	}
	// Không chạy được lệnh (không có timedatectl): Unknown.
	assertShape(t, Clock(enabled(true)).Run(cancelledCtx(), system.NewFake()), "tpan.clock", StageTPAN, Unknown)
	// Chưa ai bật: Pass mà không chạy lệnh; DB lỗi hay chưa nối: Unknown, cũng không chạy lệnh.
	for _, c := range []struct {
		on   func(context.Context) (bool, error)
		want Status
	}{{enabled(false), Pass}, {enabledErr, Unknown}, {nil, Unknown}} {
		f := system.NewFake()
		assertShape(t, Clock(c.on).Run(bg(), f), "tpan.clock", StageTPAN, c.want)
		assertNoCmd(t, f)
	}
}

func TestSecretKey(t *testing.T) {
	const p = "/etc/tpan/secret.key"
	key := strings.Repeat("k", 32)
	cases := []struct {
		name  string
		setup func(f *system.FakeSystem)
		on    func(context.Context) (bool, error)
		want  Status
	}{
		{"đúng", func(f *system.FakeSystem) { f.SetFile(p, []byte(key), 0o600, system.Root) }, enabled(true), Pass},
		{"chưa ai bật, không có file", func(*system.FakeSystem) {}, enabled(false), Pass},
		{"mất file", func(*system.FakeSystem) {}, enabled(true), Fail},
		{"quyền rộng", func(f *system.FakeSystem) { f.SetFile(p, []byte(key), 0o644, system.Root) }, enabled(true), Fail},
		{"chủ khác", func(f *system.FakeSystem) {
			f.SetFile(p, []byte(key), 0o600, system.Owner{User: "www-data", Group: "www-data"})
		}, enabled(true), Fail},
		{"cỡ sai", func(f *system.FakeSystem) { f.SetFile(p, []byte("ngan"), 0o600, system.Root) }, enabled(true), Fail},
		{"symlink", func(f *system.FakeSystem) {
			f.SetFile("/tmp/k", []byte(key), 0o600, system.Root)
			f.SetSymlink("/tmp/k", p)
		}, enabled(true), Fail},
		{"thư mục", func(f *system.FakeSystem) { f.SetDir(p, 0o600, system.Root) }, enabled(true), Fail},
		{"DB lỗi", func(*system.FakeSystem) {}, enabledErr, Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			c.setup(f)
			// Phép kiểm chỉ được Stat: mọi lần đọc nội dung bị ghi lại là lỗi.
			r := SecretKey(p, c.on).Run(bg(), &statOnly{FakeSystem: f, t: t})
			assertShape(t, r, "tpan.secretkey", StageTPAN, c.want)
			assertNoCmd(t, f)
			for _, ev := range r.Evidence {
				if strings.Contains(ev.Output, key) {
					t.Errorf("bằng chứng lộ nội dung khoá: %q", ev.Output)
				}
			}
			if c.want == Fail && lastFix(r) != "ls -l "+p {
				t.Errorf("dòng cuối = %q", lastFix(r))
			}
		})
	}
	// Stat lỗi khác "không có": Unknown.
	sys := &statOnly{FakeSystem: system.NewFake(), t: t, statErr: fs.ErrPermission}
	assertShape(t, SecretKey(p, enabled(true)).Run(bg(), sys), "tpan.secretkey", StageTPAN, Unknown)
}

// statOnly là FakeSystem cấm đọc nội dung file (SecretKey chỉ được Stat).
type statOnly struct {
	*system.FakeSystem
	t       *testing.T
	statErr error
}

func (s *statOnly) ReadFile(p string) ([]byte, error) {
	s.t.Errorf("đọc nội dung %s", p)
	return nil, fs.ErrPermission
}

func (s *statOnly) OpenRead(p string) (io.ReadCloser, int64, error) {
	s.t.Errorf("mở %s để đọc", p)
	return nil, 0, fs.ErrPermission
}

func (s *statOnly) Stat(p string) (system.FileInfo, error) {
	if s.statErr != nil {
		return system.FileInfo{}, &fs.PathError{Op: "lstat", Path: p, Err: s.statErr}
	}
	return s.FakeSystem.Stat(p)
}

// leafPEM sinh một chứng chỉ tự ký cho names với serial, issuer và hạn cho trước.
func leafPEM(t *testing.T, serial int64, issuer string, names []string, notBefore, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: issuer}, DNSNames: names,
		NotBefore: notBefore, NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestPanelACMECert(t *testing.T) {
	d, err := validate.ParseDomain("panel.example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return sslTestNow }
	id := "tpan.panelcert:panel.example.com"
	cases := []struct {
		name string
		data []byte // nil: không có file
		want Status
		sum  string
	}{
		{"đúng", makeFullchain(t, []string{"panel.example.com"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(80))), Pass, "còn 80 ngày, do R11 ký"},
		{"mất file", nil, Fail, "Không có chứng chỉ"},
		{"hỏng", []byte("rác"), Fail, "không đọc được"},
		{"sai tên", makeFullchain(t, []string{"old.example.com"}, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(80))), Fail, "không dành cho"},
		{"hết hạn", makeFullchain(t, []string{"panel.example.com"}, "R11", sslTestNow.Add(-days(90)), sslTestNow.Add(-days(1))), Fail, "hết hạn"},
		{"sắp hết", makeFullchain(t, []string{"panel.example.com"}, "R11", sslTestNow.Add(-days(80)), sslTestNow.Add(days(5))), Warn, "chỉ còn 5 ngày"},
		{"staging", makeFullchain(t, []string{"panel.example.com"}, "(STAGING) Pseudo Plum E5", sslTestNow.Add(-days(1)), sslTestNow.Add(days(80))), Warn, "chứng chỉ thử"},
		{"fake LE", makeFullchain(t, []string{"panel.example.com"}, "Fake LE Intermediate X1", sslTestNow.Add(-days(1)), sslTestNow.Add(days(80))), Warn, "chứng chỉ thử"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			if c.data != nil {
				f.SetFile(panelFullchain, c.data, 0o644, system.Root)
			}
			r := PanelACMECert(d, now).Run(bg(), f)
			assertShape(t, r, id, StageTPAN, c.want)
			if !strings.Contains(r.Summary.Vi, c.sum) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.sum)
			}
			assertNoCmd(t, f)
		})
	}
	assertShape(t, PanelACMECert(validate.Domain{}, now).Run(bg(), system.NewFake()), "tpan.panelcert:", StageTPAN, Unknown)
}

// panelServesFake: tpan active (hoặc không), lineage có chứng chỉ serial 7,
// openssl trả lần lượt các chứng chỉ trong served (lặp lại cái cuối).
func panelServesFake(t *testing.T, active bool, served ...[]byte) *system.FakeSystem {
	t.Helper()
	f := system.NewFake()
	if active {
		reply(f, []string{"systemctl", "is-active", "tpan"}, 0, "active\n", "")
	} else {
		reply(f, []string{"systemctl", "is-active", "tpan"}, 3, "inactive\n", "")
	}
	f.SetFile(panelFullchain, leafPEM(t, 7, "R11", []string{"panel.example.com"}, sslTestNow, sslTestNow.Add(days(80))), 0o644, system.Root)
	n := 0
	f.Handle([]string{"openssl", "s_client"}, func(system.Cmd) system.Result {
		i := min(n, len(served)-1)
		n++
		return system.Result{Stdout: "CONNECTED(00000003)\n---\nServer certificate\n" + string(served[i]) + "subject=CN = x\n---\nDONE\n"}
	})
	return f
}

func fastServes(t *testing.T, every, total time.Duration) {
	t.Helper()
	oe, of := panelServesEvery, panelServesFor
	panelServesEvery, panelServesFor = every, total
	t.Cleanup(func() { panelServesEvery, panelServesFor = oe, of })
}

func TestPanelServes(t *testing.T) {
	d, err := validate.ParseDomain("panel.example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := "tpan.panelserves:panel.example.com"
	right := leafPEM(t, 7, "R11", []string{"panel.example.com"}, sslTestNow, sslTestNow.Add(days(80)))
	self := leafPEM(t, 99, "tpan", []string{"localhost"}, sslTestNow, sslTestNow.Add(days(800)))

	t.Run("đúng ngay", func(t *testing.T) {
		fastServes(t, time.Millisecond, 50*time.Millisecond)
		f := panelServesFake(t, true, right)
		r := PanelServes(d, "8443").Run(bg(), f)
		assertShape(t, r, id, StageTPAN, Pass)
		ran := f.Ran()
		if len(ran) != 2 || ran[1].String() != "openssl s_client -connect 127.0.0.1:8443 -servername panel.example.com" || len(ran[1].Stdin) != 0 {
			t.Errorf("lệnh = %v", ran)
		}
	})
	t.Run("đúng sau khi thử lại", func(t *testing.T) {
		fastServes(t, time.Millisecond, time.Second)
		f := panelServesFake(t, true, self, self, right)
		r := PanelServes(d, "8443").Run(bg(), f)
		assertShape(t, r, id, StageTPAN, Pass)
		if got := len(f.Ran()); got != 4 {
			t.Errorf("số lệnh = %d, muốn 1 systemctl + 3 openssl", got)
		}
	})
	t.Run("sai mãi thì Fail", func(t *testing.T) {
		fastServes(t, time.Millisecond, 20*time.Millisecond)
		f := panelServesFake(t, true, self)
		r := PanelServes(d, "8443").Run(bg(), f)
		assertShape(t, r, id, StageTPAN, Fail)
		if !strings.Contains(r.Summary.Vi, "serial 63") || !strings.Contains(r.Summary.Vi, "không phải 7") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if got := len(f.Ran()); got < 3 {
			t.Errorf("không thử lại: %d lệnh", got)
		}
	})
	t.Run("không có chứng chỉ thì Fail", func(t *testing.T) {
		fastServes(t, time.Millisecond, 0)
		f := panelServesFake(t, true, []byte("không có PEM\n"))
		assertShape(t, PanelServes(d, "8443").Run(bg(), f), id, StageTPAN, Fail)
	})
	t.Run("tpan không chạy thì Unknown", func(t *testing.T) {
		f := panelServesFake(t, false, right)
		r := PanelServes(d, "8443").Run(bg(), f)
		assertShape(t, r, id, StageTPAN, Unknown)
		if len(f.Ran()) != 1 {
			t.Errorf("không được gọi openssl: %v", f.Ran())
		}
	})
	t.Run("không có lineage thì Unknown", func(t *testing.T) {
		f := panelServesFake(t, true, right)
		if err := f.Remove(panelFullchain); err != nil {
			t.Fatal(err)
		}
		assertShape(t, PanelServes(d, "8443").Run(bg(), f), id, StageTPAN, Unknown)
	})
	t.Run("cổng sai không chạy lệnh", func(t *testing.T) {
		for _, p := range []string{"", "0", "70000", "-1", "84a3", "8443; id", "$(id)"} {
			f := panelServesFake(t, true, right)
			assertShape(t, PanelServes(d, p).Run(bg(), f), id, StageTPAN, Unknown)
			assertNoCmd(t, f)
		}
	})
	t.Run("huỷ giữa chừng", func(t *testing.T) {
		fastServes(t, time.Millisecond, time.Second)
		f := panelServesFake(t, true, right)
		assertShape(t, PanelServes(d, "8443").Run(cancelledCtx(), f), id, StageTPAN, Unknown)
	})
}
