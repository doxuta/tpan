// Package web nhúng giao diện đã build (thư mục dist) vào file chạy tpan,
// nhờ vậy cài tpan chỉ cần chép đúng một file.
package web

import (
	"embed"
	"io/fs"
)

// Tiền tố "all:" để nhúng cả file bắt đầu bằng dấu chấm. dist/.gitkeep giữ cho
// thư mục luôn tồn tại, nên `go build` chạy được cả khi chưa build giao diện.
//
//go:embed all:dist
var dist embed.FS

// DistFS trả về nội dung thư mục dist (index.html nằm ở gốc).
func DistFS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // chỉ xảy ra khi tên thư mục ở trên bị gõ sai
	}
	return sub
}
