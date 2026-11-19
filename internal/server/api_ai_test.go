package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/ai/bridge"
	"github.com/doxuta/tpan/internal/ai/fake"
	"github.com/doxuta/tpan/internal/store"
)

// aiNewEnv là apiEnv có AI bật với bộ não giả b (nil: AI tắt).
func aiNewEnv(t *testing.T, b ai.Brain) *apiEnv {
	t.Helper()
	e := apiNewEnv(t)
	if b != nil {
		e.s.deps.AI = &ai.Panel{Store: e.st, Brain: b, Hostname: e.s.deps.Hostname, Now: e.clock}
		e.s.deps.BrainMode = BrainModeDemo
	}
	return e
}

// aiStatus là bộ não bridge soạn sẵn cho GET /ai/brain.
type aiStatus bridge.Status

func (a aiStatus) Status() bridge.Status { return bridge.Status(a) }

const aiAskBody = `{"question":"Site của mình có ổn không?","lang":"vi","view":{"path":"/sites","label":"Site"}}`

// aiStream đọc hết luồng SSE của một lượt. Handler trả về khi gửi sự kiện cuối
// (hoặc luồng đóng), nên gọi đồng bộ được; hạn 5 giây để lỗi thì không treo.
func aiStream(t *testing.T, e *apiEnv, sid, seq string) (*httptest.ResponseRecorder, []ai.Event) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, "GET", "/", nil)
	r.SetPathValue("id", sid)
	r.SetPathValue("seq", seq)
	rec := httptest.NewRecorder()
	e.s.handleAITurnEvents(rec, r)
	if ctx.Err() != nil {
		t.Fatalf("luồng SSE không đóng sau 5 giây; đã nhận: %s", rec.Body)
	}
	var evs []ai.Event
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	for _, block := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n\n") {
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		if len(lines) != 2 || lines[0] != "event: ai" || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("khối SSE sai dạng: %q", block)
		}
		var ev ai.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &ev); err != nil {
			t.Fatalf("data không phải ai.Event: %v (%s)", err, lines[1])
		}
		evs = append(evs, ev)
	}
	return rec, evs
}

func TestAIBrainStatus(t *testing.T) {
	e := apiNewEnv(t)
	get := func() BrainStatus {
		return apiDecode[BrainStatus](t, apiCall(e.s.handleAIBrain, "GET", "/", ""), 200)
	}
	const cmd = "tpan brain <user>@vps-test"

	// BrainMode rỗng: tắt.
	if b := get(); b.Mode != "off" || b.Connected || b.ConnectCommand != cmd || len(b.Warnings) != 0 {
		t.Fatalf("tắt: %+v", b)
	}

	e.s.deps.BrainMode = BrainModeDemo
	if b := get(); b.Mode != "demo" || !b.Connected || b.Host != "demo" || b.Model != "sonnet" || len(b.Warnings) != 0 {
		t.Fatalf("demo: %+v", b)
	}

	// Bridge chưa nối: không cảnh báo dù mọi trường rỗng.
	e.s.deps.BrainMode = BrainModeBridge
	e.s.deps.Brain = aiStatus{}
	if b := get(); b.Mode != "bridge" || b.Connected || b.Since != nil || len(b.Warnings) != 0 || b.ConnectCommand != cmd {
		t.Fatalf("bridge chưa nối: %+v", b)
	}

	since := time.Date(2026, 11, 19, 7, 0, 0, 0, time.UTC)
	ok := bridge.Hello{Host: "laptop", ClaudeVersion: "2.1.328", TestedVersion: "2.1.328", Model: "sonnet", AuthMethod: "claude.ai"}
	e.s.deps.Brain = aiStatus{Connected: true, Hello: ok, Since: since}
	b := get()
	if !b.Connected || b.Host != "laptop" || b.Model != "sonnet" || b.AuthMethod != "claude.ai" ||
		b.ClaudeVersion != "2.1.328" || b.TestedVersion != "2.1.328" || b.Since == nil || !b.Since.Equal(since) || len(b.Warnings) != 0 {
		t.Fatalf("bridge đã nối, đúng bản: %+v", b)
	}

	// Khác bản đã kiểm và đăng nhập bằng khoá API: hai cảnh báo, cả hai ngôn ngữ.
	bad := ok
	bad.ClaudeVersion, bad.AuthMethod = "2.2.0", "api_key"
	e.s.deps.Brain = aiStatus{Connected: true, Hello: bad, Since: since}
	b = get()
	if len(b.Warnings) != 2 {
		t.Fatalf("muốn 2 cảnh báo, có %+v", b.Warnings)
	}
	if !strings.Contains(b.Warnings[0].Vi, "2.2.0") || !strings.Contains(b.Warnings[0].En, "2.1.328") {
		t.Errorf("cảnh báo phiên bản: %+v", b.Warnings[0])
	}
	if !strings.Contains(b.Warnings[1].Vi, "tính tiền API") || !strings.Contains(b.Warnings[1].En, "api_key") {
		t.Errorf("cảnh báo đăng nhập: %+v", b.Warnings[1])
	}
}

func TestAIPreviewWritesNothing(t *testing.T) {
	e := aiNewEnv(t, &fake.Brain{})
	e.addSite(t, "shop.example.com")
	pv := apiDecode[ai.AskPreview](t, apiCall(e.s.handleAIPreview, "POST", "/", aiAskBody), 200)
	if !strings.Contains(pv.Prompt, "<question>") || len(pv.Chips) == 0 {
		t.Fatalf("preview = %+v", pv)
	}
	if strings.Contains(pv.Prompt, "shop.example.com") {
		t.Errorf("prompt lộ tên thật: %s", pv.Prompt)
	}
	ctx := context.Background()
	if list, _ := e.st.AISessions(ctx, 0); len(list) != 0 {
		t.Errorf("preview mà ghi phiên: %+v", list)
	}
	if n, _ := e.st.CountAITurnsSince(ctx, time.Time{}); n != 0 {
		t.Errorf("preview mà ghi %d lượt", n)
	}
	if a := hAudit(t, e, "ai.ask"); len(a) != 0 {
		t.Errorf("preview mà ghi nhật ký: %+v", a)
	}

	long := `{"question":"` + strings.Repeat("ư", ai.MaxQuestionLen+1) + `","lang":"vi","view":{"path":"/","label":"x"}}`
	if code := apiErrCode(t, apiCall(e.s.handleAIPreview, "POST", "/", long), 400); code != "question_too_long" {
		t.Errorf("câu dài: code = %q", code)
	}
	if code := apiErrCode(t, apiCall(e.s.handleAIPreview, "POST", "/", `{"question":"a","la":1}`), 400); code != "bad_request" {
		t.Errorf("trường lạ: code = %q", code)
	}
}

// TestAIAskStream: hỏi rồi nghe luồng tới sự kiện cuối. Delay 0 thì lượt
// thường đã xong trước khi nghe (đọc từ DB); Delay 20ms thì nghe lúc lượt
// còn chạy (từ bộ nhớ). Cả hai phải ra cùng một chuỗi sự kiện.
func TestAIAskStream(t *testing.T) {
	for _, delay := range []time.Duration{0, 20 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) {
			e := aiNewEnv(t, &fake.Brain{Delay: delay})
			e.addSite(t, "shop.example.com")
			started := apiDecode[ai.AskStarted](t, apiCall(e.s.handleAIAsk, "POST", "/", aiAskBody), 202)
			if started.SessionID == "" || started.Seq != 1 {
				t.Fatalf("ask = %+v", started)
			}

			_, evs := aiStream(t, e, started.SessionID, "1")
			if len(evs) == 0 {
				t.Fatal("không nhận sự kiện nào")
			}
			last := evs[len(evs)-1]
			if last.Kind != ai.EventDone {
				t.Fatalf("sự kiện cuối = %+v, muốn done", last)
			}
			// Bộ não giả viết bí danh site-a.example; người xem thấy tên thật.
			if !strings.Contains(last.Text, "shop.example.com") || strings.Contains(last.Text, "site-a.example") {
				t.Errorf("chữ chưa đổi về tên thật: %q", last.Text)
			}
			kinds := map[ai.EventKind]bool{}
			for _, ev := range evs {
				kinds[ev.Kind] = true
			}
			for _, k := range []ai.EventKind{ai.EventInit, ai.EventText, ai.EventToolCall, ai.EventToolResult} {
				if !kinds[k] {
					t.Errorf("thiếu sự kiện %s", k)
				}
			}

			a := hAudit(t, e, "ai.ask")
			if len(a) != 1 || a[0].Target != started.SessionID || a[0].Result != "ok" {
				t.Errorf("nhật ký = %+v", a)
			}
			if strings.Contains(a[0].Target+a[0].Result, "ổn không") {
				t.Error("nhật ký ghi câu hỏi")
			}

			list := apiDecode[[]store.AISessionSummary](t, apiCall(e.s.handleAISessions, "GET", "/api/v1/ai/sessions", ""), 200)
			if len(list) != 1 || list[0].ID != started.SessionID || list[0].Turns != 1 || list[0].Entry != store.EntryPanel {
				t.Fatalf("danh sách phiên = %+v", list)
			}
			d := apiDecode[ai.SessionDetail](t, apiCall(e.s.handleAISession, "GET", "/", "", "id", started.SessionID), 200)
			if len(d.Turns) != 1 || d.Turns[0].Status != store.TurnDone || !strings.Contains(d.Turns[0].Answer, "shop.example.com") {
				t.Fatalf("chi tiết phiên = %+v", d.Turns)
			}
		})
	}
}

func TestAIOff(t *testing.T) {
	e := aiNewEnv(t, nil)
	for _, h := range []http.HandlerFunc{e.s.handleAIPreview, e.s.handleAIAsk} {
		rec := apiCall(h, "POST", "/", aiAskBody)
		body := apiDecode[ErrorBody](t, rec, 409)
		if body.Error.Code != "ai_off" || !strings.Contains(body.Error.Message.Vi, "sudo tpan mcp config --brain bridge") {
			t.Errorf("AI tắt: %+v", body)
		}
	}
	// Lịch sử vẫn đọc được khi AI tắt: phiên mcp cũ.
	ctx := context.Background()
	if err := e.st.CreateAISession(ctx, store.AISession{ID: "mcp1", Entry: store.EntryMCP, Created: e.now}); err != nil {
		t.Fatal(err)
	}
	list := apiDecode[[]store.AISessionSummary](t, apiCall(e.s.handleAISessions, "GET", "/api/v1/ai/sessions", ""), 200)
	if len(list) != 1 || list[0].ID != "mcp1" {
		t.Fatalf("danh sách = %+v", list)
	}
	d := apiDecode[ai.SessionDetail](t, apiCall(e.s.handleAISession, "GET", "/", "", "id", "mcp1"), 200)
	if d.Session.ID != "mcp1" || d.Turns == nil || d.Calls == nil || d.Plans == nil {
		t.Fatalf("chi tiết = %+v", d)
	}
	if code := apiErrCode(t, apiCall(e.s.handleAISession, "GET", "/", "", "id", "khongco"), 404); code != "ai_session_not_found" {
		t.Errorf("phiên không có: code = %q", code)
	}
	if rec := apiCall(e.s.handleAIView, "POST", "/", `{"path":"/sites","label":"Site"}`); rec.Code != http.StatusNoContent {
		t.Errorf("views khi AI tắt: mã %d %s", rec.Code, rec.Body)
	}
}

func TestAIAskRefused(t *testing.T) {
	e := aiNewEnv(t, &fake.Brain{})
	if code := apiErrCode(t, apiCall(e.s.handleAIAsk, "POST", "/", `{"question":"   ","lang":"vi","view":{"path":"/","label":"x"}}`), 400); code != "question_empty" {
		t.Errorf("câu rỗng: code = %q", code)
	}
	if code := apiErrCode(t, apiCall(e.s.handleAIAsk, "POST", "/", `{"session_id":"khongco","question":"a","lang":"vi","view":{"path":"/","label":"x"}}`), 404); code != "ai_session_not_found" {
		t.Errorf("phiên không có: code = %q", code)
	}
	// Lời từ chối cũng vào nhật ký, với mã lỗi làm kết quả.
	if a := hAudit(t, e, "ai.ask"); len(a) != 2 || a[0].Result != "question_empty" || a[0].Target != "" {
		t.Errorf("nhật ký = %+v", a)
	}

	// Đủ MaxQuestionsPerDay lượt trong 24 giờ qua: 429.
	ctx := context.Background()
	if err := e.st.CreateAISession(ctx, store.AISession{ID: "s0", Entry: store.EntryPanel, Created: e.now}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= ai.MaxQuestionsPerDay; i++ {
		err := e.st.CreateAITurn(ctx, store.AITurn{SessionID: "s0", Seq: i, Lang: "vi", Question: "q",
			Status: store.TurnDone, Started: e.now.Add(-time.Hour), Ended: e.now.Add(-time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if code := apiErrCode(t, apiCall(e.s.handleAIAsk, "POST", "/", aiAskBody), 429); code != "ai_daily_limit" {
		t.Errorf("trần trong ngày: code = %q", code)
	}
}

func TestAISessionsLimit(t *testing.T) {
	e := apiNewEnv(t)
	for _, bad := range []string{"0", "201", "abc", "-1"} {
		if code := apiErrCode(t, apiCall(e.s.handleAISessions, "GET", "/api/v1/ai/sessions?limit="+bad, ""), 400); code != "limit_invalid" {
			t.Errorf("limit=%s: code = %q", bad, code)
		}
	}
	ctx := context.Background()
	for i, id := range []string{"a", "b", "c"} {
		if err := e.st.CreateAISession(ctx, store.AISession{ID: id, Entry: store.EntryMCP, Created: e.now.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	list := apiDecode[[]store.AISessionSummary](t, apiCall(e.s.handleAISessions, "GET", "/api/v1/ai/sessions?limit=2", ""), 200)
	if len(list) != 2 || list[0].ID != "c" {
		t.Fatalf("limit=2: %+v", list)
	}
	if got := apiCall(e.s.handleAISessions, "GET", "/api/v1/ai/sessions", "").Body.String(); strings.HasPrefix(got, "null") {
		t.Errorf("danh sách ra null: %s", got)
	}
}

func TestAIViews(t *testing.T) {
	e := apiNewEnv(t)
	if rec := apiCall(e.s.handleAIView, "POST", "/", `{"path":"/sites/shop.example.com","label":"Site shop.example.com"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("mã %d %s", rec.Code, rec.Body)
	}
	views, err := e.st.AIViews(context.Background(), 0)
	if err != nil || len(views) != 1 || views[0].Path != "/sites/shop.example.com" || !views[0].Time.Equal(e.now) {
		t.Fatalf("ai_views = %+v, %v", views, err)
	}
	for _, c := range []struct{ body, code string }{
		{`{"path":"sites","label":"x"}`, "path_invalid"},
		{`{"path":"","label":"x"}`, "path_invalid"},
		{`{"path":"/` + strings.Repeat("a", 512) + `","label":"x"}`, "path_invalid"},
		{`{"path":"/","label":""}`, "label_invalid"},
		{`{"path":"/","label":"  "}`, "label_invalid"},
		{`{"path":"/","label":"` + strings.Repeat("ư", 201) + `"}`, "label_invalid"},
		{`{"path":"/","label":"x","extra":1}`, "bad_request"},
	} {
		if code := apiErrCode(t, apiCall(e.s.handleAIView, "POST", "/", c.body), 400); code != c.code {
			t.Errorf("%.60s: code = %q, muốn %q", c.body, code, c.code)
		}
	}
	// Đúng 200 rune tiếng Việt vẫn nhận (đếm rune, không đếm byte).
	if rec := apiCall(e.s.handleAIView, "POST", "/", `{"path":"/","label":"`+strings.Repeat("ư", 200)+`"}`); rec.Code != http.StatusNoContent {
		t.Errorf("200 rune: mã %d %s", rec.Code, rec.Body)
	}
}

func TestAITurnEventsNotFound(t *testing.T) {
	e := apiNewEnv(t)
	for _, seq := range []string{"abc", "0", "99"} {
		rec, _ := aiStream(t, e, "khongco", seq)
		if code := apiErrCode(t, rec, 404); code != "ai_turn_not_found" {
			t.Errorf("seq %s: code = %q", seq, code)
		}
	}
}

// TestAITurnEventsRecovered: lượt bị Recover đóng (serve khởi động lại giữa
// lượt) không có sự kiện cuối trong cột events. Luồng phải gửi một sự kiện
// error từ lời đã lưu, không thì EventSource nối lại mãi.
func TestAITurnEventsRecovered(t *testing.T) {
	e := apiNewEnv(t)
	ctx := context.Background()
	if err := e.st.CreateAISession(ctx, store.AISession{ID: "s1", Entry: store.EntryPanel, Created: e.now}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.CreateAITurn(ctx, store.AITurn{SessionID: "s1", Seq: 1, Lang: "vi", Question: "q", Started: e.now}); err != nil {
		t.Fatal(err)
	}
	p := &ai.Panel{Store: e.st, Now: e.clock}
	if err := p.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	_, evs := aiStream(t, e, "s1", "1")
	if len(evs) != 1 || evs[0].Kind != ai.EventError || !strings.Contains(evs[0].Text, "khởi động lại") {
		t.Fatalf("sự kiện = %+v", evs)
	}
	// Lượt không có sự kiện nào: JSON là [] (AIEvent[]), không phải null.
	if body := apiCall(e.s.handleAISession, "GET", "/", "", "id", "s1").Body.String(); !strings.Contains(body, `"events":[]`) {
		t.Errorf("chi tiết phiên: %s", body)
	}
}

// TestAISessionRunningNoEvents: lượt đang chạy mà bộ não chưa gửi gì.
func TestAISessionRunningNoEvents(t *testing.T) {
	ch := make(chan ai.Event, 1)
	e := aiNewEnv(t, aiChanBrain(ch))
	started := apiDecode[ai.AskStarted](t, apiCall(e.s.handleAIAsk, "POST", "/", aiAskBody), 202)
	body := apiCall(e.s.handleAISession, "GET", "/", "", "id", started.SessionID).Body.String()
	if !strings.Contains(body, `"status":"running"`) || !strings.Contains(body, `"events":[]`) {
		t.Errorf("chi tiết phiên: %s", body)
	}
	// Kết thúc lượt và chờ nó được ghi, để goroutine của Panel không còn ghi
	// vào store sau khi test đóng store.
	ch <- ai.Event{Kind: ai.EventDone, Text: "xong"}
	close(ch)
	if _, evs := aiStream(t, e, started.SessionID, "1"); len(evs) == 0 || evs[len(evs)-1].Kind != ai.EventDone {
		t.Fatalf("sự kiện = %+v", evs)
	}
}

// aiChanBrain là bộ não mà test tự đẩy sự kiện vào.
type aiChanBrain chan ai.Event

func (b aiChanBrain) Run(context.Context, ai.Job) (<-chan ai.Event, error) { return b, nil }

// TestAIRoutes đi qua NewHandler: đường dẫn đã gắn, cần đăng nhập, POST từ
// trang khác bị chặn.
func TestAIRoutes(t *testing.T) {
	e := newAuthEnv(t)
	const ip = "192.0.2.1"
	routes := []struct{ method, path string }{
		{"GET", "/api/v1/ai/brain"},
		{"POST", "/api/v1/ai/preview"},
		{"POST", "/api/v1/ai/ask"},
		{"GET", "/api/v1/ai/sessions"},
		{"GET", "/api/v1/ai/sessions/s1"},
		{"GET", "/api/v1/ai/sessions/s1/turns/1/events"},
		{"POST", "/api/v1/ai/views"},
	}
	for _, c := range routes {
		if rec := e.req(c.method, c.path, "", ip, nil); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "unauthorized" {
			t.Errorf("%s %s chưa đăng nhập: mã %d %s, muốn 401", c.method, c.path, rec.Code, rec.Body)
		}
	}

	cookie := e.mustLogin(ip)
	for _, path := range []string{"/api/v1/ai/preview", "/api/v1/ai/ask", "/api/v1/ai/views"} {
		rec := e.req("POST", path, aiAskBody, ip, cookie, "Origin", "https://evil.example.com")
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "csrf" {
			t.Errorf("POST %s khác origin: mã %d %s, muốn 403 csrf", path, rec.Code, rec.Body)
		}
	}
	// Cùng origin, AI tắt (Deps của authEnv không có AI): tới được handler.
	if rec := e.req("POST", "/api/v1/ai/ask", aiAskBody, ip, cookie, "Sec-Fetch-Site", "same-origin"); rec.Code != http.StatusConflict || errCode(t, rec) != "ai_off" {
		t.Errorf("ask cùng origin: mã %d %s, muốn 409 ai_off", rec.Code, rec.Body)
	}
	if rec := e.req("GET", "/api/v1/ai/brain", "", ip, cookie); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mode":"off"`) {
		t.Errorf("GET /ai/brain: mã %d %s", rec.Code, rec.Body)
	}
	if rec := e.req("GET", "/api/v1/ai/sessions/khongco", "", ip, cookie); rec.Code != http.StatusNotFound || errCode(t, rec) != "ai_session_not_found" {
		t.Errorf("GET phiên không có: mã %d %s", rec.Code, rec.Body)
	}
}
