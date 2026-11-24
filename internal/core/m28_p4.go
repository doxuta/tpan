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
		r, ok := registrable(st.Domain.String())
		if ok && r == want {
			return true, nil
		}
		// M28 đợt 2, B7: site có sẵn mà tên chính là một đuôi công cộng (chỉ
		// admin tạo được, ví dụ tên của một tổ chức nằm trong phần private của
		// PSL) không ra eTLD+1 để so. Theo PSL thì www.<tên đó> là một tên
		// đăng ký riêng, nhưng thực tế nó thuộc chủ của site gốc: coi mọi tên
		// con của site đó là đã có người dùng. (d bằng đúng tên đó thì d cũng
		// không ra eTLD+1, đã bị từ chối ở trên.)
		if !ok && strings.HasSuffix(d.String(), "."+st.Domain.String()) {
			return true, nil
		}
	}
	return false, nil
}

// CodeSlugTaken: domain mới có slug trùng slug của một site đang có (B11).
const CodeSlugTaken = "slug_taken"

// refuseSlugTaken (M28 đợt 2, B11): slug (validate.SlugOf) chỉ có 24 bit băm,
// dò được một domain khác cùng slug trong vài giây. Hai site cùng slug dùng
// chung tên user Linux, DB, pool PHP. Từ chối site MỚI nếu slug của nó trùng
// slug của một site đang có trong bảng sites (mọi trạng thái, kể cả thùng
// rác) mà domain khác, cho mọi chủ. Câu trả lời không nêu tên site kia.
//
// Không đổi độ dài băm: đổi thì tên user, DB của site sẵn có đổi theo.
// ponytail: kẻ tạo trước domain trùng slug vẫn làm chủ thật không tạo được
// site (như trước, khi đó unmanaged() từ chối); nay lời từ chối nói rõ vì sao
// và admin xem được site nào giữ slug đó. Muốn hết hẳn thì cấp slug dài hơn
// cho site mới và lưu slug thành nguồn sự thật trong bảng sites.
func refuseSlugTaken(all []site.Site, d validate.Domain) error {
	want := site.LayoutOf(d).Slug
	for _, st := range all {
		if st.Domain != d && st.Slug == want {
			return refuse(CodeSlugTaken, i18n.T(
				"không tạo được site "+d.String()+": tên rút gọn (slug) "+want.String()+" của nó, dùng đặt tên user Linux, DB và pool PHP, trùng với một site đã có trên máy; chọn domain khác hoặc nhờ admin xem",
				"cannot create site "+d.String()+": its short name (slug) "+want.String()+", used for the Linux user, database and PHP pool, is already used by another site on this server; choose another domain or ask the admin"))
		}
	}
	return nil
}
