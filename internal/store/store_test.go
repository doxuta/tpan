package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

// openTemp mở store trong thư mục tạm; test không chạm vào chỗ nào khác của máy.
func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "tpan.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

var t0 = time.Date(2026, 11, 19, 10, 0, 0, 123456789, time.UTC)

func sampleJob(id string, started time.Time) engine.Job {
	return engine.Job{
		ID:      id,
		Kind:    "stack.install",
		Target:  "nginx",
		Title:   i18n.T("Cài nginx", "Install nginx"),
		Status:  engine.JobRunning,
		Started: started,
	}
}

func TestOpenTaoThuMucVaQuyenFile(t *testing.T) {
	s, path := openTemp(t)
	// Ghi một dòng để chắc cả file -wal cũng đã được tạo.
	if err := s.SetSetting(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("quyền file DB = %o, muốn 600", got)
	}
	dst, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dst.Mode().Perm(); got != 0o700 {
		t.Errorf("quyền thư mục cha = %o, muốn 700", got)
	}
}

func TestOpenSietQuyenFileCu(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tpan.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	st, _ := os.Stat(path)
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("quyền file cũ sau Open = %o, muốn 600", got)
	}
}

func TestOpenPragma(t *testing.T) {
	s, _ := openTemp(t)
	for pragma, want := range map[string]string{
		"journal_mode": "wal",
		"foreign_keys": "1",
	} {
		var got string
		if err := s.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %q, muốn %q", pragma, got, want)
		}
	}
	var bt int
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&bt); err != nil {
		t.Fatal(err)
	}
	if bt <= 0 {
		t.Errorf("busy_timeout = %d, muốn > 0", bt)
	}
}

func TestMoHaiLanKhongChayLaiMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tpan.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "web", "nginx"); err != nil {
		t.Fatal(err)
	}
	var v1 int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v1); err != nil {
		t.Fatal(err)
	}
	if v1 < 1 {
		t.Fatalf("user_version = %d sau lần mở đầu, muốn >= 1", v1)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Nếu migration chạy lại, CREATE TABLE sẽ báo bảng đã có, hoặc dữ liệu mất.
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("mở lần hai: %v", err)
	}
	defer func() { _ = s2.Close() }()
	var v2 int
	if err := s2.db.QueryRow("PRAGMA user_version").Scan(&v2); err != nil {
		t.Fatal(err)
	}
	if v2 != v1 {
		t.Errorf("user_version đổi từ %d sang %d", v1, v2)
	}
	got, ok, err := s2.GetSetting(ctx, "web")
	if err != nil || !ok || got != "nginx" {
		t.Errorf("GetSetting sau mở lại = %q, %v, %v", got, ok, err)
	}
}

func TestOpenTuChoiDBMoiHonBanHienTai(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tpan.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 9999"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := Open(ctx, path); err == nil {
		t.Fatal("Open DB có user_version lớn hơn bản tpan biết phải lỗi")
	}
}

func TestMigrateTheoThuTuVaHoanTacKhiLoi(t *testing.T) {
	ctx := context.Background()
	open := func() *sql.DB {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "m.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		db.SetMaxOpenConns(1)
		return db
	}
	version := func(db *sql.DB) (v int) {
		if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	t.Run("áp theo số, không theo tên file xếp chữ", func(t *testing.T) {
		db := open()
		fsys := fstest.MapFS{
			"migrations/002_b.sql": {Data: []byte("INSERT INTO t VALUES (2);")},
			"migrations/001_a.sql": {Data: []byte("CREATE TABLE t (n INTEGER); INSERT INTO t VALUES (1);")},
		}
		if err := migrate(ctx, db, fsys); err != nil {
			t.Fatal(err)
		}
		if v := version(db); v != 2 {
			t.Errorf("user_version = %d, muốn 2", v)
		}
		rows, err := db.Query("SELECT n FROM t ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var got []int
		for rows.Next() {
			var n int
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			got = append(got, n)
		}
		if !reflect.DeepEqual(got, []int{1, 2}) {
			t.Errorf("thứ tự áp = %v, muốn [1 2]", got)
		}
	})

	t.Run("một bản lỗi thì cả đợt bị hoàn tác", func(t *testing.T) {
		db := open()
		fsys := fstest.MapFS{
			"migrations/001_a.sql": {Data: []byte("CREATE TABLE t (n INTEGER);")},
			"migrations/002_b.sql": {Data: []byte("INSERT INTO khong_co VALUES (1);")},
		}
		if err := migrate(ctx, db, fsys); err == nil {
			t.Fatal("muốn lỗi")
		}
		if v := version(db); v != 0 {
			t.Errorf("user_version = %d sau lỗi, muốn 0", v)
		}
		if _, err := db.Exec("INSERT INTO t VALUES (1)"); err == nil {
			t.Error("bảng t còn tồn tại dù migration đã hoàn tác")
		}
	})

	t.Run("số nhảy cóc được, bản số nhỏ ghép vào sau vẫn được áp", func(t *testing.T) {
		db := open()
		first := fstest.MapFS{
			"migrations/001_a.sql": {Data: []byte("CREATE TABLE t (n INTEGER); INSERT INTO t VALUES (1);")},
			"migrations/500_e.sql": {Data: []byte("INSERT INTO t VALUES (500);")},
		}
		if err := migrate(ctx, db, first); err != nil {
			t.Fatal(err)
		}
		// M4 (số 400) được ghép sau M5 (số 500): DB đã ở 500 vẫn phải nhận 400.
		later := fstest.MapFS{
			"migrations/001_a.sql": first["migrations/001_a.sql"],
			"migrations/400_d.sql": {Data: []byte("INSERT INTO t VALUES (400);")},
			"migrations/500_e.sql": first["migrations/500_e.sql"],
		}
		if err := migrate(ctx, db, later); err != nil {
			t.Fatal(err)
		}
		var sum int
		if err := db.QueryRow("SELECT SUM(n) FROM t").Scan(&sum); err != nil || sum != 901 {
			t.Errorf("tổng = %d (%v), muốn 901: mỗi bản áp đúng một lần", sum, err)
		}
		if v := version(db); v != 500 {
			t.Errorf("user_version = %d, muốn 500", v)
		}
		// Chạy lại không áp gì thêm.
		if err := migrate(ctx, db, later); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT SUM(n) FROM t").Scan(&sum); err != nil || sum != 901 {
			t.Errorf("chạy lại: tổng = %d, muốn 901", sum)
		}
	})

	t.Run("DB của bản tpan cũ (chỉ có user_version) không áp lại", func(t *testing.T) {
		db := open()
		if _, err := db.Exec("CREATE TABLE t (n INTEGER); PRAGMA user_version = 1"); err != nil {
			t.Fatal(err)
		}
		fsys := fstest.MapFS{
			"migrations/001_a.sql": {Data: []byte("CREATE TABLE t (n INTEGER);")},
			"migrations/002_b.sql": {Data: []byte("INSERT INTO t VALUES (2);")},
		}
		if err := migrate(ctx, db, fsys); err != nil {
			t.Fatalf("áp lại 001 sẽ lỗi vì bảng t đã có: %v", err)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&n); err != nil || n != 1 {
			t.Errorf("số dòng = %d, muốn 1", n)
		}
	})

	t.Run("số bản trùng hoặc tên sai thì lỗi", func(t *testing.T) {
		for name, fsys := range map[string]fstest.MapFS{
			"trùng": {
				"migrations/001_a.sql": {Data: []byte("SELECT 1;")},
				"migrations/001_b.sql": {Data: []byte("SELECT 1;")},
			},
			"tên sai": {
				"migrations/abc.sql": {Data: []byte("SELECT 1;")},
			},
		} {
			if err := migrate(ctx, open(), fsys); err == nil {
				t.Errorf("%s: muốn lỗi", name)
			}
		}
	})
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	if _, ok, err := s.GetSetting(ctx, "web"); err != nil || ok {
		t.Fatalf("khoá chưa có: ok=%v err=%v", ok, err)
	}
	if err := s.SetSetting(ctx, "web", "nginx"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.GetSetting(ctx, "web"); err != nil || !ok || got != "nginx" {
		t.Fatalf("GetSetting = %q, %v, %v", got, ok, err)
	}
	// Ghi đè.
	if err := s.SetSetting(ctx, "web", "apache"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.GetSetting(ctx, "web"); got != "apache" {
		t.Errorf("sau ghi đè = %q, muốn apache", got)
	}
	// Giá trị rỗng vẫn là "có".
	if err := s.SetSetting(ctx, "email", ""); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := s.GetSetting(ctx, "email"); !ok || got != "" {
		t.Errorf("giá trị rỗng: %q, %v", got, ok)
	}
	all, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"web": "apache", "email": ""}; !reflect.DeepEqual(all, want) {
		t.Errorf("Settings = %v, muốn %v", all, want)
	}
}

func TestJobVaEventKhuHoi(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	job := sampleJob("aaaaaaaaaaaaaaaa", t0)
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !jobEqual(got, job) {
		t.Errorf("Job sau CreateJob = %+v, muốn %+v", got, job)
	}
	if !got.Finished.IsZero() {
		t.Errorf("Finished của job đang chạy phải là zero, được %v", got.Finished)
	}

	evs := []engine.Event{
		{JobID: job.ID, Seq: 1, Time: t0, Kind: engine.EventJobStart},
		{JobID: job.ID, Seq: 2, Time: t0.Add(time.Second), Kind: engine.EventStepStart, StepID: "apt.install:nginx"},
		{JobID: job.ID, Seq: 3, Time: t0.Add(2 * time.Second), Kind: engine.EventCommand, StepID: "apt.install:nginx", Message: "apt-get install -y nginx"},
		{JobID: job.ID, Seq: 4, Time: t0.Add(3 * time.Second), Kind: engine.EventCheck, Check: &checks.Result{
			ID:      "service.active:nginx",
			Stage:   checks.StageWeb,
			Title:   i18n.T("nginx đang chạy", "nginx is running"),
			Status:  checks.Fail,
			Summary: i18n.T("nginx không chạy", "nginx is not running"),
			Evidence: []checks.Evidence{
				{Command: "systemctl is-active nginx", ExitCode: 3, Output: "inactive\n"},
			},
			Cause:     i18n.T("dịch vụ chưa bật", "service not enabled"),
			ManualFix: []string{"systemctl enable --now nginx"},
			AutoFix:   "service.start",
		}},
	}
	for _, ev := range evs {
		if err := s.AddEvent(ctx, ev); err != nil {
			t.Fatalf("AddEvent seq %d: %v", ev.Seq, err)
		}
	}
	got2, err := s.Events(ctx, job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != len(evs) {
		t.Fatalf("Events trả %d dòng, muốn %d", len(got2), len(evs))
	}
	for i := range evs {
		if !eventEqual(got2[i], evs[i]) {
			t.Errorf("event %d khứ hồi sai:\n got  %+v\n want %+v", i, got2[i], evs[i])
		}
	}
	if !reflect.DeepEqual(got2[3].Check, evs[3].Check) {
		t.Errorf("Event.Check khứ hồi sai:\n got  %+v\n want %+v", got2[3].Check, evs[3].Check)
	}

	// afterSeq: chỉ lấy các dòng mới hơn, để client theo dõi tiếp.
	after, err := s.Events(ctx, job.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].Seq != 3 || after[1].Seq != 4 {
		t.Errorf("Events(afterSeq=2) = %+v", after)
	}
	none, err := s.Events(ctx, job.ID, 4)
	if err != nil || len(none) != 0 {
		t.Errorf("Events(afterSeq=4) = %v, %v", none, err)
	}
	other, err := s.Events(ctx, "khong-co", 0)
	if err != nil || len(other) != 0 {
		t.Errorf("Events của job lạ = %v, %v", other, err)
	}
}

func TestEventsTheoSeqTangDan(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	j := sampleJob("j1", t0)
	if err := s.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	// Chèn lệch thứ tự để chắc kết quả được sắp theo Seq chứ không theo thứ tự chèn.
	for _, seq := range []int{3, 1, 10, 2} {
		if err := s.AddEvent(ctx, engine.Event{JobID: j.ID, Seq: seq, Time: t0, Kind: engine.EventOutput, Message: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Events(ctx, j.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for _, e := range got {
		seqs = append(seqs, e.Seq)
	}
	if !reflect.DeepEqual(seqs, []int{1, 2, 3, 10}) {
		t.Errorf("thứ tự Seq = %v", seqs)
	}
}

func TestAddEventLoi(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	// Job chưa tạo: khoá ngoại phải chặn, không để log mồ côi.
	if err := s.AddEvent(ctx, engine.Event{JobID: "chua-co", Seq: 1, Time: t0, Kind: engine.EventJobStart}); err == nil {
		t.Error("AddEvent cho job chưa có phải lỗi")
	}
	j := sampleJob("j1", t0)
	if err := s.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	ev := engine.Event{JobID: j.ID, Seq: 1, Time: t0, Kind: engine.EventJobStart}
	if err := s.AddEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if err := s.AddEvent(ctx, ev); err == nil {
		t.Error("AddEvent trùng (job_id, seq) phải lỗi")
	}
	if err := s.CreateJob(ctx, j); err == nil {
		t.Error("CreateJob trùng id phải lỗi")
	}
}

func TestFinishJob(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	j := sampleJob("j1", t0)
	if err := s.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	j.Status = engine.JobRolledBack
	j.Finished = t0.Add(5 * time.Second)
	j.Error = "apt-get install nginx: exit 100"
	if err := s.FinishJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !jobEqual(got, j) {
		t.Errorf("Job sau FinishJob = %+v, muốn %+v", got, j)
	}

	err = s.FinishJob(ctx, sampleJob("khong-co", t0))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FinishJob job lạ: %v, muốn ErrNotFound", err)
	}
}

func TestJobsMoiNhatTruoc(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	// Hai mốc cách nhau dưới một giây, mốc sớm có phần lẻ ngắn hơn khi in:
	// nếu định dạng thời gian không rộng cố định thì thứ tự sẽ sai.
	times := map[string]time.Time{
		"a": t0,
		"b": t0.Add(-time.Hour),
		"c": time.Date(2026, 11, 19, 10, 0, 0, 500000000, time.UTC),
		"d": time.Date(2026, 11, 19, 10, 0, 1, 0, time.UTC),
	}
	for id, at := range times {
		if err := s.CreateJob(ctx, sampleJob(id, at)); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(js []engine.Job) (out []string) {
		for _, j := range js {
			out = append(out, j.ID)
		}
		return out
	}
	// t0 có phần lẻ .123456789 nên a < c (.5) < d (giây kế).
	all, err := s.Jobs(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"d", "c", "a", "b"}; !reflect.DeepEqual(ids(all), want) {
		t.Errorf("Jobs(0) = %v, muốn %v", ids(all), want)
	}
	two, err := s.Jobs(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"d", "c"}; !reflect.DeepEqual(ids(two), want) {
		t.Errorf("Jobs(2) = %v, muốn %v", ids(two), want)
	}
}

func TestJobKhongCo(t *testing.T) {
	s, _ := openTemp(t)
	_, err := s.Job(context.Background(), "khong-co")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Job lạ: %v, muốn ErrNotFound", err)
	}
}

// Hai tiến trình cùng mở tpan.db (`tpan serve` và một lệnh CLI): transaction
// đọc trước rồi ghi sau không được hỏng vì tiến trình kia ghi xen vào giữa.
// Với transaction deferred (mặc định), lần ghi trong tx gặp SQLITE_BUSY ngay
// mà không chờ busy_timeout; e2e M4 gặp đúng lỗi này ở migration lúc
// `tpan setup` khởi động tpan.service.
func TestTxReadThenWriteWithOtherWriter(t *testing.T) {
	ctx := context.Background()
	s1, path := openTemp(t)
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	tx, err := s1.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM settings").Scan(&n); err != nil {
		t.Fatal(err)
	}
	// Tiến trình kia ghi trong lúc tx đang mở: phải chờ tx xong, không chen vào.
	done := make(chan error, 1)
	go func() { done <- s2.SetSetting(ctx, "khac", "1") }()
	time.Sleep(200 * time.Millisecond)
	if _, err := tx.ExecContext(ctx, "INSERT INTO settings (key, value, updated_at) VALUES ('tx', '1', 'x')"); err != nil {
		t.Fatalf("ghi trong tx sau khi tiến trình khác ghi: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("ghi của tiến trình kia: %v", err)
	}
}

func TestManagedFiles(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	now := t0
	s.now = func() time.Time { return now }

	if got, err := s.ManagedFiles(ctx); err != nil || len(got) != 0 {
		t.Fatalf("ban đầu: %v, %v", got, err)
	}
	if err := s.RecordFile(ctx, "/etc/nginx/nginx.conf", "aa", "job1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFile(ctx, "/etc/nginx/sites/a.conf", "bb", "job1"); err != nil {
		t.Fatal(err)
	}
	// Ghi đè theo path: không sinh dòng thứ hai.
	now = t0.Add(time.Minute)
	if err := s.RecordFile(ctx, "/etc/nginx/nginx.conf", "cc", "job2"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ManagedFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []ManagedFile{
		{Path: "/etc/nginx/nginx.conf", SHA256: "cc", JobID: "job2", Updated: t0.Add(time.Minute)},
		{Path: "/etc/nginx/sites/a.conf", SHA256: "bb", JobID: "job1", Updated: t0},
	}
	if len(got) != len(want) {
		t.Fatalf("ManagedFiles = %+v", got)
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].SHA256 != want[i].SHA256 || got[i].JobID != want[i].JobID || !got[i].Updated.Equal(want[i].Updated) {
			t.Errorf("ManagedFiles[%d] = %+v, muốn %+v", i, got[i], want[i])
		}
	}

	if sha, ok, err := s.FileSHA256(ctx, "/etc/nginx/nginx.conf"); err != nil || !ok || sha != "cc" {
		t.Errorf("FileSHA256 = %q, %v, %v; muốn cc, true", sha, ok, err)
	}
	if sha, ok, err := s.FileSHA256(ctx, "/khong/co"); err != nil || ok || sha != "" {
		t.Errorf("FileSHA256 file lạ = %q, %v, %v; muốn rỗng, false, nil", sha, ok, err)
	}

	if err := s.ForgetFile(ctx, "/etc/nginx/nginx.conf"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ManagedFiles(ctx)
	if len(got) != 1 || got[0].Path != "/etc/nginx/sites/a.conf" {
		t.Errorf("sau ForgetFile: %+v", got)
	}
	// Quên file chưa từng ghi không phải lỗi: engine gọi cho mọi bước Delete.
	if err := s.ForgetFile(ctx, "/khong/co"); err != nil {
		t.Errorf("ForgetFile file lạ: %v", err)
	}
}

// jobEqual so sánh Job nhưng dùng Time.Equal, vì DB trả về UTC còn đầu vào có thể ở múi giờ khác.
func jobEqual(a, b engine.Job) bool {
	return a.ID == b.ID && a.Kind == b.Kind && a.Target == b.Target && a.Title == b.Title &&
		a.Status == b.Status && a.Started.Equal(b.Started) && a.Finished.Equal(b.Finished) && a.Error == b.Error
}

func eventEqual(a, b engine.Event) bool {
	return a.JobID == b.JobID && a.Seq == b.Seq && a.Time.Equal(b.Time) && a.Kind == b.Kind &&
		a.StepID == b.StepID && a.Message == b.Message
}

func TestAddAudit(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.AddAudit(ctx, AuditEntry{Time: time.Unix(0, 0), IP: "cli", Action: "admin.passwd", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM audit WHERE action = 'admin.passwd'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("đếm audit = %d, %v", n, err)
	}
	if err := s.AddAudit(ctx, AuditEntry{IP: "cli"}); err == nil {
		t.Error("thiếu thao tác phải lỗi")
	}
}
