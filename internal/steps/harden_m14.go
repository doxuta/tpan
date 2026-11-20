package steps

// Hai file cấp máy của M14 cho MỖI bản PHP (mục 6.13 của docs/KE-HOACH.md; hợp
// đồng trong m14_types.go): file ini của OPcache (lỗ L1, L2) và drop-in
// ProtectProc của dịch vụ php<bản>-fpm (lỗ L7). Nội dung và đường dẫn nằm ở
// internal/templates/m14.go.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

var (
	_ engine.Step = (*PHPHardeningINI)(nil)
	_ engine.Step = (*PHPProtectProc)(nil)
)

// hardenMode: file trong conf.d của PHP và drop-in của systemd đều là file
// config thường, 0644 root. Không có bí mật nào bên trong.
const hardenMode fs.FileMode = 0o644

// hardenVersion trả *p đã kiểm. nil hoặc rỗng lúc chạy nghĩa là DetectPHP chưa
// chạy hoặc Plan ghép sai thứ tự; sai dạng ("8.3; x", "../8.3") thì dừng ở đây,
// trước khi bản PHP được ghép vào đường dẫn, tên chương trình và tên dịch vụ.
func hardenVersion(what string, p *string) (string, error) {
	if p == nil || *p == "" {
		return "", fmt.Errorf("%s: chưa biết bản PHP (DetectPHP phải chạy trước bước này)", what)
	}
	if !phpVersion.MatchString(*p) {
		return "", fmt.Errorf("%s: bản PHP %q không hợp lệ (dạng 8.3)", what, *p)
	}
	return *p, nil
}

// hardenLabel là bản PHP để hiện trong Info: "<bản>" khi chưa biết hoặc sai
// dạng (Info không trả lỗi được, và không được in lại một chuỗi lạ vào lệnh).
func hardenLabel(p *string) string {
	if v, err := hardenVersion("", p); err == nil {
		return v
	}
	return "<bản>"
}

// hardenFile dựng một bước File chỉ để mượn Check của nó: ở đường dẫn đó là
// file thường, đúng nội dung, quyền 0644, chủ root.
func hardenFile(p string, content []byte) *File {
	return &File{Path: p, Content: content, Perm: hardenMode, Owner: system.Root}
}

// phpActive hỏi systemd php<v>-fpm có đang chạy không. is-active trả mã khác 0
// cho "không chạy" (xem Service.state): đó là câu trả lời, không phải lỗi.
func phpActive(ctx context.Context, sys system.System, v string) (bool, error) {
	unit := "php" + v + "-fpm"
	if _, err := sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"is-active", unit}}); err != nil {
		if _, ok := isExit(err); ok {
			return false, nil
		}
		return false, fmt.Errorf("hỏi systemd trạng thái %s: %w", unit, err)
	}
	return true, nil
}

// ============================================================ PHPHardeningINI

// PHPHardeningINI ghi /etc/php/<bản>/fpm/conf.d/99-tpan.ini: hai dòng OPcache
// đóng lỗ L1 và L2 (mục 6.13).
//
// Mọi pool của một bản PHP-FPM dùng chung một bộ nhớ OPcache. Không có
// opcache.validate_permission, PHP của site A include được wp-config.php của
// site B lúc file đang nằm trong cache, dù open_basedir và quyền file đều cấm.
// Không có opcache.restrict_api, site nào cũng liệt kê được file của site khác
// và xoá được cache chung.
//
// Sau khi ghi: `php-fpm<bản> -t` (sai thì trả file cũ), rồi reload NẾU dịch vụ
// đang chạy. Reload là đủ để PHP-FPM đọc lại ini (đo ở t7 của
// docs/bang-chung/M14-nghien-cuu).
//
// Version là con trỏ như PHPIdlePool: Plan cài stack dựng trước khi DetectPHP
// biết bản PHP.
type PHPHardeningINI struct {
	Version *string

	prev    saved
	applied bool
}

const hardenINIWhat = "file ini OPcache của tpan"

func (s *PHPHardeningINI) Info() engine.StepInfo {
	v := hardenLabel(s.Version)
	unit := "php" + v + "-fpm"
	info := engine.StepInfo{
		ID:    "php.harden.opcache:" + v,
		Title: i18n.T("Bật kiểm quyền file và khoá API của OPcache cho "+unit, "Turn on OPcache permission checks and lock its API for "+unit),
		Why: i18n.T(
			"Mọi site dùng chung một bản PHP thì dùng chung một bộ nhớ đệm mã (OPcache). Thiếu file này, một site bị chiếm lấy được mật khẩu cơ sở dữ liệu của site khác bằng cách nạp wp-config.php của site đó từ bộ nhớ đệm, dù quyền file và open_basedir đều cấm; nó còn xem được danh sách file của mọi site và xoá được bộ nhớ đệm chung.",
			"All sites on one PHP version share one code cache (OPcache). Without this file a compromised site can load another site's wp-config.php from the cache and get its database password, even though file permissions and open_basedir both forbid it; it can also list every site's files and wipe the shared cache.",
		),
		Commands: []string{"php-fpm" + v + " -t", "systemctl reload " + unit},
	}
	// Chỉ khai Files khi đã biết bản: preview của Plan cài stack chạy trước
	// DetectPHP, đọc "/etc/php/<bản>/..." thì vô nghĩa.
	if v, err := hardenVersion(hardenINIWhat, s.Version); err == nil {
		info.Files = []engine.FileChange{{Path: templates.PHPHardeningINIPath(v), New: templates.PHPHardeningINI()}}
	}
	return info
}

// Check chỉ đọc file: đúng nội dung, quyền và chủ. Không chạy lệnh nào.
func (s *PHPHardeningINI) Check(ctx context.Context, sys system.System) (bool, error) {
	v, err := hardenVersion(hardenINIWhat, s.Version)
	if err != nil {
		return false, err
	}
	return hardenFile(templates.PHPHardeningINIPath(v), templates.PHPHardeningINI()).Check(ctx, sys)
}

func (s *PHPHardeningINI) Apply(ctx context.Context, sys system.System) error {
	v, err := hardenVersion(hardenINIWhat, s.Version)
	if err != nil {
		return err
	}
	test, reload, err := webPHPCmds(v)
	if err != nil {
		return err
	}
	p := templates.PHPHardeningINIPath(v)
	prev, err := capture(sys, p)
	if err != nil {
		return fmt.Errorf("ghi %s: %w", p, err)
	}
	if err := sys.WriteFileAtomic(p, templates.PHPHardeningINI(), hardenMode, system.Root); err != nil {
		return fmt.Errorf("ghi %s: %w", p, err)
	}
	what := "ghi " + p
	if err := runTest(ctx, sys, test, what); err != nil {
		// Dịch vụ chưa nạp gì: chỉ cần trả file.
		return errors.Join(err, prev.restore(sys))
	}
	if err := phpReloadIfActive(ctx, sys, v, reload, what); err != nil {
		// Reload hỏng: trả file rồi reload lần nữa để PHP-FPM chạy lại bằng
		// config cũ.
		err = errors.Join(err, prev.restore(sys))
		return errors.Join(err, phpReloadIfActive(ctx, sys, v, reload, "trả lại "+p))
	}
	s.prev, s.applied = prev, true
	return nil
}

// Undo trả file về như trước Apply (xoá nếu trước đó chưa có), kiểm, reload.
func (s *PHPHardeningINI) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("%s: %w", hardenINIWhat, errNotApplied)
	}
	v, err := hardenVersion(hardenINIWhat, s.Version)
	if err != nil {
		return err
	}
	test, reload, err := webPHPCmds(v)
	if err != nil {
		return err
	}
	if err := s.prev.restore(sys); err != nil {
		return err
	}
	what := "trả lại " + s.prev.path
	if err := runTest(ctx, sys, test, what); err != nil {
		return err
	}
	return phpReloadIfActive(ctx, sys, v, reload, what)
}

// ============================================================= PHPProtectProc

// PHPProtectProc ghi drop-in systemd
// /etc/systemd/system/php<bản>-fpm.service.d/tpan.conf với
// ProtectProc=invisible (lỗ L7, mục 6.13): PHP của một site, và lệnh shell nó
// gọi, không còn thấy tiến trình và dòng lệnh của user khác trong /proc.
//
// systemd chỉ đọc drop-in khi daemon-reload, và /proc riêng của dịch vụ chỉ
// được dựng lúc dịch vụ khởi động, nên sau khi ghi phải daemon-reload rồi
// RESTART (reload không đủ). Restart ngắt request PHP đang chạy của mọi site
// dùng bản này: Danger. Dịch vụ đang tắt thì không restart: lần khởi động tới
// tự dùng drop-in.
//
// Trước khi restart có `php-fpm<bản> -t`: một PHP-FPM đang chạy với config đã
// hỏng trên đĩa (pool bị sửa tay) vẫn phục vụ bình thường cho tới lúc bị
// restart; restart nó là tắt mọi site của bản này. Kiểm sai thì bước lỗi và
// dịch vụ không bị đụng tới.
type PHPProtectProc struct {
	Version *string

	prev saved
	// dir khác nil: Apply đã tạo (hoặc sửa quyền) thư mục drop-in bằng bước
	// Dir này; Undo gọi Undo của nó.
	dir     *Dir
	applied bool
}

const protectProcWhat = "drop-in ProtectProc của tpan"

var systemdReload = system.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}}

func phpRestartCmd(v string) system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{"restart", "php" + v + "-fpm"}}
}

// protectProcShow hỏi systemd giá trị ProtectProc nó đang giữ cho dịch vụ.
// Cùng dạng lệnh với phép kiểm php.protectproc (checks/m14_types.go).
func protectProcShow(v string) system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{"show", "php" + v + "-fpm", "-p", "ProtectProc", "--value"}}
}

func (s *PHPProtectProc) Info() engine.StepInfo {
	v := hardenLabel(s.Version)
	unit := "php" + v + "-fpm"
	info := engine.StepInfo{
		ID:    "php.harden.proc:" + v,
		Title: i18n.T("Ẩn tiến trình của user khác với PHP của "+unit, "Hide other users' processes from PHP run by "+unit),
		Why: i18n.T(
			"Thiếu bước này, PHP của một site bị chiếm gọi lệnh ps là thấy tiến trình và dòng lệnh của mọi user trên máy, kể cả của site khác. Để có hiệu lực phải khởi động lại "+unit+": request PHP đang chạy của mọi site dùng bản này bị ngắt trong khoảng một giây.",
			"Without this step, PHP on a compromised site can run ps and see the processes and command lines of every user on the machine, other sites included. It only takes effect after a restart of "+unit+": PHP requests in flight on every site using this version are cut for about a second.",
		),
		Commands: []string{systemdReload.String(), "php-fpm" + v + " -t", "systemctl restart " + unit},
		Danger:   true,
	}
	if v, err := hardenVersion(protectProcWhat, s.Version); err == nil {
		info.Files = []engine.FileChange{{Path: templates.PHPDropInPath(v), New: templates.PHPDropIn()}}
	}
	return info
}

// Check: file đúng, và khi dịch vụ đang chạy thì systemd phải đang giữ
// ProtectProc=invisible cho nó. Dịch vụ không chạy thì chỉ xét file.
func (s *PHPProtectProc) Check(ctx context.Context, sys system.System) (bool, error) {
	v, err := hardenVersion(protectProcWhat, s.Version)
	if err != nil {
		return false, err
	}
	if ok, err := hardenFile(templates.PHPDropInPath(v), templates.PHPDropIn()).Check(ctx, sys); err != nil || !ok {
		return false, err
	}
	active, err := phpActive(ctx, sys, v)
	if err != nil || !active {
		return err == nil, err
	}
	res, err := sys.Run(ctx, protectProcShow(v))
	if err != nil {
		return false, withStdout(fmt.Errorf("hỏi systemd ProtectProc của php%s-fpm: %w", v, err), res)
	}
	return strings.TrimSpace(res.Stdout) == "invisible", nil
}

// load là nửa chưa đụng tới dịch vụ: daemon-reload để systemd đọc drop-in đang
// có trên đĩa, hỏi dịch vụ có đang chạy không, và nếu có thì kiểm cú pháp
// PHP-FPM. Trả true khi dịch vụ đang chạy và restart được an toàn.
func (s *PHPProtectProc) load(ctx context.Context, sys system.System, v, what string) (bool, error) {
	if res, err := sys.Run(ctx, systemdReload); err != nil {
		return false, withStdout(fmt.Errorf("nạp lại cấu hình systemd sau khi %s: %w", what, err), res)
	}
	active, err := phpActive(ctx, sys, v)
	if err != nil || !active {
		return false, err
	}
	test, _, err := webPHPCmds(v)
	if err != nil {
		return false, err
	}
	return true, runTest(ctx, sys, test, what)
}

// restart là nửa làm gián đoạn: sau nó dịch vụ dựng lại /proc riêng theo
// drop-in systemd đang giữ.
func (s *PHPProtectProc) restart(ctx context.Context, sys system.System, v, what string) error {
	if res, err := sys.Run(ctx, phpRestartCmd(v)); err != nil {
		return withStdout(fmt.Errorf("khởi động lại php%s-fpm sau khi %s: %w", v, what, err), res)
	}
	return nil
}

func (s *PHPProtectProc) Apply(ctx context.Context, sys system.System) error {
	v, err := hardenVersion(protectProcWhat, s.Version)
	if err != nil {
		return err
	}
	p := templates.PHPDropInPath(v)

	// Thư mục drop-in: mượn bước Dir (tạo 0755 root, nhớ đã tạo gì để gỡ lại).
	// Có sẵn và đúng thì không đụng.
	dir := &Dir{Path: templates.PHPDropInDir(v), Perm: 0o755, Owner: system.Root}
	ok, err := dir.Check(ctx, sys)
	if err != nil {
		return fmt.Errorf("ghi %s: %w", p, err)
	}
	if ok {
		dir = nil
	}
	prev, err := capture(sys, p)
	if err != nil {
		return fmt.Errorf("ghi %s: %w", p, err)
	}
	if dir != nil {
		if err := dir.Apply(ctx, sys); err != nil {
			return err // Dir.Apply tự dọn thư mục nó tạo dở
		}
	}
	undoFiles := func() error {
		err := prev.restore(sys)
		if dir != nil {
			err = errors.Join(err, dir.Undo(ctx, sys))
		}
		return err
	}
	if err := sys.WriteFileAtomic(p, templates.PHPDropIn(), hardenMode, system.Root); err != nil {
		return errors.Join(fmt.Errorf("ghi %s: %w", p, err), undoFiles())
	}

	// rollback trả file rồi daemon-reload để systemd quên drop-in vừa đọc.
	rollback := func(err error) error {
		err = errors.Join(err, undoFiles())
		if res, rerr := sys.Run(ctx, systemdReload); rerr != nil {
			err = errors.Join(err, withStdout(fmt.Errorf("nạp lại cấu hình systemd sau khi trả lại %s: %w", p, rerr), res))
		}
		return err
	}
	what := "ghi " + p
	active, err := s.load(ctx, sys, v, what)
	if err != nil {
		// Dịch vụ chưa bị đụng tới: chỉ cần trả file.
		return rollback(err)
	}
	if active {
		if err := s.restart(ctx, sys, v, what); err != nil {
			// Restart hỏng thì dịch vụ có thể đang tắt: trả file rồi restart
			// lần nữa để nó chạy lại bằng cấu hình cũ.
			return errors.Join(rollback(err), s.restart(ctx, sys, v, "trả lại "+p))
		}
	}
	s.prev, s.dir, s.applied = prev, dir, true
	return nil
}

// Undo trả file về như trước Apply (xoá file và thư mục nếu chính bước này
// tạo), daemon-reload, restart nếu đang chạy.
func (s *PHPProtectProc) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("%s: %w", protectProcWhat, errNotApplied)
	}
	v, err := hardenVersion(protectProcWhat, s.Version)
	if err != nil {
		return err
	}
	if err := s.prev.restore(sys); err != nil {
		return err
	}
	if s.dir != nil {
		if err := s.dir.Undo(ctx, sys); err != nil {
			return err
		}
	}
	what := "trả lại " + s.prev.path
	active, err := s.load(ctx, sys, v, what)
	if err != nil || !active {
		return err
	}
	return s.restart(ctx, sys, v, what)
}

// ================================================================ PHPHardening

// PHPHardening trả hai bước trên theo đúng thứ tự (ini rồi drop-in) cho một
// bản PHP đã biết. Thứ tự có chủ ý: bước ini chỉ reload; bước drop-in restart,
// và lần restart đó cũng nạp luôn file ini.
func PHPHardening(version string) ([]engine.Step, error) {
	if _, err := hardenVersion("bản vá cô lập của PHP", &version); err != nil {
		return nil, err
	}
	return []engine.Step{
		&PHPHardeningINI{Version: &version},
		&PHPProtectProc{Version: &version},
	}, nil
}
