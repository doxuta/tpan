package server

// GET /api/v1/site-check: kiểm ngay ô domain và email của màn Tạo site.

import (
	"context"
	"net/url"
	"testing"

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
		{"moi.example.net", ""},
		{"MOI2.Example.NET", ""},
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
	if got := check("MOI2.Example.NET", "").Domain; got.Value != "moi2.example.net" {
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
