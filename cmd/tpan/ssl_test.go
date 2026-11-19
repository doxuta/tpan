package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// seed ghi sẵn settings và site vào DB của lần chạy CLI.
func (c *cli) seed(settings map[string]string, domains ...string) {
	c.t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	for k, v := range settings {
		if err := st.SetSetting(ctx, k, v); err != nil {
			c.t.Fatal(err)
		}
	}
	for _, dom := range domains {
		d, _ := validate.ParseDomain(dom)
		if err := st.SaveSite(ctx, site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Created: time.Now()}); err != nil {
			c.t.Fatal(err)
		}
	}
}

// setting đọc lại một khoá trong DB.
func (c *cli) settingOK(key string) (string, bool) {
	c.t.Helper()
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	v, ok, err := st.GetSetting(context.Background(), key)
	if err != nil {
		c.t.Fatal(err)
	}
	return v, ok
}

var stackNginx = map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"}

// dnsHere: tên miền trỏ về đúng địa chỉ của máy giả.
func (c *cli) dnsHere() {
	c.fake.Handle([]string{"getent", "ahostsv4"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "203.0.113.10 STREAM x\n"}
	})
	c.fake.Handle([]string{"getent", "ahostsv6"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	c.fake.Handle([]string{"ip"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "2: eth0    inet 203.0.113.10/24 scope global eth0\n"}
	})
}

func TestSSLIssueRejectsBadInputBeforeAnything(t *testing.T) {
	for _, args := range [][]string{
		{"ssl", "issue", "a.com; rm -rf /"},
		{"ssl", "issue", "$(id).com", "--dry-run"},
		{"ssl", "issue", "../../etc/passwd", "--yes"},
		{"ssl", "issue", "shop.example.com", "--email", "a@b c"},
		{"ssl", "issue", "shop.example.com", "--email", "x@example.com\nrm", "--dry-run"},
		{"ssl", "settings", "--email", "$(id)"},
	} {
		c := newCLI(t)
		code, out, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "Từ chối:") || out != "" {
			t.Errorf("%q: mã %d, stdout %q, stderr %q", args, code, out, errs)
		}
		if len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: chạy lệnh %v hoặc tạo DB", args, c.fake.Ran())
		}
	}
}

func TestSSLIssueDryRun(t *testing.T) {
	const dom = "shop.example.com"

	// Chưa có DB: không cần root, không tạo DB, site không có.
	c := newCLI(t)
	c.root = false
	code, out, _ := c.run("ssl", "issue", dom, "--email", "admin@example.com", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "site_not_found" || c.dbExists() {
		t.Errorf("chưa có DB: mã %d, %s", code, out)
	}

	// Có site, chưa có email: từ chối acme_email_missing.
	c = newCLI(t)
	c.seed(stackNginx, dom)
	c.dnsHere()
	code, out, _ = c.run("ssl", "issue", dom, "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "acme_email_missing" {
		t.Errorf("chưa có email: mã %d, %s", code, out)
	}

	// --email cùng --dry-run: xem trước dùng email đó mà không ghi gì.
	code, out, errs := c.run("ssl", "issue", dom, "--email", "admin@example.com", "--dry-run")
	if code != 0 || !strings.Contains(out, "Kế hoạch:") || !strings.Contains(out, "certbot certonly") || !strings.Contains(out, "admin@example.com") {
		t.Errorf("--dry-run --email: mã %d\n%s\n%s", code, out, errs)
	}
	if v, ok := c.settingOK(core.SettingSSLEmail); ok {
		t.Errorf("--dry-run ghi ssl.email = %q", v)
	}
	for _, m := range c.mutating() {
		if !strings.HasPrefix(m, "getent ") && !strings.HasPrefix(m, "ip ") && !strings.Contains(m, "wp option get") {
			t.Errorf("--dry-run chạy lệnh thay đổi máy: %s", m)
		}
	}

	// DNS chưa trỏ: precondition_failed kèm phép kiểm.
	c.fake.Handle([]string{"getent", "ahostsv4"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	code, out, _ = c.run("ssl", "issue", dom, "--email", "admin@example.com", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "precondition_failed" || !strings.Contains(out, "dns.a:"+dom) {
		t.Errorf("DNS chưa trỏ: mã %d, %s", code, out)
	}
}

func TestSSLIssueWritesEmailAndNeedsConfirm(t *testing.T) {
	const dom = "shop.example.com"
	c := newCLI(t)
	c.seed(stackNginx, dom)
	c.dnsHere()

	c.root = false
	if code, _, errs := c.run("ssl", "issue", dom, "--email", "admin@example.com"); code != 1 || !strings.Contains(errs, "sudo") {
		t.Errorf("không phải root: mã %d, %q", code, errs)
	}
	if _, ok := c.settingOK(core.SettingSSLEmail); ok {
		t.Error("không phải root mà ghi ssl.email")
	}

	// Không có terminal, không --yes: email được ghi (lần đầu), Plan không chạy.
	c.root = true
	code, _, errs := c.run("ssl", "issue", dom, "--email", "admin@example.com")
	if code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("không terminal: mã %d, %q", code, errs)
	}
	if v, _ := c.settingOK(core.SettingSSLEmail); v != "admin@example.com" {
		t.Errorf("ssl.email = %q", v)
	}
	for _, cmd := range c.fake.Ran() {
		if cmd.Name == "certbot" {
			t.Errorf("chưa xác nhận mà chạy %s", cmd.String())
		}
	}
}

func TestSSLSettings(t *testing.T) {
	c := newCLI(t)
	code, out, _ := c.run("ssl", "settings")
	if code != 0 || !strings.Contains(out, "chưa có") || !strings.Contains(out, "acme-staging-v02") {
		t.Errorf("mặc định: mã %d\n%s", code, out)
	}

	code, out, _ = c.run("ssl", "settings", "--email", "ops@example.com", "--staging=false", "--json")
	var cfg core.SSLConfig
	if err := json.Unmarshal([]byte(out), &cfg); err != nil || code != 0 || cfg.Email != "ops@example.com" || cfg.Staging {
		t.Errorf("ghi: mã %d, %+v (%v)", code, cfg, err)
	}

	code, out, _ = c.run("ssl", "settings", "--server", "http://example.com", "--json")
	if code != 1 || refusalCode(t, out) != "ssl_server_invalid" {
		t.Errorf("server sai: mã %d, %s", code, out)
	}
	if code, _, _ := c.run("ssl", "settings", "--server", "https://127.0.0.1:14000/dir"); code != 0 {
		t.Errorf("server đúng: mã %d", code)
	}
	if v, _ := c.settingOK(core.SettingSSLServer); v != "https://127.0.0.1:14000/dir" {
		t.Errorf("ssl.server = %q", v)
	}
	if code, _, _ := c.run("ssl", "settings", "--server", ""); code != 0 {
		t.Errorf("xoá server: mã %d", code)
	}
	if v, _ := c.settingOK(core.SettingSSLServer); v != "" {
		t.Errorf("xoá server mà còn %q", v)
	}

	// --server ẩn khỏi help.
	_, out, _ = c.run("ssl", "settings", "--help")
	if strings.Contains(out, "--server") || !strings.Contains(out, "--staging") {
		t.Errorf("help:\n%s", out)
	}

	c.root = false
	if code, _, errs := c.run("ssl", "settings"); code != 1 || !strings.Contains(errs, "sudo") {
		t.Errorf("không phải root: mã %d, %q", code, errs)
	}
}

func TestSSLList(t *testing.T) {
	c := newCLI(t)
	if code, out, _ := c.run("ssl", "list"); code != 0 || !strings.Contains(out, "Chưa có site nào") {
		t.Errorf("rỗng: mã %d, %q", code, out)
	}
	c.seed(nil, "shop.example.com")
	code, out, _ := c.run("ssl", "list", "--json")
	var rows []core.SSLSiteRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 || len(rows) != 1 || rows[0].SSL {
		t.Errorf("--json: mã %d, %+v (%v)", code, rows, err)
	}
	if _, out, _ := c.run("ssl", "list"); !strings.Contains(out, "tpan ssl issue shop.example.com") {
		t.Errorf("chữ:\n%s", out)
	}
}

// memSettings là bảng settings trong bộ nhớ cộng nhật ký, cho test siteAddSSL.
type memSettings struct {
	fakeAudit
	m map[string]string
}

func (s *memSettings) GetSetting(_ context.Context, k string) (string, bool, error) {
	v, ok := s.m[k]
	return v, ok, nil
}

func (s *memSettings) SetSetting(_ context.Context, k, v string) error { s.m[k] = v; return nil }

func TestSiteAddSSLUsesSiteEmail(t *testing.T) {
	d, _ := validate.ParseDomain("shop.example.com")
	em, _ := validate.ParseEmail("admin@example.com")
	set := &memSettings{m: map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"}}
	sites := &core.Sites{Sys: system.NewFake(), Settings: set, Store: core.EmptySiteStore{}}
	e := &env{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, lang: "vi"}

	// ssl.email chưa có: lấy email của site add, ghi lại kèm nhật ký. Site
	// không có trong Store nên Plan SSL bị từ chối: lời từ chối trả về, không lỗi.
	rep, re, err := siteAddSSL(context.Background(), e, &planRun{yes: true}, sites, d, em)
	if err != nil || rep != nil || re == nil || re.Code != "site_not_found" {
		t.Errorf("rep=%v re=%v err=%v", rep, re, err)
	}
	if set.m[core.SettingSSLEmail] != "admin@example.com" {
		t.Errorf("ssl.email = %q", set.m[core.SettingSSLEmail])
	}
	if len(set.got) != 1 || set.got[0].Action != "ssl.settings" || set.got[0].IP != "cli" {
		t.Errorf("nhật ký = %+v", set.got)
	}

	// Đã có email khác: giữ nguyên.
	set.m[core.SettingSSLEmail] = "ops@example.com"
	_, _, _ = siteAddSSL(context.Background(), e, &planRun{yes: true}, sites, d, em)
	if set.m[core.SettingSSLEmail] != "ops@example.com" {
		t.Errorf("ghi đè ssl.email đã có: %q", set.m[core.SettingSSLEmail])
	}
}

func TestSiteAddSSLFlag(t *testing.T) {
	c := newCLI(t)
	c.seed(stackNginx)
	c.fake.Handle([]string{"getent"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	c.fake.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })
	code, out, errs := c.run("site", "add", "shop.example.com", "--email", "admin@example.com", "--ssl", "--dry-run")
	if code != 0 || !strings.Contains(out, "Có --ssl") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if _, ok := c.settingOK(core.SettingSSLEmail); ok {
		t.Error("--dry-run ghi ssl.email")
	}

	// JSON của site add có chỗ cho kết quả SSL, vắng khi không có --ssl.
	b, _ := json.Marshal(siteAddJSON{SSL: &engine.Report{}})
	if !strings.Contains(string(b), `"ssl"`) || strings.Contains(string(b), "ssl_error") {
		t.Errorf("JSON: %s", b)
	}
}
