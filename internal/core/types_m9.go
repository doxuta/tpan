package core

// Hợp đồng M9: backup và khôi phục (mục 6.5), thao tác DB (mục 6.9), cron
// thật cho WordPress (mục 6.10) của docs/KE-HOACH.md. CLI, API, MCP, health
// và giao diện dựa vào các kiểu và chữ ký ở đây; thân hàm nằm ở backup.go,
// dbops.go, cron.go.
//
// ---------------------------------------------------------------- backup.go (Backups)
//
//	// List trả backup, mới nhất trước. domain rỗng: mọi backup (cả của site
//	// đã xoá hẳn: dòng backups không có khoá ngoại tới sites).
//	func (b *Backups) List(ctx context.Context, domain string) ([]Backup, error)
//	// Settings đọc SettingBackupKeep (trống: DefaultBackupKeep) và
//	// SettingBackupAt (trống: tắt). Giá trị hỏng trong DB: lỗi, không đoán.
//	func (b *Backups) Settings(ctx context.Context) (BackupSettings, error)
//	// PrepareCreate dựng Plan "backup.create" cho một site (active hoặc
//	// trashed; trashed thì nguồn là site.TrashDir). Một bước:
//	// steps.BackupArchive{Layout, Src, Dir: site.BackupDir, PHPVersion}.
//	// Chặn đầy đĩa (mục 6.5) ở đây, trước khi dựng Plan: đo bằng
//	// steps.MeasureBackup, không đủ chỗ thì từ chối CodeDiskSpaceLow kèm số
//	// đo được. Execute xong (after): AddBackup một dòng (Kind full, Reason,
//	// Path/Size/SHA256/WPVersion lấy từ bước sau Apply, JobID), rồi prune.
//	// Tên file chọn LÚC APPLY, không lúc dựng Plan: Plan qua Planner (MCP)
//	// được dựng lại lúc duyệt và so dấu vân tay; tên theo giờ dựng Plan thì
//	// không bao giờ khớp.
//	func (b *Backups) PrepareCreate(ctx context.Context, d validate.Domain, reason BackupReason) (*Planned, error)
//	// CreateAll: mỗi site active một job PrepareCreate(ReasonSchedule hoặc
//	// ReasonManual), site lỗi thì ghi vào kết quả rồi đi tiếp; cuối cùng
//	// SnapshotTPANDB. Dùng cho `tpan backup create --all` và lịch backup.
//	func (b *Backups) CreateAll(ctx context.Context, reason BackupReason, onEvent func(engine.Event)) ([]BackupResult, error)
//	// SnapshotTPANDB chép tpan.db bằng VACUUM INTO vào
//	// site.TPANDBBackupDir/tpan-<YYYYMMDD-HHMMSS>.db (0600, thư mục 0700),
//	// giữ DefaultBackupKeep bản mới nhất. Trả đường dẫn file mới.
//	func (b *Backups) SnapshotTPANDB(ctx context.Context) (string, error)
//	// PrepareRestore dựng Plan "backup.restore" vào chính site đó (mục 6.5).
//	// id rỗng: bản full mới nhất của site. Từ chối: site_not_found,
//	// site_trashed (khôi phục khỏi thùng rác trước), backup_not_found (id
//	// không có, khác domain, hoặc không phải full), backup_corrupt (sha256
//	// file không khớp dòng), CodeDiskSpaceLow (cho bản tự backup trước).
//	// Các bước, đúng thứ tự:
//	//  1. steps.BackupArchive của hiện trạng (ReasonBeforeRestore).
//	//  2. steps.BackupExtract{Layout, Archive, SHA256, Dir: site.RestoreDir}.
//	//  3. steps.MoveTree public hiện tại → RestoreDir/old-public;
//	//     steps.MoveTree RestoreDir/public → l.Public.
//	//  4. steps.MariaDBDump{DB: l.DB, Path: RestoreDir/db-before.sql};
//	//     steps.DBImport{Layout, File: RestoreDir/db.sql, UndoFile: RestoreDir/db-before.sql}.
//	// Verify: siteChecks (bản SSL nếu site.SSL). Job thành công (after):
//	// AddBackup cho bản tự backup ở bước 1, gỡ RestoreDir (sys.Remove theo
//	// cây, chỉ dưới site.RestoreRoot), prune (không bao giờ xoá bản vừa dùng
//	// để khôi phục). Gỡ RestoreDir sau kiểm chứng chứ không trong Plan: kiểm
//	// chứng lỗi thì engine hoàn tác, mà hoàn tác cần old-public.
//	func (b *Backups) PrepareRestore(ctx context.Context, d validate.Domain, id string) (*Planned, error)
//	// PrepareSettings dựng Plan "backup.settings": at = "HH:MM" (24 giờ) thì
//	// ghi site.BackupCronFile bằng steps.File (templates.BackupCron(at)); at
//	// rỗng thì steps.Absent file đó. keep 1..MaxBackupKeep, ngoài khoảng thì
//	// từ chối "backup_keep_invalid"; at sai dạng: "backup_at_invalid". Job
//	// thành công (after): ghi hai setting.
//	func (b *Backups) PrepareSettings(ctx context.Context, keep int, at string) (*Planned, error)
//
// Prune (nội bộ backup.go): giữ Keep bản full mới nhất mỗi domain và Keep bản
// sql mới nhất mỗi domain; KHÔNG BAO GIỜ xoá bản ReasonBeforePurge, và không
// xoá bản đang được dùng trong job vừa chạy. Xoá file bằng sys.Remove rồi
// DeleteBackup; file không còn thì vẫn xoá dòng.
//
// ---------------------------------------------------------------- dbops.go (Backups, mục 6.9)
//
//	// PrepareDBPassword dựng Plan "db.passwd": steps.DBPassword{Layout,
//	// Password: mật khẩu mới 32 ký tự (sitePassword)}. Chỉ site active.
//	// Verify: checks của chặng DB và đầu cuối của site (trong siteChecks).
//	func (b *Backups) PrepareDBPassword(ctx context.Context, d validate.Domain) (*Planned, error)
//	// PrepareDBDump dựng Plan "db.dump": chặn đầy đĩa theo cỡ DB, rồi
//	// steps.MariaDBDump{DB: l.DB, Path: site.BackupDir/<domain>-db-<giờ>.sql}.
//	// after: AddBackup (Kind sql, ReasonManual, sha256 và cỡ đọc sau khi xuất).
//	// Site active hoặc trashed (DB của site trong thùng rác vẫn còn).
//	func (b *Backups) PrepareDBDump(ctx context.Context, d validate.Domain) (*Planned, error)
//	// PrepareDBImport dựng Plan "db.import" từ một bản xuất ĐÃ GHI trong
//	// bảng backups (Kind sql, cùng domain, sha256 khớp file; không thì
//	// backup_not_found, backup_corrupt). Luôn tự xuất bản hiện tại trước
//	// (MariaDBDump, ReasonBeforeImport) rồi steps.DBImport{File, UndoFile:
//	// bản vừa xuất}. Chỉ site active. after: AddBackup cho bản vừa xuất.
//	func (b *Backups) PrepareDBImport(ctx context.Context, d validate.Domain, id string) (*Planned, error)
//
// ---------------------------------------------------------------- cron.go (Cron, mục 6.10)
//
//	// List trả lịch cron của mọi site (active và trashed; trashed thì
//	// Enabled=false vì file cron đã gỡ) kèm lần chạy gần nhất (CronStore), và
//	// lịch backup (Backups.Settings).
//	func (c *Cron) List(ctx context.Context) (CronOverview, error)
//	// Run chạy `wp cron event run --due-now --quiet` dưới user của site (như
//	// mọi lệnh wp của tpan: AsUser, --path), đo thời gian, ghi một dòng
//	// SaveCronRun (output: 2 KB cuối). Site không active: ghi "skipped" và
//	// trả nil (root gọi runuser bỏ qua hạn tài khoản của user bị khoá, nên
//	// chặn ở đây). Site không có trong tpan: từ chối site_not_found, không
//	// ghi dòng nào (dòng cron sai thấy được trong log của cron). KHÔNG lấy khoá toàn cục và KHÔNG tạo job:
//	// chạy 5 phút một lần mỗi site, giữ khoá thì người bấm trên panel bị 409.
//	func (c *Cron) Run(ctx context.Context, d validate.Domain) (CronRun, error)
//	// siteCronSteps: hai bước cron của một site cho Plan tạo site (và
//	// khôi phục từ thùng rác): steps.WPConstant{DISABLE_WP_CRON = true} rồi
//	// steps.File ghi site.CronFile(l) từ templates.SiteCron(l) (root 0644).
//	func siteCronSteps(l site.Layout) ([]engine.Step, error)
//	// siteCronAbsent: steps.Absent của site.CronFile(l) (đưa vào thùng rác,
//	// xoá hẳn).
//	func siteCronAbsent(l site.Layout) engine.Step

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
)

// Kiểu dòng của bảng backups nằm ở gói lá site (store không import được core:
// test của core import store). Bí danh để mã của core, CLI, API gọi tên core.Backup.
type (
	BackupKind   = site.BackupKind
	BackupReason = site.BackupReason
	Backup       = site.Backup
)

const (
	BackupFull          = site.BackupFull
	BackupSQL           = site.BackupSQL
	ReasonManual        = site.ReasonManual
	ReasonSchedule      = site.ReasonSchedule
	ReasonBeforeRestore = site.ReasonBeforeRestore
	ReasonBeforePurge   = site.ReasonBeforePurge
	ReasonBeforeImport  = site.ReasonBeforeImport
)

// BackupStore là phần của store mà backup cần. *store.Store thoả giao diện này.
type BackupStore interface {
	// AddBackup thêm một dòng; ID rỗng thì store sinh 16 hex ngẫu nhiên.
	// Trả dòng đã ghi.
	AddBackup(ctx context.Context, b Backup) (Backup, error)
	// Backup trả một dòng theo id; ok=false nếu không có.
	Backup(ctx context.Context, id string) (b Backup, ok bool, err error)
	// Backups trả các dòng của domain (rỗng: mọi domain), mới nhất trước.
	Backups(ctx context.Context, domain string) ([]Backup, error)
	// DeleteBackup xoá dòng; không có thì không lỗi.
	DeleteBackup(ctx context.Context, id string) error
	// ForgetBackupLocal (M18): file của dòng đã bị dọn khỏi máy. Dòng đã lên
	// kho (remote='stored') thì thành local=0; không thì xoá dòng. Một
	// transaction có điều kiện. deleted: dòng đã bị xoá.
	ForgetBackupLocal(ctx context.Context, id string) (deleted bool, err error)
	// SnapshotDB chép tpan.db đang mở ra path bằng `VACUUM INTO` (bản sao
	// nhất quán dù đang có người ghi). path chưa được tồn tại.
	SnapshotDB(ctx context.Context, path string) error
}

// Setting của backup.
const (
	SettingBackupKeep = "backup.keep" // số bản giữ mỗi site, chuỗi thập phân
	SettingBackupAt   = "backup.at"   // giờ backup hằng ngày "HH:MM"; trống là tắt
	DefaultBackupKeep = 3             // mục 6.5
	MaxBackupKeep     = 30
)

// Mã từ chối mới của M9.
const (
	// CodeDiskSpaceLow: không đủ chỗ trống theo luật chặn đầy đĩa (mục 6.5).
	CodeDiskSpaceLow = "disk_space_low"
	// CodeBackupRequired: nâng MariaDB khi còn site active chưa có bản full
	// trong BackupFreshFor (M12 + M9).
	CodeBackupRequired = "backup_required"
)

// BackupFreshFor: bản full cũ hơn chừng này không tính là "đã backup" trước
// khi nâng MariaDB.
const BackupFreshFor = 24 * time.Hour

// BackupSettings là cấu hình backup.
type BackupSettings struct {
	Keep int `json:"keep"`
	// At là giờ backup hằng ngày "HH:MM" theo giờ máy; rỗng là tắt lịch.
	At string `json:"at"`
}

// BackupResult là kết quả một site trong CreateAll.
type BackupResult struct {
	Domain string        `json:"domain"`
	Report engine.Report `json:"report"`
	// Error: không dựng hay không chạy được Plan (kể cả từ chối vì đĩa).
	Error string `json:"error,omitempty"`
}

// Backups gom các ca sử dụng backup và thao tác DB. Dựng mới cho mỗi lệnh.
type Backups struct {
	Sites *Sites
	Store BackupStore
	// Now nil thì dùng Sites.now.
	Now func() time.Time
	// DBs (M33) khác nil thì CreateAll sao lưu cả các DB rời, sau các site và
	// trước khi chép tpan.db (bản sao tpan.db phải có dòng của các bản vừa ghi).
	// Chỉ dòng lệnh đặt trường này: lịch backup hằng ngày chạy qua dòng lệnh.
	DBs *DBs
}

// Kiểu dòng của bảng cron_runs nằm ở gói lá site (như Backup).
type (
	CronStatus = site.CronStatus
	CronRun    = site.CronRun
)

const (
	CronOK      = site.CronOK
	CronFail    = site.CronFail
	CronSkipped = site.CronSkipped
)

// CronStore là phần của store mà cron cần. *store.Store thoả giao diện này.
type CronStore interface {
	// SaveCronRun ghi đè dòng của domain.
	SaveCronRun(ctx context.Context, r CronRun) error
	// CronRuns trả mọi dòng, sắp theo domain.
	CronRuns(ctx context.Context) ([]CronRun, error)
}

// CronEvery là nhịp cron WordPress của mọi site (mục 6.10: mặc định 5 phút).
const CronEvery = "*/5 * * * *"

// CronEntry là một dòng của màn Cron.
type CronEntry struct {
	Domain   string `json:"domain"`
	File     string `json:"file"`     // site.CronFile
	Schedule string `json:"schedule"` // CronEvery
	// Command là lệnh cron chạy, để hiện (không bao giờ đem đi chạy).
	Command string `json:"command"`
	// Enabled: file cron có trên máy (site active và đã có cron).
	Enabled bool     `json:"enabled"`
	Last    *CronRun `json:"last,omitempty"`
}

// CronOverview là nội dung màn Cron.
type CronOverview struct {
	Sites  []CronEntry    `json:"sites"`
	Backup BackupSettings `json:"backup"`
	// BackupFile là site.BackupCronFile khi lịch backup bật, rỗng khi tắt.
	BackupFile string `json:"backup_file,omitempty"`
}

// Cron gom ca sử dụng của màn Cron và `tpan cron run`.
type Cron struct {
	Sites   *Sites
	Runs    CronStore
	Backups *Backups
	// Now nil thì dùng Sites.now.
	Now func() time.Time
}
