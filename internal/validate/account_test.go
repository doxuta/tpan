package validate

import (
	"errors"
	"strings"
	"testing"
)

func TestParseUserName(t *testing.T) {
	ok := []string{"admin", "khach_a", "abc", "a12", strings.Repeat("a", 32)}
	for _, s := range ok {
		if u, err := ParseUserName(s); err != nil || u.String() != s {
			t.Errorf("ParseUserName(%q) = %v, %v; muốn nhận", s, u, err)
		}
	}
	bad := map[string]string{
		"":                      CodeUserNameInvalid,
		"ab":                    CodeUserNameInvalid,
		"1abc":                  CodeUserNameInvalid,
		"Admin":                 CodeUserNameInvalid,
		"a-b-c":                 CodeUserNameInvalid,
		"abc; rm -rf /":         CodeUserNameInvalid,
		"abc\n":                 CodeUserNameInvalid,
		strings.Repeat("a", 33): CodeUserNameInvalid,
		"root":                  CodeUserNameReserved,
		"tpan":                  CodeUserNameReserved,
		"mysql":                 CodeUserNameReserved,
		"tp_shop":               CodeUserNameReserved,
		"information_schema":    CodeUserNameReserved,
	}
	for s, code := range bad {
		_, err := ParseUserName(s)
		var ve *Error
		if !errors.As(err, &ve) || ve.Code != code {
			t.Errorf("ParseUserName(%q) = %v; muốn mã %s", s, err, code)
		}
	}
}

func TestParsePackageName(t *testing.T) {
	for _, s := range []string{"default", "s", "pro_2"} {
		if _, err := ParsePackageName(s); err != nil {
			t.Errorf("ParsePackageName(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "Pro", "2x", "a b", strings.Repeat("a", 33)} {
		if _, err := ParsePackageName(s); err == nil {
			t.Errorf("ParsePackageName(%q) nhận; muốn từ chối", s)
		}
	}
}

func TestStandaloneDBName(t *testing.T) {
	u, _ := ParseUserName("khach_a")
	got, err := StandaloneDBName(u, "shop")
	if err != nil || got != "khach_a_shop" {
		t.Fatalf("StandaloneDBName = %q, %v", got, err)
	}
	for _, s := range []string{"", "_x", "a-b", "a'b", "x;drop", strings.Repeat("a", 64-len("khach_a_")+1)} {
		if _, err := StandaloneDBName(u, s); err == nil {
			t.Errorf("StandaloneDBName(%q) nhận; muốn từ chối", s)
		}
	}
	if _, err := StandaloneDBName(UserName{}, "shop"); err == nil {
		t.Error("chủ rỗng mà vẫn nhận")
	}
}
