package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// newM34Env: authzEnv (A có site a.example.com và DB rời usera_shop) trên một
// MariaDB giả mà gone quyết định DB nào đã bị xoá ngoài tpan. User DB luôn mất
// cùng DB (trường hợp tệ nhất).
func newM34Env(t *testing.T, gone map[string]bool) (*authzEnv, http.Handler) {
	t.Helper()
	e := newAuthzEnv(t)
	e.f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		sql := string(c.Stdin)
		switch {
		case strings.Contains(sql, "mysql.global_priv") && strings.Contains(sql, "SCHEMATA"):
			for name := range gone {
				if strings.Contains(sql, "SCHEMA_NAME='"+name+"'") {
					return system.Result{Stdout: "0\t0\n"}
				}
			}
			return system.Result{Stdout: "1\t1\n"}
		case strings.Contains(sql, "DEFAULT_COLLATION_NAME") && strings.Contains(sql, "GROUP BY"):
			// Câu của màn danh sách: mỗi DB còn trong MariaDB một dòng.
			var out strings.Builder
			for _, name := range []string{site.LayoutOf(apiDomain(t, authzSite)).DB, authzDB} {
				if strings.Contains(sql, "'"+name+"'") && !gone[name] {
					out.WriteString(name + "\t3\t49152\tutf8mb4\tutf8mb4_general_ci\n")
				}
			}
			return system.Result{Stdout: out.String()}
		}
		return system.Result{}
	})
	return e, e.s.handler(authzDist)
}

// DB rời đã bị xoá ngoài tpan, nhìn từ API: danh sách đánh dấu missing; màn
// quản lý dữ liệu nói rõ thay vì hiện một DB trống; tạm khoá từ chối; DELETE
// thành gỡ khỏi tpan (xem trước dbs.forget), với chủ DB cũng như admin.
func TestM34DBRoiDaMatQuaAPI(t *testing.T) {
	e, h := newM34Env(t, map[string]bool{authzDB: true})

	rows := m13Decode[[]DatabaseRow](t, e.do(h, "GET", "/api/v1/databases", "", e.cookies[authzA]), 200)
	seen := false
	for _, r := range rows {
		if r.DB == authzDB {
			seen = true
			if !r.Missing || r.Tables != -1 {
				t.Errorf("dòng của DB đã mất = %+v; muốn missing và không có số liệu", r)
			}
		} else if r.Missing {
			t.Errorf("DB còn mà bị đánh dấu missing: %+v", r)
		}
	}
	if !seen {
		t.Fatalf("danh sách không có %s: %+v", authzDB, rows)
	}

	rec := e.do(h, "GET", "/api/v1/dbs/"+authzDB+"/data", "", e.cookies[authzA])
	if rec.Code != http.StatusConflict || errCode(t, rec) != "db_missing" || !strings.Contains(rec.Body.String(), "tpan db rm "+authzDB) {
		t.Errorf("màn quản lý dữ liệu của DB đã mất: %d %s", rec.Code, rec.Body)
	}
	rec = e.do(h, "POST", "/api/v1/dbs/"+authzDB+"/suspend?dry_run=1", "", e.cookies["admin"])
	if rec.Code != http.StatusConflict || errCode(t, rec) != "db_missing" {
		t.Errorf("tạm khoá DB đã mất: %d %s", rec.Code, rec.Body)
	}
	rec = e.do(h, "POST", "/api/v1/dbs/"+authzDB+"/password?dry_run=1", "", e.cookies[authzA])
	if rec.Code != http.StatusConflict || errCode(t, rec) != "db_missing" {
		t.Errorf("đổi mật khẩu DB đã mất: %d %s", rec.Code, rec.Body)
	}

	for _, who := range []string{authzA, "admin"} {
		pv := m13Decode[engine.Preview](t, e.do(h, "DELETE", "/api/v1/dbs/"+authzDB+"?dry_run=1", "", e.cookies[who]), 200)
		if got := m33Steps(t, pv); got != "dbs.forget "+authzDB+": mariadb.user-absent mariadb.trash-dump-absent" {
			t.Errorf("%s xem trước xoá DB đã mất = %s", who, got)
		}
		if !strings.Contains(pv.Title.Vi, "không còn trong MariaDB") {
			t.Errorf("tiêu đề bản xem trước = %q", pv.Title.Vi)
		}
	}
	// Khách khác vẫn nhận 404 như DB không có: không lộ là DB này tồn tại hay đã mất.
	if rec := e.do(h, "DELETE", "/api/v1/dbs/"+authzDB+"?dry_run=1", "", e.cookies[authzB]); rec.Code != http.StatusNotFound {
		t.Errorf("khách khác xoá DB đã mất của A: %d, muốn 404", rec.Code)
	}
}

// DB của site đã bị xoá ngoài tpan: danh sách đánh dấu missing, màn quản lý dữ
// liệu chỉ đường khôi phục từ backup (không phải "gỡ": DB của site không gỡ riêng được).
func TestM34DBCuaSiteDaMatQuaAPI(t *testing.T) {
	siteDB := site.LayoutOf(apiDomain(t, authzSite)).DB
	e, h := newM34Env(t, map[string]bool{siteDB: true})
	rows := m13Decode[[]DatabaseRow](t, e.do(h, "GET", "/api/v1/databases", "", e.cookies[authzA]), 200)
	for _, r := range rows {
		if (r.DB == siteDB) != r.Missing {
			t.Errorf("missing của %s = %v", r.DB, r.Missing)
		}
	}
	rec := e.do(h, "GET", "/api/v1/databases/"+authzSite+"/data", "", e.cookies[authzA])
	if rec.Code != http.StatusConflict || errCode(t, rec) != "db_missing" || !strings.Contains(rec.Body.String(), "bản backup") || strings.Contains(rec.Body.String(), "tpan db rm") {
		t.Errorf("màn quản lý dữ liệu của DB site đã mất: %d %s", rec.Code, rec.Body)
	}
}

// DB còn nguyên thì các đường trên không đổi (không có dòng missing nào, màn dữ liệu mở được).
func TestM34DBConNguyenKhongDoi(t *testing.T) {
	e, h := newM34Env(t, nil)
	for _, r := range m13Decode[[]DatabaseRow](t, e.do(h, "GET", "/api/v1/databases", "", e.cookies[authzA]), 200) {
		if r.Missing {
			t.Errorf("DB còn mà missing: %+v", r)
		}
	}
	if rec := e.do(h, "GET", "/api/v1/dbs/"+authzDB+"/data", "", e.cookies[authzA]); rec.Code == http.StatusConflict && errCode(t, rec) == "db_missing" {
		t.Errorf("DB còn mà màn dữ liệu báo db_missing: %s", rec.Body)
	}
	pv := m13Decode[engine.Preview](t, e.do(h, "DELETE", "/api/v1/dbs/"+authzDB+"?dry_run=1", "", e.cookies[authzA]), 200)
	if pv.Kind != "dbs.trash" {
		t.Errorf("DB còn mà xoá không phải đưa vào thùng rác: %s", pv.Kind)
	}
}
