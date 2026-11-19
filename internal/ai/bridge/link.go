package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"syscall"
)

// Link chép in → socket và socket → out tới khi một bên đóng hoặc ctx huỷ.
// Đây là toàn bộ việc của `tpan brain-link`: nó không hiểu giao thức, chỉ là
// ống nối giữa ssh (stdin, stdout) và socket nội bộ của tpan serve.
func Link(ctx context.Context, path string, in io.Reader, out io.Writer) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return fmt.Errorf("không nối được %s: tpan serve chưa chạy hoặc chưa bật bộ não (sudo tpan mcp config --brain bridge rồi khởi động lại tpan): %w", path, err)
		}
		return fmt.Errorf("nối socket %s: %w", path, err)
	}
	// Bên nào xong trước (ssh đóng stdin, hoặc hub đóng kết nối) thì đóng conn
	// để bên kia cũng thoát. Đọc stdin không ngắt được: goroutine chép stdin có
	// thể còn treo tới khi tiến trình thoát, điều đó vô hại vì Link là việc cuối
	// của tpan brain-link.
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, in)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(out, conn)
		errc <- err
	}()
	select {
	case err = <-errc:
	case <-ctx.Done():
	}
	_ = conn.Close()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("chép dữ liệu qua %s: %w", path, err)
	}
	return nil
}
