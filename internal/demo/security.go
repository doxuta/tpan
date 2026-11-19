package demo

// Phần bảo mật của máy giả (mục 6.8 của docs/KE-HOACH.md): trả lời đúng các
// lệnh mà checks.SecurityChecks chạy, và các lệnh của Plan "bật ufw", "cài
// fail2ban". Trạng thái lúc dựng: ufw đã cài nhưng tắt, fail2ban chưa cài,
// root SSH chỉ vào bằng khoá, SSH còn cho mật khẩu, có swap, 2 gói bảo mật
// chờ cập nhật, panel nghe 0.0.0.0:8443 (handler ss ở machine.go).

import (
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/system"
)

// ufwState là tường lửa của máy giả. Người gọi giữ m.mu khi đọc, ghi.
type ufwState struct {
	active bool
	rules  []string // luật đã thêm, dạng "22/tcp", theo thứ tự thêm
}

// securityFiles đặt /proc/swaps: một file swap 2 GiB, khớp SwapTotal của
// /proc/meminfo (writeProc).
func (m *machine) securityFiles() {
	m.SetFile("/proc/swaps", []byte("Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/swap.img                               file\t\t2097148\t\t9728\t\t-2\n"), 0o444, rootOwner)
}

func (m *machine) securityHandlers() {
	// Cấu hình hiệu lực của sshd (một phần đủ cho các phép kiểm).
	m.Handle([]string{"sshd", "-T"}, func(system.Cmd) system.Result {
		return exit(0, "port 22\naddressfamily any\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\npermitrootlogin prohibit-password\npubkeyauthentication yes\npasswordauthentication yes\nkbdinteractiveauthentication no\nusepam yes\n", "")
	})
	m.Handle([]string{"ufw"}, m.ufwCmd)
	m.Handle([]string{"fail2ban-client"}, m.fail2ban)
	m.Handle([]string{"dpkg-query", "--show"}, m.dpkgShow)
	m.Handle([]string{"apt-get", "install"}, m.aptInstall)
	// Mô phỏng nâng cấp của checks.SecurityUpdates: hai gói của kho -security.
	m.Handle([]string{"apt-get", "-s"}, func(system.Cmd) system.Result {
		return exit(0, strings.Join([]string{
			"NOTE: This is only a simulation!",
			"Reading package lists...",
			"Building dependency tree...",
			"Reading state information...",
			"Calculating upgrade...",
			"The following packages will be upgraded:",
			"  libssl3t64 openssl",
			"2 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.",
			"Inst libssl3t64 [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])",
			"Inst openssl [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])",
			"Conf libssl3t64 (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])",
			"Conf openssl (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])",
		}, "\n")+"\n", "")
	})
}

// ufwCmd giả lập các lệnh ufw mà phép kiểm và steps.UFWAllow, UFWEnable dùng,
// với output như ufw thật (ufw tắt thì "allow" in "Rules updated", bật thì
// "Rule added").
func (m *machine) ufwCmd(c system.Cmd) system.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := &m.ufw
	a := c.Args
	switch {
	case slices.Equal(a, []string{"status", "verbose"}), slices.Equal(a, []string{"status"}):
		if !u.active {
			return exit(0, "Status: inactive\n", "")
		}
		if len(a) == 1 {
			return exit(0, "Status: active\n\n"+ufwTable(u.rules), "")
		}
		return exit(0, "Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), deny (routed)\nNew profiles: skip\n\n"+ufwTable(u.rules), "")
	case slices.Equal(a, []string{"show", "added"}):
		out := "Added user rules (see 'ufw status' for running firewall):\n"
		if len(u.rules) == 0 {
			return exit(0, out+"(None)\n", "")
		}
		for _, r := range u.rules {
			out += "ufw allow " + r + "\n"
		}
		return exit(0, out, "")
	case len(a) == 2 && a[0] == "allow":
		if slices.Contains(u.rules, a[1]) {
			return exit(0, "Skipping adding existing rule\nSkipping adding existing rule (v6)\n", "")
		}
		u.rules = append(u.rules, a[1])
		if u.active {
			return exit(0, "Rule added\nRule added (v6)\n", "")
		}
		return exit(0, "Rules updated\nRules updated (v6)\n", "")
	case len(a) == 3 && a[0] == "delete" && a[1] == "allow":
		i := slices.Index(u.rules, a[2])
		if i < 0 {
			return exit(0, "Could not delete non-existent rule\nCould not delete non-existent rule (v6)\n", "")
		}
		u.rules = slices.Delete(u.rules, i, i+1)
		return exit(0, "Rule deleted\nRule deleted (v6)\n", "")
	case slices.Equal(a, []string{"--force", "enable"}):
		u.active = true
		return exit(0, "Firewall is active and enabled on system startup\n", "")
	case slices.Equal(a, []string{"disable"}):
		u.active = false
		return exit(0, "Firewall stopped and disabled on system startup\n", "")
	}
	return exit(1, "", "ERROR: máy demo không giả lập lệnh ufw "+strings.Join(a, " ")+"\n")
}

// ufwTable là bảng luật của `ufw status`: luật IPv4 trước, rồi bản (v6).
func ufwTable(rules []string) string {
	pad := func(s string) string { return s + strings.Repeat(" ", max(27-len(s), 2)) }
	out := pad("To") + "Action      From\n" + pad("--") + "------      ----\n"
	for _, r := range rules {
		out += pad(r) + "ALLOW IN    Anywhere\n"
	}
	for _, r := range rules {
		out += pad(r+" (v6)") + "ALLOW IN    Anywhere (v6)\n"
	}
	return out
}

// fail2ban giả lập fail2ban-client. Chưa cài thì trả mã 127 kèm câu báo của
// os/exec: FakeSystem chỉ trả được mã thoát, không trả được lỗi "không có
// chương trình" như LocalSystem, nên phép kiểm báo Warn "không báo được jail
// sshd" kèm câu đó thay vì Warn "chưa cài" (cùng mức Warn, cùng nút sửa).
func (m *machine) fail2ban(c system.Cmd) system.Result {
	m.mu.Lock()
	installed, running := !m.missing["fail2ban"], m.units["fail2ban"]
	m.mu.Unlock()
	if !installed {
		return exit(127, "", `exec: "fail2ban-client": executable file not found in $PATH`+"\n")
	}
	switch {
	case slices.Equal(c.Args, []string{"-t"}):
		return exit(0, "OK: configuration test is successful\n", "")
	case !running:
		return exit(255, "", "ERROR   Failed to access socket path: /var/run/fail2ban/fail2ban.sock. Is fail2ban running?\n")
	case slices.Equal(c.Args, []string{"status", "sshd"}):
		return exit(0, "Status for the jail: sshd\n|- Filter\n|  |- Currently failed:\t2\n|  |- Total failed:\t41\n|  `- Journal matches:\t_SYSTEMD_UNIT=sshd.service + _COMM=sshd\n`- Actions\n   |- Currently banned:\t3\n   |- Total banned:\t9\n   `- Banned IP list:\t198.51.100.23 198.51.100.77 203.0.113.200\n", "")
	case slices.Equal(c.Args, []string{"reload"}):
		return exit(0, "OK\n", "")
	}
	return okResult
}

// dpkgShow trả lời `dpkg-query --show --showformat=${Package}\t${Status}\n
// <gói>...` của steps.AptInstall: gói trong m.missing chưa cài (dpkg-query
// thoát mã 1 và báo ở stderr), mọi gói khác đã cài.
func (m *machine) dpkgShow(c system.Cmd) system.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out, errOut strings.Builder
	for _, p := range c.Args[1:] {
		if strings.HasPrefix(p, "-") {
			continue
		}
		if m.missing[p] {
			errOut.WriteString("dpkg-query: no packages found matching " + p + "\n")
			continue
		}
		out.WriteString(p + "\tinstall ok installed\n")
	}
	if errOut.Len() > 0 {
		return exit(1, out.String(), errOut.String())
	}
	return exit(0, out.String(), "")
}

// aptInstall "cài" gói: bỏ khỏi m.missing. fail2ban thì như gói thật trên
// Ubuntu: có thư mục jail.d, dịch vụ chạy và tự bật ngay sau khi cài.
func (m *machine) aptInstall(c system.Cmd) system.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range c.Args[1:] {
		if strings.HasPrefix(p, "-") || !m.missing[p] {
			continue
		}
		delete(m.missing, p)
		if p == "fail2ban" {
			m.SetDir("/etc/fail2ban/jail.d", 0o755, rootOwner)
			m.units["fail2ban"] = true
		}
	}
	return okResult
}
