package store

// Truy vấn cho bảng databases: DB rời của user (migration 1300; mục 6.12 của
// docs/KE-HOACH.md). Các method ở đây cài đặt core.DBStore; store không import
// core nên khẳng định "đúng chữ ký" nằm trong databases_test.go.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/account"
)

const dbColumns = "name, owner, charset, collation, note, status, suspended, suspended_at, created_at, trashed_at, purge_at"

func scanStandaloneDB(r rowScanner) (account.Database, error) {
	var (
		d                        account.Database
		status, susp, created    string
		suspAt, trashed, purgeAt sql.NullString
	)
	if err := r.Scan(&d.Name, &d.Owner, &d.Charset, &d.Collation, &d.Note, &status, &susp, &suspAt, &created, &trashed, &purgeAt); err != nil {
		return account.Database{}, err
	}
	d.Status, d.Suspended = account.DBStatus(status), account.Suspension(susp)
	var err error
	if d.Created, err = parseTime(created); err != nil {
		return account.Database{}, fmt.Errorf("DB %s: %w", d.Name, err)
	}
	for _, f := range []struct {
		in  sql.NullString
		out *time.Time
	}{{suspAt, &d.SuspendedAt}, {trashed, &d.TrashedAt}, {purgeAt, &d.PurgeAt}} {
		if !f.in.Valid {
			continue
		}
		if *f.out, err = parseTime(f.in.String); err != nil {
			return account.Database{}, fmt.Errorf("DB %s: %w", d.Name, err)
		}
	}
	return d, nil
}

// StandaloneDB trả DB rời theo tên; ok=false nếu không có. DB đang ở thùng rác vẫn được trả.
func (s *Store) StandaloneDB(ctx context.Context, name string) (d account.Database, ok bool, err error) {
	d, err = scanStandaloneDB(s.db.QueryRowContext(ctx, "SELECT "+dbColumns+" FROM databases WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return account.Database{}, false, nil
	}
	if err != nil {
		return account.Database{}, false, fmt.Errorf("đọc DB %s: %w", name, err)
	}
	return d, true, nil
}

// StandaloneDBs trả DB rời của owner (rỗng là của mọi người), xếp theo tên;
// luôn là slice không nil.
func (s *Store) StandaloneDBs(ctx context.Context, owner string) ([]account.Database, error) {
	out := []account.Database{}
	err := queryEach(ctx, s.db,
		"SELECT "+dbColumns+" FROM databases WHERE (? = '' OR owner = ?) ORDER BY name",
		func(r rowScanner) error {
			d, err := scanStandaloneDB(r)
			if err != nil {
				return err
			}
			out = append(out, d)
			return nil
		}, owner, owner)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách DB của %q: %w", owner, err)
	}
	return out, nil
}

// SaveStandaloneDB thêm hoặc ghi đè cả dòng DB rời cùng tên (người gọi đọc ra,
// sửa, rồi lưu lại như SaveSite). Status rỗng là active; Created zero là bây giờ.
func (s *Store) SaveStandaloneDB(ctx context.Context, d account.Database) error {
	if d.Name == "" || d.Owner == "" {
		return fmt.Errorf("lưu DB %q: thiếu tên hoặc chủ (owner=%q)", d.Name, d.Owner)
	}
	if d.Status == "" {
		d.Status = account.DBActive
	}
	if d.Created.IsZero() {
		d.Created = s.now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO databases (name, owner, charset, collation, note, status, suspended, suspended_at,
		                        created_at, trashed_at, purge_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (name) DO UPDATE SET
		   owner = excluded.owner, charset = excluded.charset, collation = excluded.collation,
		   note = excluded.note, status = excluded.status, suspended = excluded.suspended,
		   suspended_at = excluded.suspended_at, created_at = excluded.created_at,
		   trashed_at = excluded.trashed_at, purge_at = excluded.purge_at`,
		d.Name, d.Owner, d.Charset, d.Collation, d.Note, string(d.Status), string(d.Suspended),
		nullTime(d.SuspendedAt), fmtTime(d.Created), nullTime(d.TrashedAt), nullTime(d.PurgeAt))
	if err != nil {
		return fmt.Errorf("lưu DB %s: %w", d.Name, err)
	}
	return nil
}

// DeleteStandaloneDB xoá dòng DB rời; không có thì ErrNotFound (như DeleteSite).
func (s *Store) DeleteStandaloneDB(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM databases WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("xoá DB %s: %w", name, err)
	}
	return needRow(res, "DB "+name, "xoá DB "+name)
}

// DBOwner trả chủ của DB rời (luật owner:db của server); ok=false khi không có.
func (s *Store) DBOwner(ctx context.Context, name string) (owner string, ok bool, err error) {
	return s.ownerOf(ctx, "SELECT owner FROM databases WHERE name = ?", "DB "+name, name)
}

// ownerOf chạy một câu SELECT owner ... trả đúng một cột; không có dòng thì ok=false.
func (s *Store) ownerOf(ctx context.Context, query, what string, arg any) (owner string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, query, arg).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc chủ của %s: %w", what, err)
	}
	return owner, true, nil
}
