package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const dbCLIDom = "shop.example.com"

// dbCLISetup ghi stack và một site active vào DB của CLI, cùng các handler
// mà đổi mật khẩu, xuất DB và kiểm chứng của chúng cần trên máy giả. Trả về
// hàm đọc các câu ALTER USER đã chạy (để lấy mật khẩu mới).
func dbCLISetup(t *testing.T, c *cli) (alters func() []string) {
	t.Helper()
	c.trashSeed(true, site.Site{Domain: domainOf(t, dbCLIDom), Status: site.Active, PHPVersion: "8.3"})
	l := site.LayoutOf(domainOf(t, dbCLIDom))
	f := c.fake
	f.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 60 << 30})
	f.SetFile(l.WPConfig, []byte("<?php\ndefine( 'DB_NAME', '"+l.DB+"' );\ndefine( 'DB_USER', '"+l.DB+"' );\ndefine( 'DB_PASSWORD', 'OldPassw0rdOldPassw0rdOldPassw0r' );\n"),
		0o600, system.Owner{User: l.User, Group: l.User})
	var sqls []string
	f.Handle([]string{"mariadb", "-N", "-B"}, func(cmd system.Cmd) system.Result {
		q := string(cmd.Stdin)
		sqls = append(sqls, q)
		if strings.Contains(q, "information_schema.TABLES") && !strings.Contains(q, "GROUP BY") {
			return system.Result{Stdout: "1048576\n"}
		}
		return system.Result{}
	})
	f.Handle([]string{"wp", "db", "query"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "information_schema\n" + l.DB + "\n"}
	})
	f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "<html><a href='http://" + dbCLIDom + "/wp-content/x'></a></html>TPAN_HTTP_CODE=200"}
	})
	f.Handle([]string{"mariadb-dump"}, func(cmd system.Cmd) system.Result {
		for _, a := range cmd.Args {
			if p, ok := strings.CutPrefix(a, "--result-file="); ok {
				f.SetFile(p, []byte("CREATE TABLE t (id int);\n-- Dump completed on 2026-11-20\n"), 0o600, system.Root)
			}
		}
		return system.Result{}
	})
	f.Handle([]string{"tail"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "-- Dump completed on 2026-11-20\n"}
	})
	f.Handle([]string{"sha256sum"}, func(cmd system.Cmd) system.Result {
		p := cmd.Args[len(cmd.Args)-1]
		data, err := f.ReadFile(p)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: err.Error()}
		}
		sum := sha256.Sum256(data)
		return system.Result{Stdout: hex.EncodeToString(sum[:]) + "  " + p + "\n"}
	})
	return func() []string {
		var out []string
		for _, q := range sqls {
			if strings.Contains(q, "IDENTIFIED BY") {
				out = append(out, q)
			}
		}
		return out
	}
}

func TestDBDomainRejectedBeforeAnything(t *testing.T) {
	for _, bad := range []string{"a.com; rm -rf /", "$(id).com", "../../etc/passwd", "a.com\nb.com", strings.Repeat("a", 300) + ".com"} {
		for _, args := range [][]string{
			{"db", "passwd", bad, "--yes"},
			{"db", "dump", bad, "--dry-run"},
			{"db", "import", bad, "--id", "0123456789abcdef", "--yes"},
			{"cron", "run", bad},
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
}

func TestDBNeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"db", "list"},
		{"db", "passwd", dbCLIDom, "--yes"},
		{"db", "dump", dbCLIDom, "--dry-run"},
		{"db", "import", dbCLIDom, "--id", "0123456789abcdef", "--yes"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") || c.dbExists() || len(c.fake.Ran()) != 0 {
			t.Errorf("%q: mã %d, stderr %q, DB %v", args, code, errs, c.dbExists())
		}
	}
}

func TestDBImportNeedsID(t *testing.T) {
	c := newCLI(t)
	if code, _, errs := c.run("db", "import", dbCLIDom, "--yes"); code != 1 || !strings.Contains(errs, "id") {
		t.Errorf("thiếu --id: mã %d, %q", code, errs)
	}
	// Chưa có tpan.db: không có bản xuất nào, từ chối mà không tạo DB.
	c = newCLI(t)
	code, out, _ := c.run("db", "import", dbCLIDom, "--id", "0123456789abcdef", "--dry-run", "--json")
	if code != 1 || c.dbExists() {
		t.Errorf("mã %d, DB %v, %s", code, c.dbExists(), out)
	}
}

func TestDBList(t *testing.T) {
	c := newCLI(t)
	if code, out, _ := c.run("db", "list"); code != 0 || !strings.Contains(out, "chưa có DB") || c.dbExists() {
		t.Errorf("list rỗng: mã %d, %q, DB %v", code, out, c.dbExists())
	}
	c.trashSeed(true, site.Site{Domain: domainOf(t, dbCLIDom), Status: site.Active, PHPVersion: "8.3"})
	l := site.LayoutOf(domainOf(t, dbCLIDom))
	c.fake.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: l.DB + "\t12\t2097152\n"}
	})
	code, out, errs := c.run("db", "list", "--json")
	if code != 0 {
		t.Fatalf("mã %d: %s", code, errs)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["domain"] != dbCLIDom || rows[0]["db"] != l.DB || rows[0]["tables"] != float64(12) || rows[0]["size_bytes"] != float64(2097152) {
		t.Errorf("JSON = %s", out)
	}
	if code, out, _ := c.run("db", "list"); code != 0 || !strings.Contains(out, "2.0 MiB") || !strings.Contains(out, l.DB) {
		t.Errorf("bảng: mã %d\n%s", code, out)
	}
}

func TestDBPasswdCLI(t *testing.T) {
	c := newCLI(t)
	alters := dbCLISetup(t, c)

	code, out, errs := c.run("db", "passwd", dbCLIDom, "--dry-run")
	if code != 0 || !strings.Contains(out, "IDENTIFIED BY") || len(c.mutating()) != 0 {
		t.Fatalf("--dry-run: mã %d, đổi máy %v\n%s%s", code, c.mutating(), out, errs)
	}

	// Không terminal, không --yes: từ chối, không đổi gì.
	if code, _, _ := c.run("db", "passwd", dbCLIDom); code != 1 || len(alters()) != 0 {
		t.Errorf("không --yes: mã %d, ALTER %d", code, len(alters()))
	}

	code, out, errs = c.run("db", "passwd", dbCLIDom, "--yes")
	if code != 0 {
		t.Fatalf("mã %d\n%s%s", code, out, errs)
	}
	a := alters()
	if len(a) != 1 {
		t.Fatalf("ALTER USER %d lần", len(a))
	}
	pw := regexp.MustCompile(`IDENTIFIED BY '([A-Za-z0-9]{32})'`).FindStringSubmatch(a[0])
	if pw == nil {
		t.Fatalf("câu lạ %q", a[0])
	}
	if strings.Contains(out+errs, pw[1]) {
		t.Error("mật khẩu mới bị in ra")
	}
	for _, cmd := range c.fake.Ran() {
		if strings.Contains(cmd.String(), pw[1]) {
			t.Errorf("mật khẩu trên dòng lệnh: %s", cmd.String())
		}
	}
	if n := c.trashAudits("db.passwd", "ok"); n != 1 {
		t.Errorf("audit db.passwd ok = %d, muốn 1", n)
	}
}

func TestDBDumpAndImportCLI(t *testing.T) {
	c := newCLI(t)
	dbCLISetup(t, c)

	code, out, errs := c.run("db", "dump", dbCLIDom, "--dry-run", "--json")
	if code != 0 || !strings.Contains(out, "mariadb-dump") || len(c.mutating()) != 0 {
		t.Fatalf("--dry-run --json: mã %d, đổi máy %v\n%s%s", code, c.mutating(), out, errs)
	}
	if code, out, errs := c.run("db", "dump", dbCLIDom, "--yes", "--json"); code != 0 || !strings.Contains(out, `"succeeded"`) {
		t.Fatalf("dump: mã %d\n%s%s", code, out, errs)
	}
	if n := c.trashAudits("db.dump", "ok"); n != 1 {
		t.Errorf("audit db.dump ok = %d", n)
	}
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.Backups(context.Background(), dbCLIDom)
	_ = st.Close()
	if err != nil || len(rows) != 1 || rows[0].Kind != "sql" {
		t.Fatalf("bảng backups: %+v %v", rows, err)
	}

	code, out, errs = c.run("db", "import", dbCLIDom, "--id", rows[0].ID, "--yes")
	if code != 0 {
		t.Fatalf("import: mã %d\n%s%s", code, out, errs)
	}
	imported := false
	for _, cmd := range c.fake.Ran() {
		imported = imported || cmd.Name == "mariadb" && strings.Contains(strings.Join(cmd.Args, " "), "source "+rows[0].Path)
	}
	if !imported {
		t.Errorf("không có lệnh nhập %s", rows[0].Path)
	}
	if n := c.trashAudits("db.import", "ok"); n != 1 {
		t.Errorf("audit db.import ok = %d", n)
	}
}
