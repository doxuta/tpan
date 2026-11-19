package server

// Hợp đồng API của M12 (phiên bản và cập nhật, mục 6.11). Tách khỏi types.go
// để các mốc chạy song song không sửa chung một file. Quy ước lỗi, dry_run,
// JobStarted giống M3 (types.go). Handler ở api_m12.go.
//
//	  GET    /api/v1/php                            → core.PHPList (có ghi chú; ghi chú đệm 1 giờ trong server)
//	  POST   /api/v1/php/install[?dry_run=1]        PHPVersionBody → Preview | JobStarted
//	  POST   /api/v1/php/remove[?dry_run=1]         PHPVersionBody → Preview | JobStarted
//	  POST   /api/v1/sites/{domain}/php[?dry_run=1] PHPVersionBody → Preview | JobStarted
//	  GET    /api/v1/updates                        → core.UpdateStatus (không chạy apt-get update)
//	  POST   /api/v1/updates[?dry_run=1]            → Preview | JobStarted (nâng gói của stack)
//	  POST   /api/v1/updates/auto[?dry_run=1]       → Preview | JobStarted (bật bản vá tự động)
//	  POST   /api/v1/mariadb/upgrade[?dry_run=1]    MariaDBUpgradeBody → Preview | JobStarted
//
// Lời từ chối của core (php_version_invalid, php_not_installed, php_in_use,
// nothing_to_update, mariadb_not_newer...) là 409 kèm checks như mọi lời từ
// chối khác; thân JSON sai là 400 bad_request. Mọi POST ghi audit: php.install,
// php.remove, site.php, update.apply, update.auto, mariadb.upgrade.
//
// Sau job update.apply và mariadb.upgrade, server chạy health đầy đủ (như nút
// "Kiểm lại") để mở sự cố cho site không còn đạt (mục 6.11, "Cập nhật").
//
// Không có API cho self-update: thay file chạy của panel từ chính panel là cắt
// job đang chạy. Màn Cập nhật hiện lệnh `sudo tpan self-update` (khối lệnh).

// PHPVersionBody là thân của các POST về bản PHP.
type PHPVersionBody struct {
	Version string `json:"version"` // "8.4"
}

// MariaDBUpgradeBody là thân của POST /api/v1/mariadb/upgrade.
type MariaDBUpgradeBody struct {
	Line string `json:"line"` // "11.8"
}
