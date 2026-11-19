package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/ai"
)

// Hub thoả ai.Brain: panel chỉ thấy bản này (đổi chữ ký ở gói ai thì lỗi ngay).
var _ ai.Brain = (*Hub)(nil)

// helloTimeout: kết nối không gửi hello trong khoảng này thì bị đóng, để một
// tiến trình treo không giữ goroutine mãi.
const helloTimeout = 10 * time.Second

// lostText là lời của EventError khi kết nối tới bộ não đứt giữa một job.
const lostText = "mất kết nối với bộ não"

// hubState là phần trong của Hub (Hub khai ở types.go).
type hubState struct {
	ln net.Listener

	mu  sync.Mutex
	cur *brainConn // bộ não đang nối; nil là chưa có
}

// brainConn là một bộ não đã được nhận (đã gửi welcome).
type brainConn struct {
	conn  net.Conn
	hello Hello
	since time.Time

	wmu sync.Mutex // job, cancel đi từ các goroutine khác nhau: mỗi lần ghi một dòng trọn
	enc *json.Encoder

	job *hubJob // job đang chạy, đọc ghi dưới hubState.mu
}

func (b *brainConn) write(m Msg) error {
	b.wmu.Lock()
	defer b.wmu.Unlock()
	return b.enc.Encode(m)
}

// Listen tạo thư mục cha (0700) nếu thiếu, xoá socket cũ còn sót, nghe unix
// socket ở path, chmod 0600, rồi nhận kết nối tới khi ctx huỷ.
//
// Gọi thẳng os và net, không qua system.System: socket là thứ tpan serve tự
// mở cho chính nó (như file DB ở internal/store), không phải cấu hình của máy.
func Listen(ctx context.Context, path string) (*Hub, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("tạo thư mục của socket %s: %w", path, err)
	}
	// Socket của lần chạy trước (tpan serve bị giết) còn nằm đó thì Listen lỗi
	// "address already in use". Chỉ xoá khi đúng là socket: không bao giờ xoá
	// nhầm một file thường ai đó đặt ở đường dẫn này.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&fs.ModeSocket == 0 {
			return nil, fmt.Errorf("%s đã có và không phải socket; không xoá, kiểm tay rồi chạy lại", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("xoá socket cũ %s: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("xem %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("nghe socket %s: %w", path, err)
	}
	// 0600: ai nối được socket này là đưa được câu trả lời giả vào panel. Thư
	// mục cha 0700 đã chặn trong khoảng ngắn trước chmod.
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("đặt quyền 0600 cho %s: %w", path, err)
	}
	h := &Hub{st: hubState{ln: ln}}
	go h.accept()
	go func() {
		<-ctx.Done()
		// Đóng listener (net tự xoá file socket) rồi đóng bộ não đang nối:
		// goroutine đọc của nó thoát và phát lỗi cho job đang dở.
		_ = ln.Close()
		h.st.mu.Lock()
		cur := h.st.cur
		h.st.mu.Unlock()
		if cur != nil {
			_ = cur.conn.Close()
		}
	}()
	return h, nil
}

func (h *Hub) accept() {
	for {
		c, err := h.st.ln.Accept()
		if err != nil {
			return // listener đã đóng
		}
		go h.handle(c)
	}
}

// handle nhận hello, giữ chỗ (hoặc từ chối), rồi đọc sự kiện tới khi kết nối đứt.
func (h *Hub) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	dec := json.NewDecoder(c)
	_ = c.SetReadDeadline(time.Now().Add(helloTimeout))
	var m Msg
	if err := dec.Decode(&m); err != nil || m.Type != MsgHello || m.Hello == nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})

	b := &brainConn{conn: c, hello: *m.Hello, since: time.Now(), enc: json.NewEncoder(c)}
	// Kiểm và giữ chỗ trong cùng một lần khoá: hai hello tới cùng lúc thì chỉ
	// một cái được nhận.
	h.st.mu.Lock()
	other := h.st.cur
	if other == nil {
		h.st.cur = b
	}
	h.st.mu.Unlock()
	if other != nil {
		_ = b.write(Msg{Type: MsgRefused, Text: fmt.Sprintf(
			"panel đã có bộ não của máy %s nối từ %s; mỗi lúc chỉ một bộ não. Dừng tpan brain ở máy đó rồi nối lại.",
			other.hello.Host, other.since.Format("2006-01-02 15:04:05"))})
		return
	}
	if err := b.write(Msg{Type: MsgWelcome}); err == nil {
		h.read(b, dec)
	}
	h.drop(b)
}

// read chuyển sự kiện của bộ não tới job đang chạy, tới khi kết nối đứt.
func (h *Hub) read(b *brainConn, dec *json.Decoder) {
	for {
		var m Msg
		if err := dec.Decode(&m); err != nil {
			return
		}
		if m.Type != MsgEvent || m.Event == nil {
			continue
		}
		ev := *m.Event
		if ev.Time.IsZero() {
			ev.Time = time.Now()
		}
		h.st.mu.Lock()
		j := b.job
		if j == nil || j.id != m.JobID {
			// Sự kiện muộn của job đã xong hoặc id lạ: bỏ.
			h.st.mu.Unlock()
			continue
		}
		final := ev.Final()
		if final {
			// Nhả chỗ ngay khi bộ não báo xong: chỉ lúc này nó mới thật sự rảnh
			// (kể cả sau khi panel đã huỷ và thôi đọc).
			b.job = nil
		}
		h.st.mu.Unlock()
		if final {
			j.finish(ev)
		} else {
			j.send(ev)
		}
	}
}

// drop gỡ b khỏi hub; job đang dở nhận EventError "mất kết nối với bộ não".
func (h *Hub) drop(b *brainConn) {
	h.st.mu.Lock()
	j := b.job
	b.job = nil
	if h.st.cur == b {
		h.st.cur = nil
	}
	h.st.mu.Unlock()
	if j != nil {
		j.finish(ai.Event{Kind: ai.EventError, Time: time.Now(), Text: lostText})
	}
}

// Run gửi job cho bộ não đang nối; xem hợp đồng ở types.go.
func (h *Hub) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error) {
	h.st.mu.Lock()
	b := h.st.cur
	if b == nil {
		h.st.mu.Unlock()
		return nil, ai.ErrBrainOffline
	}
	if b.job != nil {
		h.st.mu.Unlock()
		return nil, ai.ErrBrainBusy
	}
	j := &hubJob{id: job.ID, ctx: ctx, out: make(chan ai.Event), done: make(chan struct{})}
	b.job = j
	h.st.mu.Unlock()

	if err := b.write(Msg{Type: MsgJob, Job: &job}); err != nil {
		// Ghi hỏng nghĩa là kết nối đã hỏng: đóng để goroutine đọc thoát, nhả
		// chỗ và phát lỗi cho job này (finish chạy một lần, ai tới trước thì phát).
		_ = b.conn.Close()
		go j.finish(ai.Event{Kind: ai.EventError, Time: time.Now(), Text: fmt.Sprintf("gửi câu hỏi cho bộ não của %s: %v", b.hello.Host, err)})
		return j.out, nil
	}
	grace := cancelGrace
	go func() {
		select {
		case <-ctx.Done():
			// Phát lỗi cho panel trước: ghi cancel có thể kẹt khi ssh treo,
			// panel không phải chờ theo.
			j.finish(ai.Event{Kind: ai.EventError, Time: time.Now(), Text: "câu hỏi bị dừng: " + ctx.Err().Error()})
			// Báo bộ não giết claude. Chỗ vẫn bận tới khi bộ não gửi sự kiện
			// kết thúc hoặc kết nối đứt, để job sau không chạy chồng lên claude
			// còn đang tắt.
			_ = b.write(Msg{Type: MsgCancel, JobID: j.id})
			// Máy cá nhân ngủ giữa chừng thì không có sự kiện kết thúc mà
			// socket cũng không thấy EOF (sshd lâu mới phát hiện client chết):
			// quá hạn thì cắt kết nối để panel không kẹt "bận" mãi.
			time.AfterFunc(grace, func() {
				h.st.mu.Lock()
				stuck := b.job == j
				h.st.mu.Unlock()
				if stuck {
					_ = b.conn.Close()
				}
			})
		case <-j.done:
		}
	}()
	return j.out, nil
}

// cancelGrace: sau khi panel huỷ một job, bộ não có chừng này thời gian để báo
// kết thúc (claude bị giết mất vài giây); quá thì hub coi kết nối đã chết.
// Biến (không phải hằng) để test rút ngắn.
var cancelGrace = 30 * time.Second

// Status cho biết bộ não nào đang nối.
func (h *Hub) Status() Status {
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	b := h.st.cur
	if b == nil {
		return Status{}
	}
	return Status{Connected: true, Hello: b.hello, Since: b.since, Busy: b.job != nil}
}

// hubJob là một job đang chờ sự kiện. Mọi lần gửi vào out đi qua mu và cờ
// closed: sự kiện của bộ não, lần huỷ của panel và lần đứt kết nối tới từ ba
// goroutine khác nhau, gửi vào kênh đã đóng là panic.
type hubJob struct {
	id   string
	ctx  context.Context
	out  chan ai.Event
	done chan struct{}

	mu     sync.Mutex
	closed bool
}

// send gửi một sự kiện giữa chừng; panel đã huỷ thì bỏ (người nhận có thể đã thôi đọc).
func (j *hubJob) send(ev ai.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	select {
	case j.out <- ev:
	case <-j.ctx.Done():
	}
}

// finish gửi sự kiện kết thúc (chờ người nhận đọc: hợp đồng của ai.Brain bắt
// người nhận đọc tới khi kênh đóng) rồi đóng kênh. Chỉ lần gọi đầu có tác dụng.
func (j *hubJob) finish(ev ai.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	j.closed = true
	j.out <- ev
	close(j.out)
	close(j.done)
}
