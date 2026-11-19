package claudecode

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/ai"
)

func TestParserLine(t *testing.T) {
	var p Parser // giá trị rỗng phải dùng được (map tạo khi cần)
	lines := []string{
		`{"type":"system","subtype":"status","status":"requesting"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Đang "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}}`,
		// Khối text trong assistant trùng với các delta đã phát: không phát lại.
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Đang "},{"type":"tool_use","id":"toolu_1","name":"mcp__tpan__list_sites","input":{"q":"a"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"[{\"domain\":\"site-a.example\"}]","is_error":false}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"mcp__tpan__propose_site_repair","input":{"domain":"site-a.example"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"text","text":"{\"plan_id\":\"p0001\",\"steps\":3}"}]}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_9","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}],"is_error":true}]}}`,
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
		`không phải JSON`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Xong.","num_turns":2,"permission_denials":[]}`,
	}
	var got []ai.Event
	for _, l := range lines {
		got = append(got, p.Line([]byte(l))...)
	}
	want := []ai.Event{
		{Kind: ai.EventText, Text: "Đang "},
		{Kind: ai.EventToolCall, Tool: "mcp__tpan__list_sites", Input: []byte(`{"q":"a"}`)},
		{Kind: ai.EventToolResult, Tool: "mcp__tpan__list_sites", Text: `[{"domain":"site-a.example"}]`},
		{Kind: ai.EventToolCall, Tool: "mcp__tpan__propose_site_repair", Input: []byte(`{"domain":"site-a.example"}`)},
		{Kind: ai.EventToolResult, Tool: "mcp__tpan__propose_site_repair", Text: `{"plan_id":"p0001","steps":3}`, PlanID: "p0001"},
		{Kind: ai.EventToolResult, Text: "a\nb", IsError: true},
		{Kind: ai.EventDone, Text: "Xong."},
	}
	if len(got) != len(want) {
		t.Fatalf("có %d sự kiện, muốn %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Kind != w.Kind || g.Text != w.Text || g.Tool != w.Tool || string(g.Input) != string(w.Input) ||
			g.IsError != w.IsError || g.PlanID != w.PlanID {
			t.Errorf("sự kiện %d = %+v, muốn %+v", i, g, w)
		}
		if g.Time.IsZero() {
			t.Errorf("sự kiện %d thiếu Time", i)
		}
	}
}

// Chữ của hai tin nhắn assistant liền nhau (trước và sau một lần gọi công cụ)
// được ngăn bằng một dòng trống. Lần chạy Claude Code thật số 4 (M8, đợt 4)
// cho thấy không ngăn thì panel hiện "...kết quả đã lưu.Nguyên nhân gốc...".
func TestParserNganDoanGiuaTinNhan(t *testing.T) {
	var p Parser
	delta := func(s string) string {
		return `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + quoteJSON(s) + `}}}`
	}
	lines := []string{
		delta("Tôi chạy "), delta("health check."),
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Tôi chạy health check."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__tpan__site_health","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"{}"}]}}`,
		delta("Nguyên nhân"), delta(" gốc đã rõ."),
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Nguyên nhân gốc đã rõ."}]}}`,
		delta("**Chẩn đoán**"),
	}
	var text strings.Builder
	for _, l := range lines {
		for _, e := range p.Line([]byte(l)) {
			if e.Kind == ai.EventText {
				text.WriteString(e.Text)
			}
		}
	}
	want := "Tôi chạy health check.\n\nNguyên nhân gốc đã rõ.\n\n**Chẩn đoán**"
	if text.String() != want {
		t.Errorf("chữ nối lại = %q, muốn %q", text.String(), want)
	}

	// Tin nhắn assistant đầu tiên không có chữ (chỉ gọi công cụ): chữ đầu tiên
	// của câu trả lời không bị thêm dòng trống ở đầu.
	var q Parser
	q.Line([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__tpan__site_health","input":{}}]}}`))
	if ev := q.Line([]byte(delta("Xong."))); len(ev) != 1 || ev[0].Text != "Xong." {
		t.Errorf("chữ đầu tiên = %+v, muốn đúng \"Xong.\"", ev)
	}
}

func TestParserResultLoi(t *testing.T) {
	cases := map[string]struct {
		line string
		want []string
	}{
		"hết lượt": {
			`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":12,"errors":["Reached maximum number of turns (12)"]}`,
			[]string{"error_max_turns", "maximum number of turns"},
		},
		"success mà is_error": {
			`{"type":"result","subtype":"success","is_error":true,"result":"API Error: 529 overloaded"}`,
			[]string{"success", "529"},
		},
		"errors là đối tượng": {
			`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":[{"message":"boom"}]}`,
			[]string{"error_during_execution", "boom"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var p Parser
			ev := p.Line([]byte(c.line))
			if len(ev) != 1 || ev[0].Kind != ai.EventError {
				t.Fatalf("Line = %+v, muốn một EventError", ev)
			}
			for _, w := range c.want {
				if !strings.Contains(ev[0].Text, w) {
					t.Errorf("Text %q thiếu %q", ev[0].Text, w)
				}
			}
		})
	}
}

// Kết quả công cụ dài bị cắt theo ký tự (rune), không cắt đôi chữ có dấu; plan_id
// vẫn lấy từ bản đầy đủ.
func TestParserCatKetQua(t *testing.T) {
	long := `{"plan_id":"p9","x":"` + strings.Repeat("ệ", 3000) + `"}`
	line := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":` + quoteJSON(long) + `}]}}`
	var p Parser
	ev := p.Line([]byte(line))
	if len(ev) != 1 {
		t.Fatalf("Line = %+v", ev)
	}
	if n := utf8.RuneCountInString(ev[0].Text); n != 2000 {
		t.Errorf("Text dài %d ký tự, muốn 2000", n)
	}
	if !utf8.ValidString(ev[0].Text) {
		t.Error("Text không phải UTF-8 hợp lệ")
	}
	if ev[0].PlanID != "p9" {
		t.Errorf("PlanID = %q, muốn p9", ev[0].PlanID)
	}
}

func quoteJSON(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
