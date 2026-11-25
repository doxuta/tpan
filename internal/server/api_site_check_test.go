package server

// GET /api/v1/site-check: kiểm ngay ô domain và email của màn Tạo site.

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
)

func TestSiteCheck(t *testing.T) {
	e := newM14Env(t)
	ctx := context.Background()
	alias := apiDomain(t, "go.example.net")
	owner := apiDomain(t, m14Shop)
	if err := e.st.ClaimNames(ctx, []site.Claim{{Name: alias, Site: owner, Owner: m14UserA, Kind: site.KindAlias, Mode: site.ModeServe, ByAdmin: true}}); err != nil {
		t.Fatal(err)
	}
	check := func(domain, email string) SiteCheck {
		t.Helper()
		q := url.Values{}
		if domain != "" {
			q.Set("domain", domain)
		}
		if email != "" {
			q.Set("email", email)
		}
		return apiDecode[SiteCheck](t, e.call("GET", "/api/v1/site-check?"+q.Encode(), "", m14UserA), 200)
	}

	for _, tc := range []struct{ domain, code string }{
		{"moi.example.info", ""},
		{"MOI2.Example.INFO", ""},
		{m14Shop, "site_exists"},
		{alias.String(), "name_taken"},
		{"khong hop le", "domain_invalid"},
		{"-x.example.net", "domain_invalid"},
	} {
		got := check(tc.domain, "").Domain
		if got == nil {
			t.Fatalf("%s: không có kết quả domain", tc.domain)
		}
		if tc.code == "" && (!got.OK || got.Value == "") {
			t.Errorf("%s phải hợp lệ và còn trống: %+v", tc.domain, got)
		}
		if tc.code != "" && (got.OK || got.Code != tc.code || got.Message == nil || got.Message.Vi == "") {
			t.Errorf("%s: muốn %s, được %+v", tc.domain, tc.code, got)
		}
	}
	if got := check("MOI2.Example.INFO", "").Domain; got.Value != "moi2.example.info" {
		t.Errorf("domain phải được chuẩn hoá: %q", got.Value)
	}

	if got := check("", "admin@example.com"); got.Email == nil || !got.Email.OK || got.Domain != nil {
		t.Errorf("email hợp lệ: %+v", got)
	}
	for _, bad := range []string{"admin", "admin@", "a b@example.com", "@example.com"} {
		if got := check("", bad).Email; got == nil || got.OK || got.Code != "email_invalid" {
			t.Errorf("email %q phải bị từ chối: %+v", bad, got)
		}
	}
}

// Ô kiểm lúc gõ không được lộ site, tên phụ hay tên miền gốc của người khác:
// cùng một lời chung domain_unavailable, không có tên site nào trong lời.
func TestSiteCheckKhongLoCuaNguoiKhac(t *testing.T) {
	e := newM14Env(t)
	ctx := context.Background()
	alias := apiDomain(t, "go.example.net")
	owner := apiDomain(t, m14Shop)
	if err := e.st.ClaimNames(ctx, []site.Claim{{Name: alias, Site: owner, Owner: m14UserA, Kind: site.KindAlias, Mode: site.ModeServe, ByAdmin: true}}); err != nil {
		t.Fatal(err)
	}
	check := func(user, domain string) *FieldCheck {
		t.Helper()
		q := url.Values{"domain": {domain}}
		return apiDecode[SiteCheck](t, e.call("GET", "/api/v1/site-check?"+q.Encode(), "", user), 200).Domain
	}
	// khach_b dò đồ của khach_a: site, tên phụ, tên con cùng tên miền gốc.
	for _, d := range []string{m14Shop, alias.String(), "moi.example.net"} {
		got := check(m14UserB, d)
		if got == nil || got.OK || got.Code != "domain_unavailable" {
			t.Fatalf("khach_b kiểm %s: muốn domain_unavailable, được %+v", d, got)
		}
		if strings.Contains(got.Message.Vi, "example") || strings.Contains(got.Message.En, "example") {
			t.Errorf("lời cho khach_b không được nhắc tên miền nào: %+v", got.Message)
		}
	}
	// Chủ thật và admin vẫn thấy lời cụ thể.
	if got := check(m14UserB, "moi.example.info"); !got.OK {
		t.Errorf("tên miền gốc chưa ai dùng thì khach_b phải tạo được: %+v", got)
	}
	if got := check(m14UserA, m14Shop); got.Code != "site_exists" {
		t.Errorf("khach_a kiểm site của mình: muốn site_exists, được %+v", got)
	}
	if got := check(account.AdminName, m14Shop); got.Code != "site_exists" {
		t.Errorf("admin kiểm: muốn site_exists, được %+v", got)
	}
}
