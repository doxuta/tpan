package server

// Hợp đồng M3: API HTTP của tpan (mục 9, 10, 11 của docs/KE-HOACH.md).
//
// Bảng đường dẫn nằm ở NewHandler (server.go). Hai thợ viết song song:
//
//   - phần đăng nhập: auth.go (và file mới tên auth_*.go): đăng nhập, phiên,
//     CSRF, header bảo mật, TLS, tài khoản. Cài đặt các hàm có chú thích
//     "auth" dưới đây.
//   - phần API: api.go (và file mới tên api_*.go, sse.go): site, stack, job,
//     SSE, máy, cơ sở dữ liệu. Cài đặt các hàm có chú thích "api".
//
// Thêm đường dẫn: khai báo ở bảng trên và ở NewHandler (server.go), viết
// handler ở file của phần tương ứng.
//
// Quy ước chung:
//   - JSON, UTF-8. Thời gian dạng RFC 3339 (time.Time mặc định của Go).
//   - Lỗi luôn có dạng ErrorBody với mã HTTP phù hợp: 400 đầu vào sai
//     (bad_request, domain_invalid, email_invalid, title_invalid,
//     password_too_short, limit_invalid, after_invalid), 401 chưa đăng nhập
//     (unauthorized, wrong_password), 403 bị CSRF chặn (csrf), 404 không có
//     (not_found, site_not_found, job_not_found, session_not_found), 409 đang có
//     job khác (job_running) hoặc core.RefusedError khác (kèm Checks), 423 IP
//     đang bị khoá (login_locked, có Retry-After), 503 chưa chạy tpan setup
//     (not_set_up), 500 lỗi khác (internal).
//   - Endpoint có thay đổi nhận ?dry_run=1 và trả engine.Preview; chạy thật
//     thì trả 202 JobStarted, tiến độ lấy ở /api/v1/jobs/{id}/events (SSE).
//   - []byte trong engine.FileChange (old, new) ra JSON dạng base64 (mặc định
//     của encoding/json); giao diện tự giải.
//   - Không bao giờ trả mật khẩu trong JSON, trừ JobDetail.Secret (xem dưới).
//
// Đường dẫn (C = công khai, không cần phiên; còn lại cần phiên):
//
//	C GET    /api/v1/health                       {status, version, commit}       (đã có)
//	C POST   /api/v1/login                        LoginRequest → 204 + cookie      auth
//	C POST   /api/v1/logout                       → 204, xoá phiên                 auth
//	C GET    /api/v1/session                      → SessionInfo                     auth
//	  POST   /api/v1/account/password             PasswordChange → 204             auth
//	  GET    /api/v1/account/sessions             → []SessionRow                    auth
//	  DELETE /api/v1/account/sessions/{id}        → 204                             auth
//	  GET    /api/v1/overview                     → Overview                        api
//	  GET    /api/v1/machine                      → Machine                         api
//	  GET    /api/v1/stack                        → core.Status                     api
//	  GET    /api/v1/checks                       → core.Health (phép kiểm stack)    api
//	  GET    /api/v1/sites                        → []site.Site                     api
//	  POST   /api/v1/sites[?dry_run=1]            SiteCreate → Preview | JobStarted api
//	  GET    /api/v1/sites/{domain}               → core.SiteInfo                   api
//	  DELETE /api/v1/sites/{domain}[?dry_run=1]   → Preview | JobStarted            api
//	  GET    /api/v1/jobs[?limit=N]               → []engine.Job (mới nhất trước)    api
//	  GET    /api/v1/jobs/{id}                    → JobDetail                       api
//	  GET    /api/v1/jobs/{id}/events             SSE                               api
//	  GET    /api/v1/databases                    → []DatabaseRow                   api
//
// SSE (/api/v1/jobs/{id}/events): mỗi sự kiện là
//
//	id: <Event.Seq>
//	event: <Event.Kind>
//	data: <engine.Event dạng JSON>
//
// Header Last-Event-ID (hoặc ?after=N) để nối lại: gửi các sự kiện có Seq lớn
// hơn từ store rồi tiếp tục trực tiếp. Job đã xong thì gửi hết rồi đóng. Sau
// sự kiện job.done thì đóng luồng.

import (
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// Deps là những gì server cần. cmd/tpan dựng nó: máy thật (LocalSystem và
// /var/lib/tpan/tpan.db) hoặc chế độ --demo (internal/demo: FakeSystem và một
// DB riêng).
type Deps struct {
	Sys   system.System
	Store *store.Store
	// Demo: đang chạy --demo. Giao diện hiện nhãn "demo"; đăng nhập vẫn bắt buộc.
	Demo bool
	// Hostname là tên máy hiện trên thanh đầu trang.
	Hostname string
	// Now nil thì dùng time.Now (test đặt để có thời điểm cố định).
	Now func() time.Time
	// WPHash và WPCLISHA512 rỗng thì core dùng mã đã ghim của bản thật
	// (steps.DefaultWordPressSHA1, steps.DefaultWPCLISHA512). Chế độ --demo đặt
	// mã của file giả (demo.Hashes), không thì tạo site trên máy giả hỏng ở
	// bước kiểm checksum.
	WPHash      string
	WPCLISHA512 string
}

// ErrorBody là dạng lỗi duy nhất của API (mục 9).
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string    `json:"code"`
	Message i18n.Text `json:"message"`
	// Checks: các điều kiện đầu không đạt (từ core.RefusedError).
	Checks []checks.Result `json:"checks,omitempty"`
}

// ---- auth ----

// LoginRequest: chỉ có một tài khoản quản trị nên không có tên đăng nhập.
type LoginRequest struct {
	Password string `json:"password"`
}

// SessionInfo trả lời "tôi đã đăng nhập chưa" và thông tin chung cho khung
// trang. Gọi được khi chưa đăng nhập (LoggedIn false, các trường khác rỗng
// trừ Demo).
type SessionInfo struct {
	LoggedIn bool      `json:"logged_in"`
	Demo     bool      `json:"demo"`
	Hostname string    `json:"hostname,omitempty"`
	Version  string    `json:"version,omitempty"`
	Expires  time.Time `json:"expires,omitzero"`
}

type PasswordChange struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

// SessionRow là một phiên đăng nhập đang sống. ID là một mã ngắn để xoá
// phiên (không phải token, không phải hash của token).
type SessionRow struct {
	ID       string    `json:"id"`
	Current  bool      `json:"current"`
	IP       string    `json:"ip"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

// ---- api ----

// SiteCreate là thân của POST /api/v1/sites. Server đưa qua validate trước
// khi gọi core (mục 11).
type SiteCreate struct {
	Domain string `json:"domain"`
	Title  string `json:"title"`
	Email  string `json:"email"`
}

// JobStarted: job đã bắt đầu chạy nền.
type JobStarted struct {
	JobID string `json:"job_id"`
}

// JobDetail là một job cùng mọi sự kiện đã có.
type JobDetail struct {
	Job    engine.Job     `json:"job"`
	Events []engine.Event `json:"events"`
	// Secret chỉ có ở lần GET đầu tiên sau khi job site.add thành công và
	// thật sự cài WordPress: tài khoản quản trị WordPress, hiện một lần rồi bỏ
	// khỏi bộ nhớ (mục 11, "Bí mật"). Không bao giờ ghi vào DB hay log.
	Secret *AdminSecret `json:"secret,omitempty"`
}

type AdminSecret struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// Machine là số liệu máy cho Bảng điều khiển. Đọc /proc qua system.System để
// chế độ --demo giả lập được. Byte cho bộ nhớ và đĩa.
type Machine struct {
	// CPUPercent là phần trăm CPU bận, đo giữa hai lần đọc /proc/stat cách
	// nhau một khoảng ngắn; -1 khi chưa đo được.
	CPUPercent float64   `json:"cpu_percent"`
	CPUs       int       `json:"cpus"`
	Load1      float64   `json:"load1"`
	MemTotal   uint64    `json:"mem_total"`
	MemUsed    uint64    `json:"mem_used"` // MemTotal - MemAvailable
	SwapTotal  uint64    `json:"swap_total"`
	SwapUsed   uint64    `json:"swap_used"`
	Disk       DiskUsage `json:"disk"` // phân vùng chứa /var/www
	Uptime     float64   `json:"uptime_seconds"`
	Measured   time.Time `json:"measured"`
	Errors     []string  `json:"errors,omitempty"` // phần nào không đọc được
}

type DiskUsage struct {
	Path  string `json:"path"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// SiteRow là một tuyến trên bản đồ: site và các phép kiểm theo đúng thứ tự
// chặng (checks.SiteChecks).
type SiteRow struct {
	Site   site.Site       `json:"site"`
	Checks []checks.Result `json:"checks"`
}

// Overview là mọi thứ Bảng điều khiển cần trong một lần gọi.
type Overview struct {
	Hostname string       `json:"hostname"`
	Stack    StackSummary `json:"stack"`
	Sites    []SiteRow    `json:"sites"`
	Machine  Machine      `json:"machine"`
	Jobs     []engine.Job `json:"jobs"` // 10 job mới nhất
	// StackChecks là phép kiểm chung của máy (core.Stack.Health). Lỗi ở đây
	// là "cột" ga đỏ trên bản đồ (lỗi tầng dùng chung).
	StackChecks []checks.Result `json:"stack_checks"`
}

// StackSummary là các nhãn stack trên thanh đầu trang: "LEMP", "nginx",
// "PHP 8.3", "MariaDB". Installed false thì giao diện mời cài stack bằng CLI.
type StackSummary struct {
	Installed  bool   `json:"installed"`
	Web        string `json:"web"`         // "nginx" | "apache" | ""
	PHPVersion string `json:"php_version"` // "8.3"
}

// DatabaseRow là một dòng của màn Cơ sở dữ liệu (chỉ xem ở M3, mục 6.9).
type DatabaseRow struct {
	Domain    string `json:"domain"`
	DB        string `json:"db"`
	User      string `json:"user"`
	SizeBytes int64  `json:"size_bytes"` // -1 khi không đọc được
	Tables    int    `json:"tables"`     // -1 khi không đọc được
}
