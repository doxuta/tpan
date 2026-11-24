package core

// M28 đợt 2, P4: user thường tạo site mới chỉ so đúng tên domain, nên tạo được
// www.<site của người khác> hay mail.<site của người khác>: phục vụ nội dung
// dưới tên đó và xin được chứng chỉ Let's Encrypt (HTTP-01 đi vào vhost của kẻ
// chiếm). Vá: so theo tên miền đăng ký được (eTLD+1). Luật ở một chỗ này, gọi
// từ POST /sites (server.siteCreateOwner) và POST /apply (PrepareApply có
// Scope). Admin không qua luật này.

import (
	"errors"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// RegistrableTaken báo d có cùng eTLD+1 với một site của chủ khác caller (kể
// cả admin, kể cả site trong thùng rác hay đang khoá: all là mọi dòng của bảng
// sites). Hệ quả có chủ ý: hai khách không thể có hai tên con của cùng một
// domain gốc. Tên không ra eTLD+1 (chính là public suffix, hay một nhãn) thì
// trả lỗi domain_invalid: user thường không được tạo site dưới tên đó.
//
// Từ M19 luật nằm ở site.RootForeign (một luật cho cả tên chính và tên phụ,
// gọi validate.RegisteredDomain); hàm này chỉ dựng tên chính từ all rồi gọi nó.
// Bảng site_names (cả pending, cả tên phụ) được store.ClaimNames xét lại trong
// transaction; ở đây là lớp từ chối sớm ngoài transaction của M28.
func RegistrableTaken(all []site.Site, d validate.Domain, caller string) (bool, error) {
	names := make([]site.Name, 0, len(all))
	for _, st := range all {
		names = append(names, site.Name{Name: st.Domain, Site: st.Domain, Owner: ownerOf(st), Kind: site.KindPrimary})
	}
	taken, err := site.RootForeign(names, d, caller)
	var ne *site.NameError
	if errors.As(err, &ne) && ne.Code == site.CodeNamePublicSuffix {
		return false, refuse(validate.CodeDomainInvalid, i18n.T(
			"tên miền "+d.String()+" không có phần đăng ký được (nó là đuôi công cộng như com, co.uk, github.io); chỉ admin tạo được site với tên này",
			"the domain "+d.String()+" has no registrable part (it is a public suffix such as com, co.uk, github.io); only the admin can create a site with this name"))
	}
	return taken, err
}
