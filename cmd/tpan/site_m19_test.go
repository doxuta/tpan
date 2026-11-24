package main

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

// TestAliasArgsRejectedBeforeAnything: domain, tên, --mode sai bị từ chối trước
// khi chạm máy hay tạo DB, và lời từ chối không in lại chuỗi lạ.
func TestAliasArgsRejectedBeforeAnything(t *testing.T) {
	for args, want := range map[string]string{
		"site alias add a.com;id www.example.com":                        "domain_invalid",
		"site alias add example.com $(id)":                               "domain_invalid",
		"site alias add example.com ../../etc/passwd":                    "domain_invalid",
		"site alias add example.com www.example.com --mode all;id":       "name_mode_invalid",
		"site alias set example.com www.example.com":                     "name_mode_invalid",
		"site alias rm example.com " + strings.Repeat("a", 300) + ".com": "domain_invalid",
		"site add example.com --email a@example.com --www both":          "name_mode_invalid",
		"site alias list a..b":                                           "domain_invalid",
	} {
		c := newCLI(t)
		code, out, _ := c.run(append(strings.Fields(args), "--json")...)
		if code != 1 || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: mã %d, lệnh %v, DB %v", args, code, c.fake.Ran(), c.dbExists())
		}
		if got := refusalCode(t, out); got != want {
			t.Errorf("%q: mã từ chối %s; muốn %s", args, got, want)
		}
		if strings.Contains(out, "$(id)") || strings.Contains(out, ";id") {
			t.Errorf("%q: lời từ chối in lại chuỗi lạ: %s", args, out)
		}
	}
}

// TestAliasNeedsRoot: thao tác thật và list cần root; --dry-run thì không.
func TestAliasNeedsRoot(t *testing.T) {
	for _, args := range []string{
		"site alias add example.com www.example.com --yes",
		"site alias set example.com www.example.com --mode serve --yes",
		"site alias rm example.com www.example.com --yes",
		"site alias list example.com",
	} {
		c := newCLI(t)
		c.root = false
		code, out, _ := c.run(append(strings.Fields(args), "--json")...)
		if code != 1 || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q không root: mã %d, lệnh %v", args, code, c.fake.Ran())
		}
		if got := refusalCode(t, out); got != "root_required" {
			t.Errorf("%q: %s", args, got)
		}
	}
}

// TestRedirectArgsRejectedBeforeAnything: domain, nguồn, đích, --code, id sai bị
// từ chối trước khi chạm máy hay tạo DB, và lời từ chối không in lại chuỗi lạ.
func TestRedirectArgsRejectedBeforeAnything(t *testing.T) {
	for args, want := range map[string]string{
		"site redirect add a.com;id /old /new":                                 "domain_invalid",
		"site redirect add example.com / /new":                                 "redirect_reserved",
		"site redirect add example.com /index.php /new":                        "redirect_reserved",
		"site redirect add example.com /a;id /new":                             "redirect_invalid",
		"site redirect add example.com /$(id) /new":                            "redirect_invalid",
		"site redirect add example.com ../../etc/passwd /new":                  "redirect_invalid",
		"site redirect add example.com /.well-known/x /new":                    "redirect_invalid",
		"site redirect add example.com /old javascript:alert(1)":               "redirect_invalid",
		"site redirect add example.com /old /new;id":                           "redirect_invalid",
		"site redirect add example.com /old /new --code 303":                   "redirect_invalid",
		"site redirect add example.com /" + strings.Repeat("a", 300) + " /new": "redirect_invalid",
		"site redirect rm example.com abc":                                     "redirect_not_found",
		"site redirect rm example.com 0":                                       "redirect_not_found",
		"site redirect rm example.com $(id)":                                   "redirect_not_found",
		"site redirect list a..b":                                              "domain_invalid",
	} {
		c := newCLI(t)
		code, out, _ := c.run(append(strings.Fields(args), "--json")...)
		if code != 1 || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: mã %d, lệnh %v, DB %v", args, code, c.fake.Ran(), c.dbExists())
		}
		if got := refusalCode(t, out); got != want {
			t.Errorf("%q: mã từ chối %s; muốn %s", args, got, want)
		}
		if strings.Contains(out, "$(id)") || strings.Contains(out, ";id") || strings.Contains(out, "alert(1)") {
			t.Errorf("%q: lời từ chối in lại chuỗi lạ: %s", args, out)
		}
	}
}

// TestRedirectNeedsRoot: thao tác thật và list cần root; --dry-run thì không.
func TestRedirectNeedsRoot(t *testing.T) {
	for _, args := range []string{
		"site redirect add example.com /old /new --yes",
		"site redirect rm example.com 1 --yes",
		"site redirect list example.com",
	} {
		c := newCLI(t)
		c.root = false
		code, out, _ := c.run(append(strings.Fields(args), "--json")...)
		if code != 1 || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q không root: mã %d, lệnh %v", args, code, c.fake.Ran())
		}
		if got := refusalCode(t, out); got != "root_required" {
			t.Errorf("%q: %s", args, got)
		}
	}
}

// TestExportWarnsNames (S21): file export chưa mang tên phụ, nên khi có site
// mang tên phụ thì cảnh báo ra stderr (stdout vẫn là file đọc lại được); không
// có thì im. Chú thích đầu file nói rõ cả hai trường hợp.
func TestExportWarnsNames(t *testing.T) {
	c := newCLI(t)
	c.seedSites(activeSite(t, "a.example.com", false), activeSite(t, "b.example.com", false))

	code, out, errs := c.run("export")
	if code != 0 || strings.Contains(errs, "alias add") || strings.Contains(errs, "redirect add") {
		t.Fatalf("chưa có tên phụ mà cảnh báo: mã %d, %q", code, errs)
	}
	if !strings.Contains(out, "tpan site alias add") || !strings.Contains(out, "tpan site redirect add") {
		t.Errorf("chú thích đầu file chưa nói tên phụ và luật chuyển hướng:\n%s", out)
	}

	// Thêm một tên phụ active cho b.example.com.
	c.withStore(func(ctx context.Context, st *store.Store) {
		name, siteD := mustDomain(t, "www.b.example.com"), mustDomain(t, "b.example.com")
		cl := []site.Claim{{Name: name, Site: siteD, Owner: account.AdminName, Kind: site.KindAlias, Mode: site.ModeRedirect, ByAdmin: true}}
		if err := st.ClaimNames(ctx, cl); err != nil {
			t.Fatal(err)
		}
		if err := st.ActivateNames(ctx, siteD, []validate.Domain{name}); err != nil {
			t.Fatal(err)
		}
	})
	for _, args := range [][]string{{"export"}, {"export", "--json"}} {
		code, out, errs = c.run(args...)
		if code != 0 || !strings.Contains(errs, "b.example.com") || strings.Contains(errs, "a.example.com") ||
			!strings.Contains(errs, "tpan site alias add") || strings.Contains(errs, "redirect add") {
			t.Errorf("%v: mã %d, stderr %q", args, code, errs)
		}
		if strings.Contains(out, "cảnh báo") || strings.Contains(out, "www.b.example.com") {
			t.Errorf("%v: cảnh báo hay tên phụ lọt vào stdout:\n%s", args, out)
		}
	}
	// stdout của dạng YAML vẫn đưa lại vào apply được.
	_, out, _ = c.run("export")
	if sf, err := core.ParseSitesFile(strings.NewReader(out)); err != nil || len(sf.Sites) != 2 {
		t.Errorf("đọc lại file export: %+v, %v", sf, err)
	}
}
