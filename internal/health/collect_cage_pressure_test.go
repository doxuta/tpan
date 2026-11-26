package health

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
)

// Đi hết đường thật của health (Runner.Run trên store thật): site trong lồng
// vừa chạm trần RAM thì có kết quả cage.pressure không đạt và một sự cố mở;
// 40 phút sau không chạm nữa thì sự cố tự đóng. Chạy cả hai đường gom phép
// kiểm (cả máy, một site): test của core không thấy được Runner có đưa bảng
// mẫu và đồng hồ cho core.Sites hay không.
func TestRunSiteChamTranMoVaDongSuCo(t *testing.T) {
	ctx := context.Background()
	const id = "cage.pressure:" + dA
	for name, o := range map[string]Options{"cả máy": {Full: true}, "một site": {Domain: dA}} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, true)
			sa, _, err := e.st.Site(ctx, dom(t, dA))
			if err != nil {
				t.Fatal(err)
			}
			sa.Caged = true
			if err := e.st.SaveSite(ctx, sa); err != nil {
				t.Fatal(err)
			}
			sample := func(at time.Time, oom int64) {
				t.Helper()
				s := cage.Sample{Domain: dA, At: at, Running: true, CPUUsec: at.Unix() * 1000, OOMKills: oom, MemMax: 256 << 20, TasksMax: 64}
				if err := e.st.AddUsageSample(ctx, s); err != nil {
					t.Fatal(err)
				}
			}
			find := func(rep Report) (checks.Result, bool) {
				for _, g := range rep.Groups {
					for _, r := range g.Results {
						if strings.HasPrefix(r.ID, "cage.pressure:") {
							if g.Target != dA || r.ID != id {
								t.Errorf("cage.pressure ở sai chỗ: target %q, ID %q", g.Target, r.ID)
							}
							return r, true
						}
					}
				}
				return checks.Result{}, false
			}

			// Chưa có mẫu: chưa kiểm được, không mở sự cố.
			rep := e.run(t, o)
			if r, ok := find(rep); !ok || r.Status != checks.Unknown {
				t.Fatalf("chưa có mẫu: có=%v %s %q", ok, r.Status, r.Summary.Vi)
			}
			if openFor(t, e, id) {
				t.Fatal("chưa kiểm được mà đã mở sự cố")
			}

			// Hai lần hết RAM trong 5 phút qua.
			sample(t0.Add(-10*time.Minute), 3)
			sample(t0.Add(-5*time.Minute), 3)
			sample(t0, 5)
			rep = e.run(t, o)
			r, ok := find(rep)
			if !ok || r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, "2 lần tiến trình PHP bị dừng vì hết RAM (trần 256 MB)") {
				t.Fatalf("vừa chạm trần: có=%v %s %q", ok, r.Status, r.Summary.Vi)
			}
			opened := false
			for _, inc := range rep.Changes.Opened {
				opened = opened || (inc.CheckID == id && inc.Target == dA)
			}
			if !opened || !openFor(t, e, id) {
				t.Fatalf("không mở sự cố %s: %+v", id, rep.Changes.Opened)
			}

			// 40 phút sau, bộ đếm đứng yên: đạt, sự cố đóng vì "pass".
			e.now = t0.Add(40 * time.Minute)
			for _, m := range []int{30, 35, 40} {
				sample(t0.Add(time.Duration(m)*time.Minute), 5)
			}
			rep = e.run(t, o)
			if r, _ := find(rep); r.Status != checks.Pass {
				t.Fatalf("40 phút sau: %s %q", r.Status, r.Summary.Vi)
			}
			closed := false
			for _, inc := range rep.Changes.Closed {
				closed = closed || (inc.CheckID == id && inc.CloseReason == "pass")
			}
			if !closed || openFor(t, e, id) {
				t.Errorf("sự cố %s chưa tự đóng: %+v", id, rep.Changes.Closed)
			}
		})
	}
}

// Site ngoài lồng không có cage.pressure, kể cả khi bảng mẫu còn số cũ của nó.
func TestRunSiteNgoaiLongKhongCoCagePressure(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	for i, oom := range []int64{0, 4} {
		at := t0.Add(time.Duration(i-1) * 5 * time.Minute)
		if err := e.st.AddUsageSample(ctx, cage.Sample{Domain: dB, At: at, Running: true, CPUUsec: at.Unix(), OOMKills: oom}); err != nil {
			t.Fatal(err)
		}
	}
	for id := range targets(t, e.run(t, Options{Full: true})) {
		if strings.HasPrefix(id, "cage.pressure:") {
			t.Errorf("site ngoài lồng mà có %s", id)
		}
	}
}
