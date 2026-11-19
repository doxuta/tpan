package checks

import (
	"context"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// stateRE giới hạn chữ trạng thái của systemctl ("active", "inactive", "failed",
// "activating"...) trước khi đưa vào Summary, để output lạ không chui vào câu chữ.
var stateRE = regexp.MustCompile(`^[a-z-]{1,24}$`)

// Service kiểm một dịch vụ systemd: đang chạy (`systemctl is-active`) và tự bật
// khi khởi động máy (`systemctl is-enabled`).
//
//   - Cả hai đạt: Pass.
//   - Đang chạy nhưng không bật lúc khởi động: Warn. Hôm nay chạy tốt, nhưng khởi
//     động lại máy là web sập, nên không được báo xanh.
//   - Không chạy: Fail.
//
// is-enabled coi là "bật" khi mã thoát 0. Mã 0 gồm cả enabled, static, alias,
// indirect, generated: những trạng thái systemd tự kéo dịch vụ lên, không cần
// người bật. Chỉ disabled, masked, không tìm thấy mới trả mã khác 0.
//
// Chạy cả hai lệnh dù lệnh đầu đã cho kết quả, để bằng chứng luôn đủ hai dòng.
func Service(unit string, stage Stage) Check {
	b := base{
		id:    "service.active:" + unit,
		stage: stage,
		title: i18n.T("Dịch vụ "+unit+" đang chạy và tự bật cùng máy", "Service "+unit+" is running and starts at boot"),
	}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		if !unitNameRE.MatchString(unit) {
			return b.unknown(i18n.T("tên dịch vụ không hợp lệ: "+unit, "invalid service name: "+unit))
		}
		resA, evA, err := execCmd(ctx, sys, system.Cmd{Name: "systemctl", Args: []string{"is-active", unit}})
		if err != nil {
			return b.cantRun(err, evA)
		}
		resE, evE, err := execCmd(ctx, sys, system.Cmd{Name: "systemctl", Args: []string{"is-enabled", unit}})
		if err != nil {
			return b.cantRun(err, evA, evE)
		}

		state := strings.TrimSpace(resA.Stdout)
		active := resA.ExitCode == 0 && state == "active"
		enabled := resE.ExitCode == 0
		recheck := line("systemctl", "is-active", unit)

		switch {
		case active && enabled:
			return b.pass(i18n.T(unit+" đang chạy và tự bật khi khởi động máy", unit+" is running and starts at boot"), evA, evE)
		case active:
			return b.bad(Warn,
				i18n.T(unit+" đang chạy nhưng không tự bật khi khởi động máy", unit+" is running but does not start at boot"),
				i18n.T("Dịch vụ chưa được bật lúc khởi động. Khởi động lại máy là "+unit+" không lên.",
					"The service is not enabled at boot. After a reboot "+unit+" will not come back."),
				[]string{line("systemctl", "enable", unit), line("systemctl", "is-enabled", unit)},
				evA, evE)
		default:
			if !stateRE.MatchString(state) {
				state = "?"
			}
			return b.bad(Fail,
				i18n.T(unit+" không chạy (trạng thái: "+state+")", unit+" is not running (state: "+state+")"),
				i18n.T("Dịch vụ đã dừng hoặc bị lỗi khi khởi động. Xem log của nó để biết vì sao.",
					"The service has stopped or failed to start. Read its log to see why."),
				[]string{
					line("systemctl", "status", unit),
					line("journalctl", "-u", unit, "-n", "50"),
					line("systemctl", "enable", "--now", unit),
					recheck,
				},
				evA, evE)
		}
	})
}
