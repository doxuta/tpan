package core

// Hợp đồng M19 (đợt 0b): tên phụ của site, chuyển hướng, chứng chỉ phủ đủ các
// tên (docs/thay-doi/M19-thiet-ke.md mục 3.5). Kiểu và chữ ký ở đây là thứ
// server, CLI và giao diện dựa vào; thân hàm nằm ở names_m19.go,
// redirects_m19.go và route_m19.go.

import (
	"context"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// NamesOp là một thay đổi tên phụ hay luật chuyển hướng đã qua điều kiện đầu.
// Preview và Execute ở names_m19.go.
type NamesOp struct {
	sites *Sites
	ns    NameStore
	op    namesOpKind
	// site là dòng đang có; next là site sau thay đổi (vhost vẽ từ next.Names:
	// tên active cộng đúng thay đổi của job này, không bao giờ tên pending khác).
	site, next site.Site
	name       validate.Domain
	mode       site.NameMode
	// pending: alias.rm gỡ một dòng còn pending (nhả bằng ReleaseNames).
	pending bool
	// claim: alias.add giữ chỗ trước khi chạy job; nil với thao tác khác.
	claim     *site.Claim
	plan      engine.Plan
	needsCert bool
	// sslNote: ghi chú job site.ssl chạy tiếp, cho bản xem trước (S14).
	sslNote *i18n.Text
	// rs khác nil: thao tác luật chuyển hướng (site.redirect.add/rm); Execute
	// đi đường executeRedirect (redirects_m19.go). rule là luật được thêm (ID
	// điền lúc giữ chỗ) hay được gỡ.
	rs   RedirectStore
	rule site.Redirect
}

// NamesResult là kết quả của một NamesOp.
type NamesResult struct {
	Report engine.Report `json:"report"`
	// NeedsCert: site có SSL và bộ tên muốn có khác bộ tên trong chứng chỉ.
	NeedsCert bool        `json:"needs_cert"`
	Notes     []i18n.Text `json:"notes,omitempty"`
	// FollowUpJob: ID job site.ssl chạy tiếp (nếu có), để giao diện theo dõi.
	FollowUpJob string `json:"follow_up_job,omitempty"`
}

// RedirectInput là luật chuyển hướng người dùng gõ, chưa kiểm.
type RedirectInput struct {
	Source, Target string
	Code           int
	KeepQuery      bool
}

// NameView là một dòng của GET .../names và tpan site alias list.
type NameView struct {
	Name  string         `json:"name"`
	Kind  site.NameKind  `json:"kind"`
	Mode  site.NameMode  `json:"mode"`
	State site.NameState `json:"state"`
	// DNS: "ok" | "warn" | "missing" (tra lúc gọi).
	DNS    string `json:"dns"`
	InCert bool   `json:"in_cert"`
}

// NameStore là phần store giữ tên (mục 3.2). *store.Store cài nó; core lấy
// bằng ép kiểu s.Store.(NameStore) (hàm nameStore do thợ 1c viết). Không thêm vào SiteStore để các bản giả cũ khỏi sửa.
type NameStore interface {
	ClaimNames(ctx context.Context, cs []site.Claim) error
	CheckNames(ctx context.Context, cs []site.Claim) error
	ActivateNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	ReleaseNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	DeleteAlias(ctx context.Context, siteD, name validate.Domain) error
	SetAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) error
	SetInCert(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	NamesOf(ctx context.Context, siteD validate.Domain) ([]site.Name, error)
	NameSite(ctx context.Context, name validate.Domain) (site.Name, bool, error)
}

// RedirectStore là phần store giữ luật chuyển hướng (mục 3.2), cùng khuôn
// NameStore: *store.Store cài nó; cmd/tpan/main.go có dòng kiểm lúc biên dịch.
// Thân PrepareRedirectAdd, PrepareRedirectRemove, RedirectList ở redirects_m19.go.
type RedirectStore interface {
	// AddRedirectPending: đếm < site.MaxRedirects và kiểm trùng nguồn trong
	// cùng transaction; lời từ chối là *validate.Error mã redirect_exists hay
	// redirect_limit. Dòng pending cùng nguồn được nhận lại (S9).
	AddRedirectPending(ctx context.Context, siteD validate.Domain, r site.Redirect) (int64, error)
	ActivateRedirect(ctx context.Context, id int64) error
	// ReleaseRedirect chỉ xoá dòng pending.
	ReleaseRedirect(ctx context.Context, id int64) error
	DeleteRedirect(ctx context.Context, siteD validate.Domain, id int64) error
	// RedirectsOf: luật active, theo (created_at, id).
	RedirectsOf(ctx context.Context, siteD validate.Domain) ([]site.Redirect, error)
}

// RouteExplainHost như RouteExplain nhưng nhận cả tên phụ (mục 3.8).
func (s *Sites) RouteExplainHost(ctx context.Context, host validate.Domain) (RouteTrace, error) {
	return s.routeExplainHost(ctx, host)
}
