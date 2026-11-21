package store

// Hàm của store cho bảy cột "kho" của bảng backups (M18, migration 1800; mục
// 3.2 và 3.15 của docs/thay-doi/M18-thiet-ke.md). Các hàm này thoả
// core.RemoteStore (cùng ForgetBackupLocal ở backup.go).
//
// Luật chung: mỗi chuyển trạng thái là MỘT câu SQL có điều kiện hoặc một
// transaction. Hai tiến trình chạy cùng lúc (dọn trên máy trong `after` của
// `backup create`, dọn trên kho trong lượt đẩy) đọc-rồi-ghi qua hai lời gọi
// riêng sẽ chen nhau và để lại dòng local=0 mà không ở trên kho: bản đó mất
// hẳn, không còn đâu để khôi phục. Đẩy điều kiện xuống SQL thì SQLite tự tuần
// tự hoá các lần ghi.

import (
	"context"
	"fmt"
	"time"
)

// MarkBackupStored ghi nhận bản đã lên kho và đã đọc lại khớp sha256: remote
// ='stored', khoá object, mã đích, giờ; xoá lỗi và đếm lần thử. ok=false khi
// dòng không còn (bị xoá trong lúc đẩy): người gọi nên dọn object vừa đẩy.
func (s *Store) MarkBackupStored(ctx context.Context, id, key, targetID string, at time.Time) (ok bool, err error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE backups SET remote = 'stored', remote_key = ?, remote_target = ?, remote_at = ?,
		   remote_error = '', remote_tries = 0
		 WHERE id = ?`, key, targetID, fmtTime(at), id)
	if err != nil {
		return false, fmt.Errorf("ghi backup %s đã lên kho: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("ghi backup %s đã lên kho: %w", id, err)
	}
	return n > 0, nil
}

// AddBackupRemoteOnly ghi lại dòng của một bản chỉ còn trên kho (local=0,
// remote='stored') khi dòng đã bị dọn trên máy trong lúc bản đang được đẩy
// (F2 của lượt soát cuối). Lúc đó object vừa đọc lại khớp sha256 là bản duy
// nhất còn lại; xoá nó (cách cũ) là mất hẳn bản mà luật giữ trên kho muốn
// giữ. Dòng đã có thì không làm gì (ON CONFLICT DO NOTHING). Chủ lấy theo
// site như AddBackup; site không còn thì admin.
func (s *Store) AddBackupRemoteOnly(ctx context.Context, b Backup, key, targetID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO backups ("+backupInsertColumns+`, local, remote, remote_key, remote_target, remote_at, owner)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 'stored', ?, ?, ?, COALESCE((SELECT owner FROM sites WHERE domain = ?), 'admin'))
		 ON CONFLICT(id) DO NOTHING`,
		b.ID, b.Domain, b.Kind, b.Reason, b.Path, b.Size, b.SHA256,
		b.WPVersion, b.PHPVersion, b.JobID, fmtTime(b.Created),
		key, targetID, fmtTime(at), b.Domain)
	if err != nil {
		return fmt.Errorf("ghi lại backup %s (chỉ còn trên kho): %w", b.ID, err)
	}
	return nil
}

// MarkBackupPushFailed ghi lần đẩy lỗi. Một câu UPDATE có điều kiện, không
// đọc trước: dòng đang 'stored' (ở đích cũ, nay đẩy sang đích khác lỗi) giữ
// nguyên remote, remote_key, remote_target và đếm lần thử, vì bản vẫn còn
// nguyên trên kho cũ; chỉ remote_error được ghi để người dùng thấy. Dòng khác
// thành 'failed' ở đích targetID, và đếm thêm một lần thử khi countTry (lỗi
// "của riêng bản"; lỗi của đích không tính, mục 3.5). Dòng không còn thì
// không có gì để ghi và không phải lỗi.
func (s *Store) MarkBackupPushFailed(ctx context.Context, id, targetID, msg string, countTry bool) error {
	try := 0
	if countTry {
		try = 1
	}
	// Vế SET đọc giá trị CŨ của dòng (đặc tả của SQL), nên CASE nào cũng nhìn
	// cùng một remote, không phụ thuộc thứ tự các cột.
	_, err := s.db.ExecContext(ctx,
		`UPDATE backups SET
		   remote        = CASE WHEN remote = 'stored' THEN remote ELSE 'failed' END,
		   remote_target = CASE WHEN remote = 'stored' THEN remote_target ELSE ? END,
		   remote_error  = ?,
		   remote_tries  = CASE WHEN remote = 'stored' THEN remote_tries ELSE remote_tries + ? END
		 WHERE id = ?`, targetID, msg, try, id)
	if err != nil {
		return fmt.Errorf("ghi lỗi đẩy backup %s: %w", id, err)
	}
	return nil
}

// ResetBackupTries đưa số lần đẩy lỗi về 0 (người dùng bảo đẩy lại: `tpan
// backup push <id>`).
func (s *Store) ResetBackupTries(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE backups SET remote_tries = 0 WHERE id = ?", id); err != nil {
		return fmt.Errorf("đặt lại số lần đẩy của backup %s: %w", id, err)
	}
	return nil
}

// SetBackupLocal ghi nhận file đã tải về máy (local=1). ok=false khi dòng
// không còn.
func (s *Store) SetBackupLocal(ctx context.Context, id string) (ok bool, err error) {
	res, err := s.db.ExecContext(ctx, "UPDATE backups SET local = 1 WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("ghi backup %s đã có trên máy: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("ghi backup %s đã có trên máy: %w", id, err)
	}
	return n > 0, nil
}

// ForgetBackupRemote ghi nhận bản đã bị dọn khỏi kho: dòng còn file trên máy
// (local=1) thì chỉ xoá trạng thái kho, quay về "trên máy, chưa lên kho"; dòng
// chỉ còn trên kho (local=0) thì xoá hẳn, vì không còn nơi nào giữ bản. Một
// transaction cho cả hai câu, đối xứng với ForgetBackupLocal: dù hai hàm chạy
// theo thứ tự nào trên cùng một dòng 'stored', kết cục là dòng bị xoá, không
// bao giờ là local=0 với remote rỗng. deleted cho biết dòng đã bị xoá; dòng
// không có thì không lỗi.
func (s *Store) ForgetBackupRemote(ctx context.Context, id string) (deleted bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi kho: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }() // không làm gì nếu đã Commit
	// Câu ghi đi trước: transaction mở bằng một câu ghi thì không gặp lỗi
	// "database is locked" khi nâng từ đọc lên ghi.
	res, err := tx.ExecContext(ctx,
		`UPDATE backups SET remote = '', remote_key = '', remote_target = '', remote_at = '',
		   remote_error = '', remote_tries = 0
		 WHERE id = ? AND local = 1`, id)
	if err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi kho: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi kho: %w", id, err)
	}
	if n == 0 {
		res, err = tx.ExecContext(ctx, "DELETE FROM backups WHERE id = ? AND local = 0", id)
		if err != nil {
			return false, fmt.Errorf("dọn backup %s khỏi kho: %w", id, err)
		}
		if n, err = res.RowsAffected(); err != nil {
			return false, fmt.Errorf("dọn backup %s khỏi kho: %w", id, err)
		}
		deleted = n > 0
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("dọn backup %s khỏi kho: %w", id, err)
	}
	return deleted, nil
}

// SetBackupRemoteTarget đổi mã đích của dòng. Dùng khi đích đổi cách viết mà
// vẫn là cùng một kho (mục 3.3): nối lại các bản cũ vào đích mới.
func (s *Store) SetBackupRemoteTarget(ctx context.Context, id, targetID string) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE backups SET remote_target = ? WHERE id = ?", targetID, id); err != nil {
		return fmt.Errorf("đổi đích của backup %s: %w", id, err)
	}
	return nil
}

// AppliedMigrations trả các bản migration đã áp, tăng dần. Gói panel ghi danh
// sách này vào manifest để máy khôi phục từ chối gói của bản mới hơn nó biết.
func (s *Store) AppliedMigrations(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("đọc schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []int{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("đọc schema_migrations: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc schema_migrations: %w", err)
	}
	return out, nil
}
