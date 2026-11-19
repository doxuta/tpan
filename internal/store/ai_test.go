package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func aiSessionSample(id string) AISession {
	return AISession{
		ID:           id,
		Entry:        EntryMCP,
		Client:       "claude-code 2.0",
		Question:     "vì sao site chậm?",
		Context:      json.RawMessage(`{"screen":"sites","site":"shop.example.com"}`),
		Aliases:      map[string]string{"site_1": "shop.example.com", "ip_1": "203.0.113.7"},
		Untrusted:    true,
		BrainSession: "11111111-2222-4333-8444-555555555555",
		Created:      t0,
		Last:         t0.Add(time.Minute),
	}
}

func TestAISessionRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := aiSessionSample("s1")
	if err := s.CreateAISession(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.AISession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("đọc ra %+v\nmuốn    %+v", got, in)
	}
}

func TestCreateAISessionAliasesNilGhiLaObjectRong(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := AISession{ID: "s1", Entry: EntryPanel, Created: t0, Last: t0}
	if err := s.CreateAISession(ctx, in); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT aliases FROM ai_sessions WHERE id = 's1'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "{}" {
		t.Errorf("cột aliases = %q, muốn {}", raw)
	}
	got, _ := s.AISession(ctx, "s1")
	if got.Aliases == nil || len(got.Aliases) != 0 {
		t.Errorf("Aliases = %#v, muốn map rỗng khác nil", got.Aliases)
	}
	if got.Context != nil {
		t.Errorf("Context = %s, muốn nil khi chưa có", got.Context)
	}
}

func TestCreateAISessionMacDinhThoiGianVaTuChoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	s.now = func() time.Time { return t0 }
	if err := s.CreateAISession(ctx, AISession{ID: "s1", Entry: EntryMCP}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AISession(ctx, "s1")
	if !got.Created.Equal(t0) || !got.Last.Equal(t0) {
		t.Errorf("Created=%v Last=%v, muốn cùng %v", got.Created, got.Last, t0)
	}
	bad := map[string]AISession{
		"trùng id":     {ID: "s1", Entry: EntryMCP},
		"entry lạ":     {ID: "s2", Entry: "web"},
		"context hỏng": {ID: "s3", Entry: EntryMCP, Context: json.RawMessage("{x")},
	}
	for name, a := range bad {
		if err := s.CreateAISession(ctx, a); err == nil {
			t.Errorf("%s: CreateAISession không lỗi", name)
		}
	}
}

func TestAISessionKhongCoThiErrNotFound(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.AISession(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, muốn ErrNotFound", err)
	}
}

func TestSaveAISessionState(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := aiSessionSample("s1")
	in.Untrusted = false
	if err := s.CreateAISession(ctx, in); err != nil {
		t.Fatal(err)
	}
	upd := in
	upd.Aliases = map[string]string{"site_1": "shop.example.com", "site_2": "blog.example.com"}
	upd.Untrusted = true
	upd.Client = "codex 1.2"
	upd.Last = t0.Add(time.Hour)
	// Các trường không thuộc "trạng thái" không được đổi theo.
	upd.Question = "câu khác"
	upd.Created = t0.Add(-time.Hour)
	if err := s.SaveAISessionState(ctx, upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AISession(ctx, "s1")
	want := in
	want.Aliases, want.Untrusted, want.Client, want.Last = upd.Aliases, true, "codex 1.2", upd.Last
	if !reflect.DeepEqual(got, want) {
		t.Errorf("đọc ra %+v\nmuốn    %+v", got, want)
	}

	// Aliases nil ghi lại là "{}" chứ không phải "null".
	upd.Aliases = nil
	if err := s.SaveAISessionState(ctx, upd); err != nil {
		t.Fatal(err)
	}
	if got, err := s.AISession(ctx, "s1"); err != nil || len(got.Aliases) != 0 {
		t.Errorf("sau khi ghi Aliases nil: %v, err %v", got.Aliases, err)
	}

	upd.ID = "nope"
	if err := s.SaveAISessionState(ctx, upd); !errors.Is(err, ErrNotFound) {
		t.Errorf("phiên không có: err = %v, muốn ErrNotFound", err)
	}
}

func TestAICallsTheoSeqTangDan(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2"} {
		if err := s.CreateAISession(ctx, AISession{ID: id, Entry: EntryMCP, Created: t0, Last: t0}); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(sess string, seq int) AICall {
		return AICall{
			SessionID: sess, Seq: seq, Time: t0.Add(time.Duration(seq) * time.Second),
			Tool:   "site_list",
			Input:  json.RawMessage(`{"site":"site_1"}`),
			Output: json.RawMessage(`{"sites":["site_1"]}`),
		}
	}
	// Chèn lộn thứ tự: kết quả vẫn phải theo Seq.
	for _, c := range []AICall{mk("s1", 2), mk("s1", 1), mk("s2", 1), mk("s1", 10)} {
		if err := s.AddAICall(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.AICalls(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for _, c := range got {
		seqs = append(seqs, c.Seq)
	}
	if want := []int{1, 2, 10}; !reflect.DeepEqual(seqs, want) {
		t.Errorf("Seq = %v, muốn %v (10 phải sau 2, không xếp theo chuỗi)", seqs, want)
	}
	if !reflect.DeepEqual(got[0], mk("s1", 1)) {
		t.Errorf("đọc ra %+v\nmuốn    %+v", got[0], mk("s1", 1))
	}
	if got, err := s.AICalls(ctx, "khong-co"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("phiên không có: %#v, err %v; muốn slice rỗng khác nil", got, err)
	}
}

func TestAddAICallLoiVaTruongTuyChon(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreateAISession(ctx, AISession{ID: "s1", Entry: EntryMCP, Created: t0, Last: t0}); err != nil {
		t.Fatal(err)
	}
	// Lần gọi lỗi, không có đối số, đề xuất một Plan.
	c := AICall{SessionID: "s1", Seq: 1, Time: t0, Tool: "plan_propose", Error: "domain không hợp lệ", PlanID: "p1"}
	if err := s.AddAICall(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AICalls(ctx, "s1")
	if len(got) != 1 || got[0].Error != c.Error || got[0].PlanID != "p1" {
		t.Fatalf("đọc ra %+v", got)
	}
	// Input/Output rỗng lưu thành JSON null để Marshal lại không vỡ.
	if string(got[0].Input) != "null" || string(got[0].Output) != "null" {
		t.Errorf("Input=%s Output=%s, muốn null", got[0].Input, got[0].Output)
	}
	if _, err := json.Marshal(got[0]); err != nil {
		t.Errorf("Marshal AICall đọc ra: %v", err)
	}

	bad := map[string]AICall{
		"trùng seq":      {SessionID: "s1", Seq: 1, Time: t0, Tool: "x"},
		"phiên không có": {SessionID: "nope", Seq: 1, Time: t0, Tool: "x"},
		"input hỏng":     {SessionID: "s1", Seq: 2, Time: t0, Tool: "x", Input: json.RawMessage("{x")},
		"output hỏng":    {SessionID: "s1", Seq: 3, Time: t0, Tool: "x", Output: json.RawMessage("nope")},
	}
	for name, call := range bad {
		if err := s.AddAICall(ctx, call); err == nil {
			t.Errorf("%s: AddAICall không lỗi", name)
		}
	}
}
