package core

// Ca sử dụng của site WordPress (mục 6.2, 6.4 "Ở M2", 6.7 của docs/KE-HOACH.md):
// tạo, chạy lại, xoá hẳn, liệt kê, xem và kiểm sức khoẻ một site.
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
	// Thư mục cha của thư mục site và thư mục log: RemoveTree chỉ xoá bên dưới.
	siteWWWRoot = "/var/www"
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
				"Lấy lại wp-config.php từ backup, hoặc nếu không cần dữ liệu thì xoá hẳn bằng `sudo tpan site rm "+d+"` rồi tạo lại.",
			"site "+d+" has drifted: database user "+l.DB+" exists but "+l.WPConfig+" does not. tpan does not know the password the database user has, so it will not write a new wp-config.php. "+
				"Restore wp-config.php from a backup, or if the data is not needed, delete the site with `sudo tpan site rm "+d+"` and create it again."))
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
		// Chạy lại: giữ bản PHP và ngày tạo của site (M12 cho mỗi site một bản PHP).
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

// ---------------------------------------------------------------- xoá site

// SiteRemove là một lần xoá hẳn site (mục 6.4, "Ở M2"), sẵn sàng xem trước hoặc chạy.
type SiteRemove struct {
	Site   site.Site
	Layout site.Layout
	Plan   engine.Plan

	sites *Sites
}

// PrepareRemove dựng Plan "site.rm": gỡ vhost và pool trước (hoàn tác được),
// rồi mới xoá DB, file, user (không hoàn tác được). Không có phép kiểm chứng:
// kiểm chứng Fail sẽ làm engine hoàn tác, mà hoàn tác lúc này chỉ trả lại vhost
// và pool cho một site đã mất DB và file, tệ hơn là để yên.
func (s *Sites) PrepareRemove(ctx context.Context, d validate.Domain) (*SiteRemove, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return nil, err
	}
	if st.PHPVersion == "" {
		return nil, fmt.Errorf("site %s trong DB không có bản PHP", d)
	}
	l := st.Layout()
	web, err := steps.SiteWebAbsent(l, string(cfg.Web), st.PHPVersion)
	if err != nil {
		return nil, fmt.Errorf("dựng Plan xoá site %s: %w", d, err)
	}
	ss := append(web,
		&steps.SiteDBAbsent{Layout: l},
		&steps.RemoveTree{Path: l.Dir, Under: siteWWWRoot},
		&steps.RemoveTree{Path: l.LogDir, Under: siteLogRoot},
		&steps.SiteUserAbsent{Layout: l},
	)
	if st.SSL {
		// Xoá chứng chỉ ở CUỐI (mục 6.4): bước nào trước đó lỗi thì engine hoàn
		// tác trả lại vhost 443, mà vhost đó trỏ tới chứng chỉ; chứng chỉ đã mất
		// thì kiểm config lúc hoàn tác lỗi theo. Để cuối thì lỗi ở đây chỉ còn
		// sót chứng chỉ, chạy lại `site rm` dọn nốt.
		ss = append(ss, &steps.CertbotDelete{Layout: l})
	}
	return &SiteRemove{Site: st, Layout: l, sites: s, Plan: engine.Plan{
		Kind:   "site.rm",
		Target: d.String(),
		Title:  i18n.T("Xoá hẳn site "+d.String(), "Permanently delete site "+d.String()),
		Steps:  ss,
	}}, nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (r *SiteRemove) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(r.sites.Sys, engine.Options{}).Preview(ctx, r.Plan)
}

// Execute chạy Plan; job thành công thì xoá dòng của site khỏi bảng sites. Job
// hỏng thì giữ dòng, để chạy lại `tpan site rm` làm nốt phần còn lại.
func (r *SiteRemove) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	s := r.sites
	if s.Journal == nil {
		return engine.Report{}, errors.New("xoá site: thiếu Journal, không ghi được lịch sử job")
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(ctx, r.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s: %w", r.Plan.Kind, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return rep, nil
	}
	if err := s.Store.DeleteSite(ctx, r.Site.Domain); err != nil {
		return rep, fmt.Errorf("job %s đã xoá site %s khỏi máy nhưng không xoá được dòng trong DB của tpan: %w", rep.Job.ID, r.Site.Domain, err)
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
