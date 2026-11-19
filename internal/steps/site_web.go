package steps

import (
	"errors"
	"fmt"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Các bước web của một site (mục 6.2, bước 5 và 8; hoàn tác ở mục 6.2 và 6.4):
// pool PHP-FPM, vhost Nginx hoặc Apache, .htaccess của WordPress, và gỡ chúng.
//
// Không có kiểu Step mới: mỗi hàm chỉ dựng các File, Symlink, Absent của M1
// với nội dung từ internal/templates và đúng lệnh Test, Reload. Nhờ vậy việc
// giữ file cũ, kiểm cú pháp, trả file cũ khi kiểm sai, và Undo đã có sẵn test.

// Các Step dùng ở đây đã được khai báo thoả engine.Step trong common.go; dòng
// này nhắc lại để file tự đứng được nếu common.go đổi.
var _ engine.Step = (*File)(nil)

// webCheckLayout từ chối Layout zero hay Layout bị sửa tay. Các hàm sinh
// template cũng kiểm, nhưng SiteWebAbsent không đi qua template.
func webCheckLayout(l site.Layout) error {
	if l.Domain.IsZero() {
		return errors.New("site chưa có domain (Layout zero)")
	}
	if l != site.LayoutOf(l.Domain) {
		return fmt.Errorf("tên và đường dẫn của %s không khớp site.LayoutOf", l.Domain)
	}
	return nil
}

// webPHPCmds trả lệnh kiểm cú pháp và lệnh reload của PHP-FPM bản v. v phải
// khớp phpVersion (php.go) trước khi ghép vào tên chương trình: "8.3; x" mà
// lọt qua thì thành tên chương trình lạ.
func webPHPCmds(v string) (test, reload *system.Cmd, err error) {
	if !phpVersion.MatchString(v) {
		return nil, nil, fmt.Errorf("bản PHP %q không hợp lệ (dạng 8.3)", v)
	}
	return &system.Cmd{Name: "php-fpm" + v, Args: []string{"-t"}},
		&system.Cmd{Name: "systemctl", Args: []string{"reload", "php" + v + "-fpm"}}, nil
}

// webVhost là đường dẫn và lệnh của vhost theo web server.
type webVhost struct {
	conf, link   string
	test, reload *system.Cmd
}

func webVhostOf(l site.Layout, web string) (webVhost, error) {
	switch web {
	case "nginx":
		return webVhost{
			conf:   l.NginxConf,
			link:   l.NginxLink,
			test:   &system.Cmd{Name: "nginx", Args: []string{"-t"}},
			reload: &system.Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}},
		}, nil
	case "apache":
		return webVhost{
			conf:   l.ApacheConf,
			link:   l.ApacheLink,
			test:   &system.Cmd{Name: "apachectl", Args: []string{"configtest"}},
			reload: &system.Cmd{Name: "systemctl", Args: []string{"reload", "apache2"}},
		}, nil
	}
	return webVhost{}, fmt.Errorf("web server %q không hợp lệ (chỉ nhận nginx hoặc apache)", web)
}

// SitePool ghi pool PHP-FPM của site, kiểm bằng php-fpm<v> -t rồi reload.
func SitePool(l site.Layout, phpVersion string) (engine.Step, error) {
	test, reload, err := webPHPCmds(phpVersion)
	if err != nil {
		return nil, fmt.Errorf("dựng bước pool PHP-FPM: %w", err)
	}
	conf, err := templates.SitePool(l)
	if err != nil {
		return nil, fmt.Errorf("dựng bước pool PHP-FPM: %w", err)
	}
	d := l.Domain.String()
	return &File{Path: l.PoolConf(phpVersion), Content: conf, Perm: 0o644, Owner: system.Root,
		Test: test, Reload: reload, WaitFor: l.Socket,
		Title: i18n.T("Ghi pool PHP-FPM "+l.Pool+" cho "+d, "Write PHP-FPM pool "+l.Pool+" for "+d),
		Why: i18n.T(
			"Pool riêng cho PHP của site chạy dưới user "+l.User+", chỉ mở được file trong "+l.Dir+". Thiếu nó thì web server không có chỗ chuyển file .php tới (502); dùng chung pool thì một site bị chiếm đọc được file của site khác.",
			"A dedicated pool runs the site's PHP as "+l.User+" and only lets it open files under "+l.Dir+". Without it the web server has nowhere to send .php files (502); with a shared pool a compromised site can read the other sites' files.")}, nil
}

// SiteVhost ghi vhost vào sites-available rồi bật bằng symlink ở
// sites-enabled. Cả hai bước kiểm cú pháp rồi reload web server.
func SiteVhost(l site.Layout, web, acmeRoot string) ([]engine.Step, error) {
	v, err := webVhostOf(l, web)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost: %w", err)
	}
	var conf []byte
	if web == "nginx" {
		conf, err = templates.NginxSite(l, acmeRoot)
	} else {
		conf, err = templates.ApacheSite(l, acmeRoot)
	}
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost %s: %w", web, err)
	}
	d := l.Domain.String()
	return []engine.Step{
		&File{Path: v.conf, Content: conf, Perm: 0o644, Owner: system.Root, Test: v.test, Reload: v.reload,
			Title: i18n.T("Ghi vhost "+web+" cho "+d, "Write "+web+" vhost for "+d),
			Why: i18n.T(
				"Vhost nói cho "+web+" biết request tới "+d+" lấy file ở đâu và chuyển .php cho pool nào. Thiếu nó thì request rơi vào site mặc định của tpan (444 hoặc 403).",
				"The vhost tells "+web+" where to find files for "+d+" and which pool gets its .php requests. Without it requests fall into tpan's default site (444 or 403).")},
		&Symlink{Target: v.conf, Link: v.link, Test: v.test, Reload: v.reload},
	}, nil
}

// SiteHtaccess (chỉ Apache) ghi .htaccess chuẩn của WordPress. Chủ là user
// của site, nhóm www-data, 0640: Apache đọc qua quyền nhóm, WordPress (chạy
// dưới user của site) tự viết lại được khi lưu permalink. Apache đọc
// .htaccess ở mỗi request nên không cần Test hay Reload.
func SiteHtaccess(l site.Layout) (engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước .htaccess: %w", err)
	}
	return &File{Path: l.Public + "/.htaccess", Content: templates.WPHtaccess(), Perm: 0o640,
		Owner: system.Owner{User: l.User, Group: site.WebGroup}}, nil
}

// SiteWebAbsent gỡ link vhost, file vhost, rồi pool. Vhost gỡ trước để không
// lúc nào web server trỏ vào socket của một pool đã mất (khách sẽ thấy 502).
// Mỗi bước Absent của M1 giữ nội dung cũ nên Undo trả lại được.
func SiteWebAbsent(l site.Layout, web, phpVersion string) ([]engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước gỡ web của site: %w", err)
	}
	v, err := webVhostOf(l, web)
	if err != nil {
		return nil, fmt.Errorf("dựng bước gỡ web của site: %w", err)
	}
	ptest, preload, err := webPHPCmds(phpVersion)
	if err != nil {
		return nil, fmt.Errorf("dựng bước gỡ web của site: %w", err)
	}
	d := l.Domain.String()
	why := i18n.T(
		"Gỡ cấu hình web của "+d+" trước khi xoá dữ liệu: web server và PHP-FPM thôi phục vụ site, tiến trình PHP của user "+l.User+" thoát để xoá được user. Bước này hoàn tác được.",
		"Remove "+d+"'s web configuration before deleting data: the web server and PHP-FPM stop serving the site and the PHP processes of "+l.User+" exit so the user can be deleted. This step can be undone.")
	return []engine.Step{
		&Absent{Path: v.link, Test: v.test, Reload: v.reload, Danger: true, Why: why,
			Title: i18n.T("Tắt vhost của "+d, "Disable the vhost of "+d)},
		&Absent{Path: v.conf, Test: v.test, Reload: v.reload, Danger: true, Why: why,
			Title: i18n.T("Gỡ file vhost của "+d, "Remove the vhost file of "+d)},
		&Absent{Path: l.PoolConf(phpVersion), Test: ptest, Reload: preload, Danger: true, Why: why,
			Title: i18n.T("Gỡ pool PHP-FPM "+l.Pool, "Remove PHP-FPM pool "+l.Pool)},
	}, nil
}
