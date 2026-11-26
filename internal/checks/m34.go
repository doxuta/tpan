package checks

// M34: DB rời có phần bị xoá ngoài tpan (docs/thay-doi/M34.md).

import (
	"context"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// StandaloneDBExists: DB rời name và user DB cùng tên đều còn trong MariaDB.
// ID "db.exists:<name>", chặng DB; health gắn vào máy (""), như db.user_limit.
//
// Trước M34, một DB rời bị DROP DATABASE bằng tay chỉ để lại trong health một
// dòng cảnh báo về trần kết nối của user DB (hoặc không dòng nào, khi user còn):
// người quản trị không biết DB đã mất cho tới khi ứng dụng báo lỗi. Site thì đã
// có db.access báo chuyện này.
//
// Không có sửa tự động: gỡ dòng của một DB là quyết định của người, và tạo lại
// user DB sinh mật khẩu mới phải có người nhận.
func StandaloneDBExists(name string) Check {
	b := base{id: "db.exists:" + untrustedLine(name, 80), stage: StageDB,
		title: i18n.T("DB rời và user DB của nó còn trong MariaDB", "The standalone database and its DB user exist in MariaDB")}
	if !dbUserRE.MatchString(name) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("tên DB không hợp lệ (chỉ a-z, 0-9, _ và tối đa 64 ký tự)", "the database name is not valid (a-z, 0-9 and _ only, at most 64 characters)"))
		})
	}
	// Tên đã khớp dbUserRE (chữ thường, số, gạch dưới) nên đặt trong nháy đơn là an toàn.
	sql := "SELECT (SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='" + name +
		"'), (SELECT COUNT(*) FROM mysql.global_priv WHERE User='" + name + "' AND Host='localhost')"
	cmd := system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(sql + "\n")}
	recheck := `mariadb -N -B -e "` + sql + `"`
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		f := strings.Fields(res.Stdout)
		if res.ExitCode != 0 || len(f) != 2 {
			return b.unknown(i18n.T("không hỏi được MariaDB (mã "+strconv.Itoa(res.ExitCode)+")", "could not query MariaDB (exit "+strconv.Itoa(res.ExitCode)+")"), ev)
		}
		switch {
		case f[0] == "0":
			return b.bad(Fail,
				i18n.T("DB rời "+name+" có trong tpan nhưng không còn trong MariaDB", "The standalone database "+name+" is known to tpan but is no longer in MariaDB"),
				i18n.T("DB đã bị xoá ngoài tpan (DROP DATABASE bằng tay, hoặc MariaDB mất dữ liệu). Ứng dụng dùng DB này báo lỗi kết nối. Gỡ DB khỏi tpan bằng lệnh đầu dưới đây (trên giao diện: nút Xoá của DB ở màn Cơ sở dữ liệu); nếu DB có bản sao lưu thì sau khi gỡ, tạo lại nó từ bản đó ở màn Backup.",
					"The database was dropped outside tpan (a manual DROP DATABASE, or MariaDB lost its data). Applications using it get connection errors. Remove it from tpan with the first command below (in the web panel: the Delete button of the database on the Databases screen); if it has a backup, recreate it from that backup on the Backup screen afterwards."),
				[]string{line("tpan", "db", "rm", name), line("tpan", "backup", "list", name), recheck}, ev)
		case f[1] == "0":
			return b.bad(Fail,
				i18n.T("User DB "+name+" không còn trong MariaDB (DB và dữ liệu vẫn còn)", "The DB user "+name+" is no longer in MariaDB (the database and its data are still there)"),
				i18n.T("User DB đã bị xoá ngoài tpan nên không ứng dụng nào đăng nhập được vào DB này. Lệnh đầu dưới đây tạo lại user với mật khẩu mới (trên giao diện: Đổi mật khẩu của DB); dữ liệu không bị đụng tới, ứng dụng cần được sửa sang mật khẩu mới.",
					"The DB user was dropped outside tpan, so no application can log in to this database. The first command below recreates the user with a new password (in the web panel: Change password of the database); the data is not touched, and the application must be given the new password."),
				[]string{line("tpan", "db", "passwd", name), recheck}, ev)
		}
		return b.pass(i18n.T("DB "+name+" và user DB cùng tên đều còn", "The database "+name+" and its DB user both exist"), ev)
	})
}
