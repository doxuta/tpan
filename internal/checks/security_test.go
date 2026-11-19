package checks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// Output mẫu của sshd -T, ufw, fail2ban-client, ss, apt-get trong file này viết
// theo tài liệu của từng lệnh (man sshd, ufw, fail2ban-client, ss, apt-get) cho
// Ubuntu 24.04, chưa lấy từ máy ảo thật: phần này nằm trong mục "cần kiểm trên máy ảo".

func sshdOut(root, pass string, ports ...string) string {
	var sb strings.Builder
	for _, p := range ports {
		sb.WriteString("port " + p + "\n")
	}
	sb.WriteString("addressfamily any\nlistenaddress 0.0.0.0:22\npermitrootlogin " + root + "\npasswordauthentication " + pass + "\nsubsystem sftp /usr/lib/openssh/sftp-server\n")
	return sb.String()
}

// notInstalled giả lập máy chưa có lệnh name, đúng như LocalSystem báo lỗi:
// FakeSystem không có cách tự tạo lỗi này.
type notInstalled struct {
	system.System
	name string
}

func (n notInstalled) Run(ctx context.Context, cmd system.Cmd) (system.Result, error) {
	if cmd.Name == n.name {
		return system.Result{ExitCode: -1}, errors.New("chạy " + cmd.String() + ": exec: \"" + cmd.Name + "\": executable file not found in $PATH")
	}
	return n.System.Run(ctx, cmd)
}

// ---- SSHDConfig ----

func TestSSHDConfig(t *testing.T) {
	f := system.NewFake()
	reply(f, []string{"sshd", "-T"}, 0, sshdOut("without-password", "yes", "22", "2222"), "")
	cfg, ev, err := SSHDConfig(bg(), f)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg["port"], ","); got != "22,2222" {
		t.Errorf("port = %q", got)
	}
	if cfg["permitrootlogin"][0] != "without-password" || cfg["passwordauthentication"][0] != "yes" {
		t.Errorf("cfg = %v", cfg)
	}
	if cfg["subsystem"][0] != "sftp /usr/lib/openssh/sftp-server" {
		t.Errorf("giá trị nhiều từ phải giữ nguyên: %q", cfg["subsystem"])
	}
	if ev.Command != "sshd -T" || ev.ExitCode != 0 || !strings.Contains(ev.Output, "permitrootlogin") {
		t.Errorf("Evidence = %+v", ev)
	}
}

func TestSSHDConfigKeysAreLowercase(t *testing.T) {
	f := system.NewFake()
	reply(f, []string{"sshd", "-T"}, 0, "PermitRootLogin yes\n\n   \nPort 22\n", "")
	cfg, _, err := SSHDConfig(bg(), f)
	if err != nil || cfg["permitrootlogin"][0] != "yes" || cfg["port"][0] != "22" {
		t.Errorf("cfg = %v, err = %v", cfg, err)
	}
}

func TestSSHDConfigErrors(t *testing.T) {
	t.Run("không đủ quyền", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"sshd", "-T"}, 1, "", "sshd: no hostkeys available -- exiting.\n")
		cfg, ev, err := SSHDConfig(bg(), f)
		if err == nil || cfg != nil || !strings.Contains(err.Error(), "no hostkeys") || !strings.Contains(err.Error(), "mã 1") {
			t.Errorf("cfg = %v, err = %v", cfg, err)
		}
		if ev.ExitCode != 1 || ev.Command != "sshd -T" {
			t.Errorf("Evidence = %+v", ev)
		}
	})
	t.Run("không chạy được", func(t *testing.T) {
		_, ev, err := SSHDConfig(cancelledCtx(), system.NewFake())
		if err == nil || ev.ExitCode != -1 {
			t.Errorf("err = %v, ev = %+v", err, ev)
		}
	})
}

// ---- SSHRootLogin, SSHPasswordAuth ----

func TestSSHChecks(t *testing.T) {
	cases := []struct {
		name    string
		check   func() Check
		id      string
		stdout  string
		exit    int
		want    Status
		summary string
	}{
		{"root yes", SSHRootLogin, "sec.ssh.root", sshdOut("yes", "no", "22"), 0, Warn, "PermitRootLogin yes"},
		{"root Yes viết hoa", SSHRootLogin, "sec.ssh.root", sshdOut("Yes", "no", "22"), 0, Warn, "PermitRootLogin yes"},
		{"root prohibit-password", SSHRootLogin, "sec.ssh.root", sshdOut("prohibit-password", "yes", "22"), 0, Pass, "prohibit-password"},
		{"root without-password", SSHRootLogin, "sec.ssh.root", sshdOut("without-password", "yes", "22"), 0, Pass, "without-password"},
		{"root forced-commands-only", SSHRootLogin, "sec.ssh.root", sshdOut("forced-commands-only", "yes", "22"), 0, Pass, "forced-commands-only"},
		{"root no", SSHRootLogin, "sec.ssh.root", sshdOut("no", "yes", "22"), 0, Pass, "no"},
		{"root: sshd -T lỗi", SSHRootLogin, "sec.ssh.root", "", 1, Unknown, "không đọc được cấu hình sshd"},
		{"root: thiếu khoá", SSHRootLogin, "sec.ssh.root", "port 22\n", 0, Unknown, "permitrootlogin"},
		{"mật khẩu yes", SSHPasswordAuth, "sec.ssh.password", sshdOut("no", "yes", "22"), 0, Warn, "PasswordAuthentication yes"},
		{"mật khẩu no", SSHPasswordAuth, "sec.ssh.password", sshdOut("yes", "no", "22"), 0, Pass, "no"},
		{"mật khẩu: sshd -T lỗi", SSHPasswordAuth, "sec.ssh.password", "", 255, Unknown, "không đọc được cấu hình sshd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			reply(f, []string{"sshd", "-T"}, c.exit, c.stdout, "")
			r := c.check().Run(bg(), f)
			assertShape(t, r, c.id, StageMachine, c.want)
			assertEvidenceIsRan(t, r, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.want == Warn {
				if lastFix(r) != "sshd -T" || !strings.Contains(strings.Join(r.ManualFix, "\n"), "sshd -t") {
					t.Errorf("ManualFix = %q", r.ManualFix)
				}
				// Chỉ cảnh báo: không có lệnh nào tự sửa file cấu hình.
				for _, l := range r.ManualFix {
					if strings.HasPrefix(l, "sed ") || strings.Contains(l, " > ") {
						t.Errorf("ManualFix không được tự sửa file: %q", l)
					}
				}
			}
		})
	}
}

// ---- parseUFW và UFW ----

const ufwActive = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
80,443/tcp                 ALLOW IN    Anywhere
8443/tcp                   ALLOW IN    Anywhere
22/tcp (v6)                ALLOW IN    Anywhere (v6)
80,443/tcp (v6)            ALLOW IN    Anywhere (v6)
`

// ufwNoSSH là ufwActive bỏ cả hai dòng của cổng 22 (v4 và v6).
var ufwNoSSH = strings.NewReplacer(
	"22/tcp                     ALLOW IN    Anywhere\n", "",
	"22/tcp (v6)                ALLOW IN    Anywhere (v6)\n", "").Replace(ufwActive)

func ufwFake(ufwOut string, ufwExit int, sshd string, sshdExit int) *system.FakeSystem {
	f := system.NewFake()
	reply(f, []string{"ufw"}, ufwExit, ufwOut, "")
	reply(f, []string{"sshd", "-T"}, sshdExit, sshd, "")
	return f
}

func TestUFW(t *testing.T) {
	const id = "sec.ufw"
	cases := []struct {
		name     string
		ufw      string
		ufwExit  int
		sshd     string
		sshdExit int
		want     Status
		summary  string
		fix      string // đoạn phải có trong ManualFix
	}{
		{"bật, có SSH 22", ufwActive, 0, sshdOut("no", "no", "22"), 0, Pass, "22/tcp; 80,443/tcp; 8443/tcp", ""},
		{"tắt", "Status: inactive\n", 0, "", 0, Warn, "đang tắt", "tpan security ufw"},
		{"bật nhưng chặn SSH", ufwNoSSH, 0, sshdOut("no", "no", "22"), 0, Fail, "không cho cổng SSH 22", "ufw allow 22/tcp"},
		{"SSH ở cổng 2222, ufw chỉ mở 22", ufwActive, 0, sshdOut("no", "no", "2222"), 0, Fail, "2222", "ufw allow 2222/tcp"},
		{"hai cổng SSH, thiếu một", ufwActive, 0, sshdOut("no", "no", "22", "2222"), 0, Fail, "2222", "ufw allow 2222/tcp"},
		{"SSH ở cổng 2222 và ufw có 2222", strings.Replace(ufwActive, "22/tcp ", "2222/tcp", 1), 0, sshdOut("no", "no", "2222"), 0, Pass, "2222/tcp", ""},
		{"khoảng cổng chứa cổng SSH", "Status: active\nDefault: deny (incoming)\n\nTo  Action  From\n--  ------  ----\n2000:3000/tcp  ALLOW IN  Anywhere\n", 0, sshdOut("no", "no", "2222"), 0, Pass, "2000:3000/tcp", ""},
		{"luật tên ứng dụng OpenSSH", "Status: active\nDefault: deny (incoming)\n\nTo  Action  From\n--  ------  ----\nOpenSSH  ALLOW IN  Anywhere\nOpenSSH (v6)  ALLOW IN  Anywhere (v6)\n", 0, sshdOut("no", "no", "22"), 0, Pass, "OpenSSH", ""},
		{"cổng UDP không tính", "Status: active\nDefault: deny (incoming)\n\nTo  Action  From\n--  ------  ----\n22/udp  ALLOW IN  Anywhere\n", 0, sshdOut("no", "no", "22"), 0, Fail, "22", "ufw allow 22/tcp"},
		{"luật chặn (DENY) không tính", "Status: active\nDefault: deny (incoming)\n\nTo  Action  From\n--  ------  ----\n22/tcp  DENY IN  Anywhere\n", 0, sshdOut("no", "no", "22"), 0, Fail, "22", "ufw allow 22/tcp"},
		{"luật ra (ALLOW OUT) không tính", "Status: active\nDefault: deny (incoming)\n\nTo  Action  From\n--  ------  ----\n22/tcp  ALLOW OUT  Anywhere\n", 0, sshdOut("no", "no", "22"), 0, Fail, "22", "ufw allow 22/tcp"},
		{"luật LIMIT cho SSH tính là mở", "Status: active\nDefault: deny (incoming)\n\nTo  Action  From\n--  ------  ----\n22/tcp  LIMIT IN  Anywhere\n", 0, sshdOut("no", "no", "22"), 0, Pass, "22/tcp", ""},
		{"chính sách mặc định cho vào là allow", "Status: active\nDefault: allow (incoming), allow (outgoing), disabled (routed)\n", 0, sshdOut("no", "no", "22"), 0, Pass, "allow", ""},
		{"bật nhưng không đọc được sshd -T", ufwActive, 0, "", 1, Unknown, "cổng SSH", ""},
		{"sshd -T không có cổng", ufwActive, 0, "addressfamily any\n", 0, Unknown, "không cho biết cổng SSH", ""},
		{"ufw không đủ quyền", "ERROR: You need to be root to run this script\n", 1, "", 0, Unknown, "mã 1", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := ufwFake(c.ufw, c.ufwExit, c.sshd, c.sshdExit)
			r := UFW().Run(bg(), f)
			assertShape(t, r, id, StageMachine, c.want)
			assertEvidenceIsRan(t, r, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.fix != "" && !strings.Contains(strings.Join(r.ManualFix, "\n"), c.fix) {
				t.Errorf("ManualFix thiếu %q: %q", c.fix, r.ManualFix)
			}
			if c.want == Fail || c.want == Warn {
				if lastFix(r) != "ufw status verbose" {
					t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
				}
			}
		})
	}
}

func TestUFWInactiveDoesNotNeedSSHD(t *testing.T) {
	f := ufwFake("Status: inactive\n", 0, "", 1)
	r := UFW().Run(bg(), f)
	assertShape(t, r, "sec.ufw", StageMachine, Warn)
	if ran := f.Ran(); len(ran) != 1 || ran[0].String() != "ufw status verbose" {
		t.Errorf("lệnh đã chạy = %v", ran)
	}
}

func TestUFWNotInstalled(t *testing.T) {
	r := UFW().Run(bg(), notInstalled{System: system.NewFake(), name: "ufw"})
	assertShape(t, r, "sec.ufw", StageMachine, Warn)
	if !strings.Contains(r.Summary.Vi, "chưa cài") || len(r.Evidence) != 1 || r.Evidence[0].ExitCode != -1 {
		t.Errorf("Summary = %q, Evidence = %+v", r.Summary.Vi, r.Evidence)
	}
}

func TestUFWCannotRun(t *testing.T) {
	assertShape(t, UFW().Run(cancelledCtx(), system.NewFake()), "sec.ufw", StageMachine, Unknown)
}

func TestParseUFWIPv6Only(t *testing.T) {
	// "80,443/tcp (v6)" không được làm mất danh sách; dòng v6 trùng dòng v4 chỉ hiện một lần.
	st := parseUFW(ufwActive)
	if !st.active || st.defaultIn != "deny" {
		t.Errorf("st = %+v", st)
	}
	if got := strings.Join(st.open, "|"); got != "22/tcp|80,443/tcp|8443/tcp" {
		t.Errorf("open = %q", got)
	}
	for _, p := range []int{22, 80, 443, 8443} {
		if !st.allows(p) {
			t.Errorf("phải cho cổng %d", p)
		}
	}
	if st.allows(3306) || st.allows(21) {
		t.Error("không được cho cổng ngoài luật")
	}
}

// ---- Fail2banSSHD ----

const f2bOK = `Status for the jail: sshd
|- Filter
|  |- Currently failed:	1
|  |- Total failed:	10
|  ` + "`" + `- File list:	/var/log/auth.log
` + "`" + `- Actions
   |- Currently banned:	3
   |- Total banned:	5
   ` + "`" + `- Banned IP list:	203.0.113.5 203.0.113.6 203.0.113.7
`

func TestFail2banSSHD(t *testing.T) {
	const id = "sec.fail2ban"
	cases := []struct {
		name    string
		stdout  string
		stderr  string
		exit    int
		want    Status
		summary string
	}{
		{"đạt, 3 IP bị chặn", f2bOK, "", 0, Pass, "3 IP"},
		{"đạt, không đọc được số IP", "Status for the jail: sshd\n", "", 0, Pass, "đang chạy"},
		{"không có jail sshd", "", "Sorry but the jail 'sshd' does not exist\n", 1, Warn, "Sorry but the jail"},
		{"fail2ban chưa chạy", "", "2026-11-19 ERROR   Failed to access socket path: /var/run/fail2ban/fail2ban.sock. Is fail2ban running?\n", 255, Warn, "Failed to access socket"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			reply(f, []string{"fail2ban-client"}, c.exit, c.stdout, c.stderr)
			r := Fail2banSSHD().Run(bg(), f)
			assertShape(t, r, id, StageMachine, c.want)
			assertEvidenceIsRan(t, r, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.want == Warn && lastFix(r) != "fail2ban-client status sshd" {
				t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
			}
		})
	}
	t.Run("chưa cài", func(t *testing.T) {
		r := Fail2banSSHD().Run(bg(), notInstalled{System: system.NewFake(), name: "fail2ban-client"})
		assertShape(t, r, id, StageMachine, Warn)
		if !strings.Contains(r.Summary.Vi, "chưa cài") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("không chạy được", func(t *testing.T) {
		assertShape(t, Fail2banSSHD().Run(cancelledCtx(), system.NewFake()), id, StageMachine, Unknown)
	})
}

// ---- Swap ----

func TestSwap(t *testing.T) {
	const header = "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"
	cases := []struct {
		name    string
		content string
		want    Status
		summary string
	}{
		{"có swapfile 2 GiB", header + "/swapfile                               file\t\t2097152\t\t0\t\t-2\n", Pass, "2048 MiB"},
		{"hai vùng swap cộng lại", header + "/swapfile file 1048576 0 -2\n/dev/sda2 partition 1048576 0 -3\n", Pass, "2048 MiB"},
		{"chỉ có dòng tiêu đề", header, Warn, "không có swap"},
		{"file rỗng", "", Warn, "không có swap"},
		{"dòng rác không tính", header + "rác\n/x file abc 0 0\n", Warn, "không có swap"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			f.SetFile("/proc/swaps", []byte(c.content), 0o444, system.Root)
			r := Swap().Run(bg(), f)
			assertShape(t, r, "sec.swap", StageMachine, c.want)
			assertNoCmd(t, f)
			if !strings.Contains(strings.ToLower(r.Summary.Vi), strings.ToLower(c.summary)) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if len(r.Evidence) != 1 || r.Evidence[0].Command != "cat /proc/swaps (tpan)" {
				t.Errorf("Evidence = %+v", r.Evidence)
			}
			if c.want == Warn && lastFix(r) != "swapon --show" {
				t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
			}
		})
	}
	t.Run("không đọc được", func(t *testing.T) {
		r := Swap().Run(bg(), system.NewFake())
		assertShape(t, r, "sec.swap", StageMachine, Unknown)
	})
}

// ---- SecurityUpdates ----

const aptSim = `NOTE: This is only a simulation!
      apt-get needs root privileges for real execution.
Reading package lists...
Building dependency tree...
The following packages will be upgraded:
  libssl3t64 openssl tzdata
3 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
Inst libssl3t64 [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])
Inst openssl [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])
Inst tzdata [2024a-2ubuntu1] (2024b-0ubuntu0.24.04 Ubuntu:24.04/noble-updates [all])
Conf libssl3t64 (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])
Conf openssl (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])
Conf tzdata (2024b-0ubuntu0.24.04 Ubuntu:24.04/noble-updates [all])
`

func TestSecurityUpdates(t *testing.T) {
	t.Run("hai gói bảo mật", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"apt-get"}, 0, aptSim, "")
		r := SecurityUpdates().Run(bg(), f)
		assertShape(t, r, "sec.updates", StageMachine, Warn)
		assertEvidenceIsRan(t, r, f)
		if !strings.HasPrefix(r.Summary.Vi, "2 gói") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if got := f.Ran()[0].String(); got != "apt-get -s -o Debug::NoLocking=1 dist-upgrade" {
			t.Errorf("lệnh = %q", got)
		}
		out := r.Evidence[0].Output
		if !strings.Contains(out, "Inst libssl3t64") || !strings.Contains(out, "Inst openssl") || strings.Contains(out, "tzdata") || strings.Contains(out, "Conf ") {
			t.Errorf("Evidence.Output chỉ nên có dòng bảo mật: %q", out)
		}
		if lastFix(r) != f.Ran()[0].String() {
			t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
		}
	})
	t.Run("không có gói bảo mật", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"apt-get"}, 0, "Reading package lists...\nInst tzdata [1] (2 Ubuntu:24.04/noble-updates [all])\n0 upgraded\n", "")
		assertShape(t, SecurityUpdates().Run(bg(), f), "sec.updates", StageMachine, Pass)
	})
	t.Run("máy đã cập nhật hết", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"apt-get"}, 0, "Reading package lists...\n0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", "")
		assertShape(t, SecurityUpdates().Run(bg(), f), "sec.updates", StageMachine, Pass)
	})
	t.Run("apt lỗi", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"apt-get"}, 100, "", "E: The package lists or status file could not be parsed or opened.\n")
		r := SecurityUpdates().Run(bg(), f)
		assertShape(t, r, "sec.updates", StageMachine, Unknown)
		if !strings.Contains(r.Summary.Vi, "mã 100") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("không chạy được", func(t *testing.T) {
		assertShape(t, SecurityUpdates().Run(cancelledCtx(), system.NewFake()), "sec.updates", StageMachine, Unknown)
	})
}

// ---- PanelListen, PanelExposed ----

const (
	ssPanelLoop = "LISTEN 0 4096 127.0.0.1:8443 0.0.0.0:*\n"
	ssPanelAll  = "LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:*\nLISTEN 0 4096 [::]:8443 [::]:*\n"
)

func ssFake(out string, exit int) *system.FakeSystem {
	f := system.NewFake()
	reply(f, []string{"ss"}, exit, out, "")
	return f
}

func TestPanelListen(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		addrs   string
		exposed bool
	}{
		{"chỉ loopback", ssPanelLoop, "127.0.0.1", false},
		{"IPv6 loopback", "LISTEN 0 4096 [::1]:8443 [::]:*\n", "::1", false},
		{"mọi địa chỉ", ssPanelAll, "0.0.0.0,::", true},
		{"loopback và một địa chỉ ngoài", ssPanelLoop + "LISTEN 0 4096 203.0.113.7:8443 0.0.0.0:*\n", "127.0.0.1,203.0.113.7", true},
		{"dấu * không đọc được coi là ngoài", "LISTEN 0 4096 *:8443 *:*\n", "*", true},
		{"cổng khác không tính (ss bỏ qua bộ lọc)", "LISTEN 0 4096 0.0.0.0:80 0.0.0.0:*\nLISTEN 0 4096 0.0.0.0:84430 0.0.0.0:*\n", "", false},
		{"không ai nghe", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := ssFake(c.out, 0)
			addrs, exposed, ev, err := PanelListen(bg(), f, 8443)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(addrs, ","); got != c.addrs || exposed != c.exposed {
				t.Errorf("addrs = %q, exposed = %v; muốn %q, %v", got, exposed, c.addrs, c.exposed)
			}
			if got := f.Ran()[0].String(); got != "ss -tlnH sport = :8443" || ev.Command != got {
				t.Errorf("lệnh = %q, Evidence = %+v", got, ev)
			}
		})
	}
	t.Run("ss lỗi", func(t *testing.T) {
		_, _, ev, err := PanelListen(bg(), ssFake("", 1), 8443)
		if err == nil || ev.ExitCode != 1 {
			t.Errorf("err = %v, ev = %+v", err, ev)
		}
	})
	t.Run("không chạy được", func(t *testing.T) {
		_, _, _, err := PanelListen(cancelledCtx(), system.NewFake(), 8443)
		if err == nil {
			t.Error("muốn lỗi")
		}
	})
	for _, p := range []int{0, -1, 65536} {
		t.Run("cổng không hợp lệ", func(t *testing.T) {
			f := system.NewFake()
			if _, _, _, err := PanelListen(bg(), f, p); err == nil {
				t.Errorf("cổng %d phải bị từ chối", p)
			}
			assertNoCmd(t, f)
		})
	}
}

func TestPanelExposed(t *testing.T) {
	const id = "sec.panel"
	t.Run("riêng tư", func(t *testing.T) {
		f := ssFake(ssPanelLoop, 0)
		r := PanelExposed(8443).Run(bg(), f)
		assertShape(t, r, id, StageMachine, Pass)
		assertEvidenceIsRan(t, r, f)
		if !strings.Contains(r.Summary.Vi, "127.0.0.1") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("mở ra ngoài", func(t *testing.T) {
		f := ssFake(ssPanelAll, 0)
		r := PanelExposed(8443).Run(bg(), f)
		assertShape(t, r, id, StageMachine, Warn)
		fix := strings.Join(r.ManualFix, "\n")
		if !strings.Contains(fix, "tpan setup --private") || !strings.Contains(fix, "ssh -L 8443:127.0.0.1:8443") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		if lastFix(r) != "ss -tlnH sport = :8443" {
			t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
		}
	})
	t.Run("không ai nghe", func(t *testing.T) {
		r := PanelExposed(8443).Run(bg(), ssFake("", 0))
		assertShape(t, r, id, StageMachine, Unknown)
		if !strings.Contains(r.Summary.Vi, "8443") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("ss lỗi", func(t *testing.T) {
		assertShape(t, PanelExposed(8443).Run(bg(), ssFake("", 1)), id, StageMachine, Unknown)
	})
	t.Run("cổng không hợp lệ", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, PanelExposed(0).Run(bg(), f), id, StageMachine, Unknown)
		assertNoCmd(t, f)
	})
}

// ---- SecurityChecks ----

func TestSecurityChecksOrderAndReadOnly(t *testing.T) {
	want := []string{"sec.ufw", "sec.fail2ban", "sec.ssh.root", "sec.ssh.password", "sec.swap", "sec.updates", "sec.panel"}
	cs := SecurityChecks(8443)
	if len(cs) != len(want) {
		t.Fatalf("có %d phép kiểm, muốn %d", len(cs), len(want))
	}
	f := ufwFake(ufwActive, 0, sshdOut("yes", "yes", "22"), 0)
	reply(f, []string{"fail2ban-client"}, 0, f2bOK, "")
	reply(f, []string{"apt-get"}, 0, aptSim, "")
	reply(f, []string{"ss"}, 0, ssPanelAll, "")
	f.SetFile("/proc/swaps", []byte("Filename Type Size Used Priority\n"), 0o444, system.Root)
	before := f.Snapshot()
	results := RunAll(bg(), f, cs)
	for i, r := range results {
		if r.ID != want[i] || r.Stage != StageMachine {
			t.Errorf("phép kiểm %d: %s/%s, muốn %s/machine", i, r.ID, r.Stage, want[i])
		}
		assertText(t, "Title "+r.ID, r.Title)
		if r.Status == Fail && len(r.ManualFix) == 0 {
			t.Errorf("%s: Fail mà không có ManualFix", r.ID)
		}
		if r.Status != Pass && r.Status != Unknown && len(r.ManualFix) == 0 {
			t.Errorf("%s: %s mà không có ManualFix", r.ID, r.Status)
		}
	}
	wantStatus := []Status{Pass, Pass, Warn, Warn, Warn, Warn, Warn}
	for i, r := range results {
		if r.Status != wantStatus[i] {
			t.Errorf("%s: %s, muốn %s (%s)", r.ID, r.Status, wantStatus[i], r.Summary.Vi)
		}
	}
	// Chỉ đọc: chỉ các lệnh chỉ đọc, hệ thống file không đổi.
	for _, c := range f.Ran() {
		switch c.Name + " " + strings.Join(c.Args, " ") {
		case "ufw status verbose", "sshd -T", "fail2ban-client status sshd", "apt-get -s -o Debug::NoLocking=1 dist-upgrade", "ss -tlnH sport = :8443":
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

// Máy trống (chưa cài gì, lệnh nào cũng trả rỗng mã 0): mọi phép kiểm chạy được mà không panic.
func TestSecurityChecksEmptyMachine(t *testing.T) {
	for _, r := range RunAll(bg(), system.NewFake(), SecurityChecks(8443)) {
		assertText(t, "Title "+r.ID, r.Title)
		assertText(t, "Summary "+r.ID, r.Summary)
		if r.Status == Fail {
			t.Errorf("%s: trang bảo mật chủ yếu cảnh báo, không Fail trên máy trống: %s", r.ID, r.Summary.Vi)
		}
	}
}
