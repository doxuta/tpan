package main

// Phần chung của các lệnh M9 (backup, db, cron): dựng core.Backups và
// core.Cron từ store đang mở. Tách file để hai thợ viết backup.go và
// db.go, cron.go cùng dùng mà không sửa chung file.

import (
	"context"

	"github.com/doxuta/tpan/internal/core"
)

// openBackups dựng Backups (và Sites bên trong) như openSites. Máy chưa có
// tpan.db mà chỉ xem trước (--dry-run): Store nil, core coi như bảng backups
// rỗng và không ghi gì.
func openBackups(ctx context.Context, e *env, dryRun bool) (*core.Backups, func(), error) {
	s, done, err := openSites(ctx, e, dryRun)
	if err != nil {
		return nil, nil, err
	}
	b := &core.Backups{Sites: s}
	if st, ok := s.Store.(core.BackupStore); ok {
		b.Store = st
	}
	return b, done, nil
}

// openCron dựng Cron (kèm Backups để đọc lịch backup).
func openCron(ctx context.Context, e *env, dryRun bool) (*core.Cron, func(), error) {
	b, done, err := openBackups(ctx, e, dryRun)
	if err != nil {
		return nil, nil, err
	}
	c := &core.Cron{Sites: b.Sites, Backups: b}
	if st, ok := b.Sites.Store.(core.CronStore); ok {
		c.Runs = st
	}
	return c, done, nil
}
