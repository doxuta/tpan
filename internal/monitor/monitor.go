// Package monitor đọc số liệu máy cho Bảng điều khiển: CPU, RAM, swap, tải,
// đĩa, thời gian chạy (mục 10 của docs/KE-HOACH.md, "số liệu máy").
//
// Mọi thứ đọc qua system.System (ReadFile trên /proc, StatFS), không qua os,
// để chế độ --demo dùng FakeSystem với file /proc giả. Định dạng /proc theo
// man proc(5) và tài liệu kernel:
//   - https://man7.org/linux/man-pages/man5/proc_stat.5.html
//   - https://man7.org/linux/man-pages/man5/proc_loadavg.5.html
//   - https://man7.org/linux/man-pages/man5/proc_uptime.5.html
//   - https://docs.kernel.org/filesystems/proc.html (/proc/meminfo, đơn vị kB)
//   - account_guest_time trong kernel/sched/cputime.c: thời gian guest được
//     cộng cả vào user (hoặc nice), nên không cộng guest thêm lần nữa.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// CPUSample là khoảng cách giữa hai lần đọc /proc/stat. /proc/stat chỉ có số
// đếm cộng dồn từ lúc khởi động, nên phần trăm CPU "lúc này" phải lấy hiệu của
// hai lần đọc. 200ms đủ để ra một con số có nghĩa mà request không chậm thấy rõ.
const CPUSample = 200 * time.Millisecond

// DiskPath là chỗ đo đĩa: phân vùng chứa các site. Máy chưa có /var/www thì đo "/".
const DiskPath = "/var/www"

// Machine là số liệu đọc được. Byte cho bộ nhớ và đĩa. server đổi sang
// server.Machine (monitor không import server).
type Machine struct {
	CPUPercent float64 // -1 khi không đo được
	CPUs       int
	Load1      float64
	MemTotal   uint64
	MemUsed    uint64 // MemTotal - MemAvailable
	SwapTotal  uint64
	SwapUsed   uint64
	DiskPath   string
	DiskTotal  uint64
	DiskUsed   uint64
	Uptime     float64 // giây
	// Errors: phần nào không đọc được, mỗi phần một dòng. Một phần lỗi không
	// làm hỏng các phần còn lại.
	Errors []string
}

// Read đọc số liệu máy; mất khoảng CPUSample để đo CPU.
func Read(ctx context.Context, sys system.System) Machine {
	return read(ctx, sys, func(ctx context.Context) error {
		t := time.NewTimer(CPUSample)
		defer t.Stop()
		select {
		case <-t.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

// read là Read với cách chờ giữa hai lần đọc /proc/stat thay được: test đổi
// nội dung /proc/stat giả trong lúc "chờ".
func read(ctx context.Context, sys system.System, wait func(context.Context) error) Machine {
	m := Machine{CPUPercent: -1}
	fail := func(part string, err error) { m.Errors = append(m.Errors, part+": "+err.Error()) }

	if err := readCPU(ctx, sys, wait, &m); err != nil {
		fail("cpu", err)
	}
	if err := readMem(sys, &m); err != nil {
		fail("memory", err)
	}
	if v, err := firstFloat(sys, "/proc/loadavg"); err != nil {
		fail("load", err)
	} else {
		m.Load1 = v
	}
	if v, err := firstFloat(sys, "/proc/uptime"); err != nil {
		fail("uptime", err)
	} else {
		m.Uptime = v
	}
	if err := readDisk(sys, &m); err != nil {
		fail("disk", err)
	}
	return m
}

// cpuTimes là dòng "cpu" tổng của /proc/stat.
type cpuTimes struct{ busy, total uint64 }

// parseStat đọc dòng "cpu" tổng và đếm các dòng "cpuN". Lấy 8 cột đầu (user
// nice system idle iowait irq softirq steal): kernel cộng guest vào user và
// guest_nice vào nice (account_guest_time), nên cộng thêm là đếm hai lần.
// idle và iowait là rảnh.
func parseStat(data []byte) (cpuTimes, int, error) {
	var t cpuTimes
	found, cpus := false, 0
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		if f[0] != "cpu" {
			cpus++
			continue
		}
		if len(f) < 9 {
			return t, 0, fmt.Errorf("dòng cpu của /proc/stat thiếu cột: %q", strings.TrimSpace(line))
		}
		var v [8]uint64
		for i := range v {
			n, err := strconv.ParseUint(f[i+1], 10, 64)
			if err != nil {
				return t, 0, fmt.Errorf("cột %d của dòng cpu trong /proc/stat: %w", i+1, err)
			}
			v[i] = n
			t.total += n
		}
		t.busy = t.total - v[3] - v[4]
		found = true
	}
	if !found {
		return t, 0, errors.New("/proc/stat không có dòng cpu")
	}
	return t, cpus, nil
}

// cpuPercent là phần trăm bận giữa hai lần đọc; -1 khi số đếm không tăng
// (không chia cho 0) hoặc bị đếm lại từ đầu.
func cpuPercent(a, b cpuTimes) float64 {
	if b.total <= a.total || b.busy < a.busy {
		return -1
	}
	return float64(b.busy-a.busy) * 100 / float64(b.total-a.total)
}

func readCPU(ctx context.Context, sys system.System, wait func(context.Context) error, m *Machine) error {
	first, err := sys.ReadFile("/proc/stat")
	if err != nil {
		return err
	}
	a, cpus, err := parseStat(first)
	if err != nil {
		return err
	}
	m.CPUs = cpus
	if err := wait(ctx); err != nil {
		return fmt.Errorf("chờ lần đọc thứ hai: %w", err)
	}
	second, err := sys.ReadFile("/proc/stat")
	if err != nil {
		return err
	}
	b, _, err := parseStat(second)
	if err != nil {
		return err
	}
	m.CPUPercent = cpuPercent(a, b)
	return nil
}

// readMem đọc /proc/meminfo. Số trong đó tính bằng kB (thật ra là KiB).
// Dùng MemAvailable chứ không MemFree: MemFree không tính page cache mà kernel
// trả lại được ngay, nên máy khoẻ vẫn trông như sắp hết RAM.
func readMem(sys system.System, m *Machine) error {
	data, err := sys.ReadFile("/proc/meminfo")
	if err != nil {
		return err
	}
	v := map[string]uint64{}
	for line := range strings.Lines(string(data)) {
		k, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		if n, err := strconv.ParseUint(f[0], 10, 64); err == nil {
			v[k] = n * 1024
		}
	}
	for _, k := range []string{"MemTotal", "MemAvailable", "SwapTotal", "SwapFree"} {
		if _, ok := v[k]; !ok {
			return fmt.Errorf("/proc/meminfo thiếu %s", k)
		}
	}
	m.MemTotal, m.SwapTotal = v["MemTotal"], v["SwapTotal"]
	m.MemUsed = sub(v["MemTotal"], v["MemAvailable"])
	m.SwapUsed = sub(v["SwapTotal"], v["SwapFree"])
	return nil
}

// firstFloat đọc số đầu tiên của file (/proc/loadavg: tải 1 phút; /proc/uptime: giây).
func firstFloat(sys system.System, path string) (float64, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(data))
	if len(f) == 0 {
		return 0, fmt.Errorf("%s rỗng", path)
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, fmt.Errorf("đọc %s: %w", path, err)
	}
	return v, nil
}

// readDisk đo phân vùng chứa DiskPath. Used = Total - Free, mà Free là phần
// user thường dùng được (cột Avail của df), nên Used tính cả phần dành riêng
// cho root (thường 5%): con số hơi cao hơn cột Used của df, nghiêng về phía
// báo động sớm, đúng ý một thanh có vạch ngưỡng.
func readDisk(sys system.System, m *Machine) error {
	p := DiskPath
	du, err := sys.StatFS(p)
	if errors.Is(err, fs.ErrNotExist) {
		p = "/"
		du, err = sys.StatFS(p)
	}
	if err != nil {
		return fmt.Errorf("đo đĩa %s: %w", p, err)
	}
	m.DiskPath, m.DiskTotal, m.DiskUsed = p, du.Total, sub(du.Total, du.Free)
	return nil
}

func sub(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}
