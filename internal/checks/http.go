package checks

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Mã thoát của curl dùng ở đây (curl.se/docs/manpage.html, mục EXIT CODES):
//
//	0  xong, có nhận được phản hồi HTTP
//	7  không kết nối được (cổng đóng, không ai nghe)
//	28 hết thời gian chờ
//	52 máy chủ đóng kết nối mà không trả gì ("Empty reply from server")
const (
	curlConnectFailed = 7
	curlTimeout       = 28
	curlEmptyReply    = 52
)

var httpCodeRE = regexp.MustCompile(`^[0-9]{3}$`)

// HTTPDefault kiểm site mặc định (`tpan-default`) bằng một request thật vào
// http://127.0.0.1/ , tức là request không khớp domain nào:
//
//   - web "nginx": đạt khi máy chủ ĐÓNG kết nối không trả gì. Đó là cách mã 444
//     của nginx hoạt động: không có dòng trạng thái nào, nên curl báo "Empty
//     reply from server" (mã thoát 52) và -w in "000".
//   - web "apache": đạt khi trả 403.
//
// Đây là kiểm bằng con đường độc lập: file vhost có thể đúng cú pháp và nằm
// đúng chỗ mà vẫn không phải site hứng request lạ; chỉ gọi thật mới biết.
// Không theo chuyển hướng (không có -L) vì 301/302 ở đây chính là lỗi cần thấy,
// và không dùng -f vì 403 sẽ biến thành mã thoát 22, lẫn với lỗi khác.
//
// Phân biệt hai kiểu hỏng để gợi ý đúng việc: "không kết nối được" (xem dịch
// vụ) khác "kết nối được nhưng trả sai" (xem site mặc định).
func HTTPDefault(web string) Check {
	b := base{id: "http.default:" + web, stage: StageWeb}
	var unit, expectVi, expectEn string
	var ls, test string
	switch web {
	case "nginx":
		unit = "nginx"
		expectVi, expectEn = "đóng kết nối không trả gì (mã 444)", "close the connection with no reply (code 444)"
		b.title = i18n.T("Site mặc định của nginx đóng kết nối request lạ", "The nginx default site drops unknown requests")
		ls, test = line("ls", "-l", "/etc/nginx/sites-enabled"), line("nginx", "-t")
	case "apache":
		unit = "apache2"
		expectVi, expectEn = "trả 403", "answer 403"
		b.title = i18n.T("Site mặc định của Apache trả 403 cho request lạ", "The Apache default site answers 403 to unknown requests")
		ls, test = line("ls", "-l", "/etc/apache2/sites-enabled"), line("apachectl", "configtest")
	default:
		b.title = i18n.T("Site mặc định trả lời đúng", "The default site answers correctly")
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("web server phải là nginx hoặc apache (nhận được \""+web+"\")", "web server must be nginx or apache (got \""+web+"\")"))
		})
	}

	cmd := system.Cmd{Name: "curl", Args: []string{
		"-sS", "--max-time", "5", "-o", "/dev/null", "-w", "%{http_code}", "http://127.0.0.1/",
	}}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		code := strings.TrimSpace(res.Stdout)
		recheck := cmd.String()
		code0 := res.ExitCode == 0 && httpCodeRE.MatchString(code)

		switch {
		case web == "nginx" && res.ExitCode == curlEmptyReply:
			return b.pass(i18n.T("nginx đóng kết nối request lạ như mong đợi (mã 444)", "nginx drops unknown requests as expected (code 444)"), ev)
		case web == "apache" && code0 && code == "403":
			return b.pass(i18n.T("Apache trả 403 cho request lạ như mong đợi", "Apache answers 403 to unknown requests as expected"), ev)

		case res.ExitCode == curlConnectFailed || res.ExitCode == curlTimeout:
			vi, en := "không kết nối được tới 127.0.0.1 cổng 80", "cannot connect to 127.0.0.1 port 80"
			causeVi, causeEn := "Web server không chạy hoặc không nghe cổng 80.", "The web server is not running or does not listen on port 80."
			if res.ExitCode == curlTimeout {
				vi, en = "web server không trả lời trong 5 giây", "the web server did not answer within 5 seconds"
				causeVi, causeEn = "Web server nhận kết nối nhưng treo hoặc quá tải.", "The web server accepts connections but hangs or is overloaded."
			}
			return b.bad(Fail, i18n.T("Lỗi: "+vi, "Failed: "+en), i18n.T(causeVi, causeEn),
				[]string{
					line("systemctl", "status", unit),
					line("ss", "-tlnp"),
					line("journalctl", "-u", unit, "-n", "50"),
					recheck,
				}, ev)

		default:
			gotVi, gotEn := "nhận kết quả lạ (curl mã thoát "+strconv.Itoa(res.ExitCode)+")", "got an unexpected result (curl exit "+strconv.Itoa(res.ExitCode)+")"
			if code0 {
				gotVi, gotEn = "trả mã "+code, "answered "+code
			}
			return b.bad(Fail,
				i18n.T("Kết nối được nhưng sai: site mặc định "+gotVi+", đáng lẽ phải "+expectVi,
					"Connected but wrong: the default site "+gotEn+", it should "+expectEn),
				i18n.T("Site mặc định chưa đúng: một site khác (hoặc trang mặc định của distro) đang hứng request lạ, hoặc file tpan-default chưa được bật.",
					"The default site is not right: another site (or the distro's default page) catches unknown requests, or the tpan-default file is not enabled."),
				[]string{ls, test, line("systemctl", "reload", unit), recheck}, ev)
		}
	})
}
