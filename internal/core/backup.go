package core

// Backup và khôi phục (mục 6.5). Hợp đồng ở types_m9.go. File tạm của đợt 0:
// thợ đợt 2 thay thân hàm.

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/validate"
)

var errM9 = errors.New("chưa viết (M9)")

func (b *Backups) List(context.Context, string) ([]Backup, error)   { return nil, errM9 }
func (b *Backups) Settings(context.Context) (BackupSettings, error) { return BackupSettings{}, errM9 }
func (b *Backups) PrepareCreate(context.Context, validate.Domain, BackupReason) (*Planned, error) {
	return nil, errM9
}
func (b *Backups) CreateAll(context.Context, BackupReason, func(engine.Event)) ([]BackupResult, error) {
	return nil, errM9
}
func (b *Backups) SnapshotTPANDB(context.Context) (string, error) { return "", errM9 }
func (b *Backups) PrepareRestore(context.Context, validate.Domain, string) (*Planned, error) {
	return nil, errM9
}
func (b *Backups) PrepareSettings(context.Context, int, string) (*Planned, error) { return nil, errM9 }
