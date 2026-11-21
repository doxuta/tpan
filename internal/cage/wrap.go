package cage

// Đặc tả đầy đủ ở mục 2.2 và 3.4 của docs/thay-doi/M17-thiet-ke.md.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// Sys là System đã bọc. Mọi hàm không phải Run đi thẳng xuống System bên dưới.
type Sys struct {
	system.System
	// Rand sinh 16 ký tự hex của tên unit tạm; nil là crypto/rand. Test đặt
	// để có tên cố định.
	Rand func() (string, error)
}

// Wrap bọc sys. Gọi một lần ở cmd/tpan/main.go quanh system.NewLocal(), nên
// CLI, `tpan serve`, cron và MCP cùng đi qua.
func Wrap(sys system.System) *Sys { return &Sys{System: sys} }

const (
	// noDeadlineSec là TimeoutStartSec khi ctx không có hạn: unit tạm không
	// chết theo tpan (khác runuser), nên luôn phải có trần.
	noDeadlineSec = 3600
	// stopWait là thời gian cho `systemctl stop` khi ctx của lệnh đã bị huỷ.
	stopWait = 10 * time.Second
	// describeTail là đuôi tên unit trong dòng bằng chứng (không sinh số ngẫu nhiên).
	describeTail = "xem"
)

// Run chạy cmd. Quy tắc (mục 3.4):
//
//  1. cmd.AsUser rỗng, hoặc AsUser không có dạng tp_<slug>
//     (site.CageSlugOfUser): chuyển thẳng xuống System bên dưới. AsUser kèm
//     Env (Env chỉ dành cho `tpan brain`): trả lỗi, không chạy gì.
//  2. Stat(site.CageFenceOfSlug(slug)): không có (fs.ErrNotExist) thì chuyển
//     thẳng xuống dưới; lỗi khác thì trả lỗi, KHÔNG chạy gì. Không có ngoại lệ
//     nào chạy lệnh của site trong lồng ở ngoài hàng rào (mục 3.4: một tiến
//     trình cùng uid ở ngoài lồng mở cửa /proc/<pid>/root, t14 P4).
//  3. Có hàng rào: chạy, dưới dạng argv, đúng thứ tự tham số này:
//     systemd-run --quiet --wait --pipe --expand-environment=no
//     --service-type=oneshot --unit=<site.CageRunPrefixOfSlug(slug)><16 hex>
//     -p User=<user> -p Group=<user> -p NoNewPrivileges=yes
//     -p TimeoutStartSec=<giây> -E LC_ALL=C [--working-directory=<cmd.Dir>]
//     -- <cmd.Name> <cmd.Args...>
//     Cmd mới không có AsUser; Stdin, OnOutput, Env (không dùng trên máy chủ)
//     giữ nguyên. TimeoutStartSec = thời gian còn lại của ctx (làm tròn lên
//     giây) cộng 1; ctx không có hạn thì 3600.
//  4. Mã thoát khác 0: `systemctl show <unit> -p LoadState -p Result -p
//     ExecMainStatus -p ExecMainStartTimestamp` rồi `systemctl reset-failed
//     <unit>` (bỏ qua lỗi của lệnh này). LoadState=not-found, hoặc mã trong dải
//     200..243 mà ExecMainStartTimestamp rỗng (systemd không dựng được môi
//     trường: 226/NAMESPACE, 217/USER, 203/EXEC): lỗi thường "không chạy được
//     lệnh" kèm stderr (không phải ExitError). Result=exit-code còn lại: trả
//     *system.ExitError với mã thoát thật. Mọi Result khác (oom-kill, timeout,
//     signal, core-dump, start-limit-hit, resources...): trả *KilledError.
//  5. ctx bị huỷ giữa chừng: `systemctl stop <unit>` bằng ctx mới 10 giây.
//
// Hai lệnh systemctl ở bước 4, 5 chạy bằng root, không qua hàng rào.
func (s *Sys) Run(ctx context.Context, cmd system.Cmd) (system.Result, error) {
	if cmd.AsUser != "" && len(cmd.Env) > 0 {
		// Env là đường của `tpan brain` (HOME, PATH của người vận hành); đi cùng
		// user của site thì chắc chắn là lỗi lập trình, không đoán ý.
		return system.Result{ExitCode: -1}, fmt.Errorf("chạy %s: không nhận Env khi chạy dưới user khác", cmd.String())
	}
	slug, caged, err := s.cagedSlug(cmd)
	if err != nil {
		return system.Result{ExitCode: -1}, err
	}
	if !caged {
		return s.System.Run(ctx, cmd)
	}

	tail, err := s.randHex()
	if err != nil {
		return system.Result{ExitCode: -1}, fmt.Errorf("sinh tên unit tạm cho %s: %w", cmd.String(), err)
	}
	unit := site.CageRunPrefixOfSlug(slug) + tail + ".service"
	inner := fenced(cmd, slug, tail, timeoutSec(ctx))

	res, err := s.System.Run(ctx, inner)
	if err == nil {
		return res, nil
	}
	// Kiểm ctx trước như LocalSystem: lệnh bị giết vì ctx cũng ra mã khác 0.
	// Unit tạm không chết theo systemd-run, nên phải tự dừng nó.
	if ctx.Err() != nil {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopWait)
		defer cancel()
		_, _ = s.System.Run(sctx, systemctl("stop", unit))
		_, _ = s.System.Run(sctx, systemctl("reset-failed", unit))
		// Bọc lỗi của ctx, không bọc err: err có thể là *ExitError của
		// systemd-run bị giết, và nơi gọi không được đọc nó thành mã thoát.
		return res, fmt.Errorf("chạy %s (unit %s, đã dừng): %w (%v)", cmd.String(), unit, ctx.Err(), err)
	}
	var ee *system.ExitError
	if !errors.As(err, &ee) {
		return res, err
	}
	return res, s.classify(ctx, inner.String(), unit, ee)
}

// classify hỏi systemd vì sao unit tạm ra mã khác 0 (bước 4). Mã thoát của
// systemd-run một mình không phân biệt được "lệnh tự thoát 1" với "bị giết
// vì chạm trần RAM" (đều là 1, t9).
func (s *Sys) classify(ctx context.Context, line, unit string, ee *system.ExitError) error {
	// ctx tách khỏi huỷ: ctx vừa hết hạn đúng lúc này không được làm mất bước
	// hỏi và bước dọn unit hỏng.
	qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopWait)
	defer cancel()
	show, showErr := s.System.Run(qctx, system.Cmd{Name: "systemctl", Args: []string{
		"show", unit, "-p", "LoadState", "-p", "Result", "-p", "ExecMainStatus", "-p", "ExecMainStartTimestamp",
	}})
	// Không có --collect nên unit hỏng còn nằm lại; dọn ngay, lỗi không quan trọng.
	_, _ = s.System.Run(qctx, systemctl("reset-failed", unit))
	if showErr != nil {
		// Không biết lệnh tự thoát hay bị giết: không trả ExitError, để nơi gọi
		// không đọc nhầm thành một mã thoát có nghĩa. %v chứ không %w: lỗi của
		// systemctl show cũng là *ExitError, bọc nó là lộ mã của lệnh khác.
		return fmt.Errorf("%s: mã %d, không hỏi được systemd lý do (unit %s): %v", line, ee.Code, unit, showErr)
	}
	p := props(show.Stdout)
	notStarted := ee.Code >= 200 && ee.Code <= 243 && p["ExecMainStartTimestamp"] == ""
	switch {
	case p["LoadState"] == "not-found" || notStarted:
		return fmt.Errorf("không chạy được lệnh %s: systemd không dựng được unit %s (mã %d): %s",
			line, unit, ee.Code, strings.TrimSpace(ee.Stderr))
	case p["Result"] == "exit-code":
		return ee
	case p["Result"] == "" || p["Result"] == "success":
		// Mã khác 0 mà systemd báo thành công hay không báo gì: không có ý
		// nghĩa nào an toàn để đọc.
		return fmt.Errorf("%s: mã %d nhưng systemd báo Result=%q (unit %s): %s",
			line, ee.Code, p["Result"], unit, strings.TrimSpace(ee.Stderr))
	}
	status, _ := strconv.Atoi(p["ExecMainStatus"])
	return &KilledError{Cmd: line, Unit: unit, Result: p["Result"], Status: status, Stderr: ee.Stderr}
}

// Describe trả dòng lệnh sẽ thật sự chạy cho cmd, để engine và phép kiểm ghi
// bằng chứng đúng (system.Describer). Lệnh vào lồng: dòng `systemd-run ...` như
// bước 3 nhưng tên unit là <tiền tố>xem (không sinh số ngẫu nhiên, không chạm
// máy ngoài một lần Stat). Lệnh không vào lồng: cmd.String().
//
// TimeoutStartSec trong dòng này luôn là 3600 (Describe không có ctx); lệnh
// thật chạy theo hạn của ctx.
func (s *Sys) Describe(cmd system.Cmd) string {
	slug, ok := site.CageSlugOfUser(cmd.AsUser)
	if !ok {
		return cmd.String()
	}
	// Chỉ "chắc chắn không có hàng rào" mới in runuser. Stat lỗi khác thì Run
	// từ chối; in dạng systemd-run để người làm theo dòng "sửa tay" không tự
	// chạy lệnh của site ở ngoài lồng.
	if _, err := s.Stat(site.CageFenceOfSlug(slug)); errors.Is(err, fs.ErrNotExist) {
		return cmd.String()
	}
	return fenced(cmd, slug, describeTail, noDeadlineSec).String()
}

// cagedSlug trả slug khi cmd phải chạy trong lồng (bước 1, 2).
func (s *Sys) cagedSlug(cmd system.Cmd) (string, bool, error) {
	slug, ok := site.CageSlugOfUser(cmd.AsUser)
	if !ok {
		return "", false, nil
	}
	fence := site.CageFenceOfSlug(slug)
	_, err := s.Stat(fence)
	switch {
	case err == nil:
		return slug, true, nil
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	}
	return "", false, fmt.Errorf("chạy %s: không biết site có lồng hay không (đọc %s): %w", cmd.String(), fence, err)
}

func (s *Sys) randHex() (string, error) {
	if s.Rand != nil {
		return s.Rand()
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// fenced dựng lệnh systemd-run của bước 3. User= và Group= luôn có trên dòng
// lệnh: drop-in biến mất đúng lúc này thì lệnh vẫn chạy bằng user của site,
// không bao giờ bằng root (mục 2.2).
func fenced(cmd system.Cmd, slug, tail string, secs int) system.Cmd {
	args := []string{
		"--quiet", "--wait", "--pipe", "--expand-environment=no",
		"--service-type=oneshot", "--unit=" + site.CageRunPrefixOfSlug(slug) + tail,
		"-p", "User=" + cmd.AsUser, "-p", "Group=" + cmd.AsUser, "-p", "NoNewPrivileges=yes",
		"-p", "TimeoutStartSec=" + strconv.Itoa(secs), "-E", "LC_ALL=C",
	}
	if cmd.Dir != "" {
		args = append(args, "--working-directory="+cmd.Dir)
	}
	args = append(append(args, "--", cmd.Name), cmd.Args...)
	// Dir để trống: systemd-run chạy bằng root, không chdir vào thư mục do site
	// kiểm soát; thư mục làm việc của lệnh đã nằm ở --working-directory.
	return system.Cmd{Name: "systemd-run", Args: args, Stdin: cmd.Stdin, OnOutput: cmd.OnOutput, Env: cmd.Env}
}

// timeoutSec là TimeoutStartSec cho ctx: phần còn lại làm tròn lên giây, cộng 1.
// Không dưới 1: với systemd, 0 nghĩa là không có hạn.
func timeoutSec(ctx context.Context) int {
	dl, ok := ctx.Deadline()
	if !ok {
		return noDeadlineSec
	}
	left := (time.Until(dl) + time.Second - 1) / time.Second
	return int(max(left, 0)) + 1
}

func systemctl(verb, unit string) system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{verb, unit}}
}

// props đọc output "Khoá=Giá trị" của `systemctl show`.
func props(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			m[k] = v
		}
	}
	return m
}

var _ system.System = (*Sys)(nil)
var _ system.Describer = (*Sys)(nil)
