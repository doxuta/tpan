package store

// Test sổ phép thử tấn công (bảng probe_runs của migration 1700).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/cage"
)

// Khẳng định đúng chữ ký của core.ProbeStore (store không import core).
var _ interface {
	StartProbeRun(ctx context.Context, r cage.ProbeRun) error
	FinishProbeRun(ctx context.Context, id, state string, report []byte, at time.Time) error
	MarkProbeCleaned(ctx context.Context, id string) error
	ProbeRun(ctx context.Context, id string) (r cage.ProbeRun, ok bool, err error)
	ProbeRuns(ctx context.Context, limit int) ([]cage.ProbeRun, error)
	PendingProbeRuns(ctx context.Context) ([]cage.ProbeRun, error)
} = (*Store)(nil)

func probeSample(id string, started time.Time) cage.ProbeRun {
	return cage.ProbeRun{
		ID: id, Started: started, State: "running",
		DomainA: "probe-a.example.com", DomainB: "probe-b.example.com", Mode: "ngoai",
	}
}

func TestSoPhepThuVongDoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := probeSample("0123456789abcdef", t0)
	if err := s.StartProbeRun(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.ProbeRun(ctx, in.ID)
	if err != nil || !ok {
		t.Fatalf("ProbeRun = ok %v, err %v", ok, err)
	}
	if got.State != "running" || got.Cleaned || !got.Finished.IsZero() || got.Report != nil || !got.Started.Equal(t0) ||
		got.DomainA != in.DomainA || got.DomainB != in.DomainB || got.Mode != "ngoai" {
		t.Errorf("sau Start = %+v", got)
	}

	// Report là JSON; dạng []byte vào, []byte ra, nguyên văn.
	report := []byte(`{"id":"0123456789abcdef","results":[{"path":"L1"}],"note":"đủ dấu"}`)
	end := t0.Add(2 * time.Minute)
	if err := s.FinishProbeRun(ctx, in.ID, "done", report, end); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.ProbeRun(ctx, in.ID)
	if got.State != "done" || !got.Finished.Equal(end) || string(got.Report) != string(report) || got.Cleaned {
		t.Errorf("sau Finish = %+v", got)
	}

	if err := s.MarkProbeCleaned(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.ProbeRun(ctx, in.ID)
	if !got.Cleaned || got.State != "done" {
		t.Errorf("sau MarkCleaned = %+v", got)
	}
}

func TestSoPhepThuLoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, ok, err := s.ProbeRun(ctx, "khong-co"); ok || err != nil {
		t.Errorf("ProbeRun không có = ok %v, err %v; muốn false, nil", ok, err)
	}
	if err := s.FinishProbeRun(ctx, "khong-co", "done", nil, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("FinishProbeRun không có dòng = %v, muốn ErrNotFound", err)
	}
	if err := s.MarkProbeCleaned(ctx, "khong-co"); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkProbeCleaned không có dòng = %v, muốn ErrNotFound", err)
	}
	if err := s.StartProbeRun(ctx, probeSample("", t0)); err == nil {
		t.Error("Start thiếu ID phải lỗi")
	}
	if err := s.StartProbeRun(ctx, probeSample("x1", t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.StartProbeRun(ctx, probeSample("x1", t0)); err == nil {
		t.Error("Start trùng ID phải lỗi")
	}
	bad := probeSample("x2", t0)
	bad.Mode = "linh-tinh"
	if err := s.StartProbeRun(ctx, bad); err == nil {
		t.Error("Mode lạ phải lỗi (CHECK)")
	}
	// State kết thúc chỉ là done hay failed; "running" không phải kết thúc.
	for _, st := range []string{"running", "xong", ""} {
		if err := s.FinishProbeRun(ctx, "x1", st, nil, t0); err == nil {
			t.Errorf("Finish với state %q phải lỗi", st)
		}
	}
	// Failed không báo cáo: Report rỗng, không phải lỗi.
	if err := s.FinishProbeRun(ctx, "x1", "failed", nil, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.ProbeRun(ctx, "x1"); got.State != "failed" || got.Report != nil {
		t.Errorf("sau Finish failed = %+v", got)
	}
}

func TestSoPhepThuDanhSach(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	// Chèn không theo thứ tự thời gian để chắc là ORDER BY.
	for _, r := range []struct {
		id string
		at time.Duration
	}{{"b", time.Hour}, {"d", 3 * time.Hour}, {"a", 0}, {"c", 2 * time.Hour}} {
		if err := s.StartProbeRun(ctx, probeSample(r.id, t0.Add(r.at))); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(rs []cage.ProbeRun) string {
		out := ""
		for _, r := range rs {
			out += r.ID
		}
		return out
	}

	all, err := s.ProbeRuns(ctx, 10)
	if err != nil || ids(all) != "dcba" {
		t.Errorf("ProbeRuns(10) = %q, %v; muốn mới trước: dcba", ids(all), err)
	}
	two, _ := s.ProbeRuns(ctx, 2)
	if ids(two) != "dc" {
		t.Errorf("ProbeRuns(2) = %q, muốn dc", ids(two))
	}
	if none, err := s.ProbeRuns(ctx, 0); err != nil || none == nil || len(none) != 0 {
		t.Errorf("ProbeRuns(0) = %#v, %v; muốn slice rỗng không nil", none, err)
	}

	// Pending: cũ trước, chỉ Cleaned = false. b và c đã dọn.
	for _, id := range []string{"b", "c"} {
		if err := s.MarkProbeCleaned(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	pend, err := s.PendingProbeRuns(ctx)
	if err != nil || ids(pend) != "ad" {
		t.Errorf("PendingProbeRuns = %q, %v; muốn cũ trước: ad", ids(pend), err)
	}
	for _, id := range []string{"a", "d"} {
		if err := s.MarkProbeCleaned(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if pend, err := s.PendingProbeRuns(ctx); err != nil || pend == nil || len(pend) != 0 {
		t.Errorf("hết dòng chưa dọn = %#v, %v; muốn slice rỗng không nil", pend, err)
	}
}
