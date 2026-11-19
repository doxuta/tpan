package fake

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/ai"
)

// collect đọc kênh tới khi đóng, có hạn chót để test hỏng thì báo chứ không treo.
func collect(t *testing.T, ch <-chan ai.Event) []ai.Event {
	t.Helper()
	var got []ai.Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("kênh không đóng sau 5 giây; đã nhận %d sự kiện", len(got))
		}
	}
}

func kinds(evs []ai.Event) []ai.EventKind {
	out := make([]ai.EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestDemoScriptTheoNgonNgu(t *testing.T) {
	for lang, marker := range map[string]string{"vi": "bộ não giả", "en": "fake brain"} {
		evs := DemoScript(ai.Job{Lang: lang})
		if evs[0].Kind != ai.EventInit || evs[0].Model != "sonnet" || evs[0].Version == "" {
			t.Errorf("%s: sự kiện đầu %+v, muốn init có Version và Model sonnet", lang, evs[0])
		}
		last := evs[len(evs)-1]
		if last.Kind != ai.EventDone {
			t.Fatalf("%s: sự kiện cuối là %s, muốn done", lang, last.Kind)
		}
		for _, want := range []string{marker, "site-a.example", "tpan:site/site-a.example"} {
			if !strings.Contains(last.Text, want) {
				t.Errorf("%s: câu trả lời cuối thiếu %q:\n%s", lang, want, last.Text)
			}
		}
		// Câu cuối phải đúng bằng các đoạn chữ ghép lại (đúng hợp đồng EventDone).
		var joined strings.Builder
		var call, result int
		for _, e := range evs {
			switch e.Kind {
			case ai.EventText:
				joined.WriteString(e.Text)
			case ai.EventToolCall:
				call++
				if e.Tool != "mcp__tpan__list_sites" {
					t.Errorf("%s: tool_call %q", lang, e.Tool)
				}
			case ai.EventToolResult:
				result++
			}
		}
		if joined.String() != last.Text {
			t.Errorf("%s: done.Text khác phần chữ ghép lại", lang)
		}
		if call != 1 || result != 1 {
			t.Errorf("%s: %d tool_call, %d tool_result; muốn 1 và 1", lang, call, result)
		}
		var final int
		for _, e := range evs {
			if e.Final() {
				final++
			}
		}
		if final != 1 {
			t.Errorf("%s: %d sự kiện kết thúc, muốn 1", lang, final)
		}
	}
	// Hợp đồng chỉ có vi và en; Lang rỗng rơi về tiếng Việt (ngôn ngữ mặc định).
	empty := DemoScript(ai.Job{})
	if !strings.Contains(empty[len(empty)-1].Text, "bộ não giả") {
		t.Error("Lang rỗng không rơi về tiếng Việt")
	}
}

func TestRunMacDinhLaDemo(t *testing.T) {
	b := &Brain{}
	ch, err := b.Run(context.Background(), ai.Job{ID: "s-1", Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	got := collect(t, ch)
	want := DemoScript(ai.Job{Lang: "en"})
	if len(got) != len(want) {
		t.Fatalf("nhận %d sự kiện, muốn %d: %v", len(got), len(want), kinds(got))
	}
	for i := range got {
		if got[i].Kind != want[i].Kind || got[i].Text != want[i].Text {
			t.Errorf("sự kiện %d = %s %q, muốn %s %q", i, got[i].Kind, got[i].Text, want[i].Kind, want[i].Text)
		}
		if got[i].Time.IsZero() {
			t.Errorf("sự kiện %d chưa có Time", i)
		}
	}
}

func TestRunTuThemDoneKhiKichBanThieu(t *testing.T) {
	b := &Brain{Script: func(job ai.Job) []ai.Event {
		return []ai.Event{
			{Kind: ai.EventText, Text: "xin "},
			{Kind: ai.EventText, Text: "chào " + job.Lang},
		}
	}}
	ch, _ := b.Run(context.Background(), ai.Job{Lang: "vi"})
	got := collect(t, ch)
	if len(got) != 3 || got[2].Kind != ai.EventDone || got[2].Text != "xin chào vi" {
		t.Errorf("sự kiện = %+v, muốn text, text, done \"xin chào vi\"", got)
	}
	// Kịch bản rỗng vẫn kết thúc bằng một done.
	empty := &Brain{Script: func(ai.Job) []ai.Event { return nil }}
	ch, _ = empty.Run(context.Background(), ai.Job{})
	if got := collect(t, ch); len(got) != 1 || got[0].Kind != ai.EventDone {
		t.Errorf("kịch bản rỗng: %+v", got)
	}
}

func TestRunDungSauSuKienKetThucDauTien(t *testing.T) {
	b := &Brain{Script: func(ai.Job) []ai.Event {
		return []ai.Event{
			{Kind: ai.EventText, Text: "a"},
			{Kind: ai.EventRefused, Text: "plugin lạ"},
			{Kind: ai.EventText, Text: "không được hiện"},
			{Kind: ai.EventDone},
		}
	}}
	ch, _ := b.Run(context.Background(), ai.Job{})
	got := collect(t, ch)
	if k := kinds(got); len(k) != 2 || k[1] != ai.EventRefused {
		t.Errorf("sự kiện = %v, muốn text rồi refused và hết", k)
	}
}

func TestRunGiuNguyenThoiGianCuaKichBan(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b := &Brain{Script: func(ai.Job) []ai.Event {
		return []ai.Event{{Kind: ai.EventText, Text: "a", Time: at}}
	}}
	ch, _ := b.Run(context.Background(), ai.Job{})
	if got := collect(t, ch); !got[0].Time.Equal(at) {
		t.Errorf("Time = %v, muốn giữ %v", got[0].Time, at)
	}
}

func TestRunHuyGiuaChungPhatError(t *testing.T) {
	// Delay một giờ: chỉ ctx huỷ mới đưa test ra khỏi chờ.
	b := &Brain{Delay: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := b.Run(ctx, ai.Job{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	got := collect(t, ch)
	if len(got) != 1 || got[0].Kind != ai.EventError || !strings.Contains(got[0].Text, "context canceled") {
		t.Errorf("sau khi huỷ: %+v, muốn đúng một error", got)
	}
}

func TestRunCtxDaHuyTruocKhiChay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, _ := (&Brain{}).Run(ctx, ai.Job{})
	got := collect(t, ch)
	if len(got) != 1 || got[0].Kind != ai.EventError {
		t.Errorf("ctx huỷ sẵn: %+v, muốn đúng một error", got)
	}
}

func TestRunDelayPhatDanVaKhongKetKhiBenNhanBoDi(t *testing.T) {
	b := &Brain{Delay: 20 * time.Millisecond}
	start := time.Now()
	ch, _ := b.Run(context.Background(), ai.Job{Lang: "vi"})
	n := len(DemoScript(ai.Job{Lang: "vi"}))
	got := collect(t, ch)
	if len(got) != n {
		t.Fatalf("nhận %d sự kiện, muốn %d", len(got), n)
	}
	if min := time.Duration(n) * b.Delay; time.Since(start) < min {
		t.Errorf("phát hết sau %v, ít nhất phải %v (Delay trước mỗi sự kiện)", time.Since(start), min)
	}

	// Bên nhận bỏ đi: huỷ rồi không đọc nữa; goroutine vẫn phải thoát (kênh có
	// đệm đủ chỗ). Kiểm bằng cách chờ kênh đóng sau khi đọc nốt.
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ = (&Brain{Delay: time.Millisecond}).Run(ctx, ai.Job{})
	cancel()
	collect(t, ch)
}
