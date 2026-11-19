// Package tools là danh mục công cụ có kiểu cho AI và agent (mục 8.1 của
// docs/KE-HOACH.md). MCP (internal/mcp) và AI trong panel (M8) chỉ gọi được
// đúng các công cụ ở đây; không có công cụ shell, không có công cụ ghi file.
//
// Ba loại:
//   - đọc (KindRead): danh sách site, phép kiểm, số liệu máy, đuôi log, file do
//     tpan quản lý, lịch sử job, Plan;
//   - lập Plan (KindPlan): chỉ sinh một Plan ở trạng thái chờ duyệt;
//   - chạy Plan (KindApply): apply_plan, chỉ chạy được ở chế độ duyệt client,
//     cho Plan do chính phiên trực tiếp đó đề xuất (mục 8.3).
//
// Hợp đồng M7. Thợ của đợt 2 viết ở catalog.go, read.go, plan.go, session.go
// (cùng test):
//
//	// Catalog trả danh mục cố định, sắp theo tên. Tên công cụ:
//	//   đọc:     list_sites, site_health{domain}, stack_status, machine,
//	//            tail_log{domain, log: access|error|php-error, lines<=200},
//	//            managed_file{path} (chỉ file có trong managed_files),
//	//            list_jobs{limit<=50}, get_job{id}, list_plans{status}, get_plan{id}
//	//   lập Plan: propose_site_add{domain, email, title}, propose_site_remove{domain},
//	//            propose_site_repair{domain}, propose_stack_repair{}
//	//   chạy:    apply_plan{plan_id}
//	func Catalog() []Tool
//	// OpenSession mở phiên: o.ID rỗng thì tạo phiên mới (entry mcp, origin
//	// mcp); khác rỗng thì nối vào phiên panel có sẵn (M8; không có thì lỗi),
//	// origin ai, và chế độ duyệt luôn là panel. Đọc AgentConfig để biết mức
//	// che và chế độ duyệt; nạp bảng bí danh đã lưu; Learn mọi site và hostname.
//	func OpenSession(ctx context.Context, d Deps, o SessionOptions) (*Session, error)
//	// Call là CHỖ DUY NHẤT dữ liệu đi ra cho agent. Thứ tự: tên không có trong
//	// Catalog thì từ chối; đổi bí danh trong đối số về tên thật (Unalias, trên
//	// từng chuỗi); giải JSON chặt (trường lạ là lỗi); chạy; công cụ Untrusted
//	// thì đánh dấu phiên; che kết quả và cả lỗi (Redactor.JSON, String); ghi
//	// một dòng ai_calls (đúng thứ agent nhận) và lưu trạng thái phiên.
//	// Lỗi trả về luôn là *Error với Message đã che.
//	func (s *Session) Call(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
//	// SetClient ghi tên client MCP tự khai (clientInfo lúc initialize).
//	func (s *Session) SetClient(ctx context.Context, client string) error
//	// ID là id của phiên trong ai_sessions.
//	func (s *Session) ID() string
package tools

import (
	"encoding/json"
	"time"

	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// Kind là loại công cụ.
type Kind string

const (
	KindRead  Kind = "read"
	KindPlan  Kind = "plan"
	KindApply Kind = "apply"
)

// Tool là một công cụ trong danh mục.
type Tool struct {
	Name string
	Kind Kind
	// Description viết cho AI (tiếng Anh, ngắn): công cụ làm gì, khi nào dùng.
	Description string
	// InputSchema là JSON Schema (type "object") của đối số, sinh từ struct
	// đầu vào bằng github.com/google/jsonschema-go.
	InputSchema json.RawMessage
	// Untrusted: kết quả chứa text do người lạ gửi tới (log, output lệnh); gọi
	// công cụ này thì phiên bị đánh dấu, và Plan đề xuất sau đó mang cảnh báo.
	Untrusted bool
	// run giải đối số (đã đổi bí danh) và chạy công cụ (catalog.go).
	run runFunc
}

// Deps là những gì công cụ cần. cmd/tpan dựng nó giống server.Deps.
type Deps struct {
	Sys      system.System
	Store    *store.Store
	Hostname string
	// WPHash, WPCLISHA512 rỗng thì core dùng mã đã ghim (như server.Deps).
	WPHash, WPCLISHA512 string
	// Now nil thì dùng time.Now.
	Now func() time.Time
}

// SessionOptions cấu hình OpenSession.
type SessionOptions struct {
	// ID rỗng: phiên mới của agent nối trực tiếp. Khác rỗng: `tpan mcp
	// --session <id>`, nối vào phiên panel có sẵn (M8).
	ID string
}

// Session là một phiên làm việc của agent: bộ che, chế độ duyệt, số thứ tự
// lần gọi. Một tiến trình `tpan mcp` có đúng một Session.
type Session struct {
	// st giữ Deps, bản ghi phiên, bộ che, chế độ duyệt và số thứ tự lần gọi
	// (khai báo ở session.go, để file này không phải import thêm gói).
	st sessionState
}

// Error là lỗi trả cho agent: Code ổn định, Message đã che.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
