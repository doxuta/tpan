package main

// Lệnh `tpan brain-install`: nút "Nối bộ não" của panel (một lần bấm).
//
//	panel (trình duyệt) ──mở link tpan-brain://connect──► TPAN Brain.app (máy cá nhân, macOS)
//	                                                         │ open -a Terminal noi-bo-nao.command
//	                                                         ▼
//	                                                   Terminal: exec tpan brain <ssh-đích>
//
// Trang web của panel nằm trên máy chủ nên không tự chạy được chương trình trên
// máy cá nhân. Lệnh này cài một app nhỏ (AppleScript, dựng bằng osacompile)
// đăng ký link tpan-brain://. Bấm nút trên panel là trình duyệt mở link đó,
// macOS gọi app, app mở Terminal chạy đúng lệnh bạn vẫn gõ tay.
//
// An toàn:
//   - Link không mang gì được dùng: đích ssh ghi cố định lúc cài. Trang nào
//     cũng mở được link tpan-brain:// (Chrome hỏi trước), nhưng việc duy nhất
//     nó làm được là nối bộ não vào đúng máy chủ bạn đã chọn.
//   - Đang có bộ não chạy tới đích đó thì chỉ đưa Terminal lên trước, không
//     mở thêm bộ não thứ hai.
//   - Mở Terminal bằng `open`, không bằng AppleScript "tell application", nên
//     không phải xin quyền điều khiển Terminal (Automation) của macOS.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

func init() { register(brainInstallCmd) }

const (
	brainLinkScheme  = "tpan-brain"
	brainAppName     = "TPAN Brain.app"
	brainAppID       = "io.github.doxuta.tpan.brain"
	brainCommandFile = "noi-bo-nao.command"
	lsregisterPath   = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
)

// Test thay hai biến này: chạy được trên Linux và không phụ thuộc file chạy thật.
var (
	brainInstallGOOS = runtime.GOOS
	brainInstallExe  = func() (string, error) {
		p, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(p)
	}
)

func brainInstallCmd(e *env) *cobra.Command {
	var remove bool
	c := &cobra.Command{
		Use:   "brain-install <ssh-đích> | --remove",
		Short: "Chạy trên Mac: cài nút Nối bộ não để panel nối bộ não bằng một lần bấm",
		Long: `Chạy trên máy cá nhân (macOS, không cần sudo), làm một lần. Cài app nhỏ
~/Applications/TPAN Brain.app đăng ký link tpan-brain://. Từ đó nút "Nối bộ não"
trên panel (Cài đặt → AI và bộ não, và ngăn Hỏi AI) mở Terminal chạy
tpan brain <ssh-đích>. Đích ssh ghi cố định lúc cài; link không mang tham số
nào. Chạy lại lệnh để đổi đích; --remove để gỡ app.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if brainInstallGOOS != "darwin" {
				return fail(e, false, &core.RefusedError{Code: "brain_install_not_mac", Message: i18n.T(
					"tpan brain-install chỉ có trên macOS. Máy khác thì gõ tpan brain <ssh-đích> trong một terminal và giữ nó mở.",
					"tpan brain-install only works on macOS. Elsewhere, run tpan brain <ssh-target> in a terminal and keep it open.")})
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("tìm thư mục nhà: %w", err)
			}
			app := filepath.Join(home, "Applications", brainAppName)
			if remove {
				if len(args) != 0 {
					return fail(e, false, &core.RefusedError{Code: "brain_install_args", Message: i18n.T(
						"--remove không nhận đích ssh: chạy tpan brain-install --remove",
						"--remove takes no ssh target: run tpan brain-install --remove")})
				}
				return removeBrainApp(cmd, e, app)
			}
			if len(args) != 1 {
				return fail(e, false, &core.RefusedError{Code: "brain_target_missing", Message: i18n.T(
					"cần đúng một máy chủ, ví dụ: tpan brain-install ubuntu@example.com (hoặc tên đã khai trong ~/.ssh/config)",
					"give exactly one server, e.g. tpan brain-install ubuntu@example.com (or a name from ~/.ssh/config)")})
			}
			target := args[0]
			if !sshTargetRe.MatchString(target) {
				return fail(e, false, &core.RefusedError{Code: "brain_target_invalid", Message: i18n.T(
					"đích ssh "+quote(target)+" không hợp lệ (dạng máy hoặc user@máy, không bắt đầu bằng -)",
					"ssh target "+quote(target)+" is invalid (use host or user@host, not starting with -)")})
			}
			exe, err := brainInstallExe()
			if err != nil {
				return fmt.Errorf("tìm file chạy tpan: %w", err)
			}
			return installBrainApp(cmd, e, app, exe, target)
		},
	}
	c.Flags().BoolVar(&remove, "remove", false, "gỡ TPAN Brain.app")
	return c
}

// brainAppIsOurs: app ở đường dẫn đó do tpan brain-install cài (có file
// .command của nó). App khác trùng tên thì không bao giờ bị ghi đè hay xoá.
func brainAppIsOurs(app string) bool {
	_, err := os.Stat(filepath.Join(app, "Contents", "Resources", brainCommandFile))
	return err == nil
}

func installBrainApp(cmd *cobra.Command, e *env, app, exe, target string) error {
	ctx := cmd.Context()
	if _, err := os.Stat(app); err == nil {
		if !brainAppIsOurs(app) {
			return fail(e, false, &core.RefusedError{Code: "brain_install_foreign_app", Message: i18n.T(
				"đã có "+quote(app)+" nhưng không phải app do tpan cài: đổi tên hoặc xoá nó rồi chạy lại",
				quote(app)+" exists but was not installed by tpan: rename or remove it, then run again")})
		}
		if err := os.RemoveAll(app); err != nil {
			return fmt.Errorf("xoá bản cũ %s: %w", app, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("xem %s: %w", app, err)
	}
	if err := os.MkdirAll(filepath.Dir(app), 0o755); err != nil {
		return fmt.Errorf("tạo %s: %w", filepath.Dir(app), err)
	}

	tmp, err := os.MkdirTemp("", "tpan-brain-app-")
	if err != nil {
		return fmt.Errorf("tạo thư mục tạm: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	src := filepath.Join(tmp, "TPAN Brain.applescript")
	if err := os.WriteFile(src, []byte(brainAppleScript(filepath.Base(exe), target)), 0o600); err != nil {
		return fmt.Errorf("ghi mã AppleScript: %w", err)
	}

	run := func(name string, args ...string) error {
		res, err := e.sys.Run(ctx, system.Cmd{Name: name, Args: args})
		if err != nil {
			return fmt.Errorf("%s %s: %w\n%s", filepath.Base(name), strings.Join(args, " "), err, strings.TrimSpace(res.Stdout+res.Stderr))
		}
		return nil
	}
	if err := run("/usr/bin/osacompile", "-o", app, src); err != nil {
		return err
	}
	res := filepath.Join(app, "Contents", "Resources")
	if err := os.MkdirAll(res, 0o755); err != nil {
		return fmt.Errorf("tạo %s: %w", res, err)
	}
	if err := os.WriteFile(filepath.Join(res, brainCommandFile), []byte(brainCommandScript(exe, target)), 0o755); err != nil {
		return fmt.Errorf("ghi %s: %w", brainCommandFile, err)
	}
	plist := filepath.Join(app, "Contents", "Info.plist")
	urlTypes := `[{"CFBundleURLName":"TPAN brain","CFBundleURLSchemes":["` + brainLinkScheme + `"]}]`
	steps := [][]string{
		{"/usr/bin/plutil", "-replace", "CFBundleIdentifier", "-string", brainAppID, plist},
		{"/usr/bin/plutil", "-replace", "CFBundleURLTypes", "-json", urlTypes, plist},
		// Không hiện trên Dock: app chỉ chạy một thoáng rồi thoát.
		{"/usr/bin/plutil", "-replace", "LSUIElement", "-bool", "YES", plist},
		// Sửa Info.plist làm hỏng chữ ký của osacompile: ký lại (ad-hoc, chỉ máy này).
		{"/usr/bin/codesign", "--force", "--deep", "--sign", "-", app},
		// Báo LaunchServices có app mới nhận link tpan-brain://.
		{lsregisterPath, "-f", app},
	}
	for _, s := range steps {
		if err := run(s[0], s[1:]...); err != nil {
			return err
		}
	}
	printf(e.stdout, "%s\n", e.tr(
		fmt.Sprintf("Đã cài %s.\nNút \"Nối bộ não\" trên panel giờ mở Terminal chạy: %s brain %s\nLần đầu bấm, trình duyệt hỏi có mở TPAN Brain không: chọn luôn cho phép rồi mở.\nThử ngay không cần panel: open %s://connect", app, exe, target, brainLinkScheme),
		fmt.Sprintf("Installed %s.\nThe \"Connect brain\" button in the panel now opens Terminal running: %s brain %s\nThe first time, the browser asks whether to open TPAN Brain: always allow, then open.\nTry it without the panel: open %s://connect", app, exe, target, brainLinkScheme)))
	return nil
}

func removeBrainApp(cmd *cobra.Command, e *env, app string) error {
	if _, err := os.Stat(app); errors.Is(err, fs.ErrNotExist) {
		printf(e.stdout, "%s\n", e.tr("Chưa cài "+app+", không có gì để gỡ.", app+" is not installed, nothing to remove."))
		return nil
	}
	if !brainAppIsOurs(app) {
		return fail(e, false, &core.RefusedError{Code: "brain_install_foreign_app", Message: i18n.T(
			quote(app)+" không phải app do tpan cài nên tpan không xoá",
			quote(app)+" was not installed by tpan, so tpan will not remove it")})
	}
	// Bỏ đăng ký trước; lỗi ở đây không chặn việc xoá.
	_, _ = e.sys.Run(cmd.Context(), system.Cmd{Name: lsregisterPath, Args: []string{"-u", app}})
	if err := os.RemoveAll(app); err != nil {
		return fmt.Errorf("xoá %s: %w", app, err)
	}
	printf(e.stdout, "%s\n", e.tr("Đã gỡ "+app+". Nút Nối bộ não trên panel sẽ không mở gì nữa.", "Removed "+app+". The Connect brain button in the panel will do nothing now."))
	return nil
}

// brainRunningPattern là regex (ERE của pgrep -f) khớp dòng lệnh của một
// `tpan brain <đích>` đang chạy, gõ tay (bin/tpan) hay do app mở (đường dẫn
// đầy đủ). Neo cuối dòng nên không khớp chính shell đang chạy pgrep: dòng
// lệnh của shell đó còn đuôi "&& echo ..." phía sau.
func brainRunningPattern(exeBase, target string) string {
	return "(^|/)" + regexp.QuoteMeta(exeBase) + " brain " + regexp.QuoteMeta(target) + "$"
}

// appleString bọc s thành chuỗi AppleScript ("..." với \ và " đã thoát).
func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// brainAppleScript là mã của TPAN Brain.app. Chỉ ASCII (osacompile chạy với
// LC_ALL=C), không chứa đường dẫn file chạy: file .command trong Resources
// mới giữ lệnh, app tìm nó qua "path to me".
func brainAppleScript(exeBase, target string) string {
	return `-- TPAN Brain: tao boi "tpan brain-install". Dung sua tay: chay lai lenh do de doi.
-- Link ` + brainLinkScheme + `://... chi la tin hieu "noi bo nao"; khong doc gi tu link.
on open location theURL
	if theURL starts with "` + brainLinkScheme + `:" then connectBrain()
end open location

on run
	connectBrain()
end run

on connectBrain()
	set isUp to do shell script "/usr/bin/pgrep -f " & quoted form of ` + appleString(brainRunningPattern(exeBase, target)) + ` & " >/dev/null && echo yes || echo no"
	if isUp is "yes" then
		do shell script "/usr/bin/open -a Terminal"
	else
		set f to (POSIX path of (path to me)) & "Contents/Resources/` + brainCommandFile + `"
		do shell script "/usr/bin/open -a Terminal " & quoted form of f
	end if
end connectBrain
`
}

// shq bọc s trong nháy đơn cho sh.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// brainCommandScript là file .command Terminal chạy: exec để dòng lệnh của
// tiến trình đúng là "<exe> brain <đích>" (pgrep của app nhận ra nó).
func brainCommandScript(exe, target string) string {
	return "#!/bin/sh\n" +
		"# Tạo bởi tpan brain-install. TPAN Brain.app mở file này khi bạn bấm \"Nối bộ não\" trên panel.\n" +
		"# Bấm Ctrl+C hoặc đóng cửa sổ này là ngắt bộ não.\n" +
		"exec " + shq(exe) + " brain " + shq(target) + "\n"
}
