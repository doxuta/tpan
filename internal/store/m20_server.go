package store

// Hai truy vấn nhỏ mà server của M20 cần (docs/thay-doi/M20-thiet-ke.md mục
// 11.12 R1, R4), tách khỏi m20.go để không đụng file của phần store.

import (
	"context"
	"fmt"
)

// SetSessionHost gắn tên máy cho một phiên của bản cũ (host rỗng) ở lần đầu
// nó được dùng (R1). Chỉ ghi khi host đang rỗng: phiên đã gắn tên thì không
// bao giờ bị đổi tên, kể cả khi hai request đua nhau ghi.
func (s *Store) SetSessionHost(ctx context.Context, tokenSHA256, host string) error {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET host = ? WHERE token_sha256 = ? AND host = ''", host, tokenSHA256); err != nil {
		return fmt.Errorf("gắn tên máy cho phiên: %w", err)
	}
	return nil
}

// AnyTOTP: bảng user_totp có ít nhất một dòng (đã bật hoặc đang chờ xác
// nhận). Có dòng thì đã có bí mật niêm phong bằng file khoá, nên file khoá
// mất không được sinh lại (R4): sinh khoá mới chỉ che lỗi.
func (s *Store) AnyTOTP(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM user_totp)").Scan(&n); err != nil {
		return false, fmt.Errorf("đọc bảng hai bước: %w", err)
	}
	return n == 1, nil
}
