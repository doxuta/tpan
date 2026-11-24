package site

// Tên của site (tên chính, tên phụ) và luật chuyển hướng (M19,
// docs/thay-doi/M19-thiet-ke.md mục 3.2). Gói này thuần, không đụng DB: store
// gọi CheckClaims bên trong transaction, core gọi nó cho bản xem trước.

import (
	"errors"
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
)

// Mã của NameError (mục 3.2 và 3.7).
const (
	CodeNamePublicSuffix = "domain_public_suffix"
	CodeNameReserved     = "domain_reserved"
	CodeAliasExists      = "alias_exists"
	CodeNameTaken        = "name_taken"
	CodeNameTakenSelf    = "name_taken_self"
	CodeNameRootForeign  = "name_root_foreign"
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
}

// NameError là lời từ chối của CheckClaims. Message (ở tầng trên) không chép
// tên site hay chủ của người khác; Other chỉ điền khi Code là name_taken_self.
type NameError struct {
	Code  string
	Name  validate.Domain
	Other validate.Domain
}

func (e *NameError) Error() string { return e.Code + ": " + e.Name.String() }

// errNamesTODO: thân thật do thợ 1a viết.
var errNamesTODO = errors.New("site: CheckClaims chưa làm (M19 đợt 1)")

// CheckClaims áp luật mục 3.2 cho mọi claim trên ảnh chụp existing (mọi dòng
// của site_names). Lỗi (*NameError) ở claim đầu tiên vi phạm.
func CheckClaims(claims []Claim, existing []Name) error {
	return errNamesTODO
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
