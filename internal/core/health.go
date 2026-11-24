package core

// Hợp đồng M4: phần của core mà health và sửa tự động cần (mục 6.7 của
// docs/KE-HOACH.md). Thân hàm do thợ của phần core viết.
//
//   - Stack.Checks, Sites.Checks: trả các phép kiểm (chưa chạy) để gói health
//     chạy theo lịch, ghi kết quả, mở đóng sự cố. Cùng bộ phép kiểm với kiểm
//     chứng sau khi cài, tạo (stackChecks, siteChecks), để "lúc tạo đã đúng"
//     và "bây giờ còn đúng" là một định nghĩa.
//   - AutoFixName: tên cách sửa tự động cho một kết quả không đạt; health điền
//     vào Result.AutoFix trước khi ghi, giao diện chỉ hiện nút "Sửa tự động"
//     khi trường này khác rỗng.
//   - Fixes: dựng Plan sửa từ một kết quả, rồi xem trước hoặc chạy như mọi
//     thao tác khác (xem trước, xác nhận, kiểm chứng, hoàn tác).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Checks trả bộ phép kiểm của stack (stackChecks dựng từ settings).
// installed=false (chưa có stack.php_version) thì không trả phép kiểm nào.
func (s *Stack) Checks(ctx context.Context) (cs []checks.Check, installed bool, err error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, false, err
	}
	if cfg.PHPVersion == "" {
		return nil, false, nil
	}
	_, sha := s.wpcli()
	cs = stackChecks(cfg.Web, func() string { return cfg.PHPVersion }, sha)
	more, err := s.isolationChecks(ctx, cfg)
	if err != nil {
		return nil, false, err
	}
	return append(cs, more...), true, nil
}

// isolationChecks là các phép kiểm cấp máy của M14 (mục 6.13) mà chỉ health
// xét, vì chúng cần biết cả máy đang có gì: OPcache và ProtectProc của các bản
// PHP ngoài bản mặc định (bản mặc định đã nằm trong stackChecks), tên miền
// trùng giữa các vhost, và vhost mặc định từ chối tên lạ (thêm cổng 443 khi có
// site bật SSL). Sites nil (lệnh chỉ cần phép kiểm của stack) thì bỏ hai phép
// định tuyến.
//
// M28 đợt 2 (H5, S15): web.mod_status khi stack là Apache. Ở đây (health và
// kiểm chứng của harden), KHÔNG ở stackChecks: admin cố ý bật lại mod_status
// để giám sát thì `tpan update` vẫn chạy được.
func (s *Stack) isolationChecks(ctx context.Context, cfg StackConfig) ([]checks.Check, error) {
	var cs []checks.Check
	if cfg.Web == Apache {
		cs = append(cs, checks.ApacheModStatus())
	}
	if s.Sys != nil {
		vers, err := phpInstalled(s.Sys)
		if err != nil {
			return nil, err
		}
		for _, v := range vers {
			if v != cfg.PHPVersion {
				cs = append(cs, checks.OPcacheHardened(v), checks.PHPProtectProc(v))
			}
		}
	}
	if s.Sites == nil {
		return cs, nil
	}
	all, err := s.Sites.Sites(ctx)
	if err != nil {
		return nil, err
	}
	var named []checks.NamedSite
	var owned []checks.OwnedNames
	ssl := false
	for _, st := range all {
		// M19: mọi site (cả thùng rác) giữ tên của nó, nên names.root.shared xét hết.
		owned = append(owned, checks.OwnedNames{Site: st.Domain, Owner: ownerOf(st), Names: siteAllNames(st)})
		if st.Status != site.Active {
			continue
		}
		named = append(named, checks.NamedSite{Layout: st.Layout(), Names: siteAliasNames(st)})
		ssl = ssl || st.SSL
	}
	return append(cs,
		checks.ServerNameUnique(string(cfg.Web), named),
		checks.DefaultRejects(string(cfg.Web), ssl),
		checks.NamesRootShared(owned),
	), nil
}

// siteAliasNames là các tên phụ active của st (phục vụ rồi chuyển, đúng thứ
// tự của store).
func siteAliasNames(st site.Site) []validate.Domain {
	out := make([]validate.Domain, 0, len(st.Names.Serve)+len(st.Names.Redirect))
	out = append(out, st.Names.Serve...)
	return append(out, st.Names.Redirect...)
}

// siteAllNames là tên chính rồi các tên phụ active của st.
func siteAllNames(st site.Site) []validate.Domain {
	return append([]validate.Domain{st.Domain}, siteAliasNames(st)...)
}

// Checks trả bộ phép kiểm của site d (siteChecks: đường đi request, rồi cô
// lập hai chiều với từng site active khác). Stack chưa cài: nil, không lỗi.
// Site không có: lỗi như find (RefusedError site_not_found).
func (s *Sites) Checks(ctx context.Context, d validate.Domain) ([]checks.Check, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	cfg, err := (&Stack{Settings: s.Settings}).Config(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Web == "" || cfg.PHPVersion == "" {
		return nil, nil
	}
	others, err := s.others(ctx, d)
	if err != nil {
		return nil, err
	}
	vers, err := healthPHPVers(s.Sys)
	if err != nil {
		return nil, err
	}
	lim, err := siteCageLimits(ctx, s.Accounts, st)
	if err != nil {
		return nil, err
	}
	cs := append(siteEdgeChecks(st.Layout(), st.SSL), siteNameEdgeChecks(st)...)
	cs = append(cs, siteHealthChecks(st, cfg, others, lim, vers)...)
	state, err := s.cageStateCheck(ctx, st)
	if err != nil {
		return nil, err
	}
	if state != nil {
		cs = append(cs, state)
	}
	return cs, nil
}

// cageStateCheck trả checks.CageState cho site có caged=1, gói muốn lồng, hoặc
// có file hàng rào trên máy (một lần Stat); nil khi không có điều nào (site
// thường của máy nâng từ v0.2.0: không thêm phép kiểm nào, mục 6 của thiết kế
// M17). Có ở mọi nhánh, kể cả site tạm khoá. Stat lỗi khác "không có" thì vẫn
// sinh: phép kiểm tự báo không đọc được.
func (s *Sites) cageStateCheck(ctx context.Context, st site.Site) (checks.Check, error) {
	p, err := sitePackage(ctx, s.Accounts, st)
	if err != nil {
		return nil, fmt.Errorf("đọc gói của chủ site %s: %w", st.Domain, err)
	}
	l := st.Layout()
	fence := false
	if s.Sys != nil {
		_, err := s.Sys.Stat(l.CageFence())
		fence = !errors.Is(err, fs.ErrNotExist)
	}
	if !st.Caged && !p.Cage && !fence {
		return nil, nil
	}
	return checks.CageState(l, st.Caged, p.Cage), nil
}

// siteStateChecks là bộ phép kiểm của site theo trạng thái tạm khoá (M13, mục
// 6.12). Dùng cho health (Sites.Checks) và cho kiểm chứng của mở khoá site, để
// hai chỗ cùng một định nghĩa:
//
//   - Site tạm khoá: TLSCert (khi có SSL), VhostEnabled, một kết quả "tạm
//     khoá" (checks.SuspendedStage) cho mỗi chặng PHP, WordPress, DB, rồi
//     http.suspended (403 đúng trang tạm khoá) ở chặng đầu cuối. Không có phép
//     kiểm cô lập: cô lập hai chiều với site này vẫn nằm trong bộ của mỗi site
//     còn lại (others gồm cả site đang khoá).
//   - DB của site đang khoá (site không khoá): như site thường, trừ chặng DB
//     và đầu cuối là "tạm khoá": WordPress báo lỗi kết nối DB là điều đã biết,
//     không phải sự cố.
//   - Còn lại: siteChecks như trước M13.
//
// verify=true (kiểm chứng sau thao tác): bỏ các kết quả "tạm khoá", vì chúng
// không kiểm gì. Các chặng DNS, TLS khi chưa có SSL (siteEdgeChecks) chỉ health
// thêm, như trước.
//
// lim (M17) là trần của lồng khi st.Caged (siteCageLimits), nil khi ngoài lồng;
// đi thẳng vào siteChecks.
//
// siteHealthChecks là bộ của health: siteStateChecks (verify=false) cộng ba
// phép chỉ health xét của M28 đợt 2, ở cuối: php.pool (S8: file pool của site
// ở mọi bản PHP đang cài, phpVers), web.docroot_guard (S5: Plan không ghi vhost
// thì không làm hỏng nó vì vhost cũ), cả hai chỉ cho site không tạm khoá (site
// tạm khoá không có pool và vhost của nó không trỏ vào public); db.user_limit
// khi cả site lẫn DB của nó không tạm khoá.
func siteHealthChecks(st site.Site, cfg StackConfig, others []site.Site, lim *account.Limits, phpVers []string) []checks.Check {
	cs := siteStateChecks(st, cfg, others, lim, false)
	l := st.Layout()
	if st.Suspended == account.NotSuspended {
		// php.pool kiểm file pool CHUNG của site (tpan-<slug>.conf trong pool.d);
		// site trong lồng không có file đó (pool của nó là file của lồng, do
		// cage.state, cage.limits kiểm), nên với nó phép này chỉ báo "chưa kiểm được".
		if !st.Caged {
			cs = append(cs, checks.PHPPool(l, phpVers))
		}
		cs = append(cs, checks.DocrootGuard(string(cfg.Web), l))
	}
	// db.user_limit ở chặng DB: site hay DB đang tạm khoá thì chặng DB chỉ hiện
	// "tạm khoá" xám (M13), nên không xét; harden vẫn đặt trần cho user khoá, và
	// mở khoá xong thì health xét lại.
	if st.Suspended == account.NotSuspended && st.DBSuspended == account.NotSuspended {
		cs = append(cs, checks.DBUserLimit(l.DB))
	}
	return cs
}

// healthPHPVers là các bản PHP đang cài cho php.pool; nil khi không có System
// (lệnh chỉ dựng phép kiểm, như isolationChecks).
func healthPHPVers(sys system.System) ([]string, error) {
	if sys == nil {
		return nil, nil
	}
	return phpInstalled(sys)
}

func siteStateChecks(st site.Site, cfg StackConfig, others []site.Site, lim *account.Limits, verify bool) []checks.Check {
	web, php, l := string(cfg.Web), sitePHP(st, cfg), st.Layout()
	var cs []checks.Check
	hold := func(stage checks.Stage, db bool) {
		if !verify {
			cs = append(cs, checks.SuspendedStage(stage, l, db))
		}
	}
	switch {
	case st.Suspended != account.NotSuspended:
		if st.SSL {
			cs = append(cs, checks.TLSCert(l))
		}
		cs = append(cs, checks.VhostEnabled(web, l), checks.LogPerms(l), checks.SiteDocroot(l))
		hold(checks.StagePHP, false)
		hold(checks.StageWordPress, false)
		hold(checks.StageDB, false)
		return append(cs, checks.HTTPSuspended(l, st.SSL))
	case st.DBSuspended != account.NotSuspended:
		if st.SSL {
			cs = append(cs, checks.TLSCert(l))
		}
		cs = append(cs, checks.VhostEnabled(web, l))
		// M17 (mục 3.6): site trong lồng không có pool chung; chặng PHP của nó
		// là socket và tiến trình chính của lồng.
		if st.Caged {
			cs = append(cs, checks.CageSocket(l), checks.CageRuntime(l))
		} else {
			cs = append(cs, checks.PoolSocket(l, php))
		}
		cs = append(cs,
			checks.Heavy(checks.WPChecksums(l)),
			checks.WPConfigPerm(l),
		)
		cs = append(cs, siteHardeningChecks(cfg.Web, l, st.Htaccess)...)
		hold(checks.StageDB, true)
		hold(checks.StageEndToEnd, true)
		// Cùng luật với siteChecks (M17): không sinh Isolated từ site trong lồng.
		for _, o := range others {
			if !st.Caged {
				cs = append(cs, checks.Isolated(l, o.Layout()))
			}
			if !o.Caged {
				cs = append(cs, checks.Isolated(o.Layout(), l))
			}
		}
		return cs
	}
	return siteChecks(cfg.Web, php, st, others, lim)
}

// siteEdgeChecks là hai chặng đầu của đường đi request mà chỉ health xét: DNS,
// và TLS khi site chưa xin SSL (site có SSL thì TLSCert đã nằm trong
// siteChecks). Không đưa vào kiểm chứng sau thao tác: DNS do nhà cung cấp tên
// miền quản lý, hỏng ở đó không được làm hoàn tác một lần đổi web server.
//   - DNS luôn là Warn (checks.Soft): bản ghi nằm ở nhà cung cấp tên miền,
//     tpan không sửa được, nên không mở sự cố và không làm mọi lệnh khác thoát 1
//     (e2e M10: `stack switch` thành công vẫn thoát 1 vì DNS của site SSL).
//     Site SSL mà DNS hỏng lâu thì gia hạn hỏng và tls.cert báo lỗi đúng lúc.
//   - Site chưa có SSL: thiếu chứng chỉ là Warn: việc còn phải làm.
//
// DNS là phép kiểm nặng (gọi resolver, có thể chờ timeout): chạy mỗi giờ, khi
// gõ `tpan health` và khi bấm kiểm lại, không ở vòng 5 phút (mục 6.7).
func siteEdgeChecks(l site.Layout, ssl bool) []checks.Check {
	dns := checks.Heavy(checks.Soft(checks.DNSPointsHere(l)))
	if ssl {
		return []checks.Check{dns}
	}
	return []checks.Check{dns, checks.Soft(checks.TLSCert(l))}
}

// siteNameEdgeChecks (M19, mục 3.8) là phần của chặng DNS và TLS cho tên phụ
// mà chỉ health xét, cùng lý do với siteEdgeChecks: DNS do nhà cung cấp tên
// miền quản lý, hỏng ở đó không được làm hoàn tác một thao tác. Cả hai đều
// chỉ Warn (B4) và đều nặng (gọi resolver, mỗi tên tối đa 2 giây).
//   - site.alias.dns:<tên> cho mỗi tên phụ active;
//   - ssl.names:<tên chính> khi site có SSL và có (hoặc từng có) tên phụ: site
//     chỉ có tên chính thì chứng chỉ chỉ có tên chính, không có gì để so, và
//     site cũ giữ nguyên bộ phép kiểm như trước M19.
//
// site.alias.http nằm trong siteChecks (đường đi của request), không ở đây.
func siteNameEdgeChecks(st site.Site) []checks.Check {
	var cs []checks.Check
	for _, n := range siteAliasNames(st) {
		cs = append(cs, checks.Heavy(checks.AliasDNS(st.Layout(), n)))
	}
	if st.SSL && (len(siteAliasNames(st)) > 0 || len(st.Names.Covered) > 1) {
		cs = append(cs, checks.Heavy(checks.SSLNames(st.Layout(), siteAllNames(st))))
	}
	return cs
}

// siteAliasHTTPChecks (M19, mục 3.8) là site.alias.http:<tên> cho mỗi tên phụ
// active của st, cùng luật đạt với bước steps.NameProbe lúc thêm tên. Chỉ Warn
// (B4). Là phép kiểm nặng (một request tới PHP của site cho mỗi tên): nằm ở
// siteChecks để kiểm chứng sau thao tác dùng chung, health chỉ chạy nó mỗi giờ.
// Site chưa có tên phụ: không có phép kiểm nào.
func siteAliasHTTPChecks(st site.Site) []checks.Check {
	var cs []checks.Check
	l := st.Layout()
	for _, n := range st.Names.Serve {
		cs = append(cs, checks.Heavy(checks.AliasHTTP(l, n, site.ModeServe, st.SSL, st.Names.Covered)))
	}
	for _, n := range st.Names.Redirect {
		cs = append(cs, checks.Heavy(checks.AliasHTTP(l, n, site.ModeRedirect, st.SSL, st.Names.Covered)))
	}
	return cs
}

// Tên các cách sửa tự động (giá trị của checks.Result.AutoFix). Đối tượng
// cần sửa đọc từ Result.ID ("<loại>:<đối tượng>").
const (
	// "service.active:<unit>" Fail: steps.Service (systemctl enable --now).
	FixServiceStart = "service.start"
	// "php.socket:<domain>" Fail: khởi động lại PHP-FPM của site để socket
	// được tạo lại (steps.PHPFPMRestart). Danger: mọi site gián đoạn vài giây.
	FixPHPRestart = "php.restart"
	// "drift:<path>" Fail: ghi lại file đó từ template (đúng Step
	// File của Plan đã ghi nó: SitePool, SiteVhost, SiteHtaccess, hay file của
	// stack), có kiểm cú pháp và reload như lúc tạo. Chỉ có khi core dựng lại
	// được nội dung của path (file của tpan setup như unit systemd thì không).
	FixFileRewrite = "file.rewrite"
	// "config.nginx:nginx" | "config.apache:apache2" Fail: ghi lại mọi file
	// web của tpan (site mặc định, vhost của mọi site active). Step File bỏ
	// qua file đã đúng nội dung, nên chỉ file bị sửa hỏng được ghi.
	FixWebRewrite = "web.rewrite"
	// "wp.config:<domain>" Fail: đưa wp-config.php về đúng quyền và chủ mà
	// checks.WPConfigPerm đòi.
	FixWPConfigPerm = "wp.config.perm"
	// "db.access:<domain>" Fail: mở khoá user DB của site (steps.DBUserUnlock).
	// Phép kiểm DBAccess Fail vì lý do khác (sai mật khẩu, mất DB) thì Plan
	// này không đổi gì và kiểm chứng vẫn Fail: job lỗi, người dùng sửa tay.
	FixDBUnlock = "db.unlock"
	// "tls.cert:<domain>" Fail (site đã có SSL mà chứng chỉ mất, hỏng, sắp
	// hết hạn): xin lại chứng chỉ bằng đúng Plan của `tpan ssl issue`, kể cả
	// điều kiện đầu (email, DNS). Site chưa từng xin SSL chỉ có Warn nên không
	// có nút này: xin chứng chỉ lần đầu là một quyết định, không phải sửa lỗi.
	FixSSLIssue = "ssl.issue"
	// "php.pool:<domain>" Fail (M28 đợt 2, S8): file pool của site bị sửa tay
	// (listen TCP, user khác, quyền socket khác): ghi lại pool của bản PHP site
	// đang dùng từ template, có kiểm cú pháp và reload như lúc tạo.
	FixPHPPool = "php.pool.rewrite"
	// "web.mod_status:apache2" Fail (D2 của soát cuối đợt 2): tắt mod_status
	// bằng đúng bước của harden (steps.ApacheModOff), Verify chỉ
	// ApacheModStatus. Không phải chạy lại cả harden (một site xấu không làm
	// được job này lỗi). Chỉ khi stack là Apache.
	FixApacheModOff = "apache.mod-off"
	// "db.user_limit:<user>" Warn (D2): user DB của một site active hay một
	// DB rời active không có trần: đặt trần site.DBMaxUserConnections bằng
	// steps.DBUserLimit (cắt luôn kết nối đang mở), Verify chỉ DBUserLimit.
	// Trần đã có mà lớn hơn max_connections: không sửa tự động (tpan không
	// đè trần admin đã chọn).
	FixDBUserLimit = "db.user-limit"
)

// AutoFixName trả tên cách sửa tự động cho r, rỗng khi không có (Pass,
// Unknown, hoặc loại phép kiểm không có cách sửa tự động như disk.usage,
// http.site). Hàm thuần: không chạm máy, không đọc settings.
func AutoFixName(r checks.Result) string {
	kind, _, _ := strings.Cut(r.ID, ":")
	switch r.Status {
	case checks.Fail:
	case checks.Warn:
		// Từ đợt 2 lệch là Fail; vẫn nhận Warn để kết quả cũ trong DB còn sửa được.
		if kind == "drift" {
			return FixFileRewrite
		}
		// M14: ProtectProc thiếu chỉ là cảnh báo nhưng có sửa tự động (mục 6.13).
		if kind == "php.protectproc" {
			return FixPHPHarden
		}
		switch kind {
		case "cage.runtime":
			// M17: đang chạy bản PHP đã bị thay, hay hàng rào mới hơn lúc khởi động.
			return FixCageRestart
		case "cage.state":
			return cageStateFix(r)
		case "db.user_limit":
			// M28 đợt 2: thiếu trần kết nối chỉ là cảnh báo nhưng có sửa tự động.
			return FixDBUserLimit
		}
		return ""
	default: // Pass, Unknown: không có gì để sửa, hoặc chưa biết sửa gì.
		return ""
	}
	switch kind {
	case "service.active":
		return FixServiceStart
	case "php.socket":
		return FixPHPRestart
	case "drift":
		return FixFileRewrite
	case "config.nginx", "config.apache":
		return FixWebRewrite
	case "web.vhost":
		// Vhost của một site mất, bị thay, hay chưa bật (symlink ở
		// sites-enabled bị xoá): ghi lại vhost và symlink của riêng site đó
		// (Fixes.vhostRewrite), Verify là chính web.vhost.
		return FixWebRewrite
	case "wp.config":
		return FixWPConfigPerm
	case "db.access":
		return FixDBUnlock
	case "tls.cert":
		return FixSSLIssue
	case "php.opcache":
		return FixPHPHarden
	case "log.perm":
		return FixLogPerms
	case "web.hardened", "web.docroot_guard":
		// Vhost thiếu dòng chặn của M14 hay dấu của bản vá F4 (M28 đợt 2): ghi
		// lại vhost của riêng site đó từ template hiện tại, theo chế độ
		// .htaccess đang lưu (Fixes.vhostRewrite).
		return FixWebRewrite
	case "php.pool":
		return FixPHPPool
	case "web.mod_status":
		return FixApacheModOff
	// M17 (mục 3.7 của docs/thay-doi/M17-thiet-ke.md).
	case "cage.socket":
		return FixCageStart
	case "cage.runtime":
		return FixCageRestart
	case "cage.fence", "cage.limits":
		return FixCageSync
	case "cage.state":
		return cageStateFix(r)
	case "probe.leftover":
		return FixProbeCleanup
	}
	return ""
}

// cageStateFix chọn hướng sửa của cage.state từ chính kết quả: checks.CageState
// đặt lệnh hướng sửa (`tpan cage enable|disable <domain>`) ở dòng đầu của
// ManualFix, đúng bảng của mục 3.7 (Fail: caged=1 thì enable, caged=0 thì
// disable; Warn: gói muốn lồng mà site ngoài thì enable, ngược lại disable).
// Nhờ vậy AutoFixName vẫn thuần. Dòng đầu không khớp: không có sửa tự động.
func cageStateFix(r checks.Result) string {
	_, obj, _ := strings.Cut(r.ID, ":")
	if len(r.ManualFix) == 0 || obj == "" {
		return ""
	}
	switch r.ManualFix[0] {
	case system.Cmd{Name: "tpan", Args: []string{"cage", "enable", obj}}.String():
		return FixCageEnable
	case system.Cmd{Name: "tpan", Args: []string{"cage", "disable", obj}}.String():
		return FixCageDisable
	}
	return ""
}

// Fixes gom những gì sửa tự động cần. Dựng mới cho mỗi lệnh, như Stack, Sites.
type Fixes struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore
	// Journal nhận job khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
	// WPCLISHA512 như Stack (dùng khi dựng lại file của stack).
	WPCLISHA512 string
	// Files đọc sha256 tpan đang coi là chuẩn của một file (managed_files).
	// web.rewrite dùng nó để không ghi đè file người dùng đã nhận bản sửa tay
	// làm chuẩn. nil: không lọc, mọi file web khác template đều được ghi lại.
	Files FileRecords
	// Accounts (M17): cờ lồng và trần của gói cho các cách sửa của lồng
	// (cage.sync, cage.enable...). nil: coi như gói default (không lồng, trần 0).
	Accounts AccountStore
	// Runs (M17): sổ phép thử tấn công, cho cách sửa probe.cleanup. nil: từ
	// chối no_auto_fix.
	Runs ProbeStore
}

// FileRecords là phần của store mà sửa tự động cần từ bảng managed_files.
// *store.Store thoả giao diện này.
type FileRecords interface {
	// FileSHA256 trả sha256 (hex) đang ghi cho path; ok=false khi không có dòng.
	FileSHA256(ctx context.Context, path string) (sha string, ok bool, err error)
}

// Fix là một lần sửa đã dựng Plan. Dùng cho đúng một lần Execute.
type Fix struct {
	// CheckID là Result.ID của phép kiểm được sửa; Name là cách sửa.
	CheckID string
	Name    string
	Plan    engine.Plan

	fixes *Fixes
	// ssl có khi Name là FixSSLIssue: Execute đi qua SSLIssue.Execute để job
	// thành công thì bảng sites ghi site.SSL = true như `tpan ssl issue`.
	ssl *SSLIssue
	// planned (M17) có khi Plan có after (cage.enable, cage.disable ghi
	// sites.caged; probe.cleanup đánh dấu lượt thử đã dọn): Execute đi qua
	// Planned.Execute để after không bị bỏ.
	planned *Planned
}

// Prepare dựng Plan sửa cho r theo r.AutoFix (gọi lại AutoFixName(r) nếu
// trường rỗng). Plan.Kind là "fix." + Name, Plan.Target là đối tượng (domain,
// unit, hay đường dẫn), Plan.Verify chứa lại đúng phép kiểm r (dựng lại từ ID)
// để job chỉ thành công khi phép kiểm đã đạt (Drift sau khi ghi lại phải Pass).
// Không có cách sửa tự động: *RefusedError code "no_auto_fix".
func (f *Fixes) Prepare(ctx context.Context, r checks.Result) (*Fix, error) {
	name := AutoFixName(r)
	// r.AutoFix khác tên suy từ ID và Status thì không tin: tên sửa phải khớp
	// đúng phép kiểm, không để người gọi chọn một cách sửa cho đối tượng khác.
	if name == "" || (r.AutoFix != "" && r.AutoFix != name) {
		return nil, noAutoFix(r.ID)
	}
	if name == FixSSLIssue {
		return f.sslIssue(ctx, r)
	}
	switch name {
	case FixCageEnable, FixCageDisable, FixProbeCleanup:
		pl, err := f.plannedFix(ctx, name, r)
		if err != nil {
			return nil, err
		}
		pl.Plan.Kind = "fix." + name
		return &Fix{CheckID: r.ID, Name: name, Plan: pl.Plan, fixes: f, planned: pl}, nil
	}
	plan, err := f.plan(ctx, name, r)
	if err != nil {
		return nil, err
	}
	plan.Kind = "fix." + name
	return &Fix{CheckID: r.ID, Name: name, Plan: plan, fixes: f}, nil
}

// sslIssue dựng sửa "ssl.issue": dùng nguyên SSL.PrepareIssue (điều kiện đầu,
// Plan, kiểm chứng) để sửa tự động và `tpan ssl issue` là một việc. Lời từ
// chối của nó (thiếu email, DNS chưa trỏ về) đi thẳng ra người dùng.
func (f *Fixes) sslIssue(ctx context.Context, r checks.Result) (*Fix, error) {
	_, obj, _ := strings.Cut(r.ID, ":")
	d, err := validate.ParseDomain(obj)
	if err != nil {
		return nil, noAutoFix(r.ID)
	}
	issue, err := (&SSL{Sys: f.Sys, Settings: f.Settings, Store: f.Store, Journal: f.Journal, Accounts: f.Accounts}).PrepareIssue(ctx, d)
	if err != nil {
		return nil, err
	}
	issue.Plan.Kind = "fix." + FixSSLIssue
	return &Fix{CheckID: r.ID, Name: FixSSLIssue, Plan: issue.Plan, fixes: f, ssl: issue}, nil
}

// Preview xem trước Plan sửa (engine.Preview, có diff cho file).
func (x *Fix) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(x.fixes.Sys, engine.Options{}).Preview(ctx, x.Plan)
}

// Execute chạy Plan sửa qua engine với Journal của Fixes. Engine ghi sha256
// mới của file đã ghi vào managed_files, nên phép kiểm lệch đạt lại.
func (x *Fix) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	f := x.fixes
	if f.Journal == nil {
		return engine.Report{}, errors.New("sửa tự động: thiếu Journal, không ghi được lịch sử job")
	}
	if x.ssl != nil {
		x.ssl.Plan = x.Plan
		return x.ssl.Execute(ctx, onEvent)
	}
	if x.planned != nil {
		x.planned.Plan = x.Plan
		return x.planned.Execute(ctx, onEvent)
	}
	rep, err := engine.New(f.Sys, engine.Options{Journal: f.Journal, OnEvent: onEvent}).Execute(ctx, x.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s cho %s: %w", x.Plan.Kind, x.CheckID, err)
	}
	return rep, nil
}
