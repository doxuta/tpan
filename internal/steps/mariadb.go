package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// MariaDBSecure siết MariaDB mới cài (mục 6.1, bước 3): root@localhost đăng
// nhập bằng unix_socket, không có user ẩn danh, không có DB test. Việc "chỉ
// nghe 127.0.0.1" là một File riêng (99-tpan.cnf) do core ghép, không ở đây.
//
// Câu SQL lấy theo script mariadb-secure-installation của MariaDB 10.11 (bản
// trong Ubuntu 24.04):
// https://github.com/MariaDB/server/blob/10.11/scripts/mysql_secure_installation.sh
// Từ 10.4, tài khoản nằm ở mysql.global_priv, cách đăng nhập lưu dạng JSON;
// root mặc định là "mysql_native_password USING 'invalid' OR unix_socket", tức
// unix_socket nằm trong mảng auth_or chứ không ở trường plugin. Vì vậy Check
// tìm chuỗi "unix_socket" ở bất cứ đâu trong JSON (giống script, vốn grep
// unix_socket trong SHOW CREATE USER).
//
// SQL đi qua Stdin, không đặt trên dòng lệnh (`ps` thấy dòng lệnh). Lệnh
// `mariadb` chạy bằng root của máy nên vào được root@localhost qua unix_socket.
type MariaDBSecure struct{}

// mariadbStateSQL trả đúng một dòng, ba số: root@localhost có unix_socket
// không (0/1), số user ẩn danh, có DB test không (0/1).
const mariadbStateSQL = "SELECT " +
	"(SELECT COUNT(*) FROM mysql.global_priv WHERE User='root' AND Host='localhost' AND JSON_SEARCH(Priv, 'one', 'unix_socket') IS NOT NULL), " +
	"(SELECT COUNT(*) FROM mysql.global_priv WHERE User=''), " +
	"(SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='test');\n"

// Các câu sửa, đúng như mariadb-secure-installation 10.11.
const (
	sqlDropAnon   = "DELETE FROM mysql.global_priv WHERE User='';\n"
	sqlDropTest   = "DROP DATABASE IF EXISTS test;\n"
	sqlRootSocket = "UPDATE mysql.global_priv SET priv=json_set(priv, '$.password_last_changed', UNIX_TIMESTAMP(), '$.plugin', 'mysql_native_password', '$.authentication_string', 'invalid', '$.auth_or', json_array(json_object(), json_object('plugin', 'unix_socket'))) WHERE User='root' AND Host='localhost';\n"
	// Sửa thẳng bảng tài khoản thì phải FLUSH để máy chủ đọc lại.
	sqlFlush = "FLUSH PRIVILEGES;\n"
)

// mariadbCmd chạy SQL với -N -B: không in tên cột, cột cách nhau bằng tab, để
// đọc kết quả bằng Go mà không phải bóc bảng kẻ khung.
func mariadbCmd(sql string) system.Cmd {
	return system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(sql)}
}

type mariadbState struct {
	rootSocket bool
	anonUsers  bool
	testDB     bool
}

func (st mariadbState) ok() bool { return st.rootSocket && !st.anonUsers && !st.testDB }

func readMariaDBState(ctx context.Context, sys system.System) (mariadbState, error) {
	res, err := sys.Run(ctx, mariadbCmd(mariadbStateSQL))
	if err != nil {
		return mariadbState{}, withStdout(fmt.Errorf("đọc trạng thái bảo mật của MariaDB: %w", err), res)
	}
	f := strings.Fields(res.Stdout)
	if len(f) != 3 {
		return mariadbState{}, fmt.Errorf("đọc trạng thái bảo mật của MariaDB: kết quả lạ %q (cần đúng ba số)", res.Stdout)
	}
	return mariadbState{rootSocket: f[0] != "0", anonUsers: f[1] != "0", testDB: f[2] != "0"}, nil
}

func (s *MariaDBSecure) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.secure:localhost",
		Title: i18n.T("Siết MariaDB: root qua unix_socket, bỏ user ẩn danh và DB test", "Harden MariaDB: root via unix_socket, drop anonymous users and the test database"),
		Why: i18n.T(
			"User ẩn danh cho bất kỳ ai trên máy vào MariaDB không cần mật khẩu; DB test thì ai cũng ghi được. root dùng unix_socket nghĩa là chỉ root của máy vào được root của DB, không có mật khẩu nào để đoán hay để lộ.",
			"Anonymous users let anyone on the machine into MariaDB without a password, and anyone can write to the test database. With unix_socket only the machine's root user can log in as the database root, so there is no password to guess or leak.",
		),
		Commands: []string{mariadbCmd("").String()}, // câu SQL đi qua stdin
		// DROP DATABASE test xoá dữ liệu nếu ai đó đã dùng DB này.
		Danger: true,
		NoUndo: i18n.T(
			"Không có lý do gì để tạo lại user ẩn danh hay DB test, cả hai đều là lỗ hổng.",
			"There is no reason to recreate anonymous users or the test database; both are security holes.",
		),
	}
}

func (s *MariaDBSecure) Check(ctx context.Context, sys system.System) (bool, error) {
	st, err := readMariaDBState(ctx, sys)
	return st.ok(), err
}

func (s *MariaDBSecure) Apply(ctx context.Context, sys system.System) error {
	st, err := readMariaDBState(ctx, sys)
	if err != nil {
		return err
	}
	// Chỉ chạy câu cần thiết, để log của job nói đúng điều đã đổi.
	var sql strings.Builder
	if st.anonUsers {
		sql.WriteString(sqlDropAnon)
	}
	if st.testDB {
		sql.WriteString(sqlDropTest)
	}
	if !st.rootSocket {
		sql.WriteString(sqlRootSocket)
	}
	if sql.Len() == 0 {
		return nil
	}
	sql.WriteString(sqlFlush)
	if res, err := sys.Run(ctx, mariadbCmd(sql.String())); err != nil {
		return withStdout(fmt.Errorf("siết MariaDB: %w", err), res)
	}
	return nil
}

func (s *MariaDBSecure) Undo(context.Context, system.System) error { return nil }
