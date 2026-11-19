package checks

import (
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

func sha512Of(data string) string {
	s := sha512.Sum512([]byte(data))
	return hex.EncodeToString(s[:])
}

// ---- WPCLI ----

func TestWPCLIPass(t *testing.T) {
	fake := system.NewFake()
	fake.SetFile("/usr/local/bin/wp", []byte("#!/usr/bin/env php\nphar"), 0o755, system.Root)
	want := sha512Of("#!/usr/bin/env php\nphar")

	r := WPCLI(want).Run(bg(), fake)
	assertShape(t, r, "wpcli.binary:wp", StageMachine, Pass)
	assertNoCmd(t, fake)
	if len(r.Evidence) != 2 || !strings.Contains(r.Evidence[0].Output, "mode 0755") || !strings.Contains(r.Evidence[1].Output, want) {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
	// Chữ hoa trong mã mong đợi vẫn khớp.
	assertShape(t, WPCLI(strings.ToUpper(want)).Run(bg(), fake), "wpcli.binary:wp", StageMachine, Pass)
}

func TestWPCLIFail(t *testing.T) {
	want := sha512Of("ban-that")
	t.Run("không có file", func(t *testing.T) {
		r := WPCLI(want).Run(bg(), system.NewFake())
		assertShape(t, r, "wpcli.binary:wp", StageMachine, Fail)
		if lastFix(r) != "ls -l /usr/local/bin/wp" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("sai sha512", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/usr/local/bin/wp", []byte("ban-gia"), 0o755, system.Root)
		r := WPCLI(want).Run(bg(), fake)
		assertShape(t, r, "wpcli.binary:wp", StageMachine, Fail)
		if !strings.Contains(r.Summary.Vi, "sha512") || strings.Contains(r.Summary.Vi, "quyền") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if lastFix(r) != "sha512sum /usr/local/bin/wp" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("sai quyền", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/usr/local/bin/wp", []byte("ban-that"), 0o777, system.Root)
		r := WPCLI(want).Run(bg(), fake)
		assertShape(t, r, "wpcli.binary:wp", StageMachine, Fail)
		if !strings.Contains(r.Summary.Vi, "0777") || r.ManualFix[0] != "chmod 755 /usr/local/bin/wp" {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("cả hai", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/usr/local/bin/wp", []byte("ban-gia"), 0o644, system.Root)
		r := WPCLI(want).Run(bg(), fake)
		assertShape(t, r, "wpcli.binary:wp", StageMachine, Fail)
		if !strings.Contains(r.Summary.Vi, "quyền") || !strings.Contains(r.Summary.Vi, "sha512") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("là symlink", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/opt/wp", []byte("ban-that"), 0o755, system.Root)
		fake.SetSymlink("/opt/wp", "/usr/local/bin/wp")
		assertShape(t, WPCLI(want).Run(bg(), fake), "wpcli.binary:wp", StageMachine, Fail)
	})
}

func TestWPCLIUnknown(t *testing.T) {
	// Fake không cho tiêm lỗi vào Stat/ReadFile, nên "không kiểm được" ở đây đến từ đầu vào.
	for _, bad := range []string{"", "abc", strings.Repeat("g", 128), strings.Repeat("a", 127), strings.Repeat("a", 129)} {
		fake := system.NewFake()
		fake.SetFile("/usr/local/bin/wp", []byte("x"), 0o755, system.Root)
		r := WPCLI(bad).Run(bg(), fake)
		assertShape(t, r, "wpcli.binary:wp", StageMachine, Unknown)
	}
}

// ---- Disk ----

func setUsed(f *system.FakeSystem, usedPct uint64) {
	// Tổng 1000 byte, đã dùng usedPct*10 byte -> đúng usedPct%.
	f.SetDisk("/", system.DiskUsage{Total: 1000, Free: 1000 - usedPct*10})
}

func TestDiskNguong(t *testing.T) {
	cases := []struct {
		used uint64
		want Status
	}{
		{0, Pass}, {79, Pass}, {80, Warn}, {89, Warn}, {90, Fail}, {99, Fail}, {100, Fail},
	}
	for _, c := range cases {
		fake := system.NewFake()
		setUsed(fake, c.used)
		r := Disk("/").Run(bg(), fake)
		assertShape(t, r, "disk.usage:/", StageMachine, c.want)
		if c.want != Pass && lastFix(r) != "df -h /" {
			t.Errorf("%d%%: ManualFix = %q", c.used, r.ManualFix)
		}
	}
}

func TestDiskBangChung(t *testing.T) {
	fake := system.NewFake()
	setUsed(fake, 85)
	r := Disk("/").Run(bg(), fake)
	assertNoCmd(t, fake)
	if len(r.Evidence) != 1 || r.Evidence[0].Command != "statfs / (tpan)" || !strings.Contains(r.Evidence[0].Output, "used=85.0%") {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
	if !strings.Contains(r.Summary.Vi, "85.0%") {
		t.Errorf("Summary = %q", r.Summary.Vi)
	}
}

func TestDiskUnknown(t *testing.T) {
	t.Run("đường dẫn không có", func(t *testing.T) {
		r := Disk("/khong/co").Run(bg(), system.NewFake())
		assertShape(t, r, "disk.usage:/khong/co", StageMachine, Unknown)
	})
	t.Run("tổng bằng 0", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetDisk("/", system.DiskUsage{Total: 0, Free: 0})
		assertShape(t, Disk("/").Run(bg(), fake), "disk.usage:/", StageMachine, Unknown)
	})
	t.Run("trống nhiều hơn tổng", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetDisk("/", system.DiskUsage{Total: 10, Free: 20})
		assertShape(t, Disk("/").Run(bg(), fake), "disk.usage:/", StageMachine, Unknown)
	})
	t.Run("đường dẫn tương đối", func(t *testing.T) {
		assertShape(t, Disk("var").Run(bg(), system.NewFake()), "disk.usage:var", StageMachine, Unknown)
	})
}

func TestDiskManualFixQuoteDuongDanLa(t *testing.T) {
	fake := system.NewFake()
	fake.SetDir("/var/a b; id", 0o755, system.Root)
	fake.SetDisk("/var", system.DiskUsage{Total: 1000, Free: 10})
	r := Disk("/var/a b; id").Run(bg(), fake)
	assertShape(t, r, "disk.usage:/var/a b; id", StageMachine, Fail)
	if lastFix(r) != "df -h '/var/a b; id'" {
		t.Errorf("đường dẫn lạ phải được đặt trong nháy: %q", lastFix(r))
	}
}

// ---- OSRelease ----

// Dạng thật của /etc/os-release trên Ubuntu 24.04 (os-release(5)).
const ubuntu2404 = `PRETTY_NAME="Ubuntu 24.04.1 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
VERSION="24.04.1 LTS (Noble Numbat)"
VERSION_CODENAME=noble
ID=ubuntu
ID_LIKE=debian
HOME_URL="https://www.ubuntu.com/"
`

func TestOSRelease(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/etc/os-release", []byte(ubuntu2404), 0o644, system.Root)
		r := OSRelease("ubuntu", "24.04").Run(bg(), fake)
		assertShape(t, r, "os.release:ubuntu-24.04", StageMachine, Pass)
		assertNoCmd(t, fake)
		if !strings.Contains(r.Evidence[0].Output, `VERSION_ID="24.04"`) {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("fail: sai bản", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/etc/os-release", []byte(strings.ReplaceAll(ubuntu2404, "24.04", "22.04")), 0o644, system.Root)
		r := OSRelease("ubuntu", "24.04").Run(bg(), fake)
		assertShape(t, r, "os.release:ubuntu-24.04", StageMachine, Fail)
		if !strings.Contains(r.Summary.Vi, "ubuntu 22.04") || lastFix(r) != "cat /etc/os-release" {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("fail: sai distro", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetFile("/etc/os-release", []byte("ID=debian\nVERSION_ID='12'\n"), 0o644, system.Root)
		assertShape(t, OSRelease("ubuntu", "24.04").Run(bg(), fake), "os.release:ubuntu-24.04", StageMachine, Fail)
	})
	t.Run("fail: không có file", func(t *testing.T) {
		assertShape(t, OSRelease("ubuntu", "24.04").Run(bg(), system.NewFake()), "os.release:ubuntu-24.04", StageMachine, Fail)
	})
	t.Run("unknown: là thư mục chứ không phải file", func(t *testing.T) {
		fake := system.NewFake()
		fake.SetDir("/etc/os-release", 0o755, system.Root) // ReadFile trên thư mục lỗi, không phải "không tồn tại"
		assertShape(t, OSRelease("ubuntu", "24.04").Run(bg(), fake), "os.release:ubuntu-24.04", StageMachine, Unknown)
	})
}

func TestParseOSRelease(t *testing.T) {
	kv := parseOSRelease("# ghi chú\n\nID=ubuntu\nVERSION_ID=\"24.04\"\nNAME='Ub'\nKHONGCO\nA=\"x\n")
	for k, v := range map[string]string{"ID": "ubuntu", "VERSION_ID": "24.04", "NAME": "Ub", "A": `"x`} {
		if kv[k] != v {
			t.Errorf("%s = %q, muốn %q", k, kv[k], v)
		}
	}
	if _, ok := kv["KHONGCO"]; ok {
		t.Error("dòng không có dấu = phải bị bỏ")
	}
}
