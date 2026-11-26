package server

// API quản lý dữ liệu của một DB (M32, v0.2.14; lõi ở core/dbdata.go, đọc mô
// hình an toàn ở đầu file đó). Hai họ đường dẫn cho cùng một bộ handler, để
// dùng lại đúng hai luật phân quyền đã có và đã test:
//
//	DB của site:  /api/v1/databases/{domain}/...   luật ownerSite
//	DB rời:       /api/v1/dbs/{name}/...           luật ownerDB
//
//	GET  .../data                    → DBInfo (nối vào bằng gì, danh sách bảng)
//	GET  .../data/{table}?offset=&limit=&order=&dir=desc → core.DBRowsPage
//	GET  .../data/{table}?row=1&k.<cột khoá>=<giá trị>   → đúng một dòng, ô không cắt
//	POST .../data/{table}            core.DBRowChange → DBChanged
//	POST .../data/{table}/maintain   DBMaintain → core.DBSQLResult
//	POST .../sql                     DBSQL → core.DBSQLResult
//
// Nhóm token: databases (GET cần phạm vi đọc, POST cần phạm vi ghi). Phiên "xem
// như" chỉ GET được, như mọi route khác. Mỗi lần ghi và mỗi lần chạy SQL để lại
// một dòng nhật ký; câu SQL không vào nhật ký (có thể chứa dữ liệu của khách),
// chỉ có từ đầu của câu và độ dài.

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// DBInfo là câu trả lời của GET .../data.
type DBInfo struct {
	DB   string `json:"db"`
	User string `json:"user"`
	// Kind: "site" (DB đi kèm một site) hoặc "standalone" (DB rời).
	Kind      string         `json:"kind"`
	Domain    string         `json:"domain,omitempty"`
	Owner     string         `json:"owner"`
	Suspended bool           `json:"suspended"`
	Conn      core.DBConn    `json:"conn"`
	Tables    []core.DBTable `json:"tables"`
	// Limits: trần của một lần chạy, để giao diện nói trước.
	MaxRows    int `json:"max_rows"`
	MaxSeconds int `json:"max_seconds"`
}

// DBChanged là câu trả lời của POST .../data/{table}.
type DBChanged struct {
	Affected int64 `json:"affected"`
}

// DBMaintain là thân của POST .../data/{table}/maintain.
type DBMaintain struct {
	Op string `json:"op"` // check | repair | optimize | analyze
}

// DBSQL là thân của POST .../sql.
type DBSQL struct {
	SQL string `json:"sql"`
}

// dbRef là DB mà request chỉ tới, đã qua luật phân quyền của route.
type dbRef struct {
	db, kind, domain, owner string
	suspended               bool
}

func (s *Server) apiDBData() *core.DBData { return &core.DBData{Sys: s.deps.Sys} }

func dbDataNotFound() *core.RefusedError {
	return &core.RefusedError{Code: "not_found", Message: i18n.T("không có cơ sở dữ liệu này", "no such database")}
}

// dbRefOf tìm DB theo {domain} (DB của site đang chạy) hoặc {name} (DB rời
// không ở thùng rác). Luật của route đã kiểm quyền; ở đây chỉ đọc dòng.
func (s *Server) dbRefOf(ctx context.Context, r *http.Request) (dbRef, error) {
	if raw := r.PathValue("domain"); raw != "" {
		d, err := validate.ParseDomain(raw)
		if err != nil {
			return dbRef{}, err
		}
		st, ok, err := s.deps.Store.Site(ctx, d)
		if err != nil {
			return dbRef{}, err
		}
		if !ok || st.Status != site.Active {
			return dbRef{}, dbDataNotFound()
		}
		owner := st.Owner
		if owner == "" {
			owner = account.AdminName
		}
		return dbRef{db: st.Layout().DB, kind: "site", domain: d.String(), owner: owner, suspended: st.Suspended != "" || st.DBSuspended != ""}, nil
	}
	name := r.PathValue("name")
	if !dbNamePathRe.MatchString(name) {
		return dbRef{}, dbDataNotFound()
	}
	db, ok, err := s.deps.Store.StandaloneDB(ctx, name)
	if err != nil {
		return dbRef{}, err
	}
	if !ok || db.Status == account.DBTrashed {
		return dbRef{}, dbDataNotFound()
	}
	return dbRef{db: db.Name, kind: "standalone", owner: db.Owner, suspended: db.Suspended != ""}, nil
}

// dbRefWritable: như dbRefOf, thêm luật "DB hay site đang tạm khoá thì user
// thường không đọc ghi dữ liệu được" (khoá là để dừng mọi truy cập của khách;
// tài khoản tạm của panel không được thành đường vòng). Admin vẫn vào được.
func (s *Server) dbRefActive(w http.ResponseWriter, r *http.Request) (dbRef, bool) {
	ref, err := s.dbRefOf(r.Context(), r)
	if err != nil {
		apiFail(w, r, err)
		return dbRef{}, false
	}
	if ref.suspended && who(r).Role != account.RoleAdmin {
		apiFail(w, r, &core.RefusedError{Code: core.CodeDBSuspended, Message: i18n.T(
			"cơ sở dữ liệu này đang tạm khoá; liên hệ quản trị để mở khoá",
			"this database is suspended; ask the administrator to unsuspend it")})
		return dbRef{}, false
	}
	return ref, true
}

func (s *Server) handleDBDataInfo(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.dbRefActive(w, r)
	if !ok {
		return
	}
	// M34: DB đã bị xoá ngoài tpan thì nói rõ, đừng hiện như một DB trống.
	if gone := core.DBGoneRefusal(r.Context(), s.deps.Sys, ref.db, ref.domain); gone != nil {
		apiFail(w, r, gone)
		return
	}
	d := s.apiDBData()
	tables, err := d.Tables(r.Context(), ref.db)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	conn, err := d.Conn(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DBInfo{
		DB: ref.db, User: ref.db, Kind: ref.kind, Domain: ref.domain, Owner: ref.owner, Suspended: ref.suspended,
		Conn: conn, Tables: apiNonNil(tables), MaxRows: core.DBDataMaxRows, MaxSeconds: core.DBDataMaxSeconds,
	})
}

func (s *Server) handleDBDataRows(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.dbRefActive(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	table := r.PathValue("table")
	d := s.apiDBData()
	if q.Get("row") == "1" {
		// Một dòng theo khoá chính, ô không cắt: form sửa.
		key := map[string]*string{}
		for name, vals := range q {
			if col, found := strings.CutPrefix(name, "k."); found && len(vals) > 0 {
				v := vals[0]
				key[col] = &v
			}
		}
		page, err := d.Row(r.Context(), ref.db, table, key)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := d.Rows(r.Context(), ref.db, table, offset, limit, q.Get("order"), q.Get("dir") == "desc", false)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleDBDataChange(w http.ResponseWriter, r *http.Request) {
	var body core.DBRowChange
	if !apiReadJSONMax(w, r, &body, 2<<20) {
		return
	}
	ref, ok := s.dbRefActive(w, r)
	if !ok {
		return
	}
	table := r.PathValue("table")
	n, err := s.apiDBData().Change(r.Context(), ref.db, table, body)
	if !s.auditDone(w, r, "db.row."+auditWord(body.Op), ref.db, err) {
		return
	}
	writeJSON(w, http.StatusOK, DBChanged{Affected: n})
}

func (s *Server) handleDBDataMaintain(w http.ResponseWriter, r *http.Request) {
	var body DBMaintain
	if !apiReadJSON(w, r, &body) {
		return
	}
	ref, ok := s.dbRefActive(w, r)
	if !ok {
		return
	}
	res, err := s.apiDBData().Maintain(r.Context(), ref.db, r.PathValue("table"), body.Op)
	if !s.auditDone(w, r, "db.table."+auditWord(body.Op), ref.db, err) {
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDBDataSQL(w http.ResponseWriter, r *http.Request) {
	var body DBSQL
	if !apiReadJSONMax(w, r, &body, 2*core.DBDataMaxSQL) {
		return
	}
	ref, ok := s.dbRefActive(w, r)
	if !ok {
		return
	}
	res, err := s.apiDBData().SQL(r.Context(), ref.db, body.SQL)
	// Nhật ký: từ đầu của câu (SELECT, UPDATE...) và độ dài, không ghi câu SQL.
	verb := auditWord(strings.ToLower(firstWord(body.SQL)))
	if err == nil && res.Error != "" {
		s.audit(r, "db.sql."+verb, ref.db, "sql_error", "")
	} else if !s.auditDone(w, r, "db.sql."+verb, ref.db, err) {
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// firstWord là từ đầu tiên của câu SQL (bỏ khoảng trắng đầu).
func firstWord(sql string) string {
	f := strings.Fields(sql)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// auditWord giữ một từ ngắn chỉ gồm chữ thường cho tên việc trong nhật ký; thứ
// khác (chữ lạ người gọi gửi) thành "khac".
func auditWord(w string) string {
	if w == "" || len(w) > 16 {
		return "khac"
	}
	for _, c := range w {
		if c < 'a' || c > 'z' {
			return "khac"
		}
	}
	return w
}
