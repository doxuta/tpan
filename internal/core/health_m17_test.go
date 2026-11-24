package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// Gộp M17 với M28 đợt 2: php.pool (file pool chung của site) không áp cho site
// trong lồng, vì site đó không có file pool chung; trước khi sửa, mọi site
// trong lồng có một dòng "chưa kiểm được" trong health.
func TestSiteHealthChecksPHPPoolChiChoSiteNgoaiLong(t *testing.T) {
	cfg := StackConfig{Web: Nginx, PHPVersion: "8.3"}
	for _, caged := range []bool{false, true} {
		st := site.Site{Domain: mustDomain(t, "a.example.com"), Status: site.Active, PHPVersion: "8.3", Caged: caged}
		var lim *account.Limits
		if caged {
			lim = &account.Limits{RAMMB: 200}
		}
		ids := checkIDs(t, system.NewFake(), siteHealthChecks(st, cfg, nil, lim, []string{"8.3"}))
		has := slices.ContainsFunc(ids, func(id string) bool { return strings.HasPrefix(id, "php.pool:") })
		if has == caged {
			t.Errorf("caged=%v: có php.pool = %v; muốn %v (các phép: %v)", caged, has, !caged, ids)
		}
	}
}
