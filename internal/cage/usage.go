package cage

// Hợp đồng M17 đợt 0: thân hàm do thợ đợt 1 viết. Mục 3.8 của
// docs/thay-doi/M17-thiet-ke.md. Mẫu file thật (kernel 6.8, systemd 255) ở
// docs/bang-chung/M17-nghien-cuu/cgroup-mau/ (dang-chay: lồng đang chạy, không
// có trần đĩa; cham-tran: vừa chạm trần RAM và trần tiến trình).

import (
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// ReadUsage đọc số liệu của lồng của l từ l.CageCgroup() bằng sys.ReadFile và
// sys.ReadDir (không chạy lệnh). Thư mục không có: Usage{Running: false, At:
// now}, không lỗi. Một file thiếu hay dòng lạ thì bỏ số đó (để 0), không panic,
// không lỗi; lỗi đọc khác fs.ErrNotExist thì trả lỗi. now ghi vào Usage.At.
func ReadUsage(sys system.System, l site.Layout, now time.Time) (Usage, error) {
	return Usage{}, errors.New("cage.ReadUsage: chưa viết (đợt 1)")
}
