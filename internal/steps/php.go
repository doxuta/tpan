package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// phpRoot là nơi Debian và Ubuntu đặt cấu hình PHP, mỗi bản một thư mục con
// (/etc/php/8.3). Bản có PHP-FPM thì có thêm thư mục fpm/ bên trong.
const phpRoot = "/etc/php"

var phpVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// DetectPHP dò bản PHP-FPM đã cài (mục 6.1, bước 4) và ghi vào *Result.
//
// Plan được dựng trước khi apt cài php-fpm nên lúc đó chưa biết bản PHP. Các
// bước và phép kiểm phía sau giữ cùng con trỏ Result và đọc nó lúc chạy.
// Check cũng ghi *Result khi tìm thấy: chạy lại Plan trên máy đã cài thì
// Apply bị bỏ qua, mà các bước sau vẫn cần biết bản PHP.
type DetectPHP struct {
	Result *string
}

func (s *DetectPHP) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "php.detect:" + phpRoot,
		Title: i18n.T("Dò bản PHP-FPM đã cài", "Detect the installed PHP-FPM version"),
		Why: i18n.T(
			"Tên dịch vụ, socket và thư mục pool của PHP-FPM đều chứa số phiên bản (php8.3-fpm, /etc/php/8.3/fpm/pool.d). Không biết bản đang cài thì tpan ghi pool vào sai chỗ và site trả lỗi 502.",
			"The PHP-FPM service name, socket and pool directory all contain the version number (php8.3-fpm, /etc/php/8.3/fpm/pool.d). Without the installed version tpan writes pools to the wrong place and sites return 502.",
		),
		NoUndo: i18n.T(
			"Bước này chỉ đọc máy, không thay đổi gì nên không có gì để hoàn tác.",
			"This step only reads the machine and changes nothing, so there is nothing to undo.",
		),
	}
}

func (s *DetectPHP) Check(_ context.Context, sys system.System) (bool, error) {
	if s.Result == nil {
		return false, errors.New("dò bản PHP: Result là nil, không có chỗ ghi kết quả")
	}
	v, err := detectPHP(sys)
	if err != nil || v == "" {
		return false, err
	}
	*s.Result = v
	return true, nil
}

func (s *DetectPHP) Apply(ctx context.Context, sys system.System) error {
	done, err := s.Check(ctx, sys)
	if err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("không tìm thấy PHP-FPM: không có %s/<bản>/fpm; gói php-fpm đã cài chưa?", phpRoot)
	}
	return nil
}

func (s *DetectPHP) Undo(context.Context, system.System) error { return nil }

// detectPHP trả bản cao nhất có thư mục fpm, hoặc "" khi chưa có bản nào.
// So theo số từng phần (8.10 > 8.9), không theo chữ.
func detectPHP(sys system.System) (string, error) {
	names, err := sys.ReadDir(phpRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("đọc %s: %w", phpRoot, err)
	}
	best := ""
	for _, n := range names {
		if !phpVersion.MatchString(n) {
			continue
		}
		fi, err := sys.Stat(phpRoot + "/" + n + "/fpm")
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			continue // có php-cli mà không có php-fpm, hoặc là file lạ
		}
		if err != nil {
			return "", fmt.Errorf("kiểm %s/%s/fpm: %w", phpRoot, n, err)
		}
		if fi.Mode.IsDir() && (best == "" || versionLess(best, n)) {
			best = n
		}
	}
	return best, nil
}

// versionLess so hai bản dạng "8.3". Đầu vào đã khớp phpVersion nên Atoi
// không lỗi, trừ khi số quá lớn; khi đó coi là 0, chỉ ảnh hưởng thứ tự.
func versionLess(a, b string) bool {
	am, an, _ := strings.Cut(a, ".")
	bm, bn, _ := strings.Cut(b, ".")
	x, _ := strconv.Atoi(am)
	y, _ := strconv.Atoi(bm)
	if x != y {
		return x < y
	}
	x, _ = strconv.Atoi(an)
	y, _ = strconv.Atoi(bn)
	return x < y
}
