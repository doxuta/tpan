package checks

// Hợp đồng M4: các phép kiểm mới của health (mục 6.7 của docs/KE-HOACH.md):
// phát hiện lệch, RAM và swap, và tự kiểm chính tpan. Thân hàm do thợ của phần
// checks viết; đợt 0 chỉ có chữ ký và ID.
//
// Gói vẫn chỉ import system và i18n. Thứ cần DB (toàn vẹn, job treo, nhịp của
// bộ hẹn giờ) được đưa vào bằng hàm, gói health nối chúng với store.
//
// Mọi Result không đạt có Cause và ManualFix (dòng cuối là lệnh kiểm lại), như
// các phép kiểm của M1, M2.

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// notWritten: thân hàm chưa viết (đợt 0).
func notWritten(id string, stage Stage) Check {
	b := base{id: id, stage: stage, title: i18n.T("Chưa viết", "Not written")}
	return checkFunc(func(context.Context, system.System) Result {
		return b.unknown(i18n.T("phép kiểm chưa được viết", "this check is not written yet"))
	})
}

// Drift (ID "drift:<path>", chặng stage): so sha256 của file đang có với
// wantSHA256 (hex, giá trị ở bảng managed_files lúc tpan ghi). Khớp: Pass.
// Khác: Warn (không phải Fail: máy có thể vẫn chạy đúng, nhưng lần chạy lại
// Plan sẽ ghi đè bản sửa tay), Summary nêu đúng đường dẫn, Evidence có sha256
// hai bên. File không còn: Fail. Cách sửa tay: xem diff bằng `tpan health
// --fix --dry-run`, hoặc tự đưa file về; AutoFix do core điền (mục 6.7: ghi
// lại bản của tpan, hoặc nhận bản sửa tay làm chuẩn mới).
// Đọc file bằng sys.ReadFile, tính sha256 bằng Go (như WPCLI).
func Drift(path, wantSHA256 string, stage Stage) Check {
	return notWritten("drift:"+path, stage)
}

// DriftStage chọn chặng cho phép kiểm lệch theo đường dẫn, để lệch hiện đúng
// ga trên bản đồ tuyến: /etc/nginx/ và /etc/apache2/ là StageWeb; /etc/php/
// là StagePHP; dưới /var/www/ là StageWordPress; /etc/tpan/ và
// /etc/systemd/system/tpan* là StageTPAN; còn lại (logrotate, mariadb.conf.d)
// là StageMachine.
func DriftStage(path string) Stage {
	return StageMachine
}

// Memory (ID "memory:ram", StageMachine): đọc /proc/meminfo. RAM còn dùng
// được (MemAvailable) dưới 10% tổng: Warn; dưới 5%: Fail. Có swap và swap đã
// dùng trên 50%: Warn. Không có swap: Pass kèm ghi chú trong Summary (máy nhỏ
// không swap là chuyện thường; trang Bảo mật của M5 mới gợi ý thêm swap).
func Memory() Check {
	return notWritten("memory:ram", StageMachine)
}

// FreeSpace (ID "disk.free:<path>", chặng stage): chỗ trống tuyệt đối (như
// cột Avail của df, DiskUsage.Free) dưới minBytes là Fail. Khác Disk (phần
// trăm): dùng cho /var/lib/tpan, để kịch bản làm đầy đĩa tới 90% chỉ làm chặng
// "máy" lỗi, không kéo theo chặng "tpan" khi vẫn còn hàng GB trống.
func FreeSpace(path string, minBytes uint64, stage Stage) Check {
	return notWritten("disk.free:"+path, stage)
}

// PanelCert (ID "tpan.cert:<path>", StageTPAN): đọc chứng chỉ PEM của panel,
// parse bằng crypto/x509, tính số ngày còn lại so với now(). Dưới 14 ngày:
// Warn; hết hạn: Fail; không có file: Unknown (máy chưa chạy tpan setup, hoặc
// --demo). now nil thì dùng time.Now.
func PanelCert(path string, now func() time.Time) Check {
	return notWritten("tpan.cert:"+path, StageTPAN)
}

// FileMode (ID "tpan.perm:<path>", chặng stage): path tồn tại, không phải
// symlink, quyền đúng perm và chủ đúng owner (so như normOwner: rỗng là root).
// Sai: Fail, ManualFix là chmod/chown đúng giá trị rồi `ls -l`. Không có:
// Unknown. Dùng cho /var/lib/tpan/tpan.db (0600), /etc/tpan/tls/key.pem
// (0600), /etc/tpan (0700).
func FileMode(path string, perm uint32, owner system.Owner, stage Stage) Check {
	return notWritten("tpan.perm:"+path, stage)
}

// TPANDatabase (ID "tpan.db:integrity", StageTPAN): gọi integrity (PRAGMA
// integrity_check). "ok": Pass; chuỗi khác: Fail, Evidence là nguyên văn;
// integrity trả lỗi: Unknown.
func TPANDatabase(integrity func(ctx context.Context) (string, error)) Check {
	return notWritten("tpan.db:integrity", StageTPAN)
}

// StuckJob mô tả một job treo cho phép kiểm TPANJobs.
type StuckJob struct {
	ID      string
	Kind    string
	Target  string
	Started time.Time
}

// TPANJobs (ID "tpan.jobs:stuck", StageTPAN): stuck trả các job còn
// "running" quá lâu. Rỗng: Pass; có: Fail, Summary liệt kê id và loại job.
// stuck lỗi: Unknown.
func TPANJobs(stuck func(ctx context.Context) ([]StuckJob, error)) Check {
	return notWritten("tpan.jobs:stuck", StageTPAN)
}

// TPANScheduler (ID "tpan.scheduler:health", StageTPAN): lastBeat trả lần
// gần nhất bộ hẹn giờ health trong `tpan serve` chạy (ok=false: chưa bao giờ).
// Cách now() không quá maxAge: Pass; quá, hoặc chưa bao giờ: Warn ("tpan serve
// có đang chạy không?", ManualFix: systemctl status tpan). lastBeat lỗi:
// Unknown. now nil thì dùng time.Now.
func TPANScheduler(lastBeat func(ctx context.Context) (t time.Time, ok bool, err error), maxAge time.Duration, now func() time.Time) Check {
	return notWritten("tpan.scheduler:health", StageTPAN)
}
