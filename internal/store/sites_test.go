package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// Khẳng định đúng bốn method của core.SiteStore (store không import core, nên
// chép chữ ký ra đây): lệch chữ ký thì test không biên dịch.
var _ interface {
	Site(ctx context.Context, d validate.Domain) (s site.Site, ok bool, err error)
	Sites(ctx context.Context) ([]site.Site, error)
	SaveSite(ctx context.Context, s site.Site) error
	DeleteSite(ctx context.Context, d validate.Domain) error
} = (*Store)(nil)

func siteDomain(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatalf("ParseDomain(%q): %v", s, err)
	}
	return d
}

func siteSample(t *testing.T, domain string) site.Site {
	return site.Site{
		Domain:     siteDomain(t, domain),
		Status:     site.Active,
		PHPVersion: "8.3",
		Created:    t0,
	}
}

func TestSiteThemVaDocRa(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := siteSample(t, "shop.example.com")
	in.SSL = true
	if err := s.SaveSite(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Site(ctx, in.Domain)
	if err != nil || !ok {
		t.Fatalf("Site = ok %v, err %v", ok, err)
	}
	want := in
	want.Slug = validate.SlugOf(in.Domain)
	want.Owner = "admin"              // Owner rỗng lưu thành admin (M13)
	want.Htaccess = site.HtaccessSafe // Htaccess rỗng lưu thành an-toan (M14)
	if got != want {
		t.Errorf("đọc ra %+v, muốn %+v", got, want)
	}
	if !got.TrashedAt.IsZero() {
		t.Errorf("TrashedAt = %v, muốn zero", got.TrashedAt)
	}
}

func TestSiteKhongCo(t *testing.T) {
	s, _ := openTemp(t)
	got, ok, err := s.Site(context.Background(), siteDomain(t, "none.example.com"))
	if err != nil || ok {
		t.Fatalf("Site = ok %v, err %v, muốn ok=false, err=nil", ok, err)
	}
	if got != (site.Site{}) {
		t.Errorf("site không có phải là zero, được %+v", got)
	}
	// Domain zero cũng chỉ là "không có", không phải lỗi.
	if _, ok, err := s.Site(context.Background(), validate.Domain{}); ok || err != nil {
		t.Errorf("Site(zero) = ok %v, err %v", ok, err)
	}
}

func TestSiteGhiDe(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := siteSample(t, "a.example.com")
	if err := s.SaveSite(ctx, in); err != nil {
		t.Fatal(err)
	}
	trashed := t0.Add(time.Hour)
	in.Status = site.Trashed
	in.PHPVersion = "8.2"
	in.SSL = true
	in.TrashedAt = trashed
	if err := s.SaveSite(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Site(ctx, in.Domain)
	if err != nil || !ok {
		t.Fatalf("Site = ok %v, err %v", ok, err)
	}
	if got.Status != site.Trashed || got.PHPVersion != "8.2" || !got.SSL || !got.TrashedAt.Equal(trashed) {
		t.Errorf("ghi đè không đủ: %+v", got)
	}
	all, err := s.Sites(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("Sites = %d site, err %v; muốn đúng 1 (ghi đè, không thêm dòng)", len(all), err)
	}
	// Đưa site ra khỏi thùng rác: TrashedAt zero phải về NULL.
	in.Status = site.Active
	in.TrashedAt = time.Time{}
	if err := s.SaveSite(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Site(ctx, in.Domain)
	if !got.TrashedAt.IsZero() {
		t.Errorf("TrashedAt = %v, muốn zero", got.TrashedAt)
	}
}

func TestSiteSlugLuonLaSlugOf(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := siteSample(t, "slug.example.com")
	// Người gọi đưa slug sai: phải bị bỏ qua.
	in.Slug = validate.SlugOf(siteDomain(t, "khac.example.com"))
	if err := s.SaveSite(ctx, in); err != nil {
		t.Fatal(err)
	}
	var col string
	if err := s.db.QueryRowContext(ctx, "SELECT slug FROM sites WHERE domain = ?", in.Domain.String()).Scan(&col); err != nil {
		t.Fatal(err)
	}
	if want := validate.SlugOf(in.Domain).String(); col != want {
		t.Errorf("cột slug = %q, muốn %q", col, want)
	}
	got, _, _ := s.Site(ctx, in.Domain)
	if got.Slug != validate.SlugOf(in.Domain) {
		t.Errorf("Slug đọc ra = %q", got.Slug)
	}
}

func TestSiteDomainZeroLaLoi(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.SaveSite(context.Background(), site.Site{Status: site.Active, PHPVersion: "8.3", Created: t0}); err == nil {
		t.Fatal("SaveSite với Domain zero phải lỗi")
	}
}

func TestSiteStatusSaiBiDBChan(t *testing.T) {
	s, _ := openTemp(t)
	in := siteSample(t, "bad.example.com")
	in.Status = "deleted"
	err := s.SaveSite(context.Background(), in)
	if err == nil {
		t.Fatal("status ngoài active/trashed phải bị ràng buộc CHECK chặn")
	}
}

func TestSiteLietKeTheoDomain(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	got, err := s.Sites(ctx)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Sites rỗng = %v, err %v; muốn slice rỗng không nil", got, err)
	}
	for _, d := range []string{"c.example.com", "a.example.com", "b.example.com"} {
		if err := s.SaveSite(ctx, siteSample(t, d)); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.Sites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, st := range got {
		names = append(names, st.Domain.String())
	}
	want := []string{"a.example.com", "b.example.com", "c.example.com"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("thứ tự = %v, muốn %v", names, want)
	}
}

func TestSiteXoa(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	in := siteSample(t, "del.example.com")
	if err := s.SaveSite(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSite(ctx, in.Domain); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Site(ctx, in.Domain); ok {
		t.Error("site còn sau khi xoá")
	}
	err := s.DeleteSite(ctx, in.Domain)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("xoá lần hai: %v, muốn ErrNotFound", err)
	}
}

func TestSiteDongHongTrongDB(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	// Chèn thẳng một domain không hợp lệ để chắc đọc ra không tin mù.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sites (domain, slug, status, php_version, ssl, created_at)
		 VALUES ('a.com; rm -rf /', 'x', 'active', '8.3', 0, ?)`, fmtTime(t0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sites(ctx); err == nil {
		t.Fatal("Sites phải lỗi khi DB có domain không hợp lệ")
	}
}
