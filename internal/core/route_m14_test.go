package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

const (
	routeDom   = "a.example.com"
	routeOther = "b.example.com"
	routeAcme  = "/var/lib/tpan/acme"
)

var routeOrder = []string{HopDNS, HopVhost, HopRoot, HopSocket, HopPool, HopUser, HopOpenBasedir, HopHTTP}

// newRouteEnv là máy giả có hai site và trả lời `nginx -T`, `apache2ctl -S`
// từ đúng các file đang nằm trong sites-enabled, theo định dạng lệnh thật in.
func newRouteEnv(t *testing.T, web WebServer) *fixEnv {
	t.Helper()
	e := newFixEnv(t, web, routeDom, routeOther)
	m := e.m
	suspendCurl(m.FakeSystem, siteSimCurl(m))
	m.Handle([]string{"nginx", "-T"}, func(system.Cmd) system.Result {
		var b strings.Builder
		b.WriteString("# configuration file /etc/nginx/nginx.conf:\nhttp {\n    include /etc/nginx/sites-enabled/*;\n}\n\n")
		names, _ := m.ReadDir("/etc/nginx/sites-enabled")
		for _, n := range names {
			data, _ := m.ReadFile("/etc/nginx/sites-enabled/" + n)
			b.WriteString("# configuration file /etc/nginx/sites-enabled/" + n + ":\n" + string(data) + "\n")
		}
		return system.Result{Stdout: b.String(), Stderr: "nginx: the configuration file /etc/nginx/nginx.conf syntax is ok\n"}
	})
	open := regexp.MustCompile(`^\s*<VirtualHost\s+\*:([0-9]+)>`)
	name := regexp.MustCompile(`^\s*ServerName\s+(\S+)`)
	m.Handle([]string{"apache2ctl", "-S"}, func(system.Cmd) system.Result {
		byPort := map[string][]string{}
		var ports []string
		names, _ := m.ReadDir("/etc/apache2/sites-enabled")
		for _, n := range names {
			file := "/etc/apache2/sites-enabled/" + n
			data, _ := m.ReadFile(file)
			port, at := "", ""
			for i, ln := range strings.Split(string(data), "\n") {
				if g := open.FindStringSubmatch(ln); g != nil {
					port, at = g[1], file+":"+strconv.Itoa(i+1)
				} else if g := name.FindStringSubmatch(ln); g != nil && port != "" {
					if byPort[port] == nil {
						ports = append(ports, port)
					}
					byPort[port] = append(byPort[port], "         port "+port+" namevhost "+g[1]+" ("+at+")\n")
					port = ""
				}
			}
		}
		out := "VirtualHost configuration:\n"
		for _, p := range ports {
			out += fmt.Sprintf("%-23sis a NameVirtualHost\n", "*:"+p) + strings.Join(byPort[p], "")
		}
		return system.Result{Stdout: out + "ServerRoot: \"/etc/apache2\"\n", Stderr: "AH00558: apache2: Could not reliably determine the server's fully qualified domain name\n"}
	})
	return e
}

func routeHops(t *testing.T, tr RouteTrace) map[string]RouteHop {
	t.Helper()
	out := map[string]RouteHop{}
	for _, h := range tr.Hops {
		if h.Title.Vi == "" || h.Title.En == "" || h.Summary.Vi == "" || h.Summary.En == "" {
			t.Errorf("chặng %s thiếu chữ ở một ngôn ngữ: %+v", h.Key, h)
		}
		if h.Evidence == nil {
			t.Errorf("chặng %s có Evidence nil (JSON ra null)", h.Key)
		}
		out[h.Key] = h
	}
	return out
}

func routeKeysOf(tr RouteTrace) []string {
	var ks []string
	for _, h := range tr.Hops {
		ks = append(ks, h.Key)
	}
	return ks
}

func explain(t *testing.T, e *fixEnv, dom string) RouteTrace {
	t.Helper()
	tr, err := e.s.RouteExplain(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatalf("RouteExplain %s: %v", dom, err)
	}
	return tr
}

func wantHop(t *testing.T, h RouteHop, st checks.Status, value string) {
	t.Helper()
	if h.Status != st || h.Value != value {
		t.Errorf("chặng %s = %s %q (%s); muốn %s %q\nbằng chứng: %+v", h.Key, h.Status, h.Value, h.Summary.Vi, st, value, h.Evidence)
	}
}

// evidenceHas: một dòng bằng chứng của chặng chứa đủ các chuỗi wants.
func evidenceHas(h RouteHop, cmd string, wants ...string) bool {
	for _, ev := range h.Evidence {
		if !strings.Contains(ev.Command, cmd) {
			continue
		}
		ok := true
		for _, w := range wants {
			ok = ok && strings.Contains(ev.Output, w)
		}
		if ok {
			return true
		}
	}
	return false
}

func TestRouteExplainHealthySite(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newRouteEnv(t, web)
			l := site.LayoutOf(mustDomain(t, routeDom))
			tr := explain(t, e, routeDom)
			if tr.Domain != routeDom || tr.Web != web {
				t.Errorf("trace = %s %s", tr.Domain, tr.Web)
			}
			if got := routeKeysOf(tr); strings.Join(got, ",") != strings.Join(routeOrder, ",") {
				t.Fatalf("thứ tự chặng = %v", got)
			}
			h := routeHops(t, tr)

			link, cmd, rootLine := l.NginxLink, "nginx -T", "root "+l.Public+";"
			if web == Apache {
				link, cmd, rootLine = l.ApacheLink, "apache2ctl -S", `DocumentRoot "`+l.Public+`"`
			}
			vh := h[HopVhost]
			if vh.Status != checks.Pass || !strings.HasPrefix(vh.Value, link+":") {
				t.Errorf("vhost = %s %q (%s)", vh.Status, vh.Value, vh.Summary.Vi)
			}
			if !evidenceHas(vh, cmd, link, routeDom, "port 80") {
				t.Errorf("bằng chứng vhost: %+v", vh.Evidence)
			}
			// Bằng chứng không được là cả bản in: chỉ dòng của tên miền này.
			if evidenceHas(vh, cmd, routeOther) {
				t.Errorf("bằng chứng vhost lẫn site khác: %+v", vh.Evidence)
			}
			wantHop(t, h[HopRoot], checks.Pass, l.Public)
			if !evidenceHas(h[HopRoot], "read "+link+" (tpan)", rootLine) {
				t.Errorf("bằng chứng root: %+v", h[HopRoot].Evidence)
			}
			wantHop(t, h[HopSocket], checks.Pass, l.SocketFor("8.3"))
			if !evidenceHas(h[HopSocket], "read "+link+" (tpan)", l.SocketFor("8.3")) {
				t.Errorf("bằng chứng socket: %+v", h[HopSocket].Evidence)
			}
			wantHop(t, h[HopPool], checks.Pass, l.PoolConf("8.3")+" ["+l.Pool+"]")
			if !evidenceHas(h[HopPool], "pool.d", "listen = "+l.SocketFor("8.3"), "["+l.Pool+"]") {
				t.Errorf("bằng chứng pool: %+v", h[HopPool].Evidence)
			}
			wantHop(t, h[HopUser], checks.Pass, l.User)
			wantHop(t, h[HopOpenBasedir], checks.Pass, l.Dir+"/")
			if !evidenceHas(h[HopOpenBasedir], "read "+l.PoolConf("8.3")+" (tpan)", "php_admin_value[open_basedir] = "+l.Dir+"/") {
				t.Errorf("bằng chứng open_basedir: %+v", h[HopOpenBasedir].Evidence)
			}
			wantHop(t, h[HopHTTP], checks.Pass, "http://"+routeDom+"/")
			if len(h[HopHTTP].Evidence) == 0 || !strings.Contains(h[HopHTTP].Evidence[0].Command, "curl") {
				t.Errorf("bằng chứng http: %+v", h[HopHTTP].Evidence)
			}
			// DNS của máy giả không có bản ghi A: cảnh báo, không phải lỗi.
			if h[HopDNS].Status != checks.Warn {
				t.Errorf("dns = %s", h[HopDNS].Status)
			}
		})
	}
}

// RouteExplain chỉ đọc: không ghi file, và lệnh nào chạy cũng là lệnh đọc.
func TestRouteExplainReadOnly(t *testing.T) {
	e := newRouteEnv(t, Nginx)
	before := e.m.Snapshot()
	e.m.ResetRan()
	explain(t, e, routeDom)
	after := e.m.Snapshot()
	if len(before) != len(after) {
		t.Errorf("số mục trên đĩa giả đổi: %d thành %d", len(before), len(after))
	}
	for _, c := range e.m.Ran() {
		switch c.Name {
		case "getent", "ip", "nginx", "curl":
		default:
			t.Errorf("lệnh ngoài danh sách đọc: %s", c.String())
		}
	}
}

func TestRouteExplainDNSValue(t *testing.T) {
	e := newRouteEnv(t, Nginx)
	e.m.Handle([]string{"getent", "ahostsv4"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "192.0.2.10 STREAM " + routeDom + "\n192.0.2.10 DGRAM\n"}
	})
	e.m.Handle([]string{"getent", "ahostsv6"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	e.m.Handle([]string{"ip", "-o", "addr"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "2: eth0    inet 192.0.2.10/24 brd 192.0.2.255 scope global eth0\n"}
	})
	wantHop(t, routeHops(t, explain(t, e, routeDom))[HopDNS], checks.Pass, "192.0.2.10")
}

func TestRouteExplainSSL(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newRouteEnv(t, web)
			st := e.site(t, routeDom)
			l := st.Layout()
			conf, link, mark := l.NginxConf, l.NginxLink, "listen 443 ssl;"
			if web == Apache {
				conf, link, mark = l.ApacheConf, l.ApacheLink, "<VirtualHost *:443>"
			}
			vhost, err := templates.SiteVhost(l, string(web), "8.3", routeAcme, true, st.Htaccess)
			if err != nil {
				t.Fatal(err)
			}
			e.m.SetFile(conf, vhost, 0o644, system.Root)
			st.SSL = true
			e.setSite(t, st)

			h := routeHops(t, explain(t, e, routeDom))
			// Vhost phải là khối cổng 443, không phải khối cổng 80 chỉ chuyển hướng.
			at := 0
			for i, ln := range strings.Split(string(vhost), "\n") {
				if strings.Contains(ln, mark) {
					at = i + 1
				}
			}
			vh := h[HopVhost]
			_, lineStr, _ := strings.Cut(vh.Value, link+":")
			n, _ := strconv.Atoi(lineStr)
			if vh.Status != checks.Pass || at == 0 || n < at {
				t.Errorf("vhost = %s %q; khối 443 mở ở dòng %d", vh.Status, vh.Value, at)
			}
			if !evidenceHas(vh, "", "port 443") || !strings.Contains(vh.Summary.Vi, "443") {
				t.Errorf("vhost không nói cổng 443: %s %+v", vh.Summary.Vi, vh.Evidence)
			}
			wantHop(t, h[HopRoot], checks.Pass, l.Public)
			wantHop(t, h[HopSocket], checks.Pass, l.SocketFor("8.3"))
			wantHop(t, h[HopUser], checks.Pass, l.User)
			if h[HopHTTP].Value != "https://"+routeDom+"/" {
				t.Errorf("http value = %q", h[HopHTTP].Value)
			}
		})
	}
}

func TestRouteExplainSuspended(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newRouteEnv(t, web)
			goneEvery, midCheckWait = 0, 0
			t.Cleanup(func() { goneEvery, midCheckWait = 200_000_000, 1_000_000_000 })
			planned(t)(e.s.PrepareSuspend(ctx, mustDomain(t, routeDom)))

			tr := explain(t, e, routeDom)
			if got := strings.Join(routeKeysOf(tr), ","); got != "dns,vhost,http" {
				t.Fatalf("chặng của site tạm khoá = %s", got)
			}
			h := routeHops(t, tr)
			if h[HopVhost].Status != checks.Pass || !strings.Contains(h[HopVhost].Summary.Vi, "tạm khoá") || !strings.Contains(h[HopVhost].Summary.En, "suspended") {
				t.Errorf("vhost = %s %q", h[HopVhost].Status, h[HopVhost].Summary.Vi)
			}
			if h[HopHTTP].Status != checks.Pass {
				t.Errorf("http của site tạm khoá = %s (%s)", h[HopHTTP].Status, h[HopHTTP].Summary.Vi)
			}
		})
	}
}

// DB của site đang khoá: chặng http là "tạm khoá", không phải lỗi (như health).
func TestRouteExplainDBSuspended(t *testing.T) {
	e := newRouteEnv(t, Nginx)
	st := e.site(t, routeDom)
	st.DBSuspended = account.SuspendSelf
	e.setSite(t, st)
	h := routeHops(t, explain(t, e, routeDom))
	if h[HopHTTP].Status != checks.Unknown || h[HopUser].Status != checks.Pass {
		t.Errorf("http = %s, user = %s", h[HopHTTP].Status, h[HopUser].Status)
	}
}

// Lỗ L8: ai đó thêm tay một vhost trùng tên, đứng trước vhost của site. Chặng
// vhost phải đỏ và nêu đúng file lạ; các chặng sau là của file lạ đó.
func TestRouteExplainForeignVhost(t *testing.T) {
	t.Run("nginx", func(t *testing.T) {
		e := newRouteEnv(t, Nginx)
		l := site.LayoutOf(mustDomain(t, routeDom))
		const foreign = "/etc/nginx/sites-enabled/00-la.conf"
		e.m.SetFile(foreign, []byte("# file thêm tay\nserver {\n    listen 80;\n    server_name "+routeDom+" www.example.net;\n    root /srv/la;\n    location / { return 200 \"la\"; }\n}\n"), 0o644, system.Root)

		h := routeHops(t, explain(t, e, routeDom))
		vh := h[HopVhost]
		wantHop(t, vh, checks.Fail, foreign+":4")
		if !strings.Contains(vh.Summary.Vi, foreign) || !strings.Contains(vh.Summary.Vi, l.NginxLink) {
			t.Errorf("câu của chặng vhost không nêu file lạ và vhost của site: %s", vh.Summary.Vi)
		}
		// Bằng chứng có cả hai chỗ khai, file lạ đứng trước.
		if !evidenceHas(vh, "nginx -T", foreign+":4", l.NginxLink) {
			t.Errorf("bằng chứng vhost: %+v", vh.Evidence)
		}
		wantHop(t, h[HopRoot], checks.Fail, "/srv/la")
		wantHop(t, h[HopSocket], checks.Fail, "")
		for _, k := range []string{HopPool, HopUser, HopOpenBasedir} {
			wantHop(t, h[k], checks.Unknown, "")
		}
		// Site kia không bị ảnh hưởng.
		wantHop(t, routeHops(t, explain(t, e, routeOther))[HopVhost], checks.Pass, site.LayoutOf(mustDomain(t, routeOther)).NginxLink+":8")
	})
	t.Run("apache", func(t *testing.T) {
		e := newRouteEnv(t, Apache)
		const foreign = "/etc/apache2/sites-enabled/00-la.conf"
		e.m.SetFile(foreign, []byte("<VirtualHost *:80>\n    ServerName "+routeDom+"\n    DocumentRoot /srv/la\n</VirtualHost>\n"), 0o644, system.Root)
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopVhost], checks.Fail, foreign+":1")
		wantHop(t, h[HopRoot], checks.Fail, "/srv/la")
		wantHop(t, h[HopSocket], checks.Fail, "")
		wantHop(t, h[HopPool], checks.Unknown, "")
	})
}

// Vhost của site bị gỡ khỏi sites-enabled: không vhost nào khai tên miền.
func TestRouteExplainNoVhost(t *testing.T) {
	e := newRouteEnv(t, Nginx)
	l := site.LayoutOf(mustDomain(t, routeDom))
	if err := e.m.Remove(l.NginxLink); err != nil {
		t.Fatal(err)
	}
	h := routeHops(t, explain(t, e, routeDom))
	wantHop(t, h[HopVhost], checks.Fail, "")
	if !strings.Contains(h[HopVhost].Summary.Vi, "mặc định") {
		t.Errorf("câu = %s", h[HopVhost].Summary.Vi)
	}
	for _, k := range []string{HopRoot, HopSocket, HopPool, HopUser, HopOpenBasedir} {
		wantHop(t, h[k], checks.Unknown, "")
	}
}

func TestRouteExplainWrongRoot(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newRouteEnv(t, web)
			l := site.LayoutOf(mustDomain(t, routeDom))
			other := site.LayoutOf(mustDomain(t, routeOther))
			conf, old, repl := l.NginxConf, "root "+l.Public+";", "root "+other.Public+";"
			if web == Apache {
				conf, old, repl = l.ApacheConf, `DocumentRoot "`+l.Public+`"`, `DocumentRoot "`+other.Public+`"`
			}
			data, _ := e.m.ReadFile(conf)
			if !strings.Contains(string(data), old) {
				t.Fatalf("vhost không có %q", old)
			}
			e.m.SetFile(conf, []byte(strings.Replace(string(data), old, repl, 1)), 0o644, system.Root)

			h := routeHops(t, explain(t, e, routeDom))
			if h[HopVhost].Status != checks.Pass { // vhost vẫn là của site
				t.Errorf("vhost = %s", h[HopVhost].Status)
			}
			wantHop(t, h[HopRoot], checks.Fail, other.Public)
			if !strings.Contains(h[HopRoot].Summary.Vi, l.Public) {
				t.Errorf("câu root không nêu thư mục đúng: %s", h[HopRoot].Summary.Vi)
			}
			wantHop(t, h[HopSocket], checks.Pass, l.SocketFor("8.3"))
		})
	}
}

func TestRouteExplainPoolProblems(t *testing.T) {
	l := site.LayoutOf(mustDomain(t, routeDom))
	other := site.LayoutOf(mustDomain(t, routeOther))
	edit := func(t *testing.T, e *fixEnv, old, repl string) {
		t.Helper()
		data, _ := e.m.ReadFile(l.PoolConf("8.3"))
		if !strings.Contains(string(data), old) {
			t.Fatalf("file pool không có %q:\n%s", old, data)
		}
		e.m.SetFile(l.PoolConf("8.3"), []byte(strings.Replace(string(data), old, repl, 1)), 0o644, system.Root)
	}

	t.Run("pool chạy user khác", func(t *testing.T) {
		e := newRouteEnv(t, Nginx)
		edit(t, e, "user = "+l.User, "user = "+other.User)
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopPool], checks.Pass, l.PoolConf("8.3")+" ["+l.Pool+"]")
		wantHop(t, h[HopUser], checks.Fail, other.User)
		if !strings.Contains(h[HopUser].Summary.Vi, l.User) || !evidenceHas(h[HopUser], "read "+l.PoolConf("8.3"), "user = "+other.User) {
			t.Errorf("user: %s %+v", h[HopUser].Summary.Vi, h[HopUser].Evidence)
		}
		wantHop(t, h[HopOpenBasedir], checks.Pass, l.Dir+"/")
	})
	t.Run("open_basedir mở rộng", func(t *testing.T) {
		e := newRouteEnv(t, Nginx)
		edit(t, e, "php_admin_value[open_basedir] = "+l.Dir+"/", "php_admin_value[open_basedir] = /")
		wantHop(t, routeHops(t, explain(t, e, routeDom))[HopOpenBasedir], checks.Fail, "/")
	})
	t.Run("không có open_basedir", func(t *testing.T) {
		e := newRouteEnv(t, Nginx)
		edit(t, e, "php_admin_value[open_basedir] = ", "; php_admin_value[open_basedir] = ")
		wantHop(t, routeHops(t, explain(t, e, routeDom))[HopOpenBasedir], checks.Fail, "")
	})
	t.Run("socket do pool khác nghe", func(t *testing.T) {
		e := newRouteEnv(t, Nginx)
		// Pool của site nghe chỗ khác; pool của site kia nghe socket của site này.
		edit(t, e, "listen = "+l.SocketFor("8.3"), "listen = /run/php/khac.sock")
		data, _ := e.m.ReadFile(other.PoolConf("8.3"))
		e.m.SetFile(other.PoolConf("8.3"), []byte(strings.Replace(string(data), "listen = "+other.SocketFor("8.3"), "listen = "+l.SocketFor("8.3"), 1)), 0o644, system.Root)
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopPool], checks.Fail, other.PoolConf("8.3")+" ["+other.Pool+"]")
		wantHop(t, h[HopUser], checks.Fail, other.User)
	})
	t.Run("không pool nào nghe", func(t *testing.T) {
		e := newRouteEnv(t, Nginx)
		edit(t, e, "listen = "+l.SocketFor("8.3"), "listen = /run/php/khac.sock")
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopPool], checks.Fail, "")
		if !strings.Contains(h[HopPool].Summary.Vi, "502") {
			t.Errorf("câu pool = %s", h[HopPool].Summary.Vi)
		}
		wantHop(t, h[HopUser], checks.Unknown, "")
		wantHop(t, h[HopOpenBasedir], checks.Unknown, "")
	})
}

func TestRouteExplainCommandProblems(t *testing.T) {
	t.Run("nginx -T báo lỗi", func(t *testing.T) {
		e := newRouteEnv(t, Nginx)
		e.m.Handle([]string{"nginx", "-T"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "nginx: [emerg] unexpected \"}\" in /etc/nginx/sites-enabled/x.conf:3\n"}
		})
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopVhost], checks.Unknown, "")
		if !evidenceHas(h[HopVhost], "nginx -T", "[emerg]") {
			t.Errorf("bằng chứng: %+v", h[HopVhost].Evidence)
		}
		wantHop(t, h[HopRoot], checks.Unknown, "")
		// Chặng http vẫn gọi thật.
		if h[HopHTTP].Status != checks.Pass {
			t.Errorf("http = %s", h[HopHTTP].Status)
		}
	})
	t.Run("output lạ", func(t *testing.T) {
		e := newRouteEnv(t, Apache)
		e.m.Handle([]string{"apache2ctl", "-S"}, func(system.Cmd) system.Result { return system.Result{Stdout: "xin chào\n"} })
		wantHop(t, routeHops(t, explain(t, e, routeDom))[HopVhost], checks.Unknown, "")
	})
}

func TestRouteExplainRefusals(t *testing.T) {
	e := newRouteEnv(t, Nginx)
	if _, err := e.s.RouteExplain(ctx, mustDomain(t, "khong-co.example.com")); refusedCode(t, err) != "site_not_found" {
		t.Errorf("site không có: %v", err)
	}
	st := e.site(t, routeDom)
	st.Status = site.Trashed
	e.setSite(t, st)
	if _, err := e.s.RouteExplain(ctx, mustDomain(t, routeDom)); refusedCode(t, err) != "site_trashed" {
		t.Errorf("site ở thùng rác: %v", err)
	}

	bare := newSiteEnv(t, "")
	d := mustDomain(t, routeDom)
	if err := bare.sites.SaveSite(ctx, site.Site{Domain: d, Status: site.Active}); err != nil {
		t.Fatal(err)
	}
	if _, err := bare.s.RouteExplain(ctx, d); refusedCode(t, err) != "stack_not_installed" {
		t.Errorf("stack chưa cài: %v", err)
	}
}

func TestRouteNginxParser(t *testing.T) {
	dump := strings.Join([]string{
		"# configuration file /etc/nginx/nginx.conf:",
		"events { }",
		"http {",
		"    server_name_in_redirect off; # không phải khối server",
		"    server {",
		"        listen 127.0.0.1:8080 default_server;",
		"        listen [::]:8080;",
		"        server_name  A.Example.com.  \"b.example.com\";",
		"        location ~ \"^/x{2}\" { root /khong/lay; }",
		"        set $v \"${host}\";",
		"        root /srv/a; # chú thích; không phải chỉ thị",
		"        location ~ \\.php$ { fastcgi_pass unix:/run/php/a.sock; }",
		"    }",
		"}",
		"# configuration file /etc/nginx/conf.d/x.conf:",
		"server { server_name c.example.com; }",
		"map $a $b { default 0; }",
	}, "\n")
	decls, ok := ngxDumpDecls(dump)
	if !ok {
		t.Fatal("không nhận ra bản in")
	}
	var got []string
	for _, d := range decls {
		got = append(got, fmt.Sprintf("%s|%s|%s:%d", d.name, d.port, d.file, d.line))
	}
	want := []string{
		"a.example.com|8080|/etc/nginx/nginx.conf:7", "b.example.com|8080|/etc/nginx/nginx.conf:7",
		"c.example.com|80|/etc/nginx/conf.d/x.conf:1",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("decls = %v\nmuốn   %v", got, want)
	}

	srv := ngxServers(strings.Split(dump, "\n")[1:14])
	if len(srv) != 1 {
		t.Fatalf("số khối server = %d", len(srv))
	}
	if st, ok := srv[0].first("root", true); !ok || st.words[1] != "/srv/a" {
		t.Errorf("root cấp server = %+v %v", st, ok)
	}
	if st, ok := srv[0].first("fastcgi_pass", false); !ok || st.words[1] != "unix:/run/php/a.sock" {
		t.Errorf("fastcgi_pass = %+v %v", st, ok)
	}
	if _, ok := ngxDumpDecls("server { server_name a.example.com; }"); ok {
		t.Error("bản in không có dòng mở file mà vẫn ok")
	}
}

func TestRouteApacheParser(t *testing.T) {
	out := strings.Join([]string{
		"AH00558: apache2: Could not reliably determine the server's fully qualified domain name",
		"VirtualHost configuration:",
		"*:443                   a.example.com (/etc/apache2/sites-enabled/tpan-a.example.com.conf:38)",
		"*:80                    is a NameVirtualHost",
		"         default server tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:3)",
		"         port 80 namevhost tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:3)",
		"         port 80 namevhost A.example.com (/etc/apache2/sites-enabled/tpan-a.example.com.conf:5)",
		"                 alias www.a.example.com",
		"ServerRoot: \"/etc/apache2\"",
	}, "\n")
	decls, ok := apacheDumpDecls(out)
	if !ok {
		t.Fatal("không nhận ra output")
	}
	var got []string
	for _, d := range decls {
		got = append(got, fmt.Sprintf("%s|%s|%d", d.name, d.port, d.line))
	}
	want := "a.example.com|443|38 tpan-default.invalid|80|3 a.example.com|80|5 www.a.example.com|80|5"
	if strings.Join(got, " ") != want {
		t.Errorf("decls = %v\nmuốn   %s", got, want)
	}
	if _, ok := apacheDumpDecls("port 80 namevhost a.example.com (/x:1)"); ok {
		t.Error("output thiếu dòng tiêu đề mà vẫn ok")
	}

	lines := strings.Split("# đầu file\n<VirtualHost *:80>\n  ServerName a.example.com\n  # DocumentRoot /chu/thich\n  documentroot '/srv/a'\n  <FilesMatch \"\\.php$\">\n    SetHandler \"proxy:unix:/run/php/a.sock|fcgi://localhost\"\n  </FilesMatch>\n</VirtualHost>\n<VirtualHost *:443>\n  DocumentRoot /srv/b\n</VirtualHost>", "\n")
	root, rootLine, pass, passLine, found := apacheVhostTargets(lines, 2)
	if !found || root != "/srv/a" || rootLine != 5 || pass != "unix:/run/php/a.sock" || passLine != 7 {
		t.Errorf("khối 80 = %q:%d %q:%d %v", root, rootLine, pass, passLine, found)
	}
	if root, _, pass, _, found := apacheVhostTargets(lines, 10); !found || root != "/srv/b" || pass != "" {
		t.Errorf("khối 443 = %q %q %v", root, pass, found)
	}
	if _, _, _, _, found := apacheVhostTargets(lines, 3); found {
		t.Error("dòng không mở VirtualHost mà vẫn found")
	}
}
