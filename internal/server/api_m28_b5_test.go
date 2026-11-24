package server

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/doxuta/tpan/internal/site"
)

// TestKhoiPhucChungTenMienGocB5: khôi phục từ thùng rác cũng qua luật P4. A
// đưa www.x.example.info vào thùng rác, admin tạo x.example.info cho B; A
// khôi phục thì nhận 404 như site không tồn tại (cả xem trước lẫn chạy thật).
// Admin vẫn khôi phục được; A khôi phục site dưới gốc không ai dùng như trước.
func TestKhoiPhucChungTenMienGocB5(t *testing.T) {
	e, h := p4Env(t)
	ctx := context.Background()
	for _, d := range []string{"www.x.example.info", "solo.example.biz"} {
		if err := e.st.SaveSite(ctx, site.Site{
			Domain: apiDomain(t, d), Status: site.Trashed, PHPVersion: "8.3",
			Created: e.now, TrashedAt: e.now, Owner: authzA,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.SaveSite(ctx, site.Site{Domain: apiDomain(t, "x.example.info"), Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: authzB}); err != nil {
		t.Fatal(err)
	}
	restore := func(who, d, q string) int {
		return e.do(h, "POST", "/api/v1/sites/"+d+"/restore"+q, "", e.cookies[who]).Code
	}
	for _, q := range []string{"?dry_run=1", ""} {
		p4SameAsMissing(t, e.do(h, "POST", "/api/v1/sites/www.x.example.info/restore"+q, "", e.cookies[authzA]), "www.x.example.info")
	}
	if c := restore(authzA, "solo.example.biz", "?dry_run=1"); c != http.StatusOK {
		t.Errorf("A khôi phục solo.example.biz: %d; muốn 200", c)
	}
	if c := restore("admin", "www.x.example.info", "?dry_run=1"); c != http.StatusOK {
		t.Errorf("admin khôi phục www.x.example.info: %d; muốn 200", c)
	}

	// Lần chạy thật bị chặn có trong nhật ký.
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM audit WHERE action = 'site.restore' AND actor = ? AND target = 'www.x.example.info' AND result = 'site_not_found'", authzA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("audit site.restore của A: %d dòng, muốn 1", n)
	}
}
