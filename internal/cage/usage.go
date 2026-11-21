package cage

// Mục 3.8 của docs/thay-doi/M17-thiet-ke.md. Mẫu file thật (kernel 6.8, systemd
// 255) ở docs/bang-chung/M17-nghien-cuu/cgroup-mau/ (dang-chay: lồng đang chạy,
// không có trần đĩa; cham-tran: vừa chạm trần RAM và trần tiến trình), chép vào
// testdata/ cho test.

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// ReadUsage đọc số liệu của lồng của l từ l.CageCgroup() bằng sys.ReadFile và
// sys.ReadDir (không chạy lệnh). Thư mục không có: Usage{Running: false, At:
// now}, không lỗi. Một file thiếu hay dòng lạ thì bỏ số đó (để 0), không panic,
// không lỗi; lỗi đọc khác fs.ErrNotExist thì trả lỗi. now ghi vào Usage.At.
func ReadUsage(sys system.System, l site.Layout, now time.Time) (Usage, error) {
	dir := l.CageCgroup()
	u := Usage{At: now}
	if _, err := sys.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return u, nil
		}
		return u, fmt.Errorf("đọc cgroup của lồng %s: %w", dir, err)
	}
	u.Running = true
	r := cgReader{sys: sys, dir: dir}

	u.MemCurrent = r.single("memory.current", 0)
	u.MemPeak = r.single("memory.peak", 0)
	u.MemMax = r.single("memory.max", 0)
	u.SwapMax = r.single("memory.swap.max", -1)
	ev := r.keyed("memory.events")
	u.OOMKills, u.MemMaxHits = ev["oom_kill"], ev["max"]

	cs := r.keyed("cpu.stat")
	u.CPUUsec, u.ThrottledUsec, u.NrThrottled = cs["usage_usec"], cs["throttled_usec"], cs["nr_throttled"]
	// cpu.max "<quota|max> <chu kỳ>"; không có file (không đặt CPUQuota, t14
	// P11) là không giới hạn.
	if f := strings.Fields(r.read("cpu.max")); len(f) == 2 {
		u.CPUQuotaUs, u.CPUPeriod = num(f[0], 0), num(f[1], 0)
	}

	u.Tasks = r.single("pids.current", 0)
	u.TasksPeak = r.single("pids.peak", 0)
	u.TasksMax = r.single("pids.max", 0)
	u.TasksHits = r.tasksHits()

	// io.stat: một dòng mỗi thiết bị, "<maj:min> rbytes=.. wbytes=.. ...".
	for _, line := range strings.Split(r.read("io.stat"), "\n") {
		kv := pairs(line)
		u.IORBytes += num(kv["rbytes"], 0)
		u.IOWBytes += num(kv["wbytes"], 0)
	}
	// io.max: chỉ so rbps, wbps của dòng đầu (systemd áp lên cả đĩa, mục 3.7);
	// rỗng là không có trần đĩa.
	for _, line := range strings.Split(r.read("io.max"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		kv := pairs(line)
		u.IORBps, u.IOWBps = num(kv["rbps"], 0), num(kv["wbps"], 0)
		break
	}
	return u, r.err
}

// cgReader đọc file trong một thư mục cgroup và giữ lỗi đầu tiên khác
// ErrNotExist, để ReadUsage đọc tuần tự mà không lặp kiểm lỗi ở từng dòng.
type cgReader struct {
	sys system.System
	dir string
	err error
}

// read trả nội dung file name; file không có thì rỗng.
func (r *cgReader) read(name string) string {
	return r.readPath(r.dir + "/" + name)
}

func (r *cgReader) readPath(p string) string {
	b, err := r.sys.ReadFile(p)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) && r.err == nil {
			r.err = fmt.Errorf("đọc %s: %w", p, err)
		}
		return ""
	}
	return string(b)
}

// single đọc file một số; "max" đổi thành unlimited.
func (r *cgReader) single(name string, unlimited int64) int64 {
	return num(strings.TrimSpace(r.read(name)), unlimited)
}

// keyed đọc file dạng "khoá số" mỗi dòng (memory.events, cpu.stat,
// pids.events). Dòng không đúng hai trường thì bỏ.
func (r *cgReader) keyed(name string) map[string]int64 {
	return keyedOf(r.read(name))
}

// tasksHits đếm số lần chạm trần tiến trình (Usage.TasksHits).
func (r *cgReader) tasksHits() int64 {
	if _, err := r.sys.Stat(r.dir + "/pids.events.local"); err == nil {
		return r.keyed("pids.events")["max"]
	}
	// Kernel 6.8: bộ đếm nằm ở cgroup nơi gọi fork (t3b), nên cộng của slice và
	// mọi cgroup con trực tiếp. Chỉ đi vào thư mục: file của slice không có
	// pids.events con (đọc thử ra ENOTDIR, không phải ErrNotExist).
	n := r.keyed("pids.events")["max"]
	names, err := r.sys.ReadDir(r.dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) && r.err == nil {
			r.err = fmt.Errorf("liệt kê %s: %w", r.dir, err)
		}
		return n
	}
	for _, name := range names {
		p := r.dir + "/" + name
		if info, err := r.sys.Stat(p); err != nil || !info.Mode.IsDir() {
			continue
		}
		n += keyedOf(r.readPath(p + "/pids.events"))["max"]
	}
	return n
}

func keyedOf(s string) map[string]int64 {
	m := map[string]int64{}
	for _, line := range strings.Split(s, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			m[f[0]] = num(f[1], 0)
		}
	}
	return m
}

// pairs tách "k=v" trong một dòng; trường không có "=" thì bỏ.
func pairs(line string) map[string]string {
	m := map[string]string{}
	for _, f := range strings.Fields(line) {
		if k, v, ok := strings.Cut(f, "="); ok {
			m[k] = v
		}
	}
	return m
}

// num đọc một số nguyên; "max" là unlimited; rỗng hay hỏng là 0.
func num(s string, unlimited int64) int64 {
	if s == "max" {
		return unlimited
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
