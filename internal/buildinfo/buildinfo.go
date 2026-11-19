// Package buildinfo giữ thông tin phiên bản. Ba biến dưới đây được gán lúc build
// bằng -ldflags "-X ..." (xem Makefile); chạy `go run` thì giữ giá trị mặc định.
package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String trả về một dòng mô tả phiên bản, dùng cho `tpan version`.
func String() string {
	return fmt.Sprintf("tpan %s (commit %s, build %s)", Version, Commit, Date)
}
