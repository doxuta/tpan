package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

func TestStackSwitchFlags(t *testing.T) {
	c, _, err := newRoot(&env{}).Find([]string{"stack", "switch"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"to", "dry-run", "yes", "json", "verbose"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("stack switch thiếu cờ --%s", name)
		}
	}
}

// Web server sai bị từ chối trước khi có gì chạm tới máy; thiếu --to là lỗi cờ.
func TestStackSwitchBadInput(t *testing.T) {
	for _, bad := range []string{"caddy", "Apache", "nginx; rm -rf /", "$(id)", "../../etc/passwd", "nginx\napache", ""} {
		c := newCLI(t)
		code, out, _ := c.run("stack", "switch", "--to", bad, "--dry-run", "--json")
		if code != 1 || refusalCode(t, out) != "web_invalid" {
			t.Errorf("%q: mã %d, %s", bad, code, out)
		}
		if len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: chạy lệnh %v hoặc tạo DB", bad, c.fake.Ran())
		}
	}
	c := newCLI(t)
	if code, _, errs := c.run("stack", "switch", "--dry-run"); code != 1 || !strings.Contains(errs, "to") {
		t.Errorf("thiếu --to: mã %d, %q", code, errs)
	}
	c = newCLI(t)
	c.root = false
	if code, out, _ := c.run("stack", "switch", "--to", "apache", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "root_required" {
		t.Errorf("không root: mã %d, %s", code, out)
	}
}

func TestStackSwitchRefusals(t *testing.T) {
	// Chưa có DB: --dry-run đọc như bảng rỗng, không tạo DB.
	c := newCLI(t)
	code, out, _ := c.run("stack", "switch", "--to", "apache", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "stack_not_installed" || c.dbExists() {
		t.Errorf("chưa có DB: mã %d, %s", code, out)
	}

	c = newCLI(t)
	c.seed(stackNginx, "shop.example.com")
	if code, out, _ := c.run("stack", "switch", "--to", "nginx", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "stack_same_web" {
		t.Errorf("cùng web server: mã %d, %s", code, out)
	}

	// Site đang hỏng trên web server hiện tại: từ chối, kèm kết quả kiểm.
	c.fake.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "<html>lỗi</html>TPAN_HTTP_CODE=500"}
	})
	code, out, _ = c.run("stack", "switch", "--to", "apache", "--yes", "--json")
	var got struct {
		Error core.RefusedError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != "precondition_failed" || len(got.Error.Checks) != 1 || got.Error.Checks[0].ID != "http.site:shop.example.com" {
		t.Errorf("site hỏng: mã %d, %s", code, out)
	}
	if c.audits("stack.switch") != 0 {
		t.Error("từ chối mà có audit job")
	}
}

func TestStackSwitchDryRun(t *testing.T) {
	c := newCLI(t)
	c.seed(stackNginx, "shop.example.com")
	code, out, errs := c.run("stack", "switch", "--to", "apache", "--dry-run")
	if code != 0 || !strings.Contains(out, "Chuyển LEMP (Nginx) sang LAMP (Apache)") || !strings.Contains(out, "systemctl stop nginx") || !strings.Contains(out, "policy-rc.d") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	for _, m := range c.mutating() {
		if !strings.HasPrefix(m, "curl ") { // điều kiện đầu gọi HTTP vào từng site
			t.Errorf("--dry-run chạy lệnh thay đổi: %s", m)
		}
	}
	code, out, _ = c.run("stack", "switch", "--to", "apache", "--dry-run", "--json")
	var pv engine.Preview
	if err := json.Unmarshal([]byte(out), &pv); err != nil || code != 0 || pv.Kind != "stack.switch" || pv.Target != "apache" {
		t.Errorf("--json: mã %d, %v, %+v", code, err, pv)
	}
	if c.audits("stack.switch") != 0 {
		t.Error("--dry-run ghi audit")
	}

	// Không --yes, không terminal: không hỏi được thì từ chối, không chạy gì.
	c.fake.ResetRan()
	code, out, _ = c.run("stack", "switch", "--to", "apache", "--json")
	if code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}
	for _, m := range c.mutating() {
		if !strings.HasPrefix(m, "curl ") {
			t.Errorf("không --yes mà chạy %s", m)
		}
	}
}

// Chạy thật trên máy giả: gói không cài được (dpkg-query luôn "chưa cài"), nên
// job hoàn tác; vẫn có audit stack.switch và health chạy lại sau job.
func TestStackSwitchRunsJob(t *testing.T) {
	c := newCLI(t)
	c.seed(stackNginx, "shop.example.com")
	code, out, errs := c.run("stack", "switch", "--to", "apache", "--yes")
	if code != 1 || !strings.Contains(out, "Kiểm lại toàn bộ health") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if c.audits("stack.switch") != 1 {
		t.Error("không ghi audit stack.switch")
	}
	if v, _ := c.settingOK(core.SettingWeb); v != "nginx" {
		t.Errorf("job hoàn tác mà stack.web = %q", v)
	}
}
