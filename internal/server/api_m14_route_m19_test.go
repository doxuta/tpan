package server

// M19: GET /api/v1/sites/{domain}/route?host=<tên>. Tên phải thuộc {domain};
// tên của site khác và tên không có đều là 404 name_unknown, cùng một câu.

import (
	"context"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

func TestM19RouteHost(t *testing.T) {
	e := newM14Env(t)
	ctx := context.Background()
	alias := apiDomain(t, "go.example.net")
	owner := apiDomain(t, m14Shop)
	claim := site.Claim{Name: alias, Site: owner, Owner: m14UserA, Kind: site.KindAlias, Mode: site.ModeServe, ByAdmin: true}
	if err := e.st.ClaimNames(ctx, []site.Claim{claim}); err != nil {
		t.Fatal(err)
	}
	pending := apiDomain(t, "soon.example.net")
	if err := e.st.ClaimNames(ctx, []site.Claim{{Name: pending, Site: owner, Owner: m14UserA, Kind: site.KindAlias, Mode: site.ModeServe, ByAdmin: true}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ActivateNames(ctx, owner, []validate.Domain{alias}); err != nil {
		t.Fatal(err)
	}
	route := func(domain, host string) string {
		q := ""
		if host != "" {
			q = "?host=" + host
		}
		return "/api/v1/sites/" + domain + "/route" + q
	}

	// Chủ site hỏi tên phụ của mình: 200, trace ghi tên đã hỏi.
	tr := apiDecode[core.RouteTrace](t, e.call("GET", route(m14Shop, alias.String()), "", m14UserA), 200)
	if tr.Domain != m14Shop || tr.Host != alias.String() || tr.Kind != "alias" || tr.Mode != "serve" {
		t.Errorf("trace = %s %s %s %s", tr.Domain, tr.Host, tr.Kind, tr.Mode)
	}
	// Không có host, hoặc host là tên chính: như cũ.
	for _, h := range []string{"", m14Shop} {
		tr := apiDecode[core.RouteTrace](t, e.call("GET", route(m14Shop, h), "", m14UserA), 200)
		if tr.Host != m14Shop || tr.Kind != "primary" {
			t.Errorf("host=%q: %s %s", h, tr.Host, tr.Kind)
		}
	}
	// Tên không có, tên đang pending, tên của site khác: cùng một câu 404.
	for _, h := range []string{"evil.example.org", pending.String(), m14Wiki} {
		if code := apiErrCode(t, e.call("GET", route(m14Shop, h), "", account.AdminName), 404); code != core.CodeNameUnknown {
			t.Errorf("host=%s: %s", h, code)
		}
	}
	// Tên của shop hỏi qua site khác (khach_b có wiki): name_unknown, không lộ gì.
	if code := apiErrCode(t, e.call("GET", route(m14Wiki, alias.String()), "", m14UserB), 404); code != core.CodeNameUnknown {
		t.Errorf("tên của site khác: %s", code)
	}
	// host không phải tên miền: 400.
	if code := apiErrCode(t, e.call("GET", route(m14Shop, "a..b"), "", m14UserA), 400); code != "domain_invalid" {
		t.Errorf("host sai: %s", code)
	}
}
