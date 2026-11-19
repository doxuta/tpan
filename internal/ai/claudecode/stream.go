package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/ai"
)

// maxResultRunes: kết quả công cụ hiện trên panel chỉ cần đủ để người đọc thấy
// AI đã thấy gì; bản đầy đủ đã nằm trong ai_calls của `tpan mcp`.
const maxResultRunes = 2000

// streamLine là các trường của một dòng stream-json mà tpan dùng. Các dạng đã
// thấy ở lần gọi claude thật số 1 (docs/bang-chung/M8-claude-init.log).
type streamLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	// stream_event
	Event *struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	// assistant, user. content để thô: tin nhắn của user có thể là một chuỗi.
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// result. errors để thô vì chưa thấy dạng thật của nó (chuỗi hay đối tượng);
	// khai sai kiểu thì cả dòng result không giải được và lượt thành "không có result".
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
	Errors  json.RawMessage `json:"errors"`
}

// contentBlock là một khối trong message.content.
type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	// Content của tool_result là chuỗi hoặc mảng các khối {"type":"text",...}.
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
	Text    string          `json:"text"`
}

// Line đổi một dòng stream-json thành 0..n ai.Event. Dòng không giải được hay
// loại không dùng tới thì trả nil. Sự kiện init do Run lo (qua Gate), ở đây bỏ qua.
func (p *Parser) Line(line []byte) []ai.Event {
	var l streamLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}
	now := time.Now()
	switch l.Type {
	case "stream_event":
		// Chỉ text_delta mới là chữ của câu trả lời. Khối text trong sự kiện
		// assistant lặp lại đúng các delta này nên không phát lần nữa.
		if l.Event != nil && l.Event.Type == "content_block_delta" &&
			l.Event.Delta.Type == "text_delta" && l.Event.Delta.Text != "" {
			return []ai.Event{{Kind: ai.EventText, Time: now, Text: l.Event.Delta.Text}}
		}
	case "assistant":
		var out []ai.Event
		for _, b := range blocks(l) {
			if b.Type != "tool_use" {
				continue
			}
			if p.tools == nil {
				p.tools = map[string]string{}
			}
			p.tools[b.ID] = b.Name
			out = append(out, ai.Event{Kind: ai.EventToolCall, Time: now, Tool: b.Name, Input: b.Input})
		}
		return out
	case "user":
		var out []ai.Event
		for _, b := range blocks(l) {
			if b.Type != "tool_result" {
				continue
			}
			full := resultText(b.Content)
			e := ai.Event{Kind: ai.EventToolResult, Time: now, Tool: p.tools[b.ToolUseID],
				Text: truncate(full, maxResultRunes), IsError: b.IsError, PlanID: planID(full)}
			delete(p.tools, b.ToolUseID)
			out = append(out, e)
		}
		return out
	case "result":
		if l.Subtype == "success" && !l.IsError {
			return []ai.Event{{Kind: ai.EventDone, Time: now, Text: l.Result}}
		}
		msg := "claude báo lỗi (" + l.Subtype + ")"
		var details []string
		if l.Result != "" {
			details = append(details, l.Result)
		}
		if e := errorsText(l.Errors); e != "" {
			details = append(details, e)
		}
		if len(details) > 0 {
			msg += ": " + strings.Join(details, "; ")
		}
		return []ai.Event{{Kind: ai.EventError, Time: now, Text: msg}}
	}
	return nil
}

// blocks giải message.content thành các khối; content là chuỗi (tin nhắn thường)
// thì không có khối nào.
func blocks(l streamLine) []contentBlock {
	if l.Message == nil {
		return nil
	}
	var bs []contentBlock
	if err := json.Unmarshal(l.Message.Content, &bs); err != nil {
		return nil
	}
	return bs
}

// resultText lấy chữ của một tool_result ở cả hai dạng: chuỗi, hoặc mảng khối
// (nối chữ của các khối text bằng dấu xuống dòng; khối khác như ảnh thì bỏ).
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var bs []contentBlock
	if json.Unmarshal(raw, &bs) != nil {
		return ""
	}
	var parts []string
	for _, b := range bs {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// planID lấy "plan_id" khi kết quả là một đối tượng JSON có trường đó (kết quả
// của các công cụ propose_*).
func planID(s string) string {
	var v struct {
		PlanID string `json:"plan_id"`
	}
	if json.Unmarshal([]byte(s), &v) != nil {
		return ""
	}
	return v.PlanID
}

// truncate cắt s còn tối đa n ký tự (rune, không cắt đôi chữ có dấu), ký tự
// cuối là "…" khi có cắt.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// errorsText in trường errors của result: mảng chuỗi thì nối lại, dạng khác thì
// in JSON gọn.
func errorsText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return ""
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil {
		return strings.Join(ss, "; ")
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return fmt.Sprintf("%q", raw)
	}
	return buf.String()
}
