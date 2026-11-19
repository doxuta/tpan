// Package core chứa các ca sử dụng (cài stack, tạo site, ...). Mỗi ca sử dụng
// đọc cấu hình, kiểm điều kiện, dựng một engine.Plan từ các Step, rồi đưa cho
// engine xem trước hoặc chạy. CLI, API và MCP đều gọi vào đây.
//
// File này giữ cấu hình của stack và tên khoá của nó trong bảng settings. Cấu
// hình là tham số, không ghi cứng ở Step nào, để M12 (nhiều bản PHP, nguồn gói
// khác) không phải viết lại M1. Step nhận danh sách gói và bản PHP qua tham số;
// chỉ core đọc và ghi settings.
package core

// WebServer là web server của stack.
type WebServer string

const (
	Nginx  WebServer = "nginx"  // LEMP
	Apache WebServer = "apache" // LAMP
)

// Source là nguồn gói của một thành phần (mục 6.11). M1 chỉ có SourceUbuntu;
// M12 thêm nginx.org, kho của MariaDB, kho PHP của Ondřej Surý.
type Source string

const SourceUbuntu Source = "ubuntu"

// StackConfig là cấu hình của stack, lưu trong bảng settings.
type StackConfig struct {
	// Web rỗng nghĩa là stack chưa được cài bởi tpan.
	Web       WebServer `json:"web"`
	WebSource Source    `json:"web_source"`
	DBSource  Source    `json:"db_source"`
	PHPSource Source    `json:"php_source"`
	// PHPVersion là bản PHP mặc định, ví dụ "8.3". Rỗng trước khi cài: dò từ
	// /etc/php/*/fpm sau bước cài gói rồi mới ghi (mục 6.1, bước 4).
	PHPVersion string `json:"php_version"`
}

// Tên khoá trong bảng settings. Giá trị là chuỗi.
const (
	SettingWeb        = "stack.web"
	SettingWebSource  = "stack.web_source"
	SettingDBSource   = "stack.db_source"
	SettingPHPSource  = "stack.php_source"
	SettingPHPVersion = "stack.php_version"
)
