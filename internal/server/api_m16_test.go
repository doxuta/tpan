package server

// Test của M16 (docs/thay-doi/M16-thiet-ke.md, B1 và B2): trường db_count của
// GET /api/v1/overview, qua bảng route thật (guard + handler) với ba phiên của
// newAuthzEnv (admin, usera, userb).

import (
	"context"
	"net/http"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
)

// m16Seed thêm vào dữ liệu của newAuthzEnv những thứ dễ đếm sai: một site của
// A nằm trong thùng rác (DB của nó vẫn hiện ở /databases với status trashed),
// một DB rời của A trong thùng rác, và một site đang chạy của admin.
func m16Seed(t *testing.T, e *authzEnv) {
	t.Helper()
	ctx := context.Background()
	for _, s := range []site.Site{
		{Domain: apiDomain(t, "cu.example.com"), Status: site.Trashed, PHPVersion: "8.3", Created: e.now, TrashedAt: e.now, Owner: authzA},
		{Domain: apiDomain(t, "admin.example.net"), Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: account.AdminName},
	} {
		if err := e.st.SaveSite(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.SaveStandaloneDB(ctx, account.Database{
		Name: authzTrashedDB, Owner: authzA, Status: account.DBTrashed, Created: e.now, TrashedAt: e.now,
	}); err != nil {
		t.Fatal(err)
	}
}

// activeRows đếm hàng status "active" của GET /api/v1/databases.
func activeRows(rows []DatabaseRow) int {
	n := 0
	for _, r := range rows {
		if r.Status == "active" {
			n++
		}
	}
	return n
}

// TestOverviewDBCountMatchesDatabases (B1): với mỗi người gọi, db_count bằng
// đúng số hàng đang chạy mà màn Cơ sở dữ liệu của chính người đó liệt kê.
func TestOverviewDBCountMatchesDatabases(t *testing.T) {
	e := newAuthzEnv(t)
	m16Seed(t, e)
	h := e.s.handler(authzDist)
	// Số mong đợi viết tay để test không chỉ so hai đường cùng sai:
	// admin: site của A, site của admin, DB rời usera_app = 3;
	// A: site của mình, DB rời của mình = 2 (site và DB rời trong thùng rác không tính);
	// B: không có gì.
	want := map[string]int{account.AdminName: 3, authzA: 2, authzB: 0}
	for who, n := range want {
		ov := m13Decode[Overview](t, e.do(h, "GET", "/api/v1/overview", "", e.cookies[who]), http.StatusOK)
		dbs := m13Decode[[]DatabaseRow](t, e.do(h, "GET", "/api/v1/databases", "", e.cookies[who]), http.StatusOK)
		if ov.DBCount != n {
			t.Errorf("%s: db_count = %d, muốn %d", who, ov.DBCount, n)
		}
		if got := activeRows(dbs); got != ov.DBCount {
			t.Errorf("%s: db_count = %d nhưng /databases có %d hàng đang chạy (tổng %d hàng)", who, ov.DBCount, got, len(dbs))
		}
	}
}

// TestOverviewDBCountScoped (B2): phiên "xem như" A cho đúng số của A, không
// phải số của admin đang xem.
func TestOverviewDBCountScoped(t *testing.T) {
	e := newAuthzEnv(t)
	m16Seed(t, e)
	h := e.s.handler(authzDist)
	viewAs := e.session(account.AdminName, authzA)
	ov := m13Decode[Overview](t, e.do(h, "GET", "/api/v1/overview", "", viewAs), http.StatusOK)
	if ov.DBCount != 2 {
		t.Errorf("admin xem như %s: db_count = %d, muốn 2 (số của %s)", authzA, ov.DBCount, authzA)
	}
	ovB := m13Decode[Overview](t, e.do(h, "GET", "/api/v1/overview", "", e.cookies[authzB]), http.StatusOK)
	if ovB.DBCount != 0 {
		t.Errorf("%s: db_count = %d, muốn 0: không được tính site hay DB của người khác", authzB, ovB.DBCount)
	}
}

// TestActiveDBCount: hàm đếm thuần, bảng ca biên.
func TestActiveDBCount(t *testing.T) {
	act, tr := site.Site{Status: site.Active}, site.Site{Status: site.Trashed}
	dAct, dTr := account.Database{Status: account.DBActive}, account.Database{Status: account.DBTrashed}
	for _, c := range []struct {
		sites []site.Site
		dbs   []account.Database
		want  int
	}{
		{nil, nil, 0},
		{[]site.Site{act, tr}, nil, 1},
		{nil, []account.Database{dAct, dTr, dAct}, 2},
		{[]site.Site{act, act, tr}, []account.Database{dTr}, 2},
	} {
		if got := activeDBCount(c.sites, c.dbs); got != c.want {
			t.Errorf("activeDBCount(%d site, %d DB rời) = %d, muốn %d", len(c.sites), len(c.dbs), got, c.want)
		}
	}
}
