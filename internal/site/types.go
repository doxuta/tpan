// Package site giữ hai thứ mọi gói khác cần để nói về một site WordPress:
//
//   - Layout: mọi tên và đường dẫn của site, suy ra từ domain theo đúng một
//     quy tắc (mục 5 của docs/KE-HOACH.md). steps, checks, templates, store,
//     core đều lấy tên từ đây, nên không có chỗ thứ hai tự ghép "tp_" + slug.
//   - Site: bản ghi của site trong bảng sites.
//
// Gói này là gói lá: chỉ import validate. Nhờ vậy checks (không được import
// core hay engine) và steps (bị core import) đều dùng được.
package site

import (
	"time"

	"github.com/doxuta/tpan/internal/validate"
)

// Layout là mọi tên và đường dẫn của một site. Chỉ dựng bằng LayoutOf từ một
// validate.Domain đã kiểm: tên user, DB, pool không bao giờ lấy từ chuỗi người
// dùng gõ (mục 11).
type Layout struct {
	Domain validate.Domain `json:"domain"`
	Slug   validate.Slug   `json:"slug"`

	// User là user Linux của site, tp_<slug>. useradd tạo kèm một nhóm cùng
	// tên; PHP của site chạy dưới user và nhóm này, không thuộc nhóm www-data.
	User string `json:"user"`
	// DB là tên DB và cũng là tên user DB (user@localhost), tp_<slug>.
	DB string `json:"db"`

	// Dir là thư mục site, /var/www/<domain>: chủ User, nhóm www-data, 2750.
	// Cũng là thư mục home của User (WP-CLI ghi cache vào ~/.wp-cli).
	Dir string `json:"dir"`
	// Public là document root, Dir/public: chủ User, nhóm www-data, 2750.
	Public string `json:"public"`
	// Tmp là upload tạm và session PHP của riêng site, Dir/tmp: chủ User, 0700.
	Tmp string `json:"tmp"`
	// WPConfig là Public/wp-config.php: chủ User, 0600.
	WPConfig string `json:"wp_config"`

	// LogDir là /var/log/tpan/sites/<domain>: root:root, 0755 (mục 5 giải
	// thích vì sao không thuộc user của site).
	LogDir string `json:"log_dir"`
	// PHPErrorLog là LogDir/php-error.log: chủ User, nhóm User, 0640, để PHP
	// chạy dưới User ghi được.
	PHPErrorLog string `json:"php_error_log"`

	// Pool là tên pool PHP-FPM, tpan-<slug>.
	Pool string `json:"pool"`
	// Socket là /run/php/tpan-<slug>.sock: www-data:www-data, 0660.
	Socket string `json:"socket"`

	// Vhost của từng web server và symlink bật nó.
	NginxConf  string `json:"nginx_conf"`
	NginxLink  string `json:"nginx_link"`
	ApacheConf string `json:"apache_conf"`
	ApacheLink string `json:"apache_link"`
}

// WebGroup là nhóm của web server trên Ubuntu (cả Nginx lẫn Apache). Thư mục
// site thuộc nhóm này để web server đọc được file tĩnh qua quyền nhóm.
const WebGroup = "www-data"

// LayoutOf suy mọi tên và đường dẫn từ domain. Domain zero cho Layout zero
// (mọi trường rỗng), để lỡ quên ParseDomain thì Step từ chối vì đường dẫn rỗng
// thay vì làm việc trên "/var/www/".
func LayoutOf(d validate.Domain) Layout {
	if d.IsZero() {
		return Layout{}
	}
	slug := validate.SlugOf(d)
	dom := d.String()
	dir := "/var/www/" + dom
	public := dir + "/public"
	logDir := "/var/log/tpan/sites/" + dom
	pool := "tpan-" + slug.String()
	return Layout{
		Domain:      d,
		Slug:        slug,
		User:        slug.LinuxUser(),
		DB:          slug.DBName(),
		Dir:         dir,
		Public:      public,
		Tmp:         dir + "/tmp",
		WPConfig:    public + "/wp-config.php",
		LogDir:      logDir,
		PHPErrorLog: logDir + "/php-error.log",
		Pool:        pool,
		Socket:      "/run/php/" + pool + ".sock",
		NginxConf:   "/etc/nginx/sites-available/tpan-" + dom + ".conf",
		NginxLink:   "/etc/nginx/sites-enabled/tpan-" + dom + ".conf",
		ApacheConf:  "/etc/apache2/sites-available/tpan-" + dom + ".conf",
		ApacheLink:  "/etc/apache2/sites-enabled/tpan-" + dom + ".conf",
	}
}

// PoolConf là file pool của site cho bản PHP version (ví dụ "8.3"):
// /etc/php/<version>/fpm/pool.d/tpan-<slug>.conf. Tách khỏi Layout vì bản PHP
// là của stack (và từ M12 là của từng site), không suy từ domain.
func (l Layout) PoolConf(version string) string {
	return "/etc/php/" + version + "/fpm/pool.d/" + l.Pool + ".conf"
}

// Status là trạng thái của site trong bảng sites.
type Status string

const (
	Active  Status = "active"
	Trashed Status = "trashed" // M6: đang ở thùng rác
)

// Site là một dòng của bảng sites (mục 4.4). Không có mật khẩu nào ở đây:
// mật khẩu DB nằm trong wp-config.php, mật khẩu quản trị chỉ hiện một lần.
type Site struct {
	Domain validate.Domain `json:"domain"`
	Slug   validate.Slug   `json:"slug"`
	Status Status          `json:"status"`
	// PHPVersion là bản PHP pool của site đang dùng, ví dụ "8.3".
	PHPVersion string `json:"php_version"`
	// SSL: site đã có chứng chỉ (M5).
	SSL       bool      `json:"ssl"`
	Created   time.Time `json:"created"`
	TrashedAt time.Time `json:"trashed_at,omitzero"`
	// PurgeAt là hạn xoá hẳn khi site ở thùng rác (M6); zero khi không ở đó.
	PurgeAt time.Time `json:"purge_at,omitzero"`
}

// Layout trả tên và đường dẫn của site.
func (s Site) Layout() Layout { return LayoutOf(s.Domain) }
