package store

import (
	"context"
	"errors"
	"github.com/doxuta/tpan/internal/site"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func backupSample(domain, path string, created time.Time) Backup {
	return Backup{
		Domain: domain, Kind: "full", Reason: "manual", Path: path,
		Size: 1234, SHA256: strings.Repeat("ab", 32), WPVersion: "6.8.1",
		PHPVersion: "8.3", JobID: "job1", Created: created,
	}
}

func TestAddBackupSinhIDVaDocLai(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	at := time.Date(2026, 5, 1, 3, 30, 0, 123, time.UTC)
	got, err := s.AddBackup(ctx, backupSample("a.example.com", "/var/lib/tpan/backups/a.example.com/1.tar.gz", at))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(got.ID) {
		t.Fatalf("ID = %q, muốn 16 hex", got.ID)
	}
	back, ok, err := s.Backup(ctx, got.ID)
	if err != nil || !ok {
		t.Fatalf("Backup: ok=%v err=%v", ok, err)
	}
	if back != got {
		t.Errorf("đọc lại khác dòng đã ghi:\n got  %+v\n want %+v", back, got)
	}
	if !back.Created.Equal(at) {
		t.Errorf("Created = %v, muốn %v", back.Created, at)
	}
	// ID do người gọi đưa thì giữ nguyên.
	b := backupSample("a.example.com", "/x/2.tar.gz", at)
	b.ID = "0123456789abcdef"
	if got, err := s.AddBackup(ctx, b); err != nil || got.ID != b.ID {
		t.Errorf("giữ ID đưa vào: %+v, %v", got, err)
	}
}

func TestAddBackupCreatedTrongLaBayGio(t *testing.T) {
	s, _ := openTemp(t)
	fixed := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }
	got, err := s.AddBackup(context.Background(), backupSample("a.example.com", "/x/1.tar.gz", time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Created.Equal(fixed) {
		t.Errorf("Created = %v, muốn %v", got.Created, fixed)
	}
}

func TestAddBackupTuChoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ok := backupSample("a.example.com", "/x/1.tar.gz", time.Now())
	if _, err := s.AddBackup(ctx, ok); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Backup){
		"thiếu domain": func(b *Backup) { b.Domain = "" },
		"thiếu kind":   func(b *Backup) { b.Kind = "" },
		"thiếu path":   func(b *Backup) { b.Path = "" },
		"trùng path":   func(b *Backup) {}, // path UNIQUE
		"size âm":      func(b *Backup) { b.Size = -1; b.Path = "/x/other.tar.gz" },
	}
	for name, mut := range cases {
		b := ok
		mut(&b)
		_, err := s.AddBackup(ctx, b)
		if err == nil {
			t.Errorf("%s: muốn lỗi", name)
		} else if !strings.Contains(err.Error(), "backup") {
			t.Errorf("%s: lỗi thiếu ngữ cảnh: %v", name, err)
		}
	}
}

func TestBackupKhongCo(t *testing.T) {
	s, _ := openTemp(t)
	_, ok, err := s.Backup(context.Background(), "nope")
	if err != nil || ok {
		t.Errorf("ok=%v err=%v, muốn ok=false và không lỗi", ok, err)
	}
}

func TestBackupsMoiNhatTruocVaLocDomain(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	add := func(domain, path string, d time.Duration) string {
		b, err := s.AddBackup(ctx, backupSample(domain, path, t0.Add(d)))
		if err != nil {
			t.Fatal(err)
		}
		return b.ID
	}
	// Chèn không theo thứ tự thời gian để chắc là ORDER BY, không phải thứ tự chèn.
	mid := add("a.example.com", "/x/mid", time.Hour)
	old := add("a.example.com", "/x/old", 0)
	newest := add("a.example.com", "/x/new", 2*time.Hour)
	other := add("b.example.com", "/y/one", 90*time.Minute)

	ids := func(domain string) []string {
		list, err := s.Backups(ctx, domain)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, b := range list {
			out = append(out, b.ID)
		}
		return out
	}
	eq := func(got, want []string) bool { return strings.Join(got, ",") == strings.Join(want, ",") }
	if got, want := ids("a.example.com"), []string{newest, mid, old}; !eq(got, want) {
		t.Errorf("a.example.com = %v, muốn %v", got, want)
	}
	if got, want := ids(""), []string{newest, other, mid, old}; !eq(got, want) {
		t.Errorf("mọi domain = %v, muốn %v", got, want)
	}
	if got := ids("khong.example.com"); len(got) != 0 {
		t.Errorf("domain lạ = %v, muốn rỗng", got)
	}
	// Rỗng phải là slice không nil để JSON ra [] chứ không phải null.
	if list, _ := s.Backups(ctx, "khong.example.com"); list == nil {
		t.Error("Backups rỗng trả nil, muốn slice rỗng")
	}
}

func TestBackupsCungThoiDiemTheoThuTuChenNguoc(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first, _ := s.AddBackup(ctx, backupSample("a.example.com", "/x/1", at))
	second, _ := s.AddBackup(ctx, backupSample("a.example.com", "/x/2", at))
	list, _ := s.Backups(ctx, "a.example.com")
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Errorf("thứ tự = %+v, muốn bản chèn sau trước", list)
	}
}

func TestDeleteBackup(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b, _ := s.AddBackup(ctx, backupSample("a.example.com", "/x/1", time.Now()))
	if err := s.DeleteBackup(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Backup(ctx, b.ID); ok {
		t.Error("dòng còn sau DeleteBackup")
	}
	if err := s.DeleteBackup(ctx, b.ID); err != nil {
		t.Errorf("xoá dòng không có phải không lỗi: %v", err)
	}
}

// Backup phải sống sót khi site bị xoá hẳn: bảng không có khoá ngoại tới sites.
func TestBackupKhongCoKhoaNgoaiToiSites(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.AddBackup(context.Background(), backupSample("khong-co-site.example.com", "/x/1", time.Now())); err != nil {
		t.Fatalf("backup của domain không có trong sites: %v", err)
	}
}

func TestSnapshotDB(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetSetting(ctx, "web", "nginx"); err != nil {
		t.Fatal(err)
	}
	b, _ := s.AddBackup(ctx, backupSample("a.example.com", "/x/1", time.Now()))

	// Tên có dấu nháy đơn và dấu "?": phải đi vào câu SQL qua tham số, không nối chuỗi.
	dst := filepath.Join(t.TempDir(), "tpan-it's?.db")
	if err := s.SnapshotDB(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dst); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file bản sao: %v, %v; muốn 0600", st, err)
	}
	// Store.Open từ chối '?' trong đường dẫn, nên đổi tên về tên sạch rồi mở bản
	// sao như một DB độc lập và đọc lại dữ liệu.
	clean := filepath.Join(filepath.Dir(dst), "copy.db")
	if err := os.Rename(dst, clean); err != nil {
		t.Fatal(err)
	}
	c, err := Open(ctx, clean)
	if err != nil {
		t.Fatalf("mở bản sao: %v", err)
	}
	defer func() { _ = c.Close() }()
	if v, ok, _ := c.GetSetting(ctx, "web"); !ok || v != "nginx" {
		t.Errorf("setting trong bản sao = %q, %v", v, ok)
	}
	if _, ok, _ := c.Backup(ctx, b.ID); !ok {
		t.Error("dòng backups không có trong bản sao")
	}
}

func TestSnapshotDBTuChoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	dir := t.TempDir()
	exist := filepath.Join(dir, "da-co.db")
	if err := os.WriteFile(exist, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"rỗng":                 "",
		"đường dẫn tương đối":  "tpan.db",
		"đã tồn tại":           exist,
		"thư mục cha không có": filepath.Join(dir, "khong", "co", "x.db"),
	}
	for name, p := range cases {
		err := s.SnapshotDB(ctx, p)
		if err == nil {
			t.Errorf("%s: muốn lỗi", name)
			continue
		}
		if !strings.Contains(err.Error(), "tpan.db") && !strings.Contains(err.Error(), "bản sao") {
			t.Errorf("%s: lỗi thiếu ngữ cảnh: %v", name, err)
		}
	}
	// File có sẵn không bị đụng tới.
	if b, _ := os.ReadFile(exist); string(b) != "x" {
		t.Errorf("file có sẵn bị ghi đè: %q", b)
	}
	// Thất bại không để lại file rác.
	missingParent := filepath.Join(dir, "khong", "co", "x.db")
	if _, err := os.Stat(missingParent); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file rác sau lỗi: %v", err)
	}
}

func cronSample(domain string, started time.Time, status string) CronRun {
	return CronRun{Domain: domain, Started: started, Duration: 1500 * time.Millisecond,
		Status: site.CronStatus(status), ExitCode: 0, Output: "Success: Executed 2 cron events."}
}

func TestCronRunGhiDeVaSapTheoDomain(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	for _, d := range []string{"c.example.com", "a.example.com", "b.example.com"} {
		if err := s.SaveCronRun(ctx, cronSample(d, t0, "ok")); err != nil {
			t.Fatal(err)
		}
	}
	// Ghi đè dòng của a: chỉ còn một dòng mỗi domain.
	again := cronSample("a.example.com", t0.Add(5*time.Minute), "fail")
	again.ExitCode = 1
	again.Output = "Error: hết bộ nhớ"
	if err := s.SaveCronRun(ctx, again); err != nil {
		t.Fatal(err)
	}
	runs, err := s.CronRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var doms []string
	for _, r := range runs {
		doms = append(doms, r.Domain)
	}
	if got := strings.Join(doms, ","); got != "a.example.com,b.example.com,c.example.com" {
		t.Fatalf("thứ tự/số dòng = %s", got)
	}
	if runs[0] != again {
		t.Errorf("dòng a sau ghi đè:\n got  %+v\n want %+v", runs[0], again)
	}
	if runs[0].Duration != 1500*time.Millisecond {
		t.Errorf("Duration = %v", runs[0].Duration)
	}
}

func TestCronRunsRongLaSliceRong(t *testing.T) {
	s, _ := openTemp(t)
	runs, err := s.CronRuns(context.Background())
	if err != nil || runs == nil || len(runs) != 0 {
		t.Errorf("runs=%v err=%v, muốn slice rỗng không nil", runs, err)
	}
}

func TestSaveCronRunTuChoi(t *testing.T) {
	s, _ := openTemp(t)
	bad := cronSample("", time.Now(), "ok")
	if err := s.SaveCronRun(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "cron") {
		t.Errorf("thiếu domain: err = %v", err)
	}
	bad = cronSample("a.example.com", time.Now(), "")
	if err := s.SaveCronRun(context.Background(), bad); err == nil {
		t.Error("thiếu status: muốn lỗi")
	}
}
