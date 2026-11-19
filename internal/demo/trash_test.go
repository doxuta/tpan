package demo

import (
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// Site cũ nằm trong thùng rác như sau một lần "Đưa vào thùng rác" thật: dòng
// trashed còn vài ngày tới hạn, thư mục ở TrashDir, user DB khoá, user Linux
// hết hạn, domain rơi vào site mặc định.
func TestTrashSeed(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)
	trash, err := s.Trash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(trash) != 1 || trash[0].Domain.String() != siteOld || trash[0].Status != site.Trashed {
		t.Fatalf("thùng rác = %+v", trash)
	}
	left := time.Until(trash[0].PurgeAt)
	if left < 3*24*time.Hour || left > 5*24*time.Hour || time.Since(trash[0].TrashedAt) < 2*24*time.Hour {
		t.Errorf("TrashedAt %v, PurgeAt %v: muốn vào thùng rác 3 ngày trước, còn 4 ngày", trash[0].TrashedAt, trash[0].PurgeAt)
	}
	l := trash[0].Layout()
	if _, err := sys.Stat(site.TrashDir(l) + "/public/index.php"); err != nil {
		t.Errorf("mã nguồn không nằm trong thùng rác: %v", err)
	}
	if _, err := sys.Stat(l.Dir); err == nil {
		t.Errorf("%s vẫn còn", l.Dir)
	}
	res, err := sys.Run(ctx, system.Cmd{Name: "chage", Args: []string{"-l", l.User}})
	if err != nil || !strings.Contains(res.Stdout, "Account expires\t\t\t\t\t\t: Jan 02, 1970") {
		t.Errorf("user Linux chưa khoá: %v %q", err, res.Stdout)
	}
	res, err = sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(
		"SELECT COUNT(*), COALESCE(MAX(JSON_VALUE(Priv, '$.account_locked') IN ('1', 'true')), 0) FROM mysql.global_priv WHERE User='" + l.DB + "' AND Host='localhost';\n")})
	if err != nil || strings.TrimSpace(res.Stdout) != "1\t1" {
		t.Errorf("user DB chưa khoá: %v %q", err, res.Stdout)
	}
	// Site đang chạy không bị khoá.
	b, _ := layoutOf(siteBlog)
	res, _ = sys.Run(ctx, system.Cmd{Name: "chage", Args: []string{"-l", b.User}})
	if !strings.Contains(res.Stdout, ": never") {
		t.Errorf("user của %s bị khoá: %q", siteBlog, res.Stdout)
	}
}

// Kịch bản màn Thùng rác: khôi phục, đưa vào lại, xoá hẳn.
func TestTrashRestorePurgeOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)
	l, err := layoutOf(siteOld)
	if err != nil {
		t.Fatal(err)
	}
	d := l.Domain

	r, err := s.PrepareRestore(ctx, d)
	if err != nil {
		t.Fatalf("PrepareRestore: %v", err)
	}
	rep, err := r.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("khôi phục: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	h, err := s.Health(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if bad := notPass(h.Results); len(bad) > 0 {
		t.Errorf("site vừa khôi phục phải đạt mọi phép kiểm, có: %v", bad)
	}

	rm, err := s.PrepareRemove(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err = rm.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("đưa vào thùng rác lần hai: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}

	p, err := s.PreparePurge(ctx, d)
	if err != nil {
		t.Fatalf("PreparePurge: %v", err)
	}
	if rep, err = p.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá hẳn: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	if _, ok, _ := st.Site(ctx, d); ok {
		t.Error("xoá hẳn mà dòng của site còn")
	}
	if _, err := sys.Stat(site.TrashDir(l)); err == nil {
		t.Error("xoá hẳn mà thư mục trong thùng rác còn")
	}
	if res, _ := sys.Run(ctx, system.Cmd{Name: "getent", Args: []string{"passwd", l.User}}); res.ExitCode != 2 {
		t.Errorf("xoá hẳn mà user Linux còn: %+v", res)
	}

	// Tạo lại đúng domain đó: WordPress cài mới, có mật khẩu mới.
	in, err := input(siteOld, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.PrepareAdd(ctx, in)
	if err != nil {
		t.Fatalf("PrepareAdd sau khi xoá hẳn: %v", err)
	}
	res, err := a.Execute(ctx, nil)
	if err != nil || res.Report.Job.Status != engine.JobSucceeded || !res.WPInstalled {
		t.Fatalf("tạo lại %s: %v %s %s WPInstalled=%v", siteOld, err, res.Report.Job.Status, res.Report.Job.Error, res.WPInstalled)
	}
}

// Hết hạn: bộ hẹn giờ của serve gọi PurgeExpired; trên máy demo nó xoá được
// site cũ khi đồng hồ đã qua PurgeAt, và chưa tới hạn thì không đụng gì.
func TestPurgeExpiredOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)
	res, err := s.PurgeExpired(ctx, time.Now(), nil)
	if err != nil || len(res) != 0 {
		t.Fatalf("chưa tới hạn: %+v, %v", res, err)
	}
	res, err = s.PurgeExpired(ctx, time.Now().Add(5*24*time.Hour), nil)
	if err != nil || len(res) != 1 || res[0].Error != "" || res[0].Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("đã quá hạn: %+v, %v", res, err)
	}
	if trash, _ := s.Trash(ctx); len(trash) != 0 {
		t.Fatalf("thùng rác còn %+v", trash)
	}
}

// Kịch bản "dán danh sách" trên màn Tạo site: hai site mới và một site đã có,
// một job, reload mỗi dịch vụ đúng một lần.
func TestApplyOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)
	list, err := core.SitesFile{
		Defaults: core.SitesDefaults{AdminEmail: demoEmail},
		Sites:    []core.SitesEntry{{Domain: siteBlog}, {Domain: "a.example.com"}, {Domain: "b.example.com", Title: "B"}},
	}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	ap, err := s.PrepareApply(ctx, core.ApplyInput{Sites: list})
	if err != nil {
		t.Fatalf("PrepareApply: %v", err)
	}
	f := sys
	f.ResetRan()
	res, err := ap.Execute(ctx, nil)
	if err != nil || res.Report == nil || res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("apply: %v %+v", err, res.Report)
	}
	if len(res.Created) != 2 {
		t.Fatalf("Created = %+v", res.Created)
	}
	for _, c := range res.Created {
		if c.Status != engine.UnitSucceeded || !c.WPInstalled || len(c.AdminPassword.Reveal()) == 0 {
			t.Errorf("site %s: %+v", c.Domain, c)
		}
	}
	reloads := map[string]int{}
	for _, c := range f.Ran() {
		if c.Name == "systemctl" && len(c.Args) == 2 && c.Args[0] == "reload" {
			reloads[c.Args[1]]++
		}
	}
	if reloads["nginx"] != 1 || reloads["php"+phpVer+"-fpm"] != 1 {
		t.Errorf("reload = %v; muốn mỗi dịch vụ đúng một lần", reloads)
	}
	if n := countActive(ctx, t, s); n != 5 {
		t.Errorf("có %d site active; muốn 5", n)
	}
}
