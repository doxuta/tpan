package core

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// cageHitsOf: phần tăng của hai bộ đếm qua các cặp mẫu liền nhau.
func TestCageHitsOf(t *testing.T) {
	const win = 30 * time.Minute
	deu := func(i int, s *cage.Sample) {
		uCPU(i, s, 10)
		s.MemMax, s.TasksMax = 256*uMiB, 64
	}
	cases := []struct {
		name string
		in   []cage.Sample
		want checks.CageHits
	}{
		{"không có mẫu", nil, checks.CageHits{Window: win}},
		{"một mẫu: chưa có cặp nào để so", uSeries(1, deu), checks.CageHits{Window: win}},
		{"bảy mẫu, bộ đếm đứng yên (đã chạm từ trước khoảng xét)", uSeries(7, func(i int, s *cage.Sample) {
			deu(i, s)
			s.OOMKills, s.TasksHits = 4, 9
		}), checks.CageHits{Window: win, Pairs: 6}},
		{"oom_kill tăng 2 giữa mẫu 3 và 4", uSeries(7, func(i int, s *cage.Sample) {
			deu(i, s)
			if i >= 4 {
				s.OOMKills = 2
			}
		}), checks.CageHits{Window: win, Pairs: 6, OOMKills: 2, MemMaxMB: 256}},
		{"chạm trần tiến trình 3 lần, rải ở hai cặp", uSeries(7, func(i int, s *cage.Sample) {
			deu(i, s)
			switch {
			case i >= 5:
				s.TasksHits = 3
			case i >= 2:
				s.TasksHits = 1
			}
		}), checks.CageHits{Window: win, Pairs: 6, TasksHits: 3, TasksMax: 64}},
		{"cả hai trần", uSeries(4, func(i int, s *cage.Sample) {
			deu(i, s)
			if i >= 1 {
				s.OOMKills, s.TasksHits = 1, 5
			}
		}), checks.CageHits{Window: win, Pairs: 3, OOMKills: 1, MemMaxMB: 256, TasksHits: 5, TasksMax: 64}},
		{"lồng khởi động lại (cpu_usec tụt): số của thế hệ mới là phần tăng, không cộng số cũ", uSeries(6, func(i int, s *cage.Sample) {
			s.MemMax = 128 * uMiB
			if i < 3 {
				s.CPUUsec, s.OOMKills = int64(1000+i)*1e6, 5
				return
			}
			s.CPUUsec, s.OOMKills = int64(i)*1e6, 1
		}), checks.CageHits{Window: win, Pairs: 5, OOMKills: 1, MemMaxMB: 128}},
		{"bộ đếm tụt trong cùng thế hệ (tasks_hits kernel 6.8): không tính là tăng", uSeries(5, func(i int, s *cage.Sample) {
			deu(i, s)
			s.TasksHits = []int64{5, 5, 3, 3, 4}[i]
		}), checks.CageHits{Window: win, Pairs: 4, TasksHits: 1, TasksMax: 64}},
	}
	for _, c := range cases {
		if got := cageHitsOf(c.in, win); got != c.want {
			t.Errorf("%s:\n có   %+v\n muốn %+v", c.name, got, c.want)
		}
	}

	// Bộ lấy mẫu hụt 20 phút (máy quá tải): cặp vắt qua lúc đó vẫn tính, vì cả
	// hai mẫu nằm trong khoảng đã lấy nên phần tăng xảy ra trong khoảng đó.
	gap := uSeries(5, func(i int, s *cage.Sample) {
		deu(i, s)
		if i >= 2 {
			s.At = s.At.Add(20 * time.Minute)
			s.OOMKills = 3
		}
		if i >= 4 {
			s.OOMKills = 4
		}
	})
	if got, want := cageHitsOf(gap, win), (checks.CageHits{Window: win, Pairs: 4, OOMKills: 4, MemMaxMB: 256}); got != want {
		t.Errorf("khoảng trống dài:\n có   %+v\n muốn %+v", got, want)
	}
	// Hai mẫu cùng thời điểm: không chia cho 0, không tính cặp.
	dup := uSeries(2, func(i int, s *cage.Sample) { s.At = uT0; s.OOMKills = int64(i) })
	if got := cageHitsOf(dup, win); got.Pairs != 0 || got.OOMKills != 0 {
		t.Errorf("hai mẫu trùng giờ: %+v", got)
	}
}

// Sites.Checks (đường của health) có cage.pressure cho đúng site trong lồng
// đang chạy, đọc số từ bảng mẫu; các thao tác không lấy nó làm kiểm chứng.
func TestSitesChecksCagePressure(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom, cgOther)
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	e.s.Now = func() time.Time { return now }
	store := &usageMemStore{}
	e.s.Samples = store
	pressure := func(dom string) (checks.Result, bool) {
		t.Helper()
		cs, err := e.s.Checks(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		return resultOf(checks.RunAll(ctx, e.m, cs), "cage.pressure:")
	}
	// put ghi một mẫu của dom lúc cách now chừng ago, với bộ đếm cho trước.
	put := func(dom string, ago time.Duration, oom, hits int64) {
		at := now.Add(-ago)
		store.samples = append(store.samples, cage.Sample{
			Domain: dom, At: at, Running: true, CPUUsec: at.Unix() * 1000,
			OOMKills: oom, TasksHits: hits, MemMax: 256 * uMiB, TasksMax: 64,
		})
	}

	if r, ok := pressure(cgDom); ok {
		t.Fatalf("site ngoài lồng mà có %s", r.ID)
	}
	e.enable(t, cgDom)

	// Chưa có mẫu nào: chưa kiểm được, không phải lỗi (không mở sự cố).
	r, ok := pressure(cgDom)
	if !ok || r.Status != checks.Unknown {
		t.Fatalf("site trong lồng, chưa có mẫu: có=%v %s %q", ok, r.Status, r.Summary.Vi)
	}
	if r.ID != "cage.pressure:"+cgDom || r.Stage != checks.StagePHP {
		t.Errorf("ID %q, chặng %q", r.ID, r.Stage)
	}

	// Mẫu đều 5 phút, bộ đếm đứng yên ở 7 (chạm từ hôm trước): đạt.
	for ago := 60 * time.Minute; ago >= 0; ago -= 5 * time.Minute {
		put(cgDom, ago, 7, 2)
	}
	if r, _ := pressure(cgDom); r.Status != checks.Pass {
		t.Fatalf("bộ đếm đứng yên: %s %q", r.Status, r.Summary.Vi)
	}

	// Hai lần hết RAM và một lần hết tiến trình trong 10 phút qua: lỗi, nói rõ số và trần.
	store.samples = nil
	for ago := 60 * time.Minute; ago >= 0; ago -= 5 * time.Minute {
		oom, hits := int64(7), int64(2)
		if ago <= 10*time.Minute {
			oom, hits = 9, 3
		}
		put(cgDom, ago, oom, hits)
	}
	r, _ = pressure(cgDom)
	if r.Status != checks.Fail {
		t.Fatalf("vừa chạm trần: %s %q", r.Status, r.Summary.Vi)
	}
	for _, want := range []string{"30 phút", "2 lần tiến trình PHP bị dừng vì hết RAM (trần 256 MB)", "1 lần không tạo được tiến trình mới (trần 64 tiến trình)"} {
		if !strings.Contains(r.Summary.Vi, want) {
			t.Errorf("câu tóm tắt thiếu %q: %q", want, r.Summary.Vi)
		}
	}
	if AutoFixName(r) != "" {
		t.Errorf("cage.pressure không có sửa tự động, có %q", AutoFixName(r))
	}
	if len(r.ManualFix) == 0 || !strings.Contains(strings.Join(r.ManualFix, "\n"), "tpan usage "+cgDom+" --suggest") {
		t.Errorf("cách xử lý thiếu lệnh gợi ý trần: %v", r.ManualFix)
	}
	// Site khác (ngoài lồng) không dính.
	if r, ok := pressure(cgOther); ok {
		t.Errorf("site ngoài lồng mà có %s", r.ID)
	}

	// 40 phút sau, không chạm nữa: đạt (sự cố tự đóng).
	now = now.Add(40 * time.Minute)
	for ago := 35 * time.Minute; ago >= 0; ago -= 5 * time.Minute {
		put(cgDom, ago, 9, 3)
	}
	if r, _ := pressure(cgDom); r.Status != checks.Pass {
		t.Fatalf("40 phút sau, không chạm nữa: %s %q", r.Status, r.Summary.Vi)
	}

	// Không có bảng mẫu (dòng lệnh trên máy chưa có tpan.db): không sinh phép kiểm.
	e.s.Samples = nil
	if r, ok := pressure(cgDom); ok {
		t.Errorf("không có bảng mẫu mà có %s (%s)", r.ID, r.Status)
	}
	e.s.Samples = store

	// Site tạm khoá: lồng đã tắt, không sinh phép kiểm.
	e.setSite(t, cgDom, func(s *site.Site) { s.Suspended = account.SuspendSelf })
	if r, ok := pressure(cgDom); ok {
		t.Errorf("site tạm khoá mà có %s (%s)", r.ID, r.Status)
	}
}

// Kiểm chứng sau thao tác không có cage.pressure: site vừa chạm trần không
// phải lý do để hoàn tác một lần đổi cấu hình.
func TestCagePressureKhongNamTrongKiemChung(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	store := &usageMemStore{}
	for i := 0; i < 7; i++ {
		at := now.Add(time.Duration(i-6) * 5 * time.Minute)
		store.samples = append(store.samples, cage.Sample{Domain: cgDom, At: at, Running: true, CPUUsec: at.Unix() * 1000, OOMKills: int64(i), MemMax: 256 * uMiB})
	}
	e.s.Now, e.s.Samples = func() time.Time { return now }, store
	e.enable(t, cgDom)

	cg := e.cages()
	off, err := cg.PrepareDisable(ctx, mustDomain(t, cgDom))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range checks.RunAll(ctx, e.m, off.Plan.Verify) {
		if strings.HasPrefix(r.ID, "cage.pressure") {
			t.Errorf("kiểm chứng của %s có %s", off.Plan.Kind, r.ID)
		}
	}
	// Còn health thì có, và đang lỗi.
	if r := e.recheck(t, cgDom, "cage.pressure:"+cgDom); r.Status != checks.Fail {
		t.Errorf("health: %s %q", r.Status, r.Summary.Vi)
	}
}

// cgPutCgroup đặt các file cgroup của lồng l như kernel 6.8 (không có
// pids.events.local): bộ đếm cpu, oom_kill và số lần chạm trần tiến trình.
func cgPutCgroup(e *cgEnv, l site.Layout, cpuUsec, oom, tasksHits int64) {
	dir := l.CageCgroup()
	e.m.SetDir(dir, 0o755, system.Root)
	put := func(name, body string) { e.m.SetFile(dir+"/"+name, []byte(body), 0o644, system.Root) }
	put("memory.current", "1048576\n")
	put("memory.peak", "2097152\n")
	put("memory.max", fmt.Sprintf("%d\n", 256*uMiB))
	put("memory.events", fmt.Sprintf("low 0\nhigh 0\nmax %d\noom %d\noom_kill %d\n", oom, oom, oom))
	put("cpu.stat", fmt.Sprintf("usage_usec %d\nnr_throttled 0\nthrottled_usec 0\n", cpuUsec))
	put("pids.current", "7\n")
	put("pids.peak", "9\n")
	put("pids.max", "64\n")
	put("pids.events", fmt.Sprintf("max %d\n", tasksHits))
}

// Lần đọc cgroup ngay lúc dựng phép kiểm là mẫu cuối: thấy lần chạm trần xảy
// ra sau mẫu đã lưu gần nhất, và site vừa vào lồng thì đạt ngay.
func TestSitesChecksCagePressureDocNgay(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom)
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	store := &usageMemStore{}
	e.s.Now, e.s.Samples = func() time.Time { return now }, store
	e.enable(t, cgDom)
	pressure := func() checks.Result {
		t.Helper()
		return e.recheck(t, cgDom, "cage.pressure:"+cgDom)
	}
	stored := func(ago time.Duration, cpuUsec, oom, hits int64) {
		store.samples = []cage.Sample{{Domain: cgDom, At: now.Add(-ago), Running: true, CPUUsec: cpuUsec, OOMKills: oom, TasksHits: hits, MemMax: 256 * uMiB, TasksMax: 64}}
	}

	// Lồng vừa chạy, chưa có mẫu nào, bộ đếm là 0: đạt ngay.
	cgPutCgroup(e, e.l, 1000, 0, 0)
	if r := pressure(); r.Status != checks.Pass || !strings.Contains(r.Summary.Vi, "từ lúc lồng chạy") {
		t.Fatalf("lồng mới: %s %q", r.Status, r.Summary.Vi)
	}
	// Chưa có mẫu nào mà bộ đếm đã khác 0 (panel tắt lâu): không biết chạm lúc nào.
	cgPutCgroup(e, e.l, 1000, 2, 0)
	if r := pressure(); r.Status != checks.Unknown {
		t.Fatalf("không có mẫu, bộ đếm khác 0: %s %q", r.Status, r.Summary.Vi)
	}
	// Một mẫu lưu 3 phút trước (oom 0), giờ đọc được oom_kill 2 và 1 lần chạm
	// trần tiến trình: lỗi ngay, không chờ mẫu kế tiếp.
	stored(3*time.Minute, 500, 0, 0)
	cgPutCgroup(e, e.l, 1000, 2, 1)
	r := pressure()
	if r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, "2 lần tiến trình PHP bị dừng vì hết RAM (trần 256 MB); 1 lần không tạo được tiến trình mới (trần 64 tiến trình)") {
		t.Fatalf("chạm trần sau mẫu gần nhất: %s %q", r.Status, r.Summary.Vi)
	}
	// Mẫu lưu đã có đúng số đó: không tăng thêm, đạt.
	stored(3*time.Minute, 500, 2, 1)
	if r := pressure(); r.Status != checks.Pass || !strings.Contains(r.Summary.Vi, "30 phút") {
		t.Fatalf("bộ đếm không đổi từ mẫu trước: %s %q", r.Status, r.Summary.Vi)
	}
	// Lồng khởi động lại sau mẫu trước (cpu_usec tụt): số cũ không tính, số
	// của thế hệ mới mới là phần tăng.
	stored(3*time.Minute, 9_000_000, 5, 5)
	cgPutCgroup(e, e.l, 1000, 0, 0)
	if r := pressure(); r.Status != checks.Pass {
		t.Fatalf("lồng khởi động lại, chưa chạm: %s %q", r.Status, r.Summary.Vi)
	}
	cgPutCgroup(e, e.l, 1000, 1, 0)
	if r := pressure(); r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, "1 lần tiến trình PHP bị dừng") {
		t.Fatalf("lồng khởi động lại rồi chạm trần: %s %q", r.Status, r.Summary.Vi)
	}
	// Mẫu lưu gần nhất cách 20 phút (bộ lấy mẫu hụt nhịp): vẫn so được, vì 20
	// phút trước còn nằm trong khoảng xét.
	stored(20*time.Minute, 500, 0, 0)
	cgPutCgroup(e, e.l, 1000, 3, 0)
	if r := pressure(); r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, "3 lần tiến trình PHP bị dừng") {
		t.Fatalf("mẫu gần nhất cách 20 phút: %s %q", r.Status, r.Summary.Vi)
	}
	// Mẫu lưu gần nhất đã ra khỏi khoảng xét (panel tắt 50 phút): không có gì để so.
	stored(50*time.Minute, 500, 0, 0)
	if r := pressure(); r.Status != checks.Unknown {
		t.Fatalf("mẫu gần nhất ngoài khoảng xét: %s %q", r.Status, r.Summary.Vi)
	}
}
