package steps

// PHPIdlePool: pool www thừa của gói php<bản>-fpm (mục 16 của
// docs/KE-HOACH.md, dòng "Còn mở" ở M2; hợp đồng trong m12_types.go).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var _ engine.Step = (*PHPIdlePool)(nil)

// PHPIdlePool thay pool www của gói php<bản>-fpm bằng pool tpan-idle.
//
// Pool www chạy sẵn 2 worker dưới www-data dù không site nào dùng; nhiều bản
// PHP thì mỗi bản một pool như thế. Nhưng bỏ www.conf mà không thêm gì thì
// PHP-FPM không chạy nữa: đã kiểm trên máy ảo M12, `php-fpm8.3 -t` báo "No
// pool defined" mã 78. Nên ghi tpan-idle.conf trước (ondemand, socket chỉ
// root: không có tiến trình con, không ai gửi request vào được), rồi mới gỡ
// www.conf, để không lúc nào thư mục pool.d trống.
//
// Version là con trỏ: Plan cài stack dựng trước khi DetectPHP biết bản PHP.
type PHPIdlePool struct {
	Version *string
	// Render sinh nội dung tpan-idle.conf (templates.PHPIdlePool). Step không
	// import templates để test đưa được một hàm giả.
	Render func(version string) ([]byte, error)

	prevIdle, prevWWW saved
	applied           bool
}

// phpIdleMode: file pool như mọi file config tpan ghi, 0644 root.
const phpIdleMode fs.FileMode = 0o644

func phpPoolDir(v string) string { return phpRoot + "/" + v + "/fpm/pool.d" }

// version trả *Version đã kiểm. Rỗng lúc chạy nghĩa là DetectPHP chưa chạy
// hoặc Plan ghép sai thứ tự; lỗi ngay thay vì ghi vào /etc/php//fpm.
func (s *PHPIdlePool) version() (string, error) {
	if s.Version == nil || *s.Version == "" {
		return "", errors.New("pool tpan-idle: chưa biết bản PHP (DetectPHP phải chạy trước bước này)")
	}
	v := *s.Version
	if !phpVersion.MatchString(v) {
		return "", fmt.Errorf("pool tpan-idle: bản PHP %q không hợp lệ (dạng 8.3)", v)
	}
	if s.Render == nil {
		return "", errors.New("pool tpan-idle: thiếu hàm Render")
	}
	return v, nil
}

// prepare kiểm tham số rồi sinh nội dung, trước khi chạm vào máy.
func (s *PHPIdlePool) prepare() (v string, conf []byte, test, reload *system.Cmd, err error) {
	if v, err = s.version(); err != nil {
		return "", nil, nil, nil, err
	}
	if test, reload, err = webPHPCmds(v); err != nil {
		return "", nil, nil, nil, err
	}
	if conf, err = s.Render(v); err != nil {
		return "", nil, nil, nil, fmt.Errorf("sinh pool tpan-idle cho PHP %s: %w", v, err)
	}
	return v, conf, test, reload, nil
}

func (s *PHPIdlePool) Info() engine.StepInfo {
	v := "<bản>"
	if s.Version != nil && *s.Version != "" {
		v = *s.Version
	}
	unit := "php" + v + "-fpm"
	info := engine.StepInfo{
		ID:    "php.idle:" + v,
		Title: i18n.T("Thay pool www của "+unit+" bằng pool tpan-idle", "Replace the www pool of "+unit+" with the tpan-idle pool"),
		Why: i18n.T(
			"Gói "+unit+" tự tạo pool www chạy sẵn 2 tiến trình dưới user www-data dù không site nào dùng: tốn RAM, và là một cửa chạy PHP không thuộc site nào. Không gỡ được nó mà để trống, vì PHP-FPM từ chối chạy khi không có pool nào; pool tpan-idle giữ chỗ, chỉ root nói chuyện được với nó và không có tiến trình nào chạy sẵn.",
			"The "+unit+" package creates a www pool that keeps 2 processes running as www-data even though no site uses it: it wastes RAM and is a way to run PHP that belongs to no site. It cannot simply be removed, because PHP-FPM refuses to start without any pool; the tpan-idle pool holds that place, only root can talk to it, and it keeps no process running.",
		),
		Commands: []string{"php-fpm" + v + " -t", "systemctl reload " + unit},
	}
	// Chỉ khai Files khi đã biết bản: preview của Plan cài stack chạy trước
	// DetectPHP, đọc "/etc/php/<bản>/..." thì vô nghĩa.
	if v, conf, _, _, err := s.prepare(); err == nil {
		info.Files = []engine.FileChange{
			{Path: phpPoolDir(v) + "/tpan-idle.conf", New: conf},
			{Path: phpPoolDir(v) + "/www.conf", Delete: true},
		}
	}
	return info
}

// Check: tpan-idle.conf đúng nội dung, quyền, chủ; www.conf không có. Chỉ đọc
// file, không chạy lệnh nào.
func (s *PHPIdlePool) Check(_ context.Context, sys system.System) (bool, error) {
	v, conf, _, _, err := s.prepare()
	if err != nil {
		return false, err
	}
	idle, www := phpPoolDir(v)+"/tpan-idle.conf", phpPoolDir(v)+"/www.conf"
	fi, err := sys.Stat(idle)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", idle, err)
	}
	if !fi.Mode.IsRegular() || fi.Mode.Perm() != phpIdleMode || normOwner(fi.Owner) != system.Root {
		return false, nil
	}
	data, err := sys.ReadFile(idle)
	if err != nil {
		return false, fmt.Errorf("đọc %s: %w", idle, err)
	}
	if string(data) != string(conf) {
		return false, nil
	}
	_, err = sys.Stat(www)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", www, err)
	}
	return false, nil
}

func (s *PHPIdlePool) Apply(ctx context.Context, sys system.System) error {
	v, conf, test, reload, err := s.prepare()
	if err != nil {
		return err
	}
	idle, www := phpPoolDir(v)+"/tpan-idle.conf", phpPoolDir(v)+"/www.conf"
	prevIdle, err := capture(sys, idle)
	if err != nil {
		return fmt.Errorf("ghi pool tpan-idle: %w", err)
	}
	// capture từ chối thư mục, nên Remove dưới đây chỉ gỡ file hoặc symlink.
	prevWWW, err := capture(sys, www)
	if err != nil {
		return fmt.Errorf("gỡ pool www: %w", err)
	}
	restore := func(err error) error {
		// www.conf trả trước: lỡ dừng giữa chừng thì pool.d vẫn có một pool.
		return errors.Join(err, prevWWW.restore(sys), prevIdle.restore(sys))
	}

	if err := sys.WriteFileAtomic(idle, conf, phpIdleMode, system.Root); err != nil {
		return restore(fmt.Errorf("ghi %s: %w", idle, err))
	}
	if prevWWW.exists {
		if err := sys.Remove(www); err != nil {
			return restore(fmt.Errorf("gỡ %s: %w", www, err))
		}
	}
	what := "thay pool www của PHP " + v + " bằng tpan-idle"
	if err := runTest(ctx, sys, test, what); err != nil {
		// Dịch vụ chưa nạp gì: chỉ cần trả file.
		return restore(err)
	}
	if err := phpReloadIfActive(ctx, sys, v, reload, what); err != nil {
		// Reload hỏng: trả file rồi reload lần nữa để PHP-FPM chạy lại bằng
		// config cũ.
		err = restore(err)
		return errors.Join(err, phpReloadIfActive(ctx, sys, v, reload, "trả lại pool www của PHP "+v))
	}
	s.prevIdle, s.prevWWW, s.applied = prevIdle, prevWWW, true
	return nil
}

// Undo ghi lại www.conf, gỡ tpan-idle.conf (hoặc trả bản cũ), kiểm, reload.
func (s *PHPIdlePool) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("pool tpan-idle: %w", errNotApplied)
	}
	v, _, test, reload, err := s.prepare()
	if err != nil {
		return err
	}
	if err := errors.Join(s.prevWWW.restore(sys), s.prevIdle.restore(sys)); err != nil {
		return err
	}
	what := "trả lại pool www của PHP " + v
	if err := runTest(ctx, sys, test, what); err != nil {
		return err
	}
	return phpReloadIfActive(ctx, sys, v, reload, what)
}

// phpReloadIfActive chỉ reload khi php<v>-fpm đang chạy: reload một dịch vụ
// đang tắt thì systemctl lỗi, mà bước này không có việc bật dịch vụ.
// is-active trả mã khác 0 cho "không chạy" (xem Service.state).
func phpReloadIfActive(ctx context.Context, sys system.System, v string, reload *system.Cmd, what string) error {
	unit := "php" + v + "-fpm"
	if _, err := sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"is-active", unit}}); err != nil {
		if _, ok := isExit(err); ok {
			return nil
		}
		return fmt.Errorf("hỏi systemd trạng thái %s: %w", unit, err)
	}
	return runReload(ctx, sys, reload, what)
}
