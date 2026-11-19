package claudecode

import (
	"strings"
	"testing"
)

// goodInit giống sự kiện init thật ở docs/bang-chung/M8-claude-init.log, cộng
// MCP server tpan đã nối và hai công cụ của nó.
func goodInit() Init {
	return Init{
		Tools:      []string{"mcp__tpan__list_sites", "mcp__tpan__site_health"},
		MCPServers: []MCPStatus{{Name: "tpan", Status: "connected"}},
		Plugins: []Plugin{
			{Name: "cc-plugin-agents-md", Path: "builtin", Source: "cc-plugin-agents-md@builtin"},
			{Name: "cc-plugin-privacy", Path: "builtin", Source: "cc-plugin-privacy@builtin"},
		},
		Skills:        []string{},
		SlashCommands: []string{},
		Model:         "claude-sonnet-5-5",
		Version:       "2.1.328",
	}
}

func TestGateQua(t *testing.T) {
	if err := Gate(goodInit()); err != nil {
		t.Fatalf("Gate(init tốt) = %v", err)
	}
	// Không có công cụ nào (tpan chưa khai công cụ) vẫn qua: không có gì thừa.
	in := goodInit()
	in.Tools = nil
	if err := Gate(in); err != nil {
		t.Fatalf("Gate(không công cụ) = %v", err)
	}
}

func TestGateTuChoi(t *testing.T) {
	cases := map[string]struct {
		edit func(*Init)
		want []string // mỗi chuỗi phải có trong lời từ chối
	}{
		"công cụ Bash": {
			func(in *Init) { in.Tools = append(in.Tools, "Bash", "Read") },
			[]string{"Bash", "Read"},
		},
		"công cụ của MCP server khác": {
			func(in *Init) { in.Tools = append(in.Tools, "mcp__github__create_issue") },
			[]string{"mcp__github__create_issue"},
		},
		"tiền tố gần giống": {
			func(in *Init) { in.Tools = append(in.Tools, "mcp__tpanx__run") },
			[]string{"mcp__tpanx__run"},
		},
		"MCP server lạ": {
			func(in *Init) { in.MCPServers = append(in.MCPServers, MCPStatus{Name: "github", Status: "connected"}) },
			[]string{"github"},
		},
		"tpan chưa nối": {
			func(in *Init) { in.MCPServers = []MCPStatus{{Name: "tpan", Status: "failed"}} },
			[]string{"tpan", "failed"},
		},
		"không có tpan": {
			func(in *Init) { in.MCPServers = nil },
			[]string{"tpan"},
		},
		"plugin không builtin": {
			func(in *Init) {
				in.Plugins = append(in.Plugins, Plugin{Name: "evil", Path: "/home/u/.claude/plugins/evil", Source: "evil@market"})
			},
			[]string{"evil", "/home/u/.claude/plugins/evil"},
		},
		"skills": {
			func(in *Init) { in.Skills = []string{"deploy"} },
			[]string{"deploy"},
		},
		"slash_commands": {
			func(in *Init) { in.SlashCommands = []string{"review"} },
			[]string{"review"},
		},
		"nhiều thứ thừa cùng lúc": {
			func(in *Init) {
				in.Tools = append(in.Tools, "Bash")
				in.MCPServers = append(in.MCPServers, MCPStatus{Name: "fs", Status: "connected"})
			},
			[]string{"Bash", "fs"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := goodInit()
			c.edit(&in)
			err := Gate(in)
			if err == nil {
				t.Fatal("Gate = nil, muốn từ chối")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("lời từ chối %q thiếu %q", err, w)
				}
			}
		})
	}
}

func TestGateNgoaiLe(t *testing.T) {
	old := AllowedBuiltinTools
	t.Cleanup(func() { AllowedBuiltinTools = old })
	AllowedBuiltinTools = []string{"EndConversation"}
	in := goodInit()
	in.Tools = append(in.Tools, "EndConversation")
	if err := Gate(in); err != nil {
		t.Fatalf("công cụ trong danh sách ngoại lệ: %v", err)
	}
	in.Tools = append(in.Tools, "Bash")
	if err := Gate(in); err == nil || strings.Contains(err.Error(), "EndConversation") {
		t.Fatalf("Gate = %v, muốn chỉ nêu Bash", err)
	}
}
