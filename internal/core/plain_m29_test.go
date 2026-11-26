package core

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// newPlainEnv: newNamesEnv biết thêm lệnh đặt trang mẫu của site PHP trống
// (dd of=<đích>, chạy dưới user của site).
func newPlainEnv(t *testing.T) *siteEnv {
	t.Helper()
	se := newNamesEnv(t)
	m := se.m
	m.Handle([]string{"dd"}, func(c system.Cmd) system.Result {
		m.SetFile(strings.TrimPrefix(c.Args[0], "of="), c.Stdin, 0o640, system.Owner{User: c.AsUser, Group: c.AsUser})
		return system.Result{}
	})
	return se
}

// M29: site PHP trống. Plan tạo site không có bước nào của WordPress, không
// cần email, và mật khẩu DB tới tay người tạo đúng một lần.
func TestSiteAddPlainPlan(t *testing.T) {
	se := newPlainEnv(t)
	in := se.input(t, "example.com")
	in.App, in.Email = site.AppPHP, validate.Email{}
	a, err := se.s.PrepareAdd(context.Background(), in)
	if err != nil {
		t.Fatalf("PrepareAdd: %v", err)
	}
	if !a.Site.Plain() || a.install != nil {
		t.Fatalf("site %+v, install %v", a.Site.App, a.install)
	}
	index := false
	for _, s := range a.Plan.Steps {
		switch s := s.(type) {
		case *steps.WPArchive, *steps.WPFiles, *steps.WPConfig, *steps.WPConstant, *wpInstallStep:
			t.Errorf("Plan của site PHP trống có bước WordPress %T", s)
		case *steps.PlainIndex:
			index = true
		case *steps.NameProbe:
			if !s.Plain {
				t.Error("NameProbe của site PHP trống không bật Plain")
			}
		}
	}
	if !index {
		t.Error("thiếu bước đặt trang mẫu")
	}
	res, err := a.Execute(context.Background(), nil)
	if err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("site add: %v; %s %s; kiểm %s", err, res.Report.Job.Status, res.Report.Job.Error, failedChecks(res.Report.Checks))
	}
	if res.WPInstalled || res.DBPassword.Reveal() == "" || res.DBName != a.Layout.DB {
		t.Errorf("kết quả: wp=%v db=%q mật khẩu rỗng=%v", res.WPInstalled, res.DBName, res.DBPassword.Reveal() == "")
	}
	st, ok, err := se.st.Site(context.Background(), mustDomain(t, "example.com"))
	if err != nil || !ok || !st.Plain() {
		t.Fatalf("dòng sites: %+v %v %v", st.App, ok, err)
	}
	// Chạy lại: giữ loại, không đòi email, không trả mật khẩu DB lần nữa.
	b, err := se.s.PrepareAdd(context.Background(), SiteInput{Domain: mustDomain(t, "example.com")})
	if err != nil {
		t.Fatalf("chạy lại: %v", err)
	}
	if !b.Rerun || !b.Site.Plain() || b.install != nil {
		t.Fatalf("chạy lại: rerun=%v app=%q", b.Rerun, b.Site.App)
	}
	for _, st := range b.Plan.Steps {
		if _, ok := st.(*steps.PlainIndex); ok {
			t.Error("chạy lại mà có bước đặt trang mẫu")
		}
	}
	res2, err := b.Execute(context.Background(), nil)
	if err != nil || res2.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy lại: %v %s %s", err, res2.Report.Job.Status, failedChecks(res2.Report.Checks))
	}
	if res2.DBPassword.Reveal() != "" {
		t.Error("chạy lại mà trả mật khẩu DB")
	}
}

func TestSiteAddPlainRefusals(t *testing.T) {
	se := newPlainEnv(t)
	in := se.input(t, "example.com")
	in.App = "drupal"
	if _, err := se.s.PrepareAdd(context.Background(), in); nameCode(err) != "app_invalid" {
		t.Errorf("app lạ: %v", err)
	}
	in = se.input(t, "example.com")
	in.PHPVersion = "9.9"
	if _, err := se.s.PrepareAdd(context.Background(), in); nameCode(err) != "php_not_installed" {
		t.Errorf("bản PHP chưa cài: %v", err)
	}
	in.PHPVersion = "8.3; rm"
	if _, err := se.s.PrepareAdd(context.Background(), in); nameCode(err) != "php_version_invalid" {
		t.Errorf("bản PHP sai dạng: %v", err)
	}
	// WordPress vẫn đòi email.
	in = se.input(t, "example.com")
	in.Email = validate.Email{}
	if _, err := se.s.PrepareAdd(context.Background(), in); nameCode(err) != validate.CodeEmailInvalid {
		t.Errorf("WordPress thiếu email: %v", err)
	}
}

// Đặt lại mật khẩu WordPress không có nghĩa với site PHP trống.
func TestWPPasswordPlainRefused(t *testing.T) {
	se := newPlainEnv(t)
	in := se.input(t, "example.com")
	in.App = site.AppPHP
	a, err := se.s.PrepareAdd(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := a.Execute(context.Background(), nil); err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("site add: %v %s", err, failedChecks(res.Report.Checks))
	}
	if _, err := se.s.PrepareWPPassword(context.Background(), mustDomain(t, "example.com")); nameCode(err) != CodeSiteNotWordPress {
		t.Errorf("wp-password trên site PHP trống: %v", err)
	}
}

// Dòng backup nhớ loại site và tên phụ lúc tạo bản (để tạo lại site cho đúng).
func TestBackupOriginPlain(t *testing.T) {
	se := newPlainEnv(t)
	in := se.input(t, "example.com")
	in.App = site.AppPHP
	a, err := se.s.PrepareAdd(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := a.Execute(context.Background(), nil); err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("site add: %v %s", err, failedChecks(res.Report.Checks))
	}
	ctx := context.Background()
	row, err := se.st.AddBackup(ctx, store.Backup{Domain: "example.com", Kind: site.BackupFull, Reason: site.ReasonManual, Path: "/x/1.tar.gz", SHA256: "x"})
	if err != nil {
		t.Fatal(err)
	}
	o, ok, err := se.st.BackupOrigin(ctx, row.ID)
	if err != nil || !ok {
		t.Fatalf("BackupOrigin: %v %v", ok, err)
	}
	if o.App != "php" || len(o.Aliases) != 1 || o.Aliases[0].Name != "www.example.com" || o.Aliases[0].Mode != "redirect" || len(o.Rules) != 0 {
		t.Errorf("origin = %+v", o)
	}
	// Bản của một domain không có site: để rỗng.
	row2, err := se.st.AddBackup(ctx, store.Backup{Domain: "gone.example.org", Kind: site.BackupFull, Reason: site.ReasonManual, Path: "/x/2.tar.gz", SHA256: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if o, _, _ := se.st.BackupOrigin(ctx, row2.ID); o.App != "" || len(o.Aliases) != 0 || len(o.Rules) != 0 {
		t.Errorf("origin của domain không có site = %+v", o)
	}
	if _, ok, _ := se.st.BackupOrigin(ctx, "khong-co"); ok {
		t.Error("id lạ mà ok")
	}
}

// Site PHP trống mới trên Apache nhận .htaccess của site PHP trống, không phải
// bản của WordPress; chạy lại thì không ghi gì vào public.
func TestSiteAddPlainApacheHtaccess(t *testing.T) {
	se := newSiteEnv(t, Apache)
	se.m.Handle([]string{"dd"}, func(c system.Cmd) system.Result {
		se.m.SetFile(strings.TrimPrefix(c.Args[0], "of="), c.Stdin, 0o640, system.Owner{User: c.AsUser, Group: c.AsUser})
		return system.Result{}
	})
	in := se.input(t, "shop.example.com")
	in.App = site.AppPHP
	a, err := se.s.PrepareAdd(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var ht *steps.File
	for _, s := range a.Plan.Steps {
		if f, ok := s.(*steps.File); ok && strings.HasSuffix(f.Path, "/.htaccess") {
			ht = f
		}
	}
	if ht == nil {
		t.Fatal("site PHP trống mới trên Apache không có bước .htaccess")
	}
	if body := string(ht.Content); strings.Contains(body, "WordPress") || !strings.Contains(body, "RewriteRule . /index.php [L]") {
		t.Errorf(".htaccess của site PHP trống:\n%s", body)
	}
}

// sites.yaml: export ghi app: php cho site PHP trống; apply tạo đúng loại đó và
// trả mật khẩu DB của site PHP trống; site đã có thì bỏ qua như mọi site.
func TestSitesFilePlain(t *testing.T) {
	se := newPlainEnv(t)
	ctx := context.Background()
	in := se.input(t, "example.com")
	in.App = site.AppPHP
	a, err := se.s.PrepareAdd(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := a.Execute(ctx, nil); err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("site add: %v", err)
	}
	f, err := se.s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sites) != 1 || f.Sites[0].App != "php" {
		t.Fatalf("export = %+v", f.Sites)
	}
	f.Sites = append(f.Sites, SitesEntry{Domain: "new.example.org", App: "php"}, SitesEntry{Domain: "wp.example.org", AdminEmail: "a@example.com"})
	list, err := f.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	ap, err := se.s.PrepareApply(ctx, ApplyInput{Sites: list})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ApplyAction{}
	for _, it := range ap.Items {
		got[it.Domain] = it.Action
	}
	if got["example.com"] != ApplySkip || got["new.example.org"] != ApplyCreate || got["wp.example.org"] != ApplyCreate {
		t.Fatalf("việc của từng dòng = %v", got)
	}
	res, err := ap.Execute(ctx, nil)
	if err != nil || res.Report == nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("apply: %v %+v", err, res.Report)
	}
	byDom := map[string]ApplySiteResult{}
	for _, c := range res.Created {
		byDom[c.Domain] = c
	}
	if c := byDom["new.example.org"]; c.WPInstalled || c.DBPassword.Reveal() == "" || c.DBName == "" {
		t.Errorf("site PHP trống trong lô: %+v", c)
	}
	if c := byDom["wp.example.org"]; !c.WPInstalled || c.DBPassword.Reveal() != "" {
		t.Errorf("site WordPress trong lô: wp=%v, có mật khẩu DB=%v", c.WPInstalled, c.DBPassword.Reveal() != "")
	}
	if st, ok, _ := se.st.Site(ctx, mustDomain(t, "new.example.org")); !ok || !st.Plain() {
		t.Errorf("dòng sites của site PHP trống trong lô: %v %q", ok, st.App)
	}
	if _, err := (SitesFile{Sites: []SitesEntry{{Domain: "x.example.org", App: "drupal"}}}).Resolve(); err == nil {
		t.Error("app lạ trong sites.yaml không bị từ chối")
	}
}

// Xin SSL cho site PHP trống: không có bước đổi địa chỉ site của WordPress.
func TestSSLIssuePlain(t *testing.T) {
	const dom = "shop.example.com"
	se := newSSLEnv(t, Nginx)
	se.setEmail(t)
	se.m.Handle([]string{"dd"}, func(c system.Cmd) system.Result {
		se.m.SetFile(strings.TrimPrefix(c.Args[0], "of="), c.Stdin, 0o640, system.Owner{User: c.AsUser, Group: c.AsUser})
		return system.Result{}
	})
	in := se.input(t, dom)
	in.App = site.AppPHP
	a, err := se.s.PrepareAdd(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := a.Execute(ctx, nil); err != nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("site add: %v %s", err, failedChecks(res.Report.Checks))
	}
	d := mustDomain(t, dom)
	iss := se.ssl.issue(t, d)
	for _, s := range iss.Plan.Steps {
		if _, ok := s.(*steps.WPSiteURL); ok {
			t.Error("Plan SSL của site PHP trống có bước đổi địa chỉ WordPress")
		}
	}
	iss.Plan.Verify = nil
	rep, err := iss.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	if st, _, _ := se.sites.Site(ctx, d); !st.SSL || !st.Plain() {
		t.Errorf("sau SSL: ssl=%v app=%q", st.SSL, st.App)
	}
	if len(se.wpURL) != 0 {
		t.Errorf("đã gọi wp option update: %v", se.wpURL)
	}
}
