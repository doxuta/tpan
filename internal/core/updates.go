package core

// Ca sử dụng của màn Cập nhật (mục 6.11 của docs/KE-HOACH.md): gói của stack
// có bản mới, bản vá bảo mật tự động, nâng MariaDB. Hợp đồng M12 đợt 0; thân
// do thợ đợt 2 viết.
//
// Status: `apt-get -s -o Debug::NoLocking=1 dist-upgrade` (chỉ đọc), tách gói
// của stack (tên là hoặc bắt đầu bằng: nginx, apache2, mariadb-, galera-,
// php, certbot, python3-certbot, curl, libcurl) khỏi phần còn lại. refresh=true (chỉ CLI `tpan update --check`) thì lấy khoá
// toàn cục và chạy `apt-get update` trước. MariaDB.Upgrades và ghi chú hỏi mạng qua curl
// của Sys với timeout ngắn; không mạng thì để trống, không lỗi.
//
// Plan "update.apply" (tpan update): AptUpdate luôn chạy (lists mới nhất), rồi
// steps.AptUpgrade{gói của stack đang chờ} (giữ file cấu hình cũ:
// --force-confdef --force-confold), kiểm config (nginx -t | apachectl
// configtest, php-fpm<bản> -t cho mỗi bản đã cài), steps.ServiceRestart cho
// dịch vụ có gói vừa nâng (web server, php<bản>-fpm, mariadb). Kiểm chứng:
// stackChecks cộng siteChecks của mọi site active. Không có gói nào chờ thì từ
// chối nothing_to_update. CLI và server chạy lại toàn bộ health (health.Runner)
// sau job để mở sự cố cho site không còn đạt.
//
// Plan "update.auto" (bật bản vá tự động): AptInstall unattended-upgrades; File
// /etc/apt/apt.conf.d/52tpan-unattended-upgrades (templates.UnattendedUpgrades
// với origin của các kho ngoài đang dùng: "origin=nginx", "origin=MariaDB",
// "origin=deb.sury.org"). Kiểm chứng: checks.UnattendedUpgrades.
//
// Plan "mariadb.upgrade" (tpan mariadb upgrade --to <dòng>), một chiều, không
// hoàn tác tự động (mục 6.11): steps.MariaDBDump vào
// /var/lib/tpan/backups/mariadb-upgrade-<thời điểm>.sql (kiểm file xuất);
// steps.ServiceStop mariadb; steps.AptRepo của MariaDB dòng mới; AptUpgrade các
// gói mariadb đang cài; Service mariadb; steps.MariaDBUpgradeRun; kiểm mọi site.
// Plan.Title và Why của bước đầu nói rõ đường lùi: cài lại dòng cũ rồi nhập lại
// từ file xuất. Từ chối: mariadb_line_invalid, mariadb_not_newer (chỉ nâng lên).
// Thành công thì after ghi stack.db_source = mariadb.org, stack.db_line.

import "context"

// Status đọc nội dung màn Cập nhật. refresh chạy apt-get update trước (lấy khoá).
func (u *Updates) Status(ctx context.Context, refresh bool) (UpdateStatus, error) {
	return UpdateStatus{}, errM12Todo
}

// PrepareApply dựng Plan "update.apply". Chỉ đọc máy.
func (u *Updates) PrepareApply(ctx context.Context) (*Planned, error) {
	return nil, errM12Todo
}

// PrepareAuto dựng Plan "update.auto". Chỉ đọc máy.
func (u *Updates) PrepareAuto(ctx context.Context) (*Planned, error) {
	return nil, errM12Todo
}

// PrepareMariaDB dựng Plan "mariadb.upgrade" lên dòng line. Chỉ đọc máy.
func (u *Updates) PrepareMariaDB(ctx context.Context, line string) (*Planned, error) {
	return nil, errM12Todo
}
