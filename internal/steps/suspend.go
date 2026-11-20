package steps

// Các bước tạm khoá site (M13, mục 6.12 của docs/KE-HOACH.md): vhost bản
// "tạm khoá", trang tạm khoá chung, và đổi tên file pool PHP-FPM, file cron
// của site sang ".suspended" (và ngược lại).
//
// Vì sao đổi tên chứ không xoá: PHP-FPM chỉ nạp file *.conf trong pool.d (dòng
// include=/etc/php/<bản>/fpm/pool.d/*.conf của php-fpm.conf), cron của Debian
// bỏ qua file trong /etc/cron.d có dấu chấm trong tên (đã dựa vào ở M9, xem
// site.CronFile). Đổi tên là tắt mà vẫn giữ nguyên file trên đĩa để người vận
// hành thấy và để mở khoá trả lại đúng file đó.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// SuspendedSuffix là đuôi thêm vào file pool và file cron của site tạm khoá.
const SuspendedSuffix = ".suspended"

// ===================================================================== Rename

// Rename đổi tên From thành To trong cùng thư mục (rename nguyên tử), rồi kiểm
// cú pháp (Test) và nạp lại dịch vụ (Reload). Kiểm sai thì đổi tên ngược lại.
//
// managed_files: engine gọi Info một lần trước khi chạy và chỉ ghi file của
// bước đã Apply. From luôn được khai Delete (engine bỏ dòng của nó: health
// không còn kiểm lệch rồi báo "mất" một file đã cố ý tắt). Content khác nil
// thì To được khai với nội dung đó và engine ghi sha256 của nó: dùng khi mở
// khoá, Content là bản template mà tpan coi là chuẩn. Nếu file .suspended
// mang bản sửa tay người dùng đã nhận làm chuẩn từ trước, health sẽ báo lệch
// sau khi mở khoá, và người dùng nhận lại được (thấy được, sửa được; tốt hơn
// là lặng lẽ nhận mọi thứ nằm trong file .suspended làm chuẩn).
type Rename struct {
	From, To string
	Content  []byte
	Test     *system.Cmd
	Reload   *system.Cmd
	// WaitFor như File.WaitFor: chờ socket của pool sau khi reload.
	WaitFor string
	Danger  bool
	Title   i18n.Text
	Why     i18n.Text

	prev    saved // To trước khi Apply (thường là chưa có)
	applied bool
}

var _ engine.Step = (*Rename)(nil)

func (s *Rename) Info() engine.StepInfo {
	fc := []engine.FileChange{{Path: s.From, Delete: true}}
	if s.Content != nil {
		fc = append(fc, engine.FileChange{Path: s.To, New: s.Content})
	}
	return engine.StepInfo{
		ID:    "rename:" + s.From,
		Title: orText(s.Title, i18n.T("Đổi tên "+s.From+" thành "+s.To, "Rename "+s.From+" to "+s.To)),
		Why: orText(s.Why, i18n.T(
			"Dịch vụ chỉ nạp file có đúng tên. Đổi tên là bật hoặc tắt file mà không mất nội dung của nó. Bước này hoàn tác được.",
			"The service only loads files with the right name. Renaming turns the file on or off without losing its content. This step can be undone.")),
		Commands: cmdStrings(s.Test, s.Reload),
		Files:    fc,
		Danger:   s.Danger,
	}
}

func (s *Rename) validate() error {
	if err := checkPath("đường dẫn cần đổi tên", s.From); err != nil {
		return err
	}
	if err := checkPath("tên mới", s.To); err != nil {
		return err
	}
	if s.From == s.To {
		return fmt.Errorf("đổi tên %s: tên mới trùng tên cũ", s.From)
	}
	return nil
}

// Check: đạt khi From không còn. Đích của bước là "dịch vụ không còn nạp
// From"; From không có (đã đổi, hoặc chưa từng có) thì đích đó đã đúng.
func (s *Rename) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, err := sys.Stat(s.From)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.From, err)
	}
	if !fi.Mode.IsRegular() {
		return false, fmt.Errorf("%s không phải file thường; tpan chỉ đổi tên file của nó", s.From)
	}
	return false, nil
}

func (s *Rename) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	// To đã có (file .suspended sót từ một lần trước, hay file pool ai đó tạo
	// lại): rename đè lên nó, nên giữ nội dung để Undo trả về. capture từ chối
	// thư mục, nên không bao giờ đè lên một thư mục.
	prev, err := capture(sys, s.To)
	if err != nil {
		return fmt.Errorf("đổi tên %s: %w", s.From, err)
	}
	if err := sys.Rename(s.From, s.To); err != nil {
		return fmt.Errorf("đổi tên %s thành %s: %w", s.From, s.To, err)
	}
	what := "đổi tên " + s.From
	if err := runTest(ctx, sys, s.Test, what); err != nil {
		return errors.Join(err, s.back(sys, prev))
	}
	if err := runReload(ctx, sys, s.Reload, what); err != nil {
		if rerr := s.back(sys, prev); rerr != nil {
			return errors.Join(err, rerr)
		}
		return errors.Join(err, runReload(ctx, sys, s.Reload, "trả lại "+s.From))
	}
	s.prev, s.applied = prev, true
	if s.Reload != nil && s.WaitFor != "" {
		waitForPath(ctx, sys, s.WaitFor)
	}
	return nil
}

// back đổi tên ngược lại và trả To về như trước Apply.
func (s *Rename) back(sys system.System, prev saved) error {
	if err := sys.Rename(s.To, s.From); err != nil {
		return fmt.Errorf("đổi tên %s về %s: %w", s.To, s.From, err)
	}
	if prev.exists {
		return prev.restore(sys)
	}
	return nil
}

func (s *Rename) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("đổi tên %s: %w", s.From, errNotApplied)
	}
	if err := s.back(sys, s.prev); err != nil {
		return err
	}
	what := "trả lại " + s.From
	if err := runTest(ctx, sys, s.Test, what); err != nil {
		return err
	}
	return runReload(ctx, sys, s.Reload, what)
}

// ============================================================ Site tạm khoá

// SiteVhostSuspended ghi vhost bản tạm khoá (templates.SuspendedVhost) vào
// sites-available rồi bật bằng symlink, cùng đường dẫn, Test và Reload như
// SiteVhost: tạm khoá là ghi đè vhost đang có, mở khoá là ghi đè ngược lại.
func SiteVhostSuspended(l site.Layout, web, acmeRoot string, ssl bool) ([]engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước vhost tạm khoá: %w", err)
	}
	v, err := webVhostOf(l, web)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost tạm khoá: %w", err)
	}
	conf, err := templates.SuspendedVhost(l, web, acmeRoot, ssl)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost tạm khoá: %w", err)
	}
	d := l.Domain.String()
	return []engine.Step{
		&File{Path: v.conf, Content: conf, Perm: 0o644, Owner: system.Root, Test: v.test, Reload: v.reload, Danger: true,
			Title: i18n.T("Ghi vhost "+web+" bản tạm khoá cho "+d, "Write the suspended "+web+" vhost for "+d),
			Why: i18n.T(
				"Vhost này cho "+web+" trả 403 kèm trang \"Site đang tạm khoá\" cho mọi đường dẫn của "+d+", không đọc mã nguồn và không chuyển gì cho PHP; riêng đường thử thách của Let's Encrypt vẫn đi qua để chứng chỉ gia hạn được. Thiếu nó thì site vẫn được phục vụ như thường.",
				"This vhost makes "+web+" answer every path of "+d+" with 403 and the \"site suspended\" page, without reading the site's code or passing anything to PHP; only Let's Encrypt's challenge path still goes through so the certificate keeps renewing. Without it the site is still served as usual.")},
		&Symlink{Target: v.conf, Link: v.link, Test: v.test, Reload: v.reload},
	}, nil
}

// SuspendedPage ghi trang tạm khoá chung (templates.SuspendedPage) và thư mục
// của nó: root, thư mục 0755, file 0644, để web server (www-data) đọc được.
// Không có Test hay Reload: web server đọc file này ở mỗi request.
//
// Một bước cho mỗi site (ID "suspended.page:<domain>") dù cùng một file: khoá
// user dồn bước tạm khoá của mọi site vào một Plan, mà ID bước phải duy nhất
// trong Plan. Bước của site đầu tiên ghi file; các bước sau thấy file đã đúng
// và được bỏ qua.
func SuspendedPage(l site.Layout) (engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước trang tạm khoá: %w", err)
	}
	return &suspendedPage{
		domain: l.Domain.String(),
		dir:    &Dir{Path: templates.SuspendedDir, Perm: 0o755, Owner: system.Root},
		file:   SuspendedPageFile(),
	}, nil
}

// SuspendedPageFile là riêng bước ghi file trang tạm khoá (thư mục đã có),
// cho sửa tự động "file.rewrite" khi phép kiểm lệch báo file này bị sửa tay.
func SuspendedPageFile() *File {
	return &File{Path: templates.SuspendedPage, Content: templates.SuspendedHTML(), Perm: 0o644, Owner: system.Root,
		Title: i18n.T("Ghi trang tạm khoá "+templates.SuspendedPage, "Write the suspension page "+templates.SuspendedPage),
		Why: i18n.T(
			"Vhost tạm khoá của mọi site trả trang này kèm mã 403. Thiếu nó thì khách vẫn bị chặn nhưng thấy trang lỗi trơn của web server thay vì lời báo site đang tạm khoá, và phép kiểm http.suspended không đạt.",
			"Every suspended vhost answers with this page and status 403. Without it visitors are still blocked but see the web server's bare error page instead of the suspension notice, and the http.suspended check fails.")}
}

// suspendedPage gộp Dir và File của trang tạm khoá thành một bước có ID riêng
// cho từng site (xem SuspendedPage).
type suspendedPage struct {
	domain string
	dir    *Dir
	file   *File

	dirApplied, fileApplied bool
}

func (s *suspendedPage) Info() engine.StepInfo {
	info := s.file.Info()
	info.ID = "suspended.page:" + s.domain
	return info
}

func (s *suspendedPage) Check(ctx context.Context, sys system.System) (bool, error) {
	ok, err := s.dir.Check(ctx, sys)
	if err != nil || !ok {
		return false, err
	}
	return s.file.Check(ctx, sys)
}

func (s *suspendedPage) Apply(ctx context.Context, sys system.System) error {
	s.dirApplied, s.fileApplied = false, false
	ok, err := s.dir.Check(ctx, sys)
	if err != nil {
		return err
	}
	if !ok {
		if err := s.dir.Apply(ctx, sys); err != nil {
			return err
		}
		s.dirApplied = true
	}
	if err := s.file.Apply(ctx, sys); err != nil {
		if s.dirApplied {
			return errors.Join(err, s.dir.Undo(ctx, sys))
		}
		return err
	}
	s.fileApplied = true
	return nil
}

func (s *suspendedPage) Undo(ctx context.Context, sys system.System) error {
	if !s.fileApplied {
		return fmt.Errorf("trang tạm khoá: %w", errNotApplied)
	}
	if err := s.file.Undo(ctx, sys); err != nil {
		return err
	}
	if s.dirApplied {
		return s.dir.Undo(ctx, sys)
	}
	return nil
}

// PoolSuspend tắt pool PHP-FPM của site: đổi tên tpan-<slug>.conf thành
// tpan-<slug>.conf.suspended, `php-fpm<v> -t`, reload. PHP-FPM gỡ pool (và
// socket của nó) khi nạp lại; pool tpan-idle (M12) giữ cho PHP-FPM luôn còn
// ít nhất một pool để chạy được.
func PoolSuspend(l site.Layout, phpVersion string) (engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước tắt pool: %w", err)
	}
	test, reload, err := webPHPCmds(phpVersion)
	if err != nil {
		return nil, fmt.Errorf("dựng bước tắt pool: %w", err)
	}
	conf := l.PoolConf(phpVersion)
	return &Rename{From: conf, To: conf + SuspendedSuffix, Test: test, Reload: reload, Danger: true,
		Title: i18n.T("Tắt pool PHP-FPM "+l.Pool+" (đổi tên thành .suspended)", "Turn off PHP-FPM pool "+l.Pool+" (rename to .suspended)"),
		Why: i18n.T(
			"Site đang tạm khoá thì không được chạy PHP nào của nó, kể cả khi ai đó gọi thẳng socket. PHP-FPM chỉ nạp file .conf, nên đổi tên là tắt pool mà vẫn giữ file để mở khoá trả lại. Bước này hoàn tác được.",
			"A suspended site must not run any of its PHP, even if someone talks to the socket directly. PHP-FPM only loads .conf files, so renaming turns the pool off while keeping the file for unsuspending. This step can be undone.")}, nil
}

// PoolUnsuspend làm ngược PoolSuspend, rồi chờ socket của pool (như
// File.WaitFor của SitePool). Content là bản template của pool để engine ghi
// lại sha256 vào managed_files (xem Rename).
func PoolUnsuspend(l site.Layout, phpVersion string) (engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước bật lại pool: %w", err)
	}
	test, reload, err := webPHPCmds(phpVersion)
	if err != nil {
		return nil, fmt.Errorf("dựng bước bật lại pool: %w", err)
	}
	content, err := templates.SitePool(l, phpVersion)
	if err != nil {
		return nil, fmt.Errorf("dựng bước bật lại pool: %w", err)
	}
	conf := l.PoolConf(phpVersion)
	return &Rename{From: conf + SuspendedSuffix, To: conf, Content: content, Test: test, Reload: reload,
		WaitFor: l.SocketFor(phpVersion),
		Title:   i18n.T("Bật lại pool PHP-FPM "+l.Pool, "Turn PHP-FPM pool "+l.Pool+" back on"),
		Why: i18n.T(
			"Mở khoá site cần PHP của nó chạy lại: đổi tên file .suspended về .conf rồi nạp lại PHP-FPM. Thiếu bước này thì site mở khoá chỉ trả 502. Bước này hoàn tác được.",
			"Unsuspending the site needs its PHP running again: rename the .suspended file back to .conf and reload PHP-FPM. Without this step the unsuspended site only answers 502. This step can be undone.")}, nil
}

// CronSuspend tắt cron WordPress của site: /etc/cron.d/tpan-<slug> thành
// tpan-<slug>.suspended. Không cần reload: cron đọc lại /etc/cron.d khi thời
// điểm sửa của thư mục đổi (M9).
func CronSuspend(l site.Layout) (engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước tắt cron: %w", err)
	}
	f := site.CronFile(l)
	return &Rename{From: f, To: f + SuspendedSuffix,
		Title: i18n.T("Tắt cron WordPress của "+l.Domain.String()+" (đổi tên thành .suspended)", "Turn off the WordPress cron of "+l.Domain.String()+" (rename to .suspended)"),
		Why: i18n.T(
			"Site đang tạm khoá thì việc định kỳ của nó (gửi thư, gọi ra ngoài) cũng phải dừng. Cron bỏ qua file có dấu chấm trong tên, nên đổi tên là tắt mà vẫn giữ file. Bước này hoàn tác được.",
			"A suspended site's scheduled work (sending mail, calling out) must stop too. Cron ignores files with a dot in their name, so renaming turns it off while keeping the file. This step can be undone.")}, nil
}

// CronUnsuspend làm ngược CronSuspend; Content là bản template (xem Rename).
func CronUnsuspend(l site.Layout) (engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước bật lại cron: %w", err)
	}
	content, err := templates.SiteCron(l)
	if err != nil {
		return nil, fmt.Errorf("dựng bước bật lại cron: %w", err)
	}
	f := site.CronFile(l)
	return &Rename{From: f + SuspendedSuffix, To: f, Content: content,
		Title: i18n.T("Bật lại cron WordPress của "+l.Domain.String(), "Turn the WordPress cron of "+l.Domain.String()+" back on"),
		Why: i18n.T(
			"Mở khoá site thì việc định kỳ của WordPress (bài hẹn giờ, thư) chạy lại. Thiếu bước này thì bài hẹn giờ không bao giờ được đăng. Bước này hoàn tác được.",
			"Unsuspending the site restarts WordPress's scheduled work (scheduled posts, mail). Without this step scheduled posts never get published. This step can be undone.")}, nil
}
