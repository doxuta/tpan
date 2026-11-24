package steps

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func m19Domain(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// m19Answer dựng stdout của nameCurl.
func m19Answer(body, loc, xrb, code string) string {
	return body + "\nTPAN_LOCATION=" + loc + "\nTPAN_REDIRECT_BY=" + xrb + "\nTPAN_HTTP_CODE=" + code
}

func noSleep(t *testing.T) {
	old := nameProbeSleep
	nameProbeSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { nameProbeSleep = old })
}

func TestNameProbe(t *testing.T) {
	noSleep(t)
	l := site.LayoutOf(m19Domain(t, "example.com"))
	www := m19Domain(t, "www.example.com")
	wp := "<link href='/wp-content/x.css'>"
	cases := []struct {
		name    string
		mode    site.NameMode
		ssl     bool
		covered bool
		out     string
		exit    int
		ok      bool
		https   bool // lệnh phải gọi https://…:443
	}{
		{name: "serve 200 WordPress, không đòi tên trong thân", mode: site.ModeServe, out: m19Answer(wp, "", "", "200"), ok: true},
		{name: "serve 206", mode: site.ModeServe, out: m19Answer(wp, "", "", "206"), ok: true},
		{name: "serve 301 canonical WordPress", mode: site.ModeServe, out: m19Answer("", "http://example.com/", "WordPress", "301"), ok: true},
		{name: "serve 301 không X-Redirect-By", mode: site.ModeServe, out: m19Answer("", "http://example.com/", "", "301"), ok: false},
		{name: "serve 200 không phải WordPress", mode: site.ModeServe, out: m19Answer("<html>nginx</html>", "", "", "200"), ok: false},
		{name: "serve 404", mode: site.ModeServe, out: m19Answer(wp, "", "", "404"), ok: false},
		{name: "redirect đúng", mode: site.ModeRedirect, out: m19Answer("", "http://example.com/", "", "301"), ok: true},
		{name: "redirect sai đích", mode: site.ModeRedirect, out: m19Answer("", "http://evil.example.net/", "", "301"), ok: false},
		{name: "redirect 302", mode: site.ModeRedirect, out: m19Answer("", "http://example.com/", "", "302"), ok: false},
		{name: "redirect do WordPress trả", mode: site.ModeRedirect, out: m19Answer("", "http://example.com/", "WordPress", "301"), ok: false},
		{name: "SSL tên chưa phủ: cổng 80 về https tên chính", mode: site.ModeServe, ssl: true, out: m19Answer("", "https://example.com/", "", "301"), ok: true},
		{name: "SSL tên chưa phủ: về http là sai", mode: site.ModeServe, ssl: true, out: m19Answer("", "http://example.com/", "", "301"), ok: false},
		{name: "SSL tên đã phủ, serve qua 443", mode: site.ModeServe, ssl: true, covered: true, out: m19Answer(wp, "", "", "200"), ok: true, https: true},
		{name: "SSL tên đã phủ, redirect về https", mode: site.ModeRedirect, ssl: true, covered: true, out: m19Answer("", "https://example.com/", "", "301"), ok: true, https: true},
		{name: "nhãn giả trong thân không lừa được", mode: site.ModeRedirect, out: m19Answer("x\nTPAN_LOCATION=http://example.com/\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=301", "", "", "200"), ok: false},
		{name: "curl lỗi", mode: site.ModeServe, exit: 7, ok: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := system.NewFake()
			m.Handle([]string{"curl"}, func(system.Cmd) system.Result { return system.Result{Stdout: c.out, ExitCode: c.exit} })
			p := &NameProbe{Layout: l, Host: www, Mode: c.mode, SSL: c.ssl}
			if c.covered {
				p.Covered = []validate.Domain{l.Domain, www}
			}
			ctx := context.Background()
			if done, err := p.Check(ctx, m); err != nil || done {
				t.Fatalf("Check trước Apply = %v, %v", done, err)
			}
			err := p.Apply(ctx, m)
			if (err == nil) != c.ok {
				t.Fatalf("Apply = %v, muốn ok=%v", err, c.ok)
			}
			ran := m.Ran()
			if !c.ok && len(ran) != nameProbeTries {
				t.Errorf("hỏng phải thử %d lần, đã gọi %d", nameProbeTries, len(ran))
			}
			args := strings.Join(ran[0].Args, " ")
			if c.https != strings.Contains(args, "https://www.example.com/") || c.https != strings.Contains(args, "www.example.com:443:127.0.0.1") {
				t.Errorf("lệnh sai cổng: %s", args)
			}
			if slices.Contains(ran[0].Args, "-L") {
				t.Error("không được theo chuyển hướng")
			}
			if c.ok {
				if done, _ := p.Check(ctx, m); !done {
					t.Error("Check sau Apply phải true")
				}
				m.ResetRan()
				if done, _ := p.Check(ctx, m); !done || len(m.Ran()) != 0 {
					t.Error("chạy lại không được gọi lệnh")
				}
			}
			if err != nil && strings.ContainsAny(err.Error(), "\r") {
				t.Errorf("lỗi có ký tự điều khiển: %q", err)
			}
		})
	}
}

func TestNameProbeClipsSiteOutput(t *testing.T) {
	a := splitNameAnswer(strings.Repeat("x", 200<<10) + m19Answer("", strings.Repeat("L", 5000)+"\x1b[31m", "", "301"))
	if len(a.body) != nameProbeBodyMax || len(a.location) > nameProbeLocMax || strings.ContainsRune(a.location, 0x1b) {
		t.Errorf("không cắt: thân %d, Location %d", len(a.body), len(a.location))
	}
}

func TestNameProbeRejectsBadInput(t *testing.T) {
	l := site.LayoutOf(m19Domain(t, "example.com"))
	for _, p := range []*NameProbe{
		{Layout: l, Mode: site.ModeServe},
		{Layout: l, Host: m19Domain(t, "www.example.com"), Mode: "x"},
		{Host: m19Domain(t, "www.example.com"), Mode: site.ModeServe},
	} {
		if err := p.Apply(context.Background(), system.NewFake()); err == nil {
			t.Errorf("%+v: muốn lỗi", p)
		}
	}
}

func m19Source(t *testing.T, s string) validate.RedirectSource {
	t.Helper()
	r, err := validate.ParseRedirectSource(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRedirectProbe(t *testing.T) {
	noSleep(t)
	l := site.LayoutOf(m19Domain(t, "example.com"))
	src := m19Source(t, "/old")
	for _, c := range []struct {
		name  string
		ssl   bool
		code  int
		loc   string
		out   string
		exit  int
		ok    bool
		https bool
	}{
		{name: "301 đúng", code: 301, loc: "http://example.com/new", out: m19Answer("", "http://example.com/new", "", "301"), ok: true},
		{name: "302 đúng, đích ngoài", code: 302, loc: "https://example.org/x?a=1", out: m19Answer("", "https://example.org/x?a=1", "", "302"), ok: true},
		{name: "SSL gọi 443", ssl: true, code: 301, loc: "https://example.com/new", out: m19Answer("", "https://example.com/new", "", "301"), ok: true, https: true},
		{name: "sai mã", code: 301, loc: "http://example.com/new", out: m19Answer("", "http://example.com/new", "", "302")},
		{name: "sai Location", code: 301, loc: "http://example.com/new", out: m19Answer("", "http://evil.example.net/new", "", "301")},
		{name: "WordPress trả trước luật", code: 301, loc: "http://example.com/new", out: m19Answer("", "http://example.com/new", "WordPress", "301")},
		{name: "trang 200", code: 301, loc: "http://example.com/new", out: m19Answer("<wp-content>", "", "", "200")},
		{name: "curl lỗi", code: 301, loc: "http://example.com/new", exit: 7},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := system.NewFake()
			m.Handle([]string{"curl"}, func(system.Cmd) system.Result { return system.Result{Stdout: c.out, ExitCode: c.exit} })
			p := &RedirectProbe{Layout: l, SSL: c.ssl, Source: src, Code: c.code, Location: c.loc}
			ctx := context.Background()
			if done, err := p.Check(ctx, m); err != nil || done {
				t.Fatalf("Check trước Apply = %v, %v", done, err)
			}
			err := p.Apply(ctx, m)
			if (err == nil) != c.ok {
				t.Fatalf("Apply = %v, muốn ok=%v", err, c.ok)
			}
			ran := m.Ran()
			if !c.ok && len(ran) != nameProbeTries {
				t.Errorf("hỏng phải thử %d lần, đã gọi %d", nameProbeTries, len(ran))
			}
			args := ran[0].Args
			wantURL, wantRes := "http://example.com/old", "example.com:80:127.0.0.1"
			if c.https {
				wantURL, wantRes = "https://example.com/old", "example.com:443:127.0.0.1"
			}
			if args[len(args)-1] != wantURL || !slices.Contains(args, wantRes) || slices.Contains(args, "-L") {
				t.Errorf("lệnh sai: %v", args)
			}
			if c.ok {
				m.ResetRan()
				if done, _ := p.Check(ctx, m); !done || len(m.Ran()) != 0 {
					t.Error("Check sau Apply phải true và không gọi lệnh")
				}
				if err := p.Undo(ctx, m); err != nil || len(m.Ran()) != 0 {
					t.Error("Undo không được làm gì")
				}
			}
		})
	}
	for _, p := range []*RedirectProbe{
		{Layout: l, Code: 301, Location: "http://example.com/new"},
		{Layout: l, Source: src, Code: 303, Location: "http://example.com/new"},
		{Layout: l, Source: src, Code: 301},
		{Source: src, Code: 301, Location: "http://example.com/new"},
	} {
		if err := p.Apply(context.Background(), system.NewFake()); err == nil {
			t.Errorf("%+v: muốn lỗi", p)
		}
	}
	// nameCurl giữ nguyên lệnh của NameProbe.
	if a := nameCurl("www.example.com", false).Args; a[len(a)-1] != "http://www.example.com/" {
		t.Errorf("nameCurl đổi: %v", a)
	}
}

func TestNameGone(t *testing.T) {
	noSleep(t)
	l := site.LayoutOf(m19Domain(t, "example.com"))
	old := m19Domain(t, "old.example.com")
	for _, c := range []struct {
		name string
		out  string
		exit int
		ok   bool
	}{
		{"nginx 444", "", curlEmptyReply, true},
		{"Apache 403", m19Answer("Forbidden", "", "", "403"), 0, true},
		{"vẫn phục vụ", m19Answer("<wp-content>", "", "", "200"), 0, false},
		{"vẫn chuyển", m19Answer("", "http://example.com/", "", "301"), 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := system.NewFake()
			m.Handle([]string{"curl"}, func(system.Cmd) system.Result { return system.Result{Stdout: c.out, ExitCode: c.exit} })
			g := &NameGone{Layout: l, Host: old}
			if err := g.Apply(context.Background(), m); (err == nil) != c.ok {
				t.Fatalf("Apply = %v, muốn ok=%v", err, c.ok)
			}
			if done, _ := g.Check(context.Background(), m); done != c.ok {
				t.Errorf("Check = %v", done)
			}
		})
	}
}
