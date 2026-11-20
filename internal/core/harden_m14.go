package core

// M14 (mục 6.13 của docs/KE-HOACH.md): Plan "Áp bản vá cô lập" và Plan đổi
// chế độ .htaccess của một site. Hợp đồng (điều kiện đầu, các bước, kiểm
// chứng) ghi ở types_m14.go. Thân hàm do thợ đợt 2 viết.

import (
	"context"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// PrepareHtaccess: xem types_m14.go.
func (s *Sites) PrepareHtaccess(ctx context.Context, d validate.Domain, mode site.HtaccessMode) (*Planned, error) {
	_, _, _ = ctx, d, mode
	return nil, notImplementedM14("site.htaccess")
}

// PrepareHarden: xem types_m14.go.
func (s *Security) PrepareHarden(ctx context.Context) (*Harden, error) {
	_ = ctx
	return nil, notImplementedM14("security.harden")
}
