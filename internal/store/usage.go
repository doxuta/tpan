package store

// Số liệu tài nguyên của site trong lồng (M17, migration 1700; mục 3.8 của
// docs/thay-doi/M17-thiet-ke.md): mẫu 5 phút (usage_samples) và dòng gộp theo
// giờ (usage_hourly). Các method ở đây cài đặt core.UsageStore; store không
// import core nên khẳng định "đúng chữ ký" nằm trong usage_test.go.
//
// Cột thời gian là chuỗi do fmtTime sinh: UTC, độ rộng cố định, nên so sánh
// chuỗi trong SQL (>= from AND < to) cho đúng thứ tự thời gian.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/cage"
)

const sampleColumns = "domain, at, running, mem_current, mem_peak, oom_kills, cpu_usec, throttled_usec, nr_throttled, " +
	"tasks, tasks_peak, tasks_hits, io_rbytes, io_wbytes, mem_max, cpu_quota_pct, tasks_max"

// AddUsageSample thêm một mẫu. Trùng (domain, at) là lỗi: hai lần lấy mẫu cùng
// một khoảnh khắc cho một site là lỗi của bộ lấy mẫu, không nên bị che.
func (s *Store) AddUsageSample(ctx context.Context, sm cage.Sample) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO usage_samples ("+sampleColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		sm.Domain, fmtTime(sm.At), siteBool(sm.Running), sm.MemCurrent, sm.MemPeak, sm.OOMKills,
		sm.CPUUsec, sm.ThrottledUsec, sm.NrThrottled, sm.Tasks, sm.TasksPeak, sm.TasksHits,
		sm.IORBytes, sm.IOWBytes, sm.MemMax, sm.CPUQuotaPct, sm.TasksMax)
	if err != nil {
		return fmt.Errorf("lưu mẫu số liệu của %s lúc %s: %w", sm.Domain, fmtTime(sm.At), err)
	}
	return nil
}

// UsageSamples trả mẫu của domain có at trong [from, to), sắp theo at. Luôn là
// slice không nil.
func (s *Store) UsageSamples(ctx context.Context, domain string, from, to time.Time) ([]cage.Sample, error) {
	out := []cage.Sample{}
	err := queryEach(ctx, s.db,
		"SELECT "+sampleColumns+" FROM usage_samples WHERE domain = ? AND at >= ? AND at < ? ORDER BY at",
		func(r rowScanner) error {
			sm, err := scanSample(r)
			if err != nil {
				return err
			}
			out = append(out, sm)
			return nil
		}, domain, fmtTime(from), fmtTime(to))
	if err != nil {
		return nil, fmt.Errorf("đọc mẫu số liệu của %s: %w", domain, err)
	}
	return out, nil
}

func scanSample(r rowScanner) (cage.Sample, error) {
	var (
		sm      cage.Sample
		at      string
		running int
	)
	if err := r.Scan(&sm.Domain, &at, &running, &sm.MemCurrent, &sm.MemPeak, &sm.OOMKills,
		&sm.CPUUsec, &sm.ThrottledUsec, &sm.NrThrottled, &sm.Tasks, &sm.TasksPeak, &sm.TasksHits,
		&sm.IORBytes, &sm.IOWBytes, &sm.MemMax, &sm.CPUQuotaPct, &sm.TasksMax); err != nil {
		return cage.Sample{}, err
	}
	var err error
	if sm.At, err = parseTime(at); err != nil {
		return cage.Sample{}, fmt.Errorf("mẫu của %s: %w", sm.Domain, err)
	}
	sm.Running = running != 0
	return sm, nil
}

// UsageSampleDomains trả các domain có ít nhất một mẫu trong [from, to), sắp
// theo tên. Luôn là slice không nil.
func (s *Store) UsageSampleDomains(ctx context.Context, from, to time.Time) ([]string, error) {
	out := []string{}
	err := queryEach(ctx, s.db,
		"SELECT DISTINCT domain FROM usage_samples WHERE at >= ? AND at < ? ORDER BY domain",
		func(r rowScanner) error {
			var d string
			if err := r.Scan(&d); err != nil {
				return err
			}
			out = append(out, d)
			return nil
		}, fmtTime(from), fmtTime(to))
	if err != nil {
		return nil, fmt.Errorf("đọc các domain có mẫu số liệu: %w", err)
	}
	return out, nil
}

const hourColumns = "domain, hour, samples, mem_avg, mem_max, cpu_usec, cpu_pct_max, throttled_usec, nr_throttled, " +
	"tasks_max, oom_kills, tasks_hits, io_rbytes, io_wbytes"

// SaveUsageHour thêm hoặc ghi đè dòng (domain, hour): bộ gộp tính lại cả giờ
// mỗi lần chạy nên ghi đè là cách lưu đúng, không phải cộng dồn.
func (s *Store) SaveUsageHour(ctx context.Context, h cage.Hour) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO usage_hourly (`+hourColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (domain, hour) DO UPDATE SET
		   samples = excluded.samples, mem_avg = excluded.mem_avg, mem_max = excluded.mem_max,
		   cpu_usec = excluded.cpu_usec, cpu_pct_max = excluded.cpu_pct_max,
		   throttled_usec = excluded.throttled_usec, nr_throttled = excluded.nr_throttled,
		   tasks_max = excluded.tasks_max, oom_kills = excluded.oom_kills, tasks_hits = excluded.tasks_hits,
		   io_rbytes = excluded.io_rbytes, io_wbytes = excluded.io_wbytes`,
		h.Domain, fmtTime(h.Hour), h.Samples, h.MemAvg, h.MemMax, h.CPUUsec, h.CPUPctMax,
		h.ThrottledUsec, h.NrThrottled, h.TasksMax, h.OOMKills, h.TasksHits, h.IORBytes, h.IOWBytes)
	if err != nil {
		return fmt.Errorf("lưu dòng giờ của %s lúc %s: %w", h.Domain, fmtTime(h.Hour), err)
	}
	return nil
}

// UsageHours trả dòng gộp của domain có hour trong [from, to), sắp theo hour.
// Luôn là slice không nil.
func (s *Store) UsageHours(ctx context.Context, domain string, from, to time.Time) ([]cage.Hour, error) {
	out := []cage.Hour{}
	err := queryEach(ctx, s.db,
		"SELECT "+hourColumns+" FROM usage_hourly WHERE domain = ? AND hour >= ? AND hour < ? ORDER BY hour",
		func(r rowScanner) error {
			var (
				h    cage.Hour
				hour string
			)
			if err := r.Scan(&h.Domain, &hour, &h.Samples, &h.MemAvg, &h.MemMax, &h.CPUUsec, &h.CPUPctMax,
				&h.ThrottledUsec, &h.NrThrottled, &h.TasksMax, &h.OOMKills, &h.TasksHits, &h.IORBytes, &h.IOWBytes); err != nil {
				return err
			}
			var err error
			if h.Hour, err = parseTime(hour); err != nil {
				return fmt.Errorf("dòng giờ của %s: %w", h.Domain, err)
			}
			out = append(out, h)
			return nil
		}, domain, fmtTime(from), fmtTime(to))
	if err != nil {
		return nil, fmt.Errorf("đọc dòng giờ của %s: %w", domain, err)
	}
	return out, nil
}

// PruneUsage xoá mẫu có at < samplesBefore và dòng giờ có hour < hoursBefore,
// trong một transaction: lịch xoá không bao giờ dừng nửa chừng giữa hai bảng.
func (s *Store) PruneUsage(ctx context.Context, samplesBefore, hoursBefore time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM usage_samples WHERE at < ?", fmtTime(samplesBefore)); err != nil {
			return fmt.Errorf("xoá mẫu số liệu cũ: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM usage_hourly WHERE hour < ?", fmtTime(hoursBefore)); err != nil {
			return fmt.Errorf("xoá dòng giờ cũ: %w", err)
		}
		return nil
	})
}

// DeleteUsageOf xoá mọi mẫu và dòng giờ của một domain: site bị xoá hẳn thì
// site mới cùng tên không được thừa hưởng số cũ (bảng không có khoá ngoại tới
// sites, nên không tự xoá theo). Không có số liệu nào thì không lỗi.
func (s *Store) DeleteUsageOf(ctx context.Context, domain string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM usage_samples WHERE domain = ?", domain); err != nil {
			return fmt.Errorf("xoá mẫu số liệu của %s: %w", domain, err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM usage_hourly WHERE domain = ?", domain); err != nil {
			return fmt.Errorf("xoá dòng giờ của %s: %w", domain, err)
		}
		return nil
	})
}
