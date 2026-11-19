package checks

import (
	"context"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// mariadbSecureSQL hỏi ba điều trong một lần chạy, mỗi câu trả về một hàng có
// nhãn "<nhãn>\t<số>" để phần đọc không phụ thuộc thứ tự.
//
// Root dùng unix_socket được tìm trong mysql.global_priv chứ KHÔNG trong
// mysql.user.plugin: từ MariaDB 10.4, mysql.user chỉ là một view và có thể chỉ
// hiện plugin đầu tiên; kiểu "mysql_native_password USING 'invalid' OR
// unix_socket" của Debian/Ubuntu giữ unix_socket trong cột JSON Priv. LIKE
// '%unix_socket%' đủ dùng, không phải gọi hàm JSON.
const mariadbSecureSQL = `SELECT 'root_socket', COUNT(*) FROM mysql.global_priv WHERE User='root' AND Host='localhost' AND Priv LIKE '%unix_socket%'; ` +
	`SELECT 'anonymous', COUNT(*) FROM mysql.global_priv WHERE User=''; ` +
	`SELECT 'test_db', COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='test';`

// MariaDBSecure kiểm ba điều của mục 6.1 bước 3: root vào bằng unix_socket (không
// có mật khẩu để đoán), không có user ẩn danh, không có DB `test`. (Điều thứ tư,
// chỉ nghe 127.0.0.1, do PortLocalOnly kiểm.)
//
// Câu SQL đưa qua Stdin, cùng kiểu với mọi lệnh SQL khác của tpan (mật khẩu thì
// không bao giờ lên dòng lệnh vì `ps` thấy được). Evidence.Command vì thế chỉ là
// "mariadb -N -B"; các hàng có nhãn trong Output cho thấy đã hỏi gì và được trả gì.
func MariaDBSecure() Check {
	b := base{id: "db.secure:mariadb", stage: StageDB,
		title: i18n.T("MariaDB an toàn: root dùng unix_socket, không user ẩn danh, không DB test",
			"MariaDB is safe: root uses unix_socket, no anonymous users, no test database")}
	cmd := system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(mariadbSecureSQL + "\n")}
	// Lệnh kiểm lại cho người gõ: cùng câu SQL, qua -e. SQL không có $, ` hay nháy kép
	// nên đặt trong nháy kép an toàn.
	recheck := `mariadb -N -B -e "` + mariadbSecureSQL + `"`

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			// ERROR 1698/1045: MariaDB từ chối root ở ổ cắm Unix. Đúng nghĩa là root
			// không dùng unix_socket (đang đòi mật khẩu), là một Fail chứ không phải "không kiểm được".
			if strings.Contains(ev.Output, "ERROR 1698") || strings.Contains(ev.Output, "ERROR 1045") {
				return b.bad(Fail,
					i18n.T("root không vào được MariaDB bằng unix_socket", "root cannot log in to MariaDB with unix_socket"),
					i18n.T("Tài khoản root của MariaDB đòi mật khẩu thay vì dùng unix_socket. Đăng nhập bằng mật khẩu hiện có rồi đổi sang unix_socket.",
						"The MariaDB root account asks for a password instead of using unix_socket. Log in with the current password, then switch it to unix_socket."),
					[]string{`mariadb -u root -p -e "` + fixRoot + `"`, recheck}, ev)
			}
			return b.unknown(i18n.T("mariadb báo lỗi, không hỏi được (mã "+strconv.Itoa(res.ExitCode)+")", "mariadb failed, could not query (exit "+strconv.Itoa(res.ExitCode)+")"), ev)
		}

		got := map[string]int{}
		for _, ln := range strings.Split(res.Stdout, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(ln), "\t")
			if !ok {
				continue
			}
			if n, err := strconv.Atoi(v); err == nil {
				got[k] = n
			}
		}
		for _, k := range []string{"root_socket", "anonymous", "test_db"} {
			if _, ok := got[k]; !ok {
				return b.unknown(i18n.T("output của mariadb thiếu mục "+k, "mariadb output has no "+k+" row"), ev)
			}
		}

		var vi, en, fix []string
		if got["root_socket"] == 0 {
			vi = append(vi, "root không dùng unix_socket")
			en = append(en, "root does not use unix_socket")
			fix = append(fix, `mariadb -e "`+fixRoot+`"`)
		}
		if got["anonymous"] > 0 {
			vi = append(vi, "còn user ẩn danh")
			en = append(en, "an anonymous user exists")
			fix = append(fix, `mariadb -e "`+fixAnon+`"`)
		}
		if got["test_db"] > 0 {
			vi = append(vi, "còn DB test")
			en = append(en, "the test database exists")
			fix = append(fix, `mariadb -e "`+fixTestDB+`"`)
		}
		if len(vi) == 0 {
			return b.pass(i18n.T("MariaDB an toàn: root dùng unix_socket, không user ẩn danh, không DB test",
				"MariaDB is safe: root uses unix_socket, no anonymous users, no test database"), ev)
		}
		return b.bad(Fail,
			i18n.T("MariaDB chưa an toàn: "+strings.Join(vi, "; "), "MariaDB is not safe: "+strings.Join(en, "; ")),
			i18n.T("Bản MariaDB cài mới có thể còn tài khoản và DB mẫu. Ai cũng đoán hoặc dùng được chúng, nên phải bỏ.",
				"A fresh MariaDB install can ship sample accounts and a sample database. Anyone can guess or use them, so they must go."),
			append(fix, recheck), ev)
	})
}

// Câu SQL sửa tay cho từng điều không đạt (cùng việc với mariadb-secure-installation).
const (
	fixRoot   = `ALTER USER 'root'@'localhost' IDENTIFIED VIA unix_socket;`
	fixAnon   = `DELETE FROM mysql.global_priv WHERE User=''; FLUSH PRIVILEGES;`
	fixTestDB = `DROP DATABASE IF EXISTS test;`
)
