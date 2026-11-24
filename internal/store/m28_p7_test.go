package store

// P7 của rà soát v0.2.0: CreateJob chỉ suy chủ job từ Target theo loại job.
// DB rời tên "<user>_<tên>" nên user khach tạo DB "a" có tên đầy đủ khach_a;
// job admin chạy trên user khach_a (Target cũng là "khach_a") không được ghi
// chủ là khach.

import (
	"context"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
)

func TestCreateJobChuTheoLoaiJob(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveSiteOf(t, s, "a.example.com", "khach")
	// validate.ParseDomain nhận "php8.3-fpm": khách đặt được site trùng tên dịch vụ.
	saveSiteOf(t, s, "php8.3-fpm", "khach")
	if err := s.SaveStandaloneDB(ctx, account.Database{
		Name: "khach_a", Owner: "khach", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci",
		Status: account.DBActive, Created: t0,
	}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		kind, target, owner, actor, want string
	}{
		// Lỗ P7: Target là tên user, trùng tên đầy đủ của DB rời của khach.
		{"user.suspend", "khach_a", "", "", "admin"},
		{"user.unsuspend", "khach_a", "", "", "admin"},
		{"user.trash_all", "khach_a", "", "", "admin"},
		{"user.suspend", "khach_a", "", "admin", "admin"},
		{"cage.sync", "khach_a", "", "", "admin"},
		// Owner đặt sẵn thắng mọi thứ.
		{"user.suspend", "khach_a", "admin", "", "admin"},
		// Loại lạ (thêm sau này) mặc định không suy từ tên, kể cả khi Target
		// là domain thật của khách hay tên DB rời thật.
		{"something.new", "a.example.com", "", "", "admin"},
		{"something.new", "khach_a", "", "", "admin"},
		{"backup.key.new", "a.example.com", "", "", "admin"},
		{"something.new", "khach_a", "", "binh", "binh"}, // không suy: tới người chạy
		// Job của site: chủ của site.
		{"site.add", "a.example.com", "", "", "khach"},
		{"site.suspend", "a.example.com", "", "admin", "khach"},
		{"db.dump", "a.example.com", "", "admin", "khach"},
		{"backup.create", "a.example.com", "", "", "khach"},
		{"backup.restore", "a.example.com", "", "", "khach"},
		{"cage.enable", "a.example.com", "", "", "khach"},
		{"php.use", "a.example.com", "", "", "khach"},
		{"fix.php.restart", "a.example.com", "", "", "khach"},
		{"fix.cage.sync", "a.example.com", "", "", "khach"},
		// Sửa tự động có Target không phải domain: không tra, kể cả khi trùng.
		{"fix.service.start", "php8.3-fpm", "", "admin", "admin"},
		{"fix.something.new", "a.example.com", "", "", "admin"},
		// Job của site không nhìn bảng databases (và ngược lại).
		{"site.suspend", "khach_a", "", "", "admin"},
		{"dbs.suspend", "a.example.com", "", "", "admin"},
		// Job của DB rời: chủ của DB.
		{"dbs.suspend", "khach_a", "", "", "khach"},
		{"dbs.trash", "khach_a", "", "admin", "khach"},
		{"fix.db.user-limit", "khach_a", "", "", "khach"},
	}
	for i, c := range cases {
		j := sampleJob("p7-"+string(rune('a'+i)), t0.Add(time.Duration(i)*time.Second))
		j.Kind, j.Target, j.Owner, j.Actor = c.kind, c.target, c.owner, c.actor
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := s.JobOwner(ctx, j.ID); err != nil || !ok || got != c.want {
			t.Errorf("%s %s (owner %q, actor %q): chủ = %q, %v, %v; muốn %q",
				c.kind, c.target, c.owner, c.actor, got, ok, err, c.want)
		}
	}
}
