package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
)

type fakeAudit struct{ got []store.AuditEntry }

func (f *fakeAudit) AddAudit(_ context.Context, a store.AuditEntry) error {
	f.got = append(f.got, a)
	return nil
}

func TestCLIAudit(t *testing.T) {
	e := &env{stderr: &bytes.Buffer{}}
	f := &fakeAudit{}
	ctx := context.Background()

	cliAudit(ctx, e, f, "site.add", "example.com", engine.Job{}) // chưa có job: không ghi
	cliAudit(ctx, e, f, "site.add", "example.com", engine.Job{ID: "a1", Status: engine.JobSucceeded})
	cliAudit(ctx, e, f, "site.rm", "example.com", engine.Job{ID: "b2", Status: engine.JobRolledBack})
	cliAudit(ctx, e, struct{}{}, "site.add", "example.com", engine.Job{ID: "c3"}) // không ghi được: bỏ qua

	if len(f.got) != 2 {
		t.Fatalf("ghi %d dòng, muốn 2: %+v", len(f.got), f.got)
	}
	if g := f.got[0]; g.IP != "cli" || g.Action != "site.add" || g.Result != "ok" || g.JobID != "a1" {
		t.Errorf("dòng 1 = %+v", g)
	}
	if g := f.got[1]; g.Result != "rolled_back" || g.JobID != "b2" {
		t.Errorf("dòng 2 = %+v", g)
	}
}
