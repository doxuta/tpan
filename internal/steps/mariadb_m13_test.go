package steps

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// sdbFake là FakeSystem cộng một MariaDB giả có trạng thái cho các Step của
// M13: DB (charset, collation, nội dung), user (mật khẩu, khoá), kết nối đang
// mở. Câu SQL lạ thì lỗi, để Step đổi câu mà quên test thì test thấy.
type sdbFake struct {
	*system.FakeSystem

	mu    sync.Mutex
	dbs   map[string]sdbDB
	users map[string]sdbUser
	conns map[uint64]string // id → user
	// gone: id vẫn hiện trong PROCESSLIST nhưng KILL báo 1094 (đã tự đóng).
	gone   map[uint64]bool
	failOn []string // câu bắt đầu bằng chuỗi này thì lỗi
	// unlockOnAlter: ALTER USER ... IDENTIFIED BY làm mở khoá (để thử nhánh khoá lại).
	unlockOnAlter bool
}

type sdbDB struct{ cs, coll, data string }
type sdbUser struct {
	pw     string
	locked bool
	grant  string
}

var (
	sdbReState   = regexp.MustCompile(`^SELECT \(SELECT COUNT\(\*\) FROM information_schema\.SCHEMATA WHERE SCHEMA_NAME='([a-z0-9_]+)'\), \(SELECT COUNT\(\*\) FROM mysql\.global_priv WHERE User='([a-z0-9_]+)' AND Host='localhost'\);$`)
	sdbReLockSt  = regexp.MustCompile(`^SELECT COUNT\(\*\), COALESCE\(MAX\(JSON_VALUE\(Priv, '\$\.account_locked'\) IN \('1', 'true'\)\), 0\) FROM mysql\.global_priv WHERE User='([a-z0-9_]+)' AND Host='localhost';$`)
	sdbReSchema  = regexp.MustCompile(`^SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema\.SCHEMATA WHERE SCHEMA_NAME='([a-z0-9_]+)';$`)
	sdbReConns   = regexp.MustCompile(`^SELECT ID FROM information_schema\.PROCESSLIST WHERE USER='([a-z0-9_]+)';$`)
	sdbReMeasure = regexp.MustCompile(`^SELECT COALESCE\(SUM\(DATA_LENGTH \+ INDEX_LENGTH\), 0\) FROM information_schema\.TABLES WHERE TABLE_SCHEMA='([a-z0-9_]+)';$`)
	sdbReCreate  = regexp.MustCompile("^CREATE DATABASE `([a-z0-9_]+)` CHARACTER SET ([a-z0-9_]+) COLLATE ([a-z0-9_]+);$")
	sdbReAlterDB = regexp.MustCompile("^ALTER DATABASE `([a-z0-9_]+)` CHARACTER SET ([a-z0-9_]+) COLLATE ([a-z0-9_]+);$")
	sdbReLock    = regexp.MustCompile(`^ALTER USER '([a-z0-9_]+)'@'localhost' ACCOUNT (LOCK|UNLOCK);$`)
	sdbRePw      = regexp.MustCompile(`^ALTER USER '([a-z0-9_]+)'@'localhost' IDENTIFIED BY '([A-Za-z0-9]+)';$`)
	sdbReKill    = regexp.MustCompile(`^KILL CONNECTION ([0-9]+);$`)
	sdbReDump    = regexp.MustCompile(`^--result-file=(.+)$`)
)

func newSDBFake() *sdbFake {
	f := &sdbFake{
		FakeSystem: system.NewFake(),
		dbs:        map[string]sdbDB{}, users: map[string]sdbUser{},
		conns: map[uint64]string{}, gone: map[uint64]bool{},
	}
	f.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 80 << 30})
	fail := func(s string) system.Result { return system.Result{ExitCode: 1, Stderr: s} }
	f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out strings.Builder
		for st := range strings.SplitSeq(strings.TrimSpace(string(c.Stdin)), "\n") {
			if slices.ContainsFunc(f.failOn, func(p string) bool { return strings.HasPrefix(st, p) }) {
				return fail("ERROR 1064 (42000) at line 1: You have an error in your SQL syntax near '" + st + "'")
			}
			switch {
			case sdbReState.MatchString(st):
				m := sdbReState.FindStringSubmatch(st)
				_, d := f.dbs[m[1]]
				_, u := f.users[m[2]]
				fmt.Fprintf(&out, "%d\t%d\n", b2i(d), b2i(u))
			case sdbReLockSt.MatchString(st):
				u, ok := f.users[sdbReLockSt.FindStringSubmatch(st)[1]]
				fmt.Fprintf(&out, "%d\t%d\n", b2i(ok), b2i(u.locked))
			case sdbReSchema.MatchString(st):
				if d, ok := f.dbs[sdbReSchema.FindStringSubmatch(st)[1]]; ok {
					out.WriteString(d.cs + "\t" + d.coll + "\n")
				}
			case sdbReConns.MatchString(st):
				name := sdbReConns.FindStringSubmatch(st)[1]
				for _, id := range slices.Sorted(maps.Keys(f.conns)) {
					if f.conns[id] == name {
						fmt.Fprintf(&out, "%d\n", id)
					}
				}
			case sdbReMeasure.MatchString(st):
				out.WriteString("4096\n")
			case sdbReCreate.MatchString(st):
				m := sdbReCreate.FindStringSubmatch(st)
				if _, ok := f.dbs[m[1]]; ok {
					return fail("ERROR 1007 (HY000): Can't create database '" + m[1] + "'; database exists")
				}
				f.dbs[m[1]] = sdbDB{cs: m[2], coll: m[3]}
			case sdbReAlterDB.MatchString(st):
				m := sdbReAlterDB.FindStringSubmatch(st)
				d, ok := f.dbs[m[1]]
				if !ok {
					return fail("ERROR 1049 (42000): Unknown database '" + m[1] + "'")
				}
				d.cs, d.coll = m[2], m[3]
				f.dbs[m[1]] = d
			case sdbReLock.MatchString(st):
				m := sdbReLock.FindStringSubmatch(st)
				u, ok := f.users[m[1]]
				if !ok {
					return fail("ERROR 1396 (HY000): Operation ALTER USER failed")
				}
				u.locked = m[2] == "LOCK"
				f.users[m[1]] = u
			case sdbRePw.MatchString(st):
				m := sdbRePw.FindStringSubmatch(st)
				u, ok := f.users[m[1]]
				if !ok {
					return fail("ERROR 1396 (HY000): Operation ALTER USER failed")
				}
				u.pw = m[2]
				if f.unlockOnAlter {
					u.locked = false
				}
				f.users[m[1]] = u
			case sdbReKill.MatchString(st):
				id, _ := strconv.ParseUint(sdbReKill.FindStringSubmatch(st)[1], 10, 64)
				if f.gone[id] {
					delete(f.conns, id)
					return fail(fmt.Sprintf("ERROR 1094 (HY000) at line 1: Unknown thread id: %d", id))
				}
				delete(f.conns, id)
			case dbFakeCrUser.MatchString(st):
				m := dbFakeCrUser.FindStringSubmatch(st)
				if _, ok := f.users[m[1]]; ok {
					return fail("ERROR 1396 (HY000): Operation CREATE USER failed")
				}
				f.users[m[1]] = sdbUser{pw: m[2]}
			case dbFakeGrant.MatchString(st):
				m := dbFakeGrant.FindStringSubmatch(st)
				u, ok := f.users[m[2]]
				if !ok {
					return fail("ERROR 1133 (28000): Can't find any matching row in the user table")
				}
				u.grant = m[1]
				f.users[m[2]] = u
			case dbFakeDrUser.MatchString(st):
				delete(f.users, dbFakeDrUser.FindStringSubmatch(st)[1])
			case dbFakeDrDB.MatchString(st):
				delete(f.dbs, dbFakeDrDB.FindStringSubmatch(st)[1])
			default:
				return fail("sdbFake: câu SQL lạ: " + st)
			}
		}
		return system.Result{Stdout: out.String()}
	})
	// mariadb-dump --single-transaction --quick --result-file=<p> <db>
	f.Handle([]string{"mariadb-dump"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		if slices.Contains(f.failOn, "mariadb-dump") {
			return fail("mariadb-dump: Got error: 2002")
		}
		var p string
		for _, a := range c.Args {
			if m := sdbReDump.FindStringSubmatch(a); m != nil {
				p = m[1]
			}
		}
		db := c.Args[len(c.Args)-1]
		d, ok := f.dbs[db]
		if !ok {
			return fail("mariadb-dump: Got error: 1049: Unknown database '" + db + "'")
		}
		f.SetFile(p, []byte("-- data:"+d.data+"\n-- Dump completed on 2026-11-20 10:00:00\n"), 0o600, system.Root)
		return system.Result{}
	})
	f.Handle([]string{"tail", "-n", "1", "--"}, func(c system.Cmd) system.Result {
		data, err := f.ReadFile(c.Args[3])
		if err != nil {
			return fail("tail: cannot open")
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		return system.Result{Stdout: lines[len(lines)-1] + "\n"}
	})
	// mariadb --database=<db> --abort-source-on-error -e "source <file>"
	f.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		if slices.Contains(f.failOn, "source") {
			return fail("ERROR 1064 (42000) at line 12 in file")
		}
		db := strings.TrimPrefix(c.Args[0], "--database=")
		file := strings.TrimPrefix(c.Args[3], "source ")
		raw, err := f.ReadFile(file)
		d, ok := f.dbs[db]
		if err != nil || !ok {
			return fail("ERROR: cannot import")
		}
		d.data = strings.TrimPrefix(strings.SplitN(string(raw), "\n", 2)[0], "-- data:")
		f.dbs[db] = d
		return system.Result{}
	})
	return f
}

// whole: hệ thống file giả cộng trạng thái MariaDB giả, để so trước và sau.
func (f *sdbFake) whole() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []any{f.Snapshot(), maps.Clone(f.dbs), maps.Clone(f.users), maps.Clone(f.conns)}
}

func (f *sdbFake) set(fn func(f *sdbFake)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func sdbRun(t *testing.T, f *sdbFake, s engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(f.FakeSystem, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: []engine.Step{s}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

// sdbMutating là các lệnh làm đổi trạng thái (mọi thứ trừ câu SELECT và tail).
func sdbMutating(ran []system.Cmd) []string {
	var out []string
	for _, c := range ran {
		if c.Name == "tail" || (c.Name == "mariadb" && strings.HasPrefix(string(c.Stdin), "SELECT ")) {
			continue
		}
		out = append(out, c.String()+" "+string(c.Stdin))
	}
	return out
}

const sdbName = "alice_shop"

func sdbPw() system.Secret { return system.NewSecret("Abcdefghijklmnopqrstuvwxyz012345") }

// sdbCase là một Step cùng trạng thái đầu cần cho nó.
type sdbCase struct {
	name  string
	setup func(f *sdbFake)
	step  func() engine.Step
	after func(t *testing.T, f *sdbFake) // kiểm thêm sau Apply
}

func sdbWithDB(f *sdbFake) {
	f.dbs[sdbName] = sdbDB{cs: "utf8mb4", coll: "utf8mb4_general_ci", data: "rows"}
	f.users[sdbName] = sdbUser{pw: "Old0000000000000000000000000000x", grant: `alice\_shop`}
}

func sdbCases() []sdbCase {
	return []sdbCase{
		{"StandaloneDB", func(*sdbFake) {}, func() engine.Step {
			return &StandaloneDB{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci", Password: sdbPw()}
		}, func(t *testing.T, f *sdbFake) {
			if d := f.dbs[sdbName]; d.coll != "utf8mb4_unicode_ci" {
				t.Errorf("collation = %q", d.coll)
			}
			if u := f.users[sdbName]; u.grant != `alice\_shop` || u.pw != sdbPw().Reveal() {
				t.Errorf("user = %+v (GRANT phải thoát dấu _)", u)
			}
		}},
		{"DBCharset", sdbWithDB, func() engine.Step {
			return &DBCharset{Name: sdbName, Charset: "latin1", Collation: "latin1_swedish_ci"}
		}, func(t *testing.T, f *sdbFake) {
			if d := f.dbs[sdbName]; d.cs != "latin1" || d.coll != "latin1_swedish_ci" {
				t.Errorf("DB = %+v", d)
			}
		}},
		{"DBAccountLock", sdbWithDB, func() engine.Step { return &DBAccountLock{Name: sdbName} },
			func(t *testing.T, f *sdbFake) {
				if !f.users[sdbName].locked {
					t.Error("user chưa khoá")
				}
			}},
		{"DBAccountUnlock", func(f *sdbFake) {
			sdbWithDB(f)
			u := f.users[sdbName]
			u.locked = true
			f.users[sdbName] = u
		}, func() engine.Step { return &DBAccountUnlock{Name: sdbName} },
			func(t *testing.T, f *sdbFake) {
				if f.users[sdbName].locked {
					t.Error("user vẫn khoá")
				}
			}},
		{"DBTrashDump", func(f *sdbFake) {
			sdbWithDB(f)
			// Bản cũ còn sót: phải bị ghi đè, không được coi là đã xuất.
			f.SetFile(account.DBTrashDump(sdbName), []byte("-- data:old\n-- Dump completed\n"), 0o600, system.Root)
		}, func() engine.Step { return &DBTrashDump{Name: sdbName} },
			func(t *testing.T, f *sdbFake) {
				data, _ := f.ReadFile(account.DBTrashDump(sdbName))
				if !strings.HasPrefix(string(data), "-- data:rows\n") {
					t.Errorf("file xuất = %q, muốn bản mới", data)
				}
			}},
		{"DBDrop", func(f *sdbFake) {
			sdbWithDB(f)
			f.SetFile(account.DBTrashDump(sdbName), []byte("-- data:rows\n-- Dump completed\n"), 0o600, system.Root)
		}, func() engine.Step { return &DBDrop{Name: sdbName} },
			func(t *testing.T, f *sdbFake) {
				if _, ok := f.dbs[sdbName]; ok {
					t.Error("DB còn")
				}
				if _, ok := f.users[sdbName]; !ok {
					t.Error("DROP DATABASE không được xoá user DB")
				}
			}},
		{"DBRestore", func(f *sdbFake) {
			f.users[sdbName] = sdbUser{pw: "x", locked: true}
			f.SetFile(account.DBTrashDump(sdbName), []byte("-- data:rows\n-- Dump completed\n"), 0o600, system.Root)
		}, func() engine.Step {
			return &DBRestore{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin"}
		}, func(t *testing.T, f *sdbFake) {
			if d := f.dbs[sdbName]; d.data != "rows" || d.coll != "utf8mb4_bin" {
				t.Errorf("DB = %+v", d)
			}
		}},
	}
}

func TestDBM13StepsLifecycle(t *testing.T) {
	for _, c := range sdbCases() {
		t.Run(c.name, func(t *testing.T) {
			f := newSDBFake()
			f.set(c.setup)
			before := f.whole()

			s := c.step()
			if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
			}
			if ok, err := s.Check(context.Background(), f); !ok || err != nil {
				t.Fatalf("Check sau Apply = %v, %v", ok, err)
			}
			c.after(t, f)

			// Hoàn tác đưa về đúng như trước.
			if err := s.Undo(context.Background(), f); err != nil {
				t.Fatalf("Undo: %v", err)
			}
			if got := f.whole(); !reflect.DeepEqual(got, before) && c.name != "DBTrashDump" {
				t.Errorf("Undo chưa trả về như cũ:\n%v\nmuốn\n%v", got, before)
			}
		})
	}
}

// Undo của DBTrashDump xoá file nó vừa ghi (kể cả khi đè lên bản cũ sót lại:
// bản cũ đó không bao giờ là dữ liệu cần giữ).
func TestDBTrashDumpUndoRemoves(t *testing.T) {
	f := newSDBFake()
	f.set(sdbWithDB)
	s := &DBTrashDump{Name: sdbName}
	sdbRun(t, f, s)
	// Máy sạch: thư mục thùng rác DB do bước tạo, 0700; /var/lib/tpan giữ 0755 (acme).
	for p, want := range map[string]fs.FileMode{"/var/lib/tpan/trash/db": 0o700, "/var/lib/tpan/trash": 0o700, "/var/lib/tpan": 0o755} {
		if fi, err := f.Stat(p); err != nil || fi.Mode.Perm() != want {
			t.Errorf("%s: %v %v, muốn quyền %v", p, fi.Mode.Perm(), err, want)
		}
	}
	if err := s.Undo(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat(account.DBTrashDump(sdbName)); err == nil {
		t.Error("Undo không xoá file xuất")
	}
}

// Chạy lại các bước có trạng thái đọc được: không lệnh thay đổi nào.
func TestDBM13StepsRerunNoop(t *testing.T) {
	for _, c := range sdbCases() {
		switch c.name {
		case "DBTrashDump", "DBRestore":
			continue // Check chỉ true sau Apply của chính bước (cố ý)
		}
		t.Run(c.name, func(t *testing.T) {
			f := newSDBFake()
			f.set(c.setup)
			sdbRun(t, f, c.step())
			f.ResetRan()
			rep := sdbRun(t, f, c.step())
			if rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("chạy lại: %s", rep.Job.Error)
			}
			if m := sdbMutating(f.Ran()); len(m) != 0 {
				t.Errorf("chạy lại vẫn đổi máy: %v", m)
			}
		})
	}
}

func TestDBM13StepsCommandErrors(t *testing.T) {
	cases := []struct {
		name   string
		failOn string
		step   engine.Step
		setup  func(f *sdbFake)
		want   string
	}{
		{"tạo DB", "CREATE DATABASE", &StandaloneDB{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin", Password: sdbPw()}, func(*sdbFake) {}, "tạo DB " + sdbName},
		{"charset", "ALTER DATABASE", &DBCharset{Name: sdbName, Charset: "latin1", Collation: "latin1_bin"}, sdbWithDB, "đổi charset của DB " + sdbName},
		{"khoá", "ALTER USER", &DBAccountLock{Name: sdbName}, sdbWithDB, "khoá user DB " + sdbName},
		{"đổi mật khẩu", "ALTER USER", &DBUserPassword{Name: sdbName, Password: sdbPw()}, sdbWithDB, "đổi mật khẩu user DB " + sdbName},
		{"xuất", "mariadb-dump", &DBTrashDump{Name: sdbName}, sdbWithDB, "xuất MariaDB ra"},
		{"xoá user", "DROP USER", &DBUserAbsent{Name: sdbName}, sdbWithDB, "xoá user DB " + sdbName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newSDBFake()
			f.set(c.setup)
			f.set(func(f *sdbFake) { f.failOn = []string{c.failOn} })
			err := c.step.Apply(context.Background(), f)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("lỗi = %v, muốn có %q", err, c.want)
			}
			if strings.Contains(err.Error(), sdbPw().Reveal()) {
				t.Errorf("lỗi lộ mật khẩu: %v", err)
			}
		})
	}
}

// Tạo user lỗi thì DB vừa tạo bị xoá lại (engine không Undo bước có Apply lỗi),
// và lỗi không chép lại mật khẩu dù stderr của mariadb có nó.
func TestDBStandaloneRollbackAndRedact(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) { f.failOn = []string{"CREATE USER"} })
	before := f.whole()
	err := (&StandaloneDB{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin", Password: sdbPw()}).Apply(context.Background(), f)
	if err == nil || strings.Contains(err.Error(), sdbPw().Reveal()) {
		t.Fatalf("lỗi = %v", err)
	}
	if !reflect.DeepEqual(f.whole(), before) {
		t.Error("DB vừa tạo chưa bị xoá lại")
	}
	for _, c := range f.Ran() {
		if strings.Contains(c.String(), sdbPw().Reveal()) {
			t.Errorf("mật khẩu nằm trên dòng lệnh: %s", c.String())
		}
	}
	if strings.Contains(fmt.Sprint((&StandaloneDB{Name: sdbName, Charset: "a", Collation: "b", Password: sdbPw()}).Info()), sdbPw().Reveal()) {
		t.Error("Info lộ mật khẩu")
	}
}

// DB hay user đã có (không do bước tạo) thì Apply từ chối, không đụng gì.
func TestDBStandaloneRefusesForeign(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) { f.users[sdbName] = sdbUser{pw: "x"} })
	before := f.whole()
	s := &StandaloneDB{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin", Password: sdbPw()}
	if err := s.Apply(context.Background(), f); err == nil {
		t.Fatal("user đã có mà vẫn tạo")
	}
	if !reflect.DeepEqual(f.whole(), before) {
		t.Error("đã đụng vào thứ có sẵn")
	}
}

func TestDBKillConnections(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) {
		sdbWithDB(f)
		f.conns = map[uint64]string{7: sdbName, 9: sdbName, 11: "other"}
		f.gone[9] = true // tự đóng giữa lúc đọc và lúc KILL: 1094 là đã xong
	})
	s := &DBKillConnections{Name: sdbName}
	if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %s", rep.Job.Error)
	}
	if !reflect.DeepEqual(f.conns, map[uint64]string{11: "other"}) {
		t.Errorf("kết nối còn lại = %v, muốn chỉ của user khác", f.conns)
	}
	var kills []string
	for _, c := range f.Ran() {
		if strings.HasPrefix(string(c.Stdin), "KILL") {
			kills = append(kills, strings.TrimSpace(string(c.Stdin)))
		}
	}
	if !reflect.DeepEqual(kills, []string{"KILL CONNECTION 7;", "KILL CONNECTION 9;"}) {
		t.Errorf("KILL = %v, muốn mỗi id một lần gọi", kills)
	}
}

func TestDBKillConnectionsStuck(t *testing.T) {
	defer func(w, e time.Duration) { sdbKillWait, sdbKillEvery = w, e }(sdbKillWait, sdbKillEvery)
	sdbKillWait, sdbKillEvery = 20*time.Millisecond, time.Millisecond
	f := newSDBFake()
	f.set(func(f *sdbFake) { f.conns = map[uint64]string{7: sdbName} })
	// KILL "thành công" mà luồng không thoát.
	f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		if strings.HasPrefix(string(c.Stdin), "KILL") {
			return system.Result{}
		}
		return system.Result{Stdout: "7\n"}
	})
	err := (&DBKillConnections{Name: sdbName}).Apply(context.Background(), f)
	if err == nil || !strings.Contains(err.Error(), "vẫn còn 1 kết nối") {
		t.Fatalf("lỗi = %v", err)
	}
}

// Id lạ trong PROCESSLIST không bao giờ đi vào câu KILL.
func TestDBKillConnectionsBadID(t *testing.T) {
	f := newSDBFake()
	f.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "7; DROP DATABASE x\n"}
	})
	if err := (&DBKillConnections{Name: sdbName}).Apply(context.Background(), f); err == nil {
		t.Fatal("id lạ mà không lỗi")
	}
	for _, c := range f.Ran() {
		if strings.Contains(string(c.Stdin), "KILL") {
			t.Errorf("đã chạy KILL: %s", c.Stdin)
		}
	}
}

// Khoá user không có là đạt (như DBUserLock của M6); mở khoá user không có là lỗi.
func TestDBAccountLockMissingUser(t *testing.T) {
	f := newSDBFake()
	if ok, err := (&DBAccountLock{Name: sdbName}).Check(context.Background(), f); !ok || err != nil {
		t.Errorf("khoá user không có: Check = %v, %v", ok, err)
	}
	if err := (&DBAccountUnlock{Name: sdbName}).Apply(context.Background(), f); err == nil {
		t.Error("mở khoá user không có mà không lỗi")
	}
}

// Undo của khoá không mở một user vốn đã khoá từ trước.
func TestDBAccountLockAlreadyLocked(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) { f.users[sdbName] = sdbUser{locked: true} })
	s := &DBAccountLock{Name: sdbName}
	if err := s.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if err := s.Undo(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if !f.users[sdbName].locked {
		t.Error("Undo đã mở một user vốn khoá")
	}
}

func TestDBUserPassword(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) {
		sdbWithDB(f)
		u := f.users[sdbName]
		u.locked = true
		f.users[sdbName] = u
		f.unlockOnAlter = true
	})
	s := &DBUserPassword{Name: sdbName, Password: sdbPw()}
	if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %s", rep.Job.Error)
	}
	if u := f.users[sdbName]; u.pw != sdbPw().Reveal() || !u.locked {
		t.Errorf("user = %+v: muốn mật khẩu mới và vẫn khoá", u)
	}
	for _, c := range f.Ran() {
		if strings.Contains(c.String(), sdbPw().Reveal()) {
			t.Errorf("mật khẩu trên dòng lệnh: %s", c.String())
		}
	}
	if info := fmt.Sprint(s.Info()); strings.Contains(info, sdbPw().Reveal()) || !s.Info().Danger {
		t.Error("Info lộ mật khẩu hoặc thiếu Danger")
	}
}

// Không có bản xuất trọn vẹn thì không DROP.
func TestDBDropNeedsDump(t *testing.T) {
	for _, dump := range []string{"", "-- data:rows\n-- half"} {
		f := newSDBFake()
		f.set(sdbWithDB)
		if dump != "" {
			f.SetFile(account.DBTrashDump(sdbName), []byte(dump), 0o600, system.Root)
		}
		if err := (&DBDrop{Name: sdbName}).Apply(context.Background(), f); err == nil {
			t.Errorf("file %q: DROP mà không có bản xuất trọn vẹn", dump)
		}
		if _, ok := f.dbs[sdbName]; !ok {
			t.Errorf("file %q: DB đã bị xoá", dump)
		}
	}
}

// Nhập lỗi thì DB vừa tạo bị xoá, file xuất còn nguyên.
func TestDBRestoreImportFails(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) {
		f.SetFile(account.DBTrashDump(sdbName), []byte("-- data:rows\n-- Dump completed\n"), 0o600, system.Root)
		f.failOn = []string{"source"}
	})
	err := (&DBRestore{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin"}).Apply(context.Background(), f)
	if err == nil || !strings.Contains(err.Error(), "nhập "+account.DBTrashDump(sdbName)) {
		t.Fatalf("lỗi = %v", err)
	}
	if _, ok := f.dbs[sdbName]; ok {
		t.Error("DB nhập dở còn lại")
	}
	if _, err := f.Stat(account.DBTrashDump(sdbName)); err != nil {
		t.Error("file xuất bị mất")
	}
	// DB đã có thì không nhập đè.
	f.set(func(f *sdbFake) { f.failOn = nil; sdbWithDB(f) })
	if err := (&DBRestore{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin"}).Apply(context.Background(), f); err == nil {
		t.Error("nhập đè lên DB đã có")
	}
}

func TestDBTrashDumpAbsentAndUserAbsent(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) {
		sdbWithDB(f)
		f.SetFile(account.DBTrashDump(sdbName), []byte("x"), 0o600, system.Root)
	})
	for _, s := range []engine.Step{&DBTrashDumpAbsent{Name: sdbName}, &DBUserAbsent{Name: sdbName}} {
		if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s: %s", s.Info().ID, rep.Job.Error)
		}
		if !s.Info().Danger || s.Info().NoUndo.Vi == "" {
			t.Errorf("%s: xoá hẳn phải có Danger và NoUndo", s.Info().ID)
		}
	}
	if _, ok := f.users[sdbName]; ok {
		t.Error("user DB còn")
	}
	if (&DBTrashDumpAbsent{Name: sdbName, Restored: true}).Info().Danger {
		t.Error("xoá file sau khôi phục không phải bước nguy hiểm")
	}
}

// Tên, charset lạ (chuỗi tấn công) bị từ chối trước khi có lệnh nào chạy.
func TestDBM13StepsRejectBadInput(t *testing.T) {
	bad := []string{"a; rm -rf /", "$(id)", "../../etc/passwd", "a\nb", strings.Repeat("a", 300), "tên", "", "a'b", "a`b"}
	for _, n := range bad {
		f := newSDBFake()
		ss := []engine.Step{
			&StandaloneDB{Name: n, Charset: "utf8mb4", Collation: "utf8mb4_bin", Password: sdbPw()},
			&StandaloneDB{Name: sdbName, Charset: n, Collation: "utf8mb4_bin", Password: sdbPw()},
			&DBCharset{Name: sdbName, Charset: "utf8mb4", Collation: n},
			&DBAccountLock{Name: n}, &DBKillConnections{Name: n}, &DBAccountUnlock{Name: n},
			&DBUserPassword{Name: n, Password: sdbPw()}, &DBTrashDump{Name: n}, &DBDrop{Name: n},
			&DBRestore{Name: n, Charset: "utf8mb4", Collation: "utf8mb4_bin"},
			&DBTrashDumpAbsent{Name: n}, &DBUserAbsent{Name: n},
		}
		for _, s := range ss {
			if err := s.Apply(context.Background(), f); err == nil {
				t.Errorf("%T nhận %q", s, n)
			}
		}
		if len(f.Ran()) != 0 {
			t.Errorf("%q: đã chạy lệnh %v", n, f.Ran())
		}
	}
	if err := (&DBUserPassword{Name: sdbName, Password: system.NewSecret("short")}).Apply(context.Background(), newSDBFake()); err == nil {
		t.Error("mật khẩu ngắn được nhận")
	}
}

func TestDBNameStateAndCharsetOf(t *testing.T) {
	f := newSDBFake()
	if db, u, err := DBNameState(context.Background(), f, sdbName); db || u || err != nil {
		t.Errorf("máy sạch: %v %v %v", db, u, err)
	}
	f.set(sdbWithDB)
	if db, u, err := DBNameState(context.Background(), f, sdbName); !db || !u || err != nil {
		t.Errorf("đã có: %v %v %v", db, u, err)
	}
	cs, coll, ok, err := DBCharsetOf(context.Background(), f, sdbName)
	if cs != "utf8mb4" || coll != "utf8mb4_general_ci" || !ok || err != nil {
		t.Errorf("DBCharsetOf = %q %q %v %v", cs, coll, ok, err)
	}
	if _, _, ok, _ := DBCharsetOf(context.Background(), f, "nope"); ok {
		t.Error("DB không có mà ok")
	}
}
