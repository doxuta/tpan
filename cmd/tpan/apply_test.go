package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// seedSites ghi stack nginx/8.3 và các site vào DB của c, như máy đã cài.
func (c *cli) seedSites(sites ...site.Site) {
	c.t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
		if err := st.SetSetting(ctx, k, v); err != nil {
			c.t.Fatal(err)
		}
	}
	for _, s := range sites {
		if err := st.SaveSite(ctx, s); err != nil {
			c.t.Fatal(err)
		}
	}
}

func activeSite(t *testing.T, dom string, ssl bool) site.Site {
	t.Helper()
	d, err := validate.ParseDomain(dom)
	if err != nil {
		t.Fatal(err)
	}
	return site.Site{Domain: d, Slug: validate.SlugOf(d), Status: site.Active, PHPVersion: "8.3", SSL: ssl,
		Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

// cleanMachine cho fake trả lời như máy chưa có gì của domain: getent không
// thấy user, MariaDB không có DB hay user DB. Không có nó thì FakeSystem trả
// mã 0 cho mọi lệnh và site mới nào cũng bị coi là "đã có trên máy".
func (c *cli) cleanMachine() {
	c.fake.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	c.fake.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })
}

func TestApplyFileErrors(t *testing.T) {
	// Thiếu -f: cobra từ chối.
	c := newCLI(t)
	if code, _, _ := c.run("apply"); code == 0 {
		t.Error("thiếu -f mà mã 0")
	}

	// File không có.
	code, _, errs := c.run("apply", "-f", filepath.Join(t.TempDir(), "khong-co.yaml"), "--yes")
	if code != 1 || !strings.Contains(errs, "khong-co.yaml") {
		t.Errorf("file không có: mã %d, stderr %q", code, errs)
	}

	// File sai: từ chối trước mọi việc khác, cả khi không root.
	for _, body := range []string{
		"sites:\n  - domain: a.com; rm -rf /\n",
		"sites:\n  - domain: a.example.com\n    admin_mail: x@example.com\n",
		"",
	} {
		c := newCLI(t)
		c.root = false
		c.stdin = body
		code, out, errs := c.run("apply", "-f", "-", "--dry-run")
		if code != 1 || !strings.Contains(errs, "Từ chối:") || out != "" {
			t.Errorf("%q: mã %d, stdout %q, stderr %q", body, code, out, errs)
		}
		if len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: file sai mà vẫn chạy lệnh hoặc tạo DB", body)
		}
	}
}

func TestApplyStdinNeedsYes(t *testing.T) {
	c := newCLI(t)
	c.terminal = true
	c.stdin = "sites:\n  - domain: a.example.com\n"
	code, _, errs := c.run("apply", "-f", "-")
	if code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("mã %d, stderr %q", code, errs)
	}
	if len(c.fake.Ran()) != 0 {
		t.Error("chạy lệnh khi chưa được xác nhận")
	}
}

func TestApplyEmpty(t *testing.T) {
	c := newCLI(t)
	c.seedSites(activeSite(t, "a.example.com", false))
	file := filepath.Join(t.TempDir(), "sites.yaml")
	if err := os.WriteFile(file, []byte("sites:\n  - domain: A.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out, errs := c.run("apply", "-f", file, "--yes")
	if code != 0 || !strings.Contains(out, "Không có gì thay đổi") || !strings.Contains(out, "a.example.com") {
		t.Errorf("mã %d, stdout %q, stderr %q", code, out, errs)
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("không có gì thay đổi mà vẫn chạy lệnh: %v", m)
	}

	code, out, _ = c.run("apply", "-f", file, "--yes", "--json")
	var got struct {
		Items   []core.ApplyItem `json:"items"`
		Created []any            `json:"created"`
		Report  *json.RawMessage `json:"report"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 {
		t.Fatalf("--json: mã %d, %v\n%s", code, err, out)
	}
	if len(got.Items) != 1 || got.Items[0].Action != core.ApplySkip || got.Created == nil || got.Report != nil {
		t.Errorf("--json = %+v", got)
	}

	// Không có job nào, không ghi audit.
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if jobs, _ := st.Jobs(context.Background(), 5); len(jobs) != 0 {
		t.Errorf("Empty mà có job: %+v", jobs)
	}
}

func TestApplyDryRun(t *testing.T) {
	c := newCLI(t)
	c.cleanMachine()
	c.seedSites(activeSite(t, "a.example.com", false), activeSite(t, "old.example.com", false))
	c.stdin = "defaults:\n  admin_email: admin@example.com\nsites:\n  - domain: a.example.com\n  - domain: new.example.com\n    ssl: true\n"

	code, out, errs := c.run("apply", "-f", "-", "--prune", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, errs)
	}
	var pv core.ApplyPreview
	if err := json.Unmarshal([]byte(out), &pv); err != nil {
		t.Fatalf("stdout không phải ApplyPreview: %v\n%s", err, out)
	}
	want := map[string]core.ApplyAction{"a.example.com": core.ApplySkip, "new.example.com": core.ApplyCreate, "old.example.com": core.ApplyTrash}
	if len(pv.Items) != 3 {
		t.Fatalf("Items = %+v", pv.Items)
	}
	for _, it := range pv.Items {
		if want[it.Domain] != it.Action {
			t.Errorf("%s: %s (%s); muốn %s", it.Domain, it.Action, it.Code, want[it.Domain])
		}
	}
	if pv.Preview == nil || pv.Preview.Kind != "apply" || len(pv.Preview.Steps) == 0 {
		t.Errorf("Preview = %+v", pv.Preview)
	}
	// mutating() chưa biết các câu hỏi chỉ đọc của site (getent, wp core
	// is-installed, wp option get); lọc chúng ra ở đây.
	for _, m := range c.mutating() {
		if strings.HasPrefix(m, "getent passwd ") || strings.Contains(m, " wp core is-installed ") || strings.Contains(m, " wp option get ") {
			continue
		}
		t.Errorf("--dry-run chạy lệnh thay đổi máy: %s", m)
	}
	if c.fake.Snapshot()["/var/www/new.example.com"].Mode != 0 {
		t.Error("--dry-run tạo thư mục site")
	}

	// Bản chữ: bảng kế hoạch, bản xem trước, lời nhắc SSL chạy sau.
	_, out, _ = c.run("apply", "-f", "-", "--prune", "--dry-run")
	for _, s := range []string{"new.example.com", "tạo + SSL", "vào thùng rác", "bỏ qua (đã có)", "Kế hoạch:", "Plan xin SSL"} {
		if !strings.Contains(out, s) {
			t.Errorf("stdout thiếu %q:\n%s", s, out)
		}
	}
}

func TestApplyRefusedWholeBatch(t *testing.T) {
	c := newCLI(t)
	c.seedSites(activeSite(t, "a.example.com", false))
	c.stdin = "sites: []\n"
	code, out, _ := c.run("apply", "-f", "-", "--prune", "--yes", "--json")
	var got struct {
		Error core.RefusedError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != "prune_empty" {
		t.Errorf("prune rỗng: mã %d, %+v (%v)\n%s", code, got, err, out)
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("bị từ chối mà vẫn chạy lệnh: %v", m)
	}
}

func TestApplyRunFailureExitsNonZero(t *testing.T) {
	// Máy giả không có curl thật: bước tải WordPress (phần chung trước lô)
	// lỗi, site không chạy tới. Lệnh in bảng từng site, ghi audit "apply",
	// thoát 1.
	c := newCLI(t)
	c.cleanMachine()
	c.seedSites()
	c.stdin = "sites:\n  - domain: new.example.com\n    admin_email: admin@example.com\n"
	code, out, errs := c.run("apply", "-f", "-", "--yes")
	if code != 1 {
		t.Fatalf("mã %d; muốn 1\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	for _, s := range []string{"Tải WordPress", "== new.example.com: chưa chạy", "KẾT QUẢ", "hỏng, đã hoàn tác sạch"} {
		if !strings.Contains(out, s) {
			t.Errorf("stdout thiếu %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "Mật khẩu:") {
		t.Error("site lỗi mà in mật khẩu quản trị")
	}
	if n := c.audits("apply"); n != 1 {
		t.Errorf("audit apply = %d; muốn 1", n)
	}
}

func TestApplyJSONRevealsPasswordsOnce(t *testing.T) {
	sec := system.NewSecret("matkhau-quan-tri-thu")
	res := core.ApplyResult{
		Report: &engine.BatchReport{},
		Created: []core.ApplySiteResult{
			{Domain: "a.example.com", Status: engine.UnitSucceeded, WPInstalled: true, AdminUser: "admin", AdminPassword: sec},
			{Domain: "b.example.com", Status: engine.UnitRolledBack},
		},
	}
	j := applyJSONOf(res)
	if j.Created[0].AdminPassword != "matkhau-quan-tri-thu" || j.Created[1].AdminPassword != "" || j.Created[1].AdminUser != "" {
		t.Errorf("applyJSONOf = %+v", j.Created)
	}
	if applyOK(res) {
		t.Error("có site rolled_back mà applyOK")
	}
}

func TestExport(t *testing.T) {
	c := newCLI(t)
	trashed := activeSite(t, "z.example.com", false)
	trashed.Status = site.Trashed
	c.seedSites(activeSite(t, "b.example.com", true), activeSite(t, "a.example.com", false), trashed)

	code, out, errs := c.run("export")
	if code != 0 {
		t.Fatalf("mã %d, %s", code, errs)
	}
	if !strings.HasPrefix(out, "# ") || strings.Contains(out, "z.example.com") ||
		strings.Index(out, "a.example.com") > strings.Index(out, "b.example.com") || !strings.Contains(out, "ssl: true") {
		t.Errorf("export:\n%s", out)
	}
	// File export đưa lại vào apply được.
	sf, err := core.ParseSitesFile(strings.NewReader(out))
	if err != nil || len(sf.Sites) != 2 {
		t.Errorf("đọc lại file export: %+v, %v", sf, err)
	}

	code, out, _ = c.run("export", "--json")
	var got core.SitesFile
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || len(got.Sites) != 2 || got.Sites[0].Domain != "a.example.com" {
		t.Errorf("--json: mã %d, %+v (%v)", code, got, err)
	}

	c.root = false
	if code, _, errs := c.run("export"); code != 1 || !strings.Contains(errs, "sudo") {
		t.Errorf("không root: mã %d, %q", code, errs)
	}
}
