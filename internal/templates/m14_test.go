package templates

import (
	"strings"
	"testing"
)

func TestPHPHardeningINI(t *testing.T) {
	got := string(PHPHardeningINI())
	first, _, _ := strings.Cut(got, "\n")
	if first != "; "+Header {
		t.Errorf("dòng đầu = %q, muốn %q (cú pháp INI: chú thích bằng dấu chấm phẩy)", first, "; "+Header)
	}
	for _, want := range []string{
		"\nopcache.validate_permission=1\n",
		"\nopcache.restrict_api=/usr/local/lib/tpan/opcache-api/\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu dòng %q trong:\n%s", strings.TrimSpace(want), got)
		}
	}
	// php-fpm -t báo lỗi với dòng bắt đầu bằng "#" (PHP 7.0 đã bỏ kiểu chú thích này).
	for i, ln := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			t.Errorf("dòng %d bắt đầu bằng #: %q", i+1, ln)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("file phải kết thúc bằng xuống dòng")
	}
	if p := PHPHardeningINIPath("8.3"); p != "/etc/php/8.3/fpm/conf.d/99-tpan.ini" {
		t.Errorf("đường dẫn = %q", p)
	}
}

func TestPHPDropIn(t *testing.T) {
	got := string(PHPDropIn())
	first, _, _ := strings.Cut(got, "\n")
	if first != Header {
		t.Errorf("dòng đầu = %q, muốn %q", first, Header)
	}
	if !strings.Contains(got, "\n[Service]\nProtectProc=invisible\n") {
		t.Errorf("thiếu khối [Service] với ProtectProc=invisible:\n%s", got)
	}
	if p := PHPDropInPath("8.4"); p != "/etc/systemd/system/php8.4-fpm.service.d/tpan.conf" {
		t.Errorf("đường dẫn = %q", p)
	}
}
