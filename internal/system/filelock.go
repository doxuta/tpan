package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Locker là một khoá có tên riêng, tách với khoá job toàn cục của System.Lock
// (M18: khoá lượt đẩy lên kho S3, mục 3.5 của docs/thay-doi/M18-thiet-ke.md).
// Lượt đẩy có thể chạy hàng giờ; nếu nó giữ khoá job thì mọi thao tác khác của
// panel bị từ chối và health bỏ mọi vòng kiểm trong lúc đó.
type Locker interface {
	// Acquire lấy khoá. wait > 0: thử lại có nghỉ tới hết wait (hoặc tới khi
	// ctx bị huỷ, khi đó trả lỗi của ctx); wait == 0: thử đúng một lần.
	// ok=false (err nil) khi khoá đang bị giữ suốt thời gian đó. Lấy được thì
	// gọi unlock đúng một lần.
	Acquire(ctx context.Context, wait time.Duration) (unlock func(), ok bool, err error)
}

// DefaultRemoteLockPath là file khoá của lượt đẩy lên kho. /run là tmpfs nên
// khoá không bao giờ kẹt qua một lần khởi động lại máy.
const DefaultRemoteLockPath = "/run/tpan/remote.lock"

// FileLock là Locker bằng flock trên Path, có hiệu lực giữa các tiến trình
// (CLI của cron và bộ đẩy nền trong tpan serve).
type FileLock struct{ Path string }

// lockPoll là nhịp thử lại khi chờ khoá: flock chờ thật thì không huỷ được bằng ctx.
const lockPoll = 100 * time.Millisecond

func (l FileLock) Acquire(ctx context.Context, wait time.Duration) (func(), bool, error) {
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return nil, false, fmt.Errorf("tạo thư mục khoá %s: %w", filepath.Dir(l.Path), err)
	}
	f, err := os.OpenFile(l.Path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("mở file khoá %s: %w", l.Path, err)
	}
	fd := int(f.Fd())
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, false, fmt.Errorf("flock %s: %w", l.Path, err)
		}
		if !time.Now().Before(deadline) {
			_ = f.Close()
			return nil, false, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, false, fmt.Errorf("chờ khoá %s: %w", l.Path, ctx.Err())
		case <-time.After(lockPoll):
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(fd, syscall.LOCK_UN)
			_ = f.Close()
		})
	}, true, nil
}

// MemLock là Locker trong bộ nhớ cho test (chỉ có hiệu lực trong một tiến
// trình). Dựng bằng NewMemLock.
type MemLock struct{ ch chan struct{} }

// NewMemLock tạo một MemLock đang mở.
func NewMemLock() *MemLock { return &MemLock{ch: make(chan struct{}, 1)} }

func (l *MemLock) Acquire(ctx context.Context, wait time.Duration) (func(), bool, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case l.ch <- struct{}{}:
	default:
		if wait <= 0 {
			return nil, false, nil
		}
		select {
		case l.ch <- struct{}{}:
		case <-timer.C:
			return nil, false, nil
		case <-ctx.Done():
			return nil, false, fmt.Errorf("chờ khoá: %w", ctx.Err())
		}
	}
	var once sync.Once
	return func() { once.Do(func() { <-l.ch }) }, true, nil
}
