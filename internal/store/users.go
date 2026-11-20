package store

// Truy vấn cho bảng users và packages (migration 1300; mục 6.12 của
// docs/KE-HOACH.md). Các method ở đây cài đặt core.AccountStore; store không
// import core nên khẳng định "đúng chữ ký" nằm trong users_test.go.
//
// Hash mật khẩu không bao giờ nằm trong account.User: chỉ đi qua CreateUser,
// SetUserPassword, PasswordHashOf, ChangeUserPassword (auth.go).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/account"
)

const userColumns = "name, full_name, email, role, package, status, created_at, suspended_at, suspend_reason"

func scanUser(r rowScanner) (account.User, error) {
	var (
		u                     account.User
		role, status, created string
		suspendedAt           sql.NullString
	)
	if err := r.Scan(&u.Name, &u.FullName, &u.Email, &role, &u.Package, &status, &created, &suspendedAt, &u.SuspendReason); err != nil {
		return account.User{}, err
	}
	u.Role, u.Status = account.Role(role), account.Status(status)
	var err error
	if u.Created, err = parseTime(created); err != nil {
		return account.User{}, fmt.Errorf("user %s: %w", u.Name, err)
	}
	if suspendedAt.Valid {
		if u.SuspendedAt, err = parseTime(suspendedAt.String); err != nil {
			return account.User{}, fmt.Errorf("user %s: %w", u.Name, err)
		}
	}
	return u, nil
}

// User trả user theo tên; ok=false nếu không có.
func (s *Store) User(ctx context.Context, name string) (u account.User, ok bool, err error) {
	u, err = scanUser(s.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return account.User{}, false, nil
	}
	if err != nil {
		return account.User{}, false, fmt.Errorf("đọc user %s: %w", name, err)
	}
	return u, true, nil
}

// Users trả mọi user, xếp theo tên; luôn là slice không nil.
func (s *Store) Users(ctx context.Context) ([]account.User, error) {
	out := []account.User{}
	err := queryEach(ctx, s.db, "SELECT "+userColumns+" FROM users ORDER BY name", func(r rowScanner) error {
		u, err := scanUser(r)
		if err != nil {
			return err
		}
		out = append(out, u)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách user: %w", err)
	}
	return out, nil
}

// CreateUser thêm user mới. Trùng tên là lỗi (PRIMARY KEY); gói không có cũng
// lỗi (khoá ngoại). Status rỗng là active, Package rỗng là gói mặc định,
// Created zero là bây giờ.
func (s *Store) CreateUser(ctx context.Context, u account.User, passwordHash string) error {
	if u.Name == "" || passwordHash == "" {
		return fmt.Errorf("tạo user %q: thiếu tên hoặc hash mật khẩu", u.Name)
	}
	if u.Status == "" {
		u.Status = account.Active
	}
	if u.Package == "" {
		u.Package = account.DefaultPackage
	}
	if u.Created.IsZero() {
		u.Created = s.now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (name, full_name, email, role, package, status, password_hash, created_at, suspended_at, suspend_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Name, u.FullName, u.Email, string(u.Role), u.Package, string(u.Status), passwordHash,
		fmtTime(u.Created), nullTime(u.SuspendedAt), u.SuspendReason)
	if err != nil {
		return fmt.Errorf("tạo user %s: %w", u.Name, err)
	}
	return nil
}

// UpdateUser ghi đè full_name, email, role, package, status, suspended_at,
// suspend_reason; không đụng hash, phiên, created_at. Không có user thì ErrNotFound.
func (s *Store) UpdateUser(ctx context.Context, u account.User) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET full_name = ?, email = ?, role = ?, package = ?, status = ?,
		   suspended_at = ?, suspend_reason = ? WHERE name = ?`,
		u.FullName, u.Email, string(u.Role), u.Package, string(u.Status),
		nullTime(u.SuspendedAt), u.SuspendReason, u.Name)
	if err != nil {
		return fmt.Errorf("sửa user %s: %w", u.Name, err)
	}
	return needRow(res, "user "+u.Name, "sửa user "+u.Name)
}

// needRow trả ErrNotFound (kèm what) khi câu lệnh không chạm dòng nào.
func needRow(res sql.Result, what, ctxMsg string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", ctxMsg, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return nil
}

// SetUserPassword đổi hash và huỷ mọi phiên của user (kể cả phiên admin đang
// "xem như" họ) trong MỘT transaction: không bao giờ có lúc mật khẩu đã đổi
// mà phiên cũ còn sống. Không có user thì ErrNotFound và không đổi gì.
func (s *Store) SetUserPassword(ctx context.Context, name, passwordHash string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE name = ?", passwordHash, name)
		if err != nil {
			return fmt.Errorf("đổi mật khẩu của %s: %w", name, err)
		}
		if err := needRow(res, "user "+name, "đổi mật khẩu của "+name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, deleteSessionsOfSQL, name, name); err != nil {
			return fmt.Errorf("đổi mật khẩu của %s: huỷ phiên: %w", name, err)
		}
		return nil
	})
}

const deleteSessionsOfSQL = "DELETE FROM sessions WHERE user_name = ? OR view_as = ?"

// DeleteSessionsOf huỷ mọi phiên của user, kể cả phiên admin đang "xem như" họ.
func (s *Store) DeleteSessionsOf(ctx context.Context, name string) error {
	if _, err := s.db.ExecContext(ctx, deleteSessionsOfSQL, name, name); err != nil {
		return fmt.Errorf("huỷ phiên của %s: %w", name, err)
	}
	return nil
}

// DeleteUser xoá dòng user và phiên của họ trong một transaction. Job, nhật ký
// cũ giữ nguyên tên (không có khoá ngoại). Không có user thì ErrNotFound.
func (s *Store) DeleteUser(ctx context.Context, name string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, deleteSessionsOfSQL, name, name); err != nil {
			return fmt.Errorf("xoá user %s: huỷ phiên: %w", name, err)
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM users WHERE name = ?", name)
		if err != nil {
			return fmt.Errorf("xoá user %s: %w", name, err)
		}
		return needRow(res, "user "+name, "xoá user "+name)
	})
}

// RunningJobsOf đếm job đang chạy có owner hoặc actor là name: xoá hay khoá
// user lúc họ còn job chạy sẽ bỏ lại việc nửa vời.
func (s *Store) RunningJobsOf(ctx context.Context, name string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM jobs WHERE status = 'running' AND (owner = ? OR actor = ?)", name, name).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("đếm job đang chạy của %s: %w", name, err)
	}
	return n, nil
}

// ---- Gói ----

const packageColumns = "name, max_sites, max_dbs, disk_mb, max_backups, created_at"

func scanPackage(r rowScanner) (account.Package, error) {
	var (
		p       account.Package
		created string
	)
	if err := r.Scan(&p.Name, &p.MaxSites, &p.MaxDBs, &p.DiskMB, &p.MaxBackups, &created); err != nil {
		return account.Package{}, err
	}
	var err error
	if p.Created, err = parseTime(created); err != nil {
		return account.Package{}, fmt.Errorf("gói %s: %w", p.Name, err)
	}
	return p, nil
}

// Package trả gói theo tên; ok=false nếu không có.
func (s *Store) Package(ctx context.Context, name string) (p account.Package, ok bool, err error) {
	p, err = scanPackage(s.db.QueryRowContext(ctx, "SELECT "+packageColumns+" FROM packages WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return account.Package{}, false, nil
	}
	if err != nil {
		return account.Package{}, false, fmt.Errorf("đọc gói %s: %w", name, err)
	}
	return p, true, nil
}

// Packages trả mọi gói, xếp theo tên; luôn là slice không nil.
func (s *Store) Packages(ctx context.Context) ([]account.Package, error) {
	out := []account.Package{}
	err := queryEach(ctx, s.db, "SELECT "+packageColumns+" FROM packages ORDER BY name", func(r rowScanner) error {
		p, err := scanPackage(r)
		if err != nil {
			return err
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách gói: %w", err)
	}
	return out, nil
}

// SavePackage thêm gói mới hoặc ghi đè bốn giới hạn của gói cùng tên. Gói đã có
// giữ nguyên ngày tạo (cột created_at không bị ghi đè); gói mới có Created zero
// thì lấy bây giờ.
func (s *Store) SavePackage(ctx context.Context, p account.Package) error {
	if p.Name == "" {
		return errors.New("lưu gói: thiếu tên")
	}
	if p.Created.IsZero() {
		p.Created = s.now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO packages (name, max_sites, max_dbs, disk_mb, max_backups, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (name) DO UPDATE SET max_sites = excluded.max_sites, max_dbs = excluded.max_dbs,
		   disk_mb = excluded.disk_mb, max_backups = excluded.max_backups`,
		p.Name, p.MaxSites, p.MaxDBs, p.DiskMB, p.MaxBackups, fmtTime(p.Created))
	if err != nil {
		return fmt.Errorf("lưu gói %s: %w", p.Name, err)
	}
	return nil
}

// DeletePackage xoá gói; không có thì ErrNotFound. Gói còn user dùng thì lỗi
// khoá ngoại: core đã kiểm PackageUsers trước (package_in_use), đây là lưới đỡ.
func (s *Store) DeletePackage(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM packages WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("xoá gói %s: %w", name, err)
	}
	return needRow(res, "gói "+name, "xoá gói "+name)
}

// PackageUsers đếm user đang dùng gói.
func (s *Store) PackageUsers(ctx context.Context, name string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE package = ?", name).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm user của gói %s: %w", name, err)
	}
	return n, nil
}

// ---- Dùng bao nhiêu ----

// Usage đếm site (mọi trạng thái, kể cả thùng rác: nó còn giữ DB và file) và
// DB rời (mọi trạng thái) của user, kèm lần đo dung lượng gần nhất. User không
// có thì ErrNotFound.
func (s *Store) Usage(ctx context.Context, name string) (account.Usage, error) {
	var (
		u        account.Usage
		measured sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM sites WHERE owner = u.name),
		        (SELECT COUNT(*) FROM databases WHERE owner = u.name),
		        u.disk_bytes, u.disk_measured_at
		 FROM users u WHERE u.name = ?`, name).Scan(&u.Sites, &u.DBs, &u.DiskBytes, &measured)
	if errors.Is(err, sql.ErrNoRows) {
		return account.Usage{}, fmt.Errorf("user %s: %w", name, ErrNotFound)
	}
	if err != nil {
		return account.Usage{}, fmt.Errorf("đọc mức dùng của %s: %w", name, err)
	}
	if measured.Valid {
		if u.DiskMeasured, err = parseTime(measured.String); err != nil {
			return account.Usage{}, fmt.Errorf("mức dùng của %s: %w", name, err)
		}
	}
	return u, nil
}

// SetDiskUsage ghi lần đo dung lượng của user (byte). Không có user thì ErrNotFound.
func (s *Store) SetDiskUsage(ctx context.Context, name string, bytes int64, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE users SET disk_bytes = ?, disk_measured_at = ? WHERE name = ?", bytes, fmtTime(at), name)
	if err != nil {
		return fmt.Errorf("ghi dung lượng của %s: %w", name, err)
	}
	return needRow(res, "user "+name, "ghi dung lượng của "+name)
}
