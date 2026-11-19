package core

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- giả lập

// siteSim là FakeSystem cộng các handler giả lập phần máy mà một site đụng
// tới: user Linux (getent, useradd, userdel), MariaDB (trạng thái, CREATE,
// DROP), curl (tải WordPress, gọi HTTP vào site), tar, chmod dưới user, WP-CLI
// và socket của pool khi PHP-FPM reload. Không dùng newSim của stack: handler
// mariadb và curl của nó trả lời sai cho các câu của site.
type siteSim struct {
	*system.FakeSystem

	mu        sync.Mutex
	users     map[string]string // tên → home
	dbs       map[string]bool
	dbUsers   map[string]bool
	installed map[string]bool   // WordPress đã cài, theo user của site
	permalink map[string]string // theo user của site
	sockets   map[string]bool
	// failTar khác 0 thì tar thoát mã này.
	failTar int
}

// fakeWP là nội dung curl giả "tải" về cho WordPress; test dùng SHA-1 của nó.
var fakeWP = []byte("wordpress tarball giả\n")

func fakeWPSHA1() string {
	sum := sha1.Sum(fakeWP)
	return hex.EncodeToString(sum[:])
}

var (
	sqlStateRE   = regexp.MustCompile(`SCHEMA_NAME='([a-z0-9_]+)'`)
	sqlCreateDB  = regexp.MustCompile("CREATE DATABASE `([a-z0-9_]+)`")
	sqlCreateUsr = regexp.MustCompile(`CREATE USER '([a-z0-9_]+)'`)
	sqlDropDB    = regexp.MustCompile("DROP DATABASE IF EXISTS `([a-z0-9_]+)`")
	sqlDropUsr   = regexp.MustCompile(`DROP USER IF EXISTS '([a-z0-9_]+)'`)
)

func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func argPrefix(args []string, prefix string) string {
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, prefix); ok {
			return v
		}
	}
	return ""
}

func newSiteSim(t *testing.T) *siteSim {
	t.Helper()
	m := &siteSim{
		FakeSystem: system.NewFake(),
		users:      map[string]string{},
		dbs:        map[string]bool{},
		dbUsers:    map[string]bool{},
		installed:  map[string]bool{},
		permalink:  map[string]string{},
		sockets:    map[string]bool{},
	}
	// Những gì stack đã cài để lại.
	for _, d := range []string{
		"/etc/nginx/sites-available", "/etc/nginx/sites-enabled",
		"/etc/apache2/sites-available", "/etc/apache2/sites-enabled",
		"/etc/php/8.3/fpm/pool.d", "/var/cache/tpan", "/var/www", "/var/log/tpan/sites", "/run/php",
	} {
		m.SetDir(d, 0o755, system.Root)
	}

	ok := system.Result{}
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}

	m.Handle([]string{"getent", "passwd"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		name := c.Args[1]
		home, found := m.users[name]
		if !found {
			return exit(2, "", "")
		}
		return exit(0, name+":x:998:998::"+home+":/usr/sbin/nologin\n", "")
	})
	m.Handle([]string{"useradd"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.users[c.Args[len(c.Args)-1]] = argAfter(c.Args, "--home-dir")
		return ok
	})
	m.Handle([]string{"userdel"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		name := c.Args[len(c.Args)-1]
		if _, found := m.users[name]; !found {
			return exit(6, "", "userdel: user '"+name+"' does not exist\n")
		}
		delete(m.users, name)
		return ok
	})

	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		sql := string(c.Stdin)
		if g := sqlStateRE.FindStringSubmatch(sql); g != nil {
			b := func(v bool) int {
				if v {
					return 1
				}
				return 0
			}
			return exit(0, fmt.Sprintf("%d\t%d\n", b(m.dbs[g[1]]), b(m.dbUsers[g[1]])), "")
		}
		for _, g := range sqlCreateDB.FindAllStringSubmatch(sql, -1) {
			m.dbs[g[1]] = true
		}
		for _, g := range sqlCreateUsr.FindAllStringSubmatch(sql, -1) {
			m.dbUsers[g[1]] = true
		}
		for _, g := range sqlDropUsr.FindAllStringSubmatch(sql, -1) {
			delete(m.dbUsers, g[1])
		}
		for _, g := range sqlDropDB.FindAllStringSubmatch(sql, -1) {
			delete(m.dbs, g[1])
		}
		return ok
	})

	m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		if r := argAfter(c.Args, "--resolve"); r != "" { // checks.HTTPSite
			dom, _, _ := strings.Cut(r, ":")
			body := "<html><link rel='stylesheet' href='http://" + dom + "/wp-content/themes/x/style.css'></html>"
			return exit(0, body+"TPAN_HTTP_CODE=200", "")
		}
		if p := argAfter(c.Args, "-o"); p != "" { // WPArchive
			m.SetFile(p, fakeWP, 0o600, system.Root)
		}
		return ok
	})

	m.Handle([]string{"tar"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		code := m.failTar
		m.mu.Unlock()
		if code != 0 {
			return exit(code, "", "tar: giả lập lỗi\n")
		}
		pub := argAfter(c.Args, "-C")
		own := system.Owner{User: c.AsUser, Group: c.AsUser}
		m.SetFile(pub+"/index.php", []byte("<?php\n"), 0o644, own)
		m.SetFile(pub+"/wp-includes/version.php", []byte("<?php $wp_version = '7.1.5';\n"), 0o644, own)
		return ok
	})

	m.Handle([]string{"chmod"}, func(c system.Cmd) system.Result {
		mode, err := strconv.ParseUint(c.Args[0], 8, 32)
		if err != nil {
			return exit(1, "", "chmod: invalid mode\n")
		}
		if err := m.Chmod(c.Args[len(c.Args)-1], fs.FileMode(mode)); err != nil {
			return exit(1, "", err.Error()+"\n")
		}
		return ok
	})

	m.Handle([]string{"wp"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		u, pub := c.AsUser, argPrefix(c.Args, "--path=")
		switch {
		case len(c.Args) >= 2 && c.Args[0] == "config" && c.Args[1] == "create":
			m.SetFile(pub+"/wp-config.php", []byte("<?php // wp-config giả\n"), 0o644, system.Owner{User: u, Group: u})
		case len(c.Args) >= 2 && c.Args[0] == "core" && c.Args[1] == "is-installed":
			if !m.installed[u] {
				return exit(1, "", "")
			}
		case len(c.Args) >= 2 && c.Args[0] == "core" && c.Args[1] == "install":
			m.installed[u] = true
		case len(c.Args) >= 3 && c.Args[0] == "option" && c.Args[2] == "permalink_structure":
			return exit(0, m.permalink[u]+"\n", "")
		case len(c.Args) >= 3 && c.Args[0] == "rewrite" && c.Args[1] == "structure":
			m.permalink[u] = c.Args[2]
		case len(c.Args) >= 2 && c.Args[0] == "db" && c.Args[1] == "query":
			// DB của site cùng tên với user của site (tp_<slug>).
			return exit(0, "information_schema\n"+u+"\n", "")
		}
		return ok
	})

	// `find <public> -mindepth 1 -delete` (hoàn tác WPFiles) và
	// `rm -r --one-file-system -- <dir>` (RemoveTree). Các find khác (chmod) không đổi gì.
	m.Handle([]string{"find"}, func(c system.Cmd) system.Result {
		if slices.Contains(c.Args, "-delete") {
			m.removeUnder(c.Args[0], false)
		}
		return ok
	})
	m.Handle([]string{"rm", "-r"}, func(c system.Cmd) system.Result {
		m.removeUnder(c.Args[len(c.Args)-1], true)
		return ok
	})

	// Isolated: dưới user của site này, đọc wp-config.php của site kia bị từ chối.
	m.Handle([]string{"test", "-r"}, func(system.Cmd) system.Result { return exit(1, "", "") })

	// PHP-FPM reload: mỗi file pool trong pool.d có một socket, pool đã gỡ thì mất socket.
	m.Handle([]string{"systemctl", "reload", "php8.3-fpm"}, func(system.Cmd) system.Result {
		names, _ := m.ReadDir("/etc/php/8.3/fpm/pool.d")
		want := map[string]bool{}
		for _, n := range names {
			if pool, ok := strings.CutSuffix(n, ".conf"); ok {
				want["/run/php/"+pool+".sock"] = true
			}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for s := range m.sockets {
			if !want[s] {
				_ = m.Remove(s)
				delete(m.sockets, s)
			}
		}
		for s := range want {
			m.SetSocket(s, 0o660, system.Owner{User: "www-data", Group: "www-data"})
			m.sockets[s] = true
		}
		return ok
	})
	return m
}

// removeUnder xoá mọi thứ bên dưới p (sâu trước), và cả p nếu self.
func (m *siteSim) removeUnder(p string, self bool) {
	var paths []string
	for k := range m.Snapshot() {
		if strings.HasPrefix(k, p+"/") || (self && k == p) {
			paths = append(paths, k)
		}
	}
	slices.SortFunc(paths, func(a, b string) int { return len(b) - len(a) })
	for _, k := range paths {
		_ = m.Remove(k)
	}
}

// memSites là SiteStore trong bộ nhớ.
type memSites struct {
	mu       sync.Mutex
	sites    map[string]site.Site
	failSave error
}

func newMemSites() *memSites { return &memSites{sites: map[string]site.Site{}} }

func (m *memSites) Site(_ context.Context, d validate.Domain) (site.Site, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sites[d.String()]
	return s, ok, nil
}

func (m *memSites) Sites(context.Context) ([]site.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []site.Site{}
	for _, s := range m.sites {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b site.Site) int { return strings.Compare(a.Domain.String(), b.Domain.String()) })
	return out, nil
}

func (m *memSites) SaveSite(_ context.Context, s site.Site) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSave != nil {
		return m.failSave
	}
	s.Slug = validate.SlugOf(s.Domain)
	m.sites[s.Domain.String()] = s
	return nil
}

func (m *memSites) DeleteSite(_ context.Context, d validate.Domain) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sites[d.String()]; !ok {
		return fmt.Errorf("site %s: không có", d)
	}
	delete(m.sites, d.String())
	return nil
}

func (m *memSites) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sites)
}

type siteEnv struct {
	s     *Sites
	m     *siteSim
	sites *memSites
	st    *store.Store
}

// newSiteEnv dựng Sites trên siteSim, store thật (settings và nhật ký job) và
// SiteStore trong bộ nhớ. web rỗng: stack chưa cài.
func newSiteEnv(t *testing.T, web WebServer) *siteEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatalf("mở store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if web != "" {
		mustSet(t, st, SettingWeb, string(web))
		mustSet(t, st, SettingPHPVersion, "8.3")
	}
	m := newSiteSim(t)
	ms := newMemSites()
	return &siteEnv{
		s:     &Sites{Sys: m, Settings: st, Store: ms, Journal: st, WPVersion: "7.1.5", WPHash: fakeWPSHA1()},
		m:     m,
		sites: ms,
		st:    st,
	}
}

func mustDomain(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatalf("ParseDomain(%q): %v", s, err)
	}
	return d
}

func mustEmail(t *testing.T) validate.Email {
	t.Helper()
	e, err := validate.ParseEmail("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (se *siteEnv) input(t *testing.T, dom string) SiteInput {
	return SiteInput{Domain: mustDomain(t, dom), Email: mustEmail(t)}
}

// add tạo site và đòi job thành công.
func (se *siteEnv) add(t *testing.T, dom string) SiteAddResult {
	t.Helper()
	a, err := se.s.PrepareAdd(context.Background(), se.input(t, dom))
	if err != nil {
		t.Fatalf("PrepareAdd %s: %v", dom, err)
	}
	res, err := a.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute %s: %v", dom, err)
	}
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s (%s); bước %+v; kiểm %s", dom, res.Report.Job.Status, res.Report.Job.Error, res.Report.Steps, failedChecks(res.Report.Checks))
	}
	return res
}

func failedChecks(rs []checks.Result) string {
	var b strings.Builder
	for _, r := range rs {
		if r.Status != checks.Pass {
			fmt.Fprintf(&b, "\n  %s %s: %s", r.Status, r.ID, r.Summary.Vi)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------- logic thuần

func TestSitePassword(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9]{32}$`)
	seen := map[string]bool{}
	for range 50 {
		p, err := sitePassword()
		if err != nil {
			t.Fatal(err)
		}
		v := p.Reveal()
		if !re.MatchString(v) {
			t.Fatalf("mật khẩu %q không phải 32 ký tự [A-Za-z0-9]", v)
		}
		if seen[v] {
			t.Fatalf("hai lần sinh ra cùng mật khẩu %q", v)
		}
		seen[v] = true
		if p.String() == v {
			t.Fatal("Secret in ra mật khẩu thật")
		}
	}
}

func TestSiteTitle(t *testing.T) {
	for _, tc := range []struct {
		title string
		ok    bool
	}{
		{"Cửa hàng của tôi", true},
		{strings.Repeat("a", 200), true},
		{strings.Repeat("a", 201), false},
		{strings.Repeat("ữ", 67), false}, // 201 byte
		{"a\nb", false},
		{"a\tb", false},
		{"a\x7fb", false},
		{"a\u0085b", false},
		{"a\xffb", false},
		{"$(id); rm -rf /", true}, // chỉ là chữ: đi vào argv và DB của WordPress, không qua shell
	} {
		err := checkSiteTitle(tc.title)
		if (err == nil) != tc.ok {
			t.Errorf("checkSiteTitle(%q) = %v; muốn ok=%v", tc.title, err, tc.ok)
		}
		if err != nil && refusedCode(t, err) != "title_invalid" {
			t.Errorf("%q: mã sai", tc.title)
		}
	}
}

// ---------------------------------------------------------------- PrepareAdd

func TestSiteAddPlanOrder(t *testing.T) {
	const dom = "shop.example.com"
	for _, tc := range []struct {
		web  WebServer
		tail []string
	}{
		{Nginx, []string{
			"file:/etc/nginx/sites-available/tpan-shop.example.com.conf",
			"symlink:/etc/nginx/sites-enabled/tpan-shop.example.com.conf",
			"wp.install:shop.example.com",
		}},
		{Apache, []string{
			"file:/var/www/shop.example.com/public/.htaccess",
			"file:/etc/apache2/sites-available/tpan-shop.example.com.conf",
			"symlink:/etc/apache2/sites-enabled/tpan-shop.example.com.conf",
			"wp.install:shop.example.com",
		}},
	} {
		t.Run(string(tc.web), func(t *testing.T) {
			se := newSiteEnv(t, tc.web)
			a, err := se.s.PrepareAdd(ctx, se.input(t, dom))
			if err != nil {
				t.Fatal(err)
			}
			l := site.LayoutOf(mustDomain(t, dom))
			want := append([]string{
				"site.user:" + l.User,
				"dir:/var/www/shop.example.com",
				"dir:/var/www/shop.example.com/public",
				"dir:/var/www/shop.example.com/tmp",
				"dir:/var/log/tpan/sites/shop.example.com",
				"owned-file:/var/log/tpan/sites/shop.example.com/php-error.log",
				"mariadb.site-db:shop.example.com",
				"file:/etc/php/8.3/fpm/pool.d/" + l.Pool + ".conf",
				"wp.archive:7.1.5",
				"wp.files:shop.example.com",
				"wp.config:shop.example.com",
			}, tc.tail...)
			if got := stepIDs(a.Plan); !reflect.DeepEqual(got, want) {
				t.Errorf("thứ tự bước:\n got %v\nwant %v", got, want)
			}
			if a.Plan.Kind != "site.add" || a.Plan.Target != dom {
				t.Errorf("Plan = %s/%s", a.Plan.Kind, a.Plan.Target)
			}
			if a.Rerun || a.AdminUser != "admin" {
				t.Errorf("Rerun=%v AdminUser=%q", a.Rerun, a.AdminUser)
			}
			// Tiêu đề rỗng thì dùng domain.
			for _, s := range a.Plan.Steps {
				if w, ok := s.(*wpInstallStep); ok {
					if w.Title != dom || w.URL != "http://"+dom {
						t.Errorf("WPInstall Title=%q URL=%q", w.Title, w.URL)
					}
				}
			}
		})
	}
}

func TestSiteAddRefusals(t *testing.T) {
	const dom = "shop.example.com"

	expect := func(t *testing.T, se *siteEnv, in SiteInput, code string) (msgVi string) {
		t.Helper()
		_, err := se.s.PrepareAdd(ctx, in)
		if got := refusedCode(t, err); got != code {
			t.Fatalf("mã = %s; muốn %s (%v)", got, code, err)
		}
		var re *RefusedError
		errors.As(err, &re)
		for _, c := range se.m.Ran() {
			if !siteReadOnly(c) {
				t.Errorf("từ chối mà chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if se.sites.count() > 0 && code != "site_trashed" && code != "site_drift" {
			t.Error("từ chối mà ghi Store")
		}
		return re.Message.Vi
	}

	t.Run("stack chưa cài", func(t *testing.T) {
		se := newSiteEnv(t, "")
		expect(t, se, se.input(t, dom), "stack_not_installed")
		if len(se.m.Ran()) != 0 {
			t.Errorf("chạy lệnh: %v", se.m.Ran())
		}
	})

	t.Run("tiêu đề sai", func(t *testing.T) {
		se := newSiteEnv(t, Nginx)
		in := se.input(t, dom)
		in.Title = "a\nb"
		expect(t, se, in, "title_invalid")
		if len(se.m.Ran()) != 0 {
			t.Errorf("chạy lệnh: %v", se.m.Ran())
		}
	})

	t.Run("domain hay email rỗng", func(t *testing.T) {
		se := newSiteEnv(t, Nginx)
		expect(t, se, SiteInput{Email: mustEmail(t)}, "domain_invalid")
		expect(t, se, SiteInput{Domain: mustDomain(t, dom)}, "email_invalid")
	})

	t.Run("đang ở thùng rác", func(t *testing.T) {
		se := newSiteEnv(t, Nginx)
		d := mustDomain(t, dom)
		se.sites.sites[dom] = site.Site{Domain: d, Status: site.Trashed, PHPVersion: "8.3"}
		expect(t, se, se.input(t, dom), "site_trashed")
		if len(se.m.Ran()) != 0 {
			t.Errorf("chạy lệnh: %v", se.m.Ran())
		}
	})

	l := site.LayoutOf(mustDomain(t, dom))
	for _, tc := range []struct {
		name  string
		setup func(se *siteEnv)
		says  string
	}{
		{"thư mục site có sẵn", func(se *siteEnv) { se.m.SetDir(l.Dir, 0o755, system.Root) }, l.Dir},
		{"user Linux có sẵn", func(se *siteEnv) { se.m.users[l.User] = "/home/x" }, l.User},
		{"DB có sẵn", func(se *siteEnv) { se.m.dbs[l.DB] = true }, "DB " + l.DB},
		{"user DB có sẵn", func(se *siteEnv) { se.m.dbUsers[l.DB] = true }, "user DB " + l.DB},
	} {
		t.Run("không do tpan quản lý: "+tc.name, func(t *testing.T) {
			se := newSiteEnv(t, Nginx)
			tc.setup(se)
			msg := expect(t, se, se.input(t, dom), "site_exists_unmanaged")
			if !strings.Contains(msg, tc.says) {
				t.Errorf("lời từ chối không nói %q: %s", tc.says, msg)
			}
		})
	}

	t.Run("không xét vhost và pool", func(t *testing.T) {
		// e2e đặt một thư mục ở chỗ file vhost để gây lỗi giữa chừng.
		se := newSiteEnv(t, Nginx)
		se.m.SetDir(l.NginxConf, 0o755, system.Root)
		se.m.SetFile(l.PoolConf("8.3"), []byte("; cũ\n"), 0o644, system.Root)
		if _, err := se.s.PrepareAdd(ctx, se.input(t, dom)); err != nil {
			t.Fatalf("vhost hay pool có sẵn không được làm từ chối: %v", err)
		}
	})

	for _, tc := range []struct {
		name  string
		setup func(se *siteEnv)
		says  string
	}{
		{"user DB có, wp-config.php không", func(se *siteEnv) { se.m.dbUsers[l.DB] = true }, "wp-config.php"},
		{"wp-config.php có, user DB không", func(se *siteEnv) {
			se.m.SetFile(l.WPConfig, []byte("<?php\n"), 0o600, system.Owner{User: l.User, Group: l.User})
		}, "user DB"},
	} {
		t.Run("lệch: "+tc.name, func(t *testing.T) {
			se := newSiteEnv(t, Nginx)
			se.sites.sites[dom] = site.Site{Domain: mustDomain(t, dom), Status: site.Active, PHPVersion: "8.3"}
			tc.setup(se)
			msg := expect(t, se, se.input(t, dom), "site_drift")
			if !strings.Contains(msg, tc.says) {
				t.Errorf("lời từ chối không nói lệch ở %q: %s", tc.says, msg)
			}
		})
	}
}

// siteReadOnly: lệnh chỉ hỏi trạng thái.
func siteReadOnly(c system.Cmd) bool {
	switch c.Name {
	case "getent":
		return true
	case "mariadb":
		return strings.HasPrefix(strings.TrimSpace(string(c.Stdin)), "SELECT")
	}
	return false
}

// ---------------------------------------------------------------- Execute

func TestSiteAddExecuteAndRerun(t *testing.T) {
	const dom = "shop.example.com"
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			se := newSiteEnv(t, web)
			d := mustDomain(t, dom)

			a, err := se.s.PrepareAdd(ctx, se.input(t, dom))
			if err != nil {
				t.Fatal(err)
			}
			before := se.m.Snapshot()
			pv, err := a.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, se.m.Snapshot()) {
				t.Error("xem trước đổi hệ thống file")
			}
			if se.sites.count() != 0 {
				t.Error("xem trước ghi Store")
			}
			for _, sp := range pv.Steps {
				if sp.Done {
					t.Errorf("máy sạch mà bước %s đã Done", sp.Info.ID)
				}
			}

			res, err := a.Execute(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Report.Job.Status != engine.JobSucceeded {
				t.Fatalf("job: %s %s; kiểm %s", res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
			}
			if !res.WPInstalled || res.AdminUser != "admin" || len(res.AdminPassword.Reveal()) != 32 {
				t.Errorf("kết quả: WPInstalled=%v AdminUser=%q độ dài mật khẩu=%d", res.WPInstalled, res.AdminUser, len(res.AdminPassword.Reveal()))
			}
			for _, r := range res.Report.Checks {
				if r.Status == checks.Fail {
					t.Errorf("kiểm chứng Fail: %s", r.ID)
				}
			}
			saved, ok, _ := se.sites.Site(ctx, d)
			if !ok || saved.Status != site.Active || saved.PHPVersion != "8.3" || saved.Created.IsZero() {
				t.Fatalf("Store sau khi tạo: ok=%v %+v", ok, saved)
			}

			// Chạy lại: mọi bước đã đúng.
			saved.Created = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			se.sites.sites[dom] = saved
			a2, err := se.s.PrepareAdd(ctx, se.input(t, dom))
			if err != nil {
				t.Fatalf("chạy lại bị từ chối: %v", err)
			}
			if !a2.Rerun {
				t.Error("Rerun = false")
			}
			pv2, err := a2.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, sp := range pv2.Steps {
				if !sp.Done {
					t.Errorf("chạy lại mà bước %s chưa Done (%s)", sp.Info.ID, sp.CheckError)
				}
			}
			res2, err := a2.Execute(ctx, nil)
			if err != nil || res2.Report.Job.Status != engine.JobSucceeded {
				t.Fatalf("chạy lại: %v %s %s", err, res2.Report.Job.Status, res2.Report.Job.Error)
			}
			if res2.WPInstalled || res2.AdminPassword.Reveal() != "" {
				t.Error("chạy lại mà báo đã cài WordPress kèm mật khẩu mới")
			}
			again, _, _ := se.sites.Site(ctx, d)
			if !again.Created.Equal(saved.Created) {
				t.Errorf("chạy lại đổi Created: %v → %v", saved.Created, again.Created)
			}
		})
	}
}

func TestSiteAddPermalinkOnlyIsNotInstall(t *testing.T) {
	// WordPress đã cài, chỉ permalink lệch: bước WPInstall có chạy (applied)
	// nhưng mật khẩu quản trị mới không được đặt, nên không được báo ra.
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, dom)
	l := site.LayoutOf(mustDomain(t, dom))
	se.m.permalink[l.User] = ""
	a, err := se.s.PrepareAdd(ctx, se.input(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Execute(ctx, nil)
	if err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s", err, res.Report.Job.Error)
	}
	applied := false
	for _, s := range res.Report.Steps {
		if s.ID == "wp.install:"+dom && s.Outcome == "applied" {
			applied = true
		}
	}
	if !applied {
		t.Fatal("bước wp.install không chạy để sửa permalink")
	}
	if res.WPInstalled || res.AdminPassword.Reveal() != "" {
		t.Error("chỉ sửa permalink mà báo đã cài WordPress kèm mật khẩu")
	}
}

func TestSiteAddFailureRollsBackAndSkipsStore(t *testing.T) {
	const dom = "shop.example.com"
	for _, tc := range []struct {
		name  string
		setup func(se *siteEnv, l site.Layout)
	}{
		// Giống e2e: một thư mục nằm ở chỗ file vhost.
		{"vhost bị chặn", func(se *siteEnv, l site.Layout) { se.m.SetDir(l.NginxConf, 0o755, system.Root) }},
		{"tar lỗi", func(se *siteEnv, _ site.Layout) { se.m.failTar = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			se := newSiteEnv(t, Nginx)
			l := site.LayoutOf(mustDomain(t, dom))
			tc.setup(se, l)
			a, err := se.s.PrepareAdd(ctx, se.input(t, dom))
			if err != nil {
				t.Fatal(err)
			}
			res, err := a.Execute(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Report.Job.Status != engine.JobRolledBack {
				t.Fatalf("job = %s; muốn rolled_back (%s); bước %+v", res.Report.Job.Status, res.Report.Job.Error, res.Report.Steps)
			}
			if se.sites.count() != 0 {
				t.Error("job hỏng mà vẫn ghi Store")
			}
			if res.WPInstalled || res.AdminPassword.Reveal() != "" {
				t.Error("job hỏng mà trả mật khẩu quản trị")
			}
			if len(se.m.users) != 0 || len(se.m.dbs) != 0 || len(se.m.dbUsers) != 0 {
				t.Errorf("hoàn tác chưa sạch: users=%v dbs=%v dbUsers=%v", se.m.users, se.m.dbs, se.m.dbUsers)
			}
			if _, err := se.m.Stat(l.Dir); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("hoàn tác còn để lại %s", l.Dir)
			}
		})
	}
}

func TestSiteAddSaveFailsStillReturnsPassword(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	se.sites.failSave = errors.New("đĩa đầy")
	a, err := se.s.PrepareAdd(ctx, se.input(t, "shop.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Execute(ctx, nil)
	if err == nil {
		t.Fatal("ghi Store lỗi mà Execute không báo")
	}
	if !strings.Contains(err.Error(), "đĩa đầy") || !strings.Contains(err.Error(), "tpan site add") {
		t.Errorf("lỗi thiếu ngữ cảnh: %v", err)
	}
	// WordPress đã cài: mật khẩu phải còn để CLI in, không thì mất hẳn.
	if !res.WPInstalled || res.AdminPassword.Reveal() == "" {
		t.Error("mất mật khẩu quản trị khi ghi Store lỗi")
	}
}

func TestSiteSecretsNotLeaked(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	a, err := se.s.PrepareAdd(ctx, se.input(t, "shop.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	var dbPw string
	for _, s := range a.Plan.Steps {
		if db, ok := s.(*steps.SiteDB); ok {
			dbPw = db.Password.Reveal()
		}
	}
	adminPw := a.adminPassword.Reveal()
	if len(dbPw) != 32 || len(adminPw) != 32 || dbPw == adminPw {
		t.Fatalf("mật khẩu sinh sai: %d, %d", len(dbPw), len(adminPw))
	}

	pv, err := a.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var events []engine.Event
	res, err := a.Execute(ctx, func(ev engine.Event) { events = append(events, ev) })
	if err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s", err, res.Report.Job.Error)
	}
	stored, err := se.st.Events(ctx, res.Report.Job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Mật khẩu chỉ được nằm trong stdin của đúng lệnh cần nó.
	var cmds []string
	sawStdin := map[string]bool{}
	for _, c := range se.m.Ran() {
		cmds = append(cmds, c.String())
		for _, pw := range []string{dbPw, adminPw} {
			if strings.Contains(string(c.Stdin), pw) {
				sawStdin[pw] = true
			}
		}
	}
	if !sawStdin[dbPw] || !sawStdin[adminPw] {
		t.Error("mật khẩu không đi qua stdin của lệnh nào: test không kiểm được gì")
	}
	for name, v := range map[string]any{
		"Preview": pv, "Report": res.Report, "SiteAddResult": res, "OnEvent": events, "Events trong DB": stored, "Cmd.String": cmds,
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, pw := range []string{dbPw, adminPw} {
			if strings.Contains(string(b), pw) {
				t.Errorf("mật khẩu lọt vào %s", name)
			}
		}
	}
}

func TestSiteVerifyIsolation(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	se.add(t, "blog.example.com")
	res := se.add(t, "shop.example.com")
	ids := map[string]bool{}
	for _, r := range res.Report.Checks {
		ids[r.ID] = true
	}
	for _, want := range []string{
		"web.vhost:shop.example.com", "php.socket:shop.example.com", "wp.checksums:shop.example.com",
		"wp.config:shop.example.com", "db.access:shop.example.com", "http.site:shop.example.com",
		"isolation:shop.example.com:blog.example.com", "isolation:blog.example.com:shop.example.com",
	} {
		if !ids[want] {
			t.Errorf("kiểm chứng thiếu %s (có %v)", want, ids)
		}
	}
}

// ---------------------------------------------------------------- rm, list, info, health

func TestSiteRemove(t *testing.T) {
	const dom = "shop.example.com"

	t.Run("không có site", func(t *testing.T) {
		se := newSiteEnv(t, Nginx)
		_, err := se.s.PrepareRemove(ctx, mustDomain(t, dom))
		if refusedCode(t, err) != "site_not_found" {
			t.Error("muốn site_not_found")
		}
		if len(se.m.Ran()) != 0 {
			t.Errorf("chạy lệnh: %v", se.m.Ran())
		}
	})

	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			se := newSiteEnv(t, web)
			se.add(t, "blog.example.com")
			se.add(t, dom)
			d := mustDomain(t, dom)
			l := site.LayoutOf(d)
			conf, link := l.NginxConf, l.NginxLink
			if web == Apache {
				conf, link = l.ApacheConf, l.ApacheLink
			}

			r, err := se.s.PrepareRemove(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"absent:" + link,
				"absent:" + conf,
				"absent:" + l.PoolConf("8.3"),
				"mariadb.site-db-absent:" + dom,
				"remove-tree:" + l.Dir,
				"remove-tree:" + l.LogDir,
				"site.user.absent:" + l.User,
			}
			if got := stepIDs(r.Plan); !reflect.DeepEqual(got, want) {
				t.Errorf("thứ tự bước:\n got %v\nwant %v", got, want)
			}
			if r.Plan.Kind != "site.rm" || r.Plan.Target != dom || len(r.Plan.Verify) != 0 {
				t.Errorf("Plan = %s/%s, %d phép kiểm", r.Plan.Kind, r.Plan.Target, len(r.Plan.Verify))
			}
			pv, err := r.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			danger := 0
			for _, sp := range pv.Steps {
				if sp.Info.Danger {
					danger++
				}
			}
			if danger == 0 {
				t.Error("xoá site mà không bước nào Danger: CLI sẽ không hỏi lại")
			}

			rep, err := r.Execute(ctx, nil)
			if err != nil || rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("%v %s %s", err, rep.Job.Status, rep.Job.Error)
			}
			if _, ok, _ := se.sites.Site(ctx, d); ok {
				t.Error("xoá xong mà Store còn site")
			}
			if _, ok, _ := se.sites.Site(ctx, mustDomain(t, "blog.example.com")); !ok {
				t.Error("xoá nhầm site khác khỏi Store")
			}
			if _, found := se.m.users[l.User]; found || se.m.dbs[l.DB] || se.m.dbUsers[l.DB] {
				t.Error("còn user hoặc DB của site đã xoá")
			}
			for _, p := range []string{l.Dir, l.LogDir, conf, link, l.PoolConf("8.3")} {
				if _, err := se.m.Stat(p); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("còn %s", p)
				}
			}
		})
	}

	t.Run("job hỏng thì giữ dòng trong Store", func(t *testing.T) {
		se := newSiteEnv(t, Nginx)
		se.add(t, dom)
		se.m.Handle([]string{"userdel"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 8, Stderr: "userdel: user is currently used by process 42\n"}
		})
		r, err := se.s.PrepareRemove(ctx, mustDomain(t, dom))
		if err != nil {
			t.Fatal(err)
		}
		rep, err := r.Execute(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Job.Status == engine.JobSucceeded {
			t.Fatal("userdel lỗi mà job thành công")
		}
		if se.sites.count() != 1 {
			t.Error("job hỏng mà xoá dòng trong Store")
		}
	})
}

func TestSiteListInfoHealth(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	d := mustDomain(t, dom)

	if _, err := se.s.Info(ctx, d); refusedCode(t, err) != "site_not_found" {
		t.Error("Info site không có: muốn site_not_found")
	}
	if _, err := se.s.Health(ctx, d); refusedCode(t, err) != "site_not_found" {
		t.Error("Health site không có: muốn site_not_found")
	}

	se.add(t, "blog.example.com")
	se.add(t, dom)

	list, err := se.s.List(ctx)
	if err != nil || len(list) != 2 || list[0].Domain.String() != "blog.example.com" {
		t.Fatalf("List = %+v, %v", list, err)
	}

	info, err := se.s.Info(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if info.Site.Domain != d || info.Layout != site.LayoutOf(d) || info.Web != Nginx || len(info.Checks) == 0 {
		t.Errorf("Info = %+v", info)
	}

	h, err := se.s.Health(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Installed || h.HasFail() {
		t.Errorf("Health: installed=%v fail=%v %s", h.Installed, h.HasFail(), failedChecks(h.Results))
	}
	ids := map[string]bool{}
	for _, r := range h.Results {
		ids[r.ID] = true
	}
	if !ids["http.site:"+dom] || !ids["isolation:"+dom+":blog.example.com"] {
		t.Errorf("Health thiếu phép kiểm: %v", ids)
	}

	// Gây lỗi một chặng: health phải thấy.
	l := site.LayoutOf(d)
	if err := se.m.Chmod(l.WPConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	h, _ = se.s.Health(ctx, d)
	if !h.HasFail() {
		t.Error("wp-config.php 0644 mà health không báo lỗi")
	}
}

func TestEmptySiteStore(t *testing.T) {
	// --dry-run khi chưa có file DB.
	var es EmptySiteStore
	if _, ok, err := es.Site(ctx, mustDomain(t, "shop.example.com")); ok || err != nil {
		t.Errorf("Site = %v, %v", ok, err)
	}
	if err := es.SaveSite(ctx, site.Site{}); err == nil {
		t.Error("EmptySiteStore cho ghi")
	}
	s := &Sites{Sys: system.NewFake(), Settings: EmptySettings{}, Store: EmptySiteStore{}}
	_, err := s.PrepareAdd(ctx, SiteInput{Domain: mustDomain(t, "shop.example.com"), Email: mustEmail(t)})
	if refusedCode(t, err) != "stack_not_installed" {
		t.Error("muốn stack_not_installed")
	}
}
