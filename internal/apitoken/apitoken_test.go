package apitoken

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const alphabet = "abcdefghijklmnopqrstuvwxyz234567"

func TestNewWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		tok := New()
		if len(tok) != Len || !strings.HasPrefix(tok, Prefix) {
			t.Fatalf("token %q: dài %d, muốn %d có tiền tố %q", tok, len(tok), Len, Prefix)
		}
		if strings.Trim(tok[len(Prefix):], alphabet) != "" {
			t.Fatalf("token %q có ký tự ngoài a-z2-7", tok)
		}
		if !WellFormed(tok) {
			t.Fatalf("WellFormed(%q) = false", tok)
		}
		if seen[tok] {
			t.Fatalf("token trùng %q", tok)
		}
		seen[tok] = true
	}
}

// TestDoiMotKyTuThiSai: đổi bất kỳ một ký tự nào sau tiền tố sang một ký tự
// hợp lệ khác thì phần kiểm phải bắt được (gõ sai, chép thiếu).
func TestDoiMotKyTuThiSai(t *testing.T) {
	for range 20 {
		tok := New()
		for i := len(Prefix); i < Len; i++ {
			for _, c := range []byte(alphabet) {
				if c == tok[i] {
					continue
				}
				bad := tok[:i] + string(c) + tok[i+1:]
				if WellFormed(bad) {
					t.Fatalf("đổi vị trí %d thành %q mà vẫn hợp lệ: %q", i, c, bad)
				}
			}
		}
	}
}

func TestWellFormedTuChoi(t *testing.T) {
	tok := New()
	for _, s := range []string{
		"", Prefix, tok[:Len-1], tok + "a",
		"tpaN" + tok[4:], "xpan_" + tok[5:],
		strings.ToUpper(tok),
		tok[:10] + "1" + tok[11:], // '1' không thuộc base32
		tok[:10] + "\n" + tok[11:],
	} {
		if WellFormed(s) {
			t.Errorf("WellFormed(%q) = true", s)
		}
	}
}

func TestHashHint(t *testing.T) {
	tok := New()
	h := Hash(tok)
	if len(h) != 64 || strings.Contains(h, tok[5:]) || h != Hash(tok) {
		t.Errorf("Hash = %q", h)
	}
	if Hash("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Error("Hash không phải sha256 hex")
	}
	if got := Hint(tok); got != Prefix+tok[5:11]+"…" {
		t.Errorf("Hint = %q", got)
	}
	// Không panic với chuỗi ngắn (dòng DB hỏng, token gõ thiếu).
	_ = Hint("")
	_ = Hint("tpan_ab")
}

func TestParseScopes(t *testing.T) {
	got, err := ParseScopes([]string{"backups:read", "sites:write", "sites:read", "backups:read", "system:read"})
	if err != nil {
		t.Fatal(err)
	}
	want := Scopes{{GroupSites, true}, {GroupBackups, false}, {GroupSystem, false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseScopes = %+v, muốn %+v", got, want)
	}
	if s := got.String(); s != "sites:write,backups:read,system:read" {
		t.Errorf("String = %q", s)
	}
	if l := got.List(); !reflect.DeepEqual(l, []string{"sites:write", "backups:read", "system:read"}) {
		t.Errorf("List = %q", l)
	}
	// Đi một vòng qua dạng lưu trong DB thì như cũ.
	back, err := ParseScopes(strings.Split(got.String(), ","))
	if err != nil || !reflect.DeepEqual(back, got) {
		t.Errorf("đọc lại %q = %+v, %v", got.String(), back, err)
	}
	// Mọi nhóm đều có đọc và ghi.
	for _, g := range Groups {
		for _, p := range []string{"read", "write"} {
			if _, err := ParseScopes([]string{string(g) + ":" + p}); err != nil {
				t.Errorf("%s:%s: %v", g, p, err)
			}
		}
	}
}

func TestParseScopesLoi(t *testing.T) {
	for _, list := range [][]string{
		nil, {}, {""}, {"sites"}, {"sites:"}, {":read"}, {"sites:admin"}, {"Sites:read"}, {"sites:READ"},
		{"site:read"}, {"sites:read:x"}, {" sites:read"}, {"sites:read", "nope:write"}, {"sites:read\n"},
	} {
		_, err := ParseScopes(list)
		var ie *InvalidError
		if !errors.As(err, &ie) || ie.Code != "token_scope_invalid" || ie.Vi == "" || ie.En == "" {
			t.Errorf("ParseScopes(%q) = %v, muốn *InvalidError token_scope_invalid", list, err)
		}
	}
	_, err := ParseScopes([]string{"sites:read", "nope:write"})
	if err == nil || !strings.Contains(err.Error(), `"nope:write"`) {
		t.Errorf("lỗi phải nêu mục sai: %v", err)
	}
}

func TestListRongKhongNil(t *testing.T) {
	if l := (Scopes{}).List(); l == nil || len(l) != 0 {
		t.Errorf("List của tập rỗng = %#v, muốn []string{}", l)
	}
	if s := (Scopes{}).String(); s != "" {
		t.Errorf("String của tập rỗng = %q", s)
	}
}

func TestAllows(t *testing.T) {
	s := Scopes{{GroupSites, true}, {GroupBackups, false}}
	cases := []struct {
		g     Group
		write bool
		want  bool
	}{
		{GroupSites, false, true}, {GroupSites, true, true},
		{GroupBackups, false, true}, {GroupBackups, true, false},
		{GroupSystem, false, false}, {GroupSystem, true, false},
	}
	for _, c := range cases {
		if got := s.Allows(c.g, c.write); got != c.want {
			t.Errorf("Allows(%s, write=%v) = %v, muốn %v", c.g, c.write, got, c.want)
		}
	}
}

func TestGroupsFor(t *testing.T) {
	if got := GroupsFor(true); !reflect.DeepEqual(got, Groups) {
		t.Errorf("admin: %v", got)
	}
	user := GroupsFor(false)
	if len(user) != len(Groups)-1 {
		t.Errorf("user thường: %v", user)
	}
	for _, g := range user {
		if AdminOnly(g) {
			t.Errorf("user thường chọn được %s", g)
		}
	}
	// Không được sửa lẫn vào Groups.
	user[0] = "x"
	if Groups[0] != GroupSites {
		t.Error("GroupsFor trả chung mảng với Groups")
	}
}

func TestCheckNew(t *testing.T) {
	sites := Scopes{{GroupSites, false}}
	system := Scopes{{GroupSystem, true}}
	ok := []struct {
		admin bool
		name  string
		sc    Scopes
		days  int
	}{
		{false, "ci", sites, 1},
		{false, strings.Repeat("ạ", 64), sites, 90},
		{true, "backup đêm", system, 90},
	}
	for _, c := range ok {
		if err := CheckNew(c.admin, c.name, c.sc, c.days, DefaultMaxDays); err != nil {
			t.Errorf("CheckNew(%v, %q, %v, %d) = %v", c.admin, c.name, c.sc, c.days, err)
		}
	}
	bad := []struct {
		admin bool
		name  string
		sc    Scopes
		days  int
		code  string
	}{
		{false, "", sites, 30, "token_name_invalid"},
		{false, "   ", sites, 30, "token_name_invalid"},
		{false, strings.Repeat("a", 65), sites, 30, "token_name_invalid"},
		{false, "a\nb", sites, 30, "token_name_invalid"},
		{false, "a\x00", sites, 30, "token_name_invalid"},
		{false, "a\u0085", sites, 30, "token_name_invalid"},
		{false, "\xff\xfe", sites, 30, "token_name_invalid"},
		{false, "ci", nil, 30, "token_scope_invalid"},
		{false, "ci", system, 30, "token_scope_invalid"},
		{false, "ci", Scopes{{GroupSites, false}, {GroupSystem, false}}, 30, "token_scope_invalid"},
		{true, "ci", Scopes{{"nope", false}}, 30, "token_scope_invalid"},
		{false, "ci", sites, 0, "token_days_invalid"},
		{false, "ci", sites, -1, "token_days_invalid"},
		{false, "ci", sites, 91, "token_days_invalid"},
	}
	for _, c := range bad {
		err := CheckNew(c.admin, c.name, c.sc, c.days, DefaultMaxDays)
		var ie *InvalidError
		if !errors.As(err, &ie) || ie.Code != c.code || ie.Vi == "" || ie.En == "" {
			t.Errorf("CheckNew(%v, %q, %v, %d) = %v, muốn %s", c.admin, c.name, c.sc, c.days, err, c.code)
		}
	}
	// Trần tuyệt đối 365 ngày, kể cả khi setting ghi lớn hơn.
	if err := CheckNew(true, "ci", sites, MaxDaysLimit+1, MaxDaysLimit+100); err == nil {
		t.Error("quá MaxDaysLimit phải lỗi")
	}
	if err := CheckNew(true, "ci", sites, MaxDaysLimit, MaxDaysLimit); err != nil {
		t.Errorf("đúng MaxDaysLimit: %v", err)
	}
}
