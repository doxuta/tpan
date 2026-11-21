package server

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
)

// fakeSyncer ghi lại mọi lượt đẩy được gọi và báo qua calls.
type fakeSyncer struct {
	mu    sync.Mutex
	opts  []core.SyncOptions
	calls chan core.SyncOptions
	// ran false: lượt bị bỏ (khoá lượt đẩy đang bận).
	ran bool
	// block: Sync chặn tới khi ctx huỷ.
	block bool
}

func newFakeSyncer() *fakeSyncer {
	return &fakeSyncer{calls: make(chan core.SyncOptions, 16), ran: true}
}

func (f *fakeSyncer) Sync(ctx context.Context, opt core.SyncOptions, _ func(engine.Event)) (core.SyncResult, error) {
	f.mu.Lock()
	f.opts = append(f.opts, opt)
	ran := f.ran
	f.mu.Unlock()
	f.calls <- opt
	if f.block {
		<-ctx.Done()
		return core.SyncResult{}, ctx.Err()
	}
	return core.SyncResult{Ran: ran}, nil
}

func (f *fakeSyncer) next(t *testing.T) core.SyncOptions {
	t.Helper()
	select {
	case o := <-f.calls:
		return o
	case <-time.After(2 * time.Second):
		t.Fatal("bộ đẩy nền không chạy lượt nào")
		return core.SyncOptions{}
	}
}

func (f *fakeSyncer) none(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case o := <-f.calls:
		t.Fatalf("có lượt đẩy không mong đợi: %+v", o)
	case <-time.After(d):
	}
}

// runPusher chạy p.Run ở nền; trả hàm huỷ và kênh đóng khi Run trả về.
func runPusher(t *testing.T, p *pusher, sy syncer) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx, sy)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return cancel, done
}

// TestPusherHangChoChayOnlyRetry: id từ API chạy Sync{Only, Retry} trước, rồi
// một lượt thường; hàng chờ trống sau đó.
func TestPusherHangChoChayOnlyRetry(t *testing.T) {
	f := newFakeSyncer()
	p := &pusher{every: time.Hour}
	runPusher(t, p, f)

	p.enqueue("aaaa", "bbbb")
	p.enqueue("aaaa") // trùng: không thêm lần nữa
	first := f.next(t)
	if !first.Retry || !slices.Equal(first.Only, []string{"aaaa", "bbbb"}) || first.Wait {
		t.Fatalf("lượt đầu = %+v, muốn Only [aaaa bbbb], Retry, không Wait", first)
	}
	if second := f.next(t); len(second.Only) != 0 || second.Retry || second.Wait {
		t.Fatalf("lượt sau = %+v, muốn lượt thường", second)
	}
	if q := p.take(); len(q) != 0 {
		t.Errorf("hàng chờ còn %v", q)
	}
}

// TestPusherKhoaBanGiuHangCho: lượt Only bị bỏ vì khoá bận thì id quay lại
// hàng chờ, không mất, và không chạy lượt thường ngay sau.
func TestPusherKhoaBanGiuHangCho(t *testing.T) {
	f := newFakeSyncer()
	f.ran = false
	p := &pusher{every: time.Hour}
	p.enqueueQuiet([]string{"cccc"})
	p.round(context.Background(), f)
	if o := f.next(t); !slices.Equal(o.Only, []string{"cccc"}) {
		t.Fatalf("lượt = %+v", o)
	}
	f.none(t, 50*time.Millisecond)
	if q := p.take(); !slices.Equal(q, []string{"cccc"}) {
		t.Errorf("hàng chờ sau lượt bị bỏ = %v, muốn [cccc]", q)
	}
}

// TestPusherKickChaySom: nhịp 1 giờ, Kick làm chạy ngay một lượt thường.
func TestPusherKickChaySom(t *testing.T) {
	f := newFakeSyncer()
	p := &pusher{every: time.Hour}
	runPusher(t, p, f)
	f.none(t, 50*time.Millisecond) // không chạy khi chưa tới nhịp
	p.Kick()
	if o := f.next(t); len(o.Only) != 0 || o.Retry {
		t.Fatalf("Kick chạy %+v, muốn lượt thường", o)
	}
}

// TestPusherNhip: không ai Kick thì vẫn chạy theo nhịp.
func TestPusherNhip(t *testing.T) {
	f := newFakeSyncer()
	p := &pusher{every: 20 * time.Millisecond}
	runPusher(t, p, f)
	f.next(t)
	f.next(t)
}

// TestPusherDungKhiHuy: huỷ ctx thì Run trả về, kể cả khi đang giữa một lượt
// (lượt nhận ctx đã huỷ và dừng).
func TestPusherDungKhiHuy(t *testing.T) {
	f := newFakeSyncer()
	f.block = true
	p := &pusher{every: time.Hour}
	cancel, done := runPusher(t, p, f)
	p.Kick()
	f.next(t) // đang trong lượt
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bộ đẩy nền không dừng sau khi huỷ")
	}
}

// TestPusherKhongChayODemo: --demo không khởi động bộ đẩy nền.
func TestPusherKhongChayODemo(t *testing.T) {
	f := newFakeSyncer()
	s := &Server{deps: Deps{Demo: true}}
	s.pusher.every = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := s.startPusher(ctx, f)
	select {
	case <-done:
	default:
		t.Fatal("demo: startPusher phải trả kênh đã đóng (không có goroutine nào)")
	}
	s.pusher.Kick()
	f.none(t, 100*time.Millisecond)

	// Không demo thì có chạy (cùng đường gọi, để biết test trên không đúng vì lý do khác).
	s2 := &Server{}
	s2.pusher.every = time.Hour
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := s2.startPusher(ctx2, f)
	s2.pusher.Kick()
	f.next(t)
	cancel2()
	<-done2
}
