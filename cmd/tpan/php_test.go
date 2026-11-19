package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

const osReleaseNoble = "ID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"

func TestPHPFlags(t *testing.T) {
	root := newRoot(&env{})
	for _, path := range [][]string{{"php", "install"}, {"php", "use"}, {"php", "remove"}} {
		c, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("%v: %v", path, err)
		}
		for _, name := range []string{"dry-run", "yes", "json", "verbose"} {
			if c.Flags().Lookup(name) == nil {
				t.Errorf("%v thiếu cờ --%s", path, name)
			}
		}
	}
	if c, _, _ := root.Find([]string{"php", "list"}); c.Flags().Lookup("json") == nil {
		t.Error("php list thiếu --json")
	}
	c, _, _ := root.Find([]string{"stack", "install"})
	for _, name := range []string{"web-source", "db-source", "db-line"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("stack install thiếu cờ --%s", name)
		}
	}
}

// Đầu vào sai bị từ chối trước khi có gì chạm tới máy: không lệnh nào chạy,
// --dry-run không tạo file DB.
func TestPHPRejectsBadInputBeforeAnything(t *testing.T) {
	for _, bad := range []string{"8.3; rm -rf /", "$(id)", "../../etc/passwd", "8.3\n8.4", "8"} {
		for _, args := range [][]string{
			{"php", "install", bad, "--dry-run"},
			{"php", "remove", bad, "--dry-run"},
			{"php", "use", "shop.example.com", bad, "--dry-run"},
			{"php", "use", bad, "8.4", "--dry-run"},
		} {
			c := newCLI(t)
			code, out, errs := c.run(args...)
			if code != 1 || !strings.Contains(errs, "Từ chối:") || out != "" {
				t.Errorf("%q: mã %d, stdout %q, stderr %q", args, code, out, errs)
			}
			if ran := c.fake.Ran(); len(ran) != 0 {
				t.Errorf("%q: chạy lệnh %v", args, ran)
			}
			if c.dbExists() {
				t.Errorf("%q: tạo file DB", args)
			}
		}
	}

	// --json: mã ổn định trên stdout.
	c := newCLI(t)
	code, out, _ := c.run("php", "install", "8.3; id", "--dry-run", "--json")
	var got struct {
		Error core.RefusedError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != "php_version_invalid" {
		t.Errorf("--json: mã %d, %+v (%v)\n%s", code, got, err, out)
	}
}

func TestPHPNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"php", "list"},
		{"php", "install", "8.4", "--yes"},
		{"php", "use", "shop.example.com", "8.4", "--yes"},
		{"php", "remove", "8.4", "--yes"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") {
			t.Errorf("%q: mã %d, stderr %q", args, code, errs)
		}
		if len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: không root mà vẫn chạy lệnh hoặc tạo DB", args)
		}
	}
}

func TestPHPRefusals(t *testing.T) {
	c := newCLI(t)
	if code, _, errs := c.run("php", "install", "8.4", "--dry-run"); code != 1 || !strings.Contains(errs, "stack chưa được cài") {
		t.Errorf("chưa cài stack: mã %d, %q", code, errs)
	}
	c.seed(stackNginx, "shop.example.com")
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"php", "remove", "8.3", "--yes", "--json"}, "php_default"},
		{[]string{"php", "use", "none.example.com", "8.4", "--yes", "--json"}, "site_not_found"},
		{[]string{"php", "use", "shop.example.com", "8.4", "--yes", "--json"}, "php_not_installed"},
	} {
		code, out, _ := c.run(tc.args...)
		var got struct {
			Error core.RefusedError `json:"error"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != tc.code {
			t.Errorf("%q: mã %d, %+v; muốn %s\n%s", tc.args, code, got.Error, tc.code, out)
		}
	}
}

// Site đã ở bản đó: không phải lỗi, in "không đổi gì", thoát 0.
func TestPHPUseSameVersion(t *testing.T) {
	c := newCLI(t)
	c.seed(stackNginx, "shop.example.com")
	code, out, errs := c.run("php", "use", "shop.example.com", "8.3", "--yes")
	if code != 0 || !strings.Contains(out, "không đổi gì") || errs != "" {
		t.Errorf("mã %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, _ = c.run("php", "use", "shop.example.com", "8.3", "--yes", "--json")
	var got struct {
		Changed *bool `json:"changed"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || got.Changed == nil || *got.Changed {
		t.Errorf("--json: mã %d, %s (%v)", code, out, err)
	}
	for _, cmd := range c.fake.Ran() {
		if cmd.Name != "getent" && cmd.Name != "systemctl" && cmd.Name != "dpkg-query" {
			t.Errorf("không đổi gì mà chạy %s", cmd.String())
		}
	}
}

func TestPHPInstallDryRun(t *testing.T) {
	c := newCLI(t)
	c.fake.SetFile("/etc/os-release", []byte(osReleaseNoble), 0o644, system.Root)
	c.seed(stackNginx)
	code, out, errs := c.run("php", "install", "8.4", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, errs)
	}
	var pv engine.Preview
	if err := json.Unmarshal([]byte(out), &pv); err != nil {
		t.Fatalf("không phải JSON của Preview: %v\n%s", err, out)
	}
	if pv.Kind != "php.install" || pv.Target != "8.4" || len(pv.Steps) == 0 || pv.Steps[0].Info.ID != "apt.repo:sury" {
		t.Errorf("Preview = %s/%s %+v", pv.Kind, pv.Target, pv.Steps)
	}
	if bad := c.mutating(); len(bad) != 0 {
		// Bước thêm kho kiểm khoá bằng gpg trong thư mục tạm: không đổi gì của máy.
		for _, b := range bad {
			if !strings.HasPrefix(b, "mktemp ") && !strings.HasPrefix(b, "apt-cache ") {
				t.Errorf("--dry-run chạy lệnh thay đổi máy: %s", b)
			}
		}
	}
	// Không --yes, không terminal: không hỏi được thì từ chối.
	code, _, errs = c.run("php", "install", "8.4")
	if code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("thiếu --yes: mã %d, %q", code, errs)
	}
}

func TestPHPList(t *testing.T) {
	c := newCLI(t)
	c.seed(stackNginx, "shop.example.com")
	c.fake.SetDir("/etc/php/8.3/fpm", 0o755, system.Root)
	c.fake.SetFile("/usr/sbin/php-fpm8.3", nil, 0o755, system.Root)
	c.fake.Handle([]string{"apt-cache", "search"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "php8.3-fpm - server-side, HTML-embedded scripting language (FPM-CGI binary)\n"}
	})
	c.fake.Handle([]string{"apt-cache", "madison"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: " php8.3-fpm | 8.3.6-0ubuntu0.24.04.13 | http://archive.ubuntu.com/ubuntu noble-updates/universe amd64 Packages\n"}
	})
	code, out, errs := c.run("php", "list")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, errs)
	}
	if !strings.Contains(out, "8.3 ") || !strings.Contains(out, "ubuntu") || !strings.Contains(out, "packages.sury.org") || !strings.Contains(out, "Không hỏi được php.net") {
		t.Errorf("output:\n%s", out)
	}
	code, out, _ = c.run("php", "list", "--json")
	var list core.PHPList
	if err := json.Unmarshal([]byte(out), &list); err != nil || code != 0 {
		t.Fatalf("--json: %v\n%s", err, out)
	}
	if len(list.Versions) != 1 || !list.Versions[0].Default || list.Versions[0].Sites[0] != "shop.example.com" || list.SuryEnabled {
		t.Errorf("--json = %+v", list)
	}
}

func TestStackInstallSourceFlags(t *testing.T) {
	c := newCLI(t)
	c.fake.SetFile("/etc/os-release", []byte(osReleaseNoble), 0o644, system.Root)
	code, out, errs := c.run("stack", "install", "--web", "nginx", "--web-source", "nginx.org", "--db-source", "mariadb.org", "--db-line", "11.8", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("mã %d, stderr %s", code, errs)
	}
	var pv engine.Preview
	if err := json.Unmarshal([]byte(out), &pv); err != nil {
		t.Fatal(err)
	}
	if pv.Steps[0].Info.ID != "apt.repo:nginx" || pv.Steps[1].Info.ID != "apt.repo:mariadb" {
		t.Errorf("hai bước đầu = %s, %s", pv.Steps[0].Info.ID, pv.Steps[1].Info.ID)
	}
	if c.dbExists() {
		t.Error("--dry-run tạo file DB")
	}

	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"--web", "apache", "--web-source", "nginx.org"}, "source_unsupported"},
		{[]string{"--web", "nginx", "--web-source", "nginx.org; id"}, "source_unsupported"},
		{[]string{"--web", "nginx", "--db-source", "mariadb.org"}, "mariadb_line_invalid"},
		{[]string{"--web", "nginx", "--db-line", "11.8"}, "mariadb_line_invalid"},
	} {
		code, out, _ := c.run(append(append([]string{"stack", "install"}, tc.args...), "--dry-run", "--json")...)
		var got struct {
			Error core.RefusedError `json:"error"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != tc.code {
			t.Errorf("%q: mã %d, %+v; muốn %s", tc.args, code, got.Error, tc.code)
		}
	}

	// Đã cài với nguồn Ubuntu: chạy lại với nguồn khác bị từ chối.
	c.seed(map[string]string{core.SettingWeb: "nginx", core.SettingWebSource: "ubuntu", core.SettingPHPVersion: "8.3"})
	code, out, _ = c.run("stack", "install", "--web", "nginx", "--web-source", "nginx.org", "--dry-run", "--json")
	var got struct {
		Error core.RefusedError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 1 || got.Error.Code != "source_change_unsupported" {
		t.Errorf("đổi nguồn khi chạy lại: mã %d, %+v", code, got.Error)
	}
}
