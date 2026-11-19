// Package playbooks giữ bản hướng dẫn cho AI theo từng mục của giao diện (mục
// 8.4 của docs/KE-HOACH.md): mục này có dữ liệu gì, nên đọc gì trước, chẩn
// đoán theo thứ tự nào, Plan nào hợp lệ ở đây. Bản của mục đang mở được gửi
// kèm câu hỏi đầu tiên ở mục đó (không nằm trong system prompt, vì Claude
// Code giữ nguyên system prompt suốt một phiên).
//
// Hợp đồng M8. Các file <tên>.md viết bằng tiếng Anh (AI đọc), ngắn, chỉ nhắc
// công cụ có trong danh mục (internal/tools.Catalog) hoặc đã được giao cho M8
// thêm (current_view, propose_health_fix, propose_ssl_issue). Hai hàm:
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

import (
	"embed"
	"strings"
)

//go:embed *.md
var files embed.FS

// names là tên mọi bản hướng dẫn, đã sắp: mỗi tên có đúng một file <tên>.md.
// Khai báo tay (không đọc từ thư mục embed) để test bắt được file thừa hoặc
// thiếu: thêm một mục vào menu mà quên viết bản hướng dẫn thì test đỏ.
var names = []string{
	"backup", "bang-dieu-khien", "bao-mat", "cap-nhat", "co-so-du-lieu", "cron",
	"duyet-plan", "lich-su", "site", "ssl", "stack", "su-co", "thung-rac",
}

// exact: các đường dẫn đứng một mình. prefix: mục có trang con (danh sách và
// trang một đối tượng); "/sites/new", "/sites/<d>", "/sites/<d>/log" đều về site.
var (
	exact = map[string]string{
		"/":          "bang-dieu-khien",
		"/sites":     "site",
		"/databases": "co-so-du-lieu",
		"/ssl":       "ssl",
		"/backups":   "backup",
		"/cron":      "cron",
		"/trash":     "thung-rac",
		"/incidents": "su-co",
		"/jobs":      "lich-su",
		"/stack":     "stack",
		"/updates":   "cap-nhat",
		"/security":  "bao-mat",
		"/plans":     "duyet-plan",
	}
	prefix = map[string]string{
		"/sites/":     "site",
		"/incidents/": "su-co",
		"/jobs/":      "lich-su",
		"/plans/":     "duyet-plan",
	}
)

// For: xem chú thích đầu gói. Đường dẫn do trình duyệt gửi nên coi là không tin
// cậy: chỉ so khớp với bảng, không bao giờ ghép vào tên file.
func For(path string) (name, text string, ok bool) {
	// Bỏ query và fragment, rồi dấu "/" cuối (trừ gốc).
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	name, ok = exact[path]
	if !ok {
		for p, n := range prefix {
			// Sau tiền tố phải còn một tên, không chấp nhận "/sites//x".
			if rest, found := strings.CutPrefix(path, p); found && rest != "" && !strings.HasPrefix(rest, "/") {
				name, ok = n, true
				break
			}
		}
	}
	if !ok {
		return "", "", false
	}
	b, err := files.ReadFile(name + ".md")
	if err != nil {
		// Không xảy ra khi test qua: mỗi tên trong bảng có file.
		return "", "", false
	}
	return name, string(b), true
}

// Names trả tên mọi bản hướng dẫn, đã sắp. Bản sao: sửa nó không đổi bảng.
func Names() []string { return append([]string(nil), names...) }
