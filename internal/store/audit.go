package store

import (
	"context"
	"fmt"
	"time"
)

// AuditEntry là một dòng nhật ký (mục 11): ai làm gì, lúc nào, kết quả ra sao.
type AuditEntry struct {
	Time   time.Time `json:"time"`
	IP     string    `json:"ip"`     // "cli" khi gõ ở dòng lệnh
	Action string    `json:"action"` // "login", "login.fail", "site.add", "admin.passwd"...
	Target string    `json:"target"`
	Result string    `json:"result"` // "ok" hoặc mã lỗi
	JobID  string    `json:"job_id"`
	// Actor là user TPAN đã làm (M13); rỗng là dòng lệnh, MCP hoặc bộ hẹn giờ.
	Actor string `json:"actor,omitempty"`
}

// AddAudit ghi một dòng nhật ký. Không bao giờ ghi mật khẩu: người gọi chỉ
// đưa vào tên thao tác và đối tượng.
func (s *Store) AddAudit(ctx context.Context, a AuditEntry) error {
	if a.Action == "" || a.Result == "" {
		return fmt.Errorf("ghi nhật ký: thiếu thao tác hoặc kết quả")
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO audit (time, ip, action, target, result, job_id, actor) VALUES (?, ?, ?, ?, ?, ?, ?)",
		fmtTime(a.Time), a.IP, a.Action, a.Target, a.Result, a.JobID, a.Actor)
	if err != nil {
		return fmt.Errorf("ghi nhật ký %s: %w", a.Action, err)
	}
	return nil
}
