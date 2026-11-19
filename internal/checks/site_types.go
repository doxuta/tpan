package checks

// Hợp đồng M2: phép kiểm của một site, xếp theo đường đi của request (mục
// 6.2 bước 10 và mục 6.7 của docs/KE-HOACH.md). Viết ở internal/checks/site.go.
// Tên và đường dẫn lấy từ site.Layout; gói site là gói lá nên checks import
// được mà không tạo vòng.
//
// ID của Result có dạng "<loại>:<domain>" như dưới, ổn định giữa các lần chạy.
// Không đạt thì luôn có Cause và ManualFix (mục 6.7). Lệnh chạy dưới user của
// site dùng system.Cmd.AsUser.
//
//	// VhostEnabled (StageWeb, "web.vhost:<domain>"): file vhost của web
//	// ("nginx" | "apache") có, mang dòng templates.Header, và symlink ở
//	// sites-enabled trỏ đúng vào nó.
//	func VhostEnabled(web string, l site.Layout) Check
//
//	// PoolSocket (StagePHP, "php.socket:<domain>"): l.SocketFor(php) tồn tại, là
//	// socket, chủ www-data:www-data, quyền 0660.
//	func PoolSocket(l site.Layout, php string) Check
//
//	// WPChecksums (StageWordPress, "wp.checksums:<domain>"):
//	// wp core verify-checksums --path=l.Public, dưới l.User.
//	func WPChecksums(l site.Layout) Check
//
//	// WPConfigPerm (StageWordPress, "wp.config:<domain>"): l.WPConfig có
//	// quyền 0600, chủ l.User.
//	func WPConfigPerm(l site.Layout) Check
//
//	// DBAccess (StageDB, "db.access:<domain>"): dưới l.User, đăng nhập DB
//	// bằng thông tin trong wp-config.php (wp db query 'SHOW DATABASES'
//	// --skip-column-names --path=l.Public): thấy l.DB, và không thấy DB nào
//	// khác ngoài information_schema.
//	func DBAccess(l site.Layout) Check
//
//	// Isolated (StageWordPress, "isolation:<from domain>:<to domain>"): dưới
//	// from.User, `test -r to.WPConfig` phải lỗi (mã 1). Đọc được là Fail.
//	func Isolated(from, to site.Layout) Check
//
//	// HTTPSite (StageEndToEnd, "http.site:<domain>"): curl tới
//	// http://<domain>/ với --resolve <domain>:80:127.0.0.1 → mã 200, thân có
//	// dấu hiệu WordPress VÀ có chính domain của site (để một request rơi nhầm
//	// vào vhost khác không qua được).
//	func HTTPSite(l site.Layout) Check
//
//	// SiteChecks trả mọi phép kiểm của một site theo đúng thứ tự chặng:
//	// VhostEnabled, PoolSocket, WPChecksums, WPConfigPerm, DBAccess, HTTPSite.
//	// (Isolated cần hai site, core tự thêm. PHPFPMConfig là phép kiểm của cả
//	// máy, nằm ở phép kiểm stack: xem chú thích ở site.go.)
//	func SiteChecks(web, phpVersion string, l site.Layout) []Check
