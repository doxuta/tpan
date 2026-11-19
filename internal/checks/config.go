package checks

import (
	"context"
	"regexp"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// configCheck là khuôn chung của ba phép kiểm cú pháp config: chạy một lệnh
// "test config" của web server hoặc PHP-FPM. Cả ba đều in kết quả ra STDERR và
// báo đạt/không đạt bằng mã thoát, nên chỉ nhìn mã thoát (Evidence vẫn giữ cả
// stderr làm bằng chứng). Riêng Apache còn in cảnh báo AH00558 về ServerName
// ngay cả khi config đúng, mà mã thoát vẫn là 0: nhìn chữ thì dễ báo nhầm.
type configCheck struct {
	b       base
	name    string // tên hiển thị: "nginx", "Apache", "PHP-FPM 8.3"
	cmd     system.Cmd
	markers []string // dấu hiệu của dòng lỗi chính trong output
	ls      string   // lệnh liệt kê chỗ để file config, cho ManualFix
}

func (c configCheck) Run(ctx context.Context, sys system.System) Result {
	res, ev, err := execCmd(ctx, sys, c.cmd)
	if err != nil {
		return c.b.cantRun(err, ev)
	}
	if res.ExitCode == 0 {
		return c.b.pass(i18n.T("Cấu hình "+c.name+" hợp lệ", c.name+" configuration is valid"), ev)
	}
	why := firstLineWith(ev.Output, c.markers...)
	return c.b.bad(Fail,
		i18n.T("Cấu hình "+c.name+" có lỗi: "+why, c.name+" configuration has an error: "+why),
		i18n.T("Có file cấu hình sai cú pháp, thường do ai đó sửa tay. Dòng lỗi trong bằng chứng cho biết tên file và số dòng. Web server đang chạy vẫn giữ bản cũ, nhưng reload hoặc khởi động lại sẽ hỏng.",
			"A config file has a syntax error, usually from a manual edit. The error line in the evidence names the file and line number. A running server keeps its old config, but a reload or restart will fail."),
		[]string{c.ls, c.cmd.String()},
		ev)
}

// NginxConfig kiểm cú pháp config của nginx bằng `nginx -t`.
func NginxConfig() Check {
	return configCheck{
		b: base{id: "config.nginx:nginx", stage: StageWeb,
			title: i18n.T("Cấu hình nginx hợp lệ", "nginx configuration is valid")},
		name:    "nginx",
		cmd:     system.Cmd{Name: "nginx", Args: []string{"-t"}},
		markers: []string{"[emerg]", "[alert]", "failed"},
		ls:      line("ls", "-l", "/etc/nginx/sites-enabled"),
	}
}

// ApacheConfig kiểm cú pháp config của Apache bằng `apachectl configtest`.
func ApacheConfig() Check {
	return configCheck{
		b: base{id: "config.apache:apache2", stage: StageWeb,
			title: i18n.T("Cấu hình Apache hợp lệ", "Apache configuration is valid")},
		name:    "Apache",
		cmd:     system.Cmd{Name: "apachectl", Args: []string{"configtest"}},
		markers: []string{"Syntax error", "AH0", "Invalid command"},
		ls:      line("ls", "-l", "/etc/apache2/sites-enabled"),
	}
}

// phpVersionRE là dạng bản PHP: "8.3". Chặn sớm để tên chương trình
// "php-fpm<bản>" không bao giờ chứa ký tự lạ.
var phpVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// PHPFPMConfig kiểm cú pháp config PHP-FPM bằng `php-fpm<bản> -t`.
// version dạng "8.3"; sai dạng thì Unknown (không chạy lệnh nào).
func PHPFPMConfig(version string) Check {
	b := base{id: "config.phpfpm:" + version, stage: StagePHP,
		title: i18n.T("Cấu hình PHP-FPM "+version+" hợp lệ", "PHP-FPM "+version+" configuration is valid")}
	if !phpVersionRE.MatchString(version) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("bản PHP phải có dạng số.số, ví dụ 8.3 (nhận được \""+version+"\")",
				"PHP version must look like 8.3 (got \""+version+"\")"))
		})
	}
	return configCheck{
		b:       b,
		name:    "PHP-FPM " + version,
		cmd:     system.Cmd{Name: "php-fpm" + version, Args: []string{"-t"}},
		markers: []string{"ERROR", "WARNING"},
		ls:      line("ls", "-l", "/etc/php/"+version+"/fpm/pool.d"),
	}
}
