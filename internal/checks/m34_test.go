package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

func TestStandaloneDBExists(t *testing.T) {
	run := func(out string, code int) Result {
		f := system.NewFake()
		f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
			if !strings.Contains(string(c.Stdin), "SCHEMA_NAME='alice_shop'") || !strings.Contains(string(c.Stdin), "User='alice_shop' AND Host='localhost'") {
				t.Errorf("câu SQL lạ: %s", c.Stdin)
			}
			return system.Result{Stdout: out, ExitCode: code}
		})
		return StandaloneDBExists("alice_shop").Run(context.Background(), f)
	}

	r := run("1\t1\n", 0)
	if r.Status != Pass || r.ID != "db.exists:alice_shop" || r.Stage != StageDB {
		t.Errorf("DB và user đều còn: %+v", r)
	}

	// DB mất (user còn hay mất): lỗi, cách xử lý đầu tiên là gỡ khỏi tpan.
	for _, out := range []string{"0\t1\n", "0\t0\n"} {
		r = run(out, 0)
		if r.Status != Fail || !strings.Contains(r.Summary.Vi, "không còn trong MariaDB") || len(r.ManualFix) == 0 || r.ManualFix[0] != "tpan db rm alice_shop" {
			t.Errorf("DB mất (%q): %s %q %v", out, r.Status, r.Summary.Vi, r.ManualFix)
		}
		if !strings.Contains(r.Cause.Vi, "Backup") || r.Cause.En == "" {
			t.Errorf("nguyên nhân phải nhắc đường tạo lại từ bản sao lưu: %q", r.Cause.Vi)
		}
	}

	// Chỉ user mất: lỗi, cách xử lý là tạo lại user bằng đổi mật khẩu; nói rõ dữ liệu còn.
	r = run("1\t0\n", 0)
	if r.Status != Fail || !strings.Contains(r.Summary.Vi, "dữ liệu vẫn còn") || len(r.ManualFix) == 0 || r.ManualFix[0] != "tpan db passwd alice_shop" {
		t.Errorf("user mất: %s %q %v", r.Status, r.Summary.Vi, r.ManualFix)
	}

	// MariaDB không trả lời, hoặc trả thứ lạ: chưa kiểm được, không phải lỗi (không mở sự cố "DB đã mất" oan).
	if r = run("", 1); r.Status != Unknown {
		t.Errorf("mariadb lỗi: %s %q", r.Status, r.Summary.Vi)
	}
	if r = run("lạ\n", 0); r.Status != Unknown {
		t.Errorf("output lạ: %s %q", r.Status, r.Summary.Vi)
	}

	// Tên sai dạng không bao giờ vào câu SQL.
	f := system.NewFake()
	if r = StandaloneDBExists("a'; DROP DATABASE x; --").Run(context.Background(), f); r.Status != Unknown || len(f.Ran()) != 0 {
		t.Errorf("tên sai dạng: %s, đã chạy %d lệnh", r.Status, len(f.Ran()))
	}
}
