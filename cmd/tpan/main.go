// Lệnh tpan: điểm vào duy nhất của công cụ.
//
// Các lệnh được dựng bằng cobra (mục 3 và 9 của docs/KE-HOACH.md). Mỗi lệnh chỉ
// đọc cờ, gọi một ca sử dụng trong internal/core rồi in kết quả; mọi quyết định
// về máy nằm ở core, để CLI, API và MCP cho cùng một kết quả.
//
// Hiện có: version, serve, stack install, stack status, health.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

func main() {
	// Ctrl+C hoặc `systemctl stop` gửi tín hiệu; ctx bị huỷ, server tắt êm, job
	// đang chạy dừng và hoàn tác các bước đã làm.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], realEnv())
	stop()
	os.Exit(code)
}

// realEnv nối CLI với máy thật.
func realEnv() env {
	return env{
		sys:    system.NewLocal(),
		isRoot: func() bool { return os.Geteuid() == 0 },
		// Stdin là terminal thì mới hỏi được "Chạy? [y/N]". Chạy trong script
		// hay qua pipe thì không có ai trả lời.
		isTerminal: func() bool {
			fi, err := os.Stdin.Stat()
			return err == nil && fi.Mode()&os.ModeCharDevice != 0
		},
		// Dùng os.Stat chứ không qua system.System: file DB do store mở bằng
		// hệ thống file thật, và đây chỉ là đọc.
		fileExists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
		openStore: store.Open,
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
	}
}

// run chạy CLI và trả mã thoát. Tách khỏi main để test gọi được.
func run(ctx context.Context, args []string, e env) int {
	root := newRoot(&e)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	var ec *exitCode
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ec):
		// Lệnh đã tự in lý do.
		return ec.code
	default:
		// Lỗi cờ, lỗi đọc DB...: in một dòng, không stack trace.
		printf(e.stderr, "tpan: %v\n", err)
		return 1
	}
}

// exitCode báo mã thoát khác 0 khi lệnh đã tự in lý do cho người dùng.
type exitCode struct{ code int }

func (e *exitCode) Error() string { return fmt.Sprintf("mã thoát %d", e.code) }
