package store

// Tiêu chí thu nhỏ của M13: nâng một tpan.db do v0.1.1 tạo (migration <= 900)
// lên bản có migration 1300 mà không mất gì và không ai bị đăng xuất.

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/doxuta/tpan/internal/account"
)

// oldMigrations là các migration của v0.1.1: mọi file có số <= 900.
func oldMigrations(t *testing.T) fs.FS {
	t.Helper()
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	out := fstest.MapFS{}
	for _, e := range entries {
		num, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if err != nil {
			t.Fatalf("tên migration %q", e.Name())
		}
		if v > 900 {
			continue
		}
		data, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out["migrations/"+e.Name()] = &fstest.MapFile{Data: data}
	}
	if len(out) == 0 {
		t.Fatal("không có migration cũ nào")
	}
	return out
}

func TestNangCapTuV011(t *testing.T) {
	smallArgon(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tpan.db")
	const oldPass = "mat-khau-cu-cua-v0.1.1"
	oldHash, err := HashPassword(oldPass)
	if err != nil {
		t.Fatal(err)
	}

	// Dựng DB đúng như v0.1.1 để lại: chỉ migration <= 900, hash ở settings.
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db, oldMigrations(t)); err != nil {
		t.Fatalf("migrate bản cũ: %v", err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)", []any{AdminPasswordKey, oldHash, fmtTime(t0)}},
		{"INSERT INTO settings (key, value, updated_at) VALUES ('web', 'nginx', ?)", []any{fmtTime(t0)}},
		{"INSERT INTO sessions (token_sha256, id, ip, created_at, last_seen_at) VALUES ('tok', 'sid', '192.0.2.1', ?, ?)", []any{fmtTime(t0), fmtTime(t0)}},
		{"INSERT INTO sites (domain, slug, status, php_version, ssl, created_at) VALUES ('old.example.com', 'old-abc123', 'active', '8.3', 1, ?)", []any{fmtTime(t0)}},
		{"INSERT INTO jobs (id, kind, target, title_vi, title_en, status, started_at) VALUES ('job1', 'site.add', 'old.example.com', 'Thêm', 'Add', 'succeeded', ?)", []any{fmtTime(t0)}},
		{"INSERT INTO backups (id, domain, kind, reason, path, size, sha256, created_at) VALUES ('bk1', 'old.example.com', 'full', 'manual', '/var/lib/tpan/backups/old.example.com/a.tar.gz', 10, 'abc', ?)", []any{fmtTime(t0)}},
		{"INSERT INTO audit (time, ip, action, target, result) VALUES (?, 'cli', 'site.add', 'old.example.com', 'ok')", []any{fmtTime(t0)}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("dựng dữ liệu cũ (%s): %v", q.sql, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Mở bằng bản mới: áp migration 1300 trên dữ liệu có sẵn.
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open sau khi nâng: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Mật khẩu cũ vẫn đăng nhập được, qua đường cũ lẫn đường theo tên.
	h, ok, err := s.AdminPasswordHash(ctx)
	if err != nil || !ok || h != oldHash {
		t.Fatalf("AdminPasswordHash = %q, %v, %v; muốn hash cũ", h, ok, err)
	}
	if ok, err := VerifyPassword(h, oldPass); err != nil || !ok {
		t.Errorf("mật khẩu cũ không còn đăng nhập được: %v, %v", ok, err)
	}
	if h2, ok, _ := s.PasswordHashOf(ctx, "admin"); !ok || h2 != oldHash {
		t.Errorf("PasswordHashOf(admin) = %q, %v", h2, ok)
	}
	if _, ok, _ := s.GetSetting(ctx, AdminPasswordKey); ok {
		t.Error("khoá admin.password_hash cũ phải bị xoá khỏi settings")
	}
	if v, ok, _ := s.GetSetting(ctx, "web"); !ok || v != "nginx" {
		t.Errorf("cài đặt khác bị mất: %q, %v", v, ok)
	}
	users, _ := s.Users(ctx)
	if len(users) != 1 || users[0].Name != "admin" || users[0].Role != account.RoleAdmin ||
		users[0].Package != account.DefaultPackage || users[0].Status != account.Active {
		t.Errorf("users sau nâng cấp = %+v, muốn đúng một admin", users)
	}

	// Phiên còn sống và thuộc admin.
	ss, err := s.SessionByToken(ctx, "tok")
	if err != nil || ss.UserName != "admin" || ss.ViewAs != "" || ss.ID != "sid" {
		t.Errorf("phiên cũ = %+v, %v; muốn còn và của admin", ss, err)
	}

	// Site, job, backup thuộc admin và không bị khoá.
	st, ok, err := s.Site(ctx, siteDomain(t, "old.example.com"))
	if err != nil || !ok || st.Owner != "admin" || st.Suspended != account.NotSuspended || st.DBSuspended != account.NotSuspended || !st.SSL {
		t.Errorf("site cũ = %+v, %v, %v", st, ok, err)
	}
	if o, ok, _ := s.SiteOwner(ctx, "old.example.com"); !ok || o != "admin" {
		t.Errorf("SiteOwner = %q, %v", o, ok)
	}
	j, err := s.Job(ctx, "job1")
	if err != nil || j.Owner != "admin" || j.Actor != "" {
		t.Errorf("job cũ = %+v, %v; muốn owner admin, actor rỗng", j, err)
	}
	if o, ok, _ := s.BackupOwner(ctx, "bk1"); !ok || o != "admin" {
		t.Errorf("BackupOwner = %q, %v", o, ok)
	}
	if got, _ := s.BackupsOf(ctx, "admin", ""); len(got) != 1 {
		t.Errorf("BackupsOf(admin) = %+v", got)
	}
	var actor string
	if err := s.db.QueryRow("SELECT actor FROM audit WHERE action = 'site.add'").Scan(&actor); err != nil || actor != "" {
		t.Errorf("audit cũ: actor %q, %v", actor, err)
	}

	// Gói mặc định có, không giới hạn; admin được đếm đúng.
	def, ok, err := s.Package(ctx, account.DefaultPackage)
	if err != nil || !ok || def.MaxSites != 0 || def.MaxDBs != 0 || def.DiskMB != 0 || def.MaxBackups != 0 {
		t.Errorf("gói default = %+v, %v, %v", def, ok, err)
	}
	if u, err := s.Usage(ctx, "admin"); err != nil || u.Sites != 1 || u.DBs != 0 {
		t.Errorf("Usage(admin) = %+v, %v", u, err)
	}

	// Đổi mật khẩu sau nâng cấp vẫn chạy trên dòng admin chuyển sang.
	if err := s.ChangeUserPassword(ctx, "admin", "$moi", "tok", t0); err != nil {
		t.Errorf("ChangeUserPassword(admin) sau nâng cấp: %v", err)
	}
}

func TestMayMoiChuaCoUser(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if users, err := s.Users(ctx); err != nil || users == nil || len(users) != 0 {
		t.Fatalf("máy mới: Users = %#v, %v; muốn rỗng", users, err)
	}
	if _, ok, err := s.AdminPasswordHash(ctx); ok || err != nil {
		t.Fatalf("máy mới: AdminPasswordHash = ok %v, err %v", ok, err)
	}
	if _, ok, err := s.Package(ctx, account.DefaultPackage); !ok || err != nil {
		t.Fatalf("máy mới vẫn phải có gói default: ok %v, err %v", ok, err)
	}

	// tpan setup: SetAdminPasswordHash tạo user admin vai admin, gói default.
	if err := s.SetAdminPasswordHash(ctx, "$h"); err != nil {
		t.Fatal(err)
	}
	u, ok, err := s.User(ctx, "admin")
	if err != nil || !ok || u.Role != account.RoleAdmin || u.Status != account.Active || u.Package != account.DefaultPackage || u.Created.IsZero() {
		t.Fatalf("User(admin) = %+v, %v, %v", u, ok, err)
	}
	// Đổi hash lần hai không đụng vai hay ngày tạo.
	if err := s.SetAdminPasswordHash(ctx, "$h2"); err != nil {
		t.Fatal(err)
	}
	u2, _, _ := s.User(ctx, "admin")
	if u2 != u {
		t.Errorf("SetAdminPasswordHash lần hai đổi user: %+v -> %+v", u, u2)
	}
	if h, _, _ := s.AdminPasswordHash(ctx); h != "$h2" {
		t.Errorf("hash = %q", h)
	}
}

func TestAuditGhiActor(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.AddAudit(ctx, AuditEntry{Time: t0, IP: "192.0.2.1", Action: "site.add", Target: "a.example.com", Result: "ok", Actor: "an"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAudit(ctx, AuditEntry{Time: t0, IP: "cli", Action: "site.add", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	var got []string
	rows, err := s.db.Query("SELECT actor FROM audit ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		got = append(got, a)
	}
	if len(got) != 2 || got[0] != "an" || got[1] != "" {
		t.Errorf("actor đã ghi = %q, muốn [an \"\"]", got)
	}
}
