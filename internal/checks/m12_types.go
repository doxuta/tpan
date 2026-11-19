package checks

// Hợp đồng M12 (đợt 0): phép kiểm của phiên bản và cập nhật (mục 6.11 của
// docs/KE-HOACH.md). File này chỉ có chú thích; thợ đợt 1 viết m12.go và
// m12_test.go theo khuôn của các phép kiểm đã có (base, Evidence, ManualFix).
//
//	// PHPPoolWWWAbsent (StagePHP, ID "php.www_pool:<version>"): pool www của gói
//	// đã được gỡ: /etc/php/<version>/fpm/pool.d/www.conf không có VÀ
//	// tpan-idle.conf có. Còn www.conf thì Warn (máy vẫn chạy, chỉ tốn 2 tiến
//	// trình dưới www-data), Summary nói vì sao nên gỡ, ManualFix chỉ lệnh
//	// `sudo tpan stack install --web <web>` (chạy lại sẽ gỡ) và lệnh kiểm lại
//	// (`ls /etc/php/<version>/fpm/pool.d`, `ps -eo user,cmd | grep [p]hp-fpm`).
//	// Không có thư mục pool.d thì Unknown. version phải khớp phpVersionRE.
//	func PHPPoolWWWAbsent(version string) Check
//
//	// UnattendedUpgrades (StageMachine, ID "updates.auto"): bản vá bảo mật tự
//	// cài hằng ngày. Đọc `apt-config dump APT::Periodic::Unattended-Upgrade`
//	// (dòng `APT::Periodic::Unattended-Upgrade "1";`) và trạng thái gói
//	// unattended-upgrades (dpkg-query). Pass khi gói đã cài và giá trị là "1";
//	// Warn khi tắt hoặc chưa cài (ManualFix: `sudo tpan update auto --yes`,
//	// lệnh kiểm lại là chính lệnh apt-config ở trên). apt-config không chạy
//	// được thì Unknown.
//	func UnattendedUpgrades() Check
//
