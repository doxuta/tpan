package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Test của M19 (mục 3.8 của docs/thay-doi/M19-thiet-ke.md): hai công cụ đọc
// list_site_names và list_site_redirects.

const (
	aliasServe    = "www.shop.example.com"
	aliasRedirect = "cu.shop.example.com"
	aliasPending  = "moi.shop.example.com"
)

// seedM19 thêm vào site domA: hai tên phụ active (một serve, một redirect), một
// tên pending, và hai luật chuyển hướng active (một luật pending không được lộ).
func seedM19(t *testing.T, e *env) {
	t.Helper()
	d := mustDomain(t, domA)
	claim := func(name string, m site.NameMode) site.Claim {
		return site.Claim{Name: mustDomain(t, name), Site: d, Kind: site.KindAlias, Mode: m, ByAdmin: true}
	}
	if err := e.st.ClaimNames(ctx, []site.Claim{claim(aliasServe, site.ModeServe), claim(aliasRedirect, site.ModeRedirect), claim(aliasPending, site.ModeServe)}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ActivateNames(ctx, d, []validate.Domain{mustDomain(t, aliasServe), mustDomain(t, aliasRedirect)}); err != nil {
		t.Fatal(err)
	}
	rule := func(src, dst string, code int, keep bool) int64 {
		s, err := validate.ParseRedirectSource(src)
		if err != nil {
			t.Fatal(err)
		}
		g, err := validate.ParseRedirectTarget(dst)
		if err != nil {
			t.Fatal(err)
		}
		id, err := e.st.AddRedirectPending(ctx, d, site.Redirect{Source: s, Target: g, Code: code, KeepQuery: keep})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, id := range []int64{
		rule("/old-page", "/new-page", 301, true),
		rule("/blog", "https://"+domU+"/tin", 302, false),
	} {
		if err := e.st.ActivateRedirect(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	rule("/dang-cho", "/x", 301, false) // pending: chưa vào vhost, không được lộ
}

func TestM19ToolsInCatalog(t *testing.T) {
	for _, name := range []string{"list_site_names", "list_site_redirects"} {
		tool, ok := lookup(name)
		if !ok {
			t.Fatalf("danh mục thiếu %s", name)
		}
		if tool.Kind != KindRead || tool.Untrusted {
			t.Errorf("%s: loại %q, untrusted %v; muốn read, không untrusted", name, tool.Kind, tool.Untrusted)
		}
		var sc struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(tool.InputSchema, &sc); err != nil || len(sc.Required) != 1 || sc.Required[0] != "domain" {
			t.Errorf("%s: schema %s (%v)", name, tool.InputSchema, err)
		}
	}
}

func TestListSiteNames(t *testing.T) {
	e := newEnv(t, "")
	seedM19(t, e)
	e.f.Handle([]string{"getent", "ahostsv4"}, func(c system.Cmd) system.Result {
		// Chỉ tên serve trỏ về máy; tên khác không có bản ghi A.
		if c.Args[len(c.Args)-1] == aliasServe {
			return system.Result{Stdout: "203.0.113.7 STREAM " + aliasServe + "\n"}
		}
		return system.Result{ExitCode: 2}
	})
	e.f.Handle([]string{"ip"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "2: eth0    inet 203.0.113.7/24 scope global eth0\n"}
	})
	s := e.open(t, "")
	out := callOK(t, s, "list_site_names", `{"domain":"`+alias(s, domA)+`"}`)

	var rows []struct {
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Mode   string `json:"mode"`
		State  string `json:"state"`
		DNS    string `json:"dns"`
		InCert bool   `json:"in_cert"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("giải %s: %v", out, err)
	}
	// Tên chính, hai tên phụ active, một tên pending: đủ bốn dòng.
	if len(rows) != 4 {
		t.Fatalf("muốn 4 dòng, có %d: %s", len(rows), out)
	}
	byName := map[string]int{}
	for i, r := range rows {
		byName[s.st.red.Unalias(r.Name)] = i
	}
	pick := func(real string) int {
		i, ok := byName[real]
		if !ok {
			t.Fatalf("thiếu tên %s trong %s", real, out)
		}
		return i
	}
	if r := rows[pick(domA)]; r.Kind != "primary" || r.State != "active" {
		t.Errorf("tên chính: %+v", r)
	}
	if r := rows[pick(aliasServe)]; r.Kind != "alias" || r.Mode != "serve" || r.State != "active" || r.DNS != "ok" {
		t.Errorf("tên serve: %+v", r)
	}
	if r := rows[pick(aliasRedirect)]; r.Mode != "redirect" || r.DNS != "missing" {
		t.Errorf("tên redirect: %+v", r)
	}
	if r := rows[pick(aliasPending)]; r.State != "pending" {
		t.Errorf("tên pending: %+v", r)
	}
	// Tên phụ cũng được che như domain của site: chuỗi thật không ra tới agent.
	for _, real := range []string{aliasServe, aliasRedirect, aliasPending, domA} {
		if strings.Contains(string(out), real) {
			t.Errorf("lộ tên thật %s: %s", real, out)
		}
	}
}

func TestListSiteRedirects(t *testing.T) {
	e := newEnv(t, "")
	seedM19(t, e)
	s := e.open(t, "")
	out := callOK(t, s, "list_site_redirects", `{"domain":"`+alias(s, domA)+`"}`)

	var rows []struct {
		ID        int64  `json:"id"`
		Source    string `json:"source"`
		Target    string `json:"target"`
		Code      int    `json:"code"`
		KeepQuery bool   `json:"keep_query"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("giải %s: %v", out, err)
	}
	if len(rows) != 2 {
		t.Fatalf("muốn 2 luật active, có %d: %s", len(rows), out)
	}
	if r := rows[0]; r.ID == 0 || r.Source != "/old-page" || r.Target != "/new-page" || r.Code != 301 || !r.KeepQuery {
		t.Errorf("luật 1: %+v", r)
	}
	if r := rows[1]; r.Source != "/blog" || r.Code != 302 || r.KeepQuery || strings.Contains(r.Target, domU) || !strings.HasPrefix(r.Target, "https://") {
		t.Errorf("luật 2 (đích là site khác, phải được che): %+v", r)
	}
	if strings.Contains(string(out), "dang-cho") {
		t.Errorf("luật pending lộ ra: %s", out)
	}

	// Site chưa có luật: mảng rỗng, không phải null.
	u := alias(s, domU)
	if out := callOK(t, s, "list_site_redirects", `{"domain":"`+u+`"}`); string(out) != "[]" {
		t.Errorf("site không có luật: muốn [], có %s", out)
	}
}

// Cùng khuôn site_health: site không có thì lỗi, domain xấu bị chặn, đối số lạ
// bị từ chối, và công cụ không chạy lệnh đổi máy. MCP chạy bằng quyền root
// trên máy của chủ, nên không có "user thường": phạm vi là mọi site trong
// store, đúng như site_health.
func TestM19ToolsRefusals(t *testing.T) {
	e := newEnv(t, "")
	s := e.open(t, "")
	for _, name := range []string{"list_site_names", "list_site_redirects"} {
		if err := callErr(t, s, name, `{"domain":"nothere.example.org"}`); err.Code != "site_not_found" {
			t.Errorf("%s site không có: %v", name, err)
		}
		if err := callErr(t, s, name, `{"domain":"`+alias(s, domA)+`","x":1}`); err.Code != "invalid_arguments" {
			t.Errorf("%s đối số lạ: %v", name, err)
		}
		if err := callErr(t, s, name, `{}`); err.Code != validate.CodeDomainInvalid {
			t.Errorf("%s thiếu domain: %v", name, err)
		}
		for _, d := range []string{"a.com; rm -rf /", "$(id).example.com", "../../etc/passwd", "a.com\nb.com", strings.Repeat("a", 300) + ".com"} {
			dj, _ := json.Marshal(d)
			if err := callErr(t, s, name, `{"domain":`+string(dj)+`}`); err.Code != validate.CodeDomainInvalid {
				t.Errorf("%s %q: %v", name, d, err)
			}
		}
	}
	for _, c := range e.f.Ran() {
		if !readOnlyCmd(c) {
			t.Errorf("lệnh có thể đổi máy đã chạy: %s", c.String())
		}
	}
}
