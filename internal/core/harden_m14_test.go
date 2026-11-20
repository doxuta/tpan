package core

// Test của hai ca sử dụng M14 ở harden_m14.go (mục 6.13 của docs/KE-HOACH.md):
// Plan "Áp bản vá cô lập" và Plan đổi chế độ .htaccess, cộng chế độ "tat" khi
// chuyển web server và khi khôi phục từ thùng rác. Mọi thứ chạy trên máy giả
// của gói: site được tạo bằng Plan thật (fixEnv), rồi bị đưa về dạng mà bản
// tpan trước M14 để lại.

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// ---------------------------------------------------------------- giả lập

const (
	m14Blog = "blog.example.com"
	m14Shop = "shop.example.com"
	// m14Bad là một .htaccess có hai chỗ ngoài danh sách: SetHandler (lỗ L4) ở
	// dòng 3 và Options FollowSymLinks (lỗ L9) ở dòng 4.
	m14Bad = "# của một plugin cũ\nRewriteEngine On\nSetHandler \"proxy:unix:/run/php/x.sock|fcgi://localhost\"\nOptions +FollowSymLinks\n"
)

// m14Env là fixEnv (site tạo bằng Plan thật trên siteSim, có systemd giả) cộng
// phần máy mà Plan "Áp bản vá cô lập" và kiểm chứng của nó đụng tới: PHP-FPM
// 8.3 đang cài, WP-CLI, MariaDB chỉ nghe 127.0.0.1, `php-fpm8.3 -i` và
// `systemctl show` trả lời theo file ini và drop-in trên đĩa, bản in cấu hình
// của web server (`nginx -T`, `apache2ctl -S`), vhost mặc định từ chối tên lạ,
// và `find`, `head` dưới user của site cho phép quét .htaccess.
type m14Env struct {
	*fixEnv
	web WebServer

	lk sync.Mutex
	// protect: php8.3-fpm đã được khởi động lại sau khi có drop-in (systemd chỉ
	// dựng /proc riêng cho dịch vụ lúc nó khởi động).
	protect bool
	// catchAll: một vhost lạ hứng mọi tên miền (tên lạ nhận 200 thay vì bị từ chối).
	catchAll bool
	// down: site này trả 500.
	down map[string]bool
	// onWebReload được gọi mỗi lần web server reload.
	onWebReload func()
}

var (
	m14VhostOpenRE  = regexp.MustCompile(`^\s*<VirtualHost\s+\*:([0-9]+)>`)
	m14ServerNameRE = regexp.MustCompile(`^\s*ServerName\s+(\S+)`)
)

func newM14Env(t *testing.T, web WebServer, doms ...string) *m14Env {
	t.Helper()
	e := &m14Env{fixEnv: newFixEnv(t, web, doms...), web: web, down: map[string]bool{}}
	m := e.m
	oldWait := midCheckWait
	midCheckWait = 0
	t.Cleanup(func() { midCheckWait = oldWait })

	// Những gì `tpan stack install` để lại mà siteSim chưa có.
	for _, d := range []string{"/etc/php/8.3/fpm/conf.d", "/etc/systemd/system", "/etc/logrotate.d"} {
		m.SetDir(d, 0o755, system.Root)
	}
	m.SetFile("/usr/sbin/php-fpm8.3", []byte("ELF giả\n"), 0o755, system.Root)
	m.SetFile("/usr/local/bin/wp", fakePhar, 0o755, system.Root)
	m.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 60 << 30})

	out := func(s string) system.Result { return system.Result{Stdout: s} }
	dropIn := func() bool {
		b, _ := m.ReadFile(templates.PHPDropInPath("8.3"))
		return strings.Contains(string(b), "\nProtectProc=invisible\n")
	}
	m.Handle([]string{"systemctl", "show"}, func(c system.Cmd) system.Result {
		if !slices.Contains(c.Args, "ProtectProc") {
			return out("0\n") // MainPID: máy giả không có /proc của tiến trình
		}
		e.lk.Lock()
		defer e.lk.Unlock()
		if e.protect && dropIn() {
			return out("invisible\n")
		}
		return out("default\n")
	})
	// Khởi động lại PHP-FPM: mọi pool có socket (như fixEnv), và dịch vụ nhận
	// drop-in đang có trên đĩa.
	m.Handle([]string{"systemctl", "restart", "php8.3-fpm"}, func(system.Cmd) system.Result {
		names, _ := m.ReadDir("/etc/php/8.3/fpm/pool.d")
		for _, n := range names {
			if pool, ok := strings.CutSuffix(n, ".conf"); ok {
				m.SetSocket("/run/php/"+pool+"-8.3.sock", 0o660, system.Owner{User: "www-data", Group: "www-data"})
			}
		}
		e.lk.Lock()
		defer e.lk.Unlock()
		e.protect = dropIn()
		return system.Result{}
	})
	for _, unit := range []string{"nginx", "apache2"} {
		m.Handle([]string{"systemctl", "reload", unit}, func(system.Cmd) system.Result {
			e.lk.Lock()
			fn := e.onWebReload
			e.lk.Unlock()
			if fn != nil {
				fn()
			}
			return system.Result{}
		})
	}
	m.Handle([]string{"php-fpm8.3", "-i"}, func(system.Cmd) system.Result { return out(fakePHPInfo(m.FakeSystem, "8.3")) })
	m.Handle([]string{"ss"}, func(system.Cmd) system.Result { return out("LISTEN 0 80 127.0.0.1:3306 0.0.0.0:*\n") })
	// Chỉ câu của checks.MariaDBSecure: các test ở đây không khoá user DB.
	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		if strings.Contains(string(c.Stdin), "'root_socket'") {
			return out("root_socket\t1\nanonymous\t0\ntest_db\t0\n")
		}
		return system.Result{ExitCode: 1, Stderr: "m14Env: câu SQL lạ\n"}
	})

	// curl: tên lạ gọi vào 127.0.0.1 (http.default, web.default.rejects) bị
	// vhost mặc định từ chối; site tạm khoá trả 403 (suspendCurl); còn lại là
	// WordPress 200 khi vhost đang bật.
	suspendCurl(m.FakeSystem, func(c system.Cmd) system.Result {
		if slices.Contains(c.Args, "http://127.0.0.1/") {
			e.lk.Lock()
			catchAll := e.catchAll
			e.lk.Unlock()
			switch {
			case catchAll:
				return out("200")
			case web == Apache:
				return out("403")
			}
			return system.Result{ExitCode: 52, Stdout: "000", Stderr: "curl: (52) Empty reply from server\n"}
		}
		dom, _, _ := strings.Cut(argAfter(c.Args, "--resolve"), ":")
		e.lk.Lock()
		down := e.down[dom]
		e.lk.Unlock()
		if down {
			return out("<html>500</html>TPAN_HTTP_CODE=500")
		}
		return siteSimCurl(m)(c)
	})

	// `nginx -T`: mỗi file đang nạp mở đầu bằng "# configuration file <path>:".
	m.Handle([]string{"nginx", "-T"}, func(system.Cmd) system.Result {
		var b strings.Builder
		names, _ := m.ReadDir("/etc/nginx/sites-enabled")
		for _, n := range names {
			p := "/etc/nginx/sites-enabled/" + n
			if data, err := m.ReadFile(p); err == nil {
				b.WriteString("# configuration file " + p + ":\n" + string(data) + "\n")
			}
		}
		return out(b.String())
	})
	// `apache2ctl -S`: mỗi VirtualHost một dòng "port <n> namevhost <tên> (<file>:<dòng>)".
	m.Handle([]string{"apache2ctl", "-S"}, func(system.Cmd) system.Result {
		var b strings.Builder
		b.WriteString("VirtualHost configuration:\n*:80                   is a NameVirtualHost\n")
		names, _ := m.ReadDir("/etc/apache2/sites-enabled")
		for _, n := range names {
			file := "/etc/apache2/sites-enabled/" + n
			data, err := m.ReadFile(file)
			if err != nil {
				continue
			}
			port, at := "", ""
			for i, ln := range strings.Split(string(data), "\n") {
				if g := m14VhostOpenRE.FindStringSubmatch(ln); g != nil {
					port, at = g[1], file+":"+strconv.Itoa(i+1)
				} else if g := m14ServerNameRE.FindStringSubmatch(ln); g != nil && port != "" {
					b.WriteString("         port " + port + " namevhost " + g[1] + " (" + at + ")\n")
					port = ""
				}
			}
		}
		return out(b.String())
	})

	// checks.ScanHtaccess: `find <public> -name .htaccess ... -print0` rồi
	// `head -c <n> -- <file>`, cả hai dưới user của site. Các find khác giữ
	// nghĩa của siteSim (-delete là hoàn tác WPFiles).
	m.Handle([]string{"find"}, func(c system.Cmd) system.Result {
		if !slices.Contains(c.Args, "-print0") {
			if slices.Contains(c.Args, "-delete") {
				m.removeUnder(c.Args[0], false)
			}
			return system.Result{}
		}
		var found []string
		for p := range m.Snapshot() {
			if strings.HasPrefix(p, c.Args[0]+"/") && strings.HasSuffix(p, "/.htaccess") {
				found = append(found, p)
			}
		}
		slices.Sort(found)
		return out(strings.Join(found, "\x00") + "\x00")
	})
	m.Handle([]string{"head", "-c"}, func(c system.Cmd) system.Result {
		data, err := m.ReadFile(c.Args[len(c.Args)-1])
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "head: cannot open\n"}
		}
		return out(string(data))
	})
	return e
}

func (e *m14Env) security() *Security {
	return &Security{Sys: e.m, Journal: e.st, Settings: e.st, Sites: e.sites, WPCLISHA512: fakePharSHA()}
}

func (e *m14Env) layout(t *testing.T, dom string) site.Layout {
	t.Helper()
	return site.LayoutOf(mustDomain(t, dom))
}

func (e *m14Env) conf(t *testing.T, dom string) string {
	t.Helper()
	conf, _ := vhostPaths(e.web, e.layout(t, dom))
	return conf
}

func (e *m14Env) read(t *testing.T, p string) string {
	t.Helper()
	b, err := e.m.ReadFile(p)
	if err != nil {
		t.Fatalf("đọc %s: %v", p, err)
	}
	return string(b)
}

// oldLogs đưa log của site về dạng trước M14: thư mục root:root 0755,
// access.log do web server tự tạo (root:root 0644, đã có dòng), chưa có error.log.
func (e *m14Env) oldLogs(t *testing.T, dom string) {
	t.Helper()
	l := e.layout(t, dom)
	if err := errors.Join(e.m.Chown(l.LogDir, system.Root), e.m.Chmod(l.LogDir, 0o755), e.m.Remove(l.ErrorLog)); err != nil {
		t.Fatal(err)
	}
	e.m.SetFile(l.AccessLog, []byte("dòng log cũ\n"), 0o644, system.Root)
}

// setMode ghi chế độ .htaccess của site vào bảng sites.
func (e *m14Env) setMode(t *testing.T, dom string, mode site.HtaccessMode) {
	t.Helper()
	st := e.site(t, dom)
	st.Htaccess = mode
	e.setSite(t, st)
}

// makeOld đưa site về dạng bản tpan trước M14 để lại: vhost không có dòng
// chặn nào (Apache: AllowOverride All, không gỡ PHP_VALUE), log cũ, và cột
// htaccess là day-du như migration 1400 đặt cho site có sẵn.
func (e *m14Env) makeOld(t *testing.T, dom string) {
	t.Helper()
	l := e.layout(t, dom)
	conf := e.conf(t, dom)
	old := strings.ReplaceAll(e.read(t, conf), templates.NginxSymlinkGuard, "")
	if e.web == Apache {
		b, err := templates.ApacheSite(l, "8.3", acmeRoot, site.HtaccessFull)
		if err != nil {
			t.Fatal(err)
		}
		old = string(b)
		for _, g := range templates.ApacheFCGIGuards {
			old = strings.ReplaceAll(old, g, "")
		}
	}
	if old == e.read(t, conf) {
		t.Fatal("vhost mới không có dòng chặn để gỡ: template đã đổi?")
	}
	e.m.SetFile(conf, []byte(old), 0o644, system.Root)
	e.oldLogs(t, dom)
	e.setMode(t, dom, site.HtaccessFull)
}

// siteResults chạy bộ phép kiểm health của site, theo ID.
func (e *m14Env) siteResults(t *testing.T, dom string) map[string]checks.Result {
	t.Helper()
	cs, err := e.s.Checks(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	return resultsByID(checks.RunAll(ctx, e.m, cs))
}

// stackResults chạy bộ phép kiểm health của stack (có cả phép cấp máy của M14).
func (e *m14Env) stackResults(t *testing.T) map[string]checks.Result {
	t.Helper()
	cs, _, err := (&Stack{Sys: e.m, Settings: e.st, Sites: e.sites, WPCLISHA512: fakePharSHA()}).Checks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return resultsByID(checks.RunAll(ctx, e.m, cs))
}

// wantStatus đòi mỗi ID trong ids có đúng trạng thái want.
func wantStatus(t *testing.T, when string, got map[string]checks.Result, want checks.Status, ids ...string) {
	t.Helper()
	for _, id := range ids {
		r, ok := got[id]
		if !ok {
			t.Errorf("%s: không có phép kiểm %s", when, id)
			continue
		}
		if r.Status != want {
			t.Errorf("%s: %s = %s (%s), muốn %s", when, id, r.Status, r.Summary.Vi, want)
		}
	}
}

// ranCount đếm lệnh đã chạy có dạng in ra đúng bằng s.
func (e *m14Env) ranCount(s string) int {
	n := 0
	for _, c := range e.m.Ran() {
		if c.String() == s {
			n++
		}
	}
	return n
}

func (e *m14Env) webReload() string { return "systemctl reload " + webUnit(e.web) }

// hardenStepIDs là ID các bước của Plan "Áp bản vá cô lập" cho các site doms
// (đã xếp theo tên), đúng năm nhóm của hợp đồng.
func (e *m14Env) hardenStepIDs(t *testing.T, doms ...string) []string {
	t.Helper()
	want := []string{"php.harden.opcache:8.3", "php.harden.proc:8.3", "file:" + logrotateCnf}
	for _, dom := range doms {
		want = append(want, stepIDs(engine.Plan{Steps: steps.SiteLogs(e.layout(t, dom))})...)
	}
	for _, dom := range doms {
		conf, link := vhostPaths(e.web, e.layout(t, dom))
		want = append(want, "file:"+conf, "symlink:"+link)
	}
	return append(want, "service.reload:"+webUnit(e.web))
}

// execHarden chạy Plan và đòi job thành công.
func execHarden(t *testing.T, h *Harden) engine.Report {
	t.Helper()
	rep, err := h.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %v %s (%s); kiểm %s", h.Plan.Kind, err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	return rep
}

// allSkipped đòi mọi bước của job được bỏ qua và không lệnh nào nạp lại hay
// khởi động lại dịch vụ.
func (e *m14Env) allSkipped(t *testing.T, rep engine.Report) {
	t.Helper()
	for _, sr := range rep.Steps {
		if sr.Outcome != "skipped" {
			t.Errorf("chạy lại: bước %s %s, muốn skipped", sr.ID, sr.Outcome)
		}
	}
	for _, c := range e.m.Ran() {
		if c.Name == "systemctl" && slices.Contains([]string{"reload", "restart", "daemon-reload"}, c.Args[0]) {
			t.Errorf("chạy lại mà vẫn chạy %s", c.String())
		}
	}
}

// ---------------------------------------------------------------- harden

// Máy "cũ" (vhost không có dòng chặn, log root:root 0755, chưa có file ini và
// drop-in, site ở day-du): một lần harden đưa mọi phép kiểm M14 về đạt, lưu
// an-toan, reload web server đúng một lần; lần hai không đổi gì.
func TestHardenOldMachine(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newM14Env(t, web, m14Blog, m14Shop)
			doms := []string{m14Blog, m14Shop}
			for _, dom := range doms {
				e.makeOld(t, dom)
			}
			machine := []string{"php.opcache:8.3", "web.server_name.unique", "web.default.rejects"}
			perSite := func(dom string) []string {
				ids := []string{"log.perm:" + dom, "web.hardened:" + dom}
				for _, o := range doms {
					if o != dom {
						ids = append(ids, "isolation:"+dom+":"+o, "isolation:"+o+":"+dom)
					}
				}
				return ids
			}
			// Trước: đúng là máy cũ.
			wantStatus(t, "trước", e.stackResults(t), checks.Fail, "php.opcache:8.3")
			wantStatus(t, "trước", e.stackResults(t), checks.Warn, "php.protectproc:8.3")
			for _, dom := range doms {
				wantStatus(t, "trước", e.siteResults(t, dom), checks.Fail, "log.perm:"+dom, "web.hardened:"+dom)
			}

			before := e.m.Snapshot()
			h, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatalf("PrepareHarden: %v", err)
			}
			if h.Plan.Kind != "security.harden" || h.Plan.Target != "isolation" || h.Plan.Title.Vi != "Áp bản vá cô lập" || h.Plan.Title.En == "" {
				t.Errorf("Plan = %s %s %q", h.Plan.Kind, h.Plan.Target, h.Plan.Title.Vi)
			}
			if !slices.Equal(h.Switched, doms) || len(h.Kept) != 0 {
				t.Errorf("Switched %v, Kept %v; muốn chuyển cả hai site", h.Switched, h.Kept)
			}
			if got, want := stepIDs(h.Plan), e.hardenStepIDs(t, doms...); !slices.Equal(got, want) {
				t.Errorf("bước:\n có   %q\n muốn %q", got, want)
			}
			pv, err := h.Preview(ctx)
			if err != nil || len(pv.Steps) != len(h.Plan.Steps) {
				t.Fatalf("Preview: %v", err)
			}
			if !mapsEqual(before, e.m.Snapshot()) {
				t.Error("PrepareHarden hoặc Preview làm đổi hệ thống file")
			}
			for _, dom := range doms {
				if e.site(t, dom).Htaccess != site.HtaccessFull {
					t.Errorf("%s: bảng sites đổi trước khi job chạy", dom)
				}
			}

			e.m.ResetRan()
			execHarden(t, h)
			// Reload gom một lần; PHP-FPM restart một lần (drop-in ProtectProc).
			if n := e.ranCount(e.webReload()); n != 1 {
				t.Errorf("%s chạy %d lần, muốn 1", e.webReload(), n)
			}
			if n := e.ranCount("systemctl restart php8.3-fpm"); n != 1 {
				t.Errorf("restart php8.3-fpm chạy %d lần, muốn 1", n)
			}

			stack := e.stackResults(t)
			wantStatus(t, "sau", stack, checks.Pass, append(machine, "php.protectproc:8.3")...)
			mf := e.managedSHA(t)
			for _, dom := range doms {
				ids := perSite(dom)
				if web == Apache {
					ids = append(ids, "web.htaccess:"+dom)
				}
				wantStatus(t, "sau", e.siteResults(t, dom), checks.Pass, ids...)
				if got := e.site(t, dom).Htaccess; got != site.HtaccessSafe {
					t.Errorf("%s: bảng sites ghi %q, muốn an-toan", dom, got)
				}
				l := e.layout(t, dom)
				// Sửa quyền không được xoá log đang có.
				if got := e.read(t, l.AccessLog); got != "dòng log cũ\n" {
					t.Errorf("%s: access.log sau harden: %q", dom, got)
				}
				// Vhost vừa ghi phải vào sổ managed_files để phép kiểm lệch so đúng bản mới.
				if conf := e.conf(t, dom); mf[conf] != shaHex([]byte(e.read(t, conf))) {
					t.Errorf("%s: managed_files chưa ghi vhost mới", dom)
				}
			}
			if lr, _ := logrotateStep(web); e.read(t, logrotateCnf) != string(lr.Content) {
				t.Error("file logrotate chưa được ghi")
			}

			// Lần hai: không còn gì để làm.
			h2, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatalf("PrepareHarden lần hai: %v", err)
			}
			if len(h2.Switched) != 0 || len(h2.Kept) != 0 {
				t.Errorf("lần hai: Switched %v, Kept %v; muốn rỗng", h2.Switched, h2.Kept)
			}
			if h2.Switched == nil || h2.Kept == nil {
				t.Error("Switched, Kept rỗng phải là [] chứ không phải null trong JSON")
			}
			e.m.ResetRan()
			e.allSkipped(t, execHarden(t, h2))
		})
	}
}

// Site có .htaccess chứa SetHandler nằm trong Kept, giữ day-du; vhost của nó
// vẫn được ghi lại (hai dòng ProxyFCGISetEnvIf, AllowOverride All). Trên nginx
// vẫn quét: cột htaccess được dùng khi máy chuyển sang Apache.
func TestHardenKeepsBlockedSite(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newM14Env(t, web, m14Blog, m14Shop)
			e.makeOld(t, m14Blog)
			e.makeOld(t, m14Shop)
			ht := e.layout(t, m14Blog).Public + "/.htaccess"
			own := e.layout(t, m14Blog).User
			e.m.SetFile(ht, []byte(m14Bad), 0o640, system.Owner{User: own, Group: site.WebGroup})
			// .htaccess dưới wp-content/uploads không tính: vhost đặt AllowOverride None ở đó.
			e.m.SetFile(e.layout(t, m14Shop).Public+"/wp-content/uploads/.htaccess", []byte("SetHandler none\n"), 0o640, system.Root)

			e.m.ResetRan()
			h, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatalf("PrepareHarden: %v", err)
			}
			wantKept := []HtaccessKept{{Domain: m14Blog, Findings: []htaccess.Finding{
				{File: ht, Line: 3, Directive: "SetHandler"},
				{File: ht, Line: 4, Directive: "Options FollowSymLinks"},
			}}}
			if !slices.Equal(h.Switched, []string{m14Shop}) || len(h.Kept) != 1 ||
				h.Kept[0].Domain != m14Blog || !slices.Equal(h.Kept[0].Findings, wantKept[0].Findings) {
				t.Fatalf("Switched %v, Kept %+v; muốn chuyển shop, giữ blog với %+v", h.Switched, h.Kept, wantKept)
			}
			// Quét dưới user của site, không bằng root (user của site đặt được symlink).
			for _, c := range e.m.Ran() {
				if (c.Name == "find" || c.Name == "head") && !strings.HasPrefix(c.AsUser, "tp_") {
					t.Errorf("%s chạy dưới %q, muốn user của site", c.String(), c.AsUser)
				}
			}
			// Vhost của site bị giữ vẫn nằm trong Plan.
			if got, want := stepIDs(h.Plan), e.hardenStepIDs(t, m14Blog, m14Shop); !slices.Equal(got, want) {
				t.Errorf("bước:\n có   %q\n muốn %q", got, want)
			}
			execHarden(t, h)

			if got := e.site(t, m14Blog).Htaccess; got != site.HtaccessFull {
				t.Errorf("blog: bảng sites ghi %q, muốn giữ day-du", got)
			}
			if got := e.site(t, m14Shop).Htaccess; got != site.HtaccessSafe {
				t.Errorf("shop: bảng sites ghi %q, muốn an-toan", got)
			}
			if e.read(t, ht) != m14Bad {
				t.Error("harden đã sửa .htaccess của site")
			}
			wantStatus(t, "blog", e.siteResults(t, m14Blog), checks.Pass, "log.perm:"+m14Blog, "web.hardened:"+m14Blog)
			wantStatus(t, "shop", e.siteResults(t, m14Shop), checks.Pass, "log.perm:"+m14Shop, "web.hardened:"+m14Shop)
			if web == Apache {
				blog, shop := e.read(t, e.conf(t, m14Blog)), e.read(t, e.conf(t, m14Shop))
				for _, g := range templates.ApacheFCGIGuards {
					if !strings.Contains(blog, g) {
						t.Errorf("vhost của blog thiếu dòng %q", g)
					}
				}
				if !strings.Contains(blog, "AllowOverride All") || strings.Contains(blog, "AllowOverrideList") {
					t.Errorf("vhost của blog không còn là bản day-du:\n%s", blog)
				}
				if !strings.Contains(shop, "AllowOverrideList "+htaccess.List()) || strings.Contains(shop, "AllowOverride All") {
					t.Errorf("vhost của shop không phải bản an-toan:\n%s", shop)
				}
				// Site bị giữ vẫn mang cảnh báo; site đã chuyển thì đạt.
				wantStatus(t, "blog", e.siteResults(t, m14Blog), checks.Warn, "web.htaccess:"+m14Blog)
				wantStatus(t, "shop", e.siteResults(t, m14Shop), checks.Pass, "web.htaccess:"+m14Shop)
			}

			// Lần hai: blog vẫn bị giữ và được báo lại, không bước nào chạy.
			h2, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(h2.Switched) != 0 || len(h2.Kept) != 1 || h2.Kept[0].Domain != m14Blog {
				t.Errorf("lần hai: Switched %v, Kept %+v", h2.Switched, h2.Kept)
			}
			e.m.ResetRan()
			e.allSkipped(t, execHarden(t, h2))
		})
	}
}

// Không quét được .htaccess của một site (find lỗi): site đó được giữ ở
// day-du kèm lý do, các site khác vẫn được vá.
func TestHardenKeepsUnscannedSite(t *testing.T) {
	e := newM14Env(t, Apache, m14Blog, m14Shop)
	e.makeOld(t, m14Blog)
	e.makeOld(t, m14Shop)
	blog := e.layout(t, m14Blog)
	e.m.Handle([]string{"find", blog.Public}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "find: ‘" + blog.Public + "/x’: Permission denied\n"}
	})
	h, err := e.security().PrepareHarden(ctx)
	if err != nil {
		t.Fatalf("PrepareHarden: %v", err)
	}
	if !slices.Equal(h.Switched, []string{m14Shop}) || len(h.Kept) != 1 || h.Kept[0].Domain != m14Blog {
		t.Fatalf("Switched %v, Kept %+v", h.Switched, h.Kept)
	}
	if f := h.Kept[0].Findings; len(f) != 1 || f[0].File != blog.Public || !strings.HasPrefix(f[0].Directive, hardenUnscanned) {
		t.Errorf("Findings %+v; muốn một dòng %q", f, hardenUnscanned)
	}
	execHarden(t, h)
	if got := e.site(t, m14Blog).Htaccess; got != site.HtaccessFull {
		t.Errorf("blog: %q, muốn giữ day-du", got)
	}
}

// Site tạm khoá: vẫn đặt lại quyền log và ghi lại vhost bản tạm khoá; không
// quét, không đổi chế độ.
func TestHardenSuspendedSite(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newM14Env(t, web, m14Blog, m14Shop)
			planned(t)(e.s.PrepareSuspend(ctx, mustDomain(t, m14Blog)))
			blog := e.layout(t, m14Blog)
			conf := e.conf(t, m14Blog)
			// Vhost tạm khoá do bản cũ ghi, khác bản hiện tại một dòng chú thích.
			e.m.SetFile(conf, []byte(e.read(t, conf)+"# bản cũ\n"), 0o644, system.Root)
			e.oldLogs(t, m14Blog)
			e.setMode(t, m14Blog, site.HtaccessFull)
			e.m.SetFile(blog.Public+"/.htaccess", []byte(m14Bad), 0o640, system.Root)
			e.makeOld(t, m14Shop)

			e.m.ResetRan()
			h, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatalf("PrepareHarden: %v", err)
			}
			if !slices.Equal(h.Switched, []string{m14Shop}) || len(h.Kept) != 0 {
				t.Errorf("Switched %v, Kept %+v; site tạm khoá không được nằm ở đâu cả", h.Switched, h.Kept)
			}
			for _, c := range e.m.Ran() {
				if c.Name == "find" && slices.Contains(c.Args, blog.Public) {
					t.Errorf("quét .htaccess của site tạm khoá: %s", c.String())
				}
			}
			if got, want := stepIDs(h.Plan), e.hardenStepIDs(t, m14Blog, m14Shop); !slices.Equal(got, want) {
				t.Errorf("bước:\n có   %q\n muốn %q", got, want)
			}
			execHarden(t, h)

			vh := e.read(t, conf)
			if !strings.Contains(vh, templates.SuspendedURI) || strings.Contains(vh, "# bản cũ") || strings.Contains(vh, blog.Public) {
				t.Errorf("vhost của site tạm khoá không phải bản tạm khoá hiện tại:\n%s", vh)
			}
			st := e.site(t, m14Blog)
			if st.Htaccess != site.HtaccessFull || st.Suspended != account.SuspendSelf {
				t.Errorf("site tạm khoá: htaccess %q, suspended %q; muốn giữ nguyên", st.Htaccess, st.Suspended)
			}
			if _, err := e.m.Stat(blog.PoolConf("8.3")); err == nil {
				t.Error("harden ghi lại pool của site tạm khoá (mở khoá lén)")
			}
			got := e.siteResults(t, m14Blog)
			wantStatus(t, "site tạm khoá", got, checks.Pass, "log.perm:"+m14Blog, "http.suspended:"+m14Blog)
			wantStatus(t, "shop", e.siteResults(t, m14Shop), checks.Pass, "log.perm:"+m14Shop, "web.hardened:"+m14Shop)
		})
	}
}

// Job lỗi thì máy về như cũ. Lỗi trước khi reload: web server không bị đụng.
// Lỗi ở kiểm chứng, sau khi đã reload: file cũ về rồi web server được nạp lại
// một lần nữa, để thứ đang chạy khớp với thứ trên đĩa.
func TestHardenRollback(t *testing.T) {
	for _, tc := range []struct {
		name string
		// brk làm hỏng máy trước khi chạy.
		brk         func(t *testing.T, e *m14Env)
		wantReloads int
	}{
		{"ghi vhost lỗi giữa chừng", func(t *testing.T, e *m14Env) {
			e.m.Fail("write", e.conf(t, m14Shop), errors.New("đĩa đầy"))
		}, 0},
		{"kiểm chứng lỗi sau khi đã reload", func(_ *testing.T, e *m14Env) {
			e.lk.Lock()
			e.catchAll = true
			e.lk.Unlock()
		}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newM14Env(t, Nginx, m14Blog, m14Shop)
			e.makeOld(t, m14Blog)
			e.makeOld(t, m14Shop)
			blogConf := e.conf(t, m14Blog)
			// Nội dung vhost của blog lúc web server nạp lại, theo từng lần.
			var loaded []bool
			e.onWebReload = func() {
				b, _ := e.m.ReadFile(blogConf)
				loaded = append(loaded, strings.Contains(string(b), templates.NginxSymlinkGuard))
			}
			before := e.m.Snapshot()
			h, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatalf("PrepareHarden: %v", err)
			}
			tc.brk(t, e)
			e.m.ResetRan()
			rep, err := h.Execute(ctx, nil)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if rep.Job.Status != engine.JobRolledBack {
				t.Fatalf("job = %s (%s); muốn rolled_back", rep.Job.Status, rep.Job.Error)
			}
			after := e.m.Snapshot()
			if !mapsEqual(before, after) {
				for p, b := range before {
					if a, ok := after[p]; !ok || a != b {
						t.Errorf("%s khác sau hoàn tác (còn: %v)", p, ok)
					}
				}
				for p := range after {
					if _, ok := before[p]; !ok {
						t.Errorf("%s còn lại sau hoàn tác", p)
					}
				}
			}
			if n := e.ranCount(e.webReload()); n != tc.wantReloads {
				t.Errorf("%s chạy %d lần, muốn %d", e.webReload(), n, tc.wantReloads)
			}
			// Lần nạp đầu là vhost mới, lần nạp lúc hoàn tác là vhost cũ.
			if tc.wantReloads == 2 && !slices.Equal(loaded, []bool{true, false}) {
				t.Errorf("vhost lúc nạp (có dòng chặn?) = %v, muốn [true false]", loaded)
			}
			// PHP-FPM về lại không có drop-in, và đã được khởi động lại để bỏ nó.
			wantStatus(t, "sau hoàn tác", e.stackResults(t), checks.Warn, "php.protectproc:8.3")
			wantStatus(t, "sau hoàn tác", e.stackResults(t), checks.Fail, "php.opcache:8.3")
			for _, dom := range []string{m14Blog, m14Shop} {
				if got := e.site(t, dom).Htaccess; got != site.HtaccessFull {
					t.Errorf("%s: job hỏng mà bảng sites ghi %q", dom, got)
				}
			}
		})
	}
}

func TestHardenRefusals(t *testing.T) {
	// Stack chưa cài: từ chối như các ca sử dụng khác.
	se := newSiteEnv(t, "")
	if _, err := (&Security{Sys: se.m, Settings: se.st, Sites: se.sites}).PrepareHarden(ctx); refusedCode(t, err) != "stack_not_installed" {
		t.Error("stack chưa cài: muốn stack_not_installed")
	}
	// Thiếu Settings hay Sites là lỗi của người gọi, không phải lời từ chối.
	for _, s := range []*Security{{Sys: se.m, Sites: se.sites}, {Sys: se.m, Settings: se.st}} {
		var re *RefusedError
		if h, err := s.PrepareHarden(ctx); err == nil || errors.As(err, &re) || h != nil {
			t.Errorf("thiếu Settings hoặc Sites: h=%v err=%v; muốn lỗi thường", h, err)
		}
	}
	// Máy chưa có site nào: Plan vẫn có phần cấp máy, không có bước reload web.
	e := newM14Env(t, Nginx)
	h, err := e.security().PrepareHarden(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(h.Plan), []string{"php.harden.opcache:8.3", "php.harden.proc:8.3", "file:" + logrotateCnf}; !slices.Equal(got, want) {
		t.Errorf("bước %q, muốn %q", got, want)
	}
	execHarden(t, h)
}

// ---------------------------------------------------------------- .htaccess

func TestHtaccessRefusals(t *testing.T) {
	d := mustDomain(t, m14Shop)

	// Stack nginx: cài đặt này chưa có tác dụng.
	n := newM14Env(t, Nginx, m14Shop)
	if _, err := n.s.PrepareHtaccess(ctx, d, site.HtaccessFull); refusedCode(t, err) != CodeHtaccessNotApache {
		t.Error("stack nginx: muốn htaccess_not_apache")
	}

	e := newM14Env(t, Apache, m14Shop, m14Blog)
	if _, err := e.s.PrepareHtaccess(ctx, mustDomain(t, "none.example.com"), site.HtaccessFull); refusedCode(t, err) != "site_not_found" {
		t.Error("site không có: muốn site_not_found")
	}
	// Site mới ở an-toan; bản ghi cũ có cột rỗng cũng hiểu là an-toan.
	if _, err := e.s.PrepareHtaccess(ctx, d, site.HtaccessSafe); refusedCode(t, err) != CodeHtaccessSame {
		t.Error("cùng chế độ: muốn htaccess_same")
	}
	e.setMode(t, m14Shop, "")
	if _, err := e.s.PrepareHtaccess(ctx, d, site.HtaccessSafe); refusedCode(t, err) != CodeHtaccessSame {
		t.Error("cột rỗng là an-toan: muốn htaccess_same")
	}
	// Chế độ lạ: lỗi, không Plan nào được dựng.
	for _, bad := range []site.HtaccessMode{"", "All", "day-du; rm -rf /", "an-toan\n"} {
		if p, err := e.s.PrepareHtaccess(ctx, d, bad); err == nil || p != nil {
			t.Errorf("chế độ %q: muốn lỗi", bad)
		}
	}

	// Site có chỉ thị ngoài danh sách: không chuyển sang an-toan được.
	shop := e.layout(t, m14Shop)
	ht := shop.Public + "/.htaccess"
	e.setMode(t, m14Shop, site.HtaccessFull)
	e.m.SetFile(ht, []byte(m14Bad), 0o640, system.Owner{User: shop.User, Group: site.WebGroup})
	before := e.m.Snapshot()
	_, err := e.s.PrepareHtaccess(ctx, d, site.HtaccessSafe)
	if refusedCode(t, err) != CodeHtaccessBlocked {
		t.Fatal("muốn htaccess_blocked")
	}
	var re *RefusedError
	errors.As(err, &re)
	for _, msg := range []string{re.Message.Vi, re.Message.En} {
		// Từng dòng "<file>:<dòng>: <chỉ thị>", rồi hai lối ra.
		lines := strings.Split(msg, "\n")
		for _, want := range []string{ht + ":3: SetHandler", ht + ":4: Options FollowSymLinks"} {
			if !slices.Contains(lines, want) {
				t.Errorf("lời từ chối thiếu dòng %q:\n%s", want, msg)
			}
		}
		if !strings.Contains(msg, "sudo tpan site set "+m14Shop+" --htaccess day-du") || !strings.Contains(msg, "(1)") || !strings.Contains(msg, "(2)") {
			t.Errorf("lời từ chối thiếu hai lối ra:\n%s", msg)
		}
	}
	if !mapsEqual(before, e.m.Snapshot()) {
		t.Error("Prepare bị từ chối mà hệ thống file đổi")
	}
	// Sang tat thì không quét: Apache thôi đọc file đó.
	if _, err := e.s.PrepareHtaccess(ctx, d, site.HtaccessOff); err != nil {
		t.Errorf("day-du sang tat với .htaccess xấu: %v", err)
	}

	// File không đọc được dưới user của site cũng chặn, và tên file có ký tự
	// điều khiển không được đi nguyên vào lời từ chối.
	e.m.Handle([]string{"head", "-c"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "head: Permission denied\n"}
	})
	evil := shop.Public + "/a\x1b[31m/.htaccess"
	e.m.SetFile(evil, []byte("x\n"), 0o600, system.Root)
	_, err = e.s.PrepareHtaccess(ctx, d, site.HtaccessSafe)
	if refusedCode(t, err) != CodeHtaccessBlocked {
		t.Fatal("file không đọc được: muốn htaccess_blocked")
	}
	errors.As(err, &re)
	if strings.ContainsRune(re.Message.Vi+re.Message.En, 0x1b) || !strings.Contains(re.Message.Vi, "(không đọc được)") || !strings.Contains(re.Message.En, "(unreadable)") {
		t.Errorf("lời từ chối cho file không đọc được:\n%q\n%q", re.Message.Vi, re.Message.En)
	}

	// Site tạm khoá.
	planned(t)(e.s.PrepareSuspend(ctx, mustDomain(t, m14Blog)))
	if _, err := e.s.PrepareHtaccess(ctx, mustDomain(t, m14Blog), site.HtaccessFull); refusedCode(t, err) != CodeSiteSuspended {
		t.Error("site tạm khoá: muốn site_suspended")
	}
	// Site trong thùng rác.
	st := e.site(t, m14Blog)
	st.Status = site.Trashed
	e.setSite(t, st)
	if _, err := e.s.PrepareHtaccess(ctx, mustDomain(t, m14Blog), site.HtaccessFull); refusedCode(t, err) != "site_not_active" {
		t.Error("site trong thùng rác: muốn site_not_active")
	}
}

// Từng chuyển đổi: đúng một đường ghi vhost, vhost đúng khối của chế độ mới,
// bảng sites lưu chế độ mới, và .htaccess được ghi lại khi rời tat mà thiếu.
func TestHtaccessTransitions(t *testing.T) {
	e := newM14Env(t, Apache, m14Shop)
	d := mustDomain(t, m14Shop)
	l := e.layout(t, m14Shop)
	conf, ht := l.ApacheConf, l.Public+"/.htaccess"
	htStep := "file:" + ht
	const hardened, htaccessID = "web.hardened:" + m14Shop, "web.htaccess:" + m14Shop
	wpHt := string(templates.WPHtaccess())
	if e.read(t, ht) != wpHt || e.site(t, m14Shop).Htaccess != site.HtaccessSafe {
		t.Fatal("site mới trên Apache phải ở an-toan và có .htaccess chuẩn")
	}

	for _, tc := range []struct {
		name string
		to   site.HtaccessMode
		// dropHt xoá .htaccess trước khi chuyển; wantHt: Plan có bước ghi lại nó.
		dropHt, wantHt bool
		// has, hasNot là các đoạn vhost phải có và không được có.
		has, hasNot []string
	}{
		{"an-toan sang day-du", site.HtaccessFull, false, false,
			[]string{"AllowOverride All"}, []string{"AllowOverrideList", "FallbackResource"}},
		{"day-du sang an-toan", site.HtaccessSafe, false, false,
			[]string{"AllowOverride " + htaccess.OverrideOptions(), "AllowOverrideList " + htaccess.List()}, []string{"AllowOverride All", "FallbackResource"}},
		{"an-toan sang tat", site.HtaccessOff, false, false,
			[]string{"FallbackResource /index.php"}, []string{"AllowOverride All", "AllowOverrideList"}},
		{"tat sang an-toan, .htaccess còn", site.HtaccessSafe, false, false,
			[]string{"AllowOverrideList " + htaccess.List()}, []string{"FallbackResource"}},
		{"an-toan sang tat lần nữa", site.HtaccessOff, false, false,
			[]string{"FallbackResource /index.php"}, nil},
		{"tat sang an-toan, .htaccess thiếu", site.HtaccessSafe, true, true,
			[]string{"AllowOverrideList " + htaccess.List()}, []string{"FallbackResource"}},
		{"an-toan sang tat lần ba", site.HtaccessOff, false, false,
			[]string{"FallbackResource /index.php"}, nil},
		{"tat sang day-du, .htaccess thiếu", site.HtaccessFull, true, true,
			[]string{"AllowOverride All"}, []string{"FallbackResource"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from := e.site(t, m14Shop).Htaccess
			if tc.dropHt {
				if err := e.m.Remove(ht); err != nil {
					t.Fatal(err)
				}
			}
			htBefore, _ := e.m.ReadFile(ht)
			p, err := e.s.PrepareHtaccess(ctx, d, tc.to)
			if err != nil {
				t.Fatalf("PrepareHtaccess: %v", err)
			}
			if p.Plan.Kind != "site.htaccess" || p.Plan.Target != m14Shop || !strings.Contains(p.Plan.Title.Vi, string(from)) || !strings.Contains(p.Plan.Title.Vi, string(tc.to)) {
				t.Errorf("Plan = %s %s %q", p.Plan.Kind, p.Plan.Target, p.Plan.Title.Vi)
			}
			want := []string{"file:" + conf, "symlink:" + l.ApacheLink, "site.htaccess.check:" + m14Shop}
			if tc.wantHt {
				want = append([]string{htStep}, want...)
			}
			if got := stepIDs(p.Plan); !slices.Equal(got, want) {
				t.Errorf("bước %q, muốn %q", got, want)
			}
			ids := checkIDs(t, e.m, p.Plan.Verify)
			for _, id := range []string{hardened, "config.apache:apache2", "http.site:" + m14Shop} {
				if countOf(ids, id) != 1 {
					t.Errorf("Verify %v thiếu %s", ids, id)
				}
			}
			// Ở day-du phép web.htaccess luôn là cảnh báo: không nằm trong kiểm chứng.
			if has := slices.Contains(ids, htaccessID); has != (tc.to != site.HtaccessFull) {
				t.Errorf("Verify %v: có %s = %v", ids, htaccessID, has)
			}

			e.m.ResetRan()
			runPlanned(t, p, nil)
			if n := e.ranCount("systemctl reload apache2"); n != 1 {
				t.Errorf("reload apache2 chạy %d lần, muốn 1", n)
			}
			vh := e.read(t, conf)
			for _, s := range append(slices.Clone(tc.has), templates.ApacheFCGIGuards...) {
				if !strings.Contains(vh, s) {
					t.Errorf("vhost thiếu %q", s)
				}
			}
			for _, s := range tc.hasNot {
				if strings.Contains(vh, s) {
					t.Errorf("vhost không được có %q", s)
				}
			}
			if got := e.site(t, m14Shop).Htaccess; got != tc.to {
				t.Errorf("bảng sites ghi %q, muốn %q", got, tc.to)
			}
			switch {
			case tc.wantHt:
				if e.read(t, ht) != wpHt {
					t.Error("rời tat mà .htaccess chưa được ghi lại bản chuẩn")
				}
			case string(htBefore) != e.read(t, ht):
				// Sang tat không xoá, và không chuyển đổi nào sửa file đang có.
				t.Error(".htaccess đang có bị đổi")
			}
			wantStatus(t, "sau", e.siteResults(t, m14Shop), checks.Pass, hardened)
			// Chạy lại cùng chế độ thì bị từ chối: không có gì để đổi.
			if _, err := e.s.PrepareHtaccess(ctx, d, tc.to); refusedCode(t, err) != CodeHtaccessSame {
				t.Error("chạy lại: muốn htaccess_same")
			}
		})
	}
}

// Site có SSL: vẫn đúng một bước ghi vhost, và là bản có cổng 443 theo chế độ
// mới (ghi bản cổng 80 là đè mất HTTPS của site).
func TestHtaccessSSLVhost(t *testing.T) {
	e := newM14Env(t, Apache, m14Shop)
	st := e.site(t, m14Shop)
	st.SSL = true
	e.setSite(t, st)
	l := e.layout(t, m14Shop)
	p, err := e.s.PrepareHtaccess(ctx, mustDomain(t, m14Shop), site.HtaccessFull)
	if err != nil {
		t.Fatal(err)
	}
	want, err := templates.ApacheSiteSSL(l, "8.3", acmeRoot, site.HtaccessFull)
	if err != nil {
		t.Fatal(err)
	}
	fs := files(p.Plan.Steps)
	if len(fs) != 1 || fs[0].Path != l.ApacheConf || string(fs[0].Content) != string(want) {
		t.Errorf("bước ghi file = %d bước; muốn đúng một vhost 443 ở chế độ day-du", len(fs))
	}
	if !strings.Contains(string(want), ":443") || !strings.Contains(string(want), "AllowOverride All") {
		t.Error("bản SSL của template không có cổng 443 hoặc AllowOverride All: test không còn đúng")
	}
	if ids := checkIDs(t, e.m, p.Plan.Verify); !slices.Contains(ids, "https.site:"+m14Shop) || slices.Contains(ids, "http.site:"+m14Shop) {
		t.Errorf("Verify %v: site có SSL phải kiểm qua https", ids)
	}
}

// Site không trả lời đúng sau khi đổi chế độ: job dừng ở bước kiểm giữa chừng
// và hoàn tác, vhost về chế độ cũ, bảng sites không đổi.
func TestHtaccessRollback(t *testing.T) {
	e := newM14Env(t, Apache, m14Shop)
	conf := e.conf(t, m14Shop)
	before := e.m.Snapshot()
	p, err := e.s.PrepareHtaccess(ctx, mustDomain(t, m14Shop), site.HtaccessFull)
	if err != nil {
		t.Fatal(err)
	}
	e.lk.Lock()
	e.down[m14Shop] = true
	e.lk.Unlock()
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobRolledBack {
		t.Fatalf("job %s %v; muốn rolled_back", rep.Job.Status, err)
	}
	if !strings.Contains(rep.Job.Error, "site.htaccess.check:"+m14Shop) {
		t.Errorf("lỗi không nêu bước kiểm giữa chừng: %s", rep.Job.Error)
	}
	if !mapsEqual(before, e.m.Snapshot()) {
		t.Errorf("hệ thống file khác sau hoàn tác; vhost:\n%s", e.read(t, conf))
	}
	if got := e.site(t, m14Shop).Htaccess; got != site.HtaccessSafe {
		t.Errorf("job hỏng mà bảng sites ghi %q", got)
	}
}

// ---------------------------------------------------------------- chế độ "tat"

// Chuyển nginx sang Apache: site ở chế độ tat không được ghi .htaccess (Apache
// không đọc nó, permalink nằm trong vhost); site khác vẫn được ghi.
func TestSwitchSkipsHtaccessForOffMode(t *testing.T) {
	off := switchSite(t, "tat.example.com", false)
	off.Htaccess = site.HtaccessOff
	safe := switchSite(t, "antoan.example.com", false)
	safe.Htaccess = site.HtaccessSafe
	e := newSwitchEnv(t, Nginx, SourceUbuntu, off, safe)
	p, err := e.sw().Prepare(ctx, Apache)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	offHt, safeHt := off.Layout().Public+"/.htaccess", safe.Layout().Public+"/.htaccess"
	ids := stepIDs(p.Plan)
	if slices.Contains(ids, "file:"+offHt) || !slices.Contains(ids, "file:"+safeHt) {
		t.Errorf("bước ghi .htaccess sai: tat=%v an-toan=%v", slices.Contains(ids, "file:"+offHt), slices.Contains(ids, "file:"+safeHt))
	}
	rep, _, err := e.execute(t, p)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chuyển: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if _, err := e.Stat(offHt); err == nil {
		t.Error("site ở chế độ tat có .htaccess sau khi chuyển sang Apache")
	}
	if _, err := e.Stat(safeHt); err != nil {
		t.Errorf("site ở chế độ an-toan thiếu .htaccess: %v", err)
	}
	vh, _ := e.ReadFile(off.Layout().ApacheConf)
	if !strings.Contains(string(vh), "FallbackResource /index.php") {
		t.Errorf("vhost Apache của site tat thiếu FallbackResource:\n%s", vh)
	}
}

// Khôi phục trên Apache một site ở chế độ tat (vào thùng rác lúc còn nginx nên
// chưa có .htaccess): không ghi .htaccess.
func TestRestoreSkipsHtaccessForOffMode(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	se.trash(t, dom)
	mustSet(t, se.st, SettingWeb, string(Apache))
	st := se.siteOf(t, dom)
	st.Htaccess = site.HtaccessOff
	if err := se.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}

	rs, err := se.s.PrepareRestore(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	ht := l.Public + "/.htaccess"
	if slices.Contains(stepIDs(rs.Plan), "file:"+ht) {
		t.Errorf("Plan khôi phục ghi .htaccess cho site ở chế độ tat: %v", stepIDs(rs.Plan))
	}
	rep, err := rs.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("khôi phục: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if se.exists(ht) || !se.exists(l.ApacheLink) {
		t.Error("khôi phục site tat: có .htaccess, hoặc chưa bật vhost Apache")
	}
	vh, _ := se.m.ReadFile(l.ApacheConf)
	if !strings.Contains(string(vh), "FallbackResource /index.php") {
		t.Errorf("vhost Apache của site tat thiếu FallbackResource:\n%s", vh)
	}
}
