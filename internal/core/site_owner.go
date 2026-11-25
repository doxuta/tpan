package core

// Đổi chủ một site (chỉ admin gọi). Trước bản này chủ chỉ đặt được lúc tạo.
//
// Đổi chủ là đổi ở tầng panel: user Linux, pool PHP, DB đi kèm site đi theo
// slug của domain nên không đụng tới. Thứ đổi theo chủ:
//   - sites.owner, site_names.owner, backups.owner (store.SetSiteOwner, một
//     transaction): site, tên phụ, backup, sự cố của site hiện ở chủ mới;
//   - trần lồng: site trong lồng dùng trần của gói chủ; gói chủ mới khác trần
//     thì ghi lại file slice (Plan "cage.sync" cho đúng site này), chủ đổi ở
//     móc before như đổi gói của user (Cages.PrepareUserEdit).
//
// Không đổi: DB rời (thuộc user), lịch sử job cũ (thuộc người đã chạy).

import (
	"context"
	"fmt"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// CodeOwnerSame: chủ mới trùng chủ hiện tại.
const CodeOwnerSame = "owner_same"

// SiteOwnerStore ghi chủ mới (store.SetSiteOwner).
type SiteOwnerStore interface {
	SetSiteOwner(ctx context.Context, d validate.Domain, owner string) error
}

// SiteOwnerChange là một lần đổi chủ đã kiểm xong, chưa ghi gì.
type SiteOwnerChange struct {
	Domain   validate.Domain
	From, To string
	// Plan khác nil khi trần lồng của site phải ghi lại: chạy Plan (chủ được
	// ghi ở móc before). nil: gọi Apply.
	Plan  *Planned
	apply func(ctx context.Context) error
}

// Apply ghi chủ mới khi không có Plan.
func (c *SiteOwnerChange) Apply(ctx context.Context) error { return c.apply(ctx) }

// Title là câu mô tả cho bản xem trước.
func (c *SiteOwnerChange) Title() i18n.Text {
	d := c.Domain.String()
	return i18n.T(
		"Chuyển site "+d+" từ "+c.From+" sang "+c.To+": tên phụ và backup của site đi theo; DB rời và lịch sử job của "+c.From+" ở lại",
		"Move site "+d+" from "+c.From+" to "+c.To+": the site's aliases and backups follow; standalone databases and job history of "+c.From+" stay")
}

// PrepareSiteOwner kiểm và dựng lần đổi chủ site d sang user to. Chỉ đọc.
// Từ chối: site không có, đang ở thùng rác, đang tạm khoá; chủ mới không có,
// đang bị khoá, hết chỗ trong gói (như tạo site mới cho họ), hay trùng chủ cũ.
func (c *Cages) PrepareSiteOwner(ctx context.Context, ow SiteOwnerStore, d validate.Domain, to string) (*SiteOwnerChange, error) {
	sites := &Sites{Store: c.Store, Accounts: c.Accounts, Settings: c.Settings}
	st, err := sites.find(ctx, d)
	if err != nil {
		return nil, err
	}
	dom := d.String()
	if st.Status == site.Trashed {
		return nil, refuse("site_trashed", i18n.T(
			"site "+dom+" đang ở thùng rác; khôi phục nó rồi mới đổi chủ",
			"site "+dom+" is in the trash; restore it before changing its owner"))
	}
	if st.Suspended != account.NotSuspended {
		return nil, refuse(CodeSiteSuspended, i18n.T(
			"site "+dom+" đang tạm khoá; mở khoá rồi mới đổi chủ",
			"site "+dom+" is suspended; unsuspend it before changing its owner"))
	}
	if to == "" {
		to = account.AdminName
	}
	from := ownerOf(st)
	if to == from {
		return nil, refuse(CodeOwnerSame, i18n.T(
			"site "+dom+" đã thuộc "+to, "site "+dom+" already belongs to "+to))
	}
	// Như tạo site mới cho chủ đó: user có thật, đang hoạt động, còn chỗ trong gói.
	if _, err := sites.newOwner(ctx, to); err != nil {
		return nil, err
	}
	ch := &SiteOwnerChange{Domain: d, From: from, To: to}
	ch.apply = func(ctx context.Context) error {
		if err := ow.SetSiteOwner(ctx, d, to); err != nil {
			return fmt.Errorf("đổi chủ site %s: %w", dom, err)
		}
		return nil
	}
	if !st.Caged || st.Status != site.Active {
		return ch, nil
	}
	old, err := sitePackage(ctx, c.Accounts, st)
	if err != nil {
		return nil, err
	}
	moved := st
	moved.Owner = to
	next, err := sitePackage(ctx, c.Accounts, moved)
	if err != nil {
		return nil, err
	}
	if old.Limits() == next.Limits() {
		return ch, nil
	}
	cfg, err := sites.stack(ctx)
	if err != nil {
		return nil, err
	}
	title := ch.Title()
	title.Vi += "; áp trần lồng của gói " + next.Name
	title.En += "; apply the cage limits of package " + next.Name
	plan := c.syncPlan(cfg, dom, []site.Site{st}, next.Limits(), title)
	pl := adminOwned(newPlanned(c.Sys, c.Journal, plan, nil))
	pl.before = ch.apply
	ch.Plan = pl
	return ch, nil
}
