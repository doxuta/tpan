package core

import (
	"testing"

	"github.com/doxuta/tpan/internal/site"
)

// TestRegistrableTakenTenLaDuoiCongCongB7: site có sẵn mà tên chính là một
// đuôi công cộng (chỉ admin tạo được) không ra eTLD+1 để so. Trước B7 nó bị
// bỏ qua nên khách khác tạo được www.<tên đó>; nay mọi tên con của nó là đã
// có người dùng.
func TestRegistrableTakenTenLaDuoiCongCongB7(t *testing.T) {
	all := []site.Site{
		{Domain: mustDomain(t, "github.io"), Owner: "userb", Status: site.Active},
		{Domain: mustDomain(t, "co.uk"), Status: site.Trashed}, // Owner rỗng là admin
	}
	cases := []struct {
		domain string
		want   bool
	}{
		{"www.github.io", true},
		{"a.b.github.io", true},
		{"shop.co.uk", true},
		{"xgithub.io", false}, // chỉ chung đuôi chữ, không phải tên con
		{"example.com", false},
	}
	for _, c := range cases {
		got, err := RegistrableTaken(all, mustDomain(t, c.domain), "usera")
		if err != nil || got != c.want {
			t.Errorf("RegistrableTaken(%s) = %v, %v; muốn %v", c.domain, got, err, c.want)
		}
	}
	// Site đó của chính người gọi: không chặn chính mình.
	if got, err := RegistrableTaken(all, mustDomain(t, "www.github.io"), "userb"); err != nil || got {
		t.Errorf("chủ của github.io tạo www.github.io: %v, %v; muốn false", got, err)
	}
}
