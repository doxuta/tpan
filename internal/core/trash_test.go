package core

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// trashNow là giờ cố định cho Sites.Now, để TrashedAt và PurgeAt so được.
var trashNow = time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)

func (se *siteEnv) fixedNow(t time.Time) { se.s.Now = func() time.Time { return t } }

// trash đưa site vào thùng rác và đòi job thành công.
func (se *siteEnv) trash(t *testing.T, dom string) engine.Report {
	t.Helper()
	r, err := se.s.PrepareRemove(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatalf("PrepareRemove %s: %v", dom, err)
	}
	rep, err := r.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("đưa %s vào thùng rác: %v %s (%s)%s", dom, err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	return rep
}

func (se *siteEnv) siteOf(t *testing.T, dom string) site.Site {
	t.Helper()
	st, ok, err := se.sites.Site(ctx, mustDomain(t, dom))
	if err != nil || !ok {
		t.Fatalf("Store không có %s: %v", dom, err)
	}
	return st
}

func vhostPaths(web WebServer, l site.Layout) (conf, link string) {
	if web == Apache {
		return l.ApacheConf, l.ApacheLink
	}
	return l.NginxConf, l.NginxLink
}

func (se *siteEnv) exists(p string) bool {
	_, err := se.m.Stat(p)
	return !errors.Is(err, fs.ErrNotExist)
}

func TestTrashDays(t *testing.T) {
	se := newSiteEnv(t, "")
	if n, err := se.s.TrashDays(ctx); err != nil || n != 7 {
		t.Errorf("mặc định = %d, %v; muốn 7", n, err)
	}
	for _, n := range []int{0, 1, MaxTrashDays} {
		if err := se.s.SetTrashDays(ctx, n); err != nil {
			t.Fatalf("SetTrashDays(%d): %v", n, err)
		}
		if got, err := se.s.TrashDays(ctx); err != nil || got != n {
			t.Errorf("đặt %d, đọc lại %d, %v", n, got, err)
		}
	}
	for _, n := range []int{-1, MaxTrashDays + 1, 7000} {
		if err := se.s.SetTrashDays(ctx, n); refusedCode(t, err) != "trash_days_invalid" {
			t.Errorf("SetTrashDays(%d): muốn trash_days_invalid", n)
		}
	}
	// Giá trị hỏng trong DB: lỗi, không đoán.
	for _, v := range []string{"abc", "-3", "9999", "7 ngày"} {
		mustSet(t, se.st, SettingTrashDays, v)
		if _, err := se.s.TrashDays(ctx); err == nil {
			t.Errorf("trash.days = %q mà không lỗi", v)
		}
	}
}

// Đưa vào thùng rác rồi khôi phục, trên cả hai web server: thứ tự bước, trạng
// thái máy và bảng sites sau mỗi job, kiểm chứng đạt.
func TestSiteTrashAndRestore(t *testing.T) {
	const dom = "shop.example.com"
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			se := newSiteEnv(t, web)
			se.fixedNow(trashNow)
			se.add(t, "blog.example.com")
			se.add(t, dom)
			d := mustDomain(t, dom)
			l := site.LayoutOf(d)
			conf, link := vhostPaths(web, l)
			pool := l.PoolConf("8.3")
			trashDir := site.TrashDir(l)

			r, err := se.s.PrepareRemove(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"absent:" + link,
				"absent:" + pool,
				"db.lock:" + dom,
				"user.lock:" + dom,
				"dir.move:" + l.Dir,
			}
			if got := stepIDs(r.Plan); !reflect.DeepEqual(got, want) {
				t.Errorf("thứ tự bước:\n got %v\nwant %v", got, want)
			}
			if r.Plan.Kind != "site.rm" || r.Plan.Target != dom || len(r.Plan.Verify) != 1 ||
				r.Plan.Title.Vi != "Đưa site "+dom+" vào thùng rác" {
				t.Errorf("Plan = %s/%s %q, %d phép kiểm", r.Plan.Kind, r.Plan.Target, r.Plan.Title.Vi, len(r.Plan.Verify))
			}
			se.m.ResetRan()
			pv, err := r.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range se.m.Ran() {
				if !readOnly(c) && c.Name != "getent" && c.Name != "chage" {
					t.Errorf("xem trước chạy lệnh thay đổi máy: %s", c.String())
				}
			}
			danger := 0
			for _, sp := range pv.Steps {
				if sp.Info.Danger {
					danger++
				}
			}
			if danger == 0 {
				t.Error("đưa vào thùng rác mà không bước nào Danger: CLI sẽ không hỏi lại")
			}

			rep := se.trash(t, dom)
			if len(rep.Checks) != 1 || rep.Checks[0].ID != "http.gone:"+dom || rep.Checks[0].Status != checks.Pass {
				t.Errorf("kiểm chứng = %+v", rep.Checks)
			}
			st := se.siteOf(t, dom)
			if st.Status != site.Trashed || !st.TrashedAt.Equal(trashNow) || !st.PurgeAt.Equal(trashNow.AddDate(0, 0, 7)) {
				t.Errorf("Store sau thùng rác = %+v", st)
			}
			if se.siteOf(t, "blog.example.com").Status != site.Active {
				t.Error("site khác bị đổi trạng thái")
			}
			if se.exists(l.Dir) || !se.exists(trashDir) || !se.exists(trashDir+"/public/wp-config.php") {
				t.Error("thư mục site chưa được chuyển vào thùng rác")
			}
			if se.exists(link) || se.exists(pool) || !se.exists(conf) {
				t.Errorf("link %v, pool %v, file vhost %v; muốn chỉ còn file vhost", se.exists(link), se.exists(pool), se.exists(conf))
			}
			if !se.m.expired[l.User] || !se.m.dbLocked[l.DB] || !se.m.dbs[l.DB] || !se.m.dbUsers[l.DB] {
				t.Error("user Linux chưa khoá, hoặc user DB chưa khoá, hoặc DB mất")
			}
			if _, found := se.m.users[l.User]; !found || !se.exists(l.LogDir) {
				t.Error("thùng rác xoá mất user hoặc log")
			}

			// Đã ở thùng rác: rm lần nữa và add đều bị từ chối.
			if _, err := se.s.PrepareRemove(ctx, d); refusedCode(t, err) != "site_trashed" {
				t.Error("rm site đã ở thùng rác: muốn site_trashed")
			}
			if _, err := se.s.PrepareAdd(ctx, se.input(t, dom)); refusedCode(t, err) != "site_trashed" {
				t.Error("add site đã ở thùng rác: muốn site_trashed")
			}

			// Khôi phục.
			rs, err := se.s.PrepareRestore(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			want = []string{
				"dir.move:" + trashDir,
				"user.unlock:" + dom,
				"db.unlock:" + dom,
				"file:" + pool,
				"file:" + conf,
				"symlink:" + link,
			}
			if got := stepIDs(rs.Plan); !reflect.DeepEqual(got, want) {
				t.Errorf("thứ tự bước khôi phục:\n got %v\nwant %v", got, want)
			}
			if rs.Plan.Kind != "site.restore" || len(rs.Plan.Verify) == 0 {
				t.Errorf("Plan khôi phục = %s, %d phép kiểm", rs.Plan.Kind, len(rs.Plan.Verify))
			}
			pv, err = rs.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, sp := range pv.Steps {
				if sp.Info.ID == "file:"+conf && !sp.Done {
					t.Error("file vhost vẫn còn mà khôi phục định ghi lại")
				}
			}
			rep, err = rs.Execute(ctx, nil)
			if err != nil || rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("khôi phục: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
			}
			ids := map[string]bool{}
			for _, c := range rep.Checks {
				ids[c.ID] = true
			}
			if !ids["http.site:"+dom] || !ids["isolation:"+dom+":blog.example.com"] {
				t.Errorf("kiểm chứng khôi phục thiếu phép kiểm của 6.2: %v", ids)
			}
			st = se.siteOf(t, dom)
			if st.Status != site.Active || !st.TrashedAt.IsZero() || !st.PurgeAt.IsZero() {
				t.Errorf("Store sau khôi phục = %+v", st)
			}
			if !se.exists(l.Dir) || se.exists(trashDir) || !se.exists(link) || !se.exists(pool) {
				t.Error("khôi phục chưa trả thư mục, link hay pool")
			}
			if se.m.expired[l.User] || se.m.dbLocked[l.DB] {
				t.Error("khôi phục chưa mở khoá user")
			}
			if _, err := se.s.PrepareRestore(ctx, d); refusedCode(t, err) != "site_not_trashed" {
				t.Error("khôi phục site active: muốn site_not_trashed")
			}
		})
	}
}

// Kiểm chứng lỗi (tên miền vẫn được phục vụ) thì engine hoàn tác mọi bước:
// máy về đúng như trước, dòng giữ active.
func TestSiteTrashUndo(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, dom)
	l := site.LayoutOf(mustDomain(t, dom))
	before := se.m.Snapshot()

	se.m.serveAll = true
	r, err := se.s.PrepareRemove(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := r.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.Status != engine.JobRolledBack {
		t.Fatalf("job = %s; muốn rolled_back (%s)", rep.Job.Status, rep.Job.Error)
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Status != checks.Fail || len(rep.Checks[0].ManualFix) == 0 {
		t.Errorf("kiểm chứng = %+v", rep.Checks)
	}
	after := se.m.Snapshot()
	if !reflect.DeepEqual(keys(before), keys(after)) {
		t.Errorf("hệ thống file khác sau hoàn tác:\n trước %v\n sau   %v", keys(before), keys(after))
	}
	if se.m.expired[l.User] || se.m.dbLocked[l.DB] {
		t.Error("hoàn tác chưa mở khoá user")
	}
	if se.siteOf(t, dom).Status != site.Active {
		t.Error("job hoàn tác mà Store đổi trạng thái")
	}
}

func keys(m map[string]system.FakeEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Job hỏng giữa chừng (khoá user Linux lỗi): dòng giữ active.
func TestSiteTrashFailKeepsRow(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, dom)
	se.m.Handle([]string{"usermod", "--expiredate"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "usermod: cannot lock /etc/passwd; try again later.\n"}
	})
	r, err := se.s.PrepareRemove(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := r.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.Status == engine.JobSucceeded || !strings.Contains(rep.Job.Error, "usermod") {
		t.Fatalf("usermod lỗi mà job = %s (%s)", rep.Job.Status, rep.Job.Error)
	}
	if st := se.siteOf(t, dom); st.Status != site.Active || !st.PurgeAt.IsZero() {
		t.Errorf("job hỏng mà Store = %+v", st)
	}
	if !se.exists(site.LayoutOf(mustDomain(t, dom)).Dir) {
		t.Error("job hỏng mà thư mục site không còn ở chỗ cũ")
	}
}

func TestSiteTrashRefusals(t *testing.T) {
	const dom = "shop.example.com"
	d := mustDomain(t, dom)
	se := newSiteEnv(t, Nginx)
	for name, err := range map[string]error{
		"rm":      func() error { _, err := se.s.PrepareRemove(ctx, d); return err }(),
		"restore": func() error { _, err := se.s.PrepareRestore(ctx, d); return err }(),
		"purge":   func() error { _, err := se.s.PreparePurge(ctx, d); return err }(),
	} {
		if refusedCode(t, err) != "site_not_found" {
			t.Errorf("%s site không có: muốn site_not_found", name)
		}
	}
	if len(se.m.Ran()) != 0 {
		t.Errorf("từ chối mà chạy lệnh: %v", se.m.Ran())
	}

	se.add(t, dom)
	if _, err := se.s.PreparePurge(ctx, d); refusedCode(t, err) != "site_not_trashed" {
		t.Error("xoá hẳn site active: muốn site_not_trashed")
	}
	if _, err := se.s.PrepareRestore(ctx, d); refusedCode(t, err) != "site_not_trashed" {
		t.Error("khôi phục site active: muốn site_not_trashed")
	}

	// Domain đã được tạo lại ngoài tpan sau khi vào thùng rác.
	se.trash(t, dom)
	l := site.LayoutOf(d)
	se.m.SetDir(l.Dir, 0o755, system.Root)
	if _, err := se.s.PrepareRestore(ctx, d); refusedCode(t, err) != "site_exists_unmanaged" {
		t.Error("khôi phục khi /var/www/<domain> đã có: muốn site_exists_unmanaged")
	}
}

// Xoá hẳn: dùng lại Plan xoá hẳn của M2, thư mục site lấy từ thùng rác.
func TestSitePurge(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, "blog.example.com")
	se.add(t, dom)
	se.trash(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)

	p, err := se.s.PreparePurge(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"absent:" + l.NginxLink,
		"absent:" + l.NginxConf,
		"absent:" + l.PoolConf("8.3"),
		"mariadb.site-db-absent:" + dom,
		"remove-tree:" + site.TrashDir(l),
		"remove-tree:" + l.LogDir,
		"site.user.absent:" + l.User,
	}
	if got := stepIDs(p.Plan); !reflect.DeepEqual(got, want) {
		t.Errorf("thứ tự bước:\n got %v\nwant %v", got, want)
	}
	if p.Plan.Kind != "site.purge" || len(p.Plan.Verify) != 0 {
		t.Errorf("Plan = %s, %d phép kiểm", p.Plan.Kind, len(p.Plan.Verify))
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá hẳn: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	if _, ok, _ := se.sites.Site(ctx, d); ok {
		t.Error("xoá hẳn xong mà Store còn site")
	}
	if _, ok, _ := se.sites.Site(ctx, mustDomain(t, "blog.example.com")); !ok {
		t.Error("xoá nhầm site khác khỏi Store")
	}
	if _, found := se.m.users[l.User]; found || se.m.dbs[l.DB] || se.m.dbUsers[l.DB] {
		t.Error("còn user hoặc DB của site đã xoá hẳn")
	}
	for _, p := range []string{site.TrashDir(l), l.LogDir, l.NginxConf, l.Dir} {
		if se.exists(p) {
			t.Errorf("còn %s", p)
		}
	}

	// Site có SSL: certbot delete là bước cuối, để hoàn tác các bước trước
	// không trả lại một vhost 443 trỏ vào chứng chỉ đã mất.
	dSSL := mustDomain(t, "secure.example.com")
	_ = se.sites.SaveSite(ctx, site.Site{Domain: dSSL, Status: site.Trashed, SSL: true, PHPVersion: "8.3"})
	ps, err := se.s.PreparePurge(ctx, dSSL)
	if err != nil {
		t.Fatal(err)
	}
	ids := stepIDs(ps.Plan)
	if i := slices.Index(ids, "ssl.delete:secure.example.com"); i < 0 || i != len(ids)-1 {
		t.Errorf("vị trí certbot delete sai: %v", ids)
	}
}

// PurgeExpired chỉ xoá site đã quá hạn, theo Sites.Now cố định.
func TestPurgeExpired(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	for _, dom := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		se.add(t, dom)
	}
	se.fixedNow(trashNow)
	mustSet(t, se.st, SettingTrashDays, "3")
	se.trash(t, "b.example.com") // hạn trashNow+3
	mustSet(t, se.st, SettingTrashDays, "1")
	se.trash(t, "a.example.com") // hạn trashNow+1

	trash, err := se.s.Trash(ctx)
	if err != nil || len(trash) != 2 || trash[0].Domain.String() != "a.example.com" {
		t.Fatalf("Trash = %+v, %v; muốn a trước (sắp hết hạn trước)", trash, err)
	}
	now := trashNow.AddDate(0, 0, 2)
	exp, err := se.s.Expired(ctx, now)
	if err != nil || len(exp) != 1 || exp[0].Domain.String() != "a.example.com" {
		t.Fatalf("Expired = %+v, %v", exp, err)
	}
	// Đúng hạn (PurgeAt == now) cũng là hết hạn.
	if exp, _ := se.s.Expired(ctx, trashNow.AddDate(0, 0, 1)); len(exp) != 1 {
		t.Errorf("PurgeAt == now: Expired = %+v", exp)
	}
	if exp, _ := se.s.Expired(ctx, trashNow); len(exp) != 0 {
		t.Errorf("chưa tới hạn: Expired = %+v", exp)
	}

	res, err := se.s.PurgeExpired(ctx, now, nil)
	if err != nil || len(res) != 1 || res[0].Domain.String() != "a.example.com" || res[0].Error != "" ||
		res[0].Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("PurgeExpired = %+v, %v", res, err)
	}
	if _, ok, _ := se.sites.Site(ctx, mustDomain(t, "a.example.com")); ok {
		t.Error("site quá hạn còn trong Store")
	}
	if se.siteOf(t, "b.example.com").Status != site.Trashed || se.siteOf(t, "c.example.com").Status != site.Active {
		t.Error("PurgeExpired đụng site chưa hết hạn hoặc site active")
	}

	// Không có gì quá hạn: không job nào.
	if res, err := se.s.PurgeExpired(ctx, now, nil); err != nil || len(res) != 0 {
		t.Errorf("lần hai = %+v, %v", res, err)
	}
	// Site không dựng được Plan: ghi lỗi rồi đi tiếp.
	_ = se.sites.SaveSite(ctx, site.Site{Domain: mustDomain(t, "z.example.com"), Status: site.Trashed, PHPVersion: "8;3", PurgeAt: trashNow})
	res, err = se.s.PurgeExpired(ctx, now, nil)
	if err != nil || len(res) != 1 || res[0].Error == "" {
		t.Errorf("site hỏng: %+v, %v", res, err)
	}
	// 0 ngày: hết hạn ngay.
	mustSet(t, se.st, SettingTrashDays, "0")
	se.trash(t, "c.example.com")
	_ = se.sites.DeleteSite(ctx, mustDomain(t, "z.example.com"))
	if exp, _ := se.s.Expired(ctx, trashNow); len(exp) != 1 || exp[0].Domain.String() != "c.example.com" {
		t.Errorf("0 ngày: Expired = %+v", exp)
	}
}

// Mục 3 của việc M6: site trong thùng rác không sinh phép kiểm lệch Fail. Pool
// đã gỡ phải ra khỏi managed_files; file vhost còn nguyên nên lệch vẫn đạt.
func TestTrashNoFalseDrift(t *testing.T) {
	const dom = "shop.example.com"
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			se := newSiteEnv(t, web)
			se.add(t, dom)
			se.trash(t, dom)
			l := site.LayoutOf(mustDomain(t, dom))
			files, err := se.st.ManagedFiles(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if f.Path == l.PoolConf("8.3") {
					t.Errorf("pool đã gỡ mà còn trong managed_files")
				}
				// .htaccess đi theo thư mục site; health bỏ nó khỏi phép kiểm lệch
				// (internal/health/collect.go).
				if path.Base(f.Path) == ".htaccess" {
					continue
				}
				if r := checks.Drift(f.Path, f.SHA256, checks.DriftStage(f.Path)).Run(ctx, se.m); r.Status != checks.Pass {
					t.Errorf("lệch giả cho %s: %s", f.Path, r.Summary.Vi)
				}
			}
		})
	}
}

func TestSiteGoneCheck(t *testing.T) {
	l := site.LayoutOf(mustDomain(t, "shop.example.com"))
	for _, tc := range []struct {
		name string
		web  WebServer
		res  system.Result
		want checks.Status
	}{
		{"nginx 444", Nginx, system.Result{ExitCode: 52, Stdout: "000", Stderr: "curl: (52) Empty reply from server\n"}, checks.Pass},
		{"apache 403", Apache, system.Result{Stdout: "403"}, checks.Pass},
		{"404 nginx", Nginx, system.Result{Stdout: "404"}, checks.Pass},
		{"404 apache", Apache, system.Result{Stdout: "404"}, checks.Pass},
		{"vẫn 200", Nginx, system.Result{Stdout: "200"}, checks.Fail},
		{"403 ở nginx", Nginx, system.Result{Stdout: "403"}, checks.Fail},
		{"52 ở apache", Apache, system.Result{ExitCode: 52, Stdout: "000"}, checks.Fail},
		{"không kết nối được", Nginx, system.Result{ExitCode: 7, Stdout: "000"}, checks.Fail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := system.NewFake()
			f.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
				if argAfter(c.Args, "--resolve") != "shop.example.com:80:127.0.0.1" {
					return system.Result{ExitCode: 2, Stderr: "argv lạ"}
				}
				return tc.res
			})
			r := siteGone(tc.web, l).Run(ctx, f)
			if r.Status != tc.want || r.ID != "http.gone:shop.example.com" || r.Stage != checks.StageEndToEnd || len(r.Evidence) != 1 {
				t.Errorf("= %s %s %s (%d bằng chứng); muốn %s", r.Status, r.ID, r.Stage, len(r.Evidence), tc.want)
			}
			if r.Status == checks.Fail && (len(r.ManualFix) == 0 || !strings.HasPrefix(r.ManualFix[len(r.ManualFix)-1], "curl ")) {
				t.Errorf("Fail mà thiếu lệnh sửa tay hoặc dòng cuối không phải lệnh kiểm lại: %v", r.ManualFix)
			}
		})
	}
	// Lệnh không chạy được (ở đây: ctx đã huỷ) là chưa kiểm được, không phải lỗi.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if r := siteGone(Nginx, l).Run(cctx, system.NewFake()); r.Status != checks.Unknown {
		t.Errorf("curl không chạy được: %s; muốn unknown", r.Status)
	}
}
