package checks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func TestCagePressure(t *testing.T) {
	d, err := validate.ParseDomain("shop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	l := site.LayoutOf(d)
	run := func(h CageHits) Result {
		return CagePressure(l, h).Run(context.Background(), system.NewFake())
	}
	w := 30 * time.Minute

	r := run(CageHits{Window: w})
	if r.Status != Unknown || r.ID != "cage.pressure:shop.example.com" || r.Stage != StagePHP {
		t.Errorf("chưa có số liệu: %+v", r)
	}
	// Site vừa vào lồng: chưa có cặp mẫu, bộ đếm của lồng đang là 0.
	if r := run(CageHits{Window: w, NeverHit: true}); r.Status != Pass || !strings.Contains(r.Summary.Vi, "từ lúc lồng chạy") {
		t.Errorf("lồng mới, chưa chạm lần nào: %+v", r)
	}
	// Đã có cặp mẫu thì NeverHit không còn nghĩa: xét theo số liệu.
	if r := run(CageHits{Window: w, Pairs: 1, OOMKills: 1, NeverHit: true}); r.Status != Fail {
		t.Errorf("có cặp mẫu chạm trần mà NeverHit che mất: %+v", r)
	}
	if r := run(CageHits{Window: w, Pairs: 6}); r.Status != Pass || !strings.Contains(r.Summary.Vi, "30 phút") {
		t.Errorf("không chạm trần: %+v", r)
	}

	r = run(CageHits{Window: w, Pairs: 6, OOMKills: 2, MemMaxMB: 256})
	if r.Status != Fail || !strings.Contains(r.Summary.Vi, "2 lần tiến trình PHP bị dừng vì hết RAM (trần 256 MB)") || strings.Contains(r.Summary.Vi, "tiến trình mới") {
		t.Errorf("chạm trần RAM: %+v", r.Summary)
	}
	r = run(CageHits{Window: w, Pairs: 6, TasksHits: 5, TasksMax: 64})
	if r.Status != Fail || !strings.Contains(r.Summary.Vi, "5 lần không tạo được tiến trình mới (trần 64 tiến trình)") || strings.Contains(r.Summary.Vi, "hết RAM") {
		t.Errorf("chạm trần tiến trình: %+v", r.Summary)
	}
	r = run(CageHits{Window: w, Pairs: 1, OOMKills: 1, TasksHits: 1})
	if r.Status != Fail || !strings.Contains(r.Summary.Vi, "hết RAM; 1 lần không tạo được") || strings.Contains(r.Summary.Vi, "(trần") {
		t.Errorf("chạm cả hai, không rõ trần: %+v", r.Summary)
	}
	// Fail luôn có nguyên nhân, cách tự xử lý, và dòng cuối là lệnh kiểm lại.
	if r.Cause.Vi == "" || r.Cause.En == "" || len(r.ManualFix) < 2 || r.ManualFix[len(r.ManualFix)-1] != "tpan health shop.example.com" {
		t.Errorf("Fail thiếu Cause hay ManualFix: %+v %v", r.Cause, r.ManualFix)
	}
	if !strings.Contains(r.Cause.Vi, "tự đóng khi 30 phút") {
		t.Errorf("Cause phải nói sự cố tự đóng khi nào: %s", r.Cause.Vi)
	}
}
