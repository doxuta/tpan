package monitor

// Hợp đồng M4: số request và lỗi 5xx của một site (mục 1 bảng "Thống kê", mục
// 13 hàng M4 của docs/KE-HOACH.md). Thân hàm do thợ của phần checks/monitor viết.

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// TrafficWindow là khoảng thời gian đếm mặc định: một giờ gần nhất.
const TrafficWindow = time.Hour

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
func SiteTraffic(ctx context.Context, sys system.System, accessLog string, now time.Time, window time.Duration) (Traffic, error) {
	return Traffic{}, errors.New("monitor: SiteTraffic chưa viết")
}
