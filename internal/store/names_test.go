package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// Khẳng định đúng chữ ký của core.NameStore (store không import core).
var _ interface {
	ClaimNames(ctx context.Context, cs []site.Claim) error
	CheckNames(ctx context.Context, cs []site.Claim) error
	ActivateNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	ReleaseNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	DeleteAlias(ctx context.Context, siteD, name validate.Domain) error
	SetAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) error
	SetInCert(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	NamesOf(ctx context.Context, siteD validate.Domain) ([]site.Name, error)
	NameSite(ctx context.Context, name validate.Domain) (site.Name, bool, error)
} = (*Store)(nil)

func claim(t *testing.T, name, siteD, owner string, k site.NameKind) site.Claim {
	return site.Claim{Name: siteDomain(t, name), Site: siteDomain(t, siteD), Owner: owner, Kind: k, Mode: site.ModeServe}
}

func nameCode(err error) string {
	var ne *site.NameError
	if errors.As(err, &ne) {
		return ne.Code
	}
	return fmt.Sprint(err)
}

func countNames(t *testing.T, s *Store, where string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), "SELECT count(*) FROM site_names "+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func saveOwned(t *testing.T, s *Store, domain, owner string, ssl bool) {
	t.Helper()
	st := siteSample(t, domain)
	st.Owner, st.SSL = owner, ssl
	if err := s.SaveSite(context.Background(), st); err != nil {
		t.Fatal(err)
	}
}

// TestSiteNamesPrimaryKey (B2): DB tự chặn một tên thuộc hai site, kể cả khi
// đi vòng qua ClaimNames.
func TestSiteNamesPrimaryKey(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveOwned(t, s, "example.com", "usera", false)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO site_names (name, site, owner, kind, mode, state, in_cert, created_at)
		 VALUES ('example.com', 'other.example.net', 'userb', 'alias', 'serve', 'active', 0, ?)`, fmtTime(t0))
	if err == nil {
		t.Fatal("chèn trùng tên phải bị khoá chính chặn")
	}
	// Ghi lại site khác không đè dòng của site đang giữ tên (S5).
	if err := s.ClaimNames(ctx, []site.Claim{claim(t, "www.example.com", "example.com", "usera", site.KindAlias)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateNames(ctx, siteDomain(t, "example.com"), []validate.Domain{siteDomain(t, "www.example.com")}); err != nil {
		t.Fatal(err)
	}
	saveOwned(t, s, "www.example.com", "userb", false) // đường mã lạ quên ClaimNames
	if n, ok, err := s.NameSite(ctx, siteDomain(t, "www.example.com")); err != nil || !ok || n.Site.String() != "example.com" || n.Kind != site.KindAlias {
		t.Errorf("NameSite sau SaveSite trùng = %+v, %v, %v; muốn vẫn là tên phụ của example.com", n, ok, err)
	}
}

// TestClaimNamesForeignRoot (B1): trong transaction, cả dòng pending và tên
// phụ của chủ khác chặn tên miền gốc; tên chính suy từ sites cũng tính.
func TestClaimNamesForeignRoot(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveOwned(t, s, "example.com", "usera", false)
	// Dòng sites không có dòng site_names (đường mã cũ): vẫn chặn.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sites (domain, slug, status, php_version, ssl, created_at, owner) VALUES ('example.org', 'x-org', 'active', '8.3', 0, ?, 'usera')`,
		fmtTime(t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimNames(ctx, []site.Claim{claim(t, "pending.example.net", "example.com", "usera", site.KindAlias)}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []site.Claim{
		claim(t, "www.example.com", "b.example.info", "userb", site.KindAlias),
		claim(t, "shop.example.com", "shop.example.com", "userb", site.KindPrimary),
		claim(t, "x.example.net", "b.example.info", "userb", site.KindAlias),
		claim(t, "www.example.org", "b.example.info", "userb", site.KindAlias),
	} {
		for _, f := range []func(context.Context, []site.Claim) error{s.CheckNames, s.ClaimNames} {
			if got := nameCode(f(ctx, []site.Claim{c})); got != site.CodeNameRootForeign {
				t.Errorf("%s cho %s: %s, muốn %s", c.Name, c.Owner, got, site.CodeNameRootForeign)
			}
		}
	}
	// Một tên lỗi thì cả lô không giữ tên nào.
	err := s.ClaimNames(ctx, []site.Claim{
		claim(t, "b.example.info", "b.example.info", "userb", site.KindPrimary),
		claim(t, "www.example.com", "b.example.info", "userb", site.KindAlias),
	})
	if nameCode(err) != site.CodeNameRootForeign {
		t.Errorf("lô lỗi: %v", err)
	}
	if n := countNames(t, s, "WHERE owner = 'userb'"); n != 0 {
		t.Errorf("lô lỗi vẫn giữ %d tên", n)
	}
	// Admin được miễn luật gốc, không miễn khoá chính.
	ad := claim(t, "www.example.com", "admin.example.info", "admin", site.KindAlias)
	ad.ByAdmin = true
	if err := s.ClaimNames(ctx, []site.Claim{ad}); err != nil {
		t.Errorf("admin: %v", err)
	}
	ad.Site = siteDomain(t, "admin2.example.info")
	if got := nameCode(s.ClaimNames(ctx, []site.Claim{ad})); got != site.CodeNameTakenSelf {
		t.Errorf("admin, tên đã giữ: %s", got)
	}
}

// TestClaimNamesRace (B2): 20 goroutine, hai chủ, cùng tên và cùng tên miền
// gốc: đúng một chủ thắng, không tên nào thuộc hai site. Hai Store mở cùng
// file (hai kết nối, như CLI và tpan serve) để BEGIN IMMEDIATE thật sự đua.
func TestClaimNamesRace(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	stores := []*Store{s, s2}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners = map[string]int{}
		errs    []error
	)
	for i := range 20 {
		owner := []string{"usera", "userb"}[i%2]
		name := []string{"example.com", "shop.example.com"}[(i/2)%2]
		c := claim(t, name, name, owner, site.KindPrimary)
		st := stores[(i/4)%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := st.ClaimNames(ctx, []site.Claim{c})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners[owner]++
				return
			}
			if c := nameCode(err); c != site.CodeNameTaken && c != site.CodeNameRootForeign {
				errs = append(errs, err)
			}
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("lỗi lạ: %v", errs)
	}
	if len(winners) != 1 {
		t.Fatalf("chủ thắng = %v, muốn đúng một", winners)
	}
	if n := countNames(t, s, ""); n < 1 || n > 2 {
		t.Errorf("site_names có %d dòng, muốn 1 hoặc 2", n)
	}
	if n := countNames(t, s, "WHERE owner <> (SELECT owner FROM site_names LIMIT 1)"); n != 0 {
		t.Errorf("%d dòng của chủ thua", n)
	}
}

// TestReleaseKeepsActive: ReleaseNames chỉ xoá dòng pending của đúng site.
func TestReleaseKeepsActive(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveOwned(t, s, "example.com", "usera", false)
	ex := siteDomain(t, "example.com")
	www, shop := siteDomain(t, "www.example.com"), siteDomain(t, "shop.example.com")
	if err := s.ClaimNames(ctx, []site.Claim{
		claim(t, "www.example.com", "example.com", "usera", site.KindAlias),
		claim(t, "shop.example.com", "example.com", "usera", site.KindAlias),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateNames(ctx, ex, []validate.Domain{www}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseNames(ctx, siteDomain(t, "other.example.com"), []validate.Domain{shop}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseNames(ctx, ex, []validate.Domain{ex, www, shop}); err != nil {
		t.Fatal(err)
	}
	ns, err := s.NamesOf(ctx, ex)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range ns {
		got = append(got, n.Name.String()+"/"+string(n.State))
	}
	if want := []string{"example.com/active", "www.example.com/active"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NamesOf = %v, muốn %v", got, want)
	}
	// Tên đã bị nhả thì không activate được.
	if err := s.ActivateNames(ctx, ex, []validate.Domain{shop}); !errors.Is(err, ErrNotFound) {
		t.Errorf("ActivateNames tên đã nhả: %v", err)
	}
}

// TestDeleteSiteDropsNames: xoá hẳn site giải phóng mọi tên của nó.
func TestDeleteSiteDropsNames(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveOwned(t, s, "example.com", "usera", false)
	saveOwned(t, s, "example.net", "usera", false)
	if err := s.ClaimNames(ctx, []site.Claim{
		claim(t, "www.example.com", "example.com", "usera", site.KindAlias),
		claim(t, "a.example.com", "example.com", "usera", site.KindAlias),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateNames(ctx, siteDomain(t, "example.com"), []validate.Domain{siteDomain(t, "www.example.com")}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSite(ctx, siteDomain(t, "example.com")); err != nil {
		t.Fatal(err)
	}
	if n := countNames(t, s, "WHERE site = 'example.com'"); n != 0 {
		t.Errorf("còn %d dòng của site đã xoá", n)
	}
	if n := countNames(t, s, ""); n != 1 {
		t.Errorf("site khác mất tên: còn %d dòng", n)
	}
	if err := s.ClaimNames(ctx, []site.Claim{claim(t, "www.example.com", "www.example.com", "userb", site.KindPrimary)}); err != nil {
		t.Errorf("tên đã giải phóng: %v", err)
	}
	// Không có site: không xoá gì, lỗi ErrNotFound.
	if err := s.DeleteSite(ctx, siteDomain(t, "none.example.org")); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteSite không có: %v", err)
	}
}

// TestClaimNamesStalePending (S9): dòng pending quá 1 giờ bị bỏ; trong hạn,
// cùng site + chủ + kiểu thì nhận lại, chủ khác thì không.
func TestClaimNamesStalePending(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	now := t0
	s.now = func() time.Time { return now }
	if err := s.ClaimNames(ctx, []site.Claim{
		claim(t, "example.com", "example.com", "userx", site.KindPrimary),
		claim(t, "www.example.com", "example.com", "userx", site.KindAlias),
	}); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(30 * time.Minute)
	if got := nameCode(s.ClaimNames(ctx, []site.Claim{claim(t, "example.com", "example.com", "usery", site.KindPrimary)})); got != site.CodeNameTaken {
		t.Errorf("chủ khác trong hạn: %s", got)
	}
	// Chạy lại của chính chủ: nhận lại cả tên phụ, làm mới hạn.
	if err := s.ClaimNames(ctx, []site.Claim{
		claim(t, "example.com", "example.com", "userx", site.KindPrimary),
		claim(t, "www.example.com", "example.com", "userx", site.KindAlias),
	}); err != nil {
		t.Errorf("nhận lại: %v", err)
	}
	if n := countNames(t, s, "WHERE created_at = ?", fmtTime(now)); n != 2 {
		t.Errorf("nhận lại phải làm mới hạn của 2 dòng, được %d", n)
	}
	// Quá hạn kể từ lần nhận lại: CheckNames coi như không có (không xoá),
	// ClaimNames xoá rồi giữ cho chủ mới.
	now = now.Add(pendingTTL + time.Second)
	cy := claim(t, "example.com", "example.com", "usery", site.KindPrimary)
	if err := s.CheckNames(ctx, []site.Claim{cy}); err != nil {
		t.Errorf("CheckNames sau hạn: %v", err)
	}
	if n := countNames(t, s, ""); n != 2 {
		t.Errorf("CheckNames không được xoá: còn %d dòng", n)
	}
	if err := s.ClaimNames(ctx, []site.Claim{cy}); err != nil {
		t.Fatalf("ClaimNames sau hạn: %v", err)
	}
	if n := countNames(t, s, ""); n != 1 {
		t.Errorf("sau khi bỏ dòng quá hạn còn %d dòng, muốn 1", n)
	}
	// Dòng active không bao giờ quá hạn.
	saveOwned(t, s, "example.net", "userx", false)
	now = now.Add(48 * time.Hour)
	if got := nameCode(s.ClaimNames(ctx, []site.Claim{claim(t, "example.net", "example.net", "usery", site.KindPrimary)})); got != site.CodeNameTaken {
		t.Errorf("dòng active: %s", got)
	}
}

// TestClaimNamesLimit (S10): trần MaxNames đếm trong transaction.
func TestClaimNamesLimit(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveOwned(t, s, "example.com", "usera", false)
	var cs []site.Claim
	for i := 1; i < site.MaxNames; i++ {
		cs = append(cs, claim(t, fmt.Sprintf("n%d.example.com", i), "example.com", "usera", site.KindAlias))
	}
	if err := s.ClaimNames(ctx, cs); err != nil {
		t.Fatal(err)
	}
	if got := nameCode(s.ClaimNames(ctx, []site.Claim{claim(t, "over.example.com", "example.com", "usera", site.KindAlias)})); got != site.CodeAliasLimit {
		t.Errorf("tên thứ 26: %s", got)
	}
	if n := countNames(t, s, "WHERE site = 'example.com'"); n != site.MaxNames {
		t.Errorf("site có %d tên, muốn %d", n, site.MaxNames)
	}
}

// TestNamesEdit: DeleteAlias, SetAliasMode, SetInCert và Site.Names đọc ra.
func TestNamesEdit(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	saveOwned(t, s, "example.com", "usera", true)
	ex := siteDomain(t, "example.com")
	www, b, p := siteDomain(t, "www.example.com"), siteDomain(t, "b.example.com"), siteDomain(t, "p.example.com")
	now := t0
	s.now = func() time.Time { now = now.Add(time.Second); return now }
	for _, n := range []validate.Domain{www, b, p} {
		if err := s.ClaimNames(ctx, []site.Claim{{Name: n, Site: ex, Owner: "usera", Kind: site.KindAlias, Mode: site.ModeServe}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ActivateNames(ctx, ex, []validate.Domain{www, b}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAliasMode(ctx, ex, www, site.ModeRedirect); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInCert(ctx, ex, []validate.Domain{ex, www}); err != nil {
		t.Fatal(err)
	}
	st, ok, err := s.Site(ctx, ex)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	want := site.Names{Serve: []validate.Domain{b}, Redirect: []validate.Domain{www}, Covered: []validate.Domain{ex, www}}
	if !reflect.DeepEqual(st.Names, want) {
		t.Errorf("Site.Names = %+v, muốn %+v (pending p không vẽ)", st.Names, want)
	}
	all, err := s.SitesOf(ctx, "usera")
	if err != nil || len(all) != 1 || !reflect.DeepEqual(all[0].Names, want) {
		t.Errorf("SitesOf = %+v, %v", all, err)
	}
	if other, _ := s.SitesOf(ctx, "userb"); len(other) != 0 {
		t.Errorf("SitesOf(userb) = %+v", other)
	}
	if err := s.DeleteAlias(ctx, ex, ex); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteAlias tên chính: %v", err)
	}
	if err := s.SetAliasMode(ctx, ex, siteDomain(t, "none.example.com"), site.ModeServe); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetAliasMode không có: %v", err)
	}
	if err := s.DeleteAlias(ctx, ex, p); err != nil { // dòng pending mồ côi gỡ được
		t.Error(err)
	}
	if err := s.DeleteAlias(ctx, ex, www); err != nil {
		t.Error(err)
	}
	if n := countNames(t, s, "WHERE site = 'example.com'"); n != 2 {
		t.Errorf("còn %d tên, muốn 2", n)
	}
	if err := s.SetInCert(ctx, ex, nil); err != nil {
		t.Fatal(err)
	}
	if n := countNames(t, s, "WHERE in_cert = 1"); n != 0 {
		t.Errorf("SetInCert rỗng còn %d tên in_cert", n)
	}
}
