package core

// M14 (mục 6.13 của docs/KE-HOACH.md): Plan "Áp bản vá cô lập" và Plan đổi
// chế độ .htaccess của một site. Hợp đồng (điều kiện đầu, các bước, kiểm
// chứng) ghi ở types_m14.go.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// ---------------------------------------------------------------- .htaccess

// htaccessMaxLines là số dòng "<file>:<dòng>: <chỉ thị>" tối đa in trong lời
// từ chối. Một plugin có thể rải .htaccess ra hàng trăm thư mục; người đọc cần
// vài chục dòng đầu để biết phải sửa gì, không cần cả nghìn dòng trên terminal.
const htaccessMaxLines = 50

// PrepareHtaccess: xem types_m14.go.
func (s *Sites) PrepareHtaccess(ctx context.Context, d validate.Domain, mode site.HtaccessMode) (*Planned, error) {
	// mode đến từ CLI và API. Giá trị lạ lọt tới template thì rơi vào bản
	// an-toan trong khi bảng sites ghi một chuỗi vô nghĩa, nên chặn ở đây.
	if _, err := site.ParseHtaccessMode(string(mode)); err != nil {
		return nil, fmt.Errorf("đổi chế độ .htaccess: %w", err)
	}
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	dom := d.String()
	if st.Status != site.Active {
		return nil, refuse("site_not_active", i18n.T(
			"site "+dom+" đang ở trạng thái "+string(st.Status)+"; chỉ đổi chế độ .htaccess cho site đang chạy (active)",
			"site "+dom+" is "+string(st.Status)+"; the .htaccess mode can only be changed for an active site"))
	}
	// Site tạm khoá đang dùng vhost bản tạm khoá (không có khối AllowOverride
	// nào): không có gì để ghi lại và không có site nào trả 200 để kiểm.
	if err := refuseSuspendedPool(st); err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Web != Apache {
		return nil, refuse(CodeHtaccessNotApache, i18n.T(
			"stack đang chạy "+stackName(cfg.Web)+", mà nginx không đọc .htaccess nên chế độ này chưa có tác dụng gì. Chỉ đổi được khi stack là Apache (chuyển bằng: sudo tpan stack switch --to apache)",
			"the stack runs "+stackName(cfg.Web)+", and nginx does not read .htaccess, so this mode has no effect yet. It can only be changed on an Apache stack (switch with: sudo tpan stack switch --to apache)"))
	}
	cur := st.Htaccess.OrDefault()
	if cur == mode {
		return nil, refuse(CodeHtaccessSame, i18n.T(
			"site "+dom+" đã ở chế độ .htaccess "+string(mode)+"; không đổi gì",
			"site "+dom+" is already in .htaccess mode "+string(mode)+"; nothing to change"))
	}
	l := st.Layout()
	if mode == site.HtaccessSafe {
		// Quét TRƯỚC khi dựng Plan: một chỉ thị ngoài danh sách làm Apache trả
		// 500 cho cả site ngay khi vhost mới được nạp. Nói trước rẻ hơn hoàn tác.
		found, _, err := checks.ScanHtaccess(ctx, s.Sys, l)
		if err != nil {
			return nil, fmt.Errorf("quét .htaccess của %s trước khi chuyển sang %s (chưa quét được thì chưa biết site có báo 500 không): %w", dom, mode, err)
		}
		if len(found) > 0 {
			return nil, htaccessBlocked(dom, found)
		}
	}

	next := st
	next.Htaccess = mode
	var ss []engine.Step
	// Rời "tat": Apache bắt đầu đọc .htaccess và vhost thôi có FallbackResource,
	// nên thiếu file này thì mọi permalink /%postname%/ là 404. Ghi trước vhost
	// để lúc vhost mới được nạp file đã nằm đó. Có rồi thì để nguyên.
	if cur == site.HtaccessOff {
		ht, err := htaccessIfMissing(s.Sys, l, l.Public)
		if err != nil {
			return nil, err
		}
		if ht != nil {
			ss = append(ss, ht)
		}
	}
	// Đúng một đường ghi vhost (siteVhost): site có SSL ra bản 443, không thì
	// bản cổng 80, cả hai theo chế độ mới.
	vhost, err := siteVhost(next, cfg)
	if err != nil {
		return nil, fmt.Errorf("dựng vhost của %s theo chế độ %s: %w", dom, mode, err)
	}
	ss = append(ss, vhost...)
	// DB của site đang khoá thì không có phép gọi nào trả 200 được (siteHTTP nil).
	http := siteHTTP(next)
	if http != nil {
		// Như mở khoá site và php.use: `systemctl reload apache2` trả về trước
		// khi cấu hình mới có hiệu lực, nên chờ hai lần đạt liên tiếp. Site không
		// trả lời đúng (một .htaccess mà phép quét không lường được) thì dừng ở
		// đây và hoàn tác, thay vì để site báo lỗi cho khách.
		ss = append(ss, &midCheck{
			id:    "site.htaccess.check:" + dom,
			check: http,
			need:  2,
			title: i18n.T("Kiểm "+dom+" trả 200 ở chế độ .htaccess "+string(mode), "Check that "+dom+" answers 200 in .htaccess mode "+string(mode)),
			why: i18n.T(
				"Apache nạp vhost mới trong nền, sau khi lệnh reload đã trả về. Bước này chờ tới khi site trả lời đúng; không được thì job dừng ở đây và hoàn tác: vhost về lại chế độ "+string(cur)+".",
				"Apache loads the new vhost in the background, after the reload command has returned. This step waits until the site answers correctly; if it never does the job stops here and rolls back: the vhost goes back to mode "+string(cur)+"."),
		})
	}

	verify := []checks.Check{checks.VhostHardened(string(cfg.Web), l, mode), webConfigCheck(cfg.Web)}
	// Ở day-du phép này luôn là cảnh báo (site ghi được cấu hình Apache): không
	// kiểm gì, health vẫn nhắc.
	if mode != site.HtaccessFull {
		verify = append(verify, checks.HtaccessDirectives(l, mode))
	}
	if http != nil {
		verify = append(verify, http)
	}
	plan := engine.Plan{
		Kind:   "site.htaccess",
		Target: dom,
		Title: i18n.T(
			"Đổi chế độ .htaccess của "+dom+" từ "+string(cur)+" sang "+string(mode),
			"Change the .htaccess mode of "+dom+" from "+string(cur)+" to "+string(mode)),
		Steps:  ss,
		Verify: verify,
	}
	return newPlanned(s.Sys, s.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		if err := saveHtaccess(ctx, s.Store, d, mode); err != nil {
			return fmt.Errorf("%w (vhost đã đổi xong; chạy lại `sudo tpan site set %s --htaccess %s` để ghi nốt: các bước đã đúng được bỏ qua)", err, dom, mode)
		}
		return nil
	}), nil
}

// saveHtaccess đọc lại dòng của site rồi chỉ đổi cột htaccess: giữa lúc dựng
// Plan và lúc job xong, dòng có thể đã đổi trường khác (như markSuspended).
func saveHtaccess(ctx context.Context, store SiteStore, d validate.Domain, mode site.HtaccessMode) error {
	st, ok, err := store.Site(ctx, d)
	if err == nil && !ok {
		err = errors.New("site không còn trong bảng sites")
	}
	if err == nil {
		st.Htaccess = mode
		err = store.SaveSite(ctx, st)
	}
	if err != nil {
		return fmt.Errorf("ghi chế độ .htaccess %s của site %s: %w", mode, d, err)
	}
	return nil
}

// htaccessBlocked là lời từ chối chuyển sang an-toan: từng dòng
// "<file>:<dòng>: <chỉ thị>" rồi hai lối ra.
func htaccessBlocked(dom string, found []htaccess.Finding) *RefusedError {
	vi, en := htaccessLines(found)
	n := strconv.Itoa(len(found))
	return refuse(CodeHtaccessBlocked, i18n.T(
		"không chuyển "+dom+" sang chế độ an-toan được: .htaccess của site có "+n+" chỗ nằm ngoài danh sách chỉ thị cho phép, và Apache sẽ trả 500 cho mọi request dưới thư mục chứa file đó:\n"+vi+
			"\nHai lối ra: (1) sửa hoặc xoá các dòng trên rồi chạy lại; (2) để site ở chế độ day-du (sudo tpan site set "+dom+" --htaccess day-du) và nhận cảnh báo đi kèm trong health.",
		dom+" cannot be switched to mode an-toan: the site's .htaccess has "+n+" entries outside the allowed directive list, and Apache would answer 500 to every request under the directory holding that file:\n"+en+
			"\nTwo ways out: (1) fix or delete the lines above, then run again; (2) leave the site in mode day-du (sudo tpan site set "+dom+" --htaccess day-du) and accept the warning that health shows for it."))
}

// htaccessLines in mỗi Finding thành một dòng "<file>:<dòng>: <chỉ thị>", tối
// đa htaccessMaxLines dòng. File không đọc được (Line 0, htaccess.Scan đếm từ
// 1) không có số dòng. Tên file và chỉ thị do user của site đặt, nên ký tự điều
// khiển bị đổi trước khi in ra terminal và giao diện.
func htaccessLines(found []htaccess.Finding) (vi, en string) {
	var a, b []string
	for i, f := range found {
		if i == htaccessMaxLines {
			rest := strconv.Itoa(len(found) - i)
			a, b = append(a, "... và "+rest+" dòng nữa"), append(b, "... and "+rest+" more")
			break
		}
		file := cleanCtl(f.File)
		if f.Line == 0 {
			a, b = append(a, file+": "+cleanCtl(f.Directive)), append(b, file+": (unreadable)")
			continue
		}
		ln := file + ":" + strconv.Itoa(f.Line) + ": " + cleanCtl(f.Directive)
		a, b = append(a, ln), append(b, ln)
	}
	return strings.Join(a, "\n"), strings.Join(b, "\n")
}

// ------------------------------------------------------------------- harden

// hardenUnscanned là Finding.Directive của site mà Plan "Áp bản vá cô lập"
// không quét được .htaccess (find lỗi vì một thư mục user của site không vào
// được, chẳng hạn). Chưa quét được thì chưa biết chế độ an-toan có làm site
// báo 500 không, nên site đó được giữ ở day-du thay vì chặn bản vá của cả máy.
const hardenUnscanned = "(không quét được)"

// PrepareHarden: xem types_m14.go.
func (s *Security) PrepareHarden(ctx context.Context) (*Harden, error) {
	if s.Settings == nil || s.Sites == nil {
		return nil, errors.New("security.harden: thiếu Settings hoặc Sites")
	}
	cfg, err := (&Sites{Settings: s.Settings}).stack(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.Sites.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	var active []site.Site
	for _, st := range all {
		if st.Status == site.Active {
			active = append(active, st)
		}
	}
	wrap := func(err error) error { return fmt.Errorf("dựng Plan áp bản vá cô lập: %w", err) }

	// Nhóm 1: file ini OPcache và drop-in ProtectProc cho mọi bản PHP đang cài.
	// Mỗi bản tự kiểm, reload và restart dịch vụ của riêng nó.
	versions, err := phpInstalled(s.Sys)
	if err != nil {
		return nil, err
	}
	var ss []engine.Step
	for _, v := range versions {
		h, err := steps.PHPHardening(v)
		if err != nil {
			return nil, wrap(err)
		}
		ss = append(ss, h...)
	}

	// Nhóm 2: logrotate (bản nginx gửi HUP). Trước nhóm 3: khi thư mục log đã
	// là 0750 thì lần xoay log kế tiếp phải đi theo file mới này.
	lr, err := logrotateStep(cfg.Web)
	if err != nil {
		return nil, wrap(err)
	}
	ss = append(ss, lr)

	// Nhóm 3: quyền log của mọi site active, kể cả site tạm khoá (log của nó
	// vẫn nằm trên đĩa và vẫn có IP của khách).
	for _, st := range active {
		ss = append(ss, steps.SiteLogs(st.Layout())...)
	}

	// Nhóm 4: quyết định chế độ .htaccess. Chỉ đọc; kết quả đi vào bản ghi
	// trong active để nhóm 5 ghi vhost theo đúng chế độ đã quyết. Trên nginx
	// vẫn quét và chuyển: cột htaccess được dùng khi máy chuyển sang Apache.
	h := &Harden{Switched: []string{}, Kept: []HtaccessKept{}}
	for i, st := range active {
		// Site tạm khoá: vhost của nó là bản tạm khoá, không quét, không đổi.
		if st.Suspended != account.NotSuspended || st.Htaccess.OrDefault() != site.HtaccessFull {
			continue
		}
		l := st.Layout()
		found, _, err := checks.ScanHtaccess(ctx, s.Sys, l)
		if cerr := ctx.Err(); cerr != nil {
			return nil, wrap(cerr)
		}
		if err != nil {
			found = []htaccess.Finding{{File: l.Public, Directive: hardenUnscanned + " " + cleanCtl(err.Error())}}
		}
		if len(found) > 0 {
			h.Kept = append(h.Kept, HtaccessKept{Domain: st.Domain.String(), Findings: found})
			continue
		}
		active[i].Htaccess = site.HtaccessSafe
		h.Switched = append(h.Switched, st.Domain.String())
	}

	// Nhóm 5: ghi lại vhost của mọi site active qua siteVhost (site tạm khoá ra
	// bản tạm khoá như cũ), bỏ reload của từng bước, rồi kiểm cú pháp và reload
	// web server đúng một lần.
	var vhosts []engine.Step
	for _, st := range active {
		v, err := siteVhost(st, cfg)
		if err != nil {
			return nil, wrap(fmt.Errorf("vhost của %s: %w", st.Domain, err))
		}
		vhosts = append(vhosts, v...)
	}
	if len(vhosts) > 0 {
		cr, err := steps.WebReload(string(cfg.Web))
		if err != nil {
			return nil, wrap(err)
		}
		reload := &hardenReload{ConfigReload: cr, watch: steps.WithoutReload(vhosts)}
		for _, v := range vhosts {
			ss = append(ss, &hardenWrite{Step: v, reload: reload})
		}
		ss = append(ss, reload)
	}

	// Nhóm 6 (M28 đợt 2, H5): Apache tắt mod_status. Chỉ tắt, không áp lại phần
	// bật của danh sách stack (admin có thể đã cố ý đổi). Bước tự reload, nên
	// đặt SAU bước reload chung: đặt trước thì lần reload của nó nạp luôn các
	// vhost vừa ghi mà hardenReload chưa biết.
	var modOff *hardenModOff
	if cfg.Web == Apache {
		modOff = &hardenModOff{ApacheModOff: &steps.ApacheModOff{Module: "status"}}
		ss = append(ss, modOff)
	}

	// Nhóm 7 (M28 đợt 2, M1-DBCONN): trần kết nối cho user DB của mọi site
	// active (kể cả site hay DB đang tạm khoá: user khoá vẫn đổi trần được) và
	// mọi DB rời active. User không có (máy lệch) thì không có bước, chỉ nêu
	// tên trong bản xem trước (S2, S16).
	h.Skipped = []HardenSkipped{}
	var dbUsers []string
	for _, st := range active {
		dbUsers = append(dbUsers, st.Layout().DB)
	}
	if dbs := s.dbStore(); dbs != nil {
		all, err := dbs.StandaloneDBs(ctx, "")
		if err != nil {
			return nil, wrap(fmt.Errorf("đọc danh sách DB rời: %w", err))
		}
		for _, d := range all {
			if d.Status == account.DBActive {
				dbUsers = append(dbUsers, d.Name)
			}
		}
	}
	var limited []string
	for _, u := range dbUsers {
		// User không có, hay đọc lỗi (MariaDB tắt, output lạ): không có bước,
		// nêu trong bản xem trước. Giữ bước thì nó lỗi ở lần kiểm trước khi
		// chạy và engine hoàn tác cả bản vá vhost của mọi site (S2).
		exists, _, err := steps.DBUserLimitState(ctx, s.Sys, u)
		if err != nil {
			h.Skipped = append(h.Skipped, HardenSkipped{Target: u, Kind: HardenSkipDBUser, Message: i18n.T(
				"không đọc được trần kết nối của user DB "+u+" ("+err.Error()+"): bỏ qua bước đặt trần. Kiểm MariaDB rồi chạy lại tpan security harden.",
				"could not read the connection limit of database user "+u+" ("+err.Error()+"): the limit step is skipped. Check MariaDB, then run tpan security harden again.")})
			continue
		}
		if !exists {
			h.Skipped = append(h.Skipped, HardenSkipped{Target: u, Kind: HardenSkipDBUser, Message: i18n.T(
				"user DB "+u+" không có trong MariaDB: bỏ qua bước đặt trần kết nối. Xem tpan health của site hay DB rời dùng user này.",
				"database user "+u+" does not exist in MariaDB: the connection limit step is skipped. See tpan health for the site or database that uses it.")})
			continue
		}
		ss = append(ss, &steps.DBUserLimit{User: u, Max: site.DBMaxUserConnections})
		limited = append(limited, u)
	}

	// Nhóm 8 (M17), luôn ở cuối: khởi động lại dịch vụ của mọi lồng đang chạy,
	// để hàng rào che cả những file nhóm 1 vừa ghi (xem cageRestartSteps).
	restarts, err := cageRestartSteps(ctx, s.Sites)
	if err != nil {
		return nil, wrap(err)
	}
	ss = append(ss, restarts...)

	// S2: site trả lời sai TRƯỚC Plan (đo ngay đây, như sitesAnswer của chuyển
	// web server) được nêu trong bản xem trước, và phép HTTP, cô lập của nó
	// không chạy sau job (chỉ tốn thời gian chờ retryCheck). Site đó vẫn được
	// vá đủ mọi bước.
	healthy, broken := hardenHealthy(ctx, s.Sys, active)
	h.Skipped = append(h.Skipped, broken...)

	// Kiểm chứng (D2 của soát cuối đợt 2): chỉ những phép do chính các bước
	// của Plan quyết định mới làm job lỗi và hoàn tác: bộ của stack và các phép
	// cấp máy của M14 (Stack.Checks), quyền log, dòng chặn và dấu F4 của vhost,
	// mod_status, trần kết nối. Phép HTTP và cô lập của từng site đo thứ mà mã
	// của khách điều khiển được (site trả 500 đúng lúc kiểm, chủ site chmod
	// 2751 thư mục neo của mình): trước đây một khách xấu làm chúng Fail là cả
	// bản vá của máy bị hoàn tác, mãi mãi. Giờ chúng chạy sau các bước như một
	// báo cáo (checks.Soft: Fail thành Warn), nằm trong kết quả của job và nêu
	// đúng site có vấn đề; health chạy sau job mở sự cố cho site đó như thường.
	verify, _, err := (&Stack{Sys: s.Sys, Settings: s.Settings, Sites: s.Sites, WPCLISHA512: s.WPCLISHA512}).Checks(ctx)
	if err != nil {
		return nil, wrap(err)
	}
	if modOff != nil {
		for i, c := range verify {
			verify[i] = modStatusSoft{c: c, off: modOff}
		}
	}
	for i, st := range active {
		l := st.Layout()
		verify = append(verify, checks.LogPerms(l))
		// Vhost tạm khoá không trỏ vào public nên không có dòng chặn nào. M28
		// đợt 2: dấu của bản vá F4 (web.docroot_guard) chỉ đòi ở đây và ở
		// health; harden ghi lại vhost của mọi site nên site hỏng HTTP cũng đạt.
		if st.Suspended == account.NotSuspended {
			verify = append(verify, checks.VhostHardened(string(cfg.Web), l, st.Htaccess), checks.DocrootGuard(string(cfg.Web), l))
		}
		if !healthy[st.Domain] {
			continue
		}
		// siteHTTP: 403 đúng trang tạm khoá cho site tạm khoá, nil khi DB của
		// site đang khoá. retryCheck vì reload trả về trước khi cấu hình mới có
		// hiệu lực.
		if c := siteHTTP(st); c != nil {
			verify = append(verify, checks.Soft(retryCheck{c: c}))
		}
		// Cùng luật với siteChecks (M17, bất biến 8): Isolated chạy `test` dưới
		// user của site "from" ở NGOÀI lồng, nên không sinh từ site trong lồng.
		for _, o := range active[i+1:] {
			if !healthy[o.Domain] {
				continue
			}
			if !st.Caged {
				verify = append(verify, checks.Soft(checks.Isolated(l, o.Layout())))
			}
			if !o.Caged {
				verify = append(verify, checks.Soft(checks.Isolated(o.Layout(), l)))
			}
		}
	}
	// db.user_limit chỉ cảnh báo, không làm job lỗi: có mặt để báo cáo của job
	// ghi trần của từng user (bằng chứng cho E5).
	for _, u := range limited {
		verify = append(verify, checks.DBUserLimit(u))
	}

	plan := engine.Plan{
		Kind:   "security.harden",
		Target: "isolation",
		Title:  i18n.T("Áp bản vá cô lập", "Apply the isolation hardening"),
		Steps:  ss,
		Verify: verify,
	}
	switched := h.Switched
	h.Planned = newPlanned(s.Sys, s.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		var errs []error
		for _, dom := range switched {
			d, err := validate.ParseDomain(dom)
			if err == nil {
				err = saveHtaccess(ctx, s.Sites, d, site.HtaccessSafe)
			}
			errs = append(errs, err)
		}
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("%w (máy đã đổi xong; chạy lại `sudo tpan security harden` để ghi nốt: các bước đã đúng được bỏ qua)", err)
		}
		return nil
	})
	return h, nil
}

// dbStore là bảng DB rời cho PrepareHarden: s.DBs, hoặc s.Sites khi nó cũng là
// DBStore (server và CLI truyền cùng *store.Store cho cả hai). nil: bỏ qua DB rời.
func (s *Security) dbStore() DBStore {
	if s.DBs != nil {
		return s.DBs
	}
	if d, ok := s.Sites.(DBStore); ok {
		return d
	}
	return nil
}

// hardenHealthy đo phép HTTP của từng site active trước Plan (S2). healthy
// gồm site đạt và site không có phép HTTP để đo (DB đang tạm khoá: siteHTTP
// nil); broken là các site Fail, thành mục của Harden.Skipped.
func hardenHealthy(ctx context.Context, sys system.System, active []site.Site) (map[validate.Domain]bool, []HardenSkipped) {
	healthy := map[validate.Domain]bool{}
	var cs []checks.Check
	var doms []validate.Domain
	for _, st := range active {
		c := siteHTTP(st)
		if c == nil {
			healthy[st.Domain] = true
			continue
		}
		cs, doms = append(cs, c), append(doms, st.Domain)
	}
	var broken []HardenSkipped
	for i, r := range checks.RunAll(ctx, sys, cs) {
		if r.Status != checks.Fail {
			healthy[doms[i]] = true
			continue
		}
		dom := doms[i].String()
		broken = append(broken, HardenSkipped{Target: dom, Kind: HardenSkipSiteHTTP, Result: &r, Message: i18n.T(
			"site "+dom+" đang không trả lời đúng ("+r.Summary.Vi+"): vẫn được vá, nhưng báo cáo sau job không gọi thử site này. Sửa hoặc tạm khoá site này; health sẽ báo khi nó trả lời đúng lại.",
			"site "+dom+" does not answer correctly right now ("+r.Summary.En+"): it is still hardened, but the report after the job does not call it. Fix or suspend this site; health will show when it answers correctly again.")})
	}
	return healthy, broken
}

// hardenReload là bước kiểm cú pháp rồi reload web server MỘT lần cho mọi bước
// ghi vhost của Plan "Áp bản vá cô lập" (mục 7: reload một lần). Khác
// steps.ConfigReload trần ở hai chỗ:
//
//   - Check đạt khi không bước ghi vhost nào còn việc, nên chạy lại Plan trên
//     máy đã đúng thì bước này được bỏ qua, không reload thừa. ConfigReload luôn
//     reload vì job hàng loạt biết chắc nó vừa ghi file.
//   - Job lỗi sau khi đã reload: engine hoàn tác ngược thứ tự, tức là bước này
//     trước rồi mới tới các bước ghi vhost. Reload ở Undo của bước này thì web
//     server nạp lại đúng cấu hình MỚI (file cũ chưa về). Vì vậy việc nạp lại
//     nằm ở bước ghi vhost được hoàn tác SAU CÙNG (hardenWrite.Undo).
type hardenReload struct {
	*steps.ConfigReload
	// watch là các bước ghi vhost (đã bỏ Reload) mà bước này nạp thay.
	watch []engine.Step

	// written: số bước ghi vhost đã Apply trong job này và chưa bị hoàn tác.
	// loaded: web server đã nạp các file đó. Không cần khoá: engine chạy và
	// hoàn tác các bước của một Plan lần lượt.
	written int
	loaded  bool
}

var (
	_ engine.Step = (*hardenReload)(nil)
	_ engine.Step = (*hardenWrite)(nil)
)

func (r *hardenReload) Info() engine.StepInfo {
	info := r.ConfigReload.Info()
	unit := r.Service
	info.Title = i18n.T(
		"Kiểm cấu hình rồi nạp lại "+unit+" một lần cho mọi vhost vừa ghi",
		"Check the configuration and reload "+unit+" once for every vhost just written")
	info.Why = i18n.T(
		"Các bước trước chỉ ghi file vhost, chưa nạp. Bỏ bước này thì "+unit+" vẫn chạy vhost cũ, tức là chưa dòng chặn nào có hiệu lực. Nạp một lần cho mọi site thay vì mỗi site một lần, và kiểm cú pháp trước để một file sai không làm "+unit+" nạp hỏng.",
		"The earlier steps only wrote the vhost files. Without this step "+unit+" keeps running the old vhosts, so none of the guard lines is in effect. It reloads once for all sites instead of once per site, and checks the syntax first so a bad file never reaches "+unit+".")
	info.NoUndo = i18n.T(
		"Bước này không có gì để trả lại. Khi job lỗi, các bước ghi vhost trả file cũ về, rồi "+unit+" được kiểm và nạp lại một lần nữa.",
		"This step has nothing to give back. When the job fails, the vhost steps put the old files back, then "+unit+" is checked and reloaded once more.")
	return info
}

// Check: trong lúc job chạy, có bước ghi vhost đã Apply thì đạt khi đã reload.
// Chưa bước nào ghi (xem trước, hoặc mọi vhost đã đúng): đạt khi không bước ghi
// vhost nào còn việc.
func (r *hardenReload) Check(ctx context.Context, sys system.System) (bool, error) {
	if r.written > 0 {
		return r.loaded, nil
	}
	for _, w := range r.watch {
		if done, err := w.Check(ctx, sys); err != nil || !done {
			return false, err
		}
	}
	return true, nil
}

func (r *hardenReload) Apply(ctx context.Context, sys system.System) error {
	if err := r.again(ctx, sys); err != nil {
		return err
	}
	r.loaded = true
	return nil
}

// again kiểm cú pháp rồi reload bằng một ConfigReload mới (bước đó giữ cờ
// riêng cho Check của nó, ở đây không dùng).
func (r *hardenReload) again(ctx context.Context, sys system.System) error {
	return (&steps.ConfigReload{Service: r.Service, Test: r.Test, Reload: r.Reload}).Apply(ctx, sys)
}

// hardenWrite bọc một bước ghi vhost đã bỏ Reload: đếm bước đã ghi cho
// hardenReload, và khi bước cuối cùng được hoàn tác (mọi file cũ đã về) mà web
// server đã nạp cấu hình mới thì kiểm cú pháp và reload lại, để thứ đang chạy
// khớp với thứ trên đĩa. Info và Check đi thẳng tới bước gốc.
type hardenWrite struct {
	engine.Step
	reload *hardenReload
}

func (w *hardenWrite) Apply(ctx context.Context, sys system.System) error {
	if err := w.Step.Apply(ctx, sys); err != nil {
		return err
	}
	w.reload.written++
	return nil
}

// hardenModOff bọc bước tắt mod_status trong harden (D11 của soát cuối đợt 2).
// status được nạp bằng một dòng LoadModule ngoài mods-enabled (cấu hình admin
// tự thêm) thì a2dismod không tắt được; bước gốc trả ErrModuleLoadedElsewhere
// sau khi đã tự trả mods-enabled về như cũ. Ở đây lỗi đó không làm hoàn tác
// cả bản vá (vhost, quyền log, trần DB của mọi site): bước coi như xong, và
// web.mod_status trong kết quả của job thành cảnh báo kèm cách sửa tay.
// `tpan health --fix` cho web.mod_status dùng bước gốc nên vẫn báo lỗi rõ.
type hardenModOff struct {
	*steps.ApacheModOff
	gaveUp bool // Apply gặp ErrModuleLoadedElsewhere
}

var _ engine.Step = (*hardenModOff)(nil)

func (m *hardenModOff) Check(ctx context.Context, sys system.System) (bool, error) {
	if m.gaveUp {
		return true, nil
	}
	return m.ApacheModOff.Check(ctx, sys)
}

func (m *hardenModOff) Apply(ctx context.Context, sys system.System) error {
	err := m.ApacheModOff.Apply(ctx, sys)
	if errors.Is(err, steps.ErrModuleLoadedElsewhere) {
		m.gaveUp = true
		return nil
	}
	return err
}

func (m *hardenModOff) Undo(ctx context.Context, sys system.System) error {
	if m.gaveUp {
		return nil // bước gốc đã trả mods-enabled về, không có gì để hoàn tác
	}
	return m.ApacheModOff.Undo(ctx, sys)
}

// modStatusSoft hạ web.mod_status Fail xuống Warn khi hardenModOff đã bỏ cuộc
// (module nạp ngoài mods-enabled); mọi phép khác đi nguyên.
type modStatusSoft struct {
	c   checks.Check
	off *hardenModOff
}

func (s modStatusSoft) Run(ctx context.Context, sys system.System) checks.Result {
	r := s.c.Run(ctx, sys)
	if s.off.gaveUp && r.Status == checks.Fail && strings.HasPrefix(r.ID, "web.mod_status:") {
		r.Status = checks.Warn
	}
	return r
}

func (w *hardenWrite) Undo(ctx context.Context, sys system.System) error {
	err := w.Step.Undo(ctx, sys)
	// Đếm cả khi Undo lỗi: engine vẫn hoàn tác các bước còn lại, và lần hoàn
	// tác cuối phải nạp lại những file đã về được (có kiểm cú pháp trước).
	r := w.reload
	r.written--
	if r.written > 0 || !r.loaded {
		return err
	}
	r.loaded = false
	return errors.Join(err, r.again(ctx, sys))
}
