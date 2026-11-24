package templates

// M14 (mục 6.13 của docs/KE-HOACH.md), hai file cấp máy cho MỖI bản PHP đang
// cài: file ini của OPcache và drop-in systemd của dịch vụ PHP-FPM. Cả hai là
// nội dung cố định, không có tham số, nên là hằng trong mã thay vì file
// template: không có gì để render sai.
//
// Phần còn lại của M14 trong gói này (vhost nginx thêm disable_symlinks, vhost
// Apache thêm hai dòng ProxyFCGISetEnvIf và khối AllowOverride theo chế độ
// .htaccess, logrotate của nginx gửi HUP) nằm ngay trong các template cũ; hợp
// đồng của chúng ở m14_types.go.

// NginxSymlinkGuard là dòng chặn symlink của vhost nginx (không kèm thụt lề):
// nginx chỉ đi theo symlink khi chủ của symlink cũng là chủ của file đích (lỗ
// L5). Template dùng đúng chuỗi này; checks.VhostHardened tìm đúng dòng này
// trong các dòng không phải chú thích của vhost.
//
// Từ M28 đợt 2 template KHÔNG còn ghi dòng này (xem NginxSymlinkGuardV2). Hằng
// được giữ để checks.VhostHardened vẫn nhận vhost ghi bởi bản cũ: Plan không
// ghi lại vhost (khôi phục backup...) không bị hoàn tác trên máy vừa nâng cấp
// mà chưa chạy `tpan security harden`.
const NginxSymlinkGuard = "disable_symlinks if_not_owner from=$document_root;"

// NginxSymlinkGuardV2 là dòng chặn symlink từ M28 đợt 2 (lỗ F4): bỏ from=, nên
// nginx xét MỌI thành phần của đường dẫn, kể cả chính thư mục public. Với
// from=$document_root nginx không xét các thành phần từ document root trở lên,
// nên site A đổi public thành symlink sang thư mục của site B là nginx trả file
// của B dưới tên miền của A. /var, /var/www và neo thuộc root (không phải
// symlink) nên không bị ảnh hưởng; symlink của site trỏ vào thứ của chính site
// vẫn chạy. checks.DocrootGuard đòi đúng dòng này.
const NginxSymlinkGuardV2 = "disable_symlinks if_not_owner;"

// ApacheFCGIGuards là hai dòng gỡ biến của vhost Apache (không kèm thụt lề):
// PHP-FPM nhận PHP_VALUE và PHP_ADMIN_VALUE từ web server và cho chúng đè cả
// php_admin_value của pool, nên .htaccess đặt được open_basedir=/ (lỗ L3).
var ApacheFCGIGuards = []string{
	`ProxyFCGISetEnvIf "true" !PHP_VALUE`,
	`ProxyFCGISetEnvIf "true" !PHP_ADMIN_VALUE`,
}

// OPcacheAPIDir là giá trị của opcache.restrict_api: chỉ script nằm dưới đường
// dẫn này mới gọi được opcache_get_status(), opcache_reset()... Đường dẫn cố ý
// KHÔNG tồn tại và không site nào nằm dưới nó, nên không site nào gọi được.
// tpan không tạo thư mục này.
const OPcacheAPIDir = "/usr/local/lib/tpan/opcache-api/"

// PHPHardeningINIPath là file ini tpan ghi cho PHP-FPM bản version ("8.3"):
// /etc/php/<version>/fpm/conf.d/99-tpan.ini. Tên 99- để được đọc sau
// 10-opcache.ini của gói và thắng. version phải được người gọi kiểm trước
// (steps và checks đều có phpVersion).
func PHPHardeningINIPath(version string) string {
	return "/etc/php/" + version + "/fpm/conf.d/99-tpan.ini"
}

// PHPHardeningINI là nội dung của file trên. File theo cú pháp INI của PHP nên
// dòng đầu là "; " + Header, như file pool (sitePoolComment).
//
//   - opcache.validate_permission=1: trước khi trả một file đã cache, OPcache
//     kiểm user đang chạy có quyền đọc file đó không. Thiếu dòng này, các pool
//     dùng chung một bộ nhớ cache (một tiến trình chủ PHP-FPM) nên PHP của
//     site A include được wp-config.php của site B khi file đang nằm trong
//     cache, dù open_basedir và quyền file đều cấm (lỗ L1).
//   - opcache.restrict_api: khoá các hàm opcache_* (lỗ L2: liệt kê đường dẫn
//     file của site khác, xoá cache của mọi site).
func PHPHardeningINI() []byte {
	return []byte(sitePoolComment + `
;
; Cô lập giữa các site trên cùng một bản PHP-FPM (docs/KE-HOACH.md mục 6.13).
; Mọi pool của một bản PHP dùng chung một bộ nhớ OPcache.

; Trước khi dùng một file đã cache, kiểm user của pool có quyền đọc file đó.
; Thiếu dòng này, PHP của site này include được file PHP đã cache của site khác.
opcache.validate_permission=1

; Chỉ script nằm dưới đường dẫn này mới gọi được các hàm opcache_*. Đường dẫn
; không tồn tại, nên không site nào xem được danh sách file hay xoá cache chung.
opcache.restrict_api=` + OPcacheAPIDir + "\n")
}

// PHPDropInDir và PHPDropInPath là thư mục và file drop-in systemd của dịch vụ
// php<version>-fpm: /etc/systemd/system/php<version>-fpm.service.d/tpan.conf.
func PHPDropInDir(version string) string {
	return "/etc/systemd/system/php" + version + "-fpm.service.d"
}

func PHPDropInPath(version string) string { return PHPDropInDir(version) + "/tpan.conf" }

// PHPDropIn là nội dung drop-in. ProtectProc=invisible gắn cho riêng dịch vụ
// này một /proc mà tiến trình chỉ thấy tiến trình của chính user mình: PHP của
// một site (và lệnh shell nó gọi) không còn thấy tiến trình và dòng lệnh của
// site khác (lỗ L7). Không gắn lại /proc của cả máy. Đổi giá trị này cần
// daemon-reload rồi RESTART dịch vụ (không gian mount được dựng lúc khởi động).
func PHPDropIn() []byte {
	return []byte(Header + `
#
# Cô lập giữa các site (docs/KE-HOACH.md mục 6.13): tiến trình PHP của một site
# không thấy tiến trình của user khác trong /proc.
[Service]
ProtectProc=invisible
`)
}
