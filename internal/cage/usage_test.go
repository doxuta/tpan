package cage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

var tNow = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

func layout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

// loadSample đặt các file mẫu thật trong testdata/<dir> vào cgroup của l. Tên
// file có dạng "<tên>.khong-tran" chỉ được đặt khi gọi qua rename.
func loadSample(t *testing.T, f *system.FakeSystem, l site.Layout, dir string, rename map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		dst, ok := rename[name]
		if !ok {
			dst = name
		}
		if filepath.Ext(name) == ".khong-tran" && !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join("testdata", dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "dich-vu.pids.events" {
			// pids.events của dịch vụ PHP, cgroup con của slice (t3b).
			dst = l.CageServiceUnit() + "/pids.events"
		}
		f.SetFile(l.CageCgroup()+"/"+dst, data, 0o644, system.Root)
	}
}

func readSample(t *testing.T, dir string, rename map[string]string) Usage {
	t.Helper()
	f := system.NewFake()
	l := layout(t)
	loadSample(t, f, l, dir, rename)
	u, err := ReadUsage(f, l, tNow)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Running || !u.At.Equal(tNow) {
		t.Fatalf("Running=%v At=%v", u.Running, u.At)
	}
	return u
}

func TestReadUsageDangChay(t *testing.T) {
	got := readSample(t, "dang-chay", nil)
	want := Usage{
		Running: true, At: tNow,
		MemCurrent: 40239104, MemPeak: 44576768,
		CPUUsec: 1710432, ThrottledUsec: 2521564, NrThrottled: 37,
		Tasks: 2, TasksPeak: 2,
		IORBytes: 28672,
		MemMax:   209715200, CPUQuotaUs: 50000, CPUPeriod: 100000, TasksMax: 40,
	}
	if got != want {
		t.Fatalf("\ngot  %+v\nwant %+v", got, want)
	}
}

func TestReadUsageChamTran(t *testing.T) {
	got := readSample(t, "cham-tran", nil)
	want := Usage{
		Running: true, At: tNow,
		MemCurrent: 40960000, MemPeak: 209715200, OOMKills: 1, MemMaxHits: 35,
		CPUUsec: 1777679, ThrottledUsec: 2751263, NrThrottled: 40,
		Tasks: 39, TasksPeak: 40,
		// Slice có max 0, dịch vụ PHP có max 1: kernel 6.8 đếm ở nơi gọi fork.
		TasksHits: 1,
		IORBytes:  28672,
		MemMax:    209715200, CPUQuotaUs: 50000, CPUPeriod: 100000, TasksMax: 40,
	}
	if got != want {
		t.Fatalf("\ngot  %+v\nwant %+v", got, want)
	}
}

func TestReadUsageKhongCPU(t *testing.T) {
	got := readSample(t, "khong-cpu", nil)
	// Không có cpu.max: không giới hạn; cpu.stat không có hai số bóp: 0.
	if got.CPUQuotaUs != 0 || got.CPUPeriod != 0 || got.ThrottledUsec != 0 || got.NrThrottled != 0 {
		t.Fatalf("%+v", got)
	}
	if got.CPUUsec != 282781 || got.MemMax != 209715200 || got.TasksMax != 40 {
		t.Fatalf("%+v", got)
	}
	if got.IORBps != 0 || got.IOWBps != 0 {
		t.Fatalf("io.max rỗng là không giới hạn: %+v", got)
	}
}

func TestReadUsageCoIO(t *testing.T) {
	got := readSample(t, "co-io", nil)
	if got.IORBps != 4000000 || got.IOWBps != 5000000 {
		t.Fatalf("IORBps=%d IOWBps=%d", got.IORBps, got.IOWBps)
	}
	if got.MemMax != 209715200 || got.SwapMax != 0 {
		t.Fatalf("MemMax=%d SwapMax=%d", got.MemMax, got.SwapMax)
	}
	got = readSample(t, "co-io", map[string]string{
		"memory.max.khong-tran": "memory.max", "memory.swap.max.khong-tran": "memory.swap.max",
	})
	// memory.max "max" là 0; memory.swap.max "max" là -1 (hợp đồng của SwapMax).
	if got.MemMax != 0 || got.SwapMax != -1 {
		t.Fatalf("MemMax=%d SwapMax=%d", got.MemMax, got.SwapMax)
	}
}

func TestReadUsageKhongCoThuMuc(t *testing.T) {
	u, err := ReadUsage(system.NewFake(), layout(t), tNow)
	if err != nil {
		t.Fatal(err)
	}
	if u != (Usage{At: tNow}) {
		t.Fatalf("%+v", u)
	}
}

func TestReadUsagePidsEventsLocal(t *testing.T) {
	f := system.NewFake()
	l := layout(t)
	cg := l.CageCgroup()
	f.SetFile(cg+"/pids.events", []byte("max 3\n"), 0o644, system.Root)
	f.SetFile(cg+"/pids.events.local", []byte("max 1\n"), 0o644, system.Root)
	f.SetFile(cg+"/"+l.CageServiceUnit()+"/pids.events", []byte("max 7\n"), 0o644, system.Root)
	u, err := ReadUsage(f, l, tNow)
	if err != nil {
		t.Fatal(err)
	}
	// Kernel có pids.events.local thì pids.events của slice đã gồm cả cây con.
	if u.TasksHits != 3 {
		t.Fatalf("TasksHits = %d, muốn 3", u.TasksHits)
	}
}

func TestReadUsageDongLa(t *testing.T) {
	f := system.NewFake()
	l := layout(t)
	cg := l.CageCgroup()
	set := func(name, data string) { f.SetFile(cg+"/"+name, []byte(data), 0o644, system.Root) }
	set("memory.current", "abc\n")
	set("memory.peak", "")
	set("memory.events", "oom_kill\nmax x y\noom_kill 2\nrác\n")
	set("cpu.stat", "usage_usec 12 thừa\nusage_usec\nnr_throttled 4\n")
	set("cpu.max", "max 100000\n")
	set("pids.max", "max\n")
	set("io.stat", "8:0 rbytes=10 wbytes=x\n8:16 rbytes=5 wbytes=7 rác\nhỏng\n")
	set("io.max", "8:0 rbps=max wbps=123\n")
	set("pids.events", "max -\n")
	u, err := ReadUsage(f, l, tNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{
		Running: true, At: tNow,
		OOMKills: 2, NrThrottled: 4,
		CPUPeriod: 100000,
		IORBytes:  15, IOWBytes: 7,
		IOWBps: 123,
	}
	if u != want {
		t.Fatalf("\ngot  %+v\nwant %+v", u, want)
	}
}

// readErrSys làm ReadFile của một file lỗi khác ErrNotExist.
type readErrSys struct {
	*system.FakeSystem
	path string
}

func (r readErrSys) ReadFile(p string) ([]byte, error) {
	if p == r.path {
		return nil, &fs.PathError{Op: "open", Path: p, Err: syscall.EACCES}
	}
	return r.FakeSystem.ReadFile(p)
}

func TestReadUsageLoiDoc(t *testing.T) {
	f := system.NewFake()
	l := layout(t)
	loadSample(t, f, l, "dang-chay", nil)
	_, err := ReadUsage(readErrSys{f, l.CageCgroup() + "/memory.current"}, l, tNow)
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("lỗi = %v, muốn bọc EACCES", err)
	}
}
