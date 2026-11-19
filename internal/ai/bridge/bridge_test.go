package bridge

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/ai/fake"
)

// sockPath trả đường dẫn socket trong một thư mục tạm NGẮN: unix socket trên
// macOS chỉ nhận đường dẫn tới khoảng 104 byte, t.TempDir() thường dài hơn.
// Thư mục "run" chưa có, để Listen phải tự tạo.
func sockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "run", "b.sock")
}

func startHub(t *testing.T) (*Hub, string) {
	t.Helper()
	path := sockPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h, err := Listen(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	return h, path
}

// logs gom các dòng Log của Client (gọi từ nhiều goroutine).
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *logs) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

type runningClient struct {
	conn   net.Conn // kết nối thật tới hub, để test cắt ngang
	cancel context.CancelFunc
	done   chan error // lỗi Serve trả về
	logs   *logs
}

// connect nối một Client chạy brain vào hub ở path, trả về khi Serve đã chạy.
func connect(t *testing.T, path, host string, brain ai.Brain) *runningClient {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc := &runningClient{conn: conn, cancel: cancel, done: make(chan error, 1), logs: &logs{}}
	c := &Client{Conn: conn, Brain: brain, Hello: Hello{Host: host, ClaudeVersion: "2.1.328", Model: "sonnet", AuthMethod: "claude.ai"}, Log: rc.logs.add}
	go func() { rc.done <- c.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-rc.done:
			_ = err // lỗi Serve đã được test kiểm ở chỗ cần; ở đây chỉ chờ nó thoát
		case <-time.After(5 * time.Second):
			t.Error("Serve không trả về sau khi huỷ ctx")
		}
	})
	return rc
}

func waitStatus(t *testing.T, h *Hub, what string, ok func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := h.Status()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("chờ %s quá lâu: %+v", what, st)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func connected(st Status) bool { return st.Connected }

// collect đọc kênh tới khi đóng; kênh không đóng trong 5 giây là lỗi.
func collect(t *testing.T, ch <-chan ai.Event) []ai.Event {
	t.Helper()
	var evs []ai.Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-timeout:
			t.Fatalf("kênh không đóng; đã nhận %d sự kiện", len(evs))
		}
	}
}

func testJob(id string) ai.Job {
	return ai.Job{ID: id, SessionID: "0123456789abcdef", BrainSession: "11111111-2222-3333-4444-555555555555",
		Prompt: "<tpan-context lang=\"vi\">\n[view] site\n</tpan-context>\n<question>site này\nsao chậm?</question>", Lang: "vi"}
}

// longScript: init rồi n đoạn chữ rồi done; với Delay đủ lớn thì job còn đang
// chạy khi test huỷ hay cắt kết nối.
func longScript(n int) func(ai.Job) []ai.Event {
	return func(ai.Job) []ai.Event {
		evs := []ai.Event{{Kind: ai.EventInit, Model: "sonnet"}}
		for i := 0; i < n; i++ {
			evs = append(evs, ai.Event{Kind: ai.EventText, Text: "x"})
		}
		return append(evs, ai.Event{Kind: ai.EventDone})
	}
}

func first(t *testing.T, ch <-chan ai.Event) ai.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("không có sự kiện đầu")
	}
	return ai.Event{}
}

type simple struct {
	Kind ai.EventKind
	Text string
	Tool string
}

func simplify(evs []ai.Event) []simple {
	out := make([]simple, len(evs))
	for i, e := range evs {
		out[i] = simple{e.Kind, e.Text, e.Tool}
	}
	return out
}

func TestJobDiMotVong(t *testing.T) {
	h, path := startHub(t)
	if st := h.Status(); st.Connected {
		t.Fatalf("chưa nối mà Status = %+v", st)
	}
	rc := connect(t, path, "may-a", &fake.Brain{})
	st := waitStatus(t, h, "bộ não nối", connected)
	if st.Hello.Host != "may-a" || st.Hello.ClaudeVersion != "2.1.328" || st.Busy || st.Since.IsZero() {
		t.Errorf("Status sau khi nối: %+v", st)
	}

	job := testJob("s-1")
	ch, err := h.Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	got := collect(t, ch)

	// Kỳ vọng: đúng chuỗi sự kiện bộ não giả phát khi chạy trực tiếp.
	wantCh, _ := (&fake.Brain{}).Run(context.Background(), job)
	want := collect(t, wantCh)
	g, w := simplify(got), simplify(want)
	if len(g) != len(w) {
		t.Fatalf("nhận %d sự kiện, muốn %d:\n%+v", len(g), len(w), g)
	}
	for i := range g {
		if g[i] != w[i] {
			t.Errorf("sự kiện %d: %+v, muốn %+v", i, g[i], w[i])
		}
		if got[i].Time.IsZero() {
			t.Errorf("sự kiện %d thiếu Time", i)
		}
	}
	if got[len(got)-1].Kind != ai.EventDone {
		t.Errorf("sự kiện cuối %s", got[len(got)-1].Kind)
	}
	waitStatus(t, h, "hết bận", func(s Status) bool { return s.Connected && !s.Busy })

	// Terminal của tpan brain thấy: đã nối, câu hỏi (một dòng), lần gọi công cụ, xong.
	waitLog := time.Now().Add(2 * time.Second)
	for !strings.Contains(rc.logs.all(), "[s-1] done") && time.Now().Before(waitLog) {
		time.Sleep(5 * time.Millisecond)
	}
	l := rc.logs.all()
	for _, want := range []string{LogWelcome, "[s-1] ? site này sao chậm?", "[s-1] → mcp__tpan__list_sites {}", "[s-1] done"} {
		if !strings.Contains(l, want) {
			t.Errorf("log thiếu %q:\n%s", want, l)
		}
	}
	if strings.Contains(l, "tpan-context") {
		t.Errorf("log in cả khối ngữ cảnh, chỉ cần câu hỏi:\n%s", l)
	}

	// Lượt thứ hai trên cùng kết nối.
	ch, err = h.Run(context.Background(), testJob("s-2"))
	if err != nil {
		t.Fatal(err)
	}
	if evs := collect(t, ch); len(evs) == 0 || evs[len(evs)-1].Kind != ai.EventDone {
		t.Errorf("lượt hai: %+v", simplify(evs))
	}
}

func TestChuaNoiVaDangBan(t *testing.T) {
	h, path := startHub(t)
	if _, err := h.Run(context.Background(), testJob("j1")); !errors.Is(err, ai.ErrBrainOffline) {
		t.Fatalf("chưa nối: %v, muốn ErrBrainOffline", err)
	}
	connect(t, path, "may-a", &fake.Brain{Script: longScript(100), Delay: 20 * time.Millisecond})
	waitStatus(t, h, "bộ não nối", connected)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := h.Run(ctx, testJob("j1"))
	if err != nil {
		t.Fatal(err)
	}
	if !h.Status().Busy {
		t.Error("đang chạy job mà Status.Busy = false")
	}
	if _, err := h.Run(context.Background(), testJob("j2")); !errors.Is(err, ai.ErrBrainBusy) {
		t.Errorf("job thứ hai: %v, muốn ErrBrainBusy", err)
	}
	cancel()
	collect(t, ch)
}

func TestBoNaoThuHaiBiTuChoi(t *testing.T) {
	h, path := startHub(t)
	connect(t, path, "may-a", &fake.Brain{})
	waitStatus(t, h, "bộ não thứ nhất nối", connected)

	rc2 := connect(t, path, "may-b", &fake.Brain{})
	var err error
	select {
	case err = <-rc2.done:
	case <-time.After(5 * time.Second):
		t.Fatal("bộ não thứ hai không bị từ chối")
	}
	rc2.done <- err // cho Cleanup đọc lại
	if err == nil || !strings.Contains(err.Error(), "may-a") || !strings.Contains(err.Error(), "từ chối") {
		t.Errorf("lỗi của bộ não thứ hai: %v (phải nêu máy may-a đang nối)", err)
	}
	if strings.Contains(rc2.logs.all(), LogWelcome) {
		t.Error("bộ não bị từ chối mà vẫn log welcome")
	}
	if st := h.Status(); !st.Connected || st.Hello.Host != "may-a" {
		t.Errorf("bộ não thứ nhất phải vẫn nối: %+v", st)
	}
	// Bộ não thứ nhất vẫn trả lời được.
	ch, err := h.Run(context.Background(), testJob("j1"))
	if err != nil {
		t.Fatal(err)
	}
	if evs := collect(t, ch); evs[len(evs)-1].Kind != ai.EventDone {
		t.Errorf("sau lần từ chối: %+v", simplify(evs))
	}
}

func TestDutKetNoiGiuaJob(t *testing.T) {
	h, path := startHub(t)
	rc := connect(t, path, "may-a", &fake.Brain{Script: longScript(200), Delay: 20 * time.Millisecond})
	waitStatus(t, h, "bộ não nối", connected)

	ch, err := h.Run(context.Background(), testJob("j1"))
	if err != nil {
		t.Fatal(err)
	}
	if ev := first(t, ch); ev.Kind != ai.EventInit {
		t.Fatalf("sự kiện đầu %s", ev.Kind)
	}
	// Cắt kết nối như ssh chết: hub thấy EOF giữa job.
	_ = rc.conn.Close()
	evs := collect(t, ch)
	last := evs[len(evs)-1]
	if last.Kind != ai.EventError || last.Text != lostText {
		t.Errorf("sự kiện cuối %+v, muốn EventError %q", last, lostText)
	}
	waitStatus(t, h, "về chưa nối", func(s Status) bool { return !s.Connected && !s.Busy })
	if _, err := h.Run(context.Background(), testJob("j2")); !errors.Is(err, ai.ErrBrainOffline) {
		t.Errorf("sau khi đứt: %v, muốn ErrBrainOffline", err)
	}
	// Client thấy mất kết nối: Serve trả lỗi (không phải nil như khi Ctrl+C),
	// và chỉ trả về sau khi job đã dừng hẳn.
	select {
	case err := <-rc.done:
		rc.done <- err
		if err == nil {
			t.Error("Serve trả nil khi panel đứt kết nối")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve không trả về khi Conn đóng")
	}

	// Nối lại được.
	connect(t, path, "may-a", &fake.Brain{})
	waitStatus(t, h, "nối lại", connected)
}

func TestHuyKhiCtxCuaRunHuy(t *testing.T) {
	h, path := startHub(t)
	connect(t, path, "may-a", &fake.Brain{Script: longScript(200), Delay: 20 * time.Millisecond})
	waitStatus(t, h, "bộ não nối", connected)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := h.Run(ctx, testJob("j1"))
	if err != nil {
		t.Fatal(err)
	}
	first(t, ch)
	cancel()
	evs := collect(t, ch)
	last := evs[len(evs)-1]
	if last.Kind != ai.EventError || !strings.HasPrefix(last.Text, "câu hỏi bị dừng") {
		t.Errorf("sự kiện cuối %+v, muốn EventError của hub báo dừng", last)
	}
	for _, e := range evs[:len(evs)-1] {
		if e.Final() {
			t.Errorf("có sự kiện kết thúc trước sự kiện cuối: %+v", e)
		}
	}
	// Kịch bản còn ~4 giây; hết bận trong 3 giây nghĩa là lệnh cancel đã tới
	// bộ não và nó dừng thật (bộ não thật: claude bị giết).
	st := waitStatus(t, h, "bộ não dừng job bị huỷ", func(s Status) bool { return !s.Busy })
	if !st.Connected {
		t.Error("huỷ một job mà mất cả kết nối")
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	ch, err = h.Run(ctx2, testJob("j2"))
	if err != nil {
		t.Fatalf("job sau khi huỷ: %v", err)
	}
	cancel2()
	collect(t, ch)
}

// deafBrain phát init rồi treo, bỏ qua ctx (như máy cá nhân ngủ giữa câu hỏi);
// chỉ dừng khi test đóng release.
type deafBrain struct{ release chan struct{} }

func (d deafBrain) Run(context.Context, ai.Job) (<-chan ai.Event, error) {
	ch := make(chan ai.Event, 1)
	ch <- ai.Event{Kind: ai.EventInit}
	go func() { <-d.release; close(ch) }()
	return ch, nil
}

func TestHuyMaBoNaoKhongTraLoi(t *testing.T) {
	old := cancelGrace
	cancelGrace = 50 * time.Millisecond
	t.Cleanup(func() { cancelGrace = old })

	h, path := startHub(t)
	d := deafBrain{release: make(chan struct{})}
	connect(t, path, "may-a", d)
	t.Cleanup(func() { close(d.release) }) // chạy trước Cleanup của connect
	waitStatus(t, h, "bộ não nối", connected)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := h.Run(ctx, testJob("j1"))
	if err != nil {
		t.Fatal(err)
	}
	first(t, ch)
	cancel()
	evs := collect(t, ch)
	if last := evs[len(evs)-1]; last.Kind != ai.EventError || !strings.Contains(last.Text, "bị dừng") {
		t.Errorf("sự kiện cuối %+v", last)
	}
	// Bộ não không báo kết thúc: quá hạn thì hub cắt, không kẹt "bận" mãi.
	waitStatus(t, h, "hub cắt bộ não im lặng", func(s Status) bool { return !s.Connected && !s.Busy })
}

// errBrain không bắt đầu được job nào.
type errBrain struct{}

func (errBrain) Run(context.Context, ai.Job) (<-chan ai.Event, error) {
	return nil, errors.New("thư mục làm việc không rỗng")
}

func TestBrainKhongBatDauDuoc(t *testing.T) {
	h, path := startHub(t)
	connect(t, path, "may-a", errBrain{})
	waitStatus(t, h, "bộ não nối", connected)
	ch, err := h.Run(context.Background(), testJob("j1"))
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	if len(evs) != 1 || evs[0].Kind != ai.EventError || !strings.Contains(evs[0].Text, "không rỗng") {
		t.Errorf("sự kiện: %+v", simplify(evs))
	}
	waitStatus(t, h, "hết bận", func(s Status) bool { return !s.Busy })
}

// pipeConn ghép hai đầu io.Pipe thành Conn của Client, như stdout và stdin
// của ssh trong tpan brain.
type pipeConn struct {
	io.Reader
	io.WriteCloser
	r io.Closer
}

func (p pipeConn) Close() error {
	_ = p.WriteCloser.Close()
	return p.r.Close()
}

func TestLinkChepHaiChieu(t *testing.T) {
	h, path := startHub(t)
	inR, inW := io.Pipe()   // Client → Link (stdin của brain-link)
	outR, outW := io.Pipe() // Link → Client (stdout của brain-link)

	linkCtx, linkCancel := context.WithCancel(context.Background())
	defer linkCancel()
	linkDone := make(chan error, 1)
	go func() { linkDone <- Link(linkCtx, path, inR, outW); _ = outW.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	c := &Client{Conn: pipeConn{Reader: outR, WriteCloser: inW, r: outR}, Brain: &fake.Brain{}, Hello: Hello{Host: "may-qua-link"}}
	go func() { serveDone <- c.Serve(ctx) }()

	waitStatus(t, h, "bộ não nối qua Link", func(s Status) bool { return s.Connected && s.Hello.Host == "may-qua-link" })
	ch, err := h.Run(context.Background(), testJob("j1"))
	if err != nil {
		t.Fatal(err)
	}
	if evs := collect(t, ch); evs[len(evs)-1].Kind != ai.EventDone {
		t.Errorf("qua Link: %+v", simplify(evs))
	}

	// Ctrl+C ở tpan brain: Serve trả nil, đóng ống; Link thấy stdin hết và thoát.
	cancel()
	if err := <-serveDone; err != nil {
		t.Errorf("Serve sau Ctrl+C: %v, muốn nil", err)
	}
	select {
	case err := <-linkDone:
		if err != nil {
			t.Errorf("Link: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Link không thoát khi stdin đóng")
	}
	waitStatus(t, h, "về chưa nối", func(s Status) bool { return !s.Connected })
}

func TestLinkKhongCoSocket(t *testing.T) {
	err := Link(context.Background(), sockPath(t), strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--brain bridge") {
		t.Errorf("không có socket: %v (phải chỉ cách bật)", err)
	}
}

func TestListenQuyenVaSocketCu(t *testing.T) {
	path := sockPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := Listen(ctx, path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&fs.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket: %v, %v", fi.Mode(), err)
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("thư mục cha %v, muốn 0700", di.Mode().Perm())
	}
	cancel()
	// Huỷ ctx thì listener đóng và file socket bị xoá (chờ, vì việc đóng chạy
	// trong goroutine của Listen).
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("huỷ ctx mà socket còn nằm đó")
		}
	}

	// Socket sót lại (tpan serve bị giết, không kịp xoá): Listen xoá rồi nghe lại.
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = stale.Close()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	if _, err := Listen(ctx2, path); err != nil {
		t.Fatalf("socket cũ còn sót: %v", err)
	}

	// File thường ở đường dẫn socket: không xoá, báo lỗi.
	path2 := filepath.Join(filepath.Dir(path), "file.sock")
	if err := os.WriteFile(path2, []byte("dữ liệu"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(context.Background(), path2); err == nil || !strings.Contains(err.Error(), "không phải socket") {
		t.Errorf("file thường: %v", err)
	}
	if b, _ := os.ReadFile(path2); string(b) != "dữ liệu" {
		t.Error("Listen xoá mất file thường")
	}
}

func TestQuestion(t *testing.T) {
	for in, want := range map[string]string{
		"<ctx>\n</ctx>\n<question>a\n  b</question>": "a b",
		"không thẻ":           "không thẻ",
		"<question>chưa đóng": "chưa đóng",
	} {
		if got := question(in); got != want {
			t.Errorf("question(%q) = %q, muốn %q", in, got, want)
		}
	}
	if got := oneLine(strings.Repeat("ạ", 300), 10); got != strings.Repeat("ạ", 10)+"…" {
		t.Errorf("oneLine cắt sai: %q", got)
	}
}
