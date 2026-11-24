package server

// M28 đợt 2, B1: Host của lần đăng nhập đúng của admin thành tên dành riêng;
// khách không tạo được site mang tên đó. Luật ở core (m28_b1.go).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/system"
)

func TestDangNhapAdminGiuTenPanelB1(t *testing.T) {
	e := newAuthzEnv(t)
	ctx := context.Background()
	for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
		if err := e.st.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.f.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	e.f.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })
	// Hash trong newAuthzEnv là "x": thay phép so mật khẩu.
	e.s.auth.verify = func(_, pw string) (bool, error) { return pw == "dung-mat-khau", nil }
	h := e.s.handler(authzDist)

	login := func(name, pw, host string) int {
		r := httptest.NewRequest("POST", "/api/v1/login", strings.NewReader(`{"name":"`+name+`","password":"`+pw+`"}`))
		r.Host = host
		r.RemoteAddr = "192.0.2.1:40000"
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	create := func(domain string) *httptest.ResponseRecorder {
		return e.do(h, "POST", "/api/v1/sites?dry_run=1", `{"domain":"`+domain+`","email":"x@example.com"}`, e.cookies[authzA])
	}
	mustOK := func(domain, when string) {
		t.Helper()
		if rec := create(domain); rec.Code != http.StatusOK {
			t.Errorf("%s: khách tạo %s: %d %s; muốn 200", when, domain, rec.Code, rec.Body)
		}
	}

	// Đối chứng: chưa ai đăng nhập bằng tên đó thì khách tạo được.
	mustOK("panel2.example.org", "trước đăng nhập")

	// Đăng nhập sai, Host là IP, user thường đăng nhập: không ghi tên.
	if c := login(account.AdminName, "sai", "panel2.example.org:8443"); c != http.StatusUnauthorized {
		t.Fatalf("đăng nhập sai: %d", c)
	}
	if c := login(account.AdminName, "dung-mat-khau", "192.0.2.10:8443"); c != http.StatusNoContent {
		t.Fatalf("admin đăng nhập bằng IP: %d", c)
	}
	if c := login(authzB, "dung-mat-khau", "panel2.example.org:8443"); c != http.StatusNoContent {
		t.Fatalf("khách B đăng nhập: %d", c)
	}
	if v, ok, _ := e.st.GetSetting(ctx, core.SettingPanelHosts); ok {
		t.Errorf("chưa có lần đăng nhập đúng của admin bằng tên mà đã ghi %q", v)
	}
	mustOK("panel2.example.org", "sau đăng nhập sai/IP/khách")

	// Admin đăng nhập đúng bằng tên: khách không tạo được site mang tên đó.
	if c := login(account.AdminName, "dung-mat-khau", "Panel2.Example.org:8443"); c != http.StatusNoContent {
		t.Fatalf("admin đăng nhập: %d", c)
	}
	rec := create("panel2.example.org")
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != core.CodeDomainReserved {
		t.Errorf("khách tạo panel2.example.org: %d %s; muốn 400 %s", rec.Code, rec.Body, core.CodeDomainReserved)
	}
	// Admin cũng không, kể cả tạo hộ khách.
	rec = e.do(h, "POST", "/api/v1/sites?dry_run=1", `{"domain":"panel2.example.org","email":"x@example.com","owner":"`+authzA+`"}`, e.cookies[account.AdminName])
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != core.CodeDomainReserved {
		t.Errorf("admin tạo panel2.example.org: %d %s; muốn 400 %s", rec.Code, rec.Body, core.CodeDomainReserved)
	}
	mustOK("shop.example.org", "tên khác")
}
