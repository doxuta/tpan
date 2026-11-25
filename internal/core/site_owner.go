package core

// Đổi chủ một site (chỉ admin gọi). Trước bản này chủ chỉ đặt được lúc tạo.
//
// Đổi chủ là đổi ở tầng panel: user Linux, pool PHP, DB đi kèm site đi theo
// slug của domain nên không đụng tới. Thứ đổi theo chủ:
//   - sites.owner, site_names.owner, backups.owner (store.SetSiteOwner, một
//     transaction): site, tên phụ, backup, sự cố của site hiện ở chủ mới;
//   - trần lồng: site trong lồng dùng trần của gói chủ; gói chủ mới khác trần
//     thì cùng job đó ghi lại file slice của site (bước CageFiles).
//
// Luôn là một Plan "site.owner" có ít nhất một bước (ghi chủ mới), kể cả khi
// không đụng máy: giao diện và dòng lệnh đi đúng ba chặng xem trước, chạy,
// lịch sử như mọi thao tác khác, và job hỏng thì chủ cũ được trả lại.
//
// Không đổi: DB rời (thuộc user), lịch sử job cũ (thuộc người đã chạy).

import (
	"context"
	"fmt"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// CodeOwnerSame: chủ mới trùng chủ hiện tại.
const CodeOwnerSame = "owner_same"

// SiteOwnerStore ghi chủ mới (store.SetSiteOwner).
type SiteOwnerStore interface {
	SetSiteOwner(ctx context.Context, d validate.Domain, owner string) error
}

// siteOwnerStep ghi chủ mới của site vào bảng; Undo trả chủ cũ.
type siteOwnerStep struct {
	sites    SiteStore
	ow       SiteOwnerStore
	d        validate.Domain
	from, to string
}

func (s *siteOwnerStep) Info() engine.StepInfo {
	d := s.d.String()
	return engine.StepInfo{
		ID:    "site.owner:" + d,
		Title: i18n.T("Ghi chủ mới của "+d+": "+s.from+" → "+s.to, "Record the new owner of "+d+": "+s.from+" → "+s.to),
		Why: i18n.T(
			"Chủ của site quyết định ai thấy và sửa được site, các tên miền phụ và bản backup của nó trên panel. File, cơ sở dữ liệu và mật khẩu của site không đổi; site không ngừng chạy.",
			"The owner decides who can see and change the site, its alias domains and its backups in the panel. The site's files, database and passwords do not change; the site keeps running."),
	}
}

func (s *siteOwnerStep) Check(ctx context.Context, _ system.System) (bool, error) {
	st, ok, err := s.sites.Site(ctx, s.d)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("site %s không còn trong tpan", s.d)
	}
	return ownerOf(st) == s.to, nil
}

func (s *siteOwnerStep) Apply(ctx context.Context, _ system.System) error {
	if err := s.ow.SetSiteOwner(ctx, s.d, s.to); err != nil {
		return fmt.Errorf("đổi chủ site %s: %w", s.d, err)
	}
	return nil
}

func (s *siteOwnerStep) Undo(ctx context.Context, _ system.System) error {
	if err := s.ow.SetSiteOwner(ctx, s.d, s.from); err != nil {
		return fmt.Errorf("trả chủ cũ của site %s: %w", s.d, err)
	}
	return nil
}

// PrepareSiteOwner kiểm và dựng Plan đổi chủ site d sang user to. Chỉ đọc.
// Từ chối: site không có, đang ở thùng rác, đang tạm khoá; chủ mới không có,
// đang bị khoá, hết chỗ trong gói (như tạo site mới cho họ), hay trùng chủ cũ.
func (c *Cages) PrepareSiteOwner(ctx context.Context, ow SiteOwnerStore, d validate.Domain, to string) (*Planned, error) {
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
	plan := engine.Plan{
		Kind:   "site.owner",
		Target: dom,
		Title: i18n.T(
			"Chuyển site "+dom+" từ "+from+" sang "+to+": tên phụ và backup của site đi theo; DB rời và lịch sử job của "+from+" ở lại",
			"Move site "+dom+" from "+from+" to "+to+": the site's aliases and backups follow; standalone databases and job history of "+from+" stay"),
		Steps: []engine.Step{&siteOwnerStep{sites: c.Store, ow: ow, d: d, from: from, to: to}},
	}
	if st.Caged && st.Status == site.Active {
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
		if old.Limits() != next.Limits() {
			cfg, err := sites.stack(ctx)
			if err != nil {
				return nil, err
			}
			title := plan.Title
			title.Vi += "; áp trần lồng của gói " + next.Name
			title.En += "; apply the cage limits of package " + next.Name
			sync := c.syncPlan(cfg, dom, []site.Site{st}, next.Limits(), title)
			plan.Title = sync.Title
			plan.Steps = append(plan.Steps, sync.Steps...)
			plan.Verify = sync.Verify
		}
	}
	return newPlanned(c.Sys, c.Journal, plan, nil), nil
}
