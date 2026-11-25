package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/validate"
)

func TestSitesOfVaSiteOwner(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveSiteOf(t, s, "a.example.com", "an")
	saveSiteOf(t, s, "b.example.com", "binh")
	saveSiteOf(t, s, "c.example.com", "") // rỗng là admin

	of := func(owner string) []string {
		list, err := s.SitesOf(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		var d []string
		for _, st := range list {
			d = append(d, st.Domain.String())
		}
		return d
	}
	if got := of("an"); len(got) != 1 || got[0] != "a.example.com" {
		t.Errorf("SitesOf(an) = %v", got)
	}
	if got := of("admin"); len(got) != 1 || got[0] != "c.example.com" {
		t.Errorf("SitesOf(admin) = %v", got)
	}
	if got := of(""); len(got) != 3 {
		t.Errorf("SitesOf('') = %v, muốn cả 3", got)
	}
	if list, err := s.SitesOf(ctx, "khong-co"); err != nil || list == nil || len(list) != 0 {
		t.Errorf("SitesOf(lạ) = %#v, %v; muốn slice rỗng không nil", list, err)
	}
	if all, _ := s.Sites(ctx); len(all) != 3 {
		t.Errorf("Sites = %d", len(all))
	}

	if o, ok, err := s.SiteOwner(ctx, "b.example.com"); err != nil || !ok || o != "binh" {
		t.Errorf("SiteOwner = %q, %v, %v", o, ok, err)
	}
	if _, ok, err := s.SiteOwner(ctx, "none.example.com"); ok || err != nil {
		t.Errorf("SiteOwner lạ = ok %v, err %v", ok, err)
	}
}

func TestJobsOfLocTheoChuVaNguoiChay(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveSiteOf(t, s, "a.example.com", "an")
	n := 0
	mk := func(id, target, actor string) {
		n++
		j := sampleJob(id, t0.Add(time.Duration(n)*time.Second))
		// P7: chủ chỉ suy từ Target với loại job của site (site.*).
		j.Kind, j.Target, j.Actor = "site.add", target, actor
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	mk("j1", "a.example.com", "an")    // an chạy trên site của an
	mk("j2", "a.example.com", "admin") // admin chạy trên site của an: an vẫn thấy
	mk("j3", "nginx", "admin")         // việc của máy do admin chạy: owner = actor = admin
	mk("j4", "nginx", "")              // dòng lệnh: admin
	mk("j5", "b.example.com", "binh")  // đối tượng chưa có dòng: owner = actor
	ids := func(owner, actor string, limit int) []string {
		jobs, err := s.JobsOf(ctx, owner, actor, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, j := range jobs {
			out = append(out, j.ID)
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("được %v, muốn %v", got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("được %v, muốn %v", got, want)
				return
			}
		}
	}
	eq(ids("an", "", 0), "j2", "j1")
	eq(ids("admin", "", 0), "j4", "j3")
	eq(ids("binh", "", 0), "j5")
	eq(ids("", "admin", 0), "j3", "j2")
	eq(ids("an", "admin", 0), "j2")
	eq(ids("", "", 2), "j5", "j4")
	eq(ids("", "", 0), "j5", "j4", "j3", "j2", "j1")

	for id, want := range map[string]string{"j1": "an", "j2": "an", "j3": "admin", "j5": "binh"} {
		if o, ok, err := s.JobOwner(ctx, id); err != nil || !ok || o != want {
			t.Errorf("JobOwner(%s) = %q, %v, %v; muốn %q", id, o, ok, err, want)
		}
	}
	if _, ok, err := s.JobOwner(ctx, "khong-co"); ok || err != nil {
		t.Errorf("JobOwner lạ = ok %v, err %v", ok, err)
	}
	// Owner đặt sẵn thì được giữ nguyên, kể cả khi sites nói khác.
	j := sampleJob("j6", t0.Add(time.Hour))
	j.Target, j.Owner = "a.example.com", "cuong"
	_ = s.CreateJob(ctx, j)
	if o, _, _ := s.JobOwner(ctx, "j6"); o != "cuong" {
		t.Errorf("owner đặt sẵn bị bỏ: %q", o)
	}
	_ = engine.JobRunning
}

func failResult(id string) checks.Result {
	return checks.Result{ID: id, Stage: checks.StageDNS, Status: checks.Fail}
}

func TestIncidentVaPhepKiemTheoChu(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveSiteOf(t, s, "a.example.com", "an")
	saveSiteOf(t, s, "b.example.com", "binh")
	for target, id := range map[string]string{
		"a.example.com":    "dns.a:a.example.com",
		"b.example.com":    "dns.a:b.example.com",
		"":                 "service.active:nginx",
		TargetTPAN:         "tpan.db",
		"gone.example.com": "dns.a:gone.example.com", // site không còn dòng nào
	} {
		if _, err := s.RecordResults(ctx, target, []checks.Result{failResult(id)}, t0); err != nil {
			t.Fatal(err)
		}
	}

	incOf := func(owner string) []string {
		incs, err := s.IncidentsOf(ctx, owner, true, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, i := range incs {
			out = append(out, i.CheckID)
		}
		return out
	}
	if got := incOf("an"); len(got) != 1 || got[0] != "dns.a:a.example.com" {
		t.Errorf("IncidentsOf(an) = %v, chỉ được thấy sự cố của site mình", got)
	}
	if got := incOf(""); len(got) != 5 {
		t.Errorf("IncidentsOf('') = %v, muốn cả 5", got)
	}
	if got := incOf("khong-co"); len(got) != 0 {
		t.Errorf("IncidentsOf(lạ) = %v", got)
	}
	if all, _ := s.Incidents(ctx, true, 0); len(all) != 5 {
		t.Errorf("Incidents = %d", len(all))
	}
	// Đã đóng thì openOnly loại ra.
	if _, err := s.RecordResults(ctx, "a.example.com", []checks.Result{{ID: "dns.a:a.example.com", Stage: checks.StageDNS, Status: checks.Pass}}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := incOf("an"); len(got) != 0 {
		t.Errorf("sự cố đã đóng vẫn hiện trong openOnly: %v", got)
	}
	if incs, _ := s.IncidentsOf(ctx, "an", false, 0); len(incs) != 1 {
		t.Errorf("IncidentsOf(an, tất cả) = %d, muốn 1", len(incs))
	}

	// Chủ của sự cố.
	all, _ := s.Incidents(ctx, false, 0)
	want := map[string]string{"dns.a:a.example.com": "an", "dns.a:b.example.com": "binh",
		"service.active:nginx": "", "tpan.db": "", "dns.a:gone.example.com": ""}
	for _, i := range all {
		o, ok, err := s.IncidentOwner(ctx, i.ID)
		if err != nil || !ok || o != want[i.CheckID] {
			t.Errorf("IncidentOwner(%s) = %q, %v, %v; muốn %q", i.CheckID, o, ok, err, want[i.CheckID])
		}
	}
	if _, ok, err := s.IncidentOwner(ctx, 9999); ok || err != nil {
		t.Errorf("IncidentOwner lạ = ok %v, err %v", ok, err)
	}

	// Kết quả phép kiểm.
	rs, err := s.CheckResultsOf(ctx, "binh")
	if err != nil || len(rs) != 1 || rs[0].Target != "b.example.com" {
		t.Errorf("CheckResultsOf(binh) = %+v, %v", rs, err)
	}
	if rs, _ := s.CheckResultsOf(ctx, ""); len(rs) != 5 {
		t.Errorf("CheckResultsOf('') = %d", len(rs))
	}
	if rs, _ := s.CheckResults(ctx); len(rs) != 5 {
		t.Errorf("CheckResults = %d", len(rs))
	}
}

func TestBackupOwnerSuyTuSite(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveSiteOf(t, s, "a.example.com", "an")
	saveSiteOf(t, s, "b.example.com", "binh")

	mk := func(domain, path string) Backup {
		b, err := s.AddBackup(ctx, Backup{Domain: domain, Kind: "full", Reason: "manual", Path: path, Size: 1, Created: t0})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	ba := mk("a.example.com", "/b/a1")
	mk("a.example.com", "/b/a2")
	bb := mk("b.example.com", "/b/b1")
	bx := mk("da-xoa.example.com", "/b/x1") // không có site: admin

	for _, c := range []struct {
		id, want string
	}{{ba.ID, "an"}, {bb.ID, "binh"}, {bx.ID, "admin"}} {
		if o, ok, err := s.BackupOwner(ctx, c.id); err != nil || !ok || o != c.want {
			t.Errorf("BackupOwner(%s) = %q, %v, %v; muốn %q", c.id, o, ok, err, c.want)
		}
	}
	if _, ok, err := s.BackupOwner(ctx, "khong-co"); ok || err != nil {
		t.Errorf("BackupOwner lạ = ok %v, err %v", ok, err)
	}

	if got, _ := s.BackupsOf(ctx, "an", ""); len(got) != 2 {
		t.Errorf("BackupsOf(an) = %d, muốn 2", len(got))
	}
	if got, _ := s.BackupsOf(ctx, "an", "b.example.com"); len(got) != 0 {
		t.Errorf("BackupsOf(an, site của binh) = %d, muốn 0", len(got))
	}
	if got, _ := s.BackupsOf(ctx, "", "a.example.com"); len(got) != 2 {
		t.Errorf("BackupsOf('', a) = %d", len(got))
	}
	if got, _ := s.BackupsOf(ctx, "", ""); len(got) != 4 {
		t.Errorf("BackupsOf('', '') = %d", len(got))
	}
	if got, _ := s.Backups(ctx, ""); len(got) != 4 {
		t.Errorf("Backups = %d", len(got))
	}

	// Backup sống lâu hơn site: xoá hẳn site, backup vẫn của an.
	if err := s.DeleteSite(ctx, siteDomain(t, "a.example.com")); err != nil {
		t.Fatal(err)
	}
	if o, _, _ := s.BackupOwner(ctx, ba.ID); o != "an" {
		t.Errorf("owner của backup đổi sau khi xoá site: %q", o)
	}
}

func TestDBOwnerVaStandaloneDB(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	d := account.Database{
		Name: "an_blog", Owner: "an", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci",
		Note: "blog thử", Suspended: account.SuspendByUser, SuspendedAt: t0.Add(time.Minute),
		Created: t0,
	}
	if err := s.SaveStandaloneDB(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.StandaloneDB(ctx, "an_blog")
	if err != nil || !ok {
		t.Fatalf("StandaloneDB = ok %v, err %v", ok, err)
	}
	d.Status = account.DBActive // rỗng lúc lưu là active
	if got != d {
		t.Errorf("đọc ra %+v, muốn %+v", got, d)
	}
	if o, ok, err := s.DBOwner(ctx, "an_blog"); err != nil || !ok || o != "an" {
		t.Errorf("DBOwner = %q, %v, %v", o, ok, err)
	}
	if _, ok, err := s.DBOwner(ctx, "khong_co"); ok || err != nil {
		t.Errorf("DBOwner lạ = ok %v, err %v", ok, err)
	}
	if _, ok, _ := s.StandaloneDB(ctx, "khong_co"); ok {
		t.Error("StandaloneDB lạ phải ok=false")
	}

	// Ghi đè: vào thùng rác, mở khoá.
	d.Status, d.Suspended, d.SuspendedAt = account.DBTrashed, account.NotSuspended, time.Time{}
	d.TrashedAt, d.PurgeAt = t0.Add(time.Hour), t0.Add(48*time.Hour)
	if err := s.SaveStandaloneDB(ctx, d); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.StandaloneDB(ctx, "an_blog"); got != d {
		t.Errorf("sau ghi đè %+v, muốn %+v", got, d)
	}

	_ = s.SaveStandaloneDB(ctx, account.Database{Name: "binh_x", Owner: "binh", Charset: "latin1", Collation: "latin1_swedish_ci", Created: t0})
	if list, _ := s.StandaloneDBs(ctx, "an"); len(list) != 1 || list[0].Name != "an_blog" {
		t.Errorf("StandaloneDBs(an) = %+v", list)
	}
	if list, _ := s.StandaloneDBs(ctx, ""); len(list) != 2 || list[0].Name != "an_blog" || list[1].Name != "binh_x" {
		t.Errorf("StandaloneDBs('') = %+v", list)
	}
	if list, err := s.StandaloneDBs(ctx, "khong-co"); err != nil || list == nil || len(list) != 0 {
		t.Errorf("StandaloneDBs(lạ) = %#v, %v", list, err)
	}
	if err := s.SaveStandaloneDB(ctx, account.Database{Name: "x"}); err == nil {
		t.Error("thiếu chủ phải lỗi")
	}
	if err := s.SaveStandaloneDB(ctx, account.Database{Name: "x", Owner: "an", Status: "la"}); err == nil {
		t.Error("trạng thái lạ phải lỗi (CHECK)")
	}

	if err := s.DeleteStandaloneDB(ctx, "an_blog"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteStandaloneDB(ctx, "an_blog"); !errors.Is(err, ErrNotFound) {
		t.Errorf("xoá lần hai: %v", err)
	}
	// Job trên DB rời có chủ là chủ của DB.
	j := sampleJob("jd", t0)
	j.Kind, j.Target, j.Actor = "dbs.suspend", "binh_x", "admin" // P7: chỉ dbs.* tra bảng databases
	_ = s.CreateJob(ctx, j)
	if o, _, _ := s.JobOwner(ctx, "jd"); o != "binh" {
		t.Errorf("owner của job trên DB rời = %q, muốn binh", o)
	}
}

func TestSetSiteOwner(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveSiteOf(t, s, "a.example.com", "an")
	d, err := validate.ParseDomain("a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSiteOwner(ctx, d, "binh"); err != nil {
		t.Fatal(err)
	}
	if o, ok, err := s.SiteOwner(ctx, "a.example.com"); err != nil || !ok || o != "binh" {
		t.Errorf("SiteOwner = %q, %v, %v", o, ok, err)
	}
	// Tên chính trong site_names là bản chép của sites.owner: phải đổi theo.
	if n, ok, err := s.NameSite(ctx, d); err != nil || !ok || n.Owner != "binh" {
		t.Errorf("site_names.owner = %+v, %v, %v", n, ok, err)
	}
	if err := s.SetSiteOwner(ctx, d, ""); err != nil {
		t.Fatal(err)
	}
	if o, _, _ := s.SiteOwner(ctx, "a.example.com"); o != "admin" {
		t.Errorf("owner rỗng phải là admin, được %q", o)
	}
	none, _ := validate.ParseDomain("none.example.com")
	if err := s.SetSiteOwner(ctx, none, "binh"); !errors.Is(err, ErrNotFound) {
		t.Errorf("site không có: %v", err)
	}
}
