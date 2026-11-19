package steps

// Hợp đồng M4: hai Step mới cho sửa tự động của health (mục 6.7 của
// docs/KE-HOACH.md). Thân hàm do thợ của phần core/steps viết. Theo quy ước
// của gói (common.go) và skill tpan-step.

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var errStepNotWritten = errors.New("steps: chưa viết")

var (
	_ engine.Step = (*PHPFPMRestart)(nil)
	_ engine.Step = (*DBUserUnlock)(nil)
)

// PHPFPMRestart khởi động lại php<PHPVersion>-fpm để socket của các pool được
// tạo lại (socket bị xoá thì reload không tạo lại, vì PHP-FPM giữ socket cũ
// qua lần reload; cần kiểm trên máy ảo). Check: Socket tồn tại và là socket.
// Apply: systemctl restart php<v>-fpm. Undo: không làm gì (NoUndo có lý do).
// Danger: true (mọi site dùng bản PHP này gián đoạn vài giây).
type PHPFPMRestart struct {
	PHPVersion string
	// Socket là socket phải có sau khi khởi động lại (site.Layout.Socket).
	Socket string
}

func (s *PHPFPMRestart) Info() engine.StepInfo {
	return engine.StepInfo{ID: "php.restart:" + s.PHPVersion}
}
func (s *PHPFPMRestart) Check(context.Context, system.System) (bool, error) {
	return false, errStepNotWritten
}
func (s *PHPFPMRestart) Apply(context.Context, system.System) error { return errStepNotWritten }
func (s *PHPFPMRestart) Undo(context.Context, system.System) error  { return nil }

// DBUserUnlock mở khoá user DB của site (ALTER USER '<db>'@'localhost' ACCOUNT
// UNLOCK, SQL qua stdin của mariadb như site_db.go). Check: user có và không
// bị khoá (đọc trạng thái khoá trong MariaDB 10.11, ví dụ SHOW CREATE USER có
// "ACCOUNT LOCK"). Undo: khoá lại nếu Apply đã mở.
type DBUserUnlock struct {
	Layout site.Layout
}

func (s *DBUserUnlock) Info() engine.StepInfo {
	return engine.StepInfo{ID: "db.unlock:" + s.Layout.Domain.String()}
}
func (s *DBUserUnlock) Check(context.Context, system.System) (bool, error) {
	return false, errStepNotWritten
}
func (s *DBUserUnlock) Apply(context.Context, system.System) error { return errStepNotWritten }
func (s *DBUserUnlock) Undo(context.Context, system.System) error  { return errStepNotWritten }
