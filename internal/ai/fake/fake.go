// Package fake là bộ não giả: phát lại kịch bản soạn sẵn (mục 8.2 của
// docs/KE-HOACH.md), cho unit test và cho `tpan serve --demo` trên máy không
// có Claude Code.
//
// Hợp đồng M8:
//
//	// Run phát lần lượt các sự kiện của Script(job), cách nhau Delay, rồi
//	// đóng kênh. Script nil thì dùng DemoScript. Sự kiện kết thúc cuối kịch bản
//	// thiếu thì tự thêm EventDone. ctx huỷ: phát EventError rồi đóng.
//	func (b *Brain) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error)
//	// DemoScript là kịch bản của chế độ demo: init (Version claudecode.TestedVersion
//	// không bắt buộc, Model "sonnet"), vài đoạn chữ hiện dần bằng ngôn ngữ
//	// job.Lang có nhắc lại một bí danh site (site-a.example) và một liên kết
//	// tpan:site/site-a.example, một tool_call mcp__tpan__list_sites và
//	// tool_result của nó, rồi done với toàn bộ câu trả lời. Nói rõ đây là bộ
//	// não giả của chế độ demo.
//	func DemoScript(job ai.Job) []ai.Event
package fake

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/ai/claudecode"
)

// Brain là bộ não giả. Script rỗng thì dùng DemoScript. Delay là khoảng nghỉ
// trước mỗi sự kiện (0: phát liền, cho test; chế độ demo đặt vài trăm ms để
// chữ hiện dần như thật).
type Brain struct {
	Script func(job ai.Job) []ai.Event
	Delay  time.Duration
}

// Brain thoả ai.Brain: kiểm lúc biên dịch để đổi chữ ký ở gói ai thì lỗi ngay.
var _ ai.Brain = (*Brain)(nil)

// Run phát kịch bản trong một goroutine. Kênh có chỗ cho mọi sự kiện kể cả
// EventError chen vào khi huỷ, nên goroutine không bao giờ kẹt ở lệnh gửi dù
// bên nhận đã bỏ đi: không rò goroutine.
func (b *Brain) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error) {
	script := b.Script
	if script == nil {
		script = DemoScript
	}
	events := finish(script(job))
	out := make(chan ai.Event, len(events)+1)
	go func() {
		defer close(out)
		for _, ev := range events {
			if b.Delay > 0 {
				select {
				case <-time.After(b.Delay):
				case <-ctx.Done():
					out <- cancelled(ctx)
					return
				}
			} else if ctx.Err() != nil {
				out <- cancelled(ctx)
				return
			}
			if ev.Time.IsZero() {
				ev.Time = time.Now()
			}
			out <- ev
		}
	}()
	return out, nil
}

func cancelled(ctx context.Context) ai.Event {
	return ai.Event{Kind: ai.EventError, Time: time.Now(), Text: "bộ não giả bị dừng: " + ctx.Err().Error()}
}

// finish cắt kịch bản ở sự kiện kết thúc đầu tiên (kênh đóng sau đúng một sự
// kiện kết thúc) và thêm EventDone nếu kịch bản không có. Text của EventDone
// thêm vào là phần chữ đã phát, như bộ não thật.
func finish(script []ai.Event) []ai.Event {
	var text strings.Builder
	for i, ev := range script {
		if ev.Final() {
			return script[:i+1]
		}
		if ev.Kind == ai.EventText {
			text.WriteString(ev.Text)
		}
	}
	return append(script[:len(script):len(script)], ai.Event{Kind: ai.EventDone, Text: text.String()})
}

// demoText là các đoạn chữ của một câu trả lời demo, theo ngôn ngữ giao diện.
// Đoạn trước lần gọi công cụ, rồi đoạn sau.
type demoText struct{ before, after []string }

var demoVi = demoText{
	before: []string{
		"Đây là bộ não giả của chế độ demo, không phải Claude thật. ",
		"Mình xem danh sách site trước. ",
	},
	after: []string{
		"Máy có một site là site-a.example và nó đang chạy bình thường. ",
		"Xem chi tiết ở [site-a.example](tpan:site/site-a.example). ",
		"Câu trả lời này soạn sẵn, không dựa trên dữ liệu của bạn.",
	},
}

var demoEn = demoText{
	before: []string{
		"This is the demo mode's fake brain, not the real Claude. ",
		"Let me list the sites first. ",
	},
	after: []string{
		"The machine has one site, site-a.example, and it is running normally. ",
		"See the details at [site-a.example](tpan:site/site-a.example). ",
		"This answer is scripted and not based on your data.",
	},
}

// DemoScript là kịch bản của `tpan serve --demo`. Hàm thuần: Time để trống
// (Run điền lúc phát) nên hai lần gọi cho cùng kết quả.
func DemoScript(job ai.Job) []ai.Event {
	t := demoVi
	if job.Lang == "en" {
		t = demoEn
	}
	var full strings.Builder
	var evs []ai.Event
	text := func(parts []string) {
		for _, p := range parts {
			full.WriteString(p)
			evs = append(evs, ai.Event{Kind: ai.EventText, Text: p})
		}
	}
	evs = append(evs, ai.Event{Kind: ai.EventInit, Version: claudecode.TestedVersion, Model: ai.DefaultModel})
	text(t.before)
	evs = append(evs,
		ai.Event{Kind: ai.EventToolCall, Tool: "mcp__tpan__list_sites", Input: json.RawMessage(`{}`)},
		ai.Event{Kind: ai.EventToolResult, Tool: "mcp__tpan__list_sites",
			Text: `[{"domain":"site-a.example","status":"ok","ssl":true}]`},
	)
	text(t.after)
	return append(evs, ai.Event{Kind: ai.EventDone, Text: full.String()})
}
