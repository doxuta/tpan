package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// ssClean là dạng `ss -tlnH` của một máy Ubuntu 24.04 vừa cài xong stack:
// MariaDB chỉ nghe loopback, nginx nghe mọi địa chỉ, systemd-resolved nghe
// 127.0.0.53%lo. Cột Local Address:Port theo ss(8).
const ssClean = `LISTEN 0      4096   127.0.0.53%lo:53         0.0.0.0:*
LISTEN 0      128          0.0.0.0:22         0.0.0.0:*
LISTEN 0      80         127.0.0.1:3306       0.0.0.0:*
LISTEN 0      511          0.0.0.0:80         0.0.0.0:*
LISTEN 0      128             [::]:22            [::]:*
LISTEN 0      511             [::]:80            [::]:*
`

func TestParseSS(t *testing.T) {
	ls := parseSS(ssClean)
	if len(ls) != 6 {
		t.Fatalf("đọc được %d dòng, muốn 6: %+v", len(ls), ls)
	}
	cases := []struct {
		i        int
		port     int
		loopback bool
	}{
		{0, 53, true}, {1, 22, false}, {2, 3306, true}, {3, 80, false}, {4, 22, false}, {5, 80, false},
	}
	for _, c := range cases {
		if ls[c.i].port != c.port || ls[c.i].loopback() != c.loopback {
			t.Errorf("dòng %d: %+v, muốn cổng %d loopback=%v", c.i, ls[c.i], c.port, c.loopback)
		}
	}

	// Các dạng địa chỉ khác của ss: "*", IPv6 loopback, IPv4 trong IPv6, rác.
	more := parseSS(`LISTEN 0 80 [::1]:3306 [::]:*
LISTEN 0 511 *:8080 *:*
LISTEN 0 80 [::ffff:127.0.0.1]:3307 [::]:*
LISTEN 0 80 127.0.0.9:3308 0.0.0.0:*
ESTAB  0 0 127.0.0.1:3306 127.0.0.1:5000
LISTEN 0 80 khong-co-dau-hai-cham 0.0.0.0:*
LISTEN 0 80 127.0.0.1:abc 0.0.0.0:*
`)
	want := []struct {
		port     int
		loopback bool
	}{{3306, true}, {8080, false}, {3307, true}, {3308, true}}
	if len(more) != len(want) {
		t.Fatalf("đọc được %d dòng, muốn %d: %+v", len(more), len(want), more)
	}
	for i, w := range want {
		if more[i].port != w.port || more[i].loopback() != w.loopback {
			t.Errorf("dòng %d: %+v, muốn %+v", i, more[i], w)
		}
	}
}

func TestParseSSTenTienTrinh(t *testing.T) {
	ls := parseSS(`LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=812,fd=6),("nginx",pid=811,fd=6))` + "\n")
	if len(ls) != 1 || ls[0].proc != "nginx" {
		t.Errorf("parseSS = %+v", ls)
	}
}

func TestPortLocalOnlyPass(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"ss"}, 0, ssClean, "")
	r := PortLocalOnly(3306, StageDB).Run(bg(), fake)
	assertShape(t, r, "port.localonly:3306", StageDB, Pass)
	assertEvidenceIsRan(t, r, fake)
	if r.Evidence[0].Command != "ss -tlnH" {
		t.Errorf("Command = %q", r.Evidence[0].Command)
	}
}

func TestPortLocalOnlyPassCaIPv6(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"ss"}, 0, "LISTEN 0 80 127.0.0.1:3306 0.0.0.0:*\nLISTEN 0 80 [::1]:3306 [::]:*\n", "")
	assertShape(t, PortLocalOnly(3306, StageDB).Run(bg(), fake), "port.localonly:3306", StageDB, Pass)
}

func TestPortLocalOnlyFailKhiMoRaNgoai(t *testing.T) {
	for name, out := range map[string]string{
		"0.0.0.0":      "LISTEN 0 80 0.0.0.0:3306 0.0.0.0:*\n",
		"ipv6 mọi nơi": "LISTEN 0 80 [::]:3306 [::]:*\n",
		"sao":          "LISTEN 0 80 *:3306 *:*\n",
		"cả hai":       "LISTEN 0 80 127.0.0.1:3306 0.0.0.0:*\nLISTEN 0 80 192.0.2.10:3306 0.0.0.0:*\n",
	} {
		t.Run(name, func(t *testing.T) {
			fake := system.NewFake()
			reply(fake, []string{"ss"}, 0, out, "")
			r := PortLocalOnly(3306, StageDB).Run(bg(), fake)
			assertShape(t, r, "port.localonly:3306", StageDB, Fail)
			assertEvidenceIsRan(t, r, fake)
			if lastFix(r) != "ss -tln" {
				t.Errorf("dòng cuối ManualFix = %q", lastFix(r))
			}
			if !strings.Contains(r.ManualFix[0], "bind-address") {
				t.Errorf("với 3306 phải gợi ý bind-address: %q", r.ManualFix)
			}
		})
	}
}

func TestPortLocalOnlyFailKhiKhongAiNghe(t *testing.T) {
	fake := system.NewFake()
	// 33060 là cổng X Protocol của MySQL; không được tính là 3306.
	reply(fake, []string{"ss"}, 0, "LISTEN 0 80 0.0.0.0:33060 0.0.0.0:*\nLISTEN 0 511 0.0.0.0:80 0.0.0.0:*\n", "")
	r := PortLocalOnly(3306, StageDB).Run(bg(), fake)
	assertShape(t, r, "port.localonly:3306", StageDB, Fail)
	if !strings.Contains(r.Summary.Vi, "Không có gì") {
		t.Errorf("Summary = %q", r.Summary.Vi)
	}
}

func TestPortLocalOnlyUnknown(t *testing.T) {
	t.Run("không chạy được", func(t *testing.T) {
		assertShape(t, PortLocalOnly(3306, StageDB).Run(cancelledCtx(), system.NewFake()), "port.localonly:3306", StageDB, Unknown)
	})
	t.Run("ss báo lỗi", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"ss"}, 1, "", "Cannot open netlink socket: Operation not permitted\n")
		assertShape(t, PortLocalOnly(3306, StageDB).Run(bg(), fake), "port.localonly:3306", StageDB, Unknown)
	})
}

func TestPortsFree(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"ss"}, 0, "LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((\"sshd\",pid=700,fd=3))\n", "")
		r := PortsFree(80, 443).Run(bg(), fake)
		assertShape(t, r, "ports.free:80,443", StageMachine, Pass)
		assertEvidenceIsRan(t, r, fake)
		if r.Evidence[0].Command != "ss -tlnpH" {
			t.Errorf("Command = %q", r.Evidence[0].Command)
		}
	})
	t.Run("fail nêu tên tiến trình", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"ss"}, 0,
			"LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=812,fd=6))\n"+
				"LISTEN 0 511 [::]:80 [::]:* users:((\"nginx\",pid=812,fd=7))\n"+
				"LISTEN 0 511 0.0.0.0:443 0.0.0.0:*\n", "")
		r := PortsFree(80, 443, 8443).Run(bg(), fake)
		assertShape(t, r, "ports.free:80,443,8443", StageMachine, Fail)
		if !strings.Contains(r.Summary.Vi, "80 (đang bị nginx dùng)") || !strings.Contains(r.Summary.Vi, "443 (đang có tiến trình dùng)") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if strings.Contains(r.Summary.Vi, "8443") {
			t.Errorf("cổng 8443 trống, không được nêu: %q", r.Summary.Vi)
		}
		if lastFix(r) != "ss -tlnp" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		assertShape(t, PortsFree(80).Run(cancelledCtx(), system.NewFake()), "ports.free:80", StageMachine, Unknown)
	})
}
