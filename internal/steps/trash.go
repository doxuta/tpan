package steps

// Hợp đồng M6, đợt 0: các Step của thùng rác (mục 6.4 của docs/KE-HOACH.md).
// Thân viết ở đợt 1. Hàm và biến không xuất của file này mang tiền tố "trash".
//
// Đưa site vào thùng rác (core dựng Plan, theo đúng thứ tự mục 6.4):
//
//  1. Absent link vhost (Test, Reload web) và Absent pool (Test, Reload PHP):
//     dùng lại Absent của M1, KHÔNG gỡ file vhost trong sites-available (khôi
//     phục chỉ cần bật lại link).
//  2. DBUserLock: ALTER USER ... ACCOUNT LOCK; DB giữ nguyên.
//  3. SiteUserLock: usermod --expiredate 1 (cho tài khoản hết hạn; đặc tả ghi
//     usermod -L, lý do đổi ở chú thích của SiteUserLock).
//  4. MoveTree{From: l.Dir, To: site.TrashDir(l)}: rename.
//
// Khôi phục làm ngược lại: MoveTree{From: TrashDir, To: l.Dir}, SiteUserUnlock,
// DBUserUnlock (của M4, fix.go), rồi ghi pool và bật vhost như tạo site.
//
// Mọi bước kiểm Layout bằng site.LayoutOf như site_web.go (webCheckLayout) và
// checkPath cho mọi đường dẫn. SQL qua stdin của mariadb như site_db.go.
//
// Tài liệu đã tra (Ubuntu 24.04, shadow 4.13):
//   - https://manpages.ubuntu.com/manpages/noble/man8/usermod.8.html (--expiredate)
//   - https://manpages.ubuntu.com/manpages/noble/man1/chage.1.html (-l)
//   - rename(2): khác phân vùng thì trả EXDEV, không bao giờ chép.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"syscall"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*DBUserLock)(nil)
	_ engine.Step = (*SiteUserLock)(nil)
	_ engine.Step = (*MoveTree)(nil)
)

// ================================================================= DBUserLock

// DBUserLock khoá user DB của site: ALTER USER '<db>'@'localhost' ACCOUNT LOCK
// (dùng lại dbLockSQL, dbLockStateSQL của fix.go). Check: user có và đang bị
// khoá; user không có thì lỗi (không có gì để khoá là máy lệch). Undo: mở khoá
// nếu chính Apply đã khoá. Danger: site thôi đăng nhập được DB.
type DBUserLock struct {
	Layout site.Layout

	applied bool
	locked  bool // chính Apply đã khoá
}

func (s *DBUserLock) Info() engine.StepInfo {
	l := s.Layout
	return engine.StepInfo{
		ID: "db.lock:" + l.Domain.String(),
		Title: i18n.T(
			"Khoá user DB "+l.DB+" của site "+l.Domain.String(),
			"Lock the database user "+l.DB+" of "+l.Domain.String(),
		),
		Why: i18n.T(
			"Site đang vào thùng rác: DB được giữ nguyên để khôi phục, nhưng không ai được đăng nhập vào nó nữa. Không khoá thì mã PHP cũ (hay ai giữ mật khẩu trong wp-config.php) vẫn đọc, ghi được DB của một site đã xoá. Khoá không đổi mật khẩu hay quyền nào, khôi phục chỉ cần mở khoá.",
			"The site is going to the trash: its database is kept for a restore, but nobody may log in to it any more. Without the lock, old PHP code (or anyone holding the password in wp-config.php) can still read and write the database of a deleted site. Locking changes no password or privilege; a restore only unlocks it.",
		),
		Commands: []string{dbShow(dbLockStateSQL(l.DB)), dbShow(dbLockSQL(l.DB))},
		Danger:   true,
	}
}

// state đọc trạng thái bằng đúng hàm của DBUserUnlock (cùng câu SQL, cùng cách
// kiểm Layout), rồi coi "user không có" là lỗi: Plan thùng rác chỉ chạy cho
// site đã có DB, user mất là máy lệch, người vận hành cần biết.
func (s *DBUserLock) state(ctx context.Context, sys system.System) (bool, error) {
	exists, locked, err := (&DBUserUnlock{Layout: s.Layout}).lockState(ctx, sys)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, fmt.Errorf("user DB %s không có, không có gì để khoá (máy lệch với dữ liệu của tpan)", s.Layout.DB)
	}
	return locked, nil
}

func (s *DBUserLock) Check(ctx context.Context, sys system.System) (bool, error) {
	return s.state(ctx, sys)
}

func (s *DBUserLock) Apply(ctx context.Context, sys system.System) error {
	locked, err := s.state(ctx, sys)
	if err != nil {
		return err
	}
	name := s.Layout.DB
	if !locked {
		if err := dbRun(ctx, sys, dbLockSQL(name), "", "khoá user DB "+name); err != nil {
			return err
		}
	}
	// Undo không mở khoá một user vốn đã bị khoá từ trước.
	s.applied, s.locked = true, !locked
	return nil
}

func (s *DBUserLock) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("khoá user DB %s: %w", s.Layout.DB, errNotApplied)
	}
	if !s.locked {
		return nil
	}
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := dbRun(ctx, sys, dbUnlockSQL(s.Layout.DB), "", "mở khoá lại user DB "+s.Layout.DB); err != nil {
		return err
	}
	s.locked = false
	return nil
}

// =============================================================== SiteUserLock

// SiteUserLock khoá (Unlock=false) hoặc mở khoá (Unlock=true) user Linux của
// site bằng hạn tài khoản: khoá là `usermod --expiredate 1 l.User` (hết hạn từ
// ngày 2/1/1970), mở là `usermod --expiredate "" l.User` (đối số rỗng: không
// hạn). Check đọc `chage -l l.User` (LC_ALL=C do system.Run cố định), lấy dòng
// "Account expires", phần sau dấu ":" đầu tiên: "never" là chưa khoá, giá trị
// khác là đã khoá. User không có thì lỗi. Undo: làm ngược lại nếu chính Apply
// đã đổi. ID: "user.lock:<domain>" hoặc "user.unlock:<domain>".
//
// Đặc tả mục 6.4 ghi `usermod -L`; đổi sang hạn tài khoản vì đã kiểm trên máy
// ảo Ubuntu 24.04 (shadow 4.13) thấy -L/-U không đổi được trạng thái đọc được:
//   - User của site tạo bằng useradd không -p nên shadow là "!"; `passwd -S`
//     báo "L" ngay từ đầu.
//   - `usermod -L` thoát 0 mà `passwd -S` vẫn "L".
//   - `usermod -U` in "usermod: unlocking the user's password would result in
//     a passwordless account." và vẫn thoát 0, `passwd -S` vẫn "L".
//
// Hạn tài khoản thì đổi được và đọc lại được: `chage -l` có dòng
// "Account expires\t\t\t\t\t\t: never"; sau `usermod --expiredate 1` (thoát 0)
// thành ": Jan 02, 1970"; sau `usermod --expiredate ""` (thoát 0) về ": never".
// `runuser -u` chạy từ root vẫn chạy được với user hết hạn (root bỏ qua kiểm
// tài khoản), nên PHP và WP-CLI do tpan gọi không bị ảnh hưởng; cron và đăng
// nhập qua PAM thì bị chặn.
type SiteUserLock struct {
	Layout site.Layout
	Unlock bool

	applied bool
	changed bool
}

func trashChage(name string) system.Cmd {
	return system.Cmd{Name: "chage", Args: []string{"-l", name}}
}

// trashUsermod: lock=true là usermod --expiredate 1, false là
// usermod --expiredate "" (phần tử argv rỗng, không phải chuỗi có dấu nháy).
func trashUsermod(name string, lock bool) system.Cmd {
	date := ""
	if lock {
		date = "1"
	}
	return system.Cmd{Name: "usermod", Args: []string{"--expiredate", date, name}}
}

func (s *SiteUserLock) Info() engine.StepInfo {
	l := s.Layout
	d, u := l.Domain.String(), l.User
	if s.Unlock {
		return engine.StepInfo{
			ID:    "user.unlock:" + d,
			Title: i18n.T("Mở khoá user Linux "+u+" của "+d, "Unlock Linux user "+u+" of "+d),
			Why: i18n.T(
				"Site được lấy ra khỏi thùng rác thì user của nó phải về như lúc tạo site: bỏ hạn tài khoản. Để hết hạn thì cron của site không chạy (WordPress không đăng bài hẹn giờ, không gửi mail định kỳ), và `chage -l` vẫn cho thấy user như đang ở thùng rác.",
				"When the site comes back from the trash its user must return to how it was when the site was created: the account expiry is removed. If it stays expired, the site's cron jobs do not run (WordPress stops publishing scheduled posts and sending periodic mail), and `chage -l` still shows the user as trashed.",
			),
			Commands: []string{trashChage(u).String(), trashUsermod(u, false).String()},
		}
	}
	return engine.StepInfo{
		ID:    "user.lock:" + d,
		Title: i18n.T("Khoá user Linux "+u+" của "+d, "Lock Linux user "+u+" of "+d),
		Why: i18n.T(
			"User của site vốn không có mật khẩu và không có shell; cho tài khoản hết hạn là lớp chặn thứ hai cho một site đã xoá: cron của nó thôi chạy, không đăng nhập được qua PAM, và `chage -l` cho thấy rõ user này đang ở thùng rác. Bước này không đổi file hay mật khẩu nào và hoàn tác được.",
			"The site's user already has no password and no shell; expiring the account is a second barrier for a deleted site: its cron jobs stop, it cannot log in through PAM, and `chage -l` shows clearly that this user is in the trash. This step changes no file or password and can be undone.",
		),
		Commands: []string{trashChage(u).String(), trashUsermod(u, true).String()},
	}
}

// locked trả user có đang bị khoá không. Kiểm Layout, hỏi getent để chắc user
// có và là của tpan (không khoá nhầm một user trùng tên do người khác tạo), rồi
// đọc dòng "Account expires" của `chage -l`.
func (s *SiteUserLock) locked(ctx context.Context, sys system.System) (bool, error) {
	l := s.Layout
	if err := webCheckLayout(l); err != nil {
		return false, err
	}
	if err := userCheckLayout(l); err != nil {
		return false, err
	}
	e, found, err := userLookup(ctx, sys, l.User)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("user Linux %s không có, không khoá hay mở khoá được (máy lệch với dữ liệu của tpan)", l.User)
	}
	if err := userOurs(l, e); err != nil {
		return false, err
	}
	res, err := sys.Run(ctx, trashChage(l.User))
	if err != nil {
		return false, withStdout(fmt.Errorf("đọc trạng thái khoá của user %s: %w", l.User, err), res)
	}
	// Dòng cần: "Account expires\t\t\t\t\t\t: never" hoặc ": Jan 02, 1970".
	for _, line := range strings.Split(res.Stdout, "\n") {
		if !strings.HasPrefix(line, "Account expires") {
			continue
		}
		_, v, ok := strings.Cut(line, ":")
		v = strings.TrimSpace(v)
		if !ok || v == "" {
			return false, fmt.Errorf("đọc trạng thái khoá của user %s: chage -l trả dòng lạ %q", l.User, line)
		}
		// Ngày khác "never" (kể cả hạn do người khác đặt) đều coi là đã khoá.
		return v != "never", nil
	}
	return false, fmt.Errorf("đọc trạng thái khoá của user %s: chage -l không có dòng \"Account expires\": %q", l.User, res.Stdout)
}

func (s *SiteUserLock) Check(ctx context.Context, sys system.System) (bool, error) {
	locked, err := s.locked(ctx, sys)
	return err == nil && locked != s.Unlock, err
}

func (s *SiteUserLock) Apply(ctx context.Context, sys system.System) error {
	locked, err := s.locked(ctx, sys)
	if err != nil {
		return err
	}
	changed := locked == s.Unlock
	if changed {
		if err := trashRunUsermod(ctx, sys, s.Layout.User, !s.Unlock); err != nil {
			return err
		}
	}
	// Undo không đụng tới user vốn đã ở đúng trạng thái trước khi bước chạy.
	s.applied, s.changed = true, changed
	return nil
}

func (s *SiteUserLock) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("khoá user %s: %w", s.Layout.User, errNotApplied)
	}
	if !s.changed {
		return nil
	}
	if err := userCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := trashRunUsermod(ctx, sys, s.Layout.User, s.Unlock); err != nil {
		return err
	}
	s.changed = false
	return nil
}

func trashRunUsermod(ctx context.Context, sys system.System, name string, lock bool) error {
	what := "mở khoá user " + name
	if lock {
		what = "khoá user " + name
	}
	if res, err := sys.Run(ctx, trashUsermod(name, lock)); err != nil {
		return withStdout(fmt.Errorf("%s: %w", what, err), res)
	}
	return nil
}

// =================================================================== MoveTree

// trashRoots là hai gốc MoveTree được phép đụng tới: chỗ web server tìm site và
// thùng rác. Ngoài hai chỗ này, một lệnh rename bằng root có thể chuyển mất
// /etc hay /home.
var trashRoots = []string{"/var/www", site.TrashRoot}

// MoveTree chuyển cả một thư mục From sang To bằng system.Rename (mục 6.4,
// bước 4): cùng phân vùng thì tức thì và không bao giờ có bản chép dở.
//
//   - From và To phải là đường dẫn tuyệt đối sạch, mỗi cái nằm hẳn bên dưới
//     một trong hai gốc được phép: "/var/www" và site.TrashRoot (không bằng gốc,
//     không ra ngoài gốc, giống RemoveTree). Sai thì Check và Apply đều lỗi.
//   - Check: From không còn VÀ To có (là thư mục, Stat không theo symlink).
//     From còn và To cũng có: lỗi (hai bản, không đoán được bản nào đúng).
//     Cả hai không có: lỗi.
//   - Apply: MkdirAll thư mục cha của To (0700 root, chỉ phần chưa có) rồi
//     Rename. Rename trả lỗi khác phân vùng (EXDEV, errors.Is(err,
//     syscall.EXDEV)) thì trả lỗi nói rõ: "/var/www và /var/lib/tpan nằm trên
//     hai phân vùng khác nhau; tpan chưa hỗ trợ chép rồi xoá" (ponytail: chép
//     rồi xoá chưa làm, mục 16 ghi máy ảo cùng phân vùng; VPS cần đo).
//   - Undo: Rename To → From nếu chính Apply đã chuyển.
//   - Danger: site thôi phục vụ (file không còn ở chỗ web server tìm).
//   - ID: "dir.move:<From>". Title, Why do người gọi đặt; trống thì dùng câu
//     mặc định.
type MoveTree struct {
	From, To   string
	Title, Why i18n.Text

	applied bool
	moved   bool
	created []string // thư mục cha của To mà Apply đã tạo, từ ngoài vào trong
}

func (s *MoveTree) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "dir.move:" + s.From,
		Title: orText(s.Title, i18n.T("Chuyển "+s.From+" sang "+s.To, "Move "+s.From+" to "+s.To)),
		Why: orText(s.Why, i18n.T(
			"Chuyển cả thư mục bằng một lần đổi tên: không chép, không xoá file nào, nên dữ liệu không bao giờ nằm dở dang ở hai nơi. Bỏ bước này thì file của site vẫn ở chỗ cũ, site chưa thật sự vào (hay ra khỏi) thùng rác.",
			"The whole directory is moved with a single rename: nothing is copied or deleted, so the data is never left half in two places. Without this step the site's files stay where they were and the site is not really in (or out of) the trash.",
		)),
		Danger: true,
	}
}

// trashUnder cho biết p nằm hẳn bên dưới một trong hai gốc (không bằng gốc).
// p đã qua checkPath nên phép so tiền tố không bị lừa bởi "..".
func trashUnder(p string) bool {
	for _, r := range trashRoots {
		if strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

func (s *MoveTree) validate() error {
	for _, p := range []struct{ what, p string }{{"thư mục nguồn", s.From}, {"thư mục đích", s.To}} {
		if err := checkPath(p.what, p.p); err != nil {
			return err
		}
		if !trashUnder(p.p) {
			return fmt.Errorf("%s %q không nằm hẳn bên dưới %s", p.what, p.p, strings.Join(trashRoots, " hoặc "))
		}
	}
	if s.From == s.To || strings.HasPrefix(s.To, s.From+"/") || strings.HasPrefix(s.From, s.To+"/") {
		return fmt.Errorf("không chuyển được %s sang %s: trùng nhau hoặc cái này nằm trong cái kia", s.From, s.To)
	}
	return nil
}

// trashExists hỏi p có không (Stat không theo symlink).
func trashExists(sys system.System, p string) (system.FileInfo, bool, error) {
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return fi, false, nil
	}
	if err != nil {
		return fi, false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	return fi, true, nil
}

// state trả done (From không còn, To là thư mục) hoặc lỗi khi máy ở trạng thái
// không đoán được; done=false, err=nil nghĩa là From còn và To chưa có.
func (s *MoveTree) state(sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fromFI, fromOK, err := trashExists(sys, s.From)
	if err != nil {
		return false, err
	}
	toFI, toOK, err := trashExists(sys, s.To)
	if err != nil {
		return false, err
	}
	switch {
	case fromOK && toOK:
		return false, fmt.Errorf("cả %s lẫn %s đều có: hai bản của cùng một site, tpan không đoán bản nào đúng; xem và dọn bằng tay", s.From, s.To)
	case !fromOK && !toOK:
		return false, fmt.Errorf("cả %s lẫn %s đều không có: không còn gì để chuyển", s.From, s.To)
	case toOK && !toFI.Mode.IsDir():
		return false, fmt.Errorf("%s có nhưng không phải thư mục (%v)", s.To, toFI.Mode.Type())
	case toOK:
		return true, nil
	case !fromFI.Mode.IsDir():
		// Symlink ở chỗ thư mục site: rename sẽ chỉ chuyển cái link, dữ liệu thật
		// vẫn nằm ở chỗ nó trỏ tới.
		return false, fmt.Errorf("%s không phải thư mục (%v); tpan chỉ chuyển thư mục thật", s.From, fromFI.Mode.Type())
	}
	return false, nil
}

func (s *MoveTree) Check(_ context.Context, sys system.System) (bool, error) {
	return s.state(sys)
}

func (s *MoveTree) Apply(_ context.Context, sys system.System) error {
	done, err := s.state(sys)
	if err != nil {
		return err
	}
	if done {
		s.applied, s.moved, s.created = true, false, nil
		return nil
	}
	parent := path.Dir(s.To)
	created, err := missingDirs(sys, parent)
	if err != nil {
		return fmt.Errorf("chuyển %s: %w", s.From, err)
	}
	// 0700 root: thùng rác nằm dưới /var/lib/tpan, web server và user của site
	// khác không được đọc file của site đã xoá.
	if err := sys.MkdirAll(parent, 0o700, system.Root); err != nil {
		err = fmt.Errorf("tạo thư mục %s: %w", parent, err)
		if rerr := removeDirs(sys, created); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	if err := sys.Rename(s.From, s.To); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			err = fmt.Errorf("chuyển %s sang %s: /var/www và /var/lib/tpan nằm trên hai phân vùng khác nhau; tpan chưa hỗ trợ chép rồi xoá: %w", s.From, s.To, err)
		} else {
			err = fmt.Errorf("chuyển %s sang %s: %w", s.From, s.To, err)
		}
		// Engine không Undo bước có Apply lỗi: tự dọn thư mục cha vừa tạo.
		if rerr := removeDirs(sys, created); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	s.applied, s.moved, s.created = true, true, created
	return nil
}

func (s *MoveTree) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("chuyển %s: %w", s.From, errNotApplied)
	}
	if s.moved {
		if err := s.validate(); err != nil {
			return err
		}
		// rename(2) đè được lên một thư mục rỗng: lỡ có gì đã xuất hiện ở From
		// thì dừng, không đè.
		if _, ok, err := trashExists(sys, s.From); err != nil || ok {
			if err == nil {
				err = fmt.Errorf("trả %s về %s: %s đã có lại, tpan không đè", s.To, s.From, s.From)
			}
			return err
		}
		if err := sys.Rename(s.To, s.From); err != nil {
			return fmt.Errorf("trả %s về %s: %w", s.To, s.From, err)
		}
		s.moved = false
	}
	// Job hàng loạt: site khác có thể đã vào chung thùng rác sau khi bước này
	// tạo nó. Thư mục cha còn đồ của site khác thì để lại (vô hại), không coi là
	// hoàn tác lỗi. Linux trả ENOTEMPTY; POSIX cho phép EEXIST.
	if err := removeDirs(sys, s.created); err != nil &&
		!errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
		return err
	}
	s.created = nil
	return nil
}
