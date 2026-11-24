package steps

// Các bước dựng phần "cô lập" của một site (mục 5, "Cô lập từng site"; mục 6.2
// bước 2 và 3; mục 6.4 "Xoá hẳn"): user Linux riêng, thư mục với đúng quyền,
// file log PHP, và bước xoá cả cây thư mục khi xoá hẳn site.
//
// Tài liệu đã tra (Ubuntu 24.04, shadow 4.13):
//   - https://manpages.ubuntu.com/manpages/noble/man8/useradd.8.html
//   - https://manpages.ubuntu.com/manpages/noble/man8/userdel.8.html
//   - https://manpages.ubuntu.com/manpages/noble/man1/getent.1.html
//   - mã shadow 4.13: src/useradd.c (check_uid_range chỉ cảnh báo; Uflg chỉ
//     lấy từ USERGROUPS_ENAB khi không có -g), libmisc/find_new_gid.c (GID
//     muốn dùng phải nằm trong dải), src/groupadd.c (-g: mã 4 khi GID đã có)
//   - https://systemd.io/UIDS-GIDS/ (các dải UID đã có chủ)
//   - procps pgrep(1), pkill(1); findutils find(1); cron crontab(1)

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	uid, home, shell string
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
	return userEntry{uid: f[2], home: f[5], shell: f[6]}, true, nil
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

// Dải UID (và GID, luôn bằng UID) của user site từ M28 đợt 2 (F1 của rà soát
// v0.2.0). Trước đó useradd --system lấy UID trống CAO NHẤT của dải hệ thống
// (100-999), nên UID của site vừa xoá hẳn được cấp ngay cho site tạo kế tiếp
// của người khác, kèm mọi file sót lại của site cũ (bản bash setuid ở
// /var/tmp). Giờ tpan tự cấp số, tăng dần, không bao giờ trả lại số đã cấp
// (store.ReserveSiteUID giữ số lớn nhất từng cấp trong tpan.db).
//
// Chọn 65536-99999 vì nó không đụng dải nào đã có chủ trên Ubuntu 24.04
// (https://systemd.io/UIDS-GIDS/ và /etc/login.defs):
//   - 0-999 hệ thống (gói cài bằng adduser --system lấy số ở đây), 1000-60000
//     người thật (UID_MIN..UID_MAX), 60001-60577 systemd-homed và
//     nspawn --bind-user, 61184-65519 DynamicUser= của systemd, 65534 nobody,
//     65535 là -1 của uid_t 16 bit;
//   - từ 100000 là dải UID phụ (/etc/subuid, SUB_UID_MIN mặc định 100000) mà
//     useradd cấp cho người thật để chạy container không cần root (podman,
//     LXD từ 1000000): UID site nằm trong đó thì root của container của một
//     người dùng chính là user site.
//
// ponytail: 34464 số cho cả đời máy (mỗi site tạo mới một số, kể cả site thử
// của phép thử tấn công). Hết dải thì SiteUser báo lỗi rõ; nâng cấp: đọc
// /etc/subuid để biết chỗ trống phía trên.
const (
	siteUIDMin = 65536
	siteUIDMax = 99999
	// siteUIDTries: số lần thử khi số vừa cấp đã có người dùng (user hay nhóm
	// từ nguồn NSS khác /etc/passwd, như LDAP). Mỗi lần thử bỏ phí một số.
	siteUIDTries = 100
)

// UIDReserver giữ "UID lớn nhất từng cấp" cho user site; *store.Store thoả.
type UIDReserver interface {
	// ReserveSiteUID ghi lại và trả về max(floor, số lớn nhất từng cấp + 1).
	ReserveSiteUID(ctx context.Context, floor int) (int, error)
}

// SiteUser đảm bảo user Linux l.User có, kèm nhóm riêng cùng tên. PHP của site
// chạy dưới user này; vì nó không thuộc nhóm www-data nên không đọc được file
// của site khác (mục 5).
//
// User mới nhận UID và GID mới (xem siteUIDMin): groupadd --gid N rồi
// useradd --uid N --gid N. Không dùng --user-group: với --system, useradd chỉ
// cho nhóm mang GID bằng UID khi số đó nằm trong SYS_GID_MIN..SYS_GID_MAX
// (find_new_gid, check_gid của shadow 4.13), ngoài dải thì nó lặng lẽ lấy
// GID trống cao nhất của dải hệ thống, tức là dùng lại GID của site đã xoá.
// User site có sẵn (kể cả UID cũ trong dải hệ thống, site tạo trước bản này)
// giữ nguyên: Check chỉ xét tên, home và shell.
type SiteUser struct {
	Layout site.Layout
	// UIDs là nơi cấp số (tpan.db). nil thì Apply lỗi khi phải tạo user: không
	// bao giờ quay về để useradd tự chọn số.
	UIDs UIDReserver

	created bool // chính Apply đã chạy useradd
	applied bool
}

// groupadd tạo nhóm riêng của site với GID đã cấp. Không có --system: cờ đó
// chỉ đổi dải mà groupadd tự chọn số, ở đây số đã cho sẵn.
func (s *SiteUser) groupadd(id string) system.Cmd {
	return system.Cmd{Name: "groupadd", Args: []string{"--gid", id, s.Layout.User}}
}

func (s *SiteUser) useradd(id string) system.Cmd {
	l := s.Layout
	// --system: không có thông tin hết hạn mật khẩu trong /etc/shadow, không
	// cấp dải UID phụ (/etc/subuid). Với --uid cho sẵn, useradd không chọn số
	// trong dải hệ thống nữa; số ngoài SYS_UID_MIN..SYS_UID_MAX chỉ làm nó in
	// một dòng cảnh báo (check_uid_range), không lỗi.
	// --uid, --gid: số do tpan cấp (pickUID), nhóm đã tạo bằng groupadd.
	// --no-create-home: thư mục site do SiteDirs tạo với quyền 2750, không để
	// useradd tạo bằng quyền mặc định của nó.
	// Không có cờ mật khẩu: tài khoản không có mật khẩu, cộng shell nologin là
	// không đăng nhập được bằng bất cứ đường nào.
	return system.Cmd{Name: "useradd", Args: []string{
		"--system", "--uid", id, "--gid", id,
		"--home-dir", l.Dir, "--no-create-home",
		"--shell", userShell,
		l.User,
	}}
}

func (s *SiteUser) groupdel() system.Cmd {
	return system.Cmd{Name: "groupdel", Args: []string{s.Layout.User}}
}

// uidPlaceholder đứng thay số trong Info: số chỉ được cấp lúc Apply, để bản
// xem trước không tiêu số nào và không đổi giữa lúc duyệt và lúc chạy.
const uidPlaceholder = "<UID>"

func (s *SiteUser) Info() engine.StepInfo {
	u := s.Layout.User
	ga, ua := s.groupadd(uidPlaceholder), s.useradd(uidPlaceholder)
	return engine.StepInfo{
		ID:    "site.user:" + u,
		Title: i18n.T("Tạo user Linux "+u+" cho site", "Create Linux user "+u+" for the site"),
		Why: i18n.T(
			"PHP của site chạy dưới user riêng này. Bỏ bước này thì PHP của mọi site chạy chung một user, và một site bị chiếm là đọc được wp-config.php (mật khẩu DB) của mọi site khác trên máy. UID của user là số mới chưa từng cấp (từ 65536 trở lên): dùng lại UID của site đã xoá thì file sót lại của site đó thành của site mới.",
			"The site's PHP runs as this dedicated user. Without it every site's PHP shares one user, so one compromised site can read the wp-config.php (database password) of every other site on the machine. The user gets a UID never handed out before (65536 and up): reusing a deleted site's UID would give its leftover files to the new site.",
		),
		Commands: cmdStrings(&ga, &ua),
	}
}

// pickUID cấp số cho user mới: lớn hơn mọi UID, GID đang có trong
// /etc/passwd, /etc/group nằm trong dải (máy đã có user lạ ở đó, hay
// tpan.db mới trên máy cũ), lớn hơn mọi số tpan từng cấp, và chưa có user
// hay nhóm nào mang số đó theo getent (nguồn NSS khác file).
func (s *SiteUser) pickUID(ctx context.Context, sys system.System) (int, error) {
	if s.UIDs == nil {
		return 0, errors.New("không có nơi ghi UID đã cấp (tpan.db): tpan không để useradd tự chọn số vì nó dùng lại UID của site đã xoá")
	}
	floor := siteUIDMin
	for _, p := range []string{"/etc/passwd", "/etc/group"} {
		b, err := sys.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue // chỉ máy giả (demo, test) mới thiếu hai file này
		}
		if err != nil {
			return 0, fmt.Errorf("đọc %s: %w", p, err)
		}
		for line := range strings.Lines(string(b)) {
			// passwd(5), group(5): trường thứ ba là UID, GID.
			f := strings.SplitN(line, ":", 4)
			if len(f) < 3 {
				continue
			}
			if id, err := strconv.Atoi(f[2]); err == nil && id >= floor && id <= siteUIDMax {
				floor = id + 1
			}
		}
	}
	for range siteUIDTries {
		n, err := s.UIDs.ReserveSiteUID(ctx, floor)
		if err != nil {
			return 0, err
		}
		if n > siteUIDMax {
			return 0, fmt.Errorf("hết dải UID của user site (%d-%d): mọi số đã được cấp một lần", siteUIDMin, siteUIDMax)
		}
		taken, err := idTaken(ctx, sys, n)
		if err != nil {
			return 0, err
		}
		if !taken {
			return n, nil
		}
		floor = n + 1
	}
	return 0, fmt.Errorf("thử %d số UID liền nhau từ %d mà số nào cũng đã có user hay nhóm dùng", siteUIDTries, floor-siteUIDTries)
}

// idTaken: đã có user hay nhóm mang số n chưa (getent passwd n, getent group
// n; getent nhận số làm key). Mã 2 là "không có". Mã 0 kèm một dòng là có.
func idTaken(ctx context.Context, sys system.System, n int) (bool, error) {
	id := strconv.Itoa(n)
	for _, db := range []string{"passwd", "group"} {
		res, err := sys.Run(ctx, system.Cmd{Name: "getent", Args: []string{db, id}})
		if ee, ok := isExit(err); ok && ee.Code == 2 {
			continue
		}
		if err != nil {
			return false, withStdout(fmt.Errorf("hỏi %s %s: %w", db, id, err), res)
		}
		if strings.TrimSpace(res.Stdout) != "" {
			return true, nil
		}
	}
	return false, nil
}

// groupLookup hỏi nhóm name qua `getent group`; trả GID khi có.
func groupLookup(ctx context.Context, sys system.System, name string) (gid int, found bool, err error) {
	res, err := sys.Run(ctx, system.Cmd{Name: "getent", Args: []string{"group", name}})
	if ee, ok := isExit(err); ok && ee.Code == 2 {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, withStdout(fmt.Errorf("hỏi nhóm %s: %w", name, err), res)
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return 0, false, nil
	}
	f := strings.Split(out, ":")
	if len(f) != 4 || f[0] != name {
		return 0, false, fmt.Errorf("hỏi nhóm %s: getent trả dòng lạ %q", name, res.Stdout)
	}
	gid, err = strconv.Atoi(f[2])
	if err != nil {
		return 0, false, fmt.Errorf("hỏi nhóm %s: GID %q không phải số", name, f[2])
	}
	return gid, true, nil
}

// userGroupID chọn số cho user mới. Nhóm cùng tên còn từ một lần Apply bị
// ngắt giữa groupadd và useradd (tiến trình bị giết) thì dùng lại chính số
// của nhóm đó: số trong dải là số tpan đã cấp và đã ghi. Nhóm cùng tên mà
// GID ngoài dải thì không phải của lần chạy dở nào: lỗi, không đụng.
func (s *SiteUser) userGroupID(ctx context.Context, sys system.System) (id int, haveGroup bool, err error) {
	gid, found, err := groupLookup(ctx, sys, s.Layout.User)
	if err != nil || !found {
		if err == nil {
			id, err = s.pickUID(ctx, sys)
		}
		return id, false, err
	}
	if gid < siteUIDMin || gid > siteUIDMax {
		return 0, false, fmt.Errorf("nhóm %s đã có trên máy (GID %d) mà chưa có user cùng tên: không phải nhóm tpan để lại, tpan không đụng tới; xoá nó bằng tay (groupdel %s) nếu chắc là thừa", s.Layout.User, gid, s.Layout.User)
	}
	res, err := sys.Run(ctx, system.Cmd{Name: "getent", Args: []string{"passwd", strconv.Itoa(gid)}})
	if ee, ok := isExit(err); !ok || ee.Code != 2 {
		if err == nil {
			err = fmt.Errorf("UID %d đã thuộc user khác", gid)
		}
		return 0, false, withStdout(fmt.Errorf("dùng lại nhóm %s còn từ lần chạy dở: %w", s.Layout.User, err), res)
	}
	return gid, true, nil
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
	n, haveGroup, err := s.userGroupID(ctx, sys)
	if err != nil {
		return fmt.Errorf("tạo user %s: %w", l.User, err)
	}
	id := strconv.Itoa(n)
	if !haveGroup {
		if res, err := sys.Run(ctx, s.groupadd(id)); err != nil {
			return withStdout(fmt.Errorf("tạo nhóm %s (GID %s): %w", l.User, id, err), res)
		}
	}
	// groupadd, useradd, groupdel đều thay /etc/group bằng rename: gắn lại bản
	// riêng của các lồng sau mỗi lần đổi, kể cả khi useradd lỗi.
	defer cageRebindIdentity(ctx, sys)
	if res, err := sys.Run(ctx, s.useradd(id)); err != nil {
		err = withStdout(fmt.Errorf("tạo user %s (UID %s): %w", l.User, id, err), res)
		// Nhóm (vừa tạo, hay còn từ lần chạy dở) không để lại: số của nó đã
		// ghi trong tpan.db nên lần chạy lại lấy số mới, bỏ phí số này.
		if res, gerr := sys.Run(ctx, s.groupdel()); gerr != nil {
			err = errors.Join(err, withStdout(fmt.Errorf("gỡ nhóm %s vừa tạo: %w", l.User, gerr), res))
		}
		return err
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
		cageRebindIdentity(ctx, sys) // userdel thay /etc/passwd: gắn lại bản riêng của các lồng
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
//
// Trước userdel nó dọn mọi thứ còn chạy hay còn nằm dưới quyền của user đó
// (M28 đợt 2, F1 của rà soát v0.2.0), theo thứ tự:
//  1. crontab riêng của user (cageCrontabGone: `crontab -r -u <user>` khi
//     file spool có, rồi kiểm file đã mất). Crontab gắn với TÊN user, mà tên
//     tp_<slug> sinh từ domain: tạo lại site cùng domain là crontab cũ sống
//     lại dưới user mới. Gỡ trước khi giết để cron không sinh tiến trình mới.
//  2. mọi tiến trình mang UID đó, theo uid thật (-U) hay uid hiệu lực (-u,
//     người khác chạy một file setuid của site): `pkill -KILL` theo vòng tới
//     khi `pgrep` không còn thấy, có hạn userKillWait.
//  3. file (mọi loại trừ thư mục, kể cả symlink, không đi theo symlink) mang
//     UID đó trong /tmp, /var/tmp, /dev/shm, ba chỗ ai cũng ghi được; rồi thư
//     mục rỗng của UID đó. `find -xdev` không sang phân vùng khác.
//
// User không còn mà file crontab cùng tên còn (site xoá hẳn bằng bản trước
// M28 đợt 2) thì xoá file đó: `crontab -u` không nhận user không tồn tại.
//
// ponytail: không dọn hàng đợi `at` (Ubuntu server không cài sẵn at, và atd
// chạy job theo chủ của file job, mà UID đó không bao giờ được cấp lại); nếu
// cần: `atq` rồi `atrm` các job có cột user là user site.
type SiteUserAbsent struct{ Layout site.Layout }

// userTmpDirs là các thư mục ai cũng ghi được (1777) nơi tiến trình của site
// để file lại được ngoài thư mục site.
var userTmpDirs = []string{"/tmp", "/var/tmp", "/dev/shm"}

// Thời hạn chờ tiến trình của user thoát hẳn. Biến để test rút ngắn.
var (
	// userKillWait: SIGKILL giao bất đồng bộ, và tiến trình đã chết vẫn còn
	// (zombie) tới khi cha của nó đọc mã thoát.
	userKillWait = 5 * time.Second
	userKillTick = 100 * time.Millisecond
)

// userPgrep, userPkill: -U là uid thật, -u là uid hiệu lực; pkill, pgrep
// (procps) nhận nhiều tiêu chí thì đòi khớp tất cả, nên mỗi loại một lệnh.
// pgrep, pkill trả 1 khi không tiến trình nào khớp.
func userPgrep(flag, uid string) system.Cmd {
	return system.Cmd{Name: "pgrep", Args: []string{flag, uid}}
}

func userPkill(flag, uid string) system.Cmd {
	return system.Cmd{Name: "pkill", Args: []string{"-KILL", flag, uid}}
}

// userKillAll là `kill -KILL -1` chạy dưới user name, như cageKillAll.
func userKillAll(name string) system.Cmd {
	return system.Cmd{Name: "kill", Args: []string{"-KILL", "-1"}, AsUser: name}
}

// userFindFiles xoá mọi thứ không phải thư mục của uid trong dir. find mặc
// định không đi theo symlink (-P), -delete xoá chính symlink; -xdev đứng
// trước các phép thử (GNU find cảnh báo khi tuỳ chọn chung đứng sau).
func userFindFiles(dir, uid string) system.Cmd {
	return system.Cmd{Name: "find", Args: []string{dir, "-xdev", "-uid", uid, "!", "-type", "d", "-delete"}}
}

// userFindLeft in tên file đầu tiên còn lại của uid trong dir (nếu có).
func userFindLeft(dir, uid string) system.Cmd {
	return system.Cmd{Name: "find", Args: []string{dir, "-xdev", "-uid", uid, "!", "-type", "d", "-print", "-quit"}}
}

// userFindDirs xoá thư mục rỗng của uid (-delete kéo theo -depth: thư mục
// con rỗng đi trước, thư mục cha rỗng theo sau).
func userFindDirs(dir, uid string) system.Cmd {
	return system.Cmd{Name: "find", Args: []string{dir, "-xdev", "-uid", uid, "-type", "d", "-empty", "-delete"}}
}

func (s *SiteUserAbsent) Info() engine.StepInfo {
	l := s.Layout
	u, id := l.User, uidPlaceholder
	cron := cageCrontabRemove(l)
	cmds := []string{cron.String()}
	for _, c := range []system.Cmd{
		userPgrep("-U", id), userPgrep("-u", id),
		userKillAll(u),
		userPkill("-U", id), userPkill("-u", id),
	} {
		cmds = append(cmds, c.String())
	}
	for _, d := range userTmpDirs {
		cmds = append(cmds, userFindFiles(d, id).String(), userFindLeft(d, id).String(), userFindDirs(d, id).String())
	}
	cmds = append(cmds, userDelCmd(u).String())
	return engine.StepInfo{
		ID:    "site.user.absent:" + u,
		Title: i18n.T("Dọn và xoá user Linux "+u, "Clean up and delete Linux user "+u),
		Why: i18n.T(
			"Site đã bị xoá thì user của nó không còn việc gì. Trước khi xoá user, tpan gỡ crontab riêng của nó (tạo lại site cùng tên miền là crontab cũ chạy lại dưới user mới), giết mọi tiến trình còn chạy dưới UID của nó, và xoá file của UID đó trong /tmp, /var/tmp, /dev/shm (ví dụ một bản shell setuid để lại để ai chạy cũng thành user này).",
			"Once the site is gone its user has no purpose. Before deleting the user, tpan removes its personal crontab (re-creating a site with the same domain would bring the old crontab back under the new user), kills every process still running under its UID, and deletes that UID's files in /tmp, /var/tmp and /dev/shm (for example a setuid shell left behind so that anyone running it becomes this user).",
		),
		Commands: cmds,
		Danger:   true,
		NoUndo: i18n.T(
			"Tiến trình đã giết và file đã xoá không lấy lại được; tạo lại user sẽ ra UID khác. Muốn có lại site thì tạo lại cả site.",
			"Killed processes and deleted files cannot be brought back; recreating the user gives it a different UID. To get the site back, create the whole site again.",
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
		return userNoCrontab(sys, s.Layout)
	}
	// User cùng tên mà không phải của tpan thì không xoá.
	if err := userOurs(s.Layout, e); err != nil {
		return false, err
	}
	return false, nil
}

// userNoCrontab: không có file crontab cùng tên user trong spool của cron.
func userNoCrontab(sys system.System, l site.Layout) (bool, error) {
	_, err := sys.Stat(cageCrontab(l))
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", cageCrontab(l), err)
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
		// Crontab mồ côi của user đã xoá bằng bản cũ.
		if err := sys.Remove(cageCrontab(l)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("xoá crontab còn sót của %s (%s): %w", l.User, cageCrontab(l), err)
		}
		return nil
	}
	if err := userOurs(l, e); err != nil {
		return err
	}
	if err := userPurge(ctx, sys, l, e.uid); err != nil {
		return fmt.Errorf("dọn dấu vết của user %s trước khi xoá: %w", l.User, err)
	}
	return userDel(ctx, sys, l.User)
}

// userPurge: crontab, tiến trình, file tạm của user (thứ tự và lý do ở
// SiteUserAbsent). uid 0 bị từ chối: không bao giờ giết hay xoá thứ của root.
func userPurge(ctx context.Context, sys system.System, l site.Layout, uid string) error {
	if n, err := strconv.ParseUint(uid, 10, 32); err != nil || n == 0 {
		return fmt.Errorf("UID của %s là %q: không phải số khác 0", l.User, uid)
	}
	if err := cageCrontabGone(ctx, sys, l); err != nil {
		return err
	}
	if err := userKill(ctx, sys, l.User, uid); err != nil {
		return err
	}
	for _, d := range userTmpDirs {
		if err := userTmpClean(ctx, sys, d, uid); err != nil {
			return err
		}
	}
	return nil
}

// userKill giết mọi tiến trình mang uid tới khi pgrep không còn thấy cái nào.
func userKill(ctx context.Context, sys system.System, name, uid string) error {
	deadline := time.Now().Add(userKillWait)
	for {
		pids, err := userPids(ctx, sys, uid)
		if err != nil || len(pids) == 0 {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sau %v vẫn còn tiến trình của %s (UID %s): %s", userKillWait, name, uid, strings.Join(pids, " "))
		}
		// `kill -KILL -1` dưới chính user đó giết mọi tiến trình cùng uid trong
		// một lượt duyệt của kernel, nên một chuỗi "fork rồi tự thoát" không
		// chạy trước được như với pkill (đọc /proc rồi giết từng pid; soát cuối
		// M17, N2). Lỗi bỏ qua như cageReap (chính lệnh kill bị giết theo, hay
		// runuser từ chối tài khoản đã hết hạn): pkill bằng root ngay sau là
		// đường dự phòng, và vòng sau mới là câu trả lời.
		_, _ = sys.Run(ctx, userKillAll(name))
		for _, f := range []string{"-U", "-u"} {
			res, err := sys.Run(ctx, userPkill(f, uid))
			// 1: không tiến trình nào khớp (vừa tự thoát), không phải lỗi.
			if ee, ok := isExit(err); err != nil && (!ok || ee.Code != 1) {
				return withStdout(fmt.Errorf("giết tiến trình của %s (UID %s): %w", name, uid, err), res)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("chờ tiến trình của %s thoát: %w", name, ctx.Err())
		case <-time.After(userKillTick):
		}
	}
}

// userPids trả pid của mọi tiến trình có uid thật hay uid hiệu lực là uid.
func userPids(ctx context.Context, sys system.System, uid string) ([]string, error) {
	var pids []string
	for _, f := range []string{"-U", "-u"} {
		res, err := sys.Run(ctx, userPgrep(f, uid))
		if ee, ok := isExit(err); ok && ee.Code == 1 {
			continue
		}
		if err != nil {
			return nil, withStdout(fmt.Errorf("tìm tiến trình của UID %s: %w", uid, err), res)
		}
		pids = append(pids, strings.Fields(res.Stdout)...)
	}
	return pids, nil
}

// userTmpClean xoá file của uid trong dir (không có dir thì thôi). find thoát
// khác 0 cả khi một file tự biến mất giữa lúc duyệt (thư mục tạm luôn có file
// vào ra), nên khi lệnh xoá lỗi thì hỏi lại: chỉ lỗi khi còn file của uid,
// hay không hỏi lại được. Thư mục của uid mà còn file của người khác bên
// trong thì để lại (lần xoá thư mục rỗng bỏ qua lỗi): nó rỗng quyền, vì UID
// không bao giờ được cấp lại, và người khác không chặn được việc xoá site
// bằng cách bỏ một file vào thư mục đó.
func userTmpClean(ctx context.Context, sys system.System, dir, uid string) error {
	if _, err := sys.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("kiểm %s: %w", dir, err)
	}
	if res, err := sys.Run(ctx, userFindFiles(dir, uid)); err != nil {
		left, lerr := sys.Run(ctx, userFindLeft(dir, uid))
		if lerr != nil || strings.TrimSpace(left.Stdout) != "" {
			return withStdout(fmt.Errorf("xoá file của UID %s trong %s (còn %q, hỏi lại: %v): %w",
				uid, dir, strings.TrimSpace(left.Stdout), lerr, err), res)
		}
	}
	_, _ = sys.Run(ctx, userFindDirs(dir, uid))
	return nil
}

func (s *SiteUserAbsent) Undo(context.Context, system.System) error { return nil }

// ================================================================== SiteDirs

// SiteDirs trả các bước dựng thư mục của site theo mục 5, đúng thứ tự: thư mục
// cha trước thư mục con, thư mục log trước file log (SiteLogs).
func SiteDirs(l site.Layout) []engine.Step {
	web := system.Owner{User: l.User, Group: site.WebGroup}
	own := system.Owner{User: l.User, Group: l.User}
	return append([]engine.Step{
		// 2750: web server đọc qua nhóm www-data; setgid để file PHP tạo ra
		// (ảnh tải lên) mang nhóm www-data; "khác" không có quyền gì.
		&Dir{Path: l.Dir, Perm: fs.ModeSetgid | 0o750, Owner: web},
		&Dir{Path: l.Public, Perm: fs.ModeSetgid | 0o750, Owner: web},
		// Upload tạm và session PHP: chỉ user của site, web server cũng không đọc.
		&Dir{Path: l.Tmp, Perm: 0o700, Owner: own},
	}, SiteLogs(l)...)
}

// SiteLogs trả các bước quyền log của site (lỗ L6, mục 6.13), đúng thứ tự:
// thư mục log rồi ba file log. Dùng trong SiteDirs (site mới) và trong Plan
// "Áp bản vá cô lập" (site có từ trước M14).
//
// Trước M14 thư mục log là root:root 0755 và web server tự tạo access.log,
// error.log với 0644: user của site nào cũng đọc được log của site khác (IP
// khách, URL kèm tham số). Giờ:
//
//   - Thư mục root:<nhóm của site> 0750. Nhóm là nhóm riêng của user site
//     (useradd --user-group tạo cùng tên), nên site đọc được log của chính nó,
//     site khác thì không vào được thư mục. Chủ vẫn là root để logrotate không
//     phải đổi tên file trong thư mục do user của site kiểm soát (user đó có
//     thể đặt symlink để lừa root ghi đè file khác).
//   - access.log, error.log root:<nhóm của site> 0640, tạo sẵn (rỗng) TRƯỚC
//     khi vhost được ghi: web server mở file có sẵn và giữ nguyên quyền; để nó
//     tự tạo thì file ra 0644. Tiến trình chính của web server chạy bằng root
//     nên vẫn ghi được.
//   - php-error.log như cũ: thuộc user của site để PHP ghi được.
//
// OwnedFile giữ nguyên nội dung file đã có, chỉ sửa quyền và chủ, nên chạy
// trên site đang có log thì không mất dòng nào.
func SiteLogs(l site.Layout) []engine.Step {
	grp := system.Owner{User: "root", Group: l.User}
	own := system.Owner{User: l.User, Group: l.User}
	webWhy := i18n.T(
		"Web server ghi log truy cập và log lỗi của site vào file này. Để web server tự tạo thì file ra quyền 0644 và user của mọi site khác đọc được (IP khách, URL kèm tham số); tạo sẵn với nhóm của site và quyền 0640 thì chỉ root và chính site đó đọc được.",
		"The web server writes the site's access and error log to this file. If the web server creates it, the file gets mode 0644 and every other site's user can read it (visitor IPs, URLs with their parameters); created in advance with the site's group and mode 0640, only root and the site itself can read it.",
	)
	return []engine.Step{
		&Dir{Path: l.LogDir, Perm: 0o750, Owner: grp},
		&OwnedFile{Path: l.AccessLog, Perm: 0o640, Owner: grp, Why: webWhy},
		&OwnedFile{Path: l.ErrorLog, Perm: 0o640, Owner: grp, Why: webWhy},
		&OwnedFile{Path: l.PHPErrorLog, Perm: 0o640, Owner: own},
	}
}

// ================================================================= OwnedFile

// OwnedFile đảm bảo một file có, đúng quyền và chủ, không quan tâm nội dung.
// Dùng cho file log mà chương trình khác ghi (php-error.log, và từ M14 là
// access.log, error.log): File của M1 so nội dung nên sẽ ghi đè log.
type OwnedFile struct {
	Path  string
	Perm  fs.FileMode
	Owner system.Owner
	// Why rỗng thì dùng câu nói về file log do PHP ghi (cách dùng trước M14).
	Why i18n.Text

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
		Why: orText(s.Why, i18n.T(
			"Chương trình chạy dưới user của site (PHP) ghi vào file này nhưng không được tạo file trong thư mục của root. Thiếu file hoặc sai chủ thì lỗi PHP không được ghi lại, lúc site hỏng không có gì để xem.",
			"A program running as the site's user (PHP) writes to this file but may not create files in root's directory. If the file is missing or has the wrong owner, PHP errors are lost and there is nothing to read when the site breaks.",
		)),
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
