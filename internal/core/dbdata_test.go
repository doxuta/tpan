package core

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// dbDataEnv là máy giả cho DBData: nhớ mọi lệnh mariadb đã chạy; lệnh chạy
// bằng tài khoản tạm (có --defaults-extra-file) trả xml, lệnh root trả rootOut.
type dbDataEnv struct {
	m       *system.FakeSystem
	d       *DBData
	xml     string
	stderr  string
	rootOut string
	// cnf: nội dung file mật khẩu lúc client của tài khoản tạm đang chạy.
	cnf string
}

func newDBDataEnv(t *testing.T) *dbDataEnv {
	t.Helper()
	e := &dbDataEnv{m: system.NewFake()}
	n := byte(0)
	e.d = &DBData{Sys: e.m, Rand: func(k int) []byte {
		n++
		b := make([]byte, k)
		for i := range b {
			b[i] = n
		}
		return b
	}}
	e.m.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		if len(c.Args) > 0 && strings.HasPrefix(c.Args[0], "--defaults-extra-file=") {
			data, _ := e.m.ReadFile(strings.TrimPrefix(c.Args[0], "--defaults-extra-file="))
			e.cnf = string(data)
			if e.stderr != "" {
				return system.Result{ExitCode: 1, Stderr: e.stderr}
			}
			return system.Result{Stdout: e.xml}
		}
		return system.Result{Stdout: e.rootOut}
	})
	return e
}

const xmlAffected = `<?xml version="1.0"?>

<resultset statement="SELECT ROW_COUNT() AS tpq_affected" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <row>
	<field name="tpq_affected">%s</field>
  </row>
</resultset>
`

func xmlRows(rows string, affected string) string {
	return `<?xml version="1.0"?>

<resultset statement="EXECUTE IMMEDIATE @tpq" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
` + rows + `</resultset>
` + strings.Replace(xmlAffected, "%s", affected, 1)
}

// Chữ người dùng gõ không bao giờ đi vào đầu vào của client ở dạng chữ: chỉ có
// dạng hex. Đây là chỗ chặn lệnh shell của client (\! và system) và mọi kiểu
// chen câu. Tài khoản tạm được tạo trước, xoá sau; mật khẩu chỉ nằm trong file.
func TestDBDataChuNguoiDungChiODangHex(t *testing.T) {
	e := newDBDataEnv(t)
	e.xml = xmlRows("  <row>\n\t<field name=\"a\">1</field>\n  </row>\n", "-1")
	evil := "SELECT 1; \\! id > /tmp/x\nsystem rm -rf /; -- '\"`"
	res, err := e.d.SQL(context.Background(), "tp_shop_ab12cd", evil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || *res.Rows[0][0] != "1" || res.Columns[0] != "a" || res.Affected != -1 {
		t.Errorf("kết quả = %+v", res)
	}
	ran := e.m.Ran()
	if len(ran) != 3 {
		t.Fatalf("muốn 3 lệnh (tạo tài khoản, chạy, xoá tài khoản), có %d: %v", len(ran), ran)
	}
	acct := dbAccount("tp_shop_ab12cd")
	setup, run, drop := string(ran[0].Stdin), ran[1], string(ran[2].Stdin)
	if !strings.Contains(setup, "CREATE OR REPLACE USER '"+acct+"'@'localhost'") || !strings.Contains(setup, "GRANT ALL PRIVILEGES ON `tp\\_shop\\_ab12cd`.* TO '"+acct+"'@'localhost'") {
		t.Errorf("câu tạo tài khoản tạm (dấu _ trong tên DB phải được thoát):\n%s", setup)
	}
	if strings.Contains(setup, "*.*") || strings.Contains(strings.ToUpper(setup), "GRANT OPTION") {
		t.Errorf("tài khoản tạm không được có quyền toàn cục:\n%s", setup)
	}
	if drop != "DROP USER IF EXISTS '"+acct+"'@'localhost';\n" {
		t.Errorf("câu xoá tài khoản tạm: %q", drop)
	}
	in := string(run.Stdin)
	for _, bad := range []string{`\!`, "system", "rm -rf", "SELECT 1;", "/tmp/x"} {
		if strings.Contains(in, bad) {
			t.Errorf("đầu vào của client có chữ người dùng gõ (%q):\n%s", bad, in)
		}
	}
	want := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(evil), ";"))
	if !strings.Contains(in, "SET @tpq = CONVERT(0x"+hex.EncodeToString([]byte(want))+" USING utf8mb4);\nEXECUTE IMMEDIATE @tpq;\n") {
		t.Errorf("thiếu câu ở dạng hex:\n%s", in)
	}
	if !strings.Contains(in, "sql_select_limit=501") || !strings.Contains(in, "max_statement_time=30") {
		t.Errorf("thiếu trần số dòng và thời gian:\n%s", in)
	}
	args := strings.Join(run.Args, " ")
	for _, w := range []string{"--defaults-extra-file=/run/tpan/dbq-", "--database=tp_shop_ab12cd", "--xml", "--binary-as-hex", "--local-infile=0"} {
		if !strings.Contains(args, w) {
			t.Errorf("thiếu tham số %s: %s", w, args)
		}
	}
	if strings.Contains(args, "password") || strings.Contains(args, "-p") && strings.Contains(args, "--password") {
		t.Errorf("mật khẩu không được nằm trên dòng lệnh: %s", args)
	}
	if !strings.HasPrefix(e.cnf, "[client]\nuser="+acct+"\npassword=") {
		t.Errorf("file mật khẩu lúc chạy: %q", e.cnf)
	}
	for p := range e.m.Snapshot() {
		if strings.HasPrefix(p, "/run/tpan/dbq-") {
			t.Errorf("file mật khẩu còn lại sau khi chạy: %s", p)
		}
	}
}

func TestDBDataSQLTuChoi(t *testing.T) {
	e := newDBDataEnv(t)
	ctx := context.Background()
	for _, c := range []struct{ sql, code string }{
		{"", CodeDBSQLInvalid},
		{"  ;  ", CodeDBSQLInvalid},
		{"DROP DATABASE tp_shop_ab12cd", CodeDBSQLBlocked},
		{"drop /* x */ schema `tp_shop_ab12cd`;", CodeDBSQLBlocked},
		{"-- chú thích\nDROP   DATABASE x", CodeDBSQLBlocked},
		{strings.Repeat("a", DBDataMaxSQL+1), CodeDBSQLInvalid},
		{"SELECT \x00 1", CodeDBSQLInvalid},
	} {
		_, err := e.d.SQL(ctx, "tp_shop_ab12cd", c.sql)
		if got := nameCode(err); got != c.code {
			t.Errorf("%.30q: mã %q (%v), muốn %q", c.sql, got, err, c.code)
		}
	}
	if n := len(e.m.Ran()); n != 0 {
		t.Errorf("câu bị từ chối mà vẫn chạy %d lệnh", n)
	}
	// DROP TABLE thì được: đó là việc thường ngày trên DB của chính mình.
	e.xml = strings.Replace(xmlAffected, "%s", "0", 1)
	if _, err := e.d.SQL(ctx, "tp_shop_ab12cd", "DROP TABLE cu"); err != nil {
		t.Errorf("DROP TABLE: %v", err)
	}
	// Tên DB lạ không bao giờ vào câu SQL.
	if _, err := e.d.SQL(ctx, "x`; DROP", "SELECT 1"); err == nil {
		t.Error("tên DB lạ phải bị từ chối")
	}
}

// Lỗi của câu SQL là kết quả (Error), không phải lỗi của panel; tên tài khoản
// tạm không lộ ra trong câu lỗi.
func TestDBDataLoiCuaMariaDB(t *testing.T) {
	e := newDBDataEnv(t)
	acct := dbAccount("tp_shop_ab12cd")
	e.stderr = "--------------\nEXECUTE IMMEDIATE @tpq\n--------------\n\nERROR 1142 (42000) at line 3: SELECT command denied to user '" + acct + "'@'localhost' for table `khac`.`wp_users`\n"
	res, err := e.d.SQL(context.Background(), "tp_shop_ab12cd", "SELECT * FROM khac.wp_users")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Error, "MariaDB 1142 (42000): SELECT command denied") || strings.Contains(res.Error, acct) {
		t.Errorf("Error = %q", res.Error)
	}
}

func TestParseResultsets(t *testing.T) {
	out := xmlRows("  <row>\n\t<field name=\"id\">7</field>\n\t<field name=\"c\">a&lt;b&gt;&amp;&quot; \x01 x</field>\n\t<field name=\"n\" xsi:nil=\"true\" />\n\t<field name=\"b\">0x00FF</field>\n  </row>\n", "-1")
	sets, err := parseResultsets([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	cols, rows, affected := splitResult(sets)
	if strings.Join(cols, ",") != "id,c,n,b" || len(rows) != 1 || affected != -1 {
		t.Fatalf("cols=%v rows=%d affected=%d", cols, len(rows), affected)
	}
	r := rows[0]
	if *r[0] != "7" || !strings.HasPrefix(*r[1], `a<b>&" `) || r[2] != nil || *r[3] != "0x00FF" {
		t.Errorf("dòng = %v %q %v %v", *r[0], *r[1], r[2], *r[3])
	}
	// Câu không trả dòng: chỉ có tpq_affected.
	sets, _ = parseResultsets([]byte(strings.Replace(xmlAffected, "%s", "3", 1)))
	if cols, rows, affected := splitResult(sets); cols != nil || rows != nil || affected != 3 {
		t.Errorf("không trả dòng: cols=%v rows=%v affected=%d", cols, rows, affected)
	}
}

func TestDBDataKhoaVaGiaTri(t *testing.T) {
	cols := []DBColumn{{Name: "id", Type: "int(11)", Key: "PRI"}, {Name: "ten", Type: "varchar(50)", Nullable: true}, {Name: "anh", Type: "blob", Binary: true}}
	s := func(v string) *string { return &v }
	where, err := keyWhere(cols, "t", map[string]*string{"id": s("5")})
	if err != nil || where != " WHERE `id` = CONVERT(0x35 USING utf8mb4)" {
		t.Errorf("where = %q, %v", where, err)
	}
	if _, err := keyWhere(cols[1:], "t", map[string]*string{"ten": s("a")}); nameCode(err) != CodeDBNoPrimaryKey {
		t.Errorf("bảng không có khoá chính: %v", err)
	}
	if _, err := keyWhere(cols, "t", map[string]*string{"ten": s("a")}); nameCode(err) != CodeDBKeyInvalid {
		t.Errorf("khoá không phải cột khoá chính: %v", err)
	}
	if _, err := keyWhere(cols, "t", map[string]*string{"id": nil}); nameCode(err) != CodeDBKeyInvalid {
		t.Errorf("khoá NULL: %v", err)
	}
	names, lits, err := assignments(cols, "t", map[string]*string{"anh": s("0x4a6F"), "ten": nil})
	if err != nil || strings.Join(names, ",") != "`ten`,`anh`" || strings.Join(lits, ",") != "NULL,0x4a6F" {
		t.Errorf("assignments = %v %v %v", names, lits, err)
	}
	if _, _, err := assignments(cols, "t", map[string]*string{"anh": s("'; DROP")}); nameCode(err) != CodeDBKeyInvalid {
		t.Errorf("cột nhị phân nhận chữ thường: %v", err)
	}
	if _, _, err := assignments(cols, "t", map[string]*string{"khong_co": s("1")}); nameCode(err) != CodeDBColumnUnknown {
		t.Errorf("cột lạ: %v", err)
	}
	if got := sqlIdent("a`b"); got != "`a``b`" {
		t.Errorf("sqlIdent = %s", got)
	}
	if got := sqlText(""); got != "''" {
		t.Errorf("sqlText rỗng = %s", got)
	}
}
