package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func TestAutoFixName(t *testing.T) {
	for _, tc := range []struct {
		id     string
		status checks.Status
		want   string
	}{
		{"service.active:nginx", checks.Fail, FixServiceStart},
		{"service.active:nginx", checks.Warn, ""}, // chạy nhưng không bật lúc khởi động: sửa tay
		{"service.active:nginx", checks.Pass, ""},
		{"service.active:nginx", checks.Unknown, ""},
		{"php.socket:shop.example.com", checks.Fail, FixPHPRestart},
		{"drift:/etc/nginx/sites-available/tpan-x.conf", checks.Warn, FixFileRewrite},
		{"drift:/etc/nginx/sites-available/tpan-x.conf", checks.Fail, FixFileRewrite},
		{"drift:/etc/nginx/sites-available/tpan-x.conf", checks.Pass, ""},
		{"drift:/etc/nginx/sites-available/tpan-x.conf", checks.Unknown, ""},
		{"config.nginx:nginx", checks.Fail, FixWebRewrite},
		{"config.apache:apache2", checks.Fail, FixWebRewrite},
		{"config.nginx:nginx", checks.Warn, ""},
		{"web.vhost:shop.example.com", checks.Fail, FixWebRewrite},
		{"web.vhost:shop.example.com", checks.Pass, ""},
		{"wp.config:shop.example.com", checks.Fail, FixWPConfigPerm},
		{"db.access:shop.example.com", checks.Fail, FixDBUnlock},
		{"disk.usage:/", checks.Fail, ""},
		{"http.site:shop.example.com", checks.Fail, ""},
		{"memory:ram", checks.Fail, ""},
		{"", checks.Fail, ""},
		{"service.active", checks.Fail, FixServiceStart}, // tên đúng; Prepare từ chối vì thiếu đối tượng
	} {
		if got := AutoFixName(checks.Result{ID: tc.id, Status: tc.status}); got != tc.want {
			t.Errorf("AutoFixName(%q, %s) = %q, muốn %q", tc.id, tc.status, got, tc.want)
		}
	}
}

// fixEnv là siteEnv cộng Fixes trên cùng máy giả và store, kèm giả lập
// systemd (đủ cho checks.Service, steps.Service) và trạng thái khoá user DB.
type fixEnv struct {
	*siteEnv
	f *Fixes

	mu     sync.Mutex
	units  map[string]unitState
	locked map[string]bool // user DB bị khoá
}

var (
	fixLockState = "SELECT COUNT(*), COALESCE(MAX(JSON_VALUE(Priv, '$.account_locked') IN ('1', 'true')), 0) FROM mysql.global_priv WHERE User='"
	fixAlter     = "ALTER USER '"
)

func newFixEnv(t *testing.T, web WebServer, doms ...string) *fixEnv {
	t.Helper()
	se := newSiteEnv(t, web)
	for _, d := range doms {
		se.add(t, d)
	}
	e := &fixEnv{siteEnv: se, units: map[string]unitState{}, locked: map[string]bool{}}
	for _, u := range []string{"nginx", "apache2", "mariadb", "php8.3-fpm"} {
		e.units[u] = unitState{active: true, enabled: true}
	}
	m := se.m
	m.Handle([]string{"systemctl"}, func(c system.Cmd) system.Result {
		e.mu.Lock()
		defer e.mu.Unlock()
		unit := c.Args[len(c.Args)-1]
		u := e.units[unit]
		switch c.Args[0] {
		case "is-active":
			if u.active {
				return system.Result{Stdout: "active\n"}
			}
			return system.Result{ExitCode: 3, Stdout: "inactive\n"}
		case "is-enabled":
			if u.enabled {
				return system.Result{Stdout: "enabled\n"}
			}
			return system.Result{ExitCode: 1, Stdout: "disabled\n"}
		case "enable":
			u.active, u.enabled = true, true
		case "stop":
			u.active = false
		case "disable":
			u.enabled = false
		}
		e.units[unit] = u
		return system.Result{}
	})
	// PHP-FPM khởi động lại thì mọi pool có socket (reload giữ handler của siteSim).
	m.Handle([]string{"systemctl", "restart", "php8.3-fpm"}, func(system.Cmd) system.Result {
		names, _ := m.ReadDir("/etc/php/8.3/fpm/pool.d")
		for _, n := range names {
			if pool, ok := strings.CutSuffix(n, ".conf"); ok {
				m.SetSocket("/run/php/"+pool+".sock", 0o660, system.Owner{User: "www-data", Group: "www-data"})
			}
		}
		return system.Result{}
	})
	// Câu khoá của DBUserUnlock; các câu khác (dùng lúc tạo site) không tới đây
	// nữa vì site đã tạo xong ở trên.
	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		e.mu.Lock()
		defer e.mu.Unlock()
		sql := string(c.Stdin)
		name := func(prefix string) string {
			rest := strings.TrimPrefix(sql, prefix)
			n, _, _ := strings.Cut(rest, "'")
			return n
		}
		switch {
		case strings.HasPrefix(sql, fixLockState):
			n := name(fixLockState)
			b := 0
			if e.locked[n] {
				b = 1
			}
			return system.Result{Stdout: fmt.Sprintf("%d\t%d\n", 1, b)}
		case strings.HasPrefix(sql, fixAlter):
			e.locked[name(fixAlter)] = strings.Contains(sql, "ACCOUNT LOCK;")
			return system.Result{}
		}
		return system.Result{ExitCode: 1, Stderr: "fixEnv: câu SQL lạ\n"}
	})
	// wp db query dưới user bị khoá: lỗi 4151 như MariaDB thật.
	m.Handle([]string{"wp", "db", "query"}, func(c system.Cmd) system.Result {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.locked[c.AsUser] {
			return system.Result{ExitCode: 1, Stderr: "ERROR 4151 (HY000): Access denied, this account is locked\n"}
		}
		return system.Result{Stdout: "information_schema\n" + c.AsUser + "\n"}
	})
	e.f = &Fixes{Sys: m, Settings: se.st, Store: se.sites, Journal: se.st}
	return e
}

// fix chạy Prepare, Preview, Execute cho r và đòi job thành công.
func (e *fixEnv) fix(t *testing.T, r checks.Result) *Fix {
	t.Helper()
	x, err := e.f.Prepare(ctx, r)
	if err != nil {
		t.Fatalf("Prepare %s: %v", r.ID, err)
	}
	if want := "fix." + AutoFixName(r); x.Plan.Kind != want || x.Name != AutoFixName(r) || x.CheckID != r.ID {
		t.Errorf("Fix = %s/%s/%s", x.Plan.Kind, x.Name, x.CheckID)
	}
	if len(x.Plan.Verify) == 0 || x.Plan.Title.Vi == "" || x.Plan.Title.En == "" || x.Plan.Target == "" {
		t.Errorf("Plan thiếu Verify, Title hoặc Target: %+v", x.Plan)
	}
	if _, err := x.Preview(ctx); err != nil {
		t.Fatalf("Preview: %v", err)
	}
	rep, err := x.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s (%s); bước %+v; kiểm %s", rep.Job.Status, rep.Job.Error, rep.Steps, failedChecks(rep.Checks))
	}
	return x
}

// recheck chạy lại các phép kiểm của site hoặc stack và trả kết quả có ID id.
func (e *fixEnv) recheck(t *testing.T, dom, id string) checks.Result {
	t.Helper()
	var cs []checks.Check
	if dom == "" {
		var err error
		cs, _, err = (&Stack{Settings: e.st}).Checks(ctx)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		var err error
		cs, err = e.s.Checks(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range checks.RunAll(ctx, e.m, cs) {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("không có phép kiểm %s", id)
	return checks.Result{}
}

func stepIDsOf(x *Fix) []string { return stepIDs(x.Plan) }

const fixDom = "shop.example.com"

func TestFixWPConfigPerm(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	if err := e.m.Chmod(l.WPConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	r := e.recheck(t, fixDom, "wp.config:"+fixDom)
	if r.Status != checks.Fail || AutoFixName(r) != FixWPConfigPerm {
		t.Fatalf("trước khi sửa: %s %q", r.Status, AutoFixName(r))
	}
	x := e.fix(t, r)
	if got := stepIDsOf(x); !slices.Equal(got, []string{"wp.config.perm:" + fixDom}) {
		t.Errorf("bước %v", got)
	}
	if r := e.recheck(t, fixDom, "wp.config:"+fixDom); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}
}

func TestFixDBUnlock(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	e.locked[l.DB] = true
	r := e.recheck(t, fixDom, "db.access:"+fixDom)
	if r.Status != checks.Fail {
		t.Fatalf("user khoá mà DBAccess = %s", r.Status)
	}
	e.fix(t, r)
	if e.locked[l.DB] {
		t.Error("user DB vẫn khoá")
	}
	if r := e.recheck(t, fixDom, "db.access:"+fixDom); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s", r.Status)
	}
}

func TestFixPHPRestart(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	if err := e.m.Remove(l.Socket); err != nil {
		t.Fatal(err)
	}
	r := e.recheck(t, fixDom, "php.socket:"+fixDom)
	if r.Status != checks.Fail {
		t.Fatalf("socket mất mà PoolSocket = %s", r.Status)
	}
	x := e.fix(t, r)
	if got := stepIDsOf(x); !slices.Equal(got, []string{"php.restart:8.3"}) {
		t.Errorf("bước %v", got)
	}
	if r := e.recheck(t, fixDom, "php.socket:"+fixDom); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s", r.Status)
	}
}

func TestFixServiceStart(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	e.units["php8.3-fpm"] = unitState{}
	r := e.recheck(t, "", "service.active:php8.3-fpm")
	if r.Status != checks.Fail {
		t.Fatalf("php dừng mà Service = %s", r.Status)
	}
	x := e.fix(t, r)
	if got := stepIDsOf(x); !slices.Equal(got, []string{"service:php8.3-fpm"}) {
		t.Errorf("bước %v", got)
	}
	if r := e.recheck(t, "", "service.active:php8.3-fpm"); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s", r.Status)
	}
}

func TestFixFileRewrite(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newFixEnv(t, web, "blog.example.com", fixDom)
			l := site.LayoutOf(mustDomain(t, fixDom))
			conf := l.NginxConf
			if web == Apache {
				conf = l.ApacheConf
			}
			orig, err := e.m.ReadFile(conf)
			if err != nil {
				t.Fatal(err)
			}
			e.m.SetFile(conf, append(append([]byte{}, orig...), "# sửa tay\n"...), 0o644, system.Root)

			x := e.fix(t, checks.Result{ID: "drift:" + conf, Status: checks.Warn})
			if got := stepIDsOf(x); !slices.Equal(got, []string{"file:" + conf}) {
				t.Errorf("bước %v", got)
			}
			got, _ := e.m.ReadFile(conf)
			if string(got) != string(orig) {
				t.Errorf("file sau khi sửa khác bản của tpan:\n%s", got)
			}
			// sha mà Verify (Drift) so phải là sha của nội dung đã ghi, và engine
			// ghi đúng sha đó vào managed_files.
			sum := sha256.Sum256(orig)
			want := hex.EncodeToString(sum[:])
			if f := x.Plan.Steps[0].(*steps.File); hex.EncodeToString(sha256Of(f.Content)) != want {
				t.Error("Step File không mang nội dung của tpan")
			}
			mf, err := e.st.ManagedFiles(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range mf {
				if m.Path == conf {
					found = m.SHA256 == want
				}
			}
			if !found {
				t.Errorf("managed_files không có sha mới của %s", conf)
			}
		})
	}

	t.Run("file của stack và của site đều dựng lại được", func(t *testing.T) {
		e := newFixEnv(t, Apache, fixDom)
		l := site.LayoutOf(mustDomain(t, fixDom))
		for _, p := range []string{
			logrotateCnf, mariadbCnf, "/etc/apache2/sites-available/000-tpan-default.conf",
			l.PoolConf("8.3"), l.ApacheConf, l.Public + "/.htaccess",
		} {
			x, err := e.f.Prepare(ctx, checks.Result{ID: "drift:" + p, Status: checks.Warn})
			if err != nil {
				t.Errorf("%s: %v", p, err)
				continue
			}
			if got := stepIDsOf(x); !slices.Equal(got, []string{"file:" + p}) || x.Plan.Target != p {
				t.Errorf("%s: bước %v, target %s", p, got, x.Plan.Target)
			}
		}
	})
}

func sha256Of(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

func TestFixWebRewrite(t *testing.T) {
	e := newFixEnv(t, Nginx, "blog.example.com", fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	orig, _ := e.m.ReadFile(l.NginxConf)
	e.m.SetFile(l.NginxConf, []byte("server { hỏng\n"), 0o644, system.Root)

	x := e.fix(t, checks.Result{ID: "config.nginx:nginx", Status: checks.Fail})
	blog := site.LayoutOf(mustDomain(t, "blog.example.com"))
	want := []string{
		"file:/etc/nginx/sites-available/tpan-default.conf",
		"symlink:/etc/nginx/sites-enabled/tpan-default.conf",
		"file:" + blog.NginxConf, "symlink:" + blog.NginxLink,
		"file:" + l.NginxConf, "symlink:" + l.NginxLink,
	}
	if got := stepIDsOf(x); !slices.Equal(got, want) {
		t.Errorf("bước:\n got %v\nwant %v", got, want)
	}
	if got, _ := e.m.ReadFile(l.NginxConf); string(got) != string(orig) {
		t.Error("vhost hỏng chưa được ghi lại")
	}
	if x.Plan.Target != "nginx" {
		t.Errorf("target %s", x.Plan.Target)
	}
}

// fileRecords là managed_files trong bộ nhớ: path → sha256 hex.
type fileRecords map[string]string

func (r fileRecords) FileSHA256(_ context.Context, p string) (string, bool, error) {
	s, ok := r[p]
	return s, ok, nil
}

// File người dùng đã nhận bản sửa tay làm chuẩn (sha256 trong managed_files
// khớp nội dung đang có) thì web.rewrite không ghi đè, chỉ ghi file hỏng. Lỗi
// tìm thấy ở e2e M4: web.rewrite ghi lại vhost đã nhận của site khác, chạy
// `nginx -t` lúc vhost hỏng chưa được ghi, cả job hỏng và hoàn tác.
func TestFixWebRewriteKeepsAccepted(t *testing.T) {
	e := newFixEnv(t, Nginx, "blog.example.com", fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	blog := site.LayoutOf(mustDomain(t, "blog.example.com"))
	orig, _ := e.m.ReadFile(blog.NginxConf)
	accepted := append(slices.Clone(orig), []byte("# sửa tay, đã nhận\n")...)
	e.m.SetFile(blog.NginxConf, accepted, 0o644, system.Root)
	e.m.SetFile(l.NginxConf, []byte("server { hỏng\n"), 0o644, system.Root)
	e.f.Files = fileRecords{
		blog.NginxConf: hex.EncodeToString(sha256Of(accepted)),
		l.NginxConf:    hex.EncodeToString(sha256Of(orig)), // bản tpan ghi; file đang có khác: lệch
	}

	x := e.fix(t, checks.Result{ID: "config.nginx:nginx", Status: checks.Fail})
	for _, id := range stepIDsOf(x) {
		if id == "file:"+blog.NginxConf {
			t.Errorf("web.rewrite vẫn ghi file đã nhận làm chuẩn: %v", stepIDsOf(x))
		}
	}
	if got, _ := e.m.ReadFile(blog.NginxConf); string(got) != string(accepted) {
		t.Error("bản sửa tay đã nhận bị ghi đè")
	}
	if got, _ := e.m.ReadFile(l.NginxConf); string(got) == "server { hỏng\n" {
		t.Error("vhost hỏng chưa được ghi lại")
	}

	// Đối chứng: không có Files thì giữ cách cũ (ghi mọi file khác template).
	e.f.Files = nil
	e.m.SetFile(l.NginxConf, []byte("server { hỏng\n"), 0o644, system.Root)
	x = e.fix(t, checks.Result{ID: "config.nginx:nginx", Status: checks.Fail})
	if !slices.Contains(stepIDsOf(x), "file:"+blog.NginxConf) {
		t.Errorf("Files nil mà vẫn bỏ bước: %v", stepIDsOf(x))
	}
}

// web.vhost: symlink ở sites-enabled bị xoá (vhost chưa bật) thì sửa tự động
// dựng lại symlink của riêng site đó, và chính phép kiểm web.vhost đạt sau khi
// sửa. Vhost hỏng của site KHÁC không được đụng tới (không ghi lại vhost của
// mọi site như web.rewrite cho config.nginx).
func TestFixWebVhost(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newFixEnv(t, web, "blog.example.com", fixDom)
			l := site.LayoutOf(mustDomain(t, fixDom))
			blog := site.LayoutOf(mustDomain(t, "blog.example.com"))
			conf, link, blogConf := l.NginxConf, l.NginxLink, blog.NginxConf
			if web == Apache {
				conf, link, blogConf = l.ApacheConf, l.ApacheLink, blog.ApacheConf
			}
			id := "web.vhost:" + fixDom
			if err := e.m.Remove(link); err != nil {
				t.Fatal(err)
			}
			r := e.recheck(t, fixDom, id)
			if r.Status != checks.Fail {
				t.Fatalf("xoá symlink mà %s = %s", id, r.Status)
			}
			if got := AutoFixName(r); got != FixWebRewrite {
				t.Fatalf("AutoFixName = %q", got)
			}
			blogOrig, _ := e.m.ReadFile(blogConf)
			e.m.SetFile(blogConf, []byte("# vhost site khác, sửa tay\n"), 0o644, system.Root)

			x := e.fix(t, r)
			if want := []string{"file:" + conf, "symlink:" + link}; !slices.Equal(stepIDsOf(x), want) {
				t.Errorf("bước:\n got %v\nwant %v", stepIDsOf(x), want)
			}
			if x.Plan.Target != fixDom {
				t.Errorf("target %s", x.Plan.Target)
			}
			var verify []string
			for _, vr := range checks.RunAll(ctx, e.m, x.Plan.Verify) {
				verify = append(verify, vr.ID)
			}
			if !slices.Contains(verify, id) {
				t.Errorf("Verify thiếu %s: %v", id, verify)
			}
			if got := e.recheck(t, fixDom, id); got.Status != checks.Pass {
				t.Errorf("sau khi sửa %s = %s (%s)", id, got.Status, got.Summary.Vi)
			}
			if target, err := e.m.Readlink(link); err != nil || target != conf {
				t.Errorf("symlink %s → %q (%v); muốn %s", link, target, err, conf)
			}
			if got, _ := e.m.ReadFile(blogConf); string(got) == string(blogOrig) {
				t.Error("sửa web.vhost của một site mà ghi lại vhost của site khác")
			}
		})
	}

	t.Run("từ chối", func(t *testing.T) {
		e := newFixEnv(t, Nginx, fixDom)
		trashed := mustDomain(t, "old.example.com")
		e.sites.sites[trashed.String()] = site.Site{Domain: trashed, Status: site.Trashed, PHPVersion: "8.3"}
		for _, tc := range []struct{ obj, code string }{
			{"none.example.com", "site_not_found"},
			{trashed.String(), "no_auto_fix"},
			{"SHOP.example.com", "no_auto_fix"},
		} {
			_, err := e.f.Prepare(ctx, checks.Result{ID: "web.vhost:" + tc.obj, Status: checks.Fail})
			if got := refusedCode(t, err); got != tc.code {
				t.Errorf("web.vhost:%s: code %s, muốn %s", tc.obj, got, tc.code)
			}
		}
		if _, err := e.f.Prepare(ctx, checks.Result{ID: "web.vhost:a.com; rm -rf /", Status: checks.Fail}); err == nil {
			t.Error("domain tấn công: muốn lỗi")
		}
	})
}

func TestFixRefusals(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	trashed := mustDomain(t, "old.example.com")
	e.sites.sites[trashed.String()] = site.Site{Domain: trashed, Status: site.Trashed, PHPVersion: "8.3"}

	for _, tc := range []struct {
		name string
		r    checks.Result
		code string
	}{
		{"đạt", checks.Result{ID: "wp.config:" + fixDom, Status: checks.Pass}, "no_auto_fix"},
		{"loại không có cách sửa", checks.Result{ID: "disk.usage:/", Status: checks.Fail}, "no_auto_fix"},
		{"AutoFix không khớp ID", checks.Result{ID: "wp.config:" + fixDom, Status: checks.Fail, AutoFix: FixDBUnlock}, "no_auto_fix"},
		{"unit lạ", checks.Result{ID: "service.active:ssh", Status: checks.Fail}, "no_auto_fix"},
		{"unit có ký tự lạ", checks.Result{ID: "service.active:nginx; rm -rf /", Status: checks.Fail}, "no_auto_fix"},
		{"unit của web server khác", checks.Result{ID: "service.active:apache2", Status: checks.Fail}, "no_auto_fix"},
		{"thiếu đối tượng", checks.Result{ID: "service.active", Status: checks.Fail}, "no_auto_fix"},
		{"file tpan không dựng lại được", checks.Result{ID: "drift:/etc/systemd/system/tpan.service", Status: checks.Warn}, "no_auto_fix"},
		{"đường dẫn vòng", checks.Result{ID: "drift:/etc/nginx/sites-available/../../passwd", Status: checks.Warn}, "no_auto_fix"},
		{"config của web server khác", checks.Result{ID: "config.apache:apache2", Status: checks.Fail}, "no_auto_fix"},
		{"site không có", checks.Result{ID: "php.socket:none.example.com", Status: checks.Fail}, "site_not_found"},
		{"site trong thùng rác", checks.Result{ID: "wp.config:" + trashed.String(), Status: checks.Fail}, "no_auto_fix"},
		{"domain chữ hoa", checks.Result{ID: "wp.config:SHOP.example.com", Status: checks.Fail}, "no_auto_fix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.m.ResetRan()
			_, err := e.f.Prepare(ctx, tc.r)
			if got := refusedCode(t, err); got != tc.code {
				t.Errorf("code %s, muốn %s", got, tc.code)
			}
			for _, c := range e.m.Ran() {
				t.Errorf("Prepare chạy lệnh %s", c.String())
			}
		})
	}

	for _, bad := range []string{"a.com; rm -rf /", "$(id)", "../../etc/passwd", "a.com\nb.com", strings.Repeat("a", 300) + ".com", "bücher.de"} {
		if _, err := e.f.Prepare(ctx, checks.Result{ID: "db.access:" + bad, Status: checks.Fail}); err == nil {
			t.Errorf("domain %q: muốn lỗi", bad)
		}
	}

	t.Run("stack chưa cài", func(t *testing.T) {
		se := newSiteEnv(t, "")
		f := &Fixes{Sys: se.m, Settings: se.st, Store: se.sites}
		_, err := f.Prepare(ctx, checks.Result{ID: "service.active:nginx", Status: checks.Fail})
		if refusedCode(t, err) != "stack_not_installed" {
			t.Error("muốn stack_not_installed")
		}
	})

	t.Run("Execute thiếu Journal", func(t *testing.T) {
		f := *e.f
		f.Journal = nil
		x, err := f.Prepare(ctx, checks.Result{ID: "wp.config:" + fixDom, Status: checks.Fail})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := x.Execute(ctx, nil); err == nil || errors.As(err, new(*RefusedError)) {
			t.Errorf("err = %v", err)
		}
	})
}

// Site có SSL (M5): sửa tự động và phát hiện lệch phải sinh vhost có cổng 443,
// không thì "ghi lại từ template" xoá mất HTTPS của site.
func TestSiteFilesSSLVhost(t *testing.T) {
	d, err := validate.ParseDomain("shop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	cfg := StackConfig{Web: Nginx, PHPVersion: "8.3"}
	for _, ssl := range []bool{false, true} {
		fs, err := siteFiles(cfg, site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", SSL: ssl})
		if err != nil {
			t.Fatal(err)
		}
		var vhost string
		for _, f := range fs {
			if f.Path == site.LayoutOf(d).NginxConf {
				vhost = string(f.Content)
			}
		}
		if vhost == "" {
			t.Fatalf("ssl=%v: không có file vhost trong %d file", ssl, len(fs))
		}
		if has := strings.Contains(vhost, "listen 443"); has != ssl {
			t.Errorf("ssl=%v: vhost có cổng 443 = %v", ssl, has)
		}
	}
}
