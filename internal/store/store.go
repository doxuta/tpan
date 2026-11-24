// Package store lưu trạng thái của tpan bằng một file SQLite: cài đặt, lịch sử
// job và log từng bước, danh sách file tpan đã ghi (kèm sha256).
//
// Đặc tả: mục 4.4 của docs/KE-HOACH.md.
//
// Dùng modernc.org/sqlite (Go thuần, không cgo) để build chéo sang Linux mà
// không cần trình biên dịch C. Gói này import engine (để cài đặt engine.Journal);
// engine không import store.
//
// Không ghi bí mật: store lưu đúng thứ engine đưa. Output lệnh có thể chứa
// system.Secret, nhưng Secret đã tự in thành •••• trước khi tới đây.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // đăng ký driver tên "sqlite"

	"github.com/doxuta/tpan/internal/engine"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound: không có dòng nào khớp. Dùng errors.Is để kiểm.
var ErrNotFound = errors.New("store: không tìm thấy")

// timeLayout là RFC3339 với phần lẻ giây luôn đủ 9 chữ số và múi giờ UTC.
// time.RFC3339Nano cắt số 0 cuối nên độ dài chuỗi đổi theo giá trị, và khi đó
// so sánh chuỗi (ORDER BY started_at) sẽ xếp sai: "10:00:05Z" lớn hơn "10:00:05.5Z"
// vì 'Z' đứng sau '.'. Độ rộng cố định thì thứ tự chuỗi bằng thứ tự thời gian;
// time.RFC3339Nano vẫn đọc ngược được chuỗi này.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func fmtTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("đọc thời gian %q: %w", s, err)
	}
	return t.UTC(), nil
}

// Store là trạng thái của tpan trên đĩa. An toàn khi dùng từ nhiều goroutine.
type Store struct {
	db *sql.DB
	// now cho test đặt thời điểm cố định cho updated_at.
	now func() time.Time
}

// ponytail: bảo đảm engine.Journal được cài đặt đủ ngay lúc biên dịch.
var _ engine.Journal = (*Store)(nil)

// Open mở (và tạo nếu chưa có) file DB tại path rồi chạy migration.
//
// Thư mục cha tạo với quyền 0700 và file DB 0600: DB chứa lịch sử thao tác và
// log lệnh, chỉ root cần đọc. SQLite sao quyền của file chính sang file -wal
// và -shm nên chúng cũng 0600.
func Open(ctx context.Context, path string) (*Store, error) {
	// Driver cắt DSN ở dấu '?' để tách tham số, nên path chứa '?' sẽ bị hiểu sai.
	if path == "" || strings.ContainsRune(path, '?') {
		return nil, fmt.Errorf("mở DB: đường dẫn không hợp lệ %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("tạo thư mục cho DB %s: %w", path, err)
	}
	// Tạo file trước với đúng quyền, để SQLite không tạo nó với 0644 theo umask.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("tạo file DB %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("đóng file DB %s: %w", path, err)
	}
	// File có sẵn từ trước có thể đang rộng quyền.
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("siết quyền file DB %s: %w", path, err)
	}

	// Pragma đặt qua DSN để áp cho mọi kết nối. WAL: đọc không chặn ghi (giao
	// diện đọc log trong lúc engine đang ghi). foreign_keys: SQLite mặc định
	// tắt, bật thì job_events không thể mồ côi. busy_timeout: chờ thay vì báo
	// "database is locked" ngay. _txlock=immediate: transaction lấy quyền ghi
	// ngay ở BEGIN (có chờ theo busy_timeout). Mặc định (deferred) transaction
	// đọc trước rồi mới xin ghi; ở WAL, nếu tiến trình khác đã ghi xen vào giữa
	// thì lúc xin ghi SQLite trả SQLITE_BUSY ngay, không chờ busy_timeout (tìm
	// thấy ở e2e M4: `tpan serve` do `tpan setup` khởi động chết vì migration
	// gặp "database is locked" trong lúc setup đang ghi sự kiện job).
	// https://www.sqlite.org/lang_transaction.html#deferred_immediate_and_exclusive_transactions
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("mở DB %s: %w", path, err)
	}
	// ponytail: một kết nối là đủ cho tải của một panel và loại hẳn lỗi khoá
	// giữa các kết nối. Nâng lên khi đo thấy đọc bị chặn.
	db.SetMaxOpenConns(1)

	if err := migrate(ctx, db, migrationFS); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migration DB %s: %w", path, err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Close đóng DB. Kết nối cuối đóng thì SQLite gộp file -wal vào file chính.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("đóng DB: %w", err)
	}
	return nil
}

type migration struct {
	version int
	name    string
}

// loadMigrations đọc các file migrations/NNN_ten.sql và sắp theo số. Số không
// cần liên tục: từ M4 các mốc làm song song, mỗi mốc dùng một khoảng riêng
// (mốc n dùng n*100 đến n*100+99, mục 6.3 của agent-dev/CHAY-NHIEU-AGENT.md).
// Trùng số hay tên sai thì lỗi ngay lúc test thay vì lúc người dùng nâng cấp.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("đọc thư mục migrations: %w", err)
	}
	var ms []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		num, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if err != nil || v < 1 {
			return nil, fmt.Errorf("tên migration %q phải dạng NNN_ten.sql", e.Name())
		}
		ms = append(ms, migration{version: v, name: e.Name()})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i := 1; i < len(ms); i++ {
		if ms[i].version == ms[i-1].version {
			return nil, fmt.Errorf("migration %q và %q trùng số %d", ms[i-1].name, ms[i].name, ms[i].version)
		}
	}
	return ms, nil
}

// migrate áp mọi migration chưa áp, theo thứ tự số, trong MỘT transaction.
// Bản nào đã áp được ghi vào bảng schema_migrations, không chỉ một con số
// user_version: số có thể nhảy cóc và các mốc được ghép vào dev không theo thứ
// tự số (M5 có thể vào trước M4), nên "áp mọi bản lớn hơn bản đã có" sẽ bỏ sót
// bản 4xx trên một DB đã ở 5xx. Lỗi ở bản nào thì cả đợt bị hoàn tác; DDL của
// SQLite có transaction nên không bao giờ để DB nửa vời.
//
// DB do bản tpan cũ tạo (chỉ có user_version, chưa có schema_migrations): mọi
// bản có số <= user_version được coi là đã áp, vì bản cũ áp liên tục từ 1.
func migrate(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	ms, err := loadMigrations(fsys)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mở transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // không làm gì nếu đã Commit

	if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY) STRICT"); err != nil {
		return fmt.Errorf("tạo bảng schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := tx.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("đọc schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return fmt.Errorf("đọc schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("đọc schema_migrations: %w", err)
	}

	known := map[int]bool{}
	maxKnown := 0
	for _, m := range ms {
		known[m.version] = true
		maxKnown = max(maxKnown, m.version)
	}
	var current int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("đọc user_version: %w", err)
	}
	if current > maxKnown {
		// DB do tpan mới hơn tạo ra; chạy tiếp có thể làm hỏng dữ liệu của nó.
		return fmt.Errorf("DB ở bản %d, tpan này chỉ biết tới bản %d: hãy dùng tpan mới hơn", current, maxKnown)
	}
	if len(applied) == 0 {
		for _, m := range ms {
			if m.version <= current {
				applied[m.version] = true
				if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
					return fmt.Errorf("ghi schema_migrations: %w", err)
				}
			}
		}
	}
	for v := range applied {
		if !known[v] {
			return fmt.Errorf("DB đã áp migration %d mà tpan này không có: hãy dùng tpan mới hơn", v)
		}
	}

	for _, m := range ms {
		if applied[m.version] {
			continue
		}
		body, err := fs.ReadFile(fsys, "migrations/"+m.name)
		if err != nil {
			return fmt.Errorf("đọc %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("áp %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
			return fmt.Errorf("ghi schema_migrations: %w", err)
		}
	}
	// user_version giữ số lớn nhất đã áp, để công cụ ngoài (sqlite3) xem nhanh.
	// PRAGMA không nhận tham số ?, nhưng số này do mã tính, không phải đầu vào ngoài.
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(maxKnown)); err != nil {
		return fmt.Errorf("ghi user_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

// GetSetting trả giá trị của khoá key; ok=false nếu chưa đặt.
func (s *Store) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc cài đặt %s: %w", key, err)
	}
	return value, true, nil
}

// SetSetting ghi (hoặc ghi đè) cài đặt key.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, fmtTime(s.now()))
	if err != nil {
		return fmt.Errorf("ghi cài đặt %s: %w", key, err)
	}
	return nil
}

// Settings trả mọi cài đặt.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("đọc cài đặt: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("đọc dòng cài đặt: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc cài đặt: %w", err)
	}
	return out, nil
}

// CreateJob ghi một job mới (engine.Journal).
func (s *Store) CreateJob(ctx context.Context, job engine.Job) error {
	// owner (M13): người gọi đặt sẵn thì dùng; không thì chủ của site hay DB
	// rời có tên là Target (chỉ với loại job mà jobOwnerLookup cho phép), rồi
	// tới Actor, cuối cùng là admin. Suy ở đây, một chỗ, thay vì bắt mọi ca sử
	// dụng của core tự truyền chủ vào engine. Tra với tên rỗng không ra dòng nào
	// (NULL), COALESCE đi tiếp.
	siteName, dbName := jobOwnerLookup(job.Kind, job.Target)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, kind, target, title_vi, title_en, status, started_at, finished_at, error, actor, owner)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''),
		   (SELECT owner FROM sites WHERE domain = ?),
		   (SELECT owner FROM databases WHERE name = ?),
		   NULLIF(?, ''), 'admin'))`,
		job.ID, job.Kind, job.Target, job.Title.Vi, job.Title.En, string(job.Status),
		fmtTime(job.Started), nullTime(job.Finished), job.Error, job.Actor,
		job.Owner, siteName, dbName, job.Actor)
	if err != nil {
		return fmt.Errorf("tạo job %s: %w", job.ID, err)
	}
	return nil
}

// jobOwnerLookup (P7 của rà soát v0.2.0) cho biết CreateJob được tra chủ của
// job ở bảng nào theo Target: trả domain cần tra trong sites, tên cần tra
// trong databases, rỗng là không tra. Chỉ loại job có Target chắc chắn là
// domain của site (site.*, db.* của DB site, và các loại liệt kê dưới) hay
// tên đầy đủ của DB rời (dbs.*) mới được tra. Loại khác, kể cả loại thêm sau
// này, KHÔNG suy chủ từ tên: Target của chúng có thể là tên user (user.*),
// tên gói (cage.sync), tên dịch vụ hay tên khác mà khách tự đặt được cho
// trùng. Ví dụ: khách khach tạo DB rời "a" thì tên đầy đủ là khach_a; nếu tra
// databases cho mọi loại thì job user.suspend admin chạy trên user khach_a
// thành của khach, và khach đọc được nó.
//
// Sửa tự động ("fix." + tên cách sửa, core/health.go) liệt kê từng cái: Target
// của fix.service.start là tên dịch vụ (php8.3-fpm), mà validate.ParseDomain
// nhận "php8.3-fpm" là một domain hợp lệ.
func jobOwnerLookup(kind, target string) (siteDomain, dbName string) {
	switch {
	case strings.HasPrefix(kind, "dbs."):
		return "", target
	case strings.HasPrefix(kind, "site."), strings.HasPrefix(kind, "db."):
		return target, ""
	}
	switch kind {
	case "backup.create", "backup.restore", "cage.enable", "cage.disable", "cage.restart", "php.use",
		// Sửa tự động có Target là domain của site.
		"fix.php.restart", "fix.web.rewrite", "fix.wp.config.perm", "fix.db.unlock", "fix.ssl.issue",
		"fix.php.pool.rewrite", "fix.log.perm",
		"fix.cage.start", "fix.cage.restart", "fix.cage.sync", "fix.cage.enable", "fix.cage.disable":
		return target, ""
	case "fix.db.user-limit": // Target là user DB, trùng tên DB rời khi đó là DB rời
		return "", target
	}
	return "", ""
}

// FinishJob cập nhật Status, Finished và Error của job đã tạo (engine.Journal).
func (s *Store) FinishJob(ctx context.Context, job engine.Job) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = ?, finished_at = ?, error = ? WHERE id = ?",
		string(job.Status), nullTime(job.Finished), job.Error, job.ID)
	if err != nil {
		return fmt.Errorf("kết thúc job %s: %w", job.ID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("kết thúc job %s: %w", job.ID, err)
	} else if n == 0 {
		return fmt.Errorf("kết thúc job %s: %w", job.ID, ErrNotFound)
	}
	return nil
}

// nullTime: thời gian zero (chưa xong) lưu thành NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return fmtTime(t)
}

// AddEvent ghi một dòng log của job (engine.Journal). Cột data giữ JSON của
// cả Event và là nguồn khi đọc lại; các cột còn lại để lọc và xem.
func (s *Store) AddEvent(ctx context.Context, ev engine.Event) error {
	data, err := marshalEvent(ev)
	if err != nil {
		return fmt.Errorf("ghi sự kiện %s#%d: %w", ev.JobID, ev.Seq, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO job_events (job_id, seq, time, kind, step_id, message, data)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.JobID, ev.Seq, fmtTime(ev.Time), string(ev.Kind), ev.StepID, ev.Message, data)
	if err != nil {
		return fmt.Errorf("ghi sự kiện %s#%d: %w", ev.JobID, ev.Seq, err)
	}
	return nil
}

// RecordFile ghi sha256 của một file tpan vừa ghi; ghi đè nếu path đã có (engine.Journal).
func (s *Store) RecordFile(ctx context.Context, path, sha256Hex, jobID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO managed_files (path, sha256, job_id, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (path) DO UPDATE SET
		   sha256 = excluded.sha256, job_id = excluded.job_id, updated_at = excluded.updated_at`,
		path, sha256Hex, jobID, fmtTime(s.now()))
	if err != nil {
		return fmt.Errorf("ghi file quản lý %s: %w", path, err)
	}
	return nil
}

// FileSHA256 trả sha256 (hex) đang ghi cho path trong managed_files; ok=false
// khi path không do tpan quản lý (core.FileRecords).
func (s *Store) FileSHA256(ctx context.Context, path string) (sha string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT sha256 FROM managed_files WHERE path = ?", path).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc file quản lý %s: %w", path, err)
	}
	return sha, true, nil
}

// ForgetFile bỏ path khỏi danh sách quản lý. File chưa từng được ghi không phải lỗi (engine.Journal).
func (s *Store) ForgetFile(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM managed_files WHERE path = ?", path); err != nil {
		return fmt.Errorf("bỏ file quản lý %s: %w", path, err)
	}
	return nil
}

// rowScanner là phần chung của *sql.Row và *sql.Rows để dùng một hàm đọc Job.
type rowScanner interface{ Scan(dest ...any) error }

const jobColumns = "id, kind, target, title_vi, title_en, status, started_at, finished_at, error, actor, owner"

func scanJob(r rowScanner) (engine.Job, error) {
	var (
		j        engine.Job
		status   string
		started  string
		finished sql.NullString
	)
	if err := r.Scan(&j.ID, &j.Kind, &j.Target, &j.Title.Vi, &j.Title.En, &status, &started, &finished, &j.Error, &j.Actor, &j.Owner); err != nil {
		return engine.Job{}, err
	}
	j.Status = engine.JobStatus(status)
	var err error
	if j.Started, err = parseTime(started); err != nil {
		return engine.Job{}, err
	}
	if finished.Valid {
		if j.Finished, err = parseTime(finished.String); err != nil {
			return engine.Job{}, err
		}
	}
	return j, nil
}

// Job trả một job theo id; không có thì lỗi thoả errors.Is(err, ErrNotFound).
func (s *Store) Job(ctx context.Context, id string) (engine.Job, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return engine.Job{}, fmt.Errorf("job %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return engine.Job{}, fmt.Errorf("đọc job %s: %w", id, err)
	}
	return j, nil
}

// Jobs trả các job mới nhất trước. limit <= 0 nghĩa là không giới hạn.
func (s *Store) Jobs(ctx context.Context, limit int) ([]engine.Job, error) {
	return s.JobsOf(ctx, "", "", limit)
}

// JobsOf là Jobs có lọc: owner khác rỗng chỉ lấy job của chủ đó (kể cả job
// admin chạy trên site của họ, vì owner là chủ của đối tượng); actor khác rỗng
// chỉ lấy job do user đó chạy (màn Lịch sử của admin). Cả hai rỗng là mọi job.
func (s *Store) JobsOf(ctx context.Context, owner, actor string, limit int) ([]engine.Job, error) {
	if limit <= 0 {
		limit = -1 // SQLite: LIMIT âm là không giới hạn
	}
	// rowid làm khoá phụ để hai job cùng started_at có thứ tự ổn định (job tạo sau lên trước).
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+jobColumns+` FROM jobs WHERE (? = '' OR owner = ?) AND (? = '' OR actor = ?)
		 ORDER BY started_at DESC, rowid DESC LIMIT ?`, owner, owner, actor, actor, limit)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách job: %w", err)
	}
	defer func() { _ = rows.Close() }()
	jobs := []engine.Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("đọc dòng job: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc danh sách job: %w", err)
	}
	return jobs, nil
}

// JobOwner trả chủ của job (luật owner:job của server); ok=false khi không có job.
func (s *Store) JobOwner(ctx context.Context, id string) (owner string, ok bool, err error) {
	return s.ownerOf(ctx, "SELECT owner FROM jobs WHERE id = ?", "job "+id, id)
}

// Events trả các sự kiện của job có Seq > afterSeq, Seq tăng dần. Truyền 0 để
// lấy tất cả; client theo dõi log truyền Seq cuối cùng đã nhận.
func (s *Store) Events(ctx context.Context, jobID string, afterSeq int) ([]engine.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT data FROM job_events WHERE job_id = ? AND seq > ? ORDER BY seq", jobID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("đọc sự kiện của job %s: %w", jobID, err)
	}
	defer func() { _ = rows.Close() }()
	evs := []engine.Event{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("đọc dòng sự kiện của job %s: %w", jobID, err)
		}
		ev, err := unmarshalEvent(data)
		if err != nil {
			return nil, fmt.Errorf("giải mã sự kiện của job %s: %w", jobID, err)
		}
		evs = append(evs, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc sự kiện của job %s: %w", jobID, err)
	}
	return evs, nil
}

// ManagedFile là một file tpan đã ghi, kèm sha256 lúc ghi.
type ManagedFile struct {
	Path    string
	SHA256  string
	JobID   string
	Updated time.Time
}

// ManagedFiles trả mọi file tpan quản lý, xếp theo path.
func (s *Store) ManagedFiles(ctx context.Context) ([]ManagedFile, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT path, sha256, job_id, updated_at FROM managed_files ORDER BY path")
	if err != nil {
		return nil, fmt.Errorf("đọc file quản lý: %w", err)
	}
	defer func() { _ = rows.Close() }()
	files := []ManagedFile{}
	for rows.Next() {
		var (
			f       ManagedFile
			updated string
		)
		if err := rows.Scan(&f.Path, &f.SHA256, &f.JobID, &updated); err != nil {
			return nil, fmt.Errorf("đọc dòng file quản lý: %w", err)
		}
		if f.Updated, err = parseTime(updated); err != nil {
			return nil, fmt.Errorf("file quản lý %s: %w", f.Path, err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc file quản lý: %w", err)
	}
	return files, nil
}
