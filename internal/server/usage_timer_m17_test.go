package server

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/engine"
)

type usageCalls struct{ sample, rollup, cleanup atomic.Int32 }

// usageTestTimer là bộ hẹn giờ với mọi hàm giả đếm số lần gọi.
func usageTestTimer(e *apiEnv, pending []cage.ProbeRun) (*usageTimer, *usageCalls) {
	var c usageCalls
	ut := e.s.newUsageTimer()
	ut.sample = func(context.Context) (int, error) { c.sample.Add(1); return 1, nil }
	ut.rollup = func(context.Context) error { c.rollup.Add(1); return nil }
	ut.pending = func(context.Context) ([]cage.ProbeRun, error) { return pending, nil }
	ut.cleanup = func(context.Context) (engine.Report, bool, error) {
		if e.s.api.claim() {
			panic("dọn mà không giữ chỗ chạy job")
		}
		c.cleanup.Add(1)
		return engine.Report{Job: engine.Job{ID: "job-clean", Status: engine.JobSucceeded}}, true, nil
	}
	return ut, &c
}

func TestUsageTimerMacDinh(t *testing.T) {
	e := apiNewEnv(t)
	ut := e.s.newUsageTimer()
	if ut.every != 5*time.Minute || ut.rollupEvery != time.Hour ||
		ut.staleAfter != 2*time.Hour || ut.first <= 0 || ut.first > 5*time.Minute {
		t.Fatalf("mặc định: first %v, every %v, rollup %v, stale %v", ut.first, ut.every, ut.rollupEvery, ut.staleAfter)
	}
}

// Mẫu mỗi vòng; gộp theo giờ ở vòng đầu rồi mỗi giờ một lần. Hai việc này
// không giành chỗ chạy job: đang có job vẫn lấy mẫu.
func TestUsageTimerSampleVaRollup(t *testing.T) {
	e := apiNewEnv(t)
	ut, c := usageTestTimer(e, nil)
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	ctx := context.Background()
	ut.round(ctx)
	if c.sample.Load() != 1 || c.rollup.Load() != 1 {
		t.Fatalf("vòng đầu khi đang có job: sample %d, rollup %d", c.sample.Load(), c.rollup.Load())
	}
	e.advance(5 * time.Minute)
	ut.round(ctx)
	if c.sample.Load() != 2 || c.rollup.Load() != 1 {
		t.Fatalf("5 phút sau: sample %d, rollup %d", c.sample.Load(), c.rollup.Load())
	}
	e.advance(56 * time.Minute)
	ut.round(ctx)
	if c.sample.Load() != 3 || c.rollup.Load() != 2 {
		t.Fatalf("hơn một giờ sau: sample %d, rollup %d", c.sample.Load(), c.rollup.Load())
	}
	e.s.api.release()
}

// Gộp lỗi thì không ghi nhận đã gộp: vòng sau thử lại.
func TestUsageTimerRollupLoiThiThuLai(t *testing.T) {
	e := apiNewEnv(t)
	ut, c := usageTestTimer(e, nil)
	ut.rollup = func(context.Context) error { c.rollup.Add(1); return errors.New("hỏng") }
	ut.round(context.Background())
	ut.round(context.Background())
	if c.rollup.Load() != 2 {
		t.Fatalf("rollup gọi %d lần, muốn 2", c.rollup.Load())
	}
}

func TestUsageTimerDonLuotSot(t *testing.T) {
	e := apiNewEnv(t)
	old := cage.ProbeRun{ID: "aaaaaaaaaaaaaaaa", Started: e.clock().Add(-3 * time.Hour), State: "running"}
	old2 := cage.ProbeRun{ID: "bbbbbbbbbbbbbbbb", Started: e.clock().Add(-2 * time.Hour), State: "failed"}
	fresh := cage.ProbeRun{ID: "cccccccccccccccc", Started: e.clock().Add(-10 * time.Minute), State: "running"}
	ctx := context.Background()

	// Chỉ có lượt mới (dưới 2 giờ): có thể đang chạy, không dọn.
	ut, c := usageTestTimer(e, []cage.ProbeRun{fresh})
	if ut.cleanStale(ctx) || c.cleanup.Load() != 0 {
		t.Fatal("lượt dưới 2 giờ mà bị dọn")
	}
	// Không có lượt nào: không giành chỗ chạy job.
	ut, c = usageTestTimer(e, nil)
	if ut.cleanStale(ctx) || c.cleanup.Load() != 0 {
		t.Fatal("không có lượt nào mà dọn")
	}

	// Có lượt quá 2 giờ (đúng 2 giờ cũng tính): dọn một lần, audit ghi id các lượt quá hạn.
	ut, c = usageTestTimer(e, []cage.ProbeRun{old, old2, fresh})
	if !ut.cleanStale(ctx) || c.cleanup.Load() != 1 {
		t.Fatalf("lượt sót mà không dọn (cleanup %d)", c.cleanup.Load())
	}
	a := hAudit(t, e, auditProbeCleanup)
	if len(a) != 1 || a[0].Target != "aaaaaaaaaaaaaaaa,bbbbbbbbbbbbbbbb" || a[0].Result != "ok" || a[0].JobID != "job-clean" {
		t.Fatalf("nhật ký = %+v", a)
	}
	for _, ip := range trashAuditIP(t, e, auditProbeCleanup) {
		if ip != auditIPTimer {
			t.Errorf("ip = %q, muốn %q", ip, auditIPTimer)
		}
	}
	if !e.s.api.claim() {
		t.Fatal("dọn xong mà chưa nhả chỗ chạy job")
	}
	e.s.api.release()
}

// Đang có job, hay CLI giữ khoá toàn cục: bỏ vòng dọn và nhả chỗ đã giành.
func TestUsageTimerDonBoKhiBan(t *testing.T) {
	e := apiNewEnv(t)
	old := cage.ProbeRun{ID: "aaaaaaaaaaaaaaaa", Started: e.clock().Add(-3 * time.Hour), State: "running"}
	ut, c := usageTestTimer(e, []cage.ProbeRun{old})
	ctx := context.Background()

	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if ut.cleanStale(ctx) || c.cleanup.Load() != 0 {
		t.Fatal("đang có job mà vẫn dọn")
	}
	if e.s.api.claim() {
		t.Fatal("vòng bị bỏ đã nhả chỗ chạy job của người khác")
	}
	e.s.api.release()

	unlock, err := e.f.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ut.cleanStale(ctx) || c.cleanup.Load() != 0 {
		t.Fatal("khoá bị giữ mà vẫn dọn")
	}
	unlock()
	if !e.s.api.claim() {
		t.Fatal("vòng bị bỏ vì khoá mà chưa nhả chỗ chạy job")
	}
	e.s.api.release()
	if len(hAudit(t, e, auditProbeCleanup)) != 0 {
		t.Error("bỏ vòng mà có nhật ký")
	}
}

// Cleanup trả ran=false (người khác đã dọn giữa hai lần đọc): không audit.
func TestUsageTimerDonKhongCoGi(t *testing.T) {
	e := apiNewEnv(t)
	old := cage.ProbeRun{ID: "aaaaaaaaaaaaaaaa", Started: e.clock().Add(-3 * time.Hour), State: "running"}
	ut, _ := usageTestTimer(e, []cage.ProbeRun{old})
	ut.cleanup = func(context.Context) (engine.Report, bool, error) { return engine.Report{}, false, nil }
	if ut.cleanStale(context.Background()) || len(hAudit(t, e, auditProbeCleanup)) != 0 {
		t.Fatal("ran=false mà có việc")
	}
	if !e.s.api.claim() {
		t.Fatal("chưa nhả chỗ chạy job")
	}
	e.s.api.release()
}

// Run: dọn unit tạm hỏng một lần lúc khởi động (argv, không shell), vòng đầu
// sau first rồi mỗi every; dừng khi ctx huỷ.
func TestUsageTimerRun(t *testing.T) {
	e := apiNewEnv(t)
	ut, c := usageTestTimer(e, nil)
	ut.first, ut.every = time.Millisecond, time.Millisecond
	rounds := make(chan struct{}, 16)
	ut.onRound = func() {
		select {
		case rounds <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); ut.Run(ctx) }()
	for i := range 3 {
		select {
		case <-rounds:
		case <-time.After(5 * time.Second):
			t.Fatalf("chỉ có %d vòng", i)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run không dừng khi ctx huỷ")
	}
	if c.sample.Load() < 3 {
		t.Fatalf("sample gọi %d lần", c.sample.Load())
	}
	var resets []string
	for _, cmd := range e.f.Ran() {
		if cmd.Name == "systemctl" && len(cmd.Args) > 0 && cmd.Args[0] == "reset-failed" {
			resets = append(resets, cmd.Name+" "+strings.Join(cmd.Args, " "))
		}
	}
	if !slices.Equal(resets, []string{"systemctl reset-failed tpan-cage-*-run-*"}) {
		t.Fatalf("reset-failed lúc khởi động = %q", resets)
	}
}
