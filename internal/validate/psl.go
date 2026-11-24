package validate

// Tên miền gốc theo Public Suffix List (M19, docs/thay-doi/M19-thiet-ke.md mục
// 2.5 và 3.2). Đây là chỗ duy nhất của luật sở hữu tên (site.RootForeign,
// core.RegistrableTaken) gọi golang.org/x/net/publicsuffix. Bảng PSL nhúng
// trong gói đó chỉ đổi khi nâng golang.org/x/net trong go.mod.

import (
	"strings"

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
	r, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(d.s))
	if err != nil {
		return Domain{}, &Error{Code: CodeDomainPublicSuffix, Message: i18n.T(
			"tên miền là đuôi công cộng (như com.vn, co.uk, github.io), không có phần đăng ký được",
			"the domain is a public suffix (such as com.vn, co.uk, github.io) with no registrable part")}
	}
	return Domain{s: r}, nil
}
