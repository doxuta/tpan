package store

// Test của M17 trong store: cột mới của sites và packages (migration 1700),
// bảng số liệu usage_samples và usage_hourly.

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
)

// Khẳng định đúng chữ ký của core.UsageStore (store không import core, nên chép
// ra đây): lệch chữ ký thì test không biên dịch.
var _ interface {
	AddUsageSample(ctx context.Context, s cage.Sample) error
	UsageSamples(ctx context.Context, domain string, from, to time.Time) ([]cage.Sample, error)
	UsageSampleDomains(ctx context.Context, from, to time.Time) ([]string, error)
	SaveUsageHour(ctx context.Context, h cage.Hour) error
	UsageHours(ctx context.Context, domain string, from, to time.Time) ([]cage.Hour, error)
	PruneUsage(ctx context.Context, samplesBefore, hoursBefore time.Time) error
	DeleteUsageOf(ctx context.Context, domain string) error
} = (*Store)(nil)

// migrationsUpTo là các migration có số <= max (bản cũ của một máy chưa nâng).
func migrationsUpTo(t *testing.T, max int) fs.FS {
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
		if v > max {
			continue
		}
		data, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out["migrations/"+e.Name()] = &fstest.MapFile{Data: data}
	}
	return out
}

func TestNangCapTuBan1400LenCage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tpan.db")

	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db, migrationsUpTo(t, 1400)); err != nil {
		t.Fatalf("migrate bản 1400: %v", err)
	}
	for _, q := range []string{
		"INSERT INTO sites (domain, slug, status, php_version, ssl, created_at) VALUES ('old.example.com', 'old-abc123', 'active', '8.3', 1, '" + fmtTime(t0) + "')",
		"INSERT INTO packages (name, max_sites, max_dbs, disk_mb, max_backups, created_at) VALUES ('nho', 3, 2, 500, 4, '" + fmtTime(t0) + "')",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("dựng dữ liệu cũ (%s): %v", q, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open sau khi nâng: %v", err)
	}
	defer func() { _ = s.Close() }()

	st, ok, err := s.Site(ctx, siteDomain(t, "old.example.com"))
	if err != nil || !ok {
		t.Fatalf("site cũ = ok %v, err %v", ok, err)
	}
	if st.Caged || !st.SSL || st.PHPVersion != "8.3" {
		t.Errorf("site cũ = %+v; muốn caged=false, các cột khác giữ nguyên", st)
	}

	want := account.Package{Name: "nho", MaxSites: 3, MaxDBs: 2, DiskMB: 500, MaxBackups: 4, Created: t0}
	if got, ok, err := s.Package(ctx, "nho"); err != nil || !ok || got != want {
		t.Errorf("gói cũ = %+v, %v, %v; muốn %+v (cage false, trần 0)", got, ok, err, want)
	}
	def, ok, err := s.Package(ctx, account.DefaultPackage)
	if err != nil || !ok || def.Cage || def.RAMMB != 0 || def.PHPWorkers != 0 {
		t.Errorf("gói default = %+v, %v, %v; muốn không lồng, trần 0", def, ok, err)
	}
	// Ba bảng mới có và rỗng.
	for _, tb := range []string{"usage_samples", "usage_hourly", "probe_runs"} {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + tb).Scan(&n); err != nil || n != 0 {
			t.Errorf("bảng %s: %d dòng, %v; muốn có và rỗng", tb, n, err)
		}
	}
}

func TestSiteCagedLuuVaDocRa(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := siteSample(t, "cage.example.com")
	in.Caged = true
	if err := s.SaveSite(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.Site(ctx, in.Domain)
	if !got.Caged {
		t.Errorf("Caged = false sau khi lưu true: %+v", got)
	}
	// Ra khỏi lồng: ghi đè về false.
	got.Caged = false
	if err := s.SaveSite(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := s.Site(ctx, in.Domain); again.Caged {
		t.Errorf("Caged = true sau khi lưu false")
	}
	// SitesOf cũng đọc cột này.
	got.Caged = true
	if err := s.SaveSite(ctx, got); err != nil {
		t.Fatal(err)
	}
	if list, err := s.Sites(ctx); err != nil || len(list) != 1 || !list[0].Caged {
		t.Errorf("Sites = %+v, %v; muốn một site caged", list, err)
	}
}

func TestGoiLuuDuMuoiMotTruong(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	p := account.Package{
		Name: "long", MaxSites: 3, MaxDBs: 2, DiskMB: 500, MaxBackups: 4,
		Cage: true, CPUPercent: 150, RAMMB: 512, Tasks: 200, IOReadMB: 20, IOWriteMB: 10, PHPWorkers: 8,
		Created: t0,
	}
	if err := s.SavePackage(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Package(ctx, "long"); err != nil || !ok || got != p {
		t.Fatalf("đọc ra %+v, %v, %v; muốn %+v", got, ok, err, p)
	}
	// Trùng tên: ghi đè cả bảy trường M17, giữ ngày tạo.
	p2 := p
	p2.Cage, p2.CPUPercent, p2.RAMMB, p2.Tasks = false, 0, 1024, 0
	p2.IOReadMB, p2.IOWriteMB, p2.PHPWorkers = 0, 7, 1
	p2.Created = t0.Add(time.Hour)
	if err := s.SavePackage(ctx, p2); err != nil {
		t.Fatal(err)
	}
	want := p2
	want.Created = t0
	if got, _, _ := s.Package(ctx, "long"); got != want {
		t.Errorf("sau ghi đè = %+v, muốn %+v", got, want)
	}
	// Packages cũng đọc đủ cột.
	list, err := s.Packages(ctx)
	if err != nil || len(list) != 2 || list[1] != want {
		t.Errorf("Packages = %+v, %v", list, err)
	}
	if err := s.SavePackage(ctx, account.Package{Name: "am", RAMMB: -1}); err == nil {
		t.Error("trần âm phải lỗi (CHECK)")
	}
}

func usageSample(domain string, at time.Time) cage.Sample {
	return cage.Sample{
		Domain: domain, At: at, Running: true,
		MemCurrent: 1, MemPeak: 2, OOMKills: 3, CPUUsec: 4, ThrottledUsec: 5, NrThrottled: 6,
		Tasks: 7, TasksPeak: 8, TasksHits: 9, IORBytes: 10, IOWBytes: 11,
		MemMax: 536870912, CPUQuotaPct: 150, TasksMax: 200,
	}
}

func TestMauSoLieuThemVaDocTheoKhoang(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	// Chèn không theo thứ tự thời gian, một mẫu ở múi giờ khác: kết quả vẫn
	// sắp theo at và đọc ra UTC.
	plus7 := time.FixedZone("x", 7*3600)
	in := []cage.Sample{
		usageSample("a.example.com", base.Add(10*time.Minute)),
		usageSample("a.example.com", base),
		usageSample("a.example.com", base.Add(5*time.Minute).In(plus7)),
		usageSample("a.example.com", base.Add(15*time.Minute)),
		usageSample("b.example.com", base.Add(5*time.Minute)),
	}
	in[1].Running = false
	for _, sm := range in {
		if err := s.AddUsageSample(ctx, sm); err != nil {
			t.Fatal(err)
		}
	}

	// [from, to): lấy from, bỏ to.
	got, err := s.UsageSamples(ctx, "a.example.com", base, base.Add(15*time.Minute))
	if err != nil || len(got) != 3 {
		t.Fatalf("UsageSamples = %d mẫu, %v; muốn 3", len(got), err)
	}
	for i, off := range []time.Duration{0, 5, 10} {
		if !got[i].At.Equal(base.Add(off * time.Minute)) {
			t.Errorf("mẫu %d at = %v, muốn %v", i, got[i].At, base.Add(off*time.Minute))
		}
		if got[i].At.Location() != time.UTC {
			t.Errorf("mẫu %d không ở UTC: %v", i, got[i].At.Location())
		}
	}
	if got[0].Running {
		t.Error("mẫu đầu: Running phải false")
	}
	want := usageSample("a.example.com", base.Add(5*time.Minute))
	if got[1] != want {
		t.Errorf("mẫu giữa = %+v, muốn %+v (đủ mười bảy trường, cả ba trần)", got[1], want)
	}

	// Domain khác không lẫn vào.
	if other, _ := s.UsageSamples(ctx, "b.example.com", base, base.Add(time.Hour)); len(other) != 1 {
		t.Errorf("mẫu của b = %d, muốn 1", len(other))
	}
	if none, err := s.UsageSamples(ctx, "khong.example.com", base, base.Add(time.Hour)); err != nil || none == nil || len(none) != 0 {
		t.Errorf("domain không có mẫu = %#v, %v; muốn slice rỗng không nil", none, err)
	}

	// Domain có mẫu trong khoảng, sắp theo tên.
	doms, err := s.UsageSampleDomains(ctx, base, base.Add(time.Hour))
	if err != nil || len(doms) != 2 || doms[0] != "a.example.com" || doms[1] != "b.example.com" {
		t.Errorf("UsageSampleDomains = %v, %v", doms, err)
	}
	doms, _ = s.UsageSampleDomains(ctx, base.Add(11*time.Minute), base.Add(time.Hour))
	if len(doms) != 1 || doms[0] != "a.example.com" {
		t.Errorf("UsageSampleDomains cuối giờ = %v, muốn chỉ a", doms)
	}
	if doms, err := s.UsageSampleDomains(ctx, base.Add(time.Hour), base.Add(2*time.Hour)); err != nil || doms == nil || len(doms) != 0 {
		t.Errorf("khoảng trống = %#v, %v; muốn slice rỗng không nil", doms, err)
	}

	// Hai mẫu cùng (domain, at): lỗi, không ghi đè lặng lẽ.
	if err := s.AddUsageSample(ctx, usageSample("a.example.com", base)); err == nil {
		t.Error("mẫu trùng (domain, at) phải lỗi")
	}
}

func TestDongGioGhiDeVaDocRa(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	h0 := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	mk := func(domain string, h time.Time, samples int) cage.Hour {
		return cage.Hour{
			Domain: domain, Hour: h, Samples: samples, MemAvg: 100, MemMax: 200,
			CPUUsec: 300, CPUPctMax: 87.5, ThrottledUsec: 400, NrThrottled: 5,
			TasksMax: 6, OOMKills: 7, TasksHits: 8, IORBytes: 9, IOWBytes: 10,
		}
	}
	for _, h := range []cage.Hour{
		mk("a.example.com", h0.Add(2*time.Hour), 12),
		mk("a.example.com", h0, 12),
		mk("a.example.com", h0.Add(time.Hour), 12),
		mk("b.example.com", h0, 12),
	} {
		if err := s.SaveUsageHour(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	// Ghi đè cùng (domain, hour): còn một dòng, mang số mới.
	again := mk("a.example.com", h0, 3)
	again.CPUPctMax = 12.25
	if err := s.SaveUsageHour(ctx, again); err != nil {
		t.Fatal(err)
	}
	got, err := s.UsageHours(ctx, "a.example.com", h0, h0.Add(2*time.Hour))
	if err != nil || len(got) != 2 {
		t.Fatalf("UsageHours = %d dòng, %v; muốn 2 (nửa mở)", len(got), err)
	}
	if got[0] != again {
		t.Errorf("dòng ghi đè = %+v, muốn %+v", got[0], again)
	}
	if !got[1].Hour.Equal(h0.Add(time.Hour)) {
		t.Errorf("dòng sau = %v, muốn %v", got[1].Hour, h0.Add(time.Hour))
	}
	if none, err := s.UsageHours(ctx, "khong.example.com", h0, h0.Add(time.Hour)); err != nil || none == nil || len(none) != 0 {
		t.Errorf("domain không có dòng = %#v, %v; muốn slice rỗng không nil", none, err)
	}
}

func TestPruneVaXoaSoLieuCuaMotDomain(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		at := base.Add(time.Duration(i) * time.Hour)
		for _, d := range []string{"a.example.com", "b.example.com"} {
			if err := s.AddUsageSample(ctx, usageSample(d, at)); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveUsageHour(ctx, cage.Hour{Domain: d, Hour: at, Samples: 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	count := func(table, domain string) int {
		t.Helper()
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE domain = ?", domain).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Mẫu cũ hơn base+2h và dòng giờ cũ hơn base+1h bị xoá; mốc đúng bằng thì ở lại.
	if err := s.PruneUsage(ctx, base.Add(2*time.Hour), base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := count("usage_samples", "a.example.com"); n != 2 {
		t.Errorf("mẫu của a còn %d, muốn 2", n)
	}
	if n := count("usage_hourly", "a.example.com"); n != 3 {
		t.Errorf("dòng giờ của a còn %d, muốn 3", n)
	}
	rest, _ := s.UsageSamples(ctx, "a.example.com", base, base.Add(24*time.Hour))
	if len(rest) != 2 || !rest[0].At.Equal(base.Add(2*time.Hour)) {
		t.Errorf("mẫu còn lại = %+v, muốn bắt đầu ở base+2h", rest)
	}

	// Xoá hẳn một domain: chỉ số của nó.
	if err := s.DeleteUsageOf(ctx, "a.example.com"); err != nil {
		t.Fatal(err)
	}
	if n := count("usage_samples", "a.example.com") + count("usage_hourly", "a.example.com"); n != 0 {
		t.Errorf("a còn %d dòng sau DeleteUsageOf", n)
	}
	if n := count("usage_samples", "b.example.com"); n != 2 {
		t.Errorf("mẫu của b còn %d, muốn 2 (không bị đụng)", n)
	}
	if n := count("usage_hourly", "b.example.com"); n != 3 {
		t.Errorf("dòng giờ của b còn %d, muốn 3 (không bị đụng)", n)
	}
	// Domain không có số liệu: không lỗi.
	if err := s.DeleteUsageOf(ctx, "khong.example.com"); err != nil {
		t.Errorf("DeleteUsageOf domain không có số liệu: %v", err)
	}
}
