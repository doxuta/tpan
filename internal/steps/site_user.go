package steps

// Các bước dựng phần "cô lập" của một site (mục 5, "Cô lập từng site"; mục 6.2
// bước 2 và 3; mục 6.4 "Xoá hẳn"): user Linux riêng, thư mục với đúng quyền,
// file log PHP, và bước xoá cả cây thư mục khi xoá hẳn site.
//
// Tài liệu đã tra (Ubuntu 24.04, shadow 4.13):
//   - https://manpages.ubuntu.com/manpages/noble/man8/useradd.8.html
//   - https://manpages.ubuntu.com/manpages/noble/man8/userdel.8.html
//   - https://manpages.ubuntu.com/manpages/noble/man1/getent.1.html

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*SiteUser)(nil)
	_ engine.Step = (*SiteUserAbsent)(nil)
	_ engine.Step = (*OwnedFile)(nil)
	_ engine.Step = (*RemoveTree)(nil)
)

// userShell là shell của user site: không đăng nhập được (mục 5).
const userShell = "/usr/sbin/nologin"

// userName là tên user Linux mà các bước ở đây chịu đụng tới: chỉ tp_<slug>.
// Tên do LayoutOf sinh nên luôn khớp; kiểm lại ở đây là lớp chặn cuối, để một
// Layout dựng tay kiểu User "-f" hay "root" không bao giờ tới useradd/userdel
// (userdel nhầm "root" là mất máy). 32 là giới hạn độ dài tên của useradd.
var userName = regexp.MustCompile(`^tp_[a-z0-9_]{1,29}$`)

// userCheckLayout từ chối Layout zero hay có tên, đường dẫn lạ, trước khi chạy lệnh nào.
func userCheckLayout(l site.Layout) error {
	if l.Domain.IsZero() {
		return errors.New("site chưa có domain (Layout rỗng)")
	}
	if !userName.MatchString(l.User) {
		return fmt.Errorf("tên user Linux %q của site không hợp lệ (cần dạng tp_<slug>)", l.User)
	}
	return checkPath("thư mục site", l.Dir)
}

// userEntry là các trường của một dòng passwd mà tpan cần.
type userEntry struct {
	home, shell string
}

// userLookup hỏi user name qua `getent passwd`. getent trả mã 2 khi không có
// key đó: đây là câu trả lời "chưa có", không phải lỗi. Mã khác (1: thiếu
// tham số, 3: không liệt kê được) hay không chạy được getent mới là lỗi thật.
func userLookup(ctx context.Context, sys system.System, name string) (userEntry, bool, error) {
	res, err := sys.Run(ctx, userGetent(name))
	if ee, ok := isExit(err); ok && ee.Code == 2 {
		return userEntry{}, false, nil
	}
	if err != nil {
		return userEntry{}, false, withStdout(fmt.Errorf("hỏi user %s: %w", name, err), res)
	}
	// Dạng passwd(5): name:password:UID:GID:GECOS:home:shell.
	f := strings.Split(strings.TrimRight(res.Stdout, "\n"), ":")
	if len(f) != 7 || f[0] != name {
		return userEntry{}, false, fmt.Errorf("hỏi user %s: getent trả dòng lạ %q", name, res.Stdout)
	}
	return userEntry{home: f[5], shell: f[6]}, true, nil
}

func userGetent(name string) system.Cmd {
	return system.Cmd{Name: "getent", Args: []string{"passwd", name}}
}

// userOurs báo lỗi khi user cùng tên đã có mà home hay shell khác cái tpan
// đặt: đó không phải user do tpan tạo, tpan không sửa cũng không xoá nó.
func userOurs(l site.Layout, e userEntry) error {
	if e.home != l.Dir || e.shell != userShell {
		return fmt.Errorf("user %s đã có trên máy với home %q, shell %q (tpan đặt %q, %q): không phải user do tpan tạo, tpan không đụng tới",
			l.User, e.home, e.shell, l.Dir, userShell)
	}
	return nil
}

// ================================================================== SiteUser

// SiteUser đảm bảo user Linux l.User có, kèm nhóm riêng cùng tên. PHP của site
// chạy dưới user này; vì nó không thuộc nhóm www-data nên không đọc được file
// của site khác (mục 5).
type SiteUser struct {
	Layout site.Layout

	created bool // chính Apply đã chạy useradd
	applied bool
}

func (s *SiteUser) useradd() system.Cmd {
	l := s.Layout
	// --system: UID trong dải hệ thống, không có thông tin hết hạn mật khẩu.
	// --user-group: tạo nhóm cùng tên làm nhóm chính (không vào nhóm users hay www-data).
	// --no-create-home: thư mục site do SiteDirs tạo với quyền 2750, không để
	// useradd tạo bằng quyền mặc định của nó.
	// Không có cờ mật khẩu: tài khoản không có mật khẩu, cộng shell nologin là
	// không đăng nhập được bằng bất cứ đường nào.
	return system.Cmd{Name: "useradd", Args: []string{
		"--system", "--user-group",
		"--home-dir", l.Dir, "--no-create-home",
		"--shell", userShell,
		l.User,
	}}
}

func (s *SiteUser) Info() engine.StepInfo {
	u := s.Layout.User
	return engine.StepInfo{
		ID:    "site.user:" + u,
		Title: i18n.T("Tạo user Linux "+u+" cho site", "Create Linux user "+u+" for the site"),
		Why: i18n.T(
			"PHP của site chạy dưới user riêng này. Bỏ bước này thì PHP của mọi site chạy chung một user, và một site bị chiếm là đọc được wp-config.php (mật khẩu DB) của mọi site khác trên máy.",
			"The site's PHP runs as this dedicated user. Without it every site's PHP shares one user, so one compromised site can read the wp-config.php (database password) of every other site on the machine.",
		),
		Commands: []string{s.useradd().String()},
	}
}

func (s *SiteUser) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := userCheckLayout(s.Layout); err != nil {
		return false, err
	}
	e, found, err := userLookup(ctx, sys, s.Layout.User)
	if err != nil || !found {
		return false, err
	}
	if err := userOurs(s.Layout, e); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SiteUser) Apply(ctx context.Context, sys system.System) error {
	l := s.Layout
	if err := userCheckLayout(l); err != nil {
		return err
	}
	e, found, err := userLookup(ctx, sys, l.User)
	if err != nil {
		return fmt.Errorf("tạo user %s: %w", l.User, err)
	}
	if found {
		// Engine chỉ gọi Apply khi Check false, nên tới đây là user có mà lệch.
		if err := userOurs(l, e); err != nil {
			return err
		}
		s.created, s.applied = false, true
		return nil
	}
	if res, err := sys.Run(ctx, s.useradd()); err != nil {
		return withStdout(fmt.Errorf("tạo user %s: %w", l.User, err), res)
	}
	s.created, s.applied = true, true
	return nil
}

// Undo xoá user (userdel cũng xoá nhóm cùng tên, vì USERGROUPS_ENAB của
// Ubuntu là yes), chỉ khi chính bước này đã tạo.
func (s *SiteUser) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("user %s: %w", s.Layout.User, errNotApplied)
	}
	if !s.created {
		return nil
	}
	return userDel(ctx, sys, s.Layout.User)
}

// userDel chạy userdel không có -r (thư mục site do RemoveTree xoá, có kiểm
// đường dẫn) và không có -f (không xoá user đang có tiến trình chạy).
func userDel(ctx context.Context, sys system.System, name string) error {
	res, err := sys.Run(ctx, userDelCmd(name))
	ee, isEE := isExit(err)
	switch {
	case err == nil:
		return nil
	case isEE && ee.Code == 6:
		// 6: user không có. Đích "không còn user" đã đạt.
		return nil
	case isEE && ee.Code == 8:
		// 8: userdel từ chối vì còn tiến trình chạy dưới user (thường là worker
		// PHP-FPM của pool chưa thoát sau khi gỡ pool).
		return withStdout(fmt.Errorf("xoá user %s: còn tiến trình đang chạy dưới user này (thường là worker PHP-FPM chưa thoát); chờ vài giây rồi chạy lại: %w", name, err), res)
	}
	return withStdout(fmt.Errorf("xoá user %s: %w", name, err), res)
}

func userDelCmd(name string) system.Cmd {
	return system.Cmd{Name: "userdel", Args: []string{name}}
}

// ============================================================ SiteUserAbsent

// SiteUserAbsent xoá user của site và nhóm cùng tên (mục 6.4, "Xoá hẳn").
type SiteUserAbsent struct{ Layout site.Layout }

func (s *SiteUserAbsent) Info() engine.StepInfo {
	u := s.Layout.User
	return engine.StepInfo{
		ID:    "site.user.absent:" + u,
		Title: i18n.T("Xoá user Linux "+u, "Delete Linux user "+u),
		Why: i18n.T(
			"Site đã bị xoá thì user của nó không còn việc gì. Để lại thì UID cũ có thể bị một user mới dùng lại, và file sót lại của site cũ thuộc về user mới đó.",
			"Once the site is gone its user has no purpose. If it stays, a later user may reuse the old UID and become the owner of any files the old site left behind.",
		),
		Commands: []string{userDelCmd(u).String()},
		Danger:   true,
		NoUndo: i18n.T(
			"Tạo lại user sẽ ra UID khác; muốn có lại site thì tạo lại cả site.",
			"Recreating the user gives it a different UID; to get the site back, create the whole site again.",
		),
	}
}

func (s *SiteUserAbsent) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := userCheckLayout(s.Layout); err != nil {
		return false, err
	}
	e, found, err := userLookup(ctx, sys, s.Layout.User)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	// User cùng tên mà không phải của tpan thì không xoá.
	if err := userOurs(s.Layout, e); err != nil {
		return false, err
	}
	return false, nil
}

func (s *SiteUserAbsent) Apply(ctx context.Context, sys system.System) error {
	l := s.Layout
	if err := userCheckLayout(l); err != nil {
		return err
	}
	e, found, err := userLookup(ctx, sys, l.User)
	if err != nil {
		return fmt.Errorf("xoá user %s: %w", l.User, err)
	}
	if !found {
		return nil
	}
	if err := userOurs(l, e); err != nil {
		return err
	}
	return userDel(ctx, sys, l.User)
}

func (s *SiteUserAbsent) Undo(context.Context, system.System) error { return nil }

// ================================================================== SiteDirs

// SiteDirs trả các bước dựng thư mục của site theo mục 5, đúng thứ tự: thư mục
// cha trước thư mục con, thư mục log trước file log.
func SiteDirs(l site.Layout) []engine.Step {
	web := system.Owner{User: l.User, Group: site.WebGroup}
	own := system.Owner{User: l.User, Group: l.User}
	return []engine.Step{
		// 2750: web server đọc qua nhóm www-data; setgid để file PHP tạo ra
		// (ảnh tải lên) mang nhóm www-data; "khác" không có quyền gì.
		&Dir{Path: l.Dir, Perm: fs.ModeSetgid | 0o750, Owner: web},
		&Dir{Path: l.Public, Perm: fs.ModeSetgid | 0o750, Owner: web},
		// Upload tạm và session PHP: chỉ user của site, web server cũng không đọc.
		&Dir{Path: l.Tmp, Perm: 0o700, Owner: own},
		// Thư mục log thuộc root để logrotate không phải đổi tên file trong
		// thư mục do user của site kiểm soát (mục 5).
		&Dir{Path: l.LogDir, Perm: 0o755, Owner: system.Root},
		&OwnedFile{Path: l.PHPErrorLog, Perm: 0o640, Owner: own},
	}
}

// ================================================================= OwnedFile

// OwnedFile đảm bảo một file có, đúng quyền và chủ, không quan tâm nội dung.
// Dùng cho file log mà chương trình khác ghi (php-error.log): File của M1 so
// nội dung nên sẽ ghi đè log.
type OwnedFile struct {
	Path  string
	Perm  fs.FileMode
	Owner system.Owner

	created   bool
	prevPerm  fs.FileMode
	prevOwner system.Owner
	applied   bool
}

// Info không có Files, có chủ ý: engine ghi sha256 của FileChange.New vào nhật
// ký file sau khi Apply, nên khai file log ở đây thì health sẽ báo "lệch" ngay
// khi PHP ghi dòng log đầu tiên. Bước này cũng không ghi nội dung nào.
func (s *OwnedFile) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "owned-file:" + s.Path,
		Title: i18n.T("Tạo "+s.Path+" với đúng quyền và chủ", "Create "+s.Path+" with the right mode and owner"),
		Why: i18n.T(
			"Chương trình chạy dưới user của site (PHP) ghi vào file này nhưng không được tạo file trong thư mục của root. Thiếu file hoặc sai chủ thì lỗi PHP không được ghi lại, lúc site hỏng không có gì để xem.",
			"A program running as the site's user (PHP) writes to this file but may not create files in root's directory. If the file is missing or has the wrong owner, PHP errors are lost and there is nothing to read when the site breaks.",
		),
	}
}

func (s *OwnedFile) validate() error {
	if err := checkPath("đường dẫn file", s.Path); err != nil {
		return err
	}
	return checkPerm(s.Perm)
}

// stat trả thông tin file, hoặc lỗi nếu ở đó không phải file thường. Symlink bị
// từ chối hẳn chứ không "sửa": Stat không đi theo symlink nhưng Chmod thì có,
// nên chmod qua một symlink do user của site đặt sẽ đổi quyền của đích (ví dụ
// /etc/shadow) và trao nó cho user đó.
func (s *OwnedFile) stat(sys system.System) (system.FileInfo, bool, error) {
	fi, err := sys.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return fi, false, nil
	}
	if err != nil {
		return fi, false, fmt.Errorf("kiểm %s: %w", s.Path, err)
	}
	if !fi.Mode.IsRegular() {
		return fi, true, fmt.Errorf("%s đã có và không phải file thường (%v); tpan không đụng tới", s.Path, fi.Mode.Type())
	}
	return fi, true, nil
}

func (s *OwnedFile) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, exists, err := s.stat(sys)
	if err != nil || !exists {
		return false, err
	}
	return fi.Mode.Perm() == s.Perm && normOwner(fi.Owner) == normOwner(s.Owner), nil
}

func (s *OwnedFile) Apply(_ context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	owner := normOwner(s.Owner)
	fi, exists, err := s.stat(sys)
	if err != nil {
		return err
	}
	if !exists {
		// File rỗng; thư mục cha phải có sẵn (bước Dir đứng trước).
		if err := sys.WriteFileAtomic(s.Path, nil, s.Perm, owner); err != nil {
			return fmt.Errorf("tạo %s: %w", s.Path, err)
		}
		s.created, s.applied = true, true
		return nil
	}
	// Có sẵn: giữ nguyên nội dung, chỉ sửa quyền và chủ; nhớ giá trị cũ để Undo.
	prevPerm, prevOwner := fi.Mode.Perm(), normOwner(fi.Owner)
	if err := setPermOwner(sys, s.Path, s.Perm, owner); err != nil {
		if rerr := setPermOwner(sys, s.Path, prevPerm, prevOwner); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	s.created, s.prevPerm, s.prevOwner, s.applied = false, prevPerm, prevOwner, true
	return nil
}

func (s *OwnedFile) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("file %s: %w", s.Path, errNotApplied)
	}
	if s.created {
		if err := sys.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("xoá %s đã tạo: %w", s.Path, err)
		}
		return nil
	}
	return setPermOwner(sys, s.Path, s.prevPerm, s.prevOwner)
}

// ================================================================ RemoveTree

// RemoveTree xoá cả một cây thư mục (mục 6.4, "Xoá hẳn"): thư mục site, thư
// mục log của site.
type RemoveTree struct {
	Path  string
	Under string
}

// rm không có -f: thư mục biến mất giữa chừng thì báo lỗi chứ không im lặng.
// --one-file-system: không đi xuống một phân vùng khác lỡ được mount bên trong
// (ví dụ ai đó bind-mount /home vào thư mục site). Không có -f thì rm chỉ hỏi
// lại khi stdin là terminal; tpan không chạy lệnh với terminal nên không bị treo.
func (s *RemoveTree) rm() system.Cmd {
	return system.Cmd{Name: "rm", Args: []string{"-r", "--one-file-system", "--", s.Path}}
}

// Info không có Files, có chủ ý: Preview đọc từng file trong Files để vẽ diff,
// mà đọc một thư mục thì lỗi (không phải "chưa có"), làm hỏng cả bản xem trước.
func (s *RemoveTree) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "remove-tree:" + s.Path,
		Title: i18n.T("Xoá hẳn "+s.Path+" và mọi thứ bên trong", "Permanently delete "+s.Path+" and everything in it"),
		Why: i18n.T(
			"Site đã bị xoá thì mã nguồn và file tải lên của nó không còn ai dùng. Để lại thì chúng chiếm đĩa, và lỗ hổng trong mã cũ vẫn nằm trên máy.",
			"Once the site is deleted nobody uses its code and uploads. Left behind they take disk space, and any vulnerability in the old code stays on the machine.",
		),
		Commands: []string{s.rm().String()},
		Danger:   true,
		NoUndo: i18n.T(
			"File đã xoá không lấy lại được. Muốn giữ thì backup trước khi xoá.",
			"Deleted files cannot be recovered. Take a backup first if you need them.",
		),
	}
}

// validate là lớp chặn cuối trước một lệnh rm -r chạy bằng root: Path phải là
// đường dẫn chuẩn nằm hẳn bên dưới Under. checkPath đã từ chối "", "/", đường
// dẫn tương đối và đường dẫn chưa chuẩn (có "..", "//", "/" ở cuối), nên phép so
// tiền tố dưới đây không bị lừa bởi "/var/www/../etc".
func (s *RemoveTree) validate() error {
	if err := checkPath("thư mục giới hạn", s.Under); err != nil {
		return err
	}
	if err := checkPath("thư mục cần xoá", s.Path); err != nil {
		return err
	}
	if !strings.HasPrefix(s.Path, s.Under+"/") {
		return fmt.Errorf("thư mục cần xoá %q không nằm hẳn bên dưới %q", s.Path, s.Under)
	}
	return nil
}

func (s *RemoveTree) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	_, err := sys.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Path, err)
	}
	return false, nil
}

func (s *RemoveTree) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, s.rm()); err != nil {
		return withStdout(fmt.Errorf("xoá %s: %w", s.Path, err), res)
	}
	return nil
}

func (s *RemoveTree) Undo(context.Context, system.System) error { return nil }
