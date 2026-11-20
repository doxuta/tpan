package core

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- giả lập

// suspendCurl thay câu trả lời của curl --resolve: vhost đang bật của domain là
// bản tạm khoá thì 403 kèm trang tạm khoá trên đĩa (hoặc trang 403 trơn nếu
// trang chưa có), như Nginx với error_page. Còn lại thì hỏi other.
func suspendCurl(fs *system.FakeSystem, other func(system.Cmd) system.Result) {
	fs.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		if r := argAfter(c.Args, "--resolve"); r != "" {
			dom, _, _ := strings.Cut(r, ":")
			if d, err := validate.ParseDomain(dom); err == nil {
				l := site.LayoutOf(d)
				for _, p := range [][2]string{{l.NginxLink, l.NginxConf}, {l.ApacheLink, l.ApacheConf}} {
					if _, err := fs.Stat(p[0]); err != nil {
						continue
					}
					if conf, _ := fs.ReadFile(p[1]); strings.Contains(string(conf), templates.SuspendedURI) {
						body := "<html>403 Forbidden</html>"
						if b, err := fs.ReadFile(templates.SuspendedPage); err == nil {
							body = string(b)
						}
						return system.Result{Stdout: body + "TPAN_HTTP_CODE=403"}
					}
				}
			}
		}
		return other(c)
	})
}

// siteSimCurl là câu trả lời của siteSim cho phép kiểm HTTP: vhost bật thì 200
// đúng WordPress của domain, không thì 404.
func siteSimCurl(m *siteSim) func(system.Cmd) system.Result {
	return func(c system.Cmd) system.Result {
		dom, _, _ := strings.Cut(argAfter(c.Args, "--resolve"), ":")
		if dom == "" || !m.served(dom) {
			return system.Result{Stdout: "<html>404</html>TPAN_HTTP_CODE=404"}
		}
		return system.Result{Stdout: "<html><link rel='stylesheet' href='http://" + dom + "/wp-content/themes/x/style.css'></html>TPAN_HTTP_CODE=200"}
	}
}

const susDom = "shop.example.com"

// newSuspendEnv là fixEnv có một site và curl hiểu vhost tạm khoá.
func newSuspendEnv(t *testing.T, web WebServer) *fixEnv {
	t.Helper()
	e := newFixEnv(t, web, susDom)
	suspendCurl(e.m.FakeSystem, siteSimCurl(e.m))
	goneEvery, midCheckWait = 0, 0
	t.Cleanup(func() { goneEvery, midCheckWait = 200*time.Millisecond, time.Second })
	return e
}

// planned nhận thẳng hai giá trị của Prepare*: planned(t)(s.PrepareSuspend(ctx, d));
// xem trước rồi chạy, và đòi job thành công.
func planned(t *testing.T) func(*Planned, error) engine.Report {
	return func(p *Planned, err error) engine.Report {
		t.Helper()
		return runPlanned(t, p, err)
	}
}

func runPlanned(t *testing.T, p *Planned, err error) engine.Report {
	t.Helper()
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := p.Preview(ctx); err != nil {
		t.Fatalf("Preview: %v", err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %v (%s); bước %+v; kiểm %s", p.Plan.Kind, err, rep.Job.Error, rep.Steps, failedChecks(rep.Checks))
	}
	return rep
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (e *fixEnv) managedSHA(t *testing.T) map[string]string {
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

func (e *fixEnv) site(t *testing.T, dom string) site.Site {
	t.Helper()
	st, ok, err := e.sites.Site(ctx, mustDomain(t, dom))
	if err != nil || !ok {
		t.Fatalf("site %s: %v %v", dom, ok, err)
	}
	return st
}

func (e *fixEnv) setSite(t *testing.T, st site.Site) {
	t.Helper()
	if err := e.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
}

func resultsByID(rs []checks.Result) map[string]checks.Result {
	out := map[string]checks.Result{}
	for _, r := range rs {
		out[r.ID] = r
	}
	return out
}

// ---------------------------------------------------------------- test

// Tạm khoá rồi mở khoá trên cả hai web server: đúng file trên đĩa, đúng dòng
// managed_files, đúng bảng sites, đúng bộ phép kiểm của health.
func TestSuspendUnsuspend(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newSuspendEnv(t, web)
			d := mustDomain(t, susDom)
			l := site.LayoutOf(d)
			conf := l.NginxConf
			if web == Apache {
				conf = l.ApacheConf
			}
			pool, cron := l.PoolConf("8.3"), site.CronFile(l)
			poolOrig, _ := e.m.ReadFile(pool)
			cronOrig, _ := e.m.ReadFile(cron)
			normalVhost, _ := e.m.ReadFile(conf)

			p, err := e.s.PrepareSuspend(ctx, d)
			if err == nil {
				if p.Plan.Kind != "site.suspend" || p.Plan.Target != susDom {
					t.Errorf("Plan = %s %s", p.Plan.Kind, p.Plan.Target)
				}
				// Thứ tự: trang, vhost, rồi mới pool và cron (không lúc nào vhost
				// trỏ vào socket đã mất).
				ids := stepIDs(p.Plan)
				iv, ip := slices.Index(ids, "file:"+conf), slices.Index(ids, "rename:"+pool)
				if ids[0] != "suspended.page:"+susDom || iv < 0 || ip < iv || ids[len(ids)-1] != "rename:"+cron {
					t.Errorf("thứ tự bước = %q", ids)
				}
			}
			runPlanned(t, p, err)

			vh, _ := e.m.ReadFile(conf)
			if !strings.Contains(string(vh), templates.SuspendedURI) || strings.Contains(string(vh), l.Public) {
				t.Errorf("vhost sau khi khoá không phải bản tạm khoá:\n%s", vh)
			}
			for _, p := range []string{pool, cron, l.SocketFor("8.3")} {
				if _, err := e.m.Stat(p); err == nil {
					t.Errorf("%s còn sau khi khoá", p)
				}
			}
			if got, _ := e.m.ReadFile(pool + ".suspended"); string(got) != string(poolOrig) {
				t.Error("pool .suspended không giữ nội dung cũ")
			}
			if got, _ := e.m.ReadFile(cron + ".suspended"); string(got) != string(cronOrig) {
				t.Error("cron .suspended không giữ nội dung cũ")
			}
			if page, _ := e.m.ReadFile(templates.SuspendedPage); string(page) != string(templates.SuspendedHTML()) {
				t.Error("trang tạm khoá chưa được ghi")
			}
			mf := e.managedSHA(t)
			if _, ok := mf[pool]; ok {
				t.Error("managed_files còn dòng của pool đã tắt: health sẽ báo thiếu file")
			}
			if _, ok := mf[cron]; ok {
				t.Error("managed_files còn dòng của cron đã tắt")
			}
			if mf[conf] != shaHex(vh) || mf[templates.SuspendedPage] != shaHex(templates.SuspendedHTML()) {
				t.Error("managed_files chưa có vhost tạm khoá hay trang tạm khoá")
			}
			st := e.site(t, susDom)
			if st.Suspended != account.SuspendSelf || st.SuspendedAt.IsZero() {
				t.Errorf("bảng sites: Suspended %q, lúc %v", st.Suspended, st.SuspendedAt)
			}
			if _, err := e.s.PrepareSuspend(ctx, d); refusedCode(t, err) != CodeSiteSuspended {
				t.Error("khoá lần hai không bị từ chối site_suspended")
			}

			// Health: DNS, TLS, web như thường; PHP, WordPress, DB là "tạm khoá";
			// đầu cuối là http.suspended đạt.
			cs, err := e.s.Checks(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			got := resultsByID(checks.RunAll(ctx, e.m, cs))
			for _, stage := range []checks.Stage{checks.StagePHP, checks.StageWordPress, checks.StageDB} {
				r := got["suspended."+string(stage)+":"+susDom]
				if r.Status != checks.Unknown || !r.Suspended || r.Stage != stage {
					t.Errorf("chặng %s: %+v", stage, r)
				}
			}
			if r := got["http.suspended:"+susDom]; r.Status != checks.Pass {
				t.Errorf("http.suspended = %s %s", r.Status, r.Summary.Vi)
			}
			if r := got["web.vhost:"+susDom]; r.Status != checks.Pass {
				t.Errorf("web.vhost = %s %s", r.Status, r.Summary.Vi)
			}
			for _, id := range []string{"php.socket:", "wp.checksums:", "wp.config:", "db.access:", "http.site:"} {
				if _, ok := got[id+susDom]; ok {
					t.Errorf("site khoá vẫn chạy phép kiểm %s", id)
				}
			}

			p, err = e.s.PrepareUnsuspend(ctx, d)
			if err == nil {
				ids := stepIDs(p.Plan)
				if p.Plan.Kind != "site.unsuspend" || ids[0] != "rename:"+pool+".suspended" || ids[len(ids)-1] != "site.unsuspend.check:"+susDom {
					t.Errorf("Plan = %s, bước %q", p.Plan.Kind, ids)
				}
			}
			runPlanned(t, p, err)
			if got, _ := e.m.ReadFile(conf); string(got) != string(normalVhost) {
				t.Errorf("vhost sau khi mở khoá khác vhost thường:\n%s", got)
			}
			if got, _ := e.m.ReadFile(pool); string(got) != string(poolOrig) {
				t.Error("pool không về")
			}
			if got, _ := e.m.ReadFile(cron); string(got) != string(cronOrig) {
				t.Error("cron không về")
			}
			for _, p := range []string{pool + ".suspended", cron + ".suspended"} {
				if _, err := e.m.Stat(p); err == nil {
					t.Errorf("%s còn sau khi mở khoá", p)
				}
			}
			if _, err := e.m.Stat(l.SocketFor("8.3")); err != nil {
				t.Errorf("socket chưa có sau khi mở khoá: %v", err)
			}
			mf = e.managedSHA(t)
			if mf[pool] != shaHex(poolOrig) || mf[cron] != shaHex(cronOrig) {
				t.Error("managed_files chưa ghi lại pool hay cron sau khi mở khoá")
			}
			if st := e.site(t, susDom); st.Suspended != account.NotSuspended || !st.SuspendedAt.IsZero() {
				t.Errorf("bảng sites sau mở khoá: %q %v", st.Suspended, st.SuspendedAt)
			}
			if _, err := e.s.PrepareUnsuspend(ctx, d); refusedCode(t, err) != CodeSiteNotSuspended {
				t.Error("mở khoá site không khoá không bị từ chối")
			}
		})
	}
}

// Kiểm chứng tạm khoá hỏng (vẫn trả 200): engine hoàn tác, máy về như cũ,
// bảng sites không đổi.
func TestSuspendRollback(t *testing.T) {
	e := newSuspendEnv(t, Nginx)
	d := mustDomain(t, susDom)
	// curl luôn trả WordPress 200, như khi một vhost khác nhận tên miền này.
	e.m.Handle([]string{"curl"}, siteSimCurl(e.m))
	before := e.m.Snapshot()
	p, err := e.s.PrepareSuspend(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status == engine.JobSucceeded {
		t.Fatalf("job %s %v; muốn lỗi ở kiểm chứng", rep.Job.Status, err)
	}
	after := e.m.Snapshot()
	for path, ent := range before {
		if path == templates.SuspendedDir || strings.HasPrefix(path, templates.SuspendedDir) {
			continue
		}
		if a, ok := after[path]; !ok || string(a.Data) != string(ent.Data) {
			t.Errorf("%s khác sau hoàn tác", path)
		}
	}
	if _, err := e.m.Stat(templates.SuspendedPage); err == nil {
		t.Error("trang tạm khoá còn sau hoàn tác")
	}
	if st := e.site(t, susDom); st.Suspended != account.NotSuspended {
		t.Error("job hỏng mà bảng sites ghi đã khoá")
	}
}

// File pool .suspended mất: mở khoá ghi pool từ template, không bế tắc.
func TestUnsuspendWithoutParkedPool(t *testing.T) {
	e := newSuspendEnv(t, Nginx)
	d := mustDomain(t, susDom)
	l := site.LayoutOf(d)
	planned(t)(e.s.PrepareSuspend(ctx, d))
	if err := e.m.Remove(l.PoolConf("8.3") + ".suspended"); err != nil {
		t.Fatal(err)
	}
	p, err := e.s.PrepareUnsuspend(ctx, d)
	if err == nil && !slices.Contains(stepIDs(p.Plan), "file:"+l.PoolConf("8.3")) {
		t.Errorf("không có bước ghi pool từ template: %q", stepIDs(p.Plan))
	}
	runPlanned(t, p, err)
}

// Tạm khoá site có SSL ra bản tạm khoá có 443, và xin SSL cho site đang khoá
// (quyết định 8): vhost 443 ghi qua siteVhost nên vẫn là bản tạm khoá.
func TestSuspendSSL(t *testing.T) {
	se := newSSLEnv(t, Nginx)
	se.setEmail(t)
	se.add(t, susDom)
	suspendCurl(se.m.FakeSystem, siteSimCurl(se.m))
	d := mustDomain(t, susDom)
	l := site.LayoutOf(d)
	fe := &fixEnv{siteEnv: se.siteEnv}
	planned(t)(se.s.PrepareSuspend(ctx, d))

	iss, err := se.ssl.PrepareIssue(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range checks.RunAll(ctx, system.NewFake(), iss.Plan.Verify) {
		ids[strings.SplitN(r.ID, ":", 2)[0]] = true
	}
	if !ids["http.suspended"] || !ids["tls.cert"] || ids["https.site"] || ids["db.access"] || ids["php.socket"] {
		t.Errorf("kiểm chứng SSL của site khoá = %v", ids)
	}
	// Phần còn lại của kiểm chứng (certbot.timer, dry-run) đã có ở test SSL.
	iss.Plan.Verify = []checks.Check{checks.HTTPSuspended(l, true)}
	rep, err := iss.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s %s; bước %+v%s", err, rep.Job.Status, rep.Job.Error, rep.Steps, failedChecks(rep.Checks))
	}
	vh, _ := se.m.ReadFile(l.NginxConf)
	for _, w := range []string{"443", templates.SuspendedURI, l.CertFullchain()} {
		if !strings.Contains(string(vh), w) {
			t.Errorf("vhost sau khi xin SSL thiếu %q:\n%s", w, vh)
		}
	}
	if strings.Contains(string(vh), l.Public) || strings.Contains(string(vh), "return 301") {
		t.Errorf("vhost sau khi xin SSL không còn là bản tạm khoá:\n%s", vh)
	}
	st := fe.site(t, susDom)
	if !st.SSL || st.Suspended != account.SuspendSelf {
		t.Errorf("bảng sites: SSL %v, Suspended %q", st.SSL, st.Suspended)
	}

	// DB của site đang khoá: không xin SSL được (WPSiteURL ghi vào DB).
	st.DBSuspended = account.SuspendSelf
	fe.setSite(t, st)
	if _, err := se.ssl.PrepareIssue(ctx, d); refusedCode(t, err) != CodeDBSuspended {
		t.Error("xin SSL khi DB khoá không bị từ chối db_suspended")
	}
}

// Chuyển LEMP sang LAMP với một site đang khoá: site đó ra vhost Apache bản tạm
// khoá (có 443), điều kiện đầu và kiểm giữa chừng đòi 403 chứ không đòi 200.
func TestSuspendStackSwitch(t *testing.T) {
	sus := switchSite(t, "s.example.com", true)
	sus.Suspended = account.SuspendSelf
	e := newSwitchEnv(t, Nginx, SourceUbuntu, sus, switchSite(t, "b.example.com", false))
	e.SetFile(templates.SuspendedPage, templates.SuspendedHTML(), 0o644, system.Root)
	suspendCurl(e.FakeSystem, func(c system.Cmd) system.Result {
		if r := argAfter(c.Args, "--resolve"); r != "" {
			dom, _, _ := strings.Cut(r, ":")
			code := e.siteCode(dom)
			body := "<html>" + code + "</html>"
			if code == "200" {
				body = "<html><link rel='stylesheet' href='http://" + dom + "/wp-content/themes/x/style.css'></html>"
			}
			return system.Result{Stdout: body + "TPAN_HTTP_CODE=" + code}
		}
		switch e.active() {
		case "nginx":
			return system.Result{ExitCode: 52, Stdout: "000", Stderr: "curl: (52) Empty reply from server\n"}
		case "apache2":
			return system.Result{Stdout: "403"}
		}
		return system.Result{ExitCode: 7, Stdout: "000"}
	})
	p, err := e.sw().Prepare(ctx, Apache)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var title string
	for _, s := range p.Plan.Steps {
		if s.Info().ID == "stack.switch.check:s.example.com" {
			title = s.Info().Title.Vi
		}
	}
	if !strings.Contains(title, "403") {
		t.Errorf("kiểm giữa chừng của site khoá = %q; muốn đòi 403", title)
	}
	rep, _, err := e.execute(t, p)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v %s %s%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	ls := site.LayoutOf(mustDomain(t, "s.example.com"))
	vh, _ := e.ReadFile(ls.ApacheConf)
	if !strings.Contains(string(vh), templates.SuspendedURI) || !strings.Contains(string(vh), "*:443") || strings.Contains(string(vh), ls.Public) {
		t.Errorf("vhost Apache của site khoá không phải bản tạm khoá có 443:\n%s", vh)
	}
	lb := site.LayoutOf(mustDomain(t, "b.example.com"))
	if vb, _ := e.ReadFile(lb.ApacheConf); !strings.Contains(string(vb), lb.Public) {
		t.Errorf("vhost của site thường không phải bản thường:\n%s", vb)
	}
}

// Sửa tự động của health trên site khoá: ghi lại vhost bản tạm khoá, không bao
// giờ ghi lại pool, không khởi động lại PHP cho nó, không mở khoá user DB khi
// DB đang khoá.
func TestSuspendFixes(t *testing.T) {
	e := newSuspendEnv(t, Nginx)
	d := mustDomain(t, susDom)
	l := site.LayoutOf(d)
	planned(t)(e.s.PrepareSuspend(ctx, d))
	suspended, _ := e.m.ReadFile(l.NginxConf)

	// web.rewrite (nginx -t lỗi vì vhost bị sửa hỏng): ghi lại bản tạm khoá.
	e.m.SetFile(l.NginxConf, []byte("server { hỏng"), 0o644, system.Root)
	e.fix(t, checks.Result{ID: "config.nginx:nginx", Status: checks.Fail})
	if got, _ := e.m.ReadFile(l.NginxConf); string(got) != string(suspended) {
		t.Errorf("web.rewrite ghi vhost khác bản tạm khoá:\n%s", got)
	}
	// web.vhost (link bị gỡ): bật lại đúng bản tạm khoá.
	if err := e.m.Remove(l.NginxLink); err != nil {
		t.Fatal(err)
	}
	e.fix(t, checks.Result{ID: "web.vhost:" + susDom, Status: checks.Fail})
	if got, _ := e.m.ReadFile(l.NginxConf); string(got) != string(suspended) {
		t.Error("web.vhost ghi vhost khác bản tạm khoá")
	}
	// Lệch của trang tạm khoá: dựng lại được.
	x, err := e.f.Prepare(ctx, checks.Result{ID: "drift:" + templates.SuspendedPage, Status: checks.Fail})
	if err != nil || len(x.Plan.Steps) != 1 || x.Plan.Steps[0].Info().ID != "file:"+templates.SuspendedPage {
		t.Errorf("file.rewrite trang tạm khoá: %v %v", x, err)
	}
	// Pool của site khoá: không có cách sửa tự động (ghi lại là mở khoá lén).
	for _, id := range []string{"drift:" + l.PoolConf("8.3")} {
		if _, err := e.f.Prepare(ctx, checks.Result{ID: id, Status: checks.Fail}); refusedCode(t, err) != "no_auto_fix" {
			t.Errorf("%s không bị từ chối no_auto_fix", id)
		}
	}
	if _, err := e.f.Prepare(ctx, checks.Result{ID: "php.socket:" + susDom, Status: checks.Fail}); refusedCode(t, err) != CodeSiteSuspended {
		t.Error("php.restart cho site khoá không bị từ chối")
	}
	if _, err := e.m.Stat(l.PoolConf("8.3")); err == nil {
		t.Error("sửa tự động đã ghi lại pool")
	}

	st := e.site(t, susDom)
	st.DBSuspended = account.SuspendSelf
	e.setSite(t, st)
	if _, err := e.f.Prepare(ctx, checks.Result{ID: "db.access:" + susDom, Status: checks.Fail}); refusedCode(t, err) != CodeDBSuspended {
		t.Error("db.unlock khi DB đang khoá không bị từ chối")
	}
}

// Các thao tác cần pool hay DB của site bị từ chối khi site, DB đang khoá.
func TestSuspendRefusals(t *testing.T) {
	e := newSuspendEnv(t, Nginx)
	d := mustDomain(t, susDom)
	php := &PHP{Sys: e.m, Settings: e.st, Sites: e.sites, Journal: e.st}
	e.m.SetDir("/etc/php/8.4/fpm/pool.d", 0o755, system.Root)

	st := e.site(t, susDom)
	st.Suspended = account.SuspendSelf
	e.setSite(t, st)
	if _, err := php.PrepareUse(ctx, d, "8.4"); refusedCode(t, err) != CodeSiteSuspended {
		t.Error("đổi bản PHP của site khoá không bị từ chối")
	}
	ce := &Cron{Sites: e.s, Runs: e.st}
	if r, err := ce.Run(ctx, d); err != nil || r.Status != CronSkipped || !strings.Contains(r.Output, "suspended") {
		t.Errorf("cron của site khoá: %+v %v", r, err)
	}

	st.Suspended, st.DBSuspended = account.NotSuspended, account.SuspendByUser
	e.setSite(t, st)
	if _, err := php.PrepareUse(ctx, d, "8.4"); refusedCode(t, err) != CodeDBSuspended {
		t.Error("đổi bản PHP khi DB khoá không bị từ chối db_suspended")
	}
	if r, _ := ce.Run(ctx, d); r.Status != CronSkipped {
		t.Errorf("cron khi DB khoá: %+v", r)
	}
	// Health khi chỉ DB khoá: chặng DB và đầu cuối là "tạm khoá", phần còn lại kiểm như thường.
	cs, err := e.s.Checks(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	got := resultsByID(checks.RunAll(ctx, e.m, cs))
	for _, id := range []string{"suspended.db:", "suspended.e2e:"} {
		if r := got[id+susDom]; r.Status != checks.Unknown || !r.Suspended || !strings.Contains(r.Summary.Vi, "DB") {
			t.Errorf("%s = %+v", id, r)
		}
	}
	for _, id := range []string{"web.vhost:", "php.socket:", "wp.config:"} {
		if r := got[id+susDom]; r.Status != checks.Pass {
			t.Errorf("%s = %s %s", id, r.Status, r.Summary.Vi)
		}
	}
	for _, id := range []string{"db.access:", "http.site:", "suspended.php:"} {
		if _, ok := got[id+susDom]; ok {
			t.Errorf("DB khoá mà vẫn có %s", id)
		}
	}

	// Khôi phục từ thùng rác một dòng còn cờ khoá: từ chối (mở khoá lén).
	st.Status, st.Suspended, st.DBSuspended = site.Trashed, account.SuspendSelf, account.NotSuspended
	e.setSite(t, st)
	if _, err := e.s.PrepareRestore(ctx, d); refusedCode(t, err) != CodeSiteSuspended {
		t.Error("khôi phục site còn khoá không bị từ chối")
	}
	if _, err := e.s.PrepareSuspend(ctx, d); refusedCode(t, err) != "site_trashed" {
		t.Error("khoá site trong thùng rác không bị từ chối")
	}
	st.Suspended, st.DBSuspended = account.NotSuspended, account.SuspendSelf
	e.setSite(t, st)
	if _, err := e.s.PrepareRestore(ctx, d); refusedCode(t, err) != CodeDBSuspended {
		t.Error("khôi phục site có DB còn khoá không bị từ chối")
	}
}

// suspendedChecks thử lại tới khi đạt (reload trả về trước khi vhost mới có
// hiệu lực), và chỉ dựa vào Layout, SSL (khoá user gọi khi Suspended còn rỗng).
func TestSuspendedChecksRetry(t *testing.T) {
	goneEvery = 0
	t.Cleanup(func() { goneEvery = 200 * time.Millisecond })
	l := site.LayoutOf(mustDomain(t, susDom))
	f := system.NewFake()
	n := 0
	f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		n++
		if n < 3 {
			return system.Result{Stdout: "<html>wp</html>TPAN_HTTP_CODE=200"}
		}
		return system.Result{Stdout: string(templates.SuspendedHTML()) + "TPAN_HTTP_CODE=403"}
	})
	cs := suspendedChecks(site.Site{Domain: l.Domain})
	if len(cs) != 1 {
		t.Fatalf("%d phép kiểm", len(cs))
	}
	if r := cs[0].Run(ctx, f); r.Status != checks.Pass || r.ID != "http.suspended:"+susDom || n != 3 {
		t.Errorf("kết quả %s %s sau %d lần gọi", r.ID, r.Status, n)
	}
}
