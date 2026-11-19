package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
)

// sseScriptedJob tạo job id trong store rồi phát sự kiện theo kịch bản:
// job.start, chờ step, rồi output và job.done. Ghi store trước rồi mới gọi
// onEvent, giống engine.
func sseScriptedJob(e *apiEnv, id string, step <-chan struct{}) jobFunc {
	return func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		now := e.clock()
		job := engine.Job{ID: id, Kind: "site.rm", Target: "a.example.com", Status: engine.JobRunning, Started: now}
		if err := e.st.CreateJob(ctx, job); err != nil {
			return jobResult{}, err
		}
		emit := func(seq int, kind engine.EventKind, msg string) {
			ev := engine.Event{JobID: id, Seq: seq, Time: now, Kind: kind, Message: msg}
			_ = e.st.AddEvent(ctx, ev)
			onEvent(ev)
		}
		emit(1, engine.EventJobStart, "")
		<-step
		emit(2, engine.EventOutput, "dòng một")
		emit(3, engine.EventJobDone, string(engine.JobSucceeded))
		job.Status, job.Finished = engine.JobSucceeded, now
		return jobResult{status: engine.JobSucceeded}, e.st.FinishJob(ctx, job)
	}
}

// sseStartScripted chạy sseScriptedJob qua jobStart; trả hàm mở bước tiếp.
func sseStartScripted(t *testing.T, e *apiEnv, id string) func() {
	t.Helper()
	step := make(chan struct{})
	var once sync.Once
	next := func() { once.Do(func() { close(step) }) }
	t.Cleanup(next) // chạy trước wg.Wait của apiNewEnv
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	rec := httptest.NewRecorder()
	e.s.jobStart(rec, httptest.NewRequest("DELETE", "/", nil), "site.rm", "a.example.com", sseScriptedJob(e, id, step))
	if js := apiDecode[JobStarted](t, rec, 202); js.JobID != id {
		t.Fatalf("job id = %q", js.JobID)
	}
	return next
}

// sseIDs đọc các dòng "id: N" trong thân SSE.
func sseIDs(body string) []string {
	var ids []string
	for line := range strings.Lines(body) {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "id: "); ok {
			ids = append(ids, v)
		}
	}
	return ids
}

func TestSSEFinishedJobReplay(t *testing.T) {
	e := apiNewEnv(t)
	next := sseStartScripted(t, e, "job1")
	next()
	e.s.api.wg.Wait()

	rec := apiCall(e.s.handleJobEvents, "GET", "/", "", "id", "job1")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/event-stream" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("mã %d, header %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "id: 1\nevent: job.start\ndata: {") || !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("thân SSE sai dạng:\n%s", body)
	}
	if got := strings.Join(sseIDs(body), ","); got != "1,2,3" {
		t.Fatalf("id = %s, muốn 1,2,3", got)
	}

	// Nối lại bằng Last-Event-ID và bằng ?after.
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("id", "job1")
	r.Header.Set("Last-Event-ID", "1")
	rec = httptest.NewRecorder()
	e.s.handleJobEvents(rec, r)
	if got := strings.Join(sseIDs(rec.Body.String()), ","); got != "2,3" {
		t.Fatalf("Last-Event-ID 1: id = %s, muốn 2,3", got)
	}
	// Đã nhận hết rồi mà nối lại: 204 để EventSource thôi nối lại.
	if rec := apiCall(e.s.handleJobEvents, "GET", "/?after=3", "", "id", "job1"); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("after=3: mã %d, thân %q; muốn 204 rỗng", rec.Code, rec.Body.String())
	}

	if got := apiErrCode(t, apiCall(e.s.handleJobEvents, "GET", "/?after=x", "", "id", "job1"), 400); got != "after_invalid" {
		t.Errorf("code = %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleJobEvents, "GET", "/", "", "id", "khongco"), 404); got != "job_not_found" {
		t.Errorf("code = %q", got)
	}
}

// TestSSELive: người nghe nhận sự kiện ngay khi engine phát (không chờ job
// xong), và luồng đóng sau job.done.
func TestSSELive(t *testing.T) {
	e := apiNewEnv(t)
	next := sseStartScripted(t, e, "job2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", "job2")
		e.s.handleJobEvents(w, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	read := func() (string, bool) {
		select {
		case l, ok := <-lines:
			return l, ok
		case <-time.After(5 * time.Second):
			t.Fatal("chờ sự kiện quá lâu")
			return "", false
		}
	}
	if l, _ := read(); l != "id: 1" {
		t.Fatalf("dòng đầu = %q, muốn id: 1 (phát trực tiếp khi job còn chạy)", l)
	}
	next()
	var ids []string
	for {
		l, ok := read()
		if !ok {
			break // server đóng luồng sau job.done
		}
		if v, ok := strings.CutPrefix(l, "id: "); ok {
			ids = append(ids, v)
		}
	}
	if strings.Join(ids, ",") != "2,3" {
		t.Fatalf("id sau đó = %v, muốn 2,3", ids)
	}
}

// TestSSEClientGone: client ngắt (context của request bị huỷ) thì handler trả
// về ngay, cả khi nghe bộ phát lẫn khi đọc lại store; không goroutine nào treo.
func TestSSEClientGone(t *testing.T) {
	e := apiNewEnv(t)
	ctx := context.Background()
	for _, id := range []string{"live", "other"} {
		if err := e.st.CreateJob(ctx, engine.Job{ID: id, Kind: "site.add", Target: "a.example.com", Status: engine.JobRunning, Started: e.clock()}); err != nil {
			t.Fatal(err)
		}
	}
	// "live" có bộ phát trong tiến trình này nhưng không bao giờ xong;
	// "other" là job của tiến trình khác (chỉ có trong store), cũng chưa xong.
	e.s.api.mu.Lock()
	e.s.api.runs["live"] = &jobRun{id: "live", feed: sseNewFeed(), done: make(chan struct{})}
	e.s.api.mu.Unlock()

	for _, id := range []string{"live", "other"} {
		t.Run(id, func(t *testing.T) {
			rctx, cancel := context.WithCancel(ctx)
			r := httptest.NewRequest("GET", "/", nil).WithContext(rctx)
			r.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			returned := make(chan struct{})
			go func() {
				e.s.handleJobEvents(rec, r)
				close(returned)
			}()
			select {
			case <-returned:
				t.Fatal("job chưa xong mà luồng đã đóng")
			case <-time.After(100 * time.Millisecond):
			}
			cancel()
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("client đã ngắt mà handler vẫn chạy (rò goroutine)")
			}
		})
	}
}
