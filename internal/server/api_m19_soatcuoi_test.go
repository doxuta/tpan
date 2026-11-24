package server

// Soát cuối M19 (docs/thay-doi/M19-soat-cuoi.md), C1 và C2: POST /apply của
// user thường đi cùng luật tên miền gốc với POST /sites, kể cả khi tên miền gốc
// chỉ được giữ bằng TÊN PHỤ của site người khác, và không lộ name_taken.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

func TestApplyUserRespectsAliasRoot(t *testing.T) {
	e, h := p4Env(t)
	ctx := context.Background()
	// B (site b.example.net) giữ tên phụ example.edu: tên miền gốc example.edu
	// thuộc B dù bảng sites không có tên chính nào dưới nó.
	bSite, alias := apiDomain(t, "b.example.net"), apiDomain(t, "example.edu")
	if err := e.st.ClaimNames(ctx, []site.Claim{{Name: alias, Site: bSite, Owner: authzB, Kind: site.KindAlias, Mode: site.ModeServe}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ActivateNames(ctx, bSite, []validate.Domain{alias}); err != nil {
		t.Fatal(err)
	}
	doms := []string{"shop.example.edu", "example.edu"}
	body := `{"defaults":{"admin_email":"x@example.com"},"sites":[{"domain":"shop.example.edu"},{"domain":"example.edu"}]}`

	// Lời mà POST /sites trả cho cùng tên (đường đã chặn đúng từ trước).
	want := map[string]ErrorBody{}
	for _, d := range doms {
		rec := p4Create(e, h, authzA, d, "?dry_run=1")
		var eb ErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || rec.Code != http.StatusNotFound || eb.Error.Code != "site_not_found" {
			t.Fatalf("POST /sites %s: %d %s; muốn 404 site_not_found", d, rec.Code, rec.Body)
		}
		want[d] = eb
	}

	// Xem trước: mỗi dòng bị từ chối đúng mã và đúng câu của POST /sites
	// (không name_taken, không name_root_foreign: C2).
	p := m13Decode[core.ApplyPreview](t, e.do(h, "POST", "/api/v1/apply?dry_run=1", body, e.cookies[authzA]), http.StatusOK)
	if len(p.Items) != len(doms) {
		t.Fatalf("Items = %+v", p.Items)
	}
	for _, it := range p.Items {
		w := want[it.Domain].Error
		if it.Action != core.ApplyRefused || it.Code != w.Code || it.Reason != w.Message {
			t.Errorf("apply dòng %s: %s %q %+v; muốn refused như POST /sites: %q %+v", it.Domain, it.Action, it.Code, it.Reason, w.Code, w.Message)
		}
	}

	// Chạy thật: không còn gì để làm, không site nào, không dòng tên nào của A.
	if rec := e.do(h, "POST", "/api/v1/apply", body, e.cookies[authzA]); rec.Code != http.StatusConflict || errCode(t, rec) != "nothing_to_do" {
		t.Fatalf("apply chạy thật: %d %s; muốn 409 nothing_to_do", rec.Code, rec.Body)
	}
	for _, d := range doms {
		if _, found, err := e.st.Site(ctx, apiDomain(t, d)); err != nil || found {
			t.Errorf("site %s có trong DB sau apply của A (err %v)", d, err)
		}
	}
	if rows, err := e.st.NamesOf(ctx, apiDomain(t, "shop.example.edu")); err != nil || len(rows) != 0 {
		t.Errorf("dòng tên của shop.example.edu: %+v (err %v)", rows, err)
	}
	if rows, err := e.st.NamesOf(ctx, bSite); err != nil || len(rows) != 2 {
		t.Errorf("tên của B đổi: %+v (err %v)", rows, err)
	}

	// Admin: luật tên miền gốc không áp (vẫn bị luật một tên một site với
	// example.edu, nên chỉ xét shop.example.edu).
	rec := e.do(h, "POST", "/api/v1/apply?dry_run=1", `{"defaults":{"admin_email":"x@example.com"},"sites":[{"domain":"shop.example.edu"}]}`, e.cookies[account.AdminName])
	for _, it := range m13Decode[core.ApplyPreview](t, rec, http.StatusOK).Items {
		if it.Action != core.ApplyCreate {
			t.Errorf("admin, dòng %s: %s %s; muốn create", it.Domain, it.Action, it.Code)
		}
	}
}

// TestNameTooLongIs400 (soát cuối C8): tên phụ quá dài là lỗi đầu vào.
func TestNameTooLongIs400(t *testing.T) {
	if status, body := apiErrorStatus(&core.RefusedError{Code: site.CodeNameTooLong}); status != http.StatusBadRequest || body.Error.Code != site.CodeNameTooLong {
		t.Errorf("apiErrorStatus(name_too_long) = %d %+v; muốn 400", status, body)
	}
}
