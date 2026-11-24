package core

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// newNamesEnv: siteEnv với store thật làm SiteStore (cài NameStore), và
// curl/nginx -T giả đọc đúng các vhost đang bật trong hệ thống file giả.
func newNamesEnv(t *testing.T) *siteEnv {
	t.Helper()
	se := newSiteEnv(t, Nginx)
	se.s.Store = se.st
	m := se.m
	// nginx -T: in mọi file trong sites-enabled như nginx thật.
	m.Handle([]string{"nginx", "-T"}, func(system.Cmd) system.Result {
		var b strings.Builder
		// nginx thật luôn in nginx.conf trước, kể cả khi sites-enabled rỗng.
		b.WriteString("# configuration file /etc/nginx/nginx.conf:\n")
		names, _ := m.ReadDir("/etc/nginx/sites-enabled")
		for _, n := range names {
			p := "/etc/nginx/sites-enabled/" + n
			data, err := m.ReadFile(p)
			if err != nil {
				continue
			}
			b.WriteString("# configuration file " + p + ":\n" + string(data) + "\n")
		}
		return system.Result{Stdout: b.String()}
	})
	m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		r := argAfter(c.Args, "--resolve")
		host, _, _ := strings.Cut(r, ":")
		if !strings.Contains(argAfter(c.Args, "-w"), "TPAN_LOCATION") {
			// Như handler curl của siteSim (HTTPSite, siteGone, WPArchive).
			if r == "" {
				if p := argAfter(c.Args, "-o"); p != "" {
					m.SetFile(p, fakeWP, 0o600, system.Root)
				}
				return system.Result{}
			}
			if !m.served(host) {
				if slices.Contains(c.Args, "-o") {
					return system.Result{Stdout: "404"}
				}
				return system.Result{Stdout: "<html>404</html>TPAN_HTTP_CODE=404"}
			}
			if slices.Contains(c.Args, "-o") {
				return system.Result{Stdout: "200"}
			}
			return system.Result{Stdout: "<html><link href='http://" + host + "/wp-content/x.css'></html>TPAN_HTTP_CODE=200"}
		}
		// NameProbe/NameGone: tìm dòng server_name có host trong các vhost đang bật.
		names, _ := m.ReadDir("/etc/nginx/sites-enabled")
		for _, n := range names {
			data, err := m.ReadFile("/etc/nginx/sites-enabled/" + n)
			if err != nil {
				continue
			}
			primary := ""
			for _, ln := range strings.Split(string(data), "\n") {
				f := strings.Fields(strings.TrimSuffix(strings.TrimSpace(ln), ";"))
				if len(f) < 2 || f[0] != "server_name" {
					continue
				}
				if primary == "" {
					primary = f[1]
				}
				if !slices.Contains(f[1:], host) {
					continue
				}
				if f[1] == primary {
					return system.Result{Stdout: "<link href='/wp-content/x.css'>\nTPAN_LOCATION=\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=200"}
				}
				return system.Result{Stdout: "\nTPAN_LOCATION=http://" + primary + "/\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=301"}
			}
		}
		return system.Result{ExitCode: 52, Stderr: "curl: (52) Empty reply from server"}
	})
	return se
}

func nameCode(err error) string {
	var re *RefusedError
	if errors.As(err, &re) {
		return re.Code
	}
	return ""
}

// addSite tạo site qua đường site add thật (store thật), kiểu www tuỳ chọn.
func (se *siteEnv) addSite(t *testing.T, dom, owner string, www site.NameMode) SiteAddResult {
	t.Helper()
	in := se.input(t, dom)
	in.Owner, in.WWW = owner, www
	a, err := se.s.PrepareAdd(context.Background(), in)
	if err != nil {
		t.Fatalf("PrepareAdd %s: %v", dom, err)
	}
	res, err := a.Execute(context.Background(), nil)
	if err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("site add %s: %v; %s %s; kiểm %s", dom, err, res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
	return res
}

func rowsOf(t *testing.T, se *siteEnv, dom string) map[string]site.Name {
	t.Helper()
	rows, err := se.st.NamesOf(context.Background(), mustDomain(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]site.Name{}
	for _, r := range rows {
		out[r.Name.String()] = r
	}
	return out
}

func vhostOf(t *testing.T, se *siteEnv, dom string) string {
	t.Helper()
	b, err := se.m.ReadFile(site.LayoutOf(mustDomain(t, dom)).NginxConf)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func execNames(t *testing.T, op *NamesOp, err error) NamesResult {
	t.Helper()
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	res, err := op.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return res
}

func TestSiteAddDefaultWWW(t *testing.T) {
	for _, c := range []struct {
		dom, www string
	}{
		{"example.com", "www.example.com"},
		{"www.example.org", "example.org"},
		{"blog.example.net", ""},
	} {
		t.Run(c.dom, func(t *testing.T) {
			se := newNamesEnv(t)
			res := se.addSite(t, c.dom, "", "")
			rows := rowsOf(t, se, c.dom)
			if c.www == "" {
				if len(rows) != 1 || len(res.Site.Names.Redirect) != 0 {
					t.Fatalf("tên con không được có tên phụ: %v", rows)
				}
				return
			}
			r, ok := rows[c.www]
			if !ok || r.Kind != site.KindAlias || r.Mode != site.ModeRedirect || r.State != site.StateActive {
				t.Fatalf("dòng %s = %+v (có %v)", c.www, r, ok)
			}
			if p := rows[c.dom]; p.State != site.StateActive || p.Kind != site.KindPrimary {
				t.Errorf("tên chính %+v", p)
			}
			if !strings.Contains(vhostOf(t, se, c.dom), c.www) {
				t.Error("vhost không có tên www")
			}
			st, _, _ := se.st.Site(context.Background(), mustDomain(t, c.dom))
			if !slices.Contains(st.Names.Redirect, mustDomain(t, c.www)) {
				t.Errorf("store.Site().Names = %+v", st.Names)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		se := newNamesEnv(t)
		se.addSite(t, "example.com", "", WWWNone)
		if rows := rowsOf(t, se, "example.com"); len(rows) != 1 {
			t.Errorf("--www none mà có %v", rows)
		}
	})
	t.Run("kiểu lạ", func(t *testing.T) {
		se := newNamesEnv(t)
		in := se.input(t, "example.com")
		in.WWW = "x"
		if _, err := se.s.PrepareAdd(context.Background(), in); nameCode(err) != CodeNameModeInvalid {
			t.Errorf("--www x: %v", err)
		}
	})
}

func TestSiteAddRerunAddsNoAlias(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	a, err := se.s.PrepareAdd(context.Background(), se.input(t, "example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Rerun || len(a.claims) != 0 {
		t.Fatalf("chạy lại giữ chỗ %v", a.claims)
	}
	for _, s := range a.Plan.Steps {
		if _, ok := s.(*steps.NameProbe); ok {
			t.Error("chạy lại có bước kiểm tên phụ")
		}
	}
	if _, err := a.Execute(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if rows := rowsOf(t, se, "example.com"); len(rows) != 1 {
		t.Errorf("chạy lại thêm tên: %v", rows)
	}
}

func TestSiteAddForeignRootRefused(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "khach_a", WWWNone)
	se.s.NotAdmin = true
	in := se.input(t, "shop.example.com")
	in.Owner = "khach_b"
	_, err := se.s.PrepareAdd(context.Background(), in)
	if nameCode(err) != "site_not_found" {
		t.Fatalf("user thường tạo site dưới tên miền gốc của người khác: %v", err)
	}
	// Admin được miễn luật tên miền gốc (S1, như M28).
	se.s.NotAdmin = false
	if _, err := se.s.PrepareAdd(context.Background(), in); err != nil {
		t.Errorf("admin: %v", err)
	}
}

func TestAliasAdd(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	op, err := se.s.PrepareAliasAdd(ctx, d, mustDomain(t, "old.example.net"), site.ModeRedirect)
	if err != nil {
		t.Fatal(err)
	}
	// Xem trước không giữ chỗ.
	if _, err := op.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := rowsOf(t, se, "example.com")["old.example.net"]; ok {
		t.Fatal("xem trước đã giữ chỗ")
	}
	res := execNames(t, op, nil)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", res.Report.Job.Status, res.Report.Job.Error)
	}
	if r := rowsOf(t, se, "example.com")["old.example.net"]; r.State != site.StateActive || r.Mode != site.ModeRedirect {
		t.Errorf("dòng %+v", r)
	}
	if res.NeedsCert {
		t.Error("site không SSL mà NeedsCert")
	}
	// Đổi kiểu sang serve.
	op2, err2 := se.s.PrepareAliasMode(ctx, d, mustDomain(t, "old.example.net"), site.ModeServe)
	res = execNames(t, op2, err2)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("mode: %s %s", res.Report.Job.Status, res.Report.Job.Error)
	}
	if r := rowsOf(t, se, "example.com")["old.example.net"]; r.Mode != site.ModeServe {
		t.Errorf("mode %+v", r)
	}
	// Thêm lại: alias_exists.
	if _, err := se.s.PrepareAliasAdd(ctx, d, mustDomain(t, "old.example.net"), site.ModeServe); nameCode(err) != site.CodeAliasExists {
		t.Errorf("thêm lại: %v", err)
	}
	if _, err := se.s.PrepareAliasAdd(ctx, d, d, site.ModeServe); nameCode(err) != CodeAliasPrimary {
		t.Errorf("tên chính: %v", err)
	}
	if _, err := se.s.PrepareAliasAdd(ctx, d, mustDomain(t, "x.example.net"), "bad"); nameCode(err) != CodeNameModeInvalid {
		t.Errorf("kiểu lạ: %v", err)
	}
}

func TestAliasAddForeignRootRefused(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "khach_a", WWWNone)
	se.addSite(t, "example.net", "khach_b", WWWNone)
	ctx := context.Background()
	se.s.NotAdmin = true
	for _, n := range []string{"shop.example.com", "example.com"} {
		_, err := se.s.PrepareAliasAdd(ctx, mustDomain(t, "example.net"), mustDomain(t, n), site.ModeRedirect)
		if nameCode(err) != CodeNameUnavailable {
			t.Errorf("%s: %v", n, err)
		}
		if err != nil && strings.Contains(err.Error(), "khach_a") {
			t.Errorf("lộ tên chủ: %v", err)
		}
	}
	se.s.NotAdmin = false
	if _, err := se.s.PrepareAliasAdd(ctx, mustDomain(t, "example.net"), mustDomain(t, "example.com"), site.ModeRedirect); nameCode(err) != site.CodeNameTaken {
		t.Errorf("admin, tên của site khác: %v", err)
	}
	// Admin miễn luật tên miền gốc, không miễn luật một tên một site.
	if _, err := se.s.PrepareAliasAdd(ctx, mustDomain(t, "example.net"), mustDomain(t, "shop.example.com"), site.ModeRedirect); err != nil {
		t.Errorf("admin, tên con của tên miền gốc khác: %v", err)
	}
}

func TestAliasAddPublicSuffixRefused(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "khach_a", WWWNone)
	se.s.NotAdmin = true
	_, err := se.s.PrepareAliasAdd(context.Background(), mustDomain(t, "example.com"), mustDomain(t, "com.vn"), site.ModeRedirect)
	if nameCode(err) != site.CodeNamePublicSuffix {
		t.Errorf("com.vn: %v", err)
	}
}

func TestAliasAddSuspendedOrTrashed(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	st, _, _ := se.st.Site(ctx, d)
	st.Suspended = account.SuspendSelf
	if err := se.st.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	name := mustDomain(t, "www.example.com")
	if _, err := se.s.PrepareAliasAdd(ctx, d, name, site.ModeRedirect); nameCode(err) != CodeSiteSuspended {
		t.Errorf("tạm khoá: %v", err)
	}
	if _, err := se.s.PrepareAliasRemove(ctx, d, name); nameCode(err) != CodeSiteSuspended {
		t.Errorf("tạm khoá, gỡ: %v", err)
	}
	st.Suspended, st.Status = account.NotSuspended, site.Trashed
	if err := se.st.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := se.s.PrepareAliasMode(ctx, d, name, site.ModeServe); nameCode(err) != CodeSiteNotActive {
		t.Errorf("thùng rác: %v", err)
	}
}

func TestAliasAddDeclaredElsewhere(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	ctx := context.Background()
	d, name := mustDomain(t, "example.com"), mustDomain(t, "webmail.example.com")
	stray := "server {\n    listen 80;\n    server_name webmail.example.com;\n}\n"
	se.m.SetFile("/etc/nginx/sites-enabled/webmail.conf", []byte(stray), 0o644, system.Root)
	if _, err := se.s.PrepareAliasAdd(ctx, d, name, site.ModeServe); nameCode(err) != site.CodeNameTaken {
		t.Errorf("admin: %v", err)
	}
	se.s.NotAdmin = true
	if _, err := se.s.PrepareAliasAdd(ctx, d, name, site.ModeServe); nameCode(err) != CodeNameUnavailable {
		t.Errorf("user: %v", err)
	}
	se.s.NotAdmin = false

	// File lạ xuất hiện giữa Prepare và Execute: bước chặn chạy dưới khoá
	// engine từ chối, job hoàn tác, chỗ giữ được nhả.
	if err := se.m.Remove("/etc/nginx/sites-enabled/webmail.conf"); err != nil {
		t.Fatal(err)
	}
	op, err := se.s.PrepareAliasAdd(ctx, d, name, site.ModeServe)
	if err != nil {
		t.Fatal(err)
	}
	before := vhostOf(t, se, "example.com")
	se.m.SetFile("/etc/nginx/sites-enabled/webmail.conf", []byte(stray), 0o644, system.Root)
	res, err := op.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Job.Status == engine.JobSucceeded || !strings.Contains(res.Report.Job.Error, "webmail.conf") {
		t.Fatalf("job phải hỏng vì file lạ: %s %s", res.Report.Job.Status, res.Report.Job.Error)
	}
	if _, ok := rowsOf(t, se, "example.com")["webmail.example.com"]; ok {
		t.Error("chỗ giữ chưa nhả")
	}
	if vhostOf(t, se, "example.com") != before {
		t.Error("vhost đổi")
	}
}

func TestAliasRm(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", "")
	ctx := context.Background()
	d, www := mustDomain(t, "example.com"), mustDomain(t, "www.example.com")
	if _, err := se.s.PrepareAliasRemove(ctx, d, d); nameCode(err) != CodeAliasPrimary {
		t.Errorf("gỡ tên chính: %v", err)
	}
	if _, err := se.s.PrepareAliasRemove(ctx, d, mustDomain(t, "none.example.com")); nameCode(err) != CodeAliasNotFound {
		t.Errorf("gỡ tên không có: %v", err)
	}
	op2, err2 := se.s.PrepareAliasRemove(ctx, d, www)
	res := execNames(t, op2, err2)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s; kiểm %s", res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
	if _, ok := rowsOf(t, se, "example.com")["www.example.com"]; ok {
		t.Error("dòng còn")
	}
	if strings.Contains(vhostOf(t, se, "example.com"), "www.example.com") {
		t.Error("vhost còn tên")
	}
}

func TestAliasRmPendingOrphan(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	ctx := context.Background()
	d, n := mustDomain(t, "example.com"), mustDomain(t, "orphan.example.com")
	if err := se.st.ClaimNames(ctx, []site.Claim{{Name: n, Site: d, Owner: "admin", Kind: site.KindAlias, Mode: site.ModeServe}}); err != nil {
		t.Fatal(err)
	}
	op2, err2 := se.s.PrepareAliasRemove(ctx, d, n)
	res := execNames(t, op2, err2)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", res.Report.Job.Status, res.Report.Job.Error)
	}
	if _, ok := rowsOf(t, se, "example.com")[n.String()]; ok {
		t.Error("dòng pending mồ côi còn")
	}
}

// B6: vhost chỉ vẽ tên active, cộng đúng thay đổi của job đang chạy.
func TestVhostSkipsPendingNames(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	pend := mustDomain(t, "pending.example.com")
	if err := se.st.ClaimNames(ctx, []site.Claim{{Name: pend, Site: d, Owner: "admin", Kind: site.KindAlias, Mode: site.ModeServe}}); err != nil {
		t.Fatal(err)
	}
	op, err := se.s.PrepareAliasAdd(ctx, d, mustDomain(t, "www.example.com"), site.ModeRedirect)
	if err != nil {
		t.Fatal(err)
	}
	all := slices.Concat(op.next.Names.Serve, op.next.Names.Redirect)
	if slices.Contains(all, pend) || !slices.Contains(all, mustDomain(t, "www.example.com")) {
		t.Fatalf("next.Names = %+v", op.next.Names)
	}
	res := execNames(t, op, nil)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", res.Report.Job.Status, res.Report.Job.Error)
	}
	v := vhostOf(t, se, "example.com")
	if strings.Contains(v, pend.String()) || !strings.Contains(v, "www.example.com") {
		t.Errorf("vhost:\n%s", v)
	}
}

func TestNameList(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", "")
	se.m.Handle([]string{"getent", "ahostsv4"}, func(c system.Cmd) system.Result {
		switch c.Args[1] {
		case "example.com":
			return system.Result{Stdout: "203.0.113.7 STREAM example.com\n"}
		case "www.example.com":
			return system.Result{Stdout: "198.51.100.9 STREAM www.example.com\n"}
		}
		return system.Result{ExitCode: 2}
	})
	se.m.Handle([]string{"ip", "-o", "addr"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "2: eth0    inet 203.0.113.7/24 brd 203.0.113.255 scope global eth0\n"}
	})
	got, err := se.s.NameList(context.Background(), mustDomain(t, "example.com"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"example.com": "ok", "www.example.com": "warn"}
	if len(got) != 2 {
		t.Fatalf("NameList = %+v", got)
	}
	for _, v := range got {
		if want[v.Name] != v.DNS || v.State != site.StateActive {
			t.Errorf("%+v", v)
		}
	}
}

// B9: gỡ tên phụ đang nằm trong chứng chỉ thì kết quả đòi xin lại chứng chỉ.
func TestAliasRmNeedsCert(t *testing.T) {
	se := newNamesEnv(t)
	se.addSite(t, "example.com", "", "")
	ctx := context.Background()
	d, www := mustDomain(t, "example.com"), mustDomain(t, "www.example.com")
	st, _, _ := se.st.Site(ctx, d)
	st.SSL = true
	if err := se.st.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := se.st.SetInCert(ctx, d, []validate.Domain{d, www}); err != nil {
		t.Fatal(err)
	}
	op, err := se.s.PrepareAliasRemove(ctx, d, www)
	if err != nil {
		t.Fatal(err)
	}
	if !op.needsCert || slices.Contains(op.next.Names.Covered, www) {
		t.Errorf("needsCert=%v Covered=%v", op.needsCert, op.next.Names.Covered)
	}
	pv, err := op.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	last := pv.Steps[len(pv.Steps)-1].Info
	if !strings.HasPrefix(last.ID, "followup.ssl:") || strings.Contains(last.Title.Vi, "www.example.com") {
		t.Errorf("dòng ghi chú SSL: %+v", last)
	}
	// Thêm tên mới cho site SSL: tên chưa phủ, cũng cần chứng chỉ.
	op, err = se.s.PrepareAliasAdd(ctx, d, mustDomain(t, "old.example.net"), site.ModeRedirect)
	if err != nil {
		t.Fatal(err)
	}
	if !op.needsCert || op.sslNote == nil || !strings.Contains(op.sslNote.Vi, "old.example.net") {
		t.Errorf("thêm tên cho site SSL: needsCert=%v note=%v", op.needsCert, op.sslNote)
	}
}
