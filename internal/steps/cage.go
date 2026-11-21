package steps

// Step của lồng theo site (M17, mục 3.5 của docs/thay-doi/M17-thiet-ke.md).
// Hợp đồng đợt 0: kiểu, trường và tài liệu; thân hàm do thợ đợt 2 viết, kèm
// bốn trường hợp test chuẩn của skill tpan-step cho mỗi Step.

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var errCageStepTODO = errors.New("bước của lồng: chưa viết (M17 đợt 2)")

var (
	_ engine.Step = (*CageFiles)(nil)
	_ engine.Step = (*CageSocket)(nil)
	_ engine.Step = (*CageReap)(nil)
	_ engine.Step = (*CageRestart)(nil)
)

// CageFiles ghi (hoặc gỡ, khi Remove) bảy file của lồng của Layout:
// l.CageFiles() (slice, socket, dịch vụ, hàng rào, php-fpm.conf, passwd, group),
// nội dung từ templates.CageSlice, CageSocketUnit, CageServiceUnit, CageFence,
// CageFPMConf(l, Limits.Workers()); passwd và group mỗi file 4 dòng từ
// `getent passwd root www-data <user> nobody` và `getent group root www-data
// <user> nogroup` (kiểm từng dòng đúng dạng và đúng 4 tên trước khi ghi).
// Quyền: file 0644 root:root; /etc/tpan/cage/<slug> 0755; /etc/tpan/cage 0700;
// /etc/tpan chỉ tạo khi chưa có (0700), không bao giờ đổi quyền hay xoá.
//
// Check: cả bảy file đúng nội dung, quyền, chủ; Remove: không còn file hay thư
// mục nào của lồng.
//
// Apply (ghi): giữ bản cũ của mọi file và trạng thái active của socket và dịch
// vụ; chỉ ghi file khác nội dung, quyền hay chủ, theo thứ tự slice, socket,
// dịch vụ, php-fpm.conf, passwd, group, HÀNG RÀO SAU CÙNG (lớp bọc lệnh coi
// "có hàng rào" là "có lồng"); `systemctl daemon-reload`. Nếu file dịch vụ hay
// php-fpm.conf vừa được ghi: `php-fpm<PHP> -t --fpm-config <l.CageFPMConf()>`
// với AsUser = user của site (qua lớp bọc nên chạy trong lồng); hỏng thì đi
// đường trả lại và báo lỗi, chưa đụng tới dịch vụ. Rồi nếu dịch vụ đang chạy
// và một trong dịch vụ, hàng rào, php-fpm.conf, passwd, group vừa được ghi:
// `systemctl try-restart <dịch vụ>`; lệnh này hỏng thì ghi vào log (OnOutput
// không có: trả về trong lời nhắn của bước qua một dòng output) nhưng KHÔNG
// làm bước lỗi (mục 3.5: dịch vụ sống hay chết sau đó do phép kiểm và Plan gọi
// bước lo).
// Apply (gỡ): gỡ hàng rào TRƯỚC TIÊN, rồi các file còn lại, thư mục
// /etc/tpan/cage/<slug> và .service.d; /etc/tpan/cage nếu rỗng; daemon-reload;
// `systemctl stop 'tpan-cage-<slug>-run-*'`, `systemctl reset-failed
// 'tpan-cage-<slug>-*'` (bỏ qua lỗi).
// Đường trả lại (lỗi giữa Apply, và Undo): trả mọi file về bản cũ (file mới thì
// xoá, thư mục mới thì gỡ), daemon-reload, `systemctl reset-failed <dịch vụ>
// <socket>`, socket từng active thì `systemctl start <socket>` và chờ file
// socket (tối đa 20 giây), dịch vụ từng active thì `systemctl start <dịch vụ>`.
// Không đưa về được thì Undo trả lỗi.
// Info().Files: đúng năm file của l.CageManagedFiles() (Delete khi Remove);
// passwd và group không bao giờ khai.
type CageFiles struct {
	Layout site.Layout
	PHP    string // bản PHP của site (dòng ExecStart)
	Limits account.Limits
	Remove bool
}

func (s *CageFiles) Info() engine.StepInfo {
	return engine.StepInfo{ID: "cage.files:" + s.Layout.Domain.String(),
		Title: i18n.T("Ghi file của lồng (chưa viết)", "Write the cage files (not written yet)")}
}
func (s *CageFiles) Check(context.Context, system.System) (bool, error) {
	return false, errCageStepTODO
}
func (s *CageFiles) Apply(context.Context, system.System) error { return errCageStepTODO }
func (s *CageFiles) Undo(context.Context, system.System) error  { return errCageStepTODO }

// Want trả nội dung mong đợi của một trong năm file của CageManagedFiles (cho
// sửa tự động file.rewrite: sha256 mong đợi). ok=false với đường dẫn khác.
func (s *CageFiles) Want(path string) ([]byte, bool) { return nil, false }

// CageSocket bật (hoặc tắt, khi Disable) socket của lồng.
//
// Bật: Check đúng khi unit socket enabled và active, file l.CageSocket() có và
// là socket. Apply: `php-fpm<PHP> -t --fpm-config <conf>` với AsUser (trong
// lồng); `systemctl reset-failed <socket> <dịch vụ>` (bỏ qua lỗi "not loaded");
// `systemctl enable --now <socket>`; chờ tối đa 20 giây tới khi file socket
// có. KHÔNG khởi động dịch vụ (request đầu tiên đánh thức nó): không gì do mã
// của site điều khiển làm bước này hỏng. Hỏng thì lỗi kèm 20 dòng cuối
// `journalctl -u <dịch vụ> -u <socket> --no-pager -o cat`, đã bỏ ký tự điều
// khiển và ghi rõ là output của site. Undo: `disable --now` socket, `stop`
// dịch vụ.
// Tắt: Check đúng khi socket không enabled, không active, dịch vụ không active.
// Apply: `systemctl disable --now <socket>`, `systemctl stop <dịch vụ>`,
// `systemctl stop <slice>`, gỡ thư mục l.CageRunDir() nếu rỗng. Danger.
// Undo: reset-failed, enable --now socket, chờ file socket; dịch vụ từng chạy
// thì start lại.
type CageSocket struct {
	Layout  site.Layout
	PHP     string
	Disable bool
}

func (s *CageSocket) Info() engine.StepInfo {
	return engine.StepInfo{ID: "cage.socket:" + s.Layout.Domain.String(),
		Title: i18n.T("Bật socket của lồng (chưa viết)", "Start the cage socket (not written yet)")}
}
func (s *CageSocket) Check(context.Context, system.System) (bool, error) {
	return false, errCageStepTODO
}
func (s *CageSocket) Apply(context.Context, system.System) error { return errCageStepTODO }
func (s *CageSocket) Undo(context.Context, system.System) error  { return errCageStepTODO }

// CageReap giết (SIGKILL) mọi tiến trình có uid là user của site mà không nằm
// trong slice của site (mục 3.5, soát thiết kế điểm 4). Uid: `id -u <user>`.
// Đọc /proc bằng System (ReadDir("/proc"), ReadFile status và cgroup của từng
// số; tiến trình biến mất giữa chừng thì bỏ qua); đọc lại status ngay trước
// khi giết (cùng uid, vẫn ngoài slice); giết bằng `kill -KILL <pid>...` (argv).
// Check đúng khi không còn tiến trình nào như vậy. Undo không làm gì (NoUndo).
type CageReap struct {
	Layout site.Layout
}

func (s *CageReap) Info() engine.StepInfo {
	return engine.StepInfo{ID: "cage.reap:" + s.Layout.Domain.String(),
		Title: i18n.T("Dừng tiến trình của site còn ở ngoài lồng (chưa viết)", "Stop the site's processes outside the cage (not written yet)")}
}
func (s *CageReap) Check(context.Context, system.System) (bool, error) { return false, errCageStepTODO }
func (s *CageReap) Apply(context.Context, system.System) error         { return errCageStepTODO }
func (s *CageReap) Undo(context.Context, system.System) error          { return nil }

// CageRestart là sửa tự động cage.restart (mục 3.5, 3.7): chạy
// templates.CageMySQLDirCmd bằng root ở máy thật, rồi việc của CageReap, rồi
// `systemctl try-restart <dịch vụ>`. Check đúng khi dịch vụ không chạy, hoặc
// khi mọi điều của checks.CageRuntime đạt. Undo không làm gì (NoUndo).
type CageRestart struct {
	Layout site.Layout
}

func (s *CageRestart) Info() engine.StepInfo {
	return engine.StepInfo{ID: "cage.restart:" + s.Layout.Domain.String(),
		Title: i18n.T("Khởi động lại dịch vụ của lồng (chưa viết)", "Restart the cage service (not written yet)")}
}
func (s *CageRestart) Check(context.Context, system.System) (bool, error) {
	return false, errCageStepTODO
}
func (s *CageRestart) Apply(context.Context, system.System) error { return errCageStepTODO }
func (s *CageRestart) Undo(context.Context, system.System) error  { return nil }

// SiteVhostCaged ghi vhost bản trong lồng (templates.SiteVhostCaged: socket là
// l.CageSocket()) vào sites-available rồi bật bằng symlink, như SiteVhost và
// SiteVhostSSL (ssl chọn bản có cổng 443). Cùng đường dẫn file với vhost ngoài
// lồng: vào lồng và ra lồng là ghi đè cùng một file.
func SiteVhostCaged(l site.Layout, web, acmeRoot string, ssl bool, mode site.HtaccessMode) ([]engine.Step, error) {
	return nil, errCageStepTODO
}
