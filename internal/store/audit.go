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
	// TokenID là id của token API đã làm (M20); rỗng là phiên đăng nhập,
	// dòng lệnh hay bộ hẹn giờ. Chỉ id, không bao giờ là token.
	TokenID string `json:"token_id,omitempty"`
}

// AddAudit ghi một dòng nhật ký. Không bao giờ ghi mật khẩu: người gọi chỉ
// đưa vào tên thao tác và đối tượng.
func (s *Store) AddAudit(ctx context.Context, a AuditEntry) error {
	if a.Action == "" || a.Result == "" {
		return fmt.Errorf("ghi nhật ký: thiếu thao tác hoặc kết quả")
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO audit (time, ip, action, target, result, job_id, actor, token_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		fmtTime(a.Time), a.IP, a.Action, a.Target, a.Result, a.JobID, a.Actor, a.TokenID)
	if err != nil {
		return fmt.Errorf("ghi nhật ký %s: %w", a.Action, err)
	}
	return nil
}

// CountAuditSince đếm dòng nhật ký có đúng action và result từ since trở đi,
// theo target (với login.2fa_locked target là tên user: health đếm số lần mỗi
// user bị khoá bước hai). Không có dòng nào → map rỗng, không nil.
func (s *Store) CountAuditSince(ctx context.Context, action, result string, since time.Time) (map[string]int, error) {
	out := map[string]int{}
	// time ghi bằng fmtTime (độ rộng cố định) nên so chuỗi là so thời gian.
	err := queryEach(ctx, s.db,
		"SELECT target, COUNT(*) FROM audit WHERE action = ? AND result = ? AND time >= ? GROUP BY target",
		func(r rowScanner) error {
			var (
				target string
				n      int
			)
			if err := r.Scan(&target, &n); err != nil {
				return err
			}
			out[target] = n
			return nil
		}, action, result, fmtTime(since))
	if err != nil {
		return nil, fmt.Errorf("đếm nhật ký %s: %w", action, err)
	}
	return out, nil
}
