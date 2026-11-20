package demo

// Phần M14 của máy giả (mục 6.13 của docs/KE-HOACH.md): cô lập sâu giữa các
// site và định tuyến. Máy demo là một máy "đã cài bằng bản có M14", nên mọi
// phép kiểm của M14 phải đạt ngay khi mở demo:
//
//   - file ini OPcache (99-tpan.ini) và drop-in ProtectProc đã có sẵn cho bản
//     PHP của máy (m14Files). Bản PHP cài thêm trên demo thì nhận hai file đó
//     từ chính Plan php.install (steps.PHPHardening), không đặt tay ở đây;
//   - `php-fpm<bản> -i` in hai dòng OPcache theo file ini đang có trên máy giả;
//   - `nginx -T` và `apache2ctl -S` in cấu hình đang nạp, dựng từ các vhost
//     đang có trong sites-enabled của máy giả, đúng định dạng lệnh thật in
//     (parser ở internal/checks/m14.go đọc lại);
//   - curl mang tên miền lạ tới cổng 443 (checks.DefaultRejects): nginx từ
//     chối bắt tay TLS, Apache trả 403. Cổng 80 đã có sẵn ở machine.go và
//     m10.go (cùng dạng lệnh với checks.HTTPDefault).
//
// Quyền log của site (root:<user site> 0750 và 0640) và các dòng chặn trong
// vhost không cần đặt ở đây: site mẫu được tạo bằng chính core.Sites, nên
// chúng ra từ steps.SiteLogs và template hiện tại.
//
// `systemctl show ... -p ProtectProc` nằm ở machine.go, cạnh các handler
// systemctl khác.
//
// Đợt 2 (route explain, ma trận cô lập, chế độ .htaccess):
//
//   - `tpan route explain` và nút "Request tới site này đi đường nào" không
//     cần gì thêm: core.RouteExplain đọc `nginx -T` / `apache2ctl -S` ở trên
//     rồi đọc file vhost và file pool mà core.Sites đã ghi lên máy giả;
//   - ma trận cô lập đọc bảng check_results, do health của `tpan serve --demo`
//     ghi ngay lần chạy đầu (phép kiểm cô lập trả lời ở machine.go: test -r,
//     -x, -w luôn bị từ chối);
//   - chế độ .htaccess: site mẫu tạo sau M14 nên mang an-toan; máy demo chạy
//     nginx nên cột này chỉ hiện chế độ. `find ... -name .htaccess` và
//     `head -c` trả lời theo file thật của máy giả, để lần quét .htaccess
//     (đổi chế độ, Plan "Áp bản vá cô lập") thấy đúng nội dung khi demo đã
//     chuyển sang Apache hoặc khi test đặt một .htaccess có chỉ thị cấm.

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// m14Files đặt hai file cấp máy của M14 cho bản PHP đã cài sẵn, như Plan cài
// stack để lại. Gọi trong installFiles.
func (m *machine) m14Files() {
	m.SetFile(templates.PHPHardeningINIPath(phpVer), templates.PHPHardeningINI(), 0o644, rootOwner)
	m.SetDir(templates.PHPDropInDir(phpVer), 0o755, rootOwner)
	m.SetFile(templates.PHPDropInPath(phpVer), templates.PHPDropIn(), 0o644, rootOwner)
}

// m14Setup đăng ký handler của phần M14. Gọi sau m10Setup và m12Setup.
func (m *machine) m14Setup() {
	// FakeSystem khớp handler theo đúng tên lệnh, nên mỗi bản PHP mà máy demo
	// cài được (suryPHP gồm cả bản của Ubuntu) có một handler riêng.
	for _, v := range suryPHP {
		m.Handle([]string{"php-fpm" + v, "-i"}, func(system.Cmd) system.Result { return m.phpInfo(v) })
	}
	m.Handle([]string{"nginx", "-T"}, m.nginxDump)
	m.Handle([]string{"apache2ctl", "-S"}, m.apacheVhosts)
	m.Handle([]string{"head", "-c"}, m.headFile)
}

// findHtaccess là `find <public> -name .htaccess ( -type f -o -type l )
// -print0` của checks.ScanHtaccess: mọi file .htaccess đang có dưới thư mục
// đó trên máy giả, ngăn nhau bằng byte 0. Không có handler này thì find trả
// rỗng và lần quét của Plan "Áp bản vá cô lập" hay `site set --htaccess
// an-toan` luôn "sạch", kể cả khi .htaccess của site có chỉ thị cấm.
func (m *machine) findHtaccess(c system.Cmd) system.Result {
	root := strings.TrimSuffix(c.Args[0], "/") + "/"
	var found []string
	for p := range m.Snapshot() {
		if strings.HasPrefix(p, root) && strings.HasSuffix(p, "/.htaccess") {
			found = append(found, p)
		}
	}
	slices.Sort(found)
	var b strings.Builder
	for _, p := range found {
		b.WriteString(p + "\x00")
	}
	return exit(0, b.String(), "")
}

// headFile là `head -c <n> -- <file>`: n byte đầu của file trên máy giả
// (ScanHtaccess đọc từng .htaccess bằng lệnh này, dưới user của site).
func (m *machine) headFile(c system.Cmd) system.Result {
	file := c.Args[len(c.Args)-1]
	data, err := m.ReadFile(file)
	if err != nil {
		return exit(1, "", "head: cannot open '"+file+"' for reading: No such file or directory\n")
	}
	if n, err := strconv.Atoi(argAfter(c.Args, "-c")); err == nil && n >= 0 && n < len(data) {
		data = data[:n]
	}
	return exit(0, string(data), "")
}

// phpInfo là `php-fpm<bản> -i`: phpinfo dạng chữ, mỗi khoá một dòng
// "khoá => giá trị đang dùng => giá trị gốc". Chỉ in phần đầu và hai dòng mà
// checks.OPcacheHardened đọc. PHP thật gộp mọi file trong conf.d; máy giả chỉ
// xét file của tpan: đúng nội dung thì On, thiếu hoặc khác thì là giá trị mặc
// định của PHP (Off, "no value"), để xoá file đi là phép kiểm đỏ như máy thật.
func (m *machine) phpInfo(v string) system.Result {
	perm, api := "Off", "no value"
	if b, err := m.ReadFile(templates.PHPHardeningINIPath(v)); err == nil && bytes.Equal(b, templates.PHPHardeningINI()) {
		perm, api = "On", templates.OPcacheAPIDir
	}
	return exit(0, "phpinfo()\nPHP Version => "+v+".6\n\nZend OPcache\n\nOpcode Caching => Up and Running\n"+
		"opcache.restrict_api => "+api+" => "+api+"\n"+
		"opcache.validate_permission => "+perm+" => "+perm+"\n", "")
}

// nginxDump là `nginx -T`: bản in mọi file cấu hình đang nạp ở stdout, mỗi
// file mở đầu bằng dòng "# configuration file <đường dẫn>:"; lời "syntax is
// ok" ở stderr. Đường dẫn là đường dẫn trong sites-enabled (nginx in đường dẫn
// nó include, không đi theo symlink).
func (m *machine) nginxDump(system.Cmd) system.Result {
	var b strings.Builder
	put := func(p string) {
		if data, err := m.ReadFile(p); err == nil {
			b.WriteString("# configuration file " + p + ":\n" + string(data) + "\n")
		}
	}
	put("/etc/nginx/nginx.conf")
	names, _ := m.ReadDir("/etc/nginx/sites-enabled") // đã xếp theo tên, như include của nginx
	for _, n := range names {
		put("/etc/nginx/sites-enabled/" + n)
	}
	return exit(0, b.String(), "nginx: the configuration file /etc/nginx/nginx.conf syntax is ok\nnginx: configuration file /etc/nginx/nginx.conf test is successful\n")
}

var (
	apacheVhostOpenRE  = regexp.MustCompile(`^\s*<VirtualHost\s+\*:([0-9]+)>`)
	apacheServerNameRE = regexp.MustCompile(`^\s*ServerName\s+(\S+)`)
)

// apacheVhosts là `apache2ctl -S`: bảng VirtualHost đang nạp, theo đúng dạng
// Apache 2.4 in (hàm dump_a_vhost trong server/vhost.c). Mỗi cổng một khối
// "*:<cổng> is a NameVirtualHost", bên dưới là vhost mặc định (cái nạp đầu
// tiên) rồi từng dòng "port <cổng> namevhost <tên> (<file>:<dòng>)"; <dòng> là
// dòng mở <VirtualHost>. Cổng chỉ có một vhost thì Apache in gọn một dòng.
//
// ponytail: không in dòng "alias" vì template của tpan không có ServerAlias và
// máy demo không có vhost nào khác; cần thì đọc thêm ServerAlias ở vòng lặp dưới.
func (m *machine) apacheVhosts(system.Cmd) system.Result {
	type vhost struct{ name, at string }
	var ports []string // theo thứ tự gặp
	byPort := map[string][]vhost{}
	names, _ := m.ReadDir("/etc/apache2/sites-enabled")
	for _, n := range names {
		file := "/etc/apache2/sites-enabled/" + n
		data, err := m.ReadFile(file)
		if err != nil {
			continue
		}
		port := "" // rỗng: đang ở ngoài mọi khối VirtualHost
		at := ""
		for i, ln := range strings.Split(string(data), "\n") {
			if g := apacheVhostOpenRE.FindStringSubmatch(ln); g != nil {
				port, at = g[1], file+":"+strconv.Itoa(i+1)
			} else if g := apacheServerNameRE.FindStringSubmatch(ln); g != nil && port != "" {
				if byPort[port] == nil {
					ports = append(ports, port)
				}
				byPort[port] = append(byPort[port], vhost{g[1], at})
				port = ""
			}
		}
	}
	var b strings.Builder
	b.WriteString("VirtualHost configuration:\n")
	for _, p := range ports {
		vs := byPort[p]
		addr := fmt.Sprintf("%-23s", "*:"+p) // Apache căn cột địa chỉ rộng 23 ký tự
		if len(vs) == 1 {
			b.WriteString(addr + vs[0].name + " (" + vs[0].at + ")\n")
			continue
		}
		b.WriteString(addr + "is a NameVirtualHost\n")
		b.WriteString("         default server " + vs[0].name + " (" + vs[0].at + ")\n")
		for _, v := range vs {
			b.WriteString("         port " + p + " namevhost " + v.name + " (" + v.at + ")\n")
		}
	}
	b.WriteString("ServerRoot: \"/etc/apache2\"\nMain DocumentRoot: \"/var/www/html\"\nMain ErrorLog: \"/var/log/apache2/error.log\"\n" +
		"Mutex default: dir=\"/var/run/apache2/\" mechanism=default\nPidFile: \"/var/run/apache2/apache2.pid\"\n" +
		"Define: DUMP_VHOSTS\nDefine: DUMP_RUN_CFG\nUser: name=\"www-data\" id=33\nGroup: name=\"www-data\" id=33\n")
	return exit(0, b.String(), "AH00558: apache2: Could not reliably determine the server's fully qualified domain name, using 127.0.1.1. Set the 'ServerName' directive globally to suppress this message\n")
}

// rejectTLS trả lời phép thử cổng 443 của checks.DefaultRejects: curl với
// --resolve <tên lạ>.invalid:443:127.0.0.1. Đuôi .invalid không bao giờ là tên
// một site, nên request luôn rơi vào vhost mặc định 443 của tpan:
//
//   - nginx (ssl_reject_handshake on): từ chối ngay lúc bắt tay TLS, curl
//     thoát mã 35. Chưa có vhost mặc định 443 (chưa site nào bật SSL) thì cổng
//     443 không ai nghe, mã 7;
//   - Apache: trả 403 (in đúng mã vì lệnh dùng -o /dev/null -w %{http_code});
//     mod ssl chưa bật thì cổng 443 không ai nghe, mã 7.
//
// ok=false: không phải lệnh này, để handler curl thường trả lời.
func (x *m10) rejectTLS(c system.Cmd) (system.Result, bool) {
	name, rest, _ := strings.Cut(argAfter(c.Args, "--resolve"), ":")
	if !strings.HasSuffix(name, ".invalid") || !strings.HasPrefix(rest, "443:") {
		return system.Result{}, false
	}
	refused := exit(7, "000", "curl: (7) Failed to connect to "+name+" port 443: Connection refused\n")
	switch x.activeWeb() {
	case "nginx":
		if _, err := x.Stat("/etc/nginx/sites-enabled/tpan-default-ssl.conf"); err != nil {
			return refused, true
		}
		return exit(35, "000", "curl: (35) OpenSSL/3.0.13: error:0A000458:SSL routines::tlsv1 unrecognized name\n"), true
	case "apache2":
		x.mu.Lock()
		ssl := x.mods["ssl"]
		x.mu.Unlock()
		if !ssl {
			return refused, true
		}
		return exit(0, "403", ""), true
	}
	return refused, true
}
