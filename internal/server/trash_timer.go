package server

// Bộ hẹn giờ dọn thùng rác (mục 6.4 của docs/KE-HOACH.md): trong `tpan serve`,
// mỗi giờ một lần (và một lần vài phút sau khi khởi động), xoá hẳn các site
// trong thùng rác đã quá hạn (core.Sites.PurgeExpired).
//
// Vì sao chạy như một job của API: xoá hẳn đổi máy (xoá DB, file, user), nên
// cùng lúc với một job khác là phá luật "cùng lúc chỉ một job" (mục 4.2).
// Vòng dọn giành apiState.claim như mọi handler có job; không giành được (đang
// có job trong tiến trình này) hoặc khoá toàn cục đang bị tiến trình khác giữ
// (CLI) thì bỏ vòng, ghi một dòng log, chờ giờ sau. Giống health bỏ vòng khi
// đang có job. Site hết hạn chậm một giờ không hại gì; xoá chen giữa một job
// khác thì có.
//
// Vòng đầu chạy sau trashTimerFirst chứ không ngay lúc khởi động: lúc đó
// `tpan setup` thường còn giữ khoá, và health cũng chạy vòng đầy đủ đầu tiên.

import (
	"context"
	"log/slog"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
)

const (
	// trashTimerEvery: mục 6.4, "chạy mỗi giờ".
	trashTimerEvery = time.Hour
	// trashTimerFirst: vòng đầu sau khi serve khởi động.
	trashTimerFirst = 3 * time.Minute
	// auditIPTimer là cột ip của nhật ký cho việc bộ hẹn giờ tự làm (người gõ
	// ở dòng lệnh là "cli"): người đọc nhật ký biết không ai bấm gì cả.
	auditIPTimer = "timer"
	// auditPurgeExpired là thao tác ghi vào nhật ký, mỗi site một dòng.
	auditPurgeExpired = "trash.purge_expired"
)

// purgeFunc là chữ ký của core.Sites.PurgeExpired khi không cần sự kiện.
type purgeFunc func(ctx context.Context, now time.Time) ([]core.PurgeResult, error)

// trashTimer chạy vòng dọn theo lịch. Các trường để test đặt khoảng ngắn và
// thay purge bằng hàm giả.
type trashTimer struct {
	s            *Server
	first, every time.Duration
	purge        purgeFunc
	// onRound (có thể nil) được gọi sau mỗi vòng; ran false là vòng bị bỏ.
	onRound func(ran bool)
}

func (s *Server) newTrashTimer() *trashTimer {
	return &trashTimer{
		s: s, first: trashTimerFirst, every: trashTimerEvery,
		purge: func(ctx context.Context, now time.Time) ([]core.PurgeResult, error) {
			// Không cần sự kiện: job nào cũng đã vào Journal (Lịch sử).
			return s.apiSites().PurgeExpired(ctx, now, nil)
		},
	}
}

// Run chặn tới khi ctx bị huỷ. Vòng đang chạy thì chạy cho xong rồi mới trả
// về (xem round).
func (t *trashTimer) Run(ctx context.Context) {
	timer := time.NewTimer(t.first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		ran := t.round(ctx)
		if t.onRound != nil {
			t.onRound(ran)
		}
		timer.Reset(t.every)
	}
}

// round chạy một vòng dọn; false khi bỏ vòng vì đang có job.
func (t *trashTimer) round(ctx context.Context) bool {
	s := t.s
	if !s.api.claim() {
		slog.Info("thùng rác: đang có job, bỏ vòng dọn này")
		return false
	}
	// CLI đang giữ khoá: không thử thì mỗi site hết hạn chờ khoá lockWait rồi
	// lỗi errJobBusy, nhật ký đầy dòng lỗi giả.
	if s.lockHeldElsewhere() {
		s.api.release()
		slog.Info("thùng rác: khoá toàn cục đang bị tiến trình khác giữ, bỏ vòng dọn này")
		return false
	}
	// WithoutCancel: xoá hẳn không hoàn tác được (mục 6.4), cắt ngang giữa
	// chừng vì serve đang tắt là để máy dở dang (DB đã xoá, file còn). Đổi
	// lại, tắt serve chờ vòng đang chạy xong (vài giây mỗi site).
	res, err := t.purge(context.WithoutCancel(ctx), s.now())
	s.api.release()
	if err != nil {
		slog.Error("thùng rác: dọn site hết hạn", "err", err)
	}
	for _, r := range res {
		result := "ok"
		switch {
		case r.Error != "":
			result = "error"
			slog.Error("thùng rác: xoá hẳn site hết hạn", "domain", r.Domain.String(), "err", r.Error)
		case r.Report.Job.Status != engine.JobSucceeded:
			result = string(r.Report.Job.Status)
		}
		s.auditAs(context.WithoutCancel(ctx), auditIPTimer, auditPurgeExpired, r.Domain.String(), result, r.Report.Job.ID)
	}
	if len(res) > 0 {
		// Site đã xoá hẳn: kiểm nhẹ lại mọi thứ (web server, PHP-FPM sau reload).
		s.kick("")
	}
	return true
}
