package steps

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

func TestWPAdminPassword(t *testing.T) {
	ctx := context.Background()
	m := wpNewSim(t)
	var got system.Cmd
	admins := "chu_shop\nphu\n"
	m.Handle([]string{"wp", "user"}, func(c system.Cmd) system.Result {
		switch c.Args[1] {
		case "list":
			return system.Result{Stdout: admins}
		case "update":
			got = c
			return system.Result{}
		}
		return system.Result{ExitCode: 1}
	})
	s := &WPAdminPassword{Layout: m.l, Password: system.NewSecret("MatKhauMoi123")}
	if done, _ := s.Check(ctx, m); done {
		t.Fatal("chưa chạy mà Check báo xong")
	}
	if err := s.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	if s.Login != "chu_shop" || got.Args[2] != "chu_shop" {
		t.Errorf("phải đặt lại tài khoản quản trị đầu tiên: login %q, lệnh %s", s.Login, got.String())
	}
	if string(got.Stdin) != "MatKhauMoi123\n" || strings.Contains(got.String(), "MatKhauMoi123") {
		t.Errorf("mật khẩu phải đi qua stdin, không nằm trong dòng lệnh: stdin %q, lệnh %s", got.Stdin, got.String())
	}
	for _, a := range []string{"--prompt=user_pass", "--quiet", "--skip-email"} {
		if !slices.Contains(got.Args, a) {
			t.Errorf("user update thiếu %s: %s", a, got.String())
		}
	}
	if got.AsUser != m.l.User {
		t.Errorf("wp phải chạy dưới user của site, được %q", got.AsUser)
	}
	if done, _ := s.Check(ctx, m); !done {
		t.Error("chạy xong mà Check chưa báo xong")
	}

	// Không có tài khoản quản trị, hoặc tên bắt đầu bằng '-': không gọi update.
	for _, bad := range []string{"", "--allow-root\n"} {
		admins, got = bad, system.Cmd{}
		s := &WPAdminPassword{Layout: m.l, Password: system.NewSecret("MatKhauMoi123")}
		if err := s.Apply(ctx, m); err == nil || got.Name != "" {
			t.Errorf("danh sách %q: muốn lỗi và không gọi update, được err=%v lệnh=%s", bad, err, got.String())
		}
	}
}
