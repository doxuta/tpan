package steps

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// dbFake là FakeSystem cộng một MariaDB giả có trạng thái: đọc từng câu SQL
// trong stdin, chạy lần lượt và dừng ở câu lỗi đầu tiên như client mariadb.
// Tạo thứ đã có thì lỗi (như MariaDB thật), để lỡ Step bỏ mất kiểm trạng thái
// hay dùng IF NOT EXISTS thì test thấy.
type dbFake struct {
	*system.FakeSystem

	mu     sync.Mutex
	dbs    map[string]bool
	users  map[string]string // tên user → mật khẩu
	grants map[string]string // tên user → tên DB trong câu GRANT (đã thoát)
	// failOn: câu bắt đầu bằng một trong các chuỗi này thì lỗi, stderr chép lại
	// câu đó (giống lỗi cú pháp của MariaDB chép đoạn SQL quanh chỗ sai).
	failOn []string
	// stateOut khác rỗng: câu đọc trạng thái trả đúng chuỗi này.
	stateOut string
}

// M28 đợt 2: user mới phải có trần kết nối (dbFakeCrUser, site.DBMaxUserConnections = 20).
var (
	dbFakeState  = regexp.MustCompile(`^SELECT \(SELECT COUNT\(\*\) FROM information_schema\.SCHEMATA WHERE SCHEMA_NAME='([a-z0-9_]+)'\), \(SELECT COUNT\(\*\) FROM mysql\.global_priv WHERE User='([a-z0-9_]+)' AND Host='localhost'\);$`)
	dbFakeCrDB   = regexp.MustCompile("^CREATE DATABASE `([a-z0-9_]+)` CHARACTER SET utf8mb4;$")
	dbFakeCrUser = regexp.MustCompile(`^CREATE USER '([a-z0-9_]+)'@'localhost' IDENTIFIED BY '([A-Za-z0-9]+)' WITH MAX_USER_CONNECTIONS 20;$`)
	dbFakeGrant  = regexp.MustCompile("^GRANT ALL PRIVILEGES ON `([a-z0-9_\\\\]+)`\\.\\* TO '([a-z0-9_]+)'@'localhost';$")
	dbFakeDrUser = regexp.MustCompile(`^DROP USER IF EXISTS '([a-z0-9_]+)'@'localhost';$`)
	dbFakeDrDB   = regexp.MustCompile("^DROP DATABASE IF EXISTS `([a-z0-9_]+)`;$")
)

func newDBFake() *dbFake {
	f := &dbFake{FakeSystem: system.NewFake(), dbs: map[string]bool{}, users: map[string]string{}, grants: map[string]string{}}
	fail := func(stderr string) system.Result { return system.Result{ExitCode: 1, Stderr: stderr} }
	f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out strings.Builder
		for line, st := range strings.Split(strings.TrimSpace(string(c.Stdin)), "\n") {
			if slices.ContainsFunc(f.failOn, func(p string) bool { return strings.HasPrefix(st, p) }) {
				return fail(fmt.Sprintf("ERROR 1064 (42000) at line %d: You have an error in your SQL syntax near '%s'", line+1, st))
			}
			if m := dbFakeState.FindStringSubmatch(st); m != nil {
				if f.stateOut != "" {
					out.WriteString(f.stateOut)
					continue
				}
				_, u := f.users[m[2]]
				fmt.Fprintf(&out, "%d\t%d\n", b2i(f.dbs[m[1]]), b2i(u))
			} else if m := dbFakeCrDB.FindStringSubmatch(st); m != nil {
				if f.dbs[m[1]] {
					return fail("ERROR 1007 (HY000): Can't create database '" + m[1] + "'; database exists")
				}
				f.dbs[m[1]] = true
			} else if m := dbFakeCrUser.FindStringSubmatch(st); m != nil {
				if _, ok := f.users[m[1]]; ok {
					return fail("ERROR 1396 (HY000): Operation CREATE USER failed for '" + m[1] + "'@'localhost'")
				}
				f.users[m[1]] = m[2]
			} else if m := dbFakeGrant.FindStringSubmatch(st); m != nil {
				if _, ok := f.users[m[2]]; !ok {
					return fail("ERROR 1133 (28000): Can't find any matching row in the user table")
				}
				f.grants[m[2]] = m[1]
			} else if m := dbFakeDrUser.FindStringSubmatch(st); m != nil {
				delete(f.users, m[1])
				delete(f.grants, m[1])
			} else if m := dbFakeDrDB.FindStringSubmatch(st); m != nil {
				delete(f.dbs, m[1])
			} else {
				return fail("dbFake: câu SQL lạ: " + st)
			}
		}
		return system.Result{Stdout: out.String()}
	})
	return f
}

// whole là hệ thống file giả cộng trạng thái MariaDB giả, để so trước và sau.
func (f *dbFake) whole() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []any{f.Snapshot(), maps.Clone(f.dbs), maps.Clone(f.users), maps.Clone(f.grants)}
}

func (f *dbFake) set(fn func(f *dbFake)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// dbReadOnly: chỉ câu đọc trạng thái được chạy khi engine bỏ qua bước đã xong.
func dbReadOnly(c system.Cmd) bool {
	return c.Name == "mariadb" && strings.HasPrefix(string(c.Stdin), "SELECT ")
}

func dbExecute(t *testing.T, f *dbFake, s engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(f.FakeSystem, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: []engine.Step{s}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

const dbTestPassword = "Zq8LmN3pRt5VwX7yB2cD4fG6hJ9k" // 28 ký tự, chỉ dùng trong test

func dbLayout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("shop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

func dbStep(t *testing.T) *SiteDB {
	return &SiteDB{Layout: dbLayout(t), Password: system.NewSecret(dbTestPassword)}
}

func TestSiteDBLifecycle(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)

	t.Run("máy sạch rồi Apply thì Check true", func(t *testing.T) {
		f := newDBFake()
		s := dbStep(t)
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v; muốn false, nil", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
		}
		// Quyền cấp trên đúng tên DB, "_" đã thoát để không thành ký tự đại diện.
		if f.users[l.DB] != dbTestPassword || f.grants[l.DB] != strings.ReplaceAll(l.DB, "_", `\_`) {
			t.Errorf("user hoặc quyền sai: users %v, grants %v", f.users, f.grants)
		}
	})

	t.Run("chạy lại thì không lệnh thay đổi nào chạy", func(t *testing.T) {
		f := newDBFake()
		if rep := dbExecute(t, f, dbStep(t)); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := f.whole()
		f.ResetRan()
		rep := dbExecute(t, f, dbStep(t))
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q, muốn skipped (%s)", got, rep.Steps[0].Error)
		}
		for _, c := range f.Ran() {
			if !dbReadOnly(c) {
				t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if !reflect.DeepEqual(before, f.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})

	cases := []struct {
		name  string
		setup func(f *dbFake)
	}{
		{"máy sạch", func(*dbFake) {}},
		// DB có sẵn (không do bước này tạo): Undo chỉ xoá user, giữ DB.
		{"DB có sẵn", func(f *dbFake) { f.dbs[l.DB] = true }},
		// User có sẵn: không đổi mật khẩu của nó, Undo chỉ xoá DB.
		{"user có sẵn", func(f *dbFake) { f.users[l.DB] = "MatKhauCuCuaUserKhacXyz123" }},
	}
	for _, c := range cases {
		t.Run("Apply rồi Undo thì máy về như cũ: "+c.name, func(t *testing.T) {
			f := newDBFake()
			f.set(c.setup)
			before := f.whole()
			s := dbStep(t)
			if err := s.Apply(ctx, f); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if reflect.DeepEqual(before, f.whole()) {
				t.Fatal("Apply không đổi gì, test không kiểm được Undo")
			}
			if err := s.Undo(ctx, f); err != nil {
				t.Fatalf("Undo: %v", err)
			}
			if !reflect.DeepEqual(before, f.whole()) {
				t.Errorf("sau Undo máy không về như cũ:\n%v\nmuốn\n%v", f.whole(), before)
			}
		})
	}

	t.Run("Undo khi chưa Apply", func(t *testing.T) {
		f := newDBFake()
		if err := dbStep(t).Undo(ctx, f); !errors.Is(err, errNotApplied) || len(f.Ran()) != 0 {
			t.Errorf("Undo = %v, lệnh %v; muốn errNotApplied và không lệnh nào", err, ranStrings(f.Ran()))
		}
	})
}

func TestSiteDBCommandErrors(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)

	t.Run("đọc trạng thái lỗi", func(t *testing.T) {
		f := newDBFake()
		f.failOn = []string{"SELECT"}
		_, err := dbStep(t).Check(ctx, f)
		wantExit(t, err, "đọc trạng thái DB của site shop.example.com")
		_, _, err = SiteDBState(ctx, f, l)
		wantExit(t, err, "đọc trạng thái DB")
	})
	t.Run("kết quả lạ", func(t *testing.T) {
		f := newDBFake()
		f.stateOut = "1\n"
		if _, err := dbStep(t).Check(ctx, f); err == nil || !strings.Contains(err.Error(), "kết quả lạ") {
			t.Errorf("Check = %v; muốn lỗi kết quả lạ", err)
		}
	})
	// Lệnh nào lỗi thì Apply tự xoá phần đã tạo: engine không Undo bước có Apply lỗi.
	for _, c := range []struct{ failOn, context string }{
		{"CREATE DATABASE", "tạo DB " + l.DB},
		{"CREATE USER", "tạo user DB " + l.DB},
		{"GRANT", "cấp quyền trên DB " + l.DB},
	} {
		t.Run(c.failOn+" lỗi thì máy về như cũ", func(t *testing.T) {
			f := newDBFake()
			before := f.whole()
			f.failOn = []string{c.failOn}
			wantExit(t, dbStep(t).Apply(ctx, f), c.context)
			if !reflect.DeepEqual(before, f.whole()) {
				t.Errorf("máy không về như cũ: %v", f.whole())
			}
		})
	}
	t.Run("dọn lại cũng lỗi thì gộp cả hai lỗi", func(t *testing.T) {
		f := newDBFake()
		// GRANT lỗi, rồi câu DROP USER của bước dọn cũng lỗi.
		f.failOn = []string{"GRANT", "DROP USER"}
		err := dbStep(t).Apply(ctx, f)
		wantExit(t, err, "cấp quyền")
		if !strings.Contains(err.Error(), "vừa tạo") {
			t.Errorf("lỗi %q thiếu lỗi của bước dọn", err)
		}
	})
	t.Run("SiteDBAbsent: xoá lỗi", func(t *testing.T) {
		f := newDBFake()
		f.set(func(f *dbFake) { f.dbs[l.DB] = true })
		f.failOn = []string{"DROP USER"}
		wantExit(t, (&SiteDBAbsent{Layout: l}).Apply(ctx, f), "xoá DB và user DB của site shop.example.com")
	})
}

// Mật khẩu không được lọt ra bất cứ chuỗi nào người hay log thấy được.
func TestSiteDBNoPasswordLeak(t *testing.T) {
	ctx := context.Background()
	var seen []string
	collect := func(s engine.Step) {
		info := s.Info()
		seen = append(seen, info.ID, info.Title.Vi, info.Title.En, info.Why.Vi, info.Why.En, info.NoUndo.Vi, info.NoUndo.En)
		seen = append(seen, info.Commands...)
		seen = append(seen, fmt.Sprintf("%v %+v %#v", s, s, s))
	}

	// Đường thành công.
	ok := newDBFake()
	s := dbStep(t)
	if err := s.Apply(ctx, ok); err != nil {
		t.Fatal(err)
	}
	collect(s)
	inStdin := false
	for _, c := range ok.Ran() {
		seen = append(seen, c.String())
		inStdin = inStdin || strings.Contains(string(c.Stdin), dbTestPassword)
	}
	if !inStdin {
		t.Fatal("mật khẩu không nằm trong stdin của lệnh nào: test không kiểm được gì")
	}

	// Đường lỗi: stderr của mariadb chép lại câu CREATE USER có mật khẩu.
	bad := newDBFake()
	bad.failOn = []string{"CREATE USER"}
	err := dbStep(t).Apply(ctx, bad)
	if err == nil {
		t.Fatal("muốn lỗi")
	}
	if !strings.Contains(err.Error(), "IDENTIFIED BY '"+dbMask+"'") {
		t.Errorf("lỗi không giữ ngữ cảnh đã che: %v", err)
	}
	wantExit(t, err, "tạo user DB")
	seen = append(seen, err.Error(), fmt.Sprintf("%+v", err))
	var ee *system.ExitError
	if errors.As(err, &ee) {
		seen = append(seen, ee.Stderr, ee.Cmd)
	}
	for _, c := range bad.Ran() {
		seen = append(seen, c.String())
	}

	// Mật khẩu sai cũng không được in ra lỗi.
	wrong := "short'pw" + dbTestPassword[:4]
	_, err = (&SiteDB{Layout: dbLayout(t), Password: system.NewSecret(wrong)}).Check(ctx, newDBFake())
	if err == nil || strings.Contains(err.Error(), wrong) {
		t.Errorf("mật khẩu sai: lỗi %v", err)
	}

	for _, str := range seen {
		if strings.Contains(str, dbTestPassword) {
			t.Errorf("lộ mật khẩu: %q", str)
		}
	}
}

func TestSiteDBBadParams(t *testing.T) {
	ctx := context.Background()
	good := dbLayout(t)
	other, _ := validate.ParseDomain("other.example.com")
	attacks := []string{
		"tp_x`; DROP DATABASE mysql; --",
		"tp_x'@'%",
		"$(id)",
		"../../etc/passwd",
		"tp_x\nDROP USER root",
		"",
		strings.Repeat("a", 65),
		"mysql",
		site.LayoutOf(other).DB, // tên hợp lệ nhưng của site khác
	}
	var layouts []site.Layout
	layouts = append(layouts, site.Layout{})
	for _, a := range attacks {
		l := good
		l.DB = a
		layouts = append(layouts, l)
	}
	for i, l := range layouts {
		f := newDBFake()
		s := &SiteDB{Layout: l, Password: system.NewSecret(dbTestPassword)}
		if _, err := s.Check(ctx, f); err == nil {
			t.Errorf("#%d %q: SiteDB.Check không lỗi", i, l.DB)
		}
		if err := s.Apply(ctx, f); err == nil {
			t.Errorf("#%d %q: SiteDB.Apply không lỗi", i, l.DB)
		}
		if _, _, err := SiteDBState(ctx, f, l); err == nil {
			t.Errorf("#%d %q: SiteDBState không lỗi", i, l.DB)
		}
		a := &SiteDBAbsent{Layout: l}
		if _, err := a.Check(ctx, f); err == nil {
			t.Errorf("#%d %q: SiteDBAbsent.Check không lỗi", i, l.DB)
		}
		if err := a.Apply(ctx, f); err == nil {
			t.Errorf("#%d %q: SiteDBAbsent.Apply không lỗi", i, l.DB)
		}
		_ = s.Info() // không được panic với Layout xấu
		_ = a.Info()
		if len(f.Ran()) != 0 {
			t.Errorf("#%d %q: tham số xấu mà vẫn chạy lệnh %v", i, l.DB, ranStrings(f.Ran()))
		}
	}

	for _, pw := range []string{"", "abc", strings.Repeat("a", 23), dbTestPassword + "'", dbTestPassword + "\n", "Zq8LmN3pRt5V wX7yB2cD4fG6hJ9k", "Mậtkhẩu" + dbTestPassword} {
		f := newDBFake()
		s := &SiteDB{Layout: good, Password: system.NewSecret(pw)}
		if _, err := s.Check(ctx, f); err == nil {
			t.Errorf("mật khẩu %q: Check không lỗi", pw)
		}
		if err := s.Apply(ctx, f); err == nil {
			t.Errorf("mật khẩu %q: Apply không lỗi", pw)
		}
		if len(f.Ran()) != 0 {
			t.Errorf("mật khẩu %q: vẫn chạy lệnh", pw)
		}
	}
}

func TestSiteDBAbsent(t *testing.T) {
	ctx := context.Background()
	l := dbLayout(t)
	withSite := func(f *dbFake) {
		f.dbs[l.DB] = true
		f.users[l.DB] = dbTestPassword
		f.grants[l.DB] = strings.ReplaceAll(l.DB, "_", `\_`)
		f.dbs["tp_khac_123456"] = true // DB của site khác phải còn nguyên
	}

	t.Run("có site rồi Apply thì Check true", func(t *testing.T) {
		f := newDBFake()
		f.set(withSite)
		s := &SiteDBAbsent{Layout: l}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v", done, err)
		}
		if !f.dbs["tp_khac_123456"] || len(f.users) != 0 || len(f.grants) != 0 {
			t.Errorf("xoá sai: dbs %v users %v grants %v", f.dbs, f.users, f.grants)
		}
	})
	t.Run("chỉ còn một nửa (user hoặc DB) thì vẫn xoá", func(t *testing.T) {
		f := newDBFake()
		f.set(func(f *dbFake) { f.users[l.DB] = dbTestPassword })
		s := &SiteDBAbsent{Layout: l}
		if done, _ := s.Check(ctx, f); done {
			t.Fatal("còn user mà Check true")
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v", done, err)
		}
	})
	t.Run("chạy lại thì không lệnh thay đổi nào chạy", func(t *testing.T) {
		f := newDBFake()
		f.set(withSite)
		if rep := dbExecute(t, f, &SiteDBAbsent{Layout: l}); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := f.whole()
		f.ResetRan()
		rep := dbExecute(t, f, &SiteDBAbsent{Layout: l})
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q", got)
		}
		for _, c := range f.Ran() {
			if !dbReadOnly(c) {
				t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if !reflect.DeepEqual(before, f.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})
	t.Run("Danger, NoUndo, Undo không làm gì", func(t *testing.T) {
		s := &SiteDBAbsent{Layout: l}
		info := s.Info()
		if !info.Danger || info.NoUndo.IsZero() {
			t.Errorf("Info: Danger %v, NoUndo %v", info.Danger, info.NoUndo)
		}
		f := newDBFake()
		if err := s.Undo(ctx, f); err != nil || len(f.Ran()) != 0 {
			t.Errorf("Undo = %v, lệnh %v", err, ranStrings(f.Ran()))
		}
	})
}

func TestSiteDBInfo(t *testing.T) {
	l := dbLayout(t)
	db := (&SiteDB{Layout: l, Password: system.NewSecret(dbTestPassword)}).Info()
	ab := (&SiteDBAbsent{Layout: l}).Info()
	if db.ID == ab.ID || !strings.HasSuffix(db.ID, ":shop.example.com") || !strings.HasSuffix(ab.ID, ":shop.example.com") {
		t.Errorf("ID: %q, %q", db.ID, ab.ID)
	}
	for _, info := range []engine.StepInfo{db, ab} {
		for _, txt := range []string{info.Title.Vi, info.Title.En, info.Why.Vi, info.Why.En} {
			if txt == "" {
				t.Errorf("%s: thiếu Title hoặc Why ở một ngôn ngữ", info.ID)
			}
		}
		for _, c := range info.Commands {
			if !strings.HasPrefix(c, "mariadb -N -B  (stdin: ") {
				t.Errorf("%s: lệnh %q không bắt đầu bằng mariadb -N -B", info.ID, c)
			}
		}
	}
	want := []string{
		"mariadb -N -B  (stdin: CREATE DATABASE `" + l.DB + "` CHARACTER SET utf8mb4;)",
		"mariadb -N -B  (stdin: CREATE USER '" + l.DB + "'@'localhost' IDENTIFIED BY '••••' WITH MAX_USER_CONNECTIONS 20;)",
		"mariadb -N -B  (stdin: GRANT ALL PRIVILEGES ON `" + strings.ReplaceAll(l.DB, "_", `\_`) + "`.* TO '" + l.DB + "'@'localhost';)",
	}
	if !reflect.DeepEqual(db.Commands, want) {
		t.Errorf("Commands của SiteDB:\n%q\nmuốn\n%q", db.Commands, want)
	}
	if db.Danger || !db.NoUndo.IsZero() {
		t.Error("SiteDB không xoá dữ liệu và có Undo: không Danger, không NoUndo")
	}
}
