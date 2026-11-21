package engine

import "github.com/doxuta/tpan/internal/system"

// Note ghi line thành một sự kiện output của bước đang chạy (M18): bước không
// chạy lệnh nào (tải một file lên kho S3) vẫn có tiến độ và kết quả trong log
// job như bước chạy lệnh. sys là System mà engine đưa cho Step.Check, Apply,
// Undo; ngoài lúc Execute (xem trước, test gọi thẳng Step) thì không làm gì.
//
// line đi nguyên văn vào job_events, SSE và MCP: không bao giờ được chứa bí
// mật (khoá, mật khẩu, chữ ký).
func Note(sys system.System, line string) {
	if rs, ok := sys.(recSys); ok {
		rs.r.emit(EventOutput, rs.stepID, line)
	}
}
