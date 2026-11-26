package site

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/validate"
)

func nd(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatalf("ParseDomain(%q): %v", s, err)
	}
	return d
}

func nameRow(t *testing.T, name, siteD, owner string, k NameKind, st NameState) Name {
	return Name{Name: nd(t, name), Site: nd(t, siteD), Owner: owner, Kind: k, Mode: ModeServe, State: st}
}

func claimOf(t *testing.T, name, siteD, owner string, k NameKind) Claim {
	return Claim{Name: nd(t, name), Site: nd(t, siteD), Owner: owner, Kind: k, Mode: ModeServe}
}

// wantCode: "" là không lỗi.
func checkCode(t *testing.T, label string, err error, wantCode string) {
	t.Helper()
	if wantCode == "" {
		if err != nil {
			t.Errorf("%s: lỗi %v, muốn qua", label, err)
		}
		return
	}
	var ne *NameError
	if !errors.As(err, &ne) || ne.Code != wantCode {
		t.Errorf("%s: lỗi %v, muốn %s", label, err, wantCode)
	}
}

// TestCheckClaims: bảng luật mục 3.2 (7.1), mỗi dòng một claim trên cùng ảnh chụp.
func TestCheckClaims(t *testing.T) {
	existing := []Name{
		nameRow(t, "a.example.com", "a.example.com", "usera", KindPrimary, StateActive),
		nameRow(t, "www.a.example.com", "a.example.com", "usera", KindAlias, StateActive),
		nameRow(t, "a2.example.com", "a2.example.com", "usera", KindPrimary, StateActive),
		nameRow(t, "b.example.net", "b.example.net", "userb", KindPrimary, StateActive),
		nameRow(t, "x.example.org", "b.example.net", "userb", KindAlias, StatePending),
		nameRow(t, "p.example.info", "a.example.com", "usera", KindAlias, StatePending),
		nameRow(t, "bob.github.io", "bob.github.io", "userb", KindPrimary, StateActive),
	}
	cases := []struct {
		name string
		c    Claim
		want string
	}{
		{"tên tự do", claimOf(t, "free.example.co.uk", "a.example.com", "usera", KindAlias), ""},
		{"cùng gốc với site của chính mình", claimOf(t, "shop.example.com", "a.example.com", "usera", KindAlias), ""},
		{"tên phụ đã có của chính site", claimOf(t, "www.a.example.com", "a.example.com", "usera", KindAlias), CodeAliasExists},
		{"tên chính của site làm tên phụ", claimOf(t, "a.example.com", "a.example.com", "usera", KindAlias), CodeAliasExists},
		{"tên của site khác cùng chủ", claimOf(t, "a2.example.com", "a.example.com", "usera", KindAlias), CodeNameTakenSelf},
		{"tên phụ của site khác cùng chủ", claimOf(t, "www.a.example.com", "a2.example.com", "usera", KindAlias), CodeNameTakenSelf},
		{"tên của chủ khác", claimOf(t, "b.example.net", "a.example.com", "usera", KindAlias), CodeNameTaken},
		{"tên pending của chủ khác", claimOf(t, "x.example.org", "a.example.com", "usera", KindAlias), CodeNameTaken},
		{"tên miền gốc của chủ khác", claimOf(t, "www.b.example.net", "a.example.com", "usera", KindAlias), CodeNameRootForeign},
		{"tên miền gốc pending của chủ khác", claimOf(t, "y.example.org", "a.example.com", "usera", KindAlias), CodeNameRootForeign},
		{"site mới trên gốc của chủ khác", claimOf(t, "example.net", "example.net", "usera", KindPrimary), CodeNameRootForeign},
		{"pending của chính site: nhận lại", claimOf(t, "p.example.info", "a.example.com", "usera", KindAlias), ""},
		{"pending cùng site khác kiểu", claimOf(t, "p.example.info", "p.example.info", "usera", KindPrimary), CodeNameTakenSelf},
		{"hậu tố công cộng", claimOf(t, "com.vn", "a.example.com", "usera", KindAlias), CodeNamePublicSuffix},
		{"hậu tố private", claimOf(t, "github.io", "a.example.com", "usera", KindAlias), CodeNamePublicSuffix},
		{"tên khác dưới hậu tố private", claimOf(t, "alice.github.io", "alice.github.io", "usera", KindPrimary), ""},
		{"tên dành cho phép thử", claimOf(t, "tpan-probe-a-0123456789ab.test", "tpan-probe-a-0123456789ab.test", "usera", KindPrimary), CodeNameReserved},
		{"chạy lại tên chính", claimOf(t, "a.example.com", "a.example.com", "usera", KindPrimary), ""},
	}
	for _, c := range cases {
		checkCode(t, c.name, CheckClaims([]Claim{c.c}, existing), c.want)
	}
}

// TestCheckClaimsByAdmin: admin miễn luật tên miền gốc và public suffix, không
// miễn luật một tên một site, không miễn tên dành cho phép thử.
func TestCheckClaimsByAdmin(t *testing.T) {
	existing := []Name{nameRow(t, "b.example.net", "b.example.net", "userb", KindPrimary, StateActive)}
	admin := func(c Claim) Claim { c.ByAdmin = true; return c }
	checkCode(t, "gốc của chủ khác", CheckClaims([]Claim{admin(claimOf(t, "www.b.example.net", "c.example.org", "admin", KindAlias))}, existing), "")
	checkCode(t, "hậu tố công cộng", CheckClaims([]Claim{admin(claimOf(t, "com.vn", "com.vn", "admin", KindPrimary))}, existing), "")
	checkCode(t, "tên đã có", CheckClaims([]Claim{admin(claimOf(t, "b.example.net", "c.example.org", "admin", KindAlias))}, existing), CodeNameTaken)
	checkCode(t, "phép thử", CheckClaims([]Claim{admin(claimOf(t, "tpan-probe-b-0123456789ab.test", "c.example.org", "admin", KindAlias))}, existing), CodeNameReserved)
}

// TestCheckClaimsPrimaryRerunOtherOwner (S4): dòng tên chính pending của X
// (job chết giữa chừng) không được thành no-op cho Y.
func TestCheckClaimsPrimaryRerunOtherOwner(t *testing.T) {
	existing := []Name{nameRow(t, "example.com", "example.com", "userx", KindPrimary, StatePending)}
	checkCode(t, "chủ khác", CheckClaims([]Claim{claimOf(t, "example.com", "example.com", "usery", KindPrimary)}, existing), CodeNameTaken)
	checkCode(t, "chủ khác, admin làm", CheckClaims([]Claim{{Name: nd(t, "example.com"), Site: nd(t, "example.com"), Owner: "admin", Kind: KindPrimary, ByAdmin: true}}, existing), CodeNameTaken)
	checkCode(t, "cùng chủ", CheckClaims([]Claim{claimOf(t, "example.com", "example.com", "userx", KindPrimary)}, existing), "")
}

// TestCheckClaimsSameBatch (S10): claim đã nhận là một dòng khi xét claim sau.
func TestCheckClaimsSameBatch(t *testing.T) {
	lot := []Claim{
		claimOf(t, "example.com", "example.com", "usera", KindPrimary),
		claimOf(t, "shop.example.com", "shop.example.com", "userb", KindPrimary),
	}
	checkCode(t, "hai chủ một gốc", CheckClaims(lot, nil), CodeNameRootForeign)
	lot = []Claim{
		claimOf(t, "example.com", "example.com", "usera", KindPrimary),
		claimOf(t, "example.com", "example.com", "userb", KindPrimary),
	}
	checkCode(t, "hai chủ một tên", CheckClaims(lot, nil), CodeNameTaken)
	lot = []Claim{
		claimOf(t, "example.com", "example.com", "usera", KindPrimary),
		claimOf(t, "www.example.com", "example.com", "usera", KindAlias),
	}
	checkCode(t, "site mới có www", CheckClaims(lot, nil), "")
}

// TestCheckClaimsLimit: tên chính + 24 tên phụ; tên thứ 26 bị từ chối, kể cả
// trong cùng một lô, kể cả admin.
func TestCheckClaimsLimit(t *testing.T) {
	existing := []Name{nameRow(t, "example.com", "example.com", "usera", KindPrimary, StateActive)}
	for i := 1; i < MaxNames-1; i++ {
		existing = append(existing, nameRow(t, fmt.Sprintf("n%d.example.com", i), "example.com", "usera", KindAlias, StateActive))
	}
	one := claimOf(t, "last.example.com", "example.com", "usera", KindAlias)
	two := claimOf(t, "over.example.com", "example.com", "usera", KindAlias)
	checkCode(t, "tên thứ 25", CheckClaims([]Claim{one}, existing), "")
	checkCode(t, "tên thứ 26 cùng lô", CheckClaims([]Claim{one, two}, existing), CodeAliasLimit)
	two.ByAdmin = true
	checkCode(t, "admin", CheckClaims([]Claim{one, two}, existing), CodeAliasLimit)
	// Nhận lại dòng pending không tính thêm.
	existing = append(existing, nameRow(t, "last.example.com", "example.com", "usera", KindAlias, StatePending))
	checkCode(t, "nhận lại khi đủ", CheckClaims([]Claim{one}, existing), "")
}

// TestCheckClaimsForeignRoot (B1): user thường không gắn được tên thuộc gốc của
// người khác (admin được miễn theo S1, xem TestCheckClaimsByAdmin).
func TestCheckClaimsForeignRoot(t *testing.T) {
	existing := []Name{nameRow(t, "example.com", "example.com", "usera", KindPrimary, StateActive)}
	for _, n := range []string{"www.example.com", "shop.example.com", "a.b.example.com"} {
		checkCode(t, n, CheckClaims([]Claim{claimOf(t, n, "b.example.net", "userb", KindAlias)}, existing), CodeNameRootForeign)
	}
}

func TestRootForeign(t *testing.T) {
	existing := []Name{
		nameRow(t, "example.com", "example.com", "usera", KindPrimary, StateActive),
		nameRow(t, "x.example.net", "example.com", "usera", KindAlias, StatePending),
	}
	cases := []struct {
		d, owner string
		want     bool
	}{
		{"www.example.com", "userb", true},
		{"example.net", "userb", true}, // tên phụ pending cũng tính
		{"www.example.com", "usera", false},
		{"example.org", "userb", false},
	}
	for _, c := range cases {
		got, err := RootForeign(existing, nd(t, c.d), c.owner)
		if err != nil || got != c.want {
			t.Errorf("RootForeign(%s, %s) = %v, %v; muốn %v", c.d, c.owner, got, err, c.want)
		}
	}
	_, err := RootForeign(existing, nd(t, "co.uk"), "userb")
	checkCode(t, "co.uk", err, CodeNamePublicSuffix)
}

// TestCheckClaimsLongNames (v0.2.14): trần 46 byte của tên phụ đã bỏ, vì tpan
// nới bảng tên của nginx. Tên phụ 47 byte và 200 byte đều qua, như tên chính.
func TestCheckClaimsLongNames(t *testing.T) {
	if MaxNameLen != 253 {
		t.Fatalf("MaxNameLen = %d, muốn 253 (trần của DNS)", MaxNameLen)
	}
	n47 := strings.Repeat("a", 47-len(".example.com")) + ".example.com"
	// 200 byte, mỗi nhãn không quá 63 byte.
	n200 := strings.Repeat("b", 62) + "." + strings.Repeat("c", 62) + "." + strings.Repeat("d", 200-62-62-2-len(".example.com")) + ".example.com"
	if len(n47) != 47 || len(n200) != 200 {
		t.Fatalf("độ dài %d %d", len(n47), len(n200))
	}
	for _, n := range []string{n47, n200} {
		alias := claimOf(t, n, "a.example.com", "usera", KindAlias)
		checkCode(t, fmt.Sprintf("tên phụ %d byte", len(n)), CheckClaims([]Claim{alias}, nil), "")
		alias.ByAdmin = true
		checkCode(t, fmt.Sprintf("tên phụ %d byte, admin", len(n)), CheckClaims([]Claim{alias}, nil), "")
		checkCode(t, fmt.Sprintf("tên chính %d byte", len(n)), CheckClaims([]Claim{claimOf(t, n, n, "usera", KindPrimary)}, nil), "")
	}
}
