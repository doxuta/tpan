package store

// Truy vấn cho bảng backups và cron_runs (migration 900) và bản sao tpan.db
// (mục 4.4, 6.5, 6.10 của docs/KE-HOACH.md).
//
// Kiểu dòng (Backup, CronRun) nằm ở gói lá site: store không import được core
// (test của core import store, thành vòng import), còn core dùng bí danh
// core.Backup = site.Backup, nên *Store thoả thẳng core.BackupStore và
// core.CronStore.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/site"
)

// Backup là site.Backup (một dòng của bảng backups).
type Backup = site.Backup

// backupInsertColumns là các cột AddBackup ghi. Bảy cột của M18 (local,
// remote...) không có ở đây: dòng mới lấy DEFAULT của cột (trên máy, chưa lên
// kho), nên không người gọi nào lỡ ghi local=0 vì quên đặt trường.
const backupInsertColumns = "id, domain, kind, reason, path, size, sha256, wp_version, php_version, job_id, created_at"

// backupColumns là các cột đọc ra (M18 thêm bảy cột cuối, migration 1800).
const backupColumns = backupInsertColumns + ", local, remote, remote_key, remote_target, remote_at, remote_error, remote_tries"

func backupScan(r rowScanner) (Backup, error) {
	var (
		b                 Backup
		created, remoteAt string
		local             int
	)
	if err := r.Scan(&b.ID, &b.Domain, &b.Kind, &b.Reason, &b.Path, &b.Size, &b.SHA256,
		&b.WPVersion, &b.PHPVersion, &b.JobID, &created,
		&local, &b.Remote, &b.RemoteKey, &b.RemoteTarget, &remoteAt, &b.RemoteError, &b.RemoteTries); err != nil {
		return Backup{}, err
	}
	var err error
	if b.Created, err = parseTime(created); err != nil {
		return Backup{}, fmt.Errorf("backup %s: %w", b.ID, err)
	}
	b.Local = local != 0
	if remoteAt != "" {
		if b.RemoteAt, err = parseTime(remoteAt); err != nil {
			return Backup{}, fmt.Errorf("backup %s, remote_at: %w", b.ID, err)
		}
	}
	return b, nil
}

// AddBackup thêm một dòng và trả dòng đã ghi. ID rỗng thì sinh 16 hex ngẫu
// nhiên (8 byte từ crypto/rand: id lộ ra API và tên không đoán trước được);
// Created rỗng thì lấy giờ hiện tại. Path là UNIQUE: hai dòng không thể trỏ
// cùng một file, vì xoá dòng này sẽ kéo theo xoá file của dòng kia.
func (s *Store) AddBackup(ctx context.Context, b Backup) (Backup, error) {
	if b.Domain == "" || b.Kind == "" || b.Path == "" {
		return Backup{}, fmt.Errorf("thêm backup %q: thiếu domain, kind hoặc path (domain=%q kind=%q)", b.Path, b.Domain, b.Kind)
	}
	if b.Size < 0 {
		return Backup{}, fmt.Errorf("thêm backup %s: size âm (%d)", b.Path, b.Size)
	}
	if b.ID == "" {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return Backup{}, fmt.Errorf("thêm backup %s: sinh id: %w", b.Path, err)
		}
		b.ID = hex.EncodeToString(raw[:])
	}
	if b.Created.IsZero() {
		b.Created = s.now()
	}
	b.Created = b.Created.UTC()
	// owner (M13): chủ của site cùng domain lúc này; không có site (backup của
	// site đã xoá, hay domain lạ) thì admin. Ghi vào dòng backup vì backup sống
	// lâu hơn site.
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO backups ("+backupInsertColumns+", owner) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT owner FROM sites WHERE domain = ?), 'admin'))",
		b.ID, b.Domain, b.Kind, b.Reason, b.Path, b.Size, b.SHA256,
		b.WPVersion, b.PHPVersion, b.JobID, fmtTime(b.Created), b.Domain)
	if err != nil {
		return Backup{}, fmt.Errorf("thêm backup %s của %s: %w", b.ID, b.Domain, err)
	}
	// M29: chép loại site và các tên phụ đang có vào dòng backup (backup sống
	// lâu hơn site). Site không còn trong bảng thì để rỗng.
	if _, err := s.db.ExecContext(ctx, `UPDATE backups SET
		app = COALESCE((SELECT app FROM sites WHERE domain = ?1), ''),
		names = COALESCE((SELECT group_concat(name || ':' || mode, ' ') FROM
			(SELECT name, mode FROM site_names WHERE site = ?1 AND kind = 'alias' AND state = 'active' ORDER BY name)), ''),
		rules = COALESCE((SELECT json_group_array(json_object('source', source, 'target', target, 'code', code, 'keep_query', keep_query)) FROM
			(SELECT source, target, code, keep_query FROM site_redirects WHERE site = ?1 AND state = 'active' ORDER BY id)), '')
		WHERE id = ?2`, b.Domain, b.ID); err != nil {
		return Backup{}, fmt.Errorf("ghi loại site và tên phụ vào backup %s: %w", b.ID, err)
	}
	// Đọc lại từ DB để thời gian đã qua đúng vòng ghi/đọc như mọi lần sau.
	out, _, err := s.Backup(ctx, b.ID)
	return out, err
}

// Backup đọc một dòng theo id; ok=false nếu không có (không phải lỗi).
func (s *Store) Backup(ctx context.Context, id string) (b Backup, ok bool, err error) {
	b, err = backupScan(s.db.QueryRowContext(ctx, "SELECT "+backupColumns+" FROM backups WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Backup{}, false, nil
	}
	if err != nil {
		return Backup{}, false, fmt.Errorf("đọc backup %s: %w", id, err)
	}
	return b, true, nil
}

// Backups trả các dòng của domain (rỗng: mọi domain), mới nhất trước. rowid là
// khoá phụ: hai bản cùng created_at thì bản chèn sau xếp trước.
func (s *Store) Backups(ctx context.Context, domain string) ([]Backup, error) {
	return s.BackupsOf(ctx, "", domain)
}

// BackupsOf là Backups chỉ lấy dòng có owner khớp (rỗng là mọi chủ). Lọc theo
// cột owner của dòng backup, không join sites: backup của site đã xoá hẳn vẫn
// thuộc về chủ cũ.
func (s *Store) BackupsOf(ctx context.Context, owner, domain string) ([]Backup, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+backupColumns+` FROM backups WHERE (? = '' OR owner = ?) AND (? = '' OR domain = ?)
		 ORDER BY created_at DESC, rowid DESC`, owner, owner, domain, domain)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách backup của %q (chủ %q): %w", domain, owner, err)
	}
	defer func() { _ = rows.Close() }()
	out := []Backup{}
	for rows.Next() {
		b, err := backupScan(rows)
		if err != nil {
			return nil, fmt.Errorf("đọc danh sách backup của %q: %w", domain, err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc danh sách backup của %q: %w", domain, err)
	}
	return out, nil
}

// BackupOwner trả chủ của backup (luật owner:backup của server); ok=false khi không có.
func (s *Store) BackupOwner(ctx context.Context, id string) (owner string, ok bool, err error) {
	return s.ownerOf(ctx, "SELECT owner FROM backups WHERE id = ?", "backup "+id, id)
}

// DeleteBackup xoá dòng; không có thì không lỗi (dọn bản cũ chạy lại được).
// Chỉ xoá dòng, không đụng file: việc xoá file là của người gọi.
func (s *Store) DeleteBackup(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM backups WHERE id = ?", id); err != nil {
		return fmt.Errorf("xoá backup %s: %w", id, err)
	}
	return nil
}

// ForgetBackupLocal ghi nhận file của dòng đã bị dọn khỏi máy (M18, mục 3.2
// và 3.7 của docs/thay-doi/M18-thiet-ke.md): dòng đã lên kho (remote='stored')
// thì giữ lại thành local=0 (bản chỉ còn trên kho); không thì xoá dòng như M9.
// Một transaction cho cả hai câu: dọn trên kho chạy cùng lúc ở tiến trình khác
// (ForgetBackupRemote) không bao giờ để lại dòng local=0 mà không ở trên kho.
// deleted cho biết dòng đã bị xoá; dòng không có thì không lỗi.
func (s *Store) ForgetBackupLocal(ctx context.Context, id string) (deleted bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi máy: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }() // không làm gì nếu đã Commit
	res, err := tx.ExecContext(ctx, "DELETE FROM backups WHERE id = ? AND remote <> 'stored'", id)
	if err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi máy: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi máy: %w", id, err)
	}
	if n == 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE backups SET local = 0 WHERE id = ? AND remote = 'stored'", id); err != nil {
			return false, fmt.Errorf("dọn backup %s khỏi máy: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi máy: %w", id, err)
	}
	return n > 0, nil
}

// SnapshotDB chép DB đang mở ra path bằng VACUUM INTO: bản sao nhất quán dù
// đang có người ghi, và gọn (không kèm file -wal). Chép file .db trần bằng cp
// khi DB ở chế độ WAL có thể ra bản hỏng vì dữ liệu mới còn nằm trong -wal.
//
// path phải tuyệt đối và chưa tồn tại. File được tạo trước bằng O_EXCL với quyền
// 0600: vừa chặn ghi đè (kể cả hai lệnh chạy cùng lúc), vừa không có khoảng
// thời gian file mới nằm với quyền 0644 theo umask. SQLite chấp nhận đích là
// file rỗng có sẵn. path đi vào câu SQL bằng tham số ?, nên dấu nháy hay ký
// tự lạ trong tên không thể đổi nghĩa câu lệnh.
func (s *Store) SnapshotDB(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("sao tpan.db: đường dẫn đích phải tuyệt đối, nhận %q", path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("sao tpan.db: tạo file bản sao %s (đã tồn tại hoặc thiếu thư mục cha?): %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("sao tpan.db: đóng file bản sao %s: %w", path, err)
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		// Không để lại file rỗng hay nửa vời: lần sau người gọi sẽ tưởng bản sao đã có.
		_ = os.Remove(path)
		return fmt.Errorf("sao tpan.db: VACUUM INTO %s: %w", path, err)
	}
	return nil
}

// CronRun là site.CronRun (lần chạy gần nhất của cron WordPress một site).
type CronRun = site.CronRun

// SaveCronRun ghi đè dòng của domain (mỗi site chỉ giữ lần chạy gần nhất).
func (s *Store) SaveCronRun(ctx context.Context, r CronRun) error {
	if r.Domain == "" || r.Status == "" {
		return fmt.Errorf("ghi lần chạy cron: thiếu domain hoặc status (domain=%q status=%q)", r.Domain, r.Status)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_runs (domain, started_at, duration_ms, status, exit_code, output)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (domain) DO UPDATE SET started_at = excluded.started_at,
		   duration_ms = excluded.duration_ms, status = excluded.status,
		   exit_code = excluded.exit_code, output = excluded.output`,
		r.Domain, fmtTime(r.Started), r.Duration.Milliseconds(), r.Status, r.ExitCode, r.Output)
	if err != nil {
		return fmt.Errorf("ghi lần chạy cron của %s: %w", r.Domain, err)
	}
	return nil
}

// CronRuns trả mọi dòng, sắp theo domain; luôn là slice không nil.
func (s *Store) CronRuns(ctx context.Context) ([]CronRun, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT domain, started_at, duration_ms, status, exit_code, output FROM cron_runs ORDER BY domain")
	if err != nil {
		return nil, fmt.Errorf("đọc lần chạy cron: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []CronRun{}
	for rows.Next() {
		var (
			r       CronRun
			started string
			ms      int64
		)
		if err := rows.Scan(&r.Domain, &started, &ms, &r.Status, &r.ExitCode, &r.Output); err != nil {
			return nil, fmt.Errorf("đọc lần chạy cron: %w", err)
		}
		if r.Started, err = parseTime(started); err != nil {
			return nil, fmt.Errorf("lần chạy cron của %s: %w", r.Domain, err)
		}
		r.Duration = time.Duration(ms) * time.Millisecond
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc lần chạy cron: %w", err)
	}
	return out, nil
}

// BackupAlias là một tên phụ site có lúc backup.
type BackupAlias struct {
	Name string
	Mode string // "serve" | "redirect"
}

// BackupRule là một luật chuyển hướng site có lúc backup.
type BackupRule struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Code      int    `json:"code"`
	KeepQuery int    `json:"keep_query"`
}

// BackupOrigin là những gì dòng backup nhớ về site của nó lúc tạo bản (M29),
// để "tạo lại site rồi khôi phục" dựng lại đúng site cũ.
type BackupOrigin struct {
	// App: "wordpress" | "php"; rỗng là dòng có trước M29 (tức WordPress, và
	// không ghi tên phụ hay luật nào).
	App     string
	Aliases []BackupAlias
	Rules   []BackupRule
}

// BackupOrigin đọc phần đó của dòng backup id. ok=false khi không có dòng.
func (s *Store) BackupOrigin(ctx context.Context, id string) (o BackupOrigin, ok bool, err error) {
	var names, rules string
	err = s.db.QueryRowContext(ctx, "SELECT app, names, rules FROM backups WHERE id = ?", id).Scan(&o.App, &names, &rules)
	if errors.Is(err, sql.ErrNoRows) {
		return BackupOrigin{}, false, nil
	}
	if err != nil {
		return BackupOrigin{}, false, fmt.Errorf("đọc loại site của backup %s: %w", id, err)
	}
	for _, f := range strings.Fields(names) {
		if n, m, found := strings.Cut(f, ":"); found {
			o.Aliases = append(o.Aliases, BackupAlias{Name: n, Mode: m})
		}
	}
	if rules != "" {
		if err := json.Unmarshal([]byte(rules), &o.Rules); err != nil {
			return BackupOrigin{}, false, fmt.Errorf("đọc luật chuyển hướng của backup %s: %w", id, err)
		}
	}
	return o, true, nil
}
