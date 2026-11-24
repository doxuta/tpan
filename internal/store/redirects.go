package store

// Luật chuyển hướng của site: bảng site_redirects (M19, docs/thay-doi/
// M19-thiet-ke.md mục 3.1, 3.2, 3.5). Cùng khuôn pending/active với site_names:
// Execute giữ chỗ (pending) trước job, job xong thì Activate, hỏng thì Release.
// Vhost chỉ vẽ luật active (activeRules, điền site.Names.Rules).
//
// Luật kiểm nguồn/đích nằm ở validate; luật vòng lặp, trùng, giới hạn kiểm ở
// core lúc Prepare. Ở đây đếm và kiểm trùng lại trong MỘT transaction
// (_txlock=immediate) để hai request đua nhau không vượt được giới hạn hay
// chèn hai luật cùng nguồn.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// Mã từ chối của AddRedirectPending (cùng chuỗi với core.CodeRedirectExists,
// core.CodeRedirectLimit; store không import core). Lỗi là *validate.Error để
// core đọc mã bằng errors.As rồi trả lời từ chối 409.
const (
	codeRedirectExists = "redirect_exists"
	codeRedirectLimit  = "redirect_limit"
)

const redirectColumns = "id, site, source, target, code, keep_query, created_at"

// scanRedirect đọc một dòng site_redirects. Dữ liệu DB không tin mù: nguồn và
// đích đi lại qua validate trước khi tới template.
func scanRedirect(r rowScanner) (string, site.Redirect, error) {
	var (
		id                       int64
		siteD, src, tgt, created string
		code, keep               int
	)
	if err := r.Scan(&id, &siteD, &src, &tgt, &code, &keep, &created); err != nil {
		return "", site.Redirect{}, fmt.Errorf("đọc dòng luật chuyển hướng: %w", err)
	}
	out := site.Redirect{ID: id, Code: code, KeepQuery: keep != 0}
	var err error
	if out.Source, err = validate.ParseRedirectSource(src); err != nil {
		return "", site.Redirect{}, fmt.Errorf("luật chuyển hướng %d của %s trong DB có nguồn không hợp lệ: %w", id, siteD, err)
	}
	if out.Target, err = validate.ParseRedirectTarget(tgt); err != nil {
		return "", site.Redirect{}, fmt.Errorf("luật chuyển hướng %d của %s trong DB có đích không hợp lệ: %w", id, siteD, err)
	}
	return siteD, out, nil
}

// AddRedirectPending giữ chỗ một luật (state pending) cho site, trong một
// transaction: bỏ dòng pending quá hạn của site (pendingTTL, như tên); nguồn
// đã có luật active thì redirect_exists; đã có dòng pending cùng nguồn (chạy
// lại sau khi tiến trình chết giữa job, S9) thì nhận lại dòng đó (ghi đè đích,
// mã, giữ query, làm mới hạn); không thì đếm mọi dòng của site (cả pending),
// đủ site.MaxRedirects thì redirect_limit, chưa thì chèn. Trả id của dòng.
func (s *Store) AddRedirectPending(ctx context.Context, siteD validate.Domain, r site.Redirect) (int64, error) {
	if siteD.IsZero() || r.Source.IsZero() || r.Target.IsZero() {
		return 0, errors.New("giữ luật chuyển hướng: thiếu site, nguồn hoặc đích")
	}
	if r.Code != 301 && r.Code != 302 {
		return 0, fmt.Errorf("giữ luật chuyển hướng %s: mã %d không phải 301 hay 302", r.Source, r.Code)
	}
	if r.KeepQuery && r.Target.HasQuery() {
		return 0, fmt.Errorf("giữ luật chuyển hướng %s: giữ query khi đích đã có query", r.Source)
	}
	now := s.now()
	d, src := siteD.String(), r.Source.String()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("giữ luật chuyển hướng %s của %s: %w", src, d, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "DELETE FROM site_redirects WHERE site = ? AND state = 'pending' AND created_at < ?",
		d, fmtTime(now.Add(-pendingTTL))); err != nil {
		return 0, fmt.Errorf("bỏ luật giữ chỗ quá hạn của %s: %w", d, err)
	}
	var (
		id    int64
		state string
	)
	err = tx.QueryRowContext(ctx, "SELECT id, state FROM site_redirects WHERE site = ? AND source = ?", d, src).Scan(&id, &state)
	switch {
	case err == nil && state == "pending":
		_, err = tx.ExecContext(ctx, "UPDATE site_redirects SET target = ?, code = ?, keep_query = ?, created_at = ? WHERE id = ?",
			r.Target.String(), r.Code, siteBool(r.KeepQuery), fmtTime(now), id)
	case err == nil:
		return 0, &validate.Error{Code: codeRedirectExists, Message: i18n.T(
			"site "+d+" đã có luật cho nguồn "+src+"; gỡ luật cũ trước (tpan site redirect rm)",
			"site "+d+" already has a rule for the source "+src+"; remove the old rule first (tpan site redirect rm)")}
	case errors.Is(err, sql.ErrNoRows):
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM site_redirects WHERE site = ?", d).Scan(&n); err != nil {
			break
		}
		if n >= site.MaxRedirects {
			return 0, &validate.Error{Code: codeRedirectLimit, Message: i18n.T(
				fmt.Sprintf("site %s đã có %d luật chuyển hướng (tối đa %d)", d, n, site.MaxRedirects),
				fmt.Sprintf("site %s already has %d redirect rules (at most %d)", d, n, site.MaxRedirects))}
		}
		err = tx.QueryRowContext(ctx,
			`INSERT INTO site_redirects (site, source, target, code, keep_query, state, created_at)
			 VALUES (?, ?, ?, ?, ?, 'pending', ?) RETURNING id`,
			d, src, r.Target.String(), r.Code, siteBool(r.KeepQuery), fmtTime(now)).Scan(&id)
	}
	if err != nil {
		return 0, fmt.Errorf("giữ luật chuyển hướng %s của %s: %w", src, d, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("giữ luật chuyển hướng %s của %s: %w", src, d, err)
	}
	return id, nil
}

// ActivateRedirect chuyển dòng id sang active (đã active: không lỗi). Dòng
// không còn (bị bỏ vì quá hạn) thì ErrNotFound, để job không vẽ luật không giữ.
func (s *Store) ActivateRedirect(ctx context.Context, id int64) error {
	return s.redirectExec(ctx, fmt.Sprintf("kích hoạt luật chuyển hướng %d", id),
		"UPDATE site_redirects SET state = 'active' WHERE id = ?", id)
}

// ReleaseRedirect xoá dòng id nếu nó còn pending (job hỏng). Không bao giờ xoá
// dòng active; không có dòng nào khớp không phải lỗi.
func (s *Store) ReleaseRedirect(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM site_redirects WHERE id = ? AND state = 'pending'", id); err != nil {
		return fmt.Errorf("nhả luật chuyển hướng %d: %w", id, err)
	}
	return nil
}

// DeleteRedirect xoá luật id của site (sau job gỡ thành công). Luật không
// thuộc site đó hay không có: ErrNotFound.
func (s *Store) DeleteRedirect(ctx context.Context, siteD validate.Domain, id int64) error {
	return s.redirectExec(ctx, fmt.Sprintf("gỡ luật chuyển hướng %d của %s", id, siteD),
		"DELETE FROM site_redirects WHERE site = ? AND id = ?", siteD.String(), id)
}

// redirectExec chạy một câu sửa đúng một dòng; không dòng nào: ErrNotFound.
func (s *Store) redirectExec(ctx context.Context, what, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	} else if n == 0 {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return nil
}

// RedirectsOf trả các luật ACTIVE của site, theo (created_at, id): đúng các
// luật đang có trong vhost. Dòng pending (job đang chạy, hay mồ côi chờ chạy
// lại hoặc hết hạn) không có ở đây. Không có luật: slice nil.
func (s *Store) RedirectsOf(ctx context.Context, siteD validate.Domain) ([]site.Redirect, error) {
	m, err := activeRules(ctx, s.db, siteD.String(), "")
	if err != nil {
		return nil, err
	}
	return m[siteD.String()], nil
}

// activeRules đọc luật active của các site của owner (rỗng là mọi site; siteD
// khác rỗng là đúng một site), theo (created_at, id). Site không có luật thì
// không có khoá trong map, nên Names.Rules giữ nil (vhost như trước M19, B5).
func activeRules(ctx context.Context, q querier, siteD, owner string) (map[string][]site.Redirect, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+redirectColumns+` FROM site_redirects
		 WHERE state = 'active' AND (? = '' OR site = ?)
		   AND (? = '' OR site IN (SELECT domain FROM sites WHERE owner = ?))
		 ORDER BY created_at, id`, siteD, siteD, owner, owner)
	if err != nil {
		return nil, fmt.Errorf("đọc luật chuyển hướng: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]site.Redirect{}
	for rows.Next() {
		sd, r, err := scanRedirect(rows)
		if err != nil {
			return nil, err
		}
		out[sd] = append(out[sd], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc luật chuyển hướng: %w", err)
	}
	return out, nil
}
