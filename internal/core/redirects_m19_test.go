package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// redirectSys bọc hệ thống giả của newNamesEnv: curl của RedirectProbe (đường
// dẫn khác "/") được trả lời theo các khối "location = <nguồn>" trong vhost
// nginx đang bật, như nginx thật; mọi lệnh khác đi tiếp xuống hệ thống giả.
type redirectSys struct {
	system.System
	m *siteSim
	t *testing.T
	// wrongLocation: giả một luật khác trả lời trước (Location sai).
	wrongLocation bool
}

func (r *redirectSys) Run(ctx context.Context, c system.Cmd) (system.Result, error) {
	if c.Name != "curl" || len(c.Args) == 0 || !strings.Contains(argAfter(c.Args, "-w"), "TPAN_LOCATION") {
		return r.System.Run(ctx, c)
	}
	url := c.Args[len(c.Args)-1]
	_, rest, _ := strings.Cut(url, "://")
	host, path, _ := strings.Cut(rest, "/")
	if path == "" {
		return r.System.Run(ctx, c)
	}
	data, err := r.m.ReadFile(site.LayoutOf(mustDomain(r.t, host)).NginxConf)
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for i, ln := range lines {
			if strings.TrimSpace(ln) != "location = /"+path+" {" || i+1 >= len(lines) {
				continue
			}
			f := strings.Fields(strings.TrimSuffix(strings.TrimSpace(lines[i+1]), ";"))
			if len(f) != 3 || f[0] != "return" {
				break
			}
			loc := strings.TrimSuffix(f[2], "$is_args$args")
			if r.wrongLocation {
				loc = "http://elsewhere.example.net/"
			}
			return system.Result{Stdout: "\nTPAN_LOCATION=" + loc + "\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=" + f[1]}, nil
		}
	}
	// Không luật nào: WordPress trả 404.
	return system.Result{Stdout: "<html>404</html>\nTPAN_LOCATION=\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=404"}, nil
}

func newRedirectEnv(t *testing.T) (*siteEnv, *redirectSys) {
	t.Helper()
	se := newNamesEnv(t)
	rs := &redirectSys{System: se.m, m: se.m, t: t}
	se.s.Sys = rs
	se.addSite(t, "example.com", "", "")
	return se, rs
}

func rin(src, tgt string) RedirectInput { return RedirectInput{Source: src, Target: tgt, Code: 301} }

// addRule thêm một luật qua đúng đường Plan và đòi job thành công.
func addRule(t *testing.T, se *siteEnv, d validate.Domain, in RedirectInput) {
	t.Helper()
	op, err := se.s.PrepareRedirectAdd(context.Background(), d, in)
	if res := execNames(t, op, err); res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("thêm luật %s: %s %s; kiểm %s", in.Source, res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
}

func redirectField(err error) string {
	var re *validate.RedirectError
	if errors.As(err, &re) {
		return re.Field
	}
	return ""
}

func TestRedirectAdd(t *testing.T) {
	se, _ := newRedirectEnv(t)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	in := rin("/old", "/new")
	in.KeepQuery = true
	op, err := se.s.PrepareRedirectAdd(ctx, d, in)
	if err != nil {
		t.Fatal(err)
	}
	pv, err := op.Preview(ctx)
	if err != nil || pv.Kind != "site.redirect.add" {
		t.Fatalf("Preview = %+v, %v", pv, err)
	}
	if got, _ := se.s.RedirectList(ctx, d); len(got) != 0 {
		t.Fatalf("xem trước đã ghi luật: %+v", got)
	}
	res := execNames(t, op, nil)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s; kiểm %s", res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
	got, err := se.s.RedirectList(ctx, d)
	if err != nil || len(got) != 1 || got[0].ID == 0 || got[0].Source.String() != "/old" || !got[0].KeepQuery {
		t.Fatalf("RedirectList = %+v, %v", got, err)
	}
	if v := vhostOf(t, se, "example.com"); !strings.Contains(v, "location = /old {") {
		t.Errorf("vhost không có luật:\n%s", v)
	}
	if res.NeedsCert {
		t.Error("luật chuyển hướng không cần chứng chỉ")
	}
	// Thêm lại cùng nguồn: redirect_exists.
	if _, err := se.s.PrepareRedirectAdd(ctx, d, rin("/old", "/other")); nameCode(err) != CodeRedirectExists {
		t.Errorf("trùng nguồn: %v", err)
	}
}

// TestRedirectAddInvalid: chuỗi lạ từ CLI (không qua server) bị validate chặn,
// lỗi giữ trường hỏng.
func TestRedirectAddInvalid(t *testing.T) {
	se, _ := newRedirectEnv(t)
	d := mustDomain(t, "example.com")
	for _, c := range []struct {
		in    RedirectInput
		field string
	}{
		{rin("/a;id", "/new"), validate.FieldSource},
		{rin("/$(id)", "/new"), validate.FieldSource},
		{rin("/../../etc/passwd", "/new"), validate.FieldSource},
		{rin("/index.php", "/new"), validate.FieldSource},
		{rin("/old", "/a\nreturn 200"), validate.FieldTarget},
		{rin("/old", "javascript:alert(1)"), validate.FieldTarget},
		{RedirectInput{Source: "/old", Target: "/new", Code: 303}, "code"},
		{RedirectInput{Source: "/old", Target: "/new"}, "code"},
	} {
		if _, err := se.s.PrepareRedirectAdd(context.Background(), d, c.in); redirectField(err) != c.field {
			t.Errorf("%+v: %v, muốn field %s", c.in, err, c.field)
		}
	}
}

// TestRedirectAddProbeFails: luật không có hiệu lực (Location sai) thì job
// hỏng, vhost về như cũ, chỗ giữ được nhả.
func TestRedirectAddProbeFails(t *testing.T) {
	se, rs := newRedirectEnv(t)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	before := vhostOf(t, se, "example.com")
	rs.wrongLocation = true
	op, err := se.s.PrepareRedirectAdd(ctx, d, rin("/old", "/new"))
	res := execNames(t, op, err)
	if res.Report.Job.Status == engine.JobSucceeded {
		t.Fatal("job phải hỏng")
	}
	if vhostOf(t, se, "example.com") != before {
		t.Error("vhost không được trả về")
	}
	if op.rule.ID == 0 {
		t.Fatal("chưa giữ chỗ")
	}
	if err := se.st.ActivateRedirect(ctx, op.rule.ID); err == nil {
		t.Error("chỗ giữ chưa nhả")
	}
}

// TestRedirectAddRace: hai request cùng nguồn qua Prepare; cái thứ hai bị
// chặn lúc giữ chỗ trong transaction.
func TestRedirectAddRace(t *testing.T) {
	se, _ := newRedirectEnv(t)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	a, err := se.s.PrepareRedirectAdd(ctx, d, rin("/old", "/a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := se.s.PrepareRedirectAdd(ctx, d, rin("/old", "/b"))
	if err != nil {
		t.Fatal(err)
	}
	execNames(t, a, nil)
	if _, err := b.Execute(ctx, nil); nameCode(err) != CodeRedirectExists {
		t.Errorf("request thua: %v", err)
	}
}

func TestRedirectRm(t *testing.T) {
	se, _ := newRedirectEnv(t)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	addRule(t, se, d, rin("/old", "/new"))
	if _, err := se.s.PrepareRedirectRemove(ctx, d, 424242); nameCode(err) != CodeRedirectNotFound {
		t.Errorf("luật không có: %v", err)
	}
	rules, _ := se.s.RedirectList(ctx, d)
	op, err := se.s.PrepareRedirectRemove(ctx, d, rules[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if op.plan.Kind != "site.redirect.rm" {
		t.Errorf("Kind = %s", op.plan.Kind)
	}
	res := execNames(t, op, nil)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s; kiểm %s", res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
	if got, _ := se.s.RedirectList(ctx, d); len(got) != 0 {
		t.Errorf("còn luật: %+v", got)
	}
	if strings.Contains(vhostOf(t, se, "example.com"), "location = /old") {
		t.Error("vhost còn luật")
	}
}

// B6 cho luật: vhost chỉ vẽ luật active cộng đúng luật của job.
func TestRedirectVhostSkipsPending(t *testing.T) {
	se, _ := newRedirectEnv(t)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	src, _ := validate.ParseRedirectSource("/pending")
	tgt, _ := validate.ParseRedirectTarget("/x")
	if _, err := se.st.AddRedirectPending(ctx, d, site.Redirect{Source: src, Target: tgt, Code: 301}); err != nil {
		t.Fatal(err)
	}
	op, err := se.s.PrepareRedirectAdd(ctx, d, rin("/old", "/new"))
	if err != nil {
		t.Fatal(err)
	}
	if r := op.next.Names.Rules; len(r) != 1 || r[0].Source.String() != "/old" {
		t.Fatalf("next.Rules = %+v", r)
	}
	execNames(t, op, nil)
	if v := vhostOf(t, se, "example.com"); strings.Contains(v, "/pending") {
		t.Errorf("vhost có luật pending:\n%s", v)
	}
}

func loopSite(t *testing.T) site.Site {
	st := site.Site{Domain: mustDomain(t, "example.com")}
	st.Names.Redirect = []validate.Domain{mustDomain(t, "www.example.com")}
	st.Names.Serve = []validate.Domain{mustDomain(t, "shop.example.net")}
	return st
}

func mkRule(t *testing.T, id int64, src, tgt string) site.Redirect {
	t.Helper()
	s, err := validate.ParseRedirectSource(src)
	if err != nil {
		t.Fatal(err)
	}
	d, err := validate.ParseRedirectTarget(tgt)
	if err != nil {
		t.Fatal(err)
	}
	return site.Redirect{ID: id, Source: s, Target: d, Code: 301}
}

func TestRedirectLoop(t *testing.T) {
	st := loopSite(t)
	for _, c := range []struct {
		src, tgt string
		loop     bool
	}{
		{"/old", "/old", true},
		{"/old", "/old?x=1", true}, // S15: bỏ query trước khi so
		{"/old", "http://example.com/old", true},
		{"/old", "https://www.example.com/old?a=b", true}, // tên phụ redirect
		{"/old", "https://shop.example.net/old", true},    // tên phụ serve
		{"/old", "http://example.com:8080/old", true},     // cổng bỏ qua: từ chối thừa
		{"/old", "https://example.org/old", false},        // site khác
		{"/old", "/old/", false},                          // location = so đúng từng byte
		{"/old", "/new", false},
		{"/old", "http://example.com", false},
	} {
		err := checkRedirect(st, nil, mkRule(t, 0, c.src, c.tgt))
		if (nameCode(err) == CodeRedirectLoop) != c.loop {
			t.Errorf("%s → %s: %v, muốn vòng=%v", c.src, c.tgt, err, c.loop)
		}
	}
	// S20: hai luật chuyển qua lại.
	rules := []site.Redirect{mkRule(t, 7, "/a", "/b"), mkRule(t, 8, "/c", "https://example.org/d")}
	for _, c := range []struct {
		src, tgt string
		loop     bool
	}{
		{"/b", "/a", true},
		{"/b", "https://example.com/a?x=1", true},
		{"/b", "/c", false},
		{"/d", "/c", false}, // luật 8 ra site khác
		{"/x", "/a", false},
	} {
		err := checkRedirect(st, rules, mkRule(t, 0, c.src, c.tgt))
		if (nameCode(err) == CodeRedirectLoop) != c.loop {
			t.Errorf("S20 %s → %s: %v, muốn vòng=%v", c.src, c.tgt, err, c.loop)
		}
	}
	// Qua Prepare: cùng mã.
	se, _ := newRedirectEnv(t)
	if _, err := se.s.PrepareRedirectAdd(context.Background(), mustDomain(t, "example.com"), rin("/old", "/old?x=1")); nameCode(err) != CodeRedirectLoop {
		t.Errorf("Prepare: %v", err)
	}
}

func TestRedirectQueryConflict(t *testing.T) {
	st := loopSite(t)
	r := mkRule(t, 0, "/old", "/new?a=1")
	r.KeepQuery = true
	if err := checkRedirect(st, nil, r); nameCode(err) != CodeRedirectQueryConflict {
		t.Errorf("giữ query khi đích có query: %v", err)
	}
	r = mkRule(t, 0, "/old", "https://example.org/new")
	r.KeepQuery = true
	if err := checkRedirect(st, nil, r); err != nil {
		t.Errorf("giữ query, đích không query: %v", err)
	}
}

func TestRedirectLimit(t *testing.T) {
	st := loopSite(t)
	var rules []site.Redirect
	for i := range site.MaxRedirects {
		rules = append(rules, mkRule(t, int64(i+1), fmt.Sprintf("/a%d", i), "/b"))
	}
	if err := checkRedirect(st, rules[:site.MaxRedirects-1], mkRule(t, 0, "/new", "/b")); err != nil {
		t.Errorf("luật thứ %d: %v", site.MaxRedirects, err)
	}
	if err := checkRedirect(st, rules, mkRule(t, 0, "/new", "/b")); nameCode(err) != CodeRedirectLimit {
		t.Errorf("luật thứ %d: %v", site.MaxRedirects+1, err)
	}
}

func TestRedirectSuspendedOrTrashed(t *testing.T) {
	se, _ := newRedirectEnv(t)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	addRule(t, se, d, rin("/old", "/new"))
	rules, _ := se.s.RedirectList(ctx, d)
	st, _, _ := se.st.Site(ctx, d)
	st.Suspended = account.SuspendSelf
	if err := se.st.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := se.s.PrepareRedirectAdd(ctx, d, rin("/x", "/y")); nameCode(err) != CodeSiteSuspended {
		t.Errorf("tạm khoá, thêm: %v", err)
	}
	if _, err := se.s.PrepareRedirectRemove(ctx, d, rules[0].ID); nameCode(err) != CodeSiteSuspended {
		t.Errorf("tạm khoá, gỡ: %v", err)
	}
	st.Suspended, st.Status = account.NotSuspended, site.Trashed
	if err := se.st.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := se.s.PrepareRedirectAdd(ctx, d, rin("/x", "/y")); nameCode(err) != CodeSiteNotActive {
		t.Errorf("thùng rác, thêm: %v", err)
	}
	if _, err := se.s.PrepareRedirectRemove(ctx, d, rules[0].ID); nameCode(err) != CodeSiteNotActive {
		t.Errorf("thùng rác, gỡ: %v", err)
	}
	// Xem danh sách vẫn được.
	if got, err := se.s.RedirectList(ctx, d); err != nil || len(got) != 1 {
		t.Errorf("RedirectList ở thùng rác: %+v, %v", got, err)
	}
}
