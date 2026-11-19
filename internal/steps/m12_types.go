package steps

// Hợp đồng M12 (đợt 0): các Step của phiên bản và cập nhật (mục 6.11 của
// docs/KE-HOACH.md).
//
// File này chỉ có chú thích. Thợ đợt 1 viết file mới, không sửa file của mốc
// trước (trừ khi ghi rõ dưới đây):
//   - apt_m12.go (thợ "apt"): AptRepo, AptUpgrade, AptRemove, ServiceRestart,
//     ServiceStop, UserInGroup, MariaDBDump, MariaDBUpgradeRun.
//   - php_m12.go và self.go (thợ "php và self"): PHPIdlePool, SelfDownload,
//     SelfInstall.
// Mỗi file tự có `var _ engine.Step = (*X)(nil)` cho Step của mình. Core (đợt 2)
// ghép Plan dựa trên đúng tên và trường xuất dưới đây. Mọi lệnh apt-get mới có
// `-o DPkg::Lock::Timeout=300` (máy mới tạo có apt-daily, unattended-upgrades
// chạy nền giữ khoá dpkg). Luật chung của gói (common.go) vẫn áp dụng: tham số
// sai thì Check và Apply lỗi trước khi chạy lệnh; Apply đã đổi máy mà bước sau
// lỗi thì tự trả máy về.
//
// ---------------------------------------------------------------- apt_m12.go
//
//	// AptKeyPath là nơi AptRepo đặt khoá của kho Name:
//	// /etc/apt/keyrings/tpan-<Name>.gpg (keyring nhị phân). Core dùng đường dẫn
//	// này trong Signed-By của file sources.
//	func AptKeyPath(name string) string
//
//	// AptRepo thêm một kho apt ngoài Ubuntu (mục 6.11), Danger.
//	//   1. Tải KeyURL bằng curl (https, -fsSL, timeout) vào file tạm; nhập vào
//	//      một homedir gpg tạm (--homedir, --batch, --no-default-keyring) rồi
//	//      `gpg --export` CHỈ các vân tay trong Fingerprints ra AptKeyPath(Name)
//	//      (0644 root). Khoá tải về không chứa vân tay nào trong danh sách thì
//	//      lỗi, không ghi gì. Vân tay so bằng chữ hoa, không dấu cách.
//	//   2. Ghi Sources (nội dung deb822 core đã render, có Signed-By trỏ
//	//      AptKeyPath) vào /etc/apt/sources.list.d/tpan-<Name>.sources (0644).
//	//   3. Pin khác nil: ghi /etc/apt/preferences.d/tpan-<Name> (0644). Pin nil
//	//      thì file đó phải không có (có thì gỡ).
//	//   4. `apt-get update`; lỗi thì trả ba file về như trước rồi báo.
//	// Check: ba file đúng nội dung (khoá: chứa đúng các vân tay, đọc bằng
//	// `gpg --show-keys --with-colons`), và đã update sau khi ghi (nhớ trong
//	// Step như AptUpdate). Undo: trả ba file về như trước, `apt-get update`.
//	// Info: Title nói tên kho; Why nói đây là nguồn gói ngoài Ubuntu; Commands
//	// có lệnh curl và apt-get update; Files có sources và pin (diff được) và
//	// một dòng mô tả khoá kèm vân tay. Name khớp ^[a-z0-9][a-z0-9-]*$.
//	type AptRepo struct {
//		Name         string   // "sury", "nginx", "mariadb"
//		Title        i18n.Text // "kho PHP của Ondřej Surý (packages.sury.org)"
//		KeyURL       string   // https://...
//		Fingerprints []string // 40 ký tự hex
//		Sources      []byte
//		Pin          []byte
//	}
//
//	// AptUpgrade nâng các gói đã cài lên bản mới nhất kho đang có:
//	//   apt-get install -y --only-upgrade -o DPkg::Lock::Timeout=300
//	//     -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold <gói...>
//	// (giữ file cấu hình đang có, không hỏi). Check: `apt-get -s install
//	// --only-upgrade <gói...>` không có dòng "Inst " nào cho các gói này.
//	// Danger (dịch vụ có thể tự khởi động lại trong lúc nâng). NoUndo: không hạ
//	// gói về được bằng apt một cách an toàn.
//	type AptUpgrade struct{ Packages []string }
//
//	// AptRemove gỡ gói (giữ file cấu hình): apt-get remove -y <gói...>. Check:
//	// không gói nào còn cài (missingPackages). Danger, NoUndo. Từ chối như
//	// AptInstall nếu danh sách trống hay tên lạ.
//	type AptRemove struct{ Packages []string }
//
//	// ServiceRestart: Test (nếu có) rồi `systemctl restart Unit`. Check: false
//	// tới khi Apply của chính bước này xong (nhớ trong Step). OnlyIfActive: dịch
//	// vụ không active thì Check true ngay (không khởi động thứ đang tắt).
//	// Danger. NoUndo (khởi động lại không có gì để trả về).
//	type ServiceRestart struct {
//		Unit         string
//		Test         *system.Cmd
//		OnlyIfActive bool
//	}
//
//	// ServiceStop: `systemctl stop Unit`. Check: is-active không phải active.
//	// Undo: `systemctl start Unit` (chỉ khi lúc đầu đang chạy). Danger.
//	type ServiceStop struct{ Unit string }
//
//	// UserInGroup: `usermod -aG Group User`. Check: `id -nG User` có Group.
//	// Undo: `gpasswd -d User Group` (chỉ khi Apply đã thêm).
//	type UserInGroup struct{ User, Group string }
//
//	// MariaDBDump xuất toàn bộ DB trước khi nâng dòng MariaDB:
//	//   mariadb-dump --all-databases --single-transaction --routines --events
//	//     --triggers --result-file=Path
//	// (file, không qua stdout: LocalSystem.Run giữ output trong bộ nhớ). Path
//	// tuyệt đối dưới /var/lib/tpan/backups/; thư mục cha tạo 0700 root nếu
//	// chưa có; file 0600 root. Kiểm file xuất: dòng cuối bắt đầu bằng
//	// "-- Dump completed"; không thì lỗi. Check: Path có và kiểm như trên đạt.
//	// NoUndo (giữ file xuất: nó là đường lùi). Không Danger.
//	type MariaDBDump struct{ Path string }
//
//	// MariaDBUpgradeRun chạy `mariadb-upgrade` sau khi nâng gói. Check: file
//	// /var/lib/mysql/mariadb_upgrade_info có và nội dung bắt đầu bằng bản server
//	// đang chạy (`mariadb -N -B -e "SELECT VERSION()"`, phần trước dấu "-").
//	// NoUndo.
//	type MariaDBUpgradeRun struct{}
//
// ---------------------------------------------------------------- php_m12.go
//
//	// PHPIdlePool xử lý pool www thừa của gói php<bản>-fpm (mục 16, M2 "Còn
//	// mở": 2 worker dưới www-data dù không site nào dùng):
//	//   1. Ghi /etc/php/<bản>/fpm/pool.d/tpan-idle.conf = Render(bản) (0644
//	//      root). PHP-FPM không khởi động khi không có pool nào ("No pool
//	//      defined"), nên bản chưa có site nào vẫn cần pool này; nó là ondemand,
//	//      socket chỉ root, nên không có tiến trình con nào.
//	//   2. Gỡ /etc/php/<bản>/fpm/pool.d/www.conf (giữ nội dung trong Step để
//	//      Undo ghi lại). Đã kiểm: `apt-get install --reinstall php8.3-fpm`
//	//      không đem www.conf về.
//	//   3. `php-fpm<bản> -t`, rồi `systemctl reload php<bản>-fpm` nếu đang active.
//	//      Lỗi thì trả hai file về như trước, reload lại, báo.
//	// Version là con trỏ vì Plan cài stack dựng trước khi biết bản PHP (DetectPHP
//	// ghi vào lúc chạy); *Version rỗng lúc Check/Apply thì lỗi. Info dùng
//	// *Version nếu đã có, không thì "<bản>". Check: tpan-idle.conf đúng nội dung
//	// và www.conf không có. Undo: ghi lại www.conf, gỡ tpan-idle.conf (hoặc trả
//	// bản cũ), kiểm, reload.
//	type PHPIdlePool struct {
//		Version *string
//		Render  func(version string) ([]byte, error) // templates.PHPIdlePool
//	}
//
// ------------------------------------------------------------------ self.go
//
//	// SelfDownload tải bản phát hành của tpan:
//	//   curl -fsSL --max-time 300 -o Dest <Base>/tpan-linux-<Arch>
//	//   curl -fsSL --max-time 30 <Base>/SHA256SUMS   (dòng "<sha256>  tpan-linux-<Arch>")
//	// Base: https://... hoặc http:// chỉ khi host là 127.0.0.1, localhost, ::1.
//	// Arch: amd64 | arm64. Kiểm sha256 (đọc file bằng Sys, tính trong Go);
//	// sai thì xoá Dest và lỗi. chmod 0755 root. Chạy `Dest version` để đọc bản
//	// mới, ghi vào *NewVersion (nếu khác nil). Dest phải cùng thư mục với file
//	// chạy (rename không qua phân vùng). Check: Dest có, sha256 khớp dòng
//	// SHA256SUMS đã tải lần này (Check trước Apply thì false). Undo: xoá Dest.
//	type SelfDownload struct {
//		Base, Arch, Dest string
//		NewVersion       *string
//	}
//
//	// SelfInstall thay file chạy: Bin → Old, New → Bin (hai lần rename trong
//	// cùng thư mục; Linux cho thay file đang chạy). Check: New không còn và Bin
//	// có (đã thay). Undo: Bin → New, Old → Bin. Danger.
//	type SelfInstall struct{ Bin, New, Old string }
