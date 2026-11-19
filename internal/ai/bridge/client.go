package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/ai"
)

// LogWelcome là dòng Serve đưa vào Log đúng một lần, ngay khi hub nhận bộ não.
// Log không mang ngôn ngữ, nên `tpan brain` nhận ra dòng này để in câu "Bộ
// não: đã nối ..." song ngữ của nó. Các dòng khác của Log là dữ liệu, không
// dịch: "[job] ? <câu hỏi>", "[job] → <công cụ> <đối số>", "[job] done" hoặc
// "[job] error: ..." / "[job] refused: ...".
const LogWelcome = "welcome"

// logInputMax: đối số công cụ in ra terminal tối đa bấy nhiêu ký tự.
const logInputMax = 200

// Serve gửi hello, chờ welcome (refused: trả lỗi kèm lời của Hub), rồi nhận
// job, chạy Brain, gửi sự kiện tới khi Conn đóng (trả lỗi) hoặc ctx huỷ (trả
// nil). Mỗi lúc một job: hub không gửi job mới khi job cũ chưa báo kết thúc.
// Trước khi trả về, Serve huỷ job đang chạy (giết claude) và chờ nó dừng hẳn.
func (c *Client) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Đọc từ Conn không ngắt được bằng ctx: đóng Conn thì Decode trả lỗi ngay.
	go func() {
		<-ctx.Done()
		_ = c.Conn.Close()
	}()

	s := &clientSession{c: c, enc: json.NewEncoder(c.Conn)}
	if err := s.write(Msg{Type: MsgHello, Hello: &c.Hello}); err != nil {
		return c.ended(ctx, fmt.Errorf("gửi lời chào cho panel: %w", err))
	}
	dec := json.NewDecoder(c.Conn)
	var m Msg
	if err := dec.Decode(&m); err != nil {
		return c.ended(ctx, fmt.Errorf("chờ panel trả lời lời chào: %w", err))
	}
	switch m.Type {
	case MsgWelcome:
	case MsgRefused:
		return fmt.Errorf("panel từ chối bộ não này: %s", m.Text)
	default:
		return fmt.Errorf("panel trả lời lời chào bằng %q, không phải welcome", m.Type)
	}
	c.log(LogWelcome)

	defer s.wg.Wait() // chờ job đang dở dừng hẳn (claude đã bị giết)
	defer cancel()    // chạy trước Wait: huỷ job đang dở
	for {
		var m Msg
		if err := dec.Decode(&m); err != nil {
			return c.ended(ctx, fmt.Errorf("mất kết nối với panel: %w", err))
		}
		switch m.Type {
		case MsgJob:
			if m.Job != nil {
				s.start(ctx, *m.Job)
			}
		case MsgCancel:
			s.cancel(m.JobID)
		}
	}
}

// ended: ctx đã huỷ (Ctrl+C) thì lỗi đọc ghi chỉ là hệ quả của việc đóng Conn.
func (c *Client) ended(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func (c *Client) log(line string) {
	if c.Log != nil {
		c.Log(line)
	}
}

// clientSession là trạng thái của Serve sau khi được nhận.
type clientSession struct {
	c *Client

	wmu sync.Mutex // goroutine của job và vòng đọc cùng ghi
	enc *json.Encoder

	mu        sync.Mutex
	runningID string
	stop      context.CancelFunc
	wg        sync.WaitGroup
}

func (s *clientSession) write(m Msg) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.enc.Encode(m)
}

// event gửi một sự kiện của job id. Lỗi ghi bỏ qua: Conn hỏng thì vòng đọc
// cũng thấy và Serve trả về.
func (s *clientSession) event(id string, ev ai.Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	_ = s.write(Msg{Type: MsgEvent, JobID: id, Event: &ev})
}

func (s *clientSession) start(ctx context.Context, job ai.Job) {
	s.mu.Lock()
	if s.runningID != "" {
		s.mu.Unlock()
		// Hub giữ đúng một job một lúc nên không xảy ra; nếu có thì trả lỗi
		// cho job mới thay vì chạy hai claude chồng nhau.
		s.event(job.ID, ai.Event{Kind: ai.EventError, Text: ai.ErrBrainBusy.Error()})
		return
	}
	jctx, stop := context.WithCancel(ctx)
	s.runningID, s.stop = job.ID, stop
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			s.runningID, s.stop = "", nil
			s.mu.Unlock()
			stop()
		}()
		s.run(jctx, job)
	}()
}

func (s *clientSession) cancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningID == id && s.stop != nil {
		s.stop()
	}
}

// run chạy một job trên Brain và chuyển mọi sự kiện về hub. Luôn gửi đúng một
// sự kiện kết thúc, kể cả khi Brain không bắt đầu được hay đóng kênh thiếu.
func (s *clientSession) run(ctx context.Context, job ai.Job) {
	s.c.log(fmt.Sprintf("[%s] ? %s", job.ID, question(job.Prompt)))
	ch, err := s.c.Brain.Run(ctx, job)
	if err != nil {
		s.c.log(fmt.Sprintf("[%s] error: %v", job.ID, err))
		s.event(job.ID, ai.Event{Kind: ai.EventError, Text: err.Error()})
		return
	}
	final := false
	for ev := range ch {
		if final {
			continue // đọc cho hết kênh, bỏ thứ đến sau sự kiện kết thúc
		}
		switch {
		case ev.Kind == ai.EventToolCall:
			s.c.log(fmt.Sprintf("[%s] → %s %s", job.ID, ev.Tool, oneLine(compact(ev.Input), logInputMax)))
		case ev.Kind == ai.EventDone:
			s.c.log(fmt.Sprintf("[%s] done", job.ID))
		case ev.Final():
			s.c.log(fmt.Sprintf("[%s] %s: %s", job.ID, ev.Kind, oneLine(ev.Text, logInputMax)))
		}
		final = ev.Final()
		s.event(job.ID, ev)
	}
	if !final {
		s.event(job.ID, ai.Event{Kind: ai.EventError, Text: "bộ não dừng mà không báo kết thúc"})
	}
}

// question lấy phần <question>...</question> của Prompt (xem ai/panel_types.go);
// không có thẻ thì lấy cả Prompt. Gộp về một dòng để terminal dễ đọc.
func question(prompt string) string {
	q := prompt
	if _, after, ok := strings.Cut(prompt, "<question>"); ok {
		q, _, _ = strings.Cut(after, "</question>")
	}
	return oneLine(q, 0)
}

// oneLine gộp khoảng trắng và xuống dòng thành một dấu cách; max > 0 thì cắt
// bớt (theo rune, không cắt đôi chữ có dấu).
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); max > 0 && len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func compact(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}
