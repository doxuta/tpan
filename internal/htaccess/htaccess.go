// Package htaccess giữ danh sách chỉ thị mà một site ở chế độ "an-toan" được
// dùng trong .htaccess, và hàm quét một file .htaccess tìm chỉ thị nằm ngoài
// danh sách đó (M14, mục 6.13 của docs/KE-HOACH.md, phần ".htaccess").
//
// Gói lá: không import gói nào của tpan, không chạm máy. templates dùng
// List và OverrideOptions để sinh khối AllowOverride của vhost; checks và core
// dùng Scan để nói trước một file .htaccess có làm site báo 500 không.
package htaccess

import "strings"

// MaxNames là số tên tối đa Apache nhận trên một dòng AllowOverrideList.
// Phần thừa bị bỏ LẶNG LẼ (không cảnh báo), và dòng AllowOverrideList sau thay
// dòng trước chứ không cộng dồn (thấy trên máy ảo, Apache 2.4.58: danh sách
// sinh tự động 129 tên làm RewriteEngine rơi mất và WordPress báo 500). Vì vậy
// danh sách là một hằng chọn tay, có test đếm.
const MaxNames = 64

// Allowed là các tên chỉ thị của chế độ "an-toan" (55 tên): thứ WordPress và
// các plugin phổ biến ghi vào .htaccess (rewrite, phân quyền, header, cache,
// kiểu file, chuyển hướng). Tên bắt đầu bằng "<" là khối (section).
//
// KHÔNG được thêm:
//   - SetHandler, AddHandler, RemoveHandler, Action, ProxyFCGISetEnvIf,
//     ProxyFCGIBackendType: với chúng, .htaccess của một site gửi được request
//     sang socket PHP-FPM của site khác kèm cấu hình PHP do nó chọn, tức là
//     chạy mã của mình dưới user của site kia (lỗ L4 ở
//     docs/thay-doi/M14-nghien-cuu.md).
//   - Options: cho cả chỉ thị thì .htaccess viết được "Options FollowSymLinks"
//     (dạng không có dấu +/-), bỏ mất SymLinksIfOwnerMatch của vhost, và một
//     symlink trỏ sang file của site khác được Apache trả về (đo trên máy ảo,
//     docs/bang-chung/M14-nghien-cuu/t9-apache-options-symlink.ket-qua.txt).
//     Options được mở riêng, giới hạn theo từng tuỳ chọn: xem AllowedOptions.
//
// Test giữ cả hai luật này.
var Allowed = []string{
	"<Files", "<FilesMatch", "<IfModule", "<If", "<Else", "<ElseIf",
	"<Limit", "<LimitExcept", "<RequireAll", "<RequireAny", "<RequireNone",
	"RewriteEngine", "RewriteRule", "RewriteCond", "RewriteBase", "RewriteOptions",
	"Require", "Order", "Allow", "Deny", "Satisfy",
	"AuthType", "AuthName", "AuthUserFile", "AuthGroupFile", "AuthBasicProvider",
	"Header", "RequestHeader",
	"ExpiresActive", "ExpiresByType", "ExpiresDefault", "FileETag",
	"AddType", "AddEncoding", "AddCharset", "AddDefaultCharset",
	"AddOutputFilterByType", "AddOutputFilter", "SetOutputFilter",
	"SetEnv", "SetEnvIf", "SetEnvIfNoCase", "UnsetEnv",
	"BrowserMatch", "BrowserMatchNoCase",
	"DirectoryIndex", "ErrorDocument",
	"Redirect", "RedirectMatch", "RedirectPermanent", "RedirectTemp",
	"LimitRequestBody", "ServerSignature", "AddLanguage", "DefaultLanguage",
}

// AllowedOptions là các tuỳ chọn mà chỉ thị Options trong .htaccess được bật
// hay tắt ở chế độ "an-toan". Không có FollowSymLinks (lý do ở Allowed), không
// có ExecCGI, Includes. Plugin hay ghi "Options -Indexes" và
// "Options -MultiViews": cả hai vẫn chạy. "Options +FollowSymLinks" thì Apache
// báo 500 ("Option FollowSymLinks not allowed here"); Scan báo trước dòng đó.
var AllowedOptions = []string{"Indexes", "MultiViews", "SymLinksIfOwnerMatch"}

// List trả Allowed nối bằng dấu cách: phần đứng sau "AllowOverrideList " trong vhost.
func List() string { return strings.Join(Allowed, " ") }

// OverrideOptions trả phần đứng sau "AllowOverride " của chế độ "an-toan":
// "Options=Indexes,MultiViews,SymLinksIfOwnerMatch". AllowOverride mở nhóm
// Options với đúng các tuỳ chọn đó; mọi chỉ thị khác đi qua AllowOverrideList.
func OverrideOptions() string { return "Options=" + strings.Join(AllowedOptions, ",") }

// Finding là một chỉ thị nằm ngoài Allowed trong một file .htaccess.
type Finding struct {
	// File là đường dẫn file (người gọi đưa vào, Scan chỉ chép lại).
	File string `json:"file"`
	// Line là số dòng, tính từ 1 (dòng đầu của chỉ thị nếu nó nối nhiều dòng bằng "\").
	Line int `json:"line"`
	// Directive là tên chỉ thị đúng như viết trong file; khối thì kèm dấu "<"
	// ở đầu. Với Options mang tuỳ chọn không được phép thì là "Options <tuỳ
	// chọn>" (bỏ dấu +/-), ví dụ "Options FollowSymLinks".
	Directive string `json:"directive"`
}

// Scan đọc nội dung một file .htaccess và trả mọi chỉ thị không nằm trong
// Allowed, theo thứ tự xuất hiện. Hàm thuần: không đọc đĩa, không lỗi.
//
// Luật đọc (theo cách Apache đọc file cấu hình):
//   - dòng trống và dòng chú thích (ký tự đầu tiên khác khoảng trắng là "#") bỏ qua;
//   - dòng kết thúc bằng "\" nối với dòng sau thành một chỉ thị;
//   - tên chỉ thị là từ đầu tiên; so không phân biệt hoa thường;
//   - dòng mở khối "<Tên ...>" có tên là "<Tên" (một khối không có tham số viết
//     "<Tên>" cũng vậy); dòng đóng khối "</Tên>" bỏ qua;
//   - chỉ thị Options: được phép, nhưng mỗi tham số (bỏ dấu + hoặc - ở đầu, so
//     không phân biệt hoa thường) phải là "None" hoặc nằm trong AllowedOptions;
//     mỗi tham số ngoài danh sách cho một Finding "Options <tham số>".
//
// Scan không hiểu ngữ nghĩa của các chỉ thị khác: một chỉ thị có tên trong
// Allowed mà tham số sai thì Apache vẫn báo 500, và việc đó để phép kiểm HTTP bắt.
func Scan(file string, content []byte) []Finding {
	var out []Finding
	lines := strings.Split(string(content), "\n")
	for i := 0; i < len(lines); i++ {
		start := i + 1
		ln := strings.TrimRight(lines[i], "\r")
		// Nối dòng: Apache bỏ dấu "\" cuối dòng rồi ghép dòng sau vào.
		for strings.HasSuffix(ln, `\`) && i+1 < len(lines) {
			i++
			ln = strings.TrimSuffix(ln, `\`) + strings.TrimRight(lines[i], "\r")
		}
		fields := strings.Fields(ln)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") || strings.HasPrefix(fields[0], "</") {
			continue
		}
		name := fields[0]
		if strings.HasPrefix(name, "<") {
			name = strings.TrimSuffix(name, ">") // "<Tên>" không có tham số
		}
		if strings.EqualFold(name, "Options") {
			for _, arg := range fields[1:] {
				opt := strings.TrimLeft(arg, "+-")
				if !strings.EqualFold(opt, "None") && !containsFold(AllowedOptions, opt) {
					out = append(out, Finding{File: file, Line: start, Directive: name + " " + opt})
				}
			}
			continue
		}
		if !containsFold(Allowed, name) {
			out = append(out, Finding{File: file, Line: start, Directive: name})
		}
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
