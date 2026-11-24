package core

// Hợp đồng M19 (đợt 0b): tên phụ của site, chuyển hướng, chứng chỉ phủ đủ các
// tên (docs/thay-doi/M19-thiet-ke.md mục 3.5). Kiểu và chữ ký ở đây là thứ
// server, CLI và giao diện dựa vào; thân hàm nằm ở names_m19.go,
// redirects_m19.go và route_m19.go.

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// errM19TODO: thân thật do thợ đợt 1 đến 4 viết.
var errM19TODO = errors.New("core: M19 chưa làm")

// NamesOp là một thay đổi tên phụ hay luật chuyển hướng đã qua điều kiện đầu.
// Trường không xuất (sites, site, plan, claim, id luật) do thợ 1c thêm.
type NamesOp struct{}

// Preview trả bản xem trước của Plan, không giữ chỗ gì.
func (o *NamesOp) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.Preview{}, errM19TODO
}

// Execute: giữ chỗ (nếu cần), chạy job, activate hoặc release. error chỉ khác
// nil khi không chạy được hay không ghi được DB; job hỏng thì xem Report.
func (o *NamesOp) Execute(ctx context.Context, onEvent func(engine.Event)) (NamesResult, error) {
	return NamesResult{}, errM19TODO
}

// NamesResult là kết quả của một NamesOp.
type NamesResult struct {
	Report engine.Report `json:"report"`
	// NeedsCert: site có SSL và bộ tên muốn có khác bộ tên trong chứng chỉ.
	NeedsCert bool        `json:"needs_cert"`
	Notes     []i18n.Text `json:"notes,omitempty"`
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

func (s *Sites) PrepareAliasAdd(ctx context.Context, siteD, name validate.Domain, m site.NameMode) (*NamesOp, error) {
	return nil, errM19TODO
}

func (s *Sites) PrepareAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) (*NamesOp, error) {
	return nil, errM19TODO
}

func (s *Sites) PrepareAliasRemove(ctx context.Context, siteD, name validate.Domain) (*NamesOp, error) {
	return nil, errM19TODO
}

func (s *Sites) PrepareRedirectAdd(ctx context.Context, siteD validate.Domain, in RedirectInput) (*NamesOp, error) {
	return nil, errM19TODO
}

func (s *Sites) PrepareRedirectRemove(ctx context.Context, siteD validate.Domain, id int64) (*NamesOp, error) {
	return nil, errM19TODO
}

func (s *Sites) NameList(ctx context.Context, siteD validate.Domain) ([]NameView, error) {
	return nil, errM19TODO
}

func (s *Sites) RedirectList(ctx context.Context, siteD validate.Domain) ([]site.Redirect, error) {
	return nil, errM19TODO
}

// RouteExplainHost như RouteExplain nhưng nhận cả tên phụ (mục 3.8).
func (s *Sites) RouteExplainHost(ctx context.Context, host validate.Domain) (RouteTrace, error) {
	return RouteTrace{}, errM19TODO
}
