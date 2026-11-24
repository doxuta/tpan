package store

// Truy vấn cho bảng sites (mục 4.4 của docs/KE-HOACH.md). Các method ở đây
// cài đặt giao diện core.SiteStore; store không import core nên khẳng định
// "đúng chữ ký" nằm trong sites_test.go.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

const siteColumns = "domain, status, php_version, ssl, created_at, trashed_at, purge_at, owner, suspended, suspended_at, db_suspended, htaccess, caged"

// siteScan đọc một dòng sites. Dữ liệu DB cũng không tin mù: domain đi lại
// qua validate.ParseDomain, slug suy lại từ domain (cột slug chỉ để DB chặn
// trùng, không phải nguồn sự thật).
func siteScan(r rowScanner) (site.Site, error) {
	var (
		domain, status, php, created string
		owner, susp, dbSusp, htacc   string
		ssl, caged                   int
		trashed, purge, suspAt       sql.NullString
	)
	if err := r.Scan(&domain, &status, &php, &ssl, &created, &trashed, &purge, &owner, &susp, &suspAt, &dbSusp, &htacc, &caged); err != nil {
		return site.Site{}, err
	}
	d, err := validate.ParseDomain(domain)
	if err != nil {
		return site.Site{}, fmt.Errorf("domain %q trong DB không hợp lệ: %w", domain, err)
	}
	s := site.Site{
		Domain:      d,
		Slug:        validate.SlugOf(d),
		Status:      site.Status(status),
		PHPVersion:  php,
		SSL:         ssl != 0,
		Owner:       owner,
		Suspended:   account.Suspension(susp),
		DBSuspended: account.Suspension(dbSusp),
		Htaccess:    site.HtaccessMode(htacc),
		Caged:       caged != 0,
	}
	if suspAt.Valid {
		if s.SuspendedAt, err = parseTime(suspAt.String); err != nil {
			return site.Site{}, fmt.Errorf("site %s: %w", d, err)
		}
	}
	if s.Created, err = parseTime(created); err != nil {
		return site.Site{}, fmt.Errorf("site %s: %w", d, err)
	}
	if trashed.Valid {
		if s.TrashedAt, err = parseTime(trashed.String); err != nil {
			return site.Site{}, fmt.Errorf("site %s: %w", d, err)
		}
	}
	if purge.Valid {
		if s.PurgeAt, err = parseTime(purge.String); err != nil {
			return site.Site{}, fmt.Errorf("site %s: %w", d, err)
		}
	}
	return s, nil
}

// Site trả site theo domain; ok=false nếu không có. Site ở thùng rác vẫn được
// trả, người gọi tự xem Status.
func (s *Store) Site(ctx context.Context, d validate.Domain) (site.Site, bool, error) {
	st, err := siteScan(s.db.QueryRowContext(ctx,
		"SELECT "+siteColumns+" FROM sites WHERE domain = ?", d.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return site.Site{}, false, nil
	}
	if err != nil {
		return site.Site{}, false, fmt.Errorf("đọc site %s: %w", d, err)
	}
	return st, true, nil
}

// Sites trả mọi site, xếp theo domain.
func (s *Store) Sites(ctx context.Context) ([]site.Site, error) {
	return s.SitesOf(ctx, "")
}

// SitesOf trả site của owner (rỗng là của mọi người), xếp theo domain. Lọc ngay
// trong SQL: user thường không bao giờ nhận dữ liệu site của người khác ra tới
// tầng server.
func (s *Store) SitesOf(ctx context.Context, owner string) ([]site.Site, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+siteColumns+" FROM sites WHERE (? = '' OR owner = ?) ORDER BY domain", owner, owner)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site của %q: %w", owner, err)
	}
	defer func() { _ = rows.Close() }()
	out := []site.Site{}
	for rows.Next() {
		st, err := siteScan(rows)
		if err != nil {
			return nil, fmt.Errorf("đọc dòng site: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc danh sách site của %q: %w", owner, err)
	}
	return out, nil
}

// SiteOwner trả chủ của site theo domain (luật owner:site của server); ok=false
// khi không có site đó. Site ở thùng rác vẫn có chủ.
func (s *Store) SiteOwner(ctx context.Context, domain string) (owner string, ok bool, err error) {
	return s.ownerOf(ctx, "SELECT owner FROM sites WHERE domain = ?", "site "+domain, domain)
}

// SaveSite thêm mới hoặc ghi đè site cùng domain. Slug luôn ghi là
// validate.SlugOf(Domain): trường Slug người gọi đưa vào bị bỏ qua, để tên
// user Linux và DB không bao giờ lệch quy tắc đặt tên.
func (s *Store) SaveSite(ctx context.Context, st site.Site) error {
	if st.Domain.IsZero() {
		return errors.New("lưu site: thiếu domain")
	}
	// Ghi đè là ghi đè cả dòng: người gọi đọc Site ra, sửa, rồi lưu lại nên
	// Created đã mang giá trị cũ. Owner rỗng (mã có từ trước M13 dựng Site
	// không có chủ) thì là admin, như mọi site cũ.
	owner := st.Owner
	if owner == "" {
		owner = adminName
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sites (domain, slug, status, php_version, ssl, created_at, trashed_at, purge_at,
		                    owner, suspended, suspended_at, db_suspended, htaccess, caged)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (domain) DO UPDATE SET
		   slug = excluded.slug, status = excluded.status, php_version = excluded.php_version,
		   ssl = excluded.ssl, created_at = excluded.created_at, trashed_at = excluded.trashed_at,
		   purge_at = excluded.purge_at, owner = excluded.owner, suspended = excluded.suspended,
		   suspended_at = excluded.suspended_at, db_suspended = excluded.db_suspended,
		   htaccess = excluded.htaccess, caged = excluded.caged`,
		st.Domain.String(), validate.SlugOf(st.Domain).String(), string(st.Status), st.PHPVersion,
		siteBool(st.SSL), fmtTime(st.Created), nullTime(st.TrashedAt), nullTime(st.PurgeAt),
		owner, string(st.Suspended), nullTime(st.SuspendedAt), string(st.DBSuspended),
		string(st.Htaccess.OrDefault()), siteBool(st.Caged))
	if err != nil {
		return fmt.Errorf("lưu site %s: %w", st.Domain, err)
	}
	return nil
}

func siteBool(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeleteSite xoá dòng của site; không có thì lỗi thoả errors.Is(err, ErrNotFound).
func (s *Store) DeleteSite(ctx context.Context, d validate.Domain) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sites WHERE domain = ?", d.String())
	if err != nil {
		return fmt.Errorf("xoá site %s: %w", d, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("xoá site %s: %w", d, err)
	}
	if n == 0 {
		return fmt.Errorf("site %s: %w", d, ErrNotFound)
	}
	return nil
}
