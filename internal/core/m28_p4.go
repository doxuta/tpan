package core

// M28 đợt 2, P4: user thường tạo site mới chỉ so đúng tên domain, nên tạo được
// www.<site của người khác> hay mail.<site của người khác>: phục vụ nội dung
// dưới tên đó và xin được chứng chỉ Let's Encrypt (HTTP-01 đi vào vhost của kẻ
// chiếm). Vá: so theo tên miền đăng ký được (eTLD+1). Luật ở một chỗ này, gọi
// từ POST /sites (server.siteCreateOwner) và POST /apply (PrepareApply có
// Scope). Admin không qua luật này.

import (
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// registrable trả eTLD+1 của d theo danh sách Public Suffix nhúng trong
// golang.org/x/net/publicsuffix. Danh sách đó có cả phần private (github.io,
// appspot.com...): a.github.io và b.github.io là hai tên đăng ký khác nhau.
func registrable(d string) (string, bool) {
	// Bảng PSL so phân biệt hoa thường; validate.ParseDomain đã hạ chữ thường,
	// hạ lại cho chắc vì giá trị có thể đến từ dòng cũ trong tpan.db.
	r, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(d))
	return r, err == nil
}

// RegistrableTaken báo d có cùng eTLD+1 với một site của chủ khác caller (kể
// cả admin, kể cả site trong thùng rác hay đang khoá: all là mọi dòng của bảng
// sites). Hệ quả có chủ ý: hai khách không thể có hai tên con của cùng một
// domain gốc. Tên không ra eTLD+1 (chính là public suffix, hay một nhãn) thì
// trả lỗi domain_invalid: user thường không được tạo site dưới tên đó.
//
// ponytail: duyệt cả bảng trong Go, một máy chỉ có vài trăm site (apply trần
// 500 dòng một lô). Nhiều hơn thì thêm cột eTLD+1 có chỉ mục vào bảng sites.
func RegistrableTaken(all []site.Site, d validate.Domain, caller string) (bool, error) {
	want, ok := registrable(d.String())
	if !ok {
		return false, refuse(validate.CodeDomainInvalid, i18n.T(
			"tên miền "+d.String()+" không có phần đăng ký được (nó là đuôi công cộng như com, co.uk, github.io); chỉ admin tạo được site với tên này",
			"the domain "+d.String()+" has no registrable part (it is a public suffix such as com, co.uk, github.io); only the admin can create a site with this name"))
	}
	for _, st := range all {
		if ownerOf(st) == caller {
			continue
		}
		if r, ok := registrable(st.Domain.String()); ok && r == want {
			return true, nil
		}
	}
	return false, nil
}
