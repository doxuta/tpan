package monitor

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

const (
	statA = "cpu  100 0 100 700 100 0 0 0 50 0\ncpu0 50 0 50 350 50 0 0 0 25 0\ncpu1 50 0 50 350 50 0 0 0 25 0\nintr 1 2 3\nctxt 5\n"
	// Sau "200ms": thêm 100 bận (user 60, system 40) và 300 rảnh (idle 250, iowait 50).
	statB    = "cpu  160 0 140 950 150 0 0 0 90 0\ncpu0 80 0 70 475 75 0 0 0 45 0\ncpu1 80 0 70 475 75 0 0 0 45 0\n"
	meminfo  = "MemTotal:        4000000 kB\nMemFree:          500000 kB\nMemAvailable:    3000000 kB\nSwapTotal:       2000000 kB\nSwapFree:        1500000 kB\n"
	loadavg  = "0.42 0.30 0.20 1/123 4567\n"
	uptime   = "3600.50 7000.00\n"
	diskSize = 100 << 30
)

func monitorFake(t *testing.T) *system.FakeSystem {
	t.Helper()
	f := system.NewFake()
	f.SetFile("/proc/stat", []byte(statA), 0o444, system.Root)
	f.SetFile("/proc/meminfo", []byte(meminfo), 0o444, system.Root)
	f.SetFile("/proc/loadavg", []byte(loadavg), 0o444, system.Root)
	f.SetFile("/proc/uptime", []byte(uptime), 0o444, system.Root)
	f.SetDir("/var/www", 0o755, system.Root)
	f.SetDisk("/var/www", system.DiskUsage{Total: diskSize, Free: 25 << 30})
	return f
}

func TestRead(t *testing.T) {
	f := monitorFake(t)
	m := read(context.Background(), f, func(context.Context) error {
		f.SetFile("/proc/stat", []byte(statB), 0o444, system.Root)
		return nil
	})
	if len(m.Errors) != 0 {
		t.Fatalf("Errors = %v", m.Errors)
	}
	if math.Abs(m.CPUPercent-25) > 1e-9 {
		t.Errorf("CPUPercent = %v, muốn 25 (100 bận / 400 tổng)", m.CPUPercent)
	}
	if m.CPUs != 2 {
		t.Errorf("CPUs = %d, muốn 2", m.CPUs)
	}
	if m.MemTotal != 4000000*1024 || m.MemUsed != 1000000*1024 {
		t.Errorf("RAM = %d/%d, muốn dùng 1000000 kB trên 4000000 kB (theo MemAvailable)", m.MemUsed, m.MemTotal)
	}
	if m.SwapTotal != 2000000*1024 || m.SwapUsed != 500000*1024 {
		t.Errorf("swap = %d/%d", m.SwapUsed, m.SwapTotal)
	}
	if m.Load1 != 0.42 || m.Uptime != 3600.5 {
		t.Errorf("Load1 = %v, Uptime = %v", m.Load1, m.Uptime)
	}
	if m.DiskPath != "/var/www" || m.DiskTotal != diskSize || m.DiskUsed != 75<<30 {
		t.Errorf("đĩa = %s %d/%d", m.DiskPath, m.DiskUsed, m.DiskTotal)
	}
}

func TestReadPartialFailure(t *testing.T) {
	f := system.NewFake()
	// Chỉ có meminfo; không có /var/www nên đo "/".
	f.SetFile("/proc/meminfo", []byte(meminfo), 0o444, system.Root)
	m := read(context.Background(), f, func(context.Context) error { return nil })
	if m.MemTotal == 0 {
		t.Error("RAM đọc được mà vẫn bị bỏ")
	}
	if m.CPUPercent != -1 {
		t.Errorf("CPUPercent = %v, muốn -1 khi không đọc được", m.CPUPercent)
	}
	if m.DiskPath != "/" || m.DiskTotal == 0 {
		t.Errorf("không có /var/www thì phải đo /: %s %d", m.DiskPath, m.DiskTotal)
	}
	joined := strings.Join(m.Errors, "\n")
	for _, part := range []string{"cpu:", "load:", "uptime:"} {
		if !strings.Contains(joined, part) {
			t.Errorf("Errors thiếu %q: %v", part, m.Errors)
		}
	}
	if strings.Contains(joined, "memory") || strings.Contains(joined, "disk") {
		t.Errorf("phần đọc được lại bị báo lỗi: %v", m.Errors)
	}
}

func TestReadCPUNoChangeOrCancelled(t *testing.T) {
	f := monitorFake(t)
	m := read(context.Background(), f, func(context.Context) error { return nil })
	if m.CPUPercent != -1 {
		t.Errorf("số đếm không đổi: CPUPercent = %v, muốn -1 (không chia cho 0)", m.CPUPercent)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m = Read(ctx, f)
	if m.CPUPercent != -1 || len(m.Errors) != 1 || !strings.HasPrefix(m.Errors[0], "cpu:") {
		t.Errorf("ctx đã huỷ: CPUPercent = %v, Errors = %v", m.CPUPercent, m.Errors)
	}
	if m.MemTotal == 0 {
		t.Error("ctx huỷ chỉ được làm hỏng phần CPU")
	}
}

func TestParseStatBad(t *testing.T) {
	for _, in := range []string{"", "intr 1\n", "cpu 1 2 3\n", "cpu a b c d e f g h i\n"} {
		if _, _, err := parseStat([]byte(in)); err == nil {
			t.Errorf("parseStat(%q) không báo lỗi", in)
		}
	}
	if _, _, err := parseStat([]byte(statA)); err != nil {
		t.Fatal(err)
	}
}
