package steps

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Bản WordPress mặc định và checksum chính thức của tarball.
//
// Tra ngày 19/11/2026 bằng curl từ máy dev:
//   - bản mới nhất: https://api.wordpress.org/core/version-check/1.7/ trả
//     "current":"7.1.5".
//   - checksum: https://wordpress.org/wordpress-7.1.5.tar.gz.sha1 (cùng số với
//     https://downloads.wordpress.org/release/wordpress-7.1.5.tar.gz.sha1). Đã
//     tải wordpress-7.1.5.tar.gz và tính `shasum -a 1`, ra đúng số này.
//
// Vì sao SHA-1 mà không phải sha256 hay sha512: wordpress.org chỉ công bố .md5
// và .sha1 cho tarball. Các đường .sha256, .sha384, .sha512 trên wordpress.org
// trả 200 nhưng nội dung là chính tarball (35382945 byte), không phải checksum;
// trên downloads.wordpress.org thì trả 404. Đừng "nâng" lên một loại hash mà
// nhà phát hành không công bố. Tải qua HTTPS nên SHA-1 ở đây là lớp kiểm thứ
// hai (file bị cắt, bị sửa trong cache), không phải lớp duy nhất.
//
// Lên bản mới: tra lại cả hai URL, đổi cả hai hằng cùng lúc.
const (
	DefaultWordPressVersion = "7.1.5"
	DefaultWordPressSHA1    = "f79cd80a8f8152edd88ef9be859ae765f2ec6c36"
)

// Mọi Step của file này phải thoả engine.Step.
var (
	_ engine.Step = (*WPArchive)(nil)
	_ engine.Step = (*WPFiles)(nil)
	_ engine.Step = (*WPConfig)(nil)
	_ engine.Step = (*WPInstall)(nil)
)

var (
	// Bản WordPress có thể chỉ có hai số (bản lớn "6.5", "7.0") hoặc ba số ("7.1.5").
	wpCoreVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)
	wpSHA1Re        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	wpAdminUserRe   = regexp.MustCompile(`^[a-z0-9_]{3,60}$`)
	// wpDBNameRe là lớp chặn cuối cho tên DB lấy từ Layout (tp_<slug>).
	wpDBNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)
)

const (
	// wpPermalink là cấu trúc đường dẫn bài viết (mục 6.2, bước 9).
	wpPermalink = "/%postname%/"
	// wpTitleMax là độ dài tối đa của tiêu đề site, tính bằng byte.
	wpTitleMax = 200
	// wpSecretMax: WP-CLI đọc câu trả lời --prompt bằng readline() hoặc
	// stream_get_line(STDIN, 1024, "\n"); dài hơn thì bị cắt mà không báo.
	wpSecretMax = 1023
)

// wpRun là một lệnh wp chạy dưới user của site. Không bao giờ dùng
// --allow-root: WordPress của site chạy dưới user đó, root không có việc gì
// phải nạp mã PHP của site (plugin lạ chạy bằng root là mất máy).
func wpRun(l site.Layout, stdin []byte, args ...string) system.Cmd {
	return system.Cmd{
		Name:   "wp",
		Args:   append(args, "--path="+l.Public),
		Stdin:  stdin,
		AsUser: l.User,
	}
}

// wpCheckLayout là lớp chặn cuối cho Layout: Layout zero (quên ParseDomain) hay
// đường dẫn lạ thì không chạy lệnh nào.
func wpCheckLayout(l site.Layout) error {
	if l.Domain.IsZero() {
		return errors.New("site chưa có domain (Layout rỗng)")
	}
	if err := checkPath("thư mục public của site", l.Public); err != nil {
		return err
	}
	if err := checkPath("wp-config.php của site", l.WPConfig); err != nil {
		return err
	}
	if l.WPConfig != l.Public+"/wp-config.php" {
		return fmt.Errorf("wp-config.php %q không nằm ngay trong %q", l.WPConfig, l.Public)
	}
	if !wpDBNameRe.MatchString(l.User) || !wpDBNameRe.MatchString(l.DB) {
		return fmt.Errorf("tên user %q hoặc tên DB %q của site có ký tự không cho phép", l.User, l.DB)
	}
	return nil
}

// wpCheckSecret: mật khẩu đi vào WP-CLI qua stdin, mỗi câu trả lời là một
// dòng. Có ký tự điều khiển (xuống dòng) thì phần sau thành câu trả lời khác;
// dài quá thì bị cắt. Chỉ nhận ký tự in được, không khoảng trắng.
func wpCheckSecret(what string, s system.Secret) error {
	v := s.Reveal()
	if v == "" {
		return fmt.Errorf("%s đang trống", what)
	}
	if len(v) > wpSecretMax {
		return fmt.Errorf("%s dài quá %d byte", what, wpSecretMax)
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x21 || v[i] > 0x7e {
			return fmt.Errorf("%s có ký tự không cho phép (chỉ nhận ký tự ASCII in được, không khoảng trắng)", what)
		}
	}
	return nil
}

// wpHide thay mọi chỗ mật khẩu lọt vào thông báo lỗi bằng "••••". WP-CLI in
// lại các tham số đã hỏi qua --prompt (kể cả mật khẩu) trừ khi có --quiet, và
// output đó đi vào lỗi qua withStdout. Lỗi đã che thì mất chuỗi bọc (errors.Is):
// giữ chuỗi bọc là giữ luôn bản chưa che bên trong.
func wpHide(err error, secrets ...system.Secret) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	hidden := msg
	for _, s := range secrets {
		if v := s.Reveal(); v != "" {
			hidden = strings.ReplaceAll(hidden, v, "••••")
		}
	}
	if hidden == msg {
		return err
	}
	return errors.New(hidden)
}

// ================================================================== WPArchive

// WPArchive tải bản WordPress Version vào /var/cache/tpan/ một lần (mục 5),
// kiểm SHA-1 bằng Go trước khi bước nào khác được giải nén nó. Chạy lại thì
// dùng file trong cache nếu SHA-1 vẫn khớp.
type WPArchive struct {
	Version string
	Hash    string // SHA-1 hex, loại hash wordpress.org công bố (xem DefaultWordPressSHA1)
}

// Path trả đường dẫn file tải về trong cache.
func (s *WPArchive) Path() string { return wpCacheDir + "/wordpress-" + s.Version + ".tar.gz" }

func (s *WPArchive) validate() error {
	if !wpCoreVersionRe.MatchString(s.Version) {
		return fmt.Errorf("bản WordPress %q không đúng dạng số.số hoặc số.số.số", s.Version)
	}
	if !wpSHA1Re.MatchString(strings.ToLower(s.Hash)) {
		return errors.New("SHA-1 của WordPress phải là 40 ký tự hex")
	}
	return nil
}

func (s *WPArchive) download() system.Cmd {
	url := "https://wordpress.org/wordpress-" + s.Version + ".tar.gz"
	return system.Cmd{Name: "curl", Args: []string{"-fsSL", "-o", s.Path(), url}}
}

func (s *WPArchive) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "wp.archive:" + s.Version,
		Title: i18n.T("Tải WordPress "+s.Version+" vào "+wpCacheDir, "Download WordPress "+s.Version+" to "+wpCacheDir),
		Why: i18n.T(
			"Mã nguồn WordPress của site được giải nén từ file này. Thiếu nó thì site không có gì để chạy. Checksum được kiểm trước khi dùng: file tải hỏng hay bị sửa thì không bao giờ được giải nén vào site.",
			"The site's WordPress code is extracted from this file. Without it the site has nothing to run. The checksum is checked first, so a broken or tampered download is never extracted into a site.",
		),
		Commands: []string{s.download().String()},
		NoUndo: i18n.T(
			"Không xoá file đã tải: nó được dùng lại cho các site sau, và nằm trong cache chứ không ở site nào.",
			"The downloaded file is kept: later sites reuse it, and it lives in the cache, not in any site.",
		),
	}
}

func wpSHA1OK(data []byte, want string) bool {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:]) == strings.ToLower(want)
}

// Check: file có, là file thường, root:root 0644 (user của site đọc được để
// giải nén), và SHA-1 khớp.
func (s *WPArchive) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	p := s.Path()
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsRegular() || fi.Mode.Perm() != 0o644 || normOwner(fi.Owner) != system.Root {
		return false, nil
	}
	data, err := sys.ReadFile(p)
	if err != nil {
		return false, fmt.Errorf("đọc %s: %w", p, err)
	}
	return wpSHA1OK(data, s.Hash), nil
}

func (s *WPArchive) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	p := s.Path()
	prev, err := capture(sys, p)
	if err != nil {
		return fmt.Errorf("tải WordPress: %w", err)
	}
	// fail trả cache về như trước bước này rồi mới báo lỗi.
	fail := func(err error) error {
		if rerr := prev.restore(sys); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}

	// File đã tải lần trước mà SHA-1 vẫn khớp thì dùng lại, chỉ sửa quyền.
	if !prev.exists || prev.link != "" || !wpSHA1OK(prev.data, s.Hash) {
		if res, err := sys.Run(ctx, s.download()); err != nil {
			return fail(withStdout(fmt.Errorf("tải WordPress %s: %w", s.Version, err), res))
		}
		data, err := sys.ReadFile(p)
		if err != nil {
			return fail(fmt.Errorf("đọc WordPress vừa tải %s: %w", p, err))
		}
		if !wpSHA1OK(data, s.Hash) {
			return fail(fmt.Errorf("WordPress %s tải về có SHA-1 không khớp bản chính thức; đã bỏ file %s", s.Version, p))
		}
	}
	// Đặt chủ trước rồi quyền: root:root 0644, để tar chạy dưới user của site
	// đọc được (thư mục cache là 0755).
	if err := sys.Chown(p, system.Root); err != nil {
		return fail(fmt.Errorf("đổi chủ %s: %w", p, err))
	}
	if err := sys.Chmod(p, 0o644); err != nil {
		return fail(fmt.Errorf("đổi quyền %s: %w", p, err))
	}
	return nil
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *WPArchive) Undo(context.Context, system.System) error { return nil }

// ==================================================================== WPFiles

// WPFiles giải nén WordPress vào l.Public (mục 6.2, bước 6), dưới user của
// site để mọi file thuộc user đó ngay từ đầu, rồi đặt quyền theo mục 5: thư
// mục 2750, file 0640.
type WPFiles struct {
	Layout  site.Layout
	Archive string

	applied bool
}

func (s *WPFiles) validate() error {
	if err := wpCheckLayout(s.Layout); err != nil {
		return err
	}
	// Chỉ nhận tarball trong cache của tpan (do WPArchive tải và kiểm): tar
	// không bao giờ được đọc một file tuỳ ý.
	if err := checkPath("file WordPress", s.Archive); err != nil {
		return err
	}
	if !strings.HasPrefix(s.Archive, wpCacheDir+"/wordpress-") || !strings.HasSuffix(s.Archive, ".tar.gz") {
		return fmt.Errorf("file WordPress %q không phải tarball trong %s", s.Archive, wpCacheDir)
	}
	return nil
}

func (s *WPFiles) versionFile() string { return s.Layout.Public + "/wp-includes/version.php" }

// wpDirMode là quyền mọi thư mục bên dưới public sau khi giải nén (mục 5).
const wpDirMode = fs.ModeSetgid | 0o750

// wpMaxDirs là trần số thư mục chmodDirs đi qua. WordPress có khoảng 200;
// trần chỉ để một cây lạ không giữ job mãi.
const wpMaxDirs = 200_000

func (s *WPFiles) cmds() (extract, files, clear system.Cmd) {
	l := s.Layout
	extract = system.Cmd{
		Name:   "tar",
		Args:   []string{"-xzf", s.Archive, "-C", l.Public, "--strip-components=1"},
		AsUser: l.User,
	}
	// Thư mục: đặt quyền bằng chmodDirs (root, qua cửa sitefs), không bằng lệnh.
	// File: chạy dưới user của site, chủ của chúng. Không cần root, và một
	// symlink chen vào giữa lúc find và chmod cũng chỉ đổi được file của user đó.
	files = system.Cmd{
		Name:   "find",
		Args:   []string{l.Public, "-mindepth", "1", "-type", "f", "-exec", "chmod", "0640", "{}", "+"},
		AsUser: l.User,
	}
	// -delete không đi theo symlink, và chỉ xoá bên trong l.Public.
	clear = system.Cmd{
		Name:   "find",
		Args:   []string{l.Public, "-mindepth", "1", "-delete"},
		AsUser: l.User,
	}
	return
}

func (s *WPFiles) Info() engine.StepInfo {
	extract, files, _ := s.cmds()
	return engine.StepInfo{
		ID:    "wp.files:" + s.Layout.Domain.String(),
		Title: i18n.T("Giải nén WordPress vào "+s.Layout.Public, "Extract WordPress into "+s.Layout.Public),
		Why: i18n.T(
			"Đây là mã nguồn của site. Thiếu nó thì web server không có gì để chạy. Quyền thư mục 2750 và file 0640 cho web server đọc file tĩnh qua nhóm www-data, còn site khác thì không đọc được.",
			"This is the site's code. Without it the web server has nothing to run. Directories 2750 and files 0640 let the web server read static files through the www-data group, while other sites cannot read them.",
		),
		Commands: []string{
			extract.String(),
			"(tpan) chmod 2750 mọi thư mục bên dưới " + s.Layout.Public + ", không đi theo symlink",
			files.String(),
		},
	}
}

func (s *WPFiles) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	p := s.versionFile()
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	return fi.Mode.IsRegular(), nil
}

func (s *WPFiles) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	l := s.Layout
	// Chỉ giải nén vào thư mục rỗng: Undo xoá mọi thứ bên trong l.Public, nên
	// nếu ở đó đã có file không do bước này tạo thì Undo sẽ xoá mất dữ liệu.
	names, err := sys.ReadDir(l.Public)
	if err != nil {
		return fmt.Errorf("đọc thư mục %s: %w", l.Public, err)
	}
	if len(names) > 0 {
		return fmt.Errorf("giải nén WordPress: %s đã có %d mục mà không phải bản WordPress đầy đủ; tpan không ghi đè (xem bằng: ls -la %s)", l.Public, len(names), l.Public)
	}

	extract, files, clear := s.cmds()
	// Engine không Undo bước có Apply lỗi: hỏng ở đâu thì tự dọn phần đã giải nén.
	fail := func(err error) error {
		if _, rerr := sys.Run(ctx, clear); rerr != nil {
			return errors.Join(err, fmt.Errorf("dọn %s sau lỗi: %w", l.Public, rerr))
		}
		return err
	}
	if res, err := sys.Run(ctx, extract); err != nil {
		return fail(withStdout(fmt.Errorf("giải nén WordPress vào %s: %w", l.Public, err), res))
	}
	if err := chmodDirs(ctx, sys, l.Public, wpDirMode); err != nil {
		return fail(fmt.Errorf("đặt quyền 2750 cho thư mục trong %s: %w", l.Public, err))
	}
	if res, err := sys.Run(ctx, files); err != nil {
		return fail(withStdout(fmt.Errorf("đặt quyền 0640 cho file trong %s: %w", l.Public, err), res))
	}
	s.applied = true
	return nil
}

// chmodDirs đặt quyền perm cho mọi thư mục bên dưới root (không gồm root), thay
// cho `find <root> -mindepth 1 -type d -exec chmod 2750 {} +` chạy bằng root.
//
// Vì sao không dùng find nữa (M28): find liệt kê theo đường dẫn rồi chmod theo
// đường dẫn, mà root là thư mục của site. User của site tráo một thư mục con
// thành symlink đúng giữa hai việc đó là root đổi quyền một thư mục ngoài site
// (đo trên máy ảo: thua đua sau 5 vòng, docs/bang-chung/M28-nghien-cuu/n3).
// Ở đây mọi lời gọi đi qua sys, mà với đường dẫn trong thư mục site thì
// System đi qua cửa sitefs: không theo symlink, chmod trên fd đã mở.
//
// Phải chạy bằng root (không dưới user của site): Linux xoá bit setgid khi
// người chmod không thuộc nhóm của thư mục (www-data), mà user của site cố ý
// không thuộc nhóm đó.
//
// Giống lệnh cũ: không đổi quyền chính root (bước Dir đã lo), bỏ qua symlink
// và file, không dừng ở ranh phân vùng. Mục biến mất hay thành symlink giữa
// lúc liệt kê và lúc chmod thì bỏ qua: chỉ hại chính site. Lỗi khác làm hỏng
// bước.
func chmodDirs(ctx context.Context, sys system.System, root string, perm fs.FileMode) error {
	n := 0
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > sitefs.MaxDepth {
			return fmt.Errorf("%s sâu quá %d tầng", dir, sitefs.MaxDepth)
		}
		names, err := sys.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("liệt kê %s: %w", dir, err)
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			p := dir + "/" + name
			fi, err := sys.Stat(p)
			if chmodVanished(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("kiểm %s: %w", p, err)
			}
			if !fi.Mode.IsDir() {
				continue // file, symlink, thứ khác: không đụng
			}
			if n++; n > wpMaxDirs {
				return fmt.Errorf("%s có hơn %d thư mục", root, wpMaxDirs)
			}
			if err := sys.Chmod(p, perm); chmodVanished(err) {
				continue
			} else if err != nil {
				return fmt.Errorf("đặt quyền %s: %w", p, err)
			}
			if err := walk(p, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root, 1)
}

// chmodVanished: mục biến mất hay bị tráo thành symlink giữa lúc liệt kê và
// lúc dùng (chỉ user của site làm được việc đó, trong chính thư mục của họ).
func chmodVanished(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, sitefs.ErrSymlink)
}

// Undo xoá mọi thứ bên trong l.Public, giữ chính l.Public (Dir của nó tự Undo).
// Apply đã đảm bảo l.Public rỗng lúc bắt đầu, nên không xoá gì không do site tạo.
func (s *WPFiles) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("giải nén WordPress vào %s: %w", s.Layout.Public, errNotApplied)
	}
	_, _, clear := s.cmds()
	if res, err := sys.Run(ctx, clear); err != nil {
		return withStdout(fmt.Errorf("xoá mã nguồn trong %s: %w", s.Layout.Public, err), res)
	}
	return nil
}

// =================================================================== WPConfig

// WPConfig tạo wp-config.php (mục 6.2, bước 7) bằng `wp config create`, thêm
// DISALLOW_FILE_EDIT rồi chmod 600. Mọi lệnh chạy dưới user của site.
//
// Mật khẩu DB đi vào WP-CLI qua stdin với --prompt=dbpass (cách WP-CLI ghi
// trong ví dụ của chính lệnh này), không bao giờ nằm trên dòng lệnh.
type WPConfig struct {
	Layout     site.Layout
	DBPassword system.Secret

	created  bool        // bước này tạo file (Undo xoá)
	prevPerm fs.FileMode // file có sẵn, bước này chỉ sửa quyền (Undo trả quyền cũ)
	applied  bool
}

func (s *WPConfig) validate() error {
	if err := wpCheckLayout(s.Layout); err != nil {
		return err
	}
	return wpCheckSecret("mật khẩu DB", s.DBPassword)
}

func (s *WPConfig) cmds() (create, set system.Cmd) {
	l := s.Layout
	// --quiet: sau khi hỏi qua --prompt, WP-CLI 2.12.0 in lại cả lệnh kèm giá
	// trị vừa nhận (gồm mật khẩu) bằng WP_CLI::log; --quiet tắt dòng đó.
	// --dbcharset=utf8mb4: mặc định của lệnh là utf8, DB của site là utf8mb4.
	create = wpRun(l, []byte(s.DBPassword.Reveal()+"\n"),
		"config", "create", "--dbname="+l.DB, "--dbuser="+l.DB, "--dbhost=localhost",
		"--dbcharset=utf8mb4", "--prompt=dbpass", "--quiet")
	// Tắt trình sửa theme và plugin trong trang quản trị: lấy được tài khoản
	// quản trị thì cũng không sửa được mã PHP từ trình duyệt.
	set = wpRun(l, nil, "config", "set", "DISALLOW_FILE_EDIT", "true", "--raw", "--type=constant", "--quiet")
	return
}

// wpChmod là chmod chạy dưới user của site: file thuộc user đó nên không cần
// root, và root chmod trong thư mục do user kiểm soát thì có thể bị lừa đi
// theo symlink sang file hệ thống.
func wpChmod(l site.Layout, perm fs.FileMode, p string) system.Cmd {
	return system.Cmd{Name: "chmod", Args: []string{fmt.Sprintf("%04o", perm), "--", p}, AsUser: l.User}
}

func (s *WPConfig) Info() engine.StepInfo {
	create, set := s.cmds()
	chmod := wpChmod(s.Layout, 0o600, s.Layout.WPConfig)
	// Không đưa wp-config.php vào Files: lúc xem trước engine đọc nội dung cũ
	// của mọi file trong Files, tức là đọc luôn mật khẩu DB ra giao diện.
	return engine.StepInfo{
		ID:    "wp.config:" + s.Layout.Domain.String(),
		Title: i18n.T("Tạo "+s.Layout.WPConfig, "Create "+s.Layout.WPConfig),
		Why: i18n.T(
			"WordPress đọc tên DB, user DB và mật khẩu từ file này. Thiếu nó thì site chỉ hiện trang cài đặt, ai vào trước người đó chiếm site. Quyền 600 để chỉ user của site đọc được mật khẩu DB, web server và site khác thì không.",
			"WordPress reads the database name, user and password from this file. Without it the site shows the setup page and whoever arrives first takes the site. Mode 600 lets only the site's user read the database password, not the web server or other sites.",
		),
		Commands: cmdStrings(&create, &set, &chmod),
	}
}

func (s *WPConfig) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	p := s.Layout.WPConfig
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	return fi.Mode.IsRegular() && fi.Mode.Perm() == 0o600 && fi.Owner.User == s.Layout.User, nil
}

func (s *WPConfig) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	l := s.Layout
	p := l.WPConfig
	fi, err := sys.Stat(p)
	switch {
	case err == nil:
		// Có sẵn: không tạo lại. `config create --force` sẽ ghi một mật khẩu
		// mà user DB không biết. Chỉ sửa quyền nếu đúng là file của user site.
		if !fi.Mode.IsRegular() || fi.Owner.User != l.User {
			return fmt.Errorf("%s đã có nhưng không phải file thường của %s; tpan không ghi đè (xem bằng: ls -la %s)", p, l.User, p)
		}
		if res, err := sys.Run(ctx, wpChmod(l, 0o600, p)); err != nil {
			return withStdout(fmt.Errorf("đặt quyền 600 cho %s: %w", p, err), res)
		}
		s.created, s.prevPerm = false, fi.Mode.Perm()
	case errors.Is(err, fs.ErrNotExist):
		if err := s.create(ctx, sys); err != nil {
			return err
		}
		s.created = true
	default:
		return fmt.Errorf("kiểm %s: %w", p, err)
	}
	s.applied = true
	return nil
}

// create chạy config create, config set, chmod 600. Hỏng ở bước nào thì xoá
// file vừa tạo, vì engine không Undo bước có Apply lỗi.
func (s *WPConfig) create(ctx context.Context, sys system.System) error {
	l := s.Layout
	createCmd, setCmd := s.cmds()
	for _, c := range []struct {
		cmd  system.Cmd
		what string
	}{
		{createCmd, "tạo " + l.WPConfig},
		{setCmd, "thêm DISALLOW_FILE_EDIT vào " + l.WPConfig},
		{wpChmod(l, 0o600, l.WPConfig), "đặt quyền 600 cho " + l.WPConfig},
	} {
		if res, err := sys.Run(ctx, c.cmd); err != nil {
			err = wpHide(withStdout(fmt.Errorf("%s: %w", c.what, err), res), s.DBPassword)
			if rerr := sys.Remove(l.WPConfig); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				return errors.Join(err, fmt.Errorf("xoá %s sau lỗi: %w", l.WPConfig, rerr))
			}
			return err
		}
	}
	return nil
}

// Undo xoá file nếu bước này tạo nó (Remove không đi theo symlink); nếu file có
// sẵn thì chỉ trả lại quyền cũ, không bao giờ xoá mật khẩu DB đang dùng.
func (s *WPConfig) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("wp-config.php %s: %w", s.Layout.WPConfig, errNotApplied)
	}
	p := s.Layout.WPConfig
	if s.created {
		if err := sys.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("xoá %s: %w", p, err)
		}
		return nil
	}
	if res, err := sys.Run(ctx, wpChmod(s.Layout, s.prevPerm, p)); err != nil {
		return withStdout(fmt.Errorf("trả lại quyền cũ của %s: %w", p, err), res)
	}
	return nil
}

// ================================================================== WPInstall

// WPInstall chạy `wp core install` (mục 6.2, bước 9) rồi đặt permalink
// /%postname%/. Mật khẩu quản trị đi qua stdin với --prompt=admin_password.
type WPInstall struct {
	Layout        site.Layout
	URL           string // http://<domain>
	Title         string // core đã kiểm: không ký tự điều khiển, ≤ 200 byte
	AdminUser     string
	AdminEmail    validate.Email
	AdminPassword system.Secret
}

func (s *WPInstall) validate() error {
	if err := wpCheckLayout(s.Layout); err != nil {
		return err
	}
	d := s.Layout.Domain.String()
	if s.URL != "http://"+d && s.URL != "https://"+d {
		return fmt.Errorf("địa chỉ site %q phải là http://%s hoặc https://%s", s.URL, d, d)
	}
	// Core đã kiểm tiêu đề; kiểm lại ở đây vì nó đi thẳng vào argv và vào DB.
	switch {
	case s.Title == "":
		return errors.New("tiêu đề site đang trống")
	case len(s.Title) > wpTitleMax:
		return fmt.Errorf("tiêu đề site dài quá %d byte", wpTitleMax)
	case !utf8.ValidString(s.Title):
		return errors.New("tiêu đề site không phải UTF-8 hợp lệ")
	case strings.ContainsFunc(s.Title, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) }):
		return errors.New("tiêu đề site có ký tự điều khiển")
	}
	if !wpAdminUserRe.MatchString(s.AdminUser) {
		return fmt.Errorf("tên quản trị %q chỉ được có a-z, 0-9, _ và dài 3 tới 60 ký tự", s.AdminUser)
	}
	if s.AdminEmail.IsZero() {
		return errors.New("email quản trị đang trống")
	}
	return wpCheckSecret("mật khẩu quản trị", s.AdminPassword)
}

// Lệnh chỉ đọc dùng trong Check. --skip-plugins --skip-themes: plugin hỏng
// không làm Check lỗi, và mã của plugin không chạy chỉ để hỏi trạng thái.
func (s *WPInstall) isInstalledCmd() system.Cmd {
	return wpRun(s.Layout, nil, "core", "is-installed", "--skip-plugins", "--skip-themes")
}

func (s *WPInstall) permalinkCmd() system.Cmd {
	return wpRun(s.Layout, nil, "option", "get", "permalink_structure", "--skip-plugins", "--skip-themes")
}

func (s *WPInstall) cmds() (install, rewrite system.Cmd) {
	// --quiet: như WPConfig, WP-CLI in lại tham số đã hỏi (mật khẩu quản trị)
	// nếu không có cờ này. --skip-email: máy chủ mới thường chưa gửi được mail,
	// và mật khẩu quản trị không bao giờ đi qua email.
	install = wpRun(s.Layout, []byte(s.AdminPassword.Reveal()+"\n"),
		"core", "install", "--url="+s.URL, "--title="+s.Title, "--admin_user="+s.AdminUser,
		"--admin_email="+s.AdminEmail.String(), "--prompt=admin_password", "--skip-email", "--quiet")
	rewrite = wpRun(s.Layout, nil, "rewrite", "structure", wpPermalink, "--quiet")
	return
}

func (s *WPInstall) Info() engine.StepInfo {
	install, rewrite := s.cmds()
	return engine.StepInfo{
		ID:    "wp.install:" + s.Layout.Domain.String(),
		Title: i18n.T("Cài WordPress cho "+s.Layout.Domain.String(), "Install WordPress for "+s.Layout.Domain.String()),
		Why: i18n.T(
			"Bước này tạo bảng của WordPress và tài khoản quản trị. Thiếu nó thì ai mở site trước sẽ tự đặt tài khoản quản trị và chiếm site. Permalink /%postname%/ cho đường dẫn bài viết dễ đọc thay vì ?p=123.",
			"This step creates WordPress's tables and the admin account. Without it whoever opens the site first sets the admin account and takes the site. The /%postname%/ permalink gives readable post URLs instead of ?p=123.",
		),
		Commands: cmdStrings(&install, &rewrite),
		NoUndo: i18n.T(
			"Bảng của WordPress nằm trong DB của site; hoàn tác bước tạo DB thì chúng mất theo.",
			"WordPress's tables live in the site's database; undoing the database step removes them.",
		),
	}
}

func (s *WPInstall) installed(ctx context.Context, sys system.System) (bool, error) {
	res, err := sys.Run(ctx, s.isInstalledCmd())
	if err == nil {
		return true, nil
	}
	// Mã 1 là câu trả lời "chưa cài" (WP_CLI::halt(1)); WP-CLI cũng trả 1 khi
	// chưa có wp-config.php. Mã khác (255: lỗi PHP) là lỗi thật.
	if ee, ok := isExit(err); ok && ee.Code == 1 {
		return false, nil
	}
	return false, withStdout(fmt.Errorf("kiểm WordPress đã cài chưa: %w", err), res)
}

func (s *WPInstall) permalink(ctx context.Context, sys system.System) (string, error) {
	res, err := sys.Run(ctx, s.permalinkCmd())
	if err != nil {
		return "", withStdout(fmt.Errorf("đọc permalink: %w", err), res)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// Check: WordPress đã cài VÀ permalink đúng. Gộp cả hai để lần chạy lại sửa
// được trường hợp cài xong mà đặt permalink hỏng.
func (s *WPInstall) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	ok, err := s.installed(ctx, sys)
	if err != nil || !ok {
		return false, err
	}
	p, err := s.permalink(ctx, sys)
	if err != nil {
		return false, err
	}
	return p == wpPermalink, nil
}

func (s *WPInstall) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	install, rewrite := s.cmds()
	ok, err := s.installed(ctx, sys)
	if err != nil {
		return err
	}
	if !ok {
		if res, err := sys.Run(ctx, install); err != nil {
			return wpHide(withStdout(fmt.Errorf("cài WordPress cho %s: %w", s.Layout.Domain, err), res), s.AdminPassword)
		}
	}
	p, err := s.permalink(ctx, sys)
	if err != nil {
		return err
	}
	if p != wpPermalink {
		if res, err := sys.Run(ctx, rewrite); err != nil {
			return withStdout(fmt.Errorf("đặt permalink %s: %w", wpPermalink, err), res)
		}
	}
	return nil
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *WPInstall) Undo(context.Context, system.System) error { return nil }
