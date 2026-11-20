package steps

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// dpkgInstalled là giá trị trường Status của một gói đã cài xong
// (want=install, cờ lỗi=ok, trạng thái=installed). Xem `man dpkg-query`.
const dpkgInstalled = "install ok installed"

// missingPackages hỏi dpkg những gói nào trong pkgs chưa cài xong.
//
// Lệnh là `dpkg-query --show --showformat=...`; dpkg tự hiểu \t và \n trong
// format. Mỗi dòng có tên gói nên không phụ thuộc thứ tự dpkg in ra. Gói có
// nhiều kiến trúc (amd64, i386) in nhiều dòng; một dòng đã cài là đủ.
//
// Mã thoát 1 nghĩa là có gói dpkg chưa từng biết: đó là câu trả lời "chưa
// cài", không phải lỗi. Mã khác (2: lỗi đọc cơ sở dữ liệu dpkg) mới là lỗi.
func missingPackages(ctx context.Context, sys system.System, pkgs []string) ([]string, error) {
	cmd := dpkgQuery(pkgs)
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		if ee, ok := isExit(err); !ok || ee.Code != 1 {
			return nil, fmt.Errorf("hỏi dpkg trạng thái gói: %w", err)
		}
	}
	ok := map[string]bool{}
	for line := range strings.Lines(res.Stdout) {
		name, status, _ := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if status == dpkgInstalled {
			ok[name] = true
		}
	}
	var missing []string
	for _, p := range pkgs {
		if !ok[p] {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

func dpkgQuery(pkgs []string) system.Cmd {
	return system.Cmd{Name: "dpkg-query", Args: append([]string{"--show", `--showformat=${Package}\t${Status}\n`}, pkgs...)}
}

// ================================================================== AptUpdate

// AptUpdate chạy `apt-get update` để apt biết bản gói mới nhất trên kho.
//
// Check trả true khi mọi gói trong Packages đã cài: lúc đó bước cài sau cũng
// được bỏ qua nên không cần làm mới danh sách gói, và chạy lại Plan không tốn
// thời gian tải lại.
type AptUpdate struct {
	Packages []string

	// updated: Apply đã chạy trong lần này. Engine gọi Check lại ngay sau
	// Apply, mà lúc đó gói vẫn chưa cài (bước cài nằm sau), nên Check phải nhớ
	// là đã update xong, nếu không engine coi bước này là lỗi.
	updated bool
}

var aptUpdateCmd = system.Cmd{Name: "apt-get", Args: []string{"update"}}

// listsLockTries, listsLockWait: RunAptUpdate chờ tối đa 60 × 5 giây = 5 phút
// (bằng DPkg::Lock::Timeout=300 của các lệnh apt-get khác). Test đặt wait = 0.
var (
	listsLockTries = 60
	listsLockWait  = 5 * time.Second
)

// RunAptUpdate chạy cmd (một lệnh `apt-get update`) và chạy lại khi một
// apt-get update khác đang giữ khoá danh sách gói. Đã kiểm trên máy ảo M12
// (apt 2.8.3, Ubuntu 24.04): `-o DPkg::Lock::Timeout=...` chỉ làm apt chờ khoá
// dpkg; với khoá /var/lib/apt/lists/lock thì `apt-get update` lỗi ngay ("E:
// Could not get lock /var/lib/apt/lists/lock. It is held by process ...", mã
// 100). Máy vừa khởi động (VPS mới, bản sao máy ảo) có apt-daily chạy đúng
// lệnh đó, nên không chờ thì `tpan update --check` và mọi bước thêm kho lỗi.
func RunAptUpdate(ctx context.Context, sys system.System, cmd system.Cmd) (system.Result, error) {
	for i := 1; ; i++ {
		res, err := sys.Run(ctx, cmd)
		if err == nil || i >= listsLockTries || !strings.Contains(err.Error()+res.Stderr+res.Stdout, "/var/lib/apt/lists/lock") {
			return res, err
		}
		select {
		case <-ctx.Done():
			return res, errors.Join(err, ctx.Err())
		case <-time.After(listsLockWait):
		}
	}
}

func (s *AptUpdate) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "apt.update:lists",
		Title: i18n.T("Làm mới danh sách gói (apt-get update)", "Refresh the package lists (apt-get update)"),
		Why: i18n.T(
			"apt chỉ cài được bản gói mà nó biết. Máy mới tạo thường có danh sách gói cũ hoặc trống, nên bỏ bước này thì bước cài gói báo không tìm thấy gói hoặc cài bản cũ còn lỗ hổng.",
			"apt can only install versions it knows about. A new machine often has stale or empty package lists, so without this step the install step cannot find packages or installs old versions with known holes.",
		),
		Commands: []string{aptUpdateCmd.String()},
		NoUndo: i18n.T(
			"apt-get update chỉ làm mới danh sách gói, không cài hay gỡ gì, nên không có gì để hoàn tác.",
			"apt-get update only refreshes the package lists; it installs or removes nothing, so there is nothing to undo.",
		),
	}
}

func (s *AptUpdate) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return false, err
	}
	if s.updated {
		return true, nil
	}
	missing, err := missingPackages(ctx, sys, s.Packages)
	return len(missing) == 0, err
}

func (s *AptUpdate) Apply(ctx context.Context, sys system.System) error {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return err
	}
	if res, err := RunAptUpdate(ctx, sys, aptUpdateCmd); err != nil {
		return withStdout(fmt.Errorf("làm mới danh sách gói: %w", err), res)
	}
	s.updated = true
	return nil
}

func (s *AptUpdate) Undo(context.Context, system.System) error { return nil }

// ================================================================= AptInstall

// AptInstall cài các gói bằng `apt-get install -y`.
type AptInstall struct {
	Packages []string
}

// modPHP là các gói kéo theo mod_php của Apache (mục 3): `php` và `php8.3`
// có libapache2-mod-php là lựa chọn đầu trong Depends, nên apt chọn nó khi máy
// chưa có php-fpm; libapache2-mod-php* chính là mod_php. mod_php buộc Apache
// về MPM prefork và chạy PHP dưới www-data, phá cách cô lập từng site.
var modPHP = regexp.MustCompile(`^(php[0-9.]*|libapache2-mod-php.*)$`)

func (s *AptInstall) validate() error {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return err
	}
	for _, p := range s.Packages {
		if modPHP.MatchString(p) {
			return fmt.Errorf("không cài gói %q: nó kéo theo mod_php và buộc Apache về MPM prefork (mục 3 của KE-HOACH.md); dùng php-fpm", p)
		}
	}
	return nil
}

// cmd chờ khoá dpkg tối đa 5 phút (aptLock): VPS vừa tạo có unattended-upgrades
// giữ khoá, thiếu tuỳ chọn này thì `tpan stack install` (và install.sh) lỗi
// ngay "Could not get lock /var/lib/dpkg/lock-frontend" (gặp 20/11/2026 khi cài
// trên máy ảo vừa khởi động).
func (s *AptInstall) cmd() system.Cmd {
	return aptGet([]string{"install", "-y"}, s.Packages...)
}

func (s *AptInstall) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "apt.install:" + strings.Join(s.Packages, ","),
		Title: i18n.T("Cài gói: "+strings.Join(s.Packages, " "), "Install packages: "+strings.Join(s.Packages, " ")),
		Why: i18n.T(
			"Đây là phần mềm chạy site: web server, PHP-FPM, MariaDB, certbot. Thiếu gói nào thì mọi bước cấu hình nó phía sau đều lỗi vì chưa có file hay dịch vụ để cấu hình.",
			"This is the software that runs the sites: web server, PHP-FPM, MariaDB, certbot. If a package is missing, every later step that configures it fails because there is no file or service to configure.",
		),
		Commands: []string{s.cmd().String()},
		NoUndo: i18n.T(
			"Gói đã cài không được gỡ khi hoàn tác: gỡ gói có thể kéo theo gỡ gói khác đang dùng chung thư viện. Gói thừa không làm hỏng gì; muốn gỡ thì dùng apt-get remove bằng tay.",
			"Installed packages are not removed on undo: removing a package can take other packages that share its libraries with it. An extra package breaks nothing; remove it by hand with apt-get remove if needed.",
		),
	}
}

func (s *AptInstall) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	missing, err := missingPackages(ctx, sys, s.Packages)
	return len(missing) == 0, err
}

func (s *AptInstall) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("cài gói %s: %w", strings.Join(s.Packages, " "), err), res)
	}
	return nil
}

func (s *AptInstall) Undo(context.Context, system.System) error { return nil }

// ==================================================================== Service

// Service đảm bảo một dịch vụ systemd đang chạy và được bật khi khởi động.
type Service struct {
	Unit string

	wasActive, wasEnabled bool
	applied               bool
}

func (s *Service) validate() error {
	if !unitName.MatchString(s.Unit) {
		return fmt.Errorf("tên dịch vụ %q trống hoặc có ký tự không cho phép", s.Unit)
	}
	return nil
}

func (s *Service) enableCmd() system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{"enable", "--now", s.Unit}}
}

// resetCmd xoá trạng thái failed và bộ đếm start rate limit của unit (xem Apply).
func (s *Service) resetCmd() system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{"reset-failed", s.Unit}}
}

func (s *Service) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "service:" + s.Unit,
		Title: i18n.T("Chạy và bật "+s.Unit+" khi khởi động", "Start "+s.Unit+" and enable it at boot"),
		Why: i18n.T(
			"Gói đã cài nhưng dịch vụ không chạy thì site không trả lời. Không bật khi khởi động thì sau lần khởi động lại máy, mọi site chết cho tới khi có người vào bật tay.",
			"An installed package whose service is not running answers nothing. If it is not enabled at boot, every site is down after the next reboot until someone starts it by hand.",
		),
		Commands: []string{s.resetCmd().String(), s.enableCmd().String()},
	}
}

// state hỏi systemd. `is-active` và `is-enabled` trả mã thoát khác 0 cho "không"
// (is-active: 3 khi inactive; is-enabled: 1 khi disabled, 4 khi không có unit),
// nên mọi *ExitError là câu trả lời, chỉ lỗi khác mới là lỗi.
func (s *Service) state(ctx context.Context, sys system.System) (active, enabled bool, err error) {
	ask := func(verb string) (bool, error) {
		_, err := sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{verb, s.Unit}})
		if err == nil {
			return true, nil
		}
		if _, ok := isExit(err); ok {
			return false, nil
		}
		return false, fmt.Errorf("hỏi systemd trạng thái %s: %w", s.Unit, err)
	}
	if active, err = ask("is-active"); err != nil {
		return false, false, err
	}
	enabled, err = ask("is-enabled")
	return active, enabled, err
}

func (s *Service) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	active, enabled, err := s.state(ctx, sys)
	return active && enabled, err
}

func (s *Service) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	active, enabled, err := s.state(ctx, sys)
	if err != nil {
		return err
	}
	// Dịch vụ vừa chết liên tục (ví dụ sau `tpan self-update --rollback`: file
	// chạy cũ không mở được tpan.db của bản mới) thì systemd chặn mọi lần start
	// trong StartLimitIntervalSec ("Start request repeated too quickly"), kể cả
	// khi nguyên nhân đã hết, và `enable --now` vẫn thoát 0 trong khi dịch vụ
	// nằm ở trạng thái failed (đo trên máy ảo, e2e nâng cấp của M13 chạy lại ở
	// M14). reset-failed xoá bộ đếm đó. Lỗi của nó bỏ qua: unit chưa từng được
	// nạp thì systemd báo "not loaded", và khi đó cũng không có bộ đếm nào.
	_, _ = sys.Run(ctx, s.resetCmd())
	if res, err := sys.Run(ctx, s.enableCmd()); err != nil {
		// enable --now có thể đã bật mà start lỗi: trả về như cũ cho sạch.
		err = withStdout(fmt.Errorf("bật dịch vụ %s: %w", s.Unit, err), res)
		if rerr := s.revert(ctx, sys, active, enabled); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	s.wasActive, s.wasEnabled, s.applied = active, enabled, true
	return nil
}

func (s *Service) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("dịch vụ %s: %w", s.Unit, errNotApplied)
	}
	return s.revert(ctx, sys, s.wasActive, s.wasEnabled)
}

// revert chỉ đổi phần đã khác so với trước: dịch vụ vốn đang chạy thì không dừng.
func (s *Service) revert(ctx context.Context, sys system.System, wasActive, wasEnabled bool) error {
	var cmds []system.Cmd
	if !wasEnabled {
		cmds = append(cmds, system.Cmd{Name: "systemctl", Args: []string{"disable", s.Unit}})
	}
	if !wasActive {
		cmds = append(cmds, system.Cmd{Name: "systemctl", Args: []string{"stop", s.Unit}})
	}
	for _, c := range cmds {
		if res, err := sys.Run(ctx, c); err != nil {
			return withStdout(fmt.Errorf("trả dịch vụ %s về như cũ: %w", s.Unit, err), res)
		}
	}
	return nil
}
