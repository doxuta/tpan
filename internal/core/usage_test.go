package core

// Test của số liệu và gợi ý trần (M17, mục 3.8 của docs/thay-doi/M17-thiet-ke.md).
// Phần thuần (suggest, rollupHour) kiểm bằng bảng; phần chạm máy (Sample, Site,
// List, Rollup) dùng FakeSystem có file cgroup và một bảng mẫu trong bộ nhớ.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	uMiB  = int64(1 << 20)
	uStep = 5 * time.Minute
)

var uT0 = time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

// uSeries dựng n mẫu cách nhau uStep, mỗi mẫu đang chạy; mut chỉnh từng mẫu.
// CPUUsec mặc định là 0: mut tự đặt khi cần.
func uSeries(n int, mut func(i int, s *cage.Sample)) []cage.Sample {
	out := make([]cage.Sample, n)
	for i := range out {
		out[i] = cage.Sample{Domain: "a.example.com", At: uT0.Add(time.Duration(i) * uStep), Running: true}
		if mut != nil {
			mut(i, &out[i])
		}
	}
	return out
}

// uCPU đặt cpu_usec cộng dồn để mỗi bước 5 phút dùng pct % của một nhân.
func uCPU(i int, s *cage.Sample, pct int64) {
	s.CPUUsec = int64(i) * int64(uStep.Microseconds()) * pct / 100
}

func TestSuggestBang(t *testing.T) {
	cur := account.Limits{RAMMB: 200, CPUPercent: 50, Tasks: 40}
	cases := []struct {
		name    string
		samples []cage.Sample
		cur     account.Limits
		want    account.Limits // chỉ RAMMB, CPUPercent, Tasks
		check   func(t *testing.T, sg Suggestion)
	}{
		{
			name: "khong co mau", samples: nil, cur: cur, want: account.Limits{},
			check: func(t *testing.T, sg Suggestion) {
				if !sg.LowData || sg.Samples != 0 || len(sg.Reasons) != 1 || sg.Current != cur {
					t.Fatalf("không mẫu: muốn LowData, 0 mẫu, một lý do, Current giữ nguyên; có %+v", sg)
				}
			},
		},
		{
			// 200 MB × 1,25 = 250, lên bội 32 là 256; CPU 10% × 1,5 = 15 thấp hơn sàn 25;
			// tiến trình 8 × 2 = 16 lên 20 thấp hơn sàn 32.
			name: "it mau",
			samples: uSeries(10, func(i int, s *cage.Sample) {
				s.MemCurrent, s.Tasks, s.TasksPeak = 200*uMiB, 5, 8
				uCPU(i, s, 10)
			}),
			cur: cur, want: account.Limits{RAMMB: 256, CPUPercent: 25, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				if !sg.LowData || sg.Samples != 10 || !sg.From.Equal(uT0) || !sg.To.Equal(uT0.Add(9*uStep)) {
					t.Fatalf("ít mẫu: muốn LowData và khoảng thời gian của mẫu; có %+v", sg)
				}
			},
		},
		{
			name: "du mau khong LowData",
			samples: uSeries(UsageLowData, func(i int, s *cage.Sample) {
				s.MemCurrent = 200 * uMiB
				uCPU(i, s, 10)
			}),
			cur: cur, want: account.Limits{RAMMB: 256, CPUPercent: 25, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				if sg.LowData {
					t.Fatal("đúng 288 mẫu thì không còn LowData")
				}
			},
		},
		{
			// Bị giết hai lần ở trần 200 MB: số đo đã bị cắt ở 200 MB (250 → 256 nếu
			// tin nó); phải không thấp hơn 200 × 1,5 = 300, lên bội 32 là 320.
			name: "co oom_kill",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.MemCurrent, s.MemPeak, s.MemMax = 200*uMiB, 200*uMiB, 200*uMiB
				if i >= 8 {
					s.OOMKills = 2
				}
				uCPU(i, s, 10)
			}),
			cur: cur, want: account.Limits{RAMMB: 320, CPUPercent: 25, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				if sg.OOMKills != 2 {
					t.Fatalf("muốn 2 lần oom_kill, có %d", sg.OOMKills)
				}
				if !strings.Contains(sg.Reasons[0].Vi, "1,5") || !strings.Contains(sg.Reasons[0].En, "1.5") {
					t.Fatalf("lý do RAM phải nói luật trần × 1,5 ở cả hai ngôn ngữ: %+v", sg.Reasons[0])
				}
			},
		},
		{
			// Máy khởi động lại hai lần: cpu_usec về 0 ở mẫu 4 và 8, kèm oom_kill,
			// tasks_hits. Phần tăng: 0→2 (+2), về 0 (+0), 0→1 (+1) = 3. CPU sau mỗi
			// lần về 0 là giá trị mới (không âm, không tính lại từ số cũ).
			name: "bo dem ve 0 giua chung",
			samples: uSeries(12, func(i int, s *cage.Sample) {
				s.MemCurrent, s.MemMax = 100*uMiB, 100*uMiB
				j := i % 4 // thế hệ mới ở i = 4, 8
				uCPU(j, s, 20)
				switch {
				case i == 2 || i == 3:
					s.OOMKills = 2
				case i >= 9:
					s.OOMKills = 1
				}
			}),
			cur: cur, want: account.Limits{RAMMB: 160, CPUPercent: 30, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				// 0 → 2 ở mẫu 2, mẫu 4 là thế hệ mới với OOMKills 0 (+0), 0 → 1 ở mẫu 9.
				if sg.OOMKills != 3 {
					t.Fatalf("muốn 3 lần oom_kill qua mốc về 0, có %d", sg.OOMKills)
				}
				if sg.CPUP95 != 20 {
					t.Fatalf("p95 CPU muốn 20 (mỗi khoảng 20%%, kể cả khoảng ngay sau mốc về 0), có %v", sg.CPUP95)
				}
			},
		},
		{
			// memory.peak của slice là 4 GB từ ba tháng trước và không tăng: không được
			// kéo gợi ý (mục 3.8, soát thiết kế điểm 20a). Đỉnh thật 100 MB → 125 → 128.
			name: "mem_peak cu khong keo",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.MemCurrent, s.MemPeak = 100*uMiB, 4096*uMiB
				uCPU(i, s, 10)
			}),
			cur: cur, want: account.Limits{RAMMB: 128, CPUPercent: 25, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				if sg.MemPeak != 100*uMiB {
					t.Fatalf("đỉnh RAM muốn 100 MB, có %d", sg.MemPeak)
				}
			},
		},
		{
			// mem_peak tụt từ 4096 xuống 300 MB (cùng thế hệ): không phải đỉnh tăng, bỏ.
			name: "mem_peak giam khong tinh",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.MemCurrent, s.MemPeak = 100*uMiB, 4096*uMiB
				if i >= 10 {
					s.MemPeak = 300 * uMiB
				}
				uCPU(i, s, 10)
			}),
			cur: cur, want: account.Limits{RAMMB: 128, CPUPercent: 25, Tasks: 32},
		},
		{
			name: "mem_peak tang that thi tinh",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.MemCurrent, s.MemPeak = 100*uMiB, 150*uMiB
				if i >= 10 {
					s.MemPeak = 300 * uMiB
				}
				uCPU(i, s, 10)
			}),
			cur: cur, want: account.Limits{RAMMB: 384, CPUPercent: 25, Tasks: 32},
		},
		{
			// Bị bóp 2% thời gian (> 1%) ở trần 50%: p95 40% × 1,5 = 60 nhưng phải không
			// thấp hơn 50 × 1,5 = 75, lên bội 10 là 80.
			name: "bi bop CPU qua 1 phan tram",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.CPUQuotaPct = 50
				uCPU(i, s, 40)
				s.ThrottledUsec = int64(i) * uStep.Microseconds() * 2 / 100
				s.NrThrottled = int64(i)
			}),
			cur: cur, want: account.Limits{RAMMB: 128, CPUPercent: 80, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				if sg.ThrottledPct < 1.99 || sg.ThrottledPct > 2.01 {
					t.Fatalf("muốn bị bóp 2%%, có %v", sg.ThrottledPct)
				}
			},
		},
		{
			// Đúng 1% thì chưa quá: không có sàn trần × 1,5. p95 40% × 1,5 = 60.
			name: "bi bop CPU dung 1 phan tram",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.CPUQuotaPct = 50
				uCPU(i, s, 40)
				s.ThrottledUsec = int64(i) * uStep.Microseconds() / 100
			}),
			cur: cur, want: account.Limits{RAMMB: 128, CPUPercent: 60, Tasks: 32},
		},
		{
			// Năm mẫu cách 5 phút, riêng một khoảng 20 phút dùng CPU rất nhiều (đồng hồ
			// nhảy, panel tắt): khoảng đó bỏ, p95 còn 10%. Nếu không bỏ, p95 = lớn nhất.
			name: "khoang 20 phut bi bo",
			samples: func() []cage.Sample {
				ss := uSeries(5, func(i int, s *cage.Sample) { uCPU(i, s, 10) })
				ss[3].At = ss[2].At.Add(20 * time.Minute)
				ss[3].CPUUsec = ss[2].CPUUsec + 20*60*1_000_000 // 100% trong 20 phút
				ss[4].At = ss[3].At.Add(uStep)
				ss[4].CPUUsec = ss[3].CPUUsec + uStep.Microseconds()/10
				return ss
			}(),
			cur: cur, want: account.Limits{RAMMB: 128, CPUPercent: 25, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				if sg.CPUP95 != 10 {
					t.Fatalf("khoảng 20 phút phải bị bỏ: p95 muốn 10, có %v", sg.CPUP95)
				}
			},
		},
		{
			// Số đúng bội: 128 MB × 1,25 = 160 (bội của 32, giữ 160 chứ không lên 192);
			// p95 20% × 1,5 = 30 (không lên 40 vì sai số dấu phẩy động); tiến trình
			// 20 × 2 = 40.
			name: "so tron dung boi",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.MemCurrent, s.Tasks, s.TasksPeak = 128*uMiB, 20, 20
				uCPU(i, s, 20)
			}),
			cur: cur, want: account.Limits{RAMMB: 160, CPUPercent: 30, Tasks: 40},
		},
		{
			// Gói 30 worker: sàn tiến trình là 30 + 15 = 45, cao hơn 8 × 2 → 20.
			name: "san so tien trinh theo worker",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.Tasks, s.TasksPeak = 3, 4
			}),
			cur: account.Limits{PHPWorkers: 30}, want: account.Limits{RAMMB: 128, CPUPercent: 25, Tasks: 45},
		},
		{
			// Chạm trần tiến trình ở trần 40: 40 × 1,5 = 60 cao hơn 10 × 2 = 20.
			name: "cham tran tien trinh",
			samples: uSeries(20, func(i int, s *cage.Sample) {
				s.Tasks, s.TasksPeak, s.TasksMax = 5, 10, 40
				if i >= 6 {
					s.TasksHits = 3
				}
			}),
			cur: cur, want: account.Limits{RAMMB: 128, CPUPercent: 25, Tasks: 60},
			check: func(t *testing.T, sg Suggestion) {
				if sg.TasksHits != 3 {
					t.Fatalf("muốn 3 lần chạm trần tiến trình, có %d", sg.TasksHits)
				}
			},
		},
		{
			// tasks_hits tụt (cgroup con của lệnh tạm biến mất) trong khi cpu_usec vẫn
			// tăng: không phải bộ đếm về 0, không lấy số còn lại làm phần tăng.
			name: "tasks_hits tut khong tinh",
			samples: uSeries(10, func(i int, s *cage.Sample) {
				uCPU(i, s, 10)
				if i == 3 || i == 4 {
					s.TasksHits = 5
				}
			}),
			cur: cur, want: account.Limits{RAMMB: 128, CPUPercent: 25, Tasks: 32},
			check: func(t *testing.T, sg Suggestion) {
				if sg.TasksHits != 5 { // 0 → 5 là 5; 5 → 0 là 0
					t.Fatalf("muốn 5, có %d", sg.TasksHits)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sg := suggest("a.example.com", account.DefaultPackage, tc.samples, tc.cur)
			if tc.want.Any() || len(tc.samples) > 0 {
				got := account.Limits{RAMMB: sg.Suggested.RAMMB, CPUPercent: sg.Suggested.CPUPercent, Tasks: sg.Suggested.Tasks}
				if got != tc.want {
					t.Fatalf("gợi ý muốn %+v, có %+v (lý do: %+v)", tc.want, got, sg.Reasons)
				}
				if sg.Suggested.IOReadMB != 0 || sg.Suggested.IOWriteMB != 0 || sg.Suggested.PHPWorkers != 0 {
					t.Fatalf("không gợi ý trần đĩa và số worker: %+v", sg.Suggested)
				}
				if len(sg.Reasons) != 3 {
					t.Fatalf("muốn ba lý do (RAM, CPU, tiến trình), có %d", len(sg.Reasons))
				}
				for _, r := range sg.Reasons {
					if r.Vi == "" || r.En == "" {
						t.Fatalf("lý do thiếu một ngôn ngữ: %+v", r)
					}
				}
			} else if sg.Suggested != (account.Limits{}) {
				t.Fatalf("không mẫu thì không gợi ý: %+v", sg.Suggested)
			}
			if tc.check != nil {
				tc.check(t, sg)
			}
		})
	}
}

func TestRollupHour(t *testing.T) {
	hour := uT0
	t.Run("phan tang va dinh", func(t *testing.T) {
		ss := uSeries(12, func(i int, s *cage.Sample) {
			s.MemCurrent = int64(100+i) * uMiB
			s.Tasks = int64(i)
			uCPU(i, s, 10)
			s.IORBytes = int64(i) * 1000
			s.OOMKills = int64(i / 6)
		})
		ss[5].CPUUsec = ss[4].CPUUsec + uStep.Microseconds()/2 // một khoảng 50%
		for i := 6; i < len(ss); i++ {
			ss[i].CPUUsec = ss[i-1].CPUUsec + uStep.Microseconds()/10
		}
		h := rollupHour("a.example.com", hour, nil, ss)
		if h.Samples != 12 || h.Domain != "a.example.com" || !h.Hour.Equal(hour) {
			t.Fatalf("khoá của dòng giờ sai: %+v", h)
		}
		if h.MemAvg != (100*uMiB*12+66*uMiB)/12 { // trung bình 100..111 MB
			t.Fatalf("MemAvg muốn %d, có %d", (100*uMiB*12+66*uMiB)/12, h.MemAvg)
		}
		if h.MemMax != 111*uMiB || h.TasksMax != 11 {
			t.Fatalf("MemMax muốn 111 MB và TasksMax 11, có %d và %d", h.MemMax, h.TasksMax)
		}
		if h.CPUPctMax != 50 {
			t.Fatalf("CPUPctMax muốn 50, có %v", h.CPUPctMax)
		}
		if want := ss[11].CPUUsec - ss[0].CPUUsec; h.CPUUsec != want {
			t.Fatalf("CPUUsec (mẫu đầu không có prev) muốn %d, có %d", want, h.CPUUsec)
		}
		if h.IORBytes != 11*1000 || h.OOMKills != 1 {
			t.Fatalf("IORBytes muốn 11000 và OOMKills 1, có %d và %d", h.IORBytes, h.OOMKills)
		}
	})

	t.Run("prev tinh phan tang cua mau dau", func(t *testing.T) {
		prev := cage.Sample{At: uT0.Add(-uStep), Running: true, CPUUsec: 1000, OOMKills: 1}
		ss := []cage.Sample{{At: uT0, CPUUsec: 3000, OOMKills: 3}}
		h := rollupHour("a.example.com", hour, &prev, ss)
		if h.CPUUsec != 2000 || h.OOMKills != 2 {
			t.Fatalf("muốn tăng 2000 và 2 so với prev, có %d và %d", h.CPUUsec, h.OOMKills)
		}
	})

	t.Run("qua moc ve 0", func(t *testing.T) {
		// Mẫu 2: máy khởi động lại (cpu_usec về nhỏ hơn). Phần tăng là giá trị mới:
		// 10 → 20 → 30 (+20), về 5 (+5), 5 → 15 (+10). Tổng +35, không âm.
		ss := []cage.Sample{
			{At: uT0, CPUUsec: 10, OOMKills: 4, IOWBytes: 100},
			{At: uT0.Add(uStep), CPUUsec: 20, OOMKills: 4, IOWBytes: 200},
			{At: uT0.Add(2 * uStep), CPUUsec: 30, OOMKills: 5, IOWBytes: 300},
			{At: uT0.Add(3 * uStep), CPUUsec: 5, OOMKills: 1, IOWBytes: 40},
			{At: uT0.Add(4 * uStep), CPUUsec: 15, OOMKills: 1, IOWBytes: 90},
		}
		h := rollupHour("a.example.com", hour, nil, ss)
		if h.CPUUsec != 35 {
			t.Fatalf("CPUUsec qua mốc về 0 muốn 35, có %d", h.CPUUsec)
		}
		if h.OOMKills != 2 { // +1 rồi +1 (giá trị mới sau khi về 0)
			t.Fatalf("OOMKills muốn 2, có %d", h.OOMKills)
		}
		if h.IOWBytes != 100+100+40+50 {
			t.Fatalf("IOWBytes muốn 290, có %d", h.IOWBytes)
		}
	})

	t.Run("mem_peak cu khong vao MemMax", func(t *testing.T) {
		ss := uSeries(6, func(i int, s *cage.Sample) {
			s.MemCurrent, s.MemPeak = 100*uMiB, 900*uMiB
		})
		if h := rollupHour("a.example.com", hour, nil, ss); h.MemMax != 100*uMiB {
			t.Fatalf("đỉnh cũ không tăng: MemMax muốn 100 MB, có %d", h.MemMax)
		}
		ss[3].MemPeak = 950 * uMiB
		if h := rollupHour("a.example.com", hour, nil, ss); h.MemMax != 950*uMiB {
			t.Fatalf("đỉnh tăng trong giờ: MemMax muốn 950 MB, có %d", h.MemMax)
		}
	})

	t.Run("khong mau", func(t *testing.T) {
		h := rollupHour("a.example.com", hour, nil, nil)
		if h.Samples != 0 || h.MemAvg != 0 {
			t.Fatalf("không mẫu: %+v", h)
		}
	})
}

// ---------------------------------------------------------------- giả lập

// usageMemStore là UsageStore trong bộ nhớ, ghi lại lời gọi PruneUsage.
type usageMemStore struct {
	mu      sync.Mutex
	samples []cage.Sample
	hours   []cage.Hour
	pruned  [][2]time.Time
	failAdd error
}

func (m *usageMemStore) AddUsageSample(_ context.Context, s cage.Sample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAdd != nil && s.Domain == "bad.example.com" {
		return m.failAdd
	}
	m.samples = append(m.samples, s)
	return nil
}

func (m *usageMemStore) UsageSamples(_ context.Context, d string, from, to time.Time) ([]cage.Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []cage.Sample
	for _, s := range m.samples {
		if s.Domain == d && !s.At.Before(from) && s.At.Before(to) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b cage.Sample) int { return a.At.Compare(b.At) })
	return out, nil
}

func (m *usageMemStore) UsageSampleDomains(_ context.Context, from, to time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	set := map[string]bool{}
	for _, s := range m.samples {
		if !s.At.Before(from) && s.At.Before(to) {
			set[s.Domain] = true
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	slices.Sort(out)
	return out, nil
}

func (m *usageMemStore) SaveUsageHour(_ context.Context, h cage.Hour) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, x := range m.hours {
		if x.Domain == h.Domain && x.Hour.Equal(h.Hour) {
			m.hours[i] = h
			return nil
		}
	}
	m.hours = append(m.hours, h)
	return nil
}

func (m *usageMemStore) UsageHours(_ context.Context, d string, from, to time.Time) ([]cage.Hour, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []cage.Hour
	for _, h := range m.hours {
		if h.Domain == d && !h.Hour.Before(from) && h.Hour.Before(to) {
			out = append(out, h)
		}
	}
	slices.SortFunc(out, func(a, b cage.Hour) int { return a.Hour.Compare(b.Hour) })
	return out, nil
}

func (m *usageMemStore) PruneUsage(_ context.Context, samplesBefore, hoursBefore time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruned = append(m.pruned, [2]time.Time{samplesBefore, hoursBefore})
	return nil
}

func (m *usageMemStore) DeleteUsageOf(context.Context, string) error { return nil }

var _ UsageStore = (*usageMemStore)(nil)

// usageEnv là FakeSystem, bảng site, bảng user và bảng mẫu đã nối với nhau.
type usageEnv struct {
	sys   *system.FakeSystem
	sites *memSites
	accts *usrStore
	store *usageMemStore
	now   time.Time
	u     *Usages
}

func newUsageEnv(t *testing.T) *usageEnv {
	t.Helper()
	e := &usageEnv{
		sys: system.NewFake(), sites: newMemSites(), store: &usageMemStore{},
		now: time.Date(2026, 11, 21, 10, 30, 0, 0, time.UTC),
	}
	e.accts = newUsrStore(e.sites, newUsrDBs())
	e.accts.pkgs["pro"] = account.Package{Name: "pro", Cage: true, RAMMB: 256, CPUPercent: 50, Tasks: 64}
	e.accts.put(account.User{Name: "alice", Role: account.RoleUser, Package: "pro"})
	e.accts.put(account.User{Name: "bob", Role: account.RoleUser, Package: "pro"})
	e.u = &Usages{Sys: e.sys, Store: e.sites, Accounts: e.accts, Samples: e.store, Now: func() time.Time { return e.now }}
	return e
}

// add lưu site; owner "" là admin.
func (e *usageEnv) add(t *testing.T, domain, owner string, caged bool, status site.Status) site.Site {
	t.Helper()
	st := m17Site(t, domain)
	st.Caged, st.Status = caged, status
	if owner != "" {
		st.Owner = owner
	}
	if err := e.sites.SaveSite(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return st
}

// cgroup đặt file cgroup của site st với RAM đang dùng memCur.
func (e *usageEnv) cgroup(st site.Site, memCur int64) {
	dir := st.Layout().CageCgroup()
	put := func(name, body string) { e.sys.SetFile(dir+"/"+name, []byte(body), 0o644, system.Owner{}) }
	put("memory.current", fmt.Sprintf("%d\n", memCur))
	put("memory.peak", fmt.Sprintf("%d\n", memCur*2))
	put("memory.max", fmt.Sprintf("%d\n", 256*uMiB))
	put("memory.events", "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\n")
	put("cpu.stat", "usage_usec 1234567\nuser_usec 1000000\nsystem_usec 234567\nnr_throttled 0\nthrottled_usec 0\n")
	put("cpu.max", "50000 100000\n")
	put("pids.current", "7\n")
	put("pids.peak", "9\n")
	put("pids.max", "64\n")
}

func dom(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// ---------------------------------------------------------------- Sample

func TestUsagesSampleGhiMoiSiteCagedActive(t *testing.T) {
	e := newUsageEnv(t)
	a := e.add(t, "a.example.com", "alice", true, site.Active)
	e.add(t, "b.example.com", "alice", false, site.Active) // ngoài lồng
	e.add(t, "c.example.com", "alice", true, site.Trashed) // trong thùng rác
	e.add(t, "d.example.com", "bob", true, site.Active)    // trong lồng, chưa có cgroup
	e.cgroup(a, 100*uMiB)

	n, err := e.u.Sample(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("muốn ghi 2 mẫu (a và d), có %d, %v", n, err)
	}
	got := map[string]cage.Sample{}
	for _, s := range e.store.samples {
		got[s.Domain] = s
	}
	if len(got) != 2 {
		t.Fatalf("chỉ a và d có mẫu, có %v", got)
	}
	sa := got["a.example.com"]
	if !sa.Running || sa.MemCurrent != 100*uMiB || sa.MemMax != 256*uMiB || sa.CPUQuotaPct != 50 ||
		sa.TasksMax != 64 || sa.CPUUsec != 1234567 || !sa.At.Equal(e.now) {
		t.Fatalf("mẫu của a phải mang số đọc và trần đang áp: %+v", sa)
	}
	if sd := got["d.example.com"]; sd.Running || sd.MemCurrent != 0 {
		t.Fatalf("lồng chưa chạy: mẫu Running=false, số 0; có %+v", sd)
	}
}

func TestUsagesSampleLoiMotSiteKhongChanSiteKhac(t *testing.T) {
	e := newUsageEnv(t)
	e.add(t, "a.example.com", "alice", true, site.Active)
	e.add(t, "bad.example.com", "alice", true, site.Active)
	e.store.failAdd = errors.New("đĩa đầy")
	n, err := e.u.Sample(context.Background())
	if n != 1 || err == nil || !strings.Contains(err.Error(), "bad.example.com") || !strings.Contains(err.Error(), "đĩa đầy") {
		t.Fatalf("muốn 1 mẫu và lỗi nêu bad.example.com, có %d, %v", n, err)
	}
}

func TestUsagesSampleKhongCoBangMau(t *testing.T) {
	e := newUsageEnv(t)
	e.u.Samples = nil
	if _, err := e.u.Sample(context.Background()); err == nil {
		t.Fatal("không có bảng mẫu thì Sample phải lỗi")
	}
}

// ---------------------------------------------------------------- Site, List

func TestUsagesSite(t *testing.T) {
	e := newUsageEnv(t)
	a := e.add(t, "a.example.com", "alice", true, site.Active)
	e.add(t, "b.example.com", "alice", false, site.Active)
	e.cgroup(a, 100*uMiB)
	ctx := context.Background()

	su, err := e.u.Site(ctx, dom(t, "a.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if su.Domain != "a.example.com" || su.Owner != "alice" || !su.Usage.Running || su.Usage.MemCurrent != 100*uMiB {
		t.Fatalf("số liệu phải là số đọc từ cgroup: %+v", su)
	}
	if su.Limits.RAMMB != 256 || su.Limits.CPUPercent != 50 || su.Limits.Tasks != 64 {
		t.Fatalf("trần phải lấy từ gói của chủ: %+v", su.Limits)
	}

	_, err = e.u.Site(ctx, dom(t, "b.example.com"))
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != CodeUsageNotCaged {
		t.Fatalf("site ngoài lồng: muốn %s, có %v", CodeUsageNotCaged, err)
	}
	_, err = e.u.Site(ctx, dom(t, "x.example.com"))
	if !errors.As(err, &re) || re.Code != "site_not_found" {
		t.Fatalf("site không có: muốn site_not_found, có %v", err)
	}
}

func TestUsagesListLocTheoChu(t *testing.T) {
	e := newUsageEnv(t)
	for _, c := range []struct{ d, owner string }{
		{"b.example.com", "alice"}, {"a.example.com", "bob"}, {"c.example.com", "alice"},
	} {
		e.cgroup(e.add(t, c.d, c.owner, true, site.Active), 50*uMiB)
	}
	e.add(t, "z.example.com", "alice", false, site.Active)
	ctx := context.Background()

	all, err := e.u.List(ctx, "")
	if err != nil || len(all) != 3 || all[0].Domain != "a.example.com" || all[2].Domain != "c.example.com" {
		t.Fatalf("admin muốn 3 site sắp theo domain, có %+v, %v", all, err)
	}
	mine, err := e.u.List(ctx, "alice")
	if err != nil || len(mine) != 2 || mine[0].Domain != "b.example.com" || mine[1].Domain != "c.example.com" {
		t.Fatalf("alice chỉ thấy site của mình trong lồng, có %+v, %v", mine, err)
	}
	if mine[0].Usage.MemCurrent != 50*uMiB || mine[0].Limits.RAMMB != 256 {
		t.Fatalf("List đọc ngay cgroup và lấy trần của gói: %+v", mine[0])
	}
}

// ---------------------------------------------------------------- History

func TestUsagesHistory(t *testing.T) {
	e := newUsageEnv(t)
	ctx := context.Background()
	d := dom(t, "a.example.com")

	if _, err := e.u.History(ctx, d, "year"); err == nil {
		t.Fatal("range lạ phải bị từ chối")
	} else if re := new(RefusedError); !errors.As(err, &re) || re.Code != "args_invalid" {
		t.Fatalf("muốn args_invalid, có %v", err)
	}

	// day: mẫu 5 phút; mẫu ở 25 giờ trước chỉ làm "mẫu trước" cho điểm đầu.
	start := e.now.Add(-24*time.Hour - uStep) // trước cửa sổ một nhịp
	for i := 0; i < 5; i++ {
		s := cage.Sample{Domain: "a.example.com", At: start.Add(time.Duration(i) * uStep), Running: true,
			MemCurrent: int64(100+i) * uMiB, Tasks: int64(i), OOMKills: int64(i / 3)}
		uCPU(i, &s, 20)
		e.store.samples = append(e.store.samples, s)
	}
	h, err := e.u.History(ctx, d, "day")
	if err != nil {
		t.Fatal(err)
	}
	if h.Range != "day" || len(h.Points) != 4 {
		t.Fatalf("day: muốn 4 điểm (mẫu trước cửa sổ không thành điểm), có %+v", h)
	}
	if p := h.Points[0]; p.CPUPct != 20 || p.MemAvg != 101*uMiB {
		t.Fatalf("điểm đầu dùng mẫu trước làm gốc: CPU 20%% và 101 MB; có %+v", p)
	}
	if h.Points[2].OOMKills != 1 { // oom 0,0,0,1,1 → tăng ở mẫu thứ 4 (chỉ số 3 trong chuỗi, 2 trong điểm)
		t.Fatalf("OOMKills tăng trong khoảng: %+v", h.Points)
	}

	// week, month: từ dòng giờ.
	e.store.hours = []cage.Hour{
		{Domain: "a.example.com", Hour: e.now.Add(-3 * 24 * time.Hour).Truncate(time.Hour), MemAvg: 5, MemMax: 9, CPUUsec: 36e6 * 25, TasksMax: 4, OOMKills: 1},
		{Domain: "a.example.com", Hour: e.now.Add(-20 * 24 * time.Hour).Truncate(time.Hour), MemAvg: 1, MemMax: 2},
		{Domain: "b.example.com", Hour: e.now.Add(-time.Hour).Truncate(time.Hour), MemAvg: 7},
	}
	w, err := e.u.History(ctx, d, "week")
	if err != nil || len(w.Points) != 1 || w.Points[0].MemMax != 9 || w.Points[0].CPUPct != 25 || w.Points[0].OOMKills != 1 {
		t.Fatalf("week: muốn 1 điểm (25%% CPU), có %+v, %v", w, err)
	}
	m, err := e.u.History(ctx, d, "month")
	if err != nil || len(m.Points) != 2 {
		t.Fatalf("month: muốn 2 điểm của a, có %+v, %v", m, err)
	}
}

// ---------------------------------------------------------------- Rollup

func TestUsagesRollup(t *testing.T) {
	e := newUsageEnv(t) // now = 10:30
	ctx := context.Background()
	at := func(h, m int) time.Time { return time.Date(2026, 11, 21, h, m, 0, 0, time.UTC) }
	add := func(d string, ts time.Time, mem int64) {
		e.store.samples = append(e.store.samples, cage.Sample{Domain: d, At: ts, Running: true, MemCurrent: mem * uMiB})
	}
	add("a.example.com", at(8, 5), 10)
	add("a.example.com", at(8, 10), 20)
	add("a.example.com", at(9, 5), 30)
	add("a.example.com", at(10, 5), 99) // giờ đang chạy dở: chưa gộp
	add("b.example.com", at(9, 40), 40)
	// Giờ 06:00 đã có dòng (cũ hơn hai giờ trọn gần nhất): giữ nguyên, không tính lại.
	add("a.example.com", at(6, 5), 77)
	e.store.hours = []cage.Hour{{Domain: "a.example.com", Hour: at(6, 0), Samples: 555}}

	if err := e.u.Rollup(ctx); err != nil {
		t.Fatal(err)
	}
	byKey := map[string]cage.Hour{}
	for _, h := range e.store.hours {
		byKey[h.Domain+"@"+h.Hour.Format("15")] = h
	}
	if len(byKey) != 4 { // a@06 (cũ), a@08, a@09, b@09; không có @10
		t.Fatalf("muốn bốn dòng giờ (a@06 giữ, a@08, a@09, b@09), có %v", byKey)
	}
	if h := byKey["a.example.com@08"]; h.Samples != 2 || h.MemAvg != 15*uMiB {
		t.Fatalf("a giờ 08: %+v", h)
	}
	if h := byKey["a.example.com@09"]; h.Samples != 1 || h.MemAvg != 30*uMiB {
		t.Fatalf("a giờ 09: %+v", h)
	}
	if h := byKey["a.example.com@06"]; h.Samples != 555 {
		t.Fatalf("giờ 06 đã gộp từ trước phải giữ nguyên: %+v", h)
	}
	if len(e.store.pruned) != 1 ||
		!e.store.pruned[0][0].Equal(e.now.Add(-8*24*time.Hour)) || !e.store.pruned[0][1].Equal(e.now.Add(-30*24*time.Hour)) {
		t.Fatalf("PruneUsage muốn gọi một lần với now−8 ngày và now−30 ngày, có %v", e.store.pruned)
	}

	// Chạy lại: giờ trọn gần nhất tính lại (ghi đè), không sinh dòng trùng.
	add("a.example.com", at(9, 55), 50)
	if err := e.u.Rollup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.store.hours) != 4 {
		t.Fatalf("chạy lại không được sinh dòng trùng, có %d dòng", len(e.store.hours))
	}
	for _, h := range e.store.hours {
		if h.Domain == "a.example.com" && h.Hour.Equal(at(9, 0)) && (h.Samples != 2 || h.MemAvg != 40*uMiB) {
			t.Fatalf("giờ 09 phải tính lại với mẫu mới: %+v", h)
		}
	}
	if len(e.store.pruned) != 2 {
		t.Fatalf("mỗi lượt gộp gọi PruneUsage một lần, có %d", len(e.store.pruned))
	}
}

// ---------------------------------------------------------------- Suggest

func TestUsagesSuggestVaSuggestions(t *testing.T) {
	e := newUsageEnv(t)
	ctx := context.Background()
	e.add(t, "a.example.com", "alice", true, site.Active)
	e.add(t, "b.example.com", "bob", true, site.Active)
	e.add(t, "c.example.com", "alice", false, site.Active)
	put := func(d string, mem int64) {
		for i := 0; i < 10; i++ {
			e.store.samples = append(e.store.samples, cage.Sample{Domain: d, At: e.now.Add(-time.Duration(10-i) * uStep), Running: true, MemCurrent: mem * uMiB})
		}
	}
	put("a.example.com", 200) // → 256
	put("b.example.com", 400) // → 500 → 512

	sg, err := e.u.Suggest(ctx, dom(t, "a.example.com"))
	if err != nil || sg.Suggested.RAMMB != 256 || sg.Package != "pro" || sg.Current.RAMMB != 256 || sg.Samples != 10 {
		t.Fatalf("gợi ý của a: %+v, %v", sg, err)
	}
	var re *RefusedError
	if _, err := e.u.Suggest(ctx, dom(t, "c.example.com")); !errors.As(err, &re) || re.Code != CodeUsageNotCaged {
		t.Fatalf("site ngoài lồng: muốn %s, có %v", CodeUsageNotCaged, err)
	}

	rep, err := e.u.Suggestions(ctx)
	if err != nil || len(rep.Sites) != 2 || len(rep.Packages) != 1 {
		t.Fatalf("muốn 2 site và 1 gói, có %+v, %v", rep, err)
	}
	ps := rep.Packages[0]
	if ps.Package != "pro" || len(ps.Sites) != 2 || ps.Suggested.RAMMB != 512 || ps.Current.RAMMB != 256 {
		t.Fatalf("gói lấy số lớn nhất trong các site (512): %+v", ps)
	}
}
