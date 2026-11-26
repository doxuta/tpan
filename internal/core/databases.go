package core

// Danh sách DB của các site cho màn "Cơ sở dữ liệu" (mục 6.9 của
// docs/KE-HOACH.md). M3 chỉ xem: tên DB, user, site, dung lượng, số bảng.
//
// Câu SQL theo tài liệu MariaDB (đã tra 19/11/2026):
//   - information_schema.TABLES:   https://mariadb.com/kb/en/information-schema-tables-table/
//   - information_schema.SCHEMATA: https://mariadb.com/kb/en/information-schema-schemata-table/
// DATA_LENGTH + INDEX_LENGTH với InnoDB là số trang nhân cỡ trang, tức là byte
// đĩa đã cấp, không phải byte dữ liệu thật; đủ cho một cột "dung lượng".

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/system"
)

// Database là một dòng của màn Cơ sở dữ liệu. server đổi sang server.DatabaseRow.
type Database struct {
	Domain    string
	DB        string
	User      string // user DB trùng tên DB (mục 5): 'tp_<slug>'@'localhost'
	SizeBytes int64  // -1 khi không đọc được
	Tables    int    // -1 khi không đọc được
	// Từ M13 (DBs.List điền). Kind "site" hoặc "standalone" (Domain rỗng).
	Kind      string
	Owner     string
	Charset   string
	Collation string
	Note      string
	Suspended account.Suspension
	Status    string // "active" | "trashed"
	// App (M29): loại site của DB đi kèm site ("wordpress" | "php"); DB rời để rỗng.
	App string
}

// databaseNameRe là lớp chặn cuối trước khi tên DB đi vào chuỗi SQL. Tên lấy
// từ site.LayoutOf nên luôn khớp; kiểm lại để một lỗi ở chỗ khác không thành
// lỗ chèn SQL ở đây.
var databaseNameRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// Databases trả DB của mọi site trong bảng sites (kể cả site trong thùng rác:
// DB của nó vẫn còn), sắp theo domain. Dung lượng và số bảng đọc bằng đúng một
// câu SQL. Không đọc được (MariaDB dừng, DB đã mất) thì hai cột đó là -1 chứ
// không báo lỗi cả màn: danh sách site vẫn đúng, chỉ số liệu là thiếu. error
// chỉ khác nil khi không đọc được bảng sites.
func (s *Sites) Databases(ctx context.Context) ([]Database, error) {
	all, err := s.Store.Sites(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Database, 0, len(all))
	var names []string
	for _, st := range all {
		l := st.Layout()
		out = append(out, Database{Domain: st.Domain.String(), DB: l.DB, User: l.DB, SizeBytes: -1, Tables: -1})
		if databaseNameRe.MatchString(l.DB) {
			names = append(names, l.DB)
		}
	}
	if len(names) == 0 {
		return out, nil
	}
	res, err := s.Sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(databaseSizeSQL(names))})
	if err != nil {
		return out, nil
	}
	stats := parseDatabaseStats(res.Stdout)
	for i := range out {
		if st, ok := stats[out[i].DB]; ok {
			out[i].Tables, out[i].SizeBytes = st.tables, st.size
		}
	}
	return out, nil
}

// databaseSizeSQL trả một câu: mỗi DB có trong names một dòng "tên<TAB>số bảng<TAB>byte".
// So bằng IN chứ không dùng LIKE 'tp_%': trong LIKE dấu "_" là ký tự đại diện.
// LEFT JOIN từ SCHEMATA để DB chưa có bảng nào vẫn ra một dòng (0 bảng);
// DB không tồn tại thì không có dòng nào và giữ -1.
func databaseSizeSQL(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	return "SELECT s.SCHEMA_NAME, COUNT(t.TABLE_NAME), COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH), 0) " +
		"FROM information_schema.SCHEMATA s " +
		"LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = s.SCHEMA_NAME AND t.TABLE_TYPE IN ('BASE TABLE', 'SYSTEM VERSIONED') " +
		"WHERE s.SCHEMA_NAME IN (" + strings.Join(quoted, ", ") + ") " +
		"GROUP BY s.SCHEMA_NAME;\n"
}

type databaseStat struct {
	tables int
	size   int64
}

// parseDatabaseStats đọc output của `mariadb -N -B` (cột cách nhau bằng tab).
// Dòng nào lạ thì bỏ qua: DB đó giữ -1.
func parseDatabaseStats(out string) map[string]databaseStat {
	m := map[string]databaseStat{}
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
		if len(f) != 3 {
			continue
		}
		n, err1 := strconv.Atoi(f[1])
		size, err2 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		m[f[0]] = databaseStat{tables: n, size: size}
	}
	return m
}

// Kind của một dòng Database (M13).
const (
	DBKindSite       = "site"
	DBKindStandalone = "standalone"
)

// List là DB của site và DB rời của owner (rỗng là của mọi người), cho màn
// Cơ sở dữ liệu. Thay cho Sites.Databases ở server. Như Databases: dung lượng,
// số bảng, charset và collation đọc bằng đúng một câu SQL; MariaDB không trả
// lời thì các cột đó là -1 (charset của DB rời lấy từ bảng databases), không
// báo lỗi cả màn. DB rời trong thùng rác đã bị DROP nên luôn là -1.
func (d *DBs) List(ctx context.Context, owner string) ([]Database, error) {
	out := []Database{}
	if d.Sites != nil {
		all, err := d.Sites.Sites(ctx)
		if err != nil {
			return nil, fmt.Errorf("đọc danh sách site: %w", err)
		}
		for _, st := range all {
			if owner != "" && st.Owner != owner {
				continue
			}
			l := st.Layout()
			out = append(out, Database{
				Domain: st.Domain.String(), DB: l.DB, User: l.DB, SizeBytes: -1, Tables: -1,
				Kind: DBKindSite, Owner: st.Owner, Suspended: st.DBSuspended, Status: string(st.Status),
				App: string(st.App),
			})
		}
	}
	if d.Store != nil {
		all, err := d.Store.StandaloneDBs(ctx, owner)
		if err != nil {
			return nil, fmt.Errorf("đọc danh sách DB rời: %w", err)
		}
		for _, db := range all {
			out = append(out, Database{
				DB: db.Name, User: db.Name, SizeBytes: -1, Tables: -1, Kind: DBKindStandalone,
				Owner: db.Owner, Charset: db.Charset, Collation: db.Collation, Note: db.Note,
				Suspended: db.Suspended, Status: string(db.Status),
			})
		}
	}
	var names []string
	for _, r := range out {
		if databaseNameRe.MatchString(r.DB) {
			names = append(names, r.DB)
		}
	}
	if len(names) == 0 {
		return out, nil
	}
	res, err := d.Sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(databaseListSQL(names))})
	if err != nil {
		return out, nil
	}
	stats := parseDatabaseList(res.Stdout)
	for i := range out {
		if st, ok := stats[out[i].DB]; ok {
			out[i].Tables, out[i].SizeBytes = st.tables, st.size
			out[i].Charset, out[i].Collation = st.charset, st.collation
		}
	}
	return out, nil
}

// databaseListSQL như databaseSizeSQL, thêm charset và collation mặc định của
// DB (SCHEMATA). Năm cột: tên, số bảng, byte, charset, collation.
func databaseListSQL(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	return "SELECT s.SCHEMA_NAME, COUNT(t.TABLE_NAME), COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH), 0), " +
		"s.DEFAULT_CHARACTER_SET_NAME, s.DEFAULT_COLLATION_NAME " +
		"FROM information_schema.SCHEMATA s " +
		"LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = s.SCHEMA_NAME AND t.TABLE_TYPE IN ('BASE TABLE', 'SYSTEM VERSIONED') " +
		"WHERE s.SCHEMA_NAME IN (" + strings.Join(quoted, ", ") + ") " +
		"GROUP BY s.SCHEMA_NAME, s.DEFAULT_CHARACTER_SET_NAME, s.DEFAULT_COLLATION_NAME;\n"
}

type databaseListStat struct {
	databaseStat
	charset, collation string
}

// parseDatabaseList đọc năm cột của databaseListSQL; dòng lạ thì bỏ.
func parseDatabaseList(out string) map[string]databaseListStat {
	m := map[string]databaseListStat{}
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
		if len(f) != 5 {
			continue
		}
		n, err1 := strconv.Atoi(f[1])
		size, err2 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		m[f[0]] = databaseListStat{databaseStat{tables: n, size: size}, f[3], f[4]}
	}
	return m
}
