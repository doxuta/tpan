package steps

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

const webAcme = "/var/lib/tpan/acme"

func webLayout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

// webBase là máy đã cài stack: có các thư mục config mà bước của site ghi vào.
func webBase(m *machine, l site.Layout) {
	for _, d := range []string{
		"/etc/php/8.3/fpm/pool.d",
		"/etc/nginx/sites-available", "/etc/nginx/sites-enabled",
		"/etc/apache2/sites-available", "/etc/apache2/sites-enabled",
	} {
		m.SetDir(d, 0o755, system.Root)
	}
	m.SetDir(l.Public, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
}

// webStepOK và webStepsOK nhận thẳng hai giá trị trả về của hàm dựng bước:
// webStepOK(t)(SitePool(l, "8.3")). Lỗi thì dừng test.
func webStepOK(t *testing.T) func(engine.Step, error) engine.Step {
	return func(s engine.Step, err error) engine.Step {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
}

func webStepsOK(t *testing.T) func([]engine.Step, error) []engine.Step {
	return func(s []engine.Step, err error) []engine.Step {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
}

// Bốn trường hợp của skill tpan-step cho từng bước mà các hàm dựng ra.
func TestSiteWebLifecycle(t *testing.T) {
	l := webLayout(t)
	base := func(m *machine) { webBase(m, l) }
	pool := func() engine.Step { return webStepOK(t)(SitePool(l, "8.3")) }
	vhost := func(web string, i int) func() engine.Step {
		return func() engine.Step { return webStepsOK(t)(SiteVhost(l, web, webAcme))[i] }
	}
	// Máy đã có đủ vhost và pool của site, để thử các bước gỡ.
	full := func(m *machine) {
		webBase(m, l)
		execute(t, m, pool())
		execute(t, m, webStepsOK(t)(SiteVhost(l, "nginx", webAcme))...)
	}
	absent := func(i int) func() engine.Step {
		return func() engine.Step { return webStepsOK(t)(SiteWebAbsent(l, "nginx", "8.3"))[i] }
	}
	cases := []lifecycle{
		{name: "SitePool", setup: base, step: pool},
		{name: "SiteVhost nginx file", setup: base, step: vhost("nginx", 0)},
		{name: "SiteVhost nginx link", setup: func(m *machine) { base(m); execute(t, m, vhost("nginx", 0)()) }, step: vhost("nginx", 1)},
		{name: "SiteVhost apache file", setup: base, step: vhost("apache", 0)},
		{name: "SiteVhost apache link", setup: func(m *machine) { base(m); execute(t, m, vhost("apache", 0)()) }, step: vhost("apache", 1)},
		{name: "SiteHtaccess", setup: base, step: func() engine.Step { return webStepOK(t)(SiteHtaccess(l)) }},
		{name: "SiteWebAbsent link", setup: full, step: absent(0)},
		{name: "SiteWebAbsent conf", setup: func(m *machine) { full(m); execute(t, m, absent(0)()) }, step: absent(1)},
		{name: "SiteWebAbsent pool", setup: func(m *machine) { full(m); execute(t, m, absent(0)(), absent(1)()) }, step: absent(2)},
	}
	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}

// Đường dẫn, quyền, chủ và đúng lệnh Test/Reload của từng bước.
func TestSiteWebPaths(t *testing.T) {
	l := webLayout(t)
	phpTest := &system.Cmd{Name: "php-fpm8.3", Args: []string{"-t"}}
	phpReload := &system.Cmd{Name: "systemctl", Args: []string{"reload", "php8.3-fpm"}}
	apTest := &system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
	apReload := &system.Cmd{Name: "systemctl", Args: []string{"reload", "apache2"}}

	p := webStepOK(t)(SitePool(l, "8.3")).(*File)
	want, _ := templates.SitePool(l)
	if p.Path != "/etc/php/8.3/fpm/pool.d/tpan-"+l.Slug.String()+".conf" || p.Perm != 0o644 || p.Owner != system.Root ||
		!reflect.DeepEqual(p.Test, phpTest) || !reflect.DeepEqual(p.Reload, phpReload) || string(p.Content) != string(want) {
		t.Errorf("SitePool sai: %+v", p)
	}

	ngConf, _ := templates.NginxSite(l, webAcme)
	apConf, _ := templates.ApacheSite(l, webAcme)
	for web, w := range map[string]struct {
		conf, link   string
		test, reload *system.Cmd
		content      []byte
	}{
		"nginx":  {l.NginxConf, l.NginxLink, nginxTest, nginxReload, ngConf},
		"apache": {l.ApacheConf, l.ApacheLink, apTest, apReload, apConf},
	} {
		ss := webStepsOK(t)(SiteVhost(l, web, webAcme))
		if len(ss) != 2 {
			t.Fatalf("%s: %d bước, muốn 2", web, len(ss))
		}
		f, ok1 := ss[0].(*File)
		s, ok2 := ss[1].(*Symlink)
		if !ok1 || !ok2 {
			t.Fatalf("%s: thứ tự bước sai: %T, %T", web, ss[0], ss[1])
		}
		if f.Path != w.conf || f.Perm != 0o644 || f.Owner != system.Root || s.Target != w.conf || s.Link != w.link {
			t.Errorf("%s: đường dẫn sai: file %s, link %s → %s", web, f.Path, s.Link, s.Target)
		}
		if len(w.content) == 0 || string(f.Content) != string(w.content) {
			t.Errorf("%s: nội dung vhost không phải template của %s", web, web)
		}
		for _, c := range [][2]*system.Cmd{{f.Test, w.test}, {f.Reload, w.reload}, {s.Test, w.test}, {s.Reload, w.reload}} {
			if !reflect.DeepEqual(c[0], c[1]) {
				t.Errorf("%s: lệnh %v, muốn %v", web, c[0], c[1])
			}
		}

		ab := webStepsOK(t)(SiteWebAbsent(l, web, "8.3"))
		var paths []string
		for _, st := range ab {
			paths = append(paths, st.(*Absent).Path)
		}
		if want := []string{w.link, w.conf, l.PoolConf("8.3")}; !reflect.DeepEqual(paths, want) {
			t.Errorf("%s: SiteWebAbsent gỡ %v, muốn %v (vhost trước pool)", web, paths, want)
		}
		if a := ab[0].(*Absent); !reflect.DeepEqual(a.Test, w.test) || !reflect.DeepEqual(a.Reload, w.reload) {
			t.Errorf("%s: Absent link sai lệnh", web)
		}
		if a := ab[2].(*Absent); !reflect.DeepEqual(a.Test, phpTest) || !reflect.DeepEqual(a.Reload, phpReload) {
			t.Errorf("%s: Absent pool sai lệnh", web)
		}
	}

	h := webStepOK(t)(SiteHtaccess(l)).(*File)
	if h.Path != l.Public+"/.htaccess" || h.Perm != 0o640 || h.Owner != (system.Owner{User: l.User, Group: "www-data"}) ||
		h.Test != nil || h.Reload != nil || string(h.Content) != string(templates.WPHtaccess()) {
		t.Errorf("SiteHtaccess sai: %+v", h)
	}
}

// Cả chuỗi bước của một site trên Nginx: ghi pool, ghi vhost, bật vhost; chạy
// lại thì không lệnh nào chạy; rồi gỡ hết thì máy về như trước.
func TestSiteWebChain(t *testing.T) {
	l := webLayout(t)
	m := newMachine(t)
	webBase(m, l)
	before := m.Snapshot()

	add := func() []engine.Step {
		return append([]engine.Step{webStepOK(t)(SitePool(l, "8.3"))}, webStepsOK(t)(SiteVhost(l, "nginx", webAcme))...)
	}
	if rep := execute(t, m, add()...); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
	}
	want := []string{
		"php-fpm8.3 -t", "systemctl reload php8.3-fpm",
		"nginx -t", "systemctl reload nginx",
		"nginx -t", "systemctl reload nginx",
	}
	if got := ranStrings(m.Ran()); !reflect.DeepEqual(got, want) {
		t.Errorf("lệnh: %v, muốn %v", got, want)
	}

	m.ResetRan()
	execute(t, m, add()...)
	if len(m.Ran()) != 0 {
		t.Errorf("chạy lại vẫn chạy lệnh: %v", ranStrings(m.Ran()))
	}

	m.ResetRan()
	if rep := execute(t, m, webStepsOK(t)(SiteWebAbsent(l, "nginx", "8.3"))...); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("gỡ: %s: %s", rep.Job.Status, rep.Job.Error)
	}
	want = []string{
		"nginx -t", "systemctl reload nginx",
		"nginx -t", "systemctl reload nginx",
		"php-fpm8.3 -t", "systemctl reload php8.3-fpm",
	}
	if got := ranStrings(m.Ran()); !reflect.DeepEqual(got, want) {
		t.Errorf("lệnh gỡ: %v, muốn %v", got, want)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Error("thêm rồi gỡ mà máy khác lúc đầu")
	}
}

// php-fpm -t hay nginx -t lỗi: lỗi có ngữ cảnh, file mới không ở lại.
func TestSiteWebCommandErrors(t *testing.T) {
	l := webLayout(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		prefix []string
		step   engine.Step
		path   string
	}{
		{"pool", []string{"php-fpm8.3", "-t"}, webStepOK(t)(SitePool(l, "8.3")), l.PoolConf("8.3")},
		{"vhost", []string{"nginx", "-t"}, webStepsOK(t)(SiteVhost(l, "nginx", webAcme))[0], l.NginxConf},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMachine(t)
			webBase(m, l)
			m.failCmd(c.prefix, 1, "dòng 7: lỗi cú pháp giả")
			err := c.step.Apply(ctx, m)
			if err == nil {
				t.Fatal("Apply không lỗi khi kiểm cú pháp lỗi")
			}
			for _, s := range []string{"kiểm cấu hình", c.path, "lỗi cú pháp giả"} {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("lỗi thiếu %q: %v", s, err)
				}
			}
			if _, err := m.Stat(c.path); err == nil {
				t.Errorf("%s còn lại sau khi kiểm lỗi", c.path)
			}
		})
	}
}

// Tham số sai: hàm dựng trả lỗi, không trả bước nào.
func TestSiteWebBadParams(t *testing.T) {
	l := webLayout(t)
	tampered := l
	tampered.Public = "/var/www/example.com/public; rm -rf /"
	layouts := map[string]site.Layout{"zero": {}, "sửa tay": tampered}
	badVer := []string{"", "8", "8.3; rm -rf /", "$(id)", "../8.3", "8.3\n", "8.3.1", "a.b", " 8.3"}
	badWeb := []string{"", "Nginx", "apache2", "nginx;", "caddy", "nginx\n", "../nginx"}

	for name, bl := range layouts {
		if s, err := SitePool(bl, "8.3"); err == nil || s != nil {
			t.Errorf("SitePool Layout %s: muốn lỗi", name)
		}
		if s, err := SiteVhost(bl, "nginx", webAcme); err == nil || s != nil {
			t.Errorf("SiteVhost Layout %s: muốn lỗi", name)
		}
		if s, err := SiteHtaccess(bl); err == nil || s != nil {
			t.Errorf("SiteHtaccess Layout %s: muốn lỗi", name)
		}
		if s, err := SiteWebAbsent(bl, "nginx", "8.3"); err == nil || s != nil {
			t.Errorf("SiteWebAbsent Layout %s: muốn lỗi", name)
		}
	}
	for _, v := range badVer {
		if s, err := SitePool(l, v); err == nil || s != nil {
			t.Errorf("SitePool bản %q: muốn lỗi", v)
		}
		if s, err := SiteWebAbsent(l, "nginx", v); err == nil || s != nil {
			t.Errorf("SiteWebAbsent bản %q: muốn lỗi", v)
		}
	}
	for _, w := range badWeb {
		if s, err := SiteVhost(l, w, webAcme); err == nil || s != nil {
			t.Errorf("SiteVhost web %q: muốn lỗi", w)
		}
		if s, err := SiteWebAbsent(l, w, "8.3"); err == nil || s != nil {
			t.Errorf("SiteWebAbsent web %q: muốn lỗi", w)
		}
	}
	for _, r := range []string{"", "acme", "/var/lib/acme;", "/var/lib/$(id)", "/var/lib/../etc"} {
		if s, err := SiteVhost(l, "apache", r); err == nil || s != nil {
			t.Errorf("SiteVhost acmeRoot %q: muốn lỗi", r)
		}
	}
}

// Info của bước pool nói đúng lệnh sẽ chạy và file sẽ ghi.
func TestSiteWebInfo(t *testing.T) {
	l := webLayout(t)
	info := webStepOK(t)(SitePool(l, "8.3")).Info()
	if !reflect.DeepEqual(info.Commands, []string{"php-fpm8.3 -t", "systemctl reload php8.3-fpm"}) {
		t.Errorf("Commands = %v", info.Commands)
	}
	if len(info.Files) != 1 || info.Files[0].Path != l.PoolConf("8.3") {
		t.Errorf("Files = %v", info.Files)
	}
	if info.Title.IsZero() || info.Why.IsZero() {
		t.Error("thiếu Title hoặc Why")
	}
}
