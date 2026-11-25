package server

import (
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
)

// POST /api/v1/sites/{domain}/owner: chỉ admin; xem trước không ghi; đổi xong
// thì chủ mới thấy site, chủ cũ không còn thấy.
func TestDoiChuSiteAPI(t *testing.T) {
	e := newM14Env(t)
	path := "/api/v1/sites/" + m14Blog + "/owner"
	body := `{"owner":"` + m14UserB + `"}`

	// Khách không gọi được, kể cả với site của chính mình.
	e.call("POST", "/api/v1/sites/"+m14Shop+"/owner", body, m14UserA)
	if rec := e.call("POST", "/api/v1/sites/"+m14Shop+"/owner", body, m14UserA); rec.Code != 403 {
		t.Fatalf("khách đổi chủ: muốn 403, được %d %s", rec.Code, rec.Body.String())
	}
	if code := apiErrCode(t, e.call("POST", path, `{"owner":"khong_co"}`, account.AdminName), 404); code != core.CodeUserNotFound {
		t.Errorf("chủ không có: %s", code)
	}
	if code := apiErrCode(t, e.call("POST", path, `{"owner":"`+account.AdminName+`"}`, account.AdminName), 409); code != core.CodeOwnerSame {
		t.Errorf("trùng chủ cũ: %s", code)
	}

	// Xem trước: không ghi gì.
	if rec := e.call("POST", path+"?dry_run=1", body, account.AdminName); rec.Code != 200 {
		t.Fatalf("xem trước: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.call("GET", "/api/v1/sites/"+m14Blog, "", m14UserB); rec.Code == 200 {
		t.Fatal("xem trước đã đổi chủ")
	}

	apiDecode[JobStarted](t, e.call("POST", path, body, account.AdminName), 202)
	e.s.api.wg.Wait()
	if rec := e.call("GET", "/api/v1/sites/"+m14Blog, "", m14UserB); rec.Code != 200 {
		t.Errorf("chủ mới phải thấy site: %d", rec.Code)
	}
	if rec := e.call("GET", "/api/v1/sites/"+m14Blog, "", m14UserA); rec.Code == 200 {
		t.Error("khách khác không được thấy site")
	}
}
