package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

const curlRecheck = "curl -sS --max-time 5 -o /dev/null -w '%{http_code}' http://127.0.0.1/"

// Output mẫu của curl 8.5 (Ubuntu 24.04) với -sS: lỗi in ra stderr dạng
// "curl: (<mã>) <lý do>", -w in mã HTTP ra stdout ("000" khi không có phản hồi).
// Mã thoát: curl.se/docs/manpage.html, mục EXIT CODES.
func curlReply(f *system.FakeSystem, code int, stdout, stderr string) {
	reply(f, []string{"curl"}, code, stdout, stderr)
}

func TestHTTPDefaultNginx(t *testing.T) {
	t.Run("pass khi đóng kết nối (444)", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 52, "000", "curl: (52) Empty reply from server\n")
		r := HTTPDefault("nginx").Run(bg(), fake)
		assertShape(t, r, "http.default:nginx", StageWeb, Pass)
		assertEvidenceIsRan(t, r, fake)
		if r.Evidence[0].Command != curlRecheck || r.Evidence[0].ExitCode != 52 {
			t.Errorf("Evidence = %+v", r.Evidence[0])
		}
		if !strings.Contains(r.Evidence[0].Output, "Empty reply") {
			t.Errorf("Output = %q", r.Evidence[0].Output)
		}
	})
	t.Run("fail: trả 200 là site mặc định của distro", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 0, "200", "")
		r := HTTPDefault("nginx").Run(bg(), fake)
		assertShape(t, r, "http.default:nginx", StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "Kết nối được nhưng sai") || !strings.Contains(r.Summary.Vi, "trả mã 200") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if lastFix(r) != curlRecheck || !strings.Contains(strings.Join(r.ManualFix, "\n"), "nginx -t") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("fail: không kết nối được", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 7, "000", "curl: (7) Failed to connect to 127.0.0.1 port 80 after 0 ms: Couldn't connect to server\n")
		r := HTTPDefault("nginx").Run(bg(), fake)
		assertShape(t, r, "http.default:nginx", StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "không kết nối được") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if r.ManualFix[0] != "systemctl status nginx" || lastFix(r) != curlRecheck {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("fail: quá thời gian", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 28, "000", "curl: (28) Connection timed out after 5001 milliseconds\n")
		r := HTTPDefault("nginx").Run(bg(), fake)
		assertShape(t, r, "http.default:nginx", StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "5 giây") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		assertShape(t, HTTPDefault("nginx").Run(cancelledCtx(), system.NewFake()), "http.default:nginx", StageWeb, Unknown)
	})
}

func TestHTTPDefaultApache(t *testing.T) {
	t.Run("pass khi trả 403", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 0, "403", "")
		r := HTTPDefault("apache").Run(bg(), fake)
		assertShape(t, r, "http.default:apache", StageWeb, Pass)
		assertEvidenceIsRan(t, r, fake)
	})
	t.Run("fail: trả 200", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 0, "200", "")
		r := HTTPDefault("apache").Run(bg(), fake)
		assertShape(t, r, "http.default:apache", StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "trả mã 200") || !strings.Contains(r.Summary.Vi, "trả 403") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if r.ManualFix[0] != "ls -l /etc/apache2/sites-enabled" || lastFix(r) != curlRecheck {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("fail: đóng kết nối không trả gì không đạt với Apache", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 52, "000", "curl: (52) Empty reply from server\n")
		r := HTTPDefault("apache").Run(bg(), fake)
		assertShape(t, r, "http.default:apache", StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "Kết nối được nhưng sai") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("fail: không kết nối được", func(t *testing.T) {
		fake := system.NewFake()
		curlReply(fake, 7, "000", "curl: (7) Failed to connect to 127.0.0.1 port 80 after 0 ms: Couldn't connect to server\n")
		r := HTTPDefault("apache").Run(bg(), fake)
		assertShape(t, r, "http.default:apache", StageWeb, Fail)
		if r.ManualFix[0] != "systemctl status apache2" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		assertShape(t, HTTPDefault("apache").Run(cancelledCtx(), system.NewFake()), "http.default:apache", StageWeb, Unknown)
	})
}

func TestHTTPDefaultWebSai(t *testing.T) {
	for _, web := range []string{"", "caddy", "Nginx", "nginx; id"} {
		fake := system.NewFake()
		r := HTTPDefault(web).Run(bg(), fake)
		if r.Status != Unknown || r.Stage != StageWeb {
			t.Errorf("web %q: %+v", web, r)
		}
		assertText(t, "Title", r.Title)
		assertNoCmd(t, fake)
	}
}
