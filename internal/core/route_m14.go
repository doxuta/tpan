package core

// M14 (mục 6.13 của docs/KE-HOACH.md): "request tới tên miền này đi đường
// nào". Hợp đồng ghi ở types_m14.go. Thân hàm do thợ đợt 2 viết.

import (
	"context"

	"github.com/doxuta/tpan/internal/validate"
)

// RouteExplain: xem types_m14.go.
func (s *Sites) RouteExplain(ctx context.Context, d validate.Domain) (RouteTrace, error) {
	_, _ = ctx, d
	return RouteTrace{}, notImplementedM14("route.explain")
}
