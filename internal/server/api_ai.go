package server

// API của AI trong panel (M8, mục 8.4 và 9 của docs/KE-HOACH.md). Bảng đường
// dẫn và mã lỗi: ai_types.go. Mọi việc thật (dựng khối ngữ cảnh, che, gửi bộ
// não, lưu, phát lại) nằm ở ai.Panel; file này chỉ đọc request, gọi Panel và
// đổi lời từ chối thành mã HTTP.
//
// Hai nhóm đường dẫn:
//   - cần AI bật (preview, ask): Deps.AI nil thì trả 409 ai_off kèm lệnh bật;
//   - chỉ đọc lịch sử (sessions, session, luồng sự kiện của một lượt) và ghi
//     lần chuyển mục (views): chạy cả khi AI tắt, vì phiên của agent nối thẳng
//     qua `tpan mcp` vẫn cần xem lại được, và công cụ current_view của agent
//     đó vẫn cần biết người đang ở mục nào.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
)

const (
	// apiAISessionsDefault, apiAISessionsMax: ?limit của GET /ai/sessions.
	apiAISessionsDefault = 50
	apiAISessionsMax     = 200
	// apiAIViewPathMax, apiAIViewLabelMax: giới hạn của một lần chuyển mục
	// (rune). Đường dẫn của panel ngắn; chặn để trình duyệt (hay ai cầm phiên)
	// không đổ được chuỗi khổng lồ vào bảng ai_views rồi vào prompt.
	apiAIViewPathMax  = 512
	apiAIViewLabelMax = 200
)

// Các chế độ của bộ não (BrainStatus.Mode, Deps.BrainMode).
const (
	BrainModeOff    = "off"
	BrainModeBridge = "bridge"
	BrainModeDemo   = "demo"
)

// aiPanel trả Panel để đọc lịch sử. AI tắt thì dựng một Panel chỉ có Store
// (không Brain): Session và Subscribe chỉ đọc DB nên vẫn chạy; Ask của nó sẽ
// từ chối ai_off nhưng các handler cần AI không bao giờ dùng bản này.
func (s *Server) aiPanel() *ai.Panel {
	if s.deps.AI != nil {
		return s.deps.AI
	}
	return &ai.Panel{Store: s.deps.Store, Hostname: s.deps.Hostname}
}

// aiErrorStatus là apiErrorStatus cộng các mã của AI (hợp đồng ở ai_types.go):
// câu hỏi rỗng hoặc dài là lỗi đầu vào (400), phiên không có là 404, chạm trần
// số câu hỏi trong ngày là 429. Các lời từ chối khác (ai_turn_running,
// brain_offline...) giữ 409 như mọi lời từ chối.
func aiErrorStatus(err error) (int, ErrorBody) {
	status, body := apiErrorStatus(err)
	var re *core.RefusedError
	if errors.As(err, &re) {
		switch re.Code {
		case "question_empty", "question_too_long":
			status = http.StatusBadRequest
		case "ai_session_not_found":
			status = http.StatusNotFound
		case "ai_daily_limit":
			status = http.StatusTooManyRequests
		}
	}
	return status, body
}

func aiFail(w http.ResponseWriter, r *http.Request, err error) {
	status, body := aiErrorStatus(err)
	if status == http.StatusInternalServerError {
		apiFail(w, r, err) // apiFail ghi log lỗi 500 rồi trả cùng thân
		return
	}
	writeJSON(w, status, body)
}

// aiOff: AI đang tắt. Lời nói luôn cách bật, vì người thấy lỗi này là người
// vừa bấm "Hỏi AI" và chưa biết AI cần bật ở đâu.
func aiOff(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "ai_off", i18n.T(
		"AI đang tắt. Bật bằng lệnh sudo tpan mcp config --brain bridge trên máy chủ, rồi khởi động lại tpan: sudo systemctl restart tpan",
		"AI is turned off. Turn it on with sudo tpan mcp config --brain bridge on the server, then restart tpan: sudo systemctl restart tpan"))
}

// handleAIBrain: GET /api/v1/ai/brain.
func (s *Server) handleAIBrain(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.aiBrainStatus())
}

func (s *Server) aiBrainStatus() BrainStatus {
	mode := s.deps.BrainMode
	if mode == "" {
		mode = BrainModeOff
	}
	// Giữ nguyên chữ <user>: panel không biết người vận hành SSH vào bằng user
	// nào, họ tự thay bằng user của mình.
	out := BrainStatus{Mode: mode, ConnectCommand: "tpan brain <user>@" + s.deps.Hostname}
	switch mode {
	case BrainModeDemo:
		out.Connected, out.Host, out.Model = true, "demo", ai.DefaultModel
	case BrainModeBridge:
		if s.deps.Brain == nil {
			return out
		}
		st := s.deps.Brain.Status()
		if !st.Connected {
			return out
		}
		h := st.Hello
		since := st.Since
		out.Connected, out.Host, out.Model, out.AuthMethod = true, h.Host, h.Model, h.AuthMethod
		out.ClaudeVersion, out.TestedVersion, out.Since = h.ClaudeVersion, h.TestedVersion, &since
		// Cảnh báo chỉ khi đã nối: chưa nối thì mọi trường rỗng, so ra khác
		// nhau mà không nói lên điều gì.
		if h.ClaudeVersion != h.TestedVersion {
			out.Warnings = append(out.Warnings, i18n.T(
				fmt.Sprintf("Claude Code trên máy %s là bản %s, khác bản %s mà TPAN đã chạy bài kiểm. Bản khác có thể đổi cách khởi động; nếu cửa kiểm từ chối phiên, đưa Claude Code về bản %s.", h.Host, orUnknown(h.ClaudeVersion, "vi"), h.TestedVersion, h.TestedVersion),
				fmt.Sprintf("Claude Code on %s is version %s, not version %s that TPAN was tested with. Another version may start differently; if the gate refuses the session, go back to version %s.", h.Host, orUnknown(h.ClaudeVersion, "en"), h.TestedVersion, h.TestedVersion)))
		}
		if h.AuthMethod != "claude.ai" {
			out.Warnings = append(out.Warnings, i18n.T(
				fmt.Sprintf("Claude Code trên máy %s đăng nhập bằng %s, không phải gói Claude (claude.ai): mỗi câu hỏi tính tiền API.", h.Host, orUnknown(h.AuthMethod, "vi")),
				fmt.Sprintf("Claude Code on %s is signed in with %s, not a Claude plan (claude.ai): every question is billed to the API.", h.Host, orUnknown(h.AuthMethod, "en"))))
		}
	}
	return out
}

func orUnknown(v, lang string) string {
	if v != "" {
		return v
	}
	if lang == "en" {
		return "(unknown)"
	}
	return "(không rõ)"
}

// handleAIPreview: POST /api/v1/ai/preview. Không ghi gì nên không ghi nhật ký.
func (s *Server) handleAIPreview(w http.ResponseWriter, r *http.Request) {
	var in ai.AskInput
	if !apiReadJSON(w, r, &in) {
		return
	}
	if s.deps.AI == nil {
		aiOff(w)
		return
	}
	pv, err := s.deps.AI.Preview(r.Context(), in)
	if err != nil {
		aiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// handleAIAsk: POST /api/v1/ai/ask. Nhật ký ghi id phiên, không ghi câu hỏi:
// câu hỏi có thể chứa tên thật, và nó đã được lưu (bản che) ở ai_turns.
func (s *Server) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	var in ai.AskInput
	if !apiReadJSON(w, r, &in) {
		return
	}
	if s.deps.AI == nil {
		aiOff(w)
		return
	}
	started, err := s.deps.AI.Ask(r.Context(), in)
	if err != nil {
		// Target rỗng: SessionID ở đây là chữ trình duyệt gửi, chưa được kiểm.
		_, body := aiErrorStatus(err)
		s.audit(r, "ai.ask", "", body.Error.Code, "")
		aiFail(w, r, err)
		return
	}
	s.audit(r, "ai.ask", started.SessionID, "ok", "")
	writeJSON(w, http.StatusAccepted, started)
}

// handleAISessions: GET /api/v1/ai/sessions[?limit=N], mới dùng nhất trước.
func (s *Server) handleAISessions(w http.ResponseWriter, r *http.Request) {
	limit := apiAISessionsDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > apiAISessionsMax {
			writeError(w, http.StatusBadRequest, "limit_invalid", i18n.T(
				fmt.Sprintf("limit phải là số từ 1 tới %d.", apiAISessionsMax),
				fmt.Sprintf("limit must be a number from 1 to %d.", apiAISessionsMax)))
			return
		}
		limit = n
	}
	list, err := s.deps.Store.AISessions(r.Context(), limit)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiNonNil(list))
}

func aiSessionNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "ai_session_not_found", i18n.T("Không có phiên AI này.", "No such AI session."))
}

// handleAISession: GET /api/v1/ai/sessions/{id}.
func (s *Server) handleAISession(w http.ResponseWriter, r *http.Request) {
	d, err := s.aiPanel().Session(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		// Session trả ErrNotFound trần; qua apiErrorStatus thì thành mã
		// not_found chung, trong khi giao diện chờ ai_session_not_found.
		aiSessionNotFound(w)
		return
	}
	if err != nil {
		apiFail(w, r, err)
		return
	}
	// Lượt đang chạy mà bộ não chưa gửi gì có Events nil: ra JSON là [] như
	// kiểu TypeScript (AIEvent[]) hứa, không phải null.
	for i := range d.Turns {
		d.Turns[i].Events = apiNonNil(d.Turns[i].Events)
	}
	writeJSON(w, http.StatusOK, d)
}

// handleAIView: POST /api/v1/ai/views. Ghi cả khi AI tắt: agent nối thẳng qua
// `tpan mcp` cũng dùng công cụ current_view.
func (s *Server) handleAIView(w http.ResponseWriter, r *http.Request) {
	var v ai.View
	if !apiReadJSON(w, r, &v) {
		return
	}
	if !strings.HasPrefix(v.Path, "/") || utf8.RuneCountInString(v.Path) > apiAIViewPathMax {
		writeError(w, http.StatusBadRequest, "path_invalid", i18n.T(
			fmt.Sprintf("path phải bắt đầu bằng / và dài tối đa %d ký tự.", apiAIViewPathMax),
			fmt.Sprintf("path must start with / and be at most %d characters.", apiAIViewPathMax)))
		return
	}
	if strings.TrimSpace(v.Label) == "" || utf8.RuneCountInString(v.Label) > apiAIViewLabelMax {
		writeError(w, http.StatusBadRequest, "label_invalid", i18n.T(
			fmt.Sprintf("label không được rỗng và dài tối đa %d ký tự.", apiAIViewLabelMax),
			fmt.Sprintf("label must not be empty and be at most %d characters.", apiAIViewLabelMax)))
		return
	}
	if err := s.deps.Store.AddAIView(r.Context(), store.AIView{Time: s.now(), Path: v.Path, Label: v.Label}); err != nil {
		apiFail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAITurnEvents: GET /api/v1/ai/sessions/{id}/turns/{seq}/events (SSE).
//
// Mỗi sự kiện là "event: ai" và "data: <ai.Event JSON>". Không có "id:": sự
// kiện text và done mang toàn bộ câu trả lời tới lúc đó, nên nối lại thì gửi
// lại từ đầu là đủ (giao diện bỏ trùng các sự kiện còn lại). Luồng đóng ngay
// sau sự kiện kết thúc; giao diện thấy sự kiện đó thì tự đóng EventSource.
//
// Kênh live của Panel đóng mà chưa có sự kiện kết thúc nghĩa là người xem này
// chậm và bị Panel bỏ (để không làm kẹt lượt): chỉ đóng luồng, không giả sự
// kiện kết thúc, để EventSource tự nối lại và nhận lại từ đầu.
func (s *Server) handleAITurnEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seq < 1 {
		aiTurnNotFound(w)
		return
	}
	p := s.aiPanel()
	past, live, cancel, ok, err := p.Subscribe(ctx, id, seq)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	defer cancel()
	if !ok {
		aiTurnNotFound(w)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	// Gửi header ngay để EventSource ở trình duyệt chuyển sang "open".
	if err := rc.Flush(); err != nil {
		return
	}
	send := func(ev ai.Event) error {
		data, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("mã hoá sự kiện AI %s#%d: %w", id, seq, err)
		}
		if _, err := fmt.Fprintf(w, "event: ai\ndata: %s\n\n", data); err != nil {
			return err
		}
		return rc.Flush()
	}

	for _, ev := range past {
		if send(ev) != nil || ev.Final() {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-live:
			if !open {
				if final, ok := s.aiRecoveredFinal(ctx, id, seq); ok {
					_ = send(final)
				}
				return
			}
			if send(ev) != nil || ev.Final() {
				return
			}
		}
	}
}

// aiRecoveredFinal dựng sự kiện kết thúc cho lượt đã đóng trong DB mà các sự
// kiện đã lưu không có sự kiện kết thúc. Đó là lượt Recover đóng hộ lúc serve
// khởi động lại: tiến trình cũ chết giữa lượt, Recover chỉ đổi status và ghi
// lời lỗi. Không gửi sự kiện này thì EventSource nối lại mãi (mỗi lần chỉ nhận
// phần đã lưu rồi luồng đóng). Đây không phải "giả" sự kiện cuối: lượt đã kết
// thúc thật, chỉ là kết thúc được ghi ở cột status thay vì cột events. Lượt
// còn running (người xem chậm bị bỏ) hoặc đã có sự kiện kết thúc thì false.
func (s *Server) aiRecoveredFinal(ctx context.Context, id string, seq int) (ai.Event, bool) {
	t, err := s.deps.Store.AITurn(ctx, id, seq)
	if err != nil || (t.Status != store.TurnError && t.Status != store.TurnRefused) {
		return ai.Event{}, false
	}
	var evs []ai.Event
	if len(t.Events) > 0 && json.Unmarshal(t.Events, &evs) != nil {
		return ai.Event{}, false
	}
	if slices.ContainsFunc(evs, ai.Event.Final) {
		return ai.Event{}, false
	}
	kind := ai.EventError
	if t.Status == store.TurnRefused {
		kind = ai.EventRefused
	}
	return ai.Event{Kind: kind, Time: t.Ended, Text: t.Error}, true
}

func aiTurnNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "ai_turn_not_found", i18n.T("Không có lượt hỏi này.", "No such question in this AI session."))
}
