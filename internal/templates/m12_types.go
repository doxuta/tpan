package templates

// Hợp đồng M12 (đợt 0): template của phiên bản và cập nhật (mục 6.11 của
// docs/KE-HOACH.md). File này chỉ có chú thích; thợ đợt 1 viết m12.go, các
// file .tmpl mới trong files/ và golden trong testdata/ (go test -update).
// Mọi tham số qua cùng các phép kiểm ký tự như template của M1, M2 (lỗi bọc
// ErrParam); không tham số nào là chuỗi người dùng gõ tự do.
//
//	// PHPIdlePool sinh /etc/php/<version>/fpm/pool.d/tpan-idle.conf: pool giữ
//	// chỗ để PHP-FPM bản này khởi động được khi không có site nào (PHP-FPM từ
//	// chối chạy khi không có pool). Dòng đầu là sitePoolComment (cú pháp INI).
//	//   [tpan-idle]
//	//   user = www-data, group = www-data
//	//   listen = /run/php/tpan-idle-<version>.sock
//	//   listen.owner = root, listen.group = root, listen.mode = 0600
//	//   pm = ondemand, pm.max_children = 1, pm.process_idle_timeout = 10s
//	// Kèm chú thích tiếng Việt: vì sao có pool này, vì sao không ai nói chuyện
//	// được với nó (socket chỉ root), vì sao không tốn RAM (ondemand). version
//	// phải khớp phpVersion.
//	func PHPIdlePool(version string) ([]byte, error)
//
//	// AptSource là một kho apt dạng deb822 (man sources.list, "DEB822-STYLE").
//	type AptSource struct {
//		URIs       string // https://packages.sury.org/php/
//		Suites     string // noble
//		Components string // main (nginx.org: nginx)
//		KeyPath    string // steps.AptKeyPath(name)
//	}
//	// AptSources sinh /etc/apt/sources.list.d/tpan-<tên>.sources: dòng đầu
//	// Header, rồi Types: deb, URIs, Suites, Components, Signed-By. URIs phải
//	// là https://, không khoảng trắng; Suites, Components chỉ [a-z0-9.-]
//	// (nhiều component cách nhau một dấu cách); KeyPath tuyệt đối dưới
//	// /etc/apt/keyrings/.
//	func AptSources(s AptSource) ([]byte, error)
//
//	// AptPinRule là một đoạn của file /etc/apt/preferences.d/tpan-<tên>.
//	type AptPinRule struct {
//		Packages []string // ["*"] hoặc tên gói / mẫu glob như "php8.4*"
//		Origin   string   // host của kho, ví dụ packages.sury.org
//		Priority int      // 1..1000
//	}
//	// AptPin sinh file ghim: dòng đầu Header, mỗi luật một đoạn
//	// "Package: a b\nPin: origin <Origin>\nPin-Priority: <n>", các đoạn cách
//	// nhau một dòng trống, kèm một dòng chú thích (bắt đầu bằng "Explanation:")
//	// nói vì sao. Gói chỉ nhận [a-z0-9.+*-]; Origin là tên máy.
//	func AptPin(rules []AptPinRule) ([]byte, error)
//
//	// UnattendedUpgrades sinh /etc/apt/apt.conf.d/52tpan-unattended-upgrades
//	// (đọc sau 20auto-upgrades và 50unattended-upgrades của Ubuntu nên thắng):
//	//   APT::Periodic::Update-Package-Lists "1";
//	//   APT::Periodic::Unattended-Upgrade "1";
//	//   Unattended-Upgrade::Origins-Pattern { "origin=<o>"; ... };  (chỉ khi có origin)
//	// Danh sách trong apt.conf được cộng dồn, nên Origins-Pattern ở đây thêm vào
//	// danh sách của Ubuntu chứ không thay. origins chỉ nhận [A-Za-z0-9 .:_-].
//	func UnattendedUpgrades(origins []string) ([]byte, error)
//
//	// NginxOrgInclude sinh /etc/nginx/conf.d/tpan.conf cho nginx của nginx.org
//	// (gói đó chỉ đọc conf.d/*.conf, không có sites-enabled):
//	//   include /etc/nginx/sites-enabled/*;
//	// cộng server_names_hash_bucket_size 64 (tên miền dài), kèm chú thích.
//	func NginxOrgInclude() ([]byte, error)
//
// Sửa template đã có (golden cập nhật theo):
//   - site-nginx.conf.tmpl, site-nginx-ssl.conf.tmpl: `include fastcgi.conf;`
//     thành `include fastcgi_params;` cộng
//     `fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;`
//     (gói nginx của nginx.org không có fastcgi.conf; fastcgi.conf của Ubuntu
//     chính là fastcgi_params cộng dòng SCRIPT_FILENAME đó).
//   - logrotate-sites.tmpl, nhánh nginx: postrotate không dùng
//     `invoke-rc.d nginx rotate` (cần init script của gói Ubuntu) mà gửi USR1
//     cho tiến trình chính: `[ ! -f /run/nginx.pid ] || kill -USR1 "$(cat /run/nginx.pid)"`
//     (cả hai gói đều ghi /run/nginx.pid).
