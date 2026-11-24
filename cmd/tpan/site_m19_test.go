package main

import (
	"strings"
	"testing"
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
