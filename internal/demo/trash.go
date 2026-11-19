package demo

// Phần thùng rác của máy giả (M6, mục 6.4 của docs/KE-HOACH.md): khoá và mở
// user DB, hạn tài khoản của user Linux, và một site có sẵn trong thùng rác để
// màn Thùng rác có gì để xem, khôi phục, xoá hẳn.
//
// Các lệnh khác mà thùng rác và job hàng loạt dùng đã được máy giả trả lời từ
// trước: chuyển thư mục là system.Rename (FakeSystem làm thật trong bộ nhớ),
// xoá hẳn là DROP DATABASE/USER (mariadb), rm -r, userdel, certbot delete;
// kiểm cú pháp (nginx -t, php-fpm8.3 -t) là lệnh không có handler nên thoát 0;
// reload là handler systemctl.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// siteOld là site đang ở thùng rác của máy demo.
	siteOld = "old.example.net"
	// oldTrashedAgo: site cũ vào thùng rác cách đây chừng này; với số ngày
	// giữ mặc định (7) thì còn 4 ngày tới hạn xoá hẳn, bộ hẹn giờ không xoá nó
	// trong lúc người dùng xem demo.
	oldTrashedAgo = 3 * 24 * time.Hour
)

var (
	// Câu đọc trạng thái khoá của steps (dbLockStateSQL): "SELECT COUNT(*),
	// ... '$.account_locked' ... FROM mysql.global_priv WHERE User='<tên>' ...".
	// Phải có account_locked: câu trạng thái DB của site cũng hỏi global_priv.
	sqlLockState = regexp.MustCompile(`account_locked.*FROM mysql\.global_priv WHERE User='([a-z0-9_]+)'`)
	sqlLock      = regexp.MustCompile(`ALTER USER '([a-z0-9_]+)'@'localhost' ACCOUNT (LOCK|UNLOCK)`)
)

// mariadbLock trả lời câu đọc trạng thái khoá và câu khoá, mở khoá user DB.
// ok=false: không phải câu của nó. Người gọi giữ m.mu.
func (m *machine) mariadbLock(sql string) (system.Result, bool) {
	if g := sqlLockState.FindStringSubmatch(sql); g != nil {
		exists, locked := 0, 0
		if m.dbUsers[g[1]] {
			exists = 1
			if m.dbLocked[g[1]] {
				locked = 1
			}
		}
		return exit(0, fmt.Sprintf("%d\t%d\n", exists, locked), ""), true
	}
	if g := sqlLock.FindStringSubmatch(sql); g != nil {
		if !m.dbUsers[g[1]] {
			return exit(1, "", "ERROR 1396 (HY000) at line 1: Operation ALTER USER failed for '"+g[1]+"'@'localhost'\n"), true
		}
		m.dbLocked[g[1]] = g[2] == "LOCK"
		return okResult, true
	}
	return system.Result{}, false
}

// trashHandlers đăng ký chage -l và usermod --expiredate (khoá user Linux bằng
// hạn tài khoản, steps.SiteUserLock).
func (m *machine) trashHandlers() {
	m.Handle([]string{"chage", "-l"}, func(c system.Cmd) system.Result {
		name := c.Args[len(c.Args)-1]
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, found := m.users[name]; !found {
			return exit(1, "", "chage: user '"+name+"' does not exist in /etc/passwd\n")
		}
		exp := m.expired[name]
		if exp == "" {
			exp = "never"
		}
		// Dạng của chage thật (LC_ALL=C): tên cột, các tab, ": giá trị".
		return exit(0, strings.Join([]string{
			"Last password change\t\t\t\t\t: Nov 01, 2026",
			"Password expires\t\t\t\t\t: never",
			"Password inactive\t\t\t\t\t: never",
			"Account expires\t\t\t\t\t\t: " + exp,
			"Minimum number of days between password change\t\t: 0",
			"Maximum number of days between password change\t\t: 99999",
			"Number of days of warning before password expires\t: 7",
		}, "\n")+"\n", "")
	})
	// usermod --expiredate 1 <user>: hết hạn từ 2/1/1970 (khoá);
	// usermod --expiredate "" <user>: bỏ hạn (mở khoá).
	m.Handle([]string{"usermod", "--expiredate"}, func(c system.Cmd) system.Result {
		if len(c.Args) != 3 {
			return exit(2, "", "usermod: sai cú pháp\n")
		}
		date, name := c.Args[1], c.Args[2]
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, found := m.users[name]; !found {
			return exit(6, "", "usermod: user '"+name+"' does not exist\n")
		}
		switch date {
		case "":
			delete(m.expired, name)
		case "1":
			m.expired[name] = "Jan 02, 1970"
		default:
			return exit(3, "", "usermod: invalid date '"+date+"'\n")
		}
		return okResult
	})
}

// seedTrash tạo site cũ rồi đưa nó vào thùng rác bằng chính core.Sites (job
// thật trong lịch sử), lùi giờ về oldTrashedAgo, rồi đặt TrashedAt và PurgeAt
// theo đúng giờ đó (core ghi theo giờ chạy seed).
func seedTrash(ctx context.Context, s *core.Sites, createdAgo time.Duration) error {
	if err := addOK(ctx, s, siteOld, "Trang cũ", createdAgo); err != nil {
		return err
	}
	l, err := layoutOf(siteOld)
	if err != nil {
		return err
	}
	d := l.Domain
	shifted := *s
	shifted.Journal = pastJournal{Journal: s.Journal, ago: oldTrashedAgo}
	rm, err := shifted.PrepareRemove(ctx, d)
	if err != nil {
		return fmt.Errorf("chuẩn bị đưa %s vào thùng rác: %w", siteOld, err)
	}
	rep, err := rm.Execute(ctx, nil)
	if err != nil {
		return fmt.Errorf("đưa %s vào thùng rác: %w", siteOld, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return fmt.Errorf("đưa %s vào thùng rác: job %s: %s", siteOld, rep.Job.Status, rep.Job.Error)
	}
	st, ok, err := s.Store.Site(ctx, d)
	if err != nil || !ok {
		return fmt.Errorf("đọc lại site %s sau khi vào thùng rác: có=%v, %w", siteOld, ok, err)
	}
	days, err := s.TrashDays(ctx)
	if err != nil {
		return err
	}
	st.TrashedAt = time.Now().UTC().Add(-oldTrashedAgo)
	st.PurgeAt = st.TrashedAt.AddDate(0, 0, days)
	if err := s.Store.SaveSite(ctx, st); err != nil {
		return fmt.Errorf("đặt ngày vào thùng rác của %s: %w", siteOld, err)
	}
	return nil
}
