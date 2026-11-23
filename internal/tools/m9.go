package tools

// Công cụ của M9 (mục 6.5, 8.1 của docs/KE-HOACH.md): xem danh sách backup và
// lập Plan backup, khôi phục. Lập Plan chỉ lưu một dòng pending như mọi
// propose_* (plan.go); không công cụ nào ở đây đổi gì trên máy.

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/validate"
)

// listBackupsIn: domain tuỳ chọn, nên KHÔNG có domainArg (newTool sẽ đòi
// domain hợp lệ kể cả khi trống). listBackups tự kiểm khi có.
type listBackupsIn struct {
	Domain string `json:"domain,omitempty" jsonschema:"Only backups of this site (domain as returned by list_sites). Empty means every backup, including those of sites already deleted for good."`
}

type backupRestoreIn struct {
	Domain   string `json:"domain" jsonschema:"Site domain as returned by list_sites (for example site-a.example)."`
	BackupID string `json:"backup_id" jsonschema:"Id of a full backup of that same site, as list_backups returned it."`
}

func (a backupRestoreIn) domainArg() string { return a.Domain }

// Năm giá trị của backupRow.Location (mục 3.16 của docs/thay-doi/M18-thiet-ke.md),
// dùng chung với API (core.BackupLocation).
const (
	locMachine     = core.LocationMachine
	locRemote      = core.LocationRemote
	locBoth        = core.LocationBoth
	locOtherTarget = core.LocationOtherTarget
	locPushFailed  = core.LocationPushFailed
)

// backupLocation cho biết bản b nằm ở đâu, tính từ ba trường của dòng và mã
// đích đang đặt. hasTarget=false (chưa đặt đích): dòng stored nào cũng là của
// một đích khác. Chỉ trả một nhãn: agent không cần khoá object, lỗi hay tên
// kho để chọn bản, và những thứ đó có thể chứa tên bucket hay đường dẫn của
// người dùng.
func backupLocation(b core.Backup, cur string, hasTarget bool) string {
	return core.BackupLocation(b, cur, hasTarget)
}

// backupRow là một dòng list_backups. Không có đường dẫn file và sha256: agent
// không cần chúng để chọn bản, và công cụ không đưa đường dẫn trên máy khi
// không cần (tail_log là ngoại lệ có lý do). Cũng không có remote_key,
// remote_error hay thông tin về đích: chỉ có Location.
type backupRow struct {
	ID         string            `json:"id"`
	Domain     string            `json:"domain"`
	Kind       core.BackupKind   `json:"kind"`
	Reason     core.BackupReason `json:"reason"`
	Created    time.Time         `json:"created"`
	Size       int64             `json:"size"`
	Location   string            `json:"location"`
	WPVersion  string            `json:"wp_version,omitempty"`
	PHPVersion string            `json:"php_version,omitempty"`
}

func listBackups(ctx context.Context, c *call, in listBackupsIn, _ validate.Domain) (any, error) {
	dom := ""
	if in.Domain != "" {
		d, err := validate.ParseDomain(in.Domain)
		if err != nil {
			return nil, err
		}
		dom = d.String()
	}
	rows, err := (&core.Backups{Sites: c.s.sites(), Store: c.s.st.d.Store}).List(ctx, dom)
	if err != nil {
		return nil, err
	}
	cur, hasTarget, err := core.CurrentTargetID(ctx, c.s.st.d.Store)
	if err != nil {
		return nil, err
	}
	// Bộ che chỉ Learn domain có trong bảng sites lúc mở phiên. Danh sách không
	// lọc có cả backup của site đã xoá hẳn (dòng sites không còn). Domain dạng
	// chữ thì lượt che theo mẫu đã bắt, nhưng Learn thêm ở đây để slug và dạng
	// có dấu của nó (nếu agent gặp ở công cụ khác) cũng thành bí danh, như
	// newTool làm với domain agent đưa vào.
	out := make([]backupRow, 0, len(rows))
	var learn []validate.Domain
	for _, r := range rows {
		d, err := validate.ParseDomain(r.Domain)
		if err != nil {
			// Dòng hỏng (sửa tay): bỏ, vì không che được tên không hợp lệ.
			continue
		}
		learn = append(learn, d)
		out = append(out, backupRow{
			ID: r.ID, Domain: d.String(), Kind: r.Kind, Reason: r.Reason, Created: r.Created,
			Size: r.Size, Location: backupLocation(r, cur, hasTarget), WPVersion: r.WPVersion, PHPVersion: r.PHPVersion,
		})
	}
	c.s.st.red.Learn(learn, "")
	return out, nil
}

func proposeBackupCreate(ctx context.Context, c *call, _ domainIn, d validate.Domain) (any, error) {
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanBackupCreate, Domain: d.String()})
}

func proposeBackupRestore(ctx context.Context, c *call, in backupRestoreIn, d validate.Domain) (any, error) {
	return propose(ctx, c, core.PlanRequest{Kind: core.PlanBackupRestore, Domain: d.String(), BackupID: in.BackupID})
}
