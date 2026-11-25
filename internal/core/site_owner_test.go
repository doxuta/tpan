package core

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// ownerVia ghi chủ mới qua SiteStore trong bộ nhớ (store thật có test riêng).
type ownerVia struct{ st SiteStore }

func (o ownerVia) SetSiteOwner(ctx context.Context, d validate.Domain, owner string) error {
	s, _, err := o.st.Site(ctx, d)
	if err != nil {
		return err
	}
	s.Owner = owner
	return o.st.SaveSite(ctx, s)
}

func TestDoiChuSite(t *testing.T) {
	e := newCgEnv(t, Nginx, cgDom, cgOther)
	c := e.cages()
	ow := ownerVia{e.sites}
	d := mustDomain(t, cgDom)
	e.acc.pkgs["nho"] = account.Package{Name: "nho", Cage: true, RAMMB: 128, MaxSites: 1}
	e.acc.put(account.User{Name: "khach_a", Role: account.RoleUser, Package: cgPkg})
	e.acc.put(account.User{Name: "khach_nho", Role: account.RoleUser, Package: "nho"})
	e.acc.put(account.User{Name: "khach_khoa", Role: account.RoleUser, Status: account.Suspended})

	_, err := c.PrepareSiteOwner(ctx, ow, d, account.AdminName)
	wantRefused(t, err, CodeOwnerSame)
	_, err = c.PrepareSiteOwner(ctx, ow, d, "khong_co")
	wantRefused(t, err, CodeUserNotFound)
	_, err = c.PrepareSiteOwner(ctx, ow, d, "khach_khoa")
	wantRefused(t, err, CodeUserSuspended)
	_, err = c.PrepareSiteOwner(ctx, ow, mustDomain(t, "khong-co.example.com"), "khach_a")
	wantRefused(t, err, "site_not_found")

	// Site ngoài lồng: không có Plan, chuẩn bị không ghi gì, Apply mới ghi.
	ch, err := c.PrepareSiteOwner(ctx, ow, d, "khach_a")
	if err != nil || ch.Plan != nil || ch.From != account.AdminName || ch.To != "khach_a" {
		t.Fatalf("ngoài lồng: %+v %v", ch, err)
	}
	if got := ownerOf(e.site(t, cgDom)); got != account.AdminName {
		t.Fatalf("chuẩn bị đã đổi chủ thành %s", got)
	}
	if err := ch.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := ownerOf(e.site(t, cgDom)); got != "khach_a" {
		t.Fatalf("chủ sau Apply = %s", got)
	}

	// Site trong lồng, gói chủ mới khác trần: một job ghi lại trần, chủ đổi theo.
	e.enable(t, cgDom)
	ch, err = c.PrepareSiteOwner(ctx, ow, d, "khach_nho")
	if err != nil || ch.Plan == nil {
		t.Fatalf("trong lồng: %+v %v", ch, err)
	}
	cgOK(t, cgRun(t, ch.Plan))
	if got := ownerOf(e.site(t, cgDom)); got != "khach_nho" {
		t.Errorf("chủ sau job = %s", got)
	}
	if b, _ := e.m.ReadFile(e.l.CageSliceFile()); !strings.Contains(string(b), "MemoryMax=128M") {
		t.Errorf("slice chưa mang trần của gói chủ mới:\n%s", b)
	}

	// khach_nho chỉ được 1 site: site thứ hai bị từ chối như lúc tạo mới.
	_, err = c.PrepareSiteOwner(ctx, ow, mustDomain(t, cgOther), "khach_nho")
	wantRefused(t, err, CodeQuotaSites)

	// Site tạm khoá hay ở thùng rác: từ chối.
	e.setSite(t, cgOther, func(s *site.Site) { s.Suspended = account.SuspendSelf })
	_, err = c.PrepareSiteOwner(ctx, ow, mustDomain(t, cgOther), "khach_a")
	wantRefused(t, err, CodeSiteSuspended)
	e.setSite(t, cgOther, func(s *site.Site) { s.Suspended = account.NotSuspended; s.Status = site.Trashed })
	_, err = c.PrepareSiteOwner(ctx, ow, mustDomain(t, cgOther), "khach_a")
	wantRefused(t, err, "site_trashed")
}
