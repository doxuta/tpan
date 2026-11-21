package checks

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// Output mẫu trong file này viết theo tài liệu của systemd (systemctl show,
// is-active), proc(5) (status, cgroup, mountinfo) và cgroup v2; chưa lấy từ máy
// ảo thật: phần này nằm trong mục "cần kiểm trên máy ảo" của báo cáo M17.

func cageHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func cageAnyHas(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ---- cage.socket ----

func cageSocketFixture(l site.Layout, sockState, svcState string) *system.FakeSystem {
	f := system.NewFake()
	f.SetSocket(l.CageSocket(), 0o660, wwwData)
	reply(f, []string{"systemctl", "is-active", l.CageSocketUnit()}, stateExit(sockState), sockState+"\n", "")
	reply(f, []string{"systemctl", "is-active", l.CageServiceUnit()}, stateExit(svcState), svcState+"\n", "")
	return f
}

// stateExit: `systemctl is-active` trả 0 khi active, 3 với mọi trạng thái khác.
func stateExit(state string) int {
	if state == "active" {
		return 0
	}
	return 3
}

func TestCageSocketPass(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.socket:shop.example.com"
	// Dịch vụ ngủ (inactive) là bình thường với socket activation; đang chạy cũng đạt.
	for _, svc := range []string{"inactive", "active", "activating"} {
		t.Run("dịch vụ "+svc, func(t *testing.T) {
			f := cageSocketFixture(l, "active", svc)
			r := CageSocket(l).Run(bg(), f)
			assertShape(t, r, id, StagePHP, Pass)
			if len(r.Evidence) != 3 || len(f.Ran()) != 2 {
				t.Errorf("Evidence = %d mục, lệnh = %v", len(r.Evidence), f.Ran())
			}
			if got := f.Ran()[0].String(); got != "systemctl is-active "+l.CageSocketUnit() {
				t.Errorf("lệnh đầu = %q", got)
			}
			if r.AutoFix != "" {
				t.Errorf("AutoFix phải để core điền: %q", r.AutoFix)
			}
		})
	}
}

func TestCageSocketFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	cases := []struct {
		name  string
		build func() *system.FakeSystem
		want  string
	}{
		{"không có file socket", func() *system.FakeSystem {
			f := cageSocketFixture(l, "active", "inactive")
			if err := f.Remove(l.CageSocket()); err != nil {
				t.Fatal(err)
			}
			return f
		}, "không có file socket"},
		{"file thường chứ không phải socket", func() *system.FakeSystem {
			f := cageSocketFixture(l, "active", "inactive")
			_ = f.Remove(l.CageSocket())
			f.SetFile(l.CageSocket(), nil, 0o660, wwwData)
			return f
		}, "không phải socket"},
		{"sai chủ", func() *system.FakeSystem {
			f := cageSocketFixture(l, "active", "inactive")
			_ = f.Remove(l.CageSocket())
			f.SetSocket(l.CageSocket(), 0o660, system.Owner{User: l.User, Group: l.User})
			return f
		}, "phải là www-data:www-data"},
		{"sai quyền", func() *system.FakeSystem {
			f := cageSocketFixture(l, "active", "inactive")
			_ = f.Remove(l.CageSocket())
			f.SetSocket(l.CageSocket(), 0o666, wwwData)
			return f
		}, "quyền socket là 0666"},
		{"unit socket dừng", func() *system.FakeSystem { return cageSocketFixture(l, "inactive", "inactive") }, "phải là active"},
		{"dịch vụ failed", func() *system.FakeSystem { return cageSocketFixture(l, "active", "failed") }, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := c.build()
			f.ResetRan()
			r := CageSocket(l).Run(bg(), f)
			assertShape(t, r, "cage.socket:shop.example.com", StagePHP, Fail)
			if !strings.Contains(r.Summary.Vi, c.want) {
				t.Errorf("Summary thiếu %q: %q", c.want, r.Summary.Vi)
			}
			// Lệnh sửa tay: tự gõ được, và dòng cuối là lệnh kiểm lại.
			if !cageAnyHas(r.ManualFix, "systemctl enable --now "+l.CageSocketUnit()) || lastFix(r) != "ls -l "+l.CageSocket() {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
			if len(r.Evidence) != 3 {
				t.Errorf("phải có đủ 3 bằng chứng dù sai: %d", len(r.Evidence))
			}
		})
	}
	t.Run("nhiều điều sai được nêu hết", func(t *testing.T) {
		f := cageSocketFixture(l, "inactive", "failed")
		_ = f.Remove(l.CageSocket())
		r := CageSocket(l).Run(bg(), f)
		assertShape(t, r, "cage.socket:shop.example.com", StagePHP, Fail)
		for _, s := range []string{"không có file socket", "phải là active", "failed"} {
			if !strings.Contains(r.Summary.Vi, s) {
				t.Errorf("Summary thiếu %q: %q", s, r.Summary.Vi)
			}
		}
	})
}

func TestCageSocketUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.socket:shop.example.com"
	t.Run("systemctl không trả trạng thái đọc được", func(t *testing.T) {
		f := system.NewFake()
		f.SetSocket(l.CageSocket(), 0o660, wwwData) // lệnh không có handler: mã 0, output rỗng
		assertShape(t, CageSocket(l).Run(bg(), f), id, StagePHP, Unknown)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		f := cageSocketFixture(l, "active", "inactive")
		assertShape(t, CageSocket(l).Run(cancelledCtx(), f), id, StagePHP, Unknown)
	})
	t.Run("Layout zero", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, CageSocket(site.Layout{}).Run(bg(), f), "cage.socket:", StagePHP, Unknown)
		assertNoCmd(t, f)
	})
}

// ---- cage.runtime ----

const (
	rtPid       = 4242
	rtEnter     = "@1700000000"
	rtFenceTime = "1699999000"
)

const rtMountOK = `20 1 0:30 / /var rw,relatime - tmpfs tmpfs rw
21 1 0:31 / /run rw,nosuid - tmpfs tmpfs rw
22 1 0:32 / /etc/tpan ro,relatime - tmpfs tmpfs ro
23 21 8:1 /mysqld /run/mysqld ro,relatime - ext4 /dev/sda1 rw
24 21 0:25 /systemd/resolve /run/systemd/resolve ro,relatime - tmpfs tmpfs rw
`

// setProc đặt một tiến trình giả trong /proc.
func setProc(f *system.FakeSystem, pid int, uid, cgroup string) {
	p := fmt.Sprintf("/proc/%d/", pid)
	f.SetFile(p+"status", []byte("Name:\tphp-fpm8.3\nUid:\t"+uid+"\t"+uid+"\t"+uid+"\t"+uid+"\nNoNewPrivs:\t1\nCapEff:\t0000000000000000\n"), 0o444, system.Root)
	f.SetFile(p+"cgroup", []byte(cgroup+"\n"), 0o444, system.Root)
}

// runtimeFixture dựng một máy mà lồng chạy đúng hàng rào: mọi phép của cage.runtime đạt.
func runtimeFixture(l site.Layout) *system.FakeSystem {
	f := system.NewFake()
	inSlice := "0::/tpan.slice/" + l.CageSlice() + "/" + l.CageServiceUnit()
	setProc(f, rtPid, "1001", inSlice)
	setProc(f, 1, "0", "0::/init.scope")
	setProc(f, 777, "1002", "0::/tpan.slice/tpan-other.slice/tpan-cage-other-php.service") // site khác
	f.SetDir("/proc/self", 0o555, system.Root)                                             // không phải số: phải bị bỏ qua
	f.SetFile(fmt.Sprintf("/proc/%d/mountinfo", rtPid), []byte(rtMountOK), 0o444, system.Root)
	f.SetSymlink("/usr/sbin/php-fpm8.3", fmt.Sprintf("/proc/%d/exe", rtPid))
	f.SetFile(l.CageFence(), []byte("[Service]\n"), 0o644, system.Root)
	reply(f, []string{"id", "-u", l.User}, 0, "1001\n", "")
	reply(f, []string{"systemctl", "show"}, 0, fmt.Sprintf("MainPID=%d\nActiveEnterTimestamp=%s\n", rtPid, rtEnter), "")
	reply(f, []string{"systemctl", "is-active", "mariadb"}, 0, "active\n", "")
	reply(f, []string{"stat", "-c", "%Y", l.CageFence()}, 0, rtFenceTime+"\n", "")
	return f
}

func TestCageRuntimePass(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.runtime:shop.example.com"
	t.Run("lồng chạy đúng hàng rào", func(t *testing.T) {
		f := runtimeFixture(l)
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
		if !strings.Contains(r.Summary.Vi, "đúng hàng rào") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		var cmds []string
		for _, c := range f.Ran() {
			cmds = append(cmds, c.String())
		}
		want := []string{"id -u " + l.User,
			"systemctl show --timestamp=unix -p MainPID -p ActiveEnterTimestamp " + l.CageServiceUnit(),
			"systemctl is-active mariadb",
			"stat -c %Y " + l.CageFence()}
		if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
			t.Errorf("lệnh đã chạy = %q, muốn %q", cmds, want)
		}
		for _, c := range f.Ran() {
			if c.AsUser != "" {
				t.Errorf("phép kiểm của lồng chạy bằng quyền tpan, không dưới user của site: %+v", c)
			}
		}
	})
	t.Run("lồng đang ngủ", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"systemctl", "show"}, 0, "MainPID=0\nActiveEnterTimestamp=\n", "")
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
		if !strings.Contains(r.Summary.Vi, "đang ngủ") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if len(f.Ran()) != 2 { // id và systemctl show; không đọc tiến trình chính, không hỏi mariadb
			t.Errorf("lệnh đã chạy = %v", f.Ran())
		}
	})
	t.Run("mariadb không chạy thì không đòi dòng gắn /run/mysqld", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"systemctl", "is-active", "mariadb"}, 3, "inactive\n", "")
		f.SetFile(fmt.Sprintf("/proc/%d/mountinfo", rtPid), []byte(strings.Replace(rtMountOK, "23 21 8:1 /mysqld /run/mysqld ro,relatime - ext4 /dev/sda1 rw\n", "", 1)), 0o444, system.Root)
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("máy không có systemd-resolved: không có dòng gắn resolve vẫn đạt", func(t *testing.T) {
		f := runtimeFixture(l)
		f.SetFile(fmt.Sprintf("/proc/%d/mountinfo", rtPid), []byte(strings.Replace(rtMountOK, "24 21 0:25 /systemd/resolve /run/systemd/resolve ro,relatime - tmpfs tmpfs rw\n", "", 1)), 0o444, system.Root)
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("hàng rào sửa cùng giây với lúc dịch vụ vào active thì không cảnh báo", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"stat", "-c", "%Y", l.CageFence()}, 0, "1700000000\n", "")
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("điểm gắn có hai dòng, một là tmpfs", func(t *testing.T) {
		f := runtimeFixture(l)
		mi := "19 1 8:1 / /var rw - ext4 /dev/sda1 rw\n" + rtMountOK
		f.SetFile(fmt.Sprintf("/proc/%d/mountinfo", rtPid), []byte(mi), 0o444, system.Root)
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Pass)
	})
}

func TestCageRuntimeFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.runtime:shop.example.com"
	mainFile := func(name string) string { return fmt.Sprintf("/proc/%d/%s", rtPid, name) }
	cases := []struct {
		name  string
		build func(f *system.FakeSystem)
		want  string
	}{
		{"còn tiến trình cùng uid ở ngoài lồng", func(f *system.FakeSystem) {
			setProc(f, 999, "1001", "0::/user.slice/user-1001.slice/session-3.scope")
		}, "1 tiến trình"},
		{"tiến trình ngoài lồng khi lồng đang ngủ", func(f *system.FakeSystem) {
			setProc(f, 999, "1001", "0::/system.slice/cron.service")
			reply(f, []string{"systemctl", "show"}, 0, "MainPID=0\nActiveEnterTimestamp=\n", "")
		}, "1 tiến trình"},
		{"NoNewPrivs bằng 0", func(f *system.FakeSystem) {
			f.SetFile(mainFile("status"), []byte("Uid:\t1001\t1001\t1001\t1001\nNoNewPrivs:\t0\nCapEff:\t0000000000000000\n"), 0o444, system.Root)
		}, "NoNewPrivs=1"},
		{"không có dòng NoNewPrivs", func(f *system.FakeSystem) {
			f.SetFile(mainFile("status"), []byte("Uid:\t1001\t1001\t1001\t1001\nCapEff:\t0000000000000000\n"), 0o444, system.Root)
		}, "NoNewPrivs=1"},
		{"còn capability", func(f *system.FakeSystem) {
			f.SetFile(mainFile("status"), []byte("Uid:\t1001\t1001\t1001\t1001\nNoNewPrivs:\t1\nCapEff:\t0000000000000400\n"), 0o444, system.Root)
		}, "CapEff"},
		{"không nằm dưới slice của site", func(f *system.FakeSystem) {
			f.SetFile(mainFile("cgroup"), []byte("0::/system.slice/php8.3-fpm.service\n"), 0o444, system.Root)
		}, "không nằm dưới " + l.CageSlice()},
		{"thiếu tmpfs ở /var", func(f *system.FakeSystem) {
			f.SetFile(mainFile("mountinfo"), []byte(strings.Replace(rtMountOK, "20 1 0:30 / /var rw,relatime - tmpfs tmpfs rw\n", "", 1)), 0o444, system.Root)
		}, "tmpfs ở /var"},
		{"thiếu tmpfs ở /etc/tpan", func(f *system.FakeSystem) {
			f.SetFile(mainFile("mountinfo"), []byte(strings.Replace(rtMountOK, "22 1 0:32 / /etc/tpan ro,relatime - tmpfs tmpfs ro\n", "", 1)), 0o444, system.Root)
		}, "tmpfs ở /etc/tpan"},
		{"mariadb chạy mà không có dòng gắn /run/mysqld", func(f *system.FakeSystem) {
			f.SetFile(mainFile("mountinfo"), []byte(strings.Replace(rtMountOK, "23 21 8:1 /mysqld /run/mysqld ro,relatime - ext4 /dev/sda1 rw\n", "", 1)), 0o444, system.Root)
		}, "không có dòng gắn /run/mysqld"},
		{"/run/mysqld trỏ vào thư mục đã xoá", func(f *system.FakeSystem) {
			f.SetFile(mainFile("mountinfo"), []byte(strings.Replace(rtMountOK, "/mysqld /run/mysqld", "/mysqld//deleted /run/mysqld", 1)), 0o444, system.Root)
		}, "/run/mysqld trỏ vào thư mục đã bị xoá"},
		{"/run/systemd/resolve trỏ vào thư mục đã xoá", func(f *system.FakeSystem) {
			f.SetFile(mainFile("mountinfo"), []byte(strings.Replace(rtMountOK, "/systemd/resolve /run", "/systemd/resolve//deleted /run", 1)), 0o444, system.Root)
		}, "/run/systemd/resolve trỏ vào thư mục đã bị xoá"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := runtimeFixture(l)
			c.build(f)
			r := CageRuntime(l).Run(bg(), f)
			assertShape(t, r, id, StagePHP, Fail)
			if !strings.Contains(r.Summary.Vi, c.want) {
				t.Errorf("Summary thiếu %q: %q", c.want, r.Summary.Vi)
			}
			if !cageHas(r.ManualFix, "systemctl try-restart "+l.CageServiceUnit()) {
				t.Errorf("ManualFix thiếu try-restart: %q", r.ManualFix)
			}
			if !cageAnyHas(r.ManualFix, "/run/mysqld") { // lệnh tạo thư mục socket của MariaDB
				t.Errorf("ManualFix thiếu lệnh tạo /run/mysqld: %q", r.ManualFix)
			}
		})
	}
	t.Run("tiến trình lạc: ManualFix kê đúng pid, kiểm lại bằng ps", func(t *testing.T) {
		f := runtimeFixture(l)
		setProc(f, 999, "1001", "0::/user.slice/x.scope")
		setProc(f, 1000, "1001", "0::/user.slice/y.scope")
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Fail)
		if !cageHas(r.ManualFix, "kill -KILL 1000 999") && !cageHas(r.ManualFix, "kill -KILL 999 1000") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		if lastFix(r) != "ps -u "+l.User+" -o pid,cgroup,args" {
			t.Errorf("dòng cuối = %q", lastFix(r))
		}
		scan := r.Evidence[1]
		if scan.ExitCode != 1 || !strings.Contains(scan.Output, "pid 999") || !strings.Contains(scan.Output, "pid 1000") {
			t.Errorf("bằng chứng quét /proc = %+v", scan)
		}
		for _, e := range r.Evidence {
			if strings.Contains(e.Output, "pid 777") {
				t.Errorf("tiến trình của site khác không được nêu: %+v", e)
			}
		}
	})
	t.Run("Fail thắng Warn", func(t *testing.T) {
		f := runtimeFixture(l)
		setProc(f, 999, "1001", "0::/user.slice/x.scope")
		_ = f.Remove(mainFile("exe"))
		f.SetSymlink("/usr/sbin/php-fpm8.3 (deleted)", mainFile("exe"))
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Fail)
	})
}

func TestCageRuntimeWarn(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.runtime:shop.example.com"
	exe := fmt.Sprintf("/proc/%d/exe", rtPid)
	t.Run("bản PHP đã bị thay", func(t *testing.T) {
		f := runtimeFixture(l)
		_ = f.Remove(exe)
		f.SetSymlink("/usr/sbin/php-fpm8.3 (deleted)", exe)
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Warn)
		if !strings.Contains(r.Summary.Vi, "(deleted)") || !cageHas(r.ManualFix, "systemctl try-restart "+l.CageServiceUnit()) {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("hàng rào mới hơn lúc dịch vụ khởi động", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"stat", "-c", "%Y", l.CageFence()}, 0, "1700000100\n", "")
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Warn)
		if !strings.Contains(r.Summary.Vi, "mới hơn") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("không đọc được mốc thời gian thì không cảnh báo", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"stat", "-c", "%Y", l.CageFence()}, 1, "", "stat: cannot statx")
		reply(f, []string{"systemctl", "show"}, 0, fmt.Sprintf("MainPID=%d\nActiveEnterTimestamp=\n", rtPid), "")
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("cả hai cảnh báo", func(t *testing.T) {
		f := runtimeFixture(l)
		_ = f.Remove(exe)
		f.SetSymlink("/usr/sbin/php-fpm8.3 (deleted)", exe)
		reply(f, []string{"stat", "-c", "%Y", l.CageFence()}, 0, "1700000100\n", "")
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Warn)
		if !strings.Contains(r.Summary.Vi, "(deleted)") || !strings.Contains(r.Summary.Vi, "mới hơn") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
}

func TestCageRuntimeUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.runtime:shop.example.com"
	t.Run("không lấy được uid", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"id", "-u", l.User}, 1, "", "id: 'tp_x': no such user")
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Unknown)
	})
	t.Run("uid không phải số", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"id", "-u", l.User}, 0, "", "")
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Unknown)
	})
	t.Run("systemctl show không trả MainPID: không được coi là lồng đang ngủ", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"systemctl", "show"}, 0, "", "")
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Unknown)
		if strings.Contains(r.Summary.Vi, "ngủ") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("không đọc được /proc", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"id", "-u", l.User}, 0, "1001\n", "")
		assertShape(t, CageRuntime(l).Run(bg(), f), id, StagePHP, Unknown)
	})
	t.Run("tiến trình chính biến mất giữa chừng", func(t *testing.T) {
		f := runtimeFixture(l)
		reply(f, []string{"systemctl", "show"}, 0, "MainPID=31337\nActiveEnterTimestamp=@1700000000\n", "")
		r := CageRuntime(l).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Unknown)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, CageRuntime(l).Run(cancelledCtx(), runtimeFixture(l)), id, StagePHP, Unknown)
	})
	t.Run("Layout zero và user lạ", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, CageRuntime(site.Layout{}).Run(bg(), f), "cage.runtime:", StagePHP, Unknown)
		bad := l
		bad.User = "-x"
		assertShape(t, CageRuntime(bad).Run(bg(), f), id, StagePHP, Unknown)
		assertNoCmd(t, f)
	})
}

func TestCageRuntimeOnlyReads(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := runtimeFixture(l)
	setProc(f, 999, "1001", "0::/user.slice/x.scope")
	before := f.Snapshot()
	CageRuntime(l).Run(bg(), f)
	for _, c := range f.Ran() {
		if c.Name == "kill" || (c.Name == "systemctl" && c.Args[0] != "show" && c.Args[0] != "is-active") {
			t.Errorf("phép kiểm không được sửa máy: %s", c)
		}
	}
	if after := f.Snapshot(); len(after) != len(before) {
		t.Errorf("hệ thống file đổi: %d -> %d", len(before), len(after))
	}
}

// ---- cage.fence ----

// descSys là System bọc có Describe riêng, như cage.Sys: lệnh có AsUser in ra dạng systemd-run.
type descSys struct{ *system.FakeSystem }

func (descSys) Describe(c system.Cmd) string {
	if c.AsUser == "" {
		return c.String()
	}
	plain := c
	plain.AsUser = ""
	return "systemd-run --unit=x -p User=" + c.AsUser + " -- " + plain.String()
}

func fenceFixture(l site.Layout) *system.FakeSystem {
	f := system.NewFake()
	f.SetFile(l.CageFence(), []byte("[Service]\n"), 0o644, system.Root)
	reply(f, []string{"id", "-un"}, 0, l.User+"\n", "")
	reply(f, []string{"ls", "-A", "/var/www"}, 0, l.Domain.String()+"\n", "")
	reply(f, []string{"ls", "-A", "/tmp"}, 0, "", "")
	reply(f, []string{"ls", "-A", "/run"}, 0, "mysqld\nsystemd\n", "")
	reply(f, []string{"find", "/proc"}, 0, "", "")
	reply(f, []string{"getent", "passwd"}, 0, "root:x:0:0::/root:/bin/sh\nwww-data:x:33:33::/var/www:/usr/sbin/nologin\n"+l.User+":x:1001:1001::/nonexistent:/usr/sbin/nologin\nnobody:x:65534:65534::/nonexistent:/usr/sbin/nologin\n", "")
	reply(f, []string{"test", "-w", "/etc"}, 1, "", "")
	return f
}

func TestCageFencePass(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := fenceFixture(l)
	r := CageFence(l).Run(bg(), f)
	assertShape(t, r, "cage.fence:shop.example.com", StagePHP, Pass)
	assertEvidenceIsRan(t, r, f) // đúng bảy lệnh, mỗi lệnh một dòng; việc đọc file hàng rào không có bằng chứng riêng
}

// MariaDB chưa chạy lúc lệnh khởi động: lệnh gắn -/run/mysqld bị bỏ qua,
// /run chỉ còn systemd. Không phải hàng rào hở.
func TestCageFencePassKhongCoMysqld(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := fenceFixture(l)
	reply(f, []string{"ls", "-A", "/run"}, 0, "systemd\n", "")
	r := CageFence(l).Run(bg(), f)
	assertShape(t, r, "cage.fence:shop.example.com", StagePHP, Pass)
}

func TestCageFenceSevenCommands(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := fenceFixture(l)
	r := CageFence(l).Run(bg(), f)
	if len(r.Evidence) != 7 || len(f.Ran()) != 7 {
		t.Fatalf("Evidence = %d, lệnh = %d, muốn 7", len(r.Evidence), len(f.Ran()))
	}
	want := []string{
		"id -un",
		"ls -A /var/www",
		"ls -A /tmp",
		"ls -A /run",
		"find /proc -maxdepth 1 -type d -regex '/proc/[0-9]+' '!' -user " + l.User,
		"getent passwd",
		"test -w /etc",
	}
	for i, c := range f.Ran() {
		if c.AsUser != l.User {
			t.Errorf("lệnh %d không chạy dưới user của site: %+v", i, c)
		}
		got := system.Cmd{Name: c.Name, Args: c.Args}.String()
		if got != want[i] {
			t.Errorf("lệnh %d = %q, muốn %q", i, got, want[i])
		}
	}
}

func TestCageFenceFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	cases := []struct {
		name string
		set  func(f *system.FakeSystem)
		want string
	}{
		{"id ra tên khác", func(f *system.FakeSystem) { reply(f, []string{"id", "-un"}, 0, "root\n", "") }, "id -un"},
		{"thấy site khác trong /var/www", func(f *system.FakeSystem) {
			reply(f, []string{"ls", "-A", "/var/www"}, 0, "blog.example.org\nshop.example.com\n", "")
		}, "ls -A /var/www"},
		{"không thấy cả domain của mình", func(f *system.FakeSystem) { reply(f, []string{"ls", "-A", "/var/www"}, 0, "", "") }, "ls -A /var/www"},
		{"/tmp không rỗng", func(f *system.FakeSystem) { reply(f, []string{"ls", "-A", "/tmp"}, 0, "sess_abc\n", "") }, "ls -A /tmp"},
		{"/run có thêm thứ", func(f *system.FakeSystem) {
			reply(f, []string{"ls", "-A", "/run"}, 0, "mysqld\nphp\nsystemd\n", "")
		}, "ls -A /run"},
		{"/run thiếu systemd", func(f *system.FakeSystem) { reply(f, []string{"ls", "-A", "/run"}, 0, "mysqld\n", "") }, "ls -A /run"},
		{"thấy tiến trình của user khác", func(f *system.FakeSystem) { reply(f, []string{"find", "/proc"}, 0, "/proc/1\n", "") }, "find /proc"},
		{"getent ra 5 dòng", func(f *system.FakeSystem) {
			reply(f, []string{"getent", "passwd"}, 0, "root:x:0\nwww-data:x:33\n"+l.User+":x:1001\nnobody:x:65534\ntp_other:x:1002\n", "")
		}, "getent passwd"},
		{"getent ra user lạ thay vì nobody", func(f *system.FakeSystem) {
			reply(f, []string{"getent", "passwd"}, 0, "root:x:0\nwww-data:x:33\n"+l.User+":x:1001\ntp_other:x:1002\n", "")
		}, "getent passwd"},
		{"ghi được vào /etc", func(f *system.FakeSystem) { reply(f, []string{"test", "-w", "/etc"}, 0, "", "") }, "test -w /etc"},
		{"ls báo lỗi", func(f *system.FakeSystem) { reply(f, []string{"ls", "-A", "/tmp"}, 2, "", "ls: cannot access '/tmp'") }, "ls -A /tmp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := fenceFixture(l)
			c.set(f)
			r := CageFence(l).Run(bg(), f)
			assertShape(t, r, "cage.fence:shop.example.com", StagePHP, Fail)
			if !strings.Contains(r.Summary.Vi, c.want) {
				t.Errorf("Summary thiếu %q: %q", c.want, r.Summary.Vi)
			}
			if strings.Contains(r.Summary.Vi, "runuser") || strings.Contains(r.Summary.En, "runuser") {
				t.Errorf("Summary không được mang lệnh runuser: %q", r.Summary.Vi)
			}
			if len(r.Evidence) != 7 {
				t.Errorf("một lệnh sai không được làm bỏ các lệnh sau: %d bằng chứng", len(r.Evidence))
			}
			// Dòng cuối là lệnh đã sai, in như sẽ chạy thật.
			if !strings.HasPrefix(lastFix(r), "runuser -u "+l.User+" -- "+strings.Fields(c.want)[0]) {
				t.Errorf("ManualFix cuối = %q", lastFix(r))
			}
		})
	}
}

func TestCageFenceUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.fence:shop.example.com"
	t.Run("test -w /etc mã 1 kèm lỗi không phải câu trả lời bị từ chối", func(t *testing.T) {
		f := fenceFixture(l)
		reply(f, []string{"test", "-w", "/etc"}, 1, "", "runuser: user tp_x does not exist")
		assertShape(t, CageFence(l).Run(bg(), f), id, StagePHP, Unknown)
	})
	t.Run("find lỗi mà không in gì", func(t *testing.T) {
		f := fenceFixture(l)
		reply(f, []string{"find", "/proc"}, 1, "", "find: '/proc/123': No such file or directory")
		assertShape(t, CageFence(l).Run(bg(), f), id, StagePHP, Unknown)
	})
	t.Run("Sai và chưa rõ cùng lúc thì Fail", func(t *testing.T) {
		f := fenceFixture(l)
		reply(f, []string{"find", "/proc"}, 1, "", "find: x")
		reply(f, []string{"ls", "-A", "/tmp"}, 0, "x\n", "")
		assertShape(t, CageFence(l).Run(bg(), f), id, StagePHP, Fail)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, CageFence(l).Run(cancelledCtx(), fenceFixture(l)), id, StagePHP, Unknown)
	})
	t.Run("Layout zero", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, CageFence(site.Layout{}).Run(bg(), f), "cage.fence:", StagePHP, Unknown)
		assertNoCmd(t, f)
	})
}

// Thiếu file hàng rào thì lớp bọc lệnh không đưa lệnh vào lồng: bảy lệnh sẽ chạy
// ngoài lồng dưới uid của site. Phép kiểm không được chạy chúng.
func TestCageFenceMissingFenceRunsNothing(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := fenceFixture(l)
	_ = f.Remove(l.CageFence())
	f.ResetRan()
	r := CageFence(l).Run(bg(), f)
	assertShape(t, r, "cage.fence:shop.example.com", StagePHP, Fail)
	assertNoCmd(t, f)
	if !strings.Contains(r.Summary.Vi, l.CageFence()) {
		t.Errorf("Summary = %q", r.Summary.Vi)
	}
}

func TestCageFenceDescribe(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := fenceFixture(l)
	reply(f, []string{"ls", "-A", "/tmp"}, 0, "leak\n", "")
	sys := descSys{f}
	r := CageFence(l).Run(bg(), sys)
	assertShape(t, r, "cage.fence:shop.example.com", StagePHP, Fail)
	// Bằng chứng và dòng sửa tay là lệnh thật sẽ chạy: systemd-run, không phải runuser.
	if got := r.Evidence[2].Command; !strings.HasPrefix(got, "systemd-run --unit=x -p User="+l.User+" -- ls -A /tmp") {
		t.Errorf("bằng chứng = %q", got)
	}
	if want := "systemd-run --unit=x -p User=" + l.User + " -- ls -A /tmp"; lastFix(r) != want {
		t.Errorf("ManualFix cuối = %q, muốn %q", lastFix(r), want)
	}
	for _, s := range r.ManualFix {
		if strings.Contains(s, "runuser") {
			t.Errorf("ManualFix còn dòng runuser: %q", s)
		}
	}
}

// ---- cage.limits ----

const cgDirMode = fs.FileMode(0o755)

func cageLimitsFixture(l site.Layout, files map[string]string) *system.FakeSystem {
	f := system.NewFake()
	f.SetDir(l.CageCgroup(), cgDirMode, system.Root)
	for name, data := range files {
		f.SetFile(l.CageCgroup()+"/"+name, []byte(data), 0o644, system.Root)
	}
	return f
}

var cageWant = account.Limits{CPUPercent: 50, RAMMB: 200, Tasks: 40, IOReadMB: 4, IOWriteMB: 5}

func cageFilesOK() map[string]string {
	return map[string]string{
		"memory.max":      "209715200\n",
		"memory.swap.max": "0\n",
		"cpu.max":         "50000 100000\n",
		"pids.max":        "40\n",
		"io.max":          "8:0 rbps=4000000 wbps=5000000 riops=max wiops=max\n",
	}
}

func TestCageLimitsPass(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.limits:shop.example.com"
	t.Run("đúng gói", func(t *testing.T) {
		f := cageLimitsFixture(l, cageFilesOK())
		r := CageLimits(l, cageWant).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
		assertNoCmd(t, f)
		if len(r.Evidence) != 1 || !strings.Contains(r.Evidence[0].Output, "memory.max: want 209715200 byte, applied 209715200 byte") {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("gói không trần, cgroup toàn max", func(t *testing.T) {
		f := cageLimitsFixture(l, map[string]string{
			"memory.max": "max\n", "memory.swap.max": "max\n", "cpu.max": "max 100000\n", "pids.max": "max\n", "io.max": "",
		})
		assertShape(t, CageLimits(l, account.Limits{}).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("gói không trần, file không có (kể cả swap.max)", func(t *testing.T) {
		f := cageLimitsFixture(l, nil)
		assertShape(t, CageLimits(l, account.Limits{}).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("chỉ có trần CPU và RAM, swap max không có dòng", func(t *testing.T) {
		f := cageLimitsFixture(l, map[string]string{"memory.max": "67108864\n", "memory.swap.max": "0\n", "cpu.max": "150000 100000\n"})
		assertShape(t, CageLimits(l, account.Limits{CPUPercent: 150, RAMMB: 64}).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("slice chưa có cgroup: đạt kèm lý do", func(t *testing.T) {
		f := system.NewFake()
		r := CageLimits(l, cageWant).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
		if !strings.Contains(r.Summary.Vi, "chưa chạy từ khi máy khởi động") || !strings.Contains(r.Summary.Vi, "trần sẽ áp khi lồng chạy") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("io.max chỉ có một thiết bị: so rbps và wbps của dòng đầu", func(t *testing.T) {
		files := cageFilesOK()
		files["io.max"] = "8:0 rbps=4000000 wbps=5000000 riops=max wiops=max\n8:16 rbps=1 wbps=1\n"
		assertShape(t, CageLimits(l, cageWant).Run(bg(), cageLimitsFixture(l, files)), id, StagePHP, Pass)
	})
}

func TestCageLimitsFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.limits:shop.example.com"
	cases := []struct {
		name  string
		file  string
		value string
		want  []string
	}{
		{"memory.max sai", "memory.max", "104857600\n", []string{"memory.max: mong đợi 209715200 byte", "đang áp 104857600 byte"}},
		{"memory.max là max", "memory.max", "max\n", []string{"memory.max", "đang áp không giới hạn"}},
		// MB (10^6) khác MiB (2^20): 200 MB thập phân không phải 200 MiB.
		{"memory.max tính nhầm MB", "memory.max", "200000000\n", []string{"memory.max"}},
		{"swap.max là max", "memory.swap.max", "max\n", []string{"memory.swap.max: mong đợi 0 byte", "đang áp không giới hạn"}},
		{"swap.max không có file", "memory.swap.max", "", []string{"memory.swap.max"}},
		{"cpu.max thấp hơn", "cpu.max", "25000 100000\n", []string{"cpu.max: mong đợi 50 %", "đang áp 25 %"}},
		{"cpu.max là max", "cpu.max", "max 100000\n", []string{"cpu.max", "không giới hạn"}},
		{"pids.max sai", "pids.max", "30\n", []string{"pids.max: mong đợi 40", "đang áp 30"}},
		{"pids.max là max", "pids.max", "max\n", []string{"pids.max", "không giới hạn"}},
		{"rbps sai", "io.max", "8:0 rbps=1048576 wbps=5000000\n", []string{"io.max rbps: mong đợi 4000000 byte/s", "đang áp 1048576"}},
		{"wbps sai", "io.max", "8:0 rbps=4000000 wbps=max\n", []string{"io.max wbps", "không giới hạn"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := cageFilesOK()
			if c.value == "" {
				delete(files, c.file)
			} else {
				files[c.file] = c.value
			}
			r := CageLimits(l, cageWant).Run(bg(), cageLimitsFixture(l, files))
			assertShape(t, r, id, StagePHP, Fail)
			for _, w := range c.want {
				if !strings.Contains(r.Summary.Vi, w) {
					t.Errorf("Summary thiếu %q: %q", w, r.Summary.Vi)
				}
			}
			if !strings.Contains(r.Summary.En, "expected") || !cageAnyHas(r.ManualFix, "systemctl cat "+l.CageSlice()) {
				t.Errorf("Summary.En = %q, ManualFix = %q", r.Summary.En, r.ManualFix)
			}
		})
	}
	t.Run("lệch từng trường được nêu riêng", func(t *testing.T) {
		files := cageFilesOK()
		files["memory.max"], files["pids.max"] = "max\n", "max\n"
		r := CageLimits(l, cageWant).Run(bg(), cageLimitsFixture(l, files))
		assertShape(t, r, id, StagePHP, Fail)
		if strings.Count(r.Summary.Vi, "mong đợi") != 2 {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("gói không trần mà cgroup còn trần", func(t *testing.T) {
		assertShape(t, CageLimits(l, account.Limits{}).Run(bg(), cageLimitsFixture(l, cageFilesOK())), id, StagePHP, Fail)
	})
}

func TestCageLimitsUnits(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.limits:shop.example.com"
	// RAM tính bằng MiB (2^20): 64 MiB là 67108864.
	f := cageLimitsFixture(l, map[string]string{"memory.max": "67108864\n", "memory.swap.max": "0\n"})
	assertShape(t, CageLimits(l, account.Limits{RAMMB: 64}).Run(bg(), f), id, StagePHP, Pass)
	f = cageLimitsFixture(l, map[string]string{"memory.max": "64000000\n", "memory.swap.max": "0\n"})
	assertShape(t, CageLimits(l, account.Limits{RAMMB: 64}).Run(bg(), f), id, StagePHP, Fail)
	// Đĩa tính bằng MB (10^6): 1 MB là 1000000, không phải 1048576.
	f = cageLimitsFixture(l, map[string]string{"io.max": "8:0 rbps=1000000 wbps=max\n"})
	assertShape(t, CageLimits(l, account.Limits{IOReadMB: 1}).Run(bg(), f), id, StagePHP, Pass)
	f = cageLimitsFixture(l, map[string]string{"io.max": "8:0 rbps=1048576 wbps=max\n"})
	assertShape(t, CageLimits(l, account.Limits{IOReadMB: 1}).Run(bg(), f), id, StagePHP, Fail)
}

func TestCageLimitsDiskWarn(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.limits:shop.example.com"
	t.Run("gói có trần đĩa mà io.max rỗng: cảnh báo, không sửa tự động", func(t *testing.T) {
		files := cageFilesOK()
		files["io.max"] = ""
		r := CageLimits(l, cageWant).Run(bg(), cageLimitsFixture(l, files))
		assertShape(t, r, id, StagePHP, Warn)
		if !strings.Contains(r.Summary.Vi, "trần đĩa không áp được trên loại đĩa này") || r.AutoFix != "" {
			t.Errorf("Summary = %q, AutoFix = %q", r.Summary.Vi, r.AutoFix)
		}
	})
	t.Run("io.max không có file", func(t *testing.T) {
		files := cageFilesOK()
		delete(files, "io.max")
		assertShape(t, CageLimits(l, cageWant).Run(bg(), cageLimitsFixture(l, files)), id, StagePHP, Warn)
	})
	t.Run("chỉ có trần ghi, io.max rỗng", func(t *testing.T) {
		files := cageFilesOK()
		files["io.max"] = ""
		assertShape(t, CageLimits(l, account.Limits{CPUPercent: 50, RAMMB: 200, Tasks: 40, IOWriteMB: 5}).Run(bg(), cageLimitsFixture(l, files)), id, StagePHP, Warn)
	})
	t.Run("có trường khác lệch thì Fail, câu về đĩa vẫn nằm trong Summary", func(t *testing.T) {
		files := cageFilesOK()
		files["io.max"] = ""
		files["pids.max"] = "max\n"
		r := CageLimits(l, cageWant).Run(bg(), cageLimitsFixture(l, files))
		assertShape(t, r, id, StagePHP, Fail)
		if !strings.Contains(r.Summary.Vi, "pids.max") || !strings.Contains(r.Summary.Vi, "trần đĩa không áp được") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("gói không có trần đĩa và io.max rỗng thì đạt", func(t *testing.T) {
		files := cageFilesOK()
		files["io.max"] = ""
		assertShape(t, CageLimits(l, account.Limits{CPUPercent: 50, RAMMB: 200, Tasks: 40}).Run(bg(), cageLimitsFixture(l, files)), id, StagePHP, Pass)
	})
}

func TestCageLimitsUnknown(t *testing.T) {
	f := system.NewFake()
	assertShape(t, CageLimits(site.Layout{}, cageWant).Run(bg(), f), "cage.limits:", StagePHP, Unknown)
}

// ---- cage.state ----

func stateFixture(l site.Layout, fence bool, pools ...string) *system.FakeSystem {
	f := system.NewFake()
	if fence {
		f.SetFile(l.CageFence(), []byte("[Service]\n"), 0o644, system.Root)
	}
	for _, v := range pools {
		f.SetFile("/etc/php/"+v+"/fpm/pool.d/"+l.Pool+".conf", []byte("[pool]\n"), 0o644, system.Root)
	}
	f.SetDir("/etc/php/8.3/fpm/pool.d", 0o755, system.Root)
	f.SetFile("/etc/php/8.3/fpm/pool.d/www.conf", []byte("[www]\n"), 0o644, system.Root)
	return f
}

func TestCageState(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "cage.state:shop.example.com"
	cases := []struct {
		name         string
		caged, want  bool
		fence        bool
		pools        []string
		status       Status
		sum, fix     string
		fixHasAction string
	}{
		{"trong lồng, gói muốn, đủ file", true, true, true, nil, Pass, "trong lồng", "", ""},
		{"ngoài lồng, gói không muốn, sạch", false, false, false, nil, Pass, "ngoài lồng", "", ""},
		{"trong lồng mà gói không còn muốn", true, false, true, nil, Warn, "không còn muốn lồng", "", "tpan cage disable shop.example.com"},
		{"gói muốn lồng mà site ngoài lồng", false, true, false, nil, Warn, "muốn lồng nhưng site chưa", "", "tpan cage enable shop.example.com"},
		{"trong lồng mà thiếu file hàng rào", true, true, false, nil, Fail, "thiếu file hàng rào", "", "tpan cage enable shop.example.com"},
		{"trong lồng mà còn pool chung", true, true, true, []string{"8.3"}, Fail, "còn file pool chung /etc/php/8.3/fpm/pool.d/" + l.Pool + ".conf", "", "tpan cage enable shop.example.com"},
		{"pool chung ở hai bản PHP", true, true, true, []string{"8.2", "8.3"}, Fail, "/etc/php/8.2/fpm/pool.d/", "", "tpan cage enable shop.example.com"},
		{"ngoài lồng mà còn file hàng rào", false, false, true, nil, Fail, "còn file hàng rào", "", "tpan cage disable shop.example.com"},
		{"ngoài lồng, gói muốn, còn file hàng rào", false, true, true, nil, Fail, "muốn lồng nhưng site chưa", "", "tpan cage disable shop.example.com"},
		{"thiếu hàng rào và gói không muốn: Fail thắng Warn", true, false, false, nil, Fail, "không còn muốn lồng", "", "tpan cage enable shop.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := stateFixture(l, c.fence, c.pools...)
			r := CageState(l, c.caged, c.want).Run(bg(), f)
			assertShape(t, r, id, StagePHP, c.status)
			if !strings.Contains(r.Summary.Vi, c.sum) {
				t.Errorf("Summary thiếu %q: %q", c.sum, r.Summary.Vi)
			}
			if c.fixHasAction != "" && !cageHas(r.ManualFix, c.fixHasAction) {
				t.Errorf("ManualFix thiếu %q: %q", c.fixHasAction, r.ManualFix)
			}
			if c.status != Pass && lastFix(r) != "ls -l "+l.CageFence() {
				t.Errorf("dòng cuối ManualFix = %q", lastFix(r))
			}
			assertNoCmd(t, f)
			if r.AutoFix != "" {
				t.Errorf("AutoFix phải để core điền: %q", r.AutoFix)
			}
		})
	}
	t.Run("không có /etc/php thì không có pool chung", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.CageFence(), []byte("x"), 0o644, system.Root)
		assertShape(t, CageState(l, true, true).Run(bg(), f), id, StagePHP, Pass)
	})
	t.Run("site ngoài lồng không bị đọc /etc/php", func(t *testing.T) {
		f := stateFixture(l, false, "8.3") // pool chung là chuyện bình thường của site ngoài lồng
		r := CageState(l, false, false).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
	})
	t.Run("Layout zero và pool lạ", func(t *testing.T) {
		assertShape(t, CageState(site.Layout{}, true, true).Run(bg(), system.NewFake()), "cage.state:", StagePHP, Unknown)
		bad := l
		bad.Pool = "x; rm"
		assertShape(t, CageState(bad, true, true).Run(bg(), system.NewFake()), id, StagePHP, Unknown)
	})
}

// ---- probe.leftover ----

func TestProbeLeftover(t *testing.T) {
	id := "probe.leftover:tpan"
	t.Run("không còn gì", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, ProbeLeftover(nil).Run(bg(), f), id, StageTPAN, Pass)
		assertShape(t, ProbeLeftover([]string{}).Run(bg(), f), id, StageTPAN, Pass)
		assertNoCmd(t, f)
	})
	t.Run("còn lượt chưa dọn", func(t *testing.T) {
		r := ProbeLeftover([]string{"lượt a1b2 bắt đầu 10:00", "lượt c3d4 bắt đầu 11:00"}).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Fail)
		if !strings.Contains(r.Summary.Vi, "Còn 2 lượt") || !strings.Contains(r.Summary.Vi, "a1b2") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !cageHas(r.ManualFix, "tpan isolation probe --cleanup --yes") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("danh sách dài được cắt", func(t *testing.T) {
		var left []string
		for i := 0; i < 25; i++ {
			left = append(left, fmt.Sprintf("run-%02d", i))
		}
		r := ProbeLeftover(left).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Fail)
		if !strings.Contains(r.Summary.Vi, "Còn 25 lượt") || strings.Contains(r.Summary.Vi, "run-24") || !strings.Contains(r.Summary.En, "15 more") {
			t.Errorf("Summary = %q / %q", r.Summary.Vi, r.Summary.En)
		}
	})
}

// ---- SiteChecksCaged ----

func TestSiteChecksCaged(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	dom := "shop.example.com"
	t.Run("không SSL", func(t *testing.T) {
		cs := SiteChecksCaged("nginx", l, cageWant)
		want := []struct {
			id    string
			stage Stage
			heavy bool
		}{
			{"web.vhost:" + dom, StageWeb, false},
			{"cage.socket:" + dom, StagePHP, false},
			{"cage.runtime:" + dom, StagePHP, false},
			{"cage.fence:" + dom, StagePHP, true},
			{"cage.limits:" + dom, StagePHP, false},
			{"wp.checksums:" + dom, StageWordPress, true},
			{"wp.config:" + dom, StageWordPress, false},
			{"db.access:" + dom, StageDB, false},
			{"http.site:" + dom, StageEndToEnd, false},
		}
		cageOrderCheck(t, cs, func(i int) (string, Stage, bool) { return want[i].id, want[i].stage, want[i].heavy }, len(want))
	})
	t.Run("có SSL", func(t *testing.T) {
		cs := SiteChecksSSLCaged("nginx", l, cageWant)
		want := []struct {
			id    string
			stage Stage
			heavy bool
		}{
			{"tls.cert:" + dom, StageTLS, false},
			{"web.vhost:" + dom, StageWeb, false},
			{"cage.socket:" + dom, StagePHP, false},
			{"cage.runtime:" + dom, StagePHP, false},
			{"cage.fence:" + dom, StagePHP, true},
			{"cage.limits:" + dom, StagePHP, false},
			{"wp.checksums:" + dom, StageWordPress, false},
			{"wp.config:" + dom, StageWordPress, false},
			{"db.access:" + dom, StageDB, false},
		}
		if len(cs) != len(want)+2 {
			t.Fatalf("có %d phép kiểm, muốn %d (thêm hai phép HTTP ở cuối)", len(cs), len(want)+2)
		}
		cageOrderCheck(t, cs[:len(want)], func(i int) (string, Stage, bool) { return want[i].id, want[i].stage, want[i].heavy }, len(want))
		for i, id := range []string{"http.redirect:" + dom, "https.site:" + dom} {
			if r := cs[len(want)+i].Run(bg(), system.NewFake()); r.ID != id {
				t.Errorf("phép kiểm cuối %d = %s, muốn %s", i, r.ID, id)
			}
		}
	})
}

func cageOrderCheck(t *testing.T, cs []Check, want func(i int) (string, Stage, bool), n int) {
	t.Helper()
	if len(cs) < n {
		t.Fatalf("có %d phép kiểm, muốn %d", len(cs), n)
	}
	// Máy trống: mọi phép chạy được không panic, đúng ID, đúng chặng, đúng cờ nặng.
	f := system.NewFake()
	for i := 0; i < n; i++ {
		id, stage, heavy := want(i)
		r := cs[i].Run(bg(), f)
		if r.ID != id || r.Stage != stage {
			t.Errorf("phép kiểm %d: %s/%s, muốn %s/%s", i, r.ID, r.Stage, id, stage)
		}
		if IsHeavy(cs[i]) != heavy {
			t.Errorf("%s: Heavy = %v, muốn %v", id, IsHeavy(cs[i]), heavy)
		}
		if r.Status == Fail && len(r.ManualFix) == 0 {
			t.Errorf("%s: Fail mà không có ManualFix", id)
		}
		if r.AutoFix != "" {
			t.Errorf("%s: AutoFix phải để core điền: %q", id, r.AutoFix)
		}
	}
}

// Mọi phép kiểm của site trong lồng chỉ đọc: không start, restart, reset-failed,
// kill; hệ thống file không đổi.
func TestSiteChecksCagedOnlyRead(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := runtimeFixture(l)
	f.SetFile(l.WPConfig, []byte("<?php"), 0o644, system.Root)
	f.SetSocket(l.CageSocket(), 0o660, wwwData)
	reply(f, []string{"systemctl", "is-active"}, 0, "active\n", "")
	reply(f, []string{"wp"}, 1, "", "Error: x")
	reply(f, []string{"curl"}, 0, httpOut("", "200"), "")
	before := f.Snapshot()
	cs := append(SiteChecksCaged("nginx", l, cageWant), SiteChecksSSLCaged("nginx", l, cageWant)...)
	cs = append(cs, CageState(l, true, true), ProbeLeftover([]string{"x"}))
	RunAll(bg(), f, cs)
	for _, c := range f.Ran() {
		switch c.Name {
		case "systemctl":
			if a := c.Args[0]; a != "show" && a != "is-active" {
				t.Errorf("systemctl %s sửa máy", a)
			}
		case "id", "stat", "ls", "find", "getent", "test", "wp", "curl", "nginx", "openssl":
		default:
			t.Errorf("lệnh không mong đợi: %s", c)
		}
	}
	after := f.Snapshot()
	if len(before) != len(after) {
		t.Fatalf("hệ thống file đổi: %d -> %d mục", len(before), len(after))
	}
	for p, e := range before {
		if after[p] != e {
			t.Errorf("%s đổi: %+v -> %+v", p, e, after[p])
		}
	}
}
