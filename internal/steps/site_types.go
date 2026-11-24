package steps

// Hợp đồng M2: các Step của một site WordPress (mục 5, 6.2 của docs/KE-HOACH.md).
//
// File này chỉ có chú thích. Bốn thợ viết bốn file mới song song trong cùng
// gói, nên mỗi người tự khai báo struct của mình (struct cần trường không xuất
// để giữ trạng thái cho Undo), đúng tên và đúng các trường xuất dưới đây. Core
// (đợt 2) ghép Plan dựa trên đúng các chữ ký này.
//
// Mọi tên và đường dẫn lấy từ site.Layout (gói internal/site), không tự ghép
// chuỗi. Step kiểm l.Domain.IsZero() và checkPath cho mọi đường dẫn: Layout
// zero hay đường dẫn lạ thì Check và Apply đều lỗi, không chạy lệnh nào.
//
// Mỗi file tự có `var _ engine.Step = (*X)(nil)` cho các Step của mình (không
// sửa common.go). Hàm và biến không xuất mang tiền tố riêng của file để bốn
// file không đặt trùng tên: site_user.go dùng "user", site_db.go dùng "db",
// site_web.go dùng "web", site_wp.go dùng "wp".
//
// ---------------------------------------------------------------- site_user.go
//
//	// SiteUser đảm bảo user Linux l.User có, kèm nhóm riêng cùng tên, UID và
//	// GID là số N do UIDs cấp (tăng dần từ 65536, không bao giờ dùng lại; M28
//	// đợt 2, F1):
//	// groupadd --gid N l.User
//	// useradd --system --uid N --gid N --home-dir l.Dir --no-create-home
//	//         --shell /usr/sbin/nologin l.User
//	// Check: `getent passwd l.User` (mã 2 = chưa có). Đã có mà home hoặc shell
//	// khác thì lỗi (không sửa user không do tpan tạo). Undo: userdel l.User,
//	// chỉ khi chính bước này đã tạo.
//	type SiteUser struct{ Layout site.Layout; UIDs UIDReserver }
//
//	// SiteUserAbsent xoá user (và nhóm cùng tên): trước userdel gỡ crontab,
//	// giết tiến trình còn sống, xoá file của UID ở /tmp, /var/tmp, /dev/shm
//	// (F1). Danger, NoUndo.
//	type SiteUserAbsent struct{ Layout site.Layout }
//
//	// SiteDirs trả các bước dựng thư mục của site theo mục 5, đúng thứ tự:
//	//   Dir{l.Dir,    2750, l.User:www-data}
//	//   Dir{l.Public, 2750, l.User:www-data}
//	//   Dir{l.Tmp,    0700, l.User:l.User}
//	//   rồi các bước của SiteLogs (M14, m14_types.go): thư mục log
//	//   root:l.User 0750, access.log và error.log root:l.User 0640,
//	//   php-error.log l.User:l.User 0640. Trước M14 thư mục log là root:root
//	//   0755 và chỉ php-error.log được tạo sẵn.
//	// (2750 là fs.ModeSetgid|0o750.) Dùng lại steps.Dir của M1.
//	func SiteDirs(l site.Layout) []engine.Step
//
//	// OwnedFile đảm bảo một file có, đúng quyền và chủ, KHÔNG quan tâm nội dung
//	// (log do chương trình khác ghi; File của M1 so nội dung nên sẽ ghi đè log).
//	// Chưa có thì tạo rỗng. Undo: xoá nếu bước này tạo, trả quyền và chủ cũ nếu sửa.
//	type OwnedFile struct {
//		Path  string
//		Perm  fs.FileMode
//		Owner system.Owner
//	}
//
//	// RemoveTree xoá cả một cây thư mục: rm -r --one-file-system -- Path.
//	// Path phải nằm hẳn bên dưới Under (ví dụ Under "/var/www", Path
//	// "/var/www/example.com"; Path == Under hay ra ngoài Under thì lỗi).
//	// Check: Path không còn. Danger, NoUndo.
//	type RemoveTree struct {
//		Path  string
//		Under string
//	}
//
// ------------------------------------------------------------------ site_db.go
//
//	// SiteDB đảm bảo DB l.DB (utf8mb4) và user 'l.DB'@'localhost' có quyền
//	// ALL trên đúng DB đó. SQL đi qua stdin (mariadbCmd), mật khẩu không bao
//	// giờ nằm trong Info().Commands hay thông báo lỗi.
//	// Check: DB có VÀ user có. Apply: tạo phần còn thiếu, nhớ phần nào mình
//	// tạo. Undo: DROP đúng phần mình tạo (user trước, DB sau).
//	// Password chỉ nhận [A-Za-z0-9], dài ≥ 24: lớp chặn cuối trước khi đặt vào
//	// chuỗi SQL. Password zero hoặc sai thì Check và Apply đều lỗi.
//	type SiteDB struct {
//		Layout   site.Layout
//		Password system.Secret
//	}
//
//	// SiteDBAbsent: DROP USER IF EXISTS rồi DROP DATABASE IF EXISTS. Danger, NoUndo.
//	type SiteDBAbsent struct{ Layout site.Layout }
//
//	// SiteDBState cho core biết DB và user DB của site có chưa, để từ chối khi
//	// máy lệch (user DB có mà wp-config.php không có, hoặc ngược lại).
//	func SiteDBState(ctx context.Context, sys system.System, l site.Layout) (dbExists, userExists bool, err error)
//
// ----------------------------------------------------------------- site_web.go
//
// Template ở internal/templates/site.go (cùng thợ). web là "nginx" hoặc
// "apache"; phpVersion dạng "8.3" (kiểm ^[0-9]+\.[0-9]+$). Các hàm trả lỗi
// khi tham số sai. Dùng lại File, Symlink, Absent của M1.
//
//	// SitePool: File{l.PoolConf(v), 0644 root, Test: php-fpm<v> -t,
//	// Reload: systemctl reload php<v>-fpm}.
//	func SitePool(l site.Layout, phpVersion string) (engine.Step, error)
//
//	// SiteVhost: File{conf, 0644 root} rồi Symlink{conf → link}, cả hai có
//	// Test (nginx -t | apachectl configtest) và Reload (systemctl reload
//	// nginx | apache2). acmeRoot là webroot chung của Let's Encrypt.
//	func SiteVhost(l site.Layout, web, phpVersion, acmeRoot string) ([]engine.Step, error)
//
//	// SiteHtaccess (chỉ Apache): File{l.Public+"/.htaccess", 0640,
//	// l.User:www-data} với nội dung chuẩn của WordPress.
//	func SiteHtaccess(l site.Layout) (engine.Step, error)
//
//	// SiteWebAbsent: Absent link rồi Absent conf của vhost (có Test, Reload),
//	// rồi Absent pool (Test php-fpm<v> -t, Reload php<v>-fpm). Gỡ vhost trước
//	// để không lúc nào web server trỏ vào một socket đã mất.
//	func SiteWebAbsent(l site.Layout, web, phpVersion string) ([]engine.Step, error)
//
// ------------------------------------------------------------------ site_wp.go
//
//	// WPArchive tải bản WordPress Version vào /var/cache/tpan/ một lần, kiểm
//	// checksum (giống WPCLI của M1). Có hằng DefaultWordPressVersion và
//	// DefaultWordPress<Hash>, tra từ wordpress.org, không viết theo trí nhớ.
//	// Undo không xoá cache (NoUndo, lý do: dùng lại cho site sau).
//	type WPArchive struct {
//		Version string
//		Hash    string // hex, loại hash ghi trong chú thích của hằng
//	}
//	// Path trả đường dẫn file tải về trong cache.
//	func (s *WPArchive) Path() string
//
//	// WPFiles giải nén Archive vào l.Public dưới user l.User (AsUser), rồi đặt
//	// thư mục 2750 và file 0640. Check: l.Public/wp-includes/version.php có.
//	// Undo: xoá mọi thứ bên trong l.Public (giữ l.Public, vì Dir của nó tự Undo).
//	type WPFiles struct {
//		Layout  site.Layout
//		Archive string
//	}
//
//	// WPConfig: wp config create (mật khẩu DB qua stdin), thêm
//	// DISALLOW_FILE_EDIT, chmod 600. Chạy dưới l.User. Check: l.WPConfig có,
//	// 0600, chủ l.User. Undo: xoá file.
//	type WPConfig struct {
//		Layout     site.Layout
//		DBPassword system.Secret
//	}
//
//	// WPInstall: wp core install (mật khẩu quản trị qua stdin, --skip-email),
//	// rồi permalink /%postname%/. Check: wp core is-installed. NoUndo, lý do:
//	// bảng của WordPress nằm trong DB của site, bước SiteDB hoàn tác thì mất theo.
//	type WPInstall struct {
//		Layout        site.Layout
//		URL           string // http://<domain>
//		Title         string // core đã kiểm: không ký tự điều khiển, ≤ 200 byte
//		AdminUser     string
//		AdminEmail    validate.Email
//		AdminPassword system.Secret
//	}
