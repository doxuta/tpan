package server

// P7 của rà soát v0.2.0, tái hiện đầu-cuối trên máy giả: A có DB rời
// usera_shop; admin tạo user tên usera_shop rồi khoá nó. Bản cũ ghi chủ của
// job user.suspend là A (store suy từ Target trùng tên DB rời), nên A đọc được
// job đó qua GET /jobs/{id} và GET /jobs.

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
)

func TestP7JobCuaUserTrungTenDBRoiKhongLoChoChuDB(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	ctx := context.Background()
	admin, a := e.cookies[account.AdminName], e.cookies[authzA]

	// Phòng thủ: tạo user trùng tên DB rời bị từ chối 409, và ngược lại.
	rec := e.do(h, "POST", "/api/v1/users", `{"name":"`+authzDB+`"}`, admin)
	if rec.Code != http.StatusConflict || errCode(t, rec) != "user_exists" {
		t.Errorf("tạo user %s trùng tên DB rời: %d %s", authzDB, rec.Code, rec.Body)
	}
	if err := e.st.CreateUser(ctx, account.User{Name: "usera_x", Role: account.RoleUser, Created: e.now}, "x"); err != nil {
		t.Fatal(err)
	}
	rec = e.do(h, "POST", "/api/v1/dbs", `{"name":"x"}`, a)
	if rec.Code != http.StatusConflict || errCode(t, rec) != "db_exists" {
		t.Errorf("A tạo DB rời usera_x trùng tên user: %d %s", rec.Code, rec.Body)
	}

	// Dựng thẳng chỗ trùng (như máy đã chạy bản cũ) để thử phần suy chủ.
	if err := e.st.CreateUser(ctx, account.User{Name: authzDB, Role: account.RoleUser, Created: e.now}, "x"); err != nil {
		t.Fatal(err)
	}
	js := m13Decode[JobStarted](t, e.do(h, "POST", "/api/v1/users/"+authzDB+"/suspend", `{}`, admin), http.StatusAccepted)
	e.s.api.wg.Wait()

	if o, ok, err := e.st.JobOwner(ctx, js.JobID); err != nil || !ok || o != account.AdminName {
		t.Errorf("chủ của job user.suspend = %q, %v, %v; muốn admin", o, ok, err)
	}
	got := e.do(h, "GET", "/api/v1/jobs/"+js.JobID, "", a)
	gone := e.do(h, "GET", "/api/v1/jobs/ffffffffffffffff", "", a)
	if got.Code != http.StatusNotFound || !sameNotFound(got, gone, js.JobID, "ffffffffffffffff") {
		t.Errorf("A đọc job khoá user %s: %d %s (job không có: %s)", authzDB, got.Code, got.Body, gone.Body)
	}
	if rec := e.do(h, "GET", "/api/v1/jobs/"+js.JobID+"/events", "", a); rec.Code != http.StatusNotFound {
		t.Errorf("A đọc sự kiện của job khoá user: %d", rec.Code)
	}
	has := func(who *http.Cookie, id string) bool {
		jobs := m13Decode[[]engine.Job](t, e.do(h, "GET", "/api/v1/jobs", "", who), 200)
		return slices.ContainsFunc(jobs, func(j engine.Job) bool { return j.ID == id })
	}
	if has(a, js.JobID) {
		t.Error("A thấy job khoá user của admin trong GET /jobs")
	}
	if !has(admin, js.JobID) {
		t.Error("admin không thấy job mình vừa chạy")
	}

	// A vẫn thấy job của chính site và DB rời của mình (suy chủ theo loại job).
	const dbJob = "00000000000000db"
	if err := e.st.CreateJob(ctx, engine.Job{ID: dbJob, Kind: "dbs.suspend", Target: authzDB, Actor: account.AdminName, Status: engine.JobSucceeded, Started: e.now}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{authzJob, dbJob} {
		if !has(a, id) {
			t.Errorf("A không thấy job %s của site hay DB rời của mình", id)
		}
		if rec := e.do(h, "GET", "/api/v1/jobs/"+id, "", a); rec.Code != 200 {
			t.Errorf("A đọc job %s của mình: %d %s", id, rec.Code, rec.Body)
		}
	}
}
