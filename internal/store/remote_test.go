package store

// Test cho các hàm "kho" của bảng backups (remote.go, migration 1800).

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
)

// *Store phải thoả giao diện M18 của core. Test được import core vì core không
// import store trong mã thường, nên không thành vòng import.
var _ core.RemoteStore = (*Store)(nil)

var tRemote = time.Date(2026, 11, 20, 3, 30, 0, 0, time.UTC)

// addBackup thêm một dòng mới (local=1, remote rỗng) với đường dẫn riêng theo id.
func addBackup(t *testing.T, s *Store, id string) Backup {
	t.Helper()
	b := backupSample("a.example.com", "/var/lib/tpan/backups/a.example.com/"+id+".tar.gz", t0)
	b.ID = id
	got, err := s.AddBackup(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustBackup(t *testing.T, s *Store, id string) Backup {
	t.Helper()
	b, ok, err := s.Backup(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("Backup(%s): ok=%v err=%v", id, ok, err)
	}
	return b
}

func markStored(t *testing.T, s *Store, id, target string) {
	t.Helper()
	ok, err := s.MarkBackupStored(context.Background(), id, "sites/a.example.com/"+id+".tar.gz", target, tRemote)
	if err != nil || !ok {
		t.Fatalf("MarkBackupStored(%s): ok=%v err=%v", id, ok, err)
	}
}

// migrationsBelow là các migration có số nhỏ hơn limit (DB do bản trước M18 tạo).
func migrationsBelow(t *testing.T, limit int) fs.FS {
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
		if v >= limit {
			continue
		}
		data, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out["migrations/"+e.Name()] = &fstest.MapFile{Data: data}
	}
	if len(out) == 0 {
		t.Fatal("không có migration nào")
	}
	return out
}

func TestMigration1800TrenDBCu(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tpan.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db, migrationsBelow(t, 1800)); err != nil {
		t.Fatalf("migrate bản cũ: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO backups (id, domain, kind, reason, path, size, sha256, created_at)
		 VALUES ('bk1', 'old.example.com', 'full', 'manual', '/var/lib/tpan/backups/old.example.com/a.tar.gz', 10, 'abc', ?)`,
		fmtTime(t0)); err != nil {
		t.Fatalf("dựng dòng backup cũ: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open sau khi nâng: %v", err)
	}
	defer func() { _ = s.Close() }()

	old := mustBackup(t, s, "bk1")
	if !old.Local || old.Remote != site.RemoteNone || old.RemoteKey != "" || old.RemoteTarget != "" ||
		!old.RemoteAt.IsZero() || old.RemoteError != "" || old.RemoteTries != 0 {
		t.Errorf("dòng cũ sau nâng cấp = %+v, muốn local=true và mọi cột kho rỗng", old)
	}
	// Dòng thêm bằng AddBackup cũng "trên máy, chưa lên kho".
	nb := addBackup(t, s, "bk2")
	if !nb.Local || nb.Remote != site.RemoteNone {
		t.Errorf("AddBackup mới = local %v, remote %q; muốn true và rỗng", nb.Local, nb.Remote)
	}
	applied, err := s.AppliedMigrations(ctx)
	if err != nil || !slices.Contains(applied, 1800) {
		t.Errorf("AppliedMigrations = %v, %v; muốn có 1800", applied, err)
	}
}

func TestAppliedMigrations(t *testing.T) {
	s, _ := openTemp(t)
	got, err := s.AppliedMigrations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || !slices.IsSorted(got) || !slices.Contains(got, 1800) || !slices.Contains(got, 900) {
		t.Errorf("AppliedMigrations = %v, muốn tăng dần, có 900 và 1800", got)
	}
}

func TestMarkBackupStored(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := addBackup(t, s, "b1")
	// Đặt sẵn lỗi và số lần thử: lên kho phải xoá cả hai.
	if err := s.MarkBackupPushFailed(ctx, b.ID, "tgt0", "lỗi cũ", true); err != nil {
		t.Fatal(err)
	}
	ok, err := s.MarkBackupStored(ctx, b.ID, "sites/a.example.com/b1.tar.gz", "tgt1", tRemote.Add(123))
	if err != nil || !ok {
		t.Fatalf("MarkBackupStored: ok=%v err=%v", ok, err)
	}
	got := mustBackup(t, s, b.ID)
	if got.Remote != site.RemoteStored || got.RemoteKey != "sites/a.example.com/b1.tar.gz" || got.RemoteTarget != "tgt1" ||
		!got.RemoteAt.Equal(tRemote.Add(123)) || got.RemoteError != "" || got.RemoteTries != 0 || !got.Local {
		t.Errorf("sau MarkBackupStored = %+v", got)
	}
}

func TestMarkBackupStoredDongDaMat(t *testing.T) {
	s, _ := openTemp(t)
	ok, err := s.MarkBackupStored(context.Background(), "khong-co", "k", "t", tRemote)
	if err != nil || ok {
		t.Errorf("MarkBackupStored dòng không có: ok=%v err=%v; muốn false, nil", ok, err)
	}
}

func TestMarkBackupPushFailed(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := addBackup(t, s, "b1")

	// countTry=true: remote='failed', đích, lỗi, tries 1; lần hai tries 2.
	for want := 1; want <= 2; want++ {
		if err := s.MarkBackupPushFailed(ctx, b.ID, "tgt1", "lỗi "+strconv.Itoa(want), true); err != nil {
			t.Fatal(err)
		}
		got := mustBackup(t, s, b.ID)
		if got.Remote != site.RemoteFailed || got.RemoteTarget != "tgt1" || got.RemoteError != "lỗi "+strconv.Itoa(want) || got.RemoteTries != want {
			t.Errorf("lần %d: %+v", want, got)
		}
		if !got.Local {
			t.Errorf("lần %d: local phải giữ nguyên true", want)
		}
	}
	// countTry=false (lỗi của đích): ghi lỗi nhưng không tăng số lần thử.
	if err := s.MarkBackupPushFailed(ctx, b.ID, "tgt1", "kho từ chối", false); err != nil {
		t.Fatal(err)
	}
	got := mustBackup(t, s, b.ID)
	if got.Remote != site.RemoteFailed || got.RemoteError != "kho từ chối" || got.RemoteTries != 2 {
		t.Errorf("countTry=false: %+v, muốn failed, lỗi mới, tries vẫn 2", got)
	}
	// Dòng không có: không lỗi.
	if err := s.MarkBackupPushFailed(ctx, "khong-co", "t", "x", true); err != nil {
		t.Errorf("dòng không có: %v", err)
	}
}

func TestMarkBackupPushFailedGiuStoredODichKhac(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := addBackup(t, s, "b1")
	markStored(t, s, b.ID, "tgtCu")
	before := mustBackup(t, s, b.ID)

	if err := s.MarkBackupPushFailed(ctx, b.ID, "tgtMoi", "đích mới từ chối", true); err != nil {
		t.Fatal(err)
	}
	got := mustBackup(t, s, b.ID)
	if got.Remote != site.RemoteStored || got.RemoteKey != before.RemoteKey || got.RemoteTarget != "tgtCu" ||
		!got.RemoteAt.Equal(before.RemoteAt) || got.RemoteTries != 0 {
		t.Errorf("bản stored ở đích cũ bị đổi: %+v", got)
	}
	if got.RemoteError != "đích mới từ chối" {
		t.Errorf("RemoteError = %q, muốn câu lỗi mới", got.RemoteError)
	}
}

func TestResetBackupTries(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := addBackup(t, s, "b1")
	for range 3 {
		if err := s.MarkBackupPushFailed(ctx, b.ID, "t", "x", true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ResetBackupTries(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	got := mustBackup(t, s, b.ID)
	if got.RemoteTries != 0 || got.Remote != site.RemoteFailed || got.RemoteError != "x" {
		t.Errorf("sau ResetBackupTries = %+v, muốn chỉ tries về 0", got)
	}
	if err := s.ResetBackupTries(ctx, "khong-co"); err != nil {
		t.Errorf("dòng không có: %v", err)
	}
}

func TestSetBackupLocal(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := addBackup(t, s, "b1")
	markStored(t, s, b.ID, "t")
	if deleted, err := s.ForgetBackupLocal(ctx, b.ID); err != nil || deleted {
		t.Fatalf("ForgetBackupLocal: deleted=%v err=%v", deleted, err)
	}
	if mustBackup(t, s, b.ID).Local {
		t.Fatal("local phải là false sau ForgetBackupLocal")
	}
	ok, err := s.SetBackupLocal(ctx, b.ID)
	if err != nil || !ok {
		t.Fatalf("SetBackupLocal: ok=%v err=%v", ok, err)
	}
	got := mustBackup(t, s, b.ID)
	if !got.Local || got.Remote != site.RemoteStored {
		t.Errorf("sau tải về = %+v, muốn local và vẫn stored", got)
	}
	if ok, err := s.SetBackupLocal(ctx, "khong-co"); err != nil || ok {
		t.Errorf("dòng không có: ok=%v err=%v; muốn false, nil", ok, err)
	}
}

func TestSetBackupRemoteTarget(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := addBackup(t, s, "b1")
	markStored(t, s, b.ID, "tgtCu")
	if err := s.SetBackupRemoteTarget(ctx, b.ID, "tgtMoi"); err != nil {
		t.Fatal(err)
	}
	got := mustBackup(t, s, b.ID)
	if got.RemoteTarget != "tgtMoi" || got.Remote != site.RemoteStored || got.RemoteKey == "" {
		t.Errorf("sau SetBackupRemoteTarget = %+v, muốn chỉ đổi đích", got)
	}
}

func TestForgetBackupRemote(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	// local=1: xoá trạng thái kho, giữ dòng.
	a := addBackup(t, s, "a1")
	markStored(t, s, a.ID, "t")
	if err := s.MarkBackupPushFailed(ctx, a.ID, "t2", "lỗi", true); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.ForgetBackupRemote(ctx, a.ID)
	if err != nil || deleted {
		t.Fatalf("local=1: deleted=%v err=%v; muốn false, nil", deleted, err)
	}
	got := mustBackup(t, s, a.ID)
	if !got.Local || got.Remote != site.RemoteNone || got.RemoteKey != "" || got.RemoteTarget != "" ||
		!got.RemoteAt.IsZero() || got.RemoteError != "" || got.RemoteTries != 0 {
		t.Errorf("local=1 sau ForgetBackupRemote = %+v", got)
	}

	// local=0: xoá dòng.
	c := addBackup(t, s, "c1")
	markStored(t, s, c.ID, "t")
	if _, err := s.ForgetBackupLocal(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.ForgetBackupRemote(ctx, c.ID); err != nil || !deleted {
		t.Fatalf("local=0: deleted=%v err=%v; muốn true, nil", deleted, err)
	}
	if _, ok, _ := s.Backup(ctx, c.ID); ok {
		t.Error("dòng local=0 phải bị xoá")
	}

	// Dòng không có: không lỗi, không xoá gì.
	if deleted, err := s.ForgetBackupRemote(ctx, "khong-co"); err != nil || deleted {
		t.Errorf("dòng không có: deleted=%v err=%v", deleted, err)
	}
}

// Hai việc dọn (trên máy, trên kho) theo thứ tự nào cũng kết thúc bằng xoá
// dòng: không bao giờ để lại local=0, remote rỗng.
func TestDonHaiPhiaTheoMoiThuTu(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		first func(*Store, string) (bool, error)
		then  func(*Store, string) (bool, error)
	}{
		{"máy rồi kho",
			func(s *Store, id string) (bool, error) { return s.ForgetBackupLocal(ctx, id) },
			func(s *Store, id string) (bool, error) { return s.ForgetBackupRemote(ctx, id) }},
		{"kho rồi máy",
			func(s *Store, id string) (bool, error) { return s.ForgetBackupRemote(ctx, id) },
			func(s *Store, id string) (bool, error) { return s.ForgetBackupLocal(ctx, id) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openTemp(t)
			b := addBackup(t, s, "b1")
			markStored(t, s, b.ID, "t")
			d1, err := tc.first(s, b.ID)
			if err != nil || d1 {
				t.Fatalf("lần một: deleted=%v err=%v; muốn false (dòng còn một phía)", d1, err)
			}
			mid := mustBackup(t, s, b.ID)
			// Bất biến của mục 3.15: local=0 thì phải đang stored.
			if !mid.Local && mid.Remote != site.RemoteStored {
				t.Fatalf("giữa chừng: %+v là trạng thái cấm (local=0 mà không stored)", mid)
			}
			d2, err := tc.then(s, b.ID)
			if err != nil || !d2 {
				t.Fatalf("lần hai: deleted=%v err=%v; muốn true", d2, err)
			}
			if _, ok, _ := s.Backup(ctx, b.ID); ok {
				t.Error("dòng phải bị xoá sau khi dọn cả hai phía")
			}
		})
	}
}

// Dòng local=1 chưa từng lên kho: ForgetBackupLocal xoá dòng (như M9), còn
// ForgetBackupRemote chỉ làm sạch cột kho (không có gì để mất).
func TestForgetKhiChuaLenKho(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := addBackup(t, s, "b1")
	if deleted, err := s.ForgetBackupRemote(ctx, b.ID); err != nil || deleted {
		t.Fatalf("ForgetBackupRemote chưa lên kho: deleted=%v err=%v", deleted, err)
	}
	if deleted, err := s.ForgetBackupLocal(ctx, b.ID); err != nil || !deleted {
		t.Fatalf("ForgetBackupLocal chưa lên kho: deleted=%v err=%v; muốn xoá dòng", deleted, err)
	}
}
