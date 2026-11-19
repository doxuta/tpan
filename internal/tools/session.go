package tools

// Phiên của agent và Call, chỗ duy nhất dữ liệu đi ra cho agent (mục 8.1, 8.3).
//
// Mỗi lượt `claude -p` (và mỗi lần agent nối lại) là một tiến trình `tpan
// mcp` mới. Vì vậy mọi thứ cần nhớ giữa các lần gọi (bảng bí danh, cờ
// untrusted) được ghi xuống ai_sessions sau MỖI lần gọi, không đợi tiến trình
// thoát: tiến trình có thể bị giết bất cứ lúc nào.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

// sessionState là phần ruột của Session.
type sessionState struct {
	// mu giữ suốt một lần Call: agent (qua MCP) có thể gửi nhiều lần gọi cùng
	// lúc, mà số thứ tự lần gọi và bản ghi phiên phải đi đúng thứ tự.
	mu  sync.Mutex
	d   Deps
	red *ai.Redactor
	// approval là chế độ duyệt hiệu lực của phiên: phiên nối từ panel luôn là
	// panel, bất kể setting.
	approval core.ApprovalMode
	origin   engine.Origin
	rec      store.AISession
	seq      int
}

func (st *sessionState) now() time.Time {
	if st.d.Now != nil {
		return st.d.Now().UTC()
	}
	return time.Now().UTC()
}

// newSessionID sinh 16 ký tự hex ngẫu nhiên, cùng dạng id Plan và job.
func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh id phiên agent: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// OpenSession mở phiên. Xem hợp đồng ở types.go.
func OpenSession(ctx context.Context, d Deps, o SessionOptions) (*Session, error) {
	if d.Sys == nil || d.Store == nil {
		return nil, errors.New("mở phiên agent: thiếu Sys hoặc Store")
	}
	cfg, err := core.AgentConfig(ctx, d.Store)
	if err != nil {
		return nil, fmt.Errorf("mở phiên agent: %w", err)
	}
	s := &Session{}
	st := &s.st
	st.d = d
	if o.ID == "" {
		id, err := newSessionID()
		if err != nil {
			return nil, err
		}
		now := st.now()
		st.rec = store.AISession{ID: id, Entry: store.EntryMCP, Created: now, Last: now}
		st.approval, st.origin = cfg.Approval, engine.OriginMCP
	} else {
		rec, err := d.Store.AISession(ctx, o.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("không có phiên AI %q; --session chỉ nối vào phiên do panel mở", o.ID)
		}
		if err != nil {
			return nil, fmt.Errorf("mở phiên AI %s: %w", o.ID, err)
		}
		// Phiên mcp là của một agent nối trực tiếp khác. Cho nối vào thì agent
		// này dùng lại được bảng bí danh và Plan của phiên kia, kể cả chạy Plan
		// ở chế độ client: chỉ phiên panel mới được nối lại.
		if rec.Entry != store.EntryPanel {
			return nil, fmt.Errorf("phiên AI %s là phiên %s, không phải phiên panel; --session chỉ nối vào phiên do panel mở", o.ID, rec.Entry)
		}
		calls, err := d.Store.AICalls(ctx, rec.ID)
		if err != nil {
			return nil, fmt.Errorf("mở phiên AI %s: %w", rec.ID, err)
		}
		for _, c := range calls {
			st.seq = max(st.seq, c.Seq)
		}
		st.rec = rec
		st.approval, st.origin = core.ApprovalPanel, engine.OriginAI
	}

	st.red = ai.NewRedactor(ai.Level(cfg.Redact), st.rec.Aliases)
	sites, err := d.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("mở phiên agent: đọc danh sách site: %w", err)
	}
	domains := make([]validate.Domain, len(sites))
	for i, x := range sites {
		domains[i] = x.Domain
	}
	st.red.Learn(domains, d.Hostname)
	st.rec.Aliases = st.red.Aliases()

	if o.ID == "" {
		err = d.Store.CreateAISession(ctx, st.rec)
	} else {
		st.rec.Last = st.now()
		err = d.Store.SaveAISessionState(ctx, st.rec)
	}
	if err != nil {
		return nil, fmt.Errorf("mở phiên agent: %w", err)
	}
	return s, nil
}

// ID là id của phiên trong ai_sessions.
func (s *Session) ID() string { return s.st.rec.ID }

// SetClient ghi tên client MCP tự khai. Chỉ để xem lại, không dùng để quyết
// định gì: client khai gì cũng được.
func (s *Session) SetClient(ctx context.Context, client string) error {
	st := &s.st
	st.mu.Lock()
	defer st.mu.Unlock()
	st.rec.Client = client
	st.rec.Last = st.now()
	if err := st.d.Store.SaveAISessionState(ctx, st.rec); err != nil {
		return fmt.Errorf("ghi client của phiên %s: %w", st.rec.ID, err)
	}
	return nil
}

// Call gọi một công cụ. Xem hợp đồng ở types.go.
func (s *Session) Call(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	st := &s.st
	st.mu.Lock()
	defer st.mu.Unlock()

	c := &call{s: s}
	out, err := s.run(ctx, c, name, args)
	var agentErr *Error
	if err != nil {
		out, agentErr = nil, st.agentError(err)
	}
	// Ghi bằng ctx không bị huỷ: agent ngắt giữa chừng (apply_plan đã chạy)
	// thì nhật ký vẫn phải có dòng của lần gọi này.
	if rerr := st.record(context.WithoutCancel(ctx), c, name, args, out, agentErr); rerr != nil {
		return nil, &Error{Code: "internal", Message: st.red.String("could not record the tool call: " + rerr.Error())}
	}
	if agentErr != nil {
		return nil, agentErr
	}
	return out, nil
}

// run: tên phải có trong danh mục; đổi bí danh; chạy; che kết quả.
func (s *Session) run(ctx context.Context, c *call, name string, args json.RawMessage) (json.RawMessage, error) {
	t, ok := lookup(name)
	if !ok {
		return nil, &Error{Code: "tool_unknown", Message: fmt.Sprintf("there is no tool %q; only the tools in the list can be called", name)}
	}
	realArgs, err := unaliasArgs(s.st.red, args)
	if err != nil {
		return nil, err
	}
	res, err := t.run(ctx, c, realArgs)
	if err != nil {
		return nil, err
	}
	if t.Untrusted {
		c.untrusted = true
	}
	return s.st.red.JSON(res)
}

// unaliasArgs đổi bí danh về tên thật trên TỪNG giá trị chuỗi của đối số, chứ
// không trên cả khối JSON: thay trên văn bản thô có thể chạm vào tên khoá và
// cú pháp JSON. Số giữ nguyên dạng (UseNumber).
func unaliasArgs(r *ai.Redactor, args json.RawMessage) (json.RawMessage, error) {
	args = bytes.TrimSpace(args)
	if len(args) == 0 || string(args) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, badArgs(err.Error())
	}
	out, err := json.Marshal(unaliasValue(r, v))
	if err != nil {
		return nil, fmt.Errorf("mã hoá lại đối số: %w", err)
	}
	// Phần thừa sau đối tượng (nếu có) để decodeStrict bắt: nối lại nguyên văn.
	if rest := bytes.TrimSpace(args[dec.InputOffset():]); len(rest) > 0 {
		out = append(append(out, ' '), rest...)
	}
	return out, nil
}

func unaliasValue(r *ai.Redactor, v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = unaliasValue(r, x)
		}
	case []any:
		for i := range t {
			t[i] = unaliasValue(r, t[i])
		}
	case string:
		return r.Unalias(t)
	}
	return v
}

// agentError đổi err thành lỗi cho agent: mã ổn định, lời tiếng Anh (AI đọc
// tiếng Anh chắc hơn), đã che.
func (st *sessionState) agentError(err error) *Error {
	var (
		te  *Error
		re  *core.RefusedError
		ve  *validate.Error
		out Error
	)
	switch {
	case errors.As(err, &te):
		out = *te
	case errors.As(err, &re):
		out = Error{Code: re.Code, Message: english(re.Message)}
	case errors.As(err, &ve):
		out = Error{Code: ve.Code, Message: english(ve.Message)}
	default:
		out = Error{Code: "internal", Message: err.Error()}
	}
	out.Message = st.red.String(out.Message)
	return &out
}

func english(t i18n.Text) string {
	if t.En != "" {
		return t.En
	}
	return t.Vi
}

// record ghi một dòng ai_calls và trạng thái phiên. Input lưu bản đã che của
// đúng thứ agent gửi (thường đã là bí danh, che lại không đổi gì; agent gõ
// thẳng tên thật thì tên đó không nằm trong DB). Output là đúng thứ agent nhận.
func (st *sessionState) record(ctx context.Context, c *call, name string, args json.RawMessage, out json.RawMessage, e *Error) error {
	if c.untrusted {
		st.rec.Untrusted = true
	}
	st.seq++
	now := st.now()
	in, err := st.redactInput(args)
	if err != nil {
		return err
	}
	ac := store.AICall{
		SessionID: st.rec.ID, Seq: st.seq, Time: now, Tool: st.red.String(name),
		Input: in, Output: out, PlanID: c.planID,
	}
	if e != nil {
		ac.Error = e.Error()
	}
	if err := st.d.Store.AddAICall(ctx, ac); err != nil {
		return err
	}
	st.rec.Aliases = st.red.Aliases()
	st.rec.Last = now
	return st.d.Store.SaveAISessionState(ctx, st.rec)
}

// redactInput che đối số agent gửi. Không phải JSON hợp lệ thì lưu thành một
// chuỗi JSON (cột input phải là JSON).
func (st *sessionState) redactInput(args json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		return nil, nil
	}
	var v any = string(args)
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	var parsed any
	if dec.Decode(&parsed) == nil {
		v = parsed
	}
	return st.red.JSON(v)
}
