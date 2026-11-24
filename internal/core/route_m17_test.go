package core

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// cageFPMConf và cageService là hai file của lồng đúng dạng mục 3.3 của
// docs/thay-doi/M17-thiet-ke.md (chỉ các dòng route explain đọc, cộng vài dòng
// cho giống thật).
func cageFPMConf(l site.Layout, socket, basedir string) string {
	return "; Do tpan quản lý\n[global]\nerror_log = syslog\ndaemonize = no\n\n" +
		"[" + l.Pool + "]\nlisten = " + socket + "\npm = ondemand\npm.max_children = 5\n" +
		"php_admin_value[open_basedir] = " + basedir + "\n"
}

func cageService(l site.Layout, user string) string {
	return "# Do tpan quản lý\n[Unit]\nStartLimitIntervalSec=30\n\n[Service]\nType=notify\n" +
		"User=" + user + "\nGroup=" + user + "\nSlice=" + l.CageSlice() + "\nRestart=on-failure\n"
}

// cagedRouteEnv chuyển routeDom vào lồng như cage.enable để lại: vhost trỏ socket
// của lồng, pool chung biến mất, ba file của lồng có mặt.
func cagedRouteEnv(t *testing.T, web WebServer) (*fixEnv, site.Layout) {
	t.Helper()
	e := newRouteEnv(t, web)
	l := site.LayoutOf(mustDomain(t, routeDom))
	conf := l.NginxConf
	if web == Apache {
		conf = l.ApacheConf
	}
	data, _ := e.m.ReadFile(conf)
	if !strings.Contains(string(data), l.SocketFor("8.3")) {
		t.Fatalf("vhost không có socket của pool chung:\n%s", data)
	}
	e.m.SetFile(conf, []byte(strings.ReplaceAll(string(data), l.SocketFor("8.3"), l.CageSocket())), 0o644, system.Root)
	if err := e.m.Remove(l.PoolConf("8.3")); err != nil {
		t.Fatal(err)
	}
	e.m.SetFile(l.CageFPMConf(), []byte(cageFPMConf(l, l.CageSocket(), l.Dir+"/")), 0o644, system.Root)
	e.m.SetFile(l.CageServiceFile(), []byte(cageService(l, l.User)), 0o644, system.Root)
	st, _, _ := e.sites.Site(ctx, l.Domain)
	st.Caged = true
	if err := e.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	return e, l
}

func TestRouteExplainCaged(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e, l := cagedRouteEnv(t, web)
			tr := explain(t, e, routeDom)
			if got := routeKeysOf(tr); strings.Join(got, ",") != strings.Join(routeOrder, ",") {
				t.Fatalf("thứ tự chặng = %v", got)
			}
			h := routeHops(t, tr)
			link := l.NginxLink
			if web == Apache {
				link = l.ApacheLink
			}
			wantHop(t, h[HopSocket], checks.Pass, l.CageSocket())
			if !evidenceHas(h[HopSocket], "read "+link+" (tpan)", l.CageSocket()) {
				t.Errorf("bằng chứng socket: %+v", h[HopSocket].Evidence)
			}
			wantHop(t, h[HopPool], checks.Pass, l.CageFPMConf()+" ["+l.Pool+"]")
			if !evidenceHas(h[HopPool], l.CageFPMConf(), "listen = "+l.CageSocket(), "["+l.Pool+"]") {
				t.Errorf("bằng chứng pool: %+v", h[HopPool].Evidence)
			}
			wantHop(t, h[HopUser], checks.Pass, l.User)
			if !evidenceHas(h[HopUser], "read "+l.CageServiceFile()+" (tpan)", "User="+l.User) {
				t.Errorf("bằng chứng user: %+v", h[HopUser].Evidence)
			}
			wantHop(t, h[HopOpenBasedir], checks.Pass, l.Dir+"/")
			if !evidenceHas(h[HopOpenBasedir], "read "+l.CageFPMConf()+" (tpan)", "php_admin_value[open_basedir] = "+l.Dir+"/") {
				t.Errorf("bằng chứng open_basedir: %+v", h[HopOpenBasedir].Evidence)
			}
			wantHop(t, h[HopHTTP], checks.Pass, "http://"+routeDom+"/")
		})
	}
}

// Site trong lồng chỉ đọc: không ghi file, như site ngoài lồng.
func TestRouteExplainCagedReadOnly(t *testing.T) {
	e, _ := cagedRouteEnv(t, Nginx)
	before := e.m.Snapshot()
	explain(t, e, routeDom)
	after := e.m.Snapshot()
	if len(before) != len(after) {
		t.Fatalf("số mục hệ thống file đổi: %d -> %d", len(before), len(after))
	}
	for p, v := range before {
		if got, ok := after[p]; !ok || string(got.Data) != string(v.Data) {
			t.Errorf("RouteExplain đã đổi %s", p)
		}
	}
}

func TestRouteExplainCagedProblems(t *testing.T) {
	t.Run("vhost còn trỏ socket của pool chung", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		data, _ := e.m.ReadFile(l.NginxConf)
		e.m.SetFile(l.NginxConf, []byte(strings.ReplaceAll(string(data), l.CageSocket(), l.SocketFor("8.3"))), 0o644, system.Root)
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopSocket], checks.Fail, l.SocketFor("8.3"))
		if !strings.Contains(h[HopSocket].Summary.Vi, l.CageSocket()) {
			t.Errorf("câu socket không nêu socket của lồng: %s", h[HopSocket].Summary.Vi)
		}
		// Không file nào của lồng nghe socket đó: chặng pool lỗi, hai chặng sau bị chặn.
		wantHop(t, h[HopPool], checks.Fail, "")
		wantHop(t, h[HopUser], checks.Unknown, "")
		wantHop(t, h[HopOpenBasedir], checks.Unknown, "")
	})
	t.Run("thiếu php-fpm.conf của lồng", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		if err := e.m.Remove(l.CageFPMConf()); err != nil {
			t.Fatal(err)
		}
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopPool], checks.Unknown, "")
		if !evidenceHas(h[HopPool], "read "+l.CageFPMConf()) {
			t.Errorf("bằng chứng pool: %+v", h[HopPool].Evidence)
		}
		wantHop(t, h[HopUser], checks.Unknown, "")
	})
	t.Run("pool của lồng nghe socket khác", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		e.m.SetFile(l.CageFPMConf(), []byte(cageFPMConf(l, "/run/php/khac.sock", l.Dir+"/")), 0o644, system.Root)
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopPool], checks.Fail, "")
		if !strings.Contains(h[HopPool].Summary.Vi, "502") {
			t.Errorf("câu pool = %s", h[HopPool].Summary.Vi)
		}
	})
	t.Run("dịch vụ chạy dưới user khác", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		other := site.LayoutOf(mustDomain(t, routeOther))
		e.m.SetFile(l.CageServiceFile(), []byte(cageService(l, other.User)), 0o644, system.Root)
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopPool], checks.Pass, l.CageFPMConf()+" ["+l.Pool+"]")
		wantHop(t, h[HopUser], checks.Fail, other.User)
		if !strings.Contains(h[HopUser].Summary.Vi, l.User) {
			t.Errorf("câu user không nêu user đúng: %s", h[HopUser].Summary.Vi)
		}
	})
	t.Run("dịch vụ không có dòng User=", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		e.m.SetFile(l.CageServiceFile(), []byte("[Service]\nType=notify\n"), 0o644, system.Root)
		wantHop(t, routeHops(t, explain(t, e, routeDom))[HopUser], checks.Fail, "")
	})
	t.Run("User= nằm ngoài mục Service không tính", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		e.m.SetFile(l.CageServiceFile(), []byte("[Unit]\nUser="+l.User+"\n[Service]\nType=notify\n"), 0o644, system.Root)
		wantHop(t, routeHops(t, explain(t, e, routeDom))[HopUser], checks.Fail, "")
	})
	t.Run("User= khai hai lần thì lần sau thắng", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		e.m.SetFile(l.CageServiceFile(), []byte("[Service]\nUser=root\nUser="+l.User+"\n"), 0o644, system.Root)
		wantHop(t, routeHops(t, explain(t, e, routeDom))[HopUser], checks.Pass, l.User)
	})
	t.Run("thiếu file dịch vụ", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		if err := e.m.Remove(l.CageServiceFile()); err != nil {
			t.Fatal(err)
		}
		h := routeHops(t, explain(t, e, routeDom))
		wantHop(t, h[HopUser], checks.Unknown, "")
		if !evidenceHas(h[HopUser], "read "+l.CageServiceFile()) {
			t.Errorf("bằng chứng user: %+v", h[HopUser].Evidence)
		}
		wantHop(t, h[HopPool], checks.Pass, l.CageFPMConf()+" ["+l.Pool+"]")
	})
	t.Run("open_basedir của lồng mở rộng", func(t *testing.T) {
		e, l := cagedRouteEnv(t, Nginx)
		e.m.SetFile(l.CageFPMConf(), []byte(cageFPMConf(l, l.CageSocket(), "/")), 0o644, system.Root)
		wantHop(t, routeHops(t, explain(t, e, routeDom))[HopOpenBasedir], checks.Fail, "/")
	})
}

// Site ngoài lồng cạnh site trong lồng: vẫn đọc pool.d và socket cũ, dù có
// file của lồng nằm đó (không site nào được đọc nhầm nguồn).
func TestRouteExplainUncagedIgnoresCageFiles(t *testing.T) {
	e := newRouteEnv(t, Nginx)
	l := site.LayoutOf(mustDomain(t, routeDom))
	e.m.SetFile(l.CageFPMConf(), []byte(cageFPMConf(l, l.CageSocket(), "/")), 0o644, system.Root)
	e.m.SetFile(l.CageServiceFile(), []byte(cageService(l, "root")), 0o644, system.Root)
	h := routeHops(t, explain(t, e, routeDom))
	wantHop(t, h[HopSocket], checks.Pass, l.SocketFor("8.3"))
	wantHop(t, h[HopPool], checks.Pass, l.PoolConf("8.3")+" ["+l.Pool+"]")
	wantHop(t, h[HopUser], checks.Pass, l.User)
	wantHop(t, h[HopOpenBasedir], checks.Pass, l.Dir+"/")
}
