package claudecode

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/system"
)

// SystemPrompt là quy tắc của TPAN cho AI, đưa cho claude bằng
// --append-system-prompt-file. Viết tiếng Anh vì model theo chỉ dẫn tiếng Anh
// ổn định nhất; ngôn ngữ trả lời do thuộc tính lang trong câu hỏi quyết định.
//
//go:embed system-prompt.md
var SystemPrompt string

// stderrTail: số dòng cuối của stderr đưa vào lỗi, đủ thấy lý do mà không kéo cả log.
const stderrTail = 5

var (
	// sessionRe: id phiên tpan là 16 chữ hex (tools.newSessionID). Nó vừa là tên
	// thư mục dưới WorkRoot (chặn "../"), vừa nằm trong args của lệnh MCP mà ssh
	// ghép thành một câu cho shell ở máy chủ, nên chỉ nhận đúng dạng này.
	sessionRe = regexp.MustCompile(`^[0-9a-f]{16}$`)
	// uuidRe: Claude Code đòi UUID cho --session-id; giá trị bắt đầu bằng "-" sẽ bị
	// hiểu là cờ, nên kiểm dạng chặt.
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// New kiểm cfg, tạo WorkRoot (0700) và ghi file system prompt vào đó.
func New(cfg Config) (*Brain, error) {
	var missing []string
	if cfg.Sys == nil {
		missing = append(missing, "Sys")
	}
	if cfg.Claude == "" {
		missing = append(missing, "Claude")
	}
	if cfg.MCP.Command == "" {
		missing = append(missing, "MCP.Command")
	}
	if cfg.WorkRoot == "" {
		missing = append(missing, "WorkRoot")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("cấu hình bộ não thiếu %s", strings.Join(missing, ", "))
	}
	if !filepath.IsAbs(cfg.WorkRoot) {
		return nil, fmt.Errorf("WorkRoot %q phải là đường dẫn tuyệt đối", cfg.WorkRoot)
	}
	if cfg.Model == "" {
		cfg.Model = ai.DefaultModel
	}
	owner, err := currentOwner()
	if err != nil {
		return nil, err
	}
	// 0700: thư mục chứa phiên hỏi đáp (đã che nhưng vẫn là chuyện của máy chủ).
	if err := cfg.Sys.MkdirAll(cfg.WorkRoot, 0o700, owner); err != nil {
		return nil, fmt.Errorf("tạo thư mục bộ não: %w", err)
	}
	promptFile := filepath.Join(cfg.WorkRoot, "system-prompt.md")
	// Ghi lại mỗi lần khởi động để file luôn khớp bản tpan đang chạy.
	if err := cfg.Sys.WriteFileAtomic(promptFile, []byte(SystemPrompt), 0o600, owner); err != nil {
		return nil, fmt.Errorf("ghi system prompt: %w", err)
	}
	return &Brain{cfg: cfg, promptFile: promptFile}, nil
}

// currentOwner là user đang chạy tpan brain. Owner rỗng nghĩa là root, mà trên
// máy cá nhân tpan chạy bằng user thường nên không chown về root được.
func currentOwner() (system.Owner, error) {
	u, err := user.Current()
	if err != nil {
		return system.Owner{}, fmt.Errorf("tìm user hiện tại: %w", err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return system.Owner{}, fmt.Errorf("tìm nhóm %s của %s: %w", u.Gid, u.Username, err)
	}
	return system.Owner{User: u.Username, Group: g.Name}, nil
}

// Args là argv của claude cho job, đúng bảng cờ ở mục 8.2.
func Args(cfg Config, job ai.Job, promptFile string) []string {
	model := cfg.Model
	if model == "" {
		model = ai.DefaultModel
	}
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 || maxTurns > ai.MaxTurns {
		maxTurns = ai.MaxTurns
	}
	// Clone: append thẳng vào cfg.MCP.Args có thể ghi đè mảng dùng chung giữa các lượt.
	mcpArgs := append(slices.Clone(cfg.MCP.Args), "--session", job.SessionID)
	conf, _ := json.Marshal(struct { // chỉ có chuỗi, Marshal không lỗi được
		MCPServers map[string]MCPServer `json:"mcpServers"`
	}{map[string]MCPServer{"tpan": {Command: cfg.MCP.Command, Args: mcpArgs}}})

	args := []string{
		"-p",
		"--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--tools", "",
		"--strict-mcp-config", "--mcp-config", string(conf),
		"--allowedTools", "mcp__tpan",
		"--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--setting-sources", "", "--disable-slash-commands",
		"--append-system-prompt-file", promptFile,
		"--max-turns", strconv.Itoa(maxTurns),
		"--model", model,
	}
	// Không bao giờ --bare: ở chế độ đó claude bỏ đăng nhập gói Claude, đòi khoá API.
	if job.Resume {
		return append(args, "--resume", job.BrainSession)
	}
	return append(args, "--session-id", job.BrainSession)
}

// Run chạy một lượt; xem hợp đồng ở types.go.
func (b *Brain) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error) {
	if !sessionRe.MatchString(job.SessionID) {
		return nil, fmt.Errorf("id phiên %q không hợp lệ (cần 16 chữ hex)", job.SessionID)
	}
	if !uuidRe.MatchString(job.BrainSession) {
		return nil, fmt.Errorf("phiên %s: brain_session %q không phải UUID", job.SessionID, job.BrainSession)
	}
	if job.Prompt == "" {
		return nil, fmt.Errorf("phiên %s: câu hỏi rỗng", job.SessionID)
	}
	owner, err := currentOwner()
	if err != nil {
		return nil, err
	}
	// Cùng một thư mục cho mọi lượt của phiên: Claude Code lưu phiên theo thư
	// mục làm việc, --resume ở thư mục khác không tìm thấy phiên.
	dir := filepath.Join(b.cfg.WorkRoot, job.SessionID)
	if err := b.cfg.Sys.MkdirAll(dir, 0o700, owner); err != nil {
		return nil, fmt.Errorf("tạo thư mục làm việc của phiên %s: %w", job.SessionID, err)
	}
	// Thư mục phải rỗng: claude đọc CLAUDE.md và file khác ở thư mục làm việc,
	// thứ đó sẽ thành chỉ dẫn cho AI. Không tự xoá: có thể là dữ liệu của ai đó.
	names, err := b.cfg.Sys.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("đọc thư mục làm việc %s: %w", dir, err)
	}
	if len(names) > 0 {
		return nil, fmt.Errorf("thư mục làm việc %s không rỗng (%s); xoá các file đó rồi hỏi lại", dir, strings.Join(names, ", "))
	}

	runCtx, cancel := context.WithTimeout(ctx, ai.TurnTimeout)
	t := &turn{ctx: runCtx, cancel: cancel, ch: make(chan ai.Event)}
	cmd := system.Cmd{
		Name:     b.cfg.Claude,
		Args:     Args(b.cfg, job, b.promptFile),
		Stdin:    []byte(job.Prompt),
		Dir:      dir,
		Env:      b.cfg.Env,
		OnOutput: t.onLine,
	}
	go func() {
		res, err := b.cfg.Sys.Run(runCtx, cmd)
		t.finish(ctx, res, err)
	}()
	return t.ch, nil
}

// turn là trạng thái của một lượt đang chạy. Mọi lần gửi vào ch đi qua mu và
// cờ closed: LocalSystem.Run có WaitDelay, nên OnOutput còn có thể bị gọi sau
// khi Run đã trả về và kênh đã đóng; gửi vào kênh đã đóng là panic.
type turn struct {
	ctx    context.Context
	cancel context.CancelFunc
	ch     chan ai.Event

	mu     sync.Mutex
	gated  bool // init đã qua Gate
	final  bool // đã gửi sự kiện kết thúc; mọi dòng sau đó bỏ qua
	closed bool
	p      Parser
}

// onLine nhận từng dòng stdout và stderr của claude.
func (t *turn) onLine(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.final || t.closed {
		return
	}
	line := bytes.TrimSpace([]byte(s))
	// Dòng không phải JSON (cảnh báo trên stderr...) thì bỏ.
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var head struct{ Type, Subtype string }
	if json.Unmarshal(line, &head) != nil {
		return
	}
	if head.Type == "system" && head.Subtype == "init" {
		// Mọi init (kể cả nếu có lần thứ hai giữa luồng) đều qua cửa.
		var in Init
		if err := json.Unmarshal(line, &in); err != nil {
			t.refuse("không đọc được sự kiện init của claude: " + err.Error())
			return
		}
		if err := Gate(in); err != nil {
			t.refuse(err.Error())
			return
		}
		if !t.gated {
			t.gated = true
			t.send(ai.Event{Kind: ai.EventInit, Version: in.Version, Model: in.Model})
		}
		return
	}
	if !t.gated {
		// Chưa có init thì chưa biết phiên nạp những gì: không tin dòng nào.
		t.refuse(fmt.Sprintf("dòng JSON đầu tiên của claude không phải sự kiện init (type %q, subtype %q)", head.Type, head.Subtype))
		return
	}
	for _, e := range t.p.Line(line) {
		if e.Final() {
			t.final = true
			t.sendFinal(e)
			return
		}
		t.send(e)
	}
}

// refuse dừng claude và gửi EventRefused. Gọi khi đang giữ mu.
func (t *turn) refuse(msg string) {
	t.final = true
	t.cancel()
	t.sendFinal(ai.Event{Kind: ai.EventRefused, Text: msg})
}

// send gửi một sự kiện giữa chừng; lượt đã bị huỷ thì bỏ (người nhận có thể đã thôi đọc).
func (t *turn) send(e ai.Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	select {
	case t.ch <- e:
	case <-t.ctx.Done():
	}
}

// sendFinal gửi sự kiện kết thúc, chờ tới khi người nhận đọc: hợp đồng của
// ai.Brain bắt người nhận đọc tới khi kênh đóng.
func (t *turn) sendFinal(e ai.Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	t.ch <- e
}

// finish chạy khi claude đã thoát: chưa có sự kiện kết thúc thì gửi EventError
// nêu lý do, rồi đóng kênh.
func (t *turn) finish(parent context.Context, res system.Result, err error) {
	t.cancel()
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.final {
		t.final = true
		t.sendFinal(ai.Event{Kind: ai.EventError, Text: failText(parent, t.ctx, res, err)})
	}
	t.closed = true
	close(t.ch)
}

// failText giải thích vì sao lượt không có kết quả. Không in dòng lệnh: nó có
// lệnh ssh tới máy chủ, và lỗi này hiện trên panel.
func failText(parent, run context.Context, res system.Result, err error) string {
	var msg string
	var ee *system.ExitError
	switch {
	case parent.Err() != nil:
		msg = "lượt hỏi đã bị huỷ, đã dừng claude"
	case errors.Is(run.Err(), context.DeadlineExceeded):
		msg = fmt.Sprintf("claude chạy quá %s, đã dừng", ai.TurnTimeout)
	case errors.As(err, &ee):
		msg = fmt.Sprintf("claude thoát với mã %d mà không gửi kết quả", ee.Code)
	case err != nil:
		// LocalSystem bọc lỗi bằng "chạy <dòng lệnh>: ..."; chỉ giữ phần lỗi bên trong.
		if inner := errors.Unwrap(err); inner != nil {
			err = inner
		}
		msg = "không chạy được claude: " + err.Error()
	default:
		msg = "claude thoát mà không gửi kết quả"
	}
	if tail := lastLines(res.Stderr, stderrTail); tail != "" {
		msg += "\n" + tail
	}
	return msg
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Version chạy `claude --version` (in dạng "2.1.328 (Claude Code)").
func Version(ctx context.Context, cfg Config) (string, error) {
	res, err := cfg.Sys.Run(ctx, system.Cmd{Name: cfg.Claude, Args: []string{"--version"}, Env: cfg.Env})
	if err != nil {
		return "", fmt.Errorf("xem phiên bản claude: %w", err)
	}
	f := strings.Fields(res.Stdout)
	if len(f) == 0 {
		return "", errors.New("claude --version không in gì")
	}
	return f[0], nil
}

// Auth chạy `claude auth status --json` và chỉ đọc loggedIn, authMethod.
// Output còn có email, tổ chức...: không đưa vào lỗi, không log.
func Auth(ctx context.Context, cfg Config) (AuthStatus, error) {
	res, err := cfg.Sys.Run(ctx, system.Cmd{Name: cfg.Claude, Args: []string{"auth", "status", "--json"}, Env: cfg.Env})
	var st AuthStatus
	// Chưa đăng nhập thì claude có thể thoát khác 0 mà vẫn in JSON: đọc trước, xét lỗi sau.
	if jerr := json.Unmarshal([]byte(res.Stdout), &st); jerr == nil {
		return st, nil
	}
	var ee *system.ExitError
	if errors.As(err, &ee) {
		return AuthStatus{}, fmt.Errorf("claude auth status: mã thoát %d, output không phải JSON", ee.Code)
	}
	if err != nil {
		return AuthStatus{}, fmt.Errorf("claude auth status: %w", err)
	}
	return AuthStatus{}, errors.New("claude auth status: output không phải JSON")
}
