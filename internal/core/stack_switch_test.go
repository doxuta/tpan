package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- giả lập

// switchEnv là siteSim cộng phần máy mà Plan "stack.switch" đụng tới: gói
// (dpkg-query, apt-get install có tôn trọng policy-rc.d), systemd (start,
// stop, enable, disable; hai web server không cùng giữ được cổng 80), module
// Apache, nhóm của user nginx, gpg cho kho nginx.org, và curl trả lời theo đúng
// web server ĐANG chạy: site chỉ trả 200 khi web server đang active có vhost của
// nó (siteSim trả 200 khi có link vhost của bất kỳ web server nào).
type switchEnv struct {
	*siteSim
	st *store.Store

	lk     sync.Mutex
	pkgs   map[string]bool
	units  map[string]unitState
	mods   map[string]bool
	groups map[string][]string
	// nginxOrgPkg: gói nginx cài vào là của nginx.org (conf.d/default.conf,
	// user nginx, không có sites-*).
	nginxOrgPkg bool
	// broken: web server này trả 500 cho mọi site (giả lập lỗi giữa chừng).
	broken map[string]bool
	// failDom: site này trả 500 trên mọi web server (site hỏng sẵn).
	failDom map[string]bool
}

const switchTmp = "/tmp/tpan-apt-key.AbCd1234"

var webUnitRE = regexp.MustCompile(`^systemctl (start|restart|reload|enable)( --now)? (nginx|apache2)$`)

func newSwitchEnv(t *testing.T, from WebServer, src Source, sites ...site.Site) *switchEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatalf("mở store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &switchEnv{
		siteSim: newSiteSim(t),
		st:      st,
		pkgs:    map[string]bool{},
		units:   map[string]unitState{},
		mods:    map[string]bool{},
		groups:  map[string][]string{},
		broken:  map[string]bool{},
		failDom: map[string]bool{},
	}
	e.SetFile("/etc/os-release", []byte(osReleaseNoble), 0o644, system.Root)
	e.SetDir("/usr/sbin", 0o755, system.Root)
	e.SetDir("/tmp", 0o777, system.Root)
	e.handlers()
	midCheckWait = 0
	t.Cleanup(func() { midCheckWait = time.Second })

	mustSet(t, st, SettingWeb, string(from))
	mustSet(t, st, SettingWebSource, string(src))
	mustSet(t, st, SettingPHPVersion, "8.3")
	anySSL := false
	for _, s := range sites {
		s.Created = time.Now()
		if err := st.SaveSite(ctx, s); err != nil {
			t.Fatal(err)
		}
		e.SetDir(s.Layout().Public, 0o755, system.Root)
		anySSL = anySSL || s.SSL
	}

	// Máy đang chạy web server cũ với mọi file tpan đã ghi (cài stack, SSL, tạo
	// site), ghi qua engine để managed_files có dòng của chúng như máy thật.
	cfg := StackConfig{Web: from, WebSource: src, PHPVersion: "8.3"}
	if nginxOrg(cfg) {
		e.addNginxRepo(t, src == SourceNginxMainline)
		e.nginxOrgPkg = true
	}
	e.lk.Lock()
	e.install(webUnit(from))
	e.lk.Unlock()
	ss, err := webSteps(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if anySSL {
		ssl, err := steps.DefaultSSL(string(from))
		if err != nil {
			t.Fatal(err)
		}
		ss = append(ss, ssl...)
	}
	for _, s := range sites {
		if s.Status != site.Active {
			continue // thùng rác đã gỡ vhost
		}
		v, err := siteVhost(s, cfg)
		if err != nil {
			t.Fatal(err)
		}
		ss = append(ss, v...)
	}
	lr, err := logrotateStep(from)
	if err != nil {
		t.Fatal(err)
	}
	ss = append(ss, lr)
	rep, err := engine.New(e, engine.Options{Journal: st}).Execute(ctx, engine.Plan{
		Kind: "seed", Target: string(from), Title: i18n.T("dựng máy", "seed"), Steps: ss,
	})
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("dựng máy cũ: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	e.ResetRan()
	return e
}

func (e *switchEnv) sw() *Switch {
	return &Switch{Sys: e, Settings: e.st, Sites: e.st, Journal: e.st}
}

// addNginxRepo đặt kho nginx.org đúng như bước AptRepo để lại.
func (e *switchEnv) addNginxRepo(t *testing.T, mainline bool) {
	t.Helper()
	r, err := repoNginx("noble", mainline)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Info().Files {
		e.SetFile(f.Path, f.New, 0o644, system.Root)
	}
	e.SetFile(steps.AptKeyPath(RepoNginx), []byte("khoá"), 0o644, system.Root)
}

// install giả lập apt để lại gì sau khi cài gói p. policy-rc.d thoát 101 thì
// dịch vụ không chạy nhưng VẪN enabled (đã kiểm ở M1, mục 16). Gọi khi giữ e.lk.
func (e *switchEnv) install(p string) {
	e.pkgs[p] = true
	blocked := false
	if data, err := e.ReadFile("/usr/sbin/policy-rc.d"); err == nil && strings.Contains(string(data), "exit 101") {
		blocked = true
	}
	on := unitState{active: !blocked, enabled: true}
	switch p {
	case "nginx":
		if e.nginxOrgPkg {
			e.SetFile("/etc/nginx/conf.d/default.conf", []byte("server { listen 80; }\n"), 0o644, system.Root)
			if _, ok := e.groups["nginx"]; !ok {
				e.groups["nginx"] = []string{"nginx"}
			}
		} else {
			e.SetFile("/etc/nginx/sites-available/default", []byte("server {}\n"), 0o644, system.Root)
			e.SetSymlink("/etc/nginx/sites-available/default", "/etc/nginx/sites-enabled/default")
		}
		e.SetDir("/etc/logrotate.d", 0o755, system.Root)
		e.units["nginx"] = on
	case "apache2":
		e.SetFile("/etc/apache2/sites-available/000-default.conf", []byte("<VirtualHost *:80>\n</VirtualHost>\n"), 0o644, system.Root)
		e.SetSymlink("../sites-available/000-default.conf", "/etc/apache2/sites-enabled/000-default.conf")
		e.SetDir("/etc/logrotate.d", 0o755, system.Root)
		e.units["apache2"] = on
		e.mods["mpm_prefork"] = true
		e.mods["status"] = true // gói apache2 của Ubuntu bật sẵn (H5)
	}
}

func (e *switchEnv) unit(u string) unitState {
	e.lk.Lock()
	defer e.lk.Unlock()
	return e.units[u]
}

func (e *switchEnv) handlers() {
	ok := system.Result{}
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}
	e.Handle([]string{"dpkg-query"}, func(c system.Cmd) system.Result {
		e.lk.Lock()
		defer e.lk.Unlock()
		status := "install ok installed"
		if strings.Contains(strings.Join(c.Args, " "), "db:Status-Status") {
			status = "installed"
		}
		var out, errs strings.Builder
		for _, p := range c.Args {
			if strings.HasPrefix(p, "-") || strings.Contains(p, "$") {
				continue
			}
			if e.pkgs[p] {
				out.WriteString(p + "\t" + status + "\n")
			} else {
				errs.WriteString("dpkg-query: no packages found matching " + p + "\n")
			}
		}
		if errs.Len() > 0 {
			return exit(1, out.String(), errs.String())
		}
		return exit(0, out.String(), "")
	})
	e.Handle([]string{"apt-get", "install"}, func(c system.Cmd) system.Result {
		e.lk.Lock()
		defer e.lk.Unlock()
		for i, a := range c.Args[1:] {
			if strings.HasPrefix(a, "-") || c.Args[i] == "-o" {
				continue
			}
			e.install(a)
		}
		return ok
	})
	e.Handle([]string{"systemctl"}, func(c system.Cmd) system.Result {
		e.lk.Lock()
		defer e.lk.Unlock()
		verb, now := c.Args[0], slices.Contains(c.Args, "--now")
		var units []string
		for _, a := range c.Args[1:] {
			if !strings.HasPrefix(a, "-") {
				units = append(units, a)
			}
		}
		start := func(u string) bool {
			other := map[string]string{"nginx": "apache2", "apache2": "nginx"}[u]
			if other != "" && e.units[other].active {
				return false // cổng 80 đang bị web server kia giữ
			}
			s := e.units[u]
			s.active = true
			e.units[u] = s
			return true
		}
		for _, u := range units {
			s := e.units[u]
			switch verb {
			case "is-active":
				if !s.active {
					return exit(3, "inactive\n", "")
				}
				return exit(0, "active\n", "")
			case "is-enabled":
				if !s.enabled {
					return exit(1, "disabled\n", "")
				}
				return exit(0, "enabled\n", "")
			case "start":
				if !start(u) {
					return exit(1, "", "Job for "+u+".service failed: Address already in use\n")
				}
			case "stop":
				s.active = false
				e.units[u] = s
			case "enable":
				s.enabled = true
				e.units[u] = s
				if now && !start(u) {
					return exit(1, "", "Job for "+u+".service failed: Address already in use\n")
				}
			case "disable":
				s.enabled = false
				if now {
					s.active = false
				}
				e.units[u] = s
			}
		}
		return ok
	})
	e.Handle([]string{"a2query", "-m"}, func(c system.Cmd) system.Result {
		e.lk.Lock()
		defer e.lk.Unlock()
		if e.mods[c.Args[1]] {
			return exit(0, c.Args[1]+" (enabled by site administrator)\n", "")
		}
		return exit(32, "", "No module matches "+c.Args[1]+"\n")
	})
	mods := func(on bool) func(system.Cmd) system.Result {
		return func(c system.Cmd) system.Result {
			e.lk.Lock()
			defer e.lk.Unlock()
			for _, m := range c.Args[1:] {
				if on {
					e.mods[m] = true
				} else {
					delete(e.mods, m)
				}
			}
			return ok
		}
	}
	e.Handle([]string{"a2enmod"}, mods(true))
	e.Handle([]string{"a2dismod"}, mods(false))
	e.Handle([]string{"id", "-nG"}, func(c system.Cmd) system.Result {
		e.lk.Lock()
		defer e.lk.Unlock()
		g, found := e.groups[c.Args[1]]
		if !found {
			return exit(1, "", "id: '"+c.Args[1]+"': no such user\n")
		}
		return exit(0, strings.Join(g, " ")+"\n", "")
	})
	e.Handle([]string{"usermod", "-aG"}, func(c system.Cmd) system.Result {
		e.lk.Lock()
		defer e.lk.Unlock()
		e.groups[c.Args[2]] = append(e.groups[c.Args[2]], c.Args[1])
		return ok
	})
	e.Handle([]string{"gpasswd", "-d"}, func(c system.Cmd) system.Result {
		e.lk.Lock()
		defer e.lk.Unlock()
		e.groups[c.Args[1]] = slices.DeleteFunc(e.groups[c.Args[1]], func(g string) bool { return g == c.Args[2] })
		return ok
	})
	e.Handle([]string{"mktemp"}, func(system.Cmd) system.Result {
		e.SetDir(switchTmp, 0o700, system.Root)
		return exit(0, switchTmp+"\n", "")
	})
	e.Handle([]string{"gpg"}, func(c system.Cmd) system.Result {
		if slices.Contains(c.Args, "--show-keys") {
			return exit(0, "pub:-:4096:1:ABCD:1:::-:::scESC::::::23::0:\nfpr:::::::::"+nginxFingerprints[0]+":\n", "")
		}
		return ok
	})
	e.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		if r := argAfter(c.Args, "--resolve"); r != "" { // HTTPSite, HTTPSSite
			dom, _, _ := strings.Cut(r, ":")
			code := e.siteCode(dom)
			body := "<html>" + code + "</html>"
			if code == "200" {
				body = "<html><link rel='stylesheet' href='http://" + dom + "/wp-content/themes/x/style.css'></html>"
			}
			return exit(0, body+"TPAN_HTTP_CODE="+code, "")
		}
		if slices.Contains(c.Args, "http://127.0.0.1/") { // HTTPDefault
			switch e.active() {
			case "nginx":
				return exit(52, "000", "curl: (52) Empty reply from server\n")
			case "apache2":
				return exit(0, "403", "")
			}
			return exit(7, "000", "curl: (7) Failed to connect\n")
		}
		return ok
	})
}

// active trả web server đang giữ cổng 80, hoặc rỗng.
func (e *switchEnv) active() string {
	e.lk.Lock()
	defer e.lk.Unlock()
	for _, u := range []string{"nginx", "apache2"} {
		if e.units[u].active {
			return u
		}
	}
	return ""
}

// siteCode là mã HTTP site dom trả: 200 khi web server đang chạy có vhost của
// nó (và với gói nginx.org: đã đọc sites-enabled, user nginx đọc được file của
// site), 404 khi không, 500 khi web server hay site bị đặt hỏng.
func (e *switchEnv) siteCode(dom string) string {
	u := e.active()
	e.lk.Lock()
	broken, fail, org := e.broken[u], e.failDom[dom], e.nginxOrgPkg
	inGroup := slices.Contains(e.groups["nginx"], "www-data")
	e.lk.Unlock()
	if u == "" {
		return "000"
	}
	if broken || fail {
		return "500"
	}
	d, err := validate.ParseDomain(dom)
	if err != nil {
		return "400"
	}
	l := site.LayoutOf(d)
	link := l.ApacheLink
	if u == "nginx" {
		link = l.NginxLink
		if org {
			if _, err := e.Stat(nginxOrgConf); err != nil || !inGroup {
				return "404"
			}
		}
	}
	if _, err := e.Stat(link); err != nil {
		return "404"
	}
	return "200"
}

// ---------------------------------------------------------------- tiện ích

func switchSite(t *testing.T, dom string, ssl bool) site.Site {
	return site.Site{Domain: mustDomain(t, dom), Status: site.Active, PHPVersion: "8.3", SSL: ssl}
}

// swapIndex là vị trí bước WebSwap trong Plan.
func swapIndex(t *testing.T, p engine.Plan) int {
	t.Helper()
	for i, s := range p.Steps {
		if _, ok := s.(*steps.WebSwap); ok {
			return i
		}
	}
	t.Fatalf("Plan không có bước WebSwap: %q", stepIDs(p))
	return -1
}

// execute chạy Planned và trả số lệnh đã chạy lúc bước WebSwap bắt đầu.
func (e *switchEnv) execute(t *testing.T, p *Planned) (engine.Report, int, error) {
	t.Helper()
	cut := -1
	rep, err := p.Execute(ctx, func(ev engine.Event) {
		if cut < 0 && strings.HasPrefix(ev.StepID, "web.swap:") && (ev.Kind == engine.EventStepStart || ev.Kind == engine.EventStepSkip) {
			cut = len(e.Ran())
		}
	})
	return rep, cut, err
}

func (e *switchEnv) setting(t *testing.T, key string) string {
	t.Helper()
	v, _, err := e.st.GetSetting(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (e *switchEnv) managed(t *testing.T) map[string]string {
	t.Helper()
	fs, err := e.st.ManagedFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range fs {
		out[f.Path] = f.SHA256
	}
	return out
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// ---------------------------------------------------------------- test

// Bẫy 1: từ đầu Plan tới WebSwap không lệnh nào khởi động, khởi động lại,
// reload hay bật web server (cả lệnh khai trong Info lẫn lệnh chạy thật).
func TestSwitchNoStartBeforeSwap(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to WebServer
		src      Source
		repo     int // 0: không có kho nginx.org; 1: stable; 2: mainline
	}{
		{"nginx sang apache, có SSL", Nginx, Apache, SourceUbuntu, 0},
		{"apache sang nginx, có SSL", Apache, Nginx, SourceUbuntu, 0},
		{"nginx.org sang apache", Nginx, Apache, SourceNginxStable, 0},
		{"apache sang nginx.org", Apache, Nginx, SourceUbuntu, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSwitchEnv(t, tc.from, tc.src, switchSite(t, "a.example.com", true), switchSite(t, "b.example.com", false))
			if tc.repo > 0 {
				e.addNginxRepo(t, tc.repo == 2)
				e.nginxOrgPkg = true
			}
			p, err := e.sw().Prepare(ctx, tc.to)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			swap := swapIndex(t, p.Plan)
			for _, s := range p.Plan.Steps[:swap] {
				for _, c := range s.Info().Commands {
					if webUnitRE.MatchString(c) {
						t.Errorf("bước %s trước WebSwap khai lệnh %q", s.Info().ID, c)
					}
				}
			}
			rep, cut, err := e.execute(t, p)
			if err != nil || rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("Execute: %v %s %s%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
			}
			if cut < 0 {
				t.Fatal("không thấy sự kiện của bước WebSwap")
			}
			for _, c := range e.Ran()[:cut] {
				if webUnitRE.MatchString(c.String()) {
					t.Errorf("trước WebSwap đã chạy %q", c.String())
				}
			}
			newU, oldU := webUnit(tc.to), webUnit(tc.from)
			if u := e.unit(newU); !u.active || !u.enabled {
				t.Errorf("%s sau khi chuyển: %+v", newU, u)
			}
			if u := e.unit(oldU); u.active || u.enabled {
				t.Errorf("%s sau khi chuyển: %+v", oldU, u)
			}
			if _, err := e.Stat("/usr/sbin/policy-rc.d"); err == nil {
				t.Error("policy-rc.d còn sót")
			}
		})
	}
}

// Bẫy 2: kiểm HTTP giữa chừng lỗi sau WebSwap thì máy về đúng như cũ.
func TestSwitchRollback(t *testing.T) {
	for _, tc := range []struct{ from, to WebServer }{{Nginx, Apache}, {Apache, Nginx}} {
		t.Run(string(tc.from)+"→"+string(tc.to), func(t *testing.T) {
			e := newSwitchEnv(t, tc.from, SourceUbuntu, switchSite(t, "a.example.com", true), switchSite(t, "b.example.com", false))
			oldDir := "/etc/" + webUnit(tc.from) + "/"
			before := map[string]system.FakeEntry{}
			for k, v := range e.Snapshot() {
				if strings.HasPrefix(k, oldDir) {
					before[k] = v
				}
			}
			files := e.managed(t)
			e.broken[webUnit(tc.to)] = true
			p, err := e.sw().Prepare(ctx, tc.to)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			rep, _, err := e.execute(t, p)
			if err != nil || rep.Job.Status != engine.JobRolledBack {
				t.Fatalf("Execute: %v %s %s", err, rep.Job.Status, rep.Job.Error)
			}
			if !strings.Contains(rep.Job.Error, "http") {
				t.Errorf("lỗi không đến từ bước kiểm HTTP: %s", rep.Job.Error)
			}
			if u := e.unit(webUnit(tc.to)); u.active || u.enabled {
				t.Errorf("web server mới sau hoàn tác: %+v", u)
			}
			if u := e.unit(webUnit(tc.from)); !u.active || !u.enabled {
				t.Errorf("web server cũ sau hoàn tác: %+v", u)
			}
			if _, err := e.Stat("/usr/sbin/policy-rc.d"); err == nil {
				t.Error("policy-rc.d còn sót")
			}
			after := map[string]system.FakeEntry{}
			for k, v := range e.Snapshot() {
				if strings.HasPrefix(k, oldDir) {
					after[k] = v
				}
			}
			if !mapsEqual(before, after) {
				t.Errorf("file của %s đổi sau hoàn tác", webUnit(tc.from))
			}
			if got := e.managed(t); !mapsEqual(files, got) {
				t.Errorf("managed_files đổi sau hoàn tác:\ntrước %v\nsau   %v", files, got)
			}
			if w, src := e.setting(t, SettingWeb), e.setting(t, SettingWebSource); w != string(tc.from) || src != string(SourceUbuntu) {
				t.Errorf("settings đổi sau hoàn tác: %s %s", w, src)
			}
		})
	}
}

func mapsEqual[K comparable, V comparable](a, b map[K]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// Bẫy 3: thành công thì settings, managed_files đúng và health dựng lại được
// file với cấu hình mới.
func TestSwitchSucceeds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to WebServer
		src      Source
		repo     int
		wantSrc  Source
	}{
		{"nginx.org sang apache", Nginx, Apache, SourceNginxStable, 0, SourceUbuntu},
		{"apache sang nginx, không kho", Apache, Nginx, SourceUbuntu, 0, SourceUbuntu},
		{"apache sang nginx, kho stable", Apache, Nginx, SourceUbuntu, 1, SourceNginxStable},
		{"apache sang nginx, kho mainline", Apache, Nginx, SourceUbuntu, 2, SourceNginxMainline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSwitchEnv(t, tc.from, tc.src, switchSite(t, "a.example.com", true), switchSite(t, "b.example.com", false))
			if tc.repo > 0 {
				e.addNginxRepo(t, tc.repo == 2)
				e.nginxOrgPkg = true
			}
			newCfg := StackConfig{Web: tc.to, WebSource: tc.wantSrc, PHPVersion: "8.3"}
			// Một file của web server mới đã đúng sẵn mà chưa có dòng trong
			// managed_files: bước ghi nó được bỏ qua, mà vẫn phải được ghi nhận.
			ws, err := webSteps(newCfg, false)
			if err != nil {
				t.Fatal(err)
			}
			pre := files(ws)[len(files(ws))-1] // site mặc định cổng 80
			e.SetFile(pre.Path, pre.Content, 0o644, system.Root)

			p, err := e.sw().Prepare(ctx, tc.to)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if tc.wantSrc != SourceUbuntu {
				var bare bool
				for _, s := range p.Plan.Steps {
					if _, ok := s.(*steps.UserInGroup); ok {
						bare = true
					}
					if _, ok := s.(*groupRestart); ok {
						t.Error("Plan chuyển có bước khởi động lại nginx sau khi thêm nhóm")
					}
				}
				if !bare {
					t.Errorf("thiếu bước cho nginx vào nhóm www-data: %q", stepIDs(p.Plan))
				}
			}
			want := files(p.Plan.Steps[:swapIndex(t, p.Plan)])
			rep, _, err := e.execute(t, p)
			if err != nil || rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("Execute: %v %s %s%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
			}
			if !slices.ContainsFunc(rep.Steps, func(r engine.StepResult) bool { return r.ID == "file:"+pre.Path && r.Outcome == "skipped" }) {
				t.Errorf("bước %s không được bỏ qua", pre.Path)
			}
			if w, src := e.setting(t, SettingWeb), e.setting(t, SettingWebSource); w != string(tc.to) || src != string(tc.wantSrc) {
				t.Errorf("settings = %s %s; muốn %s %s", w, src, tc.to, tc.wantSrc)
			}
			mf := e.managed(t)
			for _, f := range want {
				if mf[f.Path] != sha(f.Content) {
					t.Errorf("managed_files thiếu hoặc sai %s", f.Path)
				}
			}
			oldDir := "/etc/" + webUnit(tc.from) + "/"
			for path := range mf {
				if strings.HasPrefix(path, oldDir) {
					t.Errorf("managed_files còn file của web server cũ: %s", path)
				}
			}
			if tc.wantSrc != SourceUbuntu && !slices.Contains(e.groups["nginx"], "www-data") {
				t.Errorf("nginx không vào nhóm www-data: %v", e.groups)
			}
			for _, c := range e.Ran() {
				if c.String() == "systemctl restart nginx" || c.String() == "systemctl restart apache2" {
					t.Errorf("chạy %s", c.String())
				}
			}

			// Health dựng lại được file với cấu hình mới (không source_unsupported).
			f := &Fixes{Sys: e, Settings: e.st, Store: e.st, WPCLISHA512: fakePharSHA()}
			cfg, err := (&Sites{Settings: e.st}).stack(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.stackPlan(cfg); err != nil {
				t.Fatalf("stackPlan với cấu hình mới: %v", err)
			}
			// ponytail: site mặc định 443 không có trong managedFile của fix.go
			// (thiếu từ M5, không do M10); bỏ qua nó ở đây.
			rebuilt := 0
			defer func() {
				if rebuilt < 3 { // site mặc định 80 và vhost của hai site
					t.Errorf("chỉ dựng lại được %d file của web server mới", rebuilt)
				}
			}()
			for path := range mf {
				if strings.HasSuffix(path, ".htaccess") || strings.Contains(path, "default-ssl") || !strings.HasPrefix(path, "/etc/"+webUnit(tc.to)+"/") {
					continue
				}
				_, file, err := f.managedFile(ctx, cfg, path)
				if err != nil || file == nil {
					t.Errorf("managedFile(%s) = %v, %v", path, file, err)
					continue
				}
				if disk, _ := e.ReadFile(path); string(disk) != string(file.Content) {
					t.Errorf("%s trên đĩa khác bản health dựng lại", path)
				}
				rebuilt++
			}
		})
	}
}

// Bẫy 4: nội dung Plan: vhost 443 cho site SSL, socket theo bản PHP của site,
// site mặc định 443 kéo theo cả site SSL trong thùng rác, .htaccess chỉ ghi
// khi chưa có. Xem trước không đổi gì.
func TestSwitchPlanContent(t *testing.T) {
	ssl := switchSite(t, "ssl.example.com", true)
	php84 := switchSite(t, "php84.example.com", false)
	php84.PHPVersion = "8.4"
	trashed := switchSite(t, "trash.example.com", true)
	trashed.Status = site.Trashed
	hasHt := switchSite(t, "ht.example.com", false)
	e := newSwitchEnv(t, Nginx, SourceUbuntu, ssl, php84, trashed, hasHt)
	htPath := hasHt.Layout().Public + "/.htaccess"
	e.SetFile(htPath, []byte("# của người dùng\n"), 0o640, system.Root)

	before := e.Snapshot()
	p, err := e.sw().Prepare(ctx, Apache)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if p.Plan.Kind != "stack.switch" || p.Plan.Target != "apache" || p.Plan.Title.Vi != "Chuyển LEMP (Nginx) sang LAMP (Apache)" {
		t.Errorf("Kind/Target/Title = %q %q %q", p.Plan.Kind, p.Plan.Target, p.Plan.Title.Vi)
	}
	pv, err := p.Preview(ctx)
	if err != nil || len(pv.Steps) != len(p.Plan.Steps) {
		t.Fatalf("Preview: %v", err)
	}
	// ID phải duy nhất trong Plan (engine.StepInfo): mod ssl của DefaultSSL
	// từng trùng ID với bước bật mod của stack.
	seen := map[string]bool{}
	for _, s := range p.Plan.Steps {
		id := s.Info().ID
		if seen[id] {
			t.Errorf("ID bước %q xuất hiện hai lần", id)
		}
		seen[id] = true
	}
	if !mapsEqual(before, e.Snapshot()) {
		t.Error("Prepare hoặc Preview làm đổi hệ thống file")
	}
	for _, c := range e.Ran() {
		if !readOnly(c) && c.Name != "curl" {
			t.Errorf("Prepare hoặc Preview chạy lệnh thay đổi: %s", c.String())
		}
	}

	ids := stepIDs(p.Plan)
	swap := swapIndex(t, p.Plan)
	byPath := map[string]*steps.File{}
	for _, f := range files(p.Plan.Steps[:swap]) {
		byPath[f.Path] = f
	}
	if f := byPath[ssl.Layout().ApacheConf]; f == nil || !strings.Contains(string(f.Content), "443") {
		t.Errorf("vhost của site SSL không phải bản 443: %v", f)
	}
	if f := byPath[php84.Layout().ApacheConf]; f == nil || !strings.Contains(string(f.Content), "-8.4.sock") {
		t.Errorf("vhost của site PHP 8.4 không trỏ socket -8.4.sock")
	}
	if byPath[trashed.Layout().ApacheConf] != nil {
		t.Error("site trong thùng rác được ghi vhost")
	}
	if byPath["/etc/apache2/sites-available/000-tpan-default-ssl.conf"] == nil {
		t.Errorf("thiếu site mặc định 443: %q", ids)
	}
	if byPath[ssl.Layout().Public+"/.htaccess"] == nil {
		t.Error("site chưa có .htaccess mà không được ghi")
	}
	if byPath[htPath] != nil {
		t.Error(".htaccess có sẵn bị ghi đè")
	}
	if byPath[logrotateCnf] == nil || !strings.Contains(string(byPath[logrotateCnf].Content), "apache2") {
		t.Error("thiếu file logrotate của apache")
	}
	for _, s := range p.Plan.Steps[:swap] {
		switch x := s.(type) {
		case *steps.File:
			if x.Reload != nil {
				t.Errorf("%s còn Reload", x.Path)
			}
		case *steps.Symlink:
			if x.Reload != nil {
				t.Errorf("%s còn Reload", x.Link)
			}
		case *steps.Absent:
			if x.Reload != nil {
				t.Errorf("%s còn Reload", x.Path)
			}
		case *steps.ApacheModules:
			if !x.NoRestart {
				t.Errorf("ApacheModules %v không NoRestart", x.Enable)
			}
		}
	}
	// Thứ tự lớn: cài không chạy, cấu hình, đổi, kiểm từng site, bật/tắt, gỡ file cũ.
	wantHead := []string{"apt.update:lists", "apt.install-nostart:apache2", "apache.modules:apache2"}
	if !slices.Equal(ids[:3], wantHead) {
		t.Errorf("đầu Plan = %q", ids[:3])
	}
	after := ids[swap:]
	wantMid := []string{"web.swap:nginx:apache2", "stack.switch.check:ht.example.com", "stack.switch.check:php84.example.com", "stack.switch.check:ssl.example.com", "service:apache2", "service.disable:nginx"}
	if !slices.Equal(after[:len(wantMid)], wantMid) {
		t.Errorf("giữa Plan = %q", after)
	}
	var gone []string
	for _, id := range after[len(wantMid):] {
		path, ok := strings.CutPrefix(id, "absent:")
		if !ok || !strings.HasPrefix(path, "/etc/nginx/") {
			t.Errorf("bước sau cùng lạ: %s", id)
		}
		gone = append(gone, path)
	}
	for _, path := range []string{
		trashed.Layout().NginxLink, trashed.Layout().NginxConf, ssl.Layout().NginxLink, ssl.Layout().NginxConf,
		"/etc/nginx/sites-enabled/tpan-default.conf", "/etc/nginx/sites-available/tpan-default.conf",
		"/etc/nginx/sites-enabled/tpan-default-ssl.conf", "/etc/nginx/sites-available/tpan-default-ssl.conf",
	} {
		if !slices.Contains(gone, path) {
			t.Errorf("không gỡ %s", path)
		}
	}
	if slices.Index(gone, ssl.Layout().NginxLink) > slices.Index(gone, ssl.Layout().NginxConf) {
		t.Error("gỡ file vhost trước link của nó")
	}
	for _, s := range p.Plan.Steps[swap:] {
		if a, ok := s.(*steps.Absent); ok && (a.Test != nil || a.Reload != nil || strings.Contains(a.Info().Why.Vi, "distro")) {
			t.Errorf("bước gỡ %s có Test/Reload hoặc câu lý do sai", a.Path)
		}
	}
	var siteIDs []string
	for _, c := range p.Plan.Verify {
		if r := c.Run(ctx, e); strings.Contains(r.ID, "example.com") {
			siteIDs = append(siteIDs, r.ID)
		}
	}
	slices.Sort(siteIDs)
	wantVerify := []string{"http.site:ht.example.com", "http.site:php84.example.com", "https.site:ssl.example.com", "web.vhost:ht.example.com", "web.vhost:php84.example.com", "web.vhost:ssl.example.com"}
	if !slices.Equal(siteIDs, wantVerify) {
		t.Errorf("phép kiểm site trong Verify = %q", siteIDs)
	}

	// Chỉ site SSL nằm trong thùng rác vẫn kéo theo site mặc định 443.
	e2 := newSwitchEnv(t, Nginx, SourceUbuntu, trashed, switchSite(t, "a.example.com", false))
	p2, err := e2.sw().Prepare(ctx, Apache)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(stepIDs(p2.Plan), "file:/etc/apache2/sites-available/000-tpan-default-ssl.conf") {
		t.Errorf("site SSL trong thùng rác không kéo theo site mặc định 443: %q", stepIDs(p2.Plan))
	}
	// Không site nào có SSL thì không bật cổng 443.
	e3 := newSwitchEnv(t, Nginx, SourceUbuntu, switchSite(t, "a.example.com", false))
	p3, err := e3.sw().Prepare(ctx, Apache)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(stepIDs(p3.Plan), func(id string) bool { return strings.Contains(id, "default-ssl") && strings.HasPrefix(id, "file:") }) {
		t.Errorf("không site SSL mà vẫn ghi site mặc định 443: %q", stepIDs(p3.Plan))
	}
}

// Bẫy 5: điều kiện đầu.
func TestSwitchRefusals(t *testing.T) {
	noMutation := func(t *testing.T, e *switchEnv) {
		t.Helper()
		for _, c := range e.Ran() {
			if !readOnly(c) && c.Name != "curl" {
				t.Errorf("từ chối mà vẫn chạy %s", c.String())
			}
		}
	}
	for _, bad := range []string{"", "caddy", "Apache", "nginx; rm -rf /", "$(id)", "../../etc/passwd", "nginx\napache"} {
		e := newSwitchEnv(t, Nginx, SourceUbuntu)
		if _, err := e.sw().Prepare(ctx, WebServer(bad)); refusedCode(t, err) != "web_invalid" {
			t.Errorf("%q: muốn web_invalid", bad)
		}
		noMutation(t, e)
	}

	e := newSwitchEnv(t, Nginx, SourceUbuntu, switchSite(t, "a.example.com", false))
	if _, err := e.sw().Prepare(ctx, Nginx); refusedCode(t, err) != "stack_same_web" {
		t.Error("muốn stack_same_web")
	}
	mustSet(t, e.st, SettingPHPVersion, "")
	if _, err := e.sw().Prepare(ctx, Apache); refusedCode(t, err) != "stack_not_installed" {
		t.Error("muốn stack_not_installed")
	}
	noMutation(t, e)
	empty := &Switch{Sys: e, Settings: EmptySettings{}, Sites: EmptySiteStore{}}
	if _, err := empty.Prepare(ctx, Apache); refusedCode(t, err) != "stack_not_installed" {
		t.Error("chưa có DB: muốn stack_not_installed")
	}

	// Một site active đang hỏng sẵn: từ chối, kèm kết quả Fail của site đó.
	e = newSwitchEnv(t, Nginx, SourceUbuntu, switchSite(t, "a.example.com", false), switchSite(t, "b.example.com", true))
	e.failDom["b.example.com"] = true
	_, err := e.sw().Prepare(ctx, Apache)
	if refusedCode(t, err) != "precondition_failed" {
		t.Fatal("muốn precondition_failed")
	}
	var re *RefusedError
	errors.As(err, &re)
	if len(re.Checks) != 1 || re.Checks[0].ID != "https.site:b.example.com" || re.Checks[0].Status != checks.Fail {
		t.Errorf("Checks = %+v", re.Checks)
	}
	noMutation(t, e)
}

// brokenSettings là store mà SetSetting lỗi khi fail bật.
type brokenSettings struct {
	Settings
	fail bool
}

func (f *brokenSettings) SetSetting(ctx context.Context, k, v string) error {
	if f.fail {
		return errors.New("đĩa đầy")
	}
	return f.Settings.SetSetting(ctx, k, v)
}

// Bẫy 6: job xong mà không ghi được settings (máy đã chạy web server mới,
// settings còn ghi cũ): Prepare không từ chối, Plan hội tụ không gián đoạn.
func TestSwitchRerunAfterSettingsFailure(t *testing.T) {
	e := newSwitchEnv(t, Nginx, SourceUbuntu, switchSite(t, "a.example.com", true))
	sw := e.sw()
	sw.Settings = &brokenSettings{Settings: e.st, fail: true}
	p, err := sw.Prepare(ctx, Apache)
	if err != nil {
		t.Fatal(err)
	}
	rep, _, err := e.execute(t, p)
	if rep.Job.Status != engine.JobSucceeded || err == nil || !strings.Contains(err.Error(), "sudo tpan stack switch --to apache") || !strings.Contains(err.Error(), "apache2") {
		t.Fatalf("Execute: %v %s", err, rep.Job.Status)
	}
	if e.setting(t, SettingWeb) != "nginx" {
		t.Fatal("settings đã đổi dù SetSetting lỗi")
	}

	e.ResetRan()
	p, err = e.sw().Prepare(ctx, Apache)
	if err != nil {
		t.Fatalf("chạy lại bị từ chối: %v", err)
	}
	rep, _, err = e.execute(t, p)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy lại: %v %s %s%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	for _, c := range e.Ran() {
		if c.Name == "systemctl" && slices.Contains([]string{"stop", "start", "disable", "enable", "restart"}, c.Args[0]) {
			t.Errorf("chạy lại làm gián đoạn: %s", c.String())
		}
		if c.Name == "apt-get" {
			t.Errorf("chạy lại cài lại gói: %s", c.String())
		}
	}
	for _, r := range rep.Steps {
		if r.Outcome != "skipped" && !strings.HasPrefix(r.ID, "stack.switch.check:") {
			t.Errorf("chạy lại: bước %s %s", r.ID, r.Outcome)
		}
	}
	if e.setting(t, SettingWeb) != "apache" || e.setting(t, SettingWebSource) != "ubuntu" {
		t.Error("chạy lại không ghi settings")
	}
	if mf := e.managed(t); mf["/etc/apache2/sites-available/000-tpan-default.conf"] == "" {
		t.Error("chạy lại không ghi nhận file mới")
	}
}

// Chuyển đi rồi chuyển về trên cùng một máy (e2e bước 8): lần về, gói nginx
// đã cài sẵn, đang tắt và không bật khi khởi động (bước cài được bỏ qua);
// file của nginx được ghi lại từ template và ghi nhận lại vào managed_files.
func TestSwitchRoundTrip(t *testing.T) {
	e := newSwitchEnv(t, Nginx, SourceUbuntu, switchSite(t, "a.example.com", true), switchSite(t, "b.example.com", false))
	for _, to := range []WebServer{Apache, Nginx} {
		p, err := e.sw().Prepare(ctx, to)
		if err != nil {
			t.Fatalf("sang %s: Prepare: %v", to, err)
		}
		rep, _, err := e.execute(t, p)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("sang %s: %v %s %s%s", to, err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
		}
		if to == Nginx && rep.Steps[1].Outcome != "skipped" {
			t.Errorf("lần về: bước %s %s; gói đã cài sẵn phải được bỏ qua", rep.Steps[1].ID, rep.Steps[1].Outcome)
		}
	}
	if w, src := e.setting(t, SettingWeb), e.setting(t, SettingWebSource); w != "nginx" || src != "ubuntu" {
		t.Errorf("settings = %s %s", w, src)
	}
	for _, dom := range []string{"a.example.com", "b.example.com"} {
		if e.active() != "nginx" || e.siteCode(dom) != "200" {
			t.Errorf("%s không trả 200 qua nginx", dom)
		}
	}
	mf := e.managed(t)
	for _, path := range []string{"/etc/nginx/sites-available/tpan-default.conf", "/etc/nginx/sites-available/tpan-default-ssl.conf", site.LayoutOf(mustDomain(t, "a.example.com")).NginxConf} {
		if mf[path] == "" {
			t.Errorf("managed_files thiếu %s sau khi chuyển về", path)
		}
	}
	for path := range mf {
		if strings.HasPrefix(path, "/etc/apache2/") {
			t.Errorf("managed_files còn %s", path)
		}
	}
}
