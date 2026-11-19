package checks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

func sha256Of(data string) string {
	s := sha256.Sum256([]byte(data))
	return hex.EncodeToString(s[:])
}

// ---- Drift ----

func TestDriftPass(t *testing.T) {
	fake := system.NewFake()
	const p = "/etc/nginx/sites-available/tpan-a.conf"
	fake.SetFile(p, []byte("server {}"), 0o644, system.Root)
	want := sha256Of("server {}")

	r := Drift(p, want, StageWeb).Run(bg(), fake)
	assertShape(t, r, "drift:"+p, StageWeb, Pass)
	assertNoCmd(t, fake)
	// Chữ hoa trong mã mong đợi vẫn khớp.
	assertShape(t, Drift(p, strings.ToUpper(want), StageWeb).Run(bg(), fake), "drift:"+p, StageWeb, Pass)
}

func TestDriftFailKhiSuaTay(t *testing.T) {
	fake := system.NewFake()
	const p = "/etc/php/8.3/fpm/pool.d/tpan-a.conf"
	fake.SetFile(p, []byte("sua tay"), 0o644, system.Root)
	want := sha256Of("ban goc")

	r := Drift(p, want, StagePHP).Run(bg(), fake)
	assertShape(t, r, "drift:"+p, StagePHP, Fail)
	assertNoCmd(t, fake)
	if !strings.Contains(r.Summary.Vi, p) || !strings.Contains(r.Summary.En, p) {
		t.Errorf("Summary phải nêu đường dẫn: %+v", r.Summary)
	}
	if lastFix(r) != "sha256sum "+p {
		t.Errorf("ManualFix = %q", r.ManualFix)
	}
	if len(r.Evidence) != 1 || !strings.Contains(r.Evidence[0].Output, sha256Of("sua tay")) || !strings.Contains(r.Evidence[0].Output, want) {
		t.Errorf("Evidence phải có sha256 hai bên: %+v", r.Evidence)
	}
}

func TestDriftFailKhiMatFile(t *testing.T) {
	const p = "/etc/nginx/sites-available/tpan-a.conf"
	r := Drift(p, sha256Of("x"), StageWeb).Run(bg(), system.NewFake())
	assertShape(t, r, "drift:"+p, StageWeb, Fail)
	if !strings.Contains(r.Summary.Vi, p) || lastFix(r) != "ls -l "+p {
		t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
	}
}

func TestDriftUnknown(t *testing.T) {
	t.Run("đọc lỗi khác không tồn tại", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetDir("/etc/nginx/conf", 0o755, system.Root) // ReadFile trên thư mục trả EISDIR
		r := Drift("/etc/nginx/conf", sha256Of("x"), StageWeb).Run(bg(), fake)
		assertShape(t, r, "drift:/etc/nginx/conf", StageWeb, Unknown)
	})
	t.Run("mã mong đợi hỏng", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/etc/a", []byte("x"), 0o644, system.Root)
		for _, bad := range []string{"", "abc", strings.Repeat("g", 64)} {
			assertShape(t, Drift("/etc/a", bad, StageMachine).Run(bg(), fake), "drift:/etc/a", StageMachine, Unknown)
		}
	})
}

func TestDriftStage(t *testing.T) {
	cases := map[string]Stage{
		"/etc/nginx/sites-available/tpan-a.conf":     StageWeb,
		"/etc/nginx/sites-enabled/tpan-a.conf":       StageWeb,
		"/etc/apache2/sites-available/000-tpan.conf": StageWeb,
		"/etc/php/8.3/fpm/pool.d/tpan-a.conf":        StagePHP,
		"/var/www/a.com/public/wp-config.php":        StageWordPress,
		"/etc/tpan/tls/cert.pem":                     StageTPAN,
		"/etc/tpan":                                  StageTPAN,
		"/etc/systemd/system/tpan.service":           StageTPAN,
		"/etc/systemd/system/tpan-health.timer":      StageTPAN,
		"/etc/systemd/system/nginx.service.d/x.conf": StageMachine,
		"/etc/logrotate.d/tpan-sites":                StageMachine,
		"/etc/mysql/mariadb.conf.d/60-tpan.cnf":      StageMachine,
		"/etc/nginx/../tpan/x":                       StageTPAN, // đi vòng qua ".." không đánh lừa được
		"/etc/nginxx/a.conf":                         StageMachine,
		"/var/wwwx/a":                                StageMachine,
		"":                                           StageMachine,
	}
	for p, want := range cases {
		if got := DriftStage(p); got != want {
			t.Errorf("DriftStage(%q) = %q, muốn %q", p, got, want)
		}
	}
}

// ---- Memory ----

func meminfoOf(totalKB, availKB, swapTotalKB, swapFreeKB int) string {
	return fmt.Sprintf("MemTotal:       %d kB\nMemFree:          1000 kB\nMemAvailable:   %d kB\nBuffers:           10 kB\nSwapTotal:      %d kB\nSwapFree:       %d kB\n",
		totalKB, availKB, swapTotalKB, swapFreeKB)
}

func TestMemory(t *testing.T) {
	const total = 4_000_000
	cases := []struct {
		name                   string
		avail, swapT, swapFree int
		want                   Status
		sumVi                  string // chuỗi phải có trong Summary
	}{
		{"khoẻ, có swap", 2_000_000, 1_000_000, 900_000, Pass, "swap đã dùng 10.0%"},
		{"khoẻ, không swap", 2_000_000, 0, 0, Pass, "không có swap"},
		{"đúng 10% là còn đạt", 400_000, 0, 0, Pass, "10.0%"},
		{"dưới 10% là cảnh báo", 399_000, 0, 0, Warn, "RAM còn dùng được"},
		{"đúng 5% là cảnh báo", 200_000, 0, 0, Warn, "5.0%"},
		{"dưới 5% là lỗi", 199_000, 0, 0, Fail, "RAM còn dùng được"},
		{"swap đúng 50% là đạt", 2_000_000, 1_000_000, 500_000, Pass, "50.0%"},
		{"swap trên 50% là cảnh báo", 2_000_000, 1_000_000, 499_000, Warn, "swap đã dùng 50.1%"},
		{"RAM lỗi thắng swap cảnh báo", 100_000, 1_000_000, 100_000, Fail, "swap đã dùng 90.0%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := system.NewFake()
			fake.SetFile("/proc/meminfo", []byte(meminfoOf(total, c.avail, c.swapT, c.swapFree)), 0o444, system.Root)
			r := Memory().Run(bg(), fake)
			assertShape(t, r, "memory:ram", StageMachine, c.want)
			assertNoCmd(t, fake)
			if !strings.Contains(r.Summary.Vi, c.sumVi) {
				t.Errorf("Summary = %q, phải có %q", r.Summary.Vi, c.sumVi)
			}
			if c.want != Pass && lastFix(r) != "free -h" {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
			if len(r.Evidence) != 1 || !strings.Contains(r.Evidence[0].Output, "MemAvailable: ") {
				t.Errorf("Evidence = %+v", r.Evidence)
			}
		})
	}
}

func TestMemoryUnknown(t *testing.T) {
	t.Run("không có /proc/meminfo", func(t *testing.T) {
		assertShape(t, Memory().Run(bg(), system.NewFake()), "memory:ram", StageMachine, Unknown)
	})
	t.Run("thiếu khoá", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/proc/meminfo", []byte("MemTotal: 100 kB\n"), 0o444, system.Root)
		r := Memory().Run(bg(), fake)
		assertShape(t, r, "memory:ram", StageMachine, Unknown)
		if !strings.Contains(r.Summary.Vi, "MemAvailable") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("số vô lý", func(t *testing.T) {
		for _, m := range []string{meminfoOf(0, 0, 0, 0), meminfoOf(100, 200, 0, 0), meminfoOf(100, 50, 10, 20)} {
			fake := system.NewFake()
			fake.SetFile("/proc/meminfo", []byte(m), 0o444, system.Root)
			assertShape(t, Memory().Run(bg(), fake), "memory:ram", StageMachine, Unknown)
		}
	})
}

// ---- FreeSpace ----

func TestFreeSpace(t *testing.T) {
	const p = "/var/lib/tpan"
	const min = 1 << 30
	cases := []struct {
		free uint64
		want Status
	}{
		{5 << 30, Pass},
		{min, Pass}, // đúng ngưỡng là đạt
		{min - 1, Fail},
		{0, Fail},
	}
	for _, c := range cases {
		fake := system.NewFake()
		fake.SetDir(p, 0o755, system.Root) // FakeSystem.StatFS cần path có thật
		fake.SetDisk(p, system.DiskUsage{Total: 100 << 30, Free: c.free})
		r := FreeSpace(p, min, StageTPAN).Run(bg(), fake)
		assertShape(t, r, "disk.free:"+p, StageTPAN, c.want)
		assertNoCmd(t, fake)
		if c.want == Fail && lastFix(r) != "df -h "+p {
			t.Errorf("free=%d: ManualFix = %q", c.free, r.ManualFix)
		}
	}
}

func TestFreeSpaceUnknown(t *testing.T) {
	assertShape(t, FreeSpace("var/lib", 1, StageMachine).Run(bg(), system.NewFake()), "disk.free:var/lib", StageMachine, Unknown)
	fake := system.NewFake()
	fake.SetDir("/x", 0o755, system.Root)
	fake.SetDir("/y", 0o755, system.Root)
	fake.SetDisk("/x", system.DiskUsage{Total: 0})
	assertShape(t, FreeSpace("/x", 1, StageMachine).Run(bg(), fake), "disk.free:/x", StageMachine, Unknown)
	fake.SetDisk("/y", system.DiskUsage{Total: 10, Free: 20})
	assertShape(t, FreeSpace("/y", 1, StageMachine).Run(bg(), fake), "disk.free:/y", StageMachine, Unknown)
}

// ---- PanelCert ----

// certPEM sinh một chứng chỉ ECDSA tự ký có hạn tới notAfter.
func certPEM(t *testing.T, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tpan-test"},
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestPanelCert(t *testing.T) {
	const p = "/etc/tpan/tls/cert.pem"
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	day := 24 * time.Hour
	cases := []struct {
		name     string
		notAfter time.Time
		want     Status
		sumVi    string
	}{
		{"còn 10 năm", now.Add(3650 * day), Pass, "còn 3650 ngày"},
		{"còn đúng 14 ngày", now.Add(14 * day), Pass, "còn 14 ngày"},
		{"còn dưới 14 ngày", now.Add(14*day - time.Hour), Warn, "còn 13 ngày"},
		{"còn 1 ngày", now.Add(30 * time.Hour), Warn, "còn 1 ngày"},
		{"còn vài giờ", now.Add(3 * time.Hour), Warn, "còn 0 ngày"},
		{"vừa hết hạn", now.Add(-time.Second), Fail, "đã hết hạn"},
		{"hết hạn lâu", now.Add(-400 * day), Fail, "đã hết hạn"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := system.NewFake()
			fake.SetFile(p, certPEM(t, c.notAfter), 0o600, system.Root)
			r := PanelCert(p, clock).Run(bg(), fake)
			assertShape(t, r, "tpan.cert:"+p, StageTPAN, c.want)
			assertNoCmd(t, fake)
			if !strings.Contains(r.Summary.Vi, c.sumVi) {
				t.Errorf("Summary = %q, phải có %q", r.Summary.Vi, c.sumVi)
			}
			if c.want != Pass && lastFix(r) != "openssl x509 -noout -enddate -in "+p {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		})
	}
}

func TestPanelCertKhac(t *testing.T) {
	const p = "/etc/tpan/tls/cert.pem"
	t.Run("không có file là Unknown", func(t *testing.T) {
		assertShape(t, PanelCert(p, nil).Run(bg(), system.NewFake()), "tpan.cert:"+p, StageTPAN, Unknown)
	})
	t.Run("file không phải chứng chỉ là Fail", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile(p, []byte("khong phai pem"), 0o600, system.Root)
		assertShape(t, PanelCert(p, nil).Run(bg(), fake), "tpan.cert:"+p, StageTPAN, Fail)
	})
	t.Run("khối PEM sai loại rồi tới chứng chỉ", func(t *testing.T) {
		now := time.Now()
		key := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("x")})
		fake := system.NewFake()
		fake.SetFile(p, append(key, certPEM(t, now.Add(100*24*time.Hour))...), 0o600, system.Root)
		assertShape(t, PanelCert(p, nil).Run(bg(), fake), "tpan.cert:"+p, StageTPAN, Pass)
	})
	t.Run("đọc lỗi khác là Unknown", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetDir(p, 0o755, system.Root)
		assertShape(t, PanelCert(p, nil).Run(bg(), fake), "tpan.cert:"+p, StageTPAN, Unknown)
	})
}

// ---- FileMode ----

func TestFileMode(t *testing.T) {
	const p = "/var/lib/tpan/tpan.db"
	t.Run("đúng", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile(p, []byte("db"), 0o600, system.Root)
		r := FileMode(p, 0o600, system.Owner{}, StageTPAN).Run(bg(), fake) // Owner rỗng là root
		assertShape(t, r, "tpan.perm:"+p, StageTPAN, Pass)
		assertNoCmd(t, fake)
	})
	t.Run("sai quyền", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile(p, []byte("db"), 0o644, system.Root)
		r := FileMode(p, 0o600, system.Root, StageTPAN).Run(bg(), fake)
		assertShape(t, r, "tpan.perm:"+p, StageTPAN, Fail)
		if r.ManualFix[0] != "chmod 0600 "+p || lastFix(r) != "ls -ld "+p || len(r.ManualFix) != 2 {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		if !strings.Contains(r.Summary.Vi, "0644") || !strings.Contains(r.Summary.Vi, "0600") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("sai chủ", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile(p, []byte("db"), 0o600, system.Owner{User: "www-data", Group: "www-data"})
		r := FileMode(p, 0o600, system.Root, StageTPAN).Run(bg(), fake)
		assertShape(t, r, "tpan.perm:"+p, StageTPAN, Fail)
		if r.ManualFix[0] != "chown root:root "+p || len(r.ManualFix) != 2 {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("sai cả hai", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile(p, []byte("db"), 0o666, system.Owner{User: "x", Group: "root"})
		r := FileMode(p, 0o600, system.Root, StageTPAN).Run(bg(), fake)
		assertShape(t, r, "tpan.perm:"+p, StageTPAN, Fail)
		if len(r.ManualFix) != 3 {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("thư mục", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetDir("/etc/tpan", 0o700, system.Root)
		assertShape(t, FileMode("/etc/tpan", 0o700, system.Root, StageTPAN).Run(bg(), fake), "tpan.perm:/etc/tpan", StageTPAN, Pass)
	})
	t.Run("symlink là lỗi dù đích đúng quyền", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/tmp/real", []byte("db"), 0o600, system.Root)
		fake.SetSymlink("/tmp/real", p)
		assertShape(t, FileMode(p, 0o600, system.Root, StageTPAN).Run(bg(), fake), "tpan.perm:"+p, StageTPAN, Fail)
	})
	t.Run("không có", func(t *testing.T) {
		assertShape(t, FileMode(p, 0o600, system.Root, StageTPAN).Run(bg(), system.NewFake()), "tpan.perm:"+p, StageTPAN, Unknown)
	})
	t.Run("đường dẫn lạ được đặt trong nháy ở ManualFix", func(t *testing.T) {
		const weird = "/etc/a b; id"
		fake := system.NewFake()
		fake.SetFile(weird, []byte("x"), 0o644, system.Root)
		r := FileMode(weird, 0o600, system.Root, StageMachine).Run(bg(), fake)
		assertShape(t, r, "tpan.perm:"+weird, StageMachine, Fail)
		if lastFix(r) != "ls -ld '/etc/a b; id'" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
}

// ---- TPANDatabase ----

func TestTPANDatabase(t *testing.T) {
	const id = "tpan.db:integrity"
	t.Run("ok", func(t *testing.T) {
		fake := system.NewFake()
		r := TPANDatabase(func(context.Context) (string, error) { return "ok\n", nil }).Run(bg(), fake)
		assertShape(t, r, id, StageTPAN, Pass)
		assertNoCmd(t, fake)
	})
	t.Run("hỏng", func(t *testing.T) {
		bad := "*** in database main ***\nPage 5: btree page 6 is out of order"
		r := TPANDatabase(func(context.Context) (string, error) { return bad, nil }).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Fail)
		if len(r.Evidence) != 1 || r.Evidence[0].Output != bad {
			t.Errorf("Evidence phải là nguyên văn: %+v", r.Evidence)
		}
		if !strings.HasPrefix(lastFix(r), "sqlite3 ") || !strings.Contains(lastFix(r), "PRAGMA integrity_check") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("lỗi là Unknown", func(t *testing.T) {
		r := TPANDatabase(func(context.Context) (string, error) { return "", errors.New("database is locked") }).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Unknown)
		if !strings.Contains(r.Summary.Vi, "database is locked") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("hàm nil là Unknown", func(t *testing.T) {
		assertShape(t, TPANDatabase(nil).Run(bg(), system.NewFake()), id, StageTPAN, Unknown)
	})
}

// ---- TPANJobs ----

func TestTPANJobs(t *testing.T) {
	const id = "tpan.jobs:stuck"
	started := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	t.Run("rỗng", func(t *testing.T) {
		r := TPANJobs(func(context.Context) ([]StuckJob, error) { return nil, nil }).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Pass)
	})
	t.Run("có job treo", func(t *testing.T) {
		jobs := []StuckJob{
			{ID: "j-1", Kind: "site.add", Target: "a.example.com", Started: started},
			{ID: "j-2", Kind: "stack.install", Started: started},
		}
		r := TPANJobs(func(context.Context) ([]StuckJob, error) { return jobs, nil }).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Fail)
		for _, want := range []string{"j-1", "site.add", "j-2", "stack.install"} {
			if !strings.Contains(r.Summary.Vi, want) {
				t.Errorf("Summary thiếu %q: %q", want, r.Summary.Vi)
			}
		}
		if len(r.Evidence) != 1 || !strings.Contains(r.Evidence[0].Output, "a.example.com") {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("nhiều job thì Summary cắt gọn, Evidence đủ", func(t *testing.T) {
		var jobs []StuckJob
		for i := range 8 {
			jobs = append(jobs, StuckJob{ID: fmt.Sprintf("j-%d", i), Kind: "k", Started: started})
		}
		r := TPANJobs(func(context.Context) ([]StuckJob, error) { return jobs, nil }).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Fail)
		if !strings.Contains(r.Summary.Vi, "8 job") || !strings.Contains(r.Summary.Vi, "3 job nữa") || strings.Contains(r.Summary.Vi, "j-7") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !strings.Contains(r.Evidence[0].Output, "j-7") {
			t.Errorf("Evidence thiếu job cuối: %q", r.Evidence[0].Output)
		}
	})
	t.Run("lỗi là Unknown", func(t *testing.T) {
		r := TPANJobs(func(context.Context) ([]StuckJob, error) { return nil, errors.New("boom") }).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Unknown)
	})
	t.Run("hàm nil là Unknown", func(t *testing.T) {
		assertShape(t, TPANJobs(nil).Run(bg(), system.NewFake()), id, StageTPAN, Unknown)
	})
}

// ---- TPANScheduler ----

func TestTPANScheduler(t *testing.T) {
	const id = "tpan.scheduler:health"
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	beat := func(t time.Time, ok bool, err error) func(context.Context) (time.Time, bool, error) {
		return func(context.Context) (time.Time, bool, error) { return t, ok, err }
	}
	const maxAge = 15 * time.Minute

	t.Run("mới chạy", func(t *testing.T) {
		assertShape(t, TPANScheduler(beat(now.Add(-2*time.Minute), true, nil), maxAge, clock).Run(bg(), system.NewFake()), id, StageTPAN, Pass)
	})
	t.Run("đúng maxAge là đạt", func(t *testing.T) {
		assertShape(t, TPANScheduler(beat(now.Add(-maxAge), true, nil), maxAge, clock).Run(bg(), system.NewFake()), id, StageTPAN, Pass)
	})
	t.Run("giờ máy lệch về tương lai vẫn đạt", func(t *testing.T) {
		assertShape(t, TPANScheduler(beat(now.Add(time.Minute), true, nil), maxAge, clock).Run(bg(), system.NewFake()), id, StageTPAN, Pass)
	})
	t.Run("quá hạn", func(t *testing.T) {
		r := TPANScheduler(beat(now.Add(-maxAge-time.Second), true, nil), maxAge, clock).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Warn)
		if lastFix(r) != "systemctl status tpan" || !strings.Contains(r.Summary.Vi, "15 phút") {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("chưa bao giờ", func(t *testing.T) {
		r := TPANScheduler(beat(time.Time{}, false, nil), maxAge, clock).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Warn)
		if !strings.Contains(r.Cause.Vi, "tpan serve") {
			t.Errorf("Cause = %q", r.Cause.Vi)
		}
	})
	t.Run("lỗi là Unknown", func(t *testing.T) {
		assertShape(t, TPANScheduler(beat(time.Time{}, false, errors.New("boom")), maxAge, clock).Run(bg(), system.NewFake()), id, StageTPAN, Unknown)
	})
	t.Run("hàm nil là Unknown", func(t *testing.T) {
		assertShape(t, TPANScheduler(nil, maxAge, clock).Run(bg(), system.NewFake()), id, StageTPAN, Unknown)
	})
	t.Run("now nil dùng giờ thật", func(t *testing.T) {
		assertShape(t, TPANScheduler(beat(time.Now(), true, nil), maxAge, nil).Run(bg(), system.NewFake()), id, StageTPAN, Pass)
	})
}
