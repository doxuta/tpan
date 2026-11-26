package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// m33Env: authzEnv (A có DB rời usera_shop) cộng một bản sao lưu thật của DB đó
// trên máy giả, và MariaDB giả đủ cho bản xem trước của sao lưu, nhập, tạo lại.
func newM33Env(t *testing.T) (*authzEnv, http.Handler, store.Backup) {
	t.Helper()
	e := newAuthzEnv(t)
	dump := []byte("-- data\n-- Dump completed on 2026-11-26 05:00:00\n")
	p := site.DBBackupDir(authzDB) + "/" + authzDB + "-db-20261126-050000.sql"
	e.f.SetFile(p, dump, 0o600, system.Root)
	e.f.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 80 << 30})
	sum := sha256.Sum256(dump)
	row, err := e.st.AddBackup(context.Background(), store.Backup{
		Domain: authzDB, Kind: site.BackupSQL, Reason: site.ReasonManual, Path: p,
		Size: int64(len(dump)), SHA256: hex.EncodeToString(sum[:]), Created: e.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.f.Handle([]string{"sha256sum", "--"}, func(c system.Cmd) system.Result {
		data, err := e.f.ReadFile(c.Args[1])
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "sha256sum: no file"}
		}
		s := sha256.Sum256(data)
		return system.Result{Stdout: hex.EncodeToString(s[:]) + "  " + c.Args[1] + "\n"}
	})
	e.f.Handle([]string{"tail", "-n", "1", "--"}, func(c system.Cmd) system.Result {
		data, err := e.f.ReadFile(c.Args[3])
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "tail: no file"}
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		return system.Result{Stdout: lines[len(lines)-1] + "\n"}
	})
	e.f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		sql := string(c.Stdin)
		switch {
		case strings.Contains(sql, "SUM(DATA_LENGTH"):
			return system.Result{Stdout: "4096\n"}
		case strings.Contains(sql, "information_schema.COLLATIONS"):
			return system.Result{Stdout: "utf8mb4\tutf8mb4_general_ci\tYes\nutf8mb4\tutf8mb4_unicode_ci\t\n"}
		case strings.Contains(sql, "mysql.global_priv") && strings.Contains(sql, "SCHEMATA"):
			// MariaDB giả đi theo bảng của tpan: DB rời còn dòng (không ở thùng
			// rác) thì DB và user DB có trong MariaDB; đã xoá hẳn thì không.
			// (M34: dòng mà không có DB là "DB đã bị xoá ngoài tpan".)
			if db, ok, _ := e.st.StandaloneDB(context.Background(), authzDB); ok && db.Status == account.DBActive {
				return system.Result{Stdout: "1\t1\n"}
			}
			return system.Result{Stdout: "0\t0\n"}
		}
		return system.Result{ExitCode: 1, Stderr: "MariaDB giả của test M33: câu lạ: " + sql}
	})
	return e, e.s.handler(authzDist), row
}

func m33Steps(t *testing.T, pv engine.Preview) string {
	t.Helper()
	var ids []string
	for _, s := range pv.Steps {
		ids = append(ids, strings.SplitN(s.Info.ID, ":", 2)[0])
	}
	return pv.Kind + " " + pv.Target + ": " + strings.Join(ids, " ")
}

// Bản sao lưu của DB rời nằm chung bảng backups: chủ thấy nó ở GET /backups
// (có cờ db), người khác thì không; bộ lọc ?db= chỉ nhận tên DB rời.
func TestM33DanhSachBanSaoLuuDBRoi(t *testing.T) {
	e, h, row := newM33Env(t)
	if o, ok, err := e.st.BackupOwner(context.Background(), row.ID); err != nil || !ok || o != authzA {
		t.Fatalf("chủ của dòng backup = %q, %v, %v; muốn %s (chủ của DB rời)", o, ok, err, authzA)
	}
	list := func(q string, who string) []store.Backup {
		t.Helper()
		return m13Decode[BackupsOverview](t, e.do(h, "GET", "/api/v1/backups"+q, "", e.cookies[who]), 200).Items
	}
	for _, who := range []string{authzA, account.AdminName} {
		got := list("?db="+authzDB, who)
		if len(got) != 1 || got[0].ID != row.ID || !got[0].DB || got[0].Domain != authzDB {
			t.Errorf("%s xem ?db=%s: %+v", who, authzDB, got)
		}
	}
	if got := list("?db="+authzDB, authzB); len(got) != 0 {
		t.Errorf("B thấy bản sao lưu DB của A: %+v", got)
	}
	// Không lọc: A thấy cả bản của site lẫn của DB rời; dòng của site không mang cờ db.
	all := list("", authzA)
	if len(all) != 2 {
		t.Fatalf("A xem mọi bản: %d dòng, muốn 2", len(all))
	}
	for _, b := range all {
		if b.DB != (b.Domain == authzDB) {
			t.Errorf("cờ db của dòng %s (%s) = %v", b.ID, b.Domain, b.DB)
		}
	}
	for _, q := range []string{"?db=a.example.com", "?db=USERA_SHOP", "?db=khongcogachduoi", "?db=" + authzDB + "&domain=" + authzSite, "?db=a%0Ab"} {
		if rec := e.do(h, "GET", "/api/v1/backups"+q, "", e.cookies[authzA]); rec.Code != http.StatusNotFound || errCode(t, rec) != "db_not_found" {
			t.Errorf("GET /backups%s: %d %s, muốn 404 db_not_found", q, rec.Code, rec.Body)
		}
	}
}

// Sao lưu ngay và nhập lại qua hai route mới: chủ được bản xem trước đúng Plan,
// người khác nhận 404 như DB không có, và id của bản khác DB bị từ chối.
func TestM33SaoLuuVaNhapQuaRoute(t *testing.T) {
	e, h, row := newM33Env(t)
	pv := m13Decode[engine.Preview](t, e.do(h, "POST", "/api/v1/dbs/"+authzDB+"/backups?dry_run=1", "", e.cookies[authzA]), 200)
	if got := m33Steps(t, pv); got != "dbs.backup "+authzDB+": mariadb.dump" {
		t.Errorf("xem trước sao lưu = %s", got)
	}
	pv = m13Decode[engine.Preview](t, e.do(h, "POST", "/api/v1/dbs/"+authzDB+"/import?dry_run=1", `{"backup_id":"`+row.ID+`"}`, e.cookies[authzA]), 200)
	if got := m33Steps(t, pv); got != "dbs.import "+authzDB+": mariadb.dump mariadb.sdb-import" {
		t.Errorf("xem trước nhập = %s", got)
	}
	// Bản backup của site (id khác DB) không nhập được vào DB rời.
	if rec := e.do(h, "POST", "/api/v1/dbs/"+authzDB+"/import?dry_run=1", `{"backup_id":"`+e.backup+`"}`, e.cookies[authzA]); rec.Code != http.StatusNotFound || errCode(t, rec) != "backup_not_found" {
		t.Errorf("nhập bản của site vào DB rời: %d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/backups", "/import"} {
		for _, q := range []string{"", "?dry_run=1"} {
			if rec := e.do(h, "POST", "/api/v1/dbs/"+authzDB+p+q, `{"backup_id":"`+row.ID+`"}`, e.cookies[authzB]); rec.Code != http.StatusNotFound {
				t.Errorf("B gọi %s%s trên DB của A: %d %s", p, q, rec.Code, rec.Body)
			}
		}
	}
}

// POST /backups/{id}/restore với dòng của DB rời: DB còn thì nhập vào, DB đã
// xoá hẳn thì tạo lại rồi nhập; không bao giờ ghi đè DB cùng tên của người khác.
func TestM33KhoiPhucDongCuaDBRoi(t *testing.T) {
	e, h, row := newM33Env(t)
	ctx := context.Background()
	path := "/api/v1/backups/" + row.ID + "/restore"
	// B không phải chủ của bản: 404 (guard owner:backup).
	if rec := e.do(h, "POST", path+"?dry_run=1", "", e.cookies[authzB]); rec.Code != http.StatusNotFound {
		t.Errorf("B khôi phục bản của A: %d %s", rec.Code, rec.Body)
	}
	// DB đang có: nhập.
	pv := m13Decode[engine.Preview](t, e.do(h, "POST", path+"?dry_run=1", "", e.cookies[authzA]), 200)
	if got := m33Steps(t, pv); got != "dbs.import "+authzDB+": mariadb.dump mariadb.sdb-import" {
		t.Errorf("DB đang có: %s", got)
	}
	// DB ở thùng rác: từ chối, bảo khôi phục từ thùng rác trước.
	db, _, _ := e.st.StandaloneDB(ctx, authzDB)
	db.Status = account.DBTrashed
	if err := e.st.SaveStandaloneDB(ctx, db); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(h, "POST", path+"?dry_run=1", "", e.cookies[authzA]); errCode(t, rec) != "db_trashed" {
		t.Errorf("DB ở thùng rác: %d %s", rec.Code, rec.Body)
	}
	// DB đã xoá hẳn: tạo lại (đúng tên, bước tạo DB rồi bước nhập).
	if err := e.st.DeleteStandaloneDB(ctx, authzDB); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{authzA, account.AdminName} {
		pv = m13Decode[engine.Preview](t, e.do(h, "POST", path+"?dry_run=1", "", e.cookies[who]), 200)
		if got := m33Steps(t, pv); got != "dbs.recreate "+authzDB+": mariadb.standalone-db mariadb.sdb-load" {
			t.Errorf("%s tạo lại DB đã xoá hẳn: %s", who, got)
		}
	}
	// Người khác vừa tạo DB trùng tên: bản của A không được nhập đè lên.
	if err := e.st.SaveStandaloneDB(ctx, account.Database{Name: authzDB, Owner: authzB, Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci", Status: account.DBActive, Created: e.now}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"", "?dry_run=1"} {
		if rec := e.do(h, "POST", path+q, "", e.cookies[authzA]); rec.Code != http.StatusNotFound || errCode(t, rec) != "backup_not_found" {
			t.Errorf("A nhập bản của mình vào DB của B%s: %d %s", q, rec.Code, rec.Body)
		}
	}
	// Admin cũng không: core so chủ của bản với chủ của DB.
	if rec := e.do(h, "POST", path+"?dry_run=1", "", e.cookies[account.AdminName]); errCode(t, rec) != "backup_not_found" {
		t.Errorf("admin nhập bản của A vào DB của B: %d %s", rec.Code, rec.Body)
	}
}
