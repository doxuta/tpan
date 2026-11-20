package core

// Ca sử dụng của site WordPress (mục 6.2, 6.4, 6.7 của docs/KE-HOACH.md):
// tạo, chạy lại, đưa vào thùng rác, liệt kê, xem và kiểm sức khoẻ một site.
// Khôi phục và xoá hẳn (từ thùng rác) nằm ở trash.go.
//
// Giống stack.go: Prepare* chỉ đọc (kiểm điều kiện, dựng Plan), Preview gọi
// engine xem trước, Execute chạy engine rồi mới ghi bảng sites khi job thành
// công. Mọi tên và đường dẫn của site lấy từ site.LayoutOf, không ghép ở đây.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// siteAdminUser là tên tài khoản quản trị WordPress của mọi site.
	siteAdminUser = "admin"
	// sitePasswordLen: mật khẩu DB và mật khẩu quản trị, 32 ký tự (mục 6.2 bước 4).
	sitePasswordLen = 32
	// siteTitleMax là độ dài tối đa của tiêu đề site, tính bằng byte (khớp
	// kiểm của steps.WPInstall, để tiêu đề sai bị từ chối trước khi job chạy
	// chứ không phải giữa chừng, lúc user và DB đã được tạo).
	siteTitleMax = 200
	// Thư mục cha của thư mục log: RemoveTree chỉ xoá bên dưới. (Thư mục site
	// chỉ bị xoá hẳn khi đã ở thùng rác, bên dưới site.TrashRoot.)
	siteLogRoot = "/var/log/tpan/sites"
)

// EmptySiteStore là bảng sites rỗng và chỉ đọc: máy chưa có tpan.db (`--dry-run`
// lần đầu). Ghi vào thì lỗi, giống EmptySettings.
type EmptySiteStore struct{}

func (EmptySiteStore) Site(context.Context, validate.Domain) (site.Site, bool, error) {
	return site.Site{}, false, nil
}

func (EmptySiteStore) Sites(context.Context) ([]site.Site, error) { return []site.Site{}, nil }

func (EmptySiteStore) SaveSite(_ context.Context, s site.Site) error {
	return fmt.Errorf("lưu site %s: chưa có DB (chế độ chỉ xem)", s.Domain)
}

func (EmptySiteStore) DeleteSite(_ context.Context, d validate.Domain) error {
	return fmt.Errorf("xoá site %s: chưa có DB (chế độ chỉ xem)", d)
}

// Sites gom những gì các ca sử dụng của site cần. Dựng mới cho mỗi lệnh.
type Sites struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore
	// Journal nhận job và sự kiện khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
	// Bản WordPress và SHA-1 của tarball. Rỗng thì dùng
	// steps.DefaultWordPressVersion/SHA1. Test đặt SHA-1 của một file giả.
	WPVersion string
	WPHash    string
	// Now nil thì dùng time.Now. Test đặt để có TrashedAt, PurgeAt cố định (M6).
	Now func() time.Time
	// Backups (M9) là bảng backups: xoá hẳn ghi vào đây bản tự backup trước
	// khi xoá (mục 6.5). *store.Store thoả. nil (xem trước khi máy chưa có
	// tpan.db, test) thì xoá hẳn vẫn backup nhưng không ghi được dòng: Execute
	// trả lỗi kèm đường dẫn file để người ghi nhận bằng tay.
	Backups BackupStore
	// Accounts (M13) là bảng users, packages: kiểm hạn mức gói khi tạo site.
	// nil (test, xem trước khi máy chưa có tpan.db) thì không kiểm hạn mức.
	Accounts AccountStore
}

func (s *Sites) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sites) wordpress() (version, hash string) {
	version, hash = s.WPVersion, s.WPHash
	if version == "" {
		version = steps.DefaultWordPressVersion
	}
	if hash == "" {
		hash = steps.DefaultWordPressSHA1
	}
	return version, hash
}

// stack đọc cấu hình stack (dùng lại Stack.Config). Stack chưa cài thì từ chối:
// site cần web server và bản PHP để có vhost và pool.
func (s *Sites) stack(ctx context.Context) (StackConfig, error) {
	cfg, err := (&Stack{Settings: s.Settings}).Config(ctx)
	if err != nil {
		return StackConfig{}, err
	}
	if cfg.Web == "" || cfg.PHPVersion == "" {
		return StackConfig{}, refuse("stack_not_installed", i18n.T(
			"stack chưa được cài; cài trước bằng: sudo tpan stack install --web nginx|apache",
			"the stack is not installed; install it first with: sudo tpan stack install --web nginx|apache"))
	}
	return cfg, nil
}

// find đọc site trong bảng sites; không có thì từ chối site_not_found.
func (s *Sites) find(ctx context.Context, d validate.Domain) (site.Site, error) {
	if d.IsZero() {
		return site.Site{}, refuse(validate.CodeDomainInvalid, i18n.T("thiếu domain", "the domain is missing"))
	}
	st, ok, err := s.Store.Site(ctx, d)
	if err != nil {
		return site.Site{}, err
	}
	if !ok {
		return site.Site{}, refuse("site_not_found", i18n.T(
			"không có site "+d.String()+" trong tpan; xem danh sách bằng: sudo tpan site list",
			"tpan has no site "+d.String()+"; list sites with: sudo tpan site list"))
	}
	return st, nil
}

// SiteInput là đầu vào của "tạo site". Domain và Email đã qua validate.
type SiteInput struct {
	Domain validate.Domain
	// Title rỗng thì dùng domain.
	Title string
	Email validate.Email
	// Owner (M13) là chủ của site mới; rỗng là account.AdminName. Chạy lại một
	// site đã có thì bỏ qua (không đổi chủ ở đây). Kiểm hạn mức gói của Owner
	// khi Sites.Accounts khác nil.
	Owner string
}

// checkSiteTitle kiểm tiêu đề site: UTF-8 hợp lệ, không ký tự điều khiển (kể
// cả C1, 0x7f-0x9f), tối đa siteTitleMax byte. Tiêu đề đi vào argv của WP-CLI
// và vào DB của WordPress; xuống dòng ở đây sẽ làm bẩn log và giao diện.
func checkSiteTitle(t string) error {
	var vi, en string
	switch {
	case t == "":
		vi, en = "tiêu đề site đang trống", "the site title is empty"
	case len(t) > siteTitleMax:
		vi, en = fmt.Sprintf("tiêu đề site dài quá %d byte", siteTitleMax), fmt.Sprintf("the site title is longer than %d bytes", siteTitleMax)
	case !utf8.ValidString(t):
		vi, en = "tiêu đề site không phải UTF-8 hợp lệ", "the site title is not valid UTF-8"
	case strings.ContainsFunc(t, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) }):
		vi, en = "tiêu đề site có ký tự điều khiển (xuống dòng, tab...)", "the site title contains control characters (newline, tab...)"
	default:
		return nil
	}
	return refuse("title_invalid", i18n.T(vi, en))
}

// sitePasswordChars là bảng chữ của mật khẩu: chỉ chữ và số, để mật khẩu đặt
// được trong '...' của SQL và đi qua stdin của WP-CLI mà không cần thoát ký tự.
const sitePasswordChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// sitePassword sinh mật khẩu sitePasswordLen ký tự bằng crypto/rand. Byte ≥ 248
// bị bỏ: 248 = 4 × 62, nên b%62 với b < 248 cho mỗi ký tự đúng 4 khả năng, không
// ký tự nào được ưu tiên.
func sitePassword() (system.Secret, error) {
	const limit = 256 - 256%len(sitePasswordChars)
	out := make([]byte, 0, sitePasswordLen)
	buf := make([]byte, sitePasswordLen*2)
	for len(out) < sitePasswordLen {
		if _, err := rand.Read(buf); err != nil {
			return system.Secret{}, fmt.Errorf("sinh mật khẩu: %w", err)
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, sitePasswordChars[int(b)%len(sitePasswordChars)])
			if len(out) == sitePasswordLen {
				break
			}
		}
	}
	return system.NewSecret(string(out)), nil
}

// pathExists: Stat thấy có; không có thì false; lỗi khác thì trả lỗi.
func pathExists(sys system.System, p string) (bool, error) {
	_, err := sys.Stat(p)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, fmt.Errorf("kiểm %s: %w", p, err)
}

// linuxUserExists hỏi `getent passwd name`. Mã 2 là "không có key đó" (câu trả
// lời, không phải lỗi); mã khác 0 còn lại mới là lỗi.
func linuxUserExists(ctx context.Context, sys system.System, name string) (bool, error) {
	_, err := sys.Run(ctx, system.Cmd{Name: "getent", Args: []string{"passwd", name}})
	var ee *system.ExitError
	if errors.As(err, &ee) && ee.Code == 2 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("hỏi user Linux %s: %w", name, err)
	}
	return true, nil
}

// unmanaged trả các thứ của site đã có trên máy dù domain chưa có trong bảng
// sites. Chỉ xét thư mục site, user Linux, DB và user DB: chúng mang dữ liệu
// hoặc quyền, tpan không được nhận làm của mình. Không xét file vhost hay pool:
// đó là file cấu hình tpan tự ghi lại được, và Step File của chúng giữ bản cũ
// để hoàn tác.
func (s *Sites) unmanaged(ctx context.Context, l site.Layout) (vi, en []string, err error) {
	dir, err := pathExists(s.Sys, l.Dir)
	if err != nil {
		return nil, nil, err
	}
	if dir {
		vi, en = append(vi, "thư mục "+l.Dir), append(en, "directory "+l.Dir)
	}
	user, err := linuxUserExists(ctx, s.Sys, l.User)
	if err != nil {
		return nil, nil, err
	}
	if user {
		vi, en = append(vi, "user Linux "+l.User), append(en, "Linux user "+l.User)
	}
	db, dbUser, err := steps.SiteDBState(ctx, s.Sys, l)
	if err != nil {
		return nil, nil, err
	}
	if db {
		vi, en = append(vi, "DB "+l.DB), append(en, "database "+l.DB)
	}
	if dbUser {
		vi, en = append(vi, "user DB "+l.DB), append(en, "database user "+l.DB)
	}
	return vi, en, nil
}

// drift từ chối khi user DB và wp-config.php không cùng có hoặc cùng không
// (mục 6.2, "Chạy lại"): mỗi lần chạy sinh mật khẩu DB mới, nên tạo một bên
// khi bên kia còn sẽ ghi một mật khẩu bên kia không biết.
func (s *Sites) drift(ctx context.Context, l site.Layout) error {
	_, dbUser, err := steps.SiteDBState(ctx, s.Sys, l)
	if err != nil {
		return err
	}
	cfg, err := pathExists(s.Sys, l.WPConfig)
	if err != nil {
		return err
	}
	d := l.Domain.String()
	switch {
	case dbUser && !cfg:
		return refuse("site_drift", i18n.T(
			"site "+d+" lệch: user DB "+l.DB+" có nhưng "+l.WPConfig+" không có. tpan không biết mật khẩu user DB đang dùng nên không ghi wp-config.php mới. "+
				"Lấy lại wp-config.php từ backup, hoặc nếu không cần dữ liệu thì xoá hẳn bằng `sudo tpan site rm "+d+"` rồi `sudo tpan trash purge "+d+"`, sau đó tạo lại.",
			"site "+d+" has drifted: database user "+l.DB+" exists but "+l.WPConfig+" does not. tpan does not know the password the database user has, so it will not write a new wp-config.php. "+
				"Restore wp-config.php from a backup, or if the data is not needed, delete the site with `sudo tpan site rm "+d+"` then `sudo tpan trash purge "+d+"`, and create it again."))
	case cfg && !dbUser:
		return refuse("site_drift", i18n.T(
			"site "+d+" lệch: "+l.WPConfig+" có nhưng user DB "+l.DB+" không có. Tạo user DB mới sẽ ra mật khẩu khác mật khẩu trong wp-config.php. "+
				"Tạo lại user DB bằng tay với mật khẩu trong wp-config.php (xem: sudo grep DB_ "+l.WPConfig+"), rồi chạy lại.",
			"site "+d+" has drifted: "+l.WPConfig+" exists but database user "+l.DB+" does not. A new database user would get a password different from the one in wp-config.php. "+
				"Recreate the database user by hand with the password from wp-config.php (see: sudo grep DB_ "+l.WPConfig+"), then run again."))
	}
	return nil
}

// others trả Layout của mọi site active khác d, để kiểm cô lập hai chiều.
func (s *Sites) others(ctx context.Context, d validate.Domain) ([]site.Layout, error) {
	all, err := s.Store.Sites(ctx)
	if err != nil {
		return nil, err
	}
	var out []site.Layout
	for _, o := range all {
		if o.Domain != d && o.Status == site.Active {
			out = append(out, o.Layout())
		}
	}
	return out, nil
}

// siteChecks là bộ phép kiểm của một site, dùng chung cho kiểm chứng sau khi
// tạo và cho health (mục 6.7): SiteChecks theo đường đi của request (bản SSL
// khi site đã có chứng chỉ: cổng 80 giờ trả 301), rồi cô lập hai chiều với
// từng site còn lại.
func siteChecks(web WebServer, php string, l site.Layout, others []site.Layout, ssl bool) []checks.Check {
	cs := checks.SiteChecks(string(web), php, l)
	if ssl {
		cs = checks.SiteChecksSSL(string(web), php, l)
	}
	for _, o := range others {
		cs = append(cs, checks.Isolated(l, o), checks.Isolated(o, l))
	}
	return cs
}

// ---------------------------------------------------------------- tạo site

// SiteAdd là một lần tạo (hoặc chạy lại) site đã qua điều kiện đầu. Dùng cho
// đúng một lần Execute: các Step giữ trạng thái để hoàn tác.
type SiteAdd struct {
	// Site là dòng sẽ được ghi vào bảng sites khi job thành công.
	Site   site.Site
	Layout site.Layout
	// Rerun: domain đã có trong bảng sites, đây là chạy lại Plan (mục 6.2).
	Rerun     bool
	AdminUser string
	Plan      engine.Plan

	sites         *Sites
	adminPassword system.Secret
	install       *wpInstallStep
}

// SiteAddResult là kết quả của SiteAdd.Execute.
type SiteAddResult struct {
	Report engine.Report `json:"report"`
	Site   site.Site     `json:"site"`
	// WPInstalled: lần chạy này thật sự chạy `wp core install`, tức là mật khẩu
	// quản trị dưới đây là mật khẩu đang dùng. false (chạy lại, hoặc job hỏng)
	// thì AdminPassword rỗng.
	WPInstalled bool   `json:"wp_installed"`
	AdminUser   string `json:"admin_user"`
	// AdminPassword chỉ có khi WPInstalled. JSON của nó luôn là "••••": người
	// gọi muốn hiện cho người dùng thì tự gọi Reveal, đúng một lần.
	AdminPassword system.Secret `json:"admin_password"`
}

// PrepareAdd làm phần chỉ đọc của "tạo site" (mục 6.2): kiểm đầu vào, kiểm
// stack, xem domain đã có chưa (trong bảng sites và trên máy), sinh mật khẩu,
// dựng Plan. Không ghi gì. Bị từ chối thì trả *RefusedError.
func (s *Sites) PrepareAdd(ctx context.Context, in SiteInput) (*SiteAdd, error) {
	d := in.Domain
	if d.IsZero() {
		return nil, refuse(validate.CodeDomainInvalid, i18n.T("thiếu domain", "the domain is missing"))
	}
	title := in.Title
	if title == "" {
		title = d.String()
	}
	if err := checkSiteTitle(title); err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}

	l := site.LayoutOf(d)
	old, found, err := s.Store.Site(ctx, d)
	if err != nil {
		return nil, err
	}
	emailGuessed := false
	rec := site.Site{Domain: d, Slug: l.Slug, Status: site.Active, PHPVersion: cfg.PHPVersion}
	if found {
		switch old.Status {
		case site.Trashed:
			return nil, refuse("site_trashed", i18n.T(
				"site "+d.String()+" đang ở thùng rác; khôi phục nó thay vì tạo lại",
				"site "+d.String()+" is in the trash; restore it instead of creating it again"))
		case site.Active:
		default:
			return nil, fmt.Errorf("site %s có trạng thái lạ %q trong DB", d, old.Status)
		}
		// M13, quyết định 8: chạy lại ghi pool PHP và vhost thường, tức là
		// mở khoá lén một site đang tạm khoá.
		if old.Suspended != account.NotSuspended {
			return nil, refuse(CodeSiteSuspended, i18n.T(
				"site "+d.String()+" đang tạm khoá; chạy lại sẽ bật lại PHP của nó. Mở khoá trước: sudo tpan site unsuspend "+d.String(),
				"site "+d.String()+" is suspended; running it again would turn its PHP back on. Unsuspend it first: sudo tpan site unsuspend "+d.String()))
		}
		// Chạy lại: giữ bản PHP, ngày tạo và chủ của site (M12 cho mỗi site
		// một bản PHP; M13 không đổi chủ ở đây).
		if old.PHPVersion == "" {
			return nil, fmt.Errorf("site %s trong DB không có bản PHP", d)
		}
		rec = old
		rec.Status = site.Active
		if err := s.drift(ctx, l); err != nil {
			return nil, err
		}
		// Chạy lại không cần email: WordPress đã cài thì bước cài được bỏ qua và
		// email không dùng tới. Điền tạm admin@<domain> cho đủ kiểu; nếu hoá ra
		// WordPress chưa cài thì wpInstallStep từ chối thay vì cài với email đoán.
		if in.Email.IsZero() {
			em, err := validate.ParseEmail("admin@" + d.String())
			if err != nil {
				return nil, err
			}
			in.Email, emailGuessed = em, true
		}
	} else {
		if in.Email.IsZero() {
			return nil, refuse(validate.CodeEmailInvalid, i18n.T("thiếu email quản trị (--email): site mới cần email cho tài khoản quản trị WordPress", "the admin email is missing (--email): a new site needs an email for its WordPress admin account"))
		}
		if rec.Owner, err = s.newOwner(ctx, in.Owner); err != nil {
			return nil, err
		}
		vi, en, err := s.unmanaged(ctx, l)
		if err != nil {
			return nil, err
		}
		if len(vi) > 0 {
			return nil, refuse("site_exists_unmanaged", i18n.T(
				"domain "+d.String()+" chưa có trong tpan nhưng trên máy đã có: "+strings.Join(vi, ", ")+
					". Đó không phải site do tpan tạo (hoặc là phần còn sót của một lần hoàn tác lỗi); tpan không nhận làm của mình. Xem và dọn bằng tay rồi chạy lại.",
				"domain "+d.String()+" is not in tpan, but the machine already has: "+strings.Join(en, ", ")+
					". That is not a site tpan created (or it is left over from a failed undo); tpan will not take it over. Inspect and clean it up by hand, then run again."))
		}
	}

	dbPw, err := sitePassword()
	if err != nil {
		return nil, err
	}
	adminPw, err := sitePassword()
	if err != nil {
		return nil, err
	}
	others, err := s.others(ctx, d)
	if err != nil {
		return nil, err
	}
	version, hash := s.wordpress()
	// Site đã có SSL (chạy lại): URL https và vhost 443 (addPlan); không thì
	// chạy lại sẽ đè vhost 443 bằng bản cổng 80.
	scheme := "http://"
	if rec.SSL {
		scheme = "https://"
	}
	install := &wpInstallStep{emailGuessed: emailGuessed, WPInstall: &steps.WPInstall{
		Layout: l, URL: scheme + d.String(), Title: title,
		AdminUser: siteAdminUser, AdminEmail: in.Email, AdminPassword: adminPw,
	}}
	plan, err := addPlan(cfg.Web, rec.PHPVersion, l, dbPw, version, hash, install, rec.SSL)
	if err != nil {
		return nil, err
	}
	plan.Verify = siteChecks(cfg.Web, rec.PHPVersion, l, others, rec.SSL)
	return &SiteAdd{
		Site: rec, Layout: l, Rerun: found, AdminUser: siteAdminUser, Plan: plan,
		sites: s, adminPassword: adminPw, install: install,
	}, nil
}

// newOwner chọn và kiểm chủ của một site mới (M13): rỗng là admin; chủ phải
// là user có thật, đang hoạt động, và còn chỗ trong gói. Accounts nil (test,
// xem trước khi chưa có tpan.db) thì chỉ kiểm dạng tên.
func (s *Sites) newOwner(ctx context.Context, owner string) (string, error) {
	if owner == "" {
		return account.AdminName, nil
	}
	if _, err := validate.ParseUserName(owner); err != nil && owner != account.AdminName {
		return "", userNotFound(owner)
	}
	if s.Accounts == nil {
		return owner, nil
	}
	u, err := lookupUser(ctx, s.Accounts, owner)
	if err != nil {
		return "", err
	}
	if u.Status != account.Active {
		return "", refuse(CodeUserSuspended, i18n.T(
			"user "+owner+" đang bị khoá; mở khoá user trước rồi mới tạo site cho họ",
			"user "+owner+" is locked; unlock the user before creating a site for them"))
	}
	return owner, checkQuota(ctx, s.Accounts, owner, QuotaSites, validate.Domain{})
}

// addPlan dựng Plan "site.add" đúng thứ tự mục 6.2.
func addPlan(web WebServer, php string, l site.Layout, dbPw system.Secret, wpVersion, wpHash string, install *wpInstallStep, ssl bool) (engine.Plan, error) {
	wrap := func(err error) error { return fmt.Errorf("dựng Plan tạo site %s: %w", l.Domain, err) }

	// Bước 2, 3, 4: user Linux, thư mục và log, DB.
	ss := []engine.Step{&steps.SiteUser{Layout: l}}
	ss = append(ss, steps.SiteDirs(l)...)
	ss = append(ss, &steps.SiteDB{Layout: l, Password: dbPw})

	// Bước 5: pool PHP-FPM.
	pool, err := steps.SitePool(l, php)
	if err != nil {
		return engine.Plan{}, wrap(err)
	}
	// Bước 6, 7: WordPress từ cache, rồi wp-config.php.
	archive := &steps.WPArchive{Version: wpVersion, Hash: wpHash}
	ss = append(ss, pool, archive,
		&steps.WPFiles{Layout: l, Archive: archive.Path()},
		&steps.WPConfig{Layout: l, DBPassword: dbPw},
	)
	// Bước 7b (M9, mục 6.10): tắt cron theo lượt xem trong wp-config.php rồi
	// đặt cron thật. Ngay sau WPConfig vì WPConstant sửa chính file đó. Đặt
	// trước vhost (không để cuối, sau bước cài) vì hai lẽ: site chưa được phục
	// vụ thì lượt xem đầu tiên đã không còn gánh việc định kỳ; và job hàng loạt
	// (apply.go) tách bước cài xuống pha C, nên bước nào đứng sau bước cài sẽ
	// chạy TRƯỚC nó trong lô mà SAU nó khi tạo một site, hai đường lệch thứ tự.
	// File cron có trước khi WordPress được cài cũng không sao: `tpan cron run`
	// bỏ qua site chưa có (dòng sites chỉ được ghi khi job thành công).
	// Site tạo trước M9: chạy lại `tpan site add <domain>` thì hai bước này
	// Apply, các bước đã đúng được bỏ qua (đường nâng cấp, docs/thay-doi/M9.md).
	cron, err := siteCronSteps(l)
	if err != nil {
		return engine.Plan{}, wrap(err)
	}
	ss = append(ss, cron...)
	// Bước 8: vhost (Apache cần thêm .htaccess, phải có trước khi vhost được bật).
	if web == Apache {
		ht, err := steps.SiteHtaccess(l)
		if err != nil {
			return engine.Plan{}, wrap(err)
		}
		ss = append(ss, ht)
	}
	vhostOf := steps.SiteVhost
	if ssl {
		vhostOf = steps.SiteVhostSSL
	}
	vhost, err := vhostOf(l, string(web), php, acmeRoot)
	if err != nil {
		return engine.Plan{}, wrap(err)
	}
	ss = append(ss, vhost...)
	// Bước 9: cài WordPress, sau vhost để URL của site đã trả lời được.
	ss = append(ss, install)

	d := l.Domain.String()
	return engine.Plan{
		Kind:   "site.add",
		Target: d,
		Title:  i18n.T("Tạo site WordPress "+d, "Create WordPress site "+d),
		Steps:  ss,
	}, nil
}

// wpInstallStep bọc steps.WPInstall để biết `wp core install` có thật sự chạy
// không. Outcome "applied" của bước chưa đủ: WordPress đã cài mà chỉ permalink
// lệch thì Apply chỉ đặt lại permalink, mật khẩu quản trị mới không được dùng,
// và in nó ra là in một mật khẩu sai. Info, Check, Undo giữ nguyên của bước gốc.
type wpInstallStep struct {
	*steps.WPInstall
	ran bool
	// emailGuessed: chạy lại không có --email, AdminEmail là admin@<domain>
	// điền tạm. Khi đó không được cài WordPress mới bằng email đoán.
	emailGuessed bool
}

// Apply hỏi `wp core is-installed` (cùng câu chỉ đọc bước gốc dùng: mã 1 là
// "chưa cài") ngay trước khi giao cho bước gốc.
func (w *wpInstallStep) Apply(ctx context.Context, sys system.System) error {
	w.ran = false
	l := w.Layout
	_, err := sys.Run(ctx, system.Cmd{
		Name:   "wp",
		Args:   []string{"core", "is-installed", "--skip-plugins", "--skip-themes", "--path=" + l.Public},
		AsUser: l.User,
	})
	var ee *system.ExitError
	fresh := errors.As(err, &ee) && ee.Code == 1
	if fresh && w.emailGuessed {
		return fmt.Errorf("WordPress của %s chưa được cài: chạy lại với --email <email quản trị> để cài", l.Domain)
	}
	if err := w.WPInstall.Apply(ctx, sys); err != nil {
		return err
	}
	w.ran = fresh
	return nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (a *SiteAdd) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(a.sites.Sys, engine.Options{}).Preview(ctx, a.Plan)
}

// Execute chạy Plan qua engine. Job thành công thì ghi site vào bảng sites
// (chạy lại thì giữ ngày tạo cũ); job hỏng thì không ghi gì. error chỉ khác nil
// khi không chạy được hoặc không ghi được bảng sites; job hỏng thì xem
// Report.Job.Status. Kể cả khi error khác nil, kết quả vẫn mang mật khẩu quản
// trị nếu WordPress đã được cài: mất nó là mất tài khoản quản trị.
func (a *SiteAdd) Execute(ctx context.Context, onEvent func(engine.Event)) (SiteAddResult, error) {
	s := a.sites
	res := SiteAddResult{Site: a.Site, AdminUser: a.AdminUser}
	if s.Journal == nil {
		return res, errors.New("tạo site: thiếu Journal, không ghi được lịch sử job")
	}
	// Site mới chưa có dòng trong bảng sites lúc job bắt đầu: ghi chủ vào job
	// ngay, để admin tạo hộ thì user chủ vẫn thấy job tạo site của mình.
	if !a.Rerun && a.Site.Owner != "" {
		ctx = engine.WithOwner(ctx, a.Site.Owner)
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(ctx, a.Plan)
	res.Report = rep
	if err != nil {
		return res, fmt.Errorf("chạy %s: %w", a.Plan.Kind, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return res, nil
	}
	if a.install.ran {
		res.WPInstalled, res.AdminPassword = true, a.adminPassword
	}
	if res.Site.Created.IsZero() {
		res.Site.Created = time.Now().UTC()
	}
	if err := s.Store.SaveSite(ctx, res.Site); err != nil {
		return res, fmt.Errorf("job %s xong nhưng không ghi được site %s vào DB của tpan: %w. Máy đã có site mà tpan chưa ghi nhận, nên `tpan site add %s` sẽ bị từ chối là site không do tpan quản lý",
			rep.Job.ID, a.Site.Domain, err, a.Site.Domain)
	}
	return res, nil
}

// ---------------------------------------------------------------- đưa vào thùng rác

// SiteRemove là một lần đưa site vào thùng rác (mục 6.4), sẵn sàng xem trước
// hoặc chạy. Tên giữ từ M2 (lúc đó là xoá hẳn) để `site rm`, API và
// propose_site_remove không phải đổi chỗ gọi.
type SiteRemove struct {
	Site   site.Site
	Layout site.Layout
	Plan   engine.Plan

	sites *Sites
}

// PrepareRemove dựng Plan "site.rm": đưa site vào thùng rác theo mục 6.4,
// bước 1-4 (trashSteps). Mọi bước hoàn tác được, nên Plan có kiểm chứng: domain
// không còn được phục vụ. Kiểm chứng lỗi thì engine trả site về như cũ. Site
// đã ở thùng rác thì từ chối site_trashed.
func (s *Sites) PrepareRemove(ctx context.Context, d validate.Domain) (*SiteRemove, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	if st.Status == site.Trashed {
		return nil, refuse("site_trashed", i18n.T(
			"site "+d.String()+" đã ở thùng rác; xoá hẳn bằng: sudo tpan trash purge "+d.String(),
			"site "+d.String()+" is already in the trash; delete it for good with: sudo tpan trash purge "+d.String()))
	}
	if st.Suspended != account.NotSuspended || st.DBSuspended != account.NotSuspended {
		return nil, refuseSuspendedTrash(st)
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	if st.PHPVersion == "" {
		return nil, fmt.Errorf("site %s trong DB không có bản PHP", d)
	}
	l := st.Layout()
	ss, err := trashSteps(cfg.Web, st.PHPVersion, l)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan đưa site %s vào thùng rác: %w", d, err)
	}
	return &SiteRemove{Site: st, Layout: l, sites: s, Plan: engine.Plan{
		Kind:   "site.rm",
		Target: d.String(),
		Title:  i18n.T("Đưa site "+d.String()+" vào thùng rác", "Move site "+d.String()+" to the trash"),
		Steps:  ss,
		Verify: []checks.Check{siteGone(cfg.Web, l)},
	}}, nil
}

// trashSteps là các bước đưa site vào thùng rác, đúng thứ tự mục 6.4:
//
//  1. Gỡ link vhost (kiểm config, reload web) rồi gỡ pool (kiểm, reload PHP):
//     lấy từ steps.SiteWebAbsent của M2 nhưng BỎ bước gỡ file vhost trong
//     sites-available, để khôi phục chỉ cần bật lại link. Site có SSL thì link
//     là của vhost 443, cùng đường dẫn.
//  2. Khoá user DB; DB giữ nguyên.
//  3. Khoá user Linux (hạn tài khoản, lý do ở steps.SiteUserLock), rồi gỡ
//     file cron của site (M9, mục 6.10).
//  4. Chuyển /var/www/<domain> vào site.TrashDir bằng một lần rename.
//
// Dùng chung cho PrepareRemove và unit "đưa vào thùng rác" của job hàng loạt.
func trashSteps(web WebServer, php string, l site.Layout) ([]engine.Step, error) {
	webAbsent, err := steps.SiteWebAbsent(l, string(web), php)
	if err != nil {
		return nil, err
	}
	conf := l.NginxConf
	if web == Apache {
		conf = l.ApacheConf
	}
	d := l.Domain.String()
	why := i18n.T(
		"Web server và PHP-FPM thôi phục vụ "+d+" trước khi thư mục site bị chuyển đi, để khách không thấy lỗi lưng chừng. File vhost trong sites-available được giữ: khôi phục chỉ cần bật lại link. Bước này hoàn tác được.",
		"The web server and PHP-FPM stop serving "+d+" before the site directory is moved, so visitors never see a half-gone site. The vhost file in sites-available is kept: a restore only re-enables the link. This step can be undone.")
	var ss []engine.Step
	for _, st := range webAbsent {
		a, ok := st.(*steps.Absent)
		if !ok {
			return nil, fmt.Errorf("bước gỡ web của %s có kiểu lạ %T", d, st)
		}
		if a.Path == conf {
			continue
		}
		a.Why = why
		ss = append(ss, a)
	}
	// Chặn khi SiteWebAbsent đổi cách dựng: thiếu bước gỡ link hay gỡ pool thì
	// site vẫn được phục vụ từ một thư mục đã chuyển đi.
	if len(ss) != 2 {
		return nil, fmt.Errorf("bước gỡ web của %s: cần đúng hai bước (link vhost, pool), có %d", d, len(ss))
	}
	return append(ss,
		&steps.DBUserLock{Layout: l},
		&steps.SiteUserLock{Layout: l},
		// M9: gỡ file cron trước khi chuyển thư mục, để cron không gọi
		// `wp` vào một thư mục đang đi. Khôi phục ghi lại (restoreSteps).
		siteCronAbsent(l),
		&steps.MoveTree{From: l.Dir, To: site.TrashDir(l),
			Title: i18n.T("Chuyển thư mục của "+d+" vào thùng rác", "Move the directory of "+d+" to the trash")},
	), nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (r *SiteRemove) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(r.sites.Sys, engine.Options{}).Preview(ctx, r.Plan)
}

// Execute chạy Plan; job thành công thì ghi site là trashed kèm hạn xoá hẳn
// (markTrashed). Job hỏng thì engine đã trả máy về như cũ, dòng giữ active.
func (r *SiteRemove) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	s := r.sites
	if s.Journal == nil {
		return engine.Report{}, errors.New("đưa site vào thùng rác: thiếu Journal, không ghi được lịch sử job")
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(ctx, r.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s: %w", r.Plan.Kind, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return rep, nil
	}
	if err := s.markTrashed(ctx, r.Site); err != nil {
		// Chạy lại thì mọi bước đã đúng được bỏ qua, chỉ còn ghi DB.
		return rep, fmt.Errorf("job %s đã đưa site %s vào thùng rác nhưng không ghi được vào DB của tpan: %w. Chạy lại `tpan site rm %s` để ghi nốt",
			rep.Job.ID, r.Site.Domain, err, r.Site.Domain)
	}
	return rep, nil
}

// ---------------------------------------------------------------- xem

// List trả mọi site, sắp theo domain.
func (s *Sites) List(ctx context.Context) ([]site.Site, error) {
	return s.Store.Sites(ctx)
}

// SiteInfo là kết quả `tpan site info`.
type SiteInfo struct {
	Site   site.Site   `json:"site"`
	Layout site.Layout `json:"layout"`
	// Socket là socket của pool site đang dùng (theo bản PHP của site, M12).
	Socket string `json:"socket"`
	// Web là web server của stack; rỗng khi stack chưa cài (khi đó Checks rỗng).
	Web    WebServer       `json:"web"`
	Checks []checks.Result `json:"checks"`
}

// Info trả bản ghi, tên và đường dẫn của site, cùng kết quả các phép kiểm.
func (s *Sites) Info(ctx context.Context, d validate.Domain) (SiteInfo, error) {
	h, st, err := s.health(ctx, d)
	if err != nil {
		return SiteInfo{}, err
	}
	php := st.PHPVersion
	if php == "" {
		php = h.Config.PHPVersion
	}
	return SiteInfo{Site: st, Layout: st.Layout(), Socket: st.Layout().SocketFor(php), Web: h.Config.Web, Checks: h.Results}, nil
}

// Health chạy bộ phép kiểm của một site (mục 6.7): đường đi của request và cô
// lập với các site khác. Stack chưa cài thì không chạy gì (Installed false).
func (s *Sites) Health(ctx context.Context, d validate.Domain) (Health, error) {
	h, _, err := s.health(ctx, d)
	return h, err
}

func (s *Sites) health(ctx context.Context, d validate.Domain) (Health, site.Site, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return Health{}, site.Site{}, err
	}
	cfg, err := (&Stack{Settings: s.Settings}).Config(ctx)
	if err != nil {
		return Health{}, st, err
	}
	h := Health{Installed: cfg.Web != "" && cfg.PHPVersion != "", Config: cfg}
	if !h.Installed {
		return h, st, nil
	}
	php := st.PHPVersion
	if php == "" {
		php = cfg.PHPVersion
	}
	others, err := s.others(ctx, d)
	if err != nil {
		return Health{}, st, err
	}
	h.Results = checks.RunAll(ctx, s.Sys, siteChecks(cfg.Web, php, st.Layout(), others, st.SSL))
	return h, st, nil
}
