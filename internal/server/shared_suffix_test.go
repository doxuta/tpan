package server

import (
	"context"
	"net/url"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/validate"
)

// Tình huống máy training: mọi site nằm dưới một tên miền. Chưa khai tên miền
// dùng chung thì khách không tạo được site ở đó (luật tên miền gốc); khai rồi
// thì mỗi tên con là của riêng một khách.
func TestTenMienDungChung(t *testing.T) {
	e := newM14Env(t)
	t.Cleanup(func() { validate.SetSharedSuffixes(nil) })
	body := `{"domain":"moi.example.net","email":"b@example.com"}`
	check := func() *FieldCheck {
		q := url.Values{"domain": {"moi.example.net"}}
		return apiDecode[SiteCheck](t, e.call("GET", "/api/v1/site-check?"+q.Encode(), "", m14UserB), 200).Domain
	}

	if rec := e.call("POST", "/api/v1/sites?dry_run=1", body, m14UserB); rec.Code != 404 {
		t.Fatalf("chưa khai: muốn 404, được %d %s", rec.Code, rec.Body.String())
	}
	if got := check(); got.OK {
		t.Fatalf("chưa khai mà ô kiểm báo dùng được: %+v", got)
	}

	if _, err := core.SetSharedSuffix(context.Background(), e.st, apiDomain(t, "example.net"), true); err != nil {
		t.Fatal(err)
	}
	if rec := e.call("POST", "/api/v1/sites?dry_run=1", body, m14UserB); rec.Code != 200 {
		t.Fatalf("đã khai: muốn 200, được %d %s", rec.Code, rec.Body.String())
	}
	if got := check(); !got.OK {
		t.Errorf("đã khai mà ô kiểm vẫn chặn: %+v", got)
	}
	// Tên con của site người khác vẫn bị chặn: www.shop.example.net thuộc gốc shop.example.net của khach_a.
	if rec := e.call("POST", "/api/v1/sites?dry_run=1", `{"domain":"www.`+m14Shop+`","email":"b@example.com"}`, m14UserB); rec.Code != 404 {
		t.Errorf("tên con của site người khác: muốn 404, được %d", rec.Code)
	}
}
