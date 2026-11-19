package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Domain có chuỗi tấn công bị từ chối ở CLI trước khi có gì chạm tới máy:
// không lệnh nào chạy, không file DB nào được tạo.
func TestSiteDomainRejectedBeforeAnything(t *testing.T) {
	for _, bad := range []string{
		"a.com; rm -rf /",
		"$(id).com",
		"../../etc/passwd",
		"a.com\nb.com",
		strings.Repeat("a", 300) + ".com",
	} {
		for _, args := range [][]string{
			{"site", "add", bad, "--email", "admin@example.com"},
			{"site", "add", bad, "--email", "admin@example.com", "--dry-run"},
			{"site", "rm", bad, "--yes"},
			{"site", "info", bad},
			{"health", bad},
		} {
			c := newCLI(t)
			code, out, errs := c.run(args...)
			if code != 1 || !strings.Contains(errs, "Từ chối:") {
				t.Errorf("%q: mã %d, stderr %q", args, code, errs)
			}
			if out != "" {
				t.Errorf("%q: stdout %q", args, out)
			}
			if ran := c.fake.Ran(); len(ran) != 0 {
				t.Errorf("%q: chạy lệnh %v", args, ran)
			}
			if c.dbExists() {
				t.Errorf("%q: tạo file DB", args)
			}
		}
	}

	// --json: lỗi có mã ổn định trên stdout.
	c := newCLI(t)
	code, out, _ := c.run("site", "add", "a.com; rm -rf /", "--email", "admin@example.com", "--json")
	var got struct {
		Error core.RefusedError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != validate.CodeDomainInvalid {
		t.Errorf("--json: mã %d, %+v (%v)\n%s", code, got, err, out)
	}
}

func TestSiteAddEmail(t *testing.T) {
	c := newCLI(t)
	// Thiếu --email không còn bị cobra chặn (chạy lại site đã có thì bỏ trống
	// được); site mới thì core từ chối, và không lệnh nào chạy.
	if code, _, errs := c.run("site", "add", "shop.example.com"); code != 1 || !strings.Contains(errs, "Từ chối:") || len(c.fake.Ran()) != 0 {
		t.Errorf("thiếu --email: mã %d, %q, lệnh %v", code, errs, c.fake.Ran())
	}
	code, _, errs := c.run("site", "add", "shop.example.com", "--email", "a@b c")
	if code != 1 || !strings.Contains(errs, "Từ chối:") || len(c.fake.Ran()) != 0 {
		t.Errorf("email sai: mã %d, %q, lệnh %v", code, errs, c.fake.Ran())
	}
}

func TestSiteAddDryRunNoDB(t *testing.T) {
	// Chưa có DB: stack chưa cài, không tạo file DB, không cần root.
	c := newCLI(t)
	c.root = false
	code, _, errs := c.run("site", "add", "shop.example.com", "--email", "admin@example.com", "--dry-run")
	if code != 1 || !strings.Contains(errs, "stack chưa được cài") {
		t.Errorf("mã %d, stderr %q", code, errs)
	}
	if c.dbExists() || len(c.fake.Ran()) != 0 {
		t.Errorf("--dry-run tạo DB hoặc chạy lệnh: %v", c.fake.Ran())
	}
}

func TestSiteNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"site", "add", "shop.example.com", "--email", "admin@example.com", "--yes"},
		{"site", "rm", "shop.example.com", "--yes"},
		{"site", "list"},
		{"site", "info", "shop.example.com"},
		{"health", "shop.example.com"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") {
			t.Errorf("%q: mã %d, stderr %q", args, code, errs)
		}
		if c.dbExists() {
			t.Errorf("%q: không phải root mà tạo DB", args)
		}
	}
}

func TestSiteListEmptyAndNotFound(t *testing.T) {
	c := newCLI(t)
	code, out, _ := c.run("site", "list")
	if code != 0 || !strings.Contains(out, "Chưa có site nào") {
		t.Errorf("list: mã %d, %q", code, out)
	}
	code, out, _ = c.run("site", "list", "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("list --json: mã %d, %q", code, out)
	}

	for _, args := range [][]string{
		{"site", "info", "shop.example.com"},
		{"site", "rm", "shop.example.com", "--yes"},
		{"site", "rm", "shop.example.com", "--dry-run"},
		{"health", "shop.example.com"},
	} {
		code, out, _ := c.run(append(args, "--json")...)
		var got struct {
			Error core.RefusedError `json:"error"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != "site_not_found" {
			t.Errorf("%q: mã %d, %+v (%v)", args, code, got, err)
		}
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("chạy lệnh thay đổi máy: %v", m)
	}
}

func TestSiteAddNeedsConfirmWithoutTerminal(t *testing.T) {
	c := newCLI(t)
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
		if err := st.SetSetting(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()
	// getent: user chưa có; mariadb: chưa có DB.
	c.fake.Handle([]string{"getent"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	c.fake.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })

	code, _, errs := c.run("site", "add", "shop.example.com", "--email", "admin@example.com")
	if code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("mã %d, stderr %q", code, errs)
	}
	// Ngoài các lệnh chỉ đọc của mutating(), xem trước còn hỏi getent và WP-CLI (chỉ đọc).
	for _, m := range c.mutating() {
		if !strings.HasPrefix(m, "getent passwd ") && !strings.Contains(m, "wp core is-installed") && !strings.Contains(m, "wp option get") {
			t.Errorf("chưa xác nhận mà chạy lệnh thay đổi máy: %s", m)
		}
	}
}

// Mật khẩu quản trị chỉ hiện khi WordPress vừa được cài, đúng một lần.
func TestSiteAdminPasswordOutput(t *testing.T) {
	d, _ := validate.ParseDomain("shop.example.com")
	const pw = "Abcdefghijklmnopqrstuvwxyz012345"
	res := core.SiteAddResult{
		Report:        engine.Report{Job: engine.Job{ID: "j1", Status: engine.JobSucceeded}},
		AdminUser:     "admin",
		AdminPassword: system.NewSecret(pw),
		WPInstalled:   true,
	}
	res.Site.Domain = d

	var out bytes.Buffer
	e := &env{stdout: &out, lang: "vi"}
	printAdminPassword(e, res)
	if strings.Count(out.String(), pw) != 1 || !strings.Contains(out.String(), "tpan không giữ") {
		t.Errorf("in mật khẩu:\n%s", out.String())
	}
	b, _ := json.Marshal(siteAddJSONOf(res))
	if strings.Count(string(b), pw) != 1 {
		t.Errorf("--json phải có mật khẩu đúng một lần: %s", b)
	}
	// Bản thân kết quả của core không bao giờ lộ mật khẩu ra JSON.
	if b, _ := json.Marshal(res); strings.Contains(string(b), pw) {
		t.Error("JSON của core.SiteAddResult lộ mật khẩu")
	}

	res.WPInstalled = false
	out.Reset()
	printAdminPassword(e, res)
	b, _ = json.Marshal(siteAddJSONOf(res))
	if out.Len() != 0 || strings.Contains(string(b), pw) {
		t.Errorf("WordPress không được cài lần này mà vẫn in mật khẩu: %q %s", out.String(), b)
	}
}
