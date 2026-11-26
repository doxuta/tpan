package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// remoteFake: MariaDB giả trả host khác localhost của user theo hosts; mọi câu
// SQL khác được ghi lại.
func remoteFake(t *testing.T, on bool, hosts map[string][]string) (*system.FakeSystem, *[]string) {
	t.Helper()
	f := system.NewFake()
	if on {
		f.SetFile(DBRemoteMarker, []byte("on\n"), 0o644, system.Root)
	}
	var sqls []string
	f.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		q := string(c.Stdin)
		if strings.HasPrefix(q, "SELECT User, Host FROM mysql.global_priv WHERE Host<>'localhost' AND User IN") {
			var b strings.Builder
			for u, hs := range hosts {
				if strings.Contains(q, "'"+u+"'") {
					for _, h := range hs {
						b.WriteString(u + "\t" + h + "\n")
					}
				}
			}
			return system.Result{Stdout: b.String()}
		}
		sqls = append(sqls, q)
		return system.Result{}
	})
	return f, &sqls
}

// Ngoài chế độ máy ngoài: dbRun gửi đúng câu cũ, không hỏi thêm gì.
func TestDBWithRemoteTatThiGiuNguyen(t *testing.T) {
	f, sqls := remoteFake(t, false, map[string][]string{"alice_shop": {"203.0.113.5"}})
	if err := dbRun(context.Background(), f, dbLockSQL("alice_shop"), "", "khoá"); err != nil {
		t.Fatal(err)
	}
	if len(f.Ran()) != 1 || (*sqls)[0] != dbLockSQL("alice_shop") {
		t.Fatalf("chạy %d lệnh, SQL %q", len(f.Ran()), *sqls)
	}
}

// Chế độ bật: khoá, đổi mật khẩu, cắt kết nối, xoá user được chép sang mọi
// account máy ngoài trong cùng lần chạy mariadb; câu khác (CREATE, GRANT) không.
func TestDBWithRemoteChepSangMayNgoai(t *testing.T) {
	f, sqls := remoteFake(t, true, map[string][]string{"alice_shop": {"203.0.113.5", "198.51.100.7"}})
	sql := dbLockSQL("alice_shop") + dbAlterUserSQL("alice_shop", "Mk-moi-12345") + dbKillSQL("alice_shop") +
		dbDropUserSQL("alice_shop") + dbGrantSQL("alice_shop")
	if err := dbRun(context.Background(), f, sql, "Mk-moi-12345", "thử"); err != nil {
		t.Fatal(err)
	}
	got := (*sqls)[0]
	for _, want := range []string{
		"ALTER USER 'alice_shop'@'203.0.113.5' ACCOUNT LOCK;",
		"ALTER USER 'alice_shop'@'198.51.100.7' ACCOUNT LOCK;",
		"ALTER USER 'alice_shop'@'203.0.113.5' IDENTIFIED BY 'Mk-moi-12345';",
		"KILL USER 'alice_shop'@'198.51.100.7';",
		"DROP USER IF EXISTS 'alice_shop'@'203.0.113.5';",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %q trong:\n%s", want, got)
		}
	}
	if strings.Count(got, "GRANT") != 1 {
		t.Errorf("GRANT bị chép sang máy ngoài:\n%s", got)
	}
}

// Host lạ (có dấu nháy) thì từ chối, không chạy gì.
func TestDBWithRemoteHostLa(t *testing.T) {
	f, sqls := remoteFake(t, true, map[string][]string{"alice_shop": {"x';DROP"}})
	err := dbRun(context.Background(), f, dbLockSQL("alice_shop"), "", "khoá")
	if err == nil || !strings.Contains(err.Error(), "host lạ") || len(*sqls) != 0 {
		t.Fatalf("lỗi %v, đã chạy %q", err, *sqls)
	}
}

func TestCheckRemoteIP(t *testing.T) {
	for in, ok := range map[string]bool{
		"203.0.113.5": true, " 198.51.100.7 ": true,
		"127.0.0.1": false, "0.0.0.0": false, "224.0.0.1": false, "255.255.255.255": false,
		"2001:db8::1": false, "10.0.0.0/8": false, "may.example.com": false, "%": false,
	} {
		if _, err := CheckRemoteIP(in); (err == nil) != ok {
			t.Errorf("%q: lỗi %v, muốn nhận=%v", in, err, ok)
		}
	}
}

// DBRemoteHosts: thêm account mới bằng hash chép trong MariaDB (không ra Go),
// xoá account thừa.
func TestDBRemoteHostsThemXoa(t *testing.T) {
	f, sqls := remoteFake(t, true, map[string][]string{"alice_shop": {"198.51.100.7"}})
	s := &DBRemoteHosts{Name: "alice_shop", Hosts: []string{"203.0.113.5"}}
	if err := s.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(*sqls, "")
	for _, want := range []string{
		"SET @a := (SELECT JSON_VALUE(Priv, '$.authentication_string') FROM mysql.global_priv WHERE User='alice_shop' AND Host='localhost');",
		"EXECUTE IMMEDIATE CONCAT('CREATE USER ''alice_shop''@''203.0.113.5'' IDENTIFIED BY PASSWORD ''', @a, ''' WITH MAX_USER_CONNECTIONS 20');",
		"GRANT ALL PRIVILEGES ON `alice\\_shop`.* TO 'alice_shop'@'203.0.113.5';",
		"'ALTER USER ''alice_shop''@''203.0.113.5'' ACCOUNT LOCK'",
		"DROP USER IF EXISTS 'alice_shop'@'198.51.100.7';",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %q trong:\n%s", want, got)
		}
	}
	for _, bad := range [][]string{{"127.0.0.1"}, {"203.0.113.5", "203.0.113.5"}, {"may.example.com"}} {
		if err := (&DBRemoteHosts{Name: "alice_shop", Hosts: bad}).validate(); err == nil {
			t.Errorf("%v: không bị từ chối", bad)
		}
	}
}

// UFWDBFrom chỉ đụng luật mang chú thích tpan-db.
func TestUFWDBFrom(t *testing.T) {
	f := system.NewFake()
	added := "Added user rules (see 'ufw status' for running firewall):\n" +
		"ufw allow 22/tcp\n" +
		"ufw allow from 198.51.100.7 to any port 3306 proto tcp comment 'tpan-db'\n" +
		"ufw allow from 192.0.2.9 to any port 3306 proto tcp\n"
	f.Handle([]string{"ufw", "show", "added"}, func(system.Cmd) system.Result { return system.Result{Stdout: added} })
	cur, err := UFWDBCurrent(context.Background(), f)
	if err != nil || len(cur) != 1 || cur[0] != "198.51.100.7" {
		t.Fatalf("UFWDBCurrent = %v, %v; muốn chỉ luật tpan-db", cur, err)
	}
	s := &UFWDBFrom{IPs: []string{"203.0.113.5"}}
	if err := s.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, c := range f.Ran() {
		if c.Name == "ufw" && c.Args[0] != "show" {
			cmds = append(cmds, c.String())
		}
	}
	want := []string{
		"ufw allow from 203.0.113.5 to any port 3306 proto tcp comment tpan-db",
		"ufw delete allow from 198.51.100.7 to any port 3306 proto tcp",
	}
	if strings.Join(cmds, "|") != strings.Join(want, "|") {
		t.Fatalf("lệnh ufw = %q; muốn %q", cmds, want)
	}
}
