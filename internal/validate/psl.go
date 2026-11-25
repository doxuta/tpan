package validate

// Tên miền gốc theo Public Suffix List (M19, docs/thay-doi/M19-thiet-ke.md mục
// 2.5 và 3.2). Đây là chỗ duy nhất của luật sở hữu tên (site.RootForeign,
// core.RegistrableTaken) gọi golang.org/x/net/publicsuffix. Bảng PSL nhúng
// trong gói đó chỉ đổi khi nâng golang.org/x/net trong go.mod.

import (
	"strings"
	"sync/atomic"

	"golang.org/x/net/publicsuffix"

	"github.com/doxuta/tpan/internal/i18n"
)

// CodeDomainPublicSuffix: tên là chính một hậu tố công cộng (co.uk, com.vn, github.io).
const CodeDomainPublicSuffix = "domain_public_suffix"

// RegisteredDomain trả tên miền gốc (eTLD+1) của d theo Public Suffix List nhúng
// trong golang.org/x/net/publicsuffix, cả phần ICANN lẫn phần private:
// alice.github.io và bob.github.io là hai tên miền gốc khác nhau. d là chính
// một hậu tố công cộng (hay rỗng) thì lỗi *Error mã CodeDomainPublicSuffix.
func RegisteredDomain(d Domain) (Domain, error) {
	// Bảng PSL so phân biệt hoa thường; ParseDomain đã hạ chữ thường, hạ lại
	// cho chắc vì Domain có thể được đọc lại từ dòng cũ trong tpan.db.
	name := strings.ToLower(d.s)
	suffixErr := &Error{Code: CodeDomainPublicSuffix, Message: i18n.T(
		"tên miền là đuôi công cộng (như com.vn, co.uk, github.io), không có phần đăng ký được",
		"the domain is a public suffix (such as com.vn, co.uk, github.io) with no registrable part")}
	if best := sharedSuffixOf(name); best != "" {
		if name == best {
			return Domain{}, suffixErr
		}
		rest := strings.TrimSuffix(name, "."+best)
		return Domain{s: rest[strings.LastIndexByte(rest, '.')+1:] + "." + best}, nil
	}
	r, err := publicsuffix.EffectiveTLDPlusOne(name)
	if err != nil {
		return Domain{}, suffixErr
	}
	return Domain{s: r}, nil
}

// sharedSuffixes là các "tên miền dùng chung" admin khai (core.SharedSuffixes):
// tên miền mà chủ máy chia tên con cho nhiều khách, ví dụ
// khach.example-hosting.vn. Với RegisteredDomain mỗi tên con trực tiếp của nó
// là một tên miền gốc riêng, như phần private của PSL (alice.github.io và
// bob.github.io). Không khai thì hai khách không thể có hai tên con của cùng
// một tên miền (luật tên miền gốc, M28 P4).
var sharedSuffixes atomic.Pointer[[]string]

// SetSharedSuffixes thay cả danh sách tên miền dùng chung của tiến trình này.
func SetSharedSuffixes(list []Domain) {
	ss := make([]string, 0, len(list))
	for _, d := range list {
		if !d.IsZero() {
			ss = append(ss, strings.ToLower(d.s))
		}
	}
	sharedSuffixes.Store(&ss)
}

// sharedSuffixOf trả tên miền dùng chung dài nhất mà name bằng hoặc nằm dưới.
func sharedSuffixOf(name string) string {
	p := sharedSuffixes.Load()
	if p == nil {
		return ""
	}
	best := ""
	for _, s := range *p {
		if (name == s || strings.HasSuffix(name, "."+s)) && len(s) > len(best) {
			best = s
		}
	}
	return best
}
