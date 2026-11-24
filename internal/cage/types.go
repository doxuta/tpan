// Package cage chạy lệnh thay cho một site trong lồng của site đó, và đọc số
// liệu tài nguyên của lồng từ cgroup v2 (M17, mục 3.4 và 3.8 của
// docs/thay-doi/M17-thiet-ke.md; cuối mốc gộp vào mục 6.15 của docs/KE-HOACH.md).
//
// Gói lá: chỉ import system, site và thư viện chuẩn. checks và core cùng dùng
// được mà không tạo vòng import.
//
// Hai việc:
//
//   - Wrap bọc một system.System. Lệnh có AsUser là user của một site (tp_<slug>)
//     mà site đó có hàng rào trên máy (file site.CageFenceOfSlug(slug) tồn tại)
//     thì không chạy bằng `runuser` nữa mà bằng `systemd-run` thành một unit tạm
//     tpan-cage-<slug>-run-<16 hex>.service, nên nhận đúng hàng rào và slice của
//     site (drop-in theo tiền tố tên unit). Mọi lệnh khác đi thẳng xuống dưới.
//   - ReadUsage đọc các file cgroup của slice tpan-<slug>.slice.
package cage

import (
	"errors"
	"fmt"
	"time"
)

// ErrKilled: lệnh trong lồng không tự thoát mà bị dừng (chạm trần RAM, quá
// giờ, chết vì tín hiệu). Dùng errors.Is. Lỗi này KHÔNG phải *system.ExitError,
// để nơi gọi không đọc nhầm "bị giết" thành một mã thoát có nghĩa (ví dụ
// `wp core is-installed` trả 1 nghĩa là "chưa cài").
var ErrKilled = errors.New("lệnh trong lồng bị dừng trước khi tự thoát")

// KilledError mô tả lệnh bị dừng. Result là giá trị "Result" của unit tạm mà
// systemd báo (`systemctl show -p Result`): "oom-kill", "timeout", "signal",
// hoặc giá trị khác ngoài "success" và "exit-code". Status là ExecMainStatus
// (số tín hiệu khi bị giết bằng tín hiệu).
type KilledError struct {
	Cmd    string // dòng lệnh đã in làm bằng chứng (Describe)
	Unit   string // tên unit tạm
	Result string
	Status int
	Stderr string // vài dòng cuối của stderr
}

func (e *KilledError) Error() string {
	why := e.Result
	switch e.Result {
	case "oom-kill":
		why = "bị giết vì chạm trần RAM của lồng"
	case "timeout":
		why = "quá thời gian cho phép"
	case "signal":
		why = fmt.Sprintf("chết vì tín hiệu %d", e.Status)
	}
	return fmt.Sprintf("%s: %s (unit %s)", e.Cmd, why, e.Unit)
}

// Is để errors.Is(err, ErrKilled) đúng với mọi *KilledError.
func (e *KilledError) Is(target error) bool { return target == ErrKilled }

// Usage là số liệu của một lồng, đọc ngay từ cgroup (không qua bảng mẫu), nên
// con số hiện ra bằng con số trong file cgroup lúc đọc (bất biến 6).
type Usage struct {
	// Running: thư mục cgroup của slice có. Không có (lồng chưa chạy lần nào
	// từ khi máy lên, hoặc site không ở trong lồng) thì mọi số dưới là 0.
	Running bool      `json:"running"`
	At      time.Time `json:"at"`

	MemCurrent int64 `json:"mem_current"` // byte, memory.current
	MemPeak    int64 `json:"mem_peak"`    // byte, memory.peak (từ khi slice có)
	OOMKills   int64 `json:"oom_kills"`   // memory.events: oom_kill
	MemMaxHits int64 `json:"mem_max_hits"`

	CPUUsec       int64 `json:"cpu_usec"`       // cpu.stat: usage_usec
	ThrottledUsec int64 `json:"throttled_usec"` // cpu.stat: throttled_usec (0 khi không có trần CPU)
	NrThrottled   int64 `json:"nr_throttled"`   // cpu.stat: nr_throttled

	Tasks     int64 `json:"tasks"`      // pids.current
	TasksPeak int64 `json:"tasks_peak"` // pids.peak
	// TasksHits: số lần chạm trần tiến trình. Kernel có pids.events.local thì
	// đọc pids.events của slice; không có (kernel 6.8) thì cộng "max" trong
	// pids.events của slice và mọi cgroup con trực tiếp (t3b: bộ đếm nằm ở
	// cgroup nơi gọi fork).
	TasksHits int64 `json:"tasks_hits"`

	IORBytes int64 `json:"io_rbytes"` // io.stat: tổng rbytes mọi thiết bị
	IOWBytes int64 `json:"io_wbytes"` // io.stat: tổng wbytes

	// Trần đang áp, đọc từ chính cgroup (để so với gói: phép kiểm cage.limits).
	// 0 nghĩa là "max" (không giới hạn).
	MemMax     int64 `json:"mem_max"`      // byte, memory.max
	SwapMax    int64 `json:"swap_max"`     // byte, memory.swap.max (-1 khi "max")
	CPUQuotaUs int64 `json:"cpu_quota_us"` // cpu.max: số đầu (micro giây mỗi chu kỳ), 0 khi "max"
	CPUPeriod  int64 `json:"cpu_period_us"`
	TasksMax   int64 `json:"tasks_max"`   // pids.max
	IORBps     int64 `json:"io_rbps_max"` // io.max: rbps của thiết bị đầu tiên có dòng, 0 khi "max" hay không có
	IOWBps     int64 `json:"io_wbps_max"`
}

// Sample là một mẫu 5 phút của một site, một dòng của bảng usage_samples (mục
// 3.8). Các trường đếm dồn (OOMKills, CPUUsec, ThrottledUsec, NrThrottled,
// TasksHits, IORBytes, IOWBytes) là giá trị của bộ đếm lúc lấy mẫu, không phải
// phần tăng: phần tăng tính khi dùng (core), và bộ đếm nhỏ hơn mẫu trước nghĩa
// là nó vừa về 0 (máy khởi động lại, t7b).
type Sample struct {
	Domain        string    `json:"domain"`
	At            time.Time `json:"at"`
	Running       bool      `json:"running"`
	MemCurrent    int64     `json:"mem_current"`
	MemPeak       int64     `json:"mem_peak"`
	OOMKills      int64     `json:"oom_kills"`
	CPUUsec       int64     `json:"cpu_usec"`
	ThrottledUsec int64     `json:"throttled_usec"`
	NrThrottled   int64     `json:"nr_throttled"`
	Tasks         int64     `json:"tasks"`
	TasksPeak     int64     `json:"tasks_peak"`
	TasksHits     int64     `json:"tasks_hits"`
	IORBytes      int64     `json:"io_rbytes"`
	IOWBytes      int64     `json:"io_wbytes"`
	// Trần đang áp lúc lấy mẫu; 0 là không giới hạn.
	MemMax      int64 `json:"mem_max"`
	CPUQuotaPct int64 `json:"cpu_quota_pct"` // % một nhân
	TasksMax    int64 `json:"tasks_max"`
}

// SampleOf dựng mẫu từ một lần đọc cgroup.
func SampleOf(domain string, u Usage) Sample {
	s := Sample{
		Domain: domain, At: u.At, Running: u.Running,
		MemCurrent: u.MemCurrent, MemPeak: u.MemPeak, OOMKills: u.OOMKills,
		CPUUsec: u.CPUUsec, ThrottledUsec: u.ThrottledUsec, NrThrottled: u.NrThrottled,
		Tasks: u.Tasks, TasksPeak: u.TasksPeak, TasksHits: u.TasksHits,
		IORBytes: u.IORBytes, IOWBytes: u.IOWBytes,
		MemMax: u.MemMax, TasksMax: u.TasksMax,
	}
	if u.CPUQuotaUs > 0 && u.CPUPeriod > 0 {
		s.CPUQuotaPct = u.CPUQuotaUs * 100 / u.CPUPeriod
	}
	return s
}

// Hour là dòng gộp một giờ của một site (bảng usage_hourly). Các trường "tăng"
// là tổng phần tăng giữa các mẫu liên tiếp trong giờ (đã xử lý bộ đếm về 0).
type Hour struct {
	Domain  string    `json:"domain"`
	Hour    time.Time `json:"hour"` // UTC, đầu giờ
	Samples int       `json:"samples"`

	MemAvg int64 `json:"mem_avg"` // trung bình MemCurrent
	MemMax int64 `json:"mem_max"` // lớn nhất của max(MemCurrent, MemPeak) trong giờ

	CPUUsec   int64   `json:"cpu_usec"`    // tăng
	CPUPctMax float64 `json:"cpu_pct_max"` // phần trăm (của một nhân) lớn nhất của một khoảng giữa hai mẫu

	ThrottledUsec int64 `json:"throttled_usec"` // tăng
	NrThrottled   int64 `json:"nr_throttled"`   // tăng
	TasksMax      int64 `json:"tasks_max"`      // lớn nhất của max(Tasks, TasksPeak)
	OOMKills      int64 `json:"oom_kills"`      // tăng
	TasksHits     int64 `json:"tasks_hits"`     // tăng
	IORBytes      int64 `json:"io_rbytes"`      // tăng
	IOWBytes      int64 `json:"io_wbytes"`      // tăng
}

// ProbeRun là một dòng của sổ phép thử tấn công (bảng probe_runs, mục 3.9).
// Dòng được ghi TRƯỚC khi tạo bất cứ thứ gì trên máy; Cleaned chỉ thành true
// khi mọi thứ mang hai tên miền này đã được dọn.
type ProbeRun struct {
	ID       string    `json:"id"` // 16 ký tự hex
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	// State: "running", "done", "failed".
	State   string `json:"state"`
	DomainA string `json:"domain_a"`
	DomainB string `json:"domain_b"`
	// Mode: "" (cả hai lượt), "ngoai", "trong".
	Mode    string `json:"mode"`
	Cleaned bool   `json:"cleaned"`
	// Report là core.ProbeReport dạng JSON (rỗng khi chưa xong).
	Report []byte `json:"-"`
}
