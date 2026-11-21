package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// stepTen là một bước chỉ có Info, để dựng Plan cho bộ in tiến độ.
type stepTen struct{ info engine.StepInfo }

func (s stepTen) Info() engine.StepInfo                              { return s.info }
func (s stepTen) Check(context.Context, system.System) (bool, error) { return false, nil }
func (s stepTen) Apply(context.Context, system.System) error         { return nil }
func (s stepTen) Undo(context.Context, system.System) error          { return nil }

// Soát cuối M17, pha 2: Plan của phép thử tấn công có hai bước cùng ID (vhost
// lúc tạo site thử rồi vhost trong lồng cùng "file:<đường dẫn>"; ghi rồi gỡ file
// của lồng cùng "cage.files:<domain>"). Bộ in tiến độ tra tiêu đề theo ID nên
// bước sau đè tên bước trước: trên máy ảo, log in "Gỡ file của lồng" cho bước
// đang GHI file của lồng. Tiêu đề phải theo đúng lần xuất hiện, cả khi hoàn tác
// (ngược thứ tự, chỉ bước đã xong).
func TestProgressTieuDeTheoLanXuatHien(t *testing.T) {
	step := func(id, title string, noUndo string) engine.Step {
		info := engine.StepInfo{ID: id, Title: i18n.T(title, title)}
		if noUndo != "" {
			info.NoUndo = i18n.T(noUndo, noUndo)
		}
		return stepTen{info}
	}
	plan := engine.Plan{Steps: []engine.Step{
		step("file:/v", "Ghi vhost thường", ""),
		step("cage.files:a", "Ghi file của lồng", ""),
		step("reap:a", "Dừng tiến trình", "không trả lại được"),
		step("file:/v", "Ghi vhost trong lồng", ""),
		step("cage.files:a", "Gỡ file của lồng", ""),
	}}
	var out bytes.Buffer
	pr := newProgress(&out, "vi", false, plan)
	ev := func(k engine.EventKind, id string) { pr.onEvent(engine.Event{Kind: k, StepID: id}) }
	// Chạy xuôi: bước 4 (vhost trong lồng) đã đúng nên bỏ qua; bước 5 hỏng.
	for _, id := range []string{"file:/v", "cage.files:a", "reap:a"} {
		ev(engine.EventStepStart, id)
		ev(engine.EventStepDone, id)
	}
	ev(engine.EventStepStart, "file:/v")
	ev(engine.EventStepSkip, "file:/v")
	ev(engine.EventStepStart, "cage.files:a")
	ev(engine.EventStepFail, "cage.files:a")
	// Engine hoàn tác ngược thứ tự, chỉ bước đã xong: 3, 2, 1.
	for _, id := range []string{"reap:a", "cage.files:a", "file:/v"} {
		ev(engine.EventUndoStart, id)
		ev(engine.EventUndoDone, id)
	}

	var got []string
	for _, ln := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(ln, "- ") || strings.HasPrefix(ln, "  không có gì") {
			got = append(got, ln)
		}
	}
	want := []string{
		"- Ghi vhost thường ...",
		"- Ghi file của lồng ...",
		"- Dừng tiến trình ...",
		"- Ghi vhost trong lồng ...",
		"- Gỡ file của lồng ...",
		"- Hoàn tác: Dừng tiến trình ...",
		"  không có gì để hoàn tác: không trả lại được",
		"- Hoàn tác: Ghi file của lồng ...",
		"- Hoàn tác: Ghi vhost thường ...",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tiêu đề in ra:\n%s\nmuốn:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
