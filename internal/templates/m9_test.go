package templates

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

func m9Layout(t *testing.T, domain string) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain(domain)
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

// Golden của template cron M9 (cập nhật bằng cờ -update như TestGolden).
func TestM9Golden(t *testing.T) {
	cases := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"site-cron", func() ([]byte, error) { return SiteCron(m9Layout(t, "shop.example.com")) }},
		{"backup-cron", func() ([]byte, error) { return BackupCron("03:30") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.gen()
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join("testdata", c.name+".golden")
			if *update {
				if err := os.WriteFile(p, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("đọc golden (chạy với -update để tạo): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("khác golden %s (chạy với -update rồi đọc git diff):\n--- got ---\n%s", p, got)
			}
		})
	}
}

// cronLines là các dòng không phải chú thích và không rỗng: man 5 crontab.
func cronLines(b []byte) []string {
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// File trong /etc/cron.d: dòng đầu là Header, kết thúc bằng đúng một xuống
// dòng (cron bỏ qua dòng cuối thiếu \n), đúng một dòng lệnh.
func TestM9CronShape(t *testing.T) {
	site1, err := SiteCron(m9Layout(t, "shop.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	bk, err := BackupCron("03:30")
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]byte{"site": site1, "backup": bk} {
		if first, _, _ := strings.Cut(string(got), "\n"); first != Header {
			t.Errorf("%s: dòng đầu = %q", name, first)
		}
		if !bytes.HasSuffix(got, []byte("\n")) || bytes.HasSuffix(got, []byte("\n\n")) {
			t.Errorf("%s: cuối file phải có đúng một xuống dòng", name)
		}
		if n := len(cronLines(got)); n != 1 {
			t.Errorf("%s: %d dòng lệnh, muốn 1", name, n)
		}
	}
	if got, want := cronLines(site1)[0], "*/5 * * * * root /usr/local/bin/tpan cron run shop.example.com >/dev/null 2>&1"; got != want {
		t.Errorf("dòng cron site = %q", got)
	}
	if got, want := cronLines(bk)[0], "30 03 * * * root /usr/local/bin/tpan backup create --all --scheduled --yes >/dev/null 2>&1"; got != want {
		t.Errorf("dòng cron backup = %q", got)
	}
	// Dòng cron của site phải khớp hằng CronEvery mà core hiển thị (mục 6.10).
	if !strings.HasPrefix(cronLines(site1)[0], "*/5 * * * * ") {
		t.Error("nhịp cron lệch CronEvery")
	}
}

func TestBackupCronGio(t *testing.T) {
	for at, want := range map[string]string{
		"00:00": "00 00 * * * root",
		"03:05": "05 03 * * * root",
		"23:59": "59 23 * * * root",
		"12:00": "00 12 * * * root",
	} {
		got, err := BackupCron(at)
		if err != nil {
			t.Errorf("%s: %v", at, err)
			continue
		}
		if line := cronLines(got)[0]; !strings.HasPrefix(line, want+" ") {
			t.Errorf("%s: dòng = %q, muốn bắt đầu %q", at, line, want)
		}
	}
}

func TestBackupCronTuChoi(t *testing.T) {
	for _, at := range []string{
		"", "3:30", "03:5", "24:00", "23:60", "99:99", "-1:30", "03-30", "03:30:00", " 03:30",
		"03:30 ", "03:30\n", "03:30\n* * * * * root id", "03:30; rm -rf /", "$(id):30", "0３:30",
		"aa:bb", "03:30 * * * * root rm -rf /\n#",
	} {
		out, err := BackupCron(at)
		if err == nil || !errors.Is(err, ErrParam) {
			t.Errorf("BackupCron(%q) = %q, %v; muốn ErrParam", at, out, err)
		}
	}
}

func TestSiteCronTuChoi(t *testing.T) {
	if _, err := SiteCron(site.Layout{}); !errors.Is(err, ErrParam) {
		t.Errorf("Layout zero: %v, muốn ErrParam", err)
	}
	// Layout bị sửa (không khớp LayoutOf) cũng phải bị từ chối.
	l := m9Layout(t, "shop.example.com")
	l.User = "khac"
	if _, err := SiteCron(l); !errors.Is(err, ErrParam) {
		t.Errorf("Layout lệch: %v, muốn ErrParam", err)
	}
}

// Domain tấn công đã bị validate.ParseDomain chặn trước; test này khẳng định
// điều đó và rằng mọi domain hợp lệ đều ra dòng cron an toàn cho shell.
func TestSiteCronDomainTanCong(t *testing.T) {
	for _, s := range []string{
		"a.com; rm -rf /", "$(id).example.com", "../../etc/passwd", "a.com\nb.com",
		strings.Repeat("a", 300) + ".com", "a.com && id", "`id`.com",
	} {
		if _, err := validate.ParseDomain(s); err == nil {
			t.Errorf("ParseDomain(%q) phải lỗi", s)
		}
	}
	for _, s := range []string{"a.com", "shop-1.example.com", "xn--bcher-kva.example", "a.b.c.d.example.com", "tên-miền.vn"} {
		d, err := validate.ParseDomain(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		got, err := SiteCron(site.LayoutOf(d))
		if err != nil {
			t.Errorf("%s: %v", s, err)
			continue
		}
		line := cronLines(got)[0]
		dom := strings.TrimSuffix(strings.TrimPrefix(line, "*/5 * * * * root /usr/local/bin/tpan cron run "), " >/dev/null 2>&1")
		if dom != d.String() || strings.ContainsAny(dom, " ;&|$`()<>\\\"'\n") {
			t.Errorf("%s: phần domain trong dòng cron = %q (muốn %q, ASCII)", s, dom, d.String())
		}
	}
}
