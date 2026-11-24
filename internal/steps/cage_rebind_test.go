package steps

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// Hai lồng đang chạy (a, b) và một dòng lạ: lệnh liệt kê dịch vụ của lồng
// trả về như systemctl thật (--plain --no-legend: tên unit ở cột đầu).
const rebindListOut = "tpan-cage-a_example_com_7653e4-php.service loaded active running PHP a\n" +
	"tpan-cage-b_example_com_cd59d5-php.service loaded active running PHP b\n" +
	"tpan-cage-Bad-Slug-php.service loaded active running lạ\n"

var rebindList = []string{"systemctl", "list-units", "--type=service", "--state=active", "--no-legend", "--plain", cageIdentityUnits}

func rebindWant() []string {
	var out []string
	for _, s := range []string{"a_example_com_7653e4", "b_example_com_cd59d5"} {
		u := "tpan-cage-" + s + "-php.service"
		out = append(out,
			"systemctl bind --read-only "+u+" /etc/tpan/cage/"+s+"/passwd /etc/passwd",
			"systemctl bind --read-only "+u+" /etc/tpan/cage/"+s+"/group /etc/group")
	}
	return out
}

func replyCmd(f *system.FakeSystem, prefix []string, code int, out string) {
	f.Handle(prefix, func(system.Cmd) system.Result { return system.Result{ExitCode: code, Stdout: out} })
}

// assertRebind: sau lệnh đổi user (after), đúng các lệnh gắn lại của rebindWant
// chạy, theo thứ tự, và không lệnh bind nào cho slug lạ.
func assertRebind(t *testing.T, ran []system.Cmd, after string) {
	t.Helper()
	got := ranStrings(ran)
	i := slices.Index(got, after)
	if i < 0 {
		t.Fatalf("không thấy lệnh %q: %q", after, got)
	}
	var binds []string
	for _, c := range got[i+1:] {
		if strings.HasPrefix(c, "systemctl bind ") {
			binds = append(binds, c)
		}
	}
	if !slices.Equal(binds, rebindWant()) {
		t.Errorf("lệnh gắn lại sau %q = %q\nmuốn %q", after, binds, rebindWant())
	}
}

// useradd, userdel, usermod -aG, gpasswd -d thay /etc/passwd, /etc/group bằng
// rename; kernel gỡ lần gắn passwd riêng trong namespace của mọi lồng đang
// chạy (e2e M17: A thấy 37 dòng passwd, có tp_* của B). Mỗi bước đổi user hay
// nhóm phải gắn lại cho mọi lồng đang chạy, và lỗi gắn lại không làm hỏng bước.
func TestDoiUserThiGanLaiPasswdCuaLong(t *testing.T) {
	ctx := context.Background()
	l := userTestLayout(t)

	t.Run("SiteUser tạo user", func(t *testing.T) {
		m := userMachine(t)
		replyCmd(m.FakeSystem, rebindList, 0, rebindListOut)
		if err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		assertRebind(t, m.Ran(), "useradd --system --uid 65536 --gid 65536 --home-dir "+l.Dir+" --no-create-home --shell "+userShell+" "+l.User)
	})
	t.Run("SiteUser đã có user: không đổi gì nên không gắn lại", func(t *testing.T) {
		m := userMachine(t)
		replyCmd(m.FakeSystem, rebindList, 0, rebindListOut)
		s := &SiteUser{Layout: l, UIDs: &memUIDs{}}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.ResetRan()
		if err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		for _, c := range ranStrings(m.Ran()) {
			if strings.HasPrefix(c, "systemctl bind ") {
				t.Errorf("user có sẵn mà vẫn gắn lại: %q", c)
			}
		}
	})
	t.Run("SiteUserAbsent xoá user", func(t *testing.T) {
		m := userMachine(t)
		if err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		replyCmd(m.FakeSystem, rebindList, 0, rebindListOut)
		if err := (&SiteUserAbsent{Layout: l}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		assertRebind(t, m.Ran(), "userdel "+l.User)
	})
	t.Run("lệnh bind hỏng không làm hỏng bước", func(t *testing.T) {
		m := userMachine(t)
		replyCmd(m.FakeSystem, rebindList, 0, rebindListOut)
		replyCmd(m.FakeSystem, []string{"systemctl", "bind"}, 1, "")
		if err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply hỏng vì gắn lại hỏng: %v", err)
		}
	})
	t.Run("không liệt kê được lồng: bước vẫn xong, không bind", func(t *testing.T) {
		m := userMachine(t)
		replyCmd(m.FakeSystem, rebindList, 1, "")
		if err := (&SiteUser{Layout: l, UIDs: &memUIDs{}}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	})
	t.Run("UserInGroup thêm rồi bỏ nhóm", func(t *testing.T) {
		m := newM12(t)
		m.groups["nginx"] = []string{"nginx"}
		replyCmd(m.FakeSystem, rebindList, 0, rebindListOut)
		s := &UserInGroup{User: "nginx", Group: "www-data"}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		assertRebind(t, m.Ran(), "usermod -aG www-data nginx")
		m.ResetRan()
		if err := s.Undo(ctx, m); err != nil {
			t.Fatal(err)
		}
		assertRebind(t, m.Ran(), "gpasswd -d nginx www-data")
	})
	// Gói apt tạo user, nhóm hệ thống bằng groupadd, useradd (e2e M17: chuyển
	// sang Apache tạo nhóm ssl-cert và làm mọi lồng mất group riêng).
	t.Run("AptInstall", func(t *testing.T) {
		m := newMachine(t)
		replyCmd(m.FakeSystem, rebindList, 0, rebindListOut)
		if err := (&AptInstall{Packages: []string{"apache2"}}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		assertRebind(t, m.Ran(), "apt-get install -y -o DPkg::Lock::Timeout=300 apache2")
	})
	t.Run("AptInstallNoStart", func(t *testing.T) {
		m := newMachine(t)
		m.SetDir("/usr/sbin", 0o755, system.Root)
		replyCmd(m.FakeSystem, rebindList, 0, rebindListOut)
		if err := (&AptInstallNoStart{Packages: []string{"apache2"}}).Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		assertRebind(t, m.Ran(), "apt-get install -y -o DPkg::Lock::Timeout=300 apache2")
	})
}
