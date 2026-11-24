package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func m19HealthSite(t *testing.T, dom string, serve, redirect []string, ssl bool) site.Site {
	t.Helper()
	st := m17Site(t, dom)
	st.SSL = ssl
	for _, n := range serve {
		st.Names.Serve = append(st.Names.Serve, mustDomain(t, n))
	}
	for _, n := range redirect {
		st.Names.Redirect = append(st.Names.Redirect, mustDomain(t, n))
	}
	if ssl {
		st.Names.Covered = []validate.Domain{st.Domain}
	}
	return st
}

// Site chưa có tên phụ: health y như trước M19 (B5). Có tên phụ: site.alias.http
// cho từng tên (serve rồi redirect), site.alias.dns từng tên và ssl.names khi
// site SSL; tất cả là phép kiểm nặng (chỉ chạy mỗi giờ).
func TestSiteChecksAliasHealth(t *testing.T) {
	ids := func(cs []checks.Check) []string { return checkIDs(t, system.NewFake(), cs) }
	heavy := func(cs []checks.Check) bool {
		for _, c := range cs {
			if !checks.IsHeavy(c) {
				return false
			}
		}
		return true
	}

	plain := m19HealthSite(t, "shop.example.com", nil, nil, true)
	if got := siteNameEdgeChecks(plain); len(got) != 0 {
		t.Errorf("site không có tên phụ không được có phép kiểm tên: %v", ids(got))
	}
	if got := siteAliasHTTPChecks(plain); len(got) != 0 {
		t.Errorf("site không có tên phụ không được có site.alias.http: %v", ids(got))
	}
	base := ids(siteChecks(Nginx, "8.3", plain, nil, nil))
	for _, id := range base {
		if strings.HasPrefix(id, "site.alias.") || strings.HasPrefix(id, "ssl.names") {
			t.Errorf("site cũ có phép kiểm lạ %q", id)
		}
	}

	st := m19HealthSite(t, "shop.example.com", []string{"www.shop.example.com"}, []string{"old.shop.example.com"}, true)
	http := siteAliasHTTPChecks(st)
	if got, want := ids(http), []string{"site.alias.http:www.shop.example.com", "site.alias.http:old.shop.example.com"}; !slices.Equal(got, want) {
		t.Errorf("site.alias.http = %v, muốn %v", got, want)
	}
	edge := siteNameEdgeChecks(st)
	if got, want := ids(edge), []string{"site.alias.dns:www.shop.example.com", "site.alias.dns:old.shop.example.com", "ssl.names:shop.example.com"}; !slices.Equal(got, want) {
		t.Errorf("phép kiểm tên ở health = %v, muốn %v", got, want)
	}
	if !heavy(http) || !heavy(edge) {
		t.Error("phép kiểm tên phụ phải là phép kiểm nặng")
	}
	// site.alias.http nằm trong siteChecks (kiểm chứng sau thao tác dùng chung).
	if all := ids(siteChecks(Nginx, "8.3", st, nil, nil)); !slices.Contains(all, "site.alias.http:www.shop.example.com") {
		t.Errorf("siteChecks thiếu site.alias.http: %v", all)
	}

	// Site chưa SSL: có DNS của tên phụ, không có ssl.names.
	noSSL := m19HealthSite(t, "shop.example.com", []string{"www.shop.example.com"}, nil, false)
	if got, want := ids(siteNameEdgeChecks(noSSL)), []string{"site.alias.dns:www.shop.example.com"}; !slices.Equal(got, want) {
		t.Errorf("site chưa SSL: %v, muốn %v", got, want)
	}
	// Chứng chỉ còn tên đã gỡ (Covered > 1) mà site hết tên phụ: vẫn có ssl.names.
	stale := m19HealthSite(t, "shop.example.com", nil, nil, true)
	stale.Names.Covered = append(stale.Names.Covered, mustDomain(t, "old.shop.example.com"))
	if got, want := ids(siteNameEdgeChecks(stale)), []string{"ssl.names:shop.example.com"}; !slices.Equal(got, want) {
		t.Errorf("chứng chỉ thừa tên: %v, muốn %v", got, want)
	}
}

// Stack.Checks (health): web.server_name.unique mang cả tên phụ, và
// names.root.shared cảnh báo khi hai chủ chung tên miền gốc.
func TestStackChecksNamesM19(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	se.add(t, fixDom)
	st := se.sites.sites[fixDom]
	st.Names.Serve = []validate.Domain{mustDomain(t, "www."+fixDom)}
	se.sites.sites[fixDom] = st

	s := &Stack{Sys: se.m, Settings: se.st, Sites: se.sites}
	run := func() []checks.Result {
		t.Helper()
		cs, _, err := s.Checks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return checks.RunAll(ctx, system.NewFake(), cs)
	}
	find := func(rs []checks.Result, id string) checks.Result {
		t.Helper()
		for _, r := range rs {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("không có %s", id)
		return checks.Result{}
	}
	if r := find(run(), "names.root.shared"); r.Status != checks.Pass {
		t.Errorf("một chủ: names.root.shared = %s (%s)", r.Status, r.Summary.Vi)
	}

	// Một site của chủ khác dưới cùng tên miền gốc (dữ liệu từ trước M19).
	// Site đầu là của admin, mà admin được miễn (S1): đổi nó sang một user
	// thường để có đúng hai chủ thường.
	first := se.sites.sites[fixDom]
	first.Owner = "alice"
	se.sites.sites[fixDom] = first
	if r := find(run(), "names.root.shared"); r.Status != checks.Pass {
		t.Errorf("một chủ thường: names.root.shared = %s (%s)", r.Status, r.Summary.Vi)
	}
	other := m17Site(t, "blog."+fixDom)
	other.Owner = "bob"
	se.sites.sites[other.Domain.String()] = other
	r := find(run(), "names.root.shared")
	if r.Status != checks.Warn {
		t.Errorf("hai chủ chung tên miền gốc: names.root.shared = %s", r.Status)
	}
	if AutoFixName(r) != "" {
		t.Errorf("names.root.shared không có sửa tự động, có %q", AutoFixName(r))
	}
}
