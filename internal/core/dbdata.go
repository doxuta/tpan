package core

// Quản lý dữ liệu của một DB từ panel (M32, v0.2.14): danh sách bảng, xem và
// sửa dòng, công cụ bảng (kiểm tra, sửa, tối ưu), và chạy một câu SQL.
//
// MÔ HÌNH AN TOÀN. Khách hàng gõ SQL tuỳ ý, nên chỗ này phải chặn ba đường:
//
//  1. Chạm DB của người khác. Câu SQL không bao giờ chạy bằng root. Mỗi lần
//     chạy, tpan (bằng root, qua socket) tạo một tài khoản MariaDB tạm chỉ có
//     quyền trên đúng DB đó (GRANT ... ON `db`.*, dấu "_" trong tên được thoát
//     như steps.dbGrantSQL), chạy câu SQL bằng tài khoản ấy, rồi xoá tài khoản.
//     Việc "được đụng vào DB nào" do chính MariaDB quyết, không do tpan đọc câu
//     SQL rồi đoán. Tài khoản không có quyền toàn cục nào (FILE, SUPER, GRANT...),
//     nên SELECT ... INTO OUTFILE, LOAD_FILE, SET GLOBAL, CREATE USER đều bị từ chối.
//
//  2. Lệnh của chương trình mariadb. Client `mariadb` hiểu vài lệnh riêng trong
//     đầu vào, có cả lệnh chạy shell (`\!`, `system`), mà client chạy bằng root.
//     Vì vậy chữ người dùng gõ KHÔNG BAO GIỜ đi vào đầu vào của client ở dạng
//     chữ: nó được mã hoá hex và chạy bằng EXECUTE IMMEDIATE trên máy chủ
//     (`SET @q = CONVERT(0x... USING utf8mb4); EXECUTE IMMEDIATE @q;`). Client
//     chỉ thấy chữ số hex. Tên bảng, tên cột, giá trị ô cũng đi theo đường hex đó.
//
//  3. Đọc file trên máy qua LOAD DATA LOCAL. Client chạy với --local-infile=0,
//     và LOAD DATA không chạy được trong EXECUTE IMMEDIATE.
//
// Mật khẩu của tài khoản tạm nằm trong một file 0600 của root dưới /run/tpan
// (tmpfs), truyền bằng --defaults-extra-file, không bao giờ nằm trên dòng lệnh.
// Mỗi lần chạy có trần: 500 dòng trả về, 30 giây, 8 MiB kết quả.
//
// Không qua engine.Plan: đây là đọc và sửa DỮ LIỆU của khách (như phpMyAdmin),
// không phải đổi cấu hình máy; mỗi lần ghi vẫn để lại một dòng nhật ký (server).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Trần của một lần chạy.
const (
	DBDataMaxRows    = 500
	DBDataMaxSeconds = 30
	DBDataMaxOutput  = 8 << 20
	DBDataMaxSQL     = 64 << 10
	// DBDataCellClip: trong lưới, ô dài hơn chừng này ký tự bị cắt (Clipped);
	// form sửa đọc lại nguyên dòng bằng khoá chính.
	DBDataCellClip = 300
	// DBDataMaxCell: ô lớn hơn chừng này byte thì không sửa được từ form.
	DBDataMaxCell = 256 << 10
	dbDataPageMax = 200
)

// Mã từ chối.
const (
	CodeDBTableNotFound = "db_table_not_found"
	CodeDBColumnUnknown = "db_column_unknown"
	CodeDBNoPrimaryKey  = "db_no_primary_key"
	CodeDBSQLInvalid    = "db_sql_invalid"
	CodeDBSQLBlocked    = "db_sql_blocked"
	CodeDBOpInvalid     = "db_op_invalid"
	CodeDBKeyInvalid    = "db_key_invalid"
)

var dbDataNameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// DBData chạy các việc dữ liệu trên một DB. Dựng mới cho mỗi request; Sys
// KHÔNG phải là bản giữ khoá job (đọc và sửa dữ liệu không đổi cấu hình máy).
type DBData struct {
	Sys system.System
	// Rand trả n byte ngẫu nhiên; nil là crypto/rand. Test đặt để có tên file
	// và mật khẩu cố định.
	Rand func(n int) []byte
}

// dbDataMu: mỗi lúc một lần chạy. Tài khoản tạm của một DB có tên cố định, hai
// lần chạy chồng nhau sẽ xoá tài khoản của nhau.
var dbDataMu sync.Mutex

// DBTable là một bảng (hoặc view) của DB.
type DBTable struct {
	Name      string `json:"name"`
	Engine    string `json:"engine"`
	Rows      int64  `json:"rows"` // ước lượng của MariaDB (information_schema)
	Bytes     int64  `json:"bytes"`
	Collation string `json:"collation"`
	View      bool   `json:"view"`
}

// DBConn là thông tin để một ứng dụng nối vào DB.
type DBConn struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Socket string `json:"socket"`
}

// DBColumn là một cột của bảng.
type DBColumn struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable bool    `json:"nullable"`
	Key      string  `json:"key"` // PRI, UNI, MUL hoặc rỗng
	Default  *string `json:"default"`
	Extra    string  `json:"extra"`
	// Binary: cột nhị phân (BLOB, BINARY, BIT...). Giá trị đi và về ở dạng 0x<hex>.
	Binary bool `json:"binary"`
}

// DBRowsPage là một trang dòng của một bảng.
type DBRowsPage struct {
	Table   string      `json:"table"`
	Columns []DBColumn  `json:"columns"`
	Primary []string    `json:"primary"`
	Rows    [][]*string `json:"rows"`
	// Clipped[i][j]: ô đã bị cắt bớt cho lưới.
	Clipped [][]bool `json:"clipped"`
	Offset  int      `json:"offset"`
	Limit   int      `json:"limit"`
	More    bool     `json:"more"`
}

// DBRowChange là một thay đổi trên một dòng.
type DBRowChange struct {
	Op     string             `json:"op"` // insert | update | delete
	Key    map[string]*string `json:"key,omitempty"`
	Values map[string]*string `json:"values,omitempty"`
}

// DBSQLResult là kết quả một câu SQL. Error khác rỗng: MariaDB từ chối câu đó
// (lỗi của câu SQL, không phải lỗi của panel).
type DBSQLResult struct {
	Columns   []string    `json:"columns"`
	Rows      [][]*string `json:"rows"`
	Affected  int64       `json:"affected"`
	Truncated bool        `json:"truncated"`
	Error     string      `json:"error,omitempty"`
	Millis    int64       `json:"ms"`
}

// ---------------------------------------------------------------- dựng SQL

func (d *DBData) random(n int) []byte {
	if d.Rand != nil {
		return d.Rand(n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return b
}

// sqlText là một chuỗi ở dạng hex: không ký tự nào của s xuất hiện trong câu SQL.
func sqlText(s string) string {
	if s == "" {
		return "''"
	}
	return "CONVERT(0x" + hex.EncodeToString([]byte(s)) + " USING utf8mb4)"
}

// sqlIdent bọc một tên bảng hay cột bằng dấu ` (dấu ` trong tên được gấp đôi).
func sqlIdent(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

var hexValueRe = regexp.MustCompile(`^0[xX]([0-9a-fA-F]{2})*$`)

// sqlValue là giá trị một ô: nil là NULL; cột nhị phân nhận 0x<hex>; còn lại là chuỗi.
func sqlValue(col DBColumn, v *string) (string, error) {
	if v == nil {
		return "NULL", nil
	}
	if col.Binary {
		if !hexValueRe.MatchString(*v) {
			return "", refuse(CodeDBKeyInvalid, i18n.T(
				"cột "+col.Name+" là cột nhị phân: giá trị phải ở dạng 0x theo sau là các cặp chữ số hex, ví dụ 0x4a6f",
				"column "+col.Name+" is binary: the value must be 0x followed by pairs of hex digits, for example 0x4a6f"))
		}
		if len(*v) == 2 {
			return "''", nil
		}
		return "0x" + (*v)[2:], nil
	}
	return sqlText(*v), nil
}

// dbAccount là tên tài khoản tạm của một DB: cố định theo tên DB, để lần chạy
// sau thay được tài khoản còn sót của lần trước (CREATE OR REPLACE USER).
func dbAccount(db string) string {
	sum := sha256.Sum256([]byte(db))
	return "tpq_" + hex.EncodeToString(sum[:8])
}

func checkDBName(db string) error {
	if !dbDataNameRe.MatchString(db) {
		return fmt.Errorf("tên DB %q không hợp lệ", db)
	}
	return nil
}

// ---------------------------------------------------------------- chạy

// rootRows chạy một câu do tpan tự viết bằng root, trả các dòng đã tách cột.
func (d *DBData) rootRows(ctx context.Context, sql string) ([][]string, error) {
	res, err := d.Sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(sql)})
	if err != nil {
		return nil, fmt.Errorf("hỏi MariaDB: %w", err)
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		for i := range f {
			f[i] = batchUnescape(f[i])
		}
		rows = append(rows, f)
	}
	return rows, nil
}

// batchUnescape bỏ lớp thoát của `mariadb -B`: \t, \n, \0, \\.
func batchUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case '0':
			b.WriteByte(0)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// sqlErrRe lấy câu lỗi của MariaDB khỏi stderr của client:
// "ERROR 1146 (42S02) at line 3: Table 'x.y' doesn't exist".
var sqlErrRe = regexp.MustCompile(`ERROR (\d+) \(([0-9A-Z]+)\)(?: at line \d+)?: (.*)`)

// restricted chạy đúng một câu stmt trên DB db bằng tài khoản tạm, tối đa limit
// dòng trả về (cộng một dòng để biết còn nữa). Trả XML của client; sqlErr khác
// rỗng là MariaDB từ chối câu đó.
func (d *DBData) restricted(ctx context.Context, db, stmt string, limit int) (out []byte, sqlErr string, err error) {
	if err := checkDBName(db); err != nil {
		return nil, "", err
	}
	dbDataMu.Lock()
	defer dbDataMu.Unlock()

	acct := dbAccount(db)
	pw := hex.EncodeToString(d.random(24))
	file := "/run/tpan/dbq-" + hex.EncodeToString(d.random(8)) + ".cnf"
	grantDB := strings.ReplaceAll(db, "_", `\_`)
	setup := "CREATE OR REPLACE USER '" + acct + "'@'localhost' IDENTIFIED BY '" + pw + "' WITH MAX_USER_CONNECTIONS 2;\n" +
		"GRANT ALL PRIVILEGES ON `" + grantDB + "`.* TO '" + acct + "'@'localhost';\n"
	if _, err := d.Sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(setup)}); err != nil {
		return nil, "", fmt.Errorf("tạo tài khoản tạm cho DB %s: %w", db, err)
	}
	defer func() {
		// Dọn bằng context riêng: request bị huỷ giữa chừng vẫn phải xoá tài khoản.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = d.Sys.Remove(file)
		if _, derr := d.Sys.Run(cctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte("DROP USER IF EXISTS '" + acct + "'@'localhost';\n")}); derr != nil && err == nil {
			err = fmt.Errorf("xoá tài khoản tạm của DB %s: %w", db, derr)
		}
	}()
	if err := d.Sys.MkdirAll("/run/tpan", 0o700, system.Root); err != nil {
		return nil, "", fmt.Errorf("tạo /run/tpan: %w", err)
	}
	if err := d.Sys.WriteFileAtomic(file, []byte("[client]\nuser="+acct+"\npassword="+pw+"\n"), 0o600, system.Root); err != nil {
		return nil, "", fmt.Errorf("ghi %s: %w", file, err)
	}
	script := "SET SESSION sql_select_limit=" + strconv.Itoa(limit+1) + ", max_statement_time=" + strconv.Itoa(DBDataMaxSeconds) + ";\n" +
		"SET @tpq = " + sqlText(stmt) + ";\n" +
		"EXECUTE IMMEDIATE @tpq;\n" +
		"SELECT ROW_COUNT() AS tpq_affected;\n"
	rctx, cancel := context.WithTimeout(ctx, (DBDataMaxSeconds+10)*time.Second)
	defer cancel()
	res, rerr := d.Sys.Run(rctx, system.Cmd{Name: "mariadb", Args: []string{
		"--defaults-extra-file=" + file, "--database=" + db, "--xml", "--binary-as-hex", "--local-infile=0",
		"--default-character-set=utf8mb4", "--connect-timeout=5",
	}, Stdin: []byte(script)})
	if rerr != nil {
		text := res.Stderr
		var ee *system.ExitError
		if errors.As(rerr, &ee) && text == "" {
			text = ee.Stderr
		}
		if m := sqlErrRe.FindStringSubmatch(text); m != nil {
			// Tên tài khoản tạm không nói gì với người dùng: thay bằng chữ thường.
			msg := strings.ReplaceAll(strings.TrimSpace(m[3]), "'"+acct+"'@'localhost'", "this database's account")
			return []byte(res.Stdout), "MariaDB " + m[1] + " (" + m[2] + "): " + msg, nil
		}
		return nil, "", fmt.Errorf("chạy SQL trên DB %s: %w", db, rerr)
	}
	if len(res.Stdout) > DBDataMaxOutput {
		return nil, "", refuse(CodeDBSQLInvalid, i18n.T(
			"kết quả lớn hơn 8 MiB; thêm điều kiện WHERE hoặc chọn ít cột hơn",
			"the result is larger than 8 MiB; add a WHERE condition or select fewer columns"))
	}
	return []byte(res.Stdout), "", nil
}

// ---------------------------------------------------------------- đọc XML

type xmlField struct {
	Name string `xml:"name,attr"`
	Nil  string `xml:"http://www.w3.org/2001/XMLSchema-instance nil,attr"`
	Text string `xml:",chardata"`
}

type xmlResultset struct {
	Rows []struct {
		Fields []xmlField `xml:"field"`
	} `xml:"row"`
}

// parseResultsets đọc output `mariadb --xml`: mỗi câu trả kết quả là một
// <resultset>. Nhiều câu thì client in nhiều tài liệu XML nối nhau (mỗi cái có
// dòng <?xml ...?> riêng), nên đọc theo từng phần tử chứ không Unmarshal cả khối.
func parseResultsets(out []byte) ([]xmlResultset, error) {
	// Ký tự điều khiển (trừ tab, xuống dòng) không hợp lệ trong XML 1.0 mà client
	// vẫn in nguyên: đổi thành U+FFFD để bộ đọc không dừng giữa chừng.
	clean := bytes.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return utf8.RuneError
		}
		return r
	}, bytes.ToValidUTF8(out, []byte("�")))
	dec := xml.NewDecoder(bytes.NewReader(clean))
	dec.Strict = false
	var sets []xmlResultset
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return sets, nil
			}
			return nil, fmt.Errorf("đọc kết quả của MariaDB: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "resultset" {
			var rs xmlResultset
			if err := dec.DecodeElement(&rs, &se); err != nil {
				return nil, fmt.Errorf("đọc kết quả của MariaDB: %w", err)
			}
			sets = append(sets, rs)
		}
	}
}

// splitResult tách các resultset thành (kết quả của câu người dùng, số dòng bị
// đổi). Resultset cuối luôn là tpq_affected của restricted.
func splitResult(sets []xmlResultset) (cols []string, rows [][]*string, affected int64) {
	affected = -1
	if n := len(sets); n > 0 {
		last := sets[n-1]
		if len(last.Rows) == 1 && len(last.Rows[0].Fields) == 1 && last.Rows[0].Fields[0].Name == "tpq_affected" {
			affected, _ = strconv.ParseInt(strings.TrimSpace(last.Rows[0].Fields[0].Text), 10, 64)
			sets = sets[:n-1]
		}
	}
	if len(sets) == 0 {
		return nil, nil, affected
	}
	rs := sets[0]
	for i, r := range rs.Rows {
		if i == 0 {
			for _, f := range r.Fields {
				cols = append(cols, f.Name)
			}
		}
		row := make([]*string, len(r.Fields))
		for j, f := range r.Fields {
			if f.Nil == "true" {
				continue
			}
			v := f.Text
			row[j] = &v
		}
		rows = append(rows, row)
	}
	return cols, rows, affected
}

// ---------------------------------------------------------------- việc

// Conn trả host, cổng và socket của MariaDB trên máy (cho ứng dụng nối vào).
func (d *DBData) Conn(ctx context.Context) (DBConn, error) {
	rows, err := d.rootRows(ctx, "SELECT @@port, @@socket;\n")
	if err != nil {
		return DBConn{}, err
	}
	c := DBConn{Host: "localhost", Port: 3306}
	if len(rows) == 1 && len(rows[0]) == 2 {
		if p, err := strconv.Atoi(rows[0][0]); err == nil && p > 0 {
			c.Port = p
		}
		c.Socket = rows[0][1]
	}
	return c, nil
}

// Tables liệt kê bảng và view của DB, sắp theo tên.
func (d *DBData) Tables(ctx context.Context, db string) ([]DBTable, error) {
	if err := checkDBName(db); err != nil {
		return nil, err
	}
	rows, err := d.rootRows(ctx, "SELECT TABLE_NAME, IFNULL(ENGINE,''), IFNULL(TABLE_ROWS,0), IFNULL(DATA_LENGTH,0)+IFNULL(INDEX_LENGTH,0), IFNULL(TABLE_COLLATION,''), TABLE_TYPE "+
		"FROM information_schema.TABLES WHERE TABLE_SCHEMA='"+db+"' ORDER BY TABLE_NAME;\n")
	if err != nil {
		return nil, err
	}
	out := make([]DBTable, 0, len(rows))
	for _, r := range rows {
		if len(r) != 6 {
			continue
		}
		n, _ := strconv.ParseInt(r[2], 10, 64)
		b, _ := strconv.ParseInt(r[3], 10, 64)
		out = append(out, DBTable{Name: r[0], Engine: r[1], Rows: n, Bytes: b, Collation: r[4], View: r[5] == "VIEW"})
	}
	return out, nil
}

var binaryTypeRe = regexp.MustCompile(`(?i)^(tiny|medium|long)?blob|^(var)?binary|^bit\b|^geometry|^point|^linestring|^polygon`)

// Columns trả các cột của một bảng theo thứ tự; bảng không có thì CodeDBTableNotFound.
func (d *DBData) Columns(ctx context.Context, db, table string) ([]DBColumn, error) {
	if err := checkDBName(db); err != nil {
		return nil, err
	}
	if table == "" || len(table) > 64 {
		return nil, dbTableNotFound(table)
	}
	rows, err := d.rootRows(ctx, "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT IS NULL, IFNULL(COLUMN_DEFAULT,''), EXTRA "+
		"FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='"+db+"' AND TABLE_NAME="+sqlText(table)+" ORDER BY ORDINAL_POSITION;\n")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, dbTableNotFound(table)
	}
	cols := make([]DBColumn, 0, len(rows))
	for _, r := range rows {
		if len(r) != 7 {
			continue
		}
		c := DBColumn{Name: r[0], Type: r[1], Nullable: r[2] == "YES", Key: r[3], Extra: r[6], Binary: binaryTypeRe.MatchString(r[1])}
		if r[4] != "1" {
			v := r[5]
			c.Default = &v
		}
		cols = append(cols, c)
	}
	return cols, nil
}

func dbTableNotFound(table string) *RefusedError {
	return refuse(CodeDBTableNotFound, i18n.T("DB này không có bảng "+strconv.Quote(table), "this database has no table "+strconv.Quote(table)))
}

func primaryOf(cols []DBColumn) []string {
	var pk []string
	for _, c := range cols {
		if c.Key == "PRI" {
			pk = append(pk, c.Name)
		}
	}
	return pk
}

func columnByName(cols []DBColumn, name string) (DBColumn, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return DBColumn{}, false
}

// Rows trả một trang dòng của bảng. order rỗng: theo khoá chính (nếu có). full:
// không cắt ô (form sửa một dòng).
func (d *DBData) Rows(ctx context.Context, db, table string, offset, limit int, order string, desc, full bool) (DBRowsPage, error) {
	cols, err := d.Columns(ctx, db, table)
	if err != nil {
		return DBRowsPage{}, err
	}
	if limit <= 0 || limit > dbDataPageMax {
		limit = 50
	}
	offset = max(offset, 0)
	pk := primaryOf(cols)
	stmt := "SELECT * FROM " + sqlIdent(table)
	switch {
	case order != "":
		if _, ok := columnByName(cols, order); !ok {
			return DBRowsPage{}, refuse(CodeDBColumnUnknown, i18n.T("bảng "+table+" không có cột "+strconv.Quote(order), "table "+table+" has no column "+strconv.Quote(order)))
		}
		stmt += " ORDER BY " + sqlIdent(order)
		if desc {
			stmt += " DESC"
		}
	case len(pk) > 0:
		parts := make([]string, len(pk))
		for i, c := range pk {
			parts[i] = sqlIdent(c)
			if desc {
				parts[i] += " DESC"
			}
		}
		stmt += " ORDER BY " + strings.Join(parts, ", ")
	}
	stmt += " LIMIT " + strconv.Itoa(limit+1) + " OFFSET " + strconv.Itoa(offset)
	return d.page(ctx, db, table, cols, stmt, offset, limit, full)
}

// Row trả đúng một dòng theo khoá chính, ô không cắt (form sửa một dòng).
func (d *DBData) Row(ctx context.Context, db, table string, key map[string]*string) (DBRowsPage, error) {
	cols, err := d.Columns(ctx, db, table)
	if err != nil {
		return DBRowsPage{}, err
	}
	where, err := keyWhere(cols, table, key)
	if err != nil {
		return DBRowsPage{}, err
	}
	return d.page(ctx, db, table, cols, "SELECT * FROM "+sqlIdent(table)+where+" LIMIT 1", 0, 1, true)
}

func (d *DBData) page(ctx context.Context, db, table string, cols []DBColumn, stmt string, offset, limit int, full bool) (DBRowsPage, error) {
	pk := primaryOf(cols)
	out, sqlErr, err := d.restricted(ctx, db, stmt, limit)
	if err != nil {
		return DBRowsPage{}, err
	}
	if sqlErr != "" {
		return DBRowsPage{}, refuse(CodeDBSQLInvalid, i18n.T(sqlErr, sqlErr))
	}
	sets, err := parseResultsets(out)
	if err != nil {
		return DBRowsPage{}, err
	}
	_, rows, _ := splitResult(sets)
	page := DBRowsPage{Table: table, Columns: cols, Primary: pk, Offset: offset, Limit: limit, Rows: [][]*string{}, Clipped: [][]bool{}}
	if page.Primary == nil {
		page.Primary = []string{}
	}
	if len(rows) > limit {
		page.More, rows = true, rows[:limit]
	}
	for _, r := range rows {
		clip := make([]bool, len(r))
		if !full {
			for j, v := range r {
				if v != nil && utf8.RuneCountInString(*v) > DBDataCellClip {
					s := string([]rune(*v)[:DBDataCellClip])
					r[j], clip[j] = &s, true
				}
			}
		}
		page.Rows, page.Clipped = append(page.Rows, r), append(page.Clipped, clip)
	}
	return page, nil
}

// keyWhere dựng "WHERE `pk1` = ... AND `pk2` = ..." từ key; key phải có đủ và
// chỉ các cột khoá chính.
func keyWhere(cols []DBColumn, table string, key map[string]*string) (string, error) {
	pk := primaryOf(cols)
	if len(pk) == 0 {
		return "", refuse(CodeDBNoPrimaryKey, i18n.T(
			"bảng "+table+" không có khoá chính nên panel không chỉ ra được đúng một dòng để sửa hay xoá; dùng ô Chạy SQL với điều kiện WHERE của bạn",
			"table "+table+" has no primary key, so the panel cannot point at exactly one row to edit or delete; use Run SQL with your own WHERE condition"))
	}
	if len(key) != len(pk) {
		return "", refuse(CodeDBKeyInvalid, i18n.T("khoá của dòng phải gồm đúng các cột khoá chính: "+strings.Join(pk, ", "), "the row key must be exactly the primary key columns: "+strings.Join(pk, ", ")))
	}
	parts := make([]string, 0, len(pk))
	for _, name := range pk {
		v, ok := key[name]
		if !ok || v == nil {
			return "", refuse(CodeDBKeyInvalid, i18n.T("khoá của dòng thiếu cột "+name, "the row key is missing column "+name))
		}
		c, _ := columnByName(cols, name)
		lit, err := sqlValue(c, v)
		if err != nil {
			return "", err
		}
		parts = append(parts, sqlIdent(name)+" = "+lit)
	}
	return " WHERE " + strings.Join(parts, " AND "), nil
}

// assignments dựng danh sách cột và giá trị từ values (sắp theo thứ tự cột của bảng).
func assignments(cols []DBColumn, table string, values map[string]*string) (names, lits []string, err error) {
	for name := range values {
		if _, ok := columnByName(cols, name); !ok {
			return nil, nil, refuse(CodeDBColumnUnknown, i18n.T("bảng "+table+" không có cột "+strconv.Quote(name), "table "+table+" has no column "+strconv.Quote(name)))
		}
	}
	for _, c := range cols {
		v, ok := values[c.Name]
		if !ok {
			continue
		}
		if v != nil && len(*v) > DBDataMaxCell {
			return nil, nil, refuse(CodeDBKeyInvalid, i18n.T("giá trị của cột "+c.Name+" lớn hơn 256 KiB; dùng nhập file .sql cho dữ liệu lớn", "the value of column "+c.Name+" is larger than 256 KiB; use the .sql import for large data"))
		}
		lit, err := sqlValue(c, v)
		if err != nil {
			return nil, nil, err
		}
		names, lits = append(names, sqlIdent(c.Name)), append(lits, lit)
	}
	return names, lits, nil
}

// Change thêm, sửa hoặc xoá đúng một dòng; trả số dòng bị đổi.
func (d *DBData) Change(ctx context.Context, db, table string, ch DBRowChange) (int64, error) {
	cols, err := d.Columns(ctx, db, table)
	if err != nil {
		return 0, err
	}
	var stmt string
	switch ch.Op {
	case "insert":
		names, lits, err := assignments(cols, table, ch.Values)
		if err != nil {
			return 0, err
		}
		if len(names) == 0 {
			stmt = "INSERT INTO " + sqlIdent(table) + " () VALUES ()"
		} else {
			stmt = "INSERT INTO " + sqlIdent(table) + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(lits, ", ") + ")"
		}
	case "update":
		names, lits, err := assignments(cols, table, ch.Values)
		if err != nil {
			return 0, err
		}
		if len(names) == 0 {
			return 0, refuse(CodeDBOpInvalid, i18n.T("chưa có cột nào để sửa", "no column to change"))
		}
		where, err := keyWhere(cols, table, ch.Key)
		if err != nil {
			return 0, err
		}
		set := make([]string, len(names))
		for i := range names {
			set[i] = names[i] + " = " + lits[i]
		}
		stmt = "UPDATE " + sqlIdent(table) + " SET " + strings.Join(set, ", ") + where + " LIMIT 1"
	case "delete":
		where, err := keyWhere(cols, table, ch.Key)
		if err != nil {
			return 0, err
		}
		stmt = "DELETE FROM " + sqlIdent(table) + where + " LIMIT 1"
	default:
		return 0, refuse(CodeDBOpInvalid, i18n.T(`"op" phải là insert, update hoặc delete`, `"op" must be insert, update or delete`))
	}
	out, sqlErr, err := d.restricted(ctx, db, stmt, 1)
	if err != nil {
		return 0, err
	}
	if sqlErr != "" {
		return 0, refuse(CodeDBSQLInvalid, i18n.T(sqlErr, sqlErr))
	}
	sets, err := parseResultsets(out)
	if err != nil {
		return 0, err
	}
	_, _, affected := splitResult(sets)
	return affected, nil
}

// Maintain chạy một công cụ bảng: check, repair, optimize, analyze. Trả các
// dòng kết quả của MariaDB (Table, Op, Msg_type, Msg_text).
func (d *DBData) Maintain(ctx context.Context, db, table, op string) (DBSQLResult, error) {
	verb, ok := map[string]string{"check": "CHECK", "repair": "REPAIR", "optimize": "OPTIMIZE", "analyze": "ANALYZE"}[op]
	if !ok {
		return DBSQLResult{}, refuse(CodeDBOpInvalid, i18n.T(`"op" phải là check, repair, optimize hoặc analyze`, `"op" must be check, repair, optimize or analyze`))
	}
	if _, err := d.Columns(ctx, db, table); err != nil {
		return DBSQLResult{}, err
	}
	return d.run(ctx, db, verb+" TABLE "+sqlIdent(table))
}

// dropDBRe: câu xoá cả DB. Không phải ranh giới bảo mật (tài khoản của site cũng
// xoá được DB của chính nó), chỉ để một câu gõ nhầm không làm panel còn dòng DB
// mà DB thật đã mất: xoá DB đi qua thùng rác của panel, có bản xuất .sql.
var (
	sqlCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/|--[ \t][^\n]*|#[^\n]*`)
	dropDBRe     = regexp.MustCompile(`(?i)^\s*drop\s+(database|schema)\b`)
)

// SQL chạy đúng một câu SQL của người dùng trên DB.
func (d *DBData) SQL(ctx context.Context, db, sql string) (DBSQLResult, error) {
	stmt := strings.TrimSpace(sql)
	stmt = strings.TrimSpace(strings.TrimSuffix(stmt, ";"))
	if stmt == "" {
		return DBSQLResult{}, refuse(CodeDBSQLInvalid, i18n.T("chưa có câu SQL", "there is no SQL statement"))
	}
	if len(stmt) > DBDataMaxSQL {
		return DBSQLResult{}, refuse(CodeDBSQLInvalid, i18n.T("câu SQL dài hơn 64 KiB; dữ liệu lớn thì nhập bằng file .sql", "the SQL statement is longer than 64 KiB; import large data from a .sql file"))
	}
	if !utf8.ValidString(stmt) || strings.ContainsRune(stmt, 0) {
		return DBSQLResult{}, refuse(CodeDBSQLInvalid, i18n.T("câu SQL có ký tự không hợp lệ", "the SQL statement contains invalid characters"))
	}
	if dropDBRe.MatchString(sqlCommentRe.ReplaceAllString(stmt, " ")) {
		return DBSQLResult{}, refuse(CodeDBSQLBlocked, i18n.T(
			"panel không chạy DROP DATABASE ở đây: xoá DB bằng nút Xoá ở màn Cơ sở dữ liệu, DB sẽ vào thùng rác kèm bản xuất .sql và khôi phục được",
			"the panel does not run DROP DATABASE here: delete the database with the Delete button on the Databases screen; it goes to the trash with a .sql export and can be restored"))
	}
	return d.run(ctx, db, stmt)
}

func (d *DBData) run(ctx context.Context, db, stmt string) (DBSQLResult, error) {
	start := time.Now()
	out, sqlErr, err := d.restricted(ctx, db, stmt, DBDataMaxRows)
	if err != nil {
		return DBSQLResult{}, err
	}
	res := DBSQLResult{Columns: []string{}, Rows: [][]*string{}, Millis: time.Since(start).Milliseconds(), Error: sqlErr}
	if sqlErr != "" {
		return res, nil
	}
	sets, err := parseResultsets(out)
	if err != nil {
		return DBSQLResult{}, err
	}
	cols, rows, affected := splitResult(sets)
	if len(rows) > DBDataMaxRows {
		res.Truncated, rows = true, rows[:DBDataMaxRows]
	}
	if cols != nil {
		res.Columns = cols
	}
	if rows != nil {
		res.Rows = rows
	}
	res.Affected = affected
	return res, nil
}
