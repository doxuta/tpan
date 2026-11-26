package steps

// v0.2.16: reload PHP-FPM chỉ trả về khi PHP-FPM đã đọc xong cấu hình mới.
//
// Lỗi gốc (cổng kiểm v0.2.15, t18m, và tái hiện riêng bằng
// tmp-phien/fpm-dua.sh: 2/20 vòng): `systemctl reload php8.3-fpm` chỉ gửi
// SIGUSR2 rồi trả về sau khoảng 3 ms. Master PHP-FPM cho tiến trình con thôi
// việc, execvp chính nó, rồi mới liệt kê `include=pool.d/*.conf` và mở từng
// file, khoảng 20 đến 30 ms sau. Khoá một khách có hai site là hai bước đổi
// tên pool liền nhau, mỗi bước reload một lần: bước thứ hai đổi tên
// tpan-<site 2>.conf đúng lúc master đã liệt kê thư mục nhưng chưa mở file đó.
// Master báo "failed to open configuration file ... No such file or directory",
// "FPM initialization failed", thoát mã 78 (journal, 26/11/2026 16:36:56), và
// từ đó mọi site PHP của bản đó trả 502, mọi lần reload sau báo "not active,
// cannot reload".
//
// Sửa ở hai chỗ, cả hai nằm trong reloadFPM:
//
//  1. Chờ: chụp kích thước error_log của PHP-FPM trước lệnh reload, rồi đọc
//     phần ghi thêm tới khi thấy "Reloading in progress" rồi "ready to handle
//     connections" (master mới đã đọc xong mọi pool). Bước kế tiếp chỉ đụng
//     pool.d sau đó, nên không còn trúng lúc master đang đọc.
//  2. Cứu: master chết giữa lần reload (vì một thay đổi khác trong pool.d,
//     không phải của TPAN) thì `systemctl start` lại. Cấu hình trên đĩa đã qua
//     `php-fpm -t` ở bước gọi; master mới đọc nó từ đầu, nên kết quả giống một
//     lần reload thành công. Start cũng hỏng thì trả lỗi gộp.
//
// Không đọc được php-fpm.conf (máy giả trong test, --demo) thì chạy lệnh như
// cũ, không chờ. error_log không phải file (syslog) hoặc không đọc được: chờ
// cố định fpmSettle rồi chỉ kiểm dịch vụ còn chạy.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// Thời hạn chờ dòng "ready", nhịp đọc log, nhịp hỏi systemd và thời gian chờ
// cố định khi không có log. Biến để test rút ngắn.
var (
	fpmWaitMax  = 5 * time.Second
	fpmWaitTick = 20 * time.Millisecond
	fpmAskEvery = 5 // hỏi `systemctl is-active` mỗi 5 nhịp
	fpmSettle   = 500 * time.Millisecond
)

const (
	fpmLineStart = "Reloading in progress"
	fpmLineReady = "ready to handle connections"
)

// fpmReloadVersion nhận "systemctl reload php8.3-fpm" và trả "8.3".
func fpmReloadVersion(cmd system.Cmd) (string, bool) {
	if cmd.Name != "systemctl" || len(cmd.Args) != 2 || cmd.Args[0] != "reload" {
		return "", false
	}
	v, ok := strings.CutPrefix(cmd.Args[1], "php")
	if !ok {
		return "", false
	}
	v, ok = strings.CutSuffix(v, "-fpm")
	if !ok || !phpVersion.MatchString(v) {
		return "", false
	}
	return v, true
}

// fpmConfPath là file cấu hình chính của PHP-FPM bản v trên Ubuntu.
func fpmConfPath(v string) string { return "/etc/php/" + v + "/fpm/php-fpm.conf" }

// fpmLogOf đọc php-fpm.conf của bản v. ok=false: không đọc được file (không
// chờ gì cả). logPath rỗng: error_log không phải file tuyệt đối (syslog, đường
// dẫn tương đối theo prefix) thì dùng chờ cố định.
func fpmLogOf(sys system.System, v string) (logPath string, ok bool) {
	b, err := sys.ReadFile(fpmConfPath(v))
	if err != nil {
		return "", false
	}
	// Mặc định của gói Ubuntu khi dòng error_log bị comment.
	logPath = "/var/log/php" + v + "-fpm.log"
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && line != "[global]" {
			break // hết phần [global]
		}
		k, val, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(k) != "error_log" {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if !path.IsAbs(val) {
			return "", true
		}
		logPath = val
	}
	return logPath, true
}

// reloadFPM: xem đầu file.
func reloadFPM(ctx context.Context, sys system.System, cmd system.Cmd, v string) (system.Result, error) {
	logPath, ok := fpmLogOf(sys, v)
	if !ok {
		return sys.Run(ctx, cmd)
	}
	off := int64(-1)
	if logPath != "" {
		if fi, err := sys.Stat(logPath); err == nil {
			off = fi.Size
		}
	}
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		// Reload bị từ chối (dịch vụ đang tắt từ trước): giữ nguyên lỗi, bước
		// này không tự bật một dịch vụ người quản trị đã tắt.
		return res, err
	}
	if werr := waitFPMReload(ctx, sys, v, logPath, off); werr != nil {
		return res, werr
	}
	return res, nil
}

// waitFPMReload chờ master đọc xong cấu hình mới; master chết thì start lại.
func waitFPMReload(ctx context.Context, sys system.System, v, logPath string, off int64) error {
	unit := "php" + v + "-fpm"
	if off < 0 {
		if err := sleepCtx(ctx, fpmSettle); err != nil {
			return err
		}
		return fpmAliveOrStart(ctx, sys, v, "")
	}
	deadline := time.Now().Add(fpmWaitMax)
	for tick := 1; ; tick++ {
		if fpmLogState(sys, logPath, &off) == fpmReady {
			return nil
		}
		if tick%fpmAskEvery == 0 {
			if active, err := phpActive(ctx, sys, v); err == nil && !active {
				return fpmAliveOrStart(ctx, sys, v, "master thoát giữa lần nạp lại")
			}
		}
		if time.Now().After(deadline) {
			// Không thấy dòng "ready" (log_level cao hơn notice, log bị xoay
			// giữa chừng): dịch vụ còn chạy là đủ, không thì start lại.
			return fpmAliveOrStart(ctx, sys, v, fmt.Sprintf("không thấy %q trong %s sau %v", fpmLineReady, logPath, fpmWaitMax))
		}
		if err := sleepCtx(ctx, fpmWaitTick); err != nil {
			return fmt.Errorf("chờ %s nạp lại: %w", unit, err)
		}
	}
}

type fpmState int

const (
	fpmWaiting fpmState = iota
	fpmReady
)

// fpmLogState đọc phần log ghi thêm sau *off. Log bị xoay (file mới nhỏ hơn
// off) thì đọc từ đầu file mới. Thấy "Reloading in progress" rồi tới "ready
// to handle connections" sau nó là xong. Không cập nhật off: phải thấy cả hai
// dòng trong cùng một đoạn tính từ lúc trước reload.
func fpmLogState(sys system.System, logPath string, off *int64) fpmState {
	fi, err := sys.Stat(logPath)
	if err != nil {
		return fpmWaiting
	}
	if fi.Size < *off {
		*off = 0
	}
	if fi.Size == *off {
		return fpmWaiting
	}
	b, err := sys.ReadFile(logPath)
	if err != nil || int64(len(b)) < *off {
		return fpmWaiting
	}
	tail := string(b[*off:])
	i := strings.Index(tail, fpmLineStart)
	if i < 0 {
		return fpmWaiting
	}
	if strings.Contains(tail[i:], fpmLineReady) {
		return fpmReady
	}
	return fpmWaiting
}

// fpmAliveOrStart: dịch vụ còn chạy thì xong; đã chết thì `systemctl start`
// (đồng bộ: unit Type=notify, systemctl chờ master báo READY).
func fpmAliveOrStart(ctx context.Context, sys system.System, v, why string) error {
	active, err := phpActive(ctx, sys, v)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	unit := "php" + v + "-fpm"
	res, serr := sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"start", unit}})
	if serr != nil {
		base := fmt.Errorf("%s đã dừng sau khi nạp lại", unit)
		if why != "" {
			base = fmt.Errorf("%s đã dừng sau khi nạp lại (%s)", unit, why)
		}
		return withStdout(errors.Join(base, fmt.Errorf("bật lại %s: %w (xem: journalctl -u %s -n 30)", unit, serr, unit)), res)
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
