package core

// Hợp đồng M18 (đợt 0): sao lưu ra kho S3, khôi phục từ kho, gói panel,
// cloud-init (mục 6.16 của docs/KE-HOACH.md; thiết kế ở
// docs/thay-doi/M18-thiet-ke.md, mục 3). CLI, API, health dựa vào các kiểu và
// chữ ký ở đây.
//
// remote_target.go (đợt 0, phiên chính) đọc đích, khoá và dựng kho. Thợ đợt 2
// viết thân ở:
//
// ---------------------------------------------------------------- remote.go
//
//	// Target là phần xem của đích (mục 3.3): không trường nào mang khoá.
//	func (r *Remote) Target(ctx context.Context) (TargetView, error)
//	// PrepareSetTarget dựng Plan "backup.target.set" (mục 3.3, sáu bước).
//	// Điều kiện đầu (cả lúc xem trước): kiểm mọi trường (s3.Check*), dựng
//	// kho, Check; lỗi thành RefusedError mã s3_* (s3Refuse). in.GenerateKey
//	// (chỉ CLI) và chưa có khoá sao lưu: thêm SecretFile với khoá mới, đặt
//	// TargetSet.NewKey. after: setting, nối lại khi TargetID đổi (TargetSet.Result).
//	func (r *Remote) PrepareSetTarget(ctx context.Context, in TargetInput) (*TargetSet, error)
//	// PrepareRemoveTarget dựng "backup.target.rm"; chưa đặt đích: s3_not_configured.
//	func (r *Remote) PrepareRemoveTarget(ctx context.Context) (*Planned, error)
//	// PrepareTestTarget dựng "backup.target.test" (một S3Probe).
//	func (r *Remote) PrepareTestTarget(ctx context.Context) (*Planned, error)
//	// PrepareNewKey dựng "backup.key.new" (chỉ CLI): một SecretFile với khoá
//	// mới; after: s3.panel_due = 1.
//	func (r *Remote) PrepareNewKey(ctx context.Context) (*KeyNew, error)
//	// Sync là một lượt đẩy (mục 3.5, mười việc theo thứ tự).
//	func (r *Remote) Sync(ctx context.Context, opt SyncOptions, onEvent func(engine.Event)) (SyncResult, error)
//	// PreparePush dựng "backup.push" cho một bản (xem trước của API, và dùng
//	// trong Sync). Từ chối: s3_not_configured, backup_not_found (không có,
//	// không phải full, local=0).
//	func (r *Remote) PreparePush(ctx context.Context, id string) (*Planned, error)
//	// PreparePull dựng "backup.pull" (một S3Pull; after: SetBackupLocal).
//	// Bản không cần tải về: backup_already_local; ở đích khác:
//	// backup_not_on_machine.
//	func (r *Remote) PreparePull(ctx context.Context, id string) (*Planned, error)
//	// Pull lấy khoá lượt đẩy (chờ như Sync{Wait:true}), chạy PreparePull,
//	// nhả khoá sau khi after đã chạy. Không giữ khoá job của máy (B10).
//	func (r *Remote) Pull(ctx context.Context, id string, onEvent func(engine.Event)) (engine.Report, error)
//	// ListRemote liệt kê trên kho, ghép với bảng backups (mục 3.6).
//	func (r *Remote) ListRemote(ctx context.Context) (RemoteListing, error)
//	// State là đầu vào của phép kiểm tpan.s3:fresh (chỉ đọc DB và settings).
//	func (r *Remote) State(ctx context.Context) (checks.S3State, error)
//	// Probe là đầu vào của phép kiểm tpan.s3:target (Check, rồi Head bản
//	// stored mới nhất của từng site active).
//	func (r *Remote) Probe(ctx context.Context) (checks.S3Probe, error)
//	// remoteSurvives là luật duy nhất "bản này sống qua lần dọn kho" (mục 3.5),
//	// dùng cho cả chọn bản cần đẩy lẫn pruneRemote.
//	func remoteSurvives(row Backup, rows []Backup, targetID string, keep int) bool
//
// ---------------------------------------------------------------- panel.go
//
//	// PreparePanel dựng "backup.panel": steps.PanelBundle rồi steps.S3Push
//	// {From: bundle}. Chưa có khoá sao lưu: backup_key_missing. Chủ job admin.
//	func (r *Remote) PreparePanel(ctx context.Context) (*Planned, error)
//
// ---------------------------------------------------------------- backup.go (sửa luồng M9)
//
//   - prune (dọn trên máy): chỉ đếm dòng Local; bản bị dọn: xoá file rồi
//     Store.ForgetBackupLocal; xoá thêm dòng Local=false mà Remote != stored.
//   - fullCount: chỉ đếm dòng Local.
//   - PrepareRestore: bản cần tải về (NeedsPull) hoặc Local=false ở đích
//     khác thì từ chối CodeBackupNotOnMachine kèm chỉ đường (mục 3.6). id
//     rỗng: bản full mới nhất có lý do manual hoặc schedule, không có thì bản
//     full mới nhất bất kỳ.
//
// ---------------------------------------------------------------- cloudinit.go
//
//	// Render sinh user-data chế độ site mới (mục 3.9). Mật khẩu quản trị mới
//	// sinh bằng sitePassword, chỉ bản băm (c.Hash) vào user-data.
//	func (c *CloudInit) Render(ctx context.Context, in CloudInitInput) (CloudInitResult, error)

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// Khoá trong bảng settings của M18 (mục 3.2). Không khoá nào là bí mật. Giá
// trị "" là chưa đặt (Settings không có hàm xoá).
const (
	SettingS3Endpoint       = "s3.endpoint" // rỗng: chưa đặt đích
	SettingS3Region         = "s3.region"
	SettingS3Bucket         = "s3.bucket"
	SettingS3Prefix         = "s3.prefix"
	SettingS3PathStyle      = "s3.path_style" // "1" (mặc định) hoặc "0"
	SettingS3Keep           = "s3.keep"       // số bản giữ trên kho mỗi site
	SettingS3TargetError    = "s3.target_error"
	SettingS3TargetFailures = "s3.target_failures"
	SettingS3RetryAt        = "s3.retry_at" // RFC 3339
	SettingS3PanelLast      = "s3.panel_last"
	SettingS3PanelError     = "s3.panel_error"
	SettingS3PanelDue       = "s3.panel_due" // "1": cần gói panel mới
	// Nhịp lùi riêng của gói panel (R3 của bản soát hẹp): gói hỏng vì lý do
	// bất kỳ thì lùi theo RemoteBackoff, để không chụp lại tpan.db mỗi 2 phút.
	SettingS3PanelFailures = "s3.panel_failures"
	SettingS3PanelRetryAt  = "s3.panel_retry_at" // RFC 3339
	// SettingS3RelinkDue "1": đích vừa đổi, còn phải nối lại các dòng stored
	// ở đích cũ (R5): nối lại hỏng thì chạy lại `target set` sẽ nối tiếp.
	SettingS3RelinkDue = "s3.relink_due"
)

// Mã từ chối mới của M18 (mục 3.14). Mã s3_* nằm ở gói s3 (s3.Code*).
const (
	CodeBackupNotOnMachine = "backup_not_on_machine"
	CodeBackupAlreadyLocal = "backup_already_local"
	CodeBackupKeyMissing   = "backup_key_missing"
	CodeCloudInitInvalid   = "cloudinit_invalid"
	CodeCloudInitTooLarge  = "cloudinit_too_large"
	CodeCloudInitNoConfig  = "cloudinit_no_config"
)

// Thời gian của lượt đẩy (mục 3.5).
const (
	// RemoteLockWait: lệnh gõ tay, cron chờ khoá lượt đẩy tối đa chừng này.
	RemoteLockWait = 30 * time.Minute
	// RemoteMaxTries: đẩy tự động bỏ qua dòng có remote_tries từ số này.
	RemoteMaxTries = 5
	// PanelMaxAge: gói panel cũ hơn chừng này thì lượt đẩy làm gói mới.
	PanelMaxAge = 24 * time.Hour
	// PanelKeepLocal: số gói panel giữ trên máy.
	PanelKeepLocal = 3
)

// RemoteBackoff là nhịp lùi sau lỗi của đích theo số lượt hỏng liên tiếp
// (1, 2, 3, từ 4): 10 phút, 30 phút, 1 giờ, 6 giờ.
var RemoteBackoff = []time.Duration{10 * time.Minute, 30 * time.Minute, time.Hour, 6 * time.Hour}

// RemoteStore là phần của store mà M18 cần (mục 3.2). *store.Store thoả giao
// diện này (thợ đợt 1 viết các hàm ở internal/store/remote.go). Mọi hàm đổi
// trạng thái là một transaction có điều kiện.
type RemoteStore interface {
	BackupStore
	MarkBackupStored(ctx context.Context, id, key, targetID string, at time.Time) (ok bool, err error)
	MarkBackupPushFailed(ctx context.Context, id, targetID, msg string, countTry bool) error
	ResetBackupTries(ctx context.Context, id string) error
	SetBackupLocal(ctx context.Context, id string) (ok bool, err error)
	ForgetBackupRemote(ctx context.Context, id string) (deleted bool, err error)
	SetBackupRemoteTarget(ctx context.Context, id, targetID string) error
	AppliedMigrations(ctx context.Context) ([]int, error)
	// AbortStaleJobs đóng job các loại kinds còn running (R12): lượt đẩy gọi
	// lúc đang giữ khoá lượt đẩy, nên job đẩy, tải, gói nào còn running là của
	// tiến trình đã chết.
	AbortStaleJobs(ctx context.Context, kinds []string, reason string) (int, error)
}

// Remote gom các ca sử dụng của kho S3. Dựng mới cho mỗi lệnh hay request.
type Remote struct {
	Sys      system.System
	Settings Settings
	// Store là bảng backups và các hàm của M18. nil chỉ hợp lệ khi xem trước
	// trên máy chưa có tpan.db.
	Store RemoteStore
	// Sites là bảng sites (State, Probe cần danh sách site active). *store.Store thoả.
	Sites SiteStore
	// Journal nhận job; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
	// Snapshot chép tpan.db đang mở (store.SnapshotDB), cho gói panel.
	Snapshot func(ctx context.Context, path string) error
	Hostname string
	Version  string
	// NewStore dựng kho từ cấu hình; nil: client thật (s3.New). Test đặt hàm
	// trả *s3.Mem.
	NewStore func(cfg s3.Config) (steps.ObjectStore, error)
	// Lock là khoá lượt đẩy; nil: system.FileLock{Path: system.DefaultRemoteLockPath}.
	Lock system.Locker
	// LockWait: chờ khoá khi SyncOptions.Wait; 0: RemoteLockWait.
	LockWait time.Duration
	// Now nil thì dùng time.Now.
	Now func() time.Time
}

// TargetConfig là phần không bí mật của đích, đọc từ settings.
type TargetConfig struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	PathStyle bool
	Keep      int
	// ID là s3.TargetID(Endpoint, Bucket, Prefix).
	ID string
}

// PanelInfo là gói panel mới nhất đã lên kho (setting s3.panel_last).
type PanelInfo struct {
	Name   string    `json:"name"`
	At     time.Time `json:"at"`
	SHA256 string    `json:"sha256"`
}

// TargetView là phần xem của đích (mục 3.3). Không trường nào mang khoá bí
// mật, kể cả dạng che.
type TargetView struct {
	Configured bool   `json:"configured"`
	Endpoint   string `json:"endpoint,omitempty"`
	Region     string `json:"region,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
	PathStyle  bool   `json:"path_style"`
	Keep       int    `json:"keep,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	HasCA      bool   `json:"has_ca"`
	// AccessKeyTail: bốn ký tự cuối khi access key dài từ 12 ký tự
	// (s3.AccessKeyTail); rỗng thì người hiển thị in AccessKeyLen.
	AccessKeyTail string `json:"access_key_tail,omitempty"`
	AccessKeyLen  int    `json:"access_key_len,omitempty"`
	HasBackupKey  bool   `json:"has_backup_key"`
	// KeyFingerprint: agebox.Fingerprint của khoá sao lưu.
	KeyFingerprint string     `json:"key_fingerprint,omitempty"`
	PanelLast      *PanelInfo `json:"panel_last,omitempty"`
	PanelError     string     `json:"panel_error,omitempty"`
	PanelDue       bool       `json:"panel_due"`
	TargetError    string     `json:"target_error,omitempty"`
	RetryAt        time.Time  `json:"retry_at,omitzero"`
	// Đếm trên bảng backups: chỉ còn trên kho (đích này), stored ở đích
	// khác, đẩy lỗi.
	RemoteOnly  int `json:"remote_only"`
	OtherTarget int `json:"other_target"`
	Failed      int `json:"failed"`
}

// CAMode là việc làm với chứng chỉ gốc riêng khi đặt đích.
type CAMode int

const (
	CAKeep CAMode = iota // giữ như đang có
	CASet                // đặt CAPEM
	CANone               // bỏ CA riêng
)

// TargetInput là đầu vào của PrepareSetTarget.
type TargetInput struct {
	Endpoint  string
	Region    string // rỗng: s3.DefaultRegion
	Bucket    string
	Prefix    string // rỗng: s3.DefaultPrefix(Hostname)
	PathStyle bool
	Keep      int // 0: s3.DefaultKeep
	CA        CAMode
	CAPEM     []byte
	// KeepKeys: giữ hai khoá đang lưu (API gửi rỗng; CLI không có
	// --keys-stdin và không có terminal). Chưa có khoá nào: s3_keys_invalid.
	KeepKeys  bool
	AccessKey string
	SecretKey system.Secret
	// GenerateKey (chỉ CLI): chưa có khoá sao lưu thì sinh và ghi.
	GenerateKey bool
}

// TargetSetResult là kết quả nối lại sau khi đặt đích (điền trong after).
type TargetSetResult struct {
	Relinked    int `json:"relinked"`
	OtherTarget int `json:"other_target"`
}

// TargetSet là Plan đặt đích kèm khoá sao lưu vừa sinh (nếu có).
type TargetSet struct {
	*Planned
	// NewKey khác rỗng khi Plan có bước ghi khoá sao lưu mới: CLI in MỘT lần
	// sau khi job thành công. Không bao giờ ra JSON hay API.
	NewKey system.Secret
	// Result được after điền khi job thành công.
	Result *TargetSetResult
}

// KeyNew là Plan "backup.key.new" kèm khoá mới (CLI in một lần sau khi job thành công).
type KeyNew struct {
	*Planned
	Key system.Secret
}

// SyncOptions là lựa chọn của một lượt đẩy (mục 3.5).
type SyncOptions struct {
	// Wait: chờ khoá lượt đẩy tối đa LockWait (CLI, cron); false: bận thì
	// thôi (bộ đẩy nền).
	Wait bool
	// Retry (lệnh gõ tay): bỏ nhịp lùi, gồm cả dòng remote_tries ≥
	// RemoteMaxTries (đặt lại về 0).
	Retry bool
	// Only: chỉ các id này (tpan backup push <id>, hàng chờ của API); rỗng:
	// theo luật chọn.
	Only []string
	// PanelDue: đặt s3.panel_due = 1 trước khi chạy (backup create --all).
	PanelDue bool
}

// SyncFailure là một bản đẩy lỗi trong lượt.
type SyncFailure struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Error  string `json:"error"`
}

// SyncResult là kết quả của một lượt đẩy.
type SyncResult struct {
	// Ran false: không có đích, đang lùi, hoặc khoá bận (Skipped nói lý do).
	Ran     bool          `json:"ran"`
	Skipped string        `json:"skipped,omitempty"`
	Pushed  []string      `json:"pushed"`
	Failed  []SyncFailure `json:"failed"`
	// Panel: tên gói panel vừa lên kho trong lượt này.
	Panel       string          `json:"panel,omitempty"`
	PanelError  string          `json:"panel_error,omitempty"`
	TargetError string          `json:"target_error,omitempty"`
	Reports     []engine.Report `json:"reports"`
}

// RemoteObject là một object trên kho, ghép với bảng backups.
type RemoteObject struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// BackupID, Domain rỗng: kho có mà tpan.db không biết.
	BackupID string `json:"backup_id,omitempty"`
	Domain   string `json:"domain,omitempty"`
}

// RemoteListing là kết quả ListRemote.
type RemoteListing struct {
	Objects []RemoteObject `json:"objects"`
	// Missing: dòng stored ở đích này mà kho không còn object.
	Missing []Backup `json:"missing"`
	// OtherTarget: số dòng stored ở đích khác.
	OtherTarget int `json:"other_target"`
}

// CloudInit gom ca sử dụng sinh user-data (mục 3.9).
type CloudInit struct {
	Sys system.System
	// Version là buildinfo.Version của bản đang chạy (tên bản mặc định của Base).
	Version string
	// Hash băm mật khẩu quản trị (store.HashPassword; core không import store).
	Hash func(pw string) (string, error)
}

// CloudInitInput là đầu vào của Render.
type CloudInitInput struct {
	Web     WebServer
	Private bool
	// Sites nil: máy mới không có site nào.
	Sites *SitesFile
	// Base rỗng: cloudinit.DefaultBaseFormat với Version.
	Base string
	// SumAMD64, SumARM64: sha256 của hai file chạy. Rỗng thì lấy từ Sums,
	// rồi (khi AllowFetch và Base là https://) tải <Base>/SHA256SUMS.
	SumAMD64, SumARM64 string
	Sums               []byte
	// AllowFetch: được tải SHA256SUMS (CLI). API luôn false (mục 3.11).
	AllowFetch bool
	MaxBytes   int // 0: cloudinit.DefaultMaxBytes
}

// CloudInitResult là kết quả Render.
type CloudInitResult struct {
	UserData []byte `json:"-"`
	Bytes    int    `json:"bytes"`
	// AdminPassword: mật khẩu quản trị của máy mới, in hay trả ĐÚNG MỘT LẦN
	// (CLI: stderr; API: một trường của câu trả lời). JSON của Secret là "••••".
	AdminPassword system.Secret `json:"admin_password"`
}
