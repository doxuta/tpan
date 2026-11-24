package core

// Soát cuối M19 (docs/thay-doi/M19-soat-cuoi.md): C8 (tên phụ dài làm hỏng
// bảng băm tên của nginx) và C9 (tên www mặc định, tên khớp wildcard của vhost
// khác).

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// soatNamesEnv là newNamesEnv với `nginx -T` giả giống bản thật hơn: luôn in
// nginx.conf trước, kể cả khi sites-enabled rỗng (nginx thật không bao giờ in
// rỗng khi thành công).
func soatNamesEnv(t *testing.T) *siteEnv {
	t.Helper()
	se := newNamesEnv(t)
	m := se.m
	m.Handle([]string{"nginx", "-T"}, func(system.Cmd) system.Result {
		var b strings.Builder
		b.WriteString("# configuration file /etc/nginx/nginx.conf:\nhttp {\n    include /etc/nginx/sites-enabled/*;\n}\n")
		names, _ := m.ReadDir("/etc/nginx/sites-enabled")
		for _, n := range names {
			p := "/etc/nginx/sites-enabled/" + n
			if data, err := m.ReadFile(p); err == nil {
				b.WriteString("# configuration file " + p + ":\n" + string(data) + "\n")
			}
		}
		return system.Result{Stdout: b.String()}
	})
	return se
}

// nameOfLen trả tên n byte có đuôi suffix.
func nameOfLen(n int, suffix string) string {
	return strings.Repeat("a", n-len(suffix)) + suffix
}

func TestAliasNameTooLong(t *testing.T) {
	se := soatNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	ctx := context.Background()
	d := mustDomain(t, "example.com")
	long := mustDomain(t, nameOfLen(site.MaxNameLen+1, ".example.com"))
	for _, notAdmin := range []bool{false, true} {
		se.s.NotAdmin = notAdmin
		_, err := se.s.PrepareAliasAdd(ctx, d, long, site.ModeServe)
		if nameCode(err) != site.CodeNameTooLong || !strings.Contains(err.Error(), "46") {
			t.Errorf("NotAdmin=%v, tên %d byte: %v; muốn name_too_long nêu trần 46", notAdmin, len(long.String()), err)
		}
	}
	se.s.NotAdmin = false
	ok := mustDomain(t, nameOfLen(site.MaxNameLen, ".example.com"))
	op, err := se.s.PrepareAliasAdd(ctx, d, ok, site.ModeServe)
	res := execNames(t, op, err)
	if res.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("tên 46 byte: %s %s", res.Report.Job.Status, res.Report.Job.Error)
	}
	if _, found := rowsOf(t, se, "example.com")[ok.String()]; !found {
		t.Error("tên 46 byte chưa vào DB")
	}
}

func TestSiteAddLongNameSkipsWWW(t *testing.T) {
	// Gốc 43 byte: www.<gốc> 47 byte, quá trần; gốc 42 byte: www 46 byte, vừa.
	for _, c := range []struct {
		n       int
		wantWWW bool
	}{{site.MaxNameLen - 3, false}, {site.MaxNameLen - 4, true}} {
		se := soatNamesEnv(t)
		dom := nameOfLen(c.n, ".example")
		res := se.addSite(t, dom, "", "")
		www := "www." + dom
		_, has := rowsOf(t, se, dom)[www]
		if has != c.wantWWW || strings.Contains(vhostOf(t, se, dom), www) != c.wantWWW {
			t.Errorf("gốc %d byte: có www = %v (vhost %v); muốn %v", c.n, has, strings.Contains(vhostOf(t, se, dom), www), c.wantWWW)
		}
		if !c.wantWWW && !strings.Contains(notesText(res.Notes), www) {
			t.Errorf("gốc %d byte: thiếu ghi chú bỏ www: %v", c.n, res.Notes)
		}
		if c.wantWWW && len(res.Notes) != 0 {
			t.Errorf("gốc %d byte: ghi chú thừa %v", c.n, res.Notes)
		}
	}
}

func TestSiteAddWWWDeclaredElsewhere(t *testing.T) {
	const stray = "/etc/nginx/sites-enabled/www-company.conf"
	for _, c := range []struct {
		label, decl string
		notAdmin    bool
	}{
		{"admin, tên trùng hệt", "www.company.example", false},
		{"user, tên trùng hệt", "www.company.example", true},
		{"user, wildcard", "*.company.example", true},
	} {
		t.Run(c.label, func(t *testing.T) {
			se := soatNamesEnv(t)
			se.m.SetFile(stray, []byte("server {\n    listen 80;\n    server_name "+c.decl+";\n    location / { proxy_pass http://127.0.0.1:8080; }\n}\n"), 0o644, system.Root)
			se.s.NotAdmin = c.notAdmin
			res := se.addSite(t, "company.example", "", "")
			if _, has := rowsOf(t, se, "company.example")["www.company.example"]; has {
				t.Error("www.company.example vẫn được giữ cho site")
			}
			if strings.Contains(vhostOf(t, se, "company.example"), "www.company.example") {
				t.Error("vhost của tpan khai www.company.example")
			}
			if len(res.Notes) != 1 || !strings.Contains(res.Notes[0].Vi, "www.company.example") {
				t.Fatalf("ghi chú = %+v", res.Notes)
			}
			// User thường không được biết đường dẫn vhost của người khác.
			if leaked := strings.Contains(res.Notes[0].Vi+res.Notes[0].En, "www-company.conf"); leaked == c.notAdmin {
				t.Errorf("ghi chú (NotAdmin=%v) có tên file = %v: %+v", c.notAdmin, leaked, res.Notes[0])
			}
		})
	}
	t.Run("không ai khai: www vẫn có", func(t *testing.T) {
		se := soatNamesEnv(t)
		se.addSite(t, "company.example", "", "")
		if _, has := rowsOf(t, se, "company.example")["www.company.example"]; !has {
			t.Error("thiếu www.company.example")
		}
	})
}

// TestAliasAddDeclaredWildcard: alias.add cũng từ chối tên khớp wildcard của
// vhost khác (DeclaredFile dùng chung với site add).
func TestAliasAddDeclaredWildcard(t *testing.T) {
	se := soatNamesEnv(t)
	se.addSite(t, "example.com", "", WWWNone)
	se.m.SetFile("/etc/nginx/sites-enabled/wild.conf", []byte("server {\n    listen 80;\n    server_name *.example.com;\n}\n"), 0o644, system.Root)
	ctx := context.Background()
	d, name := mustDomain(t, "example.com"), mustDomain(t, "x.example.com")
	if _, err := se.s.PrepareAliasAdd(ctx, d, name, site.ModeServe); nameCode(err) != site.CodeNameTaken {
		t.Errorf("admin: %v", err)
	}
	se.s.NotAdmin = true
	if _, err := se.s.PrepareAliasAdd(ctx, d, name, site.ModeServe); nameCode(err) != CodeNameUnavailable {
		t.Errorf("user: %v", err)
	}
}
