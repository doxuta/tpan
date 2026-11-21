package core

// Test của phần hợp đồng M17 đã có thân ở đợt 0 (mục 3.6 của
// docs/thay-doi/M17-thiet-ke.md): những hàm nhỏ mà mọi đợt sau dựa vào.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/validate"
)

func m17Site(t *testing.T, d string) site.Site {
	t.Helper()
	dom, err := validate.ParseDomain(d)
	if err != nil {
		t.Fatal(err)
	}
	return site.Site{Domain: dom, Slug: validate.SlugOf(dom), Status: site.Active, PHPVersion: "8.3", Owner: account.AdminName}
}

// Site ngoài lồng: sitePHPSteps ra đúng một bước pool chung như trước M17.
func TestSitePHPStepsNgoaiLong(t *testing.T) {
	st := m17Site(t, "a.example.com")
	ss, err := sitePHPSteps(st, StackConfig{Web: Nginx, PHPVersion: "8.3"}, account.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 {
		t.Fatalf("site ngoài lồng phải có đúng một bước pool, có %d", len(ss))
	}
	f, ok := ss[0].(*steps.File)
	if !ok || f.Path != st.Layout().PoolConf("8.3") {
		t.Fatalf("bước phải là file pool %s, có %T %+v", st.Layout().PoolConf("8.3"), ss[0], ss[0])
	}
}

// Không có bảng user (xem trước khi chưa có tpan.db): gói default, không lồng.
func TestSitePackageKhongCoBang(t *testing.T) {
	st := m17Site(t, "a.example.com")
	p, err := sitePackage(context.Background(), nil, st)
	if err != nil || p.Name != account.DefaultPackage || p.Cage {
		t.Fatalf("muốn gói default không lồng, có %+v, %v", p, err)
	}
	lim, want, err := cageLimitsOf(context.Background(), nil, st)
	if err != nil || want || lim.Any() {
		t.Fatalf("muốn không trần, không lồng; có %+v, %v, %v", lim, want, err)
	}
}

// Tên miền của site thử bị từ chối khi tạo site.
func TestTaoSiteTuChoiTenPhepThu(t *testing.T) {
	d, err := validate.ParseDomain("tpan-probe-a-0123456789ab.test")
	if err != nil {
		t.Fatal(err)
	}
	s := &Sites{Settings: EmptySettings{}, Store: EmptySiteStore{}}
	_, err = s.PrepareAdd(context.Background(), SiteInput{Domain: d})
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != CodeDomainReserved {
		t.Fatalf("muốn từ chối %s, có %v", CodeDomainReserved, err)
	}
}

// Hai hàm thuần của số liệu có mặt (thân do đợt 2 viết).
func TestUsageThuanCoMat(t *testing.T) {
	_ = suggest("a.example.com", account.DefaultPackage, nil, account.Limits{})
	_ = rollupHour("a.example.com", time.Time{}, nil, []cage.Sample{})
}
