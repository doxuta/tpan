package steps

// Đổi mật khẩu DB (mục 6.9) và hằng của wp-config.php (mục 6.10). Luật chung
// ở m9_types.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*DBPassword)(nil)
	_ engine.Step = (*WPConstant)(nil)
)

// DBPassword đổi mật khẩu user DB của site (mục 6.9) mà mật khẩu không bao
// giờ nằm trên dòng lệnh hay trong log. Apply:
//  1. Đọc mật khẩu cũ từ wp-config.php bằng sys.ReadFile (không qua Run, vì
//     mọi output của Run vào log job): Stat (lstat) phải là file thường của
//     user site; đúng một dòng define mỗi DB_NAME, DB_USER, DB_PASSWORD;
//     DB_NAME và DB_USER đúng l.DB (chặn tráo file giữa Stat và ReadFile,
//     xem readOld); DB_PASSWORD đúng dạng mật khẩu tpan sinh
//     ([A-Za-z0-9]{32}). Sai thì từ chối (mật khẩu không do tpan sinh: đổi
//     tay). Giữ để hoàn tác.
//  2. ALTER USER '<db>'@'localhost' IDENTIFIED BY '<mới>' qua Stdin của mariadb.
//  3. `wp config set DB_PASSWORD --prompt --type=constant --quiet` dưới user
//     của site, mật khẩu qua Stdin (một dòng). Không phải `--prompt=value`:
//     đã kiểm trên máy ảo (WP-CLI 2.12.0), `--prompt=<danh sách>` chỉ hỏi
//     tham số dạng --tên=giá trị, còn <value> là tham số theo vị trí nên lệnh
//     báo usage và thoát 1. `--prompt` trơn hỏi mọi tham số còn thiếu theo thứ
//     tự synopsis: <value> trước, rồi [--add]... gặp hết stdin thì WP-CLI dừng
//     hỏi và chạy với những gì đã có.
//     Bước 3 lỗi thì đặt lại mật khẩu cũ ở bước 2 rồi mới trả lỗi.
//
// Check: chỉ true sau Apply của chính bước (đổi mật khẩu là một hành động,
// không phải trạng thái đọc lại được mà không đọc mật khẩu). Undo: bước 2 và
// 3 với mật khẩu cũ.
//
// Giữa bước 2 và 3 site báo lỗi kết nối DB. Opcache của PHP-FPM kiểm lại
// file mỗi 2 giây (opcache.revalidate_freq), nên request rơi vào khoảng đó có
// thể giữ wp-config.php cũ thêm tới 2 giây sau bước 3 (đo trên máy ảo: 500
// rồi về 200). Phép kiểm "site còn 200" sau bước này phải chờ hoặc thử lại.
type DBPassword struct {
	Layout   site.Layout
	Password system.Secret

	// old là mật khẩu cũ đọc từ wp-config.php, để hoàn tác. Con trỏ: fmt không
	// gọi String() của trường không xuất, nên in bước bằng %v sẽ lộ một
	// Secret nằm thẳng trong trường riêng; con trỏ lồng thì fmt chỉ in địa chỉ.
	old     *system.Secret
	applied bool
}

var (
	// dbDefineRe bắt các dòng define DB_* có giá trị chuỗi của wp-config.php,
	// dạng `wp config create` ghi: define( 'DB_PASSWORD', '...' );
	dbDefineRe = regexp.MustCompile(`define\(\s*['"](DB_[A-Z]+)['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
	// dbTpanPasswordRe: mật khẩu DB tpan sinh khi tạo site. Mật khẩu khác
	// dạng này là do người khác đặt; tpan không biết nó còn dùng ở đâu.
	dbTpanPasswordRe = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)
)

// dbAlterUserSQL đổi mật khẩu của user DB. Mật khẩu chỉ gồm chữ và số
// (dbPasswordRe, dbTpanPasswordRe) nên không cần thoát ký tự nào.
// https://mariadb.com/kb/en/alter-user/
func dbAlterUserSQL(name, password string) string {
	return "ALTER USER '" + name + "'@'localhost' IDENTIFIED BY '" + password + "';\n"
}

func (s *DBPassword) validate() error {
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := wpCheckLayout(s.Layout); err != nil {
		return err
	}
	// Không in mật khẩu ra lỗi, kể cả mật khẩu sai.
	if !dbPasswordRe.MatchString(s.Password.Reveal()) {
		return fmt.Errorf("mật khẩu DB mới của site %s phải gồm ít nhất 24 ký tự, chỉ chữ cái và chữ số", s.Layout.Domain)
	}
	return wpCheckSecret("mật khẩu DB mới", s.Password)
}

// setCmd ghi mật khẩu vào wp-config.php. Mật khẩu chỉ nằm trong Stdin.
// --quiet: không có cờ này WP-CLI in lại cả lệnh kèm giá trị vừa hỏi (đã thấy
// trên máy ảo). --type=constant: chỉ sửa hằng, không đụng biến PHP trùng tên.
func (s *DBPassword) setCmd(password string) system.Cmd {
	var stdin []byte
	if password != "" {
		stdin = []byte(password + "\n")
	}
	return wpRun(s.Layout, stdin, "config", "set", "DB_PASSWORD", "--prompt", "--type=constant", "--quiet")
}

func (s *DBPassword) Info() engine.StepInfo {
	l := s.Layout
	set := s.setCmd("")
	return engine.StepInfo{
		ID: "mariadb.db-password:" + l.Domain.String(),
		Title: i18n.T(
			"Đổi mật khẩu user DB "+l.DB+" của site "+l.Domain.String(),
			"Change the password of database user "+l.DB+" for "+l.Domain.String(),
		),
		Why: i18n.T(
			"Ai có mật khẩu DB thì đọc và sửa được toàn bộ bài viết, tài khoản của site. Đổi khi nghi mật khẩu bị lộ (wp-config.php bị đọc trộm, bản sao lưu rơi ra ngoài); không đổi thì người đó vẫn vào được. Mật khẩu mới ghi vào MariaDB rồi vào wp-config.php, chỉ qua stdin, không bao giờ nằm trên dòng lệnh hay trong log. Giữa hai lệnh site báo lỗi kết nối DB vài giây.",
			"Whoever has the database password can read and change every post and account of the site. Change it when you suspect it leaked (wp-config.php was read, a backup got out); otherwise that person still gets in. The new password goes into MariaDB and then wp-config.php through stdin only, never on a command line or in a log. Between the two commands the site shows a database connection error for a few seconds.",
		),
		// Không đưa wp-config.php vào Files: xem trước sẽ đọc mật khẩu ra giao diện.
		Commands: []string{
			dbShow(dbAlterUserSQL(l.DB, dbMask)),
			set.String() + "  (stdin: " + dbMask + ")",
		},
		Danger: true,
	}
}

// readOld đọc mật khẩu DB đang dùng từ wp-config.php. Không bao giờ in giá trị
// đọc được ra lỗi.
func (s *DBPassword) readOld(sys system.System) (system.Secret, error) {
	l := s.Layout
	p := l.WPConfig
	// Stat không đi theo symlink: thư mục site do user của site kiểm soát, một
	// symlink ở đây có thể trỏ tới wp-config.php của site khác (root đọc được).
	fi, err := sys.Stat(p)
	if err != nil {
		return system.Secret{}, fmt.Errorf("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsRegular() || fi.Owner.User != l.User {
		return system.Secret{}, fmt.Errorf("%s không phải file thường của %s; tpan không đọc (xem bằng: ls -la %s)", p, l.User, p)
	}
	data, err := sys.ReadFile(p)
	if err != nil {
		return system.Secret{}, fmt.Errorf("đọc %s: %w", p, err)
	}
	defs := map[string][]string{}
	for _, m := range dbDefineRe.FindAllSubmatch(data, -1) {
		defs[string(m[1])] = append(defs[string(m[1])], string(m[2]))
	}
	for _, name := range []string{"DB_NAME", "DB_USER", "DB_PASSWORD"} {
		if n := len(defs[name]); n != 1 {
			return system.Secret{}, fmt.Errorf("%s có %d dòng define %s, cần đúng một; tpan không đoán dòng nào đang dùng, đổi mật khẩu bằng tay", p, n, name)
		}
	}
	// Stat rồi ReadFile không nguyên tử: user của site có thể tráo file thường
	// với symlink sang wp-config.php của site khác đúng giữa hai lần gọi. Mật
	// khẩu đọc được chỉ được tin khi chính những byte đó nói đây là DB của
	// site này; file site khác mang tên DB và user DB khác nên bị từ chối.
	if defs["DB_NAME"][0] != l.DB || defs["DB_USER"][0] != l.DB {
		return system.Secret{}, fmt.Errorf("%s không ghi DB_NAME và DB_USER là %s; tpan không đổi mật khẩu theo file này", p, l.DB)
	}
	old := defs["DB_PASSWORD"][0]
	if !dbTpanPasswordRe.MatchString(old) {
		return system.Secret{}, fmt.Errorf("mật khẩu DB trong %s không phải loại tpan sinh (32 chữ cái và chữ số), có thể do người khác đặt và còn dùng ở chỗ khác; tpan không đổi, đổi bằng tay", p)
	}
	return system.NewSecret(old), nil
}

// alter đặt mật khẩu của user DB; lỗi đã lọc mật khẩu.
func (s *DBPassword) alter(ctx context.Context, sys system.System, pw system.Secret, what string) error {
	v := pw.Reveal()
	return dbRun(ctx, sys, dbAlterUserSQL(s.Layout.DB, v), v, what)
}

// write ghi mật khẩu vào wp-config.php.
func (s *DBPassword) write(ctx context.Context, sys system.System, pw system.Secret, what string) error {
	res, err := sys.Run(ctx, s.setCmd(pw.Reveal()))
	if err != nil {
		return withStdout(fmt.Errorf("%s: %w", what, err), res)
	}
	return nil
}

// Check chỉ đọc cờ trong bộ nhớ: không có cách đọc lại "mật khẩu đã đổi"
// mà không đọc chính mật khẩu.
func (s *DBPassword) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *DBPassword) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	old, err := s.readOld(sys)
	if err != nil {
		return err
	}
	l := s.Layout
	if err := s.alter(ctx, sys, s.Password, "đổi mật khẩu user DB "+l.DB); err != nil {
		return wpHide(err, s.Password, old)
	}
	if err := s.write(ctx, sys, s.Password, "ghi mật khẩu DB mới vào "+l.WPConfig); err != nil {
		// Engine không Undo bước có Apply lỗi: tự đặt lại mật khẩu cũ để khớp
		// wp-config.php (vẫn là mật khẩu cũ), site chạy lại như trước.
		if rerr := s.alter(ctx, sys, old, "đặt lại mật khẩu cũ cho user DB "+l.DB); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return wpHide(err, s.Password, old)
	}
	s.old, s.applied = &old, true
	return nil
}

// Undo đặt lại mật khẩu cũ ở MariaDB rồi ở wp-config.php. Ghi file lỗi thì
// đặt lại mật khẩu mới ở MariaDB, để DB và file vẫn khớp nhau (site chạy).
func (s *DBPassword) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("đổi mật khẩu DB của %s: %w", s.Layout.Domain, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	l := s.Layout
	if err := s.alter(ctx, sys, *s.old, "đặt lại mật khẩu cũ cho user DB "+l.DB); err != nil {
		return wpHide(err, s.Password, *s.old)
	}
	if err := s.write(ctx, sys, *s.old, "ghi lại mật khẩu DB cũ vào "+l.WPConfig); err != nil {
		if rerr := s.alter(ctx, sys, s.Password, "đặt lại mật khẩu mới cho user DB "+l.DB+" cho khớp wp-config.php"); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return wpHide(err, s.Password, *s.old)
	}
	s.applied = false
	return nil
}

// WPConstant đặt một hằng trong wp-config.php bằng `wp config set <Name>
// <Value> --raw --type=constant --quiet` dưới user của site (giá trị không bí
// mật). Check: `wp config get <Name> --type=constant --format=json` in đúng
// Value (không --format thì WP-CLI in true là "1", false là dòng trống; JSON
// in true, false, số đúng như Value). Undo: khôi phục giá trị cũ (đọc trước
// khi Apply) hoặc `wp config delete` nếu trước đó chưa có. Dùng cho
// DISABLE_WP_CRON = true.
type WPConstant struct {
	Layout site.Layout
	Name   string // chỉ [A-Z_]
	Value  string // giá trị raw: true, false, hoặc số nguyên

	had     bool   // hằng đã có trước Apply
	prev    string // giá trị cũ, dạng JSON như `wp config get --format=json` in
	applied bool
}

var (
	wpConstNameRe  = regexp.MustCompile(`^[A-Z_]{1,64}$`)
	wpConstValueRe = regexp.MustCompile(`^(true|false|-?[0-9]{1,18})$`)
	// wpConstRawRe: giá trị cũ ghi lại được bằng --raw (JSON của bool, null, số).
	wpConstRawRe = regexp.MustCompile(`^(true|false|null|-?[0-9]{1,18}(\.[0-9]{1,18})?)$`)
)

// wpConstNotDefined là đoạn stderr WP-CLI 2.12.0 in khi hằng chưa có (đã thấy
// trên máy ảo). Thiếu wp-config.php cũng thoát 1, nên phải phân biệt bằng chữ.
const wpConstNotDefined = "is not defined"

func (s *WPConstant) validate() error {
	if err := wpCheckLayout(s.Layout); err != nil {
		return err
	}
	if !wpConstNameRe.MatchString(s.Name) {
		return fmt.Errorf("tên hằng %q chỉ được có A-Z và _", s.Name)
	}
	if !wpConstValueRe.MatchString(s.Value) {
		return fmt.Errorf("giá trị %q của hằng %s phải là true, false hoặc số nguyên", s.Value, s.Name)
	}
	return nil
}

func (s *WPConstant) getCmd() system.Cmd {
	return wpRun(s.Layout, nil, "config", "get", s.Name, "--type=constant", "--format=json")
}

func (s *WPConstant) setCmd(value string, raw bool) system.Cmd {
	args := []string{"config", "set", s.Name, value}
	if raw {
		args = append(args, "--raw")
	}
	return wpRun(s.Layout, nil, append(args, "--type=constant", "--quiet")...)
}

func (s *WPConstant) deleteCmd() system.Cmd {
	return wpRun(s.Layout, nil, "config", "delete", s.Name, "--type=constant", "--quiet")
}

// restoreCmd là lệnh ghi lại giá trị cũ prev (JSON). bool, null, số ghi bằng
// --raw; chuỗi ghi như chuỗi. Giá trị khác (mảng, chuỗi có ký tự điều khiển
// hay bắt đầu bằng "-", bị WP-CLI đọc thành tuỳ chọn) thì không ghi lại đúng
// được: Apply từ chối từ đầu.
func (s *WPConstant) restoreCmd(prev string) (system.Cmd, error) {
	if wpConstRawRe.MatchString(prev) {
		return s.setCmd(prev, true), nil
	}
	var str string
	if err := json.Unmarshal([]byte(prev), &str); err == nil &&
		!strings.HasPrefix(str, "-") && utf8.ValidString(str) &&
		!strings.ContainsFunc(str, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s.setCmd(str, false), nil
	}
	return system.Cmd{}, fmt.Errorf("hằng %s trong %s đang có giá trị mà tpan không ghi lại được nếu phải hoàn tác; sửa tay (xem bằng: wp config get %s --type=constant)", s.Name, s.Layout.WPConfig, s.Name)
}

func (s *WPConstant) Info() engine.StepInfo {
	l := s.Layout
	set := s.setCmd(s.Value, true)
	why := i18n.T(
		"WordPress đọc hằng này trong wp-config.php mỗi lần chạy. Thiếu nó thì WordPress dùng giá trị mặc định thay vì giá trị tpan cần.",
		"WordPress reads this constant from wp-config.php on every run. Without it WordPress uses its default instead of the value tpan needs.",
	)
	if s.Name == "DISABLE_WP_CRON" {
		why = i18n.T(
			"Tắt cron mặc định của WordPress, thứ chạy việc định kỳ (đăng bài hẹn giờ, kiểm cập nhật) mỗi khi có khách vào trang. tpan đã đặt cron thật chạy 5 phút một lần. Bỏ bước này thì lượt xem trang vẫn phải gánh thêm việc định kỳ, và việc chạy từ hai nơi.",
			"Turns off WordPress's built-in cron, which runs scheduled work (scheduled posts, update checks) whenever someone visits a page. tpan has set up a real cron every 5 minutes. Without this step page views still carry the scheduled work, and it runs from two places.",
		)
	}
	return engine.StepInfo{
		ID: "wp.constant:" + l.Domain.String() + ":" + s.Name,
		Title: i18n.T(
			"Đặt "+s.Name+" = "+s.Value+" trong "+l.WPConfig,
			"Set "+s.Name+" = "+s.Value+" in "+l.WPConfig,
		),
		Why:      why,
		Commands: []string{set.String()},
	}
}

// get đọc giá trị hiện tại (JSON). had false: hằng chưa có.
func (s *WPConstant) get(ctx context.Context, sys system.System) (value string, had bool, err error) {
	res, err := sys.Run(ctx, s.getCmd())
	if err == nil {
		return strings.TrimSpace(res.Stdout), true, nil
	}
	if ee, ok := isExit(err); ok && ee.Code == 1 && strings.Contains(ee.Stderr, wpConstNotDefined) {
		return "", false, nil
	}
	return "", false, withStdout(fmt.Errorf("đọc hằng %s trong %s: %w", s.Name, s.Layout.WPConfig, err), res)
}

// Check: chưa có wp-config.php thì false (xem trước Plan tạo site chạy khi
// file chưa được tạo), hằng có và đúng Value thì true.
func (s *WPConstant) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	p := s.Layout.WPConfig
	if _, err := sys.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	v, had, err := s.get(ctx, sys)
	return had && v == s.Value, err
}

func (s *WPConstant) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	prev, had, err := s.get(ctx, sys)
	if err != nil {
		return err
	}
	if had {
		if _, err := s.restoreCmd(prev); err != nil {
			return err
		}
	}
	if res, err := sys.Run(ctx, s.setCmd(s.Value, true)); err != nil {
		return withStdout(fmt.Errorf("đặt %s = %s trong %s: %w", s.Name, s.Value, s.Layout.WPConfig, err), res)
	}
	s.had, s.prev, s.applied = had, prev, true
	return nil
}

func (s *WPConstant) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("đặt %s trong %s: %w", s.Name, s.Layout.WPConfig, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	cmd, what := s.deleteCmd(), "xoá hằng "+s.Name+" khỏi "+s.Layout.WPConfig
	if s.had {
		c, err := s.restoreCmd(s.prev)
		if err != nil {
			return err
		}
		cmd, what = c, "ghi lại giá trị cũ của "+s.Name+" vào "+s.Layout.WPConfig
	}
	if res, err := sys.Run(ctx, cmd); err != nil {
		return withStdout(fmt.Errorf("%s: %w", what, err), res)
	}
	s.applied = false
	return nil
}
