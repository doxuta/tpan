// Package account giữ các kiểu của M13 (mục 6.12 của docs/KE-HOACH.md): user
// của TPAN, vai, gói, hạn mức, trạng thái tạm khoá. Là gói lá (chỉ import
// validate, i18n) để store, core, server cùng dùng mà không import nhau.
package account

import "time"

// Role là vai của một user TPAN.
type Role string

const (
	// RoleAdmin thấy và làm mọi thứ (như trước M13). Có thể có nhiều admin.
	RoleAdmin Role = "admin"
	// RoleUser là khách hosting: chỉ thấy và làm trên đối tượng của chính mình.
	RoleUser Role = "user"
)

// Valid cho biết r là một vai đã biết.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleUser }

// Status là trạng thái của một user.
type Status string

const (
	Active    Status = "active"
	Suspended Status = "suspended"
)

// Suspension nói một site, DB của site hay DB rời đang bị tạm khoá vì sao.
// Cần nguồn chứ không chỉ có/không: mở khoá user chỉ mở lại đúng thứ bị khoá
// VÌ user bị khoá; thứ đã bị khoá riêng từ trước thì giữ khoá (mục 6.12).
type Suspension string

const (
	// NotSuspended: đang chạy bình thường.
	NotSuspended Suspension = ""
	// SuspendSelf: admin khoá riêng đối tượng này.
	SuspendSelf Suspension = "self"
	// SuspendByUser: bị khoá cùng lúc khoá user chủ của nó.
	SuspendByUser Suspension = "user"
)

// Valid cho biết s là một giá trị đã biết.
func (s Suspension) Valid() bool {
	return s == NotSuspended || s == SuspendSelf || s == SuspendByUser
}

// Tên có sẵn.
const (
	// AdminName là tài khoản quản trị có từ trước M13; migration 1300 chuyển
	// hash mật khẩu cũ sang user này. Đăng nhập để trống tên thì hiểu là nó.
	AdminName = "admin"
	// DefaultPackage là gói không giới hạn, có sẵn, không xoá được.
	DefaultPackage = "default"
)

// User là một dòng của bảng users. Không có trường mật khẩu: hash chỉ đi qua
// các hàm riêng của store (PasswordHashOf, SetUserPassword), không bao giờ
// nằm trong kiểu được trả ra API.
type User struct {
	Name     string    `json:"name"`
	FullName string    `json:"full_name"`
	Email    string    `json:"email"`
	Role     Role      `json:"role"`
	Package  string    `json:"package"`
	Status   Status    `json:"status"`
	Created  time.Time `json:"created"`
	// SuspendedAt và SuspendReason chỉ có khi Status là Suspended.
	SuspendedAt   time.Time `json:"suspended_at,omitzero"`
	SuspendReason string    `json:"suspend_reason,omitempty"`
}

// Package là một gói hạn mức. 0 ở mọi trường giới hạn là không giới hạn.
type Package struct {
	Name string `json:"name"`
	// MaxSites: số site tối đa (site trong thùng rác vẫn tính: nó còn giữ DB và file).
	MaxSites int `json:"max_sites"`
	// MaxDBs: số DB rời tối đa; không tính DB đi kèm site.
	MaxDBs int `json:"max_dbs"`
	// DiskMB: tổng thư mục site + DB, đo mỗi giờ. Vượt là cảnh báo và chặn
	// tạo mới, không xoá hay khoá gì tự động.
	DiskMB int `json:"disk_mb"`
	// MaxBackups: số bản backup đầy đủ tối đa của MỖI site.
	MaxBackups int `json:"max_backups"`

	// M17 (mục 3.1 của docs/thay-doi/M17-thiet-ke.md): cờ lồng ("muốn" đưa site
	// của gói vào lồng) và các trần áp lên slice của mỗi site trong lồng. 0 là
	// không giới hạn; riêng PHPWorkers: 0 là DefaultPHPWorkers.
	Cage       bool `json:"cage"`
	CPUPercent int  `json:"cpu_percent"` // % của một nhân
	RAMMB      int  `json:"ram_mb"`      // MiB
	Tasks      int  `json:"tasks"`       // tiến trình và luồng
	IOReadMB   int  `json:"io_read_mb"`  // MB/giây
	IOWriteMB  int  `json:"io_write_mb"` // MB/giây
	PHPWorkers int  `json:"php_workers"` // pm.max_children

	Created time.Time `json:"created"`
}

// DefaultPHPWorkers là pm.max_children khi gói để 0: đúng con số của pool PHP
// ngoài lồng (mục 5 của docs/KE-HOACH.md), vì PHP-FPM bắt buộc có một con số.
const DefaultPHPWorkers = 5

// Limits là các trần của một site trong lồng (lấy từ gói của chủ site).
type Limits struct {
	CPUPercent int `json:"cpu_percent"`
	RAMMB      int `json:"ram_mb"`
	Tasks      int `json:"tasks"`
	IOReadMB   int `json:"io_read_mb"`
	IOWriteMB  int `json:"io_write_mb"`
	PHPWorkers int `json:"php_workers"`
}

// Limits trả các trần của gói.
func (p Package) Limits() Limits {
	return Limits{CPUPercent: p.CPUPercent, RAMMB: p.RAMMB, Tasks: p.Tasks,
		IOReadMB: p.IOReadMB, IOWriteMB: p.IOWriteMB, PHPWorkers: p.PHPWorkers}
}

// Workers là pm.max_children thật sẽ dùng: PHPWorkers, hoặc DefaultPHPWorkers khi 0.
func (l Limits) Workers() int {
	if l.PHPWorkers > 0 {
		return l.PHPWorkers
	}
	return DefaultPHPWorkers
}

// Any cho biết có trần nào khác 0 (kể cả số worker): gói như vậy phải bật lồng.
func (l Limits) Any() bool { return l != Limits{} }

// Usage là phần một user đang dùng, để so với gói.
type Usage struct {
	Sites int `json:"sites"`
	DBs   int `json:"dbs"`
	// DiskBytes là lần đo gần nhất; -1 khi chưa đo lần nào.
	DiskBytes    int64     `json:"disk_bytes"`
	DiskMeasured time.Time `json:"disk_measured,omitzero"`
}

// Identity là người đang gọi một thao tác: phiên đăng nhập trên web, hoặc
// root ở dòng lệnh và MCP (luôn là admin, mục 6.12).
type Identity struct {
	// Name là user đang đăng nhập. Rỗng ở dòng lệnh và MCP (chạy bằng root).
	Name string `json:"name"`
	Role Role   `json:"role"`
	// ViewedBy khác rỗng khi admin đang "xem như user này": Name, Role ở trên
	// đã là của user được xem, phiên chỉ đọc, ViewedBy giữ tên admin thật.
	ViewedBy string `json:"viewed_by,omitempty"`
}

// IsAdmin cho biết người gọi có vai admin.
func (i Identity) IsAdmin() bool { return i.Role == RoleAdmin }

// ReadOnly: phiên "xem như" không được thay đổi gì.
func (i Identity) ReadOnly() bool { return i.ViewedBy != "" }

// Root là danh tính của dòng lệnh, MCP và bộ hẹn giờ: admin, không tên.
var Root = Identity{Role: RoleAdmin}

// DBStatus là trạng thái của một DB rời.
type DBStatus string

const (
	DBActive  DBStatus = "active"
	DBTrashed DBStatus = "trashed" // đã xuất .sql vào thùng rác và DROP; user DB còn, đang khoá
)

// Database là một DB rời (không gắn với site), một dòng của bảng databases.
// Tên DB và tên user DB trùng nhau: "<owner>_<tên>" (validate.StandaloneDBName).
type Database struct {
	Name        string     `json:"name"`
	Owner       string     `json:"owner"`
	Charset     string     `json:"charset"`
	Collation   string     `json:"collation"`
	Note        string     `json:"note"`
	Status      DBStatus   `json:"status"`
	Suspended   Suspension `json:"suspended,omitempty"`
	SuspendedAt time.Time  `json:"suspended_at,omitzero"`
	Created     time.Time  `json:"created"`
	TrashedAt   time.Time  `json:"trashed_at,omitzero"`
	PurgeAt     time.Time  `json:"purge_at,omitzero"`
}

// DBTrashDump là file .sql của DB rời đã vào thùng rác (mục 6.12, "Xoá DB rời").
func DBTrashDump(name string) string { return "/var/lib/tpan/trash/db/" + name + ".sql" }
