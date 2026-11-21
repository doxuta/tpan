package checks

// Phép kiểm của lồng theo site (M17, mục 3.7 của docs/thay-doi/M17-thiet-ke.md).
// Hợp đồng đợt 0: chữ ký, ID và tài liệu; thân do thợ đợt 2 viết. Mỗi phép
// trả Unknown "chưa viết" cho tới lúc đó.

import (
	"context"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

func cageTODO(id string, stage Stage) Check {
	b := base{id: id, stage: stage, title: i18n.T("Phép kiểm của lồng (chưa viết)", "Cage check (not written yet)")}
	return checkFunc(func(context.Context, system.System) Result {
		return b.unknown(i18n.T("chưa viết (M17 đợt 2)", "not written yet (M17 wave 2)"))
	})
}

// CageSocket: "cage.socket:<domain>", chặng PHP. File l.CageSocket() là
// socket www-data:www-data 0660; unit socket active (`systemctl is-active`);
// dịch vụ không failed. Sai một điều: Fail (sửa tự động cage.start).
func CageSocket(l site.Layout) Check { return cageTODO("cage.socket:"+l.Domain.String(), StagePHP) }

// CageRuntime: "cage.runtime:<domain>", chặng PHP (mục 3.7). Cho cả máy: không
// có tiến trình nào cùng uid với site ở ngoài slice của site (đọc /proc như
// steps.CageReap), có là Fail. Khi dịch vụ đang chạy (MainPID khác 0; không
// chạy thì phần này Pass, "lồng đang ngủ"), đọc tiến trình chính:
// /proc/<pid>/status có "NoNewPrivs:\t1" và CapEff bằng 0; /proc/<pid>/cgroup
// nằm dưới tpan-<slug>.slice; /proc/<pid>/mountinfo có tmpfs ở /var, /run,
// /etc/tpan, có dòng gắn /run/mysqld (chỉ đòi khi `systemctl is-active mariadb`
// là active) và dòng gắn /run/systemd/resolve không mang "//deleted". Sai một
// điều: Fail. /proc/<pid>/exe (Readlink) kết thúc bằng " (deleted)", hoặc
// `systemctl show --timestamp=unix -p ActiveEnterTimestamp` sớm hơn
// `stat -c %Y <l.CageFence()>`: Warn. Sửa tự động: cage.restart.
func CageRuntime(l site.Layout) Check { return cageTODO("cage.runtime:"+l.Domain.String(), StagePHP) }

// CageFence: "cage.fence:<domain>", chặng PHP. Bảy lệnh argv chạy với
// AsUser = l.User (đi qua lớp bọc của lồng nên chạy trong hàng rào), mỗi lệnh
// một dòng bằng chứng (mục 2.3): `id -un` ra l.User; `ls -A /var/www` ra đúng
// l.Domain; `ls -A /tmp` rỗng; `ls -A /run` ra "mysqld" và "systemd";
// `find /proc -maxdepth 1 -type d -regex /proc/[0-9]+ ! -user <l.User>` rỗng;
// `getent passwd` đúng 4 dòng (root, www-data, l.User, nobody); `test -w /etc`
// mã 1. Một lệnh sai: Fail. Người gọi bọc Heavy (chạy mỗi giờ).
func CageFence(l site.Layout) Check { return cageTODO("cage.fence:"+l.Domain.String(), StagePHP) }

// CageLimits: "cage.limits:<domain>", chặng PHP. Đọc cgroup của slice (cùng
// cách đọc với cage.ReadUsage) và so memory.max, memory.swap.max, cpu.max,
// pids.max, io.max với want. Thư mục cgroup chưa có: Pass kèm câu "lồng chưa
// chạy từ khi máy khởi động; trần sẽ áp khi lồng chạy". Lệch: Fail, nêu từng
// trường (mong đợi, đang áp).
func CageLimits(l site.Layout, want account.Limits) Check {
	return cageTODO("cage.limits:"+l.Domain.String(), StagePHP)
}

// CageState: "cage.state:<domain>", chặng PHP. caged là sites.caged, want là
// cờ lồng của gói. (a) want khác caged: Warn. (b) caged mà thiếu file hàng rào
// (l.CageFence()) hoặc còn file pool chung (/etc/php/*/fpm/pool.d/<pool>.conf,
// đọc bằng ReadDir từng bản); hoặc không caged mà còn file hàng rào: Fail.
// Core sinh phép này cho site có caged, want, hoặc có file hàng rào.
func CageState(l site.Layout, caged, want bool) Check {
	return cageTODO("cage.state:"+l.Domain.String(), StagePHP)
}

// ProbeLeftover: "probe.leftover:tpan", chặng TPAN. left là mô tả các lượt thử
// tấn công chưa dọn (sổ probe_runs, Cleaned = false, đã bắt đầu quá 2 giờ
// hoặc không có job nào đang chạy: core quyết). Rỗng: Pass. Có: Fail.
func ProbeLeftover(left []string) Check { return cageTODO("probe.leftover:tpan", StageTPAN) }

// SiteChecksCaged là SiteChecks của site trong lồng: như SiteChecks(web, php,
// l) nhưng thay PoolSocket bằng CageSocket(l), CageRuntime(l),
// Heavy(CageFence(l)), CageLimits(l, want). SiteChecksSSLCaged tương tự với
// SiteChecksSSL.
func SiteChecksCaged(web string, l site.Layout, want account.Limits) []Check {
	return []Check{cageTODO("cage.socket:"+l.Domain.String(), StagePHP)}
}

func SiteChecksSSLCaged(web string, l site.Layout, want account.Limits) []Check {
	return []Check{cageTODO("cage.socket:"+l.Domain.String(), StagePHP)}
}
