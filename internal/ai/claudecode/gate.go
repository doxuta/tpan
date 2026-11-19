package claudecode

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Gate là "kiểm cửa rồi mới tin" của mục 8.2: sự kiện init cho biết phiên claude
// THẬT SỰ đã nạp gì. Các cờ trên dòng lệnh có thể không còn tác dụng như tài
// liệu nói ở một bản Claude Code sau, nên tpan không tin cờ mà tin init: thấy
// bất cứ thứ gì ngoài MCP server tpan và công cụ của nó thì dừng. Lời từ chối
// nêu đúng từng thứ thừa để người vận hành biết phải gỡ gì.
func Gate(in Init) error {
	var bad []string

	var tools []string
	for _, t := range in.Tools {
		if !strings.HasPrefix(t, "mcp__tpan__") && !slices.Contains(AllowedBuiltinTools, t) {
			tools = append(tools, t)
		}
	}
	if len(tools) > 0 {
		bad = append(bad, "công cụ ngoài tpan: "+strings.Join(tools, ", "))
	}

	tpan := false // đã thấy MCP server tên tpan (dù trạng thái nào)
	var others []string
	for _, s := range in.MCPServers {
		switch {
		case s.Name != "tpan":
			others = append(others, fmt.Sprintf("%s (%s)", s.Name, s.Status))
		case s.Status != "connected":
			tpan = true
			bad = append(bad, fmt.Sprintf("MCP server tpan ở trạng thái %q, không phải connected", s.Status))
		default:
			tpan = true
		}
	}
	if len(others) > 0 {
		bad = append(bad, "MCP server lạ: "+strings.Join(others, ", "))
	}
	// Không có tpan thì AI chỉ trả lời bằng trí nhớ, không có bằng chứng nào.
	if !tpan {
		bad = append(bad, "không có MCP server tpan")
	}

	var plugins []string
	for _, p := range in.Plugins {
		if p.Path != "builtin" {
			plugins = append(plugins, fmt.Sprintf("%s (%s)", p.Name, p.Path))
		}
	}
	if len(plugins) > 0 {
		bad = append(bad, "plugin không phải builtin: "+strings.Join(plugins, ", "))
	}
	if len(in.Skills) > 0 {
		bad = append(bad, "skills: "+strings.Join(in.Skills, ", "))
	}
	if len(in.SlashCommands) > 0 {
		bad = append(bad, "slash_commands: "+strings.Join(in.SlashCommands, ", "))
	}

	if len(bad) == 0 {
		return nil
	}
	return errors.New("cửa kiểm từ chối phiên Claude Code (" + in.Version + "): " + strings.Join(bad, "; "))
}
