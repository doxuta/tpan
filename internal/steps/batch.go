package steps

// Hợp đồng M6, đợt 0: ba mảnh để core dựng job hàng loạt (mục 7 của
// docs/KE-HOACH.md, engine.Batch). Thân viết ở đợt 1. Tiền tố riêng của file:
// "batch".

import (
	"context"
	"errors"
	"sync"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var _ engine.Step = (*ConfigReload)(nil)

var errBatchStepTODO = errors.New("steps: bước của job hàng loạt chưa viết (M6 đợt 1)")

// ConfigReload là bước của pha B: kiểm config một lần rồi reload một lần cho
// cả lô. Check luôn trả false (reload là việc phải làm mỗi lần chạy, không có
// "trạng thái đã đúng" để đọc). Apply: chạy Test (nil thì bỏ), lỗi thì trả lỗi
// kèm output; rồi Reload. Undo: không làm gì (NoUndo: "reload không có gì để
// trả lại; các bước ghi file tự trả file cũ"). ID: "service.reload:<Service>".
// Danger false: reload không làm rớt kết nối đang mở.
type ConfigReload struct {
	// Service chỉ để đặt ID và tiêu đề: "nginx", "apache2", "php8.3-fpm".
	Service string
	Test    *system.Cmd
	Reload  *system.Cmd
}

func (s *ConfigReload) Info() engine.StepInfo {
	return engine.StepInfo{ID: "service.reload:" + s.Service}
}
func (s *ConfigReload) Check(context.Context, system.System) (bool, error) {
	return false, errBatchStepTODO
}
func (s *ConfigReload) Apply(context.Context, system.System) error { return errBatchStepTODO }
func (s *ConfigReload) Undo(context.Context, system.System) error  { return nil }

// WebReload và PHPReload dựng ConfigReload với đúng lệnh kiểm và lệnh reload
// mà SiteVhost, SitePool dùng (webVhostOf, webPHPCmds của site_web.go). web là
// "nginx" | "apache"; phpVersion dạng "8.3". Tham số sai thì lỗi.
func WebReload(web string) (*ConfigReload, error) { return nil, errBatchStepTODO }
func PHPReload(phpVersion string) (*ConfigReload, error) {
	return nil, errBatchStepTODO
}

// WithoutReload bỏ Reload (giữ Test) của các bước *File, *Symlink, *Absent
// trong ss, tại chỗ, rồi trả lại ss. Bước loại khác giữ nguyên. Dùng cho pha
// A: file được ghi và kiểm cú pháp ngay, nhưng dịch vụ chỉ được nạp ở pha B.
// Undo của các bước này khi đó cũng không reload; engine.ExecuteBatch chạy
// lại pha B sau khi hoàn tác một unit ở pha C.
func WithoutReload(ss []engine.Step) []engine.Step { return ss }

// Serial bọc một bước để Apply và Undo của nó chạy dưới mu: các unit của job
// hàng loạt chạy song song, nhưng hai việc không được chạy cùng lúc:
//   - useradd/userdel/usermod (khoá /etc/passwd; tiến trình thứ hai lỗi
//     "cannot lock /etc/passwd");
//   - ghi file rồi `nginx -t`/`apachectl configtest`/`php-fpm -t` (kiểm cả
//     config: file dở của site B làm site A lỗi oan).
//
// Info và Check đi thẳng (chỉ đọc). Bước được bọc giữ nguyên ID.
func Serial(mu *sync.Mutex, s engine.Step) engine.Step { return s }

// batchTitle tránh "imported and not used" cho i18n khi chưa có thân.
var _ = i18n.T
