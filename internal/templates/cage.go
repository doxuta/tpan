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
	return nil, fmt.Errorf("sinh slice của lồng: %w: chưa viết (đợt 1)", ErrParam)
}

// CageSocketUnit sinh tpan-cage-<slug>-php.socket đúng như mục 3.3: dòng
// ExecStartPre=- với CageMySQLDirCmd, ListenStream=l.CageSocket(),
// SocketUser/SocketGroup www-data, SocketMode=0660, DirectoryMode=0755,
// RemoveOnStop=yes, [Install] WantedBy=sockets.target.
func CageSocketUnit(l site.Layout) ([]byte, error) {
	return nil, fmt.Errorf("sinh unit socket của lồng: %w: chưa viết (đợt 1)", ErrParam)
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
	return nil, fmt.Errorf("sinh unit dịch vụ của lồng: %w: chưa viết (đợt 1)", ErrParam)
}

// CageFence sinh drop-in hàng rào fence.conf: [Service] rồi đúng danh sách của
// mục 2.3 (bảng "Hàng rào đã chốt"), theo thứ tự và giá trị của hàm hang_rao
// trong docs/bang-chung/M17-nghien-cuu/lib.sh, với ba chỗ khác:
// không có PrivateUsers; có SystemCallFilter=~@privileged @resources; các lệnh
// gắn /run/mysqld (cả hai) và /run/systemd/resolve có dấu "-"; mỗi bản trong
// CagePHPVersions một mục -/etc/php/<bản>/fpm/pool.d trong InaccessiblePaths.
func CageFence(l site.Layout) ([]byte, error) {
	return nil, fmt.Errorf("sinh hàng rào của lồng: %w: chưa viết (đợt 1)", ErrParam)
}

// CageFPMConf sinh /etc/tpan/cage/<slug>/php-fpm.conf đúng như mục 3.3
// ([global]: error_log = syslog, syslog.ident = php-fpm-<slug>, daemonize = no,
// process_control_timeout = 25s; pool [tpan-<slug>] với listen = l.CageSocket()
// và đúng các dòng php_admin_* của pool ngoài lồng, files/site-pool.conf.tmpl).
// workers là pm.max_children, 1..200 (account.Limits.Workers() đã đổi 0 thành 5).
func CageFPMConf(l site.Layout, workers int) ([]byte, error) {
	return nil, fmt.Errorf("sinh php-fpm.conf của lồng: %w: chưa viết (đợt 1)", ErrParam)
}

// SiteVhostCaged sinh vhost của site trong lồng: giống hệt SiteVhost(l, web,
// <bất kỳ>, acmeRoot, ssl, mode) trừ socket là l.CageSocket() thay cho
// l.SocketFor(php). Không đổi template nào của vhost ngoài lồng: golden của
// chúng giữ nguyên từng byte (bất biến 1).
func SiteVhostCaged(l site.Layout, web, acmeRoot string, ssl bool, mode site.HtaccessMode) ([]byte, error) {
	return nil, fmt.Errorf("sinh vhost trong lồng: %w: chưa viết (đợt 1)", ErrParam)
}
