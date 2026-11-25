package core

import (
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

func TestBackupDelete(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	f1 := be.seedFull(t, dom, "20261101-000000", "1", "1", ReasonManual)
	f2 := be.seedFull(t, dom, "20261102-000000", "2", "2", ReasonManual)

	_, err := be.b.PrepareDelete(ctx, "ffffffffffffffff")
	wantRefused(t, err, "backup_not_found")
	_, err = be.b.PrepareDelete(ctx, "../etc")
	wantRefused(t, err, "backup_not_found")

	pl, err := be.b.PrepareDelete(ctx, f1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pl.Preview(ctx); err != nil || !be.exists(f1.Path) {
		t.Fatalf("xem trước đã xoá file hoặc lỗi: %v", err)
	}
	rep, err := pl.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %v %+v", err, rep.Job)
	}
	if be.exists(f1.Path) || !be.exists(f2.Path) {
		t.Errorf("file: bản xoá còn=%v, bản khác còn=%v", be.exists(f1.Path), be.exists(f2.Path))
	}
	if _, ok, _ := be.bs.Backup(ctx, f1.ID); ok {
		t.Error("dòng của bản đã xoá vẫn còn")
	}
	if n, _ := be.b.fullCount(ctx, dom); n != 1 {
		t.Errorf("còn %d bản đầy đủ, muốn 1", n)
	}

	// Dòng trỏ ra ngoài thư mục backup (sửa tay): job hỏng, file ngoài còn nguyên.
	be.m.SetFile("/etc/passwd", []byte("root:x:0:0"), 0o644, system.Root)
	bad, err := be.bs.AddBackup(ctx, Backup{Domain: dom, Kind: BackupFull, Reason: ReasonManual, Path: "/etc/passwd", SHA256: "x", Created: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	pl, err = be.b.PrepareDelete(ctx, bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep, _ := pl.Execute(ctx, nil); rep.Job.Status == engine.JobSucceeded || !be.exists("/etc/passwd") {
		t.Errorf("xoá được file ngoài thư mục backup: %+v", rep.Job)
	}
}
