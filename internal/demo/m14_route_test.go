package demo

// Đợt 2 của M14 trên máy demo: route explain cho từng site mẫu (cả sau khi
// chuyển sang Apache), dữ liệu cho ma trận cô lập, và lần quét .htaccess đọc
// đúng file của máy giả.

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var routeAll = []string{core.HopDNS, core.HopVhost, core.HopRoot, core.HopSocket, core.HopPool, core.HopUser, core.HopOpenBasedir, core.HopHTTP}

// hopStatuses trả "khoá=trạng thái" của từng chặng, để so cả đường đi một lần.
func hopStatuses(tr core.RouteTrace) (keys []string, bad []string) {
	for _, h := range tr.Hops {
		keys = append(keys, h.Key)
		if h.Status != checks.Pass {
			bad = append(bad, h.Key+"="+string(h.Status)+" ("+h.Summary.Vi+")")
		}
	}
	return keys, bad
}

func TestRouteExplainOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)
	explain := func(dom string) core.RouteTrace {
		t.Helper()
		tr, err := s.RouteExplain(ctx, domain(t, dom))
		if err != nil {
			t.Fatalf("RouteExplain %s: %v", dom, err)
		}
		for _, h := range tr.Hops {
			// Chặng "chưa rõ" vì chặng trước hỏng thì không chạy gì, nên không có bằng chứng.
			if h.Status != checks.Unknown && (len(h.Evidence) == 0 || h.Evidence[0].Command == "") {
				t.Errorf("%s: chặng %s không có bằng chứng", dom, h.Key)
			}
		}
		return tr
	}

	// Site thường và site có SSL: đủ tám chặng, tất cả đạt (kể cả DNS của demo).
	for _, dom := range []string{siteShop, siteBlog} {
		tr := explain(dom)
		keys, bad := hopStatuses(tr)
		if tr.Web != core.Nginx || !slices.Equal(keys, routeAll) || len(bad) > 0 {
			t.Errorf("%s: web %s, chặng %v, không đạt %v", dom, tr.Web, keys, bad)
		}
		l := demoLayouts(t, dom)[0]
		if !strings.HasPrefix(tr.Hops[1].Value, l.NginxLink+":") || tr.Hops[2].Value != l.Public || tr.Hops[5].Value != l.User {
			t.Errorf("%s: vhost %q, root %q, user %q", dom, tr.Hops[1].Value, tr.Hops[2].Value, tr.Hops[5].Value)
		}
	}
	// Site hỏng socket: cấu hình vẫn đúng đường, chỉ chặng gọi thật đỏ.
	if _, bad := hopStatuses(explain(siteWiki)); len(bad) != 1 || !strings.HasPrefix(bad[0], core.HopHTTP+"=fail") {
		t.Errorf("wiki: không đạt = %v; muốn chỉ http", bad)
	}
	// Site tạm khoá: dừng sau vhost, gọi thật ra trang tạm khoá.
	keys, bad := hopStatuses(explain(siteLab))
	if !slices.Equal(keys, []string{core.HopDNS, core.HopVhost, core.HopHTTP}) || len(bad) > 0 {
		t.Errorf("lab: chặng %v, không đạt %v", keys, bad)
	}
	// Site ở thùng rác: từ chối, không lần.
	if _, err := s.RouteExplain(ctx, domain(t, siteOld)); err == nil || !strings.Contains(err.Error(), "thùng rác") {
		t.Errorf("site ở thùng rác: %v", err)
	}

	// Lỗ L8 trên demo: vhost thêm tay đứng trước nhận tên miền của shop.
	const stray = "/etc/nginx/sites-enabled/aaa-la.conf"
	sys.SetFile(stray, []byte("server {\n    listen 80;\n    server_name "+siteShop+";\n    return 200 \"la\";\n}\n"), 0o644, system.Root)
	tr := explain(siteShop)
	if tr.Hops[1].Status != checks.Fail || tr.Hops[1].Value != stray+":3" || tr.Hops[2].Status != checks.Fail {
		t.Errorf("vhost lạ: %s %q, root %s", tr.Hops[1].Status, tr.Hops[1].Value, tr.Hops[2].Status)
	}
	if err := sys.Remove(stray); err != nil {
		t.Fatal(err)
	}

	// Chuyển sang Apache (chữa wiki trước, như test của M10): cùng đường đi,
	// lần này đọc từ apache2ctl -S và vhost Apache.
	if code, _ := run(t, sys, "systemctl", "restart", "php"+phpVer+"-fpm"); code != 0 {
		t.Fatalf("restart php-fpm: mã %d", code)
	}
	p, err := demoSwitch(sys, st).Prepare(ctx, core.Apache)
	runOK(t, ctx, "nginx → apache")(p, err)
	for _, dom := range []string{siteShop, siteBlog, siteWiki} {
		tr := explain(dom)
		keys, bad := hopStatuses(tr)
		l := demoLayouts(t, dom)[0]
		if tr.Web != core.Apache || !slices.Equal(keys, routeAll) || len(bad) > 0 || !strings.HasPrefix(tr.Hops[1].Value, l.ApacheLink+":") {
			t.Errorf("apache %s: web %s, chặng %v, vhost %q, không đạt %v", dom, tr.Web, keys, tr.Hops[1].Value, bad)
		}
	}
	if keys, bad := hopStatuses(explain(siteLab)); len(keys) != 3 || len(bad) > 0 {
		t.Errorf("apache lab: chặng %v, không đạt %v", keys, bad)
	}
}

// Ma trận cô lập của màn Bảo mật đọc bảng check_results: sau một lần health
// (như `tpan serve --demo` chạy lúc khởi động) mọi cặp site active có kết quả,
// và mọi site mẫu mang một chế độ .htaccess.
func TestIsolationDataOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	demoHealth(t, ctx, sys, st)
	stored, err := st.CheckResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]checks.Status{}
	for _, r := range stored {
		have[r.ID] = r.Status
	}
	active := []string{siteBlog, siteLab, siteShop, siteWiki}
	for _, from := range active {
		for _, to := range active {
			if from != to && have["isolation:"+from+":"+to] != checks.Pass {
				t.Errorf("isolation:%s:%s = %q; muốn pass", from, to, have["isolation:"+from+":"+to])
			}
		}
	}
	for _, id := range []string{"php.opcache:" + phpVer, "php.protectproc:" + phpVer, "web.server_name.unique", "web.default.rejects", "log.perm:" + siteLab, "web.hardened:" + siteShop} {
		if have[id] != checks.Pass {
			t.Errorf("%s = %q; muốn pass", id, have[id])
		}
	}
	sites, err := st.Sites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		if s.Htaccess != site.HtaccessSafe {
			t.Errorf("site mẫu %s có chế độ .htaccess %q; muốn an-toan", s.Domain, s.Htaccess)
		}
	}
}

// find và head của máy giả trả lời theo file thật, nên lần quét .htaccess
// không "sạch" giả: chỉ thị cấm được nêu đúng file và dòng, .htaccess trong
// uploads bị bỏ như trên máy thật.
func TestScanHtaccessOnDemo(t *testing.T) {
	ctx, sys, _ := newDemo(t)
	l := demoLayouts(t, siteShop)[0]
	owner := system.Owner{User: l.User, Group: l.User}

	found, ev, err := checks.ScanHtaccess(ctx, sys, l)
	if err != nil || len(found) != 0 || len(ev) == 0 {
		t.Fatalf("máy demo mới: %v, %+v", err, found)
	}

	sys.SetFile(l.Public+"/.htaccess", []byte("# BEGIN WordPress\nRewriteEngine On\nSetHandler application/x-httpd-php\n# END WordPress\n"), 0o644, owner)
	sys.SetFile(l.Public+"/wp-content/uploads/.htaccess", []byte("AddHandler cgi-script .php\n"), 0o644, owner)
	sys.SetFile(l.Public+"/wp-admin/.htaccess", []byte("Options +FollowSymLinks\n"), 0o644, owner)
	found, _, err = checks.ScanHtaccess(ctx, sys, l)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range found {
		got = append(got, strings.TrimPrefix(f.File, l.Public)+":"+string(rune('0'+f.Line))+":"+f.Directive)
	}
	slices.Sort(got)
	if want := []string{"/.htaccess:3:SetHandler", "/wp-admin/.htaccess:1:Options FollowSymLinks"}; !slices.Equal(got, want) {
		t.Errorf("Finding = %v; muốn %v", got, want)
	}
	// Site khác không bị lẫn file của shop.
	if found, _, err := checks.ScanHtaccess(ctx, sys, demoLayouts(t, siteBlog)[0]); err != nil || len(found) != 0 {
		t.Errorf("blog: %v, %+v", err, found)
	}
}
