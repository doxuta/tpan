package main

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// mariadbInstalled: máy noble có mariadb-server 10.11 của Ubuntu.
func (c *cli) mariadbInstalled() {
	c.fake.SetFile("/etc/os-release", []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"), 0o644, system.Root)
	c.fake.Handle([]string{"dpkg-query", "-W", "-f", `${db:Status-Status}\t${Version}`, "mariadb-server"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "installed\t1:10.11.14-0ubuntu0.24.04.1"}
	})
	c.fake.Handle([]string{"dpkg-query", "-W", "-f", `${Package}\t${db:Status-Status}\n`}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stdout: "mariadb-server\tinstalled\nmariadb-client\tinstalled\n"}
	})
}

func TestMariaDBUpgradeFlags(t *testing.T) {
	// Thiếu --to: cobra báo, không chạy gì.
	c := newCLI(t)
	if code, _, errs := c.run("mariadb", "upgrade", "--dry-run"); code != 1 || !strings.Contains(errs, "to") || len(c.fake.Ran()) != 0 {
		t.Errorf("thiếu --to: mã %d, %q", code, errs)
	}
	for _, tc := range []struct{ line, code string }{
		{"a.com; rm -rf /", "mariadb_line_invalid"},
		{"$(id)", "mariadb_line_invalid"},
		{"10.11", "mariadb_not_newer"},
	} {
		c := newCLI(t)
		c.mariadbInstalled()
		code, out, _ := c.run("mariadb", "upgrade", "--to", tc.line, "--dry-run", "--json")
		if code != 1 || refusalCode(t, out) != tc.code || c.dbExists() {
			t.Errorf("%q: mã %d, %s", tc.line, code, out)
		}
		for _, m := range c.mutating() {
			t.Errorf("%q: chạy lệnh %s", tc.line, m)
		}
	}
}

func TestMariaDBUpgradeDryRun(t *testing.T) {
	c := newCLI(t)
	c.mariadbInstalled()
	code, out, errs := c.run("mariadb", "upgrade", "--to", "11.8", "--dry-run")
	if code != 0 {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	for _, s := range []string{"10.11", "11.8", "mariadb-dump", "/var/lib/tpan/backups/mariadb-upgrade-", "systemctl stop mariadb", "mariadb-upgrade"} {
		if !strings.Contains(out, s) {
			t.Errorf("bản xem trước thiếu %q:\n%s", s, out)
		}
	}
	if c.dbExists() {
		t.Error("--dry-run tạo DB")
	}
}
