package steps

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// sdbOnlyDB: DB còn (có dữ liệu), user DB đã bị xoá ngoài tpan.
func sdbOnlyDB(f *sdbFake) {
	sdbWithDB(f)
	d := f.dbs[sdbName]
	d.data = "rows"
	f.dbs[sdbName] = d
	delete(f.users, sdbName)
}

func TestDBUserRecreate(t *testing.T) {
	for _, locked := range []bool{false, true} {
		f := newSDBFake()
		f.set(sdbOnlyDB)
		s := &DBUserRecreate{Name: sdbName, Password: sdbPw(), Locked: locked}
		if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("locked=%v: job: %s", locked, rep.Job.Error)
		}
		u, ok := f.users[sdbName]
		if !ok || u.pw != sdbPw().Reveal() || u.locked != locked {
			t.Errorf("locked=%v: user = %+v (có=%v): muốn mật khẩu mới, khoá=%v", locked, u, ok, locked)
		}
		// Quyền trên đúng DB này: dấu "_" trong tên được thoát để không khớp DB khác.
		if want := strings.ReplaceAll(sdbName, "_", `\_`); u.grant != want {
			t.Errorf("locked=%v: quyền cấp trên %q, muốn %q", locked, u.grant, want)
		}
		if d := f.dbs[sdbName]; d.data != "rows" {
			t.Errorf("locked=%v: dữ liệu của DB bị đụng: %+v", locked, d)
		}
		for _, c := range f.Ran() {
			if strings.Contains(c.String(), sdbPw().Reveal()) {
				t.Errorf("mật khẩu trên dòng lệnh: %s", c.String())
			}
		}
		info := s.Info()
		if strings.Contains(fmt.Sprint(info), sdbPw().Reveal()) {
			t.Error("Info lộ mật khẩu")
		}
		if hasLock := strings.Contains(strings.Join(info.Commands, "\n"), "ACCOUNT LOCK"); hasLock != locked {
			t.Errorf("locked=%v: lệnh xem trước có ACCOUNT LOCK = %v", locked, hasLock)
		}
		// Hoàn tác: xoá đúng user vừa tạo, DB còn nguyên.
		if err := s.Undo(context.Background(), f); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if _, ok := f.users[sdbName]; ok {
			t.Error("hoàn tác xong mà user vẫn còn")
		}
		if _, ok := f.dbs[sdbName]; !ok {
			t.Error("hoàn tác xoá mất DB")
		}
	}
}

func TestDBUserRecreateTuChoi(t *testing.T) {
	ctx := context.Background()
	// DB không còn: không có gì để cấp quyền, không tạo user mồ côi.
	f := newSDBFake()
	if err := (&DBUserRecreate{Name: sdbName, Password: sdbPw()}).Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "không có trong MariaDB") {
		t.Errorf("DB không có: err = %v", err)
	}
	if len(f.users) != 0 {
		t.Errorf("DB không có mà vẫn tạo user: %v", f.users)
	}
	// User đã có: không ghi đè mật khẩu của user không do bước này tạo.
	f = newSDBFake()
	f.set(sdbWithDB)
	old := f.users[sdbName]
	if err := (&DBUserRecreate{Name: sdbName, Password: sdbPw()}).Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "đã có trong MariaDB") {
		t.Errorf("user đã có: err = %v", err)
	}
	if f.users[sdbName] != old {
		t.Errorf("user đã có mà bị đổi: %+v -> %+v", old, f.users[sdbName])
	}
	// Mật khẩu sai dạng và tên sai dạng: từ chối trước khi chạy lệnh nào.
	f = newSDBFake()
	f.set(sdbOnlyDB)
	for _, s := range []*DBUserRecreate{
		{Name: sdbName, Password: system.NewSecret("short")},
		{Name: "a'; DROP USER root; --", Password: sdbPw()},
	} {
		if err := s.Apply(ctx, f); err == nil {
			t.Errorf("%q: không từ chối", s.Name)
		}
	}
	if got := sdbMutating(f.Ran()); len(got) != 0 {
		t.Errorf("đầu vào sai mà vẫn chạy lệnh đổi trạng thái: %v", got)
	}
	// Undo khi chưa Apply: lỗi, không xoá user nào.
	f = newSDBFake()
	f.set(sdbWithDB)
	if err := (&DBUserRecreate{Name: sdbName, Password: sdbPw()}).Undo(ctx, f); err == nil {
		t.Error("Undo khi chưa Apply mà không lỗi")
	}
	if _, ok := f.users[sdbName]; !ok {
		t.Error("Undo khi chưa Apply đã xoá user có sẵn")
	}
}

// CREATE USER được nhưng GRANT (hoặc LOCK) lỗi: user tạo dở phải bị xoá, không
// sót một user không quyền, hay một user không khoá của DB đang tạm khoá.
func TestDBUserRecreateTaoDo(t *testing.T) {
	for _, fail := range []string{"GRANT ALL", "ALTER USER"} {
		f := newSDBFake()
		f.set(func(f *sdbFake) {
			sdbOnlyDB(f)
			f.failOn = []string{fail}
		})
		s := &DBUserRecreate{Name: sdbName, Password: sdbPw(), Locked: true}
		rep := sdbRun(t, f, s)
		if rep.Job.Status == engine.JobSucceeded {
			t.Fatalf("%s lỗi mà job thành công", fail)
		}
		if strings.Contains(rep.Job.Error, sdbPw().Reveal()) {
			t.Errorf("%s: lỗi của job lộ mật khẩu: %s", fail, rep.Job.Error)
		}
		if u, ok := f.users[sdbName]; ok {
			t.Errorf("%s lỗi mà còn sót user tạo dở: %+v", fail, u)
		}
		if _, ok := f.dbs[sdbName]; !ok {
			t.Errorf("%s lỗi mà DB bị xoá", fail)
		}
	}
}

// NoDB: DB cũng đã mất mà người gọi vẫn cần user trước (DB của một site). User
// được tạo với quyền trên đúng DB đó; không tạo DB nào.
func TestDBUserRecreateNoDB(t *testing.T) {
	f := newSDBFake()
	s := &DBUserRecreate{Name: sdbName, Password: sdbPw(), NoDB: true}
	if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %s", rep.Job.Error)
	}
	u, ok := f.users[sdbName]
	if !ok || u.pw != sdbPw().Reveal() || u.grant != strings.ReplaceAll(sdbName, "_", `\_`) {
		t.Errorf("user = %+v (có=%v): muốn có, mật khẩu mới, quyền trên đúng DB của nó", u, ok)
	}
	if len(f.dbs) != 0 {
		t.Errorf("bước tạo user mà tạo cả DB: %v", f.dbs)
	}
	if why := s.Info().Why.Vi; !strings.Contains(why, "DB cùng tên cũng không còn") {
		t.Errorf("lời giải thích không nói DB cũng đã mất: %s", why)
	}
}

// ============================================================ SiteDBUserRestore

// m34CfgPw: mật khẩu DB "đang ghi trong wp-config.php", đúng dạng tpan sinh.
const m34CfgPw = "abcdefghijklmnopqrstuvwxyzABCDEF"

// m34SiteFake: site WordPress còn DB và wp-config.php, user DB đã bị xoá.
func m34SiteFake(t *testing.T) (*dbFake, site.Layout) {
	t.Helper()
	l := dbLayout(t)
	f := newDBFake()
	f.SetDir(l.Public, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
	f.SetFile(l.WPConfig, []byte(bkWPConfig(l.DB, m34CfgPw)), 0o600, system.Owner{User: l.User, Group: l.User})
	f.set(func(f *dbFake) { f.dbs[l.DB] = true })
	return f, l
}

func TestSiteDBUserRestore(t *testing.T) {
	ctx := context.Background()
	for _, locked := range []bool{false, true} {
		f, l := m34SiteFake(t)
		files := f.Snapshot()
		s := &SiteDBUserRestore{Layout: l, Locked: locked}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("locked=%v: Check trước = %v, %v (user chưa có)", locked, done, err)
		}
		rep := dbExecute(t, f, s)
		if rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("locked=%v: job: %s", locked, rep.Job.Error)
		}
		// User có lại ĐÚNG mật khẩu trong wp-config.php, quyền trên đúng DB
		// của site, và khoá hay không theo Locked.
		if got := f.users[l.DB]; got != m34CfgPw {
			t.Errorf("locked=%v: mật khẩu của user tạo lại = %q, muốn mật khẩu trong wp-config.php", locked, got)
		}
		if want := strings.ReplaceAll(l.DB, "_", `\_`); f.grants[l.DB] != want {
			t.Errorf("locked=%v: quyền cấp trên %q, muốn %q", locked, f.grants[l.DB], want)
		}
		if f.locked[l.DB] != locked {
			t.Errorf("locked=%v: user khoá = %v", locked, f.locked[l.DB])
		}
		// Không file nào bị sửa (wp-config.php giữ nguyên), DB còn nguyên.
		if !reflect.DeepEqual(files, f.Snapshot()) {
			t.Errorf("locked=%v: hệ thống file đổi sau bước", locked)
		}
		if !f.dbs[l.DB] {
			t.Errorf("locked=%v: DB bị đụng", locked)
		}
		// Mật khẩu không nằm trên dòng lệnh, trong Info, hay trong nhật ký job.
		for _, c := range f.Ran() {
			if strings.Contains(c.String(), m34CfgPw) {
				t.Errorf("mật khẩu trên dòng lệnh: %s", c.String())
			}
		}
		if strings.Contains(fmt.Sprint(s.Info()), m34CfgPw) || strings.Contains(fmt.Sprintf("%+v", rep), m34CfgPw) {
			t.Error("Info hoặc báo cáo của job lộ mật khẩu")
		}
		if hasLock := strings.Contains(strings.Join(s.Info().Commands, "\n"), "ACCOUNT LOCK"); hasLock != locked {
			t.Errorf("locked=%v: lệnh xem trước có ACCOUNT LOCK = %v", locked, hasLock)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Errorf("locked=%v: Check sau = %v, %v", locked, done, err)
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if _, ok := f.users[l.DB]; ok || !f.dbs[l.DB] {
			t.Errorf("locked=%v: sau hoàn tác: user còn=%v, DB còn=%v", locked, ok, f.dbs[l.DB])
		}
	}
}

func TestSiteDBUserRestoreTuChoi(t *testing.T) {
	ctx := context.Background()
	noUser := func(t *testing.T, f *dbFake, l site.Layout, what string) {
		t.Helper()
		if _, ok := f.users[l.DB]; ok {
			t.Errorf("%s: vẫn tạo user", what)
		}
	}

	// User đã có (ai đó vừa tạo lại): Check nói đã đúng, engine bỏ qua bước,
	// mật khẩu của user đó không bị đụng. Gọi thẳng Apply thì lỗi.
	f, l := m34SiteFake(t)
	f.set(func(f *dbFake) { f.users[l.DB] = "matkhaucuanguoikhac0123456789" })
	s := &SiteDBUserRestore{Layout: l}
	if rep := dbExecute(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("user đã có: job %s", rep.Job.Error)
	}
	if f.users[l.DB] != "matkhaucuanguoikhac0123456789" {
		t.Errorf("user đã có mà mật khẩu bị đổi thành %q", f.users[l.DB])
	}
	if err := s.Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "đã có trong MariaDB") {
		t.Errorf("Apply khi user đã có: %v", err)
	}
	if err := s.Undo(ctx, f); err == nil {
		t.Error("Undo khi bước chưa tạo gì mà không lỗi")
	}
	if _, ok := f.users[l.DB]; !ok {
		t.Error("Undo khi bước chưa tạo gì đã xoá user có sẵn")
	}

	// wp-config.php không dùng được: mỗi trường hợp đều dừng trước khi tạo
	// user, và lời báo không chép lại mật khẩu trong file.
	const odd = "p@ss'; DROP USER root; --"
	cases := map[string]func(f *dbFake, l site.Layout){
		"không có wp-config.php": func(f *dbFake, l site.Layout) {
			if err := f.Remove(l.WPConfig); err != nil {
				t.Fatal(err)
			}
		},
		"mật khẩu không phải loại tpan sinh": func(f *dbFake, l site.Layout) {
			f.SetFile(l.WPConfig, []byte(bkWPConfig(l.DB, odd)), 0o600, system.Owner{User: l.User, Group: l.User})
		},
		"wp-config.php ghi DB của site khác": func(f *dbFake, l site.Layout) {
			f.SetFile(l.WPConfig, []byte(bkWPConfig("tp_site_khac_000000", m34CfgPw)), 0o600, system.Owner{User: l.User, Group: l.User})
		},
		"wp-config.php không phải của user site": func(f *dbFake, l site.Layout) {
			f.SetFile(l.WPConfig, []byte(bkWPConfig(l.DB, m34CfgPw)), 0o600, system.Root)
		},
		"hai dòng DB_PASSWORD": func(f *dbFake, l site.Layout) {
			f.SetFile(l.WPConfig, []byte(bkWPConfig(l.DB, m34CfgPw)+"define( 'DB_PASSWORD', '"+m34CfgPw+"' );\n"), 0o600, system.Owner{User: l.User, Group: l.User})
		},
	}
	for name, breakIt := range cases {
		f, l := m34SiteFake(t)
		breakIt(f, l)
		f.ResetRan()
		err := (&SiteDBUserRestore{Layout: l}).Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "không tạo lại được user DB") {
			t.Errorf("%s: err = %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), odd) || strings.Contains(err.Error(), m34CfgPw) {
			t.Errorf("%s: lời báo lộ mật khẩu: %v", name, err)
		}
		noUser(t, f, l, name)
		for _, c := range f.Ran() {
			if c.Name == "mariadb" && !dbReadOnly(c) {
				t.Errorf("%s: vẫn chạy câu đổi trạng thái: %s", name, c.Stdin)
			}
		}
	}

	// CREATE USER được mà GRANT lỗi: user tạo dở bị xoá, lỗi không lộ mật khẩu.
	f, l = m34SiteFake(t)
	f.set(func(f *dbFake) { f.failOn = []string{"GRANT ALL"} })
	rep := dbExecute(t, f, &SiteDBUserRestore{Layout: l})
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatal("GRANT lỗi mà job thành công")
	}
	if strings.Contains(rep.Job.Error, m34CfgPw) {
		t.Errorf("lỗi của job lộ mật khẩu: %s", rep.Job.Error)
	}
	noUser(t, f, l, "GRANT lỗi")

	// Layout rỗng (quên ParseDomain): từ chối, không lệnh nào chạy.
	f = newDBFake()
	if err := (&SiteDBUserRestore{}).Apply(ctx, f); err == nil || len(f.Ran()) != 0 {
		t.Errorf("Layout rỗng: err = %v, lệnh đã chạy = %d", err, len(f.Ran()))
	}
}

// ================================================================ DBImport Fresh

// bkFresh: DB của site đã bị xoá ngoài tpan, có một bản xuất trọn vẹn để nhập.
func bkFresh(t *testing.T) (*bkFake, *DBImport) {
	t.Helper()
	f := newBkFake(t)
	file := site.BackupDir(f.l) + "/shop.example.com-db-20261118-000000.sql"
	f.SetFile(file, []byte(bkDumpOf("imported")), 0o600, system.Root)
	f.mu.Lock()
	f.db, f.gone = "", true
	f.mu.Unlock()
	return f, &DBImport{Layout: f.l, File: file, Fresh: true}
}

func (f *bkFake) isGone() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gone
}

func TestDBImportFresh(t *testing.T) {
	ctx := context.Background()

	t.Run("DB đã mất: tạo lại rồi nhập, không xuất gì trước; Undo xoá DB vừa tạo", func(t *testing.T) {
		f, s := bkFresh(t)
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if rep := runOne(t, f, s); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s %s", rep.Job.Status, rep.Job.Error)
		}
		if f.dbNow() != "imported" || f.isGone() {
			t.Errorf("sau khi nhập: DB = %q, mất = %v", f.dbNow(), f.isGone())
		}
		if f.ranHas("mariadb-dump") {
			t.Errorf("DB không có mà vẫn chạy mariadb-dump: %v", ranStrings(f.Ran()))
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		if !f.isGone() || f.dbNow() != "" {
			t.Errorf("sau Undo: DB = %q, mất = %v; muốn về trạng thái không có DB", f.dbNow(), f.isGone())
		}
	})

	t.Run("DB đã có lại từ lúc dựng Plan: dừng, không xoá đè", func(t *testing.T) {
		f, s := bkFresh(t)
		f.mu.Lock()
		f.db, f.gone = "du lieu vua tao lai", false
		f.mu.Unlock()
		err := s.Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "đã có lại trong MariaDB") {
			t.Errorf("err = %v", err)
		}
		if f.dbNow() != "du lieu vua tao lai" {
			t.Errorf("DB = %q: dữ liệu vừa có lại đã bị đụng", f.dbNow())
		}
		for _, c := range f.Ran() {
			if strings.Contains(string(c.Stdin), "DROP DATABASE") || strings.Contains(c.String(), "source ") {
				t.Errorf("vẫn chạy lệnh đổi DB: %s %s", c.String(), c.Stdin)
			}
		}
	})

	t.Run("nhập lỗi giữa chừng: xoá DB nhập dở, về trạng thái không có DB", func(t *testing.T) {
		f, s := bkFresh(t)
		f.importFail = s.File
		err := s.Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "1064") {
			t.Errorf("err = %v", err)
		}
		if !f.isGone() || f.dbNow() != "" {
			t.Errorf("sau lỗi: DB = %q, mất = %v; muốn không còn DB nhập dở", f.dbNow(), f.isGone())
		}
		if done, _ := s.Check(ctx, f); done {
			t.Error("Apply lỗi mà Check báo đã xong")
		}
	})

	t.Run("file nhập không trọn vẹn: không tạo DB", func(t *testing.T) {
		f, s := bkFresh(t)
		f.SetFile(s.File, []byte("-- MariaDB dump\nBODY:dở\n"), 0o600, system.Root)
		if err := s.Apply(ctx, f); err == nil {
			t.Error("file dở mà không lỗi")
		}
		if !f.isGone() {
			t.Error("file dở mà vẫn tạo DB")
		}
	})

	t.Run("Fresh không đi cùng UndoFile; đường dẫn lạ bị từ chối", func(t *testing.T) {
		f, s := bkFresh(t)
		bad := *s
		bad.UndoFile = site.BackupDir(f.l) + "/x.sql"
		if err := bad.Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "không dùng file hoàn tác") {
			t.Errorf("Fresh kèm UndoFile: %v", err)
		}
		bad = *s
		bad.File = "/tmp/x.sql"
		if err := bad.Apply(ctx, f); err == nil {
			t.Error("file ngoài thư mục backup mà không bị từ chối")
		}
		if !f.isGone() {
			t.Error("đầu vào sai mà vẫn tạo DB")
		}
	})

	t.Run("Undo trước Apply: lỗi; xem trước nói rõ không có đường lùi về bản cũ", func(t *testing.T) {
		f, s := bkFresh(t)
		if err := s.Undo(ctx, f); err == nil {
			t.Error("Undo trước Apply: muốn lỗi")
		}
		info := s.Info()
		if !info.Danger || !strings.Contains(info.Why.Vi, "hoàn tác là xoá DB vừa tạo") {
			t.Errorf("Info = %+v", info)
		}
	})
}
