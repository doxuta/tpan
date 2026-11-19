// Package ai giữ phần dùng chung của AI và agent: che dữ liệu trước khi gửi đi
// (mục 8.1, nguyên tắc 4 và 5 của docs/KE-HOACH.md). Từ M8 thêm phiên hỏi đáp
// và giao diện Brain.
//
// Hợp đồng M7. Thợ của đợt 1 viết ở redact.go (cùng test):
//
//	// NewRedactor dựng bộ che ở mức level. aliases là bảng bí danh → giá trị
//	// thật đã lưu của phiên (nil cho phiên mới): bí danh cũ được giữ nguyên.
//	func NewRedactor(level Level, aliases map[string]string) *Redactor
//	// Learn cho bộ che biết các tên thật cần che dù chúng xuất hiện ở đâu:
//	// domain của mọi site (cả dạng punycode lẫn Unicode), slug suy từ domain
//	// (validate.SlugOf) và các tên suy từ slug (user Linux, DB, pool tp_<slug>,
//	// tpan-<slug>), hostname của máy. Gọi lại nhiều lần được; bí danh gán theo
//	// thứ tự đã sắp xếp nên ổn định.
//	func (r *Redactor) Learn(domains []validate.Domain, hostname string)
//	// String che một chuỗi:
//	//   - luôn (cả LevelSecrets): mật khẩu, khoá, salt bị loại hẳn, thay bằng
//	//     Removed. Ví dụ: define('DB_PASSWORD', '...'), các AUTH_KEY/..._SALT
//	//     của wp-config.php, IDENTIFIED BY '...', password=..., khoá PEM.
//	//   - thêm ở LevelAll: tên đã Learn và mọi domain khác, email, IPv4, IPv6
//	//     được thay bằng bí danh ổn định trong phiên: domain → site-a.example,
//	//     site-b.example...; slug → site_a...; hostname → server-a.example;
//	//     IPv4 → 203.0.113.N; IPv6 → 2001:db8::N; email → user-N@mail.example.
//	//     Không che 127.0.0.1, ::1, 0.0.0.0 và tên đuôi file (wp-config.php,
//	//     nginx.conf...): chẩn đoán cần chúng và chúng không lộ gì.
//	func (r *Redactor) String(s string) string
//	// JSON mã hoá v thành JSON rồi che: giá trị của khoá có tên như mật khẩu,
//	// khoá, token, salt bị thay bằng Removed; mọi chuỗi còn lại (kể cả tên
//	// khoá của map) qua String.
//	func (r *Redactor) JSON(v any) (json.RawMessage, error)
//	// Unalias đổi mọi bí danh trong s về giá trị thật. Dùng cho đối số agent
//	// gửi vào và cho panel hiển thị câu trả lời (M8). Chuỗi không chứa bí danh
//	// giữ nguyên (người dùng có thể gõ thẳng tên thật trong Claude Code).
//	func (r *Redactor) Unalias(s string) string
//	// Aliases trả bản sao bảng bí danh → giá trị thật, để lưu vào ai_sessions.
//	func (r *Redactor) Aliases() map[string]string
//	// Untrusted bọc text do người lạ gửi tới (log truy cập, output lệnh) trong
//	// thẻ đánh dấu, để AI coi là dữ liệu chứ không phải mệnh lệnh. Thẻ đóng lỡ
//	// nằm sẵn trong text được vô hiệu hoá để text không tự thoát ra ngoài thẻ.
//	func Untrusted(source, text string) string
//
// Bộ che an toàn khi dùng từ nhiều goroutine.
package ai

// Level là mức che (setting ai.redact).
type Level string

const (
	LevelAll     Level = "all"     // mặc định: che bí mật, tên miền, IP, email
	LevelSecrets Level = "secrets" // chỉ loại bí mật
)

// Removed là chuỗi thay cho bí mật bị loại, giống system.Secret khi in ra.
const Removed = "••••"

// Redactor che dữ liệu của một phiên AI. Bí danh ổn định suốt phiên.
type Redactor struct {
	// Trường không xuất (khoá, mức che, bảng bí danh hai chiều) do thợ của
	// đợt 1 thêm.
}
