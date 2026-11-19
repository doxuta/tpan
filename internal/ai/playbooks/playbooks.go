// Package playbooks giữ bản hướng dẫn cho AI theo từng mục của giao diện (mục
// 8.4 của docs/KE-HOACH.md): mục này có dữ liệu gì, nên đọc gì trước, chẩn
// đoán theo thứ tự nào, Plan nào hợp lệ ở đây. Bản của mục đang mở được gửi
// kèm câu hỏi đầu tiên ở mục đó (không nằm trong system prompt, vì Claude
// Code giữ nguyên system prompt suốt một phiên).
//
// Hợp đồng M8. Thợ đợt 1 viết các file <tên>.md (tiếng Anh, ngắn, chỉ nhắc
// đúng công cụ có trong internal/tools.Catalog) và thân hai hàm (cùng test):
//
//	// For trả tên và nội dung bản hướng dẫn cho đường dẫn path của panel
//	// (bỏ query, bỏ dấu "/" cuối). Bảng:
//	//   "/"                                 → bang-dieu-khien
//	//   "/sites", "/sites/new", "/sites/<d>" → site
//	//   "/databases" → co-so-du-lieu; "/ssl" → ssl; "/backups" → backup;
//	//   "/cron" → cron; "/trash" → thung-rac; "/incidents", "/incidents/<id>" → su-co;
//	//   "/jobs", "/jobs/<id>" → lich-su; "/stack" → stack; "/updates" → cap-nhat;
//	//   "/security" → bao-mat; "/plans", "/plans/<id>" → duyet-plan.
//	// Mục khác (/ai, /settings...): ok=false.
//	func For(path string) (name, text string, ok bool)
//	// Names trả tên mọi bản, đã sắp. Test: mỗi tên có file, mỗi file có tên.
//	func Names() []string
package playbooks

// For: thân tạm của đợt 0.
func For(path string) (name, text string, ok bool) { return "", "", false }

// Names: thân tạm của đợt 0.
func Names() []string { return nil }
