package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// ======================================================================= File

// File đảm bảo một file có đúng nội dung, quyền và chủ. Dùng cho mọi file
// config tpan sinh từ template (vhost, pool PHP, logrotate, 99-tpan.cnf).
//
// Sau khi ghi, Test (ví dụ `nginx -t`) kiểm cú pháp; sai thì file cũ được trả
// về và dịch vụ không bao giờ thấy config hỏng. Đạt thì Reload (ví dụ
// `systemctl reload nginx`) để dịch vụ dùng config mới.
type File struct {
	Path    string
	Content []byte
	Perm    fs.FileMode
	Owner   system.Owner
	Test    *system.Cmd // nil: không có lệnh kiểm cú pháp
	Reload  *system.Cmd // nil: không cần nạp lại dịch vụ
	Danger  bool
	// Title và Why rỗng thì dùng câu chung cho mọi file config. Người dựng
	// Plan đặt câu riêng khi biết file này là gì (pool, vhost của một site).
	Title i18n.Text
	Why   i18n.Text
	// WaitFor khác rỗng: sau khi Reload chạy xong, chờ (tối đa waitForTimeout)
	// tới khi đường dẫn này có. Dùng cho pool PHP-FPM: `systemctl reload
	// php8.3-fpm` chỉ gửi USR2 cho tiến trình chính rồi trả về ngay (10 ms),
	// socket của pool mới có sau đó khoảng 175 ms (đo trên máy ảo, M6). Không
	// chờ thì phép kiểm chứng php.socket chạy ngay sau đó (khôi phục site từ
	// thùng rác) thấy "không có socket" và job bị hoàn tác oan. Hết giờ mà
	// chưa có thì không báo lỗi: kiểm chứng của Plan tự nói socket thiếu.
	// Reload nil (WithoutReload, pha A của job hàng loạt) thì không chờ.
	WaitFor string

	prev    saved
	applied bool
	// secret (M18): chỉ SecretFile đặt được; File thường từ chối ba file bí
	// mật (IsSecretPath), vì Info của File khai nội dung mới vào bản xem trước.
	secret bool
}

func (s *File) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "file:" + s.Path,
		Title: orText(s.Title, i18n.T("Ghi "+s.Path, "Write "+s.Path)),
		Why: orText(s.Why, i18n.T(
			"Dịch vụ đọc cấu hình từ file này. Thiếu nó hoặc sai nội dung thì dịch vụ chạy theo cấu hình khác với cấu hình tpan đã kiểm. tpan kiểm cú pháp trước khi nạp, sai thì trả file cũ về.",
			"The service reads its configuration from this file. If it is missing or wrong, the service runs with a configuration tpan has not checked. tpan checks the syntax before reloading and puts the old file back if the check fails.",
		)),
		Commands: cmdStrings(s.Test, s.Reload),
		Files:    []engine.FileChange{{Path: s.Path, New: s.Content}},
		Danger:   s.Danger,
	}
}

func (s *File) validate() error {
	if err := checkPath("đường dẫn file", s.Path); err != nil {
		return err
	}
	if err := secretGuard(s.Path, s.secret); err != nil {
		return err
	}
	return checkPerm(s.Perm)
}

func (s *File) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, err := sys.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Path, err)
	}
	// Symlink hay thứ khác ở chỗ này thì chưa đạt: Apply sẽ báo lỗi hoặc thay
	// bằng file thường. Không đi theo symlink, vì file tpan quản lý phải là của tpan.
	if !fi.Mode.IsRegular() || fi.Mode.Perm() != s.Perm || normOwner(fi.Owner) != normOwner(s.Owner) {
		return false, nil
	}
	data, err := sys.ReadFile(s.Path)
	if err != nil {
		return false, fmt.Errorf("đọc %s: %w", s.Path, err)
	}
	return string(data) == string(s.Content), nil
}

func (s *File) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	prev, err := capture(sys, s.Path)
	if err != nil {
		return fmt.Errorf("ghi %s: %w", s.Path, err)
	}
	if err := sys.WriteFileAtomic(s.Path, s.Content, s.Perm, normOwner(s.Owner)); err != nil {
		return fmt.Errorf("ghi %s: %w", s.Path, err)
	}
	if err := verifyOrRestore(ctx, sys, prev, s.Test, s.Reload, "ghi "+s.Path); err != nil {
		return err
	}
	s.prev, s.applied = prev, true
	if s.Reload != nil && s.WaitFor != "" {
		waitForPath(ctx, sys, s.WaitFor)
	}
	return nil
}

func (s *File) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("file %s: %w", s.Path, errNotApplied)
	}
	if err := s.prev.restore(sys); err != nil {
		return err
	}
	what := "trả lại " + s.Path
	if err := runTest(ctx, sys, s.Test, what); err != nil {
		return err
	}
	return runReload(ctx, sys, s.Reload, what)
}

// ==================================================================== Symlink

// Symlink đảm bảo Link là symlink trỏ tới Target. Dùng để bật một vhost:
// sites-enabled/x.conf → sites-available/x.conf (cách Debian và Ubuntu bật site).
type Symlink struct {
	Target string
	Link   string
	Test   *system.Cmd
	Reload *system.Cmd

	prev    saved
	applied bool
}

func (s *Symlink) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "symlink:" + s.Link,
		Title: i18n.T("Bật "+s.Link+" → "+s.Target, "Enable "+s.Link+" → "+s.Target),
		Why: i18n.T(
			"Web server chỉ nạp các file nằm trong sites-enabled. File ở sites-available mà không có link này thì site coi như tắt: request đi vào site khác hoặc bị từ chối.",
			"The web server only loads files in sites-enabled. Without this link the file in sites-available is ignored, and requests go to another site or are refused.",
		),
		Commands: cmdStrings(s.Test, s.Reload),
	}
}

func (s *Symlink) validate() error {
	if err := checkPath("đích của symlink", s.Target); err != nil {
		return err
	}
	return checkPath("đường dẫn symlink", s.Link)
}

func (s *Symlink) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, err := sys.Stat(s.Link)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm symlink %s: %w", s.Link, err)
	}
	if fi.Mode&fs.ModeSymlink == 0 {
		// File thật ở chỗ link: có thể ai đó đã chép config vào đây. Không
		// đè lên dữ liệu đó; báo để người vận hành xem.
		return false, fmt.Errorf("%s đã có và không phải symlink; tpan không ghi đè", s.Link)
	}
	got, err := sys.Readlink(s.Link)
	if err != nil {
		return false, fmt.Errorf("đọc đích của %s: %w", s.Link, err)
	}
	return got == s.Target, nil
}

func (s *Symlink) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	prev, err := capture(sys, s.Link)
	if err != nil {
		return fmt.Errorf("bật %s: %w", s.Link, err)
	}
	if prev.exists && prev.link == "" {
		return fmt.Errorf("bật %s: đã có và không phải symlink; tpan không ghi đè", s.Link)
	}
	if err := replaceSymlink(sys, s.Target, s.Link); err != nil {
		return fmt.Errorf("bật %s: %w", s.Link, err)
	}
	if err := verifyOrRestore(ctx, sys, prev, s.Test, s.Reload, "bật "+s.Link); err != nil {
		return err
	}
	s.prev, s.applied = prev, true
	return nil
}

func (s *Symlink) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("symlink %s: %w", s.Link, errNotApplied)
	}
	if err := s.prev.restore(sys); err != nil {
		return err
	}
	what := "trả lại " + s.Link
	if err := runTest(ctx, sys, s.Test, what); err != nil {
		return err
	}
	return runReload(ctx, sys, s.Reload, what)
}

// ===================================================================== Absent

// Absent đảm bảo một file hoặc symlink không còn. Dùng để gỡ site mặc định của
// distro (sites-enabled/default của Nginx, 000-default.conf của Apache) và gỡ
// vhost, pool của một site. Undo trả lại đúng như cũ, nên không có dữ liệu nào
// mất hẳn.
type Absent struct {
	Path   string
	Test   *system.Cmd
	Reload *system.Cmd
	// Danger: gỡ file này làm một dịch vụ ngừng phục vụ (vhost của site).
	Danger bool
	// Title và Why rỗng thì dùng câu nói về site mặc định của distro (cách
	// dùng ở M1).
	Title i18n.Text
	Why   i18n.Text

	prev    saved
	applied bool
	// secret (M18): chỉ SecretAbsent đặt được; Absent thường từ chối ba file
	// bí mật, vì engine đọc nội dung cũ của mọi file trong Info vào bản xem trước.
	secret bool
}

func (s *Absent) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "absent:" + s.Path,
		Title: orText(s.Title, i18n.T("Gỡ "+s.Path, "Remove "+s.Path)),
		Why: orText(s.Why, i18n.T(
			"Site mặc định của distro hứng mọi request không khớp tên miền nào và trả trang chào của nó. Để lại thì site mặc định của tpan (trả 444 hoặc 403) không bao giờ được dùng, và người lạ thấy trang mặc định.",
			"The distribution's default site catches every request that matches no domain and shows its welcome page. If it stays, tpan's default site (which returns 444 or 403) is never used and strangers see the welcome page.",
		)),
		Commands: cmdStrings(s.Test, s.Reload),
		Files:    []engine.FileChange{{Path: s.Path, Delete: true}},
		Danger:   s.Danger,
	}
}

// orText trả t, hoặc def khi t rỗng.
func orText(t, def i18n.Text) i18n.Text {
	if t.IsZero() {
		return def
	}
	return t
}

func (s *Absent) Check(_ context.Context, sys system.System) (bool, error) {
	if err := checkPath("đường dẫn cần gỡ", s.Path); err != nil {
		return false, err
	}
	if err := secretGuard(s.Path, s.secret); err != nil {
		return false, err
	}
	fi, err := sys.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Path, err)
	}
	if fi.Mode.IsDir() {
		return false, fmt.Errorf("%s là thư mục; bước này chỉ gỡ file hoặc symlink", s.Path)
	}
	return false, nil
}

func (s *Absent) Apply(ctx context.Context, sys system.System) error {
	if err := checkPath("đường dẫn cần gỡ", s.Path); err != nil {
		return err
	}
	if err := secretGuard(s.Path, s.secret); err != nil {
		return err
	}
	// capture từ chối thư mục, nên Remove dưới đây không bao giờ xoá thư mục.
	prev, err := capture(sys, s.Path)
	if err != nil {
		return fmt.Errorf("gỡ %s: %w", s.Path, err)
	}
	if prev.exists {
		if err := sys.Remove(s.Path); err != nil {
			return fmt.Errorf("gỡ %s: %w", s.Path, err)
		}
	}
	if err := verifyOrRestore(ctx, sys, prev, s.Test, s.Reload, "gỡ "+s.Path); err != nil {
		return err
	}
	s.prev, s.applied = prev, true
	return nil
}

func (s *Absent) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("gỡ %s: %w", s.Path, errNotApplied)
	}
	if err := s.prev.restore(sys); err != nil {
		return err
	}
	what := "trả lại " + s.Path
	if err := runTest(ctx, sys, s.Test, what); err != nil {
		return err
	}
	return runReload(ctx, sys, s.Reload, what)
}

// ======================================================================== Dir

// Dir đảm bảo một thư mục có, với đúng quyền và chủ. Thư mục có sẵn mà sai
// quyền hay chủ thì sửa lại.
type Dir struct {
	Path  string
	Perm  fs.FileMode
	Owner system.Owner

	created   []string // các thư mục bước này tạo, từ ngoài vào trong
	prevPerm  fs.FileMode
	prevOwner system.Owner
	applied   bool
}

func (s *Dir) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "dir:" + s.Path,
		Title: i18n.T("Tạo thư mục "+s.Path, "Create directory "+s.Path),
		Why: i18n.T(
			"Các bước sau ghi file vào thư mục này. Thiếu thư mục thì chúng lỗi; sai quyền hay sai chủ thì hoặc dịch vụ không đọc được, hoặc người không nên đọc lại đọc được.",
			"Later steps write files into this directory. Without it they fail; with the wrong mode or owner either the service cannot read it or someone who should not can.",
		),
	}
}

func (s *Dir) validate() error {
	if err := checkPath("đường dẫn thư mục", s.Path); err != nil {
		return err
	}
	return checkDirPerm(s.Perm)
}

func (s *Dir) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fi, err := sys.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm thư mục %s: %w", s.Path, err)
	}
	if !fi.Mode.IsDir() {
		// Symlink tới thư mục cũng bị từ chối: chmod sẽ đổi quyền của đích, là
		// thứ tpan không định đụng tới.
		return false, fmt.Errorf("%s đã có và không phải thư mục", s.Path)
	}
	return dirMode(fi.Mode) == s.Perm && normOwner(fi.Owner) == normOwner(s.Owner), nil
}

func (s *Dir) Apply(_ context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	owner := normOwner(s.Owner)
	fi, err := sys.Stat(s.Path)
	switch {
	case err == nil && !fi.Mode.IsDir():
		return fmt.Errorf("tạo thư mục %s: đã có và không phải thư mục", s.Path)
	case err == nil:
		// Có sẵn: chỉ sửa quyền và chủ, nhớ giá trị cũ để Undo.
		s.prevPerm, s.prevOwner, s.created = dirMode(fi.Mode), normOwner(fi.Owner), nil
		if err := setPermOwner(sys, s.Path, s.Perm, owner); err != nil {
			if rerr := setPermOwner(sys, s.Path, s.prevPerm, s.prevOwner); rerr != nil {
				return errors.Join(err, rerr)
			}
			return err
		}
	case errors.Is(err, fs.ErrNotExist):
		created, err := missingDirs(sys, s.Path)
		if err != nil {
			return err
		}
		// Thư mục cha còn thiếu tạo bằng 0755 root như mọi thư mục hệ thống;
		// chỉ thư mục đích nhận Perm và Owner riêng.
		err = sys.MkdirAll(path.Dir(s.Path), 0o755, system.Root)
		if err == nil {
			err = sys.MkdirAll(s.Path, s.Perm, owner)
		}
		if err == nil {
			// Đặt lại tường minh: trên máy thật umask có thể đã bớt quyền lúc tạo.
			err = setPermOwner(sys, s.Path, s.Perm, owner)
		}
		if err != nil {
			err = fmt.Errorf("tạo thư mục %s: %w", s.Path, err)
			if rerr := removeDirs(sys, created); rerr != nil {
				return errors.Join(err, rerr)
			}
			return err
		}
		s.created = created
	default:
		return fmt.Errorf("kiểm thư mục %s: %w", s.Path, err)
	}
	s.applied = true
	return nil
}

func (s *Dir) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("thư mục %s: %w", s.Path, errNotApplied)
	}
	if len(s.created) > 0 {
		return removeDirs(sys, s.created)
	}
	return setPermOwner(sys, s.Path, s.prevPerm, s.prevOwner)
}

// setPermOwner đổi chủ trước rồi mới đổi quyền: trên Linux chown có thể xoá
// bit setgid/setuid, làm ngược thì mất quyền vừa đặt.
func setPermOwner(sys system.System, p string, perm fs.FileMode, owner system.Owner) error {
	if err := sys.Chown(p, owner); err != nil {
		return fmt.Errorf("đổi chủ %s: %w", p, err)
	}
	if err := sys.Chmod(p, perm); err != nil {
		return fmt.Errorf("đổi quyền %s: %w", p, err)
	}
	return nil
}

// missingDirs trả các thư mục còn thiếu trên đường tới p (gồm cả p), từ ngoài
// vào trong. Undo cần danh sách này để xoá đúng những gì bước đã tạo.
func missingDirs(sys system.System, p string) ([]string, error) {
	var out []string
	for d := p; d != "/"; d = path.Dir(d) {
		_, err := sys.Stat(d)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("kiểm thư mục %s: %w", d, err)
		}
		out = append(out, d)
	}
	slices.Reverse(out)
	return out, nil
}

// removeDirs xoá các thư mục đã tạo, từ trong ra ngoài. Remove chỉ xoá thư
// mục rỗng: thư mục có thứ khác ghi vào thì giữ lại và báo lỗi, không xoá dữ liệu.
func removeDirs(sys system.System, created []string) error {
	for _, d := range slices.Backward(created) {
		if err := sys.Remove(d); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("xoá thư mục %s đã tạo (chỉ xoá khi rỗng): %w", d, err)
		}
	}
	return nil
}
