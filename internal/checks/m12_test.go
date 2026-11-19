package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

const poolDir83 = "/etc/php/8.3/fpm/pool.d"

func poolFake(files ...string) *system.FakeSystem {
	f := system.NewFake()
	f.SetDir(poolDir83, 0o755, system.Owner{})
	for _, n := range files {
		f.SetFile(poolDir83+"/"+n, []byte("[x]\n"), 0o644, system.Owner{})
	}
	return f
}

func TestPHPPoolWWWAbsentPass(t *testing.T) {
	fake := poolFake("tpan-idle.conf", "tpan-example_com_a379a6.conf")
	r := PHPPoolWWWAbsent("8.3").Run(bg(), fake)
	assertShape(t, r, "php.www_pool:8.3", StagePHP, Pass)
	assertNoCmd(t, fake) // đọc thư mục bằng Go, không chạy lệnh
	if len(r.Evidence) != 1 || r.Evidence[0].Command != "ls "+poolDir83+" (tpan)" || r.Evidence[0].ExitCode != 0 {
		t.Fatalf("Evidence = %+v", r.Evidence)
	}
	if !strings.Contains(r.Evidence[0].Output, "tpan-idle.conf") {
		t.Errorf("Evidence phải liệt kê thư mục: %q", r.Evidence[0].Output)
	}
}

func TestPHPPoolWWWAbsentPassKhiChiCoPoolGiuCho(t *testing.T) {
	assertShape(t, PHPPoolWWWAbsent("8.3").Run(bg(), poolFake("tpan-idle.conf")), "php.www_pool:8.3", StagePHP, Pass)
}

func TestPHPPoolWWWAbsentWarnKhiConWWW(t *testing.T) {
	fake := poolFake("www.conf", "tpan-idle.conf")
	r := PHPPoolWWWAbsent("8.3").Run(bg(), fake)
	assertShape(t, r, "php.www_pool:8.3", StagePHP, Warn)
	if !strings.Contains(r.Cause.Vi, "www-data") || !strings.Contains(r.Cause.Vi, "2 tiến trình") {
		t.Errorf("Cause phải nói vì sao nên gỡ: %q", r.Cause.Vi)
	}
	want := []string{
		"sudo tpan stack install --web '<web>'",
		"ls " + poolDir83,
		"ps -eo user,cmd | grep [p]hp-fpm",
	}
	if strings.Join(r.ManualFix, "\n") != strings.Join(want, "\n") {
		t.Errorf("ManualFix = %q, muốn %q", r.ManualFix, want)
	}
	if r.Evidence[0].Output != "tpan-idle.conf\nwww.conf" {
		t.Errorf("Evidence.Output = %q", r.Evidence[0].Output)
	}
	assertNoCmd(t, fake)
}

func TestPHPPoolWWWAbsentWarnKhiThieuPoolGiuCho(t *testing.T) {
	r := PHPPoolWWWAbsent("8.3").Run(bg(), poolFake("tpan-example_com_a379a6.conf"))
	assertShape(t, r, "php.www_pool:8.3", StagePHP, Warn)
	if !strings.Contains(r.Summary.Vi, "tpan-idle") {
		t.Errorf("Summary phải nêu tpan-idle: %q", r.Summary.Vi)
	}
	// Chỉ là file giống tên (www.conf.dpkg-dist) thì không tính là pool www.
	assertShape(t, PHPPoolWWWAbsent("8.3").Run(bg(), poolFake("www.conf.dpkg-dist", "tpan-idle.conf")), "php.www_pool:8.3", StagePHP, Pass)
}

func TestPHPPoolWWWAbsentUnknown(t *testing.T) {
	t.Run("không có thư mục pool.d", func(t *testing.T) {
		r := PHPPoolWWWAbsent("8.3").Run(bg(), system.NewFake())
		assertShape(t, r, "php.www_pool:8.3", StagePHP, Unknown)
		if len(r.Evidence) != 1 || r.Evidence[0].ExitCode != 1 {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("pool.d là file, không phải thư mục", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(poolDir83, []byte("x"), 0o644, system.Owner{})
		r := PHPPoolWWWAbsent("8.3").Run(bg(), f)
		assertShape(t, r, "php.www_pool:8.3", StagePHP, Unknown)
		if len(r.Evidence) != 1 || r.Evidence[0].ExitCode != -1 {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("bản PHP sai dạng", func(t *testing.T) {
		for _, v := range []string{"", "8", "8.3.1", "8.3/../..", "8.3\n", "$(id)", "abc"} {
			fake := system.NewFake()
			r := PHPPoolWWWAbsent(v).Run(bg(), fake)
			assertShape(t, r, "php.www_pool:"+v, StagePHP, Unknown)
			assertNoCmd(t, fake)
			if len(r.Evidence) != 0 {
				t.Errorf("version %q: không đọc gì thì không có Evidence: %+v", v, r.Evidence)
			}
		}
	})
}

// ---- UnattendedUpgrades ----

const aptKeyCmd = "apt-config dump APT::Periodic::Unattended-Upgrade"

// updFake dựng máy giả với output của apt-config dump và dpkg-query.
func updFake(aptOut, dpkgOut string, dpkgCode int) *system.FakeSystem {
	f := system.NewFake()
	reply(f, []string{"apt-config"}, 0, aptOut, "")
	reply(f, []string{"dpkg-query"}, dpkgCode, dpkgOut, "")
	return f
}

func TestUnattendedUpgradesPass(t *testing.T) {
	fake := updFake("APT::Periodic::Unattended-Upgrade \"1\";\n", "installed\n", 0)
	r := UnattendedUpgrades().Run(bg(), fake)
	assertShape(t, r, "updates.auto", StageMachine, Pass)
	assertEvidenceIsRan(t, r, fake)
	if got := r.Evidence[0].Command; got != aptKeyCmd {
		t.Errorf("Command[0] = %q", got)
	}
	if got := r.Evidence[1].Command; got != `dpkg-query -W -f '${db:Status-Status}\n' unattended-upgrades` {
		t.Errorf("Command[1] = %q", got)
	}
}

func TestUnattendedUpgradesWarn(t *testing.T) {
	cases := []struct {
		name      string
		apt, dpkg string
		dpkgCode  int
		in        string // chữ phải có trong Summary
	}{
		{"tắt (0)", "APT::Periodic::Unattended-Upgrade \"0\";\n", "installed\n", 0, "\"0\""},
		{"chưa đặt", "", "installed\n", 0, "chưa được đặt"},
		{"đặt 7 ngày", "APT::Periodic::Unattended-Upgrade \"7\";\n", "installed\n", 0, "\"7\""},
		{"gói chưa cài, giá trị 1", "APT::Periodic::Unattended-Upgrade \"1\";\n", "", 1, "chưa cài"},
		{"gói đã gỡ còn config", "APT::Periodic::Unattended-Upgrade \"1\";\n", "config-files\n", 0, "chưa cài"},
		{"cả hai đều thiếu", "", "not-installed\n", 0, "chưa cài"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := updFake(c.apt, c.dpkg, c.dpkgCode)
			r := UnattendedUpgrades().Run(bg(), fake)
			assertShape(t, r, "updates.auto", StageMachine, Warn)
			assertEvidenceIsRan(t, r, fake)
			if !strings.Contains(r.Summary.Vi, c.in) {
				t.Errorf("Summary = %q, muốn chứa %q", r.Summary.Vi, c.in)
			}
			if len(r.ManualFix) != 2 || r.ManualFix[0] != "sudo tpan update auto --yes" || lastFix(r) != aptKeyCmd {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		})
	}
}

func TestUnattendedUpgradesUnknown(t *testing.T) {
	t.Run("apt-config không chạy được", func(t *testing.T) {
		fake := system.NewFake()
		r := UnattendedUpgrades().Run(cancelledCtx(), fake)
		assertShape(t, r, "updates.auto", StageMachine, Unknown)
		if len(r.Evidence) != 1 || r.Evidence[0].ExitCode != -1 {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("apt-config thoát mã khác 0", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"apt-config"}, 100, "", "E: Unable to read configuration\n")
		r := UnattendedUpgrades().Run(bg(), fake)
		assertShape(t, r, "updates.auto", StageMachine, Unknown)
		assertEvidenceIsRan(t, r, fake)
		if ran := fake.Ran(); len(ran) != 1 {
			t.Errorf("chưa biết giá trị thì không dò dpkg-query: %v", ran)
		}
		if !strings.Contains(r.Summary.Vi, "E: Unable to read configuration") {
			t.Errorf("Summary phải nói lý do: %q", r.Summary.Vi)
		}
	})
	t.Run("dpkg-query lỗi khoá", func(t *testing.T) {
		fake := updFake("APT::Periodic::Unattended-Upgrade \"1\";\n", "", 2)
		r := UnattendedUpgrades().Run(bg(), fake)
		assertShape(t, r, "updates.auto", StageMachine, Unknown)
		assertEvidenceIsRan(t, r, fake)
	})
}

func TestAptConfigValue(t *testing.T) {
	const key = "APT::Periodic::Unattended-Upgrade"
	for in, want := range map[string]string{
		"":                         "",
		key + " \"1\";\n":          "1",
		key + " \"0\";":            "0",
		"  " + key + " \"7\";  \n": "7",
		"APT::Periodic::Update-Package-Lists \"1\";\n" + key + " \"1\";\n": "1",
		// Khoá khác có chung tiền tố không được lẫn vào.
		key + "::Verbose \"2\";\n": "",
		key + "\n":                 "",
	} {
		if got := aptConfigValue(in, key); got != want {
			t.Errorf("aptConfigValue(%q) = %q, muốn %q", in, got, want)
		}
	}
}
