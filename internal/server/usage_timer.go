package server

// Bộ hẹn giờ của số liệu và dọn lượt thử tấn công (M17, mục 3.8 và 3.9 của
// docs/thay-doi/M17-thiet-ke.md), cùng khuôn với trash_timer.go. Mỗi vòng, cách
// nhau 5 phút:
//
//  1. Usages.Sample: ghi một mẫu cho mỗi site trong lồng;
//  2. Usages.Rollup: gộp theo giờ, tối đa mỗi giờ một lần;
//  3. dọn lượt thử còn sót, khi có lượt chưa dọn đã quá 2 giờ.
//
// Hai việc đầu chỉ đọc cgroup và ghi tpan.db, không đổi máy, nên không giành
// chỗ chạy job: giành thì mỗi job dài làm hụt một mẫu. Việc thứ ba chạy một
// job (dọn site thử), nên giành apiState.claim và bỏ vòng khi khoá toàn cục
// đang bị tiến trình khác giữ (CLI), đúng như bộ dọn thùng rác.
//
// Lúc khởi động còn dọn một lần các unit tạm hỏng của lệnh chạy thay cho site
// (cage.Wrap): systemd giữ unit hỏng tới khi reset-failed, tpan bị tắt giữa
// chừng thì không ai gọi.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// usageTimerFirst: vòng đầu sau khi serve khởi động.
	usageTimerFirst = time.Minute
	// usageRollupEvery: gộp theo giờ.
	usageRollupEvery = time.Hour
	// probeStaleAfter: lượt thử chưa dọn quá lâu thì là sót (khớp probe.leftover).
	probeStaleAfter = 2 * time.Hour
	// auditProbeCleanup là thao tác ghi vào nhật ký khi bộ hẹn giờ tự dọn.
	auditProbeCleanup = "isolation.probe.cleanup"
	// cageRunUnits: mẫu tên unit tạm của lệnh chạy thay cho site. systemctl tự
	// mở rộng dấu sao; không có shell nào ở đây.
	cageRunUnits = "tpan-cage-*-run-*"
)

// usageTimer chạy vòng số liệu theo lịch. Các trường hàm để test thay bằng hàm
// giả và đặt khoảng ngắn.
type usageTimer struct {
	s                       *Server
	first, every            time.Duration
	rollupEvery, staleAfter time.Duration

	sample  func(ctx context.Context) (int, error)
	rollup  func(ctx context.Context) error
	pending func(ctx context.Context) ([]cage.ProbeRun, error)
	// cleanup chạy Plan probe.cleanup; ran false khi không có gì để dọn.
	cleanup func(ctx context.Context) (rep engine.Report, ran bool, err error)
	// onRound (có thể nil) được gọi sau mỗi vòng.
	onRound func()

	lastRollup time.Time
}

func (s *Server) newUsageTimer() *usageTimer {
	return &usageTimer{
		s: s, first: usageTimerFirst, every: core.UsageSampleEvery,
		rollupEvery: usageRollupEvery, staleAfter: probeStaleAfter,
		sample:  func(ctx context.Context) (int, error) { return s.apiUsages().Sample(ctx) },
		rollup:  func(ctx context.Context) error { return s.apiUsages().Rollup(ctx) },
		pending: s.deps.Store.PendingProbeRuns,
		cleanup: func(ctx context.Context) (engine.Report, bool, error) {
			p, err := s.apiProbe().PrepareCleanup(ctx)
			if err != nil || p == nil {
				return engine.Report{}, false, err
			}
			rep, err := p.Execute(ctx, nil)
			return rep, true, err
		},
	}
}

// Run chặn tới khi ctx bị huỷ. Vòng đang chạy thì chạy cho xong rồi mới trả về.
func (t *usageTimer) Run(ctx context.Context) {
	t.resetFailedUnits(ctx)
	timer := time.NewTimer(t.first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		t.round(ctx)
		if t.onRound != nil {
			t.onRound()
		}
		timer.Reset(t.every)
	}
}

// resetFailedUnits dọn unit tạm hỏng còn sót. Lỗi bỏ qua: máy chưa có lồng nào
// thì không có unit nào khớp, và không có systemd (máy dev) thì không có gì để dọn.
func (t *usageTimer) resetFailedUnits(ctx context.Context) {
	sys := t.s.deps.Sys
	if sys == nil {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, _ = sys.Run(sctx, system.Cmd{Name: "systemctl", Args: []string{"reset-failed", cageRunUnits}})
}

// round chạy một vòng. Lỗi chỉ ghi log: thiếu một mẫu không hại gì, mẫu sau bù.
func (t *usageTimer) round(ctx context.Context) {
	if n, err := t.sample(ctx); err != nil {
		slog.Warn("số liệu: lấy mẫu", "ok", n, "err", err)
	}
	if now := t.s.now(); t.lastRollup.IsZero() || now.Sub(t.lastRollup) >= t.rollupEvery {
		if err := t.rollup(ctx); err != nil {
			slog.Warn("số liệu: gộp theo giờ", "err", err)
		} else {
			t.lastRollup = now
		}
	}
	t.cleanStale(ctx)
}

// cleanStale dọn lượt thử sót khi có lượt chưa dọn đã quá staleAfter; true khi
// đã chạy Plan dọn. Plan dọn đọc sổ trong job (đã giữ khoá): lúc ấy không lượt
// thử nào đang chạy nên mọi dòng chưa dọn là sót.
func (t *usageTimer) cleanStale(ctx context.Context) bool {
	s := t.s
	runs, err := t.pending(ctx)
	if err != nil {
		slog.Warn("thử tấn công: đọc sổ lượt chưa dọn", "err", err)
		return false
	}
	var stale []string
	for _, r := range runs {
		if s.now().Sub(r.Started) >= t.staleAfter {
			stale = append(stale, r.ID)
		}
	}
	if len(stale) == 0 {
		return false
	}
	if !s.api.claim() {
		slog.Info("thử tấn công: đang có job, để vòng sau dọn lượt sót")
		return false
	}
	if s.lockHeldElsewhere() {
		s.api.release()
		slog.Info("thử tấn công: khoá toàn cục đang bị tiến trình khác giữ, để vòng sau dọn lượt sót")
		return false
	}
	// WithoutCancel: dọn dở dang để lại site thử nửa vời; đổi lại tắt serve chờ
	// vòng đang chạy xong.
	rep, ran, err := t.cleanup(context.WithoutCancel(ctx))
	s.api.release()
	if err != nil {
		slog.Error("thử tấn công: dọn lượt sót", "runs", stale, "err", err)
	}
	if !ran {
		return false
	}
	result := "ok"
	switch {
	case err != nil:
		result = "error"
	case rep.Job.Status != engine.JobSucceeded:
		result = string(rep.Job.Status)
	}
	s.auditAs(context.WithoutCancel(ctx), auditIPTimer, auditProbeCleanup, strings.Join(stale, ","), result, rep.Job.ID)
	s.kick("")
	return true
}
