package demo

// Phần M10 của máy giả (mục 6.6 của docs/KE-HOACH.md): chuyển LEMP ⇄ LAMP.
// Trả lời đúng các lệnh mà Plan stack.switch (core.Switch) chạy, để nút "Đổi
// web server" ở màn Stack chạy trọn trên máy demo, theo cả hai chiều:
//
//   - systemd: nginx và apache2 mỗi cái có hai trạng thái riêng, "đang chạy"
//     (m.units, như mọi dịch vụ khác) và "bật khi khởi động" (enabled ở đây).
//     start, stop, enable, disable đổi đúng hai trạng thái đó. Hai web server
//     không chạy cùng lúc được: cái thứ hai start thì lỗi, như khi cổng 80 bận;
//   - apt: apache2 chưa cài lúc dựng máy. Cài nó tạo /etc/apache2, site mặc
//     định của distro, và (như postinst thật) bật nó khi khởi động rồi chạy
//     ngay, trừ khi /usr/sbin/policy-rc.d trả 101. Nhờ vậy hỏng bước chặn
//     tự khởi động thì Plan lộ ra ngay thay vì êm;
//   - Apache: a2enmod, a2dismod, a2query có trạng thái module. Mặc định như
//     Ubuntu 24.04 (mpm_event, setenvif, status). apachectl configtest báo lỗi
//     khi vhost dùng SSLEngine mà ssl chưa bật; apachectl -M in module đang
//     bật (m28.go);
//   - curl vào máy: trả lời theo web server đang chạy (nginx: như machine.go;
//     Apache: site mặc định trả 403; không ai chạy: không kết nối được);
//   - ss: tiến trình giữ cổng 80 và 443 là web server đang chạy.
//
// Máy giả không dựng sẵn mọi thứ như máy thật ở một điểm: site wiki.example.org
// hỏng sẵn ở chặng PHP, mà Plan chuyển web server từ chối khi có site đang hỏng
// (core.Switch.Prepare, precondition_failed). Muốn chuyển trên demo thì sửa
// wiki trước (khởi động lại PHP-FPM 8.3, hoặc "Sửa" ở trang Sức khoẻ).

import (
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

const (
	policyRcFile = "/usr/sbin/policy-rc.d"
	apacheServer = "Apache/2.4.58 (Ubuntu)"
)

// m10 là trạng thái riêng của phần này. Cả hai map dùng chung khoá m.mu của
// machine (cùng khoá với m.units) để đọc "đang chạy" và "bật khi khởi động"
// trong một lần khoá.
type m10 struct {
	*machine
	enabled map[string]bool // web server bật khi khởi động máy (chỉ nginx, apache2)
	mods    map[string]bool // module Apache đang bật
}

func isWeb(unit string) bool { return unit == "nginx" || unit == "apache2" }

func otherWeb(unit string) string {
	if unit == "nginx" {
		return "apache2"
	}
	return "nginx"
}

// m10Setup đặt trạng thái đầu và handler của phần M10. Gọi sau m12Setup: handler
// cùng độ dài prefix thì cái đăng ký sau thắng, và phần này thay các handler
// systemctl, apt-get install, curl, ss của machine.go và m12.go.
func (m *machine) m10Setup() {
	x := &m10{
		machine: m,
		enabled: map[string]bool{"nginx": true},
		// status: Ubuntu bật sẵn; Plan cài stack phải tắt nó (H5, M28 đợt 2).
		mods: map[string]bool{"mpm_event": true, "setenvif": true, "status": true},
	}
	m.mu.Lock()
	m.missing["apache2"] = true // gói của LAMP chưa cài; nginx đã cài sẵn
	m.mu.Unlock()

	m.Handle([]string{"systemctl", "is-active"}, x.isActive)
	m.Handle([]string{"systemctl", "is-enabled"}, x.isEnabled)
	m.Handle([]string{"systemctl", "start"}, func(c system.Cmd) system.Result { return x.start(unitOf(c)) })
	m.Handle([]string{"systemctl", "stop"}, func(c system.Cmd) system.Result { return x.stop(unitOf(c)) })
	m.Handle([]string{"systemctl", "enable"}, func(c system.Cmd) system.Result { return x.enable(unitOf(c), false) })
	m.Handle([]string{"systemctl", "enable", "--now"}, func(c system.Cmd) system.Result { return x.enable(unitOf(c), true) })
	m.Handle([]string{"systemctl", "disable"}, func(c system.Cmd) system.Result { return x.disable(unitOf(c), false) })
	m.Handle([]string{"systemctl", "disable", "--now"}, func(c system.Cmd) system.Result { return x.disable(unitOf(c), true) })

	m.Handle([]string{"apt-get", "install", "-y"}, x.install)
	m.Handle([]string{"a2enmod"}, func(c system.Cmd) system.Result { return x.setMods(c, true) })
	m.Handle([]string{"a2dismod"}, func(c system.Cmd) system.Result { return x.setMods(c, false) })
	m.Handle([]string{"a2query"}, x.a2query)
	m.Handle([]string{"apachectl", "configtest"}, x.configtest)
	m.Handle([]string{"apachectl", "-M"}, x.apachectlM)

	m.Handle([]string{"curl"}, x.curl)
	m.Handle([]string{"ss"}, x.ss)
}

func unitOf(c system.Cmd) string { return c.Args[len(c.Args)-1] }

// ---------------------------------------------------------------- systemd

// enabledAt cho biết unit bật khi khởi động. Dịch vụ khác của máy giả luôn
// "chạy và tự bật" như machine.go (không có trạng thái riêng). Người gọi giữ m.mu.
func (x *m10) enabledAt(unit string) bool {
	if isWeb(unit) {
		return x.enabled[unit]
	}
	return x.units[unit]
}

func (x *m10) isActive(c system.Cmd) system.Result {
	x.mu.Lock()
	on := x.units[unitOf(c)]
	x.mu.Unlock()
	if on {
		return exit(0, "active\n", "")
	}
	return exit(3, "inactive\n", "")
}

func (x *m10) isEnabled(c system.Cmd) system.Result {
	unit := unitOf(c)
	x.mu.Lock()
	defer x.mu.Unlock()
	if isWeb(unit) && x.missing[unit] {
		return exit(1, "", "Failed to get unit file state for "+unit+".service: No such file or directory\n")
	}
	if x.enabledAt(unit) {
		return exit(0, "enabled\n", "")
	}
	return exit(1, "disabled\n", "")
}

func (x *m10) start(unit string) system.Result {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.startLocked(unit)
}

// startLocked chạy unit; người gọi giữ m.mu. Web server: gói phải cài rồi (mã 5
// như systemd khi không có unit), và web server kia không được đang chạy (cổng
// 80 bận, mã 1).
func (x *m10) startLocked(unit string) system.Result {
	if isWeb(unit) {
		if x.missing[unit] {
			return exit(5, "", "Failed to start "+unit+".service: Unit "+unit+".service not found.\n")
		}
		if x.units[otherWeb(unit)] {
			return exit(1, "", "Job for "+unit+".service failed because the control process exited with error code (port 80 is in use by "+otherWeb(unit)+").\n")
		}
	}
	x.units[unit] = true
	return okResult
}

func (x *m10) stop(unit string) system.Result {
	x.mu.Lock()
	delete(x.units, unit)
	x.mu.Unlock()
	return okResult
}

// enable: web server thì bật khi khởi động (có --now thì chạy luôn). Dịch vụ
// khác: `enable --now` chạy nó như trước, `enable` không đổi gì.
func (x *m10) enable(unit string, now bool) system.Result {
	x.mu.Lock()
	defer x.mu.Unlock()
	if isWeb(unit) {
		if x.missing[unit] {
			return exit(1, "", "Failed to enable unit: Unit file "+unit+".service does not exist.\n")
		}
		x.enabled[unit] = true
	}
	if !now {
		return okResult
	}
	return x.startLocked(unit)
}

// disable: chỉ web server có trạng thái riêng; dịch vụ khác không đổi gì.
func (x *m10) disable(unit string, now bool) system.Result {
	if !isWeb(unit) {
		return okResult
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.enabled, unit)
	if now {
		delete(x.units, unit)
	}
	return okResult
}

// activeWeb là web server đang chạy: "nginx", "apache2", hoặc "" khi không có.
func (x *m10) activeWeb() string {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, u := range []string{"nginx", "apache2"} {
		if x.units[u] {
			return u
		}
	}
	return ""
}

// ---------------------------------------------------------------- apt

// install bọc handler apt-get install của m12.go (php<bản>-fpm, fail2ban...)
// và thêm phần của web server mới cài: các file gói để lại, bật khi khởi động,
// và chạy ngay trừ khi policy-rc.d chặn. Gói đã cài từ trước thì không đổi gì.
func (x *m10) install(c system.Cmd) system.Result {
	var fresh []string
	x.mu.Lock()
	for _, p := range aptPkgArgs(c.Args) {
		if isWeb(p) && x.missing[p] {
			fresh = append(fresh, p)
		}
	}
	x.mu.Unlock()

	// install của m12 chỉ dùng các hàm của machine, không đụng trường của m12.
	res := (&m12{machine: x.machine}).install(c)
	for _, p := range fresh {
		if p == "apache2" {
			x.apacheFiles()
		}
		x.mu.Lock()
		x.enabled[p] = true // postinst: deb-systemd-helper enable
		x.mu.Unlock()
		if blocked, _ := x.policyBlocks(); blocked {
			continue // invoke-rc.d thoát 101: không khởi động
		}
		x.mu.Lock()
		r := x.startLocked(p)
		x.mu.Unlock()
		if r.ExitCode != 0 {
			return exit(100, "", "invoke-rc.d: initscript "+p+", action \"start\" failed.\n"+r.Stderr)
		}
	}
	return res
}

func (x *m10) policyBlocks() (bool, error) {
	data, err := x.ReadFile(policyRcFile)
	return err == nil && strings.Contains(string(data), "exit 101"), err
}

// apacheFiles đặt những gì gói apache2 để lại: thư mục cấu hình, site mặc định
// của distro đã bật (Plan phải gỡ nó), thư mục log.
func (x *m10) apacheFiles() {
	for _, d := range []string{"/etc/apache2/sites-available", "/etc/apache2/sites-enabled", "/var/log/apache2"} {
		x.SetDir(d, 0o755, rootOwner)
	}
	x.SetFile("/etc/apache2/apache2.conf", []byte("# apache2.conf giả của demo\nIncludeOptional sites-enabled/*.conf\n"), 0o644, rootOwner)
	x.SetFile("/etc/apache2/sites-available/000-default.conf", []byte("<VirtualHost *:80>\n    DocumentRoot /var/www/html\n</VirtualHost>\n"), 0o644, rootOwner)
	x.SetSymlink("/etc/apache2/sites-available/000-default.conf", "/etc/apache2/sites-enabled/000-default.conf")
}

// ---------------------------------------------------------------- Apache

// setMods là a2enmod (on) và a2dismod (!on): các tham số sau -q là tên module.
// ssl kéo theo socache_shmcb như a2enmod thật.
func (x *m10) setMods(c system.Cmd, on bool) system.Result {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.missing["apache2"] {
		return exit(127, "", "a2enmod: command not found\n")
	}
	for _, mod := range c.Args {
		if strings.HasPrefix(mod, "-") {
			continue
		}
		if on {
			x.mods[mod] = true
			if mod == "ssl" {
				x.mods["socache_shmcb"] = true
			}
		} else {
			delete(x.mods, mod)
		}
	}
	return exit(0, "", "")
}

// a2query -m <mod>: mã 0 đang bật, 32 không bật (man a2query).
func (x *m10) a2query(c system.Cmd) system.Result {
	mod := argAfter(c.Args, "-m")
	x.mu.Lock()
	on := x.mods[mod]
	x.mu.Unlock()
	if on {
		return exit(0, mod+" (enabled by site administrator)\n", "")
	}
	return exit(32, "", "No module matches "+mod+" (specified by site administrator)\n")
}

// configtest: Apache luôn in cảnh báo AH00558 về ServerName rồi "Syntax OK",
// mã thoát 0 (checks.ApacheConfig chỉ nhìn mã thoát vì chuyện này). Vhost đã
// bật mà dùng SSLEngine khi ssl chưa bật thì sai cú pháp.
func (x *m10) configtest(system.Cmd) system.Result {
	const warn = "AH00558: apache2: Could not reliably determine the server's fully qualified domain name, using 127.0.1.1. Set the 'ServerName' directive globally to suppress this message\n"
	x.mu.Lock()
	ssl := x.mods["ssl"]
	x.mu.Unlock()
	names, _ := x.ReadDir("/etc/apache2/sites-enabled")
	for _, n := range names {
		conf, err := x.ReadFile("/etc/apache2/sites-enabled/" + n)
		if err == nil && !ssl && strings.Contains(string(conf), "SSLEngine") {
			return exit(1, "", warn+"AH00526: Syntax error on line 1 of /etc/apache2/sites-enabled/"+n+":\nInvalid command 'SSLEngine', perhaps misspelled or defined by a module not included in the server configuration\nAction 'configtest' failed.\n")
		}
	}
	return exit(0, "", warn+"Syntax OK\n")
}

// ---------------------------------------------------------------- HTTP

// curl trả lời theo web server đang chạy. Nginx: đúng như machine.go. Không
// web server nào chạy: mọi request vào máy (--resolve, -w, http://...) không
// nối được (mã 7). Apache: xem apacheSite. Lệnh khác của curl (tải WordPress,
// WP-CLI, ghi chú) không phụ thuộc web server nên để machine.go lo.
func (x *m10) curl(c system.Cmd) system.Result {
	// Site tạm khoá (M13, m13.go) trả 403 kèm trang tạm khoá, như nhau với
	// nginx và Apache.
	if res, ok := x.suspendedSite(c); ok {
		return res
	}
	// M14 (m14.go): tên miền lạ tới cổng 443, phép thử của checks.DefaultRejects.
	if res, ok := x.rejectTLS(c); ok {
		return res
	}
	web := x.activeWeb()
	resolve := argAfter(c.Args, "--resolve")
	local := resolve != "" || slices.Contains(c.Args, "-w") ||
		slices.ContainsFunc(c.Args, func(a string) bool { return strings.HasPrefix(a, "http://") })
	if web == "nginx" || !local {
		return x.machine.curl(c)
	}
	if web == "" {
		return exit(7, "", "curl: (7) Failed to connect to 127.0.0.1 port 80: Connection refused\n")
	}
	switch {
	case resolve != "":
		dom, rest, _ := strings.Cut(resolve, ":")
		port, _, _ := strings.Cut(rest, ":")
		return x.apacheSite(dom, port, slices.Contains(c.Args, "-D"))
	case slices.Contains(c.Args, "-w"):
		return exit(0, "403", "") // checks.HTTPDefault: site mặc định của Apache trả 403
	}
	if res, ok := x.acmeProbe(c.Args); ok {
		return res
	}
	return okResult
}

// apacheSite giả lập một request tới site qua Apache và PHP-FPM, ở cổng port.
// Khác nginx ở ba điểm: vhost chưa bật thì request rơi vào site mặc định và
// nhận 403 (nginx đóng kết nối); cổng 443 chỉ nghe khi mod ssl bật; còn lại
// như httpSite (301 sang https khi vhost có SSL, 502 khi mất socket, 200).
func (x *m10) apacheSite(dom, port string, headers bool) system.Result {
	l, ok := siteOf(dom)
	if !ok {
		return exit(6, "", "curl: (6) Could not resolve host\n")
	}
	x.mu.Lock()
	ssl := x.mods["ssl"]
	x.mu.Unlock()
	if port == "443" && !ssl {
		return exit(7, "", "curl: (7) Failed to connect to "+dom+" port 443: Connection refused\n")
	}
	reply := func(code, status, extra, body string) system.Result {
		if headers {
			return exit(0, "HTTP/1.1 "+code+" "+status+"\r\nServer: "+apacheServer+"\r\n"+extra+"\r\nTPAN_HTTP_CODE="+code, "")
		}
		return exit(0, body+"TPAN_HTTP_CODE="+code, "")
	}
	page := func(code, status string) system.Result {
		return reply(code, status, "", "<html><head><title>"+code+" "+status+"</title></head><body>"+code+" "+status+"</body></html>")
	}
	if _, err := x.Stat(l.ApacheLink); err != nil {
		return page("403", "Forbidden")
	}
	vhostSSL := x.apacheVhostSSL(l)
	scheme := "http"
	switch {
	case port == "443" && !vhostSSL:
		return page("403", "Forbidden") // site mặc định 443
	case port == "443":
		scheme = "https"
	case vhostSSL:
		return reply("301", "Moved Permanently", "Location: https://"+dom+"/\r\n",
			"<html><head><title>301 Moved Permanently</title></head><body>301 Moved Permanently</body></html>")
	}
	if _, err := x.Stat(x.vhostSocket(l)); err != nil {
		return page("503", "Service Unavailable") // proxy_fcgi: không nối được socket
	}
	body := "<html><head><link rel='stylesheet' href='" + scheme + "://" + dom + "/wp-content/themes/x/style.css'></head><body>" + dom + "</body></html>"
	return reply("200", "OK", "", body)
}

func (x *m10) apacheVhostSSL(l site.Layout) bool {
	conf, err := x.ReadFile(l.ApacheConf)
	return err == nil && strings.Contains(string(conf), "<VirtualHost *:443>")
}

// ss: cổng 80 và 443 do web server đang chạy giữ; không chạy thì không ai nghe.
// Phần còn lại giống handler của machine.go.
func (x *m10) ss(system.Cmd) system.Result {
	var web []string
	if w := x.activeWeb(); w != "" {
		proc := `users:(("` + w + `",pid=1180,fd=`
		web = []string{
			`LISTEN 0 511 0.0.0.0:80 0.0.0.0:* ` + proc + `5))`,
			`LISTEN 0 511 0.0.0.0:443 0.0.0.0:* ` + proc + `7))`,
			`LISTEN 0 511 [::]:80 [::]:* ` + proc + `6))`,
			`LISTEN 0 511 [::]:443 [::]:* ` + proc + `8))`,
		}
	}
	return exit(0, strings.Join(append([]string{
		`LISTEN 0 4096 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=612,fd=14))`,
		`LISTEN 0 4096 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=701,fd=3))`,
		`LISTEN 0 80 127.0.0.1:3306 0.0.0.0:* users:(("mariadbd",pid=905,fd=21))`,
		`LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:* users:(("tpan",pid=640,fd=9))`,
		`LISTEN 0 4096 [::]:22 [::]:* users:(("sshd",pid=701,fd=4))`,
	}, web...), "\n")+"\n", "")
}
