package core

// Test của phần hợp đồng M17 đã có thân ở đợt 0 (mục 3.6 của
// docs/thay-doi/M17-thiet-ke.md): những hàm nhỏ mà mọi đợt sau dựa vào.

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
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

// Site trong lồng: đúng hai bước của lồng, không có pool chung (một nơi phục
// vụ PHP duy nhất cho mỗi site).
func TestSitePHPStepsTrongLong(t *testing.T) {
	st := m17Site(t, "a.example.com")
	st.Caged = true
	lim := account.Limits{RAMMB: 256}
	ss, err := sitePHPSteps(st, StackConfig{Web: Nginx, PHPVersion: "8.3"}, lim)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 {
		t.Fatalf("site trong lồng phải có đúng hai bước, có %d", len(ss))
	}
	f, ok := ss[0].(*steps.CageFiles)
	if !ok || f.Limits != lim || f.PHP != "8.3" || f.Remove {
		t.Fatalf("bước đầu phải là CageFiles ghi với trần của gói, có %T %+v", ss[0], ss[0])
	}
	if s, ok := ss[1].(*steps.CageSocket); !ok || s.Disable {
		t.Fatalf("bước hai phải là CageSocket bật, có %T %+v", ss[1], ss[1])
	}
}

// cage.enable: thứ tự CageFiles, CageSocket, gỡ pool chung, CageReap, rồi vhost
// trỏ socket của lồng (mục 3.6).
func TestCageEnableStepsThuTu(t *testing.T) {
	st := m17Site(t, "a.example.com")
	ss, err := cageEnableSteps(st, StackConfig{Web: Nginx, PHPVersion: "8.3"}, account.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) < 6 {
		t.Fatalf("thiếu bước: %d", len(ss))
	}
	if _, ok := ss[0].(*steps.CageFiles); !ok {
		t.Fatalf("bước 1 phải là CageFiles, có %T", ss[0])
	}
	if _, ok := ss[1].(*steps.CageSocket); !ok {
		t.Fatalf("bước 2 phải là CageSocket, có %T", ss[1])
	}
	if a, ok := ss[2].(*steps.Absent); !ok || a.Path != st.Layout().PoolConf("8.3") {
		t.Fatalf("bước 3 phải gỡ pool chung, có %T %+v", ss[2], ss[2])
	}
	if _, ok := ss[3].(*steps.CageReap); !ok {
		t.Fatalf("bước 4 phải là CageReap, có %T", ss[3])
	}
	want, err := siteVhost(func() site.Site { c := st; c.Caged = true; return c }(), StackConfig{Web: Nginx, PHPVersion: "8.3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 4+len(want) {
		t.Fatalf("sau CageReap phải là đúng các bước vhost của lồng (%d), có %d bước", len(want), len(ss)-4)
	}
}

// Bất biến 8: Isolated(from, to) chạy lệnh dưới user của "from" ở ngoài lồng,
// nên không bao giờ sinh khi "from" ở trong lồng (t14 P4); chiều ngược lại (từ
// site ngoài lồng sang site trong lồng) giữ nguyên.
func TestIsolatedKhongSinhTuSiteTrongLong(t *testing.T) {
	a := m17Site(t, "a.example.com")
	a.Caged = true
	b := m17Site(t, "b.example.com")
	lim := account.Limits{}
	ids := checkIDs(t, system.NewFake(), siteChecks(Nginx, "8.3", a, []site.Site{b}, &lim))
	if slices.Contains(ids, "isolation:a.example.com:b.example.com") {
		t.Errorf("site trong lồng a không được là phía chạy lệnh của Isolated: %v", ids)
	}
	if !slices.Contains(ids, "isolation:b.example.com:a.example.com") {
		t.Errorf("thiếu Isolated từ site ngoài lồng b sang a: %v", ids)
	}
	// b ở ngoài lồng, a (trong lồng) là "others": chỉ chiều b → a.
	ids = checkIDs(t, system.NewFake(), siteChecks(Nginx, "8.3", b, []site.Site{a}, nil))
	if slices.Contains(ids, "isolation:a.example.com:b.example.com") || !slices.Contains(ids, "isolation:b.example.com:a.example.com") {
		t.Errorf("siteChecks của b: muốn đúng chiều b → a, có %v", ids)
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

// siteCageLimits: ngoài lồng là nil (siteChecks chọn bộ thường); trong lồng mà
// không có bảng gói thì lỗi: trần 0 sẽ xoá trần của site ở lần ghi file kế tiếp.
func TestSiteCageLimits(t *testing.T) {
	st := m17Site(t, "a.example.com")
	if lim, err := siteCageLimits(context.Background(), nil, st); err != nil || lim != nil {
		t.Fatalf("site ngoài lồng: muốn nil, có %v, %v", lim, err)
	}
	st.Caged = true
	if lim, err := siteCageLimits(context.Background(), nil, st); err == nil {
		t.Fatalf("site trong lồng không có bảng gói: muốn lỗi, có %v", lim)
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
