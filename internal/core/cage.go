package core

// Ca sử dụng của lồng theo site (M17, mục 3.6 của docs/thay-doi/M17-thiet-ke.md).
// Hợp đồng đợt 0: chữ ký, tài liệu và hai hàm nhỏ đã viết (sitePackage,
// cageLimitsOf); các thân còn lại do thợ đợt 3 viết.
//
// Hai chỗ duy nhất chọn giữa "ngoài lồng" và "trong lồng":
//   - sitePHPSteps (ở đây): nơi phục vụ PHP của site (pool chung hay lồng);
//   - siteVhost (fix.go): vhost trỏ socket nào.
// Mọi Plan ghi lại PHP hay vhost của một site đi qua hai hàm này, nên không
// đường nào lén đưa một site trong lồng về pool chung (bất biến 3 và 7).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// errCageTODO là lỗi của thân hàm chưa viết (đợt 3 thay).
var errCageTODO = errors.New("lồng theo site: chưa viết (M17 đợt 3)")

// Cages gom những gì ca sử dụng của lồng cần. Dựng mới cho mỗi lệnh.
type Cages struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore
	// Accounts: user và gói (cờ lồng, trần). nil (test, xem trước khi máy chưa
	// có tpan.db): mọi site coi như thuộc gói default (không lồng, không trần).
	Accounts AccountStore
	// Journal nhận job khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
	Now     func() time.Time
}

// sitePackage trả gói của chủ site st. Accounts nil, hoặc chủ là admin chưa có
// dòng trong users: gói default (account.Package{Name: default}, không lồng).
func sitePackage(ctx context.Context, as AccountStore, st site.Site) (account.Package, error) {
	if as == nil {
		return account.Package{Name: account.DefaultPackage}, nil
	}
	owner := st.Owner
	if owner == "" {
		owner = account.AdminName
	}
	u, _, err := ownerUser(ctx, as, owner)
	if err != nil {
		return account.Package{}, err
	}
	p, ok, err := as.Package(ctx, u.Package)
	if err != nil {
		return account.Package{}, err
	}
	if !ok {
		// Gói của user đã mất khỏi bảng (dòng lạ): coi như default, không đoán trần.
		return account.Package{Name: account.DefaultPackage}, nil
	}
	return p, nil
}

// cageLimitsOf là trần của site st theo gói của chủ, kèm cờ "gói muốn lồng".
func cageLimitsOf(ctx context.Context, as AccountStore, st site.Site) (account.Limits, bool, error) {
	if as == nil && st.Caged {
		// Không có bảng gói mà site đang trong lồng: trả trần 0 thì bước nào ghi
		// lại file của lồng cũng xoá trần của site (bất biến 7). Báo lỗi để chỗ
		// dựng ca sử dụng quên Accounts lộ ra ngay. Máy chưa có tpan.db thì không
		// có site nào caged, nên xem trước khi chưa có DB không gặp lỗi này.
		return account.Limits{}, false, fmt.Errorf("site %s đang ở trong lồng nhưng không có bảng gói để đọc trần (thiếu Accounts)", st.Domain)
	}
	p, err := sitePackage(ctx, as, st)
	if err != nil {
		return account.Limits{}, false, err
	}
	return p.Limits(), p.Cage, nil
}

// siteCageLimits là đối số lim của siteChecks cho site st: nil khi st ở ngoài
// lồng; trỏ tới trần của gói khi st.Caged. Mọi chỗ gọi siteChecks và
// siteStateChecks lấy trần qua đây (một cách đọc gói duy nhất).
func siteCageLimits(ctx context.Context, as AccountStore, st site.Site) (*account.Limits, error) {
	if !st.Caged {
		return nil, nil
	}
	lim, _, err := cageLimitsOf(ctx, as, st)
	if err != nil {
		return nil, fmt.Errorf("đọc trần của gói cho site %s: %w", st.Domain, err)
	}
	return &lim, nil
}

// sitePHPSteps là các bước dựng nơi phục vụ PHP của site st (mục 3.6):
//   - st.Caged false: [steps.SitePool(l, php)] như trước M17;
//   - st.Caged true: steps.CageFiles{Layout, PHP, Limits: lim} rồi
//     steps.CageSocket{Layout, PHP} (bật).
//
// php là sitePHP(st, cfg). Chỉ dựng bước, không chạm máy.
func sitePHPSteps(st site.Site, cfg StackConfig, lim account.Limits) ([]engine.Step, error) {
	php := sitePHP(st, cfg)
	if !st.Caged {
		pool, err := steps.SitePool(st.Layout(), php)
		if err != nil {
			return nil, err
		}
		return []engine.Step{pool}, nil
	}
	return []engine.Step{
		&steps.CageFiles{Layout: st.Layout(), PHP: php, Limits: lim},
		&steps.CageSocket{Layout: st.Layout(), PHP: php},
	}, nil
}

// cageEnableSteps là các bước của cage.enable (mục 3.6), dùng chung cho
// PrepareEnable và phép thử tấn công (probe.go), theo đúng thứ tự:
//
//  1. CageFiles với trần lim (hàng rào ghi sau cùng);
//  2. CageSocket bật (socket nghe; dịch vụ chưa chạy; vhost còn trỏ pool chung);
//  3. gỡ pool chung của site (php-fpm -t, reload: tiến trình con của pool đó dừng);
//  4. CageReap: giết tiến trình cùng uid còn ở ngoài slice;
//  5. vhost bản trong lồng qua siteVhost (từ đây request vào socket của lồng).
//
// Bước 3 đến 5 nằm trước request đầu tiên vào lồng, nên lúc mã của site bắt đầu
// chạy trong lồng thì không còn tiến trình cùng uid nào ở ngoài để mở
// /proc/<pid>/root (t14 P4). st là bản ghi như đang có (Caged false với site
// chưa vào lồng); hàm tự dùng bản sao Caged true. Site đã ở trong lồng thì bước
// 3 tự bỏ qua (file pool không có). Chỉ dựng bước, không chạm máy.
func cageEnableSteps(st site.Site, cfg StackConfig, lim account.Limits) ([]engine.Step, error) {
	in := st
	in.Caged = true
	l, php := st.Layout(), sitePHP(st, cfg)
	ss, err := sitePHPSteps(in, cfg, lim)
	if err != nil {
		return nil, err
	}
	ss = append(ss,
		&steps.Absent{
			Path:   l.PoolConf(php),
			Test:   &system.Cmd{Name: "php-fpm" + php, Args: []string{"-t"}},
			Reload: &system.Cmd{Name: "systemctl", Args: []string{"reload", "php" + php + "-fpm"}},
			Title:  i18n.T("Gỡ pool chung "+l.Pool+" ở PHP "+php, "Remove the shared pool "+l.Pool+" from PHP "+php),
			Why: i18n.T(
				"Site đã có PHP-FPM riêng trong lồng. Pool chung không còn ai dùng mà vẫn là một cửa chạy PHP dưới user "+l.User+" ở ngoài lồng. Từ bước này tới lúc vhost đổi sang socket của lồng, request của site được trả 502 trong chốc lát. Bước này hoàn tác được.",
				"The site now has its own PHP-FPM inside the cage. The shared pool is unused but is still a way to run PHP as "+l.User+" outside the cage. From this step until the vhost points to the cage socket, the site's requests get a 502 for a moment. This step can be undone."),
		},
		&steps.CageReap{Layout: l},
	)
	vh, err := siteVhost(in, cfg)
	if err != nil {
		return nil, err
	}
	return append(ss, vh...), nil
}

// Status trả trạng thái lồng của một site (d khác zero) hoặc mọi site active.
func (c *Cages) Status(ctx context.Context, d validate.Domain) ([]CageRow, error) {
	return nil, errCageTODO
}

// PrepareEnable dựng Plan "cage.enable" (mục 3.6). after: ghi caged = true.
func (c *Cages) PrepareEnable(ctx context.Context, d validate.Domain) (*Planned, error) {
	return nil, errCageTODO
}

// PrepareDisable dựng Plan "cage.disable" (mục 3.6). after: ghi caged = false.
func (c *Cages) PrepareDisable(ctx context.Context, d validate.Domain) (*Planned, error) {
	return nil, errCageTODO
}

// PrepareRestart dựng Plan sửa "cage.restart" cho một site trong lồng.
func (c *Cages) PrepareRestart(ctx context.Context, d validate.Domain) (*Planned, error) {
	return nil, errCageTODO
}

// PrepareEditPackage: sửa gói p (mục 3.6). Chỉ đọc. nil, nil khi không có site
// caged và active nào dùng gói hay trần không đổi (người gọi dùng
// Accounts.EditPackage như trước, không có job). Có: Planned của Plan
// "cage.sync" dựng từ trần mới, với before = ghi dòng gói (Accounts.EditPackage)
// TRƯỚC khi engine chạy: chính sách của admin không chờ máy, và không bước nào
// của cage.sync phụ thuộc thứ site điều khiển.
func (c *Cages) PrepareEditPackage(ctx context.Context, p account.Package) (*Planned, error) {
	return nil, errCageTODO
}

// PrepareUserEdit: sửa user name (mục 3.6). Chỉ đọc. nil, nil khi ch không đổi
// gói hay trần của các site caged của user không đổi (người gọi dùng
// Accounts.EditUser). Có: Planned của "cage.sync" với trần của gói mới, before
// = Accounts.EditUser(ctx, name, ch, by) (áp mọi thay đổi của ch).
func (c *Cages) PrepareUserEdit(ctx context.Context, name string, ch UserChange, by account.Identity) (*Planned, error) {
	return nil, errCageTODO
}
