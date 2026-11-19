package engine

// Plan chờ duyệt (mục 4.2 và 8.1 của docs/KE-HOACH.md). Plan do AI hay agent
// qua MCP đề xuất không chạy ngay: nó được lưu ở bảng plans cùng bản xem trước
// lúc đề xuất. Ngay trước khi chạy, Plan được dựng lại trên trạng thái hiện tại
// của máy; dấu vân tay của bản xem trước mới khác bản đã lưu thì phải duyệt lại.
//
// Kiểu nằm ở engine vì store (lưu) và core (dựng lại, duyệt) cùng dùng, và
// store đã import engine.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
)

// Origin là nguồn gốc của một Plan chờ duyệt. Giao diện hiện nhãn theo nó.
type Origin string

const (
	OriginHuman Origin = "human" // người gõ ở panel hoặc CLI
	OriginAI    Origin = "ai"    // AI trong panel (M8, phiên có --session)
	OriginMCP   Origin = "mcp"   // agent nối trực tiếp qua `tpan mcp`
)

// PlanStatus là trạng thái của một Plan chờ duyệt.
type PlanStatus string

const (
	PlanPending  PlanStatus = "pending"  // chờ người duyệt
	PlanApplied  PlanStatus = "applied"  // đã duyệt và đã giao cho engine (JobID cho biết job)
	PlanRejected PlanStatus = "rejected" // người từ chối
)

// PendingPlan là một dòng của bảng plans.
type PendingPlan struct {
	// ID: 16 ký tự hex ngẫu nhiên, core sinh lúc đề xuất.
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Target string    `json:"target"`
	Title  i18n.Text `json:"title"`
	// Request là JSON của core.PlanRequest, mang giá trị thật (không bí danh):
	// đủ để dựng lại Plan lúc duyệt.
	Request json.RawMessage `json:"request"`
	// Preview là bản xem trước mới nhất mà người duyệt được xem.
	Preview     Preview `json:"preview"`
	Fingerprint string  `json:"fingerprint"`
	Origin      Origin  `json:"origin"`
	// SessionID là phiên AI (bảng ai_sessions) đã đề xuất; rỗng khi người đề xuất.
	SessionID string `json:"session_id,omitempty"`
	// Untrusted: lúc đề xuất, phiên đó đã đọc nội dung do người lạ gửi tới
	// (log truy cập, output lệnh). Màn duyệt hiện cảnh báo (mục 8.1, ý 5).
	Untrusted bool       `json:"untrusted"`
	Status    PlanStatus `json:"status"`
	Created   time.Time  `json:"created"`
	Decided   time.Time  `json:"decided,omitzero"`
	// DecidedBy: "cli", "web:<ip>", hoặc "mcp:<session id>" (chế độ client).
	DecidedBy string `json:"decided_by,omitempty"`
	JobID     string `json:"job_id,omitempty"`
}

// Fingerprint là sha256 (hex) của bản xem trước dạng JSON: từng bước, bước nào
// đã đúng, lệnh, file và nội dung cũ, mới. Hai bản xem trước cùng dấu vân tay
// nghĩa là người duyệt đang xem đúng thứ sẽ chạy.
//
// ponytail: băm cả JSON thay vì chọn trường; trường nào đổi (kể cả câu chữ của
// bước) cũng đòi duyệt lại, chặt hơn mức cần nhưng không bao giờ lỏng.
func Fingerprint(p Preview) string {
	// Preview chỉ có chuỗi, bool, slice và []byte: Marshal không lỗi được.
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
