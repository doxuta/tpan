package templates

// Hợp đồng M14 (đợt 0) cho các template cũ, theo mục 6.13 của
// docs/KE-HOACH.md. File này chỉ có chú thích. Chữ ký đã đổi ở đợt 0 (tham số
// mode đã có và đang bị bỏ qua); thợ đợt 1 sửa file template, thân hàm, file
// golden và test.
//
// 1. Vhost nginx của site (site-nginx.conf.tmpl, và khối server cổng 443 của
//    site-nginx-ssl.conf.tmpl): thêm ở cấp server, ngay sau dòng root:
//
//	disable_symlinks if_not_owner from=$document_root;
//
//    (M28 đợt 2: dòng này đổi thành NginxSymlinkGuardV2, không có from=, và
//    vhost Apache có thêm khối <Directory "<neo>">; xem m14.go và lỗ F4.)
//    kèm chú thích nói lý do (nginx chạy dưới www-data đọc được mọi site qua
//    nhóm; không có dòng này thì symlink trong site A trỏ sang file 640 của
//    site B được trả về, kể cả mã nguồn PHP dưới tên .txt: lỗ L5). Khối server
//    cổng 80 của bản SSL chỉ chuyển hướng, không có root: không thêm. Hai bản
//    tạm khoá của nginx không có root ở cấp server: không đổi. Site mặc định:
//    không đổi.
//
// 2. Vhost Apache của site (site-apache.conf.tmpl, và <VirtualHost *:443> của
//    site-apache-ssl.conf.tmpl): thêm ở cấp VirtualHost
//
//	ProxyFCGISetEnvIf "true" !PHP_VALUE
//	ProxyFCGISetEnvIf "true" !PHP_ADMIN_VALUE
//
//    (gỡ hai biến mà PHP-FPM nhận từ web server và cho đè cả php_admin_value
//    của pool: lỗ L3). Khối <Directory "<public>"> thay dòng AllowOverride All
//    theo mode (mode rỗng hiểu là site.HtaccessSafe, dùng mode.OrDefault()):
//
//	site.HtaccessSafe  AllowOverride <htaccess.OverrideOptions()>
//	                   AllowOverrideList <htaccess.List()>   (một dòng duy nhất)
//	site.HtaccessOff   AllowOverride None
//	                   FallbackResource /index.php
//	site.HtaccessFull  AllowOverride All
//
//    Bản an-toan KHÔNG có "Options" trong AllowOverrideList: chỉ thị Options đi
//    qua "AllowOverride Options=..." với đúng ba tuỳ chọn, để .htaccess không
//    viết được "Options FollowSymLinks" (đo trên máy ảo, t9 trong
//    docs/bang-chung/M14-nghien-cuu/). Chú thích trong template nói rõ điều này.
//    mode khác ba giá trị trên: lỗi ErrParam. Khối wp-content/uploads giữ
//    AllowOverride None như cũ. Bản tạm khoá của Apache không trỏ vào public:
//    không đổi. <VirtualHost *:80> của bản SSL chỉ chuyển hướng: không đổi.
//
//	func ApacheSite(l site.Layout, php, acmeRoot string, mode site.HtaccessMode) ([]byte, error)
//	func ApacheSiteSSL(l site.Layout, php, acmeRoot string, mode site.HtaccessMode) ([]byte, error)
//	func SiteVhost(l site.Layout, web, php, acmeRoot string, ssl bool, mode site.HtaccessMode) ([]byte, error)
//
//    Golden: site-apache.golden và site-apache-ssl.golden là bản an-toan; thêm
//    site-apache-tat.golden, site-apache-day-du.golden (và hai bản -ssl tương
//    ứng). Test: dòng AllowOverrideList có đúng len(htaccess.Allowed) tên.
//
// 3. Hằng dấu nhận biết NginxSymlinkGuard và ApacheFCGIGuards (đã khai ở
//    m14.go): phép kiểm checks.VhostHardened tìm đúng các dòng này (bỏ dòng chú thích).
//    Test của template khẳng định vhost sinh ra chứa đúng các chuỗi đó, để
//    template và phép kiểm không lệch nhau.
//
// 4. Logrotate (logrotate-sites.tmpl), nhánh nginx: postrotate gửi HUP thay cho
//    USR1:
//
//	[ ! -f /run/nginx.pid ] || kill -HUP "$(cat /run/nginx.pid)"
//
//    Lý do (đo trên máy ảo, docs/bang-chung/M14-nghien-cuu/t8-log-usr1-hay-hup.ket-qua.txt):
//    từ M14 thư mục log của site là root:<nhóm của site> 0750. Nhận USR1, từng
//    worker nginx (www-data) tự mở lại file log theo đường dẫn và bị từ chối ở
//    thư mục, nên log mới vẫn rơi vào access.log.1. Nhận HUP, tiến trình chính
//    (root) mở file rồi sinh worker mới thừa kế file đã mở; worker cũ làm nốt
//    request đang chạy rồi thoát, không ngắt kết nối nào. Sửa cả đoạn chú thích
//    đầu file (thư mục log không còn là root:root 0755). Nhánh Apache không đổi
//    (reload êm: tiến trình cha chạy bằng root mở lại log).
