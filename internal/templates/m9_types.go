package templates

// Hợp đồng M9 (đợt 0): template của cron (mục 6.5, 6.10 của
// docs/KE-HOACH.md). File này chỉ có chú thích; thợ đợt 1 viết m9.go, các
// file .tmpl mới trong files/ và golden trong testdata/ (go test -update).
//
// File trong /etc/cron.d theo man 5 crontab của Debian: mỗi dòng
// "phút giờ ngày tháng thứ USER lệnh", file phải kết thúc bằng xuống dòng,
// root 0644, tên không có dấu chấm (cron bỏ qua im lặng). Dòng đầu là Header
// (cron coi dòng bắt đầu bằng # là chú thích). Lệnh dùng đường dẫn tuyệt đối
// vì PATH của cron rất hẹp. Dòng cron do /bin/sh chạy, nên chỉ được chứa giá
// trị đã kiểm: domain qua validate.Domain (site.Layout), giờ qua kiểm số.
//
//	// SiteCron sinh /etc/cron.d/tpan-<slug> (site.CronFile): một dòng
//	//   */5 * * * * root /usr/local/bin/tpan cron run <domain> >/dev/null 2>&1
//	// kèm chú thích tiếng Việt: vì sao chạy bằng root rồi tpan tự hạ xuống
//	// user của site (ghi kết quả vào tpan.db cho màn Cron, bỏ qua site
//	// không active), vì sao bỏ output (kết quả đã nằm trong tpan.db; cron
//	// không có máy gửi thư). Layout zero: lỗi ErrParam.
//	func SiteCron(l site.Layout) ([]byte, error)
//
//	// BackupCron sinh /etc/cron.d/tpan-backup (site.BackupCronFile): một dòng
//	//   <MM> <HH> * * * root /usr/local/bin/tpan backup create --all --scheduled --yes >/dev/null 2>&1
//	// at là "HH:MM" 24 giờ (00..23, 00..59, đúng hai chữ số mỗi phần), sai
//	// thì ErrParam. Chú thích: giờ theo múi giờ của máy; kết quả xem ở màn
//	// Backup và Lịch sử.
//	func BackupCron(at string) ([]byte, error)
