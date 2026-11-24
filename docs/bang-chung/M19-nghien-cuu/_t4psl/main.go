// Bản thử t4 của M19 đợt 0: golang.org/x/net/publicsuffix (bản trong go.mod của
// repo) trả gì cho các tên sẽ gặp khi kiểm quyền sở hữu tên. Thư mục bắt đầu
// bằng "_" nên `go vet ./...` và `go test ./...` bỏ qua; chạy tay:
//
//	go run ./docs/bang-chung/M19-nghien-cuu/_t4psl
package main

import (
	"fmt"

	"golang.org/x/net/publicsuffix"

	"github.com/doxuta/tpan/internal/validate"
)

func main() {
	names := []string{
		"example.com", "www.example.com", "a.b.example.com",
		"example.co.uk", "shop.example.co.uk",
		"example.com.vn", "www.example.com.vn", "example.vn",
		"việtnam.example.net", "tênmiền.vn", "www.tênmiền.vn",
		"alice.github.io", "bob.github.io", "x.alice.github.io",
		"example.test", "www.example.test", "example.invalid", "example.cromulent",
		"co.uk", "com.vn", "github.io", "vn",
		"WWW.Example.COM", "www.example.com.",
	}
	for _, raw := range names {
		d, err := validate.ParseDomain(raw)
		if err != nil {
			fmt.Printf("%-24q ParseDomain lỗi: %v\n", raw, err)
			continue
		}
		suf, icann := publicsuffix.PublicSuffix(d.String())
		root, err := publicsuffix.EffectiveTLDPlusOne(d.String())
		if err != nil {
			fmt.Printf("%-24q ascii=%-28s suffix=%-14s icann=%-5v eTLD+1 LỖI: %v\n", raw, d, suf, icann, err)
			continue
		}
		fmt.Printf("%-24q ascii=%-28s suffix=%-14s icann=%-5v eTLD+1=%s\n", raw, d, suf, icann, root)
	}
}
