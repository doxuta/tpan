package checks

// Phép kiểm của site tạm khoá (M13, mục 6.12 của docs/KE-HOACH.md).
//
// Site tạm khoá trả 403 kèm trang tạm khoá cho mọi đường dẫn và không có pool
// PHP: các chặng PHP, WordPress, DB và phép gọi HTTP thường chắc chắn không
// đạt, mà không đạt đó là ý muốn chứ không phải sự cố. Health thay chúng bằng
// một kết quả "tạm khoá" mỗi chặng (SuspendedStage: Unknown, cờ Suspended, nên
// không mở hay đóng sự cố), và thay phép gọi HTTP thường bằng HTTPSuspended.

import (
	"context"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// SuspendedStage là kết quả thay cho cả một chặng không được kiểm vì site
// (db=false) hay user DB của site (db=true) đang tạm khoá. ID
// "suspended.<chặng>:<domain>": ID riêng, không mượn ID của phép kiểm thường
// (php.socket, db.access...). Lý do: Unknown không đóng sự cố, nên mượn ID thì
// sự cố mở từ trước khi khoá cứ mở suốt thời gian khoá; ID riêng thì phép kiểm
// thường biến mất khỏi lần chạy đầy đủ và health đóng sự cố của nó với lý do
// "gone" như với site đã xoá (M4).
func SuspendedStage(stage Stage, l site.Layout, db bool) Check {
	b := siteBase("suspended."+string(stage), stage, l,
		i18n.T("Chặng này không được kiểm khi đang tạm khoá", "This stage is not checked while suspended"))
	sum := i18n.T("Site đang tạm khoá; chặng này không được kiểm", "The site is suspended; this stage is not checked")
	if db {
		sum = i18n.T("DB đang tạm khoá; chặng này không được kiểm", "The database is suspended; this stage is not checked")
	}
	return checkFunc(func(context.Context, system.System) Result {
		r := b.result(Unknown, sum)
		r.Suspended = true
		return r
	})
}

// HTTPSuspended (StageEndToEnd, "http.suspended:<domain>") gọi thật vào site
// đang tạm khoá, nối thẳng vào 127.0.0.1 như HTTPSite: http (cổng 80), và cả
// https (cổng 443, -k) khi ssl. Đạt khi mọi lần gọi trả 403 VÀ thân có
// templates.SuspendedMarker: 403 không có chuỗi đó là 403 của thứ khác (site
// mặc định của Apache), còn 200 là site vẫn đang được phục vụ.
//
// Chỉ gọi một lần, không thử lại: health chạy phép kiểm này mỗi 5 phút và
// không được chờ. Kiểm chứng ngay sau khi tạm khoá (reload trả về trước khi
// cấu hình mới có hiệu lực) tự bọc thử lại ở core.
func HTTPSuspended(l site.Layout, ssl bool) Check {
	b := siteBase("http.suspended", StageEndToEnd, l,
		i18n.T("Gọi vào site đang tạm khoá trả 403 kèm trang tạm khoá", "A request to the suspended site returns 403 with the suspension page"))
	if g := siteGuard(b, l, true, l.LogDir); g != nil {
		return g
	}
	dom := l.Domain.String()
	type call struct {
		port, url, resolve string
		cmd                system.Cmd
		recheck            string
	}
	mk := func(scheme, port string, insecure bool) call {
		url, resolve := scheme+"://"+dom+"/", dom+":"+port+":127.0.0.1"
		args, re := []string{"-sS"}, []string{"-sS"}
		if insecure {
			// Chứng chỉ staging không được tin; chứng chỉ có phép kiểm riêng (TLSCert).
			args, re = append(args, "-k"), append(re, "-k")
		}
		args = append(args, "--max-time", "15", "--resolve", resolve, "-w", siteHTTPMarker+"%{http_code}", url)
		re = append(re, "--max-time", "15", "--resolve", resolve, "-o", "/dev/null", "-w", "%{http_code}\n", url)
		return call{port: port, url: url, resolve: resolve, cmd: system.Cmd{Name: "curl", Args: args}, recheck: line("curl", re...)}
	}
	calls := []call{mk("http", "80", false)}
	if ssl {
		calls = append(calls, mk("https", "443", true))
	}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		var evs []Evidence
		for _, c := range calls {
			res, ev, err := execCmd(ctx, sys, c.cmd)
			if err != nil {
				return b.cantRun(err, append(evs, ev)...)
			}
			body, code := siteSplitCode(res.Stdout)
			ev.Output = siteHTTPEvidence(code, body, res.Stderr)
			evs = append(evs, ev)
			if r, handled := sslCurlProblem(b, l, res, ev, c.port, c.recheck); handled {
				r.Evidence = evs
				return r
			}
			switch {
			case code == "":
				return b.unknown(i18n.T("curl không in mã trạng thái", "curl printed no status code"), evs...)
			case code == "403" && strings.Contains(body, templates.SuspendedMarker):
				continue
			case code == "403":
				return b.bad(Fail,
					i18n.T(c.url+" trả 403 nhưng không phải trang tạm khoá của tpan", c.url+" answered 403 but not with tpan's suspension page"),
					i18n.T("Trang "+templates.SuspendedPage+" thiếu hoặc web server không đọc được, hoặc request rơi vào site mặc định thay vì vhost tạm khoá của "+dom+".",
						"The page "+templates.SuspendedPage+" is missing or unreadable by the web server, or the request fell into the default site instead of the suspended vhost of "+dom+"."),
					[]string{line("ls", "-l", templates.SuspendedPage), line("grep", "-Rl", dom, "/etc/nginx/sites-enabled", "/etc/apache2/sites-enabled"),
						line("tpan", "health", dom), c.recheck}, evs...)
			case code == "200":
				return b.bad(Fail,
					i18n.T(c.url+" vẫn trả 200: site chưa bị khoá", c.url+" still answers 200: the site is not blocked"),
					i18n.T("Vhost thường của "+dom+" còn bật (vhost tạm khoá bị ghi đè, hoặc web server chưa nạp lại), nên khách vẫn vào được site.",
						"The normal vhost of "+dom+" is still active (the suspended vhost was overwritten, or the web server was not reloaded), so visitors still reach the site."),
					[]string{line("grep", "-Rl", dom, "/etc/nginx/sites-enabled", "/etc/apache2/sites-enabled"),
						line("tpan", "health", dom), c.recheck}, evs...)
			}
			return b.bad(Fail,
				i18n.T(c.url+" trả "+code+", đáng lẽ 403 kèm trang tạm khoá", c.url+" answered "+code+"; it should be 403 with the suspension page"),
				i18n.T("Vhost của "+dom+" không phải bản tạm khoá của tpan, hoặc web server chưa nạp lại.",
					"The vhost of "+dom+" is not tpan's suspended version, or the web server was not reloaded."),
				[]string{line("grep", "-Rl", dom, "/etc/nginx/sites-enabled", "/etc/apache2/sites-enabled"),
					line("tpan", "health", dom), c.recheck}, evs...)
		}
		return b.pass(i18n.T("Site trả 403 kèm trang tạm khoá", "The site answers 403 with the suspension page"), evs...)
	})
}
