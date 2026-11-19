package core

import "github.com/doxuta/tpan/internal/store"

var (
	_ BackupStore = (*store.Store)(nil)
	_ CronStore   = (*store.Store)(nil)
)
