package core

// Test của các chỗ sửa theo soát cuối M28 đợt 2 (agent-dev/M28-dot-2-ban-nhap/
// soat-cuoi-dot2.md) phần core: harden không bị khách xấu làm hoàn tác (D2),
// mod_status nạp ngoài mods-enabled (D11), hai cách sửa tự động mới
// web.mod_status và db.user_limit (D2).

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// D2: hai khách xấu. Chủ của blog chmod 2751 thư mục neo của mình (Isolated
// từ shop sang blog Fail: `test -x` lọt); shop trả 200 lúc đo trước Plan và
// 500 lúc kiểm chứng. Harden vẫn xong: vhost có dấu F4, status tắt, trần DB
// đặt; hai phép đó nằm trong kết quả job dưới dạng cảnh báo.
func TestD2HardenKhachXauKhongHoanTac(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			e := newM14Env(t, web, m14Blog, m14Shop)
			e.makeOld(t, m14Blog)
			e.makeOld(t, m14Shop)
			blog := e.layout(t, m14Blog)
			e.m.Handle([]string{"test", "-x", blog.Dir}, func(system.Cmd) system.Result { return system.Result{} })

			h, err := e.security().PrepareHarden(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(h.Skipped) != 0 {
				t.Fatalf("trước Plan mọi site lành, Skipped = %+v", h.Skipped)
			}
			e.lk.Lock()
			e.down[m14Shop] = true // 500 từ lúc này: sau khi đo trước Plan
			e.lk.Unlock()

			rep := execHarden(t, h)
			got := resultsByID(rep.Checks)
			for _, id := range []string{"isolation:" + m14Shop + ":" + m14Blog, "http.site:" + m14Shop} {
				if r, ok := got[id]; !ok || r.Status != checks.Warn {
					t.Errorf("báo cáo sau job: %s = %+v, muốn Warn", id, r.Status)
				}
			}
			for _, dom := range []string{m14Blog, m14Shop} {
				wantStatus(t, "sau harden", got, checks.Pass, "web.docroot_guard:"+dom, "web.hardened:"+dom, "log.perm:"+dom)
				if n := e.dbLimit(t, dom); n != site.DBMaxUserConnections {
					t.Errorf("%s: trần kết nối %d, muốn %d", dom, n, site.DBMaxUserConnections)
				}
			}
			if web == Apache && e.modOn("status") {
				t.Error("status còn bật")
			}
		})
	}
}

// D11: status nạp bằng LoadModule ngoài mods-enabled. Harden không hoàn tác
// phần còn lại; web.mod_status trong kết quả job là cảnh báo. Sửa tự động
// web.mod_status thì báo lỗi rõ (job không thành công).
func TestD11ModStatusNapNgoaiModsEnabled(t *testing.T) {
	e := newM14Env(t, Apache, m14Shop)
	e.makeOld(t, m14Shop)
	e.m.Handle([]string{"apachectl", "-M"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "Loaded Modules:\n core_module (static)\n status_module (shared)\n"}
	})
	h, err := e.security().PrepareHarden(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rep := execHarden(t, h)
	got := resultsByID(rep.Checks)
	if r := got["web.mod_status:apache2"]; r.Status != checks.Warn || !strings.Contains(r.Cause.Vi, "DUMP_INCLUDES") {
		t.Errorf("web.mod_status = %s (%s), muốn Warn kèm cách tìm dòng LoadModule", r.Status, r.Cause.Vi)
	}
	wantStatus(t, "sau harden", got, checks.Pass, "web.docroot_guard:"+m14Shop)
	if n := e.dbLimit(t, m14Shop); n != site.DBMaxUserConnections {
		t.Errorf("trần kết nối %d, muốn %d", n, site.DBMaxUserConnections)
	}
	if !e.modOn("status") {
		t.Error("mods-enabled phải được trả về như cũ")
	}

	r := e.recheck(t, "", "web.mod_status:apache2")
	if r.Status != checks.Fail || AutoFixName(r) != FixApacheModOff {
		t.Fatalf("web.mod_status: %s %q", r.Status, AutoFixName(r))
	}
	x, err := e.f.Prepare(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	fr, err := x.Execute(ctx, nil)
	if err != nil || fr.Job.Status == engine.JobSucceeded || !strings.Contains(fr.Job.Error, "DUMP_INCLUDES") {
		t.Errorf("sửa khi module nạp ngoài mods-enabled: %v %s (%s)", err, fr.Job.Status, fr.Job.Error)
	}
}

// health --fix cho web.mod_status: tắt status, Verify chỉ ApacheModStatus.
// Stack nginx thì không có cách sửa.
func TestFixApacheModStatus(t *testing.T) {
	e := newM14Env(t, Apache, m14Shop)
	e.lk.Lock()
	e.mods["status"] = true
	e.lk.Unlock()
	r := e.recheck(t, "", "web.mod_status:apache2")
	if r.Status != checks.Fail || AutoFixName(r) != FixApacheModOff {
		t.Fatalf("status bật: %s %q", r.Status, AutoFixName(r))
	}
	x := e.fix(t, r)
	if ids := stepIDsOf(x); !slices.Equal(ids, []string{"apache.mod-off:status"}) {
		t.Errorf("bước %q", ids)
	}
	if got := checkIDs(t, e.m, x.Plan.Verify); !slices.Equal(got, []string{"web.mod_status:apache2"}) {
		t.Errorf("Verify %q", got)
	}
	if e.modOn("status") {
		t.Error("status còn bật sau khi sửa")
	}

	n := newM14Env(t, Nginx, m14Shop)
	if _, err := n.f.Prepare(ctx, r); err == nil {
		t.Error("stack nginx mà vẫn dựng được cách sửa web.mod_status")
	}
}

// health --fix cho db.user_limit: chỉ user DB của site active hay DB rời
// active, chỉ khi trần đang là 0.
func TestFixDBUserLimit(t *testing.T) {
	e := newM14Env(t, Nginx, m14Shop)
	db := e.layout(t, m14Shop).DB
	e.setDBLimit(t, m14Shop, 0)
	id := "db.user_limit:" + db
	r := e.recheck(t, m14Shop, id)
	if r.Status != checks.Warn || AutoFixName(r) != FixDBUserLimit {
		t.Fatalf("không trần: %s %q", r.Status, AutoFixName(r))
	}
	x := e.fix(t, r)
	if ids := stepIDsOf(x); !slices.Equal(ids, []string{"mariadb.user-limit:" + db}) {
		t.Errorf("bước %q", ids)
	}
	if got := checkIDs(t, e.m, x.Plan.Verify); !slices.Equal(got, []string{id}) {
		t.Errorf("Verify %q", got)
	}
	if n := e.dbLimit(t, m14Shop); n != site.DBMaxUserConnections {
		t.Errorf("trần sau khi sửa %d", n)
	}

	// Trần admin đặt lớn hơn max_connections (151): cảnh báo, nhưng không đè.
	e.setDBLimit(t, m14Shop, 500)
	if r := e.recheck(t, m14Shop, id); r.Status != checks.Warn {
		t.Fatalf("trần 500: %s", r.Status)
	} else if _, err := e.f.Prepare(ctx, r); err == nil {
		t.Error("trần 500 mà vẫn dựng cách sửa")
	}

	// Đối tượng không phải user DB của tpan (root của MariaDB, DB rời trong
	// thùng rác): từ chối trước khi chạm máy.
	e.m.mu.Lock()
	e.m.dbUsers["root"], e.m.dbLimit["root"] = true, 0
	e.m.dbUsers["app_cu"], e.m.dbLimit["app_cu"] = true, 0
	e.m.dbUsers["app_kho"], e.m.dbLimit["app_kho"] = true, 0
	e.m.mu.Unlock()
	e.f.Store = struct {
		SiteStore
		DBStore
	}{e.f.Store, fakeDBs{{Name: "app_kho", Status: account.DBActive}, {Name: "app_cu", Status: account.DBTrashed}}}
	for _, u := range []string{"root", "app_cu"} {
		bad := checks.Result{ID: "db.user_limit:" + u, Status: checks.Warn}
		if _, err := e.f.Prepare(ctx, bad); err == nil {
			t.Errorf("dựng được cách sửa cho user %s", u)
		}
	}
	e.fix(t, checks.Result{ID: "db.user_limit:app_kho", Status: checks.Warn})
	e.m.mu.Lock()
	root, kho := e.m.dbLimit["root"], e.m.dbLimit["app_kho"]
	e.m.mu.Unlock()
	if root != 0 || kho != site.DBMaxUserConnections {
		t.Errorf("trần: root %d (muốn 0), app_kho %d (muốn %d)", root, kho, site.DBMaxUserConnections)
	}
}
