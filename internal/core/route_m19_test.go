package core

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

const (
	routeAliasPrimary = "example.com"
	routeAliasWWW     = "www.example.com" // redirect (mặc định của site add)
	routeAliasServe   = "shop.example.net"
)

// newRouteAliasEnv: một site example.com có www redirect (mặc định) và một
// tên phụ serve; site khác other.example.org để thử "tên không thuộc site này".
func newRouteAliasEnv(t *testing.T) *siteEnv {
	t.Helper()
	se := newNamesEnv(t)
	se.addSite(t, routeAliasPrimary, "", "")
	se.addSite(t, "other.example.org", "", WWWNone)
	op, err := se.s.PrepareAliasAdd(context.Background(), mustDomain(t, routeAliasPrimary), mustDomain(t, routeAliasServe), site.ModeServe)
	execNames(t, op, err)
	return se
}

func routeHostTrace(t *testing.T, se *siteEnv, host string) RouteTrace {
	t.Helper()
	tr, err := se.s.routeExplainHost(context.Background(), mustDomain(t, host))
	if err != nil {
		t.Fatalf("routeExplainHost %s: %v", host, err)
	}
	return tr
}

func TestRouteExplainAlias(t *testing.T) {
	t.Run("serve: đủ chuỗi như tên chính, http gọi bằng host", func(t *testing.T) {
		se := newRouteAliasEnv(t)
		tr := routeHostTrace(t, se, routeAliasServe)
		if tr.Domain != routeAliasPrimary || tr.Host != routeAliasServe || tr.Kind != "alias" || tr.Mode != "serve" {
			t.Errorf("tiêu đề = %s %s %s %s", tr.Domain, tr.Host, tr.Kind, tr.Mode)
		}
		if got := routeKeysOf(tr); strings.Join(got, ",") != strings.Join(routeOrder, ",") {
			t.Fatalf("thứ tự chặng = %v", got)
		}
		h := routeHops(t, tr)
		l := site.LayoutOf(mustDomain(t, routeAliasPrimary))
		vh := h[HopVhost]
		if vh.Status != checks.Pass || !strings.HasPrefix(vh.Value, l.NginxLink+":") {
			t.Errorf("vhost = %s %q (%s)", vh.Status, vh.Value, vh.Summary.Vi)
		}
		if !evidenceHas(vh, "nginx -T", routeAliasServe, "port 80") {
			t.Errorf("bằng chứng vhost không có tên phụ: %+v", vh.Evidence)
		}
		wantHop(t, h[HopRoot], checks.Pass, l.Public)
		wantHop(t, h[HopSocket], checks.Pass, l.SocketFor("8.3"))
		wantHop(t, h[HopUser], checks.Pass, l.User)
		wantHop(t, h[HopHTTP], checks.Pass, "http://"+routeAliasServe+"/")
		cmd := h[HopHTTP].Evidence[0].Command
		if !strings.Contains(cmd, "--resolve "+routeAliasServe+":80:127.0.0.1") || !strings.Contains(cmd, "http://"+routeAliasServe+"/") {
			t.Errorf("http không gọi bằng host: %s", cmd)
		}
		// DNS tra đúng tên phụ chứ không phải tên chính.
		if !strings.Contains(h[HopDNS].Evidence[0].Command, routeAliasServe) {
			t.Errorf("dns không hỏi %s: %+v", routeAliasServe, h[HopDNS].Evidence)
		}
	})

	t.Run("redirect: dừng ở chặng redirect, kèm câu đi tiếp", func(t *testing.T) {
		se := newRouteAliasEnv(t)
		tr := routeHostTrace(t, se, routeAliasWWW)
		if tr.Host != routeAliasWWW || tr.Kind != "alias" || tr.Mode != "redirect" {
			t.Errorf("tiêu đề = %s %s %s", tr.Host, tr.Kind, tr.Mode)
		}
		if got := routeKeysOf(tr); strings.Join(got, ",") != HopDNS+","+HopVhost+","+HopRedirect {
			t.Fatalf("chặng = %v", got)
		}
		h := routeHops(t, tr)
		wantHop(t, h[HopRedirect], checks.Pass, "301 http://"+routeAliasPrimary+"/")
		if want := "tpan route explain " + routeAliasPrimary; !strings.Contains(h[HopRedirect].Summary.Vi, want) || !strings.Contains(h[HopRedirect].Summary.En, want) {
			t.Errorf("thiếu câu đi tiếp: %+v", h[HopRedirect].Summary)
		}
		if !evidenceHas(h[HopRedirect], "curl", "HTTP 301", "Location: http://"+routeAliasPrimary+"/") {
			t.Errorf("bằng chứng redirect: %+v", h[HopRedirect].Evidence)
		}
	})

	t.Run("redirect sai đích là Fail", func(t *testing.T) {
		se := newRouteAliasEnv(t)
		m := se.m
		m.Handle([]string{"curl"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "\nTPAN_LOCATION=http://evil.example.org/\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=301"}
		})
		h := routeHops(t, routeHostTrace(t, se, routeAliasWWW))
		if h[HopRedirect].Status != checks.Fail {
			t.Errorf("redirect = %s (%s)", h[HopRedirect].Status, h[HopRedirect].Summary.Vi)
		}
	})

	t.Run("không có tên: name_unknown", func(t *testing.T) {
		se := newRouteAliasEnv(t)
		_, err := se.s.routeExplainHost(context.Background(), mustDomain(t, "evil.example.org"))
		if nameCode(err) != CodeNameUnknown {
			t.Fatalf("lỗi = %v", err)
		}
	})

	t.Run("tên chính: như RouteExplain, thêm host và kind", func(t *testing.T) {
		se := newRouteAliasEnv(t)
		tr := routeHostTrace(t, se, routeAliasPrimary)
		old, err := se.s.RouteExplain(context.Background(), mustDomain(t, routeAliasPrimary))
		if err != nil {
			t.Fatal(err)
		}
		if tr.Host != routeAliasPrimary || tr.Kind != "primary" || tr.Mode != "" {
			t.Errorf("tiêu đề = %s %s %q", tr.Host, tr.Kind, tr.Mode)
		}
		if strings.Join(routeKeysOf(tr), ",") != strings.Join(routeKeysOf(old), ",") {
			t.Errorf("chặng khác nhau: %v và %v", routeKeysOf(tr), routeKeysOf(old))
		}
	})

	t.Run("RouteExplainName: tên của site khác là name_unknown", func(t *testing.T) {
		se := newRouteAliasEnv(t)
		ctx := context.Background()
		other := mustDomain(t, "other.example.org")
		if _, err := se.s.RouteExplainName(ctx, other, mustDomain(t, routeAliasServe)); nameCode(err) != CodeNameUnknown {
			t.Errorf("tên của site khác: %v", err)
		}
		tr, err := se.s.RouteExplainName(ctx, mustDomain(t, routeAliasPrimary), mustDomain(t, routeAliasServe))
		if err != nil || tr.Host != routeAliasServe {
			t.Errorf("tên của chính site: %v %+v", err, tr.Host)
		}
	})

	t.Run("tên pending chưa phục vụ: name_unknown", func(t *testing.T) {
		se := newRouteAliasEnv(t)
		ctx := context.Background()
		claim := site.Claim{Name: mustDomain(t, "pending.example.net"), Site: mustDomain(t, routeAliasPrimary),
			Kind: site.KindAlias, Mode: site.ModeServe, ByAdmin: true}
		if err := se.st.ClaimNames(ctx, []site.Claim{claim}); err != nil {
			t.Fatal(err)
		}
		if _, err := se.s.routeExplainHost(ctx, claim.Name); nameCode(err) != CodeNameUnknown {
			t.Errorf("tên pending: %v", err)
		}
	})
}

// Chặng http của tên phụ serve theo từng kiểu câu trả lời của curl.
func TestRouteExplainAliasCurlProblems(t *testing.T) {
	cases := []struct {
		name   string
		reply  system.Result
		status checks.Status
	}{
		{"nginx 444", system.Result{ExitCode: 52, Stderr: "curl: (52) Empty reply from server"}, checks.Fail},
		{"không kết nối", system.Result{ExitCode: 7, Stderr: "curl: (7) Failed to connect"}, checks.Fail},
		{"trang không phải WordPress", system.Result{Stdout: "<html>hello</html>\nTPAN_LOCATION=\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=200"}, checks.Fail},
		{"WordPress chuyển canonical", system.Result{Stdout: "\nTPAN_LOCATION=http://example.com/\nTPAN_REDIRECT_BY=WordPress\nTPAN_HTTP_CODE=301"}, checks.Pass},
		{"301 không phải WordPress", system.Result{Stdout: "\nTPAN_LOCATION=http://example.com/\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=301"}, checks.Fail},
		{"500", system.Result{Stdout: "oops\nTPAN_LOCATION=\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=500"}, checks.Fail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			se := newRouteAliasEnv(t)
			se.m.Handle([]string{"curl"}, func(system.Cmd) system.Result { return c.reply })
			h := routeHops(t, routeHostTrace(t, se, routeAliasServe))
			if got := h[HopHTTP]; got.Status != c.status {
				t.Errorf("http = %s (%s)", got.Status, got.Summary.Vi)
			}
		})
	}
}

// Site có SSL: tên chưa có trong chứng chỉ gọi cổng 80 và mong 301 về https;
// tên đã có trong chứng chỉ gọi cổng 443 (-k).
func TestRouteRedirectHopSSL(t *testing.T) {
	primary, host := mustDomain(t, "example.com"), mustDomain(t, "www.example.com")
	reply := func(loc string) *system.FakeSystem {
		f := system.NewFake()
		f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "\nTPAN_LOCATION=" + loc + "\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=301"}
		})
		return f
	}
	for _, c := range []struct {
		name         string
		siteSSL, tls bool
		loc, port    string
	}{
		{"không SSL", false, false, "http://example.com/", ":80:"},
		{"SSL, tên chưa trong chứng chỉ", true, false, "https://example.com/", ":80:"},
		{"SSL, tên trong chứng chỉ", true, true, "https://example.com/", ":443:"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := reply(c.loc)
			hop := routeRedirectHop(context.Background(), f, primary, host, c.siteSSL, c.tls)
			if hop.Status != checks.Pass {
				t.Fatalf("hop = %s (%s)", hop.Status, hop.Summary.Vi)
			}
			if cmd := hop.Evidence[0].Command; !strings.Contains(cmd, c.port) || strings.Contains(cmd, "-k") != c.tls {
				t.Errorf("lệnh = %s", cmd)
			}
		})
	}
}
