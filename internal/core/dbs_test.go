package core

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// dbsSim là MariaDB giả cho test của dbs.go: DB (charset, collation, dữ
// liệu), user (mật khẩu, khoá), kết nối. Câu SQL lạ thì lỗi.
type dbsSim struct {
	*system.FakeSystem

	mu    sync.Mutex
	dbs   map[string]dbsSimDB
	users map[string]dbsSimUser
	conns map[string]string // id → user
}

type dbsSimDB struct{ cs, coll, data string }
type dbsSimUser struct {
	pw     string
	locked bool
	limit  int // MAX_USER_CONNECTIONS (0: không trần)
}

var (
	dbsReState   = regexp.MustCompile(`^SELECT \(SELECT COUNT\(\*\) FROM information_schema\.SCHEMATA WHERE SCHEMA_NAME='(\w+)'\), \(SELECT COUNT\(\*\) FROM mysql\.global_priv WHERE User='(\w+)' AND Host='localhost'\);$`)
	dbsReLockSt  = regexp.MustCompile(`^SELECT COUNT\(\*\), COALESCE\(MAX\(JSON_VALUE.* WHERE User='(\w+)' AND Host='localhost';$`)
	dbsReSchema  = regexp.MustCompile(`^SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema\.SCHEMATA WHERE SCHEMA_NAME='(\w+)';$`)
	dbsReConns   = regexp.MustCompile(`^SELECT ID FROM information_schema\.PROCESSLIST WHERE USER='(\w+)';$`)
	dbsReCreate  = regexp.MustCompile("^CREATE DATABASE `(\\w+)` CHARACTER SET (\\w+) COLLATE (\\w+);$")
	dbsReAlterDB = regexp.MustCompile("^ALTER DATABASE `(\\w+)` CHARACTER SET (\\w+) COLLATE (\\w+);$")
	dbsReLock    = regexp.MustCompile(`^ALTER USER '(\w+)'@'localhost' ACCOUNT (LOCK|UNLOCK);$`)
	dbsRePw      = regexp.MustCompile(`^ALTER USER '(\w+)'@'localhost' IDENTIFIED BY '(\w+)';$`)
	dbsReKill    = regexp.MustCompile(`^KILL CONNECTION (\d+);$`)
	dbsReCrUser  = regexp.MustCompile(`^CREATE USER '(\w+)'@'localhost' IDENTIFIED BY '(\w+)' WITH MAX_USER_CONNECTIONS 20;$`)
	dbsReGrant   = regexp.MustCompile("^GRANT ALL PRIVILEGES ON `[\\w\\\\]+`\\.\\* TO '(\\w+)'@'localhost';$")
	dbsReDrUser  = regexp.MustCompile(`^DROP USER IF EXISTS '(\w+)'@'localhost';$`)
	dbsReDrDB    = regexp.MustCompile("^DROP DATABASE IF EXISTS `(\\w+)`;$")
	dbsReLimSt   = regexp.MustCompile(`^SELECT COUNT\(\*\), COALESCE\(MAX\(max_user_connections\), 0\) FROM mysql\.user WHERE User='(\w+)' AND Host='localhost';$`)
	dbsReLimit   = regexp.MustCompile(`^ALTER USER '(\w+)'@'localhost' WITH MAX_USER_CONNECTIONS (\d+);$`)
	dbsReKillU   = regexp.MustCompile(`^KILL USER '(\w+)'@'localhost';$`)
)

const dbsCollationsOut = "latin1\tlatin1_bin\t\nlatin1\tlatin1_swedish_ci\tYes\n" +
	"utf8mb4\tutf8mb4_bin\t\nutf8mb4\tutf8mb4_general_ci\tYes\nutf8mb4\tutf8mb4_unicode_ci\t\n" +
	"NULL\tuca1400_ai_ci\t\ndòng lạ\n"

func newDBsSim() *dbsSim {
	f := &dbsSim{FakeSystem: system.NewFake(), dbs: map[string]dbsSimDB{}, users: map[string]dbsSimUser{}, conns: map[string]string{}}
	f.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 80 << 30})
	fail := func(s string) system.Result { return system.Result{ExitCode: 1, Stderr: s} }
	f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out strings.Builder
		for st := range strings.SplitSeq(strings.TrimSpace(string(c.Stdin)), "\n") {
			b := func(v bool) int {
				if v {
					return 1
				}
				return 0
			}
			var m []string
			match := func(re *regexp.Regexp) bool { m = re.FindStringSubmatch(st); return m != nil }
			switch {
			case st == strings.TrimSpace(dbsCollationsSQL):
				out.WriteString(dbsCollationsOut)
			case strings.HasPrefix(st, "SELECT s.SCHEMA_NAME"):
				for _, n := range slices.Sorted(maps.Keys(f.dbs)) {
					if strings.Contains(st, "'"+n+"'") {
						fmt.Fprintf(&out, "%s\t3\t49152\t%s\t%s\n", n, f.dbs[n].cs, f.dbs[n].coll)
					}
				}
			case strings.HasPrefix(st, "SELECT COALESCE(SUM(DATA_LENGTH"):
				out.WriteString("4096\n")
			case match(dbsReState):
				_, d := f.dbs[m[1]]
				_, u := f.users[m[2]]
				fmt.Fprintf(&out, "%d\t%d\n", b(d), b(u))
			case match(dbsReLockSt):
				u, ok := f.users[m[1]]
				fmt.Fprintf(&out, "%d\t%d\n", b(ok), b(u.locked))
			case match(dbsReSchema):
				if d, ok := f.dbs[m[1]]; ok {
					out.WriteString(d.cs + "\t" + d.coll + "\n")
				}
			case match(dbsReConns):
				for _, id := range slices.Sorted(maps.Keys(f.conns)) {
					if f.conns[id] == m[1] {
						out.WriteString(id + "\n")
					}
				}
			case match(dbsReCreate):
				if _, ok := f.dbs[m[1]]; ok {
					return fail("ERROR 1007: database exists")
				}
				f.dbs[m[1]] = dbsSimDB{cs: m[2], coll: m[3]}
			case match(dbsReAlterDB):
				d := f.dbs[m[1]]
				d.cs, d.coll = m[2], m[3]
				f.dbs[m[1]] = d
			case match(dbsReLock):
				u := f.users[m[1]]
				u.locked = m[2] == "LOCK"
				f.users[m[1]] = u
			case match(dbsRePw):
				u := f.users[m[1]]
				u.pw = m[2]
				f.users[m[1]] = u
			case match(dbsReKill):
				delete(f.conns, m[1])
			case match(dbsReLimSt):
				u, ok := f.users[m[1]]
				fmt.Fprintf(&out, "%d\t%d\n", b(ok), u.limit)
			case match(dbsReLimit):
				u := f.users[m[1]]
				u.limit, _ = strconv.Atoi(m[2])
				f.users[m[1]] = u
			case match(dbsReKillU):
				for id, u := range f.conns {
					if u == m[1] {
						delete(f.conns, id)
					}
				}
			case match(dbsReCrUser):
				f.users[m[1]] = dbsSimUser{pw: m[2], limit: 20}
			case match(dbsReGrant):
			case match(dbsReDrUser):
				delete(f.users, m[1])
			case match(dbsReDrDB):
				delete(f.dbs, m[1])
			default:
				return fail("dbsSim: câu SQL lạ: " + st)
			}
		}
		return system.Result{Stdout: out.String()}
	})
	f.Handle([]string{"mariadb-dump"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := strings.TrimPrefix(c.Args[2], "--result-file=")
		d := f.dbs[c.Args[3]]
		f.SetFile(p, []byte("-- data:"+d.data+"\n-- Dump completed on x\n"), 0o600, system.Root)
		return system.Result{}
	})
	f.Handle([]string{"tail", "-n", "1", "--"}, func(c system.Cmd) system.Result {
		data, err := f.ReadFile(c.Args[3])
		if err != nil {
			return fail("tail: no file")
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		return system.Result{Stdout: lines[len(lines)-1] + "\n"}
	})
	// mariadb --database=<db> --abort-source-on-error -e "source <file>"
	f.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		db := strings.TrimPrefix(c.Args[0], "--database=")
		raw, err := f.ReadFile(strings.TrimPrefix(c.Args[3], "source "))
		d, ok := f.dbs[db]
		if err != nil || !ok {
			return fail("import lỗi")
		}
		d.data = strings.TrimPrefix(strings.SplitN(string(raw), "\n", 2)[0], "-- data:")
		f.dbs[db] = d
		return system.Result{}
	})
	return f
}

// ---- store giả ----

type dbsMemStore struct {
	mu  sync.Mutex
	dbs map[string]account.Database
}

func (m *dbsMemStore) StandaloneDB(_ context.Context, name string) (account.Database, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.dbs[name]
	return d, ok, nil
}

func (m *dbsMemStore) StandaloneDBs(_ context.Context, owner string) ([]account.Database, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []account.Database{}
	for _, n := range slices.Sorted(maps.Keys(m.dbs)) {
		if owner == "" || m.dbs[n].Owner == owner {
			out = append(out, m.dbs[n])
		}
	}
	return out, nil
}

func (m *dbsMemStore) SaveStandaloneDB(_ context.Context, d account.Database) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbs[d.Name] = d
	return nil
}

func (m *dbsMemStore) DeleteStandaloneDB(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.dbs, name)
	return nil
}

// dbsMemAccounts chỉ có phần AccountStore mà DB rời đọc (User; Package và
// Usage để checkQuota thật chạy được). Gọi phương thức khác thì panic.
type dbsMemAccounts struct {
	AccountStore
	users map[string]account.User
}

func (m *dbsMemAccounts) User(_ context.Context, name string) (account.User, bool, error) {
	u, ok := m.users[name]
	return u, ok, nil
}

func (m *dbsMemAccounts) Package(context.Context, string) (account.Package, bool, error) {
	return account.Package{Name: account.DefaultPackage}, true, nil
}

func (m *dbsMemAccounts) Usage(context.Context, string) (account.Usage, error) {
	return account.Usage{DiskBytes: -1}, nil
}

// ---- môi trường ----

var dbsNow = time.Date(2026, 11, 20, 3, 4, 5, 0, time.UTC)

type dbsEnv struct {
	d     *DBs
	sim   *dbsSim
	store *dbsMemStore
	sites *memSites
	acc   *dbsMemAccounts
}

func newDBsEnv(t *testing.T) *dbsEnv {
	t.Helper()
	sim := newDBsSim()
	ms := &dbsMemStore{dbs: map[string]account.Database{}}
	acc := &dbsMemAccounts{users: map[string]account.User{
		"alice": {Name: "alice", Role: account.RoleUser, Status: account.Active, Package: account.DefaultPackage},
		"bob":   {Name: "bob", Role: account.RoleUser, Status: account.Suspended, Package: account.DefaultPackage},
	}}
	sites := newMemSites()
	return &dbsEnv{
		d:   &DBs{Sys: sim, Store: ms, Sites: sites, Accounts: acc, Journal: dbsJournal{}, Now: func() time.Time { return dbsNow }},
		sim: sim, store: ms, sites: sites, acc: acc,
	}
}

// dbsJournal là Journal không ghi gì (test không đọc lịch sử job).
type dbsJournal struct{}

func (dbsJournal) CreateJob(context.Context, engine.Job) error              { return nil }
func (dbsJournal) AddEvent(context.Context, engine.Event) error             { return nil }
func (dbsJournal) FinishJob(context.Context, engine.Job) error              { return nil }
func (dbsJournal) RecordFile(context.Context, string, string, string) error { return nil }
func (dbsJournal) ForgetFile(context.Context, string) error                 { return nil }

func dbsUser(t *testing.T, s string) validate.UserName {
	t.Helper()
	u, err := validate.ParseUserName(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// dbsExec(t)(e.d.PrepareX(...)) chạy Plan và đòi job thành công.
func dbsExec(t *testing.T) func(p *Planned, err error) engine.Report {
	return func(p *Planned, err error) engine.Report {
		t.Helper()
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		rep, err := p.Execute(ctx, nil)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("Execute: %v, %s %s", err, rep.Job.Status, rep.Job.Error)
		}
		return rep
	}
}

// add tạo alice_shop và trả mật khẩu.
func (e *dbsEnv) add(t *testing.T) *DBAdd {
	t.Helper()
	a, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, "alice"), Name: "shop", Note: "app"})
	if err != nil {
		t.Fatal(err)
	}
	dbsExec(t)(a.Planned, nil)
	return a
}

// ---- test ----

func TestDBAddStandalone(t *testing.T) {
	e := newDBsEnv(t)
	a := e.add(t)
	if a.DB.Name != "alice_shop" || a.DB.Charset != "utf8mb4" || a.DB.Collation != "utf8mb4_general_ci" {
		t.Errorf("DB = %+v; muốn utf8mb4 và collation mặc định đọc từ COLLATIONS", a.DB)
	}
	if a.Plan.Target != "alice_shop" {
		t.Errorf("Target = %q, muốn đúng tên DB (store suy chủ của job từ đây)", a.Plan.Target)
	}
	row, ok := e.store.dbs["alice_shop"]
	if !ok || row.Owner != "alice" || row.Status != account.DBActive || row.Note != "app" || !row.Created.Equal(dbsNow) {
		t.Errorf("dòng = %+v", row)
	}
	if u := e.sim.users["alice_shop"]; u.pw != a.Password.Reveal() {
		t.Error("user DB không có đúng mật khẩu đã trả")
	}
	if e.sim.dbs["alice_shop"].coll != "utf8mb4_general_ci" {
		t.Errorf("DB = %+v", e.sim.dbs["alice_shop"])
	}
	for _, c := range e.sim.Ran() {
		if strings.Contains(c.String(), a.Password.Reveal()) {
			t.Errorf("mật khẩu trên dòng lệnh: %s", c.String())
		}
	}
	// Chỉ collation: charset suy từ nó.
	b, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, "alice"), Name: "blog", Collation: "latin1_bin"})
	if err != nil || b.DB.Charset != "latin1" {
		t.Errorf("chỉ collation: %+v, %v", b, err)
	}
}

func TestDBAddRefusals(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.sim.users["alice_raw"] = dbsSimUser{} // user DB tạo tay ngoài tpan
	alice := dbsUser(t, "alice")
	cases := []struct {
		name string
		in   DBInput
		want string
	}{
		{"tên sai", DBInput{Owner: alice, Name: "a; rm -rf /"}, validate.CodeDBNameInvalid},
		{"tên $(id)", DBInput{Owner: alice, Name: "$(id)"}, validate.CodeDBNameInvalid},
		{"tên dài", DBInput{Owner: alice, Name: strings.Repeat("a", 300)}, validate.CodeDBNameInvalid},
		{"thiếu chủ", DBInput{Name: "x"}, validate.CodeDBNameInvalid},
		{"chủ không có", DBInput{Owner: dbsUser(t, "carol"), Name: "x"}, CodeUserNotFound},
		{"chủ bị khoá", DBInput{Owner: dbsUser(t, "bob"), Name: "x"}, CodeUserSuspended},
		{"trùng bảng", DBInput{Owner: alice, Name: "shop"}, CodeDBExists},
		{"trùng MariaDB", DBInput{Owner: alice, Name: "raw"}, CodeDBExists},
		{"collation lạ", DBInput{Owner: alice, Name: "x", Charset: "utf8mb4", Collation: "latin1_bin"}, CodeCollationInvalid},
		{"collation chèn", DBInput{Owner: alice, Name: "x", Collation: "x; DROP DATABASE y"}, CodeCollationInvalid},
		{"collation không charset (uca1400)", DBInput{Owner: alice, Name: "x", Collation: "uca1400_ai_ci"}, CodeCollationInvalid},
		{"ghi chú xuống dòng", DBInput{Owner: alice, Name: "x", Note: "a\nb"}, codeNoteInvalid},
	}
	for _, c := range cases {
		_, err := e.d.PrepareAdd(ctx, c.in)
		if got := dbsCode(err); got != c.want {
			t.Errorf("%s: mã %q (%v), muốn %q", c.name, got, err, c.want)
		}
	}
}

// dbsCode lấy mã của RefusedError hoặc validate.Error.
func dbsCode(err error) string {
	switch e := err.(type) {
	case *RefusedError:
		return e.Code
	case *validate.Error:
		return e.Code
	case nil:
		return ""
	}
	return "lỗi khác: " + err.Error()
}

func TestDBCollationsParse(t *testing.T) {
	got := parseCollations(dbsCollationsOut + "utf8mb4\tx'y\t\n")
	if len(got) != 5 || got[1] != (Collation{"latin1", "latin1_swedish_ci", true}) {
		t.Errorf("parseCollations = %+v; muốn bỏ dòng NULL, dòng lạ, tên lạ", got)
	}
	e := newDBsEnv(t)
	if l, err := e.d.Collations(ctx); err != nil || len(l) != 5 {
		t.Errorf("Collations = %v, %v", l, err)
	}
}

func TestDBSuspendStandalone(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.sim.conns["41"] = "alice_shop"
	p, err := e.d.PrepareSuspend(ctx, "alice_shop")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Plan.Steps[0].Info().Danger || !strings.Contains(p.Plan.Title.Vi, "lỗi kết nối DB") {
		t.Error("xem trước phải nói rõ sẽ mất kết nối DB")
	}
	dbsExec(t)(p, nil)
	if !e.sim.users["alice_shop"].locked || len(e.sim.conns) != 0 {
		t.Errorf("user = %+v, kết nối = %v", e.sim.users["alice_shop"], e.sim.conns)
	}
	if r := e.store.dbs["alice_shop"]; r.Suspended != account.SuspendSelf || !r.SuspendedAt.Equal(dbsNow) {
		t.Errorf("dòng = %+v", r)
	}
	if _, err := e.d.PrepareSuspend(ctx, "alice_shop"); dbsCode(err) != CodeDBSuspended {
		t.Errorf("khoá lại: %v", err)
	}
	if _, err := e.d.PrepareTrash(ctx, "alice_shop"); dbsCode(err) != CodeDBSuspended {
		t.Errorf("thùng rác khi đang khoá: %v, muốn db_suspended", err)
	}
	// Đổi mật khẩu khi khoá thì được, và user vẫn khoá.
	pw, err := e.d.PreparePassword(ctx, "alice_shop")
	dbsExec(t)(pw.Planned, err)
	if u := e.sim.users["alice_shop"]; u.pw != pw.Password.Reveal() || !u.locked {
		t.Errorf("sau đổi mật khẩu: %+v", u)
	}
	dbsExec(t)(e.d.PrepareUnsuspend(ctx, "alice_shop"))
	if e.sim.users["alice_shop"].locked || e.store.dbs["alice_shop"].Suspended != "" {
		t.Error("chưa mở khoá")
	}
	if _, err := e.d.PrepareUnsuspend(ctx, "alice_shop"); dbsCode(err) != CodeDBNotSuspended {
		t.Errorf("mở khoá DB không khoá: %v", err)
	}
	if _, err := e.d.PrepareSuspend(ctx, "nope"); dbsCode(err) != CodeDBNotFound {
		t.Errorf("DB không có: %v", err)
	}
}

// Mở khoá DB bị khoá vì user, khi user vẫn khoá: mở khoá lén, từ chối.
func TestDBUnsuspendByUserRefused(t *testing.T) {
	e := newDBsEnv(t)
	e.store.dbs["bob_x"] = account.Database{Name: "bob_x", Owner: "bob", Status: account.DBActive, Suspended: account.SuspendByUser}
	if _, err := e.d.PrepareUnsuspend(ctx, "bob_x"); dbsCode(err) != CodeUserSuspended {
		t.Errorf("mã %v, muốn user_suspended", err)
	}
	// Khoá riêng (self) đè lên khoá vì user thì được.
	e.sim.users["bob_x"] = dbsSimUser{locked: true}
	dbsExec(t)(e.d.PrepareSuspend(ctx, "bob_x"))
	if e.store.dbs["bob_x"].Suspended != account.SuspendSelf {
		t.Error("nguồn khoá chưa chuyển sang self")
	}
}

func TestDBTrashRestorePurge(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.sim.mu.Lock()
	d := e.sim.dbs["alice_shop"]
	d.data = "orders"
	e.sim.dbs["alice_shop"] = d
	e.sim.conns["7"] = "alice_shop"
	e.sim.mu.Unlock()

	p, err := e.d.PrepareTrash(ctx, "alice_shop")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range p.Plan.Steps {
		ids = append(ids, strings.SplitN(s.Info().ID, ":", 2)[0])
	}
	if want := []string{"mariadb.lock", "mariadb.kill", "mariadb.trash-dump", "mariadb.drop"}; !slices.Equal(ids, want) {
		t.Errorf("thứ tự bước = %v, muốn %v (khoá trước, DROP sau cùng)", ids, want)
	}
	dbsExec(t)(p, nil)
	r := e.store.dbs["alice_shop"]
	if r.Status != account.DBTrashed || !r.TrashedAt.Equal(dbsNow) || !r.PurgeAt.Equal(dbsNow.AddDate(0, 0, DefaultTrashDays)) {
		t.Errorf("dòng = %+v", r)
	}
	if _, ok := e.sim.dbs["alice_shop"]; ok {
		t.Error("DB chưa bị DROP")
	}
	if u, ok := e.sim.users["alice_shop"]; !ok || !u.locked {
		t.Error("trong thùng rác user DB phải còn và đang khoá")
	}
	if _, err := e.sim.Stat(account.DBTrashDump("alice_shop")); err != nil {
		t.Fatalf("file xuất: %v", err)
	}
	for _, f := range []func() error{
		func() error { _, err := e.d.PreparePassword(ctx, "alice_shop"); return err },
		func() error { _, err := e.d.PrepareSuspend(ctx, "alice_shop"); return err },
		func() error { _, err := e.d.PrepareCharset(ctx, "alice_shop", "", ""); return err },
		func() error { _, err := e.d.PrepareTrash(ctx, "alice_shop"); return err },
		func() error { _, err := e.d.PrepareUnsuspend(ctx, "alice_shop"); return err },
	} {
		if dbsCode(f()) != codeDBTrashed {
			t.Error("thao tác trên DB trong thùng rác: muốn db_trashed")
		}
	}

	e.store.mu.Lock()
	r = e.store.dbs["alice_shop"]
	r.Suspended = account.SuspendByUser
	e.store.dbs["alice_shop"] = r
	e.store.mu.Unlock()
	if _, err := e.d.PrepareUnsuspend(ctx, "alice_shop"); dbsCode(err) != codeDBTrashed {
		t.Errorf("mở khoá DB trong thùng rác: %v, muốn db_trashed", err)
	}
	r.Suspended = ""
	e.store.dbs["alice_shop"] = r
	// D10: user vào thùng rác lúc máy chưa có bản vá M1-DBCONN (không trần),
	// còn một kết nối mở từ trước.
	u := e.sim.users["alice_shop"]
	u.limit = 0
	e.sim.users["alice_shop"] = u
	e.sim.conns["77"] = "alice_shop"

	dbsExec(t)(e.d.PrepareRestore(ctx, "alice_shop"))
	if n := e.sim.users["alice_shop"].limit; n != site.DBMaxUserConnections {
		t.Errorf("trần sau khôi phục DB rời = %d, muốn %d", n, site.DBMaxUserConnections)
	}
	if _, ok := e.sim.conns["77"]; ok {
		t.Error("kết nối mở trước khi có trần không bị cắt (D3)")
	}
	if got := e.sim.dbs["alice_shop"]; got.data != "orders" || got.coll != "utf8mb4_general_ci" {
		t.Errorf("DB khôi phục = %+v", got)
	}
	if e.sim.users["alice_shop"].locked {
		t.Error("user DB chưa mở khoá")
	}
	if _, err := e.sim.Stat(account.DBTrashDump("alice_shop")); err == nil {
		t.Error("file xuất còn sau khôi phục")
	}
	if r := e.store.dbs["alice_shop"]; r.Status != account.DBActive || !r.PurgeAt.IsZero() {
		t.Errorf("dòng sau khôi phục = %+v", r)
	}
	if _, err := e.d.PrepareRestore(ctx, "alice_shop"); dbsCode(err) != codeDBNotTrashed {
		t.Errorf("khôi phục DB không ở thùng rác: %v", err)
	}

	// Vào thùng rác lần nữa rồi tự xoá hẳn khi hết hạn.
	dbsExec(t)(e.d.PrepareTrash(ctx, "alice_shop"))
	got, err := e.d.PurgeExpired(ctx, dbsNow.AddDate(0, 0, DefaultTrashDays-1), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("chưa hết hạn mà xoá: %v %v", got, err)
	}
	got, err = e.d.PurgeExpired(ctx, dbsNow.AddDate(0, 0, DefaultTrashDays), nil)
	if err != nil || !slices.Equal(got, []string{"alice_shop"}) {
		t.Fatalf("PurgeExpired = %v, %v", got, err)
	}
	if _, ok := e.store.dbs["alice_shop"]; ok {
		t.Error("dòng còn")
	}
	if _, ok := e.sim.users["alice_shop"]; ok {
		t.Error("user DB còn")
	}
	if _, err := e.sim.Stat(account.DBTrashDump("alice_shop")); err == nil {
		t.Error("file xuất còn")
	}
}

func TestDBRestoreRefusals(t *testing.T) {
	e := newDBsEnv(t)
	e.store.dbs["bob_x"] = account.Database{Name: "bob_x", Owner: "bob", Status: account.DBTrashed, Charset: "utf8mb4", Collation: "utf8mb4_bin"}
	if _, err := e.d.PrepareRestore(ctx, "bob_x"); dbsCode(err) != CodeUserSuspended {
		t.Errorf("chủ bị khoá: %v", err)
	}
	e.store.dbs["alice_x"] = account.Database{Name: "alice_x", Owner: "alice", Status: account.DBTrashed, Charset: "utf8mb4", Collation: "utf8mb4_bin"}
	if _, err := e.d.PrepareRestore(ctx, "alice_x"); dbsCode(err) != codeDBDumpMissing {
		t.Errorf("thiếu file xuất: %v", err)
	}
	e.sim.SetFile(account.DBTrashDump("alice_x"), []byte("-- Dump completed\n"), 0o600, system.Root)
	e.sim.dbs["alice_x"] = dbsSimDB{cs: "utf8mb4", coll: "utf8mb4_bin"}
	if _, err := e.d.PrepareRestore(ctx, "alice_x"); dbsCode(err) != CodeDBExists {
		t.Errorf("DB đã có trong MariaDB: %v", err)
	}
	if _, err := e.d.PreparePurge(ctx, "nope"); dbsCode(err) != CodeDBNotFound {
		t.Errorf("xoá hẳn DB không có: %v", err)
	}
}

func TestDBCharsetAndNote(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	dbsExec(t)(e.d.PrepareCharset(ctx, "alice_shop", "latin1", ""))
	if r := e.store.dbs["alice_shop"]; r.Charset != "latin1" || r.Collation != "latin1_swedish_ci" {
		t.Errorf("dòng = %+v", r)
	}
	if d := e.sim.dbs["alice_shop"]; d.coll != "latin1_swedish_ci" {
		t.Errorf("MariaDB = %+v", d)
	}
	if _, err := e.d.PrepareCharset(ctx, "alice_shop", "latin1", "utf8mb4_bin"); dbsCode(err) != CodeCollationInvalid {
		t.Errorf("cặp sai: %v", err)
	}
	if err := e.d.SetNote(ctx, "alice_shop", "đơn hàng"); err != nil || e.store.dbs["alice_shop"].Note != "đơn hàng" {
		t.Errorf("SetNote: %v", err)
	}
	if err := e.d.SetNote(ctx, "alice_shop", strings.Repeat("x", 600)); dbsCode(err) != codeNoteInvalid {
		t.Errorf("ghi chú dài: %v", err)
	}
	if err := e.d.SetNote(ctx, "../../etc/passwd", "x"); dbsCode(err) != CodeDBNotFound {
		t.Errorf("tên lạ: %v", err)
	}
}

func (e *dbsEnv) site(t *testing.T, dom string, mod func(*site.Site)) site.Site {
	t.Helper()
	st := site.Site{Domain: mustDomain(t, dom), Status: site.Active, Owner: "alice", PHPVersion: "8.3"}
	if mod != nil {
		mod(&st)
	}
	if err := e.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	l := st.Layout()
	e.sim.dbs[l.DB] = dbsSimDB{cs: "utf8mb4", coll: "utf8mb4_unicode_ci"}
	e.sim.users[l.DB] = dbsSimUser{pw: "x"}
	return st
}

func TestDBSiteSuspend(t *testing.T) {
	e := newDBsEnv(t)
	// Site đang tạm khoá: mở khoá DB không kiểm site 200 (vhost trả 403).
	st := e.site(t, "shop.example.com", func(s *site.Site) { s.Suspended = account.SuspendSelf })
	dom, l := st.Domain, st.Layout()
	e.sim.conns["3"] = l.DB

	p, err := e.d.PrepareSiteDBSuspend(ctx, dom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Plan.Kind != "db.suspend" || p.Plan.Target != dom.String() || !strings.Contains(p.Plan.Title.Vi, "lỗi kết nối DB") {
		t.Errorf("Plan = %s %s %q", p.Plan.Kind, p.Plan.Target, p.Plan.Title.Vi)
	}
	dbsExec(t)(p, nil)
	if !e.sim.users[l.DB].locked || len(e.sim.conns) != 0 {
		t.Error("user DB của site chưa khoá hoặc còn kết nối")
	}
	if got, _, _ := e.sites.Site(ctx, dom); got.DBSuspended != account.SuspendSelf || got.Suspended != account.SuspendSelf {
		t.Errorf("site = %+v", got)
	}
	if _, err := e.d.PrepareSiteDBSuspend(ctx, dom); dbsCode(err) != CodeDBSuspended {
		t.Errorf("khoá lại: %v", err)
	}
	u, err := e.d.PrepareSiteDBUnsuspend(ctx, dom)
	if err != nil || len(u.Plan.Verify) != 0 {
		t.Fatalf("site tạm khoá: Verify = %v, %v", u, err)
	}
	dbsExec(t)(u, nil)
	if got, _, _ := e.sites.Site(ctx, dom); got.DBSuspended != "" || e.sim.users[l.DB].locked {
		t.Error("chưa mở khoá")
	}
	if _, err := e.d.PrepareSiteDBUnsuspend(ctx, dom); dbsCode(err) != CodeDBNotSuspended {
		t.Errorf("mở DB không khoá: %v", err)
	}

	// Site chạy bình thường: mở khoá DB có kiểm chứng DB và site 200.
	st2 := e.site(t, "blog.example.com", func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	u2, err := e.d.PrepareSiteDBUnsuspend(ctx, st2.Domain)
	if err != nil || len(u2.Plan.Verify) != 2 {
		t.Errorf("Verify = %v, %v; muốn DBAccess và HTTP", u2, err)
	}
	// Khoá vì user mà chủ vẫn khoá: từ chối.
	st3 := e.site(t, "bob.example.com", func(s *site.Site) { s.Owner, s.DBSuspended = "bob", account.SuspendByUser })
	if _, err := e.d.PrepareSiteDBUnsuspend(ctx, st3.Domain); dbsCode(err) != CodeUserSuspended {
		t.Errorf("mở khoá lén: %v", err)
	}
	if _, err := e.d.PrepareSiteDBSuspend(ctx, mustDomain(t, "none.example.com")); dbsCode(err) != "site_not_found" {
		t.Errorf("site không có: %v", err)
	}
	st4 := e.site(t, "old.example.com", func(s *site.Site) { s.Status = site.Trashed })
	if _, err := e.d.PrepareSiteDBSuspend(ctx, st4.Domain); dbsCode(err) != "site_trashed" {
		t.Errorf("site thùng rác: %v", err)
	}
}

func TestDBSiteCharset(t *testing.T) {
	e := newDBsEnv(t)
	st := e.site(t, "shop.example.com", nil)
	p, err := e.d.PrepareSiteDBCharset(ctx, st.Domain, "utf8mb4", "utf8mb4_bin")
	if err != nil || p.Plan.Kind != "db.charset" || p.Plan.Target != st.Domain.String() {
		t.Fatalf("%v %v", p, err)
	}
	dbsExec(t)(p, nil)
	if d := e.sim.dbs[st.Layout().DB]; d.coll != "utf8mb4_bin" {
		t.Errorf("DB = %+v", d)
	}
}

func TestDatabaseListM13(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	sa := e.site(t, "a.example.com", func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	e.site(t, "b.example.com", func(s *site.Site) { s.Owner = "admin" })
	e.store.dbs["bob_old"] = account.Database{Name: "bob_old", Owner: "bob", Status: account.DBTrashed, Charset: "latin1", Collation: "latin1_bin"}
	e.sim.ResetRan()

	all, err := e.d.List(ctx, "")
	if err != nil || len(all) != 4 {
		t.Fatalf("List = %+v, %v", all, err)
	}
	if n := len(e.sim.Ran()); n != 1 {
		t.Errorf("chạy %d lệnh, muốn đúng một câu SQL", n)
	}
	a := all[0]
	if a.Kind != DBKindSite || a.Domain != "a.example.com" || a.DB != sa.Layout().DB || a.Owner != "alice" ||
		a.Suspended != account.SuspendSelf || a.Status != "active" || a.Tables != 3 || a.SizeBytes != 49152 || a.Collation != "utf8mb4_unicode_ci" {
		t.Errorf("dòng site = %+v", a)
	}
	var shop, old Database
	for _, r := range all {
		switch r.DB {
		case "alice_shop":
			shop = r
		case "bob_old":
			old = r
		}
	}
	if shop.Kind != DBKindStandalone || shop.Domain != "" || shop.Note != "app" || shop.Tables != 3 || shop.Charset != "utf8mb4" {
		t.Errorf("DB rời = %+v", shop)
	}
	if old.Status != "trashed" || old.Tables != -1 || old.Collation != "latin1_bin" {
		t.Errorf("DB rời trong thùng rác = %+v (số liệu -1, charset từ bảng)", old)
	}

	mine, err := e.d.List(ctx, "alice")
	if err != nil || len(mine) != 2 {
		t.Errorf("List(alice) = %+v, %v; muốn site a và alice_shop", mine, err)
	}
	for _, r := range mine {
		if r.Owner != "alice" {
			t.Errorf("lọt DB của %s", r.Owner)
		}
	}
	// MariaDB không trả lời: danh sách vẫn đủ, số liệu -1.
	e.sim.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 1} })
	down, err := e.d.List(ctx, "")
	if err != nil || len(down) != 4 || down[0].Tables != -1 {
		t.Errorf("MariaDB dừng: %+v, %v", down, err)
	}
}

// Đổi mật khẩu, nhập DB của site khi site hay DB của site đang tạm khoá:
// kiểm chứng (DB đăng nhập được, site 200) chắc chắn hỏng, nên từ chối trước.
func TestDBOpsRefuseSuspended(t *testing.T) {
	de := newDBEnv(t, false)
	dom := mustDomain(t, dbDom)
	set := func(fn func(*site.Site)) {
		st := de.siteOf(t, dbDom)
		st.Suspended, st.DBSuspended = "", ""
		fn(&st)
		if err := de.sites.SaveSite(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	set(func(s *site.Site) { s.Suspended = account.SuspendSelf })
	if _, err := de.b.PrepareDBPassword(ctx, dom); dbsCode(err) != CodeSiteSuspended {
		t.Errorf("passwd site khoá: %v", err)
	}
	if _, err := de.b.PrepareDBImport(ctx, dom, "x"); dbsCode(err) != CodeSiteSuspended {
		t.Errorf("import site khoá: %v", err)
	}
	set(func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	if _, err := de.b.PrepareDBPassword(ctx, dom); dbsCode(err) != CodeDBSuspended {
		t.Errorf("passwd DB khoá: %v", err)
	}
	if _, err := de.b.PrepareDBImport(ctx, dom, "x"); dbsCode(err) != CodeDBSuspended {
		t.Errorf("import DB khoá: %v", err)
	}
	// Xuất vẫn được (root, không qua user DB).
	if _, err := de.b.PrepareDBDump(ctx, dom); err != nil {
		t.Errorf("dump khi DB khoá: %v", err)
	}
}
