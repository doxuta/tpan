package monitor

// Hợp đồng M4: số request và lỗi 5xx của một site (mục 1 bảng "Thống kê", mục
// 13 hàng M4 của docs/KE-HOACH.md).

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// TrafficWindow là khoảng thời gian đếm mặc định: một giờ gần nhất.
const TrafficWindow = time.Hour

// trafficTailLines là số dòng cuối của access.log được đọc. Cố định để một
// site đông khách (log hàng GB) không làm health đọc hết file mỗi 5 phút.
const trafficTailLines = 50000

// combinedTime là dạng thời điểm trong cặp [] của định dạng "combined".
const combinedTime = "02/Jan/2006:15:04:05 -0700"

// Traffic là số request và số phản hồi 5xx trong một khoảng gần đây, đếm từ
// access.log của site (định dạng "combined", cả Nginx lẫn Apache ghi như vậy).
type Traffic struct {
	Requests  int       `json:"requests"`
	Errors5xx int       `json:"errors_5xx"`
	Since     time.Time `json:"since"` // đầu khoảng đếm
	// Partial: chỉ đọc được phần cuối của log (giới hạn số dòng) mà phần đó
	// chưa phủ hết khoảng đếm, nên số thật có thể lớn hơn.
	Partial bool `json:"partial,omitempty"`
}

// SiteTraffic đếm các dòng của accessLog có thời điểm trong [now-window, now].
// Đọc phần cuối file bằng `tail -n <N> <accessLog>` qua sys.Run (argv, không
// shell; N cố định, ví dụ 50000) để file log lớn không bị đọc hết. Thời điểm
// lấy trong cặp [] (dạng 02/Jan/2006:15:04:05 -0700), mã trạng thái là số đứng
// ngay sau chuỗi request trong nháy kép. Dòng không đọc được thì bỏ qua.
// File chưa có (site mới, chưa ai vào) là 0 request, không lỗi.
// window <= 0 thì dùng TrafficWindow.
func SiteTraffic(ctx context.Context, sys system.System, accessLog string, now time.Time, window time.Duration) (Traffic, error) {
	if window <= 0 {
		window = TrafficWindow
	}
	t := Traffic{Since: now.Add(-window)}
	// Đường dẫn tuyệt đối thì không bao giờ bắt đầu bằng "-" để tail hiểu là cờ.
	if !path.IsAbs(accessLog) {
		return t, fmt.Errorf("đường dẫn access log phải tuyệt đối (nhận được %q)", accessLog)
	}

	res, err := sys.Run(ctx, system.Cmd{Name: "tail", Args: []string{"-n", strconv.Itoa(trafficTailLines), accessLog}})
	if err != nil {
		var ee *system.ExitError
		// Site mới chưa ai vào thì chưa có file: tail thoát mã 1 và nói "No such
		// file" (LocalSystem đặt LC_ALL=C nên câu chữ cố định). Đó là 0 request.
		if errors.As(err, &ee) && (strings.Contains(res.Stderr, "No such file") || strings.Contains(ee.Stderr, "No such file")) {
			return t, nil
		}
		return t, fmt.Errorf("đọc cuối %s: %w", accessLog, err)
	}

	lines := 0
	var oldest time.Time // dòng đầu tiên đọc được: log ghi theo thứ tự thời gian nên đó là dòng cũ nhất
	for l := range strings.Lines(res.Stdout) {
		lines++
		at, status, ok := parseCombined(l)
		if !ok {
			continue // log có thể chứa chuỗi lạ do khách gửi; bỏ qua, không làm hỏng cả lần đếm
		}
		if oldest.IsZero() {
			oldest = at
		}
		if at.Before(t.Since) || at.After(now) {
			continue
		}
		t.Requests++
		if status >= 500 {
			t.Errors5xx++
		}
	}
	// Đủ N dòng nghĩa là có thể còn dòng cũ hơn mà tail đã cắt; nếu dòng cũ
	// nhất đọc được vẫn nằm trong khoảng đếm thì phần đầu khoảng bị thiếu.
	t.Partial = lines >= trafficTailLines && !oldest.IsZero() && !oldest.Before(t.Since)
	return t, nil
}

// parseCombined lấy thời điểm và mã trạng thái từ một dòng log "combined":
//
//	203.0.113.7 - - [19/Nov/2026:10:00:00 +0000] "GET / HTTP/1.1" 200 1234 "-" "curl/8.5"
//
// Mã trạng thái nằm ngay sau chuỗi request trong nháy kép. Chuỗi request do
// khách gửi nên có thể chứa nháy kép đã thoát bằng "\" (Apache ghi \", Nginx
// ghi \x22): tìm nháy đóng bằng cách bỏ qua ký tự đứng sau dấu "\".
func parseCombined(l string) (at time.Time, status int, ok bool) {
	lb := strings.IndexByte(l, '[')
	if lb < 0 {
		return time.Time{}, 0, false
	}
	rb := strings.IndexByte(l[lb:], ']')
	if rb < 0 {
		return time.Time{}, 0, false
	}
	at, err := time.Parse(combinedTime, l[lb+1:lb+rb])
	if err != nil {
		return time.Time{}, 0, false
	}
	rest := strings.TrimLeft(l[lb+rb+1:], " ")
	if !strings.HasPrefix(rest, `"`) {
		return time.Time{}, 0, false
	}
	end := -1
	for i := 1; i < len(rest); i++ {
		if rest[i] == '\\' {
			i++
		} else if rest[i] == '"' {
			end = i
			break
		}
	}
	if end < 0 {
		return time.Time{}, 0, false
	}
	f := strings.Fields(rest[end+1:])
	if len(f) == 0 || len(f[0]) != 3 {
		return time.Time{}, 0, false
	}
	status, err = strconv.Atoi(f[0])
	if err != nil || status < 100 || status > 599 {
		return time.Time{}, 0, false
	}
	return at, status, true
}
