package store

import (
	"context"
	"fmt"
	"strconv"
)

// SiteUIDKey là khoá trong bảng settings giữ UID lớn nhất từng cấp cho user
// Linux của site (M28 đợt 2, F1 của rà soát v0.2.0). Không cần bảng mới hay
// migration: một dòng settings là đủ.
const SiteUIDKey = "site_uid_max"

// ReserveSiteUID cấp một số UID cho user site mới: max(floor, số lớn nhất từng
// cấp + 1), ghi số đó lại rồi trả về. Số đã cấp không bao giờ được cấp lại,
// kể cả khi user đã bị xoá hẳn: UID dùng lại là file sót (bản bash setuid ở
// /var/tmp) của site cũ thành của site mới.
//
// Một câu lệnh duy nhất (INSERT ... ON CONFLICT ... RETURNING), nên panel và
// CLI chạy cùng lúc trên cùng tpan.db vẫn nhận hai số khác nhau. Người gọi
// (steps.SiteUser) ghi số TRƯỚC khi chạy useradd: useradd lỗi thì số đó bỏ
// phí, không sao; ngược lại (useradd xong mà chưa kịp ghi) mới là dùng lại.
//
// Mất tpan.db (cài lại từ đầu) là mất lịch sử này; khi đó steps.SiteUser vẫn
// bắt đầu từ trên mọi UID đang có trong /etc/passwd, /etc/group của dải.
func (s *Store) ReserveSiteUID(ctx context.Context, floor int) (int, error) {
	// settings là bảng STRICT, cột value kiểu TEXT: đổi qua lại bằng CAST.
	// Giá trị cũ không phải số (sửa tay) thì CAST ra 0, tức là dùng floor.
	var v string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET
		   value = CAST(MAX(CAST(settings.value AS INTEGER) + 1, CAST(excluded.value AS INTEGER)) AS TEXT),
		   updated_at = excluded.updated_at
		 RETURNING value`,
		SiteUIDKey, strconv.Itoa(floor), fmtTime(s.now())).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("cấp UID cho user site: %w", err)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("cấp UID cho user site: giá trị %q trong settings không phải số", v)
	}
	return n, nil
}
