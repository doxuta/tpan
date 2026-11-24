package checks

// M19 (S13 của docs/thay-doi/M19-thiet-ke.md): tên phụ mới không được là tên
// đang khai ở một vhost khác trên máy (kể cả vhost admin dựng tay, không có
// trong DB của tpan). nginx thấy server_name trùng chỉ cảnh báo và khối nạp
// trước thắng; tpan-*.conf đứng trước nhiều file theo thứ tự chữ cái, nên tên
// phụ sẽ lặng lẽ chiếm lưu lượng của vhost kia.

import (
	"path"
	"strings"
)

// DeclaredNames đọc bản in cấu hình đang nạp (web "nginx": stdout của
// `nginx -T`; "apache": stdout cộng stderr của `apache2ctl -S`) và trả mỗi tên
// đang khai (chữ thường, bỏ chấm cuối) kèm file khai nó. Một tên khai ở nhiều
// file thì giữ file nạp trước (file web server thật sự dùng). ok=false khi
// web lạ hoặc output không đọc được.
func DeclaredNames(web string, dump string) (map[string]string, bool) {
	var decls []vhostDecl
	var ok bool
	switch web {
	case "nginx":
		decls, ok = nginxDecls(dump)
	case "apache":
		decls, ok = apacheDecls(dump)
	}
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(decls))
	for _, d := range decls {
		if _, seen := out[d.name]; !seen && d.name != "" {
			out[d.name] = d.file
		}
	}
	return out, true
}

// DeclaredFile trả file khai name trong decl (kết quả của DeclaredNames), ""
// khi không ai khai. Khớp đúng thắng (như nginx); không có thì xét tên
// wildcard (soát cuối C9): "*.x" khớp mọi tên con của x ở mọi độ sâu (không
// khớp chính x), ".x" của nginx khớp x và mọi tên con, còn lại "*" và "?" như
// ServerAlias của Apache và "x.*" của nginx (path.Match: tên không có "/").
// Nhiều wildcard cùng khớp thì lấy file đứng trước theo tên (ổn định).
//
// ponytail: bỏ qua server_name dạng regex (~...) của nginx; đọc regex của
// người khác để đoán tên nào khớp không đáng rủi ro sai. Cần thì dịch bằng
// regexp (cú pháp PCRE khác RE2 ở vài chỗ).
func DeclaredFile(decl map[string]string, name string) string {
	if f, ok := decl[name]; ok {
		return f
	}
	best := ""
	for pat, f := range decl {
		if strings.HasPrefix(pat, "~") || !strings.ContainsAny(pat, "*?") && !strings.HasPrefix(pat, ".") {
			continue
		}
		match := false
		switch {
		case strings.HasPrefix(pat, "."):
			match = name == pat[1:] || strings.HasSuffix(name, pat)
		case strings.HasPrefix(pat, "*."):
			match = strings.HasSuffix(name, pat[1:])
		default:
			match, _ = path.Match(pat, name)
		}
		if match && (best == "" || f < best) {
			best = f
		}
	}
	return best
}
