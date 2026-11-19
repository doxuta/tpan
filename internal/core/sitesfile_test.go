package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
)

// Ví dụ nguyên văn ở mục 7 của KE-HOACH.md.
const sitesFileExample = `# sites.yaml
defaults:
  admin_email: admin@example.com
  ssl: false
sites:
  - domain: shop.example.com
    title: Cửa hàng
  - domain: blog.example.com
    ssl: true
`

func parseOK(t *testing.T, src string) SitesFile {
	t.Helper()
	f, err := ParseSitesFile(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseSitesFile: %v", err)
	}
	return f
}

func TestParseSitesFileExample(t *testing.T) {
	f := parseOK(t, sitesFileExample)
	if f.Defaults.AdminEmail != "admin@example.com" || f.Defaults.SSL {
		t.Errorf("defaults = %+v", f.Defaults)
	}
	if len(f.Sites) != 2 || f.Sites[0].Domain != "shop.example.com" || f.Sites[0].Title != "Cửa hàng" {
		t.Fatalf("sites = %+v", f.Sites)
	}
	if f.Sites[0].SSL != nil || f.Sites[1].SSL == nil || !*f.Sites[1].SSL {
		t.Errorf("ssl của hai dòng = %v, %v; muốn nil, true", f.Sites[0].SSL, f.Sites[1].SSL)
	}
}

func TestParseSitesFileRejects(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string // want: đoạn phải có trong lời lỗi (rỗng thì bỏ qua)
	}{
		{"khoá lạ ở dòng site", "sites:\n  - domain: a.example.com\n    admin_mail: x@example.com\n", "line 3"},
		{"khoá lạ ở gốc", "sites: []\nextra: 1\n", "line 2"},
		{"hai tài liệu", "sites:\n  - domain: a.example.com\n---\nsites:\n  - domain: b.example.com\n", "nhiều hơn một"},
		{"cú pháp hỏng", "sites: [\n", "line"},
		{"sai kiểu", "sites: abc\n", "line 1"},
		{"trống", "", "trống"},
		{"toàn khoảng trắng", "  \n\n", "trống"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSitesFile(strings.NewReader(tc.src))
			if got := refusedCode(t, err); got != "sites_file_invalid" {
				t.Errorf("code = %q", got)
			}
			var re *RefusedError
			errors.As(err, &re)
			if !strings.Contains(re.Message.Vi, tc.want) && !strings.Contains(re.Message.En, tc.want) {
				t.Errorf("lời lỗi %+v không có %q", re.Message, tc.want)
			}
		})
	}
}

func TestParseSitesFileTooLarge(t *testing.T) {
	// Đúng MaxSitesFileBytes thì nhận; thừa một byte thì từ chối. Phần đệm là
	// chú thích YAML nên file vẫn hợp lệ về cú pháp.
	head := "sites: []\n#"
	fill := func(n int) string { return head + strings.Repeat("x", n-len(head)) }
	if _, err := ParseSitesFile(strings.NewReader(fill(MaxSitesFileBytes))); err != nil {
		t.Errorf("file đúng %d byte bị từ chối: %v", MaxSitesFileBytes, err)
	}
	_, err := ParseSitesFile(strings.NewReader(fill(MaxSitesFileBytes + 1)))
	if got := refusedCode(t, err); got != "sites_file_invalid" {
		t.Errorf("code = %q", got)
	}
}

func resolveOK(t *testing.T, src string) []ApplySite {
	t.Helper()
	out, err := parseOK(t, src).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return out
}

func TestResolveExample(t *testing.T) {
	out := resolveOK(t, sitesFileExample)
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	shop, blog := out[0], out[1]
	if shop.Input.Domain.String() != "shop.example.com" || shop.Input.Title != "Cửa hàng" || shop.SSL {
		t.Errorf("shop = %+v", shop)
	}
	// Email lấy từ defaults; ssl của blog tự ghi true thắng defaults false.
	if shop.Input.Email.String() != "admin@example.com" || blog.Input.Email.String() != "admin@example.com" {
		t.Errorf("email = %q, %q", shop.Input.Email, blog.Input.Email)
	}
	if !blog.SSL || blog.Input.Title != "" {
		t.Errorf("blog = %+v", blog)
	}
}

func TestResolveDefaultsAndOverrides(t *testing.T) {
	out := resolveOK(t, `
defaults:
  admin_email: base@example.com
  ssl: true
sites:
  - domain: a.example.com
  - domain: b.example.com
    admin_email: own@example.com
    ssl: false
`)
	a, b := out[0], out[1]
	if !a.SSL || a.Input.Email.String() != "base@example.com" {
		t.Errorf("a = %+v; muốn ssl và email lấy từ defaults", a)
	}
	if b.SSL || b.Input.Email.String() != "own@example.com" {
		t.Errorf("b = %+v; muốn ssl=false tự ghi thắng defaults true, email riêng", b)
	}
}

func TestResolveNoEmailIsNotAnError(t *testing.T) {
	out := resolveOK(t, "sites:\n  - domain: a.example.com\n")
	if !out[0].Input.Email.IsZero() {
		t.Errorf("email = %q; muốn rỗng (PrepareApply mới từ chối site mới thiếu email)", out[0].Input.Email)
	}
}

func TestResolveRejects(t *testing.T) {
	long := strings.Repeat("a", 300) + ".example.com"
	for _, tc := range []struct {
		name, src, code, inMsg string
	}{
		{"chèn lệnh vào domain", "sites:\n  - domain: \"a.com; rm -rf /\"\n", "domain_invalid", "dòng 1"},
		{"$(id)", "sites:\n  - domain: \"$(id).example.com\"\n", "domain_invalid", "dòng 1"},
		{"đường dẫn", "sites:\n  - domain: ../../etc/passwd\n", "domain_invalid", "dòng 1"},
		{"xuống dòng", "sites:\n  - domain: \"a.example.com\\nb.com\"\n", "domain_invalid", "dòng 1"},
		{"domain quá dài", "sites:\n  - domain: " + long + "\n", "domain_invalid", "dòng 1"},
		{"domain rỗng ở dòng 2", "sites:\n  - domain: a.example.com\n  - title: x\n", "domain_invalid", "dòng 2"},
		{"email dòng hỏng", "sites:\n  - domain: a.example.com\n    admin_email: not-an-email\n", "email_invalid", "dòng 1 (a.example.com)"},
		{"email defaults hỏng", "defaults:\n  admin_email: \"x@y.com; id\"\nsites:\n  - domain: a.example.com\n", "email_invalid", "defaults.admin_email"},
		{"title có xuống dòng", "sites:\n  - domain: a.example.com\n    title: \"a\\nb\"\n", "title_invalid", "dòng 1 (a.example.com)"},
		{"trùng sau chuẩn hoá", "sites:\n  - domain: Shop.Example.com\n  - domain: shop.example.com\n", "sites_file_invalid", "trùng với dòng 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseOK(t, tc.src).Resolve()
			if got := refusedCode(t, err); got != tc.code {
				t.Fatalf("code = %q; muốn %q (%v)", got, tc.code, err)
			}
			var re *RefusedError
			errors.As(err, &re)
			if !strings.Contains(re.Message.Vi, tc.inMsg) {
				t.Errorf("lời lỗi %q không nói %q", re.Message.Vi, tc.inMsg)
			}
			// Lời lỗi không được chép lại giá trị độc hại người dùng gõ.
			for _, bad := range []string{"rm -rf", "$(id)", "passwd"} {
				if strings.Contains(re.Message.Vi+re.Message.En, bad) {
					t.Errorf("lời lỗi chép lại giá trị người dùng gõ (%q): %+v", bad, re.Message)
				}
			}
		})
	}
}

func TestResolveTooManyEntries(t *testing.T) {
	f := SitesFile{}
	for range MaxSitesEntries + 1 {
		f.Sites = append(f.Sites, SitesEntry{Domain: "a.example.com"})
	}
	// Quá số dòng phải bị chặn trước khi duyệt (nếu duyệt thì sẽ báo trùng).
	_, err := f.Resolve()
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != "sites_file_invalid" || !strings.Contains(re.Message.Vi, "tối đa") {
		t.Errorf("err = %v; muốn sites_file_invalid nói về trần số dòng", err)
	}
}

func TestResolveNoPartialResult(t *testing.T) {
	out, err := parseOK(t, "sites:\n  - domain: a.example.com\n  - domain: bad_domain\n").Resolve()
	if err == nil || out != nil {
		t.Errorf("out=%v err=%v; muốn lỗi và không có kết quả dở", out, err)
	}
}

func TestSitesFileMarshalRoundTrip(t *testing.T) {
	yes, no := true, false
	in := SitesFile{
		Defaults: SitesDefaults{AdminEmail: "admin@example.com", SSL: true},
		Sites: []SitesEntry{
			{Domain: "a.example.com", Title: "Cửa hàng: \"một\" # hai", AdminEmail: "x@example.com", SSL: &no},
			{Domain: "b.example.com", SSL: &yes},
			{Domain: "c.example.com"},
		},
	}
	b, err := in.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# ") || !strings.Contains(string(b), "tpan export") {
		t.Errorf("thiếu dòng chú thích đầu:\n%s", b)
	}
	out, err := ParseSitesFile(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("đọc lại: %v\n%s", err, b)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("Marshal rồi Parse ra khác:\nvào  %+v\nra   %+v\n%s", in, out, b)
	}
}

func TestSitesFileMarshalShape(t *testing.T) {
	// Không có defaults thì không in khối defaults; thụt hai dấu cách như mục 7.
	yes := true
	b, err := SitesFile{Sites: []SitesEntry{{Domain: "a.example.com", SSL: &yes}}}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\ndefaults:") {
		t.Errorf("in khối defaults rỗng:\n%s", b)
	}
	if !strings.Contains(string(b), "sites:\n  - domain: a.example.com\n    ssl: true\n") {
		t.Errorf("hình dạng khác mục 7:\n%s", b)
	}
	// Danh sách rỗng vẫn đọc lại được (export máy chưa có site).
	b, err = SitesFile{Sites: []SitesEntry{}}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSitesFile(strings.NewReader(string(b))); err != nil {
		t.Errorf("file rỗng không đọc lại được: %v\n%s", err, b)
	}
}

func TestExport(t *testing.T) {
	ctx := context.Background()
	ms := newMemSites()
	for _, s := range []site.Site{
		{Domain: mustDomain(t, "zeta.example.com"), Status: site.Active, SSL: true},
		{Domain: mustDomain(t, "alpha.example.com"), Status: site.Active},
		{Domain: mustDomain(t, "gone.example.com"), Status: site.Trashed, SSL: true},
	} {
		if err := ms.SaveSite(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	f, err := (&Sites{Store: ms}).Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sites) != 2 || f.Sites[0].Domain != "alpha.example.com" || f.Sites[1].Domain != "zeta.example.com" {
		t.Fatalf("sites = %+v; muốn alpha, zeta (bỏ site thùng rác, sắp theo domain)", f.Sites)
	}
	if f.Sites[0].SSL != nil || f.Sites[1].SSL == nil || !*f.Sites[1].SSL {
		t.Errorf("ssl chỉ ghi khi true: %v, %v", f.Sites[0].SSL, f.Sites[1].SSL)
	}
	if f.Sites[0].Title != "" || f.Sites[0].AdminEmail != "" || f.Defaults != (SitesDefaults{}) {
		t.Errorf("export không được có title, email, defaults: %+v", f)
	}

	// File export đưa lại cho Resolve phải qua (email thiếu không phải lỗi).
	b, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseOK(t, string(b)).Resolve(); err != nil {
		t.Errorf("Resolve file export: %v\n%s", err, b)
	}
}

func TestExportEmptyAndError(t *testing.T) {
	ctx := context.Background()
	f, err := (&Sites{Store: EmptySiteStore{}}).Export(ctx)
	if err != nil || f.Sites == nil || len(f.Sites) != 0 {
		t.Errorf("máy chưa có site: f=%+v err=%v; muốn danh sách rỗng không nil", f, err)
	}
}
