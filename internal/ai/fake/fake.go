// Package fake là bộ não giả: phát lại kịch bản soạn sẵn (mục 8.2 của
// docs/KE-HOACH.md), cho unit test và cho `tpan serve --demo` trên máy không
// có Claude Code.
//
// Hợp đồng M8. Thợ đợt 1 viết thân (cùng test):
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
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/ai"
)

// Brain là bộ não giả.
type Brain struct {
	Script func(job ai.Job) []ai.Event
	Delay  time.Duration
}

var errTodo = errors.New("fake: chưa làm (M8 đợt 1)")

// Thân tạm của đợt 0.
func (b *Brain) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error) { return nil, errTodo }

// DemoScript: thân tạm của đợt 0.
func DemoScript(job ai.Job) []ai.Event { return nil }
