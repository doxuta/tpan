package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// Dạng `dpkg-query -W -f '${Package}\t${db:Status-Status}\n'` (dpkg-query(1)).
// Gói không biết thì dpkg-query in lỗi ra stderr và thoát mã 1.

func TestPackagesAbsentPass(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"dpkg-query"}, 1, "",
		"dpkg-query: no packages found matching nginx\ndpkg-query: no packages found matching apache2\n")
	r := PackagesAbsent("nginx", "apache2").Run(bg(), fake)
	assertShape(t, r, "packages.absent:nginx,apache2", StageMachine, Pass)
	assertEvidenceIsRan(t, r, fake)
	if got := r.Evidence[0].Command; got != `dpkg-query -W -f '${Package}\t${db:Status-Status}\n' nginx apache2` {
		t.Errorf("Command = %q", got)
	}
	if r.Evidence[0].ExitCode != 1 {
		t.Errorf("ExitCode = %d", r.Evidence[0].ExitCode)
	}
}

func TestPackagesAbsentPassKhiChiConFileCauHinh(t *testing.T) {
	// "config-files": đã gỡ (remove) nhưng còn file cấu hình; "not-installed": có trong danh sách nhưng chưa cài.
	fake := system.NewFake()
	reply(fake, []string{"dpkg-query"}, 0, "nginx\tconfig-files\nmysql-server\tnot-installed\n", "")
	assertShape(t, PackagesAbsent("nginx", "mysql-server").Run(bg(), fake), "packages.absent:nginx,mysql-server", StageMachine, Pass)
}

func TestPackagesAbsentFail(t *testing.T) {
	fake := system.NewFake()
	// apache2 chưa cài (stderr + mã 1), nginx và mariadb-server đã cài.
	reply(fake, []string{"dpkg-query"}, 1,
		"nginx\tinstalled\nmariadb-server\tinstalled\n",
		"dpkg-query: no packages found matching apache2\n")
	r := PackagesAbsent("nginx", "apache2", "mariadb-server").Run(bg(), fake)
	assertShape(t, r, "packages.absent:nginx,apache2,mariadb-server", StageMachine, Fail)
	assertEvidenceIsRan(t, r, fake)
	if !strings.Contains(r.Summary.Vi, "mariadb-server, nginx") || strings.Contains(r.Summary.Vi, "apache2") {
		t.Errorf("Summary phải nêu đúng các gói đã cài: %q", r.Summary.Vi)
	}
	if lastFix(r) != "dpkg -l nginx apache2 mariadb-server" {
		t.Errorf("ManualFix = %q", r.ManualFix)
	}
}

func TestPackagesAbsentFailVoiTrangThaiDoDang(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"dpkg-query"}, 0, "nginx\thalf-installed\n", "")
	assertShape(t, PackagesAbsent("nginx").Run(bg(), fake), "packages.absent:nginx", StageMachine, Fail)
}

func TestPackagesAbsentUnknown(t *testing.T) {
	t.Run("không chạy được", func(t *testing.T) {
		assertShape(t, PackagesAbsent("nginx").Run(cancelledCtx(), system.NewFake()), "packages.absent:nginx", StageMachine, Unknown)
	})
	t.Run("dpkg-query lỗi khác 1", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"dpkg-query"}, 2, "", "dpkg-query: error: unable to lock the administrative directory\n")
		assertShape(t, PackagesAbsent("nginx").Run(bg(), fake), "packages.absent:nginx", StageMachine, Unknown)
	})
	t.Run("tên gói xấu", func(t *testing.T) {
		for _, p := range []string{"", "-f", "--help", "nginx; id", "a b", "NGINX", "$(id)", "a\nb"} {
			fake := system.NewFake()
			r := PackagesAbsent("nginx", p).Run(bg(), fake)
			if r.Status != Unknown {
				t.Errorf("gói %q: Status = %q, muốn unknown", p, r.Status)
			}
			assertNoCmd(t, fake)
		}
	})
}

func TestPackagesAbsentKhongCoGoiNao(t *testing.T) {
	fake := system.NewFake()
	assertShape(t, PackagesAbsent().Run(bg(), fake), "packages.absent:", StageMachine, Pass)
	assertNoCmd(t, fake)
}
