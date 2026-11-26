package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// dbTestNow là giờ cố định của các test thao tác DB: tên file xuất so được.
var dbTestNow = time.Date(2026, 11, 20, 3, 4, 5, 0, time.UTC)

// dbOldPassword là mật khẩu DB "đang dùng" trong wp-config.php giả.
const dbOldPassword = "OldPassw0rdOldPassw0rdOldPassw0r"

const dbDom = "shop.example.com"

// dbEnv là siteEnv có một site đã tạo, cộng các handler mà thao tác DB cần:
// mariadb-dump ghi file xuất thật vào hệ thống file giả, tail đọc dòng cuối,
// sha256sum tính trên nội dung giả, mariadb trả cỡ DB và ghi lại các câu
// đổi mật khẩu và các lần nhập.
type dbEnv struct {
	*siteEnv
	b *Backups
	l site.Layout

	mu       sync.Mutex
	dbSize   string   // stdout của câu đo cỡ DB
	alters   []string // stdin của các câu ALTER USER ... IDENTIFIED BY
	wpSets   []string // stdin của `wp config set DB_PASSWORD`
	imports  []string // file của các lần `mariadb ... -e "source <file>"`
	http500  int      // số lần curl --resolve tiếp theo trả 500
	curlRuns int
	// M34: state là stdout của câu "DB và user DB có không" ("1\t0\n": DB còn,
	// user mất). Rỗng: câu đó trả rỗng như trước, tức là không hỏi được MariaDB,
	// và mọi thao tác giữ đường cũ. sqls: mọi lô SQL đổi trạng thái đã chạy.
	state string
	sqls  []string
}

// newDBEnv: setup (nếu có) chạy trước khi thay handler mariadb, để tạo hay
// đưa thêm site vào thùng rác bằng handler đầy đủ của siteSim.
func newDBEnv(t *testing.T, ssl bool, setup ...func(*siteEnv)) *dbEnv {
	t.Helper()
	se := newSiteEnv(t, Nginx)
	se.fixedNow(dbTestNow)
	se.add(t, dbDom)
	for _, fn := range setup {
		fn(se)
	}
	if ssl {
		st := se.siteOf(t, dbDom)
		st.SSL = true
		if err := se.sites.SaveSite(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	l := site.LayoutOf(mustDomain(t, dbDom))
	de := &dbEnv{siteEnv: se, l: l, dbSize: "1048576\n"}
	de.b = &Backups{Sites: se.s, Store: se.st}
	se.m.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 60 << 30})
	se.m.SetFile(l.WPConfig, []byte("<?php\n"+
		"define( 'DB_NAME', '"+l.DB+"' );\n"+
		"define( 'DB_USER', '"+l.DB+"' );\n"+
		"define( 'DB_PASSWORD', '"+dbOldPassword+"' );\n"),
		0o600, system.Owner{User: l.User, Group: l.User})

	m := se.m
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}
	// Đăng ký sau se.add: cùng độ dài tiền tố thì handler sau thắng, nên câu
	// SQL của site (đã chạy xong) không còn qua handler của siteSim.
	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		de.mu.Lock()
		defer de.mu.Unlock()
		sql := string(c.Stdin)
		switch {
		case strings.Contains(sql, "information_schema.TABLES"):
			return exit(0, de.dbSize, "")
		case strings.Contains(sql, "information_schema.SCHEMATA") && strings.Contains(sql, "mysql.global_priv"):
			return exit(0, de.state, "")
		case strings.Contains(sql, "ALTER USER") && strings.Contains(sql, "IDENTIFIED BY"):
			de.alters = append(de.alters, sql)
		}
		if !strings.HasPrefix(strings.TrimSpace(sql), "SELECT") {
			de.sqls = append(de.sqls, sql)
			// state đi theo các câu đã chạy, từng câu theo thứ tự trong lô, để
			// lần đọc lại sau một bước thấy đúng thứ bước đó vừa làm.
			if f := strings.Fields(de.state); len(f) == 2 {
				for _, stmt := range strings.SplitAfter(sql, ";\n") {
					switch {
					case strings.HasPrefix(stmt, "CREATE DATABASE"):
						f[0] = "1"
					case strings.HasPrefix(stmt, "DROP DATABASE"):
						f[0] = "0"
					case strings.HasPrefix(stmt, "CREATE USER"):
						f[1] = "1"
					case strings.HasPrefix(stmt, "DROP USER"):
						f[1] = "0"
					}
				}
				de.state = f[0] + "\t" + f[1] + "\n"
			}
		}
		return exit(0, "", "")
	})
	m.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		if i := slices.Index(c.Args, "-e"); i >= 0 {
			de.mu.Lock()
			de.imports = append(de.imports, strings.TrimPrefix(c.Args[i+1], "source "))
			de.mu.Unlock()
		}
		return exit(0, "", "")
	})
	m.Handle([]string{"mariadb-dump"}, func(c system.Cmd) system.Result {
		p := argPrefix(c.Args, "--result-file=")
		db := c.Args[len(c.Args)-1]
		m.SetFile(p, []byte("-- MariaDB dump của "+db+" lúc "+p+"\nCREATE TABLE t (id int);\n-- Dump completed on 2026-11-20  3:04:05\n"), 0o600, system.Root)
		return exit(0, "", "")
	})
	m.Handle([]string{"tail"}, func(c system.Cmd) system.Result {
		data, err := m.ReadFile(c.Args[len(c.Args)-1])
		if err != nil {
			return exit(1, "", err.Error()+"\n")
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		return exit(0, lines[len(lines)-1]+"\n", "")
	})
	m.Handle([]string{"sha256sum"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		data, err := m.ReadFile(p)
		if err != nil {
			return exit(1, "", "sha256sum: "+p+": No such file or directory\n")
		}
		sum := sha256.Sum256(data)
		return exit(0, hex.EncodeToString(sum[:])+"  "+p+"\n", "")
	})
	m.Handle([]string{"wp", "config", "set", "DB_PASSWORD"}, func(c system.Cmd) system.Result {
		de.mu.Lock()
		defer de.mu.Unlock()
		de.wpSets = append(de.wpSets, string(c.Stdin))
		return exit(0, "", "")
	})
	m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		r := argAfter(c.Args, "--resolve")
		de.mu.Lock()
		de.curlRuns++
		fail := de.http500 > 0
		if fail {
			de.http500--
		}
		de.mu.Unlock()
		if fail {
			return exit(0, "<html>There has been a critical error</html>TPAN_HTTP_CODE=500", "")
		}
		dom, _, _ := strings.Cut(r, ":")
		return exit(0, "<html><link href='https://"+dom+"/wp-content/x.css'></html>TPAN_HTTP_CODE=200", "")
	})
	return de
}

// dump chạy db.dump tới xong và trả dòng backups vừa ghi.
func (de *dbEnv) dump(t *testing.T) Backup {
	t.Helper()
	p, err := de.b.PrepareDBDump(ctx, mustDomain(t, dbDom))
	if err != nil {
		t.Fatalf("PrepareDBDump: %v", err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("db.dump: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	rows, err := de.st.Backups(ctx, dbDom)
	if err != nil || len(rows) == 0 {
		t.Fatalf("bảng backups sau db.dump: %v %v", rows, err)
	}
	return rows[0]
}

// mutating: các lệnh trong Ran() có thay đổi máy (xem trước không được chạy).
func dbMutating(ran []system.Cmd) []string {
	var bad []string
	for _, c := range ran {
		switch {
		case c.Name == "sha256sum", c.Name == "tail":
		case c.Name == "mariadb" && strings.HasPrefix(strings.TrimSpace(string(c.Stdin)), "SELECT"):
		default:
			bad = append(bad, c.String())
		}
	}
	return bad
}

func TestDBPasswordPlan(t *testing.T) {
	for _, ssl := range []bool{false, true} {
		t.Run(fmt.Sprintf("ssl=%v", ssl), func(t *testing.T) {
			de := newDBEnv(t, ssl)
			p, err := de.b.PrepareDBPassword(ctx, mustDomain(t, dbDom))
			if err != nil {
				t.Fatal(err)
			}
			if p.Plan.Kind != "db.passwd" || p.Plan.Target != dbDom {
				t.Errorf("Plan = %s/%s", p.Plan.Kind, p.Plan.Target)
			}
			if got, want := stepIDs(p.Plan), []string{"mariadb.db-password:" + dbDom}; !reflect.DeepEqual(got, want) {
				t.Errorf("bước = %v, muốn %v", got, want)
			}
			if len(p.Plan.Verify) != 2 {
				t.Errorf("kiểm chứng có %d phép kiểm, muốn 2 (DB và đầu cuối)", len(p.Plan.Verify))
			}

			de.m.ResetRan()
			pv, err := p.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if bad := dbMutating(de.m.Ran()); len(bad) > 0 {
				t.Errorf("xem trước chạy lệnh thay đổi máy: %v", bad)
			}
			if !pv.Steps[0].Info.Danger {
				t.Error("đổi mật khẩu DB không Danger: CLI sẽ không hỏi lại")
			}

			rep, err := p.Execute(ctx, nil)
			if err != nil || rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("job: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
			}
			wantIDs := []string{"db.access:" + dbDom, "http.site:" + dbDom}
			if ssl {
				wantIDs[1] = "https.site:" + dbDom
			}
			var gotIDs []string
			for _, r := range rep.Checks {
				gotIDs = append(gotIDs, r.ID)
			}
			if !reflect.DeepEqual(gotIDs, wantIDs) {
				t.Errorf("phép kiểm = %v, muốn %v", gotIDs, wantIDs)
			}

			// Mật khẩu mới: 32 chữ số, khác mật khẩu cũ, cùng một giá trị ở
			// MariaDB và wp-config.php, chỉ đi qua stdin.
			if len(de.alters) != 1 || len(de.wpSets) != 1 {
				t.Fatalf("ALTER USER %d lần, wp config set %d lần; muốn 1 và 1", len(de.alters), len(de.wpSets))
			}
			g := regexp.MustCompile(`IDENTIFIED BY '([A-Za-z0-9]{32})'`).FindStringSubmatch(de.alters[0])
			if g == nil {
				t.Fatalf("câu ALTER USER lạ: %q", de.alters[0])
			}
			pw := g[1]
			if pw == dbOldPassword || de.wpSets[0] != pw+"\n" {
				t.Errorf("mật khẩu mới %q, wp config set nhận %q", pw, de.wpSets[0])
			}
			for _, c := range de.m.Ran() {
				if strings.Contains(c.String(), pw) || strings.Contains(c.String(), dbOldPassword) {
					t.Errorf("mật khẩu nằm trên dòng lệnh: %s", c.String())
				}
			}
			evs, err := de.st.Events(ctx, rep.Job.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []any{evs, rep, pv} {
				raw, _ := json.Marshal(v)
				if strings.Contains(string(raw), pw) || strings.Contains(string(raw), dbOldPassword) {
					t.Errorf("mật khẩu lọt vào %T: %s", v, raw)
				}
			}
		})
	}
}

// Sau đổi mật khẩu site có thể trả 500 thêm khoảng 2 giây (opcache): kiểm
// chứng phải thử lại, không hoàn tác oan; còn lỗi mãi thì hoàn tác.
func TestDBPasswordVerifyRetries(t *testing.T) {
	oldWait, oldEvery := dbVerifyWait, dbVerifyEvery
	t.Cleanup(func() { dbVerifyWait, dbVerifyEvery = oldWait, oldEvery })
	dbVerifyWait, dbVerifyEvery = 2*time.Second, time.Millisecond

	de := newDBEnv(t, false)
	de.http500 = 3
	p, err := de.b.PrepareDBPassword(ctx, mustDomain(t, dbDom))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("500 thoáng qua mà job hỏng: %v %s%s", err, rep.Job.Status, failedChecks(rep.Checks))
	}
	if de.curlRuns != 4 {
		t.Errorf("curl chạy %d lần, muốn 4 (3 lần 500 rồi 200)", de.curlRuns)
	}

	dbVerifyWait = 20 * time.Millisecond
	de = newDBEnv(t, false)
	de.http500 = 1 << 30
	p, err = de.b.PrepareDBPassword(ctx, mustDomain(t, dbDom))
	if err != nil {
		t.Fatal(err)
	}
	rep, _ = p.Execute(ctx, nil)
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatal("site trả 500 mãi mà job thành công")
	}
	// Hoàn tác: đặt lại mật khẩu cũ ở MariaDB và wp-config.php.
	if len(de.alters) != 2 || !strings.Contains(de.alters[1], dbOldPassword) ||
		len(de.wpSets) != 2 || de.wpSets[1] != dbOldPassword+"\n" {
		t.Errorf("hoàn tác không đặt lại mật khẩu cũ: alters %d, wpSets %q", len(de.alters), de.wpSets)
	}
}

func TestDBOpsRefuse(t *testing.T) {
	de := newDBEnv(t, false, func(se *siteEnv) {
		se.add(t, "blog.example.com")
		se.trash(t, "blog.example.com")
	})
	trashed, missing := mustDomain(t, "blog.example.com"), mustDomain(t, "none.example.com")

	if _, err := de.b.PrepareDBPassword(ctx, missing); refusedCode(t, err) != "site_not_found" {
		t.Error("passwd site không có: muốn site_not_found")
	}
	if _, err := de.b.PrepareDBPassword(ctx, trashed); refusedCode(t, err) != "site_trashed" {
		t.Error("passwd site trong thùng rác: muốn site_trashed")
	}
	if _, err := de.b.PrepareDBImport(ctx, trashed, "x"); refusedCode(t, err) != "site_trashed" {
		t.Error("import site trong thùng rác: muốn site_trashed")
	}
	if _, err := de.b.PrepareDBDump(ctx, missing); refusedCode(t, err) != "site_not_found" {
		t.Error("dump site không có: muốn site_not_found")
	}
	// DB của site trong thùng rác vẫn còn: xuất được.
	if _, err := de.b.PrepareDBDump(ctx, trashed); err != nil {
		t.Errorf("dump site trong thùng rác: %v", err)
	}
}

func TestDBDump(t *testing.T) {
	de := newDBEnv(t, false)
	d := mustDomain(t, dbDom)
	p, err := de.b.PrepareDBDump(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	want := site.BackupDir(de.l) + "/" + dbDom + "-db-20261120-030405.sql"
	if got := stepIDs(p.Plan); !reflect.DeepEqual(got, []string{"mariadb.dump:" + want}) {
		t.Errorf("bước = %v, muốn file %s", got, want)
	}
	de.m.ResetRan()
	if _, err := p.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if bad := dbMutating(de.m.Ran()); len(bad) > 0 {
		t.Errorf("xem trước chạy lệnh thay đổi máy: %v", bad)
	}

	bk := de.dump(t)
	data, err := de.m.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if bk.Path != want || bk.Kind != BackupSQL || bk.Reason != ReasonManual || bk.Domain != dbDom ||
		bk.SHA256 != hex.EncodeToString(sum[:]) || bk.Size != int64(len(data)) || bk.JobID == "" {
		t.Errorf("dòng backups = %+v", bk)
	}

	// Cùng một giây: không đè, không bỏ qua bước xuất.
	if _, err := de.b.PrepareDBDump(ctx, d); refusedCode(t, err) != "backup_exists" {
		t.Error("xuất lần hai trong cùng giây: muốn backup_exists")
	}

	// Giữ 1 bản: xuất bản mới thì bản cũ bị dọn cả file lẫn dòng.
	mustSet(t, de.st, SettingBackupKeep, "1")
	de.fixedNow(dbTestNow.Add(time.Second))
	bk2 := de.dump(t)
	if rows, _ := de.st.Backups(ctx, dbDom); len(rows) != 1 || rows[0].ID != bk2.ID {
		t.Errorf("sau khi dọn còn %+v, muốn chỉ %s", rows, bk2.ID)
	}
	if de.exists(want) {
		t.Errorf("bản cũ %s chưa bị xoá", want)
	}
}

func TestDBDumpRefusesLowDisk(t *testing.T) {
	de := newDBEnv(t, false)
	// DB 1 MiB, đĩa 10 GiB còn trống 2 MiB: đủ × 1,2 nhưng không còn 10% đĩa.
	de.m.SetDisk("/", system.DiskUsage{Total: 10 << 30, Free: 2 << 20})
	d := mustDomain(t, dbDom)
	_, err := de.b.PrepareDBDump(ctx, d)
	if refusedCode(t, err) != CodeDiskSpaceLow {
		t.Fatalf("muốn %s, có %v", CodeDiskSpaceLow, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "1.0 MiB") || !strings.Contains(msg, "2.0 MiB") || !strings.Contains(msg, "10.0 GiB") {
		t.Errorf("lời từ chối thiếu số đo: %s", msg)
	}
	if _, err := de.b.PrepareDBImport(ctx, d, "x"); refusedCode(t, err) != "backup_not_found" {
		t.Error("import id không có: muốn backup_not_found")
	}
}

func TestDBImport(t *testing.T) {
	de := newDBEnv(t, false)
	d := mustDomain(t, dbDom)
	bk := de.dump(t)

	p, err := de.b.PrepareDBImport(ctx, d, bk.ID)
	if err != nil {
		t.Fatal(err)
	}
	undo := site.BackupDir(de.l) + "/" + dbDom + "-db-20261120-030405-before-import.sql"
	want := []string{"mariadb.dump:" + undo, "db.import:" + dbDom}
	if got := stepIDs(p.Plan); !reflect.DeepEqual(got, want) {
		t.Errorf("bước = %v, muốn %v", got, want)
	}
	de.m.ResetRan()
	pv, err := p.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bad := dbMutating(de.m.Ran()); len(bad) > 0 {
		t.Errorf("xem trước chạy lệnh thay đổi máy: %v", bad)
	}
	if !pv.Steps[1].Info.Danger {
		t.Error("nhập DB không Danger")
	}

	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if !reflect.DeepEqual(de.imports, []string{bk.Path}) {
		t.Errorf("đã nhập %v, muốn đúng %s", de.imports, bk.Path)
	}
	rows, err := de.st.Backups(ctx, dbDom)
	if err != nil {
		t.Fatal(err)
	}
	var before *Backup
	for i := range rows {
		if rows[i].Path == undo {
			before = &rows[i]
		}
	}
	if before == nil || before.Kind != BackupSQL || before.Reason != ReasonBeforeImport || before.SHA256 == "" {
		t.Errorf("thiếu dòng cho bản tự xuất trước khi nhập: %+v", rows)
	}
}

// Kiểm chứng sau nhập lỗi: engine hoàn tác bằng cách nhập lại bản vừa xuất.
func TestDBImportUndoOnVerifyFail(t *testing.T) {
	oldWait := dbVerifyWait
	t.Cleanup(func() { dbVerifyWait = oldWait })
	dbVerifyWait = 0

	de := newDBEnv(t, false)
	bk := de.dump(t)
	p, err := de.b.PrepareDBImport(ctx, mustDomain(t, dbDom), bk.ID)
	if err != nil {
		t.Fatal(err)
	}
	de.http500 = 1 << 30
	rep, _ := p.Execute(ctx, nil)
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatal("site trả 500 mà job thành công")
	}
	undo := site.BackupDir(de.l) + "/" + dbDom + "-db-20261120-030405-before-import.sql"
	if !reflect.DeepEqual(de.imports, []string{bk.Path, undo}) {
		t.Errorf("lần nhập = %v, muốn nhập rồi nhập lại %s", de.imports, undo)
	}
	if rows, _ := de.st.Backups(ctx, dbDom); len(rows) != 1 {
		t.Errorf("job hỏng mà vẫn ghi dòng backups: %+v", rows)
	}
}

func TestDBImportRefusesBadBackup(t *testing.T) {
	de := newDBEnv(t, false, func(se *siteEnv) { se.add(t, "blog.example.com") })
	d := mustDomain(t, dbDom)
	bk := de.dump(t)

	add := func(b Backup) string {
		t.Helper()
		out, err := de.st.AddBackup(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	full := add(Backup{Domain: dbDom, Kind: BackupFull, Reason: ReasonManual, Path: site.BackupDir(de.l) + "/x.tar.gz", SHA256: bk.SHA256})
	other := add(Backup{Domain: "blog.example.com", Kind: BackupSQL, Reason: ReasonManual, Path: site.BackupRoot + "/blog.example.com/b.sql", SHA256: bk.SHA256})
	outside := add(Backup{Domain: dbDom, Kind: BackupSQL, Reason: ReasonManual, Path: "/tmp/evil.sql", SHA256: bk.SHA256})
	gone := add(Backup{Domain: dbDom, Kind: BackupSQL, Reason: ReasonManual, Path: site.BackupDir(de.l) + "/gone.sql", SHA256: bk.SHA256})

	for _, tc := range []struct{ id, code string }{
		{"", "backup_not_found"},
		{"../../etc/passwd", "backup_not_found"},
		{"a\x1b[2Jb", "backup_not_found"},
		{"0123456789abcdef", "backup_not_found"},
		{full, "backup_not_found"},
		{other, "backup_not_found"},
		{outside, "backup_corrupt"},
		{gone, "backup_corrupt"},
	} {
		_, err := de.b.PrepareDBImport(ctx, d, tc.id)
		if refusedCode(t, err) != tc.code {
			t.Errorf("id %q: muốn %s", tc.id, tc.code)
		}
		if strings.Contains(err.Error(), "\x1b") {
			t.Errorf("id lạ bị in lại trong lời từ chối: %q", err.Error())
		}
	}

	// File bị sửa sau lúc xuất: sha256 khác dòng.
	de.m.SetFile(bk.Path, []byte("DROP DATABASE mysql;\n-- Dump completed\n"), 0o600, system.Root)
	de.m.ResetRan()
	if _, err := de.b.PrepareDBImport(ctx, d, bk.ID); refusedCode(t, err) != "backup_corrupt" {
		t.Error("file bị sửa: muốn backup_corrupt")
	}
	if bad := dbMutating(de.m.Ran()); len(bad) > 0 {
		t.Errorf("từ chối mà vẫn chạy lệnh thay đổi máy: %v", bad)
	}

	// Store nil (--dry-run khi chưa có tpan.db): coi như bảng rỗng.
	nb := &Backups{Sites: de.s}
	if _, err := nb.PrepareDBImport(ctx, d, bk.ID); refusedCode(t, err) != "backup_not_found" {
		t.Error("Store nil: muốn backup_not_found")
	}
}

func TestDBHumanBytes(t *testing.T) {
	for n, want := range map[uint64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB"} {
		if got := dbHumanBytes(n); got != want {
			t.Errorf("dbHumanBytes(%d) = %q, muốn %q", n, got, want)
		}
	}
}
