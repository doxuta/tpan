package core

// Xoá một bản backup khỏi máy theo yêu cầu của chủ (hay admin). Trước bản này
// backup chỉ mất khi bị dọn theo số bản giữ: user dùng gói có trần số bản
// backup chạm trần là không backup tay thêm được, mà cũng không có cách bỏ bản cũ.
//
// Cùng nghĩa với lúc dọn (prune): xoá file trên máy; bản đã lên kho thì dòng ở
// lại thành "chỉ còn trên kho" (kho do admin quản lý, dọn theo số bản giữ của
// kho), không thì xoá dòng. Bản chỉ còn trên kho không có gì để xoá ở đây.

import (
	"context"
	"fmt"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// CodeBackupRemoteOnly: bản backup chỉ còn trên kho, không có file trên máy để xoá.
const CodeBackupRemoteOnly = "backup_remote_only"

type backupDeleteStep struct {
	b   *Backups
	row Backup
}

func (s *backupDeleteStep) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "backup.delete:" + s.row.ID,
		Title: i18n.T("Xoá file backup "+s.row.Path, "Delete the backup file "+s.row.Path),
		Why: i18n.T(
			"Giải phóng chỗ trên đĩa và một chỗ trong số bản backup của gói. Bản đã đẩy lên kho thì bản trên kho vẫn còn.",
			"Frees disk space and one slot in the package's backup count. If the backup was pushed to storage, the copy there stays."),
		Danger: true,
		NoUndo: i18n.T("File đã xoá không lấy lại được.", "A deleted file cannot be brought back."),
	}
}

func (s *backupDeleteStep) Check(ctx context.Context, _ system.System) (bool, error) {
	row, ok, err := s.b.Store.Backup(ctx, s.row.ID)
	if err != nil {
		return false, err
	}
	return !ok || !row.Local, nil
}

func (s *backupDeleteStep) Apply(ctx context.Context, _ system.System) error {
	if err := s.b.removeBackupFile(s.row.Path); err != nil {
		return err
	}
	gone, err := s.b.Store.ForgetBackupLocal(ctx, s.row.ID)
	if err != nil {
		return fmt.Errorf("file %s đã xoá nhưng chưa ghi được vào bảng backups: %w", s.row.Path, err)
	}
	// Dòng đã mất hẳn (bản chưa lên kho). Nếu một job đẩy đang tải đúng bản này
	// thì khi xong nó sẽ không thấy dòng; dấu dưới đây bảo nó xoá object vừa tải
	// thay vì ghi lại dòng "chỉ còn trên kho": người dùng đã bảo xoá.
	if t, ok := s.b.Store.(backupTombstones); ok && gone {
		if err := t.MarkBackupDeleted(ctx, s.row.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *backupDeleteStep) Undo(context.Context, system.System) error { return nil }

// PrepareDelete dựng Plan "backup.delete" cho bản backup id. Chỉ đọc.
func (b *Backups) PrepareDelete(ctx context.Context, id string) (*Planned, error) {
	notFound := refuse("backup_not_found", i18n.T(
		"không có bản backup "+id+"; xem các bản có bằng: sudo tpan backup list",
		"there is no backup "+id+"; list the copies with: sudo tpan backup list"))
	if !backupIDRe.MatchString(id) || b.Store == nil {
		return nil, notFound
	}
	row, ok, err := b.Store.Backup(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc backup %s: %w", id, err)
	}
	if !ok {
		return nil, notFound
	}
	if !row.Local {
		return nil, refuse(CodeBackupRemoteOnly, i18n.T(
			"bản backup "+id+" chỉ còn trên kho, không có file trên máy để xoá; bản trên kho được dọn theo số bản giữ của kho",
			"backup "+id+" only exists in remote storage, there is no file on this machine to delete; copies in storage are pruned by the storage retention"))
	}
	title := i18n.T("Xoá bản backup "+id+" của "+row.Domain, "Delete backup "+id+" of "+row.Domain)
	if row.Remote == site.RemoteStored {
		title.Vi += " khỏi máy (bản trên kho vẫn còn)"
		title.En += " from this machine (the copy in storage stays)"
	}
	plan := engine.Plan{Kind: "backup.delete", Target: row.Domain, Title: title,
		Steps: []engine.Step{&backupDeleteStep{b: b, row: row}}}
	return newPlanned(b.Sites.Sys, b.Sites.Journal, plan, nil), nil
}

// backupTombstones là phần của store giữ dấu "người dùng đã xoá bản này"
// (store.MarkBackupDeleted, TakeBackupDeleted). Tách riêng để các store giả
// của test không phải cài.
type backupTombstones interface {
	MarkBackupDeleted(ctx context.Context, id string) error
	TakeBackupDeleted(ctx context.Context, id string) (bool, error)
}
