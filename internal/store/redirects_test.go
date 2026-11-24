package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// Khẳng định đúng chữ ký của core.RedirectStore (store không import core).
var _ interface {
	AddRedirectPending(ctx context.Context, siteD validate.Domain, r site.Redirect) (int64, error)
	ActivateRedirect(ctx context.Context, id int64) error
	ReleaseRedirect(ctx context.Context, id int64) error
	DeleteRedirect(ctx context.Context, siteD validate.Domain, id int64) error
	RedirectsOf(ctx context.Context, siteD validate.Domain) ([]site.Redirect, error)
} = (*Store)(nil)

func rule(t *testing.T, src, tgt string) site.Redirect {
	t.Helper()
	s, err := validate.ParseRedirectSource(src)
	if err != nil {
		t.Fatal(err)
	}
	d, err := validate.ParseRedirectTarget(tgt)
	if err != nil {
		t.Fatal(err)
	}
	return site.Redirect{Source: s, Target: d, Code: 301}
}

func redirectCode(err error) string {
	var ve *validate.Error
	if errors.As(err, &ve) {
		return ve.Code
	}
	return fmt.Sprint(err)
}

func countRedirects(t *testing.T, s *Store, where string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), "SELECT count(*) FROM site_redirects "+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestRedirectStoreLifecycle: pending không vào Names.Rules; activate thì vào,
// theo thứ tự (created_at, id); release chỉ xoá pending; delete theo site.
func TestRedirectStoreLifecycle(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	now := t0
	s.now = func() time.Time { now = now.Add(time.Second); return now }
	saveOwned(t, s, "example.com", "", false)
	saveOwned(t, s, "example.net", "", false)
	d := siteDomain(t, "example.com")

	if st, _, _ := s.Site(ctx, d); st.Names.Rules != nil {
		t.Fatalf("site không luật: Rules phải nil, được %#v", st.Names.Rules)
	}
	r1 := rule(t, "/old", "/new")
	r1.KeepQuery = true
	id1, err := s.AddRedirectPending(ctx, d, r1)
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := s.Site(ctx, d); st.Names.Rules != nil {
		t.Fatalf("luật pending đã vào Names.Rules: %+v", st.Names.Rules)
	}
	if err := s.ActivateRedirect(ctx, id1); err != nil {
		t.Fatal(err)
	}
	id2, err := s.AddRedirectPending(ctx, d, rule(t, "/a", "https://example.org/b?x=1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateRedirect(ctx, id2); err != nil {
		t.Fatal(err)
	}
	st, _, err := s.Site(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Names.Rules
	if len(got) != 2 || got[0].ID != id1 || got[1].ID != id2 || !got[0].KeepQuery || got[0].Code != 301 ||
		got[0].Target.String() != "/new" || got[1].Target.String() != "https://example.org/b?x=1" {
		t.Fatalf("Rules = %+v", got)
	}
	all, err := s.SitesOf(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range all {
		if want := map[string]int{"example.com": 2, "example.net": 0}[x.Domain.String()]; len(x.Names.Rules) != want {
			t.Errorf("SitesOf %s: %d luật", x.Domain, len(x.Names.Rules))
		}
	}
	if rs, err := s.RedirectsOf(ctx, d); err != nil || len(rs) != 2 {
		t.Errorf("RedirectsOf = %+v, %v", rs, err)
	}

	// Release không xoá dòng active.
	if err := s.ReleaseRedirect(ctx, id1); err != nil {
		t.Fatal(err)
	}
	if n := countRedirects(t, s, "WHERE id = ?", id1); n != 1 {
		t.Error("ReleaseRedirect xoá dòng active")
	}
	id3, err := s.AddRedirectPending(ctx, d, rule(t, "/c", "/d"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseRedirect(ctx, id3); err != nil || countRedirects(t, s, "WHERE id = ?", id3) != 0 {
		t.Errorf("ReleaseRedirect pending: %v", err)
	}
	if err := s.ActivateRedirect(ctx, id3); !errors.Is(err, ErrNotFound) {
		t.Errorf("Activate dòng đã nhả: %v", err)
	}
	// Delete theo site: id của site khác là không có.
	if err := s.DeleteRedirect(ctx, siteDomain(t, "example.net"), id1); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete khác site: %v", err)
	}
	if err := s.DeleteRedirect(ctx, d, id1); err != nil {
		t.Fatal(err)
	}
	// Xoá hẳn site: luật đi theo (ON DELETE CASCADE).
	if err := s.DeleteSite(ctx, d); err != nil {
		t.Fatal(err)
	}
	if n := countRedirects(t, s, ""); n != 0 {
		t.Errorf("xoá site còn %d luật", n)
	}
}

// TestRedirectLimitAndUnique: trùng nguồn (active) là redirect_exists; dòng
// pending cùng nguồn được nhận lại (S9); đủ MaxRedirects (đếm cả pending) là
// redirect_limit; đua 20 goroutine cùng nguồn: đúng một dòng.
func TestRedirectLimitAndUnique(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	now := t0
	s.now = func() time.Time { return now }
	saveOwned(t, s, "example.com", "", false)
	d := siteDomain(t, "example.com")

	id, err := s.AddRedirectPending(ctx, d, rule(t, "/old", "/new"))
	if err != nil {
		t.Fatal(err)
	}
	// Chạy lại (tiến trình trước chết giữa job): nhận lại đúng dòng, ghi đè đích.
	now = t0.Add(10 * time.Minute)
	again, err := s.AddRedirectPending(ctx, d, rule(t, "/old", "/newer"))
	if err != nil || again != id {
		t.Fatalf("nhận lại pending: id %d (muốn %d), %v", again, id, err)
	}
	if n := countRedirects(t, s, "WHERE target = '/newer' AND created_at = ?", fmtTime(now)); n != 1 {
		t.Error("nhận lại không ghi đè đích hay không làm mới hạn")
	}
	if err := s.ActivateRedirect(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRedirectPending(ctx, d, rule(t, "/old", "/x")); redirectCode(err) != codeRedirectExists {
		t.Errorf("trùng nguồn active: %v", err)
	}
	// Đầu vào sai không tới DB.
	bad := rule(t, "/q", "/new?a=1")
	bad.KeepQuery = true
	for _, r := range []site.Redirect{{}, {Source: bad.Source, Target: bad.Target, Code: 303}, bad} {
		if _, err := s.AddRedirectPending(ctx, d, r); err == nil {
			t.Errorf("%+v: muốn lỗi", r)
		}
	}
	// Site không có: khoá ngoại chặn.
	if _, err := s.AddRedirectPending(ctx, siteDomain(t, "none.example.com"), rule(t, "/a", "/b")); err == nil {
		t.Error("site không có mà giữ được luật")
	}

	// Đủ giới hạn, đếm cả pending.
	for i := 1; i < site.MaxRedirects-1; i++ {
		if _, err := s.AddRedirectPending(ctx, d, rule(t, fmt.Sprintf("/a%d", i), "/b")); err != nil {
			t.Fatal(err)
		}
	}
	last, err := s.AddRedirectPending(ctx, d, rule(t, "/last", "/b"))
	if err != nil {
		t.Fatalf("luật thứ %d: %v", site.MaxRedirects, err)
	}
	if _, err := s.AddRedirectPending(ctx, d, rule(t, "/over", "/b")); redirectCode(err) != codeRedirectLimit {
		t.Errorf("luật thứ %d: %v", site.MaxRedirects+1, err)
	}
	// Pending quá hạn bị bỏ, nhường chỗ.
	now = now.Add(pendingTTL + time.Minute)
	if _, err := s.AddRedirectPending(ctx, d, rule(t, "/over", "/b")); err != nil {
		t.Errorf("sau khi pending quá hạn bị bỏ: %v", err)
	}
	if countRedirects(t, s, "WHERE id = ?", last) != 0 || countRedirects(t, s, "WHERE state = 'active'") != 1 {
		t.Error("bỏ pending quá hạn sai dòng")
	}

	// Đua: 20 goroutine thêm cùng một nguồn mới; DB chỉ có một dòng.
	saveOwned(t, s, "example.org", "", false)
	o := siteDomain(t, "example.org")
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.AddRedirectPending(ctx, o, rule(t, "/race", "/b")); err != nil {
				t.Errorf("đua: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := countRedirects(t, s, "WHERE site = 'example.org'"); n != 1 {
		t.Errorf("đua để lại %d dòng", n)
	}
}
