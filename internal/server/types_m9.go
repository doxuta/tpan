package server

// Hợp đồng API của M9 (backup mục 6.5, DB mục 6.9, cron mục 6.10). Quy ước
// lỗi, dry_run, JobStarted giống M3 (types.go). Thợ đợt 3 viết thân ở
// api_m9.go. Mọi thao tác có thay đổi là job một Plan (planJob của
// api_trash.go, kiểu apiPlan: core.Planned thoả nó) và ghi audit với action
// trong ngoặc.
//
//	  GET    /api/v1/backups[?domain=D]                    → BackupsOverview
//	  POST   /api/v1/sites/{domain}/backups[?dry_run=1]    → Preview | JobStarted   (backup.create)
//	  POST   /api/v1/backups/{id}/restore[?dry_run=1]      → Preview | JobStarted   (backup.restore; site lấy từ dòng backup)
//	  POST   /api/v1/backups/settings[?dry_run=1]          BackupSettingsRequest → Preview | JobStarted (backup.settings)
//	  GET    /api/v1/cron                                  → core.CronOverview
//	  POST   /api/v1/databases/{domain}/password[?dry_run=1]  → Preview | JobStarted (db.passwd)
//	  POST   /api/v1/databases/{domain}/dump[?dry_run=1]      → Preview | JobStarted (db.dump)
//	  POST   /api/v1/databases/{domain}/import[?dry_run=1]    DBImportRequest → Preview | JobStarted (db.import)
//
// Lời từ chối của core (disk_space_low, backup_not_found, backup_corrupt,
// site_trashed, site_not_found...) là 409 kèm checks như mọi lời từ chối
// khác; backup_keep_invalid, backup_at_invalid, domain_invalid là 400;
// backup_not_found là 404. Job của restore, password, import kick health cho
// site đó (như site.restore của M6).
//
// GET /api/v1/databases (M3) giữ nguyên; bản xuất DB của một site lấy ở
// GET /api/v1/backups?domain=D (Kind "sql").

import "github.com/doxuta/tpan/internal/core"

// BackupsOverview là nội dung màn Backup.
type BackupsOverview struct {
	Settings core.BackupSettings `json:"settings"`
	// Items: mới nhất trước; gồm cả backup của site đã xoá hẳn.
	Items []core.Backup `json:"items"`
}

// BackupSettingsRequest là thân của POST /api/v1/backups/settings. At rỗng
// là tắt lịch.
type BackupSettingsRequest struct {
	Keep int    `json:"keep"`
	At   string `json:"at"`
}

// DBImportRequest là thân của POST /api/v1/databases/{domain}/import:
// BackupID là id một bản xuất (Kind "sql") của đúng site đó.
type DBImportRequest struct {
	BackupID string `json:"backup_id"`
}
