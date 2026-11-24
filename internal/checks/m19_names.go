package checks

// M19 (S13 của docs/thay-doi/M19-thiet-ke.md): tên phụ mới không được là tên
// đang khai ở một vhost khác trên máy (kể cả vhost admin dựng tay, không có
// trong DB của tpan). nginx thấy server_name trùng chỉ cảnh báo và khối nạp
// trước thắng; tpan-*.conf đứng trước nhiều file theo thứ tự chữ cái, nên tên
// phụ sẽ lặng lẽ chiếm lưu lượng của vhost kia.

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
