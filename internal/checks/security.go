package checks

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Các dòng của trang bảo mật (mục 6.8 của docs/KE-HOACH.md). Hợp đồng ở
// ssl_types.go. Mọi phép kiểm ở đây thuộc StageMachine, chỉ đọc, và chủ yếu
// CẢNH BÁO: Fail chỉ dành cho chỗ đang nguy hiểm thật (ufw bật mà chặn mất cổng
// SSH). Tên hàm không xuất của file này có tiền tố "sec".

// secBase dựng base của một dòng bảo mật.
func secBase(id string, title i18n.Text) base {
	return base{id: id, stage: StageMachine, title: title}
}

// secNotInstalled cho biết lệnh không có trên máy. LocalSystem trả lỗi của
// os/exec ("exec: \"ufw\": executable file not found in $PATH"); gói này không
// được import os/exec (test tĩnh của system), nên nhận ra bằng chữ trong lỗi.
func secNotInstalled(err error) bool {
	return err != nil && strings.Contains(err.Error(), "executable file not found")
}

// ---- sshd ----

// SSHDConfig chạy `sshd -T`, lệnh in cấu hình HIỆU LỰC của sshd (đã gộp
// sshd_config và sshd_config.d, đã điền mặc định). Đọc file cấu hình bằng tay
// sẽ sai ngay khi có một dòng Include hay một mặc định ngầm; hỏi chính sshd thì không.
//
// Kết quả: khoá viết thường (sshd -T đã in thường) kèm MỌI giá trị, mỗi dòng
// một phần tử, vì "port" có thể xuất hiện nhiều lần. Cần quyền root (sshd cần
// đọc khoá host); không chạy được thì trả lỗi để phép kiểm gọi nó báo Unknown.
// Không phải Check: core cũng gọi nó để biết cổng SSH thật trước khi bật ufw.
func SSHDConfig(ctx context.Context, sys system.System) (map[string][]string, Evidence, error) {
	res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "sshd", Args: []string{"-T"}})
	if err != nil {
		return nil, ev, fmt.Errorf("chạy sshd -T: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, ev, fmt.Errorf("sshd -T thoát với mã %d: %s", res.ExitCode, sslClean(firstLineWith(joinOutput(res))))
	}
	cfg := map[string][]string{}
	for _, ln := range strings.Split(res.Stdout, "\n") {
		key, val, _ := strings.Cut(strings.TrimSpace(ln), " ")
		if key == "" {
			continue
		}
		key = strings.ToLower(key)
		cfg[key] = append(cfg[key], strings.TrimSpace(val))
	}
	return cfg, ev, nil
}

// secSSHD là khuôn của hai dòng SSH: lấy khoá key từ SSHDConfig, Warn khi giá
// trị đầu tiên là warnValue, Pass khi khác (kể cả các biến thể an toàn hơn).
// sshd chỉ nhận một giá trị cho hai khoá này, nên giá trị đầu là giá trị hiệu lực.
func secSSHD(id string, title i18n.Text, key, warnValue string, summary func(val string, warn bool) i18n.Text, cause i18n.Text, fix []string) Check {
	b := secBase(id, title)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		cfg, ev, err := SSHDConfig(ctx, sys)
		if err != nil {
			return b.unknown(i18n.T("không đọc được cấu hình sshd ("+sslClean(err.Error())+")", "could not read the sshd configuration ("+sslClean(err.Error())+")"), ev)
		}
		vals := cfg[key]
		if len(vals) == 0 || vals[0] == "" {
			return b.unknown(i18n.T("sshd -T không in khoá "+key, "sshd -T did not print the key "+key), ev)
		}
		val := strings.ToLower(vals[0])
		if val == warnValue {
			return b.bad(Warn, summary(sslClean(val), true), cause, fix, ev)
		}
		return b.pass(summary(sslClean(val), false), ev)
	})
}

// secSSHDFix là các lệnh sửa tay cho một khoá của sshd. Không có lệnh nào tự
// sửa file: đổi sai là mất đường vào máy (mục 6.8), nên người đọc tự sửa ở bước
// giữa, kiểm cú pháp bằng sshd -t, rồi mới nạp lại.
func secSSHDFix(key string) []string {
	return []string{
		line("grep", "-rn", "-i", key, "/etc/ssh/sshd_config", "/etc/ssh/sshd_config.d"),
		line("sshd", "-t"),
		line("systemctl", "reload", "ssh"),
		line("sshd", "-T"),
	}
}

// SSHRootLogin kiểm root có đăng nhập SSH bằng mật khẩu được không. "yes" là
// Warn; các giá trị còn lại (đăng nhập bằng khoá, hoặc cấm hẳn) là Pass. Chỉ
// cảnh báo, không tự sửa.
func SSHRootLogin() Check {
	return secSSHD("sec.ssh.root",
		i18n.T("Root không đăng nhập SSH bằng mật khẩu", "Root cannot log in over SSH with a password"),
		"permitrootlogin", "yes",
		func(val string, warn bool) i18n.Text {
			if warn {
				return i18n.T("SSH cho root đăng nhập (PermitRootLogin yes)", "SSH lets root log in (PermitRootLogin yes)")
			}
			return i18n.T("PermitRootLogin là "+val+": root không vào bằng mật khẩu được", "PermitRootLogin is "+val+": root cannot log in with a password")
		},
		i18n.T("Ai đoán được mật khẩu root là vào được cả máy; máy nào mở SSH ra internet cũng bị dò mật khẩu cả ngày. TPAN không tự sửa vì sửa sai là mất đường vào. Cách làm: có sẵn một user thường đăng nhập bằng khoá và dùng sudo, GIỮ phiên đó mở, rồi đặt \"PermitRootLogin prohibit-password\" trong file tìm thấy ở lệnh đầu, kiểm bằng sshd -t, nạp lại, và thử đăng nhập bằng một cửa sổ mới trước khi đóng phiên cũ.",
			"Anyone who guesses the root password owns the whole machine; any machine with SSH open to the internet gets password guesses all day. TPAN does not fix it for you because a wrong fix locks you out. The way: have a normal user that logs in with a key and uses sudo, KEEP that session open, then set \"PermitRootLogin prohibit-password\" in the file found by the first command, check with sshd -t, reload, and test a login from a new window before closing the old session."),
		secSSHDFix("PermitRootLogin"))
}

// SSHPasswordAuth kiểm SSH còn cho đăng nhập bằng mật khẩu không ("yes" là Warn).
func SSHPasswordAuth() Check {
	return secSSHD("sec.ssh.password",
		i18n.T("SSH không cho đăng nhập bằng mật khẩu", "SSH does not allow password login"),
		"passwordauthentication", "yes",
		func(val string, warn bool) i18n.Text {
			if warn {
				return i18n.T("SSH còn cho đăng nhập bằng mật khẩu (PasswordAuthentication yes)", "SSH still allows password login (PasswordAuthentication yes)")
			}
			return i18n.T("PasswordAuthentication là "+val, "PasswordAuthentication is "+val)
		},
		i18n.T("Mật khẩu bị dò được bằng cách thử hàng loạt; khoá SSH thì không. TPAN không tự sửa vì sửa sai là mất đường vào. Cách làm: thêm khoá công khai của bạn vào ~/.ssh/authorized_keys của user sẽ dùng, thử đăng nhập bằng khoá, GIỮ phiên đó mở, rồi đặt \"PasswordAuthentication no\" trong file tìm thấy ở lệnh đầu, kiểm bằng sshd -t, nạp lại, và thử từ một cửa sổ mới.",
			"Passwords can be brute-forced; SSH keys cannot. TPAN does not fix it for you because a wrong fix locks you out. The way: add your public key to ~/.ssh/authorized_keys of the user you will use, test a key login, KEEP that session open, then set \"PasswordAuthentication no\" in the file found by the first command, check with sshd -t, reload, and test from a new window."),
		secSSHDFix("PasswordAuthentication"))
}

// ---- ufw ----

// ufwPortRE đọc cột "To" của `ufw status`: danh sách cổng hoặc khoảng, có thể
// kèm giao thức ("22/tcp", "80,443/tcp", "60000:61000/udp", "22").
var ufwPortRE = regexp.MustCompile(`^([0-9]+(?::[0-9]+)?(?:,[0-9]+(?::[0-9]+)?)*)(?:/(tcp|udp))?$`)

var ufwDefaultInRE = regexp.MustCompile(`(?i)(\w+) \(incoming\)`)

// ufwColSplit tách các cột của bảng luật: ufw ngăn cột bằng từ hai dấu cách trở
// lên, còn trong một ô chỉ có một dấu cách ("ALLOW IN", "22/tcp (v6)").
var ufwColSplit = regexp.MustCompile(`\s{2,}`)

type ufwRange struct{ lo, hi int }

type ufwState struct {
	active    bool
	defaultIn string     // "deny", "allow", "reject"... (chữ thường); rỗng nếu không đọc được
	open      []string   // ô "To" của các luật cho vào, bỏ trùng, giữ thứ tự
	ranges    []ufwRange // các cổng TCP được cho vào
}

func (s ufwState) allows(port int) bool {
	for _, r := range s.ranges {
		if port >= r.lo && port <= r.hi {
			return true
		}
	}
	return false
}

// parseUFW đọc output của `ufw status verbose`. Chỉ tính luật cho VÀO (ALLOW,
// LIMIT; bỏ "ALLOW OUT" và "ALLOW FWD") và chỉ cổng TCP, vì sshd nghe TCP.
// Luật dạng tên ứng dụng "OpenSSH"/"SSH" được tính là cổng 22: đó là cổng hồ sơ
// ứng dụng của ufw định nghĩa (/etc/ufw/applications.d/openssh-server).
func parseUFW(out string) ufwState {
	var s ufwState
	inRules := false
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r \t")
		low := strings.ToLower(strings.TrimSpace(ln))
		switch {
		case strings.HasPrefix(low, "status:"):
			s.active = strings.TrimSpace(low[len("status:"):]) == "active"
			continue
		case strings.HasPrefix(low, "default:"):
			if m := ufwDefaultInRE.FindStringSubmatch(ln); m != nil {
				s.defaultIn = strings.ToLower(m[1])
			}
			continue
		case strings.HasPrefix(low, "--"):
			inRules = true
			continue
		}
		if !inRules || strings.TrimSpace(ln) == "" {
			continue
		}
		cols := ufwColSplit.Split(strings.TrimSpace(ln), -1)
		if len(cols) < 2 {
			continue
		}
		act := strings.ToUpper(cols[1])
		if (!strings.HasPrefix(act, "ALLOW") && !strings.HasPrefix(act, "LIMIT")) || strings.Contains(act, "OUT") || strings.Contains(act, "FWD") {
			continue
		}
		to := strings.TrimSpace(strings.TrimSuffix(cols[0], "(v6)"))
		first := strings.Fields(to)
		if len(first) == 0 || strings.EqualFold(first[0], "anywhere") {
			continue
		}
		spec := first[0]
		if m := ufwPortRE.FindStringSubmatch(spec); m != nil {
			if m[2] == "udp" {
				continue
			}
			for _, part := range strings.Split(m[1], ",") {
				lo, hi, isRange := strings.Cut(part, ":")
				l, _ := strconv.Atoi(lo)
				h := l
				if isRange {
					h, _ = strconv.Atoi(hi)
				}
				s.ranges = append(s.ranges, ufwRange{l, h})
			}
		} else if strings.EqualFold(spec, "openssh") || strings.EqualFold(spec, "ssh") {
			s.ranges = append(s.ranges, ufwRange{22, 22})
		}
		if !containsStr(s.open, to) {
			s.open = append(s.open, to)
		}
	}
	return s
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// secSSHPorts lấy các cổng hợp lệ từ khoá "port" của SSHDConfig.
func secSSHPorts(cfg map[string][]string) []int {
	var ports []int
	for _, v := range cfg["port"] {
		if p, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && p > 0 && p < 65536 && !containsInt(ports, p) {
			ports = append(ports, p)
		}
	}
	return ports
}

// UFW kiểm tường lửa.
//
//   - Không có lệnh ufw hoặc đang tắt: Warn (máy chạy được, chỉ là không có lớp chặn cổng).
//   - Bật mà không cho cổng SSH nào sshd đang nghe: Fail. Đây là chỗ nguy hiểm
//     thật: phiên SSH đang mở còn sống, nhưng đăng nhập mới sẽ bị chặn.
//   - Bật và có cổng SSH: Pass, Summary liệt kê các cổng đang mở.
//
// Chính sách mặc định cho vào là "allow" thì ufw không chặn gì nên không có Fail
// vì thiếu luật SSH.
func UFW() Check {
	b := secBase("sec.ufw", i18n.T("Tường lửa ufw đang bật và cho cổng SSH", "The ufw firewall is on and allows the SSH port"))
	recheck := line("ufw", "status", "verbose")
	enable := line("tpan", "security", "ufw")
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "ufw", Args: []string{"status", "verbose"}})
		if secNotInstalled(err) {
			return b.bad(Warn,
				i18n.T("ufw chưa cài", "ufw is not installed"),
				i18n.T("Máy chưa có tường lửa: mọi cổng dịch vụ đang nghe đều mở ra internet. Nút bật trên trang này cài ufw, mở cổng SSH, 80, 443 rồi mới bật.",
					"The machine has no firewall: every listening service port is open to the internet. The enable button on this page installs ufw, opens SSH, 80 and 443, and only then turns it on."),
				[]string{enable, recheck}, ev)
		}
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			return b.unknown(i18n.T("ufw báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(joinOutput(res), "ERROR"))+")", "ufw failed (exit "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(joinOutput(res), "ERROR"))+")"), ev)
		}
		st := parseUFW(res.Stdout)
		if !st.active {
			return b.bad(Warn,
				i18n.T("ufw đang tắt", "ufw is off"),
				i18n.T("Tường lửa đã cài nhưng chưa bật nên mọi cổng đang nghe đều mở ra internet. Dùng lệnh của tpan để bật: nó mở cổng SSH, 80, 443 trước rồi mới bật, nên không tự khoá mình ngoài máy. Đừng gõ ufw enable khi chưa mở cổng SSH.",
					"The firewall is installed but off, so every listening port is open to the internet. Use the tpan command to turn it on: it opens SSH, 80 and 443 first and only then enables, so you do not lock yourself out. Do not type ufw enable before opening the SSH port."),
				[]string{enable, recheck}, ev)
		}

		cfg, sshEv, err := SSHDConfig(ctx, sys)
		if err != nil {
			return b.unknown(i18n.T("ufw đang bật nhưng không biết cổng SSH để đối chiếu ("+sslClean(err.Error())+")", "ufw is on but the SSH port is unknown ("+sslClean(err.Error())+")"), ev, sshEv)
		}
		ports := secSSHPorts(cfg)
		if len(ports) == 0 {
			return b.unknown(i18n.T("sshd -T không cho biết cổng SSH", "sshd -T gave no SSH port"), ev, sshEv)
		}
		if st.defaultIn == "allow" {
			return b.pass(i18n.T("ufw đang bật nhưng chính sách mặc định cho mọi kết nối vào (allow)", "ufw is on but the default policy allows all incoming (allow)"), ev, sshEv)
		}
		var missing []string
		for _, p := range ports {
			if !st.allows(p) {
				missing = append(missing, strconv.Itoa(p))
			}
		}
		if len(missing) > 0 {
			fix := []string{}
			for _, p := range missing {
				fix = append(fix, line("ufw", "allow", p+"/tcp"))
			}
			return b.bad(Fail,
				i18n.T("ufw đang bật nhưng không cho cổng SSH "+strings.Join(missing, ", "), "ufw is on but does not allow the SSH port "+strings.Join(missing, ", ")),
				i18n.T("Phiên SSH đang mở còn sống, nhưng đăng nhập mới sẽ bị chặn. Mở cổng SSH ngay, trước khi đóng phiên này.",
					"The SSH session you have open stays alive, but new logins will be blocked. Open the SSH port now, before closing this session."),
				append(fix, recheck), ev, sshEv)
		}
		return b.pass(i18n.T("ufw đang bật, cho vào: "+sslClean(strings.Join(st.open, "; ")), "ufw is on, allowing: "+sslClean(strings.Join(st.open, "; "))), ev, sshEv)
	})
}

// ---- fail2ban ----

var f2bBannedRE = regexp.MustCompile(`Currently banned:\s*([0-9]+)`)

// Fail2banSSHD kiểm jail sshd của fail2ban đang chạy. Mã thoát 0 là đạt; Summary
// nói số IP đang bị chặn. Không có lệnh: Warn "chưa cài". Mã khác (fail2ban chưa
// chạy, không có jail sshd) cũng Warn kèm dòng lỗi.
func Fail2banSSHD() Check {
	b := secBase("sec.fail2ban", i18n.T("fail2ban đang bảo vệ SSH", "fail2ban is protecting SSH"))
	fix := []string{line("tpan", "security", "fail2ban"), line("fail2ban-client", "status", "sshd")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "fail2ban-client", Args: []string{"status", "sshd"}})
		if secNotInstalled(err) {
			return b.bad(Warn,
				i18n.T("fail2ban chưa cài", "fail2ban is not installed"),
				i18n.T("Không có gì chặn IP dò mật khẩu SSH. Nút cài trên trang này cài fail2ban và bật jail sshd.",
					"Nothing blocks IPs that guess SSH passwords. The install button on this page installs fail2ban and enables the sshd jail."),
				fix, ev)
		}
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			why := sslClean(firstLineWith(joinOutput(res), "Sorry", "ERROR", "Error"))
			return b.bad(Warn,
				i18n.T("fail2ban không báo được jail sshd ("+why+")", "fail2ban cannot report the sshd jail ("+why+")"),
				i18n.T("fail2ban chưa chạy hoặc chưa có jail sshd, nên IP dò mật khẩu không bị chặn.",
					"fail2ban is not running or has no sshd jail, so IPs guessing passwords are not blocked."),
				fix, ev)
		}
		if m := f2bBannedRE.FindStringSubmatch(res.Stdout); m != nil {
			return b.pass(i18n.T("jail sshd đang chạy, "+m[1]+" IP đang bị chặn", "the sshd jail is running, "+m[1]+" IPs are banned"), ev)
		}
		return b.pass(i18n.T("jail sshd đang chạy", "the sshd jail is running"), ev)
	})
}

// ---- swap ----

// Swap đọc /proc/swaps. Không có vùng swap nào là Warn: máy hết RAM thì nhân
// giết tiến trình (thường là MariaDB), không có chỗ đệm.
//
// Bằng chứng ghi "cat /proc/swaps (tpan)" như phép kiểm /etc/os-release: tpan đọc
// file bằng Go và Output là nguyên nội dung file.
func Swap() Check {
	b := secBase("sec.swap", i18n.T("Máy có swap", "The machine has swap"))
	const file = "/proc/swaps"
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "cat " + file + " (tpan)"}
		data, err := sys.ReadFile(file)
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+file+" ("+err.Error()+")", "could not read "+file+" ("+err.Error()+")"), ev)
		}
		ev.Output = clip(strings.TrimRight(string(data), "\n"))
		var kib int64
		n := 0
		for _, ln := range strings.Split(string(data), "\n") {
			f := strings.Fields(ln)
			if len(f) < 3 || f[0] == "Filename" {
				continue
			}
			if sz, err := strconv.ParseInt(f[2], 10, 64); err == nil {
				kib += sz
				n++
			}
		}
		if n == 0 {
			return b.bad(Warn,
				i18n.T("Máy không có swap", "The machine has no swap"),
				i18n.T("Khi RAM hết, nhân Linux giết tiến trình để lấy lại bộ nhớ, và thường chọn MariaDB: site sập mà không có lỗi rõ ràng. Một file swap vài trăm MB là đủ làm đệm. Số 1G dưới đây là ví dụ, chọn theo ổ đĩa của bạn.",
					"When RAM runs out the Linux kernel kills processes to get memory back, and it often picks MariaDB: sites go down with no clear error. A swap file of a few hundred MB is enough as a buffer. The 1G below is an example, pick a size that fits your disk."),
				[]string{line("fallocate", "-l", "1G", "/swapfile"), line("chmod", "600", "/swapfile"), line("mkswap", "/swapfile"), line("swapon", "/swapfile"),
					"echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab", line("swapon", "--show")}, ev)
		}
		mib := strconv.FormatInt(kib/1024, 10)
		return b.pass(i18n.T("Có swap, tổng "+mib+" MiB", "Swap is on, "+mib+" MiB in total"), ev)
	})
}

// ---- gói bảo mật chờ cập nhật ----

// SecurityUpdates mô phỏng một lần nâng cấp (`apt-get -s`, không cài gì) và đếm
// các dòng "Inst" của kho -security. Debug::NoLocking=1 để mô phỏng không đòi
// khoá của dpkg: nó chạy được cả khi có một lần cập nhật khác đang chạy. Danh
// sách gói do apt update lần gần nhất ghi lại, nên kết quả chỉ mới như lần đó.
func SecurityUpdates() Check {
	b := secBase("sec.updates", i18n.T("Không có gói bảo mật chờ cập nhật", "No security updates are waiting"))
	cmd := system.Cmd{Name: "apt-get", Args: []string{"-s", "-o", "Debug::NoLocking=1", "dist-upgrade"}}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			return b.unknown(i18n.T("apt-get báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(joinOutput(res), "E:"))+")", "apt-get failed (exit "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(joinOutput(res), "E:"))+")"), ev)
		}
		var sec []string
		for _, ln := range strings.Split(res.Stdout, "\n") {
			if strings.HasPrefix(ln, "Inst ") && strings.Contains(ln, "-security") {
				sec = append(sec, strings.TrimRight(ln, "\r"))
			}
		}
		if len(sec) == 0 {
			return b.pass(i18n.T("Không có gói bảo mật nào chờ cập nhật", "No security updates are waiting"), ev)
		}
		// Output thật của mô phỏng có cả trăm dòng "Conf" đứng sau các dòng "Inst" và
		// clip giữ phần CUỐI, nên các dòng quan trọng sẽ bị cắt mất; chỉ giữ dòng bảo mật.
		ev.Output = clip("(chỉ các dòng Inst của kho -security / only the Inst lines of the -security pocket)\n" + strings.Join(sec, "\n"))
		n := strconv.Itoa(len(sec))
		return b.bad(Warn,
			i18n.T(n+" gói bảo mật đang chờ cập nhật", n+" security updates are waiting"),
			i18n.T("Có bản vá lỗi bảo mật chưa cài. Lỗ hổng đã công bố thì kẻ xấu có công cụ khai thác ngay. Cập nhật xong có thể cần khởi động lại máy nếu có bản vá nhân (kernel).",
				"Security fixes are not installed yet. Once a vulnerability is public attackers have tools for it right away. After updating you may need to reboot if a kernel fix was included."),
			[]string{line("apt-get", "update"), line("apt-get", "dist-upgrade"), cmd.String()}, ev)
	})
}

// ---- cổng của panel ----

// PanelListen trả các địa chỉ đang nghe cổng TCP port. Bộ lọc `sport = :<port>`
// của ss làm phần lớn việc, nhưng kết quả vẫn được lọc lại bằng parseSS: nếu một
// bản ss bỏ qua bộ lọc thì phép kiểm không coi cổng khác là cổng panel.
// exposed là có ít nhất một địa chỉ KHÔNG phải loopback; địa chỉ không đọc được
// ("*") tính là không loopback, để sai về phía an toàn.
func PanelListen(ctx context.Context, sys system.System, port int) (addrs []string, exposed bool, ev Evidence, err error) {
	if port < 1 || port > 65535 {
		return nil, false, Evidence{}, fmt.Errorf("cổng panel %d không hợp lệ", port)
	}
	res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "ss", Args: []string{"-tlnH", "sport", "=", ":" + strconv.Itoa(port)}})
	if err != nil {
		return nil, false, ev, fmt.Errorf("chạy ss: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, false, ev, fmt.Errorf("ss thoát với mã %d", res.ExitCode)
	}
	for _, l := range parseSS(res.Stdout) {
		if l.port != port {
			continue
		}
		addrs = append(addrs, addrText(l))
		if !l.loopback() {
			exposed = true
		}
	}
	return addrs, exposed, ev, nil
}

// PanelExposed kiểm cổng panel có mở ra ngoài không. Không ai nghe là Unknown
// (panel đang chạy thì phải có người nghe; không có thì chưa biết gì về việc mở ra ngoài).
// Chỉ loopback là Pass (chế độ riêng tư). Có địa chỉ ngoài loopback là Warn: panel
// có đăng nhập và TLS, nhưng mọi cổng mở ra internet đều bị dò.
func PanelExposed(port int) Check {
	p := strconv.Itoa(port)
	b := secBase("sec.panel", i18n.T("Cổng panel chỉ nghe ở 127.0.0.1", "The panel port listens on 127.0.0.1 only"))
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		addrs, exposed, ev, err := PanelListen(ctx, sys, port)
		if err != nil {
			return b.unknown(i18n.T("không đọc được cổng "+p+" ("+sslClean(err.Error())+")", "could not read port "+p+" ("+sslClean(err.Error())+")"), ev)
		}
		list := sslClean(strings.Join(addrs, ", "))
		switch {
		case len(addrs) == 0:
			return b.unknown(i18n.T("không có gì đang nghe cổng "+p, "nothing is listening on port "+p), ev)
		case exposed:
			return b.bad(Warn,
				i18n.T("Panel đang mở ra ngoài (nghe ở "+list+", cổng "+p+")", "The panel is open to the outside (listening on "+list+", port "+p+")"),
				i18n.T("Ai trên internet cũng thấy trang đăng nhập của panel và thử mật khẩu được. Chế độ riêng tư chỉ cho panel nghe ở 127.0.0.1; bạn vào bằng đường hầm SSH, chạy lệnh ssh ở dưới trên MÁY CỦA BẠN (không phải trên máy chủ) rồi mở https://127.0.0.1:"+p+".",
					"Anyone on the internet can see the panel login page and try passwords. Private mode makes the panel listen on 127.0.0.1 only; you reach it through an SSH tunnel: run the ssh command below on YOUR OWN COMPUTER (not on the server) and open https://127.0.0.1:"+p+"."),
				[]string{line("tpan", "setup", "--private"), "ssh -L " + p + ":127.0.0.1:" + p + " <user>@<server-address>", line("ss", "-tlnH", "sport", "=", ":"+p)}, ev)
		}
		return b.pass(i18n.T("Panel chỉ nghe ở "+list+" (chế độ riêng tư)", "The panel listens only on "+list+" (private mode)"), ev)
	})
}

// SecurityChecks là các dòng của trang bảo mật theo đúng thứ tự trên trang.
func SecurityChecks(panelPort int) []Check {
	return []Check{
		UFW(),
		Fail2banSSHD(),
		SSHRootLogin(),
		SSHPasswordAuth(),
		Swap(),
		SecurityUpdates(),
		PanelExposed(panelPort),
	}
}
