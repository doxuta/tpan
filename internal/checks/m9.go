package checks

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// Phép kiểm của M9 (mục 6.7 "Tự kiểm TPAN", 6.5 của docs/KE-HOACH.md). Hợp đồng
// ở m9_types.go.

// tpanBackupName: tên file bản sao tpan.db do Backups.SnapshotTPANDB ghi, giờ UTC.
var tpanBackupName = regexp.MustCompile(`^tpan-(\d{8}-\d{6})\.db$`)

// tpanBackupTimeLayout là phần thời gian trong tên file ở trên.
const tpanBackupTimeLayout = "20060102-150405"

// TPANBackupAge (ID "tpan.backup:db", StageTPAN) kiểm tpan.db có bản sao đủ mới.
//
// Vì sao: tpan.db giữ lịch sử job, danh sách site, bản ghi backup (đường dẫn và
// sha256 của từng file) và cài đặt. Mất nó thì máy vẫn chạy nhưng tpan quên mọi
// bản backup đang có. Bản sao do `tpan backup create --all` và lịch backup hằng
// ngày tạo ra; phép kiểm này bắt trường hợp lịch chưa bật hoặc đã ngừng chạy.
//
// Tuổi lấy từ giờ trong TÊN file, không từ mtime: system.FileInfo không có
// mtime, và tên do chính tpan đặt nên đáng tin hơn mtime (cp -a, rsync có thể
// đổi mtime). Tên không đúng dạng bị bỏ qua (file lạ trong thư mục không làm
// phép kiểm sai). Thư mục chưa có hoặc chưa có bản nào là Warn: máy mới cài
// chưa bao giờ sao lưu là tình trạng có thật. Lỗi đọc khác (hỏng đĩa, thiếu
// quyền) là Unknown vì chưa biết có bản sao hay không. Giờ của bản sao nằm
// sau now (đồng hồ máy bị lùi) thì tính là mới, không báo cũ vì tuổi âm.
// dir rỗng thì dùng site.TPANDBBackupDir; now nil thì dùng time.Now.
func TPANBackupAge(dir string, maxAge time.Duration, now func() time.Time) Check {
	if dir == "" {
		dir = site.TPANDBBackupDir
	}
	if now == nil {
		now = time.Now
	}
	b := base{id: "tpan.backup:db", stage: StageTPAN,
		title: i18n.T("tpan.db có bản sao lưu gần đây", "tpan.db has a recent backup")}
	ls := line("ls", dir)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: ls + " (tpan)"}
		names, err := sys.ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+dir+" ("+sslClean(err.Error())+")", "could not read "+dir+" ("+sslClean(err.Error())+")"), ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = 1, "No such file or directory"
		}

		var newest string
		var newestAt time.Time
		for _, n := range names {
			m := tpanBackupName.FindStringSubmatch(n)
			if m == nil {
				continue
			}
			at, perr := time.Parse(tpanBackupTimeLayout, m[1]) // không có múi giờ trong layout: UTC
			if perr != nil {
				continue // ví dụ tháng 13: tên giống bản sao nhưng không phải do tpan ghi
			}
			if newest == "" || at.After(newestAt) {
				newest, newestAt = n, at
			}
		}

		fix := []string{
			line("sudo", "tpan", "backup", "create", "--all", "--yes"),
			line("sudo", "tpan", "backup", "settings", "--at", "03:30"),
			ls,
		}
		if newest == "" {
			ev.Output = clip(strings.Join(names, "\n"))
			return b.bad(Warn,
				i18n.T("chưa có bản sao lưu tpan.db nào", "there is no backup of tpan.db yet"),
				i18n.T("Chưa ai chép tpan.db. Nếu file này hỏng hoặc mất, tpan quên mọi site, lịch sử và bản backup đang có. Tạo một bản ngay và bật lịch hằng ngày để lần sau không phải nhớ.",
					"Nobody has copied tpan.db yet. If this file is damaged or lost, tpan forgets every site, the history and the backups it has. Make one now and turn on the daily schedule so nobody has to remember."),
				fix, ev)
		}
		ev.Output = "newest: " + newest
		age := now().Sub(newestAt)
		if age > maxAge {
			hours := int(age / time.Hour)
			return b.bad(Warn,
				i18n.T(fmt.Sprintf("bản sao lưu tpan.db mới nhất đã %d giờ tuổi", hours), fmt.Sprintf("the newest tpan.db backup is %d hours old", hours)),
				i18n.T(fmt.Sprintf("Bản sao gần nhất (%s) cũ hơn %d giờ, nên lịch backup có thể chưa bật hoặc đã ngừng chạy. Mất tpan.db lúc này là mất mọi thay đổi từ bản đó.", newest, int(maxAge/time.Hour)),
					fmt.Sprintf("The latest copy (%s) is older than %d hours, so the backup schedule may be off or may have stopped. Losing tpan.db now loses every change since that copy.", newest, int(maxAge/time.Hour))),
				fix, ev)
		}
		return b.pass(i18n.T("Bản sao lưu tpan.db mới nhất: "+newest, "The newest tpan.db backup: "+newest), ev)
	})
}
