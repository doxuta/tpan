package core

// Test của M28 đợt 2 phần core (H5, M1-DBCONN, S2, nối phép kiểm mới):
// Plan cài stack và chuyển sang Apache tắt status; harden tắt status, đặt
// trần kết nối cho user DB, không để một site hỏng làm hoàn tác bản vá cả máy;
// hai cách sửa tự động mới (web.docroot_guard, php.pool).

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// apacheModStep trả bước bật/tắt module của stack trong Plan.
func apacheModStep(t *testing.T, p engine.Plan) *steps.ApacheModules {
	t.Helper()
	for _, s := range p.Steps {
		if m, ok := s.(*steps.ApacheModules); ok {
			return m
		}
	}
	t.Fatalf("Plan không có bước module Apache: %q", stepIDs(p))
	return nil
}

// H5: cài stack Apache tắt cả mpm_prefork lẫn status (gói Ubuntu bật sẵn cả hai).
func TestH5InstallApacheTatStatus(t *testing.T) {
	s, m, _ := newTestStack(t)
	in, err := s.PrepareInstall(ctx, Apache)
	if err != nil {
		t.Fatal(err)
	}
	if got := apacheModStep(t, in.Plan).Disable; !slices.Equal(got, steps.ApacheModulesOff) {
		t.Errorf("Disable = %q, muốn %q", got, steps.ApacheModulesOff)
	}
	rep, err := in.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mods["status"] || m.mods["mpm_prefork"] || !m.mods["mpm_event"] {
		t.Errorf("module sau khi cài: %v", m.mods)
	}
}

// H5: chuyển nginx sang Apache cũng tắt status.
func TestH5SwitchApacheTatStatus(t *testing.T) {
	e := newSwitchEnv(t, Nginx, SourceUbuntu, switchSite(t, "a.example.com", false))
	p, err := e.sw().Prepare(ctx, Apache)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := apacheModStep(t, p.Plan).Disable; !slices.Contains(got, "status") {
		t.Errorf("Disable = %q, thiếu status", got)
	}
	rep, _, err := e.execute(t, p)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v %s %s%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	e.lk.Lock()
	defer e.lk.Unlock()
	if e.mods["status"] {
		t.Errorf("status còn bật sau khi chuyển sang Apache: %v", e.mods)
	}
}

// S2: một site trả 500 trước Plan vẫn được vá đủ, nhưng phép HTTP và cô lập
// của nó không vào kiểm chứng, nên job không hoàn tác; site đó nêu trong
// Skipped. Site còn lại vẫn có đủ phép kiểm.
func TestHardenSiteHongVanQua(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newM14Env(t, web, m14Blog, m14Shop)
			e.makeOld(t, m14Blog)
			e.makeOld(t, m14Shop)
			e.lk.Lock()
			e.down[m14Blog] = true
			e.lk.Unlock()

			h, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(h.Skipped) != 1 || h.Skipped[0].Kind != HardenSkipSiteHTTP || h.Skipped[0].Target != m14Blog ||
				!strings.Contains(h.Skipped[0].Message.Vi, m14Blog) || !strings.Contains(h.Skipped[0].Message.Vi, "tạm khoá") ||
				h.Skipped[0].Result == nil || h.Skipped[0].Result.Status != checks.Fail {
				t.Fatalf("Skipped = %+v; muốn đúng site %s, kèm kết quả Fail", h.Skipped, m14Blog)
			}
			if got, want := stepIDs(h.Plan), e.hardenStepIDs(t, m14Blog, m14Shop); !slices.Equal(got, want) {
				t.Errorf("site hỏng phải vẫn có đủ bước:\n có   %q\n muốn %q", got, want)
			}
			ids := checkIDs(t, system.NewFake(), h.Plan.Verify)
			for _, id := range ids {
				if strings.Contains(id, m14Blog) && (strings.HasPrefix(id, "http.") || strings.HasPrefix(id, "isolation:")) {
					t.Errorf("kiểm chứng còn phép %s của site hỏng", id)
				}
			}
			for _, id := range []string{"web.hardened:" + m14Blog, "web.docroot_guard:" + m14Blog, "log.perm:" + m14Blog, "http.site:" + m14Shop} {
				if !slices.Contains(ids, id) {
					t.Errorf("kiểm chứng thiếu %s: %v", id, ids)
				}
			}

			execHarden(t, h)
			wantStatus(t, "site hỏng sau harden", e.siteResults(t, m14Blog), checks.Pass, "web.hardened:"+m14Blog, "web.docroot_guard:"+m14Blog, "log.perm:"+m14Blog)
			if n := e.dbLimit(t, m14Blog); n != site.DBMaxUserConnections {
				t.Errorf("site hỏng: trần kết nối %d, muốn %d", n, site.DBMaxUserConnections)
			}
			if web == Apache && e.modOn("status") {
				t.Error("site hỏng làm status không được tắt")
			}
		})
	}
}

// S16: trần admin đã nâng thì giữ; user DB không có thì không có bước, nêu
// trong Skipped, job vẫn qua.
func TestHardenDBUserLimitGiuVaBoQua(t *testing.T) {
	e := newM14Env(t, Nginx, m14Blog, m14Shop)
	e.setDBLimit(t, m14Blog, 50)
	shop := e.layout(t, m14Shop).DB
	e.m.mu.Lock()
	delete(e.m.dbUsers, shop)
	e.m.mu.Unlock()

	h, err := e.security().PrepareHarden(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := stepIDs(h.Plan)
	if slices.Contains(ids, "mariadb.user-limit:"+shop) || !slices.Contains(ids, "mariadb.user-limit:"+e.layout(t, m14Blog).DB) {
		t.Errorf("bước: %q", ids)
	}
	if !slices.ContainsFunc(h.Skipped, func(s HardenSkipped) bool { return s.Kind == HardenSkipDBUser && s.Target == shop }) {
		t.Errorf("Skipped = %+v; muốn có user %s", h.Skipped, shop)
	}
	execHarden(t, h)
	if n := e.dbLimit(t, m14Blog); n != 50 {
		t.Errorf("trần admin đã nâng bị đổi thành %d", n)
	}
}

// S2, S16: MariaDB trả output lạ khi đọc trần của một user thì user đó không
// có bước (nêu trong Skipped kèm lỗi), user khác vẫn có bước, job vẫn qua.
func TestHardenDBUserLimitDocLoi(t *testing.T) {
	e := newM14Env(t, Nginx, m14Blog, m14Shop)
	blog, shop := e.layout(t, m14Blog).DB, e.layout(t, m14Shop).DB
	e.m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		if sql := string(c.Stdin); strings.Contains(sql, "COALESCE(MAX(max_user_connections)") && strings.Contains(sql, "'"+shop+"'") {
			return system.Result{Stdout: "rác\n"}
		}
		return e.m.sql(string(c.Stdin))
	})

	h, err := e.security().PrepareHarden(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := stepIDs(h.Plan)
	if slices.Contains(ids, "mariadb.user-limit:"+shop) || !slices.Contains(ids, "mariadb.user-limit:"+blog) {
		t.Errorf("bước: %q", ids)
	}
	if !slices.ContainsFunc(h.Skipped, func(s HardenSkipped) bool {
		return s.Kind == HardenSkipDBUser && s.Target == shop && strings.Contains(s.Message.Vi, "không đọc được")
	}) {
		t.Errorf("Skipped = %+v; muốn có user %s kèm lỗi đọc", h.Skipped, shop)
	}
	execHarden(t, h)
	if n := e.dbLimit(t, m14Blog); n != site.DBMaxUserConnections {
		t.Errorf("trần của %s: %d, muốn %d", blog, n, site.DBMaxUserConnections)
	}
}

// fakeDBs là bảng DB rời trong bộ nhớ (DBStore).
type fakeDBs []account.Database

func (f fakeDBs) StandaloneDB(_ context.Context, name string) (account.Database, bool, error) {
	for _, d := range f {
		if d.Name == name {
			return d, true, nil
		}
	}
	return account.Database{}, false, nil
}
func (f fakeDBs) StandaloneDBs(context.Context, string) ([]account.Database, error) { return f, nil }
func (fakeDBs) SaveStandaloneDB(context.Context, account.Database) error            { return nil }
func (fakeDBs) DeleteStandaloneDB(context.Context, string) error                    { return nil }

// M1-DBCONN: harden đặt trần cho user của DB rời active, không đụng DB rời
// trong thùng rác. Câu SQL trong bản xem trước không có mật khẩu.
func TestHardenDBRoiVaBanXemTruoc(t *testing.T) {
	e := newM14Env(t, Nginx, m14Shop)
	e.makeOld(t, m14Shop)
	e.m.mu.Lock()
	e.m.dbUsers["app_kho"], e.m.dbLimit["app_kho"] = true, 0
	e.m.dbUsers["app_cu"], e.m.dbLimit["app_cu"] = true, 0
	e.m.mu.Unlock()
	sec := e.security()
	sec.DBs = fakeDBs{{Name: "app_kho", Status: account.DBActive}, {Name: "app_cu", Status: account.DBTrashed}}

	h, err := sec.PrepareHarden(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := stepIDs(h.Plan)
	if !slices.Contains(ids, "mariadb.user-limit:app_kho") || slices.Contains(ids, "mariadb.user-limit:app_cu") {
		t.Errorf("bước: %q", ids)
	}
	pv, err := h.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(pv)
	for _, bad := range []string{"IDENTIFIED", "PASSWORD", "authentication_string"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("bản xem trước có %q", bad)
		}
	}
	if !strings.Contains(string(b), "WITH MAX_USER_CONNECTIONS 20") {
		t.Error("bản xem trước thiếu câu ALTER USER ... WITH MAX_USER_CONNECTIONS 20")
	}
	execHarden(t, h)
	e.m.mu.Lock()
	kho, cu := e.m.dbLimit["app_kho"], e.m.dbLimit["app_cu"]
	e.m.mu.Unlock()
	if kho != 20 || cu != 0 {
		t.Errorf("trần: app_kho %d (muốn 20), app_cu %d (muốn 0)", kho, cu)
	}
	// Sites là *store.Store-như (cũng là DBStore) thì không cần đặt DBs.
	if (&Security{Sites: struct {
		SiteStore
		DBStore
	}{e.sites, fakeDBs{}}}).dbStore() == nil {
		t.Error("dbStore không lấy DBStore từ Sites")
	}
}

// Khôi phục từ thùng rác đặt trần cho user DB của site (S16).
func TestRestoreDatTranKetNoi(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	se.add(t, m14Shop)
	se.trash(t, m14Shop)
	db := site.LayoutOf(mustDomain(t, m14Shop)).DB
	se.m.mu.Lock()
	se.m.dbLimit[db] = 0 // site vào thùng rác trước khi máy có bản vá
	se.m.mu.Unlock()
	rs, err := se.s.PrepareRestore(ctx, mustDomain(t, m14Shop))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := rs.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("khôi phục: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	se.m.mu.Lock()
	n := se.m.dbLimit[db]
	se.m.mu.Unlock()
	if n != site.DBMaxUserConnections {
		t.Errorf("trần sau khôi phục %d, muốn %d", n, site.DBMaxUserConnections)
	}
}

// health --fix cho web.docroot_guard: vhost nginx còn dòng from= cũ (đạt
// web.hardened, không đạt dấu F4) được ghi lại, kể cả khi sổ managed_files
// đang ghi đúng bản cũ.
func TestFixDocrootGuard(t *testing.T) {
	e := newM14Env(t, Nginx, m14Shop)
	conf := e.conf(t, m14Shop)
	good := e.read(t, conf)
	old := strings.ReplaceAll(good, templates.NginxSymlinkGuardV2, templates.NginxSymlinkGuard)
	if old == good {
		t.Fatal("vhost không có dòng của bản vá F4")
	}
	e.m.SetFile(conf, []byte(old), 0o644, system.Root)
	e.f.Files = fileRecords{conf: shaHex([]byte(old))}

	id := "web.docroot_guard:" + m14Shop
	r := e.recheck(t, m14Shop, id)
	if r.Status != checks.Fail || AutoFixName(r) != FixWebRewrite {
		t.Fatalf("vhost cũ: %s %q", r.Status, AutoFixName(r))
	}
	if h := e.recheck(t, m14Shop, "web.hardened:"+m14Shop); h.Status != checks.Pass {
		t.Errorf("web.hardened phải nhận dòng cũ: %s", h.Status)
	}
	x := e.fix(t, r)
	if !slices.Contains(checkIDs(t, e.m, x.Plan.Verify), id) {
		t.Errorf("Verify thiếu %s", id)
	}
	if e.read(t, conf) != good {
		t.Error("vhost sau khi sửa khác bản của template")
	}
	if r := e.recheck(t, m14Shop, id); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}
}

// health --fix cho php.pool: pool bị sửa tay sang TCP được ghi lại từ template.
func TestFixPHPPool(t *testing.T) {
	e := newM14Env(t, Nginx, m14Shop)
	l := e.layout(t, m14Shop)
	p := l.PoolConf("8.3")
	good := e.read(t, p)
	bad := strings.ReplaceAll(good, "listen = "+l.SocketFor("8.3"), "listen = 127.0.0.1:9000")
	if bad == good {
		t.Fatal("pool không có dòng listen như mong đợi")
	}
	e.m.SetFile(p, []byte(bad), 0o644, system.Root)

	id := "php.pool:" + m14Shop
	r := e.recheck(t, m14Shop, id)
	if r.Status != checks.Fail || AutoFixName(r) != FixPHPPool {
		t.Fatalf("pool TCP: %s %q (%s)", r.Status, AutoFixName(r), r.Summary.Vi)
	}
	x := e.fix(t, r)
	if got := stepIDsOf(x); !slices.Equal(got, []string{"file:" + p}) {
		t.Errorf("bước %q", got)
	}
	if e.read(t, p) != good {
		t.Error("pool sau khi sửa khác bản của template")
	}
	if r := e.recheck(t, m14Shop, id); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}

	// Site tạm khoá: không ghi lại pool (mở khoá lén).
	planned(t)(e.s.PrepareSuspend(ctx, mustDomain(t, m14Shop)))
	if _, err := e.f.Prepare(ctx, r); err == nil {
		t.Error("sửa php.pool cho site tạm khoá mà không bị từ chối")
	}
}
