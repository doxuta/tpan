package demo

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func demoDBs(sys system.System, st *store.Store) *core.DBs {
	return &core.DBs{Sys: sys, Settings: st, Store: st, Sites: st, Accounts: st, Journal: st}
}

// Máy demo có đủ dữ liệu cho mọi màn mới của M13: gói, hai user, chủ của
// site, một site tạm khoá, một DB rời đang dùng, một DB rời trong thùng rác.
func TestM13Seed(t *testing.T) {
	ctx, sys, st := newDemo(t)

	p, ok, err := st.Package(ctx, pkgBasic)
	if err != nil || !ok || p.MaxSites != 2 || p.MaxDBs != 1 || p.DiskMB == 0 || p.MaxBackups == 0 {
		t.Errorf("gói %s = %+v, %v, %v", pkgBasic, p, ok, err)
	}
	// khach_b dùng gói default ở seedM13, rồi seedM17 đổi sang gói có lồng nang_cao.
	for name, pkg := range map[string]string{userA: pkgBasic, userB: pkgPlus} {
		u, ok, err := st.User(ctx, name)
		if err != nil || !ok || u.Role != account.RoleUser || u.Package != pkg || u.Status != account.Active {
			t.Errorf("user %s = %+v, %v, %v", name, u, ok, err)
		}
		use, err := st.Usage(ctx, name)
		if err != nil || use.DiskBytes <= 0 {
			t.Errorf("dung lượng của %s chưa đo: %+v, %v", name, use, err)
		}
	}
	if !slices.Equal(Users(), []string{userA, userB}) {
		t.Errorf("Users() = %v", Users())
	}

	owners := map[string]string{siteBlog: account.AdminName, siteShop: userA, siteWiki: userB, siteLab: userB,
		siteLongShop: userA, siteLongEdu: account.AdminName, siteLongNews: userB}
	for dom, want := range owners {
		s, ok, err := st.Site(ctx, domain(t, dom))
		if err != nil || !ok || s.Owner != want {
			t.Errorf("chủ của %s = %q (%v, %v); muốn %q", dom, s.Owner, ok, err, want)
		}
		wantSusp := account.NotSuspended
		if dom == siteLab {
			wantSusp = account.SuspendSelf
		}
		if s.Suspended != wantSusp {
			t.Errorf("%s tạm khoá = %q; muốn %q", dom, s.Suspended, wantSusp)
		}
	}
	// khach_a (gói co_ban, tối đa 2 site) đủ 2/2: màn Người dùng có thanh mức dùng đầy.
	all, err := st.Sites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nA := 0
	for _, s := range all {
		if s.Owner == userA {
			nA++
		}
	}
	if nA != 2 {
		t.Errorf("%s có %d site; muốn đúng 2 (đủ hạn mức gói %s)", userA, nA, pkgBasic)
	}

	dbs, err := st.StandaloneDBs(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]account.DBStatus{}
	for _, d := range dbs {
		got[d.Name] = d.Status
	}
	want := map[string]account.DBStatus{userA + "_" + dbApp: account.DBActive, userB + "_" + dbOld: account.DBTrashed}
	if len(got) != len(want) || got[userA+"_"+dbApp] != account.DBActive || got[userB+"_"+dbOld] != account.DBTrashed {
		t.Errorf("DB rời = %v; muốn %v", got, want)
	}
	if _, err := sys.ReadFile(account.DBTrashDump(userB + "_" + dbOld)); err != nil {
		t.Errorf("bản xuất của DB trong thùng rác: %v", err)
	}
}

// Màn Cơ sở dữ liệu: DB của site và DB rời có charset, số bảng, dung lượng
// (máy giả trả lời câu năm cột của DBs.List).
func TestM13DatabaseList(t *testing.T) {
	ctx, sys, st := newDemo(t)
	list, err := demoDBs(sys, st).List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, d := range list {
		kinds[d.DB] = d.Kind
		if d.Status == string(account.DBTrashed) {
			continue
		}
		if d.SizeBytes <= 0 || d.Tables <= 0 || d.Charset != "utf8mb4" || d.Collation == "" {
			t.Errorf("dòng %s thiếu số liệu: %+v", d.DB, d)
		}
	}
	if kinds[userA+"_"+dbApp] != core.DBKindStandalone || kinds[userB+"_"+dbOld] != core.DBKindStandalone {
		t.Errorf("thiếu DB rời trong danh sách: %v", kinds)
	}
}

// Thao tác của màn Cơ sở dữ liệu chạy được trên máy giả: đổi charset, khoá và
// mở khoá, đổi mật khẩu, vào thùng rác và khôi phục; tạo DB thứ hai cho
// khach_a bị từ chối vì gói chỉ cho một.
func TestM13DBOpsOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	d := demoDBs(sys, st)
	name := userA + "_" + dbApp

	runOK(t, ctx, "charset")(d.PrepareCharset(ctx, name, "utf8mb4", "utf8mb4_unicode_ci"))
	if row, _, _ := st.StandaloneDB(ctx, name); row.Collation != "utf8mb4_unicode_ci" {
		t.Errorf("collation sau khi đổi = %q", row.Collation)
	}
	runOK(t, ctx, "suspend")(d.PrepareSuspend(ctx, name))
	runOK(t, ctx, "unsuspend")(d.PrepareUnsuspend(ctx, name))
	pw, err := d.PreparePassword(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	runOK(t, ctx, "passwd")(pw.Planned, nil)
	runOK(t, ctx, "trash")(d.PrepareTrash(ctx, name))
	runOK(t, ctx, "restore")(d.PrepareRestore(ctx, name))

	owner, err := validate.ParseUserName(userA)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.PrepareAdd(ctx, core.DBInput{Owner: owner, Name: "thu_hai"})
	var re *core.RefusedError
	if !errors.As(err, &re) || re.Code != core.CodeQuotaDBs {
		t.Errorf("DB thứ hai của %s: %v; muốn %s", userA, err, core.CodeQuotaDBs)
	}
}

// Mở khoá site tạm khoá trên giao diện demo: site trả 200 lại, health hết
// chặng xám; khoá lại thì xám trở lại.
func TestM13UnsuspendOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	s := NewSites(sys, st)
	lab := domain(t, siteLab)
	// Sites.Checks là bộ phép kiểm của health (theo trạng thái tạm khoá).
	run := func() []checks.Result {
		t.Helper()
		cs, err := s.Checks(ctx, lab)
		if err != nil {
			t.Fatal(err)
		}
		return checks.RunAll(ctx, sys, cs)
	}

	rs := run()
	if got := statusOf(rs, "http.suspended:"); got != checks.Pass {
		t.Errorf("site tạm khoá: http.suspended = %q; muốn pass (%v)", got, notPass(rs))
	}

	runOK(t, ctx, "unsuspend")(s.PrepareUnsuspend(ctx, lab))
	rs = run()
	var bad []string
	for _, r := range rs {
		if r.Status != checks.Pass && !strings.HasPrefix(r.ID, "tls.cert:") && !strings.HasPrefix(r.ID, "dns.") {
			bad = append(bad, r.ID)
		}
	}
	if len(bad) > 0 || statusOf(rs, "http.site:") != checks.Pass {
		t.Errorf("sau khi mở khoá còn phép kiểm không đạt: %v", notPass(rs))
	}
	runOK(t, ctx, "suspend")(s.PrepareSuspend(ctx, lab))
}
