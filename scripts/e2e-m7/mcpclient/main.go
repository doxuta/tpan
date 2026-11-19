// Command mcpclient is a tiny MCP test client for scripts/e2e-m7.sh. It starts
// the server command given after "--" (for example `ssh ... sudo -n tpan mcp`)
// with the official Go SDK's CommandTransport, runs a script read from stdin in
// ONE client session, and prints one JSON object per line on stdout.
//
// Script lines (blank lines and lines starting with # are ignored):
//
//	list-tools
//	<tool> [<json arguments>]
//
// "{{plan_id}}" in the arguments is replaced by the plan_id found in the most
// recent tool result that had one, so a script can propose and then apply in
// the same session. "{{site:N}}" is replaced by the domain of the N-th site
// (from 0) in the most recent list_sites result, i.e. the alias the agent saw.
//
// Output lines: {"op":"connect",...}, {"op":"list_tools",...},
// {"op":"call",...} and {"op":"close",...}. Arguments are never echoed back,
// so the output holds only what the server sent (plus timings and the server
// command's exit code).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	os.Exit(run())
}

func run() int {
	timeout := flag.Duration("timeout", 20*time.Minute, "overall deadline for the whole session")
	protocol := flag.String("protocol", "", "protocol version to request; empty means the SDK's latest")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mcpclient [-timeout D] [-protocol V] -- <server command...> <script")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	out := json.NewEncoder(os.Stdout)
	emit := func(v map[string]any) {
		if err := out.Encode(v); err != nil {
			fmt.Fprintln(os.Stderr, "mcpclient: write stdout:", err)
		}
	}

	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	// exec drops the child's stderr by default; keep it so ssh and tpan errors
	// reach the script's log.
	cmd.Stderr = os.Stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "tpan-e2e-mcpclient", Version: "1"}, nil)

	start := time.Now()
	cs, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, &sdk.ClientSessionOptions{ProtocolVersion: *protocol})
	if err != nil {
		emit(map[string]any{"op": "connect", "ms": ms(start), "error": err.Error()})
		return 1
	}
	init := cs.InitializeResult()
	emit(map[string]any{"op": "connect", "ms": ms(start), "protocol": init.ProtocolVersion, "server": init.ServerInfo})

	failed := runScript(ctx, cs, emit)

	start = time.Now()
	cerr := cs.Close()
	closed := map[string]any{"op": "close", "ms": ms(start), "exit_code": -1}
	if cmd.ProcessState != nil {
		closed["exit_code"] = cmd.ProcessState.ExitCode()
	}
	if cerr != nil {
		closed["error"] = cerr.Error()
	}
	emit(closed)
	if failed || cerr != nil {
		return 1
	}
	return 0
}

// runScript runs every line of stdin; it reports whether any line failed at the
// protocol level (a tool result with isError is not a protocol failure).
func runScript(ctx context.Context, cs *sdk.ClientSession, emit func(map[string]any)) bool {
	failed := false
	planID := ""
	var sites []string
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "list-tools" {
			start := time.Now()
			res, err := cs.ListTools(ctx, nil)
			if err != nil {
				emit(map[string]any{"op": "list_tools", "ms": ms(start), "error": err.Error()})
				failed = true
				continue
			}
			emit(map[string]any{"op": "list_tools", "ms": ms(start), "result": res})
			continue
		}
		name, raw, _ := strings.Cut(line, " ")
		raw = strings.ReplaceAll(raw, "{{plan_id}}", planID)
		for i, d := range sites {
			raw = strings.ReplaceAll(raw, fmt.Sprintf("{{site:%d}}", i), d)
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			raw = "{}"
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			emit(map[string]any{"op": "call", "tool": name, "error": "bad script arguments: " + err.Error()})
			failed = true
			continue
		}
		start := time.Now()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			emit(map[string]any{"op": "call", "tool": name, "ms": ms(start), "error": err.Error()})
			failed = true
			continue
		}
		if id := planIDOf(res); id != "" {
			planID = id
		}
		if name == "list_sites" {
			sites = domainsOf(res)
		}
		emit(map[string]any{"op": "call", "tool": name, "ms": ms(start), "result": res})
	}
	if err := sc.Err(); err != nil {
		emit(map[string]any{"op": "script", "error": err.Error()})
		failed = true
	}
	return failed
}

// textOf is the single text content of a successful result, or "".
func textOf(res *sdk.CallToolResult) string {
	if res.IsError || len(res.Content) != 1 {
		return ""
	}
	if tc, ok := res.Content[0].(*sdk.TextContent); ok {
		return tc.Text
	}
	return ""
}

// planIDOf reads "plan_id" from a result whose text is a JSON object.
func planIDOf(res *sdk.CallToolResult) string {
	var v struct {
		PlanID string `json:"plan_id"`
	}
	if json.Unmarshal([]byte(textOf(res)), &v) != nil {
		return ""
	}
	return v.PlanID
}

// domainsOf reads the domains of a list_sites result (a JSON array of sites).
func domainsOf(res *sdk.CallToolResult) []string {
	var v []struct {
		Domain string `json:"domain"`
	}
	if json.Unmarshal([]byte(textOf(res)), &v) != nil {
		return nil
	}
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = s.Domain
	}
	return out
}

func ms(t time.Time) int64 { return time.Since(t).Milliseconds() }
