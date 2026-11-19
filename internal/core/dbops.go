package core

// Thao tác DB của site (mục 6.9): đổi mật khẩu, xuất, nhập. Hợp đồng ở
// types_m9.go. File tạm của đợt 0: thợ đợt 2 thay thân hàm.

import (
	"context"

	"github.com/doxuta/tpan/internal/validate"
)

func (b *Backups) PrepareDBPassword(context.Context, validate.Domain) (*Planned, error) {
	return nil, errM9
}
func (b *Backups) PrepareDBDump(context.Context, validate.Domain) (*Planned, error) {
	return nil, errM9
}
func (b *Backups) PrepareDBImport(context.Context, validate.Domain, string) (*Planned, error) {
	return nil, errM9
}
