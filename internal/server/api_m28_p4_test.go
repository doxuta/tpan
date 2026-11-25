package server

// M28 đợt 2, P4: user thường không được tạo site mới chung tên miền gốc
// (eTLD+1) với site của người khác: chặn chiếm www.<site của người khác> rồi xin
// chứng chỉ cho tên đó. Luật ở core.RegistrableTaken; đây là test qua bảng
// route thật cho POST /sites và POST /apply.
//
// Hệ quả có chủ ý: hai khách không thể có hai tên con của cùng một domain gốc,
// nên mỗi khách trong test dùng một gốc riêng: A dưới example.com (a.example.com
// của newAuthzEnv), B dưới example.net, example.org, co.uk, github.io.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// p4Env là newAuthzEnv cộng site của B và stack đã cài (để bản xem trước của
// ca được phép ra 200, như apiEnv.stack).
func p4Env(t *testing.T) (*authzEnv, http.Handler) {
	t.Helper()
	e := newAuthzEnv(t)
	ctx := context.Background()
	for _, d := range []string{"b.example.net", "www.c.example.org", "shop.example.co.uk", "b.github.io"} {
		if err := e.st.SaveSite(ctx, site.Site{Domain: apiDomain(t, d), Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: authzB}); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
		if err := e.st.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.f.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	e.f.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })
	return e, e.s.handler(authzDist)
}

func p4Create(e *authzEnv, h http.Handler, who, domain, q string) *httptest.ResponseRecorder {
	return e.do(h, "POST", "/api/v1/sites"+q, `{"domain":"`+domain+`","email":"x@example.com"}`, e.cookies[who])
}

// p4SameAsMissing: đúng mã, đúng mã lỗi và đúng câu của "không có site d".
func p4SameAsMissing(t *testing.T, rec *httptest.ResponseRecorder, domain string) {
	t.Helper()
	status, want := apiErrorStatus(siteNotFound(apiDomain(t, domain)))
	sudoNote(&want) // người gọi trong các test này là user thường
	var got ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%s: thân không đọc được: %s", domain, rec.Body)
	}
	if rec.Code != status || got.Error.Code != want.Error.Code || got.Error.Message != want.Error.Message {
		t.Errorf("%s: %d %s; muốn đúng lời site không tồn tại (%d %+v)", domain, rec.Code, rec.Body, status, want.Error)
	}
}

// p4CreateRefused: lời từ chối của POST /sites cho tên thuộc người khác: cùng
// mã HTTP và mã lỗi với "không có site", không nhắc chủ hay site nào khác.
func p4CreateRefused(t *testing.T, rec *httptest.ResponseRecorder, domain string) {
	t.Helper()
	status, want := apiErrorStatus(siteCreateRefused(apiDomain(t, domain)))
	var got ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%s: thân không đọc được: %s", domain, rec.Body)
	}
	if rec.Code != 404 || rec.Code != status || got.Error.Code != "site_not_found" || got.Error.Message != want.Error.Message {
		t.Errorf("%s: %d %s; muốn lời từ chối tạo site (%d %+v)", domain, rec.Code, rec.Body, status, want.Error)
	}
}

func TestTaoSiteChungTenMienGocP4(t *testing.T) {
	e, h := p4Env(t)
	for _, q := range []string{"?dry_run=1", ""} {
		for _, d := range []string{
			"www.b.example.net", "mail.b.example.net", "b.example.net", "example.net",
			// Ngược lại: B có www.c.example.org thì A không lấy được c.example.org.
			"c.example.org", "example.org",
			// Đuôi công cộng nhiều nhãn: eTLD là co.uk, eTLD+1 là example.co.uk.
			"example.co.uk", "x.shop.example.co.uk",
			// Hoa thường như nhau (validate hạ chữ thường).
			"WWW.B.Example.NET",
		} {
			p4CreateRefused(t, p4Create(e, h, authzA, d, q), d)
		}
	}

	// Được phép: cùng gốc với site của chính mình; github.io là public suffix
	// (phần private của PSL, EffectiveTLDPlusOne có tính) nên a.github.io và
	// b.github.io là hai tên đăng ký khác nhau.
	for _, d := range []string{"www.a.example.com", "shop.a.example.com", "a.github.io"} {
		if rec := p4Create(e, h, authzA, d, "?dry_run=1"); rec.Code != http.StatusOK {
			t.Errorf("A tạo %s: %d %s; muốn 200", d, rec.Code, rec.Body)
		}
	}
	// Tên chính là public suffix: user thường 400 domain_invalid.
	for _, d := range []string{"github.io", "co.uk"} {
		if rec := p4Create(e, h, authzA, d, "?dry_run=1"); rec.Code != http.StatusBadRequest || errCode(t, rec) != "domain_invalid" {
			t.Errorf("A tạo %s: %d %s; muốn 400 domain_invalid", d, rec.Code, rec.Body)
		}
	}
	// Admin không đổi gì: tạo được mọi tên, kể cả cho khách.
	for _, d := range []string{"www.b.example.net", "example.co.uk", "github.io"} {
		if rec := p4Create(e, h, account.AdminName, d, "?dry_run=1"); rec.Code != http.StatusOK {
			t.Errorf("admin tạo %s: %d %s; muốn 200", d, rec.Code, rec.Body)
		}
	}
	rec := e.do(h, "POST", "/api/v1/sites?dry_run=1", `{"domain":"mail.b.example.net","email":"x@example.com","owner":"`+authzA+`"}`, e.cookies[account.AdminName])
	if rec.Code != http.StatusOK {
		t.Errorf("admin tạo mail.b.example.net cho A: %d %s", rec.Code, rec.Body)
	}

	// Chạy thật bị chặn thì ghi nhật ký như nhánh trùng tên.
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM audit WHERE action = 'site.add' AND actor = ? AND target = 'mail.b.example.net' AND result = 'site_not_found'", authzA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("audit site.add mail.b.example.net của A: %d dòng, muốn 1 (chỉ lần chạy thật)", n)
	}
}

// TestTaoSiteChungTenMienGocP4ThungRac: site của B ở thùng rác vẫn giữ tên
// miền gốc (B khôi phục được).
func TestTaoSiteChungTenMienGocP4ThungRac(t *testing.T) {
	e, h := p4Env(t)
	if err := e.st.SaveSite(context.Background(), site.Site{
		Domain: apiDomain(t, "old.example.info"), Status: site.Trashed, PHPVersion: "8.3",
		Created: e.now, TrashedAt: e.now, Owner: authzB,
	}); err != nil {
		t.Fatal(err)
	}
	p4CreateRefused(t, p4Create(e, h, authzA, "www.old.example.info", "?dry_run=1"), "www.old.example.info")
}

// TestApplyChungTenMienGocP4: POST /apply theo phạm vi user đi cùng luật.
func TestApplyChungTenMienGocP4(t *testing.T) {
	e, h := p4Env(t)
	// Chỉ xem trước: chạy thật đi qua đúng PrepareApply đó (handleApply), khỏi
	// chạy job cài site trên máy giả.
	body := `{"defaults":{"admin_email":"x@example.com"},"sites":[{"domain":"www.b.example.net"},{"domain":"c.example.org"},{"domain":"github.io"},{"domain":"www.a.example.com"}]}`
	p := m13Decode[core.ApplyPreview](t, e.do(h, "POST", "/api/v1/apply?dry_run=1", body, e.cookies[authzA]), http.StatusOK)
	want := map[string]string{"www.b.example.net": "site_not_found", "c.example.org": "site_not_found", "github.io": "domain_invalid", "www.a.example.com": ""}
	if len(p.Items) != len(want) {
		t.Fatalf("Items = %+v", p.Items)
	}
	for _, it := range p.Items {
		code, ok := want[it.Domain]
		if !ok || it.Code != code || (code == "") != (it.Action == core.ApplyCreate) {
			t.Errorf("dòng %s: %s %s; muốn mã %q", it.Domain, it.Action, it.Code, code)
		}
	}
	// Admin áp cùng file: không dòng nào bị luật P4 chặn.
	rec := e.do(h, "POST", "/api/v1/apply?dry_run=1", `{"defaults":{"admin_email":"x@example.com"},"sites":[{"domain":"www.b.example.net"},{"domain":"github.io"}]}`, e.cookies[account.AdminName])
	for _, it := range m13Decode[core.ApplyPreview](t, rec, http.StatusOK).Items {
		if it.Action != core.ApplyCreate {
			t.Errorf("admin, dòng %s: %s %s; muốn create", it.Domain, it.Action, it.Code)
		}
	}
}
