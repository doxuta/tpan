package checks

// Hợp đồng M14 (đợt 0): phép kiểm của cô lập sâu và định tuyến (mục 6.13 của
// docs/KE-HOACH.md; lỗ L1 đến L8 ở docs/thay-doi/M14-nghien-cuu.md). File này
// chỉ có chú thích. Thợ đợt 1 sửa Isolated trong site.go và viết m14.go,
// m14_test.go theo khuôn của các phép kiểm đã có (base, Evidence, ManualFix,
// siteGuard, execCmd). Phép kiểm chỉ đọc; lệnh chạy dưới user của site dùng
// Cmd.AsUser. Mọi tên và đường dẫn lấy từ site.Layout và templates.
//
// Tên cách sửa tự động (Result.AutoFix) do core.AutoFixName điền theo ID, không
// điền ở đây.
//
// ------------------------------------------------------ site.go: Isolated (sửa)
//
//	// Isolated kiểm cô lập từ site from sang site to. Chữ ký và ID
//	// ("isolation:<from>:<to>", StageWordPress) không đổi. Từ M14 có năm phép
//	// thử, mỗi phép một dòng bằng chứng, đều chạy dưới from.User:
//	//   1. test -r <to.WPConfig>          (như cũ)
//	//   2. test -x <to.Dir>               (không vào được thư mục site)
//	//   3. test -w <to.Public>            (không ghi được vào public)
//	//   4. test -r <to.AccessLog>         (không đọc được log, lỗ L6)
//	//   5. test -w <socket>               cho MỖI socket của pool to:
//	//      các mục "<to.Pool>-<bản>.sock" trong /run/php (đọc bằng sys.ReadDir;
//	//      site tạm khoá không có socket nào thì bỏ phép này)
//	// Luật cũ giữ nguyên cho từng phép: "bị từ chối" là mã 1 VÀ stderr rỗng;
//	// đích phải tồn tại (stat bằng quyền của tpan) thì phép thử mới có nghĩa:
//	// đích của phép 1 không có thì cả phép kiểm Unknown như cũ; đích của phép
//	// 2 đến 5 không có thì bỏ riêng phép đó và ghi một dòng bằng chứng nói
//	// vì sao bỏ.
//	// Kết quả: một phép trả mã 0 là Fail (Summary nêu đúng thứ chạm được;
//	// ManualFix theo từng thứ: chmod 2750 thư mục, chmod 600 wp-config.php,
//	// `sudo tpan health --fix` cho log, xem listen.owner/listen.mode của pool
//	// cho socket). Một phép trả mã khác hoặc có stderr là Unknown. Còn lại Pass.
//	// Title đổi thành "Site <from> không chạm được vào file, log và socket của
//	// site <to>".
//	func Isolated(from, to site.Layout) Check
//
// -------------------------------------------------------------------- m14.go
//
//	// OPcacheHardened (StagePHP, ID "php.opcache:<version>"): OPcache của
//	// PHP-FPM bản version đã được siết (lỗ L1, L2). Hai bằng chứng:
//	//   1. file templates.PHPHardeningINIPath(version) có và nội dung bằng
//	//      templates.PHPHardeningINI();
//	//   2. `php-fpm<version> -i` có dòng "opcache.validate_permission => On"
//	//      và dòng "opcache.restrict_api => <templates.OPcacheAPIDir>".
//	// Một trong hai sai: Fail. Cause nói rõ hậu quả (PHP của một site include
//	// được file PHP đã cache của site khác, ví dụ wp-config.php; gọi được
//	// opcache_get_status, opcache_reset). ManualFix: `sudo tpan security
//	// harden`, rồi lệnh kiểm lại. Không chạy được php-fpm<version>: Unknown.
//	// version phải khớp phpVersionRE.
//	func OPcacheHardened(version string) Check
//
//	// PHPProtectProc (StagePHP, ID "php.protectproc:<version>"): dịch vụ
//	// php<version>-fpm chạy với ProtectProc=invisible (lỗ L7). Chạy
//	// `systemctl show php<version>-fpm -p ProtectProc --value`: "invisible" là
//	// Pass, giá trị khác là Warn (không phải Fail: quyền file vẫn giữ, chỉ lộ
//	// danh sách tiến trình và dòng lệnh). ManualFix: `sudo tpan security
//	// harden`, lệnh kiểm lại. Lệnh không chạy được: Unknown.
//	func PHPProtectProc(version string) Check
//
//	// LogPerms (StageWeb, ID "log.perm:<domain>"): thư mục và file log của
//	// site không đọc được bởi site khác (lỗ L6). Đạt khi l.LogDir là thư mục
//	// root:<l.User> quyền 0750, và l.AccessLog, l.ErrorLog là file thường
//	// root:<l.User> quyền 0640. Thiếu file, sai chủ, nhóm hay quyền: Fail,
//	// Summary liệt kê từng chỗ sai. ManualFix: chgrp, chmod đúng từng thứ rồi
//	// `ls -ld`. Đọc bằng sys.Stat (bằng chứng dạng "stat <path> (tpan)").
//	func LogPerms(l site.Layout) Check
//
//	// VhostHardened (StageWeb, ID "web.hardened:<domain>"): vhost của site mang
//	// các dòng chặn của M14. Phép kiểm lệch (Drift) so file với bản tpan đã
//	// ghi, nên một vhost ghi bởi bản tpan cũ vẫn "không lệch"; phép này mới
//	// nói được vhost thiếu dòng chặn. Đọc l.NginxConf hoặc l.ApacheConf, chỉ
//	// xét dòng không phải chú thích:
//	//   - nginx: có templates.NginxSymlinkGuard (lỗ L5);
//	//   - apache: có đủ templates.ApacheFCGIGuards (lỗ L3), và khối AllowOverride
//	//     khớp mode (mode.OrDefault()): an-toan thì có dòng "AllowOverride " +
//	//     htaccess.OverrideOptions() và dòng "AllowOverrideList " + htaccess.List(),
//	//     không có "AllowOverride All"; tat thì không có "AllowOverride All",
//	//     không có AllowOverrideList, có "FallbackResource /index.php"; day-du
//	//     thì có "AllowOverride All".
//	// Thiếu: Fail, Summary nêu dòng thiếu, Cause nêu lỗ còn mở. ManualFix:
//	// `sudo tpan security harden`, rồi lệnh grep kiểm lại. File không có:
//	// Unknown (web.vhost đã báo). web lạ: Unknown.
//	func VhostHardened(web string, l site.Layout, mode site.HtaccessMode) Check
//
//	// ScanHtaccess tìm mọi .htaccess mà Apache sẽ đọc dưới l.Public và quét
//	// từng file bằng htaccess.Scan. Mọi lệnh chạy dưới l.User (không đọc bằng
//	// root: user của site có thể đặt symlink):
//	//   find <l.Public> -name .htaccess ( -type f -o -type l ) -print0
//	// bỏ mọi đường dẫn dưới <l.Public>/wp-content/uploads/ (vhost đặt
//	// AllowOverride None cho thư mục đó), rồi với từng file (tối đa 200 file,
//	// mỗi file tối đa 256 KiB): `head -c 262144 -- <file>`. Đường dẫn find trả
//	// về chỉ đi vào argv, không bao giờ vào chuỗi lệnh; đường dẫn không nằm
//	// dưới l.Public thì bỏ. File không đọc được dưới user của site cho một
//	// Finding có Directive "(không đọc được)". ev là bằng chứng của lệnh find
//	// và một dòng tóm tắt số file đã quét. err khác nil khi không chạy được
//	// find (không phải khi có Finding).
//	func ScanHtaccess(ctx context.Context, sys system.System, l site.Layout) (found []htaccess.Finding, ev []Evidence, err error)
//
//	// HtaccessDirectives (StageWeb, ID "web.htaccess:<domain>"): .htaccess của
//	// site theo chế độ mode (mode.OrDefault()). core chỉ đưa phép này vào khi
//	// web server là Apache và site không tạm khoá.
//	//   - an-toan: ScanHtaccess. Có Finding thì Fail, Summary nêu số chỉ thị
//	//     và chỉ thị đầu tiên, bằng chứng liệt kê "<file>:<dòng>: <chỉ thị>"
//	//     (tối đa 20 dòng), Cause: một chỉ thị ngoài danh sách làm mọi request
//	//     dưới thư mục đó báo 500. ManualFix: sửa dòng đó, hoặc
//	//     `sudo tpan site set <domain> --htaccess day-du` và nhận cảnh báo.
//	//     Không có: Pass. ScanHtaccess lỗi: Unknown.
//	//   - day-du: luôn Warn. Summary và Cause nói rõ: site này ghi được cấu
//	//     hình Apache qua .htaccess (AllowOverride All) nên, nếu bị chiếm, có
//	//     thể chạy mã dưới user của site khác (lỗ L4) và đọc file của site
//	//     khác qua symlink. ManualFix: `sudo tpan site set <domain> --htaccess an-toan`.
//	//   - tat: Pass, "Apache không đọc .htaccess của site này".
//	func HtaccessDirectives(l site.Layout, mode site.HtaccessMode) Check
//
//	// ServerNameUnique (StageWeb, ID "web.server_name.unique"): mỗi tên miền
//	// của site tpan chỉ được khai ở đúng vhost của site đó (lỗ L8). sites là
//	// Layout của mọi site active.
//	//   - nginx: `nginx -T`. Mỗi file bắt đầu bằng dòng
//	//     "# configuration file <đường dẫn>:"; trong từng khối server lấy các
//	//     cổng ở listen và các tên ở server_name. (nginx -T thoát mã 0 kèm
//	//     cảnh báo "conflicting server name" khi trùng: không dựa vào mã thoát.)
//	//   - apache: `apache2ctl -S`. Các dòng "port <n> namevhost <tên>
//	//     (<file>:<dòng>)" và "alias <tên>".
//	// Fail khi một tên miền của site xuất hiện ở hơn một server/VirtualHost
//	// trên cùng một cổng, hoặc trong một file không phải vhost của chính site
//	// đó (so với l.NginxConf, l.NginxLink hoặc l.ApacheConf, l.ApacheLink).
//	// Summary nêu tên miền, hai file và số dòng; Cause: web server chỉ dùng
//	// vhost được nạp trước, nên tên miền có thể đang trả trang của vhost lạ.
//	// ManualFix: xem hai file, gỡ file lạ bằng tay (tpan không xoá file nó
//	// không ghi), kiểm cú pháp, reload, kiểm lại. Lệnh không chạy được hoặc
//	// output không đọc được: Unknown. Không có site nào: Pass.
//	func ServerNameUnique(web string, sites []site.Layout) Check
//
//	// DefaultRejects (StageWeb, ID "web.default.rejects"): request mang tên
//	// lạ bị vhost mặc định từ chối, không rơi vào site nào. Tên thử là
//	// "tpan-<8 ký tự hex ngẫu nhiên>.invalid", sinh lúc dựng phép kiểm
//	// (crypto/rand; có hàm không xuất nhận tên sẵn để test).
//	//   - cổng 80: curl -sS --max-time 5 -o /dev/null -w %{http_code}
//	//     -H "Host: <tên>" http://127.0.0.1/ . nginx: đạt khi curl báo máy chủ
//	//     đóng kết nối không trả gì (mã thoát 52, như HTTPDefault); apache:
//	//     đạt khi 403.
//	//   - ssl=true (có ít nhất một site bật SSL), thêm cổng 443: curl -sS -k
//	//     --max-time 5 -o /dev/null -w %{http_code} --resolve <tên>:443:127.0.0.1
//	//     https://<tên>/ . nginx: đạt khi bắt tay TLS bị từ chối (curl mã thoát
//	//     35); apache: đạt khi 403.
//	// Trả lời khác (200, 301, 404, hay trang của một site): Fail, Cause: một
//	// vhost đang hứng mọi tên (default_server lạ, hoặc vhost mặc định của tpan
//	// bị gỡ). ManualFix: liệt kê sites-enabled, `sudo tpan health --fix`, kiểm
//	// lại. Không kết nối được: Fail như HTTPDefault. web lạ: Unknown.
//	func DefaultRejects(web string, ssl bool) Check
