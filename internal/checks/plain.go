package checks

// Phép kiểm cho site PHP trống (site.AppPHP, M29): cùng chặng và cùng mã (id)
// với bản của site WordPress để bản đồ đường đi và sự cố dùng lại được, nhưng
// không đòi WordPress: không WP-CLI, không wp-config.php, trang chủ không phải
// là WordPress.

import (
	"context"
	"github.com/doxuta/tpan/internal/account"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// DBExists: DB của site và user DB cùng tên đều còn trong MariaDB. Site PHP
// trống tự giữ mật khẩu DB trong mã của nó, nên TPAN không thử đăng nhập hộ
// (bản WordPress, DBAccess, đăng nhập bằng wp-config.php).
func DBExists(l site.Layout) Check {
	b := siteBase("db.access", StageDB, l,
		i18n.T("DB của site và user DB còn trong MariaDB", "The site's database and DB user exist in MariaDB"))
	if g := siteGuard(b, l, false); g != nil {
		return g
	}
	if !siteDBRE.MatchString(l.DB) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("tên DB của site không hợp lệ", "the site DB name is not valid"))
		})
	}
	// Tên DB đã khớp siteDBRE (chữ, số, gạch dưới) nên đặt trong nháy đơn là an toàn.
	sql := "SELECT (SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='" + l.DB +
		"'), (SELECT COUNT(*) FROM mysql.global_priv WHERE User='" + l.DB + "' AND Host='localhost')"
	cmd := system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(sql + "\n")}
	recheck := `mariadb -e "SHOW GRANTS FOR '` + l.DB + `'@'localhost'"`
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		f := strings.Fields(res.Stdout)
		if res.ExitCode != 0 || len(f) != 2 {
			return b.unknown(i18n.T("không hỏi được MariaDB (mã "+strconv.Itoa(res.ExitCode)+")", "could not query MariaDB (exit "+strconv.Itoa(res.ExitCode)+")"), ev)
		}
		switch {
		case f[0] == "0":
			return b.bad(Fail,
				i18n.T("DB "+l.DB+" không còn trong MariaDB", "The database "+l.DB+" is no longer in MariaDB"),
				i18n.T("DB của site đã bị xoá ngoài TPAN. Mã của site sẽ báo lỗi kết nối DB. Khôi phục từ một bản backup của site.",
					"The site's database was dropped outside TPAN. The site's code will report a database connection error. Restore it from a backup of the site."),
				[]string{`mariadb -e "SHOW DATABASES"`, recheck}, ev)
		case f[1] == "0":
			return b.bad(Fail,
				i18n.T("User DB "+l.DB+" không còn trong MariaDB", "The DB user "+l.DB+" is no longer in MariaDB"),
				i18n.T("User DB của site đã bị xoá ngoài TPAN. Đổi mật khẩu DB của site ở màn Cơ sở dữ liệu không tạo lại user; chạy lại tạo site để dựng lại.",
					"The site's DB user was dropped outside TPAN. Run the site creation again to rebuild it."),
				[]string{recheck, line("tpan", "site", "add", l.Domain.String())}, ev)
		}
		return b.pass(i18n.T("DB "+l.DB+" và user DB cùng tên đều còn", "The database "+l.DB+" and its DB user both exist"), ev)
	})
}

// HTTPPlain gọi trang chủ của site PHP trống ngay trên máy (cổng 80, hoặc 443
// khi https). Đạt khi web server và PHP trả lời (mã dưới 500); 4xx là cảnh báo
// vì đó là chuyện nội dung của chủ site (thư mục public trống, mã tự chặn).
func HTTPPlain(l site.Layout, https bool) Check {
	kind, port, scheme := "http.site", "80", "http://"
	if https {
		kind, port, scheme = "https.site", "443", "https://"
	}
	b := siteBase(kind, StageEndToEnd, l,
		i18n.T("Gọi "+strings.ToUpper(strings.TrimSuffix(scheme, "://"))+" vào site có trả lời", "A request to the site over "+strings.ToUpper(strings.TrimSuffix(scheme, "://"))+" gets an answer"))
	if g := siteGuard(b, l, true, l.LogDir, l.PHPErrorLog, l.Public); g != nil {
		return g
	}
	dom := l.Domain.String()
	url := scheme + dom + "/"
	resolve := dom + ":" + port + ":127.0.0.1"
	args := []string{"-sS"}
	if https {
		args = append(args, "-k")
	}
	args = append(args, "--max-time", "15", "--resolve", resolve, "-o", "/dev/null", "-w", "%{http_code}", url)
	cmd := system.Cmd{Name: "curl", Args: args}
	recheck := line("curl", args...)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		code := strings.TrimSpace(res.Stdout)
		switch {
		case res.ExitCode == curlConnectFailed || res.ExitCode == curlTimeout:
			return b.bad(Fail,
				i18n.T("Không kết nối được tới 127.0.0.1 cổng "+port+", hoặc web server không trả lời trong 15 giây", "Cannot connect to 127.0.0.1 port "+port+", or the web server did not answer within 15 seconds"),
				i18n.T("Web server không chạy, không nghe cổng "+port+", hoặc PHP treo.", "The web server is not running, does not listen on port "+port+", or PHP is hung."),
				[]string{line("systemctl", "status", "nginx", "apache2", "--no-pager"), line("ss", "-tlnp"), line("tail", "-n", "20", l.LogDir+"/error.log"), recheck}, ev)
		case res.ExitCode == curlEmptyReply || res.ExitCode == curlRecvFailure:
			return b.bad(Fail,
				i18n.T("Web server đóng kết nối không trả gì cho "+dom, "The web server closed the connection without replying for "+dom),
				i18n.T("Request mang tên "+dom+" rơi vào site mặc định: vhost của site chưa được bật hoặc chưa được nạp.",
					"The request for "+dom+" fell into the default site: the site vhost is not enabled or not loaded."),
				siteVhostFix(dom, recheck), ev)
		case res.ExitCode != 0 || len(code) != 3:
			return b.unknown(i18n.T("curl báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(res.Stderr, "curl")+")", "curl failed (exit "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(res.Stderr, "curl")+")"), ev)
		case code[0] == '5':
			return b.bad(Fail,
				i18n.T("Site trả "+code+": PHP báo lỗi hoặc web server không nói chuyện được với PHP", "The site answered "+code+": PHP reported an error, or the web server cannot talk to PHP"),
				i18n.T("500 thường là lỗi trong mã PHP của site (xem log lỗi PHP). 502, 503, 504 là pool PHP của site không chạy hoặc quá tải.",
					"500 is usually an error in the site's PHP code (see the PHP error log). 502, 503 and 504 mean the site's PHP pool is not running or is overloaded."),
				[]string{line("tail", "-n", "20", l.PHPErrorLog), line("tail", "-n", "20", l.LogDir+"/error.log"), line("ls", "-l", "/run/php"), recheck}, ev)
		case code[0] == '4':
			return b.bad(Warn,
				i18n.T("Site trả "+code+": web server và PHP chạy, nhưng trang chủ không hiện", "The site answered "+code+": the web server and PHP work, but the home page does not show"),
				i18n.T("Thư mục public của site chưa có index.php hay index.html (403, 404), hoặc mã của site tự chặn. Đây là nội dung của site, TPAN không sửa.",
					"The site's public directory has no index.php or index.html (403, 404), or the site's code blocks the request. This is the site's content; TPAN does not change it."),
				[]string{line("ls", "-l", l.Public), recheck}, ev)
		}
		return b.pass(i18n.T("Site trả "+code, "The site answered "+code), ev)
	})
}

// SiteChecksPlain là SiteChecks / SiteChecksSSL của site PHP trống (M29): cùng
// các chặng theo đường đi của request, không có phép kiểm nào của WordPress.
func SiteChecksPlain(web, phpVersion string, l site.Layout, ssl bool) []Check {
	var cs []Check
	if ssl {
		cs = append(cs, TLSCert(l))
	}
	cs = append(cs, VhostEnabled(web, l), PoolSocket(l, phpVersion), DBExists(l))
	if ssl {
		cs = append(cs, HTTPRedirect(l))
	}
	return append(cs, HTTPPlain(l, ssl))
}

// SiteChecksPlainCaged là SiteChecksPlain của site PHP trống trong lồng.
func SiteChecksPlainCaged(web string, l site.Layout, want account.Limits, ssl bool) []Check {
	var cs []Check
	if ssl {
		cs = append(cs, TLSCert(l))
	}
	cs = append(cs, VhostEnabled(web, l), CageSocket(l), CageRuntime(l), Heavy(CageFence(l)), CageLimits(l, want), DBExists(l))
	if ssl {
		cs = append(cs, HTTPRedirect(l))
	}
	return append(cs, HTTPPlain(l, ssl))
}
