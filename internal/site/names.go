package site

// Tên của site (tên chính, tên phụ) và luật chuyển hướng (M19,
// docs/thay-doi/M19-thiet-ke.md mục 3.2). Gói này thuần, không đụng DB: store
// gọi CheckClaims bên trong transaction, core gọi nó cho bản xem trước.

import (
	"slices"
	"time"

	"github.com/doxuta/tpan/internal/validate"
)

// NameKind: "primary" (tên chính, sites.domain) hoặc "alias" (tên phụ).
type NameKind string

// NameMode: "serve" (phục vụ cùng nội dung) hoặc "redirect" (301 về tên chính).
type NameMode string

// NameState: "pending" (đã giữ chỗ, job đang chạy) hoặc "active".
type NameState string

const (
	KindPrimary NameKind = "primary"
	KindAlias   NameKind = "alias"

	ModeServe    NameMode = "serve"
	ModeRedirect NameMode = "redirect"

	StatePending NameState = "pending"
	StateActive  NameState = "active"
)

const (
	// MaxNames: tên chính + 24 tên phụ, vừa profile nhỏ nhất của Let's Encrypt.
	MaxNames = 25
	// MaxRedirects: số luật chuyển hướng tối đa mỗi site.
	MaxRedirects = 50
	// MaxNameLen (soát cuối C8): số byte tối đa của một tên phụ, để nginx với
	// server_names_hash_bucket_size 64 (nginx-org-include.conf.tmpl; gói
	// Ubuntu mặc định bằng cache line, 64 trên amd64 và arm64) vẫn dựng được
	// bảng băm tên. src/core/ngx_hash.c, ngx_hash_init: một tên cần
	// NGX_HASH_ELT_SIZE = sizeof(void*) + ngx_align(len+2, sizeof(void*)) cộng
	// thêm sizeof(void*) đánh dấu cuối bucket; máy 64 bit: 8 + align8(len+2) +
	// 8 <= 64, tức len <= 46. Khớp số đo của soát cuối (46 byte: nginx -t đạt,
	// 47 byte: "could not build server_names_hash"). Áp cho mọi stack: máy
	// Apache có thể đổi sang nginx, và một tên dài làm hỏng đổi stack cả máy.
	// Tên chính không bị luật này (hành vi trước M19 giữ nguyên).
	MaxNameLen = 46
)

// Mã của NameError (mục 3.2 và 3.7).
const (
	CodeNamePublicSuffix = "domain_public_suffix"
	CodeNameReserved     = "domain_reserved"
	CodeAliasExists      = "alias_exists"
	CodeNameTaken        = "name_taken"
	CodeNameTakenSelf    = "name_taken_self"
	CodeNameRootForeign  = "name_root_foreign"
	// CodeAliasLimit: site đã có đủ MaxNames tên (S10).
	CodeAliasLimit = "alias_limit"
	// CodeNameTooLong: tên phụ dài hơn MaxNameLen byte (soát cuối C8).
	CodeNameTooLong = "name_too_long"
)

// Name là một dòng của bảng site_names.
type Name struct {
	Name    validate.Domain
	Site    validate.Domain
	Owner   string
	Kind    NameKind
	Mode    NameMode
	State   NameState
	InCert  bool
	Created time.Time
}

// Claim là yêu cầu giữ một tên cho một site.
type Claim struct {
	Name  validate.Domain
	Site  validate.Domain
	Owner string
	Kind  NameKind
	Mode  NameMode
	// ByAdmin: admin đang làm; miễn luật tên miền gốc và public suffix (như
	// M28 P4), không miễn luật một tên một site.
	ByAdmin bool
}

// NameError là lời từ chối của CheckClaims. Message (ở tầng trên) không chép
// tên site hay chủ của người khác; Other chỉ điền khi Code là name_taken_self.
type NameError struct {
	Code  string
	Name  validate.Domain
	Other validate.Domain
}

func (e *NameError) Error() string { return e.Code + ": " + e.Name.String() }

// CheckClaims áp luật mục 3.2 cho mọi claim, theo thứ tự, trên ảnh chụp
// existing (mọi dòng của site_names, cộng tên chính suy từ bảng sites). Lỗi
// (*NameError) ở claim đầu tiên vi phạm. Mỗi claim đã nhận được coi là một
// dòng pending trước khi xét claim sau (S10), nên hai claim trong cùng một lô
// cũng không lấn tên miền gốc của nhau.
//
// Với mỗi claim:
//  1. Tên dành cho phép thử (IsProbeDomain): domain_reserved, kể cả admin.
//     Tên phụ dài hơn MaxNameLen byte: name_too_long, kể cả admin.
//  2. Đã có dòng mang tên đó:
//     - claim tên chính, dòng cùng site VÀ cùng chủ: no-op (chạy lại site add,
//     apply lần hai), không xét gì thêm (S4);
//     - claim tên phụ, dòng pending cùng site, cùng chủ, cùng kiểu: nhận lại
//     (job trước chết giữa chừng, S9);
//     - còn lại: alias_exists (cùng site), name_taken_self (site khác của cùng
//     chủ), name_taken (của chủ khác). Không ai được miễn luật này (B2).
//  3. Trừ admin: tên là hậu tố công cộng (domain_public_suffix) hay tên miền
//     gốc có tên của chủ khác (name_root_foreign).
//  4. Site đã đủ MaxNames tên: alias_limit.
func CheckClaims(claims []Claim, existing []Name) error {
	rows := append([]Name(nil), existing...)
	for _, c := range claims {
		if IsProbeDomain(c.Name.String()) {
			return &NameError{Code: CodeNameReserved, Name: c.Name}
		}
		// Trước bước tra dòng: lời chỉ phụ thuộc chính tên, không lộ tên của ai.
		if c.Kind == KindAlias && len(c.Name.String()) > MaxNameLen {
			return &NameError{Code: CodeNameTooLong, Name: c.Name}
		}
		reclaim := false
		if i := slices.IndexFunc(rows, func(n Name) bool { return n.Name == c.Name }); i >= 0 {
			n := rows[i]
			mine := n.Site == c.Site && n.Owner == c.Owner
			switch {
			case mine && c.Kind == KindPrimary && n.Kind == KindPrimary:
				continue
			case mine && c.Kind == KindAlias && n.Kind == KindAlias && n.State == StatePending:
				reclaim = true
			case n.Owner != c.Owner:
				return &NameError{Code: CodeNameTaken, Name: c.Name}
			case n.Site == c.Site:
				return &NameError{Code: CodeAliasExists, Name: c.Name}
			default:
				return &NameError{Code: CodeNameTakenSelf, Name: c.Name, Other: n.Site}
			}
		}
		if !c.ByAdmin {
			foreign, err := RootForeign(rows, c.Name, c.Owner)
			if err != nil {
				return err
			}
			if foreign {
				return &NameError{Code: CodeNameRootForeign, Name: c.Name}
			}
		}
		if reclaim {
			continue // dòng đã có, không thêm vào số tên
		}
		count := 0
		for _, n := range rows {
			if n.Site == c.Site {
				count++
			}
		}
		if count >= MaxNames {
			return &NameError{Code: CodeAliasLimit, Name: c.Name}
		}
		rows = append(rows, Name{Name: c.Name, Site: c.Site, Owner: c.Owner, Kind: c.Kind, Mode: c.Mode, State: StatePending})
	}
	return nil
}

// RootForeign báo có dòng nào trong existing (pending hay active, tên chính hay
// phụ) cùng tên miền gốc với d mà owner khác. d không có tên miền gốc (là
// public suffix) thì lỗi *NameError{Code: CodeNamePublicSuffix}. Dòng không
// tính được tên miền gốc (dữ liệu cũ, admin tạo) thì bỏ qua, như M28. Đây là
// luật P4 duy nhất: core.RegistrableTaken (M28) và CheckClaims đều gọi nó.
//
// ponytail: duyệt cả danh sách và tính PSL lại mỗi dòng; một máy chỉ có vài
// trăm tên. Nhiều hơn thì thêm cột tên miền gốc có chỉ mục.
func RootForeign(existing []Name, d validate.Domain, owner string) (bool, error) {
	want, err := validate.RegisteredDomain(d)
	if err != nil {
		return false, &NameError{Code: CodeNamePublicSuffix, Name: d}
	}
	for _, n := range existing {
		if n.Owner == owner {
			continue
		}
		if r, err := validate.RegisteredDomain(n.Name); err == nil && r == want {
			return true, nil
		}
	}
	return false, nil
}

// Names là các tên phụ active và luật chuyển hướng active của một site, theo
// thứ tự cố định (created_at, name) / (created_at, id) để vhost sinh ra không
// đổi giữa hai lần vẽ.
type Names struct {
	Serve    []validate.Domain
	Redirect []validate.Domain
	// Covered: tên có in_cert = 1 (cả tên chính); chỉ bản SSL dùng.
	Covered []validate.Domain
	Rules   []Redirect
}

// Redirect là một luật chuyển hướng theo đường dẫn.
type Redirect struct {
	ID        int64
	Source    validate.RedirectSource
	Target    validate.RedirectTarget
	Code      int // 301 | 302
	KeepQuery bool
}
