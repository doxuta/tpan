package health

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
)

// probeResult chạy selfChecks và trả kết quả probe.leftover.
func probeResult(t *testing.T, e *env) checks.Result {
	t.Helper()
	cs, err := e.r.selfChecks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if r := c.Run(context.Background(), e.sys); r.ID == "probe.leftover:tpan" {
			return r
		}
	}
	t.Fatal("selfChecks không có probe.leftover:tpan")
	return checks.Result{}
}

// M17: lượt thử chưa dọn chỉ bị báo khi đã bắt đầu quá 2 giờ (lượt đang chạy
// thì không), và lượt đã dọn không bao giờ bị báo.
func TestSelfChecksProbeLeftover(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if r := probeResult(t, e); r.Status != checks.Pass {
		t.Fatalf("sổ rỗng: %s %s", r.Status, r.Summary.Vi)
	}
	run := func(id string, started time.Time) cage.ProbeRun {
		return cage.ProbeRun{ID: id, Started: started, State: "running",
			DomainA: "tpan-probe-a-" + id[:12] + ".test", DomainB: "tpan-probe-b-" + id[:12] + ".test"}
	}
	fresh := run("00000000000000aa", e.now.Add(-30*time.Minute))
	old := run("00000000000000bb", e.now.Add(-3*time.Hour))
	cleaned := run("00000000000000cc", e.now.Add(-5*time.Hour))
	for _, r := range []cage.ProbeRun{fresh, old, cleaned} {
		if err := e.st.StartProbeRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.MarkProbeCleaned(ctx, cleaned.ID); err != nil {
		t.Fatal(err)
	}
	r := probeResult(t, e)
	if r.Status != checks.Fail || r.Stage != checks.StageTPAN {
		t.Fatalf("có lượt cũ chưa dọn: %s %s", r.Status, r.Summary.Vi)
	}
	if !strings.Contains(r.Summary.Vi, old.ID) || strings.Contains(r.Summary.Vi, fresh.ID) || strings.Contains(r.Summary.Vi, cleaned.ID) {
		t.Errorf("chỉ lượt quá 2 giờ và chưa dọn được báo: %s", r.Summary.Vi)
	}
	// Lượt mới chưa tới 2 giờ: không báo.
	if err := e.st.MarkProbeCleaned(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if r := probeResult(t, e); r.Status != checks.Pass {
		t.Errorf("chỉ còn lượt đang chạy: %s %s", r.Status, r.Summary.Vi)
	}
	// Đọc sổ lỗi (DB đã đóng): Unknown, không Fail.
	_ = e.st.Close()
	if r := probeResult(t, e); r.Status != checks.Unknown {
		t.Errorf("sổ lỗi: %s", r.Status)
	}
}
