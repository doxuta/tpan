package server

// Test handler của M14 trên máy demo (internal/demo): bốn route mới đi qua
// bảng route thật (guard + handler), với dữ liệu mẫu có ba chủ: admin
// (blog.example.com), khach_a (shop.example.net), khach_b (wiki, lab).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/demo"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	m14Blog  = "blog.example.com" // của admin, có SSL
	m14Shop  = "shop.example.net" // của khach_a
	m14Wiki  = "wiki.example.org" // của khach_b, hỏng socket
	m14Lab   = "lab.example.org"  // của khach_b, đang tạm khoá
	m14UserA = "khach_a"
	m14UserB = "khach_b"
)

// m14Env là Server chạy trên máy demo, kèm phiên của admin và hai user mẫu.
type m14Env struct {
	*authzEnv
	h http.Handler
}

func newM14Env(t *testing.T) *m14Env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dir := t.TempDir()
	sys, st, err := demo.New(ctx, dir)
	if err != nil {
		t.Fatalf("dựng máy demo: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SetAdminPasswordHash(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	e := &authzEnv{t: t, st: st, f: sys, now: time.Now(), cookies: map[string]*http.Cookie{}, dbPath: filepath.Join(dir, "tpan.db")}
	for _, n := range []string{account.AdminName, m14UserA, m14UserB} {
		e.cookies[n] = e.session(n, "")
	}
	wp, wpcli := demo.Hashes()
	e.s = newServer(Deps{Sys: sys, Store: st, Demo: true, Hostname: "demo", WPHash: wp, WPCLISHA512: wpcli})
	t.Cleanup(e.s.api.wg.Wait)
	return &m14Env{authzEnv: e, h: e.s.handler(authzDist)}
}

func (e *m14Env) call(method, path, body, who string) *httptest.ResponseRecorder {
	return e.do(e.h, method, path, body, e.cookies[who])
}

// runHealth chạy health đầy đủ như bộ hẹn giờ của `tpan serve --demo`.
func (e *m14Env) runHealth(t *testing.T) {
	t.Helper()
	if _, err := e.s.healthRunner().Run(context.Background(), health.Options{Full: true}); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func (e *m14Env) audit(t *testing.T, action string) []store.AuditEntry {
	t.Helper()
	return hAudit(t, &apiEnv{dbPath: e.dbPath}, action)
}

// healWiki khởi động lại PHP-FPM của máy demo: socket của wiki có lại, để Plan
// có kiểm chứng trên mọi site (harden, chuyển web server) không vướng site hỏng sẵn.
func (e *m14Env) healWiki(t *testing.T) {
	t.Helper()
	if _, err := e.f.Run(context.Background(), system.Cmd{Name: "systemctl", Args: []string{"restart", "php8.3-fpm"}}); err != nil {
		t.Fatal(err)
	}
}

// toApache chuyển máy demo sang Apache bằng chính core.Switch.
func (e *m14Env) toApache(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	e.healWiki(t)
	p, err := (&core.Switch{Sys: e.f, Settings: e.st, Sites: e.st, Journal: e.st}).Prepare(ctx, core.Apache)
	if err != nil {
		t.Fatalf("chuẩn bị chuyển sang Apache: %v", err)
	}
	if rep, err := p.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chuyển sang Apache: %v, %s %s", err, rep.Job.Status, rep.Job.Error)
	}
}

func (e *m14Env) site(t *testing.T, dom string) site.Site {
	t.Helper()
	st, ok, err := e.st.Site(context.Background(), apiDomain(t, dom))
	if err != nil || !ok {
		t.Fatalf("site %s: %v %v", dom, ok, err)
	}
	return st
}

// badHtaccess đặt vào site một .htaccess có chỉ thị ngoài danh sách ở dòng 3.
func (e *m14Env) badHtaccess(t *testing.T, dom string) string {
	l := site.LayoutOf(apiDomain(t, dom))
	p := l.Public + "/.htaccess"
	e.f.SetFile(p, []byte("# BEGIN WordPress\nRewriteEngine On\nSetHandler application/x-httpd-php\n# END WordPress\n"), 0o644, system.Owner{User: l.User, Group: l.User})
	return p
}

// job gửi một POST chạy thật, chờ job xong và trả nó.
func (e *m14Env) job(t *testing.T, path, body, who string) engine.Job {
	t.Helper()
	js := apiDecode[JobStarted](t, e.call("POST", path, body, who), 202)
	e.s.api.wg.Wait()
	j, err := e.st.Job(context.Background(), js.JobID)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// ---------------------------------------------------------------- /route

func TestM14RouteTrenMayDemo(t *testing.T) {
	e := newM14Env(t)
	want := []string{core.HopDNS, core.HopVhost, core.HopRoot, core.HopSocket, core.HopPool, core.HopUser, core.HopOpenBasedir, core.HopHTTP}
	l := site.LayoutOf(apiDomain(t, m14Shop))

	// Chủ của site và admin đều xem được.
	for _, who := range []string{account.AdminName, m14UserA} {
		tr := apiDecode[core.RouteTrace](t, e.call("GET", "/api/v1/sites/"+m14Shop+"/route", "", who), 200)
		if tr.Domain != m14Shop || tr.Web != core.Nginx {
			t.Fatalf("%s: trace = %s %s", who, tr.Domain, tr.Web)
		}
		var keys []string
		for _, h := range tr.Hops {
			keys = append(keys, h.Key)
			if h.Key != core.HopDNS && h.Status != checks.Pass {
				t.Errorf("%s: chặng %s = %s (%s)", who, h.Key, h.Status, h.Summary.Vi)
			}
			if len(h.Evidence) == 0 || h.Title.Vi == "" || h.Summary.En == "" {
				t.Errorf("%s: chặng %s thiếu bằng chứng hoặc chữ: %+v", who, h.Key, h)
			}
		}
		if !slices.Equal(keys, want) {
			t.Errorf("%s: chặng = %v", who, keys)
		}
		if tr.Hops[1].Value == "" || !strings.HasPrefix(tr.Hops[1].Value, l.NginxLink+":") || tr.Hops[5].Value != l.User {
			t.Errorf("%s: vhost %q, user %q", who, tr.Hops[1].Value, tr.Hops[5].Value)
		}
	}

	// User khác: 404 như site không tồn tại (cùng câu với site không có thật).
	other := e.call("GET", "/api/v1/sites/"+m14Shop+"/route", "", m14UserB)
	none := e.call("GET", "/api/v1/sites/none.example.com/route", "", m14UserB)
	if apiErrCode(t, other, 404) != "site_not_found" || apiErrCode(t, none, 404) != "site_not_found" {
		t.Errorf("user khác: %s; site không có: %s", other.Body, none.Body)
	}
	if code := apiErrCode(t, e.call("GET", "/api/v1/sites/a..b/route", "", account.AdminName), 400); code != "domain_invalid" {
		t.Errorf("domain sai dạng: %s", code)
	}

	// Site tạm khoá: dừng sau vhost. Site có SSL: gọi thật qua https.
	lab := apiDecode[core.RouteTrace](t, e.call("GET", "/api/v1/sites/"+m14Lab+"/route", "", m14UserB), 200)
	if len(lab.Hops) != 3 || lab.Hops[2].Key != core.HopHTTP || !strings.Contains(lab.Hops[1].Summary.Vi, "tạm khoá") {
		t.Errorf("site tạm khoá: %+v", lab.Hops)
	}
	blog := apiDecode[core.RouteTrace](t, e.call("GET", "/api/v1/sites/"+m14Blog+"/route", "", account.AdminName), 200)
	if len(blog.Hops) != 8 || blog.Hops[7].Value != "https://"+m14Blog+"/" || blog.Hops[7].Status != checks.Pass || !strings.Contains(blog.Hops[1].Summary.Vi, "443") {
		t.Errorf("site SSL: %+v", blog.Hops)
	}
	// Site hỏng socket: đường đi theo cấu hình vẫn đúng, chặng gọi thật đỏ.
	wiki := apiDecode[core.RouteTrace](t, e.call("GET", "/api/v1/sites/"+m14Wiki+"/route", "", m14UserB), 200)
	if wiki.Hops[4].Status != checks.Pass || wiki.Hops[7].Status != checks.Fail {
		t.Errorf("site hỏng socket: pool %s, http %s", wiki.Hops[4].Status, wiki.Hops[7].Status)
	}
	// JSON không có null ở danh sách.
	if body := e.call("GET", "/api/v1/sites/"+m14Shop+"/route", "", account.AdminName).Body.String(); strings.Contains(body, `"evidence":null`) || strings.Contains(body, `"hops":null`) {
		t.Errorf("JSON có null: %s", body)
	}
}

// ---------------------------------------------------------------- /htaccess

func TestM14HtaccessLuat(t *testing.T) {
	e := newM14Env(t)
	path := "/api/v1/sites/" + m14Shop + "/htaccess"

	// Mode lạ: 400, và thông báo không in lại giá trị người gọi gửi.
	for _, body := range []string{`{"mode":"all"}`, `{"mode":""}`, `{}`, `{"mode":"<script>x</script>"}`} {
		rec := e.call("POST", path, body, m14UserA)
		if code := apiErrCode(t, rec, 400); code != "htaccess_mode_invalid" {
			t.Errorf("thân %s: mã %s", body, code)
		}
		if strings.Contains(rec.Body.String(), "script") {
			t.Errorf("thông báo in lại giá trị lạ: %s", rec.Body)
		}
	}
	if code := apiErrCode(t, e.call("POST", path, `{"mode":"tat","x":1}`, m14UserA), 400); code != "bad_request" {
		t.Errorf("trường lạ: %s", code)
	}

	// day-du: user thường, kể cả chủ của site, nhận 403; cả khi chỉ xem trước.
	for _, q := range []string{"", "?dry_run=1"} {
		if code := apiErrCode(t, e.call("POST", path+q, `{"mode":"day-du"}`, m14UserA), 403); code != "forbidden" {
			t.Errorf("chủ đặt day-du%s: %s", q, code)
		}
	}
	if a := e.audit(t, "site.htaccess"); len(a) != 2 || a[0].Target != m14Shop || a[0].Result != "forbidden" {
		t.Errorf("audit của lần bị cấm = %+v", a)
	}
	// User khác: 404 trước khi tới handler, mode nào cũng vậy.
	if code := apiErrCode(t, e.call("POST", path, `{"mode":"day-du"}`, m14UserB), 404); code != "site_not_found" {
		t.Errorf("user khác: %s", code)
	}

	// Admin đặt day-du, chủ đặt an-toan: qua được luật của server và tới core.
	// Máy demo chạy nginx nên core từ chối htaccess_not_apache (409), không
	// phải 400 hay 403.
	for who, mode := range map[string]string{account.AdminName: "day-du", m14UserA: "an-toan", m14UserB: "tat"} {
		p := path
		if who == m14UserB {
			p = "/api/v1/sites/" + m14Wiki + "/htaccess"
		}
		for _, q := range []string{"?dry_run=1", ""} {
			rec := e.call("POST", p+q, `{"mode":"`+mode+`"}`, who)
			if code := apiErrCode(t, rec, 409); code != core.CodeHtaccessNotApache {
				t.Errorf("%s đặt %s%s: %s", who, mode, q, rec.Body)
			}
		}
	}
	// Lời từ chối của lần chạy thật được ghi nhật ký; lần xem trước thì không.
	var refused int
	for _, a := range e.audit(t, "site.htaccess") {
		if a.Result == core.CodeHtaccessNotApache {
			refused++
		}
	}
	if refused != 3 {
		t.Errorf("audit htaccess_not_apache = %d; muốn 3", refused)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); slices.ContainsFunc(jobs, func(j engine.Job) bool { return j.Kind == "site.htaccess" }) {
		t.Error("bị từ chối mà vẫn có job site.htaccess")
	}
}

// Đi hết vòng trên máy demo đã chuyển sang Apache: chủ đổi chế độ, core quét
// .htaccess, admin đặt day-du, và ma trận cô lập phản ánh đúng.
func TestM14HtaccessTrenApache(t *testing.T) {
	e := newM14Env(t)
	e.toApache(t)
	ctx := context.Background()
	path := "/api/v1/sites/" + m14Shop + "/htaccess"
	l := site.LayoutOf(apiDomain(t, m14Shop))

	// Xem trước: Plan, không job, không nhật ký, bảng sites giữ nguyên.
	pv := apiDecode[engine.Preview](t, e.call("POST", path+"?dry_run=1", `{"mode":"tat"}`, m14UserA), 200)
	if pv.Kind != "site.htaccess" || pv.Target != m14Shop || len(pv.Steps) == 0 {
		t.Fatalf("Preview = %+v", pv)
	}
	if len(e.audit(t, "site.htaccess")) != 0 || e.site(t, m14Shop).Htaccess != site.HtaccessSafe {
		t.Error("xem trước mà có nhật ký hoặc đã đổi chế độ")
	}
	if code := apiErrCode(t, e.call("POST", path, `{"mode":"an-toan"}`, m14UserA), 409); code != core.CodeHtaccessSame {
		t.Errorf("đặt lại chế độ đang có: %s", code)
	}

	// Chủ của site chuyển sang "tat": job thành công, ghi người chạy và nhật ký.
	j := e.job(t, path, `{"mode":"tat"}`, m14UserA)
	if j.Kind != "site.htaccess" || j.Target != m14Shop || j.Status != engine.JobSucceeded || j.Actor != m14UserA {
		t.Fatalf("job = %+v", j)
	}
	if a := e.audit(t, "site.htaccess"); len(a) != 2 || a[1].JobID != j.ID || a[1].Result != "ok" {
		t.Errorf("audit = %+v", a)
	}
	vhost, _ := e.f.ReadFile(l.ApacheConf)
	if e.site(t, m14Shop).Htaccess != site.HtaccessOff || !strings.Contains(string(vhost), "FallbackResource /index.php") {
		t.Errorf("sau job: chế độ %q, vhost:\n%s", e.site(t, m14Shop).Htaccess, vhost)
	}
	// GET /sites/{domain} có trường htaccess (hợp đồng types_m14.go).
	if body := e.call("GET", "/api/v1/sites/"+m14Shop, "", m14UserA).Body.String(); !strings.Contains(body, `"htaccess":"tat"`) {
		t.Errorf("GET site thiếu htaccess=tat: %.300s", body)
	}

	// Về an-toan khi .htaccess có chỉ thị ngoài danh sách: 409 kèm đúng dòng vướng.
	bad := e.badHtaccess(t, m14Shop)
	rec := e.call("POST", path, `{"mode":"an-toan"}`, m14UserA)
	eb := apiDecode[ErrorBody](t, rec, 409)
	if eb.Error.Code != core.CodeHtaccessBlocked || !strings.Contains(eb.Error.Message.Vi, bad+":3: SetHandler") {
		t.Errorf("an-toan với .htaccess vướng: %s", rec.Body)
	}

	// Lối ra còn lại là day-du, và chỉ admin đi được.
	if code := apiErrCode(t, e.call("POST", path, `{"mode":"day-du"}`, m14UserA), 403); code != "forbidden" {
		t.Errorf("chủ đặt day-du: %s", code)
	}
	if j := e.job(t, path, `{"mode":"day-du"}`, account.AdminName); j.Status != engine.JobSucceeded || e.site(t, m14Shop).Htaccess != site.HtaccessFull {
		t.Fatalf("admin đặt day-du: %+v", j)
	}

	// Ma trận: dòng của shop là day-du kèm cảnh báo web.htaccess, và hiện nút.
	e.runHealth(t)
	iso := apiDecode[core.Isolation](t, e.call("GET", "/api/v1/security/isolation", "", account.AdminName), 200)
	i := slices.IndexFunc(iso.Htaccess, func(r core.HtaccessRow) bool { return r.Domain == m14Shop })
	if iso.Web != core.Apache || i < 0 || iso.Htaccess[i].Mode != site.HtaccessFull || iso.Htaccess[i].Result == nil || iso.Htaccess[i].Result.Status != checks.Warn || !iso.NeedsHarden {
		t.Errorf("ma trận sau day-du: web %s, needs_harden %v, htaccess %+v", iso.Web, iso.NeedsHarden, iso.Htaccess)
	}
	for _, r := range iso.Htaccess {
		if r.Domain != m14Shop && r.Domain != m14Lab && (r.Result == nil || r.Result.Status != checks.Pass) {
			t.Errorf("web.htaccess của %s: %+v", r.Domain, r.Result)
		}
	}

	// Plan "Áp bản vá cô lập" quét đúng file đó: shop được giữ, kèm dòng vướng.
	hp := apiDecode[HardenPreview](t, e.call("POST", "/api/v1/security/harden?dry_run=1", "", account.AdminName), 200)
	if len(hp.Switched) != 0 || len(hp.Kept) != 1 || hp.Kept[0].Domain != m14Shop || len(hp.Kept[0].Findings) != 1 ||
		hp.Kept[0].Findings[0].File != bad || hp.Kept[0].Findings[0].Line != 3 || hp.Kept[0].Findings[0].Directive != "SetHandler" {
		t.Errorf("harden xem trước: switched %v, kept %+v", hp.Switched, hp.Kept)
	}
	if jobs, _ := e.st.Jobs(ctx, 0); slices.ContainsFunc(jobs, func(j engine.Job) bool { return j.Kind == "security.harden" }) {
		t.Error("xem trước harden mà có job")
	}
}

// ---------------------------------------------------------------- /security/isolation

func TestM14IsolationTuKetQuaDaLuu(t *testing.T) {
	e := newM14Env(t)
	ctx := context.Background()
	get := func() core.Isolation {
		t.Helper()
		rec := e.call("GET", "/api/v1/security/isolation", "", account.AdminName)
		if strings.Contains(rec.Body.String(), ":null") {
			t.Errorf("JSON có null: %s", rec.Body)
		}
		return apiDecode[core.Isolation](t, rec, 200)
	}
	// Xếp theo tên. Ba site tên dài có từ M16 (dữ liệu demo cho ảnh và phép đo giao diện).
	sites := []string{m14Blog, "cua-hang-so-mot.khach-hang-vi-du.example.com", "hoc-truc-tuyen.lop-hoc-a.example.net", m14Lab, m14Shop, "tin-tuc.to-soan-bao-vi-du.example.org", m14Wiki}
	cells := len(sites) * (len(sites) - 1) // mọi cặp có thứ tự, trừ đường chéo

	// Health chưa chạy: đủ hàng, cột và chế độ .htaccess, ô chưa có kết quả.
	e.f.ResetRan()
	iso := get()
	if n := len(e.f.Ran()); n != 0 {
		t.Errorf("mở ma trận chạy %d lệnh; phải chỉ đọc kết quả đã lưu", n)
	}
	if iso.Web != core.Nginx || !slices.Equal(iso.Sites, sites) || len(iso.Cells) != cells || len(iso.Htaccess) != len(sites) {
		t.Fatalf("trước health: web %s, sites %v, %d ô, %d dòng htaccess", iso.Web, iso.Sites, len(iso.Cells), len(iso.Htaccess))
	}
	for _, c := range iso.Cells {
		if c.Result != nil || c.From == c.To {
			t.Errorf("ô %s→%s trước health: %+v", c.From, c.To, c.Result)
		}
	}
	for _, r := range iso.Htaccess {
		if r.Mode != site.HtaccessSafe || r.Result != nil {
			t.Errorf("htaccess %s = %q %+v", r.Domain, r.Mode, r.Result)
		}
	}
	if len(iso.Machine) != 0 || len(iso.PerSite) != 0 || iso.NeedsHarden {
		t.Errorf("trước health: machine %d, per_site %d, needs_harden %v", len(iso.Machine), len(iso.PerSite), iso.NeedsHarden)
	}

	// Sau health: mọi ô có kết quả đạt, đúng ID, kèm bằng chứng. Trừ hàng của
	// hai site trong lồng của demo (M17, internal/demo/m17.go): không có phép
	// cô lập đi từ site trong lồng (core.siteChecks), cage.fence thay cho nó.
	e.runHealth(t)
	iso = get()
	caged := []string{"cua-hang-so-mot.khach-hang-vi-du.example.com", "tin-tuc.to-soan-bao-vi-du.example.org"}
	for _, c := range iso.Cells {
		if slices.Contains(caged, c.From) {
			if c.Result != nil {
				t.Errorf("ô %s→%s (đi từ site trong lồng) có kết quả: %+v", c.From, c.To, c.Result)
			}
			continue
		}
		if c.Result == nil || c.Result.ID != "isolation:"+c.From+":"+c.To || c.Result.Status != checks.Pass || len(c.Result.Evidence) == 0 {
			t.Errorf("ô %s→%s sau health: %+v", c.From, c.To, c.Result)
		}
	}
	var machine, perSite []string
	for _, r := range iso.Machine {
		machine = append(machine, r.ID)
		if r.Status != checks.Pass {
			t.Errorf("%s = %s (%s)", r.ID, r.Status, r.Summary.Vi)
		}
	}
	for _, r := range iso.PerSite {
		perSite = append(perSite, r.ID)
	}
	slices.Sort(machine)
	if want := []string{"php.opcache:8.3", "php.protectproc:8.3", "web.default.rejects", "web.server_name.unique"}; !slices.Equal(machine, want) {
		t.Errorf("machine = %v; muốn %v", machine, want)
	}
	for _, d := range sites {
		if !slices.Contains(perSite, "log.perm:"+d) {
			t.Errorf("per_site thiếu log.perm:%s: %v", d, perSite)
		}
	}
	if !slices.Contains(perSite, "web.hardened:"+m14Shop) {
		t.Errorf("per_site thiếu web.hardened:%s: %v", m14Shop, perSite)
	}
	if iso.NeedsHarden {
		t.Errorf("máy demo đã siết mà needs_harden = true: %v %v", machine, perSite)
	}

	// Một phép kiểm mà Plan sửa được không đạt: hiện nút.
	bad := checks.Result{ID: "log.perm:" + m14Shop, Stage: checks.StageWeb, Status: checks.Fail}
	if _, err := e.st.RecordResults(ctx, m14Shop, []checks.Result{bad}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !get().NeedsHarden {
		t.Error("log.perm lỗi mà needs_harden = false")
	}
	bad.Status = checks.Pass
	if _, err := e.st.RecordResults(ctx, m14Shop, []checks.Result{bad}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Phép kiểm định tuyến lỗi thì Plan không sửa được (sửa tay): không hiện nút.
	if _, err := e.st.RecordResults(ctx, "", []checks.Result{{ID: "web.server_name.unique", Stage: checks.StageWeb, Status: checks.Fail}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if get().NeedsHarden {
		t.Error("web.server_name.unique lỗi mà needs_harden = true")
	}

	// Một site còn ở day-du: hiện nút, và dòng htaccess nói đúng chế độ.
	st, ok, err := e.st.Site(ctx, apiDomain(t, m14Shop))
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	st.Htaccess = site.HtaccessFull
	if err := e.st.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	iso = get()
	i := slices.IndexFunc(iso.Htaccess, func(r core.HtaccessRow) bool { return r.Domain == m14Shop })
	if !iso.NeedsHarden || i < 0 || iso.Htaccess[i].Mode != site.HtaccessFull {
		t.Errorf("site day-du: needs_harden %v, htaccess %+v", iso.NeedsHarden, iso.Htaccess)
	}

	// Stack Apache: dòng htaccess kèm kết quả web.htaccess đã lưu.
	if err := e.st.SetSetting(ctx, core.SettingWeb, string(core.Apache)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.RecordResults(ctx, m14Shop, []checks.Result{{ID: "web.htaccess:" + m14Shop, Stage: checks.StageWeb, Status: checks.Warn}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	iso = get()
	if r := iso.Htaccess[i].Result; iso.Web != core.Apache || r == nil || r.Status != checks.Warn {
		t.Errorf("apache: web %s, kết quả web.htaccess %+v", iso.Web, r)
	}

	// Không phải admin: 403 (ma trận lộ tên site của mọi người).
	if code := apiErrCode(t, e.call("GET", "/api/v1/security/isolation", "", m14UserA), 403); code != "forbidden" {
		t.Errorf("user thường: %s", code)
	}
}

// ---------------------------------------------------------------- /security/harden

func TestM14HardenTrenMayDemo(t *testing.T) {
	e := newM14Env(t)
	e.healWiki(t)
	ctx := context.Background()
	if code := apiErrCode(t, e.call("POST", "/api/v1/security/harden?dry_run=1", "", m14UserA), 403); code != "forbidden" {
		t.Errorf("user thường: %s", code)
	}

	// Một site còn ở day-du như máy nâng từ bản trước M14: .htaccess của site
	// mẫu sạch nên Plan chuyển nó sang an-toan.
	st, ok, err := e.st.Site(ctx, apiDomain(t, m14Shop))
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	st.Htaccess = site.HtaccessFull
	if err := e.st.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}

	rec := e.call("POST", "/api/v1/security/harden?dry_run=1", "", account.AdminName)
	pv := apiDecode[HardenPreview](t, rec, 200)
	if pv.Kind != "security.harden" || pv.Target != "isolation" || len(pv.Steps) == 0 {
		t.Fatalf("Preview = %s %s, %d bước", pv.Kind, pv.Target, len(pv.Steps))
	}
	if !slices.Equal(pv.Switched, []string{m14Shop}) || len(pv.Kept) != 0 {
		t.Errorf("switched %v, kept %+v", pv.Switched, pv.Kept)
	}
	// Hai danh sách luôn là mảng trong JSON, kể cả khi rỗng.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || string(raw["kept"]) != "[]" || raw["switched"] == nil || raw["steps"] == nil {
		t.Errorf("JSON xem trước: kept %s, switched %s (%v)", raw["kept"], raw["switched"], err)
	}
	if jobs, _ := e.st.Jobs(ctx, 0); slices.ContainsFunc(jobs, func(j engine.Job) bool { return j.Kind == "security.harden" }) {
		t.Fatal("xem trước mà có job")
	}
	if a := e.audit(t, "security.harden"); len(a) != 0 {
		t.Errorf("xem trước mà có nhật ký: %+v", a)
	}

	// Chạy thật: 202, job thành công, nhật ký, và site đã sang an-toan.
	js := apiDecode[JobStarted](t, e.call("POST", "/api/v1/security/harden", "", account.AdminName), 202)
	e.s.api.wg.Wait()
	job, err := e.st.Job(ctx, js.JobID)
	if err != nil || job.Kind != "security.harden" || job.Target != "isolation" || job.Status == engine.JobRunning || job.Actor != account.AdminName {
		t.Fatalf("job = %+v, %v", job, err)
	}
	if a := e.audit(t, "security.harden"); len(a) != 1 || a[0].JobID != js.JobID || a[0].Target != "isolation" {
		t.Errorf("audit = %+v", a)
	}
	if job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", job.Status, job.Error)
	}
	if st, _, _ := e.st.Site(ctx, apiDomain(t, m14Shop)); st.Htaccess != site.HtaccessSafe {
		t.Errorf("sau harden site vẫn ở %q", st.Htaccess)
	}
	// Health kiểm lại sau job: ma trận có kết quả và không còn cần nút.
	e.runHealth(t)
	iso := apiDecode[core.Isolation](t, e.call("GET", "/api/v1/security/isolation", "", account.AdminName), 200)
	if iso.NeedsHarden || len(iso.Machine) == 0 {
		t.Errorf("sau harden: needs_harden %v, machine %d", iso.NeedsHarden, len(iso.Machine))
	}
}
