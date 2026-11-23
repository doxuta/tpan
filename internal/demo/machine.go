package demo

// Máy giả của chế độ --demo: một FakeSystem cộng các handler giả lập phần máy
// mà stack và site đụng tới (user Linux, MariaDB, curl, tar, WP-CLI, systemd,
// socket của PHP-FPM) và số liệu /proc. Dựa trên siteSim và sim của core nhưng
// viết lại ở đây vì file test không import được, và vì hai điểm khác:
//
//   - curl vào site trả mã theo trạng thái thật của máy giả (vhost có bật
//     không, socket có không), để một site hỏng ở chặng PHP thì chặng cuối
//     (gọi HTTP) cũng đỏ như trên máy thật;
//   - có thể "làm hỏng" một pool: socket mất và các lần reload PHP-FPM sau
//     không tạo lại, nên site hỏng vẫn hỏng khi người dùng tạo site mới.

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"io/fs"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// phpVer là bản PHP của máy giả; core đọc nó từ settings stack.php_version.
	phpVer = "8.3"
	// numCPU và memTotalKB: máy 4 CPU, 4 GB RAM (MemTotal thật của máy "4 GB" thấp hơn một chút).
	numCPU     = 4
	memTotalKB = 4013828
	// procTick là nhịp cập nhật /proc/stat. Phải ngắn hơn khoảng cách giữa hai
	// lần đọc của phép đo CPU, nếu không hai lần đọc thấy cùng số và CPU luôn 0%.
	procTick = 100 * time.Millisecond
)

// Nội dung giả mà curl "tải về". Mã băm của chúng là giá trị ghim của máy giả:
// không có file nào trùng SHA-1 hay sha512 thật mà core ghim, nên Sites và
// Stack dựng cho demo phải đặt các giá trị này (xem NewSites, NewStack).
var (
	fakeWordPress = []byte("tarball WordPress giả của chế độ demo\n")
	fakeWPCLI     = []byte("<?php // WP-CLI giả của chế độ demo\n")
)

func wordPressSHA1() string {
	sum := sha1.Sum(fakeWordPress)
	return hex.EncodeToString(sum[:])
}

func wpCLISHA512() string {
	sum := sha512.Sum512(fakeWPCLI)
	return hex.EncodeToString(sum[:])
}

var (
	rootOwner = system.Root
	wwwOwner  = system.Owner{User: "www-data", Group: "www-data"}
	okResult  = system.Result{}
)

// machine là FakeSystem cộng trạng thái của các handler.
type machine struct {
	*system.FakeSystem

	mu      sync.Mutex
	users   map[string]string // user Linux → thư mục home
	dbs     map[string]bool
	dbUsers map[string]bool
	// dbLocked: user DB bị khoá (ALTER USER ... ACCOUNT LOCK, thùng rác M6).
	dbLocked map[string]bool
	// dbCharset: charset và collation mặc định của DB (M13, m13.go): ghi lúc
	// CREATE và ALTER DATABASE, đọc lại cho màn Cơ sở dữ liệu và bước đổi charset.
	dbCharset map[string][2]string
	// expired: user Linux có hạn tài khoản (usermod --expiredate, thùng rác
	// M6), giá trị là dòng "Account expires" của chage -l.
	expired   map[string]string
	installed map[string]bool              // WordPress đã cài, theo user của site
	options   map[string]map[string]string // tuỳ chọn WordPress (wp option), theo user của site
	sockets   map[string]bool              // socket pool mà lần reload gần nhất đã tạo
	units     map[string]bool              // dịch vụ systemd đang chạy và tự bật
	missing   map[string]bool              // gói apt chưa cài (mọi gói khác coi như đã cài)
	ufw       ufwState                     // tường lửa (security.go)
	// broken: socket của pool bị làm hỏng. Reload không tạo lại; restart thì sửa.
	broken map[string]bool

	// Bộ đếm /proc/stat, mỗi lõi một bộ (đơn vị 1/100 giây).
	cpu   [numCPU]cpuTimes
	ticks int
}

type cpuTimes struct{ user, system, idle uint64 }

func newMachine() *machine {
	m := &machine{
		FakeSystem: system.NewFake(),
		users:      map[string]string{},
		dbs:        map[string]bool{},
		dbUsers:    map[string]bool{},
		dbLocked:   map[string]bool{},
		dbCharset:  map[string][2]string{},
		expired:    map[string]string{},
		installed:  map[string]bool{},
		options:    map[string]map[string]string{},
		sockets:    map[string]bool{},
		broken:     map[string]bool{},
		// Dịch vụ của stack, và certbot.timer (gói certbot cài sẵn nó).
		units:   map[string]bool{"nginx": true, "mariadb": true, "php" + phpVer + "-fpm": true, "certbot.timer": true},
		missing: map[string]bool{"fail2ban": true},
	}
	m.installFiles()
	m.handlers()
	m.sslHandlers()
	m.securityHandlers()
	m.trashHandlers()
	// Sau securityHandlers: m12.go thay handler `apt-get -s` của security.go
	// (cùng độ dài prefix thì handler đăng ký sau thắng).
	m.m12Setup()
	m.m15Setup() // M15 (m15.go): nguồn tải giả của "Kiểm bản mới", file tpan.old.
	// Sau m12Setup: m10.go thay các handler systemctl, apt-get install, curl, ss.
	m.m10Setup()
	// M14 (m14.go): php-fpm -i, nginx -T, apache2ctl -S.
	m.m14Setup()
	m.writeProc()
	return m
}

// installFiles đặt các file mà stack đã cài để lại (mục 5 và 6.1 của
// docs/KE-HOACH.md): web server, PHP-FPM, MariaDB, WP-CLI, logrotate, thư mục
// của tpan.
func (m *machine) installFiles() {
	for _, d := range []string{
		"/etc/nginx/sites-available", "/etc/nginx/sites-enabled",
		"/etc/php/" + phpVer + "/fpm/pool.d", "/etc/php/" + phpVer + "/fpm/conf.d", "/etc/systemd/system",
		"/etc/mysql/mariadb.conf.d", "/etc/logrotate.d",
		"/var/lib/tpan/acme", "/var/cache/tpan", "/var/www", "/var/log/tpan/sites", "/usr/local/bin",
	} {
		m.SetDir(d, 0o755, rootOwner)
	}
	m.SetDir("/run/php", 0o755, wwwOwner)
	// Máy demo đã qua stack install của M12: pool www của gói đã gỡ, thay bằng
	// pool tpan-idle (socket chỉ root).
	m.SetSocket("/run/php/tpan-idle-"+phpVer+".sock", 0o600, rootOwner)

	m.SetFile("/etc/os-release", []byte("NAME=\"Ubuntu\"\nVERSION=\"24.04.2 LTS (Noble Numbat)\"\nID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"), 0o644, rootOwner)
	m.SetFile("/etc/hostname", []byte("tpan-demo\n"), 0o644, rootOwner)
	m.SetFile("/etc/nginx/nginx.conf", []byte("user www-data;\nworker_processes auto;\nevents { worker_connections 768; }\nhttp { include /etc/nginx/sites-enabled/*; }\n"), 0o644, rootOwner)
	if idle, err := templates.PHPIdlePool(phpVer); err == nil {
		m.SetFile("/etc/php/"+phpVer+"/fpm/pool.d/tpan-idle.conf", idle, 0o644, rootOwner)
	}
	m.SetFile("/etc/mysql/mariadb.conf.d/99-tpan.cnf", []byte(templates.Header+"\n[mysqld]\nbind-address = 127.0.0.1\n"), 0o644, rootOwner)
	m.SetFile("/usr/local/bin/wp", fakeWPCLI, 0o755, rootOwner)

	// Site mặc định và logrotate sinh từ chính template của tpan, để file giả
	// giống file thật. Template lỗi thì đây là lỗi lập trình: test bắt ngay.
	if conf, err := templates.NginxDefault(templates.DefaultSite{AcmeRoot: "/var/lib/tpan/acme"}); err == nil {
		m.SetFile("/etc/nginx/sites-available/tpan-default.conf", conf, 0o644, rootOwner)
		m.SetSymlink("/etc/nginx/sites-available/tpan-default.conf", "/etc/nginx/sites-enabled/tpan-default.conf")
	}
	if conf, err := templates.LogrotateSites(templates.Logrotate{Web: "nginx"}); err == nil {
		m.SetFile("/etc/logrotate.d/tpan-sites", conf, 0o644, rootOwner)
	}

	m.SetDisk("/", system.DiskUsage{Total: 80 << 30, Free: 46 << 30})
	m.sslFiles()
	m.securityFiles()
	// M14 (m14.go): file ini OPcache và drop-in ProtectProc của bản PHP đã cài.
	m.m14Files()

	// Bộ đếm CPU khởi đầu: máy đã chạy hơn 12 ngày.
	for i := range m.cpu {
		m.cpu[i] = cpuTimes{user: 210_000 + uint64(i)*3_100, system: 61_000 + uint64(i)*900, idle: 9_400_000 + uint64(i)*12_000}
	}
}

// siteOf đổi domain (từ dòng lệnh) thành Layout; domain sai thì ok=false.
func siteOf(dom string) (site.Layout, bool) {
	d, err := validate.ParseDomain(dom)
	if err != nil {
		return site.Layout{}, false
	}
	return site.LayoutOf(d), true
}

func exit(code int, stdout, stderr string) system.Result {
	return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
}

func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func argPrefix(args []string, prefix string) string {
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, prefix); ok {
			return v
		}
	}
	return ""
}

// handlers đăng ký cách trả lời cho các lệnh. Handler dài prefix hơn thắng, nên
// systemctl có một handler chung và một handler riêng cho reload/restart PHP-FPM.
func (m *machine) handlers() {
	m.Handle([]string{"getent", "passwd"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		name := c.Args[1]
		home, found := m.users[name]
		if !found {
			return exit(2, "", "") // mã 2: không có key đó
		}
		return exit(0, name+":x:998:998::"+home+":/usr/sbin/nologin\n", "")
	})
	m.Handle([]string{"useradd"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.users[c.Args[len(c.Args)-1]] = argAfter(c.Args, "--home-dir")
		return okResult
	})
	m.Handle([]string{"userdel"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		name := c.Args[len(c.Args)-1]
		if _, found := m.users[name]; !found {
			return exit(6, "", "userdel: user '"+name+"' does not exist\n")
		}
		delete(m.users, name)
		// Xoá hẳn site (M6) rồi tạo lại cùng domain trên giao diện demo thì
		// phải là WordPress mới cài, có mật khẩu mới: quên mọi thứ của user cũ.
		delete(m.expired, name)
		delete(m.installed, name)
		delete(m.options, name)
		return okResult
	})

	m.Handle([]string{"mariadb", "-N", "-B"}, m.mariadb)
	m.Handle([]string{"curl"}, m.curl)
	// Log của site: log trực tiếp và đếm request (logs.go).
	m.Handle([]string{"tail"}, m.tail)

	// tar giải nén WordPress: chỉ dựng vài file đủ để phép kiểm thấy có mã nguồn.
	m.Handle([]string{"tar"}, func(c system.Cmd) system.Result {
		pub := argAfter(c.Args, "-C")
		own := system.Owner{User: c.AsUser, Group: c.AsUser}
		m.SetFile(pub+"/index.php", []byte("<?php\n"), 0o644, own)
		m.SetFile(pub+"/wp-includes/version.php", []byte("<?php $wp_version = '7.1.5';\n"), 0o644, own)
		return okResult
	})
	m.Handle([]string{"chmod"}, func(c system.Cmd) system.Result {
		mode, err := strconv.ParseUint(c.Args[0], 8, 32)
		if err != nil {
			return exit(1, "", "chmod: invalid mode\n")
		}
		if err := m.Chmod(c.Args[len(c.Args)-1], fs.FileMode(mode)); err != nil {
			return exit(1, "", err.Error()+"\n")
		}
		return okResult
	})
	m.Handle([]string{"wp"}, m.wp)

	// `find <public> -mindepth 1 -delete` (hoàn tác WPFiles) và
	// `rm -r --one-file-system -- <dir>` (RemoveTree). `find ... -name .htaccess
	// ... -print0` là lần quét .htaccess của M14 (m14.go). Các find khác không
	// đổi gì.
	m.Handle([]string{"find"}, func(c system.Cmd) system.Result {
		if slices.Contains(c.Args, "-delete") {
			m.removeUnder(c.Args[0], false)
		}
		if slices.Contains(c.Args, "-print0") && argAfter(c.Args, "-name") == ".htaccess" {
			return m.findHtaccess(c)
		}
		return okResult
	})
	m.Handle([]string{"rm", "-r"}, func(c system.Cmd) system.Result {
		m.removeUnder(c.Args[len(c.Args)-1], true)
		return okResult
	})

	// Phép kiểm cô lập: dưới user của site này, đọc wp-config.php của site kia
	// phải bị từ chối. Máy giả luôn từ chối, như máy thật khi quyền đúng.
	// M14 (mục 6.13) thêm bốn phép thử: vào thư mục (-x), ghi vào public và
	// socket PHP-FPM (-w), đọc access.log (-r).
	for _, flag := range []string{"-r", "-x", "-w"} {
		m.Handle([]string{"test", flag}, func(system.Cmd) system.Result { return exit(1, "", "") })
	}

	// Dịch vụ trong m.units luôn chạy và tự bật. Dịch vụ khác thì không có.
	m.Handle([]string{"systemctl"}, func(c system.Cmd) system.Result {
		unit := c.Args[len(c.Args)-1]
		m.mu.Lock()
		known := m.units[unit]
		m.mu.Unlock()
		switch c.Args[0] {
		case "is-active":
			if known {
				return exit(0, "active\n", "")
			}
			return exit(3, "inactive\n", "")
		case "is-enabled":
			if known {
				return exit(0, "enabled\n", "")
			}
			return exit(1, "disabled\n", "")
		}
		return okResult // reload, restart, enable... của dịch vụ khác: không đổi gì
	})
	// M14 (mục 6.13): `systemctl show <unit> -p ProtectProc --value`, lệnh mà
	// bước steps.PHPProtectProc và phép kiểm php.protectproc dùng. Trả lời
	// theo drop-in tpan.conf đang có trên máy giả; chưa đặt thì "default".
	m.Handle([]string{"systemctl", "show"}, func(c system.Cmd) system.Result {
		b, _ := m.ReadFile("/etc/systemd/system/" + c.Args[1] + ".service.d/tpan.conf")
		if strings.Contains(string(b), "\nProtectProc=invisible\n") {
			return exit(0, "invisible\n", "")
		}
		return exit(0, "default\n", "")
	})
	// reload, restart php<bản>-fpm của mọi bản PHP (M12: nhiều bản song song).
	// Dịch vụ khác: không đổi gì, như handler chung ở trên.
	m.Handle([]string{"systemctl", "reload"}, func(c system.Cmd) system.Result {
		if v, ok := fpmVersion(c.Args[len(c.Args)-1]); ok {
			m.syncSockets(v)
		}
		return okResult
	})
	m.Handle([]string{"systemctl", "restart"}, func(c system.Cmd) system.Result {
		v, ok := fpmVersion(c.Args[len(c.Args)-1])
		if !ok {
			return okResult
		}
		// Khởi động lại là nạp lại mọi pool của bản đó, kể cả pool đang hỏng.
		m.mu.Lock()
		for s := range m.broken {
			if strings.HasSuffix(s, "-"+v+".sock") {
				delete(m.broken, s)
			}
		}
		m.mu.Unlock()
		m.syncSockets(v)
		return okResult
	})

	// Cổng đang nghe: web ở 80 và 443 (đã có site SSL), SSH ở 22, MariaDB chỉ
	// ở 127.0.0.1 (mục 6.1 bước 8), panel ở 0.0.0.0:8443 (chưa bật chế độ
	// riêng tư, trang bảo mật cảnh báo).
	m.Handle([]string{"ss"}, func(system.Cmd) system.Result {
		return exit(0, strings.Join([]string{
			`LISTEN 0 4096 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=612,fd=14))`,
			`LISTEN 0 4096 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=701,fd=3))`,
			`LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=1180,fd=5))`,
			`LISTEN 0 511 0.0.0.0:443 0.0.0.0:* users:(("nginx",pid=1180,fd=7))`,
			`LISTEN 0 80 127.0.0.1:3306 0.0.0.0:* users:(("mariadbd",pid=905,fd=21))`,
			`LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:* users:(("tpan",pid=640,fd=9))`,
			`LISTEN 0 4096 [::]:22 [::]:* users:(("sshd",pid=701,fd=4))`,
			`LISTEN 0 511 [::]:80 [::]:* users:(("nginx",pid=1180,fd=6))`,
			`LISTEN 0 511 [::]:443 [::]:* users:(("nginx",pid=1180,fd=8))`,
		}, "\n")+"\n", "")
	})
}

var (
	sqlStateRE   = regexp.MustCompile(`SCHEMA_NAME='([a-z0-9_]+)'`)
	sqlCreateDB  = regexp.MustCompile("CREATE DATABASE `([a-z0-9_]+)`")
	sqlCreateUsr = regexp.MustCompile(`CREATE USER '([a-z0-9_]+)'`)
	sqlDropDB    = regexp.MustCompile("DROP DATABASE IF EXISTS `([a-z0-9_]+)`")
	sqlDropUsr   = regexp.MustCompile(`DROP USER IF EXISTS '([a-z0-9_]+)'`)
	sqlQuoted    = regexp.MustCompile(`'([a-z0-9_]+)'`)
)

// wpTables là số bảng của một DB WordPress mới cài.
const wpTables = 12

// dbSize cho dung lượng giả của một DB: từ 1,5 MiB tới dưới 8 MiB, cố định theo
// tên để mỗi lần chạy demo ra cùng số, và khác nhau giữa các site.
func dbSize(name string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return 3<<19 + int64(h.Sum32()%(13<<19))
}

// mariadb trả lời câu SQL đưa qua stdin. Câu của checks.MariaDBSecure cũng chứa
// SCHEMA_NAME='test', nên phải nhận nó trước khi xét câu trạng thái DB của site.
func (m *machine) mariadb(c system.Cmd) system.Result {
	sql := string(c.Stdin)
	if strings.Contains(sql, "'root_socket'") {
		return exit(0, "root_socket\t1\nanonymous\t0\ntest_db\t0\n", "")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if res, ok := m.mariadbM13(sql); ok {
		return res
	}
	if res, ok := m.mariadbLock(sql); ok {
		return res
	}
	if res, ok := m.mariadbLimit(sql); ok {
		return res
	}
	// Màn Cơ sở dữ liệu hỏi dung lượng và số bảng của các DB trong một câu
	// (core.Sites.Databases): mỗi DB có một dòng "tên<TAB>số bảng<TAB>byte".
	if i := strings.Index(sql, "SCHEMA_NAME IN ("); i >= 0 && strings.Contains(sql, "information_schema.SCHEMATA") {
		var out strings.Builder
		for _, g := range sqlQuoted.FindAllStringSubmatch(sql[i:], -1) {
			if m.dbs[g[1]] {
				fmt.Fprintf(&out, "%s\t%d\t%d\n", g[1], wpTables, dbSize(g[1]))
			}
		}
		return exit(0, out.String(), "")
	}
	if g := sqlStateRE.FindStringSubmatch(sql); g != nil {
		b := func(v bool) int {
			if v {
				return 1
			}
			return 0
		}
		return exit(0, fmt.Sprintf("%d\t%d\n", b(m.dbs[g[1]]), b(m.dbUsers[g[1]])), "")
	}
	for _, g := range sqlCreateDB.FindAllStringSubmatch(sql, -1) {
		m.dbs[g[1]] = true
	}
	for _, g := range sqlCreateUsr.FindAllStringSubmatch(sql, -1) {
		m.dbUsers[g[1]] = true
	}
	for _, g := range sqlDropUsr.FindAllStringSubmatch(sql, -1) {
		delete(m.dbUsers, g[1])
		delete(m.dbLocked, g[1])
	}
	for _, g := range sqlDropDB.FindAllStringSubmatch(sql, -1) {
		delete(m.dbs, g[1])
	}
	return okResult
}

// curl có bốn cách dùng trong tpan, phân biệt bằng tham số:
//
//	--resolve   checks.HTTPSite, HTTPRedirect, HTTPSSite: gọi vào site ở cổng
//	            80 hoặc 443, trả mã theo trạng thái của site;
//	-w          checks.HTTPDefault: nginx đóng kết nối request lạ (curl mã 52);
//	acme        steps.ACMEProbe: gọi http://<domain>/.well-known/acme-challenge/<tên>
//	            (ssl.go);
//	còn lại     tải WordPress hoặc WP-CLI vào đường dẫn sau -o.
//
// Thứ tự này quan trọng: HTTPDefault cũng có -o (/dev/null), HTTPRedirect có cả -w.
func (m *machine) curl(c system.Cmd) system.Result {
	if r := argAfter(c.Args, "--resolve"); r != "" {
		dom, rest, _ := strings.Cut(r, ":")
		port, _, _ := strings.Cut(rest, ":")
		return m.httpSite(dom, port, slices.Contains(c.Args, "-D"))
	}
	if slices.Contains(c.Args, "-w") {
		return exit(52, "000", "curl: (52) Empty reply from server\n")
	}
	if res, ok := m.acmeProbe(c.Args); ok {
		return res
	}
	if p := argAfter(c.Args, "-o"); p != "" {
		data := fakeWordPress
		if strings.Contains(strings.Join(c.Args, " "), "wp-cli") {
			data = fakeWPCLI
		}
		m.SetFile(p, data, 0o600, rootOwner)
	}
	return okResult
}

// httpSite giả lập một request tới site qua nginx và PHP-FPM, ở cổng port
// ("80" hoặc "443"); headers: curl được gọi với -D - (in header thay vì thân).
// Vhost chưa bật (site chưa tạo hoặc đã vào thùng rác) thì request rơi vào
// site mặc định của tpan: nginx trả 444, tức đóng kết nối không trả lời, curl
// mã 52 như trên máy thật (phép kiểm "domain không còn được phục vụ" của thùng
// rác chờ đúng mã này). Vhost có SSL (ssl.go) thì cổng 80 chuyển hướng 301
// sang https; không có SSL thì cổng 443 không nối được. Socket của pool mất thì
// 502; còn lại là trang WordPress của site.
func (m *machine) httpSite(dom, port string, headers bool) system.Result {
	l, ok := siteOf(dom)
	if !ok {
		return exit(6, "", "curl: (6) Could not resolve host\n")
	}
	if _, err := m.Stat(l.NginxLink); err != nil {
		return exit(52, "", "curl: (52) Empty reply from server\n")
	}
	ssl := m.vhostSSL(l)
	reply := func(code, status, extra, body string) system.Result {
		if headers {
			return exit(0, "HTTP/1.1 "+code+" "+status+"\r\nServer: nginx\r\n"+extra+"\r\nTPAN_HTTP_CODE="+code, "")
		}
		return exit(0, body+"TPAN_HTTP_CODE="+code, "")
	}
	scheme := "http"
	switch {
	case port == "443" && !ssl:
		return exit(7, "", "curl: (7) Failed to connect to "+dom+" port 443: Connection refused\n")
	case port == "443":
		scheme = "https"
	case ssl:
		loc := "https://" + dom + "/"
		return reply("301", "Moved Permanently", "Location: "+loc+"\r\n",
			"<html><head><title>301 Moved Permanently</title></head><body>301 Moved Permanently</body></html>")
	}
	if _, err := m.Stat(m.vhostSocket(l)); err != nil {
		return reply("502", "Bad Gateway", "", "<html><head><title>502 Bad Gateway</title></head><body>502 Bad Gateway</body></html>")
	}
	body := "<html><head><link rel='stylesheet' href='" + scheme + "://" + dom + "/wp-content/themes/x/style.css'></head><body>" + dom + "</body></html>"
	return reply("200", "OK", "", body)
}

// wp giả lập WP-CLI, chạy dưới user của site.
func (m *machine) wp(c system.Cmd) system.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, pub := c.AsUser, argPrefix(c.Args, "--path=")
	switch {
	case len(c.Args) >= 2 && c.Args[0] == "config" && c.Args[1] == "create":
		m.SetFile(pub+"/wp-config.php", []byte("<?php // wp-config giả của demo\n"), 0o644, system.Owner{User: u, Group: u})
	case len(c.Args) >= 2 && c.Args[0] == "core" && c.Args[1] == "is-installed":
		if !m.installed[u] {
			return exit(1, "", "")
		}
	case len(c.Args) >= 2 && c.Args[0] == "core" && c.Args[1] == "install":
		m.installed[u] = true
		url := argPrefix(c.Args, "--url=")
		m.setOption(u, "home", url)
		m.setOption(u, "siteurl", url)
	case len(c.Args) >= 3 && c.Args[0] == "option" && c.Args[1] == "get":
		// Tuỳ chọn chưa đặt thì in dòng rỗng, như permalink_structure của một
		// WordPress mới cài.
		return exit(0, m.options[u][c.Args[2]]+"\n", "")
	case len(c.Args) >= 4 && c.Args[0] == "option" && c.Args[1] == "update":
		m.setOption(u, c.Args[2], c.Args[3])
	case len(c.Args) >= 3 && c.Args[0] == "rewrite" && c.Args[1] == "structure":
		m.setOption(u, "permalink_structure", c.Args[2])
	case len(c.Args) >= 2 && c.Args[0] == "db" && c.Args[1] == "query":
		// DB của site cùng tên với user của site (tp_<slug>) và là DB duy nhất nó thấy.
		return exit(0, "information_schema\n"+u+"\n", "")
	}
	return okResult
}

// setOption ghi một tuỳ chọn WordPress của user u. Người gọi giữ m.mu.
func (m *machine) setOption(u, name, val string) {
	if m.options[u] == nil {
		m.options[u] = map[string]string{}
	}
	m.options[u][name] = val
}

// fpmVersion đọc bản PHP từ tên dịch vụ "php8.4-fpm".
func fpmVersion(unit string) (string, bool) {
	v, ok := strings.CutPrefix(unit, "php")
	v, ok2 := strings.CutSuffix(v, "-fpm")
	return v, ok && ok2 && phpVerRE.MatchString(v)
}

var (
	phpVerRE      = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	fastcgiSockRE = regexp.MustCompile(`unix:(/run/php/[A-Za-z0-9._-]+\.sock)`)
)

// vhostSocket là socket PHP-FPM mà vhost của site đang trỏ tới (fastcgi_pass
// unix:...), như web server thật: site đổi bản PHP thì vhost đổi socket. Không
// đọc được thì "" (Stat lỗi, request ra 502).
func (m *machine) vhostSocket(l site.Layout) string {
	conf, err := m.ReadFile(l.NginxConf)
	if err != nil {
		// Đã chuyển sang Apache (M10): vhost nằm ở /etc/apache2.
		if conf, err = m.ReadFile(l.ApacheConf); err != nil {
			return ""
		}
	}
	if g := fastcgiSockRE.FindSubmatch(conf); g != nil {
		return string(g[1])
	}
	return ""
}

// syncSockets làm cho socket của bản PHP v khớp với các file pool của bản đó:
// mỗi tpan-*.conf trong pool.d có một socket, pool đã gỡ thì mất socket. Pool
// đang hỏng (broken) thì không có socket dù file pool còn. Socket của pool giữ
// chỗ tpan-idle chỉ root dùng được (0600), như template của nó.
func (m *machine) syncSockets(v string) {
	names, _ := m.ReadDir("/etc/php/" + v + "/fpm/pool.d")
	want := map[string]bool{}
	for _, n := range names {
		if pool, ok := strings.CutSuffix(n, ".conf"); ok && strings.HasPrefix(pool, "tpan-") {
			want["/run/php/"+pool+"-"+v+".sock"] = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for s := range m.sockets {
		if strings.HasSuffix(s, "-"+v+".sock") && (!want[s] || m.broken[s]) {
			_ = m.Remove(s)
			delete(m.sockets, s)
		}
	}
	for s := range want {
		switch {
		case m.broken[s]:
		case s == "/run/php/tpan-idle-"+v+".sock":
			m.SetSocket(s, 0o600, rootOwner)
			m.sockets[s] = true
		default:
			m.SetSocket(s, 0o660, wwwOwner)
			m.sockets[s] = true
		}
	}
}

// breakPool làm hỏng pool PHP-FPM của site: xoá socket và giữ nó mất qua các
// lần reload sau. Đây là kịch bản "xoá socket của một pool" của scripts/faults
// (mục 6.7), chạy trên máy giả.
func (m *machine) breakPool(l site.Layout) error {
	m.mu.Lock()
	m.broken[l.SocketFor(phpVer)] = true
	delete(m.sockets, l.SocketFor(phpVer))
	m.mu.Unlock()
	if err := m.Remove(l.SocketFor(phpVer)); err != nil {
		return fmt.Errorf("xoá socket %s: %w", l.SocketFor(phpVer), err)
	}
	return nil
}

// removeUnder xoá mọi thứ bên dưới p (sâu trước), và cả p nếu self.
func (m *machine) removeUnder(p string, self bool) {
	var paths []string
	for k := range m.Snapshot() {
		if strings.HasPrefix(k, p+"/") || (self && k == p) {
			paths = append(paths, k)
		}
	}
	slices.SortFunc(paths, func(a, b string) int { return len(b) - len(a) })
	for _, k := range paths {
		_ = m.Remove(k)
	}
}

// ---------------------------------------------------------------- /proc

// load là mức bận (0..1) của lõi core ở nhịp thứ tick: một sóng chậm lệch pha
// theo từng lõi, nằm trong khoảng 5% tới 30%, để biểu đồ trên giao diện có
// hình dạng chứ không phải một đường phẳng.
func load(tick, core int) float64 {
	return 0.175 + 0.125*math.Sin(float64(tick)/60+float64(core)*1.3)
}

// writeProc ghi /proc/stat, /proc/meminfo, /proc/loadavg, /proc/uptime theo
// nhịp hiện tại, sau khi tăng bộ đếm CPU thêm một nhịp. /proc/stat đổi mỗi nhịp
// để hai lần đọc cách nhau ngắn thấy chênh lệch; ba file kia đổi chậm nên
// chỉ ghi mỗi giây (xem runProc).
func (m *machine) writeProc() {
	m.mu.Lock()
	m.ticks++
	n := m.ticks
	var tot cpuTimes
	var b strings.Builder
	perTick := uint64(procTick / (10 * time.Millisecond)) // jiffies (1/100 giây) mỗi nhịp
	lines := make([]string, numCPU)
	for i := range m.cpu {
		busy := uint64(math.Round(load(n, i) * float64(perTick)))
		c := &m.cpu[i]
		c.user += busy - busy/4
		c.system += busy / 4
		c.idle += perTick - busy
		tot.user, tot.system, tot.idle = tot.user+c.user, tot.system+c.system, tot.idle+c.idle
		lines[i] = fmt.Sprintf("cpu%d %d 0 %d %d 0 0 0 0 0 0\n", i, c.user, c.system, c.idle)
	}
	m.mu.Unlock()

	fmt.Fprintf(&b, "cpu  %d 0 %d %d 0 0 0 0 0 0\n", tot.user, tot.system, tot.idle)
	for _, l := range lines {
		b.WriteString(l)
	}
	b.WriteString("intr 91827364\nctxt 163482910\nbtime 1790000000\nprocesses 48211\nprocs_running 1\nprocs_blocked 0\n")
	m.SetFile("/proc/stat", []byte(b.String()), 0o444, rootOwner)

	if n%10 != 1 { // ba file chậm: ghi ở lần đầu rồi mỗi 10 nhịp
		return
	}
	secs := float64(n) * procTick.Seconds()
	avail := memTotalKB*62/100 + int(float64(memTotalKB)*0.03*math.Sin(secs/90))
	mem := fmt.Sprintf("MemTotal:       %d kB\nMemFree:        %d kB\nMemAvailable:   %d kB\nBuffers:        148212 kB\nCached:         %d kB\n"+
		"SwapCached:          0 kB\nSwapTotal:      2097148 kB\nSwapFree:       %d kB\n",
		memTotalKB, avail/2, avail, avail/2, 2097148-9_728)
	m.SetFile("/proc/meminfo", []byte(mem), 0o444, rootOwner)

	var l1 float64
	for i := range numCPU {
		l1 += load(n, i)
	}
	m.SetFile("/proc/loadavg", fmt.Appendf(nil, "%.2f 0.31 0.27 2/318 %d\n", l1, 48211+n/10), 0o444, rootOwner)
	up := 1_083_000 + secs
	m.SetFile("/proc/uptime", fmt.Appendf(nil, "%.2f %.2f\n", up, up*3.6), 0o444, rootOwner)
}

// runProc cập nhật /proc theo nhịp cho tới khi ctx bị huỷ.
func (m *machine) runProc(ctx context.Context) {
	t := time.NewTicker(procTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.writeProc()
		}
	}
}
