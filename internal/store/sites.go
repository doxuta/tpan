package store

// Truy vấn cho bảng sites (mục 4.4 của docs/KE-HOACH.md). Các method ở đây
// cài đặt giao diện core.SiteStore; store không import core nên khẳng định
// "đúng chữ ký" nằm trong sites_test.go.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

const siteColumns = "domain, status, php_version, ssl, created_at, trashed_at"

// siteScan đọc một dòng sites. Dữ liệu DB cũng không tin mù: domain đi lại
// qua validate.ParseDomain, slug suy lại từ domain (cột slug chỉ để DB chặn
// trùng, không phải nguồn sự thật).
func siteScan(r rowScanner) (site.Site, error) {
	var (
		domain, status, php, created string
		ssl                          int
		trashed                      sql.NullString
	)
	if err := r.Scan(&domain, &status, &php, &ssl, &created, &trashed); err != nil {
		return site.Site{}, err
	}
	d, err := validate.ParseDomain(domain)
	if err != nil {
		return site.Site{}, fmt.Errorf("domain %q trong DB không hợp lệ: %w", domain, err)
	}
	s := site.Site{
		Domain:     d,
		Slug:       validate.SlugOf(d),
		Status:     site.Status(status),
		PHPVersion: php,
		SSL:        ssl != 0,
	}
	if s.Created, err = parseTime(created); err != nil {
		return site.Site{}, fmt.Errorf("site %s: %w", d, err)
	}
	if trashed.Valid {
		if s.TrashedAt, err = parseTime(trashed.String); err != nil {
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
	rows, err := s.db.QueryContext(ctx, "SELECT "+siteColumns+" FROM sites ORDER BY domain")
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
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
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	return out, nil
}

// SaveSite thêm mới hoặc ghi đè site cùng domain. Slug luôn ghi là
// validate.SlugOf(Domain): trường Slug người gọi đưa vào bị bỏ qua, để tên
// user Linux và DB không bao giờ lệch quy tắc đặt tên.
func (s *Store) SaveSite(ctx context.Context, st site.Site) error {
	if st.Domain.IsZero() {
		return errors.New("lưu site: thiếu domain")
	}
	// Ghi đè là ghi đè cả dòng: người gọi đọc Site ra, sửa, rồi lưu lại nên
	// Created đã mang giá trị cũ.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sites (domain, slug, status, php_version, ssl, created_at, trashed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (domain) DO UPDATE SET
		   slug = excluded.slug, status = excluded.status, php_version = excluded.php_version,
		   ssl = excluded.ssl, created_at = excluded.created_at, trashed_at = excluded.trashed_at`,
		st.Domain.String(), validate.SlugOf(st.Domain).String(), string(st.Status), st.PHPVersion,
		siteBool(st.SSL), fmtTime(st.Created), nullTime(st.TrashedAt))
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
