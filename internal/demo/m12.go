package demo

// Phần M12 của máy giả (mục 6.11 của docs/KE-HOACH.md): nhiều bản PHP, gói chờ
// nâng, bản vá tự động, nâng MariaDB. Trả lời đúng các lệnh mà core.PHP,
// core.Updates, core.SelfUpdate chạy khi đọc và khi xem trước, và các lệnh của
// Plan php.install, php.use, php.remove, update.apply, update.auto,
// mariadb.upgrade, để những job đó chạy trọn trên máy giả.
//
// Trạng thái lúc dựng:
//   - PHP 8.3 của Ubuntu đã cài. Kho PHP của Ondřej Surý đã được thêm từ trước
//     (máy từng cài PHP 8.2 từ kho này rồi gỡ: file ghim còn dòng php8.2*), nên
//     danh sách có 8.1 tới 8.5 và cài 8.4 thì ghi lại file ghim của kho.
//   - Gói của stack chờ nâng: nginx, PHP 8.3 (bản vá thường) và curl (bản vá
//     bảo mật); gói khác của hệ điều hành: openssl, tzdata. Đúng 2 dòng của kho
//     -security (curl, libcurl4t64), khớp trang bảo mật ("2 gói bảo mật").
//   - Bản vá tự động đang bật (unattended-upgrades đã cài, giá trị "1").
//   - MariaDB 10.11 của Ubuntu; REST của MariaDB trả các dòng LTS thật.
//   - Ghi chú php.net, WordPress.org, MariaDB là JSON mẫu theo đúng dạng đã
//     kiểm trên máy ảo (docs/thay-doi/M12.md, "Kết quả kiểm cho mục 16").
//
// Nâng PHP 8.3 khởi động lại php8.3-fpm, mà khởi động lại thì chữa pool hỏng
// của wiki (machine.go): như máy thật, cập nhật xong site wiki chạy lại.

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

const (
	// suryFpr là vân tay khoá của kho sury (docs/thay-doi/M12.md). Chỉ dùng cho
	// khoá đã thêm sẵn lúc dựng máy; khoá tải về trong job mang đúng vân tay
	// mà core đưa cho gpg --export.
	suryFpr = "15058500A0235D97F5D10063B188E2B695BD4743"
	// demoDBVersion là bản mariadb-server của Ubuntu 24.04.
	demoDBVersion = "1:10.11.13-0ubuntu0.24.04.1"
)

// Bản PHP mỗi kho có. Kho Ubuntu 24.04 chỉ có 8.3.
var (
	ubuntuPHP = []string{phpVer}
	suryPHP   = []string{"8.1", "8.2", "8.3", "8.4", "8.5"}
	// phpExts là bộ gói của một bản PHP (mục 3), cùng bộ core cài.
	phpExts      = []string{"fpm", "cli", "mysql", "curl", "gd", "mbstring", "xml", "zip", "intl"}
	mariadbPkgs  = []string{"mariadb-client", "mariadb-client-core", "mariadb-common", "mariadb-server", "mariadb-server-core"}
	mariadbRepoR = regexp.MustCompile(`mariadb-server/([0-9]+\.[0-9]+)/`)
	dbVersionRE  = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?`)
)

// demoPkg là một gói đang có bản mới.
type demoPkg struct {
	name, from, to string
	origins        []string
	arch           string
}

func (p demoPkg) inst() string {
	return "Inst " + p.name + " [" + p.from + "] (" + p.to + " " + strings.Join(p.origins, ", ") + " [" + p.arch + "])"
}

var (
	updates  = []string{"Ubuntu:24.04/noble-updates"}
	security = []string{"Ubuntu:24.04/noble-updates", "Ubuntu:24.04/noble-security"}
	// demoPending: gói chờ nâng lúc dựng máy, theo thứ tự apt in ra.
	demoPending = []demoPkg{
		{"curl", "8.5.0-2ubuntu10.6", "8.5.0-2ubuntu10.7", security, "amd64"},
		{"libcurl4t64", "8.5.0-2ubuntu10.6", "8.5.0-2ubuntu10.7", security, "amd64"},
		{"libssl3t64", "3.0.13-0ubuntu3.5", "3.0.13-0ubuntu3.6", updates, "amd64"},
		{"nginx", "1.24.0-2ubuntu7.4", "1.24.0-2ubuntu7.5", updates, "amd64"},
		{"nginx-common", "1.24.0-2ubuntu7.4", "1.24.0-2ubuntu7.5", updates, "all"},
		{"openssl", "3.0.13-0ubuntu3.5", "3.0.13-0ubuntu3.6", updates, "amd64"},
		{"php8.3-cli", "8.3.6-0ubuntu0.24.04.13", "8.3.6-0ubuntu0.24.04.14", updates, "amd64"},
		{"php8.3-common", "8.3.6-0ubuntu0.24.04.13", "8.3.6-0ubuntu0.24.04.14", updates, "amd64"},
		{"php8.3-fpm", "8.3.6-0ubuntu0.24.04.13", "8.3.6-0ubuntu0.24.04.14", updates, "amd64"},
		{"php8.3-mysql", "8.3.6-0ubuntu0.24.04.13", "8.3.6-0ubuntu0.24.04.14", updates, "amd64"},
		{"tzdata", "2025b-0ubuntu0.24.04", "2025b-0ubuntu0.24.04.1", updates, "all"},
	}
)

// Câu trả lời mẫu của ba nguồn ghi chú, dạng như đã kiểm trên máy ảo.
const (
	phpStatesJSON = `{"8":{` +
		`"8.2":{"state":"security","initial_release":"2022-12-08T00:00:00+00:00","active_support_end":"2024-12-31T00:00:00+00:00","security_support_end":"2026-12-31T00:00:00+00:00"},` +
		`"8.3":{"state":"security","initial_release":"2023-11-23T00:00:00+00:00","active_support_end":"2025-12-31T00:00:00+00:00","security_support_end":"2027-12-31T00:00:00+00:00"},` +
		`"8.4":{"state":"stable","initial_release":"2024-11-21T00:00:00+00:00","active_support_end":"2026-12-31T00:00:00+00:00","security_support_end":"2028-12-31T00:00:00+00:00"},` +
		`"8.5":{"state":"stable","initial_release":"2025-11-20T00:00:00+00:00","active_support_end":"2027-12-31T00:00:00+00:00","security_support_end":"2029-12-31T00:00:00+00:00"}}}`
	wpServeHappyJSON = `{"recommended_version":"8.3","minimum_version":"7.4","is_supported":true,"is_secure":true,"is_acceptable":true}`
	mariadbRESTJSON  = `{"major_releases":[` +
		`{"release_id":"13.2","release_name":"MariaDB Server 13.2","release_status":"Preview","release_support_type":"Short Term Support","release_eol_date":null},` +
		`{"release_id":"13.1","release_name":"MariaDB Server 13.1","release_status":"RC","release_support_type":"Short Term Support","release_eol_date":null},` +
		`{"release_id":"13.0","release_name":"MariaDB Server 13.0","release_status":"Stable","release_support_type":"Rolling Release","release_eol_date":null},` +
		`{"release_id":"12.3","release_name":"MariaDB Server 12.3","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2029-06-12"},` +
		`{"release_id":"11.8","release_name":"MariaDB Server 11.8","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2030-02-13"},` +
		`{"release_id":"11.4","release_name":"MariaDB Server 11.4","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2029-05-29"},` +
		`{"release_id":"10.11","release_name":"MariaDB Server 10.11","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2028-02-16"}]}`
)

// m12 là trạng thái riêng của phần này. Tách khỏi machine để machine.go không
// phải đổi; khoá riêng, không giữ m.mu khi gọi hàm của machine tự khoá.
type m12 struct {
	*machine
	mu        sync.Mutex
	upgraded  map[string]bool // gói của demoPending đã nâng
	dbVersion string          // bản mariadb-server đang cài
	tmpN      int             // đếm thư mục tạm của mktemp
}

func phpPkgs(v string) []string {
	out := make([]string, len(phpExts))
	for i, e := range phpExts {
		out[i] = "php" + v + "-" + e
	}
	return out
}

// m12Setup đặt file và handler của phần M12. Gọi sau securityHandlers.
func (m *machine) m12Setup() {
	x := &m12{machine: m, upgraded: map[string]bool{}, dbVersion: demoDBVersion}
	x.files()

	m.Handle([]string{"apt-cache", "search"}, x.aptCacheSearch)
	m.Handle([]string{"apt-cache", "madison"}, x.aptCacheMadison)
	m.Handle([]string{"curl", "-fsS"}, x.notes)
	m.Handle([]string{"mktemp", "-d"}, x.mktemp)
	m.Handle([]string{"gpg"}, x.gpg)
	m.Handle([]string{"apt-get", "-s"}, x.simulate)
	m.Handle([]string{"apt-get", "install", "-y"}, x.install)
	m.Handle([]string{"apt-get", "install", "-y", "--only-upgrade"}, x.upgrade)
	m.Handle([]string{"apt-get", "remove", "-y"}, x.remove)
	m.Handle([]string{"apt-config", "dump"}, func(system.Cmd) system.Result {
		return exit(0, "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n", "")
	})
	m.Handle([]string{"dpkg-query", "-W"}, x.dpkgW)
	// Dịch vụ dừng, bật thật sự đổi trạng thái (mariadb.upgrade dừng rồi bật
	// lại mariadb; Service bật php<bản>-fpm vừa cài).
	for verb, on := range map[string]bool{"stop": false, "start": true, "enable --now": true} {
		m.Handle(append([]string{"systemctl"}, strings.Fields(verb)...), func(c system.Cmd) system.Result {
			m.setUnit(c.Args[len(c.Args)-1], on)
			return okResult
		})
	}
	m.Handle([]string{"tail", "-n", "1", "--"}, x.tailLast)
	m.Handle([]string{"mariadb-dump"}, x.dump)
	m.Handle([]string{"mariadb", "-N", "-B", "-e"}, func(system.Cmd) system.Result {
		return exit(0, x.dbRunning()+"\n", "")
	})
	m.Handle([]string{"mariadb-upgrade"}, func(system.Cmd) system.Result {
		v, _, _ := strings.Cut(x.dbRunning(), "-MariaDB")
		m.SetFile("/var/lib/mysql/mariadb_upgrade_info", []byte(v+"-MariaDB"), 0o644, system.Owner{User: "mysql", Group: "mysql"})
		return exit(0, "Phase 1/8: Checking and upgrading mysql database\n...\nOK\n", "")
	})
}

// files đặt file của các thứ đã cài: file chạy của PHP 8.3, kho sury đã thêm.
func (x *m12) files() {
	m := x.machine
	m.SetFile("/usr/sbin/php-fpm"+phpVer, []byte("ELF php-fpm giả\n"), 0o755, rootOwner)
	m.SetFile("/etc/apt/apt.conf.d/20auto-upgrades", []byte("APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"), 0o644, rootOwner)
	m.SetDir("/var/lib/mysql", 0o755, system.Owner{User: "mysql", Group: "mysql"})
	m.SetDir("/tmp", 0o1777, rootOwner)

	// Kho sury như core ghi (repoSury trong core/repos.go), với ghim của 8.2.
	src, err1 := templates.AptSources(templates.AptSource{URIs: "https://packages.sury.org/php/", Suites: "noble", Components: "main", KeyPath: steps.AptKeyPath(core.RepoSury)})
	pin, err2 := templates.AptPin([]templates.AptPinRule{
		{Packages: []string{"*"}, Origin: "packages.sury.org", Priority: 100},
		{Packages: []string{"php-common", "php8.2*"}, Origin: "packages.sury.org", Priority: 500},
	})
	if err1 == nil && err2 == nil {
		m.SetFile(core.RepoSourcesPath(core.RepoSury), src, 0o644, rootOwner)
		m.SetFile("/etc/apt/preferences.d/tpan-"+core.RepoSury, pin, 0o644, rootOwner)
		m.SetFile(steps.AptKeyPath(core.RepoSury), []byte(suryFpr+"\n"), 0o644, rootOwner)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Bản PHP của kho sury chưa cài; panel đang chạy (tpan.service).
	for _, v := range suryPHP {
		if !slices.Contains(ubuntuPHP, v) {
			for _, p := range phpPkgs(v) {
				m.missing[p] = true
			}
		}
	}
	m.units["tpan"] = true
}

// setUnit bật (chạy và tự bật) hoặc dừng dịch vụ unit.
func (m *machine) setUnit(unit string, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if on {
		m.units[unit] = true
	} else {
		delete(m.units, unit)
	}
}

func (x *m12) suryOn() bool {
	_, err := x.Stat(core.RepoSourcesPath(core.RepoSury))
	return err == nil
}

// ---------------------------------------------------------------- PHP

func (x *m12) aptCacheSearch(system.Cmd) system.Result {
	vs := slices.Clone(ubuntuPHP)
	if x.suryOn() {
		vs = append(vs, suryPHP...)
	}
	slices.Sort(vs)
	var b strings.Builder
	for _, v := range slices.Compact(vs) {
		b.WriteString("php" + v + "-fpm - server-side, HTML-embedded scripting language (FPM-CGI binary)\n")
	}
	return exit(0, b.String(), "")
}

// aptCacheMadison: "php8.3-fpm | 8.3.6-0ubuntu0.24.04.13 | http://... noble-updates/universe amd64 Packages".
func (x *m12) aptCacheMadison(c system.Cmd) system.Result {
	sury := x.suryOn()
	var b strings.Builder
	for _, pkg := range c.Args[1:] {
		v, ok := strings.CutPrefix(pkg, "php")
		if v, ok2 := strings.CutSuffix(v, "-fpm"); ok && ok2 {
			if slices.Contains(ubuntuPHP, v) {
				b.WriteString(" " + pkg + " | " + v + ".6-0ubuntu0.24.04.13 | http://archive.ubuntu.com/ubuntu noble-updates/universe amd64 Packages\n")
			}
			if sury && slices.Contains(suryPHP, v) {
				b.WriteString(" " + pkg + " | " + v + ".1-1+ubuntu24.04.1+deb.sury.org+1 | https://packages.sury.org/php noble/main amd64 Packages\n")
			}
		}
	}
	return exit(0, b.String(), "")
}

// install bọc aptInstall (security.go): gói php<bản>-fpm vừa cài thì có thư
// mục, file chạy, pool www mặc định và dịch vụ chạy ngay, như gói thật.
func (x *m12) install(c system.Cmd) system.Result {
	if res := x.aptInstall(c); res.ExitCode != 0 {
		return res
	}
	for _, p := range aptPkgArgs(c.Args) {
		v, ok := fpmVersion(p)
		if !ok {
			continue
		}
		dir := "/etc/php/" + v + "/fpm/pool.d"
		if _, err := x.Stat("/usr/sbin/php-fpm" + v); err == nil {
			continue
		}
		x.SetDir(dir, 0o755, rootOwner)
		x.SetFile(dir+"/www.conf", []byte("[www]\nuser = www-data\ngroup = www-data\nlisten = /run/php/php"+v+"-fpm.sock\npm = dynamic\npm.max_children = 5\n"), 0o644, rootOwner)
		x.SetFile("/usr/sbin/php-fpm"+v, []byte("ELF php-fpm giả\n"), 0o755, rootOwner)
		x.setUnit("php"+v+"-fpm", true)
		x.syncSockets(v)
	}
	return okResult
}

// remove gỡ gói: chưa cài lại, file chạy và dịch vụ mất; /etc/php/<bản> còn
// (apt-get remove giữ file cấu hình), socket của bản đó mất.
func (x *m12) remove(c system.Cmd) system.Result {
	for _, p := range aptPkgArgs(c.Args) {
		x.machine.mu.Lock()
		x.missing[p] = true
		x.machine.mu.Unlock()
		if v, ok := fpmVersion(p); ok {
			_ = x.Remove("/usr/sbin/php-fpm" + v)
			x.setUnit("php"+v+"-fpm", false)
			x.machine.mu.Lock()
			for s := range x.sockets {
				if strings.HasSuffix(s, "-"+v+".sock") {
					_ = x.Remove(s)
					delete(x.sockets, s)
				}
			}
			x.machine.mu.Unlock()
		}
	}
	return okResult
}

// ---------------------------------------------------------------- ghi chú

// notes trả lời `curl -fsS ... <url>` của ghi chú php.net, WordPress.org và
// REST của MariaDB. URL lạ thì 404 như curl -f.
func (x *m12) notes(c system.Cmd) system.Result {
	u := c.Args[len(c.Args)-1]
	switch {
	case u == "https://www.php.net/releases/states.php":
		return exit(0, phpStatesJSON, "")
	case strings.HasPrefix(u, "https://api.wordpress.org/core/serve-happy/1.0/?php_version="):
		return exit(0, wpServeHappyJSON, "")
	case u == "https://downloads.mariadb.org/rest-api/mariadb/":
		return exit(0, mariadbRESTJSON, "")
	}
	return exit(22, "", "curl: (22) The requested URL returned error: 404\n")
}

// ---------------------------------------------------------------- khoá kho apt

// mktemp -d -p /tmp tpan-apt-key.XXXXXXXX: tám ký tự cuối là chữ và số.
func (x *m12) mktemp(system.Cmd) system.Result {
	x.mu.Lock()
	x.tmpN++
	n := x.tmpN
	x.mu.Unlock()
	dir := fmt.Sprintf("/tmp/tpan-apt-key.demo%04d", n%10000)
	x.SetDir(dir, 0o700, rootOwner)
	return exit(0, dir+"\n", "")
}

// gpg giả: --export ghi vào file --output đúng các vân tay được hỏi (khoá tải
// về luôn "có" khoá đó); --show-keys đọc lại các vân tay ấy ra dạng
// --with-colons. Như vậy steps.AptRepo kiểm được vân tay mà máy giả không phải
// biết khoá thật. --import và các lệnh khác: không làm gì.
func (x *m12) gpg(c system.Cmd) system.Result {
	switch {
	case slices.Contains(c.Args, "--export"):
		i := slices.Index(c.Args, "--export")
		out := argAfter(c.Args, "--output")
		x.SetFile(out, []byte(strings.Join(c.Args[i+1:], "\n")+"\n"), 0o600, rootOwner)
	case slices.Contains(c.Args, "--show-keys"):
		data, err := x.ReadFile(c.Args[len(c.Args)-1])
		if err != nil {
			return exit(2, "", "gpg: can't open '"+c.Args[len(c.Args)-1]+"': No such file or directory\n")
		}
		var b strings.Builder
		for _, f := range strings.Fields(string(data)) {
			b.WriteString("pub:-:4096:1::1700000000:::-:::scESC::::::23::0:\n")
			b.WriteString("fpr:::::::::" + f + ":\n")
		}
		return exit(0, b.String(), "")
	}
	return okResult
}

// ---------------------------------------------------------------- gói chờ nâng

// pending là các gói đang có bản mới: phần của demoPending chưa nâng, cộng
// các gói MariaDB khi kho của MariaDB (dòng cao hơn dòng đang chạy) đã thêm.
func (x *m12) pending() []demoPkg {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []demoPkg
	for _, p := range demoPending {
		if !x.upgraded[p.name] {
			out = append(out, p)
		}
	}
	src, err := x.ReadFile(core.RepoSourcesPath(core.RepoMariaDB))
	if err != nil {
		return out
	}
	g := mariadbRepoR.FindSubmatch(src)
	if g == nil || !lineNewer(string(g[1]), x.dbLine()) {
		return out
	}
	to := "1:" + string(g[1]) + ".5+maria~ubu2404"
	for _, n := range mariadbPkgs {
		out = append(out, demoPkg{n, x.dbVersion, to, []string{"MariaDB:" + string(g[1]) + "/noble"}, "amd64"})
	}
	slices.SortFunc(out, func(a, b demoPkg) int { return strings.Compare(a.name, b.name) })
	return out
}

// dbLine là dòng MariaDB đang cài ("10.11"). Người gọi giữ x.mu.
func (x *m12) dbLine() string {
	v := x.dbVersion
	if _, after, ok := strings.Cut(v, ":"); ok {
		v = after
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

// lineNewer: dòng a ("11.8") cao hơn dòng b ("10.11"), so theo số.
func lineNewer(a, b string) bool {
	var am, an, bm, bn int
	_, _ = fmt.Sscanf(a, "%d.%d", &am, &an)
	_, _ = fmt.Sscanf(b, "%d.%d", &bm, &bn)
	return am > bm || (am == bm && an > bn)
}

// aptPkgArgs là tên gói trong argv của apt-get: bỏ lệnh (install, remove...),
// cờ, và cặp "-o <giá trị>".
func aptPkgArgs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o":
			i++
		case strings.HasPrefix(a, "-"), a == "install", a == "remove", a == "dist-upgrade", a == "upgrade":
		default:
			out = append(out, a)
		}
	}
	return out
}

// simulate trả lời `apt-get -s ... dist-upgrade` (mọi gói chờ) và
// `apt-get -s install --only-upgrade ... <gói>` (chỉ các gói được hỏi).
func (x *m12) simulate(c system.Cmd) system.Result {
	ask := aptPkgArgs(c.Args)
	var inst []string
	for _, p := range x.pending() {
		if slices.Contains(c.Args, "dist-upgrade") || slices.Contains(ask, p.name) {
			inst = append(inst, p.inst())
		}
	}
	out := "NOTE: This is only a simulation!\nReading package lists...\nBuilding dependency tree...\nReading state information...\nCalculating upgrade...\n"
	out += fmt.Sprintf("%d upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", len(inst))
	for _, l := range inst {
		out += l + "\n"
	}
	return exit(0, out, "")
}

// upgrade nâng các gói được hỏi đang có bản mới.
func (x *m12) upgrade(c system.Cmd) system.Result {
	ask := aptPkgArgs(c.Args)
	for _, p := range x.pending() {
		if !slices.Contains(ask, p.name) {
			continue
		}
		x.mu.Lock()
		if slices.Contains(mariadbPkgs, p.name) {
			x.dbVersion = p.to
		} else {
			x.upgraded[p.name] = true
		}
		x.mu.Unlock()
	}
	return exit(0, "Reading package lists...\nThe following packages will be upgraded:\n  "+strings.Join(ask, " ")+"\n", "")
}

// ---------------------------------------------------------------- dpkg-query -W

// dpkgW trả lời ba dạng `dpkg-query -W -f <format> <mẫu>...` của core và
// checks: trạng thái một gói, trạng thái kèm bản (mariadb-server), và danh
// sách "tên<TAB>trạng thái" theo mẫu (php8.4-*, mariadb-* galera-*). Mẫu không
// khớp gói nào thì thoát 1 như dpkg-query thật, vẫn in các gói khớp mẫu khác.
func (x *m12) dpkgW(c system.Cmd) system.Result {
	format := argAfter(c.Args, "-f")
	i := slices.Index(c.Args, "-f")
	patterns := c.Args[i+2:]
	x.mu.Lock()
	dbv := x.dbVersion
	x.mu.Unlock()

	known := slices.Concat(mariadbPkgs, []string{"unattended-upgrades"})
	for _, v := range suryPHP {
		known = append(known, phpPkgs(v)...)
	}
	installed := func(p string) bool {
		x.machine.mu.Lock()
		defer x.machine.mu.Unlock()
		return !x.missing[p]
	}

	var out, errOut strings.Builder
	for _, pat := range patterns {
		found := false
		for _, p := range known {
			if ok, _ := path.Match(pat, p); !ok || !installed(p) {
				continue
			}
			found = true
			switch format {
			case `${Package}\t${db:Status-Status}\n`:
				out.WriteString(p + "\tinstalled\n")
			case `${db:Status-Status}\t${Version}`:
				out.WriteString("installed\t" + dbv)
			default:
				out.WriteString("installed\n")
			}
		}
		if !found {
			errOut.WriteString("dpkg-query: no packages found matching " + pat + "\n")
		}
	}
	if errOut.Len() > 0 {
		return exit(1, out.String(), errOut.String())
	}
	return exit(0, out.String(), "")
}

// ---------------------------------------------------------------- MariaDB

// dbRunning là kết quả SELECT VERSION() của bản đang cài: "11.8.5-MariaDB-ubu2404".
func (x *m12) dbRunning() string {
	x.mu.Lock()
	v := x.dbVersion
	x.mu.Unlock()
	if _, after, ok := strings.Cut(v, ":"); ok {
		v = after
	}
	return dbVersionRE.FindString(v) + "-MariaDB-ubu2404"
}

// dump ghi file xuất (--result-file=) với dòng cuối như mariadb-dump thật.
func (x *m12) dump(c system.Cmd) system.Result {
	p := argPrefix(c.Args, "--result-file=")
	if p == "" {
		return exit(2, "", "mariadb-dump: thiếu --result-file\n")
	}
	body := "-- MariaDB dump 10.19  Distrib " + x.dbRunning() + ", for debian-linux-gnu (x86_64)\n--\n-- Host: localhost    Database: \n" +
		"CREATE DATABASE /*!32312 IF NOT EXISTS*/ `tp_demo`;\n-- Dump completed on " + time.Now().UTC().Format("2006-01-02 15:04:05") + "\n"
	x.SetFile(p, []byte(body), 0o600, rootOwner)
	return okResult
}

// tailLast trả dòng cuối của một file (`tail -n 1 -- <file>` của MariaDBDump).
// Log của site không phải file trên máy giả: để logs.go trả lời.
func (x *m12) tailLast(c system.Cmd) system.Result {
	if strings.HasPrefix(c.Args[len(c.Args)-1], "/var/log/tpan/sites/") {
		return x.tail(c)
	}
	data, err := x.ReadFile(c.Args[len(c.Args)-1])
	if err != nil {
		return exit(1, "", "tail: cannot open '"+c.Args[len(c.Args)-1]+"' for reading: No such file or directory\n")
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return exit(0, lines[len(lines)-1]+"\n", "")
}
