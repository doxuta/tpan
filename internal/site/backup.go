package site

// Đường dẫn của backup và cron (mục 5, 6.5, 6.10 của docs/KE-HOACH.md, M9).
// Tách file riêng như trash.go để Layout không phải đổi.

// BackupRoot là thư mục cha của mọi backup: root:root 0700. Thư mục cha của nó
// (/var/lib/tpan) giữ 0755 vì acme bên trong phải đi qua được.
const BackupRoot = "/var/lib/tpan/backups"

// BackupDir là thư mục backup của một site: BackupRoot/<domain>, root 0700.
// Theo domain (không theo slug) để người đọc `ls` là biết của site nào, và để
// backup của site đã xoá hẳn vẫn tìm được theo tên.
func BackupDir(l Layout) string {
	if l.Domain.IsZero() {
		return ""
	}
	return BackupRoot + "/" + l.Domain.String()
}

// TPANDBBackupDir giữ các bản sao của tpan.db (VACUUM INTO). Tên có "_" nên
// không bao giờ trùng thư mục của một domain (validate.Domain chỉ nhận
// a-z0-9 và "-").
const TPANDBBackupDir = BackupRoot + "/_tpan"

// RestoreRoot là chỗ giải nén bản backup trước khi tráo vào site: root 0700,
// cùng phân vùng với /var/www (như thùng rác) để tráo bằng rename.
const RestoreRoot = "/var/lib/tpan/restore"

// RestoreDir là thư mục giải nén của một lần khôi phục site: RestoreRoot/<slug>.
func RestoreDir(l Layout) string {
	if l.Domain.IsZero() {
		return ""
	}
	return RestoreRoot + "/" + l.Slug.String()
}

// CronFile là lịch cron WordPress của site: /etc/cron.d/tpan-<slug>, root
// 0644. Theo slug, không theo domain: cron của Debian bỏ qua (không báo gì)
// file trong /etc/cron.d có dấu chấm trong tên.
func CronFile(l Layout) string {
	if l.Domain.IsZero() {
		return ""
	}
	return "/etc/cron.d/tpan-" + l.Slug.String()
}

// BackupCronFile là lịch backup theo giờ đặt (`tpan backup settings --at`).
const BackupCronFile = "/etc/cron.d/tpan-backup"
