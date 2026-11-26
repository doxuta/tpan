package checks

// cage.pressure (v0.2.15): site trong lồng có chạm trần của gói gần đây không.
//
// Trước bản này, site chạm trần RAM hay trần số tiến trình thì panel chỉ tăng
// bộ đếm ở màn Tài nguyên: người quản trị không vào đúng màn đó thì không biết
// request của site đang bị dừng (thiếu sót ghi lại khi đẩy thật từng trần trên
// máy ảo, 26/11/2026). Phép kiểm này đưa chuyện đó vào health: có lần chạm
// trần trong khoảng vừa qua là Fail, tức là mở một sự cố ở mục Sự cố; hết
// khoảng đó mà không chạm nữa là Pass và sự cố tự đóng.
//
// Chỉ health dùng (core.Sites.Checks). Không nằm trong kiểm chứng sau thao tác:
// site vừa chạm trần không phải lý do để hoàn tác một lần đổi cấu hình.
//
// Chỉ tính hai trần mà chạm tới là có thứ bị dừng: RAM (kernel dừng tiến trình,
// request trả 502 hoặc 503) và số tiến trình (không tạo được tiến trình mới).
// Trần CPU và trần đọc ghi đĩa chỉ làm chậm, không làm hỏng request, nên chỉ
// hiện ở màn Tài nguyên.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// CageHits là số lần một site chạm trần trong Window gần đây. Gói checks không
// đọc tpan.db: core tính từ bảng usage_samples (mẫu 5 phút) cộng một lần đọc
// cgroup ngay lúc dựng phép kiểm, rồi đưa vào.
type CageHits struct {
	// Window là khoảng đã xét, tính tới lúc dựng phép kiểm.
	Window time.Duration
	// Pairs là số cặp mẫu liền nhau so được trong khoảng. 0: chưa đủ số liệu.
	Pairs int
	// NeverHit: chưa có cặp nào để so, nhưng hai bộ đếm của lồng đang là 0,
	// tức là lồng chưa chạm trần lần nào từ lúc chạy (site vừa vào lồng). Khi
	// đó phép kiểm đạt ngay thay vì "chưa kiểm được".
	NeverHit bool
	// OOMKills: số lần tiến trình bị dừng vì chạm trần RAM. MemMaxMB là trần lúc đó (MiB), 0 khi không rõ.
	OOMKills int64
	MemMaxMB int64
	// TasksHits: số lần chạm trần số tiến trình. TasksMax là trần lúc đó, 0 khi không rõ.
	TasksHits int64
	TasksMax  int64
}

// CagePressure trả phép kiểm cage.pressure của site trong lồng l với số liệu h.
func CagePressure(l site.Layout, h CageHits) Check {
	b := siteBase("cage.pressure", StagePHP, l,
		i18n.T("Site không chạm trần RAM hay trần số tiến trình của gói", "The site does not hit its package's RAM or process limit"))
	if g := cageGuard(b, l, false); g != nil {
		return g
	}
	dom := l.Domain.String()
	mins := strconv.Itoa(int(h.Window.Minutes()))
	return checkFunc(func(context.Context, system.System) Result {
		if h.Pairs == 0 && h.NeverHit {
			return b.pass(i18n.T(
				"Chưa chạm trần RAM hay trần số tiến trình lần nào từ lúc lồng chạy",
				"Has not hit its RAM or process limit since its cage started"))
		}
		if h.Pairs == 0 {
			return b.unknown(i18n.T(
				"chưa đủ số liệu của lồng trong "+mins+" phút qua để biết site có vừa chạm trần không (panel lấy mẫu 5 phút một lần; lồng đang không chạy hoặc panel vừa khởi động lại sau một lúc tắt)",
				"not enough cage readings in the last "+mins+" minutes to tell whether the site just hit a limit (the panel samples every 5 minutes; the cage is not running or the panel just restarted after being down)"))
		}
		if h.OOMKills == 0 && h.TasksHits == 0 {
			return b.pass(i18n.T(
				"Không chạm trần RAM hay trần số tiến trình trong "+mins+" phút qua",
				"Did not hit its RAM or process limit in the last "+mins+" minutes"))
		}
		var vi, en []string
		if n := h.OOMKills; n > 0 {
			capVi, capEn := "", ""
			if h.MemMaxMB > 0 {
				m := strconv.FormatInt(h.MemMaxMB, 10)
				capVi, capEn = " (trần "+m+" MB)", " (limit "+m+" MB)"
			}
			k := strconv.FormatInt(n, 10)
			vi = append(vi, k+" lần tiến trình PHP bị dừng vì hết RAM"+capVi)
			en = append(en, k+" time(s) a PHP process was stopped for running out of RAM"+capEn)
		}
		if n := h.TasksHits; n > 0 {
			capVi, capEn := "", ""
			if h.TasksMax > 0 {
				m := strconv.FormatInt(h.TasksMax, 10)
				capVi, capEn = " (trần "+m+" tiến trình)", " (limit "+m+" processes)"
			}
			k := strconv.FormatInt(n, 10)
			vi = append(vi, k+" lần không tạo được tiến trình mới"+capVi)
			en = append(en, k+" time(s) a new process could not be started"+capEn)
		}
		return b.bad(Fail,
			i18n.T("Site chạm trần của gói trong "+mins+" phút qua: "+strings.Join(vi, "; "),
				"The site hit its package limits in the last "+mins+" minutes: "+strings.Join(en, "; ")),
			i18n.T(
				"Lồng giữ site đúng trong trần của gói. Hết RAM thì kernel dừng tiến trình PHP đang chạy, request đó trả 502 hoặc 503; hết số tiến trình thì việc mới không khởi động được. Site đang quá tải, có mã chạy sai, hoặc gói nhỏ hơn nhu cầu thật của site. Các site khác trên máy không bị ảnh hưởng. Sự cố này tự đóng khi "+mins+" phút trôi qua mà không chạm trần nữa.",
				"The cage keeps the site within its package limits. Out of RAM, the kernel stops the running PHP process and that request returns 502 or 503; out of processes, new work cannot start. The site is overloaded, runs faulty code, or its package is smaller than what it really needs. Other sites on this machine are not affected. This incident closes by itself once "+mins+" minutes pass without another hit."),
			[]string{
				line("tpan", "usage", dom, "--suggest"),
				line("journalctl", "-k", "--since", "-1h", "--grep", "oom-kill|Memory cgroup out of memory"),
				line("tail", "-n", "30", l.PHPErrorLog),
				line("tpan", "health", dom),
			})
	})
}
