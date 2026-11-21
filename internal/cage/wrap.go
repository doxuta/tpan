package cage

// Hợp đồng M17 đợt 0: chữ ký và tài liệu cố định; thân hàm do thợ đợt 1 viết.
// Đặc tả đầy đủ ở mục 2.2 và 3.4 của docs/thay-doi/M17-thiet-ke.md.

import (
	"context"
	"errors"

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
	return system.Result{}, errors.New("cage.Sys.Run: chưa viết (đợt 1)")
}

// Describe trả dòng lệnh sẽ thật sự chạy cho cmd, để engine và phép kiểm ghi
// bằng chứng đúng (system.Describer). Lệnh vào lồng: dòng `systemd-run ...` như
// bước 3 nhưng tên unit là <tiền tố>xem (không sinh số ngẫu nhiên, không chạm
// máy ngoài một lần Stat). Lệnh không vào lồng: cmd.String().
func (s *Sys) Describe(cmd system.Cmd) string { return cmd.String() }

var _ system.System = (*Sys)(nil)
var _ system.Describer = (*Sys)(nil)
