package core

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// TestExportOmitted (S21): Export ghi lại site mang tên phụ active hay luật
// chuyển hướng active (file export chưa mang chúng), sắp theo domain, bỏ site
// thùng rác; YAML không có trường đó.
func TestExportOmitted(t *testing.T) {
	ctx := context.Background()
	ms := newMemSites()
	alias := mustDomain(t, "www.zeta.example.com")
	src, err := validate.ParseRedirectSource("/old")
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := validate.ParseRedirectTarget("/new")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []site.Site{
		{Domain: mustDomain(t, "zeta.example.com"), Status: site.Active,
			Names: site.Names{Redirect: []validate.Domain{alias}, Rules: []site.Redirect{{ID: 1, Source: src, Target: tgt, Code: 301}}}},
		{Domain: mustDomain(t, "alpha.example.com"), Status: site.Active,
			Names: site.Names{Serve: []validate.Domain{mustDomain(t, "a.example.net")}}},
		{Domain: mustDomain(t, "plain.example.com"), Status: site.Active},
		{Domain: mustDomain(t, "gone.example.com"), Status: site.Trashed,
			Names: site.Names{Serve: []validate.Domain{mustDomain(t, "g.example.net")}}},
	} {
		if err := ms.SaveSite(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	f, err := (&Sites{Store: ms}).Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Omitted.NameSites, " "); got != "alpha.example.com zeta.example.com" {
		t.Errorf("NameSites = %q", got)
	}
	if got := strings.Join(f.Omitted.RuleSites, " "); got != "zeta.example.com" {
		t.Errorf("RuleSites = %q", got)
	}
	if !f.Omitted.Any() {
		t.Error("Any() = false")
	}
	b, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "omitted") || strings.Contains(strings.ToLower(string(b)), "namesites") {
		t.Errorf("Omitted lọt vào YAML:\n%s", b)
	}
	if !strings.Contains(string(b), "tpan site alias add") {
		t.Errorf("chú thích đầu file không nói tên phụ:\n%s", b)
	}
	if (ExportOmitted{}).Any() {
		t.Error("zero mà Any() = true")
	}
}
