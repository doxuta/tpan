package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

func TestServicePass(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"systemctl", "is-active"}, 0, "active\n", "")
	reply(fake, []string{"systemctl", "is-enabled"}, 0, "enabled\n", "")

	r := Service("nginx", StageWeb).Run(bg(), fake)
	assertShape(t, r, "service.active:nginx", StageWeb, Pass)
	assertEvidenceIsRan(t, r, fake)
	if got := r.Evidence[0].Command + " | " + r.Evidence[1].Command; got != "systemctl is-active nginx | systemctl is-enabled nginx" {
		t.Errorf("Evidence = %q", got)
	}
	if r.Evidence[0].Output != "active" || r.Evidence[1].Output != "enabled" {
		t.Errorf("Output = %+v", r.Evidence)
	}
}

func TestServiceStaticCungLaBat(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"systemctl", "is-active"}, 0, "active\n", "")
	reply(fake, []string{"systemctl", "is-enabled"}, 0, "static\n", "")
	assertShape(t, Service("php8.3-fpm", StagePHP).Run(bg(), fake), "service.active:php8.3-fpm", StagePHP, Pass)
}

func TestServiceWarnKhiKhongBatLucKhoiDong(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"systemctl", "is-active"}, 0, "active\n", "")
	reply(fake, []string{"systemctl", "is-enabled"}, 1, "disabled\n", "")

	r := Service("mariadb", StageDB).Run(bg(), fake)
	assertShape(t, r, "service.active:mariadb", StageDB, Warn)
	if lastFix(r) != "systemctl is-enabled mariadb" {
		t.Errorf("dòng cuối ManualFix = %q", lastFix(r))
	}
	if r.ManualFix[0] != "systemctl enable mariadb" {
		t.Errorf("ManualFix[0] = %q", r.ManualFix[0])
	}
}

func TestServiceFailKhiKhongChay(t *testing.T) {
	// Dạng thật: `systemctl is-active` in "inactive" hoặc "failed" rồi thoát mã 3.
	for _, state := range []string{"inactive", "failed", "activating"} {
		fake := system.NewFake()
		reply(fake, []string{"systemctl", "is-active"}, 3, state+"\n", "")
		reply(fake, []string{"systemctl", "is-enabled"}, 0, "enabled\n", "")

		r := Service("nginx", StageWeb).Run(bg(), fake)
		assertShape(t, r, "service.active:nginx", StageWeb, Fail)
		assertEvidenceIsRan(t, r, fake)
		if !strings.Contains(r.Summary.Vi, state) {
			t.Errorf("Summary phải nói trạng thái %q: %q", state, r.Summary.Vi)
		}
		want := []string{
			"systemctl status nginx",
			"journalctl -u nginx -n 50",
			"systemctl enable --now nginx",
			"systemctl is-active nginx",
		}
		if strings.Join(r.ManualFix, "\n") != strings.Join(want, "\n") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	}
}

func TestServiceTrangThaiLaKhongVaoSummary(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"systemctl", "is-active"}, 3, "boom; rm -rf /\n", "")
	r := Service("nginx", StageWeb).Run(bg(), fake)
	assertShape(t, r, "service.active:nginx", StageWeb, Fail)
	if strings.Contains(r.Summary.Vi, "rm -rf") {
		t.Errorf("output lạ lọt vào Summary: %q", r.Summary.Vi)
	}
}

func TestServiceUnknownKhiLenhKhongChayDuoc(t *testing.T) {
	fake := system.NewFake()
	r := Service("nginx", StageWeb).Run(cancelledCtx(), fake)
	assertShape(t, r, "service.active:nginx", StageWeb, Unknown)
	if len(r.Evidence) != 1 || r.Evidence[0].ExitCode != -1 {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
}

func TestServiceTenKhongHopLe(t *testing.T) {
	for _, unit := range []string{"", "--now", "-h", "nginx; id", "a b", "$(id)", "../x", "nginx\nid"} {
		fake := system.NewFake()
		r := Service(unit, StageWeb).Run(bg(), fake)
		if r.Status != Unknown {
			t.Errorf("unit %q: Status = %q, muốn unknown", unit, r.Status)
		}
		assertNoCmd(t, fake)
	}
}
