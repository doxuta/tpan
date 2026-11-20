package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// Hai dòng /proc lấy từ máy ảo M14 (Ubuntu 24.04, systemd 255): dịch vụ có
// ProtectProc=invisible được systemd gắn một /proc riêng với hidepid=invisible.
const (
	mountinfoHidden = "533 502 0:25 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw\n" +
		"604 502 0:59 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw,hidepid=invisible\n"
	mountinfoOpen = "533 502 0:25 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw\n" +
		"26 30 0:24 / /proc rw,nosuid,nodev,noexec,relatime shared:13 - proc proc rw\n"
)

// procFake trả ProtectProc=shown cho systemd và MainPID=pid; mountinfo khác
// rỗng thì đặt file /proc/<pid>/mountinfo.
func procFake(shown, pid, mountinfo string) *system.FakeSystem {
	f := system.NewFake()
	f.Handle([]string{"systemctl", "show"}, func(c system.Cmd) system.Result {
		if len(c.Args) > 3 && c.Args[3] == "MainPID" {
			return system.Result{Stdout: pid + "\n"}
		}
		return system.Result{Stdout: shown + "\n"}
	})
	if mountinfo != "" {
		f.SetFile("/proc/"+pid+"/mountinfo", []byte(mountinfo), 0o444, system.Root)
	}
	return f
}

func TestPHPProtectProcHoiTienTrinhDangChay(t *testing.T) {
	const id = "php.protectproc:8.3"

	t.Run("systemd và tiến trình đều invisible: đạt, có bằng chứng mountinfo", func(t *testing.T) {
		r := PHPProtectProc("8.3").Run(bg(), procFake("invisible", "4242", mountinfoHidden))
		assertShape(t, r, id, StagePHP, Pass)
		if len(r.Evidence) != 2 || !strings.Contains(r.Evidence[1].Output, "hidepid=invisible") {
			t.Errorf("bằng chứng = %+v", r.Evidence)
		}
	})
	t.Run("đã daemon-reload nhưng chưa restart: cảnh báo, chỉ lệnh restart", func(t *testing.T) {
		r := PHPProtectProc("8.3").Run(bg(), procFake("invisible", "4242", mountinfoOpen))
		assertShape(t, r, id, StagePHP, Warn)
		if r.ManualFix[0] != "systemctl restart php8.3-fpm" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("không biết MainPID hoặc không đọc được mountinfo: tin systemd", func(t *testing.T) {
		for _, f := range []*system.FakeSystem{
			procFake("invisible", "0", ""),
			procFake("invisible", "", ""),
			procFake("invisible", "4242", ""),
		} {
			assertShape(t, PHPProtectProc("8.3").Run(bg(), f), id, StagePHP, Pass)
		}
	})
}

func TestProcHidden(t *testing.T) {
	cases := []struct {
		name, pid, mountinfo string
		hidden, known        bool
	}{
		{"ẩn", "7", mountinfoHidden, true, true},
		{"không ẩn", "7", mountinfoOpen, false, true},
		// Gắn chồng: dòng /proc cuối cùng là cái tiến trình đang thấy.
		{"gắn chồng, dòng cuối ẩn", "7", mountinfoOpen + mountinfoHidden, true, true},
		{"dịch vụ không chạy", "0", "", false, false},
		{"không có dòng /proc", "7", "533 502 0:25 / /sys rw - sysfs sysfs rw\n", false, false},
		{"PID lạ", "abc", "", false, false},
	}
	for _, c := range cases {
		hidden, known, _ := ProcHidden(bg(), procFake("invisible", c.pid, c.mountinfo), "php8.3-fpm")
		if hidden != c.hidden || known != c.known {
			t.Errorf("%s: hidden=%v known=%v, muốn %v %v", c.name, hidden, known, c.hidden, c.known)
		}
	}
}
