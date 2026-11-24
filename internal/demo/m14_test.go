package demo

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

func demoLayouts(t *testing.T, doms ...string) []site.Layout {
	t.Helper()
	var ls []site.Layout
	for _, d := range doms {
		l, err := layoutOf(d)
		if err != nil {
			t.Fatal(err)
		}
		ls = append(ls, l)
	}
	return ls
}

func runCheck(ctx context.Context, sys system.System, c checks.Check) checks.Result {
	return checks.RunAll(ctx, sys, []checks.Check{c})[0]
}

func evidenceText(r checks.Result) string {
	var b strings.Builder
	for _, e := range r.Evidence {
		b.WriteString(e.Output + "\n")
	}
	return b.String()
}

// Máy demo là máy đã cài bằng bản có M14: bốn phép kiểm cấp máy đạt ngay, và
// đạt vì máy giả trả lời theo trạng thái của nó chứ không phải vì trả lời
// cứng: gỡ file hay thêm vhost trùng tên thì phép kiểm đỏ như trên máy thật.
func TestM14ChecksOnDemo(t *testing.T) {
	ctx, sys, _ := newDemo(t)
	ls := demoLayouts(t, siteBlog, siteShop, siteWiki)

	for _, c := range []checks.Check{
		checks.OPcacheHardened(phpVer),
		checks.PHPProtectProc(phpVer),
		checks.ServerNameUnique("nginx", namedOf(ls)),
		checks.DefaultRejects("nginx", true),
	} {
		if r := runCheck(ctx, sys, c); r.Status != checks.Pass {
			t.Errorf("%s: %s %s\n%s", r.ID, r.Status, r.Summary.Vi, evidenceText(r))
		}
	}

	// nginx -T phải thật sự in vhost của từng site: thiếu thì phép kiểm vẫn
	// "đạt" vì không thấy tên nào để trùng.
	ev := evidenceText(runCheck(ctx, sys, checks.ServerNameUnique("nginx", namedOf(ls))))
	for _, want := range []string{
		ls[0].NginxLink + ":", siteBlog + " (port 80)", siteBlog + " (port 443)", siteShop + " (port 80)", siteWiki + " (port 80)",
	} {
		if !strings.Contains(ev, want) {
			t.Errorf("bằng chứng của web.server_name.unique thiếu %q:\n%s", want, ev)
		}
	}

	// Lỗ L8: ai đó thêm tay một vhost khai trùng tên của shop.
	const stray = "/etc/nginx/sites-enabled/aaa-la.conf"
	sys.SetFile(stray, []byte("server {\n    listen 80;\n    server_name "+siteShop+";\n    return 200 \"la\";\n}\n"), 0o644, system.Root)
	if r := runCheck(ctx, sys, checks.ServerNameUnique("nginx", namedOf(ls))); r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, stray+":3") {
		t.Errorf("vhost lạ trùng tên: %s %s", r.Status, r.Summary.Vi)
	}

	// File ini bị sửa: php-fpm -i của máy giả thôi in On.
	ini := templates.PHPHardeningINIPath(phpVer)
	sys.SetFile(ini, []byte("; ai đó sửa tay\n"), 0o644, system.Root)
	if r := runCheck(ctx, sys, checks.OPcacheHardened(phpVer)); r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, "opcache.validate_permission đang là Off") {
		t.Errorf("ini bị sửa: %s %s", r.Status, r.Summary.Vi)
	}
	// Drop-in bị gỡ: systemd trả giá trị mặc định, chỉ là cảnh báo.
	if err := sys.Remove(templates.PHPDropInPath(phpVer)); err != nil {
		t.Fatal(err)
	}
	if r := runCheck(ctx, sys, checks.PHPProtectProc(phpVer)); r.Status != checks.Warn {
		t.Errorf("gỡ drop-in: %s %s", r.Status, r.Summary.Vi)
	}
}

// apache2ctl -S của máy giả dựng từ các vhost đang có trong sites-enabled, và
// checks.ServerNameUnique đọc được: cổng có nhiều vhost in dạng NameVirtualHost,
// vhost có SSL hiện ở cả hai cổng, vhost lạ trùng tên bị bắt.
func TestApacheVhostsOnDemo(t *testing.T) {
	m := newMachine()
	ctx := context.Background()
	ls := demoLayouts(t, siteBlog, siteShop)
	must := func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	m.SetFile("/etc/apache2/sites-enabled/000-tpan-default.conf", must(templates.ApacheDefault(templates.DefaultSite{AcmeRoot: acmeRoot})), 0o644, rootOwner)
	m.SetFile("/etc/apache2/sites-enabled/000-tpan-default-ssl.conf", must(templates.ApacheDefaultSSL()), 0o644, rootOwner)
	for i, ssl := range []bool{true, false} {
		m.SetFile(ls[i].ApacheConf, must(templates.SiteVhost(ls[i], "apache", phpVer, acmeRoot, ssl, site.HtaccessSafe)), 0o644, rootOwner)
		m.SetSymlink(ls[i].ApacheConf, ls[i].ApacheLink)
	}

	res, err := m.Run(ctx, system.Cmd{Name: "apache2ctl", Args: []string{"-S"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"VirtualHost configuration:\n",
		"*:80                   is a NameVirtualHost\n",
		"         default server tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:",
		"         port 80 namevhost " + siteShop + " (" + ls[1].ApacheLink + ":",
		"         port 443 namevhost " + siteBlog + " (" + ls[0].ApacheLink + ":",
	} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("apache2ctl -S thiếu %q:\n%s", want, res.Stdout)
		}
	}

	r := runCheck(ctx, m, checks.ServerNameUnique("apache", namedOf(ls)))
	if r.Status != checks.Pass {
		t.Fatalf("web.server_name.unique: %s %s", r.Status, r.Summary.Vi)
	}
	ev := evidenceText(r)
	for _, want := range []string{siteBlog + " (port 80)", siteBlog + " (port 443)", siteShop + " (port 80)"} {
		if !strings.Contains(ev, want) {
			t.Errorf("bằng chứng thiếu %q:\n%s", want, ev)
		}
	}

	const stray = "/etc/apache2/sites-enabled/aaa-la.conf"
	m.SetFile(stray, []byte("<VirtualHost *:80>\n    ServerName "+siteShop+"\n</VirtualHost>\n"), 0o644, rootOwner)
	if r := runCheck(ctx, m, checks.ServerNameUnique("apache", namedOf(ls))); r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, stray+":1") {
		t.Errorf("vhost lạ trùng tên: %s %s", r.Status, r.Summary.Vi)
	}
}

// namedOf bọc danh sách Layout thành NamedSite không tên phụ (M19 đổi chữ ký
// ServerNameUnique).
func namedOf(ls []site.Layout) []checks.NamedSite {
	out := make([]checks.NamedSite, 0, len(ls))
	for _, l := range ls {
		out = append(out, checks.NamedSite{Layout: l})
	}
	return out
}
