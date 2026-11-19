package demo

// Log giả của máy demo: trả lời lệnh `tail` mà server dùng ở hai chỗ (M4):
//
//   - `tail -n 100 -F <file>`: log trực tiếp trên trang site. Máy thật thì tail
//     chạy mãi; máy giả trả vài dòng rồi thoát ngay (server gửi sự kiện "end"
//     và giao diện hiện nút "Nối lại"), để --demo không bao giờ treo.
//   - `tail -n <N> <access.log>`: monitor.SiteTraffic đếm request và 5xx.
//
// Dòng log dựng lúc gọi, với giờ trong một giờ gần nhất, để số request trên
// Bảng điều khiển không về 0 khi demo chạy lâu. Mã trả về theo trạng thái thật
// của máy giả: socket của pool mất thì 502 (như wiki.example.org), sửa xong
// (khởi động lại PHP-FPM) thì các lần gọi sau ra 200.

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// demoLogLines là số dòng access.log giả của một site trong một giờ.
const demoLogLines = 60

// Trang mà khách giả vào, và User-Agent của họ. IP lấy trong dải dành cho tài
// liệu (RFC 5737) để không trỏ vào máy có thật.
var (
	demoPaths = []string{"/", "/", "/", "/gioi-thieu/", "/lien-he/", "/feed/", "/wp-login.php", "/?s=go", "/wp-content/uploads/2026/11/anh.jpg", "/robots.txt"}
	demoUA    = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
	}
)

// tail trả lời `tail [-n N] [-F] <file>`. File là tham số cuối. Chỉ log của
// site (dưới /var/log/tpan/sites/<domain>/) có nội dung; file khác thì không có
// dòng nào, như một file rỗng.
func (m *machine) tail(c system.Cmd) system.Result {
	if len(c.Args) == 0 {
		return exit(1, "", "tail: thiếu tên file\n")
	}
	p := c.Args[len(c.Args)-1]
	n, err := strconv.Atoi(argAfter(c.Args, "-n"))
	if err != nil || n <= 0 {
		n = 10 // mặc định của tail
	}
	rest, ok := strings.CutPrefix(p, "/var/log/tpan/sites/")
	if !ok {
		return okResult
	}
	dom, file, _ := strings.Cut(rest, "/")
	l, ok := siteOf(dom)
	if !ok || l.Domain.String() != dom {
		return okResult
	}
	now := time.Now()
	var lines []string
	switch file {
	case "access.log":
		lines = m.accessLog(l, now)
	case "error.log":
		lines = m.errorLog(l, now)
	case "php-error.log":
		lines = phpErrorLog(l, now)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 0 {
		return okResult
	}
	return exit(0, strings.Join(lines, "\n")+"\n", "")
}

// seedOf cho một số cố định theo chuỗi, để mỗi site có nhịp khách riêng mà
// mỗi lần chạy demo vẫn giống nhau.
func seedOf(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// poolDown: socket của pool không có, request PHP sẽ ra 502.
func (m *machine) poolDown(l site.Layout) bool {
	_, err := m.Stat(l.Socket)
	return err != nil
}

// accessLog là demoLogLines dòng định dạng "combined", cũ trước, rải đều
// trong 59 phút gần nhất.
func (m *machine) accessLog(l site.Layout, now time.Time) []string {
	seed := seedOf(l.Domain.String())
	down := m.poolDown(l)
	out := make([]string, demoLogLines)
	for i := range demoLogLines {
		k := seed + uint32(i)*2654435761 // trộn chỉ số để trang, IP không lặp theo chu kỳ ngắn
		at := now.Add(-time.Duration(demoLogLines-i) * 59 * time.Minute / demoLogLines).UTC()
		path := demoPaths[k%uint32(len(demoPaths))]
		status, size := 200, 4000+int(k%30000)
		switch {
		case path == "/robots.txt":
			size = 67
		case strings.HasPrefix(path, "/wp-content/"):
			status, size = 304, 0 // file tĩnh nginx tự trả, không qua PHP
		case down:
			status, size = 502, 166
		}
		out[i] = fmt.Sprintf(`203.0.113.%d - - [%s] "GET %s HTTP/1.1" %d %d "-" "%s"`,
			1+k%250, at.Format("02/Jan/2006:15:04:05 -0700"), path, status, size, demoUA[k%uint32(len(demoUA))])
	}
	return out
}

// errorLog là log lỗi của nginx cho site: site hỏng pool thì nginx báo không
// nối được socket (đúng câu của nginx thật), site lành chỉ có vài lần bot dò file.
func (m *machine) errorLog(l site.Layout, now time.Time) []string {
	dom := l.Domain.String()
	const layout = "2006/01/02 15:04:05"
	if m.poolDown(l) {
		var out []string
		for i := range 5 {
			at := now.Add(-time.Duration(50-i*11) * time.Minute).UTC()
			out = append(out, fmt.Sprintf(`%s [crit] 1180#1180: *%d connect() to unix:%s failed (2: No such file or directory) while connecting to upstream, client: 203.0.113.%d, server: %s, request: "GET / HTTP/1.1", upstream: "fastcgi://unix:%s:", host: "%s"`,
				at.Format(layout), 4100+i*37, l.Socket, 20+i, dom, l.Socket, dom))
		}
		return out
	}
	return []string{
		fmt.Sprintf(`%s [error] 1180#1180: *3921 access forbidden by rule, client: 198.51.100.23, server: %s, request: "GET /.env HTTP/1.1", host: "%s"`,
			now.Add(-42*time.Minute).UTC().Format(layout), dom, dom),
		fmt.Sprintf(`%s [error] 1180#1180: *4410 access forbidden by rule, client: 198.51.100.71, server: %s, request: "GET /.git/config HTTP/1.1", host: "%s"`,
			now.Add(-7*time.Minute).UTC().Format(layout), dom, dom),
	}
}

// phpErrorLog là log lỗi của PHP-FPM cho site: vài cảnh báo của theme.
func phpErrorLog(l site.Layout, now time.Time) []string {
	const layout = "02-Jan-2006 15:04:05 MST"
	fn := l.Public + "/wp-content/themes/x/functions.php"
	return []string{
		fmt.Sprintf(`[%s] PHP Warning:  Undefined array key "page" in %s on line 42`, now.Add(-35*time.Minute).UTC().Format(layout), fn),
		fmt.Sprintf(`[%s] PHP Deprecated:  Creation of dynamic property X_Widget::$id is deprecated in %s on line 118`, now.Add(-12*time.Minute).UTC().Format(layout), fn),
	}
}
