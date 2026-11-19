package checks

// Hợp đồng M9 (đợt 0): phép kiểm "tuổi bản sao lưu gần nhất của tpan.db" mà
// M4 để lại (mục 6.7 "Tự kiểm TPAN", ghi chú "Đã chốt khi làm M4"). Thợ đợt 1
// viết ở m9.go kèm test; phiên chính nối vào health/collect.go (selfChecks).
//
//	// TPANBackupAge (ID "tpan.backup:db", StageTPAN): đọc dir (ReadDir; mặc
//	// định site.TPANDBBackupDir), lấy file tên dạng tpan-<YYYYMMDD-HHMMSS>.db
//	// mới nhất theo thời điểm trong tên (FileInfo không có mtime). Không có
//	// thư mục hay không có file nào: Warn "chưa có bản sao lưu tpan.db nào"
//	// (ManualFix: `sudo tpan backup create --all --yes` và
//	// `sudo tpan backup settings --at 03:30`). Cũ hơn maxAge: Warn, nói số
//	// giờ. Còn mới: Pass. ReadDir lỗi khác không có: Unknown. Bằng chứng là
//	// "ls <dir>" kèm tên file mới nhất. now nil thì time.Now. Tên file dùng
//	// giờ UTC (Backups.SnapshotTPANDB ghi như vậy).
//	func TPANBackupAge(dir string, maxAge time.Duration, now func() time.Time) Check
