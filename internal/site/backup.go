package site

import "time"

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

// ---- Dòng của bảng backups và cron_runs (migration 900). Ở gói lá này để
// store (ghi) và core (dùng) cùng thấy mà store không phải import core.

// BackupKind là loại file backup.
type BackupKind string

const (
	// BackupFull: .tar.gz gồm public/, db.sql, manifest.json (mục 6.5).
	BackupFull BackupKind = "full"
	// BackupSQL: bản xuất DB riêng .sql (mục 6.9).
	BackupSQL BackupKind = "sql"
)

// BackupReason là vì sao có bản backup.
type BackupReason string

const (
	ReasonManual        BackupReason = "manual"
	ReasonSchedule      BackupReason = "schedule"
	ReasonBeforeRestore BackupReason = "before-restore"
	ReasonBeforePurge   BackupReason = "before-purge"
	ReasonBeforeImport  BackupReason = "before-import"
)

// Backup là một dòng của bảng backups. Domain là chuỗi, không phải
// validate.Domain: dòng của site đã xoá hẳn vẫn được đọc lại nguyên vẹn.
type Backup struct {
	ID         string       `json:"id"`
	Domain     string       `json:"domain"`
	Kind       BackupKind   `json:"kind"`
	Reason     BackupReason `json:"reason"`
	Path       string       `json:"path"`
	Size       int64        `json:"size"`
	SHA256     string       `json:"sha256"`
	WPVersion  string       `json:"wp_version,omitempty"`
	PHPVersion string       `json:"php_version,omitempty"`
	JobID      string       `json:"job_id,omitempty"`
	Created    time.Time    `json:"created"`
}

// CronStatus là kết quả một lần chạy cron WordPress.
type CronStatus string

const (
	CronOK      CronStatus = "ok"
	CronFail    CronStatus = "fail"
	CronSkipped CronStatus = "skipped"
)

// CronRun là một dòng của bảng cron_runs.
type CronRun struct {
	Domain   string        `json:"domain"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration_ns"`
	Status   CronStatus    `json:"status"`
	ExitCode int           `json:"exit_code"`
	Output   string        `json:"output"`
}
