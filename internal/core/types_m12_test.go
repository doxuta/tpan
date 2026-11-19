package core

import (
	"context"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// Planned không có Journal thì xem trước được nhưng không chạy: chạy mà không
// ghi được job là thay đổi máy không để lại lịch sử.
func TestPlannedNeedsJournal(t *testing.T) {
	called := false
	p := newPlanned(system.NewFake(), nil, engine.Plan{Kind: "test.m12"}, func(context.Context, engine.Report) error {
		called = true
		return nil
	})
	if _, err := p.Preview(context.Background()); err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if _, err := p.Execute(context.Background(), nil); err == nil {
		t.Fatal("Execute không Journal phải lỗi")
	}
	if called {
		t.Fatal("after không được chạy khi job không chạy")
	}
}
