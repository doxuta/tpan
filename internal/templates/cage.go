package templates

// Template của lồng theo site (M17, mục 3.3 của docs/thay-doi/M17-thiet-ke.md).
// Hợp đồng đợt 0: chữ ký, tài liệu và danh sách dòng cố định; thân hàm và file
// template (files/cage-*.tmpl) do thợ đợt 1 viết, kèm golden test.
//
// Luật chung của gói giữ nguyên: không chạm máy; Layout phải đúng là
// site.LayoutOf(l.Domain) (siteDataOf đã kiểm điều đó); bản PHP qua phpVersion;
// mọi số qua kiểm khoảng; lỗi bọc ErrParam. Dòng đầu của mỗi file là Header
// theo đúng dấu chú thích của loại file: "# " cho unit systemd (Header đã bắt
// đầu bằng "#"), sitePoolComment ("; " + Header) cho php-fpm.conf.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
)

// CagePHPVersions là các bản PHP mà hàng rào che thư mục pool.d (mục 2.3: chỉ
// che bản đang dùng thì site vẫn đọc được tên pool của site khác ở bản kia).
// Đường dẫn có dấu "-" ở đầu nên bản chưa cài không làm hỏng gì.
var CagePHPVersions = []string{
	"5.6", "7.0", "7.1", "7.2", "7.3", "7.4",
	"8.0", "8.1", "8.2", "8.3", "8.4", "8.5", "8.6", "8.7", "8.8", "8.9",
	"9.0", "9.1", "9.2", "9.3", "9.4", "9.5", "9.6", "9.7", "9.8", "9.9",
}

// CageMySQLDirCmd là lệnh tạo thư mục socket của MariaDB, đúng như unit của gói
// MariaDB tự chạy. Unit socket của lồng chạy nó trước khi nghe (mục 2.8: lúc
// máy vừa lên, lồng có thể được đánh thức trước MariaDB); sửa tự động
// cage.restart cũng chạy đúng lệnh này. Tham số cố định, không lấy từ đâu.
var CageMySQLDirCmd = []string{"/usr/bin/install", "-d", "-m", "0755", "-o", "mysql", "-g", "root", "/run/mysqld"}

// cageMaxWorkers là trần trên của pm.max_children (mục 3.1: 1 đến 200).
const cageMaxWorkers = 200

// cageAnyPHP là một bản PHP hợp lệ bất kỳ, chỉ để đi qua siteDataOf (cửa kiểm
// Layout chung của gói). Socket theo bản PHP mà nó sinh ra luôn bị thay bằng
// socket của lồng, nên giá trị này không lọt vào file nào.
const cageAnyPHP = "8.3"

// cageSlug: slug chỉ gồm [a-z0-9_] (validate.SlugOf). Kiểm lại ở đây vì slug
// đi thẳng vào tên unit và tên slice: một dấu "-" lọt vào là systemd lồng
// slice thành nhiều tầng (t6), một ký tự lạ là phá cú pháp file unit.
var cageSlug = regexp.MustCompile(`^[a-z0-9_]+$`)

// cageData là dữ liệu đưa vào năm template của lồng.
type cageData struct {
	Header      string
	Domain      string
	Slug        string
	User        string
	Pool        string
	Dir         string
	Tmp         string
	LogDir      string
	PHPErrorLog string
	ConfDir     string
	Passwd      string
	Group       string
	FPMConf     string
	Slice       string
	Socket      string
	MySQLDirCmd string
	PoolDirs    string
	PHP         string
	Workers     int
	Lim         account.Limits
}

// cageDataOf kiểm Layout bằng đúng cửa kiểm của site (siteDataOf: Layout phải
// là LayoutOf(Domain), tên và đường dẫn qua danh sách ký tự cho phép), rồi kiểm
// thêm slug và các đường dẫn riêng của lồng, theo cùng luật.
func cageDataOf(l site.Layout) (cageData, error) {
	sd, err := siteDataOf(l, cageAnyPHP)
	if err != nil {
		return cageData{}, err
	}
	if !cageSlug.MatchString(l.Slug.String()) {
		return cageData{}, fmt.Errorf("%w: slug %q có ký tự không cho phép", ErrParam, l.Slug)
	}
	for _, p := range []string{l.CageConfDir(), l.CagePasswd(), l.CageGroup(), l.CageFPMConf(), l.CageSocket()} {
		if err := siteCheckPath(p); err != nil {
			return cageData{}, err
		}
	}
	// CagePHPVersions là biến xuất ra ngoài, nên vẫn kiểm từng bản trước khi
	// ghép vào hàng rào, như mọi chuỗi khác đi vào file config.
	dirs := make([]string, 0, len(CagePHPVersions))
	for _, v := range CagePHPVersions {
		if !phpVersion.MatchString(v) {
			return cageData{}, fmt.Errorf("%w: CagePHPVersions có bản %q không hợp lệ", ErrParam, v)
		}
		dirs = append(dirs, "-/etc/php/"+v+"/fpm/pool.d")
	}
	return cageData{
		Header:      Header,
		Domain:      sd.Domain,
		Slug:        l.Slug.String(),
		User:        sd.User,
		Pool:        sd.Pool,
		Dir:         sd.Dir,
		Tmp:         sd.Tmp,
		LogDir:      sd.LogDir,
		PHPErrorLog: sd.PHPErrorLog,
		ConfDir:     l.CageConfDir(),
		Passwd:      l.CagePasswd(),
		Group:       l.CageGroup(),
		FPMConf:     l.CageFPMConf(),
		Slice:       l.CageSlice(),
		Socket:      l.CageSocket(),
		MySQLDirCmd: strings.Join(CageMySQLDirCmd, " "),
		PoolDirs:    strings.Join(dirs, " "),
	}, nil
}

func cageRender(name string, d cageData) ([]byte, error) {
	return sslRender(name, d)
}

// CageSlice sinh file slice tpan-<slug>.slice: bốn dòng Accounting=yes và các
// dòng trần của lim (trần bằng 0 thì không có dòng đó):
//
//	CPUQuota=<CPUPercent>%
//	MemoryMax=<RAMMB>M và MemorySwapMax=0
//	TasksMax=<Tasks>
//	IOReadBandwidthMax=/var/www <IOReadMB>M
//	IOWriteBandwidthMax=/var/www <IOWriteMB>M
//
// Không có MemoryHigh (mục 2.4: trần mềm làm cả site treo). Số âm là ErrParam.
func CageSlice(l site.Layout, lim account.Limits) ([]byte, error) {
	d, err := cageDataOf(l)
	if err != nil {
		return nil, fmt.Errorf("sinh slice của lồng: %w", err)
	}
	// Chỉ chặn số âm: khoảng hợp lệ của từng trường (5..6400, 64.. ...) do core
	// kiểm; lặp lại ở đây thì hai nơi dễ lệch nhau. Số âm thì không bao giờ đúng
	// và sẽ sinh một dòng systemd đọc sai ("MemoryMax=-1M").
	for _, f := range []struct {
		name string
		v    int
	}{
		{"CPUPercent", lim.CPUPercent}, {"RAMMB", lim.RAMMB}, {"Tasks", lim.Tasks},
		{"IOReadMB", lim.IOReadMB}, {"IOWriteMB", lim.IOWriteMB}, {"PHPWorkers", lim.PHPWorkers},
	} {
		if f.v < 0 {
			return nil, fmt.Errorf("sinh slice của lồng %s: %w: trần %s = %d là số âm", d.Domain, ErrParam, f.name, f.v)
		}
	}
	d.Lim = lim
	return cageRender("cage-slice.tmpl", d)
}

// CageSocketUnit sinh tpan-cage-<slug>-php.socket đúng như mục 3.3: dòng
// ExecStartPre=- với CageMySQLDirCmd, ListenStream=l.CageSocket(),
// SocketUser/SocketGroup www-data, SocketMode=0660, DirectoryMode=0711,
// RemoveOnStop=yes, [Install] WantedBy=sockets.target.
func CageSocketUnit(l site.Layout) ([]byte, error) {
	d, err := cageDataOf(l)
	if err != nil {
		return nil, fmt.Errorf("sinh unit socket của lồng: %w", err)
	}
	return cageRender("cage-socket.tmpl", d)
}

// CageServiceUnit sinh tpan-cage-<slug>-php.service đúng như mục 3.3: [Unit]
// có StartLimitIntervalSec=30, StartLimitBurst=10, RequiresMountsFor=l.Dir;
// [Service] có Type=notify, User, Group, Slice=l.CageSlice(), NoNewPrivileges
// (lặp lại dù hàng rào cũng có: mất drop-in thì dịch vụ vẫn chạy bằng user
// của site, trong slice có trần), Environment=FPM_SOCKETS=<l.CageSocket()>=3,
// ExecStart=/usr/sbin/php-fpm<php> --nodaemonize --fpm-config <l.CageFPMConf()>,
// ExecReload, KillSignal=SIGQUIT, TimeoutStopSec=30, OOMPolicy=continue,
// Restart=on-failure, RestartSec=5s (t15). Không có [Install]: dịch vụ chỉ
// chạy khi socket đánh thức.
func CageServiceUnit(l site.Layout, php string) ([]byte, error) {
	d, err := cageDataOf(l)
	if err == nil && !phpVersion.MatchString(php) {
		err = fmt.Errorf("%w: bản PHP %q không hợp lệ (dạng 8.3)", ErrParam, php)
	}
	if err != nil {
		return nil, fmt.Errorf("sinh unit dịch vụ của lồng: %w", err)
	}
	d.PHP = php
	return cageRender("cage-service.tmpl", d)
}

// CageFence sinh drop-in hàng rào fence.conf: [Service] rồi đúng danh sách của
// mục 2.3 (bảng "Hàng rào đã chốt"), theo thứ tự và giá trị của hàm hang_rao
// trong docs/bang-chung/M17-nghien-cuu/lib.sh, với ba chỗ khác:
// không có PrivateUsers; có SystemCallFilter=~@privileged @resources; các lệnh
// gắn /run/mysqld (cả hai) và /run/systemd/resolve có dấu "-"; mỗi bản trong
// CagePHPVersions một mục -/etc/php/<bản>/fpm/pool.d trong InaccessiblePaths.
func CageFence(l site.Layout) ([]byte, error) {
	d, err := cageDataOf(l)
	if err != nil {
		return nil, fmt.Errorf("sinh hàng rào của lồng: %w", err)
	}
	return cageRender("cage-fence.tmpl", d)
}

// CageFPMConf sinh /etc/tpan/cage/<slug>/php-fpm.conf đúng như mục 3.3
// ([global]: error_log = syslog, syslog.ident = php-fpm-<slug>, daemonize = no,
// process_control_timeout = 25s; pool [tpan-<slug>] với listen = l.CageSocket()
// và đúng các dòng php_admin_* của pool ngoài lồng, files/site-pool.conf.tmpl).
// workers là pm.max_children, 1..200 (account.Limits.Workers() đã đổi 0 thành 5).
func CageFPMConf(l site.Layout, workers int) ([]byte, error) {
	d, err := cageDataOf(l)
	if err == nil && (workers < 1 || workers > cageMaxWorkers) {
		err = fmt.Errorf("%w: số worker %d ngoài khoảng 1..%d", ErrParam, workers, cageMaxWorkers)
	}
	if err != nil {
		return nil, fmt.Errorf("sinh php-fpm.conf của lồng: %w", err)
	}
	d.Header = sitePoolComment
	d.Workers = workers
	return cageRender("cage-fpm.conf.tmpl", d)
}

// SiteVhostCaged sinh vhost của site trong lồng: giống hệt SiteVhost(l, web,
// <bất kỳ>, acmeRoot, ssl, mode) trừ socket là l.CageSocket() thay cho
// l.SocketFor(php). Không đổi template nào của vhost ngoài lồng: golden của
// chúng giữ nguyên từng byte (bất biến 1).
//
// Từ M19 đây là SiteVhostCagedNames với site.Names{}.
func SiteVhostCaged(l site.Layout, web, acmeRoot string, ssl bool, mode site.HtaccessMode) ([]byte, error) {
	return SiteVhostCagedNames(l, web, acmeRoot, ssl, mode, site.Names{})
}

// SiteVhostCagedNames như SiteVhostCaged cộng tên phụ (M19, như SiteVhostNames).
func SiteVhostCagedNames(l site.Layout, web, acmeRoot string, ssl bool, mode site.HtaccessMode, n site.Names) ([]byte, error) {
	out, err := siteVhostCaged(l, web, acmeRoot, ssl, mode, n)
	if err != nil {
		return nil, fmt.Errorf("sinh vhost trong lồng: %w", err)
	}
	return out, nil
}

// siteVhostCaged dựng dữ liệu bằng đúng cửa kiểm của vhost ngoài lồng
// (siteVhostData, sslSiteData), chỉ thay trường Socket, rồi render đúng tên
// template mà SiteVhost chọn. Bốn nhánh dưới phải khớp bốn nhánh của SiteVhost.
func siteVhostCaged(l site.Layout, web, acmeRoot string, ssl bool, mode site.HtaccessMode, n site.Names) ([]byte, error) {
	if web != "nginx" && web != "apache" {
		return nil, fmt.Errorf("%w: web %q phải là \"nginx\" hoặc \"apache\"", ErrParam, web)
	}
	if ssl {
		d, err := sslSiteData(l, cageAnyPHP, acmeRoot)
		if err == nil {
			d.siteData, err = cageVhostData(l, d.siteData, web, mode)
		}
		if err == nil {
			d.siteData, err = withNames(d.siteData, l, d.Cert, d.Key, true, n)
		}
		if err != nil {
			return nil, err
		}
		return sslRender("site-"+web+"-ssl.conf.tmpl", d)
	}
	d, err := siteVhostData(l, cageAnyPHP, acmeRoot)
	if err == nil {
		d, err = cageVhostData(l, d, web, mode)
	}
	if err == nil {
		d, err = withNames(d, l, "", "", false, n)
	}
	if err != nil {
		return nil, err
	}
	return siteRender("site-"+web+".conf.tmpl", d)
}

// cageVhostData thay socket bằng socket của lồng (qua cùng luật đường dẫn) và,
// với Apache, điền khối .htaccess như ApacheSite.
func cageVhostData(l site.Layout, d siteData, web string, mode site.HtaccessMode) (siteData, error) {
	if err := siteCheckPath(l.CageSocket()); err != nil {
		return siteData{}, err
	}
	d.Socket = l.CageSocket()
	if web == "apache" {
		return withHtaccess(d, mode)
	}
	return d, nil
}
