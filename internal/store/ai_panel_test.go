package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// panelSession tạo một phiên panel để các lượt có chỗ bám (khoá ngoại).
func panelSession(t *testing.T, s *Store, id string, last time.Time) {
	t.Helper()
	err := s.CreateAISession(context.Background(), AISession{
		ID: id, Entry: EntryPanel, Question: "vì sao chậm?",
		BrainSession: "11111111-2222-4333-8444-555555555555", Created: t0, Last: last,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func turnSample(session string, seq int, started time.Time) AITurn {
	return AITurn{
		SessionID: session, Seq: seq, Lang: "vi", Question: "site chậm?",
		Context:  json.RawMessage(`[{"kind":"view","label":"Sites"}]`),
		Playbook: "site", Prompt: "toàn văn prompt",
		Status: TurnRunning, Started: started,
	}
}

func TestBrainSessionGhiVaDoc(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	got, err := s.AISession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrainSession != "11111111-2222-4333-8444-555555555555" {
		t.Errorf("BrainSession = %q", got.BrainSession)
	}
	// Phiên mcp không có brain_session: rỗng.
	if err := s.CreateAISession(ctx, AISession{ID: "m1", Entry: EntryMCP, Created: t0}); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.AISession(ctx, "m1"); m.BrainSession != "" {
		t.Errorf("phiên mcp có BrainSession = %q", m.BrainSession)
	}
}

func TestAITurnTaoKetThucVaDoc(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	in := turnSample("s1", 1, t0)
	if err := s.CreateAITurn(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.AITurn(ctx, "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	// Events rỗng ghi "[]"; Ended rỗng khi còn chạy.
	in.Events = json.RawMessage("[]")
	if !reflect.DeepEqual(got, in) {
		t.Errorf("đọc ra %+v\nmuốn    %+v", got, in)
	}
	if !got.Ended.IsZero() {
		t.Errorf("lượt đang chạy có Ended = %v", got.Ended)
	}

	end := TurnEnd{
		Status: TurnDone, Answer: "do pool PHP", Events: json.RawMessage(`[{"kind":"done"}]`),
		At: t0.Add(3 * time.Second),
	}
	if err := s.FinishAITurn(ctx, "s1", 1, end); err != nil {
		t.Fatal(err)
	}
	got, _ = s.AITurn(ctx, "s1", 1)
	if got.Status != TurnDone || got.Answer != "do pool PHP" || string(got.Events) != `[{"kind":"done"}]` || !got.Ended.Equal(end.At) {
		t.Errorf("sau khi kết thúc: %+v", got)
	}

	// Kết thúc lần hai: lượt không còn running; không được ghi đè câu trả lời.
	err = s.FinishAITurn(ctx, "s1", 1, TurnEnd{Status: TurnError, Error: "x", At: t0})
	if !errors.Is(err, ErrTurnNotRunning) {
		t.Errorf("kết thúc lần hai: err = %v, muốn ErrTurnNotRunning", err)
	}
	if again, _ := s.AITurn(ctx, "s1", 1); again.Status != TurnDone || again.Answer != "do pool PHP" {
		t.Errorf("kết thúc lần hai làm đổi lượt: %+v", again)
	}

	// Không có lượt: ErrNotFound, cả ở FinishAITurn lẫn AITurn.
	if err := s.FinishAITurn(ctx, "s1", 9, TurnEnd{Status: TurnDone}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Finish lượt không có: err = %v", err)
	}
	if _, err := s.AITurn(ctx, "s1", 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("AITurn lượt không có: err = %v", err)
	}
}

func TestFinishAITurnTuChoiTrangThaiLa(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	if err := s.CreateAITurn(ctx, turnSample("s1", 1, t0)); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"", TurnRunning, "xong"} {
		if err := s.FinishAITurn(ctx, "s1", 1, TurnEnd{Status: st}); err == nil {
			t.Errorf("Finish với status %q không lỗi", st)
		}
	}
	if err := s.FinishAITurn(ctx, "s1", 1, TurnEnd{Status: TurnRefused, Error: "plugin lạ", Events: json.RawMessage("{x")}); err == nil {
		t.Error("Finish với events hỏng không lỗi")
	}
	if got, _ := s.AITurn(ctx, "s1", 1); got.Status != TurnRunning {
		t.Errorf("lượt bị đổi dù Finish lỗi: %+v", got)
	}
}

func TestCreateAITurnTuChoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	if err := s.CreateAITurn(ctx, turnSample("s1", 1, t0)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]AITurn{
		"trùng (phiên, seq)": turnSample("s1", 1, t0),
		"phiên không có":     turnSample("nope", 1, t0),
		"lang lạ":            func() AITurn { a := turnSample("s1", 2, t0); a.Lang = "fr"; return a }(),
		"context hỏng":       func() AITurn { a := turnSample("s1", 3, t0); a.Context = json.RawMessage("{x"); return a }(),
		"events hỏng":        func() AITurn { a := turnSample("s1", 4, t0); a.Events = json.RawMessage("{x"); return a }(),
		"status lạ":          func() AITurn { a := turnSample("s1", 5, t0); a.Status = "xong"; return a }(),
	}
	for name, a := range bad {
		if err := s.CreateAITurn(ctx, a); err == nil {
			t.Errorf("%s: CreateAITurn không lỗi", name)
		}
	}
	// Mặc định: Status rỗng là running, Context rỗng là "[]", Started rỗng là bây giờ.
	s.now = func() time.Time { return t0 }
	if err := s.CreateAITurn(ctx, AITurn{SessionID: "s1", Seq: 7, Lang: "en", Question: "q", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AITurn(ctx, "s1", 7)
	if got.Status != TurnRunning || string(got.Context) != "[]" || !got.Started.Equal(t0) {
		t.Errorf("mặc định sai: %+v", got)
	}
}

func TestAITurnsTheoSeqTang(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	panelSession(t, s, "s2", t0)
	for _, seq := range []int{3, 1, 2} {
		if err := s.CreateAITurn(ctx, turnSample("s1", seq, t0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateAITurn(ctx, turnSample("s2", 1, t0)); err != nil {
		t.Fatal(err)
	}
	got, err := s.AITurns(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Seq != 1 || got[1].Seq != 2 || got[2].Seq != 3 {
		t.Errorf("AITurns = %+v, muốn seq 1 2 3 của s1", got)
	}
	none, err := s.AITurns(ctx, "khong-co")
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("phiên không có lượt: %#v, %v; muốn slice rỗng khác nil", none, err)
	}
}

func TestCountAITurnsSince(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	// Lượt ở t0-25h, t0-1h, t0, t0+1s; mốc đếm t0-24h: lấy ba lượt sau. Mốc bằng
	// đúng thời điểm bắt đầu thì tính (>=).
	for i, d := range []time.Duration{-25 * time.Hour, -time.Hour, 0, time.Second} {
		if err := s.CreateAITurn(ctx, turnSample("s1", i+1, t0.Add(d))); err != nil {
			t.Fatal(err)
		}
	}
	for since, want := range map[time.Time]int{
		t0.Add(-24 * time.Hour): 3,
		t0:                      2,
		t0.Add(time.Second):     1,
		t0.Add(time.Minute):     0,
		t0.Add(-48 * time.Hour): 4,
	} {
		got, err := s.CountAITurnsSince(ctx, since)
		if err != nil || got != want {
			t.Errorf("CountAITurnsSince(%v) = %d, %v; muốn %d", since, got, err, want)
		}
	}
}

func TestFailRunningAITurns(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	for seq := 1; seq <= 3; seq++ {
		if err := s.CreateAITurn(ctx, turnSample("s1", seq, t0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.FinishAITurn(ctx, "s1", 1, TurnEnd{Status: TurnDone, Answer: "ok", At: t0}); err != nil {
		t.Fatal(err)
	}
	at := t0.Add(time.Hour)
	n, err := s.FailRunningAITurns(ctx, "serve khởi động lại", at)
	if err != nil || n != 2 {
		t.Fatalf("FailRunningAITurns = %d, %v; muốn 2", n, err)
	}
	for _, seq := range []int{2, 3} {
		got, _ := s.AITurn(ctx, "s1", seq)
		if got.Status != TurnError || got.Error != "serve khởi động lại" || !got.Ended.Equal(at) {
			t.Errorf("lượt %d: %+v", seq, got)
		}
	}
	if done, _ := s.AITurn(ctx, "s1", 1); done.Status != TurnDone || done.Answer != "ok" {
		t.Errorf("lượt đã xong bị đụng tới: %+v", done)
	}
	if n, _ := s.FailRunningAITurns(ctx, "x", at); n != 0 {
		t.Errorf("chạy lần hai chuyển %d lượt, muốn 0", n)
	}
}

func TestAISessionsDemVaThuTu(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "cu", t0)
	panelSession(t, s, "moi", t0.Add(2*time.Hour))
	panelSession(t, s, "giua", t0.Add(time.Hour))
	if err := s.CreateAISession(ctx, AISession{ID: "mcp", Entry: EntryMCP, Created: t0, Last: t0.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	for seq := 1; seq <= 2; seq++ {
		if err := s.CreateAITurn(ctx, turnSample("moi", seq, t0)); err != nil {
			t.Fatal(err)
		}
	}
	for seq := 1; seq <= 3; seq++ {
		if err := s.AddAICall(ctx, AICall{SessionID: "moi", Seq: seq, Time: t0, Tool: "list_sites"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddAICall(ctx, AICall{SessionID: "giua", Seq: 1, Time: t0, Tool: "machine"}); err != nil {
		t.Fatal(err)
	}
	for i, sid := range []string{"moi", "giua", "giua"} {
		p := planSample(fmt.Sprintf("p%d", i), t0.Add(time.Duration(i)*time.Minute))
		p.SessionID = sid
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.AISessions(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, g := range got {
		ids = append(ids, g.ID)
	}
	if !reflect.DeepEqual(ids, []string{"moi", "giua", "cu", "mcp"}) {
		t.Fatalf("thứ tự = %v, muốn moi giua cu mcp (last_at giảm dần)", ids)
	}
	want := map[string][3]int{"moi": {2, 3, 1}, "giua": {0, 1, 2}, "cu": {0, 0, 0}, "mcp": {0, 0, 0}}
	for _, g := range got {
		if w := want[g.ID]; g.Turns != w[0] || g.Calls != w[1] || g.Plans != w[2] {
			t.Errorf("%s: turns/calls/plans = %d/%d/%d, muốn %v", g.ID, g.Turns, g.Calls, g.Plans, w)
		}
	}
	if got[0].Question != "vì sao chậm?" || got[0].BrainSession == "" || got[0].Aliases == nil {
		t.Errorf("dòng đầu thiếu trường của AISession: %+v", got[0].AISession)
	}
	if lim, _ := s.AISessions(ctx, 2); len(lim) != 2 || lim[0].ID != "moi" || lim[1].ID != "giua" {
		t.Errorf("limit 2 = %+v", lim)
	}
}

func TestPlansBySession(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	panelSession(t, s, "s1", t0)
	// Tạo lệch thứ tự so với created_at để chắc là xếp theo thời gian, cũ trước.
	for _, c := range []struct {
		id, session string
		at          time.Time
	}{
		{"p2", "s1", t0.Add(2 * time.Minute)},
		{"p1", "s1", t0.Add(time.Minute)},
		{"px", "khac", t0},
		{"p0", "", t0},
	} {
		p := planSample(c.id, c.at)
		p.SessionID = c.session
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.PlansBySession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range got {
		ids = append(ids, p.ID)
	}
	if !reflect.DeepEqual(ids, []string{"p1", "p2"}) {
		t.Errorf("PlansBySession = %v, muốn p1 p2", ids)
	}
	none, err := s.PlansBySession(ctx, "khong-co")
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("phiên không có Plan: %#v, %v", none, err)
	}
}

func TestAIViewsGiu50DongMoiNhatMoiTruoc(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for i := 1; i <= 55; i++ {
		v := AIView{Time: t0.Add(time.Duration(i) * time.Second), Path: fmt.Sprintf("/sites/%d", i), Label: fmt.Sprintf("Site %d", i)}
		if err := s.AddAIView(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.AIViews(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 50 {
		t.Fatalf("giữ %d dòng, muốn đúng 50", len(all))
	}
	if all[0].Path != "/sites/55" || all[49].Path != "/sites/6" {
		t.Errorf("đầu %q cuối %q; muốn /sites/55 ... /sites/6", all[0].Path, all[49].Path)
	}
	if all[0].Label != "Site 55" || !all[0].Time.Equal(t0.Add(55*time.Second)) || all[0].ID == 0 {
		t.Errorf("dòng đầu: %+v", all[0])
	}
	ten, _ := s.AIViews(ctx, 10)
	if len(ten) != 10 || ten[0].Path != "/sites/55" || ten[9].Path != "/sites/46" {
		t.Errorf("limit 10 sai: %+v", ten)
	}
}

func TestAddAIViewTuChoiRongVaMacDinhThoiGian(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for name, v := range map[string]AIView{
		"path rỗng":  {Path: "", Label: "Site"},
		"label rỗng": {Path: "/sites", Label: ""},
	} {
		if err := s.AddAIView(ctx, v); err == nil {
			t.Errorf("%s: AddAIView không lỗi", name)
		}
	}
	if got, _ := s.AIViews(ctx, 10); len(got) != 0 {
		t.Errorf("lần bị từ chối vẫn ghi: %+v", got)
	}
	s.now = func() time.Time { return t0 }
	if err := s.AddAIView(ctx, AIView{Path: "/", Label: "Bảng điều khiển"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AIViews(ctx, 1)
	if len(got) != 1 || !got[0].Time.Equal(t0) {
		t.Errorf("Time mặc định sai: %+v", got)
	}
}
