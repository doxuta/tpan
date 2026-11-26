package steps

// Nới bảng tên miền của nginx (v0.2.14).
//
// nginx xếp mọi server_name vào một bảng băm, mỗi ô mặc định 64 byte (32 trên
// vài nền tảng). Một tên miền dài cỡ 48 ký tự trở lên (cộng bản www. của nó)
// không vừa ô, `nginx -t` báo "could not build server_names_hash, you should
// increase server_names_hash_bucket_size: 64" và site không tạo được. Lỗi này
// thấy khi thử tạo site tên dài trên máy ảo (26/11/2026); tên miền hợp lệ dài
// tới 253 ký tự nên panel phải nhận được.
//
// Cách sửa: một file của tpan trong /etc/nginx/conf.d (nginx.conf của cả gói
// Ubuntu lẫn gói nginx.org đều include conf.d/*.conf trong khối http). File
// được ghi ngay trước khi ghi vhost của một site nginx, trong cùng bước File
// (File.Before): lệnh `nginx -t` của bước đó kiểm luôn cả hai, và máy cài bằng
// bản cũ tự được nới ở lần ghi vhost kế tiếp, không cần bước nâng cấp riêng.

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"

	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

const (
	// NginxNamesConf là file tpan đặt trần bảng tên của nginx.
	NginxNamesConf = "/etc/nginx/conf.d/tpan-names.conf"
	// nginxOrgInclude là conf.d/tpan.conf của máy dùng gói nginx.org. Bản trước
	// v0.2.14 của file này có dòng server_names_hash_bucket_size 64; để nguyên
	// thì hai file cùng đặt một chỉ thị và nginx từ chối ("is duplicate").
	nginxOrgInclude = "/etc/nginx/conf.d/tpan.conf"
	nginxMainConf   = "/etc/nginx/nginx.conf"
	nginxConfD      = "/etc/nginx/conf.d"
)

// namesDirectiveRE khớp một dòng đang có hiệu lực (không phải chú thích) đặt
// server_names_hash_bucket_size.
var namesDirectiveRE = regexp.MustCompile(`(?m)^[ \t]*server_names_hash_bucket_size[ \t]+[0-9]+[kKmM]?[ \t]*;[^\n]*\n?`)

// readIfExists trả nội dung file; file không có thì ("", false, nil).
func readIfExists(sys system.System, path string) (string, bool, error) {
	data, err := sys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc %s: %w", path, err)
	}
	return string(data), true, nil
}

// EnsureNginxNames ghi NginxNamesConf khi nó thiếu hoặc khác nội dung của
// templates.NginxNames, và gỡ dòng server_names_hash_bucket_size cũ khỏi
// conf.d/tpan.conf của gói nginx.org. Không chạy lệnh nào: bước gọi nó tự kiểm
// cú pháp và reload. Trả hàm hoàn lại (nil khi không đổi gì).
//
// Người quản trị đã tự đặt chỉ thị này trong nginx.conf thì không chen vào.
func EnsureNginxNames(sys system.System) (func() error, error) {
	main, _, err := readIfExists(sys, nginxMainConf)
	if err != nil {
		return nil, err
	}
	if namesDirectiveRE.MatchString(main) {
		return nil, nil
	}
	want := templates.NginxNames()
	names, haveNames, err := readIfExists(sys, NginxNamesConf)
	if err != nil {
		return nil, err
	}
	org, haveOrg, err := readIfExists(sys, nginxOrgInclude)
	if err != nil {
		return nil, err
	}
	fixNames := !haveNames || names != string(want)
	fixOrg := haveOrg && namesDirectiveRE.MatchString(org)
	if !fixNames && !fixOrg {
		return nil, nil
	}

	prevNames, err := capture(sys, NginxNamesConf)
	if err != nil {
		return nil, fmt.Errorf("ghi %s: %w", NginxNamesConf, err)
	}
	var prevOrg saved
	if fixOrg {
		if prevOrg, err = capture(sys, nginxOrgInclude); err != nil {
			return nil, fmt.Errorf("ghi %s: %w", nginxOrgInclude, err)
		}
	}
	// Thư mục conf.d do chính hàm này tạo (máy thật luôn có sẵn) thì hoàn lại cũng
	// gỡ nó, để Apply rồi Undo trả máy về đúng như trước.
	createdDir := false
	if _, err := sys.Stat(nginxConfD); errors.Is(err, fs.ErrNotExist) {
		createdDir = true
	}
	restore := func() error {
		if fixOrg {
			if err := prevOrg.restore(sys); err != nil {
				return err
			}
		}
		if fixNames {
			if err := prevNames.restore(sys); err != nil {
				return err
			}
			if createdDir {
				// Còn file khác trong thư mục thì Remove lỗi: để nguyên, không sao.
				_ = sys.Remove(nginxConfD)
			}
		}
		return nil
	}
	if fixOrg {
		fi, err := sys.Stat(nginxOrgInclude)
		if err != nil {
			return nil, fmt.Errorf("kiểm %s: %w", nginxOrgInclude, err)
		}
		if err := sys.WriteFileAtomic(nginxOrgInclude, []byte(namesDirectiveRE.ReplaceAllString(org, "")), fi.Mode.Perm(), fi.Owner); err != nil {
			return nil, fmt.Errorf("ghi %s: %w", nginxOrgInclude, err)
		}
	}
	if fixNames {
		// conf.d có sẵn ở cả hai gói nginx; tạo khi thiếu để không phụ thuộc điều đó.
		err := sys.MkdirAll(nginxConfD, 0o755, system.Root)
		if err == nil {
			err = sys.WriteFileAtomic(NginxNamesConf, want, 0o644, system.Root)
		}
		if err != nil {
			if rerr := restore(); rerr != nil {
				return nil, fmt.Errorf("ghi %s: %w (trả lại file cũ cũng lỗi: %v)", NginxNamesConf, err, rerr)
			}
			return nil, fmt.Errorf("ghi %s: %w", NginxNamesConf, err)
		}
	}
	return restore, nil
}
