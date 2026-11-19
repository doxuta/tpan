// Command fakeclaude is a stand-in for the `claude` command (Claude Code) used
// by scripts/e2e-m8.sh, step 13 of section 12 in docs/KE-HOACH.md. The e2e puts
// it first on PATH under the name `claude` and runs `tpan brain` with it, so a
// question sent from the panel goes the whole way: panel -> bridge.Hub ->
// tpan brain -> this program -> `ssh ... sudo -n tpan mcp --session <id>`.
//
//	claude --version            prints "2.1.328 (Claude Code)"
//	claude auth status --json   prints {"loggedIn":true,"authMethod":"claude.ai"}
//	claude -p ...               plays one turn of `claude -p`, see below
//
// One `-p` turn:
//  1. reads the whole prompt from stdin and records argv, cwd and prompt in
//     <record_dir>/turn-N.json (N counts up from 1);
//  2. opens the MCP server named "tpan" in --mcp-config with the official Go
//     SDK client (exactly that command and args) and lists its tools;
//  3. prints the init event in the shape of a real one
//     (docs/bang-chung/M8-claude-init.log): tools mcp__tpan__<name>, one MCP
//     server tpan connected, the 5 builtin plugins, no skills, no slash
//     commands; extra_tool in the config adds that tool name (e.g. "Bash");
//  4. waits think_ms (stands in for the model's latency; this is what lets
//     tpan's gate kill a refused session before any tool call), then writes
//     <record_dir>/turn-N.toolcall and plays the script for "mode":
//     propose: text, list_sites, text naming a site alias split across two
//     deltas, propose_site_add {domain c.example.com, email
//     admin@example.com}, text, result;
//     read: text, list_sites, text, result.
//     Tool calls really go to tpan mcp; their lines are printed as real
//     assistant tool_use / user tool_result lines.
//
// Configuration comes from fakeclaude.json next to the executable, re-read
// on every run (tpan brain passes only HOME, PATH, USER, LOGNAME, TMPDIR to
// claude, so environment variables set by the e2e would not arrive). The
// environment variable FAKE_CLAUDE_EXTRA_TOOL, when it does reach the
// program, overrides extra_tool.
//
// The program never writes into its working directory: tpan requires that
// directory to stay empty for --resume on the next turn.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "2.1.328"

type config struct {
	RecordDir string `json:"record_dir"`
	ExtraTool string `json:"extra_tool"`
	Mode      string `json:"mode"`     // "propose" (default) or "read"
	ThinkMS   int    `json:"think_ms"` // default 1000
	DeltaMS   int    `json:"delta_ms"` // pause between text deltas, default 150
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	switch {
	case len(args) == 1 && args[0] == "--version":
		fmt.Println(version + " (Claude Code)")
		return 0
	case len(args) >= 2 && args[0] == "auth" && args[1] == "status":
		fmt.Println(`{"loggedIn":true,"authMethod":"claude.ai"}`)
		return 0
	case len(args) >= 1 && args[0] == "-p":
		if err := turn(args); err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "fakeclaude: unsupported arguments %q\n", args)
	return 2
}

func loadConfig() (config, error) {
	cfg := config{Mode: "propose", ThinkMS: 1000, DeltaMS: 150}
	exe, err := os.Executable()
	if err != nil {
		return cfg, err
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "fakeclaude.json"))
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("fakeclaude.json: %w", err)
	}
	if v := os.Getenv("FAKE_CLAUDE_EXTRA_TOOL"); v != "" {
		cfg.ExtraTool = v
	}
	if cfg.RecordDir == "" {
		return cfg, errors.New("fakeclaude.json: record_dir is empty")
	}
	return cfg, nil
}

// flagValue returns the value after name in args ("" and false if absent).
// Parsed by hand: the flag package rejects unknown flags and claude's argv has
// many (and `--tools ""` must keep its empty value).
func flagValue(args []string, name string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1], true
		}
	}
	return "", false
}

type mcpConfig struct {
	MCPServers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"mcpServers"`
}

// out prints one stream-json line on stdout.
func out(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only maps and strings: cannot fail
	}
	_, _ = os.Stdout.Write(append(b, '\n')) // stdout closed means tpan killed us: nothing to report to
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// nextTurn picks N for turn-N.json in dir.
func nextTurn(dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	for n := 1; ; n++ {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("turn-%d.json", n))); errors.Is(err, os.ErrNotExist) {
			return n, nil
		}
	}
}

func turn(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	session, ok := flagValue(args, "--session-id")
	resume := false
	if !ok {
		session, resume = flagValue(args, "--resume")
	}
	model := "claude-sonnet-5-5"
	if m, _ := flagValue(args, "--model"); m != "" && m != "sonnet" {
		model = m
	}
	cwd, _ := os.Getwd()
	n, err := nextTurn(cfg.RecordDir)
	if err != nil {
		return err
	}
	rec := func(suffix string) string { return filepath.Join(cfg.RecordDir, fmt.Sprintf("turn-%d%s", n, suffix)) }
	record := map[string]any{"args": args, "cwd": cwd, "prompt": string(prompt), "resume": resume, "session": session, "mode": cfg.Mode, "extra_tool": cfg.ExtraTool}
	if err := writeJSON(rec(".json"), record); err != nil {
		return err
	}

	// 2. MCP server tpan, exactly as given in --mcp-config.
	raw, _ := flagValue(args, "--mcp-config")
	var mc mcpConfig
	if err := json.Unmarshal([]byte(raw), &mc); err != nil {
		return fmt.Errorf("--mcp-config: %w", err)
	}
	srv, ok := mc.MCPServers["tpan"]
	if !ok || len(mc.MCPServers) != 1 {
		return fmt.Errorf("--mcp-config must have exactly the server tpan, got %d servers", len(mc.MCPServers))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.Command(srv.Command, srv.Args...)
	// The server's stderr goes to a file, never to our stderr: tpan reads our
	// stdout and stderr together, and a long-lived ssh holding that pipe would
	// keep tpan waiting after this process is killed.
	errFile, err := os.Create(rec(".mcp-stderr"))
	if err != nil {
		return err
	}
	defer func() { _ = errFile.Close() }()
	cmd.Stderr = errFile
	client := sdk.NewClient(&sdk.Implementation{Name: "claude-code", Version: version}, nil)
	t0 := time.Now()
	cs, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return fmt.Errorf("connect MCP server tpan: %w", err)
	}
	defer func() { _ = cs.Close() }() // ends the ssh to tpan mcp; its exit code is not ours
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	mcpMS := time.Since(t0).Milliseconds()
	tools := []string{}
	for _, t := range lt.Tools {
		tools = append(tools, "mcp__tpan__"+t.Name)
	}
	if cfg.ExtraTool != "" {
		tools = append(tools, cfg.ExtraTool)
	}
	record["mcp_ms"] = mcpMS
	record["tools"] = tools
	if err := writeJSON(rec(".json"), record); err != nil {
		return err
	}

	// 3. init, in the shape of the real one.
	plugins := []map[string]string{}
	for _, p := range []string{"agents-md", "telemetry", "privacy", "plugin-authoring", "easel"} {
		name := "cc-plugin-" + p
		plugins = append(plugins, map[string]string{"name": name, "path": "builtin", "source": name + "@builtin"})
	}
	out(map[string]any{
		"type": "system", "subtype": "init", "cwd": cwd, "session_id": session,
		"tools":          tools,
		"mcp_servers":    []map[string]string{{"name": "tpan", "status": "connected", "source": "dynamic"}},
		"model":          model,
		"permissionMode": "dontAsk", "slash_commands": []string{}, "apiKeySource": "none",
		"claude_code_version": version, "output_style": "default",
		"agents":  []string{"claude", "Explore", "general-purpose", "Plan", "statusline-setup"},
		"skills":  []string{},
		"plugins": plugins,
		"uuid":    newUUID(),
	})

	// 4. "model latency", then the script.
	time.Sleep(time.Duration(cfg.ThinkMS) * time.Millisecond)
	if err := os.WriteFile(rec(".toolcall"), []byte(time.Now().Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		return err
	}
	p := &player{ctx: ctx, cs: cs, session: session, delta: time.Duration(cfg.DeltaMS) * time.Millisecond}
	p.text("Mình xem danh sách site trên máy trước.")
	sites := p.call("list_sites", map[string]any{})
	alias := firstDomain(sites)
	if len(alias) < 5 {
		alias = "site-a.example"
	}
	// The alias is cut in two deltas on purpose: the panel must join the text
	// before turning aliases back into real names.
	cut := len(alias) - 4
	// Each assistant message starts its own text, as with the real claude
	// (run 4 of M8): no space or newline joins it to the previous message.
	p.text("Máy đang có site `tpan:site/"+alias[:cut], alias[cut:]+"`.")
	if cfg.Mode == "propose" {
		p.text(" Theo yêu cầu, mình đề xuất tạo site mới.")
		res := p.call("propose_site_add", map[string]any{"domain": "c.example.com", "email": "admin@example.com"})
		id := planIDOf(res)
		p.text("Plan `tpan:plan/" + id + "` đang chờ bạn duyệt; máy chưa đổi gì.")
	} else {
		p.text(" Site đó không có gì bất thường trong danh sách.")
	}
	// Like the real claude: the last message as an assistant line, and the
	// result field holds only that last message, not the whole turn.
	last := p.msg.String()
	out(map[string]any{
		"type":               "assistant",
		"message":            map[string]any{"id": "msg_fake", "type": "message", "role": "assistant", "content": []map[string]any{{"type": "text", "text": last}}},
		"parent_tool_use_id": nil, "session_id": session, "uuid": newUUID(),
	})
	out(map[string]any{
		"type": "result", "subtype": "success", "is_error": false, "num_turns": p.turns + 1,
		"result": last, "permission_denials": []string{}, "session_id": session,
		"duration_ms": time.Since(t0).Milliseconds(),
	})
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// player prints the lines of one turn.
type player struct {
	ctx     context.Context
	cs      *sdk.ClientSession
	session string
	delta   time.Duration
	msg     strings.Builder // text of the current assistant message
	turns   int
	n       int
}

// text prints each part as one text_delta stream event.
func (p *player) text(parts ...string) {
	for _, s := range parts {
		out(map[string]any{
			"type":               "stream_event",
			"event":              map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": s}},
			"session_id":         p.session,
			"parent_tool_use_id": nil,
			"uuid":               newUUID(),
		})
		p.msg.WriteString(s)
		time.Sleep(p.delta)
	}
}

// call prints the assistant message (text so far plus one tool_use), calls the
// tool for real over MCP, and prints the user tool_result line.
func (p *player) call(tool string, args map[string]any) string {
	p.n++
	p.turns++
	id := fmt.Sprintf("toolu_fake%02d", p.n)
	content := []map[string]any{}
	if p.msg.Len() > 0 {
		content = append(content, map[string]any{"type": "text", "text": p.msg.String()})
		p.msg.Reset()
	}
	content = append(content, map[string]any{"type": "tool_use", "id": id, "name": "mcp__tpan__" + tool, "input": args, "caller": map[string]string{"type": "direct"}})
	out(map[string]any{
		"type":               "assistant",
		"message":            map[string]any{"id": "msg_fake", "type": "message", "role": "assistant", "content": content},
		"parent_tool_use_id": nil, "session_id": p.session, "uuid": newUUID(),
	})
	text, isErr := "", false
	res, err := p.cs.CallTool(p.ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		text, isErr = "MCP error: "+err.Error(), true
	} else {
		isErr = res.IsError
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				text += tc.Text
			}
		}
	}
	out(map[string]any{
		"type": "user",
		"message": map[string]any{"role": "user", "content": []map[string]any{{
			"tool_use_id": id, "type": "tool_result", "is_error": isErr,
			"content": []map[string]string{{"type": "text", "text": text}},
		}}},
		"parent_tool_use_id": nil, "session_id": p.session, "uuid": newUUID(),
	})
	return text
}

func firstDomain(listSites string) string {
	var v []struct {
		Domain string `json:"domain"`
	}
	if json.Unmarshal([]byte(listSites), &v) != nil || len(v) == 0 {
		return ""
	}
	return v[0].Domain
}

func planIDOf(s string) string {
	var v struct {
		PlanID string `json:"plan_id"`
	}
	_ = json.Unmarshal([]byte(s), &v)
	return v.PlanID
}
