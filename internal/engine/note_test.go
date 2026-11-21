package engine

import (
	"context"
	"testing"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// noteStep gọi Note trong Apply, không chạy lệnh nào.
type noteStep struct{ applied bool }

func (s *noteStep) Info() StepInfo {
	return StepInfo{ID: "note:test", Title: i18n.T("ghi chú", "note")}
}
func (s *noteStep) Check(context.Context, system.System) (bool, error) { return s.applied, nil }
func (s *noteStep) Apply(_ context.Context, sys system.System) error {
	Note(sys, "đã tải 50%")
	s.applied = true
	return nil
}
func (s *noteStep) Undo(context.Context, system.System) error { return nil }

func TestNoteGhiVaoLogJob(t *testing.T) {
	var got []Event
	e := New(system.NewFake(), Options{OnEvent: func(ev Event) { got = append(got, ev) }})
	rep, err := e.Execute(context.Background(), Plan{Kind: "t", Target: "t", Steps: []Step{&noteStep{}}})
	if err != nil || rep.Job.Status != JobSucceeded {
		t.Fatalf("Execute: %v, %s", err, rep.Job.Status)
	}
	found := false
	for _, ev := range got {
		if ev.Kind == EventOutput && ev.StepID == "note:test" && ev.Message == "đã tải 50%" {
			found = true
		}
	}
	if !found {
		t.Fatalf("không có sự kiện output của Note: %+v", got)
	}
	// Ngoài Execute: không làm gì, không panic.
	Note(system.NewFake(), "bỏ qua")
}
