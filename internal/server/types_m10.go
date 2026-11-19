package server

// Hợp đồng API của M10 (chuyển LEMP ⇄ LAMP, mục 6.6). Quy ước lỗi, dry_run,
// JobStarted giống M3 (types.go). Handler ở api_m10.go.
//
//	POST /api/v1/stack/switch[?dry_run=1]  StackSwitchBody → Preview | JobStarted
//
// Lời từ chối của core là 409 kèm checks: web_invalid, stack_not_installed,
// stack_same_web, precondition_failed (checks là các site đang không trả lời
// đúng trên web server hiện tại). Thân JSON sai là 400 bad_request. Audit:
// stack.switch, target là web server đích. Job xong (thành công hay không) thì
// server chạy health đầy đủ như sau update.apply: phép kiểm của web server cũ
// không còn chạy, sự cố của chúng đóng.

// StackSwitchBody là thân của POST /api/v1/stack/switch.
type StackSwitchBody struct {
	To string `json:"to"` // "nginx" | "apache"
}
