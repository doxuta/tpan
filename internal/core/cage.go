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
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

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
//   - st.Caged false: [cageAbsentPool{sharedPoolStep(st, cfg)}]: pool chung như
//     trước M17, bọc để không bao giờ ghi khi site đã có hàng rào;
//   - st.Caged true: steps.CageFiles{Layout, PHP, Limits: lim} rồi
//     steps.CageSocket{Layout, PHP} (bật).
//
// php là sitePHP(st, cfg). Chỉ dựng bước, không chạm máy.
func sitePHPSteps(st site.Site, cfg StackConfig, lim account.Limits) ([]engine.Step, error) {
	php := sitePHP(st, cfg)
	if !st.Caged {
		pool, err := sharedPoolStep(st, cfg)
		if err != nil {
			return nil, err
		}
		return []engine.Step{&cageAbsentPool{Step: pool, Layout: st.Layout()}}, nil
	}
	return []engine.Step{
		&steps.CageFiles{Layout: st.Layout(), PHP: php, Limits: lim},
		&steps.CageSocket{Layout: st.Layout(), PHP: php},
	}, nil
}

// sharedPoolStep là chỗ DUY NHẤT gọi steps.SitePool (test tĩnh
// TestKhongGoiPoolChungNgoaiHamChon): bước ghi pool chung của site st ở bản
// sitePHP(st, cfg), không có gác. Plan thường lấy pool qua sitePHPSteps (có
// gác cageAbsentPool); chỉ chỗ nào cố ý ghi pool chung trong lúc hàng rào còn
// (ra lồng) mới gọi thẳng hàm này.
func sharedPoolStep(st site.Site, cfg StackConfig) (engine.Step, error) {
	return steps.SitePool(st.Layout(), sitePHP(st, cfg))
}

// cageAbsentPool bọc bước ghi pool chung của một site ngoài lồng (soát cuối
// M17, N4). Plan được dựng trước khi engine lấy khoá toàn cục, nên một Plan
// dựng lúc site còn ngoài lồng (php.use, chạy lại site add, khôi phục, mở khoá)
// có thể chạy ngay sau một job cage.enable của admin. Không có gác thì nó ghi
// lại pool chung, đổi vhost về pool đó, và móc after ghi caged=0: site tự ra
// khỏi lồng (bất biến 7). Apply xem máy (file hàng rào, như lớp bọc lệnh) ngay
// trước khi ghi: có hàng rào thì trả lỗi, không ghi gì, và job hoàn tác.
// Info, Check, Undo đi thẳng tới bước bên trong: ID và bản xem trước không đổi.
type cageAbsentPool struct {
	engine.Step
	Layout site.Layout
}

func (p *cageAbsentPool) Apply(ctx context.Context, sys system.System) error {
	l := p.Layout
	fenced, err := pathExists(sys, l.CageFence())
	if err != nil {
		return fmt.Errorf("ghi pool chung của %s: %w", l.Domain, err)
	}
	if fenced {
		d := l.Domain.String()
		return fmt.Errorf("không ghi pool chung của %s: site đang ở trong lồng (có %s); Plan này được dựng khi site còn ở ngoài lồng, có thể ngay trước khi admin đưa nó vào lồng. Chạy lại thao tác để dựng Plan mới (vẫn báo lỗi này thì lồng của site đang dựng dở: sudo tpan health --fix, hoặc sudo tpan cage enable %s) / "+
			"not writing the shared pool of %s: the site is caged (%s exists); this Plan was built while the site was still outside the cage, possibly just before an admin caged it. Run the action again to build a new Plan (if this error repeats the site's cage is half built: sudo tpan health --fix, or sudo tpan cage enable %s)",
			d, l.CageFence(), d, d, l.CageFence(), d)
	}
	return p.Step.Apply(ctx, sys)
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

// ---------------------------------------------------------------- đọc

func (c *Cages) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Trạng thái của một site về lồng (CageRow.State).
const (
	cageStateIn      = "trong"
	cageStateOut     = "ngoai"
	cageStateWantIn  = "can-vao"
	cageStateWantOut = "can-ra"
)

func cageStateOf(caged, want bool) string {
	switch {
	case caged && want:
		return cageStateIn
	case want:
		return cageStateWantIn
	case caged:
		return cageStateWantOut
	}
	return cageStateOut
}

// Status trả trạng thái lồng của một site (d khác zero) hoặc mọi site active.
// Files đọc máy (một lần Stat hàng rào), Usage đọc cgroup khi site Caged: hai
// nguồn này có thể khác bảng sites khi panel bị tắt giữa cage.enable/disable.
func (c *Cages) Status(ctx context.Context, d validate.Domain) ([]CageRow, error) {
	var list []site.Site
	if !d.IsZero() {
		st, err := (&Sites{Store: c.Store}).find(ctx, d)
		if err != nil {
			return nil, err
		}
		list = []site.Site{st}
	} else {
		all, err := c.Store.Sites(ctx)
		if err != nil {
			return nil, fmt.Errorf("đọc danh sách site: %w", err)
		}
		for _, st := range all {
			if st.Status == site.Active {
				list = append(list, st)
			}
		}
		slices.SortFunc(list, func(a, b site.Site) int { return strings.Compare(a.Domain.String(), b.Domain.String()) })
	}
	rows := make([]CageRow, 0, len(list))
	for _, st := range list {
		row, err := c.row(ctx, st)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (c *Cages) row(ctx context.Context, st site.Site) (CageRow, error) {
	l := st.Layout()
	p, err := sitePackage(ctx, c.Accounts, st)
	if err != nil {
		return CageRow{}, fmt.Errorf("đọc gói của chủ site %s: %w", st.Domain, err)
	}
	files, err := pathExists(c.Sys, l.CageFence())
	if err != nil {
		return CageRow{}, err
	}
	owner := st.Owner
	if owner == "" {
		owner = account.AdminName
	}
	row := CageRow{
		Domain: st.Domain.String(), Owner: owner, Package: p.Name,
		Caged: st.Caged, Want: p.Cage, Files: files, State: cageStateOf(st.Caged, p.Cage),
		Suspended: st.Suspended != account.NotSuspended, Limits: p.Limits(),
	}
	if st.Caged {
		u, err := cage.ReadUsage(c.Sys, l, c.now())
		if err != nil {
			return CageRow{}, fmt.Errorf("đọc số liệu của lồng %s: %w", st.Domain, err)
		}
		row.Usage = &u
	}
	return row, nil
}

// ---------------------------------------------------------------- điều kiện đầu

// cgroupControllers chỉ có trên máy dùng cgroup v2 (hệ thống file cgroup2 gắn ở
// /sys/fs/cgroup). Trần của lồng (MemoryMax, TasksMax...) cần v2.
const cgroupControllers = "/sys/fs/cgroup/cgroup.controllers"

// cageSystemdMin: cờ --expand-environment của systemd-run (lớp bọc lệnh, mục
// 3.4) có từ systemd 254.
const cageSystemdMin = 254

// cageSite đọc site d: có (site_not_found), active (site_trashed), không tạm
// khoá (site_suspended). what là việc định làm, cho lời từ chối.
func (c *Cages) cageSite(ctx context.Context, d validate.Domain, whatVi, whatEn string) (site.Site, error) {
	st, err := (&Sites{Store: c.Store}).find(ctx, d)
	if err != nil {
		return site.Site{}, err
	}
	dom := d.String()
	if st.Status != site.Active {
		return site.Site{}, refuse("site_trashed", i18n.T(
			"site "+dom+" đang ở thùng rác; không "+whatVi+" được",
			"site "+dom+" is in the trash; cannot "+whatEn))
	}
	if err := refuseSuspendedPool(st); err != nil {
		return site.Site{}, err
	}
	return st, nil
}

// cageHostReady kiểm máy chạy được lồng: cgroup v2 và systemd từ 254.
func (c *Cages) cageHostReady(ctx context.Context) error {
	v2, err := pathExists(c.Sys, cgroupControllers)
	if err != nil {
		return err
	}
	if !v2 {
		return refuse(CodeCageCgroupV1, i18n.T(
			"máy không dùng cgroup v2 (không có "+cgroupControllers+"); lồng cần cgroup v2 để áp trần. Ubuntu 22.04 trở lên dùng cgroup v2 mặc định",
			"the machine does not use cgroup v2 (no "+cgroupControllers+"); the cage needs cgroup v2 to apply limits. Ubuntu 22.04 and later use cgroup v2 by default"))
	}
	res, err := c.Sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"--version"}})
	if err != nil {
		return fmt.Errorf("hỏi bản systemd: %w", err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(res.Stdout), "\n")
	f := strings.Fields(first)
	n := 0
	if len(f) >= 2 && f[0] == "systemd" {
		n, _ = strconv.Atoi(f[1])
	}
	if n < cageSystemdMin {
		return refuse(CodeCageSystemdOld, i18n.T(
			fmt.Sprintf("systemd của máy là %q, lồng cần systemd từ %d (cờ --expand-environment của systemd-run)", first, cageSystemdMin),
			fmt.Sprintf("the machine's systemd is %q, the cage needs systemd %d or later (systemd-run's --expand-environment flag)", first, cageSystemdMin)))
	}
	return nil
}

// cageDrift: site ngoài lồng mà file pool chung hay vhost không khớp đúng bản
// tpan sinh thì từ chối cage_drift. Ra lồng sinh lại hai file đó từ template,
// nên chỉ khi lúc vào chúng đã khớp template thì ra lồng mới trả lại đúng từng
// byte (bất biến 2). Dùng chính Check() của các bước sẽ ghi chúng.
func (c *Cages) cageDrift(ctx context.Context, st site.Site, cfg StackConfig) error {
	out := st
	out.Caged = false
	ss, err := sitePHPSteps(out, cfg, account.Limits{})
	if err != nil {
		return err
	}
	vh, err := siteVhost(out, cfg)
	if err != nil {
		return err
	}
	var bad []string
	for _, s := range append(ss, vh...) {
		ok, err := s.Check(ctx, c.Sys)
		if err != nil {
			return fmt.Errorf("so pool và vhost của %s với bản của tpan: %w", st.Domain, err)
		}
		if ok {
			continue
		}
		info := s.Info()
		if len(info.Files) == 0 {
			bad = append(bad, info.ID)
		}
		for _, f := range info.Files {
			bad = append(bad, f.Path)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	dom, list := st.Domain.String(), strings.Join(bad, ", ")
	return refuse(CodeCageDrift, i18n.T(
		"pool hoặc vhost của "+dom+" đang khác bản tpan sinh ("+list+"); ra lồng sẽ không trả lại đúng các file này. Đưa về bản của tpan trước bằng: sudo tpan site add "+dom+" (hoặc sudo tpan health --fix), rồi chạy lại",
		"the pool or vhost of "+dom+" differs from tpan's version ("+list+"); leaving the cage would not give these files back as they are. Restore tpan's version first with: sudo tpan site add "+dom+" (or sudo tpan health --fix), then run again"))
}

// ---------------------------------------------------------------- kiểm chứng

// policyCheck hạ Fail xuống Warn khi phép kiểm không thuộc tập cứng (policyHard).
type policyCheck struct{ c checks.Check }

func (p policyCheck) Run(ctx context.Context, sys system.System) checks.Result {
	r := p.c.Run(ctx, sys)
	if r.Status == checks.Fail && !policyHard(r) {
		r.Status = checks.Warn
	}
	return r
}

// policyHard là tập phép cứng của Plan chính sách: điều site không điều khiển
// được. Chặng web (cấu hình web server, vhost), socket do systemd giữ
// (cage.socket, và php.socket khi ra lồng), trần trên cgroup (cage.limits).
func policyHard(r checks.Result) bool {
	if r.Stage == checks.StageWeb {
		return true
	}
	for _, p := range []string{"cage.socket:", "cage.limits:", "php.socket:"} {
		if strings.HasPrefix(r.ID, p) {
			return true
		}
	}
	return false
}

// policyVerify bọc kiểm chứng của Plan chính sách của admin (cage.enable,
// cage.disable): Fail của phép ngoài tập cứng thành Warn (như checks.Soft nhưng
// quyết theo Result). Lý do (mục 3.6 "Hai loại Plan"): mã của site quyết định mã
// HTTP, giết được tiến trình chính của FPM, và lấp được số tiến trình của slice
// (bằng exec) làm lệnh systemd-run của cage.fence không khởi động được; hàng rào
// đã được kiểm khi `php-fpm -t` chạy trong lồng ở bước CageSocket, trước khi mã
// của site vào slice. Không có vậy thì một site trả 500 giữ được mình ngoài lồng.
func policyVerify(cs []checks.Check) []checks.Check {
	out := make([]checks.Check, 0, len(cs))
	for _, c := range cs {
		var w checks.Check = policyCheck{c}
		if checks.IsHeavy(c) {
			w = checks.Heavy(w)
		}
		out = append(out, w)
	}
	return out
}

// markCaged đọc lại dòng của site rồi chỉ đổi Caged: giữa lúc dựng Plan và lúc
// job xong, dòng có thể đã đổi trường khác (như markSuspended).
func (c *Cages) markCaged(ctx context.Context, d validate.Domain, caged bool) error {
	st, err := (&Sites{Store: c.Store}).find(ctx, d)
	if err == nil {
		st.Caged = caged
		err = c.Store.SaveSite(ctx, st)
	}
	if err != nil {
		return fmt.Errorf("ghi trạng thái lồng của site %s (máy đã đổi xong; chạy lại lệnh để ghi nốt): %w", d, err)
	}
	return nil
}

// ---------------------------------------------------------------- Plan

// PrepareEnable dựng Plan "cage.enable" (mục 3.6). after: ghi caged = true.
// Chỉ đọc máy. Site đã Caged không bị từ chối: Plan chạy lại, bước đúng tự bỏ
// qua (cách áp lại trần và sửa lệch).
func (c *Cages) PrepareEnable(ctx context.Context, d validate.Domain) (*Planned, error) {
	st, err := c.cageSite(ctx, d, "đưa vào lồng", "put it in a cage")
	if err != nil {
		return nil, err
	}
	dom := d.String()
	p, err := sitePackage(ctx, c.Accounts, st)
	if err != nil {
		return nil, fmt.Errorf("đọc gói của chủ site %s: %w", dom, err)
	}
	if !p.Cage {
		return nil, refuse(CodeCageNotInPackage, i18n.T(
			"gói "+p.Name+" của chủ site "+dom+" chưa bật lồng; bật bằng: sudo tpan package edit "+p.Name+" --cage on",
			"package "+p.Name+" of the owner of "+dom+" does not enable the cage; enable it with: sudo tpan package edit "+p.Name+" --cage on"))
	}
	if err := c.cageHostReady(ctx); err != nil {
		return nil, err
	}
	cfg, err := (&Sites{Settings: c.Settings}).stack(ctx)
	if err != nil {
		return nil, err
	}
	php := sitePHP(st, cfg)
	if err := checkPHPVersion(php); err != nil {
		return nil, fmt.Errorf("bản PHP đang ghi cho site %s: %w", dom, err)
	}
	ok, err := phpIsInstalled(c.Sys, php)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("php_not_installed", i18n.T(
			"PHP "+php+" của site "+dom+" chưa được cài; cài bằng: sudo tpan php install "+php,
			"PHP "+php+" of site "+dom+" is not installed; install it with: sudo tpan php install "+php))
	}
	if !st.Caged {
		if err := c.cageDrift(ctx, st, cfg); err != nil {
			return nil, err
		}
	}
	lim := p.Limits()
	ss, err := cageEnableSteps(st, cfg, lim)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan vào lồng cho %s: %w", dom, err)
	}
	if ss, err = c.withStalePools(ss, st.Layout(), php); err != nil {
		return nil, err
	}
	others, err := (&Sites{Store: c.Store}).others(ctx, d)
	if err != nil {
		return nil, err
	}
	in := st
	in.Caged = true
	title := i18n.T("Đưa "+dom+" vào lồng", "Put "+dom+" in a cage")
	if cfg.Web == Apache && st.Htaccess.OrDefault() == site.HtaccessFull {
		// Soát thiết kế điểm 14: Apache chạy bằng www-data nối được mọi socket
		// của lồng, và .htaccess ở chế độ đầy đủ được đặt SetHandler.
		title.Vi += " (cảnh báo: .htaccess đang ở chế độ day-du; lồng không chặn được đường chuyển request sang socket của site khác bằng .htaccess ở chế độ này)"
		title.En += " (warning: .htaccess is in day-du mode; the cage cannot block routing requests to another site's socket through .htaccess in this mode)"
	}
	plan := engine.Plan{
		Kind:   "cage.enable",
		Target: dom,
		Title:  title,
		Steps:  ss,
		Verify: policyVerify(siteChecks(cfg.Web, php, in, others, &lim)),
	}
	return newPlanned(c.Sys, c.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		return c.markCaged(ctx, d, true)
	}), nil
}

// withStalePools chèn, ngay sau bước gỡ pool chung của bản php, một bước gỡ
// pool chung của site ở mỗi bản PHP khác còn file (soát cuối M17, V3: panel bị
// tắt giữa php.use để lại pool ở bản cũ). Pool sót đó vẫn sinh worker dưới user
// của site ở ngoài lồng khi socket của nó có kết nối (từ www-data, nên một
// .htaccess day-du của site Apache nào cũng dẫn tới được), và cage.state, vốn
// xét mọi /etc/php/<bản>/fpm/pool.d, báo lỗi mãi. Bản được xét đúng như
// cage.state: mọi thư mục trong /etc/php khớp mẫu số.số. Bản chưa cài (gói đã
// gỡ, file còn) thì chỉ gỡ file: không có php-fpm để kiểm, không có dịch vụ để
// reload, và đòi lệnh đó thì job hỏng mãi.
func (c *Cages) withStalePools(ss []engine.Step, l site.Layout, php string) ([]engine.Step, error) {
	vers, err := c.Sys.ReadDir(phpConfRoot)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("đọc %s: %w", phpConfRoot, err)
	}
	var extra []engine.Step
	for _, v := range vers {
		if v == php || !phpVersionRE.MatchString(v) {
			continue
		}
		ok, err := pathExists(c.Sys, l.PoolConf(v))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		a := &steps.Absent{
			Path:  l.PoolConf(v),
			Title: i18n.T("Gỡ pool chung "+l.Pool+" còn sót ở PHP "+v, "Remove the leftover shared pool "+l.Pool+" from PHP "+v),
			Why: i18n.T(
				"Site đang dùng PHP "+php+", nhưng pool chung ở PHP "+v+" vẫn còn (sót lại sau một lần đổi bản dở dang). Để lại thì pool này vẫn chạy PHP dưới user "+l.User+" ở ngoài lồng khi có kết nối tới socket của nó. Bước này hoàn tác được.",
				"The site uses PHP "+php+", but a shared pool on PHP "+v+" is still there (left over from an interrupted version change). If it stays, it still runs PHP as "+l.User+" outside the cage whenever its socket gets a connection. This step can be undone."),
		}
		inst, err := phpIsInstalled(c.Sys, v)
		if err != nil {
			return nil, err
		}
		if inst {
			a.Test = &system.Cmd{Name: "php-fpm" + v, Args: []string{"-t"}}
			a.Reload = &system.Cmd{Name: "systemctl", Args: []string{"reload", "php" + v + "-fpm"}}
		}
		extra = append(extra, a)
	}
	if len(extra) == 0 {
		return ss, nil
	}
	for i, s := range ss {
		if a, ok := s.(*steps.Absent); ok && a.Path == l.PoolConf(php) {
			return slices.Insert(ss, i+1, extra...), nil
		}
	}
	return nil, fmt.Errorf("dựng Plan vào lồng cho %s: không thấy bước gỡ pool chung của PHP %s", l.Domain, php)
}

// PrepareDisable dựng Plan "cage.disable" (mục 3.6). after: ghi caged = false.
// Điều kiện đọc máy (có file của lồng), không đòi caged=1: đây cũng là Plan
// dọn lồng dựng dở. Không đòi cờ của gói: admin được đưa site ra để gỡ lỗi.
func (c *Cages) PrepareDisable(ctx context.Context, d validate.Domain) (*Planned, error) {
	st, err := c.cageSite(ctx, d, "đưa ra khỏi lồng", "take it out of its cage")
	if err != nil {
		return nil, err
	}
	dom, l := d.String(), st.Layout()
	has := false
	// Thư mục cấu hình của lồng cũng tính: còn nó mà mất hàng rào thì lớp bọc
	// lệnh (internal/cage) từ chối mọi lệnh của site và chỉ tới lệnh này; từ
	// chối cage_not_caged ở đây là để site kẹt mãi.
	for _, p := range append(l.CageFiles(), l.CageConfDir()) {
		ok, err := pathExists(c.Sys, p)
		if err != nil {
			return nil, err
		}
		has = has || ok
	}
	if !has {
		return nil, refuse(CodeCageNotCaged, i18n.T(
			"site "+dom+" không có file nào của lồng trên máy; không có gì để đưa ra",
			"site "+dom+" has no cage files on the machine; there is nothing to take out"))
	}
	cfg, err := (&Sites{Settings: c.Settings}).stack(ctx)
	if err != nil {
		return nil, err
	}
	out := st
	out.Caged = false
	php := sitePHP(st, cfg)
	// Gọi thẳng sharedPoolStep, không qua sitePHPSteps: Plan ra lồng ghi pool
	// chung trong lúc hàng rào còn (hàng rào chỉ gỡ ở bước CageFiles cuối, để
	// lệnh của site không chạy ngoài lồng trước khi dịch vụ của lồng dừng), nên
	// gác cageAbsentPool sẽ chặn đúng việc Plan này phải làm.
	pool, err := sharedPoolStep(out, cfg)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan ra lồng cho %s: %w", dom, err)
	}
	ss := []engine.Step{pool}
	vh, err := siteVhost(out, cfg)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan ra lồng cho %s: %w", dom, err)
	}
	ss = append(ss, vh...)
	// Pool chung và vhost thường đã phục vụ site trước khi lồng tắt: request
	// không rơi vào khoảng trống nào. Hàng rào gỡ trước tiên trong CageFiles.
	ss = append(ss,
		&steps.CageSocket{Layout: l, PHP: php, Disable: true},
		&steps.CageFiles{Layout: l, PHP: php, Remove: true},
	)
	others, err := (&Sites{Store: c.Store}).others(ctx, d)
	if err != nil {
		return nil, err
	}
	plan := engine.Plan{
		Kind:   "cage.disable",
		Target: dom,
		Title:  i18n.T("Đưa "+dom+" ra khỏi lồng", "Take "+dom+" out of its cage"),
		Steps:  ss,
		Verify: policyVerify(siteChecks(cfg.Web, php, out, others, nil)),
	}
	return newPlanned(c.Sys, c.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		return c.markCaged(ctx, d, false)
	}), nil
}

// PrepareRestart dựng Plan sửa "cage.restart" cho một site trong lồng.
func (c *Cages) PrepareRestart(ctx context.Context, d validate.Domain) (*Planned, error) {
	st, err := c.cageSite(ctx, d, "khởi động lại lồng", "restart its cage")
	if err != nil {
		return nil, err
	}
	dom := d.String()
	if !st.Caged {
		return nil, refuse(CodeCageNotCaged, i18n.T(
			"site "+dom+" không ở trong lồng; không có lồng để khởi động lại",
			"site "+dom+" is not caged; there is no cage to restart"))
	}
	l := st.Layout()
	plan := engine.Plan{
		Kind:   "cage.restart",
		Target: dom,
		Title:  i18n.T("Khởi động lại lồng của "+dom, "Restart the cage of "+dom),
		Steps:  []engine.Step{&steps.CageRestart{Layout: l}},
		Verify: []checks.Check{checks.CageRuntime(l)},
	}
	return newPlanned(c.Sys, c.Journal, plan, nil), nil
}

// syncPlan dựng Plan "cage.sync": mỗi site caged và active trong sites một bước
// CageFiles với trần lim (bước bỏ qua khi file đã đúng). Không có phép gọi HTTP:
// trần đổi qua file slice và daemon-reload, có hiệu lực trên cgroup cả khi dịch
// vụ đang chết (mục 2.4), và site không điều khiển được bước nào. Kiểm chứng:
// cage.limits từng site. Site đang dùng RAM (memory.current lúc dựng Plan) quá
// trần mới thì Title nói rõ; job vẫn chạy (chính sách của admin).
func (c *Cages) syncPlan(cfg StackConfig, target string, sites []site.Site, lim account.Limits, title i18n.Text) engine.Plan {
	plan := engine.Plan{Kind: "cage.sync", Target: target, Title: title}
	var overVi, overEn []string
	for _, st := range sites {
		if !st.Caged || st.Status != site.Active {
			continue
		}
		l := st.Layout()
		plan.Steps = append(plan.Steps, &steps.CageFiles{Layout: l, PHP: sitePHP(st, cfg), Limits: lim})
		plan.Verify = append(plan.Verify, checks.CageLimits(l, lim))
		if lim.RAMMB == 0 {
			continue
		}
		// ponytail: đọc cgroup lỗi thì bỏ câu cảnh báo, không chặn chính sách của
		// admin vì một dòng xem trước.
		if u, err := cage.ReadUsage(c.Sys, l, c.now()); err == nil && u.Running && u.MemCurrent > int64(lim.RAMMB)<<20 {
			mib := strconv.FormatInt(u.MemCurrent>>20, 10)
			overVi = append(overVi, st.Domain.String()+" đang dùng "+mib+" MiB")
			overEn = append(overEn, st.Domain.String()+" is using "+mib+" MiB")
		}
	}
	if len(overVi) > 0 {
		ram := strconv.Itoa(lim.RAMMB)
		plan.Title.Vi += " (cảnh báo: " + strings.Join(overVi, ", ") + ", cao hơn trần RAM mới " + ram + " MiB; request lớn sẽ chết khi chạm trần)"
		plan.Title.En += " (warning: " + strings.Join(overEn, ", ") + ", above the new RAM limit of " + ram + " MiB; large requests will die when they hit it)"
	}
	return plan
}

// cagedSitesWhere trả các site caged và active mà keep(st) đúng.
func (c *Cages) cagedSitesWhere(ctx context.Context, keep func(site.Site) (bool, error)) ([]site.Site, error) {
	all, err := c.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	var out []site.Site
	for _, st := range all {
		if !st.Caged || st.Status != site.Active {
			continue
		}
		ok, err := keep(st)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, st)
		}
	}
	return out, nil
}

// syncTitle là tiêu đề của cage.sync khi sửa gói hay đổi gói; gói tắt lồng thì
// nói rõ site vẫn trong lồng nhưng không còn trần.
func syncTitle(vi, en string, n int, p account.Package) i18n.Text {
	t := i18n.T(fmt.Sprintf("%s cho %d site trong lồng", vi, n), fmt.Sprintf("%s for %d caged sites", en, n))
	if !p.Cage {
		t.Vi += " (gói " + p.Name + " không bật lồng: site vẫn ở trong lồng nhưng trần về 0, tức không giới hạn; health cảnh báo cho tới khi đưa site ra bằng tpan cage disable)"
		t.En += " (package " + p.Name + " does not enable the cage: the sites stay caged but their limits become 0, that is unlimited; health warns until they are taken out with tpan cage disable)"
	}
	return t
}

// PrepareEditPackage: sửa gói p (mục 3.6). Chỉ đọc. nil, nil khi không có site
// caged và active nào dùng gói hay trần không đổi (người gọi dùng
// Accounts.EditPackage như trước, không có job). Có: Planned của Plan
// "cage.sync" dựng từ trần mới, với before = ghi dòng gói (Accounts.EditPackage)
// TRƯỚC khi engine chạy: chính sách của admin không chờ máy, và không bước nào
// của cage.sync phụ thuộc thứ site điều khiển.
func (c *Cages) PrepareEditPackage(ctx context.Context, p account.Package) (*Planned, error) {
	name, err := validate.ParsePackageName(p.Name)
	if err != nil {
		return nil, err
	}
	if err := checkPackageLimits(p); err != nil {
		return nil, err
	}
	if c.Accounts == nil {
		return nil, fmt.Errorf("sửa gói %s: thiếu bảng gói (Accounts)", name)
	}
	old, err := lookupPackage(ctx, c.Accounts, name)
	if err != nil {
		return nil, err
	}
	p.Name = name
	if old.Limits() == p.Limits() {
		return nil, nil
	}
	sites, err := c.cagedSitesWhere(ctx, func(st site.Site) (bool, error) {
		sp, err := sitePackage(ctx, c.Accounts, st)
		return sp.Name == name, err
	})
	if err != nil || len(sites) == 0 {
		return nil, err
	}
	cfg, err := (&Sites{Settings: c.Settings}).stack(ctx)
	if err != nil {
		return nil, err
	}
	plan := c.syncPlan(cfg, name, sites, p.Limits(), syncTitle("Áp trần mới của gói "+name, "Apply the new limits of package "+name, len(sites), p))
	pl := adminOwned(newPlanned(c.Sys, c.Journal, plan, nil)) // P7: Target là tên gói hay tên user
	pl.before = func(ctx context.Context) error {
		return (&Accounts{Sys: c.Sys, Store: c.Accounts, Now: c.Now}).EditPackage(ctx, p)
	}
	return pl, nil
}

// PrepareUserEdit: sửa user name (mục 3.6). Chỉ đọc. nil, nil khi ch không đổi
// gói hay trần của các site caged của user không đổi (người gọi dùng
// Accounts.EditUser). Có: Planned của "cage.sync" với trần của gói mới, before
// = Accounts.EditUser(ctx, name, ch, by) (áp mọi thay đổi của ch).
func (c *Cages) PrepareUserEdit(ctx context.Context, name string, ch UserChange, by account.Identity) (*Planned, error) {
	if ch.Package == nil {
		return nil, nil
	}
	if c.Accounts == nil {
		return nil, fmt.Errorf("sửa user %s: thiếu bảng user (Accounts)", name)
	}
	u, err := lookupUser(ctx, c.Accounts, name)
	if err != nil {
		return nil, err
	}
	pkg, err := parsePackageRef(ctx, c.Accounts, *ch.Package)
	if err != nil {
		return nil, err
	}
	if pkg == u.Package {
		return nil, nil
	}
	sites, err := c.cagedSitesWhere(ctx, func(st site.Site) (bool, error) {
		owner := st.Owner
		if owner == "" {
			owner = account.AdminName
		}
		return owner == u.Name, nil
	})
	if err != nil || len(sites) == 0 {
		return nil, err
	}
	// Mọi site của một user cùng một gói, nên site đầu cho trần cũ.
	old, err := sitePackage(ctx, c.Accounts, sites[0])
	if err != nil {
		return nil, err
	}
	next, err := lookupPackage(ctx, c.Accounts, pkg)
	if err != nil {
		return nil, err
	}
	if old.Limits() == next.Limits() {
		return nil, nil
	}
	cfg, err := (&Sites{Settings: c.Settings}).stack(ctx)
	if err != nil {
		return nil, err
	}
	plan := c.syncPlan(cfg, u.Name, sites, next.Limits(),
		syncTitle("Áp trần của gói "+pkg+" (user "+u.Name+")", "Apply the limits of package "+pkg+" (user "+u.Name+")", len(sites), next))
	pl := adminOwned(newPlanned(c.Sys, c.Journal, plan, nil)) // P7: Target là tên gói hay tên user
	pl.before = func(ctx context.Context) error {
		_, err := (&Accounts{Sys: c.Sys, Store: c.Accounts, Now: c.Now}).EditUser(ctx, name, ch, by)
		return err
	}
	return pl, nil
}
