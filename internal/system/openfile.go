package system

import (
	"fmt"
	"os"
	"syscall"
)

// OpenRegularNoFollow mở một file thường để đọc theo luồng (tải bản backup về
// trình duyệt: file vài trăm MB không đọc cả vào bộ nhớ được). Tên cuối của
// đường dẫn là symlink thì từ chối (O_NOFOLLOW); mở xong kiểm lại trên chính
// file đã mở là file thường. Người gọi kiểm đường dẫn nằm đúng thư mục và đóng file.
func OpenRegularNoFollow(path string) (*os.File, os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("%s không phải file thường", path)
	}
	return f, fi, nil
}
