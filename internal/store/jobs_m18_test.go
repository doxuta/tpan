package store

import (
	"context"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

func TestAbortStaleJobs(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	start := time.Now().Add(-time.Hour)
	for _, j := range []engine.Job{
		{ID: "a1", Kind: "backup.push", Target: "a.example.com", Status: engine.JobRunning, Started: start},
		{ID: "a2", Kind: "backup.panel", Target: "panel", Status: engine.JobRunning, Started: start},
		{ID: "a3", Kind: "backup.create", Target: "a.example.com", Status: engine.JobRunning, Started: start},
		{ID: "a4", Kind: "backup.push", Target: "b.example.com", Status: engine.JobSucceeded, Started: start, Finished: start},
	} {
		j.Title = i18n.T("t", "t")
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.AbortStaleJobs(ctx, []string{"backup.push", "backup.pull", "backup.panel"}, "đứt vì tiến trình dừng")
	if err != nil || n != 2 {
		t.Fatalf("AbortStaleJobs = %d, %v; muốn 2", n, err)
	}
	for id, want := range map[string]engine.JobStatus{"a1": engine.JobRolledBack, "a2": engine.JobRolledBack, "a3": engine.JobRunning, "a4": engine.JobSucceeded} {
		j, err := s.Job(ctx, id)
		if err != nil || j.Status != want {
			t.Fatalf("job %s: %s err=%v, muốn %s", id, j.Status, err, want)
		}
		if want == engine.JobRolledBack && (j.Error != "đứt vì tiến trình dừng" || j.Finished.IsZero()) {
			t.Fatalf("job %s đóng thiếu lý do hoặc giờ: %+v", id, j)
		}
	}
	if n, err := s.AbortStaleJobs(ctx, nil, "x"); n != 0 || err != nil {
		t.Fatalf("kinds rỗng: %d, %v", n, err)
	}
}
