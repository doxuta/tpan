package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/doxuta/tpan/internal/demo"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/tools"
)

// connect dựng phiên tools trên máy demo (có sẵn site thật trong DB), nối
// server với client của SDK qua transport trong bộ nhớ. protocol rỗng là bản
// mới nhất (client thử server/discover trước); đặt bản cũ thì client đi
// đường initialize cũ.
func connect(t *testing.T, protocol string) (*sdk.ClientSession, *tools.Session, *store.Store) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sess, st := openSession(t)

	srvT, cliT := sdk.NewInMemoryTransports()
	ss, err := newServer(sess, io.Discard).Connect(ctx, srvT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	c := sdk.NewClient(&sdk.Implementation{Name: "test-agent", Version: "1.2.3"}, nil)
	cs, err := c.Connect(ctx, cliT, &sdk.ClientSessionOptions{ProtocolVersion: protocol})
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, sess, st
}

// openSession mở phiên tools trên máy demo (có sẵn site thật trong DB).
func openSession(t *testing.T) (*tools.Session, *store.Store) {
	t.Helper()
	ctx := context.Background()
	sys, st, err := demo.New(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	wpHash, wpcliHash := demo.Hashes()
	sess, err := tools.OpenSession(ctx, tools.Deps{Sys: sys, Store: st, Hostname: "demo", WPHash: wpHash, WPCLISHA512: wpcliHash}, tools.SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	return sess, st
}

// Agent ngắt kết nối (SSH đóng stdin) là cách dừng bình thường: serveOn trả
// nil để `tpan mcp` thoát mã 0. Đi qua IOTransport, cùng đường đọc dòng với
// StdioTransport, và kiểm luôn stdout chỉ có JSON-RPC.
func TestServeStopsCleanlyOnDisconnect(t *testing.T) {
	sess, _ := openSession(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveOn(context.Background(), sess, &sdk.IOTransport{Reader: inR, Writer: outW}, io.Discard)
	}()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"pipe","version":"1"}}}` + "\n"
	if _, err := io.WriteString(inW, req); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(outR).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil || resp.JSONRPC != "2.0" || resp.ID != 1 || resp.Result == nil {
		t.Fatalf("dòng đầu ở stdout không phải phản hồi JSON-RPC của initialize: %q (%v)", line, err)
	}

	_ = inW.Close()
	go func() { _, _ = io.Copy(io.Discard, outR) }() // đừng để server kẹt khi ghi
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("ngắt kết nối mà serveOn trả lỗi: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("đóng stdin mà server không dừng")
	}
}

func asJSONValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func text(t *testing.T, r *sdk.CallToolResult) string {
	t.Helper()
	if len(r.Content) != 1 {
		t.Fatalf("muốn đúng một khối nội dung, có %d", len(r.Content))
	}
	tc, ok := r.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("nội dung là %T, muốn *TextContent", r.Content[0])
	}
	return tc.Text
}

func TestListToolsMatchesCatalog(t *testing.T) {
	cs, _, _ := connect(t, "")
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cat := tools.Catalog()
	if len(res.Tools) != len(cat) {
		t.Fatalf("server có %d công cụ, danh mục có %d", len(res.Tools), len(cat))
	}
	got := map[string]*sdk.Tool{}
	for _, tl := range res.Tools {
		got[tl.Name] = tl
	}
	for _, want := range cat {
		tl, ok := got[want.Name]
		if !ok {
			t.Errorf("thiếu công cụ %s", want.Name)
			continue
		}
		if tl.Description != want.Description {
			t.Errorf("%s: mô tả %q, muốn %q", want.Name, tl.Description, want.Description)
		}
		// Server gửi RawMessage, client nhận map: so ở dạng giá trị JSON.
		var ws any
		if err := json.Unmarshal(want.InputSchema, &ws); err != nil {
			t.Fatalf("%s: schema của danh mục không phải JSON: %v", want.Name, err)
		}
		if gs := asJSONValue(t, tl.InputSchema); !reflect.DeepEqual(gs, ws) {
			t.Errorf("%s: schema\n%v\nmuốn\n%v", want.Name, gs, ws)
		}
		ro := want.Kind == tools.KindRead || want.Kind == tools.KindPlan
		if tl.Annotations == nil || tl.Annotations.ReadOnlyHint != ro {
			t.Errorf("%s (%s): Annotations = %+v, muốn ReadOnlyHint %v", want.Name, want.Kind, tl.Annotations, ro)
		}
	}
	if a := got["apply_plan"]; a == nil || a.Annotations.ReadOnlyHint {
		t.Error("apply_plan phải có và không được đánh dấu chỉ đọc")
	}
}

func TestCallListSitesIsRedacted(t *testing.T) {
	cs, sess, st := connect(t, "")
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_sites"})
	if err != nil {
		t.Fatalf("list_sites: lỗi giao thức %v", err)
	}
	out := text(t, res)
	if res.IsError {
		t.Fatalf("list_sites IsError: %s", out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("kết quả không phải JSON: %s", out)
	}
	// Máy demo có các site này; mức che mặc định (all) không để lộ tên thật.
	for _, real := range []string{"blog.example.com", "shop.example.net", "wiki.example.org"} {
		if strings.Contains(out, real) {
			t.Errorf("kết quả lộ domain thật %s:\n%s", real, out)
		}
	}
	calls, err := st.AICalls(ctx, sess.ID())
	if err != nil || len(calls) != 1 || calls[0].Tool != "list_sites" {
		t.Errorf("ai_calls = %+v (%v)", calls, err)
	}
}

func TestToolErrorIsResultNotProtocolError(t *testing.T) {
	cs, _, _ := connect(t, "")
	ctx := context.Background()
	for _, p := range []*sdk.CallToolParams{
		{Name: "get_plan", Arguments: map[string]any{"id": "khongco"}},
		{Name: "list_sites", Arguments: map[string]any{"la": "truong la"}}, // trường lạ: giải JSON chặt
		{Name: "site_health", Arguments: map[string]any{"domain": "a.com; rm -rf /"}},
	} {
		res, err := cs.CallTool(ctx, p)
		if err != nil {
			t.Errorf("%s %v: lỗi giao thức %v, muốn kết quả IsError", p.Name, p.Arguments, err)
			continue
		}
		if out := text(t, res); !res.IsError || out == "" || strings.Contains(out, internalErrorText) {
			t.Errorf("%s %v: IsError=%v, %q", p.Name, p.Arguments, res.IsError, out)
		}
	}
	// Công cụ không có trong danh mục: đó mới là lỗi giao thức.
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "shell"}); err == nil {
		t.Error("gọi công cụ không có mà không lỗi")
	}
}

func TestClientInfoRecorded(t *testing.T) {
	// "" đi đường server/discover (bản 2026-07-28), "2025-06-18" đi đường
	// initialize cũ: kiểm bản đã thoả thuận để chắc cả hai đường đều được thử.
	for protocol, negotiated := range map[string]string{"": "2026-07-28", "2025-06-18": "2025-06-18"} {
		cs, sess, st := connect(t, protocol)
		if v := cs.InitializeResult().ProtocolVersion; v != negotiated {
			t.Errorf("giao thức %q: thoả thuận được %q, muốn %q", protocol, v, negotiated)
		}
		a, err := st.AISession(context.Background(), sess.ID())
		if err != nil {
			t.Fatal(err)
		}
		if a.Client != "test-agent 1.2.3" {
			t.Errorf("giao thức %q: ai_sessions.client = %q", protocol, a.Client)
		}
	}
}

func TestClientName(t *testing.T) {
	// Bảng là slice, không phải map: sdk.Implementation có trường slice (Icons)
	// nên không làm khoá map được.
	for _, c := range []struct {
		in   sdk.Implementation
		want string
	}{
		{sdk.Implementation{Name: "claude-code", Version: "2.1.0"}, "claude-code 2.1.0"},
		{sdk.Implementation{Name: "codex"}, "codex"},
		{sdk.Implementation{Name: "x\n\x1b[31my", Version: "1\x00"}, "x[31my 1"},
		{sdk.Implementation{Name: strings.Repeat("ă", 300)}, strings.Repeat("ă", maxClientLen/2)},
	} {
		if got := clientName(&c.in); got != c.want {
			t.Errorf("clientName(%q, %q) = %q, muốn %q", c.in.Name, c.in.Version, got, c.want)
		}
	}
}
