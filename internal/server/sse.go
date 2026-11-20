package server

// Luồng sự kiện của job: GET /api/v1/jobs/{id}/events (Server-Sent Events).
//
// Định dạng theo chuẩn HTML "Server-sent events" (đã tra 19/11/2026):
// https://html.spec.whatwg.org/multipage/server-sent-events.html
// Mỗi sự kiện là các dòng "id:", "event:", "data:" rồi một dòng trống. data là
// JSON một dòng (encoding/json không bao giờ sinh ký tự xuống dòng). Trình
// duyệt tự nối lại khi mất kết nối và gửi header Last-Event-ID = Seq cuối cùng
// nó đã nhận; ?after=N làm việc tương tự cho client tự viết.
//
// Hai nguồn sự kiện:
//   - job đang chạy trong tiến trình này: bộ phát sseFeed của job, nhận sự
//     kiện trực tiếp từ onEvent của engine;
//   - job khác (đã xong, hoặc đang chạy ở tiến trình khác như CLI): đọc từ
//     store. Job chưa xong thì đọc lại mỗi sseStorePoll cho tới job.done.
//
// Luồng đóng ngay sau khi gửi job.done, hoặc khi client ngắt (context của
// request bị huỷ): không goroutine nào còn chờ.
//
// EventSource tự nối lại cả khi server đóng luồng bình thường; chỉ dừng khi
// trang gọi close() hoặc server trả 204. Nên job đã xong mà không còn sự kiện
// nào sau Last-Event-ID thì trả 204, để trình duyệt không nối lại mãi.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
)

// sseStorePoll: khoảng đọc lại store cho job không có bộ phát trong tiến trình này.
const sseStorePoll = 500 * time.Millisecond

// sseFeed là bộ phát của một job. Nó giữ mọi sự kiện của job trong bộ nhớ (job
// sống ngắn; bộ phát bị bỏ khi job xong) và đánh thức người nghe bằng cách
// đóng kênh wake rồi thay kênh mới. Người phát (engine) không bao giờ phải chờ
// người nghe chậm: mỗi người nghe tự đọc phần mình chưa gửi.
type sseFeed struct {
	mu     sync.Mutex
	events []engine.Event
	done   bool // đã có job.done, hoặc job đã kết thúc
	wake   chan struct{}
}

func sseNewFeed() *sseFeed { return &sseFeed{wake: make(chan struct{})} }

func (f *sseFeed) publish(ev engine.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	if ev.Kind == engine.EventJobDone {
		f.done = true
	}
	close(f.wake)
	f.wake = make(chan struct{})
}

// close đánh dấu job đã kết thúc kể cả khi không có job.done (engine lỗi giữa chừng).
func (f *sseFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.done {
		f.done = true
		close(f.wake)
		f.wake = make(chan struct{})
	}
}

func (f *sseFeed) finished() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.done
}

// since trả các sự kiện có Seq > after, job đã kết thúc chưa, và kênh sẽ đóng
// khi có thêm sự kiện. Ba thứ lấy cùng một lúc dưới khoá nên không lọt sự kiện.
func (f *sseFeed) since(after int) ([]engine.Event, bool, <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []engine.Event
	for _, ev := range f.events {
		if ev.Seq > after {
			out = append(out, ev)
		}
	}
	return out, f.done, f.wake
}

// sseAfter đọc chỗ nối lại: header Last-Event-ID, không có thì ?after=N.
func sseAfter(r *http.Request) (int, error) {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("after")
	}
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("giá trị Last-Event-ID/after %q không phải số không âm", v)
	}
	return n, nil
}

// sseStream ghi sự kiện ra một response đã mở luồng.
type sseStream struct {
	w    http.ResponseWriter
	rc   *http.ResponseController
	last int  // Seq lớn nhất đã gửi
	sent bool // đã gửi job.done
	// hide (M13, foreignCheck): sự kiện "check" của phép kiểm bị giấu thì không
	// gửi, nhưng last vẫn tiến qua nó (nối lại và job.done không lệch).
	hide func(id string) bool
}

// send ghi các sự kiện có Seq > last, rồi flush một lần. Lỗi ghi nghĩa là
// client đã ngắt.
func (st *sseStream) send(evs []engine.Event) error {
	n := 0
	for _, ev := range evs {
		if ev.Seq <= st.last || st.sent {
			continue
		}
		if st.hide != nil && ev.Check != nil && st.hide(ev.Check.ID) {
			st.last = ev.Seq
			continue
		}
		data, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("mã hoá sự kiện %d: %w", ev.Seq, err)
		}
		if _, err := fmt.Fprintf(st.w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Kind, data); err != nil {
			return err
		}
		st.last, n = ev.Seq, n+1
		if ev.Kind == engine.EventJobDone {
			st.sent = true
		}
	}
	if n == 0 {
		return nil
	}
	return st.rc.Flush()
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	after, err := sseAfter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "after_invalid", i18n.T(err.Error(), "Last-Event-ID/after must be a non-negative number"))
		return
	}
	job, err := s.deps.Store.Job(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job_not_found", i18n.T("Không có job "+id+".", "No job "+id+"."))
			return
		}
		apiFail(w, r, err)
		return
	}
	run := s.api.run(id)
	if run == nil && job.Status != engine.JobRunning {
		evs, err := s.deps.Store.Events(ctx, id, after)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		if len(evs) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	hide, err := s.foreignCheck(r)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	st := &sseStream{w: w, rc: http.NewResponseController(w), last: after, hide: hide}
	// Gửi header ngay để EventSource ở trình duyệt chuyển sang "open".
	if err := st.rc.Flush(); err != nil {
		return
	}

	if run != nil {
		sseFromFeed(ctx, st, run.feed)
		return
	}
	s.sseFromStore(ctx, st, id)
}

// sseFromFeed gửi từ bộ phát của job cho tới job.done hoặc client ngắt.
func sseFromFeed(ctx context.Context, st *sseStream, f *sseFeed) {
	for {
		evs, done, wake := f.since(st.last)
		if err := st.send(evs); err != nil || st.sent || done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}

// sseFromStore gửi từ store. Job đã xong thì gửi hết rồi đóng; chưa xong (job
// của tiến trình khác) thì đọc lại định kỳ.
func (s *Server) sseFromStore(ctx context.Context, st *sseStream, id string) {
	for {
		evs, err := s.deps.Store.Events(ctx, id, st.last)
		if err != nil || st.send(evs) != nil || st.sent {
			return
		}
		job, err := s.deps.Store.Job(ctx, id)
		if err != nil {
			return
		}
		if job.Status != engine.JobRunning {
			// Đọc lần cuối: sự kiện có thể được ghi giữa hai lần đọc trên.
			if evs, err := s.deps.Store.Events(ctx, id, st.last); err == nil {
				_ = st.send(evs)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sseStorePoll):
		}
	}
}
