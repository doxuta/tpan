package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// withStore mở DB của CLI cho test ghi dữ liệu mẫu hay đọc kết quả.
func (c *cli) withStore(fn func(ctx context.Context, st *store.Store)) {
	c.t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	fn(ctx, st)
}

// seedUser thêm user vai user dùng gói pkg (tạo gói với giới hạn lim nếu chưa có).
func (c *cli) seedUser(name, pkg string, lim account.Package) {
	c.t.Helper()
	c.withStore(func(ctx context.Context, st *store.Store) {
		if _, ok, _ := st.Package(ctx, pkg); !ok {
			lim.Name, lim.Created = pkg, time.Now()
			if err := st.SavePackage(ctx, lim); err != nil {
				c.t.Fatal(err)
			}
		}
		u := account.User{Name: name, Role: account.RoleUser, Package: pkg, Status: account.Active, Created: time.Now()}
		if err := st.CreateUser(ctx, u, "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$aGFzaA"); err != nil {
			c.t.Fatal(err)
		}
	})
}

// mariadbM13 cho máy giả trả lời các câu của DB rời: trạng thái tên (chưa có,
// rồi có sau CREATE), danh sách collation; câu khác trả rỗng.
func (c *cli) mariadbM13() {
	created := false
	c.fake.Handle([]string{"mariadb", "-N", "-B"}, func(cmd system.Cmd) system.Result {
		q := string(cmd.Stdin)
		switch {
		case strings.Contains(q, "information_schema.COLLATIONS"):
			return system.Result{Stdout: "utf8mb4\tutf8mb4_general_ci\tYes\nutf8mb4\tutf8mb4_unicode_ci\t\n"}
		case strings.HasPrefix(q, "CREATE DATABASE"):
			created = true
		case strings.Contains(q, "COUNT(*) FROM information_schema.SCHEMATA"):
			if created {
				return system.Result{Stdout: "1\t1\n"}
			}
			return system.Result{Stdout: "0\t0\n"}
		}
		return system.Result{}
	})
	c.fake.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
}

func TestM13ArgsRejectedBeforeAnything(t *testing.T) {
	for _, bad := range []string{"a;rm -rf /", "$(id)", "../../etc/passwd", "a\nb", strings.Repeat("a", 300), "Khách"} {
		for _, args := range [][]string{
			{"user", "add", bad, "--yes"},
			{"user", "rm", bad, "--yes"},
			{"user", "passwd", bad, "--yes"},
			{"user", "suspend", bad, "--dry-run"},
			{"user", "info", bad},
			{"package", "add", bad, "--yes"},
			{"package", "rm", bad, "--yes"},
			{"db", "add", bad, "--yes"},
			{"db", "add", "app", "--owner", bad, "--yes"},
			{"db", "rm", bad, "--yes"},
			{"db", "suspend", bad, "--dry-run"},
			{"db", "edit", bad, "--note", "x", "--yes"},
			{"site", "add", "a.example.com", "--email", "admin@example.com", "--owner", bad, "--dry-run"},
			{"site", "suspend", bad + ".com", "--dry-run"},
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

// user add in mật khẩu đúng một lần (chữ thường và --json ở trường riêng),
// --dry-run không ghi gì, thêm trùng bị từ chối.
func TestUserAdd(t *testing.T) {
	c := newCLI(t)
	c.mariadbM13()
	if code, out, _ := c.run("user", "add", "khach_a", "--yes", "--json"); code != 1 || refusalCode(t, out) != "setup_required" || c.dbExists() {
		t.Errorf("chưa có tpan.db: mã %d, %s", code, out)
	}
	c.trashSeed(false)

	code, out, errs := c.run("user", "add", "khach_a", "--dry-run")
	if code != 0 || !strings.Contains(out, "khach_a") || !strings.Contains(out, "--dry-run") {
		t.Errorf("--dry-run: mã %d, %s%s", code, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if _, ok, _ := st.User(ctx, "khach_a"); ok {
			t.Error("--dry-run mà đã thêm user")
		}
	})
	if code, out, _ := c.run("user", "add", "khach_a", "--json"); code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}

	code, out, errs = c.run("user", "add", "khach_a", "--full-name", "Khách A", "--email", "a@example.com", "--yes", "--json")
	var got server.UserCreated
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || len(got.Password) < 12 || got.User.User.Name != "khach_a" || got.User.User.Role != account.RoleUser {
		t.Fatalf("add --json: mã %d, %v, %s%s", code, err, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		h, ok, err := st.PasswordHashOf(ctx, "khach_a")
		match, verr := store.VerifyPassword(h, got.Password)
		if err != nil || !ok || verr != nil || !match {
			t.Errorf("mật khẩu in ra không khớp hash đã lưu: %v %v", ok, err)
		}
	})
	if n := c.audits("user.add"); n != 1 {
		t.Errorf("audit user.add = %d", n)
	}

	code, out, _ = c.run("user", "add", "khach_b", "--yes")
	if code != 0 || strings.Count(out, "    ") != 1 || !strings.Contains(out, "MỘT lần") {
		t.Errorf("add: mã %d, mật khẩu phải in đúng một lần:\n%s", code, out)
	}
	if code, out, _ := c.run("user", "add", "khach_a", "--yes", "--json"); code != 1 || refusalCode(t, out) != core.CodeUserExists {
		t.Errorf("thêm trùng: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("user", "list"); code != 0 || !strings.Contains(out, "khach_a") || !strings.Contains(out, "khach_b") {
		t.Errorf("list: mã %d\n%s", code, out)
	}
}

func TestUserEditPasswdRm(t *testing.T) {
	c := newCLI(t)
	c.trashSeed(false)
	c.seedUser("khach_a", "co_ban", account.Package{MaxSites: 1})

	if code, out, errs := c.run("user", "edit", "khach_a", "--package", "default", "--yes"); code != 0 {
		t.Errorf("edit: mã %d, %s%s", code, out, errs)
	}
	if code, out, _ := c.run("user", "edit", "khach_a", "--package", "khong_co", "--yes", "--json"); code != 1 || refusalCode(t, out) != core.CodePackageNotFound {
		t.Errorf("edit gói không có: mã %d, %s", code, out)
	}
	code, out, _ := c.run("user", "passwd", "khach_a", "--yes", "--json")
	var pw struct{ Password string }
	if err := json.Unmarshal([]byte(out), &pw); err != nil || code != 0 || len(pw.Password) < 12 {
		t.Errorf("passwd: mã %d, %v, %s", code, err, out)
	}

	// Còn site thì không xoá được.
	s := activeSite(t, "a.example.com", false)
	s.Owner = "khach_a"
	c.seedSites(s)
	if code, out, _ := c.run("user", "rm", "khach_a", "--yes", "--json"); code != 1 || refusalCode(t, out) != core.CodeUserNotEmpty {
		t.Errorf("rm user còn site: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("user", "rm", "khach_a", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != core.CodeUserNotEmpty {
		t.Errorf("rm --dry-run cũng phải từ chối: mã %d, %s", code, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if _, ok, _ := st.User(ctx, "khach_a"); !ok {
			t.Error("user bị xoá dù còn site")
		}
	})

	c.seedUser("khach_c", "default", account.Package{})
	if code, out, errs := c.run("user", "rm", "khach_c", "--yes"); code != 0 || !strings.Contains(out, "khach_c") {
		t.Errorf("rm: mã %d, %s%s", code, out, errs)
	}
	if code, out, _ := c.run("user", "info", "khach_c", "--json"); code != 1 || refusalCode(t, out) != core.CodeUserNotFound {
		t.Errorf("info sau khi xoá: mã %d, %s", code, out)
	}
}

func TestPackageCommands(t *testing.T) {
	c := newCLI(t)
	c.trashSeed(false)
	if code, out, errs := c.run("package", "add", "co_ban", "--max-sites", "2", "--max-dbs", "1", "--yes"); code != 0 {
		t.Fatalf("add: mã %d, %s%s", code, out, errs)
	}
	if code, out, errs := c.run("package", "edit", "co_ban", "--disk-mb", "500", "--yes"); code != 0 {
		t.Fatalf("edit: mã %d, %s%s", code, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		p, _, _ := st.Package(ctx, "co_ban")
		if p.MaxSites != 2 || p.MaxDBs != 1 || p.DiskMB != 500 {
			t.Errorf("edit phải giữ giới hạn cũ, đổi đúng cờ đã ghi: %+v", p)
		}
	})
	code, out, _ := c.run("package", "list", "--json")
	var rows []struct {
		Package account.Package `json:"package"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 || len(rows) != 2 {
		t.Errorf("list: mã %d, %v, %s", code, err, out)
	}
	if code, out, _ := c.run("package", "rm", "default", "--yes", "--json"); code != 1 || refusalCode(t, out) != core.CodePackageInUse {
		t.Errorf("rm default: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("package", "add", "co-ban", "--yes", "--json"); code != 1 || refusalCode(t, out) != "package_name_invalid" {
		t.Errorf("tên có gạch ngang: mã %d, %s", code, out)
	}
	if code, _, errs := c.run("package", "rm", "co_ban", "--yes"); code != 0 {
		t.Errorf("rm: mã %d, %s", code, errs)
	}
}

// site suspend --dry-run in kế hoạch mà không đổi gì trên máy hay trong DB.
func TestSiteSuspendDryRun(t *testing.T) {
	c := newCLI(t)
	c.seedSites(activeSite(t, "a.example.com", false))
	before := c.fake.Snapshot()
	code, out, errs := c.run("site", "suspend", "a.example.com", "--dry-run")
	if code != 0 || !strings.Contains(out, "Kế hoạch:") || !strings.Contains(out, ".suspended") {
		t.Fatalf("--dry-run: mã %d\n%s%s", code, out, errs)
	}
	if m := c.mutating(); len(m) > 0 {
		t.Errorf("--dry-run chạy lệnh thay đổi máy: %v", m)
	}
	if len(c.fake.Snapshot()) != len(before) {
		t.Error("--dry-run đổi hệ thống file")
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		s, _, _ := st.Site(ctx, domainOf(t, "a.example.com"))
		if s.Suspended != account.NotSuspended {
			t.Errorf("--dry-run mà site đã đánh dấu khoá: %q", s.Suspended)
		}
	})
	if code, out, _ := c.run("site", "unsuspend", "a.example.com", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != core.CodeSiteNotSuspended {
		t.Errorf("unsuspend site không khoá: mã %d, %s", code, out)
	}
}

// db add vượt gói bị từ chối trước khi chạy gì; trong gói thì tạo và in mật
// khẩu user DB đúng một lần (--json ở trường db_password).
func TestDBAddQuotaAndPassword(t *testing.T) {
	c := newCLI(t)
	c.trashSeed(true)
	c.mariadbM13()
	c.seedUser("khach_a", "co_ban", account.Package{MaxDBs: 1})
	c.withStore(func(ctx context.Context, st *store.Store) {
		if err := st.SaveStandaloneDB(ctx, account.Database{Name: "khach_a_cu", Owner: "khach_a", Charset: "utf8mb4",
			Collation: "utf8mb4_general_ci", Status: account.DBActive, Created: time.Now()}); err != nil {
			t.Fatal(err)
		}
	})
	c.fake.ResetRan()
	code, out, _ := c.run("db", "add", "moi", "--owner", "khach_a", "--yes", "--json")
	if code != 1 || refusalCode(t, out) != core.CodeQuotaDBs || len(c.mutating()) != 0 {
		t.Errorf("vượt gói: mã %d, %s, lệnh %v", code, out, c.mutating())
	}

	code, out, errs := c.run("db", "add", "app", "--yes", "--json")
	var got dbSecretJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || got.DB != "admin_app" || len(got.DBPassword) != 32 ||
		got.Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("db add --json: mã %d, %v, %s%s", code, err, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		d, ok, _ := st.StandaloneDB(ctx, "admin_app")
		if !ok || d.Owner != "admin" || d.Charset != "utf8mb4" {
			t.Errorf("dòng DB rời = %+v, %v", d, ok)
		}
	})
	// Mật khẩu chỉ đi qua stdin của mariadb, không có trên dòng lệnh.
	for _, cmd := range c.fake.Ran() {
		if strings.Contains(strings.Join(cmd.Args, " "), got.DBPassword) {
			t.Errorf("mật khẩu nằm trên dòng lệnh: %s", cmd.String())
		}
	}
	if code, out, _ := c.run("db", "rm", "a.example.com", "--yes", "--json"); code != 1 || refusalCode(t, out) != "args_invalid" {
		t.Errorf("db rm với domain: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("db", "list"); code != 0 || !strings.Contains(out, "admin_app") || !strings.Contains(out, "rời") {
		t.Errorf("db list: mã %d\n%s", code, out)
	}
}

// apply -f nhận owner: site mới thuộc user đó, kiểm hạn mức của user đó theo
// cả lô; owner không có thì dòng đó bị từ chối.
func TestApplyOwner(t *testing.T) {
	c := newCLI(t)
	c.cleanMachine()
	c.seedSites()
	c.seedUser("khach_a", "co_ban", account.Package{MaxSites: 1})
	c.stdin = "defaults:\n  admin_email: admin@example.com\nsites:\n" +
		"  - domain: a.example.com\n    owner: khach_a\n" +
		"  - domain: b.example.com\n    owner: khach_a\n" +
		"  - domain: c.example.com\n    owner: ma_quai\n" +
		"  - domain: d.example.com\n"
	code, out, errs := c.run("apply", "-f", "-", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("mã %d, %s", code, errs)
	}
	var pv core.ApplyPreview
	if err := json.Unmarshal([]byte(out), &pv); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := map[string]string{"a.example.com": "", "b.example.com": core.CodeQuotaSites, "c.example.com": core.CodeUserNotFound, "d.example.com": ""}
	for _, it := range pv.Items {
		if it.Code != want[it.Domain] {
			t.Errorf("%s: %s (%s); muốn mã %q", it.Domain, it.Action, it.Code, want[it.Domain])
		}
	}
}

func TestExportOwner(t *testing.T) {
	c := newCLI(t)
	s := activeSite(t, "a.example.com", false)
	s.Owner = "khach_a"
	c.seedSites(s, activeSite(t, "b.example.com", false))
	code, out, _ := c.run("export")
	if code != 0 || !strings.Contains(out, "owner: khach_a") || strings.Count(out, "owner:") != 1 {
		t.Errorf("export phải in owner của site không thuộc admin (và chỉ chúng):\n%s", out)
	}
	if code, out, _ := c.run("site", "list"); code != 0 || !strings.Contains(out, "khach_a") {
		t.Errorf("site list thiếu cột chủ:\n%s", out)
	}
}

func TestTrashListStandaloneDB(t *testing.T) {
	c := newCLI(t)
	now := time.Now()
	c.trashSeed(false, site.Site{Domain: domainOf(t, "a.example.com"), Status: site.Trashed, PHPVersion: "8.3", TrashedAt: now, PurgeAt: now.AddDate(0, 0, 7)})
	c.withStore(func(ctx context.Context, st *store.Store) {
		if err := st.SaveStandaloneDB(ctx, account.Database{Name: "khach_b_cu", Owner: "khach_b", Charset: "utf8mb4", Collation: "utf8mb4_general_ci",
			Status: account.DBTrashed, Created: now, TrashedAt: now, PurgeAt: now.AddDate(0, 0, 7)}); err != nil {
			t.Fatal(err)
		}
	})
	code, out, _ := c.run("trash", "list")
	if code != 0 || !strings.Contains(out, "a.example.com") || !strings.Contains(out, "khach_b_cu") {
		t.Errorf("trash list:\n%s", out)
	}
	code, out, _ = c.run("trash", "list", "--json")
	var got trashListJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || len(got.Sites) != 1 || len(got.DBs) != 1 {
		t.Errorf("trash list --json: %v\n%s", err, out)
	}
}

// trash purge --expired dọn cả DB rời hết hạn, kể cả khi không có site nào quá hạn.
func TestTrashPurgeExpiredStandaloneDB(t *testing.T) {
	c := newCLI(t)
	past := time.Now().Add(-time.Hour)
	const name = "khach_b_cu"
	c.trashSeed(true)
	c.withStore(func(ctx context.Context, st *store.Store) {
		if err := st.SaveStandaloneDB(ctx, account.Database{Name: name, Owner: "khach_b", Charset: "utf8mb4", Collation: "utf8mb4_general_ci",
			Status: account.DBTrashed, Created: past, TrashedAt: past, PurgeAt: past}); err != nil {
			t.Fatal(err)
		}
	})
	c.fake.SetFile(account.DBTrashDump(name), []byte("-- Dump completed on 2026-11-20\n"), 0o600, system.Root)
	c.fake.Handle([]string{"getent"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
	c.fake.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "0\t0\n"} })

	code, out, errs := c.run("trash", "purge", "--expired", "--dry-run", "--json")
	var pv struct {
		DBs []purgeDBPreview `json:"dbs"`
	}
	if err := json.Unmarshal([]byte(out), &pv); err != nil || code != 0 || len(pv.DBs) != 1 || pv.DBs[0].Name != name || pv.DBs[0].Preview == nil {
		t.Fatalf("--dry-run --json: mã %d, %v, %s%s", code, err, out, errs)
	}
	if _, out, _ := c.run("trash", "purge", "--expired", "--dry-run"); !strings.Contains(out, "== "+name) {
		t.Errorf("--dry-run thiếu DB rời:\n%s", out)
	}

	code, out, errs = c.run("trash", "purge", "--expired", "--yes", "--json")
	var res struct {
		DBs     []string `json:"dbs"`
		DBError string   `json:"db_error"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 || len(res.DBs) != 1 || res.DBs[0] != name {
		t.Fatalf("--yes --json: mã %d, %v, %s%s", code, err, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if _, ok, _ := st.StandaloneDB(ctx, name); ok {
			t.Error("DB rời hết hạn vẫn còn dòng sau khi dọn")
		}
	})
	if c.trashAudits("dbs.purge", "ok") != 1 {
		t.Error("xoá hẳn DB rời không vào audit")
	}
}

// health cho cả máy đo luôn dung lượng của từng user; health cho một site thì không.
func TestHealthMeasuresDisk(t *testing.T) {
	c, _ := installedCLI(t)
	c.seedUser("khach_a", "default", account.Package{})
	s := activeSite(t, "a.example.com", false)
	s.Owner = "khach_a"
	c.seedSites(s)
	c.fake.SetDir(s.Layout().Dir, 0o750, system.Root)
	c.fake.Handle([]string{"du"}, func(cmd system.Cmd) system.Result {
		return system.Result{Stdout: "12345\t" + cmd.Args[len(cmd.Args)-1] + "\n"}
	})
	usage := func() int64 {
		var n int64
		c.withStore(func(ctx context.Context, st *store.Store) {
			u, err := st.Usage(ctx, "khach_a")
			if err != nil {
				t.Fatal(err)
			}
			n = u.DiskBytes
		})
		return n
	}
	c.run("health", "a.example.com")
	if n := usage(); n != -1 {
		t.Errorf("health một site mà đã đo dung lượng: %d", n)
	}
	c.run("health")
	if n := usage(); n != 12345 {
		t.Errorf("dung lượng sau health = %d; muốn 12345", n)
	}
}

// Các lệnh có Plan của user (khoá, đưa mọi thứ vào thùng rác) và khoá DB của
// site: --dry-run in kế hoạch, không đổi gì.
func TestM13PlansDryRun(t *testing.T) {
	c := newCLI(t)
	c.seedUser("khach_a", "default", account.Package{})
	s := activeSite(t, "a.example.com", false)
	s.Owner = "khach_a"
	c.seedSites(s)
	before := c.fake.Snapshot()
	for _, args := range [][]string{
		{"user", "suspend", "khach_a", "--reason", "chưa trả tiền", "--dry-run"},
		{"user", "trash-all", "khach_a", "--dry-run"},
		{"db", "suspend", "a.example.com", "--dry-run"},
	} {
		code, out, errs := c.run(args...)
		if code != 0 || !strings.Contains(out, "Kế hoạch:") {
			t.Errorf("%q: mã %d\n%s%s", args, code, out, errs)
		}
	}
	for _, m := range c.mutating() {
		// getent chỉ đọc (mutating() chưa biết nó, như ở TestApplyDryRun).
		if !strings.HasPrefix(m, "getent passwd ") {
			t.Errorf("--dry-run chạy lệnh thay đổi máy: %s", m)
		}
	}
	if len(c.fake.Snapshot()) != len(before) {
		t.Error("--dry-run đổi hệ thống file")
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		u, _, _ := st.User(ctx, "khach_a")
		s, _, _ := st.Site(ctx, domainOf(t, "a.example.com"))
		if u.Status != account.Active || s.Suspended != account.NotSuspended || s.DBSuspended != account.NotSuspended || s.Status != site.Active {
			t.Errorf("--dry-run mà đã đổi DB: user %+v, site %+v", u, s)
		}
	})
	if code, out, _ := c.run("user", "unsuspend", "khach_b", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != core.CodeUserNotFound {
		t.Errorf("unsuspend user không có: mã %d, %s", code, out)
	}
}

// admin passwd vẫn đặt lại user admin (M13: hash nằm ở bảng users).
func TestAdminPasswdUsersTable(t *testing.T) {
	c := newCLI(t)
	code, out, _ := c.run("admin", "passwd", "--yes", "--json")
	var got struct {
		AdminPassword string `json:"admin_password"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || got.AdminPassword == "" {
		t.Fatalf("admin passwd: mã %d, %v, %s", code, err, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		u, ok, err := st.User(ctx, account.AdminName)
		h, _, _ := st.PasswordHashOf(ctx, account.AdminName)
		match, _ := store.VerifyPassword(h, got.AdminPassword)
		if err != nil || !ok || u.Role != account.RoleAdmin || !match {
			t.Errorf("user admin = %+v, %v, %v, khớp mật khẩu %v", u, ok, err, match)
		}
	})
}
