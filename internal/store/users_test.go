package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
)

// Khẳng định đúng chữ ký của core.AccountStore và core.DBStore (store không
// import core, nên chép chữ ký ra đây): lệch thì test không biên dịch.
var _ interface {
	User(ctx context.Context, name string) (u account.User, ok bool, err error)
	Users(ctx context.Context) ([]account.User, error)
	CreateUser(ctx context.Context, u account.User, passwordHash string) error
	UpdateUser(ctx context.Context, u account.User) error
	SetUserPassword(ctx context.Context, name, passwordHash string) error
	DeleteSessionsOf(ctx context.Context, name string) error
	DeleteUser(ctx context.Context, name string) error
	RunningJobsOf(ctx context.Context, name string) (int, error)
	Package(ctx context.Context, name string) (p account.Package, ok bool, err error)
	Packages(ctx context.Context) ([]account.Package, error)
	SavePackage(ctx context.Context, p account.Package) error
	DeletePackage(ctx context.Context, name string) error
	PackageUsers(ctx context.Context, name string) (int, error)
	Usage(ctx context.Context, name string) (account.Usage, error)
	SetDiskUsage(ctx context.Context, name string, bytes int64, at time.Time) error
} = (*Store)(nil)

var _ interface {
	StandaloneDB(ctx context.Context, name string) (d account.Database, ok bool, err error)
	StandaloneDBs(ctx context.Context, owner string) ([]account.Database, error)
	SaveStandaloneDB(ctx context.Context, d account.Database) error
	DeleteStandaloneDB(ctx context.Context, name string) error
} = (*Store)(nil)

func addUser(t *testing.T, s *Store, name string, role account.Role) account.User {
	t.Helper()
	u := account.User{Name: name, Role: role, Created: t0}
	if err := s.CreateUser(context.Background(), u, "$hash-"+name); err != nil {
		t.Fatalf("CreateUser(%s): %v", name, err)
	}
	return u
}

func TestUserTaoDocSuaXoa(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	if _, ok, err := s.User(ctx, "an"); ok || err != nil {
		t.Fatalf("User chưa tạo = ok %v, err %v", ok, err)
	}
	if err := s.SavePackage(ctx, account.Package{Name: "nho", MaxSites: 2, Created: t0}); err != nil {
		t.Fatal(err)
	}
	in := account.User{Name: "an", FullName: "An", Email: "an@example.com", Role: account.RoleUser, Package: "nho", Created: t0}
	if err := s.CreateUser(ctx, in, "$h"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.User(ctx, "an")
	if err != nil || !ok {
		t.Fatalf("User = ok %v, err %v", ok, err)
	}
	in.Status = account.Active // Status rỗng lúc tạo là active
	if got != in {
		t.Errorf("đọc ra %+v, muốn %+v", got, in)
	}
	if err := s.CreateUser(ctx, in, "$h"); err == nil {
		t.Error("tạo trùng tên phải lỗi")
	}
	if err := s.CreateUser(ctx, account.User{Name: "x", Role: account.RoleUser, Package: "khong-co"}, "$h"); err == nil {
		t.Error("gói không có phải lỗi (khoá ngoại)")
	}
	if err := s.CreateUser(ctx, account.User{Name: "y", Role: "quan-tri"}, "$h"); err == nil {
		t.Error("vai lạ phải lỗi (CHECK)")
	}
	if err := s.CreateUser(ctx, account.User{Name: "z", Role: account.RoleUser}, ""); err == nil {
		t.Error("thiếu hash phải lỗi")
	}

	// UpdateUser ghi tạm khoá nhưng không đụng hash.
	got.Status, got.SuspendedAt, got.SuspendReason, got.FullName = account.Suspended, t0.Add(time.Hour), "nợ phí", "An B"
	if err := s.UpdateUser(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _, _ := s.User(ctx, "an")
	if again != got {
		t.Errorf("sau UpdateUser đọc ra %+v, muốn %+v", again, got)
	}
	if h, ok, _ := s.PasswordHashOf(ctx, "an"); !ok || h != "$h" {
		t.Errorf("UpdateUser làm đổi hash: %q, %v", h, ok)
	}
	if err := s.UpdateUser(ctx, account.User{Name: "khong-co", Role: account.RoleUser, Package: "default", Status: account.Active}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateUser user lạ: %v, muốn ErrNotFound", err)
	}

	list, err := s.Users(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "an" {
		t.Fatalf("Users = %+v, %v", list, err)
	}

	if err := s.DeleteUser(ctx, "an"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.User(ctx, "an"); ok {
		t.Error("user còn sau DeleteUser")
	}
	if err := s.DeleteUser(ctx, "an"); !errors.Is(err, ErrNotFound) {
		t.Errorf("xoá lần hai: %v, muốn ErrNotFound", err)
	}
}

func TestSetUserPasswordHuyPhienCuaUserVaPhienXemNhu(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	addUser(t, s, "an", account.RoleUser)
	addUser(t, s, "binh", account.RoleUser)
	for _, ss := range []Session{
		{TokenSHA256: "an1", ID: "an1", IP: "192.0.2.1", Created: t0, UserName: "an"},
		{TokenSHA256: "ad-xem-an", ID: "adx", IP: "192.0.2.2", Created: t0, UserName: "admin", ViewAs: "an"},
		{TokenSHA256: "ad", ID: "ad", IP: "192.0.2.3", Created: t0},
		{TokenSHA256: "binh1", ID: "binh1", IP: "192.0.2.4", Created: t0, UserName: "binh"},
	} {
		if err := s.CreateSession(ctx, ss); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetUserPassword(ctx, "an", "$moi"); err != nil {
		t.Fatal(err)
	}
	if h, _, _ := s.PasswordHashOf(ctx, "an"); h != "$moi" {
		t.Errorf("hash = %q", h)
	}
	left, _ := s.Sessions(ctx)
	ids := map[string]bool{}
	for _, ss := range left {
		ids[ss.ID] = true
	}
	if len(left) != 3 || !ids["ad"] || !ids["binh1"] || !ids["adx"] {
		t.Errorf("chỉ phiên của an bị huỷ (phiên admin xem như an vẫn còn), còn lại %+v", left)
	}
	if ss, err := s.SessionByToken(ctx, "ad-xem-an"); err != nil || ss.UserName != "admin" || ss.ViewAs != "" {
		t.Errorf("phiên admin đang xem như an phải còn và thoát xem như: %+v, %v", ss, err)
	}

	// User lạ: lỗi và không huỷ gì.
	if err := s.SetUserPassword(ctx, "khong-co", "$x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("user lạ: %v, muốn ErrNotFound", err)
	}
	if left, _ := s.Sessions(ctx); len(left) != 3 {
		t.Errorf("lỗi mà vẫn huỷ phiên: %+v", left)
	}
}

// TestHuyPhienGiuPhienAdminXemNhu: khoá (DeleteSessionsOf), đổi mật khẩu
// (SetUserPassword), xoá (DeleteUser) user X huỷ phiên của X nhưng không đăng
// xuất admin đang "xem như" X: phiên đó chỉ thoát xem như (e2e M13 trên máy ảo
// thấy admin bị đăng xuất khi user đang được xem bị khoá).
func TestHuyPhienGiuPhienAdminXemNhu(t *testing.T) {
	ops := map[string]func(*Store, context.Context) error{
		"DeleteSessionsOf": func(s *Store, ctx context.Context) error { return s.DeleteSessionsOf(ctx, "an") },
		"SetUserPassword":  func(s *Store, ctx context.Context) error { return s.SetUserPassword(ctx, "an", "$moi") },
		"DeleteUser":       func(s *Store, ctx context.Context) error { return s.DeleteUser(ctx, "an") },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			s, _ := openTemp(t)
			ctx := context.Background()
			addUser(t, s, "an", account.RoleUser)
			for _, ss := range []Session{
				{TokenSHA256: "an1", ID: "an1", IP: "192.0.2.1", Created: t0, UserName: "an"},
				{TokenSHA256: "ad-xem-an", ID: "adx", IP: "192.0.2.2", Created: t0, UserName: "admin", ViewAs: "an"},
			} {
				if err := s.CreateSession(ctx, ss); err != nil {
					t.Fatal(err)
				}
			}
			if err := op(s, ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SessionByToken(ctx, "an1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("phiên của an còn: %v", err)
			}
			ss, err := s.SessionByToken(ctx, "ad-xem-an")
			if err != nil || ss.UserName != "admin" || ss.ViewAs != "" {
				t.Errorf("phiên admin phải còn, view_as rỗng: %+v, %v", ss, err)
			}
		})
	}
}

func TestDeleteUserXoaPhienNhungGiuJob(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	addUser(t, s, "an", account.RoleUser)
	_ = s.CreateSession(ctx, Session{TokenSHA256: "t", ID: "i", IP: "192.0.2.1", Created: t0, UserName: "an"})
	j := sampleJob("j1", t0)
	j.Actor = "an"
	j.Status = engine.JobSucceeded
	if err := s.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, "an"); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.Sessions(ctx); len(left) != 0 {
		t.Errorf("phiên của user đã xoá còn: %+v", left)
	}
	got, err := s.Job(ctx, "j1")
	if err != nil || got.Actor != "an" {
		t.Errorf("job cũ phải giữ tên người chạy: %+v, %v", got, err)
	}
}

func TestRunningJobsOf(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	mk := func(id, target, actor string, st engine.JobStatus) {
		j := sampleJob(id, t0)
		j.Target, j.Actor, j.Status = target, actor, st
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	addUser(t, s, "an", account.RoleUser)
	saveSiteOf(t, s, "an.example.com", "an")
	mk("a", "x.example.com", "an", engine.JobRunning)     // actor = an
	mk("b", "an.example.com", "", engine.JobRunning)      // owner = an (admin chạy hộ qua dòng lệnh)
	mk("c", "an.example.com", "admin", engine.JobRunning) // owner = an, actor = admin
	mk("d", "an.example.com", "an", engine.JobSucceeded)  // xong rồi, không đếm
	mk("e", "khac.example.com", "binh", engine.JobRunning)
	n, err := s.RunningJobsOf(ctx, "an")
	if err != nil || n != 3 {
		t.Fatalf("RunningJobsOf(an) = %d, %v; muốn 3", n, err)
	}
}

func TestGoiVaMacDinh(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	def, ok, err := s.Package(ctx, account.DefaultPackage)
	if err != nil || !ok || def.MaxSites != 0 || def.MaxDBs != 0 || def.DiskMB != 0 || def.MaxBackups != 0 {
		t.Fatalf("gói default = %+v, %v, %v; muốn không giới hạn", def, ok, err)
	}
	p := account.Package{Name: "nho", MaxSites: 3, MaxDBs: 2, DiskMB: 500, MaxBackups: 4, Created: t0}
	if err := s.SavePackage(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Package(ctx, "nho"); got != p {
		t.Errorf("đọc ra %+v, muốn %+v", got, p)
	}
	// Ghi đè đổi giới hạn, giữ ngày tạo.
	p2 := p
	p2.MaxSites, p2.Created = 9, t0.Add(time.Hour)
	if err := s.SavePackage(ctx, p2); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.Package(ctx, "nho")
	if got.MaxSites != 9 || !got.Created.Equal(t0) {
		t.Errorf("sau ghi đè = %+v, muốn MaxSites 9 và Created cũ", got)
	}
	if err := s.SavePackage(ctx, account.Package{MaxSites: -1}); err == nil {
		t.Error("gói không tên phải lỗi")
	}
	if err := s.SavePackage(ctx, account.Package{Name: "am", MaxSites: -1}); err == nil {
		t.Error("giới hạn âm phải lỗi (CHECK)")
	}
	list, _ := s.Packages(ctx)
	if len(list) != 2 || list[0].Name != "default" || list[1].Name != "nho" {
		t.Errorf("Packages = %+v", list)
	}

	// Gói còn user dùng: PackageUsers đếm, xoá bị chặn bởi khoá ngoại.
	if err := s.CreateUser(ctx, account.User{Name: "an", Role: account.RoleUser, Package: "nho", Created: t0}, "$h"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PackageUsers(ctx, "nho"); err != nil || n != 1 {
		t.Errorf("PackageUsers = %d, %v", n, err)
	}
	if err := s.DeletePackage(ctx, "nho"); err == nil {
		t.Error("xoá gói còn user dùng phải lỗi")
	}
	_ = s.DeleteUser(ctx, "an")
	if err := s.DeletePackage(ctx, "nho"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePackage(ctx, "nho"); !errors.Is(err, ErrNotFound) {
		t.Errorf("xoá gói lần hai: %v", err)
	}
}

func saveSiteOf(t *testing.T, s *Store, domain, owner string) {
	t.Helper()
	st := siteSample(t, domain)
	st.Owner = owner
	if err := s.SaveSite(context.Background(), st); err != nil {
		t.Fatal(err)
	}
}

func TestUsageDemMoiTrangThai(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	addUser(t, s, "an", account.RoleUser)
	addUser(t, s, "binh", account.RoleUser)

	u, err := s.Usage(ctx, "an")
	if err != nil || u.Sites != 0 || u.DBs != 0 || u.DiskBytes != -1 || !u.DiskMeasured.IsZero() {
		t.Fatalf("Usage user mới = %+v, %v", u, err)
	}

	saveSiteOf(t, s, "a1.example.com", "an")
	trashed := siteSample(t, "a2.example.com")
	trashed.Owner, trashed.Status, trashed.TrashedAt, trashed.PurgeAt = "an", site.Trashed, t0, t0.Add(time.Hour)
	_ = s.SaveSite(ctx, trashed) // site ở thùng rác vẫn tính (còn giữ DB và file)
	saveSiteOf(t, s, "b1.example.com", "binh")
	for _, d := range []account.Database{
		{Name: "an_blog", Owner: "an", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci", Created: t0},
		{Name: "an_cu", Owner: "an", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci", Status: account.DBTrashed, Created: t0},
		{Name: "binh_x", Owner: "binh", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci", Created: t0},
	} {
		if err := s.SaveStandaloneDB(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetDiskUsage(ctx, "an", 12345, t0); err != nil {
		t.Fatal(err)
	}
	u, err = s.Usage(ctx, "an")
	if err != nil || u.Sites != 2 || u.DBs != 2 || u.DiskBytes != 12345 || !u.DiskMeasured.Equal(t0) {
		t.Fatalf("Usage(an) = %+v, %v; muốn 2 site, 2 DB (cả thùng rác), 12345 byte", u, err)
	}
	if u, _ := s.Usage(ctx, "binh"); u.Sites != 1 || u.DBs != 1 || u.DiskBytes != -1 {
		t.Errorf("Usage(binh) = %+v", u)
	}
	if _, err := s.Usage(ctx, "khong-co"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Usage user lạ: %v", err)
	}
	if err := s.SetDiskUsage(ctx, "khong-co", 1, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetDiskUsage user lạ: %v", err)
	}
}

func TestPhienTheoUser(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	addUser(t, s, "an", account.RoleUser)
	// UserName rỗng lúc tạo là admin.
	for _, ss := range []Session{
		{TokenSHA256: "t-ad", ID: "i-ad", IP: "192.0.2.1", Created: t0},
		{TokenSHA256: "t-an", ID: "i-an", IP: "192.0.2.2", Created: t0.Add(time.Minute), UserName: "an"},
	} {
		if err := s.CreateSession(ctx, ss); err != nil {
			t.Fatal(err)
		}
	}
	if ss, err := s.SessionByToken(ctx, "t-ad"); err != nil || ss.UserName != "admin" || ss.ViewAs != "" {
		t.Fatalf("phiên admin = %+v, %v", ss, err)
	}
	if of, _ := s.SessionsOf(ctx, "an"); len(of) != 1 || of[0].ID != "i-an" {
		t.Errorf("SessionsOf(an) = %+v", of)
	}
	if of, _ := s.SessionsOf(ctx, "admin"); len(of) != 1 || of[0].ID != "i-ad" {
		t.Errorf("SessionsOf(admin) = %+v", of)
	}
	if all, _ := s.Sessions(ctx); len(all) != 2 {
		t.Errorf("Sessions = %+v", all)
	}

	// DeleteSessionOf: không xoá được phiên của người khác, và báo như không có.
	if err := s.DeleteSessionOf(ctx, "an", "i-ad"); !errors.Is(err, ErrNotFound) {
		t.Errorf("xoá phiên của admin bằng tên an: %v, muốn ErrNotFound", err)
	}
	if _, err := s.SessionByToken(ctx, "t-ad"); err != nil {
		t.Errorf("phiên admin lỡ bị xoá: %v", err)
	}
	if err := s.DeleteSessionOf(ctx, "an", "i-an"); err != nil {
		t.Fatal(err)
	}

	// SetViewAs.
	if err := s.SetViewAs(ctx, "t-ad", "an"); err != nil {
		t.Fatal(err)
	}
	if ss, _ := s.SessionByToken(ctx, "t-ad"); ss.ViewAs != "an" || ss.UserName != "admin" {
		t.Errorf("sau SetViewAs = %+v", ss)
	}
	if err := s.SetViewAs(ctx, "t-ad", ""); err != nil {
		t.Fatal(err)
	}
	if ss, _ := s.SessionByToken(ctx, "t-ad"); ss.ViewAs != "" {
		t.Errorf("gỡ xem-như = %+v", ss)
	}
	if err := s.SetViewAs(ctx, "khong-co", "an"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetViewAs phiên lạ: %v", err)
	}
}

func TestChangeUserPassword(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	addUser(t, s, "an", account.RoleUser)
	addUser(t, s, "binh", account.RoleUser)
	for _, ss := range []Session{
		{TokenSHA256: "keep", ID: "k", IP: "192.0.2.1", Created: t0, UserName: "an"},
		{TokenSHA256: "other", ID: "o", IP: "192.0.2.2", Created: t0, UserName: "an"},
		{TokenSHA256: "view", ID: "v", IP: "192.0.2.3", Created: t0, ViewAs: "an"},
		{TokenSHA256: "binh", ID: "b", IP: "192.0.2.4", Created: t0, UserName: "binh"},
	} {
		_ = s.CreateSession(ctx, ss)
	}
	if err := s.ChangeUserPassword(ctx, "an", "$moi", "keep", t0); err != nil {
		t.Fatal(err)
	}
	if h, _, _ := s.PasswordHashOf(ctx, "an"); h != "$moi" {
		t.Errorf("hash = %q", h)
	}
	left, _ := s.Sessions(ctx)
	if len(left) != 2 {
		t.Fatalf("phải còn phiên giữ lại và phiên của binh, được %+v", left)
	}
	for _, ss := range left {
		if ss.ID != "k" && ss.ID != "b" {
			t.Errorf("phiên %s lẽ ra bị huỷ", ss.ID)
		}
	}
	if err := s.ChangeUserPassword(ctx, "khong-co", "$x", "keep", t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("user lạ: %v", err)
	}
	// Chưa có hash thì PasswordHashOf báo ok=false.
	if _, ok, err := s.PasswordHashOf(ctx, "khong-co"); ok || err != nil {
		t.Errorf("PasswordHashOf lạ = ok %v, err %v", ok, err)
	}
}
