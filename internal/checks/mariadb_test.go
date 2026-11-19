package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// Output của `mariadb -N -B`: -N bỏ dòng tên cột, -B ra dạng tab. Mỗi hàng là
// "<nhãn>\t<số>" vì câu SQL tự đặt nhãn.
const mariadbOK = "root_socket\t1\nanonymous\t0\ntest_db\t0\n"

func TestMariaDBSecurePass(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"mariadb"}, 0, mariadbOK, "")
	r := MariaDBSecure().Run(bg(), fake)
	assertShape(t, r, "db.secure:mariadb", StageDB, Pass)
	assertEvidenceIsRan(t, r, fake)
	if r.Evidence[0].Command != "mariadb -N -B" {
		t.Errorf("Command = %q", r.Evidence[0].Command)
	}
	// SQL đi qua Stdin, không nằm trên dòng lệnh.
	ran := fake.Ran()[0]
	if !strings.Contains(string(ran.Stdin), "mysql.global_priv") || strings.Contains(strings.Join(ran.Args, " "), "SELECT") {
		t.Errorf("SQL phải ở Stdin: args=%v stdin=%q", ran.Args, ran.Stdin)
	}
}

func TestMariaDBSecureFail(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		wantSum  []string
		wantFix  []string
		notInFix []string
	}{
		{"root không unix_socket", "root_socket\t0\nanonymous\t0\ntest_db\t0\n",
			[]string{"root không dùng unix_socket"}, []string{"IDENTIFIED VIA unix_socket"}, []string{"DROP DATABASE", "DELETE FROM"}},
		{"user ẩn danh", "root_socket\t1\nanonymous\t2\ntest_db\t0\n",
			[]string{"user ẩn danh"}, []string{"DELETE FROM mysql.global_priv WHERE User=''"}, []string{"DROP DATABASE", "ALTER USER"}},
		{"DB test", "root_socket\t1\nanonymous\t0\ntest_db\t1\n",
			[]string{"DB test"}, []string{"DROP DATABASE IF EXISTS test"}, []string{"ALTER USER", "DELETE FROM"}},
		{"cả ba", "test_db\t1\nanonymous\t1\nroot_socket\t0\n", // thứ tự hàng khác cũng đọc đúng
			[]string{"root không dùng", "user ẩn danh", "DB test"}, []string{"ALTER USER", "DELETE FROM", "DROP DATABASE"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := system.NewFake()
			reply(fake, []string{"mariadb"}, 0, c.out, "")
			r := MariaDBSecure().Run(bg(), fake)
			assertShape(t, r, "db.secure:mariadb", StageDB, Fail)
			assertEvidenceIsRan(t, r, fake)
			for _, w := range c.wantSum {
				if !strings.Contains(r.Summary.Vi, w) {
					t.Errorf("Summary thiếu %q: %q", w, r.Summary.Vi)
				}
			}
			fix := strings.Join(r.ManualFix, "\n")
			for _, w := range c.wantFix {
				if !strings.Contains(fix, w) {
					t.Errorf("ManualFix thiếu %q:\n%s", w, fix)
				}
			}
			for _, w := range c.notInFix {
				if strings.Contains(fix, w) {
					t.Errorf("ManualFix không được có %q:\n%s", w, fix)
				}
			}
			if last := lastFix(r); !strings.HasPrefix(last, "mariadb -N -B -e ") || !strings.Contains(last, "mysql.global_priv") {
				t.Errorf("dòng cuối phải là lệnh kiểm lại: %q", last)
			}
		})
	}
}

func TestMariaDBSecureRootDoiMatKhau(t *testing.T) {
	// Khi root không dùng unix_socket, `mariadb` chạy bằng root của Linux bị từ chối.
	fake := system.NewFake()
	reply(fake, []string{"mariadb"}, 1, "", "ERROR 1698 (28000): Access denied for user 'root'@'localhost'\n")
	r := MariaDBSecure().Run(bg(), fake)
	assertShape(t, r, "db.secure:mariadb", StageDB, Fail)
	if !strings.Contains(strings.Join(r.ManualFix, "\n"), "IDENTIFIED VIA unix_socket") {
		t.Errorf("ManualFix = %q", r.ManualFix)
	}
}

func TestMariaDBSecureUnknown(t *testing.T) {
	t.Run("không chạy được", func(t *testing.T) {
		assertShape(t, MariaDBSecure().Run(cancelledCtx(), system.NewFake()), "db.secure:mariadb", StageDB, Unknown)
	})
	t.Run("dịch vụ chết", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"mariadb"}, 1, "", "ERROR 2002 (HY000): Can't connect to local server through socket '/run/mysqld/mysqld.sock' (2)\n")
		assertShape(t, MariaDBSecure().Run(bg(), fake), "db.secure:mariadb", StageDB, Unknown)
	})
	t.Run("output lạ", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"mariadb"}, 0, "root_socket\t1\n", "")
		r := MariaDBSecure().Run(bg(), fake)
		assertShape(t, r, "db.secure:mariadb", StageDB, Unknown)
		if !strings.Contains(r.Summary.Vi, "anonymous") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
}
