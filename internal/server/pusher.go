package server

// Bộ đẩy nền lên kho S3 (M18, mục 3.5 của docs/thay-doi/M18-thiet-ke.md, phần
// "Ai gọi lượt đẩy"). Sống trong `tpan serve`, 2 phút một lần gọi một lượt
// đẩy (core.Remote.Sync). Không có việc thì lượt chỉ là vài truy vấn SQLite,
// không gọi mạng, nên nhịp 2 phút rẻ.
//
// Vì sao đẩy ở nền mà không chạy như một job của API: tải vài GB qua mạng chậm
// mất hàng giờ. Lượt đẩy chỉ giữ khoá riêng của nó (khoá lượt đẩy), không giữ
// khoá job của máy (B10), nên panel vẫn tạo site, sao lưu, health vẫn kiểm
// trong lúc đẩy.
//
// Hàng chờ: POST /api/v1/backups/{id}/push đưa id vào đây rồi đánh thức bộ
// đẩy. Lượt kế tiếp chạy Sync{Only: ids, Retry: true} (như `sudo tpan backup
// push <id>`) trước, rồi một lượt thường. Hàng chờ chỉ nằm trong bộ nhớ: panel
// khởi động lại trước lượt kế tiếp thì mất, câu trả lời 202 của API nói rõ.
// Tải về (POST .../pull) KHÔNG đi qua đây (R6 của bản soát hẹp, api_m18.go).

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
)

// pushEvery là nhịp của bộ đẩy nền.
const pushEvery = 2 * time.Minute

// syncer là phần của core.Remote mà bộ đẩy nền cần; test đưa bản giả.
type syncer interface {
	Sync(ctx context.Context, opt core.SyncOptions, onEvent func(engine.Event)) (core.SyncResult, error)
}

// syncFunc biến một hàm thành syncer: server dựng core.Remote mới cho mỗi lượt
// (Remote "dựng mới cho mỗi lệnh hay request"), nên đọc lại Deps mỗi lần.
type syncFunc func(ctx context.Context, opt core.SyncOptions, onEvent func(engine.Event)) (core.SyncResult, error)

func (f syncFunc) Sync(ctx context.Context, opt core.SyncOptions, onEvent func(engine.Event)) (core.SyncResult, error) {
	return f(ctx, opt, onEvent)
}

// pusher là bộ đẩy nền. Zero value dùng được (kênh tạo lười dưới mu, như
// health.Scheduler): handler đưa id vào hàng chờ được cả khi bộ đẩy không chạy
// (test dựng Server bằng struct literal).
type pusher struct {
	// every: 0 thì dùng pushEvery; test đặt nhỏ lại.
	every time.Duration

	mu    sync.Mutex
	queue []string
	kick  chan struct{}
}

func (p *pusher) kickChan() chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.kick == nil {
		// Đệm 1: một tín hiệu đang chờ là đủ, mọi Kick sau gộp vào nó.
		p.kick = make(chan struct{}, 1)
	}
	return p.kick
}

// Kick xin chạy một lượt sớm, không chờ hết nhịp 2 phút. Không chặn.
func (p *pusher) Kick() {
	select {
	case p.kickChan() <- struct{}{}:
	default:
	}
}

// enqueue đưa các id (đã qua PreparePush) vào hàng chờ rồi đánh thức bộ đẩy.
// id đã có trong hàng chờ thì không thêm lần nữa.
func (p *pusher) enqueue(ids ...string) {
	p.enqueueQuiet(ids)
	p.Kick()
}

// take lấy hết hàng chờ.
func (p *pusher) take() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := p.queue
	p.queue = nil
	return ids
}

// Run chặn tới khi ctx huỷ: mỗi nhịp, hoặc khi được Kick, chạy một vòng.
// Lượt đang dở dừng theo ctx (core huỷ lời gọi kho, không ghi lỗi vào dòng).
func (p *pusher) Run(ctx context.Context, sy syncer) {
	every := p.every
	if every <= 0 {
		every = pushEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	kick := p.kickChan()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-kick:
		}
		p.round(ctx, sy)
	}
}

// round là một vòng: hàng chờ của API trước (Retry: người bấm "đẩy" muốn thử
// ngay, bỏ nhịp lùi), rồi một lượt thường cho mọi bản khác.
func (p *pusher) round(ctx context.Context, sy syncer) {
	if ids := p.take(); len(ids) > 0 {
		res, ok := p.sync(ctx, sy, core.SyncOptions{Only: ids, Retry: true})
		if ok && !res.Ran {
			// Lượt bị bỏ (thường vì `tpan backup create --all` của cron đang
			// giữ khoá lượt đẩy): giữ yêu cầu cho vòng sau thay vì đánh rơi.
			// ponytail: bỏ vì chưa đặt đích (đích vừa bị gỡ) cũng giữ lại; vài
			// id trong bộ nhớ, mỗi vòng một truy vấn, mất khi panel khởi động lại.
			p.enqueueQuiet(ids)
			return
		}
	}
	p.sync(ctx, sy, core.SyncOptions{})
}

// enqueueQuiet trả id về hàng chờ mà không Kick (không quay vòng ngay).
func (p *pusher) enqueueQuiet(ids []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range ids {
		if !slices.Contains(p.queue, id) {
			p.queue = append(p.queue, id)
		}
	}
}

// sync chạy một lượt và ghi log kết quả xấu. ok=false khi Sync trả lỗi.
//
// Mọi câu lỗi qua s3.CleanMessage trước khi vào log: core đã làm sạch (lỗi của
// client S3 không chứa khoá, lỗi đọc file bí mật chỉ nêu đường dẫn), đây chỉ
// bỏ ký tự điều khiển và cắt ngắn để một câu lỗi của kho không phá dòng log.
func (p *pusher) sync(ctx context.Context, sy syncer, opt core.SyncOptions) (core.SyncResult, bool) {
	res, err := sy.Sync(ctx, opt, nil)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("bộ đẩy nền: lượt đẩy lỗi", "only", len(opt.Only), "err", s3.CleanMessage(err.Error()))
		}
		return res, false
	}
	if res.TargetError != "" {
		slog.Warn("bộ đẩy nền: kho lỗi, lùi lại", "err", s3.CleanMessage(res.TargetError))
	}
	for _, f := range res.Failed {
		slog.Warn("bộ đẩy nền: một bản chưa lên kho", "id", f.ID, "domain", f.Domain, "err", s3.CleanMessage(f.Error))
	}
	if res.PanelError != "" {
		slog.Warn("bộ đẩy nền: gói panel lỗi", "err", s3.CleanMessage(res.PanelError))
	}
	return res, true
}

// startPusher chạy bộ đẩy nền tới khi ctx huỷ; trả kênh đóng khi nó dừng hẳn.
// --demo: không chạy (máy giả không có kho thật nào để đẩy), trả kênh đã đóng.
func (s *Server) startPusher(ctx context.Context, sy syncer) <-chan struct{} {
	done := make(chan struct{})
	if s.deps.Demo {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		s.pusher.Run(ctx, sy)
	}()
	return done
}
