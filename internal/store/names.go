package store

// Tên của site: bảng site_names (M19, docs/thay-doi/M19-thiet-ke.md mục 3.1,
// 3.2). Luật ai được giữ tên nào nằm ở site.CheckClaims (thuần); ở đây là
// ảnh chụp để kiểm và transaction để kiểm rồi ghi không bị đua.
//
// DB mở với _txlock=immediate (store.go): BEGIN lấy quyền ghi ngay, nên "đọc
// ảnh chụp, kiểm, chèn" trong một transaction là tuần tự trên toàn máy, kể cả
// giữa CLI (root) và tpan serve.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// pendingTTL: dòng pending cũ hơn chừng này là của job đã chết giữa chừng
// (tiến trình bị giết, máy khởi động lại); lần giữ chỗ sau bỏ nó (S9).
const pendingTTL = time.Hour

// nameSnapshot là mọi dòng site_names hợp với một dòng tên chính suy từ mỗi
// dòng sites chưa có dòng của mình. Nhờ vậy một đường tạo dòng sites mà quên
// site_names (mã cũ, khôi phục) vẫn không để tên đó bị người khác giữ (B2).
const nameSnapshot = `SELECT name, site, owner, kind, mode, state, in_cert, created_at FROM site_names
UNION ALL
SELECT domain, domain, owner, 'primary', 'serve', 'active', ssl, created_at FROM sites
 WHERE NOT EXISTS (SELECT 1 FROM site_names WHERE site_names.name = sites.domain)`

// queryNames đọc ảnh chụp, lọc bằng tail (WHERE/ORDER BY với tham số ?).
func queryNames(ctx context.Context, q querier, tail string, args ...any) ([]site.Name, error) {
	rows, err := q.QueryContext(ctx, "SELECT name, site, owner, kind, mode, state, in_cert, created_at FROM ("+nameSnapshot+") "+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("đọc tên của site: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []site.Name
	for rows.Next() {
		var (
			name, siteD, owner, kind, mode, state, created string
			inCert                                         int
		)
		if err := rows.Scan(&name, &siteD, &owner, &kind, &mode, &state, &inCert, &created); err != nil {
			return nil, fmt.Errorf("đọc dòng tên: %w", err)
		}
		// Dữ liệu DB cũng không tin mù, như siteScan.
		n := site.Name{Owner: owner, Kind: site.NameKind(kind), Mode: site.NameMode(mode), State: site.NameState(state), InCert: inCert != 0}
		if n.Name, err = validate.ParseDomain(name); err != nil {
			return nil, fmt.Errorf("tên %q trong DB không hợp lệ: %w", name, err)
		}
		if n.Site, err = validate.ParseDomain(siteD); err != nil {
			return nil, fmt.Errorf("site %q của tên %s trong DB không hợp lệ: %w", siteD, name, err)
		}
		if n.Created, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("tên %s: %w", name, err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc tên của site: %w", err)
	}
	return out, nil
}

// withOwner: Owner rỗng là admin, như SaveSite.
func withOwner(cs []site.Claim) []site.Claim {
	out := slices.Clone(cs)
	for i := range out {
		if out[i].Owner == "" {
			out[i].Owner = adminName
		}
	}
	return out
}

// ClaimNames giữ chỗ (state pending) mọi tên trong cs trong MỘT transaction:
// bỏ dòng pending quá hạn, đọc ảnh chụp, site.CheckClaims, rồi ghi. Một tên
// lỗi thì không giữ tên nào; lỗi luật là *site.NameError (không bọc).
func (s *Store) ClaimNames(ctx context.Context, cs []site.Claim) error {
	cs = withOwner(cs)
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("giữ tên: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "DELETE FROM site_names WHERE state = 'pending' AND created_at < ?",
		fmtTime(now.Add(-pendingTTL))); err != nil {
		return fmt.Errorf("bỏ tên giữ chỗ quá hạn: %w", err)
	}
	existing, err := queryNames(ctx, tx, "")
	if err != nil {
		return err
	}
	if err := site.CheckClaims(cs, existing); err != nil {
		return err
	}
	for _, c := range cs {
		i := slices.IndexFunc(existing, func(n site.Name) bool { return n.Name == c.Name })
		switch {
		case i < 0:
			// Kiểm và ghi cùng transaction IMMEDIATE, nên xung đột còn lại chỉ
			// có thể là tên lặp lại trong chính cs: bỏ qua lần sau.
			_, err = tx.ExecContext(ctx,
				`INSERT INTO site_names (name, site, owner, kind, mode, state, in_cert, created_at)
				 VALUES (?, ?, ?, ?, ?, 'pending', 0, ?) ON CONFLICT (name) DO NOTHING`,
				c.Name.String(), c.Site.String(), c.Owner, string(c.Kind), string(modeOf(c)), fmtTime(now))
		case existing[i].State == site.StatePending:
			// Nhận lại dòng pending của chính site (CheckClaims đã xét cùng
			// site, cùng chủ, cùng kiểu): làm mới hạn để không bị coi là mồ côi.
			_, err = tx.ExecContext(ctx, "UPDATE site_names SET mode = ?, created_at = ? WHERE name = ?",
				string(modeOf(c)), fmtTime(now), c.Name.String())
		default:
			// Chạy lại tên chính của site đang có: không ghi gì.
			continue
		}
		if err != nil {
			return fmt.Errorf("giữ tên %s: %w", c.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("giữ tên: %w", err)
	}
	return nil
}

// modeOf: tên chính luôn serve (CHECK của bảng); tên phụ chưa đặt kiểu là serve.
func modeOf(c site.Claim) site.NameMode {
	if c.Kind == site.KindPrimary || c.Mode == "" {
		return site.ModeServe
	}
	return c.Mode
}

// CheckNames: cùng luật với ClaimNames, chỉ đọc (Prepare và ?dry_run=1). Dòng
// pending quá hạn được coi như không có, nhưng không xoá.
func (s *Store) CheckNames(ctx context.Context, cs []site.Claim) error {
	all, err := queryNames(ctx, s.db, "")
	if err != nil {
		return err
	}
	cutoff := s.now().Add(-pendingTTL)
	live := slices.DeleteFunc(all, func(n site.Name) bool {
		return n.State == site.StatePending && n.Created.Before(cutoff)
	})
	return site.CheckClaims(withOwner(cs), live)
}

// ActivateNames chuyển các dòng pending của site sang active. Tên đã active
// (chạy lại tên chính) không phải lỗi; tên không còn dòng nào của site này
// (bị bỏ vì quá hạn) thì lỗi ErrNotFound, để job không vẽ một tên không giữ.
func (s *Store) ActivateNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kích hoạt tên của %s: %w", siteD, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, n := range names {
		res, err := tx.ExecContext(ctx, "UPDATE site_names SET state = 'active' WHERE site = ? AND name = ?", siteD.String(), n.String())
		if err != nil {
			return fmt.Errorf("kích hoạt tên %s của %s: %w", n, siteD, err)
		}
		if k, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("kích hoạt tên %s của %s: %w", n, siteD, err)
		} else if k == 0 {
			return fmt.Errorf("kích hoạt tên %s của %s: %w", n, siteD, ErrNotFound)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("kích hoạt tên của %s: %w", siteD, err)
	}
	return nil
}

// ReleaseNames xoá các dòng PENDING của site (job hỏng). Không bao giờ xoá dòng active.
func (s *Store) ReleaseNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error {
	if len(names) == 0 {
		return nil
	}
	args := []any{siteD.String()}
	for _, n := range names {
		args = append(args, n.String())
	}
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM site_names WHERE state = 'pending' AND site = ? AND name IN ("+placeholders(len(names))+")",
		args...); err != nil {
		return fmt.Errorf("nhả tên của %s: %w", siteD, err)
	}
	return nil
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.Repeat("?, ", n-1) + "?"
}

// aliasRow sửa một tên phụ của site trong transaction: không có dòng thì
// ErrNotFound, tên chính thì lỗi (gỡ tên chính là xoá site).
func (s *Store) aliasRow(ctx context.Context, siteD, name validate.Domain, what, query string, args ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s %s của %s: %w", what, name, siteD, err)
	}
	defer func() { _ = tx.Rollback() }()
	var kind string
	err = tx.QueryRowContext(ctx, "SELECT kind FROM site_names WHERE site = ? AND name = ?", siteD.String(), name.String()).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s của %s: %w", what, name, siteD, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("%s %s của %s: %w", what, name, siteD, err)
	}
	if kind != string(site.KindAlias) {
		return fmt.Errorf("%s %s: đây là tên chính của site %s, không phải tên phụ", what, name, siteD)
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("%s %s của %s: %w", what, name, siteD, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s %s của %s: %w", what, name, siteD, err)
	}
	return nil
}

// DeleteAlias xoá một tên phụ (sau job gỡ thành công; nhận cả dòng pending
// mồ côi). Tên chính: lỗi. Không có: ErrNotFound.
func (s *Store) DeleteAlias(ctx context.Context, siteD, name validate.Domain) error {
	return s.aliasRow(ctx, siteD, name, "gỡ tên phụ",
		"DELETE FROM site_names WHERE site = ? AND name = ?", siteD.String(), name.String())
}

// SetAliasMode đổi kiểu của một tên phụ. Tên chính: lỗi. Không có: ErrNotFound.
func (s *Store) SetAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) error {
	if m != site.ModeServe && m != site.ModeRedirect {
		return fmt.Errorf("đổi kiểu tên phụ %s: kiểu %q không hợp lệ", name, m)
	}
	return s.aliasRow(ctx, siteD, name, "đổi kiểu tên phụ",
		"UPDATE site_names SET mode = ? WHERE site = ? AND name = ?", string(m), siteD.String(), name.String())
}

// SetInCert ghi cột in_cert của mọi tên của site: 1 cho tên có trong names, 0
// cho tên còn lại.
func (s *Store) SetInCert(ctx context.Context, siteD validate.Domain, names []validate.Domain) error {
	args := make([]any, 0, len(names)+1)
	for _, n := range names {
		args = append(args, n.String())
	}
	args = append(args, siteD.String())
	// SQLite nhận "IN ()" rỗng: mọi tên thành 0.
	if _, err := s.db.ExecContext(ctx,
		"UPDATE site_names SET in_cert = (name IN ("+placeholders(len(names))+")) WHERE site = ?", args...); err != nil {
		return fmt.Errorf("ghi tên trong chứng chỉ của %s: %w", siteD, err)
	}
	return nil
}

// NamesOf: mọi dòng của một site (cả pending), theo (created_at, name).
func (s *Store) NamesOf(ctx context.Context, siteD validate.Domain) ([]site.Name, error) {
	return queryNames(ctx, s.db, "WHERE site = ? ORDER BY created_at, name", siteD.String())
}

// NameSite tra một tên về dòng của nó (route explain, API ?host=).
func (s *Store) NameSite(ctx context.Context, name validate.Domain) (site.Name, bool, error) {
	ns, err := queryNames(ctx, s.db, "WHERE name = ?", name.String())
	if err != nil || len(ns) == 0 {
		return site.Name{}, false, err
	}
	return ns[0], true, nil
}

// activeNames đọc site.Names (chỉ dòng active) của các site của owner (rỗng là
// mọi site; siteD khác rỗng là đúng một site), theo (created_at, name). Site
// không có dòng nào thì không có khoá trong map, nên Names giữ giá trị zero.
// Rules (luật chuyển hướng) điền ở đợt 2.
func activeNames(ctx context.Context, q querier, siteD, owner string) (map[string]site.Names, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT name, site, kind, mode, in_cert FROM site_names
		 WHERE state = 'active' AND (? = '' OR site = ?)
		   AND (? = '' OR site IN (SELECT domain FROM sites WHERE owner = ?))
		 ORDER BY created_at, name`, siteD, siteD, owner, owner)
	if err != nil {
		return nil, fmt.Errorf("đọc tên của site: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]site.Names{}
	for rows.Next() {
		var (
			name, sd, kind, mode string
			inCert               int
		)
		if err := rows.Scan(&name, &sd, &kind, &mode, &inCert); err != nil {
			return nil, fmt.Errorf("đọc dòng tên: %w", err)
		}
		d, err := validate.ParseDomain(name)
		if err != nil {
			return nil, fmt.Errorf("tên %q trong DB không hợp lệ: %w", name, err)
		}
		ns := out[sd]
		if kind == string(site.KindAlias) {
			if mode == string(site.ModeRedirect) {
				ns.Redirect = append(ns.Redirect, d)
			} else {
				ns.Serve = append(ns.Serve, d)
			}
		}
		if inCert != 0 {
			ns.Covered = append(ns.Covered, d)
		}
		out[sd] = ns
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc tên của site: %w", err)
	}
	return out, nil
}
