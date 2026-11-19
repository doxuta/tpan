package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/system"
)

// `claude` giả là chính binary test: TestMain thấy TPAN_FAKE_CLAUDE thì đóng
// vai claude, in kịch bản stream-json theo giá trị biến rồi thoát, không chạy
// test nào. Nhờ vậy test đi qua đúng đường thật (system.LocalSystem, argv,
// stdin, Dir, Env) mà không cần Claude Code hay mạng.
func TestMain(m *testing.M) {
	if s := os.Getenv("TPAN_FAKE_CLAUDE"); s != "" {
		os.Exit(fakeClaude(s))
	}
	os.Exit(m.Run())
}

func fakeInit(tools ...string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "system", "subtype": "init", "cwd": "/x", "session_id": "s",
		"tools":       append([]string{"mcp__tpan__list_sites", "mcp__tpan__propose_site_repair"}, tools...),
		"mcp_servers": []map[string]string{{"name": "tpan", "status": "connected"}},
		"model":       "claude-sonnet-5-5", "permissionMode": "dontAsk",
		"slash_commands": []string{}, "claude_code_version": "2.1.328",
		"agents": []string{"claude", "Explore"}, "skills": []string{},
		"plugins": []map[string]string{{"name": "cc-plugin-privacy", "path": "builtin", "source": "cc-plugin-privacy@builtin"}},
	})
	return string(b)
}

func delta(s string) string {
	return `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + strconv.Quote(s) + `}}}`
}

func fakeClaude(scenario string) int {
	stdin, _ := io.ReadAll(os.Stdin)
	out := func(lines ...string) {
		for _, l := range lines {
			fmt.Println(l)
		}
	}
	switch scenario {
	case "stream":
		fmt.Fprintln(os.Stderr, "warning: không phải JSON, phải bị bỏ qua")
		out(fakeInit(),
			`{"type":"system","subtype":"status","status":"requesting"}`,
			delta("Đang "), delta("kiểm tra"),
			`{"type":"assistant","message":{"content":[{"type":"text","text":"Đang kiểm tra"},{"type":"tool_use","id":"toolu_1","name":"mcp__tpan__list_sites","input":{}}]}}`,
			`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"[{\"domain\":\"site-a.example\"}]","is_error":false}]}}`,
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"mcp__tpan__propose_site_repair","input":{"domain":"site-a.example"}}]}}`,
			`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"text","text":"{\"plan_id\":\"p0001\"}"}],"is_error":false}]}}`,
			`{"type":"rate_limit_event","rate_limit_info":{}}`,
			"rác không phải JSON",
			delta(" Xong."),
			`{"type":"result","subtype":"success","is_error":false,"result":"Đang kiểm tra Xong.","num_turns":3,"session_id":"s","permission_denials":[]}`)
	case "bash":
		out(fakeInit("Bash"), delta("không được hiện"),
			`{"type":"result","subtype":"success","is_error":false,"result":"không được hiện"}`)
	case "status-first":
		out(`{"type":"system","subtype":"status","status":"requesting"}`, fakeInit(), delta("x"),
			`{"type":"result","subtype":"success","is_error":false,"result":"x"}`)
	case "echo":
		// In lại argv, stdin, thư mục làm việc và các file trong đó qua trường result.
		cwd, _ := os.Getwd()
		var files []string
		if es, err := os.ReadDir(cwd); err == nil {
			for _, e := range es {
				files = append(files, e.Name())
			}
		}
		b, _ := json.Marshal(map[string]any{"args": os.Args[1:], "stdin": string(stdin), "cwd": cwd, "files": files})
		r, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": string(b)})
		out(fakeInit(), string(r))
	case "error-result":
		out(fakeInit(), `{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":12,"errors":["Reached maximum number of turns (12)"]}`)
	case "no-result":
		out(fakeInit(), delta("nửa chừng"))
		fmt.Fprintln(os.Stderr, "boom: mất mạng")
		return 3
	case "sleep":
		out(fakeInit())
		time.Sleep(time.Minute)
	case "version":
		out("2.1.328 (Claude Code)")
	case "auth":
		out(`{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"nguoi@example.com","orgId":"org-1","orgName":"Công ty A","subscriptionType":"max"}`)
	case "auth-bad":
		out(`loggedIn nguoi@example.com`)
		return 1
	default:
		fmt.Fprintln(os.Stderr, "kịch bản lạ:", scenario)
		return 2
	}
	return 0
}

const (
	testSession = "0123456789abcdef"
	testBrain   = "123e4567-e89b-12d3-a456-426614174000"
)

var testMCP = MCPServer{Command: "ssh", Args: []string{"-o", "BatchMode=yes", "ubuntu@vm.example", "sudo", "-n", "tpan", "mcp"}}

func fakeConfig(t *testing.T, scenario string) Config {
	t.Helper()
	exe, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		Sys:      system.NewLocal(),
		Claude:   exe,
		Env:      []string{"TPAN_FAKE_CLAUDE=" + scenario},
		MCP:      testMCP,
		WorkRoot: filepath.Join(t.TempDir(), "brain"),
	}
}

func newFake(t *testing.T, scenario string) *Brain {
	t.Helper()
	b, err := New(fakeConfig(t, scenario))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func testJob(resume bool) ai.Job {
	return ai.Job{ID: testSession + "-1", SessionID: testSession, BrainSession: testBrain, Resume: resume, Prompt: "<tpan-context lang=\"vi\"></tpan-context>\nSite chết?", Lang: "vi"}
}

// collect đọc hết kênh; kênh phải đóng sau đúng một sự kiện kết thúc.
func collect(t *testing.T, ch <-chan ai.Event) []ai.Event {
	t.Helper()
	var evs []ai.Event
	timeout := time.After(30 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				finals := 0
				for _, e := range evs {
					if e.Final() {
						finals++
					}
				}
				if finals != 1 || !evs[len(evs)-1].Final() {
					t.Fatalf("kênh đóng với %d sự kiện kết thúc (phải đúng 1, ở cuối): %+v", finals, evs)
				}
				return evs
			}
			evs = append(evs, e)
		case <-timeout:
			t.Fatalf("kênh không đóng sau 30 giây; đã nhận %+v", evs)
		}
	}
}

func runFake(t *testing.T, b *Brain, job ai.Job) []ai.Event {
	t.Helper()
	ch, err := b.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return collect(t, ch)
}

func kinds(evs []ai.Event) []ai.EventKind {
	var k []ai.EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func TestRunLuongSuKien(t *testing.T) {
	evs := runFake(t, newFake(t, "stream"), testJob(false))
	want := []ai.EventKind{ai.EventInit, ai.EventText, ai.EventText, ai.EventToolCall, ai.EventToolResult,
		ai.EventToolCall, ai.EventToolResult, ai.EventText, ai.EventDone}
	if !slices.Equal(kinds(evs), want) {
		t.Fatalf("loại sự kiện = %v, muốn %v\n%+v", kinds(evs), want, evs)
	}
	if evs[0].Version != "2.1.328" || evs[0].Model != "claude-sonnet-5-5" {
		t.Errorf("init = %+v", evs[0])
	}
	var text string
	for _, e := range evs {
		if e.Kind == ai.EventText {
			text += e.Text
		}
	}
	if text != "Đang kiểm tra Xong." {
		t.Errorf("chữ ghép từ các EventText = %q", text)
	}
	if e := evs[3]; e.Tool != "mcp__tpan__list_sites" || string(e.Input) != "{}" {
		t.Errorf("tool_call = %+v", e)
	}
	if e := evs[4]; e.Tool != "mcp__tpan__list_sites" || e.Text != `[{"domain":"site-a.example"}]` || e.PlanID != "" {
		t.Errorf("tool_result 1 = %+v", e)
	}
	if e := evs[6]; e.Tool != "mcp__tpan__propose_site_repair" || e.PlanID != "p0001" {
		t.Errorf("tool_result 2 = %+v", e)
	}
	if e := evs[8]; e.Text != "Đang kiểm tra Xong." {
		t.Errorf("done = %+v", e)
	}
}

// Init khai thêm Bash: chỉ một EventRefused nêu Bash, không chữ nào của câu trả lời.
func TestRunCuaKiemTuChoi(t *testing.T) {
	for _, sc := range []string{"bash", "status-first"} {
		t.Run(sc, func(t *testing.T) {
			evs := runFake(t, newFake(t, sc), testJob(false))
			if !slices.Equal(kinds(evs), []ai.EventKind{ai.EventRefused}) {
				t.Fatalf("sự kiện = %+v, muốn đúng một EventRefused", evs)
			}
			want := map[string]string{"bash": "Bash", "status-first": "init"}[sc]
			if !strings.Contains(evs[0].Text, want) {
				t.Errorf("lời từ chối %q thiếu %q", evs[0].Text, want)
			}
		})
	}
}

type echoed struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
	Cwd   string   `json:"cwd"`
	Files []string `json:"files"`
}

func runEcho(t *testing.T, b *Brain, job ai.Job) echoed {
	t.Helper()
	evs := runFake(t, b, job)
	last := evs[len(evs)-1]
	if last.Kind != ai.EventDone {
		t.Fatalf("sự kiện cuối = %+v", last)
	}
	var e echoed
	if err := json.Unmarshal([]byte(last.Text), &e); err != nil {
		t.Fatalf("giải output của claude giả: %v", err)
	}
	return e
}

func TestRunArgsVaThuMuc(t *testing.T) {
	cfg := fakeConfig(t, "echo")
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	promptFile := filepath.Join(cfg.WorkRoot, "system-prompt.md")
	wantDir, err := filepath.EvalSymlinks(cfg.WorkRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantDir = filepath.Join(wantDir, testSession)

	for i, resume := range []bool{false, true} {
		job := testJob(resume)
		e := runEcho(t, b, job)

		// Bảng cờ mục 8.2, viết tay ở đây (không gọi Args) để test không tự khớp với chính nó.
		last := []string{"--session-id", testBrain}
		if resume {
			last = []string{"--resume", testBrain}
		}
		mcpIdx := slices.Index(e.Args, "--mcp-config") + 1
		if mcpIdx == 0 || mcpIdx >= len(e.Args) {
			t.Fatalf("không có --mcp-config: %q", e.Args)
		}
		want := append([]string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
			"--tools", "", "--strict-mcp-config", "--mcp-config", e.Args[mcpIdx], "--allowedTools", "mcp__tpan",
			"--permission-mode", "dontAsk", "--permission-prompts", "none",
			"--setting-sources", "", "--disable-slash-commands",
			"--append-system-prompt-file", promptFile, "--max-turns", "12", "--model", "sonnet"}, last...)
		if !slices.Equal(e.Args, want) {
			t.Errorf("lượt %d: argv\n got %q\nwant %q", i+1, e.Args, want)
		}
		if slices.Contains(e.Args, "--bare") {
			t.Errorf("lượt %d: có --bare", i+1)
		}

		var conf struct {
			MCPServers map[string]MCPServer `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(e.Args[mcpIdx]), &conf); err != nil {
			t.Fatalf("--mcp-config không phải JSON: %v", err)
		}
		wantConf := map[string]MCPServer{"tpan": {Command: "ssh", Args: append(slices.Clone(testMCP.Args), "--session", testSession)}}
		if len(conf.MCPServers) != 1 || conf.MCPServers["tpan"].Command != wantConf["tpan"].Command ||
			!slices.Equal(conf.MCPServers["tpan"].Args, wantConf["tpan"].Args) {
			t.Errorf("--mcp-config = %+v, muốn %+v", conf.MCPServers, wantConf)
		}

		if e.Stdin != job.Prompt {
			t.Errorf("stdin = %q, muốn %q", e.Stdin, job.Prompt)
		}
		cwd, err := filepath.EvalSymlinks(e.Cwd)
		if err != nil {
			t.Fatal(err)
		}
		if cwd != wantDir {
			t.Errorf("lượt %d: cwd = %s, muốn %s (cùng thư mục ở mọi lượt để --resume tìm thấy phiên)", i+1, cwd, wantDir)
		}
		if len(e.Files) != 0 {
			t.Errorf("thư mục làm việc không rỗng: %q", e.Files)
		}
	}
	// Run không được làm thay đổi cfg.MCP.Args (append vào slice dùng chung).
	if !slices.Equal(cfg.MCP.Args, testMCP.Args) {
		t.Errorf("MCP.Args bị đổi: %q", cfg.MCP.Args)
	}
	if fi, err := os.Stat(wantDir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("thư mục phiên: %v, quyền %v, muốn 0700", err, fi.Mode().Perm())
	}
}

func TestArgsMacDinh(t *testing.T) {
	a := Args(Config{MCP: MCPServer{Command: "tpan", Args: nil}}, testJob(false), "/p.md")
	i := slices.Index(a, "--model")
	if i < 0 || a[i+1] != ai.DefaultModel {
		t.Errorf("--model mặc định: %q", a)
	}
	j := slices.Index(a, "--mcp-config")
	if a[j+1] != `{"mcpServers":{"tpan":{"command":"tpan","args":["--session","0123456789abcdef"]}}}` {
		t.Errorf("--mcp-config = %s", a[j+1])
	}
}

// Thư mục phiên có sẵn file (ví dụ ai đó đặt CLAUDE.md vào) thì không chạy:
// claude có thể đọc file ở thư mục làm việc.
func TestRunThuMucKhongRong(t *testing.T) {
	cfg := fakeConfig(t, "echo")
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.WorkRoot, testSession)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ch, err := b.Run(context.Background(), testJob(false)); err == nil || ch != nil || !strings.Contains(err.Error(), "CLAUDE.md") {
		t.Fatalf("Run = %v, %v; muốn lỗi nêu CLAUDE.md", ch, err)
	}
}

func TestRunJobKhongHopLe(t *testing.T) {
	cfg := fakeConfig(t, "echo")
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*ai.Job){
		"session rỗng":       func(j *ai.Job) { j.SessionID = "" },
		"session ../":        func(j *ai.Job) { j.SessionID = "../../etc" },
		"session ;":          func(j *ai.Job) { j.SessionID = "0123456789abcde;" },
		"session $()":        func(j *ai.Job) { j.SessionID = "$(id)" },
		"session xuống dòng": func(j *ai.Job) { j.SessionID = "0123456789abcde\n" },
		"session chữ hoa":    func(j *ai.Job) { j.SessionID = "0123456789ABCDEF" },
		"session dài":        func(j *ai.Job) { j.SessionID = strings.Repeat("a", 300) },
		"brain rỗng":         func(j *ai.Job) { j.BrainSession = "" },
		"brain là cờ":        func(j *ai.Job) { j.BrainSession = "--bare" },
		"brain không UUID":   func(j *ai.Job) { j.BrainSession = "123e4567-e89b-12d3-a456-42661417400" },
		"prompt rỗng":        func(j *ai.Job) { j.Prompt = "" },
	}
	for name, edit := range bad {
		t.Run(name, func(t *testing.T) {
			job := testJob(false)
			edit(&job)
			if ch, err := b.Run(context.Background(), job); err == nil || ch != nil {
				t.Fatalf("Run = %v, %v; muốn lỗi", ch, err)
			}
		})
	}
	// Không job hỏng nào được tạo thư mục ngoài WorkRoot hay trong WorkRoot.
	names, _ := os.ReadDir(cfg.WorkRoot)
	for _, n := range names {
		if n.Name() != "system-prompt.md" {
			t.Errorf("WorkRoot có thêm %s", n.Name())
		}
	}
}

func TestRunResultLoi(t *testing.T) {
	evs := runFake(t, newFake(t, "error-result"), testJob(false))
	if !slices.Equal(kinds(evs), []ai.EventKind{ai.EventInit, ai.EventError}) || !strings.Contains(evs[1].Text, "error_max_turns") {
		t.Fatalf("sự kiện = %+v", evs)
	}
}

func TestRunThoatKhongCoResult(t *testing.T) {
	evs := runFake(t, newFake(t, "no-result"), testJob(false))
	if !slices.Equal(kinds(evs), []ai.EventKind{ai.EventInit, ai.EventText, ai.EventError}) {
		t.Fatalf("sự kiện = %+v", evs)
	}
	msg := evs[2].Text
	for _, w := range []string{"3", "boom: mất mạng"} {
		if !strings.Contains(msg, w) {
			t.Errorf("lỗi %q thiếu %q", msg, w)
		}
	}
	// Lỗi hiện trên panel: không in argv (có lệnh ssh tới máy) ra đó.
	if strings.Contains(msg, "vm.example") || strings.Contains(msg, "--mcp-config") {
		t.Errorf("lỗi lộ dòng lệnh: %q", msg)
	}
}

func TestRunHuyCtx(t *testing.T) {
	b := newFake(t, "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := b.Run(ctx, testJob(false))
	if err != nil {
		t.Fatal(err)
	}
	if e := <-ch; e.Kind != ai.EventInit {
		t.Fatalf("sự kiện đầu = %+v", e)
	}
	start := time.Now()
	cancel()
	evs := collect(t, ch)
	if !slices.Equal(kinds(evs), []ai.EventKind{ai.EventError}) {
		t.Fatalf("sau khi huỷ = %+v", evs)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("huỷ mất %v; claude không bị giết?", d)
	}
}

func TestRunKhongCoClaude(t *testing.T) {
	cfg := fakeConfig(t, "echo")
	cfg.Claude = filepath.Join(t.TempDir(), "khong-co-claude")
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	evs := runFake(t, b, testJob(false))
	if !slices.Equal(kinds(evs), []ai.EventKind{ai.EventError}) {
		t.Fatalf("sự kiện = %+v", evs)
	}
}

func TestNew(t *testing.T) {
	cfg := fakeConfig(t, "echo")
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if b.cfg.Model != ai.DefaultModel {
		t.Errorf("Model = %q", b.cfg.Model)
	}
	p := filepath.Join(cfg.WorkRoot, "system-prompt.md")
	data, err := os.ReadFile(p)
	if err != nil || string(data) != SystemPrompt || b.promptFile != p {
		t.Fatalf("file system prompt: %v, promptFile %s", err, b.promptFile)
	}
	fi, _ := os.Stat(p)
	di, _ := os.Stat(cfg.WorkRoot)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("quyền: file %v, thư mục %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	for _, w := range []string{"lang", "mcp__tpan__", "propose_", "<untrusted>", "AI đề xuất, chưa được kiểm", "site-a.example", "tpan:plan/<id>"} {
		if !strings.Contains(SystemPrompt, w) {
			t.Errorf("SystemPrompt thiếu %q", w)
		}
	}

	bad := map[string]func(*Config){
		"không Sys":          func(c *Config) { c.Sys = nil },
		"không Claude":       func(c *Config) { c.Claude = "" },
		"không MCP":          func(c *Config) { c.MCP.Command = "" },
		"không WorkRoot":     func(c *Config) { c.WorkRoot = "" },
		"WorkRoot tương đối": func(c *Config) { c.WorkRoot = "brain" },
	}
	for name, edit := range bad {
		c := fakeConfig(t, "echo")
		edit(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: New không lỗi", name)
		}
	}
}

func TestVersion(t *testing.T) {
	v, err := Version(context.Background(), fakeConfig(t, "version"))
	if err != nil || v != "2.1.328" {
		t.Fatalf("Version = %q, %v", v, err)
	}
}

func TestAuth(t *testing.T) {
	st, err := Auth(context.Background(), fakeConfig(t, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	if st != (AuthStatus{LoggedIn: true, AuthMethod: "claude.ai"}) {
		t.Errorf("Auth = %+v", st)
	}
	// Output hỏng có email: lỗi không được chép output.
	_, err = Auth(context.Background(), fakeConfig(t, "auth-bad"))
	if err == nil || strings.Contains(err.Error(), "example.com") {
		t.Fatalf("Auth(output hỏng) = %v", err)
	}
}

// --max-turns chỉ hạ được: 0 và số lớn hơn trần đều về ai.MaxTurns.
func TestArgsMaxTurns(t *testing.T) {
	job := ai.Job{SessionID: "0123456789abcdef", BrainSession: "11111111-2222-4333-8444-555555555555"}
	for in, want := range map[int]string{0: "12", 3: "3", 99: "12"} {
		args := Args(Config{MaxTurns: in, MCP: MCPServer{Command: "tpan"}}, job, "/p")
		i := slices.Index(args, "--max-turns")
		if i < 0 || args[i+1] != want {
			t.Errorf("MaxTurns %d: --max-turns = %v, muốn %s", in, args, want)
		}
	}
}
