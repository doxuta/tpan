package steps

// Hợp đồng M14 (đợt 0): các Step của cô lập sâu (mục 6.13 của
// docs/KE-HOACH.md). File này chỉ có chú thích. Thợ đợt 1 viết file mới
// harden_m14.go (+ harden_m14_test.go) và sửa SiteDirs trong site_user.go.
// Luật chung của gói (common.go) vẫn áp dụng: tham số sai thì Check và Apply
// lỗi trước khi chạy lệnh; Apply đã đổi máy mà bước sau lỗi thì tự trả máy về.
//
// ------------------------------------------------------------- harden_m14.go
//
//	// PHPHardeningINI ghi templates.PHPHardeningINIPath(v) (0644 root) với nội
//	// dung templates.PHPHardeningINI(): opcache.validate_permission và
//	// opcache.restrict_api cho PHP-FPM bản v (lỗ L1, L2). Sau khi ghi: kiểm
//	// `php-fpm<v> -t` (sai thì trả file cũ về), rồi reload php<v>-fpm NẾU dịch
//	// vụ đang chạy (phpReloadIfActive: lúc cài stack gói vừa cài có thể chưa
//	// chạy). reload là đủ để PHP-FPM nhận ini mới (đo ở t7).
//	//
//	// Version là con trỏ như PHPIdlePool: Plan cài stack dựng trước khi
//	// DetectPHP biết bản PHP. nil, rỗng hoặc sai dạng lúc chạy thì lỗi.
//	// Info: khi đã biết bản thì khai Files (để file vào sổ managed_files và
//	// bản xem trước vẽ được diff) và Commands; chưa biết thì tiêu đề chung.
//	// Check: file có đúng nội dung, quyền và chủ. Undo: trả file về như trước
//	// (xoá nếu trước đó chưa có), reload nếu đang chạy.
//	type PHPHardeningINI struct{ Version *string }
//
//	// PHPProtectProc ghi drop-in templates.PHPDropInPath(v) (0644 root, thư mục
//	// templates.PHPDropInDir(v) 0755 root) với nội dung templates.PHPDropIn():
//	// ProtectProc=invisible cho dịch vụ php<v>-fpm (lỗ L7). Sau khi ghi:
//	// `systemctl daemon-reload`, rồi `systemctl restart php<v>-fpm` NẾU dịch vụ
//	// đang chạy (đổi không gian mount cần restart, reload không đủ). Danger:
//	// restart ngắt request PHP đang chạy của mọi site dùng bản này trong
//	// khoảng một giây; Why nói rõ điều đó.
//	//
//	// Check: file đúng nội dung VÀ, khi dịch vụ đang chạy,
//	// `systemctl show php<v>-fpm -p ProtectProc --value` in "invisible" (file
//	// đã đúng mà dịch vụ chưa restart thì chưa xong). Dịch vụ không chạy thì
//	// chỉ xét file. Undo: trả file về như trước (xoá file và thư mục nếu chính
//	// bước này tạo), daemon-reload, restart nếu đang chạy.
//	// Version là con trỏ, luật như PHPHardeningINI.
//	type PHPProtectProc struct{ Version *string }
//
//	// PHPHardening trả hai bước trên theo đúng thứ tự (ini rồi drop-in) cho bản
//	// version đã biết. Dùng trong Plan thêm một bản PHP và Plan "Áp bản vá cô
//	// lập". version sai dạng thì lỗi.
//	func PHPHardening(version string) ([]engine.Step, error)
//
// -------------------------------------------------- site_user.go (sửa SiteDirs)
//
//	// SiteLogs trả các bước quyền log của site (lỗ L6), đúng thứ tự:
//	//   Dir       l.LogDir       root:<l.User>   0750
//	//   OwnedFile l.AccessLog    root:<l.User>   0640
//	//   OwnedFile l.ErrorLog     root:<l.User>   0640
//	//   OwnedFile l.PHPErrorLog  <l.User>:<l.User> 0640   (như cũ)
//	// Nhóm là nhóm riêng của user site (useradd --user-group tạo cùng tên), nên
//	// site đọc được log của chính nó, site khác thì không; chủ vẫn là root để
//	// logrotate không đổi tên file trong thư mục do user của site kiểm soát.
//	// access.log và error.log được tạo sẵn (rỗng) trước khi vhost được ghi:
//	// web server mở file có sẵn và giữ nguyên quyền; để nó tự tạo thì file ra
//	// 0644. OwnedFile giữ nguyên nội dung file đã có, chỉ sửa quyền và chủ.
//	// SiteDirs gọi SiteLogs thay cho hai dòng log cũ; Plan "Áp bản vá cô lập"
//	// gọi thẳng SiteLogs cho từng site đã có.
//	func SiteLogs(l site.Layout) []engine.Step
//
// Chữ ký đã đổi ở đợt 0 (tham số mode đã được truyền xuống templates):
//
//	func SiteVhost(l site.Layout, web, phpVersion, acmeRoot string, mode site.HtaccessMode) ([]engine.Step, error)
//	func SiteVhostSSL(l site.Layout, web, phpVersion, acmeRoot string, mode site.HtaccessMode) ([]engine.Step, error)
